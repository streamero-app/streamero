package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/streamero-app/streamero/server/internal/library"
)

// LocalNFO applies .nfo files kept beside the media (Kodi format): movie.nfo or
// <name>.nfo for movies, tvshow.nfo for series, <episode name>.nfo for episodes.
// Fields present in the file overwrite downloaded metadata; a file is read again
// only when it changes. Hand-edited (locked) items are left alone.
func (r *Refresher) LocalNFO(ctx context.Context, libraryID uuid.UUID) (int, error) {
	if r.ResolvePath == nil {
		return 0, nil
	}
	rows, err := r.Pool.Query(ctx, `SELECT i.id, i.kind, i.path, i.nfo_read_at,
			(SELECT m.path FROM media_files m WHERE m.item_id = i.id ORDER BY m.path LIMIT 1)
		FROM items i WHERE i.library_id = $1 AND i.kind IN ('movie', 'series', 'episode') AND NOT i.metadata_locked`, libraryID)
	if err != nil {
		return 0, err
	}
	type item struct {
		id         uuid.UUID
		kind, path string
		readAt     *time.Time
		file       *string
	}
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id, &it.kind, &it.path, &it.readAt, &it.file); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	done := 0
	for _, it := range items {
		if ctx.Err() != nil {
			return done, ctx.Err()
		}
		src := r.findNFO(it.kind, it.path, it.file)
		if src == "" {
			continue
		}
		st, err := os.Stat(src)
		if err != nil || (it.readAt != nil && !st.ModTime().After(*it.readAt)) {
			continue
		}
		f, err := os.Open(src) //nolint:gosec // G304: fixed .nfo names inside the item's resolved folder
		if err != nil {
			continue
		}
		n, err := ParseNFO(f)
		_ = f.Close()
		if err != nil {
			r.Log.Debug("unreadable nfo", "path", src, "err", err)
			_, _ = r.Pool.Exec(ctx, `UPDATE items SET nfo_read_at = now() WHERE id = $1`, it.id)
			continue
		}
		if err := r.applyNFO(ctx, it.id, n); err != nil {
			return done, err
		}
		done++
	}
	return done, nil
}

// findNFO returns the .nfo file that describes an item, or "".
func (r *Refresher) findNFO(kind, path string, file *string) string {
	try := func(p string) string {
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			return p
		}
		return ""
	}
	stem := func(p string) string { return strings.TrimSuffix(p, filepath.Ext(p)) }
	switch kind {
	case "series":
		dir, err := r.ResolvePath(path)
		if err != nil {
			return ""
		}
		return try(filepath.Join(dir, "tvshow.nfo"))
	case "episode":
		if file == nil {
			return ""
		}
		host, err := r.ResolvePath(*file)
		if err != nil {
			return ""
		}
		return try(stem(host) + ".nfo")
	default: // movie: folder item or a loose file
		host, err := r.ResolvePath(path)
		if err != nil {
			return ""
		}
		if st, err := os.Stat(host); err == nil && st.IsDir() {
			if p := try(filepath.Join(host, "movie.nfo")); p != "" {
				return p
			}
			if file != nil {
				if fh, err := r.ResolvePath(*file); err == nil {
					return try(stem(fh) + ".nfo")
				}
			}
			return ""
		}
		return try(stem(host) + ".nfo")
	}
}

func (r *Refresher) applyNFO(ctx context.Context, id uuid.UUID, n *NFO) error {
	var year *int
	if y, err := strconv.Atoi(strings.TrimSpace(n.Year)); err == nil && y > 1800 && y < 3000 {
		year = &y
	}
	premiere := n.Premiere()
	if year == nil && premiere != nil {
		y := premiere.Year()
		year = &y
	}
	ids := n.ProviderIDs()
	rawIDs, _ := json.Marshal(ids)
	sortTitle := strings.TrimSpace(n.SortTitle)
	if sortTitle == "" && n.Title != "" {
		sortTitle = library.SortTitle(n.Title)
	}
	return pgx.BeginFunc(ctx, r.Pool, func(tx pgx.Tx) error {
		// Only fields the file has replace what is there.
		if _, err := tx.Exec(ctx, `UPDATE items SET
				metadata_title = COALESCE(NULLIF($2, ''), metadata_title),
				sort_title = COALESCE(NULLIF($3, ''), sort_title),
				original_title = COALESCE(NULLIF($4, ''), original_title),
				overview = COALESCE(NULLIF($5, ''), overview),
				tagline = COALESCE(NULLIF($6, ''), tagline),
				genres = CASE WHEN cardinality($7::text[]) > 0 THEN $7 ELSE genres END,
				studios = CASE WHEN cardinality($8::text[]) > 0 THEN $8 ELSE studios END,
				community_rating = COALESCE($9, community_rating),
				official_rating = COALESCE(NULLIF($10, ''), official_rating),
				premiere_date = COALESCE($11, premiere_date),
				year = COALESCE($12, year),
				provider_ids = provider_ids || $13::jsonb,
				metadata_error = NULL, nfo_read_at = now(), updated_at = now()
			WHERE id = $1`, id, n.Title, sortTitle, strings.TrimSpace(n.OriginalTitle), n.Overview(), strings.TrimSpace(n.Tagline),
			clean(n.Genres), clean(n.Studios), n.Score(), strings.TrimSpace(n.MPAA), premiere, year, rawIDs); err != nil {
			return err
		}
		crew := clean(n.Directors)
		writers := clean(n.Writers)
		if len(n.Actors) > 0 || len(crew) > 0 || len(writers) > 0 {
			if _, err := tx.Exec(ctx, `DELETE FROM item_people WHERE item_id = $1`, id); err != nil {
				return err
			}
			for i, a := range n.Actors {
				if i >= 30 || strings.TrimSpace(a.Name) == "" {
					continue
				}
				if _, err := tx.Exec(ctx, `INSERT INTO item_people (item_id, position, kind, name, role) VALUES ($1, $2, 'cast', $3, $4)`,
					id, i, strings.TrimSpace(a.Name), strings.TrimSpace(a.Role)); err != nil {
					return err
				}
			}
			pos := 0
			for _, list := range []struct {
				names []string
				role  string
			}{{crew, "Director"}, {writers, "Writer"}} {
				for _, name := range list.names {
					if _, err := tx.Exec(ctx, `INSERT INTO item_people (item_id, position, kind, name, role) VALUES ($1, $2, 'crew', $3, $4)`,
						id, pos, name, list.role); err != nil {
						return err
					}
					pos++
				}
			}
		}
		if set := strings.TrimSpace(n.Set.Name); set != "" {
			var cid uuid.UUID
			err := tx.QueryRow(ctx, `SELECT id FROM collections WHERE lower(name) = lower($1) ORDER BY created_at LIMIT 1`, set).Scan(&cid)
			if errors.Is(err, pgx.ErrNoRows) {
				cid = uuid.Must(uuid.NewV7())
				_, err = tx.Exec(ctx, `INSERT INTO collections (id, name) VALUES ($1, $2)`, cid, set)
			}
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO collection_items (collection_id, item_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, cid, id); err != nil {
				return err
			}
		}
		return nil
	})
}
