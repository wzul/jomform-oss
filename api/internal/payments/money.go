// Money invariants for the payment path. All amounts are integer SEN
// (AGENTS.md: "Money = integer sen everywhere"). These pure helpers are the
// single source of truth for total_sen math so the money path can be unit
// tested without a database or CHIP.
package payments

import "github.com/wzul/jomform/api/internal/dbstore"

// SumItems computes the sale total in SEN as subtotal + shipping - discount.
// It is the canonical PRE-FEE total: what the buyer would pay if the merchant
// bears the acquirer's charge themselves. It is also the asking price the
// processing-fee gross-up is computed against (see GrossUp).
func SumItems(subtotalSen, shippingSen, discountSen int64) int64 {
	return subtotalSen + shippingSen - discountSen
}

// SumWithFee is the sale total when the buyer bears the processing fee:
// subtotal + shipping - discount + fee. This is the one formula total_sen
// follows after a fee is applied (payments.GrossUp returns exactly this pair,
// so the components and the total cannot disagree).
func SumWithFee(subtotalSen, shippingSen, discountSen, feeSen int64) int64 {
	return SumItems(subtotalSen, shippingSen, discountSen) + feeSen
}

// LineSubtotal returns the SEN subtotal for a set of line items
// (sum of price_sen × qty). Used to derive subtotal_sen before applying
// shipping/discount.
func LineSubtotal(items []dbstore.SaleItem) int64 {
	var total int64
	for _, it := range items {
		total += it.PriceSen * it.Qty
	}
	return total
}
