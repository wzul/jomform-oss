// Guard for the scoped login-counter reclaim (issue #474 follow-up).
//
// The auth fixture's counters live in a Valkey DB shared by every checkout, so
// cleaning them up has to be SCOPED to what this process owns. The obvious
// versions are both cross-process clobbers: SCAN+DEL of the production prefixes
// deletes a concurrent run's live lockout counter (and a live API's session
// tokens), and FlushDB is the same defect sharper. A peer losing the counter it
// just drove to the lockout threshold reads 200 where it asserted 429. This test
// states the requirement in a form the suite can check on its own, so either
// shape cannot come back unnoticed.
package httpapi

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/wzul/jomform/api/internal/auth"
)

// livePeerSeedKeys returns the two PRODUCTION-prefix keys the reclaim guard seeds
// to stand in for what a live process on the shared DB owns: a `jf:session:`
// token and the per-ACCOUNT `jf:loginfail:user:` counter the breaker writes. Both
// SHAPES are what the assertions read; the NAME carries the pid, which is what
// this process alone can produce.
//
// The pid is load-bearing, not decoration. The guard's t.Cleanup deletes these by
// name, so a fixed name is a cross-process collision of the class the scoped
// reclaim exists to remove: a second `go test` binary on the same Valkey DB
// deletes this process's live seed, and the guard fails on itself. Measured on a
// fixed name, 4 of 100 concurrent guard-test binaries red. Naming them per
// process is the fix; TestLivePeerSeedKeysArePerProcess pins it.
func livePeerSeedKeys(pid uint32) (session, accountCounter string) {
	own := strconv.Itoa(int(pid))
	return "jf:session:PEER-LIVE-SESSION-" + own,
		"jf:loginfail:user:peer-live-" + own + "@example.test"
}

// TestReclaimLeavesPeersLoginCountersAlone proves the fixture's one-time cleanup
// is scoped to this process's own test-IP /24: a counter under a /24 this process
// does not own must survive, and one under ours must be cleared so a -count rerun
// starts deterministic. Keys that merely sit on the PRODUCTION prefixes — a live
// API's session token, an account-keyed counter — must survive too, whatever this
// process is doing.
//
// It drives authTestValkey — the real wiring — rather than the helper, so a
// regression back to a whole-prefix sweep fails here. authOnce is reset so the
// cleanup runs for this test; the package's tests are sequential (no t.Parallel
// anywhere), so mutating the package-level once is safe.
func TestReclaimLeavesPeersLoginCountersAlone(t *testing.T) {
	pid := uint32(os.Getpid())
	prefix := loginCounterKeyPrefixes[0] // the login breaker's per-IP counter
	peer := peerCounterKey(pid)

	// The peer sentinel has to be a counter NO process's scoped reclaim can claim,
	// or the process that owns its /24 deletes it as one of its own stale counters
	// while this test asserts it survived — the false red peerCounterKey documents.
	// Checked exhaustively over the pid space here, and pinned on its own by
	// TestPeerCounterSentinelBelongsToNoProcess.
	if claimed := pidsClaimingLoginCounter(prefix, peer); len(claimed) > 0 {
		t.Fatalf("test setup: the peer sentinel %q is claimed by the scoped reclaim of pid ≡ %d (mod 65536), so that process would delete it before the assertion below reads it", peer, claimed[0])
	}

	// Connect directly first, so the keys are seeded BEFORE the fixture's
	// one-time cleanup runs.
	valkeyURL := os.Getenv("JOMFORM_TEST_VALKEY_URL")
	if valkeyURL == "" {
		t.Skip("JOMFORM_TEST_VALKEY_URL not set; skipping scoped-reclaim test")
	}
	sess, err := auth.NewSessions(valkeyURL)
	if err != nil {
		t.Fatalf("valkey sessions: %v", err)
	}
	ctx := context.Background()
	rdb := sess.Client()

	// A stale counter we own (the -count case: a previous run's leftover inside
	// our own address range), a live one standing in for a counter under a peer's
	// /24, and two keys a live process on this DB would own: its session token and
	// the per-ACCOUNT counter the breaker writes under the production prefix.
	//
	// All three seeds are named per PROCESS, not literally: the cleanup below
	// deletes BY NAME, so a fixed name would be this test's own cross-process bug
	// (see livePeerSeedKeys and peerCounterKey).
	mine := prefix + testIPFor(pid, maxTestIPsPerProcess)
	liveSession, liveAccountCounter := livePeerSeedKeys(pid)
	for k, v := range map[string]string{mine: "10", peer: "10", liveSession: "1", liveAccountCounter: "7"} {
		if err := rdb.Set(ctx, k, v, 0).Err(); err != nil {
			t.Fatalf("seed %s: %v", k, err)
		}
	}
	t.Cleanup(func() {
		_ = rdb.Del(context.Background(), peer, mine, liveSession, liveAccountCounter).Err()
	})

	authOnce = sync.Once{}
	authTestValkey(t)

	if n, err := rdb.Exists(ctx, mine).Result(); err != nil || n != 0 {
		t.Errorf("own stale counter %q still present (exists=%d err=%v) — a -count rerun would start from a leftover lockout", mine, n, err)
	}
	for _, tc := range []struct {
		key  string
		what string
	}{
		{peer, "a login counter under a /24 this process does not own"},
		{liveSession, "a live process's session token"},
		{liveAccountCounter, "a live process's per-account failure counter"},
	} {
		if n, err := rdb.Exists(ctx, tc.key).Result(); err != nil || n != 1 {
			t.Errorf("%s was deleted from the shared DB: %q (exists=%d err=%v)", tc.what, tc.key, n, err)
		}
	}
}

// TestLivePeerSeedKeysArePerProcess is the deterministic half of the concurrency
// fix above. The reclaim guard's own flake is probabilistic (4 of 100 concurrent
// binaries on a fixed name), so a rate measurement alone cannot keep it from
// coming back; this pins the property that removes it, in one process.
//
// The property is NEGATIVE, which is why it needs stating: the two seeded keys
// must be names no OTHER live process can also produce. That is the difference
// between this fixture and the whole-prefix sweep it replaced — a fixed literal
// satisfies every assertion in the guard (the shapes are right, the keys exist)
// and is still a cross-process clobber, because the cleanup deletes BY NAME.
func TestLivePeerSeedKeysArePerProcess(t *testing.T) {
	pid := uint32(os.Getpid())
	session, account := livePeerSeedKeys(pid)

	if why := livePeerSeedShapesOK(session, account, pid); why != "" {
		t.Errorf("the fixture's seeds do not keep the shapes the reclaim guard asserts on: %s", why)
	}

	// The names a concurrent `go test` binary would seed MUST differ from ours,
	// for the pids it can actually hold beside us: processes started at the same
	// instant hold adjacent pids (and the +256 block-adjacent case is the one a
	// single-octet derivation aliases on).
	for _, peer := range []uint32{pid + 1, pid + 2, pid + 3, pid + 7, pid + 8, pid + 256, pid + 1000} {
		peerSession, peerAccount := livePeerSeedKeys(peer)
		if peerSession == session {
			t.Errorf("pid %d and pid %d seed the same session key %q — one process's cleanup deletes the other's live seed", pid, peer, session)
		}
		if peerAccount == account {
			t.Errorf("pid %d and pid %d seed the same account-counter key %q — one process's cleanup deletes the other's live seed", pid, peer, account)
		}
	}

	// Positive control: the two checks above must be able to FAIL. The literals
	// this change replaced are the exact revert a future edit would make, so
	// assert the shape check rejects them — otherwise the assertions above could
	// be vacuous and a revert would surface only as a 4-in-100 flake.
	fixedSession := "jf:session:PEER-LIVE-SESSION"
	fixedAccount := "jf:loginfail:user:peer-live@example.test"
	if livePeerSeedShapesOK(fixedSession, fixedAccount, pid) == "" {
		t.Errorf("control: the fixed literals %q / %q pass the shape check, so the check cannot catch a revert to them", fixedSession, fixedAccount)
	}
	if fixedSession == session || fixedAccount == account {
		t.Errorf("the fixture seeds a fixed literal (%q / %q) — a concurrent run deletes it by name", fixedSession, fixedAccount)
	}
}

// livePeerSeedShapesOK reports the first way a pair of seed keys fails to carry
// the shapes TestReclaimLeavesPeersLoginCountersAlone reads, or "" when both are
// in shape. Split out so the control above can prove it rejects the fixed
// literals, rather than the check being asserted only against the values it was
// written for.
func livePeerSeedShapesOK(session, account string, pid uint32) string {
	if !strings.HasPrefix(session, "jf:session:") {
		return fmt.Sprintf("session seed %q is not under the production `jf:session:` prefix the assertion reads", session)
	}
	if !strings.HasPrefix(account, "jf:loginfail:user:") {
		return fmt.Sprintf("account-counter seed %q is not under the production `jf:loginfail:user:` prefix the assertion reads", account)
	}
	own := strconv.Itoa(int(pid))
	if !strings.HasSuffix(session, "-"+own) {
		return fmt.Sprintf("session seed %q does not encode this process's pid — a fixed name is what two concurrent runs collide on", session)
	}
	if !strings.Contains(account, own) {
		return fmt.Sprintf("account-counter seed %q does not encode this process's pid — a fixed name is what two concurrent runs collide on", account)
	}
	return ""
}

// TestOwnLoginCounterKeyRejectsForeignAndMalformedKeys is the unit half: the
// predicate must accept exactly the addresses uniqueTestIP can produce and
// reject everything else, including a near-miss that merely shares the prefix.
func TestOwnLoginCounterKeyRejectsForeignAndMalformedKeys(t *testing.T) {
	family := testIPFamilyFor(uint32(os.Getpid()))
	prefix := "jf:loginfail:ip:"

	for _, tc := range []struct {
		key  string
		want bool
	}{
		{prefix + family + "1", true},                // our first address
		{prefix + family + "254", true},              // our last address
		{prefix + family + "0", false},               // index 0 is never handed out
		{prefix + family + "255", false},             // beyond the per-process bound
		{prefix + family + "01", false},              // leading zero: not a shape we emit
		{prefix + family + "1:extra", false},         // a longer key that merely starts alike
		{prefix + "10.99.99.1", false},               // another process's /24
		{prefix + "not-an-ip", false},                //
		{"jf:loginfail:user:" + family + "1", false}, // per-account key, not per-IP
	} {
		if got := ownLoginCounterKey(prefix, tc.key, family); got != tc.want {
			t.Errorf("ownLoginCounterKey(%q) = %v, want %v", tc.key, got, tc.want)
		}
	}
}

// peerCounterKey returns the login-counter key the reclaim guard seeds to stand
// in for a counter under a PEER's /24: a live counter this process's scoped
// reclaim must leave alone.
//
// The address is deliberately outside every /24 testIPFor can hand a process.
// That helper only ever produces 10.<pid low>.<pid high>.<n>, so each of its 65536
// /24s is owned by some pid (pid mod 65536), and a key inside that space belongs to
// a process that can really be running: on this host, `go test` binaries started
// together hold pids 1-8 apart, with an adjacent pair (gap 1) measured across 80
// simultaneous binaries.
//
// This key used to be `testIPFamilyFor(pid+1)+"1"` — pid+1's own FIRST test
// address. The process holding pid+1 therefore reclaimed it as one of ITS OWN
// stale counters (its SCAN is `prefix+family+"*"`, and ownLoginCounterKey accepts
// the key under that family) while this process asserted it must still exist: a
// false red the moment two binaries landed on adjacent pids, with no defect behind
// it. That is a latent red, not a live one — it did not fire in 0/320 concurrent
// guard runs — but the assertion has to hold for every pid pair, not the pairs a
// measurement happened to hit.
//
// 198.0.0.0/8 is outside every family, so no process's reclaim can claim this key
// (TestPeerCounterSentinelBelongsToNoProcess proves it over the whole pid space).
// The pid in the address is what keeps two concurrent runs of this test from
// deleting each other's seed by name in t.Cleanup, exactly as livePeerSeedKeys'
// pid does — the guard's own cleanup deletes BY NAME, so a shared literal would be
// the same collision in a different place. The pid's low 24 bits are enough:
// pid_max here is 4194304 = 2^22, so distinct live pids always derive distinct
// addresses.
func peerCounterKey(pid uint32) string {
	return fmt.Sprintf("%s198.%d.%d.%d", loginCounterKeyPrefixes[0],
		(pid>>16)&0xff, (pid>>8)&0xff, pid&0xff)
}

// pidsClaimingLoginCounter returns the pids whose own scoped reclaim would claim
// key as one of its own stale counters: the pids p for which ownLoginCounterKey
// accepts key under testIPFamilyFor(p). An empty result means no process — present
// or future — can delete the key, which is the property any key this suite asserts
// must SURVIVE has to have.
//
// The range is exhaustive rather than a sample of offsets: testIPFamilyFor maps pid
// mod 65536 onto the 65536 possible /24s one-to-one, so [0, 0xffff] covers every
// family a reclaim can ever scope to, and pid_max (4194304 = 2^22) is below that
// modulus, so a live pid always has its representative in the range.
func pidsClaimingLoginCounter(prefix, key string) []uint32 {
	var out []uint32
	for p := uint32(0); p <= 0xffff; p++ {
		if ownLoginCounterKey(prefix, key, testIPFamilyFor(p)) {
			out = append(out, p)
		}
	}
	return out
}

// TestPeerCounterSentinelBelongsToNoProcess is the deterministic half of the peer
// sentinel's fix. The defect it removes is probabilistic in the guard above (it
// needs two binaries on adjacent pids), so a rate measurement alone cannot keep it
// from coming back; this pins the property that removes it, in one process.
//
// The property is NEGATIVE, which is why it needs stating: the sentinel must be a
// key no process's reclaim can claim. The derivation it replaced satisfies every
// shape check the guard asserts on — it is a well-formed `ip:` counter, and it
// exists where the guard wants it — and is still wrong, because another process
// owns it.
func TestPeerCounterSentinelBelongsToNoProcess(t *testing.T) {
	pid := uint32(os.Getpid())
	prefix := loginCounterKeyPrefixes[0]
	sentinel := peerCounterKey(pid)

	// The whole-prefix sweep this suite forbids deletes by PREFIX, so the sentinel
	// has to be a key that sweep would match — the `<prefix><address>` counter shape
	// the breaker writes — or the bite goes quiet the day someone restores the sweep.
	if addr := strings.TrimPrefix(sentinel, prefix); addr == sentinel || !literalIPAddress.MatchString(addr) {
		t.Errorf("the peer sentinel %q is not a `%s<address>` login counter — a whole-prefix sweep would not match it, so the bite would stop witnessing the scoping", sentinel, prefix)
	}

	claimed := pidsClaimingLoginCounter(prefix, sentinel)
	if len(claimed) > 0 {
		t.Errorf("the peer sentinel %q is claimed by the scoped reclaim of pid ≡ %d (mod 65536), which deletes it as one of its own stale counters while the guard asserts it survived", sentinel, claimed[0])
	}

	// The name carries the pid for the same reason the other two seeds do: the
	// guard's t.Cleanup deletes this key BY NAME, so a name two runs share lets one
	// delete the seed the other is asserting on.
	for _, other := range []uint32{pid + 1, pid + 7, pid + 256, pid + 1000} {
		if peerCounterKey(other) == sentinel {
			t.Errorf("pid %d and pid %d derive the same peer sentinel %q — one run's cleanup deletes the other's live seed", pid, other, sentinel)
		}
	}

	// Positive control: the check above must be able to FAIL on the shape it
	// replaced, or it is asserting nothing. The pid+1 derivation is claimed by
	// exactly the process whose /24 it sits in — that IS the false red.
	old := prefix + testIPFamilyFor(pid+1) + "1"
	oldClaimed := pidsClaimingLoginCounter(prefix, old)
	if len(oldClaimed) != 1 || testIPFamilyFor(oldClaimed[0]) != testIPFamilyFor(pid+1) {
		t.Fatalf("control: the pid+1 derivation %q must be claimed by exactly the process whose /24 it sits in; the scan reported pids %v", old, oldClaimed)
	}
}
