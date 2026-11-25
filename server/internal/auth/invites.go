package auth

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Invite is an invite link (the token itself is never stored).
type Invite struct {
	ID          uuid.UUID
	Note        string
	CreatedBy   string
	CreatedAt   time.Time
	ExpiresAt   time.Time
	MaxUses     int
	Uses        int
	Permissions Permissions
	Libraries   LibraryAccess // used when Permissions.AllLibraries is off (D-054)
}

// CreateInvite makes a link valid for ttl and maxUses accounts; returns the token once.
func (s *Service) CreateInvite(ctx context.Context, actor Principal, note string, ttl time.Duration, maxUses int, perms *Permissions, libs *LibraryAccess) (Invite, string, error) {
	token, hash, err := newToken()
	if err != nil {
		return Invite{}, "", err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return Invite{}, "", err
	}
	p := DefaultPermissions()
	if perms != nil {
		p = *perms
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return Invite{}, "", err
	}
	la := LibraryAccess{IDs: []uuid.UUID{}}
	if libs != nil {
		la.NewLibraries = libs.NewLibraries
		if libs.IDs != nil {
			la.IDs = libs.IDs
		}
	}
	now := s.now()
	inv := Invite{ID: id, Note: truncate(note, 100), CreatedBy: actor.User.Username, CreatedAt: now,
		ExpiresAt: now.Add(ttl), MaxUses: maxUses, Permissions: p}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// Only existing, non-adult libraries are kept (D-053, D-054).
		if err := tx.QueryRow(ctx, `INSERT INTO invites (id, token_hash, note, created_by, created_at, expires_at, max_uses, permissions,
				library_ids, new_libraries)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8,
				ARRAY(SELECT l.id FROM libraries l WHERE l.id = ANY ($9::uuid[]) AND l.kind <> 'adult' ORDER BY l.id), $10)
			RETURNING library_ids, new_libraries`,
			id, hash, inv.Note, actor.User.ID, now, inv.ExpiresAt, maxUses, raw, la.IDs, la.NewLibraries).Scan(&inv.Libraries.IDs, &inv.Libraries.NewLibraries); err != nil {
			return err
		}
		return audit(ctx, tx, &actor.User.ID, "invite.created", map[string]any{"invite_id": id, "max_uses": maxUses, "expires_at": inv.ExpiresAt})
	})
	return inv, token, err
}

// ListInvites returns all invites, newest first.
func (s *Service) ListInvites(ctx context.Context) ([]Invite, error) {
	rows, err := s.pool.Query(ctx, `SELECT i.id, i.note, u.username, i.created_at, i.expires_at, i.max_uses, i.uses, i.permissions,
			ARRAY(SELECT x FROM unnest(i.library_ids) x WHERE EXISTS (SELECT 1 FROM libraries l WHERE l.id = x)), i.new_libraries
		FROM invites i JOIN users u ON u.id = i.created_by ORDER BY i.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Invite
	for rows.Next() {
		var inv Invite
		var raw []byte
		if err := rows.Scan(&inv.ID, &inv.Note, &inv.CreatedBy, &inv.CreatedAt, &inv.ExpiresAt, &inv.MaxUses, &inv.Uses, &raw,
			&inv.Libraries.IDs, &inv.Libraries.NewLibraries); err != nil {
			return nil, err
		}
		inv.Permissions = parsePermissions(raw)
		out = append(out, inv)
	}
	return out, rows.Err()
}

// DeleteInvite revokes an invite.
func (s *Service) DeleteInvite(ctx context.Context, actor Principal, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM invites WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return audit(ctx, s.pool, &actor.User.ID, "invite.deleted", map[string]any{"invite_id": id})
}

// CheckInvite returns the expiry of a usable invite, or ErrNotFound.
func (s *Service) CheckInvite(ctx context.Context, token string) (time.Time, error) {
	var exp time.Time
	err := s.pool.QueryRow(ctx, `SELECT expires_at FROM invites WHERE token_hash = $1 AND expires_at > $2 AND uses < max_uses`,
		hashToken(token), s.now()).Scan(&exp)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, ErrNotFound
	}
	return exp, err
}

// AcceptInvite creates a non-admin account from a usable invite and signs it in.
func (s *Service) AcceptInvite(ctx context.Context, token, username, password string, dev Device) (Principal, string, error) {
	username, err := NormalizeUsername(username)
	if err != nil {
		return Principal{}, "", err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return Principal{}, "", err
	}
	var p Principal
	var sessionToken string
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// Consuming a use and creating the account happen together, so the use limit holds under concurrency.
		var inviteID uuid.UUID
		var raw []byte
		var la LibraryAccess
		err := tx.QueryRow(ctx, `UPDATE invites SET uses = uses + 1
			WHERE token_hash = $1 AND expires_at > $2 AND uses < max_uses RETURNING id, permissions, library_ids, new_libraries`,
			hashToken(token), s.now()).Scan(&inviteID, &raw, &la.IDs, &la.NewLibraries)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		perms := parsePermissions(raw)
		u, err := insertUser(ctx, tx, username, hash, false, &perms)
		if err != nil {
			return err
		}
		if !perms.AllLibraries {
			if u, err = setLibraryAccess(ctx, tx, u.ID, la); err != nil {
				return err
			}
		}
		p, sessionToken, err = createSession(ctx, tx, u, dev, s.now())
		if err != nil {
			return err
		}
		return audit(ctx, tx, &u.ID, "invite.accepted", map[string]any{"invite_id": inviteID, "username": u.Username})
	})
	return p, sessionToken, err
}
