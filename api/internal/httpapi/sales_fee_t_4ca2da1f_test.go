package httpapi

// t_4ca2da1f — the dashboard's Sales page never showed the processing fee the
// buyer bore, nor what the merchant actually receives.
//
// THE DEFECT (measured on this tree against a private schema, FPX at 100 bp, one
// paid sale with subtotal 100000 / fee 1010 / total 101010): /sales rendered
// "JUMLAH RM1,010.10" and the detail drawer carried no fee row and no net
// figure, so a treasurer read RM1,010.10 as income when the merchant nets
// RM1,000.00. The data was there — sales.fee_sen/fee_method/fee_fixed_sen/
// fee_percent_bp — and the buyer's receipt already printed the line
// (payments.SaleRef.FeeLine, "Processing fee — FPX 1.00%: RM 10.10"); the REST row
// carried fee_sen but nothing the dashboard could label it with.
//
// WHAT THIS GUARD COVERS
//   1. toSaleRow carries the fee AND a human rate label, and the JSON emits
//      both (`fee_sen`, `fee_rate_label`) for the page to read. Asserted through
//      the marshalled payload, because that is what the page actually sees.
//   2. The label is produced by the receipt's OWN formatter over the sale's
//      FROZEN snapshot — the test compares it against SaleRef.FeeLine() for the
//      same sale, so the drawer and the receipt cannot describe one charge two
//      ways, and no percentage arithmetic is re-implemented on this side.
//   3. A sale with no fee is unchanged: fee_rate_label is absent from the JSON
//      (omitempty), so a pre-fee sale's row stays byte-identical.
//
// WHAT IT DOES NOT COVER: that the page paints it. That is
// web/src/lib/salesFee.test.ts (the arithmetic and wording) plus the browser
// drive of /sales in the handoff.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/payments"
)

// feeBearingSale is the row shape the order path writes for RM1,000 at FPX 1%:
// subtotal 100000, fee 1010, total 101010 (payments.GrossUp(100000, 0, 100)).
func feeBearingSale() *dbstore.Sale {
	return &dbstore.Sale{
		RefCode:      "JF-FEE1",
		Status:       dbstore.SalePaid,
		SubtotalSen:  100000,
		FeeSen:       1010,
		FeeMethod:    payments.FeeMethodFPX,
		FeeFixedSen:  0,
		FeePercentBp: 100,
		TotalSen:     101010,
	}
}

func TestSaleRowCarriesTheFeeAndItsFrozenRateLabel(t *testing.T) {
	row := toSaleRow(feeBearingSale(), "")

	if row.FeeSen != 1010 {
		t.Fatalf("FeeSen = %d, want 1010 — the dashboard cannot show a fee the row dropped", row.FeeSen)
	}
	if got, want := row.FeeRateLabel, "FPX 1.00%"; got != want {
		t.Fatalf("FeeRateLabel = %q, want %q", got, want)
	}
	// The page reads JSON, so the keys are the contract.
	body, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshal sale row: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("unmarshal sale row: %v", err)
	}
	if got := decoded["fee_sen"]; got != float64(1010) {
		t.Errorf("JSON fee_sen = %v, want 1010", got)
	}
	if got := decoded["fee_rate_label"]; got != "FPX 1.00%" {
		t.Errorf("JSON fee_rate_label = %v, want %q", got, "FPX 1.00%")
	}
}

// The drawer's fee line and the buyer's receipt must name the same charge the
// same way. Since card t_1a17d22d the receipt renders "Platform fee: RM <fee>"
// — the flat amount the buyer agreed to, with no rate and no acquirer's name —
// so the part the two surfaces share is the charge's NAME, and it must come
// from the one constant (payments.PlatformFeeLineName) rather than a second
// literal. The row keeps the frozen legacy rate beside it for the drawer, which
// is why the label no longer appears inside the receipt's line.
func TestSaleRowFeeLabelIsTheReceiptsOwnLabel(t *testing.T) {
	sl := feeBearingSale()
	// A combined rate, so a fixed amount and a percentage are both exercised.
	sl.FeeMethod = payments.FeeMethodCard
	sl.FeeFixedSen = 100
	sl.FeePercentBp = 200
	sl.FeeSen = 2182 // 2% of 101010, rounded, + RM1.00 — the value is irrelevant to the label
	sl.TotalSen = 103192

	row := toSaleRow(sl, "")
	ref := payments.SaleRef{
		FeeSen:       sl.FeeSen,
		FeeMethod:    sl.FeeMethod,
		FeeFixedSen:  sl.FeeFixedSen,
		FeePercentBp: sl.FeePercentBp,
	}
	line := ref.FeeLine()
	if line == "" {
		t.Fatalf("the receipt rendered no fee line for a fee-bearing sale")
	}
	if !strings.Contains(line, payments.PlatformFeeLineName) {
		t.Fatalf("receipt line %q does not name the charge %q the drawer names — the drawer and the receipt would call one charge two things", line, payments.PlatformFeeLineName)
	}
	if got, want := row.FeeRateLabel, "Card 2.00% + RM1.00"; got != want {
		t.Errorf("FeeRateLabel = %q, want %q", got, want)
	}
}

func TestSaleRowWithoutAFeeEmitsNoFeeLabel(t *testing.T) {
	row := toSaleRow(&dbstore.Sale{RefCode: "JF-PLAIN", Status: dbstore.SalePaid, SubtotalSen: 5000, TotalSen: 5000}, "")
	if row.FeeSen != 0 || row.FeeRateLabel != "" {
		t.Fatalf("a no-fee sale grew a fee: fee_sen=%d fee_rate_label=%q", row.FeeSen, row.FeeRateLabel)
	}
	body, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshal sale row: %v", err)
	}
	if strings.Contains(string(body), "fee_rate_label") {
		t.Fatalf("a no-fee sale's JSON grew fee_rate_label: %s", body)
	}
}
