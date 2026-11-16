package arr

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/requests"
)

func svc(t *testing.T, id string) Service {
	t.Helper()
	s, ok := Lookup(id)
	if !ok {
		t.Fatalf("no service %q", id)
	}
	return s
}

func TestServicesOrderAndBases(t *testing.T) {
	want := map[string]string{"radarr": "api/v3", "sonarr": "api/v3", "lidarr": "api/v1", "prowlarr": "api/v1", "bazarr": "api", "seerr": "api/v1"}
	order := []string{"radarr", "sonarr", "lidarr", "prowlarr", "bazarr", "seerr"}
	if len(Services) != len(order) {
		t.Fatalf("services: %d", len(Services))
	}
	for i, s := range Services {
		if s.ID != order[i] || s.APIBase != want[s.ID] {
			t.Errorf("service %d: %+v", i, s)
		}
	}
}

func TestCleanPathRejectsEscapes(t *testing.T) {
	r := svc(t, "radarr")
	for _, p := range []string{
		"", "/api/v3/movie", "api/v3/../../etc/passwd", "api/../x", "api/./v3", "..", "api/v3/..",
		"api//v3", "http://evil.example/api", "https://evil/api/v3", "//evil.example/api",
		"api\\v3", "api/v3\\..\\x", "api/v3/%2e%2e/x", "api/%2E%2E/x", "api/v3/%2f/x", "api%2Fv3", "api/v3%5Cx",
		"api/%2e/x", "api/v3/%00", "api/v3/x%0a", "api/v3/\x00", "api/v3/x\ty", "api/%252e%252e/x", "api/%zz",
		"api/v3/%", "api/v3/%2", "config", "logs/x", "initialize.js", "MediaCoverX/1", "apix/v3",
	} {
		if got, err := CleanPath(r, p); err == nil {
			t.Errorf("CleanPath(%q) = %q, want error", p, got)
		}
	}
}

func TestCleanPathAllowedPrefixes(t *testing.T) {
	ok := map[string][]string{
		"radarr":   {"api/v3/movie", "api/v3/movie/12", "api/v3/movie/", "MediaCover/12/poster.jpg", "mediacover/1/fanart-360.jpg", "api/v3/mediacover/12/poster.jpg", "api/v3/movie/lookup/name%20with%20space"},
		"sonarr":   {"api/v3/series", "MediaCover/3/banner.jpg"},
		"lidarr":   {"api/v1/artist", "MediaCover/Artists/1/poster.jpg", "api/v1/mediacover/artist/1/poster.jpg"},
		"prowlarr": {"api/v1/indexer", "api/v1/search"},
		"bazarr":   {"api/system/status", "api/movies"},
		"seerr":    {"api/v1/request", "imageproxy/tmdb/t/p/w300/x.jpg"},
	}
	for id, paths := range ok {
		for _, p := range paths {
			if got, err := CleanPath(svc(t, id), p); err != nil || got != p {
				t.Errorf("%s: CleanPath(%q) = %q, %v", id, p, got, err)
			}
		}
	}
	// Artwork paths only for the apps that serve them.
	for id, p := range map[string]string{"prowlarr": "MediaCover/1/poster.jpg", "bazarr": "MediaCover/1/x.jpg", "seerr": "MediaCover/1/x.jpg", "radarr": "imageproxy/x.jpg"} {
		if _, err := CleanPath(svc(t, id), p); err == nil {
			t.Errorf("%s: %q allowed", id, p)
		}
	}
}

func TestFilterQuery(t *testing.T) {
	cases := map[string]string{
		"":                                      "",
		"page=1&sortKey=title":                  "page=1&sortKey=title",
		"apikey=x&page=1":                       "page=1",
		"page=1&apiKey=x&API_KEY=y&api%5Fkey=z": "page=1",
		"term=a%20b&ApiKey":                     "term=a%20b",
		"q=apikey&x=1":                          "q=apikey&x=1",
		"a=1&&b=2":                              "a=1&b=2",
	}
	for in, want := range cases {
		if got := FilterQuery(in); got != want {
			t.Errorf("FilterQuery(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTargetKeepsURLBase(t *testing.T) {
	cases := []struct{ base, path, query, want string }{
		{"http://radarr:7878", "api/v3/movie", "", "http://radarr:7878/api/v3/movie"},
		{"http://radarr:7878/", "api/v3/movie", "page=2", "http://radarr:7878/api/v3/movie?page=2"},
		{"http://host:8080/radarr", "api/v3/movie/1", "", "http://host:8080/radarr/api/v3/movie/1"},
		{"https://host/sub/sonarr/", "api/v3/series", "a=1&b=2", "https://host/sub/sonarr/api/v3/series?a=1&b=2"},
		{"http://radarr:7878", "api/v3/movie/lookup/a%20b", "", "http://radarr:7878/api/v3/movie/lookup/a%20b"},
	}
	for _, c := range cases {
		u, err := Target(c.base, c.path, c.query)
		if err != nil || u.String() != c.want {
			t.Errorf("Target(%q, %q, %q) = %v, %v; want %q", c.base, c.path, c.query, u, err, c.want)
		}
	}
}

func TestOutboundHeadersStripAndInjectKey(t *testing.T) {
	in := http.Header{}
	in.Set("Authorization", "Bearer streamero-session")
	in.Set("Cookie", "streamero_session=abc")
	in.Set("X-Api-Key", "client-supplied")
	in.Set("Connection", "keep-alive, X-Foo")
	in.Set("X-Foo", "bar")
	in.Set("Te", "trailers")
	in.Set("Upgrade", "websocket")
	in.Set("X-Forwarded-For", "1.2.3.4")
	in.Set("Content-Type", "application/json")
	in.Set("Accept", "application/json")
	in.Set("Accept-Language", "hr")
	for _, s := range Services {
		out := OutboundHeaders(in, s, "stored-"+s.ID)
		if len(out) != 4 {
			t.Errorf("%s: headers %v", s.ID, out)
		}
		for _, h := range []string{"Authorization", "Cookie", "Connection", "X-Foo", "Te", "Upgrade", "X-Forwarded-For"} {
			if out.Get(h) != "" {
				t.Errorf("%s: %s forwarded", s.ID, h)
			}
		}
		if out.Get("Content-Type") != "application/json" || out.Get("Accept") != "application/json" || out.Get("Accept-Language") != "hr" {
			t.Errorf("%s: allowed headers lost: %v", s.ID, out)
		}
		name := "X-Api-Key"
		if s.ID == "bazarr" {
			name = "X-API-KEY"
		}
		if v := out[name]; len(v) != 1 || v[0] != "stored-"+s.ID {
			t.Errorf("%s: key header %q = %v (all: %v)", s.ID, name, v, out)
		}
	}
}

func TestCopyResponseHeaders(t *testing.T) {
	src := http.Header{}
	src.Set("Content-Type", "application/json")
	src.Set("Content-Length", "12")
	src.Set("Set-Cookie", "RadarrAuth=secret")
	src.Set("Connection", "close")
	src.Set("Transfer-Encoding", "chunked")
	src.Set("ETag", `"v1"`)
	src.Set("Last-Modified", "Mon, 02 Jan 2006 15:04:05 GMT")
	src.Set("Content-Disposition", "attachment; filename=backup.zip")
	src.Set("Cache-Control", "no-cache")
	src.Set("X-Application-Version", "5.0")
	dst := http.Header{}
	CopyResponseHeaders(dst, src, false)
	for _, h := range []string{"Set-Cookie", "Connection", "Transfer-Encoding", "X-Application-Version"} {
		if dst.Get(h) != "" {
			t.Errorf("%s passed through", h)
		}
	}
	for _, h := range passHeaders {
		if dst.Get(h) != src.Get(h) {
			t.Errorf("%s: %q, want %q", h, dst.Get(h), src.Get(h))
		}
	}
	if dst.Get("Content-Security-Policy") == "" {
		t.Error("no sandbox policy")
	}
	img := http.Header{}
	CopyResponseHeaders(img, src, true)
	if img.Get("Cache-Control") != "private, max-age=86400" {
		t.Errorf("image cache: %q", img.Get("Cache-Control"))
	}
}

func TestLimiter(t *testing.T) {
	now := time.Unix(0, 0)
	l := &limiter{rate: 20, burst: 60, users: map[uuid.UUID]*bucket{}, now: func() time.Time { return now }}
	u, other := uuid.New(), uuid.New()
	for i := range 60 {
		if ok, _ := l.allow(u); !ok {
			t.Fatalf("request %d refused within the burst", i)
		}
	}
	if ok, wait := l.allow(u); ok || wait <= 0 {
		t.Fatalf("61st request: %v %v", ok, wait)
	}
	if ok, _ := l.allow(other); !ok {
		t.Fatal("another user is limited too")
	}
	now = now.Add(time.Second)
	for i := range 20 {
		if ok, _ := l.allow(u); !ok {
			t.Fatalf("refill request %d refused", i)
		}
	}
}

func TestServeRefusesEarly(t *testing.T) {
	p := &Proxy{}
	r := svc(t, "radarr")
	call := func(method, path, base, key string, body io.Reader) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		p.Serve(w, httptest.NewRequest(method, "/x", body), uuid.New(), r, base, key, path)
		return w
	}
	if w := call(http.MethodPatch, "api/v3/movie", "http://x", "k", nil); w.Code != 405 {
		t.Errorf("PATCH on radarr: %d", w.Code)
	}
	if w := call(http.MethodGet, "api/../x", "http://x", "k", nil); w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_path") {
		t.Errorf("traversal: %d %s", w.Code, w.Body)
	}
	if w := call(http.MethodGet, "api/v3/movie", "", "", nil); w.Code != 409 || !strings.Contains(w.Body.String(), "arr_not_configured") {
		t.Errorf("unconfigured: %d %s", w.Code, w.Body)
	}
	if w := call(http.MethodGet, "api/v3/movie", "http://127.0.0.1:1", "k", nil); w.Code != 502 || !strings.Contains(w.Body.String(), "arr_unreachable") {
		t.Errorf("unreachable: %d %s", w.Code, w.Body)
	}
}

// A body without a Content-Length is cut off past the limit.
func TestServeChunkedBodyLimit(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(204)
	}))
	defer up.Close()
	p := &Proxy{}
	big := io.MultiReader(bytes.NewReader(make([]byte, MaxBody)), strings.NewReader("x"))
	req := httptest.NewRequest(http.MethodPost, "/x", io.NopCloser(big))
	req.ContentLength = -1
	w := httptest.NewRecorder()
	p.Serve(w, req, uuid.New(), svc(t, "radarr"), up.URL, "k", "api/v3/system/backup/restore/upload")
	if w.Code != 413 {
		t.Fatalf("chunked over limit: %d %s", w.Code, w.Body)
	}
}

func TestStatusesReportsVersionsAndCaches(t *testing.T) {
	var hits atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch {
		case r.URL.Path == "/api/system/status" && r.Header.Get("X-Api-Key") == "b":
			_, _ = io.WriteString(w, `{"data":{"bazarr_version":"1.5.1"}}`)
		case r.URL.Path == "/radarr/api/v3/system/status" && r.Header.Get("X-Api-Key") == "r":
			_, _ = io.WriteString(w, `{"version":"5.14.0"}`)
		default:
			w.WriteHeader(401)
		}
	}))
	defer up.Close()
	p := &Proxy{}
	st := requests.Settings{RadarrURL: up.URL + "/radarr", RadarrKey: "r", BazarrURL: up.URL, BazarrKey: "b",
		SonarrURL: up.URL, SonarrKey: "wrong", LidarrURL: "http://127.0.0.1:1", LidarrKey: "l"}
	got := p.Statuses(context.Background(), st)
	byID := map[string]Status{}
	for _, s := range got {
		byID[s.ID] = s
	}
	if r := byID["radarr"]; !r.Reachable || r.Version != "5.14.0" || r.APIBase != "api/v3" {
		t.Errorf("radarr: %+v", r)
	}
	if b := byID["bazarr"]; !b.Reachable || b.Version != "1.5.1" {
		t.Errorf("bazarr: %+v", b)
	}
	if s := byID["sonarr"]; s.Reachable || !strings.Contains(s.Error, "401") {
		t.Errorf("sonarr: %+v", s)
	}
	if l := byID["lidarr"]; !l.Configured || l.Reachable || l.Error == "" {
		t.Errorf("lidarr: %+v", l)
	}
	if pr := byID["prowlarr"]; pr.Configured || pr.Reachable {
		t.Errorf("prowlarr: %+v", pr)
	}
	before := hits.Load()
	p.Statuses(context.Background(), st)
	if hits.Load() != before {
		t.Errorf("not cached: %d more calls", hits.Load()-before)
	}
	st.RadarrKey = "changed"
	p.Statuses(context.Background(), st)
	if hits.Load() != before+1 {
		t.Errorf("a changed key must be checked again: %d calls", hits.Load()-before)
	}
}
