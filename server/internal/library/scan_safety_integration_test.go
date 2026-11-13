package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func (f *fixture) countItems(t *testing.T, libID uuid.UUID) (items, files int) {
	t.Helper()
	ctx := context.Background()
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM items WHERE library_id = $1`, libID).Scan(&items); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM media_files WHERE library_id = $1`, libID).Scan(&files); err != nil {
		t.Fatal(err)
	}
	return items, files
}

// A scan that would remove most of a folder (or all of it) keeps everything until an
// admin confirms; ordinary removals still happen (D-043).
func TestScanHoldsMassRemovalIntegration(t *testing.T) {
	root := t.TempDir()
	movies := filepath.Join(root, "movies")
	var names []string
	for i := range 25 {
		names = append(names, fmt.Sprintf("Movie %02d (2001)/Movie %02d (2001).mkv", i, i))
	}
	touch(t, movies, names...)
	f := newFixture(t, root)
	ctx := context.Background()
	lib, err := f.svc.Create(ctx, "Hold", "movies", []string{movies})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.svc.Delete(context.Background(), lib.ID) })
	f.scan(t, lib.ID)

	// A few files gone: removed as before.
	for i := range 2 {
		if err := os.RemoveAll(filepath.Join(movies, fmt.Sprintf("Movie %02d (2001)", i))); err != nil {
			t.Fatal(err)
		}
	}
	if res := f.scan(t, lib.ID); res.Removed != 2 || res.Held != 0 {
		t.Fatalf("small removal: %+v", res)
	}
	if items, files := f.countItems(t, lib.ID); items != 23 || files != 23 {
		t.Fatalf("after small removal: %d items %d files", items, files)
	}

	// 20 of 23 gone at once (a half-mounted share): nothing is removed.
	for i := 2; i < 22; i++ {
		if err := os.Rename(filepath.Join(movies, fmt.Sprintf("Movie %02d (2001)", i)), filepath.Join(root, fmt.Sprintf("away-%02d", i))); err != nil {
			t.Fatal(err)
		}
	}
	res := f.scan(t, lib.ID)
	if res.Removed != 0 || res.Held != 20 {
		t.Fatalf("mass removal: %+v", res)
	}
	if items, files := f.countItems(t, lib.ID); items != 23 || files != 23 {
		t.Fatalf("items removed while held: %d items %d files", items, files)
	}
	l, _ := f.svc.Get(ctx, lib.ID)
	if l.ScanState != "idle" || l.HeldRemovals != 20 || !strings.Contains(l.ScanError, "20 missing files") {
		t.Fatalf("library after held scan: %+v", l)
	}
	// New files are still added while removal is held.
	touch(t, movies, "Brand New (2024)/Brand New (2024).mkv")
	if res := f.scan(t, lib.ID); res.Held != 20 {
		t.Fatalf("second held scan: %+v", res)
	}
	if items, _ := f.countItems(t, lib.ID); items != 24 {
		t.Fatalf("new movie not added while held: %d", items)
	}

	// The admin confirms: the missing files go.
	res, err = f.scanner.ScanWith(ctx, lib.ID, ScanOptions{ConfirmRemoval: true})
	if err != nil || res.Removed != 20 || res.Held != 0 {
		t.Fatalf("confirmed scan: %+v %v", res, err)
	}
	if items, files := f.countItems(t, lib.ID); items != 4 || files != 4 {
		t.Fatalf("after confirmed removal: %d items %d files", items, files)
	}
	if l, _ := f.svc.Get(ctx, lib.ID); l.HeldRemovals != 0 || l.ScanError != "" {
		t.Fatalf("library after confirmed scan: %+v", l)
	}
}

// An empty library folder (an unmounted disk leaves the mount point behind) removes nothing,
// however small the library.
func TestScanOfEmptiedFolderRemovesNothingIntegration(t *testing.T) {
	root := t.TempDir()
	shows := filepath.Join(root, "shows")
	touch(t, shows, "Show (2020)/Season 01/Show S01E01.mkv", "Show (2020)/Season 01/Show S01E02.mkv")
	f := newFixture(t, root)
	ctx := context.Background()
	lib, err := f.svc.Create(ctx, "Empty", "shows", []string{shows})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.svc.Delete(context.Background(), lib.ID) })
	f.scan(t, lib.ID)
	if err := os.RemoveAll(filepath.Join(shows, "Show (2020)")); err != nil {
		t.Fatal(err)
	}
	if res := f.scan(t, lib.ID); res.Removed != 0 || res.Held != 2 {
		t.Fatalf("emptied folder: %+v", res)
	}
	if items, files := f.countItems(t, lib.ID); items != 4 || files != 2 {
		t.Fatalf("items removed from emptied folder: %d items %d files", items, files)
	}
}

// A sub-folder that cannot be read keeps its items (and their series and seasons).
func TestScanKeepsUnreadableFolderIntegration(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every folder")
	}
	root := t.TempDir()
	shows := filepath.Join(root, "shows")
	touch(t, shows,
		"Kept (2020)/Season 01/Kept S01E01.mkv",
		"Kept (2020)/Season 02/Kept S02E01.mkv",
		"Other (2021)/Season 01/Other S01E01.mkv",
		"Other (2021)/Season 01/Other S01E02.mkv",
		"Gone (2019)/Gone S01E01.mkv",
	)
	f := newFixture(t, root)
	ctx := context.Background()
	lib, err := f.svc.Create(ctx, "Locked", "shows", []string{shows})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.svc.Delete(context.Background(), lib.ID) })
	f.scan(t, lib.ID)
	before, beforeFiles := f.countItems(t, lib.ID)

	locked := filepath.Join(shows, "Kept (2020)", "Season 02")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	whole := filepath.Join(shows, "Other (2021)")
	if err := os.Chmod(whole, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(whole, 0o755) })
	if err := os.RemoveAll(filepath.Join(shows, "Gone (2019)")); err != nil {
		t.Fatal(err)
	}
	res := f.scan(t, lib.ID)
	if res.Removed != 1 || len(res.Unreadable) != 2 {
		t.Fatalf("scan with unreadable folders: %+v", res)
	}
	items, files := f.countItems(t, lib.ID)
	// "Gone" loses its series, season and episode; everything else stays.
	if items != before-3 || files != beforeFiles-1 {
		t.Fatalf("after scan: %d items (was %d), %d files (was %d)", items, before, files, beforeFiles)
	}
	if l, _ := f.svc.Get(ctx, lib.ID); l.ScanState != "idle" || !strings.Contains(l.ScanError, "could not be read") {
		t.Fatalf("library: %+v", l)
	}
}

// IDs added by the metadata refresh survive re-scans; IDs in the file name win.
func TestRescanKeepsProviderIDsIntegration(t *testing.T) {
	root := t.TempDir()
	movies := filepath.Join(root, "movies")
	touch(t, movies, "Plain (2020)/Plain (2020).mkv", "Named (2021) [tmdbid-222]/Named (2021).mkv")
	f := newFixture(t, root)
	ctx := context.Background()
	lib, err := f.svc.Create(ctx, "IDs", "movies", []string{movies})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.svc.Delete(context.Background(), lib.ID) })
	f.scan(t, lib.ID)
	// What the refresher stores.
	if _, err := f.pool.Exec(ctx, `UPDATE items SET provider_ids = '{"tmdb": "111", "imdb": "tt111"}', metadata_refreshed_at = now()
		WHERE library_id = $1`, lib.ID); err != nil {
		t.Fatal(err)
	}
	f.scan(t, lib.ID)
	ids := func(title string) map[string]string {
		var m map[string]string
		if err := f.pool.QueryRow(ctx, `SELECT provider_ids FROM items WHERE library_id = $1 AND title = $2`, lib.ID, title).Scan(&m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	if m := ids("Plain"); m["tmdb"] != "111" || m["imdb"] != "tt111" {
		t.Fatalf("refreshed IDs lost on re-scan: %v", m)
	}
	if m := ids("Named"); m["tmdb"] != "222" || m["imdb"] != "tt111" {
		t.Fatalf("file-name ID must win and others stay: %v", m)
	}
	// Unchanged items are not written again (the merge result is stable).
	var updated time.Time
	_ = f.pool.QueryRow(ctx, `SELECT max(updated_at) FROM items WHERE library_id = $1`, lib.ID).Scan(&updated)
	f.scan(t, lib.ID)
	var again time.Time
	_ = f.pool.QueryRow(ctx, `SELECT max(updated_at) FROM items WHERE library_id = $1`, lib.ID).Scan(&again)
	if !again.Equal(updated) {
		t.Fatalf("unchanged items written again: %v → %v", updated, again)
	}
}

// A failed probe is retried on a later scan (at most hourly) and a transient failure
// does not wipe what an earlier probe found.
func TestProbeRetryIntegration(t *testing.T) {
	root := t.TempDir()
	movies := filepath.Join(root, "movies")
	touch(t, movies, "A (2020)/A (2020).mkv", "B (2021)/B (2021).mkv")
	f := newFixture(t, root)
	var failing atomic.Value
	failing.Store("A (2020).mkv")
	ok := f.scanner.Probe
	f.scanner.Probe = func(ctx context.Context, p string) (ProbeResult, error) {
		if filepath.Base(p) == failing.Load().(string) {
			f.probes.Add(1)
			return ProbeResult{}, errors.New("share busy")
		}
		return ok(ctx, p)
	}
	ctx := context.Background()
	lib, err := f.svc.Create(ctx, "Probe", "movies", []string{movies})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.svc.Delete(context.Background(), lib.ID) })
	f.scan(t, lib.ID)
	state := func(name string) (probeErr *string, width *int) {
		if err := f.pool.QueryRow(ctx, `SELECT probe_error, width FROM media_files WHERE library_id = $1 AND path LIKE '%/' || $2`,
			lib.ID, name).Scan(&probeErr, &width); err != nil {
			t.Fatal(err)
		}
		return
	}
	if e, _ := state("A (2020).mkv"); e == nil {
		t.Fatal("probe error not recorded")
	}
	// Right away: not probed again.
	if res := f.scan(t, lib.ID); res.Probed != 0 {
		t.Fatalf("failed probe retried too soon: %+v", res)
	}
	// An hour later (and the share is back): probed again and fixed.
	failing.Store("")
	if _, err := f.pool.Exec(ctx, `UPDATE media_files SET probed_at = now() - interval '2 hours' WHERE library_id = $1`, lib.ID); err != nil {
		t.Fatal(err)
	}
	if res := f.scan(t, lib.ID); res.Probed != 1 {
		t.Fatalf("failed probe not retried: %+v", res)
	}
	if e, w := state("A (2020).mkv"); e != nil || w == nil || *w != 1920 {
		t.Fatalf("after retry: %v %v", e, w)
	}

	// B changes on disk but its probe fails: the earlier stream details stay.
	failing.Store("B (2021).mkv")
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(movies, "B (2021)/B (2021).mkv"), later, later); err != nil {
		t.Fatal(err)
	}
	f.scan(t, lib.ID)
	if e, w := state("B (2021).mkv"); e == nil || w == nil || *w != 1920 {
		t.Fatalf("transient failure wiped probe data: %v %v", e, w)
	}
}

// Next Up looks a series' episodes up by parent (index), not by scanning all episodes.
func TestNextUpEpisodesUseParentIndex(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	id := uuid.New()
	if _, err := conn.Exec(ctx, `INSERT INTO libraries (id, name, kind, paths) VALUES ($1, 'explain-nextup', 'shows', '{/y}')`, id); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(context.Background(), `DELETE FROM libraries WHERE id = $1`, id) }()
	// 200 series × 5 seasons × 10 episodes: realistic statistics for the planner.
	for _, q := range []string{
		`INSERT INTO items (id, library_id, kind, path, title, sort_title)
			SELECT gen_random_uuid(), $1, 'series', '/y/' || g, 'S' || g, 's' || g FROM generate_series(1, 200) g`,
		`INSERT INTO items (id, library_id, parent_id, kind, path, title, sort_title, season_number)
			SELECT gen_random_uuid(), $1, s.id, 'season', s.path || '#season-' || n, 'Season ' || n, 'season ' || n, n
			FROM items s, generate_series(1, 5) n WHERE s.library_id = $1 AND s.kind = 'series'`,
		`INSERT INTO items (id, library_id, parent_id, kind, path, title, sort_title, season_number, episode_number)
			SELECT gen_random_uuid(), $1, se.id, 'episode', se.path || '/e' || n, 'E' || n, 'e' || n, se.season_number, n
			FROM items se, generate_series(1, 10) n WHERE se.library_id = $1 AND se.kind = 'season'`,
	} {
		if _, err := conn.Exec(ctx, q, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := conn.Exec(ctx, `ANALYZE items`); err != nil {
		t.Fatal(err)
	}
	var series uuid.UUID
	if err := conn.QueryRow(ctx, `SELECT id FROM items WHERE library_id = $1 AND kind = 'series' LIMIT 1`, id).Scan(&series); err != nil {
		t.Fatal(err)
	}
	query := `SELECT e.id FROM (` + episodesOf("$1::uuid") + `) e
		WHERE (COALESCE(e.season_number, 1), e.episode_number) > (1, 3)
		ORDER BY COALESCE(e.season_number, 1), e.episode_number LIMIT 1`
	rows, err := conn.Query(ctx, "EXPLAIN "+query, series)
	if err != nil {
		t.Fatal(err)
	}
	var plan string
	for rows.Next() {
		var line string
		_ = rows.Scan(&line)
		plan += line + "\n"
	}
	rows.Close()
	if !strings.Contains(plan, "items_parent_idx") || strings.Contains(plan, "Seq Scan on items") {
		t.Fatalf("next-up episodes do not use items_parent_idx:\n%s", plan)
	}
	var next string
	if err := conn.QueryRow(ctx, `SELECT i.title FROM items i WHERE i.id = (`+query+`)`, series).Scan(&next); err != nil || next != "E4" {
		t.Fatalf("next episode: %q %v", next, err)
	}
}

// The watcher walks a library folder once, not on every reload of the library list.
func TestWatcherWalksRootsOnceIntegration(t *testing.T) {
	root := t.TempDir()
	movies := filepath.Join(root, "movies")
	touch(t, movies, "Old (2000)/Old (2000).mkv")
	f := newFixture(t, root)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lib, err := f.svc.Create(ctx, "WalkOnce", "movies", []string{movies})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.svc.Delete(context.Background(), lib.ID) })
	var walks atomic.Int32
	w := &Watcher{Pool: f.pool, Roots: Roots{root}, Log: f.scanner.Log, Debounce: time.Hour, Refresh: 50 * time.Millisecond,
		Trigger: func(context.Context, uuid.UUID) error { return nil },
		onWalk: func(r string) {
			if r == movies {
				walks.Add(1)
			}
		}}
	go func() { _ = w.Run(ctx) }()
	time.Sleep(600 * time.Millisecond) // about ten reloads
	if n := walks.Load(); n != 1 {
		t.Fatalf("library folder walked %d times, want 1", n)
	}
}
