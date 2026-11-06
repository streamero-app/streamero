package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/streamero-app/streamero/server/internal/api"
	"github.com/streamero-app/streamero/server/internal/auth"
	"github.com/streamero-app/streamero/server/internal/config"
)

const testSetupCode = "TEST-CODE"

// newAuthTestServer starts the full router on a clean database.
// Integration: needs STREAMERO_TEST_DB_URL / STREAMERO_TEST_VALKEY_URL.
func newAuthTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	dbURL, vkURL := os.Getenv("STREAMERO_TEST_DB_URL"), os.Getenv("STREAMERO_TEST_VALKEY_URL")
	if dbURL == "" || vkURL == "" {
		t.Skip("STREAMERO_TEST_DB_URL / STREAMERO_TEST_VALKEY_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	state := &State{SetupCode: testSetupCode}
	deps, err := Bootstrap(ctx, config.Config{DatabaseURL: dbURL, ValkeyURL: vkURL, AutoMigrate: true}, state, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(deps.Close)
	if _, err := deps.Pool.Exec(ctx, `TRUNCATE users, sessions, audit_log;
		DELETE FROM settings WHERE key IN ('setup.completed', 'server.name')`); err != nil {
		t.Fatal(err)
	}
	h := &api.Handler{State: state, ServerName: "Default", StartedAt: time.Now(), Auth: state.Auth, Settings: state.Settings}
	router, err := NewRouter(state, h)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return srv
}

type client struct {
	t     *testing.T
	base  string
	http  *http.Client
	token string
}

func newClient(t *testing.T, srv *httptest.Server) *client {
	jar, _ := cookiejar.New(nil)
	return &client{t: t, base: srv.URL + "/api/v1", http: &http.Client{Jar: jar}}
}

// do sends a JSON request and decodes the JSON response into out (if non-nil).
func (c *client) do(method, path string, body any, out any) int {
	c.t.Helper()
	var r io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		r = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, c.base+path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if out != nil {
		raw, _ := io.ReadAll(resp.Body)
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, out); err != nil {
				c.t.Fatalf("%s %s: decode %q: %v", method, path, raw, err)
			}
		}
	}
	return resp.StatusCode
}

type authResult struct {
	Token     string `json:"token"`
	SessionID string `json:"sessionId"`
	User      struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		IsAdmin  bool   `json:"isAdmin"`
	} `json:"user"`
}

type apiError struct {
	Code string `json:"code"`
}

func setupAdmin(t *testing.T, c *client) authResult {
	t.Helper()
	var res authResult
	if s := c.do("POST", "/setup", map[string]any{"setupCode": strings.ToLower(testSetupCode), "serverName": "Kino",
		"username": "Admin", "password": "admin-pass-1", "device": map[string]any{"name": "test"}}, &res); s != 200 {
		t.Fatalf("setup: HTTP %d", s)
	}
	c.token = res.Token
	return res
}

func TestSetupFlowIntegration(t *testing.T) {
	srv := newAuthTestServer(t)
	c := newClient(t, srv)

	var info struct {
		Name           string `json:"name"`
		SetupCompleted bool   `json:"setupCompleted"`
	}
	c.do("GET", "/system/info", nil, &info)
	if info.SetupCompleted || info.Name != "Default" {
		t.Fatalf("before setup: %+v", info)
	}

	var e apiError
	if s := c.do("POST", "/setup", map[string]any{"setupCode": "WRONG-CODE", "username": "x", "password": "password1"}, &e); s != 403 || e.Code != "invalid_setup_code" {
		t.Fatalf("wrong code: HTTP %d %+v", s, e)
	}
	if s := c.do("POST", "/setup", map[string]any{"setupCode": testSetupCode, "username": "x", "password": "short"}, &e); s != 400 {
		t.Fatalf("short password: HTTP %d", s)
	}

	res := setupAdmin(t, c)
	if res.Token == "" || !res.User.IsAdmin || res.User.Username != "Admin" {
		t.Fatalf("setup result: %+v", res)
	}
	c.do("GET", "/system/info", nil, &info)
	if !info.SetupCompleted || info.Name != "Kino" {
		t.Fatalf("after setup: %+v", info)
	}
	if s := c.do("POST", "/setup", map[string]any{"setupCode": testSetupCode, "username": "evil", "password": "password1"}, &e); s != 409 || e.Code != "setup_completed" {
		t.Fatalf("second setup: HTTP %d %+v", s, e)
	}
}

func TestLoginSessionsAndCookieIntegration(t *testing.T) {
	srv := newAuthTestServer(t)
	adminC := newClient(t, srv)
	setupAdmin(t, adminC)

	anon := newClient(t, srv)
	var e apiError
	if s := anon.do("GET", "/auth/me", nil, &e); s != 401 || e.Code != "unauthenticated" {
		t.Fatalf("anonymous /auth/me: HTTP %d %+v", s, e)
	}
	if s := anon.do("GET", "/users", nil, nil); s != 401 {
		t.Fatalf("anonymous /users: HTTP %d", s)
	}

	// Create a normal user.
	var u struct {
		ID string `json:"id"`
	}
	if s := adminC.do("POST", "/users", map[string]any{"username": "ana", "password": "ana-pass-1"}, &u); s != 201 {
		t.Fatalf("create user: HTTP %d", s)
	}
	if s := adminC.do("POST", "/users", map[string]any{"username": "ANA", "password": "ana-pass-1"}, &e); s != 409 || e.Code != "username_taken" {
		t.Fatalf("duplicate (case-insensitive) user: HTTP %d %+v", s, e)
	}

	// Web login with cookie: no token in the body, cookie authenticates.
	web := newClient(t, srv)
	var res authResult
	if s := web.do("POST", "/auth/login", map[string]any{"username": "Ana", "password": "ana-pass-1", "useCookie": true,
		"device": map[string]any{"name": "Firefox", "client": "Streamero Web"}}, &res); s != 200 || res.Token != "" {
		t.Fatalf("cookie login: HTTP %d token=%q", s, res.Token)
	}
	var me struct {
		Username string `json:"username"`
		IsAdmin  bool   `json:"isAdmin"`
	}
	if s := web.do("GET", "/auth/me", nil, &me); s != 200 || me.Username != "ana" || me.IsAdmin {
		t.Fatalf("me via cookie: HTTP %d %+v", s, me)
	}
	if s := web.do("GET", "/users", nil, &e); s != 403 || e.Code != "forbidden" {
		t.Fatalf("non-admin /users: HTTP %d %+v", s, e)
	}

	// Sessions: user sees only own; admin sees all and can revoke.
	var sessions []struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Device   string `json:"deviceName"`
		Current  bool   `json:"current"`
	}
	web.do("GET", "/sessions", nil, &sessions)
	if len(sessions) != 1 || !sessions[0].Current || sessions[0].Device != "Firefox" {
		t.Fatalf("own sessions: %+v", sessions)
	}
	adminC.do("GET", "/sessions", nil, &sessions)
	if len(sessions) != 2 {
		t.Fatalf("admin sees %d sessions, want 2", len(sessions))
	}
	var anaSession string
	for _, s := range sessions {
		if s.Username == "ana" {
			anaSession = s.ID
		}
	}
	if s := adminC.do("DELETE", "/sessions/"+anaSession, nil, nil); s != 204 {
		t.Fatalf("revoke: HTTP %d", s)
	}
	if s := web.do("GET", "/auth/me", nil, nil); s != 401 {
		t.Fatalf("revoked session still valid: HTTP %d", s)
	}

	// Logout clears the session.
	web.do("POST", "/auth/login", map[string]any{"username": "ana", "password": "ana-pass-1", "useCookie": true}, nil)
	if s := web.do("POST", "/auth/logout", nil, nil); s != 204 {
		t.Fatalf("logout: HTTP %d", s)
	}
	if s := web.do("GET", "/auth/me", nil, nil); s != 401 {
		t.Fatalf("after logout: HTTP %d", s)
	}
}

func TestLockoutAndUserAdminIntegration(t *testing.T) {
	srv := newAuthTestServer(t)
	adminC := newClient(t, srv)
	admin := setupAdmin(t, adminC)

	var u struct {
		ID string `json:"id"`
	}
	adminC.do("POST", "/users", map[string]any{"username": "bob", "password": "bob-pass-1"}, &u)

	bob := newClient(t, srv)
	var e apiError
	for i := 1; i < auth.MaxFailedLogins; i++ {
		if s := bob.do("POST", "/auth/login", map[string]any{"username": "bob", "password": "nope"}, &e); s != 401 {
			t.Fatalf("attempt %d: HTTP %d", i, s)
		}
	}
	if s := bob.do("POST", "/auth/login", map[string]any{"username": "bob", "password": "nope"}, &e); s != 429 || e.Code != "account_locked" {
		t.Fatalf("lockout: HTTP %d %+v", s, e)
	}
	if s := bob.do("POST", "/auth/login", map[string]any{"username": "bob", "password": "bob-pass-1"}, &e); s != 429 {
		t.Fatalf("correct password while locked: HTTP %d", s)
	}
	// Unknown users get the same answer as wrong passwords.
	if s := bob.do("POST", "/auth/login", map[string]any{"username": "nobody", "password": "x"}, &e); s != 401 || e.Code != "invalid_credentials" {
		t.Fatalf("unknown user: HTTP %d %+v", s, e)
	}

	// Admin resets the password: lock is cleared.
	if s := adminC.do("PATCH", "/users/"+u.ID, map[string]any{"password": "bob-pass-2"}, nil); s != 200 {
		t.Fatalf("reset password: HTTP %d", s)
	}
	var res authResult
	if s := bob.do("POST", "/auth/login", map[string]any{"username": "bob", "password": "bob-pass-2"}, &res); s != 200 {
		t.Fatalf("login after reset: HTTP %d", s)
	}
	bob.token = res.Token

	// Disabling signs bob out immediately.
	if s := adminC.do("PATCH", "/users/"+u.ID, map[string]any{"disabled": true}, nil); s != 200 {
		t.Fatalf("disable: HTTP %d", s)
	}
	if s := bob.do("GET", "/auth/me", nil, nil); s != 401 {
		t.Fatalf("disabled user still signed in: HTTP %d", s)
	}

	// The last admin cannot be demoted or delete themselves.
	if s := adminC.do("PATCH", "/users/"+admin.User.ID, map[string]any{"isAdmin": false}, &e); s != 409 || e.Code != "last_admin" {
		t.Fatalf("demote last admin: HTTP %d %+v", s, e)
	}
	if s := adminC.do("DELETE", "/users/"+admin.User.ID, nil, &e); s != 409 || e.Code != "self" {
		t.Fatalf("delete self: HTTP %d %+v", s, e)
	}
	if s := adminC.do("DELETE", "/users/"+u.ID, nil, nil); s != 204 {
		t.Fatalf("delete bob: HTTP %d", s)
	}

	// Password change signs out other sessions but keeps the current one.
	other := newClient(t, srv)
	other.do("POST", "/auth/login", map[string]any{"username": "admin", "password": "admin-pass-1"}, &res)
	other.token = res.Token
	if s := adminC.do("POST", "/auth/password", map[string]any{"currentPassword": "admin-pass-1", "newPassword": "admin-pass-2"}, nil); s != 204 {
		t.Fatalf("change password: HTTP %d", s)
	}
	if s := other.do("GET", "/auth/me", nil, nil); s != 401 {
		t.Fatalf("other session survived password change: HTTP %d", s)
	}
	if s := adminC.do("GET", "/auth/me", nil, nil); s != 200 {
		t.Fatalf("current session lost after password change: HTTP %d", s)
	}

	// Server settings.
	var st struct {
		ServerName string `json:"serverName"`
	}
	if s := adminC.do("PUT", "/settings/server", map[string]any{"serverName": "  Novi kino "}, &st); s != 200 || st.ServerName != "Novi kino" {
		t.Fatalf("update settings: HTTP %d %+v", s, st)
	}
}
