package dbstore

import "testing"

// Regression test — the form's collection mode is what decides
// whether a form is a "sales" form.
//
// THE BUG: the /forms list labelled EVERY row "Sales", including response-only
// forms (an RSVP, a contact form, an event registration) that can never record
// a sale. The reason the dashboard had to guess is that the forms LIST payload
// carried no mode at all — `restForm` had no `mode` field. The discriminator
// already existed and was already load-bearing (the public submit path records
// a submission instead of an order for a response form; the entry-limit counter
// counts submissions instead of sales), it just was not surfaced.
//
// WHAT IS PINNED HERE
// 1. An explicit "response" mode is response.
// 2. Anything else is PAYMENT — including a form with no `mode` key at all.
// Every form created before response mode existed has no key, and those
// are payment forms (they are also the forms that actually have sales in
// production). Defaulting to response would hide the Sales action on real
// sales forms — the worse failure, and the one a naive
// `mode == "payment"` test would not catch.
// 3. A nil form / nil config does not panic and is a payment form.
// 4. A non-string `mode` (any JSON shape) is not mistaken for "response".
//
// Run with: cd api && go test ./internal/dbstore/
func TestFormModeOnlyExplicitResponseIsResponse(t *testing.T) {
	cases := []struct {
		name string
		form *Form
		want string
	}{
		{"nil form", nil, FormModePayment},
		{"nil config", &Form{ID: "f1"}, FormModePayment},
		{"empty config", &Form{ID: "f2", Config: map[string]any{}}, FormModePayment},
		{"explicit response", &Form{ID: "f3", Config: map[string]any{"mode": "response"}}, FormModeResponse},
		{"explicit payment", &Form{ID: "f4", Config: map[string]any{"mode": "payment"}}, FormModePayment},
		// The load-bearing default: a legacy form (pre-#171) has no mode key and
		// IS a payment form. This case is the whole reason the rule defaults to
		// payment rather than treating "unknown" as response.
		{"legacy form with no mode key", &Form{ID: "f5", Config: map[string]any{"blocks": []any{}}}, FormModePayment},
		// A config that carries other keys but not mode — same legacy shape.
		{"config without mode", &Form{ID: "f6", Config: map[string]any{"category": "Events"}}, FormModePayment},
		// Wrong-typed / unexpected values must never be read as "response".
		{"mode as a non-string", &Form{ID: "f7", Config: map[string]any{"mode": 1}}, FormModePayment},
		{"mode as an unexpected string", &Form{ID: "f8", Config: map[string]any{"mode": "RESPONSE"}}, FormModePayment},
		{"mode empty string", &Form{ID: "f9", Config: map[string]any{"mode": ""}}, FormModePayment},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FormMode(tc.form); got != tc.want {
				t.Errorf("FormMode() = %q, want %q", got, tc.want)
			}
			// IsResponseForm must agree with FormMode in every case — two
			// readers of the same rule that disagree would be its own bug.
			if got := IsResponseForm(tc.form); got != (tc.want == FormModeResponse) {
				t.Errorf("IsResponseForm() = %v, want %v", got, tc.want == FormModeResponse)
			}
		})
	}
}
