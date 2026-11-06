// Package db owns the Postgres connection pool and schema migrations.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	dbfs "github.com/streamero-app/streamero/server/db"
)

// Connect opens a pgx pool and verifies connectivity.
func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse db url: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return pool, nil
}

func gooseProvider(pool *pgxpool.Pool) (*goose.Provider, *sql.DB, error) {
	sqlDB := stdlib.OpenDBFromPool(pool)
	migrations, err := fs.Sub(dbfs.Migrations, "migrations")
	if err != nil {
		return nil, nil, err
	}
	p, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations)
	if err != nil {
		_ = sqlDB.Close()
		return nil, nil, err
	}
	return p, sqlDB, nil
}

// Migrate applies all pending application and River (job queue) migrations.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	p, sqlDB, err := gooseProvider(pool)
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	rm, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return err
	}
	if _, err := rm.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("river migrate: %w", err)
	}
	return nil
}

// Pending reports whether any application migration has not been applied.
func Pending(ctx context.Context, pool *pgxpool.Pool) (bool, error) {
	p, sqlDB, err := gooseProvider(pool)
	if err != nil {
		return false, err
	}
	defer sqlDB.Close()
	return p.HasPending(ctx)
}
