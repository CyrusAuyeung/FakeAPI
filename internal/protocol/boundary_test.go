package protocol

import (
	"encoding/json"
	"strings"
	"testing"

	"fakeapi/internal/core"
)

func TestMalformedConstraintsAndBlocks(t *testing.T) {
	tests := []struct {
		protocol core.Protocol
		body     string
	}{
		{core.Chat, `{"model":"test","messages":[{"role":"user","content":"x","tool_calls":{}}]}`},
		{core.Chat, `{"model":"test","messages":[{"role":"user","content":"x","tool_calls":[{"type":"function","id":"c","function":{"name":"f","arguments":"{}"}}]}]}`},
		{core.Chat, `{"model":"test","messages":[{"role":"user","content":"x"}],"seed":0.5}`},
		{core.Chat, `{"model":"test","messages":[{"role":"user","content":"x"}],"response_format":{"type":"json_schema","json_schema":{"schema":{},"strict":"yes"}}}`},
		{core.Chat, `{"model":"test","messages":[{"role":"user","content":"x"}],"response_format":{"type":"json_object","extra":true}}`},
		{core.Chat, `{"model":"test","messages":[{"role":"user","content":"x"}],"tools":[{"type":"function","function":{"name":"f","parameters":[]}}]}`},
		{core.Responses, `{"model":"test","input":"x","text":true}`},
		{core.Responses, `{"model":"test","input":[{"type":"item_reference","id":"x"}]}`},
		{core.Responses, `{"model":"test","input":[{"role":"user","content":"x","other":"private-canary"}]}`},
		{core.Responses, `{"model":"test","input":[{"type":"function_call","name":"f","call_id":"one","arguments":"{}","parsed_arguments":{"changed":true}}]}`},
		{core.Responses, `{"model":"test","input":[{"role":"assistant","content":[{"type":"output_text","text":"{}","parsed":{"changed":true}}]}]}`},
		{core.Anthropic, `{"model":"test","max_tokens":100,"messages":[{"role":"user","content":"x"}],"tools":{}}`},
		{core.Anthropic, `{"model":"test","max_tokens":100,"messages":[{"role":"user","content":"x"}],"output_config":[]}`},
		{core.Anthropic, `{"model":"test","max_tokens":100,"messages":[{"role":"user","content":[{"type":"tool_use","id":"x","name":"f","input":{}}]}]}`},
		{core.Gemini, `{"contents":[{"parts":[{"text":"x","inlineData":{"mimeType":"image/png","data":"AQ=="}}]}]}`},
		{core.Gemini, `{"contents":[{"parts":[{"text":"x"}]}],"generationConfig":true}`},
		{core.Gemini, `{"contents":[{"parts":[{"text":"x"}]}],"generationConfig":{"responseSchema":[]}}`},
		{core.Gemini, `{"contents":[{"parts":[{"text":"x"}]}],"generationConfig":{"responseMimeType":"application/json","responseFormat":{"text":{"mimeType":"TEXT_PLAIN"}}}}`},
		{core.Gemini, `{"contents":[{"parts":[{"text":"x"}]}],"generationConfig":{"responseFormat":{"audio":{}}}}`},
		{core.Gemini, `{"contents":[{"parts":[{"text":"x"}]}],"tools":[{"functionDeclarations":{}}]}`},
		{core.Gemini, `{"contents":[{"role":"user","parts":[{"functionCall":{"name":"f","args":{}}}]}]}`},
	}
	for _, tc := range tests {
		if _, e := Decode(tc.protocol, strings.NewReader(tc.body), "test", false); e == nil || e.Status != 400 || strings.Contains(e.Message, "private-canary") {
			t.Fatal("malformed request accepted/leaked", tc.protocol, e)
		}
	}
}

func TestImagesAndStructuredFormats(t *testing.T) {
	for mime, kind := range map[string]string{"APPLICATION_JSON": "json_object", "TEXT_PLAIN": ""} {
		r := decode(t, core.Gemini, `{"contents":[{"parts":[{"text":"hello"}]}],"generationConfig":{"responseFormat":{"text":{"mimeType":"`+mime+`"}}}}`)
		if r.Constraint.Kind != kind {
			t.Fatal("Gemini responseFormat MIME normalization", mime, r.Constraint.Kind)
		}
	}
	chat := decode(t, core.Chat, `{"model":"test","messages":[{"role":"user","content":[{"type":"text","text":"hello"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AQ==","detail":"low"}}]}],"tools":[{"type":"function","function":{"name":"weather","parameters":{"type":"object"}}}],"tool_choice":{"type":"function","function":{"name":"weather"}},"response_format":{"type":"json_object"}}`)
	if !chat.Latest.HasImages || chat.Messages[0].Parts[1].Detail != "low" || chat.Constraint.Kind != "json_object" || chat.ToolChoice != "weather" {
		t.Fatal("Chat image/constraint normalization")
	}
	gemini := decode(t, core.Gemini, `{"systemInstruction":{"parts":[{"text":"instruction"}]},"contents":[{"role":"user","parts":[{"inlineData":{"mimeType":"image/png","data":"AQ=="}}]}],"generationConfig":{"responseMimeType":"application/json","responseSchema":{"type":"OBJECT","properties":{"ok":{"type":"BOOLEAN"},"source":{"type":"STRING","description":"origin"}},"required":["ok","source"],"propertyOrdering":["ok","source"]},"thinkingConfig":{"includeThoughts":false}}}`)
	if !gemini.Latest.HasImages || gemini.Latest.Text != "" || !gemini.ThinkingOff || gemini.Constraint.Schema["type"] != "object" {
		t.Fatal("Gemini JSON/image normalization")
	}
	anthropic := decode(t, core.Anthropic, `{"model":"test","max_tokens":100,"system":[{"type":"text","text":"instruction"}],"messages":[{"role":"user","content":[{"type":"image","source":{"type":"url","url":"https://example.com/image.png"}}]}],"output_config":{"format":{"type":"json_schema","schema":{"type":"object"}}},"thinking":{"type":"disabled"}}`)
	if !anthropic.Latest.HasImages || !anthropic.ThinkingOff || anthropic.Constraint.Kind != "json_schema" {
		t.Fatal("Anthropic JSON/image normalization")
	}
}

func FuzzDecode(f *testing.F) {
	for _, seed := range []string{`{}`, `{"model":"test","input":"hello"}`, `{"contents":[{"parts":[{"text":"hello"}]}]}`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, body string) {
		if len(body) > 16384 {
			t.Skip()
		}
		for _, p := range []core.Protocol{core.Chat, core.Responses, core.Anthropic, core.Gemini} {
			r, e := Decode(p, strings.NewReader(body), "test", false)
			if e == nil {
				if r == nil || r.Model == "" {
					t.Fatal("invalid successful normalization")
				}
				if _, e := json.Marshal(r.Messages); e != nil {
					t.Fatal(e)
				}
			}
		}
	})
}
