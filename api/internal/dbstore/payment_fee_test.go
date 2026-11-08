package dbstore

// Processing-fee rate store + resolution (card t_135c3c72).
//
// These run against the shared test database through testdb.Open, which gives
// this process its OWN scratch schema dropped and re-created on every run — so
// migration 000082 is applied from zero here, and no per-card database is
// created.

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// feeFixture creates a workspace + account pair and returns both ids.
func feeFixture(t *testing.T, ctx context.Context, p *pgxpool.Pool) (workspaceID, accountID string) {
	t.Helper()
	var userID string
	if err := p.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-fee-' || md5(random()::text) || '@example.test', 'it') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := p.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-fee-' || md5(random()::text), 'it-fee-' || md5(random()::text)) RETURNING id`,
		userID).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := p.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 VALUES ($1, 'it-fee-acc-' || md5(random()::text), 'it-fee-' || md5(random()::text), $2)
		 RETURNING id`, userID, workspaceID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	return workspaceID, accountID
}

// TestFeeRatesRoundTripAndReplace: the stored set round-trips, and a Replace
// that omits a method REMOVES it (silently keeping a stale rate is how a method
// the merchant switched off keeps charging buyers).
func TestFeeRatesRoundTripAndReplace(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	ws, _ := feeFixture(t, ctx, p)
	st := NewPaymentFeeStore(p)

	if got, err := st.List(ctx, ws); err != nil || len(got) != 0 {
		t.Fatalf("a fresh workspace must have no rates: got %v, %v", got, err)
	}

	if err := st.Replace(ctx, ws, "owner@example.test", []PaymentFeeRate{
		{Method: "card", Enabled: true, PercentBP: 200},
		{Method: "fpx", Enabled: true, FixedSen: 100},
	}); err != nil {
		t.Fatalf("replace: %v", err)
	}
	got, err := st.List(ctx, ws)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("list returned %d rates, want 2: %+v", len(got), got)
	}
	// Ordered by the method list the dashboard renders (fpx before card).
	if got[0].Method != "fpx" || got[0].FixedSen != 100 || got[0].Enabled != true {
		t.Errorf("first rate = %+v, want fpx fixed 100 enabled", got[0])
	}
	if got[1].Method != "card" || got[1].PercentBP != 200 {
		t.Errorf("second rate = %+v, want card 200bp", got[1])
	}
	if got[1].UpdatedBy != "owner@example.test" {
		t.Errorf("updated_by = %q, want the caller", got[1].UpdatedBy)
	}

	// Replace with only card: fpx must be GONE, not merely disabled.
	if err := st.Replace(ctx, ws, "owner@example.test", []PaymentFeeRate{
		{Method: "card", Enabled: true, PercentBP: 200},
	}); err != nil {
		t.Fatalf("replace 2: %v", err)
	}
	got, _ = st.List(ctx, ws)
	if len(got) != 1 || got[0].Method != "card" {
		t.Fatalf("after replace with only card, list = %+v, want just card", got)
	}
}

// TestFeeRateValidation: the same bounds the schema CHECK enforces are refused
// at the edge with a sentence, before a constraint violation can surface as 500.
func TestFeeRateValidation(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	ws, _ := feeFixture(t, ctx, p)
	st := NewPaymentFeeStore(p)

	bad := []struct {
		name string
		rate PaymentFeeRate
	}{
		{"100% has no gross-up solution", PaymentFeeRate{Method: "card", Enabled: true, PercentBP: 10000}},
		{"negative percentage", PaymentFeeRate{Method: "card", Enabled: true, PercentBP: -1}},
		{"negative fixed", PaymentFeeRate{Method: "card", Enabled: true, FixedSen: -1}},
		{"unknown method", PaymentFeeRate{Method: "visa", Enabled: true, PercentBP: 100}},
	}
	for _, c := range bad {
		err := st.Replace(ctx, ws, "owner@example.test", []PaymentFeeRate{c.rate})
		if err == nil {
			t.Errorf("%s: Replace accepted %+v, want a refusal", c.name, c.rate)
		}
	}
	if got, _ := st.List(ctx, ws); len(got) != 0 {
		t.Fatalf("a refused Replace left rows behind: %+v", got)
	}
}

// TestFeeForFormRequiresAcknowledgement: no fee may reach a buyer on a workspace
// where the merchant never accepted the pass-on disclosure — regardless of what
// the rate rows say.
func TestFeeForFormRequiresAcknowledgement(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	ws, acc := feeFixture(t, ctx, p)
	st := NewPaymentFeeStore(p)

	if err := st.Replace(ctx, ws, "owner@example.test", []PaymentFeeRate{
		{Method: "card", Enabled: true, PercentBP: 200},
	}); err != nil {
		t.Fatalf("replace: %v", err)
	}
	rates, err := st.ForForm(ctx, acc, "")
	if err != nil {
		t.Fatalf("ForForm: %v", err)
	}
	if len(rates) != 0 {
		t.Fatalf("fee active WITHOUT the merchant's acknowledgement: %+v", rates)
	}

	if err := st.Acknowledge(ctx, ws, "owner@example.test"); err != nil {
		t.Fatalf("acknowledge: %v", err)
	}
	ack, err := st.Acknowledgement(ctx, ws)
	if err != nil || ack == nil {
		t.Fatalf("acknowledgement not stored: %v %v", ack, err)
	}
	if ack.AcknowledgedBy != "owner@example.test" {
		t.Errorf("acknowledged_by = %q, want the merchant who switched it on", ack.AcknowledgedBy)
	}
	rates, _ = st.ForForm(ctx, acc, "")
	if len(rates) != 1 || rates[0].Method != "card" || rates[0].PercentBP != 200 {
		t.Fatalf("after acknowledgement ForForm = %+v, want the enabled card rate", rates)
	}
}

// TestFeeForFormOverrideAndDisabled: a form can switch the fee off for itself,
// a disabled rate is not offered, and an enabled rate with no rate at all
// charges nothing so it is not offered either.
func TestFeeForFormOverrideAndDisabled(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	ws, acc := feeFixture(t, ctx, p)
	st := NewPaymentFeeStore(p)
	forms := NewFormStore(p)

	// A published form owned by this account.
	form := &Form{AccountID: acc, Title: "Fee form", Slug: "fee-form-" + randSuffix(), Status: FormStatusDraft}
	if err := forms.Create(ctx, form); err != nil {
		t.Fatalf("create form: %v", err)
	}

	if err := st.Acknowledge(ctx, ws, "owner@example.test"); err != nil {
		t.Fatalf("acknowledge: %v", err)
	}
	if err := st.Replace(ctx, ws, "owner@example.test", []PaymentFeeRate{
		{Method: "card", Enabled: true, PercentBP: 200},
		{Method: "fpx", Enabled: false, FixedSen: 100}, // switched off
		{Method: "ewallet", Enabled: true},             // enabled, no rate
	}); err != nil {
		t.Fatalf("replace: %v", err)
	}

	rates, err := st.ForForm(ctx, acc, form.ID)
	if err != nil {
		t.Fatalf("ForForm: %v", err)
	}
	if len(rates) != 1 || rates[0].Method != "card" {
		t.Fatalf("ForForm = %+v, want only the enabled, priced card rate", rates)
	}

	// The form overrides the workspace: switch it off for this form only.
	if err := forms.SetFeeOverride(ctx, acc, form.ID, FeeOverrideOff); err != nil {
		t.Fatalf("set override: %v", err)
	}
	ov, err := forms.FeeOverrideFor(ctx, acc, form.ID)
	if err != nil || ov != FeeOverrideOff {
		t.Fatalf("FeeOverrideFor = %q, %v; want %q", ov, err, FeeOverrideOff)
	}
	rates, _ = st.ForForm(ctx, acc, form.ID)
	if len(rates) != 0 {
		t.Fatalf("a form override of 'off' still charged: %+v", rates)
	}
	// A DIFFERENT form (or a formless order) still inherits the workspace.
	if rates, _ := st.ForForm(ctx, acc, ""); len(rates) != 1 {
		t.Fatalf("the form override leaked to other checkouts: %+v", rates)
	}

	// Clearing the override restores inheritance.
	if err := forms.SetFeeOverride(ctx, acc, form.ID, ""); err != nil {
		t.Fatalf("clear override: %v", err)
	}
	if rates, _ := st.ForForm(ctx, acc, form.ID); len(rates) != 1 {
		t.Fatalf("cleared override did not restore the workspace rate: %+v", rates)
	}

	// An override value that is not one of the two legal values is refused.
	if err := forms.SetFeeOverride(ctx, acc, form.ID, "maybe"); err == nil {
		t.Fatal("SetFeeOverride accepted 'maybe'")
	}
}

// TestSaleFeeSnapshotSurvivesRateChange is the card's snapshot requirement: a
// rate change after a sale exists must NOT rewrite the sale. The sale stores its
// own fee and the rate in force at purchase time.
func TestSaleFeeSnapshotSurvivesRateChange(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	ws, acc := feeFixture(t, ctx, p)
	fees := NewPaymentFeeStore(p)
	sales := NewSaleStore(p)

	if err := fees.Acknowledge(ctx, ws, "owner@example.test"); err != nil {
		t.Fatalf("acknowledge: %v", err)
	}
	if err := fees.Replace(ctx, ws, "owner@example.test", []PaymentFeeRate{
		{Method: "card", Enabled: true, PercentBP: 100},
	}); err != nil {
		t.Fatalf("replace: %v", err)
	}

	// A sale priced at 1%: asking 100000, buyer paid 101010, fee 1010.
	sale := &Sale{
		AccountID: acc, RefCode: "JF-FEE-" + randSuffix(),
		Client:      SaleClient{FullName: "Ali", Email: "ali@example.test"},
		Items:       []SaleItem{{Name: "Tee", PriceSen: 100000, Qty: 1}},
		SubtotalSen: 100000, TotalSen: 101010,
		FeeSen: 1010, FeeMethod: "card", FeeFixedSen: 0, FeePercentBp: 100,
		SaleType: SaleTypeOneTime, PaymentMethod: "chip", Status: SalePending,
	}
	if err := sales.Create(ctx, sale); err != nil {
		t.Fatalf("create sale: %v", err)
	}

	// The merchant now doubles their MDR to 2%.
	if err := fees.Replace(ctx, ws, "owner@example.test", []PaymentFeeRate{
		{Method: "card", Enabled: true, PercentBP: 200},
	}); err != nil {
		t.Fatalf("replace 2: %v", err)
	}

	got, err := sales.ByRefCode(ctx, sale.RefCode)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.FeeSen != 1010 || got.TotalSen != 101010 {
		t.Fatalf("rate change rewrote history: fee=%d total=%d, want fee=1010 total=101010",
			got.FeeSen, got.TotalSen)
	}
	if got.FeeMethod != "card" || got.FeePercentBp != 100 || got.FeeFixedSen != 0 {
		t.Fatalf("frozen rate lost: method=%q bp=%d fixed=%d, want card/100/0",
			got.FeeMethod, got.FeePercentBp, got.FeeFixedSen)
	}
	// The money invariant on the frozen snapshot: total = subtotal + fee.
	if got.TotalSen != got.SubtotalSen+got.ShippingSen-got.DiscountSen+got.FeeSen {
		t.Fatalf("total %d is not subtotal+shipping-discount+fee (%d+%d-%d+%d)",
			got.TotalSen, got.SubtotalSen, got.ShippingSen, got.DiscountSen, got.FeeSen)
	}
}

// TestSaleFeeSnapshotIsolation: another workspace's rates never price this
// workspace's checkout — the resolution is keyed on the account's workspace.
func TestSaleFeeSnapshotIsolation(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	wsA, accA := feeFixture(t, ctx, p)
	wsB, accB := feeFixture(t, ctx, p)
	st := NewPaymentFeeStore(p)

	for _, ws := range []string{wsA, wsB} {
		if err := st.Acknowledge(ctx, ws, "owner@example.test"); err != nil {
			t.Fatalf("acknowledge %s: %v", ws, err)
		}
	}
	if err := st.Replace(ctx, wsA, "a@example.test", []PaymentFeeRate{{Method: "card", Enabled: true, PercentBP: 300}}); err != nil {
		t.Fatalf("replace A: %v", err)
	}

	ratesB, err := st.ForForm(ctx, accB, "")
	if err != nil {
		t.Fatalf("ForForm B: %v", err)
	}
	if len(ratesB) != 0 {
		t.Fatalf("workspace A's rate priced workspace B's checkout: %+v", ratesB)
	}
	ratesA, _ := st.ForForm(ctx, accA, "")
	if len(ratesA) != 1 || ratesA[0].PercentBP != 300 {
		t.Fatalf("workspace A's own rate not resolved: %+v", ratesA)
	}
}
