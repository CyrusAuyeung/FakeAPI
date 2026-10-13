package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"fakeapi/internal/config"
	"fakeapi/internal/core"
)

func nativeBlock(block map[string]any) (core.Part, *core.Error) {
	part := core.Part{Kind: stringValue(block["type"])}
	for _, key := range []string{"type", "text", "thinking", "signature", "data", "id", "name"} {
		if value, exists := block[key]; exists {
			if _, ok := value.(string); !ok {
				return part, core.Err("upstream_bad_response", 502, "Invalid upstream block field")
			}
		}
	}
	part.Text, part.Signature, part.Data = stringValue(block["thinking"]), stringValue(block["signature"]), stringValue(block["data"])
	part.ID, part.Name = stringValue(block["id"]), stringValue(block["name"])
	switch part.Kind {
	case "text":
		if _, ok := block["text"].(string); !ok {
			return part, core.Err("upstream_bad_response", 502, "Invalid upstream text block")
		}
	case "thinking":
		if _, ok := block["thinking"].(string); !ok {
			return part, core.Err("upstream_bad_response", 502, "Invalid upstream thinking block")
		}
	case "redacted_thinking":
		if part.Data == "" {
			return part, core.Err("upstream_bad_response", 502, "Invalid upstream redacted thinking")
		}
	case "tool_use":
		if part.ID == "" || part.Name == "" {
			return part, core.Err("upstream_bad_response", 502, "Invalid upstream tool block")
		}
	default:
		return part, core.Err("upstream_bad_response", 502, "Unsupported upstream block")
	}
	return part, nil
}

func translateAnthropic(r *core.Request, t config.Target) (map[string]any, *core.Error) {
	controls, e := reasoningParameters(r, t)
	if e != nil {
		return nil, e
	}
	maximum := 8192
	if r.MaxTokens != nil {
		maximum = *r.MaxTokens
	}
	body := map[string]any{"model": t.Model.Resolve(), "stream": r.Stream, "max_tokens": maximum}
	messages, system := []any{}, []any{}
	pending := map[string]bool{}
	for _, message := range r.Messages {
		role := message.Role
		if role == "developer" {
			role = "system"
		}
		if role == "system" && len(messages) > 0 {
			return nil, core.Err("unsupported_capability", 400, "Claude requires system instructions before the conversation")
		}
		if role == "tool" {
			role = "user"
		}
		parts := []any{}
		for _, part := range message.Parts {
			switch part.Kind {
			case "text":
				parts = append(parts, map[string]any{"type": "text", "text": part.Text})
			case "image":
				source := map[string]any{"type": "url", "url": part.URL}
				if part.Data != "" {
					source = map[string]any{"type": "base64", "media_type": part.MIME, "data": part.Data}
				}
				parts = append(parts, map[string]any{"type": "image", "source": source})
			case "thinking":
				parts = append(parts, map[string]any{"type": "thinking", "thinking": part.Text, "signature": part.Signature})
			case "redacted_thinking":
				parts = append(parts, map[string]any{"type": "redacted_thinking", "data": part.Data})
			case "tool_call":
				if len(pending) != 0 {
					return nil, core.Err("unsupported_tool_sequence", 400, "Only one outstanding tool call is supported")
				}
				pending[part.ID] = true
				var input map[string]any
				if json.Unmarshal(part.Arguments, &input) != nil || input == nil {
					return nil, core.Invalid("tool arguments")
				}
				parts = append(parts, map[string]any{"type": "tool_use", "id": part.ID, "name": part.Name, "input": input})
			case "tool_result":
				if !pending[part.ID] {
					return nil, core.Err("unsupported_tool_sequence", 400, "Tool result does not match a pending call")
				}
				delete(pending, part.ID)
				parts = append(parts, map[string]any{"type": "tool_result", "tool_use_id": part.ID, "content": part.Text})
			}
		}
		if role == "system" {
			system = append(system, parts...)
		} else if len(parts) > 0 {
			messages = append(messages, map[string]any{"role": role, "content": parts})
		}
	}
	if len(pending) != 0 {
		return nil, core.Err("unsupported_tool_sequence", 400, "Tool call is missing its result")
	}
	body["messages"] = messages
	if len(system) > 0 {
		body["system"] = system
	}
	for key, value := range r.Parameters {
		switch key {
		case "reasoning_effort":
		case "temperature", "top_p":
			body[key] = value
		default:
			return nil, core.Err("unsupported_capability", 400, "Parameter has no Claude equivalent")
		}
	}
	for key, value := range controls {
		body[key] = value
	}
	if len(r.Stop) > 0 {
		body["stop_sequences"] = r.Stop
	}
	if len(r.Tools) > 0 {
		tools := []any{}
		for _, tool := range r.Tools {
			tools = append(tools, map[string]any{"name": tool.Name, "description": tool.Description, "input_schema": tool.Schema})
		}
		body["tools"] = tools
		choice := map[string]any{"type": r.ToolChoice}
		if r.ToolChoice == "required" {
			choice["type"] = "any"
		} else if r.ToolChoice != "auto" && r.ToolChoice != "none" {
			choice = map[string]any{"type": "tool", "name": r.ToolChoice}
		}
		choice["disable_parallel_tool_use"] = true
		body["tool_choice"] = choice
	}
	if r.Constraint.Kind != "" {
		format := map[string]any{"type": "json_schema", "schema": r.Constraint.Schema}
		if r.Constraint.Kind == "json_object" {
			format["schema"] = map[string]any{"type": "object", "additionalProperties": true}
		}
		output, _ := body["output_config"].(map[string]any)
		if output == nil {
			output = map[string]any{}
		}
		output["format"] = format
		body["output_config"] = output
	}
	return body, nil
}

func (c *Client) runAnthropic(ctx context.Context, r *core.Request, t config.Target, u config.Upstream, limits config.Server, emit func(core.Event) error) *core.Error {
	body, e := translateAnthropic(r, t)
	if e != nil {
		return e
	}
	encoded, _ := json.Marshal(body)
	sub, cancel := context.WithCancel(ctx)
	defer cancel()
	var timedOut atomic.Bool
	var firstState atomic.Uint32
	first := time.AfterFunc(u.Timeouts.FirstEvent.Value(), func() {
		if firstState.CompareAndSwap(0, 2) {
			timedOut.Store(true)
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
	req, err := http.NewRequestWithContext(sub, "POST", strings.TrimRight(u.BaseURL.Resolve(), "/")+"/messages", bytes.NewReader(encoded))
	if err != nil {
		return core.Err("upstream_request_error", 502, "Unable to prepare upstream request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", os.Getenv(u.APIKeyEnv))
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if timedOut.Load() || ctx.Err() != nil {
			return core.Err("upstream_timeout", 504, "Upstream response timed out")
		}
		return core.Err("upstream_connection_error", 502, "Unable to connect to upstream")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return mapped(resp.StatusCode)
	}
	result := core.Result{}
	bytesOut, input, output, cached := 0, 0, 0, 0
	usageProvided, portableOpen := false, false
	push := func(event core.Event) *core.Error {
		if e := markFirst(); e != nil {
			return e
		}
		if err := emit(event); err != nil {
			return core.Err("client_write_failed", 499, "Client disconnected")
		}
		return nil
	}
	count := func(text string) *core.Error {
		bytesOut += len(text)
		if bytesOut > limits.MaxResponseBytes {
			return core.Err("upstream_response_too_large", 502, "Upstream output exceeds configured limit")
		}
		return nil
	}
	reason := func(text string) *core.Error {
		if e := count(text); e != nil {
			return e
		}
		if result.Text != "" || result.Tool != nil {
			return core.Err("upstream_bad_response", 502, "Upstream reasoning appeared after the response body")
		}
		result.Reasoning += text
		if text == "" {
			return nil
		}
		if e := markFirst(); e != nil {
			return e
		}
		if r.ThinkingOff {
			return nil
		}
		if r.Protocol == core.Chat {
			return push(core.Event{Kind: "reasoning_delta", Text: text})
		}
		if r.Protocol != core.Anthropic && u.ReasoningTranslation == "portable_text" {
			prefix := ""
			if !portableOpen {
				prefix, portableOpen = "[上游推理] ", true
			}
			return push(core.Event{Kind: "text_delta", Text: prefix + text})
		}
		return nil
	}
	closePortable := func() *core.Error {
		if portableOpen {
			portableOpen = false
			return push(core.Event{Kind: "text_delta", Text: "\n\n"})
		}
		return nil
	}
	text := func(value string) *core.Error {
		if e := count(value); e != nil {
			return e
		}
		if result.Tool != nil {
			return core.Err("upstream_bad_response", 502, "Mixed text and tool output is unsupported")
		}
		result.Text += value
		if value == "" {
			return nil
		}
		if e := closePortable(); e != nil {
			return e
		}
		return push(core.Event{Kind: "text_delta", Text: value})
	}
	readUsage := func(value map[string]any) {
		if n, ok := integer(value["input_tokens"]); ok {
			input = n
			usageProvided = true
		}
		if n, ok := integer(value["cache_read_input_tokens"]); ok {
			cached = n
		}
		if n, ok := integer(value["cache_creation_input_tokens"]); ok {
			input += n
		}
		if n, ok := integer(value["output_tokens"]); ok {
			output = n
		}
	}
	finish := func(stop, sequence string) *core.Error {
		switch stop {
		case "end_turn", "stop_sequence":
			result.Finish = "stop"
		case "max_tokens":
			result.Finish = "length"
		case "tool_use":
			result.Finish = "tool_calls"
		case "refusal":
			result.Finish = "content_filter"
		default:
			return core.Err("upstream_bad_response", 502, "Unsupported upstream stop reason")
		}
		result.Stop = sequence
		return nil
	}
	partDone := func(part core.Part, arguments string) *core.Error {
		switch part.Kind {
		case "thinking":
			if part.Signature == "" {
				return core.Err("upstream_bad_response", 502, "Upstream thinking signature is missing")
			}
			if !r.ThinkingOff && r.Protocol == core.Anthropic {
				return push(core.Event{Kind: "thinking_block", Part: &part})
			}
		case "redacted_thinking":
			if part.Data == "" {
				return core.Err("upstream_bad_response", 502, "Invalid redacted thinking block")
			}
			if !r.ThinkingOff && r.Protocol == core.Anthropic {
				return push(core.Event{Kind: "thinking_block", Part: &part})
			}
		case "tool_use":
			if result.Tool != nil || result.Text != "" {
				return core.Err("upstream_bad_response", 502, "Only one function without accompanying text is supported")
			}
			call := &core.ToolCall{ID: part.ID, Name: part.Name, Arguments: arguments}
			if call.ID == "" || validateTool(r, call, true) != nil {
				return core.Err("upstream_constraint_failed", 502, "Upstream function does not satisfy the declaration")
			}
			result.Tool = call
			if e := push(core.Event{Kind: "tool_start", Tool: call}); e != nil {
				return e
			}
			if e := push(core.Event{Kind: "tool_args_delta", Text: arguments}); e != nil {
				return e
			}
			return push(core.Event{Kind: "tool_done"})
		}
		return nil
	}
	if !r.Stream {
		raw, err := io.ReadAll(io.LimitReader(resp.Body, int64(limits.MaxResponseBytes)+1))
		if err != nil {
			return core.Err("upstream_timeout", 504, "Unable to read upstream response")
		}
		if len(raw) > limits.MaxResponseBytes {
			return core.Err("upstream_response_too_large", 502, "Upstream output exceeds configured limit")
		}
		var value map[string]any
		if json.Unmarshal(raw, &value) != nil || value["type"] != "message" || value["role"] != "assistant" {
			return core.Err("upstream_bad_response", 502, "Invalid upstream message")
		}
		readUsage(object(value["usage"]))
		if e := finish(stringValue(value["stop_reason"]), stringValue(value["stop_sequence"])); e != nil {
			return e
		}
		blocks, valid := value["content"].([]any)
		if !valid {
			return core.Err("upstream_bad_response", 502, "Invalid upstream content list")
		}
		for _, item := range blocks {
			block := object(item)
			part, failure := nativeBlock(block)
			if failure != nil {
				return failure
			}
			switch part.Kind {
			case "text":
				if e := text(stringValue(block["text"])); e != nil {
					return e
				}
			case "thinking":
				part.Text = stringValue(block["thinking"])
				if e := reason(part.Text); e != nil {
					return e
				}
			case "redacted_thinking", "tool_use":
			default:
				return core.Err("upstream_bad_response", 502, "Unsupported upstream content block")
			}
			if e := count(part.Signature + part.Data + part.ID + part.Name); e != nil {
				return e
			}
			arguments := ""
			if part.Kind == "tool_use" {
				raw, _ := json.Marshal(block["input"])
				arguments = string(raw)
				if e := count(arguments); e != nil {
					return e
				}
			}
			if e := partDone(part, arguments); e != nil {
				return e
			}
		}
	} else {
		if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
			return core.Err("upstream_bad_response", 502, "Expected upstream event stream")
		}
		idle := time.AfterFunc(u.Timeouts.Idle.Value(), func() { timedOut.Store(true); cancel() })
		defer idle.Stop()
		started, done, open, stopped := false, false, false, false
		index := 0
		part, arguments := core.Part{}, ""
		checked := func(failure *core.Error) error {
			if failure == nil {
				return nil
			}
			return failure
		}
		err := ReadSSE(resp.Body, limits.MaxSSEEventBytes, func(data string) error {
			idle.Reset(u.Timeouts.Idle.Value())
			var value map[string]any
			if json.Unmarshal([]byte(data), &value) != nil {
				return core.Err("upstream_bad_response", 502, "Invalid upstream event")
			}
			switch value["type"] {
			case "ping":
				return nil
			case "message_start":
				message := object(value["message"])
				if started || message["role"] != "assistant" {
					return core.Err("upstream_bad_response", 502, "Invalid upstream message start")
				}
				started = true
				readUsage(object(message["usage"]))
			case "content_block_start":
				i, ok := integer(value["index"])
				if !started || stopped || open || !ok || i != index {
					return core.Err("upstream_bad_response", 502, "Invalid upstream block order")
				}
				open = true
				block := object(value["content_block"])
				var failure *core.Error
				part, failure = nativeBlock(block)
				if failure != nil {
					return failure
				}
				arguments = ""
				if e := count(part.Signature + part.Data + part.ID + part.Name); e != nil {
					return e
				}
				switch part.Kind {
				case "text":
					return checked(text(stringValue(block["text"])))
				case "thinking":
					return checked(reason(part.Text))
				case "redacted_thinking", "tool_use":
				default:
					return core.Err("upstream_bad_response", 502, "Unsupported upstream block")
				}
			case "content_block_delta":
				i, ok := integer(value["index"])
				if !open || !ok || i != index {
					return core.Err("upstream_bad_response", 502, "Invalid upstream block delta")
				}
				delta := object(value["delta"])
				for _, key := range []string{"type", "text", "thinking", "signature", "partial_json"} {
					if value, exists := delta[key]; exists {
						if _, ok := value.(string); !ok {
							return core.Err("upstream_bad_response", 502, "Invalid upstream delta field")
						}
					}
				}
				switch delta["type"] {
				case "text_delta":
					if part.Kind != "text" {
						break
					}
					return checked(text(stringValue(delta["text"])))
				case "thinking_delta":
					if part.Kind != "thinking" {
						break
					}
					value := stringValue(delta["thinking"])
					part.Text += value
					return checked(reason(value))
				case "signature_delta":
					if part.Kind != "thinking" {
						break
					}
					value := stringValue(delta["signature"])
					part.Signature += value
					return checked(count(value))
				case "input_json_delta":
					if part.Kind != "tool_use" {
						break
					}
					value := stringValue(delta["partial_json"])
					arguments += value
					return checked(count(value))
				}
				return core.Err("upstream_bad_response", 502, "Invalid upstream delta type")
			case "content_block_stop":
				i, ok := integer(value["index"])
				if !open || !ok || i != index {
					return core.Err("upstream_bad_response", 502, "Invalid upstream block stop")
				}
				if e := partDone(part, arguments); e != nil {
					return e
				}
				open = false
				index++
			case "message_delta":
				if !started || open || stopped {
					return core.Err("upstream_bad_response", 502, "Invalid upstream message delta")
				}
				delta := object(value["delta"])
				if e := finish(stringValue(delta["stop_reason"]), stringValue(delta["stop_sequence"])); e != nil {
					return e
				}
				readUsage(object(value["usage"]))
				stopped = true
			case "message_stop":
				if !started || !stopped || open {
					return core.Err("upstream_bad_response", 502, "Upstream stream ended before completion")
				}
				done = true
				return io.EOF
			default:
				return core.Err("upstream_bad_response", 502, "Upstream returned an error or unsupported event")
			}
			return nil
		})
		if timedOut.Load() || sub.Err() != nil {
			return core.Err("upstream_timeout", 504, "Upstream response timed out")
		}
		if err != nil && !errors.Is(err, io.EOF) {
			if failure, ok := err.(*core.Error); ok {
				return failure
			}
			return core.Err("upstream_bad_response", 502, "Invalid upstream event stream")
		}
		if !done {
			return core.Err("upstream_bad_response", 502, "Upstream stream ended before completion")
		}
	}
	if e := validateResult(r, result); e != nil {
		return e
	}
	if e := closePortable(); e != nil {
		return e
	}
	reasonTokens := core.Estimate(result.Reasoning)
	if reasonTokens > output {
		reasonTokens = output
	}
	usage := core.Usage{Input: input + cached, Visible: output - reasonTokens, Reasoning: reasonTokens, Cached: cached, Source: "mixed"}
	if result.Reasoning == "" {
		usage.Source = "upstream"
	}
	if !usageProvided {
		usage = core.Usage{Input: core.InputUsage(r), Visible: core.Estimate(result.Text), Reasoning: core.Estimate(result.Reasoning), Source: "estimated"}
	}
	if r.Protocol != core.Chat && r.Protocol != core.Anthropic && u.ReasoningTranslation == "portable_text" && !r.ThinkingOff {
		usage.Visible += usage.Reasoning
		usage.Reasoning = 0
	}
	return push(core.Event{Kind: "finish", Finish: result.Finish, Stop: result.Stop, Usage: usage})
}
