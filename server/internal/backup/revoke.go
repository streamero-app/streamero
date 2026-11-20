package backup

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// A backup holds the sessions and API keys that existed when it was taken. A
// restore would bring back every device signed out and every key deleted since
// then, so after a restore all of them are revoked (D-053): only the session of
// the administrator who started the restore is carried over, so they stay
// signed in. Pending Quick Connect codes and single sign-on flows go too.

type keptSession struct {
	id, userID                     uuid.UUID
	tokenHash                      []byte
	deviceName, client             string
	createdAt, lastSeen, expiresAt time.Time
}

// loadSession reads the session to carry over (nil when there is none).
func (s *Service) loadSession(ctx context.Context, id uuid.UUID) (*keptSession, error) {
	if id == uuid.Nil {
		return nil, nil
	}
	k := &keptSession{}
	err := s.Pool.QueryRow(ctx, `SELECT id, user_id, token_hash, device_name, client, created_at, last_seen_at, expires_at
		FROM sessions WHERE id = $1`, id).Scan(&k.id, &k.userID, &k.tokenHash, &k.deviceName, &k.client, &k.createdAt, &k.lastSeen, &k.expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return k, err
}

// revokeCredentials signs out every session, deletes every API key and pending
// sign-in, then puts back keep when its user is still an enabled administrator
// in the restored data.
func (s *Service) revokeCredentials(ctx context.Context, keep *keptSession, res *RestoreResult) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM sessions`)
		if err != nil {
			return err
		}
		res.SignedOutSessions = int(tag.RowsAffected())
		if tag, err = tx.Exec(ctx, `DELETE FROM api_keys`); err != nil {
			return err
		}
		res.RevokedAPIKeys = int(tag.RowsAffected())
		for _, t := range []string{"quick_connect", "oidc_flows", "oidc_handoffs"} {
			if _, err := tx.Exec(ctx, `DELETE FROM `+pgx.Identifier{t}.Sanitize()); err != nil {
				return err
			}
		}
		if keep == nil {
			return nil
		}
		_, err = tx.Exec(ctx, `INSERT INTO sessions (id, user_id, token_hash, device_name, client, created_at, last_seen_at, expires_at)
			SELECT $1, $2, $3, $4, $5, $6, $7, $8 WHERE EXISTS (SELECT 1 FROM users WHERE id = $2 AND is_admin AND NOT disabled)`,
			keep.id, keep.userID, keep.tokenHash, keep.deviceName, keep.client, keep.createdAt, keep.lastSeen, keep.expiresAt)
		return err
	})
}
