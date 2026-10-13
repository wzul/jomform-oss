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

// claimState reads one row's claim state back by its own dedup_key. Every
// assertion about "this claim" in this file goes through here, never through a
// table-wide count or a page-limited list.
func claimState(t *testing.T, ctx context.Context, es *WebhookEventStore, dedupKey string) (string, bool) {
	t.Helper()
	status, processedAt, err := es.EventState(ctx, dedupKey)
	if err != nil {
		t.Fatalf("EventState(%s): %v", dedupKey, err)
	}
	return status, processedAt != nil
}

// TestClaimClaimedEventsAreInvisibleToReplay pins the concurrency boundary: a
// live claim is neither replayable nor released, so the sweeper cannot race a
// delivery that is still working.
//
// ReclaimStale is deliberately TABLE-wide — it is the sweeper that releases
// dead claims across the whole queue (internal/payments/callback_claim.go) — so
// its return count is about every 'processing' row in this package's scratch
// schema, not about this one. Asserting `n == 0` therefore made this test's
// verdict depend on what sibling tests in this package had left behind and on
// how long the run took: rows they leave live (the r578-* claims,
// r574-refuse-processing) cross the one-minute threshold as the run gets
// slower, and the assertion then counted those. Measured on an unmodified tree
// with one leaked aged claim: the pre-fix assertion failed with exactly
// "ReclaimStale released 1 rows, want 0 (the claim is live)" while this test's
// checks stayed green. Everything read back below is this test's own row, by
// its own dedup_key.
//
// The stale claim is the positive control, and it is the second reason this
// test is two-armed. A live-claim-only version is green for a sweeper that
// releases nothing at all — measured: moving ReclaimStale's window far into the
// past (a sweeper that never releases anything, which is precisely the
// regression that strands the #517 backlog) left the single-arm version PASSING.
// Both rows are released by the same sweep call and read back afterwards, so
// "this claim survived" is only asserted together with "that claim was released
// and the read-back saw it" — one measurement, not an assumption that the sweep
// did anything.
func TestClaimClaimedEventsAreInvisibleToReplay(t *testing.T) {
	ctx := context.Background()
	es := NewWebhookEventStore(testPool(t))

	live := "pur_live_" + claimSuffix() + ":purchase.paid:paid"
	stale := "pur_stale_" + claimSuffix() + ":purchase.paid:paid"
	for _, key := range []string{live, stale} {
		claimed, err := es.ClaimStandalone(ctx, &WebhookEvent{Source: "chip",
			EventType: "purchase.paid", Payload: []byte(`{"id":"pur_live"}`), DedupKey: key}, time.Minute)
		if err != nil || !claimed {
			t.Fatalf("claim %s = %v, %v; want true, nil", key, claimed, err)
		}
	}
	// The second claim's attempt is gone — untouched for well past the window
	// swept below. That is the crash / deploy-restart case the sweeper exists
	// for, and it is created here rather than inherited from a sibling test.
	if _, err := es.pool.Exec(ctx,
		`UPDATE webhook_events SET updated_at=now() - interval '30 minutes' WHERE dedup_key=$1`,
		stale); err != nil {
		t.Fatalf("age the stale claim: %v", err)
	}

	// One sweep, both arms. Its return count is table-wide, so the only count
	// this test can assert is the one its own rows guarantee: at least the
	// stale claim above.
	n, err := es.ReclaimStale(ctx, time.Minute)
	if err != nil {
		t.Fatalf("ReclaimStale: %v", err)
	}
	if n < 1 {
		t.Fatalf("ReclaimStale released %d rows but this test's own claim (untouched for 30 minutes, "+
			"timeout 1 minute) was released — the sweeper's count does not include it", n)
	}

	// Positive control: the sweep really released a dead claim, and the read-back
	// below notices. If this arm is green the live-claim arm cannot be green
	// merely because the sweep did nothing.
	if status, processed := claimState(t, ctx, es, stale); status != WebhookEventPending || processed {
		t.Fatalf("after a sweep over a claim untouched for 30 minutes: state = %q processed_at = %v, "+
			"want pending/unset (the sweeper did not release a dead claim, so a crashed attempt is "+
			"never retried and the #517 backlog strands)", status, processed)
	}
	// The invariant this test exists for.
	if status, processed := claimState(t, ctx, es, live); status != WebhookEventProcessing || processed {
		t.Fatalf("after a sweep over a live claim: state = %q processed_at = %v, want processing/unset "+
			"(the sweeper released a live claim, so a delivery still working can be replayed)", status, processed)
	}

	// ...and the released one is replayable while the live one is invisible to
	// replay, keyed to this test's own rows. Reclaimable is page-limited, so
	// absence from it only means "not replayable" when the page came back short
	// — a full page would make both checks below vacuous, so fail loudly instead.
	const page = 500
	rows, err := es.Reclaimable(ctx, page)
	if err != nil {
		t.Fatalf("Reclaimable: %v", err)
	}
	if len(rows) == page {
		t.Fatalf("Reclaimable returned a full page (%d rows): the per-key checks below cannot see a row "+
			"past the cap — this test needs a key-scoped read", page)
	}
	replayable := map[string]bool{}
	for _, r := range rows {
		replayable[r.DedupKey] = true
	}
	if !replayable[stale] {
		t.Fatal("a released dead claim must be listed as replayable — the retry path is broken")
	}
	if replayable[live] {
		t.Fatal("a live claim must not be replayable")
	}
}
