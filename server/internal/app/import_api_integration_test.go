package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestImportAPIIntegration(t *testing.T) {
	const key = "source-api-key-123"
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Authorization"), `Token="`+key+`"`) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/System/Info":
			_, _ = w.Write([]byte(`{"Id":"old","ServerName":"Old box","Version":"10.10.7"}`))
		case "/Users":
			_, _ = w.Write([]byte(`[{"Id":"u1","Name":"admin","Policy":{"IsAdministrator":true}},{"Id":"u2","Name":"Guest","Policy":{}}]`))
		case "/Items":
			_, _ = w.Write([]byte(`{"Items":[],"TotalRecordCount":0}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer source.Close()
	srv := newAuthTestServer(t)
	c := newClient(t, srv)
	me := setupAdmin(t, c)

	var e apiError
	if s := c.do("POST", "/import/source", map[string]any{"url": source.URL, "apiKey": "wrong"}, &e); s != 502 || e.Code != "import_key_rejected" {
		t.Fatalf("wrong key: HTTP %d %+v", s, e)
	}
	var info struct {
		ServerName string `json:"serverName"`
		Users      []struct {
			ID              string `json:"id"`
			SuggestedUserID string `json:"suggestedUserId"`
		} `json:"users"`
	}
	if s := c.do("POST", "/import/source", map[string]any{"url": source.URL + "/", "apiKey": key}, &info); s != 200 ||
		info.ServerName != "Old box" || len(info.Users) != 2 {
		t.Fatalf("source: HTTP %d %+v", s, info)
	}
	if info.Users[0].SuggestedUserID != me.User.ID || info.Users[1].SuggestedUserID != "" {
		t.Fatalf("suggested mapping: %+v (admin %s)", info.Users, me.User.ID)
	}

	body := map[string]any{"url": source.URL, "apiKey": key, "dryRun": true,
		"users":        []map[string]any{{"sourceUserId": "u1", "sourceUserName": "admin", "userId": me.User.ID}, {"sourceUserId": "u2", "sourceUserName": "Guest"}},
		"pathMappings": []map[string]any{{"from": "/data", "to": "/etc"}}}
	if s := c.do("POST", "/import/runs", body, &e); s != 400 || e.Code != "invalid_path" {
		t.Fatalf("path outside media folders: HTTP %d %+v", s, e)
	}
	body["pathMappings"] = []map[string]any{{"from": "/data", "to": mediaRoot}}
	var run map[string]any
	if s := c.do("POST", "/import/runs", body, &run); s != 202 {
		t.Fatalf("start: HTTP %d %v", s, run)
	}
	var runs []map[string]any
	if s := c.do("GET", "/import/runs", nil, &runs); s != 200 || len(runs) != 1 {
		t.Fatalf("list: HTTP %d %v", s, runs)
	}
	var got map[string]any
	if s := c.do("GET", "/import/runs/"+run["id"].(string), nil, &got); s != 200 || got["status"] != "done" || got["dryRun"] != true {
		t.Fatalf("run: HTTP %d %v", s, got)
	}
	report := got["report"].(map[string]any)
	if skipped := report["skippedUsers"].([]any); len(skipped) != 1 || skipped[0] != "Guest" {
		t.Fatalf("report: %v", report)
	}
	raw, _ := json.Marshal([]any{run, runs, got})
	if strings.Contains(string(raw), key) {
		t.Fatal("the API key must never be returned")
	}
	var secrets int
	if err := testPool.QueryRow(t.Context(), `SELECT count(*) FROM import_runs WHERE secret IS NOT NULL`).Scan(&secrets); err != nil || secrets != 0 {
		t.Fatalf("stored keys after the run: %d %v", secrets, err)
	}

	// Only administrators may import.
	if s := c.do("POST", "/users", map[string]any{"username": "viewer", "password": "viewer-pass-1"}, nil); s != 201 && s != 200 {
		t.Fatalf("create user: HTTP %d", s)
	}
	v := newClient(t, srv)
	var login authResult
	if s := v.do("POST", "/auth/login", map[string]any{"username": "viewer", "password": "viewer-pass-1", "device": map[string]any{"name": "t"}}, &login); s != 200 {
		t.Fatalf("login: HTTP %d", s)
	}
	v.token = login.Token
	if s := v.do("GET", "/import/runs", nil, nil); s != 403 {
		t.Fatalf("viewer: HTTP %d", s)
	}
}
