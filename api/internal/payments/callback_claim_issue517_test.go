package payments

// Issue #517: pin the claim/complete dedup contract and the stale-reclaim path.
//
// The dedup row must mean "this event has been successfully processed". A
// failed attempt releases the claim (or leaves a dead one), and a dead claim is
// reclaimable — by a CHIP redelivery or by RunInboundCallbackSweep — while a
// completed event never re-fires a webhook, a receipt, or a subscription
// activation.
//
// Integration against a real Postgres; skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/chip"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// TestClaimTxRejectsLiveClaimAllowsStaleClaim pins the claim gate: a live
// 'processing' claim held by another attempt is refused (concurrent duplicate),
// but a claim whose attempt is gone — untouched past the claim timeout — is
// retaken, so a crashed attempt is retried instead of swallowed.
func TestClaimTxRejectsLiveClaimAllowsStaleClaim(t *testing.T) {
	ctx := context.Background()
	pool := lockTestPool(t)
	es := dbstore.NewWebhookEventStore(pool)

	dedupKey := "pur_reclaim_" + lockTestSuffix() + ":purchase.paid:paid"
	ev := &dbstore.WebhookEvent{Source: "chip", EventType: "purchase.paid",
		Payload: []byte(`{"id":"pur_reclaim"}`), DedupKey: dedupKey}

	// Attempt 1 claims and then "dies" without completing (tx committed, no
	// processed_at) — exactly the state a deploy restart mid-callback leaves.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	ok, err := es.ClaimTx(ctx, tx, ev, time.Second)
	if err != nil || !ok {
		t.Fatalf("first claim = %v, %v; want true, nil", ok, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit claim: %v", err)
	}

	// A redelivery while the claim is live must NOT process (concurrent).
	tx2, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	ok2, err := es.ClaimTx(ctx, tx2, ev, time.Second)
	if err != nil {
		t.Fatalf("live claim: %v", err)
	}
	_ = tx2.Rollback(ctx)
	if ok2 {
		t.Fatal("a live claim must not be re-claimable — concurrent identical deliveries cannot both process")
	}

	// Wait out the (deliberately tiny) claim timeout: the claim is now stale.
	time.Sleep(60 * time.Millisecond)
	tx3, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	ok3, err := es.ClaimTx(ctx, tx3, ev, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("stale claim: %v", err)
	}
	if !ok3 {
		t.Fatal("a stale claim (attempt gone) must be re-claimable — the retry path")
	}
	if err := tx3.Commit(ctx); err != nil {
		t.Fatalf("commit stale reclaim: %v", err)
	}

	status, processedAt, err := es.EventState(ctx, dedupKey)
	if err != nil {
		t.Fatalf("event state: %v", err)
	}
	if status != dbstore.WebhookEventProcessing || processedAt != nil {
		t.Fatalf("state = %q processed_at = %v; want processing/NULL", status, processedAt)
	}
}

// TestReclaimStaleReleasesDeadClaims verifies the sweeper's release step: only
// stale, unprocessed 'processing' rows are released to 'pending'; a completed
// event and a live claim are untouched.
func TestReclaimStaleReleasesDeadClaims(t *testing.T) {
	ctx := context.Background()
	pool := lockTestPool(t)
	es := dbstore.NewWebhookEventStore(pool)

	stale := "pur_stale_" + lockTestSuffix() + ":purchase.paid:paid"
	live := "pur_live_" + lockTestSuffix() + ":purchase.paid:paid"
	done := "pur_done_" + lockTestSuffix() + ":purchase.paid:paid"
	for _, k := range []string{stale, live, done} {
		if _, err := es.TryInsert(ctx, &dbstore.WebhookEvent{Source: "chip",
			EventType: "purchase.paid", Payload: []byte(`{"id":"x"}`), DedupKey: k}); err != nil {
			t.Fatalf("seed %s: %v", k, err)
		}
	}
	// stale: claimed long ago; live: just claimed; done: completed.
	if _, err := pool.Exec(ctx,
		`UPDATE webhook_events SET status=$2, updated_at=now() - interval '30 minutes' WHERE dedup_key=$1`,
		stale, dbstore.WebhookEventProcessing); err != nil {
		t.Fatalf("age stale: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE webhook_events SET status=$2 WHERE dedup_key=$1`, live, dbstore.WebhookEventProcessing); err != nil {
		t.Fatalf("mark live: %v", err)
	}
	if err := es.MarkProcessed(ctx, done); err != nil {
		t.Fatalf("mark processed: %v", err)
	}

	n, err := es.ReclaimStale(ctx, dbstore.DefaultClaimTimeout)
	if err != nil {
		t.Fatalf("ReclaimStale: %v", err)
	}
	if n != 1 {
		t.Fatalf("ReclaimStale released %d rows, want 1 (only the stale claim)", n)
	}
	assertState(t, ctx, es, stale, dbstore.WebhookEventPending, false)
	assertState(t, ctx, es, live, dbstore.WebhookEventProcessing, false)
	assertState(t, ctx, es, done, dbstore.WebhookEventProcessed, true)

	// The released event is now replayable; the live and completed ones are not.
	rows, err := es.Reclaimable(ctx, 100)
	if err != nil {
		t.Fatalf("Reclaimable: %v", err)
	}
	got := map[string]bool{}
	for _, r := range rows {
		got[r.DedupKey] = true
	}
	if !got[stale] {
		t.Fatal("released stale claim must be listed as replayable")
	}
	if got[live] || got[done] {
		t.Fatalf("live=%v done=%v must not be replayable", got[live], got[done])
	}
	if n, err := es.UnprocessedCount(ctx); err != nil || n < 1 {
		t.Fatalf("UnprocessedCount = %d, %v; want >= 1 (the #517 backlog is detectable)", n, err)
	}
}

// TestProcessCallbackClaimedStaleClaimIsReplayedOnce is the end-to-end
// recoverability pin for the chosen mechanism: an event stranded by a dead
// attempt is replayed by the sweep and settles the sale exactly once; a
// completed event is never replayed (no second webhook).
func TestProcessCallbackClaimedStaleClaimIsReplayedOnce(t *testing.T) {
	ctx := context.Background()
	pool := lockTestPool(t)
	es := dbstore.NewWebhookEventStore(pool)
	saleID, purchaseID := seedLockSale(t, ctx, pool)

	var webhooks int64
	svc := &Service{
		Sales:    dbstore.NewSaleStore(pool),
		Accounts: dbstore.NewAccountStore(pool),
		OnPaid: func(_ context.Context, _, _ string, _ map[string]any) {
			atomic.AddInt64(&webhooks, 1)
		},
	}

	// Attempt 1 claims the event, then dies before processing (the crash /
	// deploy-restart case): the row stands at processing, processed_at NULL,
	// and the sale is still pending.
	dedupKey := purchaseID + ":purchase.paid:paid"
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	ok, err := es.ClaimTx(ctx, tx, &dbstore.WebhookEvent{
		Source: "chip", EventType: "purchase.paid", DedupKey: dedupKey,
		Payload: []byte(`{"id":"` + purchaseID + `","status":"paid"}`),
	}, time.Second)
	if err != nil || !ok {
		t.Fatalf("claim = %v, %v; want true", ok, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if got, _ := dbstore.NewSaleStore(pool).ByID(ctx, saleID); got.Status != dbstore.SalePending {
		t.Fatalf("sale status = %q, want pending (stranded claim)", got.Status)
	}

	// Age the dead claim, then run one sweep pass: it releases the claim and
	// replays the event.
	if _, err := pool.Exec(ctx,
		`UPDATE webhook_events SET updated_at=now() - interval '30 minutes' WHERE dedup_key=$1`, dedupKey); err != nil {
		t.Fatalf("age claim: %v", err)
	}
	sweepInboundCallbacks(ctx, svc, es)

	got, err := dbstore.NewSaleStore(pool).ByID(ctx, saleID)
	if err != nil {
		t.Fatalf("reload sale: %v", err)
	}
	if got.Status != dbstore.SalePaid {
		t.Fatalf("sale status after sweep = %q, want %q — a stranded callback must be recovered (#517)", got.Status, dbstore.SalePaid)
	}
	if n := atomic.LoadInt64(&webhooks); n != 1 {
		t.Fatalf("webhooks after sweep = %d, want exactly 1", n)
	}
	assertState(t, ctx, es, dedupKey, dbstore.WebhookEventProcessed, true)

	// A second sweep pass must be a no-op: the event is completed, so it is
	// not replayable and cannot double-fire.
	sweepInboundCallbacks(ctx, svc, es)
	if n := atomic.LoadInt64(&webhooks); n != 1 {
		t.Fatalf("webhooks after second sweep = %d, want exactly 1 (no double-fire)", n)
	}
}

// TestProcessCallbackClaimedConcurrentIdenticalDeliveriesSettleOnce verifies
// the claim gate + per-sale lock serialize concurrent identical deliveries at
// the service level: a single settle, a single webhook, and exactly one attempt
// reported as the processor.
func TestProcessCallbackClaimedConcurrentIdenticalDeliveriesSettleOnce(t *testing.T) {
	ctx := context.Background()
	pool := lockTestPool(t)
	es := dbstore.NewWebhookEventStore(pool)
	saleID, purchaseID := seedLockSale(t, ctx, pool)

	var webhooks int64
	svc := &Service{
		Sales:    dbstore.NewSaleStore(pool),
		Accounts: dbstore.NewAccountStore(pool),
		OnPaid: func(_ context.Context, _, _ string, _ map[string]any) {
			atomic.AddInt64(&webhooks, 1)
		},
	}
	dedupKey := purchaseID + ":purchase.paid:paid"
	raw := []byte(`{"id":"` + purchaseID + `","event_type":"purchase.paid","status":"paid","total":1000}`)
	p := &chip.Purchase{ID: purchaseID, EventType: "purchase.paid", Status: chip.StatusPaid}

	const n = 4
	var processed int64
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			claimed, err := svc.ProcessCallbackClaimed(ctx, p, dedupKey, raw, es, time.Minute)
			if err != nil {
				t.Errorf("ProcessCallbackClaimed: %v", err)
				return
			}
			if claimed {
				atomic.AddInt64(&processed, 1)
			}
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt64(&processed); got != 1 {
		t.Fatalf("attempts that processed = %d, want exactly 1", got)
	}
	if n := atomic.LoadInt64(&webhooks); n != 1 {
		t.Fatalf("webhooks = %d, want exactly 1", n)
	}
	got, err := dbstore.NewSaleStore(pool).ByID(ctx, saleID)
	if err != nil {
		t.Fatalf("reload sale: %v", err)
	}
	if got.Status != dbstore.SalePaid {
		t.Fatalf("sale status = %q, want %q", got.Status, dbstore.SalePaid)
	}
	assertState(t, ctx, es, dedupKey, dbstore.WebhookEventProcessed, true)
}

// lockTestSuffix returns a short random suffix for dedup keys.
func lockTestSuffix() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// assertState asserts an event row's status and whether processed_at is set.
func assertState(t *testing.T, ctx context.Context, es *dbstore.WebhookEventStore, dedupKey, wantStatus string, wantProcessed bool) {
	t.Helper()
	status, processedAt, err := es.EventState(ctx, dedupKey)
	if err != nil {
		t.Fatalf("EventState(%s): %v", dedupKey, err)
	}
	if status != wantStatus {
		t.Fatalf("event %s status = %q, want %q", dedupKey, status, wantStatus)
	}
	if (processedAt != nil) != wantProcessed {
		t.Fatalf("event %s processed_at set = %v, want %v", dedupKey, processedAt != nil, wantProcessed)
	}
}
