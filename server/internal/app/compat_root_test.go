package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/api"
	"github.com/streamero-app/streamero/server/internal/compat"
)

// spaRoutes reads every route the web client's router defines (web/src/app/router.tsx),
// with path parameters filled in, so a route added there is checked here too.
func spaRoutes(t *testing.T) []string {
	t.Helper()
	src, err := os.ReadFile("../../../web/src/app/router.tsx")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`createRoute\(\{\s*getParentRoute: \(\) => (\w+),\s*path: "([^"]*)"`)
	param := regexp.MustCompile(`\$\w+`)
	sample := uuid.New().String()
	var out []string
	for _, m := range re.FindAllStringSubmatch(string(src), -1) {
		parent, p := m[1], m[2]
		switch parent {
		case "rootRoute", "appRoute":
		case "settingsRoute":
			p = "/settings/" + strings.TrimPrefix(p, "/")
		default:
			t.Fatalf("route %q has parent %s: teach spaRoutes its prefix", p, parent)
		}
		for _, v := range []string{sample, strings.ReplaceAll(sample, "-", ""), "x"} {
			out = append(out, param.ReplaceAllString(p, v))
		}
	}
	if len(out) < 3*35 {
		t.Fatalf("found only %d web routes; did router.tsx change shape?", len(out)/3)
	}
	return out
}

// compatRootPaths are Jellyfin-style paths media clients call on the plain server
// address, in the casing clients use.
var compatRootPaths = []struct{ method, path string }{
	{"GET", "/System/Info/Public"}, {"GET", "/system/info/public"}, {"GET", "/System/Info"}, {"GET", "/System/Ping"},
	{"POST", "/System/Ping"}, {"GET", "/Users/Public"}, {"POST", "/Users/AuthenticateByName"},
	{"GET", "/Users/0123456789abcdef0123456789abcdef"}, {"GET", "/Users/Me"},
	{"GET", "/Users/0123456789abcdef0123456789abcdef/Views"}, {"GET", "/UserViews"},
	{"GET", "/Items"}, {"GET", "/Items/0123456789abcdef0123456789abcdef"}, {"GET", "/Items/Latest"},
	{"GET", "/Items/0123456789abcdef0123456789abcdef/Images/Primary"},
	{"POST", "/Items/0123456789abcdef0123456789abcdef/PlaybackInfo"},
	{"GET", "/Videos/0123456789abcdef0123456789abcdef/stream"}, {"GET", "/Videos/0123456789abcdef0123456789abcdef/stream.mkv"},
	{"GET", "/Audio/0123456789abcdef0123456789abcdef/stream"}, {"GET", "/Shows/NextUp"},
	{"GET", "/Shows/0123456789abcdef0123456789abcdef/Seasons"}, {"GET", "/QuickConnect/Enabled"},
	{"POST", "/QuickConnect/Initiate"}, {"GET", "/QuickConnect/Connect"}, {"POST", "/Sessions/Playing"},
	{"POST", "/Sessions/Playing/Progress"}, {"POST", "/Sessions/Capabilities/Full"}, {"GET", "/Library/MediaFolders"},
	{"POST", "/Library/Refresh"}, {"GET", "/Genres"}, {"GET", "/Branding/Configuration"}, {"GET", "/Branding/Css"},
	{"GET", "/DisplayPreferences/usersettings"}, {"GET", "/socket"}, {"GET", "/Search/Hints"},
	{"GET", "/web/index.html"}, {"GET", "/MediaSegments/0123456789abcdef0123456789abcdef"},
	{"POST", "/UserPlayedItems/0123456789abcdef0123456789abcdef"},
}

func readyState(c *compat.Server) *State {
	st := &State{}
	id := uuid.New()
	st.serverID.Store(&id)
	st.ready.Store(true)
	if c != nil {
		st.SetCompat(c)
	}
	return st
}

func isSPA(rec *httptest.ResponseRecorder) bool {
	return rec.Code == http.StatusOK && rec.Header().Get("Content-Type") == "text/html; charset=utf-8" &&
		!strings.Contains(rec.Body.String(), "location.replace")
}

// TestCompatAtRootDoesNotTakeWebRoutes hits every web client route, its static
// files and Streamero's own endpoints: none may reach the compat API.
func TestCompatAtRootDoesNotTakeWebRoutes(t *testing.T) {
	h := newTestRouter(t, readyState(nil)) // compat not installed: a compat hit would answer 503
	paths := append(spaRoutes(t), "/", "/assets/index-abc123.js", "/assets/index.css", "/favicon.svg",
		"/favicon.ico", "/manifest.webmanifest", "/sw.js", "/robots.txt", "/icons/icon-192.png")
	for _, p := range paths {
		for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodOptions} {
			if compat.Matches(m, p) {
				t.Errorf("%s %s matches a compat route", m, p)
			}
		}
		if rec := do(h, p); !isSPA(rec) {
			t.Errorf("GET %s: %d %q, want the web client", p, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
	// Streamero's own routes keep their handlers.
	for _, p := range []string{"/healthz", "/readyz", "/api/v1/system/info", "/api/v1/system/ping"} {
		if compat.Matches(http.MethodGet, p) {
			t.Errorf("%s matches a compat route", p)
		}
		rec := do(h, p)
		if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
			t.Errorf("GET %s: %d %q", p, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
	for _, p := range []string{"/stream/hls/x/master.m3u8", "/Stream/x", "/api/v1/Items", "/compatx/System/Info/Public"} {
		if compat.Matches(http.MethodGet, p) {
			t.Errorf("%s matches a compat route", p)
		}
	}
}

// TestCompatAtRootServesClientPaths: Jellyfin-style paths reach the compat API
// at the root (case-insensitively) and under /compat, and only there when turned off.
func TestCompatAtRootServesClientPaths(t *testing.T) {
	h := newTestRouter(t, readyState(nil))
	for _, c := range compatRootPaths {
		if !compat.Matches(c.method, c.path) {
			t.Errorf("%s %s does not match a compat route", c.method, c.path)
			continue
		}
		for _, p := range []string{c.path, "/compat" + c.path} {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(c.method, p, nil))
			// Compat is not installed in this state, so reaching it means 503 "starting".
			if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "server is starting") {
				t.Errorf("%s %s: %d %q, want the compat API", c.method, p, rec.Code, rec.Body.String())
			}
		}
	}

	// Public endpoints answer for real.
	h = newTestRouter(t, readyState(&compat.Server{ServerID: uuid.New()}))
	for _, p := range []string{"/System/Info/Public", "/SYSTEM/INFO/PUBLIC", "/compat/System/Info/Public"} {
		rec := do(h, p)
		var info map[string]any
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &info) != nil || info["ProductName"] != compat.ProductName {
			t.Errorf("GET %s: %d %s", p, rec.Code, rec.Body)
		}
	}
	if rec := do(h, "/web/index.html"); !strings.Contains(rec.Body.String(), "location.replace") {
		t.Errorf("/web/index.html: %d %s", rec.Code, rec.Body)
	}
	// Unknown endpoints in client casing are the compat API's 404, not the web page.
	for _, p := range []string{"/Plugins", "/LiveTv/Channels", "/Items/Counts/Extra"} {
		if rec := do(h, p); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: %d, want 404 from the compat API", p, rec.Code)
		}
	}

	// Turned off: root paths are the web client again, /compat still works.
	off, err := NewRouter(readyState(&compat.Server{ServerID: uuid.New()}),
		&api.Handler{ServerName: "Test", StartedAt: time.Now()}, RouterOptions{NoCompatAtRoot: true})
	if err != nil {
		t.Fatal(err)
	}
	if rec := do(off, "/System/Info/Public"); !isSPA(rec) {
		t.Errorf("root path with NoCompatAtRoot: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec := do(off, "/compat/System/Info/Public"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "ProductName") {
		t.Errorf("/compat with NoCompatAtRoot: %d", rec.Code)
	}
}

// TestCompatCORS: web-based TV apps call the API from their own origin.
func TestCompatCORS(t *testing.T) {
	h := newTestRouter(t, readyState(&compat.Server{ServerID: uuid.New()}))
	for _, p := range []string{"/Items", "/compat/Items", "/Users/AuthenticateByName"} {
		req := httptest.NewRequest(http.MethodOptions, p, nil)
		req.Header.Set("Origin", "file://")
		req.Header.Set("Access-Control-Request-Method", "GET")
		req.Header.Set("Access-Control-Request-Headers", "authorization")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent || rec.Header().Get("Access-Control-Allow-Origin") != "*" ||
			!strings.Contains(rec.Header().Get("Access-Control-Allow-Headers"), "Authorization") {
			t.Errorf("preflight %s: %d %v", p, rec.Code, rec.Header())
		}
		if rec.Header().Get("Access-Control-Allow-Credentials") != "" {
			t.Errorf("preflight %s allows credentials", p)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/System/Info/Public", nil)
	req.Header.Set("Origin", "http://tv.local")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("simple request: %d %v", rec.Code, rec.Header())
	}
	// The web client's own API gets no CORS headers.
	if rec := do(h, "/api/v1/system/info"); rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("CORS on /api/v1")
	}
}

func TestCompatSignInRateLimitedAtRoot(t *testing.T) {
	for _, c := range []struct {
		method, path string
		want         bool
	}{
		{"POST", "/Users/AuthenticateByName", true}, {"POST", "/users/authenticatebyname", true},
		{"POST", "/compat/Users/AuthenticateByName", true}, {"POST", "/Users/AuthenticateWithQuickConnect", true},
		{"GET", "/QuickConnect/Initiate", true}, {"POST", "/compat/QuickConnect/Initiate", true},
		{"GET", "/Users/Me", false}, {"GET", "/System/Info/Public", false},
	} {
		if got := rateLimited(httptest.NewRequest(c.method, c.path, nil)); got != c.want {
			t.Errorf("%s %s: limited=%v", c.method, c.path, got)
		}
	}
}

// TestSessionCookieNotUsedForCompat: compat requests never authenticate with
// the web session cookie (their responses allow any origin).
func TestSessionCookieNotUsedForCompat(t *testing.T) {
	var got string
	h := sessionFromCookie(true)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
	}))
	for path, want := range map[string]string{
		"/api/v1/me": "Bearer secret", "/library/x": "Bearer secret",
		"/Items": "", "/compat/Items": "", "/socket": "", "/Users/Me": "",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(&http.Cookie{Name: api.SessionCookie, Value: "secret"})
		h.ServeHTTP(httptest.NewRecorder(), req)
		if got != want {
			t.Errorf("%s: Authorization %q, want %q", path, got, want)
		}
	}
}
