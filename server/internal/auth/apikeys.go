package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// APIKeyPrefix marks API keys so they can be told apart from session tokens.
const APIKeyPrefix = "sk_"

// APIKey is an integration key (the secret itself is never stored).
type APIKey struct {
	ID         uuid.UUID
	Name       string
	Prefix     string
	CreatedBy  string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// CreateAPIKey creates a key acting as the calling admin and returns the secret once.
func (s *Service) CreateAPIKey(ctx context.Context, actor Principal, name string) (APIKey, string, error) {
	if actor.IsKid() {
		// A key acts with the account's full rights: never from a kids profile.
		return APIKey{}, "", ErrKidProfile
	}
	name = truncate(name, 100)
	if name == "" {
		return APIKey{}, "", ErrInvalidName
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return APIKey{}, "", err
	}
	secret := APIKeyPrefix + base64.RawURLEncoding.EncodeToString(buf)
	id, err := uuid.NewV7()
	if err != nil {
		return APIKey{}, "", err
	}
	k := APIKey{ID: id, Name: name, Prefix: secret[:len(APIKeyPrefix)+6], CreatedBy: actor.User.Username}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO api_keys (id, name, token_hash, prefix, user_id)
			VALUES ($1, $2, $3, $4, $5) RETURNING created_at`, id, name, hashToken(secret), k.Prefix, actor.User.ID).
			Scan(&k.CreatedAt); err != nil {
			return err
		}
		return audit(ctx, tx, &actor.User.ID, "apikey.created", map[string]any{"api_key_id": id, "name": name})
	})
	return k, secret, err
}

// ListAPIKeys returns all keys.
func (s *Service) ListAPIKeys(ctx context.Context) ([]APIKey, error) {
	rows, err := s.pool.Query(ctx, `SELECT k.id, k.name, k.prefix, u.username, k.created_at, k.last_used_at
		FROM api_keys k JOIN users u ON u.id = k.user_id ORDER BY k.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIKey
	for rows.Next() {
		var k APIKey
		if err := rows.Scan(&k.ID, &k.Name, &k.Prefix, &k.CreatedBy, &k.CreatedAt, &k.LastUsedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// DeleteAPIKey revokes a key immediately.
func (s *Service) DeleteAPIKey(ctx context.Context, actor Principal, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM api_keys WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return audit(ctx, s.pool, &actor.User.ID, "apikey.deleted", map[string]any{"api_key_id": id})
}

// authenticateAPIKey resolves an API key. The key only works while its owner is
// an enabled admin, so demoting or disabling the owner also disables their keys.
func (s *Service) authenticateAPIKey(ctx context.Context, secret string) (Principal, error) {
	var p Principal
	var keyID uuid.UUID
	var lastUsed *time.Time
	var perms []byte
	err := s.pool.QueryRow(ctx, `SELECT k.id, k.last_used_at, u.id, u.username, u.is_admin, u.disabled, `+totpEnabledSQL("u")+`,
			u.created_at, u.last_login_at, u.permissions
		FROM api_keys k JOIN users u ON u.id = k.user_id
		WHERE k.token_hash = $1 AND u.is_admin AND NOT u.disabled`, hashToken(secret)).
		Scan(&keyID, &lastUsed, &p.User.ID, &p.User.Username, &p.User.IsAdmin, &p.User.Disabled, &p.User.TOTPEnabled,
			&p.User.CreatedAt, &p.User.LastLoginAt, &perms)
	p.User.Permissions = parsePermissions(perms)
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, ErrUnauthenticated
	}
	if err != nil {
		return Principal{}, err
	}
	if now := s.now(); lastUsed == nil || now.Sub(*lastUsed) > touchInterval {
		_, _ = s.pool.Exec(ctx, `UPDATE api_keys SET last_used_at = $2 WHERE id = $1`, keyID, now)
	}
	p.APIKeyID = keyID
	return p, nil
}

// RenameSession changes a device's display name. Non-admins may only rename their own.
func (s *Service) RenameSession(ctx context.Context, actor Principal, id uuid.UUID, name string) error {
	name = truncate(name, 100)
	if name == "" {
		return ErrInvalidName
	}
	var owner *uuid.UUID
	if !actor.User.IsAdmin {
		owner = &actor.User.ID
	}
	tag, err := s.pool.Exec(ctx, `UPDATE sessions SET device_name = $3 WHERE id = $1 AND ($2::uuid IS NULL OR user_id = $2)`,
		id, owner, name)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func isAPIKey(token string) bool { return strings.HasPrefix(token, APIKeyPrefix) }
