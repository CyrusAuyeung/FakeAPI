package pacing

import (
	"context"
	"fakeapi/internal/config"
	"fakeapi/internal/core"
	"testing"
	"time"
)

func TestGraphemeAndReproducibility(t *testing.T) {
	chunks := Chunks("👨‍👩‍👧‍👦é中", 1)
	if len(chunks) != 3 {
		t.Fatal(chunks)
	}
	r := &core.Request{Model: "x", Latest: core.LatestUser{Text: "same"}}
	a, b := RNG(42, r, "rule", "body"), RNG(42, r, "rule", "body")
	for i := 0; i < 100; i++ {
		if a.Uint64() != b.Uint64() {
			t.Fatal("random state dependent")
		}
	}
	c := RNG(42, r, "rule", "timing")
	if RNG(42, r, "rule", "body").Uint64() == c.Uint64() {
		t.Fatal("domains coupled")
	}
}
func TestCancelledWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if Wait(ctx, time.Hour) == nil || time.Since(start) > time.Second {
		t.Fatal("cancelled wait not released")
	}
	tune := config.Timing{ChunkSize: 4, GraphemesPerSecond: 0}
	if Delay(tune, "hello", RNG(0, &core.Request{}, "", "timing")) != 0 {
		t.Fatal("instant pacing")
	}
}
