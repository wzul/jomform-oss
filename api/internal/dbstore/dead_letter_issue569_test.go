package dbstore

// Issue #569: the reclaim sweep retried an unprocessable event FOREVER.
//
// ReclaimStale releases a claim older than the timeout so the event can be
// retried — correct for an interrupted attempt, but nothing distinguished that
// from an attempt that can NEVER succeed. An event referencing a purchase that no
// longer exists failed at the same point every time: released, replayed, failed,
// released again. Observed live at attempts=46, every 5 minutes, with
// unprocessed_total pinned at 1 so any backlog signal was drowned out.
//
// The fix gives an event a terminal state: after MaxReplayAttempts failures it is
// parked as 'dead', excluded from both ReclaimStale and Reclaimable, with the
// reason kept for an operator.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/testdb"
)

func seedEvent(t *testing.T, ctx context.Context, s *WebhookEventStore, dedup string, status string, attempts int, age time.Duration) {
	t.Helper()
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO webhook_events (source, event_type, payload, dedup_key, status, attempts, updated_at)
		 VALUES ('chip','purchase.paid','{}'::jsonb,$1,$2,$3, now() - $4::interval)`,
		dedup, status, attempts, age.String()); err != nil {
		t.Fatalf("seed event: %v", err)
	}
}

// TestIssue569EventIsBuriedAfterMaxAttempts is the regression.
func TestIssue569EventIsBuriedAfterMaxAttempts(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	s := NewWebhookEventStore(pool)

	dedup := fmt.Sprintf("r569-exhausted-%d", time.Now().UnixNano())
	// A 'processing' event that has been reclaimed and retried many times.
	seedEvent(t, ctx, s, dedup, WebhookEventProcessing, MaxReplayAttempts, 2*time.Hour)

	// The sweep must park it.
	n, err := s.BuryExhausted(ctx, "max replay attempts exceeded")
	if err != nil {
		t.Fatalf("BuryExhausted: %v", err)
	}
	if n == 0 {
		t.Fatalf("issue #569: an event at attempts=%d was not buried", MaxReplayAttempts)
	}

	var status, lastErr string
	if err := pool.QueryRow(ctx,
		`SELECT status, COALESCE(last_error,'') FROM webhook_events WHERE dedup_key=$1`, dedup).
		Scan(&status, &lastErr); err != nil {
		t.Fatalf("read: %v", err)
	}
	if status != WebhookEventDead {
		t.Errorf("issue #569: status = %q, want %q — an event that can never be processed must reach a terminal state", status, WebhookEventDead)
	}
	if lastErr == "" {
		t.Errorf("issue #569: no last_error recorded — the operator cannot see why it was parked")
	}

	// Terminal: reclaim must not resurrect it.
	if _, err := s.ReclaimStale(ctx, DefaultClaimTimeout); err != nil {
		t.Fatalf("ReclaimStale: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM webhook_events WHERE dedup_key=$1`, dedup).Scan(&status); err != nil {
		t.Fatalf("reread: %v", err)
	}
	if status != WebhookEventDead {
		t.Errorf("issue #569: ReclaimStale resurrected a dead event (status=%q) — the retry loop would resume", status)
	}

	// And it must leave the replay backlog.
	rows, err := s.Reclaimable(ctx, 500)
	if err != nil {
		t.Fatalf("Reclaimable: %v", err)
	}
	for _, e := range rows {
		if e.DedupKey == dedup {
			t.Errorf("issue #569: a dead event is still listed as reclaimable — the backlog never drains")
		}
	}
}

// TestIssue569TransientFailureIsStillRetried keeps the cap from being too eager:
// an event that has failed a few times must stay retryable.
func TestIssue569TransientFailureIsStillRetried(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	s := NewWebhookEventStore(pool)

	dedup := fmt.Sprintf("r569-transient-%d", time.Now().UnixNano())
	seedEvent(t, ctx, s, dedup, WebhookEventFailed, 3, time.Minute)

	if n, err := s.BuryExhausted(ctx, "max replay attempts exceeded"); err != nil {
		t.Fatalf("BuryExhausted: %v", err)
	} else if n != 0 {
		t.Errorf("issue #569: an event at attempts=3 was buried (count=%d) — a transient fault must still be retried", n)
	}

	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM webhook_events WHERE dedup_key=$1`, dedup).Scan(&status); err != nil {
		t.Fatalf("read: %v", err)
	}
	if status != WebhookEventFailed {
		t.Errorf("status = %q, want %q (unchanged)", status, WebhookEventFailed)
	}

	rows, err := s.Reclaimable(ctx, 500)
	if err != nil {
		t.Fatalf("Reclaimable: %v", err)
	}
	found := false
	for _, e := range rows {
		if e.DedupKey == dedup {
			found = true
		}
	}
	if !found {
		t.Errorf("a transiently failed event is no longer reclaimable — the retry path is broken")
	}
}

// TestIssue569ProcessedEventIsNeverBuried guards the settled case.
func TestIssue569ProcessedEventIsNeverBuried(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	s := NewWebhookEventStore(pool)

	dedup := fmt.Sprintf("r569-processed-%d", time.Now().UnixNano())
	seedEvent(t, ctx, s, dedup, WebhookEventProcessed, MaxReplayAttempts+10, time.Hour)

	if _, err := s.BuryExhausted(ctx, "x"); err != nil {
		t.Fatalf("BuryExhausted: %v", err)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM webhook_events WHERE dedup_key=$1`, dedup).Scan(&status); err != nil {
		t.Fatalf("read: %v", err)
	}
	if status != WebhookEventProcessed {
		t.Errorf("a PROCESSED event was changed to %q — settled events must never be touched", status)
	}
}
