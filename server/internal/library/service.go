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
// Person is a cast or crew credit; ImageID is the TMDb person id when a photo is cached.
type Person struct {
	Name, Role string
	ImageID    *int64
}

// Item is a movie, series, season or episode.
type Item struct {
	HasPoster                         bool
	HasBackdrop                       bool
	HasLogo                           bool
	SeriesID                          *uuid.UUID
	SeriesTitle                       string
	Colors                            []string
	Rating                            *float64
	VideoHeight                       *int
	HDR                               *string
	Artist, Album                     *string // music: the performer, and a track's album
	PositionMS                        int64
	Played                            bool
	Favorite                          bool
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
	// VideoProfile, VideoLevel and VideoBitDepth: "" / 0 when unknown (scanned before they were recorded).
	VideoProfile  string
	VideoLevel    int
	VideoBitDepth int
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
	// Resume returns the administrator's resume thresholds (defaults when nil).
	Resume func(ctx context.Context) Resume
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

// RetryFailedMetadata lets the next metadata pass try again the titles that found
// nothing before (for example before a TMDb key was set or while TMDb was unreachable).
func (s *Service) RetryFailedMetadata(ctx context.Context) error {
	_, err := s.Pool.Exec(ctx, `UPDATE items SET metadata_refreshed_at = NULL
		WHERE metadata_error IS NOT NULL AND NOT metadata_locked AND parent_id IS NULL`)
	return err
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

func (s *Service) validate(name, _ string, paths []string) (string, []string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil, ErrInvalidName
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
	tag, err := s.Pool.Exec(ctx, `UPDATE libraries SET scan_state = CASE WHEN scan_state = 'scanning' THEN scan_state ELSE 'queued' END
		WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	// Always enqueue: the job queue drops duplicates while one is waiting, and a
	// state left at "queued" by a lost job must never block scanning for good.
	return s.Enqueue(ctx, id)
}

const itemCols = `i.id, i.library_id, i.parent_id, i.kind, COALESCE(i.metadata_title, i.title), i.year, i.season_number,
	i.episode_number, i.episode_number_end, i.added_at, i.provider_ids,
	(SELECT max(f.duration_ms) FROM media_files f WHERE f.item_id = i.id), i.has_poster, i.has_backdrop, i.has_logo, i.colors, i.community_rating,
	COALESCE(u.position_ms, 0), COALESCE(u.played, false), COALESCE(u.favorite, false),
	(SELECT max(f.height) FROM media_files f WHERE f.item_id = i.id),
	(SELECT f.hdr FROM media_files f WHERE f.item_id = i.id AND f.hdr <> '' ORDER BY f.height DESC NULLS LAST LIMIT 1),
	CASE i.kind WHEN 'album' THEN (SELECT p.title FROM items p WHERE p.id = i.parent_id)
		WHEN 'track' THEN COALESCE(i.artist, (SELECT g.title FROM items a JOIN items g ON g.id = a.parent_id WHERE a.id = i.parent_id)) END,
	CASE WHEN i.kind = 'track' THEN (SELECT a.title FROM items a WHERE a.id = i.parent_id) END`

// userJoin attaches the viewer's playback state ($owner is the profile or user id).
func userJoin(owner string) string {
	return ` LEFT JOIN user_item_data u ON u.item_id = i.id AND u.owner_id = ` + owner + ` `
}

func scanItem(row pgx.Row) (Item, error) {
	var it Item
	var ids []byte
	err := row.Scan(&it.ID, &it.LibraryID, &it.ParentID, &it.Kind, &it.Title, &it.Year, &it.Season, &it.Episode,
		&it.EpisodeEnd, &it.AddedAt, &ids, &it.DurationMS, &it.HasPoster, &it.HasBackdrop, &it.HasLogo, &it.Colors, &it.Rating, &it.PositionMS, &it.Played, &it.Favorite,
		&it.VideoHeight, &it.HDR, &it.Artist, &it.Album)
	if errors.Is(err, pgx.ErrNoRows) {
		return it, ErrNotFound
	}
	_ = json.Unmarshal(ids, &it.ProviderIDs)
	return it, err
}

// ItemFilter narrows a library listing.
type ItemFilter struct {
	Genre            string
	Unplayed         bool
	Favorite         bool
	Resumable        bool
	YearFrom, YearTo int    // 0 = open
	Letter           string // "A".."Z", "#" for anything else, "" = all
	Kind             string // music: artist, album or track instead of top-level items
	// ExcludeRatings hides items with one of these official ratings (parental limits, see RatingAge).
	ExcludeRatings []string
}

// ListItems returns a filtered page of top-level items (movies, series) with the viewer's state.
func (s *Service) ListItems(ctx context.Context, libID, owner uuid.UUID, sortBy string, limit, offset int, f ItemFilter) ([]Item, int, error) {
	order := "i.sort_title, i.year NULLS LAST"
	switch sortBy {
	case "year":
		order = "i.year DESC NULLS LAST, i.sort_title"
	case "added":
		order = "i.added_at DESC, i.sort_title"
	case "rating":
		order = "i.community_rating DESC NULLS LAST, i.sort_title"
	case "random":
		order = "random()"
	}
	if f.Kind == "track" && sortBy == "title" {
		order = "(SELECT a.sort_title FROM items a WHERE a.id = i.parent_id), i.parent_id, i.season_number NULLS FIRST, i.episode_number NULLS LAST, i.sort_title"
	}
	where := ` WHERE i.library_id = $1 AND (($10 = '' AND i.parent_id IS NULL) OR i.kind = $10)
		AND ($2 = '' OR i.genres @> ARRAY[$2]::text[]) AND (NOT $3 OR NOT COALESCE(u.played, false))
		AND (NOT $5 OR COALESCE(u.favorite, false)) AND (NOT $6 OR (COALESCE(u.position_ms, 0) > 0 AND NOT COALESCE(u.played, false)))
		AND ($7 = 0 OR i.year >= $7) AND ($8 = 0 OR i.year <= $8)
		AND ($9 = '' OR ($9 = '#' AND upper(left(i.sort_title, 1)) !~ '^[A-ZČĆĐŠŽ]') OR upper(left(i.sort_title, 1)) = $9)
		AND (cardinality($11::text[]) = 0 OR i.official_rating IS NULL OR NOT i.official_rating = ANY($11::text[]))`
	excl := f.ExcludeRatings
	if excl == nil {
		excl = []string{}
	}
	args := []any{libID, f.Genre, f.Unplayed, owner, f.Favorite, f.Resumable, f.YearFrom, f.YearTo, strings.ToUpper(f.Letter), f.Kind, excl}
	var total int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM items i`+userJoin("$4")+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	// #nosec G202 -- order is one of the constants above
	rows, err := s.Pool.Query(ctx, `SELECT `+itemCols+` FROM items i`+userJoin("$4")+where+`
		ORDER BY `+order+` LIMIT $12 OFFSET $13`, append(args, limit, offset)...)
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
func (s *Service) GetItem(ctx context.Context, id, owner uuid.UUID) (Item, []MediaFile, []Item, error) {
	it, err := scanItem(s.Pool.QueryRow(ctx, `SELECT `+itemCols+` FROM items i`+userJoin("$2")+`WHERE i.id = $1`, id, owner))
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
	rows, err = s.Pool.Query(ctx, `SELECT `+itemCols+` FROM items i`+userJoin("$2")+`WHERE i.parent_id = $1
		ORDER BY i.season_number NULLS LAST, i.episode_number NULLS LAST, i.sort_title`, id, owner)
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
	Genres, Studios                                         []string
	Premiere                                                *time.Time
	HasBackdrop                                             bool
	HasLogo                                                 bool
	Locked                                                  bool
	Cast, Crew                                              []Person
}

// GetMetadata returns the description, people and artwork flags of an item.
func (s *Service) GetMetadata(ctx context.Context, id uuid.UUID) (ItemMetadata, error) {
	var m ItemMetadata
	err := s.Pool.QueryRow(ctx, `SELECT COALESCE(overview, ''), COALESCE(tagline, ''), COALESCE(original_title, ''),
			COALESCE(official_rating, ''), COALESCE(metadata_error, ''), genres, studios, premiere_date, has_backdrop, has_logo, metadata_locked
		FROM items WHERE id = $1`, id).Scan(&m.Overview, &m.Tagline, &m.OriginalTitle, &m.OfficialRating, &m.Error,
		&m.Genres, &m.Studios, &m.Premiere, &m.HasBackdrop, &m.HasLogo, &m.Locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, ErrNotFound
	}
	if err != nil {
		return m, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT kind, name, role, CASE WHEN has_image THEN tmdb_person_id END
		FROM item_people WHERE item_id = $1 ORDER BY kind, position`, id)
	if err != nil {
		return m, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var p Person
		if err := rows.Scan(&kind, &p.Name, &p.Role, &p.ImageID); err != nil {
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

// Resume says when a stopped title keeps its position and when it counts as watched.
type Resume struct {
	MinPercent         int `json:"minResumePercent"`
	MaxPercent         int `json:"maxResumePercent"`
	MinDurationSeconds int `json:"minResumeDurationSeconds"`
}

// KeyResume is the settings key of the resume thresholds.
const KeyResume = "playback.resume"

// DefaultResume keeps positions between 5% and 90%, for titles of five minutes or more.
func DefaultResume() Resume { return Resume{MinPercent: 5, MaxPercent: 90, MinDurationSeconds: 300} }

// SaveProgress records a playback position for owner (profile or user). Past the
// resume maximum (90% by default) the item counts as watched and the position resets.
func (s *Service) SaveProgress(ctx context.Context, owner, itemID uuid.UUID, positionMS int64, durationMS *int64, played bool) error {
	_, err := s.saveProgress(ctx, owner, itemID, positionMS, durationMS, played, nil)
	return err
}

// SaveProgressAt records a position that was reached at a given time (offline
// playback, D-040). It only replaces the stored state when that is older:
// newest wins. applied is false when the stored state was newer.
func (s *Service) SaveProgressAt(ctx context.Context, owner, itemID uuid.UUID, positionMS int64, durationMS *int64, played bool, at time.Time) (applied bool, err error) {
	return s.saveProgress(ctx, owner, itemID, positionMS, durationMS, played, &at)
}

func (s *Service) saveProgress(ctx context.Context, owner, itemID uuid.UUID, positionMS int64, durationMS *int64, played bool, at *time.Time) (bool, error) {
	var dur int64
	if durationMS != nil {
		dur = *durationMS
	} else {
		_ = s.Pool.QueryRow(ctx, `SELECT COALESCE(max(duration_ms), 0) FROM media_files WHERE item_id = $1`, itemID).Scan(&dur)
	}
	r := DefaultResume()
	if s.Resume != nil {
		r = s.Resume(ctx)
	}
	if dur > 0 && positionMS*100 >= dur*int64(r.MaxPercent) {
		played = true
	}
	// Too early, or too short a title, to be worth resuming.
	if !played && (dur > 0 && positionMS*100 < dur*int64(r.MinPercent) || dur > 0 && dur < int64(r.MinDurationSeconds)*1000) {
		positionMS = 0
	}
	if played {
		positionMS = 0
	}
	if at == nil {
		tag, err := s.Pool.Exec(ctx, `INSERT INTO user_item_data (owner_id, item_id, position_ms, played, play_count, last_played_at)
			SELECT $1, $2, $3, $4, CASE WHEN $4 THEN 1 ELSE 0 END, now() WHERE EXISTS (SELECT 1 FROM items WHERE id = $2)
			ON CONFLICT (owner_id, item_id) DO UPDATE SET position_ms = EXCLUDED.position_ms, last_played_at = now(),
				play_count = user_item_data.play_count + CASE WHEN EXCLUDED.played AND NOT user_item_data.played THEN 1 ELSE 0 END,
				played = EXCLUDED.played`, owner, itemID, positionMS, played)
		if err != nil {
			return false, err
		}
		if tag.RowsAffected() == 0 {
			return false, ErrNotFound
		}
		return true, nil
	}
	// A device clock ahead of the server's must not freeze the state: future times count as now.
	when := *at
	if now := time.Now(); when.After(now) {
		when = now
	}
	tag, err := s.Pool.Exec(ctx, `INSERT INTO user_item_data (owner_id, item_id, position_ms, played, play_count, last_played_at)
		SELECT $1, $2, $3, $4, CASE WHEN $4 THEN 1 ELSE 0 END, $5 WHERE EXISTS (SELECT 1 FROM items WHERE id = $2)
		ON CONFLICT (owner_id, item_id) DO UPDATE SET position_ms = EXCLUDED.position_ms, last_played_at = EXCLUDED.last_played_at,
			play_count = user_item_data.play_count + CASE WHEN EXCLUDED.played AND NOT user_item_data.played THEN 1 ELSE 0 END,
			played = EXCLUDED.played
		WHERE user_item_data.last_played_at < EXCLUDED.last_played_at`, owner, itemID, positionMS, played, when)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ContinueWatching returns started, unfinished movies and episodes of owner.
func (s *Service) ContinueWatching(ctx context.Context, owner uuid.UUID, limit int) ([]Item, error) {
	rows, err := s.Pool.Query(ctx, `SELECT `+itemCols+` FROM user_item_data u JOIN items i ON i.id = u.item_id
		WHERE u.owner_id = $1 AND u.position_ms > 0 AND NOT u.played AND i.kind IN ('movie', 'episode')
		ORDER BY u.last_played_at DESC LIMIT $2`, owner, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	return out, s.AttachSeries(ctx, out)
}

func (s *Service) queryItems(ctx context.Context, sql string, args ...any) ([]Item, error) {
	rows, err := s.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// Genres lists the genres used by top-level items of a library.
func (s *Service) Genres(ctx context.Context, libID uuid.UUID) ([]string, error) {
	rows, err := s.Pool.Query(ctx, `SELECT DISTINCT g FROM items, unnest(genres) g
		WHERE library_id = $1 AND (parent_id IS NULL OR kind = 'album') ORDER BY g`, libID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// Search finds movies, series and episodes by title (typo tolerant) or person.
func (s *Service) Search(ctx context.Context, owner uuid.UUID, q string, limit int) ([]Item, error) {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return []Item{}, nil
	}
	like := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(q) + "%"
	return s.queryItems(ctx, `SELECT `+itemCols+` FROM items i`+userJoin("$3")+`
		WHERE i.kind IN ('movie', 'series', 'episode', 'artist', 'album', 'track') AND (
			lower(COALESCE(i.metadata_title, i.title)) LIKE $2
			-- typo tolerance: 0.35 keeps swapped letters ("matirx") and drops unrelated titles
			OR word_similarity($1, lower(COALESCE(i.metadata_title, i.title))) >= 0.35
			OR EXISTS (SELECT 1 FROM item_people p WHERE p.item_id = i.id AND lower(p.name) LIKE $2))
		ORDER BY (lower(COALESCE(i.metadata_title, i.title)) LIKE $2) DESC,
			word_similarity($1, lower(COALESCE(i.metadata_title, i.title))) DESC,
			(i.kind IN ('episode', 'track')), i.sort_title
		LIMIT $4`, q, like, owner, limit)
}

// RecentlyAdded returns the newest movies and series.
func (s *Service) RecentlyAdded(ctx context.Context, owner uuid.UUID, limit int) ([]Item, error) {
	return s.queryItems(ctx, `SELECT `+itemCols+` FROM items i`+userJoin("$1")+`
		WHERE i.parent_id IS NULL AND i.kind IN ('movie', 'series') ORDER BY i.added_at DESC LIMIT $2`, owner, limit)
}

// Favorites returns the owner's favorites, most recently marked first.
func (s *Service) Favorites(ctx context.Context, owner uuid.UUID, limit int) ([]Item, error) {
	return s.queryItems(ctx, `SELECT `+itemCols+` FROM user_item_data u JOIN items i ON i.id = u.item_id
		WHERE u.owner_id = $1 AND u.favorite ORDER BY u.last_played_at DESC LIMIT $2`, owner, limit)
}

// SetUserData marks an item favorite and/or watched. Watching a series or season
// applies to all its episodes (and their seasons).
func (s *Service) SetUserData(ctx context.Context, owner, itemID uuid.UUID, favorite, played *bool) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM items WHERE id = $1)`, itemID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		if favorite != nil {
			if _, err := tx.Exec(ctx, `INSERT INTO user_item_data (owner_id, item_id, favorite) VALUES ($1, $2, $3)
				ON CONFLICT (owner_id, item_id) DO UPDATE SET favorite = EXCLUDED.favorite, last_played_at = now()`,
				owner, itemID, *favorite); err != nil {
				return err
			}
		}
		if played != nil {
			if _, err := tx.Exec(ctx, `WITH RECURSIVE tree AS (
					SELECT id FROM items WHERE id = $2
					UNION ALL SELECT i.id FROM items i JOIN tree t ON i.parent_id = t.id)
				INSERT INTO user_item_data (owner_id, item_id, played, position_ms, play_count)
				SELECT $1, id, $3, 0, CASE WHEN $3 THEN 1 ELSE 0 END FROM tree
				ON CONFLICT (owner_id, item_id) DO UPDATE SET played = EXCLUDED.played, position_ms = 0, last_played_at = now(),
					play_count = user_item_data.play_count + CASE WHEN EXCLUDED.played AND NOT user_item_data.played THEN 1 ELSE 0 END`,
				owner, itemID, *played); err != nil {
				return err
			}
		}
		return nil
	})
}

// MetadataEdit is a hand edit of an item's metadata.
type MetadataEdit struct {
	Title, OriginalTitle, Overview, Tagline, OfficialRating string
	Year                                                    *int
	Rating                                                  *float64
	Genres                                                  []string
	Locked                                                  bool
}

// EditMetadata stores hand-edited metadata; Locked keeps it on later refreshes.
func (s *Service) EditMetadata(ctx context.Context, id uuid.UUID, e MetadataEdit) error {
	title := strings.TrimSpace(e.Title)
	if title == "" {
		return ErrInvalidName
	}
	genres := make([]string, 0, len(e.Genres))
	for _, g := range e.Genres {
		if g = strings.TrimSpace(g); g != "" {
			genres = append(genres, g)
		}
	}
	tag, err := s.Pool.Exec(ctx, `UPDATE items SET metadata_title = $2, sort_title = $3, original_title = NULLIF($4, ''),
			overview = NULLIF($5, ''), tagline = NULLIF($6, ''), official_rating = NULLIF($7, ''), year = $8,
			community_rating = $9, genres = $10, metadata_locked = $11, updated_at = now()
		WHERE id = $1`, id, title, SortTitle(title), strings.TrimSpace(e.OriginalTitle), strings.TrimSpace(e.Overview),
		strings.TrimSpace(e.Tagline), strings.TrimSpace(e.OfficialRating), e.Year, e.Rating, genres, e.Locked)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
