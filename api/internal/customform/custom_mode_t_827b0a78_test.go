package customform

// Card t_827b0a78, write side: the raw-config doors (REST form create/update,
// MCP upsert_form's config input, the template writers) call
// ValidateAndNormalizeConfig, so a config that carries an ENABLED custom_html
// section is stored with `mode:"response"` beside it.
//
// The section and the mode are two statements of one fact. Storing the section
// alone left a form that described itself as a payment form while carrying a
// body only the response renderer paints, and the buyer got the payment/legacy
// layout instead of the merchant's form. mcpsrv's documented custom door
// (applyCustomFormConfig) already forced the mode; these pin the same rule for
// every other writer, in the package that owns the section.

import (
	"strings"
	"testing"
)

func enabledCfg() map[string]any {
	return map[string]any{
		ConfigKey: map[string]any{
			"html": `<div><input type="email" name="email"></div>`,
			"fields": []any{
				map[string]any{"field_id": "email", "type": "email", "label": "E-mel"},
			},
		},
	}
}

func TestNormalizeConfigForcesResponseMode(t *testing.T) {
	// The card's shape: enabled section, no mode key at all.
	noMode := enabledCfg()
	NormalizeConfig(noMode)
	if got, _ := noMode["mode"].(string); got != ModeResponse {
		t.Fatalf("mode after NormalizeConfig = %q, want %q — the stored config would describe a "+
			"payment form while carrying a custom body", got, ModeResponse)
	}

	// An explicit payment mode is not honoured beside an enabled section either:
	// the section IS the form's body, and only the response renderer paints it.
	explicit := enabledCfg()
	explicit["mode"] = "payment"
	NormalizeConfig(explicit)
	if got, _ := explicit["mode"].(string); got != ModeResponse {
		t.Fatalf("mode after NormalizeConfig over an explicit payment mode = %q, want %q", got, ModeResponse)
	}

	// Everything else is left ALONE — this is the control arm. In particular a
	// cleared section must not drag a payment form onto the response renderer:
	// the docs promise that emptying html puts the form back on the blocks.
	cleared := map[string]any{
		ConfigKey: map[string]any{"html": "   ", "fields": []any{}},
		"mode":    "payment",
	}
	NormalizeConfig(cleared)
	if got, _ := cleared["mode"].(string); got != "payment" {
		t.Fatalf("mode = %q after normalizing an EMPTIED section, want the caller's \"payment\"", got)
	}

	noSection := map[string]any{"blocks": []any{}}
	NormalizeConfig(noSection)
	if _, ok := noSection["mode"]; ok {
		t.Fatalf("NormalizeConfig added a mode to a config with no custom section: %v", noSection)
	}

	nilCfg := map[string]any{}
	NormalizeConfig(nilCfg)
	if len(nilCfg) != 0 {
		t.Fatalf("NormalizeConfig touched an empty config: %v", nilCfg)
	}
	NormalizeConfig(nil) // must not panic
}

func TestValidateAndNormalizeConfigIsTheWriteEntryPoint(t *testing.T) {
	// A valid section with no mode: accepted AND self-consistent afterwards.
	ok := enabledCfg()
	if err := ValidateAndNormalizeConfig(ok); err != nil {
		t.Fatalf("ValidateAndNormalizeConfig over a valid section: %v", err)
	}
	if got, _ := ok["mode"].(string); got != ModeResponse {
		t.Fatalf("mode after the write guard = %q, want %q", got, ModeResponse)
	}

	// A REFUSED contract must not be normalized on the way out: the caller's
	// map is reported as the problem, and a refusal that also rewrote the mode
	// would make the 400 hard to reason about.
	bad := map[string]any{
		"mode": "payment",
		ConfigKey: map[string]any{
			"html": `<div><input type="email" name="emel"></div>`,
			"fields": []any{
				map[string]any{"field_id": "nama", "type": "text", "label": "Nama", "required": true},
			},
		},
	}
	err := ValidateAndNormalizeConfig(bad)
	if err == nil {
		t.Fatalf("ValidateAndNormalizeConfig accepted an unsatisfiable contract")
	}
	if !strings.Contains(err.Error(), "required field missing from the HTML") {
		t.Errorf("refusal = %q, want the missing-control message", err)
	}
	if got, _ := bad["mode"].(string); got != "payment" {
		t.Errorf("a REFUSED write still normalized mode to %q — the guard must leave the map alone", got)
	}

	// A config with no custom section is not touched at all (the common case for
	// every ordinary form write).
	plain := map[string]any{"mode": "payment", "blocks": []any{}}
	if err := ValidateAndNormalizeConfig(plain); err != nil {
		t.Fatalf("ValidateAndNormalizeConfig over a plain config: %v", err)
	}
	if got, _ := plain["mode"].(string); got != "payment" {
		t.Errorf("mode = %q after guarding a plain payment config, want \"payment\"", got)
	}
}
