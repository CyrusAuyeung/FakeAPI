package protocol

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/url"
	"reflect"
	"strconv"
	"strings"

	"fakeapi/internal/core"
	"fakeapi/internal/schema"
)

func obj(v any) map[string]any { m, _ := v.(map[string]any); return m }
func str(v any) string         { s, _ := v.(string); return s }
func arr(v any) []any          { a, _ := v.([]any); return a }
func number(v any) (float64, bool) {
	switch n := v.(type) {
	case json.Number:
		f, e := n.Float64()
		return f, e == nil
	case float64:
		return n, true
	}
	return 0, false
}
func fields(m map[string]any, allowed, path string) *core.Error {
	for k := range m {
		if !strings.Contains(" "+allowed+" ", " "+k+" ") {
			return core.Invalid(path + k)
		}
	}
	return nil
}
func checkBool(m map[string]any, k string) *core.Error {
	if v, ok := m[k]; ok && v != nil {
		if _, ok := v.(bool); !ok {
			return core.Invalid(k)
		}
	}
	return nil
}
func scalarBool(v any) bool { b, _ := v.(bool); return b }
func objectField(v any, path string) (map[string]any, *core.Error) {
	if v == nil {
		return nil, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, core.Invalid(path)
	}
	return m, nil
}
func arrayField(v any, path string) ([]any, *core.Error) {
	if v == nil {
		return nil, nil
	}
	a, ok := v.([]any)
	if !ok {
		return nil, core.Invalid(path)
	}
	return a, nil
}
func stringList(v any) ([]string, *core.Error) {
	if s, ok := v.(string); ok {
		if s == "" {
			return nil, core.Invalid("stop")
		}
		return []string{s}, nil
	}
	if v == nil {
		return nil, nil
	}
	a, ok := v.([]any)
	if !ok || len(a) > 4 {
		return nil, core.Invalid("stop")
	}
	out := []string{}
	for _, x := range a {
		s, ok := x.(string)
		if !ok || s == "" {
			return nil, core.Invalid("stop")
		}
		out = append(out, s)
	}
	return out, nil
}
func imageURL(s string) (core.Part, *core.Error) {
	p := core.Part{Kind: "image", URL: s}
	if strings.HasPrefix(s, "data:") {
		i := strings.Index(s, ";base64,")
		if i < 0 {
			return p, core.Invalid("image.data")
		}
		mime := s[5:i]
		b, e := base64.StdEncoding.DecodeString(s[i+8:])
		if e != nil || len(b) == 0 || !strings.HasPrefix(mime, "image/") {
			return p, core.Invalid("image.data")
		}
		p.URL = ""
		p.MIME = mime
		p.Data = s[i+8:]
		return p, nil
	}
	u, e := url.Parse(s)
	if e != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return p, core.Invalid("image.url")
	}
	return p, nil
}
func textParts(v any, mode string) ([]core.Part, *core.Error) {
	if s, ok := v.(string); ok {
		return []core.Part{{Kind: "text", Text: s}}, nil
	}
	a, ok := v.([]any)
	if !ok {
		return nil, core.Invalid("content")
	}
	out := []core.Part{}
	for _, x := range a {
		b := obj(x)
		if b == nil {
			return nil, core.Invalid("content.block")
		}
		typ := str(b["type"])
		if mode == "chat" && typ != "text" && typ != "image_url" || mode == "responses" && typ != "text" && typ != "input_text" && typ != "output_text" && typ != "input_image" || mode == "anthropic" && typ != "text" && typ != "image" && typ != "tool_use" && typ != "tool_result" && typ != "thinking" && typ != "redacted_thinking" {
			return nil, core.Invalid("content.type")
		}
		switch typ {
		case "text", "input_text", "output_text":
			allowed := "type text"
			if typ == "output_text" && mode == "responses" {
				allowed += " annotations logprobs parsed"
			}
			if e := fields(b, allowed, "content."); e != nil {
				return nil, e
			}
			s, ok := b["text"].(string)
			if !ok {
				return nil, core.Invalid("content.text")
			}
			if !parsedMatches(s, b["parsed"]) {
				return nil, core.Invalid("content.parsed")
			}
			out = append(out, core.Part{Kind: "text", Text: s})
		case "image_url", "input_image":
			allowed := "type image_url file_id detail"
			if typ == "image_url" {
				allowed = "type image_url"
			}
			if e := fields(b, allowed, "image."); e != nil {
				return nil, e
			}
			s := str(b["image_url"])
			detail := str(b["detail"])
			if typ == "image_url" {
				m, e := objectField(b["image_url"], "image_url")
				if e != nil {
					return nil, e
				}
				if e = fields(m, "url detail", "image_url."); e != nil {
					return nil, e
				}
				s, detail = str(m["url"]), str(m["detail"])
			}
			if detail != "" && detail != "auto" && detail != "low" && detail != "high" {
				return nil, core.Invalid("image.detail")
			}
			if s == "" && str(b["file_id"]) != "" {
				out = append(out, core.Part{Kind: "image", URL: "file_id:" + str(b["file_id"]), Opaque: true})
				continue
			}
			p, e := imageURL(s)
			if e != nil {
				return nil, e
			}
			p.Detail = detail
			out = append(out, p)
		case "image":
			if e := fields(b, "type source", "image."); e != nil {
				return nil, e
			}
			src := obj(b["source"])
			if e := fields(src, "type media_type data url", "image.source."); e != nil {
				return nil, e
			}
			if str(src["type"]) == "base64" {
				p, e := imageURL("data:" + str(src["media_type"]) + ";base64," + str(src["data"]))
				if e != nil {
					return nil, e
				}
				out = append(out, p)
			} else if str(src["type"]) == "url" {
				p, e := imageURL(str(src["url"]))
				if e != nil {
					return nil, e
				}
				out = append(out, p)
			} else {
				return nil, core.Invalid("image.source")
			}
		case "tool_use":
			if e := fields(b, "type id name input", "tool_use."); e != nil {
				return nil, e
			}
			args, ok := b["input"].(map[string]any)
			if !ok || str(b["id"]) == "" || str(b["name"]) == "" {
				return nil, core.Invalid("tool_use")
			}
			j, _ := json.Marshal(args)
			out = append(out, core.Part{Kind: "tool_call", ID: str(b["id"]), Name: str(b["name"]), Arguments: j})
		case "tool_result":
			if e := fields(b, "type tool_use_id content is_error", "tool_result."); e != nil {
				return nil, e
			}
			if e := checkBool(b, "is_error"); e != nil {
				return nil, e
			}
			if str(b["tool_use_id"]) == "" {
				return nil, core.Invalid("tool_result.id")
			}
			parts, e := textParts(b["content"], mode)
			if e != nil {
				return nil, e
			}
			var texts []string
			opaque := false
			for _, p := range parts {
				if p.Kind == "text" {
					texts = append(texts, p.Text)
				} else {
					opaque = true
				}
			}
			out = append(out, core.Part{Kind: "tool_result", ID: str(b["tool_use_id"]), Text: strings.Join(texts, "\n"), Opaque: opaque})
		case "thinking", "redacted_thinking":
			if typ == "thinking" {
				if e := fields(b, "type thinking signature", "thinking."); e != nil {
					return nil, e
				}
				text, ok := b["thinking"].(string)
				sig, valid := b["signature"].(string)
				if !ok || !valid || sig == "" {
					return nil, core.Invalid("thinking")
				}
				out = append(out, core.Part{Kind: "thinking", Text: text, Signature: sig, Opaque: true})
			} else {
				if e := fields(b, "type data", "redacted_thinking."); e != nil {
					return nil, e
				}
				data, ok := b["data"].(string)
				if !ok || data == "" {
					return nil, core.Invalid("redacted_thinking")
				}
				out = append(out, core.Part{Kind: "redacted_thinking", Data: data, Opaque: true})
			}
		default:
			return nil, core.Invalid("content.type")
		}
	}
	return out, nil
}
func toolCall(v any) (core.Part, *core.Error) {
	m := obj(v)
	f := obj(m["function"])
	name := str(f["name"])
	args := str(f["arguments"])
	if str(m["type"]) != "function" || str(m["id"]) == "" || name == "" || !json.Valid([]byte(args)) || objFromJSON(args) == nil {
		return core.Part{}, core.Invalid("tool_calls")
	}
	return core.Part{Kind: "tool_call", ID: str(m["id"]), Name: name, Arguments: json.RawMessage(args)}, nil
}
func objFromJSON(s string) map[string]any {
	var m map[string]any
	_ = json.Unmarshal([]byte(s), &m)
	return m
}

// Some SDK final-object helpers attach parsed metadata to replayed output.
// A non-null annotation must be exactly the JSON value carried by the text.
func parsedMatches(raw string, annotation any) bool {
	if annotation == nil {
		return true
	}
	encoded, e := json.Marshal(annotation)
	if e != nil {
		return false
	}
	var expected, parsed any
	return json.Unmarshal([]byte(raw), &expected) == nil && json.Unmarshal(encoded, &parsed) == nil && reflect.DeepEqual(expected, parsed)
}
func Decode(p core.Protocol, reader io.Reader, pathModel string, pathStream bool) (*core.Request, *core.Error) {
	d := json.NewDecoder(reader)
	d.UseNumber()
	var b map[string]any
	if e := d.Decode(&b); e != nil || b == nil {
		return nil, core.Err("invalid_json", 400, "Invalid JSON body")
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		return nil, core.Err("invalid_json", 400, "Expected one JSON object")
	}
	if depth(b) > 32 {
		return nil, core.Invalid("JSON depth")
	}
	r := &core.Request{Protocol: p, Model: str(b["model"]), ToolChoice: "auto", Parameters: map[string]any{}, Extras: map[string]any{}}
	var e *core.Error
	if p != core.Gemini {
		if e = checkBool(b, "stream"); e != nil {
			return nil, e
		}
		r.Stream = scalarBool(b["stream"])
	} else {
		r.Model = pathModel
		r.Stream = pathStream
	}
	switch p {
	case core.Chat:
		e = decodeChat(b, r)
	case core.Responses:
		e = decodeResponses(b, r)
	case core.Anthropic:
		e = decodeAnthropic(b, r)
	case core.Gemini:
		e = decodeGemini(b, r)
	default:
		e = core.Invalid("protocol")
	}
	if e != nil {
		return nil, e
	}
	if r.Reasoning.Enabled != nil && (r.Reasoning.Effort == "none" && *r.Reasoning.Enabled || r.Reasoning.Effort != "" && r.Reasoning.Effort != "none" && !*r.Reasoning.Enabled) {
		return nil, core.Invalid("reasoning.conflicting_controls")
	}
	if r.Model == "" {
		return nil, core.Invalid("model")
	}
	r.Latest = core.Extract(r.Messages)
	seenTools := map[string]bool{}
	for _, t := range r.Tools {
		if t.Name == "" {
			return nil, core.Invalid("tools.name")
		}
		if seenTools[t.Name] {
			return nil, core.Invalid("tools.duplicate_name")
		}
		seenTools[t.Name] = true
		if len(t.Schema) > 0 {
			if _, err := schema.Compile(t.Schema); err != nil {
				return nil, core.Invalid("tools.schema")
			}
		}
	}
	if len(r.Constraint.Schema) > 0 {
		if _, err := schema.Compile(r.Constraint.Schema); err != nil {
			return nil, core.Invalid("output.schema")
		}
	}
	return r, nil
}
func depth(v any) int {
	n := 0
	switch x := v.(type) {
	case map[string]any:
		for _, a := range x {
			if d := depth(a); d > n {
				n = d
			}
		}
	case []any:
		for _, a := range x {
			if d := depth(a); d > n {
				n = d
			}
		}
	}
	return n + 1
}
func params(b map[string]any, r *core.Request, max, stop string) *core.Error {
	if v, ok := b[max]; ok && v != nil {
		n, ok := number(v)
		if !ok || n < 1 || n != float64(int(n)) || n > 1e6 {
			return core.Invalid(max)
		}
		i := int(n)
		r.MaxTokens = &i
	}
	var e *core.Error
	r.Stop, e = stringList(b[stop])
	if e != nil {
		return e
	}
	for _, k := range []string{"temperature", "top_p", "frequency_penalty", "presence_penalty", "seed"} {
		if v, ok := b[k]; ok && v != nil {
			n, ok := number(v)
			if !ok {
				return core.Invalid(k)
			}
			if k == "seed" && (n != float64(int64(n)) || n < -9.22e18 || n > 9.22e18) {
				return core.Invalid(k)
			}
			if k == "temperature" && (n < 0 || n > 2) || k == "top_p" && (n < 0 || n > 1) || (k == "frequency_penalty" || k == "presence_penalty") && (n < -2 || n > 2) {
				return core.Invalid(k)
			}
			r.Parameters[k] = v
		}
	}
	return nil
}
func reasoningEffort(v any, r *core.Request) *core.Error {
	if v == nil {
		return nil
	}
	effort, ok := v.(string)
	if !ok {
		return core.Invalid("reasoning_effort")
	}
	switch effort {
	case "none", "minimal", "low", "medium", "high", "xhigh", "max":
		r.Parameters["reasoning_effort"] = effort
		r.Reasoning.Effort = effort
		r.ThinkingOff = effort == "none"
		return nil
	default:
		return core.Invalid("reasoning_effort")
	}
}
func format(v any, r *core.Request, chat bool) *core.Error {
	if v == nil {
		return nil
	}
	m := obj(v)
	if m == nil {
		return core.Invalid("response_format")
	}
	kind := str(m["type"])
	switch kind {
	case "text", "":
		if e := fields(m, "type", "format."); e != nil {
			return e
		}
		return nil
	case "json_object":
		if e := fields(m, "type", "format."); e != nil {
			return e
		}
		r.Constraint.Kind = "json_object"
	case "json_schema":
		r.Constraint.Kind = "json_schema"
		if chat {
			if e := fields(m, "type json_schema", "format."); e != nil {
				return e
			}
			m = obj(m["json_schema"])
		}
		if e := fields(m, "type name description schema strict", "format."); e != nil {
			return e
		}
		if e := checkBool(m, "strict"); e != nil {
			return e
		}
		r.Constraint.Schema = obj(m["schema"])
		r.Constraint.Name = str(m["name"])
		r.Constraint.Strict = scalarBool(m["strict"])
		if r.Constraint.Schema == nil {
			return core.Invalid("response_format.schema")
		}
	default:
		return core.Invalid("response_format.type")
	}
	return nil
}
func chatTools(v any, r *core.Request, flat bool) *core.Error {
	if v == nil {
		return nil
	}
	a, ok := v.([]any)
	if !ok {
		return core.Invalid("tools")
	}
	for _, x := range a {
		m := obj(x)
		if str(m["type"]) != "function" {
			return core.Invalid("tools.type")
		}
		f := m
		if !flat {
			if e := fields(m, "type function", "tools."); e != nil {
				return e
			}
			f = obj(m["function"])
		}
		if e := fields(f, "type name description parameters strict", "tools.function."); e != nil {
			return e
		}
		if e := checkBool(f, "strict"); e != nil {
			return e
		}
		if _, e := objectField(f["parameters"], "tools.parameters"); e != nil {
			return e
		}
		r.Tools = append(r.Tools, core.Tool{Name: str(f["name"]), Description: str(f["description"]), Schema: obj(f["parameters"])})
	}
	return nil
}
func chatChoice(v any, r *core.Request, flat bool) *core.Error {
	if v == nil {
		return nil
	}
	if s, ok := v.(string); ok {
		if s != "auto" && s != "none" && s != "required" {
			return core.Invalid("tool_choice")
		}
		r.ToolChoice = s
		return nil
	}
	m := obj(v)
	if str(m["type"]) != "function" {
		return core.Invalid("tool_choice")
	}
	if flat {
		r.ToolChoice = str(m["name"])
	} else {
		r.ToolChoice = str(obj(m["function"])["name"])
	}
	if r.ToolChoice == "" {
		return core.Invalid("tool_choice.name")
	}
	return nil
}
func decodeChat(b map[string]any, r *core.Request) *core.Error {
	if e := fields(b, "model messages stream stream_options tools tool_choice max_tokens max_completion_tokens stop response_format temperature top_p frequency_penalty presence_penalty seed n store metadata user safety_identifier parallel_tool_calls service_tier reasoning_effort", ""); e != nil {
		return e
	}
	if v := b["n"]; v != nil {
		n, ok := number(v)
		if !ok || n != 1 {
			return core.Invalid("n")
		}
	}
	if scalarBool(b["store"]) {
		return core.Invalid("store")
	}
	if e := checkBool(b, "store"); e != nil {
		return e
	}
	if b["max_tokens"] != nil && b["max_completion_tokens"] != nil {
		return core.Invalid("max_tokens")
	}
	max := "max_tokens"
	if b["max_completion_tokens"] != nil {
		max = "max_completion_tokens"
	}
	if e := params(b, r, max, "stop"); e != nil {
		return e
	}
	if e := reasoningEffort(b["reasoning_effort"], r); e != nil {
		return e
	}
	a, ok := b["messages"].([]any)
	if !ok || len(a) == 0 {
		return core.Invalid("messages")
	}
	for i, x := range a {
		m := obj(x)
		if e := fields(m, "role content name tool_call_id tool_calls refusal reasoning_content", "messages."); e != nil {
			return e
		}
		// Keep assistant reasoning separate from text and latest-user matching.
		if value := m["reasoning_content"]; value != nil {
			text, ok := value.(string)
			if !ok || text != "" && str(m["role"]) != "assistant" {
				return core.Invalid("messages.reasoning_content")
			}
		}
		role := str(m["role"])
		if !strings.Contains(" system developer user assistant tool ", " "+role+" ") || role == "" {
			return core.Invalid("messages.role")
		}
		message := core.Message{Role: role, SourceIndex: i}
		if text := str(m["reasoning_content"]); text != "" {
			message.Parts = append(message.Parts, core.Part{Kind: "reasoning", Text: text})
		}
		if m["content"] != nil {
			parts, e := textParts(m["content"], "chat")
			if e != nil {
				return e
			}
			message.Parts = append(message.Parts, parts...)
		} else if role != "assistant" {
			return core.Invalid("messages.content")
		}
		if role == "tool" {
			if str(m["tool_call_id"]) == "" {
				return core.Invalid("tool_call_id")
			}
			text := ""
			for _, p := range message.Parts {
				if p.Kind != "text" {
					return core.Invalid("tool content")
				}
				text += p.Text
			}
			message.Parts = []core.Part{{Kind: "tool_result", ID: str(m["tool_call_id"]), Text: text}}
		}
		calls, e := arrayField(m["tool_calls"], "messages.tool_calls")
		if e != nil {
			return e
		}
		if len(calls) > 0 && role != "assistant" {
			return core.Invalid("messages.tool_calls.role")
		}
		for _, call := range calls {
			p, e := toolCall(call)
			if e != nil {
				return e
			}
			message.Parts = append(message.Parts, p)
		}
		r.Messages = append(r.Messages, message)
	}
	if e := chatTools(b["tools"], r, false); e != nil {
		return e
	}
	if e := chatChoice(b["tool_choice"], r, false); e != nil {
		return e
	}
	if e := format(b["response_format"], r, true); e != nil {
		return e
	}
	if b["stream_options"] != nil {
		m := obj(b["stream_options"])
		if m == nil {
			return core.Invalid("stream_options")
		}
		if e := fields(m, "include_usage include_obfuscation", "stream_options."); e != nil {
			return e
		}
		if e := checkBool(m, "include_usage"); e != nil {
			return e
		}
		r.IncludeUsage = scalarBool(m["include_usage"])
	}
	return nil
}
func decodeResponses(b map[string]any, r *core.Request) *core.Error {
	if e := fields(b, "model input instructions stream max_output_tokens tools tool_choice text store metadata user safety_identifier temperature top_p parallel_tool_calls reasoning truncation service_tier", ""); e != nil {
		return e
	}
	if e := checkBool(b, "store"); e != nil {
		return e
	}
	if scalarBool(b["store"]) {
		return core.Invalid("store")
	}
	if e := params(b, r, "max_output_tokens", "__no_stop"); e != nil {
		return e
	}
	if e := responseReasoning(b["reasoning"], r); e != nil {
		return e
	}
	if b["instructions"] != nil {
		s, ok := b["instructions"].(string)
		if !ok {
			return core.Invalid("instructions")
		}
		r.Messages = append(r.Messages, core.Message{Role: "developer", SourceIndex: -1, Parts: []core.Part{{Kind: "text", Text: s}}})
	}
	if s, ok := b["input"].(string); ok {
		r.Messages = append(r.Messages, core.Message{Role: "user", Parts: []core.Part{{Kind: "text", Text: s}}})
	} else {
		a, ok := b["input"].([]any)
		if !ok {
			return core.Invalid("input")
		}
		for i, x := range a {
			m := obj(x)
			typ := str(m["type"])
			switch typ {
			case "", "message":
				if e := fields(m, "type role content id status", "input."); e != nil {
					return e
				}
				role := str(m["role"])
				if role != "user" && role != "assistant" && role != "system" && role != "developer" {
					return core.Invalid("input.role")
				}
				parts, e := textParts(m["content"], "responses")
				if e != nil {
					return e
				}
				r.Messages = append(r.Messages, core.Message{Role: role, SourceIndex: i, Parts: parts})
			case "function_call":
				if e := fields(m, "type name call_id arguments id status parsed_arguments", "input."); e != nil {
					return e
				}
				name, id, args := str(m["name"]), str(m["call_id"]), str(m["arguments"])
				if name == "" || id == "" || objFromJSON(args) == nil {
					return core.Invalid("function_call")
				}
				if !parsedMatches(args, m["parsed_arguments"]) {
					return core.Invalid("function_call.parsed_arguments")
				}
				r.Messages = append(r.Messages, core.Message{Role: "assistant", SourceIndex: i, Parts: []core.Part{{Kind: "tool_call", Name: name, ID: id, Arguments: json.RawMessage(args)}}})
			case "function_call_output":
				if e := fields(m, "type call_id output id status", "input."); e != nil {
					return e
				}
				parts, e := textParts(m["output"], "responses")
				if e != nil {
					return e
				}
				var texts []string
				for _, p := range parts {
					if p.Kind != "text" {
						return core.Invalid("function_call_output")
					}
					texts = append(texts, p.Text)
				}
				if str(m["call_id"]) == "" {
					return core.Invalid("call_id")
				}
				r.Messages = append(r.Messages, core.Message{Role: "tool", SourceIndex: i, Parts: []core.Part{{Kind: "tool_result", ID: str(m["call_id"]), Text: strings.Join(texts, "\n")}}})
			case "reasoning":
				if e := fields(m, "type id status summary encrypted_content", "reasoning."); e != nil {
					return e
				}
				if m["encrypted_content"] != nil {
					return core.Invalid("encrypted_content")
				}
				a, e := arrayField(m["summary"], "reasoning.summary")
				if e != nil {
					return e
				}
				parts := []core.Part{}
				for _, value := range a {
					summary := obj(value)
					if e := fields(summary, "type text", "reasoning.summary."); e != nil {
						return e
					}
					text, ok := summary["text"].(string)
					if str(summary["type"]) != "summary_text" || !ok {
						return core.Invalid("reasoning.summary")
					}
					parts = append(parts, core.Part{Kind: "reasoning_summary", Text: text})
				}
				r.Messages = append(r.Messages, core.Message{Role: "assistant", SourceIndex: i, Parts: parts})
			default:
				return core.Invalid("input.type")
			}
		}
	}
	if e := chatTools(b["tools"], r, true); e != nil {
		return e
	}
	if e := chatChoice(b["tool_choice"], r, true); e != nil {
		return e
	}
	if b["text"] != nil {
		m, e := objectField(b["text"], "text")
		if e != nil {
			return e
		}
		if e := fields(m, "format verbosity", "text."); e != nil {
			return e
		}
		if e := format(m["format"], r, false); e != nil {
			return e
		}
	}
	return nil
}
func decodeAnthropic(b map[string]any, r *core.Request) *core.Error {
	if e := fields(b, "model messages system max_tokens stream tools tool_choice stop_sequences temperature top_p top_k metadata thinking output_config service_tier", ""); e != nil {
		return e
	}
	if b["max_tokens"] == nil {
		return core.Invalid("max_tokens")
	}
	if e := params(b, r, "max_tokens", "stop_sequences"); e != nil {
		return e
	}
	if b["top_k"] != nil {
		r.Extras["top_k"] = b["top_k"]
	}
	if b["system"] != nil {
		parts, e := textParts(b["system"], "anthropic")
		if e != nil {
			return e
		}
		for _, p := range parts {
			if p.Kind != "text" {
				return core.Invalid("system")
			}
		}
		r.Messages = append(r.Messages, core.Message{Role: "system", SourceIndex: -1, Parts: parts})
	}
	a, ok := b["messages"].([]any)
	if !ok || len(a) == 0 {
		return core.Invalid("messages")
	}
	for i, x := range a {
		m := obj(x)
		if e := fields(m, "role content", "messages."); e != nil {
			return e
		}
		role := str(m["role"])
		if role != "user" && role != "assistant" {
			return core.Invalid("messages.role")
		}
		parts, e := textParts(m["content"], "anthropic")
		if e != nil {
			return e
		}
		only := len(parts) > 0
		for _, p := range parts {
			if (p.Kind == "thinking" || p.Kind == "redacted_thinking") && role != "assistant" {
				return core.Invalid("thinking.role")
			}
			if p.Kind == "tool_call" && role != "assistant" || p.Kind == "tool_result" && role != "user" {
				return core.Invalid("messages.tool_role")
			}
			if p.Kind != "tool_result" {
				only = false
			}
		}
		if role == "user" && only {
			role = "tool"
		}
		r.Messages = append(r.Messages, core.Message{Role: role, SourceIndex: i, Parts: parts})
	}
	tools, e := arrayField(b["tools"], "tools")
	if e != nil {
		return e
	}
	for _, x := range tools {
		m := obj(x)
		if e := fields(m, "type name description input_schema", "tools."); e != nil {
			return e
		}
		if str(m["type"]) != "" {
			return core.Invalid("tools.type")
		}
		if obj(m["input_schema"]) == nil {
			return core.Invalid("tools.input_schema")
		}
		r.Tools = append(r.Tools, core.Tool{Name: str(m["name"]), Description: str(m["description"]), Schema: obj(m["input_schema"])})
	}
	if b["tool_choice"] != nil {
		m := obj(b["tool_choice"])
		switch str(m["type"]) {
		case "auto", "none":
			r.ToolChoice = str(m["type"])
		case "any":
			r.ToolChoice = "required"
		case "tool":
			r.ToolChoice = str(m["name"])
			if r.ToolChoice == "" {
				return core.Invalid("tool_choice.name")
			}
		default:
			return core.Invalid("tool_choice")
		}
	}
	if b["output_config"] != nil {
		m, e := objectField(b["output_config"], "output_config")
		if e != nil {
			return e
		}
		if e := fields(m, "format effort", "output_config."); e != nil {
			return e
		}
		if e := format(m["format"], r, false); e != nil {
			return e
		}
		if e := reasoningEffort(m["effort"], r); e != nil {
			return e
		}
	}
	if e := anthropicReasoning(b["thinking"], r); e != nil {
		return e
	}
	return nil
}
func decodeGemini(b map[string]any, r *core.Request) *core.Error {
	if e := fields(b, "contents systemInstruction tools toolConfig generationConfig safetySettings", ""); e != nil {
		return e
	}
	settings, e := arrayField(b["safetySettings"], "safetySettings")
	if e != nil {
		return e
	}
	if len(settings) > 6 {
		return core.Invalid("safetySettings")
	}
	seen := map[string]bool{}
	for _, value := range settings {
		setting := obj(value)
		if e := fields(setting, "category threshold", "safetySettings."); e != nil {
			return e
		}
		category, threshold := str(setting["category"]), str(setting["threshold"])
		switch category {
		case "HARM_CATEGORY_HATE_SPEECH", "HARM_CATEGORY_HARASSMENT", "HARM_CATEGORY_SEXUALLY_EXPLICIT", "HARM_CATEGORY_DANGEROUS_CONTENT", "HARM_CATEGORY_CIVIC_INTEGRITY", "HARM_CATEGORY_JAILBREAK":
		default:
			return core.Invalid("safetySettings.category")
		}
		if seen[category] {
			return core.Invalid("safetySettings.category")
		}
		switch threshold {
		case "HARM_BLOCK_THRESHOLD_UNSPECIFIED", "BLOCK_LOW_AND_ABOVE", "BLOCK_MEDIUM_AND_ABOVE", "BLOCK_ONLY_HIGH", "BLOCK_NONE", "OFF":
		default:
			return core.Invalid("safetySettings.threshold")
		}
		seen[category] = true
	}
	if len(settings) > 0 {
		// Presets are local fixtures. A Chat upstream has no equivalent for
		// these native controls, so the existing Extras preflight rejects them.
		r.Extras["safetySettings"] = settings
	}
	parse := func(x any, index int, total int) (core.Message, *core.Error) {
		m := obj(x)
		if e := fields(m, "role parts", "contents."); e != nil {
			return core.Message{}, e
		}
		role := str(m["role"])
		if role == "" {
			if total != 1 {
				return core.Message{}, core.Invalid("contents.role")
			}
			role = "user"
		}
		if role == "model" {
			role = "assistant"
		}
		if role != "user" && role != "assistant" && role != "system" {
			return core.Message{}, core.Invalid("contents.role")
		}
		parts, ok := m["parts"].([]any)
		if !ok || len(parts) == 0 {
			return core.Message{}, core.Invalid("contents.parts")
		}
		msg := core.Message{Role: role, SourceIndex: index}
		onlyResult := true
		for _, x := range parts {
			p := obj(x)
			if p == nil {
				return msg, core.Invalid("parts")
			}
			if e := fields(p, "text thought thoughtSignature inlineData fileData functionCall functionResponse", "parts."); e != nil {
				return msg, e
			}
			part := core.Part{}
			count := 0
			for _, key := range []string{"text", "inlineData", "fileData", "functionCall", "functionResponse"} {
				if p[key] != nil {
					count++
				}
			}
			if count != 1 {
				return msg, core.Invalid("parts.type")
			}
			if e := checkBool(p, "thought"); e != nil {
				return msg, e
			}
			switch {
			case p["text"] != nil:
				s, ok := p["text"].(string)
				if !ok {
					return msg, core.Invalid("parts.text")
				}
				part = core.Part{Kind: "text", Text: s}
				if scalarBool(p["thought"]) {
					if role != "assistant" {
						return msg, core.Invalid("thought.role")
					}
					part.Kind = "reasoning_summary"
				}
			case p["inlineData"] != nil:
				d := obj(p["inlineData"])
				v, e := imageURL("data:" + str(d["mimeType"]) + ";base64," + str(d["data"]))
				if e != nil {
					return msg, e
				}
				part = v
			case p["fileData"] != nil:
				d := obj(p["fileData"])
				s := str(d["fileUri"])
				if strings.HasPrefix(s, "https://generativelanguage.googleapis.com/") || strings.HasPrefix(s, "gs://") {
					part = core.Part{Kind: "image", URL: s, Opaque: true}
				} else {
					v, e := imageURL(s)
					if e != nil {
						return msg, e
					}
					part = v
				}
			case p["functionCall"] != nil:
				if role != "assistant" {
					return msg, core.Invalid("functionCall.role")
				}
				f := obj(p["functionCall"])
				if str(f["name"]) == "" || obj(f["args"]) == nil {
					return msg, core.Invalid("functionCall")
				}
				j, _ := json.Marshal(f["args"])
				part = core.Part{Kind: "tool_call", ID: str(f["id"]), Name: str(f["name"]), Arguments: j}
				if part.ID == "" {
					part.ID = "call_gemini_" + str(f["name"]) + "_" + strconv.Itoa(index)
				}
			case p["functionResponse"] != nil:
				if role != "user" {
					return msg, core.Invalid("functionResponse.role")
				}
				f := obj(p["functionResponse"])
				if str(f["name"]) == "" || obj(f["response"]) == nil {
					return msg, core.Invalid("functionResponse")
				}
				j, _ := json.Marshal(f["response"])
				part = core.Part{Kind: "tool_result", ID: str(f["id"]), Name: str(f["name"]), Text: string(j)}
			default:
				return msg, core.Invalid("parts.type")
			}
			if p["thoughtSignature"] != nil {
				part.Opaque = true
			}
			if part.Kind != "tool_result" {
				onlyResult = false
			}
			msg.Parts = append(msg.Parts, part)
		}
		if role == "user" && onlyResult {
			msg.Role = "tool"
		}
		return msg, nil
	}
	if b["systemInstruction"] != nil {
		m := obj(b["systemInstruction"])
		if m == nil {
			return core.Invalid("systemInstruction")
		}
		m["role"] = "system"
		msg, e := parse(m, -1, 1)
		if e != nil {
			return e
		}
		r.Messages = append(r.Messages, msg)
	}
	a, ok := b["contents"].([]any)
	if !ok || len(a) == 0 {
		return core.Invalid("contents")
	}
	for i, x := range a {
		msg, e := parse(x, i, len(a))
		if e != nil {
			return e
		}
		if msg.Role == "system" {
			return core.Invalid("contents.role")
		}
		r.Messages = append(r.Messages, msg)
	}
	tools, e := arrayField(b["tools"], "tools")
	if e != nil {
		return e
	}
	for _, t := range tools {
		m := obj(t)
		if e := fields(m, "functionDeclarations", "tools."); e != nil {
			return e
		}
		functions, e := arrayField(m["functionDeclarations"], "functionDeclarations")
		if e != nil {
			return e
		}
		for _, x := range functions {
			f := obj(x)
			if e := fields(f, "name description parameters parametersJsonSchema", "functionDeclarations."); e != nil {
				return e
			}
			if f["parameters"] != nil && f["parametersJsonSchema"] != nil {
				return core.Invalid("functionDeclarations.parameters")
			}
			if _, e := objectField(f["parameters"], "functionDeclarations.parameters"); e != nil {
				return e
			}
			if _, e := objectField(f["parametersJsonSchema"], "functionDeclarations.parametersJsonSchema"); e != nil {
				return e
			}
			s := obj(f["parametersJsonSchema"])
			if s == nil {
				s = obj(f["parameters"])
			}
			r.Tools = append(r.Tools, core.Tool{Name: str(f["name"]), Description: str(f["description"]), Schema: normalizeSchema(s)})
		}
	}
	choice := obj(obj(b["toolConfig"])["functionCallingConfig"])
	switch str(choice["mode"]) {
	case "", "AUTO", "VALIDATED":
	case "NONE":
		r.ToolChoice = "none"
	case "ANY":
		r.ToolChoice = "required"
		names := arr(choice["allowedFunctionNames"])
		if len(names) > 1 {
			return core.Invalid("toolConfig.allowedFunctionNames")
		}
		if len(names) == 1 {
			r.ToolChoice = str(names[0])
			if r.ToolChoice == "" {
				return core.Invalid("toolConfig.allowedFunctionNames")
			}
		}
	default:
		return core.Invalid("toolConfig.mode")
	}
	g, e := objectField(b["generationConfig"], "generationConfig")
	if e != nil {
		return e
	}
	if e := fields(g, "temperature topP topK candidateCount maxOutputTokens stopSequences responseMimeType responseSchema responseJsonSchema _responseJsonSchema responseFormat thinkingConfig seed", "generationConfig."); e != nil {
		return e
	}
	if g["candidateCount"] != nil {
		n, ok := number(g["candidateCount"])
		if !ok || n != 1 {
			return core.Invalid("candidateCount")
		}
	}
	v := map[string]any{"max_tokens": g["maxOutputTokens"], "stop": g["stopSequences"], "temperature": g["temperature"], "top_p": g["topP"], "seed": g["seed"]}
	if e := params(v, r, "max_tokens", "stop"); e != nil {
		return e
	}
	if g["topK"] != nil {
		r.Extras["top_k"] = g["topK"]
	}
	var schemas []map[string]any
	for _, k := range []string{"responseSchema", "responseJsonSchema", "_responseJsonSchema"} {
		if g[k] != nil {
			s, e := objectField(g[k], k)
			if e != nil {
				return e
			}
			schemas = append(schemas, normalizeSchema(s))
		}
	}
	responseFormat, e := objectField(g["responseFormat"], "responseFormat")
	if e != nil {
		return e
	}
	if e := fields(responseFormat, "text", "responseFormat."); e != nil {
		return e
	}
	f, e := objectField(responseFormat["text"], "responseFormat.text")
	if e != nil {
		return e
	}
	if e := fields(f, "mimeType schema", "responseFormat.text."); e != nil {
		return e
	}
	newMime := ""
	switch str(f["mimeType"]) {
	case "", "MIME_TYPE_UNSPECIFIED":
	case "APPLICATION_JSON":
		newMime = "application/json"
	case "TEXT_PLAIN":
		newMime = "text/plain"
	default:
		return core.Invalid("responseFormat.text.mimeType")
	}
	if f["schema"] != nil {
		s, e := objectField(f["schema"], "responseFormat.text.schema")
		if e != nil {
			return e
		}
		schemas = append(schemas, normalizeSchema(s))
	}
	mime := str(g["responseMimeType"])
	if mime != "" && mime != "text/plain" && mime != "application/json" {
		return core.Invalid("responseMimeType")
	}
	if newMime != "" {
		if mime != "" && mime != newMime {
			return core.Invalid("conflicting JSON formats")
		}
		mime = newMime
	}
	if mime == "text/plain" && len(schemas) > 0 {
		return core.Invalid("conflicting JSON formats")
	}
	if mime == "application/json" {
		r.Constraint.Kind = "json_object"
	}
	if len(schemas) > 0 {
		r.Constraint.Kind = "json_schema"
		r.Constraint.Schema = schemas[0]
		for _, s := range schemas[1:] {
			a, _ := json.Marshal(schemas[0])
			b, _ := json.Marshal(s)
			if string(a) != string(b) {
				return core.Invalid("conflicting JSON formats")
			}
		}
	}
	if e := geminiReasoning(g["thinkingConfig"], r); e != nil {
		return e
	}
	return nil
}
func normalizeSchema(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := map[string]any{}
	for k, v := range m {
		switch k {
		case "property_ordering":
			out["propertyOrdering"] = v
		case "type":
			if s, ok := v.(string); ok {
				out[k] = strings.ToLower(s)
			} else {
				out[k] = v
			}
		case "nullable":
			if scalarBool(v) {
				t := str(m["type"])
				out["type"] = []any{strings.ToLower(t), "null"}
			}
		case "properties", "$defs":
			x := obj(v)
			n := map[string]any{}
			for name, s := range x {
				n[name] = normalizeSchema(obj(s))
			}
			out[k] = n
		case "items", "additionalProperties":
			if n := obj(v); n != nil {
				out[k] = normalizeSchema(n)
			} else {
				out[k] = v
			}
		default:
			out[k] = v
		}
	}
	if scalarBool(m["nullable"]) {
		out["type"] = []any{strings.ToLower(str(m["type"])), "null"}
	}
	return out
}
