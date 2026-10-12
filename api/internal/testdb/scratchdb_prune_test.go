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
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
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

// keptReason returns the reason the last prune recorded for keeping name (""
// when it recorded none). The kept list is the prune's audit trail, one
// `"<name>: <reason>"` entry per candidate it refused, so a guard can assert
// WHICH arm held a database instead of guessing from the fact that it survived.
func keptReason(res dbPruneResult, name string) string {
	for _, k := range res.kept {
		if strings.HasPrefix(k, name+": ") {
			return strings.TrimPrefix(k, name+": ")
		}
	}
	return ""
}

// dropRecorded reports whether the last prune recorded dropping name. A drop
// that happened but was not recorded is a different failure from a drop that
// never happened, and the guards below need the two apart.
func dropRecorded(res dbPruneResult, name string) bool {
	for _, d := range res.dropped {
		if d == name {
			return true
		}
	}
	return false
}

// leakOwner is a process this test starts and holds open, so a fixture's name
// can carry a pid that is ALIVE while the test sets the fixture up and DEAD the
// moment the test wants the fixture to be a leak's shape.
//
// Why a fixture needs one (card t_9a1cbc7e): a database planted in the leak's
// shape — a family name, a DEAD pid, nothing connected — is reclaimable by ANY
// pruner on the host, because the rule reads nothing about which process is
// asking (see reclaimableScratchDatabase). This host runs more than one gate at
// a time and each one's `internal/testdb` prunes, so a fixture planted with a
// dead pid can be taken by a PEER lane's prune in the window between the plant
// and the assertion that reads it. Measured: a planted fixture gone by the time
// the test dropped it (`database "<fixture>" does not exist, SQLSTATE 3D000`)
// with a peer's full `go test ./...` running.
//
// A pid-qualified name whose owner still exists is never reclaimed — not by
// this process's prune, not by a peer's — so holding the owner alive is what
// makes the plant→set-up window peer-proof rather than merely short. The owner
// is ended by release(), at the one moment the test wants the leak's shape; from
// there on the fixture is reclaimable by anyone and nothing can be done about it
// (the rule has no input that tells two pruners apart), which is why the reclaim
// guard treats "a peer took it" as an attempt with no verdict rather than as a
// failure.
type leakOwner struct {
	cmd      *exec.Cmd
	released bool
}

// startLeakOwner starts the process whose pid a fixture's name will carry. The
// test decides when it ends (release); the cleanup kills it if the test returns
// without doing so.
func startLeakOwner(t *testing.T) *leakOwner {
	t.Helper()
	cmd := exec.Command("sleep", "600")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot spawn the process a fixture's name must carry to be peer-proof: %v", err)
	}
	o := &leakOwner{cmd: cmd}
	t.Cleanup(o.kill)
	return o
}

// pid is the pid a fixture's name should carry while the owner is alive.
func (o *leakOwner) pid() int { return o.cmd.Process.Pid }

// kill ends the owner process and reaps it. The reaping is the point: `kill(pid,
// 0)` still SUCCEEDS for a zombie, so a killed-but-unreaped owner would leave the
// fixture looking owned and protected — a green guard that proves nothing.
func (o *leakOwner) kill() {
	_ = o.cmd.Process.Kill()
	_, _ = o.cmd.Process.Wait()
}

// release ends the owner and asserts the pid really is gone, which is what turns
// the fixture into the leak's shape.
func (o *leakOwner) release(t *testing.T) {
	t.Helper()
	if o.released {
		t.Fatalf("the fixture's owner (pid %d) was released twice — the second release would assert "+
			"liveness of a pid that is already gone and say nothing about the fixture", o.pid())
	}
	o.kill()
	o.released = true
	if ownerAliveFromErr(syscall.Kill(o.pid(), 0)) {
		t.Fatalf("the fixture's owner (pid %d) still probes as a live process after this test ended it, "+
			"so the fixture is not in the leak's shape and the reclaim below would assert the wrong thing "+
			"(`kill(pid, 0)` reports a killed-but-unreaped process as live, so ending the owner means "+
			"killing AND reaping it)", o.pid())
	}
}

// leakReclaimAttempts bounds how many fixtures the reclaim guard plants.
//
// The verdict itself is not probabilistic: an attempt is a verdict (this
// process's prune reclaimed the fixture AND recorded it), a dead reclaim (the
// fixture survived — the failure that guard exists for), or no verdict at all (a
// peer's prune took the fixture in the hand-off window, which no input can close
// — see leakOwner). Repeating only reduces how often a run ends without a
// verdict; both real failures are raised per attempt, never aggregated away.
const leakReclaimAttempts = 5

// reclaimOneLeak plants one peer-proof fixture, asserts the premise that makes it
// peer-proof, ends its owner, and requires the REAL prune to reclaim the leak's
// shape. It reports whether THIS process's prune reclaimed it and recorded that
// (the verdict); false means a peer's prune took the fixture first, which is
// logged here and retried by the caller.
//
// Every other outcome is a reclaim failure and is raised here:
//
//   - the prune reclaims the fixture while its owner is still alive — the
//     direction that costs a peer lane the database its live run is migrating;
//   - the prune leaves the abandoned fixture behind — the dead reclaim that
//     brings the measured 4.5 GB leak back;
//   - the prune neither decided on the fixture nor recorded a reason for it —
//     its own inputs degraded, which is how the reclaim silently stops working.
func reclaimOneLeak(t *testing.T, ctx context.Context, dsn string, admin *pgxpool.Pool, attempt int) bool {
	t.Helper()

	owner := startLeakOwner(t)
	leaked := fixtureName(t, scratchDBFamilies[0].prefix, owner.pid())
	plantDatabase(t, ctx, dsn, leaked)
	if !databaseExists(t, ctx, admin, leaked) {
		t.Fatalf("attempt %d: fixture %s is not present immediately after this test created it. Its name carries "+
			"a pid that is still alive (pid %d), so no pruner on this host may reclaim it — either the pid arm "+
			"of the rule is not holding, or the name's pid is not the live process this guard started (a fixture "+
			"in the leak's shape is reclaimable by every pruner on the host), or the plant did not do what its "+
			"own error check says it did.", attempt, leaked, owner.pid())
	}
	// The precondition the reclaim turns on: the fixture is not in use. Fail
	// loudly rather than reading a busy database as "the prune kept it".
	if n := databaseBackends(t, ctx, admin, leaked); n != 0 {
		t.Fatalf("attempt %d: fixture %s has %d backend(s) on it before the prune; this guard needs an idle fixture",
			attempt, leaked, n)
	}

	// The premise of the whole fixture construction, asserted rather than
	// assumed: while the owner is alive the real prune must KEEP the fixture and
	// name the pid arm as the cause. That is what makes the set-up window
	// peer-proof, and it is asserted because the alternative — planting in the
	// leak's shape straight away — is what a peer's prune reclaimed out from
	// under this suite.
	pruneDeadDatabases(ctx, admin)
	res, ok := lastDBPrune()
	if !ok {
		t.Fatalf("attempt %d: no prune recorded an outcome — pruneDeadDatabases did not run the wired path", attempt)
	}
	if !databaseExists(t, ctx, admin, leaked) {
		t.Fatalf("attempt %d: the prune reclaimed %s while its creating process (pid %d) is STILL ALIVE.\n\n"+
			"That is the one failure this rule must never have: a peer lane's live run would lose the database it "+
			"is migrating, mid-run.\n\nLast prune: scanned=%d dropped=%v kept=%v\nnote=%s",
			attempt, leaked, owner.pid(), res.scanned, res.dropped, res.kept, databasePruneNote())
	}
	if reason := keptReason(res, leaked); reason == "" {
		t.Fatalf("attempt %d: the prune recorded no reason for keeping %s while its owner (pid %d) is alive — the "+
			"fixture survived without the rule having decided on it, so it is protected by luck rather than by the "+
			"pid arm this guard's whole construction rests on.\n\nLast prune: scanned=%d dropped=%v kept=%v\nnote=%s",
			attempt, leaked, owner.pid(), res.scanned, res.dropped, res.kept, databasePruneNote())
	} else if want := strconv.Itoa(owner.pid()); !strings.Contains(reason, "still exists") || !strings.Contains(reason, want) {
		t.Fatalf("attempt %d: the prune kept %s for %q, want the pid arm — `its creating process (pid %d) still "+
			"exists`. That arm is what makes this fixture peer-proof while the test sets it up; any other arm "+
			"(a family, a grace period) leaves it reclaimable by a peer's prune and this guard back where the "+
			"card found it.", attempt, leaked, reason, owner.pid())
	}

	// The hand-off: the owner ends here, and the fixture is from this instant the
	// leak's shape — a family name whose creating process is gone, with nothing
	// connected. Nothing about the rule can then tell this process's pruner from
	// a peer's, so a peer taking it is a possible outcome of the prune below and
	// not a fault of the tree under test.
	owner.release(t)

	pruneDeadDatabases(ctx, admin)
	res, ok = lastDBPrune()
	if !ok {
		t.Fatalf("attempt %d: no prune recorded an outcome for the reclaim of the abandoned fixture", attempt)
	}
	if databaseExists(t, ctx, admin, leaked) {
		t.Fatalf("attempt %d: the prune left the abandoned database %s behind.\n\n"+
			"Nothing is connected to it and its creating process is gone (pid %d, ended and reaped by this test), "+
			"so no arm of the rule could have suppressed the reclaim — this is a dead reclaim, and the leak the "+
			"card measured (273 databases / 4506 MB) comes straight back.\n\n"+
			"Last prune: scanned=%d dropped=%v bytes=%d kept=%v\nnote=%s",
			attempt, leaked, owner.pid(), res.scanned, res.dropped, res.bytes, res.kept, databasePruneNote())
	}
	if dropRecorded(res, leaked) {
		return true
	}
	// Gone, and not by this prune: the fixture was reclaimed between the owner's
	// death and this prune's candidate read, and the only other pruner on the
	// host is a PEER lane's. No verdict from this attempt.
	t.Logf("attempt %d: a peer's prune reclaimed %s before this process's prune read the candidate list — the "+
		"hand-off window no input of the rule can close (see leakOwner); planting a fresh fixture", attempt, leaked)
	return false
}

// TestPruneDeadDatabasesReclaimsALeakedDatabase is the reclaim arm on a real
// server: a database whose creating process is gone, with nothing connected to
// it, must be dropped by the real prune. Pre-fix (and with any prune that
// forgets this family) it survives forever, which is the 4.5 GB the card
// measured.
//
// The fixture has to BE the leak — a family name, a DEAD pid, nothing connected
// — and that shape is reclaimable by every pruner on the host, by definition:
// the rule reads the name, the backends and the age, never which process is
// asking. This host runs more than one gate at a time and each one's
// `internal/testdb` prunes, so a fixture planted in that shape can be taken by a
// PEER lane's prune before this test reads it (card t_9a1cbc7e, measured: a
// planted fixture gone by the time the test reached it, `database "…" does not
// exist (SQLSTATE 3D000)`). That is a red on a correct tree.
//
// So the fixture is NOT planted in that shape. It is planted with a LIVE pid —
// a process this test starts and holds (leakOwner) — which the rule's own pid
// arm protects from every pruner on the host, and the owner is ended only at the
// moment the test wants the leak's shape. The premise is asserted, not assumed:
// each attempt first requires the real prune to KEEP the live-owned fixture and
// name the pid arm (reclaimOneLeak).
//
// Once the owner is ended the fixture is reclaimable by anyone and no input can
// tell this process's pruner from a peer's, so an attempt ends three ways:
// this process's prune reclaimed it and recorded it (the verdict, returned on),
// the fixture survived (a dead reclaim — raised immediately), or a peer's prune
// took it in the hand-off (no verdict — retried with a fresh fixture). A run in
// which every attempt was taken by a peer SKIPS with the mechanism named: it
// reports no verdict rather than a wrong one, and it can never red a tree for
// the host it ran on.
func TestPruneDeadDatabasesReclaimsALeakedDatabase(t *testing.T) {
	ctx := context.Background()
	dsn := dsnOrSkip(t)
	admin, err := db.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("admin connect: %v", err)
	}
	defer admin.Close()

	for attempt := 1; attempt <= leakReclaimAttempts; attempt++ {
		if reclaimOneLeak(t, ctx, dsn, admin, attempt) {
			return
		}
	}
	t.Skipf("every one of the %d fixtures this guard planted was reclaimed by a PEER lane's prune before this "+
		"process's own prune could be attributed with it, so this run cannot say whether the wired prune reclaims "+
		"a leaked database.\n\n"+
		"This is a contention report, not a verdict: the same rule took each fixture (its inputs cannot tell one "+
		"pruner from another — see leakOwner), and a DEAD reclaim would have failed every attempt instead. Re-run "+
		"on a quieter host, or widen leakReclaimAttempts, to get the live verdict.", leakReclaimAttempts)
}

// TestPruneDeadDatabasesKeepsADatabaseAPeerIsConnectedTo is the other direction
// on the same server, and the one with the expensive failure: a database with a
// live connection on it is never reclaimed, even though nothing about its name
// says it is in use (its owner is gone — that is WHY a guard can connect to it:
// it is exactly what a leaked database looks like while a peer is verifying it,
// or while a fresh run is still migrating).
//
// The order is the fix (card t_9a1cbc7e). A dead-pid fixture is fair game for
// EVERY pruner on this host, so a peer lane's prune can take it in the window
// between the plant and the peer connection landing — measured: `fixture … was
// not planted`, and the same shape gone mid-test with a peer's gate running. So
// the fixture is planted with a LIVE pid (leakOwner) and the owner is ended only
// AFTER the peer connection is in place. From then on the connection is the only
// thing keeping the fixture, and it is a sufficient one: the rule yields to a
// live backend before it ever looks at the pid, and the reason it records is
// asserted below. No window remains in which a peer's prune could take it, so
// this guard cannot red on a correct tree for the host it ran on.
func TestPruneDeadDatabasesKeepsADatabaseAPeerIsConnectedTo(t *testing.T) {
	ctx := context.Background()
	dsn := dsnOrSkip(t)
	admin, err := db.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("admin connect: %v", err)
	}
	defer admin.Close()

	// The owner is alive here, so the plant cannot be taken out from under this
	// guard while it sets the fixture up.
	owner := startLeakOwner(t)
	busy := fixtureName(t, scratchDBFamilies[0].prefix, owner.pid())
	plantDatabase(t, ctx, dsn, busy)
	if !databaseExists(t, ctx, admin, busy) {
		t.Fatalf("fixture %s is not present immediately after this test created it, while its owner (pid %d) "+
			"is alive and no pruner on this host may reclaim it", busy, owner.pid())
	}

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

	// Only now does the owner end. The fixture IS the leak from this instant —
	// a family name whose creating process is gone, with nothing about the name
	// saying it is in use — and the peer connection landed before it, so there
	// is no instant in which the fixture was both abandoned-looking and
	// unconnected for a pruner to take. release() asserts the pid is really
	// gone, so "its owner is gone" is measured and not merely intended.
	owner.release(t)

	pruneDeadDatabases(ctx, admin)

	if !databaseExists(t, ctx, admin, busy) {
		t.Fatalf("the prune dropped %s while a peer was connected to it. `DROP DATABASE` would have "+
			"refused, so the reclaim was forced — that takes a peer lane's database (and its whole run) "+
			"away, which is the one failure this rule must never have. Check that the prune issues a plain "+
			"drop and never WITH (FORCE).", busy)
	}
	res, _ := lastDBPrune()
	reason := keptReason(res, busy)
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

// TestPruneInputsSurviveADatabaseDroppedMidScan is the guard for the flake this
// card measured (t_55ee71c8): ONE database whose directory vanished while the
// prune was reading the cluster errored the WHOLE age read —
//
//	database creation times unavailable: ERROR: could not stat file
//	"base/<oid>/PG_VERSION": No such file or directory (SQLSTATE 58P01)
//
// — the prune recorded that degradation note, and
// TestPruneDeadDatabasesNeverDropsProtectedDatabases (a guard about a DIFFERENT
// property) turned the note into a red on whatever tree happened to be under
// test. It cost the merge lane a 418s `test` gate and then a full re-run of the
// same bytes, which passed solo: the red was the host, not the tree.
//
// The window is not raced for here, it is HELD OPEN. `DROP DATABASE` unlinks
// base/<oid> before the pg_database row it deletes becomes invisible, and a
// REPEATABLE READ transaction that has already read that row keeps seeing it
// after the drop has committed. So from a held snapshot the database is listed
// and its directory is already gone — the exact condition the reported error
// names — and the prune's own reads (the live querier, driven over that
// transaction rather than over the pool) are what this guard asserts on.
//
// Both directions are pinned:
//
//   - a vanished directory is ONE database's unreadable input, not a failure of
//     the read: the age read must answer for the databases whose directories are
//     there, must leave the dropped one out (an unknown age is never reclaimed),
//     and must raise NO note; the candidate read must survive the equally
//     unreadable size (measured on this host: pg_database_size answers NULL for a
//     database with no directory, while it errors for nothing in this shape);
//   - the staging really is the reported failure: read WITHOUT the tolerance, the
//     same query still raises the flake's own `could not stat file … SQLSTATE
//     58P01` — so the tolerance is load-bearing and cannot be replaced by
//     something that only moves the problem.
//
// The reads run in their own held snapshots, so a red under one of them cannot
// be explained by the other's failure (a statement error aborts its transaction).
func TestPruneInputsSurviveADatabaseDroppedMidScan(t *testing.T) {
	ctx := context.Background()
	dsn := dsnOrSkip(t)
	admin, err := db.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("admin connect: %v", err)
	}
	defer admin.Close()

	// Two fixtures: the one that is dropped mid-scan, and one that stays — the
	// control for "the read still answered for the databases that are still
	// there", which the shared database cannot provide (another lane's drop of it
	// is not this test's business, but its presence is not this test's to assume
	// either).
	//
	// Both carry a LIVE pid (this process), not deadPID's: this host runs two
	// gates at once, and a dead-pid database with nothing connected to it is
	// exactly what a PEER lane's prune is entitled to reclaim — measured while
	// writing this guard, with the merger lane's `go test ./...` running: a
	// planted dead-pid fixture was gone by the time the test dropped it
	// (`database "<fixture>" does not exist, SQLSTATE 3D000`). A live pid is the
	// rule's own "its creating process still exists" arm, so no peer can take
	// these two out from under the test.
	victim := fixtureName(t, scratchDBFamilies[0].prefix, os.Getpid())
	plantDatabase(t, ctx, dsn, victim)
	keeper := fixtureName(t, scratchDBFamilies[0].prefix, os.Getpid())
	plantDatabase(t, ctx, dsn, keeper)

	// A snapshot in which the victim is visible, held across the drop. pgx gives
	// REPEATABLE READ its snapshot at the first statement, so the read of the
	// victim's oid is what pins it (and is why it is the first thing this does).
	//
	// Each snapshot gets its own SESSION rather than a connection from the admin
	// pool: the pool's Close waits for its connections to come back, and these
	// transactions are still open when the test returns (measured as a 120s hang
	// in the first cut of this guard), while closing a dedicated session ends the
	// transaction it holds.
	holdSnapshot := func() pgx.Tx {
		t.Helper()
		conn, err := pgx.Connect(ctx, dsn)
		if err != nil {
			t.Fatalf("connect a session to hold a snapshot: %v", err)
		}
		t.Cleanup(func() { _ = conn.Close(context.Background()) })
		tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		if err != nil {
			t.Fatalf("begin a snapshot-holding transaction: %v", err)
		}
		var oid int32
		if err := tx.QueryRow(ctx, `SELECT oid::int4 FROM pg_database WHERE datname = $1`, victim).Scan(&oid); err != nil {
			t.Fatalf("pin a snapshot with %s visible: %v", victim, err)
		}
		return tx
	}
	agesTx := holdSnapshot()
	candsTx := holdSnapshot()
	controlTx := holdSnapshot()

	// The drop, from a connection that is not one of the held snapshots.
	// IF EXISTS, so a peer that managed to reclaim the fixture first leaves the
	// staging intact rather than failing the test: the held snapshots below are
	// what prove the database is listed with no directory, whoever unlinked it.
	if _, err := admin.Exec(ctx, `DROP DATABASE IF EXISTS `+quote(victim)); err != nil {
		t.Fatalf("drop %s mid-scan: %v", victim, err)
	}

	// The staging is asserted, not assumed: the held snapshot still lists the
	// database, and its directory is already unlinked.
	var oid int32
	if err := agesTx.QueryRow(ctx, `SELECT oid::int4 FROM pg_database WHERE datname = $1`, victim).Scan(&oid); err != nil {
		t.Fatalf("re-read %s in the held snapshot: %v — this guard cannot stage the window it needs", victim, err)
	}
	var dirGone bool
	if err := agesTx.QueryRow(ctx, fmt.Sprintf(`SELECT pg_stat_file('base/%d', true) IS NULL`, oid)).Scan(&dirGone); err != nil {
		t.Fatalf("probe the base directory of %s: %v", victim, err)
	}
	if !dirGone {
		t.Fatalf("the base directory of %s is still readable after its drop — this guard needs a database that is "+
			"still listed in a held snapshot with its directory already unlinked, or it asserts nothing", victim)
	}

	// 1. The age read: the dropped database's age is unknown, the others' are not.
	ages, err := pgDatabases{admin: agesTx}.creationTimes(ctx)
	if err != nil {
		t.Errorf("reading database creation times failed with ONE database dropped mid-scan: %v\n\n"+
			"A database's directory is unlinked before its pg_database row goes, so a database this read can still\n"+
			"see may have no base/<oid>/PG_VERSION to stat. That is one database's age unknown — the conservative\n"+
			"answer the rule already takes for an age it cannot read — and not an unavailability of the age source,\n"+
			"which is what the degradation note is reserved for. Reporting it as a degradation reds the whole\n"+
			"`test` gate on whichever tree happens to be under test (see scratchDBAgesQuery).", err)
	} else {
		if _, ok := ages[victim]; ok {
			t.Errorf("the age read answered for %s, whose directory was already unlinked — that age cannot be its "+
				"creation time, and a value here would let the unattributable arm reclaim on it", victim)
		}
		if _, ok := ages[keeper]; !ok {
			t.Errorf("the age read answered for none of the databases that still have a directory (%s is missing from %d "+
				"entr(ies)) — one dropped database must not cost every other database its age", keeper, len(ages))
		}
	}

	// 2. The control: the same read, un-tolerated, still raises the flake's error.
	//    In its own held snapshot, because a failure here aborts its transaction
	//    and would otherwise take the reads below down with it.
	controlRows, controlErr := controlTx.Query(ctx, `SELECT datname, (pg_stat_file('base/' || oid || '/PG_VERSION')).modification
  FROM pg_database
 WHERE NOT datistemplate`)
	if controlErr == nil {
		for controlRows.Next() {
		}
		controlRows.Close()
		controlErr = controlRows.Err()
	}
	if controlErr == nil {
		t.Errorf("the un-tolerated age read answered every database while %s is listed with no directory — this guard "+
			"is not staging the failure the card measured, so a green above proves nothing", victim)
	} else if !strings.Contains(controlErr.Error(), "could not stat file") ||
		!strings.Contains(controlErr.Error(), "58P01") {
		t.Errorf("the un-tolerated age read failed with %v, want the flake's own `could not stat file … (SQLSTATE 58P01)` — "+
			"the mechanism this guard stages has changed", controlErr)
	}

	// 3. The candidate read: the dropped database's size is unreadable too, which
	//    must not fail the list either. In its own held snapshot, so the aborted
	//    transaction above cannot be blamed for it.
	cands, err := pgDatabases{admin: candsTx}.candidates(ctx)
	if err != nil {
		t.Errorf("listing the databases failed with ONE database dropped mid-scan: %v\n\n"+
			"pg_database_size(oid) walks the database's directory and answers NULL for one that has none — the same\n"+
			"window as the age read. A NULL must not be scanned into an int and must not fail the list; a database\n"+
			"being dropped is not this prune's to reclaim, so it is left out.", err)
	} else {
		var sawKeeper bool
		for _, c := range cands {
			if c.name == victim {
				t.Errorf("the database %s, whose directory has already been unlinked, came back as a candidate of size "+
					"%d — it is being dropped by whoever unlinked it, and %d is not its size", victim, c.size, c.size)
			}
			if c.name == keeper {
				sawKeeper = true
			}
		}
		if !sawKeeper {
			t.Errorf("%s (a database with a directory, planted by this guard) is not among the %d candidate(s) — one "+
				"dropped database must not cost the enumeration its other rows", keeper, len(cands))
		}
	}
	// The note is the degradation channel, and neither read degraded.
	if note := databasePruneNote(); note != "" {
		t.Errorf("the prune's reads recorded %q for ONE database whose directory was unlinked mid-scan. A note turns "+
			"TestPruneDeadDatabasesNeverDropsProtectedDatabases — a guard about the PROTECTED names — into a red on "+
			"whatever tree is under test, which is exactly the flake this card is about.", note)
	}
}

// TestScratchDatabaseAgeReadStillFailsWhenTheAgeSourceIsUnavailable is the other
// half of the same fix (t_55ee71c8), and the reason the tolerance added there is
// a tolerance and not a cover-up. The degradation
// TestScratchDatabaseCreationTimesAreReadable guards — a role that cannot call
// pg_stat_file at all — must still FAIL the read: `missing_ok` turns away a
// missing file only, so nothing about a vanished directory can answer for it. If
// it ever did, every age would silently read as "unknown", the unattributable arm
// of the rule would stop reclaiming (most of the leak), the prune's note would
// stay empty, and the guard would be green on a host where the reclaim is dead.
//
// Driven with a role that holds neither pg_monitor nor pg_read_server_files, in a
// transaction that is rolled back: CREATE ROLE is transactional, so the role
// cannot outlive the test (asserted below, because a leftover role is exactly the
// kind of residue a guard on a shared cluster must not add).
func TestScratchDatabaseAgeReadStillFailsWhenTheAgeSourceIsUnavailable(t *testing.T) {
	ctx := context.Background()
	dsn := dsnOrSkip(t)
	admin, err := db.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("admin connect: %v", err)
	}
	defer admin.Close()

	tx, err := admin.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin the transaction that owns the throwaway role: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	role := "jf_age_nopriv_" + randHex(t)
	if _, err := tx.Exec(ctx, "CREATE ROLE "+quote(role)+" NOLOGIN"); err != nil {
		t.Fatalf("create the unprivileged role %s (needs CREATEROLE/superuser on this host): %v", role, err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE "+quote(role)); err != nil {
		t.Fatalf("become %s for the read: %v", role, err)
	}

	rows, qerr := tx.Query(ctx, scratchDBAgesQuery)
	answered := 0
	if qerr == nil {
		for rows.Next() {
			answered++
		}
		rows.Close()
		qerr = rows.Err()
	}
	if qerr == nil {
		t.Errorf("the age read answered %d database(s) as %s, a role holding neither pg_monitor nor "+
			"pg_read_server_files — pg_stat_file is not available to it, so every creation time it reported can "+
			"only be wrong, and the prune would go on reclaiming (or, here, NOT reclaiming) on an age source that "+
			"does not exist. The tolerance for a database dropped mid-scan must not reach this shape: it is the "+
			"degradation the note and TestScratchDatabaseCreationTimesAreReadable exist for", answered, role)
	} else if !strings.Contains(qerr.Error(), "permission denied") {
		t.Errorf("the age read as %s failed with %v, want `permission denied for function pg_stat_file` — the "+
			"degradation this guard stages has changed shape", role, qerr)
	}

	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("roll back the transaction that created %s: %v", role, err)
	}
	var left int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM pg_roles WHERE rolname = $1`, role).Scan(&left); err != nil {
		t.Fatalf("check whether %s survived the rollback: %v", role, err)
	}
	if left != 0 {
		t.Errorf("the throwaway role %s survived the rollback (%d row(s) in pg_roles) — this guard must not leave a "+
			"cluster object behind on a host other lanes share", role, left)
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
