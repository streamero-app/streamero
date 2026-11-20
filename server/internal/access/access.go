// Package access decides which library items a caller may see: library access
// (the allLibraries permission) and the parental limit of the caller's profile.
// Every surface that exposes items (the API, the compat API, DLNA, downloads,
// watch-together) asks here, so the rule exists once (D-053).
//
// The parental rule: an item is hidden when its own official rating, or the
// rating of any ancestor (season, series, album, artist, photo album), implies
// an age above the profile's limit. An item without a rating therefore
// inherits from what it belongs to; a title that is unrated all the way up is
// shown, because it carries no age to compare.
package access

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/streamero-app/streamero/server/internal/auth"
	"github.com/streamero-app/streamero/server/internal/library"
)

// ErrHidden is returned for an item the caller may not see. It wraps
// library.ErrNotFound: a hidden item looks exactly like one that does not exist.
var ErrHidden = fmt.Errorf("%w", library.ErrNotFound)

// DB is what the checks need from a pool or a transaction.
type DB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// CanSeeLibraries reports library access: administrators, or accounts with the
// allLibraries permission. There are no per-library grants.
func CanSeeLibraries(p auth.Principal) bool {
	return p.User.IsAdmin || p.User.Permissions.AllLibraries
}

// MaxAge returns the parental limit of the caller's profile (nil = none). It
// applies to every profile that has one, also on an administrator's account.
func MaxAge(p auth.Principal) *int {
	if p.Profile == nil {
		return nil
	}
	return p.Profile.MaxRating
}

// Limited reports whether the caller has a parental limit.
func Limited(p auth.Principal) bool { return MaxAge(p) != nil }

// distinctRatingsSQL walks the index on official_rating one distinct value at a
// time, so it costs one index probe per rating label, not one per item.
const distinctRatingsSQL = `WITH RECURSIVE r AS (
		(SELECT official_rating AS v FROM items WHERE official_rating IS NOT NULL ORDER BY official_rating LIMIT 1)
		UNION ALL
		SELECT (SELECT official_rating FROM items WHERE official_rating > r.v ORDER BY official_rating LIMIT 1)
		FROM r WHERE r.v IS NOT NULL)
	SELECT v FROM r WHERE v IS NOT NULL`

// BlockedLabels returns the official ratings in use that imply an age above
// maxAge (always non-nil). It is read fresh on every call: a title rated a
// moment ago is hidden on the next request.
func BlockedLabels(ctx context.Context, db DB, maxAge int) ([]string, error) {
	rows, err := db.Query(ctx, distinctRatingsSQL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var known []string
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			return nil, err
		}
		known = append(known, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return library.BlockedRatings(known, maxAge), nil
}

// Labels returns the ratings hidden from p: nil when p has no limit.
func Labels(ctx context.Context, db DB, p auth.Principal) ([]string, error) {
	limit := MaxAge(p)
	if limit == nil {
		return nil, nil
	}
	return BlockedLabels(ctx, db, *limit)
}

// BlockedAmong returns the ids whose item or any ancestor has one of labels.
func BlockedAmong(ctx context.Context, db DB, labels []string, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	out := map[uuid.UUID]bool{}
	if len(labels) == 0 || len(ids) == 0 {
		return out, nil
	}
	rows, err := db.Query(ctx, `WITH RECURSIVE up AS (
			SELECT id AS start, parent_id, official_rating, 0 AS depth FROM items WHERE id = ANY($1)
			UNION ALL
			SELECT up.start, i.parent_id, i.official_rating, up.depth + 1 FROM items i JOIN up ON i.id = up.parent_id WHERE up.depth < 16)
		SELECT DISTINCT start FROM up WHERE official_rating = ANY($2)`, ids, labels)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// Hidden returns which of ids p may not see because of its parental limit.
func Hidden(ctx context.Context, db DB, p auth.Principal, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	labels, err := Labels(ctx, db, p)
	if err != nil {
		return nil, err
	}
	return BlockedAmong(ctx, db, labels, ids)
}

// Item reports whether p may see an item: nil, or ErrHidden without library
// access or above the profile's limit. An id that is no item passes: the
// caller's own lookup answers "not found".
func Item(ctx context.Context, db DB, p auth.Principal, id uuid.UUID) error {
	if !CanSeeLibraries(p) {
		return ErrHidden
	}
	if !Limited(p) {
		return nil
	}
	hidden, err := Hidden(ctx, db, p, []uuid.UUID{id})
	if err != nil {
		return err
	}
	if hidden[id] {
		return ErrHidden
	}
	return nil
}

// File is Item for the item a media file belongs to. An unknown file passes.
func File(ctx context.Context, db DB, p auth.Principal, fileID uuid.UUID) error {
	if !CanSeeLibraries(p) {
		return ErrHidden
	}
	if !Limited(p) {
		return nil
	}
	var item uuid.UUID
	err := db.QueryRow(ctx, `SELECT item_id FROM media_files WHERE id = $1`, fileID).Scan(&item)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return Item(ctx, db, p, item)
}

// Filter drops the entries p may not see, keeping the order. Without library
// access nothing is left.
func Filter[T any](ctx context.Context, db DB, p auth.Principal, in []T, id func(T) uuid.UUID) ([]T, error) {
	if !CanSeeLibraries(p) {
		return in[:0:0], nil
	}
	if !Limited(p) || len(in) == 0 {
		return in, nil
	}
	ids := make([]uuid.UUID, len(in))
	for i, x := range in {
		ids[i] = id(x)
	}
	hidden, err := Hidden(ctx, db, p, ids)
	if err != nil {
		return nil, err
	}
	if len(hidden) == 0 {
		return in, nil
	}
	out := make([]T, 0, len(in))
	for i, x := range in {
		if !hidden[ids[i]] {
			out = append(out, x)
		}
	}
	return out, nil
}

// Items is Filter for library items.
func Items(ctx context.Context, db DB, p auth.Principal, in []library.Item) ([]library.Item, error) {
	return Filter(ctx, db, p, in, func(it library.Item) uuid.UUID { return it.ID })
}
