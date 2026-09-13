package dbstore

// Issue #517: the inbound webhook event row is a claim/complete record. These
// unit tests pin the lifecycle helpers that carry no HTTP surface, keeping the
// #517 guarantee covered package-locally (the end-to-end path lives in
// internal/httpapi and internal/payments).
//
// Integration against a real Postgres; skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"testing"
	"time"
)

func claimSuffix() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// TestClaimStandaloneCompleteAndRelease pins the no-sale claim path used by
// subscription/card-update events: a new key claims, a live claim refuses, a
// release (MarkFailed) makes it immediately re-claimable, and a completed event
// is a permanent duplicate that must never be re-claimed.
func TestClaimStandaloneCompleteAndRelease(t *testing.T) {
	ctx := context.Background()
	es := NewWebhookEventStore(testPool(t))

	key := "pur_sub_" + claimSuffix() + ":purchase.paid:paid"
	ev := &WebhookEvent{Source: "chip", EventType: "purchase.paid",
		Payload: []byte(`{"id":"pur_sub"}`), DedupKey: key}

	claimed, err := es.ClaimStandalone(ctx, ev, time.Minute)
	if err != nil || !claimed {
		t.Fatalf("first claim = %v, %v; want true, nil", claimed, err)
	}
	// A second attempt while the claim is live must not process.
	again, err := es.ClaimStandalone(ctx, ev, time.Minute)
	if err != nil {
		t.Fatalf("live claim: %v", err)
	}
	if again {
		t.Fatal("a live claim must not be re-claimable")
	}

	// Release it: an errored attempt must be retryable immediately.
	if err := es.MarkFailed(ctx, key); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	retry, err := es.ClaimStandalone(ctx, ev, time.Minute)
	if err != nil || !retry {
		t.Fatalf("claim after release = %v, %v; want true, nil (a failed attempt must be re-processable)", retry, err)
	}

	// Complete it: from now on it is a permanent duplicate.
	if err := es.MarkProcessed(ctx, key); err != nil {
		t.Fatalf("MarkProcessed: %v", err)
	}
	status, processedAt, err := es.EventState(ctx, key)
	if err != nil {
		t.Fatalf("EventState: %v", err)
	}
	if status != WebhookEventProcessed || processedAt == nil {
		t.Fatalf("state = %q processed_at = %v; want processed/set", status, processedAt)
	}
	// Even with a stale timestamp, a completed event is never re-claimable.
	if _, err := es.pool.Exec(ctx,
		`UPDATE webhook_events SET updated_at=now() - interval '30 minutes' WHERE dedup_key=$1`, key); err != nil {
		t.Fatalf("age processed row: %v", err)
	}
	done, err := es.ClaimStandalone(ctx, ev, time.Minute)
	if err != nil {
		t.Fatalf("claim processed: %v", err)
	}
	if done {
		t.Fatal("a successfully processed event must never be re-claimed — no double webhook/receipt")
	}
}

// TestClaimClaimedEventsAreInvisibleToReplay pins the concurrency boundary: a
// live claim is neither replayable nor released, so the sweeper cannot race a
// delivery that is still working.
func TestClaimClaimedEventsAreInvisibleToReplay(t *testing.T) {
	ctx := context.Background()
	es := NewWebhookEventStore(testPool(t))

	key := "pur_live_" + claimSuffix() + ":purchase.paid:paid"
	ev := &WebhookEvent{Source: "chip", EventType: "purchase.paid",
		Payload: []byte(`{"id":"pur_live"}`), DedupKey: key}
	if _, err := es.ClaimStandalone(ctx, ev, time.Minute); err != nil {
		t.Fatalf("claim: %v", err)
	}

	rows, err := es.Reclaimable(ctx, 100)
	if err != nil {
		t.Fatalf("Reclaimable: %v", err)
	}
	for _, r := range rows {
		if r.DedupKey == key {
			t.Fatal("a live claim must not be replayable")
		}
	}
	n, err := es.ReclaimStale(ctx, time.Minute)
	if err != nil {
		t.Fatalf("ReclaimStale: %v", err)
	}
	if n != 0 {
		t.Fatalf("ReclaimStale released %d rows, want 0 (the claim is live)", n)
	}
}
