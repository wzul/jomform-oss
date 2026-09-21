// Integration guard for the scratch-schema PRUNE, against a real Postgres.
//
// The unit tests above pin the delete RULE; this one pins the WIRING — that
// pruneDeadSchemas actually consults that rule and actually drops what it should.
// Without it the rule can be perfectly correct while prune ignores it (drop
// nothing and a crashed run leaks a schema forever; drop everything and it
// recreates the cross-checkout race the naming scheme exists to prevent).
package testdb

import (
	"context"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/wzul/jomform/api/internal/db"
)

// dsnOrSkip returns the integration DSN, skipping the test when it is unset so a
// unit-only run stays hermetic (same rule as Open).
func dsnOrSkip(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv(DSNEnv)
	if dsn == "" {
		t.Skipf("%s not set; skipping integration test", DSNEnv)
	}
	return dsn
}

// TestOwnerAliveFromErr pins the EPERM arm of the liveness probe. `kill(pid, 0)`
// returns EPERM when the process exists but is owned by another user; reading that
// as "dead" would drop a running peer's schema. This is tested through the error
// mapping because the syscall arm cannot be provoked from a single-user test run.
func TestOwnerAliveFromErr(t *testing.T) {
	if !ownerAliveFromErr(nil) {
		t.Error("ownerAliveFromErr(nil) = false — a successful probe means the process exists")
	}
	if !ownerAliveFromErr(syscall.EPERM) {
		t.Error("ownerAliveFromErr(EPERM) = false — EPERM means the process EXISTS but is owned by someone else; reading it as dead drops a live peer's schema")
	}
	if ownerAliveFromErr(syscall.ESRCH) {
		t.Error("ownerAliveFromErr(ESRCH) = true — ESRCH is 'no such process', which is exactly the reclaimable case")
	}
}

// fakeScratch drives pruneScratch without a database, so both directions of the
// delete rule can be asserted (the live-database test can only observe whichever
// answer the real host happens to give).
type fakeScratch struct {
	names   []string
	others  int
	dropped []string
}

func (f *fakeScratch) schemas(context.Context) ([]string, error) { return f.names, nil }
func (f *fakeScratch) otherConnections(context.Context) (int, error) {
	return f.others, nil
}
func (f *fakeScratch) dropSchema(_ context.Context, name string) error {
	f.dropped = append(f.dropped, name)
	return nil
}

// TestPruneScratchDropsOnlyReclaimableSchemas pins the WIRING, not just the rule:
// that pruneScratch consults reclaimableScratchSchema and drops exactly what it
// says. Without this, the rule can be perfectly correct while prune drops
// nothing (a killed run's schema leaks forever) or drops everything (which
// recreates the cross-checkout race the naming exists to prevent).
//
// The cases with `others > 0` all expect NOTHING to be dropped: the reclaim is
// `DROP SCHEMA ... CASCADE`, which is database-wide DDL, and issuing it while a
// peer is reading its own catalogue is the mechanism behind `could not open
// relation with OID (SQLSTATE XX000)`. Cleanup yields to a peer, always.
func TestPruneScratchDropsOnlyReclaimableSchemas(t *testing.T) {
	dead := "test_dbstore_p" + strconv.Itoa(deadPID(t))
	own := pkgName()

	for _, tc := range []struct {
		name      string
		names     []string
		others    int
		wantDrop  []string
		neverDrop []string
	}{
		{
			name:      "a peer is connected: no DDL at all, not even for an abandoned schema",
			names:     []string{dead, "test_dbstore", own},
			others:    3,
			wantDrop:  nil,
			neverDrop: []string{dead, "test_dbstore", own},
		},
		{
			name:      "alone: the abandoned schema is reclaimed and the live one is not",
			names:     []string{dead, own},
			others:    0,
			wantDrop:  []string{dead},
			neverDrop: []string{own},
		},
		{
			name:      "alone: the legacy name is reclaimed too",
			names:     []string{"test_dbstore", own},
			others:    0,
			wantDrop:  []string{"test_dbstore"},
			neverDrop: []string{own},
		},
		{
			name:      "alone: this process's own schema is still never dropped",
			names:     []string{own},
			others:    0,
			wantDrop:  nil,
			neverDrop: []string{own},
		},
		{
			name:      "a live peer's schema is never dropped even when alone",
			names:     []string{"test_dbstore_p1"},
			others:    0,
			wantDrop:  nil,
			neverDrop: []string{"test_dbstore_p1"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeScratch{names: tc.names, others: tc.others}
			got, err := pruneScratch(context.Background(), own, f)
			if err != nil {
				t.Fatalf("pruneScratch: %v", err)
			}
			if strings.Join(got, ",") != strings.Join(tc.wantDrop, ",") {
				t.Errorf("pruneScratch dropped %v, want %v", got, tc.wantDrop)
			}
			for _, n := range tc.neverDrop {
				for _, d := range f.dropped {
					if d == n {
						t.Errorf("pruneScratch DROPPED %q, which must never be dropped in this case", n)
					}
				}
			}
		})
	}
}

// TestPruneDeadSchemasIsWiredIntoOpen pins that the cleanup is actually CALLED:
// prune can be flawless and still never run, and then a killed run's schema
// accumulates forever. Asserted on the call site rather than by observing the
// database, because the live rule is deliberately conservative and a passing
// live run proves nothing about whether it executed.
func TestPruneDeadSchemasIsWiredIntoOpen(t *testing.T) {
	src, err := os.ReadFile("testdb.go")
	if err != nil {
		t.Fatalf("read testdb.go: %v", err)
	}
	if !strings.Contains(string(src), "pruneDeadSchemas(ctx, admin, schema)") {
		t.Error("open() no longer calls pruneDeadSchemas — leftover scratch schemas from killed runs would accumulate forever")
	}
}

// TestPruneDeadSchemasDropsOnlyAbandonedScratchSchemas drives pruneDeadSchemas
// against the real test database while a PEER connection is open.
//
// The expectation is that it drops NOTHING: the reclaim is `DROP SCHEMA ...
// CASCADE`, database-wide DDL, and issuing it while a peer is alive is the
// mechanism behind a peer's `could not open relation with OID (SQLSTATE XX000)`.
// Cleanup must yield to a peer unconditionally — a leaked empty schema costs
// nothing, a peer's gate run costs 11 minutes and a false regression report.
// Everything planted here must still exist afterwards.
func TestPruneDeadSchemasDropsOnlyAbandonedScratchSchemas(t *testing.T) {
	ctx := context.Background()
	admin, err := db.Connect(ctx, dsnOrSkip(t))
	if err != nil {
		t.Fatalf("admin connect: %v", err)
	}
	defer admin.Close()

	// Process-unique tails: two concurrent runs of this test must not clobber
	// each other's fixtures — that is the exact defect under fix here, and a
	// guard that reintroduced it would be worse than no guard. The legacy tail
	// deliberately is not a usable pid, so it still exercises the
	// "cannot be attributed to a process" rule.
	tail := strconv.Itoa(os.Getpid()) + "x"
	abandoned := "test_pruneprobe_p" + strconv.Itoa(deadPID(t))
	own := pkgName()
	legacy := "test_pruneprobe_legacy_" + tail
	notScratch := "nonscratch_probe_" + tail
	all := []string{abandoned, own, legacy, notScratch}

	drop := func(name string) {
		_, _ = admin.Exec(ctx, `DROP SCHEMA IF EXISTS `+quote(name)+` CASCADE`)
	}
	create := func(name string) {
		// IF NOT EXISTS: a peer lane running this same test must not fail on our
		// fixture, and the drops above already made the state deterministic.
		if _, err := admin.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS `+quote(name)); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	for _, n := range all {
		drop(n)
		create(n)
		defer drop(n)
	}

	// A peer connection, so "am I alone?" is false — the state a gated run is
	// actually in, and the answer we want tested.
	peer, err := db.Connect(ctx, dsnOrSkip(t))
	if err != nil {
		t.Fatalf("peer connect: %v", err)
	}
	defer peer.Close()
	if _, err := peer.Exec(ctx, `SELECT 1`); err != nil {
		t.Fatalf("peer probe: %v", err)
	}

	pruneDeadSchemas(ctx, admin, own)

	exists := func(name string) bool {
		var n int
		if err := admin.QueryRow(ctx,
			`SELECT count(*) FROM pg_namespace WHERE nspname = $1`, name).Scan(&n); err != nil {
			t.Fatalf("probe %s: %v", name, err)
		}
		return n > 0
	}

	for _, n := range all {
		if !exists(n) {
			t.Errorf("pruneDeadSchemas dropped %s while a peer backend was connected — cleanup must issue NO DDL with a peer alive (DROP SCHEMA is database-wide and the mechanism behind a peer's XX000), and it must never drop this process's own schema at all", n)
		}
	}
}
