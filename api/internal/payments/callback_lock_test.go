package payments

// Issue #202: per-sale callback locking. Two different events for the same
// sale (e.g. purchase.paid racing a payment_failure, or a redelivered paid
// event) must be serialized per sale: only one callback processes the sale at
// a time, conflicting status transitions are rejected (no paid → failed flip),
// and side effects (webhooks) are idempotent under the lock.
//
// Integration against a real Postgres; skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/chip"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

func lockTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return testdb.Open(t)
}

// seedLockSale creates a pending one-time sale with a CHIP purchase id and
// returns the sale id + purchase id.
func seedLockSale(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (saleID, purchaseID string) {
	t.Helper()
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
		AccountID: accountID, RefCode: "IT-LOCK-" + accountID[:8],
		Client:   dbstore.SaleClient{FullName: "Buyer", Email: "buyer@example.test"},
		Items:    []dbstore.SaleItem{{ProductID: "p1", Name: "Tee", PriceSen: 1000, Qty: 1}},
		TotalSen: 1000, SaleType: dbstore.SaleTypeOneTime, PaymentMethod: "chip",
		Status: dbstore.SalePending,
	}
	if err := dbstore.NewSaleStore(pool).Create(ctx, sale); err != nil {
		t.Fatalf("create sale: %v", err)
	}
	purchaseID = "pur_lock_" + accountID[:8]
	if err := dbstore.NewSaleStore(pool).AttachChipPurchase(ctx, sale.ID, purchaseID, "https://gate.example/checkout/"+purchaseID); err != nil {
		t.Fatalf("attach purchase: %v", err)
	}
	return sale.ID, purchaseID
}

// TestProcessCallbackSerializedPerSale verifies the per-sale lock serializes
// callbacks and the transition guard rejects a conflicting late event. The
// paid event settles the sale first; then a duplicate paid event and a late
// payment_failure race concurrently — the sale must stay paid (no paid → failed
// flip) and exactly one webhook fires.
func TestProcessCallbackSerializedPerSale(t *testing.T) {
	ctx := context.Background()
	pool := lockTestPool(t)
	saleID, purchaseID := seedLockSale(t, ctx, pool)

	webhooks := testdb.NewWebhookCounter()
	svc := &Service{
		Sales:    dbstore.NewSaleStore(pool),
		Accounts: dbstore.NewAccountStore(pool),
		OnPaid:   webhooks.OnPaid,
	}

	// First paid event settles the sale.
	if err := svc.ProcessCallback(ctx, &chip.Purchase{ID: purchaseID, Status: chip.StatusPaid}); err != nil {
		t.Fatalf("paid callback: %v", err)
	}

	// Now race a duplicate paid event against a late failure. Both must be
	// serialized per sale; the late failure must NOT flip paid → failed, and
	// the duplicate paid must not re-fire the webhook.
	paid := &chip.Purchase{ID: purchaseID, Status: chip.StatusPaid}
	failed := &chip.Purchase{ID: purchaseID, Status: chip.StatusError}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = svc.ProcessCallback(ctx, paid) }()
	go func() { defer wg.Done(); _ = svc.ProcessCallback(ctx, failed) }()
	wg.Wait()

	got, err := dbstore.NewSaleStore(pool).ByID(ctx, saleID)
	if err != nil {
		t.Fatalf("reload sale: %v", err)
	}
	if got.Status != dbstore.SalePaid {
		t.Fatalf("final status = %q, want %q (late failure must not flip paid → failed)", got.Status, dbstore.SalePaid)
	}
	webhooks.WaitExact(t, 1, testdb.WebhookSettle, "webhooks fired (idempotent under lock)")
}

// TestProcessCallbackPaidIdempotent races two identical paid events for the
// same sale. Only one may fire the webhook; the second is a no-op.
func TestProcessCallbackPaidIdempotent(t *testing.T) {
	ctx := context.Background()
	pool := lockTestPool(t)
	_, purchaseID := seedLockSale(t, ctx, pool)

	webhooks := testdb.NewWebhookCounter()
	svc := &Service{
		Sales:    dbstore.NewSaleStore(pool),
		Accounts: dbstore.NewAccountStore(pool),
		OnPaid:   webhooks.OnPaid,
	}

	paid := &chip.Purchase{ID: purchaseID, Status: chip.StatusPaid}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = svc.ProcessCallback(ctx, paid) }()
	go func() { defer wg.Done(); _ = svc.ProcessCallback(ctx, paid) }()
	wg.Wait()

	webhooks.WaitExact(t, 1, testdb.WebhookSettle, "webhooks fired (duplicate paid event must be idempotent)")
}

// TestProcessCallbackLateFailureRejected verifies a payment_failure arriving
// after the sale is already paid is rejected (no paid → failed flip).
func TestProcessCallbackLateFailureRejected(t *testing.T) {
	ctx := context.Background()
	pool := lockTestPool(t)
	saleID, purchaseID := seedLockSale(t, ctx, pool)

	svc := &Service{Sales: dbstore.NewSaleStore(pool), Accounts: dbstore.NewAccountStore(pool)}

	// First mark the sale paid (as a prior paid callback would).
	if err := svc.ProcessCallback(ctx, &chip.Purchase{ID: purchaseID, Status: chip.StatusPaid}); err != nil {
		t.Fatalf("paid callback: %v", err)
	}
	// Now a late failure must be ignored.
	if err := svc.ProcessCallback(ctx, &chip.Purchase{ID: purchaseID, Status: chip.StatusError}); err != nil {
		t.Fatalf("late failure callback: %v", err)
	}

	got, err := dbstore.NewSaleStore(pool).ByID(ctx, saleID)
	if err != nil {
		t.Fatalf("reload sale: %v", err)
	}
	if got.Status != dbstore.SalePaid {
		t.Fatalf("status after late failure = %q, want %q", got.Status, dbstore.SalePaid)
	}
}

// TestProcessCallbackRefunded verifies a purchase.refunded webhook flips a
// paid sale to refunded (issue #407).
func TestProcessCallbackRefunded(t *testing.T) {
	ctx := context.Background()
	pool := lockTestPool(t)
	saleID, purchaseID := seedLockSale(t, ctx, pool)

	svc := &Service{Sales: dbstore.NewSaleStore(pool), Accounts: dbstore.NewAccountStore(pool)}

	// First mark the sale paid (as a prior paid callback would).
	if err := svc.ProcessCallback(ctx, &chip.Purchase{ID: purchaseID, Status: chip.StatusPaid}); err != nil {
		t.Fatalf("paid callback: %v", err)
	}
	// Now the refunded event flips it to refunded.
	if err := svc.ProcessCallback(ctx, &chip.Purchase{ID: purchaseID, Status: chip.StatusRefunded}); err != nil {
		t.Fatalf("refunded callback: %v", err)
	}

	got, err := dbstore.NewSaleStore(pool).ByID(ctx, saleID)
	if err != nil {
		t.Fatalf("reload sale: %v", err)
	}
	if got.Status != dbstore.SaleRefunded {
		t.Fatalf("status after refunded callback = %q, want %q", got.Status, dbstore.SaleRefunded)
	}
}

var _ = time.Now
