// Package testdb — regression tests for the scratch-schema extension trap.
//
// The defect these guard against: migration 000001_init declares
// `CREATE EXTENSION IF NOT EXISTS citext;` (and pgcrypto) without a SCHEMA, so
// the extension is installed into whatever schema the session search_path
// names first. Every test package migrates under `SET search_path=<scratch>,
// public`, so on a database whose `public` has no citext the FIRST package to
// migrate installed the extension into its own scratch schema. The extension
// then existed, so `IF NOT EXISTS` skipped it everywhere else, and every other
// package's `<their scratch>,public` could not resolve `citext`:
//
//	migrate scratch schema test_payments: ERROR: type "citext" does not exist (SQLSTATE 42704)
//
// That is ~430 failures across packages the change never touched, which reads
// as "this diff broke the whole backend". It was reachable on any fresh
// database (and on a database where `public` lacked citext, e.g. one created
// from template0), and it survived runs: `DROP SCHEMA ... CASCADE` only drops
// an extension living inside the schema being dropped, so the package that
// caused it healed its own schema on the next run while every other package
// stayed broken.
//
// These tests create a throwaway database from template0 — deliberately NOT
// the DSN's own database, which has citext in public and would therefore mask
// the bug entirely — and drop it again on cleanup. They skip without
// JOMFORM_TEST_DATABASE_URL, matching the integration-test convention.

package testdb

import (
	"context"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/db"
)

// scratchDBPrefix names the throwaway databases these tests create.
const scratchDBPrefix = "jomform_testdb_regress_"

// createScratchDB creates a uniquely named database from TEMPLATE template0 and
// registers its removal with t.Cleanup, returning its name.
//
// It uses TEMPLATE template0 on purpose. A database created that way inherits
// no extensions at all, which is the precondition of the bug: the default
// template on this host has citext already installed in public, and against
// such a database the broken code passes and the test would prove nothing.
//
// This is the package's ONLY "make me an empty database" helper: the guards in
// extensions_test.go call it too (via createExtensionFreeDB, which names its
// databases after its own owner). Two independent implementations would drift,
// and the template0 detail is exactly what makes these guards non-vacuous.
func createScratchDB(t *testing.T, ctx context.Context, dsn string) string {
	t.Helper()
	name := scratchDBPrefix + randSuffix(t)

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to create scratch database: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	if _, err := conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()+" TEMPLATE template0"); err != nil {
		t.Fatalf("create scratch database %s (needs CREATEDB): %v", name, err)
	}

	t.Cleanup(func() {
		dropCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		c, err := pgx.Connect(dropCtx, dsn)
		if err != nil {
			t.Logf("cleanup: could not connect to drop %s: %v", name, err)
			return
		}
		defer func() { _ = c.Close(dropCtx) }()
		quoted := pgx.Identifier{name}.Sanitize()
		if _, err := c.Exec(dropCtx, "DROP DATABASE IF EXISTS "+quoted+" WITH (FORCE)"); err != nil {
			if _, err2 := c.Exec(dropCtx, "DROP DATABASE IF EXISTS "+quoted); err2 != nil {
				t.Logf("cleanup: could not drop scratch database %s: %v", name, err2)
			}
		}
	})

	return name
}

// dsnFor returns the connection string with its database name replaced.
func dsnFor(t *testing.T, dsn, database string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse %s: %v", DSNEnv, err)
	}
	u.Path = "/" + database
	return u.String()
}

// integrationDSN skips unless the integration DSN is configured, then returns it.
func integrationDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv(DSNEnv)
	if dsn == "" {
		t.Skipf("%s not set; skipping integration test", DSNEnv)
	}
	return dsn
}

// extensionSchema reports the schema an extension is installed in, or "" when
// the extension is not installed at all.
func extensionSchema(t *testing.T, ctx context.Context, pool *pgxpool.Pool, ext string) string {
	t.Helper()
	var schema string
	err := pool.QueryRow(ctx, `
		SELECT n.nspname
		  FROM pg_extension e
		  JOIN pg_namespace n ON n.oid = e.extnamespace
		 WHERE e.extname = $1`, ext).Scan(&schema)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return ""
		}
		t.Fatalf("resolve schema of extension %s: %v", ext, err)
	}
	return schema
}

// TestScratchSchemaExtensionsLiveInPublic is the bite for this card.
//
// It prepares TWO scratch schemas against one throwaway database, exactly as
// two packages of a single `go test ./...` run do, and requires that each one
// can resolve `citext` and that the extensions ended up in `public`.
//
// Pre-fix this fails: the first schema installs citext into itself, the second
// reports `type "citext" does not exist (SQLSTATE 42704)`.
func TestScratchSchemaExtensionsLiveInPublic(t *testing.T) {
	dsn := integrationDSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	name := createScratchDB(t, ctx, dsn)
	dbDSN := dsnFor(t, dsn, name)

	for _, schema := range []string{"pkg_alpha", "pkg_beta", "pkg_gamma"} {
		pool, err := open(ctx, dbDSN, schema)
		if err != nil {
			t.Fatalf("open scratch schema %s: %v\n\n"+
				"This is the scratch-schema extension trap: on a database whose public "+
				"schema has no citext, migration 000001 installs it into the first "+
				"package's scratch schema, and every later package cannot resolve it.",
				schema, err)
		}
		// A pool on this schema must be able to use citext unqualified, which
		// is what the migrations and the store code do.
		table := pgx.Identifier{schema, "citext_probe"}.Sanitize()
		if _, err := pool.Exec(ctx, "CREATE TABLE "+table+" (e citext)"); err != nil {
			t.Errorf("schema %s cannot resolve citext after open: %v", schema, err)
		}
		pool.Close()
	}

	admin, err := db.Connect(ctx, dbDSN)
	if err != nil {
		t.Fatalf("connect to scratch database for assertions: %v", err)
	}
	defer admin.Close()

	for _, ext := range sharedExtensions {
		got := extensionSchema(t, ctx, admin, ext)
		if got == "" {
			t.Errorf("extension %s is not installed after preparation", ext)
			continue
		}
		if got != publicSchema {
			t.Errorf("extension %s is installed in schema %q, want %q — a scratch-schema "+
				"install is unresolvable for every other package's search_path",
				ext, got, publicSchema)
		}
	}

	// No scratch schema may hold an extension.
	var strays []string
	rows, err := admin.Query(ctx, `
		SELECT e.extname || ' in ' || n.nspname
		  FROM pg_extension e
		  JOIN pg_namespace n ON n.oid = e.extnamespace
		 WHERE n.nspname <> $1 AND n.nspname <> 'pg_catalog'`, publicSchema)
	if err != nil {
		t.Fatalf("list extensions outside %s: %v", publicSchema, err)
	}
	defer rows.Close()
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatalf("scan stray extension: %v", err)
		}
		strays = append(strays, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate stray extensions: %v", err)
	}
	if len(strays) > 0 {
		t.Errorf("extensions installed outside %s: %s", publicSchema, strings.Join(strays, ", "))
	}
}

// TestEnsureSchemaRepairsPoisonedDatabase proves the repair path, which is the
// only thing that can fix a database an earlier (pre-fix) run left broken:
// `CREATE EXTENSION ... IF NOT EXISTS ... SCHEMA public` does NOT relocate an
// extension that already exists elsewhere — it degrades to a NOTICE and the
// schema argument is ignored — so `ALTER EXTENSION ... SET SCHEMA` is required.
func TestEnsureSchemaRepairsPoisonedDatabase(t *testing.T) {
	dsn := integrationDSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	name := createScratchDB(t, ctx, dsn)
	dbDSN := dsnFor(t, dsn, name)

	admin, err := db.Connect(ctx, dbDSN)
	if err != nil {
		t.Fatalf("connect to scratch database: %v", err)
	}
	defer admin.Close()

	// Recreate the exact pre-fix shape: install the extensions into a scratch
	// schema via the search_path, as migration 000001 did.
	poisoned := "test_poison"
	stmt := "CREATE SCHEMA " + quote(poisoned) + ";\n" +
		"SET search_path=" + quote(poisoned) + ",public;\n" +
		"CREATE EXTENSION IF NOT EXISTS citext;\n" +
		"CREATE EXTENSION IF NOT EXISTS pgcrypto;\n"
	if _, err := admin.Exec(ctx, stmt); err != nil {
		t.Fatalf("set up the poisoned state: %v", err)
	}
	if got := extensionSchema(t, ctx, admin, "citext"); got != poisoned {
		t.Fatalf("setup failed: citext is in %q, want %q (the test cannot prove the repair otherwise)", got, poisoned)
	}

	// Now the ordinary entry point must both fix and survive this.
	if err := ensureSchema(ctx, admin, "test_repaired"); err != nil {
		t.Fatalf("ensureSchema over a poisoned database: %v", err)
	}
	for _, ext := range sharedExtensions {
		if got := extensionSchema(t, ctx, admin, ext); got != publicSchema {
			t.Errorf("extension %s is in %q after repair, want %q", ext, got, publicSchema)
		}
	}

	// The repaired schema must actually be usable.
	pool, err := open(ctx, dbDSN, "test_repaired_use")
	if err != nil {
		t.Fatalf("open a scratch schema after the repair: %v", err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, "CREATE TABLE citext_after_repair (e citext)"); err != nil {
		t.Errorf("citext still unresolvable after repair: %v", err)
	}
}

// TestPrepareExtensionsConcurrentInstall covers the race the advisory lock
// exists for. `go test ./...` runs packages in parallel against one database,
// and concurrent `CREATE EXTENSION IF NOT EXISTS` calls are NOT safe on their
// own: they can collide on the database-global pg_extension_name_index
// (duplicate key, SQLSTATE 23505). Every participant must succeed.
func TestPrepareExtensionsConcurrentInstall(t *testing.T) {
	dsn := integrationDSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	name := createScratchDB(t, ctx, dsn)
	dbDSN := dsnFor(t, dsn, name)

	const racers = 8
	errs := make([]error, racers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// A pool per racer: sharing one would serialize on a single
			// connection and never exercise the concurrent-install path.
			p, err := db.Connect(ctx, dbDSN)
			if err != nil {
				errs[i] = err
				return
			}
			defer p.Close()
			<-start
			errs[i] = ensureExtensions(ctx, p)
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("concurrent ensureExtensions #%d failed: %v", i, err)
		}
	}

	admin, err := db.Connect(ctx, dbDSN)
	if err != nil {
		t.Fatalf("connect for assertions: %v", err)
	}
	defer admin.Close()

	for _, ext := range sharedExtensions {
		if got := extensionSchema(t, ctx, admin, ext); got != publicSchema {
			t.Errorf("extension %s is in %q after %d concurrent preparations, want %q", ext, got, racers, publicSchema)
		}
	}
}
