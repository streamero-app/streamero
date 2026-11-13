package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Scanner turns library folders into items and media files.
type Scanner struct {
	Pool        *pgxpool.Pool
	Roots       Roots
	Probe       Prober
	Log         *slog.Logger
	Concurrency int // parallel ffprobe runs (default 4)
}

type planFile struct {
	path  string
	size  int64
	mtime time.Time
	label string
	subs  []SubtitleStream
}

type planItem struct {
	key, kind, parentKey string
	title                string
	year                 int
	season, ep, epEnd    *int
	ids                  map[string]string
	files                []planFile
	order                int        // parents (series, seasons) before children
	artist               string     // tracks by someone other than the album artist
	meta                 *MediaMeta // books, audiobooks, photos: what the file says about itself
	takenAt              *time.Time // photos and videos
}

// ScanResult summarises a scan.
type ScanResult struct {
	Files, Probed, Removed int
	// Held counts files that were not found but kept because removing them looked
	// unsafe (an offline or half-mounted folder); see removalHeld.
	Held int
	// Unreadable lists folders and files that could not be read; nothing under them
	// is removed.
	Unreadable []string
}

// ScanOptions change how a scan treats files it did not find.
type ScanOptions struct {
	// ConfirmRemoval removes missing files even when that looks like an offline
	// folder (the admin confirmed it). Unreadable folders are still kept.
	ConfirmRemoval bool
}

// Removal safety (D-043): a scan that would remove more than half of the known files
// under a library folder (and at least minHeldRemoval of them), or every file under
// it, removes nothing and keeps the library as it was until an admin confirms.
const minHeldRemoval = 20

// Scan rescans one library. If any library folder cannot be read the scan fails
// without removing anything (an unmounted disk must not empty the library).
func (s *Scanner) Scan(ctx context.Context, libraryID uuid.UUID) (ScanResult, error) {
	return s.ScanWith(ctx, libraryID, ScanOptions{})
}

// ScanWith rescans one library with options.
func (s *Scanner) ScanWith(ctx context.Context, libraryID uuid.UUID, opts ScanOptions) (ScanResult, error) {
	var kind string
	var paths []string
	if err := s.Pool.QueryRow(ctx, `UPDATE libraries SET scan_state = 'scanning', scan_error = NULL WHERE id = $1
		RETURNING kind, paths`, libraryID).Scan(&kind, &paths); err != nil {
		return ScanResult{}, err
	}
	res, err := s.scan(ctx, libraryID, kind, paths, opts)
	if err != nil {
		_, _ = s.Pool.Exec(context.WithoutCancel(ctx), `UPDATE libraries SET scan_state = 'failed', scan_error = $2 WHERE id = $1`,
			libraryID, err.Error())
		return res, err
	}
	var warning *string
	switch {
	case res.Held > 0:
		msg := fmt.Sprintf("%d missing files were kept: a library folder looks offline or mostly empty. Scan again with removal confirmed if they were deleted on purpose.", res.Held)
		warning = &msg
		s.Log.Warn("scan kept missing files: the removal looks like an offline folder", "library", libraryID, "files", res.Held)
	case len(res.Unreadable) > 0:
		msg := fmt.Sprintf("%d folders or files could not be read (first: %s); their items were kept.", len(res.Unreadable), res.Unreadable[0])
		warning = &msg
	}
	if len(res.Unreadable) > 0 {
		s.Log.Warn("scan could not read some folders; items under them were kept", "library", libraryID,
			"count", len(res.Unreadable), "first", res.Unreadable[0])
	}
	_, err = s.Pool.Exec(ctx, `UPDATE libraries SET scan_state = 'idle', last_scan_at = now(), files_seen = $2,
		scan_error = $3, held_removals = $4 WHERE id = $1`, libraryID, res.Files, warning, res.Held)
	return res, err
}

// walkResult is what walking the library folders found besides the plan.
type walkResult struct {
	roots      []string
	unreadable []string
}

func (s *Scanner) scan(ctx context.Context, libID uuid.UUID, kind string, paths []string, opts ScanOptions) (ScanResult, error) {
	plan := map[string]*planItem{}
	walk := walkResult{roots: make([]string, 0, len(paths))}
	for _, p := range paths {
		root, err := ResolveSafePath(s.Roots, p)
		if err != nil {
			return ScanResult{}, fmt.Errorf("folder %s is not available: %w", p, err)
		}
		walk.roots = append(walk.roots, root)
	}
	switch kind {
	case "books", "audiobooks", "photos":
		var (
			plan   map[string]*planItem
			probed map[string]probeOutcome
			err    error
		)
		switch kind {
		case "books":
			plan, probed, err = s.planBooks(ctx, libID, &walk)
		case "audiobooks":
			plan, probed, err = s.planAudiobooks(ctx, libID, &walk)
		default:
			plan, probed, err = s.planPhotos(ctx, libID, &walk)
		}
		if err != nil {
			return ScanResult{}, err
		}
		return s.apply(ctx, libID, plan, probed, walk, opts)
	}
	if kind == "music" {
		plan, probed, genres, err := s.planMusic(ctx, libID.String(), &walk)
		if err != nil {
			return ScanResult{}, err
		}
		res, err := s.apply(ctx, libID, plan, probed, walk, opts)
		if err != nil {
			return res, err
		}
		// Album genres come from the tags of their tracks (unless edited by hand).
		for key, g := range genres {
			if _, err := s.Pool.Exec(ctx, `UPDATE items SET genres = $3 WHERE library_id = $1 AND path = $2
				AND NOT metadata_locked AND genres IS DISTINCT FROM $3`, libID, key, g); err != nil {
				return res, err
			}
		}
		return res, nil
	}
	for _, root := range walk.roots {
		if err := s.planFolder(ctx, root, kind, plan, &walk.unreadable); err != nil {
			return ScanResult{}, err
		}
	}
	return s.apply(ctx, libID, plan, nil, walk, opts)
}

type dirContent struct {
	videos []planFile
	subs   []string
}

// collect walks root and groups videos and subtitles by folder. Folders and files
// that cannot be read are added to unreadable (nothing under them may be removed).
func collect(ctx context.Context, root string, unreadable *[]string) (map[string]*dirContent, error) {
	dirs := map[string]*dirContent{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			*unreadable = append(*unreadable, path)
			return nil // unreadable sub-folder: skip it, but keep what is known under it
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (IgnoredName(name) || IsExtraDir(name) || hasIgnoreMarker(path)) {
				return filepath.SkipDir
			}
			return nil
		}
		if IgnoredName(name) || !d.Type().IsRegular() {
			return nil // symlinks are not followed (they could leave the media roots)
		}
		dir := filepath.Dir(path)
		c := dirs[dir]
		if c == nil {
			c = &dirContent{}
			dirs[dir] = c
		}
		switch {
		case IsVideo(name) && !IsExtraFile(name):
			info, err := d.Info()
			if err != nil {
				*unreadable = append(*unreadable, path)
				return nil
			}
			c.videos = append(c.videos, planFile{path: path, size: info.Size(), mtime: info.ModTime().UTC().Truncate(time.Microsecond)})
		case IsSubtitle(name):
			c.subs = append(c.subs, path)
		}
		return nil
	})
	return dirs, err
}

func (s *Scanner) planFolder(ctx context.Context, root, kind string, plan map[string]*planItem, unreadable *[]string) error {
	dirs, err := collect(ctx, root, unreadable)
	if err != nil {
		return fmt.Errorf("reading %s: %w", root, err)
	}
	for _, c := range dirs {
		for i := range c.videos {
			for _, sp := range c.subs {
				if sub, ok := ExternalSubtitle(c.videos[i].path, sp); ok {
					c.videos[i].subs = append(c.videos[i].subs, sub)
				}
			}
		}
	}
	switch kind {
	case "movies":
		planMovies(root, dirs, plan)
	case "shows":
		planShows(root, dirs, plan, nil)
	case "mixed":
		shows := map[string]bool{}
		for dir, c := range dirs {
			top := topLevel(root, dir)
			if top == "" {
				continue
			}
			for _, v := range c.videos {
				if e, ok := ParseEpisode(strings.TrimSuffix(filepath.Base(v.path), filepath.Ext(v.path))); ok && !e.Absolute {
					shows[top] = true
				}
			}
			if _, ok := SeasonFromDir(filepath.Base(dir)); ok {
				shows[top] = true
			}
		}
		movieDirs := map[string]*dirContent{}
		for dir, c := range dirs {
			if !shows[topLevel(root, dir)] {
				movieDirs[dir] = c
			}
		}
		planMovies(root, movieDirs, plan)
		planShows(root, dirs, plan, shows)
	}
	return nil
}

// hasIgnoreMarker reports a ".streameroignore" (or ".ignore") file that hides a folder.
func hasIgnoreMarker(dir string) bool {
	for _, m := range []string{".streameroignore", ".ignore"} {
		if _, err := os.Stat(filepath.Join(dir, m)); err == nil {
			return true
		}
	}
	return false
}

// topLevel returns the first folder below root that contains dir ("" for root itself).
func topLevel(root, dir string) string {
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	return filepath.Join(root, strings.SplitN(rel, string(filepath.Separator), 2)[0])
}

func planMovies(root string, dirs map[string]*dirContent, plan map[string]*planItem) {
	for dir, c := range dirs {
		if len(c.videos) == 0 {
			continue
		}
		folder := ParseName(filepath.Base(dir))
		groups := map[string][]planFile{}
		parsed := map[string]Parsed{}
		for _, v := range c.videos {
			p := ParseName(strings.TrimSuffix(filepath.Base(v.path), filepath.Ext(v.path)))
			v.label = p.Label
			k := strings.ToLower(p.Title) + "|" + fmt.Sprint(p.Year)
			groups[k] = append(groups[k], v)
			if _, ok := parsed[k]; !ok {
				parsed[k] = p
			}
		}
		// One movie per folder when all files are versions of the same title and the
		// folder is not the library root; otherwise one movie per file.
		if len(groups) == 1 && dir != root {
			for k, files := range groups {
				p := folder
				if p.Title == "" {
					p = parsed[k]
				}
				for id, v := range parsed[k].ProviderIDs {
					if _, ok := p.ProviderIDs[id]; !ok {
						p.ProviderIDs[id] = v
					}
				}
				addMovie(plan, dir, p, files)
			}
			continue
		}
		for k, files := range groups {
			for _, f := range files {
				addMovie(plan, f.path, parsed[k], []planFile{f})
			}
		}
	}
}

func addMovie(plan map[string]*planItem, key string, p Parsed, files []planFile) {
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	it := &planItem{key: key, kind: "movie", title: p.Title, ids: p.ProviderIDs, files: files}
	if it.title == "" {
		it.title = strings.TrimSuffix(filepath.Base(key), filepath.Ext(key))
	}
	it.year = p.Year
	plan[key] = it
}

var episodeTitleRe = strings.NewReplacer("_", " ", ".", " ")

func planShows(root string, dirs map[string]*dirContent, plan map[string]*planItem, only map[string]bool) {
	for dir, c := range dirs {
		series := topLevel(root, dir)
		if series == "" || (only != nil && !only[series]) {
			continue
		}
		for _, v := range c.videos {
			stem := strings.TrimSuffix(filepath.Base(v.path), filepath.Ext(v.path))
			e, ok := ParseEpisode(stem)
			if !ok {
				continue
			}
			// SxxEyy / daily names carry the season; absolute numbering takes it from
			// the folder ("Season 02") and defaults to season 1.
			season := e.Season
			if season < 0 {
				season = 1
				if n, ok := SeasonFromDir(filepath.Base(dir)); ok {
					season = n
				}
			}
			if _, ok := plan[series]; !ok {
				p := ParseName(filepath.Base(series))
				plan[series] = &planItem{key: series, kind: "series", title: p.Title, year: p.Year, ids: p.ProviderIDs, order: -2}
			}
			seasonKey := fmt.Sprintf("%s#season-%d", series, season)
			if _, ok := plan[seasonKey]; !ok {
				sn := season
				title := fmt.Sprintf("Season %d", season)
				if season == 0 {
					title = "Specials"
				}
				plan[seasonKey] = &planItem{key: seasonKey, kind: "season", parentKey: series, title: title, season: &sn,
					ids: map[string]string{}, order: -1}
			}
			sn, en := season, e.Episode
			ep := &planItem{key: v.path, kind: "episode", parentKey: seasonKey, season: &sn, ep: &en,
				ids: map[string]string{}, files: []planFile{v}, title: episodeTitle(stem, e)}
			if e.EpisodeEnd > 0 {
				end := e.EpisodeEnd
				ep.epEnd = &end
			}
			plan[v.path] = ep
		}
	}
}

// episodeTitle takes the text after the episode marker ("Show - S01E02 - Pilot" → "Pilot").
func episodeTitle(stem string, e Episode) string {
	if e.Daily {
		return e.Title
	}
	if loc := sxxexx.FindStringIndex(stem); loc != nil {
		rest := strings.TrimSpace(strings.Trim(episodeTitleRe.Replace(stem[loc[1]:]), " -"))
		if rest != "" && ParseName(rest).Title != "" && !releaseTags[strings.ToLower(strings.Fields(rest)[0])] {
			return ParseName(rest).Title
		}
	}
	return fmt.Sprintf("Episode %d", e.Episode)
}

type existingItem struct {
	id                      uuid.UUID
	kind, title             string
	parent                  *uuid.UUID
	year, season, ep, epEnd *int
	ids                     map[string]string
	artist                  string
	meta                    []byte
	takenAt                 *time.Time
}

type existingFile struct {
	id         uuid.UUID
	item       uuid.UUID
	size       int64
	mtime      time.Time
	subtitles  []SubtitleStream
	probeError bool
	probedAt   *time.Time
}

// probeRetryAfter is how long a file whose probe failed waits before it is probed again.
const probeRetryAfter = time.Hour

// under reports whether path (a file path or an item key) is p or lies below it.
// Season keys ("<series>#season-1") count as part of their series folder.
func under(path, p string) bool {
	if i := strings.IndexByte(path, '#'); i >= 0 {
		path = path[:i]
	}
	return path == p || strings.HasPrefix(path, p+string(filepath.Separator))
}

func underAny(path string, list []string) bool {
	for _, p := range list {
		if under(path, p) {
			return true
		}
	}
	return false
}

func (s *Scanner) apply(ctx context.Context, libID uuid.UUID, plan map[string]*planItem, preProbed map[string]probeOutcome,
	walk walkResult, opts ScanOptions) (ScanResult, error) {
	items := map[string]existingItem{}
	rows, err := s.Pool.Query(ctx, `SELECT id, path, kind, title, parent_id, year, season_number, episode_number,
		episode_number_end, provider_ids, COALESCE(artist, ''), media_meta, taken_at FROM items WHERE library_id = $1`, libID)
	if err != nil {
		return ScanResult{}, err
	}
	for rows.Next() {
		var e existingItem
		var path string
		var ids []byte
		if err := rows.Scan(&e.id, &path, &e.kind, &e.title, &e.parent, &e.year, &e.season, &e.ep, &e.epEnd, &ids, &e.artist, &e.meta, &e.takenAt); err != nil {
			rows.Close()
			return ScanResult{}, err
		}
		_ = json.Unmarshal(ids, &e.ids)
		items[path] = e
	}
	rows.Close()
	files := map[string]existingFile{}
	rows, err = s.Pool.Query(ctx, `SELECT id, item_id, path, size, mtime, subtitles, probe_error IS NOT NULL, probed_at
		FROM media_files WHERE library_id = $1`, libID)
	if err != nil {
		return ScanResult{}, err
	}
	for rows.Next() {
		var f existingFile
		var path string
		var subs []byte
		if err := rows.Scan(&f.id, &f.item, &path, &f.size, &f.mtime, &subs, &f.probeError, &f.probedAt); err != nil {
			rows.Close()
			return ScanResult{}, err
		}
		_ = json.Unmarshal(subs, &f.subtitles)
		files[path] = f
	}
	rows.Close()

	ordered := make([]*planItem, 0, len(plan))
	for _, it := range plan {
		ordered = append(ordered, it)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].order != ordered[j].order {
			return ordered[i].order < ordered[j].order
		}
		return ordered[i].key < ordered[j].key
	})

	// Probe new and changed files in parallel before touching the database; files
	// whose last probe failed are tried again once probeRetryAfter has passed.
	var toProbe []planFile
	res := ScanResult{Unreadable: walk.unreadable}
	now := time.Now()
	for _, it := range ordered {
		for _, f := range it.files {
			res.Files++
			ex, ok := files[f.path]
			retry := ok && ex.probeError && (ex.probedAt == nil || now.Sub(*ex.probedAt) >= probeRetryAfter)
			if !ok || ex.size != f.size || !ex.mtime.Equal(f.mtime) || retry {
				toProbe = append(toProbe, f)
			}
		}
	}
	var rest []planFile
	for _, f := range toProbe {
		if _, done := preProbed[f.path]; !done {
			rest = append(rest, f)
		}
	}
	probed := s.probeAll(ctx, rest)
	for k, v := range preProbed {
		probed[k] = v
	}
	res.Probed = len(toProbe)

	ids := map[string]uuid.UUID{}
	for _, it := range ordered {
		var parent *uuid.UUID
		if it.parentKey != "" {
			if pid, ok := ids[it.parentKey]; ok {
				parent = &pid
			}
		}
		id, err := s.upsertItem(ctx, libID, it, parent, items)
		if err != nil {
			return res, err
		}
		ids[it.key] = id
		for _, f := range it.files {
			if err := s.upsertFile(ctx, libID, id, f, files, probed); err != nil {
				return res, err
			}
		}
	}

	return res, s.removeMissing(ctx, plan, items, files, walk, opts, &res)
}

// removeMissing deletes files the scan did not find, and items left without a place
// in the plan, unless that looks like an offline folder (D-043):
//   - nothing under a folder or file that could not be read is removed;
//   - when a library folder yields no files although files are known under it, or
//     more than half of its known files (and at least minHeldRemoval) would go,
//     nothing is removed and the count is reported as held, unless the admin
//     confirmed the removal.
func (s *Scanner) removeMissing(ctx context.Context, plan map[string]*planItem, items map[string]existingItem,
	files map[string]existingFile, walk walkResult, opts ScanOptions, res *ScanResult) error {
	seen := planFileSet(plan)
	keepItem := map[uuid.UUID]bool{}
	var gone []uuid.UUID
	var gonePaths []string
	for path, f := range files {
		if _, ok := seen[path]; ok {
			continue
		}
		if underAny(path, walk.unreadable) {
			keepItem[f.item] = true
			continue
		}
		gone = append(gone, f.id)
		gonePaths = append(gonePaths, path)
	}
	if len(gone) > 0 && !opts.ConfirmRemoval && removalLooksUnsafe(walk.roots, files, seen, gonePaths) {
		res.Held = len(gone)
		return nil
	}
	if len(gone) > 0 {
		if _, err := s.Pool.Exec(ctx, `DELETE FROM media_files WHERE id = ANY($1)`, gone); err != nil {
			return err
		}
	}
	// Items under unreadable folders stay, and so do the seasons, series, albums and
	// artists above anything that stays (deleting a parent would cascade).
	byID := make(map[uuid.UUID]existingItem, len(items))
	for path, e := range items {
		byID[e.id] = e
		if underAny(path, walk.unreadable) {
			keepItem[e.id] = true
		}
	}
	for id := range keepItem {
		for p := byID[id].parent; p != nil && !keepItem[*p]; p = byID[*p].parent {
			keepItem[*p] = true
		}
	}
	var goneItems []uuid.UUID
	for path, e := range items {
		if _, ok := plan[path]; !ok && !keepItem[e.id] {
			goneItems = append(goneItems, e.id)
		}
	}
	if len(goneItems) > 0 {
		if _, err := s.Pool.Exec(ctx, `DELETE FROM items WHERE id = ANY($1)`, goneItems); err != nil {
			return err
		}
	}
	res.Removed = len(gone)
	return nil
}

// removalLooksUnsafe applies the offline-folder rule to each library folder.
func removalLooksUnsafe(roots []string, files map[string]existingFile, seen map[string]struct{}, gone []string) bool {
	for _, root := range roots {
		known, found, missing := 0, 0, 0
		for path := range files {
			if under(path, root) {
				known++
			}
		}
		if known == 0 {
			continue
		}
		for path := range seen {
			if under(path, root) {
				found++
			}
		}
		for _, path := range gone {
			if under(path, root) {
				missing++
			}
		}
		if missing > 0 && found == 0 {
			return true // the folder is empty: an unmounted share, not a deleted library
		}
		if missing >= minHeldRemoval && missing*2 > known {
			return true
		}
	}
	return false
}

func planFileSet(plan map[string]*planItem) map[string]struct{} {
	set := map[string]struct{}{}
	for _, it := range plan {
		for _, f := range it.files {
			set[f.path] = struct{}{}
		}
	}
	return set
}

type probeOutcome struct {
	res ProbeResult
	err error
}

func (s *Scanner) probeAll(ctx context.Context, files []planFile) map[string]probeOutcome {
	n := s.Concurrency
	if n <= 0 {
		n = 4
	}
	out := make(map[string]probeOutcome, len(files))
	var mu sync.Mutex
	var wg sync.WaitGroup
	ch := make(chan string)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range ch {
				r, err := s.Probe(ctx, p)
				mu.Lock()
				out[p] = probeOutcome{r, err}
				mu.Unlock()
			}
		}()
	}
	for _, f := range files {
		ch <- f.path
	}
	close(ch)
	wg.Wait()
	return out
}

func intPtrEq(a, b *int) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }

func (s *Scanner) upsertItem(ctx context.Context, libID uuid.UUID, it *planItem, parent *uuid.UUID, existing map[string]existingItem) (uuid.UUID, error) {
	var year *int
	if it.year > 0 {
		y := it.year
		year = &y
	}
	if e, ok := existing[it.key]; ok && e.kind == it.kind && e.title == it.title && intPtrEq(e.year, year) &&
		intPtrEq(e.season, it.season) && intPtrEq(e.ep, it.ep) && intPtrEq(e.epEnd, it.epEnd) &&
		((e.parent == nil && parent == nil) || (e.parent != nil && parent != nil && *e.parent == *parent)) && containsIDs(e.ids, it.ids) &&
		e.artist == it.artist && metaEqual(e.meta, it.meta) && timePtrEq(e.takenAt, it.takenAt) {
		return e.id, nil // unchanged: no write (keeps re-scans fast)
	}
	newID, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, err
	}
	ids, _ := json.Marshal(nonNilMap(it.ids))
	var (
		meta, genres []byte
		authors      = []string{}
		seriesName   *string
		seriesIndex  *float64
	)
	if it.meta != nil {
		meta, _ = json.Marshal(it.meta)
		authors = nonNil(it.meta.Authors)
		if it.meta.Series != "" {
			seriesName = &it.meta.Series
		}
		seriesIndex = it.meta.SeriesIndex
		genres, _ = json.Marshal(nonNil(it.meta.Genres))
	}
	var id uuid.UUID
	err = s.Pool.QueryRow(ctx, `INSERT INTO items (id, library_id, parent_id, kind, path, title, sort_title, year,
			season_number, episode_number, episode_number_end, provider_ids, artist, media_meta, authors, series_name,
			series_index, taken_at, genres)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NULLIF($13, ''), $14, $15, $16, $17, $18,
			COALESCE(ARRAY(SELECT jsonb_array_elements_text($19::jsonb)), '{}'))
		ON CONFLICT (library_id, path) DO UPDATE SET parent_id = EXCLUDED.parent_id, kind = EXCLUDED.kind,
			title = EXCLUDED.title,
			-- once metadata named the item, its title decides the sort order
			sort_title = CASE WHEN items.metadata_title IS NULL THEN EXCLUDED.sort_title ELSE items.sort_title END,
			year = COALESCE(EXCLUDED.year, items.year),
			season_number = EXCLUDED.season_number, episode_number = EXCLUDED.episode_number,
			-- IDs from the metadata refresh stay; IDs written in the file name win
			episode_number_end = EXCLUDED.episode_number_end, provider_ids = items.provider_ids || EXCLUDED.provider_ids,
			artist = EXCLUDED.artist,
			media_meta = EXCLUDED.media_meta, authors = EXCLUDED.authors, series_name = EXCLUDED.series_name,
			series_index = EXCLUDED.series_index, taken_at = EXCLUDED.taken_at,
			-- genres from the file (books) unless edited by hand; other kinds keep theirs
			genres = CASE WHEN $19::jsonb IS NULL OR items.metadata_locked THEN items.genres ELSE EXCLUDED.genres END,
			updated_at = now()
		RETURNING id`, newID, libID, parent, it.kind, it.key, it.title, SortTitle(it.title), year,
		it.season, it.ep, it.epEnd, ids, it.artist, meta, authors, seriesName, seriesIndex, it.takenAt, genres).Scan(&id)
	return id, err
}

// containsIDs reports whether have already holds every ID of want with the same
// value. Only the file-name IDs (want) decide whether an item changed: the IDs the
// metadata refresh added are not in the file name and are kept by the upsert.
func containsIDs(have, want map[string]string) bool {
	for k, v := range want {
		if have[k] != v {
			return false
		}
	}
	return true
}

func (s *Scanner) upsertFile(ctx context.Context, libID, itemID uuid.UUID, f planFile, existing map[string]existingFile, probed map[string]probeOutcome) error {
	ex, known := existing[f.path]
	out, wasProbed := probed[f.path]
	if known && !wasProbed {
		// Unchanged file: only refresh external subtitles or a moved item if needed.
		embedded := make([]SubtitleStream, 0, len(ex.subtitles))
		var oldExternal []SubtitleStream
		for _, sub := range ex.subtitles {
			if sub.External {
				oldExternal = append(oldExternal, sub)
			} else {
				embedded = append(embedded, sub)
			}
		}
		if ex.item == itemID && subsEqual(oldExternal, f.subs) {
			return nil
		}
		subs, _ := json.Marshal(append(embedded, f.subs...))
		_, err := s.Pool.Exec(ctx, `UPDATE media_files SET item_id = $2, label = $3, subtitles = $4 WHERE id = $1`,
			ex.id, itemID, f.label, subs)
		return err
	}
	r := out.res
	var probeErr *string
	if out.err != nil {
		msg := out.err.Error()
		probeErr = &msg
		s.Log.Warn("probe failed", "path", f.path, "err", out.err)
		if known && !ex.probeError {
			// Probed fine before: a failure now is most likely transient (a busy share),
			// so keep the stream details and retry later. Size and mtime stay as they
			// were, so a changed file is probed again on the next scan as well.
			_, err := s.Pool.Exec(ctx, `UPDATE media_files SET item_id = $2, label = $3, probe_error = $4, probed_at = now()
				WHERE id = $1`, ex.id, itemID, f.label, probeErr)
			return err
		}
	}
	if r.Container == "" {
		r.Container = strings.TrimPrefix(strings.ToLower(filepath.Ext(f.path)), ".")
	}
	audio, _ := json.Marshal(nonNil(r.Audio))
	subs, _ := json.Marshal(append(nonNil(r.Subtitles), f.subs...))
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	var dur *int64
	if r.DurationMS > 0 {
		dur = &r.DurationMS
	}
	tags, _ := json.Marshal(nonNilMap(r.Tags))
	chapters, _ := json.Marshal(nonNil(r.Chapters))
	_, err = s.Pool.Exec(ctx, `INSERT INTO media_files (id, item_id, library_id, path, label, size, mtime, container,
			duration_ms, width, height, video_codec, hdr, audio, subtitles, probe_error, tags, video_profile, video_level, video_bit_depth,
			probed_at, chapters)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULLIF($10, 0), NULLIF($11, 0), $12, $13, $14, $15, $16, $17, $18,
			NULLIF($19, 0), NULLIF($20, 0), now(), $21)
		ON CONFLICT (library_id, path) DO UPDATE SET item_id = EXCLUDED.item_id, label = EXCLUDED.label,
			size = EXCLUDED.size, mtime = EXCLUDED.mtime, container = EXCLUDED.container, duration_ms = EXCLUDED.duration_ms,
			width = EXCLUDED.width, height = EXCLUDED.height, video_codec = EXCLUDED.video_codec, hdr = EXCLUDED.hdr,
			audio = EXCLUDED.audio, subtitles = EXCLUDED.subtitles, probe_error = EXCLUDED.probe_error, tags = EXCLUDED.tags,
			video_profile = EXCLUDED.video_profile, video_level = EXCLUDED.video_level, video_bit_depth = EXCLUDED.video_bit_depth,
			probed_at = EXCLUDED.probed_at, analyzed_at = NULL, trickplay = NULL, chapters = EXCLUDED.chapters`,
		id, itemID, libID, f.path, f.label, f.size, f.mtime, r.Container, dur, r.Width, r.Height, r.VideoCodec, r.HDR,
		audio, subs, probeErr, tags, r.VideoProfile, r.VideoLevel, r.VideoBitDepth, chapters)
	return err
}

func nonNilMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func subsEqual(a, b []SubtitleStream) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ErrUnsupportedKind is returned for library kinds the scanner cannot handle yet.
var ErrUnsupportedKind = errors.New("unsupported library kind")

// Stat helper used by the API to validate folders.
func isDir(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}
