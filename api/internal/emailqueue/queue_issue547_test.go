package emailqueue

// Issue #547: the background queue must not LOSE a job whose worker dies between
// claiming it and finishing it.
//
// Previously Dequeue used a destructive BLPOP, so a claimed-but-unfinished job was
// absent from Valkey entirely — a deploy/OOM/reboot in that window lost it
// silently, and the reaper did not exist. There was also no dead-letter list: a job
// that exhausted its attempts was logged and discarded.
//
// These tests FAIL against the pre-fix code.

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestIssue547ClaimSurvivesWorkerDeath is the core guard: a job claimed by a worker
// that never acks must still exist and be retryable.
func TestIssue547ClaimSurvivesWorkerDeath(t *testing.T) {
	rdb := testClient(t)
	q := NewQueue(rdb)
	ctx := context.Background()

	job := Job{Type: "receipt", To: "buyer@example.test", Subject: "S", Body: "B"}
	if err := q.Enqueue(ctx, job); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// A worker claims it...
	got, raw, err := q.DequeueRaw(ctx)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if got.Type != "receipt" {
		t.Fatalf("claimed wrong job: %+v", got)
	}
	if raw == "" {
		t.Fatal("claim returned no raw blob, so it cannot be acked")
	}
	// ...and the process dies before acking. The job must still be recorded.
	n, err := q.InflightLen(ctx)
	if err != nil {
		t.Fatalf("inflight len: %v", err)
	}
	if n != 1 {
		t.Fatalf("issue #547: in-flight count = %d, want 1 — the claimed job must remain "+
			"recorded until it is acked, or a worker crash loses it", n)
	}
	if ready, _ := q.Len(ctx); ready != 0 {
		t.Fatalf("ready len = %d, want 0 (it was claimed)", ready)
	}

	// Now the reaper (which runs on the next worker's start) must return it.
	reaped, err := q.ReapInflight(ctx, 0) // timeout 0: treat the claim as stale
	if err != nil {
		t.Fatalf("reap: %v", err)
	}
	if reaped != 1 {
		t.Fatalf("issue #547: reaped %d jobs, want 1 — an abandoned claim must be retried", reaped)
	}
	if n, _ := q.InflightLen(ctx); n != 0 {
		t.Fatalf("in-flight after reap = %d, want 0", n)
	}
	if ready, _ := q.Len(ctx); ready != 1 {
		t.Fatalf("ready after reap = %d, want 1 (the job is retryable again)", ready)
	}
	// And it is claimable again, intact.
	again, _, err := q.DequeueRaw(ctx)
	if err != nil {
		t.Fatalf("re-claim: %v", err)
	}
	if again.Type != "receipt" || again.To != "buyer@example.test" {
		t.Fatalf("re-claimed job mismatch: %+v", again)
	}
	if again.ClaimedAt == 0 {
		t.Fatal("re-claimed job should carry a fresh ClaimedAt")
	}
}

// TestIssue547AckClearsInflight proves a successful job leaves nothing behind.
func TestIssue547AckClearsInflight(t *testing.T) {
	rdb := testClient(t)
	q := NewQueue(rdb)
	ctx := context.Background()

	if err := q.Enqueue(ctx, Job{Type: "verify", To: "a@b.test", Subject: "S", Body: "B"}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	_, raw, err := q.DequeueRaw(ctx)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := q.Ack(ctx, raw); err != nil {
		t.Fatalf("ack: %v", err)
	}
	if n, _ := q.InflightLen(ctx); n != 0 {
		t.Fatalf("in-flight after ack = %d, want 0", n)
	}
	if ready, _ := q.Len(ctx); ready != 0 {
		t.Fatalf("ready after ack = %d, want 0", ready)
	}
}

// TestIssue547FreshClaimIsNotReaped proves the visibility timeout protects a job
// that is legitimately still running.
func TestIssue547FreshClaimIsNotReaped(t *testing.T) {
	rdb := testClient(t)
	q := NewQueue(rdb)
	ctx := context.Background()

	if err := q.Enqueue(ctx, Job{Type: "verify", To: "a@b.test", Subject: "S", Body: "B"}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if _, _, err := q.DequeueRaw(ctx); err != nil {
		t.Fatalf("claim: %v", err)
	}
	// With the real visibility timeout, a claim just taken must NOT be reaped,
	// or a slow handler would be duplicated mid-flight.
	reaped, err := q.ReapInflight(ctx, VisibilityTimeout)
	if err != nil {
		t.Fatalf("reap: %v", err)
	}
	if reaped != 0 {
		t.Fatalf("issue #547: reaped %d fresh claims, want 0 — the visibility timeout must "+
			"protect a job whose handler is still running", reaped)
	}
	if n, _ := q.InflightLen(ctx); n != 1 {
		t.Fatalf("in-flight = %d, want 1 (still held)", n)
	}
}

// TestIssue547DeadLetterOnExhaustion proves a permanently failing job is preserved
// for inspection rather than discarded.
func TestIssue547DeadLetterOnExhaustion(t *testing.T) {
	rdb := testClient(t)
	q := NewQueue(rdb)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := &failingHandler{}
	w := NewWorker(q, h, nil)
	w.backoff = []time.Duration{5 * time.Millisecond, 5 * time.Millisecond, 5 * time.Millisecond, 5 * time.Millisecond}

	if err := q.Enqueue(ctx, Job{Type: "verify", To: "a@b.test", Subject: "S", Body: "B"}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()

	deadline := time.Now().Add(5 * time.Second)
	dead := int64(-1)
	for time.Now().Before(deadline) {
		// Read with a fresh context: the worker's ctx is cancelled below, and the
		// assertion must not race that cancellation.
		if n, derr := q.DeadLen(context.Background()); derr == nil && n > 0 {
			dead = n
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if calls := h.calls.Load(); calls != int32(w.maxAttempts) {
		t.Fatalf("handler calls = %d, want %d (max attempts)", calls, w.maxAttempts)
	}
	if dead != 1 {
		t.Fatalf("issue #547: dead-letter count = %d, want 1 — an exhausted job must be "+
			"preserved for inspection, not silently dropped", dead)
	}
	cancel()
	<-done

	// The queue must be clean: nothing left ready, delayed, or in flight.
	bg := context.Background()
	if n, _ := q.Len(bg); n != 0 {
		t.Fatalf("ready len = %d, want 0", n)
	}
	if n, _ := rdb.ZCard(bg, delayedKey).Result(); n != 0 {
		t.Fatalf("delayed len = %d, want 0", n)
	}
	if n, _ := q.InflightLen(bg); n != 0 {
		t.Fatalf("in-flight len = %d, want 0", n)
	}
}

// TestIssue547SuccessfulJobIsAckedByWorker proves the worker loop acks on success,
// leaving neither ready nor in-flight entries.
func TestIssue547SuccessfulJobIsAckedByWorker(t *testing.T) {
	rdb := testClient(t)
	q := NewQueue(rdb)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := &okHandler{}
	w := NewWorker(q, h, nil)
	if err := q.Enqueue(ctx, Job{Type: "verify", To: "a@b.test", Subject: "S", Body: "B"}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if h.calls.Load() >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done

	if h.calls.Load() != 1 {
		t.Fatalf("handler calls = %d, want 1", h.calls.Load())
	}
	if n, _ := q.InflightLen(ctx); n != 0 {
		t.Fatalf("issue #547: in-flight after a successful send = %d, want 0 (the worker must ack)", n)
	}
	if n, _ := q.Len(ctx); n != 0 {
		t.Fatalf("ready after successful send = %d, want 0", n)
	}
	if n, _ := q.DeadLen(ctx); n != 0 {
		t.Fatalf("dead after successful send = %d, want 0", n)
	}
}

// TestIssue547PromoteDueIsSingleClaim proves the delayed-promotion path cannot
// promote the same retry twice under concurrency (multi-worker safety).
func TestIssue547PromoteDueIsSingleClaim(t *testing.T) {
	rdb := testClient(t)
	q := NewQueue(rdb)
	ctx := context.Background()

	job := Job{Type: "receipt", To: "x@y.test", Subject: "R", Body: "B", Attempts: 1}
	if err := q.EnqueueDelayed(ctx, job, -1*time.Second); err != nil { // already due
		t.Fatalf("enqueue delayed: %v", err)
	}
	// Three concurrent promoters: the member must land on the ready list exactly
	// once, because each promoter ZREM-claims it before pushing.
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if err := q.PromoteDue(ctx); err != nil {
				t.Errorf("promote: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	n, err := q.Len(ctx)
	if err != nil {
		t.Fatalf("len: %v", err)
	}
	if n != 1 {
		t.Fatalf("issue #547: ready len = %d after 3 concurrent promotions, want exactly 1 — "+
			"the ZREM-first claim must make promotion single-owner", n)
	}
	// And the delayed set must be empty (the member was claimed exactly once).
	if z, _ := rdb.ZCard(ctx, delayedKey).Result(); z != 0 {
		t.Fatalf("delayed len = %d, want 0", z)
	}
}

// okHandler always succeeds.
type okHandler struct{ calls atomic.Int32 }

func (h *okHandler) Send(ctx context.Context, job Job) error {
	h.calls.Add(1)
	return nil
}
