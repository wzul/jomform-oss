// Package db — embedded migrations applied on startup.
package db

import (
	"context"
	"embed"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.up.sql
var migrationFS embed.FS

// Migration is one embedded migration file: its filename (also the version
// recorded in schema_migrations) and its SQL body.
type Migration struct {
	Name string
	SQL  string
}

// MigrationStatements returns the embedded *.up.sql migrations in the order
// Migrate applies them. It lets a caller apply the schema somewhere other than
// the default search_path (e.g. an isolated per-test scratch schema) while
// keeping migration ordering single-sourced here.
func MigrationStatements() ([]Migration, error) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	var files []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".up.sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	out := make([]Migration, 0, len(files))
	for _, name := range files {
		sqlBytes, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return nil, err
		}
		out = append(out, Migration{Name: name, SQL: string(sqlBytes)})
	}
	return out, nil
}

// Migrate applies any pending *.up.sql migrations in filename order.
// Each file runs in a transaction; applied files are recorded in
// schema_migrations so restarts are no-ops.
//
// The shared extensions (pgcrypto, citext) are provisioned into `public` first,
// serialized across processes: migration 000001 declares them with
// `CREATE EXTENSION IF NOT EXISTS`, which is not race-safe, and under a
// search_path that does not start with public it would install them into the
// first schema that ran it — leaving every other schema to fail on
// pg_extension_name_index. See EnsureSharedExtensions.
func Migrate(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	if err := EnsureSharedExtensions(ctx, pool, log); err != nil {
		return err
	}

	if _, err := pool.Exec(ctx,
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	files, err := MigrationStatements()
	if err != nil {
		return err
	}

	for _, m := range files {
		name := m.Name
		var exists bool
		if err := pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, name,
		).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}

		tx, err := pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, m.SQL); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, name); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit %s: %w", name, err)
		}
		log.Info("migration_applied", "file", name)
	}
	return nil
}
