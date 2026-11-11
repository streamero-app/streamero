package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// OpenLibrary looks up book descriptions and covers on Open Library (openlibrary.org).
// It is only used when the administrator turns it on (D-039); nothing depends on it.
type OpenLibrary struct {
	Base       string // default https://openlibrary.org
	CoversBase string // default https://covers.openlibrary.org
	HTTP       *http.Client
	// Pace is the pause between requests (Open Library asks for gentle use).
	Pace time.Duration
}

// OLBook is what Open Library knows about a book.
type OLBook struct {
	Description string
	CoverID     int
}

var errNoMatch = errors.New("open library: no match")

func (o *OpenLibrary) base() string {
	if o.Base != "" {
		return strings.TrimRight(o.Base, "/")
	}
	return "https://openlibrary.org"
}

func (o *OpenLibrary) coversBase() string {
	if o.CoversBase != "" {
		return strings.TrimRight(o.CoversBase, "/")
	}
	return "https://covers.openlibrary.org"
}

func (o *OpenLibrary) client() *http.Client {
	if o.HTTP != nil {
		return o.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func (o *OpenLibrary) get(ctx context.Context, u string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Streamero/1 (self-hosted media server)")
	resp, err := o.client().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return errNoMatch
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("open library: %s", resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(v)
}

// description is a string or {"type": "/type/text", "value": "…"}.
type olText string

func (t *olText) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*t = olText(s)
		return nil
	}
	var o struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(b, &o); err != nil {
		return err
	}
	*t = olText(o.Value)
	return nil
}

// Lookup finds a book by ISBN, else by title and author.
func (o *OpenLibrary) Lookup(ctx context.Context, isbn, title, author string) (OLBook, error) {
	var out OLBook
	workKey := ""
	if isbn != "" {
		var ed struct {
			Covers      []int                  `json:"covers"`
			Description olText                 `json:"description"`
			Works       []struct{ Key string } `json:"works"`
		}
		if err := o.get(ctx, o.base()+"/isbn/"+url.PathEscape(isbn)+".json", &ed); err == nil {
			out.Description = string(ed.Description)
			if len(ed.Covers) > 0 && ed.Covers[0] > 0 {
				out.CoverID = ed.Covers[0]
			}
			if len(ed.Works) > 0 {
				workKey = ed.Works[0].Key
			}
		} else if !errors.Is(err, errNoMatch) {
			return out, err
		}
	}
	if workKey == "" && title != "" {
		q := url.Values{"title": {title}, "limit": {"1"}, "fields": {"key,cover_i"}}
		if author != "" {
			q.Set("author", author)
		}
		var res struct {
			Docs []struct {
				Key    string `json:"key"`
				CoverI int    `json:"cover_i"`
			} `json:"docs"`
		}
		if err := o.get(ctx, o.base()+"/search.json?"+q.Encode(), &res); err != nil {
			return out, err
		}
		if len(res.Docs) == 0 {
			if out.CoverID == 0 && out.Description == "" {
				return out, errNoMatch
			}
			return out, nil
		}
		workKey = res.Docs[0].Key
		if out.CoverID == 0 {
			out.CoverID = res.Docs[0].CoverI
		}
	}
	if out.Description == "" && strings.HasPrefix(workKey, "/works/") {
		var w struct {
			Description olText `json:"description"`
			Covers      []int  `json:"covers"`
		}
		if err := o.get(ctx, o.base()+workKey+".json", &w); err == nil {
			out.Description = strings.TrimSpace(string(w.Description))
			if out.CoverID == 0 && len(w.Covers) > 0 && w.Covers[0] > 0 {
				out.CoverID = w.Covers[0]
			}
		}
	}
	return out, nil
}

// Cover downloads a cover image (JPEG).
func (o *OpenLibrary) Cover(ctx context.Context, id int, dst string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/b/id/%d-L.jpg", o.coversBase(), id), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Streamero/1 (self-hosted media server)")
	resp, err := o.client().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "image/") {
		return fmt.Errorf("open library cover: %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return err
	}
	if len(data) < 1024 {
		return errNoMatch // the 1×1 placeholder Open Library returns for missing covers
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o600)
}

// OnlineBooks fills missing descriptions and covers of books and audiobooks from
// Open Library, when the administrator turned it on. Each title is asked once
// (metadata_refreshed_at); edited titles are left alone.
func (r *Refresher) OnlineBooks(ctx context.Context, libraryID uuid.UUID) (int, error) {
	cfg, err := LoadSettings(ctx, r.Settings)
	if err != nil || !cfg.OnlineBookMetadata {
		return 0, err
	}
	ol := r.OpenLibrary
	if ol == nil {
		ol = &OpenLibrary{Pace: time.Second}
	}
	rows, err := r.Pool.Query(ctx, `SELECT id, title, COALESCE(media_meta->>'isbn', ''), COALESCE(authors[1], ''),
			COALESCE(media_meta->>'description', '') <> '' OR overview IS NOT NULL, has_poster
		FROM items WHERE library_id = $1 AND kind IN ('book', 'audiobook') AND metadata_refreshed_at IS NULL
			AND NOT metadata_locked AND (NOT has_poster OR (overview IS NULL AND COALESCE(media_meta->>'description', '') = ''))
		ORDER BY added_at LIMIT 1000`, libraryID)
	if err != nil {
		return 0, err
	}
	type row struct {
		id                  uuid.UUID
		title, isbn, author string
		hasText, hasPoster  bool
	}
	var todo []row
	for rows.Next() {
		var x row
		if err := rows.Scan(&x.id, &x.title, &x.isbn, &x.author, &x.hasText, &x.hasPoster); err != nil {
			rows.Close()
			return 0, err
		}
		todo = append(todo, x)
	}
	rows.Close()
	done := 0
	for i, x := range todo {
		if ctx.Err() != nil {
			return done, ctx.Err()
		}
		if i > 0 && ol.Pace > 0 {
			time.Sleep(ol.Pace)
		}
		b, err := ol.Lookup(ctx, x.isbn, x.title, x.author)
		if err != nil && !errors.Is(err, errNoMatch) {
			r.Log.Info("open library lookup failed", "title", x.title, "err", err)
			continue // try again next time
		}
		var overview *string
		if !x.hasText && b.Description != "" {
			overview = &b.Description
		}
		poster := false
		if !x.hasPoster && b.CoverID > 0 && ol.Cover(ctx, b.CoverID, ImagePath(r.ImageDir, x.id, "poster")) == nil {
			poster = true
		}
		if _, err := r.Pool.Exec(ctx, `UPDATE items SET metadata_refreshed_at = now(), overview = COALESCE($2, overview),
			has_poster = has_poster OR $3, colors = CASE WHEN $3 THEN NULL ELSE colors END WHERE id = $1`, x.id, overview, poster); err != nil {
			return done, err
		}
		if overview != nil || poster {
			done++
		}
	}
	return done, nil
}
