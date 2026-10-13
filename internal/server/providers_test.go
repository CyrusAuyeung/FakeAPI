package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"fakeapi/internal/config"
	"fakeapi/internal/core"
	"fakeapi/internal/upstream"
)

func providerHarness(t *testing.T, name string, handler http.HandlerFunc) *harness {
	t.Helper()
	raw, err := os.ReadFile("../../examples/providers/" + name + ".yaml")
	if err != nil {
		t.Fatal(err)
	}
	var overlay struct {
		Upstreams map[string]config.Upstream `yaml:"upstreams"`
		Targets   map[string]config.Target   `yaml:"targets"`
	}
	if err := config.Strict(raw, &overlay); err != nil {
		t.Fatal(err)
	}
	fixture := httptest.NewServer(handler)
	t.Cleanup(fixture.Close)
	return setup(t, func(c *config.Config) {
		c.Upstreams, c.Targets = overlay.Upstreams, overlay.Targets
		c.DefaultTarget = "primary-fast"
		u := c.Upstreams["primary"]
		u.BaseURL = config.StringValue{Literal: fixture.URL + "/v1"}
		c.Upstreams["primary"] = u
		p := c.KeyPolicies["test"]
		limit := int64(100)
		p.Tags, p.AILimit = []string{"ai"}, &limit
		c.KeyPolicies["test"] = p
		for i := range c.Rules {
			if strings.HasPrefix(c.Rules[i].ID, "ai-") {
				enabled := true
				c.Rules[i].Enabled = &enabled
			}
		}
	})
}

func fixtureReply(w http.ResponseWriter, native, stream bool) {
	reason, sig := "fixture reasoning", "fixture-signature"
	if !native {
		if !stream {
			json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "OK", "reasoning_content": reason}, "finish_reason": "stop"}}, "usage": map[string]any{"prompt_tokens": 8, "completion_tokens": 10, "completion_tokens_details": map[string]any{"reasoning_tokens": 8}}})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, delta := range []any{map[string]any{"reasoning_content": reason}, map[string]any{"content": "OK"}} {
			encoded, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": delta}}})
			fmt.Fprintf(w, "data: %s\n\n", encoded)
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":10}}\n\ndata: [DONE]\n\n")
		return
	}
	if !stream {
		json.NewEncoder(w).Encode(map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "thinking", "thinking": reason, "signature": sig}, map[string]any{"type": "redacted_thinking", "data": "opaque-fixture"}, map[string]any{"type": "text", "text": "OK"}}, "stop_reason": "end_turn", "usage": map[string]any{"input_tokens": 8, "output_tokens": 10}})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	values := []string{
		`{"type":"message_start","message":{"role":"assistant","usage":{"input_tokens":8}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"fixture reasoning"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"fixture-signature"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"redacted_thinking","data":"opaque-fixture"}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"content_block_start","index":2,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":"OK"}}`,
		`{"type":"content_block_stop","index":2}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":10}}`,
		`{"type":"message_stop"}`,
	}
	for _, value := range values {
		fmt.Fprintf(w, "data: %s\n\n", value)
	}
}

func TestProviderTemplatesFourProtocolReasoningControls(t *testing.T) {
	for _, provider := range []string{"deepseek", "gpt", "claude", "gemini", "glm", "kimi"} {
		t.Run(provider, func(t *testing.T) {
			var calls atomic.Int64
			h := providerHarness(t, provider, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					return
				}
				if provider == "claude" {
					if !strings.HasSuffix(r.URL.Path, "/messages") || body["reasoning_effort"] != nil || body["output_config"].(map[string]any)["effort"] != "low" || body["thinking"].(map[string]any)["type"] != "adaptive" {
						t.Error("Claude control was not translated to native Messages")
					}
				} else if body["reasoning_effort"] != "low" {
					t.Error("reasoning effort was dropped", provider)
				}
				if provider == "glm" && body["thinking"].(map[string]any)["clear_thinking"] != false {
					t.Error("GLM thinking history not retained")
				}
				fixtureReply(w, provider == "claude", body["stream"] == true)
			})
			for _, protocol := range []string{"chat", "responses", "anthropic", "gemini"} {
				for _, stream := range []bool{false, true} {
					body := payload(protocol, stream, "你好")
					switch protocol {
					case "chat":
						body["reasoning_effort"] = "low"
					case "responses":
						body["reasoning"] = map[string]any{"effort": "low", "summary": "none"}
					case "anthropic":
						body["thinking"] = map[string]any{"type": "adaptive"}
						body["output_config"] = map[string]any{"effort": "low"}
					case "gemini":
						body["generationConfig"] = map[string]any{"thinkingConfig": map[string]any{"thinkingLevel": "LOW", "includeThoughts": false}}
					}
					response := h.call(t, path(protocol, stream), body, nil)
					raw := read(t, response)
					if response.StatusCode != 200 || !strings.Contains(raw, "OK") {
						t.Fatal(provider, protocol, stream, response.StatusCode, response.Header, calls.Load(), raw)
					}
					if (protocol == "responses" || protocol == "gemini") && strings.Contains(raw, "fixture reasoning") {
						t.Fatal("hidden thinking leaked into response")
					}
					if stream {
						end := map[string]string{"chat": "[DONE]", "responses": "response.completed", "anthropic": "message_stop", "gemini": "STOP"}[protocol]
						if !strings.Contains(raw, end) {
							t.Fatal("native stream completion missing", raw)
						}
					}
				}
			}
			key, err := h.db.Auth(context.Background(), h.key, time.Now())
			if err != nil || key.AIUsed != 8 || key.TotalUsed != 8 || calls.Load() != 8 {
				t.Fatal("provider admission/accounting", key, err, calls.Load())
			}
		})
	}
}

func TestUnsupportedThinkingControlsAreUncharged(t *testing.T) {
	for _, name := range []string{"gemini", "kimi"} {
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int64
			h := providerHarness(t, name, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) })
			for _, protocol := range []string{"chat", "responses", "anthropic", "gemini"} {
				body := payload(protocol, false, "你好")
				switch protocol {
				case "chat":
					body["reasoning_effort"] = "none"
				case "responses":
					body["reasoning"] = map[string]any{"effort": "none"}
				case "anthropic":
					body["thinking"] = map[string]any{"type": "disabled"}
				case "gemini":
					body["generationConfig"] = map[string]any{"thinkingConfig": map[string]any{"thinkingBudget": 0}}
				}
				response := h.call(t, path(protocol, false), body, nil)
				read(t, response)
				if response.StatusCode != 400 {
					t.Fatal("unsupported disable not refused", response.StatusCode)
				}
			}
			key, _ := h.db.Auth(context.Background(), h.key, time.Now())
			if key.TotalUsed != 0 || key.AIUsed != 0 || calls.Load() != 0 {
				t.Fatal("unsupported control reached upstream or charged quota")
			}
		})
	}
}

func TestMultiRoundReasoningPreservationAndLocalFallback(t *testing.T) {
	for _, provider := range []string{"deepseek", "claude", "glm", "kimi"} {
		t.Run(provider, func(t *testing.T) {
			var calls atomic.Int64
			h := providerHarness(t, provider, func(w http.ResponseWriter, r *http.Request) {
				count := calls.Add(1)
				var body map[string]any
				json.NewDecoder(r.Body).Decode(&body)
				if count == 2 {
					messages := body["messages"].([]any)
					assistant := messages[1].(map[string]any)
					if provider == "claude" {
						blocks := assistant["content"].([]any)
						if blocks[0].(map[string]any)["signature"] != "fixture-signature" || blocks[0].(map[string]any)["thinking"] != "fixture reasoning" || blocks[1].(map[string]any)["data"] != "opaque-fixture" {
							t.Error("signed blocks changed across replay")
						}
					} else if assistant["reasoning_content"] != "fixture reasoning" {
						t.Error("full Chat reasoning history was dropped")
					}
				}
				fixtureReply(w, provider == "claude", false)
			})
			protocol := "chat"
			if provider == "claude" {
				protocol = "anthropic"
			}
			first := payload(protocol, false, "你好")
			raw := read(t, h.call(t, path(protocol, false), first, nil))
			var reply map[string]any
			if err := json.Unmarshal([]byte(raw), &reply); err != nil {
				t.Fatal(err)
			}
			var assistant any
			if protocol == "anthropic" {
				assistant = map[string]any{"role": "assistant", "content": reply["content"]}
			} else {
				assistant = reply["choices"].([]any)[0].(map[string]any)["message"]
			}
			messages := []any{map[string]any{"role": "user", "content": "你好"}, assistant, map[string]any{"role": "user", "content": "你好"}}
			second := payload(protocol, false, "你好")
			second["messages"] = messages
			response := h.call(t, path(protocol, false), second, nil)
			if body := read(t, response); response.StatusCode != 200 || !strings.Contains(body, "OK") {
				t.Fatal("reasoning replay failed", response.StatusCode, body)
			}
			messages[2] = map[string]any{"role": "user", "content": "请帮我设计一个图书管理系统"}
			second["max_tokens"] = 4096
			response = h.call(t, path(protocol, false), second, nil)
			if body := read(t, response); response.StatusCode != 200 || len(body) < 1000 {
				t.Fatal("local fallback after reasoning replay", response.StatusCode)
			}
			key, _ := h.db.Auth(context.Background(), h.key, time.Now())
			if calls.Load() != 2 || key.AIUsed != 2 || key.TotalUsed != 3 {
				t.Fatal("replayed reasoning changed latest-user matching or quota")
			}
		})
	}
}

func TestNativeThinkingCannotCrossToChatUpstream(t *testing.T) {
	r := &core.Request{Protocol: core.Anthropic, ToolChoice: "auto", Messages: []core.Message{{Role: "assistant", Parts: []core.Part{{Kind: "thinking", Text: "signed", Signature: "signature", Opaque: true}}}}}
	if err := upstream.Preflight(r, config.Target{}, config.Upstream{Kind: "openai_chat"}); err == nil {
		t.Fatal("signature silently converted")
	}
}
