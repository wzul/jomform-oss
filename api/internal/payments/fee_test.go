package payments

// Bite-proof grid for the processing-fee gross-up (card t_135c3c72).
//
// The property every combination must satisfy is
//
//	0 ≤ (F − fee(F)) − A ≤ 1        (integer rounding only)
//
// where F is what the buyer pays and A is the asking price. The second test in
// this file feeds the SAME grid to the naive formula the WooCommerce plugin uses
// (`fee = total × rate`) and asserts it is RED — without that, a grid that
// cannot fail proves nothing about the formula it blesses.

import (
	"fmt"
	"testing"
)

// feeGridRates / feeGridFixed / feeGridAmounts are the combinations the card
// requires, exactly as written there. The amounts must include the
// pay-what-you-want values the card t_06851d88 grid asks for (100, 101, 999,
// 10000, 123456) — a buyer-typed amount is arbitrary, so the guarantee has to
// hold for every sen value, not only for round catalogue prices.
var (
	feeGridRates   = []int64{0, 50, 100, 140, 200, 300} // basis points
	feeGridFixed   = []int64{0, 100, 200}               // sen
	feeGridAmounts = []int64{1, 100, 101, 999, 1000, 10000, 123456, 100000000}
)

// grossUpFn prices one combination: it returns the total the buyer pays and the
// fee charged on that total.
type grossUpFn func(askingSen, fixedSen, percentBP int64) (totalSen, feeSen int64)

// gridViolations runs the full card grid through fn and returns every
// combination that breaks the guarantee, with its numbers, so a failure names
// the inputs rather than just the count.
func gridViolations(fn grossUpFn) []string {
	var bad []string
	for _, bp := range feeGridRates {
		for _, fixed := range feeGridFixed {
			prevTotal := int64(-1)
			for _, asking := range feeGridAmounts { // ascending: monotonicity below
				if v, why := invariantViolation(fn, asking, fixed, bp, prevTotal); why != "" {
					bad = append(bad, v+" — "+why)
				}
				if total, _ := fn(asking, fixed, bp); total >= 0 {
					prevTotal = total
				}
			}
		}
	}
	return bad
}

// invariantViolation checks ONE combination against the guarantee and returns a
// human-readable failure (empty string = it holds). It is shared by the grid and
// by the bite proof's single worked example, so both cannot drift.
func invariantViolation(fn grossUpFn, asking, fixed, bp, prevTotal int64) (string, string) {
	total, fee := fn(asking, fixed, bp)
	net := total - fee
	desc := fmt.Sprintf("asking=%d fixed=%d bp=%d → total=%d fee=%d net=%d", asking, fixed, bp, total, fee, net)
	if net-asking < 0 || net-asking > 1 {
		return desc, fmt.Sprintf("net−asking=%d is outside 0…1", net-asking)
	}
	// The fee charged must be exactly the formula's fee on the total: a
	// gross-up that returns an inconsistent pair is a silent over/under-charge
	// even when the net looks right.
	if want := ProcessingFee(total, fixed, bp); fee != want {
		return desc, fmt.Sprintf("fee is not ProcessingFee(total)=%d", want)
	}
	if prevTotal >= 0 && total < prevTotal {
		return desc, fmt.Sprintf("total DECREASED from %d when the asking price rose", prevTotal)
	}
	return "", ""
}

// naiveTotal is the WooCommerce plugin's formula (class-chip-woocommerce-gateway
// add_item_order_fee): add the rate applied to the ASKING price and stop there.
// It is shipped here only as the negative control for the grid.
//
// The second return is what the ACQUIRER charges, which is the fee on the amount
// the buyer actually pays (the naive total), not the fee that was added. That
// gap — the fee charged on the fee itself — is exactly the merchant's loss:
// RM 1,000 at 1% adds RM 10.00, the acquirer then takes RM 10.10 of the
// RM 1,010.00, and the merchant is left 10 sen short.
func naiveTotal(askingSen, fixedSen, percentBP int64) (totalSen, feeSen int64) {
	total := askingSen + fixedSen + (askingSen*percentBP+5000)/10000
	return total, ProcessingFee(total, fixedSen, percentBP)
}

// TestGrossUpGridSatisfiesMerchantExposureCap is the card's property grid: every
// rate × fixed × amount combination must leave the merchant exposed to at most
// ONE SEN of rounding, and the charged total must be monotonic in the asking
// price.
func TestGrossUpGridSatisfiesMerchantExposureCap(t *testing.T) {
	if bad := gridViolations(GrossUp); len(bad) > 0 {
		t.Fatalf("gross-up broke the 0…1 sen guarantee in %d combination(s):\n  %v", len(bad), bad)
	}
}

// TestNaiveFeeFormulaFailsTheSameGrid is the bite proof: it runs the IDENTICAL
// grid through the naive `A × (1+p)` formula and requires it to be red, with the
// card's own example (RM 1,000 at 1% → merchant loses 10 sen) among the
// failures. If this test ever passes, the grid above is not measuring anything.
func TestNaiveFeeFormulaFailsTheSameGrid(t *testing.T) {
	bad := gridViolations(naiveTotal)
	if len(bad) == 0 {
		t.Fatal("the naive fee formula PASSED the grid — the grid does not bite, so the green above is empty")
	}
	// The card's worked example must be a failure under the naive formula:
	// A = 100000 sen (RM 1,000), 1% (100 bp), no fixed charge → the buyer pays
	// 101000, the acquirer takes 1010, the merchant nets 99990 — 10 sen short.
	if _, why := invariantViolation(naiveTotal, 100000, 0, 100, -1); why == "" {
		t.Fatal("the RM 1,000 @1% example PASSES the naive formula — the worked example is not measuring the loss")
	}
	total, fee := naiveTotal(100000, 0, 100)
	if total != 101000 || fee != 1010 {
		t.Fatalf("naive example moved: naiveTotal(100000,0,100) = (%d,%d), want (101000,1010)", total, fee)
	}
	if net := total - fee; net != 99990 {
		t.Fatalf("naive example: merchant nets %d sen, want 99990 (10 sen short of 100000)", net)
	}
}

// TestGrossUpWorkedExample locks the card's headline number: RM 1,000 at 1%
// makes the buyer pay RM 1,010.10 and leaves the merchant exactly RM 1,000.00.
func TestGrossUpWorkedExample(t *testing.T) {
	total, fee := GrossUp(100000, 0, 100)
	if total != 101010 || fee != 1010 {
		t.Fatalf("GrossUp(100000,0,100) = (%d,%d), want (101010,1010)", total, fee)
	}
	if net := total - fee; net != 100000 {
		t.Fatalf("merchant nets %d, want exactly 100000", net)
	}
}

// TestGrossUpDisabledChargesNothing: with no fixed charge and no rate, the buyer
// pays the asking price and no fee line exists. This is what keeps a workspace
// that never turned the feature on byte-identical to the old behaviour.
func TestGrossUpDisabledChargesNothing(t *testing.T) {
	for _, asking := range feeGridAmounts {
		total, fee := GrossUp(asking, 0, 0)
		if total != asking || fee != 0 {
			t.Fatalf("disabled rate on %d: got total=%d fee=%d, want total=%d fee=0", asking, total, fee, asking)
		}
	}
}

// TestGrossUpFixedOnly: a fixed charge alone is exactly the fixed amount added
// once, no percentage — the buyer pays A + fixed and the merchant nets A.
func TestGrossUpFixedOnly(t *testing.T) {
	for _, fixed := range []int64{100, 200, 1} {
		for _, asking := range feeGridAmounts {
			total, fee := GrossUp(asking, fixed, 0)
			if fee != fixed {
				t.Fatalf("asking=%d fixed=%d: fee=%d, want %d", asking, fixed, fee, fixed)
			}
			if net := total - fee; net != asking {
				t.Fatalf("asking=%d fixed=%d: net=%d, want %d", asking, fixed, net, asking)
			}
		}
	}
}

// TestGrossUpIsMinimal: no smaller total can still cover the asking price. The
// grid asserts the guarantee; this asserts we are not silently over-charging the
// buyer by rounding up an extra sen.
func TestGrossUpIsMinimal(t *testing.T) {
	for _, bp := range feeGridRates {
		for _, fixed := range feeGridFixed {
			for _, asking := range feeGridAmounts {
				total, _ := GrossUp(asking, fixed, bp)
				if total <= 0 {
					continue
				}
				prev := total - 1
				if prev-ProcessingFee(prev, fixed, bp) >= asking {
					t.Fatalf("asking=%d fixed=%d bp=%d: total %d is not minimal (total-1=%d also covers it)",
						asking, fixed, bp, total, prev)
				}
			}
		}
	}
}

// TestGrossUpRejectsAPercentThatCannotBePriced documents the bound the schema
// and the settings route enforce: at 100% the fee grows as fast as the amount,
// so GrossUp clamps to the largest storable rate instead of looping forever.
func TestGrossUpRejectsAPercentThatCannotBePriced(t *testing.T) {
	if MaxFeePercentBP != 9999 {
		t.Fatalf("MaxFeePercentBP = %d, want 9999", MaxFeePercentBP)
	}
	total, fee := GrossUp(100000, 0, 10000) // 100%: refused at write time
	if total <= 0 || fee <= 0 {
		t.Fatalf("GrossUp(…,10000) returned (%d,%d) — it must clamp, not spin", total, fee)
	}
	if net := total - fee; net < 100000 {
		t.Fatalf("clamped 100%% rate left the merchant worse off: net=%d want ≥100000", net)
	}
}

// TestFeeRateLabel pins the receipt/disclosure wording to the FROZEN snapshot.
func TestFeeRateLabel(t *testing.T) {
	cases := []struct {
		method  string
		fixed   int64
		bp      int64
		want    string
		explain string
	}{
		{FeeMethodCard, 0, 200, "Card 2.00%", "the card example from the card body"},
		{FeeMethodFPX, 100, 0, "FPX RM1.00", "a fixed-only FPX charge"},
		{FeeMethodEWallet, 100, 140, "e-Wallet 1.40% + RM1.00", "both components, percentage first"},
		{FeeMethodDuitNowQ, 0, 50, "DuitNow QR 0.50%", "half a percent keeps two decimals"},
		{FeeMethodFPX, 0, 1, "FPX 0.01%", "one basis point"},
		{FeeMethodCard, 0, 0, "Card", "a zero rate degrades to the method label alone"},
		{"mystery", 0, 100, "mystery 1.00%", "an unknown key falls back to the key, never a blank"},
	}
	for _, c := range cases {
		if got := FeeRateLabel(c.method, c.fixed, c.bp); got != c.want {
			t.Errorf("%s: FeeRateLabel(%q,%d,%d) = %q, want %q", c.explain, c.method, c.fixed, c.bp, got, c.want)
		}
	}
}

// TestValidFeeMethod covers the schema's method list from the Go side, so a
// method the DB accepts but the code does not is caught here rather than at
// checkout.
func TestValidFeeMethod(t *testing.T) {
	for _, m := range FeeMethods {
		if !ValidFeeMethod(m) {
			t.Errorf("ValidFeeMethod(%q) = false for a listed method", m)
		}
	}
	for _, m := range []string{"", "visa", "CARD", "duitnow"} {
		if ValidFeeMethod(m) {
			t.Errorf("ValidFeeMethod(%q) = true, want false", m)
		}
	}
}

// msgf is gone: the grid builds its failure strings with fmt.Sprintf directly.

// TestTwoFPXFeeKeysAreDistinct pins the KEY split from card t_1d0198e3: FPX B2C
// (fpx) and FPX B2B1 (fpx_b2b1) must be separately storable and separately
// labelled, or a workspace cannot charge RM1 for one and RM2 for the other.
//
// BITE PROOF: drop FeeMethodFPXB2B1 from FeeMethods (or fold the key back into
// FeeMethodFPX) and this test fails on the rate-label equality below, because both
// products then print the same buyer-facing line.
func TestTwoFPXFeeKeysAreDistinct(t *testing.T) {
	if !ValidFeeMethod(FeeMethodFPXB2B1) {
		t.Fatalf("ValidFeeMethod(%q) = false — the DB CHECK accepts it, so the code must too", FeeMethodFPXB2B1)
	}
	if FeeMethodFPX == FeeMethodFPXB2B1 {
		t.Fatalf("the two FPX products share one fee key (%q)", FeeMethodFPX)
	}
	// Both are in the storable list, adjacent and in the picker's order.
	idx := -1
	for i, m := range FeeMethods {
		if m == FeeMethodFPXB2B1 {
			idx = i
		}
	}
	if idx != 1 || FeeMethods[0] != FeeMethodFPX {
		t.Fatalf("FeeMethods = %v, want FPX B2C first and FPX B2B1 second", FeeMethods)
	}
	// The labels a buyer reads differ, and so do the amounts they can carry.
	b2c := FeeRateLabel(FeeMethodFPX, 100, 0)
	b2b1 := FeeRateLabel(FeeMethodFPXB2B1, 200, 0)
	if b2c != "FPX RM1.00" {
		t.Fatalf("FPX B2C label = %q, want %q", b2c, "FPX RM1.00")
	}
	if b2b1 != "FPX B2B1 RM2.00" {
		t.Fatalf("FPX B2B1 label = %q, want %q", b2b1, "FPX B2B1 RM2.00")
	}
	if b2c == b2b1 {
		t.Fatalf("the two FPX products print the same fee line (%q)", b2c)
	}
}
