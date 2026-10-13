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
)

func TestNativeAnthropicLifecycle(t *testing.T) {
	t.Setenv("NATIVE_CLAUDE_TEST_KEY", "native-key-canary")
	for _, scenario := range []string{"normal", "bad-index", "missing-signature", "missing-stop", "provider-error", "timeout", "authentication", "oversize"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != "native-key-canary" || r.Header.Get("anthropic-version") != "2023-06-01" || r.Header.Get("Authorization") != "" {
					t.Error("incorrect Claude endpoint/authentication")
				}
				if scenario == "authentication" {
					w.WriteHeader(401)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				write := func(value string) { fmt.Fprintf(w, "data: %s\n\n", value); w.(http.Flusher).Flush() }
				write(`{"type":"message_start","message":{"role":"assistant","usage":{"input_tokens":8,"cache_read_input_tokens":2}}}`)
				if scenario == "provider-error" {
					write(`{"type":"error","error":{"message":"private-provider-canary"}}`)
					return
				}
				if scenario == "timeout" {
					<-r.Context().Done()
					return
				}
				write(`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`)
				if scenario == "bad-index" {
					write(`{"type":"content_block_delta","index":1,"delta":{"type":"thinking_delta","thinking":"think"}}`)
					return
				}
				write(`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"think"}}`)
				if scenario != "missing-signature" {
					write(`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"original-signature"}}`)
				}
				write(`{"type":"content_block_stop","index":0}`)
				write(`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`)
				write(`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"OK"}}`)
				write(`{"type":"content_block_stop","index":1}`)
				write(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":10}}`)
				if scenario != "missing-stop" {
					write(`{"type":"message_stop"}`)
				}
			}))
			defer fixture.Close()
			u := localSettings(fixture.URL)
			u.Kind, u.APIKeyEnv = "anthropic", "NATIVE_CLAUDE_TEST_KEY"
			u.Timeouts.FirstEvent = config.Duration(200 * time.Millisecond)
			client := New(u)
			defer client.Close()
			target := config.Target{Model: config.StringValue{Literal: "actual"}, Reasoning: config.Reasoning{Mode: "anthropic"}}
			r := &core.Request{Protocol: core.Anthropic, Stream: true, ToolChoice: "auto", Messages: []core.Message{{Role: "user", Parts: []core.Part{{Kind: "text", Text: "你好"}}}}}
			limits := config.Server{MaxResponseBytes: 65536, MaxSSEEventBytes: 65536}
			if scenario == "oversize" {
				limits.MaxResponseBytes = 8
			}
			var events []core.Event
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err := client.Run(ctx, r, target, u, limits, func(event core.Event) error { events = append(events, event); return nil })
			if (err == nil) != (scenario == "normal") {
				t.Fatal(scenario, err)
			}
			finished := false
			for _, event := range events {
				if event.Kind == "finish" {
					finished = true
					if event.Usage.Input != 10 {
						t.Fatal("native cache usage", event.Usage)
					}
				}
				if event.Kind == "thinking_block" && (event.Part.Signature != "original-signature" || event.Part.Text != "think") {
					t.Fatal("thinking changed before replay")
				}
			}
			if finished != (scenario == "normal") {
				t.Fatal("invalid stream success marker")
			}
			if err != nil && strings.Contains(err.Message, "private-provider-canary") {
				t.Fatal("provider message leaked")
			}
		})
	}
}

func TestNativeAnthropicJSONAndSignedTranslation(t *testing.T) {
	maximum := 2048
	r := &core.Request{Protocol: core.Anthropic, MaxTokens: &maximum, ToolChoice: "auto", Messages: []core.Message{
		{Role: "system", Parts: []core.Part{{Kind: "text", Text: "system"}}},
		{Role: "assistant", Parts: []core.Part{{Kind: "thinking", Text: "original thought\n", Signature: "exact-signature", Opaque: true}, {Kind: "redacted_thinking", Data: "exact-data", Opaque: true}, {Kind: "text", Text: "old"}}},
		{Role: "user", Parts: []core.Part{{Kind: "text", Text: "你好"}}},
	}}
	target := config.Target{Model: config.StringValue{Literal: "actual"}, Reasoning: config.Reasoning{Mode: "anthropic", History: "preserve"}}
	body, err := Translate(r, target)
	if err != nil {
		t.Fatal(err)
	}
	blocks := body["messages"].([]any)[0].(map[string]any)["content"].([]any)
	if blocks[0].(map[string]any)["thinking"] != "original thought\n" || blocks[0].(map[string]any)["signature"] != "exact-signature" || blocks[1].(map[string]any)["data"] != "exact-data" {
		t.Fatal("signed source blocks modified")
	}
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "thinking", "thinking": "next thought", "signature": "next-signature"}, map[string]any{"type": "text", "text": "OK"}}, "stop_reason": "end_turn", "usage": map[string]any{"input_tokens": 8, "output_tokens": 10}})
	}))
	defer fixture.Close()
	u := localSettings(fixture.URL)
	u.Kind = "anthropic"
	client := New(u)
	defer client.Close()
	var thinking *core.Part
	var text string
	err = client.Run(context.Background(), r, target, u, config.Server{MaxResponseBytes: 65536}, func(event core.Event) error {
		if event.Kind == "thinking_block" {
			thinking = event.Part
		}
		if event.Kind == "text_delta" {
			text += event.Text
		}
		return nil
	})
	if err != nil || thinking == nil || thinking.Signature != "next-signature" || text != "OK" {
		t.Fatal("native JSON thinking/body", err)
	}
}
