// Pay-what-you-want ("custom_amount") products — card t_06851d88.
//
// A custom_amount product has no fixed asking price: the buyer types the amount
// and the SERVER decides what that amount means. The client's `amount_sen` is a
// CLAIM, exactly like `fee_method` and `expected_total_sen`: it is validated
// against the product's own stored limits, and only then does it become the
// line item's price. Nothing the buyer's page sends is ever charged unverified.
//
// The rules that live here:
//
//   - the amount must be a whole number of sen (money is integer sen, never a
//     float) and must exist at all;
//   - it must be at least the product's min_price_sen AND at least the platform
//     floor payments.MinCustomAmountSen (RM1.00 — see the constant for why);
//   - it must not exceed max_price_sen when the product sets one;
//   - a refused amount answers 400 with an ENGLISH sentence (the wire `error`),
//     carrying the buyer's language in `message`, so a JS page and a curl client
//     both read a usable refusal.
package httpapi

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/wzul/jomform/api/internal/buyertext"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/payments"
)

// moneyLabel renders integer sen as a buyer-facing amount ("RM1.00") for a
// refusal sentence. The order core refuses a mismatched amount BEFORE the sale's
// currency is resolved in some paths, so the product's own currency (MYR by
// default) is the right label here.
func moneyLabel(sen int64, currency string) string {
	cur := strings.TrimSpace(currency)
	if cur == "" {
		cur = "MYR"
	}
	return fmt.Sprintf("%s%.2f", curSymbol(cur), float64(sen)/100)
}

// curSymbol is the display symbol for the currencies the platform sells in.
// It is deliberately tiny: a refusal must never print "MYR1.00" to a buyer on a
// MYR workspace, and a currency without a known symbol keeps its ISO code.
func curSymbol(code string) string {
	switch code {
	case "MYR":
		return "RM"
	case "USD", "SGD", "AUD", "NZD", "CAD", "HKD", "BND", "TWD":
		return "$"
	case "EUR":
		return "€"
	case "GBP":
		return "£"
	case "JPY", "CNY":
		return "¥"
	case "INR":
		return "₹"
	default:
		return code + " "
	}
}

// amountFloorSen is the smallest amount a custom_amount product may charge: its
// own min_price_sen, but never below the platform floor.
func amountFloorSen(p *dbstore.Product) int64 {
	floor := p.MinPriceSen
	if floor < payments.MinCustomAmountSen {
		floor = payments.MinCustomAmountSen
	}
	return floor
}

// customAmountSen validates the buyer-chosen amount for one custom_amount
// product and returns the amount, in integer sen, that the sale must record.
//
// raw is the item's `amount_sen` exactly as the client sent it (a json.Number,
// so "100.5" is readable as a non-integer rather than silently truncated).
// Every refusal is a buyertext message whose `error` text is English.
func customAmountSen(p *dbstore.Product, raw *json.Number) (int64, error) {
	if raw == nil || strings.TrimSpace(raw.String()) == "" {
		return 0, buyertext.New(buyertext.CodeOrderAmountRequired, nil)
	}
	amt, err := strconv.ParseInt(strings.TrimSpace(raw.String()), 10, 64)
	if err != nil {
		// A float ("100.5"), a string ("RM10"), or a value out of int64 range.
		return 0, buyertext.New(buyertext.CodeOrderAmountInvalid, nil)
	}
	floor := amountFloorSen(p)
	if amt < floor {
		return 0, buyertext.New(buyertext.CodeOrderAmountTooLow,
			map[string]string{"min": moneyLabel(floor, p.Currency)})
	}
	if p.MaxPriceSen != nil && amt > *p.MaxPriceSen {
		return 0, buyertext.New(buyertext.CodeOrderAmountTooHigh,
			map[string]string{"max": moneyLabel(*p.MaxPriceSen, p.Currency)})
	}
	return amt, nil
}

// customAmountWriteValues is a resolved + validated limit triple for a
// custom_amount product write.
type customAmountWriteValues struct {
	MinSen     int64
	MaxSen     *int64
	SuggestSen *int64
	// PriceSen is the display price derived from the triple (suggested, else
	// the floor). It is NEVER the price a custom-amount sale is charged.
	PriceSen int64
}

// mergeCustomAmountLimits merges a (possibly partial) write over the stored
// limits and validates the result. A partial update cannot be judged alone —
// raising min_price_sen past an old suggested_price_sen only fails as a
// MERGED triple — so callers pass the stored values and only the fields the
// request actually set.
//
// setMax/setSuggest distinguish "not sent" (keep) from "sent as null" (clear),
// which is why they are separate booleans rather than a nil check on the value.
// It returns a merchant-facing sentence when the merged triple is unusable.
func mergeCustomAmountLimits(storedMin int64, storedMax, storedSuggest *int64,
	newMin *int64, setMax bool, newMax *int64, setSuggest bool, newSuggest *int64) (customAmountWriteValues, string) {
	out := customAmountWriteValues{MinSen: storedMin, MaxSen: storedMax, SuggestSen: storedSuggest}
	if newMin != nil {
		out.MinSen = *newMin
	}
	if setMax {
		out.MaxSen = newMax
	}
	if setSuggest {
		out.SuggestSen = newSuggest
	}
	if msg := customProductLimits(out.MinSen, out.MaxSen, out.SuggestSen); msg != "" {
		return out, msg
	}
	out.PriceSen = customProductDisplayPrice(out.MinSen, out.SuggestSen)
	return out, ""
}

// customProductLimits validates the write-side limits of a custom_amount
// product (create, or an update that touches type/limits). It returns a
// merchant-facing English sentence, or "" when the triple is usable.
//
// The rule itself lives in payments so the REST handlers and the MCP tools
// cannot drift apart (both write the same columns).
func customProductLimits(minSen int64, maxSen, suggestedSen *int64) string {
	return payments.ValidateCustomAmountLimits(minSen, maxSen, suggestedSen)
}

// customProductDisplayPrice is the price_sen stored for a custom_amount product:
// the suggested amount when the merchant set one, else the floor. price_sen is
// NOT what such a product is charged (the buyer's validated amount is), but it
// must stay a sane number for every consumer that still reads a product price.
func customProductDisplayPrice(minSen int64, suggestedSen *int64) int64 {
	return payments.CustomAmountDisplayPrice(minSen, suggestedSen)
}

// saleRecordsCustomAmount reports whether the cart's already-resolved sale type
// must become custom_amount: true when at least one line item was priced from a
// buyer-chosen amount and the sale is not a subscription.
//
// A subscription wins: a mixed cart keeps the recurring terms it committed to,
// and the one-off donation line is recorded as what it was charged. This mirrors
// resolveSubscriptionTerms, which is why the order core calls it AFTER that
// resolution and only downgrades a still-one_time sale.
func saleRecordsCustomAmount(currentSaleType string, sawCustomAmount bool) string {
	if sawCustomAmount && currentSaleType == dbstore.SaleTypeOneTime {
		return dbstore.SaleTypeCustomAmount
	}
	return currentSaleType
}
