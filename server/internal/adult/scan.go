package adult

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/streamero-app/streamero/server/internal/library"
)

// minHeldRemoval: like other libraries (D-043), a scan that would remove more than
// half of the known files (and at least this many) keeps them until an admin confirms.
const minHeldRemoval = 20

type foundFile struct {
	path, folder string
	size         int64
	mtime        time.Time
	subs         []library.SubtitleStream
}

type knownFile struct {
	id, scene uuid.UUID
	size      int64
	mtime     time.Time
	probeErr  bool
}

func ignoredDir(dir string) bool {
	for _, m := range []string{".streameroignore", ".ignore"} {
		if _, err := os.Stat(filepath.Join(dir, m)); err == nil {
			return true
		}
	}
	return false
}

// walk lists the video files under root with their sidecar subtitles.
func walk(ctx context.Context, root string, unreadable *[]string) ([]foundFile, error) {
	type dir struct {
		videos []foundFile
		subs   []string
	}
	dirs := map[string]*dir{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			*unreadable = append(*unreadable, path)
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (library.IgnoredName(name) || library.IsExtraDir(name) || ignoredDir(path)) {
				return filepath.SkipDir
			}
			return nil
		}
		if library.IgnoredName(name) || !d.Type().IsRegular() {
			return nil // symlinks are not followed
		}
		parent := filepath.Dir(path)
		c := dirs[parent]
		if c == nil {
			c = &dir{}
			dirs[parent] = c
		}
		switch {
		case library.IsVideo(name) && !library.IsExtraFile(name):
			info, err := d.Info()
			if err != nil {
				*unreadable = append(*unreadable, path)
				return nil
			}
			folder := ""
			if parent != root {
				folder = filepath.Base(parent)
			}
			c.videos = append(c.videos, foundFile{path: path, folder: folder, size: info.Size(),
				mtime: info.ModTime().UTC().Truncate(time.Microsecond)})
		case library.IsSubtitle(name):
			c.subs = append(c.subs, path)
		}
		return nil
	})
	var out []foundFile
	for _, c := range dirs {
		for _, v := range c.videos {
			for _, sp := range c.subs {
				if sub, ok := library.ExternalSubtitle(v.path, sp); ok {
					v.subs = append(v.subs, sub)
				}
			}
			out = append(out, v)
		}
	}
	return out, err
}

// Scan reads an adult library's folders: new and changed files are probed and
// become scenes (parsed from their names), missing ones are removed with the
// same offline-folder safety as other libraries. It never logs file names.
func (s *Service) Scan(ctx context.Context, libID uuid.UUID, paths []string, opts library.ScanOptions) (library.ScanResult, error) {
	var res library.ScanResult
	var roots []string
	for _, p := range paths {
		root, err := library.ResolveSafePath(s.Roots, p)
		if err != nil {
			return res, fmt.Errorf("a folder of the adult library is not available: %w", err)
		}
		roots = append(roots, root)
	}
	var found []foundFile
	for _, root := range roots {
		files, err := walk(ctx, root, &res.Unreadable)
		if err != nil {
			return res, fmt.Errorf("reading the adult library folder: %w", err)
		}
		found = append(found, files...)
	}
	res.Files = len(found)
	known, err := s.knownFiles(ctx, libID)
	if err != nil {
		return res, err
	}
	var todo []foundFile
	seen := map[string]bool{}
	for _, f := range found {
		seen[f.path] = true
		k, ok := known[f.path]
		if !ok || k.size != f.size || !k.mtime.Equal(f.mtime) || k.probeErr {
			todo = append(todo, f)
		}
	}
	probed := s.probeAll(ctx, todo)
	res.Probed = len(todo)
	for _, f := range todo {
		if err := s.upsert(ctx, libID, f, known, probed[f.path]); err != nil {
			return res, err
		}
	}
	// Missing files.
	var gone []knownFile
	for p, k := range known {
		if seen[p] || underAny(p, res.Unreadable) {
			continue
		}
		gone = append(gone, k)
	}
	if len(gone) > 0 && !opts.ConfirmRemoval && (len(found) == 0 || (len(gone) >= minHeldRemoval && len(gone)*2 > len(known))) {
		res.Held = len(gone)
		gone = nil
	}
	for _, k := range gone {
		if _, err := s.Pool.Exec(ctx, `DELETE FROM adult_files WHERE id = $1`, k.id); err != nil {
			return res, err
		}
		res.Removed++
	}
	if _, err := s.Pool.Exec(ctx, `DELETE FROM adult_scenes a WHERE a.library_id = $1
		AND NOT EXISTS (SELECT 1 FROM adult_files f WHERE f.scene_id = a.id)`, libID); err != nil {
		return res, err
	}
	return res, s.pruneNames(ctx)
}

// pruneNames removes studios and performers no scene refers to any more.
func (s *Service) pruneNames(ctx context.Context) error {
	if _, err := s.Pool.Exec(ctx, `DELETE FROM adult_performers p
		WHERE NOT EXISTS (SELECT 1 FROM adult_scene_performers sp WHERE sp.performer_id = p.id)`); err != nil {
		return err
	}
	_, err := s.Pool.Exec(ctx, `DELETE FROM adult_studios st WHERE NOT EXISTS (SELECT 1 FROM adult_scenes a WHERE a.studio_id = st.id)`)
	return err
}

func underAny(p string, dirs []string) bool {
	for _, d := range dirs {
		if p == d || strings.HasPrefix(p, strings.TrimSuffix(d, string(filepath.Separator))+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func (s *Service) knownFiles(ctx context.Context, libID uuid.UUID) (map[string]knownFile, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id, scene_id, path, size, mtime, probe_error IS NOT NULL FROM adult_files WHERE library_id = $1`, libID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]knownFile{}
	for rows.Next() {
		var k knownFile
		var p string
		if err := rows.Scan(&k.id, &k.scene, &p, &k.size, &k.mtime, &k.probeErr); err != nil {
			return nil, err
		}
		k.mtime = k.mtime.UTC()
		out[p] = k
	}
	return out, rows.Err()
}

type probeOutcome struct {
	res library.ProbeResult
	err error
}

func (s *Service) probeAll(ctx context.Context, files []foundFile) map[string]probeOutcome {
	out := map[string]probeOutcome{}
	if s.Probe == nil {
		return out
	}
	var mu sync.Mutex
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for _, f := range files {
		wg.Add(1)
		sem <- struct{}{}
		go func(p string) {
			defer wg.Done()
			defer func() { <-sem }()
			r, err := s.Probe(ctx, p)
			mu.Lock()
			out[p] = probeOutcome{r, err}
			mu.Unlock()
		}(f.path)
	}
	wg.Wait()
	return out
}

// upsert stores a file and its scene. A scene keyed by the file path keeps its
// matched metadata; an unmatched one follows its file name.
func (s *Service) upsert(ctx context.Context, libID uuid.UUID, f foundFile, known map[string]knownFile, pr probeOutcome) error {
	parsed := ParseFile(f.folder, filepath.Base(f.path))
	rawParsed, _ := json.Marshal(parsed)
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var sceneID uuid.UUID
	var matched bool
	err = tx.QueryRow(ctx, `SELECT id, provider_ids <> '{}'::jsonb FROM adult_scenes WHERE library_id = $1 AND path = $2`,
		libID, f.path).Scan(&sceneID, &matched)
	var date *string
	if parsed.Date != "" {
		date = &parsed.Date
	}
	var year *int
	if parsed.Year > 0 {
		year = &parsed.Year
	}
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		sceneID = uuid.Must(uuid.NewV7())
		studio, err := upsertStudio(ctx, tx, parsed.Studio)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO adult_scenes (id, library_id, kind, path, title, sort_title, release_date, year, studio_id, parsed)
			VALUES ($1, $2, $3, $4, $5, $6, $7::date, $8, $9, $10)`, sceneID, libID, parsed.Kind, f.path, parsed.Title,
			sortTitle(parsed.Title), date, year, studio, rawParsed); err != nil {
			return err
		}
		if err := linkPerformers(ctx, tx, sceneID, parsed.Performers); err != nil {
			return err
		}
	case err != nil:
		return err
	case !matched:
		studio, err := upsertStudio(ctx, tx, parsed.Studio)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE adult_scenes SET kind = $2, title = $3, sort_title = $4, release_date = $5::date, year = $6,
			studio_id = $7, parsed = $8, updated_at = now() WHERE id = $1`, sceneID, parsed.Kind, parsed.Title, sortTitle(parsed.Title),
			date, year, studio, rawParsed); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM adult_scene_performers WHERE scene_id = $1`, sceneID); err != nil {
			return err
		}
		if err := linkPerformers(ctx, tx, sceneID, parsed.Performers); err != nil {
			return err
		}
	}
	r := pr.res
	var probeErr *string
	if pr.err != nil {
		msg := "the file could not be read by ffprobe"
		probeErr = &msg
	} else if s.Probe == nil {
		r.Container = strings.TrimPrefix(strings.ToLower(filepath.Ext(f.path)), ".")
	}
	audio, _ := json.Marshal(nonNil(r.Audio))
	subs := append(nonNil(r.Subtitles), f.subs...)
	rawSubs, _ := json.Marshal(subs)
	id := uuid.Must(uuid.NewV7())
	if k, ok := known[f.path]; ok {
		id = k.id
	}
	if _, err := tx.Exec(ctx, `INSERT INTO adult_files (id, scene_id, library_id, path, size, mtime, container, duration_ms, width, height,
			video_codec, video_profile, video_level, video_bit_depth, hdr, audio, subtitles, probe_error)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
		ON CONFLICT (library_id, path) DO UPDATE SET scene_id = EXCLUDED.scene_id, size = EXCLUDED.size, mtime = EXCLUDED.mtime,
			container = EXCLUDED.container, duration_ms = EXCLUDED.duration_ms, width = EXCLUDED.width, height = EXCLUDED.height,
			video_codec = EXCLUDED.video_codec, video_profile = EXCLUDED.video_profile, video_level = EXCLUDED.video_level,
			video_bit_depth = EXCLUDED.video_bit_depth, hdr = EXCLUDED.hdr, audio = EXCLUDED.audio, subtitles = EXCLUDED.subtitles,
			probe_error = EXCLUDED.probe_error`,
		id, sceneID, libID, f.path, f.size, f.mtime, r.Container, nilZero64(r.DurationMS), nilZero(r.Width), nilZero(r.Height),
		r.VideoCodec, r.VideoProfile, r.VideoLevel, r.VideoBitDepth, r.HDR, audio, rawSubs, probeErr); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func nilZero(v int) *int {
	if v == 0 {
		return nil
	}
	return &v
}

func nilZero64(v int64) *int64 {
	if v == 0 {
		return nil
	}
	return &v
}

// upsertStudio returns the id of the studio called name (nil for "").
func upsertStudio(ctx context.Context, tx pgx.Tx, name string) (*uuid.UUID, error) {
	key := foldKey(name)
	if key == "" {
		return nil, nil
	}
	var id uuid.UUID
	err := tx.QueryRow(ctx, `INSERT INTO adult_studios (id, name, name_key) VALUES ($1, $2, $3)
		ON CONFLICT (name_key) DO UPDATE SET name_key = EXCLUDED.name_key RETURNING id`, uuid.Must(uuid.NewV7()), name, key).Scan(&id)
	return &id, err
}

// upsertPerformer returns the id of the performer called name.
func upsertPerformer(ctx context.Context, tx pgx.Tx, name string) (uuid.UUID, bool, error) {
	key := foldKey(name)
	if key == "" {
		return uuid.Nil, false, nil
	}
	var id uuid.UUID
	err := tx.QueryRow(ctx, `INSERT INTO adult_performers (id, name, name_key) VALUES ($1, $2, $3)
		ON CONFLICT (name_key) DO UPDATE SET name_key = EXCLUDED.name_key RETURNING id`, uuid.Must(uuid.NewV7()), name, key).Scan(&id)
	return id, err == nil, err
}

func linkPerformers(ctx context.Context, tx pgx.Tx, sceneID uuid.UUID, names []string) error {
	for i, n := range names {
		id, ok, err := upsertPerformer(ctx, tx, n)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		if _, err := tx.Exec(ctx, `INSERT INTO adult_scene_performers (scene_id, performer_id, position) VALUES ($1, $2, $3)
			ON CONFLICT DO NOTHING`, sceneID, id, i); err != nil {
			return err
		}
	}
	return nil
}
