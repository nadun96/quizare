// Package db opens PostgreSQL and applies the tutoring schema's migrations.
package db

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Open connects, retrying for up to wait so the service can start before the database.
func Open(ctx context.Context, url string, maxConns int32, wait time.Duration) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	if maxConns > 0 {
		cfg.MaxConns = maxConns
	}
	deadline := time.Now().Add(wait)
	for {
		pool, err := pgxpool.NewWithConfig(ctx, cfg)
		if err == nil {
			if err = pool.Ping(ctx); err == nil {
				return pool, nil
			}
			pool.Close()
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("database: %w", err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// Migrate applies every migration not applied yet, each in its own
// transaction. The list is tutoring.tutoring_schema_migrations, so it never mixes
// with the platform's own list when both share a database.
func Migrate(ctx context.Context, pool *pgxpool.Pool, fsys embed.FS) error {
	// The schema is created by the first migration; the list lives beside it.
	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS public.tutoring_schema_migrations (
		version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	names, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		version := strings.TrimSuffix(name, ".sql")
		var done bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM public.tutoring_schema_migrations WHERE version=$1)`, version).Scan(&done); err != nil {
			return err
		}
		if done {
			continue
		}
		sql, err := fsys.ReadFile(name)
		if err != nil {
			return err
		}
		if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(sql)); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO public.tutoring_schema_migrations (version) VALUES ($1)`, version)
			return err
		}); err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
	}
	return nil
}
