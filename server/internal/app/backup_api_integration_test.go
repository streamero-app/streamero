package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/streamero-app/streamero/server/internal/backup"
)

// The backup endpoints are admin-only and never reach outside the backup folder.
// A full restore drops the schema, so it is tested on a database of its own in
// internal/backup; here the shared database is only read.
func TestBackupAPIIntegration(t *testing.T) {
	srv := newAuthTestServer(t)
	adminC := newClient(t, srv)
	setupAdmin(t, adminC)
	adminC.do("POST", "/users", map[string]any{"username": "bea", "password": "bea-pass-1"}, nil)
	user := newClient(t, srv)
	var res authResult
	user.do("POST", "/auth/login", map[string]any{"username": "bea", "password": "bea-pass-1", "device": map[string]any{"name": "t"}}, &res)
	user.token = res.Token

	for _, c := range []struct{ method, path string }{{"GET", "/backups"}, {"POST", "/backups"}, {"DELETE", "/backups/x.tar.gz"},
		{"GET", "/backups/x.tar.gz/download"}, {"PUT", "/backups/settings"}} {
		var body any
		if c.method == "PUT" {
			body = map[string]any{"enabled": true, "keepLast": 3, "includeImages": false}
		}
		if s := user.do(c.method, c.path, body, nil); s != http.StatusForbidden {
			t.Errorf("non-admin %s %s: HTTP %d", c.method, c.path, s)
		}
	}

	var queued struct {
		ScanRunning bool `json:"scanRunning"`
	}
	ranTasks = nil
	if s := adminC.do("POST", "/backups", nil, &queued); s != http.StatusAccepted || len(ranTasks) != 1 {
		t.Fatalf("back up now: HTTP %d %v", s, ranTasks)
	}

	info, err := testBackups.Create(context.Background(), backup.CreateOptions{Kind: backup.KindManual})
	if err != nil {
		t.Fatal(err)
	}
	var overview struct {
		Backups []struct {
			Name       string `json:"name"`
			Restorable bool   `json:"restorable"`
			Kind       string `json:"kind"`
		} `json:"backups"`
		Settings struct {
			KeepLast int `json:"keepLast"`
		} `json:"settings"`
		SchemaVersion int64 `json:"schemaVersion"`
	}
	if s := adminC.do("GET", "/backups", nil, &overview); s != 200 || len(overview.Backups) != 1 || !overview.Backups[0].Restorable ||
		overview.Backups[0].Name != info.Name || overview.Settings.KeepLast < 1 || overview.SchemaVersion == 0 {
		t.Fatalf("list: HTTP %d %+v", s, overview)
	}

	var st struct {
		KeepLast int `json:"keepLast"`
	}
	if s := adminC.do("PUT", "/backups/settings", map[string]any{"enabled": false, "keepLast": 3, "includeImages": true}, &st); s != 200 || st.KeepLast != 3 {
		t.Fatalf("settings: HTTP %d %+v", s, st)
	}

	// Download, then upload the same bytes: it comes back as an uploaded backup.
	req, _ := http.NewRequest("GET", adminC.base+"/backups/"+info.Name+"/download", nil)
	req.Header.Set("Authorization", "Bearer "+adminC.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Disposition") != `attachment; filename="`+info.Name+`"` || len(raw) != int(info.Size) {
		t.Fatalf("download: HTTP %d %q %d bytes", resp.StatusCode, resp.Header.Get("Content-Disposition"), len(raw))
	}
	upload := func(body []byte) (int, map[string]any) {
		req, _ := http.NewRequest("POST", adminC.base+"/backups/upload", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+adminC.token)
		req.Header.Set("Content-Type", "application/octet-stream")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		out := map[string]any{}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	if s, out := upload(raw); s != http.StatusCreated || out["kind"] != "uploaded" {
		t.Fatalf("upload: HTTP %d %v", s, out)
	}
	if s, out := upload([]byte("not a backup")); s != http.StatusBadRequest || out["code"] != "invalid_backup" {
		t.Fatalf("junk upload: HTTP %d %v", s, out)
	}

	// Nothing outside the backup folder can be named.
	outside := filepath.Join(filepath.Dir(testBackups.Dir), "outside.tar.gz")
	if err := os.WriteFile(outside, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(testBackups.Dir, "link.tar.gz")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"..%2Foutside.tar.gz", "link.tar.gz", "..outside.tar.gz"} {
		if s := adminC.do("GET", "/backups/"+name+"/download", nil, nil); s != 400 && s != 404 {
			t.Errorf("download %q: HTTP %d", name, s)
		}
		if s := adminC.do("DELETE", "/backups/"+name, nil, nil); s != 400 && s != 404 {
			t.Errorf("delete %q: HTTP %d", name, s)
		}
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatal("file outside the backup folder was touched")
	}

	// Restore needs the name typed back; nothing starts without it.
	var e apiError
	if s := adminC.do("POST", "/backups/"+info.Name+"/restore", map[string]any{"confirm": "yes"}, &e); s != 400 || e.Code != "backup_confirm_mismatch" {
		t.Fatalf("restore without confirmation: HTTP %d %+v", s, e)
	}
	if s := adminC.do("DELETE", "/backups/"+info.Name, nil, nil); s != http.StatusNoContent {
		t.Fatalf("delete: HTTP %d", s)
	}
	if s := adminC.do("GET", "/backups/"+info.Name+"/download", nil, nil); s != 404 {
		t.Fatalf("deleted backup still downloadable: HTTP %d", s)
	}
}
