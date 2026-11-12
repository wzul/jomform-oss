package dbstore

// Platform-fee resolution and snapshot (card t_1a17d22d).
//
// The charge moved from a percentage GROSS-UP (the acquirer's rate passed on) to
// a flat PLATFORM FEE added to the CHIP purchase as its own line. Two models now
// share one settings row, and the resolution below has to tell them apart
// without changing what an existing workspace is charged:
//
//   - platform_fee_sen > 0                → platform model, flat amount
//   - percent_bp = 0, platform = 0, fixed > 0
//                                        → the same arithmetic as a flat platform
//                                          fee of `fixed`, so it is read as one
//   - percent_bp > 0                     → LEGACY gross-up, kept for one release
//
// Runs on the shared test database through testdb.Open (a per-process scratch
// schema), so migration 000090 is applied from zero here.

import (
	"context"
	"testing"
)

// TestForFormResolvesTheTwoModels: each stored row is read as the model its
// fields describe, and the legacy flag is set so the route prices it the old way.
func TestForFormResolvesTheTwoModels(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	ws, acc := feeFixture(t, ctx, p)
	st := NewPaymentFeeStore(p)

	if err := st.Acknowledge(ctx, ws, "owner@example.test"); err != nil {
		t.Fatalf("acknowledge: %v", err)
	}
	if err := st.Replace(ctx, ws, "owner@example.test", []PaymentFeeRate{
		{Method: "fpx", Enabled: true, PlatformFeeSen: 100},
		{Method: "ewallet", Enabled: true, FixedSen: 50},              // fixed-only row
		{Method: "card", Enabled: true, PercentBP: 100, FixedSen: 20}, // legacy
	}); err != nil {
		t.Fatalf("replace: %v", err)
	}

	rates, err := st.ForForm(ctx, acc, "")
	if err != nil {
		t.Fatalf("ForForm: %v", err)
	}
	byMethod := map[string]EnabledRate{}
	for _, r := range rates {
		byMethod[r.Method] = r
	}
	if len(rates) != 3 {
		t.Fatalf("ForForm = %+v, want three resolved methods", rates)
	}

	fpx, ok := byMethod["fpx"]
	if !ok || fpx.PlatformFeeSen != 100 || fpx.LegacyGrossUp {
		t.Errorf("fpx = %+v, want a 100-sen platform fee on the B2C identifier", fpx)
	}
	// A FIXED-only row is the legacy model's degenerate case: grossing up a
	// constant fee leaves exactly A + fixed, which is a flat platform fee. It is
	// read as one so the buyer's price stops moving.
	wallet, ok := byMethod["ewallet"]
	if !ok || wallet.PlatformFeeSen != 50 || wallet.LegacyGrossUp {
		t.Errorf("ewallet (fixed-only) = %+v, want a 50-sen platform fee, not a legacy rate", wallet)
	}
	card, ok := byMethod["card"]
	if !ok || !card.LegacyGrossUp || card.PlatformFeeSen != 0 || card.PercentBP != 100 {
		t.Errorf("card (percentage) = %+v, want the LEGACY gross-up, unchanged (migration 000090)", card)
	}
}

// TestForFormKeepsTheTwoFPXIdentifiersAsSeparateRows: `fpx` (B2C) and `fpx_b2b1`
// (B2B1) are two CHIP products and two buyer choices (#1710, card t_1d0198e3), so
// the resolution keeps two rows with their own amounts — equal or not. Collapsing
// them into one "FPX" row would price the buyer whichever variant happened to
// win, which is the regression card t_1a17d22d exists to prevent.
func TestForFormKeepsTheTwoFPXIdentifiersAsSeparateRows(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	ws, acc := feeFixture(t, ctx, p)
	st := NewPaymentFeeStore(p)

	if err := st.Acknowledge(ctx, ws, "owner@example.test"); err != nil {
		t.Fatalf("acknowledge: %v", err)
	}
	// Equal amounts still resolve to TWO rows: the two products are two choices
	// regardless of what they cost.
	if err := st.Replace(ctx, ws, "owner@example.test", []PaymentFeeRate{
		{Method: "fpx", Enabled: true, PlatformFeeSen: 100},
		{Method: "fpx_b2b1", Enabled: true, PlatformFeeSen: 100},
	}); err != nil {
		t.Fatalf("replace: %v", err)
	}
	rates, err := st.ForForm(ctx, acc, "")
	if err != nil {
		t.Fatalf("ForForm: %v", err)
	}
	got := map[string]EnabledRate{}
	for _, r := range rates {
		got[r.Method] = r
	}
	if len(rates) != 2 {
		t.Fatalf("equal FPX amounts resolved to %+v, want TWO rows (B2C and B2B1)", rates)
	}
	if r := got["fpx"]; r.PlatformFeeSen != 100 {
		t.Errorf("fpx row = %+v, want 100 sen on the B2C identifier", r)
	}
	if r := got["fpx_b2b1"]; r.PlatformFeeSen != 100 {
		t.Errorf("fpx_b2b1 row = %+v, want its own 100 sen on the B2B1 identifier", r)
	}

	// Different amounts keep both rows too — each keyed by its own identifier.
	if err := st.Replace(ctx, ws, "owner@example.test", []PaymentFeeRate{
		{Method: "fpx", Enabled: true, PlatformFeeSen: 100},
		{Method: "fpx_b2b1", Enabled: true, PlatformFeeSen: 200},
	}); err != nil {
		t.Fatalf("replace 2: %v", err)
	}
	rates, err = st.ForForm(ctx, acc, "")
	if err != nil {
		t.Fatalf("ForForm 2: %v", err)
	}
	got = map[string]EnabledRate{}
	for _, r := range rates {
		got[r.Method] = r
	}
	if len(rates) != 2 {
		t.Fatalf("different FPX amounts resolved to %+v, want two rows (B2C and B2B1)", rates)
	}
	if r := got["fpx"]; r.PlatformFeeSen != 100 {
		t.Errorf("fpx row = %+v, want 100 sen", r)
	}
	if r := got["fpx_b2b1"]; r.PlatformFeeSen != 200 {
		t.Errorf("fpx_b2b1 row = %+v, want its own 200 sen, not the B2C 100", r)
	}
}

// TestPlatformFeeRateValidation: the new amount is validated like the legacy
// one, a method may carry one model at a time, and fpx_b2b1 is a legal key.
func TestPlatformFeeRateValidation(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	ws, _ := feeFixture(t, ctx, p)
	st := NewPaymentFeeStore(p)

	if err := st.Replace(ctx, ws, "owner@example.test", []PaymentFeeRate{
		{Method: "fpx_b2b1", Enabled: true, PlatformFeeSen: 200},
	}); err != nil {
		t.Fatalf("fpx_b2b1 with a platform amount was refused: %v — B2C and B2B1 must be able to differ", err)
	}

	both := []PaymentFeeRate{{Method: "card", Enabled: true, PlatformFeeSen: 100, PercentBP: 100}}
	if err := st.Replace(ctx, ws, "owner@example.test", both); err == nil {
		t.Fatalf("a row with BOTH a platform amount and a legacy percentage was accepted: %+v", both)
	}

	negative := []PaymentFeeRate{{Method: "card", Enabled: true, PlatformFeeSen: -1}}
	if err := st.Replace(ctx, ws, "owner@example.test", negative); err == nil {
		t.Fatalf("a negative platform amount was accepted: %+v", negative)
	}
}

// TestSalePlatformFeeSnapshotRoundTrips: the new snapshot columns survive the
// insert/select pair, which is what makes the CSV/report and the receipt able to
// show the platform fee as its own amount after the fact.
func TestSalePlatformFeeSnapshotRoundTrips(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	_, acc := feeFixture(t, ctx, p)
	sales := NewSaleStore(p)

	sale := &Sale{
		AccountID: acc, RefCode: "JF-PLAT-" + randSuffix(),
		Client:      SaleClient{FullName: "Ali", Email: "ali@example.test"},
		Items:       []SaleItem{{Name: "Derma", PriceSen: 100000, Qty: 1}},
		SubtotalSen: 100000, TotalSen: 100100,
		FeeSen: 100, FeeMethod: "fpx", PlatformFeeSen: 100,
		FeeIdentifier: "fpx", FeeModel: FeeModelPlatform,
		SaleType: SaleTypeOneTime, PaymentMethod: "chip", Status: SalePending,
	}
	if err := sales.Create(ctx, sale); err != nil {
		t.Fatalf("create sale: %v", err)
	}
	got, err := sales.ByRefCode(ctx, sale.RefCode)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.PlatformFeeSen != 100 || got.FeeModel != FeeModelPlatform || got.FeeIdentifier != "fpx" {
		t.Errorf("snapshot = {platform:%d model:%q identifier:%q}, want {100 platform fpx}",
			got.PlatformFeeSen, got.FeeModel, got.FeeIdentifier)
	}
	// The money invariant this card promises: total = item + platform fee.
	if got.TotalSen != got.SubtotalSen+got.PlatformFeeSen {
		t.Errorf("total %d != subtotal %d + platform fee %d", got.TotalSen, got.SubtotalSen, got.PlatformFeeSen)
	}
	// A sale with no fee writes the zero values, so the CSV can tell them apart.
	if got.FeePercentBp != 0 || got.FeeFixedSen != 0 {
		t.Errorf("a platform sale carried legacy rate fields: bp=%d fixed=%d", got.FeePercentBp, got.FeeFixedSen)
	}
}
