package library

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Errors for the API layer.
var (
	ErrNotFound    = errors.New("not found")
	ErrInvalidPath = errors.New("folder does not exist or is outside the media folders")
	ErrInvalidName = errors.New("a name is required")
)

// Library is a configured library with its scan state.
type Library struct {
	ID         uuid.UUID
	Name       string
	Kind       string
	Paths      []string
	ItemCount  int
	ScanState  string
	ScanError  string
	LastScanAt *time.Time
	FilesSeen  int
}

// Person is a cast or crew member.
type Person struct{ Name, Role string }

// Item is a movie, series, season or episode.
type Item struct {
	HasPoster                         bool
	Rating                            *float64
	ID                                uuid.UUID
	LibraryID                         uuid.UUID
	ParentID                          *uuid.UUID
	Kind, Title                       string
	Year, Season, Episode, EpisodeEnd *int
	DurationMS                        *int64
	AddedAt                           time.Time
	ProviderIDs                       map[string]string
}

// MediaFile is a file belonging to an item.
type MediaFile struct {
	ID            uuid.UUID
	Path, Label   string
	Size          int64
	Container     string
	DurationMS    *int64
	Width, Height *int
	VideoCodec    string
	HDR           string
	Audio         []AudioStream
	Subtitles     []SubtitleStream
}

// Service backs the library API.
type Service struct {
	Pool    *pgxpool.Pool
	Roots   Roots
	Enqueue func(ctx context.Context, libraryID uuid.UUID) error // queue a scan job
	// EnqueueMetadata queues a metadata refresh (force = refetch everything).
	EnqueueMetadata func(ctx context.Context, libraryID uuid.UUID, force bool) error
	// ImageDir holds cached artwork (<dir>/<item id>/<kind>.jpg).
	ImageDir string
}

const libCols = `l.id, l.name, l.kind, l.paths, l.scan_state, COALESCE(l.scan_error, ''), l.last_scan_at, l.files_seen,
	(SELECT count(*) FROM items i WHERE i.library_id = l.id AND i.parent_id IS NULL)`

func scanLibrary(row pgx.Row) (Library, error) {
	var l Library
	err := row.Scan(&l.ID, &l.Name, &l.Kind, &l.Paths, &l.ScanState, &l.ScanError, &l.LastScanAt, &l.FilesSeen, &l.ItemCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return l, ErrNotFound
	}
	return l, err
}

// List returns all libraries by name.
func (s *Service) List(ctx context.Context) ([]Library, error) {
	rows, err := s.Pool.Query(ctx, `SELECT `+libCols+` FROM libraries l ORDER BY lower(l.name)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Library
	for rows.Next() {
		l, err := scanLibrary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Service) validate(name, kind string, paths []string) (string, []string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil, ErrInvalidName
	}
	if kind == "music" {
		return "", nil, ErrUnsupportedKind
	}
	seen := map[string]bool{}
	var clean []string
	for _, p := range paths {
		resolved, err := ResolveSafePath(s.Roots, strings.TrimSpace(p))
		if err != nil || !isDir(resolved) {
			return "", nil, ErrInvalidPath
		}
		if !seen[resolved] {
			seen[resolved] = true
			clean = append(clean, resolved)
		}
	}
	return name, clean, nil
}

// Create adds a library and queues its first scan.
func (s *Service) Create(ctx context.Context, name, kind string, paths []string) (Library, error) {
	name, paths, err := s.validate(name, kind, paths)
	if err != nil {
		return Library{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return Library{}, err
	}
	if _, err := s.Pool.Exec(ctx, `INSERT INTO libraries (id, name, kind, paths) VALUES ($1, $2, $3, $4)`, id, name, kind, paths); err != nil {
		return Library{}, err
	}
	if err := s.Scan(ctx, id); err != nil {
		return Library{}, err
	}
	return s.Get(ctx, id)
}

// Get returns one library.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (Library, error) {
	return scanLibrary(s.Pool.QueryRow(ctx, `SELECT `+libCols+` FROM libraries l WHERE l.id = $1`, id))
}

// Update changes name, kind or folders and rescans.
func (s *Service) Update(ctx context.Context, id uuid.UUID, name, kind string, paths []string) (Library, error) {
	name, paths, err := s.validate(name, kind, paths)
	if err != nil {
		return Library{}, err
	}
	tag, err := s.Pool.Exec(ctx, `UPDATE libraries SET name = $2, kind = $3, paths = $4 WHERE id = $1`, id, name, kind, paths)
	if err != nil {
		return Library{}, err
	}
	if tag.RowsAffected() == 0 {
		return Library{}, ErrNotFound
	}
	if err := s.Scan(ctx, id); err != nil {
		return Library{}, err
	}
	return s.Get(ctx, id)
}

// Delete removes the library and its items from the database. Files are never touched.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := s.Pool.Exec(ctx, `DELETE FROM libraries WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Scan queues a scan job (a queued or running scan is not duplicated).
func (s *Service) Scan(ctx context.Context, id uuid.UUID) error {
	tag, err := s.Pool.Exec(ctx, `UPDATE libraries SET scan_state = 'queued' WHERE id = $1 AND scan_state NOT IN ('queued', 'scanning')`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM libraries WHERE id = $1)`, id).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		return nil // already queued or running
	}
	return s.Enqueue(ctx, id)
}

const itemCols = `i.id, i.library_id, i.parent_id, i.kind, COALESCE(i.metadata_title, i.title), i.year, i.season_number,
	i.episode_number, i.episode_number_end, i.added_at, i.provider_ids,
	(SELECT max(f.duration_ms) FROM media_files f WHERE f.item_id = i.id), i.has_poster, i.community_rating`

func scanItem(row pgx.Row) (Item, error) {
	var it Item
	var ids []byte
	err := row.Scan(&it.ID, &it.LibraryID, &it.ParentID, &it.Kind, &it.Title, &it.Year, &it.Season, &it.Episode,
		&it.EpisodeEnd, &it.AddedAt, &ids, &it.DurationMS, &it.HasPoster, &it.Rating)
	if errors.Is(err, pgx.ErrNoRows) {
		return it, ErrNotFound
	}
	_ = json.Unmarshal(ids, &it.ProviderIDs)
	return it, err
}

// ListItems returns a page of top-level items (movies, series).
func (s *Service) ListItems(ctx context.Context, libID uuid.UUID, sortBy string, limit, offset int) ([]Item, int, error) {
	order := "i.sort_title, i.year NULLS LAST"
	switch sortBy {
	case "year":
		order = "i.year DESC NULLS LAST, i.sort_title"
	case "added":
		order = "i.added_at DESC, i.sort_title"
	}
	var total int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM items i WHERE i.library_id = $1 AND i.parent_id IS NULL`, libID).Scan(&total); err != nil {
		return nil, 0, err
	}
	// #nosec G202 -- order is one of three constants above
	rows, err := s.Pool.Query(ctx, `SELECT `+itemCols+` FROM items i WHERE i.library_id = $1 AND i.parent_id IS NULL
		ORDER BY `+order+` LIMIT $2 OFFSET $3`, libID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, it)
	}
	return out, total, rows.Err()
}

// GetItem returns an item with its files and children.
func (s *Service) GetItem(ctx context.Context, id uuid.UUID) (Item, []MediaFile, []Item, error) {
	it, err := scanItem(s.Pool.QueryRow(ctx, `SELECT `+itemCols+` FROM items i WHERE i.id = $1`, id))
	if err != nil {
		return it, nil, nil, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT id, path, label, size, container, duration_ms, width, height, video_codec, hdr,
		audio, subtitles FROM media_files WHERE item_id = $1 ORDER BY height DESC NULLS LAST, path`, id)
	if err != nil {
		return it, nil, nil, err
	}
	var files []MediaFile
	for rows.Next() {
		var f MediaFile
		var audio, subs []byte
		if err := rows.Scan(&f.ID, &f.Path, &f.Label, &f.Size, &f.Container, &f.DurationMS, &f.Width, &f.Height,
			&f.VideoCodec, &f.HDR, &audio, &subs); err != nil {
			rows.Close()
			return it, nil, nil, err
		}
		_ = json.Unmarshal(audio, &f.Audio)
		_ = json.Unmarshal(subs, &f.Subtitles)
		files = append(files, f)
	}
	rows.Close()
	rows, err = s.Pool.Query(ctx, `SELECT `+itemCols+` FROM items i WHERE i.parent_id = $1
		ORDER BY i.season_number NULLS LAST, i.episode_number NULLS LAST, i.sort_title`, id)
	if err != nil {
		return it, nil, nil, err
	}
	defer rows.Close()
	var children []Item
	for rows.Next() {
		c, err := scanItem(rows)
		if err != nil {
			return it, nil, nil, err
		}
		children = append(children, c)
	}
	return it, files, children, rows.Err()
}

// Browse lists sub-folders of path (or the media roots when path is empty).
func (s *Service) Browse(path string) (string, string, []string, error) {
	if path == "" {
		var roots []string
		for _, r := range s.Roots {
			if isDir(r) {
				roots = append(roots, r)
			}
		}
		return "", "", roots, nil
	}
	resolved, err := ResolveSafePath(s.Roots, path)
	if err != nil || !isDir(resolved) {
		return "", "", nil, ErrInvalidPath
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return "", "", nil, ErrInvalidPath
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() && !IgnoredName(e.Name()) {
			dirs = append(dirs, filepath.Join(resolved, e.Name()))
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return strings.ToLower(dirs[i]) < strings.ToLower(dirs[j]) })
	parent := ""
	if p := filepath.Dir(resolved); p != resolved {
		if _, err := ResolveSafePath(s.Roots, p); err == nil {
			parent = p
		}
	}
	return resolved, parent, dirs, nil
}

// ItemMetadata is the descriptive part of an item.
type ItemMetadata struct {
	Overview, Tagline, OriginalTitle, OfficialRating, Error string
	Genres                                                  []string
	Premiere                                                *time.Time
	HasBackdrop                                             bool
	Cast, Crew                                              []Person
}

// GetMetadata returns the description, people and artwork flags of an item.
func (s *Service) GetMetadata(ctx context.Context, id uuid.UUID) (ItemMetadata, error) {
	var m ItemMetadata
	err := s.Pool.QueryRow(ctx, `SELECT COALESCE(overview, ''), COALESCE(tagline, ''), COALESCE(original_title, ''),
			COALESCE(official_rating, ''), COALESCE(metadata_error, ''), genres, premiere_date, has_backdrop
		FROM items WHERE id = $1`, id).Scan(&m.Overview, &m.Tagline, &m.OriginalTitle, &m.OfficialRating, &m.Error,
		&m.Genres, &m.Premiere, &m.HasBackdrop)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, ErrNotFound
	}
	if err != nil {
		return m, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT kind, name, role FROM item_people WHERE item_id = $1 ORDER BY kind, position`, id)
	if err != nil {
		return m, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var p Person
		if err := rows.Scan(&kind, &p.Name, &p.Role); err != nil {
			return m, err
		}
		if kind == "cast" {
			m.Cast = append(m.Cast, p)
		} else {
			m.Crew = append(m.Crew, p)
		}
	}
	return m, rows.Err()
}
