package protocol

import (
	"encoding/json"
	"strings"
	"testing"

	"fakeapi/internal/core"
)

func TestReasoningControlsAndDisplay(t *testing.T) {
	cases := []struct {
		protocol     core.Protocol
		body, effort string
		enabled      *bool
		budget       int
		off          bool
	}{
		{core.Chat, `{"model":"x","messages":[{"role":"user","content":"你好"}],"reasoning_effort":"none"}`, "none", nil, 0, true},
		{core.Responses, `{"model":"x","input":"你好","reasoning":{"effort":"low","summary":"none"}}`, "low", nil, 0, true},
		{core.Anthropic, `{"model":"x","max_tokens":2048,"messages":[{"role":"user","content":"你好"}],"thinking":{"type":"disabled"}}`, "", boolValue(false), 0, true},
		{core.Anthropic, `{"model":"x","max_tokens":2048,"messages":[{"role":"user","content":"你好"}],"thinking":{"type":"enabled","budget_tokens":1024,"display":"omitted"}}`, "", boolValue(true), 1024, true},
		{core.Gemini, `{"contents":[{"parts":[{"text":"你好"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":0}}}`, "", boolValue(false), 0, true},
		{core.Gemini, `{"contents":[{"parts":[{"text":"你好"}]}],"generationConfig":{"thinkingConfig":{"includeThoughts":false,"thinkingLevel":"LOW"}}}`, "low", nil, 0, true},
		{core.Gemini, `{"contents":[{"parts":[{"text":"你好"}]}],"generationConfig":{"thinkingConfig":{"includeThoughts":false}}}`, "", nil, 0, true},
	}
	for _, tc := range cases {
		r := decode(t, tc.protocol, tc.body)
		if r.Reasoning.Effort != tc.effort || r.ThinkingOff != tc.off || (r.Reasoning.Enabled == nil) != (tc.enabled == nil) {
			t.Fatalf("%s: controls changed %+v", tc.protocol, r.Reasoning)
		}
		if tc.enabled != nil && *r.Reasoning.Enabled != *tc.enabled {
			t.Fatal("display choice changed generation")
		}
		if tc.budget != 0 && (r.Reasoning.Budget == nil || *r.Reasoning.Budget != tc.budget) {
			t.Fatal("thinking budget lost")
		}
	}
}

func boolValue(value bool) *bool { return &value }

func TestReasoningReplayFromOwnResponses(t *testing.T) {
	for _, protocol := range []core.Protocol{core.Chat, core.Responses, core.Gemini} {
		r := &core.Request{Protocol: protocol, Model: "x"}
		value := Body(r, core.Result{ID: "round1", Text: "reply", Reasoning: "displayed reasoning", Finish: "stop"}, 1)
		var input map[string]any
		switch protocol {
		case core.Chat:
			input = map[string]any{"model": "x", "messages": []any{value["choices"].([]any)[0].(M)["message"], M{"role": "user", "content": "你好"}}}
		case core.Responses:
			input = map[string]any{"model": "x", "input": append(value["output"].([]any), M{"role": "user", "content": "你好"})}
		case core.Gemini:
			input = map[string]any{"contents": []any{value["candidates"].([]any)[0].(M)["content"], M{"role": "user", "parts": []any{M{"text": "你好"}}}}}
		}
		body, _ := json.Marshal(input)
		got := decode(t, protocol, string(body))
		if got.Latest.Text != "你好" {
			t.Fatal("reasoning affected routing")
		}
	}
}

func TestInvalidReasoningControls(t *testing.T) {
	cases := []struct {
		protocol core.Protocol
		body     string
	}{
		{core.Chat, `{"model":"x","messages":[{"role":"user","content":"你好","reasoning_content":"hidden"}]}`},
		{core.Responses, `{"model":"x","input":"你好","reasoning":false}`},
		{core.Responses, `{"model":"x","input":"你好","reasoning":{"effort":"invalid"}}`},
		{core.Responses, `{"model":"x","input":"你好","reasoning":{"summary":false}}`},
		{core.Responses, `{"model":"x","input":[{"type":"reasoning","summary":[{"type":"summary_text","text":1}]}]}`},
		{core.Anthropic, `{"model":"x","max_tokens":2048,"messages":[{"role":"user","content":"你好"}],"thinking":{"type":"enabled","budget_tokens":2048}}`},
		{core.Anthropic, `{"model":"x","max_tokens":2048,"messages":[{"role":"user","content":"你好"}],"thinking":{"type":"adaptive","budget_tokens":1024}}`},
		{core.Anthropic, `{"model":"x","max_tokens":2048,"messages":[{"role":"user","content":"你好"}],"thinking":{"type":"disabled"},"output_config":{"effort":"high"}}`},
		{core.Gemini, `{"contents":[{"parts":[{"text":"你好"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":0.5}}}`},
		{core.Gemini, `{"contents":[{"parts":[{"text":"你好"}]}],"generationConfig":{"thinkingConfig":{"thinkingLevel":"invalid"}}}`},
		{core.Gemini, `{"contents":[{"parts":[{"text":"你好"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":0,"thinkingLevel":"LOW"}}}`},
	}
	for _, tc := range cases {
		if _, err := Decode(tc.protocol, strings.NewReader(tc.body), "x", false); err == nil {
			t.Fatal("invalid reasoning control accepted", tc.protocol)
		}
	}
}
