package library

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// NameCount is an author or series with how many titles it has.
type NameCount struct {
	Name   string
	Count  int
	ItemID uuid.UUID // a title to borrow a cover from
}

// Authors lists the authors of a book or audiobook library.
func (s *Service) Authors(ctx context.Context, libID uuid.UUID) ([]NameCount, error) {
	return s.nameCounts(ctx, `SELECT a, count(*)::int, (array_agg(i.id ORDER BY i.has_poster DESC, i.year NULLS LAST, i.sort_title))[1]
		FROM items i CROSS JOIN LATERAL unnest(i.authors) a
		WHERE i.library_id = $1 AND i.kind IN ('book', 'audiobook')
		GROUP BY a ORDER BY lower(a)`, libID)
}

// Series lists the series of a book or audiobook library.
func (s *Service) Series(ctx context.Context, libID uuid.UUID) ([]NameCount, error) {
	return s.nameCounts(ctx, `SELECT i.series_name, count(*)::int,
			(array_agg(i.id ORDER BY i.series_index NULLS LAST, i.sort_title))[1]
		FROM items i WHERE i.library_id = $1 AND i.series_name IS NOT NULL AND i.kind IN ('book', 'audiobook')
		GROUP BY i.series_name ORDER BY lower(i.series_name)`, libID)
}

func (s *Service) nameCounts(ctx context.Context, q string, args ...any) ([]NameCount, error) {
	rows, err := s.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NameCount{}
	for rows.Next() {
		var n NameCount
		if err := rows.Scan(&n.Name, &n.Count, &n.ItemID); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ContinueReading returns started, unfinished books and audiobooks of owner.
func (s *Service) ContinueReading(ctx context.Context, owner uuid.UUID, limit int) ([]Item, error) {
	return s.queryItems(ctx, `SELECT `+itemCols+` FROM user_item_data u JOIN items i ON i.id = u.item_id
		WHERE u.owner_id = $1 AND NOT u.played AND (u.progression > 0 OR u.position_ms > 0) AND i.kind IN ('book', 'audiobook')
		ORDER BY u.last_played_at DESC LIMIT $2`, owner, limit)
}

// Reading is a viewer's place in a book.
type Reading struct {
	Page        *int
	Locator     string
	Progression float64
	Finished    bool
	UpdatedAt   *time.Time
}

// GetReading returns owner's reading position (zero value when never opened).
func (s *Service) GetReading(ctx context.Context, owner, itemID uuid.UUID) (Reading, error) {
	var r Reading
	var prog *float64
	var loc *string
	err := s.Pool.QueryRow(ctx, `SELECT page, locator, progression, played, last_played_at FROM user_item_data
		WHERE owner_id = $1 AND item_id = $2`, owner, itemID).Scan(&r.Page, &loc, &prog, &r.Finished, &r.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, nil
	}
	if prog != nil {
		r.Progression = *prog
	}
	if loc != nil {
		r.Locator = *loc
	}
	if r.Finished {
		r.Progression = 1
	}
	return r, err
}

// SaveReading stores owner's position in a book or comic. The last page (or a
// progression of 99.5%) marks it as read.
func (s *Service) SaveReading(ctx context.Context, owner, itemID uuid.UUID, page *int, locator string, progression float64, finished bool) error {
	var pageCount *int
	var kind string
	err := s.Pool.QueryRow(ctx, `SELECT kind, (media_meta->>'pageCount')::int FROM items WHERE id = $1`, itemID).Scan(&kind, &pageCount)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && kind != "book") {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	progression = min(max(progression, 0), 1)
	if page != nil && pageCount != nil && *pageCount > 0 && *page >= *pageCount-1 {
		finished = true
	}
	if progression >= 0.995 {
		finished = true
	}
	var loc *string
	if locator != "" {
		loc = &locator
	}
	_, err = s.Pool.Exec(ctx, `INSERT INTO user_item_data (owner_id, item_id, page, locator, progression, played, play_count, last_played_at)
		VALUES ($1, $2, $3, $4, $5, $6, CASE WHEN $6 THEN 1 ELSE 0 END, now())
		ON CONFLICT (owner_id, item_id) DO UPDATE SET page = EXCLUDED.page, locator = EXCLUDED.locator,
			progression = EXCLUDED.progression, last_played_at = now(),
			play_count = user_item_data.play_count + CASE WHEN EXCLUDED.played AND NOT user_item_data.played THEN 1 ELSE 0 END,
			played = EXCLUDED.played`, owner, itemID, page, loc, progression, finished)
	return err
}

// ItemMeta returns what the scanner read from an item's file (nil for other kinds).
func (s *Service) ItemMeta(ctx context.Context, id uuid.UUID) (*MediaMeta, error) {
	var raw []byte
	err := s.Pool.QueryRow(ctx, `SELECT media_meta FROM items WHERE id = $1`, id).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil || raw == nil {
		return nil, err
	}
	m := &MediaMeta{}
	return m, json.Unmarshal(raw, m)
}

// ItemFile is the single file behind a book or photo.
type ItemFile struct {
	ID            uuid.UUID
	Kind, Path    string
	Width, Height *int
}

// ItemFile returns the file of a book, photo or video item.
func (s *Service) ItemFile(ctx context.Context, id uuid.UUID) (ItemFile, error) {
	f := ItemFile{}
	err := s.Pool.QueryRow(ctx, `SELECT f.id, i.kind, f.path, f.width, f.height FROM items i JOIN media_files f ON f.item_id = i.id
		WHERE i.id = $1 ORDER BY f.path LIMIT 1`, id).Scan(&f.ID, &f.Kind, &f.Path, &f.Width, &f.Height)
	if errors.Is(err, pgx.ErrNoRows) {
		return f, ErrNotFound
	}
	return f, err
}

// AudiobookPart is one file of an audiobook.
type AudiobookPart struct {
	FileID               uuid.UUID
	Title                string
	OffsetMS, DurationMS int64
}

// AudiobookChapter is a chapter, positioned in the whole book.
type AudiobookChapter struct {
	Title          string
	StartMS, EndMS int64
}

// Audiobook returns the parts in listening order and the chapters of an audiobook.
func (s *Service) Audiobook(ctx context.Context, id uuid.UUID) ([]AudiobookPart, []AudiobookChapter, error) {
	var kind string
	if err := s.Pool.QueryRow(ctx, `SELECT kind FROM items WHERE id = $1`, id).Scan(&kind); err != nil || kind != "audiobook" {
		return nil, nil, ErrNotFound
	}
	rows, err := s.Pool.Query(ctx, `SELECT id, path, COALESCE(duration_ms, 0), tags, chapters FROM media_files WHERE item_id = $1`, id)
	if err != nil {
		return nil, nil, err
	}
	type file struct {
		id       uuid.UUID
		dur      int64
		tags     map[string]string
		chapters []ProbeChapter
	}
	files := map[string]file{}
	var paths []string
	for rows.Next() {
		var f file
		var p string
		var tags, chs []byte
		if err := rows.Scan(&f.id, &p, &f.dur, &tags, &chs); err != nil {
			rows.Close()
			return nil, nil, err
		}
		_ = json.Unmarshal(tags, &f.tags)
		_ = json.Unmarshal(chs, &f.chapters)
		files[p] = f
		paths = append(paths, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	AudiobookPartOrder(paths, func(p string) map[string]string { return files[p].tags })
	parts := make([]AudiobookPart, 0, len(paths))
	var chapters []AudiobookChapter
	var off int64
	for i, p := range paths {
		f := files[p]
		title := strings.TrimSpace(f.tags["title"])
		if title == "" || len(paths) > 1 && i > 0 && title == strings.TrimSpace(files[paths[0]].tags["title"]) {
			title = strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
		}
		parts = append(parts, AudiobookPart{FileID: f.id, Title: title, OffsetMS: off, DurationMS: f.dur})
		if len(f.chapters) > 0 {
			for j, c := range f.chapters {
				end := f.dur
				if j+1 < len(f.chapters) {
					end = f.chapters[j+1].StartMS
				}
				t := c.Title
				if t == "" {
					t = title
				}
				chapters = append(chapters, AudiobookChapter{Title: t, StartMS: off + c.StartMS, EndMS: off + end})
			}
		} else {
			chapters = append(chapters, AudiobookChapter{Title: title, StartMS: off, EndMS: off + f.dur})
		}
		off += f.dur
	}
	if chapters == nil {
		chapters = []AudiobookChapter{}
	}
	return parts, chapters, nil
}
