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
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/streamero-app/streamero/server/internal/api"
	"github.com/streamero-app/streamero/server/internal/auth"
	"github.com/streamero-app/streamero/server/internal/compat"
	"github.com/streamero-app/streamero/server/internal/config"
	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/stream"
)

const testSetupCode = "TEST-CODE"

// mediaRoot is the media folder of the most recent test server.
var mediaRoot string

// testPool is the database pool of the most recent test server.
var testPool *pgxpool.Pool

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
	mediaRoot = t.TempDir()
	state.SetLibrary(&library.Service{Pool: deps.Pool, Roots: library.Roots{mediaRoot},
		Enqueue: func(context.Context, uuid.UUID) error { return nil }})
	testPool = deps.Pool
	state.SetStream(&stream.Server{Pool: deps.Pool, Roots: library.Roots{mediaRoot}, Signer: stream.Signer{Key: []byte("test-key")},
		HLS: &stream.Manager{Dir: t.TempDir(), FFmpeg: "ffmpeg-not-installed", Log: log}, FFmpeg: "ffmpeg-not-installed"})
	state.SetCompat(&compat.Server{Pool: deps.Pool, Auth: state.Auth(), Libraries: state.Library(), ServerID: state.ServerID(),
		Log: log, ServerName: func(context.Context) string { return "Kino" }})
	h := &api.Handler{State: state, ServerName: "Default", StartedAt: time.Now(), Auth: state.Auth, Settings: state.Settings,
		Library: state.Library, Stream: state.Stream}
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

func TestLibrariesAPIIntegration(t *testing.T) {
	srv := newAuthTestServer(t)
	adminC := newClient(t, srv)
	setupAdmin(t, adminC)
	movies := mediaRoot + "/movies"
	if err := os.MkdirAll(movies+"/Some Movie (2020)", 0o755); err != nil {
		t.Fatal(err)
	}

	var listing struct {
		Path        string   `json:"path"`
		Directories []string `json:"directories"`
	}
	if s := adminC.do("GET", "/fs/directories", nil, &listing); s != 200 || len(listing.Directories) != 1 || listing.Directories[0] != mediaRoot {
		t.Fatalf("roots: HTTP %d %+v", s, listing)
	}
	if s := adminC.do("GET", "/fs/directories?path="+mediaRoot, nil, &listing); s != 200 || len(listing.Directories) != 1 {
		t.Fatalf("browse: HTTP %d %+v", s, listing)
	}
	var e apiError
	if s := adminC.do("GET", "/fs/directories?path=/etc", nil, &e); s != 400 || e.Code != "invalid_path" {
		t.Fatalf("browse outside roots: HTTP %d %+v", s, e)
	}
	if s := adminC.do("GET", "/fs/directories?path="+mediaRoot+"/../../etc", nil, &e); s != 400 {
		t.Fatalf("traversal: HTTP %d", s)
	}
	if s := adminC.do("POST", "/libraries", map[string]any{"name": "X", "kind": "movies", "paths": []string{"/etc"}}, &e); s != 400 || e.Code != "invalid_path" {
		t.Fatalf("library outside roots: HTTP %d %+v", s, e)
	}
	if s := adminC.do("POST", "/libraries", map[string]any{"name": "X", "kind": "music", "paths": []string{movies}}, &e); s != 400 || e.Code != "unsupported_kind" {
		t.Fatalf("music library: HTTP %d %+v", s, e)
	}
	var lib struct {
		ID   string `json:"id"`
		Scan struct {
			State string `json:"state"`
		} `json:"scan"`
	}
	if s := adminC.do("POST", "/libraries", map[string]any{"name": "Filmovi", "kind": "movies", "paths": []string{movies}}, &lib); s != 201 || lib.Scan.State != "queued" {
		t.Fatalf("create: HTTP %d %+v", s, lib)
	}
	if s := adminC.do("POST", "/libraries/"+lib.ID+"/scan", nil, nil); s != 202 {
		t.Fatalf("scan: HTTP %d", s)
	}
	var page struct {
		Total int `json:"total"`
	}
	if s := adminC.do("GET", "/libraries/"+lib.ID+"/items?limit=10&sort=added", nil, &page); s != 200 {
		t.Fatalf("items: HTTP %d", s)
	}

	// A user without the "all libraries" permission sees nothing.
	perms := map[string]any{"allLibraries": false, "download": true, "videoTranscoding": true, "audioTranscoding": true,
		"remux": true, "liveTv": true, "manageLiveTv": false, "syncPlay": true, "deleteMedia": false,
		"manageCollections": false, "editSubtitles": false, "remoteControl": true, "maxSessions": 0}
	adminC.do("POST", "/users", map[string]any{"username": "kid", "password": "kid-pass-1", "permissions": perms}, nil)
	adminC.do("POST", "/users", map[string]any{"username": "eva", "password": "eva-pass-1"}, nil)
	// Other packages may create libraries in the same database concurrently: check by ID.
	for name, wantOurs := range map[string]bool{"kid": false, "eva": true} {
		c := newClient(t, srv)
		var res authResult
		c.do("POST", "/auth/login", map[string]any{"username": name, "password": name + "-pass-1"}, &res)
		c.token = res.Token
		var libs []map[string]any
		if s := c.do("GET", "/libraries", nil, &libs); s != 200 {
			t.Fatalf("%s libraries: HTTP %d", name, s)
		}
		sees := false
		for _, l := range libs {
			sees = sees || l["id"] == lib.ID
		}
		if sees != wantOurs || (!wantOurs && len(libs) != 0) {
			t.Fatalf("%s sees our library: %v (total %d), want %v", name, sees, len(libs), wantOurs)
		}
		if name == "kid" {
			if s := c.do("GET", "/libraries/"+lib.ID+"/items", nil, nil); s != 404 {
				t.Fatalf("kid items: HTTP %d", s)
			}
		}
		if s := c.do("POST", "/libraries/"+lib.ID+"/scan", nil, nil); s != 403 {
			t.Fatalf("%s scan: HTTP %d", name, s)
		}
	}
	if s := adminC.do("DELETE", "/libraries/"+lib.ID, nil, nil); s != 204 {
		t.Fatalf("delete: HTTP %d", s)
	}
	if _, err := os.Stat(movies + "/Some Movie (2020)"); err != nil {
		t.Fatal("deleting a library touched the files")
	}
}

func TestPlaybackIntegration(t *testing.T) {
	srv := newAuthTestServer(t)
	adminC := newClient(t, srv)
	setupAdmin(t, adminC)
	movies := mediaRoot + "/movies"
	payload := []byte("0123456789abcdefghij")
	if err := os.MkdirAll(movies+"/Film (2020)", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(movies+"/Film (2020)/Film (2020).mp4", payload, 0o644); err != nil {
		t.Fatal(err)
	}
	var lib struct {
		ID string `json:"id"`
	}
	adminC.do("POST", "/libraries", map[string]any{"name": "P", "kind": "movies", "paths": []string{movies}}, &lib)
	libID := uuid.MustParse(lib.ID)
	scanner := &library.Scanner{Pool: testPool, Roots: library.Roots{mediaRoot}, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Probe: func(context.Context, string) (library.ProbeResult, error) {
			return library.ProbeResult{Container: "mp4", VideoCodec: "h264", Width: 1920, Height: 1080, DurationMS: 100_000,
				Audio: []library.AudioStream{{Codec: "aac", Channels: 2}}}, nil
		}}
	if _, err := scanner.Scan(context.Background(), libID); err != nil {
		t.Fatal(err)
	}
	var page struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	adminC.do("GET", "/libraries/"+lib.ID+"/items", nil, &page)
	item := page.Items[0].ID

	web := map[string]any{"containers": []string{"mp4", "webm"}, "videoCodecs": []string{"h264"}, "audioCodecs": []string{"aac"}, "hls": true}
	var info struct {
		Method  string `json:"method"`
		URL     string `json:"url"`
		StartMs int64  `json:"startMs"`
	}
	if s := adminC.do("POST", "/items/"+item+"/playback", map[string]any{"device": web}, &info); s != 200 || info.Method != "direct" {
		t.Fatalf("playback: HTTP %d %+v", s, info)
	}
	get := func(u string, rng string) (int, string) {
		req, _ := http.NewRequest("GET", srv.URL+u, nil)
		if rng != "" {
			req.Header.Set("Range", rng)
		}
		resp, err := http.DefaultClient.Do(req) // no cookie, no token: the URL itself authorises
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	if code, body := get(info.URL, "bytes=10-13"); code != 206 || body != "abcd" {
		t.Fatalf("range request: %d %q", code, body)
	}
	if code, _ := get(strings.Replace(info.URL, "sig=", "sig=x", 1), ""); code != 403 {
		t.Fatalf("tampered signature: %d", code)
	}
	// A signature for one file does not open another.
	other := strings.Replace(info.URL, "/stream/file/", "/stream/file/00000000-0000-0000-0000-000000000000/x/../", 1)
	if code, _ := get(other, ""); code != 403 && code != 404 {
		t.Fatalf("signature reused for another path: %d", code)
	}

	// mkv needs remux: without ffmpeg the transcoder fails cleanly.
	var e apiError
	mkvOnly := map[string]any{"containers": []string{"webm"}, "videoCodecs": []string{"h264"}, "audioCodecs": []string{"aac"}, "hls": true}
	if s := adminC.do("POST", "/items/"+item+"/playback", map[string]any{"device": mkvOnly}, &e); s != 502 || e.Code != "transcode_failed" {
		t.Fatalf("remux without ffmpeg: HTTP %d %+v", s, e)
	}

	// Progress → continue watching → resume position; >90% marks as watched.
	if s := adminC.do("PUT", "/items/"+item+"/progress", map[string]any{"positionMs": 30_000}, nil); s != 204 {
		t.Fatalf("progress: HTTP %d", s)
	}
	var cont []struct {
		ID         string `json:"id"`
		PositionMs int64  `json:"positionMs"`
	}
	if adminC.do("GET", "/home/continue", nil, &cont); len(cont) != 1 || cont[0].PositionMs != 30_000 {
		t.Fatalf("continue watching: %+v", cont)
	}
	adminC.do("POST", "/items/"+item+"/playback", map[string]any{"device": web}, &info)
	if info.StartMs != 30_000 {
		t.Fatalf("resume position: %d", info.StartMs)
	}
	adminC.do("PUT", "/items/"+item+"/progress", map[string]any{"positionMs": 95_000}, nil)
	if adminC.do("GET", "/home/continue", nil, &cont); len(cont) != 0 {
		t.Fatalf("watched item still in continue: %+v", cont)
	}
	var detail struct {
		Item struct {
			Played bool `json:"played"`
		} `json:"item"`
	}
	if adminC.do("GET", "/items/"+item, nil, &detail); !detail.Item.Played {
		t.Fatal("item not marked as played")
	}
	// Progress is per profile: a new profile starts fresh.
	var prof struct {
		ID string `json:"id"`
	}
	adminC.do("POST", "/profiles", map[string]any{"name": "Lea", "color": "#3dd68c", "isKid": false}, &prof)
	adminC.do("POST", "/profiles/"+prof.ID+"/select", nil, nil)
	detail.Item.Played = false // absent fields keep old values when decoding
	if adminC.do("GET", "/items/"+item, nil, &detail); detail.Item.Played {
		t.Fatal("watched state leaked into another profile")
	}
	_ = adminC.do("DELETE", "/libraries/"+lib.ID, nil, nil)
}

func TestBrowseIntegration(t *testing.T) {
	srv := newAuthTestServer(t)
	adminC := newClient(t, srv)
	setupAdmin(t, adminC)
	root := mediaRoot + "/m"
	for _, f := range []string{"The Matrix (1999)/The Matrix (1999).mkv", "Amelie (2001)/Amelie (2001).mkv",
		"Show (2020)/Season 01/Show S01E01.mkv", "Show (2020)/Season 01/Show S01E02.mkv"} {
		if err := os.MkdirAll(filepath.Dir(root+"/"+f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(root+"/"+f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var lib struct {
		ID string `json:"id"`
	}
	// Search is global: remove leftovers of earlier failed runs and always clean up.
	if _, err := testPool.Exec(context.Background(), `DELETE FROM libraries WHERE name = 'browse-test'`); err != nil {
		t.Fatal(err)
	}
	adminC.do("POST", "/libraries", map[string]any{"name": "browse-test", "kind": "mixed", "paths": []string{root}}, &lib)
	libID := uuid.MustParse(lib.ID)
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM libraries WHERE id = $1`, libID) })
	scanner := &library.Scanner{Pool: testPool, Roots: library.Roots{mediaRoot}, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Probe: func(context.Context, string) (library.ProbeResult, error) {
			return library.ProbeResult{Container: "mkv"}, nil
		}}
	if _, err := scanner.Scan(context.Background(), libID); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		if _, err := testPool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE items SET genres = '{Action,"Science Fiction"}', community_rating = 8.7 WHERE library_id = $1 AND title = 'The Matrix'`, libID)
	exec(`UPDATE items SET genres = '{Comedy,Romance}', community_rating = 7.9, metadata_title = 'Amélie' WHERE library_id = $1 AND title = 'Amelie'`, libID)
	exec(`INSERT INTO item_people (item_id, position, kind, name, role) SELECT id, 0, 'cast', 'Keanu Reeves', 'Neo' FROM items WHERE library_id = $1 AND title = 'The Matrix'`, libID)

	type sum struct {
		ID       string `json:"id"`
		Title    string `json:"title"`
		Kind     string `json:"kind"`
		Played   bool   `json:"played"`
		Favorite bool   `json:"favorite"`
	}
	search := func(q string) []sum {
		var out []sum
		if s := adminC.do("GET", "/search?q="+url.QueryEscape(q), nil, &out); s != 200 {
			t.Fatalf("search %q: HTTP %d", q, s)
		}
		return out
	}
	if r := search("matirx"); len(r) == 0 || r[0].Title != "The Matrix" {
		t.Fatalf("typo search: %+v", r)
	}
	if r := search("keanu"); len(r) != 1 || r[0].Title != "The Matrix" {
		t.Fatalf("person search: %+v", r)
	}
	if r := search("amélie"); len(r) == 0 || r[0].Title != "Amélie" {
		t.Fatalf("metadata title search: %+v", r)
	}
	if r := search("%_%"); len(r) != 0 { // only a literal % in a title could match
		t.Fatalf("wildcards not escaped: %+v", r)
	}

	var genres []string
	adminC.do("GET", "/libraries/"+lib.ID+"/genres", nil, &genres)
	if strings.Join(genres, ",") != "Action,Comedy,Romance,Science Fiction" {
		t.Fatalf("genres: %v", genres)
	}
	var page struct {
		Items []sum `json:"items"`
		Total int   `json:"total"`
	}
	adminC.do("GET", "/libraries/"+lib.ID+"/items?genre="+url.QueryEscape("Science Fiction"), nil, &page)
	if page.Total != 1 || page.Items[0].Title != "The Matrix" {
		t.Fatalf("genre filter: %+v", page)
	}
	adminC.do("GET", "/libraries/"+lib.ID+"/items?sort=rating", nil, &page)
	if page.Items[0].Title != "The Matrix" || page.Items[1].Title != "Amélie" {
		t.Fatalf("rating sort: %+v", page.Items)
	}

	// Mark the series watched: its episodes follow; the unplayed filter hides it.
	var showID, matrixID string
	for _, it := range page.Items {
		switch it.Title {
		case "Show":
			showID = it.ID
		case "The Matrix":
			matrixID = it.ID
		}
	}
	if s := adminC.do("PUT", "/items/"+showID+"/user-data", map[string]any{"played": true}, nil); s != 204 {
		t.Fatalf("mark played: HTTP %d", s)
	}
	var detail struct {
		Children []sum `json:"children"`
	}
	adminC.do("GET", "/items/"+showID, nil, &detail)
	var seasonDetail struct {
		Children []sum `json:"children"`
	}
	adminC.do("GET", "/items/"+detail.Children[0].ID, nil, &seasonDetail)
	if len(seasonDetail.Children) != 2 || !seasonDetail.Children[0].Played || !seasonDetail.Children[1].Played {
		t.Fatalf("episodes not marked: %+v", seasonDetail.Children)
	}
	adminC.do("GET", "/libraries/"+lib.ID+"/items?unplayed=true", nil, &page)
	if page.Total != 2 {
		t.Fatalf("unplayed filter: %+v", page.Items)
	}

	// Favorites and recently added.
	adminC.do("PUT", "/items/"+matrixID+"/user-data", map[string]any{"favorite": true}, nil)
	var favs []sum
	if adminC.do("GET", "/home/favorites", nil, &favs); len(favs) != 1 || favs[0].Title != "The Matrix" || !favs[0].Favorite {
		t.Fatalf("favorites: %+v", favs)
	}
	adminC.do("PUT", "/items/"+matrixID+"/user-data", map[string]any{"favorite": false}, nil)
	favs = nil
	if adminC.do("GET", "/home/favorites", nil, &favs); len(favs) != 0 {
		t.Fatalf("unfavorite: %+v", favs)
	}
	var latest []sum
	if adminC.do("GET", "/home/latest", nil, &latest); len(latest) < 3 {
		t.Fatalf("recently added: %+v", latest)
	}
	_ = adminC.do("DELETE", "/libraries/"+lib.ID, nil, nil)
}

func TestCompatAPIIntegration(t *testing.T) {
	srv := newAuthTestServer(t)
	adminC := newClient(t, srv)
	setupAdmin(t, adminC)
	root := mediaRoot + "/c"
	for _, f := range []string{"Dune (2021) [tmdbid-438631]/Dune (2021) - 2160p.mkv", "Show (2020)/Season 01/Show S01E01.mkv",
		"Show (2020)/Season 01/Show S01E02.mkv"} {
		if err := os.MkdirAll(filepath.Dir(root+"/"+f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(root+"/"+f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var lib struct {
		ID string `json:"id"`
	}
	adminC.do("POST", "/libraries", map[string]any{"name": "compat-test", "kind": "mixed", "paths": []string{root}}, &lib)
	libID := uuid.MustParse(lib.ID)
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM libraries WHERE id = $1`, libID) })
	scanner := &library.Scanner{Pool: testPool, Roots: library.Roots{mediaRoot}, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Probe: func(_ context.Context, p string) (library.ProbeResult, error) {
			if strings.Contains(p, "2160p") {
				return library.ProbeResult{Container: "mkv", Width: 3840, Height: 2160, DurationMS: 60_000}, nil
			}
			return library.ProbeResult{Container: "mkv", Width: 1920, Height: 1080}, nil
		}}
	if _, err := scanner.Scan(context.Background(), libID); err != nil {
		t.Fatal(err)
	}

	call := func(method, path, token string, body any, out any) int {
		t.Helper()
		var rd io.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			rd = bytes.NewReader(raw)
		}
		req, _ := http.NewRequest(method, srv.URL+"/compat"+path, rd)
		req.Header.Set("Content-Type", "application/json")
		auth := `MediaBrowser Client="Jellyseerr", Device="Server", DeviceId="abc", Version="2.0"`
		if token != "" {
			auth += `, Token="` + token + `"`
		}
		req.Header.Set("Authorization", auth)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		if out != nil {
			_ = json.NewDecoder(resp.Body).Decode(out)
		}
		return resp.StatusCode
	}

	var pub map[string]any
	if s := call("GET", "/System/Info/Public", "", nil, &pub); s != 200 || pub["ProductName"] != "Streamero" || pub["ServerName"] != "Kino" || pub["Version"] == "" {
		t.Fatalf("public info: %d %v", s, pub)
	}
	if s := call("POST", "/Users/AuthenticateByName", "", map[string]any{"Username": "admin", "Pw": "wrong"}, nil); s != 401 {
		t.Fatalf("bad password: %d", s)
	}
	var login struct {
		AccessToken string
		User        struct {
			ID     string `json:"Id"`
			Policy struct{ IsAdministrator bool }
		}
	}
	if s := call("POST", "/Users/AuthenticateByName", "", map[string]any{"Username": "Admin", "Pw": "admin-pass-1"}, &login); s != 200 ||
		login.AccessToken == "" || !login.User.Policy.IsAdministrator || len(login.User.ID) != 32 {
		t.Fatalf("login: %d %+v", s, login)
	}
	tok := login.AccessToken
	if s := call("GET", "/Users/Me", "", nil, nil); s != 401 {
		t.Fatalf("anonymous me: %d", s)
	}
	var users []map[string]any
	if s := call("GET", "/Users", tok, nil, &users); s != 200 || len(users) == 0 {
		t.Fatalf("users: %d %v", s, users)
	}
	var views struct {
		Items []struct {
			ID             string `json:"Id"`
			Name           string
			CollectionType string
		}
	}
	call("GET", "/Library/MediaFolders", tok, nil, &views)
	found := ""
	for _, v := range views.Items {
		if v.Name == "compat-test" {
			found = v.ID
		}
	}
	if found != strings.ReplaceAll(lib.ID, "-", "") {
		t.Fatalf("views: %+v", views)
	}
	type dto struct {
		ID           string `json:"Id"`
		Name         string
		Type         string
		ProviderIDs  map[string]string `json:"ProviderIds"`
		MediaSources []struct {
			MediaStreams []struct {
				Type   string
				Height int
			}
		}
		IndexNumber int
	}
	var items struct {
		Items            []dto
		TotalRecordCount int
	}
	call("GET", "/Users/"+login.User.ID+"/Items?ParentId="+found+"&Recursive=true&IncludeItemTypes=Movie,Series&Fields=ProviderIds,MediaSources", tok, nil, &items)
	if items.TotalRecordCount != 2 {
		t.Fatalf("items: %+v", items)
	}
	var seriesID string
	for _, it := range items.Items {
		switch it.Type {
		case "Movie":
			if it.ProviderIDs["Tmdb"] != "438631" || it.MediaSources[0].MediaStreams[0].Height != 2160 {
				t.Fatalf("movie dto: %+v", it)
			}
		case "Series":
			seriesID = it.ID
		}
	}
	var seasons, episodes struct{ Items []dto }
	call("GET", "/Shows/"+seriesID+"/Seasons", tok, nil, &seasons)
	call("GET", "/Shows/"+seriesID+"/Episodes?SeasonId="+seasons.Items[0].ID, tok, nil, &episodes)
	if len(seasons.Items) != 1 || seasons.Items[0].IndexNumber != 1 || len(episodes.Items) != 2 || episodes.Items[1].IndexNumber != 2 {
		t.Fatalf("seasons/episodes: %+v %+v", seasons, episodes)
	}

	// API keys work as tokens (the *arr connection uses one) and may trigger targeted scans.
	var key struct {
		Key string `json:"key"`
	}
	adminC.do("POST", "/apikeys", map[string]any{"name": "Radarr"}, &key)
	if _, err := testPool.Exec(context.Background(), `UPDATE libraries SET scan_state = 'idle' WHERE id = $1`, libID); err != nil {
		t.Fatal(err)
	}
	if s := call("POST", "/Library/Media/Updated", key.Key, map[string]any{"Updates": []map[string]any{{"Path": root + "/Dune (2021) [tmdbid-438631]", "UpdateType": "Created"}}}, nil); s != 204 {
		t.Fatalf("media updated: %d", s)
	}
	var state string
	_ = testPool.QueryRow(context.Background(), `SELECT scan_state FROM libraries WHERE id = $1`, libID).Scan(&state)
	if state != "queued" {
		t.Fatalf("scan not queued: %s", state)
	}

	// Non-admins can sign in and read, but not trigger scans.
	adminC.do("POST", "/users", map[string]any{"username": "eva", "password": "eva-pass-1"}, nil)
	var evaLogin struct{ AccessToken string }
	call("POST", "/Users/AuthenticateByName", "", map[string]any{"Username": "eva", "Pw": "eva-pass-1"}, &evaLogin)
	if s := call("POST", "/Library/Refresh", evaLogin.AccessToken, nil, nil); s != 403 {
		t.Fatalf("non-admin refresh: %d", s)
	}

	// "Play on" links open the Streamero item page.
	resp, err := http.Get(srv.URL + "/compat/web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !strings.Contains(string(body), `location.replace("/item/"`) || !strings.Contains(resp.Header.Get("Content-Security-Policy"), "sha256-") {
		t.Fatalf("web redirect: %s %s", resp.Header.Get("Content-Security-Policy"), body)
	}
}
