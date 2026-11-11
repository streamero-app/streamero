package stream

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/testmedia"
)

// Test subtitles draw a white 100×40 box at (110, 180) of a black 320×240 picture
// from 1 s to 4 s.
const boxX, boxY, boxW, boxH = 110, 180, 100, 40

// pgsBox writes a .sup file showing the test box from 1 s to 4 s.
func pgsBox(t *testing.T, path string) {
	t.Helper()
	sup := testmedia.PGSBox(320, 240, testmedia.Box{X: boxX, Y: boxY, W: boxW, H: boxH}, 1000, 4000)
	if err := os.WriteFile(path, sup, 0o600); err != nil {
		t.Fatal(err)
	}
}

// assBox is an ASS script drawing the box as a vector shape (no font needed);
// plain-text conversion would lose it, so a visible box proves libass styling.
const assBox = `[Script Info]
ScriptType: v4.00+
PlayResX: 320
PlayResY: 240

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: Default,Sans,20,&H00FFFFFF,&H00FFFFFF,&H00FFFFFF,&H00000000,0,0,0,0,100,100,0,0,1,0,0,7,0,0,0,1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:01.00,0:00:04.00,Default,,0,0,0,,{\an7\pos(110,180)\p1}m 0 0 l 100 0 100 40 0 40{\p0}
`

func ffmpegOrSkip(t *testing.T) string {
	t.Helper()
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	out, _ := exec.Command(ff, "-hide_banner", "-filters").Output()
	if !strings.Contains(string(out), " subtitles ") || !strings.Contains(string(out), " overlay ") {
		t.Skip("ffmpeg without libass/overlay")
	}
	return ff
}

// brightness returns the mean luma of the box area at second sec of an HLS session.
func brightness(t *testing.T, ff, dir string, sec float64) float64 {
	t.Helper()
	// fMP4 HLS: the init segment followed by the media segments is one playable file.
	segs, _ := filepath.Glob(filepath.Join(dir, "seg_*.m4s"))
	init, err := os.ReadFile(filepath.Join(dir, "init.mp4")) // #nosec G304 -- test directory
	if err != nil {
		t.Fatal(err)
	}
	whole := init
	for _, s := range segs {
		b, err := os.ReadFile(s) // #nosec G304 -- test directory
		if err != nil {
			t.Fatal(err)
		}
		whole = append(whole, b...)
	}
	joined := filepath.Join(dir, "joined.mp4")
	if err := os.WriteFile(joined, whole, 0o600); err != nil {
		t.Fatal(err)
	}
	crop := "crop=" + strconv.Itoa(boxW-20) + ":" + strconv.Itoa(boxH-10) + ":" + strconv.Itoa(boxX+10) + ":" + strconv.Itoa(boxY+5)
	out, err := exec.Command(ff, "-v", "error", "-ss", strconv.FormatFloat(sec, 'f', 2, 64), "-i", joined,
		"-frames:v", "1", "-vf", crop, "-f", "rawvideo", "-pix_fmt", "gray", "-").Output()
	if err != nil || len(out) == 0 {
		t.Fatalf("frame at %.1f: %v", sec, err)
	}
	sum := 0
	for _, v := range out {
		sum += int(v)
	}
	return float64(sum) / float64(len(out))
}

func TestBurnInWithFFmpeg(t *testing.T) {
	ff := ffmpegOrSkip(t)
	work := t.TempDir()
	sup, ass, mkv := filepath.Join(work, "film.sup"), filepath.Join(work, "film.ass"), filepath.Join(work, "film.mkv")
	pgsBox(t, sup)
	if err := os.WriteFile(ass, []byte(assBox), 0o600); err != nil {
		t.Fatal(err)
	}
	// Black picture with an embedded PGS track (stream 0) and ASS track (stream 1).
	if out, err := exec.Command(ff, "-v", "error", "-y", "-f", "lavfi", "-i", "color=black:s=320x240:r=10:d=6",
		"-i", sup, "-i", ass, "-map", "0:v", "-map", "1:s", "-map", "2:s", "-c:v", "libx264", "-preset", "ultrafast",
		"-g", "10", "-c:s", "copy", mkv).CombinedOutput(); err != nil {
		t.Fatalf("make mkv: %v %s", err, out)
	}
	f := library.MediaFile{Container: "mkv", VideoCodec: "h264", Height: intp(240), Subtitles: []library.SubtitleStream{
		{Codec: "hdmv_pgs_subtitle"}, {Codec: "ass"},
		{Codec: "sup", External: true, Path: sup}, {Codec: "ass", External: true, Path: ass},
	}}
	web := Device{Containers: []string{"mp4"}, VideoCodecs: []string{"h264"}, HLS: true}
	m := &Manager{Dir: t.TempDir(), FFmpeg: ff, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	enc := DefaultEncoding()
	enc.SegmentSeconds = 1
	m.Encoding = func(context.Context) Encoding { return enc }

	cases := []struct {
		name    string
		sub     int
		force   bool
		startMS int64
	}{
		{"embedded pgs", 0, false, 0},
		{"embedded pgs after seek", 0, false, 1_500},
		{"embedded ass", 1, true, 0},
		{"embedded ass after seek", 1, true, 1_500},
		{"sidecar sup", 2, false, 0},
		{"sidecar sup after seek", 2, false, 1_500},
		{"sidecar ass after seek", 3, true, 1_500},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			plan := DecideWithSubtitle(f, -1, c.sub, c.force, web)
			if plan.Burn == nil {
				t.Fatalf("no burn-in: %+v", plan)
			}
			plan.Burn.Path = f.Subtitles[c.sub].Path
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			id, err := m.Start(ctx, Job{Input: mkv, StartMS: c.startMS, AudioIndex: -1, Plan: plan})
			if err != nil {
				t.Fatal(err)
			}
			m.mu.Lock()
			s := m.streams[id]
			m.mu.Unlock()
			select {
			case <-s.done:
			case <-ctx.Done():
				t.Fatal("transcode did not finish")
			}
			// Positions in the file: the box shows from 1 s to 4 s.
			on := 2.5 - float64(c.startMS)/1000
			off := 4.8 - float64(c.startMS)/1000
			if b := brightness(t, ff, s.dir, on); b < 150 {
				t.Errorf("subtitle missing at file time 2.5 s: luma %.0f", b)
			}
			if b := brightness(t, ff, s.dir, off); b > 40 {
				t.Errorf("subtitle still shown at file time 4.8 s: luma %.0f", b)
			}
			m.stop(id)
		})
	}
}
