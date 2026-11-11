// The buyer's chosen payment method (card t_453489b9).
//
// JomForm asks the buyer HOW they want to pay on its own page and then hands
// CHIP the answer, so the buyer is not asked the same question twice: CHIP's
// hosted page is skipped for the chosen method and opens straight on it. Two
// levers do that, and this file owns both of them:
//
//  1. the purchase's payment_method_whitelist, narrowed to the chosen GROUP —
//     whatever a method's ?preferred= support is, a whitelist holding only one
//     group leaves CHIP nothing else to offer;
//  2. a ?preferred=<identifier> parameter appended to the URL CHIP returned.
//
// The mapping and the two pitfalls copied here are chip-for-woocommerce's
// `bypass_chip()` (includes/class-chip-woocommerce-gateway.php), which is the
// reference implementation of this feature:
//
//	fpx                      -> preferred=fpx            (+ fpx_bank_code= when a bank was chosen)
//	fpx_b2b1                 -> preferred=fpx_b2b1
//	card (visa/mastercard/maestro) -> preferred=card
//	dnqr / duitnow_qr        -> preferred=dnqr            (dnqr wins over the legacy duitnow_qr)
//	shopee_pay / razer_shopeepay -> preferred=<whichever CHIP resolved>
//	crypto_coin, mpgs_google_pay, mpgs_apple_pay -> preferred=<the identifier>
//
// FPX IS TWO PRODUCTS, NOT ONE GROUP (card t_1d0198e3). CHIP lists `fpx` (FPX
// B2C, personal retail online banking) and `fpx_b2b1` (FPX B2B1, corporate online
// banking) as separate methods with separate minimums and — for the merchant —
// separate fee structures. While both lived under one `fpx` group, the picker
// offered ONE row and CHIP then asked the buyer the same question again on its
// own page, and no fee rate could tell the two apart. Each is now its own group
// key, so the narrowing below holds THAT identifier and CHIP has nothing else to
// offer.
//
// PITFALL 1 — never append ?preferred= to a `direct_post_url`. With a card-only
// whitelist CHIP answers with one, and it is a POST target for its hosted card
// fields, not a page: appending a query corrupts it. CheckoutRedirect below
// returns that URL untouched and, when CHIP supplied a real checkout URL as
// well, redirects there instead — exactly what the plugin does on a plain
// redirect surface (its `order-pay` branch), which is what JomForm is.
//
// PITFALL 2 — `preferred` is a hint, the whitelist is the guarantee. A method
// with no ?preferred= value still opens alone when the whitelist contains only
// its group, so the narrowing is done in both cases and an identifier we cannot
// name honestly gets NO param rather than an invented one.
package payments

import (
	"errors"
	"net/url"
	"sort"
	"strings"
)

// The buyer-facing payment-method groups. These are the merchant's own
// aggregator keys (the ones the processing-fee rates are stored against and the
// ones the dashboard's whitelist multiselect offers), so one key describes a
// method on the page, in the sale's fee snapshot and in the CHIP request.
//
// PayMethodFPX and PayMethodFPXB2B1 are TWO buyer choices (card t_1d0198e3):
// personal (B2C) and corporate (B2B1) FPX, each with its own fee rate.
const (
	PayMethodFPX       = "fpx"
	PayMethodFPXB2B1   = "fpx_b2b1"
	PayMethodDuitNowQR = "duitnow_qr"
	PayMethodCard      = "card"
	PayMethodEWallet   = "ewallet"
)

// PayMethods is every group the picker may render, in the order the approved
// mockup shows them (FPX B2C, FPX B2B1, DuitNow QR, card, e-wallet).
var PayMethods = []string{PayMethodFPX, PayMethodFPXB2B1, PayMethodDuitNowQR, PayMethodCard, PayMethodEWallet}

// IsFPXPayMethod reports whether a group key is one of the two FPX products.
// Used where a rule is about "the FPX rows" as a set (their availability at
// CHIP) rather than about one of them.
func IsFPXPayMethod(group string) bool {
	return group == PayMethodFPX || group == PayMethodFPXB2B1
}

// ErrPayMethodUnavailable refuses a choice the checkout cannot honour. It is a
// BUYER error (400), never a silent fallback: falling back to the merchant's
// whole whitelist would let the buyer pay with a method they did not pick.
var ErrPayMethodUnavailable = errors.New("payments: payment method unavailable")

// payMethodIdentifiers lists, per group, the CHIP identifiers that make it up,
// most-preferred first. The first entry that is actually present decides the
// ?preferred= value (see PreferredParam).
//
// `fpx`, `fpx_b2b1` and `duitnow_qr` are identifiers as well as group keys; `card`
// and `ewallet` are aggregators that exist only on our side (CHIP knows the
// networks and the individual wallets). fpx and fpx_b2b1 are DELIBERATELY one
// identifier each: they are two products, and grouping them is what let CHIP
// re-ask the buyer B2C-or-B2B1 after the picker had already asked.
var payMethodIdentifiers = map[string][]string{
	PayMethodFPX:       {"fpx"},
	PayMethodFPXB2B1:   {"fpx_b2b1"},
	PayMethodDuitNowQR: {"dnqr", "duitnow_qr"},
	PayMethodCard:      {"visa", "mastercard", "maestro"},
	PayMethodEWallet: {
		"shopee_pay", "razer_shopeepay",
		"razer_grabpay", "grabpay", "razer_tng", "razer_atome", "razer_maybankqr", "boost",
	},
}

// payMethodAggregator reports whether a group key is a name of our own (card,
// ewallet) rather than a CHIP identifier. An aggregator key in a whitelist means
// "the whole group" — the merchant did not choose one identifier.
func payMethodAggregator(group string) bool {
	return group == PayMethodCard || group == PayMethodEWallet
}

// PayMethodGroup maps a CHIP payment-method identifier to the buyer-facing
// group. ok is false for an identifier no group covers (crypto_coin,
// mpgs_google_pay, mpgs_apple_pay): the picker has no honest row to put it in,
// so it is left to CHIP's own page rather than mislabelled.
func PayMethodGroup(id string) (group string, ok bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		return "", false
	}
	for _, g := range PayMethods {
		for _, member := range payMethodIdentifiers[g] {
			if member == id {
				return g, true
			}
		}
	}
	// The aggregator keys are accepted as identifiers too, so a client may send
	// what the picker showed.
	if payMethodAggregator(id) {
		return id, true
	}
	return "", false
}

// KnownPayMethod reports whether m is a group this package can label, narrow
// and redirect for.
func KnownPayMethod(m string) bool {
	_, ok := PayMethodGroup(m)
	return ok
}

// PayOption is one row of the picker: a group, the identifiers this checkout
// actually allows for it, and the ?preferred= value to send ("" = rely on the
// narrowed whitelist alone).
type PayOption struct {
	Method      string
	Identifiers []string
	Preferred   string
}

// PayOptions resolves the picker's rows from the merchant's effective whitelist:
// only the groups the workspace enables appear, in PayMethods order. An EMPTY
// whitelist means "the merchant restricted nothing", so every group is offered
// and the buyer's pick is what narrows the CHIP request — the alternative
// (offer nothing) would hide the picker from exactly the merchants who never
// configured one.
//
// The list is filtered, never invented: an identifier that maps to no group
// cannot produce a row.
//
// This function is pure. The brand's LIVE listing of the two FPX products is
// applied on top of it by Service.CheckoutPayOptions, which is what the picker
// calls (card t_1d0198e3): a whitelist cannot say whether CHIP enabled FPX B2B1
// for the brand, and an unrestricted workspace would otherwise offer a row CHIP
// does not have.
func PayOptions(whitelist []string) []PayOption {
	ids := make(map[string]bool, len(whitelist))
	for _, m := range whitelist {
		if m = strings.ToLower(strings.TrimSpace(m)); m != "" {
			ids[m] = true
		}
	}
	out := make([]PayOption, 0, len(PayMethods))
	for _, g := range PayMethods {
		members := payMethodIdentifiers[g]
		var have []string
		if len(ids) == 0 || (payMethodAggregator(g) && ids[g]) {
			have = append(have, members...)
		} else {
			for _, m := range members {
				if ids[m] {
					have = append(have, m)
				}
			}
		}
		if len(have) == 0 {
			continue
		}
		pref, _ := PreferredParam(g, have)
		out = append(out, PayOption{Method: g, Identifiers: have, Preferred: pref})
	}
	return out
}

// PayOptionFor returns the enabled option for a chosen group.
func PayOptionFor(whitelist []string, method string) (PayOption, bool) {
	method = strings.ToLower(strings.TrimSpace(method))
	if method == "" {
		return PayOption{}, false
	}
	if g, ok := PayMethodGroup(method); ok {
		method = g
	} else {
		return PayOption{}, false
	}
	for _, o := range PayOptions(whitelist) {
		if o.Method == method {
			return o, true
		}
	}
	return PayOption{}, false
}

// ValidatePayMethod refuses (ErrPayMethodUnavailable) a group the merchant's
// whitelist does not enable, or a value that is not a group at all. An empty
// method is always valid: it means "the buyer's page did not ask" (an older
// page, an embed), and the request then behaves exactly as it did before this
// feature — CHIP shows its own method screen.
func ValidatePayMethod(method string, whitelist []string) error {
	if strings.TrimSpace(method) == "" {
		return nil
	}
	if _, ok := PayOptionFor(whitelist, method); !ok {
		return ErrPayMethodUnavailable
	}
	return nil
}

// PreferredParam returns the ?preferred= value for a group given the identifiers
// this checkout allows. ok=false means "we cannot name it honestly", and the
// caller then sends no parameter at all — the narrowed whitelist is what makes
// CHIP open on that method (pitfall 2).
func PreferredParam(group string, identifiers []string) (value string, ok bool) {
	has := make(map[string]bool, len(identifiers))
	for _, id := range identifiers {
		has[strings.ToLower(strings.TrimSpace(id))] = true
	}
	pick := func(order ...string) (string, bool) {
		for _, id := range order {
			if has[id] {
				return id, true
			}
		}
		return "", false
	}
	switch group {
	case PayMethodFPX:
		return pick("fpx")
	case PayMethodFPXB2B1:
		// Its own value, never the FPX group's: `preferred=fpx_b2b1` is what
		// makes CHIP open on FPX B2B1 instead of falling back to B2C.
		return pick("fpx_b2b1")
	case PayMethodDuitNowQR:
		return pick("dnqr", "duitnow_qr")
	case PayMethodCard:
		// CHIP knows the card group by the identifier `card`, never by a
		// network name: the whitelist carries visa/mastercard/maestro, the
		// redirect parameter is `card`. The aggregator key itself counts too
		// (a fee sale stores exactly []string{"card"}).
		if has[PayMethodCard] {
			return "card", true
		}
		for _, id := range payMethodIdentifiers[PayMethodCard] {
			if has[id] {
				return "card", true
			}
		}
		return "", false
	case PayMethodEWallet:
		// Only the two Shopee keys map to a redirect parameter we know: the
		// other wallets need razer_bank_code=<display name> as well, and that
		// display name is not something this checkout has. They keep the
		// narrowed whitelist and get no parameter (never invent one).
		return pick("shopee_pay", "razer_shopeepay")
	}
	return "", false
}

// NarrowWhitelist reduces a resolved CHIP whitelist to the identifiers of one
// group, so CHIP cannot offer anything else (levers 1). The result is in the
// group's own preference order and de-duplicated; an empty result means the
// group is not enabled at all.
//
// An EMPTY resolved whitelist means the merchant restricted nothing: the group's
// whole identifier set is the correct narrowing, and leaving it empty would
// defeat the purpose (CHIP would offer everything).
func NarrowWhitelist(group string, resolved []string) []string {
	group = strings.ToLower(strings.TrimSpace(group))
	members, ok := payMethodIdentifiers[group]
	if !ok {
		return nil
	}
	if len(resolved) == 0 {
		return append([]string{}, members...)
	}
	have := make(map[string]bool, len(resolved))
	aggregate := false
	for _, m := range resolved {
		m = strings.ToLower(strings.TrimSpace(m))
		have[m] = true
		if payMethodAggregator(group) && m == group {
			aggregate = true
		}
	}
	if aggregate {
		// The whitelist names the group itself ("card", "ewallet"): the
		// merchant enabled the whole group, so every identifier in it.
		return append([]string{}, members...)
	}
	out := make([]string, 0, len(members))
	for _, m := range members {
		if have[m] {
			out = append(out, m)
		}
	}
	// An identifier the group owns but this package does not list (a wallet CHIP
	// added later, still tagged with a known prefix) is kept: dropping it would
	// refuse a method the merchant enabled.
	for _, m := range resolved {
		m = strings.ToLower(strings.TrimSpace(m))
		if m == "" || containsString(members, m) || containsString(out, m) {
			continue
		}
		if groupOfPrefix(m) == group {
			out = append(out, m)
		}
	}
	return out
}

// groupOfPrefix maps a CHIP identifier to a group by its prefix. It is only used
// to keep an identifier this package does not enumerate in its own group's
// narrowed list; it never decides a ?preferred= value.
func groupOfPrefix(id string) string {
	switch {
	case strings.HasPrefix(id, "razer_"), id == "grabpay", id == "boost":
		return PayMethodEWallet
	}
	return ""
}

func containsString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// CheckoutRedirect returns the URL the buyer's browser must visit after the
// purchase is created: CHIP's checkout page with the chosen method
// pre-selected.
//
// PITFALL 1 is enforced here, in one place, for every surface:
//   - a `direct_post_url` is NEVER modified and never returned as a redirect
//     when CHIP also supplied a checkout URL. It is a POST target for hosted
//     card fields; a GET to it with a query appended is a corrupted request,
//     and there is no page behind it a buyer can use;
//   - when the only URL CHIP supplied IS the direct-post URL, it is returned
//     untouched (no query added) — the caller's buyer still lands somewhere
//     CHIP defined rather than on a URL this package mangled.
//
// An empty preferred ("we cannot name the method") returns the untouched
// checkout URL: the narrowed whitelist already did the work.
func CheckoutRedirect(checkoutURL, directPostURL, preferred string) string {
	checkoutURL = strings.TrimSpace(checkoutURL)
	directPostURL = strings.TrimSpace(directPostURL)
	base := checkoutURL
	if base == "" {
		base = directPostURL
	}
	if base == "" {
		return ""
	}
	// Never touch (or hand out as a redirect) a URL whose only purpose is a
	// card-data POST. Comparing the strings is exact: CHIP returns them as two
	// distinct fields, and when it returns one URL both fields hold it.
	if directPostURL != "" && base == directPostURL {
		return base
	}
	preferred = strings.TrimSpace(preferred)
	if preferred == "" {
		return base
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return base
	}
	q := u.Query()
	q.Set("preferred", preferred)
	u.RawQuery = q.Encode()
	return u.String()
}

// identifierBrands are the buyer-facing brand names of CHIP identifiers that
// have one. They are BRAND names, not translated copy: "GrabPay" is GrabPay in
// every locale, which is why they are a table here rather than a dictionary
// entry a translator could "fix".
//
// Only identifiers whose product a buyer would recognise by name are listed; an
// e-wallet that is not here contributes nothing to the row's description rather
// than a raw wire value (razer_tng is never printed to a buyer).
var identifierBrands = map[string]string{
	"shopee_pay":      "ShopeePay",
	"razer_shopeepay": "ShopeePay", // the legacy identifier — ONE brand, printed once
	"razer_grabpay":   "GrabPay",
	"grabpay":         "GrabPay",
	"razer_tng":       "Touch 'n Go",
	"razer_atome":     "Atome",
	"razer_maybankqr": "Maybank QRPay",
	"boost":           "Boost",
}

// IdentifierBrand returns the brand name of a CHIP identifier, or "" when it has
// none this product can print.
func IdentifierBrand(id string) string {
	return identifierBrands[strings.ToLower(strings.TrimSpace(id))]
}

// PayMethodKeys returns the group keys of a whitelist, sorted — the diagnostic
// view used by tests and logs.
func PayMethodKeys(whitelist []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range whitelist {
		if g, ok := PayMethodGroup(m); ok && !seen[g] {
			seen[g] = true
			out = append(out, g)
		}
	}
	sort.Strings(out)
	return out
}
