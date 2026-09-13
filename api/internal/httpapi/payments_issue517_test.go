package httpapi

// Issue #517 (money-critical): a CHIP callback whose business logic fails must
// be re-processable, not swallowed as a duplicate.
//
// The dedup row used to be recorded BEFORE ProcessCallback ran. When the
// business logic failed (the handler returns 500), the row stayed at
// processed_at = NULL with its dedup_key already taken, so every redelivery was
// answered 200 "duplicate" and never re-processed — the customer was charged
// and the sale was never marked paid, permanently.
//
// These tests drive the real handler (including the #365 fail-closed signature
// gate — the payloads are genuinely RSA-signed, not bypassed) against a real
// Postgres, and use only pre-existing APIs so the regression test can be run
// against the un-fixed code to prove it fails there.

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/payments"
	"github.com/wzul/jomform/api/internal/testdb"
)

// signChipBody signs the raw body the way CHIP does, so signature verification
// stays mandatory and the test payload is accepted on its own merits.
func signChipBody(t *testing.T, key *rsa.PrivateKey, raw []byte) string {
	t.Helper()
	digest := sha256.Sum256(raw)
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("sign body: %v", err)
	}
	return base64.StdEncoding.EncodeToString(sig)
}

// claimHarness wires the real callback handler over a real Postgres, with a
// pending one-time sale and a counting OnPaid webhook.
type claimHarness struct {
	pool       *pgxpool.Pool
	h          *PaymentsHandlers
	key        *rsa.PrivateKey
	saleID     string
	purchaseID string
	webhooks   *int64
}

func newClaimHarness(t *testing.T, ctx context.Context) *claimHarness {
	t.Helper()
	pool := testdb.Open(t)
	testdb.Reset(t, pool, "webhook_events")

	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ('it-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	var accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 SELECT $1, 'it-acc-' || md5(random()::text), 'it-' || md5(random()::text)
		 RETURNING id`, userID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	sale := &dbstore.Sale{
		AccountID: accountID, RefCode: "IT-517-" + accountID[:8],
		Client:   dbstore.SaleClient{FullName: "Buyer", Email: "buyer@example.test"},
		Items:    []dbstore.SaleItem{{ProductID: "p1", Name: "Tee", PriceSen: 1000, Qty: 1}},
		TotalSen: 1000, SaleType: dbstore.SaleTypeOneTime, PaymentMethod: "chip",
		Status: dbstore.SalePending,
	}
	sales := dbstore.NewSaleStore(pool)
	if err := sales.Create(ctx, sale); err != nil {
		t.Fatalf("create sale: %v", err)
	}
	purchaseID := "pur_517_" + accountID[:8]
	if err := sales.AttachChipPurchase(ctx, sale.ID, purchaseID, "https://gate.example/checkout/"+purchaseID); err != nil {
		t.Fatalf("attach purchase: %v", err)
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	webhooks := new(int64)
	svc := &payments.Service{
		Sales:    sales,
		Accounts: dbstore.NewAccountStore(pool),
		OnPaid: func(_ context.Context, _, _ string, _ map[string]any) {
			atomic.AddInt64(webhooks, 1)
		},
	}
	return &claimHarness{
		pool: pool, key: key, saleID: sale.ID, purchaseID: purchaseID, webhooks: webhooks,
		h: &PaymentsHandlers{
			Service:   svc,
			Events:    dbstore.NewWebhookEventStore(pool),
			PublicKey: &key.PublicKey,
		},
	}
}

// deliver posts a signed purchase.paid callback for the harness purchase.
func (c *claimHarness) deliver(t *testing.T, ctx context.Context) *httptest.ResponseRecorder {
	t.Helper()
	raw := []byte(fmt.Sprintf(`{"id":%q,"event_type":"purchase.paid","status":"paid","total":1000}`, c.purchaseID))
	req := httptest.NewRequest(http.MethodPost, "/v1/payments/chip/callback", bytes.NewReader(raw)).
		WithContext(ctx)
	req.Header.Set("X-Signature", signChipBody(t, c.key, raw))
	rr := httptest.NewRecorder()
	c.h.handleChipCallback(rr, req)
	return rr
}

// saleStatus reads the sale status straight from Postgres.
func (c *claimHarness) saleStatus(t *testing.T, ctx context.Context) string {
	t.Helper()
	got, err := dbstore.NewSaleStore(c.pool).ByID(ctx, c.saleID)
	if err != nil {
		t.Fatalf("reload sale: %v", err)
	}
	return got.Status
}

// webhookCount reads the number of sale.paid webhooks fired so far.
func (c *claimHarness) webhookCount() int64 { return atomic.LoadInt64(c.webhooks) }

// TestChipCallbackFailedAttemptIsReprocessedOnRedelivery is the #517
// regression: a delivery whose business logic fails (500) must NOT consume the
// dedup key. A redelivery has to settle the sale — exactly once, with exactly
// one webhook.
//
// The failure is reproduced with a real DB condition rather than a stub: a
// second connection holds the per-sale advisory lock (a long-running sibling
// callback, or the state a deploy restart mid-transaction leaves behind), so
// the first delivery fails on the locked DB operation and returns 500 — the
// documented failure path from the issue.
//
// Before the fix this test fails: the first delivery's insert-first dedup row
// survives the 500, so the redelivery is answered 200 "duplicate" and the sale
// stays pending forever.
func TestChipCallbackFailedAttemptIsReprocessedOnRedelivery(t *testing.T) {
	ctx := context.Background()
	c := newClaimHarness(t, ctx)

	// Hold the per-sale advisory lock exactly as SaleStore.WithSaleLock does.
	blocker, err := c.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin blocker tx: %v", err)
	}
	if _, err := blocker.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1)::bigint)`, c.saleID); err != nil {
		t.Fatalf("take sale lock: %v", err)
	}

	// Delivery 1: blocked on the sale lock → fails → 500.
	deadline, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	rr := c.deliver(t, deadline)
	cancel()
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("delivery 1 (lock held) = %d %s, want 500", rr.Code, rr.Body.String())
	}
	if got := c.saleStatus(t, ctx); got != dbstore.SalePending {
		t.Fatalf("sale status after failed delivery = %q, want %q (nothing may settle)", got, dbstore.SalePending)
	}
	if n := c.webhookCount(); n != 0 {
		t.Fatalf("failed delivery fired %d webhooks, want 0", n)
	}

	// CHIP redelivers after the failure.
	if err := blocker.Rollback(ctx); err != nil {
		t.Fatalf("release sale lock: %v", err)
	}
	rr2 := c.deliver(t, ctx)
	if rr2.Code != http.StatusOK {
		t.Fatalf("redelivery = %d %s, want 200", rr2.Code, rr2.Body.String())
	}

	// THE BUG: on the un-fixed code the redelivery is swallowed as a
	// duplicate and the sale stays pending here.
	if got := c.saleStatus(t, ctx); got != dbstore.SalePaid {
		t.Fatalf("sale status after redelivery = %q, want %q — a failed callback was swallowed as a duplicate (#517)", got, dbstore.SalePaid)
	}
	if n := c.webhookCount(); n != 1 {
		t.Fatalf("webhooks after redelivery = %d, want exactly 1", n)
	}

	// A further redelivery must be a no-op duplicate: no second settlement,
	// no second webhook.
	rr3 := c.deliver(t, ctx)
	if rr3.Code != http.StatusOK {
		t.Fatalf("third delivery = %d %s, want 200 duplicate", rr3.Code, rr3.Body.String())
	}
	if got := c.saleStatus(t, ctx); got != dbstore.SalePaid {
		t.Fatalf("sale status after third delivery = %q, want %q", got, dbstore.SalePaid)
	}
	if n := c.webhookCount(); n != 1 {
		t.Fatalf("webhooks after third delivery = %d, want exactly 1 (no double-fire)", n)
	}
}

// TestChipCallbackSuccessThenRedeliveryIsNoop pins the invariant that a
// callback which SUCCEEDED stays idempotent: a redelivery is a no-op duplicate
// with no second webhook (the receipt, notifications and subscription
// activation ride the same pending → paid transition, which never re-runs).
func TestChipCallbackSuccessThenRedeliveryIsNoop(t *testing.T) {
	ctx := context.Background()
	c := newClaimHarness(t, ctx)

	if rr := c.deliver(t, ctx); rr.Code != http.StatusOK {
		t.Fatalf("delivery 1 = %d %s, want 200", rr.Code, rr.Body.String())
	}
	if got := c.saleStatus(t, ctx); got != dbstore.SalePaid {
		t.Fatalf("sale status = %q, want %q", got, dbstore.SalePaid)
	}
	if n := c.webhookCount(); n != 1 {
		t.Fatalf("webhooks = %d, want 1", n)
	}

	var processedAt *time.Time
	if err := c.pool.QueryRow(ctx,
		`SELECT processed_at FROM webhook_events WHERE dedup_key LIKE $1`,
		c.purchaseID+":%").Scan(&processedAt); err != nil {
		t.Fatalf("read event row: %v", err)
	}
	if processedAt == nil {
		t.Fatal("event row processed_at is NULL after a successful callback")
	}

	rr := c.deliver(t, ctx)
	if rr.Code != http.StatusOK {
		t.Fatalf("redelivery = %d %s, want 200", rr.Code, rr.Body.String())
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte("duplicate")) {
		t.Fatalf("redelivery body = %s, want duplicate", rr.Body.String())
	}
	if got := c.saleStatus(t, ctx); got != dbstore.SalePaid {
		t.Fatalf("sale status after redelivery = %q, want %q", got, dbstore.SalePaid)
	}
	if n := c.webhookCount(); n != 1 {
		t.Fatalf("webhooks after redelivery = %d, want exactly 1 (no double-fire)", n)
	}
}

// TestChipCallbackConcurrentIdenticalDeliveriesSettleOnce verifies two
// simultaneous identical deliveries cannot both process: the sale settles once
// and exactly one webhook fires.
func TestChipCallbackConcurrentIdenticalDeliveriesSettleOnce(t *testing.T) {
	ctx := context.Background()
	c := newClaimHarness(t, ctx)

	start := make(chan struct{})
	var wg sync.WaitGroup
	codes := make([]int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			codes[i] = c.deliver(t, ctx).Code
		}(i)
	}
	close(start)
	wg.Wait()

	for i, code := range codes {
		if code != http.StatusOK {
			t.Fatalf("concurrent delivery %d = %d, want 200", i, code)
		}
	}
	if got := c.saleStatus(t, ctx); got != dbstore.SalePaid {
		t.Fatalf("sale status = %q, want %q", got, dbstore.SalePaid)
	}
	if n := c.webhookCount(); n != 1 {
		t.Fatalf("webhooks = %d, want exactly 1 for concurrent identical deliveries", n)
	}
}
