package dbstore

// Issue #574: a dead-lettered event was invisible, and could not be retried
// after the operator fixed the cause.
//
// The #569 fix stops an unprocessable event being retried forever by parking it
// with status='dead'. That is correct, but nothing could then READ it: no MCP
// tool and no REST endpoint touched webhook_events, so the only trace was a log
// line at the moment it was buried. The failure mode moved from noisy to silent.
//
// These tests cover the visibility (ListDead) and the recovery (RequeueDead) —
// including the trap that makes a naive requeue useless.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/testdb"
)

func insertDeadEvent(t *testing.T, ctx context.Context, s *WebhookEventStore, dedup string, attempts int, reason string) {
	t.Helper()
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO webhook_events (source, event_type, payload, dedup_key, status, attempts, last_error, updated_at)
		 VALUES ('chip','purchase.paid','{}'::jsonb,$1,$2,$3,$4, now() - interval '1 hour')`,
		dedup, WebhookEventDead, attempts, reason); err != nil {
		t.Fatalf("seed dead event: %v", err)
	}
}

// TestIssue574DeadEventIsVisible is the visibility half of the fix.
func TestIssue574DeadEventIsVisible(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	s := NewWebhookEventStore(pool)

	dedup := fmt.Sprintf("r574-visible-%d", time.Now().UnixNano())
	insertDeadEvent(t, ctx, s, dedup, MaxReplayAttempts+2, "max replay attempts exceeded")

	dead, err := s.ListDead(ctx, 100)
	if err != nil {
		t.Fatalf("ListDead: %v", err)
	}
	var found *DeadLetterEvent
	for _, e := range dead {
		if e.DedupKey == dedup {
			found = e
		}
	}
	if found == nil {
		t.Fatalf("issue #574: a parked event is not listed — the operator cannot see it exists")
	}
	if found.Attempts != MaxReplayAttempts+2 {
		t.Errorf("attempts = %d, want %d", found.Attempts, MaxReplayAttempts+2)
	}
	if found.LastError == "" {
		t.Errorf("issue #574: no reason surfaced — the operator cannot tell why it was parked")
	}
	if found.DeadSince.IsZero() {
		t.Errorf("dead_since is zero — the operator cannot tell how long it has been parked")
	}
}

// TestIssue574RequeueLetsTheEventActuallyRun is the recovery half, and covers
// the trap: the sweep buries BEFORE it reclaims, so an event revived while still
// at or over MaxReplayAttempts would be buried again in the same pass without
// ever being replayed. The requeue would look like it worked and do nothing.
func TestIssue574RequeueLetsTheEventActuallyRun(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	s := NewWebhookEventStore(pool)

	dedup := fmt.Sprintf("r574-requeue-%d", time.Now().UnixNano())
	insertDeadEvent(t, ctx, s, dedup, MaxReplayAttempts+5, "max replay attempts exceeded")

	ok, err := s.RequeueDead(ctx, dedup)
	if err != nil {
		t.Fatalf("RequeueDead: %v", err)
	}
	if !ok {
		t.Fatalf("issue #574: a dead event could not be requeued")
	}

	var status string
	var attempts int
	if err := pool.QueryRow(ctx,
		`SELECT status, attempts FROM webhook_events WHERE dedup_key=$1`, dedup).
		Scan(&status, &attempts); err != nil {
		t.Fatalf("read: %v", err)
	}
	if status != WebhookEventPending {
		t.Errorf("status = %q, want %q", status, WebhookEventPending)
	}
	if attempts != 0 {
		t.Errorf("issue #574: attempts = %d after requeue, want 0 — the sweep buries before it reclaims, so a revived event still over the cap is parked again in the SAME pass and never runs", attempts)
	}

	// The decisive assertion: the very next sweep must not bury it again.
	if n, err := s.BuryExhausted(ctx, "max replay attempts exceeded"); err != nil {
		t.Fatalf("BuryExhausted: %v", err)
	} else if n != 0 {
		t.Errorf("issue #574: the sweep buried the requeued event again (count=%d) — the requeue is a no-op", n)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM webhook_events WHERE dedup_key=$1`, dedup).Scan(&status); err != nil {
		t.Fatalf("reread: %v", err)
	}
	if status != WebhookEventPending {
		t.Errorf("status = %q after the sweep, want %q — it must be eligible for replay", status, WebhookEventPending)
	}

	// And it must be in the replay backlog.
	rows, err := s.Reclaimable(ctx, 500)
	if err != nil {
		t.Fatalf("Reclaimable: %v", err)
	}
	inBacklog := false
	for _, e := range rows {
		if e.DedupKey == dedup {
			inBacklog = true
		}
	}
	if !inBacklog {
		t.Errorf("issue #574: a requeued event is not reclaimable — it was revived but will never be replayed")
	}
}

// TestIssue574RequeueRefusesNonDeadEvents guards the other direction: requeue
// must only touch a parked event, never one in flight or already settled.
func TestIssue574RequeueRefusesNonDeadEvents(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	s := NewWebhookEventStore(pool)

	for _, tc := range []struct{ name, status string }{
		{"processing", WebhookEventProcessing},
		{"processed", WebhookEventProcessed},
		{"pending", WebhookEventPending},
	} {
		dedup := fmt.Sprintf("r574-refuse-%s-%d", tc.name, time.Now().UnixNano())
		if _, err := s.pool.Exec(ctx,
			`INSERT INTO webhook_events (source, event_type, payload, dedup_key, status, attempts)
			 VALUES ('chip','purchase.paid','{}'::jsonb,$1,$2,1)`, dedup, tc.status); err != nil {
			t.Fatalf("seed %s: %v", tc.name, err)
		}
		ok, err := s.RequeueDead(ctx, dedup)
		if err != nil {
			t.Fatalf("RequeueDead(%s): %v", tc.name, err)
		}
		if ok {
			t.Errorf("issue #574: a %q event was requeued — only a parked event may be revived", tc.status)
		}
		var status string
		if err := pool.QueryRow(ctx, `SELECT status FROM webhook_events WHERE dedup_key=$1`, dedup).Scan(&status); err != nil {
			t.Fatalf("read: %v", err)
		}
		if status != tc.status {
			t.Errorf("%s event changed to %q — it must be untouched", tc.status, status)
		}
	}
}
