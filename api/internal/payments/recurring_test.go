package payments

// Issue #152: subscription billing count — infinite vs fixed N charges.
// A subscription with subscription_charges = N stops after the Nth charge
// (the initial token purchase is charge #1) and is marked 'completed', so no
// further charges are scheduled. Integration against a real Postgres; skipped
// without JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/chip"
	"github.com/wzul/jomform/api/internal/cryptoutil"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// recurTestPool returns the payments package scratch-schema pool.
//
// The recurring-engine fixture reuses a fixed CHIP purchase id
// ("pur_charge_child") that is UNIQUE on subscription_charges; before issue
// #474 this needed a manual `TRUNCATE subscription_charges` because a dirty
// shared DB made AddCharge fail silently. The scratch schema is re-created on
// every run, so the table starts empty and no shared table is truncated —
// removing the cross-package race that truncate used to cause.
func recurTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return testdb.Open(t)
}

// seedRecurringFixture creates an account with CHIP creds, a subscription
// product with a fixed billing count (nil = infinite), and a due active
// subscription.
func seedRecurringFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, charges *int) (accountID, productID, subID string) {
	t.Helper()
	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ('it-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 SELECT $1, 'it-acc-' || md5(random()::text), 'it-' || md5(random()::text)
		 RETURNING id`, userID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	sealed, err := cryptoutil.Encrypt(key, []byte("sk_test_recurring"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if err := dbstore.NewAccountStore(pool).SetChipCredentials(ctx, accountID, "br_recur", sealed, "test", "", nil, true); err != nil {
		t.Fatalf("set chip creds: %v", err)
	}
	prod := &dbstore.Product{
		AccountID: accountID, Name: "Sub N", PriceSen: 1500,
		Type: dbstore.SaleTypeSubscription, BillingPeriod: "month", Active: true,
		SubscriptionCharges: charges,
	}
	if err := dbstore.NewProductStore(pool).Create(ctx, prod); err != nil {
		t.Fatalf("create product: %v", err)
	}
	// Unique per run: subscription_charges has a UNIQUE constraint on
	// chip_purchase_id, so a fixed literal collides on a second run (or with a
	// parallel test) against the shared test database.
	tokenID := fmt.Sprintf("tok-purchase-%d", time.Now().UnixNano())
	sub := &dbstore.Subscription{
		AccountID: accountID, ClientEmail: "buyer@example.com", ProductID: prod.ID,
		ChipTokenPurchaseID: tokenID, Status: dbstore.SubActive, Period: "month",
		AmountSen: 1500, NextChargeAt: time.Now().Add(-time.Minute),
	}
	if err := dbstore.NewSubscriptionStore(pool).Create(ctx, sub); err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	// Model production's activateSubscription: the customer's first payment is
	// recorded as a PAID charge row stamped with the token purchase id, so
	// ChargeCount() starts at 1. Without this the fixture would not represent a
	// real subscription and would mask exactly the accounting the engine relies
	// on (a fixed N-charge subscription is complete once it has N PAID rows).
	if err := dbstore.NewSubscriptionStore(pool).AddCharge(
		ctx, sub.ID, tokenID, 1500, dbstore.ChargePaid, 0); err != nil {
		t.Fatalf("seed initial paid charge: %v", err)
	}
	return accountID, prod.ID, sub.ID
}

// mockChipServer returns a CHIP client backed by a stub that always returns a
// successful (pending_charge) purchase for /purchases/ + /charge/. The charge
// id is unique per call so repeated runs (or `-count=N`) never collide on the
// subscription_charges UNIQUE constraint — no shared-table truncation needed.
func mockChipServer(t *testing.T, chargeID string) *chip.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /purchases/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": chargeID, "status": "created"})
	})
	mux.HandleFunc("POST /purchases/"+chargeID+"/charge/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": chargeID, "status": "pending_charge"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return chip.New(srv.URL)
}

// TestRecurringEngineStopsAfterNCharges verifies that a subscription with a
// fixed billing count stops after the Nth charge: the engine charges while
// the count is below N, and marks the subscription completed once the Nth
// charge is reached (no further charges scheduled).
func TestRecurringEngineStopsAfterNCharges(t *testing.T) {
	ctx := context.Background()
	pool := recurTestPool(t)
	two := 2
	accountID, productID, subID := seedRecurringFixture(t, ctx, pool, &two)

	subs := dbstore.NewSubscriptionStore(pool)
	products := dbstore.NewProductStore(pool)
	accounts := dbstore.NewAccountStore(pool)
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	// Unique charge id per run so repeated runs (or `-count=N`) never collide
	// on the subscription_charges UNIQUE constraint.
	chargeID := fmt.Sprintf("pur_charge_child_%d", time.Now().UnixNano())
	svc := &Service{
		Chip:       mockChipServer(t, chargeID),
		Accounts:   accounts,
		Subs:       subs,
		Products:   products,
		DecryptKey: key,
		BaseURL:    "https://app.example",
	}

	// Before any recurring charge: total consumed = 1 (token purchase) < N=2,
	// so the subscription must NOT be completed and a charge should proceed.
	sub, err := subs.ByIDForAccount(ctx, accountID, subID)
	if err != nil {
		t.Fatalf("load sub: %v", err)
	}
	done, err := svc.maybeCompleteSubscription(ctx, sub)
	if err != nil {
		t.Fatalf("maybeComplete before charges: %v", err)
	}
	if done {
		t.Fatalf("subscription completed before reaching N charges")
	}
	if err := svc.chargeSubscription(ctx, sub); err != nil {
		t.Fatalf("charge subscription: %v", err)
	}
	// The charge was created as pending; simulate the CHIP callback marking it
	// paid, then the engine must advance the count and complete the sub.
	if err := subs.MarkChargePaid(ctx, chargeID); err != nil {
		t.Fatalf("mark charge paid: %v", err)
	}
	sub2, err := subs.ByIDForAccount(ctx, accountID, subID)
	if err != nil {
		t.Fatalf("reload sub: %v", err)
	}
	if err := svc.completeIfLimitReached(ctx, sub2); err != nil {
		t.Fatalf("completeIfLimitReached: %v", err)
	}
	got, err := subs.ByIDForAccount(ctx, accountID, subID)
	if err != nil {
		t.Fatalf("reload sub after completion: %v", err)
	}
	if got.Status != dbstore.SubCompleted {
		t.Fatalf("sub.status = %q, want completed after Nth charge", got.Status)
	}
	// A completed subscription must not be picked up by the due-charge scan.
	due, err := subs.DueActive(ctx, time.Now().Add(time.Minute), 10)
	if err != nil {
		t.Fatalf("due scan: %v", err)
	}
	for _, d := range due {
		if d.ID == subID {
			t.Fatalf("completed subscription still scheduled for a charge")
		}
	}
	_ = productID
}

// TestRecurringEngineInfiniteKeepsCharging verifies that a subscription with
// NULL subscription_charges (infinite) is never completed by the engine.
func TestRecurringEngineInfiniteKeepsCharging(t *testing.T) {
	ctx := context.Background()
	pool := recurTestPool(t)
	accountID, _, subID := seedRecurringFixture(t, ctx, pool, nil)

	subs := dbstore.NewSubscriptionStore(pool)
	products := dbstore.NewProductStore(pool)
	accounts := dbstore.NewAccountStore(pool)
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	svc := &Service{
		Chip:       mockChipServer(t, fmt.Sprintf("pur_charge_child_%d", time.Now().UnixNano())),
		Accounts:   accounts,
		Subs:       subs,
		Products:   products,
		DecryptKey: key,
		BaseURL:    "https://app.example",
	}

	sub, err := subs.ByIDForAccount(ctx, accountID, subID)
	if err != nil {
		t.Fatalf("load sub: %v", err)
	}
	// Force the product's subscription_charges to NULL (infinite).
	var nilCharges *int
	if err := products.Update(ctx, accountID, sub.ProductID, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, &nilCharges); err != nil {
		t.Fatalf("clear subscription_charges: %v", err)
	}
	done, err := svc.maybeCompleteSubscription(ctx, sub)
	if err != nil {
		t.Fatalf("maybeComplete infinite: %v", err)
	}
	if done {
		t.Fatalf("infinite subscription must never be completed")
	}
	if err := svc.chargeSubscription(ctx, sub); err != nil {
		t.Fatalf("charge infinite subscription: %v", err)
	}
	got, err := subs.ByIDForAccount(ctx, accountID, subID)
	if err != nil {
		t.Fatalf("reload sub: %v", err)
	}
	if got.Status != dbstore.SubActive {
		t.Fatalf("infinite sub.status = %q, want active", got.Status)
	}
}
