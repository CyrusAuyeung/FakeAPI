package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"fakeapi/internal/config"
)

func TestHTTPPreflightLimitsDoNotCharge(t *testing.T) {
	for _, protocol := range []string{"chat", "responses", "anthropic", "gemini"} {
		t.Run(protocol+"/oversize", func(t *testing.T) {
			h := setup(t, func(c *config.Config) { c.Server.MaxRequestBytes = 1024 })
			resp := h.call(t, path(protocol, false), payload(protocol, false, strings.Repeat("private-limit-canary", 100)), nil)
			body := read(t, resp)
			k, e := h.db.Get(context.Background(), h.keyID)
			if resp.StatusCode != 413 || strings.Contains(body+h.log.String(), "private-limit-canary") || e != nil || k.TotalUsed != 0 || h.s.Active() != 0 {
				t.Fatal("oversize request charged or leaked", protocol, resp.StatusCode, e)
			}
		})
	}
	for _, scenario := range []string{"method", "content-type", "invalid-json", "model", "messages", "output-limit", "seed"} {
		t.Run(scenario, func(t *testing.T) {
			h := setup(t, func(c *config.Config) { c.Server.MaxMessages = 2 })
			p := payload("chat", false, "只回答 OK")
			status := 400
			switch scenario {
			case "method":
				status = 405
			case "model":
				p["model"] = "unknown"
				status = 404
			case "messages":
				p["messages"] = []any{map[string]any{"role": "user", "content": "1"}, map[string]any{"role": "assistant", "content": "2"}, map[string]any{"role": "user", "content": "3"}}
			case "output-limit":
				p["max_tokens"] = 8193
			}
			body, _ := json.Marshal(p)
			if scenario == "invalid-json" {
				body = []byte(`{"private-json-canary":`)
			}
			req := httptest.NewRequest("POST", path("chat", false), bytes.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+h.key)
			req.Header.Set("Content-Type", "application/json")
			if scenario == "method" {
				req.Method = "GET"
			}
			if scenario == "content-type" {
				req.Header.Set("Content-Type", "text/plain")
			}
			if scenario == "seed" {
				req.Header.Set("X-FakeAPI-Seed", "not-an-integer")
			}
			writer := httptest.NewRecorder()
			h.s.ServeHTTP(writer, req)
			k, e := h.db.Get(context.Background(), h.keyID)
			if writer.Code != status || !json.Valid(writer.Body.Bytes()) || e != nil || k.TotalUsed != 0 || h.s.Active() != 0 {
				t.Fatal("preflight refusal charged", scenario, writer.Code, e)
			}
			if strings.Contains(writer.Body.String()+h.log.String(), h.key) || strings.Contains(writer.Body.String()+h.log.String(), "private-json-canary") {
				t.Fatal("preflight error disclosed private input")
			}
		})
	}
}

func TestModelPaginationAndChangedCatalog(t *testing.T) {
	h := setup(t, nil)
	get := func(h *harness, endpoint string, status int) map[string]any {
		t.Helper()
		req, _ := http.NewRequest("GET", h.url+endpoint, nil)
		req.Header.Set("Authorization", "Bearer "+h.key)
		req.Header.Set("anthropic-version", "2023-06-01")
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		var value map[string]any
		if e = json.Unmarshal([]byte(read(t, resp)), &value); e != nil || resp.StatusCode != status {
			t.Fatal(endpoint, resp.StatusCode, e)
		}
		return value
	}
	first := get(h, "/anthropic/v1/models?limit=1", 200)
	if first["has_more"] != true || first["first_id"] != "fakeapi-frog" {
		t.Fatal("unstable model order", first)
	}
	last := get(h, "/anthropic/v1/models?limit=1&after_id=fakeapi-frog", 200)
	if last["has_more"] != false || last["last_id"] != "fakeapi-test" {
		t.Fatal("incorrect last page", last)
	}
	if len(get(h, "/anthropic/v1/models?before_id=fakeapi-frog", 200)["data"].([]any)) != 0 {
		t.Fatal("before-first cursor should be empty")
	}
	for _, query := range []string{"limit=0", "limit=1001", "limit=abc", "after_id=missing", "after_id=fakeapi-frog&before_id=fakeapi-test"} {
		get(h, "/anthropic/v1/models?"+query, 400)
	}
	page := get(h, "/v1beta/models?pageSize=1", 200)
	token, ok := page["nextPageToken"].(string)
	if !ok || len(get(h, "/v1beta/models?pageSize=1&pageToken="+token, 200)["models"].([]any)) != 1 {
		t.Fatal("missing Gemini second page")
	}
	for _, query := range []string{"pageSize=0", "pageSize=1001", "pageSize=abc", "pageToken=invalid"} {
		get(h, "/v1beta/models?"+query, 400)
	}
	changed := setup(t, func(c *config.Config) { c.Models[0].DisplayName = "changed catalog" })
	get(changed, "/v1beta/models?pageToken="+token, 400)
	for _, db := range []*harness{h, changed} {
		k, e := db.db.Get(context.Background(), db.keyID)
		if e != nil || k.TotalUsed != 0 {
			t.Fatal("catalog request consumed generation quota", e)
		}
	}
}

func TestStoppingReadinessAndAdmission(t *testing.T) {
	h := setup(t, nil)
	h.s.StopAdmission()
	for endpoint, status := range map[string]int{"/healthz": 200, "/readyz": 503} {
		writer := httptest.NewRecorder()
		h.s.ServeHTTP(writer, httptest.NewRequest("GET", endpoint, nil))
		if writer.Code != status {
			t.Fatal("incorrect shutdown readiness", endpoint, writer.Code)
		}
	}
	resp := h.call(t, path("chat", false), payload("chat", false, "只回答 OK"), nil)
	if resp.StatusCode != 503 || !strings.Contains(read(t, resp), "service_stopping") {
		t.Fatal("new generation admitted while stopping")
	}
	k, e := h.db.Get(context.Background(), h.keyID)
	if e != nil || k.TotalUsed != 0 || h.s.Active() != 0 {
		t.Fatal("shutdown refusal consumed quota", e)
	}
}

func TestCloseCancelsAdmittedRequestWithoutRefund(t *testing.T) {
	h := setup(t, func(c *config.Config) {
		p := c.Profiles["instant"]
		p.Timing.FirstOutputDelay = config.Duration(5 * time.Second)
		p.Timing.MaxTotal = config.Duration(10 * time.Second)
		c.Profiles["instant"] = p
	})
	data, _ := json.Marshal(payload("chat", true, "只回答 OK"))
	req, _ := http.NewRequest("POST", h.url+path("chat", true), bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+h.key)
	req.Header.Set("Content-Type", "application/json")
	type answer struct {
		status int
		body   string
		err    error
	}
	done := make(chan answer, 1)
	go func() {
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			done <- answer{err: e}
			return
		}
		defer resp.Body.Close()
		body, e := io.ReadAll(resp.Body)
		done <- answer{status: resp.StatusCode, body: string(body), err: e}
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		k, e := h.db.Get(context.Background(), h.keyID)
		if e != nil {
			t.Fatal(e)
		}
		if k.TotalUsed == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("request admission did not commit")
		}
		time.Sleep(time.Millisecond)
	}
	h.s.Close()
	select {
	case got := <-done:
		if got.err != nil || got.status != 504 || strings.Contains(got.body, "[DONE]") {
			t.Fatal("shutdown cancellation reported success", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not interrupt pending output")
	}
	for h.s.Active() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	k, e := h.db.Get(context.Background(), h.keyID)
	if e != nil || k.TotalUsed != 1 || h.s.Active() != 0 {
		t.Fatal("shutdown refunded or leaked capacity", e)
	}
}
