package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"fakeapi/internal/config"
	"fakeapi/internal/requestlog"
	"fakeapi/internal/store"
)

type harness struct {
	s           *Server
	db          *store.Store
	key         string
	keyID       string
	log, errlog *bytes.Buffer
	url         string
}

func setup(t *testing.T, modify func(*config.Config)) *harness {
	t.Helper()
	c, e := config.Load("../../examples/testing/fakeapi.yaml", true)
	if e != nil {
		t.Fatal(e)
	}
	c.Storage.Path = filepath.Join(t.TempDir(), "state.sqlite")
	if modify != nil {
		modify(c)
	}
	db, e := store.Open(c.Storage.Path, time.Second)
	if e != nil {
		t.Fatal(e)
	}
	k, key, e := db.Create(context.Background(), c.KeyPolicies["test"], "test", "")
	if e != nil {
		t.Fatal(e)
	}
	log, errlog := new(bytes.Buffer), new(bytes.Buffer)
	logger, e := requestlog.New(c.RequestLog, log, errlog)
	if e != nil {
		t.Fatal(e)
	}
	s := New(c, db, logger)
	ts := httptest.NewServer(s)
	t.Cleanup(func() { s.Close(); ts.Close(); logger.Close(); db.Close() })
	return &harness{s: s, db: db, key: key, keyID: k.ID, log: log, errlog: errlog, url: ts.URL}
}
func (h *harness) call(t *testing.T, path string, body any, extra map[string]string) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	req, e := http.NewRequest("POST", h.url+path, bytes.NewReader(b))
	if e != nil {
		t.Fatal(e)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.key)
	req.Header.Set("anthropic-version", "2023-06-01")
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	return resp
}
func payload(p string, stream bool, text string) map[string]any {
	switch p {
	case "responses":
		return map[string]any{"model": "fakeapi-test", "input": text, "stream": stream, "store": false}
	case "anthropic":
		return map[string]any{"model": "fakeapi-test", "max_tokens": 8192, "messages": []any{map[string]any{"role": "user", "content": text}}, "stream": stream}
	case "gemini":
		return map[string]any{"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": text}}}}}
	default:
		return map[string]any{"model": "fakeapi-test", "messages": []any{map[string]any{"role": "user", "content": text}}, "stream": stream, "stream_options": map[string]any{"include_usage": true}}
	}
}
func path(p string, stream bool) string {
	switch p {
	case "responses":
		return "/v1/responses"
	case "anthropic":
		return "/anthropic/v1/messages"
	case "gemini":
		if stream {
			return "/v1beta/models/fakeapi-test:streamGenerateContent?alt=sse"
		}
		return "/v1beta/models/fakeapi-test:generateContent"
	default:
		return "/v1/chat/completions"
	}
}
func read(t *testing.T, r *http.Response) string {
	t.Helper()
	defer r.Body.Close()
	b, e := io.ReadAll(r.Body)
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}
func TestFourProtocols(t *testing.T) {
	h := setup(t, nil)
	for _, p := range []string{"chat", "responses", "anthropic", "gemini"} {
		for _, stream := range []bool{false, true} {
			t.Run(p+map[bool]string{true: "-stream", false: "-json"}[stream], func(t *testing.T) {
				r := h.call(t, path(p, stream), payload(p, stream, "只回答 OK"), nil)
				b := read(t, r)
				if r.StatusCode != 200 {
					t.Fatalf("%d %s", r.StatusCode, b)
				}
				if !strings.Contains(b, "OK") {
					t.Fatal("text missing")
				}
				if stream {
					switch p {
					case "chat":
						if !strings.HasSuffix(b, "data: [DONE]\n\n") {
							t.Fatal("missing done")
						}
					case "responses":
						if !strings.Contains(b, "event: response.completed") || strings.Contains(b, "[DONE]") {
							t.Fatal("responses termination")
						}
					case "anthropic":
						if !strings.Contains(b, "event: message_stop") || strings.Contains(b, "[DONE]") {
							t.Fatal("anthropic termination")
						}
					case "gemini":
						for _, line := range strings.Split(b, "\n") {
							if line != "" && !strings.HasPrefix(line, "data: ") {
								t.Fatalf("invalid gemini SSE line %s", line)
							}
						}
						if !strings.Contains(b, `"finishReason":"STOP"`) {
							t.Fatal("missing STOP")
						}
					}
				} else if !json.Valid([]byte(b)) {
					t.Fatal("invalid JSON")
				}
			})
		}
	}
	k, _ := h.db.Get(context.Background(), h.keyID)
	if k.TotalUsed != 8 {
		t.Fatal(k.TotalUsed)
	}
	for _, line := range strings.Split(strings.TrimSpace(h.log.String()), "\n") {
		var m map[string]any
		_ = json.Unmarshal([]byte(line), &m)
		if len(m) != 4 || m["rule"] != "preset-ok" {
			t.Fatalf("bad log %s", line)
		}
	}
}
func TestConstraintAndQuotaPreflight(t *testing.T) {
	h := setup(t, nil)
	p := payload("chat", false, "a task")
	p["response_format"] = map[string]any{"type": "json_object"}
	resp := h.call(t, path("chat", false), p, nil)
	b := read(t, resp)
	if resp.StatusCode != 400 || !strings.Contains(b, "unsupported_constraint") {
		t.Fatal(resp.StatusCode, b)
	}
	k, _ := h.db.Get(context.Background(), h.keyID)
	if k.TotalUsed != 0 {
		t.Fatal("preflight consumed quota")
	}
	p = payload("chat", false, "/test/json")
	p["response_format"] = map[string]any{"type": "json_object"}
	resp = h.call(t, path("chat", false), p, nil)
	b = read(t, resp)
	if resp.StatusCode != 200 || !strings.Contains(b, `\"ok\":true`) {
		t.Fatal(resp.StatusCode, b)
	}
}
func TestHybridRoutingAndPrivacy(t *testing.T) {
	var count atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		if r.Header.Get("Authorization") != "Bearer upstream-test-key" {
			t.Error("wrong upstream key")
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "actual-model" {
			t.Error("wrong target")
		}
		messages := body["messages"].([]any)
		if len(messages) != 3 {
			t.Errorf("history lost: %d", len(messages))
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "upstream reply"}, "finish_reason": "stop"}}, "usage": map[string]any{"prompt_tokens": 7, "completion_tokens": 3}})
	}))
	defer up.Close()
	t.Setenv("UPSTREAM_TEST_KEY", "upstream-test-key")
	h := setup(t, func(c *config.Config) {
		c.KeyPolicies["test"] = config.Policy{Tags: []string{"ai", "test"}, Group: "testing", AILimit: ptr(int64(10)), MaxConcurrent: 16}
		c.Upstreams = map[string]config.Upstream{"test": {Kind: "openai_chat", BaseURL: config.StringValue{Literal: up.URL + "/v1"}, APIKeyEnv: "UPSTREAM_TEST_KEY", MaxConcurrent: 4, Timeouts: config.Timeouts{Connect: config.Duration(time.Second), FirstEvent: config.Duration(time.Second), Idle: config.Duration(time.Second), Total: config.Duration(5 * time.Second)}}}
		c.Targets = map[string]config.Target{"test": {Upstream: "test", Model: config.StringValue{Literal: "actual-model"}, MaxTokensParameter: "max_tokens", Capabilities: config.Capabilities{DeveloperRole: true}}}
		c.DefaultTarget = "test"
		for i := range c.Rules {
			if c.Rules[i].ID == "ai-greeting" {
				c.Rules[i].Enabled = ptr(true)
			}
		}
	})
	p := payload("chat", false, "你好")
	p["messages"] = []any{map[string]any{"role": "system", "content": "system-private-canary"}, map[string]any{"role": "user", "content": "history-private-canary"}, map[string]any{"role": "user", "content": "你好"}}
	resp := h.call(t, path("chat", false), p, nil)
	b := read(t, resp)
	if resp.StatusCode != 200 || !strings.Contains(b, "upstream reply") {
		t.Fatal(resp.StatusCode, b)
	}
	resp = h.call(t, path("chat", false), payload("chat", false, "你好，请分析代码-private-canary"), nil)
	if resp.StatusCode != 200 {
		t.Fatal(read(t, resp))
	}
	read(t, resp)
	if count.Load() != 1 {
		t.Fatal("unexpected real AI calls")
	}
	k, _ := h.db.Get(context.Background(), h.keyID)
	if k.AIUsed != 1 || k.TotalUsed != 2 {
		t.Fatal(k)
	}
	if strings.Contains(h.log.String()+h.errlog.String(), "private-canary") || strings.Contains(h.log.String(), h.key) {
		t.Fatal("private log leak")
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		b, _ := os.ReadFile(h.db.Path + suffix)
		if bytes.Contains(b, []byte("private-canary")) || bytes.Contains(b, []byte(h.key)) {
			t.Fatal("private database leak")
		}
	}
}
func TestToolFixtureRoundTrip(t *testing.T) {
	h := setup(t, nil)
	p := payload("chat", true, "/test/tool")
	p["tools"] = []any{map[string]any{"type": "function", "function": map[string]any{"name": "get_weather", "parameters": map[string]any{"type": "object", "properties": map[string]any{"city": map[string]any{"type": "string"}}, "required": []string{"city"}}}}}
	p["tool_choice"] = "required"
	resp := h.call(t, path("chat", true), p, nil)
	b := read(t, resp)
	if resp.StatusCode != 200 || !strings.Contains(b, "tool_calls") {
		t.Fatal(resp.StatusCode, b)
	}
	p["stream"] = false
	resp = h.call(t, path("chat", false), p, nil)
	var v map[string]any
	if e := json.Unmarshal([]byte(read(t, resp)), &v); e != nil {
		t.Fatal(e)
	}
	choice := v["choices"].([]any)[0].(map[string]any)
	assistant := choice["message"].(map[string]any)
	call := assistant["tool_calls"].([]any)[0].(map[string]any)
	p["messages"] = []any{map[string]any{"role": "user", "content": "/test/tool"}, assistant, map[string]any{"role": "tool", "tool_call_id": call["id"], "content": "{\"temperature\":25}"}}
	p["tool_choice"] = "none"
	resp = h.call(t, path("chat", false), p, nil)
	b = read(t, resp)
	if resp.StatusCode != 200 || !strings.Contains(b, "工具测试完成。") {
		t.Fatal(resp.StatusCode, b)
	}
	p["tool_choice"] = "required"
	resp = h.call(t, path("chat", false), p, nil)
	if resp.StatusCode != 400 {
		t.Fatal(read(t, resp))
	}
	read(t, resp)
}
func TestCancellationReleasesCapacity(t *testing.T) {
	h := setup(t, func(c *config.Config) {
		p := c.Profiles["instant"]
		p.Timing.FirstOutputDelay = config.Duration(5 * time.Second)
		p.Timing.MaxTotal = config.Duration(10 * time.Second)
		c.Profiles["instant"] = p
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p, _ := json.Marshal(payload("chat", true, "只回答 OK"))
	req, _ := http.NewRequestWithContext(ctx, "POST", h.url+path("chat", true), bytes.NewReader(p))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.key)
	done := make(chan struct{})
	go func() {
		r, _ := http.DefaultClient.Do(req)
		if r != nil {
			r.Body.Close()
		}
		close(done)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for h.s.Active() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if h.s.Active() != 1 {
		t.Fatal("request not admitted")
	}
	// Acquiring capacity precedes the transaction. Wait for its durable commit
	// before exercising the no-refund rule; cancellation before it costs zero.
	for {
		k, e := h.db.Get(context.Background(), h.keyID)
		if e != nil {
			t.Fatal(e)
		}
		if k.TotalUsed == 1 {
			break
		}
		if !time.Now().Before(deadline) {
			t.Fatal("request did not commit admission")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	for h.s.Active() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if h.s.Active() != 0 {
		t.Fatal("capacity leaked")
	}
	k, _ := h.db.Get(context.Background(), h.keyID)
	if k.TotalUsed != 1 {
		t.Fatal("cancelled admitted request refunded")
	}
}
