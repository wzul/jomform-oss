package payments

// Card t_135c3c72 — the receipt and the PDF must show the buyer-borne
// processing fee as its OWN line, with the rate FROZEN at purchase time. A fee
// that is folded into an item price hides what the buyer paid for the goods; a
// fee recomputed from the merchant's CURRENT settings rewrites history every
// time the MDR changes.

import (
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

func TestReceiptFeeLineUsesFrozenRate(t *testing.T) {
	ref := SaleRef{
		RefCode:      "JF-1",
		ItemsSummary: "Produk A × 1 — RM 1,000.00",
		AmountSen:    101010,
		FeeSen:       1010,
		FeeMethod:    FeeMethodCard,
		FeePercentBp: 100,
	}
	if got, want := ref.FeeLine(), "Processing fee — Card 1.00%: RM 10.10"; got != want {
		t.Errorf("FeeLine() = %q, want %q", got, want)
	}
	items := receiptItems(ref)
	if !strings.Contains(items, "Produk A") {
		t.Errorf("receipt items dropped the line items: %q", items)
	}
	if !strings.Contains(items, "Processing fee — Card 1.00%: RM 10.10") {
		t.Errorf("receipt items have no separate fee line: %q — a buyer cannot see what the goods cost and what paying cost", items)
	}

	// The PDF grid carries the same frozen wording, its own row, above the total.
	var feeRow, amountRow int
	rows := receiptDetailRows(ref)
	for i, r := range rows {
		switch {
		case strings.Contains(r[0], "Processing fee"):
			feeRow = i
			if r[1] != "Processing fee — Card 1.00%: RM 10.10" {
				t.Errorf("PDF fee row value = %q, want the frozen rate wording", r[1])
			}
		case strings.HasPrefix(r[0], "Jumlah"):
			amountRow = i
		}
	}
	if feeRow == 0 {
		t.Fatalf("PDF grid has no processing-fee row: %#v", rows)
	}
	if amountRow == 0 || feeRow > amountRow {
		t.Errorf("PDF fee row must sit above the total (fee at %d, total at %d)", feeRow, amountRow)
	}
}

func TestReceiptWithoutFeeIsUnchanged(t *testing.T) {
	ref := SaleRef{RefCode: "JF-2", ItemsSummary: "Produk A × 1 — RM 10.00", AmountSen: 1000}
	if line := ref.FeeLine(); line != "" {
		t.Errorf("FeeLine() = %q, want empty on a sale with no fee", line)
	}
	if got := receiptItems(ref); got != ref.ItemsSummary {
		t.Errorf("receipt items = %q, want the summary untouched", got)
	}
	for _, r := range receiptDetailRows(ref) {
		if strings.Contains(r[0], "Processing fee") {
			t.Errorf("a sale with no fee rendered a fee row: %#v", r)
		}
	}
}

// TestFeeLineFollowsTheSnapshotNotTheStore: the same sale rendered after the
// merchant changes their rate still prints the rate the buyer actually paid.
func TestFeeLineFollowsTheSnapshotNotTheStore(t *testing.T) {
	sale := &dbstore.Sale{
		RefCode: "JF-3", TotalSen: 101010, SubtotalSen: 100000,
		FeeSen: 1010, FeeMethod: FeeMethodCard, FeePercentBp: 100, FeeFixedSen: 0,
	}
	ref := SaleRef{
		RefCode: sale.RefCode, AmountSen: sale.TotalSen, ItemsSummary: "Produk A",
		FeeSen: sale.FeeSen, FeeMethod: sale.FeeMethod,
		FeeFixedSen: sale.FeeFixedSen, FeePercentBp: sale.FeePercentBp,
	}
	if got, want := ref.FeeLine(), "Processing fee — Card 1.00%: RM 10.10"; got != want {
		t.Fatalf("FeeLine() = %q, want %q", got, want)
	}
	// A helper that reads a CURRENT rate (were one used) would print 6.00% here.
	if strings.Contains(ref.FeeLine(), "6.00%") {
		t.Errorf("receipt used the current rate instead of the sale's snapshot")
	}
}
