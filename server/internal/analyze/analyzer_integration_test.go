package analyze

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

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/db"
	"github.com/streamero-app/streamero/server/internal/library"
)

// Synthetic episodes: a cold open, a shared 30 s theme ("intro tone"), 90 s of
// episode-specific sound, then 25 s of end credits: either a shared end song over
// the picture, or episode-specific sound over a black screen.
const (
	themeSec   = 30
	bodySec    = 90
	creditsSec = 25
)

func episodeSeconds(coldOpen int) int { return coldOpen + themeSec + bodySec + creditsSec }

func seasonEpisode(t *testing.T, ffmpeg, path string, coldOpen, seed int, blackCredits bool, chapters string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	ending := "aevalsrc=0.6*sin(2*PI*(880-20*t)*t)+0.3*sin(2*PI*(330+12*t)*t):s=44100:d=" + itoa(creditsSec)
	endPicture := "testsrc2=size=320x180:rate=5:d=" + itoa(creditsSec)
	if blackCredits {
		ending = melody(creditsSec, seed+90)
		endPicture = "color=black:size=320x180:rate=5:d=" + itoa(creditsSec)
	}
	args := []string{"-v", "error", "-y",
		"-f", "lavfi", "-i", melody(coldOpen, seed),
		"-f", "lavfi", "-i", "aevalsrc=0.6*sin(2*PI*(220+40*t)*t)+0.3*sin(2*PI*(660-15*t)*t):s=44100:d=" + itoa(themeSec),
		"-f", "lavfi", "-i", melody(bodySec, seed+50),
		"-f", "lavfi", "-i", ending,
		"-f", "lavfi", "-i", "testsrc2=size=320x180:rate=5:d=" + itoa(coldOpen+themeSec+bodySec),
		"-f", "lavfi", "-i", endPicture}
	if chapters != "" {
		meta := path + ".meta"
		if err := os.WriteFile(meta, []byte(chapters), 0o644); err != nil {
			t.Fatal(err)
		}
		args = append(args, "-f", "ffmetadata", "-i", meta, "-map_chapters", "6")
	}
	args = append(args, "-filter_complex", "[0][1][2][3]concat=n=4:v=0:a=1[a];[4][5]concat=n=2:v=1:a=0[v]", "-map", "[v]", "-map", "[a]",
		"-c:v", "libx264", "-preset", "ultrafast", "-g", "25", "-c:a", "aac", path)
	if out, err := exec.Command(ffmpeg, args...).CombinedOutput(); err != nil {
		t.Fatalf("make %s: %v %s", path, err, out)
	}
}

// melody is episode-specific "dialogue/score": notes that change twice a second,
// in a sequence that depends on seed.
func melody(seconds, seed int) string {
	return "aevalsrc=0.5*sin(2*PI*(180+" + itoa(23+seed*7) + "*mod(floor(t*2)*" + itoa(seed*3+5) + "\\," + itoa(11+seed%5) + "))*t):s=44100:d=" + itoa(seconds)
}

func itoa(n int) string { return strconv.Itoa(n) }

func toolsOrSkip(t *testing.T) (ffmpeg, ffprobe, fpcalc string) {
	t.Helper()
	var err error
	if ffmpeg, err = exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	if ffprobe, err = exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed")
	}
	if fpcalc, err = exec.LookPath("fpcalc"); err != nil {
		t.Skip("fpcalc (libchromaprint-tools) not installed")
	}
	return ffmpeg, ffprobe, fpcalc
}

func nearMS(got, want int64) bool { return got > want-2500 && got < want+2500 }

// TestDetectSyntheticSeason fingerprints a generated season with fpcalc and finds
// the shared intro, the shared end song and, for the episode without it, the
// credits from black frames.
func TestDetectSyntheticSeason(t *testing.T) {
	ffmpeg, _, fpcalc := toolsOrSkip(t)
	dir := t.TempDir()
	colds := []int{4, 9, 14, 6}
	d := &Detector{FFmpeg: ffmpeg, Fpcalc: fpcalc}
	var eps []Prints
	for i, c := range colds {
		path := filepath.Join(dir, "e"+itoa(i)+".mkv")
		seasonEpisode(t, ffmpeg, path, c, i*6+1, i == 3, "")
		dur := int64(episodeSeconds(c)) * 1000
		introSec, endSec := d.windows(dur)
		p := Prints{DurationMS: dur, CreditsOffsetMS: dur - secMS(endSec)}
		p.Intro = d.fingerprint(context.Background(), path, 0, introSec)
		p.Credits = d.fingerprint(context.Background(), path, float64(p.CreditsOffsetMS)/1000, endSec)
		if len(p.Intro) < 100 || len(p.Credits) < 100 {
			t.Fatalf("episode %d prints: %d %d", i, len(p.Intro), len(p.Credits))
		}
		eps = append(eps, p)
	}
	got := MatchSeason(eps)
	for i, c := range colds {
		dur := int64(episodeSeconds(c)) * 1000
		if in := got[i].Intro; in == nil || !nearMS(in.StartMS, int64(c)*1000) || !nearMS(in.EndMS, int64(c+themeSec)*1000) {
			t.Errorf("episode %d intro %+v, want %d..%d s", i, in, c, c+themeSec)
		}
		cr := got[i].Credits
		if i == 3 {
			if cr != nil {
				t.Errorf("episode 3 has no shared end song, got %+v", cr)
			}
			src := filepath.Join(dir, "e3.mkv")
			_, endSec := d.windows(dur)
			offset := dur - secMS(endSec)
			c, ok := CreditsFromBlack(d.blackdetect(context.Background(), src, offset, endSec), dur)
			if !ok || !nearMS(c.StartMS, dur-creditsSec*1000) || c.EndMS != dur {
				t.Errorf("black-frame credits %+v %v, want from %d", c, ok, dur-creditsSec*1000)
			}
			continue
		}
		if cr == nil || !nearMS(cr.StartMS, dur-creditsSec*1000) || cr.EndMS != dur {
			t.Errorf("episode %d credits %+v, want %d..%d", i, cr, dur-creditsSec*1000, dur)
		}
	}
}

func TestAnalyzeAndDetectIntegration(t *testing.T) {
	ffmpeg, ffprobe, fpcalc := toolsOrSkip(t)
	url := os.Getenv("STREAMERO_TEST_DB_URL")
	if url == "" {
		t.Skip("STREAMERO_TEST_DB_URL not set")
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	show := filepath.Join(root, "Shows", "Tone Show (2020)", "Season 01")
	ep := func(n int) string { return filepath.Join(show, "Tone Show S01E0"+itoa(n)+".mkv") }
	// E01 names its credits by chapter (from 40 s before the end): the chapter wins.
	e1End := episodeSeconds(4) * 1000
	seasonEpisode(t, ffmpeg, ep(1), 4, 1, false, ";FFMETADATA1\n[CHAPTER]\nTIMEBASE=1/1000\nSTART=0\nEND="+itoa(e1End-40_000)+
		"\ntitle=Part A\n[CHAPTER]\nTIMEBASE=1/1000\nSTART="+itoa(e1End-40_000)+"\nEND="+itoa(e1End)+"\ntitle=End Credits\n")
	seasonEpisode(t, ffmpeg, ep(2), 9, 7, false, "")
	seasonEpisode(t, ffmpeg, ep(3), 14, 13, true, "")

	roots := library.Roots{root}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := &library.Service{Pool: pool, Roots: roots, Enqueue: func(context.Context, uuid.UUID) error { return nil }}
	lib, err := svc.Create(ctx, "Tones", "shows", []string{filepath.Join(root, "Shows")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Delete(context.Background(), lib.ID) })
	scanner := &library.Scanner{Pool: pool, Roots: roots, Probe: library.FFprobe(ffprobe), Log: log}
	if _, err := scanner.Scan(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}
	resolve := func(p string) (string, error) { return library.ResolveSafePath(roots, p) }
	cache := t.TempDir()
	a := &Analyzer{Pool: pool, FFmpeg: ffmpeg, FFprobe: ffprobe, CacheDir: cache, Trickplay: true, Log: log, ResolvePath: resolve}
	if n, err := a.Run(ctx, lib.ID); err != nil || n != 3 {
		t.Fatalf("analyze: %d %v", n, err)
	}
	// A manual edit (the future segment editor) is never replaced.
	if _, err := pool.Exec(ctx, `INSERT INTO media_segments (file_id, kind, start_ms, end_ms, source)
		SELECT id, 'intro', 1000, 2000, 'manual' FROM media_files WHERE path = $1`, ep(2)); err != nil {
		t.Fatal(err)
	}
	d := &Detector{Pool: pool, FFmpeg: ffmpeg, Fpcalc: fpcalc, Log: log, ResolvePath: resolve}
	if n, err := d.Run(ctx, lib.ID); err != nil || n != 3 {
		t.Fatalf("detect: %d %v", n, err)
	}

	type seg struct {
		source     string
		start, end int64
	}
	segments := func() map[string]seg {
		rows, err := pool.Query(ctx, `SELECT f.path, s.kind, s.source, s.start_ms, s.end_ms FROM media_segments s
			JOIN media_files f ON f.id = s.file_id WHERE f.library_id = $1`, lib.ID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		out := map[string]seg{}
		for rows.Next() {
			var path, kind string
			var s seg
			if err := rows.Scan(&path, &kind, &s.source, &s.start, &s.end); err != nil {
				t.Fatal(err)
			}
			out[strings.TrimSuffix(strings.TrimPrefix(filepath.Base(path), "Tone Show S01"), ".mkv")+"/"+kind] = s
		}
		return out
	}
	expect := func(key, source string, start, end int64) {
		t.Helper()
		s, ok := segments()[key]
		if !ok || s.source != source || !nearMS(s.start, start) || !nearMS(s.end, end) {
			t.Errorf("%s: %+v (want %s %d..%d; all %+v)", key, s, source, start, end, segments())
		}
	}
	dur := func(cold int) int64 { return int64(episodeSeconds(cold)) * 1000 }
	expect("E01/intro", "fingerprint", 4_000, 34_000)
	expect("E01/credits", "chapter", dur(4)-40_000, dur(4))
	expect("E02/intro", "manual", 1000, 2000)
	expect("E02/credits", "fingerprint", dur(9)-creditsSec*1000, dur(9))
	expect("E03/intro", "fingerprint", 14_000, 44_000)
	expect("E03/credits", "blackframe", dur(14)-creditsSec*1000, dur(14))

	// Incremental: nothing new, nothing decoded.
	if n, err := d.Run(ctx, lib.ID); err != nil || n != 0 {
		t.Fatalf("second detect: %d %v", n, err)
	}
	// A new episode is fingerprinted alone and compared with the stored prints.
	seasonEpisode(t, ffmpeg, ep(4), 6, 19, false, "")
	// A replaced file counts as new.
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(ep(3), later, later); err != nil {
		t.Fatal(err)
	}
	if _, err := scanner.Scan(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := d.Run(ctx, lib.ID); err != nil || n != 2 {
		t.Fatalf("detect after new episode: %d %v", n, err)
	}
	expect("E04/intro", "fingerprint", 6_000, 36_000)
	expect("E04/credits", "fingerprint", dur(6)-creditsSec*1000, dur(6))
	expect("E03/credits", "blackframe", dur(14)-creditsSec*1000, dur(14))
	var prints int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM media_fingerprints fp JOIN media_files f ON f.id = fp.file_id
		WHERE f.library_id = $1 AND fp.size = f.size AND fp.mtime = f.mtime`, lib.ID).Scan(&prints); err != nil || prints != 4 {
		t.Fatalf("fresh fingerprints: %d %v", prints, err)
	}

	// Chapters and trickplay still come from the analyzer.
	var chapters, trick []byte
	if err := pool.QueryRow(ctx, `SELECT chapters, trickplay FROM media_files WHERE library_id = $1 AND path LIKE '%E01.mkv'`, lib.ID).
		Scan(&chapters, &trick); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(chapters), "End Credits") || !strings.Contains(strings.ReplaceAll(string(trick), " ", ""), `"intervalMs":10000`) {
		t.Fatalf("chapters %s trickplay %s", chapters, trick)
	}
	if n, _ := a.Run(ctx, lib.ID); n != 2 { // E04 and the replaced E03
		t.Fatalf("analyzer after rescan: %d", n)
	}
}
