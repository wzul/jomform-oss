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
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/db"
)

// reachScratchPrefix names the throwaway databases these guards create. Kept
// short so the generated name stays well inside Postgres' 63-byte limit.
const reachScratchPrefix = "jomform_reachtest_"

// otherBackends counts every connection to the current database except this one
// — the same question open()'s prune asks, spelled out here so the fixture can
// refuse to run when the idle precondition does not hold. It is deliberately
// kept identical to open()'s predicate (EVERY backend, not just client
// backends): the guard may only take its verdict on a database that open() will
// itself agree is idle, and a narrower count here would let the guard assert a
// reclaim that open() had correctly suppressed.
const otherBackends = `SELECT count(*) FROM pg_stat_activity
  WHERE datname = current_database() AND pid <> pg_backend_pid()`

// idleWaitBudget and idleSampleInterval bound how long the reclaim guard waits
// for its throwaway database to go idle. A transient autovacuum worker leaves
// within milliseconds, so the budget never has to be large; a peer test binary
// never leaves within it, which is what keeps the refusal honest. See
// awaitNoOtherBackends.
const (
	idleWaitBudget     = 30 * time.Second
	idleSampleInterval = 250 * time.Millisecond
)

// backendDump renders who else is connected. It exists so a refusal names its
// cause — a peer test binary and an autovacuum worker are very different
// findings, and the gate's reader should not have to re-derive which one it was.
const backendDump = `SELECT pid, backend_type, coalesce(state, ''), coalesce(query, '')
  FROM pg_stat_activity
  WHERE datname = current_database() AND pid <> pg_backend_pid()
  ORDER BY pid`

// abandonedSchemaSurvived reports whether a schema is still present, and — when it
// is — whether the backend that was there at the moment open() took its decision
// was a TRANSIENT visitor or a PEER.
//
// WHY THE DISTINCTION IS THE WHOLE POINT (card t_741cd8f2)
//
// `pruneDeadSchemas` suppresses the reclaim whenever `others > 0`, and that is
// correct by design: `DROP SCHEMA ... CASCADE` is database-wide DDL and a peer can
// be mid-catalogue-read. But it means a transient visitor (an autovacuum worker
// ANALYZEing the catalogue, which the CREATE DATABASE churn of this very test
// provokes) produces an outcome INDISTINGUISHABLE from a dead reclaim by schema
// presence alone. The test's own docstring says as much and then asserted on
// presence anyway:
//
//	"a reclaim that works reclaims on at least one attempt, which is all the
//	 claim needs"
//
// …while `t.Errorf` fired per attempt. Measured on this host at load 15-18:
// 3 red in 20 runs, every red a single suppressed attempt with `witnessed` true —
// i.e. the reclaim was DEMONSTRABLY working on the other four attempts and the
// test still red. That is a false-red generator inside the gate it was written to
// de-flake.
//
// So a surviving schema is only evidence against the reclaim when the visitor
// OUTLIVED the sample that declared the database idle. A visitor that was already
// gone when open() ran cannot have suppressed anything: we returned 0 ourselves.
// A visitor that is STILL there afterwards may have been the one open() saw, and
// that is the case the guard must report.
//
// Nothing is excused on a database that is quiet: the sampling interval is 250ms
// and an autovacuum worker on a catalogue this size is a few milliseconds, so on a
// genuinely idle host the peer count is 0 and the check below is fully armed.
func abandonedSchemaSurvived(ctx context.Context, p *pgxpool.Pool, name string) (survived bool, peers int, detail string, err error) {
	if qerr := p.QueryRow(ctx, `SELECT count(*) FROM pg_namespace WHERE nspname = $1`, name).Scan(&peers); qerr != nil {
		return false, 0, "", qerr
	}
	if peers == 0 {
		return false, 0, "", nil
	}
	var dump string
	if derr := p.QueryRow(ctx, backendDumpAgg).Scan(&dump); derr != nil {
		return true, 0, "(backend dump failed: " + derr.Error() + ")", nil
	}
	// Recount after the dump, deliberately AFTER: a visitor that left while the
	// dump ran was transient. This is the same reasoning the reclaim's own
	// `solo` predicate uses, in the opposite direction.
	var after int
	if aerr := p.QueryRow(ctx, otherBackendsCount).Scan(&after); aerr != nil {
		return true, 0, "", aerr
	}
	return true, after, dump, nil
}

// otherBackendsCount and backendDumpAgg are the post-hoc variants used above.
// They read the same predicate as open()'s prune (EVERY backend, not just client
// backends), because that is the count the reclaim acts on.
const otherBackendsCount = `SELECT count(*) FROM pg_stat_activity
  WHERE datname = current_database() AND pid <> pg_backend_pid()`

const backendDumpAgg = `SELECT coalesce(string_agg(
    format('pid=%s type=%s state=%s query=%s', pid, backend_type, coalesce(state,''), left(coalesce(query,''), 80)), E'\n'), '(none)')
  FROM pg_stat_activity
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

	// reclaimAttempts bounds how many times the reclaim is driven. A single
	// attempt cannot take the verdict: idle is a property of the whole server,
	// and under host load a transient visitor — an autovacuum worker ANALYZEing
	// the shared catalogue, which the CREATE DATABASE churn itself provokes — can
	// still be connected at the instant open() runs its own prune, so open()
	// suppresses the reclaim exactly as designed. Measured on the loaded host:
	// 8/300 attempts suppressed after the idle wait had already succeeded, and
	// the pre-wait suppression rate was 2/300. Several attempts each have to
	// observe the abandoned schema, and ALL of them have to survive:
	//
	//   - a reclaim that is really broken (say, the prune moved after the pool
	//     is built) leaves the schema behind on every attempt, so the guard
	//     still reds, and
	//   - a reclaim that works reclaims on at least one attempt, which is all
	//     the claim needs.
	//
	// The vacuity risk this creates — every attempt yielding to a visitor and
	// the guard passing with nothing observed — is closed below by asserting
	// that at least one attempt was genuinely observed, and by failing the test
	// outright if no attempt ever saw an idle server at all.
	const reclaimAttempts = 5

	type attemptOutcome struct {
		abandonedLeft bool
		legacyLeft    bool
		ownDropped    bool
		// peersAfter is how many OTHER backends were connected when the probe ran.
		// 0 with a surviving schema is a dead reclaim; >0 means the suppression the
		// reclaim is designed to make, which this test reports and does not fail.
		peersAfter int
		detail     string
	}

	var outcomes []attemptOutcome
	for attempt := 1; attempt <= reclaimAttempts; attempt++ {
		// Fresh fixtures per attempt: whether the reclaim happened is asked of
		// the database, so a schema an earlier attempt left behind would make
		// every later attempt read as a failure. plant's errors are fatal — a
		// fixture that cannot be planted must not be silently read as "the
		// schema is gone".
		abandoned := "test_reach_abandoned_p" + strconv.Itoa(deadPID(t)) + "_a" + strconv.Itoa(attempt)
		legacy := "test_reach_legacy_a" + strconv.Itoa(attempt)

		plant, err := db.Connect(ctx, dbDSN)
		if err != nil {
			t.Fatalf("connect to %s to plant the fixtures: %v", dbname, err)
		}
		var firstErr string
		for _, n := range []string{abandoned, legacy} {
			if _, err := plant.Exec(ctx, `CREATE SCHEMA `+quote(n)); err != nil {
				// A duplicate schema is the fingerprint of an earlier attempt
				// that the reclaim did not clean and this fixture did not
				// expect: record it rather than letting the run die here.
				firstErr = fmt.Sprintf("plant %s: %v", n, err)
				break
			}
		}
		if firstErr != "" {
			plant.Close()
			t.Fatalf("attempt %d on %s could not plant its fixtures (%s) — the reclaim left an earlier attempt's schema behind, so this attempt cannot take a verdict", attempt, dbname, firstErr)
		}

		// Wait for the throwaway database to be idle before letting open() take
		// its own read, so a visitor already connected does not pre-empt the
		// reclaim before the attempt even starts. "Idle" is idleness under
		// open()'s OWN predicate: that count is every backend, not just client
		// backends, so a transient background visitor is enough to make open()
		// suppress the reclaim by design.
		ok, detail, err := awaitNoOtherBackends(ctx, plant, idleWaitBudget)
		if err != nil {
			t.Fatalf("count backends on %s: %v", dbname, err)
		}
		if !ok {
			plant.Close()
			t.Fatalf("attempt %d: throwaway database %s never went idle within %s (%s) — the idle precondition does not hold, so this test cannot distinguish a working reclaim from a suppressed one",
				attempt, dbname, idleWaitBudget, detail)
		}
		plant.Close()

		// The real path under test.
		p, err := open(ctx, dbDSN, own)
		if err != nil {
			t.Fatalf("open on the throwaway database %s: %v", dbname, err)
		}

		var landed string
		if err := p.QueryRow(ctx, `SELECT current_database()`).Scan(&landed); err != nil {
			t.Fatalf("read current_database: %v", err)
		}
		if landed != dbname {
			t.Fatalf("open() landed on database %q, want the throwaway %q — this guard would otherwise inspect the wrong database", landed, dbname)
		}

		o := attemptOutcome{}
		survived, peersAfter, dump, err := abandonedSchemaSurvived(ctx, p, abandoned)
		if err != nil {
			t.Fatalf("probe %s: %v", abandoned, err)
		}
		o.abandonedLeft = survived
		// A schema that survived while NO backend is connected afterwards is
		// unambiguous: nothing was there to suppress the reclaim, so the reclaim
		// did not run (or ran and failed). This is a hard failure regardless of
		// what any other attempt witnessed — it is the direction that must never
		// pass.
		if survived && peersAfter == 0 {
			t.Errorf("attempt %d: open() left %s behind on a database with NO other backend connected — nothing could have suppressed the reclaim, so this is a dead reclaim, not a transient visitor. Check that open() still prunes BEFORE it creates the caller's pool (pgxpool is lazy, so pruning after pgxpool.NewWithConfig/Ping counts the caller's own backend as a peer and suppresses the reclaim forever). Backends seen just before this probe:\n%s",
				attempt, abandoned, dump)
		}
		o.peersAfter = peersAfter
		o.detail = dump

		legacySurvived, legacyPeers, legacyDump, lerr := abandonedSchemaSurvived(ctx, p, legacy)
		if lerr != nil {
			t.Fatalf("probe %s: %v", legacy, lerr)
		}
		o.legacyLeft = legacySurvived
		if legacySurvived && legacyPeers == 0 {
			t.Errorf("attempt %d: open() left the pre-pid schema %s behind with NO other backend connected — a name with no pid can never be attributed to a process, so it is reclaimable exactly when nobody else is connected, which is this case. Backends seen before this probe:\n%s",
				attempt, legacy, legacyDump)
		}

		var ownCount int
		if err := p.QueryRow(ctx, `SELECT count(*) FROM pg_namespace WHERE nspname = $1`, own).Scan(&ownCount); err != nil {
			t.Fatalf("probe %s: %v", own, err)
		}
		o.ownDropped = ownCount == 0
		outcomes = append(outcomes, o)
		p.Close()
	}

	reclaimedLegacy := false
	witnessed := false
	suppressed := 0
	for i, o := range outcomes {
		switch {
		case !o.abandonedLeft:
			witnessed = true
		case o.peersAfter > 0:
			// Suppressed by a visitor that was still there when we probed. That is
			// the designed behaviour (`others > 0` => no DDL), and the docstring's
			// own model says one witnessed attempt is all the claim needs — so this
			// is reported, not failed. The hard failure for this shape lives in
			// TestOpenReclaimsAnAbandonedSchemaOnAnIdleDatabaseNeverSuppressedAlone.
			suppressed++
			t.Logf("attempt %d: reclaim suppressed while %d backend(s) were connected — by design (DROP SCHEMA is database-wide DDL); waiting for the run's verdict:\n%s",
				i+1, o.peersAfter, o.detail)
		}
		if !o.abandonedLeft && !o.legacyLeft {
			reclaimedLegacy = true
		}
		if o.ownDropped {
			t.Errorf("attempt %d: open() dropped this process's own schema %s — the prune must never reclaim the schema it is about to migrate and run against", i+1, own)
		}
	}
	if suppressed > 0 {
		t.Logf("note: %d/%d attempts were suppressed by a concurrent backend; %d attempt(s) witnessed a successful reclaim",
			suppressed, len(outcomes), len(outcomes)-suppressed)
	}

	// The vacuity guard, and the assertion that gives the others their meaning:
	// if every attempt yielded to a visitor then nothing was ever observed, and
	// a report of "reclaim works" would be a report of the guard proving
	// nothing. It has to be a hard failure, not a skip, because the direction it
	// protects is the one that silently rots: a reclaim that is dead would
	// otherwise be indistinguishable from a busy host.
	if !witnessed {
		t.Fatalf("open() never reclaimed an abandoned schema in %d attempts on an idle database — either the reclaim is dead (every attempt left the schema behind) or every attempt yielded to a transient visitor, and this guard cannot tell the two apart", reclaimAttempts)
	}
	if !reclaimedLegacy {
		t.Logf("note: an abandoned schema was reclaimed, but never in an attempt that also reclaimed the legacy shape — the two shapes are reached by different predicates, so check the legacy arm if it stays silent across runs")
	}
}

// TestOpenReclaimsAnAbandonedSchemaOnAnIdleDatabaseNeverSuppressedAlone is the
// discriminator the guard above cannot be.
//
// The positive arm above must tolerate a suppressed attempt — the reclaim's own
// design (`others > 0` => no DDL) makes suppression correct, and the claim it
// carries ("a reclaim that works reclaims on at least one attempt") is true. But
// tolerating it means a reclaim that is DEAD on a genuinely quiet database could
// hide behind "some visitor must have been there". This test closes that: it drives
// open() on a database this test has just confirmed quiet, and requires the reclaim
// to happen — with NO attempt-level tolerance.
//
// It is one attempt, deliberately. Repeating it would re-import the very
// probabilistic reasoning that made the other test red on a working reclaim: at the
// measured visitor rate a second attempt can legitimately be suppressed. One attempt
// on a database confirmed quiet BEFORE open() runs is a sharper question — and if
// the host is genuinely too busy to answer it, the test SKIPS with the backends
// dumped rather than reporting a verdict it cannot support.
func TestOpenReclaimsAnAbandonedSchemaOnAnIdleDatabaseNeverSuppressedAlone(t *testing.T) {
	ctx := context.Background()
	dbname, dbDSN := createIdleTestDB(t, ctx, dsnOrSkip(t))

	own := "test_reach_own3_p" + strconv.Itoa(os.Getpid())
	abandoned := "test_reach_never_p" + strconv.Itoa(deadPID(t))

	plant, err := db.Connect(ctx, dbDSN)
	if err != nil {
		t.Fatalf("connect to %s to plant the fixture: %v", dbname, err)
	}
	if _, err := plant.Exec(ctx, `CREATE SCHEMA `+quote(abandoned)); err != nil {
		plant.Close()
		t.Fatalf("plant %s: %v", abandoned, err)
	}
	// The plant connection must be GONE before open() takes its decision, or the
	// fixture is itself the "other backend" it is watching for.
	ok, detail, err := awaitNoOtherBackends(ctx, plant, idleWaitBudget)
	plant.Close()
	if err != nil {
		t.Fatalf("count backends on %s: %v", dbname, err)
	}
	if !ok {
		t.Skipf("throwaway database %s never went idle within %s (%s) — this test needs a quiet database to ask whether the reclaim runs unbidden; it reports no verdict rather than a weak one",
			dbname, idleWaitBudget, detail)
	}

	// No sampling between here and open(): the fixture is closed and the database
	// answered "quiet" on the immediately preceding query.
	p, err := open(ctx, dbDSN, own)
	if err != nil {
		t.Fatalf("open on the throwaway database %s: %v", dbname, err)
	}
	defer p.Close()

	survived, peersAfter, dump, err := abandonedSchemaSurvived(ctx, p, abandoned)
	if err != nil {
		t.Fatalf("probe %s: %v", abandoned, err)
	}
	if !survived {
		return // the reclaim ran: exactly what this test requires.
	}
	if peersAfter > 0 {
		t.Skipf("the abandoned schema %s survived, but %d backend(s) were connected when it was probed, so this run cannot say whether the reclaim ran — and a skip is honest here where an Error would send a lane hunting a reclaim that may be fine. Backends:\n%s",
			abandoned, peersAfter, dump)
	}
	t.Errorf("open() left the abandoned schema %s behind on a database with NO other backend connected, and the reclaim is REQUIRED to run in that state — a killed run then leaks a schema per run. Check that open() still prunes BEFORE it creates the caller's pool (pgxpool is lazy, so pruning after pgxpool.NewWithConfig/Ping counts the caller's own backend as a peer and suppresses the reclaim forever)",
		abandoned)
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

// awaitNoOtherBackends waits for the database to have no backend but `conn`,
// and reports whether it got there inside budget.
//
// The wait, not a narrower count, is what fixes the false red. Host contention
// does not create peers in a throwaway database whose name was drawn 48 bits of
// randomness a moment ago; what it does is make the CREATE DATABASE churn on the
// shared catalogue frequent enough that the autovacuum launcher keeps sending a
// worker to ANALYZE pg_database. A worker that is mid-ANALYZE is connected, so it
// counts under open()'s predicate, and open() then suppresses the reclaim
// exactly as designed — whereupon the old instant read of that same count called
// the suppression a broken reclaim. A worker is transient: the live capture
// showed it leaving within the sample interval, and on an empty database it has
// nothing to hold. A real peer never leaves, so a refusal here still means what
// the guard intends it to mean.
//
// It reports rather than fails, because both callers have to assert on the
// answer and the refusal test must be able to observe a false without that false
// ending the test that is looking for it. `detail` names who was still connected
// when the budget ran out; `err` is non-nil only when the count itself failed.
//
// The count and the dump both come from the caller's own connection, so the
// backend this process holds is never the one being waited on: only foreign
// backends can keep this looping.
func awaitNoOtherBackends(ctx context.Context, conn *pgxpool.Pool, budget time.Duration) (ok bool, detail string, err error) {
	deadline := time.Now().Add(budget)
	for attempt := 0; ; attempt++ {
		var others int
		if err := conn.QueryRow(ctx, otherBackends).Scan(&others); err != nil {
			return false, "", err
		}
		if others == 0 {
			return true, "", nil
		}
		if time.Now().After(deadline) {
			return false, fmt.Sprintf("%d other backends after %d samples; who is connected:\n%s",
				others, attempt+1, renderBackends(ctx, conn)), nil
		}
		select {
		case <-ctx.Done():
			return false, "", ctx.Err()
		case <-time.After(idleSampleInterval):
		}
	}
}

// renderBackends formats the other backends on the current database, best
// effort — it runs on the refusal path, where a diagnostic that itself dies is
// not allowed to obscure the refusal it was called to explain.
func renderBackends(ctx context.Context, conn *pgxpool.Pool) string {
	rows, err := conn.Query(ctx, backendDump)
	if err != nil {
		return fmt.Sprintf("  (backend dump failed: %v)", err)
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var pid int
		var backendType, state, query string
		if err := rows.Scan(&pid, &backendType, &state, &query); err != nil {
			lines = append(lines, fmt.Sprintf("  (scan failed: %v)", err))
			break
		}
		lines = append(lines, fmt.Sprintf("  pid=%d backend_type=%s state=%s query=%q", pid, backendType, state, query))
	}
	if len(lines) == 0 {
		return "  (no rows — the other backend left between the count and the dump)"
	}
	return strings.Join(lines, "\n")
}

// TestAwaitNoOtherBackendsWaitsOutATransientVisitor pins BOTH directions of the
// wait that keeps host contention from reading as a broken reclaim.
//
// This is the guard for a red that actually happened: with the host at load ~22
// on 6 cores, TestOpenReclaimsAnAbandonedSchemaOnAnIdleDatabase read
// `1 other backends` and failed, while nothing in the package is parallel and
// every throwaway database name is freshly drawn — so there was no peer to blame.
// The visitor is an autovacuum worker. CREATE DATABASE churn on the shared
// catalogue draws a worker to ANALYZE pg_catalog.pg_database, the worker counts
// under open()'s predicate, and open() suppresses the reclaim exactly as
// designed; the old instant read of that same count called the suppression a
// broken reclaim.
//
// A pure fake pins the classification without a live autovacuum worker to
// provoke, and pins the refusal too: a visitor that never leaves must still
// fail, because a guard that waits out a real peer would let this test pass
// vacuously — it would call a suppressed reclaim a correct one.
func TestAwaitNoOtherBackendsWaitsOutATransientVisitor(t *testing.T) {
	if dsnOrSkip(t) == "" {
		return // dsnOrSkip already skipped
	}
	ctx := context.Background()

	// A database of our own, so the peer planted below cannot make the shared
	// test database's own cleanup (or any other package's) think the host is
	// busy.
	_, dbDSN := createIdleTestDB(t, ctx, dsnOrSkip(t))

	t.Run("a transient visitor is waited out, not reported as a failure", func(t *testing.T) {
		watcher, err := db.Connect(ctx, dbDSN)
		if err != nil {
			t.Fatalf("connect: %v", err)
		}
		defer watcher.Close()

		leaver, err := db.Connect(ctx, dbDSN)
		if err != nil {
			t.Fatalf("connect the transient visitor: %v", err)
		}
		if _, err := leaver.Exec(ctx, `SELECT 1`); err != nil {
			t.Fatalf("make the transient visitor real: %v", err)
		}
		go func() {
			time.Sleep(2 * idleSampleInterval)
			leaver.Close()
		}()

		start := time.Now()
		ok, detail, err := awaitNoOtherBackends(ctx, watcher, idleWaitBudget)
		if err != nil {
			t.Fatalf("awaitNoOtherBackends: %v", err)
		}
		if !ok {
			t.Fatalf("awaitNoOtherBackends refused although the only other backend was a visitor that left (%s) — a transient autovacuum worker would red the reclaim guard exactly this way under host load", detail)
		}
		if elapsed := time.Since(start); elapsed < idleSampleInterval {
			t.Errorf("awaitNoOtherBackends returned in %s, before it could have sampled a still-present visitor — the wait is not being exercised", elapsed)
		}
		t.Logf("waited %s for the visitor to leave", time.Since(start).Round(time.Millisecond))
	})

	t.Run("a backend that stays still refuses", func(t *testing.T) {
		watcher, err := db.Connect(ctx, dbDSN)
		if err != nil {
			t.Fatalf("connect: %v", err)
		}
		defer watcher.Close()

		peer, err := db.Connect(ctx, dbDSN)
		if err != nil {
			t.Fatalf("connect the peer: %v", err)
		}
		defer peer.Close()
		if _, err := peer.Exec(ctx, `SELECT 1`); err != nil {
			t.Fatalf("make the peer real: %v", err)
		}

		// The wait must not turn the idle precondition into a formality: a
		// backend that is still there when the budget runs out has to refuse,
		// because a suppressed reclaim reported as a working one is the vacuous
		// pass this guard exists to prevent.
		ok, detail, err := awaitNoOtherBackends(ctx, watcher, 3*idleSampleInterval)
		if err != nil {
			t.Fatalf("awaitNoOtherBackends: %v", err)
		}
		if ok {
			t.Error("awaitNoOtherBackends returned true with a peer backend the whole time — waiting must not turn the idle precondition into a formality, or a suppressed reclaim would be reported as a working one")
		}
		if detail == "" {
			t.Error("awaitNoOtherBackends refused without saying who was still connected; the gate's reader has to be able to tell a peer from an autovacuum worker")
		}
		t.Logf("refused as designed: %s", detail)
	})
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
