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

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/db"
	"github.com/streamero-app/streamero/server/internal/library"
)

// episode writes a short episode: noise, a shared rising tone ("theme song"), noise.
func episode(t *testing.T, ffmpeg, path string, coldOpen, seed int, chapters string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	args := []string{"-v", "error", "-y",
		"-f", "lavfi", "-i", melody(coldOpen, seed),
		"-f", "lavfi", "-i", "aevalsrc=0.6*sin(2*PI*(220+40*t)*t)+0.3*sin(2*PI*(660-15*t)*t):s=44100:d=30",
		"-f", "lavfi", "-i", melody(200, seed+50),
		"-f", "lavfi", "-i", "testsrc2=size=320x180:rate=5:d=" + itoa(coldOpen+230)}
	if chapters != "" {
		meta := path + ".meta"
		if err := os.WriteFile(meta, []byte(chapters), 0o644); err != nil {
			t.Fatal(err)
		}
		args = append(args, "-f", "ffmetadata", "-i", meta, "-map_chapters", "4")
	}
	args = append(args, "-filter_complex", "[0][1][2]concat=n=3:v=0:a=1[a]", "-map", "3:v", "-map", "[a]",
		"-c:v", "libx264", "-preset", "ultrafast", "-g", "25", "-c:a", "aac", "-shortest", path)
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

func TestAnalyzerIntegration(t *testing.T) {
	ffmpeg, err1 := exec.LookPath("ffmpeg")
	ffprobe, err2 := exec.LookPath("ffprobe")
	if err1 != nil || err2 != nil {
		t.Skip("ffmpeg/ffprobe not installed")
	}
	if out, _ := exec.Command(ffmpeg, "-hide_banner", "-muxers").Output(); !strings.Contains(string(out), "chromaprint") {
		t.Skip("ffmpeg without chromaprint")
	}
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
	episode(t, ffmpeg, filepath.Join(show, "Tone Show S01E01.mkv"), 20, 1,
		";FFMETADATA1\n[CHAPTER]\nTIMEBASE=1/1000\nSTART=0\nEND=20000\ntitle=Cold open\n[CHAPTER]\nTIMEBASE=1/1000\nSTART=20000\nEND=235000\ntitle=Part A\n[CHAPTER]\nTIMEBASE=1/1000\nSTART=235000\nEND=250000\ntitle=End Credits\n")
	episode(t, ffmpeg, filepath.Join(show, "Tone Show S01E02.mkv"), 45, 7, "")

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
	cache := t.TempDir()
	a := &Analyzer{Pool: pool, FFmpeg: ffmpeg, FFprobe: ffprobe, CacheDir: cache, Trickplay: true, Log: log,
		ResolvePath: func(p string) (string, error) { return library.ResolveSafePath(roots, p) }}
	n, err := a.Run(ctx, lib.ID)
	if err != nil || n != 2 {
		t.Fatalf("run: %d %v", n, err)
	}

	type seg struct {
		file, kind, source string
		start, end         int64
	}
	rows, err := pool.Query(ctx, `SELECT f.path, s.kind, s.source, s.start_ms, s.end_ms FROM media_segments s
		JOIN media_files f ON f.id = s.file_id WHERE f.library_id = $1 ORDER BY f.path, s.kind`, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	var segs []seg
	for rows.Next() {
		var s seg
		if err := rows.Scan(&s.file, &s.kind, &s.source, &s.start, &s.end); err != nil {
			t.Fatal(err)
		}
		s.file = filepath.Base(s.file)
		segs = append(segs, s)
	}
	rows.Close()
	near := func(got, want int64) bool { return got > want-2500 && got < want+2500 }
	byKey := map[string]seg{}
	for _, s := range segs {
		byKey[s.file+"/"+s.kind] = s
	}
	if s, ok := byKey["Tone Show S01E01.mkv/intro"]; !ok || s.source != "fingerprint" || !near(s.start, 20_000) || !near(s.end, 50_000) {
		t.Errorf("E01 intro: %+v (all %+v)", s, segs)
	}
	if s, ok := byKey["Tone Show S01E02.mkv/intro"]; !ok || !near(s.start, 45_000) || !near(s.end, 75_000) {
		t.Errorf("E02 intro: %+v (all %+v)", s, segs)
	}
	if s, ok := byKey["Tone Show S01E01.mkv/credits"]; !ok || s.source != "chapter" || s.start != 235_000 {
		t.Errorf("E01 credits from chapter: %+v", s)
	}

	var chapters, trick []byte
	if err := pool.QueryRow(ctx, `SELECT chapters, trickplay FROM media_files WHERE library_id = $1 AND path LIKE '%E01.mkv'`, lib.ID).
		Scan(&chapters, &trick); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(chapters), "End Credits") || !strings.Contains(strings.ReplaceAll(string(trick), " ", ""), `"intervalMs":10000`) {
		t.Fatalf("chapters %s trickplay %s", chapters, trick)
	}
	if sheets, _ := filepath.Glob(filepath.Join(cache, "trickplay", "*", "0.jpg")); len(sheets) != 2 {
		t.Fatalf("trickplay sheets: %v", sheets)
	}
	// Analyzed once: nothing left on a second run.
	if n, _ := a.Run(ctx, lib.ID); n != 0 {
		t.Fatalf("second run analyzed %d", n)
	}
}
