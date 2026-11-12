// Buyer-borne charges on a CHIP purchase (cards t_135c3c72 and t_1a17d22d).
//
// TWO MODELS LIVE HERE, and only one of them is the buyer's path.
//
//  1. PLATFORM FEE (the model going forward). The merchant sets, PER CHIP
//     PAYMENT METHOD, a flat amount in sen. JomForm adds that amount as its OWN
//     line in the CHIP purchase (`purchase.products[]`), so the buyer's typed
//     amount is untouched:
//
//     products: [ {name: "<item>", price: A}, {name: "Platform fee", price: P} ]
//
//     It is JomForm's charge, not the acquirer's: CHIP's own MDR is still the
//     merchant's cost and this fee does not describe itself as covering it.
//
//  2. LEGACY GROSS-UP (kept for exactly one release, see migration 000090). The
//     merchant stored a RATE (fixed sen and/or basis points) and the engine
//     grosses the total up so the merchant nets the asking price —
//     F - fee(F) >= A, e.g. RM1,000 at 1% -> the buyer pays RM1,010.10. A
//     workspace carrying a percentage keeps being charged exactly what it
//     charges today until the merchant saves a platform amount; nothing
//     silently changes under it.
//
// GrossUp below is that legacy path. It stays correct and tested because
// percentage rows still price with it, and because a workspace is moved to the
// platform model by an explicit save, never by a release.
//
// All amounts are integer SEN (AGENTS.md). A legacy rate is BASIS POINTS:
// 100 = 1%.
package payments

import (
	"fmt"
	"strings"
)

// FeeMethod* are the CHIP payment methods a per-method fee can be stored
// against. They are the keys the merchant's settings are stored under and the
// identifiers the CHIP purchase whitelist takes (payments.ResolveWhitelist
// expands `card` to the network identifiers CHIP expects).
//
// FeeMethodFPX and FeeMethodFPXB2B1 are TWO keys (card t_1d0198e3): FPX B2C and
// FPX B2B1 are two CHIP identifiers with their own acquirer rate, so a
// workspace must be able to store a different fee for each (RM1 vs RM2 in the
// common configuration). They are also two BUYER choices, which is why the key
// split had to reach the picker as well as this table — and it is what makes
// the platform amount land on the right identifier, since the platform fee is
// stored PER CHIP IDENTIFIER (card t_1a17d22d).
const (
	FeeMethodFPX      = "fpx"
	FeeMethodFPXB2B1  = "fpx_b2b1"
	FeeMethodCard     = "card"
	FeeMethodEWallet  = "ewallet"
	FeeMethodDuitNowQ = "duitnow_qr"
)

// FeeMethods lists every method a platform amount (or a legacy rate) can be
// stored for, in the order the dashboard settings and the buyer-facing chooser
// display them.
var FeeMethods = []string{FeeMethodFPX, FeeMethodFPXB2B1, FeeMethodCard, FeeMethodEWallet, FeeMethodDuitNowQ}

// PlatformFeeLineName is the name of the charge when it is sent to CHIP as its
// own purchase.products line (card t_1a17d22d). It is deliberately JomForm's own
// word: the buyer must not read it as a gateway or acquirer charge.
const PlatformFeeLineName = "Platform fee"

// feeMethodLabels are the human labels shown to the BUYER in the checkout
// breakdown and on the receipt line. A raw method key must never reach a buyer;
// an unknown key falls back to the key itself rather than an empty label.
var feeMethodLabels = map[string]string{
	FeeMethodFPX:      "FPX",
	FeeMethodFPXB2B1:  "FPX B2B1",
	FeeMethodCard:     "Card",
	FeeMethodEWallet:  "e-Wallet",
	FeeMethodDuitNowQ: "DuitNow QR",
}

// ValidFeeMethod reports whether m is a method a rate may be stored against.
func ValidFeeMethod(m string) bool {
	_, ok := feeMethodLabels[m]
	return ok
}

// FeeMethodLabel returns the buyer-facing label for a CHIP method key.
func FeeMethodLabel(m string) string {
	if l, ok := feeMethodLabels[m]; ok {
		return l
	}
	return m
}

// MaxAmountSen bounds the amounts this package will price (RM 10 billion). It
// exists so the division in GrossUp cannot overflow int64; the order route
// refuses a larger total outright rather than pricing it wrongly.
const MaxAmountSen int64 = 1_000_000_000_000

// MinCustomAmountSen is the smallest buyer-chosen amount a pay-what-you-want
// product may accept (RM1.00), and the smallest allowed min_price_sen of such
// a product.
//
// Why a floor at all: the acquirer charges a FIXED per-transaction fee, so a
// 1-sen donation costs the merchant more than it collects, and the gross-up
// that keeps that fee on top of the buyer's amount has no honest answer for an
// amount smaller than the fee itself. RM1.00 is the smallest amount where the
// fixed fee is a sane fraction of the gift. The value is mirrored by a schema
// CHECK (000084_custom_amount_products) so a direct write cannot create a
// product the checkout refuses to price.
const MinCustomAmountSen int64 = 100

// MaxFeePercentBP is the largest storable rate (99.99%). At exactly 100% the
// fee grows as fast as the amount and `F − fee(F) ≥ A` has no solution, so a
// merchant cannot save it: the store validates against this bound and the
// schema CHECK enforces the same rule.
const MaxFeePercentBP int64 = 9999

// ProcessingFee is what the LEGACY gross-up charges on amountSen, in integer
// sen: the fixed charge plus the percentage rounded HALF UP. Rounding half up
// is the single rule both this and the checkout page's client-side mirror use,
// because the page's number and the charge must agree to the sen.
//
// It prices a stored PERCENTAGE rate only (migration 000090): a platform-fee
// sale never calls it, because a platform fee does not depend on the amount.
func ProcessingFee(amountSen, fixedSen, percentBP int64) int64 {
	if amountSen <= 0 {
		if fixedSen < 0 {
			return 0
		}
		return fixedSen
	}
	if fixedSen < 0 {
		fixedSen = 0
	}
	if percentBP < 0 {
		percentBP = 0
	}
	if percentBP > MaxFeePercentBP {
		percentBP = MaxFeePercentBP
	}
	return fixedSen + (amountSen*percentBP+5000)/10000
}

// GrossUp returns the smallest integer total F such that
// F − ProcessingFee(F) ≥ askingSen, together with the fee charged on F.
//
// LEGACY ONLY (migration 000090). Only a workspace that stores a PERCENTAGE rate
// reaches this; a platform-fee sale adds a flat amount instead and never moves
// the buyer's price. It is exact and O(1): the closed-form estimate is computed,
// then the result is walked to the true minimum. The walk is bounded by one or
// two sen in practice (the estimate is the real solution rounded down by the
// integer division).
//
// Precondition: askingSen ≤ MaxAmountSen and 0 ≤ percentBP < 10000 (both
// enforced at the write path and by the schema). askingsen may be 0 (a
// zero-priced order) — the fee is then the smallest amount that covers the
// acquirer's charge.
func GrossUp(askingSen, fixedSen, percentBP int64) (totalSen, feeSen int64) {
	if fixedSen < 0 {
		fixedSen = 0
	}
	if percentBP < 0 {
		percentBP = 0
	}
	if percentBP > MaxFeePercentBP {
		percentBP = MaxFeePercentBP
	}
	if askingSen < 0 {
		askingSen = 0
	}
	if askingSen > MaxAmountSen {
		askingSen = MaxAmountSen
	}
	if fixedSen == 0 && percentBP == 0 {
		// No fee configured: the buyer pays the asking price exactly. This is
		// the path that keeps every fee-disabled workspace byte-identical to
		// the pre-fee behaviour.
		return askingSen, 0
	}
	// Closed form for the continuous solution, floored: F ≥ (A + fixed)·10000/(10000 − bp).
	f := (askingSen + fixedSen) * 10000 / (10000 - percentBP)
	if f < fixedSen {
		f = fixedSen
	}
	// Walk UP to the first F whose net covers the asking price. The estimate
	// can only be low by a few sen, but the loop is written to terminate on the
	// predicate rather than on the estimate being right.
	for f-ProcessingFee(f, fixedSen, percentBP) < askingSen {
		f++
	}
	// Walk DOWN while the previous sen still covers it, so F is the MINIMUM.
	// Without this the buyer can be charged a sen more than necessary.
	for f > 0 && (f-1)-ProcessingFee(f-1, fixedSen, percentBP) >= askingSen {
		f--
	}
	return f, ProcessingFee(f, fixedSen, percentBP)
}

// FeeAcknowledgementText is the disclosure sentence a merchant must accept
// before a fee may reach a buyer: the price shown is the price paid, so charging
// the buyer on top of it has to be an explicit, recorded decision. It is served
// WITH the settings (REST and MCP) so an agent — or any client with no
// dictionary of its own — gets the disclosure the merchant is taking
// responsibility for.
//
// WHAT IT MUST SAY (card t_1a17d22d): the charge is JomForm's PLATFORM FEE, added
// on top of the price, and it is SEPARATE from the fee CHIP charges the merchant.
// The old sentence described the buyer as bearing the ACQUIRER's charge, which is
// no longer what this is: CHIP's own MDR stays the merchant's cost unless the
// merchant chooses to fund it out of this fee.
//
// IT IS ENGLISH, AND IT IS NOT THE SENTENCE THE MERCHANT READS. The dashboard
// renders its own dictionary arm for the same disclosure
// (web/src/components/i18nDict.ts "set.feesAck", whose `en` arm is this exact
// sentence and which carries all five locales). Serving the DASHBOARD's sentence
// from here is what put Malay on an English Settings page (t_541a31d5): a JSON
// call never carries the caller's language (issue #6177), so prose written on
// this side reaches every locale in one language. The two are pinned together by
// web/src/lib/errorMessageI18n.test.ts, which fails if the `en` arm and this
// constant drift apart.
//
// WHAT THE RECORD IS: only (workspace_id, acknowledged_by, acknowledged_at) is
// stored (migration 000082), never a copy of this text. Rewording the sentence
// therefore does not rewrite what any merchant already agreed to, and there is
// no stored prose to migrate.
const FeeAcknowledgementText = "I understand this platform fee is charged to the buyer on top of the price and is separate from the fee CHIP charges me."

// FeeRateLabel renders the stored charge for a method, e.g. "Card 2.00%",
// "FPX RM1.00", or "Card 2.00% + RM1.00". Used by the checkout chooser row and
// the settings surface; it reads ONLY the frozen values it is handed, so a later
// change cannot rewrite what a past buyer was shown.
//
// For a platform fee the percentage is 0, so it renders the flat amount
// ("FPX RM1.00") — the word "Platform fee" is the surrounding line's job
// (formLang's FeeLine, payments.FeeLine), not this label's.
func FeeRateLabel(method string, fixedSen, percentBP int64) string {
	label := FeeMethodLabel(method)
	var parts []string
	if percentBP > 0 {
		parts = append(parts, formatBasisPoints(percentBP)+"%")
	}
	if fixedSen > 0 {
		parts = append(parts, "RM"+formatSen(fixedSen))
	}
	if len(parts) == 0 {
		return label
	}
	return label + " " + joinPlus(parts)
}

// formatBasisPoints renders basis points as a percentage with as many decimals
// as needed and no trailing zeros: 100 → "1.00", 50 → "0.50", 1 → "0.01".
func formatBasisPoints(bp int64) string {
	if bp%10 == 0 {
		return fmt.Sprintf("%d.%d0", bp/100, (bp%100)/10)
	}
	return fmt.Sprintf("%d.%02d", bp/100, bp%100)
}

// formatSen renders integer sen as "1234.56" (no currency symbol).
func formatSen(sen int64) string {
	return fmt.Sprintf("%d.%02d", sen/100, sen%100)
}

func joinPlus(parts []string) string {
	return strings.Join(parts, " + ")
}
