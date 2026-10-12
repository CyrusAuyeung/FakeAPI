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

func localSettings(url string) config.Upstream {
	return config.Upstream{BaseURL: config.StringValue{Literal: url + "/v1"}, MaxConcurrent: 2, ReasoningTranslation: "omit", Timeouts: config.Timeouts{Connect: config.Duration(time.Second), FirstEvent: config.Duration(time.Second), Idle: config.Duration(time.Second), Total: config.Duration(3 * time.Second)}}
}

func TestUsageMapping(t *testing.T) {
	cases := []struct {
		name               string
		usage              map[string]any
		visible, reasoning int
		source             string
	}{
		{"provided", map[string]any{"prompt_tokens": 7, "completion_tokens": 10, "completion_tokens_details": map[string]any{"reasoning_tokens": 4}}, 6, 4, "upstream"},
		{"mixed", map[string]any{"prompt_tokens": 7, "completion_tokens": 10}, 8, 2, "mixed"},
		{"negative-reasoning", map[string]any{"prompt_tokens": 7, "completion_tokens": 10, "completion_tokens_details": map[string]any{"reasoning_tokens": -1}}, 2, 2, "estimated"},
		{"excess-reasoning", map[string]any{"prompt_tokens": 7, "completion_tokens": 10, "completion_tokens_details": map[string]any{"reasoning_tokens": 11}}, 2, 2, "estimated"},
		{"negative-completion", map[string]any{"prompt_tokens": 7, "completion_tokens": -1}, 2, 2, "estimated"},
		{"invalid-cached", map[string]any{"prompt_tokens": 7, "completion_tokens": 10, "prompt_tokens_details": map[string]any{"cached_tokens": 8}}, 2, 2, "estimated"},
		{"absent", nil, 2, 2, "estimated"},
	}
	for _, stream := range []bool{false, true} {
		for _, protocol := range []core.Protocol{core.Chat, core.Responses, core.Anthropic, core.Gemini} {
			for _, tc := range cases {
				t.Run(fmt.Sprintf("%s/%t/%s", protocol, stream, tc.name), func(t *testing.T) {
					up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, h *http.Request) {
						if !stream {
							json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "answer", "reasoning_content": "think"}, "finish_reason": "stop"}}, "usage": tc.usage})
							return
						}
						w.Header().Set("Content-Type", "text/event-stream")
						for _, choice := range []any{map[string]any{"delta": map[string]any{"reasoning_content": "think"}}, map[string]any{"delta": map[string]any{"content": "answer"}, "finish_reason": "stop"}} {
							b, _ := json.Marshal(map[string]any{"choices": []any{choice}})
							fmt.Fprintf(w, "data: %s\n\n", b)
						}
						b, _ := json.Marshal(map[string]any{"choices": []any{}, "usage": tc.usage})
						fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", b)
					}))
					defer up.Close()
					u := localSettings(up.URL)
					if protocol != core.Chat {
						u.ReasoningTranslation = "portable_text"
					}
					client := New(u)
					defer client.Close()
					r := &core.Request{Protocol: protocol, Stream: stream, ToolChoice: "auto", Messages: []core.Message{{Role: "user", Parts: []core.Part{{Kind: "text", Text: "task"}}}}}
					var usage core.Usage
					var text, reason string
					e := client.Run(context.Background(), r, config.Target{Model: config.StringValue{Literal: "actual"}}, u, config.Server{MaxResponseBytes: 1 << 20, MaxSSEEventBytes: 1 << 20}, func(event core.Event) error {
						if event.Kind == "text_delta" {
							text += event.Text
						}
						if event.Kind == "reasoning_delta" {
							reason += event.Text
						}
						if event.Kind == "finish" {
							usage = event.Usage
						}
						return nil
					})
					if e != nil {
						t.Fatal(e)
					}
					wantVisible, wantReason, source := tc.visible, tc.reasoning, tc.source
					if protocol == core.Chat {
						if text != "answer" || reason != "think" {
							t.Fatal("native reasoning mixed into body", text, reason)
						}
					} else {
						if text != "[上游推理] think\n\nanswer" || reason != "" {
							t.Fatal("portable reasoning duplicated or missing separator", text, reason)
						}
						wantReason = 0
						if source == "estimated" || protocol == core.Anthropic && stream {
							wantVisible, source = core.Estimate(text), "estimated"
						} else {
							wantVisible = 10
						}
					}
					if usage.Visible != wantVisible || usage.Reasoning != wantReason || usage.Source != source {
						t.Fatalf("usage: %+v, want visible=%d reasoning=%d source=%s", usage, wantVisible, wantReason, source)
					}
					if source == "estimated" && usage.Input != core.InputUsage(r) {
						t.Fatal("invalid usage retained")
					}
				})
			}
		}
	}
}

func TestToolStreamValidation(t *testing.T) {
	for _, scenario := range []string{"valid", "undeclared", "bad-args", "changed-id", "after-finish"} {
		t.Run(scenario, func(t *testing.T) {
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, h *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				send := func(v any) { b, _ := json.Marshal(v); fmt.Fprintf(w, "data: %s\n\n", b) }
				name := "weather"
				if scenario == "undeclared" {
					name = "private-function"
				}
				call := map[string]any{"index": 0, "id": "one", "type": "function", "function": map[string]any{"name": name, "arguments": `{"city":`}}
				send(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{call}}}}})
				args := `"test"}`
				if scenario == "bad-args" {
					args = `42}`
				}
				call = map[string]any{"index": 0, "function": map[string]any{"arguments": args}}
				if scenario == "changed-id" {
					call["id"] = "two"
				}
				send(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{call}}, "finish_reason": "tool_calls"}}})
				if scenario == "after-finish" {
					send(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": "extra"}}}})
				}
				fmt.Fprint(w, "data: [DONE]\n\n")
			}))
			defer up.Close()
			u := localSettings(up.URL)
			client := New(u)
			defer client.Close()
			r := &core.Request{Protocol: core.Chat, Stream: true, ToolChoice: "required", Tools: []core.Tool{{Name: "weather", Schema: map[string]any{"type": "object", "properties": map[string]any{"city": map[string]any{"type": "string"}}, "required": []string{"city"}, "additionalProperties": false}}}}
			var events []core.Event
			e := client.Run(context.Background(), r, config.Target{Model: config.StringValue{Literal: "actual"}}, u, config.Server{MaxResponseBytes: 1 << 20, MaxSSEEventBytes: 1 << 20}, func(event core.Event) error { events = append(events, event); return nil })
			if (e == nil) != (scenario == "valid") {
				t.Fatal(scenario, e)
			}
			if scenario == "undeclared" && len(events) != 0 {
				t.Fatal("undeclared function committed output")
			}
			for _, event := range events {
				if scenario != "valid" && event.Kind == "finish" {
					t.Fatal("invalid stream reported success")
				}
			}
		})
	}
}

func TestNativeJSONAndOmittedReasoningLimit(t *testing.T) {
	for _, oversized := range []bool{false, true} {
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, h *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			count := 1
			if oversized {
				count = 20
			}
			for i := 0; i < count; i++ {
				fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"thinking\"}}]}\n\n")
			}
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"{}\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		}))
		u := localSettings(up.URL)
		client := New(u)
		r := &core.Request{Protocol: core.Responses, Stream: true, ToolChoice: "auto", Constraint: core.Constraint{Kind: "json_object"}}
		var text strings.Builder
		e := client.Run(context.Background(), r, config.Target{Model: config.StringValue{Literal: "actual"}}, u, config.Server{MaxResponseBytes: 64, MaxSSEEventBytes: 1024}, func(event core.Event) error {
			if event.Kind == "text_delta" {
				text.WriteString(event.Text)
			}
			return nil
		})
		client.Close()
		up.Close()
		if (e != nil) != oversized {
			t.Fatal(oversized, e)
		}
		if !oversized && text.String() != "{}" {
			t.Fatal(text.String())
		}
	}
}
