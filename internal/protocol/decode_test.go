package protocol

import (
	"encoding/json"
	"fakeapi/internal/core"
	"strings"
	"testing"
)

func decode(t *testing.T, p core.Protocol, body string) *core.Request {
	t.Helper()
	r, e := Decode(p, strings.NewReader(body), "fakeapi-test", false)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func TestLatestUserExtraction(t *testing.T) {
	cases := []struct {
		p          core.Protocol
		body, text string
	}{{core.Chat, `{"model":"x","messages":[{"role":"system","content":"hello"},{"role":"user","content":"hello"},{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/image.png"}}]}]}`, ""}, {core.Responses, `{"model":"x","instructions":"hello","input":[{"role":"user","content":[{"type":"input_text","text":"one"},{"type":"input_text","text":"two"}]},{"type":"function_call_output","call_id":"a","output":"hello"}]}`, "one\ntwo"}, {core.Anthropic, `{"model":"x","max_tokens":100,"messages":[{"role":"user","content":"task"},{"role":"user","content":[{"type":"tool_result","tool_use_id":"a","content":"hello"}]}]}`, "task"}, {core.Gemini, `{"contents":[{"role":"user","parts":[{"text":"task"}]},{"role":"user","parts":[{"functionResponse":{"name":"f","response":{"text":"hello"}}}]}]}`, "task"}}
	for _, tc := range cases {
		r := decode(t, tc.p, tc.body)
		if r.Latest.Text != tc.text {
			t.Fatalf("%s got %q", tc.p, r.Latest.Text)
		}
	}
}
func TestResponsesReplay(t *testing.T) {
	r := &core.Request{Protocol: core.Responses, Model: "x"}
	v := core.Result{ID: "a", Text: "reply", Finish: "stop"}
	result := Body(r, v, 1)
	output := result["output"].([]any)
	input := append([]any{map[string]any{"role": "user", "content": "hello"}}, output...)
	input = append(input, map[string]any{"role": "user", "content": "new task"})
	b, _ := json.Marshal(map[string]any{"model": "x", "input": input})
	got := decode(t, core.Responses, string(b))
	if got.Latest.Text != "new task" || len(got.Messages) != 3 {
		t.Fatal("output history replay failed")
	}
}
func TestInvalidRequests(t *testing.T) {
	for _, body := range []string{`{}`, `{"model":"x","messages":[]}`, `{"model":"x","messages":[{"role":"user","content":"hello"}],"n":2}`, `{"model":"x","messages":[{"role":"user","content":"hello"}],"max_tokens":0}`, `{"model":"x","messages":[{"role":"user","content":"hello"}],"temperature":99}`, `{"model":"x","messages":[{"role":"user","content":"hello"}],"unknown":"canary"}`, `{"model":"x","messages":[{"role":"user","content":[{"type":"audio"}]}]}`, `{"model":"x","messages":[{"role":"user","content":"hello"}],"max_tokens":1,"max_completion_tokens":1}`} {
		if _, e := Decode(core.Chat, strings.NewReader(body), "", false); e == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}

func TestChatReasoningEffort(t *testing.T) {
	for _, effort := range []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"} {
		r := decode(t, core.Chat, `{"model":"x","messages":[{"role":"user","content":"你好"}],"reasoning_effort":"`+effort+`"}`)
		if r.Parameters["reasoning_effort"] != effort || r.ThinkingOff != (effort == "none") {
			t.Fatal("reasoning preference lost", effort, r.Parameters, r.ThinkingOff)
		}
	}
	for _, effort := range []string{`""`, `"unknown"`, `"low high"`, `true`, `1`, `{}`, `[]`} {
		body := `{"model":"x","messages":[{"role":"user","content":"你好"}],"reasoning_effort":` + effort + `}`
		if _, e := Decode(core.Chat, strings.NewReader(body), "", false); e == nil {
			t.Fatal("invalid reasoning effort accepted", effort)
		}
	}
	for _, suffix := range []string{"", `,"reasoning_effort":null`} {
		r := decode(t, core.Chat, `{"model":"x","messages":[{"role":"user","content":"你好"}]`+suffix+`}`)
		if _, ok := r.Parameters["reasoning_effort"]; ok || r.ThinkingOff {
			t.Fatal("provider default replaced")
		}
	}
}
