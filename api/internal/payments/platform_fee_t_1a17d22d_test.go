package payments

// Card t_1a17d22d — the platform fee is its OWN line in `purchase.products`,
// and the buyer's typed amount does not move.
//
// WHAT SHIPPED BEFORE: the charge was modelled as the acquirer's fee passed on
// and GROSSED UP into the total (RM1,000 at 1% -> the buyer paid RM1,010.10).
// The arithmetic was right but the presentation was not: it read as a mysterious
// markup on the buyer's own amount, and CHIP's checkout showed one total larger
// than the item.
//
// WHAT THIS PINS:
//  1. a platform-fee sale sends TWO product lines — the item at the price the
//     buyer typed, and "Platform fee" at the flat amount — and the total is
//     exactly item + fee;
//  2. a sale with no fee keeps one line per item, byte for byte;
//  3. the fee line's NAME is JomForm's own word, never the acquirer's;
//  4. the purchase is pinned to the CHIP identifier whose fee was charged
//     (`fpx` vs `fpx_b2b1`), so the buyer cannot reach the other variant after
//     seeing the amount.

import (
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/chip"
	"github.com/wzul/jomform/api/internal/dbstore"
)

func platformFeeSale() *dbstore.Sale {
	return &dbstore.Sale{
		Items:          []dbstore.SaleItem{{ProductID: "p1", Name: "Derma Surau", PriceSen: 100000, Qty: 1}},
		SubtotalSen:    100000,
		TotalSen:       100100,
		FeeSen:         100,
		PlatformFeeSen: 100,
		FeeMethod:      FeeMethodFPX,
		FeeIdentifier:  FeeMethodFPX,
		FeeModel:       dbstore.FeeModelPlatform,
	}
}

func TestChipProductsCarryThePlatformFeeAsItsOwnLine(t *testing.T) {
	sale := platformFeeSale()
	products := chipProductsForSale(sale)
	if len(products) != 2 {
		t.Fatalf("products = %d lines, want 2 (the item and the platform fee): %#v", len(products), products)
	}
	if products[0].Name != "Derma Surau" || products[0].Price != 100000 {
		t.Errorf("item line = %q/%d, want the buyer's own item at the price they typed (100000)", products[0].Name, products[0].Price)
	}
	if products[1].Name != PlatformFeeLineName || products[1].Price != 100 {
		t.Errorf("fee line = %q/%d, want %q/100", products[1].Name, products[1].Price, PlatformFeeLineName)
	}
	// The buyer's typed amount must not move: the item line is untouched and the
	// charged total is the item plus the fee, nothing more.
	if sale.Items[0].PriceSen != 100000 {
		t.Errorf("the sale's item price moved to %d — a platform fee must never be folded into the item", sale.Items[0].PriceSen)
	}
	if sum(products) != sale.TotalSen {
		t.Errorf("products sum = %d, total = %d — the total must equal item + platform fee", sum(products), sale.TotalSen)
	}
}

func TestChipProductsWithoutAFeeAreJustTheItems(t *testing.T) {
	sale := &dbstore.Sale{
		Items:       []dbstore.SaleItem{{Name: "Tee", PriceSen: 4500, Qty: 2}},
		SubtotalSen: 9000,
		TotalSen:    9000,
	}
	products := chipProductsForSale(sale)
	if len(products) != 1 {
		t.Fatalf("products = %d lines, want 1 — a fee-free sale gained a fee line: %#v", len(products), products)
	}
	if products[0].Price != 4500 || products[0].Quantity != "2" {
		t.Errorf("item line = %+v, want the item unchanged", products[0])
	}
}

// TestChipProductsNeverCallTheFeeTheAcquirersCharge is the wording guard: the
// buyer's CHIP checkout is the one surface where the name is all they have.
func TestChipProductsNeverCallTheFeeTheAcquirersCharge(t *testing.T) {
	// A legacy gross-up sale carries the same line name (migration 000090): the
	// old "Processing fee — Card 1.00%" must not survive on the buyer's path.
	legacy := platformFeeSale()
	legacy.FeeIdentifier = ""
	legacy.FeeModel = dbstore.FeeModelLegacyGrossUp
	legacy.FeeSen = 1010
	legacy.PlatformFeeSen = 0
	legacy.FeePercentBp = 100
	legacy.TotalSen = 101010
	for _, sale := range []*dbstore.Sale{platformFeeSale(), legacy} {
		products := chipProductsForSale(sale)
		fee := products[len(products)-1]
		if fee.Name != PlatformFeeLineName {
			t.Errorf("fee line name = %q, want %q on every model", fee.Name, PlatformFeeLineName)
		}
		if wantsAcquirerWording(fee.Name) {
			t.Errorf("fee line %q reads as the acquirer's charge — it is JomForm's platform fee", fee.Name)
		}
	}
}

// TestFeeWhitelistPinsTheChargedIdentifier: the purchase offers exactly the CHIP
// identifier whose fee was charged.
func TestFeeWhitelistPinsTheChargedIdentifier(t *testing.T) {
	configured := []string{"fpx", "card", "ewallet"}

	// A platform fee charged for the corporate FPX flow pins fpx_b2b1 — not the
	// buyer-facing group, and never both variants.
	b2b := platformFeeSale()
	b2b.FeeIdentifier = FeeMethodFPXB2B1
	if got := feeWhitelist(b2b, configured); len(got) != 1 || got[0] != FeeMethodFPXB2B1 {
		t.Errorf("feeWhitelist = %v, want exactly [%s]", got, FeeMethodFPXB2B1)
	}

	// A legacy sale written before fee_identifier existed falls back to the
	// group key, which is what it has always pinned.
	legacy := platformFeeSale()
	legacy.FeeIdentifier = ""
	legacy.FeeMethod = FeeMethodCard
	legacy.FeeModel = dbstore.FeeModelLegacyGrossUp
	if got := feeWhitelist(legacy, configured); len(got) != 1 || got[0] != FeeMethodCard {
		t.Errorf("legacy feeWhitelist = %v, want exactly [%s]", got, FeeMethodCard)
	}

	// No fee: the merchant's own whitelist is untouched.
	noFee := platformFeeSale()
	noFee.FeeSen = 0
	noFee.PlatformFeeSen = 0
	noFee.FeeMethod = ""
	noFee.FeeIdentifier = ""
	noFee.FeeModel = ""
	if got := feeWhitelist(noFee, configured); len(got) != len(configured) {
		t.Errorf("feeWhitelist = %v, want the merchant's configured %v", got, configured)
	}
}

// TestLegacyPercentageSaleStillGrossesUp states the migration rule (card
// requirement 6) in the engine that implements it: a stored percentage keeps
// charging EXACTLY what it charges today for one release, and the total is not
// the buyer's price plus a flat platform amount.
func TestLegacyPercentageSaleStillGrossesUp(t *testing.T) {
	total, fee := GrossUp(100000, 0, 100)
	if total != 101010 || fee != 1010 {
		t.Fatalf("legacy GrossUp(100000,0,100) = (%d,%d), want (101010,1010) — a workspace with a stored percentage must not be charged differently by this release", total, fee)
	}
	if total == 100000+1000 {
		t.Fatalf("legacy rate was priced as a flat fee: %d — that is the platform model, not the legacy one", total)
	}
}

func sum(products []chip.Product) int64 {
	var total int64
	for _, p := range products {
		total += p.Price
	}
	return total
}

// wantsAcquirerWording reports whether a fee-line name frames the charge as the
// gateway's/acquirer's rather than JomForm's.
func wantsAcquirerWording(name string) bool {
	lower := strings.ToLower(name)
	for _, bad := range []string{"processing fee", "acquirer", "gateway", "mdr", "%"} {
		if strings.Contains(lower, bad) {
			return true
		}
	}
	return false
}
