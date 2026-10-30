// Email queue + worker unit tests (issue #199).
//
// These exercise the Valkey-backed queue and the worker's retry ladder against
// a real Valkey at JOMFORM_TEST_VALKEY_URL (skipped in CI, mirroring the
// breaker tests). They verify: enqueue → dequeue round-trip, durable delayed
// promotion, and that a failing handler is retried with backoff then dropped.
package emailqueue

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/wzul/jomform/api/internal/testdb"
)

// testQueue returns a queue on this test's own key namespace, plus the client
// backing it (issue #21aec38d).
//
// The previous shape pinned DB /3 and FlushDB'd it. A fixed DB number is not
// owned by the test binary — a live API or a second suite configured on that
// number shares it, and the flush deleted their keys. A namespace of the test's
// own is the same isolation without any delete; see testdb/valkey.go.
func testQueue(t *testing.T) (*Queue, *goredis.Client) {
	t.Helper()
	// The namespace comes from testdb.Valkey, which is per TEST — the isolation
	// the old FlushDB was standing in for, without deleting anything else's keys.
	// See testdb/valkey.go.
	rdb, prefix := testdb.Valkey(t)
	return NewQueueWithPrefix(rdb, prefix), rdb
}

// TestEnqueueDequeueRoundTrip verifies a job enqueued is dequeued intact.
func TestEnqueueDequeueRoundTrip(t *testing.T) {
	q, _ := testQueue(t)
	ctx := context.Background()

	job := Job{Type: "verify", To: "a@b.c", Subject: "S", Body: "B", AccountID: "u1"}
	if err := q.Enqueue(ctx, job); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	got, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	if got.Type != "verify" || got.To != "a@b.c" || got.Subject != "S" || got.Body != "B" || got.AccountID != "u1" {
		t.Fatalf("dequeued job mismatch: %+v", got)
	}
}

// TestDelayedPromotion verifies a delayed job stays out of the ready list until
// its due time passes, and is promoted once it has.
//
// The clock is MOVED here, never waited on. "A job that is not due is not
// promoted" is a property of the queue; a wall-clock margin between
// EnqueueDelayed and PromoteDue only makes it a property of how fast the test
// process was scheduled. The old body used a 1100ms delay and slept 1200ms, and
// that margin is the whole of the separation between the two calls: under the
// full -race suite (httpapi/chat/dbstore running concurrently on a shared host)
// a stall past ~1.1s makes the job legitimately due, PromoteDue does the correct
// thing, and the test failed with "queue len = 1, want 0 before due" — a red
// verdict for a clean tree. (Unix() second granularity made the real margin
// anywhere from 0 to 1100ms, depending on where the enqueue landed.)
//
// So: enqueue an hour out, assert the job is sitting in the delayed set and NOT
// in the ready list, then write the score the clock would have written an hour
// later and assert it is promoted — with no sleep in the passing path.
func TestDelayedPromotion(t *testing.T) {
	q, rdb := testQueue(t)
	ctx := context.Background()

	job := Job{Type: "receipt", To: "x@y.z", Subject: "R", Body: "B"}
	// An hour out: no plausible stall in this suite can reach it, so "not yet
	// due" holds for the whole test instead of for the next second.
	if err := q.EnqueueDelayed(ctx, job, time.Hour); err != nil {
		t.Fatalf("enqueue delayed: %v", err)
	}
	// The job really is waiting in the delayed set — without this, the Len == 0
	// assertion below would also pass for an enqueue that wrote nothing at all.
	if n, err := rdb.ZCard(ctx, q.DelayedKey()).Result(); err != nil || n != 1 {
		t.Fatalf("delayed set len = %d (err %v), want 1 after enqueue", n, err)
	}
	// Not yet due → nothing to promote.
	if err := q.PromoteDue(ctx); err != nil {
		t.Fatalf("promote due: %v", err)
	}
	if n, _ := q.Len(ctx); n != 0 {
		t.Fatalf("queue len = %d, want 0 before due", n)
	}
	// Move the due time into the past: the exact state an hour of wall clock
	// would produce in the delayed set, written directly instead of waited for.
	members, err := rdb.ZRange(ctx, q.DelayedKey(), 0, -1).Result()
	if err != nil || len(members) != 1 {
		t.Fatalf("delayed members = %d (err %v), want 1 to move", len(members), err)
	}
	if err := rdb.ZAdd(ctx, q.DelayedKey(), goredis.Z{
		Score:  float64(time.Now().Add(-time.Minute).Unix()),
		Member: members[0],
	}).Err(); err != nil {
		t.Fatalf("move due time: %v", err)
	}
	if err := q.PromoteDue(ctx); err != nil {
		t.Fatalf("promote due: %v", err)
	}
	if n, _ := q.Len(ctx); n != 1 {
		t.Fatalf("queue len = %d, want 1 after due", n)
	}
	// The job that was promoted is the one that was waiting, not just "something",
	// and it left the delayed set.
	if got, err := rdb.LIndex(ctx, q.ReadyKey(), 0).Result(); err != nil || got != members[0] {
		t.Fatalf("ready job = %q (err %v), want the delayed member %q", got, err, members[0])
	}
	if n, _ := rdb.ZCard(ctx, q.DelayedKey()).Result(); n != 0 {
		t.Fatalf("delayed set len = %d, want 0 after promotion", n)
	}
}

// failingHandler always errors, so the worker retries then drops.
type failingHandler struct{ calls atomic.Int32 }

func (h *failingHandler) Send(ctx context.Context, job Job) error {
	h.calls.Add(1)
	return errors.New("ses down")
}

// workerWaitBudget bounds every state wait in the worker-ladder tests below.
//
// It is a FAILURE bound, not a pass condition: the fixed path drives the retry
// itself and takes milliseconds, so reaching this bound means the worker did
// not do its job, not that the host was slow. A fixed budget used to be the
// PASS condition here, which is what made the verdict a property of the
// scheduler (see TestWorkerRetriesThenDrops).
const workerWaitBudget = 5 * dequeueTimeout

// runRetryLadder waits, one attempt at a time, for a failing job to be retried
// maxAttempts times, driving the promotion of each scheduled retry instead of
// leaving it to the worker's blocking dequeue.
//
// Why it drives: the worker promotes due retries only at the top of its loop,
// immediately before a BLMove that blocks for dequeueTimeout (queue.go:443),
// and EnqueueDelayed scores a member at UNIX SECOND granularity (queue.go:207).
// A sub-second backoff written in the last few ms of a second therefore scores
// the NEXT second, is not due at that PromoteDue, and its promotion costs the
// full 2s block. Waiting it out is waiting on the sub-second alignment of the
// failure — the clock, not the queue. TestDelayedPromotion states the same rule
// for the due time ("the clock is MOVED here, never waited on"); this is the
// same move applied to the retry ladder.
func runRetryLadder(t *testing.T, q *Queue, rdb *goredis.Client, calls func() int32, maxAttempts int) {
	t.Helper()
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		waitForCalls(t, calls, int32(attempt), maxAttempts)
		if attempt == maxAttempts {
			// The last failure dead-letters instead of re-enqueueing. Wait for
			// that record: waitForCalls returns when the handler is ENTERED, and
			// retry() writes the dead letter after the handler returns.
			waitForDeadLetter(t, q, 1)
			return
		}
		promoteScheduledRetry(t, q, rdb, calls, int32(attempt))
	}
}

// waitForDeadLetter blocks until the queue holds `want` dead-letter records.
func waitForDeadLetter(t *testing.T, q *Queue, want int64) {
	t.Helper()
	bg := context.Background()
	deadline := time.Now().Add(workerWaitBudget)
	var last int64 = -1
	for time.Now().Before(deadline) {
		n, err := q.DeadLen(bg)
		if err != nil {
			t.Fatalf("dead-letter len: %v", err)
		}
		last = n
		if n >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("dead-letter len = %d, want %d within %s — an exhausted job must be preserved "+
		"for inspection, not silently dropped", last, want, workerWaitBudget)
}

// waitForCalls blocks until the handler has been entered `want` times.
func waitForCalls(t *testing.T, calls func() int32, want int32, maxAttempts int) {
	t.Helper()
	deadline := time.Now().Add(workerWaitBudget)
	for time.Now().Before(deadline) {
		if calls() >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("handler calls = %d, want %d of %d attempts within %s — the worker loop stopped "+
		"retrying (it enqueues the retry into the delayed set and promotes it at the top of its "+
		"loop); this is not a slow host, the fixed path is driven by promoteScheduledRetry",
		calls(), want, maxAttempts, workerWaitBudget)
}

// promoteScheduledRetry waits for the failed attempt's retry to be sitting in the
// delayed set, writes the due time the clock would have written once the backoff
// elapsed, and promotes it. The worker's blocked BLMove wakes on that push.
//
// If the worker's own PromoteDue claimed the member first, the set is already
// empty and the next attempt starts on its own — nothing to drive.
func promoteScheduledRetry(t *testing.T, q *Queue, rdb *goredis.Client, calls func() int32, attempt int32) {
	t.Helper()
	bg := context.Background()
	deadline := time.Now().Add(workerWaitBudget)
	scheduled := false
	for time.Now().Before(deadline) {
		n, err := rdb.ZCard(bg, q.DelayedKey()).Result()
		if err != nil {
			t.Fatalf("read delayed set: %v", err)
		}
		if n > 0 {
			scheduled = true
			break
		}
		if calls() > attempt {
			return // promoted by the worker before this test looked
		}
		time.Sleep(time.Millisecond)
	}
	if !scheduled {
		t.Fatalf("attempt %d: no retry in the delayed set within %s — retry() must re-enqueue "+
			"the job there, and this test drives its promotion from that state",
			attempt, workerWaitBudget)
	}
	members, err := rdb.ZRange(bg, q.DelayedKey(), 0, -1).Result()
	if err != nil {
		t.Fatalf("read delayed members: %v", err)
	}
	if len(members) > 0 {
		due := time.Now().Add(-time.Second).Unix()
		zs := make([]goredis.Z, 0, len(members))
		for _, m := range members {
			zs = append(zs, goredis.Z{Score: float64(due), Member: m})
		}
		// XX: only move a member that is still in the set. Without it, a member
		// the worker claimed between the read and this write would be re-added
		// to the delayed set and promoted a SECOND time, which would run the job
		// one extra time.
		if err := rdb.ZAddArgs(bg, q.DelayedKey(), goredis.ZAddArgs{XX: true, Members: zs}).Err(); err != nil {
			t.Fatalf("make retry due: %v", err)
		}
	}
	if err := q.PromoteDue(bg); err != nil {
		t.Fatalf("promote due: %v", err)
	}
}

// TestWorkerRetriesThenDrops verifies a failing job is retried (backoff) and
// dead-lettered after maxAttempts, and that the queue is left empty.
//
// The ladder is driven, not waited out (runRetryLadder says why). The old body
// waited a fixed 3s for 5 attempts, which reddened the full
// `go test ./... -race` gate with "handler calls = 2, want 5" while passing 5/5
// in isolation: with a sub-second backoff, whether a retry was due at the
// worker's next PromoteDue depended on where the failure fell inside a second,
// and a miss cost a full 2s BLMove. So the pass condition was the scheduler's.
//
// What is asserted is the property, not the timing: exactly maxAttempts calls
// (not fewer — the ladder stopped; not more — a retry was promoted twice), one
// dead-letter record carrying those attempts, and nothing left ready, delayed
// or in flight.
func TestWorkerRetriesThenDrops(t *testing.T) {
	q, rdb := testQueue(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := &failingHandler{}
	w := NewWorker(q, h, nil)
	// A real (short) backoff ladder: retry() is exercised with a non-zero delay;
	// the WAIT for it is a moved due time (promoteScheduledRetry), not a sleep.
	w.backoff = []time.Duration{10 * time.Millisecond, 10 * time.Millisecond, 10 * time.Millisecond, 10 * time.Millisecond}

	if err := q.Enqueue(ctx, Job{Type: "verify", To: "a@b.c", Subject: "S", Body: "B"}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()

	runRetryLadder(t, q, rdb, h.calls.Load, w.maxAttempts)

	if got := h.calls.Load(); got != int32(w.maxAttempts) {
		t.Fatalf("handler calls = %d, want %d (max attempts)", got, w.maxAttempts)
	}
	cancel()
	<-done

	// Read with a fresh context: the worker's ctx is cancelled above and these
	// assertions must not race that cancellation.
	bg := context.Background()
	// The job must be DEAD-LETTERED, not merely dropped after N calls: the
	// record is what an operator inspects and replays, and "5 calls" alone would
	// also hold for a loop that retried and never retired the job.
	if n, err := q.DeadLen(bg); err != nil || n != 1 {
		t.Fatalf("dead-letter len = %d (err %v), want 1 — an exhausted job must be preserved "+
			"for inspection, not silently dropped", n, err)
	}
	raw, err := rdb.LIndex(bg, q.DeadKey(), 0).Result()
	if err != nil {
		t.Fatalf("read dead letter: %v", err)
	}
	var rec struct {
		Job      Job    `json:"job"`
		Reason   string `json:"reason"`
		DiedAt   int64  `json:"died_at"`
		Attempts int    `json:"attempts"`
	}
	if err := json.Unmarshal([]byte(raw), &rec); err != nil {
		t.Fatalf("dead letter is not a record: %v (%q)", err, raw)
	}
	if rec.Attempts != w.maxAttempts || rec.Job.Attempts != w.maxAttempts {
		t.Fatalf("dead letter records attempts %d / job.attempts %d, want %d — the retired job "+
			"must carry the count it died at", rec.Attempts, rec.Job.Attempts, w.maxAttempts)
	}
	if rec.Reason == "" || !strings.Contains(rec.Reason, "ses down") {
		t.Fatalf("dead letter reason = %q, want the handler's error", rec.Reason)
	}
	// The job should be gone from both the ready list and the delayed set.
	if n, _ := q.Len(bg); n != 0 {
		t.Fatalf("ready queue len = %d, want 0 after drop", n)
	}
	if n, _ := rdb.ZCard(bg, q.DelayedKey()).Result(); n != 0 {
		t.Fatalf("delayed set len = %d, want 0 after drop", n)
	}
	if n, _ := q.InflightLen(bg); n != 0 {
		t.Fatalf("in-flight len = %d, want 0 after drop — retry() must ack the claim it retires", n)
	}
}
