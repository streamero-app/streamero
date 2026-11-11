package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/livetv"
	"github.com/streamero-app/streamero/server/internal/metadata"
	"github.com/streamero-app/streamero/server/internal/stream"
)

// fakeLiveSource is an IPTV provider: an M3U playlist, an XMLTV guide, a logo
// and channels that stream a generated MPEG-TS (ffmpeg testsrc) in real time.
type fakeLiveSource struct {
	srv   *httptest.Server
	mu    sync.Mutex
	guide string
	open  map[string]*atomic.Int32 // current connections per channel
	total map[string]*atomic.Int32 // connections ever
}

func (f *fakeLiveSource) counter(m map[string]*atomic.Int32, name string) *atomic.Int32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	if m[name] == nil {
		m[name] = &atomic.Int32{}
	}
	return m[name]
}

type flushWriter struct{ w http.ResponseWriter }

func (f flushWriter) Write(p []byte) (int, error) {
	n, err := f.w.Write(p)
	if fl, ok := f.w.(http.Flusher); ok {
		fl.Flush()
	}
	return n, err
}

// serveTS streams a live test pattern with a tone: H.264 (keyframe every second) and AAC.
func serveTS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "video/mp2t")
	cmd := exec.CommandContext(r.Context(), "ffmpeg", "-nostdin", "-loglevel", "error", "-re",
		"-f", "lavfi", "-i", "testsrc=size=320x240:rate=25", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000",
		"-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency", "-g", "25", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "64k", "-f", "mpegts", "pipe:1")
	cmd.Stdout = flushWriter{w}
	_ = cmd.Run()
}

func newFakeLiveSource(t *testing.T) *fakeLiveSource {
	f := &fakeLiveSource{open: map[string]*atomic.Int32{}, total: map[string]*atomic.Int32{}}
	var logo bytes.Buffer
	_ = png.Encode(&logo, image.NewRGBA(image.Rect(0, 0, 4, 4)))
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/playlist.m3u":
			_, _ = fmt.Fprintf(w, "#EXTM3U\n#EXTINF:-1 tvg-id=\"one.test\" tvg-logo=\"%s/logo.png\" group-title=\"News\",Channel One\n%s/live/one.ts\n"+
				"#EXTINF:-1 group-title=\"Movies\",Channel Two HD\n%s/live/two.ts\n", f.srv.URL, f.srv.URL, f.srv.URL)
		case r.URL.Path == "/guide.xml":
			f.mu.Lock()
			g := f.guide
			f.mu.Unlock()
			_, _ = io.WriteString(w, g)
		case r.URL.Path == "/logo.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(logo.Bytes())
		case strings.HasPrefix(r.URL.Path, "/live/"):
			name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/live/"), ".ts")
			f.counter(f.total, name).Add(1)
			c := f.counter(f.open, name)
			c.Add(1)
			defer c.Add(-1)
			serveTS(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// stopLive ends the DVR loop and every live session before the fake sources close.
func stopLive(cancel context.CancelFunc, hls *stream.Manager) {
	cancel()
	for i := 0; i < 100 && hls.Active() > 0; i++ {
		time.Sleep(100 * time.Millisecond)
	}
}

func xmltvTime(t time.Time) string { return t.UTC().Format("20060102150405") + " +0000" }

// liveGuide builds the XMLTV guide around now. clipStart is when the short
// programme that gets recorded begins.
func liveGuide(now, clipStart time.Time, newsDesc string) string {
	day := now.UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
	prog := func(ch string, from, to time.Time, inner string) string {
		return fmt.Sprintf(`<programme start="%s" stop="%s" channel="%s">%s</programme>`+"\n", xmltvTime(from), xmltvTime(to), ch, inner)
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><tv>` + "\n")
	b.WriteString(`<channel id="one.test"><display-name>One</display-name></channel>` + "\n")
	b.WriteString(`<channel id="two.test"><display-name>Channel Two</display-name></channel>` + "\n")
	b.WriteString(prog("one.test", now.Add(-30*time.Minute), clipStart, `<title>Live Show</title><category>News</category><icon src="`+"%LOGO%"+`"/>`))
	b.WriteString(prog("one.test", clipStart, clipStart.Add(8*time.Second), `<title>Short Clip</title><desc>Recorded in the test</desc>`))
	b.WriteString(prog("one.test", clipStart.Add(8*time.Second), now.Add(2*time.Hour), `<title>Afterwards</title>`))
	b.WriteString(prog("two.test", now.Add(-time.Hour), now.Add(2*time.Hour), `<title>Big Film</title><category>Movie</category>`))
	// Tomorrow: a series with a new episode, its repeat and a second episode on both channels.
	news := `<title>Evening News</title><desc>` + newsDesc + `</desc><category>News</category>`
	b.WriteString(prog("one.test", day.Add(19*time.Hour), day.Add(20*time.Hour), news+`<episode-num system="onscreen">S01E01</episode-num><new/>`))
	b.WriteString(prog("one.test", day.Add(23*time.Hour), day.Add(24*time.Hour), news+`<episode-num system="onscreen">S01E01</episode-num><previously-shown/>`))
	b.WriteString(prog("one.test", day.Add(43*time.Hour), day.Add(44*time.Hour), news+`<episode-num system="onscreen">S01E02</episode-num>`))
	b.WriteString(prog("two.test", day.Add(43*time.Hour), day.Add(44*time.Hour), news+`<episode-num system="onscreen">S01E02</episode-num>`))
	b.WriteString(prog("two.test", day.Add(19*time.Hour+30*time.Minute), day.Add(21*time.Hour), `<title>Late Film</title><category>Movie</category>`))
	b.WriteString(`</tv>`)
	return b.String()
}

type liveChannelJSON struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Number   string `json:"number"`
	Favorite bool   `json:"favorite"`
	Category string `json:"category"`
	LogoURL  string `json:"logoUrl"`
	Now      *struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"now"`
}

type liveRecordingJSON struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Error     string `json:"error"`
	SizeBytes int64  `json:"sizeBytes"`
	ItemID    string `json:"itemId"`
	Priority  int    `json:"priority"`
}

func getURL(t *testing.T, u string) (int, []byte) {
	t.Helper()
	resp, err := http.Get(u) //nolint:gosec,noctx // test server URL
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

// segmentsOf lists the media segment URIs of an HLS playlist.
func segmentsOf(playlist string) []string {
	var out []string
	for _, l := range strings.Split(playlist, "\n") {
		if l != "" && !strings.HasPrefix(l, "#") {
			out = append(out, l)
		}
	}
	return out
}

func TestLiveTVIntegration(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	srv := newAuthTestServer(t)
	adminC := newClient(t, srv)
	setupAdmin(t, adminC)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if _, err := testPool.Exec(ctx, `DELETE FROM livetv_recordings; DELETE FROM livetv_series_rules; DELETE FROM livetv_favorites;
		DELETE FROM livetv_tuners; DELETE FROM livetv_guides; DELETE FROM settings WHERE key = 'livetv'`); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// Live sessions with 1-second segments (the source has a keyframe every second).
	hls := &stream.Manager{Dir: t.TempDir(), FFmpeg: "ffmpeg", Log: log,
		Encoding: func(context.Context) stream.Encoding { return stream.Encoding{SegmentSeconds: 1} }}
	go hls.Reap(ctx)
	recDir := filepath.Join(mediaRoot, "recordings")
	if err := os.MkdirAll(recDir, 0o755); err != nil {
		t.Fatal(err)
	}
	signer := stream.Signer{Key: []byte("test-key")}
	svc := &livetv.Service{Pool: testPool, Settings: testState.Settings, Signer: signer, HLS: hls, FFmpeg: "ffmpeg", FFprobe: "ffprobe",
		FileRoots: library.Roots{mediaRoot}, RecordingsDir: recDir, Library: testState.Library(),
		HTTP: livetv.NewHTTPClient(time.Minute, true), ImageDir: t.TempDir(), Log: log}
	testState.SetStream(&stream.Server{Pool: testPool, Roots: library.Roots{mediaRoot}, Signer: signer, HLS: hls, FFmpeg: "ffmpeg",
		Images: svc.ImageHandler()})
	testState.SetLiveTV(svc)
	go svc.Run(ctx)

	src := newFakeLiveSource(t)
	t.Cleanup(func() { stopLive(cancel, hls) })
	now := time.Now().UTC().Truncate(time.Second)
	clipStart := now.Add(45 * time.Second)
	src.guide = strings.ReplaceAll(liveGuide(now, clipStart, "Daily news."), "%LOGO%", src.srv.URL+"/logo.png")

	// --- tuner: test, add (one stream at a time) ---
	var tr struct {
		Ok           bool   `json:"ok"`
		Message      string `json:"message"`
		ChannelCount int    `json:"channelCount"`
	}
	if s := adminC.do("POST", "/livetv/tuners/test", map[string]any{"kind": "m3u", "name": "IPTV", "url": src.srv.URL + "/playlist.m3u"}, &tr); s != 200 ||
		!tr.Ok || tr.ChannelCount != 2 {
		t.Fatalf("tuner test: %d %+v", s, tr)
	}
	if s := adminC.do("POST", "/livetv/tuners/test", map[string]any{"kind": "m3u", "name": "IPTV", "filePath": "/etc/passwd"}, &tr); s != 200 || tr.Ok {
		t.Fatalf("file outside the allowed folders: %+v", tr)
	}
	var tuner struct {
		ID           string `json:"id"`
		ChannelCount int    `json:"channelCount"`
		TunerCount   int    `json:"tunerCount"`
	}
	if s := adminC.do("POST", "/livetv/tuners", map[string]any{"kind": "m3u", "name": "IPTV", "url": src.srv.URL + "/playlist.m3u", "tunerCount": 1}, &tuner); s != 201 ||
		tuner.ChannelCount != 2 || tuner.TunerCount != 1 {
		t.Fatalf("add tuner: %d %+v", s, tuner)
	}

	// --- guide: add, load, skip unchanged, apply a change ---
	var guide struct {
		ID string `json:"id"`
	}
	if s := adminC.do("POST", "/livetv/guides", map[string]any{"name": "EPG", "url": src.srv.URL + "/guide.xml", "refreshHours": 6}, &guide); s != 201 {
		t.Fatalf("add guide: %d", s)
	}
	gid := uuid.MustParse(guide.ID)
	res, err := svc.RefreshGuide(ctx, gid, true)
	if err != nil {
		t.Fatal(err)
	}
	total := res.Inserted + res.Updated + res.Unchanged
	if total != 9 || res.Channels != 2 {
		t.Fatalf("first load: %+v", res)
	}
	if res, err = svc.RefreshGuide(ctx, gid, false); err != nil || !res.Skipped {
		t.Fatalf("unchanged source: %+v %v", res, err)
	}
	var progIDBefore string
	_ = testPool.QueryRow(ctx, `SELECT id::text FROM livetv_programs WHERE title = 'Evening News' ORDER BY start_at LIMIT 1`).Scan(&progIDBefore)
	src.mu.Lock()
	src.guide = strings.ReplaceAll(liveGuide(now, clipStart, "Weather too."), "%LOGO%", src.srv.URL+"/logo.png")
	src.mu.Unlock()
	if res, err = svc.RefreshGuide(ctx, gid, false); err != nil || res.Updated != 4 || res.Inserted != 0 || res.Unchanged != 5 || res.Deleted != 0 {
		t.Fatalf("changed descriptions: %+v %v", res, err)
	}
	var progIDAfter string
	_ = testPool.QueryRow(ctx, `SELECT id::text FROM livetv_programs WHERE title = 'Evening News' ORDER BY start_at LIMIT 1`).Scan(&progIDAfter)
	if progIDBefore == "" || progIDBefore != progIDAfter {
		t.Fatal("a changed programme must keep its id")
	}

	// --- mapping: by tvg-id and by name ---
	var mapping struct {
		Channels []struct {
			Channel        liveChannelJSON `json:"channel"`
			Mapping        string          `json:"mapping"`
			GuideChannelID string          `json:"guideChannelId"`
		} `json:"channels"`
		GuideChannels []struct{ ID string } `json:"guideChannels"`
	}
	if s := adminC.do("GET", "/livetv/mapping", nil, &mapping); s != 200 || len(mapping.Channels) != 2 || len(mapping.GuideChannels) != 2 {
		t.Fatalf("mapping: %d %+v", s, mapping)
	}
	byName := map[string]liveChannelJSON{}
	for _, c := range mapping.Channels {
		byName[c.Channel.Name] = c.Channel
		want := map[string]string{"Channel One": "one.test", "Channel Two HD": "two.test"}[c.Channel.Name]
		if c.Mapping != "auto" || c.GuideChannelID != want {
			t.Fatalf("auto mapping of %s: %+v", c.Channel.Name, c)
		}
	}
	one, two := byName["Channel One"], byName["Channel Two HD"]
	// Manual mapping wins and survives a guide refresh; "auto" goes back.
	var ca struct {
		Mapping        string `json:"mapping"`
		GuideChannelID string `json:"guideChannelId"`
	}
	if s := adminC.do("PATCH", "/livetv/channels/"+two.ID, map[string]any{"mapping": "manual", "guideSourceId": guide.ID, "guideChannelId": "one.test"}, &ca); s != 200 ||
		ca.GuideChannelID != "one.test" {
		t.Fatalf("manual: %d %+v", s, ca)
	}
	if s := adminC.do("PATCH", "/livetv/channels/"+two.ID, map[string]any{"mapping": "auto", "category": "sports"}, &ca); s != 200 || ca.GuideChannelID != "two.test" {
		t.Fatalf("back to auto: %d %+v", s, ca)
	}

	// --- watching: channels, now/next, guide, favourites, categories ---
	var chans []liveChannelJSON
	if s := adminC.do("GET", "/livetv/channels", nil, &chans); s != 200 || len(chans) != 2 || chans[0].Now == nil || chans[0].Now.Title != "Live Show" {
		t.Fatalf("channels: %d %+v", s, chans)
	}
	if chans[0].Category != "news" || chans[1].Category != "sports" {
		t.Fatalf("categories: %+v", chans)
	}
	if code, body := getURL(t, srv.URL+chans[0].LogoURL); code != 200 || !bytes.HasPrefix(body, []byte("\x89PNG")) {
		t.Fatalf("logo: %d", code)
	}
	if code, _ := getURL(t, srv.URL+strings.Replace(chans[0].LogoURL, "sig=", "sig=x", 1)); code != 403 {
		t.Fatalf("unsigned logo: %d", code)
	}
	if s := adminC.do("PUT", "/livetv/channels/"+two.ID+"/favorite", nil, nil); s != 204 {
		t.Fatalf("favourite: %d", s)
	}
	var favs []liveChannelJSON
	adminC.do("GET", "/livetv/channels?favorites=true", nil, &favs)
	if len(favs) != 1 || favs[0].ID != two.ID || !favs[0].Favorite {
		t.Fatalf("favourites: %+v", favs)
	}
	var onNow []liveChannelJSON
	adminC.do("GET", "/livetv/on-now?limit=5", nil, &onNow)
	if len(onNow) != 2 || onNow[0].ID != two.ID {
		t.Fatalf("on now (favourites first): %+v", onNow)
	}
	var grid struct {
		Rows []struct {
			Channel  liveChannelJSON `json:"channel"`
			Programs []struct {
				ID       string `json:"id"`
				Title    string `json:"title"`
				Category string `json:"category"`
			} `json:"programs"`
		} `json:"rows"`
	}
	tomorrow := now.UTC().Truncate(24 * time.Hour).Add(24*time.Hour + 18*time.Hour).Format(time.RFC3339)
	if s := adminC.do("GET", "/livetv/guide?hours=6&start="+tomorrow, nil, &grid); s != 200 || len(grid.Rows) != 2 {
		t.Fatalf("guide: %d %+v", s, grid)
	}
	if len(grid.Rows[0].Programs) != 2 || grid.Rows[0].Programs[0].Title != "Evening News" || len(grid.Rows[1].Programs) != 1 {
		t.Fatalf("guide window: %+v", grid.Rows)
	}
	lateFilm := grid.Rows[1].Programs[0]
	eveningNews := grid.Rows[0].Programs[0]
	adminC.do("GET", "/livetv/guide?hours=6&category=movies&start="+tomorrow, nil, &grid)
	if len(grid.Rows) != 1 || grid.Rows[0].Channel.ID != two.ID {
		t.Fatalf("movies category: %+v", grid.Rows)
	}

	// --- compat API (TV apps): channels, guide, items and playback info ---
	testState.Compat().LiveTV = svc
	tv := &compatCaller{t: t, base: srv.URL}
	var tvLogin struct {
		AccessToken string
		User        struct {
			Policy map[string]any
		}
	}
	if s := tv.call("POST", "/Users/AuthenticateByName", map[string]any{"Username": "Admin", "Pw": "admin-pass-1"}, &tvLogin); s != 200 ||
		tvLogin.User.Policy["EnableLiveTvAccess"] != true {
		t.Fatalf("compat login: %d %+v", s, tvLogin.User.Policy)
	}
	tv.token = tvLogin.AccessToken
	var info struct{ IsEnabled bool }
	if tv.call("GET", "/LiveTv/Info", nil, &info); !info.IsEnabled {
		t.Fatal("compat live tv info")
	}
	var cl struct {
		Items []struct {
			ID             string `json:"Id"`
			Name, Type     string
			ChannelNumber  string
			ImageTags      map[string]string
			CurrentProgram *struct{ Name string }
			UserData       struct{ IsFavorite bool }
		}
		TotalRecordCount int
	}
	if s := tv.call("GET", "/LiveTv/Channels?AddCurrentProgram=true", nil, &cl); s != 200 || cl.TotalRecordCount != 2 ||
		cl.Items[0].Type != "TvChannel" || cl.Items[0].CurrentProgram == nil || cl.Items[0].CurrentProgram.Name != "Live Show" ||
		cl.Items[0].ImageTags["Primary"] == "" || !cl.Items[1].UserData.IsFavorite {
		t.Fatalf("compat channels: %d %+v", s, cl)
	}
	if code, _, hdr := tv.raw("GET", "/Items/"+cl.Items[0].ID+"/Images/Primary", nil, nil); code != 200 || hdr.Get("Content-Type") != "image/png" {
		t.Fatalf("compat channel logo: %d", code)
	}
	var item struct{ Name, Type string }
	if s := tv.call("GET", "/Items/"+cl.Items[0].ID, nil, &item); s != 200 || item.Type != "TvChannel" || item.Name != "Channel One" {
		t.Fatalf("compat channel item: %d %+v", s, item)
	}
	var progs struct {
		Items []struct {
			Name, Type, ChannelID string
			StartDate             string
			IsNews                bool
			IndexNumber           int
		}
		TotalRecordCount int
	}
	if s := tv.call("POST", "/LiveTv/Programs", map[string]any{"ChannelIds": []string{cl.Items[0].ID}, "MinStartDate": tomorrow,
		"HasAired": false}, &progs); s != 200 || progs.TotalRecordCount != 2 || progs.Items[0].Name != "Evening News" || !progs.Items[0].IsNews ||
		progs.Items[0].IndexNumber != 1 || progs.Items[0].Type != "Program" {
		t.Fatalf("compat programmes: %d %+v", s, progs)
	}
	if s := tv.call("GET", "/LiveTv/Programs/Recommended?IsAiring=true&Limit=10", nil, &progs); s != 200 || progs.TotalRecordCount != 2 {
		t.Fatalf("compat on now: %d %+v", s, progs)
	}
	var empty struct{ TotalRecordCount int }
	if s := tv.call("GET", "/LiveTv/Timers", nil, &empty); s != 200 || empty.TotalRecordCount != 0 {
		t.Fatalf("compat timers: %d", s)
	}

	// --- permissions ---
	var u struct {
		ID          string         `json:"id"`
		Permissions map[string]any `json:"permissions"`
	}
	adminC.do("POST", "/users", map[string]any{"username": "tvviewer", "password": "viewer-pass-1"}, &u)
	adminC.do("GET", "/users/"+u.ID, nil, &u)
	if u.Permissions == nil {
		t.Fatal("no permissions in the user")
	}
	viewerC := newClient(t, srv)
	var login authResult
	viewerC.do("POST", "/auth/login", map[string]any{"username": "tvviewer", "password": "viewer-pass-1"}, &login)
	viewerC.token = login.Token
	if s := viewerC.do("GET", "/livetv/channels", nil, &chans); s != 200 || len(chans) != 2 || chans[0].Favorite || chans[1].Favorite {
		t.Fatalf("viewer channels (own favourites): %d %+v", s, chans)
	}
	var e apiError
	if s := viewerC.do("POST", "/livetv/recordings", map[string]any{"programId": eveningNews.ID}, &e); s != 403 {
		t.Fatalf("viewer may not record: %d", s)
	}
	if s := viewerC.do("GET", "/livetv/tuners", nil, &e); s != 403 {
		t.Fatalf("viewer may not see tuners: %d", s)
	}
	u.Permissions["liveTv"] = false
	adminC.do("PATCH", "/users/"+u.ID, map[string]any{"permissions": u.Permissions}, nil)
	if s := viewerC.do("GET", "/livetv/channels", nil, &e); s != 403 {
		t.Fatalf("without liveTv: %d", s)
	}
	if s := viewerC.do("GET", "/livetv/on-now", nil, &onNow); s != 200 || len(onNow) != 0 {
		t.Fatalf("on now without liveTv: %d %+v", s, onNow)
	}
	// A manager who is not an administrator cannot set the post-processing command.
	if _, err := svc.SaveSettings(ctx, livetv.Settings{PrePadding: 0, PostPadding: 0, TimeshiftMinutes: 1,
		PostProcess: livetv.PostProcess{Command: "/bin/true", TimeoutMinutes: 1}}, false); !errors.Is(err, livetv.ErrForbidden) {
		t.Fatalf("post-processing by a non-admin: %v", err)
	}

	// --- settings: 1-minute buffer, no padding, post-processing script ---
	script := filepath.Join(t.TempDir(), "post.sh")
	outFile := filepath.Join(t.TempDir(), "post.out")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > "+outFile+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	var st map[string]any
	if s := adminC.do("PUT", "/livetv/settings", map[string]any{"prePaddingSeconds": 0, "postPaddingSeconds": 0, "timeshiftMinutes": 1,
		"retention":   map[string]any{"enabled": false, "deleteAfterDays": 0, "keepLast": 0},
		"postProcess": map[string]any{"command": script, "args": []string{"{path}", "{title}", "--channel={channel}"}, "timeoutMinutes": 1}}, &st); s != 200 {
		t.Fatalf("settings: %d %+v", s, st)
	}
	if st["recordingsPath"] != recDir {
		t.Fatalf("recordings path: %+v", st)
	}

	// --- live playback: remux, shared session, time-shift window ---
	device := map[string]any{"device": map[string]any{"containers": []string{"mp4"}, "videoCodecs": []string{"h264"}, "audioCodecs": []string{"aac"}, "hls": true}}
	var pb struct {
		URL              string `json:"url"`
		Method           string `json:"method"`
		TimeshiftSeconds int    `json:"timeshiftSeconds"`
		Now              struct {
			Title string `json:"title"`
		} `json:"now"`
	}
	if s := adminC.do("POST", "/livetv/channels/"+one.ID+"/play", device, &pb); s != 200 || pb.Method != "remux" || pb.TimeshiftSeconds != 60 ||
		pb.Now.Title != "Live Show" {
		t.Fatalf("play: %d %+v", s, pb)
	}
	var codec string
	_ = testPool.QueryRow(ctx, `SELECT video_codec || '/' || audio_codec FROM livetv_channels WHERE id = $1`, one.ID).Scan(&codec)
	if codec != "h264/aac" {
		t.Fatalf("probed codecs: %q", codec)
	}
	code, body := getURL(t, srv.URL+pb.URL)
	if code != 200 || !strings.Contains(string(body), "#EXTINF") || strings.Contains(string(body), "#EXT-X-ENDLIST") {
		t.Fatalf("live playlist: %d %s", code, body)
	}
	// A second viewer of the same channel shares the session and the tuner.
	var pb2 struct {
		URL string `json:"url"`
	}
	adminC.do("POST", "/livetv/channels/"+one.ID+"/play", device, &pb2)
	if stream.ScopeOf(strings.SplitN(pb2.URL, "?", 2)[0]) != stream.ScopeOf(strings.SplitN(pb.URL, "?", 2)[0]) || svc.InUse(uuid.MustParse(tuner.ID)) != 1 {
		t.Fatalf("not shared: %s vs %s", pb2.URL, pb.URL)
	}
	// A TV app gets the same channel as an HLS transcoding URL (sharing the tuner).
	var pi struct {
		MediaSources []compatSource
		ErrorCode    string
	}
	if s := tv.call("POST", "/Items/"+strings.ReplaceAll(one.ID, "-", "")+"/PlaybackInfo", map[string]any{"DeviceProfile": map[string]any{
		"TranscodingProfiles": []map[string]any{{"Container": "ts", "Type": "Video", "VideoCodec": "h264", "AudioCodec": "aac", "Protocol": "hls"}}}}, &pi); s != 200 ||
		len(pi.MediaSources) != 1 || pi.MediaSources[0].TranscodingSubProtocol != "hls" || pi.MediaSources[0].SupportsDirectPlay {
		t.Fatalf("compat playback info: %d %+v", s, pi)
	}
	if code, body := getURL(t, srv.URL+pi.MediaSources[0].TranscodingURL); code != 200 || !strings.Contains(string(body), "#EXTINF") {
		t.Fatalf("compat live playlist: %d", code)
	}
	if svc.InUse(uuid.MustParse(tuner.ID)) != 1 {
		t.Fatal("the TV app took a second tuner")
	}
	// The only tuner is busy: another channel cannot be tuned.
	if s := adminC.do("POST", "/livetv/channels/"+two.ID+"/play", device, &e); s != 503 || e.Code != "tuners_busy" {
		t.Fatalf("second channel on a one-tuner source: %d %+v", s, e)
	}
	// Time-shift: after a few seconds the window holds several segments, and
	// the oldest one (the furthest a viewer can rewind) can still be fetched.
	time.Sleep(6 * time.Second)
	base := srv.URL + strings.SplitN(pb.URL, "index.m3u8", 2)[0]
	_, body = getURL(t, srv.URL+pb.URL)
	segs := segmentsOf(string(body))
	if len(segs) < 4 {
		t.Fatalf("time-shift window: %s", body)
	}
	if code, seg := getURL(t, base+segs[0]); code != 200 || len(seg) == 0 {
		t.Fatalf("rewind to the oldest segment: %d", code)
	}
	if code, _ := getURL(t, base+"init.mp4?"+strings.SplitN(pb.URL, "?", 2)[1]); code != 200 {
		t.Fatalf("init segment: %d", code)
	}

	// The buffer is bounded: a session with a 3-segment window deletes older segments.
	small, err := hls.StartLive(ctx, stream.Job{Input: src.srv.URL + "/live/two.ts", AudioIndex: 0, Plan: stream.Plan{Method: "remux"},
		InputArgs: []string{"-protocol_whitelist", "http,tcp"}, ADTSAudio: true}, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(9 * time.Second)
	files, _ := filepath.Glob(filepath.Join(hls.Dir, small, "seg_*.m4s"))
	pl, _ := os.ReadFile(filepath.Join(hls.Dir, small, "index.m3u8"))
	if len(files) == 0 || len(files) > 6 || len(segmentsOf(string(pl))) > 3 || !strings.Contains(string(pl), "#EXT-X-MEDIA-SEQUENCE:") ||
		strings.Contains(string(pl), "#EXT-X-MEDIA-SEQUENCE:0\n") {
		t.Fatalf("bounded buffer: %d files, playlist %s", len(files), pl)
	}
	if _, err := os.Stat(filepath.Join(hls.Dir, small, "seg_00000.m4s")); !os.IsNotExist(err) {
		t.Fatal("the oldest segment was not deleted")
	}
	hls.Stop(small)

	// --- recording: takes the viewer's tuner, records, post-processes ---
	var clipID string
	if err := testPool.QueryRow(ctx, `SELECT id::text FROM livetv_programs WHERE title = 'Short Clip'`).Scan(&clipID); err != nil {
		t.Fatal(err)
	}
	var rec liveRecordingJSON
	if s := adminC.do("POST", "/livetv/recordings", map[string]any{"programId": clipID}, &rec); s != 201 || rec.Status != "scheduled" {
		t.Fatalf("schedule: %d %+v", s, rec)
	}
	var again liveRecordingJSON
	if adminC.do("POST", "/livetv/recordings", map[string]any{"programId": clipID}, &again); again.ID != rec.ID {
		t.Fatal("scheduling twice must not duplicate")
	}
	deadline := time.Now().Add(90 * time.Second)
	sawRecording := false
	for {
		var list []liveRecordingJSON
		adminC.do("GET", "/livetv/recordings?status=recording,completed,failed", nil, &list)
		var cur *liveRecordingJSON
		for i := range list {
			if list[i].ID == rec.ID {
				cur = &list[i]
			}
		}
		if cur != nil && cur.Status == "recording" && !sawRecording {
			sawRecording = true
			// The live viewer was stopped so the recording could use the only tuner.
			time.Sleep(time.Second)
			if svc.ActiveSessions() != 0 {
				t.Fatal("the live session kept the tuner")
			}
			if code, _ := getURL(t, srv.URL+pb.URL); code != 404 {
				t.Fatalf("evicted session still served: %d", code)
			}
		}
		if cur != nil && (cur.Status == "completed" || cur.Status == "failed") {
			rec = *cur
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("recording did not finish: %+v", list)
		}
		time.Sleep(time.Second)
	}
	if rec.Status != "completed" || rec.SizeBytes == 0 || !sawRecording {
		t.Fatalf("recording: %+v (saw recording %v)", rec, sawRecording)
	}
	var path string
	_ = testPool.QueryRow(ctx, `SELECT path FROM livetv_recordings WHERE id = $1`, rec.ID).Scan(&path)
	if !strings.HasPrefix(path, filepath.Join(recDir, "Short Clip")+"/") || !strings.HasSuffix(path, ".ts") {
		t.Fatalf("path %q", path)
	}
	probe, err := library.FFprobe("ffprobe")(ctx, path)
	if err != nil || probe.VideoCodec != "h264" || probe.DurationMS < 3000 || len(probe.Audio) != 1 {
		t.Fatalf("recorded file: %+v %v", probe, err)
	}
	nfo, err := os.ReadFile(strings.TrimSuffix(path, ".ts") + ".nfo")
	if err != nil || !strings.Contains(string(nfo), "<showtitle>Short Clip</showtitle>") || !strings.Contains(string(nfo), "Recorded in the test") {
		t.Fatalf("nfo: %s %v", nfo, err)
	}
	post, err := os.ReadFile(outFile)
	if err != nil || string(post) != path+"\nShort Clip\n--channel=Channel One\n" {
		t.Fatalf("post-processing arguments: %q %v", post, err)
	}

	// --- the recordings library ---
	libID := svc.RecordingsLibrary(ctx)
	if libID == nil {
		t.Fatal("no recordings library")
	}
	scanner := &library.Scanner{Pool: testPool, Roots: library.Roots{mediaRoot}, Probe: library.FFprobe("ffprobe"), Log: log}
	if _, err := scanner.Scan(ctx, *libID); err != nil {
		t.Fatal(err)
	}
	ref := &metadata.Refresher{Pool: testPool, Log: log, ResolvePath: func(p string) (string, error) { return library.ResolveSafePath(library.Roots{mediaRoot}, p) }}
	if _, err := ref.LocalNFO(ctx, *libID); err != nil {
		t.Fatal(err)
	}
	var recs []liveRecordingJSON
	adminC.do("GET", "/livetv/recordings?status=completed", nil, &recs)
	if len(recs) != 1 || recs[0].ItemID == "" {
		t.Fatalf("recording in the library: %+v", recs)
	}
	var series string
	_ = testPool.QueryRow(ctx, `SELECT COALESCE(s.metadata_title, s.title) FROM items e JOIN items se ON se.id = e.parent_id
		JOIN items s ON s.id = se.parent_id WHERE e.id = $1`, recs[0].ItemID).Scan(&series)
	if series != "Short Clip" {
		t.Fatalf("library series: %q", series)
	}

	// --- series rule (new episodes only) and conflicts on the one tuner ---
	var rule struct {
		ID       string `json:"id"`
		Upcoming int    `json:"upcoming"`
	}
	if s := adminC.do("POST", "/livetv/series-rules", map[string]any{"programId": eveningNews.ID, "newOnly": true}, &rule); s != 201 || rule.Upcoming != 2 {
		t.Fatalf("series rule: %d %+v", s, rule)
	}
	var film liveRecordingJSON
	if s := adminC.do("POST", "/livetv/recordings", map[string]any{"programId": lateFilm.ID, "channelId": two.ID}, &film); s != 201 || film.Status != "conflict" {
		t.Fatalf("overlapping recording: %d %+v", s, film)
	}
	if s := adminC.do("PATCH", "/livetv/recordings/"+film.ID, map[string]any{"priority": 10}, &film); s != 200 || film.Status != "scheduled" {
		t.Fatalf("higher priority: %d %+v", s, film)
	}
	var upcoming []liveRecordingJSON
	adminC.do("GET", "/livetv/recordings?status=scheduled,conflict", nil, &upcoming)
	statuses := map[string]int{}
	for _, r := range upcoming {
		statuses[r.Title+":"+r.Status]++
	}
	if statuses["Evening News:conflict"] != 1 || statuses["Evening News:scheduled"] != 1 || statuses["Late Film:scheduled"] != 1 {
		t.Fatalf("after the priority change: %+v", statuses)
	}
	if s := adminC.do("DELETE", "/livetv/series-rules/"+rule.ID, nil, nil); s != 204 {
		t.Fatalf("delete rule: %d", s)
	}
	adminC.do("GET", "/livetv/recordings?status=scheduled,conflict", nil, &upcoming)
	if len(upcoming) != 1 || upcoming[0].Title != "Late Film" {
		t.Fatalf("upcoming after deleting the rule: %+v", upcoming)
	}

	// --- retention: off by default; on, it removes only DVR recordings ---
	own := filepath.Join(recDir, "Short Clip", "my own file.ts")
	_ = os.WriteFile(own, []byte("mine"), 0o644)
	userMedia := filepath.Join(mediaRoot, "movies", "Keep.ts")
	_ = os.MkdirAll(filepath.Dir(userMedia), 0o755)
	_ = os.WriteFile(userMedia, []byte("keep"), 0o644)
	_, _ = testPool.Exec(ctx, `UPDATE livetv_recordings SET finished_at = now() - interval '10 days' WHERE id = $1`, rec.ID)
	if err := svc.ApplyRetention(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("retention ran while turned off")
	}
	if s := adminC.do("PUT", "/livetv/settings", map[string]any{"prePaddingSeconds": 0, "postPaddingSeconds": 0, "timeshiftMinutes": 1,
		"retention":   map[string]any{"enabled": true, "deleteAfterDays": 7, "keepLast": 0},
		"postProcess": map[string]any{"command": script, "args": []string{"{path}", "{title}", "--channel={channel}"}, "timeoutMinutes": 1}}, &st); s != 200 {
		t.Fatalf("enable retention: %d", s)
	}
	// A recording row pointing outside the recordings folder is never deleted.
	_, _ = testPool.Exec(ctx, `INSERT INTO livetv_recordings (id, title, start_at, end_at, status, path, size_bytes, finished_at)
		VALUES ($1, 'Forged', now(), now(), 'completed', $2, 4, now() - interval '30 days')`, uuid.New(), userMedia)
	if err := svc.ApplyRetention(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("old recording kept")
	}
	if _, err := os.Stat(strings.TrimSuffix(path, ".ts") + ".nfo"); !os.IsNotExist(err) {
		t.Fatal("its nfo kept")
	}
	for _, p := range []string{own, userMedia} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s was deleted", p)
		}
	}
	var status string
	_ = testPool.QueryRow(ctx, `SELECT status FROM livetv_recordings WHERE id = $1`, rec.ID).Scan(&status)
	if status != "deleted" {
		t.Fatalf("status after retention: %s", status)
	}
}

// fakeHDHomeRunDevice answers the documented HDHomeRun HTTP API: a two-tuner
// device with one working channel and one whose tuning fails as "all tuners in use".
func fakeHDHomeRunDevice(t *testing.T) *httptest.Server {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/discover.json":
			_ = json.NewEncoder(w).Encode(map[string]any{"FriendlyName": "HDHomeRun FLEX", "ModelNumber": "HDFX-2US", "DeviceID": "10ABCDEF",
				"TunerCount": 2, "BaseURL": srv.URL, "LineupURL": srv.URL + "/lineup.json"})
		case "/lineup.json":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"GuideNumber": "5.1", "GuideName": "Antenna Five", "URL": srv.URL + "/auto/v5.1", "HD": 1, "VideoCodec": "H264", "AudioCodec": "AAC"},
				{"GuideNumber": "9.1", "GuideName": "Busy Nine", "URL": srv.URL + "/auto/v9.1", "VideoCodec": "H264", "AudioCodec": "AAC"},
			})
		case "/auto/v5.1":
			serveTS(w, r)
		case "/auto/v9.1":
			w.Header().Set("X-HDHomeRun-Error", "805 All Tuners In Use")
			http.Error(w, "All Tuners In Use", http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestLiveTVHDHomeRunIntegration(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	srv := newAuthTestServer(t)
	adminC := newClient(t, srv)
	setupAdmin(t, adminC)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if _, err := testPool.Exec(ctx, `DELETE FROM livetv_recordings; DELETE FROM livetv_tuners; DELETE FROM livetv_guides; DELETE FROM settings WHERE key = 'livetv'`); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	hls := &stream.Manager{Dir: t.TempDir(), FFmpeg: "ffmpeg", Log: log,
		Encoding: func(context.Context) stream.Encoding { return stream.Encoding{SegmentSeconds: 1} }}
	go hls.Reap(ctx)
	signer := stream.Signer{Key: []byte("test-key")}
	svc := &livetv.Service{Pool: testPool, Settings: testState.Settings, Signer: signer, HLS: hls, FFmpeg: "ffmpeg", FFprobe: "ffprobe-not-needed",
		FileRoots: library.Roots{mediaRoot}, HTTP: livetv.NewHTTPClient(time.Minute, true), Log: log}
	testState.SetStream(&stream.Server{Pool: testPool, Roots: library.Roots{mediaRoot}, Signer: signer, HLS: hls, FFmpeg: "ffmpeg"})
	testState.SetLiveTV(svc)
	dev := fakeHDHomeRunDevice(t)
	t.Cleanup(func() { stopLive(cancel, hls) })

	var tr struct {
		Ok         bool   `json:"ok"`
		TunerCount int    `json:"tunerCount"`
		Model      string `json:"model"`
	}
	if s := adminC.do("POST", "/livetv/tuners/test", map[string]any{"kind": "hdhomerun", "name": "Antenna", "url": strings.TrimPrefix(dev.URL, "http://")}, &tr); s != 200 ||
		!tr.Ok || tr.TunerCount != 2 || tr.Model != "HDFX-2US" {
		t.Fatalf("test: %d %+v", s, tr)
	}
	var tuner struct {
		ID           string `json:"id"`
		TunerCount   int    `json:"tunerCount"`
		ChannelCount int    `json:"channelCount"`
		DeviceID     string `json:"deviceId"`
	}
	if s := adminC.do("POST", "/livetv/tuners", map[string]any{"kind": "hdhomerun", "name": "Antenna", "url": dev.URL}, &tuner); s != 201 ||
		tuner.TunerCount != 2 || tuner.ChannelCount != 2 || tuner.DeviceID != "10ABCDEF" {
		t.Fatalf("add: %d %+v", s, tuner)
	}
	var chans []liveChannelJSON
	adminC.do("GET", "/livetv/channels", nil, &chans)
	if len(chans) != 2 || chans[0].Number != "5.1" || chans[1].Number != "9.1" {
		t.Fatalf("lineup: %+v", chans)
	}
	device := map[string]any{"device": map[string]any{"containers": []string{"mp4"}, "videoCodecs": []string{"h264"}, "audioCodecs": []string{"aac"}, "hls": true}}
	var pb struct {
		URL    string `json:"url"`
		Method string `json:"method"`
	}
	// Codecs come from the lineup (no probe: the probe binary does not even exist here).
	if s := adminC.do("POST", "/livetv/channels/"+chans[0].ID+"/play", device, &pb); s != 200 || pb.Method != "remux" {
		t.Fatalf("tune 5.1: %d %+v", s, pb)
	}
	if code, body := getURL(t, srv.URL+pb.URL); code != 200 || !strings.Contains(string(body), "#EXTINF") {
		t.Fatalf("playlist: %d", code)
	}
	// One of our two tuners is free, but the device answers 503 (all its tuners in use).
	var e apiError
	if s := adminC.do("POST", "/livetv/channels/"+chans[1].ID+"/play", device, &e); s != 503 || e.Code != "tuners_busy" {
		t.Fatalf("device busy: %d %+v", s, e)
	}
	// A browser without AC3 support gets AC3 audio transcoded.
	_, _ = testPool.Exec(ctx, `UPDATE livetv_channels SET audio_codec = 'ac3' WHERE id = $1`, chans[0].ID)
	var pb3 struct {
		Method string `json:"method"`
		Reason string `json:"reason"`
	}
	if s := adminC.do("POST", "/livetv/channels/"+chans[0].ID+"/play", device, &pb3); s != 200 || pb3.Method != "remux" || !strings.Contains(pb3.Reason, "ac3") {
		t.Fatalf("audio transcode: %d %+v", s, pb3)
	}
	// Both tuners are now in use by us: a third stream is refused without asking the device.
	if s := adminC.do("POST", "/livetv/channels/"+chans[1].ID+"/play", device, &e); s != 503 || e.Code != "tuners_busy" {
		t.Fatalf("tuner limit: %d %+v", s, e)
	}
}
