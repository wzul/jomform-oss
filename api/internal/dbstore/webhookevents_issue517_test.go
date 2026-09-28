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
	// ReclaimStale is deliberately TABLE-wide (it is the sweeper that releases
	// dead claims across the whole queue), so its return count is about every
	// 'processing' row in the schema, not about this one. Asserting `n == 0`
	// therefore made this test's verdict depend on whatever sibling tests in
	// this package had left behind and on how long the run took: rows they leave
	// live cross the 1-minute threshold as the run gets slower. Measured on an
	// unmodified tree — dbstore GREEN at 30s standalone and at 72.8s in a pair
	// run, RED with exactly this message at 92.6s / 104.5s / 114.3s in the
	// whole-suite runs, and reproduced deterministically by leaking one live
	// claim and aging it past the threshold.
	//
	// The premise this test exists for is "ReclaimStale must not release THIS
	// claim", so it is re-read after the sweep from the row itself. The sweep
	// still runs, so a regression that made a live claim reclaimable is still
	// caught — by the state check below rather than by an aggregate count that
	// was never about this claim.
	if _, err := es.ReclaimStale(ctx, time.Minute); err != nil {
		t.Fatalf("ReclaimStale: %v", err)
	}
	status, processedAt, err := es.EventState(ctx, key)
	if err != nil {
		t.Fatalf("EventState: %v", err)
	}
	if status != WebhookEventProcessing || processedAt != nil {
		t.Fatalf("after a sweep over a live claim: state = %q processed_at = %v, want processing/unset "+
			"(the sweeper released a live claim, so a delivery still working can be replayed)", status, processedAt)
	}
}
