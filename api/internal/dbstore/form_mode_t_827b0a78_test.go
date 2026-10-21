package dbstore

// Card t_827b0a78: `mode` is the ONE rule that decides whether a form takes
// payment, and this file pins the half of it the card is about.
//
// A form whose config carries an ENABLED custom_html section but no
// `"mode":"response"` used to read as a PAYMENT form here, so serveForm painted
// the payment/legacy layout while the config carried a body only the response
// renderer can paint — the merchant's form never appeared. The section itself is
// the authority: a custom-HTML form is response-only by construction (issue
// #255), so this rule must not depend on the caller having also written the
// mode. Every reader of the mode (public render, submit dispatch, entry limit,
// dashboard label, publish readiness) goes through FormMode, which is why the
// fix belongs here and not at one call site.

import (
	"testing"

	"github.com/wzul/jomform/api/internal/customform"
)

// customSectionCfg builds a form config carrying the custom-form section as the
// raw-config door receives it.
func customSectionCfg(html string) map[string]any {
	return map[string]any{
		customform.ConfigKey: map[string]any{
			"html": html,
			"fields": []any{
				map[string]any{"field_id": "email", "type": "email", "label": "E-mel"},
			},
		},
	}
}

// TestFormModeCustomSectionIsAResponseForm is the read-side rule: the section
// decides, and an explicit payment mode cannot outvote it (the section is the
// form's body; the mode is what tells the renderers which layout to paint, and
// only the response renderer can paint a custom body).
func TestFormModeCustomSectionIsAResponseForm(t *testing.T) {
	const body = `<div><input type="email" name="email" required></div>`

	// The card's exact shape: enabled section, NO mode key at all.
	noMode := customSectionCfg(body)

	// The same section with an explicit payment mode — reachable from the
	// dashboard, which preserves the section (card t_59bc3055) and carries the
	// mode its own select holds.
	explicitPayment := customSectionCfg(body)
	explicitPayment["mode"] = FormModePayment

	// A section with no body is NOT a custom form: the docs promise that
	// emptying html puts the form back on the block/payment renderer.
	cleared := customSectionCfg("   ")
	cleared["mode"] = FormModePayment
	clearedNoMode := customSectionCfg("")

	// A config whose custom_html cannot be decoded as a section is not a custom
	// form either (customform.FromConfig returns nil, Enabled is false), so the
	// old mode rule must still answer.
	undecodable := map[string]any{customform.ConfigKey: "bukan objek"}

	cases := []struct {
		name string
		cfg  map[string]any
		want string
	}{
		{"enabled section, no mode key (the card's shape)", noMode, FormModeResponse},
		{"enabled section, mode: payment (body wins)", explicitPayment, FormModeResponse},
		{"emptied section, mode: payment", cleared, FormModePayment},
		{"emptied section, no mode", clearedNoMode, FormModePayment},
		{"no custom section, no mode (legacy form)", map[string]any{"blocks": []any{}}, FormModePayment},
		{"no custom section, mode: payment", map[string]any{"mode": FormModePayment}, FormModePayment},
		{"no custom section, mode: response", map[string]any{"mode": FormModeResponse}, FormModeResponse},
		{"undecodable custom_html, no mode", undecodable, FormModePayment},
		{"nil config", nil, FormModePayment},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := FormMode(&Form{Config: tc.cfg})
			if got != tc.want {
				t.Fatalf("FormMode(%v) = %q, want %q", tc.cfg, got, tc.want)
			}
			if isResp := IsResponseForm(&Form{Config: tc.cfg}); isResp != (tc.want == FormModeResponse) {
				t.Errorf("IsResponseForm(%v) = %v, want %v", tc.cfg, isResp, tc.want == FormModeResponse)
			}
		})
	}

	// A nil form is a payment form — the historical default every legacy caller
	// relies on.
	if got := FormMode(nil); got != FormModePayment {
		t.Fatalf("FormMode(nil) = %q, want %q", got, FormModePayment)
	}
}

// TestFormModeResponseValueIsTheCustomFormOne pins the two names for the stored
// value to a single literal: dbstore.FormModeResponse aliases
// customform.ModeResponse, so a rename on either side cannot silently leave the
// write side storing a value this rule no longer recognises.
func TestFormModeResponseValueIsTheCustomFormOne(t *testing.T) {
	if FormModeResponse != customform.ModeResponse {
		t.Fatalf("FormModeResponse = %q but customform.ModeResponse = %q — the reader and the "+
			"writer of forms.config.mode disagree", FormModeResponse, customform.ModeResponse)
	}
	if FormModeResponse != "response" {
		t.Fatalf("FormModeResponse = %q, want the stored value \"response\"", FormModeResponse)
	}
}
