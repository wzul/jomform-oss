// Guard: the feedback-notify queue fixture must not hand its assertions a key
// set another test PROCESS can also write.
//
// The fixture (liveQueue, feedback_notify_failure_test.go) used to pin
// `const feedbackNotifyKeyspace = 14` and FlushDB it at entry, while building
// its queue with emailqueue.NewQueue — the PRODUCTION key literals
// (`jf:emailq:jobs` and its siblings). Both halves are shared resources: the DB
// number with every other process pointed at that index, and the KEY NAMES with
// every other process on the URL, whatever index it chose. `go test ./...` runs
// from several worktrees on this host at once against the same local Valkey
// (the normal state of this board), so a peer's flush deleted the job the
// assertion was about and the suite failed on a branch whose diff never touched
// feedback:
//
//	feedback_notify_issue_test.go:304: admin reply enqueued 0 job(s), want exactly 1
//
// This test pins the property that removes it. Its positive control makes sure
// the assertions cannot pass for any name at all, and the last block drives the
// REAL fixture so the check is on the keys the tests use, not only on a
// reconstruction of them.
package httpapi

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/emailqueue"
	"github.com/wzul/jomform/api/internal/testdb"
)

// feedbackQueueOwnershipProblem returns "" when q's keys belong to the test
// process with this pid running the test with this name, or a sentence naming
// what a concurrent run would collide on.
//
// It is a shape check, and deliberately not a "is anything else connected?"
// probe: ownership is a property of the NAME. A peer that writes the same name
// collides whether or not it happens to be connected this instant.
func feedbackQueueOwnershipProblem(q *emailqueue.Queue, pid int, name string) string {
	key := q.ReadyKey()
	if strings.HasPrefix(key, emailqueue.DefaultKeyPrefix) {
		return fmt.Sprintf("ready key %q sits under the production prefix %q — every process on this Valkey writes "+
			"that exact key, including a peer suite built from the old fixture and a live API worker", key,
			emailqueue.DefaultKeyPrefix)
	}
	namespace := fmt.Sprintf("jftest:p%d:", pid)
	if !strings.Contains(key, namespace) {
		return fmt.Sprintf("ready key %q does not carry this process's namespace %q — a name two processes share "+
			"is what concurrent runs collide on: one run's flush deletes the other's job mid-test", key, namespace)
	}
	// Compare against the SAME component the key was built with. testdb.PrefixFor
	// builds it with testdb.KeyPart: the readable sanitised form PLUS a hash of
	// the raw name, because sanitising alone maps every character outside
	// [A-Za-z0-9_-] to '_' and is therefore not injective — two names can
	// sanitise to one string (see the injectivity pin below). A subtest's name
	// carries a '/', so a check against the raw t.Name() can never match either,
	// and a correct tree reds for any subtest or -run name carrying a slash. The
	// rule is testdb's own, not a second copy of it.
	part := testdb.KeyPart(name)
	if !strings.Contains(key, part+":") {
		return fmt.Sprintf("ready key %q does not carry this TEST's own name %q (key namespace component %q) — "+
			"without it two tests in one binary share the queue the old flush-at-entry emptied between them",
			key, name, part)
	}
	return ""
}

// TestFeedbackNotifyQueueKeysAreOwnedByThisProcess is the cross-process guard for
// the fix above.
func TestFeedbackNotifyQueueKeysAreOwnedByThisProcess(t *testing.T) {
	pid := os.Getpid()

	// The rule, built the way the fixture builds it: a per-TEST key namespace
	// derived from THIS process's pid (testdb.PrefixFor → testdb.Prefix). No
	// Valkey is needed for this half, so the guard runs in every `go test ./...`
	// rather than only where the env is set — a guard that skips is a guard that
	// stops guarding.
	own := emailqueue.NewQueueWithPrefix(nil, testdb.PrefixFor(t))
	mine := own.ReadyKey()
	if why := feedbackQueueOwnershipProblem(own, pid, t.Name()); why != "" {
		t.Errorf("the queue namespace the fixture builds is not owned by this process: %s", why)
	}

	// Regression pin: the same construction one level down, where the test name
	// is a SUBTEST PATH. The key is built from testdb.KeyPart(t.Name()) and a
	// subtest's name carries a '/', so a check that compared against the raw
	// t.Name() red a correct tree here — the failure read as a namespace bug, not
	// as the sanitisation mismatch it was, and a wrapped body would have hit it.
	// It is a subtest on purpose: a top-level name has no '/' and cannot catch
	// this.
	//
	// The parent's own key part is taken from the test OBJECT before t.Run, not
	// spelled out as a literal: a hard-coded parent name makes the detector inert
	// the moment this function is renamed — the collapse stays live while the
	// assertion names a test that no longer exists (measured on card t_e26669bd:
	// collapse + rename left this file green before, and is red now).
	parentPart := testdb.KeyPart(t.Name())
	t.Run("subtest", func(t *testing.T) {
		sub := emailqueue.NewQueueWithPrefix(nil, testdb.PrefixFor(t))
		if why := feedbackQueueOwnershipProblem(sub, pid, t.Name()); why != "" {
			t.Errorf("a subtest name is reported as not owned by this process: %s", why)
		}
		// The name leg must still see this SUBTEST as distinct from its parent:
		// deriving the key part must not collapse the two names into one.
		subPart := testdb.KeyPart(t.Name())
		if subPart == parentPart {
			t.Fatalf("the subtest's key part %q equals its parent's %q — the per-test name is no longer "+
				"distinguishing the two, which is the isolation the name exists to give", subPart, parentPart)
		}
		if sub.ReadyKey() == mine {
			t.Fatalf("the subtest's ready key %q equals the parent test's %q — one namespace for two tests", sub.ReadyKey(), mine)
		}
		if !strings.Contains(sub.ReadyKey(), subPart+":") {
			t.Errorf("ready key %q does not carry this subtest's key part %q", sub.ReadyKey(), subPart)
		}
	})

	// INJECTIVITY PIN (card t_e26669bd): the per-test component must distinguish
	// two DIFFERENT names. The raw NAME is not enough — testdb.SanitizeKeyPart
	// maps everything outside [A-Za-z0-9_-] to '_' — so "X/a" and "X_a" sanitise
	// to ONE string, and under the plain sanitised component those two tests share
	// one queue namespace: the isolation failure this whole guard is about (two
	// tests in one binary emptying each other's queue), arriving through the NAME
	// instead of through the pid. The ownership leg is blind to it by construction
	// — it compares the key against that component, so it accepts BOTH keys for
	// BOTH names, i.e. it reports a collided namespace as correctly owned.
	// testdb.KeyPart closes it by hashing the RAW name, and these assertions keep
	// that from being undone: with the derivation neutralised back to the bare
	// sanitised name, every other assertion in this file stays green.
	for _, pair := range [][2]string{
		{"X/a", "X_a"},             // the pair from the card
		{"TestX/a/b", "TestX/a_b"}, // subtest paths, where the collision is easiest to hit
		{"TestX/a", "TestX:a"},     // '/' against another character the rule maps
	} {
		readableA, readableB := testdb.SanitizeKeyPart(pair[0]), testdb.SanitizeKeyPart(pair[1])
		if readableA != readableB {
			t.Fatalf("the names %q and %q no longer sanitise to the same string (%q vs %q), so this pin is "+
				"not exercising a collision at all — fix the pair, do not delete the pin", pair[0], pair[1], readableA, readableB)
		}
		a := emailqueue.NewQueueWithPrefix(nil, testdb.PrefixForName(pair[0]))
		b := emailqueue.NewQueueWithPrefix(nil, testdb.PrefixForName(pair[1]))
		if a.ReadyKey() == b.ReadyKey() {
			t.Fatalf("the two names %q and %q build ONE ready key %q (both sanitise to %q) — two tests in one "+
				"binary share that queue and empty it for each other, which is the isolation the per-test name "+
				"exists to give", pair[0], pair[1], a.ReadyKey(), readableA)
		}
		if why := feedbackQueueOwnershipProblem(b, pid, pair[0]); why == "" {
			t.Fatalf("the ownership check ACCEPTS %q (built for %q) as this test's own name %q — the two names "+
				"share a key part, so the leg reports a collided namespace as owned by both. %q and %q sanitise "+
				"to %q; the component must not be the sanitised form alone",
				b.ReadyKey(), pair[1], pair[0], pair[0], pair[1], readableA)
		}
	}

	// Peer processes: same package, same Valkey, different pids. Our key must
	// satisfy OUR pid's check and no peer's, which is what stops one run's flush
	// from landing on another's job. The pids are the ones a burst of processes
	// started at the same instant actually holds on this host (pid_max is
	// 4194304, so neighbours and the +256 block both occur).
	for _, peer := range []int{pid + 1, pid + 2, pid + 7, pid + 256, pid + 1000} {
		if feedbackQueueOwnershipProblem(own, peer, t.Name()) == "" {
			t.Errorf("ready key %q satisfies pid %d's ownership check as well as this process's (%d) — two "+
				"concurrent runs would share one key set", mine, peer, pid)
		}
	}

	// POSITIVE CONTROL (per-test name): the shape check must also REJECT a key
	// that carries this process's pid but NOT this test's name, or the name leg
	// can be deleted without any gate noticing. Prefix() is the pid-only half of
	// PrefixFor, built through the same constructor the fixture uses.
	noName := emailqueue.NewQueueWithPrefix(nil, testdb.Prefix())
	if noName.ReadyKey() == mine {
		t.Fatalf("the pid-only namespace %q equals this test's own key %q — this control cannot exercise the "+
			"name leg because PrefixFor no longer adds the test's name to Prefix", noName.ReadyKey(), mine)
	}
	if feedbackQueueOwnershipProblem(noName, pid, t.Name()) == "" {
		t.Fatalf("the ownership check accepts the process-only key %q (testdb.Prefix() with no test name) — "+
			"a name two tests in one binary share is what the old flush-at-entry emptied between them",
			noName.ReadyKey())
	}

	// POSITIVE CONTROL (production prefix): the shape check must REJECT the
	// pre-fix fixture, or every assertion above would pass for any name
	// whatsoever. NewQueue is exactly what the old fixture built its queue with.
	preFix := emailqueue.NewQueue(nil)
	if feedbackQueueOwnershipProblem(preFix, pid, t.Name()) == "" {
		t.Fatalf("the ownership check accepts the production key %q (emailqueue.NewQueue) — the pre-fix fixture "+
			"(fixed DB number + FlushDB + production literals) would pass this guard, so it guards nothing",
			preFix.ReadyKey())
	}

	// POSITIVE CONTROL (production prefix), ISOLATING (card t_aae913ea): the
	// preFix control above is satisfied by the PID leg ALONE — `jf:emailq:jobs`
	// carries no `jftest:p<pid>:` namespace either — so neutralising or deleting
	// the prefix leg left every gate green. This control satisfies every OTHER
	// leg on purpose, so only the prefix rule can reject it:
	//
	//   `jf:emailq:` + PrefixFor(t)  ->  starts with the production prefix,
	//   still carries this process's `jftest:p<pid>:` namespace, and still
	//   carries this test's sanitised name.
	//
	// The assertion names the rule it expects, not merely "some problem": an
	// assertion for "not empty" would be satisfied by the pid leg again, which is
	// the exact shape of the hole this control closes.
	prefixOnly := emailqueue.NewQueueWithPrefix(nil, emailqueue.DefaultKeyPrefix+testdb.PrefixFor(t))
	if prefixOnly.ReadyKey() == mine {
		t.Fatalf("the production-prefixed key %q equals this test's own key %q — this control cannot exercise "+
			"the prefix leg", prefixOnly.ReadyKey(), mine)
	}
	if !strings.Contains(prefixOnly.ReadyKey(), testdb.Prefix()) {
		t.Fatalf("the production-prefixed key %q no longer carries this process's namespace %q, so the pid leg "+
			"would reject it and the control would prove nothing about the prefix rule",
			prefixOnly.ReadyKey(), testdb.Prefix())
	}
	if !strings.Contains(prefixOnly.ReadyKey(), testdb.KeyPart(t.Name())+":") {
		t.Fatalf("the production-prefixed key %q no longer carries this test's key part %q, so the NAME leg "+
			"would reject it and the control would prove nothing about the prefix rule",
			prefixOnly.ReadyKey(), testdb.KeyPart(t.Name()))
	}
	whyPrefix := feedbackQueueOwnershipProblem(prefixOnly, pid, t.Name())
	if whyPrefix == "" {
		t.Fatalf("the ownership check ACCEPTS %q — a key under the production prefix %q that satisfies the pid "+
			"and name legs. Every process on this Valkey writes that prefix, including a peer suite built from "+
			"the old fixture and a live API worker, so the prefix rule is the only thing standing between a test "+
			"and the production queue; without this control it can be neutralised or deleted with every gate "+
			"still green", prefixOnly.ReadyKey(), emailqueue.DefaultKeyPrefix)
	}
	if !strings.Contains(whyPrefix, "production prefix") {
		t.Fatalf("the ownership check rejected %q for the WRONG reason: %q. This key satisfies the pid and name "+
			"legs by construction, so the verdict must come from the production-prefix rule",
			prefixOnly.ReadyKey(), whyPrefix)
	}

	// And the REAL fixture, where the env lets it be built. PrefixFor(t) above and
	// liveQueue(t) below are the same test object, so the keys must match exactly:
	// this is what ties the reconstruction to the queue the assertions read.
	if os.Getenv(testdb.ValkeyEnv) == "" {
		t.Logf("%s not set: checked the namespace the fixture builds (%q); the built fixture was not exercised",
			testdb.ValkeyEnv, mine)
		return
	}
	q, _ := liveQueue(t)
	if q.ReadyKey() != mine {
		t.Errorf("liveQueue's ready key = %q, want the owned namespace %q — the fixture the feedback tests read "+
			"must be the one this guard describes", q.ReadyKey(), mine)
	}
	if why := feedbackQueueOwnershipProblem(q, pid, t.Name()); why != "" {
		t.Errorf("liveQueue's queue is not owned by this process: %s", why)
	}
}
