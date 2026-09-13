package payments

// Issue #534 end-to-end: the recurring engine must not charge a due subscription
// twice for one period while the first charge is still pending.
//
// This drives the real engine (chargeSubscription) twice against a stub CHIP
// server that COUNTS charge calls — each one a real money movement on the
// customer's card. Against the pre-fix code the second call charged again:
//
//	REAL MONEY BUG: the same due subscription was charged 2 times while its
//	first charge is still pending.
//
// The claim added in ChargeSubscription fixes it: the second call sees the
// subscription already leased (and a charge still pending) and does not charge.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/chip"
	"github.com/wzul/jomform/api/internal/cryptoutil"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// countingChip builds a CHIP client whose stub counts every /charge/ request.
func countingChip(t *testing.T, calls *int64) *chip.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /purchases/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(strings.TrimSuffix(r.URL.Path, "/"), "/charge") {
			atomic.AddInt64(calls, 1)
		}
		w.WriteHeader(http.StatusCreated)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "pur_534", "status": "pending_charge",
		})
	})
	mux.HandleFunc("POST /purchases/pur_534/charge/", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(calls, 1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "pur_534", "status": "pending_charge",
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return chip.New(srv.URL)
}

// issue534Fixture creates an account with usable CHIP creds, a subscription
// product, and a due active subscription.
func issue534Fixture(t *testing.T, ctx context.Context, p *pgxpool.Pool) (accountID, subID string) {
	t.Helper()
	var userID string
	if err := p.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('r534-' || md5(random()::text) || '@example.test', 'it') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := p.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 SELECT $1, 'r534-' || md5(random()::text), 'r534-' || md5(random()::text)
		 RETURNING id`, userID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	sealed, err := cryptoutil.Encrypt(key, []byte("sek_test_534"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if err := dbstore.NewAccountStore(p).SetChipCredentials(ctx, accountID, "br_534", sealed, "test", "", nil, true); err != nil {
		t.Fatalf("set chip creds: %v", err)
	}
	prod := &dbstore.Product{
		AccountID: accountID, Name: "Sub 534", PriceSen: 1500,
		Type: dbstore.SaleTypeSubscription, BillingPeriod: "month", Active: true,
	}
	if err := dbstore.NewProductStore(p).Create(ctx, prod); err != nil {
		t.Fatalf("create product: %v", err)
	}
	sub := &dbstore.Subscription{
		AccountID: accountID, ClientEmail: "buyer534@example.test", ProductID: prod.ID,
		ChipTokenPurchaseID: "tok-534", Status: dbstore.SubActive, Period: "month",
		AmountSen: 1500, NextChargeAt: time.Now().Add(-time.Minute),
	}
	if err := dbstore.NewSubscriptionStore(p).Create(ctx, sub); err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	return accountID, sub.ID
}

// TestIssue534EngineChargesOnceWhilePending is the end-to-end money guard.
func TestIssue534EngineChargesOnceWhilePending(t *testing.T) {
	ctx := context.Background()
	p := testdb.Open(t)
	accountID, subID := issue534Fixture(t, ctx, p)

	subs := dbstore.NewSubscriptionStore(p)
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	var calls int64
	svc := &Service{
		Chip:       countingChip(t, &calls),
		Accounts:   dbstore.NewAccountStore(p),
		Subs:       subs,
		Products:   dbstore.NewProductStore(p),
		DecryptKey: key,
		BaseURL:    "https://app.example",
	}

	// Tick 1: due, so a charge is created and left pending.
	sub, err := subs.ByIDForAccount(ctx, accountID, subID)
	if err != nil {
		t.Fatalf("load sub: %v", err)
	}
	if err := svc.chargeSubscription(ctx, sub); err != nil {
		t.Fatalf("tick 1: %v", err)
	}
	if n := atomic.LoadInt64(&calls); n != 1 {
		t.Fatalf("tick 1 issued %d charges, want 1", n)
	}

	// Tick 2 — the next hourly sweep, callback still outstanding.
	sub2, err := subs.ByIDForAccount(ctx, accountID, subID)
	if err != nil {
		t.Fatalf("reload sub: %v", err)
	}
	if err := svc.chargeSubscription(ctx, sub2); err != nil {
		t.Fatalf("tick 2: %v", err)
	}
	if total := atomic.LoadInt64(&calls); total != 1 {
		t.Fatalf("issue #534: the same due subscription was charged %d times while its "+
			"first charge is still pending — this is a REAL double charge on the customer's "+
			"card (money movement per call), want exactly 1", total)
	}

	// Sanity: exactly one charge row, still pending.
	var rows int
	if err := p.QueryRow(ctx,
		`SELECT count(*) FROM subscription_charges WHERE subscription_id=$1`, subID).Scan(&rows); err != nil {
		t.Fatalf("count charges: %v", err)
	}
	if rows != 1 {
		t.Fatalf("subscription_charges rows = %d, want 1", rows)
	}
	_ = fmt.Sprint()
}

// TestIssue534EngineProceedsWhenNoChargePending proves the lease does not wedge
// the engine: after a successful (paid) cycle advances the schedule, the next due
// period charges normally.
func TestIssue534EngineProceedsWhenNoChargePending(t *testing.T) {
	ctx := context.Background()
	p := testdb.Open(t)
	accountID, subID := issue534Fixture(t, ctx, p)

	subs := dbstore.NewSubscriptionStore(p)
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	var calls int64
	svc := &Service{
		Chip:       countingChip(t, &calls),
		Accounts:   dbstore.NewAccountStore(p),
		Subs:       subs,
		Products:   dbstore.NewProductStore(p),
		DecryptKey: key,
		BaseURL:    "https://app.example",
	}

	sub, err := subs.ByIDForAccount(ctx, accountID, subID)
	if err != nil {
		t.Fatalf("load sub: %v", err)
	}
	if err := svc.chargeSubscription(ctx, sub); err != nil {
		t.Fatalf("charge: %v", err)
	}

	// The callback lands: mark the charge paid and advance the schedule a full
	// period, exactly as processSubscriptionEvent does.
	if err := subs.MarkChargePaid(ctx, "pur_534"); err != nil {
		t.Fatalf("mark paid: %v", err)
	}
	if err := subs.ScheduleNextCharge(ctx, subID, time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("make next period due: %v", err)
	}

	// A new period is due with no pending charge — the engine must charge again.
	sub2, err := subs.ByIDForAccount(ctx, accountID, subID)
	if err != nil {
		t.Fatalf("reload sub: %v", err)
	}
	if err := svc.chargeSubscription(ctx, sub2); err != nil {
		t.Fatalf("next period charge: %v", err)
	}
	if got := atomic.LoadInt64(&calls); got != 2 {
		t.Fatalf("next period issued %d total charges, want 2 — the lease must not "+
			"permanently block a legitimately due period", got)
	}
}
