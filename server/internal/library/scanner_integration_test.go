package library

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/streamero-app/streamero/server/internal/db"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
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
	// No TRUNCATE: other packages' integration tests use the same database in parallel;
	// every test works only with the libraries it creates.
	return pool
}

func touch(t *testing.T, root string, files ...string) {
	t.Helper()
	for _, f := range files {
		p := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

type fixture struct {
	pool    *pgxpool.Pool
	svc     *Service
	scanner *Scanner
	probes  atomic.Int32
}

func newFixture(t *testing.T, root string) *fixture {
	f := &fixture{pool: testPool(t)}
	f.scanner = &Scanner{Pool: f.pool, Roots: Roots{root}, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Probe: func(context.Context, string) (ProbeResult, error) {
			f.probes.Add(1)
			return ProbeResult{Container: "mkv", DurationMS: 5000, Width: 1920, Height: 1080, VideoCodec: "h264",
				Audio: []AudioStream{{Codec: "aac", Language: "eng", Channels: 2}}}, nil
		}}
	f.svc = &Service{Pool: f.pool, Roots: Roots{root}, Enqueue: func(context.Context, uuid.UUID) error { return nil }}
	return f
}

func (f *fixture) scan(t *testing.T, id uuid.UUID) ScanResult {
	t.Helper()
	res, err := f.scanner.Scan(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func titles(items []Item) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Title)
	}
	sort.Strings(out)
	return out
}

func TestMovieLibraryScanIntegration(t *testing.T) {
	root := t.TempDir()
	movies := filepath.Join(root, "movies")
	touch(t, movies,
		"Big Test Movie (2021)/Big Test Movie (2021).mkv",
		"Big Test Movie (2021)/Big Test Movie (2021) - 2160p.mkv",
		"Big Test Movie (2021)/Big Test Movie (2021).en.srt",
		"Big Test Movie (2021)/Big Test Movie (2021).hr.forced.srt",
		"Big Test Movie (2021)/trailers/Official Trailer.mp4",
		"Big Test Movie (2021)/Big Test Movie (2021)-sample.mkv",
		"Dune (2021) [tmdbid-438631]/Dune.2021.2160p.WEB-DL.mkv",
		"The.Dark.Knight.2008.1080p.BluRay.x264.mkv", // loose file in the library root
		".hidden/Secret (2000).mkv",
		"@eaDir/thumb.mkv",
	)
	f := newFixture(t, root)
	ctx := context.Background()
	lib, err := f.svc.Create(ctx, "Filmovi", "movies", []string{filepath.Join(root, "..", filepath.Base(root), "movies", "..", "movies")})
	if err != nil {
		t.Fatal(err) // ".." inside the root is fine after resolving
	}
	t.Cleanup(func() { _ = f.svc.Delete(context.Background(), lib.ID) })
	if _, err := f.svc.Create(ctx, "Escape", "movies", []string{filepath.Join(root, "..")}); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("folder outside the roots: %v", err)
	}
	if lib.ScanState != "queued" {
		t.Fatalf("state after create: %s", lib.ScanState)
	}

	res := f.scan(t, lib.ID)
	if res.Files != 4 || res.Probed != 4 {
		t.Fatalf("first scan: %+v", res)
	}
	items, total, err := f.svc.ListItems(ctx, lib.ID, uuid.Nil, "title", 100, 0)
	if err != nil || total != 3 {
		t.Fatalf("items: %d %v", total, err)
	}
	if got := titles(items); len(got) != 3 || got[0] != "Big Test Movie" || got[1] != "Dune" || got[2] != "The Dark Knight" {
		t.Fatalf("titles: %v", got)
	}
	var big Item
	for _, it := range items {
		if it.Title == "Big Test Movie" {
			big = it
		}
		if it.Title == "Dune" && it.ProviderIDs["tmdb"] != "438631" {
			t.Fatalf("dune ids: %+v", it.ProviderIDs)
		}
	}
	_, files, _, err := f.svc.GetItem(ctx, big.ID, uuid.Nil)
	if err != nil || len(files) != 2 {
		t.Fatalf("versions: %d %v", len(files), err)
	}
	var labels []string
	external := 0
	for _, fl := range files {
		labels = append(labels, fl.Label)
		for _, s := range fl.Subtitles {
			if s.External {
				external++
			}
		}
	}
	sort.Strings(labels)
	if labels[0] != "" || labels[1] != "2160p" || external != 4 { // both versions get both sidecar subtitles
		t.Fatalf("labels %v external subs %d", labels, external)
	}

	// Re-scan without changes probes nothing.
	if res := f.scan(t, lib.ID); res.Probed != 0 || res.Files != 4 {
		t.Fatalf("rescan: %+v", res)
	}
	// A changed file is probed again; a removed one disappears with its item.
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(movies, "Dune (2021) [tmdbid-438631]/Dune.2021.2160p.WEB-DL.mkv"), later, later); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(movies, "The.Dark.Knight.2008.1080p.BluRay.x264.mkv")); err != nil {
		t.Fatal(err)
	}
	if res := f.scan(t, lib.ID); res.Probed != 1 || res.Removed != 1 {
		t.Fatalf("after changes: %+v", res)
	}
	if _, total, _ := f.svc.ListItems(ctx, lib.ID, uuid.Nil, "title", 100, 0); total != 2 {
		t.Fatalf("items after removal: %d", total)
	}

	// An unavailable folder fails the scan and removes nothing.
	if err := os.Rename(movies, movies+"-offline"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.scanner.Scan(ctx, lib.ID); err == nil {
		t.Fatal("scan of a missing folder succeeded")
	}
	if _, total, _ := f.svc.ListItems(ctx, lib.ID, uuid.Nil, "title", 100, 0); total != 2 {
		t.Fatalf("items deleted while folder offline: %d", total)
	}
	if l, _ := f.svc.Get(ctx, lib.ID); l.ScanState != "failed" || l.ScanError == "" {
		t.Fatalf("library state: %+v", l)
	}
}

func TestShowLibraryScanIntegration(t *testing.T) {
	root := t.TempDir()
	shows := filepath.Join(root, "shows")
	touch(t, shows,
		"Test Show (2020)/Season 01/Test Show S01E01.mkv",
		"Test Show (2020)/Season 01/Test Show S01E02 - The Second One.mkv",
		"Test Show (2020)/Season 01/Test Show S01E05-E06.mkv",
		"Test Show (2020)/Season 02/Test Show S02E01.mkv",
		"Test Show (2020)/Specials/Test Show S00E01.mkv",
		"Daily Show/Daily Show 2024-03-15.mp4",
		"Anime Absolute/Season 03/[Group] Anime Absolute - 101 [1080p].mkv",
		"Not An Episode/random.mkv",
	)
	f := newFixture(t, root)
	ctx := context.Background()
	lib, err := f.svc.Create(ctx, "Serije", "shows", []string{shows})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.svc.Delete(context.Background(), lib.ID) })
	f.scan(t, lib.ID)
	series, total, _ := f.svc.ListItems(ctx, lib.ID, uuid.Nil, "title", 100, 0)
	if total != 3 {
		t.Fatalf("series: %v", titles(series))
	}
	for _, s := range series {
		_, _, seasons, err := f.svc.GetItem(ctx, s.ID, uuid.Nil)
		if err != nil {
			t.Fatal(err)
		}
		switch s.Title {
		case "Test Show":
			if s.Year == nil || *s.Year != 2020 || len(seasons) != 3 || *seasons[0].Season != 0 || seasons[0].Title != "Specials" {
				t.Fatalf("test show seasons: %+v", seasons)
			}
			_, _, eps, _ := f.svc.GetItem(ctx, seasons[1].ID, uuid.Nil)
			if len(eps) != 3 || eps[1].Title != "The Second One" || eps[2].EpisodeEnd == nil || *eps[2].EpisodeEnd != 6 {
				t.Fatalf("season 1 episodes: %+v", eps)
			}
		case "Daily Show":
			_, _, eps, _ := f.svc.GetItem(ctx, seasons[0].ID, uuid.Nil)
			if *seasons[0].Season != 2024 || len(eps) != 1 || eps[0].Title != "2024-03-15" {
				t.Fatalf("daily: %+v %+v", seasons, eps)
			}
		case "Anime Absolute":
			_, _, eps, _ := f.svc.GetItem(ctx, seasons[0].ID, uuid.Nil)
			if *seasons[0].Season != 3 || len(eps) != 1 || *eps[0].Episode != 101 {
				t.Fatalf("anime: %+v %+v", seasons, eps)
			}
		default:
			t.Fatalf("unexpected series %q", s.Title)
		}
	}
}

// Hot-path queries must be able to use an index (CLAUDE.md: EXPLAIN in tests).
func TestLibraryQueriesUseIndexes(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	// Tiny test tables make sequential scans cheapest; forbid them to see which index applies.
	if _, err := conn.Exec(ctx, `SET enable_seqscan = off`); err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	for query, index := range map[string]string{
		`SELECT id FROM items i WHERE i.library_id = $1 AND i.parent_id IS NULL ORDER BY i.sort_title LIMIT 100`:    "items_top_title_idx",
		`SELECT id FROM items i WHERE i.library_id = $1 AND i.parent_id IS NULL ORDER BY i.added_at DESC LIMIT 100`: "items_top_added_idx",
		`SELECT id FROM items i WHERE i.parent_id = $1 ORDER BY i.season_number, i.episode_number`:                  "items_parent_idx",
		`SELECT id FROM media_files WHERE item_id = $1`:                                                             "media_files_item_idx",
	} {
		var plan string
		rows, err := conn.Query(ctx, "EXPLAIN "+query, id)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var line string
			_ = rows.Scan(&line)
			plan += line + "\n"
		}
		rows.Close()
		if !strings.Contains(plan, index) {
			t.Errorf("%s\ndoes not use %s:\n%s", query, index, plan)
		}
	}
}

func TestWatcherQueuesScanOnNewFilesIntegration(t *testing.T) {
	root := t.TempDir()
	movies := filepath.Join(root, "movies")
	touch(t, movies, "Old (2000)/Old (2000).mkv")
	f := newFixture(t, root)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lib, err := f.svc.Create(ctx, "W", "movies", []string{movies})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.svc.Delete(context.Background(), lib.ID) })

	triggered := make(chan uuid.UUID, 10)
	w := &Watcher{Pool: f.pool, Roots: Roots{root}, Log: f.scanner.Log, Debounce: 300 * time.Millisecond, Refresh: time.Hour,
		Trigger: func(_ context.Context, id uuid.UUID) error { triggered <- id; return nil }}
	go func() { _ = w.Run(ctx) }()
	time.Sleep(500 * time.Millisecond) // watches installed

	// A new folder with a movie copied in (events in a fresh sub-folder must be seen too).
	if err := os.MkdirAll(filepath.Join(movies, "New (2024)"), 0o755); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	touch(t, movies, "New (2024)/New (2024).mkv")
	select {
	case id := <-triggered:
		if id != lib.ID {
			t.Fatalf("triggered %s, want %s", id, lib.ID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no scan queued after adding a file")
	}
	// Debounced: one trigger for the burst of events.
	select {
	case <-triggered:
		t.Fatal("more than one scan for one change")
	case <-time.After(800 * time.Millisecond):
	}
}

func TestIgnoreMarkerIntegration(t *testing.T) {
	root := t.TempDir()
	movies := filepath.Join(root, "movies")
	touch(t, movies, "Keep (2020)/Keep (2020).mkv", "Private (2021)/Private (2021).mkv", "Private (2021)/.streameroignore")
	f := newFixture(t, root)
	lib, err := f.svc.Create(context.Background(), "I", "movies", []string{movies})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.svc.Delete(context.Background(), lib.ID) })
	f.scan(t, lib.ID)
	items, _, _ := f.svc.ListItems(context.Background(), lib.ID, uuid.Nil, "title", 10, 0)
	if got := titles(items); len(got) != 1 || got[0] != "Keep" {
		t.Fatalf("ignored folder scanned: %v", got)
	}
}
