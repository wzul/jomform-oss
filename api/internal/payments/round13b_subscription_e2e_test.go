package payments

// Round 13b: end-to-end proof of the recurring engine for the real product
// question — "3 months class, RM300, auto-deduct RM100 monthly".
//
// Everything runs through the REAL deployed path, no reimplementation:
//
//	payment #1  purchase.paid -> activateSubscription   (creates the subscription)
//	month 2..N  hourly sweep  -> RunDueCharges -> chargeSubscription -> chip.Charge
//	            purchase.paid -> processSubscriptionEvent -> MarkChargePaid
//	                          -> ScheduleNextCharge -> completeIfLimitReached
//
// CHIP is stubbed, so no card is ever touched.
//
// TEST ISOLATION: RunDueCharges is a PLATFORM-WIDE sweep (it loads every due
// subscription, which is correct for a worker). A shared test DB can contain
// other tests' due subscriptions, so every assertion counts only THIS
// subscription's own charge rows — never a global stub counter.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/chip"
	"github.com/wzul/jomform/api/internal/cryptoutil"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// subE2EChip is a CHIP stub issuing a UNIQUE purchase id per charge, so each
// callback maps to exactly one charge row.
type subE2EChip struct {
	mu     sync.Mutex
	n      int64
	issued []string
}

func newSubE2EChip(t *testing.T) (*chip.Client, *subE2EChip) {
	t.Helper()
	s := &subE2EChip{}
	// Purchase ids must be GLOBALLY unique, not per-stub: subscription_charges has
	// a UNIQUE constraint on chip_purchase_id and ByChargePurchase looks a row up
	// by it alone. A per-stub counter restarts at 1 for every test, so a second
	// test in the same run reuses ids and its callbacks resolve to the FIRST
	// test's rows — which corrupts both tests. (CHIP ids are unique in production.)
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("rand: %v", err)
	}
	prefix := hex.EncodeToString(b[:])
	mux := http.NewServeMux()
	mux.HandleFunc("POST /purchases/", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.n++
		id := fmt.Sprintf("pur_e2e_%s_%d", prefix, s.n)
		s.issued = append(s.issued, id)
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "status": "pending_charge"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return chip.New(srv.URL), s
}

// chargeRows returns this subscription's charge rows as (purchase_id, status).
func chargeRows(t *testing.T, ctx context.Context, p *pgxpool.Pool, subID string) [][2]string {
	t.Helper()
	rows, err := p.Query(ctx,
		`SELECT COALESCE(chip_purchase_id,''), status FROM subscription_charges
		  WHERE subscription_id = $1 ORDER BY charged_at, status`, subID)
	if err != nil {
		t.Fatalf("query charges: %v", err)
	}
	defer rows.Close()
	var out [][2]string
	for rows.Next() {
		var id, st string
		if err := rows.Scan(&id, &st); err != nil {
			t.Fatalf("scan charge: %v", err)
		}
		out = append(out, [2]string{id, st})
	}
	return out
}

// newSubE2EAccount creates a user + account with test-mode CHIP creds, and a
// subscription product priced RM100 with an optional fixed charge count.
func newSubE2EAccount(t *testing.T, ctx context.Context, p *pgxpool.Pool, charges *int) (accountID, prodID string) {
	t.Helper()
	var userID string
	if err := p.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('r13b-' || md5(random()::text) || '@example.test', 'it') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := p.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 SELECT $1, 'r13b-' || md5(random()::text), 'r13b-' || md5(random()::text)
		 RETURNING id`, userID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	sealed, err := cryptoutil.Encrypt(key, []byte("sk_test_round13b"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if err := dbstore.NewAccountStore(p).SetChipCredentials(ctx, accountID, "br_r13b", sealed, "test", "", nil, true); err != nil {
		t.Fatalf("set chip creds: %v", err)
	}

	prod := &dbstore.Product{
		AccountID: accountID, Name: "Kelas 3 Bulan", PriceSen: 10000, // RM100.00
		Type: dbstore.SaleTypeSubscription, BillingPeriod: "month", Active: true,
		SubscriptionCharges: charges,
	}
	if err := dbstore.NewProductStore(p).Create(ctx, prod); err != nil {
		t.Fatalf("create product: %v", err)
	}
	return accountID, prod.ID
}

func subE2EService(t *testing.T, p *pgxpool.Pool, client *chip.Client) *Service {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	return &Service{
		Chip:       client,
		Accounts:   dbstore.NewAccountStore(p),
		Sales:      dbstore.NewSaleStore(p),
		Subs:       dbstore.NewSubscriptionStore(p),
		Products:   dbstore.NewProductStore(p),
		DecryptKey: key,
		BaseURL:    "https://app.example",
	}
}

// firstPayment runs the REAL purchase.paid activation for a subscription sale:
// exactly what happens when the buyer checks out and the gateway calls back.
func firstPayment(t *testing.T, ctx context.Context, p *pgxpool.Pool, svc *Service,
	accountID, prodID, purchaseID, refCode string) string {
	t.Helper()
	sale := &dbstore.Sale{
		AccountID: accountID, RefCode: refCode,
		Client:      dbstore.SaleClient{Email: "buyer13b@example.test"},
		Items:       []dbstore.SaleItem{{ProductID: prodID, Name: "Kelas 3 Bulan", Qty: 1, PriceSen: 10000}},
		SubtotalSen: 10000, TotalSen: 10000, Currency: "MYR",
		SaleType: dbstore.SaleTypeSubscription, PaymentMethod: "chip",
		Status: dbstore.SalePaid, ChipPurchaseID: purchaseID,
	}
	if err := dbstore.NewSaleStore(p).Create(ctx, sale); err != nil {
		t.Fatalf("create sale: %v", err)
	}
	if err := svc.activateSubscription(ctx, sale); err != nil {
		t.Fatalf("activateSubscription: %v", err)
	}
	sub, err := dbstore.NewSubscriptionStore(p).ByTokenPurchase(ctx, purchaseID)
	if err != nil {
		t.Fatalf("subscription was not created by activateSubscription: %v", err)
	}
	return sub.ID
}

// deliverPaid replays the real purchase.paid callback for a charge purchase id.
func deliverPaid(t *testing.T, ctx context.Context, svc *Service, purchaseID string) {
	t.Helper()
	if err := svc.processSubscriptionEvent(ctx, &chip.Purchase{ID: purchaseID, Status: chip.StatusPaid}); err != nil {
		t.Fatalf("processSubscriptionEvent(%s): %v", purchaseID, err)
	}
}

func statusOf(t *testing.T, ctx context.Context, p *pgxpool.Pool, subID string) string {
	t.Helper()
	var st string
	if err := p.QueryRow(ctx, `SELECT status FROM subscriptions WHERE id=$1`, subID).Scan(&st); err != nil {
		t.Fatalf("status: %v", err)
	}
	return st
}

func paidCount(t *testing.T, ctx context.Context, subs *dbstore.SubscriptionStore, subID string) int64 {
	t.Helper()
	n, err := subs.ChargeCount(ctx, subID)
	if err != nil {
		t.Fatalf("charge count: %v", err)
	}
	return n
}

// runMonths drives `months` sweep cycles, delivering each charge's callback.
func runMonths(t *testing.T, ctx context.Context, p *pgxpool.Pool, svc *Service,
	subs *dbstore.SubscriptionStore, accountID, subID string, months int) (engineCharges int) {
	t.Helper()
	for cycle := 0; cycle < months; cycle++ {
		// A month passes: make the subscription due again.
		if err := subs.ScheduleNextCharge(ctx, subID, time.Now().Add(-10*time.Minute)); err != nil {
			t.Fatalf("schedule: %v", err)
		}
		before := chargeRows(t, ctx, p, subID)
		// THE REAL SWEEP the hourly ticker calls.
		if err := svc.RunDueCharges(ctx); err != nil && err != dbstore.ErrNoDueSubs {
			t.Fatalf("cycle %d RunDueCharges: %v", cycle, err)
		}
		after := chargeRows(t, ctx, p, subID)

		newOnes := after[len(before):]
		for _, row := range newOnes {
			engineCharges++
			deliverPaid(t, ctx, svc, row[0])
		}
		t.Logf("  month %d: charge_rows %d->%d (+%d) status=%s paid=%d",
			cycle+2, len(before), len(after), len(newOnes),
			statusOf(t, ctx, p, subID), paidCount(t, ctx, subs, subID))

		if statusOf(t, ctx, p, subID) == dbstore.SubCompleted {
			break
		}
	}
	return engineCharges
}

// TestRound13bFixedCountCollectsExactlyNThenStops is the proof for
// "RM300 for 3 months, auto-deduct monthly": 3 payments of RM100, then STOP.
func TestRound13bFixedCountCollectsExactlyNThenStops(t *testing.T) {
	const N = 3
	ctx := context.Background()
	p := testdb.Open(t)
	n := N
	accountID, prodID := newSubE2EAccount(t, ctx, p, &n)

	client, _ := newSubE2EChip(t)
	svc := subE2EService(t, p, client)
	subs := dbstore.NewSubscriptionStore(p)

	// Payment #1 — the buyer's checkout.
	subID := firstPayment(t, ctx, p, svc, accountID, prodID, "pur_e2e_init_"+prodID[:8], "R13B-"+prodID[:8])
	rows := chargeRows(t, ctx, p, subID)
	t.Logf("payment #1 (real activateSubscription): rows=%v paid=%d", rows, paidCount(t, ctx, subs, subID))
	if len(rows) != 1 || rows[0][1] != dbstore.ChargePaid {
		t.Errorf("after payment #1 the subscription must have exactly ONE PAID charge row, got %v — "+
			"a PENDING row with no purchase id is a phantom outstanding charge that blocks every future sweep", rows)
	}

	engineCharges := runMonths(t, ctx, p, svc, subs, accountID, subID, 8)

	t.Logf("RESULT: %d recurring charges + payment #1 = %d payments, cap N=%d",
		engineCharges, engineCharges+1, N)

	if engineCharges+1 != N {
		t.Errorf("payments collected = %d (1 initial + %d recurring), want exactly %d — a fixed %d-charge subscription must collect %d times and stop",
			engineCharges+1, engineCharges, N, N, N)
	}
	if got := statusOf(t, ctx, p, subID); got != dbstore.SubCompleted {
		t.Errorf("final status = %q, want %q", got, dbstore.SubCompleted)
	}
	if got := paidCount(t, ctx, subs, subID); got != N {
		t.Errorf("paid charge rows = %d, want %d", got, N)
	}

	// A completed subscription must never be picked up again.
	if err := subs.ScheduleNextCharge(ctx, subID, time.Now().Add(-2*time.Hour)); err != nil {
		t.Fatalf("re-schedule: %v", err)
	}
	before := chargeRows(t, ctx, p, subID)
	if err := svc.RunDueCharges(ctx); err != nil && err != dbstore.ErrNoDueSubs {
		t.Fatalf("final sweep: %v", err)
	}
	if after := chargeRows(t, ctx, p, subID); len(after) != len(before) {
		t.Errorf("a COMPLETED subscription was charged again: %d -> %d rows", len(before), len(after))
	}
}

// TestRound13bInfiniteSubscriptionKeepsCharging is the control: no fixed count
// (NULL = infinite) must NOT stop early.
func TestRound13bInfiniteSubscriptionKeepsCharging(t *testing.T) {
	ctx := context.Background()
	p := testdb.Open(t)
	accountID, prodID := newSubE2EAccount(t, ctx, p, nil) // nil = infinite

	client, _ := newSubE2EChip(t)
	svc := subE2EService(t, p, client)
	subs := dbstore.NewSubscriptionStore(p)

	subID := firstPayment(t, ctx, p, svc, accountID, prodID, "pur_e2e_inf_"+prodID[:8], "R13BI-"+prodID[:8])
	const months = 5
	engineCharges := runMonths(t, ctx, p, svc, subs, accountID, subID, months)

	if engineCharges != months {
		t.Errorf("an INFINITE subscription issued %d recurring charges over %d due months, want %d — it must keep charging",
			engineCharges, months, months)
	}
	if got := statusOf(t, ctx, p, subID); got != dbstore.SubActive {
		t.Errorf("infinite subscription status = %q, want %q", got, dbstore.SubActive)
	}
	if got := paidCount(t, ctx, subs, subID); got != months+1 {
		t.Errorf("paid charges = %d, want %d (initial + %d recurring)", got, months+1, months)
	}
}

// TestRound13bSinglePaymentSubscriptionCompletesImmediately covers N == 1: a
// product that bills exactly once must not schedule a second charge.
func TestRound13bSinglePaymentSubscriptionCompletesImmediately(t *testing.T) {
	ctx := context.Background()
	p := testdb.Open(t)
	one := 1
	accountID, prodID := newSubE2EAccount(t, ctx, p, &one)

	client, _ := newSubE2EChip(t)
	svc := subE2EService(t, p, client)
	subs := dbstore.NewSubscriptionStore(p)

	subID := firstPayment(t, ctx, p, svc, accountID, prodID, "pur_e2e_one_"+prodID[:8], "R13B1-"+prodID[:8])

	if got := statusOf(t, ctx, p, subID); got != dbstore.SubCompleted {
		t.Errorf("a 1-charge subscription status = %q, want %q — it is fully paid after the first payment",
			got, dbstore.SubCompleted)
	}
	engineCharges := runMonths(t, ctx, p, svc, subs, accountID, subID, 3)
	if engineCharges != 0 {
		t.Errorf("a 1-charge subscription issued %d extra recurring charges, want 0", engineCharges)
	}
}
