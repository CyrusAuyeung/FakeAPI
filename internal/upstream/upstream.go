package upstream

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"fakeapi/internal/config"
	"fakeapi/internal/core"
	"fakeapi/internal/preset"
	"fakeapi/internal/schema"
)

func Preflight(r *core.Request, t config.Target, u config.Upstream) *core.Error {
	c := t.Capabilities
	if r.ToolChoice != "auto" && r.ToolChoice != "none" {
		if len(r.Tools) == 0 {
			return core.Err("unsupported_constraint", 400, "Forced tool choice requires a function declaration")
		}
		if r.ToolChoice != "required" {
			found := false
			for _, tool := range r.Tools {
				if tool.Name == r.ToolChoice {
					found = true
				}
			}
			if !found {
				return core.Err("unsupported_constraint", 400, "Forced function must be declared")
			}
		}
	}
	if len(r.Tools) > 0 && !c.Tools {
		return core.Err("unsupported_capability", 400, "Target does not support tools")
	}
	if r.Constraint.Kind == "json_object" && !c.JSONObject || r.Constraint.Kind == "json_schema" && !c.JSONSchema {
		return core.Err("unsupported_capability", 400, "Target does not support the requested JSON format")
	}
	if r.Protocol != core.Chat && u.ReasoningTranslation == "portable_text" && (r.Constraint.Kind != "" || r.ToolChoice != "auto" && r.ToolChoice != "none") {
		return core.Err("unsupported_constraint", 400, "Portable reasoning conflicts with output constraints")
	}
	for _, m := range r.Messages {
		if m.Role == "developer" && !c.DeveloperRole && t.DeveloperRoleFallback != "system" {
			return core.Err("unsupported_capability", 400, "Target does not support developer messages")
		}
		for _, p := range m.Parts {
			if p.Kind == "image" && (!c.Images || p.Opaque) {
				return core.Err("unsupported_capability", 400, "Target cannot accept this image")
			}
			if p.Opaque {
				if u.Kind == "anthropic" && r.Protocol == core.Anthropic && (p.Kind == "thinking" || p.Kind == "redacted_thinking") {
					continue
				}
				return core.Err("unsupported_capability", 400, "Content cannot be translated without losing information")
			}
			if p.Kind == "reasoning" && t.Reasoning.History == "reject" {
				return core.Err("unsupported_capability", 400, "Target rejects reasoning history")
			}
		}
	}
	if len(r.Extras) > 0 {
		return core.Err("unsupported_capability", 400, "Request contains parameters without an upstream equivalent")
	}
	if _, e := reasoningParameters(r, t); e != nil {
		return e
	}
	return nil
}
func Translate(r *core.Request, t config.Target) (map[string]any, *core.Error) {
	if t.Reasoning.Mode == "anthropic" {
		return translateAnthropic(r, t)
	}
	reasoning, err := reasoningParameters(r, t)
	if err != nil {
		return nil, err
	}
	messages := []any{}
	pending := map[string]string{}
	for _, m := range r.Messages {
		role := m.Role
		if role == "developer" && !t.Capabilities.DeveloperRole {
			role = "system"
		}
		text := []any{}
		calls := []any{}
		history := ""
		for _, p := range m.Parts {
			switch p.Kind {
			case "reasoning":
				if t.Reasoning.History == "preserve" {
					history += p.Text
				}
			case "text":
				text = append(text, map[string]any{"type": "text", "text": p.Text})
			case "image":
				u := p.URL
				if p.Data != "" {
					u = "data:" + p.MIME + ";base64," + p.Data
				}
				image := map[string]any{"url": u}
				if p.Detail != "" {
					image["detail"] = p.Detail
				}
				text = append(text, map[string]any{"type": "image_url", "image_url": image})
			case "tool_call":
				if len(pending) > 0 {
					return nil, core.Err("unsupported_tool_sequence", 400, "Only one outstanding tool call is supported")
				}
				pending[p.ID] = p.Name
				calls = append(calls, map[string]any{"id": p.ID, "type": "function", "function": map[string]any{"name": p.Name, "arguments": string(p.Arguments)}})
			case "tool_result":
				id := p.ID
				if id == "" && len(pending) == 1 {
					for key, name := range pending {
						if name == p.Name {
							id = key
						}
					}
				}
				if _, ok := pending[id]; !ok {
					return nil, core.Err("unsupported_tool_sequence", 400, "Tool result does not match a pending call")
				}
				delete(pending, id)
				messages = append(messages, map[string]any{"role": "tool", "tool_call_id": id, "content": p.Text})
			}
		}
		if len(text) > 0 || len(calls) > 0 {
			v := map[string]any{"role": role}
			if len(text) > 0 {
				plain := ""
				allText := true
				for _, part := range text {
					m := object(part)
					if m["type"] != "text" {
						allText = false
						break
					}
					if plain != "" {
						plain += "\n"
					}
					plain += stringValue(m["text"])
				}
				if allText {
					v["content"] = plain
				} else {
					v["content"] = text
				}
			} else {
				v["content"] = nil
			}
			if history != "" {
				v["reasoning_content"] = history
			}
			if len(calls) > 0 {
				v["tool_calls"] = calls
			}
			messages = append(messages, v)
		}
	}
	if len(pending) > 0 {
		return nil, core.Err("unsupported_tool_sequence", 400, "Tool call is missing its result")
	}
	body := map[string]any{"model": t.Model.Resolve(), "messages": messages, "stream": r.Stream}
	if r.MaxTokens != nil {
		body[t.MaxTokensParameter] = *r.MaxTokens
	}
	if len(r.Stop) > 0 {
		body["stop"] = r.Stop
	}
	for k, v := range r.Parameters {
		if k == "reasoning_effort" {
			continue
		}
		body[k] = v
	}
	for k, v := range reasoning {
		body[k] = v
	}
	if r.Stream {
		body["stream_options"] = map[string]any{"include_usage": true}
	}
	if len(r.Tools) > 0 {
		tools := []any{}
		for _, tool := range r.Tools {
			tools = append(tools, map[string]any{"type": "function", "function": map[string]any{"name": tool.Name, "description": tool.Description, "parameters": tool.Schema}})
		}
		body["tools"] = tools
		if t.Capabilities.ParallelToolCallsParameter {
			body["parallel_tool_calls"] = false
		}
	}
	if r.ToolChoice == "auto" || r.ToolChoice == "none" || r.ToolChoice == "required" {
		if len(r.Tools) > 0 {
			body["tool_choice"] = r.ToolChoice
		}
	} else {
		body["tool_choice"] = map[string]any{"type": "function", "function": map[string]any{"name": r.ToolChoice}}
	}
	if r.Constraint.Kind == "json_object" {
		body["response_format"] = map[string]any{"type": "json_object"}
	} else if r.Constraint.Kind == "json_schema" {
		body["response_format"] = map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": defaultName(r.Constraint.Name), "strict": r.Constraint.Strict, "schema": r.Constraint.Schema}}
	}
	return body, nil
}
func defaultName(s string) string {
	if s == "" {
		return "fakeapi_output"
	}
	return s
}
func mapped(status int) *core.Error {
	switch {
	case status == 401 || status == 403:
		return core.Err("upstream_authentication_error", 502, "Upstream authentication failed")
	case status == 429:
		return core.Err("upstream_rate_limited", 503, "Upstream temporarily rate limited")
	case status >= 500:
		return core.Err("upstream_unavailable", 503, "Upstream temporarily unavailable")
	default:
		return core.Err("upstream_bad_response", 502, "Upstream returned an unsupported response")
	}
}

type Client struct{ HTTP *http.Client }

func New(u config.Upstream) *Client {
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: (&net.Dialer{Timeout: u.Timeouts.Connect.Value(), KeepAlive: 30 * time.Second}).DialContext, TLSHandshakeTimeout: u.Timeouts.Connect.Value(), MaxIdleConns: 16, MaxIdleConnsPerHost: u.MaxConcurrent, IdleConnTimeout: 60 * time.Second}
	return &Client{HTTP: &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (c *Client) Close() { c.HTTP.CloseIdleConnections() }
func (c *Client) Run(ctx context.Context, r *core.Request, t config.Target, u config.Upstream, limits config.Server, emit func(core.Event) error) *core.Error {
	if u.Kind == "anthropic" {
		return c.runAnthropic(ctx, r, t, u, limits, emit)
	}
	body, e := Translate(r, t)
	if e != nil {
		return e
	}
	b, _ := json.Marshal(body)
	sub, cancel := context.WithCancel(ctx)
	defer cancel()
	var timeout atomic.Bool
	var firstState atomic.Uint32 // 0 pending, 1 received, 2 expired
	first := time.AfterFunc(u.Timeouts.FirstEvent.Value(), func() {
		if firstState.CompareAndSwap(0, 2) {
			timeout.Store(true)
			cancel()
		}
	})
	defer first.Stop()
	markFirst := func() *core.Error {
		firstState.CompareAndSwap(0, 1)
		first.Stop()
		if firstState.Load() == 2 || sub.Err() != nil {
			return core.Err("upstream_timeout", 504, "Upstream response timed out")
		}
		return nil
	}
	req, err := http.NewRequestWithContext(sub, "POST", strings.TrimRight(u.BaseURL.Resolve(), "/")+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return core.Err("upstream_request_error", 502, "Unable to prepare upstream request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+os.Getenv(u.APIKeyEnv))
	req.Header.Set("Accept", "application/json")
	if r.Stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil || timeout.Load() {
			return core.Err("upstream_timeout", 504, "Upstream response timed out")
		}
		return core.Err("upstream_connection_error", 502, "Unable to connect to upstream")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return mapped(resp.StatusCode)
	}
	result := core.Result{Finish: "stop"}
	wireFinish := ""
	usageProvided := false
	bytesOut := 0
	var wireUsage map[string]any
	portableStarted, portableClosed := false, false
	output := func(e core.Event) *core.Error {
		if sub.Err() != nil {
			return core.Err("upstream_timeout", 504, "Upstream response timed out")
		}
		bytesOut += len(e.Text)
		if e.Tool != nil {
			bytesOut += len(e.Tool.ID) + len(e.Tool.Name)
		}
		if bytesOut > limits.MaxResponseBytes {
			return core.Err("upstream_response_too_large", 502, "Upstream output exceeds configured limit")
		}
		if e.Kind == "text_delta" && e.Text != "" || e.Kind == "reasoning_delta" && e.Text != "" || e.Kind == "tool_start" || e.Kind == "finish" {
			if e := markFirst(); e != nil {
				return e
			}
		}
		if err := emit(e); err != nil {
			return core.Err("client_write_failed", 499, "Client disconnected")
		}
		return nil
	}
	readUsage := func(v map[string]any) {
		input, ok1 := integer(v["prompt_tokens"])
		completion, ok2 := integer(v["completion_tokens"])
		if !ok1 || !ok2 || input < 0 || completion < 0 {
			return
		}
		rawReason := object(v["completion_tokens_details"])["reasoning_tokens"]
		reason, validReason := integer(rawReason)
		has := rawReason != nil
		source := "upstream"
		if has && (!validReason || reason > completion) {
			return
		}
		if !has && result.Reasoning != "" {
			reason = core.Estimate(result.Reasoning)
			if reason > completion {
				reason = completion
			}
			source = "mixed"
		}
		rawCached := object(v["prompt_tokens_details"])["cached_tokens"]
		cached, validCached := integer(rawCached)
		if rawCached != nil && (!validCached || cached > input) {
			return
		}
		result.Usage = core.Usage{Input: input, Visible: completion - reason, Reasoning: reason, Cached: cached, Source: source}
		usageProvided = true
	}
	emitReason := func(s string) *core.Error {
		if resultSize(result)+len(s) > limits.MaxResponseBytes {
			return core.Err("upstream_response_too_large", 502, "Upstream output exceeds configured limit")
		}
		if result.Text != "" || result.Tool != nil {
			return core.Err("upstream_bad_response", 502, "Upstream reasoning appeared after the response body")
		}
		result.Reasoning += s
		if s == "" {
			return nil
		}
		if e := markFirst(); e != nil {
			return e
		}
		if r.ThinkingOff {
			return nil
		}
		if r.Protocol == core.Chat {
			return output(core.Event{Kind: "reasoning_delta", Text: s})
		}
		if u.ReasoningTranslation == "portable_text" {
			tag := ""
			if !portableStarted {
				tag = "[上游推理] "
				portableStarted = true
			}
			return output(core.Event{Kind: "text_delta", Text: tag + s})
		}
		return nil
	}
	closePortable := func() *core.Error {
		if portableStarted && !portableClosed {
			portableClosed = true
			return output(core.Event{Kind: "text_delta", Text: "\n\n"})
		}
		return nil
	}
	finishUsage := func() {
		readUsage(wireUsage)
		displayed := result
		if r.Protocol != core.Chat {
			displayed.Reasoning = ""
			if u.ReasoningTranslation == "portable_text" && result.Reasoning != "" && !r.ThinkingOff {
				displayed.Text = "[上游推理] " + result.Reasoning + "\n\n" + result.Text
				if usageProvided {
					result.Usage.Visible += result.Usage.Reasoning
					result.Usage.Reasoning = 0
				}
			}
		}
		if !usageProvided || r.Protocol == core.Anthropic && r.Stream {
			result.Usage = preset.Estimate(r, displayed)
		}
	}
	if !r.Stream {
		data, err := io.ReadAll(io.LimitReader(resp.Body, int64(limits.MaxResponseBytes)+1))
		if err != nil {
			return core.Err("upstream_timeout", 504, "Unable to read upstream response")
		}
		if len(data) > limits.MaxResponseBytes {
			return core.Err("upstream_response_too_large", 502, "Upstream output exceeds configured limit")
		}
		var v map[string]any
		if json.Unmarshal(data, &v) != nil {
			return core.Err("upstream_bad_json", 502, "Invalid upstream JSON response")
		}
		choices := array(v["choices"])
		if len(choices) != 1 {
			return core.Err("upstream_bad_response", 502, "Expected one upstream choice")
		}
		choice := object(choices[0])
		m := object(choice["message"])
		wireFinish = stringValue(choice["finish_reason"])
		if wireFinish == "" {
			return core.Err("upstream_bad_response", 502, "Missing upstream finish reason")
		}
		result.Text = stringValue(m["content"])
		result.Reasoning = stringValue(m["reasoning_content"])
		calls := array(m["tool_calls"])
		if len(calls) > 1 {
			return core.Err("upstream_bad_response", 502, "Multiple tool calls are unsupported")
		}
		if len(calls) == 1 {
			call := object(calls[0])
			f := object(call["function"])
			args := stringValue(f["arguments"])
			if stringValue(call["id"]) == "" || stringValue(f["name"]) == "" || !json.Valid([]byte(args)) {
				return core.Err("upstream_bad_response", 502, "Invalid upstream tool call")
			}
			result.Tool = &core.ToolCall{ID: stringValue(call["id"]), Name: stringValue(f["name"]), Arguments: args}
		}
		result.Finish = wireFinish
		if er := validateResult(r, result); er != nil {
			return er
		}
		if r.Protocol != core.Chat && u.ReasoningTranslation == "portable_text" && result.Reasoning != "" && result.Tool != nil {
			return core.Err("upstream_constraint_failed", 502, "Portable reasoning cannot be mixed with function output")
		}
		wireUsage = object(v["usage"])
		finishUsage()
		if result.Reasoning != "" {
			reason := result.Reasoning
			body, call := result.Text, result.Tool
			result.Reasoning = ""
			result.Text, result.Tool = "", nil
			if er := emitReason(reason); er != nil {
				return er
			}
			result.Text, result.Tool = body, call
		}
		if er := closePortable(); er != nil {
			return er
		}
		if result.Text != "" {
			if er := output(core.Event{Kind: "text_delta", Text: result.Text}); er != nil {
				return er
			}
		}
		if result.Tool != nil {
			if er := output(core.Event{Kind: "tool_start", Tool: result.Tool}); er != nil {
				return er
			}
			if er := output(core.Event{Kind: "tool_args_delta", Text: result.Tool.Arguments}); er != nil {
				return er
			}
			if er := output(core.Event{Kind: "tool_done"}); er != nil {
				return er
			}
		}
		return output(core.Event{Kind: "finish", Finish: wireFinish, Usage: result.Usage})
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		return core.Err("upstream_bad_response", 502, "Expected upstream event stream")
	}
	idle := time.AfterFunc(u.Timeouts.Idle.Value(), func() { timeout.Store(true); cancel() })
	defer idle.Stop()
	done := false
	err = ReadSSE(resp.Body, limits.MaxSSEEventBytes, func(data string) error {
		idle.Reset(u.Timeouts.Idle.Value())
		if data == "[DONE]" {
			done = true
			return io.EOF
		}
		var v map[string]any
		if json.Unmarshal([]byte(data), &v) != nil {
			return fmt.Errorf("E_SSE_JSON")
		}
		if v["error"] != nil {
			return fmt.Errorf("E_SSE_UPSTREAM")
		}
		if v["usage"] != nil {
			wireUsage = object(v["usage"])
		}
		choices := array(v["choices"])
		if len(choices) == 0 {
			return nil
		}
		if len(choices) != 1 {
			return fmt.Errorf("E_SSE_CHOICES")
		}
		choice := object(choices[0])
		delta := object(choice["delta"])
		if wireFinish != "" && (stringValue(delta["content"]) != "" || stringValue(delta["reasoning_content"]) != "" || len(array(delta["tool_calls"])) > 0 || stringValue(choice["finish_reason"]) != "") {
			return fmt.Errorf("E_SSE_AFTER_FINISH")
		}
		if s := stringValue(delta["reasoning_content"]); s != "" {
			if e := emitReason(s); e != nil {
				return e
			}
		}
		if s := stringValue(delta["content"]); s != "" {
			if result.Tool != nil || resultSize(result)+len(s) > limits.MaxResponseBytes {
				return fmt.Errorf("E_SSE_CONTENT")
			}
			if e := closePortable(); e != nil {
				return e
			}
			result.Text += s
			if e := output(core.Event{Kind: "text_delta", Text: s}); e != nil {
				return e
			}
		}
		calls := array(delta["tool_calls"])
		if len(calls) > 1 {
			return fmt.Errorf("E_SSE_TOOLS")
		}
		if len(calls) == 1 {
			if result.Text != "" || portableStarted {
				return fmt.Errorf("E_SSE_MIXED_OUTPUT")
			}
			call := object(calls[0])
			if idx, ok := integer(call["index"]); !ok || idx != 0 {
				return fmt.Errorf("E_SSE_TOOL_INDEX")
			}
			f := object(call["function"])
			if result.Tool == nil {
				id, name := stringValue(call["id"]), stringValue(f["name"])
				if id == "" || name == "" {
					return fmt.Errorf("E_SSE_TOOL_START")
				}
				result.Tool = &core.ToolCall{ID: id, Name: name}
				if e := validateTool(r, result.Tool, false); e != nil {
					return e
				}
				if e := output(core.Event{Kind: "tool_start", Tool: result.Tool}); e != nil {
					return e
				}
			} else if id, name := stringValue(call["id"]), stringValue(f["name"]); id != "" && id != result.Tool.ID || name != "" && name != result.Tool.Name {
				return fmt.Errorf("E_SSE_TOOL_IDENTITY")
			}
			s := stringValue(f["arguments"])
			if resultSize(result)+len(s) > limits.MaxResponseBytes {
				return fmt.Errorf("E_SSE_SIZE")
			}
			result.Tool.Arguments += s
			if s != "" {
				if e := output(core.Event{Kind: "tool_args_delta", Text: s}); e != nil {
					return e
				}
			}
		}
		if f := stringValue(choice["finish_reason"]); f != "" {
			wireFinish = f
		}
		return nil
	})
	if err != nil && !errors.Is(err, io.EOF) {
		if ce, ok := err.(*core.Error); ok {
			return ce
		}
		if ctx.Err() != nil || timeout.Load() {
			return core.Err("upstream_timeout", 504, "Upstream event stream timed out")
		}
		return core.Err("upstream_bad_stream", 502, "Invalid upstream event stream")
	}
	if !done || wireFinish == "" {
		if ctx.Err() != nil || timeout.Load() {
			return core.Err("upstream_timeout", 504, "Upstream event stream timed out")
		}
		return core.Err("upstream_incomplete_stream", 502, "Upstream event stream ended before its terminal marker")
	}
	result.Finish = wireFinish
	if e := validateResult(r, result); e != nil {
		return e
	}
	if result.Tool != nil {
		if e := output(core.Event{Kind: "tool_done"}); e != nil {
			return e
		}
	}
	if e := closePortable(); e != nil {
		return e
	}
	finishUsage()
	return output(core.Event{Kind: "finish", Finish: wireFinish, Usage: result.Usage})
}

func resultSize(v core.Result) int {
	n := len(v.Text) + len(v.Reasoning)
	if v.Tool != nil {
		n += len(v.Tool.ID) + len(v.Tool.Name) + len(v.Tool.Arguments)
	}
	return n
}

func validateTool(r *core.Request, call *core.ToolCall, complete bool) *core.Error {
	if r.ToolChoice == "none" || r.ToolChoice != "auto" && r.ToolChoice != "required" && r.ToolChoice != call.Name {
		return core.Err("upstream_constraint_failed", 502, "Upstream function does not match tool choice")
	}
	for _, tool := range r.Tools {
		if tool.Name != call.Name {
			continue
		}
		if complete {
			var args map[string]any
			if json.Unmarshal([]byte(call.Arguments), &args) != nil || args == nil || len(tool.Schema) > 0 && schema.Validate(tool.Schema, args) != nil {
				return core.Err("upstream_constraint_failed", 502, "Upstream function arguments do not satisfy the declaration")
			}
		}
		return nil
	}
	return core.Err("upstream_constraint_failed", 502, "Upstream function was not declared")
}

func validateResult(r *core.Request, result core.Result) *core.Error {
	switch result.Finish {
	case "stop", "length", "content_filter":
		if result.Tool != nil {
			return core.Err("upstream_bad_response", 502, "Upstream function is missing its finish reason")
		}
	case "tool_calls":
		if result.Tool == nil || result.Text != "" {
			return core.Err("upstream_bad_response", 502, "Upstream function finish is inconsistent with its output")
		}
		if e := validateTool(r, result.Tool, true); e != nil {
			return e
		}
	default:
		return core.Err("upstream_bad_response", 502, "Unsupported upstream finish reason")
	}
	if r.Protocol == core.Anthropic && result.Finish == "content_filter" {
		return core.Err("upstream_content_filtered", 502, "Upstream refusal cannot be translated to this protocol")
	}
	// Native reasoning is separate from JSON content. Portable reasoning is
	// rejected in Preflight when a constrained format is requested.
	result.Reasoning = ""
	if e := preset.CheckConstraints(r, result); e != nil {
		return core.Err("upstream_constraint_failed", 502, "Upstream did not satisfy output constraints")
	}
	return nil
}
func object(v any) map[string]any { m, _ := v.(map[string]any); return m }
func array(v any) []any           { a, _ := v.([]any); return a }
func stringValue(v any) string    { s, _ := v.(string); return s }
func integer(v any) (int, bool) {
	n, ok := v.(float64)
	if ok && n >= 0 && n <= 1e9 && n == float64(int(n)) {
		return int(n), true
	}
	return 0, false
}
func ReadSSE(reader io.Reader, max int, consume func(string) error) error {
	r := bufio.NewReaderSize(reader, 16<<10)
	var data []string
	size := 0
	for {
		var line []byte
		var end error
		for {
			part, e := r.ReadSlice('\n')
			line = append(line, part...)
			if len(line) > max {
				return fmt.Errorf("E_SSE_SIZE")
			}
			if e == bufio.ErrBufferFull {
				continue
			}
			end = e
			break
		}
		s := strings.TrimSuffix(strings.TrimSuffix(string(line), "\n"), "\r")
		if s == "" {
			if len(data) > 0 {
				e := consume(strings.Join(data, "\n"))
				if e != nil {
					return e
				}
				data = nil
				size = 0
			}
		} else if strings.HasPrefix(s, "data:") {
			v := strings.TrimPrefix(s, "data:")
			v = strings.TrimPrefix(v, " ")
			size += len(v) + 1
			if size > max {
				return fmt.Errorf("E_SSE_SIZE")
			}
			data = append(data, v)
		}
		if end != nil {
			if end == io.EOF && len(data) > 0 {
				if e := consume(strings.Join(data, "\n")); e != nil {
					return e
				}
			}
			return end
		}
	}
}
