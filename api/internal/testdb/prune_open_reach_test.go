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

// schemaSurvived reports whether a schema is still present, and — when it is —
// who else was connected at the moment of this probe, as report context only.
//
// The VERDICT is deliberately NOT taken here. Whether a surviving schema is
// evidence of a dead reclaim depends on what open()'s prune SAW when it decided
// — connectionSnapshot() — not on a fresh count taken an instant later. A
// transient visitor can come and go between the two reads, and comparing them is
// exactly what produced the false RED this guard carried (card t_53b9b15f): the
// prune read `others=1` and suppressed by design, this probe read 0 and called
// it "a dead reclaim with NO other backend connected", and the only gate went
// red for a reclaim that was working. So the dump returned here only makes a
// report richer; it is never the basis of a failure.
func schemaSurvived(ctx context.Context, p *pgxpool.Pool, name string) (survived bool, liveDump string, err error) {
	var n int
	if err := p.QueryRow(ctx, `SELECT count(*) FROM pg_namespace WHERE nspname = $1`, name).Scan(&n); err != nil {
		return false, "", err
	}
	if n == 0 {
		return false, "", nil
	}
	var dump string
	if derr := p.QueryRow(ctx, backendDumpAgg).Scan(&dump); derr != nil {
		dump = "(backend dump failed: " + derr.Error() + ")"
	}
	return true, dump, nil
}

// backendDumpAgg renders the foreign backends on the current database, best
// effort, for report context. It reads the same predicate as open()'s prune
// (EVERY backend, not just client backends), because that is the count the
// reclaim acts on.
const backendDumpAgg = `SELECT coalesce(string_agg(
    format('pid=%s type=%s state=%s query=%s', pid, backend_type, coalesce(state,''), left(coalesce(query,''), 80)), E'\n'), '(none)')
  FROM pg_stat_activity
  WHERE datname = current_database() AND pid <> pg_backend_pid()`

// judgeSurvival decides what a surviving schema means, against the connection
// reading open()'s prune took its decision on. It is the fix for card
// t_53b9b15f, and it is a pure function so both directions can be pinned by a
// unit test without a live transient visitor to provoke.
//
//   - !haveSnap: no prune ran (or none recorded its reading), so nothing can be
//     concluded — the caller must refuse to take a verdict.
//   - snap.others > 0: the prune saw a foreign backend and issued no DDL by
//     design (`others > 0` => no DDL). The schema surviving is CORRECT; the
//     guard reports it and does not fail, however quiet the database looks now.
//   - snap.others == 0: the prune saw nobody, so nothing could have suppressed
//     it. A surviving schema here is a DEAD reclaim — the direction that must
//     never pass, and it stays a hard failure.
func judgeSurvival(survived bool, snap connSnapshot, haveSnap bool) (dead, suppressed, unknown bool) {
	if !survived {
		return false, false, false
	}
	switch {
	case !haveSnap:
		return false, false, true
	case snap.others > 0:
		return false, true, false
	default:
		return true, false, false
	}
}

// TestJudgeSurvivalNeverCallsASeenBackendADeadReclaim pins the exact false RED
// this card is about, as a pure regression on the verdict function.
//
// The scenario is the one measured on the loaded host: the prune reads a foreign
// backend (others=1) and issues no DDL by design, and by the time anything probes
// afterwards that backend is gone, so a fresh count reads 0. The guard that
// judged on the fresh count reported "a dead reclaim with NO other backend
// connected" and the only gate went red for a reclaim that was working. With the
// verdict taken on the prune's OWN reading, that scenario cannot produce `dead`
// — whatever a later probe sees.
func TestJudgeSurvivalNeverCallsASeenBackendADeadReclaim(t *testing.T) {
	snap := connSnapshot{others: 1, dump: "pid=99 type=autovacuum worker state=active query=autovacuum: ANALYZE pg_catalog.pg_database"}

	dead, suppressed, unknown := judgeSurvival(true, snap, true)
	if dead {
		t.Error("judgeSurvival called a surviving schema a DEAD reclaim although the prune SAW another backend (others=1) — that prune suppressed the reclaim by design (`others > 0` => no DDL), so calling it dead is the load-sensitive false RED (card t_53b9b15f)")
	}
	if !suppressed {
		t.Error("judgeSurvival did not report the seen-backend case as suppressed — a surviving schema under a prune that saw a backend is the designed yield, and the run must report it as such rather than silently passing")
	}
	if unknown {
		t.Error("judgeSurvival reported unknown although it had the prune's reading")
	}
}

// TestJudgeSurvivalStillFailsADeadReclaim is the direction that must NEVER pass,
// and it is the reason the verdict has to exist at all: a prune that saw NOBODY
// and still left the schema behind is a reclaim that did not run. If the fix
// above ever widened to treat every surviving schema as suppressed, this reds.
func TestJudgeSurvivalStillFailsADeadReclaim(t *testing.T) {
	dead, suppressed, _ := judgeSurvival(true, connSnapshot{others: 0}, true)
	if !dead {
		t.Error("judgeSurvival did not call a schema that survived a prune which saw NO other backend a dead reclaim — that is the direction that must never pass (a reclaim that does not run leaks a schema per killed run)")
	}
	if suppressed {
		t.Error("judgeSurvival reported the seen-nobody case as suppressed — nothing was there to suppress it")
	}
}

// TestJudgeSurvivalRefusesWithoutAReading pins that the guard refuses to take a
// verdict when the prune recorded nothing: it cannot tell a dead reclaim from a
// reclaim that never ran the prune at all, and guessing either way is how a guard
// lies.
func TestJudgeSurvivalRefusesWithoutAReading(t *testing.T) {
	if dead, suppressed, unknown := judgeSurvival(true, connSnapshot{}, false); !unknown || dead || suppressed {
		t.Errorf("judgeSurvival(survived=true, haveSnap=false) = (dead=%v suppressed=%v unknown=%v), want unknown only — with no prune reading the guard must refuse a verdict, not invent one", dead, suppressed, unknown)
	}
	if dead, suppressed, unknown := judgeSurvival(false, connSnapshot{}, false); dead || suppressed || unknown {
		t.Errorf("judgeSurvival(survived=false, haveSnap=false) = (dead=%v suppressed=%v unknown=%v), want all false — a reclaimed schema is a success regardless of whether a reading was recorded", dead, suppressed, unknown)
	}
}

// TestOpenRecordsThePruneConnectionReading pins the WIRING the verdict depends
// on: a real open() must record the reading its prune decided on, so the guard
// can judge that decision. Without it connectionSnapshot() would hand the guard a
// stale (or absent) reading and every surviving schema would look unknown.
func TestOpenRecordsThePruneConnectionReading(t *testing.T) {
	ctx := context.Background()
	dbname, dbDSN := createIdleTestDB(t, ctx, dsnOrSkip(t))
	own := "test_reach_own_snap_p" + strconv.Itoa(os.Getpid())

	// Drain any reading left by another test in this binary, so this test proves
	// the reading came from ITS open(), not a neighbour's.
	lastPruneSnapshot.Store(nil)

	p, err := open(ctx, dbDSN, own)
	if err != nil {
		t.Fatalf("open on the throwaway database %s: %v", dbname, err)
	}
	defer p.Close()

	snap, ok := connectionSnapshot()
	if !ok {
		t.Fatalf("open() on %s recorded no prune connection reading — the reclaim guard would have no reading to judge the reclaim against (card t_53b9b15f)", dbname)
	}
	if snap.others < 0 {
		t.Errorf("recorded reading has others=%d, want >= 0", snap.others)
	}
}

// TestOpenReclaimsAnAbandonedSchemaOnAnIdleDatabase is the positive arm of the
// prune wiring guard: on a database with nobody else connected, a real open()
// must drop a leftover scratch schema whose owner is gone.
//
// Both reclaimable shapes are planted, because they are reached by different
// predicates: a pid-qualified name whose owner has exited (abandoned), and a
// pre-pid name that cannot be attributed to any process at all (legacy). This
// process's own schema must survive both — ensureSchema has just created it, and
// a prune that dropped it would fail the very next query the suite runs.
//
// The verdict is taken against the connection reading the prune ITSELF decided
// on (connectionSnapshot), not a fresh count taken afterwards (card t_53b9b15f).
// That closes the load-sensitive false RED this guard carried: on a busy host a
// transient visitor could be connected at the prune's instant and gone an instant
// later, so the prune read `others=1` and suppressed by design while a post-hoc
// probe read 0 and called it "a dead reclaim with NO other backend connected".
func TestOpenReclaimsAnAbandonedSchemaOnAnIdleDatabase(t *testing.T) {
	ctx := context.Background()
	dbname, dbDSN := createIdleTestDB(t, ctx, dsnOrSkip(t))

	own := "test_reach_own_p" + strconv.Itoa(os.Getpid())

	// reclaimAttempts bounds how many times the reclaim is driven, so that at
	// least one attempt observes a successful reclaim on a busy host.
	//
	// The verdict itself is NO LONGER probabilistic (card t_53b9b15f): a
	// surviving schema is judged against the connection reading open()'s prune
	// took its DECISION on (connectionSnapshot), not against a fresh count taken
	// after open() returned. If that reading saw another backend, the prune
	// suppressed the reclaim by design and no attempt-level failure is possible;
	// if it saw none, the reclaim is dead and this test fails. Repeating is only
	// here to guarantee the run contains at least one attempt that the prune was
	// left free to reclaim on — a host busy enough that EVERY attempt's prune saw
	// a visitor carries no verdict at all, and that vacuity is asserted below.
	const reclaimAttempts = 5

	type attemptOutcome struct {
		abandonedLeft bool
		legacyLeft    bool
		ownDropped    bool
		// snap is the connection reading open()'s prune took its DECISION on.
		// haveSnap is false when no prune recorded one, in which case this
		// attempt cannot take a verdict (see judgeSurvival).
		snap     connSnapshot
		haveSnap bool
	}

	var outcomes []attemptOutcome
	for attempt := 1; attempt <= reclaimAttempts; attempt++ {
		// Fresh fixtures per attempt: whether the reclaim happened is asked of
		// the database, so a schema an earlier attempt left behind would make
		// every later attempt read as a failure. plant's errors are fatal — a
		// fixture that cannot be planted must not be silently read as "the
		// schema is gone".
		abandoned, legacy := reclaimFixtureNames(attempt, deadPID(t))

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
		// Record the connection reading open()'s prune decided on. This is the
		// reading the attempt is judged against: a fresh one taken here could be
		// (and on a loaded host was) different, and judging against it is the
		// false-RED this guard used to carry.
		snap, haveSnap := connectionSnapshot()
		o.snap, o.haveSnap = snap, haveSnap
		if !haveSnap {
			t.Errorf("attempt %d: no prune recorded a connection reading — open() did not run pruneDeadSchemas, so this guard cannot judge the reclaim at all", attempt)
		}

		survived, dump, err := schemaSurvived(ctx, p, abandoned)
		if err != nil {
			t.Fatalf("probe %s: %v", abandoned, err)
		}
		o.abandonedLeft = survived
		// The verdict is taken on the PRUNE'S reading, never on the count taken
		// just now: `dead` is the direction that must never pass (the prune saw
		// nobody and left the schema), `suppressed` is the designed yield to a
		// backend the prune saw, and `unknown` is a prune that left no reading.
		dead, _, unknown := judgeSurvival(survived, snap, haveSnap)
		if dead {
			t.Errorf("attempt %d: open()'s prune ran with NO other backend connected and left %s behind — nothing could have suppressed the reclaim, so this is a dead reclaim. Check that open() still prunes BEFORE it creates the caller's pool (pgxpool is lazy, so pruning after pgxpool.NewWithConfig/Ping counts the caller's own backend as a peer and suppresses the reclaim forever). Reading the prune decided on: others=%d\n%s\nBackends at the probe (after open(), for context only — NOT the basis of this failure):\n%s",
				attempt, abandoned, snap.others, snap.dump, dump)
		}
		if unknown {
			t.Errorf("attempt %d: %s survived and the prune recorded no connection reading, so whether the reclaim was suppressed or dead cannot be decided on this attempt", attempt, abandoned)
		}

		legacySurvived, legacyDump, lerr := schemaSurvived(ctx, p, legacy)
		if lerr != nil {
			t.Fatalf("probe %s: %v", legacy, lerr)
		}
		o.legacyLeft = legacySurvived
		if legacyDead, _, _ := judgeSurvival(legacySurvived, snap, haveSnap); legacyDead {
			t.Errorf("attempt %d: open()'s prune ran with NO other backend connected and left the pre-pid schema %s behind — a name with no pid can never be attributed to a process, so it is reclaimable exactly when nobody else is connected, which is the reading the prune took. Reading the prune decided on: others=%d\n%s\nBackends at the probe (after open(), for context only):\n%s",
				attempt, legacy, snap.others, snap.dump, legacyDump)
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
	unknown := 0
	for i, o := range outcomes {
		switch {
		case !o.abandonedLeft:
			witnessed = true
		case !o.haveSnap:
			// No prune read was recorded, so this attempt is neither evidence of
			// a working nor a dead reclaim; the per-attempt error above already
			// reported it.
			unknown++
		case o.snap.others > 0:
			// The prune SAW another backend and issued no DDL — the designed
			// behaviour (`others > 0` => no DDL). This is a CORRECT yield, not a
			// failure, and it is judged on the reading the prune itself took.
			suppressed++
			t.Logf("attempt %d: reclaim suppressed while the prune saw %d backend(s) — by design (DROP SCHEMA is database-wide DDL); waiting for the run's verdict:\n%s",
				i+1, o.snap.others, o.snap.dump)
		}
		if !o.abandonedLeft && !o.legacyLeft {
			reclaimedLegacy = true
		}
		if o.ownDropped {
			t.Errorf("attempt %d: open() dropped this process's own schema %s — the prune must never reclaim the schema it is about to migrate and run against", i+1, own)
		}
	}
	if suppressed > 0 {
		t.Logf("note: %d/%d attempts were suppressed by a backend the prune saw; %d attempt(s) witnessed a successful reclaim",
			suppressed, len(outcomes), len(outcomes)-suppressed-unknown)
	}

	// The vacuity guard, and the assertion that gives the others their meaning:
	// if every attempt's prune saw a visitor (or recorded no reading) then
	// nothing was ever observed, and a report of "reclaim works" would be a
	// report of the guard proving nothing. It has to be a hard failure, not a
	// skip, because the direction it protects is the one that silently rots: a
	// reclaim that is dead would otherwise be indistinguishable from a busy host.
	// (A dead reclaim itself is already an unconditional per-attempt failure
	// above — this only catches the different case of a run with no verdict.)
	if !witnessed {
		t.Fatalf("open() never reclaimed an abandoned schema in %d attempts on a database whose prune saw nobody — either the reclaim is dead (every attempt's prune saw no backend and left the schema) or every attempt's prune saw a transient visitor, and this guard cannot tell the two apart", reclaimAttempts)
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

	// The reading open()'s prune took its DECISION on is the whole verdict here
	// (card t_53b9b15f): judging on a fresh count taken after open() returned is
	// what let a transient visitor red this guard for a working reclaim.
	snap, haveSnap := connectionSnapshot()

	survived, dump, err := schemaSurvived(ctx, p, abandoned)
	if err != nil {
		t.Fatalf("probe %s: %v", abandoned, err)
	}
	if !survived {
		return // the reclaim ran: exactly what this test requires.
	}
	if !haveSnap {
		t.Skipf("the abandoned schema %s survived but no prune recorded a connection reading (open() did not run pruneDeadSchemas), so this run cannot judge the reclaim", abandoned)
	}
	if snap.others > 0 {
		t.Skipf("the abandoned schema %s survived, but the prune SAW %d other backend(s) and suppressed the reclaim by design, so this run cannot say whether the reclaim runs unbidden — a skip is honest here where an Error would send a lane hunting a reclaim that may be fine. Reading the prune decided on:\n%s",
			abandoned, snap.others, snap.dump)
	}
	t.Errorf("open()'s prune ran with NO other backend connected and left the abandoned schema %s behind, and the reclaim is REQUIRED to run in that state — a killed run then leaks a schema per run. Check that open() still prunes BEFORE it creates the caller's pool (pgxpool is lazy, so pruning after pgxpool.NewWithConfig/Ping counts the caller's own backend as a peer and suppresses the reclaim forever). Reading the prune decided on: others=%d\nBackends at the probe (after open(), for context only):\n%s",
		abandoned, snap.others, dump)
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

// TestTheReclaimFixturesPlantBOTHShapes is the guard for card t_a6a23a03: the
// pid-qualified/dead-owner shape must still be PLANTED, not merely claimed.
//
// The defect was a naming accident with a silent effect. `schemaPID` is
// `LastIndex(schema, "_p")` and then Atoi of everything after it, so
// `test_reach_abandoned_p<deadpid>_a<N>` parses the tail `<deadpid>_a<N>` as a pid,
// FAILS, and the name is read as having no pid — the legacy shape. Both fixtures
// then planted the SAME shape and the abandoned direction stopped being observed on
// the live open() path, while every assertion still passed.
//
// This pins the two properties that make the fixtures exercise two different
// reclaim paths: the abandoned name must PARSE as pid-qualified (so the reclaim has
// an owner to check), and the legacy name must NOT (so it takes the unattributable
// branch). Asserted through `schemaPID` itself, which is the code that decides.
// reclaimFixtureNames returns the two schema names planted for ONE reclaim attempt.
//
// A function rather than two literals at the plant site, because the guard below
// has to assert the shape the fixture ACTUALLY uses: reconstructing the name in the
// guard would make it blind to a rename here, which is exactly the accident this
// exists to prevent (measured: with the names inlined, mutating the fixture back to
// the broken spelling left the guard green).
func reclaimFixtureNames(attempt, dead int) (abandoned, legacy string) {
	// The per-attempt suffix goes BEFORE the pid, never after it (card t_a6a23a03).
	// `schemaPID` is `LastIndex(schema, "_p")` then Atoi of EVERYTHING after it
	// (testdb.go), so `..._p999999_a1` parses the tail `999999_a1` as a pid, fails,
	// and the name is treated as having NO pid at all — i.e. as the LEGACY shape.
	// Both fixtures then planted the same shape, so the pid-qualified / dead-owner
	// direction stopped being observed on the live open() path.
	//
	// `test_reach_abandoned_a<N>_p<deadpid>`: the attempt number is part of the
	// package name, and the pid stays the LAST `_p` segment so it parses.
	return "test_reach_abandoned_a" + strconv.Itoa(attempt) + "_p" + strconv.Itoa(dead),
		"test_reach_legacy_a" + strconv.Itoa(attempt)
}

func TestTheReclaimFixturesPlantBOTHShapes(t *testing.T) {
	dead := deadPID(t)
	abandoned, legacy := reclaimFixtureNames(1, dead)

	pid, ok := schemaPID(abandoned)
	if !ok || pid != dead {
		t.Errorf("schemaPID(%q) = (%d, %v), want (%d, true) — the abandoned fixture must "+
			"parse as PID-QUALIFIED, or it takes the legacy branch and the dead-owner "+
			"direction stops being observed (t_a6a23a03)", abandoned, pid, ok, dead)
	}
	if _, ok := schemaPID(legacy); ok {
		t.Errorf("schemaPID(%q) parsed as pid-qualified, want false — the legacy fixture "+
			"must take the UNATTRIBUTABLE branch", legacy)
	}
	// And the OLD spelling, which is what the defect looked like, must NOT parse
	// as pid-qualified: if a future edit puts a suffix back after the pid, this
	// names the mistake instead of silently merging the two shapes.
	oldShape := "test_reach_abandoned_p" + strconv.Itoa(dead) + "_a1"
	if _, ok := schemaPID(oldShape); ok {
		t.Errorf("schemaPID(%q) parsed as pid-qualified, but schemaPID reads everything "+
			"after the LAST `_p` as the pid — a per-attempt suffix after the pid makes "+
			"the name read as LEGACY (t_a6a23a03)", oldShape)
	}
}
