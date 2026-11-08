// Processing-fee gross-up for CHIP purchases (buyer bears the acquirer's MDR).
//
// The merchant sets, PER CHIP PAYMENT METHOD, the rate their acquirer actually
// charges them (a fixed sen amount and/or basis points). When the fee is passed
// on to the buyer, CHIP still charges its fee on the amount the buyer ACTUALLY
// pays (F), not on the asking price (A). So adding `A × p` to A leaves the
// merchant short by exactly the fee charged on the fee itself — the
// WooCommerce plugin's naive `fee = total × rate` loses `p² × A` (10 sen on
// RM 1,000 at 1%).
//
// The correct answer is the smallest integer F with
//
//	F − fee(F) ≥ A        where fee(F) = fixed + round(F × bp / 10000)
//
// which for A = RM 1,000 at 1% is RM 1,010.10 (fee RM 10.10, merchant nets
// exactly RM 1,000.00). GrossUp guarantees `0 ≤ (F − fee(F)) − A ≤ 1` sen for
// every input: the ±1 is integer rounding, nothing more.
//
// All amounts are integer SEN (AGENTS.md). The rate is BASIS POINTS: 100 = 1%.
package payments

import (
	"fmt"
	"strings"
)

// FeeMethod* are the CHIP payment methods a per-method rate can be stored
// against. They are the aggregator keys the merchant sees in the dashboard and
// the same keys the CHIP purchase whitelist takes (payments.ResolveWhitelist
// expands `card` to the network identifiers CHIP expects).
const (
	FeeMethodFPX      = "fpx"
	FeeMethodCard     = "card"
	FeeMethodEWallet  = "ewallet"
	FeeMethodDuitNowQ = "duitnow_qr"
)

// FeeMethods lists every method a rate can be stored for, in the order the
// dashboard and the buyer-facing chooser display them.
var FeeMethods = []string{FeeMethodFPX, FeeMethodCard, FeeMethodEWallet, FeeMethodDuitNowQ}

// feeMethodLabels are the human labels shown to the BUYER in the checkout
// breakdown and on the receipt line ("Processing fee — Card 2.00%"). A raw
// method key must never reach a buyer; an unknown key falls back to the key
// itself rather than an empty label.
var feeMethodLabels = map[string]string{
	FeeMethodFPX:      "FPX",
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

// ProcessingFee is what the acquirer charges on amountSen, in integer sen:
// the fixed charge plus the percentage rounded HALF UP. Rounding half up is the
// single rule both this and the checkout page's client-side mirror use, because
// the page's number and the charge must agree to the sen.
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
// It is exact and O(1): the closed-form estimate is computed, then the result is
// walked to the true minimum. The walk is bounded by one or two sen in practice
// (the estimate is the real solution rounded down by the integer division).
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
// before a fee may reach a buyer: the price shown is the price paid, so passing
// the acquirer's charge on has to be an explicit, recorded decision. It is
// served WITH the settings (REST and MCP) so an agent — or any client with no
// dictionary of its own — gets the disclosure the merchant is taking
// responsibility for, and it names the card-issuer/acquirer rules that
// responsibility covers.
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
const FeeAcknowledgementText = "I understand this processing fee is passed on to the buyer and is subject to acquirer/card-issuer rules."

// FeeRateLabel renders the rate the buyer was charged, e.g. "Card 2.00%",
// "FPX RM1.00", or "Card 2.00% + RM1.00". Used by the receipt line and the
// checkout disclosure; it reads ONLY the frozen rate snapshot, so a later rate
// change cannot rewrite what a past buyer was shown.
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
