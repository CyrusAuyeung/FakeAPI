package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fakeapi/internal/config"
	"fakeapi/internal/core"
	_ "modernc.org/sqlite"
)

type Store struct {
	DB   *sql.DB
	Path string
}
type Key struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	config.Policy
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at"`
	TotalUsed int64      `json:"total_used"`
	AIUsed    int64      `json:"ai_used"`
	hash      []byte
}

const migration = `CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,applied_at_ms INTEGER NOT NULL);
CREATE TABLE api_keys(id TEXT PRIMARY KEY,secret_hash BLOB NOT NULL,label TEXT NOT NULL,tags_json TEXT NOT NULL,rule_group TEXT NOT NULL,created_at_ms INTEGER NOT NULL,expires_at_ms INTEGER,revoked_at_ms INTEGER,total_limit INTEGER CHECK(total_limit>=0),ai_limit INTEGER CHECK(ai_limit>=0),rpm INTEGER CHECK(rpm>=0),max_concurrent INTEGER NOT NULL CHECK(max_concurrent>0));
CREATE TABLE key_usage(key_id TEXT PRIMARY KEY REFERENCES api_keys(id),total_used INTEGER NOT NULL DEFAULT 0,ai_used INTEGER NOT NULL DEFAULT 0,CHECK(total_used>=ai_used AND ai_used>=0));
CREATE TABLE rate_windows(key_id TEXT PRIMARY KEY REFERENCES api_keys(id),minute_start_ms INTEGER NOT NULL,accepted_count INTEGER NOT NULL CHECK(accepted_count>=0));`

func Open(path string, busy time.Duration) (*Store, error) {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return nil, fmt.Errorf("E_DB_DIRECTORY")
	}
	q := url.Values{}
	q.Add("_pragma", "busy_timeout("+fmt.Sprint(busy.Milliseconds())+")")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "synchronous(FULL)")
	db, e := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?"+q.Encode())
	if e != nil {
		return nil, fmt.Errorf("E_DB_OPEN")
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	fail := func() (*Store, error) { db.Close(); return nil, fmt.Errorf("E_DB_SCHEMA") }
	if _, e = db.Exec("PRAGMA journal_mode=WAL"); e != nil {
		return fail()
	}
	var n int
	if e = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'").Scan(&n); e != nil {
		return fail()
	}
	if n == 0 {
		tx, e := db.Begin()
		if e != nil {
			return fail()
		}
		if _, e = tx.Exec(migration); e != nil {
			tx.Rollback()
			return fail()
		}
		if _, e = tx.Exec("INSERT INTO schema_migrations VALUES(1,?)", time.Now().UnixMilli()); e != nil {
			tx.Rollback()
			return fail()
		}
		if e = tx.Commit(); e != nil {
			return fail()
		}
	}
	var version int
	if e = db.QueryRow("SELECT max(version) FROM schema_migrations").Scan(&version); e != nil || version != 1 {
		return fail()
	}
	return &Store{DB: db, Path: path}, nil
}
func (s *Store) Close() error { return s.DB.Close() }

type scanner interface{ Scan(...any) error }

const selectKey = `SELECT k.id,k.secret_hash,k.label,k.tags_json,k.rule_group,k.created_at_ms,k.expires_at_ms,k.revoked_at_ms,k.total_limit,k.ai_limit,k.rpm,k.max_concurrent,u.total_used,u.ai_used FROM api_keys k JOIN key_usage u ON u.key_id=k.id`

func scanKey(row scanner) (Key, error) {
	var k Key
	var tags string
	var created int64
	var exp, rev, total, ai, rpm sql.NullInt64
	e := row.Scan(&k.ID, &k.hash, &k.Label, &tags, &k.Group, &created, &exp, &rev, &total, &ai, &rpm, &k.MaxConcurrent, &k.TotalUsed, &k.AIUsed)
	if e != nil {
		return k, e
	}
	_ = json.Unmarshal([]byte(tags), &k.Tags)
	k.CreatedAt = time.UnixMilli(created).UTC()
	if exp.Valid {
		v := time.UnixMilli(exp.Int64).UTC()
		k.ExpiresAt = &v
	}
	if rev.Valid {
		v := time.UnixMilli(rev.Int64).UTC()
		k.RevokedAt = &v
	}
	if total.Valid {
		k.TotalLimit = &total.Int64
	}
	if ai.Valid {
		k.AILimit = &ai.Int64
	}
	if rpm.Valid {
		k.RPM = &rpm.Int64
	}
	return k, nil
}
func (s *Store) Get(ctx context.Context, id string) (Key, error) {
	return scanKey(s.DB.QueryRowContext(ctx, selectKey+" WHERE k.id=?", id))
}
func (s *Store) List(ctx context.Context) ([]Key, error) {
	rows, e := s.DB.QueryContext(ctx, selectKey+" ORDER BY k.created_at_ms,k.id")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Key{}
	for rows.Next() {
		k, e := scanKey(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, k)
	}
	return out, rows.Err()
}
func valid(k Key, now time.Time) *core.Error {
	if k.RevokedAt != nil {
		return core.Err("revoked_api_key", 401, "API key revoked")
	}
	if k.ExpiresAt != nil && !now.Before(*k.ExpiresAt) {
		return core.Err("expired_api_key", 401, "API key expired")
	}
	return nil
}
func (s *Store) Auth(ctx context.Context, raw string, now time.Time) (Key, *core.Error) {
	parts := strings.Split(raw, "_")
	if len(parts) < 3 || parts[0] != "fk" {
		return Key{}, core.Err("invalid_api_key", 401, "Invalid API key")
	}
	k, e := s.Get(ctx, parts[1])
	h := sha256.Sum256([]byte(raw))
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return Key{}, dbError(e)
	}
	if e != nil || subtle.ConstantTimeCompare(h[:], k.hash) != 1 {
		return Key{}, core.Err("invalid_api_key", 401, "Invalid API key")
	}
	return k, valid(k, now)
}
func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UnixMilli()
}
func (s *Store) Create(ctx context.Context, p config.Policy, label string, existing string) (Key, string, error) {
	var id, raw string
	if existing != "" {
		p := strings.SplitN(existing, "_", 3)
		if len(p) != 3 || p[0] != "fk" || len(p[1]) != 20 {
			return Key{}, "", fmt.Errorf("E_KEY_FORMAT")
		}
		secret, e := base64.RawURLEncoding.DecodeString(p[2])
		if e != nil || len(secret) != 32 {
			return Key{}, "", fmt.Errorf("E_KEY_FORMAT")
		}
		if _, e = hex.DecodeString(p[1]); e != nil {
			return Key{}, "", fmt.Errorf("E_KEY_FORMAT")
		}
		id = p[1]
		raw = existing
	} else {
		b := make([]byte, 42)
		if _, e := rand.Read(b); e != nil {
			return Key{}, "", fmt.Errorf("E_RANDOM")
		}
		id = hex.EncodeToString(b[:10])
		raw = "fk_" + id + "_" + base64.RawURLEncoding.EncodeToString(b[10:])
	}
	h := sha256.Sum256([]byte(raw))
	tags, _ := json.Marshal(p.Tags)
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return Key{}, "", fmt.Errorf("E_DB_WRITE")
	}
	defer tx.Rollback()
	_, e = tx.ExecContext(ctx, `INSERT INTO api_keys(id,secret_hash,label,tags_json,rule_group,created_at_ms,expires_at_ms,total_limit,ai_limit,rpm,max_concurrent) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, id, h[:], label, string(tags), p.Group, time.Now().UnixMilli(), nullableTime(p.ExpiresAt), p.TotalLimit, p.AILimit, p.RPM, p.MaxConcurrent)
	if e != nil {
		return Key{}, "", fmt.Errorf("E_KEY_CREATE")
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO key_usage(key_id) VALUES(?)", id)
	if e != nil {
		return Key{}, "", fmt.Errorf("E_DB_WRITE")
	}
	if e = tx.Commit(); e != nil {
		return Key{}, "", fmt.Errorf("E_DB_WRITE")
	}
	k, e := s.Get(ctx, id)
	return k, raw, e
}
func (s *Store) Revoke(ctx context.Context, id string) error {
	r, e := s.DB.ExecContext(ctx, "UPDATE api_keys SET revoked_at_ms=? WHERE id=?", time.Now().UnixMilli(), id)
	if e != nil {
		return fmt.Errorf("E_DB_WRITE")
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return fmt.Errorf("E_KEY_NOT_FOUND")
	}
	return nil
}
func (s *Store) Update(ctx context.Context, k Key) error {
	b, _ := json.Marshal(k.Tags)
	result, e := s.DB.ExecContext(ctx, `UPDATE api_keys SET label=?,tags_json=?,rule_group=?,expires_at_ms=?,total_limit=?,ai_limit=?,rpm=?,max_concurrent=? WHERE id=?`, k.Label, string(b), k.Group, nullableTime(k.ExpiresAt), k.TotalLimit, k.AILimit, k.RPM, k.MaxConcurrent, k.ID)
	if e != nil {
		return fmt.Errorf("E_DB_WRITE")
	}
	if n, e := result.RowsAffected(); e != nil || n != 1 {
		return fmt.Errorf("E_KEY_NOT_FOUND")
	}
	return nil
}
func (s *Store) Reset(ctx context.Context, id string) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return fmt.Errorf("E_DB_WRITE")
	}
	defer tx.Rollback()
	r, e := tx.ExecContext(ctx, "UPDATE key_usage SET total_used=0,ai_used=0 WHERE key_id=?", id)
	if e != nil {
		return fmt.Errorf("E_DB_WRITE")
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return fmt.Errorf("E_KEY_NOT_FOUND")
	}
	if _, e = tx.ExecContext(ctx, "DELETE FROM rate_windows WHERE key_id=?", id); e != nil {
		return fmt.Errorf("E_DB_WRITE")
	}
	return tx.Commit()
}
func dbError(e error) *core.Error {
	if strings.Contains(strings.ToLower(e.Error()), "busy") || strings.Contains(strings.ToLower(e.Error()), "locked") {
		return core.Err("database_busy", 503, "Database temporarily busy")
	}
	return core.Err("database_error", 500, "Database operation failed")
}
func (s *Store) Admit(ctx context.Context, id string, isAI bool, now time.Time, active int) *core.Error {
	conn, e := s.DB.Conn(ctx)
	if e != nil {
		return dbError(e)
	}
	defer conn.Close()
	if _, e = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); e != nil {
		return dbError(e)
	}
	defer conn.ExecContext(context.Background(), "ROLLBACK")
	k, e := scanKey(conn.QueryRowContext(ctx, selectKey+" WHERE k.id=?", id))
	if e != nil {
		if !errors.Is(e, sql.ErrNoRows) {
			return dbError(e)
		}
		return core.Err("invalid_api_key", 401, "Invalid API key")
	}
	if e := valid(k, now); e != nil {
		return e
	}
	if active > k.MaxConcurrent {
		return core.Err("key_concurrency_exceeded", 503, "Key concurrent request limit reached")
	}
	if k.TotalUsed == math.MaxInt64 || k.AIUsed == math.MaxInt64 {
		return core.Err("counter_exhausted", 500, "Request counter exhausted")
	}
	if k.TotalLimit != nil && k.TotalUsed >= *k.TotalLimit {
		return core.Err("request_quota_exceeded", 429, "Request quota exceeded")
	}
	if isAI && k.AILimit != nil && k.AIUsed >= *k.AILimit {
		return core.Err("ai_quota_exceeded", 429, "AI request quota exceeded")
	}
	minute := now.UTC().Truncate(time.Minute).UnixMilli()
	var stored, count int64
	e = conn.QueryRowContext(ctx, "SELECT minute_start_ms,accepted_count FROM rate_windows WHERE key_id=?", id).Scan(&stored, &count)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return dbError(e)
	}
	if minute > stored {
		stored = minute
		count = 0
	}
	if k.RPM != nil && count >= *k.RPM {
		err := core.Err("rate_limit_exceeded", 429, "Rate limit exceeded")
		if *k.RPM == 0 {
			return err
		}
		err.RetryAfter = int((stored + 60000 - now.UnixMilli() + 999) / 1000)
		if err.RetryAfter < 1 {
			err.RetryAfter = 1
		}
		return err
	}
	if count == math.MaxInt64 {
		return core.Err("counter_exhausted", 500, "Request counter exhausted")
	}
	inc := 0
	if isAI {
		inc = 1
	}
	if _, e = conn.ExecContext(ctx, "UPDATE key_usage SET total_used=total_used+1,ai_used=ai_used+? WHERE key_id=?", inc, id); e != nil {
		return dbError(e)
	}
	if _, e = conn.ExecContext(ctx, `INSERT INTO rate_windows VALUES(?,?,?) ON CONFLICT(key_id) DO UPDATE SET minute_start_ms=excluded.minute_start_ms,accepted_count=excluded.accepted_count`, id, stored, count+1); e != nil {
		return dbError(e)
	}
	if e = ctx.Err(); e != nil {
		return core.Err("cancelled", 499, "Request cancelled")
	}
	if _, e = conn.ExecContext(ctx, "COMMIT"); e != nil {
		return dbError(e)
	}
	return nil
}
func (s *Store) Backup(ctx context.Context, out string) error {
	p, _ := filepath.Abs(out)
	src, _ := filepath.Abs(s.Path)
	if p == src || p == src+"-wal" || p == src+"-shm" {
		return fmt.Errorf("E_BACKUP_PATH")
	}
	if _, e := os.Stat(p); !os.IsNotExist(e) {
		return fmt.Errorf("E_BACKUP_EXISTS")
	}
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		return fmt.Errorf("E_BACKUP_DIRECTORY")
	}
	if _, e := s.DB.ExecContext(ctx, "VACUUM INTO ?", p); e != nil {
		return fmt.Errorf("E_BACKUP_FAILED")
	}
	return nil
}
