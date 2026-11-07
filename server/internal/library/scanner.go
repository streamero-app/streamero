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
	order                int // parents (series, seasons) before children
}

// ScanResult summarises a scan.
type ScanResult struct {
	Files, Probed, Removed int
}

// Scan rescans one library. If any library folder cannot be read the scan fails
// without removing anything (an unmounted disk must not empty the library).
func (s *Scanner) Scan(ctx context.Context, libraryID uuid.UUID) (ScanResult, error) {
	var kind string
	var paths []string
	if err := s.Pool.QueryRow(ctx, `UPDATE libraries SET scan_state = 'scanning', scan_error = NULL WHERE id = $1
		RETURNING kind, paths`, libraryID).Scan(&kind, &paths); err != nil {
		return ScanResult{}, err
	}
	res, err := s.scan(ctx, libraryID, kind, paths)
	if err != nil {
		_, _ = s.Pool.Exec(context.WithoutCancel(ctx), `UPDATE libraries SET scan_state = 'failed', scan_error = $2 WHERE id = $1`,
			libraryID, err.Error())
		return res, err
	}
	_, err = s.Pool.Exec(ctx, `UPDATE libraries SET scan_state = 'idle', last_scan_at = now(), files_seen = $2 WHERE id = $1`,
		libraryID, res.Files)
	return res, err
}

func (s *Scanner) scan(ctx context.Context, libID uuid.UUID, kind string, paths []string) (ScanResult, error) {
	plan := map[string]*planItem{}
	for _, p := range paths {
		root, err := ResolveSafePath(s.Roots, p)
		if err != nil {
			return ScanResult{}, fmt.Errorf("folder %s is not available: %w", p, err)
		}
		if err := s.planFolder(ctx, root, kind, plan); err != nil {
			return ScanResult{}, err
		}
	}
	return s.apply(ctx, libID, plan)
}

type dirContent struct {
	videos []planFile
	subs   []string
}

// collect walks root and groups videos and subtitles by folder.
func collect(ctx context.Context, root string) (map[string]*dirContent, error) {
	dirs := map[string]*dirContent{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			return nil // unreadable sub-folder: skip it
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

func (s *Scanner) planFolder(ctx context.Context, root, kind string, plan map[string]*planItem) error {
	dirs, err := collect(ctx, root)
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
}

type existingFile struct {
	id        uuid.UUID
	item      uuid.UUID
	size      int64
	mtime     time.Time
	subtitles []SubtitleStream
}

func (s *Scanner) apply(ctx context.Context, libID uuid.UUID, plan map[string]*planItem) (ScanResult, error) {
	items := map[string]existingItem{}
	rows, err := s.Pool.Query(ctx, `SELECT id, path, kind, title, parent_id, year, season_number, episode_number,
		episode_number_end, provider_ids FROM items WHERE library_id = $1`, libID)
	if err != nil {
		return ScanResult{}, err
	}
	for rows.Next() {
		var e existingItem
		var path string
		var ids []byte
		if err := rows.Scan(&e.id, &path, &e.kind, &e.title, &e.parent, &e.year, &e.season, &e.ep, &e.epEnd, &ids); err != nil {
			rows.Close()
			return ScanResult{}, err
		}
		_ = json.Unmarshal(ids, &e.ids)
		items[path] = e
	}
	rows.Close()
	files := map[string]existingFile{}
	rows, err = s.Pool.Query(ctx, `SELECT id, item_id, path, size, mtime, subtitles FROM media_files WHERE library_id = $1`, libID)
	if err != nil {
		return ScanResult{}, err
	}
	for rows.Next() {
		var f existingFile
		var path string
		var subs []byte
		if err := rows.Scan(&f.id, &f.item, &path, &f.size, &f.mtime, &subs); err != nil {
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

	// Probe new and changed files in parallel before touching the database.
	var toProbe []planFile
	res := ScanResult{}
	for _, it := range ordered {
		for _, f := range it.files {
			res.Files++
			if ex, ok := files[f.path]; !ok || ex.size != f.size || !ex.mtime.Equal(f.mtime) {
				toProbe = append(toProbe, f)
			}
		}
	}
	probed := s.probeAll(ctx, toProbe)
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

	// Remove what disappeared (files first; then items with nothing left).
	var gone []uuid.UUID
	for path, f := range files {
		if _, ok := planFileSet(plan)[path]; !ok {
			gone = append(gone, f.id)
		}
	}
	if len(gone) > 0 {
		if _, err := s.Pool.Exec(ctx, `DELETE FROM media_files WHERE id = ANY($1)`, gone); err != nil {
			return res, err
		}
	}
	var goneItems []uuid.UUID
	for path, e := range items {
		if _, ok := plan[path]; !ok {
			goneItems = append(goneItems, e.id)
		}
	}
	if len(goneItems) > 0 {
		if _, err := s.Pool.Exec(ctx, `DELETE FROM items WHERE id = ANY($1)`, goneItems); err != nil {
			return res, err
		}
	}
	res.Removed = len(gone)
	return res, nil
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
		((e.parent == nil && parent == nil) || (e.parent != nil && parent != nil && *e.parent == *parent)) && mapsEqual(e.ids, it.ids) {
		return e.id, nil // unchanged: no write (keeps re-scans fast)
	}
	newID, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, err
	}
	ids, _ := json.Marshal(it.ids)
	var id uuid.UUID
	err = s.Pool.QueryRow(ctx, `INSERT INTO items (id, library_id, parent_id, kind, path, title, sort_title, year,
			season_number, episode_number, episode_number_end, provider_ids)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (library_id, path) DO UPDATE SET parent_id = EXCLUDED.parent_id, kind = EXCLUDED.kind,
			title = EXCLUDED.title,
			-- once metadata named the item, its title decides the sort order
			sort_title = CASE WHEN items.metadata_title IS NULL THEN EXCLUDED.sort_title ELSE items.sort_title END,
			year = COALESCE(EXCLUDED.year, items.year),
			season_number = EXCLUDED.season_number, episode_number = EXCLUDED.episode_number,
			episode_number_end = EXCLUDED.episode_number_end, provider_ids = EXCLUDED.provider_ids, updated_at = now()
		RETURNING id`, newID, libID, parent, it.kind, it.key, it.title, SortTitle(it.title), year,
		it.season, it.ep, it.epEnd, ids).Scan(&id)
	return id, err
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
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
	_, err = s.Pool.Exec(ctx, `INSERT INTO media_files (id, item_id, library_id, path, label, size, mtime, container,
			duration_ms, width, height, video_codec, hdr, audio, subtitles, probe_error)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULLIF($10, 0), NULLIF($11, 0), $12, $13, $14, $15, $16)
		ON CONFLICT (library_id, path) DO UPDATE SET item_id = EXCLUDED.item_id, label = EXCLUDED.label,
			size = EXCLUDED.size, mtime = EXCLUDED.mtime, container = EXCLUDED.container, duration_ms = EXCLUDED.duration_ms,
			width = EXCLUDED.width, height = EXCLUDED.height, video_codec = EXCLUDED.video_codec, hdr = EXCLUDED.hdr,
			audio = EXCLUDED.audio, subtitles = EXCLUDED.subtitles, probe_error = EXCLUDED.probe_error`,
		id, itemID, libID, f.path, f.label, f.size, f.mtime, r.Container, dur, r.Width, r.Height, r.VideoCodec, r.HDR,
		audio, subs, probeErr)
	return err
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
var ErrUnsupportedKind = errors.New("music libraries arrive in phase P9")

// Stat helper used by the API to validate folders.
func isDir(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}
