package adult

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // decoder for downloaded artwork
	"image/jpeg"
	_ "image/png" // decoder for downloaded artwork
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/streamero-app/streamero/server/internal/library"
)

// enrichBatch is how many scenes one run looks up (a run per scan; at 2 calls a
// second per service a large first scan takes a few runs).
const enrichBatch = 400

// Image owners and types (paths are built only from these and UUIDs).
var (
	imageOwners = map[string]bool{"scene": true, "studio": true, "performer": true}
	imageTypes  = map[string]bool{"poster": true, "backdrop": true, "image": true}
)

// ImagePath returns the artwork file of a scene, studio or performer ("" for
// unknown owners or types).
func (s *Service) ImagePath(owner string, id uuid.UUID, typ string) string {
	if !imageOwners[owner] || !imageTypes[typ] || s.ImageDir == "" {
		return ""
	}
	return filepath.Join(s.ImageDir, owner, id.String(), typ+".jpg")
}

type pending struct {
	id     uuid.UUID
	kind   string
	parsed Parsed
	title  string
	path   string
	dur    *int64
	poster bool
}

// Enrich looks up scenes of a library that were not looked up yet (all of them
// with force) and grabs a frame as artwork for scenes left without one.
func (s *Service) Enrich(ctx context.Context, libID uuid.UUID, force bool) (int, error) {
	q := `SELECT a.id, a.kind, a.parsed, a.title, f.path, f.duration_ms, a.has_poster FROM adult_scenes a
		JOIN LATERAL (SELECT path, duration_ms FROM adult_files WHERE scene_id = a.id ORDER BY height DESC NULLS LAST LIMIT 1) f ON true
		WHERE a.library_id = $1`
	if !force {
		q += ` AND a.metadata_refreshed_at IS NULL`
	}
	rows, err := s.Pool.Query(ctx, q+` ORDER BY a.added_at DESC LIMIT `+strconv.Itoa(enrichBatch), libID)
	if err != nil {
		return 0, err
	}
	var todo []pending
	for rows.Next() {
		var p pending
		var raw []byte
		if err := rows.Scan(&p.id, &p.kind, &raw, &p.title, &p.path, &p.dur, &p.poster); err != nil {
			rows.Close()
			return 0, err
		}
		_ = json.Unmarshal(raw, &p.parsed)
		todo = append(todo, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	keys, err := s.LoadKeys(ctx)
	if err != nil {
		return 0, err
	}
	matched := 0
	for _, p := range todo {
		if ctx.Err() != nil {
			return matched, ctx.Err()
		}
		ok, err := s.enrichOne(ctx, keys, p)
		if err != nil {
			if errors.Is(err, ErrBadKey) {
				return matched, err // every further call fails the same way
			}
			s.log().Warn("adult metadata lookup failed", "scene", p.id, "err", err)
			continue
		}
		if ok {
			matched++
		}
	}
	// Galleries follow their scene's new studio, date and tags.
	return matched, s.LinkGalleries(ctx, libID)
}

// RefreshScene looks one scene up again.
func (s *Service) RefreshScene(ctx context.Context, id uuid.UUID) error {
	var p pending
	var raw []byte
	err := s.Pool.QueryRow(ctx, `SELECT a.id, a.kind, a.parsed, a.title, f.path, f.duration_ms, a.has_poster FROM adult_scenes a
		JOIN LATERAL (SELECT path, duration_ms FROM adult_files WHERE scene_id = a.id ORDER BY height DESC NULLS LAST LIMIT 1) f ON true
		WHERE a.id = $1`, id).Scan(&p.id, &p.kind, &raw, &p.title, &p.path, &p.dur, &p.poster)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	_ = json.Unmarshal(raw, &p.parsed)
	keys, err := s.LoadKeys(ctx)
	if err != nil {
		return err
	}
	if _, err = s.enrichOne(ctx, keys, p); err != nil {
		return err
	}
	var libID uuid.UUID
	if err := s.Pool.QueryRow(ctx, `SELECT library_id FROM adult_scenes WHERE id = $1`, id).Scan(&libID); err != nil {
		return err
	}
	return s.LinkGalleries(ctx, libID)
}

func (s *Service) enrichOne(ctx context.Context, keys Keys, p pending) (bool, error) {
	q := Query{Kind: p.kind, Studio: p.parsed.Studio, Date: p.parsed.Date, Title: p.parsed.Title, Year: p.parsed.Year,
		Performers: p.parsed.Performers}
	if q.Title == "" {
		q.Title = p.title
	}
	var best *Match
	var lastErr error
	tried := false
	for _, src := range []struct {
		key    string
		search func(context.Context, Query) ([]Match, error)
	}{{keys.TPDB, func(ctx context.Context, q Query) ([]Match, error) { return s.tpdb(keys.TPDB).search(ctx, q) }},
		{keys.StashDB, func(ctx context.Context, q Query) ([]Match, error) { return s.stashdb(keys.StashDB).search(ctx, q) }}} {
		if src.key == "" {
			continue
		}
		tried = true
		found, err := src.search(ctx, q)
		if err != nil {
			if errors.Is(err, ErrBadKey) {
				return false, err
			}
			lastErr = err
			continue
		}
		if m := Best(q, found); m != nil {
			best = m
			break
		}
	}
	if best != nil {
		if err := s.apply(ctx, p.id, *best); err != nil {
			return false, err
		}
	} else {
		msg := ""
		switch {
		case lastErr != nil:
			// Not marked as looked up: the next run tries again.
			_, err := s.Pool.Exec(ctx, `UPDATE adult_scenes SET metadata_error = $2 WHERE id = $1`, p.id, "lookup failed; will retry")
			if err != nil {
				return false, err
			}
			s.ensureFrame(ctx, p)
			return false, nil
		case tried:
			msg = "no match in ThePornDB / StashDB"
		}
		var me *string
		if msg != "" {
			me = &msg
		}
		if _, err := s.Pool.Exec(ctx, `UPDATE adult_scenes SET metadata_refreshed_at = now(), metadata_error = $2 WHERE id = $1`, p.id, me); err != nil {
			return false, err
		}
	}
	s.ensureFrame(ctx, p)
	return best != nil, nil
}

// Best picks the candidate that fits what the file name says, or nil when none
// fits well enough (a wrong match is worse than none).
func Best(q Query, found []Match) *Match {
	var best *Match
	bestScore := 0
	for i := range found {
		m := &found[i]
		score := 0
		tq, tm := foldKey(q.Title), foldKey(m.Title)
		switch {
		case tq != "" && tq == tm:
			score += 3
		case tq != "" && tm != "" && (strings.Contains(tm, tq) || strings.Contains(tq, tm)):
			score += 2
		}
		if q.Date != "" && strings.HasPrefix(m.Date, q.Date) {
			score += 3
		}
		if sq := foldKey(q.Studio); sq != "" && (sq == foldKey(m.Studio) || sq == foldKey(m.StudioParent)) {
			score += 2
		}
		for _, pn := range q.Performers {
			for _, mp := range m.Performers {
				if foldKey(pn) == foldKey(mp.Name) {
					score++
				}
			}
		}
		if q.Kind == "movie" && q.Year > 0 && len(m.Date) >= 4 {
			if y, err := strconv.Atoi(m.Date[:4]); err == nil && (y < q.Year-1 || y > q.Year+1) {
				continue
			}
		}
		if score > bestScore {
			best, bestScore = m, score
		}
	}
	need := 4 // e.g. same date and studio, or same date and title
	if q.Kind == "movie" {
		need = 3 // the same title within a year
	}
	if bestScore < need {
		return nil
	}
	return best
}

// apply stores a match: title, date, details, studio, performers, tags and artwork.
func (s *Service) apply(ctx context.Context, sceneID uuid.UUID, m Match) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var date *string
	var year *int
	if t, err := time.Parse("2006-01-02", firstN(m.Date, 10)); err == nil {
		d := t.Format("2006-01-02")
		y := t.Year()
		date, year = &d, &y
	}
	var studioID *uuid.UUID
	if m.Studio != "" {
		if studioID, err = upsertStudio(ctx, tx, m.Studio); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE adult_studios SET name = $2, url = COALESCE(NULLIF($3, ''), url), parent = COALESCE(NULLIF($4, ''), parent),
			provider_ids = provider_ids || jsonb_build_object($5::text, $6::text), refreshed_at = now() WHERE id = $1`,
			studioID, m.Studio, m.StudioURL, m.StudioParent, m.Provider, m.StudioID); err != nil {
			return err
		}
	}
	title := m.Title
	if strings.TrimSpace(title) == "" {
		if err := tx.QueryRow(ctx, `SELECT title FROM adult_scenes WHERE id = $1`, sceneID).Scan(&title); err != nil {
			return err
		}
	}
	tags := nonNil(m.Tags)
	if len(tags) > 30 {
		tags = tags[:30]
	}
	if _, err := tx.Exec(ctx, `UPDATE adult_scenes SET title = $2, sort_title = $3, release_date = COALESCE($4::date, release_date),
		year = COALESCE($5, year), studio_id = COALESCE($6, studio_id), description = NULLIF($7, ''), url = NULLIF($8, ''), tags = $9,
		provider_ids = provider_ids || jsonb_build_object($10::text, $11::text), metadata_refreshed_at = now(), metadata_error = NULL,
		updated_at = now() WHERE id = $1`, sceneID, title, sortTitle(title), date, year, studioID, m.Details, m.URL, tags,
		m.Provider, m.ID); err != nil {
		return err
	}
	var performerIDs []uuid.UUID
	if len(m.Performers) > 0 {
		if _, err := tx.Exec(ctx, `DELETE FROM adult_scene_performers WHERE scene_id = $1`, sceneID); err != nil {
			return err
		}
		for i, p := range m.Performers {
			id, ok, err := upsertPerformer(ctx, tx, p.Name)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			aliases := nonNil(p.Aliases)
			if len(aliases) > 20 {
				aliases = aliases[:20]
			}
			if _, err := tx.Exec(ctx, `UPDATE adult_performers SET name = $2, aliases = CASE WHEN cardinality($3::text[]) > 0 THEN $3 ELSE aliases END,
				gender = COALESCE(NULLIF($4, ''), gender), birthdate = COALESCE(NULLIF($5, ''), birthdate),
				country = COALESCE(NULLIF($6, ''), country), description = COALESCE(NULLIF($7, ''), description),
				provider_ids = provider_ids || jsonb_build_object($8::text, $9::text), refreshed_at = now() WHERE id = $1`,
				id, p.Name, aliases, p.Gender, p.Birthdate, p.Country, p.Bio, m.Provider, p.ID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO adult_scene_performers (scene_id, performer_id, position) VALUES ($1, $2, $3)
				ON CONFLICT DO NOTHING`, sceneID, id, i); err != nil {
				return err
			}
			performerIDs = append(performerIDs, id)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	_ = s.pruneNames(ctx)
	// Artwork after the commit: downloads are slow and may fail on their own.
	if s.fetchImage(ctx, m.Poster, s.ImagePath("scene", sceneID, "poster")) {
		_, _ = s.Pool.Exec(ctx, `UPDATE adult_scenes SET has_poster = true WHERE id = $1`, sceneID)
	}
	if s.fetchImage(ctx, m.Backdrop, s.ImagePath("scene", sceneID, "backdrop")) {
		_, _ = s.Pool.Exec(ctx, `UPDATE adult_scenes SET has_backdrop = true WHERE id = $1`, sceneID)
	}
	if studioID != nil && m.StudioImage != "" {
		var has bool
		_ = s.Pool.QueryRow(ctx, `SELECT has_image FROM adult_studios WHERE id = $1`, *studioID).Scan(&has)
		if !has && s.fetchImage(ctx, m.StudioImage, s.ImagePath("studio", *studioID, "image")) {
			_, _ = s.Pool.Exec(ctx, `UPDATE adult_studios SET has_image = true WHERE id = $1`, *studioID)
		}
	}
	for i, id := range performerIDs {
		if i >= len(m.Performers) || m.Performers[i].Image == "" {
			continue
		}
		var has bool
		_ = s.Pool.QueryRow(ctx, `SELECT has_image FROM adult_performers WHERE id = $1`, id).Scan(&has)
		if !has && s.fetchImage(ctx, m.Performers[i].Image, s.ImagePath("performer", id, "image")) {
			_, _ = s.Pool.Exec(ctx, `UPDATE adult_performers SET has_image = true WHERE id = $1`, id)
		}
	}
	return nil
}

func firstN(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// fetchImage downloads an image (https/http only, same-host redirects, at most
// maxImage bytes) and stores it re-encoded as JPEG, so nothing but pixels is kept.
func (s *Service) fetchImage(ctx context.Context, src, dst string) bool {
	if src == "" || dst == "" || (!strings.HasPrefix(src, "https://") && !strings.HasPrefix(src, "http://")) {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return false
	}
	res, err := s.httpClient().Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return false
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxImage+1))
	if err != nil || len(data) > maxImage {
		return false
	}
	return s.storeImage(ctx, data, dst)
}

func (s *Service) storeImage(ctx context.Context, data []byte, dst string) bool {
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return false
	}
	var out bytes.Buffer
	if img, _, err := image.Decode(bytes.NewReader(data)); err == nil {
		if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: 85}); err != nil {
			return false
		}
	} else if s.FFmpeg != "" {
		// WebP and other formats the standard library cannot read.
		// #nosec G204 -- fixed binary and arguments; the image comes in on stdin.
		cmd := exec.CommandContext(ctx, s.FFmpeg, "-v", "error", "-i", "pipe:0", "-frames:v", "1", "-f", "image2", "-c:v", "mjpeg", "-q:v", "3", "pipe:1")
		cmd.Stdin = bytes.NewReader(data)
		cmd.Stdout = &out
		if cmd.Run() != nil || out.Len() == 0 {
			return false
		}
	} else {
		return false
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, out.Bytes(), 0o600); err != nil {
		return false
	}
	return os.Rename(tmp, dst) == nil
}

// ensureFrame makes a frame of the video the artwork of a scene that has none.
func (s *Service) ensureFrame(ctx context.Context, p pending) {
	if s.FFmpeg == "" {
		return
	}
	var has bool
	if err := s.Pool.QueryRow(ctx, `SELECT has_poster FROM adult_scenes WHERE id = $1`, p.id).Scan(&has); err != nil || has {
		return
	}
	path, err := library.ResolveSafePath(s.Roots, p.path)
	if err != nil {
		return
	}
	at := int64(60_000)
	if p.dur != nil && *p.dur > 0 {
		at = *p.dur / 5
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	// #nosec G204 -- fixed binary; the path was validated and is one argument.
	cmd := exec.CommandContext(ctx, s.FFmpeg, "-v", "error", "-ss", fmt.Sprintf("%.3f", float64(at)/1000), "-i", path,
		"-frames:v", "1", "-vf", "scale=960:-2", "-f", "image2", "-c:v", "mjpeg", "-q:v", "4", "pipe:1")
	var out bytes.Buffer
	cmd.Stdout = &out
	if cmd.Run() != nil || out.Len() == 0 {
		return
	}
	if s.storeImage(ctx, out.Bytes(), s.ImagePath("scene", p.id, "poster")) {
		_, _ = s.Pool.Exec(ctx, `UPDATE adult_scenes SET has_poster = true WHERE id = $1`, p.id)
	}
}

// RemoveLibraryImages deletes the artwork of scenes of a library being deleted
// (studios and performers are shared and pruned when unused).
func (s *Service) RemoveLibraryImages(ctx context.Context, libID uuid.UUID) error {
	rows, err := s.Pool.Query(ctx, `SELECT id FROM adult_scenes WHERE library_id = $1`, libID)
	if err != nil {
		return err
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if s.ImageDir != "" {
			_ = os.RemoveAll(filepath.Join(s.ImageDir, "scene", id.String()))
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	// Cached gallery thumbnails and converted pictures.
	imgs, err := s.Pool.Query(ctx, `SELECT id FROM adult_images WHERE library_id = $1`, libID)
	if err != nil {
		return err
	}
	var gone []uuid.UUID
	for imgs.Next() {
		var id uuid.UUID
		if err := imgs.Scan(&id); err != nil {
			imgs.Close()
			return err
		}
		gone = append(gone, id)
	}
	imgs.Close()
	for _, id := range gone {
		s.removeImageCache(id)
	}
	return imgs.Err()
}
