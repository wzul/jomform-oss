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

// TestDelayedPromotion verifies a delayed job is promoted back to the ready
// list once its due time passes.
func TestDelayedPromotion(t *testing.T) {
	q, _ := testQueue(t)
	ctx := context.Background()

	job := Job{Type: "receipt", To: "x@y.z", Subject: "R", Body: "B"}
	// Use a delay that spans a second boundary (Unix() has second granularity)
	// so the job is genuinely not yet due immediately after enqueue.
	if err := q.EnqueueDelayed(ctx, job, 1100*time.Millisecond); err != nil {
		t.Fatalf("enqueue delayed: %v", err)
	}
	// Not yet due → nothing to promote.
	if err := q.PromoteDue(ctx); err != nil {
		t.Fatalf("promote due: %v", err)
	}
	if n, _ := q.Len(ctx); n != 0 {
		t.Fatalf("queue len = %d, want 0 before due", n)
	}
	time.Sleep(1200 * time.Millisecond)
	if err := q.PromoteDue(ctx); err != nil {
		t.Fatalf("promote due: %v", err)
	}
	if n, _ := q.Len(ctx); n != 1 {
		t.Fatalf("queue len = %d, want 1 after due", n)
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
