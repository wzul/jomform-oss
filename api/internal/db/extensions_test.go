// Package db — guards for the shared-extension provisioning (card t_4c9a9dcc).
//
// Incident this guards against: on a FRESH (never-migrated) database, the first
// test package to migrate ran migration 000001 with
// `search_path = test_<pkg>,public`, so its `CREATE EXTENSION IF NOT EXISTS
// pgcrypto/citext` installed both extensions INSIDE that package's scratch
// schema. Every other package's migration then resolved the names to that
// schema through its own search_path, IF NOT EXISTS saw them as present and did
// nothing — but a concurrent CREATE EXTENSION takes a lock on the extension's
// NAME, so the losers died with
//
//	duplicate key value violates unique constraint "pg_extension_name_index"
//	(SQLSTATE 23505)
//
// and ~440 tests across dbstore/httpapi/mcpsrv/payments failed with "test
// database unavailable". Measured pre-fix on a fresh database: 436 failing
// tests, 92 of them this 23505, extensions stranded in test_chat. The gate's
// green depended on a pre-existing lucky state of the shared database.

package db

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestSharedExtensionsCoverEveryMigrationCreate is the completeness guard for
// db.SharedExtensions: every extension a migration creates must be provisioned
// by EnsureSharedExtensions. An extension missing from that list would again be
// created inside whichever scratch schema ran the migration first, which is the
// 23505 race this card exists to close.
//
// It reads the migrations through db's OWN view of the SQL (createExtensionRE
// and stripSQLComments) rather than a second regex of its own: two parsers
// disagreeing about what a migration declares is how a guard and the code it
// guards drift apart, and the shared one knows about comments, dollar-quoted
// bodies and string literals.
func TestSharedExtensionsCoverEveryMigrationCreate(t *testing.T) {
	migrations, err := MigrationStatements()
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	if len(migrations) == 0 {
		t.Fatal("no embedded migrations found — the go:embed directive is broken")
	}

	provisioned := make(map[string]bool, len(SharedExtensions))
	for _, ext := range SharedExtensions {
		provisioned[ext] = true
	}

	seen := make(map[string]bool)
	for _, m := range migrations {
		for _, match := range createExtensionRE.FindAllStringSubmatch(
			stripSQLComments(m.SQL), -1) {
			ext := match[1]
			seen[ext] = true
			if !provisioned[ext] {
				t.Errorf("migration %s creates extension %q, but it is not in "+
					"db.SharedExtensions (%v).\n\n"+
					"Add it to SharedExtensions: migration-created extensions MUST be "+
					"provisioned into `public` before the per-schema migrations run, or "+
					"the first schema to migrate claims them and every other schema "+
					"fails on pg_extension_name_index (23505). See EnsureSharedExtensions.",
					m.Name, ext, SharedExtensions)
			}
		}
	}

	// The scan must actually see the statement this guard exists for: if the
	// regex stops matching (a reformatted 000001), the check above would pass
	// vacuously on an empty set.
	if !seen["pgcrypto"] || !seen["citext"] {
		t.Fatalf("the scan found %v in the migrations, but 000001_init.up.sql is "+
			"known to create pgcrypto and citext — the guard is matching nothing "+
			"and would pass vacuously", seen)
	}

	// A shared extension no migration declares is dead weight and, worse, would
	// hide a typo in the list.
	for _, ext := range SharedExtensions {
		if !seen[ext] {
			t.Errorf("db.SharedExtensions lists %q but no migration creates it — "+
				"remove it or fix the name", ext)
		}
	}
}

// freshDatabase creates a throwaway database and returns a pool on it plus its
// name. It skips unless JOMFORM_TEST_DATABASE_URL is set, like the rest of the
// db package's integration tests.
//
// It copies template0, i.e. the database starts with no extension installed.
// The scratch database these guards provision or plant state in MUST NOT
// inherit the server's default template: a template1-derived database on a
// provisioned server already holds pgcrypto and citext in `public`, which both
// breaks the fixtures that plant a stranded state (SQLSTATE 42710) and turns
// the placement assertions into vacuous passes. See createScratchDBFromTemplate.
func freshDatabase(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	dsn := requireTestDSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)

	name := createScratchDBFromTemplate(t, ctx, dsn, "template0")
	pool, err := Connect(ctx, dsnFor(t, dsn, name))
	if err != nil {
		t.Fatalf("connect to scratch database %s: %v", name, err)
	}
	t.Cleanup(pool.Close)
	return pool, name
}

// requireTestDSN returns JOMFORM_TEST_DATABASE_URL, skipping the test when it
// is unset (the package's integration-test convention).
func requireTestDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("JOMFORM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping integration test")
	}
	return dsn
}

// TestMigrateLeavesSharedExtensionsInPublic is the direct guard for the defect:
// a migration run whose search_path starts with a NON-public schema (exactly
// what every scratch schema does) must leave pgcrypto and citext installed in
// `public`.
//
// Pre-fix this failed: 000001's `CREATE EXTENSION IF NOT EXISTS` honoured the
// search_path and put both extensions in the scratch schema, which is what
// stranded them there for every other package.
func TestMigrateLeavesSharedExtensionsInPublic(t *testing.T) {
	pool, dbName := freshDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// template0 + this precondition: on a database derived from a provisioned
	// template1 the extensions are already in `public` before Migrate runs, so
	// the placement assertion below could not fail whatever Migrate did — the
	// guard would pass without testing anything.
	assertNoSharedExtensionsInstalled(t, ctx, pool, dbName)

	// Reproduce the scratch-schema search_path: a schema that is not public
	// comes first, so an unqualified CREATE EXTENSION would land inside it.
	if _, err := pool.Exec(ctx, `CREATE SCHEMA test_first_pkg`); err != nil {
		t.Fatalf("create scratch schema: %v", err)
	}
	if _, err := pool.Exec(ctx, `SET search_path=test_first_pkg,public`); err != nil {
		t.Fatalf("set search_path: %v", err)
	}

	if err := Migrate(ctx, pool, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatalf("Migrate on a fresh database failed: %v", err)
	}

	inPublic, misplaced := sharedExtensionPlacementNow(t, ctx, pool)
	for _, ext := range SharedExtensions {
		if !inPublic[ext] {
			t.Errorf("after Migrate, extension %q is not in public — it is in %q.\n\n"+
				"On a fresh database this is the reported defect: the first package to "+
				"migrate claims the extensions into its own scratch schema, and every "+
				"other package then fails with pg_extension_name_index (23505).",
				ext, misplaced[ext])
		}
	}
}

// TestEnsureSharedExtensionsRelocatesAStrandedExtension is the self-healing
// guard: a database left in the pre-fix state (extension inside a scratch
// schema, which is what the shared `jomform_test` database is in today) must be
// repaired by provisioning, not merely tolerated.
//
// The scratch database is copied from template0 on purpose. Copying the
// server's default template instead (the old `CREATE DATABASE <name>`, i.e.
// template1) makes the planted state depend on the server: on a host whose
// template1 already carries pgcrypto and citext in `public`, the unqualified
// `CREATE EXTENSION` below is satisfied through `public` while a copy still
// exists in test_stranded, so the fixture dies with SQLSTATE 42710 before the
// relocation assertion runs — the arm this test exists for was unguarded on
// every provisioned server. template0 makes the starting state explicit.
func TestEnsureSharedExtensionsRelocatesAStrandedExtension(t *testing.T) {
	pool, dbName := freshDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// Fixture precondition: the database must start with NO shared extension
	// installed, or the planting below cannot create the pre-fix state (42710)
	// and the relocation assertion would be checking nothing.
	assertNoSharedExtensionsInstalled(t, ctx, pool, dbName)

	// Plant exactly the pre-fix state: both extensions inside a scratch schema.
	if _, err := pool.Exec(ctx, `CREATE SCHEMA test_stranded`); err != nil {
		t.Fatalf("create scratch schema: %v", err)
	}
	if _, err := pool.Exec(ctx, `SET search_path=test_stranded,public`); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	for _, ext := range SharedExtensions {
		if _, err := pool.Exec(ctx, `CREATE EXTENSION `+ext); err != nil {
			t.Fatalf("plant extension %s in the scratch schema: %v", ext, err)
		}
	}
	inPublic, misplaced := sharedExtensionPlacementNow(t, ctx, pool)
	if !sameExtensionSet(misplaced, SharedExtensions) {
		t.Fatalf("fixture precondition failed: expected all %d extensions stranded in "+
			"test_stranded (%v) and none in public (%v), got %v",
			len(SharedExtensions), SharedExtensions, publicSchema, misplaced)
	}

	if err := EnsureSharedExtensions(ctx, pool, nil); err != nil {
		t.Fatalf("EnsureSharedExtensions: %v", err)
	}

	inPublic, misplaced = sharedExtensionPlacementNow(t, ctx, pool)
	for _, ext := range SharedExtensions {
		if !inPublic[ext] {
			t.Errorf("EnsureSharedExtensions did not relocate %q into public; it is in %q",
				ext, misplaced[ext])
		}
	}

	// A stranded-then-relocated extension must remain usable by a NEW schema
	// through search_path — that is the property every scratch schema depends on.
	if _, err := pool.Exec(ctx, `CREATE SCHEMA test_after_move`); err != nil {
		t.Fatalf("create schema after relocation: %v", err)
	}
	if _, err := pool.Exec(ctx, `SET search_path=test_after_move,public`); err != nil {
		t.Fatalf("set search_path after relocation: %v", err)
	}
	if _, err := pool.Exec(ctx, `CREATE TABLE t (email CITEXT, id UUID DEFAULT gen_random_uuid())`); err != nil {
		t.Fatalf("a schema created after relocation cannot use CITEXT/gen_random_uuid() "+
			"through public — the extensions did not really move: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO t (email) VALUES ('A@B.com')`); err != nil {
		t.Fatalf("insert through the relocated CITEXT type: %v", err)
	}
}

// TestConcurrentEnsureSharedExtensionsIsRaceFree is the guard for the second
// half of the defect: concurrent creators must not race on
// pg_extension_name_index. Each goroutine gets its OWN pool and its own
// transaction, which is the same shape as two test packages (or two checkouts)
// migrating one database at once.
//
// Pre-fix — i.e. with a bare `CREATE EXTENSION IF NOT EXISTS` and no advisory
// lock — 8 concurrent callers on a fresh database produced 23505 on the losers,
// measured on Postgres 16.
func TestConcurrentEnsureSharedExtensionsIsRaceFree(t *testing.T) {
	pool, _ := freshDatabase(t)
	dsn := requireTestDSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	_ = pool

	// A DSN for the same throwaway database, so each goroutine has its own pool
	// and its own backend connection.
	var current string
	if err := pool.QueryRow(ctx, `SELECT current_database()`).Scan(&current); err != nil {
		t.Fatalf("resolve current_database: %v", err)
	}
	ownDSN := dsnFor(t, dsn, current)

	const callers = 8
	pools := make([]*pgxpool.Pool, callers)
	for i := range pools {
		p, err := Connect(ctx, ownDSN)
		if err != nil {
			t.Fatalf("connect caller %d: %v", i, err)
		}
		defer p.Close()
		pools[i] = p
	}

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	start := make(chan struct{})
	for i, p := range pools {
		wg.Add(1)
		go func(i int, p *pgxpool.Pool) {
			defer wg.Done()
			<-start // release all callers together, to widen the race window
			if err := EnsureSharedExtensions(ctx, p, nil); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}(i, p)
	}
	close(start)
	wg.Wait()

	if len(errs) > 0 {
		for _, err := range errs {
			t.Errorf("concurrent EnsureSharedExtensions failed: %v", err)
		}
		t.Fatalf("%d of %d concurrent callers failed.\n\n"+
			"`CREATE EXTENSION IF NOT EXISTS` is NOT race-safe: the IF NOT EXISTS "+
			"check is a snapshot read while the insert is guarded by "+
			"pg_extension_name_index, so two concurrent creators both insert and the "+
			"loser gets 23505. The advisory lock in EnsureSharedExtensions is what "+
			"serializes them; do not remove it.", len(errs), callers)
	}

	inPublic, misplaced := sharedExtensionPlacementNow(t, ctx, pool)
	for _, ext := range SharedExtensions {
		if !inPublic[ext] {
			t.Errorf("after %d concurrent callers, extension %q is not in public (in %q)",
				callers, ext, misplaced[ext])
		}
	}
}

// TestEnsureSharedExtensionsIsIdempotent proves the helper is safe on the
// restart path — Migrate runs it on every API boot.
func TestEnsureSharedExtensionsIsIdempotent(t *testing.T) {
	pool, _ := freshDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	for i := 1; i <= 3; i++ {
		if err := EnsureSharedExtensions(ctx, pool, nil); err != nil {
			t.Fatalf("EnsureSharedExtensions run %d failed: %v", i, err)
		}
	}
	for _, ext := range SharedExtensions {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_extension WHERE extname=$1`, ext).Scan(&n); err != nil {
			t.Fatalf("count extension %s: %v", ext, err)
		}
		if n != 1 {
			t.Errorf("pg_extension has %d rows for %s after 3 provisioning runs, want 1", n, ext)
		}
	}
}

// assertNoSharedExtensionsInstalled fails the test unless the scratch database
// starts with NO shared extension installed. It is the fixture precondition for
// every guard that plants or provisions a state: on a database derived from a
// provisioned template1 the extensions are already in `public` before the test
// runs, which makes relocation a no-op and turns assertions about it into
// vacuous passes (or, where the fixture plants them, into SQLSTATE 42710).
func assertNoSharedExtensionsInstalled(t *testing.T, ctx context.Context, pool *pgxpool.Pool, dbName string) {
	t.Helper()
	inPublic, misplaced := sharedExtensionPlacementNow(t, ctx, pool)
	if len(inPublic) > 0 || len(misplaced) > 0 {
		t.Fatalf("fixture precondition failed: scratch database %s already has shared "+
			"extensions installed (public: %v, elsewhere: %v) — it was not created from "+
			"template0, so the starting state depends on the server's template and this "+
			"guard would not prove what it claims", dbName, inPublic, misplaced)
	}
}

// sameExtensionSet reports whether the misplaced map holds exactly the wanted
// extensions — the fixture precondition for the stranded-database guard. A
// length-only check would pass if one extension were stranded and the other
// were missing, which is not the pre-fix state the guard is about.
func sameExtensionSet(misplaced map[string]string, want []string) bool {
	if len(misplaced) != len(want) {
		return false
	}
	for _, ext := range want {
		if _, ok := misplaced[ext]; !ok {
			return false
		}
	}
	return true
}

// sharedExtensionPlacementNow reads where the shared extensions currently live,
// returning a set of those in public and a map of extname -> actual schema for
// those that are not.
func sharedExtensionPlacementNow(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (map[string]bool, map[string]string) {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT e.extname, n.nspname
		  FROM pg_extension e
		  JOIN pg_namespace n ON n.oid = e.extnamespace
		 WHERE e.extname = ANY($1)`, SharedExtensions)
	if err != nil {
		t.Fatalf("read extension placement: %v", err)
	}
	defer rows.Close()

	inPublic := make(map[string]bool)
	misplaced := make(map[string]string)
	for rows.Next() {
		var ext, schema string
		if err := rows.Scan(&ext, &schema); err != nil {
			t.Fatalf("scan extension placement: %v", err)
		}
		if schema == publicSchema {
			inPublic[ext] = true
			continue
		}
		misplaced[ext] = schema
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate extension placement: %v", err)
	}
	return inPublic, misplaced
}
