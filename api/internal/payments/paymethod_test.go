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
		ids  []string
		want string
		ok   bool
	}{
		{"fpx", []string{"fpx"}, "fpx", true},
		{"fpx b2b1", []string{"fpx_b2b1"}, "fpx_b2b1", true},
		{"fpx prefers fpx over b2b1", []string{"fpx_b2b1", "fpx"}, "fpx", true},
		{"card networks collapse to card", []string{"visa", "mastercard", "maestro"}, "card", true},
		{"card from the aggregator id alone", []string{"card"}, "card", true},
		{"dnqr", []string{"dnqr"}, "dnqr", true},
		{"duitnow_qr is the legacy identifier", []string{"duitnow_qr"}, "duitnow_qr", true},
		{"dnqr wins over duitnow_qr", []string{"duitnow_qr", "dnqr"}, "dnqr", true},
		{"shopee_pay", []string{"shopee_pay"}, "shopee_pay", true},
		{"legacy razer_shopeepay is the same group", []string{"razer_shopeepay"}, "razer_shopeepay", true},
		{"shopee_pay wins over the legacy key", []string{"razer_shopeepay", "shopee_pay"}, "shopee_pay", true},
		// The wallets that need razer_bank_code=<display name> have no value
		// this checkout can name, so it sends none (PITFALL 2): the narrowed
		// whitelist is the lever.
		{"grabpay has no honest preferred value", []string{"razer_grabpay"}, "", false},
		{"boost has no honest preferred value", []string{"boost"}, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := PreferredParam(PayMethodGroupOf(tc.ids[0]), tc.ids)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("PreferredParam(%q, %v) = (%q,%v), want (%q,%v)", tc.ids[0], tc.ids, got, ok, tc.want, tc.ok)
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
		"fpx":             PayMethodFPX,
		"fpx_b2b1":        PayMethodFPX,
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
	// which is a different product the merchant did not enable.
	got = NarrowWhitelist(PayMethodFPX, []string{"fpx"})
	if strings.Join(got, ",") != "fpx" {
		t.Fatalf("fpx narrowing = %v", got)
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
	// narrows the request.
	if got := PayOptions(nil); len(got) != 4 {
		t.Fatalf("empty whitelist must offer four groups, got %d", len(got))
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
	// An unrestricted checkout enables everything it can label.
	if err := ValidatePayMethod("duitnow_qr", nil); err != nil {
		t.Fatalf("an empty whitelist must accept a group we can label: %v", err)
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
