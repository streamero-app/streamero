package app

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/api"
	"github.com/streamero-app/streamero/server/internal/netx"
	"github.com/streamero-app/streamero/server/internal/webui"
)

var bundle = "export const app = '" + strings.Repeat("streamero ", 5000) + "';\n"

func readyRouter(t *testing.T, opts RouterOptions) http.Handler {
	t.Helper()
	state := &State{}
	id := uuid.New()
	state.serverID.Store(&id)
	state.ready.Store(true)
	if opts.Web == nil {
		opts.Web = webui.FromFS(fstest.MapFS{
			"index.html":        {Data: []byte("<!doctype html><title>Streamero</title><script type=module src=/assets/app.js></script>")},
			"assets/app-abc.js": {Data: []byte(bundle)},
		})
	}
	r, err := NewRouter(state, &api.Handler{State: state, ServerName: "Test", StartedAt: time.Now()}, opts)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestOversizedBodiesAreRejected(t *testing.T) {
	h := readyRouter(t, RouterOptions{})
	big := bytes.Repeat([]byte("a"), 2<<20)
	for _, c := range []struct {
		name   string
		method string
		path   string
		length bool // send Content-Length (otherwise chunked: the reader stops it)
	}{
		{"declared", http.MethodPost, "/api/v1/quickconnect/poll", true},
		{"chunked", http.MethodPost, "/api/v1/quickconnect/poll", false},
		{"login", http.MethodPost, "/api/v1/auth/login", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			body := append(append([]byte(`{"secret":"`), big...), '"', '}')
			var rd io.Reader = bytes.NewReader(body)
			if !c.length {
				rd = io.MultiReader(rd) // hides the length
			}
			req := httptest.NewRequest(c.method, c.path, rd)
			req.Header.Set("Content-Type", "application/json")
			if !c.length {
				req.ContentLength = -1
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusRequestEntityTooLarge || !strings.Contains(rec.Body.String(), "request_too_large") {
				t.Fatalf("status %d: %s", rec.Code, rec.Body)
			}
		})
	}
	// Pictures get their own, larger limit; backup uploads have none.
	for path, want := range map[string]int64{
		"/api/v1/auth/me/avatar":                           maxAvatarBody,
		"/api/v1/profiles/" + uuid.NewString() + "/avatar": maxAvatarBody,
		"/api/v1/backups/upload":                           -1,
		"/api/v1/quickconnect/initiate":                    maxJSONBody,
		"/api/v1/profiles/x/avatar/../poll":                maxJSONBody,
	} {
		if got := bodyLimit(httptest.NewRequest(http.MethodPut, path, nil)); got != want {
			t.Errorf("%s: limit %d, want %d", path, got, want)
		}
	}
}

func TestWebClientSecurityHeaders(t *testing.T) {
	h := readyRouter(t, RouterOptions{})
	rec := do(h, "/library/123")
	csp := rec.Header().Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'self'", "script-src 'self';", "frame-ancestors 'none'", "object-src 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP lacks %q: %s", want, csp)
		}
	}
	if strings.Contains(csp, "script-src 'self' 'unsafe-inline'") {
		t.Error("inline scripts must stay blocked")
	}
	if rec.Header().Get("Strict-Transport-Security") != "" {
		t.Error("HSTS sent over plain HTTP")
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Strict-Transport-Security") != "max-age=31536000" {
		t.Errorf("HSTS over HTTPS: %q", rec.Header().Get("Strict-Transport-Security"))
	}
	// The API and the media routes do not get the web client's policy.
	if rec := do(h, "/api/v1/system/info"); rec.Header().Get("Content-Security-Policy") != "" {
		t.Error("CSP on API responses")
	}
}

func TestWebClientAndJSONAreGzipped(t *testing.T) {
	h := readyRouter(t, RouterOptions{})
	get := func(path string, hdr ...string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Accept-Encoding", "gzip")
		for i := 0; i+1 < len(hdr); i += 2 {
			req.Header.Set(hdr[i], hdr[i+1])
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	rec := get("/assets/app-abc.js")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("js asset: %d %q", rec.Code, rec.Header().Get("Content-Encoding"))
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := io.ReadAll(zr)
	if string(plain) != bundle {
		t.Fatal("decompressed asset differs")
	}
	if rec := get("/api/v1/system/info"); rec.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("JSON not compressed: %q", rec.Header().Get("Content-Encoding"))
	}
	// Byte ranges stay byte ranges.
	if rec := get("/assets/app-abc.js", "Range", "bytes=0-9"); rec.Code != http.StatusPartialContent || rec.Header().Get("Content-Encoding") != "" || rec.Body.Len() != 10 {
		t.Errorf("range on an asset: %d %q %d", rec.Code, rec.Header().Get("Content-Encoding"), rec.Body.Len())
	}
	// Media is outside the compressor: a range request reaches the stream server untouched
	// (here it is still starting, so the answer is the plain 503).
	if rec := get("/stream/file/x/video.mp4", "Range", "bytes=0-1"); rec.Header().Get("Content-Encoding") != "" {
		t.Error("stream response was compressed")
	}
}

func TestQuickConnectAuthorizeIsRateLimited(t *testing.T) {
	for _, path := range []string{"/api/v1/quickconnect/authorize", "/QuickConnect/Authorize", "/compat/quickconnect/authorize"} {
		if !rateLimited(httptest.NewRequest(http.MethodPost, path+"?Code=123456", nil)) {
			t.Errorf("%s is not rate limited", path)
		}
	}
	h := readyRouter(t, RouterOptions{AuthLimiter: netx.NewLimiter(1, 1)})
	try := func() int {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/quickconnect/authorize", strings.NewReader(`{"code":"123456"}`)))
		return rec.Code
	}
	try()
	if c := try(); c != http.StatusTooManyRequests {
		t.Fatalf("second authorize: %d, want 429", c)
	}
}
