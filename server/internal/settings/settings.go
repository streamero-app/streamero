// Package settings reads and writes server-wide settings stored in Postgres.
package settings

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Keys of well-known settings.
const (
	KeyServerID       = "server.id"
	KeySetupCompleted = "setup.completed"
)

// Store is a settings store backed by the settings table.
type Store struct{ pool *pgxpool.Pool }

// New returns a Store.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Get decodes the setting into dst. It returns false if the key is not set.
func (s *Store) Get(ctx context.Context, key string, dst any) (bool, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT value FROM settings WHERE key = $1`, key).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal(raw, dst)
}

// Set stores the setting.
func (s *Store) Set(ctx context.Context, key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO settings (key, value) VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`, key, raw)
	return err
}

// ServerID returns the stable server ID, creating it on first call.
func (s *Store) ServerID(ctx context.Context) (uuid.UUID, error) {
	candidate, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, err
	}
	raw, _ := json.Marshal(candidate.String())
	// Insert-if-absent keeps concurrent starts (serve + worker) consistent.
	if _, err := s.pool.Exec(ctx, `INSERT INTO settings (key, value) VALUES ($1, $2)
		ON CONFLICT (key) DO NOTHING`, KeyServerID, raw); err != nil {
		return uuid.Nil, err
	}
	var id string
	if _, err := s.Get(ctx, KeyServerID, &id); err != nil {
		return uuid.Nil, err
	}
	return uuid.Parse(id)
}
