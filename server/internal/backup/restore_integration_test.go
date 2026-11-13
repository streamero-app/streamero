package backup

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/streamero-app/streamero/server/internal/db"
)

// scratchDB creates a database of its own: a restore drops the whole schema, and other
// packages' integration tests share STREAMERO_TEST_DB_URL in parallel.
func scratchDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	base := os.Getenv("STREAMERO_TEST_DB_URL")
	if base == "" {
		t.Skip("STREAMERO_TEST_DB_URL not set")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close(ctx) }()
	var rnd [6]byte
	_, _ = rand.Read(rnd[:])
	name := "backup_test_" + hex.EncodeToString(rnd[:])
	if _, err := admin.Exec(ctx, `CREATE DATABASE `+name); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	pool, err := db.Connect(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		c, err := pgx.Connect(context.Background(), base)
		if err == nil {
			_, _ = c.Exec(context.Background(), `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`)
			_ = c.Close(context.Background())
		}
	})
	return pool
}

type user struct {
	Username, Hash string
	Admin          bool
	Locked         *time.Time
}

func getUser(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) (user, bool) {
	t.Helper()
	var u user
	err := pool.QueryRow(context.Background(), `SELECT username, password_hash, is_admin, locked_until FROM users WHERE id = $1`, id).
		Scan(&u.Username, &u.Hash, &u.Admin, &u.Locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return u, true
}

func count(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Back up, change data, restore, and check that the data came back.
func TestBackupRestoreIntegration(t *testing.T) {
	pool := scratchDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	svc := &Service{Pool: pool, Dir: filepath.Join(root, "config", "backups"), ConfigDir: filepath.Join(root, "config"),
		CacheDir: filepath.Join(root, "cache"), Version: "9.9.9"}
	mustWrite(t, filepath.Join(root, "config", "extra", "settings.yml"), "a: 1")
	mustWrite(t, filepath.Join(root, "cache", "avatars", "x.jpg"), "jpeg")
	mustWrite(t, filepath.Join(root, "cache", "images", "art.jpg"), "art")

	// Text that COPY must escape, a NULL, an array and a foreign key.
	ana, lib := uuid.New(), uuid.New()
	locked := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	exec(t, pool, `INSERT INTO users (id, username, password_hash, is_admin, locked_until) VALUES ($1, $2, $3, true, $4)`,
		ana, "ana\ttab\nnewline \\ back šđč 🎬", "hash\x01", locked)
	exec(t, pool, `INSERT INTO sessions (id, user_id, token_hash, expires_at) VALUES ($1, $2, '\xdeadbeef', now() + interval '1 day')`, uuid.New(), ana)
	exec(t, pool, `INSERT INTO libraries (id, name, kind, paths, scan_state) VALUES ($1, 'Filmovi', 'movies', ARRAY['/media/a', '/media/b c'], 'scanning')`, lib)
	exec(t, pool, `INSERT INTO settings (key, value) VALUES ('test.key', '{"v": 1}')`)
	exec(t, pool, `INSERT INTO settings (key, value) VALUES ('stream.key', '"key-at-backup"')`)
	exec(t, pool, `INSERT INTO audit_log (action) VALUES ('one'), ('two')`)
	var lastAudit int64
	if err := pool.QueryRow(ctx, `SELECT max(id) FROM audit_log`).Scan(&lastAudit); err != nil {
		t.Fatal(err)
	}

	info, err := svc.Create(ctx, CreateOptions{Kind: KindManual})
	if err != nil {
		t.Fatal(err)
	}
	m := info.Manifest
	if !m.ScanRunning {
		t.Error("running scan not recorded")
	}
	if len(m.Skipped) != 0 || m.Files.Config != 1 || m.Files.Avatars != 1 || m.Files.Images != 0 {
		t.Errorf("manifest %+v", m)
	}
	for _, tb := range m.Tables {
		if tb.Name == "goose_db_version" || len(tb.Name) > 6 && tb.Name[:6] == "river_" {
			t.Errorf("bookkeeping table %s dumped", tb.Name)
		}
	}
	if _, err := Verify(filepath.Join(svc.Dir, info.Name)); err != nil {
		t.Fatal(err)
	}
	// A second backup while the first holds the lock is refused, not queued behind it.
	unlock, err := svc.lock(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, CreateOptions{}); !errors.Is(err, ErrBusy) {
		t.Errorf("concurrent backup: %v", err)
	}
	unlock()

	// Change everything.
	ivo := uuid.New()
	exec(t, pool, `DELETE FROM users WHERE id = $1`, ana) // cascades to sessions
	exec(t, pool, `INSERT INTO users (id, username, password_hash) VALUES ($1, 'ivo', 'h')`, ivo)
	exec(t, pool, `UPDATE settings SET value = '{"v": 2}' WHERE key = 'test.key'`)
	exec(t, pool, `UPDATE settings SET value = '"key-now"' WHERE key = 'stream.key'`)
	exec(t, pool, `INSERT INTO audit_log (action) VALUES ('three'), ('four'), ('five')`)
	mustWrite(t, filepath.Join(root, "config", "extra", "settings.yml"), "a: 2")
	if err := os.Remove(filepath.Join(root, "cache", "avatars", "x.jpg")); err != nil {
		t.Fatal(err)
	}

	res, err := svc.Restore(ctx, filepath.Join(svc.Dir, info.Name), RestoreOptions{SafetyBackup: true})
	if err != nil {
		t.Fatal(err, res.Warnings)
	}
	if !res.Changed || res.SafetyBackup == "" || len(res.Warnings) != 0 {
		t.Errorf("result %+v", res)
	}
	got, ok := getUser(t, pool, ana)
	if !ok || got.Username != "ana\ttab\nnewline \\ back šđč 🎬" || got.Hash != "hash\x01" || !got.Admin || got.Locked == nil || !got.Locked.Equal(locked) {
		t.Errorf("ana after restore: %+v %v", got, ok)
	}
	if _, ok := getUser(t, pool, ivo); ok {
		t.Error("user created after the backup survived the restore")
	}
	if n := count(t, pool, `SELECT count(*) FROM sessions WHERE user_id = $1`, ana); n != 1 {
		t.Errorf("sessions %d", n)
	}
	var v string
	if err := pool.QueryRow(ctx, `SELECT value->>'v' FROM settings WHERE key = 'test.key'`).Scan(&v); err != nil || v != "1" {
		t.Errorf("setting %q %v", v, err)
	}
	// The stream signing key is never in a backup; a restore keeps the server's own.
	if err := pool.QueryRow(ctx, `SELECT value #>> '{}' FROM settings WHERE key = 'stream.key'`).Scan(&v); err != nil || v != "key-now" {
		t.Errorf("stream key after restore %q %v", v, err)
	}
	var paths []string
	var state string
	if err := pool.QueryRow(ctx, `SELECT paths, scan_state FROM libraries WHERE id = $1`, lib).Scan(&paths, &state); err != nil ||
		len(paths) != 2 || paths[1] != "/media/b c" || state != "idle" {
		t.Errorf("library %v %q %v", paths, state, err)
	}
	if n := count(t, pool, `SELECT count(*) FROM audit_log`); n != 2 {
		t.Errorf("audit rows %d", n)
	}
	// Sequences continue after the restored rows.
	var next int64
	if err := pool.QueryRow(ctx, `INSERT INTO audit_log (action) VALUES ('after') RETURNING id`).Scan(&next); err != nil || next <= lastAudit {
		t.Errorf("next audit id %d (last %d) %v", next, lastAudit, err)
	}
	// Foreign keys are back and enforced.
	if _, err := pool.Exec(ctx, `INSERT INTO sessions (id, user_id, token_hash, expires_at) VALUES ($1, $2, '\x00', now())`, uuid.New(), uuid.New()); err == nil {
		t.Error("foreign key missing after restore")
	}
	if b, _ := os.ReadFile(filepath.Join(root, "config", "extra", "settings.yml")); string(b) != "a: 1" {
		t.Errorf("config file %q", b)
	}
	if _, err := os.Stat(filepath.Join(root, "cache", "avatars", "x.jpg")); err != nil {
		t.Error("avatar not restored")
	}

	// The safety backup holds the state from just before the restore.
	list, err := svc.List()
	if err != nil {
		t.Fatal(err)
	}
	var safety *Info
	for i := range list {
		if list[i].Name == res.SafetyBackup {
			safety = &list[i]
		}
	}
	if safety == nil || safety.Kind != KindPreRestore {
		t.Fatalf("safety backup missing: %+v", list)
	}
	// Without a key of its own the server gets none from the backup (a fresh one is made at start).
	exec(t, pool, `DELETE FROM settings WHERE key = 'stream.key'`)
	if _, err := svc.Restore(ctx, filepath.Join(svc.Dir, safety.Name), RestoreOptions{}); err != nil {
		t.Fatal(err)
	}
	if n := count(t, pool, `SELECT count(*) FROM settings WHERE key = 'stream.key'`); n != 0 {
		t.Errorf("a stream key came back from a backup: %d", n)
	}
	if _, ok := getUser(t, pool, ivo); !ok {
		t.Error("safety backup did not bring back the later user")
	}
}

// A backup from an older schema is loaded into that schema and then migrated forward.
func TestRestoreOlderSchemaIntegration(t *testing.T) {
	old := scratchDB(t)
	cur := scratchDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	latest, err := db.LatestVersion()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.MigrateTo(ctx, old, latest-1); err != nil {
		t.Fatal(err)
	}
	// Migration numbers may have gaps (reserved by parallel work), so the older
	// schema is whatever latest-1 resolved to.
	previous, err := db.SchemaVersion(ctx, old)
	if err != nil || previous >= latest {
		t.Fatalf("older schema: %d %v", previous, err)
	}
	if err := db.Migrate(ctx, cur); err != nil {
		t.Fatal(err)
	}
	ana := uuid.New()
	exec(t, old, `INSERT INTO users (id, username, password_hash) VALUES ($1, 'ana', 'h')`, ana)
	exec(t, cur, `INSERT INTO users (id, username, password_hash) VALUES ($1, 'ivo', 'h')`, uuid.New())

	dir := t.TempDir()
	src := &Service{Pool: old, Dir: dir, Version: "0.0.1"}
	info, err := src.Create(ctx, CreateOptions{Kind: KindScheduled})
	if err != nil {
		t.Fatal(err)
	}
	if info.Manifest.SchemaVersion != previous {
		t.Fatalf("schema %d", info.Manifest.SchemaVersion)
	}
	dst := &Service{Pool: cur, Dir: dir, Version: "9.9.9"}
	res, err := dst.Restore(ctx, filepath.Join(dir, info.Name), RestoreOptions{SafetyBackup: true, SkipFiles: true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Error("not marked changed")
	}
	if v, err := db.SchemaVersion(ctx, cur); err != nil || v != latest {
		t.Errorf("schema after restore %d %v", v, err)
	}
	if pending, err := db.Pending(ctx, cur); err != nil || pending {
		t.Errorf("pending %v %v", pending, err)
	}
	if n := count(t, cur, `SELECT count(*) FROM users`); n != 1 {
		t.Errorf("users %d", n)
	}
	if _, ok := getUser(t, cur, ana); !ok {
		t.Error("user from the old backup missing")
	}
	// River's tables were rebuilt too.
	if n := count(t, cur, `SELECT count(*) FROM river_job`); n != 0 {
		t.Errorf("river_job %d", n)
	}
}

func exec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A restore recorded as running without anyone holding the lock is marked as failed.
func TestRecoverStatusIntegration(t *testing.T) {
	pool := scratchDB(t)
	svc := &Service{Pool: pool, Dir: t.TempDir()}
	ctx := context.Background()
	if err := svc.WriteStatus(Status{Backup: "a.tar.gz", State: StateRunning, StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	// While the lock is held (a restore runs), the status is left alone.
	unlock, err := svc.lock(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	svc.RecoverStatus(ctx)
	if st, _ := svc.ReadStatus(); st.State != StateRunning {
		t.Fatalf("status changed while locked: %+v", st)
	}
	unlock()
	svc.RecoverStatus(ctx)
	if st, _ := svc.ReadStatus(); st.State != StateFailed || st.FinishedAt == nil || st.Error == "" {
		t.Fatalf("status %+v", st)
	}
}
