package app

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// fakeArr is a stand-in for Radarr behind a URL base (/radarr), following
// Radarr's public API documentation.
type fakeArr struct {
	mu       sync.Mutex
	lastPath string
	lastQ    string
	lastKey  string
	lastAuth string
	lastCook string
	lastBody int
}

func (f *fakeArr) handler(other string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.lastPath, f.lastQ, f.lastKey = r.URL.EscapedPath(), r.URL.RawQuery, r.Header.Get("X-Api-Key")
		f.lastAuth, f.lastCook, f.lastBody = r.Header.Get("Authorization"), r.Header.Get("Cookie"), len(body)
		f.mu.Unlock()
		if r.Header.Get("X-Api-Key") != "radarr-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/radarr/api/v3/system/status":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"version":"5.14.0.9383"}`)
		case "/radarr/api/v3/movie":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Set-Cookie", "RadarrAuth=secret; Path=/")
			w.Header().Set("ETag", `"m1"`)
			if r.Method == http.MethodPost {
				w.WriteHeader(http.StatusCreated)
			}
			_, _ = io.WriteString(w, `[{"id":1,"title":"The Matrix"}]`)
		case "/radarr/MediaCover/1/poster.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte{0xff, 0xd8, 0xff})
		case "/radarr/backup/manual/radarr_backup_v5.14.zip":
			w.Header().Set("Content-Type", "application/zip")
			w.Header().Set("Content-Disposition", `attachment; filename="radarr_backup_v5.14.zip"`)
			_, _ = w.Write(make([]byte, 3<<20))
		case "/radarr/api/v3/elsewhere":
			http.Redirect(w, r, other+"/steal", http.StatusFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func TestArrProxyIntegration(t *testing.T) {
	srv := newAuthTestServer(t)
	c := newClient(t, srv)
	setupAdmin(t, c)

	var stolen atomic.Int32
	thief := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "" {
			stolen.Add(1)
		}
		_, _ = io.WriteString(w, "{}")
	}))
	defer thief.Close()
	fake := &fakeArr{}
	radarr := httptest.NewServer(fake.handler(thief.URL))
	defer radarr.Close()

	var rs map[string]any
	if s := c.do("PUT", "/settings/requests", map[string]any{"usersCanRequest": true,
		"radarrUrl": radarr.URL + "/radarr/", "radarrApiKey": "radarr-key",
		"lidarrUrl": "http://127.0.0.1:1", "lidarrApiKey": "lidarr-key",
		"prowlarrUrl": radarr.URL + "/prowlarr"}, &rs); s != 200 {
		t.Fatalf("save: HTTP %d %v", s, rs)
	}
	if rs["lidarrConfigured"] != true || rs["lidarrUrl"] != "http://127.0.0.1:1" || rs["prowlarrConfigured"] != false || rs["prowlarrUrl"] != radarr.URL+"/prowlarr" {
		t.Fatalf("lidarr/prowlarr settings: %v", rs)
	}
	if _, leaked := rs["lidarrApiKey"]; leaked {
		t.Fatal("key returned")
	}

	// listArrServices: all six, in order, with reachability.
	var list struct {
		Services []struct {
			ID         string `json:"id"`
			Configured bool   `json:"configured"`
			Reachable  bool   `json:"reachable"`
			Version    string `json:"version"`
			Error      string `json:"error"`
			APIBase    string `json:"apiBase"`
		} `json:"services"`
	}
	if s := c.do("GET", "/arr/services", nil, &list); s != 200 || len(list.Services) != 6 {
		t.Fatalf("services: HTTP %d %+v", s, list)
	}
	for i, id := range []string{"radarr", "sonarr", "lidarr", "prowlarr", "bazarr", "seerr"} {
		if list.Services[i].ID != id {
			t.Fatalf("order: %+v", list.Services)
		}
	}
	if r := list.Services[0]; !r.Configured || !r.Reachable || r.Version != "5.14.0.9383" || r.APIBase != "api/v3" {
		t.Errorf("radarr: %+v", r)
	}
	if l := list.Services[2]; !l.Configured || l.Reachable || l.Error == "" || l.APIBase != "api/v1" {
		t.Errorf("lidarr (dead): %+v", l)
	}
	if s := list.Services[1]; s.Configured || s.Reachable {
		t.Errorf("sonarr: %+v", s)
	}

	call := func(token, method, path string, body io.Reader, hdr map[string]string) (*http.Response, string) {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+"/api/v1/arr/"+path, body)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		raw, _ := io.ReadAll(resp.Body)
		return resp, string(raw)
	}

	// Admin: forwarded under the URL base with the stored key, the query kept
	// (minus key parameters), the caller's credentials not forwarded.
	resp, body := call(c.token, "GET", "radarr/proxy/api/v3/movie?page=2&apikey=client&sortKey=title", nil,
		map[string]string{"X-Api-Key": "client-key", "Cookie": "streamero_session=x"})
	if resp.StatusCode != 200 || !strings.Contains(body, "The Matrix") {
		t.Fatalf("proxy GET: %d %s", resp.StatusCode, body)
	}
	fake.mu.Lock()
	if fake.lastPath != "/radarr/api/v3/movie" || fake.lastQ != "page=2&sortKey=title" || fake.lastKey != "radarr-key" || fake.lastAuth != "" || fake.lastCook != "" {
		t.Errorf("upstream saw path=%q query=%q key=%q auth=%q cookie=%q", fake.lastPath, fake.lastQ, fake.lastKey, fake.lastAuth, fake.lastCook)
	}
	fake.mu.Unlock()
	if resp.Header.Get("Set-Cookie") != "" || resp.Header.Get("ETag") != `"m1"` || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		t.Errorf("response headers: %v", resp.Header)
	}

	// Artwork outside the API, cacheable by the client.
	resp, body = call(c.token, "GET", "radarr/proxy/MediaCover/1/poster.jpg", nil, nil)
	if resp.StatusCode != 200 || len(body) != 3 || resp.Header.Get("Cache-Control") != "private, max-age=86400" {
		t.Errorf("poster: %d %q %v", resp.StatusCode, body, resp.Header)
	}

	// Backup downloads (outside the API prefix), streamed with their file name; GET only.
	resp, body = call(c.token, "GET", "radarr/proxy/backup/manual/radarr_backup_v5.14.zip", nil, nil)
	if resp.StatusCode != 200 || len(body) != 3<<20 || resp.Header.Get("Content-Disposition") != `attachment; filename="radarr_backup_v5.14.zip"` {
		t.Errorf("backup: %d %d bytes %v", resp.StatusCode, len(body), resp.Header)
	}
	if resp, _ := call(c.token, "DELETE", "radarr/proxy/backup/manual/radarr_backup_v5.14.zip", nil, nil); resp.StatusCode != 405 {
		t.Errorf("DELETE backup file: %d", resp.StatusCode)
	}

	// Path safety.
	for _, p := range []string{"radarr/proxy/api/v3/../../etc/passwd", "radarr/proxy/api/%2e%2e/x", "radarr/proxy/api%2Fv3/movie",
		"radarr/proxy//evil.example/api", "radarr/proxy/config.xml"} {
		if resp, body := call(c.token, "GET", p, nil, nil); resp.StatusCode != 400 || !strings.Contains(body, "invalid_path") {
			t.Errorf("%s: %d %s", p, resp.StatusCode, body)
		}
	}

	// Body limit: 10 MiB passes, 20 MiB is refused (the rest of /api/v1 allows 1 MiB).
	resp, body = call(c.token, "POST", "radarr/proxy/api/v3/movie", bytes.NewReader(make([]byte, 10<<20)), map[string]string{"Content-Type": "application/json"})
	fake.mu.Lock()
	got := fake.lastBody
	fake.mu.Unlock()
	if resp.StatusCode != 201 || got != 10<<20 {
		t.Errorf("10 MiB body: %d (upstream got %d bytes) %s", resp.StatusCode, got, body)
	}
	if resp, body := call(c.token, "POST", "radarr/proxy/api/v3/movie", bytes.NewReader(make([]byte, 20<<20)), nil); resp.StatusCode != 413 || !strings.Contains(body, "request_too_large") {
		t.Errorf("20 MiB body: %d %s", resp.StatusCode, body)
	}

	// A redirect to another host is not followed (the key would go with it).
	if resp, body := call(c.token, "GET", "radarr/proxy/api/v3/elsewhere", nil, nil); resp.StatusCode != 502 || !strings.Contains(body, "arr_unreachable") || stolen.Load() != 0 {
		t.Errorf("cross-host redirect: %d %s (key sent %d times)", resp.StatusCode, body, stolen.Load())
	}
	// Not configured / unreachable / unknown service / method.
	if resp, body := call(c.token, "GET", "sonarr/proxy/api/v3/series", nil, nil); resp.StatusCode != 409 || !strings.Contains(body, "arr_not_configured") {
		t.Errorf("unconfigured: %d %s", resp.StatusCode, body)
	}
	if resp, body := call(c.token, "GET", "prowlarr/proxy/api/v1/indexer", nil, nil); resp.StatusCode != 409 || !strings.Contains(body, "arr_not_configured") {
		t.Errorf("no key: %d %s", resp.StatusCode, body)
	}
	if resp, body := call(c.token, "GET", "lidarr/proxy/api/v1/artist", nil, nil); resp.StatusCode != 502 || !strings.Contains(body, "arr_unreachable") {
		t.Errorf("unreachable: %d %s", resp.StatusCode, body)
	}
	if resp, _ := call(c.token, "GET", "plex/proxy/api/x", nil, nil); resp.StatusCode != 404 {
		t.Errorf("unknown service: %d", resp.StatusCode)
	}
	if resp, _ := call(c.token, "PATCH", "radarr/proxy/api/v3/movie", nil, nil); resp.StatusCode != 405 {
		t.Errorf("PATCH: %d", resp.StatusCode)
	}

	// Signed out, normal user, kids profile.
	if resp, body := call("", "GET", "radarr/proxy/api/v3/movie", nil, nil); resp.StatusCode != 401 || !strings.Contains(body, "unauthenticated") {
		t.Errorf("anonymous: %d %s", resp.StatusCode, body)
	}
	c.do("POST", "/users", map[string]any{"username": "arrviewer", "password": "viewer-pass-1"}, nil)
	u := newClient(t, srv)
	var login authResult
	u.do("POST", "/auth/login", map[string]any{"username": "arrviewer", "password": "viewer-pass-1", "device": map[string]any{"name": "t"}}, &login)
	if resp, body := call(login.Token, "GET", "radarr/proxy/api/v3/movie", nil, nil); resp.StatusCode != 403 || !strings.Contains(body, "forbidden") {
		t.Errorf("viewer: %d %s", resp.StatusCode, body)
	}
	u.token = login.Token
	if s := u.do("GET", "/arr/services", nil, nil); s != 403 {
		t.Errorf("viewer services: %d", s)
	}
	var kid struct {
		ID string `json:"id"`
	}
	if s := c.do("POST", "/profiles", map[string]any{"name": "Mia", "color": "#3dd68c", "isKid": true, "maxRating": 7}, &kid); s != 201 {
		t.Fatalf("kid: HTTP %d", s)
	}
	tablet := newClient(t, srv)
	var tl authResult
	tablet.do("POST", "/auth/login", map[string]any{"username": "Admin", "password": "admin-pass-1", "device": map[string]any{"name": "tablet"}}, &tl)
	tablet.token = tl.Token
	if s := tablet.do("POST", "/profiles/"+kid.ID+"/select", nil, nil); s != 204 {
		t.Fatalf("select kid: %d", s)
	}
	if resp, body := call(tl.Token, "GET", "radarr/proxy/api/v3/movie", nil, nil); resp.StatusCode != 403 || !strings.Contains(body, "forbidden") {
		t.Errorf("kid profile: %d %s", resp.StatusCode, body)
	}
	if s := tablet.do("GET", "/arr/services", nil, nil); s != 403 {
		t.Errorf("kid services: %d", s)
	}
	// The web client's session cookie works too (same auth as /api/v1).
	resp, _ = call("", "GET", "radarr/proxy/api/v3/system/status", nil, map[string]string{"Cookie": "streamero_session=" + c.token})
	if resp.StatusCode != 200 {
		t.Errorf("cookie session: %d", resp.StatusCode)
	}
}
