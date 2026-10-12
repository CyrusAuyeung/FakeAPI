package store

import (
	"context"
	"math"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"fakeapi/internal/config"
)

func TestAIQuotaContention(t *testing.T) {
	s := database(t)
	k, _, e := s.Create(context.Background(), config.Policy{TotalLimit: ptr(int64(50)), AILimit: ptr(int64(10)), MaxConcurrent: 200}, "", "")
	if e != nil {
		t.Fatal(e)
	}
	var accepted atomic.Int64
	var workers sync.WaitGroup
	for i := 0; i < 100; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if e := s.Admit(context.Background(), k.ID, true, time.Now(), 1); e == nil {
				accepted.Add(1)
			} else if e.Code != "ai_quota_exceeded" {
				t.Errorf("unexpected refusal: %s", e.Code)
			}
		}()
	}
	workers.Wait()
	v, e := s.Get(context.Background(), k.ID)
	if e != nil || accepted.Load() != 10 || v.TotalUsed != 10 || v.AIUsed != 10 {
		t.Fatal("non-atomic AI quota", accepted.Load(), v.TotalUsed, v.AIUsed, e)
	}
}

func TestManagementRateAndCounterBoundaries(t *testing.T) {
	s := database(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 12, 9, 0, 10, 0, time.UTC)
	expires := now.Add(2 * time.Minute)
	k, raw, e := s.Create(ctx, config.Policy{MaxConcurrent: 3, RPM: ptr(int64(1)), ExpiresAt: &expires}, "initial", "")
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e := s.Create(ctx, k.Policy, "duplicate", raw); e == nil {
		t.Fatal("duplicate imported key accepted")
	}
	other := database(t)
	if restored, got, e := other.Create(ctx, k.Policy, "restored", raw); e != nil || got != raw || restored.ID != k.ID {
		t.Fatal("key import failed", e)
	}
	for _, bad := range []string{"not-a-key", "fk_short_x", "fk_zzzzzzzzzzzzzzzzzzzz_invalid"} {
		if _, _, e := s.Create(ctx, k.Policy, "", bad); e == nil {
			t.Fatal("invalid imported key accepted")
		}
	}
	if _, e := s.Auth(ctx, "fk_missing_x", now); e == nil || e.Status != 401 {
		t.Fatal("missing key authenticated")
	}
	if _, e := s.Auth(ctx, raw+"x", now); e == nil || e.Status != 401 {
		t.Fatal("altered key authenticated")
	}
	if e := s.Admit(ctx, k.ID, false, now, 1); e != nil {
		t.Fatal(e)
	}
	if e := s.Admit(ctx, k.ID, false, now, 1); e == nil || e.RetryAfter != 50 {
		t.Fatal("rate retry boundary", e)
	}
	if e := s.Admit(ctx, k.ID, false, now.Add(50*time.Second), 1); e != nil {
		t.Fatal("new minute did not reset window", e)
	}
	k, _ = s.Get(ctx, k.ID)
	k.Label, k.Tags, k.Group = "updated", []string{"ai"}, "friends"
	k.MaxConcurrent, k.TotalLimit = 1, ptr(int64(2))
	if e := s.Update(ctx, k); e != nil {
		t.Fatal(e)
	}
	listed, e := s.List(ctx)
	if e != nil || len(listed) != 1 || listed[0].TotalUsed != 2 || listed[0].Group != "friends" {
		t.Fatal("management update changed usage", e)
	}
	if e := s.Admit(ctx, k.ID, false, now.Add(time.Minute), 2); e == nil || e.Code != "key_concurrency_exceeded" {
		t.Fatal("updated concurrency was not rechecked", e)
	}
	if e := s.Admit(ctx, k.ID, false, now.Add(time.Minute), 1); e == nil || e.Code != "request_quota_exceeded" {
		t.Fatal("updated quota was not rechecked", e)
	}
	if e := s.Reset(ctx, k.ID); e != nil {
		t.Fatal(e)
	}
	k.RPM, k.TotalLimit = ptr(int64(0)), nil
	if e := s.Update(ctx, k); e != nil {
		t.Fatal(e)
	}
	if e := s.Admit(ctx, k.ID, false, now, 1); e == nil || e.Code != "rate_limit_exceeded" || e.RetryAfter != 0 {
		t.Fatal("zero rate must prohibit without a false retry estimate", e)
	}
	k.RPM = nil
	if e := s.Update(ctx, k); e != nil {
		t.Fatal(e)
	}
	if _, e := s.DB.Exec("UPDATE key_usage SET total_used=?,ai_used=? WHERE key_id=?", math.MaxInt64, math.MaxInt64, k.ID); e != nil {
		t.Fatal(e)
	}
	if e := s.Admit(ctx, k.ID, true, now, 1); e == nil || e.Code != "counter_exhausted" {
		t.Fatal("integer overflow not stopped", e)
	}
	if e := s.Update(ctx, Key{ID: "missing", Policy: config.Policy{MaxConcurrent: 1}}); e == nil {
		t.Fatal("missing update reported success")
	}
	if e := s.Revoke(ctx, "missing"); e == nil {
		t.Fatal("missing revoke reported success")
	}
	if e := s.Reset(ctx, "missing"); e == nil {
		t.Fatal("missing reset reported success")
	}
	for _, target := range []string{s.Path, s.Path + "-wal", s.Path + "-shm"} {
		if e := s.Backup(ctx, target); e == nil {
			t.Fatal("unsafe backup destination accepted")
		}
	}
	out := filepath.Join(t.TempDir(), "backup.sqlite")
	if e := s.Backup(ctx, out); e != nil {
		t.Fatal(e)
	}
	if e := s.Backup(ctx, out); e == nil {
		t.Fatal("backup overwrote file")
	}
}

func TestBusyRollbackAndFutureSchema(t *testing.T) {
	s := database(t)
	ctx := context.Background()
	k, raw, e := s.Create(ctx, config.Policy{MaxConcurrent: 10}, "", "")
	if e != nil {
		t.Fatal(e)
	}
	second, e := Open(s.Path, 10*time.Millisecond)
	if e != nil {
		t.Fatal(e)
	}
	defer second.Close()
	conn, e := s.DB.Conn(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); e != nil {
		t.Fatal(e)
	}
	eAdmission := second.Admit(ctx, k.ID, true, time.Now(), 1)
	if _, e = conn.ExecContext(ctx, "ROLLBACK"); e != nil {
		t.Fatal(e)
	}
	conn.Close()
	if eAdmission == nil || eAdmission.Code != "database_busy" {
		t.Fatal("busy database not surfaced", eAdmission)
	}
	unchanged, e := s.Get(ctx, k.ID)
	if e != nil || unchanged.TotalUsed != 0 || unchanged.AIUsed != 0 {
		t.Fatal("busy operation consumed quota", e)
	}
	if _, e := s.DB.Exec("INSERT INTO schema_migrations VALUES(2,1)"); e != nil {
		t.Fatal(e)
	}
	if newer, e := Open(s.Path, time.Second); e == nil {
		newer.Close()
		t.Fatal("newer schema opened for downgrade")
	}
	second.Close()
	if _, e := second.Auth(ctx, raw, time.Now()); e == nil || e.Status != 500 {
		t.Fatal("database failure misreported as invalid key", e)
	}
}

func TestRateWriteFailureRollsBackAdmission(t *testing.T) {
	s := database(t)
	ctx := context.Background()
	k, _, e := s.Create(ctx, config.Policy{MaxConcurrent: 2}, "", "")
	if e != nil {
		t.Fatal(e)
	}
	if _, e := s.DB.Exec("CREATE TRIGGER refuse_rate BEFORE INSERT ON rate_windows BEGIN SELECT RAISE(ABORT,'private-fault-canary'); END"); e != nil {
		t.Fatal(e)
	}
	err := s.Admit(ctx, k.ID, true, time.Now(), 1)
	if err == nil || err.Code != "database_error" || err.Message != "Database operation failed" {
		t.Fatal("unsafe database error", err)
	}
	v, e := s.Get(ctx, k.ID)
	if e != nil || v.TotalUsed != 0 || v.AIUsed != 0 {
		t.Fatal("failed rate write committed a partial debit", e)
	}
}
