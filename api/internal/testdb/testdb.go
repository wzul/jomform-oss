// Package testdb provides the shared integration-test database helper.
//
// JomForm's integration tests run against a real Postgres (skipped when
// JOMFORM_TEST_DATABASE_URL is unset). Historically every test package used the
// SAME `public` schema of that one database, so the test DB accumulated rows
// across runs and tests became order- and history-dependent: stale
// webhook_retries filled DueRetries(limit), an affiliate workspace referenced a
// missing owner user, a reused CHIP purchase id collided on subscription_charges,
// and global-count assertions (email_delivery_events) saw other runs' rows.
//
// Open gives each package a scratch schema that is dropped and re-created on
// every test-binary run, then migrated into. A test therefore starts from a
// schema it alone owns, with only migration seed data present:
//
//   - history-independent — a previous run's rows can never be seen, and
//   - cross-package-safe — `go test ./...` runs packages in parallel against
//     the same database, and each package owns its own schema, so a cleanup in
//     one package can no longer delete rows another package is using.
//
// Tables the migrations seed (system_settings, the template library) are
// populated, exactly as a freshly-migrated database would be. For tests that
// assert on the WHOLE table (global List/COUNT), Reset empties the tables a
// test owns at entry and again in t.Cleanup, so the assertion is deterministic
// on every -count.
package testdb

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/db"
)

// DSNEnv is the environment variable holding the integration-test database URL.
const DSNEnv = "JOMFORM_TEST_DATABASE_URL"

var (
	once sync.Once
	pool *pgxpool.Pool
	err  error
)

// Open returns a pool connected to this test package's scratch schema, applying
// migrations on first use (mirroring the production boot path). It skips the
// test when DSNEnv is unset, so unit-only runs stay hermetic.
//
// The scratch schema is named after the test package and is dropped and
// re-created on every call — i.e. once per test-binary run — so no row from a
// previous run survives into this one.
func Open(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv(DSNEnv)
	if dsn == "" {
		t.Skipf("%s not set; skipping integration test", DSNEnv)
	}
	once.Do(func() { pool, err = open(context.Background(), dsn, pkgName()) })
	if err != nil {
		t.Fatalf("test database unavailable: %v", err)
	}
	return pool
}

// Connect is Open for tests that use subtests (t.Run) and therefore want the
// package pool without a *testing.T. It is safe to call from any test in the
// package; the pool is created once.
func Connect(t *testing.T) (*pgxpool.Pool, error) {
	t.Helper()
	dsn := os.Getenv(DSNEnv)
	if dsn == "" {
		t.Skipf("%s not set; skipping integration test", DSNEnv)
	}
	once.Do(func() { pool, err = open(context.Background(), dsn, pkgName()) })
	return pool, err
}

func open(ctx context.Context, dsn, schema string) (*pgxpool.Pool, error) {
	// Admin connection to (re)create the scratch schema.
	admin, err := db.Connect(ctx, dsn)
	if err != nil {
		return nil, err
	}
	defer admin.Close()
	if err := ensureSchema(ctx, admin, schema); err != nil {
		return nil, err
	}

	// Pool whose connections resolve unqualified names to the scratch schema
	// first, then `public` for shared extensions (pgcrypto, citext). This is the
	// same mechanism as `SET search_path` in psql.
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	if err := p.Ping(ctx); err != nil {
		p.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	if err := migrate(ctx, p, schema); err != nil {
		p.Close()
		return nil, err
	}
	return p, nil
}

func ensureSchema(ctx context.Context, admin *pgxpool.Pool, schema string) error {
	if _, err := admin.Exec(ctx, `DROP SCHEMA IF EXISTS `+quote(schema)+` CASCADE`); err != nil {
		return fmt.Errorf("drop scratch schema: %w", err)
	}
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+quote(schema)); err != nil {
		return fmt.Errorf("create scratch schema: %w", err)
	}
	return nil
}

// migrate applies the embedded migrations into schema. Unlike db.Migrate it
// does not consult schema_migrations: the scratch schema was just re-created,
// so every migration must run (seed data included), and a single multi-
// statement Exec keeps the ~64 migrations to one round trip.
func migrate(ctx context.Context, p *pgxpool.Pool, schema string) error {
	migrations, err := db.MigrationStatements()
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("SET search_path=" + quote(schema) + ",public;\n")
	for _, m := range migrations {
		b.WriteString(m.SQL)
		b.WriteString(";\n")
	}
	if _, err := p.Exec(ctx, b.String()); err != nil {
		return fmt.Errorf("migrate scratch schema %s: %w", schema, err)
	}
	return nil
}

// Reset empties the given tables so a test that asserts on global table state
// (a List/COUNT over the whole table) starts deterministic, and schedules the
// same cleanup for after the test. Because each package owns its own schema,
// this only ever affects rows this package created — it can never delete rows
// another package is using concurrently.
//
// Call it before seeding the rows under test:
//
//	pool := testdb.Open(t)
//	testdb.Reset(t, pool, "email_delivery_events")
//
// The reset runs again in t.Cleanup, so a -count=3 run leaves each iteration a
// clean schema. Unknown table names fail the test rather than silently passing.
func Reset(t *testing.T, pool *pgxpool.Pool, tables ...string) {
	t.Helper()
	clean := func() error {
		if len(tables) == 0 {
			return nil
		}
		stmt := "TRUNCATE TABLE " + strings.Join(quoted(tables), ", ") + " RESTART IDENTITY CASCADE"
		_, err := pool.Exec(context.Background(), stmt)
		return err
	}
	if err := clean(); err != nil {
		t.Fatalf("testdb.Reset(%v): %v", tables, err)
	}
	t.Cleanup(func() {
		if err := clean(); err != nil {
			t.Errorf("testdb.Reset cleanup(%v): %v", tables, err)
		}
	})
}

// ResetSchema drops and re-creates this package's scratch schema, wiping every
// row (including migration seed data) and re-applying migrations. Use it only
// for a test that must observe a truly empty database; prefer Reset for
// per-table cleanup.
func ResetSchema(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv(DSNEnv)
	if dsn == "" {
		t.Skipf("%s not set; skipping integration test", DSNEnv)
	}
	once.Do(func() { pool, err = open(context.Background(), dsn, pkgName()) })
	if err != nil {
		t.Fatalf("test database unavailable: %v", err)
	}
	schema := pkgName()
	admin, cerr := db.Connect(context.Background(), dsn)
	if cerr != nil {
		t.Fatalf("test database unavailable: %v", cerr)
	}
	defer admin.Close()
	if cerr := ensureSchema(context.Background(), admin, schema); cerr != nil {
		t.Fatalf("reset scratch schema: %v", cerr)
	}
	if cerr := migrate(context.Background(), pool, schema); cerr != nil {
		t.Fatalf("re-migrate scratch schema: %v", cerr)
	}
	return pool
}

// NewLogger is the discard logger used for migration output.
func NewLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

// pkgName derives a schema name from the calling test package (e.g.
// internal/dbstore → test_dbstore). Different packages run in parallel against
// the same database, so the name must be per-package.
func pkgName() string {
	return "test_" + sanitize(pkgFromFrames())
}

func sanitize(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "pkg"
	}
	return b.String()
}

func quote(ident string) string {
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}

func quoted(idents []string) []string {
	out := make([]string, len(idents))
	for i, id := range idents {
		out[i] = quote(id)
	}
	return out
}
