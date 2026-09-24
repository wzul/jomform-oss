// Guard for the RECLAIM arm of the scratch-schema prune, through the real
// open() path.
//
// The other live guard (TestPruneDeadSchemasDropsOnlyAbandonedScratchSchemas)
// asserts the YIELD direction: with a peer connected, nothing may be dropped.
// It can only ever observe that direction, because it drives pruneDeadSchemas
// from the test process with a peer pool open — the "peers are running" case by
// construction. The case the reclaim exists for, a database with nothing else on
// it where a killed run's schema must actually go away, was therefore unasserted.
//
// That gap is the one that matters, because the reclaim is reachable only by
// virtue of an ORDERING inside open(): the prune is called before the caller's
// pool is created, and pgxpool is lazy, so at the prune point the admin
// connection is the only backend and otherConnections is 0. Move the prune after
// pgxpool.NewWithConfig/Ping — a natural-looking tidy-up — and the caller's own
// pinged pool becomes a "peer": otherConnections >= 1 forever, solo is false,
// reclaimableScratchSchema returns false for everything, and every abandoned
// schema leaks silently while the yield-direction guard stays green. Nothing
// else in the suite can see that, so this guard drives the real open() and
// asserts the reclaim happened.
//
// A THROWAWAY database is what makes the idle condition deterministic, and it is
// not optional here:
//
//   - on the shared test database, `go test ./...` runs packages in parallel, so
//     "is anyone else connected?" is whatever the host happens to be doing at
//     that moment, and
//   - the shared database is full of pre-existing legacy `test_<pkg>` names that
//     an idle run does sweep away (they are unattributable, so reclaimable when
//     alone) — a pass there proves nothing about the planted fixture and can
//     destroy another lane's in-flight schema.
//
// In the throwaway database the only schemas that exist are the ones planted, so
// "is the abandoned one gone?" has exactly one cause.
package testdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/wzul/jomform/api/internal/db"
)

// reachScratchPrefix names the throwaway databases these guards create. Kept
// short so the generated name stays well inside Postgres' 63-byte limit.
const reachScratchPrefix = "jomform_reachtest_"

// otherBackends counts every connection to the current database except this one
// — the same question open()'s prune asks, spelled out here so the fixture can
// refuse to run when the idle precondition does not hold.
const otherBackends = `SELECT count(*) FROM pg_stat_activity
  WHERE datname = current_database() AND pid <> pg_backend_pid()`

// TestOpenReclaimsAnAbandonedSchemaOnAnIdleDatabase is the positive arm of the
// prune wiring guard: on a database with nobody else connected, a real open()
// must drop a leftover scratch schema whose owner is gone.
//
// Both reclaimable shapes are planted, because they are reached by different
// predicates: a pid-qualified name whose owner has exited (abandoned), and a
// pre-pid name that cannot be attributed to any process at all (legacy). This
// process's own schema must survive both — ensureSchema has just created it, and
// a prune that dropped it would fail the very next query the suite runs.
func TestOpenReclaimsAnAbandonedSchemaOnAnIdleDatabase(t *testing.T) {
	ctx := context.Background()
	dbname, dbDSN := createIdleTestDB(t, ctx, dsnOrSkip(t))

	own := "test_reach_own_p" + strconv.Itoa(os.Getpid())
	abandoned := "test_reach_abandoned_p" + strconv.Itoa(deadPID(t))
	legacy := "test_reach_legacy"

	// Plant from a connection that is then CLOSED: a connection left open is a
	// peer, which would suppress the reclaim and make this test vacuous in the
	// direction it exists to check.
	plant, err := db.Connect(ctx, dbDSN)
	if err != nil {
		t.Fatalf("connect to %s to plant the fixtures: %v", dbname, err)
	}
	for _, n := range []string{abandoned, legacy} {
		if _, err := plant.Exec(ctx, `CREATE SCHEMA `+quote(n)); err != nil {
			t.Fatalf("plant %s: %v", n, err)
		}
	}
	var others int
	if err := plant.QueryRow(ctx, otherBackends).Scan(&others); err != nil {
		t.Fatalf("count backends on %s: %v", dbname, err)
	}
	plant.Close()
	if others != 0 {
		t.Fatalf("throwaway database %s already has %d other backends — the idle precondition does not hold, so this test cannot distinguish a working reclaim from a suppressed one", dbname, others)
	}

	// The real path under test.
	p, err := open(ctx, dbDSN, own)
	if err != nil {
		t.Fatalf("open on the throwaway database %s: %v", dbname, err)
	}
	defer p.Close()

	// open() must actually have landed on the throwaway database. Without this
	// the whole guard passes vacuously if the DSN is ever mis-built (issue #532:
	// a mutated pgx config silently kept connecting to the DSN's own database,
	// so a fresh-database guard inspected an already-migrated one and proved
	// nothing).
	var landed string
	if err := p.QueryRow(ctx, `SELECT current_database()`).Scan(&landed); err != nil {
		t.Fatalf("read current_database: %v", err)
	}
	if landed != dbname {
		t.Fatalf("open() landed on database %q, want the throwaway %q — this guard would otherwise inspect the wrong database", landed, dbname)
	}

	exists := func(name string) bool {
		t.Helper()
		var n int
		if err := p.QueryRow(ctx, `SELECT count(*) FROM pg_namespace WHERE nspname = $1`, name).Scan(&n); err != nil {
			t.Fatalf("probe %s: %v", name, err)
		}
		return n > 0
	}

	if exists(abandoned) {
		t.Errorf("open() left %s behind on an idle database — its owner (%s) is gone, so nothing will ever reclaim it: a killed run now leaks a schema per run. Check that open() still prunes BEFORE it creates the caller's pool (pgxpool is lazy, so pruning after pgxpool.NewWithConfig/Ping counts the caller's own backend as a peer and suppresses the reclaim forever).",
			abandoned, abandoned[len("test_reach_abandoned_"):])
	}
	if exists(legacy) {
		t.Errorf("open() left the pre-pid schema %s behind on an idle database — a name with no pid can never be attributed to a process, so it is reclaimable exactly when nobody else is connected, which is this case", legacy)
	}
	if !exists(own) {
		t.Errorf("open() dropped this process's own schema %s — the prune must never reclaim the schema it is about to migrate and run against", own)
	}
}

// TestOpenLeavesAnAbandonedSchemaAloneWhileAPeerIsConnected is the other arm
// through the same real path: with a peer backend alive, the reclaim must yield
// and issue NO DDL at all.
//
// `DROP SCHEMA ... CASCADE` is database-wide DDL — it invalidates catalogue
// entries a peer may be mid-lookup on, which surfaces in the peer's own
// untouched package as `could not open relation with OID (SQLSTATE XX000)`.
// The abandoned-looking schema must therefore survive, however dead its owner
// is: a leaked empty schema costs nothing, a peer's gate run costs a re-run and
// a false regression report.
//
// Asserted through open() rather than by calling pruneDeadSchemas directly,
// because the suppression has to hold for the path that actually runs — a prune
// reachable here would mean the ordering fix went too far and the XX000 hazard
// is back.
func TestOpenLeavesAnAbandonedSchemaAloneWhileAPeerIsConnected(t *testing.T) {
	ctx := context.Background()
	dbname, dbDSN := createIdleTestDB(t, ctx, dsnOrSkip(t))

	own := "test_reach_own2_p" + strconv.Itoa(os.Getpid())
	abandoned := "test_reach_peerabandoned_p" + strconv.Itoa(deadPID(t))

	// The peer is this connection, held open (and made real with a query —
	// pgxpool is lazy, so an untouched pool holds no backend at all) across the
	// whole of open().
	peer, err := db.Connect(ctx, dbDSN)
	if err != nil {
		t.Fatalf("connect a peer to %s: %v", dbname, err)
	}
	defer peer.Close()
	if _, err := peer.Exec(ctx, `CREATE SCHEMA `+quote(abandoned)); err != nil {
		t.Fatalf("plant %s: %v", abandoned, err)
	}

	p, err := open(ctx, dbDSN, own)
	if err != nil {
		t.Fatalf("open on the throwaway database %s: %v", dbname, err)
	}
	defer p.Close()

	var n int
	if err := p.QueryRow(ctx, `SELECT count(*) FROM pg_namespace WHERE nspname = $1`, abandoned).Scan(&n); err != nil {
		t.Fatalf("probe %s: %v", abandoned, err)
	}
	if n == 0 {
		t.Errorf("open() dropped %s while a peer backend was connected — cleanup must issue NO DDL with a peer alive (DROP SCHEMA is database-wide and the mechanism behind a peer's XX000), however abandoned the schema looks", abandoned)
	}
	if err := p.QueryRow(ctx, `SELECT count(*) FROM pg_namespace WHERE nspname = $1`, own).Scan(&n); err != nil {
		t.Fatalf("probe %s: %v", own, err)
	}
	if n == 0 {
		t.Errorf("open() did not create %s — ensureSchema must run regardless of whether the reclaim yielded", own)
	}
}

// createIdleTestDB creates a uniquely named, empty database and registers its
// removal, returning the name and a DSN pointing at it. It connects through the
// DSN's own database, so no separate maintenance database needs to exist.
func createIdleTestDB(t *testing.T, ctx context.Context, dsn string) (string, string) {
	t.Helper()
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("generate throwaway database name: %v", err)
	}
	name := reachScratchPrefix + hex.EncodeToString(buf)

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to the test database to create %s: %v", name, err)
	}
	defer func() { _ = conn.Close(ctx) }()
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("create throwaway database %s (needs CREATEDB): %v", name, err)
	}
	t.Cleanup(func() {
		dropCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		c, err := pgx.Connect(dropCtx, dsn)
		if err != nil {
			t.Logf("cleanup: could not connect to drop throwaway database %s: %v", name, err)
			return
		}
		defer func() { _ = c.Close(dropCtx) }()
		quoted := pgx.Identifier{name}.Sanitize()
		if _, err := c.Exec(dropCtx, "DROP DATABASE IF EXISTS "+quoted+" WITH (FORCE)"); err != nil {
			// WITH (FORCE) needs Postgres 13+; fall back to a plain drop.
			if _, err2 := c.Exec(dropCtx, "DROP DATABASE IF EXISTS "+quoted); err2 != nil {
				t.Logf("cleanup: could not drop throwaway database %s: %v", name, err2)
			}
		}
	})

	// Build the DSN with url.URL, NOT pgx.ParseConfig + a mutated cfg.Database:
	// pgx's ConnString() returns the ORIGINAL string and discards the mutation,
	// so the caller silently keeps connecting to the DSN's own database (issue
	// #532). The current_database assertion in the test above is the backstop.
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse %s: %v", DSNEnv, err)
	}
	u.Path = "/" + name
	return name, u.String()
}
