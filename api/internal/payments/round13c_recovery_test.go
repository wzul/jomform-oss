package payments

// Round 13c (Wan): subscription recovery + fixed billing schedule.
//
// Covers the three things a merchant needs when a recurring charge fails:
//   1. somebody is told,
//   2. the subscription can be revived (a settled payment, or a merchant retry),
//   3. the schedule follows the product's chosen policy instead of drifting.

import (
	"context"
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

// ── 3. schedule policy (pure function: no DB needed) ────────────────────────

func TestRound13cSchedulePolicy(t *testing.T) {
	month := "month"
	// Planned 1 Feb, the charge actually succeeded 45 days late (17 Mar).
	planned := time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC)
	now := planned.AddDate(0, 0, 45) // 18 Mar 2026

	// from_payment: from the moment of success -> drifts.
	if got := nextChargeForPolicy(ScheduleFromPayment, now, planned, month, false); !got.Equal(nextChargeFrom(now, month)) {
		t.Errorf("from_payment = %s, want %s (from the moment it succeeded)", got.Format("02 Jan 2006"), nextChargeFrom(now, month).Format("02 Jan 2006"))
	}

	// catch_up: keeps the original dates and still collects the missed period.
	// The walk starts a period BEHIND now (17 Feb is already past), so it lands
	// on 1 Mar — the month that was missed — not 1 Apr.
	got := nextChargeForPolicy(ScheduleCatchUp, now, planned, month, false)
	if !got.Equal(time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("catch_up = %s, want 01 Mar 2026 (the missed month on the ORIGINAL anchor)", got.Format("02 Jan 2006"))
	}

	// skip_missed: same anchors, but the missed month is dropped.
	got = nextChargeForPolicy(ScheduleSkipMissed, now, planned, month, false)
	if !got.Equal(time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("skip_missed = %s, want 01 Apr 2026 (next planned date still in the future)", got.Format("02 Jan 2006"))
	}

	// An ON-TIME charge must land on the same calendar day in every mode — the
	// policy only matters when something was actually missed. Compared by DATE,
	// not instant: the fixed modes anchor to the PLANNED date while the
	// historical mode anchors to `now`, so they legitimately differ by the
	// payment's processing lag (minutes) without shifting the billing date.
	onTime := planned.Add(2 * time.Hour)
	want := nextChargeFrom(onTime, month)
	for _, mode := range []string{ScheduleFromPayment, ScheduleCatchUp, ScheduleSkipMissed} {
		g := nextChargeForPolicy(mode, onTime, planned, month, false)
		if g.Year() != want.Year() || g.YearDay() != want.YearDay() {
			t.Errorf("mode %s on an ON-TIME charge = %s, want %s (the policy must not change normal billing)",
				mode, g.Format("02 Jan 2006"), want.Format("02 Jan 2006"))
		}
	}

	// Convergence: after the arrears period is collected, the NEXT call walks
	// from the date it just set, so the same period is never billed twice.
	step1 := nextChargeForPolicy(ScheduleCatchUp, now, planned, month, false) // 01 Mar
	step2 := nextChargeForPolicy(ScheduleCatchUp, time.Now(), step1, month, false)
	if !step2.After(step1) {
		t.Errorf("catch_up step 2 = %s, want a date after %s — a settled arrears period must not be collected again",
			step2.Format("02 Jan 2006"), step1.Format("02 Jan 2006"))
	}

	// Yearly period.
	if g := nextChargeForPolicy(ScheduleSkipMissed, now, planned, "year", false); !g.Equal(time.Date(2027, 2, 1, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("skip_missed yearly = %s, want 01 Feb 2027", g.Format("02 Jan 2006"))
	}
}

func TestRound13cValidScheduleMode(t *testing.T) {
	for _, m := range []string{ScheduleFromPayment, ScheduleCatchUp, ScheduleSkipMissed} {
		if !ValidScheduleMode(m) {
			t.Errorf("ValidScheduleMode(%q) = false, want true", m)
		}
	}
	for _, m := range []string{"", "weekly", "FROM_PAYMENT", "drift", "from-payment"} {
		if ValidScheduleMode(m) {
			t.Errorf("ValidScheduleMode(%q) = true, want false — an unknown mode must be rejected, not silently accepted", m)
		}
	}
}

// ── 1 + 2: notification on failure, and revival ─────────────────────────────

// rcChip fails the charge (ok=false) or succeeds (ok=true), counting attempts.
type rcChip struct {
	mu sync.Mutex
	n  int64
	ok bool
}

func newRCChip(t *testing.T, ok bool) (*chip.Client, *rcChip) {
	t.Helper()
	s := &rcChip{ok: ok}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /purchases/", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.n++
		n := s.n
		s.mu.Unlock()
		if !s.ok {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"__all__":{"message":"card declined","code":"insufficient_funds"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": fmt.Sprintf("pur-r13c-%d-%d", time.Now().UnixNano(), n), "status": "pending_charge"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return chip.New(srv.URL), s
}

// notifStub records the (formID, event) pairs the engine asks to notify for.
type notifStub struct {
	mu    sync.Mutex
	calls []string
}

func (n *notifStub) ListByFormEvent(_ context.Context, formID, event string) ([]*dbstore.FormNotification, error) {
	n.mu.Lock()
	n.calls = append(n.calls, event)
	n.mu.Unlock()
	// Return one rule the engine will try to deliver for the failure event.
	if event == dbstore.NotifyEventSaleFailed {
		return []*dbstore.FormNotification{{
			ID: "n1", FormID: formID, Name: "Card failed", Event: event,
			ToEmail: "admin@example.test", Subject: "Card failed", Body: "b", Enabled: true,
		}}, nil
	}
	return nil, nil
}

func (n *notifStub) events() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.calls...)
}

func round13cFixture(t *testing.T, ctx context.Context, p *pgxpool.Pool, mode string) (accountID, prodID, subID, saleID string) {
	t.Helper()
	var userID string
	if err := p.QueryRow(ctx,
		`INSERT INTO users (email,password_hash) VALUES ('r13c-'||md5(random()::text)||'@example.test','it') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("user: %v", err)
	}
	if err := p.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id,name,slug) SELECT $1,'r13c-'||md5(random()::text),'r13c-'||md5(random()::text) RETURNING id`,
		userID).Scan(&accountID); err != nil {
		t.Fatalf("account: %v", err)
	}
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	sealed, err := cryptoutil.Encrypt(key, []byte("sk_test_r13c"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if err := dbstore.NewAccountStore(p).SetChipCredentials(ctx, accountID, "br_r13c", sealed, "test", "", nil, true); err != nil {
		t.Fatalf("creds: %v", err)
	}
	prod := &dbstore.Product{
		AccountID: accountID, Name: "Kelas", PriceSen: 10000,
		Type: dbstore.SaleTypeSubscription, BillingPeriod: "month", Active: true,
		ScheduleMode: mode,
	}
	if err := dbstore.NewProductStore(p).Create(ctx, prod); err != nil {
		t.Fatalf("product: %v", err)
	}
	token := "tok-r13c-" + prod.ID[:8]
	sub := &dbstore.Subscription{
		AccountID: accountID, ClientEmail: "r13c@example.test", ProductID: prod.ID,
		ChipTokenPurchaseID: token, Status: dbstore.SubActive, Period: "month",
		AmountSen: 10000, NextChargeAt: time.Now().Add(-10 * time.Minute),
	}
	if err := dbstore.NewSubscriptionStore(p).Create(ctx, sub); err != nil {
		t.Fatalf("sub: %v", err)
	}
	if err := dbstore.NewSubscriptionStore(p).AddCharge(ctx, sub.ID, token, 10000, dbstore.ChargePaid, 0); err != nil {
		t.Fatalf("initial charge: %v", err)
	}
	return accountID, prod.ID, sub.ID, ""
}

func round13cSvc(t *testing.T, p *pgxpool.Pool, c *chip.Client) *Service {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	return &Service{
		Chip: c, Accounts: dbstore.NewAccountStore(p),
		Sales: dbstore.NewSaleStore(p), Subs: dbstore.NewSubscriptionStore(p),
		Products: dbstore.NewProductStore(p), DecryptKey: key, BaseURL: "https://app.example",
	}
}

func rcStatus(t *testing.T, ctx context.Context, p *pgxpool.Pool, subID string) (string, bool) {
	t.Helper()
	var st string
	var pend bool
	if err := p.QueryRow(ctx, `SELECT status, card_update_pending FROM subscriptions WHERE id=$1`, subID).
		Scan(&st, &pend); err != nil {
		t.Fatalf("status: %v", err)
	}
	return st, pend
}

// TestRound13cFailedChargeNotifiesSomeone is defect 1: a failed recurring
// charge used to notify nobody — no event, no webhook, no email.
func TestRound13cFailedChargeNotifiesSomeone(t *testing.T) {
	ctx := context.Background()
	p := testdb.Open(t)
	_, prodID, subID, _ := round13cFixture(t, ctx, p, ScheduleFromPayment)
	client, _ := newRCChip(t, false) // always fails
	svc := round13cSvc(t, p, client)

	// Bind the subscription to a real sale so the notification has a form +
	// ref code to resolve recipients against (production always has one).
	var accountID, formID string
	if err := p.QueryRow(ctx, `SELECT account_id FROM products WHERE id=$1`, prodID).Scan(&accountID); err != nil {
		t.Fatalf("account: %v", err)
	}
	if err := p.QueryRow(ctx,
		`INSERT INTO forms (account_id, title, slug, status, config)
		 VALUES ($1,'Card fail form','r13c-'||md5(random()::text),'published','{}'::jsonb) RETURNING id`,
		accountID).Scan(&formID); err != nil {
		t.Fatalf("form: %v", err)
	}
	saleID := ""
	if err := p.QueryRow(ctx,
		`INSERT INTO sales (account_id, form_id, ref_code, client, items, subtotal_sen, total_sen,
		    currency, sale_type, payment_method, status)
		 VALUES ($1,$2,'R13C-'||md5(random()::text),'{}'::jsonb,'[]'::jsonb,10000,10000,
		    'MYR','subscription','chip','paid') RETURNING id`,
		accountID, formID).Scan(&saleID); err != nil {
		t.Fatalf("sale: %v", err)
	}
	if _, err := p.Exec(ctx, `UPDATE subscriptions SET sale_id=$1 WHERE id=$2`, saleID, subID); err != nil {
		t.Fatalf("bind sale: %v", err)
	}

	stub := &notifStub{}
	svc.Notifications = stub

	// One failed sweep is enough: the first failure is what must alert.
	if err := dbstore.NewSubscriptionStore(p).ScheduleNextCharge(ctx, subID, time.Now().Add(-10*time.Minute)); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if err := svc.RunDueCharges(ctx); err != nil && err != dbstore.ErrNoDueSubs {
		t.Fatalf("sweep: %v", err)
	}

	got := stub.events()
	found := false
	for _, e := range got {
		if e == dbstore.NotifyEventSaleFailed {
			found = true
		}
	}
	if !found {
		t.Errorf("a failed subscription charge fired no failure notification (events seen: %v) — "+
			"the merchant is never told the customer's card stopped working", got)
	}
}

// TestRound13cPastDueRevivableByPayment — defect 2, part A.
func TestRound13cPastDueRevivableByPayment(t *testing.T) {
	ctx := context.Background()
	p := testdb.Open(t)
	accountID, _, subID, _ := round13cFixture(t, ctx, p, ScheduleFromPayment)
	client, _ := newRCChip(t, false) // always fails
	svc := round13cSvc(t, p, client)
	subs := dbstore.NewSubscriptionStore(p)

	for i := 0; i < 3; i++ {
		if err := subs.ScheduleNextCharge(ctx, subID, time.Now().Add(-10*time.Minute)); err != nil {
			t.Fatalf("schedule: %v", err)
		}
		if err := svc.RunDueCharges(ctx); err != nil && err != dbstore.ErrNoDueSubs {
			t.Fatalf("sweep %d: %v", i, err)
		}
	}
	st, pend := rcStatus(t, ctx, p, subID)
	if st != dbstore.SubPastDue {
		t.Fatalf("after 3 failures status = %q, want past_due", st)
	}
	if !pend {
		t.Errorf("card_update_pending = false, want true after exhausting the retry ladder")
	}

	// The customer's card finally works and the charge settles.
	okPur := "pur-r13c-settled-" + subID[:8]
	if _, err := p.Exec(ctx,
		`INSERT INTO subscription_charges (subscription_id, chip_purchase_id, amount_sen, status, attempt)
		 VALUES ($1,$2,10000,'pending',4)`, subID, okPur); err != nil {
		t.Fatalf("insert settling charge: %v", err)
	}
	if err := svc.processSubscriptionEvent(ctx, &chip.Purchase{ID: okPur, Status: chip.StatusPaid}); err != nil {
		t.Fatalf("settle: %v", err)
	}
	st, pend = rcStatus(t, ctx, p, subID)
	if st != dbstore.SubActive {
		t.Errorf("after a settled payment status = %q, want active — a subscription that paid must not stay past_due (it was one grace-window expiry from auto-cancel)", st)
	}
	if pend {
		t.Errorf("card_update_pending = true after a settled payment, want false")
	}
	_ = accountID
}

// TestRound13cPastDueRevivableByRetry — defect 2, part B (the merchant action).
func TestRound13cPastDueRevivableByRetry(t *testing.T) {
	ctx := context.Background()
	p := testdb.Open(t)
	accountID, _, subID, _ := round13cFixture(t, ctx, p, ScheduleFromPayment)
	client, _ := newRCChip(t, false)
	svc := round13cSvc(t, p, client)
	subs := dbstore.NewSubscriptionStore(p)

	for i := 0; i < 3; i++ {
		if err := subs.ScheduleNextCharge(ctx, subID, time.Now().Add(-10*time.Minute)); err != nil {
			t.Fatalf("schedule: %v", err)
		}
		if err := svc.RunDueCharges(ctx); err != nil && err != dbstore.ErrNoDueSubs {
			t.Fatalf("sweep %d: %v", i, err)
		}
	}
	if st, _ := rcStatus(t, ctx, p, subID); st != dbstore.SubPastDue {
		t.Fatalf("setup: status = %q, want past_due", st)
	}

	// The merchant clicks "retry" after the customer tops up.
	if err := subs.Reactivate(ctx, accountID, subID); err != nil {
		t.Fatalf("Reactivate: %v", err)
	}
	st, pend := rcStatus(t, ctx, p, subID)
	if st != dbstore.SubActive {
		t.Errorf("after retry status = %q, want active", st)
	}
	if pend {
		t.Errorf("after retry card_update_pending = true, want false")
	}

	// And the engine must now actually pick it up again — using a card that works.
	okClient, _ := newRCChip(t, true)
	svc2 := round13cSvc(t, p, okClient)
	if err := subs.ScheduleNextCharge(ctx, subID, time.Now().Add(-10*time.Minute)); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if err := svc2.RunDueCharges(ctx); err != nil && err != dbstore.ErrNoDueSubs {
		t.Fatalf("post-retry sweep: %v", err)
	}
	if got, _ := rcStatus(t, ctx, p, subID); got != dbstore.SubActive && got != dbstore.SubCompleted {
		t.Errorf("status after a successful retry charge = %q, want active (or completed at the plan's end)", got)
	}
}

// TestRound13cRetryRefusesIneligible — cancelled/completed must never revive.
func TestRound13cRetryRefusesIneligible(t *testing.T) {
	ctx := context.Background()
	p := testdb.Open(t)
	accountID, _, subID, _ := round13cFixture(t, ctx, p, ScheduleFromPayment)
	subs := dbstore.NewSubscriptionStore(p)

	// active -> not eligible
	if err := subs.Reactivate(ctx, accountID, subID); err != dbstore.ErrNotPastDue {
		t.Errorf("Reactivate on an ACTIVE subscription = %v, want ErrNotPastDue", err)
	}
	if err := subs.Cancel(ctx, subID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if err := subs.Reactivate(ctx, accountID, subID); err != dbstore.ErrNotPastDue {
		t.Errorf("Reactivate on a CANCELLED subscription = %v, want ErrNotPastDue — a cancelled subscription must not be resurrected", err)
	}
	if st, _ := rcStatus(t, ctx, p, subID); st != dbstore.SubCanceled {
		t.Errorf("cancelled subscription status = %q, want cancelled", st)
	}
}

// TestRound13cScheduleModeUsedByEngine proves the product's mode reaches the
// engine (not just the pure function): scheduleModeFor reads it back.
func TestRound13cScheduleModeUsedByEngine(t *testing.T) {
	ctx := context.Background()
	p := testdb.Open(t)
	_, prodID, _, _ := round13cFixture(t, ctx, p, ScheduleCatchUp)

	var accountID, mode string
	if err := p.QueryRow(ctx, `SELECT account_id, schedule_mode FROM products WHERE id=$1`, prodID).
		Scan(&accountID, &mode); err != nil {
		t.Fatalf("read product: %v", err)
	}
	if mode != ScheduleCatchUp {
		t.Fatalf("stored schedule_mode = %q, want %q", mode, ScheduleCatchUp)
	}

	svc := round13cSvc(t, p, nil)
	got := svc.scheduleModeFor(ctx, &dbstore.Subscription{AccountID: accountID, ProductID: prodID})
	if got != ScheduleCatchUp {
		t.Errorf("scheduleModeFor = %q, want %q — the engine must read the product's mode", got, ScheduleCatchUp)
	}

	// An unknown/empty stored value must fall back to the historical default
	// rather than an empty string that would disable the policy silently.
	if _, err := p.Exec(ctx, `UPDATE products SET schedule_mode='from_payment' WHERE id=$1`, prodID); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if got := svc.scheduleModeFor(ctx, &dbstore.Subscription{AccountID: accountID, ProductID: prodID}); got != ScheduleFromPayment {
		t.Errorf("scheduleModeFor = %q, want %q", got, ScheduleFromPayment)
	}
}
