// Pay-what-you-want ("custom_amount") product rules that are shared by the
// REST handlers, the MCP tools and the buyer checkout — card t_06851d88.
//
// They live in this package rather than in httpapi because all three surfaces
// must apply the SAME rule: a product the dashboard accepts must be a product
// an agent can create, and a product either of them creates must be a product
// the order route can price. One implementation, three callers.
package payments

import "fmt"

// ValidateCustomAmountLimits checks the limit triple of a custom_amount
// product. It returns a sentence for the caller to refuse with, or "" when the
// triple is usable.
//
// The rules mirror the schema CHECK in migration 000084_custom_amount_products;
// validating here too is what turns a raw SQLSTATE 23514 into a message the
// editor (or the agent) can act on.
func ValidateCustomAmountLimits(minSen int64, maxSen, suggestedSen *int64) string {
	if minSen < MinCustomAmountSen {
		return fmt.Sprintf("min_price_sen must be at least %d sen (RM1.00): below that the acquirer's fixed fee exceeds the donation", MinCustomAmountSen)
	}
	if maxSen != nil && *maxSen < minSen {
		return "max_price_sen must be greater than or equal to min_price_sen"
	}
	if maxSen != nil && *maxSen > MaxAmountSen {
		return "max_price_sen is too large"
	}
	if suggestedSen != nil {
		if *suggestedSen < minSen {
			return "suggested_price_sen must be greater than or equal to min_price_sen"
		}
		if maxSen != nil && *suggestedSen > *maxSen {
			return "suggested_price_sen must not exceed max_price_sen"
		}
	}
	return ""
}

// CustomAmountDisplayPrice is the price_sen stored for a custom_amount product:
// the suggested amount when the merchant set one, else the floor.
//
// price_sen is NOT what such a product is charged (the buyer's validated amount
// is), but it must stay a sane number for every consumer that still reads a
// product price — the product list, the menu, an export.
func CustomAmountDisplayPrice(minSen int64, suggestedSen *int64) int64 {
	if suggestedSen != nil && *suggestedSen > 0 {
		return *suggestedSen
	}
	return minSen
}
