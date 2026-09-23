// Package db — fresh-database migration guard (issue #473).
//
// Incident this guards against: migration 000047_affiliates declared
// affiliates.workspace_id as TEXT with a FK to workspaces(id), which is uuid.
// Postgres rejected the constraint (SQLSTATE 42804, "foreign key constraint
// cannot be implemented") and the API restart-looped in production until a
// hotfix shipped. Nothing in the test suite ever applied the migration set to
// an empty database, so the failure was first seen by users.
//
// TestMigrationsApplyFromZero closes that gap: it creates a throwaway database
// with a unique name, runs EVERY embedded *.up.sql through the same runner the
// API uses at startup (Migrate), and fails on the first error. That catches
// type mismatches, bad/absent FKs, ordering bugs and duplicate objects — the
// whole 000047 bug class — before the code is merged.
//
// TestEveryMigrationHasDownFile is the companion static check: every up
// migration must ship a rollback file, so the set stays reversible. Two
// pre-existing gaps are pinned in the allowlist below rather than silently
// tolerated; a full reverse-rollback integration test cannot be asserted while
// those gaps exist (rolling back stops at 000001_init.down.sql, which cannot
// drop users while the spaces table created by 000012 is still present).
//
// Both tests skip unless JOMFORM_TEST_DATABASE_URL is set, matching the
// dbstore integration-test convention. The scratch database is dropped on
// cleanup, so the test is safe to run repeatedly and never touches the
// database named in the DSN.

package db

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/url"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// scratchPrefix names the throwaway databases this test creates. Kept short so
// the generated name stays well inside Postgres' 63-byte identifier limit.
const scratchPrefix = "jomform_migtest_"

// migrationsWithoutDown lists migrations that predate the rollback rule and
// have no .down.sql. Keyed by version, value explains why it is pinned. These
// are pre-existing gaps, not endorsements: new migrations must always ship a
// matching .down.sql or TestEveryMigrationHasDownFile fails.
var migrationsWithoutDown = map[string]string{
	"000005_form_overrides": "predates the down-file rule; tracked as a rollback gap",
	"000012_spaces":         "predates the down-file rule; tracked as a rollback gap",
}

// migrationUpFiles returns every embedded *.up.sql in the same order the
// startup runner applies them (filename order).
func migrationUpFiles(t *testing.T) []string {
	t.Helper()
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	var files []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".up.sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	if len(files) == 0 {
		t.Fatal("no embedded *.up.sql migrations found — the go:embed directive is broken")
	}
	return files
}

// currentDatabase reports the database the pool is actually connected to.
func currentDatabase(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	var name string
	err := pool.QueryRow(ctx, `SELECT current_database()`).Scan(&name)
	return name, err
}

// dsnFor returns the connection string with its database name replaced.
//
// Do NOT go back to pgx.ParseConfig(dsn) + cfg.Database = database +
// cfg.ConnString(): pgx's ConnString() returns the ORIGINAL string and discards
// a mutated Database, so the caller silently keeps connecting to the DSN's own
// database. That defect (issue #532) made this guard connect to the shared test
// database instead of the empty scratch one, find every migration already
// applied, apply none, and pass vacuously — the exact 000047 class it exists to
// catch could have shipped green.
//
// Building the URL with url.URL keeps it explicit and correct, and
// TestMigrationsApplyFromZero additionally asserts (via current_database) that
// the pool really landed on the scratch database, so a regression here fails
// loudly instead of silently disabling the guard.
func dsnFor(t *testing.T, dsn, database string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse JOMFORM_TEST_DATABASE_URL: %v", err)
	}
	u.Path = "/" + database
	return u.String()
}

// createScratchDB creates a uniquely named empty database and registers its
// removal with t.Cleanup. It connects through the DSN's own database so no
// separate "postgres" maintenance database needs to exist.
//
// It passes no TEMPLATE clause, i.e. it copies the server's default template
// (template1) — the same database shape the DSN's own database has. Use
// createScratchDBFromTemplate when the fixture needs a starting state it
// controls rather than one the server happens to have.
func createScratchDB(t *testing.T, ctx context.Context, dsn string) string {
	t.Helper()
	return createScratchDBFromTemplate(t, ctx, dsn, "")
}

// createScratchDBFromTemplate is createScratchDB with an explicit template: an
// empty string means "no TEMPLATE clause" (Postgres' default, template1), any
// other value is used as `CREATE DATABASE … TEMPLATE <template>`.
//
// A fixture that plants a pre-existing state must copy template0. A
// template1-derived database inherits whatever the server's template1 carries —
// on a provisioned server that already includes pgcrypto and citext in
// `public` — which silently changes what the fixture proves: an unqualified
// `CREATE EXTENSION` resolved through a `search_path` that ends in `public`
// then finds the inherited extension and dies with SQLSTATE 42710 ("extension
// … already exists") instead of planting the state the guard is about. Copying
// template0 makes the starting state deterministic on every server, provisioned
// or not.
func createScratchDBFromTemplate(t *testing.T, ctx context.Context, dsn, template string) string {
	t.Helper()
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("generate scratch database name: %v", err)
	}
	name := scratchPrefix + hex.EncodeToString(buf)

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to test database to create scratch db: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	stmt := "CREATE DATABASE " + pgx.Identifier{name}.Sanitize()
	if template != "" {
		stmt += " TEMPLATE " + pgx.Identifier{template}.Sanitize()
	}
	if _, err := conn.Exec(ctx, stmt); err != nil {
		t.Fatalf("create scratch database %s (needs CREATEDB): %v", name, err)
	}

	t.Cleanup(func() {
		dropCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		c, err := pgx.Connect(dropCtx, dsn)
		if err != nil {
			t.Logf("cleanup: could not connect to drop scratch database %s: %v", name, err)
			return
		}
		defer func() { _ = c.Close(dropCtx) }()
		quoted := pgx.Identifier{name}.Sanitize()
		if _, err := c.Exec(dropCtx, "DROP DATABASE IF EXISTS "+quoted+" WITH (FORCE)"); err != nil {
			// WITH (FORCE) needs Postgres 13+; fall back to a plain drop.
			if _, err2 := c.Exec(dropCtx, "DROP DATABASE IF EXISTS "+quoted); err2 != nil {
				t.Logf("cleanup: could not drop scratch database %s: %v", name, err2)
			}
		}
	})

	return name
}

// TestMigrationsApplyFromZero applies the full embedded migration set to a
// brand-new database and fails on any error — the guard for the 000047 class
// of production crash-loop.
func TestMigrationsApplyFromZero(t *testing.T) {
	dsn := os.Getenv("JOMFORM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping fresh-DB migration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	ups := migrationUpFiles(t)

	name := createScratchDB(t, ctx, dsn)
	pool, err := Connect(ctx, dsnFor(t, dsn, name))
	if err != nil {
		t.Fatalf("connect to scratch database %s: %v", name, err)
	}
	defer pool.Close()

	// Issue #532: prove we are actually on the throwaway database, and that it
	// starts EMPTY. Without this the guard can silently degrade into applying
	// the migrations to the shared test database (where they are already
	// applied), finding every migration present, applying none, and passing
	// vacuously — i.e. the 000047 class could ship green. Asserting here means
	// any regression fails loudly instead.
	if cur, err := currentDatabase(ctx, pool); err != nil {
		t.Fatalf("resolve current_database for the scratch pool: %v", err)
	} else if cur != name {
		t.Fatalf("fresh-DB guard is connected to %q but the scratch database is %q — "+
			"the migrations would be applied to the wrong database and the guard would "+
			"pass vacuously (see issue #532)", cur, name)
	}
	var preExisting int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.tables
		 WHERE table_schema = 'public' AND table_name = 'schema_migrations'`,
	).Scan(&preExisting); err != nil {
		t.Fatalf("probe scratch database for a pre-existing schema_migrations: %v", err)
	}
	if preExisting != 0 {
		t.Fatalf("scratch database %s is NOT empty: schema_migrations already exists — "+
			"the from-zero guard cannot prove anything against a pre-migrated database", name)
	}

	log := slog.New(slog.DiscardHandler)
	if err := Migrate(ctx, pool, log); err != nil {
		t.Fatalf("applying %d migrations to an empty database failed: %v\n\n"+
			"This is exactly the 000047 class of bug (a migration that only fails "+
			"on a database where the object does not already exist). It MUST be "+
			"fixed before merge — see AGENTS.md 'Adding a migration safely'.", len(ups), err)
	}

	// Every up file must be recorded as applied. A migration that was silently
	// skipped (or a version-key collision) shows up here.
	rows, err := pool.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		t.Fatalf("read schema_migrations: %v", err)
	}
	applied := make(map[string]bool)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			t.Fatalf("scan schema_migrations: %v", err)
		}
		applied[v] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate schema_migrations: %v", err)
	}

	for _, f := range ups {
		if !applied[f] {
			t.Errorf("migration %s was not recorded in schema_migrations after Migrate()", f)
		}
	}
	if len(applied) != len(ups) {
		t.Errorf("schema_migrations has %d rows, expected %d (one per *.up.sql)", len(applied), len(ups))
	}

	// A restart must be a no-op: the production boot path runs Migrate on every
	// start, so a migration that is not idempotent would crash-loop too.
	if err := Migrate(ctx, pool, log); err != nil {
		t.Fatalf("second Migrate() run (restart path) failed: %v", err)
	}
}

// TestEveryMigrationHasDownFile enforces that each up migration ships a
// rollback file. The two allowlisted gaps predate the rule and are pinned so
// they cannot grow silently.
//
// This reads the migrations directory from disk, not migrationFS: the embed
// directive only pulls in *.up.sql (the runtime runner never needs the downs).
func TestEveryMigrationHasDownFile(t *testing.T) {
	entries, err := os.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read migrations directory: %v", err)
	}
	have := make(map[string]bool)
	for _, e := range entries {
		have[e.Name()] = true
	}

	for _, up := range migrationUpFiles(t) {
		version := strings.TrimSuffix(up, ".up.sql")
		if have[version+".down.sql"] {
			continue
		}
		if _, pinned := migrationsWithoutDown[version]; pinned {
			continue
		}
		t.Errorf("migration %s has no %s.down.sql — every new migration must ship a rollback (see AGENTS.md)", up, version)
	}

	// Fail loudly if a pinned gap is fixed, so the allowlist cannot rot.
	for version, reason := range migrationsWithoutDown {
		if have[version+".down.sql"] {
			t.Errorf("%s now has a .down.sql (%s) — remove it from migrationsWithoutDown", version, reason)
		}
	}
}
