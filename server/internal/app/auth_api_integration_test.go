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
	"image"
	"image/color"
	_ "image/jpeg"
	pngenc "image/png"
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

	"github.com/streamero-app/streamero/server/internal/activity"
	"github.com/streamero-app/streamero/server/internal/api"
	"github.com/streamero-app/streamero/server/internal/auth"
	"github.com/streamero-app/streamero/server/internal/backup"
	"github.com/streamero-app/streamero/server/internal/compat"
	"github.com/streamero-app/streamero/server/internal/config"
	"github.com/streamero-app/streamero/server/internal/importer"
	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/metadata"
	"github.com/streamero-app/streamero/server/internal/notify"
	"github.com/streamero-app/streamero/server/internal/settings"
	"github.com/streamero-app/streamero/server/internal/stream"
)

const testSetupCode = "TEST-CODE"

// mediaRoot is the media folder of the most recent test server.
var mediaRoot string

// testPool is the database pool of the most recent test server.
var testPool *pgxpool.Pool

// testState is the state of the most recent test server.
var testState *State

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
	if _, err := deps.Pool.Exec(ctx, `TRUNCATE users, sessions, audit_log, api_keys, user_totp, recovery_codes, quick_connect, invites, profiles, import_runs;
		DELETE FROM settings WHERE key IN ('setup.completed', 'server.name', 'playback.resume', 'encoding', 'branding', 'integrations.requests', 'enhancements', 'dlna')`); err != nil {
		t.Fatal(err)
	}
	mediaRoot = t.TempDir()
	state.SetLibrary(&library.Service{Pool: deps.Pool, Roots: library.Roots{mediaRoot},
		Enqueue: func(context.Context, uuid.UUID) error { return nil }})
	testPool, testState = deps.Pool, state
	state.SetStream(&stream.Server{Pool: deps.Pool, Roots: library.Roots{mediaRoot}, Signer: stream.Signer{Key: []byte("test-key")},
		HLS: &stream.Manager{Dir: t.TempDir(), FFmpeg: "ffmpeg-not-installed", Log: log}, FFmpeg: "ffmpeg-not-installed"})
	state.SetCompat(&compat.Server{Pool: deps.Pool, Auth: state.Auth(), Libraries: state.Library(), Stream: state.Stream(), ServerID: state.ServerID(),
		Log: log, ServerName: func(context.Context) string { return "Kino" }})
	h := &api.Handler{State: state, ServerName: "Default", StartedAt: time.Now(), Auth: state.Auth, Settings: state.Settings,
		Library: state.Library, Stream: state.Stream, NowPlaying: &activity.Registry{},
		NewTMDb: func(key string) *metadata.TMDb {
			if fakeTMDbURL != "" {
				return &metadata.TMDb{Key: key, BaseURL: fakeTMDbURL + "/3", ImageBase: fakeTMDbURL + "/img"}
			}
			return &metadata.TMDb{Key: key}
		},
		Notify: &notify.Dispatcher{Settings: state.Settings}, Paths: api.Paths{Media: []string{mediaRoot}, Cache: t.TempDir()},
		RunTaskFn: func(_ context.Context, key string) error { ranTasks = append(ranTasks, key); return nil },
		// Imports run synchronously here instead of in a worker.
		EnqueueImport: func(ctx context.Context, id uuid.UUID) error {
			return (&importer.Runner{Pool: deps.Pool, Log: log}).Run(ctx, id)
		}}
	h.Notify.Server = h.ServerNameFor
	testBackups = &backup.Service{Pool: deps.Pool, Dir: t.TempDir(), Version: "test"}
	h.Backup.Store(&api.Backups{Service: testBackups, Enqueue: func(context.Context) error { ranTasks = append(ranTasks, "backup"); return nil }})
	state.Library().Resume = h.LoadResume
	h.Stream().HLS.Encoding = h.LoadEncoding
	state.Compat().OnPlayback, state.Compat().OnProgress = h.TrackPlayback, h.TrackProgress
	testNowPlaying = h.NowPlaying
	router, err := NewRouter(state, h)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return srv
}

// testBackups is the backup service of the most recent test server.
var testBackups *backup.Service

// testNowPlaying is the "now playing" registry of the most recent test server.
var testNowPlaying *activity.Registry

// fakeTMDbURL points the API's TMDb client at a test server when set.
var fakeTMDbURL string

// ranTasks records tasks started through the API (the test server queues nothing).
var ranTasks []string

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

	// The test file is 100 s long: below the default 5 min minimum nothing would be kept.
	var rs map[string]int
	if s := adminC.do("GET", "/settings/resume", nil, &rs); s != 200 || rs["minResumeDurationSeconds"] != 300 || rs["maxResumePercent"] != 90 {
		t.Fatalf("resume defaults: HTTP %d %v", s, rs)
	}
	if s := adminC.do("PUT", "/settings/resume", map[string]int{"minResumePercent": 60, "maxResumePercent": 50, "minResumeDurationSeconds": 0}, nil); s != 400 {
		t.Fatalf("min above max: HTTP %d", s)
	}
	if s := adminC.do("PUT", "/settings/resume", map[string]int{"minResumePercent": 5, "maxResumePercent": 90, "minResumeDurationSeconds": 0}, nil); s != 200 {
		t.Fatalf("save resume: HTTP %d", s)
	}
	// Below the minimum percentage no position is kept.
	adminC.do("PUT", "/items/"+item+"/progress", map[string]any{"positionMs": 2_000}, nil)
	var early []struct{ ID string }
	if adminC.do("GET", "/home/continue", nil, &early); len(early) != 0 {
		t.Fatalf("2%% should not be resumable: %+v", early)
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
	if s := call("GET", "/System/Info/Public", "", nil, &pub); s != 200 || pub["ProductName"] != compat.ProductName || pub["ServerName"] != "Kino" || pub["Version"] == "" {
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
	// Sonarr's connection test.
	var conf map[string]any
	if s := call("GET", "/System/Configuration", tok, nil, &conf); s != 200 || conf["ServerName"] != "Kino" {
		t.Fatalf("configuration: %d %v", s, conf)
	}
	if s := call("GET", "/System/Configuration", "", nil, nil); s != 401 {
		t.Fatalf("anonymous configuration: %d", s)
	}
	if s := call("POST", "/Notifications/Admin", tok, map[string]any{"Name": "Test", "Description": "x"}, nil); s != 204 {
		t.Fatalf("admin notification: %d", s)
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

func TestAdminDashboardIntegration(t *testing.T) {
	srv := newAuthTestServer(t)
	adminC := newClient(t, srv)
	setupAdmin(t, adminC)
	movies := mediaRoot + "/dash"
	if err := os.MkdirAll(movies+"/Dash (2020)", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(movies+"/Dash (2020)/Dash (2020).mp4", []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	var lib struct {
		ID string `json:"id"`
	}
	adminC.do("POST", "/libraries", map[string]any{"name": "D", "kind": "movies", "paths": []string{movies}}, &lib)
	scanner := &library.Scanner{Pool: testPool, Roots: library.Roots{mediaRoot}, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Probe: func(context.Context, string) (library.ProbeResult, error) {
			return library.ProbeResult{Container: "mp4", VideoCodec: "h264", DurationMS: 60_000, Audio: []library.AudioStream{{Codec: "aac"}}}, nil
		}}
	if _, err := scanner.Scan(context.Background(), uuid.MustParse(lib.ID)); err != nil {
		t.Fatal(err)
	}
	var page struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	adminC.do("GET", "/libraries/"+lib.ID+"/items", nil, &page)
	item := page.Items[0].ID
	device := map[string]any{"containers": []string{"mp4"}, "videoCodecs": []string{"h264"}, "audioCodecs": []string{"aac"}}
	if s := adminC.do("POST", "/items/"+item+"/playback", map[string]any{"device": device}, nil); s != 200 {
		t.Fatalf("playback: %d", s)
	}
	adminC.do("PUT", "/items/"+item+"/progress", map[string]any{"positionMs": 12_000}, nil)

	var dash struct {
		Server     struct{ Name, Version string }
		Storage    []struct{ Kind string }
		NowPlaying []struct {
			User, Device, Title, Method string
			PositionMs                  int64
		}
		Activity []struct{ Action string }
	}
	if s := adminC.do("GET", "/dashboard", nil, &dash); s != 200 {
		t.Fatalf("dashboard: %d", s)
	}
	if dash.Server.Name != "Kino" || len(dash.Storage) < 2 || len(dash.NowPlaying) != 1 || dash.NowPlaying[0].Title != "Dash" ||
		dash.NowPlaying[0].Method != "direct" || dash.NowPlaying[0].PositionMs != 12_000 || dash.NowPlaying[0].Device != "test" {
		t.Fatalf("dashboard: %+v", dash)
	}
	if len(dash.Activity) == 0 || dash.Activity[0].Action == "" {
		t.Fatalf("activity: %+v", dash.Activity)
	}

	var tasks []struct {
		Key     string
		Running bool
	}
	if s := adminC.do("GET", "/tasks", nil, &tasks); s != 200 || len(tasks) != 6 {
		t.Fatalf("tasks: %d %+v", s, tasks)
	}
	if s := adminC.do("POST", "/tasks/refresh_metadata/run", nil, nil); s != 202 || ranTasks[len(ranTasks)-1] != "refresh_metadata" {
		t.Fatalf("run task: %d %v", s, ranTasks)
	}
	if s := adminC.do("POST", "/tasks/nope/run", nil, nil); s != 400 {
		t.Fatalf("unknown task: %d", s)
	}

	// Notifications: a generic webhook subscribed to failed sign-ins.
	got := make(chan map[string]any, 4)
	hook := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		body["auth"] = r.Header.Get("Authorization")
		got <- body
	}))
	defer hook.Close()
	target := map[string]any{"id": uuid.NewString(), "name": "Hook", "kind": "generic", "url": hook.URL, "token": "secret",
		"events": []string{"login.failed"}, "enabled": true}
	if s := adminC.do("PUT", "/settings/notifications", map[string]any{"targets": []any{target}}, nil); s != 200 {
		t.Fatalf("save targets: %d", s)
	}
	var saved struct {
		Targets []map[string]any
	}
	adminC.do("GET", "/settings/notifications", nil, &saved)
	if len(saved.Targets) != 1 || saved.Targets[0]["hasToken"] != true || saved.Targets[0]["token"] != nil {
		t.Fatalf("token must be write-only: %+v", saved)
	}
	// Saving again without the token keeps it.
	delete(target, "token")
	adminC.do("PUT", "/settings/notifications", map[string]any{"targets": []any{target}}, nil)
	if s := adminC.do("POST", "/settings/notifications/test", target, nil); s != 204 {
		t.Fatalf("test notification: %d", s)
	}
	if b := <-got; b["event"] != "test" || b["auth"] != "Bearer secret" || b["server"] != "Kino" {
		t.Fatalf("test payload: %+v", b)
	}
	bad := newClient(t, srv)
	bad.do("POST", "/auth/login", map[string]any{"username": "Admin", "password": "wrong-password", "device": map[string]any{"name": "x"}}, nil)
	select {
	case b := <-got:
		if b["event"] != "login.failed" {
			t.Fatalf("event: %+v", b)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no login.failed notification")
	}
	if s := adminC.do("PUT", "/settings/notifications", map[string]any{"targets": []any{map[string]any{
		"id": uuid.NewString(), "name": "x", "kind": "ntfy", "url": "ftp://nope", "events": []string{}, "enabled": true}}}, nil); s != 400 {
		t.Fatalf("invalid target: %d", s)
	}

	// Not for regular users.
	userC := newClient(t, srv)
	adminC.do("POST", "/users", map[string]any{"username": "dashuser", "password": "user-pass-1"}, nil)
	var res authResult
	userC.do("POST", "/auth/login", map[string]any{"username": "dashuser", "password": "user-pass-1", "device": map[string]any{"name": "u"}}, &res)
	userC.token = res.Token
	if s := userC.do("GET", "/dashboard", nil, nil); s != 403 {
		t.Fatalf("user dashboard: %d", s)
	}
}

func TestNextUpAndPreferencesIntegration(t *testing.T) {
	srv := newAuthTestServer(t)
	adminC := newClient(t, srv)
	setupAdmin(t, adminC)
	shows := mediaRoot + "/nextup"
	for _, f := range []string{"Show (2020)/Season 01/Show S01E01.mkv", "Show (2020)/Season 01/Show S01E02.mkv",
		"Show (2020)/Season 01/Show S01E03.mkv", "Show (2020)/Season 02/Show S02E01.mkv", "Show (2020)/Season 01/Show S01E03.hr.srt"} {
		if err := os.MkdirAll(filepath.Dir(shows+"/"+f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(shows+"/"+f, []byte("1\n00:00:01,000 --> 00:00:02,000\nBok\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var lib struct {
		ID string `json:"id"`
	}
	adminC.do("POST", "/libraries", map[string]any{"name": "N", "kind": "shows", "paths": []string{shows}}, &lib)
	scanner := &library.Scanner{Pool: testPool, Roots: library.Roots{mediaRoot}, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Probe: func(context.Context, string) (library.ProbeResult, error) {
			return library.ProbeResult{Container: "mkv", VideoCodec: "h264", DurationMS: 60_000,
				Audio: []library.AudioStream{{Codec: "aac", Language: "eng"}, {Codec: "aac", Language: "hrv"}}}, nil
		}}
	if _, err := scanner.Scan(context.Background(), uuid.MustParse(lib.ID)); err != nil {
		t.Fatal(err)
	}
	type summary struct {
		ID            string `json:"id"`
		Title         string `json:"title"`
		SeasonNumber  int    `json:"seasonNumber"`
		EpisodeNumber int    `json:"episodeNumber"`
		SeriesTitle   string `json:"seriesTitle"`
	}
	var eps []summary
	rows, err := testPool.Query(context.Background(), `SELECT id, season_number, episode_number FROM items
		WHERE library_id = $1 AND kind = 'episode' ORDER BY season_number, episode_number`, uuid.MustParse(lib.ID))
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var e summary
		var id uuid.UUID
		if err := rows.Scan(&id, &e.SeasonNumber, &e.EpisodeNumber); err != nil {
			t.Fatal(err)
		}
		e.ID = id.String()
		eps = append(eps, e)
	}
	rows.Close()
	if len(eps) != 4 {
		t.Fatalf("episodes: %+v", eps)
	}
	var next []summary
	adminC.do("GET", "/home/next-up", nil, &next)
	if len(next) != 0 {
		t.Fatalf("nothing watched yet: %+v", next)
	}
	adminC.do("PUT", "/items/"+eps[0].ID+"/user-data", map[string]any{"played": true}, nil)
	adminC.do("PUT", "/items/"+eps[1].ID+"/user-data", map[string]any{"played": true}, nil)
	adminC.do("GET", "/home/next-up", nil, &next)
	if len(next) != 1 || next[0].ID != eps[2].ID || next[0].SeriesTitle != "Show" {
		t.Fatalf("next up: %+v", next)
	}
	var after summary
	if s := adminC.do("GET", "/items/"+eps[2].ID+"/next", nil, &after); s != 200 || after.ID != eps[3].ID {
		t.Fatalf("next episode: %d %+v", s, after)
	}
	if s := adminC.do("GET", "/items/"+eps[3].ID+"/next", nil, nil); s != 404 {
		t.Fatalf("after the last episode: %d", s)
	}

	var prefs map[string]any
	adminC.do("GET", "/me/preferences", nil, &prefs)
	if prefs["subtitleMode"] != "default" || prefs["autoplayNext"] != true {
		t.Fatalf("default prefs: %+v", prefs)
	}
	if s := adminC.do("PUT", "/me/preferences", map[string]any{"audioLanguage": "en", "subtitleLanguage": "hr", "subtitleMode": "always",
		"autoplayNext": false}, &prefs); s != 200 || prefs["autoplayNext"] != false {
		t.Fatalf("save prefs: %d %+v", s, prefs)
	}
	if s := adminC.do("PUT", "/me/preferences", map[string]any{"subtitleMode": "loud", "autoplayNext": true}, nil); s != 400 {
		t.Fatalf("bad mode: %d", s)
	}
	var info struct {
		AudioIndex      int  `json:"audioIndex"`
		SubtitleDefault *int `json:"subtitleDefault"`
		Subtitles       []struct{ Language string }
	}
	device := map[string]any{"containers": []string{"mkv"}, "videoCodecs": []string{"h264"}, "audioCodecs": []string{"aac"}, "hls": true}
	// (A Croatian audio preference would pick track 1 and need a remux; the test server has no ffmpeg.)
	if s := adminC.do("POST", "/items/"+eps[2].ID+"/playback", map[string]any{"device": device}, &info); s != 200 {
		t.Fatalf("playback: %d", s)
	}
	if info.AudioIndex != 0 || info.SubtitleDefault == nil || info.Subtitles[*info.SubtitleDefault].Language != "hr" {
		t.Fatalf("preferences not applied: %+v", info)
	}
}

func TestCollectionsAndPlaylistsIntegration(t *testing.T) {
	srv := newAuthTestServer(t)
	adminC := newClient(t, srv)
	setupAdmin(t, adminC)
	dir := mediaRoot + "/cp"
	for _, f := range []string{"A (2001)/A (2001).mkv", "B (2002)/B (2002).mkv", "C (2003)/C (2003).mkv"} {
		if err := os.MkdirAll(filepath.Dir(dir+"/"+f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dir+"/"+f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var lib struct {
		ID string `json:"id"`
	}
	adminC.do("POST", "/libraries", map[string]any{"name": "CP", "kind": "movies", "paths": []string{dir}}, &lib)
	scanner := &library.Scanner{Pool: testPool, Roots: library.Roots{mediaRoot}, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Probe: func(context.Context, string) (library.ProbeResult, error) {
			return library.ProbeResult{Container: "mkv", DurationMS: 60_000}, nil
		}}
	if _, err := scanner.Scan(context.Background(), uuid.MustParse(lib.ID)); err != nil {
		t.Fatal(err)
	}
	var page struct {
		Items []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"items"`
	}
	adminC.do("GET", "/libraries/"+lib.ID+"/items?sort=title", nil, &page)
	a, b, c := page.Items[0].ID, page.Items[1].ID, page.Items[2].ID

	// Collections: admin only; ordered by year; shown on the item.
	var col struct {
		ID        string
		ItemCount int
		Auto      bool
	}
	if s := adminC.do("POST", "/collections", map[string]any{"name": "Trilogija", "itemIds": []string{c, a}}, &col); s != 201 || col.ItemCount != 2 || col.Auto {
		t.Fatalf("create collection: %d %+v", s, col)
	}
	var detail struct {
		Items []struct{ Title string }
	}
	adminC.do("GET", "/collections/"+col.ID, nil, &detail)
	if len(detail.Items) != 2 || detail.Items[0].Title != "A" || detail.Items[1].Title != "C" {
		t.Fatalf("collection order: %+v", detail)
	}
	var item struct {
		Collections []struct{ Name string }
	}
	adminC.do("GET", "/items/"+a, nil, &item)
	if len(item.Collections) != 1 || item.Collections[0].Name != "Trilogija" {
		t.Fatalf("item collections: %+v", item)
	}
	if s := adminC.do("POST", "/collections", map[string]any{"name": " ", "itemIds": []string{}}, nil); s != 400 {
		t.Fatalf("empty name: %d", s)
	}

	userC := newClient(t, srv)
	adminC.do("POST", "/users", map[string]any{"username": "cpuser", "password": "user-pass-1"}, nil)
	var res authResult
	userC.do("POST", "/auth/login", map[string]any{"username": "cpuser", "password": "user-pass-1", "device": map[string]any{"name": "u"}}, &res)
	userC.token = res.Token
	if s := userC.do("POST", "/collections", map[string]any{"name": "Mine", "itemIds": []string{}}, nil); s != 403 {
		t.Fatalf("user creates collection: %d", s)
	}
	var cols []struct{ Name string }
	userC.do("GET", "/collections", nil, &cols)
	if len(cols) == 0 {
		t.Fatal("users see collections")
	}

	// Playlists: owned per viewer, private by default, duplicates allowed, reorder by replacing the list.
	var pl struct {
		ID        string
		ItemCount int
		Mine      bool
		Public    bool
	}
	if s := userC.do("POST", "/playlists", map[string]any{"name": "Vikend"}, &pl); s != 201 || !pl.Mine || pl.ItemCount != 0 {
		t.Fatalf("create playlist: %d %+v", s, pl)
	}
	userC.do("POST", "/playlists/"+pl.ID+"/items", map[string]any{"itemIds": []string{b, a, b}}, &pl)
	if pl.ItemCount != 3 {
		t.Fatalf("append: %+v", pl)
	}
	if s := adminC.do("GET", "/playlists/"+pl.ID, nil, nil); s != 404 {
		t.Fatalf("private playlist visible to others: %d", s)
	}
	userC.do("PUT", "/playlists/"+pl.ID, map[string]any{"name": "Vikend", "public": true, "itemIds": []string{c, b}}, &pl)
	var pd struct {
		Playlist struct{ Mine bool }
		Items    []struct{ Title string }
	}
	if s := adminC.do("GET", "/playlists/"+pl.ID, nil, &pd); s != 200 || pd.Playlist.Mine || len(pd.Items) != 2 || pd.Items[0].Title != "C" {
		t.Fatalf("public playlist: %d %+v", s, pd)
	}
	if s := adminC.do("POST", "/playlists/"+pl.ID+"/items", map[string]any{"itemIds": []string{a}}, nil); s != 403 {
		t.Fatalf("others edit a public playlist: %d", s)
	}
	if s := adminC.do("DELETE", "/playlists/"+pl.ID, nil, nil); s != 403 {
		t.Fatalf("others delete: %d", s)
	}
	if s := userC.do("DELETE", "/playlists/"+pl.ID, nil, nil); s != 204 {
		t.Fatalf("owner delete: %d", s)
	}
	if s := adminC.do("DELETE", "/collections/"+col.ID, nil, nil); s != 204 {
		t.Fatalf("delete collection: %d", s)
	}
}

func TestIdentifyAndEditMetadataIntegration(t *testing.T) {
	tm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/3/configuration":
			_, _ = w.Write([]byte(`{}`))
		case "/3/search/movie":
			_, _ = w.Write([]byte(`{"results":[{"id":77,"title":"Pravi film","original_title":"Real Movie","release_date":"1999-03-31","overview":"Tocno.","poster_path":"/p.jpg"},{"id":78,"title":"Krivi film","release_date":"2005-01-01"}]}`))
		case "/3/movie/77":
			_, _ = w.Write([]byte(`{"id":77,"title":"Pravi film","original_title":"Real Movie","overview":"Tocno.","release_date":"1999-03-31","vote_average":8.1,"genres":[{"name":"Drama"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer tm.Close()
	fakeTMDbURL = tm.URL
	t.Cleanup(func() { fakeTMDbURL = "" })
	srv := newAuthTestServer(t)
	adminC := newClient(t, srv)
	setupAdmin(t, adminC)
	dir := mediaRoot + "/ident"
	if err := os.MkdirAll(dir+"/Wrong Name (1999)", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/Wrong Name (1999)/Wrong Name (1999).mkv", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	var lib struct {
		ID string `json:"id"`
	}
	adminC.do("POST", "/libraries", map[string]any{"name": "I", "kind": "movies", "paths": []string{dir}}, &lib)
	scanner := &library.Scanner{Pool: testPool, Roots: library.Roots{mediaRoot}, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Probe: func(context.Context, string) (library.ProbeResult, error) {
			return library.ProbeResult{Container: "mkv"}, nil
		}}
	if _, err := scanner.Scan(context.Background(), uuid.MustParse(lib.ID)); err != nil {
		t.Fatal(err)
	}
	var page struct {
		Items []struct{ ID string } `json:"items"`
	}
	adminC.do("GET", "/libraries/"+lib.ID+"/items", nil, &page)
	item := page.Items[0].ID

	if s := adminC.do("GET", "/items/"+item+"/identify", nil, nil); s != 409 {
		t.Fatalf("identify without key: %d", s)
	}
	old, _ := metadata.LoadSettings(context.Background(), settings.New(testPool))
	t.Cleanup(func() { _ = metadata.SaveSettings(context.Background(), settings.New(testPool), old) })
	// A lookup that failed earlier is retried once a key is saved.
	if _, err := testPool.Exec(context.Background(), `UPDATE items SET metadata_refreshed_at = now(), metadata_error = 'no match' WHERE id = $1`, item); err != nil {
		t.Fatal(err)
	}
	if s := adminC.do("PUT", "/settings/metadata", map[string]any{"tmdbApiKey": "k", "language": "hr-HR", "country": "HR"}, nil); s != 200 {
		t.Fatalf("save key: %d", s)
	}
	var retried bool
	if err := testPool.QueryRow(context.Background(), `SELECT metadata_refreshed_at IS NULL FROM items WHERE id = $1`, item).Scan(&retried); err != nil || !retried {
		t.Fatalf("failed lookup not reset: %v %v", retried, err)
	}
	var cands []struct {
		TmdbID    int `json:"tmdbId"`
		Title     string
		Year      int
		PosterURL string `json:"posterUrl"`
	}
	if s := adminC.do("GET", "/items/"+item+"/identify?query=pravi", nil, &cands); s != 200 || len(cands) != 2 || cands[0].Year != 1999 ||
		cands[0].PosterURL != "https://image.tmdb.org/t/p/w185/p.jpg" {
		t.Fatalf("candidates: %d %+v", s, cands)
	}
	if s := adminC.do("POST", "/items/"+item+"/identify", map[string]any{"tmdbId": 77}, nil); s != 204 {
		t.Fatalf("identify: %d", s)
	}
	var d struct {
		Item        struct{ Title string }
		Overview    string
		ProviderIDs map[string]string `json:"providerIds"`
		Locked      bool
	}
	adminC.do("GET", "/items/"+item, nil, &d)
	if d.Item.Title != "Pravi film" || d.Overview != "Tocno." || d.ProviderIDs["tmdb"] != "77" || d.Locked {
		t.Fatalf("after identify: %+v", d)
	}

	// Hand edit with lock: a refresh is refused instead of overwriting it.
	if s := adminC.do("PUT", "/items/"+item+"/metadata", map[string]any{"title": "Moj naslov", "year": 2000, "genres": []string{"Drama", " "},
		"overview": "Vlastiti opis.", "rating": 9.5, "locked": true}, nil); s != 204 {
		t.Fatalf("edit: %d", s)
	}
	adminC.do("GET", "/items/"+item, nil, &d)
	if d.Item.Title != "Moj naslov" || d.Overview != "Vlastiti opis." || !d.Locked {
		t.Fatalf("after edit: %+v", d)
	}
	if s := adminC.do("POST", "/items/"+item+"/refresh", nil, nil); s != 409 {
		t.Fatalf("refresh of locked item: %d", s)
	}
	if s := adminC.do("PUT", "/items/"+item+"/metadata", map[string]any{"title": "  ", "genres": []string{}, "locked": false}, nil); s != 400 {
		t.Fatalf("empty title: %d", s)
	}
}

func TestServerSettingsIntegration(t *testing.T) {
	srv := newAuthTestServer(t)
	adminC := newClient(t, srv)
	setupAdmin(t, adminC)
	var enc map[string]any
	if s := adminC.do("GET", "/settings/encoding", nil, &enc); s != 200 || enc["preset"] != "veryfast" || enc["toneMapping"] != true {
		t.Fatalf("encoding defaults: HTTP %d %v", s, enc)
	}
	bad := map[string]any{"hardwareAcceleration": "vaapi", "vaapiDevice": "/etc/passwd", "preset": "fast", "quality": 20,
		"audioBitrateKbps": 256, "segmentSeconds": 4, "maxSessions": 2, "toneMapping": false}
	if s := adminC.do("PUT", "/settings/encoding", bad, nil); s != 400 {
		t.Fatalf("device outside /dev/dri: HTTP %d", s)
	}
	bad["vaapiDevice"] = "/dev/dri/renderD128"
	if s := adminC.do("PUT", "/settings/encoding", bad, &enc); s != 200 || enc["toneMapping"] != false || enc["maxSessions"] != float64(2) {
		t.Fatalf("save encoding: HTTP %d %v", s, enc)
	}
	if s := adminC.do("PUT", "/settings/branding", map[string]string{"loginDisclaimer": "Samo za obitelj.", "customCss": "</style><script>"}, nil); s != 400 {
		t.Fatalf("css closing the style element: HTTP %d", s)
	}
	if s := adminC.do("PUT", "/settings/branding", map[string]string{"loginDisclaimer": "Samo za obitelj.", "customCss": "body{letter-spacing:0}"}, nil); s != 200 {
		t.Fatalf("save branding: HTTP %d", s)
	}
	var info map[string]any
	newClient(t, srv).do("GET", "/system/info", nil, &info)
	if info["loginDisclaimer"] != "Samo za obitelj." || info["customCss"] != "body{letter-spacing:0}" {
		t.Fatalf("branding in system info: %v", info)
	}
	var act struct {
		Entries []struct{ Action string } `json:"entries"`
		Total   int                       `json:"total"`
	}
	if s := adminC.do("GET", "/admin/activity?limit=5", nil, &act); s != 200 || act.Total < 2 || act.Entries[0].Action != "settings.branding" {
		t.Fatalf("activity: HTTP %d %+v", s, act)
	}
	var logs []map[string]any
	if s := adminC.do("GET", "/admin/logs?level=debug", nil, &logs); s != 200 {
		t.Fatalf("logs: HTTP %d", s)
	}
}

func TestAccountAndAvatarIntegration(t *testing.T) {
	srv := newAuthTestServer(t)
	c := newClient(t, srv)
	setupAdmin(t, c)
	var me map[string]any
	if s := c.do("PUT", "/auth/me", map[string]any{"displayName": "  Ana Kino ", "email": "ana@example.com", "language": "hr"}, &me); s != 200 {
		t.Fatalf("update me: HTTP %d", s)
	}
	if me["displayName"] != "Ana Kino" || me["email"] != "ana@example.com" || me["language"] != "hr" || me["hasAvatar"] != false {
		t.Fatalf("me: %v", me)
	}
	var e apiError
	if s := c.do("PUT", "/auth/me", map[string]any{"email": "not an address"}, &e); s != 400 || e.Code != "invalid_email" {
		t.Fatalf("bad e-mail: HTTP %d %+v", s, e)
	}
	// Only the fields sent change.
	me = map[string]any{}
	c.do("PUT", "/auth/me", map[string]any{"displayName": ""}, &me)
	if _, has := me["displayName"]; has || me["email"] != "ana@example.com" {
		t.Fatalf("partial update: %v", me)
	}

	upload := func(path string, body []byte) int {
		req, _ := http.NewRequest("PUT", c.base+path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Header.Set("Authorization", "Bearer "+c.token)
		resp, err := c.http.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	if s := upload("/auth/me/avatar", []byte("not a picture")); s != 400 {
		t.Fatalf("garbage upload: HTTP %d", s)
	}
	img := image.NewRGBA(image.Rect(0, 0, 900, 600))
	for y := 0; y < 600; y++ {
		for x := 0; x < 900; x++ {
			img.Set(x, y, color.RGBA{200, uint8(x % 256), 40, 255})
		}
	}
	var png bytes.Buffer
	_ = pngenc.Encode(&png, img)
	if s := upload("/auth/me/avatar", png.Bytes()); s != 204 {
		t.Fatalf("upload: HTTP %d", s)
	}
	c.do("GET", "/auth/me", nil, &me)
	if me["hasAvatar"] != true {
		t.Fatalf("hasAvatar after upload: %v", me)
	}
	req, _ := http.NewRequest("GET", c.base+"/avatars/"+me["id"].(string), nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	pic, _, err := image.Decode(resp.Body)
	_ = resp.Body.Close()
	if err != nil || pic.Bounds().Dx() != 256 || pic.Bounds().Dy() != 256 {
		t.Fatalf("stored picture should be a 256 px square: %v %v", err, pic)
	}
	if s := c.do("DELETE", "/auth/me/avatar", nil, nil); s != 204 {
		t.Fatalf("delete: HTTP %d", s)
	}
	if s := c.do("GET", "/avatars/"+me["id"].(string), nil, nil); s != 404 {
		t.Fatalf("after delete: HTTP %d", s)
	}
	// Someone else's profile cannot be given a picture.
	if s := upload("/profiles/"+uuid.NewString()+"/avatar", png.Bytes()); s != 404 {
		t.Fatalf("foreign profile: HTTP %d", s)
	}
}

func TestDiscoverIntegration(t *testing.T) {
	seerr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "seerr-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v1/search":
			_, _ = w.Write([]byte(`{"page":1,"totalPages":1,"results":[{"id":603,"mediaType":"movie","title":"The Matrix","releaseDate":"1999-03-30","posterPath":"/m.jpg"}]}`))
		case "/api/v1/request":
			_, _ = w.Write([]byte(`{"status":1}`))
		case "/api/v1/status":
			_, _ = w.Write([]byte(`{"version":"2.7.3"}`))
		}
	}))
	defer seerr.Close()
	srv := newAuthTestServer(t)
	c := newClient(t, srv)
	setupAdmin(t, c)
	var page map[string]any
	if s := c.do("GET", "/discover?q=matrix", nil, &page); s != 200 || page["configured"] != false {
		t.Fatalf("unconfigured discover: HTTP %d %v", s, page)
	}
	var e apiError
	if s := c.do("PUT", "/settings/requests", map[string]any{"seerrUrl": "seerr:5055", "usersCanRequest": true}, &e); s != 400 || e.Code != "invalid_url" {
		t.Fatalf("bad url: HTTP %d %+v", s, e)
	}
	var rs map[string]any
	if s := c.do("PUT", "/settings/requests", map[string]any{"seerrUrl": seerr.URL + "/", "seerrApiKey": "seerr-key", "usersCanRequest": false}, &rs); s != 200 ||
		rs["seerrConfigured"] != true || rs["seerrUrl"] != seerr.URL {
		t.Fatalf("save: HTTP %d %v", s, rs)
	}
	if _, leaked := rs["seerrApiKey"]; leaked {
		t.Fatal("the key must never be returned")
	}
	var checks []map[string]any
	if c.do("POST", "/settings/requests/test", nil, &checks); len(checks) != 1 || checks[0]["ok"] != true || checks[0]["version"] != "2.7.3" {
		t.Fatalf("test connection: %v", checks)
	}
	// A title already in the library comes back as available with its item id.
	if _, err := testPool.Exec(context.Background(), `INSERT INTO libraries (id, name, kind, paths) VALUES ('01a1c0a0-0000-7000-8000-000000000001', 'D', 'movies', '{/x}') ON CONFLICT DO NOTHING;
		INSERT INTO items (id, library_id, kind, path, title, sort_title, provider_ids) VALUES ('01a1c0a0-0000-7000-8000-000000000002', '01a1c0a0-0000-7000-8000-000000000001', 'movie', '/x/m', 'The Matrix', 'Matrix', '{"tmdb":"603"}')
		ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM libraries WHERE id = '01a1c0a0-0000-7000-8000-000000000001'`)
	})
	page = map[string]any{}
	if s := c.do("GET", "/discover?q=matrix", nil, &page); s != 200 || page["canRequest"] != true {
		t.Fatalf("discover: HTTP %d %v", s, page)
	}
	titles := page["titles"].([]any)
	first := titles[0].(map[string]any)
	if first["status"] != "available" || first["itemId"] != "01a1c0a0-0000-7000-8000-000000000002" || first["posterUrl"] != "https://image.tmdb.org/t/p/w342/m.jpg" {
		t.Fatalf("title: %v", first)
	}
	var res map[string]any
	if s := c.do("POST", "/discover/request", map[string]any{"tmdbId": 603, "mediaType": "movie"}, &res); s != 200 || res["status"] != "pending" {
		t.Fatalf("request: HTTP %d %v", s, res)
	}
	// With usersCanRequest off, a normal user may search but not request.
	c.do("POST", "/users", map[string]any{"username": "viewer", "password": "viewer-pass-1"}, nil)
	u := newClient(t, srv)
	var login authResult
	u.do("POST", "/auth/login", map[string]any{"username": "viewer", "password": "viewer-pass-1", "device": map[string]any{"name": "t"}}, &login)
	u.token = login.Token
	page = map[string]any{}
	if u.do("GET", "/discover?q=matrix", nil, &page); page["canRequest"] != false {
		t.Fatalf("viewer canRequest: %v", page)
	}
	if s := u.do("POST", "/discover/request", map[string]any{"tmdbId": 603, "mediaType": "movie"}, &e); s != 403 {
		t.Fatalf("viewer request: HTTP %d", s)
	}
}

func TestEnhancementsIntegration(t *testing.T) {
	srv := newAuthTestServer(t)
	c := newClient(t, srv)
	setupAdmin(t, c)
	var e map[string]any
	if s := c.do("GET", "/enhancements", nil, &e); s != 200 || e["qualityTags"] != true || e["autoPip"] != false {
		t.Fatalf("defaults: HTTP %d %v", s, e)
	}
	off := map[string]bool{"qualityTags": false, "randomButton": true, "pauseScreen": true, "arrLinks": false, "discoverInSearch": true,
		"removeFromContinue": true, "shortcutsHelp": true, "autoPip": true}
	if s := c.do("PUT", "/enhancements", off, &e); s != 200 || e["qualityTags"] != false || e["autoPip"] != true {
		t.Fatalf("save: HTTP %d %v", s, e)
	}
	_, _ = testPool.Exec(context.Background(), `DELETE FROM settings WHERE key = 'enhancements'`)
}

func TestBazarrSubtitlesIntegration(t *testing.T) {
	var downloaded string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/radarr/api/v3/movie":
			_, _ = w.Write([]byte(`[{"id":7,"tmdbId":603}]`))
		case r.URL.Path == "/bazarr/api/system/status":
			_, _ = w.Write([]byte(`{"data":{"bazarr_version":"1.5.1"}}`))
		case r.URL.Path == "/bazarr/api/providers/movies" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"data":[{"provider":"a","language":"en","forced":"False","hearing_impaired":"False","score":50,"subtitle":"low","original_format":"False"},
				{"provider":"b","language":"hr","forced":"False","hearing_impaired":"False","score":95,"subtitle":"best","original_format":"False"}]}`))
		case r.URL.Path == "/bazarr/api/providers/movies" && r.Method == http.MethodPost:
			downloaded = r.URL.Query().Get("subtitle")
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer fake.Close()
	srv := newAuthTestServer(t)
	c := newClient(t, srv)
	setupAdmin(t, c)
	if _, err := testPool.Exec(context.Background(), `INSERT INTO libraries (id, name, kind, paths) VALUES ('01a1c0b0-0000-7000-8000-000000000001', 'B', 'movies', '{/x}') ON CONFLICT DO NOTHING;
		INSERT INTO items (id, library_id, kind, path, title, sort_title, provider_ids) VALUES ('01a1c0b0-0000-7000-8000-000000000002', '01a1c0b0-0000-7000-8000-000000000001', 'movie', '/x/m', 'The Matrix', 'Matrix', '{"tmdb":"603"}')
		ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM libraries WHERE id = '01a1c0b0-0000-7000-8000-000000000001'`)
	})
	item := "/items/01a1c0b0-0000-7000-8000-000000000002/subtitles"
	var search map[string]any
	if s := c.do("GET", item+"/search", nil, &search); s != 200 || search["configured"] != false {
		t.Fatalf("unconfigured: HTTP %d %v", s, search)
	}
	var rs map[string]any
	if s := c.do("PUT", "/settings/requests", map[string]any{"usersCanRequest": true, "radarrUrl": fake.URL + "/radarr", "radarrApiKey": "r",
		"bazarrUrl": fake.URL + "/bazarr", "bazarrApiKey": "b"}, &rs); s != 200 || rs["bazarrConfigured"] != true {
		t.Fatalf("save: HTTP %d %v", s, rs)
	}
	var checks []map[string]any
	c.do("POST", "/settings/requests/test", nil, &checks)
	if len(checks) != 2 || checks[1]["app"] != "Bazarr" || checks[1]["version"] != "1.5.1" {
		t.Fatalf("checks: %v", checks)
	}
	search = map[string]any{}
	if s := c.do("GET", item+"/search", nil, &search); s != 200 {
		t.Fatalf("search: HTTP %d", s)
	}
	offers := search["offers"].([]any)
	best := offers[0].(map[string]any)
	if len(offers) != 2 || best["language"] != "hr" {
		t.Fatalf("offers should be best first: %v", offers)
	}
	if s := c.do("POST", item+"/download", best, nil); s != 204 || downloaded != "best" {
		t.Fatalf("download: HTTP %d %q", s, downloaded)
	}
	// A user without the subtitle permission may not.
	c.do("POST", "/users", map[string]any{"username": "subviewer", "password": "viewer-pass-1"}, nil)
	u := newClient(t, srv)
	var login authResult
	u.do("POST", "/auth/login", map[string]any{"username": "subviewer", "password": "viewer-pass-1", "device": map[string]any{"name": "t"}}, &login)
	u.token = login.Token
	if s := u.do("GET", item+"/search", nil, nil); s != 403 {
		t.Fatalf("viewer: HTTP %d", s)
	}
}
