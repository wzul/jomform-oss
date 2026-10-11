// Package testdb — live guards for the throwaway-database reclaim
// (scratchdb.go), driven against the real cluster.
//
// The unit guards pin the RULE and the WIRING. These pin the two properties
// only a real server can answer: that a leaked database the family set names is
// actually dropped by the real prune, and that a database something else is
// using is actually left alone. The asymmetry is why both are here — a prune
// that reclaims nothing keeps the 4.5 GB the card measured, and a prune that
// reclaims too much costs a peer lane its run.
//
// Every fixture is planted on the shared test host, because that is where the
// leak happens: the candidates are whole DATABASES, so a leak cannot be staged
// inside a throwaway one the way the scratch-schema guards stage theirs.
package testdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/db"
)

// plantDatabase creates a database named name and registers its removal, so a
// guard's own fixture can never become part of the leak it is testing for.
//
// The cleanup uses WITH (FORCE) where the prune deliberately does not: these
// fixtures are databases this process created and dropped, so terminating a
// leftover connection to one is this process cleaning up after itself, while
// the prune must never terminate a peer's.
func plantDatabase(t *testing.T, ctx context.Context, dsn, name string) {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to the test database to plant %s: %v", name, err)
	}
	defer func() { _ = conn.Close(ctx) }()
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+quote(name)); err != nil {
		t.Fatalf("plant throwaway database %s (needs CREATEDB): %v", name, err)
	}
	t.Cleanup(func() {
		dropCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		c, err := pgx.Connect(dropCtx, dsn)
		if err != nil {
			t.Logf("cleanup: could not connect to drop fixture %s: %v", name, err)
			return
		}
		defer func() { _ = c.Close(dropCtx) }()
		q := quote(name)
		if _, err := c.Exec(dropCtx, `DROP DATABASE IF EXISTS `+q+` WITH (FORCE)`); err != nil {
			if _, err2 := c.Exec(dropCtx, `DROP DATABASE IF EXISTS `+q); err2 != nil {
				t.Logf("cleanup: could not drop fixture %s: %v", name, err2)
			}
		}
	})
}

// fixtureName builds a throwaway-database name in the shape the in-tree
// creators use: <family><random>_p<pid>. A dead pid means "a process that has
// exited" — that is the leak's shape, and the only way to plant one.
func fixtureName(t *testing.T, family string, pid int) string {
	t.Helper()
	return family + randHex(t) + "_p" + strconv.Itoa(pid)
}

// databaseExists reports whether a database is present.
func databaseExists(t *testing.T, ctx context.Context, admin *pgxpool.Pool, name string) bool {
	t.Helper()
	var n int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM pg_database WHERE datname = $1`, name).Scan(&n); err != nil {
		t.Fatalf("probe database %s: %v", name, err)
	}
	return n > 0
}

// databaseBackends counts the backends on one database, excluding this session.
func databaseBackends(t *testing.T, ctx context.Context, admin *pgxpool.Pool, name string) int {
	t.Helper()
	var n int
	if err := admin.QueryRow(ctx,
		`SELECT count(*) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()`, name).Scan(&n); err != nil {
		t.Fatalf("count backends on %s: %v", name, err)
	}
	return n
}

// TestPruneDeadDatabasesReclaimsALeakedDatabase is the reclaim arm on a real
// server: a database whose creating process is gone, with nothing connected to
// it, must be dropped by the real prune. Pre-fix (and with any prune that
// forgets this family) it survives forever, which is the 4.5 GB the card
// measured.
func TestPruneDeadDatabasesReclaimsALeakedDatabase(t *testing.T) {
	ctx := context.Background()
	dsn := dsnOrSkip(t)
	admin, err := db.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("admin connect: %v", err)
	}
	defer admin.Close()

	leaked := fixtureName(t, scratchDBFamilies[0].prefix, deadPID(t))
	plantDatabase(t, ctx, dsn, leaked)
	if !databaseExists(t, ctx, admin, leaked) {
		t.Fatalf("fixture %s was not planted", leaked)
	}
	// The precondition the reclaim turns on: the fixture is not in use. Fail
	// loudly rather than reading a busy database as "the prune kept it".
	if n := databaseBackends(t, ctx, admin, leaked); n != 0 {
		t.Fatalf("fixture %s has %d backend(s) on it before the prune; this guard needs an idle fixture", leaked, n)
	}

	pruneDeadDatabases(ctx, admin)

	if databaseExists(t, ctx, admin, leaked) {
		res, _ := lastDBPrune()
		t.Fatalf("the prune left the abandoned database %s behind.\n\n"+
			"Nothing is connected to it and its creating process is gone, so no arm of the rule could have "+
			"suppressed the reclaim — this is a dead reclaim, and the leak the card measured (273 databases / "+
			"4506 MB) comes straight back.\n\nLast prune: scanned=%d dropped=%v bytes=%d kept=%v\nnote=%s",
			leaked, res.scanned, res.dropped, res.bytes, res.kept, databasePruneNote())
	}
	res, ok := lastDBPrune()
	if !ok {
		t.Fatalf("no prune recorded an outcome — pruneDeadDatabases did not run the wired path")
	}
	found := false
	for _, d := range res.dropped {
		if d == leaked {
			found = true
		}
	}
	if !found {
		t.Errorf("the prune dropped %s but did not record it in dropped=%v — the outcome a guard (or an "+
			"operator) reads would not match what happened", leaked, res.dropped)
	}
}

// TestPruneDeadDatabasesKeepsADatabaseAPeerIsConnectedTo is the other direction
// on the same server, and the one with the expensive failure: a database with a
// live connection on it is never reclaimed, even though nothing about its name
// says it is in use (its owner is gone — that is WHY a guard can connect to it:
// it is exactly what a leaked database looks like while a peer is verifying it,
// or while a fresh run is still migrating).
func TestPruneDeadDatabasesKeepsADatabaseAPeerIsConnectedTo(t *testing.T) {
	ctx := context.Background()
	dsn := dsnOrSkip(t)
	admin, err := db.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("admin connect: %v", err)
	}
	defer admin.Close()

	busy := fixtureName(t, scratchDBFamilies[0].prefix, deadPID(t))
	plantDatabase(t, ctx, dsn, busy)

	// A peer: this guard's own second connection to the fixture, standing in
	// for any run that is mid-test on a database whose creator is gone.
	peerDSN := dsnFor(t, dsn, busy)
	peer, err := pgx.Connect(ctx, peerDSN)
	if err != nil {
		t.Fatalf("connect a peer to %s: %v", busy, err)
	}
	defer func() { _ = peer.Close(ctx) }()
	if _, err := peer.Exec(ctx, `SELECT 1`); err != nil {
		t.Fatalf("peer probe on %s: %v", busy, err)
	}
	if n := databaseBackends(t, ctx, admin, busy); n == 0 {
		t.Fatalf("the peer connection to %s is not visible in pg_stat_activity, so this guard would "+
			"assert nothing", busy)
	}

	pruneDeadDatabases(ctx, admin)

	if !databaseExists(t, ctx, admin, busy) {
		t.Fatalf("the prune dropped %s while a peer was connected to it. `DROP DATABASE` would have "+
			"refused, so the reclaim was forced — that takes a peer lane's database (and its whole run) "+
			"away, which is the one failure this rule must never have. Check that the prune issues a plain "+
			"drop and never WITH (FORCE).", busy)
	}
	res, _ := lastDBPrune()
	reason := ""
	for _, k := range res.kept {
		if len(k) > len(busy)+2 && k[:len(busy)+2] == busy+": " {
			reason = k
		}
	}
	if reason == "" {
		t.Errorf("the prune kept %s but recorded no reason for it; kept=%v", busy, res.kept)
	} else if !strings.Contains(reason, "connected") {
		t.Errorf("the prune kept %s for %q, want the connection to be named as the cause", busy, reason)
	}
}

// TestPruneDeadDatabasesKeepsAFreshUnattributableDatabase is the live guard for
// the grace period — the arm that protects a database whose creator is unknown
// to the name AND which no backend is on yet.
//
// That window is not hypothetical: prune_open_reach_test.go's fixture closes
// its planting connection and then calls open(), which connects again, and a
// pre-pid checkout's database has exactly this shape for its whole life. A
// prune that trusted "no backend is connected" alone would drop it in the
// window and red a peer lane mid-test.
func TestPruneDeadDatabasesKeepsAFreshUnattributableDatabase(t *testing.T) {
	ctx := context.Background()
	dsn := dsnOrSkip(t)
	admin, err := db.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("admin connect: %v", err)
	}
	defer admin.Close()

	// No pid suffix: a name from before the pid convention, i.e. every
	// database the card actually measured.
	fresh := "jomform_migtest_" + randHex(t)
	plantDatabase(t, ctx, dsn, fresh)
	if pid, ok := schemaPID(fresh); ok {
		t.Fatalf("fixture %s parses as pid-qualified (%d) — it must NOT, or this guard tests the "+
			"attribution arm instead of the grace period", fresh, pid)
	}
	if n := databaseBackends(t, ctx, admin, fresh); n != 0 {
		t.Fatalf("fixture %s has %d backend(s) on it; the grace arm only applies to an unattributable "+
			"name with nothing connected", fresh, n)
	}

	pruneDeadDatabases(ctx, admin)

	if !databaseExists(t, ctx, admin, fresh) {
		t.Fatalf("the prune reclaimed %s, created less than a second ago with an unattributable name.\n\n"+
			"Nothing about it says it is abandoned, and this is the exact shape a live run has between "+
			"`CREATE DATABASE` and the first connection of its pool (pgxpool is lazy). Reclaiming it costs "+
			"that run its database — check that the %s grace period still applies to a name with no pid",
			fresh, legacyDatabaseGrace)
	}
}

// TestScratchDatabaseCreationTimesAreReadable pins the input the grace arm
// depends on. There is no catalogue column for a database's creation time, so
// the rule reads the database directory's PG_VERSION through pg_stat_file —
// which needs a role that is superuser or holds pg_monitor/pg_read_server_files.
//
// When that read fails the prune degrades CONSERVATIVELY: an age it cannot read
// is never reclaimed, so it drops nothing it should not — but the arm that
// reclaims the pre-pid databases silently stops working, which is most of the
// leak. This guard fails loudly in that case, and it asserts on the same query
// constant the prune uses, so the two cannot drift.
func TestScratchDatabaseCreationTimesAreReadable(t *testing.T) {
	ctx := context.Background()
	dsn := dsnOrSkip(t)
	admin, err := db.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("admin connect: %v", err)
	}
	defer admin.Close()

	fresh := "jomform_migtest_" + randHex(t)
	before := time.Now().Add(-time.Minute)
	plantDatabase(t, ctx, dsn, fresh)

	ages, err := pgDatabases{admin}.creationTimes(ctx)
	if err != nil {
		t.Fatalf("reading database creation times failed: %v\n\n"+
			"pg_stat_file needs a superuser or pg_monitor/pg_read_server_files role. Without it the prune "+
			"cannot tell a database a run is about to use from an abandoned one, so it reclaims none of the "+
			"pre-pid databases — the leak stays. Grant the privilege or give the prune another age source.", err)
	}
	created, ok := ages[fresh]
	if !ok {
		t.Fatalf("the freshly planted database %s has no creation time in %v — the age read found nothing "+
			"for a database that exists", fresh, ages)
	}
	if created.Before(before) || created.After(time.Now().Add(time.Minute)) {
		t.Fatalf("database %s reports a creation time of %s, want a time within a minute of now (%s) — "+
			"the age source no longer means \"when this database was created\", and the grace arm is being "+
			"decided on something else", fresh, created, before)
	}
}

// TestPruneDeadDatabasesProtectsTheSharedDatabaseFromAThrowawayConnection is
// the guard for the incident this card's own code caused: the prune ran from a
// connection to a THROWAWAY database (open() is called with a fixture's DSN by
// most of the guards in this package), and the only name it excluded was
// `current_database()` — the fixture. The suite's shared database then matched
// its own family prefix (`jomform_test`), was unattributable, was momentarily
// unconnected, and was DROPPED. Every test after that died with
// `database "jomform_test" does not exist (SQLSTATE 3D000)`, and the database
// had to be rebuilt.
//
// So: prune from a fixture's connection and require the shared database to be
// untouched. The fixture is protected twice over (it is the connection's own
// database, and this process is alive), so the shared name is the claim here.
func TestPruneDeadDatabasesProtectsTheSharedDatabaseFromAThrowawayConnection(t *testing.T) {
	ctx := context.Background()
	dsn := dsnOrSkip(t)
	shared := dsnDatabaseName()
	if shared == "" {
		t.Fatalf("%s does not name a database, so this guard could not tell which name must be "+
			"protected — the prune would have nothing to exclude", DSNEnv)
	}

	admin, err := db.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("admin connect: %v", err)
	}
	defer admin.Close()
	if !databaseExists(t, ctx, admin, shared) {
		t.Fatalf("the shared database %s does not exist, so this guard cannot prove it is protected", shared)
	}

	// A throwaway database to prune FROM, exactly as open() does inside the
	// fixture-based guards.
	fixture := fixtureName(t, scratchDBFamilies[0].prefix, os.Getpid())
	plantDatabase(t, ctx, dsn, fixture)
	fixtureAdmin, err := db.Connect(ctx, dsnFor(t, dsn, fixture))
	if err != nil {
		t.Fatalf("connect to the throwaway database %s: %v", fixture, err)
	}
	defer fixtureAdmin.Close()

	pruneDeadDatabases(ctx, fixtureAdmin)

	if !databaseExists(t, ctx, admin, shared) {
		res, _ := lastDBPrune()
		t.Fatalf("pruning from a connection to %s DROPPED the shared database %s.\n\n"+
			"`current_database()` is the FIXTURE there, so excluding only the connection's own database "+
			"leaves the suite's shared database looking like an ordinary leaked candidate: its name matches "+
			"the `jomform_test` family, it cannot be attributed to a process, and at that instant nothing is "+
			"connected to it. Protect the name %s names as well (dsnDatabaseName).\n\n"+
			"protected=%v dropped=%v", fixture, shared, DSNEnv, res.protected, res.dropped)
	}
	res, ok := lastDBPrune()
	if !ok {
		t.Fatalf("no prune recorded an outcome")
	}
	for _, p := range res.protected {
		if p == shared {
			return
		}
	}
	t.Errorf("the prune ran from %s and its protected set was %v, which does not contain the shared "+
		"database %s that %s names — it survived by luck, not by rule", fixture, res.protected, shared, DSNEnv)
}

// TestPruneDeadDatabasesNeverDropsProtectedDatabases drives the real prune on
// the real host and asserts what it must not have touched: the shared test
// database the DSN names, and `postgres`. Both are protected by the RULE (the
// name check), not by luck — and `postgres` is protected by nothing else, since
// it is not a family member and this role is superuser.
func TestPruneDeadDatabasesNeverDropsProtectedDatabases(t *testing.T) {
	ctx := context.Background()
	dsn := dsnOrSkip(t)
	admin, err := db.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("admin connect: %v", err)
	}
	defer admin.Close()

	var shared string
	if err := admin.QueryRow(ctx, `SELECT current_database()`).Scan(&shared); err != nil {
		t.Fatalf("read current_database: %v", err)
	}

	pruneDeadDatabases(ctx, admin)

	for _, name := range []string{shared, "postgres"} {
		if !databaseExists(t, ctx, admin, name) {
			t.Fatalf("the prune dropped %s. The shared test database is the one the whole suite runs in, "+
				"and postgres is the maintenance database every client expects; both are excluded by NAME "+
				"because the `jomform_test` family prefix matches the shared one.", name)
		}
	}
	res, ok := lastDBPrune()
	if !ok {
		t.Fatalf("the prune recorded no outcome — pruneDeadDatabases did not run")
	}
	// A prune that degraded (an unreadable creation time, or the DSN naming no
	// database) turns arms of the rule off silently. On this host every input
	// must be readable, so a note here is a failure, not a diagnostic.
	if note := databasePruneNote(); note != "" {
		t.Errorf("the database prune reported %q — on this host every input the rule needs is readable, "+
			"and a degradation is how the reclaim silently stops working", note)
	}
	for _, d := range res.dropped {
		if d == shared || d == "postgres" {
			t.Errorf("the prune reported dropping %q", d)
		}
	}
}

// randHex returns a short random hex string for a fixture name.
func randHex(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("generate fixture suffix: %v", err)
	}
	return hex.EncodeToString(buf)
}
