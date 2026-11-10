// The buyer's payment-method picker (card t_453489b9).
//
// The approved mockup replaces the demo's decorative chips UNDER the Pay button
// with a real radio group ABOVE it, and it is a real feature rather than
// decoration: the group the buyer picks narrows the CHIP purchase to that method
// and pre-selects it on CHIP's page, so the buyer is not asked the same question
// twice (payments.paymethod.go owns that half).
//
// ONE resolution, two surfaces. The menu checkout sheet (menu.go) and the
// custom-form payment block (render_blocks.go) render the SAME view from this
// file, and the order core validates the submitted group against the SAME
// whitelist (payments.CheckBuyerPayMethod), so the rows a buyer sees, the value
// the page posts and the whitelist the purchase is created with cannot drift.
//
// The rows are DERIVED, never a hardcoded array: payments.PayOptions filters the
// merchant's effective whitelist (form/chip-feed override wins, then the
// account's), so a workspace that enabled only FPX shows only FPX. The four
// names in the mockup are the four GROUPS this product can label — a CHIP method
// that belongs to none of them (crypto_coin, Google/Apple Pay) gets no row and
// stays on CHIP's own page rather than being mislabelled into one.
package httpapi

import (
	"context"
	"strings"

	"github.com/wzul/jomform/api/internal/buyertext"
	"github.com/wzul/jomform/api/internal/payments"
)

// errPayMethodUnavailable refuses an order whose chosen payment method this
// checkout cannot honour. Refused (400) instead of silently falling back to the
// merchant's whole whitelist: a fallback would pay with a method the buyer never
// picked (`payments.ErrPayMethodUnavailable` is its payments-layer twin).
var errPayMethodUnavailable = buyertext.New(buyertext.CodeOrderPayMethodUnavailable, nil)

// payMethodChoiceView is one row of the picker.
//
// FixedSen/PercentBP ride on the row as data attributes so the page can price
// the buyer-borne processing fee for the CHOSEN method without a reload — the
// same numbers the server will price with (card t_135c3c72). Zero/zero means
// this method carries no fee.
type payMethodChoiceView struct {
	Key       string // fpx | duitnow_qr | card | ewallet — the posted value
	Label     string // "FPX", "DuitNow QR", "Debit / credit card", "e-Wallet"
	Desc      string // the small second line ("Scan with any bank app")
	FixedSen  int64
	PercentBP int64
	RateLabel string // "Card 2.00%" — the fee this row will add, "" when none
	Selected  bool
}

// payMethodChoices resolves the picker for a checkout.
//
// Returns nil when the merchant's checkout offers no group this product can
// label: then the page renders no picker and posts no pay_method, which is
// exactly the pre-feature behaviour (CHIP's own page asks). One group still
// renders — the row shows the buyer what they are about to pay with and the Pay
// button says so — and it is preselected, so it costs no click.
func (h *PublicHandlers) payMethodChoices(ctx context.Context, accountID, formID string, ls langStrings) []payMethodChoiceView {
	if h.Pay == nil || accountID == "" {
		return nil
	}
	// The form's own CHIP override (a per-form brand/key + whitelist) decides
	// the whitelist for its checkouts, exactly as the purchase does. A menu's
	// checkout is attributed to the menu FORM, so the same read serves both.
	//
	// STATED LIMITATION: a form's chip_feeds (issue #208) narrow the brand and
	// whitelist per SUBMISSION, matched against the buyer's answers — those
	// answers do not exist when the page is rendered, so the picker cannot see
	// them. A feed that enables less than the form override does can therefore
	// offer a row the purchase then refuses (ErrPayMethodUnavailable, 400 with
	// "that payment method is not available"), which is the honest outcome: the
	// buyer learns before paying rather than being switched silently.
	var ov *payments.ChipOverride
	if formID != "" && h.Forms != nil {
		if b, s, ef, wl, err := h.Forms.ChipOverrideFor(ctx, accountID, formID); err == nil && b != "" && s != "" {
			ov = &payments.ChipOverride{BrandID: b, SecretEnc: s, EmailFallback: ef, MethodWhitelist: wl}
		}
	}
	wl, err := h.Pay.EffectivePayWhitelist(ctx, accountID, ov)
	if err != nil {
		// A whitelist we cannot read is not a reason to hide the picker behind
		// an error page: render what an unrestricted checkout shows. The order
		// route still validates the choice against the live list.
		wl = nil
	}
	opts := payments.PayOptions(wl)
	if len(opts) == 0 {
		return nil
	}
	fees := h.feeOptions(ctx, accountID, formID)
	out := make([]payMethodChoiceView, 0, len(opts))
	for i, o := range opts {
		v := payMethodChoiceView{
			Key:      o.Method,
			Label:    payMethodLabel(ls, o.Method),
			Desc:     payMethodDesc(ls, o),
			Selected: i == 0,
		}
		for _, f := range fees {
			if f.Method == o.Method {
				v.FixedSen, v.PercentBP, v.RateLabel = f.FixedSen, f.PercentBP, f.RateLabel
				break
			}
		}
		out = append(out, v)
	}
	return out
}

// payMethodLabel is the row's own name.
//
// FPX and DuitNow QR are SCHEME names: they read the same in every locale, so
// they are one word in the dictionary rather than a translation a translator
// could "fix". Card and e-Wallet are ordinary nouns and are translated.
func payMethodLabel(ls langStrings, method string) string {
	switch method {
	case payments.PayMethodFPX:
		return "FPX"
	case payments.PayMethodDuitNowQR:
		return "DuitNow QR"
	case payments.PayMethodCard:
		return ls.PayCardLabel
	case payments.PayMethodEWallet:
		return ls.PayEWalletLabel
	}
	return method
}

// payMethodDesc is the small line under the row's name. It says what the group
// MEANS, and for e-wallets it names only the wallets this checkout actually
// enabled — the mockup's "GrabPay · Boost · ShopeePay" was illustrative, and
// printing a wallet the merchant did not enable is the same broken promise the
// decorative chips made.
func payMethodDesc(ls langStrings, o payments.PayOption) string {
	switch o.Method {
	case payments.PayMethodFPX:
		return ls.PayFpxDesc
	case payments.PayMethodDuitNowQR:
		return ls.PayDuitNowDesc
	case payments.PayMethodCard:
		return ls.PayCardDesc
	case payments.PayMethodEWallet:
		// Name the wallets this checkout enabled — but only when there are few
		// enough to read. An UNRESTRICTED workspace lists every wallet CHIP
		// offers, which is a wall of brands in a 44px row; it gets the generic
		// sentence instead, so the row never promises more than the shop's own
		// settings say.
		if brands := walletBrands(o.Identifiers); brands != "" {
			return brands
		}
		return ls.PayEWalletDesc
	}
	return ""
}

// walletBrands lists the buyer-facing brand names of the enabled e-wallet
// identifiers, de-duplicated and in the option's preference order (shopee_pay
// and its legacy razer_shopeepay are ONE brand, so the buyer reads it once).
// More than three brands returns "" — the caller shows the generic sentence
// rather than a list too long for the row.
func walletBrands(ids []string) string {
	seen := map[string]bool{}
	var out []string
	for _, id := range ids {
		brand := payments.IdentifierBrand(id)
		if brand == "" || seen[brand] {
			continue
		}
		seen[brand] = true
		out = append(out, brand)
	}
	if len(out) == 0 || len(out) > 3 {
		return ""
	}
	return strings.Join(out, " · ")
}
