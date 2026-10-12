package protocol

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"fakeapi/internal/core"
)

func events(t *testing.T, p core.Protocol, body string) []map[string]any {
	t.Helper()
	var result []map[string]any
	for _, block := range strings.Split(strings.TrimSpace(body), "\n\n") {
		name, data := "", ""
		for _, line := range strings.Split(block, "\n") {
			if strings.HasPrefix(line, "event: ") {
				name = strings.TrimPrefix(line, "event: ")
			}
			if strings.HasPrefix(line, "data: ") {
				data = strings.TrimPrefix(line, "data: ")
			}
			if p == core.Gemini && !strings.HasPrefix(line, "data: ") {
				t.Fatal("non-data Gemini event", line)
			}
		}
		if data == "[DONE]" {
			if p != core.Chat {
				t.Fatal("foreign terminal marker")
			}
			continue
		}
		var value map[string]any
		if json.Unmarshal([]byte(data), &value) != nil {
			t.Fatal("malformed event", data)
		}
		if p == core.Responses {
			if value["type"] != name || value["sequence_number"] != float64(len(result)) {
				t.Fatal("inconsistent Responses sequence", value)
			}
		}
		result = append(result, value)
	}
	return result
}

func TestStreamContracts(t *testing.T) {
	for _, p := range []core.Protocol{core.Chat, core.Responses, core.Anthropic, core.Gemini} {
		for _, kind := range []string{"plain", "empty", "native", "tool"} {
			if p == core.Anthropic && kind == "native" {
				continue
			} // Prepared as portable text by preset.
			t.Run(string(p)+"/"+kind, func(t *testing.T) {
				request := &core.Request{Protocol: p, Model: "test", Stream: true, IncludeUsage: true, ToolChoice: "auto"}
				writer := httptest.NewRecorder()
				stream := NewStream(writer, request, "result", time.Second)
				emit := func(event core.Event) {
					t.Helper()
					if e := stream.Emit(event); e != nil {
						t.Fatal(e)
					}
				}
				if kind == "native" {
					emit(core.Event{Kind: "reasoning_delta", Text: "模拟思考"})
				}
				if kind == "plain" || kind == "native" {
					emit(core.Event{Kind: "text_delta", Text: "你👨‍👩‍👧‍👦"})
					emit(core.Event{Kind: "text_delta", Text: "好"})
				}
				finish := "stop"
				if kind == "tool" {
					finish = "tool_calls"
					emit(core.Event{Kind: "tool_start", Tool: &core.ToolCall{ID: "call_one", Name: "weather"}})
					emit(core.Event{Kind: "tool_args_delta", Text: `{"city":`})
					emit(core.Event{Kind: "tool_args_delta", Text: `"test"}`})
					emit(core.Event{Kind: "tool_done"})
				}
				emit(core.Event{Kind: "finish", Finish: finish, Usage: core.Usage{Input: 10, Visible: 5, Reasoning: 2, Cached: 1}})
				if !stream.Finished || !stream.Committed || writer.Code != 200 {
					t.Fatal("missing stream terminal state")
				}
				before := writer.Body.String()
				stream.Fail(core.Err("late_error", 500, "safe error"))
				if writer.Body.String() != before || stream.Emit(core.Event{Kind: "finish"}) == nil {
					t.Fatal("double terminal event")
				}
				wire := events(t, p, before)
				body := Body(request, stream.Result, 1)
				switch p {
				case core.Chat:
					if !strings.HasSuffix(before, "data: [DONE]\n\n") {
						t.Fatal("missing Chat terminal")
					}
					last := wire[len(wire)-1]
					if len(arr(last["choices"])) != 0 || obj(last["usage"])["completion_tokens"] != float64(7) {
						t.Fatal("usage is not in a separate terminal chunk")
					}
					for _, event := range wire[:len(wire)-1] {
						if event["usage"] != nil {
							t.Fatal("repeated stream usage")
						}
					}
				case core.Responses:
					if wire[0]["type"] != "response.created" || wire[1]["type"] != "response.in_progress" || wire[len(wire)-1]["type"] != "response.completed" {
						t.Fatal("Responses lifecycle")
					}
					final := obj(wire[len(wire)-1]["response"])
					a, _ := json.Marshal(final["output"])
					b, _ := json.Marshal(body["output"])
					if string(a) != string(b) {
						t.Fatal("final object differs from accumulated output")
					}
				case core.Anthropic:
					if wire[0]["type"] != "message_start" || wire[len(wire)-1]["type"] != "message_stop" || wire[len(wire)-2]["type"] != "message_delta" {
						t.Fatal("Anthropic lifecycle")
					}
					if obj(wire[len(wire)-2]["usage"])["output_tokens"] != float64(7) {
						t.Fatal("Anthropic output usage")
					}
				case core.Gemini:
					if strings.Contains(before, "[DONE]") || obj(wire[len(wire)-1]["usageMetadata"])["totalTokenCount"] != float64(17) {
						t.Fatal("Gemini terminal/usage")
					}
					if kind == "tool" && !strings.Contains(before, `"args":{"city":"test"}`) {
						t.Fatal("Gemini function args are not complete JSON")
					}
				}
			})
		}
	}
}

func TestErrorsAndIncompleteStates(t *testing.T) {
	for _, p := range []core.Protocol{core.Chat, core.Responses, core.Anthropic, core.Gemini} {
		for _, status := range []int{400, 401, 403, 404, 413, 429, 500, 502, 503, 504} {
			writer := httptest.NewRecorder()
			WriteError(writer, p, &core.Error{Code: "test_error", Status: status, Message: "safe error", RetryAfter: 2})
			var body map[string]any
			if writer.Code != status || writer.Header().Get("Retry-After") != "2" || json.Unmarshal(writer.Body.Bytes(), &body) != nil || body["error"] == nil {
				t.Fatal("error envelope", p, status)
			}
		}
		request := &core.Request{Protocol: p, Model: "test"}
		writer := httptest.NewRecorder()
		stream := NewStream(writer, request, "failed", time.Second)
		if e := stream.Emit(core.Event{Kind: "text_delta", Text: "partial"}); e != nil {
			t.Fatal(e)
		}
		stream.Fail(core.Err("upstream_timeout", 504, "safe error"))
		if !stream.Finished || strings.Contains(writer.Body.String(), "[DONE]") || strings.Contains(writer.Body.String(), "response.completed") || strings.Contains(writer.Body.String(), "message_stop") {
			t.Fatal("failure reported success")
		}
		_ = events(t, p, writer.Body.String())
	}
	r := &core.Request{Protocol: core.Responses, Model: "test"}
	for finish, reason := range map[string]string{"length": "max_output_tokens", "content_filter": "content_filter"} {
		v := core.Result{ID: "x", Text: "partial", Finish: finish}
		body := ResponseObject(r, v, 1, "completed")
		if body["status"] != "incomplete" || obj(body["incomplete_details"])["reason"] != reason {
			t.Fatal("incomplete response mapping")
		}
		if ResponseObject(r, v, 1, "failed")["status"] != "failed" {
			t.Fatal("failure overwritten by incomplete")
		}
	}
	if anthropicFinish("stop", "end") != "stop_sequence" || geminiFinish("content_filter") != "SAFETY" {
		t.Fatal("finish reason mapping")
	}
}
