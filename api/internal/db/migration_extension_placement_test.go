// Package db — extension placement guard.
//
// Incident this guards against: 000001_init.up.sql declared its shared
// extensions with an unqualified `CREATE EXTENSION IF NOT EXISTS citext`.
// A CREATE EXTENSION without SCHEMA puts the extension in the FIRST schema of
// the session's search_path, and internal/testdb applies the migration set with
// `SET search_path=<package scratch schema>,public`. On a database that had no
// extensions yet, whichever test package migrated first therefore created
// citext *inside its own scratch schema*, and every other package — which
// reaches unqualified names through `<its schema>,public` — failed with
// `type "citext" does not exist` (SQLSTATE 42704). The suite only passed where
// a human had pre-created the extensions in `public`.
//
// TestMigrationsPlaceExtensionsInPublicFromAnySearchPath reproduces that setup
// hermetically: a throwaway database created from template0, so it genuinely
// has no extensions, the full migration set applied under a scratch
// search_path, then the placement asserted from another schema. The database is
// dropped on cleanup and the DSN's own database is never touched.

package db

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// createExtensionFreeDB creates a uniquely named database from TEMPLATE
// template0 and registers its removal with t.Cleanup, returning its name.
//
// It delegates to createScratchDBFromTemplate (migrations_fresh_db_test.go) so
// the package has ONE definition of "make me an empty database": two helpers
// that both create a template0 database would drift, and the template0 detail is
// the one that makes this guard non-vacuous, so it must not be duplicated
// anywhere it can be got wrong independently.
func createExtensionFreeDB(t *testing.T, ctx context.Context, dsn string) string {
	t.Helper()
	return createScratchDBFromTemplate(t, ctx, dsn, "template0")
}

// declaredExtensions lists the extensions the embedded migrations declare, and
// fails the test if the derivation comes back empty.
//
// This is the static half of the guard: a NEW migration that declares an
// extension unqualified reintroduces the bug even if the placement assertions
// below still happen to pass on the machine that runs them, so the omission
// must fail the build on its own.
func declaredExtensionsFromMigrations(t *testing.T) []string {
	t.Helper()
	names, err := DeclaredExtensions()
	if err != nil {
		t.Fatalf("read declared extensions from the embedded migrations: %v", err)
	}
	if len(names) == 0 {
		t.Fatalf("no CREATE EXTENSION found in the embedded migration set — either the migrations " +
			"stopped declaring the shared extensions or the parser in db.DeclaredExtensions no longer " +
			"matches them; the placement guard below would then pass vacuously")
	}
	return namedExtensionsFromSQL(t, "every migration declares SCHEMA public")
}

// namedExtensionsFromSQL re-scans each migration for CREATE EXTENSION and
// asserts the statement pins `SCHEMA public`, returning the names it found.
// It shares db's comment-aware view of the SQL, so a statement quoted in a
// comment is not mistaken for a declaration.
func namedExtensionsFromSQL(t *testing.T, why string) []string {
	t.Helper()
	migrations, err := MigrationStatements()
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	var names []string
	for _, m := range migrations {
		for _, match := range createExtensionRE.FindAllStringSubmatch(stripSQLComments(m.SQL), -1) {
			if !strings.Contains(strings.ToUpper(match[2]), "SCHEMA PUBLIC") {
				t.Errorf("%s declares `CREATE EXTENSION %s` without `SCHEMA public` (%s).\n"+
					"An unqualified CREATE EXTENSION lands the extension in the first schema of the "+
					"session's search_path (and IF NOT EXISTS then matches it by name from anywhere), so "+
					"a caller applying the migrations with `SET search_path=<scratch>,public` strands it "+
					"there and every other caller fails with SQLSTATE 42704. Pin `SCHEMA public`.",
					m.Name, match[1], why)
			}
			names = append(names, match[1])
		}
	}
	return names
}

// TestMigrationsPlaceExtensionsInPublicFromAnySearchPath applies the migration
// set to an extension-free database the way internal/testdb does and asserts
// every declared extension ends up in `public`, reachable from a scratch
// schema's search_path.
func TestMigrationsPlaceExtensionsInPublicFromAnySearchPath(t *testing.T) {
	dsn := os.Getenv("JOMFORM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping extension placement test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	declared := declaredExtensionsFromMigrations(t)

	name := createExtensionFreeDB(t, ctx, dsn)
	pool, err := Connect(ctx, dsnFor(t, dsn, name))
	if err != nil {
		t.Fatalf("connect to scratch database %s: %v", name, err)
	}
	defer pool.Close()

	// Prove the guard is not vacuous: a cloned-from-template1 database would
	// already hold the extensions in `public`, and then the assertions below
	// would hold however the migrations were written.
	var preexisting []string
	rows, err := pool.Query(ctx, `SELECT extname FROM pg_extension WHERE extname <> 'plpgsql'`)
	if err != nil {
		t.Fatalf("probe scratch database for pre-existing extensions: %v", err)
	}
	for rows.Next() {
		var ext string
		if err := rows.Scan(&ext); err != nil {
			rows.Close()
			t.Fatalf("scan pg_extension: %v", err)
		}
		preexisting = append(preexisting, ext)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate pg_extension: %v", err)
	}
	if len(preexisting) != 0 {
		t.Fatalf("scratch database %s is NOT extension-free (%v) — this guard cannot prove anything "+
			"about extension placement against a database that already has them", name, preexisting)
	}

	// The scratch schema and the search_path below mirror testdb.open()/
	// testdb.migrate(): a per-package schema first, then `public`.
	const scratch = "migtest_scratch"
	scratchIdent := pgx.Identifier{scratch}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE SCHEMA "+scratchIdent); err != nil {
		t.Fatalf("create scratch schema: %v", err)
	}

	stmts, err := MigrationStatements()
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	var b strings.Builder
	b.WriteString("SET search_path=" + scratchIdent + ",public;\n")
	for _, m := range stmts {
		b.WriteString(m.SQL)
		b.WriteString(";\n")
	}
	if _, err := pool.Exec(ctx, b.String()); err != nil {
		t.Fatalf("applying %d migrations under `SET search_path=%s,public` failed: %v", len(stmts), scratch, err)
	}

	// Every declared extension must be in `public`; none may be in the scratch
	// schema. This is the assertion the pre-fix migration fails.
	placed := make(map[string]string)
	rows, err = pool.Query(ctx, `
		SELECT e.extname, n.nspname
		  FROM pg_extension e
		  JOIN pg_namespace n ON n.oid = e.extnamespace`)
	if err != nil {
		t.Fatalf("read extension placement: %v", err)
	}
	for rows.Next() {
		var ext, schema string
		if err := rows.Scan(&ext, &schema); err != nil {
			rows.Close()
			t.Fatalf("scan extension placement: %v", err)
		}
		placed[ext] = schema
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate extension placement: %v", err)
	}
	for _, ext := range declared {
		switch got := placed[ext]; got {
		case "public":
		case "":
			t.Errorf("extension %s is missing after applying the migration set to an empty database", ext)
		default:
			t.Errorf("extension %s landed in schema %q, not `public`.\n"+
				"internal/testdb applies the migrations with `SET search_path=<scratch>,public`, so the "+
				"first package to migrate strands the extension in its own scratch schema and every other "+
				"package fails with `type %q does not exist` (SQLSTATE 42704). The migration must name "+
				"`SCHEMA public`.", ext, got, ext)
		}
	}
	if got := placed["plpgsql"]; got != "pg_catalog" {
		t.Errorf("unexpected extension placement for plpgsql: %q", got)
	}

	// And the lookup every test package actually performs: a scratch schema
	// that did not create these extensions must still resolve them, and a
	// column declared CITEXT (users.email) plus a gen_random_uuid() default
	// (pgcrypto) must both bind through `<scratch>,public`.
	other := pgx.Identifier{"migtest_other"}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE SCHEMA "+other); err != nil {
		t.Fatalf("create second scratch schema: %v", err)
	}
	if _, err := pool.Exec(ctx, "SET search_path="+other+",public;\n"+
		"CREATE TABLE probe_resolution (email CITEXT NOT NULL, id UUID PRIMARY KEY DEFAULT gen_random_uuid())"); err != nil {
		t.Fatalf("a schema whose search_path is `%s,public` cannot resolve the declared extension types: %v\n"+
			"This is the SQLSTATE 42704 the suite fails with when the extensions are not in `public`.", "migtest_other", err)
	}
}
