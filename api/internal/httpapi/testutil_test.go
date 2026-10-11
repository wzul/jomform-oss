package httpapi

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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
// runKey is the per-PASS entropy these fixtures need to stay unique across
// `-test.count=N` (card t_5299cae1).
//
// The fixtures below used to mint their DB-unique keys from data that does NOT
// vary between passes — a package-level const domain, or an email derived from
// `t.Name()` alone. Pass 2 then collided on the DB's real uniques
// (`users_email_key`, `account_domains_domain_key`) and 14 assertions failed,
// measured with `go test ./internal/httpapi/ -count=2 -run 'TestRESTReadwrite…'`.
// Weakening those indexes is not an option, so the KEYS have to vary.
//
// `-test.count` re-runs the whole binary in ONE process, so the pid does not
// change; the process start time does not either. `time.Now().UnixNano()` plus a
// counter is unique within a run and across runs, on a fresh or reused DB, and it
// carries no ordering semantics a test could depend on. The counter matters even
// though the clock does: two passes can land in the same nanosecond on a fast host,
// and the counter then makes them differ anyway.
var runKeyCounter atomic.Uint64

func runKey() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), runKeyCounter.Add(1))
}

func uniqueTestIP() string {
	return testIPFor(uint32(os.Getpid()), testIPCounter.Add(1))
}

// testIP returns the address the CURRENT test owns for the whole of one run of
// that test: every call from the same test gets the same address, and a
// different test (or the same test on a later -count pass) gets a fresh one.
//
// It exists because a test-only address must sit inside the /24 the auth
// fixture reclaims (testIPFamilyFor), and a hand-written literal never does.
// `jf:loginfail:ip:<addr>` has a 15-minute TTL in a Valkey DB SHARED by every
// checkout, and the fixture deliberately deletes only counters under THIS
// process's /24 — so a literal like 10.20.0.2 is a counter the reclaim can
// never clear. One test drives it to the lockout threshold and the next lane's
// run inherits it, which is the 429-instead-of-401 flake this whole mechanism
// exists to remove. Measured on the pre-fix tree: after one full run,
// `jf:loginfail:ip:10.9.9.9` survived with its TTL re-armed.
//
// Two shapes are deliberately avoided:
//
//   - It is NOT a fixed address per test. A test that drives its address into
//     lockout (TestAuthLoginLockoutAfter10) would find the counter already at
//     the threshold on the next -count pass, and assert 429 where it wants 401.
//     Returning a fresh address per pass is what keeps -count deterministic.
//   - It is NOT a fresh address per call, which would spend the 254-address
//     per-process budget on call sites rather than on tests: the package has
//     well over a hundred IP-bearing request sites but only ~60 tests that need
//     one, so per-call allocation would exhaust maxTestIPsPerProcess after a
//     couple of -count passes and panic inside a test run.
//
// Tests still share one address across the requests that must be seen as one
// client (an arm + its email-code, a lockout sequence, a magic-link request and
// its verify), which is what the old per-test literal was doing.
func testIP(t *testing.T) string {
	t.Helper()
	name := t.Name()

	testIPMu.Lock()
	defer testIPMu.Unlock()
	if name == testIPName {
		return testIPAddr
	}
	testIPName, testIPAddr = name, uniqueTestIP()
	return testIPAddr
}

// setTestClientAddr presents ip as the request's SOCKET PEER, which is where the
// API reads a client address from on a request that reached it directly.
//
// middleware.ClientIP reads a forwarding header only when the socket peer is an
// address a request can arrive through, and it never uses CF-Connecting-IP
// unless the chain shows a Cloudflare edge (card t_ac16d273: the header used to
// be read unconditionally, so a request straight to the origin chose its own
// rate-limit bucket). A test that wants "this request came from address X" must
// therefore put X where a directly-connected client's address really is:
// r.RemoteAddr. Setting only a header leaves httptest's default 192.0.2.1 as the
// peer, which every such request then shares.
//
// A test that drives the API over TCP (`http.DefaultClient.Do` against an
// httptest server) cannot choose its peer — the kernel says 127.0.0.1 — so those
// present their address in X-Forwarded-For instead, where a front would put it.
func setTestClientAddr(req *http.Request, ip string) {
	if ip == "" {
		return
	}
	req.RemoteAddr = ip + ":1234"
}

// testIPMu guards the one-entry memo testIP keeps. One address per test rather
// than one per call, so the memo is just "what did the test I am in already
// get". Tests in this package are sequential (no t.Parallel anywhere), so the
// memo is only ever read by the test that wrote it; the mutex is there because
// testIP is called from helpers too, and a future t.Parallel must not race.
var (
	testIPMu   sync.Mutex
	testIPName string
	testIPAddr string
)

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
