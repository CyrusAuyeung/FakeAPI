package upstream

import (
	"strings"
	"testing"

	"fakeapi/internal/config"
	"fakeapi/internal/core"
	"fakeapi/internal/protocol"
)

func TestReasoningSwitchDoesNotFollowDisplay(t *testing.T) {
	r, err := protocol.Decode(core.Gemini, strings.NewReader(`{"contents":[{"parts":[{"text":"你好"}]}],"generationConfig":{"thinkingConfig":{"includeThoughts":false}}}`), "x", false)
	if err != nil {
		t.Fatal(err)
	}
	body, err := Translate(r, config.Target{Model: config.StringValue{Literal: "real"}, Reasoning: config.Reasoning{Mode: "effort"}})
	if err != nil || body["reasoning_effort"] != nil {
		t.Fatal("hiding thoughts disabled generation", body, err)
	}
	r, err = protocol.Decode(core.Gemini, strings.NewReader(`{"contents":[{"parts":[{"text":"你好"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":0}}}`), "x", false)
	if err != nil {
		t.Fatal(err)
	}
	body, err = Translate(r, config.Target{Model: config.StringValue{Literal: "real"}})
	if err != nil || body["reasoning_effort"] != "none" {
		t.Fatal("zero budget did not disable generation", body, err)
	}
}

func TestExactBudgetRequiresSupportedTarget(t *testing.T) {
	r, err := protocol.Decode(core.Anthropic, strings.NewReader(`{"model":"x","max_tokens":4096,"messages":[{"role":"user","content":"你好"}],"thinking":{"type":"enabled","budget_tokens":1024}}`), "x", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := Preflight(r, config.Target{}, config.Upstream{}); err == nil {
		t.Fatal("numeric budget silently replaced with an effort")
	}
	body, err := Translate(r, config.Target{Model: config.StringValue{Literal: "real"}, Reasoning: config.Reasoning{Mode: "google", Budget: true}})
	if err != nil {
		t.Fatal(err)
	}
	thinking := body["extra_body"].(map[string]any)["google"].(map[string]any)["thinking_config"].(map[string]any)
	if thinking["thinking_budget"] != 1024 || body["reasoning_effort"] != nil {
		t.Fatal("exact budget lost or duplicate controls", body)
	}
}

func TestNativeToolSignatureSequenceTranslation(t *testing.T) {
	r := &core.Request{Protocol: core.Anthropic, ToolChoice: "auto", Messages: []core.Message{
		{Role: "assistant", Parts: []core.Part{{Kind: "thinking", Text: "unaltered", Signature: "signature", Opaque: true}, {Kind: "tool_call", ID: "call", Name: "f", Arguments: []byte(`{"x":1}`)}}},
		{Role: "tool", Parts: []core.Part{{Kind: "tool_result", ID: "call", Text: "result"}}},
		{Role: "user", Parts: []core.Part{{Kind: "text", Text: "你好"}}},
	}}
	body, err := Translate(r, config.Target{Model: config.StringValue{Literal: "real"}, Reasoning: config.Reasoning{Mode: "anthropic"}})
	if err != nil {
		t.Fatal(err)
	}
	messages := body["messages"].([]any)
	first := messages[0].(map[string]any)["content"].([]any)
	if first[0].(map[string]any)["signature"] != "signature" || first[1].(map[string]any)["id"] != "call" || messages[1].(map[string]any)["role"] != "user" {
		t.Fatal("native thinking/tool sequence changed")
	}
}
