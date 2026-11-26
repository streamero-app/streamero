package adult

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/streamero-app/streamero/server/internal/library"
)

// Ref is a studio or performer named on a scene.
type Ref struct {
	ID   uuid.UUID
	Name string
}

// Scene is a scene or movie in a list.
type Scene struct {
	ID, LibraryID          uuid.UUID
	Kind, Title            string
	Date                   *time.Time
	Year                   *int
	Studio                 *Ref
	Performers             []Ref
	Tags                   []string
	DurationMS             *int64
	Height                 *int
	HasPoster, HasBackdrop bool
	PositionMS             int64
	Played                 bool
	AddedAt                time.Time
	Description, URL       string
	ProviderIDs            map[string]string
	MetadataError          string
	Files                  []File
	Galleries              []Gallery
}

// File is a video file of a scene.
type File struct {
	ID               uuid.UUID
	Path             string
	Size             int64
	Container, Codec string
	Width, Height    *int
	DurationMS       *int64
}

// Studio is a studio (site) with its scene count.
type Studio struct {
	ID           uuid.UUID
	Name         string
	SceneCount   int
	GalleryCount int
	HasImage   bool
	URL        string
	Parent     string
}

// Performer is a performer with their scene count.
type Performer struct {
	ID                                      uuid.UUID
	Name                                    string
	SceneCount, GalleryCount                int
	HasImage, Favorite                      bool
	Aliases                                 []string
	Gender, Birthdate, Country, Description string
}

// Filter narrows a scene list.
type Filter struct {
	LibraryID, StudioID, PerformerID *uuid.UUID
	Kind, Query, Tag, Status, Sort   string
	Year                             *int
	Offset, Limit                    int
	// FavoritePerformers keeps scenes with a performer the viewer marked as a favourite.
	FavoritePerformers bool
	// AddedSince keeps scenes added after this time.
	AddedSince *time.Time
}

const sceneCols = `a.id, a.library_id, a.kind, a.title, a.release_date, a.year, a.studio_id, st.name, a.tags,
	(SELECT max(f.duration_ms) FROM adult_files f WHERE f.scene_id = a.id),
	(SELECT max(f.height) FROM adult_files f WHERE f.scene_id = a.id),
	a.has_poster, a.has_backdrop, COALESCE(pr.position_ms, 0), COALESCE(pr.played, false), a.added_at,
	COALESCE(a.description, ''), COALESCE(a.url, ''), a.provider_ids, COALESCE(a.metadata_error, '')`

func sceneFrom(user string) string {
	return ` FROM adult_scenes a
		JOIN libraries l ON l.id = a.library_id AND l.kind = 'adult'
		LEFT JOIN adult_studios st ON st.id = a.studio_id
		LEFT JOIN adult_progress pr ON pr.scene_id = a.id AND pr.user_id = ` + user + ` `
}

func scanScene(row pgx.Row) (Scene, error) {
	var sc Scene
	var studioID *uuid.UUID
	var studio *string
	var ids []byte
	err := row.Scan(&sc.ID, &sc.LibraryID, &sc.Kind, &sc.Title, &sc.Date, &sc.Year, &studioID, &studio, &sc.Tags,
		&sc.DurationMS, &sc.Height, &sc.HasPoster, &sc.HasBackdrop, &sc.PositionMS, &sc.Played, &sc.AddedAt,
		&sc.Description, &sc.URL, &ids, &sc.MetadataError)
	if errors.Is(err, pgx.ErrNoRows) {
		return sc, ErrNotFound
	}
	if err != nil {
		return sc, err
	}
	if studioID != nil && studio != nil {
		sc.Studio = &Ref{ID: *studioID, Name: *studio}
	}
	_ = json.Unmarshal(ids, &sc.ProviderIDs)
	if sc.Tags == nil {
		sc.Tags = []string{}
	}
	return sc, nil
}

// Scenes returns a page of scenes for a viewer.
func (s *Service) Scenes(ctx context.Context, v Viewer, f Filter) ([]Scene, int, error) {
	hidden, hideTagged, err := s.hiddenTags(ctx, v.UserID)
	if err != nil {
		return nil, 0, err
	}
	args := []any{v.UserID}
	where := []string{"true"}
	add := func(cond string, val any) {
		args = append(args, val)
		where = append(where, strings.ReplaceAll(cond, "?", fmt.Sprintf("$%d", len(args))))
	}
	if f.LibraryID != nil {
		add("a.library_id = ?", *f.LibraryID)
	}
	if f.StudioID != nil {
		add("a.studio_id = ?", *f.StudioID)
	}
	if f.PerformerID != nil {
		add("EXISTS (SELECT 1 FROM adult_scene_performers sp WHERE sp.scene_id = a.id AND sp.performer_id = ?)", *f.PerformerID)
	}
	if f.Kind != "" {
		add("a.kind = ?", f.Kind)
	}
	if f.Tag != "" {
		add("? = ANY (a.tags)", f.Tag)
	}
	if f.Year != nil {
		add("a.year = ?", *f.Year)
	}
	if f.AddedSince != nil {
		add("a.added_at > ?", *f.AddedSince)
	}
	if f.FavoritePerformers {
		add(`EXISTS (SELECT 1 FROM adult_scene_performers sp JOIN adult_favorite_performers fp ON fp.performer_id = sp.performer_id
			WHERE sp.scene_id = a.id AND fp.user_id = ?)`, v.UserID)
	}
	if hideTagged && len(hidden) > 0 {
		add("NOT EXISTS (SELECT 1 FROM unnest(a.tags) tg WHERE lower(tg) = ANY (?::text[]))", lowerAll(hidden))
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		add(`(a.title ILIKE ? OR st.name ILIKE ? OR EXISTS (SELECT 1 FROM adult_scene_performers sp
			JOIN adult_performers p ON p.id = sp.performer_id WHERE sp.scene_id = a.id AND p.name ILIKE ?)
			OR EXISTS (SELECT 1 FROM unnest(a.tags) tg WHERE tg ILIKE ?))`, likePattern(q))
	}
	switch f.Status {
	case "unwatched":
		where = append(where, "NOT COALESCE(pr.played, false)")
	case "inProgress":
		where = append(where, "pr.position_ms > 0 AND NOT pr.played")
	case "watched":
		where = append(where, "pr.played")
	}
	order := "a.release_date DESC NULLS LAST, a.sort_title, a.id"
	switch f.Sort {
	case "added":
		order = "a.added_at DESC, a.id"
	case "title":
		order = "a.sort_title, a.id"
	case "played":
		order = "pr.last_played_at DESC NULLS LAST, a.id"
	case "duration":
		order = "(SELECT max(f.duration_ms) FROM adult_files f WHERE f.scene_id = a.id) DESC NULLS LAST, a.sort_title, a.id"
	case "oldest":
		order = "a.release_date ASC NULLS LAST, a.sort_title, a.id"
	}
	if f.Status == "inProgress" && f.Sort == "" {
		order = "pr.last_played_at DESC NULLS LAST, a.id"
	}
	cond := strings.Join(where, " AND ")
	var total int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*)`+sceneFrom("$1")+` WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 60
	}
	args = append(args, limit, max(f.Offset, 0))
	rows, err := s.Pool.Query(ctx, `SELECT `+sceneCols+sceneFrom("$1")+` WHERE `+cond+` ORDER BY `+order+
		fmt.Sprintf(` LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...) // #nosec G202 -- conditions and order are fixed strings
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Scene
	for rows.Next() {
		sc, err := scanScene(rows)
		if err != nil {
			return nil, 0, err
		}
		sc.Tags = withoutTags(sc.Tags, hidden)
		out = append(out, sc)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, s.attachPerformers(ctx, out)
}

// likePattern is an ILIKE pattern for words anywhere in a text.
func likePattern(q string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(strings.TrimSpace(q)) + "%"
}

func lowerAll(in []string) []string {
	out := make([]string, len(in))
	for i, t := range in {
		out[i] = strings.ToLower(t)
	}
	return out
}

// withoutTags drops hidden tags (case-insensitively).
func withoutTags(tags, hidden []string) []string {
	if len(hidden) == 0 {
		return tags
	}
	h := map[string]bool{}
	for _, t := range hidden {
		h[strings.ToLower(t)] = true
	}
	out := []string{}
	for _, t := range tags {
		if !h[strings.ToLower(t)] {
			out = append(out, t)
		}
	}
	return out
}

func (s *Service) attachPerformers(ctx context.Context, list []Scene) error {
	if len(list) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(list))
	idx := map[uuid.UUID]int{}
	for i, sc := range list {
		ids[i] = sc.ID
		idx[sc.ID] = i
		list[i].Performers = []Ref{}
	}
	rows, err := s.Pool.Query(ctx, `SELECT sp.scene_id, p.id, p.name FROM adult_scene_performers sp
		JOIN adult_performers p ON p.id = sp.performer_id WHERE sp.scene_id = ANY ($1) ORDER BY sp.position, p.name`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var sid uuid.UUID
		var r Ref
		if err := rows.Scan(&sid, &r.ID, &r.Name); err != nil {
			return err
		}
		list[idx[sid]].Performers = append(list[idx[sid]].Performers, r)
	}
	return rows.Err()
}

// Scene returns one scene with its files.
func (s *Service) Scene(ctx context.Context, v Viewer, id uuid.UUID) (Scene, error) {
	sc, err := scanScene(s.Pool.QueryRow(ctx, `SELECT `+sceneCols+sceneFrom("$1")+` WHERE a.id = $2`, v.UserID, id))
	if err != nil {
		return sc, err
	}
	one := []Scene{sc}
	if err := s.attachPerformers(ctx, one); err != nil {
		return sc, err
	}
	sc = one[0]
	hidden, _, err := s.hiddenTags(ctx, v.UserID)
	if err != nil {
		return sc, err
	}
	sc.Tags = withoutTags(sc.Tags, hidden)
	if sc.Files, err = s.files(ctx, id); err != nil {
		return sc, err
	}
	sc.Galleries, _, err = s.Galleries(ctx, v, GalleryFilter{SceneID: &id, Limit: 20})
	return sc, err
}

func (s *Service) files(ctx context.Context, sceneID uuid.UUID) ([]File, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id, path, size, container, video_codec, width, height, duration_ms
		FROM adult_files WHERE scene_id = $1 ORDER BY height DESC NULLS LAST, size DESC`, sceneID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []File{}
	for rows.Next() {
		var f File
		if err := rows.Scan(&f.ID, &f.Path, &f.Size, &f.Container, &f.Codec, &f.Width, &f.Height, &f.DurationMS); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// MediaFiles returns the files of a scene as library media files (for playback).
func (s *Service) MediaFiles(ctx context.Context, sceneID uuid.UUID) ([]library.MediaFile, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id, path, size, container, duration_ms, width, height, video_codec, hdr,
		video_profile, video_level, video_bit_depth, audio, subtitles FROM adult_files
		WHERE scene_id = $1 AND probe_error IS NULL ORDER BY height DESC NULLS LAST, size DESC`, sceneID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []library.MediaFile
	for rows.Next() {
		var f library.MediaFile
		var audio, subs []byte
		if err := rows.Scan(&f.ID, &f.Path, &f.Size, &f.Container, &f.DurationMS, &f.Width, &f.Height, &f.VideoCodec, &f.HDR,
			&f.VideoProfile, &f.VideoLevel, &f.VideoBitDepth, &audio, &subs); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(audio, &f.Audio)
		_ = json.Unmarshal(subs, &f.Subtitles)
		out = append(out, f)
	}
	return out, rows.Err()
}

// FilePath returns the stored path of an adult file (the stream server checks it
// against the media folders again).
func (s *Service) FilePath(ctx context.Context, fileID uuid.UUID) (string, error) {
	var p string
	err := s.Pool.QueryRow(ctx, `SELECT f.path FROM adult_files f JOIN libraries l ON l.id = f.library_id AND l.kind = 'adult'
		WHERE f.id = $1`, fileID).Scan(&p)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return p, err
}

// Position returns the saved resume position of a scene (0 when watched or new).
func (s *Service) Position(ctx context.Context, v Viewer, sceneID uuid.UUID) int64 {
	var pos int64
	_ = s.Pool.QueryRow(ctx, `SELECT position_ms FROM adult_progress WHERE user_id = $1 AND scene_id = $2 AND NOT played`,
		v.UserID, sceneID).Scan(&pos)
	return pos
}

// SaveProgress stores the position; past 90 % (or played) the scene is watched.
func (s *Service) SaveProgress(ctx context.Context, v Viewer, sceneID uuid.UUID, pos int64, dur *int64, played bool) error {
	var exists bool
	if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM adult_scenes WHERE id = $1)`, sceneID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	if dur != nil && *dur > 0 && pos*10 >= *dur*9 {
		played = true
	}
	if played {
		pos = 0
	}
	_, err := s.Pool.Exec(ctx, `INSERT INTO adult_progress (user_id, scene_id, position_ms, played, last_played_at)
		VALUES ($1, $2, $3, $4, now()) ON CONFLICT (user_id, scene_id) DO UPDATE
		SET position_ms = EXCLUDED.position_ms, played = EXCLUDED.played, last_played_at = now()`, v.UserID, sceneID, max(pos, 0), played)
	return err
}

// NamedFilter narrows a studio or performer list.
type NamedFilter struct {
	Query, Sort   string
	Favorites     bool
	Offset, Limit int
}

const studioScenes = `(SELECT count(*)::int FROM adult_scenes a JOIN libraries l ON l.id = a.library_id AND l.kind = 'adult' WHERE a.studio_id = x.id)`
const studioGalleries = `(SELECT count(*)::int FROM adult_galleries g JOIN libraries l ON l.id = g.library_id AND l.kind = 'adult' WHERE g.studio_id = x.id)`
const performerScenes = `(SELECT count(*)::int FROM adult_scene_performers sp JOIN adult_scenes a ON a.id = sp.scene_id
	JOIN libraries l ON l.id = a.library_id AND l.kind = 'adult' WHERE sp.performer_id = x.id)`
const performerGalleries = `(SELECT count(*)::int FROM adult_gallery_performers gp JOIN adult_galleries g ON g.id = gp.gallery_id
	JOIN libraries l ON l.id = g.library_id AND l.kind = 'adult' WHERE gp.performer_id = x.id)`

// Studios returns studios that have scenes or galleries, most first.
func (s *Service) Studios(ctx context.Context, v Viewer, f NamedFilter) ([]Studio, int, error) {
	return listNamed(ctx, s, v, `adult_studios`, studioScenes, studioGalleries, `x.id, x.name, x.has_image, COALESCE(x.url, ''), COALESCE(x.parent, '')`,
		f, false, func(row pgx.Rows) (Studio, error) {
			var st Studio
			err := row.Scan(&st.ID, &st.Name, &st.HasImage, &st.URL, &st.Parent, &st.SceneCount, &st.GalleryCount)
			return st, err
		})
}

// Studio returns one studio.
func (s *Service) Studio(ctx context.Context, id uuid.UUID) (Studio, error) {
	var st Studio
	err := s.Pool.QueryRow(ctx, `SELECT x.id, x.name, x.has_image, COALESCE(x.url, ''), COALESCE(x.parent, ''), `+studioScenes+`, `+studioGalleries+`
		FROM adult_studios x WHERE x.id = $1`, id).Scan(&st.ID, &st.Name, &st.HasImage, &st.URL, &st.Parent, &st.SceneCount, &st.GalleryCount)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && st.SceneCount+st.GalleryCount == 0) {
		return st, ErrNotFound
	}
	return st, err
}

const performerCols = `x.id, x.name, x.has_image, x.aliases, COALESCE(x.gender, ''), COALESCE(x.birthdate, ''),
	COALESCE(x.country, ''), COALESCE(x.description, '')`

func scanPerformer(row pgx.Row) (Performer, error) {
	var p Performer
	err := row.Scan(&p.ID, &p.Name, &p.HasImage, &p.Aliases, &p.Gender, &p.Birthdate, &p.Country, &p.Description,
		&p.Favorite, &p.SceneCount, &p.GalleryCount)
	if p.Aliases == nil {
		p.Aliases = []string{}
	}
	return p, err
}

// Performers returns performers who appear in scenes or galleries, most first.
func (s *Service) Performers(ctx context.Context, v Viewer, f NamedFilter) ([]Performer, int, error) {
	return listNamed(ctx, s, v, `adult_performers`, performerScenes, performerGalleries,
		performerCols+`, EXISTS (SELECT 1 FROM adult_favorite_performers fp WHERE fp.performer_id = x.id AND fp.user_id = $1)`,
		f, true, func(row pgx.Rows) (Performer, error) { return scanPerformer(row) })
}

// Performer returns one performer.
func (s *Service) Performer(ctx context.Context, v Viewer, id uuid.UUID) (Performer, error) {
	p, err := scanPerformer(s.Pool.QueryRow(ctx, `SELECT `+performerCols+`,
		EXISTS (SELECT 1 FROM adult_favorite_performers fp WHERE fp.performer_id = x.id AND fp.user_id = $2), `+performerScenes+`, `+performerGalleries+`
		FROM adult_performers x WHERE x.id = $1`, id, v.UserID))
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && p.SceneCount+p.GalleryCount == 0) {
		return p, ErrNotFound
	}
	return p, err
}

// SetFavorite marks or unmarks a performer as a favourite of the viewer.
func (s *Service) SetFavorite(ctx context.Context, v Viewer, id uuid.UUID, on bool) error {
	if _, err := s.Performer(ctx, v, id); err != nil {
		return err
	}
	var err error
	if on {
		_, err = s.Pool.Exec(ctx, `INSERT INTO adult_favorite_performers (user_id, performer_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, v.UserID, id)
	} else {
		_, err = s.Pool.Exec(ctx, `DELETE FROM adult_favorite_performers WHERE user_id = $1 AND performer_id = $2`, v.UserID, id)
	}
	return err
}

// listNamed lists studios or performers with at least one scene or gallery.
// $1 is always the viewer (performer columns read the favourites with it).
func listNamed[T any](ctx context.Context, s *Service, v Viewer, table, scenes, galleries, cols string, f NamedFilter, favorites bool,
	scan func(pgx.Rows) (T, error)) ([]T, int, error) {
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	where := `(` + scenes + ` + ` + galleries + `) > 0`
	args := []any{v.UserID}
	if q := strings.TrimSpace(f.Query); q != "" {
		args = append(args, likePattern(q))
		where += fmt.Sprintf(` AND (x.name ILIKE $%d`, len(args))
		if favorites {
			where += fmt.Sprintf(` OR EXISTS (SELECT 1 FROM unnest(x.aliases) al WHERE al ILIKE $%d)`, len(args))
		}
		where += `)`
	}
	if favorites && f.Favorites {
		where += ` AND EXISTS (SELECT 1 FROM adult_favorite_performers fp WHERE fp.performer_id = x.id AND fp.user_id = $1)`
	}
	order := `q.n + q.gn DESC, lower(q.name), q.id`
	if f.Sort == "name" {
		order = `lower(q.name), q.id`
	}
	var total int
	// #nosec G202 -- table, link, columns and order are constants of this package
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM `+table+` x WHERE `+where+` AND $1::uuid IS NOT NULL`, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, max(f.Offset, 0))
	// #nosec G202 -- see above
	rows, err := s.Pool.Query(ctx, fmt.Sprintf(`SELECT * FROM (SELECT %s, %s AS n, %s AS gn FROM %s x WHERE %s AND $1::uuid IS NOT NULL) q
		ORDER BY %s LIMIT $%d OFFSET $%d`,
		cols, scenes, galleries, table, where, order, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, v)
	}
	return out, total, rows.Err()
}
