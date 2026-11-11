// Unit tests for the buyer-chosen payment method (card t_453489b9).
//
// WHAT IS PINNED HERE. The feature is "the buyer picks on JomForm, CHIP does not
// ask again", and it works through two levers — a narrowed
// payment_method_whitelist and a ?preferred= parameter on the URL CHIP returned.
// Each test below is one thing that would silently pay with the wrong method (or
// send the buyer to a URL that is not a page) if it broke:
//
//   - the identifier map (fpx / fpx_b2b1 / card / dnqr / shopee_pay …);
//   - an existing query string on the checkout URL surviving the append;
//   - a direct_post_url never gaining a query and never being handed out as a
//     GET redirect while a real checkout URL exists (PITFALL 1);
//   - an identifier we cannot map honestly getting NO parameter (PITFALL 2);
//   - the whitelist narrowing itself, so CHIP has nothing else to offer.
package payments

import (
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestPreferredParamMapping(t *testing.T) {
	cases := []struct {
		name string
		// group is the ROW the value is being resolved for. It is optional: an
		// empty group means "the group the first identifier belongs to", which is
		// how every pre-t_1d0198e3 case reads.
		group string
		ids   []string
		want  string
		ok    bool
	}{
		{"fpx", "", []string{"fpx"}, "fpx", true},
		{"fpx b2b1", "", []string{"fpx_b2b1"}, "fpx_b2b1", true},
		// Card t_1d0198e3: the two FPX products are separate groups, so a row
		// holding both identifiers still names ITS OWN one — the FPX B2C row can
		// never send preferred=fpx_b2b1 (or the buyer would land on B2B1), and the
		// B2B1 row can never send preferred=fpx.
		{"fpx row with both identifiers still prefers fpx", PayMethodFPX, []string{"fpx_b2b1", "fpx"}, "fpx", true},
		{"b2b1 row with both identifiers still prefers b2b1", PayMethodFPXB2B1, []string{"fpx_b2b1", "fpx"}, "fpx_b2b1", true},
		{"card networks collapse to card", "", []string{"visa", "mastercard", "maestro"}, "card", true},
		{"card from the aggregator id alone", "", []string{"card"}, "card", true},
		{"dnqr", "", []string{"dnqr"}, "dnqr", true},
		{"duitnow_qr is the legacy identifier", "", []string{"duitnow_qr"}, "duitnow_qr", true},
		{"dnqr wins over duitnow_qr", "", []string{"duitnow_qr", "dnqr"}, "dnqr", true},
		{"shopee_pay", "", []string{"shopee_pay"}, "shopee_pay", true},
		{"legacy razer_shopeepay is the same group", "", []string{"razer_shopeepay"}, "razer_shopeepay", true},
		{"shopee_pay wins over the legacy key", "", []string{"razer_shopeepay", "shopee_pay"}, "shopee_pay", true},
		// The wallets that need razer_bank_code=<display name> have no value
		// this checkout can name, so it sends none (PITFALL 2): the narrowed
		// whitelist is the lever.
		{"grabpay has no honest preferred value", "", []string{"razer_grabpay"}, "", false},
		{"boost has no honest preferred value", "", []string{"boost"}, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			group := tc.group
			if group == "" {
				group = PayMethodGroupOf(tc.ids[0])
			}
			got, ok := PreferredParam(group, tc.ids)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("PreferredParam(%q, %v) = (%q,%v), want (%q,%v)", group, tc.ids, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// PayMethodGroupOf is a test-only shortcut: the group the first identifier
// belongs to, so each case above reads as "the group this identifier is in".
func PayMethodGroupOf(id string) string {
	g, _ := PayMethodGroup(id)
	return g
}

func TestPayMethodGroupMapping(t *testing.T) {
	want := map[string]string{
		"fpx": PayMethodFPX,
		// Card t_1d0198e3: FPX B2B1 is its OWN group, not a member of the FPX
		// group — sharing one group is what made the picker ask the buyer
		// B2C-or-B2B1 once and CHIP ask again.
		"fpx_b2b1":        PayMethodFPXB2B1,
		"card":            PayMethodCard,
		"visa":            PayMethodCard,
		"mastercard":      PayMethodCard,
		"maestro":         PayMethodCard,
		"dnqr":            PayMethodDuitNowQR,
		"duitnow_qr":      PayMethodDuitNowQR,
		"shopee_pay":      PayMethodEWallet,
		"razer_shopeepay": PayMethodEWallet,
		"razer_grabpay":   PayMethodEWallet,
		"razer_tng":       PayMethodEWallet,
		"grabpay":         PayMethodEWallet,
		// Not groupable, so the picker offers no row for them and CHIP's own
		// page stays in charge of that method.
		"crypto_coin":     "",
		"mpgs_google_pay": "",
		"mpgs_apple_pay":  "",
	}
	for id, group := range want {
		got, ok := PayMethodGroup(id)
		if got != group {
			t.Errorf("PayMethodGroup(%q) = %q, want %q", id, got, group)
		}
		if group == "" && ok {
			t.Errorf("PayMethodGroup(%q) reported ok=true for an ungroupable identifier", id)
		}
	}
}

func TestCheckoutRedirectAppendsPreferredAndKeepsQuery(t *testing.T) {
	// A real CHIP checkout URL already carries a query; the old plugin appended
	// "?preferred=" blindly, which would produce two '?' and a broken URL here.
	got := CheckoutRedirect("https://gate.chip-in.asia/p/abc/?lang=ms&x=1", "", "fpx")
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("returned an unparseable URL: %v", err)
	}
	if u.Query().Get("preferred") != "fpx" {
		t.Fatalf("preferred= missing: %s", got)
	}
	if u.Query().Get("lang") != "ms" || u.Query().Get("x") != "1" {
		t.Fatalf("existing query lost: %s", got)
	}
	if strings.Count(got, "?") != 1 {
		t.Fatalf("the URL must keep exactly one '?', got %s", got)
	}
}

func TestCheckoutRedirectLeavesDirectPostURLUntouched(t *testing.T) {
	// PITFALL 1. A card-only whitelist makes CHIP answer with a direct_post_url,
	// which is the POST target for its hosted card fields — not a page. BITE
	// PROOF: delete the `directPostURL != "" && base == directPostURL` guard in
	// CheckoutRedirect and this test fails with
	// ".../direct_post/?preferred=card" instead of the untouched URL.
	const direct = "https://gate.chip-in.asia/direct_post/abc/"
	if got := CheckoutRedirect(direct, direct, "card"); got != direct {
		t.Fatalf("direct_post_url was modified: %q", got)
	}
	// When CHIP also returned the real checkout page, the buyer goes THERE and
	// the direct-post URL is still never a redirect target.
	got := CheckoutRedirect("https://gate.chip-in.asia/p/abc/", direct, "card")
	if strings.Contains(got, "direct_post") {
		t.Fatalf("redirected to a direct_post_url: %q", got)
	}
	if !strings.HasSuffix(got, "?preferred=card") {
		t.Fatalf("card checkout should pre-select the card method: %q", got)
	}
}

func TestCheckoutRedirectNeverInventsAParam(t *testing.T) {
	// PITFALL 2: an e-wallet we cannot name (grabpay) gets the narrowed
	// whitelist and NO parameter.
	checkout := "https://gate.chip-in.asia/p/abc/"
	if got := CheckoutRedirect(checkout, "", ""); got != checkout {
		t.Fatalf("no preferred value must leave the URL byte-identical, got %q", got)
	}
	// A URL that is not absolute (a fake in a test, a misconfigured base) is
	// returned untouched rather than mangled into "?preferred=" garbage.
	if got := CheckoutRedirect("not a url", "", "fpx"); got != "not a url" {
		t.Fatalf("a non-absolute URL must pass through untouched, got %q", got)
	}
}

func TestNarrowWhitelist(t *testing.T) {
	// ResolveWhitelist has already expanded 'card' to the networks, so this is
	// the shape the purchase path hands us.
	got := NarrowWhitelist(PayMethodCard, []string{"fpx", "visa", "mastercard", "maestro"})
	if strings.Join(got, ",") != "visa,mastercard,maestro" {
		t.Fatalf("card narrowing = %v", got)
	}
	// The aggregator key itself means the whole group (fee sales store exactly
	// that: feeWhitelist returns []string{"card"}).
	got = NarrowWhitelist(PayMethodEWallet, []string{"ewallet"})
	if !containsString(got, "shopee_pay") {
		t.Fatalf("ewallet aggregator must expand to its identifiers, got %v", got)
	}
	// fpx is an identifier, not an aggregator: narrowing must NOT add fpx_b2b1,
	// which is a different product (and a different fee) the merchant did not
	// enable — card t_1d0198e3.
	got = NarrowWhitelist(PayMethodFPX, []string{"fpx", "fpx_b2b1"})
	if strings.Join(got, ",") != "fpx" {
		t.Fatalf("fpx narrowing = %v", got)
	}
	// ...and the FPX B2B1 group narrows to ITS identifier alone, so CHIP cannot
	// offer the personal product to a corporate payer.
	got = NarrowWhitelist(PayMethodFPXB2B1, []string{"fpx", "fpx_b2b1"})
	if strings.Join(got, ",") != "fpx_b2b1" {
		t.Fatalf("fpx_b2b1 narrowing = %v", got)
	}
	// A whitelist holding only the other FPX product does not enable this one.
	if got := NarrowWhitelist(PayMethodFPXB2B1, []string{"fpx"}); len(got) != 0 {
		t.Fatalf("fpx_b2b1 must not be reachable from a B2C-only whitelist, got %v", got)
	}
	// An unrestricted checkout narrows FPX B2B1 to that identifier too, never to
	// the whole FPX family.
	got = NarrowWhitelist(PayMethodFPXB2B1, nil)
	if strings.Join(got, ",") != "fpx_b2b1" {
		t.Fatalf("empty whitelist b2b1 narrowing = %v", got)
	}
	// An unrestricted checkout (empty whitelist) narrows to the whole group;
	// leaving it empty would let CHIP offer every method again.
	got = NarrowWhitelist(PayMethodDuitNowQR, nil)
	if strings.Join(got, ",") != "dnqr,duitnow_qr" {
		t.Fatalf("empty whitelist narrowing = %v", got)
	}
	// A group the merchant disabled yields nothing — the caller refuses.
	if got := NarrowWhitelist(PayMethodDuitNowQR, []string{"fpx"}); len(got) != 0 {
		t.Fatalf("disabled group must narrow to nothing, got %v", got)
	}
	if got := NarrowWhitelist("unknown", []string{"fpx"}); got != nil {
		t.Fatalf("unknown group must narrow to nil, got %v", got)
	}
}

func TestPayOptionsFromWhitelist(t *testing.T) {
	// Only what the workspace enables, in the mockup's order.
	opts := PayOptions([]string{"fpx", "shopee_pay", "crypto_coin"})
	if len(opts) != 2 || opts[0].Method != PayMethodFPX || opts[1].Method != PayMethodEWallet {
		t.Fatalf("options = %+v", opts)
	}
	if opts[1].Preferred != "shopee_pay" {
		t.Fatalf("e-wallet preferred = %q", opts[1].Preferred)
	}
	// An unrestricted checkout offers every group; the buyer's pick is what
	// narrows the request. Card t_1d0198e3 made FPX two of those groups.
	if got := PayOptions(nil); len(got) != 5 {
		t.Fatalf("empty whitelist must offer five groups, got %d", len(got))
	}
	// card expands to its networks, and the redirect value stays `card`.
	opts = PayOptions([]string{"card"})
	if len(opts) != 1 || opts[0].Method != PayMethodCard || opts[0].Preferred != "card" {
		t.Fatalf("card options = %+v", opts)
	}
	if strings.Join(opts[0].Identifiers, ",") != "visa,mastercard,maestro" {
		t.Fatalf("card identifiers = %v", opts[0].Identifiers)
	}
}

func TestValidatePayMethod(t *testing.T) {
	wl := []string{"fpx", "card"}
	if err := ValidatePayMethod("", wl); err != nil {
		t.Fatalf("an empty method (an older page) must stay valid: %v", err)
	}
	if err := ValidatePayMethod("fpx", wl); err != nil {
		t.Fatalf("fpx is enabled: %v", err)
	}
	if err := ValidatePayMethod("visa", wl); err != nil {
		t.Fatalf("a card network is part of an enabled group: %v", err)
	}
	// Disabled group, unknown value, and an identifier with no group.
	for _, bad := range []string{"duitnow_qr", "crypto_coin", "bitcoin"} {
		if err := ValidatePayMethod(bad, wl); !errors.Is(err, ErrPayMethodUnavailable) {
			t.Fatalf("ValidatePayMethod(%q) = %v, want ErrPayMethodUnavailable", bad, err)
		}
	}
	// Card t_1d0198e3: a B2C-only whitelist must refuse the corporate product —
	// an existing ["fpx"] keeps meaning FPX B2C, never silently B2B1.
	if err := ValidatePayMethod("fpx_b2b1", []string{"fpx"}); !errors.Is(err, ErrPayMethodUnavailable) {
		t.Fatalf("ValidatePayMethod(fpx_b2b1, [fpx]) = %v, want ErrPayMethodUnavailable", err)
	}
	// Enabling both makes both choices valid.
	if err := ValidatePayMethod("fpx_b2b1", []string{"fpx", "fpx_b2b1"}); err != nil {
		t.Fatalf("both FPX products enabled must accept fpx_b2b1: %v", err)
	}
	// An unrestricted checkout enables everything it can label.
	if err := ValidatePayMethod("duitnow_qr", nil); err != nil {
		t.Fatalf("an empty whitelist must accept a group we can label: %v", err)
	}
}

// TestFPXIsTwoBuyerChoices is the core of card t_1d0198e3: the picker offers FPX
// B2C and FPX B2B1 as TWO rows, each carrying only its own CHIP identifier and its
// own ?preferred= value.
//
// BITE PROOF (run and observed on 2026-11-11): fold `fpx_b2b1` back into
// payMethodIdentifiers[PayMethodFPX] and these go RED —
//
//	TestFPXIsTwoBuyerChoices          "first FPX row … wanted the B2C product only"
//	TestNarrowWhitelist               "fpx narrowing = [fpx fpx_b2b1]"
//	TestPayMethodNarrowsWhitelistAndPrefersFPX (route level)
//	TestB2B1ChoiceNarrowsToB2B1Alone  "whitelist = fpx,fpx_b2b1, want only fpx_b2b1"
//	TestPickerHidesB2B1WhenChipDoesNotListIt "posting b2b1 status = 201, want 400"
//
// The two ROWS still render under that fold — the regression is the narrowing:
// the purchase keeps offering the other product, which is exactly what lets CHIP
// ask the buyer B2C-or-B2B1 again after the picker already asked.
func TestFPXIsTwoBuyerChoices(t *testing.T) {
	var fpx []PayOption
	for _, o := range PayOptions(nil) {
		if IsFPXPayMethod(o.Method) {
			fpx = append(fpx, o)
		}
	}
	if len(fpx) != 2 {
		t.Fatalf("an unrestricted checkout must offer FPX B2C and FPX B2B1 as two rows, got %d: %+v", len(fpx), fpx)
	}
	if fpx[0].Method != PayMethodFPX || strings.Join(fpx[0].Identifiers, ",") != "fpx" || fpx[0].Preferred != "fpx" {
		t.Fatalf("first FPX row = %+v, want the B2C product only (fpx/fpx)", fpx[0])
	}
	if fpx[1].Method != PayMethodFPXB2B1 || strings.Join(fpx[1].Identifiers, ",") != "fpx_b2b1" || fpx[1].Preferred != "fpx_b2b1" {
		t.Fatalf("second FPX row = %+v, want the B2B1 product only (fpx_b2b1/fpx_b2b1)", fpx[1])
	}
	// A merchant who ticked only one product sees only that row (the migration
	// rule: an existing ["fpx"] keeps working and does NOT become B2B1).
	onlyB2C := PayOptions([]string{"fpx"})
	if len(onlyB2C) != 1 || onlyB2C[0].Method != PayMethodFPX {
		t.Fatalf(`whitelist ["fpx"] must render exactly the FPX B2C row, got %+v`, onlyB2C)
	}
	onlyB2B1 := PayOptions([]string{"fpx_b2b1"})
	if len(onlyB2B1) != 1 || onlyB2B1[0].Method != PayMethodFPXB2B1 {
		t.Fatalf(`whitelist ["fpx_b2b1"] must render exactly the FPX B2B1 row, got %+v`, onlyB2B1)
	}
	// Picking one narrows the CHIP request to that identifier alone, so CHIP
	// cannot offer the other product on its own page.
	if got := NarrowWhitelist(PayMethodFPXB2B1, []string{"fpx", "fpx_b2b1"}); strings.Join(got, ",") != "fpx_b2b1" {
		t.Fatalf("B2B1 purchase whitelist = %v, want only fpx_b2b1", got)
	}
	if got := NarrowWhitelist(PayMethodFPX, []string{"fpx", "fpx_b2b1"}); strings.Join(got, ",") != "fpx" {
		t.Fatalf("B2C purchase whitelist = %v, want only fpx", got)
	}
}

func TestPayMethodKeysIsDiagnosticOnly(t *testing.T) {
	// The helper exists so a log line says "fpx,card" for a stored whitelist,
	// including legacy keys, and never panics on junk.
	got := PayMethodKeys([]string{"visa", "fpx", "crypto_coin", ""})
	if strings.Join(got, ",") != "card,fpx" {
		t.Fatalf("keys = %v", got)
	}
}
