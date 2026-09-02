// Money invariants for the payment path. All amounts are integer SEN
// (AGENTS.md: "Money = integer sen everywhere"). These pure helpers are the
// single source of truth for total_sen math so the money path can be unit
// tested without a database or CHIP.
package payments

import "github.com/wzul/jomform/api/internal/dbstore"

// SumItems computes the sale total in SEN as subtotal + shipping - discount.
// It is the canonical total_sen formula; the HTTP submit path and any
// downstream consumer must agree with it (issue #99 money invariant).
func SumItems(subtotalSen, shippingSen, discountSen int64) int64 {
	return subtotalSen + shippingSen - discountSen
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
