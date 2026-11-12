package payments

// Cards t_135c3c72 and t_1a17d22d — the receipt and the PDF must show the
// buyer's charge as its OWN line, with the amount FROZEN at purchase time. A fee
// that is folded into an item price hides what the buyer paid for the goods; a
// fee recomputed from the merchant's CURRENT settings rewrites history every
// time the settings change.
//
// t_1a17d22d renamed the line: the charge is a PLATFORM FEE — JomForm's own
// charge, not the acquirer's — so the buyer-facing word is "Platform fee" on
// every surface, and the old "Processing fee — <method> %" wording (which said
// the acquirer was taking it) is gone from the buyer's path.

import (
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

func TestReceiptFeeLineUsesTheFrozenAmount(t *testing.T) {
	ref := SaleRef{
		RefCode:      "JF-1",
		ItemsSummary: "Produk A × 1 — RM 1,000.00",
		AmountSen:    101010,
		FeeSen:       1010,
		FeeMethod:    FeeMethodCard,
		FeePercentBp: 100, // a legacy gross-up sale
	}
	if got, want := ref.FeeLine(), "Platform fee: RM 10.10"; got != want {
		t.Errorf("FeeLine() = %q, want %q", got, want)
	}
	items := receiptItems(ref)
	if !strings.Contains(items, "Produk A") {
		t.Errorf("receipt items dropped the line items: %q", items)
	}
	if !strings.Contains(items, "Platform fee: RM 10.10") {
		t.Errorf("receipt items have no separate fee line: %q — a buyer cannot see what the goods cost and what paying cost", items)
	}
	// The buyer's path must never call it the acquirer's charge (card
	// t_1a17d22d): no "processing fee", no percentage, no acquirer wording.
	for _, banned := range []string{"Processing fee", "processing fee", "1.00%", "acquirer"} {
		if strings.Contains(ref.FeeLine(), banned) || strings.Contains(items, banned) {
			t.Errorf("buyer receipt shows %q — the charge is JomForm's platform fee, never the acquirer's and never a rate the buyer did not agree to (line %q, items %q)",
				banned, ref.FeeLine(), items)
		}
	}

	// The PDF grid carries the same amount, its own row, above the total.
	var feeRow, amountRow int
	rows := receiptDetailRows(ref)
	for i, r := range rows {
		switch {
		case strings.Contains(r[0], "Platform fee"):
			feeRow = i + 1
			if r[1] != "RM 10.10" {
				t.Errorf("PDF fee row value = %q, want the frozen amount RM 10.10", r[1])
			}
		case strings.HasPrefix(r[0], "Jumlah") || strings.HasPrefix(r[0], "Amount"):
			amountRow = i + 1
		}
	}
	if feeRow == 0 {
		t.Fatalf("PDF grid has no platform-fee row: %#v", rows)
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
		if strings.Contains(r[0], "Platform fee") || strings.Contains(r[1], "Platform fee") {
			t.Errorf("a sale with no fee rendered a fee row: %#v", r)
		}
	}
}

// TestFeeLineFollowsTheSnapshotNotTheStore: the same sale rendered after the
// merchant changes their settings still prints what the buyer actually paid.
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
	if got, want := ref.FeeLine(), "Platform fee: RM 10.10"; got != want {
		t.Fatalf("FeeLine() = %q, want %q", got, want)
	}
	// A helper that reads a CURRENT rate would print the new rate (or a bigger
	// fee) here; the snapshot is what is printed.
	if strings.Contains(ref.FeeLine(), "%") {
		t.Errorf("receipt re-derived a rate instead of printing the sale's snapshot: %q", ref.FeeLine())
	}
}
