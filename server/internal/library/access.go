package library

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Per-library access (D-054).
//
// Which libraries a viewer may reach is decided in one place, the SQL function
// visible_library_ids(owner) (migration 00036): administrators and accounts with
// allLibraries see every library except adult ones, other accounts only the
// libraries listed in user_library_access. owner is the id every viewer query
// already carries (stream.Owner: the profile or the account), so profiles always
// inherit their account's libraries and can never widen them.
//
// Every query that returns items, files or libraries to a viewer adds Visible (or
// one of the helpers below). Background work (scans, metadata) does not.

// EveryLibrary is an owner that sees every non-adult library, for server-side
// work and tests. It is never derived from a request (stream.Owner returns a
// profile or account id), so no viewer can pass it.
var EveryLibrary = uuid.Max

// Visible is a SQL condition keeping the library id column col to the libraries
// the viewer in the placeholder owner (e.g. "$2") may see.
func Visible(col, owner string) string {
	return col + ` IN (SELECT visible_library_ids(` + owner + `))`
}

// VisibleItem is a SQL condition keeping the item id column col to items in
// libraries the viewer may see.
func VisibleItem(col, owner string) string {
	return `EXISTS (SELECT 1 FROM items vi WHERE vi.id = ` + col + ` AND ` + Visible("vi.library_id", owner) + `)`
}

// VisibleLibraryIDs returns the libraries owner (profile or user id) may see.
func (s *Service) VisibleLibraryIDs(ctx context.Context, owner uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.Pool.Query(ctx, `SELECT visible_library_ids($1)`, owner)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// ListFor returns the libraries owner may see, by name.
func (s *Service) ListFor(ctx context.Context, owner uuid.UUID) ([]Library, error) {
	rows, err := s.Pool.Query(ctx, `SELECT `+libCols+` FROM libraries l WHERE `+Visible("l.id", "$1")+` ORDER BY lower(l.name)`, owner)
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

// GetFor returns one library owner may see (ErrNotFound otherwise).
func (s *Service) GetFor(ctx context.Context, id, owner uuid.UUID) (Library, error) {
	return scanLibrary(s.Pool.QueryRow(ctx, `SELECT `+libCols+` FROM libraries l WHERE l.id = $1 AND `+Visible("l.id", "$2"), id, owner))
}

// CanSeeItem returns ErrNotFound unless the item is in a library owner may see.
func (s *Service) CanSeeItem(ctx context.Context, owner, itemID uuid.UUID) error {
	var ok bool
	if err := s.Pool.QueryRow(ctx, `SELECT `+VisibleItem("$2::uuid", "$1"), owner, itemID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

// CanSeeItems returns ErrNotFound unless every item is in a library owner may see.
func (s *Service) CanSeeItems(ctx context.Context, owner uuid.UUID, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	var hidden int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM unnest($2::uuid[]) x WHERE NOT `+VisibleItem("x", "$1"), owner, ids).Scan(&hidden); err != nil {
		return err
	}
	if hidden > 0 {
		return ErrNotFound
	}
	return nil
}

// CanSeeFile returns ErrNotFound unless the media file is in a library owner may see.
func (s *Service) CanSeeFile(ctx context.Context, owner, fileID uuid.UUID) error {
	var ok bool
	err := s.Pool.QueryRow(ctx, `SELECT `+Visible("f.library_id", "$1")+` FROM media_files f WHERE f.id = $2`, owner, fileID).Scan(&ok)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !ok {
		return ErrNotFound
	}
	return err
}

// grantNewLibrary adds a just-created library to the lists of accounts that
// asked for new libraries (D-054). Accounts with allLibraries see it anyway;
// adult libraries are never added.
func (s *Service) grantNewLibrary(ctx context.Context, id uuid.UUID, kind string) error {
	if kind == KindAdult {
		return nil
	}
	_, err := s.Pool.Exec(ctx, `INSERT INTO user_library_access (user_id, library_id)
		SELECT u.id, $1 FROM users u
		WHERE u.new_libraries AND NOT u.is_admin AND NOT COALESCE((u.permissions ->> 'allLibraries')::boolean, true)
		ON CONFLICT DO NOTHING`, id)
	return err
}
