package protocol

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"fakeapi/internal/core"
)

type M = map[string]any

func Usage(p core.Protocol, u core.Usage) M {
	output := u.Visible + u.Reasoning
	switch p {
	case core.Chat:
		return M{"prompt_tokens": u.Input, "completion_tokens": output, "total_tokens": u.Input + output, "prompt_tokens_details": M{"cached_tokens": u.Cached}, "completion_tokens_details": M{"reasoning_tokens": u.Reasoning}}
	case core.Responses:
		return M{"input_tokens": u.Input, "output_tokens": output, "total_tokens": u.Input + output, "input_tokens_details": M{"cached_tokens": u.Cached}, "output_tokens_details": M{"reasoning_tokens": u.Reasoning}}
	case core.Anthropic:
		return M{"input_tokens": u.Input, "output_tokens": output, "cache_creation_input_tokens": 0, "cache_read_input_tokens": u.Cached}
	default:
		return M{"promptTokenCount": u.Input, "candidatesTokenCount": u.Visible, "thoughtsTokenCount": u.Reasoning, "totalTokenCount": u.Input + output, "cachedContentTokenCount": u.Cached}
	}
}
func errorType(status int) string {
	switch status {
	case 400, 404, 413:
		return "invalid_request_error"
	case 401:
		return "authentication_error"
	case 403:
		return "permission_error"
	case 429:
		return "rate_limit_error"
	default:
		return "server_error"
	}
}
func ErrorBody(p core.Protocol, e *core.Error) M {
	switch p {
	case core.Anthropic:
		t := errorType(e.Status)
		if e.Status == 404 {
			t = "not_found_error"
		}
		if e.Status == 413 {
			t = "request_too_large"
		}
		if e.Status >= 500 {
			t = "api_error"
		}
		if e.Status == 504 {
			t = "timeout_error"
		}
		return M{"type": "error", "error": M{"type": t, "message": e.Message}, "request_id": core.ID("req_fake_")}
	case core.Gemini:
		s := "INTERNAL"
		switch e.Status {
		case 400:
			s = "INVALID_ARGUMENT"
		case 401:
			s = "UNAUTHENTICATED"
		case 403:
			s = "PERMISSION_DENIED"
		case 404:
			s = "NOT_FOUND"
		case 413, 429:
			s = "RESOURCE_EXHAUSTED"
		case 502, 503:
			s = "UNAVAILABLE"
		case 504:
			s = "DEADLINE_EXCEEDED"
		}
		return M{"error": M{"code": e.Status, "message": e.Message, "status": s}}
	default:
		var param any
		if e.Param != "" {
			param = e.Param
		}
		return M{"error": M{"type": errorType(e.Status), "message": e.Message, "code": e.Code, "param": param}}
	}
}
func WriteError(w http.ResponseWriter, p core.Protocol, e *core.Error) {
	if e.RetryAfter > 0 {
		w.Header().Set("Retry-After", fmt.Sprint(e.RetryAfter))
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(e.Status)
	_ = json.NewEncoder(w).Encode(ErrorBody(p, e))
}
func messageItem(result core.Result, status string) M {
	return M{"id": "msg_" + result.ID, "type": "message", "role": "assistant", "status": status, "content": []any{M{"type": "output_text", "text": result.Text, "annotations": []any{}, "logprobs": []any{}}}}
}
func functionItem(result core.Result, status string) M {
	return M{"id": "fc_" + result.ID, "type": "function_call", "status": status, "call_id": result.Tool.ID, "name": result.Tool.Name, "arguments": result.Tool.Arguments}
}
func reasoningItem(result core.Result) M {
	return M{"id": "rs_" + result.ID, "type": "reasoning", "summary": []any{M{"type": "summary_text", "text": result.Reasoning}}}
}
func ResponseObject(r *core.Request, result core.Result, created int64, status string) M {
	output := []any{}
	if status != "in_progress" {
		if result.Reasoning != "" {
			output = append(output, reasoningItem(result))
		}
		if result.Tool != nil {
			output = append(output, functionItem(result, "completed"))
		} else {
			output = append(output, messageItem(result, "completed"))
		}
	}
	var incomplete any
	if status != "in_progress" && status != "failed" && (result.Finish == "length" || result.Finish == "content_filter") {
		status = "incomplete"
		reason := "max_output_tokens"
		if result.Finish == "content_filter" {
			reason = "content_filter"
		}
		incomplete = M{"reason": reason}
	}
	var usage any
	if status != "in_progress" {
		usage = Usage(core.Responses, result.Usage)
	}
	return M{"id": result.ID, "object": "response", "created_at": created, "status": status, "completed_at": nil, "error": nil, "incomplete_details": incomplete, "model": r.Model, "output": output, "usage": usage, "instructions": nil, "max_output_tokens": r.MaxTokens, "parallel_tool_calls": false, "previous_response_id": nil, "reasoning": M{"effort": nil, "summary": nil}, "store": false, "temperature": 1, "text": M{"format": M{"type": "text"}}, "tool_choice": "auto", "tools": []any{}, "top_p": 1, "truncation": "disabled", "metadata": M{}, "user": nil, "service_tier": "default"}
}
func Body(r *core.Request, result core.Result, created int64) M {
	switch r.Protocol {
	case core.Chat:
		message := M{"role": "assistant", "content": result.Text, "refusal": nil}
		if result.Reasoning != "" {
			message["reasoning_content"] = result.Reasoning
		}
		if result.Tool != nil {
			message["content"] = nil
			message["tool_calls"] = []any{M{"id": result.Tool.ID, "type": "function", "function": M{"name": result.Tool.Name, "arguments": result.Tool.Arguments}}}
		}
		return M{"id": result.ID, "object": "chat.completion", "created": created, "model": r.Model, "choices": []any{M{"index": 0, "message": message, "finish_reason": result.Finish, "logprobs": nil}}, "usage": Usage(r.Protocol, result.Usage)}
	case core.Responses:
		return ResponseObject(r, result, created, "completed")
	case core.Anthropic:
		content := []any{}
		if result.Tool != nil {
			content = append(content, M{"type": "tool_use", "id": result.Tool.ID, "name": result.Tool.Name, "input": objFromJSON(result.Tool.Arguments)})
		} else {
			content = append(content, M{"type": "text", "text": result.Text})
		}
		reason := anthropicFinish(result.Finish, result.Stop)
		var stop any
		if result.Stop != "" {
			stop = result.Stop
		}
		return M{"id": result.ID, "type": "message", "role": "assistant", "model": r.Model, "content": content, "stop_reason": reason, "stop_sequence": stop, "usage": Usage(r.Protocol, result.Usage)}
	default:
		parts := []any{}
		if result.Reasoning != "" {
			parts = append(parts, M{"text": result.Reasoning, "thought": true})
		}
		if result.Tool != nil {
			parts = append(parts, M{"functionCall": M{"id": result.Tool.ID, "name": result.Tool.Name, "args": objFromJSON(result.Tool.Arguments)}})
		} else {
			parts = append(parts, M{"text": result.Text})
		}
		return M{"candidates": []any{M{"index": 0, "content": M{"role": "model", "parts": parts}, "finishReason": geminiFinish(result.Finish)}}, "usageMetadata": Usage(r.Protocol, result.Usage), "modelVersion": r.Model, "responseId": result.ID}
	}
}
func anthropicFinish(f, stop string) string {
	if f == "tool_calls" {
		return "tool_use"
	}
	if f == "length" {
		return "max_tokens"
	}
	if stop != "" {
		return "stop_sequence"
	}
	return "end_turn"
}
func geminiFinish(f string) string {
	if f == "length" {
		return "MAX_TOKENS"
	}
	if f == "content_filter" {
		return "SAFETY"
	}
	return "STOP"
}

// Stream has one terminal path. Start events are delayed until a semantic event.
type Stream struct {
	W          http.ResponseWriter
	Request    *core.Request
	Result     core.Result
	Created    int64
	Committed  bool
	Finished   bool
	seq        int
	index      int
	textOpen   bool
	reasonOpen bool
	toolOpen   bool
	deadline   time.Duration
}

func NewStream(w http.ResponseWriter, r *core.Request, id string, d time.Duration) *Stream {
	return &Stream{W: w, Request: r, Result: core.Result{ID: id, Finish: "stop"}, Created: time.Now().Unix(), deadline: d}
}
func (s *Stream) wire(name string, data any) error {
	_ = http.NewResponseController(s.W).SetWriteDeadline(time.Now().Add(s.deadline))
	b, e := json.Marshal(data)
	if e != nil {
		return e
	}
	var line string
	if s.Request.Protocol == core.Responses {
		m := obj(data)
		m["type"] = name
		m["sequence_number"] = s.seq
		s.seq++
		b, e = json.Marshal(m)
		if e != nil {
			return e
		}
	}
	if name != "" && (s.Request.Protocol == core.Responses || s.Request.Protocol == core.Anthropic) {
		line = "event: " + name + "\n"
	}
	line += "data: " + string(b) + "\n\n"
	if _, e = fmt.Fprint(s.W, line); e != nil {
		return e
	}
	return http.NewResponseController(s.W).Flush()
}
func (s *Stream) chat(delta M, finish any, usage any) error {
	return s.wire("", M{"id": s.Result.ID, "object": "chat.completion.chunk", "created": s.Created, "model": s.Request.Model, "choices": []any{M{"index": 0, "delta": delta, "finish_reason": finish, "logprobs": nil}}, "usage": usage})
}
func (s *Stream) begin() error {
	if s.Committed {
		return nil
	}
	s.W.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	s.W.Header().Set("Cache-Control", "no-cache, no-transform")
	s.W.Header().Set("X-Accel-Buffering", "no")
	s.W.WriteHeader(200)
	s.Committed = true
	switch s.Request.Protocol {
	case core.Chat:
		return s.chat(M{"role": "assistant", "content": ""}, nil, nil)
	case core.Responses:
		o := ResponseObject(s.Request, s.Result, s.Created, "in_progress")
		if e := s.wire("response.created", M{"response": o}); e != nil {
			return e
		}
		return s.wire("response.in_progress", M{"response": o})
	case core.Anthropic:
		o := Body(s.Request, s.Result, s.Created)
		o["content"] = []any{}
		o["stop_reason"] = nil
		o["usage"] = Usage(core.Anthropic, core.Usage{Input: core.InputUsage(s.Request)})
		return s.wire("message_start", M{"type": "message_start", "message": o})
	}
	return nil
}
func (s *Stream) openText() error {
	if s.textOpen {
		return nil
	}
	s.textOpen = true
	switch s.Request.Protocol {
	case core.Responses:
		item := messageItem(core.Result{ID: s.Result.ID}, "in_progress")
		item["content"] = []any{}
		if e := s.wire("response.output_item.added", M{"output_index": s.index, "item": item}); e != nil {
			return e
		}
		return s.wire("response.content_part.added", M{"output_index": s.index, "content_index": 0, "item_id": "msg_" + s.Result.ID, "part": M{"type": "output_text", "text": "", "annotations": []any{}, "logprobs": []any{}}})
	case core.Anthropic:
		return s.wire("content_block_start", M{"type": "content_block_start", "index": 0, "content_block": M{"type": "text", "text": ""}})
	}
	return nil
}
func (s *Stream) Emit(e core.Event) error {
	if s.Finished {
		return fmt.Errorf("E_STREAM_TERMINAL")
	}
	if err := s.begin(); err != nil {
		return err
	}
	switch e.Kind {
	case "reasoning_delta":
		s.Result.Reasoning += e.Text
		switch s.Request.Protocol {
		case core.Chat:
			return s.chat(M{"reasoning_content": e.Text}, nil, nil)
		case core.Gemini:
			return s.geminiParts([]any{M{"text": e.Text, "thought": true}})
		case core.Responses:
			if !s.reasonOpen {
				s.reasonOpen = true
				if err := s.wire("response.output_item.added", M{"output_index": 0, "item": M{"id": "rs_" + s.Result.ID, "type": "reasoning", "summary": []any{}}}); err != nil {
					return err
				}
				if err := s.wire("response.reasoning_summary_part.added", M{"output_index": 0, "summary_index": 0, "item_id": "rs_" + s.Result.ID, "part": M{"type": "summary_text", "text": ""}}); err != nil {
					return err
				}
			}
			return s.wire("response.reasoning_summary_text.delta", M{"output_index": 0, "summary_index": 0, "item_id": "rs_" + s.Result.ID, "delta": e.Text})
		}
	case "text_delta":
		if err := s.closeReason(); err != nil {
			return err
		}
		if err := s.openText(); err != nil {
			return err
		}
		s.Result.Text += e.Text
		switch s.Request.Protocol {
		case core.Chat:
			return s.chat(M{"content": e.Text}, nil, nil)
		case core.Responses:
			return s.wire("response.output_text.delta", M{"output_index": s.index, "content_index": 0, "item_id": "msg_" + s.Result.ID, "delta": e.Text, "logprobs": []any{}})
		case core.Anthropic:
			return s.wire("content_block_delta", M{"type": "content_block_delta", "index": 0, "delta": M{"type": "text_delta", "text": e.Text}})
		default:
			return s.geminiParts([]any{M{"text": e.Text}})
		}
	case "tool_start":
		if err := s.closeReason(); err != nil {
			return err
		}
		s.toolOpen = true
		t := *e.Tool
		t.Arguments = ""
		s.Result.Tool = &t
		switch s.Request.Protocol {
		case core.Chat:
			return s.chat(M{"tool_calls": []any{M{"index": 0, "id": t.ID, "type": "function", "function": M{"name": t.Name, "arguments": ""}}}}, nil, nil)
		case core.Responses:
			return s.wire("response.output_item.added", M{"output_index": s.index, "item": functionItem(s.Result, "in_progress")})
		case core.Anthropic:
			return s.wire("content_block_start", M{"type": "content_block_start", "index": 0, "content_block": M{"type": "tool_use", "id": t.ID, "name": t.Name, "input": M{}}})
		}
	case "tool_args_delta":
		if s.Result.Tool == nil {
			return fmt.Errorf("E_STREAM_TOOL")
		}
		s.Result.Tool.Arguments += e.Text
		switch s.Request.Protocol {
		case core.Chat:
			return s.chat(M{"tool_calls": []any{M{"index": 0, "function": M{"arguments": e.Text}}}}, nil, nil)
		case core.Responses:
			return s.wire("response.function_call_arguments.delta", M{"output_index": s.index, "item_id": "fc_" + s.Result.ID, "delta": e.Text})
		case core.Anthropic:
			return s.wire("content_block_delta", M{"type": "content_block_delta", "index": 0, "delta": M{"type": "input_json_delta", "partial_json": e.Text}})
		}
	case "tool_done":
		if s.Request.Protocol == core.Gemini {
			return s.geminiParts([]any{M{"functionCall": M{"id": s.Result.Tool.ID, "name": s.Result.Tool.Name, "args": objFromJSON(s.Result.Tool.Arguments)}}})
		}
	case "finish":
		s.Result.Usage = e.Usage
		s.Result.Finish = e.Finish
		s.Result.Stop = e.Stop
		return s.finish()
	}
	return nil
}
func (s *Stream) closeReason() error {
	if !s.reasonOpen {
		return nil
	}
	s.reasonOpen = false
	s.index = 1
	for _, v := range []struct {
		name  string
		value M
	}{{"response.reasoning_summary_text.done", M{"output_index": 0, "summary_index": 0, "item_id": "rs_" + s.Result.ID, "text": s.Result.Reasoning}}, {"response.reasoning_summary_part.done", M{"output_index": 0, "summary_index": 0, "item_id": "rs_" + s.Result.ID, "part": M{"type": "summary_text", "text": s.Result.Reasoning}}}, {"response.output_item.done", M{"output_index": 0, "item": reasoningItem(s.Result)}}} {
		if e := s.wire(v.name, v.value); e != nil {
			return e
		}
	}
	return nil
}
func (s *Stream) finish() error {
	if e := s.closeReason(); e != nil {
		return e
	}
	if s.Result.Tool == nil && !s.textOpen {
		if e := s.openText(); e != nil {
			return e
		}
	}
	switch s.Request.Protocol {
	case core.Chat:
		if e := s.chat(M{}, s.Result.Finish, nil); e != nil {
			return e
		}
		if s.Request.IncludeUsage {
			if e := s.wire("", M{"id": s.Result.ID, "object": "chat.completion.chunk", "created": s.Created, "model": s.Request.Model, "choices": []any{}, "usage": Usage(core.Chat, s.Result.Usage)}); e != nil {
				return e
			}
		}
		if _, e := fmt.Fprint(s.W, "data: [DONE]\n\n"); e != nil {
			return e
		}
		if e := http.NewResponseController(s.W).Flush(); e != nil {
			return e
		}
	case core.Responses:
		if s.Result.Tool != nil {
			if e := s.wire("response.function_call_arguments.done", M{"output_index": s.index, "item_id": "fc_" + s.Result.ID, "arguments": s.Result.Tool.Arguments, "name": s.Result.Tool.Name}); e != nil {
				return e
			}
			if e := s.wire("response.output_item.done", M{"output_index": s.index, "item": functionItem(s.Result, "completed")}); e != nil {
				return e
			}
		} else {
			part := M{"type": "output_text", "text": s.Result.Text, "annotations": []any{}, "logprobs": []any{}}
			if e := s.wire("response.output_text.done", M{"output_index": s.index, "content_index": 0, "item_id": "msg_" + s.Result.ID, "text": s.Result.Text, "logprobs": []any{}}); e != nil {
				return e
			}
			if e := s.wire("response.content_part.done", M{"output_index": s.index, "content_index": 0, "item_id": "msg_" + s.Result.ID, "part": part}); e != nil {
				return e
			}
			if e := s.wire("response.output_item.done", M{"output_index": s.index, "item": messageItem(s.Result, "completed")}); e != nil {
				return e
			}
		}
		name := "response.completed"
		if s.Result.Finish == "length" || s.Result.Finish == "content_filter" {
			name = "response.incomplete"
		}
		if e := s.wire(name, M{"response": ResponseObject(s.Request, s.Result, s.Created, "completed")}); e != nil {
			return e
		}
	case core.Anthropic:
		if e := s.wire("content_block_stop", M{"type": "content_block_stop", "index": 0}); e != nil {
			return e
		}
		var stop any
		if s.Result.Stop != "" {
			stop = s.Result.Stop
		}
		if e := s.wire("message_delta", M{"type": "message_delta", "delta": M{"stop_reason": anthropicFinish(s.Result.Finish, s.Result.Stop), "stop_sequence": stop}, "usage": M{"output_tokens": s.Result.Usage.Visible + s.Result.Usage.Reasoning}}); e != nil {
			return e
		}
		if e := s.wire("message_stop", M{"type": "message_stop"}); e != nil {
			return e
		}
	case core.Gemini:
		if e := s.wire("", M{"candidates": []any{M{"index": 0, "finishReason": geminiFinish(s.Result.Finish)}}, "usageMetadata": Usage(core.Gemini, s.Result.Usage), "modelVersion": s.Request.Model, "responseId": s.Result.ID}); e != nil {
			return e
		}
	}
	s.Finished = true
	return nil
}
func (s *Stream) Fail(e *core.Error) {
	if !s.Committed {
		WriteError(s.W, s.Request.Protocol, e)
		return
	}
	if s.Finished {
		return
	}
	switch s.Request.Protocol {
	case core.Responses:
		o := ResponseObject(s.Request, s.Result, s.Created, "failed")
		code := "server_error"
		if e.Status == 429 || e.Status == 503 {
			code = "rate_limit_exceeded"
		}
		o["error"] = M{"code": code, "message": e.Message}
		_ = s.wire("response.failed", M{"response": o})
	case core.Anthropic:
		_ = s.wire("error", ErrorBody(core.Anthropic, e))
	default:
		_ = s.wire("", ErrorBody(s.Request.Protocol, e))
	}
	s.Finished = true
}
func SafeFinish(f string) string {
	switch strings.ToLower(f) {
	case "stop", "length", "tool_calls", "content_filter":
		return f
	default:
		return "stop"
	}
}
func (s *Stream) geminiParts(parts []any) error {
	content := M{"role": "model", "parts": parts}
	candidate := M{"index": 0, "content": content}
	return s.wire("", M{"candidates": []any{candidate}, "modelVersion": s.Request.Model, "responseId": s.Result.ID})
}
