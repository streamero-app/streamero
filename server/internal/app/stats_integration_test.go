package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"

	"github.com/streamero-app/streamero/server/internal/metadata"
)

// statsCall sends one request with the given headers and returns the status,
// content type and body.
func statsCall(t *testing.T, method, url string, hdr map[string]string, body string) (int, string, string) {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
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
	return resp.StatusCode, resp.Header.Get("Content-Type"), string(raw)
}

// createKey makes an API key through the API and returns its secret.
func createKey(t *testing.T, c *client, name string, readOnly bool) string {
	t.Helper()
	var created struct {
		Key    string `json:"key"`
		APIKey struct {
			ReadOnly bool `json:"readOnly"`
		} `json:"apiKey"`
	}
	if s := c.do("POST", "/apikeys", map[string]any{"name": name, "readOnly": readOnly}, &created); s != 201 {
		t.Fatalf("create key: HTTP %d", s)
	}
	if created.APIKey.ReadOnly != readOnly {
		t.Fatalf("created key readOnly = %v, want %v", created.APIKey.ReadOnly, readOnly)
	}
	return created.Key
}

type statsBody struct {
	Version        *string `json:"version"`
	Commit         *string `json:"commit"`
	Movies         *int64  `json:"movies"`
	Series         *int64  `json:"series"`
	Episodes       *int64  `json:"episodes"`
	Albums         *int64  `json:"albums"`
	Tracks         *int64  `json:"tracks"`
	Books          *int64  `json:"books"`
	Audiobooks     *int64  `json:"audiobooks"`
	Photos         *int64  `json:"photos"`
	Videos         *int64  `json:"videos"`
	Libraries      *int64  `json:"libraries"`
	Users          *int64  `json:"users"`
	UsageBytes     *int64  `json:"usageBytes"`
	ActiveStreams  *int64  `json:"activeStreams"`
	Transcodes     *int64  `json:"transcodes"`
	Processing     *int64  `json:"processing"`
	Failed         *int64  `json:"failed"`
	DiskTotalBytes *int64  `json:"diskTotalBytes"`
	DiskFreeBytes  *int64  `json:"diskFreeBytes"`
	LastAddedAt    *string `json:"lastAddedAt"`
	Jobs           []struct {
		Queue   *string `json:"queue"`
		Queued  *int64  `json:"queued"`
		Running *int64  `json:"running"`
		Failed  *int64  `json:"failed"`
	} `json:"jobs"`
}

func parseStats(t *testing.T, status int, ctype, body string) statsBody {
	t.Helper()
	if status != 200 || !strings.HasPrefix(ctype, "application/json") {
		t.Fatalf("stats: HTTP %d %q %s", status, ctype, body)
	}
	var s statsBody
	if err := json.Unmarshal([]byte(body), &s); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	for name, v := range map[string]*int64{"movies": s.Movies, "series": s.Series, "episodes": s.Episodes, "albums": s.Albums,
		"tracks": s.Tracks, "books": s.Books, "audiobooks": s.Audiobooks, "photos": s.Photos, "videos": s.Videos,
		"libraries": s.Libraries, "users": s.Users, "usageBytes": s.UsageBytes, "activeStreams": s.ActiveStreams,
		"transcodes": s.Transcodes, "processing": s.Processing, "failed": s.Failed, "diskTotalBytes": s.DiskTotalBytes,
		"diskFreeBytes": s.DiskFreeBytes} {
		if v == nil {
			t.Fatalf("field %s missing in %s", name, body)
		}
	}
	if s.Version == nil || s.Commit == nil || s.Jobs == nil {
		t.Fatalf("version/commit/jobs missing in %s", body)
	}
	for _, j := range s.Jobs {
		if j.Queue == nil || j.Queued == nil || j.Running == nil || j.Failed == nil {
			t.Fatalf("job entry incomplete in %s", body)
		}
	}
	return s
}

func TestSystemStatsIntegration(t *testing.T) {
	srv := newAuthTestServer(t)
	ctx := context.Background()
	// Other tests leave their libraries and jobs behind.
	if _, err := testPool.Exec(ctx, `DELETE FROM libraries; DELETE FROM river_job`); err != nil {
		t.Fatal(err)
	}
	adminC := newClient(t, srv)
	admin := setupAdmin(t, adminC)
	fullKey := createKey(t, adminC, "Scripts", false)
	roKey := createKey(t, adminC, "Dashboard", true)

	// Listing tells the two keys apart.
	var keys []struct {
		Name     string `json:"name"`
		ReadOnly bool   `json:"readOnly"`
	}
	adminC.do("GET", "/apikeys", nil, &keys)
	if len(keys) != 2 {
		t.Fatalf("keys: %+v", keys)
	}
	for _, k := range keys {
		if k.ReadOnly != (k.Name == "Dashboard") {
			t.Fatalf("keys: %+v", keys)
		}
	}

	stats, native := srv.URL+"/api/stats", srv.URL+"/api/v1/system/stats"

	// No credentials: JSON, never the web client's page.
	for _, u := range []string{stats, native} {
		s, ct, body := statsCall(t, "GET", u, nil, "")
		if s != 401 || !strings.HasPrefix(ct, "application/json") || !strings.Contains(body, `"code":"unauthenticated"`) {
			t.Fatalf("%s without credentials: HTTP %d %q %s", u, s, ct, body)
		}
	}
	// A wrong key, and a session token in X-Api-Key (only API keys are taken from it).
	for _, hdr := range []map[string]string{{"X-Api-Key": "sk_wrong"}, {"X-Api-Key": admin.Token}} {
		if s, ct, body := statsCall(t, "GET", stats, hdr, ""); s != 401 || !strings.HasPrefix(ct, "application/json") {
			t.Fatalf("stats with %v: HTTP %d %q %s", hdr, s, ct, body)
		}
	}

	// Empty library: zero counts, no lastAddedAt, the default queue listed.
	s := parseStats(t, 0+200, "application/json", func() string {
		code, ct, body := statsCall(t, "GET", stats, map[string]string{"X-Api-Key": roKey}, "")
		if code != 200 || !strings.HasPrefix(ct, "application/json") {
			t.Fatalf("empty stats: HTTP %d %q %s", code, ct, body)
		}
		if strings.Contains(body, "lastAddedAt") {
			t.Fatalf("lastAddedAt present on an empty library: %s", body)
		}
		return body
	}())
	if *s.Movies != 0 || *s.Videos != 0 || *s.UsageBytes != 0 || *s.Libraries != 0 || *s.Users != 1 || *s.Failed != 0 || *s.Processing != 0 {
		t.Fatalf("empty stats: %+v", s)
	}
	if len(s.Jobs) != 1 || *s.Jobs[0].Queue != "default" || *s.Jobs[0].Queued != 0 || *s.Jobs[0].Failed != 0 {
		t.Fatalf("empty jobs: %+v", s.Jobs)
	}
	if *s.DiskTotalBytes <= 0 || *s.DiskFreeBytes < 0 || *s.DiskFreeBytes > *s.DiskTotalBytes {
		t.Fatalf("disk: total %d free %d", *s.DiskTotalBytes, *s.DiskFreeBytes)
	}

	// Seed: 2 movies (one failed lookup, one waiting), a series with a season and
	// 2 episodes, an album with a track, a book, an audiobook, 2 photos and a
	// video; 3 files (one unreadable); jobs in every state.
	const lib = "01a5a000-0000-7000-8000-000000000001"
	item := func(n, kind, parent, extraCols, extraVals string) string {
		p := "NULL"
		if parent != "" {
			p = "'01a5a000-0000-7000-8000-0000000001" + parent + "'"
		}
		return `INSERT INTO items (id, library_id, parent_id, kind, path, title, sort_title` + extraCols + `) VALUES (
			'01a5a000-0000-7000-8000-0000000001` + n + `', '` + lib + `', ` + p + `, '` + kind + `', '/x/` + n + `', 'T` + n + `', 'T` + n + `'` + extraVals + `);`
	}
	seed := `INSERT INTO libraries (id, name, kind, paths) VALUES ('` + lib + `', 'S', 'mixed', '{/x}');` +
		item("01", "movie", "", ", metadata_refreshed_at, metadata_error", ", now(), 'no match'") +
		item("02", "movie", "", "", "") +
		item("03", "series", "", ", metadata_refreshed_at", ", now()") +
		item("04", "season", "03", "", "") +
		item("05", "episode", "04", ", added_at", ", '2031-02-03T04:05:06Z'") +
		item("06", "episode", "04", "", "") +
		item("07", "artist", "", "", "") +
		item("08", "album", "07", "", "") +
		item("09", "track", "08", "", "") +
		item("10", "book", "", "", "") +
		item("11", "audiobook", "", "", "") +
		item("12", "photoalbum", "", "", "") +
		item("13", "photo", "12", "", "") +
		item("14", "photo", "12", "", "") +
		item("15", "video", "12", "", "") + `
		INSERT INTO media_files (id, item_id, library_id, path, size, mtime, probe_error) VALUES
			('01a5a000-0000-7000-8000-000000000201', '01a5a000-0000-7000-8000-000000000101', '` + lib + `', '/x/a.mkv', 1000, now(), NULL),
			('01a5a000-0000-7000-8000-000000000202', '01a5a000-0000-7000-8000-000000000105', '` + lib + `', '/x/b.mkv', 2000, now(), NULL),
			('01a5a000-0000-7000-8000-000000000203', '01a5a000-0000-7000-8000-000000000106', '` + lib + `', '/x/c.mkv', 500, now(), 'broken');
		INSERT INTO river_job (kind, state, max_attempts, args, finalized_at) VALUES
			('t.a', 'available', 3, '{}', NULL), ('t.b', 'scheduled', 3, '{}', NULL), ('t.c', 'retryable', 3, '{}', NULL),
			('t.d', 'running', 3, '{}', NULL), ('t.e', 'discarded', 3, '{}', now()), ('t.f', 'discarded', 3, '{}', now()),
			('t.g', 'completed', 3, '{}', now()), ('t.h', 'cancelled', 3, '{}', now());
		INSERT INTO river_job (kind, state, queue, max_attempts, args) VALUES ('t.i', 'available', 'other', 3, '{}');`
	if _, err := testPool.Exec(ctx, seed); err != nil {
		t.Fatal(err)
	}

	// The database part is cached: the poll right after the seed costs nothing new.
	s = parseStats(t, 200, "application/json", func() string {
		_, _, b := statsCall(t, "GET", stats, map[string]string{"X-Api-Key": roKey}, "")
		return b
	}())
	if *s.Movies != 0 {
		t.Fatalf("stats were not cached: %+v", *s.Movies)
	}
	testHandler.ForgetStats()

	check := func(name string, s statsBody, processing int64) {
		t.Helper()
		got := []int64{*s.Movies, *s.Series, *s.Episodes, *s.Albums, *s.Tracks, *s.Books, *s.Audiobooks, *s.Photos, *s.Videos,
			*s.Libraries, *s.Users, *s.UsageBytes, *s.Failed, *s.Processing, *s.ActiveStreams, *s.Transcodes}
		want := []int64{2, 1, 2, 1, 1, 1, 1, 2, 5, 1, 1, 3500, 2, processing, 0, 0}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s: counts %v, want %v", name, got, want)
			}
		}
		if s.LastAddedAt == nil || !strings.HasPrefix(*s.LastAddedAt, "2031-02-03T04:05:06") {
			t.Fatalf("%s: lastAddedAt %v", name, s.LastAddedAt)
		}
		if len(s.Jobs) != 2 || *s.Jobs[0].Queue != "default" || *s.Jobs[0].Queued != 3 || *s.Jobs[0].Running != 1 || *s.Jobs[0].Failed != 2 ||
			*s.Jobs[1].Queue != "other" || *s.Jobs[1].Queued != 1 || *s.Jobs[1].Running != 0 || *s.Jobs[1].Failed != 0 {
			t.Fatalf("%s: jobs %s", name, mustJSON(s.Jobs))
		}
	}
	// Every way of signing in, on both addresses. No metadata key yet: nothing waits.
	for name, c := range map[string]struct {
		url string
		hdr map[string]string
	}{
		"read-only key, X-Api-Key":       {stats, map[string]string{"X-Api-Key": roKey}},
		"read-only key, bearer":          {stats, map[string]string{"Authorization": "Bearer " + roKey}},
		"admin key, X-Api-Key":           {stats, map[string]string{"X-Api-Key": fullKey}},
		"admin key, bearer":              {stats, map[string]string{"Authorization": "Bearer " + fullKey}},
		"admin session, bearer":          {stats, map[string]string{"Authorization": "Bearer " + admin.Token}},
		"admin session, cookie":          {stats, map[string]string{"Cookie": "streamero_session=" + admin.Token}},
		"native, read-only X-Api-Key":    {native, map[string]string{"X-Api-Key": roKey}},
		"native, read-only bearer":       {native, map[string]string{"Authorization": "Bearer " + roKey}},
		"native, admin session":          {native, map[string]string{"Authorization": "Bearer " + admin.Token}},
		"key wins over a foreign cookie": {stats, map[string]string{"X-Api-Key": roKey, "Cookie": "streamero_session=nope"}},
	} {
		code, ct, body := statsCall(t, "GET", c.url, c.hdr, "")
		check(name, parseStats(t, code, ct, body), 0)
	}

	// With a metadata key, the unvisited movie is waiting for its metadata.
	if err := metadata.SaveSettings(ctx, testState.Settings(), metadata.Settings{TMDbKey: "k", Language: "en-US", Country: "US"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM settings WHERE key = $1`, metadata.KeySettings)
	})
	testHandler.ForgetStats()
	code, ct, body := statsCall(t, "GET", stats, map[string]string{"X-Api-Key": roKey}, "")
	check("with metadata key", parseStats(t, code, ct, body), 1)

	adminC.do("POST", "/users", map[string]any{"username": "eva", "password": "eva-pass-1"}, nil)
	eva := newClient(t, srv)
	var res authResult
	eva.do("POST", "/auth/login", map[string]any{"username": "eva", "password": "eva-pass-1"}, &res)

	// Not an administrator: 403 JSON on both addresses.
	for _, u := range []string{stats, native} {
		s, ct, body := statsCall(t, "GET", u, map[string]string{"Authorization": "Bearer " + res.Token}, "")
		if s != 403 || !strings.HasPrefix(ct, "application/json") || !strings.Contains(body, `"code":"forbidden"`) {
			t.Fatalf("%s as a user: HTTP %d %q %s", u, s, ct, body)
		}
	}
	// Only GET is served there; nothing else reaches the web client either.
	if s, ct, _ := statsCall(t, "POST", stats, map[string]string{"X-Api-Key": fullKey}, "{}"); s == 200 || strings.Contains(ct, "text/html") {
		t.Fatalf("POST /api/stats: HTTP %d %q", s, ct)
	}
}

func mustJSON(v any) string { raw, _ := json.Marshal(v); return string(raw) }

// A read-only key can read everywhere a key is accepted and change nothing.
func TestReadOnlyKeyIntegration(t *testing.T) {
	srv := newAuthTestServer(t)
	adminC := newClient(t, srv)
	setupAdmin(t, adminC)
	fullKey := createKey(t, adminC, "Scripts", false)
	roKey := createKey(t, adminC, "Dashboard", true)

	fake := &fakeArr{}
	radarr := httptest.NewServer(fake.handler("http://127.0.0.1:1"))
	defer radarr.Close()
	if s := adminC.do("PUT", "/settings/requests", map[string]any{"usersCanRequest": true,
		"radarrUrl": radarr.URL + "/radarr/", "radarrApiKey": "radarr-key"}, nil); s != 200 {
		t.Fatalf("requests settings: HTTP %d", s)
	}

	bearer := func(k string) map[string]string { return map[string]string{"Authorization": "Bearer " + k} }
	xkey := func(k string) map[string]string { return map[string]string{"X-Api-Key": k} }
	emby := func(k string) map[string]string { return map[string]string{"X-Emby-Token": k} }
	mb := func(k string) map[string]string {
		return map[string]string{"Authorization": `MediaBrowser Client="t", Device="d", DeviceId="1", Version="1", Token="` + k + `"`}
	}
	api, arr := srv.URL+"/api/v1", srv.URL+"/api/v1/arr/radarr/proxy/api/v3/movie"

	// Reads work, on every surface.
	for name, c := range map[string]struct {
		url string
		hdr map[string]string
	}{
		"native list, bearer":    {api + "/users", bearer(roKey)},
		"native list, X-Api-Key": {api + "/libraries", xkey(roKey)},
		"native me":              {api + "/auth/me", xkey(roKey)},
		"arr proxy, bearer":      {arr, bearer(roKey)},
		"arr proxy, X-Api-Key":   {arr, xkey(roKey)},
		"compat, token header":   {srv.URL + "/Users", emby(roKey)},
		"compat, MediaBrowser":   {srv.URL + "/compat/Users", mb(roKey)},
		"compat, query":          {srv.URL + "/Users?api_key=" + roKey, nil},
		"compat, ApiKey query":   {srv.URL + "/Auth/Keys?ApiKey=" + roKey, nil},
	} {
		if s, _, body := statsCall(t, "GET", c.url, c.hdr, ""); s != 200 {
			t.Errorf("read %s: HTTP %d %s", name, s, body)
		}
	}
	if s, _, _ := statsCall(t, "HEAD", arr, bearer(roKey), ""); s == 403 {
		t.Errorf("HEAD with a read-only key was refused")
	}

	// Writes are refused with 403 read_only_key, however the key is sent.
	writes := []struct {
		name, method, url string
		hdr               func(string) map[string]string
		body              string
		ok                int // what a normal key gets
	}{
		{"native POST, bearer", "POST", api + "/users", bearer, `{"username":"w1","password":"w1-pass-123"}`, 201},
		{"native POST, X-Api-Key", "POST", api + "/users", xkey, `{"username":"w2","password":"w2-pass-123"}`, 201},
		{"native PUT", "PUT", api + "/settings/server", xkey, `{"serverName":"Changed"}`, 200},
		{"native PATCH", "PATCH", api + "/sessions/00000000-0000-7000-8000-000000000000", bearer, `{"deviceName":"x"}`, 404},
		{"native DELETE", "DELETE", api + "/apikeys/00000000-0000-7000-8000-000000000000", bearer, "", 404},
		{"create a key", "POST", api + "/apikeys", xkey, `{"name":"escalate"}`, 201},
		{"arr proxy POST, bearer", "POST", arr, bearer, `{"title":"x"}`, 201},
		{"arr proxy POST, X-Api-Key", "POST", arr, xkey, `{"title":"x"}`, 201},
		{"arr proxy DELETE", "DELETE", arr, bearer, "", 200},
		{"compat POST /Auth/Keys, header", "POST", srv.URL + "/Auth/Keys?App=x", emby, "", 204},
		{"compat POST /Auth/Keys, query", "POST", srv.URL + "/Auth/Keys?App=x&api_key=", nil, "", 204},
		{"compat POST /Library/Refresh", "POST", srv.URL + "/Library/Refresh", mb, "", 204},
		{"compat under /compat", "POST", srv.URL + "/compat/Library/Refresh", bearer, "", 204},
	}
	for _, w := range writes {
		for _, key := range []string{roKey, fullKey} {
			url, hdr := w.url, map[string]string(nil)
			if w.hdr != nil {
				hdr = w.hdr(key)
			} else {
				url += key
			}
			s, ct, body := statsCall(t, w.method, url, hdr, w.body)
			if key == roKey {
				if s != 403 || !strings.HasPrefix(ct, "application/json") || !strings.Contains(body, `"code":"read_only_key"`) ||
					!strings.Contains(body, `"message":"this API key is read-only"`) {
					t.Errorf("%s with a read-only key: HTTP %d %q %s", w.name, s, ct, body)
				}
			} else if s != w.ok {
				t.Errorf("%s with a normal key: HTTP %d, want %d (%s)", w.name, s, w.ok, body)
			}
		}
	}

	// Nothing the read-only key tried went through.
	var users []struct {
		Username string `json:"username"`
	}
	adminC.do("GET", "/users", nil, &users)
	for _, u := range users {
		if u.Username != "Admin" && u.Username != "w1" && u.Username != "w2" {
			t.Errorf("unexpected user %q", u.Username)
		}
	}
	var n int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM api_keys WHERE read_only`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("read-only keys: %d %v", n, err)
	}

	// Sockets carry commands, so a read-only key cannot open one; a normal key can.
	ws := "ws" + strings.TrimPrefix(srv.URL, "http")
	for name, u := range map[string]string{"compat socket": ws + "/socket?deviceId=tv-1&api_key=", "watch-together socket": ws + "/api/v1/syncplay/socket"} {
		for _, key := range []string{roKey, fullKey} {
			target, opts := u, &websocket.DialOptions{}
			if strings.HasSuffix(u, "api_key=") {
				target += key
			} else {
				opts.HTTPHeader = http.Header{"Authorization": {"Bearer " + key}}
			}
			conn, resp, err := websocket.Dial(context.Background(), target, opts)
			if conn != nil {
				_ = conn.CloseNow()
			}
			if key == roKey && (err == nil || resp == nil || resp.StatusCode != 403) {
				t.Errorf("%s with a read-only key: err %v resp %v", name, err, resp)
			}
			if key == fullKey && err != nil {
				t.Errorf("%s with a normal key: %v", name, err)
			}
		}
	}

	// Outside an HTTP request (no method known) a read-only key is refused too.
	if _, err := testState.Auth().Authenticate(context.Background(), roKey); err == nil {
		t.Error("read-only key authenticated without a request")
	}
	if _, err := testState.Auth().Authenticate(context.Background(), fullKey); err != nil {
		t.Errorf("normal key: %v", err)
	}
}
