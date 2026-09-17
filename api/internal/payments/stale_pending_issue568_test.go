package payments

// Issue #568: a pending charge whose callback never arrived blocked its
// subscription FOREVER.
//
// A charge row is created 'pending' and resolved only by the CHIP callback.
// afterFailedCharge (the retry ladder) is reached only when Chip.Charge returns
// an error, so a charge the gateway ACCEPTED whose callback was lost stayed
// pending for good — and ClaimDue refuses to charge a subscription that has any
// pending row:
//
//	AND NOT EXISTS (SELECT 1 FROM subscription_charges c
//	                 WHERE c.subscription_id = $1 AND c.status = 'pending')
//
// The subscription kept reading 'active', the engine kept finding it due, the
// claim was declined every hour, and nothing surfaced why. Observed live: a
// renewal charge sat pending after a 503'd callback and blocked the next sweep
// until the row was cleared by hand — after which the very next sweep created a
// fresh charge that completed normally.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/chip"
	"github.com/wzul/jomform/api/internal/cryptoutil"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// TestIssue568StalePendingChargeNoLongerBlocksTheSubscription is the regression.
func TestIssue568StalePendingChargeNoLongerBlocksTheSubscription(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)

	var userID, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email,password_hash) VALUES ('r568-'||md5(random()::text)||'@example.test','it') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id,name,slug) SELECT $1,'r568-'||md5(random()::text),'r568-'||md5(random()::text) RETURNING id`,
		userID).Scan(&accountID); err != nil {
		t.Fatalf("account: %v", err)
	}
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	sealed, err := cryptoutil.Encrypt(key, []byte("sk_test_568"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if err := dbstore.NewAccountStore(pool).SetChipCredentials(ctx, accountID, "br_568", sealed, "test", "", nil, true); err != nil {
		t.Fatalf("creds: %v", err)
	}
	n := 3
	prod := &dbstore.Product{
		AccountID: accountID, Name: "R568", PriceSen: 10000,
		Type: dbstore.SaleTypeSubscription, BillingPeriod: "month", Active: true,
		SubscriptionCharges: &n,
	}
	if err := dbstore.NewProductStore(pool).Create(ctx, prod); err != nil {
		t.Fatalf("product: %v", err)
	}
	sub := &dbstore.Subscription{
		AccountID: accountID, ClientEmail: "r568@example.test", ProductID: prod.ID,
		ChipTokenPurchaseID: fmt.Sprintf("tok-568-%d", time.Now().UnixNano()),
		Status:              dbstore.SubActive, Period: "month",
		AmountSen: 10000, NextChargeAt: time.Now().Add(-10 * time.Minute),
	}
	subs := dbstore.NewSubscriptionStore(pool)
	if err := subs.Create(ctx, sub); err != nil {
		t.Fatalf("sub: %v", err)
	}
	// The initial payment, paid.
	if err := subs.AddCharge(ctx, sub.ID, sub.ChipTokenPurchaseID, 10000, dbstore.ChargePaid, 0); err != nil {
		t.Fatalf("initial charge: %v", err)
	}

	// A LOST callback: a charge left pending well past the callback timeout.
	if _, err := pool.Exec(ctx,
		`INSERT INTO subscription_charges (subscription_id, chip_purchase_id, amount_sen, status, attempt, charged_at)
		 VALUES ($1, $2, 10000, 'pending', 1, now() - interval '3 hours')`,
		sub.ID, "pur-lost-"+sub.ID[:8]); err != nil {
		t.Fatalf("insert lost charge: %v", err)
	}

	svc := &Service{
		Chip:     stubRejectChip(t),
		Accounts: dbstore.NewAccountStore(pool), Sales: dbstore.NewSaleStore(pool),
		Subs: subs, Products: dbstore.NewProductStore(pool),
		DecryptKey: key, BaseURL: "https://app.example",
	}

	// BEFORE the fix this claim is refused forever, because of the stale row.
	claimed, err := subs.ClaimDue(ctx, sub.ID, time.Hour)
	if err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}
	if claimed {
		t.Fatalf("precondition: the pending row should block the claim before the reap")
	}

	// The sweep must now reap the stale row and NOT stay blocked.
	if err := svc.RunDueCharges(ctx); err != nil && err != dbstore.ErrNoDueSubs {
		t.Fatalf("sweep: %v", err)
	}

	var status, reason string
	if err := pool.QueryRow(ctx,
		`SELECT status, COALESCE(failure_reason,'') FROM subscription_charges WHERE chip_purchase_id=$1`,
		"pur-lost-"+sub.ID[:8]).Scan(&status, &reason); err != nil {
		t.Fatalf("read reaped charge: %v", err)
	}
	if status != dbstore.ChargeFailed {
		t.Errorf("issue #568: the stale pending charge is still %q after a sweep.\n"+
			"A pending row makes ClaimDue refuse its subscription FOREVER, and nothing else "+
			"clears it — the subscription reads 'active' and is silently disabled.", status)
	}
	if reason == "" {
		t.Errorf("issue #568: the reaped charge records no reason — the merchant cannot tell a lost callback from a declined card")
	}

	// And the subscription must actually have been CHARGED again.
	//
	// Asserting on a fresh ClaimDue would be wrong: the sweep's own claim
	// advances next_charge_at by the lease, so the subscription is legitimately
	// not due any more. The signal that matters is that a NEW attempt row exists —
	// before the fix the sweep logged sub_charge_skipped_not_claimed and created
	// nothing at all.
	var attempts int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM subscription_charges WHERE subscription_id=$1 AND attempt >= 1`,
		sub.ID).Scan(&attempts); err != nil {
		t.Fatalf("count attempts: %v", err)
	}
	if attempts == 0 {
		t.Errorf("issue #568: the subscription was never charged after the stale charge was reaped — "+
			"the pending row still blocks the sweep (sub_id=%s)", sub.ID)
	}
}

// TestIssue568HealthyPendingChargeIsNotReaped keeps the timeout honest: a charge
// whose callback is still legitimately in flight must not be written off.
func TestIssue568HealthyPendingChargeIsNotReaped(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)

	var userID, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email,password_hash) VALUES ('r568b-'||md5(random()::text)||'@example.test','it') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id,name,slug) SELECT $1,'r568b-'||md5(random()::text),'r568b-'||md5(random()::text) RETURNING id`,
		userID).Scan(&accountID); err != nil {
		t.Fatalf("account: %v", err)
	}
	prod := &dbstore.Product{AccountID: accountID, Name: "R568b", PriceSen: 100, Type: dbstore.SaleTypeSubscription, BillingPeriod: "month", Active: true}
	if err := dbstore.NewProductStore(pool).Create(ctx, prod); err != nil {
		t.Fatalf("product: %v", err)
	}
	subs := dbstore.NewSubscriptionStore(pool)
	sub := &dbstore.Subscription{
		AccountID: accountID, ClientEmail: "b568@example.test", ProductID: prod.ID,
		ChipTokenPurchaseID: fmt.Sprintf("tok-568b-%d", time.Now().UnixNano()),
		Status:              dbstore.SubActive, Period: "month",
		AmountSen: 100, NextChargeAt: time.Now(),
	}
	if err := subs.Create(ctx, sub); err != nil {
		t.Fatalf("sub: %v", err)
	}
	// Fresh: a callback is still expected.
	if err := subs.AddCharge(ctx, sub.ID, "pur-fresh-"+sub.ID[:8], 100, dbstore.ChargePending, 1); err != nil {
		t.Fatalf("fresh charge: %v", err)
	}

	n, err := subs.FailStalePendingCharges(ctx, time.Now().Add(-chargeCallbackTimeout), staleChargeReason)
	if err != nil {
		t.Fatalf("reap: %v", err)
	}
	if n != 0 {
		t.Errorf("a FRESH pending charge was reaped (count=%d) — the timeout must exceed a normal gateway round-trip", n)
	}
	var status string
	if err := pool.QueryRow(ctx,
		`SELECT status FROM subscription_charges WHERE chip_purchase_id=$1`, "pur-fresh-"+sub.ID[:8]).Scan(&status); err != nil {
		t.Fatalf("read: %v", err)
	}
	if status != dbstore.ChargePending {
		t.Errorf("fresh charge status = %q, want %q", status, dbstore.ChargePending)
	}
}

// stubRejectChip fails the charge synchronously, so no real gateway is contacted.
// The point of these tests is the subscription's ability to be charged AT ALL,
// not the outcome of the attempt.
func stubRejectChip(t *testing.T) *chip.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /purchases/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"__all__":[{"message":"stub decline","code":"declined"}]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return chip.New(srv.URL)
}
