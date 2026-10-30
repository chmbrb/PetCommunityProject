// SPDX-License-Identifier: AGPL-3.0-or-later

// Package database connects to PostgreSQL, applies migrations, and exposes the
// generated queries (ADR 0004, ADR 0005).
package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver for goose
	"github.com/pressly/goose/v3"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/chmbrb/PetCommunityProject/internal/store"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Store combines the generated queries with connection-level operations.
type Store struct {
	*store.Queries
	pool *pgxpool.Pool
}

// Connect opens a connection pool and verifies the database is reachable.
func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open database pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

// NewStore wraps a connection pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{Queries: store.New(pool), pool: pool}
}

// Ping reports whether the database is reachable.
func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// InTx runs fn in a transaction, committing if it returns nil and rolling back otherwise.
// fn gets the raw transaction too, so it can insert jobs in the same transaction
// (ADR 0006).
func (s *Store) InTx(ctx context.Context, fn func(tx pgx.Tx, q *store.Queries) error) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		return fn(tx, s.WithTx(tx))
	})
}

// MigrationResult describes one applied migration.
type MigrationResult struct {
	Version int64
	Source  string
}

// MigrationStatus describes one known migration and whether it has been applied.
type MigrationStatus struct {
	Version int64
	Source  string
	Applied bool
}

// Migrate applies all pending migrations: the project's own, then the job queue's
// (River manages its own schema).
func Migrate(ctx context.Context, databaseURL string) ([]MigrationResult, error) {
	provider, db, err := newProvider(databaseURL)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	results, err := provider.Up(ctx)
	if err != nil {
		return nil, fmt.Errorf("apply migrations: %w", err)
	}
	applied := make([]MigrationResult, 0, len(results))
	for _, r := range results {
		applied = append(applied, MigrationResult{Version: r.Source.Version, Source: r.Source.Path})
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return applied, fmt.Errorf("open database pool: %w", err)
	}
	defer pool.Close()
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return applied, fmt.Errorf("load job queue migrations: %w", err)
	}
	riverResult, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil)
	if err != nil {
		return applied, fmt.Errorf("apply job queue migrations: %w", err)
	}
	for _, v := range riverResult.Versions {
		applied = append(applied, MigrationResult{Version: int64(v.Version), Source: fmt.Sprintf("river v%d", v.Version)})
	}
	return applied, nil
}

// Status lists every known migration and whether it has been applied.
func Status(ctx context.Context, databaseURL string) ([]MigrationStatus, error) {
	provider, db, err := newProvider(databaseURL)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	statuses, err := provider.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("read migration status: %w", err)
	}
	out := make([]MigrationStatus, 0, len(statuses))
	for _, s := range statuses {
		out = append(out, MigrationStatus{
			Version: s.Source.Version,
			Source:  s.Source.Path,
			Applied: s.State == goose.StateApplied,
		})
	}
	return out, nil
}

func newProvider(databaseURL string) (*goose.Provider, *sql.DB, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("open database: %w", err)
	}
	migrations, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations)
	if err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("load migrations: %w", err)
	}
	return provider, db, nil
}
