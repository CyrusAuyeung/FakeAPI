// Development-only local upstream. Scenarios are chosen by CLI, never prompts.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"fakeapi/assets"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:9099", "Local listen address")
	scenario := flag.String("scenario", "normal", "normal, auth, rate, bad-json, role-error, partial-eof, first-timeout, idle-timeout")
	flag.Parse()
	http.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		var body map[string]any
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20)).Decode(&body) != nil {
			w.WriteHeader(400)
			return
		}
		switch *scenario {
		case "auth":
			w.WriteHeader(401)
			fmt.Fprint(w, `{"error":{"message":"Offline authentication failure"}}`)
			return
		case "rate":
			w.WriteHeader(429)
			return
		case "bad-json":
			fmt.Fprint(w, "invalid JSON")
			return
		case "first-timeout":
			select {
			case <-r.Context().Done():
			case <-time.After(time.Minute):
			}
			return
		}
		text := "Offline upstream response."
		model, _ := body["model"].(string)
		if strings.Contains(model, "svg") {
			b, _ := assets.Files.ReadFile("fixtures/pelican-bicycle.svg.txt")
			text = string(b)
		}
		stream, _ := body["stream"].(bool)
		usage := map[string]any{"prompt_tokens": 11, "completion_tokens": (len(text) + 3) / 4, "total_tokens": 11 + (len(text)+3)/4}
		if !stream {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"id": "offline-test", "object": "chat.completion", "created": 1, "model": model, "choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": text}, "finish_reason": "stop"}}, "usage": usage})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		send := func(v any) { b, _ := json.Marshal(v); fmt.Fprintf(w, "data: %s\n\n", b); w.(http.Flusher).Flush() }
		chunk := func(delta map[string]any, finish any) {
			send(map[string]any{"id": "offline-test", "object": "chat.completion.chunk", "created": 1, "model": model, "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}})
		}
		chunk(map[string]any{"role": "assistant"}, nil)
		if *scenario == "role-error" {
			send(map[string]any{"error": map[string]any{"message": "Offline scripted error"}})
			return
		}
		for len(text) > 0 {
			n := 16
			if n > len(text) {
				n = len(text)
			}
			chunk(map[string]any{"content": text[:n]}, nil)
			text = text[n:]
			if *scenario == "partial-eof" {
				return
			}
			if *scenario == "idle-timeout" {
				select {
				case <-r.Context().Done():
				case <-time.After(time.Minute):
				}
				return
			}
		}
		chunk(map[string]any{}, "stop")
		send(map[string]any{"choices": []any{}, "usage": usage})
		fmt.Fprint(w, "data: [DONE]\n\n")
	})
	fmt.Fprintln(os.Stderr, "Offline mock upstream listening", *listen)
	if http.ListenAndServe(*listen, nil) != nil {
		fmt.Fprintln(os.Stderr, "E_MOCK_SERVER")
		os.Exit(1)
	}
}
