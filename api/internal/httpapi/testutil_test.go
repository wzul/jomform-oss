package httpapi

import (
	"fmt"
	"log/slog"
	"math/bits"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// testMailDomain is the recipient domain for any test whose flow must actually
// reach the mailer (magic-link sign-in, 2FA, invites).
//
// It is deliberately NOT one of the RFC 2606/6761 names. A magic link to a
// reserved name is refused before the mailer by the unroutable-recipient guard
// (card t_641d62d5), so a test that aimed one at example.test/example.com would
// now be asserting against that guard instead of the behaviour it names —
// silently, because "no mail was sent" is also what a broken flow produces.
// No mail leaves the process: every test wires a recording mailer.
const testMailDomain = "qa.jomform.com"

// testRegistrableDomain is the recipient domain for a test whose REGISTRATION
// must pass the MX pre-check (issue #56) and then reach the layer the test
// actually names (suppression skip, verification breaker).
//
// It has to be a domain that publishes real MX records, and it is deliberately
// neither of the other two candidates:
//   - not testMailDomain: qa.jomform.com has no MX, which is fine on the
//     magic-link path (no MX check there) but makes registration return 400;
//   - not example.com/.net/.org: those apexes publish an RFC 7505 null MX,
//     which since card t_f5b4d28a the checker correctly reads as "accepts no
//     mail" — a registration aimed there is refused before the code under test
//     (that is exactly what TestSESRegisterRefusesNullMXDomain asserts).
//
// No mail leaves the process: every test wires a recording mailer.
const testRegistrableDomain = "jomform.com"

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
// It exists because a test-only address must sit inside the address block the
// auth fixture reclaims (testIPPoolFor), and a hand-written literal never does.
// `jf:loginfail:ip:<addr>` has a 15-minute TTL in a Valkey DB SHARED by every
// checkout, and the fixture deliberately deletes only counters under THIS
// process's block — so a literal like 10.20.0.2 is a counter the reclaim can
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
//   - It is NOT a fresh address per call, which would spend the per-process
//     budget on call sites rather than on tests: the package has well over a
//     hundred IP-bearing request sites but only ~60 tests that need one, so
//     per-call allocation would exhaust maxTestIPsPerProcess in a pass or two
//     and panic inside a test run.
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

// testIPHostsPerSlash24 is the number of usable host addresses one /24
// contributes: .1–.254. The first and last addresses of a block are not handed
// out, so a test address is always a host address and never the network or
// broadcast one of its /24.
const testIPHostsPerSlash24 = 254

// testIPSlash24sPerProcess is how many /24s ONE process owns.
//
// It has to be more than one. This suite spends 139 uniqueTestIP addresses per
// pass (measured 2026-10-14 by reading the counter at the end of each pass of a
// `-count=3` run: 139 / 278 / 417), so `-test.count=3` asks for 417 addresses and
// a single /24 — 254 — is exhausted during pass 2: the pre-fix run died at call
// 255, which is the panic card t_7655c2bf measured. Two /24s = 508 addresses
// covers -count=3 and no more, and -count=4 (556) must therefore panic rather
// than wrap (see maxTestIPsPerProcess). The block cannot grow further while the
// pid-churn invariant on testIPPoolCount holds — a /22 (1024 addresses) would
// need a block count below the measured churn — so a suite that grows past ~169
// IP-consuming tests per pass needs a different mechanism, not a bigger number
// here.
const testIPSlash24sPerProcess = 2

// maxTestIPsPerProcess bounds the per-process counter. Exceeding it must FAIL
// LOUDLY rather than wrap: a silent repeat is precisely the stale-counter bug
// this helper exists to prevent (issue #474), because the counter a repeated
// address reads is one this process already drove to the lockout threshold, and
// the reclaim that clears it runs once per PROCESS, not once per pass.
const maxTestIPsPerProcess = testIPHostsPerSlash24 * testIPSlash24sPerProcess

// testIPPoolCount is how many per-process address blocks the fixture can hand
// out: 10.0.0.0/8 holds 2^24 addresses, a block is two /24s (2^9), so 2^15
// blocks.
//
// This number is load-bearing, not a detail: it is the modulus of the pid →
// block mapping, so two pids that differ by a multiple of it own the SAME
// addresses. The block must therefore be bigger than the number of pids the
// host allocates inside the 15-minute TTL of `jf:loginfail:ip:<addr>`, so that
// a process sharing a peer's block can only be one whose peer's counters have
// expired. Measured on this host on 2026-10-14 (`cat
// /proc/sys/kernel/ns_last_pid` a minute apart): 1447 pids/min, i.e. 21705 pids
// per TTL — well under 32768, whereas a /22 (16384 blocks) would sit UNDER it
// and re-open the cross-process flake. TestTestIPPoolCountOutlastsTheCounterTTL
// pins that.
const testIPPoolCount = 1 << 15

// testIPPool is the block of synthetic client addresses ONE process owns: two
// consecutive /24s (a /23), each contributing its 254 host addresses. Every
// address testIPFor can produce from a pid sits inside this block, which is
// what lets the auth fixture reclaim its own stale login counters without
// touching a peer's (authTestValkey / reclaimOwnLoginCounters).
//
// It is a struct rather than a prefix string because the block is no longer one
// /24: ownership is "the address's octets fall inside the block", which a string
// prefix can no longer express (10.40.4. and 10.40.5. are both inside
// 10.40.4.0/23, and 10.40.6. is not).
type testIPPool struct {
	octet2 uint32 // the block's second octet — fixed
	octet3 uint32 // the block's third octet — the /23 base, i.e. even
}

// String renders the block as its CIDR, for panic messages and failures. The
// prefix length is derived from the block's width, so it cannot drift from
// testIPSlash24sPerProcess.
func (p testIPPool) String() string {
	// log2 of the number of /24s in the block: 2 /24s -> a /23.
	blockBits := bits.Len32(testIPSlash24sPerProcess) - 1
	return fmt.Sprintf("10.%d.%d.0/%d", p.octet2, p.octet3, 24-blockBits)
}

// families lists the /24 key-prefix fragments of the block, which is the scope
// the reclaim SCANs: `jf:loginfail:ip:` + one of these + `*`.
func (p testIPPool) families() []string {
	out := make([]string, 0, testIPSlash24sPerProcess)
	for i := uint32(0); i < testIPSlash24sPerProcess; i++ {
		out = append(out, fmt.Sprintf("10.%d.%d.", p.octet2, p.octet3+i))
	}
	return out
}

// owns reports whether ip is an address THIS block can hand out — the exact
// shape testIPFor produces, not merely a string that starts like one.
func (p testIPPool) owns(ip string) bool {
	octets, ok := parseTestIPv4(ip)
	if !ok {
		return false
	}
	return octets[0] == 10 &&
		octets[1] == p.octet2 &&
		octets[2] >= p.octet3 && octets[2] < p.octet3+testIPSlash24sPerProcess &&
		octets[3] >= 1 && octets[3] <= testIPHostsPerSlash24
}

// testIPPoolFor maps a pid onto the block of addresses that process owns:
//
//	k = pid mod testIPPoolCount  ->  the /23 at 10.(k>>7).((k&0x7f)<<1).0
//
// Encoding the pid in both upper octets is deliberate (issue #474): a
// single-octet derivation isolated two processes only while every live pid
// differed in that octet, and on this host that is measurably false — three
// processes started at the same instant held pids 3734250, 3734251 and 3734252
// (low bytes 234, 235, 236) beside processes in the neighbouring PID_NR block
// (low bytes 43, 44, 45), because pid_max is 4194304.
//
// The mapping is injective for any two pids less than testIPPoolCount apart —
// including the ones a concurrent `go test` can really hold (measured 1-8, and
// the block-adjacent +256 case) — and aliases at exactly testIPPoolCount. That
// alias is the boundary of the guarantee, and it is safe only because of the
// pid-churn argument on testIPPoolCount; both halves are pinned by
// TestUniqueTestIPIsPerProcess and TestTestIPPoolCountOutlastsTheCounterTTL.
func testIPPoolFor(pid uint32) testIPPool {
	k := pid & (testIPPoolCount - 1)
	return testIPPool{octet2: k >> 7, octet3: (k & 0x7f) << 1}
}

// testIPFor is the pure derivation behind uniqueTestIP: call n of a process is
//
//	10 . <pid mod testIPPoolCount encoded> . <n mapped onto the block>
//
// The call index fills the block a /24 at a time — .1–.254 of the first /24,
// then .1–.254 of the second — so n=1..254 is the old single-/24 range
// unchanged, and n carries on into the block's second /24 instead of panicking
// at 255.
func testIPFor(pid, n uint32) string {
	if n == 0 || n > maxTestIPsPerProcess {
		panic(fmt.Sprintf("uniqueTestIP: call %d is outside the %d addresses this process owns in %s — the fixture's per-process counter must not wrap, because a reused address would read a login-failure counter this process had already driven to the lockout threshold",
			n, maxTestIPsPerProcess, testIPPoolFor(pid)))
	}
	pool := testIPPoolFor(pid)
	return fmt.Sprintf("10.%d.%d.%d",
		pool.octet2,
		pool.octet3+(n-1)/testIPHostsPerSlash24,
		(n-1)%testIPHostsPerSlash24+1)
}

// parseTestIPv4 parses a dotted quad the way testIPFor writes one: four decimal
// octets, no leading zeros (so "10.1.2.01" is not mistaken for "10.1.2.1"), no
// sign, no port, no extra suffix. A key found by a SCAN pattern that merely
// starts like an address must not be read as one.
func parseTestIPv4(ip string) ([4]uint32, bool) {
	var out [4]uint32
	parts := strings.Split(ip, ".")
	if len(parts) != 4 {
		return out, false
	}
	for i, part := range parts {
		if part == "" || len(part) > 3 || (len(part) > 1 && part[0] == '0') {
			return out, false
		}
		v, err := strconv.Atoi(part)
		if err != nil || v < 0 || v > 255 || strconv.Itoa(v) != part {
			return out, false
		}
		out[i] = uint32(v)
	}
	return out, true
}

// TestUniqueTestIPIsPerProcess pins the property that makes the fixture safe
// under a concurrent `go test` run: the login breaker and the coarse rate
// limiter key on the address, and that keyspace is shared by every checkout.
// Before this helper, both processes handed out 10.20.0.2 for their first call,
// so one run's failed logins locked the other run's assertions out.
func TestUniqueTestIPIsPerProcess(t *testing.T) {
	pid := uint32(os.Getpid())

	// One process's whole BLOCK must be its own — for the pids a peer can
	// actually hold beside us. Processes started at the same time hold adjacent
	// pids (measured 1-8 apart, hence the small offsets) and both make their
	// first call at index 1; the same-octet peer +256 is here because that is the
	// offset a single-octet derivation would silently collide on, and +20000
	// pins that the block is wider than a single /24 (a block per /24 would
	// collide with a peer a few hundred pids away, which is a live peer).
	ours := map[string]uint32{}
	for n := uint32(1); n <= maxTestIPsPerProcess; n++ {
		ip := testIPFor(pid, n)
		if prev, ok := ours[ip]; ok {
			t.Fatalf("testIPFor(pid=%d, n=%d) reused the address %s this process first handed out at n=%d — a repeat re-keys a counter already driven to lockout",
				pid, n, ip, prev)
		}
		ours[ip] = n
	}
	if len(ours) != maxTestIPsPerProcess {
		t.Fatalf("%d calls produced only %d distinct addresses — the block is not being filled one address per call", maxTestIPsPerProcess, len(ours))
	}
	peers := []uint32{pid + 1, pid + 2, pid + 3, pid + 7, pid + 100, pid + 256, pid + 1000, pid + 20000}
	for _, peer := range peers {
		mine, theirs := testIPFor(pid, 1), testIPFor(peer, 1)
		if mine == theirs {
			t.Fatalf("pid %d and pid %d derive the same first test IP %s — two concurrent `go test` binaries would share the jf:loginfail:ip counter",
				pid, peer, mine)
		}
		if testIPPoolFor(pid) == testIPPoolFor(peer) {
			t.Fatalf("pid %d and pid %d own the same address block %s — the fixture's scoped counter cleanup would clobber the peer's live counters",
				pid, peer, testIPPoolFor(pid))
		}
		for n := uint32(1); n <= maxTestIPsPerProcess; n++ {
			theirs := testIPFor(peer, n)
			if whose, ok := ours[theirs]; ok {
				t.Fatalf("pid %d and pid %d both derive %s (ours at n=%d, theirs at n=%d) — two concurrent `go test` binaries would share a jf:loginfail:ip counter",
					pid, peer, theirs, whose, n)
			}
		}
	}

	// The alias is the BOUNDARY of that guarantee, so it is stated rather than
	// assumed: two pids exactly testIPPoolCount apart own the same block. It is
	// harmless only because a pid that far away is ~23 minutes away in start
	// time, which outlasts the counters' 15-minute TTL — the argument pinned by
	// TestTestIPPoolCountOutlastsTheCounterTTL.
	if testIPPoolFor(pid) != testIPPoolFor(pid+testIPPoolCount) {
		t.Errorf("pid %d and pid %d (testIPPoolCount apart) own different blocks — the modulus this fixture's safety argument is written against no longer holds",
			pid, pid+testIPPoolCount)
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

// TestTestIPPoolCountOutlastsTheCounterTTL is the guard on the ONE number that
// makes the pid → block alias harmless, and it exists because that number can be
// spent by accident. The fixture's safety argument is not "blocks never repeat"
// — they do, at a multiple of testIPPoolCount apart — it is that a peer sharing
// a block has been gone long enough for its `jf:loginfail:ip:<addr>` counters
// (15-minute TTL, in a Valkey DB every checkout shares) to have expired.
//
// So the block count must exceed the pids this host allocates inside that TTL.
// Measured here on 2026-10-14 with
//
//	a=$(cat /proc/sys/kernel/ns_last_pid); sleep 60; b=$(cat /proc/sys/kernel/ns_last_pid)
//
// -> 1447 pids/min, i.e. 21705 per TTL window. The count is 2^15 = 32768 today;
// a /22 (16384 blocks, i.e. a 1016-address block) would sit UNDER the measured
// churn and would therefore let two live processes own the same addresses —
// which is exactly the cross-process flake this whole mechanism removes. Enlarge
// a process's block only together with this bound.
func TestTestIPPoolCountOutlastsTheCounterTTL(t *testing.T) {
	// measuredPidsPerCounterTTL is the host's pid churn inside the counters'
	// 15-minute TTL, as measured above. It is a floor for testIPPoolCount, not a
	// target: leaving it at the measured value means the check fails the moment
	// the block count drops to the churn the fix was written against.
	const measuredPidsPerCounterTTL = 21705
	if testIPPoolCount <= measuredPidsPerCounterTTL {
		t.Fatalf("testIPPoolCount=%d does not outlast the %d pids this host allocates inside the login counters' 15-minute TTL — two live processes would own the same address block and re-open the cross-process jf:loginfail:ip flake (a block of %d addresses per process is as large as this bound allows)",
			testIPPoolCount, measuredPidsPerCounterTTL, maxTestIPsPerProcess)
	}
}
