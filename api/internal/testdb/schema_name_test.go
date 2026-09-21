// Scratch-schema naming guard.
//
// Open() names each test process's schema `test_<pkg>_p<pid>`. The pid part is
// what keeps two checkouts running the SAME package at the same time from
// dropping and re-creating each other's schema mid-test — the failure shape is
// `relation "accounts" does not exist (SQLSTATE 42P01)` or a peer's rows
// surviving into a schema that was just re-created, across tests in code the
// diff never touched. These tests pin the naming rules without needing a second
// process.
package testdb

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// TestPkgNameCarriesThisProcess pins the property that isolates concurrent runs:
// the schema name must contain this process's pid, so two `go test` binaries for
// the same package never resolve to the same schema.
func TestPkgNameCarriesThisProcess(t *testing.T) {
	got := pkgName()
	want := "_p" + strconv.Itoa(os.Getpid())
	if !strings.HasPrefix(got, "test_") {
		t.Fatalf("pkgName() = %q, want the test_<pkg>_p<pid> shape", got)
	}
	if !strings.HasSuffix(got, want) {
		t.Fatalf("pkgName() = %q, want it to end in %q so a concurrent run of the same package gets its own schema", got, want)
	}
	pid, ok := schemaPID(got)
	if !ok || pid != os.Getpid() {
		t.Fatalf("schemaPID(%q) = (%d, %v), want this process's pid %d", got, pid, ok, os.Getpid())
	}
}

// TestPkgNameKeepsThePackageSeparator pins the other half of the name: packages
// `go test ./...` runs in parallel share the database, so the per-package
// dimension must survive alongside the pid.
func TestPkgNameKeepsThePackageSeparator(t *testing.T) {
	got := pkgName()
	if got != "test_"+sanitize(pkgFromFrames())+"_p"+strconv.Itoa(os.Getpid()) {
		t.Fatalf("pkgName() = %q, want test_<pkg>_p<pid> (package part, then pid)", got)
	}
	// Guard the shape a naive "just append the pid" edit would break: the name
	// must not collapse to two bare pieces.
	if strings.Count(got, "_p") < 1 || !strings.Contains(got[:len(got)-len(strconv.Itoa(os.Getpid()))], "test_") {
		t.Fatalf("pkgName() = %q, want the test_<pkg> prefix preserved in front of the pid", got)
	}
}

// TestSchemaPIDRejectsLegacyAndMalformedNames keeps the prune path conservative.
// A name without a pid suffix predates this naming scheme, so it must NOT be
// parsed as one (and liveScratchSchema must therefore report it live rather than
// claim it is abandoned).
func TestSchemaPIDRejectsLegacyAndMalformedNames(t *testing.T) {
	for _, name := range []string{
		"test_httpapi",   // legacy: no pid suffix
		"test_httpapi_p", // empty pid
		"test_httpapi_px",
		"test_httpapi_p0", // pid 0 is never a real process
		"test_httpapi_p-9",
	} {
		if _, ok := schemaPID(name); ok {
			t.Errorf("schemaPID(%q) parsed a pid from a name that has none", name)
		}
		if !liveScratchSchema(name) {
			t.Errorf("liveScratchSchema(%q) = false — a name we cannot attribute must not be reported as abandoned", name)
		}
	}
}

// TestReclaimableScratchSchemaIsConservative states the delete rule directly.
// The load-bearing property is the "never" direction: no schema is reclaimed
// while ANY other connection exists, because the reclaim is `DROP SCHEMA ...
// CASCADE` — database-wide DDL that invalidates catalogue entries a peer may be
// reading (observed as `could not open relation with OID (SQLSTATE XX000)` in a
// peer's pg_indexes scan). A leaked empty schema costs nothing; a peer's gate
// run costs 11 minutes and a false regression report.
func TestReclaimableScratchSchemaIsConservative(t *testing.T) {
	live := pkgName() // this process's own schema
	legacy := "test_httpapi"
	dead := "test_httpapi_p" + strconv.Itoa(deadPID(t))

	for _, tc := range []struct {
		name string
		solo bool
		want bool
	}{
		{name: live, solo: true, want: false},              // ours, even alone
		{name: live, solo: false, want: false},             // ours, with peers
		{name: "test_httpapi_p1", solo: true, want: false}, // pid 1 (init) is alive
		{name: legacy, solo: false, want: false},           // unattributable, a peer may be using it
		{name: legacy, solo: true, want: true},             // unattributable, nobody else connected
		{name: dead, solo: false, want: false},             // owner gone, but a peer is reading: no DDL
		{name: dead, solo: true, want: true},               // owner gone and nobody else connected
	} {
		if got := reclaimableScratchSchema(tc.name, tc.solo); got != tc.want {
			t.Errorf("reclaimableScratchSchema(%q, solo=%v) = %v, want %v", tc.name, tc.solo, got, tc.want)
		}
	}
}

// deadPID returns a pid that is guaranteed no longer to be a live process: it
// comes from a child this test started and reaped, so the kernel cannot hand
// that pid to the concurrent run in between (pid allocation is sequential —
// reuse requires a full wraparound).
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Skipf("cannot spawn a short-lived process to obtain a dead pid: %v", err)
	}
	if liveScratchSchema("test_x_p" + strconv.Itoa(cmd.Process.Pid)) {
		t.Fatalf("the pid of a process this test just reaped (%d) still probes as live — the dead-pid cases below would not test anything", cmd.Process.Pid)
	}
	return cmd.Process.Pid
}

// TestLiveScratchSchemaProbesTheOwner: a schema owned by a live process is kept
// (dropping it is exactly the mid-test race), and one whose owner has exited is
// reclaimable.
func TestLiveScratchSchemaProbesTheOwner(t *testing.T) {
	live := pkgName()
	if !liveScratchSchema(live) {
		t.Fatalf("liveScratchSchema(%q) = false for THIS process's own schema", live)
	}

	dead := "test_httpapi_p" + strconv.Itoa(deadPID(t))
	if liveScratchSchema(dead) {
		t.Fatalf("liveScratchSchema(%q) = true, but its owner has exited — its schema would accumulate forever", dead)
	}
}
