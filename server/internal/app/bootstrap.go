// Package app wires configuration, dependencies and HTTP serving together.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/streamero-app/streamero/server/internal/config"
	"github.com/streamero-app/streamero/server/internal/db"
	"github.com/streamero-app/streamero/server/internal/settings"
)

// Deps are the shared runtime dependencies.
type Deps struct {
	Pool   *pgxpool.Pool
	Valkey *redis.Client
}

// Close releases all dependencies.
func (d *Deps) Close() {
	if d.Valkey != nil {
		_ = d.Valkey.Close()
	}
	if d.Pool != nil {
		d.Pool.Close()
	}
}

// Bootstrap connects to Postgres and Valkey (retrying until ctx is done),
// applies or verifies migrations, then marks state ready.
func Bootstrap(ctx context.Context, cfg config.Config, state *State, log *slog.Logger) (*Deps, error) {
	deps := &Deps{}
	backoff := 250 * time.Millisecond
	for {
		err := tryBootstrap(ctx, cfg, deps, state)
		if err == nil {
			state.ready.Store(true)
			return deps, nil
		}
		state.setErr(err)
		log.Warn("startup: dependency not ready, retrying", "err", err, "in", backoff)
		select {
		case <-ctx.Done():
			deps.Close()
			return nil, errors.Join(ctx.Err(), err)
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 5*time.Second)
	}
}

func tryBootstrap(ctx context.Context, cfg config.Config, deps *Deps, state *State) error {
	if deps.Pool == nil {
		pool, err := db.Connect(ctx, cfg.DatabaseURL)
		if err != nil {
			return fmt.Errorf("postgres: %w", err)
		}
		deps.Pool = pool
	}
	if deps.Valkey == nil {
		opts, err := redis.ParseURL(cfg.ValkeyURL)
		if err != nil {
			return fmt.Errorf("valkey url: %w", err)
		}
		deps.Valkey = redis.NewClient(opts)
	}
	if err := deps.Valkey.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("valkey: %w", err)
	}
	if cfg.AutoMigrate {
		if err := db.Migrate(ctx, deps.Pool); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	} else if pending, err := db.Pending(ctx, deps.Pool); err != nil {
		return fmt.Errorf("check migrations: %w", err)
	} else if pending {
		return errors.New("database has pending migrations; run `streamero migrate`")
	}
	st := settings.New(deps.Pool)
	id, err := st.ServerID(ctx)
	if err != nil {
		return fmt.Errorf("server id: %w", err)
	}
	state.serverID.Store(&id)
	state.settings.Store(st)
	return nil
}
