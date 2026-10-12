package pacing

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"fakeapi/internal/config"
	"fakeapi/internal/core"
)

func TestLogicalScheduleAndCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := &core.Request{Model: "test"}
		p := config.Profile{Timing: config.Timing{FirstOutputDelay: config.Duration(300 * time.Millisecond), ThinkingToBodyDelay: config.Duration(400 * time.Millisecond), GraphemesPerSecond: 10, ChunkSize: 1}}
		start := time.Now()
		var at []time.Duration
		var kinds []string
		e := Run(context.Background(), r, core.Result{Reasoning: "AB", Text: "CD", Finish: "stop"}, p, RNG(42, r, "test", "timing"), func(event core.Event) error {
			at = append(at, time.Since(start))
			kinds = append(kinds, event.Kind)
			return nil
		})
		if e != nil {
			t.Fatal(e)
		}
		want := []time.Duration{300 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond, 900 * time.Millisecond, 900 * time.Millisecond}
		if len(at) != len(want) {
			t.Fatal(kinds, at)
		}
		for i := range want {
			if at[i] != want[i] {
				t.Fatal("wrong logical schedule", at)
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		called := false
		e = Run(ctx, r, core.Result{Text: "must not emit"}, p, RNG(42, r, "test", "timing"), func(core.Event) error { called = true; return nil })
		if !errors.Is(e, context.DeadlineExceeded) || called {
			t.Fatal("cancellation before first output failed", e)
		}
	})
}

func TestPortableGapAndToolPacing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := &core.Request{Model: "test"}
		p := config.Profile{Thinking: config.Thinking{Enabled: true}, Timing: config.Timing{FirstOutputDelay: config.Duration(100 * time.Millisecond), ThinkingToBodyDelay: config.Duration(400 * time.Millisecond), ChunkSize: 128}}
		start := time.Now()
		var text string
		var at []time.Duration
		e := Run(context.Background(), r, core.Result{Text: "[模拟思考] AB\n\nCD"}, p, RNG(42, r, "test", "timing"), func(event core.Event) error {
			if event.Kind == "text_delta" {
				text += event.Text
				at = append(at, time.Since(start))
			}
			return nil
		})
		if e != nil || text != "[模拟思考] AB\n\nCD" || len(at) != 2 || at[0] != 100*time.Millisecond || at[1] != 500*time.Millisecond {
			t.Fatal("portable gap", at, e)
		}
		p.Thinking.Enabled = false
		p.Timing = config.Timing{GraphemesPerSecond: 10, ChunkSize: 1}
		start = time.Now()
		args := ""
		e = Run(context.Background(), r, core.Result{Tool: &core.ToolCall{ID: "call", Name: "f", Arguments: "{}"}, Finish: "tool_calls"}, p, RNG(42, r, "test", "timing"), func(event core.Event) error {
			if event.Kind == "tool_args_delta" {
				args += event.Text
			}
			return nil
		})
		if e != nil || args != "{}" || time.Since(start) != 100*time.Millisecond {
			t.Fatal("tool pacing", e)
		}
	})
}
