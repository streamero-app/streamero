package library

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrForbidden is returned when a viewer edits a playlist they do not own.
var ErrForbidden = errors.New("not allowed")

// Collection is a box set (from TMDb) or a hand-made group of titles.
type Collection struct {
	ID          uuid.UUID
	Name        string
	Overview    string
	TMDbID      *int
	ItemCount   int
	HasPoster   bool
	HasBackdrop bool
}

const collectionCols = `c.id, c.name, COALESCE(c.overview, ''), c.tmdb_id,
	(SELECT count(*) FROM collection_items ci WHERE ci.collection_id = c.id), c.has_poster, c.has_backdrop`

func scanCollection(row pgx.Row) (Collection, error) {
	var c Collection
	err := row.Scan(&c.ID, &c.Name, &c.Overview, &c.TMDbID, &c.ItemCount, &c.HasPoster, &c.HasBackdrop)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// Collections lists collections with at least one title (or all, for admins editing).
func (s *Service) Collections(ctx context.Context) ([]Collection, error) {
	rows, err := s.Pool.Query(ctx, `SELECT `+collectionCols+` FROM collections c
		WHERE EXISTS (SELECT 1 FROM collection_items ci WHERE ci.collection_id = c.id) ORDER BY lower(c.name)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Collection
	for rows.Next() {
		c, err := scanCollection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetCollection returns a collection and its titles (oldest first, like a saga).
func (s *Service) GetCollection(ctx context.Context, id, owner uuid.UUID) (Collection, []Item, error) {
	c, err := scanCollection(s.Pool.QueryRow(ctx, `SELECT `+collectionCols+` FROM collections c WHERE c.id = $1`, id))
	if err != nil {
		return c, nil, err
	}
	items, err := s.queryItems(ctx, `SELECT `+itemCols+` FROM collection_items ci JOIN items i ON i.id = ci.item_id
		`+userJoin("$2")+` WHERE ci.collection_id = $1 ORDER BY i.year NULLS LAST, i.sort_title`, id, owner)
	return c, items, err
}

// ItemCollections returns the collections an item belongs to.
func (s *Service) ItemCollections(ctx context.Context, itemID uuid.UUID) ([]Collection, error) {
	rows, err := s.Pool.Query(ctx, `SELECT `+collectionCols+` FROM collections c
		JOIN collection_items x ON x.collection_id = c.id WHERE x.item_id = $1 ORDER BY c.name`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Collection
	for rows.Next() {
		c, err := scanCollection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SaveCollection creates (id == Nil) or updates a collection and replaces its titles.
func (s *Service) SaveCollection(ctx context.Context, id uuid.UUID, name, overview string, itemIDs []uuid.UUID) (Collection, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Collection{}, ErrInvalidName
	}
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if id == uuid.Nil {
			id = uuid.Must(uuid.NewV7())
			if _, err := tx.Exec(ctx, `INSERT INTO collections (id, name, overview) VALUES ($1, $2, NULLIF($3, ''))`, id, name, overview); err != nil {
				return err
			}
		} else {
			tag, err := tx.Exec(ctx, `UPDATE collections SET name = $2, overview = NULLIF($3, '') WHERE id = $1`, id, name, overview)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return ErrNotFound
			}
			if _, err := tx.Exec(ctx, `DELETE FROM collection_items WHERE collection_id = $1`, id); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `INSERT INTO collection_items (collection_id, item_id)
			SELECT $1, i.id FROM items i WHERE i.id = ANY($2) ON CONFLICT DO NOTHING`, id, itemIDs)
		return err
	})
	if err != nil {
		return Collection{}, err
	}
	return scanCollection(s.Pool.QueryRow(ctx, `SELECT `+collectionCols+` FROM collections c WHERE c.id = $1`, id))
}

// DeleteCollection removes a collection; its titles stay.
func (s *Service) DeleteCollection(ctx context.Context, id uuid.UUID) error {
	tag, err := s.Pool.Exec(ctx, `DELETE FROM collections WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Playlist is an ordered list of titles owned by a viewer.
type Playlist struct {
	ID         uuid.UUID
	OwnerID    uuid.UUID
	Name       string
	Public     bool
	ItemCount  int
	DurationMS int64
	Covers     []uuid.UUID
	UpdatedAt  time.Time
}

const playlistCols = `p.id, p.owner_id, p.name, p.public, p.updated_at,
	(SELECT count(*) FROM playlist_items pi WHERE pi.playlist_id = p.id),
	(SELECT COALESCE(sum(d.ms), 0)::bigint FROM playlist_items pi
		JOIN LATERAL (SELECT max(f.duration_ms) AS ms FROM media_files f WHERE f.item_id = pi.item_id) d ON true
		WHERE pi.playlist_id = p.id),
	ARRAY(SELECT pi.item_id FROM playlist_items pi JOIN items i ON i.id = pi.item_id
		WHERE pi.playlist_id = p.id AND (i.has_poster OR i.has_backdrop) ORDER BY pi.position LIMIT 4)`

func scanPlaylist(row pgx.Row) (Playlist, error) {
	var p Playlist
	err := row.Scan(&p.ID, &p.OwnerID, &p.Name, &p.Public, &p.UpdatedAt, &p.ItemCount, &p.DurationMS, &p.Covers)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// Playlists lists the owner's playlists, then other viewers' public ones.
func (s *Service) Playlists(ctx context.Context, owner uuid.UUID) ([]Playlist, error) {
	rows, err := s.Pool.Query(ctx, `SELECT `+playlistCols+` FROM playlists p
		WHERE p.owner_id = $1 OR p.public ORDER BY (p.owner_id = $1) DESC, p.updated_at DESC`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Playlist
	for rows.Next() {
		p, err := scanPlaylist(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Service) playlistFor(ctx context.Context, id, owner uuid.UUID, edit bool) (Playlist, error) {
	p, err := scanPlaylist(s.Pool.QueryRow(ctx, `SELECT `+playlistCols+` FROM playlists p WHERE p.id = $1`, id))
	if err != nil {
		return p, err
	}
	if p.OwnerID != owner && (edit || !p.Public) {
		if !p.Public {
			return p, ErrNotFound // private playlists of others do not exist for you
		}
		return p, ErrForbidden
	}
	return p, nil
}

// GetPlaylist returns a playlist visible to owner, with its items in order.
func (s *Service) GetPlaylist(ctx context.Context, id, owner uuid.UUID) (Playlist, []Item, error) {
	p, err := s.playlistFor(ctx, id, owner, false)
	if err != nil {
		return p, nil, err
	}
	items, err := s.queryItems(ctx, `SELECT `+itemCols+` FROM playlist_items pi JOIN items i ON i.id = pi.item_id
		`+userJoin("$2")+` WHERE pi.playlist_id = $1 ORDER BY pi.position`, id, owner)
	if err != nil {
		return p, nil, err
	}
	return p, items, s.AttachSeries(ctx, items)
}

func writePlaylistItems(ctx context.Context, tx pgx.Tx, id uuid.UUID, itemIDs []uuid.UUID, from int) error {
	for i, item := range itemIDs {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM items WHERE id = $1)`, item).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			continue
		}
		if _, err := tx.Exec(ctx, `INSERT INTO playlist_items (playlist_id, position, item_id) VALUES ($1, $2, $3)`, id, from+i, item); err != nil {
			return err
		}
	}
	return nil
}

// SavePlaylist creates (id == Nil) or updates a playlist; itemIDs == nil keeps the items.
func (s *Service) SavePlaylist(ctx context.Context, id, owner uuid.UUID, name string, public bool, itemIDs []uuid.UUID) (Playlist, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Playlist{}, ErrInvalidName
	}
	if id != uuid.Nil {
		if _, err := s.playlistFor(ctx, id, owner, true); err != nil {
			return Playlist{}, err
		}
	}
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if id == uuid.Nil {
			id = uuid.Must(uuid.NewV7())
			if _, err := tx.Exec(ctx, `INSERT INTO playlists (id, owner_id, name, public) VALUES ($1, $2, $3, $4)`, id, owner, name, public); err != nil {
				return err
			}
		} else if _, err := tx.Exec(ctx, `UPDATE playlists SET name = $2, public = $3, updated_at = now() WHERE id = $1`, id, name, public); err != nil {
			return err
		}
		if itemIDs == nil {
			return nil
		}
		if _, err := tx.Exec(ctx, `DELETE FROM playlist_items WHERE playlist_id = $1`, id); err != nil {
			return err
		}
		return writePlaylistItems(ctx, tx, id, itemIDs, 0)
	})
	if err != nil {
		return Playlist{}, err
	}
	return s.playlistFor(ctx, id, owner, false)
}

// AppendToPlaylist adds titles at the end (duplicates are allowed, like any playlist).
func (s *Service) AppendToPlaylist(ctx context.Context, id, owner uuid.UUID, itemIDs []uuid.UUID) (Playlist, error) {
	if _, err := s.playlistFor(ctx, id, owner, true); err != nil {
		return Playlist{}, err
	}
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var next int
		// Lock the playlist row so concurrent appends do not collide on positions.
		if _, err := tx.Exec(ctx, `SELECT 1 FROM playlists WHERE id = $1 FOR UPDATE`, id); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT COALESCE(max(position) + 1, 0) FROM playlist_items WHERE playlist_id = $1`, id).Scan(&next); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE playlists SET updated_at = now() WHERE id = $1`, id); err != nil {
			return err
		}
		return writePlaylistItems(ctx, tx, id, itemIDs, next)
	})
	if err != nil {
		return Playlist{}, err
	}
	return s.playlistFor(ctx, id, owner, false)
}

// DeletePlaylist removes a playlist the owner owns.
func (s *Service) DeletePlaylist(ctx context.Context, id, owner uuid.UUID) error {
	if _, err := s.playlistFor(ctx, id, owner, true); err != nil {
		return err
	}
	_, err := s.Pool.Exec(ctx, `DELETE FROM playlists WHERE id = $1`, id)
	return err
}
