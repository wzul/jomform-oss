package httpapi

import (
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

var testIPCounter atomic.Uint32

// uniqueTestIP returns a fresh IP for one test. The auth handlers key their
// per-IP lockout and rate-limit counters in the shared test Valkey (15-minute
// TTL), so a fixed IP accumulates failures across -count runs: a test that
// deliberately drives one IP into lockout would find that IP already locked on
// the next run. Deriving the IP per test keeps the counters isolated
// (issue #474).
//
// The counters live in a Valkey keyspace SHARED by every checkout — the whole
// board gates at once, several worktrees at a time — so the address must be
// unique across PROCESSES too, which a bare per-process counter is not: every
// `go test` binary started the same sequence at the same address, so two
// concurrent runs keyed the same `jf:loginfail:ip:<addr>` counter and each one's
// failed logins answered the other's assertions with 429 ("failed login attempt
// 5: status = 429, want 401") while a peer's successful login reset a counter
// this process had just driven to the lockout threshold. The pid — which the
// kernel keeps unique among LIVE processes — therefore fixes two octets, and only
// the last one counts within the process.
//
// Generate one value at the top of the test and reuse it for every request in
// that test — tests that compare responses, or that drive a single IP to a
// limit, need a stable IP within the test.
func uniqueTestIP() string {
	return testIPFor(uint32(os.Getpid()), testIPCounter.Add(1))
}

// maxTestIPsPerProcess bounds the per-process counter. Octet 4 is the only one
// left for the call index, so exceeding it must FAIL LOUDLY rather than wrap: a
// silent repeat is precisely the stale-counter bug this helper exists to
// prevent (issue #474). The package has a handful of uniqueTestIP call sites, so
// even a -count rerun stays far below the bound.
const maxTestIPsPerProcess = 254

// testIPFor is the pure derivation behind uniqueTestIP:
//
//		10 . <pid low byte> . <pid high byte> . <call index>
//
//	  - Octets 2-3 encode the pid, which the kernel keeps unique among LIVE
//	    processes, so two `go test` binaries running this package at the same time
//	    never share an address — and therefore never share the
//	    `jf:loginfail:ip:<addr>` counter in the shared test Valkey. The mapping is
//	    injective for pids within the same 64K block; it aliases only for pids
//	    65536 apart, which two concurrent runs cannot be: processes started at the
//	    same instant on this host hold pids a handful apart (measured 1-8), and the
//	    counters have a 15-minute TTL, so no longer-relevant pid can hold one.
//
//	    Both pid octets are encoded deliberately. An address that varied by a
//	    single octet per process would be isolated only while every live pid
//	    differs in that octet, and on this host that is measurably false: three
//	    processes started at the same instant held pids 3734250, 3734251 and
//	    3734252 (low bytes 234, 235, 236) beside processes in the neighbouring
//	    PID_NR block (low bytes 43, 44, 45), because pid_max is 4194304.
//	  - Octet 4 is the per-process call index (issue #474's -count case).
//
// Encoding the pid in the upper octets — rather than, say, borrowing octet 3 for
// counter overflow — keeps a process's whole address range inside ONE /24, which
// is what lets the fixture reclaim its own stale counters without touching a
// peer's (see authTestValkey / testIPFamilyFor).
func testIPFor(pid, n uint32) string {
	if n == 0 || n > maxTestIPsPerProcess {
		panic(fmt.Sprintf("uniqueTestIP: call %d is outside the %d addresses this process owns in %s* — the fixture's per-process counter must not wrap, because a reused address would read a login-failure counter this process had already driven to the lockout threshold",
			n, maxTestIPsPerProcess, testIPFamilyFor(pid)))
	}
	return fmt.Sprintf("10.%d.%d.%d", pid&0xff, (pid>>8)&0xff, n)
}

// testIPFamilyFor is the /24 one process owns, as a Valkey key pattern fragment:
// every address testIPFor can produce from this pid sits inside it.
//
// It is also the scope the auth fixture reclaims stale login counters under
// (authTestValkey): a process may delete a counter only if the address is one it
// could have written.
func testIPFamilyFor(pid uint32) string {
	return fmt.Sprintf("10.%d.%d.", pid&0xff, (pid>>8)&0xff)
}

// TestUniqueTestIPIsPerProcess pins the property that makes the fixture safe
// under a concurrent `go test` run: the login breaker and the coarse rate
// limiter key on the address, and that keyspace is shared by every checkout.
// Before this helper, both processes handed out 10.20.0.2 for their first call,
// so one run's failed logins locked the other run's assertions out.
func TestUniqueTestIPIsPerProcess(t *testing.T) {
	pid := uint32(os.Getpid())

	// One process's whole range must be its own — for the pids a peer can
	// actually hold beside us. Processes started at the same time hold adjacent
	// pids (measured 1-8 apart, hence the small offsets) and both make their
	// first call at index 1; the same-octet peer +256 is here because that is the
	// offset a single-octet derivation would silently collide on.
	ours := map[string]uint32{}
	for n := uint32(1); n <= maxTestIPsPerProcess; n++ {
		ip := testIPFor(pid, n)
		if prev, ok := ours[ip]; ok {
			t.Fatalf("testIPFor(pid=%d, n=%d) reused the address %s this process first handed out at n=%d — a repeat re-keys a counter already driven to lockout",
				pid, n, ip, prev)
		}
		ours[ip] = n
	}
	peers := []uint32{pid + 1, pid + 2, pid + 3, pid + 7, pid + 100, pid + 256, pid + 1000}
	for _, peer := range peers {
		mine, theirs := testIPFor(pid, 1), testIPFor(peer, 1)
		if mine == theirs {
			t.Fatalf("pid %d and pid %d derive the same first test IP %s — two concurrent `go test` binaries would share the jf:loginfail:ip counter",
				pid, peer, mine)
		}
		if testIPFamilyFor(pid) == testIPFamilyFor(peer) {
			t.Fatalf("pid %d and pid %d own the same /24 %s* — the fixture's scoped counter cleanup would clobber the peer's live counters",
				pid, peer, testIPFamilyFor(pid))
		}
		for n := uint32(1); n <= maxTestIPsPerProcess; n++ {
			theirs := testIPFor(peer, n)
			if whose, ok := ours[theirs]; ok {
				t.Fatalf("pid %d and pid %d both derive %s (ours at n=%d, theirs at n=%d) — two concurrent `go test` binaries would share a jf:loginfail:ip counter",
					pid, peer, theirs, whose, n)
			}
		}
	}

	// The counter must not wrap silently: a repeat would read this process's own
	// already-locked counter, which is the defect class in -count form.
	for _, n := range []uint32{0, maxTestIPsPerProcess + 1} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("testIPFor(pid=%d, n=%d) did not fail loudly — a wrapped counter reuses an address silently", pid, n)
				}
			}()
			_ = testIPFor(pid, n)
		}()
	}
}
