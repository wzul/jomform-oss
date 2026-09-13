package payments

// Issue #518: partial-refund accounting + the over-refund guard (money
// correctness).
//
// Before the fix a partial refund marked the sale FULLY refunded and recorded
// nothing, so the outstanding balance was unreachable and there was no guard
// against refunding more than the sale total. These tests pin the guard and the
// race safety against a real Postgres, reusing the #202 lock fixtures
// (testdb.Open, seedLockSale, WithSaleLock).
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL);
// skipped without it.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/chip"
	"github.com/wzul/jomform/api/internal/cryptoutil"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// refundTestService builds a payments Service whose CHIP client points at a
// stub that records every refund request. The account is given CHIP
// credentials so decryptSecret succeeds. Returns the service + the request
// counter + the account id.
func refundTestService(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (*Service, *int64) {
	t.Helper()
	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ('it-r518-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	var accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 SELECT $1, 'it-r518-acc-' || md5(random()::text), 'it-r518-' || md5(random()::text)
		 RETURNING id`, userID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	sealed, err := cryptoutil.Encrypt(key, []byte("sk_test_refund518"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if err := dbstore.NewAccountStore(pool).SetChipCredentials(ctx, accountID, "br_r518", sealed, "test", "", nil, true); err != nil {
		t.Fatalf("set chip creds: %v", err)
	}

	var hits int64
	mux := http.NewServeMux()
	mux.HandleFunc("POST /purchases/{id}/refund/", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": r.PathValue("id"), "status": chip.StatusRefunded})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	svc := &Service{
		Chip:       chip.New(srv.URL),
		Accounts:   dbstore.NewAccountStore(pool),
		Sales:      dbstore.NewSaleStore(pool),
		DecryptKey: key,
		BaseURL:    "https://app.example",
	}
	t.Cleanup(func() { _ = accountID })
	return svc, &hits
}

// seedRefundSale creates a PAID sale with a CHIP purchase id on the account
// returned by refundTestService.
func seedRefundSale(t *testing.T, ctx context.Context, pool *pgxpool.Pool, totalSen int64) (saleID, refCode string) {
	t.Helper()
	var accountID string
	if err := pool.QueryRow(ctx, `SELECT id FROM accounts ORDER BY created_at DESC LIMIT 1`).Scan(&accountID); err != nil {
		t.Fatalf("resolve account: %v", err)
	}
	sale := &dbstore.Sale{
		AccountID: accountID, RefCode: "IT-R518-" + accountID[:8],
		Client:      dbstore.SaleClient{FullName: "Buyer", Email: "buyer@example.test"},
		Items:       []dbstore.SaleItem{{ProductID: "p1", Name: "Tee", PriceSen: totalSen, Qty: 1}},
		SubtotalSen: totalSen, TotalSen: totalSen,
		SaleType: dbstore.SaleTypeOneTime, PaymentMethod: "chip", Status: dbstore.SalePaid,
	}
	if err := dbstore.NewSaleStore(pool).Create(ctx, sale); err != nil {
		t.Fatalf("create sale: %v", err)
	}
	purchaseID := "pur_r518_" + accountID[:8]
	if err := dbstore.NewSaleStore(pool).AttachChipPurchase(ctx, sale.ID, purchaseID, "https://gate.example/co/"+purchaseID); err != nil {
		t.Fatalf("attach purchase: %v", err)
	}
	return sale.ID, sale.RefCode
}

func loadSale(t *testing.T, pool *pgxpool.Pool, saleID string) *dbstore.Sale {
	t.Helper()
	s, err := dbstore.NewSaleStore(pool).ByID(context.Background(), saleID)
	if err != nil {
		t.Fatalf("reload sale: %v", err)
	}
	return s
}

// TestRefund518PartialRecordsAmountAndStaysPartial is the core defect pinned at
// the service layer: a partial refund records the amount, leaves the sale
// reading as partial, and does not flip the status to the terminal `refunded`.
func TestRefund518PartialRecordsAmountAndStaysPartial(t *testing.T) {
	ctx := context.Background()
	pool := lockTestPool(t)
	svc, hits := refundTestService(t, ctx, pool)
	saleID, _ := seedRefundSale(t, ctx, pool, 5000)

	sale := loadSale(t, pool, saleID)
	amount := int64(1000)
	res, err := svc.RefundSale(ctx, sale, &amount)
	if err != nil {
		t.Fatalf("partial refund: %v", err)
	}
	if res.RefundedSen != 1000 || res.OutstandingSen != 4000 {
		t.Errorf("result refunded=%d outstanding=%d, want 1000 / 4000", res.RefundedSen, res.OutstandingSen)
	}
	if res.FullyRefunded {
		t.Errorf("FullyRefunded = true after a partial refund")
	}
	got := loadSale(t, pool, saleID)
	if got.RefundedSen != 1000 {
		t.Errorf("sale.refunded_sen = %d, want 1000", got.RefundedSen)
	}
	if got.Status == dbstore.SaleRefunded {
		t.Errorf("sale status = %q after a partial refund — must stay partially refunded, not fully refunded", got.Status)
	}
	if got.RefundStatus() != dbstore.RefundPartial {
		t.Errorf("RefundStatus() = %q, want %q", got.RefundStatus(), dbstore.RefundPartial)
	}
	if n := atomic.LoadInt64(hits); n != 1 {
		t.Errorf("CHIP refund calls = %d, want 1", n)
	}
}

// TestRefund518RemainderCompletesExactlyOnce refunds part, then the remainder:
// only the second refund marks the sale fully refunded, and the recorded total
// is exactly the sale total.
func TestRefund518RemainderCompletesExactlyOnce(t *testing.T) {
	ctx := context.Background()
	pool := lockTestPool(t)
	svc, hits := refundTestService(t, ctx, pool)
	saleID, _ := seedRefundSale(t, ctx, pool, 5000)

	first := int64(1000)
	if _, err := svc.RefundSale(ctx, loadSale(t, pool, saleID), &first); err != nil {
		t.Fatalf("first partial: %v", err)
	}
	rest := int64(4000)
	res, err := svc.RefundSale(ctx, loadSale(t, pool, saleID), &rest)
	if err != nil {
		t.Fatalf("remainder: %v", err)
	}
	if !res.FullyRefunded {
		t.Errorf("FullyRefunded = false after refunding the remainder")
	}
	got := loadSale(t, pool, saleID)
	if got.RefundedSen != 5000 {
		t.Errorf("sale.refunded_sen = %d, want 5000", got.RefundedSen)
	}
	if got.Status != dbstore.SaleRefunded {
		t.Errorf("sale status = %q, want %q once fully refunded", got.Status, dbstore.SaleRefunded)
	}
	if n := atomic.LoadInt64(hits); n != 2 {
		t.Errorf("CHIP refund calls = %d, want 2", n)
	}

	// A third refund must be rejected by the refundable-state guard and must
	// not reach CHIP.
	small := int64(100)
	if _, err := svc.RefundSale(ctx, loadSale(t, pool, saleID), &small); err == nil {
		t.Errorf("refund after full refund was accepted — expected the refundable-state guard to reject it")
	}
	if n := atomic.LoadInt64(hits); n != 2 {
		t.Errorf("CHIP refund calls = %d, want 2 — nothing may reach CHIP after a full refund", n)
	}
}

// TestRefund518NilAmountRefundsOutstanding is the full-refund semantics: a nil
// amount returns the remaining balance, not "the whole total again".
func TestRefund518NilAmountRefundsOutstanding(t *testing.T) {
	ctx := context.Background()
	pool := lockTestPool(t)
	svc, hits := refundTestService(t, ctx, pool)
	saleID, _ := seedRefundSale(t, ctx, pool, 5000)

	partial := int64(2000)
	if _, err := svc.RefundSale(ctx, loadSale(t, pool, saleID), &partial); err != nil {
		t.Fatalf("partial: %v", err)
	}
	// nil = full refund → must return the OUTSTANDING 3000, not 5000.
	res, err := svc.RefundSale(ctx, loadSale(t, pool, saleID), nil)
	if err != nil {
		t.Fatalf("nil full refund: %v", err)
	}
	if res.AmountSen != 3000 {
		t.Errorf("nil full refund amount = %d, want the outstanding balance 3000", res.AmountSen)
	}
	if got := loadSale(t, pool, saleID); got.RefundedSen != 5000 {
		t.Errorf("sale.refunded_sen = %d, want 5000 (never more than total)", got.RefundedSen)
	}
	if n := atomic.LoadInt64(hits); n != 2 {
		t.Errorf("CHIP refund calls = %d, want 2", n)
	}
}

// TestRefund518OverRefundRejectedBeforeChip is the missing over-refund guard:
// refunded_sen + amount_sen > total_sen must be rejected BEFORE CHIP is called.
func TestRefund518OverRefundRejectedBeforeChip(t *testing.T) {
	ctx := context.Background()
	pool := lockTestPool(t)
	svc, hits := refundTestService(t, ctx, pool)
	saleID, _ := seedRefundSale(t, ctx, pool, 5000)

	first := int64(4000)
	if _, err := svc.RefundSale(ctx, loadSale(t, pool, saleID), &first); err != nil {
		t.Fatalf("first refund: %v", err)
	}
	before := atomic.LoadInt64(hits)

	over := int64(2000) // 4000 + 2000 > 5000
	_, err := svc.RefundSale(ctx, loadSale(t, pool, saleID), &over)
	if err == nil {
		t.Fatalf("over-refund was ACCEPTED — the missing guard is back (issue #518)")
	}
	if after := atomic.LoadInt64(hits); after != before {
		t.Errorf("CHIP refund calls = %d (was %d) — an over-refund must be rejected BEFORE calling CHIP", after, before)
	}
	if got := loadSale(t, pool, saleID); got.RefundedSen != 4000 {
		t.Errorf("sale.refunded_sen = %d, want 4000 (a rejected refund must not be recorded)", got.RefundedSen)
	}
	// The balance survives the rejection.
	rest := int64(1000)
	if _, err := svc.RefundSale(ctx, loadSale(t, pool, saleID), &rest); err != nil {
		t.Fatalf("remainder after rejection: %v", err)
	}
	if got := loadSale(t, pool, saleID); got.RefundedSen != 5000 {
		t.Errorf("sale.refunded_sen = %d, want 5000", got.RefundedSen)
	}
}

// TestRefund518ConcurrentPartialsCannotOverRefund is the race test: two
// concurrent partial refunds must not both pass the balance check and push
// refunded_sen past total_sen. The check + increment run under the per-sale
// advisory lock (WithSaleLock), so exactly one of the two RM 30 refunds against
// a RM 50 sale may succeed, and CHIP must be called exactly once.
func TestRefund518ConcurrentPartialsCannotOverRefund(t *testing.T) {
	ctx := context.Background()
	pool := lockTestPool(t)
	svc, hits := refundTestService(t, ctx, pool)
	saleID, _ := seedRefundSale(t, ctx, pool, 5000)

	amount := int64(3000) // 3000 + 3000 > 5000: at most one may win
	var wg sync.WaitGroup
	var okCount, errCount int64
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sale, err := dbstore.NewSaleStore(pool).ByID(ctx, saleID)
			if err != nil {
				atomic.AddInt64(&errCount, 1)
				return
			}
			if _, err := svc.RefundSale(ctx, sale, &amount); err != nil {
				atomic.AddInt64(&errCount, 1)
				return
			}
			atomic.AddInt64(&okCount, 1)
		}()
	}
	wg.Wait()

	got := loadSale(t, pool, saleID)
	if got.RefundedSen > got.TotalSen {
		t.Errorf("refunded_sen = %d > total_sen = %d — concurrent partials OVER-REFUNDED (issue #518)", got.RefundedSen, got.TotalSen)
	}
	if got.RefundedSen != 3000 {
		t.Errorf("refunded_sen = %d, want exactly 3000 (only one of the two RM 30 refunds may win)", got.RefundedSen)
	}
	if okCount != 1 {
		t.Errorf("successful refunds = %d, want exactly 1", okCount)
	}
	if errCount != 1 {
		t.Errorf("rejected refunds = %d, want exactly 1", errCount)
	}
	if n := atomic.LoadInt64(hits); n != 1 {
		t.Errorf("CHIP refund calls = %d, want exactly 1 — the loser must be rejected before CHIP", n)
	}
	if got.Status == dbstore.SaleRefunded {
		t.Errorf("sale status = %q — 3000 of 5000 is a PARTIAL refund", got.Status)
	}
}

// TestRefund518NonRefundableSaleStillRejected preserves the paid/confirmed
// guard end to end.
func TestRefund518NonRefundableSaleStillRejected(t *testing.T) {
	ctx := context.Background()
	pool := lockTestPool(t)
	svc, hits := refundTestService(t, ctx, pool)
	saleID, _ := seedRefundSale(t, ctx, pool, 5000)

	if err := dbstore.NewSaleStore(pool).SetStatus(ctx, saleID, dbstore.SalePending); err != nil {
		t.Fatalf("reset status: %v", err)
	}
	amount := int64(1000)
	if _, err := svc.RefundSale(ctx, loadSale(t, pool, saleID), &amount); err == nil {
		t.Errorf("refund of a PENDING sale was accepted — the paid/confirmed guard is broken")
	}
	if n := atomic.LoadInt64(hits); n != 0 {
		t.Errorf("CHIP refund calls = %d, want 0 — a non-refundable sale must be rejected before CHIP", n)
	}
	if got := loadSale(t, pool, saleID); got.RefundedSen != 0 {
		t.Errorf("refunded_sen = %d, want 0", got.RefundedSen)
	}
}

// TestRefund518NonPositiveAmountRejected preserves the > 0 check.
func TestRefund518NonPositiveAmountRejected(t *testing.T) {
	ctx := context.Background()
	pool := lockTestPool(t)
	svc, hits := refundTestService(t, ctx, pool)
	saleID, _ := seedRefundSale(t, ctx, pool, 5000)

	for _, amount := range []int64{0, -250} {
		if _, err := svc.RefundSale(ctx, loadSale(t, pool, saleID), &amount); err == nil {
			t.Errorf("amount_sen = %d was accepted, want a rejection", amount)
		}
	}
	if n := atomic.LoadInt64(hits); n != 0 {
		t.Errorf("CHIP refund calls = %d, want 0 for non-positive amounts", n)
	}
}

// TestRefund518WebhookRefundedRecordsFullAmount keeps the money invariant
// refunded_sen <= total_sen on the purchase.refunded webhook path: an
// out-of-band refund flips the sale terminal, so the recorded amount must be
// the full total.
func TestRefund518WebhookRefundedRecordsFullAmount(t *testing.T) {
	ctx := context.Background()
	pool := lockTestPool(t)
	purchaseID := fmt.Sprintf("pur_webhook_r518_%d", time.Now().UnixNano())
	sale := seedPaidSaleForWebhook(t, ctx, pool, 5000, purchaseID)

	svc, _ := refundTestService(t, ctx, pool)
	// Point the sale at the refund fixture's account so decrypt resolves… not
	// needed: ProcessCallback does not decrypt. Use the sale's own store.
	if err := svc.Sales.SetStatus(ctx, sale.ID, dbstore.SalePaid); err != nil {
		t.Fatalf("set paid: %v", err)
	}
	if err := svc.ProcessCallback(ctx, &chip.Purchase{ID: purchaseID, Status: chip.StatusRefunded}); err != nil {
		t.Fatalf("refunded callback: %v", err)
	}
	got := loadSale(t, pool, sale.ID)
	if got.Status != dbstore.SaleRefunded {
		t.Fatalf("status = %q, want %q", got.Status, dbstore.SaleRefunded)
	}
	if got.RefundedSen != got.TotalSen {
		t.Errorf("refunded_sen = %d, want the full total %d after a terminal refunded webhook", got.RefundedSen, got.TotalSen)
	}
	if got.RefundedSen > got.TotalSen {
		t.Errorf("refunded_sen = %d > total_sen = %d — money invariant violated", got.RefundedSen, got.TotalSen)
	}
}

// seedPaidSaleForWebhook creates a pending sale with a CHIP purchase id whose
// account has CHIP credentials, so ProcessCallback's transitions run.
func seedPaidSaleForWebhook(t *testing.T, ctx context.Context, pool *pgxpool.Pool, totalSen int64, purchaseID string) *dbstore.Sale {
	t.Helper()
	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ('it-wh518-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	var accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 SELECT $1, 'it-wh518-acc-' || md5(random()::text), 'it-wh518-' || md5(random()::text)
		 RETURNING id`, userID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	sale := &dbstore.Sale{
		AccountID: accountID, RefCode: "IT-WH518-" + accountID[:8],
		Client:      dbstore.SaleClient{FullName: "Buyer", Email: "buyer@example.test"},
		Items:       []dbstore.SaleItem{{ProductID: "p1", Name: "Tee", PriceSen: totalSen, Qty: 1}},
		SubtotalSen: totalSen, TotalSen: totalSen,
		SaleType: dbstore.SaleTypeOneTime, PaymentMethod: "chip", Status: dbstore.SalePending,
	}
	if err := dbstore.NewSaleStore(pool).Create(ctx, sale); err != nil {
		t.Fatalf("create sale: %v", err)
	}
	if err := dbstore.NewSaleStore(pool).AttachChipPurchase(ctx, sale.ID, purchaseID, "https://gate.example/co/"+purchaseID); err != nil {
		t.Fatalf("attach purchase: %v", err)
	}
	return sale
}
