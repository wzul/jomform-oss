package dbstore

// Onboarding-gap guard: a PAYMENT form with nothing to sell must not be able to
// go live silently, and a RESPONSE form with nothing to sell must stay
// publishable.
//
// THE GAP (evidence, not a crash). Two of the three outside signups that built
// a form and never published left a draft whose config had NO `products` key at
// all while its mode was payment:
//
//   alpha00 / contest-entry   blocks: email, name, phone, text, textarea   products: (none)
//   bravo00 / donation   blocks: email, name, text, textarea          products: (none)
//
// Publishing them succeeded (the handler only warns). The form then had no way
// to record a sale and nothing at the moment that mattered said so. This file
// pins the rule that closes that hole, and — just as important — pins the shapes
// that must NOT be blocked, because over-blocking a response form would break
// the one legitimate reason to build a form with no products.
//
// WHAT IT DOES NOT COVER: the pixels. The dashboard panel and the "Publish
// anyway" path are covered by web/src/lib/publishResult.test.ts (the decision
// module) and by the browser measurement recorded in the PR.
//
// Pure unit tests — no database, because the rule reads the form's own struct.

import "testing"

// formWithConfig builds a form struct for the rule (no DB, no account).
func formWithConfig(status, mode string, extra map[string]any) *Form {
	cfg := map[string]any{}
	if mode != "" {
		cfg["mode"] = mode
	}
	for k, v := range extra {
		cfg[k] = v
	}
	return &Form{Status: status, Config: cfg}
}

func productBlock(productID string) map[string]any {
	return map[string]any{"id": "b1", "type": "product", "product_id": productID}
}

func TestPaymentFormWithoutProductsFlagsTheProductionDraftShape(t *testing.T) {
	// The exact shape the two production drafts had: payment mode, ordinary
	// field blocks, no `products` key.
	prod := formWithConfig(FormStatusDraft, FormModePayment, map[string]any{
		"blocks": []any{
			map[string]any{"id": "b1", "type": "email", "value": "Email"},
			map[string]any{"id": "b2", "type": "name", "value": "Name"},
			map[string]any{"id": "b3", "type": "phone", "value": "Phone"},
			map[string]any{"id": "b4", "type": "text", "value": "Contest details"},
			map[string]any{"id": "b5", "type": "textarea", "value": "Notes"},
		},
	})
	if !PaymentFormWithoutProducts(prod) {
		t.Fatal("a payment form with no products was reported as ready — this is the hole the card is about: " +
			"it publishes fine and can never record a sale")
	}
}

func TestPublishNeedsAckOnlyOnTheFirstLiveTransition(t *testing.T) {
	emptyPayment := formWithConfig(FormStatusDraft, FormModePayment, nil)

	// Entering a live status for the first time needs an acknowledgement...
	for _, live := range []string{FormStatusPublished, FormStatusUnlisted, FormStatusPrivate} {
		if !PublishNeedsAck(emptyPayment, live) {
			t.Errorf("moving a draft payment form with no products to %q did not require an acknowledgement", live)
		}
	}
	// ...but draft does not (it is not live), and neither does re-sending the
	// status a form already has — the dashboard's status control posts the
	// current status on every save, so a non-transition rule would nag the
	// merchant every time they edited the title of a form they published empty
	// on purpose.
	if PublishNeedsAck(emptyPayment, FormStatusDraft) {
		t.Error("moving to draft required an acknowledgement — draft is not live")
	}
	for _, live := range []string{FormStatusPublished, FormStatusUnlisted, FormStatusPrivate} {
		already := formWithConfig(live, FormModePayment, nil)
		if PublishNeedsAck(already, live) {
			t.Errorf("re-sending status %q on a form that already has it required an acknowledgement", live)
		}
		// Moving from one live status to another is also not a first publish.
		if PublishNeedsAck(already, FormStatusPublished) && live != FormStatusPublished {
			t.Errorf("moving %q -> published required an acknowledgement", live)
		}
	}
	// A nil form is never "unready" — a lookup failure must not block a write
	// (the callers resolve the form first and report a real not-found).
	if PublishNeedsAck(nil, FormStatusPublished) {
		t.Error("a nil form required an acknowledgement")
	}
}

func TestResponseFormWithNoProductsStaysPublishable(t *testing.T) {
	// The over-blocking guard. A response form collects answers; having no
	// products is its normal state, not a mistake.
	resp := formWithConfig(FormStatusDraft, FormModeResponse, map[string]any{
		"blocks": []any{map[string]any{"id": "b1", "type": "email", "value": "Email"}},
	})
	if PaymentFormWithoutProducts(resp) {
		t.Fatal("a response form with no products was reported as unready — a product-free form is exactly " +
			"what response mode is for, and blocking it would be the worse failure")
	}
	if PublishNeedsAck(resp, FormStatusPublished) {
		t.Fatal("publishing a response form with no products required an acknowledgement")
	}
	// A response form with no `mode` key at all is NOT a response form (legacy
	// forms are payment forms); it must still be reported, or the default would
	// silently exempt every pre-#171 row.
	if !PaymentFormWithoutProducts(formWithConfig(FormStatusDraft, "", nil)) {
		t.Error("a form with no mode key was exempted — legacy rows are payment forms and must be reported")
	}
}

func TestPaymentFormWithSomethingToSellIsReady(t *testing.T) {
	cases := []struct {
		name string
		form *Form
	}{
		{"snapshot product", formWithConfig(FormStatusDraft, FormModePayment, map[string]any{
			"products": []any{map[string]any{"id": "p1", "name": "Tee", "price_sen": float64(1000)}},
		})},
		{"product block", formWithConfig(FormStatusDraft, FormModePayment, map[string]any{
			"blocks": []any{productBlock("76109c58-0000-0000-0000-000000000000")},
		})},
		{"priced option block", formWithConfig(FormStatusDraft, FormModePayment, map[string]any{
			"blocks": []any{map[string]any{"id": "o1", "type": "option", "value": "Extra", "price_sen": float64(500)}},
		})},
		{"priced shipping choice", formWithConfig(FormStatusDraft, FormModePayment, map[string]any{
			"blocks": []any{map[string]any{"id": "s1", "type": "shipping", "value": "Postage", "choices": []any{
				map[string]any{"label": "West Malaysia", "value": "wm", "price_sen": float64(800)},
			}}},
		})},
		{"enabled per-form menu", formWithConfig(FormStatusDraft, FormModePayment, map[string]any{
			"menu": map[string]any{"enabled": true, "categories": []any{}},
		})},
		// An id that is not a UUID is still an id: the rule asks "is there
		// something to point at", not "does that row exist" — a merchant whose
		// product was archived must not be nagged by a publish guard.
		{"snapshot with a non-uuid id", formWithConfig(FormStatusDraft, FormModePayment, map[string]any{
			"products": []any{map[string]any{"id": "legacy-1", "price_sen": 0}},
		})},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if PaymentFormWithoutProducts(tc.form) {
				t.Errorf("%s: reported as a payment form with nothing to sell", tc.name)
			}
			if PublishNeedsAck(tc.form, FormStatusPublished) {
				t.Errorf("%s: publish required an acknowledgement", tc.name)
			}
		})
	}
}

func TestMenuExemptionRequiresTheMenuToBeEnabled(t *testing.T) {
	// A menu that is CONFIGURED but switched off does not sell anything — the
	// form renders its own blocks, so the products rule applies as usual.
	off := formWithConfig(FormStatusDraft, FormModePayment, map[string]any{
		"menu": map[string]any{"enabled": false, "categories": []any{}},
	})
	if !PaymentFormWithoutProducts(off) {
		t.Error("a disabled menu exempted a payment form with nothing to sell")
	}
	// And a malformed menu blob must not exempt anything either.
	bad := formWithConfig(FormStatusDraft, FormModePayment, map[string]any{"menu": "yes"})
	if !PaymentFormWithoutProducts(bad) {
		t.Error("a malformed menu value exempted a payment form with nothing to sell")
	}
}

func TestEmptyProductEntriesDoNotCountAsSellable(t *testing.T) {
	// An empty `products` array, a null entry, an entry with no id: none of
	// these is something a buyer can pay for, and each is a realistic shape of
	// "the merchant opened the products panel and it stayed empty".
	cases := []struct {
		name string
		cfg  map[string]any
	}{
		{"empty products array", map[string]any{"products": []any{}}},
		{"null product entry", map[string]any{"products": []any{nil}}},
		{"entry with no id", map[string]any{"products": []any{map[string]any{"name": "Tee"}}}},
		{"blank product block", map[string]any{"blocks": []any{map[string]any{"type": "product", "product_id": ""}}}},
		{"zero-priced option with no choices", map[string]any{"blocks": []any{
			map[string]any{"type": "option", "price_sen": float64(0)},
		}}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			f := formWithConfig(FormStatusDraft, FormModePayment, tc.cfg)
			if !PaymentFormWithoutProducts(f) {
				t.Errorf("%s: counted as sellable, so a form that cannot take money would publish silently", tc.name)
			}
		})
	}
	// A nil config is the same shape (a form created with no config at all).
	if !PaymentFormWithoutProducts(formWithConfig(FormStatusDraft, FormModePayment, nil)) {
		t.Error("a payment form with a nil config was reported as ready")
	}
}

// TestOGImagePubliclyCachedIsThePurgeTrigger pins WHICH statuses put a form's
// social card in a shared cache. It is a different question from IsLiveStatus —
// private is live but is served `private, no-store` — and it is the only thing
// that decides whether a status change has to purge the edge. Getting it wrong in
// either direction is a defect: too narrow and a cached card keeps naming a form
// whose page 404s (the prod measurement this rule comes from); too wide and every
// publish costs a CDN call for an entry that cannot exist.
func TestOGImagePubliclyCachedIsThePurgeTrigger(t *testing.T) {
	cases := []struct {
		status string
		want   bool
		why    string
	}{
		{FormStatusPublished, true, "the public arm: `Cache-Control: public`"},
		{FormStatusUnlisted, true, "still live at its direct URL, served publicly"},
		{FormStatusDraft, false, "404 with the unknown-slug body — nothing cacheable is written"},
		{FormStatusPrivate, false, "404 until unlocked, and `private, no-store` when it is"},
		{"", false, "no status: nothing is served"},
	}
	for _, tc := range cases {
		if got := OGImagePubliclyCached(tc.status); got != tc.want {
			t.Errorf("OGImagePubliclyCached(%q) = %v, want %v (%s)", tc.status, got, tc.want, tc.why)
		}
	}
	// The distinction that makes this a separate rule, stated as an assertion so it
	// cannot be collapsed back into IsLiveStatus by a later "simplification".
	if !IsLiveStatus(FormStatusPrivate) || OGImagePubliclyCached(FormStatusPrivate) {
		t.Errorf("private must be LIVE (its page shows the password gate) but NOT publicly " +
			"cached (an image route has no gate to show, so it 404s and declares no-store)")
	}
}
