package app

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
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
	if _, err := deps.Pool.Exec(ctx, `TRUNCATE users, sessions, audit_log, api_keys, user_totp, recovery_codes, quick_connect, invites, profiles;
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

func TestAPIKeysAndDeviceRenameIntegration(t *testing.T) {
	srv := newAuthTestServer(t)
	adminC := newClient(t, srv)
	setupAdmin(t, adminC)

	var created struct {
		Key    string `json:"key"`
		APIKey struct {
			ID     string `json:"id"`
			Prefix string `json:"prefix"`
		} `json:"apiKey"`
	}
	if s := adminC.do("POST", "/apikeys", map[string]any{"name": "Jellyseerr"}, &created); s != 201 {
		t.Fatalf("create key: HTTP %d", s)
	}
	if !strings.HasPrefix(created.Key, "sk_") || !strings.HasPrefix(created.Key, created.APIKey.Prefix) {
		t.Fatalf("key %q prefix %q", created.Key, created.APIKey.Prefix)
	}

	// The key authenticates as the creating admin.
	integ := newClient(t, srv)
	integ.token = created.Key
	var me struct {
		Username string `json:"username"`
	}
	if s := integ.do("GET", "/auth/me", nil, &me); s != 200 || me.Username != "Admin" {
		t.Fatalf("me via key: HTTP %d %+v", s, me)
	}
	if s := integ.do("GET", "/users", nil, nil); s != 200 {
		t.Fatalf("admin endpoint via key: HTTP %d", s)
	}
	var e apiError
	if s := integ.do("POST", "/auth/password", map[string]any{"currentPassword": "admin-pass-1", "newPassword": "x-pass-123"}, &e); s != 403 || e.Code != "session_required" {
		t.Fatalf("password change via key: HTTP %d %+v", s, e)
	}

	// Listing never returns the secret; usage is recorded.
	var keys []map[string]any
	adminC.do("GET", "/apikeys", nil, &keys)
	if len(keys) != 1 || keys[0]["lastUsedAt"] == nil {
		t.Fatalf("keys: %+v", keys)
	}
	for _, v := range keys[0] {
		if s, ok := v.(string); ok && s == created.Key {
			t.Fatal("secret leaked in listing")
		}
	}

	// Non-admins cannot manage keys.
	adminC.do("POST", "/users", map[string]any{"username": "eva", "password": "eva-pass-1"}, nil)
	eva := newClient(t, srv)
	var res authResult
	eva.do("POST", "/auth/login", map[string]any{"username": "eva", "password": "eva-pass-1"}, &res)
	eva.token = res.Token
	if s := eva.do("POST", "/apikeys", map[string]any{"name": "x"}, nil); s != 403 {
		t.Fatalf("non-admin create key: HTTP %d", s)
	}

	// Rename own device; eva cannot rename the admin's device.
	if s := eva.do("PATCH", "/sessions/"+res.SessionID, map[string]any{"deviceName": "Evin iPhone"}, nil); s != 204 {
		t.Fatalf("rename own: HTTP %d", s)
	}
	var sessions []struct {
		ID     string `json:"id"`
		Device string `json:"deviceName"`
	}
	eva.do("GET", "/sessions", nil, &sessions)
	if len(sessions) != 1 || sessions[0].Device != "Evin iPhone" {
		t.Fatalf("after rename: %+v", sessions)
	}
	var adminSessions []struct {
		ID string `json:"id"`
	}
	adminC.do("GET", "/sessions", nil, &adminSessions)
	for _, s := range adminSessions {
		if s.ID != res.SessionID {
			if code := eva.do("PATCH", "/sessions/"+s.ID, map[string]any{"deviceName": "pwned"}, nil); code != 404 {
				t.Fatalf("rename foreign session: HTTP %d", code)
			}
		}
	}

	// Revoking the key works immediately.
	if s := adminC.do("DELETE", "/apikeys/"+created.APIKey.ID, nil, nil); s != 204 {
		t.Fatalf("delete key: HTTP %d", s)
	}
	if s := integ.do("GET", "/auth/me", nil, nil); s != 401 {
		t.Fatalf("revoked key still works: HTTP %d", s)
	}
}

// totpNow independently computes the current RFC 6238 code (and the step) for a base32 secret.
func totpNow(t *testing.T, secretB32 string, offsetSteps int64) string {
	t.Helper()
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secretB32)
	if err != nil {
		t.Fatal(err)
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(time.Now().Unix()/30+offsetSteps))
	mac := hmac.New(sha1.New, secret)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(sum[off:off+4])&0x7fffffff)%1_000_000)
}

func TestTwoFactorIntegration(t *testing.T) {
	srv := newAuthTestServer(t)
	adminC := newClient(t, srv)
	setupAdmin(t, adminC)
	var u struct {
		ID string `json:"id"`
	}
	adminC.do("POST", "/users", map[string]any{"username": "mia", "password": "mia-pass-1"}, &u)
	mia := newClient(t, srv)
	var res authResult
	mia.do("POST", "/auth/login", map[string]any{"username": "mia", "password": "mia-pass-1"}, &res)
	mia.token = res.Token

	var e apiError
	if s := mia.do("POST", "/auth/totp/enable", map[string]any{"code": "123456"}, &e); s != 409 || e.Code != "totp_not_started" {
		t.Fatalf("enable before setup: HTTP %d %+v", s, e)
	}
	var setup struct {
		Secret string `json:"secret"`
		URI    string `json:"otpauthUri"`
		QR     string `json:"qrPng"`
	}
	if s := mia.do("POST", "/auth/totp/setup", nil, &setup); s != 200 || setup.Secret == "" || setup.QR == "" ||
		!strings.HasPrefix(setup.URI, "otpauth://totp/Kino:mia?") {
		t.Fatalf("setup: HTTP %d %+v", s, setup)
	}
	if s := mia.do("POST", "/auth/totp/enable", map[string]any{"code": "000000"}, &e); s != 401 || e.Code != "invalid_totp" {
		t.Fatalf("enable with wrong code: HTTP %d %+v", s, e)
	}
	var rc struct {
		Codes []string `json:"recoveryCodes"`
	}
	enableCode := totpNow(t, setup.Secret, 0)
	if s := mia.do("POST", "/auth/totp/enable", map[string]any{"code": enableCode}, &rc); s != 200 || len(rc.Codes) != 10 {
		t.Fatalf("enable: HTTP %d %d codes", s, len(rc.Codes))
	}
	var me struct {
		TOTP bool `json:"totpEnabled"`
	}
	if mia.do("GET", "/auth/me", nil, &me); !me.TOTP {
		t.Fatal("me.totpEnabled false after enabling")
	}

	login := func(code string) (int, apiError) {
		var e apiError
		c := newClient(t, srv)
		body := map[string]any{"username": "mia", "password": "mia-pass-1"}
		if code != "" {
			body["totpCode"] = code
		}
		return c.do("POST", "/auth/login", body, &e), e
	}
	if s, e := login(""); s != 401 || e.Code != "totp_required" {
		t.Fatalf("login without code: HTTP %d %+v", s, e)
	}
	if s, e := login("999999"); s != 401 || e.Code != "invalid_totp" {
		t.Fatalf("login with wrong code: HTTP %d %+v", s, e)
	}
	// The code used to enable 2FA cannot be replayed for a login.
	if s, _ := login(enableCode); s == 200 {
		t.Fatal("enable code replayed for login")
	}
	// The next step (clock drift tolerance) works once.
	next := totpNow(t, setup.Secret, 1)
	if s, e := login(next); s != 200 {
		t.Fatalf("login with valid code: HTTP %d %+v", s, e)
	}
	if s, _ := login(next); s == 200 {
		t.Fatal("same code accepted twice")
	}
	// Recovery codes work once, in any formatting.
	if s, e := login(strings.ToUpper(rc.Codes[0])); s != 200 {
		t.Fatalf("recovery code: HTTP %d %+v", s, e)
	}
	if s, _ := login(rc.Codes[0]); s == 200 {
		t.Fatal("recovery code accepted twice")
	}

	// Disabling needs the password.
	if s := mia.do("POST", "/auth/totp/disable", map[string]any{"password": "wrong"}, &e); s != 401 {
		t.Fatalf("disable with wrong password: HTTP %d", s)
	}
	// Admin reset (lost phone) turns 2FA off and signs mia out.
	if s := adminC.do("PATCH", "/users/"+u.ID, map[string]any{"resetTwoFactor": true}, nil); s != 200 {
		t.Fatalf("admin reset: HTTP %d", s)
	}
	if s := mia.do("GET", "/auth/me", nil, nil); s != 401 {
		t.Fatalf("session survived 2FA reset: HTTP %d", s)
	}
	if s, e := login(""); s != 200 {
		t.Fatalf("login after reset: HTTP %d %+v", s, e)
	}
}

func TestQuickConnectIntegration(t *testing.T) {
	srv := newAuthTestServer(t)
	adminC := newClient(t, srv)
	setupAdmin(t, adminC)

	tv := newClient(t, srv)
	var start struct {
		Code   string `json:"code"`
		Secret string `json:"secret"`
	}
	if s := tv.do("POST", "/quickconnect/initiate", map[string]any{"name": "Dnevni boravak TV", "client": "Streamero iOS"}, &start); s != 200 || len(start.Code) != 6 {
		t.Fatalf("initiate: HTTP %d %+v", s, start)
	}
	if s := tv.do("POST", "/quickconnect/poll", map[string]any{"secret": start.Secret}, nil); s != 202 {
		t.Fatalf("poll before approval: HTTP %d", s)
	}
	if s := tv.do("POST", "/quickconnect/authorize", map[string]any{"code": start.Code}, nil); s != 401 {
		t.Fatalf("anonymous authorize: HTTP %d", s)
	}
	var e apiError
	if s := adminC.do("POST", "/quickconnect/authorize", map[string]any{"code": "000000x"}, &e); s != 404 {
		t.Fatalf("unknown code: HTTP %d %+v", s, e)
	}
	var ok struct {
		Device string `json:"deviceName"`
	}
	if s := adminC.do("POST", "/quickconnect/authorize", map[string]any{"code": start.Code}, &ok); s != 200 || ok.Device != "Dnevni boravak TV" {
		t.Fatalf("authorize: HTTP %d %+v", s, ok)
	}
	if s := adminC.do("POST", "/quickconnect/authorize", map[string]any{"code": start.Code}, nil); s != 404 {
		t.Fatalf("approving twice: HTTP %d", s)
	}
	var res authResult
	if s := tv.do("POST", "/quickconnect/poll", map[string]any{"secret": start.Secret}, &res); s != 200 || res.Token == "" || res.User.Username != "Admin" {
		t.Fatalf("poll after approval: HTTP %d %+v", s, res)
	}
	tv.token = res.Token
	if s := tv.do("GET", "/auth/me", nil, nil); s != 200 {
		t.Fatalf("quick connect session: HTTP %d", s)
	}
	// The secret works only once.
	if s := newClient(t, srv).do("POST", "/quickconnect/poll", map[string]any{"secret": start.Secret}, nil); s != 404 {
		t.Fatalf("second poll: HTTP %d", s)
	}
	var sessions []struct {
		Device string `json:"deviceName"`
		Client string `json:"client"`
	}
	tv.do("GET", "/sessions", nil, &sessions)
	found := false
	for _, s := range sessions {
		found = found || (s.Device == "Dnevni boravak TV" && s.Client == "Streamero iOS")
	}
	if !found {
		t.Fatalf("device not in session list: %+v", sessions)
	}
}

func TestPermissionsAndMaxSessionsIntegration(t *testing.T) {
	srv := newAuthTestServer(t)
	adminC := newClient(t, srv)
	setupAdmin(t, adminC)

	type perms map[string]any
	var u struct {
		ID    string `json:"id"`
		Perms perms  `json:"permissions"`
	}
	if s := adminC.do("POST", "/users", map[string]any{"username": "leo", "password": "leo-pass-1"}, &u); s != 201 {
		t.Fatalf("create: HTTP %d", s)
	}
	if u.Perms["download"] != true || u.Perms["deleteMedia"] != false || u.Perms["maxSessions"] != float64(0) {
		t.Fatalf("defaults: %+v", u.Perms)
	}

	p := u.Perms
	p["download"] = false
	p["maxSessions"] = 2
	if s := adminC.do("PATCH", "/users/"+u.ID, map[string]any{"permissions": p}, &u); s != 200 || u.Perms["download"] != false {
		t.Fatalf("update perms: HTTP %d %+v", s, u.Perms)
	}
	var e apiError
	p["maxSessions"] = 1000
	if s := adminC.do("PATCH", "/users/"+u.ID, map[string]any{"permissions": p}, &e); s != 400 {
		t.Fatalf("out-of-range maxSessions: HTTP %d", s)
	}

	login := func(device string) *client {
		c := newClient(t, srv)
		var res authResult
		if s := c.do("POST", "/auth/login", map[string]any{"username": "leo", "password": "leo-pass-1",
			"device": map[string]any{"name": device}}, &res); s != 200 {
			t.Fatalf("login %s: HTTP %d", device, s)
		}
		c.token = res.Token
		return c
	}
	first := login("first")
	second := login("second")
	var me struct {
		Perms perms `json:"permissions"`
	}
	if second.do("GET", "/auth/me", nil, &me); me.Perms["download"] != false {
		t.Fatalf("me permissions: %+v", me.Perms)
	}
	third := login("third") // limit 2: the least recently used session (first) is signed out
	if s := first.do("GET", "/auth/me", nil, nil); s != 401 {
		t.Fatalf("oldest session survived: HTTP %d", s)
	}
	for _, c := range []*client{second, third} {
		if s := c.do("GET", "/auth/me", nil, nil); s != 200 {
			t.Fatalf("recent session lost: HTTP %d", s)
		}
	}
	// Lowering the limit trims existing sessions immediately.
	p["maxSessions"] = 1
	adminC.do("PATCH", "/users/"+u.ID, map[string]any{"permissions": p}, nil)
	alive := 0
	for _, c := range []*client{second, third} {
		if c.do("GET", "/auth/me", nil, nil) == 200 {
			alive++
		}
	}
	if alive != 1 {
		t.Fatalf("%d sessions alive after lowering limit to 1", alive)
	}
}

func TestInvitesIntegration(t *testing.T) {
	srv := newAuthTestServer(t)
	adminC := newClient(t, srv)
	setupAdmin(t, adminC)

	perms := map[string]any{"allLibraries": true, "download": false, "videoTranscoding": true, "audioTranscoding": true,
		"remux": true, "liveTv": false, "manageLiveTv": false, "syncPlay": true, "deleteMedia": false,
		"manageCollections": false, "editSubtitles": false, "remoteControl": true, "maxSessions": 2}
	var created struct {
		Token  string `json:"token"`
		Invite struct {
			ID string `json:"id"`
		} `json:"invite"`
	}
	if s := adminC.do("POST", "/invites", map[string]any{"note": "obitelj", "expiresInHours": 24, "maxUses": 1, "permissions": perms}, &created); s != 201 || created.Token == "" {
		t.Fatalf("create invite: HTTP %d", s)
	}
	anon := newClient(t, srv)
	var info struct {
		ServerName string `json:"serverName"`
	}
	if s := anon.do("GET", "/invite/"+created.Token, nil, &info); s != 200 || info.ServerName != "Kino" {
		t.Fatalf("check invite: HTTP %d %+v", s, info)
	}
	if s := anon.do("GET", "/invite/not-a-token", nil, nil); s != 404 {
		t.Fatalf("bad token: HTTP %d", s)
	}
	var res authResult
	if s := anon.do("POST", "/invite/"+created.Token, map[string]any{"username": "Teta", "password": "teta-pass-1"}, &res); s != 200 || res.User.IsAdmin {
		t.Fatalf("accept: HTTP %d %+v", s, res)
	}
	anon.token = res.Token
	var me struct {
		Perms map[string]any `json:"permissions"`
	}
	if anon.do("GET", "/auth/me", nil, &me); me.Perms["download"] != false || me.Perms["maxSessions"] != float64(2) {
		t.Fatalf("invite permissions not applied: %+v", me.Perms)
	}
	// Single use: the link is now spent.
	if s := newClient(t, srv).do("POST", "/invite/"+created.Token, map[string]any{"username": "Drugi", "password": "drugi-pass-1"}, nil); s != 404 {
		t.Fatalf("second use: HTTP %d", s)
	}
	var list []struct {
		Uses int `json:"uses"`
	}
	if adminC.do("GET", "/invites", nil, &list); len(list) != 1 || list[0].Uses != 1 {
		t.Fatalf("invite list: %+v", list)
	}
	if s := adminC.do("DELETE", "/invites/"+created.Invite.ID, nil, nil); s != 204 {
		t.Fatalf("delete invite: HTTP %d", s)
	}
	if s := anon.do("POST", "/invites", map[string]any{"expiresInHours": 1, "maxUses": 1}, nil); s != 403 {
		t.Fatalf("non-admin create invite: HTTP %d", s)
	}
}

func TestProfilesIntegration(t *testing.T) {
	srv := newAuthTestServer(t)
	adminC := newClient(t, srv)
	setupAdmin(t, adminC)

	var kid, mama struct {
		ID     string `json:"id"`
		HasPin bool   `json:"hasPin"`
	}
	if s := adminC.do("POST", "/profiles", map[string]any{"name": "Lea", "color": "#3DD68C", "isKid": true, "maxRating": 7}, &kid); s != 201 {
		t.Fatalf("create kid: HTTP %d", s)
	}
	if s := adminC.do("POST", "/profiles", map[string]any{"name": "Mama", "color": "#e5484d", "isKid": false, "pin": "1234"}, &mama); s != 201 || !mama.HasPin {
		t.Fatalf("create pin profile: HTTP %d %+v", s, mama)
	}
	var e apiError
	if s := adminC.do("POST", "/profiles", map[string]any{"name": "x", "color": "red", "isKid": false}, &e); s != 400 {
		t.Fatalf("bad colour: HTTP %d", s)
	}

	// PIN protection with lockout.
	if s := adminC.do("POST", "/profiles/"+mama.ID+"/select", map[string]any{}, &e); s != 401 || e.Code != "pin_required" {
		t.Fatalf("select without pin: HTTP %d %+v", s, e)
	}
	if s := adminC.do("POST", "/profiles/"+mama.ID+"/select", map[string]any{"pin": "1234"}, nil); s != 204 {
		t.Fatalf("select with pin: HTTP %d", s)
	}
	var me struct {
		Profile *struct {
			Name string `json:"name"`
		} `json:"currentProfile"`
	}
	if adminC.do("GET", "/auth/me", nil, &me); me.Profile == nil || me.Profile.Name != "Mama" {
		t.Fatalf("current profile: %+v", me.Profile)
	}

	// Kids profile: no settings, no admin, leaving needs the password.
	if s := adminC.do("POST", "/profiles/"+kid.ID+"/select", nil, nil); s != 204 {
		t.Fatalf("select kid: HTTP %d", s)
	}
	for _, c := range []struct{ method, path string }{{"GET", "/users"}, {"GET", "/sessions"}, {"POST", "/apikeys"}, {"POST", "/profiles"}} {
		var body any
		if c.method == "POST" {
			body = map[string]any{"name": "x", "color": "#000000", "isKid": false}
		}
		if s := adminC.do(c.method, c.path, body, &e); s != 403 {
			t.Fatalf("%s %s from kids profile: HTTP %d", c.method, c.path, s)
		}
	}
	if s := adminC.do("POST", "/profiles/main/select", map[string]any{"password": "wrong"}, &e); s != 401 {
		t.Fatalf("leave kids profile with wrong password: HTTP %d", s)
	}
	if s := adminC.do("POST", "/profiles/main/select", map[string]any{"password": "admin-pass-1"}, nil); s != 204 {
		t.Fatalf("leave kids profile: HTTP %d", s)
	}
	me.Profile = nil // json.Unmarshal leaves pointers untouched when the field is absent
	if adminC.do("GET", "/auth/me", nil, &me); me.Profile != nil {
		t.Fatalf("still in a profile: %+v", me.Profile)
	}
	for i := 0; i < 5; i++ {
		adminC.do("POST", "/profiles/"+mama.ID+"/select", map[string]any{"pin": "0000"}, &e)
	}
	if e.Code != "profile_locked" {
		t.Fatalf("PIN lockout: %+v", e)
	}
	// Deleting a profile drops sessions back to the main profile.
	if s := adminC.do("DELETE", "/profiles/"+kid.ID, nil, nil); s != 204 {
		t.Fatalf("delete profile: HTTP %d", s)
	}
	var list []map[string]any
	if adminC.do("GET", "/profiles", nil, &list); len(list) != 1 {
		t.Fatalf("profiles after delete: %+v", list)
	}
}
