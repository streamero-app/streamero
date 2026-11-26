package adult

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Gallery is a set of pictures in a list.
type Gallery struct {
	ID, LibraryID uuid.UUID
	Title         string
	Date          *time.Time
	Year          *int
	Studio        *Ref
	Scene         *Ref
	Performers    []Ref
	Tags          []string
	ImageCount    int
	CoverImageID  *uuid.UUID
	AddedAt       time.Time
	Archive       bool
	Images        []GalleryImage
}

// GalleryImage is one picture of a gallery.
type GalleryImage struct {
	ID            uuid.UUID
	Position      int
	Width, Height *int
	Name          string
}

// GalleryFilter narrows a gallery list.
type GalleryFilter struct {
	LibraryID, StudioID, PerformerID, SceneID *uuid.UUID
	Query, Tag, Sort                          string
	Year                                      *int
	Offset, Limit                             int
}

const galleryCols = `g.id, g.library_id, g.title, g.release_date, g.year, g.studio_id, st.name, g.scene_id, sc.title, g.tags,
	g.image_count, g.cover_image_id, g.added_at, g.archive`

const galleryFrom = ` FROM adult_galleries g
	JOIN libraries l ON l.id = g.library_id AND l.kind = 'adult'
	LEFT JOIN adult_studios st ON st.id = g.studio_id
	LEFT JOIN adult_scenes sc ON sc.id = g.scene_id `

func scanGallery(row pgx.Row) (Gallery, error) {
	var g Gallery
	var studioID, sceneID *uuid.UUID
	var studio, scene *string
	err := row.Scan(&g.ID, &g.LibraryID, &g.Title, &g.Date, &g.Year, &studioID, &studio, &sceneID, &scene, &g.Tags,
		&g.ImageCount, &g.CoverImageID, &g.AddedAt, &g.Archive)
	if errors.Is(err, pgx.ErrNoRows) {
		return g, ErrNotFound
	}
	if err != nil {
		return g, err
	}
	if studioID != nil && studio != nil {
		g.Studio = &Ref{ID: *studioID, Name: *studio}
	}
	if sceneID != nil && scene != nil {
		g.Scene = &Ref{ID: *sceneID, Name: *scene}
	}
	if g.Tags == nil {
		g.Tags = []string{}
	}
	return g, nil
}

// Galleries returns a page of galleries for a viewer.
func (s *Service) Galleries(ctx context.Context, v Viewer, f GalleryFilter) ([]Gallery, int, error) {
	hidden, hideTagged, err := s.hiddenTags(ctx, v.UserID)
	if err != nil {
		return nil, 0, err
	}
	var args []any
	where := []string{"g.image_count > 0"}
	add := func(cond string, val any) {
		args = append(args, val)
		where = append(where, strings.ReplaceAll(cond, "?", fmt.Sprintf("$%d", len(args))))
	}
	if f.LibraryID != nil {
		add("g.library_id = ?", *f.LibraryID)
	}
	if f.StudioID != nil {
		add("g.studio_id = ?", *f.StudioID)
	}
	if f.SceneID != nil {
		add("g.scene_id = ?", *f.SceneID)
	}
	if f.PerformerID != nil {
		add("EXISTS (SELECT 1 FROM adult_gallery_performers gp WHERE gp.gallery_id = g.id AND gp.performer_id = ?)", *f.PerformerID)
	}
	if f.Tag != "" {
		add("? = ANY (g.tags)", f.Tag)
	}
	if f.Year != nil {
		add("g.year = ?", *f.Year)
	}
	if hideTagged && len(hidden) > 0 {
		add("NOT EXISTS (SELECT 1 FROM unnest(g.tags) tg WHERE lower(tg) = ANY (?::text[]))", lowerAll(hidden))
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		add(`(g.title ILIKE ? OR st.name ILIKE ? OR EXISTS (SELECT 1 FROM adult_gallery_performers gp
			JOIN adult_performers p ON p.id = gp.performer_id WHERE gp.gallery_id = g.id AND p.name ILIKE ?)
			OR EXISTS (SELECT 1 FROM unnest(g.tags) tg WHERE tg ILIKE ?))`, likePattern(q))
	}
	order := "g.release_date DESC NULLS LAST, g.sort_title, g.id"
	switch f.Sort {
	case "added":
		order = "g.added_at DESC, g.id"
	case "title":
		order = "g.sort_title, g.id"
	case "images":
		order = "g.image_count DESC, g.sort_title, g.id"
	case "oldest":
		order = "g.release_date ASC NULLS LAST, g.sort_title, g.id"
	}
	cond := strings.Join(where, " AND ")
	var total int
	// #nosec G202 -- conditions and order are fixed strings
	if err := s.Pool.QueryRow(ctx, `SELECT count(*)`+galleryFrom+` WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 60
	}
	args = append(args, limit, max(f.Offset, 0))
	rows, err := s.Pool.Query(ctx, `SELECT `+galleryCols+galleryFrom+` WHERE `+cond+` ORDER BY `+order+
		fmt.Sprintf(` LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...) // #nosec G202 -- see above
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Gallery{}
	for rows.Next() {
		g, err := scanGallery(rows)
		if err != nil {
			return nil, 0, err
		}
		g.Tags = withoutTags(g.Tags, hidden)
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, s.attachGalleryPerformers(ctx, out)
}

func (s *Service) attachGalleryPerformers(ctx context.Context, list []Gallery) error {
	if len(list) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(list))
	idx := map[uuid.UUID]int{}
	for i, g := range list {
		ids[i] = g.ID
		idx[g.ID] = i
		list[i].Performers = []Ref{}
	}
	rows, err := s.Pool.Query(ctx, `SELECT gp.gallery_id, p.id, p.name FROM adult_gallery_performers gp
		JOIN adult_performers p ON p.id = gp.performer_id WHERE gp.gallery_id = ANY ($1) ORDER BY gp.position, p.name`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var gid uuid.UUID
		var r Ref
		if err := rows.Scan(&gid, &r.ID, &r.Name); err != nil {
			return err
		}
		list[idx[gid]].Performers = append(list[idx[gid]].Performers, r)
	}
	return rows.Err()
}

// Gallery returns one gallery with its pictures in order.
func (s *Service) Gallery(ctx context.Context, v Viewer, id uuid.UUID) (Gallery, error) {
	g, err := scanGallery(s.Pool.QueryRow(ctx, `SELECT `+galleryCols+galleryFrom+` WHERE g.id = $1`, id))
	if err != nil {
		return g, err
	}
	hidden, hideTagged, err := s.hiddenTags(ctx, v.UserID)
	if err != nil {
		return g, err
	}
	if hideTagged && len(withoutTags(g.Tags, hidden)) != len(g.Tags) {
		return g, ErrNotFound
	}
	g.Tags = withoutTags(g.Tags, hidden)
	one := []Gallery{g}
	if err := s.attachGalleryPerformers(ctx, one); err != nil {
		return g, err
	}
	g = one[0]
	rows, err := s.Pool.Query(ctx, `SELECT id, position, width, height, orientation, path, entry FROM adult_images
		WHERE gallery_id = $1 ORDER BY position, id`, id)
	if err != nil {
		return g, err
	}
	defer rows.Close()
	g.Images = []GalleryImage{}
	for rows.Next() {
		var im GalleryImage
		var o int
		var p, entry string
		if err := rows.Scan(&im.ID, &im.Position, &im.Width, &im.Height, &o, &p, &entry); err != nil {
			return g, err
		}
		if o >= 5 { // shown rotated by a quarter turn
			im.Width, im.Height = im.Height, im.Width
		}
		im.Name = baseName(p, entry)
		g.Images = append(g.Images, im)
	}
	return g, rows.Err()
}

func baseName(p, entry string) string {
	if entry != "" {
		p = strings.ReplaceAll(entry, "\\", "/")
	}
	if i := strings.LastIndexAny(p, "/\\"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// Tag is a tag with how many scenes and galleries carry it.
type Tag struct {
	Name                     string
	SceneCount, GalleryCount int
	Hidden                   bool
}

// TagFilter narrows the tag list.
type TagFilter struct {
	Query, Sort   string
	IncludeHidden bool
	Offset, Limit int
}

// Tags lists the tags of scenes and galleries (from ThePornDB / StashDB),
// leaving out the viewer's hidden tags unless asked for.
func (s *Service) Tags(ctx context.Context, v Viewer, f TagFilter) ([]Tag, int, error) {
	hidden, _, err := s.hiddenTags(ctx, v.UserID)
	if err != nil {
		return nil, 0, err
	}
	lower := make([]string, len(hidden))
	for i, h := range hidden {
		lower[i] = strings.ToLower(h)
	}
	args := []any{lower}
	where := "true"
	if !f.IncludeHidden {
		where += " AND NOT (lower(t.name) = ANY ($1::text[]))"
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		args = append(args, likePattern(q))
		where += fmt.Sprintf(" AND t.name ILIKE $%d", len(args))
	}
	base := `WITH t AS (
		SELECT name, sum(sc)::int AS sc, sum(gc)::int AS gc FROM (
			SELECT unnest(a.tags) AS name, 1 AS sc, 0 AS gc FROM adult_scenes a JOIN libraries l ON l.id = a.library_id AND l.kind = 'adult'
			UNION ALL
			SELECT unnest(g.tags), 0, 1 FROM adult_galleries g JOIN libraries l ON l.id = g.library_id AND l.kind = 'adult'
		) x GROUP BY name)
		SELECT t.name, t.sc, t.gc, lower(t.name) = ANY ($1::text[]) FROM t WHERE ` + where
	var total int
	// #nosec G202 -- fixed strings
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM (`+base+`) c`, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	order := "t.sc + t.gc DESC, lower(t.name)"
	if f.Sort == "name" {
		order = "lower(t.name)"
	}
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	args = append(args, limit, max(f.Offset, 0))
	// #nosec G202 -- fixed strings
	rows, err := s.Pool.Query(ctx, base+fmt.Sprintf(` ORDER BY %s LIMIT $%d OFFSET $%d`, order, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Tag{}
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.Name, &t.SceneCount, &t.GalleryCount, &t.Hidden); err != nil {
			return nil, 0, err
		}
		out = append(out, t)
	}
	return out, total, rows.Err()
}

// Home is the start page of the adult section.
type Home struct {
	Continue, RecentlyAdded, RecentlyReleased, FavoritePerformers []Scene
	RecentGalleries                                              []Gallery
}

// Home returns the rows of the adult start page: only adult progress and
// adult favourites are read.
func (s *Service) Home(ctx context.Context, v Viewer) (Home, error) {
	var h Home
	var err error
	if h.Continue, _, err = s.Scenes(ctx, v, Filter{Status: "inProgress", Limit: 12}); err != nil {
		return h, err
	}
	if h.RecentlyAdded, _, err = s.Scenes(ctx, v, Filter{Sort: "added", Limit: 18}); err != nil {
		return h, err
	}
	if h.RecentlyReleased, _, err = s.Scenes(ctx, v, Filter{Limit: 18}); err != nil {
		return h, err
	}
	if h.FavoritePerformers, _, err = s.Scenes(ctx, v, Filter{FavoritePerformers: true, Limit: 18}); err != nil {
		return h, err
	}
	if h.RecentGalleries, _, err = s.Galleries(ctx, v, GalleryFilter{Sort: "added", Limit: 18}); err != nil {
		return h, err
	}
	return h, nil
}

// SearchResult is what a search inside the adult section finds.
type SearchResult struct {
	Scenes     []Scene
	Galleries  []Gallery
	Performers []Performer
	Studios    []Studio
	Tags       []Tag
}

// Search looks for words in scenes, galleries, performers, studios and tags.
func (s *Service) Search(ctx context.Context, v Viewer, q string, limit int) (SearchResult, error) {
	var r SearchResult
	if strings.TrimSpace(q) == "" {
		return SearchResult{Scenes: []Scene{}, Galleries: []Gallery{}, Performers: []Performer{}, Studios: []Studio{}, Tags: []Tag{}}, nil
	}
	if limit <= 0 || limit > 50 {
		limit = 12
	}
	var err error
	if r.Scenes, _, err = s.Scenes(ctx, v, Filter{Query: q, Limit: limit}); err != nil {
		return r, err
	}
	if r.Scenes == nil {
		r.Scenes = []Scene{}
	}
	if r.Galleries, _, err = s.Galleries(ctx, v, GalleryFilter{Query: q, Limit: limit}); err != nil {
		return r, err
	}
	if r.Performers, _, err = s.Performers(ctx, v, NamedFilter{Query: q, Limit: limit}); err != nil {
		return r, err
	}
	if r.Studios, _, err = s.Studios(ctx, v, NamedFilter{Query: q, Limit: limit}); err != nil {
		return r, err
	}
	r.Tags, _, err = s.Tags(ctx, v, TagFilter{Query: q, Limit: limit})
	return r, err
}
