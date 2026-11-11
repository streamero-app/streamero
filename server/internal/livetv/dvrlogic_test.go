package livetv

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

var t0 = time.Date(2026, 11, 11, 20, 0, 0, 0, time.UTC)

func at(min int) time.Time { return t0.Add(time.Duration(min) * time.Minute) }

func TestWindowPadding(t *testing.T) {
	s, e := Window(at(0), at(60), 120, 300)
	if !s.Equal(at(-2)) || !e.Equal(at(65)) {
		t.Fatalf("padding: %v %v", s, e)
	}
	s, e = Window(at(0), at(60), -5, 999999)
	if !s.Equal(at(0)) || !e.Equal(at(60).Add(2*time.Hour)) {
		t.Fatalf("clamped: %v %v", s, e)
	}
}

func TestResolveConflicts(t *testing.T) {
	tunerA, tunerB := uuid.New(), uuid.New()
	id := func() uuid.UUID { return uuid.New() }
	a, b, c, d, e := id(), id(), id(), id(), id()
	slots := []Slot{
		{ID: a, Tuner: tunerA, Start: at(0), End: at(60), Created: at(-100)},
		{ID: b, Tuner: tunerA, Start: at(30), End: at(90), Created: at(-90)},
		{ID: c, Tuner: tunerA, Start: at(45), End: at(50), Created: at(-80), Priority: 5}, // wins on priority
		{ID: d, Tuner: tunerA, Start: at(60), End: at(120), Created: at(-70)},            // back-to-back with a
		{ID: e, Tuner: tunerB, Start: at(0), End: at(120), Created: at(-60)},             // other source
	}
	caps := map[uuid.UUID]int{tunerA: 2, tunerB: 1}
	res := ResolveConflicts(slots, func(t uuid.UUID) int { return caps[t] })
	// c (prio 5) first, then a, then b overlaps a and c at 45-50 → 3 > 2: conflict. d fits (a ended at 60).
	want := map[uuid.UUID]bool{a: true, b: false, c: true, d: true, e: true}
	for k, v := range want {
		if res.Accepted[k] != v {
			t.Errorf("accepted[%v] = %v, want %v", k, res.Accepted[k], v)
		}
	}
	if len(res.ConflictsWith[b]) != 2 { // a and c were accepted before b was considered
		t.Errorf("b conflicts with %v", res.ConflictsWith[b])
	}

	// A running recording always keeps its tuner, even over capacity.
	r := uuid.New()
	slots = []Slot{{ID: a, Tuner: tunerB, Start: at(0), End: at(60), Priority: 100, Created: at(-10)},
		{ID: r, Tuner: tunerB, Start: at(-10), End: at(30), Running: true, Created: at(0)}}
	res = ResolveConflicts(slots, func(uuid.UUID) int { return 1 })
	if !res.Accepted[r] || res.Accepted[a] {
		t.Fatalf("running first: %+v", res.Accepted)
	}
	// Unlimited sources accept everything.
	res = ResolveConflicts(slots, func(uuid.UUID) int { return 0 })
	if !res.Accepted[a] || !res.Accepted[r] {
		t.Fatal("unlimited")
	}
	// Equal priority: the earlier-created recording wins.
	x, y := uuid.New(), uuid.New()
	res = ResolveConflicts([]Slot{{ID: y, Tuner: tunerA, Start: at(0), End: at(10), Created: at(5)},
		{ID: x, Tuner: tunerA, Start: at(5), End: at(15), Created: at(1)}}, func(uuid.UUID) int { return 1 })
	if !res.Accepted[x] || res.Accepted[y] {
		t.Fatal("earlier created wins")
	}
}

func TestSeriesRuleMatching(t *testing.T) {
	ch1, ch2 := uuid.New(), uuid.New()
	p := func() uuid.UUID { return uuid.New() }
	shared := p()
	airings := []Airing{
		{ProgramID: p(), ChannelID: ch1, Title: "The News", Start: at(0), EpisodeKey: "s1e1", IsNew: true},
		{ProgramID: p(), ChannelID: ch1, Title: "the news!", Start: at(60), EpisodeKey: "s1e1", IsRepeat: true}, // repeat of e1
		{ProgramID: p(), ChannelID: ch2, Title: "The News", Start: at(120), EpisodeKey: "s1e2"},
		{ProgramID: p(), ChannelID: ch1, Title: "The News", Start: at(180), EpisodeKey: "s1e3", IsRepeat: true}, // marked repeat
		{ProgramID: p(), ChannelID: ch1, Title: "Other Show", Start: at(30), EpisodeKey: "s1e9"},
		{ProgramID: shared, ChannelID: ch1, Title: "The News", Start: at(240)},
		{ProgramID: shared, ChannelID: ch2, Title: "The News", Start: at(240)}, // same airing on another channel
		{ProgramID: p(), ChannelID: ch1, Title: "The News", Start: at(300), EpisodeKey: "s1e4"},
	}
	newOnly := SelectAirings(RuleSpec{Title: "THE NEWS", NewOnly: true}, airings, map[string]bool{"s1e4": true})
	var keys []string
	for _, a := range newOnly {
		keys = append(keys, a.EpisodeKey)
	}
	if strings.Join(keys, ",") != "s1e1,s1e2," {
		t.Fatalf("new only = %q", strings.Join(keys, ","))
	}
	all := SelectAirings(RuleSpec{Title: "The News"}, airings, nil)
	if len(all) != 6 { // every airing once; the shared one only once
		t.Fatalf("all = %d", len(all))
	}
	onCh2 := SelectAirings(RuleSpec{Title: "The News", ChannelID: &ch2}, airings, nil)
	if len(onCh2) != 2 || onCh2[0].ChannelID != ch2 || onCh2[1].ChannelID != ch2 {
		t.Fatalf("channel = %+v", onCh2)
	}
	if (RuleSpec{Title: "News"}).Matches(airings[0]) {
		t.Fatal("partial titles must not match")
	}
}

func TestPostProcessArgs(t *testing.T) {
	s, e := 2, 5
	r := Recording{ID: uuid.MustParse("01900000-0000-7000-8000-000000000001"), Path: "/recordings/Show/Season 2/Show - S02E05 - It's \"on\"; rm -rf.ts",
		Title: "Show; $(reboot)", SubTitle: "It's \"on\"", ChannelName: "HRT 1", ChannelNumber: "1", Start: at(0), End: at(30),
		Season: &s, Episode: &e}
	got := PostProcessArgs([]string{"--in={path}", "{title}", "S{season}E{episode}", "{dir}", "{filename}", "{start}", "literal"}, r)
	want := []string{"--in=" + r.Path, "Show; $(reboot)", "S2E5", "/recordings/Show/Season 2", filepath.Base(r.Path), "2026-11-11T20:00:00Z", "literal"}
	if len(got) != len(want) {
		t.Fatalf("%v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("arg %d = %q, want %q", i, got[i], want[i])
		}
	}
	if err := validatePostProcess(PostProcess{Command: "relative/cmd", TimeoutMinutes: 5}); err == nil {
		t.Error("relative command accepted")
	}
	if err := validatePostProcess(PostProcess{Command: "/etc/hostname", TimeoutMinutes: 5}); err == nil {
		t.Error("non-executable accepted")
	}
	if err := validatePostProcess(PostProcess{Command: ""}); err != nil {
		t.Error("empty = off")
	}
}

func TestRecordingPaths(t *testing.T) {
	dir := t.TempDir()
	s := &Service{RecordingsDir: dir}
	se, ep, y := 1, 2, 1999
	r := Recording{Title: "Doctor/Who: ..", SubTitle: "The \"Pilot\"", Season: &se, Episode: &ep, Start: at(0)}
	p := s.recordingPath(r)
	if p != filepath.Join(dir, "Doctor Who", "Season 1", "Doctor Who - S01E02 - The Pilot.ts") {
		t.Fatalf("episode path %q", p)
	}
	m := Recording{Title: "Casablanca", Categories: []string{"Movie"}, Year: &y, Start: at(0)}
	if got := s.recordingPath(m); got != filepath.Join(dir, "Movies", "Casablanca (1999)", "Casablanca (1999).ts") {
		t.Fatalf("movie path %q", got)
	}
	n := Recording{Title: "Dnevnik", Start: at(0)}
	got := s.recordingPath(n)
	if !strings.HasPrefix(got, filepath.Join(dir, "Dnevnik", "Dnevnik - 2026-11-1")) || !strings.HasSuffix(got, ".ts") {
		t.Fatalf("dated path %q", got)
	}
	// An existing file is never overwritten.
	_ = os.MkdirAll(filepath.Dir(got), 0o755)
	_ = os.WriteFile(got, []byte("x"), 0o644)
	if again := s.recordingPath(n); again == got || !strings.HasSuffix(again, " (2).ts") {
		t.Fatalf("collision: %q", again)
	}
	if safeName("..") != "Recording" || safeName("a\x00b") != "ab" {
		t.Fatal("safeName")
	}
	// Retention only ever touches .ts files inside the recordings folder.
	if _, err := s.recordingFile(got); err != nil {
		t.Fatalf("own file refused: %v", err)
	}
	other := filepath.Join(t.TempDir(), "movie.ts")
	_ = os.WriteFile(other, []byte("x"), 0o644)
	if _, err := s.recordingFile(other); err == nil {
		t.Fatal("file outside the recordings folder accepted")
	}
	nfo := filepath.Join(dir, "Dnevnik", "notes.nfo")
	_ = os.WriteFile(nfo, []byte("x"), 0o644)
	if _, err := s.recordingFile(nfo); err == nil {
		t.Fatal("non-recording file accepted")
	}
	if _, err := s.recordingFile(filepath.Join(dir, "Dnevnik", "..", "..", filepath.Base(other))); err == nil {
		t.Fatal("traversal accepted")
	}
}

// fakeHDHomeRun answers the documented HTTP API of a two-tuner device.
func fakeHDHomeRun(t *testing.T) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/discover.json":
			_ = json.NewEncoder(w).Encode(map[string]any{"FriendlyName": "HDHomeRun DUO", "ModelNumber": "HDHR5-2US",
				"DeviceID": "1050A1B2", "TunerCount": 2, "BaseURL": srv.URL, "LineupURL": "http://10.9.9.9/lineup.json"})
		case "/lineup.json":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"GuideNumber": "5.1", "GuideName": "KXYZ-HD", "URL": "http://10.9.9.9:5004/auto/v5.1", "HD": 1, "VideoCodec": "MPEG2", "AudioCodec": "AC3"},
				{"GuideNumber": "7.1", "GuideName": "Premium", "URL": "http://10.9.9.9:5004/auto/v7.1", "DRM": 1},
				{"GuideNumber": "9.1", "GuideName": "KABC", "URL": "http://10.9.9.9:5004/auto/v9.1", "VideoCodec": "H264", "AudioCodec": "AAC"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestHDHomeRunLineup(t *testing.T) {
	dev := fakeHDHomeRun(t)
	s := &Service{HTTP: NewHTTPClient(5*time.Second, true)}
	res := s.TestTuner(context.Background(), TunerInput{Kind: "hdhomerun", Name: "Antenna", URL: dev.URL})
	if !res.OK || res.TunerCount == nil || *res.TunerCount != 2 || *res.ChannelCount != 2 || res.Model != "HDHR5-2US" {
		t.Fatalf("test: %+v", res)
	}
	chans, d, err := s.loadSource(context.Background(), TunerInput{Kind: "hdhomerun", URL: strings.TrimPrefix(dev.URL, "http://")})
	if err != nil || d == nil || d.DeviceID != "1050A1B2" {
		t.Fatal(err)
	}
	host := strings.TrimPrefix(dev.URL, "http://")
	host = host[:strings.LastIndex(host, ":")]
	// DRM channels are left out; streams go to the configured address, on the device's port.
	if len(chans) != 2 || chans[0].url != "http://"+host+":5004/auto/v5.1" || chans[0].videoCodec != "mpeg2video" ||
		chans[0].audioCodec != "ac3" || !chans[0].hd || chans[1].number != "9.1" {
		t.Fatalf("lineup: %+v", chans)
	}
	// Loopback and link-local addresses are refused outside tests.
	strict := &Service{HTTP: NewHTTPClient(5*time.Second, false)}
	if r := strict.TestTuner(context.Background(), TunerInput{Kind: "hdhomerun", Name: "x", URL: dev.URL}); r.OK {
		t.Fatal("loopback allowed")
	}
	if r := strict.TestTuner(context.Background(), TunerInput{Kind: "hdhomerun", Name: "x", URL: "169.254.169.254"}); r.OK ||
		!strings.Contains(r.Message, "not allowed") {
		t.Fatalf("metadata address: %+v", r)
	}
}

func TestStreamSchemes(t *testing.T) {
	for raw, ok := range map[string]bool{"http://a/b.ts": true, "udp://@239.0.0.1:1234": true, "rtsp://cam/x": true,
		"file:///etc/passwd": false, "/etc/passwd": false, "concat:a|b": false, "pipe:0": false, "http:///nohost": false} {
		if allowedStreamScheme(raw) != ok {
			t.Errorf("%q: want %v", raw, ok)
		}
	}
	if a := inputArgs("udp://@239.0.0.1:1", ""); strings.Contains(strings.Join(a, " "), "reconnect") {
		t.Fatal("http options on udp")
	}
	if a := strings.Join(inputArgs("https://x/y", "UA/1"), " "); !strings.Contains(a, "-user_agent UA/1") || !strings.Contains(a, "-protocol_whitelist") {
		t.Fatalf("http args: %s", a)
	}
}
