package dbstore

// Money-path invariant integration tests (issue #99): ref_code uniqueness and
// webhook idempotency. These need a real Postgres (JOMFORM_TEST_DATABASE_URL);
// they are skipped in CI without it, mirroring the other dbstore integration
// tests.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"testing"
)

// randSuffix returns a short random hex suffix for unique test ref codes.
func randSuffix() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// TestRefCodeUniqueness asserts the sales.ref_code UNIQUE constraint: two
// sales must never share a reference code, so a buyer's payment can always be
// traced to exactly one sale (no double-charge / no ambiguous lookup).
func TestRefCodeUniqueness(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	ss := NewSaleStore(p)

	acc := uniqueAccount(t, ctx, p)

	ref := "JF-UNIQ-" + randSuffix()
	first := &Sale{
		AccountID: acc, RefCode: ref,
		Client:      SaleClient{FullName: "Ali", Email: "ali@example.test"},
		Items:       []SaleItem{{Name: "Tee", PriceSen: 1000, Qty: 1}},
		SubtotalSen: 1000, TotalSen: 1000,
		SaleType: SaleTypeOneTime, PaymentMethod: "chip", Status: SalePending,
	}
	if err := ss.Create(ctx, first); err != nil {
		t.Fatalf("create first sale: %v", err)
	}

	// Second sale with the SAME ref_code must be rejected by the DB.
	dup := &Sale{
		AccountID: acc, RefCode: ref,
		Client:      SaleClient{FullName: "Baba", Email: "baba@example.test"},
		Items:       []SaleItem{{Name: "Cap", PriceSen: 2000, Qty: 1}},
		SubtotalSen: 2000, TotalSen: 2000,
		SaleType: SaleTypeOneTime, PaymentMethod: "chip", Status: SalePending,
	}
	if err := ss.Create(ctx, dup); err == nil {
		t.Fatal("expected unique violation for duplicate ref_code, got nil")
	}

	// A distinct ref_code still inserts fine.
	other := &Sale{
		AccountID: acc, RefCode: ref + "-2",
		Client:      SaleClient{FullName: "Chong", Email: "chong@example.test"},
		Items:       []SaleItem{{Name: "Cap", PriceSen: 2000, Qty: 1}},
		SubtotalSen: 2000, TotalSen: 2000,
		SaleType: SaleTypeOneTime, PaymentMethod: "chip", Status: SalePending,
	}
	if err := ss.Create(ctx, other); err != nil {
		t.Fatalf("create distinct ref_code: %v", err)
	}
}

// TestWebhookIdempotency asserts the webhook_events dedup_key UNIQUE index:
// a re-delivered CHIP callback (same purchase id + event type + status) is
// inserted exactly once, so the business logic never double-charges or
// double-confirms.
func TestWebhookIdempotency(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	es := NewWebhookEventStore(p)

	dedupKey := "pur_" + randSuffix() + ":purchase.paid:paid"
	ev := &WebhookEvent{Source: "chip", EventType: "purchase.paid", Payload: []byte(`{"id":"pur_abc"}`), DedupKey: dedupKey}

	first, err := es.TryInsert(ctx, ev)
	if err != nil {
		t.Fatalf("first TryInsert: %v", err)
	}
	if !first {
		t.Fatal("first TryInsert should report inserted=true")
	}

	// Redelivery of the SAME event must be a no-op (idempotent).
	second, err := es.TryInsert(ctx, ev)
	if err != nil {
		t.Fatalf("second TryInsert: %v", err)
	}
	if second {
		t.Fatal("second TryInsert should report inserted=false (duplicate)")
	}

	// A DIFFERENT event (e.g. a later status) is a new key and inserts.
	ev2 := &WebhookEvent{Source: "chip", EventType: "purchase.paid", Payload: []byte(`{"id":"pur_abc"}`), DedupKey: "pur_" + randSuffix() + ":purchase.paid:error"}
	third, err := es.TryInsert(ctx, ev2)
	if err != nil {
		t.Fatalf("third TryInsert: %v", err)
	}
	if !third {
		t.Fatal("distinct dedup_key should insert")
	}
}
