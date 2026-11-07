package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/settings"
)

// Settings are the admin-editable metadata settings (stored in the settings table).
type Settings struct {
	TMDbKey  string `json:"tmdbKey,omitempty"`
	Language string `json:"language"`
	Country  string `json:"country"`
}

// KeySettings is the settings-table key.
const KeySettings = "metadata"

// LoadSettings returns the stored settings with defaults.
func LoadSettings(ctx context.Context, st *settings.Store) (Settings, error) {
	s := Settings{Language: "hr-HR", Country: "HR"}
	_, err := st.Get(ctx, KeySettings, &s)
	return s, err
}

// SaveSettings stores the settings.
func SaveSettings(ctx context.Context, st *settings.Store, s Settings) error {
	return st.Set(ctx, KeySettings, s)
}

// Refresher fills items with metadata and caches artwork under ImageDir.
type Refresher struct {
	Pool     *pgxpool.Pool
	Settings *settings.Store
	ImageDir string
	Log      *slog.Logger
	// NewClient builds the TMDb client (tests point it at a fake server).
	NewClient func(key string) *TMDb
}

// ImagePath is where an item's cached image lives.
func ImagePath(dir string, itemID uuid.UUID, kind string) string {
	return filepath.Join(dir, itemID.String(), kind+".jpg")
}

type itemRow struct {
	id, libraryID uuid.UUID
	kind, title   string
	year          *int
	season, ep    *int
	ids           map[string]string
}

// RefreshLibrary fetches metadata for items that have none (or all when force).
func (r *Refresher) RefreshLibrary(ctx context.Context, libraryID uuid.UUID, force bool) (int, error) {
	cfg, err := LoadSettings(ctx, r.Settings)
	if err != nil {
		return 0, err
	}
	if cfg.TMDbKey == "" {
		return 0, nil // metadata is optional; nothing to do without a key
	}
	c := r.client(cfg.TMDbKey)
	rows, err := r.Pool.Query(ctx, `SELECT id, library_id, kind, title, year, season_number, episode_number, provider_ids
		FROM items WHERE library_id = $1 AND parent_id IS NULL AND ($2 OR metadata_refreshed_at IS NULL)
		ORDER BY added_at`, libraryID, force)
	if err != nil {
		return 0, err
	}
	items, err := collectItems(rows)
	if err != nil {
		return 0, err
	}
	done := 0
	for _, it := range items {
		if ctx.Err() != nil {
			return done, ctx.Err()
		}
		if err := r.refreshTop(ctx, c, cfg, it); err != nil {
			if errors.Is(err, ErrInvalidKey) {
				return done, err
			}
			r.Log.Info("no metadata", "item", it.title, "err", err)
			_, _ = r.Pool.Exec(ctx, `UPDATE items SET metadata_refreshed_at = now(), metadata_error = $2 WHERE id = $1`, it.id, err.Error())
			continue
		}
		done++
	}
	return done, nil
}

func (r *Refresher) client(key string) *TMDb {
	if r.NewClient != nil {
		return r.NewClient(key)
	}
	return &TMDb{Key: key}
}

func collectItems(rows pgx.Rows) ([]itemRow, error) {
	defer rows.Close()
	var out []itemRow
	for rows.Next() {
		var it itemRow
		var ids []byte
		if err := rows.Scan(&it.id, &it.libraryID, &it.kind, &it.title, &it.year, &it.season, &it.ep, &ids); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(ids, &it.ids)
		if it.ids == nil {
			it.ids = map[string]string{}
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (r *Refresher) resolveID(ctx context.Context, c *TMDb, kind string, it itemRow, lang string) (int, error) {
	if v := it.ids["tmdb"]; v != "" {
		if id, err := strconv.Atoi(v); err == nil {
			return id, nil
		}
	}
	if v := it.ids["imdb"]; v != "" {
		if id, err := c.FindIMDb(ctx, kind, v); err == nil {
			return id, nil
		}
	}
	year := 0
	if it.year != nil {
		year = *it.year
	}
	id, err := c.Search(ctx, kind, it.title, year, lang)
	if errors.Is(err, errNotFound) {
		return 0, fmt.Errorf("no TMDb match for %q", it.title)
	}
	return id, err
}

func (r *Refresher) refreshTop(ctx context.Context, c *TMDb, cfg Settings, it itemRow) error {
	kind := "movie"
	if it.kind == "series" {
		kind = "tv"
	}
	id, err := r.resolveID(ctx, c, kind, it, cfg.Language)
	if err != nil {
		return err
	}
	var d Details
	if kind == "movie" {
		d, err = c.Movie(ctx, id, cfg.Language)
	} else {
		d, err = c.Series(ctx, id, cfg.Language)
	}
	if err != nil {
		return err
	}
	// Overviews are often missing in smaller languages: fall back to English.
	if d.Overview == "" && !strings.HasPrefix(cfg.Language, "en") {
		var en Details
		if kind == "movie" {
			en, err = c.Movie(ctx, id, "en-US")
		} else {
			en, err = c.Series(ctx, id, "en-US")
		}
		if err == nil {
			d.Overview, d.Tagline = en.Overview, firstNonEmpty(d.Tagline, en.Tagline)
		}
	}
	it.ids["tmdb"] = strconv.Itoa(d.ID)
	if imdb := firstNonEmpty(d.IMDbID, d.ExternalIDs.IMDbID); imdb != "" {
		it.ids["imdb"] = imdb
	}
	if d.ExternalIDs.TVDbID > 0 {
		it.ids["tvdb"] = strconv.Itoa(d.ExternalIDs.TVDbID)
	}
	poster := r.saveImage(ctx, c, it.id, "poster", d.PosterPath, "w500")
	backdrop := r.saveImage(ctx, c, it.id, "backdrop", d.BackdropPath, "w1280")
	if err := r.store(ctx, it.id, d, cfg.Country, it.ids, poster, backdrop); err != nil {
		return err
	}
	if kind == "tv" {
		return r.refreshSeasons(ctx, c, cfg, it.id, d)
	}
	return nil
}

func (r *Refresher) store(ctx context.Context, id uuid.UUID, d Details, country string, ids map[string]string, poster, backdrop bool) error {
	genres := make([]string, 0, len(d.Genres))
	for _, g := range d.Genres {
		genres = append(genres, g.Name)
	}
	var premiere *time.Time
	if t, err := time.Parse("2006-01-02", d.Date()); err == nil {
		premiere = &t
	}
	var rating *float64
	if d.VoteAverage > 0 {
		v := d.VoteAverage
		rating = &v
	}
	rawIDs, _ := json.Marshal(ids)
	title := d.DisplayTitle()
	return pgx.BeginFunc(ctx, r.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE items SET metadata_title = NULLIF($2, ''), sort_title = COALESCE(NULLIF($3, ''), sort_title),
				original_title = NULLIF($4, ''), overview = NULLIF($5, ''), tagline = NULLIF($6, ''), genres = $7,
				community_rating = $8, official_rating = NULLIF($9, ''), premiere_date = $10, has_poster = $11,
				has_backdrop = $12, provider_ids = $13, year = COALESCE(year, $14), metadata_refreshed_at = now(),
				metadata_error = NULL, updated_at = now()
			WHERE id = $1`, id, title, library.SortTitle(title), d.Original(), d.Overview, d.Tagline, genres, rating,
			d.Certification(country), premiere, poster, backdrop, rawIDs, yearOf(premiere)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM item_people WHERE item_id = $1`, id); err != nil {
			return err
		}
		for i, p := range d.Credits.Cast {
			if i >= 20 {
				break
			}
			if _, err := tx.Exec(ctx, `INSERT INTO item_people (item_id, position, kind, name, role) VALUES ($1, $2, 'cast', $3, $4)`,
				id, i, p.Name, p.Character); err != nil {
				return err
			}
		}
		n := 0
		for _, p := range d.Credits.Crew {
			if p.Job != "Director" && p.Job != "Screenplay" && p.Job != "Writer" && p.Job != "Creator" {
				continue
			}
			if _, err := tx.Exec(ctx, `INSERT INTO item_people (item_id, position, kind, name, role) VALUES ($1, $2, 'crew', $3, $4)
				ON CONFLICT DO NOTHING`, id, n, p.Name, p.Job); err != nil {
				return err
			}
			n++
		}
		return nil
	})
}

func (r *Refresher) refreshSeasons(ctx context.Context, c *TMDb, cfg Settings, seriesID uuid.UUID, d Details) error {
	rows, err := r.Pool.Query(ctx, `SELECT id, library_id, kind, title, year, season_number, episode_number, provider_ids
		FROM items WHERE parent_id = $1`, seriesID)
	if err != nil {
		return err
	}
	seasons, err := collectItems(rows)
	if err != nil {
		return err
	}
	posters := map[int]string{}
	for _, s := range d.Seasons {
		posters[s.SeasonNumber] = s.PosterPath
	}
	for _, s := range seasons {
		if s.season == nil {
			continue
		}
		poster := r.saveImage(ctx, c, s.id, "poster", posters[*s.season], "w500")
		eps, overview, err := c.Season(ctx, d.ID, *s.season, cfg.Language)
		if err != nil && !errors.Is(err, errNotFound) {
			return err
		}
		_, _ = r.Pool.Exec(ctx, `UPDATE items SET has_poster = $2, overview = NULLIF($3, ''), metadata_refreshed_at = now() WHERE id = $1`,
			s.id, poster, overview)
		byNumber := map[int]Episode{}
		for _, e := range eps {
			byNumber[e.EpisodeNumber] = e
		}
		rows, err := r.Pool.Query(ctx, `SELECT id, library_id, kind, title, year, season_number, episode_number, provider_ids
			FROM items WHERE parent_id = $1`, s.id)
		if err != nil {
			return err
		}
		episodes, err := collectItems(rows)
		if err != nil {
			return err
		}
		for _, e := range episodes {
			if e.ep == nil {
				continue
			}
			meta, ok := byNumber[*e.ep]
			if !ok {
				continue
			}
			still := r.saveImage(ctx, c, e.id, "still", meta.StillPath, "w300")
			var air *time.Time
			if t, err := time.Parse("2006-01-02", meta.AirDate); err == nil {
				air = &t
			}
			var rating *float64
			if meta.VoteAverage > 0 {
				v := meta.VoteAverage
				rating = &v
			}
			if _, err := r.Pool.Exec(ctx, `UPDATE items SET metadata_title = NULLIF($2, ''), overview = NULLIF($3, ''),
					premiere_date = $4, community_rating = $5, has_poster = $6, metadata_refreshed_at = now(), metadata_error = NULL
				WHERE id = $1`, e.id, meta.Name, meta.Overview, air, rating, still); err != nil {
				return err
			}
		}
	}
	return nil
}

// saveImage downloads artwork into the cache; it reports whether an image is available.
func (r *Refresher) saveImage(ctx context.Context, c *TMDb, id uuid.UUID, kind, path, size string) bool {
	if path == "" || r.ImageDir == "" {
		return false
	}
	data, err := c.Image(ctx, path, size)
	if err != nil {
		r.Log.Debug("image download failed", "item", id, "kind", kind, "err", err)
		return false
	}
	dst := ImagePath(r.ImageDir, id, kind)
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return false
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return false
	}
	return os.Rename(tmp, dst) == nil
}

// CleanupImages removes cached artwork of items that no longer exist.
func (r *Refresher) CleanupImages(ctx context.Context) error {
	if r.ImageDir == "" {
		return nil
	}
	entries, err := os.ReadDir(r.ImageDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		id, err := uuid.Parse(e.Name())
		if err != nil || !e.IsDir() {
			continue
		}
		var exists bool
		if err := r.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM items WHERE id = $1)`, id).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			_ = os.RemoveAll(filepath.Join(r.ImageDir, e.Name()))
		}
	}
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func yearOf(t *time.Time) *int {
	if t == nil {
		return nil
	}
	y := t.Year()
	return &y
}
