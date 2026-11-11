package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/downloads"
	"github.com/streamero-app/streamero/server/internal/library"
)

type downloadJob struct {
	ID        string  `json:"id"`
	ItemID    string  `json:"itemId"`
	Quality   string  `json:"quality"`
	State     string  `json:"state"`
	Progress  float64 `json:"progress"`
	SizeBytes int64   `json:"sizeBytes"`
	URL       string  `json:"url"`
	FileName  string  `json:"fileName"`
	Container string  `json:"container"`
	Subtitles []struct {
		Index  int    `json:"index"`
		URL    string `json:"url"`
		VttURL string `json:"vttUrl"`
	} `json:"subtitles"`
	Item struct {
		Title string `json:"title"`
		Kind  string `json:"kind"`
	} `json:"item"`
	Series *struct {
		Title string `json:"title"`
	} `json:"series"`
}

// TestDownloadsIntegration covers offline downloads end to end against real
// Postgres (and ffmpeg when installed): permissions and parental limits, the
// transcode job lifecycle including restart recovery and admin removal,
// season / next-N selection, Range serving through signed URLs, offline
// progress sync (newest wins), revocation and cache cleanup.
func TestDownloadsIntegration(t *testing.T) {
	srv := newAuthTestServer(t)
	adminC := newClient(t, srv)
	setupAdmin(t, adminC)
	ffmpeg, err1 := exec.LookPath("ffmpeg")
	ffprobe, err2 := exec.LookPath("ffprobe")
	if err1 != nil || err2 != nil {
		t.Skip("ffmpeg/ffprobe not installed")
	}
	testState.Stream().FFmpeg = ffmpeg
	ctx := context.Background()
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))

	// A movie (with a sidecar subtitle) and a series with one season of four episodes.
	clip := func(path string, size string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command(ffmpeg, "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc2=s="+size+":r=10:d=3",
			"-f", "lavfi", "-i", "sine=frequency=440:duration=3", "-c:v", "mpeg4", "-c:a", "mp3", path).CombinedOutput(); err != nil {
			t.Fatalf("make %s: %v %s", path, err, out)
		}
	}
	clip(mediaRoot+"/dl/movies/Night Train (2020)/Night Train (2020).mkv", "320x240")
	if err := os.WriteFile(mediaRoot+"/dl/movies/Night Train (2020)/Night Train (2020).en.srt", []byte("1\n00:00:01,000 --> 00:00:02,000\nAll aboard\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 4; i++ {
		clip(mediaRoot+"/dl/shows/Harbour/Season 01/Harbour - S01E0"+string(rune('0'+i))+".mkv", "160x120")
	}
	scan := func(name, kind, dir string) string {
		t.Helper()
		var lib struct {
			ID string `json:"id"`
		}
		if s := adminC.do("POST", "/libraries", map[string]any{"name": name, "kind": kind, "paths": []string{dir}}, &lib); s != 201 {
			t.Fatalf("library: HTTP %d", s)
		}
		id := uuid.MustParse(lib.ID)
		t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM libraries WHERE id = $1`, id) })
		sc := &library.Scanner{Pool: testPool, Roots: library.Roots{mediaRoot}, Probe: library.FFprobe(ffprobe), Log: quiet}
		if _, err := sc.Scan(ctx, id); err != nil {
			t.Fatal(err)
		}
		return lib.ID
	}
	scan("Films", "movies", mediaRoot+"/dl/movies")
	scan("Shows", "shows", mediaRoot+"/dl/shows")
	itemID := func(kind, title string) string {
		t.Helper()
		var id string
		if err := testPool.QueryRow(ctx, `SELECT id::text FROM items WHERE kind = $1 AND title = $2`, kind, title).Scan(&id); err != nil {
			t.Fatalf("%s %q: %v", kind, title, err)
		}
		return id
	}
	movie := itemID("movie", "Night Train")
	series := itemID("series", "Harbour")
	var season string
	if err := testPool.QueryRow(ctx, `SELECT id::text FROM items WHERE kind = 'season' AND parent_id = $1`, series).Scan(&season); err != nil {
		t.Fatal(err)
	}
	var episodes []string
	rows, _ := testPool.Query(ctx, `SELECT id::text FROM items WHERE kind = 'episode' AND parent_id = $1 ORDER BY episode_number`, season)
	for rows.Next() {
		var id string
		_ = rows.Scan(&id)
		episodes = append(episodes, id)
	}
	rows.Close()
	if len(episodes) != 4 {
		t.Fatalf("episodes: %v", episodes)
	}

	// A user without the download permission.
	var leo struct {
		ID    string         `json:"id"`
		Perms map[string]any `json:"permissions"`
	}
	if s := adminC.do("POST", "/users", map[string]any{"username": "leo", "password": "leo-pass-1"}, &leo); s != 201 {
		t.Fatalf("user: HTTP %d", s)
	}
	setPerm := func(key string, v bool) {
		leo.Perms[key] = v
		if s := adminC.do("PATCH", "/users/"+leo.ID, map[string]any{"permissions": leo.Perms}, &leo); s != 200 {
			t.Fatalf("perms: HTTP %d", s)
		}
	}
	setPerm("download", false)
	leoC := newClient(t, srv)
	var res authResult
	if s := leoC.do("POST", "/auth/login", map[string]any{"username": "leo", "password": "leo-pass-1", "device": map[string]any{"name": "phone"}}, &res); s != 200 {
		t.Fatalf("login: HTTP %d", s)
	}
	leoC.token = res.Token
	var e apiError
	if s := leoC.do("POST", "/downloads", map[string]any{"itemId": movie, "quality": "original"}, &e); s != 403 || e.Code != "forbidden" {
		t.Fatalf("download without permission: HTTP %d %+v", s, e)
	}
	if s := leoC.do("POST", "/items/"+movie+"/original", map[string]any{}, &e); s != 403 {
		t.Fatalf("original link without permission: HTTP %d", s)
	}
	setPerm("download", true)
	setPerm("videoTranscoding", false)
	if s := leoC.do("POST", "/downloads", map[string]any{"itemId": movie, "quality": "720p"}, &e); s != 403 {
		t.Fatalf("transcoded download without transcoding permission: HTTP %d", s)
	}
	setPerm("videoTranscoding", true)

	// Original: ready at once, served with byte ranges through the signed URL.
	var jobs []downloadJob
	postDL := func(body map[string]any, _ any) int {
		jobs = nil
		return leoC.do("POST", "/downloads", body, &jobs)
	}
	if s := postDL(map[string]any{"itemId": movie, "quality": "original"}, nil); s != 201 || len(jobs) != 1 {
		t.Fatalf("original: HTTP %d %+v", s, jobs)
	}
	orig := jobs[0]
	if orig.State != "ready" || orig.URL == "" || orig.Container != "mkv" || orig.Item.Title != "Night Train" || len(orig.Subtitles) != 1 {
		t.Fatalf("original job: %+v", orig)
	}
	get := func(u string, rng string) (int, []byte, http.Header) {
		t.Helper()
		req, _ := http.NewRequest("GET", srv.URL+u, nil)
		if rng != "" {
			req.Header.Set("Range", rng)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, b, resp.Header
	}
	if s, b, h := get(orig.URL, "bytes=0-99"); s != 206 || len(b) != 100 || !strings.HasPrefix(h.Get("Content-Range"), "bytes 0-99/") ||
		!strings.Contains(h.Get("Content-Disposition"), "Night Train (2020).mkv") {
		t.Fatalf("range: %d %d %v", s, len(b), h)
	}
	if s, b, _ := get(orig.Subtitles[0].URL, ""); s != 200 || !strings.Contains(string(b), "All aboard") || !strings.Contains(string(b), "-->") {
		t.Fatalf("srt sidecar: %d %q", s, b)
	}
	// Tampered or foreign signatures are refused.
	if s, _, _ := get(strings.Replace(orig.URL, orig.ID, uuid.NewString(), 1), ""); s != 403 {
		t.Fatalf("other id with this signature: %d", s)
	}
	// Idempotent: asking again returns the same job.
	if postDL(map[string]any{"itemId": movie, "quality": "original"}, nil); jobs[0].ID != orig.ID {
		t.Fatalf("not idempotent: %+v", jobs)
	}

	// Web: original link with Content-Disposition.
	var link struct {
		URL       string `json:"url"`
		FileName  string `json:"fileName"`
		SizeBytes int64  `json:"sizeBytes"`
	}
	if s := leoC.do("POST", "/items/"+movie+"/original", map[string]any{}, &link); s != 200 || link.FileName != "Night Train (2020).mkv" {
		t.Fatalf("original link: %d %+v", s, link)
	}
	if s, b, h := get(link.URL, ""); s != 200 || int64(len(b)) != link.SizeBytes || !strings.HasPrefix(h.Get("Content-Disposition"), "attachment;") {
		t.Fatalf("original link fetch: %d %d %v", s, len(b), h)
	}

	// Transcode to 720p: queued, produced by the worker's transcoder, then ready.
	testEnqueued = nil
	if s := postDL(map[string]any{"itemId": movie, "quality": "720p"}, nil); s != 201 || jobs[0].State != "queued" || jobs[0].URL != "" {
		t.Fatalf("720p: %d %+v", s, jobs)
	}
	tc := jobs[0]
	if len(testEnqueued) != 1 {
		t.Fatalf("enqueued: %v", testEnqueued)
	}
	tr := &downloads.Transcoder{Pool: testPool, Roots: library.Roots{mediaRoot}, FFmpeg: ffmpeg, Dir: testDownloads.Dir,
		Settings: testState.Settings(), Log: quiet, Tick: 50 * time.Millisecond,
		Enqueue: func(_ context.Context, id uuid.UUID) error { testEnqueued = append(testEnqueued, id); return nil }}

	// Concurrency cap: with the only slot taken, the job waits.
	if _, err := testPool.Exec(ctx, `UPDATE download_transcodes SET state = 'transcoding', heartbeat_at = now() - interval '1 hour' WHERE id = $1`, testEnqueued[0]); err != nil {
		t.Fatal(err)
	}
	other := uuid.New()
	var fileID uuid.UUID
	_ = testPool.QueryRow(ctx, `SELECT file_id FROM download_transcodes WHERE id = $1`, testEnqueued[0]).Scan(&fileID)
	if _, err := testPool.Exec(ctx, `INSERT INTO download_transcodes (id, file_id, quality, audio_index) VALUES ($1, $2, 'source', 0)`, other, fileID); err != nil {
		t.Fatal(err)
	}
	if err := tr.Run(ctx, other); !errors.Is(err, downloads.ErrBusy) {
		t.Fatalf("over the cap: %v", err)
	}
	// The worker died during the first transcode: recovery requeues it (jobs survive restarts).
	testEnqueued = nil
	if err := tr.Recover(ctx, downloads.StaleAfter, false); err != nil || len(testEnqueued) != 1 {
		t.Fatalf("recover: %v %v", err, testEnqueued)
	}
	var state string
	_ = testPool.QueryRow(ctx, `SELECT state FROM download_transcodes WHERE id = $1`, testEnqueued[0]).Scan(&state)
	if state != "queued" {
		t.Fatalf("after recovery: %s", state)
	}
	// Admin raises the cap to two; both run.
	if s := adminC.do("PUT", "/settings/downloads", map[string]any{"maxTranscodes": 2, "cacheLimitGb": 5, "keepDays": 3}, nil); s != 200 {
		t.Fatalf("settings: HTTP %d", s)
	}
	if err := tr.Run(ctx, testEnqueued[0]); err != nil {
		t.Fatalf("transcode: %v", err)
	}
	tc = downloadJob{}
	if s := leoC.do("GET", "/downloads/"+jobs[0].ID, nil, &tc); s != 200 || tc.State != "ready" || tc.Progress != 1 || tc.URL == "" || tc.Container != "mp4" || tc.SizeBytes == 0 {
		t.Fatalf("ready: %d %+v", s, tc)
	}
	s, b, h := get(tc.URL, "bytes=0-31")
	if s != 206 || h.Get("Content-Type") != "video/mp4" || !strings.Contains(string(b), "ftyp") {
		t.Fatalf("mp4 range: %d %q %v", s, b, h)
	}
	_, whole, _ := get(tc.URL, "")
	tmp := filepath.Join(t.TempDir(), "out.mp4")
	_ = os.WriteFile(tmp, whole, 0o600)
	if out, err := exec.Command(ffprobe, "-v", "error", "-show_entries", "stream=codec_name,height", "-of", "csv=p=0", tmp).CombinedOutput(); err != nil ||
		!strings.Contains(string(out), "h264") || !strings.Contains(string(out), "aac") {
		t.Fatalf("output streams: %v %s", err, out)
	}

	// Admin view: the ready transcode with its user; removing one stops and deletes it.
	var adm struct {
		Running    int   `json:"running"`
		CacheBytes int64 `json:"cacheBytes"`
		Settings   struct {
			MaxTranscodes int `json:"maxTranscodes"`
		} `json:"settings"`
		Transcodes []struct {
			ID    string   `json:"id"`
			State string   `json:"state"`
			Users []string `json:"users"`
			Title string   `json:"title"`
		} `json:"transcodes"`
	}
	if s := leoC.do("GET", "/admin/downloads", nil, &adm); s != 403 {
		t.Fatalf("admin view as user: %d", s)
	}
	if s := adminC.do("GET", "/admin/downloads", nil, &adm); s != 200 || adm.Settings.MaxTranscodes != 2 || adm.CacheBytes != tc.SizeBytes || len(adm.Transcodes) != 2 {
		t.Fatalf("admin view: %d %+v", s, adm)
	}
	for _, x := range adm.Transcodes {
		if x.State == "ready" && (len(x.Users) != 1 || x.Users[0] != "leo" || x.Title != "Night Train") {
			t.Fatalf("admin transcode: %+v", x)
		}
	}
	if s := adminC.do("DELETE", "/admin/downloads/"+other.String(), nil, nil); s != 204 {
		t.Fatalf("admin delete: %d", s)
	}
	if err := tr.Run(ctx, other); err != nil {
		t.Fatalf("removed transcode ran: %v", err)
	}

	// Season and next N episodes.
	if s := postDL(map[string]any{"itemId": season, "quality": "original"}, nil); s != 201 || len(jobs) != 4 ||
		jobs[0].ItemID != episodes[0] || jobs[0].Series == nil || jobs[0].Series.Title != "Harbour" {
		t.Fatalf("season: %d %+v", s, jobs)
	}
	adminC.do("PUT", "/items/"+episodes[0]+"/progress", map[string]any{"positionMs": 0, "played": true}, nil) // admin's own state
	leoC.do("PUT", "/items/"+episodes[0]+"/user-data", map[string]any{"played": true}, nil)
	if s := postDL(map[string]any{"itemId": series, "quality": "original", "nextEpisodes": 2,
		"skipItemIds": []string{episodes[1]}}, nil); s != 201 || len(jobs) != 1 || jobs[0].ItemID != episodes[2] {
		t.Fatalf("next 2 with one on the device: %d %+v", s, jobs)
	}

	// Offline progress: newest wins.
	type syncRes struct {
		Revoked []string `json:"revoked"`
		Applied int      `json:"applied"`
		Items   []struct {
			ItemID     string    `json:"itemId"`
			PositionMs int64     `json:"positionMs"`
			Played     bool      `json:"played"`
			UpdatedAt  time.Time `json:"updatedAt"`
		} `json:"items"`
	}
	adminC.do("PUT", "/settings/resume", map[string]any{"minResumePercent": 0, "maxResumePercent": 90, "minResumeDurationSeconds": 0}, nil)
	leoC.do("PUT", "/items/"+movie+"/progress", map[string]any{"positionMs": 1000}, nil) // online, now
	var sr syncRes
	old := time.Now().Add(-time.Hour)
	if s := leoC.do("POST", "/downloads/sync", map[string]any{"itemIds": []string{movie, episodes[3]},
		"progress": []map[string]any{{"itemId": movie, "positionMs": 2000, "played": false, "updatedAt": old},
			{"itemId": episodes[3], "positionMs": 1500, "played": false, "updatedAt": old}}}, &sr); s != 200 {
		t.Fatalf("sync: %d", s)
	}
	pos := map[string]int64{}
	for _, it := range sr.Items {
		pos[it.ItemID] = it.PositionMs
	}
	if sr.Applied != 1 || pos[movie] != 1000 || pos[episodes[3]] != 1500 || len(sr.Revoked) != 0 {
		t.Fatalf("older offline report: %+v", sr)
	}
	if leoC.do("POST", "/downloads/sync", map[string]any{"itemIds": []string{movie},
		"progress": []map[string]any{{"itemId": movie, "positionMs": 2500, "played": false, "updatedAt": time.Now().Add(time.Hour)}}}, &sr); sr.Applied != 1 ||
		sr.Items[0].PositionMs != 2500 || sr.Items[0].UpdatedAt.After(time.Now().Add(time.Minute)) {
		t.Fatalf("newer offline report: %+v", sr)
	}

	// Parental limit: a kids profile cannot download an R-rated film, and loses it on sync.
	if _, err := testPool.Exec(ctx, `UPDATE items SET official_rating = 'R' WHERE id = $1`, movie); err != nil {
		t.Fatal(err)
	}
	var kid struct {
		ID string `json:"id"`
	}
	leoC.do("POST", "/profiles", map[string]any{"name": "Mia", "color": "#3DD68C", "isKid": true, "maxRating": 12}, &kid)
	if s := leoC.do("POST", "/profiles/"+kid.ID+"/select", nil, nil); s != 204 {
		t.Fatalf("select kid: %d", s)
	}
	if s := leoC.do("POST", "/downloads", map[string]any{"itemId": movie, "quality": "original"}, &e); s != 403 || e.Code != "parental_limit" {
		t.Fatalf("kid download: %d %+v", s, e)
	}
	if leoC.do("POST", "/downloads/sync", map[string]any{"itemIds": []string{movie, episodes[2]}, "progress": []any{}}, &sr); len(sr.Revoked) != 1 || sr.Revoked[0] != movie {
		t.Fatalf("kid sync: %+v", sr)
	}
	leoC.do("POST", "/profiles/main/select", map[string]any{"password": "leo-pass-1"}, nil)

	// Losing the download permission revokes everything on the next sync, and the links stop working.
	setPerm("download", false)
	if leoC.do("POST", "/downloads/sync", map[string]any{"itemIds": []string{movie, episodes[2]}, "progress": []any{}}, &sr); len(sr.Revoked) != 2 {
		t.Fatalf("revoked: %+v", sr)
	}
	if s, _, _ := get(tc.URL, ""); s != 404 {
		t.Fatalf("revoked job still served: %d", s)
	}

	// Cleanup: the transcode has no job any more; after a day unused it goes, with its file.
	path := downloads.TranscodePath(testDownloads.Dir, uuid.MustParse(adm.Transcodes[0].ID))
	for _, x := range adm.Transcodes {
		if x.State == "ready" {
			path = downloads.TranscodePath(testDownloads.Dir, uuid.MustParse(x.ID))
		}
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("cache file: %v", err)
	}
	stray := filepath.Join(testDownloads.Dir, uuid.NewString()+".mp4")
	_ = os.WriteFile(stray, []byte("x"), 0o600)
	_ = os.Chtimes(stray, time.Now().Add(-2*time.Hour), time.Now().Add(-2*time.Hour))
	if err := tr.Maintain(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("recently used transcode removed: %v", err)
	}
	if _, err := os.Stat(stray); !os.IsNotExist(err) {
		t.Fatalf("stray file kept: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE download_transcodes SET last_access_at = now() - interval '2 days'`); err != nil {
		t.Fatal(err)
	}
	if err := tr.Maintain(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = testPool.QueryRow(ctx, `SELECT count(*) FROM download_transcodes`).Scan(&n)
	if _, err := os.Stat(path); !os.IsNotExist(err) || n != 0 {
		t.Fatalf("unused transcode kept: %v %d", err, n)
	}

	// Size limit: the least recently fetched ready file goes first.
	setPerm("download", true)
	postDL(map[string]any{"itemId": episodes[0], "quality": "source"}, nil)
	postDL(map[string]any{"itemId": episodes[1], "quality": "source"}, nil)
	ids := []uuid.UUID{}
	rows, _ = testPool.Query(ctx, `SELECT id FROM download_transcodes ORDER BY created_at`)
	for rows.Next() {
		var id uuid.UUID
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if err := tr.Run(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	_, _ = testPool.Exec(ctx, `UPDATE download_transcodes SET size_bytes = 3 * 1024 * 1024 * 1024::bigint, last_access_at = now() - interval '1 hour' WHERE id = $1`, ids[0])
	_, _ = testPool.Exec(ctx, `UPDATE download_transcodes SET size_bytes = 3 * 1024 * 1024 * 1024::bigint WHERE id = $1`, ids[1])
	if err := tr.Evict(ctx); err != nil {
		t.Fatal(err)
	}
	var left []uuid.UUID
	rows, _ = testPool.Query(ctx, `SELECT id FROM download_transcodes`)
	for rows.Next() {
		var id uuid.UUID
		_ = rows.Scan(&id)
		left = append(left, id)
	}
	rows.Close()
	if len(left) != 1 || left[0] != ids[1] {
		t.Fatalf("eviction kept %v (want %v)", left, ids[1])
	}
}
