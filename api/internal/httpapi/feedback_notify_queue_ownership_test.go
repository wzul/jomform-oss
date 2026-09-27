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
	if !strings.Contains(key, name+":") {
		return fmt.Sprintf("ready key %q does not carry this TEST's own name %q — without it two tests in one "+
			"binary share the queue the old flush-at-entry emptied between them", key, name)
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
