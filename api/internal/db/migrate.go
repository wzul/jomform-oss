// Package db — embedded migrations applied on startup.
package db

import (
	"context"
	"embed"
	"fmt"
	"log/slog"
	"regexp"
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

// createExtensionRE finds every CREATE EXTENSION statement in a migration and
// captures the extension name plus the rest of the statement — the tail must
// carry an explicit `SCHEMA public`.
//
// An unqualified CREATE EXTENSION lands the extension in the FIRST schema of
// the session's search_path, and `IF NOT EXISTS` then matches it by name
// GLOBALLY (pg_extension's name index is cluster-wide, not per schema). A
// caller applying the migrations with `SET search_path=<scratch>,public` — as
// internal/testdb does — would therefore strand the extension in its own
// scratch schema, where no other caller can see it: SQLSTATE 42704, `type
// "citext" does not exist`. Pinning the schema is what keeps the placement
// independent of the caller's search_path.
//
// The name is deliberately narrow — an unquoted lower-case identifier, which is
// the only form a migration has ever used and the form Postgres folds to
// lower case anyway. A quoted or otherwise exotic name would not be collected,
// which is why the callers fail loudly on an empty result instead of treating
// "found nothing" as "nothing to check".
var createExtensionRE = regexp.MustCompile(`(?is)\bCREATE\s+EXTENSION\s+(?:IF\s+NOT\s+EXISTS\s+)?([a-z_][a-z0-9_]*)([^;]*);`)

// stripSQLComments removes `--` line comments and (nested) `/* */` block
// comments from migration SQL, and blanks out dollar-quoted bodies (`$$ … $$`,
// `$tag$ … $tag$`) — a PL/pgSQL function body is arbitrary text and must not be
// read as top-level DDL.
//
// Without this, prose in a comment is indistinguishable from a statement: a
// doc comment saying "CREATE EXTENSION creates it in the first schema…" is
// itself collected as `CREATE EXTENSION creates`, which silently corrupts the
// list of extensions the callers depend on. String literals are intentionally
// left alone — blanking them would corrupt the `SCHEMA public` clause the
// callers match on.
func stripSQLComments(sql string) string {
	var b strings.Builder
	b.Grow(len(sql))
	for i := 0; i < len(sql); {
		switch {
		case strings.HasPrefix(sql[i:], "--"):
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
		case strings.HasPrefix(sql[i:], "/*"):
			depth := 0
			for i < len(sql) {
				if strings.HasPrefix(sql[i:], "/*") {
					depth++
					i += 2
					continue
				}
				if strings.HasPrefix(sql[i:], "*/") {
					depth--
					i += 2
					if depth == 0 {
						break
					}
					continue
				}
				i++
			}
		case sql[i] == '\'':
			// Single-quoted literal: copy verbatim, including '' escapes.
			b.WriteByte(sql[i])
			i++
			for i < len(sql) {
				b.WriteByte(sql[i])
				if sql[i] == '\'' {
					if i+1 < len(sql) && sql[i+1] == '\'' {
						b.WriteByte(sql[i+1])
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
		case sql[i] == '$':
			tag := dollarQuoteTag(sql[i:])
			if tag == "" {
				b.WriteByte(sql[i])
				i++
				continue
			}
			end := strings.Index(sql[i+len(tag):], tag)
			if end < 0 {
				// Unterminated: everything left is the body.
				return b.String()
			}
			i += len(tag) + end + len(tag)
		default:
			b.WriteByte(sql[i])
			i++
		}
	}
	return b.String()
}

// dollarQuoteTag recognises a dollar-quote opener at the start of s (`$$`,
// `$tag$` — a tag is an identifier) and returns it, or "" if s does not open a
// dollar-quoted string. A `$1` placeholder or a `$` in any other position
// returns "" so the caller copies it verbatim.
func dollarQuoteTag(s string) string {
	if len(s) == 0 || s[0] != '$' {
		return ""
	}
	for j := 1; j < len(s); j++ {
		c := s[j]
		switch {
		case c == '$':
			return s[:j+1]
		case c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (j > 1 && c >= '0' && c <= '9'):
		default:
			return ""
		}
	}
	return ""
}

// DeclaredExtensions returns the extension names the embedded migrations
// declare, in migration order. It is the single source for "which extensions
// must be reachable from any schema" — internal/testdb's repair and the
// placement guards both read it, and db.SharedExtensions is derived from it, so
// adding an extension to a migration cannot leave a caller behind.
func DeclaredExtensions() ([]string, error) {
	migrations, err := MigrationStatements()
	if err != nil {
		return nil, err
	}
	var names []string
	for _, m := range migrations {
		for _, match := range createExtensionRE.FindAllStringSubmatch(stripSQLComments(m.SQL), -1) {
			names = append(names, match[1])
		}
	}
	return names, nil
}

// Migrate applies any pending *.up.sql migrations in filename order.
// Each file runs in a transaction; applied files are recorded in
// schema_migrations so restarts are no-ops.
//
// The shared extensions are provisioned into `public` first, serialized across
// processes: migration 000001 declares them with `CREATE EXTENSION IF NOT
// EXISTS`, which is not race-safe, and under a search_path that does not start
// with public it would install them into the first schema that ran it — leaving
// every other schema to fail on pg_extension_name_index. See
// EnsureSharedExtensions.
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
