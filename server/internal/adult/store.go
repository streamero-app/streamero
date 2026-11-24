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
	ID         uuid.UUID
	Name       string
	SceneCount int
	HasImage   bool
	URL        string
	Parent     string
}

// Performer is a performer with their scene count.
type Performer struct {
	ID                                      uuid.UUID
	Name                                    string
	SceneCount                              int
	HasImage                                bool
	Aliases                                 []string
	Gender, Birthdate, Country, Description string
}

// Filter narrows a scene list.
type Filter struct {
	LibraryID, StudioID, PerformerID *uuid.UUID
	Kind, Query, Tag, Status, Sort   string
	Year                             *int
	Offset, Limit                    int
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
	if q := strings.TrimSpace(f.Query); q != "" {
		like := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(q) + "%"
		add(`(a.title ILIKE ? OR st.name ILIKE ? OR EXISTS (SELECT 1 FROM adult_scene_performers sp
			JOIN adult_performers p ON p.id = sp.performer_id WHERE sp.scene_id = a.id AND p.name ILIKE ?))`, like)
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
		out = append(out, sc)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, s.attachPerformers(ctx, out)
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
	sc.Files, err = s.files(ctx, id)
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

// Studios returns studios that have scenes, most scenes first.
func (s *Service) Studios(ctx context.Context, q string, offset, limit int) ([]Studio, int, error) {
	return listNamed(ctx, s, `adult_studios`, `a.studio_id = x.id`, `x.id, x.name, x.has_image, COALESCE(x.url, ''), COALESCE(x.parent, '')`,
		q, offset, limit, func(row pgx.Rows) (Studio, error) {
			var st Studio
			err := row.Scan(&st.ID, &st.Name, &st.HasImage, &st.URL, &st.Parent, &st.SceneCount)
			return st, err
		})
}

// Studio returns one studio.
func (s *Service) Studio(ctx context.Context, id uuid.UUID) (Studio, error) {
	var st Studio
	err := s.Pool.QueryRow(ctx, `SELECT x.id, x.name, x.has_image, COALESCE(x.url, ''), COALESCE(x.parent, ''),
		(SELECT count(*)::int FROM adult_scenes a JOIN libraries l ON l.id = a.library_id AND l.kind = 'adult' WHERE a.studio_id = x.id)
		FROM adult_studios x WHERE x.id = $1`, id).Scan(&st.ID, &st.Name, &st.HasImage, &st.URL, &st.Parent, &st.SceneCount)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && st.SceneCount == 0) {
		return st, ErrNotFound
	}
	return st, err
}

const performerCols = `x.id, x.name, x.has_image, x.aliases, COALESCE(x.gender, ''), COALESCE(x.birthdate, ''),
	COALESCE(x.country, ''), COALESCE(x.description, '')`

func scanPerformer(row pgx.Row, withCount bool) (Performer, error) {
	var p Performer
	dst := []any{&p.ID, &p.Name, &p.HasImage, &p.Aliases, &p.Gender, &p.Birthdate, &p.Country, &p.Description}
	if withCount {
		dst = append(dst, &p.SceneCount)
	}
	err := row.Scan(dst...)
	if p.Aliases == nil {
		p.Aliases = []string{}
	}
	return p, err
}

// Performers returns performers who appear in scenes, most scenes first.
func (s *Service) Performers(ctx context.Context, q string, offset, limit int) ([]Performer, int, error) {
	return listNamed(ctx, s, `adult_performers`,
		`EXISTS (SELECT 1 FROM adult_scene_performers sp WHERE sp.scene_id = a.id AND sp.performer_id = x.id)`,
		performerCols, q, offset, limit, func(row pgx.Rows) (Performer, error) { return scanPerformer(row, true) })
}

// Performer returns one performer.
func (s *Service) Performer(ctx context.Context, id uuid.UUID) (Performer, error) {
	p, err := scanPerformer(s.Pool.QueryRow(ctx, `SELECT `+performerCols+`,
		(SELECT count(*)::int FROM adult_scene_performers sp JOIN adult_scenes a ON a.id = sp.scene_id
			JOIN libraries l ON l.id = a.library_id AND l.kind = 'adult' WHERE sp.performer_id = x.id)
		FROM adult_performers x WHERE x.id = $1`, id), true)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && p.SceneCount == 0) {
		return p, ErrNotFound
	}
	return p, err
}

// listNamed lists studios or performers with at least one scene.
func listNamed[T any](ctx context.Context, s *Service, table, link, cols, q string, offset, limit int, scan func(pgx.Rows) (T, error)) ([]T, int, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	count := `(SELECT count(*)::int FROM adult_scenes a JOIN libraries l ON l.id = a.library_id AND l.kind = 'adult' WHERE ` + link + `)`
	where := count + ` > 0`
	args := []any{}
	if q = strings.TrimSpace(q); q != "" {
		args = append(args, "%"+strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(q)+"%")
		where += ` AND x.name ILIKE $1`
	}
	var total int
	// #nosec G202 -- table, link and columns are constants of this package
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM `+table+` x WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, max(offset, 0))
	// #nosec G202 -- see above
	rows, err := s.Pool.Query(ctx, fmt.Sprintf(`SELECT %s, %s AS n FROM %s x WHERE %s ORDER BY n DESC, lower(x.name), x.id LIMIT $%d OFFSET $%d`,
		cols, count, table, where, len(args)-1, len(args)), args...)
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
