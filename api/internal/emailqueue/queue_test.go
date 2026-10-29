// Email queue + worker unit tests (issue #199).
//
// These exercise the Valkey-backed queue and the worker's retry ladder against
// a real Valkey at JOMFORM_TEST_VALKEY_URL (skipped in CI, mirroring the
// breaker tests). They verify: enqueue → dequeue round-trip, durable delayed
// promotion, and that a failing handler is retried with backoff then dropped.
package emailqueue

import (
	"context"
	"errors"
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

// TestWorkerRetriesThenDrops verifies a failing job is retried (backoff) and
// dropped after maxAttempts, and that the queue is left empty.
func TestWorkerRetriesThenDrops(t *testing.T) {
	q, rdb := testQueue(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := &failingHandler{}
	w := NewWorker(q, h, nil)
	// Override the backoff ladder to keep the test fast.
	w.backoff = []time.Duration{10 * time.Millisecond, 10 * time.Millisecond, 10 * time.Millisecond, 10 * time.Millisecond}

	if err := q.Enqueue(ctx, Job{Type: "verify", To: "a@b.c", Subject: "S", Body: "B"}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()

	// Wait until the job has been dropped (attempts >= maxAttempts).
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if h.calls.Load() >= int32(w.maxAttempts) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := h.calls.Load(); got != int32(w.maxAttempts) {
		t.Fatalf("handler calls = %d, want %d (max attempts)", got, w.maxAttempts)
	}
	// The job should be gone from both the ready list and the delayed set.
	if n, _ := q.Len(ctx); n != 0 {
		t.Fatalf("ready queue len = %d, want 0 after drop", n)
	}
	if n, _ := rdb.ZCard(ctx, q.DelayedKey()).Result(); n != 0 {
		t.Fatalf("delayed set len = %d, want 0 after drop", n)
	}
	cancel()
	<-done
}
