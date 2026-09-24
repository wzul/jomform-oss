// Package testdb — integration guard for shared-extension provisioning on a
// FRESH database (card t_4c9a9dcc).
//
// The defect this guards: testdb's own migration path (open → migrate) sets
// `search_path = <scratch>,public` and then runs migration 000001, whose
// `CREATE EXTENSION IF NOT EXISTS pgcrypto/citext` therefore installed both
// extensions INSIDE the scratch schema of whichever package migrated first.
// Every other package's search_path resolved the names to that schema, so its
// create raced on pg_extension_name_index and the whole DB-backed suite died
// with "test database unavailable" + SQLSTATE 23505. On a fresh database the
// api gate was red for reasons unrelated to the branch under test.
//
// open() now provisions the extensions into `public` first. These tests use the
// REAL open() against a throwaway database, so removing that provisioning turns
// them red — asserting on db.EnsureSharedExtensions alone would not, because
// that would leave the call site unguarded.

package testdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// extensionPlacement is a small read of pg_extension → pg_namespace for the
// extensions migration 000001 creates. Kept local (not imported from db) so the
// guard can run against a throwaway database without reaching into another
// package's internals.
const extensionPlacementQ = `
	SELECT e.extname, n.nspname
	  FROM pg_extension e
	  JOIN pg_namespace n ON n.oid = e.extnamespace
	 WHERE e.extname IN ('pgcrypto', 'citext')`

// migrationExtensions are the extensions 000001_init.up.sql creates and every
// schema then uses UNQUALIFIED (CITEXT columns, gen_random_uuid() defaults).
var migrationExtensions = []string{"pgcrypto", "citext"}

// scratchDatabaseForThisTest creates a throwaway database and returns its DSN,
// dropping it on cleanup. An empty template means "no TEMPLATE clause"
// (Postgres' default, template1); every guard in this file passes "template0",
// so no guard's starting state can depend on which server it runs against.
//
// The template matters: a template1-derived database inherits whatever the
// server's template1 carries — on a provisioned server both pgcrypto and citext
// in `public` — and an unqualified `CREATE EXTENSION` resolved through a
// `search_path` ending in `public` then fails with SQLSTATE 42710 ("extension
// … already exists") instead of planting the state the guard is about.
func scratchDatabaseForThisTest(t *testing.T, template string) string {
	t.Helper()
	dsn := os.Getenv(DSNEnv)
	if dsn == "" {
		t.Skipf("%s not set; skipping integration test", DSNEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)

	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("generate scratch database name: %v", err)
	}
	name := "jomform_tdbguard_" + hex.EncodeToString(buf)

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
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer dropCancel()
		c, err := pgx.Connect(dropCtx, dsn)
		if err != nil {
			t.Logf("cleanup: could not connect to drop %s: %v", name, err)
			return
		}
		defer func() { _ = c.Close(dropCtx) }()
		quoted := pgx.Identifier{name}.Sanitize()
		if _, err := c.Exec(dropCtx, "DROP DATABASE IF EXISTS "+quoted+" WITH (FORCE)"); err != nil {
			if _, err2 := c.Exec(dropCtx, "DROP DATABASE IF EXISTS "+quoted); err2 != nil {
				t.Logf("cleanup: could not drop %s: %v", name, err2)
			}
		}
	})

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse %s: %v", DSNEnv, err)
	}
	u.Path = "/" + name
	return u.String()
}

// TestOpenOnFreshDatabaseLeavesSharedExtensionsInPublic drives the REAL open()
// against a brand-new database and asserts the extensions landed in `public`
// rather than in the scratch schema.
func TestOpenOnFreshDatabaseLeavesSharedExtensionsInPublic(t *testing.T) {
	// template0: this guard is about what open() PROVISIONS, so its starting
	// state must not depend on whether the server's template1 already carries
	// the extensions (see the helper). A template1 copy on a provisioned server
	// is born with both of them already in `public`, and the placement assertion
	// at the end would then be asserting a state that was true before open() ran.
	dsn := scratchDatabaseForThisTest(t, "template0")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// Fixture precondition: the database must start with NO shared extension
	// installed, or the placement assertion below cannot tell what open()
	// provisioned from what the template handed over — which is the vacuous
	// pass this guard exists to avoid.
	preConn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to scratch database: %v", err)
	}
	installed, err := installedMigrationExtensions(ctx, preConn)
	_ = preConn.Close(ctx)
	if err != nil {
		t.Fatalf("read preset extension placement: %v", err)
	}
	if len(installed) > 0 {
		t.Fatalf("fixture precondition failed: the scratch database already has %v "+
			"installed — it was not created from template0, so the placement assertion "+
			"below would be checking a state open() never produced", installed)
	}

	const schema = "test_freshpkg"
	pool, err := open(ctx, dsn, schema)
	if err != nil {
		t.Fatalf("open() on a fresh database failed: %v\n\n"+
			"This is the reported defect shape: the first package to migrate used to "+
			"claim pgcrypto/citext into its own scratch schema, and every other package "+
			"then failed with pg_extension_name_index (23505) — taking the whole api "+
			"test gate down on a clean database.", err)
	}
	defer pool.Close()

	// Fixture precondition: the migrations must really have run into the scratch
	// schema, or the placement assertion below would be checking a database that
	// was never migrated and prove nothing.
	var tables int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.tables
		 WHERE table_schema = $1 AND table_name IN ('users', 'accounts', 'forms')`,
		schema).Scan(&tables); err != nil {
		t.Fatalf("count migrated tables in %s: %v", schema, err)
	}
	if tables != 3 {
		t.Fatalf("the scratch schema %s holds %d of the 3 expected migrated tables — "+
			"migrations did not run, so the extension placement below proves nothing",
			schema, tables)
	}

	assertExtensionsInPublicForTest(t, ctx, pool, schema)
}

// TestOpenRepairsAnAlreadyStrandedDatabase proves open() self-heals a database
// left in the pre-fix state — the state the shared `jomform_test` database is in
// today (extensions owned by a scratch schema).
func TestOpenRepairsAnAlreadyStrandedDatabase(t *testing.T) {
	// template0: the planted pre-fix state must not depend on whether the
	// server's template1 already carries the extensions (see the helper).
	dsn := scratchDatabaseForThisTest(t, "template0")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// Fixture precondition: the database must start with NO shared extension
	// installed, or the planting below cannot create the pre-fix state (42710)
	// and the repair assertion would be checking nothing.
	preConn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to scratch database: %v", err)
	}
	installed, err := installedMigrationExtensions(ctx, preConn)
	_ = preConn.Close(ctx)
	if err != nil {
		t.Fatalf("read preset extension placement: %v", err)
	}
	if len(installed) > 0 {
		t.Fatalf("fixture precondition failed: the scratch database already has %v "+
			"installed — it was not created from template0, so the pre-fix state cannot "+
			"be planted and this guard would prove nothing", installed)
	}

	// Plant the pre-fix state: both extensions inside a legacy scratch schema.
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to scratch database: %v", err)
	}
	if _, err := conn.Exec(ctx, `CREATE SCHEMA test_legacy`); err != nil {
		_ = conn.Close(ctx)
		t.Fatalf("create legacy scratch schema: %v", err)
	}
	if _, err := conn.Exec(ctx, `SET search_path=test_legacy,public`); err != nil {
		_ = conn.Close(ctx)
		t.Fatalf("set search_path: %v", err)
	}
	for _, ext := range migrationExtensions {
		if _, err := conn.Exec(ctx, `CREATE EXTENSION `+ext); err != nil {
			_ = conn.Close(ctx)
			t.Fatalf("plant %s in the legacy scratch schema: %v", ext, err)
		}
	}
	// The planted state must really be the pre-fix one: every extension inside
	// test_legacy. Asserting it here means a fixture that merely coexists with
	// an extension in `public` fails loudly instead of silently repairing a
	// database that was never stranded.
	stranded, err := extensionStrandedPlacements(ctx, conn)
	_ = conn.Close(ctx)
	if err != nil {
		t.Fatalf("read planted extension placement: %v", err)
	}
	if len(stranded) != len(migrationExtensions) {
		t.Fatalf("fixture precondition failed: expected all %d extensions inside the "+
			"legacy scratch schema, got %v", len(migrationExtensions), stranded)
	}

	pool, err := open(ctx, dsn, "test_freshpkg")
	if err != nil {
		t.Fatalf("open() against a database with stranded extensions failed: %v", err)
	}
	defer pool.Close()

	assertExtensionsInPublicForTest(t, ctx, pool, "test_freshpkg")
}

// installedMigrationExtensions returns the extensions 000001 creates that are
// ALREADY installed in the given connection's database, in "<ext> in <schema>"
// form. Used to assert a fixture's starting state instead of assuming it.
func installedMigrationExtensions(ctx context.Context, conn *pgx.Conn) ([]string, error) {
	rows, err := conn.Query(ctx, extensionPlacementQ)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var installed []string
	for rows.Next() {
		var ext, nsp string
		if err := rows.Scan(&ext, &nsp); err != nil {
			return nil, err
		}
		installed = append(installed, ext+" in "+nsp)
	}
	return installed, rows.Err()
}

// extensionStrandedPlacements returns the extensions 000001 creates that are
// installed somewhere OTHER than public. Empty means none is stranded.
func extensionStrandedPlacements(ctx context.Context, conn *pgx.Conn) ([]string, error) {
	rows, err := conn.Query(ctx, extensionPlacementQ+` AND n.nspname <> 'public'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var stranded []string
	for rows.Next() {
		var ext, nsp string
		if err := rows.Scan(&ext, &nsp); err != nil {
			return nil, err
		}
		stranded = append(stranded, ext+" in "+nsp)
	}
	return stranded, rows.Err()
}

// assertExtensionsInPublicForTest is the shared assertion: each extension
// migration 000001 creates must be installed in `public` and NOT inside the
// package's scratch schema. It also proves the scratch schema can still use them
// through search_path, which is what every migrated table depends on.
func assertExtensionsInPublicForTest(t *testing.T, ctx context.Context, pool *pgxpool.Pool, schema string) {
	t.Helper()

	rows, err := pool.Query(ctx, extensionPlacementQ)
	if err != nil {
		t.Fatalf("read extension placement: %v", err)
	}
	defer rows.Close()
	placement := map[string]string{}
	for rows.Next() {
		var ext, nsp string
		if err := rows.Scan(&ext, &nsp); err != nil {
			t.Fatalf("scan extension placement: %v", err)
		}
		placement[ext] = nsp
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate extension placement: %v", err)
	}

	for _, ext := range migrationExtensions {
		nsp, ok := placement[ext]
		if !ok {
			t.Errorf("extension %s is not installed at all after open()", ext)
			continue
		}
		if nsp != "public" {
			t.Errorf("extension %s lives in schema %q after open(), want %q.\n\n"+
				"Extensions installed inside a scratch schema are dropped with it AND "+
				"make every other package's CREATE EXTENSION IF NOT EXISTS race on "+
				"pg_extension_name_index (23505). open() must provision them into "+
				"public via db.EnsureSharedExtensions before migrating.", ext, nsp, "public")
		}
		if nsp == schema {
			t.Errorf("extension %s was claimed by the scratch schema %s", ext, schema)
		}
	}

	// The property the migrated tables rely on: unqualified CITEXT and
	// gen_random_uuid() must resolve in a schema that does NOT own them.
	if _, err := pool.Exec(ctx, `SET search_path=`+quote(schema)+`,public`); err != nil {
		t.Fatalf("set search_path for the usability probe: %v", err)
	}
	if _, err := pool.Exec(ctx, `CREATE TABLE ext_use_probe (
		email CITEXT, id UUID DEFAULT gen_random_uuid())`); err != nil {
		t.Fatalf("schema %s cannot use CITEXT/gen_random_uuid() through public — "+
			"the extensions are not reachable by search_path: %v", schema, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO ext_use_probe (email) VALUES ('A@B.com')`); err != nil {
		t.Fatalf("insert through CITEXT in %s: %v", schema, err)
	}
}
