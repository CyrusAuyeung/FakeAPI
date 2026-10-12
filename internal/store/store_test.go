package store

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"fakeapi/internal/config"
)

func ptr[T any](v T) *T { return &v }
func database(t *testing.T) *Store {
	t.Helper()
	s, e := Open(filepath.Join(t.TempDir(), "state.sqlite"), 2*time.Second)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func TestAtomicQuotaAndRestart(t *testing.T) {
	s := database(t)
	ctx := context.Background()
	k, raw, e := s.Create(ctx, config.Policy{TotalLimit: ptr(int64(50)), AILimit: ptr(int64(10)), MaxConcurrent: 200}, "test", "")
	if e != nil {
		t.Fatal(e)
	}
	if _, e := s.Auth(ctx, raw, time.Now()); e != nil {
		t.Fatal(e)
	}
	var total, ai atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(isAI bool) {
			defer wg.Done()
			err := s.Admit(ctx, k.ID, isAI, time.Now(), 1)
			if err == nil {
				total.Add(1)
				if isAI {
					ai.Add(1)
				}
			} else if err.Status != 429 {
				t.Errorf("unexpected admission: %v", err)
			}
		}(i%2 == 0)
	}
	wg.Wait()
	if total.Load() != 50 || ai.Load() > 10 {
		t.Fatalf("admitted total=%d ai=%d", total.Load(), ai.Load())
	}
	k, e = s.Get(ctx, k.ID)
	if e != nil || k.TotalUsed != 50 || k.AIUsed != ai.Load() {
		t.Fatal(k, e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s2, e := Open(s.Path, 2*time.Second)
	if e != nil {
		t.Fatal(e)
	}
	defer s2.Close()
	k, e = s2.Get(ctx, k.ID)
	if e != nil || k.TotalUsed != 50 {
		t.Fatal("usage lost after restart")
	}
	s2.Close()
	b, e := os.ReadFile(s.Path)
	if e != nil {
		t.Fatal(e)
	}
	if stringContains(b, raw) {
		t.Fatal("key persisted in plaintext")
	}
}
func stringContains(b []byte, s string) bool {
	for i := 0; i+len(s) <= len(b); i++ {
		if string(b[i:i+len(s)]) == s {
			return true
		}
	}
	return false
}
func TestRateExpiryAndCancel(t *testing.T) {
	s := database(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	expiry := now.Add(time.Minute)
	k, _, e := s.Create(ctx, config.Policy{RPM: ptr(int64(1)), ExpiresAt: &expiry, MaxConcurrent: 4}, "", "")
	if e != nil {
		t.Fatal(e)
	}
	if err := s.Admit(ctx, k.ID, false, now, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.Admit(ctx, k.ID, false, now.Add(-time.Minute), 1); err == nil || err.Status != 429 {
		t.Fatal("clock rollback reset rate window")
	}
	if err := s.Admit(ctx, k.ID, false, expiry, 1); err == nil || err.Status != 401 {
		t.Fatal("exact expiry accepted")
	}
	if err := s.Reset(ctx, k.ID); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := s.Admit(cancelled, k.ID, true, now, 1); err == nil {
		t.Fatal("cancelled admitted")
	}
	v, _ := s.Get(ctx, k.ID)
	if v.TotalUsed != 0 || v.AIUsed != 0 {
		t.Fatal("cancelled consumed quota")
	}
	if e := s.Revoke(ctx, k.ID); e != nil {
		t.Fatal(e)
	}
	if err := s.Admit(ctx, k.ID, false, now, 1); err == nil || err.Status != 401 {
		t.Fatal("revoked admitted")
	}
}
func TestAILimitDoesNotConsumeTotal(t *testing.T) {
	s := database(t)
	ctx := context.Background()
	k, _, e := s.Create(ctx, config.Policy{TotalLimit: ptr(int64(3)), AILimit: ptr(int64(0)), MaxConcurrent: 1}, "", "")
	if e != nil {
		t.Fatal(e)
	}
	if err := s.Admit(ctx, k.ID, true, time.Now(), 1); err == nil || err.Code != "ai_quota_exceeded" {
		t.Fatal(err)
	}
	v, _ := s.Get(ctx, k.ID)
	if v.TotalUsed != 0 {
		t.Fatal("partial debit")
	}
	out := filepath.Join(t.TempDir(), "backup.sqlite")
	if e = s.Backup(ctx, out); e != nil {
		t.Fatal(e)
	}
	backup, e := Open(out, time.Second)
	if e != nil {
		t.Fatal(e)
	}
	defer backup.Close()
	if _, e = backup.Get(ctx, k.ID); e != nil {
		t.Fatal(e)
	}
}
