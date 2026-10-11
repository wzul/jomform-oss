// Package testdb — unit guards for the throwaway-database RECLAIM rule
// (scratchdb.go), plus the wiring that puts the rule in the prune's path.
//
// Why a fake driver is not optional here: the arm that matters most is "a
// database a peer run is using must never be reclaimed", and on a live host
// that state cannot be planted on demand — a candidate is either connected or
// it is not, and the test cannot make a peer's connection appear at the instant
// the prune decides. Driving pruneScratchDatabases with planted answers is what
// lets both directions be asserted instead of hoped for.
package testdb

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestReclaimableScratchDatabasePinsBothDirections is the rule's guard, and it
// is written as a table of the DECISIONS rather than of the code paths: for
// every shape the card records (a leaked family, a live owner, a peer's
// database, the shared database, a name nothing can attribute), it pins whether
// the database is reclaimed and — when it is not — that the reason names the
// real cause.
//
// The consequence of getting either direction wrong is asymmetric and both are
// expensive: reclaim a database a peer is using and that lane's gate goes red
// for a diff that never touched it (and the failure is irreversible), while
// skip a leaked one and the disk fills up again with 4.5 GB of scratch
// databases no run will ever clean.
func TestReclaimableScratchDatabasePinsBothDirections(t *testing.T) {
	const shared = "jomform_test"
	dead := deadPID(t)
	live := os.Getpid()
	now := time.Now()
	// The protect set a live prune really carries: the DSN's database and the
	// database the connection is in.
	protected := []string{shared, shared}

	for _, tc := range []struct {
		name       string
		cand       scratchDBCandidate
		protect    []string
		want       bool
		wantReason string // substring of the reason when want is false
	}{
		{
			// The `jomform_test` family prefix matches the shared database's
			// own name, so this exclusion is load-bearing, not decorative.
			name:       "the shared test database is never a candidate, though a family prefix matches its name",
			cand:       scratchDBCandidate{name: shared, created: now.Add(-72 * time.Hour), haveCreated: true},
			protect:    protected,
			want:       false,
			wantReason: "shared test database",
		},
		{
			// The regression that destroyed this host's shared database while
			// this card was being built: the prune ran from a THROWAWAY
			// database's connection (open() on a fixture), so the connection's
			// own database was the fixture and nothing else excluded the
			// suite's shared one — which is old, unattributable and, at that
			// instant, unconnected. Every later test died with 3D000.
			name:       "the shared database is protected from a connection to another database",
			cand:       scratchDBCandidate{name: shared, created: now.Add(-72 * time.Hour), haveCreated: true},
			protect:    []string{"jomform_migtest_9f3a2b_p" + strconv.Itoa(live), shared},
			want:       false,
			wantReason: "shared test database",
		},
		{
			name:       "the database this connection is in is never a candidate",
			cand:       scratchDBCandidate{name: "jomform_reachtest_9f3a2b", created: now.Add(-72 * time.Hour), haveCreated: true},
			protect:    []string{"jomform_reachtest_9f3a2b", shared},
			want:       false,
			wantReason: "database the suite runs in",
		},
		{
			name:       "an empty protect entry protects nothing (an unset DSN must not match every name)",
			cand:       scratchDBCandidate{name: "jomform_migtest_9f3a2b_p" + strconv.Itoa(dead)},
			protect:    []string{""},
			want:       true,
			wantReason: "",
		},
		{
			name:       "postgres is never reclaimed even when nothing is connected",
			cand:       scratchDBCandidate{name: "postgres", created: now.Add(-72 * time.Hour), haveCreated: true},
			want:       false,
			wantReason: "cluster-reserved",
		},
		{
			name:       "template0 is never reclaimed (it is not a family member, but the rule must not depend on that)",
			cand:       scratchDBCandidate{name: "template0", created: now.Add(-72 * time.Hour), haveCreated: true},
			want:       false,
			wantReason: "cluster-reserved",
		},
		{
			name:       "a database outside every family is left alone",
			cand:       scratchDBCandidate{name: "jomform_prod", created: now.Add(-72 * time.Hour), haveCreated: true},
			want:       false,
			wantReason: "no throwaway-database family",
		},
		{
			name:       "a database whose name only looks like a family (no trailing underscore) is left alone",
			cand:       scratchDBCandidate{name: "jfprod", created: now.Add(-72 * time.Hour), haveCreated: true},
			want:       false,
			wantReason: "no throwaway-database family",
		},
		{
			name: "a leaked database whose creating process is gone is reclaimed — the whole point of the card",
			cand: scratchDBCandidate{name: "jomform_migtest_9f3a2b_p" + strconv.Itoa(dead)},
			want: true,
		},
		{
			name:       "the same name while its creating process is alive is never reclaimed",
			cand:       scratchDBCandidate{name: "jomform_migtest_9f3a2b_p" + strconv.Itoa(live)},
			want:       false,
			wantReason: "still exists",
		},
		{
			// Connection first, attribution second: a name whose owner is gone
			// is still left alone while a backend is on it (that is the state
			// a guard that connected to a leaked database is in).
			name:       "a database with a connected backend is never reclaimed, even with no live owner",
			cand:       scratchDBCandidate{name: "jomform_migtest_9f3a2b_p" + strconv.Itoa(dead), backends: 1},
			want:       false,
			wantReason: "connected",
		},
		{
			name:       "a live owner with a connected backend reports the connection, not the pid",
			cand:       scratchDBCandidate{name: "jomform_migtest_9f3a2b_p" + strconv.Itoa(live), backends: 2},
			want:       false,
			wantReason: "connected",
		},
		{
			// The window this protects: a creator that has issued its create
			// and not yet opened its pool (pgxpool is lazy) has NO backend on
			// the database, and looks exactly like an abandoned one.
			name:       "an unattributable name younger than the grace period is never reclaimed",
			cand:       scratchDBCandidate{name: "jomform_migtest_9f3a2b", created: now.Add(-2 * time.Minute), haveCreated: true},
			want:       false,
			wantReason: "grace period",
		},
		{
			name: "an unattributable name older than the grace period is reclaimed",
			cand: scratchDBCandidate{name: "jomform_migtest_9f3a2b", created: now.Add(-6 * time.Hour), haveCreated: true},
			want: true,
		},
		{
			// A creation time in the future means the clock is not to be
			// trusted; reading it as "old" is the direction that drops a
			// database a run is about to use.
			name:       "an unattributable name with a future creation time is treated as young",
			cand:       scratchDBCandidate{name: "jomform_migtest_9f3a2b", created: now.Add(time.Hour), haveCreated: true},
			want:       false,
			wantReason: "grace period",
		},
		{
			name:       "an unattributable name whose age could not be read is never reclaimed",
			cand:       scratchDBCandidate{name: "jomform_migtest_9f3a2b"},
			want:       false,
			wantReason: "creation time could not be read",
		},
		{
			name: "a measured legacy family is reclaimed too — a prune that knows one prefix leaves the rest behind",
			cand: scratchDBCandidate{name: "jf_qa_t_abc123", created: now.Add(-30 * time.Hour), haveCreated: true},
			want: true,
		},
		{
			name: "another measured legacy family is reclaimed",
			cand: scratchDBCandidate{name: "repro_9f3a2b", created: now.Add(-30 * time.Hour), haveCreated: true},
			want: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Cases that do not name a protect set are driven with the set a
			// live prune carries, so the table reads like production.
			protect := tc.protect
			if protect == nil {
				protect = protected
			}
			got, why := reclaimableScratchDatabase(tc.cand, protect)
			if got != tc.want {
				t.Fatalf("reclaimableScratchDatabase(%s) = %v (%s), want %v\n\n"+
					"Reclaiming a database a peer may be using costs that lane its run — the\n"+
					"reclaim is irreversible. Leaving an abandoned one behind is the leak this\n"+
					"prune exists to end. Both directions are in the table; check which one this is.",
					tc.cand.name, got, why, tc.want)
			}
			if !tc.want && !strings.Contains(why, tc.wantReason) {
				t.Errorf("reclaimableScratchDatabase(%s) refused for %q, want a reason naming %q — "+
					"a refusal whose reason does not name the real cause sends the next reader "+
					"hunting the wrong arm", tc.cand.name, why, tc.wantReason)
			}
			if tc.want && why != "" {
				t.Errorf("reclaimableScratchDatabase(%s) reclaimed it and still gave a reason (%q); "+
					"a reclaim must be unconditional", tc.cand.name, why)
			}
		})
	}
}

// TestEveryInTreeFamilyIsAttributable pins the shape that makes the reclaim
// immediate rather than clock-based: a database created by a fixture in THIS
// tree ends with the creating pid, exactly as a scratch schema does. Without it
// every leftover database has to wait out the grace period, and the arm that
// protects a live run has nothing to read.
func TestEveryInTreeFamilyIsAttributable(t *testing.T) {
	for _, f := range scratchDBFamilies {
		if f.createdBy == "" {
			continue
		}
		// The fixture's own naming: <family><random>_p<pid>.
		sample := f.prefix + "9f3a2b" + "_p" + strconv.Itoa(os.Getpid())
		pid, ok := schemaPID(sample)
		if !ok || pid != os.Getpid() {
			t.Errorf("schemaPID(%q) = (%d, %v), want (%d, true) — the naming convention scratchdb.go "+
				"attributes on", sample, pid, ok, os.Getpid())
		}
		if live := liveScratchSchema(sample); !live {
			t.Errorf("liveScratchSchema(%q) = false, but it carries THIS process's pid — a database this "+
				"process just created would be reclaimed out from under it", sample)
		}
	}
}

// TestTheSharedTestDatabaseIsNeverReclaimed drives the rule with the DSN's real
// database name rather than the literal the fixture is allowed to use: the DSN
// is configuration, and the exclusion has to hold for whatever name it holds.
func TestTheSharedTestDatabaseIsNeverReclaimed(t *testing.T) {
	dsn := os.Getenv(DSNEnv)
	if dsn == "" {
		t.Skipf("%s not set; skipping", DSNEnv)
	}
	shared := sharedDatabaseName(t, dsn)
	if shared == "" {
		t.Fatal("the DSN names no database — the exclusion this rule makes would have nothing to match")
	}
	if !strings.HasPrefix(shared, "jomform_test") {
		t.Logf("note: the DSN's database is %q, not the `jomform_test` this board uses; the exclusion is "+
			"being asserted on the configured name, which is the point", shared)
	}
	// Old enough and connected to nothing: every OTHER arm would reclaim it.
	got, why := reclaimableScratchDatabase(scratchDBCandidate{
		name:        shared,
		created:     time.Now().Add(-365 * 24 * time.Hour),
		haveCreated: true,
	}, []string{"jomform_reachtest_9f3a2b", shared})
	if got {
		t.Fatalf("the shared test database %q (the name %s points at) is reclaimable — the prune would "+
			"DROP the database the whole suite runs in", shared, DSNEnv)
	}
	if !strings.Contains(why, "shared test database") {
		t.Errorf("refused %q for %q, want the reason to name the shared test database", shared, why)
	}
}

// sharedDatabaseName returns the database name the integration DSN points at.
// The rule's exclusion is asserted on THIS rather than on the literal the other
// guards use, because the name is configuration: whatever the DSN says is the
// database the suite runs in, and that is the one the prune must never touch.
func sharedDatabaseName(t *testing.T, dsn string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse %s: %v", DSNEnv, err)
	}
	return strings.TrimPrefix(u.Path, "/")
}

// fakeScratchDBs drives pruneScratchDatabases without a cluster, so the WIRING
// can be asserted: that the rule is consulted for every candidate, that a
// refusal is honoured, and that a drop which fails is recorded rather than
// reported as a reclaim.
type fakeScratchDBs struct {
	// currentDB is the database this fake "connection" is in.
	currentDB string
	cands     []scratchDBCandidate
	// dropErr makes the drop of a named database fail, standing in for the
	// race a peer's new connection wins.
	dropErr map[string]error
	dropped []string
}

func (f *fakeScratchDBs) currentDatabase(context.Context) (string, error) { return f.currentDB, nil }
func (f *fakeScratchDBs) candidates(context.Context) ([]scratchDBCandidate, error) {
	return f.cands, nil
}
func (f *fakeScratchDBs) dropDatabase(_ context.Context, name string) error {
	if err := f.dropErr[name]; err != nil {
		return err
	}
	f.dropped = append(f.dropped, name)
	return nil
}

func TestPruneScratchDatabasesDropsExactlyTheReclaimable(t *testing.T) {
	dead := deadPID(t)
	leaked := "jomform_migtest_9f3a2b_p" + strconv.Itoa(dead)
	legacy := "jf_qa_t_abc123"
	peer := "jomform_reachtest_9f3a2b_p" + strconv.Itoa(dead)
	shared := "jomform_test"
	unrelated := "jomform_prod"

	f := &fakeScratchDBs{
		currentDB: "jomform_test",
		cands: []scratchDBCandidate{
			{name: leaked, size: 8 << 20},
			{name: legacy, size: 12 << 20, created: time.Now().Add(-2 * time.Hour), haveCreated: true},
			{name: peer, size: 4 << 20, backends: 1},
			{name: shared, size: 120 << 20, created: time.Now().Add(-48 * time.Hour), haveCreated: true},
			{name: unrelated, size: 1 << 20, created: time.Now().Add(-48 * time.Hour), haveCreated: true},
		},
	}

	// The protect set the live caller builds: the database the DSN names. The
	// fake's own connection is already in the shared database here, which is
	// exactly the case where that protection alone would look sufficient — see
	// the throwaway-connection guard below for the case where it is not.
	res, err := pruneScratchDatabases(context.Background(), f, shared)
	if err != nil {
		t.Fatalf("pruneScratchDatabases: %v", err)
	}
	if strings.Join(res.protected, ",") != strings.Join([]string{shared, shared}, ",") {
		t.Errorf("prune protected %v, want the connection's database AND the DSN's name (%v)", res.protected, []string{shared, shared})
	}
	if strings.Join(res.dropped, ",") != strings.Join([]string{leaked, legacy}, ",") {
		t.Fatalf("prune dropped %v, want exactly the abandoned databases %v — every other candidate is "+
			"one a peer or the suite itself is using", res.dropped, []string{leaked, legacy})
	}
	if res.scanned != len(f.cands) {
		t.Errorf("prune scanned %d candidates, want %d", res.scanned, len(f.cands))
	}
	if got, want := res.bytes, int64((8<<20)+(12<<20)); got != want {
		t.Errorf("prune reported %d reclaimed bytes, want %d (the sizes of the databases it dropped)", got, want)
	}
	for _, must := range []string{shared, peer, unrelated} {
		for _, d := range f.dropped {
			if d == must {
				t.Errorf("prune DROPPED %q, which the rule refuses", must)
			}
		}
	}
	// The kept list is the audit trail: every refusal has to say why.
	for _, must := range []string{shared, peer, unrelated} {
		found := false
		for _, k := range res.kept {
			if strings.HasPrefix(k, must+": ") && len(k) > len(must)+2 {
				found = true
			}
		}
		if !found {
			t.Errorf("prune recorded no reason for keeping %q; kept=%v", must, res.kept)
		}
	}
}

// TestPruneScratchDatabasesHonoursARefusedDrop covers the race the rule yields
// to: the drop is issued after the count, and a peer that connects in between
// makes `DROP DATABASE` fail. That failure must be recorded as a refusal — not
// counted as a reclaim, and never escalated into an error that would fail the
// test about to run.
func TestPruneScratchDatabasesHonoursARefusedDrop(t *testing.T) {
	dead := deadPID(t)
	leaked := "jomform_migtest_9f3a2b_p" + strconv.Itoa(dead)
	f := &fakeScratchDBs{
		currentDB: "jomform_test",
		cands:     []scratchDBCandidate{{name: leaked, size: 8 << 20}},
		dropErr:   map[string]error{leaked: errors.New("database is being accessed by other users")},
	}

	res, err := pruneScratchDatabases(context.Background(), f, "jomform_test")
	if err != nil {
		t.Fatalf("pruneScratchDatabases: %v (a refused drop must be best-effort, not fatal)", err)
	}
	if len(res.dropped) != 0 {
		t.Errorf("prune reported %v as reclaimed although the drop failed", res.dropped)
	}
	if res.bytes != 0 {
		t.Errorf("prune reported %d reclaimed bytes although nothing was dropped", res.bytes)
	}
	joined := strings.Join(res.kept, "\n")
	if !strings.Contains(joined, leaked) || !strings.Contains(joined, "drop refused") {
		t.Errorf("the refused drop is not recorded in kept=%v — an operator reading the log would believe "+
			"nothing was attempted", res.kept)
	}
}

// TestPruneDeadDatabasesIsWiredIntoOpen pins that the cleanup is actually
// CALLED. A reclaim can be flawless and never run, and then every killed run
// leaks a database forever — which is exactly the state the card measured. It
// is asserted on the call site because the live rule is deliberately
// conservative: a run that reclaims nothing and a run that never ran look
// identical from the outside.
func TestPruneDeadDatabasesIsWiredIntoOpen(t *testing.T) {
	src, err := os.ReadFile("testdb.go")
	if err != nil {
		t.Fatalf("read testdb.go: %v", err)
	}
	if !strings.Contains(string(src), "pruneDeadDatabases(ctx, admin)") {
		t.Error("open() no longer calls pruneDeadDatabases — leftover throwaway databases from killed " +
			"runs would accumulate forever (measured: 273 databases / 4506 MB on the shared test host)")
	}
}
