package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/compat"
	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/testmedia"
)

// TestSubtitleBurnInAndSegmentsIntegration plays a real file with a PGS track
// through the Streamero and compat APIs (burn-in decision, reason on the
// dashboard, HLS output, seeking) and checks that detected segments reach both,
// and that the detection task is listed and can be started.
func TestSubtitleBurnInAndSegmentsIntegration(t *testing.T) {
	srv := newAuthTestServer(t)
	adminC := newClient(t, srv)
	setupAdmin(t, adminC)

	// Tasks: intro/credits detection is its own scheduled task with "Run now".
	var tasks []struct {
		Key, Interval string
	}
	if s := adminC.do("GET", "/tasks", nil, &tasks); s != 200 || !slices.ContainsFunc(tasks, func(x struct{ Key, Interval string }) bool {
		return x.Key == "detect_segments" && x.Interval == "24h"
	}) {
		t.Fatalf("tasks: %d %+v", s, tasks)
	}
	ranTasks = nil
	if s := adminC.do("POST", "/tasks/detect_segments/run", nil, nil); s != 202 || len(ranTasks) != 1 || ranTasks[0] != "detect_segments" {
		t.Fatalf("run detection: %d %v", s, ranTasks)
	}

	ffmpeg, err1 := exec.LookPath("ffmpeg")
	ffprobe, err2 := exec.LookPath("ffprobe")
	if err1 != nil || err2 != nil {
		t.Skip("ffmpeg/ffprobe not installed")
	}
	testState.Stream().FFmpeg, testState.Stream().HLS.FFmpeg = ffmpeg, ffmpeg

	dir := mediaRoot + "/burn/Signs (2021)"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	sup := filepath.Join(t.TempDir(), "signs.sup")
	if err := os.WriteFile(sup, testmedia.PGSBox(320, 240, testmedia.Box{X: 110, Y: 180, W: 100, H: 40}, 1000, 4000), 0o600); err != nil {
		t.Fatal(err)
	}
	film := filepath.Join(dir, "Signs (2021).mkv")
	if out, err := exec.Command(ffmpeg, "-v", "error", "-y", "-f", "lavfi", "-i", "color=black:s=320x240:r=10:d=6",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=6", "-i", sup, "-map", "0:v", "-map", "1:a", "-map", "2:s",
		"-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac", "-c:s", "copy", film).CombinedOutput(); err != nil {
		t.Fatalf("make film: %v %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(dir, "Signs (2021).en.srt"), []byte("1\n00:00:01,000 --> 00:00:02,000\nHi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var lib struct {
		ID string `json:"id"`
	}
	adminC.do("POST", "/libraries", map[string]any{"name": "Burn", "kind": "movies", "paths": []string{mediaRoot + "/burn"}}, &lib)
	libID := uuid.MustParse(lib.ID)
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM libraries WHERE id = $1`, libID) })
	scanner := &library.Scanner{Pool: testPool, Roots: library.Roots{mediaRoot}, Probe: library.FFprobe(ffprobe),
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if _, err := scanner.Scan(context.Background(), libID); err != nil {
		t.Fatal(err)
	}
	var page struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	adminC.do("GET", "/libraries/"+lib.ID+"/items", nil, &page)
	if len(page.Items) != 1 {
		t.Fatalf("items: %+v", page)
	}
	item := page.Items[0].ID

	type playback struct {
		Method    string `json:"method"`
		URL       string `json:"url"`
		Reason    string `json:"reason"`
		Subtitles []struct {
			Index int    `json:"index"`
			URL   string `json:"url"`
		} `json:"subtitles"`
		BitmapSubtitles []struct {
			Index  int    `json:"index"`
			Codec  string `json:"codec"`
			Forced bool   `json:"forced"`
		} `json:"bitmapSubtitles"`
		BurnedSubtitle  *int `json:"burnedSubtitle"`
		SubtitleDefault *int `json:"subtitleDefault"`
		Segments        []struct {
			Kind    string `json:"kind"`
			StartMs int64  `json:"startMs"`
			EndMs   int64  `json:"endMs"`
		} `json:"segments"`
	}
	web := map[string]any{"containers": []string{"mp4", "mkv"}, "videoCodecs": []string{"h264"}, "audioCodecs": []string{"aac"}, "hls": true}
	play := func(body map[string]any) playback {
		t.Helper()
		var info playback
		body["device"] = web
		if s := adminC.do("POST", "/items/"+item+"/playback", body, &info); s != 200 {
			t.Fatalf("playback %v: HTTP %d", body, s)
		}
		return info
	}
	// The embedded PGS track (subtitle 0 or 1 depending on sorting) is offered for burn-in, not as WebVTT.
	info := play(map[string]any{})
	if info.Method != "direct" || info.BurnedSubtitle != nil || len(info.BitmapSubtitles) != 1 ||
		info.BitmapSubtitles[0].Codec != "hdmv_pgs_subtitle" || len(info.Subtitles) != 1 {
		t.Fatalf("plain playback: %+v", info)
	}
	pgs := info.BitmapSubtitles[0].Index

	// Choosing it transcodes with the picture burned in, and seeking still works.
	for _, start := range []int64{0, 2_500} {
		info = play(map[string]any{"subtitleIndex": pgs, "startMs": start})
		if info.Method != "transcode" || info.BurnedSubtitle == nil || *info.BurnedSubtitle != pgs ||
			!strings.Contains(info.Reason, "subtitle burn-in") || info.SubtitleDefault != nil {
			t.Fatalf("burn-in from %d: %+v", start, info)
		}
		resp, err := http.Get(srv.URL + info.URL)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != 200 || !strings.Contains(string(body), "#EXTINF") || !strings.Contains(string(body), "sig=") {
			t.Fatalf("hls playlist from %d: %d %s", start, resp.StatusCode, body)
		}
	}
	var dash struct {
		NowPlaying []struct{ Method, Reason string }
	}
	if adminC.do("GET", "/dashboard", nil, &dash); len(dash.NowPlaying) != 1 || dash.NowPlaying[0].Method != "transcode" ||
		!strings.Contains(dash.NowPlaying[0].Reason, "subtitle burn-in") {
		t.Fatalf("dashboard: %+v", dash)
	}
	// The text track is a WebVTT file, chosen without touching the picture.
	info = play(map[string]any{"subtitleIndex": info.Subtitles[0].Index})
	if info.Method != "direct" || info.SubtitleDefault == nil || *info.SubtitleDefault != 0 {
		t.Fatalf("srt choice: %+v", info)
	}

	// A forced PGS track is burned in by default; "none" turns it off.
	if _, err := testPool.Exec(context.Background(), `UPDATE media_files SET subtitles = jsonb_set(subtitles, ARRAY[$2::text, 'forced'], 'true')
		WHERE library_id = $1`, libID, strconv.Itoa(pgs)); err != nil {
		t.Fatal(err)
	}
	if info = play(map[string]any{}); info.BurnedSubtitle == nil || *info.BurnedSubtitle != pgs || info.Method != "transcode" {
		t.Fatalf("forced default: %+v", info)
	}
	if info = play(map[string]any{"subtitleIndex": -1}); info.BurnedSubtitle != nil || info.Method != "direct" {
		t.Fatalf("forced turned off: %+v", info)
	}

	// Detected segments reach the player and compat clients like the others.
	var fileID uuid.UUID
	if err := testPool.QueryRow(context.Background(), `SELECT id FROM media_files WHERE library_id = $1`, libID).Scan(&fileID); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(context.Background(), `INSERT INTO media_segments (file_id, kind, start_ms, end_ms, source)
		VALUES ($1, 'intro', 500, 2500, 'fingerprint'), ($1, 'credits', 4000, 6000, 'blackframe')`, fileID); err != nil {
		t.Fatal(err)
	}
	if info = play(map[string]any{"subtitleIndex": -1}); len(info.Segments) != 2 || info.Segments[0].Kind != "intro" ||
		info.Segments[1].Kind != "credits" || info.Segments[1].StartMs != 4000 {
		t.Fatalf("segments: %+v", info.Segments)
	}

	// Compat client: a profile without bitmap support asks for the track, the server encodes it.
	c := &compatCaller{t: t, base: srv.URL + "/compat"}
	var login struct{ AccessToken string }
	c.call("POST", "/Users/AuthenticateByName", map[string]any{"Username": "Admin", "Pw": "admin-pass-1"}, &login)
	c.token = login.AccessToken
	id := compat.ID(uuid.MustParse(item))
	profile := map[string]any{"DirectPlayProfiles": []any{map[string]any{"Type": "Video", "Container": "mkv", "VideoCodec": "h264", "AudioCodec": "aac"}},
		"TranscodingProfiles": []any{map[string]any{"Type": "Video", "Container": "ts", "VideoCodec": "h264", "AudioCodec": "aac", "Protocol": "hls"}},
		"SubtitleProfiles":    []any{map[string]any{"Format": "srt", "Method": "External"}, map[string]any{"Format": "pgssub", "Method": "Encode"}}}
	var pb struct {
		MediaSources []compatSource
	}
	// Streams: video 0, audio 1, subtitles from 2.
	if s := c.call("POST", "/Items/"+id+"/PlaybackInfo", map[string]any{"DeviceProfile": profile, "SubtitleStreamIndex": 2 + pgs}, &pb); s != 200 ||
		len(pb.MediaSources) != 1 || pb.MediaSources[0].TranscodingURL == "" || pb.MediaSources[0].SupportsDirectPlay {
		t.Fatalf("compat burn-in: %d %+v", s, pb)
	}
	for _, m := range pb.MediaSources[0].MediaStreams {
		if m.Type == "Subtitle" && m.Index == 2+pgs && m.DeliveryMethod != "Encode" {
			t.Fatalf("pgs delivery: %+v", m)
		}
	}
	var segs struct {
		Items []struct {
			Type       string
			StartTicks int64
		}
	}
	if s := c.call("GET", "/MediaSegments/"+id, nil, &segs); s != 200 || len(segs.Items) != 2 || segs.Items[0].Type != "Intro" ||
		segs.Items[1].Type != "Outro" || segs.Items[0].StartTicks != 5_000_000 {
		t.Fatalf("compat segments: %d %+v", s, segs)
	}
}
