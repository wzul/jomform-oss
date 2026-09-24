// Package testdb — shared-extension placement guard.
//
// Incident this guards against: the migration set declared its shared
// extensions with an unqualified `CREATE EXTENSION IF NOT EXISTS citext`, so
// whichever test package migrated FIRST on a database that had none created
// citext inside its own scratch schema instead of `public`. Every other package
// resolves unqualified names through `<its schema>,public` and failed with
// `type "citext" does not exist` (SQLSTATE 42704); the package that owned the
// stranded copy then failed differently (SQLSTATE 23505, pg_extension name
// collision) once its scratch schema was re-created. The suite was only green
// on a database where a human had pre-created the extensions in `public`.
//
// This package now repairs that placement before every scratch-schema reset, so
// the suite must be green whatever the operator's database looks like. These
// tests assert the repair on a schema this test actually breaks and on a
// database whose extension is genuinely stranded — not on the happy path that
// was already working.
//
// The helpers these tests share with the rest of the package live in
// scratch_extension_test.go (dsnFor, extensionSchema, createScratchDB) and
// prepare_extensions_ownership_test.go (randSuffix, schemaOfExtension): this
// file deliberately declares none of them twice, so the package has ONE
// definition of each.

package testdb

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/db"
)

// createExtensionFreeDB creates a uniquely named database from TEMPLATE
// template0 and registers its removal with t.Cleanup, returning its name.
//
// It delegates to createScratchDB (scratch_extension_test.go) so the package has
// ONE implementation of the template0 detail that makes these guards
// non-vacuous; only the name prefix differs, so a leaked database is
// attributable to the guard that owns it.
func createExtensionFreeDB(t *testing.T, ctx context.Context, dsn string) string {
	t.Helper()
	return createScratchDB(t, ctx, dsn)
}

// adminPool opens a pool on the DSN's own database. Unlike Open's pool it does
// NOT narrow search_path to a scratch schema: these tests manage extension
// placement, which is database-global.
func adminPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv(DSNEnv)
	if dsn == "" {
		t.Skipf("%s not set; skipping integration test", DSNEnv)
	}
	p, err := db.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

// TestEnsureExtensionsRepairsAStrandedExtension is the direct regression guard:
// on a throwaway database it strands citext in a scratch schema exactly the way
// the unqualified migration did, then asserts ensureExtensions moves it back to
// `public` and that a second schema can resolve the type again.
//
// The staging has to happen on an extension-free database. A shared test
// database already holds citext in `public`, and `CREATE EXTENSION IF NOT
// EXISTS citext SCHEMA <scratch>` is a silent no-op there (no second copy is
// possible — pg_extension's name index is cluster-wide), so the defect cannot
// be staged on it at all. An earlier version of this test tried, and skipped
// itself on every run: a guard that never runs is worth nothing, so it now
// creates its own database from TEMPLATE template0 and fails loudly if the
// defect cannot be staged.
//
// Without the ALTER in ensureExtensions this fails with citext still in the
// scratch schema — the state that produced SQLSTATE 42704 for every other
// package.
func TestEnsureExtensionsRepairsAStrandedExtension(t *testing.T) {
	ctx := context.Background()
	dsn := os.Getenv(DSNEnv)
	if dsn == "" {
		t.Skipf("%s not set; skipping integration test", DSNEnv)
	}

	const scratchPrefix = "strand_guard"

	for _, ext := range sharedExtensions {
		// A fresh database per extension: ensureExtensions repairs EVERY shared
		// extension at once, so after the first iteration the rest are already
		// in `public` and cannot be stranded again on the same database.
		name := createExtensionFreeDB(t, ctx, dsn)
		p, err := db.Connect(ctx, dsnFor(t, dsn, name))
		if err != nil {
			t.Fatalf("connect to scratch database %s: %v", name, err)
		}

		scratch := scratchPrefix + "_" + ext
		ident := pgx.Identifier{scratch}.Sanitize()
		if _, err := p.Exec(ctx, "CREATE SCHEMA "+ident); err != nil {
			p.Close()
			t.Fatalf("create scratch schema %s: %v", scratch, err)
		}
		t.Cleanup(func() {
			_, _ = p.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+ident+" CASCADE")
			_, _ = p.Exec(context.Background(), "DROP TABLE IF EXISTS should_not_resolve_"+ext)
			p.Close()
		})

		// Strand it exactly as the unqualified migration did: the extension
		// does not exist yet, so CREATE EXTENSION without a SCHEMA clause puts
		// it in the first schema of the search_path.
		if _, err := p.Exec(ctx, "SET search_path="+ident+",public; CREATE EXTENSION "+ext); err != nil {
			t.Fatalf("strand extension %s in %s: %v", ext, scratch, err)
		}

		// Prove the defect is actually staged before asserting the repair. A
		// failure here means the test is not testing what it claims.
		if got := extensionSchema(t, ctx, p, ext); got != scratch {
			t.Fatalf("could not stage the defect: %s is in %q, not the scratch schema %q.\n"+
				"This guard must reproduce the stranded state, not skip past it.", ext, got, scratch)
		}

		// And prove the stranded state is the failure users saw: a schema that
		// did not create the extension cannot resolve its types.
		strayTable := "should_not_resolve_" + ext
		if _, err := p.Exec(ctx, "SET search_path=public; CREATE TABLE "+strayTable+" (e CITEXT)"); err == nil {
			t.Fatalf("staging is inconsistent: citext resolved while %s is stranded in %q", ext, scratch)
		}

		if err := ensureExtensions(ctx, p); err != nil {
			t.Fatalf("ensureExtensions must repair the stranded extension, got: %v", err)
		}
		if got := extensionSchema(t, ctx, p, ext); got != "public" {
			t.Errorf("extension %s is in %q after ensureExtensions, want `public`.\n"+
				"A stranded extension is invisible to every other package's scratch schema, which is "+
				"the SQLSTATE 42704 the full suite fails with.", ext, got)
		}

		// The point of the repair: a scratch schema can resolve the type again.
		if _, err := p.Exec(ctx, "SET search_path="+ident+",public;"+
			" CREATE TABLE resolves_now (email CITEXT NOT NULL)"); err != nil {
			t.Errorf("a scratch schema cannot resolve CITEXT after ensureExtensions repaired %s: %v", ext, err)
		}
	}
}

// TestEnsureExtensionsLeavesAWorkingDatabaseAlone is the other half: on the
// normal path (extensions already in `public`) the repair must be a no-op that
// returns cleanly, so it cannot break the environment it is meant to protect.
func TestEnsureExtensionsLeavesAWorkingDatabaseAlone(t *testing.T) {
	ctx := context.Background()
	p := adminPool(t)

	if err := ensureExtensions(ctx, p); err != nil {
		t.Fatalf("ensureExtensions on an already-correct database: %v", err)
	}
	for _, ext := range sharedExtensions {
		if got := extensionSchema(t, ctx, p, ext); got != "public" {
			t.Errorf("extension %s is in %q after ensureExtensions, want `public`", ext, got)
		}
	}
	// Running it twice must stay clean (the production boot path re-runs
	// migrations on every start; this repair runs on every test binary).
	if err := ensureExtensions(ctx, p); err != nil {
		t.Fatalf("second ensureExtensions run: %v", err)
	}
}

// TestSharedExtensionsAreDerivedAndNonEmpty pins the repair's input against a
// silent, empty result.
//
// sharedExtensions is db.SharedExtensions, which is derived from the migration
// set, so it cannot drift out of step with it. What it CAN do is come back
// empty — if the parser stops matching (or the embed directive breaks),
// ensureExtensions would repair nothing and this package would report green
// while the suite it protects fails with SQLSTATE 42704. The names below are
// literals, independent of the parser, so a parser that breaks fails here.
func TestSharedExtensionsAreDerivedAndNonEmpty(t *testing.T) {
	have := make(map[string]bool, len(sharedExtensions))
	for _, ext := range sharedExtensions {
		have[ext] = true
	}
	for _, required := range []string{"citext", "pgcrypto"} {
		if !have[required] {
			t.Errorf("sharedExtensions is missing %q (got %v).\n"+
				"The migration set declares it, so ensureExtensions would leave it wherever it is and "+
				"every scratch schema would fail with SQLSTATE 42704. Either a migration dropped it or "+
				"the parser in db.DeclaredExtensions stopped matching.", required, sharedExtensions)
		}
	}
}
