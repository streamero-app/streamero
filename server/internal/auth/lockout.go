package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/streamero-app/streamero/server/internal/netx"
)

// Account lockout without a lockout of the owner (D-053). Wrong passwords from
// addresses that never signed in to the account lock the account for
// LockoutDuration, as before. An address that signed in successfully before is
// a known client: it has its own failure count and lock, and the account lock
// does not apply to it. Someone who only knows a username can therefore no
// longer keep its owner out by guessing passwords from elsewhere.

// knownClientTTL is how long a successful sign-in keeps an address known.
const knownClientTTL = 90 * 24 * time.Hour

// clientAddress is the caller's address as the router resolved it ("" unknown).
func clientAddress(ctx context.Context) string {
	if ip := netx.FromContext(ctx); ip.IsValid() {
		return ip.Unmap().String()
	}
	return ""
}

// knownClient reports whether the caller's address signed in to the account
// before, and until when that address is locked (nil = not locked).
func (s *Service) knownClient(ctx context.Context, userID uuid.UUID, now time.Time) (bool, *time.Time, error) {
	addr := clientAddress(ctx)
	if addr == "" {
		return false, nil, nil
	}
	var locked *time.Time
	err := s.pool.QueryRow(ctx, `SELECT locked_until FROM login_clients WHERE user_id = $1 AND address = $2 AND last_login_at > $3`,
		userID, addr, now.Add(-knownClientTTL)).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, err
	}
	if locked != nil && !now.Before(*locked) {
		locked = nil
	}
	return true, locked, nil
}

// recordClientFailure counts a failure of a known client against that client only.
func (s *Service) recordClientFailure(ctx context.Context, userID uuid.UUID, now time.Time, reason string, fail error) error {
	var lock *time.Time
	if err := s.pool.QueryRow(ctx, `UPDATE login_clients SET
			locked_until  = CASE WHEN failed_logins + 1 >= $3 THEN $4 ELSE locked_until END,
			failed_logins = CASE WHEN failed_logins + 1 >= $3 THEN 0 ELSE failed_logins + 1 END
		WHERE user_id = $1 AND address = $2 RETURNING CASE WHEN locked_until > $5 THEN locked_until END`,
		userID, clientAddress(ctx), MaxFailedLogins, now.Add(LockoutDuration), now).Scan(&lock); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return s.recordFailedLogin(ctx, userID, now, reason, fail)
		}
		return err
	}
	_ = audit(ctx, s.pool, &userID, "login.failed", map[string]any{"reason": reason, "known_client": true, "locked": lock != nil})
	if lock != nil {
		return &LockedError{Until: *lock}
	}
	return fail
}

// rememberClient marks the caller's address as known after a successful sign-in.
func rememberClient(ctx context.Context, db execer, userID uuid.UUID, now time.Time) error {
	addr := clientAddress(ctx)
	if addr == "" {
		return nil
	}
	if _, err := db.Exec(ctx, `INSERT INTO login_clients (user_id, address, last_login_at) VALUES ($1, $2, $3)
		ON CONFLICT (user_id, address) DO UPDATE SET last_login_at = EXCLUDED.last_login_at, failed_logins = 0, locked_until = NULL`,
		userID, addr, now); err != nil {
		return err
	}
	_, err := db.Exec(ctx, `DELETE FROM login_clients WHERE user_id = $1 AND last_login_at < $2`, userID, now.Add(-knownClientTTL))
	return err
}
