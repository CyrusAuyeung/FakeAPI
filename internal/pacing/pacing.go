package pacing

import (
	"context"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"math/rand/v2"
	"strings"
	"time"

	"fakeapi/internal/config"
	"fakeapi/internal/core"
	"github.com/rivo/uniseg"
)

func RNG(seed int64, r *core.Request, rule, domain string) *rand.Rand {
	b := sha256.Sum256([]byte(r.Latest.Text + "\x00" + r.Model + "\x00" + rule + "\x00" + domain))
	s := uint64(seed)
	return rand.New(rand.NewPCG(binary.LittleEndian.Uint64(b[:8])^s, binary.LittleEndian.Uint64(b[8:16])^s))
}
func Seed(c config.Random, override *int64) int64 {
	if override != nil && c.AllowRequestSeed {
		return *override
	}
	if c.Mode == "deterministic" && c.Seed != nil {
		return *c.Seed
	}
	b := make([]byte, 8)
	_, _ = crand.Read(b)
	return int64(binary.LittleEndian.Uint64(b))
}
func Chunks(text string, size int) []string {
	if size < 1 {
		size = 16
	}
	g := uniseg.NewGraphemes(text)
	var out []string
	buf := ""
	n := 0
	for g.Next() {
		buf += g.Str()
		n++
		if n == size {
			out = append(out, buf)
			buf = ""
			n = 0
		}
	}
	if buf != "" {
		out = append(out, buf)
	}
	return out
}
func Wait(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
func Delay(t config.Timing, chunk string, rng *rand.Rand) time.Duration {
	d := time.Duration(0)
	if t.GraphemesPerSecond > 0 {
		d = time.Duration(float64(uniseg.GraphemeClusterCount(chunk)) / t.GraphemesPerSecond * float64(time.Second))
	}
	if t.Jitter > 0 {
		d += time.Duration((rng.Float64()*2 - 1) * float64(t.Jitter))
	}
	if d < 0 {
		d = 0
	}
	if rng.Float64() < t.PauseProbability {
		d += t.PauseDuration.Value()
	}
	return d
}
func Run(ctx context.Context, r *core.Request, result core.Result, p config.Profile, rng *rand.Rand, emit func(core.Event) error) error {
	if e := Wait(ctx, p.Timing.FirstOutputDelay.Value()); e != nil {
		return e
	}
	sections := []struct{ kind, text string }{{"reasoning_delta", result.Reasoning}, {"text_delta", result.Text}}
	if result.Reasoning == "" && p.Thinking.Enabled && strings.HasPrefix(result.Text, "[模拟思考]") {
		if thinking, body, ok := strings.Cut(result.Text, "\n\n"); ok {
			sections = []struct{ kind, text string }{{"text_delta", thinking + "\n\n"}, {"text_delta", body}}
		}
	}
	for i, s := range sections {
		if s.text == "" {
			continue
		}
		chunks := Chunks(s.text, p.Timing.ChunkSize)
		for j, text := range chunks {
			if e := emit(core.Event{Kind: s.kind, Text: text}); e != nil {
				return e
			}
			if j+1 < len(chunks) {
				if e := Wait(ctx, Delay(p.Timing, text, rng)); e != nil {
					return e
				}
			}
		}
		if i == 0 && sections[1].text != "" {
			if e := Wait(ctx, p.Timing.ThinkingToBodyDelay.Value()); e != nil {
				return e
			}
		}
	}
	if result.Tool != nil {
		if e := emit(core.Event{Kind: "tool_start", Tool: result.Tool}); e != nil {
			return e
		}
		chunks := Chunks(result.Tool.Arguments, p.Timing.ChunkSize)
		for i, chunk := range chunks {
			if e := emit(core.Event{Kind: "tool_args_delta", Text: chunk}); e != nil {
				return e
			}
			if i+1 < len(chunks) {
				if e := Wait(ctx, Delay(p.Timing, chunk, rng)); e != nil {
					return e
				}
			}
		}
		if e := emit(core.Event{Kind: "tool_done"}); e != nil {
			return e
		}
	}
	return emit(core.Event{Kind: "finish", Usage: result.Usage, Finish: result.Finish, Stop: result.Stop})
}
