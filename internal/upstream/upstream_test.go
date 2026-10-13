package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"fakeapi/internal/config"
	"fakeapi/internal/core"
	"fakeapi/internal/protocol"
)

func TestSSEParser(t *testing.T) {
	var data []string
	e := ReadSSE(strings.NewReader(":comment\r\nevent: chunk\r\ndata: {\"a\":\r\ndata: 1}\r\n\r\ndata: [DONE]\r\n\r\n"), 4096, func(s string) error { data = append(data, s); return nil })
	if e == nil || len(data) != 2 || data[0] != "{\"a\":\n1}" {
		t.Fatal(data, e)
	}
	if e = ReadSSE(strings.NewReader("data: "+strings.Repeat("x", 100)), 32, func(string) error { return nil }); e == nil {
		t.Fatal("oversized line accepted")
	}
	big := strings.Repeat("a", 100000)
	var got string
	_ = ReadSSE(strings.NewReader("data: "+big+"\n\n"), 1<<20, func(s string) error { got = s; return nil })
	if got != big {
		t.Fatal("large valid event truncated")
	}
}
func TestStreamingLifecycle(t *testing.T) {
	for _, scenario := range []string{"normal", "empty", "role-error", "partial-eof", "missing-done", "bad-json", "timeout"} {
		t.Run(scenario, func(t *testing.T) {
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				ioEvent := func(v any) { b, _ := json.Marshal(v); fmt.Fprintf(w, "data: %s\n\n", b); w.(http.Flusher).Flush() }
				ioEvent(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant"}, "finish_reason": nil}}})
				switch scenario {
				case "role-error":
					fmt.Fprint(w, "data: {\"error\":{\"message\":\"private-canary\"}}\n\n")
					return
				case "bad-json":
					fmt.Fprint(w, "data: invalid\n\n")
					return
				case "timeout":
					<-r.Context().Done()
					return
				}
				if scenario != "empty" {
					ioEvent(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "hello"}, "finish_reason": nil}}})
				}
				if scenario == "partial-eof" {
					return
				}
				ioEvent(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}}})
				if scenario != "missing-done" {
					fmt.Fprint(w, "data: [DONE]\n\n")
				}
			}))
			defer up.Close()
			u := config.Upstream{BaseURL: config.StringValue{Literal: up.URL + "/v1"}, APIKeyEnv: "UPSTREAM_TEST_KEY", MaxConcurrent: 4, Timeouts: config.Timeouts{Connect: config.Duration(time.Second), FirstEvent: config.Duration(100 * time.Millisecond), Idle: config.Duration(time.Second), Total: config.Duration(2 * time.Second)}}
			c := New(u)
			defer c.Close()
			r := &core.Request{Protocol: core.Chat, Stream: true, Model: "display", ToolChoice: "auto", Messages: []core.Message{{Role: "user", Parts: []core.Part{{Kind: "text", Text: "task"}}}}}
			target := config.Target{Model: config.StringValue{Literal: "actual"}, MaxTokensParameter: "max_tokens"}
			var events []core.Event
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			e := c.Run(ctx, r, target, u, config.Server{MaxResponseBytes: 1 << 20, MaxSSEEventBytes: 1 << 20}, func(e core.Event) error { events = append(events, e); return nil })
			success := scenario == "normal" || scenario == "empty"
			if (e == nil) != success {
				t.Fatal(scenario, e)
			}
			finished := false
			for _, event := range events {
				if event.Kind == "finish" {
					finished = true
				}
			}
			if finished != success {
				t.Fatal("incorrect terminal event")
			}
			if e != nil && strings.Contains(e.Message, "private-canary") {
				t.Fatal("upstream error leak")
			}
			if scenario == "role-error" && len(events) != 0 {
				t.Fatal("role-only committed semantic output")
			}
		})
	}
}
func TestFullContextTranslation(t *testing.T) {
	r := &core.Request{Protocol: core.Chat, Model: "display", ToolChoice: "auto", Messages: []core.Message{{Role: "system", Parts: []core.Part{{Kind: "text", Text: "instruction"}}}, {Role: "user", Parts: []core.Part{{Kind: "text", Text: "old"}}}, {Role: "assistant", Parts: []core.Part{{Kind: "tool_call", ID: "a", Name: "f", Arguments: json.RawMessage(`{"x":1}`)}}}, {Role: "tool", Parts: []core.Part{{Kind: "tool_result", ID: "a", Text: "result"}}}, {Role: "user", Parts: []core.Part{{Kind: "text", Text: "hello"}}}}}
	target := config.Target{Model: config.StringValue{Literal: "actual"}, MaxTokensParameter: "max_tokens"}
	v, e := Translate(r, target)
	if e != nil {
		t.Fatal(e)
	}
	if v["model"] != "actual" || len(v["messages"].([]any)) != 5 {
		t.Fatal("context dropped")
	}
	r.Messages = append(r.Messages, core.Message{Role: "developer", Parts: []core.Part{{Kind: "text", Text: "private"}}})
	if e := Preflight(r, target, config.Upstream{}); e == nil {
		t.Fatal("unsupported developer accepted")
	}
	target.DeveloperRoleFallback = "system"
	if e := Preflight(r, target, config.Upstream{}); e != nil {
		t.Fatal(e)
	}
}

func TestChatReasoningEffortReachesProvider(t *testing.T) {
	for _, effort := range []string{"none", "low", "high", "max"} {
		r, err := protocol.Decode(core.Chat, strings.NewReader(`{"model":"display","messages":[{"role":"user","content":"你好"}],"reasoning_effort":"`+effort+`"}`), "", false)
		if err != nil {
			t.Fatal(err)
		}
		body, err := Translate(r, config.Target{Model: config.StringValue{Literal: "actual"}})
		if err != nil || body["reasoning_effort"] != effort {
			t.Fatal("provider did not receive reasoning preference", body, err)
		}
	}
}
