// Issue #197: persistent webhook retry queue. Verifies enqueue, due-retry
// sweep, dead-lettering, and manual retry. Integration against a real
// Postgres; skipped without JOMFORM_TEST_DATABASE_URL.
package dbstore

import (
	"context"
	"testing"
	"time"
)

func TestWebhookRetryQueue(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)

	acc := uniqueAccount(t, ctx, p)
	ws := NewWebhookStore(p)
	wh, err := ws.Create(ctx, acc, "", "https://example.test/hook", "secret1234", []string{"sale.paid"})
	if err != nil {
		t.Fatalf("create webhook: %v", err)
	}

	// Enqueue a retry that is due now.
	if err := ws.EnqueueRetry(ctx, wh.ID, "sale.paid", `{"ok":1}`, 1, time.Now()); err != nil {
		t.Fatalf("enqueue retry: %v", err)
	}
	// Enqueue a retry that is not yet due (should not be picked up).
	if err := ws.EnqueueRetry(ctx, wh.ID, "sale.paid", `{"ok":2}`, 1, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("enqueue future retry: %v", err)
	}

	// Use a large limit so this webhook's retry is always included even when
	// other dbstore tests (running in parallel) have enqueued their own due
	// retries with earlier next_retry_at timestamps.
	due, err := ws.DueRetries(ctx, 1000)
	if err != nil {
		t.Fatalf("due retries: %v", err)
	}
	// The shared test DB may hold retries from other tests; filter to this
	// webhook. Only the due-now retry for this webhook should be returned.
	var mine []*WebhookRetry
	for _, r := range due {
		if r.WebhookID == wh.ID {
			mine = append(mine, r)
		}
	}
	if len(mine) != 1 {
		t.Fatalf("due retries for webhook = %d, want 1 (only the due-now one)", len(mine))
	}
	if mine[0].Payload != `{"ok":1}` {
		t.Errorf("due retry payload = %q, want the due-now one", mine[0].Payload)
	}

	// Simulate a failed delivery: advance the attempt and dead-letter it.
	if err := ws.ScheduleRetry(ctx, mine[0].ID, 2, time.Now().Add(5*time.Minute)); err != nil {
		t.Fatalf("schedule retry: %v", err)
	}
	if err := ws.MarkDeadLetter(ctx, mine[0].ID); err != nil {
		t.Fatalf("dead-letter: %v", err)
	}

	// Dead-lettered retries are visible (not silently dropped).
	dead, err := ws.ListDeadLettered(ctx, acc, wh.ID, 10)
	if err != nil {
		t.Fatalf("list dead-lettered: %v", err)
	}
	if len(dead) != 1 {
		t.Fatalf("dead-lettered = %d, want 1", len(dead))
	}

	// Manual retry un-dead-letters it so the sweep re-delivers.
	if err := ws.RetryNow(ctx, acc, mine[0].ID); err != nil {
		t.Fatalf("retry now: %v", err)
	}
	dead, _ = ws.ListDeadLettered(ctx, acc, wh.ID, 10)
	if len(dead) != 0 {
		t.Fatalf("dead-lettered after retry = %d, want 0", len(dead))
	}
	due, _ = ws.DueRetries(ctx, 1000)
	mine = nil
	for _, r := range due {
		if r.WebhookID == wh.ID {
			mine = append(mine, r)
		}
	}
	if len(mine) != 1 {
		t.Fatalf("due retries for webhook after manual retry = %d, want 1", len(mine))
	}

	// Success removes the retry from the queue.
	if err := ws.DeleteRetry(ctx, mine[0].ID); err != nil {
		t.Fatalf("delete retry: %v", err)
	}
	due, _ = ws.DueRetries(ctx, 1000)
	mine = nil
	for _, r := range due {
		if r.WebhookID == wh.ID {
			mine = append(mine, r)
		}
	}
	if len(mine) != 0 {
		t.Fatalf("due retries for webhook after delete = %d, want 0", len(mine))
	}

	// Account scoping: a different account cannot retry this webhook's retry.
	other := uniqueAccount(t, ctx, p)
	if err := ws.EnqueueRetry(ctx, wh.ID, "sale.paid", `{"ok":3}`, 1, time.Now()); err != nil {
		t.Fatalf("enqueue retry 3: %v", err)
	}
	due, _ = ws.DueRetries(ctx, 1000)
	mine = nil
	for _, r := range due {
		if r.WebhookID == wh.ID {
			mine = append(mine, r)
		}
	}
	if len(mine) != 1 {
		t.Fatalf("due retries for webhook = %d, want 1", len(mine))
	}
	if err := ws.RetryNow(ctx, other, mine[0].ID); err == nil {
		t.Fatalf("other account retried a foreign retry — account scoping failed")
	}
}
