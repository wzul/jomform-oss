// Sharded execution of this package's test suite (card t_0d0af849).
//
// THE COST. `go test ./internal/httpapi -race` is the single most expensive
// package in the module: 1283 top-level tests, 490.5s of test time, and it sits
// in 16 of the 17 affected-package sets the merge train gates (measured over the
// last 20 api merges), so every merge paid it serially behind the gate lock.
//
// THE MEASUREMENT THAT CHOSE THIS FIX. A `-cpuprofile` run of the whole package
// on this host: 527.7s wall, 315.1s user + 136.6s system = 451s of CPU, i.e.
// **0.81 of ONE core**. 62% of the samples are the race detector's own
// `__tsan_read` (144s), `racecall` (55s), `__tsan_write` (30s) and
// `MemoryAccessRangeT` (18s); the rest is argon2id password hashing (~40s),
// `disintegration/imaging` resizes (~27s), flate/png (~20s) and go/parser
// (~10s). None of that is duplicated work to delete — the assertions are the
// assertions — and the suite is SERIAL, so the lever is processes, not
// micro-optimisation.
//
// MEASURED, same tree, back to back, host load 9-14 (a peer lane gating at the
// same time), package elapsed as `go test` reports it:
//
//	1 process  515.5s  (82% of one core)
//	3 processes 183.5s  (220%)
//	4 processes 159.2s  (251%)   <- the default
//	6 processes 148.9s  (280%)
//
// Four is the knee: two more processes buy 10s and cost 30% more CPU plus two
// more scratch schemas. A merge batch therefore pays ~2.6 minutes for this
// package instead of ~8.6.
//
// WHY THE COUNT IS THE MEASURED KNEE, NOT `GOMAXPROCS-2`. Both gated paths run
// the preflight under a CPU quota (`deploy/merge-train.sh` TRAIN_CPU_QUOTA and
// `deploy/build-and-ship.sh` GATE_CPU_QUOTA, 300% each), and Go 1.27 reads
// cgroup v2's cpu.max, so inside that scope runtime.GOMAXPROCS(0) is 3 on this
// 6-core host — measured 6 uncapped, 3 at CPUQuota=300%, 4 at CPUQuota=400%. A
// `GOMAXPROCS-2` derivation therefore returned 1 under exactly the quota this
// sharding exists to beat, and runShardedTests returned without a word: the
// gate ran the serial suite (measured on this host, same tree, same flags:
// 198.8s uncapped with four shard processes, 476.2s under CPUQuota=300% with
// no shard line at all). The count no longer reads GOMAXPROCS — the 251% the
// four shards draw fits inside the 300% quota with room for the sibling
// packages — and testShardCountFor keeps that decision assertable in a unit
// test, fed the quota's CPU count rather than needing a real cgroup.
//
// WHAT THIS DOES. TestMain re-executes this same test binary as N children, each
// handed a disjoint `-test.run` regex, and waits for all of them. A fresh
// process is exactly the isolation the package's fixtures already assume:
//
//   - testdb gives each PROCESS its own `test_<pkg>_p<pid>` schema, dropped and
//     re-migrated at entry (`internal/testdb`);
//   - the session/trust fixtures namespace their Valkey keys by pid
//     (`jftest:p<pid>`), and TestMain sweeps exactly that namespace at exit;
//   - testIP hands each process its own address block, so login-counter
//     reclaims cannot cross processes.
//
// So two shards cannot see each other's rows or keys, by the same mechanism that
// already lets two checkouts gate at once.
//
// NOTHING IS DROPPED. The children between them receive every test the run would
// otherwise have executed — the enumeration is `-test.list` over the caller's OWN
// `-test.run` pattern, and the partition is a complete, disjoint hash split of
// that list (TestShardPartitionCoversEveryListedTestExactlyOnce asserts the
// property on the live list). A child that fails makes the package fail; a child
// that cannot start is a failure, never a skip.
//
// WHEN IT DOES NOT SHARD. Every one of these falls back to today's single
// process, so the worst outcome of a bug here is the OLD runtime — never a
// silent subset:
//
//   - JOMFORM_HTTPAPI_TEST_SHARDS is 0 or 1, this process may use only one CPU,
//     or the run is already a shard child;
//   - a profile/coverage flag is present (`-test.cpuprofile`, `-test.memprofile`,
//     `-test.blockprofile`, `-test.mutexprofile`, `-test.trace`,
//     `-test.coverprofile`, `-test.gocoverdir`): every child would write the same
//     file and the last one would win;
//   - `go test` asked for a CACHE RECORD (`-test.testlogfile`): that record must
//     describe the whole package, and merging per-shard logs means depending on
//     an undocumented format — a wrong cache hit is far worse than a slow run.
//     The gate passes `-count=1`, which is the run whose latency matters, and
//     that run carries no testlogfile;
//   - the list cannot be enumerated, or it is shorter than testShardFloor (an
//     enumeration that silently lost most of the suite must not be sharded).
//
// TUNING. JOMFORM_HTTPAPI_TEST_SHARDS overrides the count (0/1 disables). The
// default is the measured knee (four), not "cores minus two": under a CPU quota
// GOMAXPROCS already means "cores this cgroup may use", so subtracting headroom
// from it walks the count to 1 exactly where the gates run (GOMAXPROCS=3 at
// CPUQuota=300%). Four shards at 251% of a core is what fits inside that 300%
// alongside the sibling packages `go test ./...` is running at the same time.
package httpapi

import (
	"bytes"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
)

const (
	// testShardsEnv overrides the shard count. "0" and "1" disable sharding.
	testShardsEnv = "JOMFORM_HTTPAPI_TEST_SHARDS"

	// testShardChildEnv marks a process this runner started. A child never
	// shards again, which is what keeps the recursion to one level.
	testShardChildEnv = "JOMFORM_HTTPAPI_TEST_SHARD_CHILD"

	// testShardDefault caps the automatic count: four children of a
	// race-instrumented suite is ~4 of this host's 6 cores, and the gate needs
	// the rest for the other packages in the inventory.
	testShardDefault = 4

	// testShardFloor is the shortest list worth sharding. Below it the count is
	// almost certainly an enumeration failure rather than a filtered run (a
	// caller's own `-test.run` on a handful of tests is a legitimate short
	// list, and sharding it buys nothing anyway).
	testShardFloor = 100
)

// runShardedTests runs the suite as N concurrent child processes. It reports
// whether it handled the run at all: (0, false) means "no sharding happened, the
// caller must run m.Run() exactly as before".
func runShardedTests() (int, bool) {
	n := testShardCount()
	if n < 2 {
		// Never silent: this branch is what hid the defect this card fixes. A
		// `GOMAXPROCS-2` derivation returned 1 inside the gates' CPUQuota=300%
		// scope, and because nothing was printed the gate looked sharded while
		// running the serial suite (476.2s instead of 198.8s).
		shardLogf("running as one process: %d shard(s) resolved, sharding needs at least 2 (%s=%q, GOMAXPROCS=%d)",
			n, testShardsEnv, os.Getenv(testShardsEnv), runtime.GOMAXPROCS(0))
		return 0, false
	}
	if os.Getenv(testShardChildEnv) != "" {
		return 0, false
	}
	if flag, why := shardingForbidden(os.Args[1:]); flag != "" {
		shardLogf("running as one process: %s", why)
		return 0, false
	}

	runFilter, runFilterErr := testRunFilter(os.Args[1:])
	if runFilterErr != nil {
		shardLogf("running as one process: %v", runFilterErr)
		return 0, false
	}
	names, err := enumerateTests(runFilter)
	if err != nil {
		shardLogf("running as one process: cannot enumerate this package's tests: %v", err)
		return 0, false
	}
	if len(names) < testShardFloor {
		shardLogf("running as one process: only %d test(s) matched, below the shard floor of %d", len(names), testShardFloor)
		return 0, false
	}

	buckets := shardBuckets(names, n)
	exe, err := os.Executable()
	if err != nil {
		shardLogf("running as one process: cannot resolve the test binary: %v", err)
		return 0, false
	}

	cmds := make([]*exec.Cmd, 0, len(buckets))
	for _, bucket := range buckets {
		// A shard with no tests is not started: an empty `-test.run=^()$` would
		// only pay process startup and schema migration for nothing.
		if len(bucket) == 0 {
			continue
		}
		cmd := exec.Command(exe, shardChildArgs(os.Args[1:], bucket)...)
		cmd.Env = append(os.Environ(), testShardChildEnv+"=1")
		// The children share this process's stdout/stderr, so `go test` (and
		// `go test -json`, which parses the same stream) sees one stream.
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmds = append(cmds, cmd)
	}
	if len(cmds) < 2 {
		shardLogf("running as one process: only %d non-empty shard(s)", len(cmds))
		return 0, false
	}

	for _, cmd := range cmds {
		if err := cmd.Start(); err != nil {
			for _, started := range cmds {
				if started.Process != nil {
					_ = started.Process.Kill()
				}
			}
			shardLogf("FAIL shard start: %v", err)
			return 1, true
		}
	}

	// A killed gate must not leave children behind: they hold DB connections and
	// a live Valkey namespace. SIGINT/SIGTERM here kills every child, which makes
	// the waits below return promptly and the exit code non-zero.
	stopSignals := forwardSignals(cmds)
	defer stopSignals()

	code := 0
	for i, cmd := range cmds {
		if err := cmd.Wait(); err != nil {
			shardLogf("shard %d/%d failed: %v", i+1, len(cmds), err)
			if code == 0 {
				code = 1
			}
		}
	}
	if code == 0 {
		shardLogf("ran %d test(s) in %d concurrent shard process(es)", len(names), len(cmds))
	}
	return code, true
}

// shardLogf writes a line to stderr. Stderr, not stdout: `go test -json` turns
// every unsolicited stdout line into an "output" event, and this runner's own
// bookkeeping is not a test's output.
func shardLogf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "httpapi: "+format+"\n", args...)
}

// testShardCount resolves the shard count for this process.
func testShardCount() int {
	return testShardCountFor(runtime.GOMAXPROCS(0))
}

// testShardCountFor resolves the shard count from cpus, the number of CPUs this
// process may use (runtime.GOMAXPROCS(0) in production; a parameter so a unit
// test can stand in for a cgroup quota without creating one).
//
// It deliberately does NOT derive the count from cpus. Under the gates'
// CPUQuota=300% Go reports GOMAXPROCS=3 (measured on this host, 6 uncapped), so
// any "cpus minus headroom" rule collapses to a single process exactly where
// the sharding pays. Four race-instrumented children draw 251% of a core and
// fit inside the 300% quota. The one exception is a process that may use a
// single CPU: four children on one core only queue behind each other.
func testShardCountFor(cpus int) int {
	if raw := strings.TrimSpace(os.Getenv(testShardsEnv)); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			shardLogf("ignoring %s=%q: not a non-negative integer", testShardsEnv, raw)
			return testShardDefault
		}
		return n
	}
	if cpus < 2 {
		return 1
	}
	return testShardDefault
}

// shardingForbidden reports the first flag or condition that makes re-execution
// unsafe. It returns ("", "") when sharding is fine.
func shardingForbidden(args []string) (flag, why string) {
	// One file per run: children would clobber each other's profile/coverage
	// output and the last writer would win silently.
	single := []string{
		"-test.cpuprofile",
		"-test.memprofile",
		"-test.blockprofile",
		"-test.mutexprofile",
		"-test.trace",
		"-test.coverprofile",
		"-test.gocoverdir",
		"-test.testlogfile",
	}
	for _, a := range args {
		name := a
		if i := strings.IndexByte(a, '='); i >= 0 {
			name = a[:i]
		}
		for _, s := range single {
			if name == s {
				return name, fmt.Sprintf("%s writes one per-run file, so the children would overwrite each other", name)
			}
		}
	}
	return "", ""
}

// testRunFilter extracts the caller's own `-test.run` value so the enumeration
// lists exactly the tests this run would have executed. Empty means "everything".
func testRunFilter(args []string) (string, error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case strings.HasPrefix(a, "-test.run="):
			return strings.TrimPrefix(a, "-test.run="), nil
		case a == "-test.run":
			if i+1 >= len(args) {
				return "", errors.New("-test.run was passed without a value")
			}
			return args[i+1], nil
		}
	}
	return "", nil
}

// shardChildArgs forwards this process's `-test.*` argv to a child, replacing
// the run filter with the child's shard. Everything else (verbosity, timeout,
// count, parallel, shuffle) has to reach the child or a sharded run would behave
// differently from an unsharded one.
func shardChildArgs(args, bucket []string) []string {
	out := make([]string, 0, len(args)+1)
	skipNext := false
	for _, a := range args {
		if skipNext {
			skipNext = false
			continue
		}
		name := a
		if i := strings.IndexByte(a, '='); i >= 0 {
			name = a[:i]
		}
		if name == "-test.run" || name == "-test.list" {
			// The value of a two-token flag is dropped with it.
			if !strings.Contains(a, "=") {
				skipNext = true
			}
			continue
		}
		out = append(out, a)
	}
	return append(out, "-test.run="+anchorAlternation(bucket))
}

// anchorAlternation builds the exact-match alternation for one shard. Without
// the anchors a name could also match a LONGER test name (`TestAuth` inside
// `TestAuthHardening`), which would run a test in two shards at once.
func anchorAlternation(names []string) string {
	return "^(" + strings.Join(names, "|") + ")$"
}

// enumerateTests lists the tests this run would execute, by running this same
// binary with `-test.list` — the supported way to ask a test binary what it
// contains (`go test -list` is the same call). The child carries the shard-child
// marker so it can never shard itself, and with `-test.list` set testing.M.Run
// prints the names instead of running them.
func enumerateTests(runFilter string) ([]string, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	pattern := runFilter
	if pattern == "" {
		pattern = "."
	}
	cmd := exec.Command(exe, "-test.list="+pattern)
	cmd.Env = append(os.Environ(), testShardChildEnv+"=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
	}
	var names []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		// -test.list also prints benchmarks, fuzz targets and examples; only
		// tests take a -test.run filter that can be split this way.
		if strings.HasPrefix(line, "Test") {
			names = append(names, line)
		}
	}
	if len(names) == 0 {
		return nil, errors.New("the test binary listed no tests")
	}
	return names, nil
}

// shardBuckets splits names into n complete, disjoint buckets. The assignment is
// a stable hash of the test name, so a shard's contents do not depend on the
// order the package happens to declare tests in and every name lands in exactly
// one bucket. Slow tests are spread by the same hash; the suite's long tail is
// ~250s of ~490s, so a perfect balance is not required — an even split of the
// 1283 names was measured at 302/312/331/338.
func shardBuckets(names []string, n int) [][]string {
	buckets := make([][]string, n)
	for _, name := range names {
		h := fnv.New32a()
		_, _ = h.Write([]byte(name))
		i := int(h.Sum32() % uint32(n))
		buckets[i] = append(buckets[i], name)
	}
	return buckets
}

// forwardSignals kills every child when this process is asked to stop, and
// returns a function that stops the forwarding. A child that outlives its parent
// holds a DB schema and a Valkey namespace; killing them keeps a cancelled gate
// from leaving a run behind.
func forwardSignals(cmds []*exec.Cmd) func() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	var once sync.Once
	go func() {
		select {
		case <-ch:
			for _, cmd := range cmds {
				if cmd.Process != nil {
					_ = cmd.Process.Kill()
				}
			}
		case <-done:
		}
	}()
	return func() {
		once.Do(func() {
			signal.Stop(ch)
			close(done)
		})
	}
}

// TestShardPartitionCoversEveryListedTestExactlyOnce is the property the whole
// mechanism rests on: whatever the shard count, every test the run would have
// executed lands in exactly one shard. A lost test is a silent coverage hole (the
// suite would go green having run less), and a duplicated one runs twice against
// the same package fixtures.
//
// It enumerates the LIVE list rather than a pinned copy, so a test file added
// tomorrow is covered by the assertion the day it appears.
func TestShardPartitionCoversEveryListedTestExactlyOnce(t *testing.T) {
	names, err := enumerateTests("")
	if err != nil {
		t.Fatalf("enumerate this package's tests: %v", err)
	}
	if len(names) < testShardFloor {
		t.Fatalf("the live list holds %d test(s), below the shard floor of %d — "+
			"an enumeration this short would be refused at run time, so this guard would "+
			"be testing a mechanism that never fires", len(names), testShardFloor)
	}
	for _, n := range []int{2, 3, 4, 7} {
		buckets := shardBuckets(names, n)
		if len(buckets) != n {
			t.Fatalf("shardBuckets(n=%d) returned %d bucket(s)", n, len(buckets))
		}
		seen := make(map[string]int, len(names))
		for _, bucket := range buckets {
			for _, name := range bucket {
				seen[name]++
			}
		}
		if len(seen) != len(names) {
			t.Errorf("n=%d: the %d shard(s) cover %d distinct test(s) of %d — %d would never run",
				n, n, len(seen), len(names), len(names)-len(seen))
		}
		for name, count := range seen {
			if count != 1 {
				t.Errorf("n=%d: %s would run in %d shards", n, name, count)
			}
		}
	}
}

// TestShardCountKeepsFourProcessesUnderACPUQuota pins the derivation that a
// CPUQuota=300% scope broke. Go is cgroup-aware, so inside the quota the gates
// run under, GOMAXPROCS is 3 (measured on this host: 6 uncapped, 3 at 300%, 4
// at 400%), and the old `GOMAXPROCS-2` rule returned 1 — so the gate ran the
// serial suite while the code looked sharded. The count must not depend on the
// quota at all. The single-core exception and the env override are pinned here
// too, so a future "tidy-up" of the derivation is caught by the suite rather
// than by a merge train that quietly got 2.4x slower.
func TestShardCountKeepsFourProcessesUnderACPUQuota(t *testing.T) {
	t.Setenv(testShardsEnv, "")
	for _, cpus := range []int{2, 3, 4, 6, 32} {
		if got := testShardCountFor(cpus); got != testShardDefault {
			t.Errorf("testShardCountFor(%d) = %d, want %d — the gates run under CPUQuota=300%%, "+
				"where GOMAXPROCS is 3; a count derived from it collapses to 1", cpus, got, testShardDefault)
		}
	}
	// A single usable CPU is the only auto case that stays serial.
	for _, cpus := range []int{0, 1} {
		if got := testShardCountFor(cpus); got != 1 {
			t.Errorf("testShardCountFor(%d) = %d, want 1", cpus, got)
		}
	}
	// The override still wins, including the documented 0/1 off switch.
	for raw, want := range map[string]int{"0": 0, "1": 1, "2": 2, "7": 7} {
		t.Setenv(testShardsEnv, raw)
		if got := testShardCountFor(3); got != want {
			t.Errorf("%s=%s: testShardCountFor(3) = %d, want %d", testShardsEnv, raw, got, want)
		}
	}
	t.Setenv(testShardsEnv, "not-a-number")
	if got := testShardCountFor(3); got != testShardDefault {
		t.Errorf("a malformed override resolved to %d, want the default %d", got, testShardDefault)
	}
}

// TestShardRegexIsAnchoredSoAShardCannotRunItsNeighbours is the control for the
// partition above: -test.run is matched as a SUBSTRING, so an unanchored
// alternation of one shard's names also selects every test whose name starts
// with one of them. Both directions are asserted, because a regex that matched
// nothing would make the suite pass while running no tests.
func TestShardRegexIsAnchoredSoAShardCannotRunItsNeighbours(t *testing.T) {
	rx := anchorAlternation([]string{"TestAuth", "TestFeedbackReply"})

	if ok, err := regexp.MatchString(rx, "TestAuth"); err != nil || !ok {
		t.Fatalf("the shard regex %q does not match its own test: ok=%v err=%v", rx, ok, err)
	}
	if ok, err := regexp.MatchString(rx, "TestFeedbackReplyBodyUsesTheFilersLanguage"); err != nil || ok {
		t.Fatalf("the shard regex %q also matches a LONGER name, which would run that test in two shards: ok=%v err=%v", rx, ok, err)
	}
	if ok, err := regexp.MatchString(rx, "TestAuthHardening"); err != nil || ok {
		t.Fatalf("the shard regex %q also matches a longer name sharing its prefix: ok=%v err=%v", rx, ok, err)
	}
	// A one-element shard must still be exactly one test.
	if got := anchorAlternation([]string{"TestHealthz"}); got != "^(TestHealthz)$" {
		t.Fatalf("anchorAlternation of one name = %q", got)
	}
}

// TestShardChildArgsKeepEveryOtherFlagAndReplaceTheRunFilter pins the argv
// translation: the child must receive everything the parent was told that
// changes what a test sees (verbosity, timeout, count, parallel, shuffle) and
// must NOT receive the parent's run filter, which its shard replaces. Dropping
// -test.timeout would give every child Go's 10m default — the exact budget that
// reds this package on a loaded host.
func TestShardChildArgsKeepEveryOtherFlagAndReplaceTheRunFilter(t *testing.T) {
	parent := []string{
		"-test.paniconexit0",
		"-test.timeout=30m0s",
		"-test.count=1",
		"-test.v=test2json",
		"-test.parallel=6",
		"-test.run",
		"TestOnlyThis",
		"-test.list=TestX",
	}
	got := shardChildArgs(parent, []string{"TestA", "TestB"})

	want := []string{
		"-test.paniconexit0",
		"-test.timeout=30m0s",
		"-test.count=1",
		"-test.v=test2json",
		"-test.parallel=6",
		"-test.run=^(TestA|TestB)$",
	}
	if len(got) != len(want) {
		t.Fatalf("child argv = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("child argv = %q, want %q (differ at %d)", got, want, i)
		}
	}
	// The two-token spelling must be dropped along with its value, or the child
	// would see the parent's filter as a bare argument — and a bare "TestOnlyThis"
	// argument makes the test binary fail on an unknown flag.
	for _, a := range got {
		if strings.Contains(a, "TestOnlyThis") {
			t.Fatalf("the parent's -test.run value leaked into the child argv: %q", got)
		}
	}
}

// TestShardingIsRefusedWhenTheRunWritesOnePerRunFile holds the fail-safe: a
// profile or coverage flag must turn sharding OFF, because every child would
// write the same file and the last writer would win silently. The same rule
// covers `go test`'s cache record (-test.testlogfile), which must describe the
// whole package.
func TestShardingIsRefusedWhenTheRunWritesOnePerRunFile(t *testing.T) {
	refused := [][]string{
		{"-test.cpuprofile=/tmp/cpu.prof"},
		{"-test.memprofile", "/tmp/mem.prof"},
		{"-test.blockprofile=/tmp/block.prof"},
		{"-test.mutexprofile=/tmp/mutex.prof"},
		{"-test.trace=/tmp/trace.out"},
		{"-test.coverprofile=/tmp/cover.out"},
		{"-test.gocoverdir=/tmp/cov"},
		{"-test.testlogfile=/tmp/testlog.txt"},
	}
	for _, args := range refused {
		if flag, why := shardingForbidden(args); flag == "" || why == "" {
			t.Errorf("shardingForbidden(%q) allowed the run, but %s writes one per-run file", args, args[0])
		}
	}
	// The flags `go test` passes on EVERY run must not disable it, or sharding
	// would never happen where it matters.
	for _, args := range [][]string{
		{},
		{"-test.paniconexit0", "-test.outputdir=/repo/api"},
		{"-test.timeout=30m0s", "-test.count=1", "-test.run=TestX"},
	} {
		if flag, why := shardingForbidden(args); flag != "" {
			t.Errorf("shardingForbidden(%q) refused the run because of %s (%s)", args, flag, why)
		}
	}
}

// TestShardRunFilterReadsBothSpellings keeps the parent's own filter intact:
// `go test -run TestX` must shard TestX's shard, not the whole package, and both
// argv spellings reach a test binary in practice.
func TestShardRunFilterReadsBothSpellings(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{nil, ""},
		{[]string{"-test.run=TestHealthz"}, "TestHealthz"},
		{[]string{"-test.run", "TestHealthz"}, "TestHealthz"},
		{[]string{"-test.timeout=1m0s", "-test.run", "^TestAuth", "-test.count=1"}, "^TestAuth"},
	} {
		got, err := testRunFilter(tc.args)
		if err != nil {
			t.Fatalf("testRunFilter(%q): %v", tc.args, err)
		}
		if got != tc.want {
			t.Errorf("testRunFilter(%q) = %q, want %q", tc.args, got, tc.want)
		}
	}
	if _, err := testRunFilter([]string{"-test.run"}); err == nil {
		t.Error("a -test.run with no value was accepted; the child would then run the whole package")
	}
}
