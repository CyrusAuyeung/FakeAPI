package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"fakeapi/internal/config"
)

func TestFallbackOnlyBeforeCommit(t *testing.T) {
	for _, protocol := range []string{"chat", "responses", "anthropic", "gemini"} {
		for _, mode := range []string{"before", "after", "auth"} {
			t.Run(protocol+"/"+mode, func(t *testing.T) {
				up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if mode == "auth" {
						w.WriteHeader(401)
						fmt.Fprint(w, "private-upstream-canary")
						return
					}
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n")
					w.(http.Flusher).Flush()
					if mode == "after" {
						fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"},\"finish_reason\":null}]}\n\n")
						w.(http.Flusher).Flush()
					}
					fmt.Fprint(w, "data: {\"error\":{\"message\":\"private-upstream-canary\"}}\n\n")
				}))
				defer up.Close()
				text := "safe fallback"
				h := setup(t, func(c *config.Config) {
					c.KeyPolicies["test"] = config.Policy{Tags: []string{"ai"}, MaxConcurrent: 16}
					c.Upstreams = map[string]config.Upstream{"test": {BaseURL: config.StringValue{Literal: up.URL + "/v1"}, Timeouts: config.Timeouts{Connect: config.Duration(time.Second), FirstEvent: config.Duration(time.Second), Idle: config.Duration(time.Second), Total: config.Duration(2 * time.Second)}, MaxConcurrent: 4}}
					c.Targets = map[string]config.Target{"test": {Upstream: "test", Model: config.StringValue{Literal: "real"}, MaxTokensParameter: "max_tokens"}}
					c.DefaultTarget = "test"
					for i := range c.Rules {
						if c.Rules[i].ID == "ai-greeting" {
							c.Rules[i].Enabled = ptr(true)
							c.Rules[i].Action.OnError = &config.OnError{Mode: "preset", Response: &config.Action{Type: "preset", Text: &text, StrictText: true}}
						}
					}
				})
				resp := h.call(t, path(protocol, true), payload(protocol, true, "hello"), nil)
				b := read(t, resp)
				terminal := map[string]string{"chat": "[DONE]", "responses": "response.completed", "anthropic": "message_stop", "gemini": `"finishReason":"STOP"`}[protocol]
				if resp.StatusCode != 200 {
					t.Fatal(resp.StatusCode, b)
				}
				if mode == "after" {
					if !strings.Contains(b, "partial") || strings.Contains(b, "safe fallback") || strings.Contains(b, terminal) || !strings.Contains(b, "error") {
						t.Fatal(b)
					}
				} else if !strings.Contains(b, "safe fallback") || !strings.Contains(b, terminal) {
					t.Fatal(resp.StatusCode, b)
				}
				if strings.Contains(b+h.log.String(), "private-upstream-canary") {
					t.Fatal("error leak")
				}
				k, _ := h.db.Get(context.Background(), h.keyID)
				if k.TotalUsed != 1 || k.AIUsed != 1 {
					t.Fatal("fallback double charged", k)
				}
			})
		}
	}
}
