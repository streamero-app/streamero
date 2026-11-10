package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/streamero-app/streamero/server/internal/db"
)

// writeTestArchive builds a small valid archive with one table and one config file.
func writeTestArchive(t *testing.T, s *Service, name string, mutate func(*Manifest)) string {
	t.Helper()
	tmp := t.TempDir()
	data := []byte("1\tana\n2\tivo\n")
	if err := os.WriteFile(filepath.Join(tmp, "users.tsv"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	latest, err := db.LatestVersion()
	if err != nil {
		t.Fatal(err)
	}
	m := &Manifest{Format: FormatVersion, App: appName, ServerVersion: "1.2.3", SchemaVersion: latest,
		CreatedAt: time.Date(2026, 11, 10, 15, 30, 0, 0, time.UTC), Kind: KindScheduled,
		Tables: []Table{{Name: "users", Columns: []string{"id", "username"}, Rows: 2, Bytes: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}}}
	if mutate != nil {
		mutate(m)
	}
	cfg := filepath.Join(t.TempDir(), "cfg.yml")
	if err := os.WriteFile(cfg, []byte("x: 1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(s.Dir, 0o750); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(s.Dir, name)
	if err := s.writeArchive(p, tmp, m, []archiveFile{{prefix: configPrefix, rel: "sub/cfg.yml", path: cfg}}); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLatestVersion(t *testing.T) {
	v, err := db.LatestVersion()
	if err != nil || v < 21 {
		t.Fatalf("latest = %d, %v", v, err)
	}
}

func TestVerifyAndManifest(t *testing.T) {
	s := &Service{Dir: t.TempDir()}
	p := writeTestArchive(t, s, "streamero-20261110T153000Z-1.2.3.tar.gz", nil)
	m, err := Verify(p)
	if err != nil {
		t.Fatal(err)
	}
	if m.ServerVersion != "1.2.3" || len(m.Tables) != 1 || m.Tables[0].Rows != 2 {
		t.Fatalf("manifest %+v", m)
	}
	if _, err := Prepare(p); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyDetectsDamage(t *testing.T) {
	s := &Service{Dir: t.TempDir()}
	p := writeTestArchive(t, s, "a.tar.gz", func(m *Manifest) { m.Tables[0].SHA256 = strings.Repeat("0", 64) })
	if _, err := Verify(p); !errors.Is(err, ErrInvalid) {
		t.Fatalf("wrong checksum accepted: %v", err)
	}

	p = writeTestArchive(t, s, "b.tar.gz", nil)
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	// Truncated download.
	if err := os.WriteFile(p, raw[:len(raw)-20], 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(p); err == nil {
		t.Fatal("truncated archive accepted")
	}

	junk := filepath.Join(s.Dir, "c.tar.gz")
	if err := os.WriteFile(junk, []byte("not a backup"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(junk); !errors.Is(err, ErrInvalid) {
		t.Fatalf("junk: %v", err)
	}
}

func TestNewerSchemaRefused(t *testing.T) {
	s := &Service{Dir: t.TempDir()}
	p := writeTestArchive(t, s, "streamero-x.tar.gz", func(m *Manifest) { m.SchemaVersion += 5 })
	if _, err := Prepare(p); !errors.Is(err, ErrNewerSchema) {
		t.Fatalf("newer schema: %v", err)
	}
	list, err := s.List()
	if err != nil || len(list) != 1 || list[0].Problem != "newer_schema" || list[0].Restorable() {
		t.Fatalf("list %+v %v", list, err)
	}
}

// tarGz builds an archive by hand, for entries the writer never produces.
func tarGz(t *testing.T, p string, entries map[string]string, order []string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, name := range order {
		body := entries[name]
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(body))
	}
	_ = tw.Close()
	_ = gz.Close()
	if err := os.WriteFile(p, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"format":1,"app":"streamero","schemaVersion":1,"tables":[],"sequences":[]}`
	for _, bad := range []string{"config/../../etc/passwd", "/etc/passwd", "config//x", "other/file", "db/x.tsv"} {
		p := filepath.Join(dir, "t.tar.gz")
		tarGz(t, p, map[string]string{"manifest.json": manifest, bad: "x"}, []string{"manifest.json", bad})
		if _, err := Verify(p); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q accepted: %v", bad, err)
		}
	}
	p := filepath.Join(dir, "ok.tar.gz")
	tarGz(t, p, map[string]string{"manifest.json": manifest, "config/a/b.txt": "x"}, []string{"manifest.json", "config/a/b.txt"})
	if _, err := Verify(p); err != nil {
		t.Fatal(err)
	}
	// The manifest must come first.
	tarGz(t, p, map[string]string{"manifest.json": manifest, "config/a": "x"}, []string{"config/a", "manifest.json"})
	if _, err := Verify(p); !errors.Is(err, ErrInvalid) {
		t.Fatalf("manifest not first: %v", err)
	}
}

func TestPathStaysInBackupDir(t *testing.T) {
	root := t.TempDir()
	s := &Service{Dir: filepath.Join(root, "backups")}
	writeTestArchive(t, s, "streamero-ok.tar.gz", nil)
	if err := os.WriteFile(filepath.Join(root, "secret.tar.gz"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "secret.tar.gz"), filepath.Join(s.Dir, "link.tar.gz")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Path("streamero-ok.tar.gz"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"../secret.tar.gz", "..%2fsecret.tar.gz", "link.tar.gz", "/etc/passwd", "a/../secret.tar.gz", "x.zip", "", ".hidden.tar.gz"} {
		if _, err := s.Path(bad); err == nil {
			t.Errorf("%q resolved", bad)
		}
	}
	if err := s.Delete("../secret.tar.gz"); err == nil {
		t.Fatal("deleted outside the backup folder")
	}
	if _, err := os.Stat(filepath.Join(root, "secret.tar.gz")); err != nil {
		t.Fatal(err)
	}
}

func TestPruneKeepsUploadsAndSafetyBackups(t *testing.T) {
	s := &Service{Dir: t.TempDir()}
	for i, name := range []string{"streamero-1.tar.gz", "streamero-2.tar.gz", "streamero-3.tar.gz",
		"uploaded-1.tar.gz", "streamero-pre-restore-1.tar.gz"} {
		writeTestArchive(t, s, name, func(m *Manifest) { m.CreatedAt = m.CreatedAt.Add(time.Duration(i) * time.Hour) })
	}
	removed, err := s.Prune(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0] != "streamero-1.tar.gz" {
		t.Fatalf("removed %v", removed)
	}
	list, _ := s.List()
	kinds := map[string]string{}
	for _, b := range list {
		kinds[b.Name] = b.Kind
	}
	if len(list) != 4 || kinds["uploaded-1.tar.gz"] != KindUploaded || kinds["streamero-pre-restore-1.tar.gz"] != KindPreRestore || kinds["streamero-3.tar.gz"] != KindScheduled {
		t.Fatalf("left %v", kinds)
	}
	if got := s.Newest(); !got.Equal(time.Date(2026, 11, 10, 17, 30, 0, 0, time.UTC)) {
		t.Fatalf("newest %v", got)
	}
}

func TestImport(t *testing.T) {
	src := &Service{Dir: t.TempDir()}
	p := writeTestArchive(t, src, "x.tar.gz", nil)
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{Dir: filepath.Join(t.TempDir(), "backups"), Now: func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }}
	info, err := s.Import(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "uploaded-20260102T030405Z-1.2.3.tar.gz" || info.Kind != KindUploaded {
		t.Fatalf("info %+v", info)
	}
	if _, err := s.Import(strings.NewReader("garbage")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("garbage: %v", err)
	}
	left, _ := filepath.Glob(filepath.Join(s.Dir, ".upload-*"))
	if len(left) != 0 {
		t.Fatalf("partial uploads left: %v", left)
	}
}

func TestFileNameIsSafeAndUnique(t *testing.T) {
	s := &Service{Dir: t.TempDir()}
	at := time.Date(2026, 11, 10, 15, 30, 0, 0, time.UTC)
	n := s.fileName("streamero", at, "v0.8.3+dirty/../x")
	if n != "streamero-20261110T153000Z-v0.8.3-dirty-.-x.tar.gz" || !validName.MatchString(n) {
		t.Fatalf("name %q", n)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, n), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if n2 := s.fileName("streamero", at, "v0.8.3+dirty/../x"); n2 == n {
		t.Fatal("name not unique")
	}
}

func TestSettingsNormalize(t *testing.T) {
	if got := (Settings{KeepLast: 0}).Normalize().KeepLast; got != 1 {
		t.Fatal(got)
	}
	if got := (Settings{KeepLast: 1000}).Normalize().KeepLast; got != 100 {
		t.Fatal(got)
	}
}

func TestExtractStaysInside(t *testing.T) {
	root := t.TempDir()
	s := &Service{Dir: filepath.Join(root, "config", "backups"), ConfigDir: filepath.Join(root, "config"), CacheDir: filepath.Join(root, "cache")}
	if err := s.extract(configPrefix, "a/b.txt", strings.NewReader("hi")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "config", "a", "b.txt")); string(b) != "hi" {
		t.Fatalf("got %q", b)
	}
	if err := s.extract(configPrefix, "../evil", strings.NewReader("x")); err == nil {
		t.Fatal("escaped")
	}
	// Backups inside the config folder are never overwritten by a restore.
	if err := s.extract(configPrefix, "backups/x.tar.gz", strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "x.tar.gz")); err == nil {
		t.Fatal("wrote into the backup folder")
	}
	// A symlinked folder pointing outside is refused.
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "config", "out")); err != nil {
		t.Fatal(err)
	}
	if err := s.extract(configPrefix, "out/x", strings.NewReader("x")); err == nil {
		t.Fatal("followed a symlink out of the config folder")
	}
}

func TestBackupLeavesOutSecrets(t *testing.T) {
	got := withoutSecrets("import_runs", []string{"id", "status", "secret", "source_url"})
	if slices.Contains(got, "secret") || len(got) != 3 {
		t.Fatalf("import_runs columns: %v", got)
	}
	if got := withoutSecrets("users", []string{"id", "password_hash"}); len(got) != 2 {
		t.Fatalf("other tables must keep their columns: %v", got)
	}
}
