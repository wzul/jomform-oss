package mcpsrv

// Card t_827b0a78 — the write side, MCP half. `upsert_form` takes a raw `config`
// map verbatim, and that door stored an ENABLED custom_html section without
// `"mode":"response"`: the form then described itself as a payment form while
// carrying a body only the response renderer paints, so the buyer got the
// payment/legacy layout instead of the merchant's form.
//
// The documented custom_html + custom_fields inputs were never affected —
// applyCustomFormConfig sets the mode itself (parity.go) — which is exactly the
// behaviour the raw door lacked. Both halves now store the mode beside the
// section, so the row is self-describing whichever door wrote it.
//
// Assertions read the STORED config: the defect is about what reached the
// database.
//
// Integration test; needs real Postgres (JOMFORM_TEST_DATABASE_URL).

import (
	"testing"

	"github.com/wzul/jomform/api/internal/customform"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// customCfgNoModeMCP is the raw config an agent following /docs/custom-forms
// sends through upsert_form: the section, and no mode key.
func customCfgNoModeMCP() map[string]any {
	return map[string]any{
		customform.ConfigKey: map[string]any{
			"html": `<div><label>E-mel</label><input type="email" name="email"></div>`,
			"fields": []any{
				map[string]any{"field_id": "email", "type": "email", "label": "E-mel"},
			},
		},
	}
}

func TestUpsertFormRawConfigStoresResponseModeBesideCustomSection(t *testing.T) {
	st := issue501MCPFixture(t)
	forms := dbstore.NewFormStore(st.pool)

	// Create through the raw config door, with no mode key.
	_, out, err := st.srv.upsertForm(st.ctx, nil, upsertFormIn{
		Title:  "Borang tersuai cipta",
		Config: customCfgNoModeMCP(),
	})
	if err != nil {
		t.Fatalf("upsert_form create with a raw custom_html config: %v", err)
	}
	createdID := out["form_id"]
	if createdID == "" {
		t.Fatalf("upsert_form create returned no form_id: %v", out)
	}
	stored, err := forms.GetByID(st.ctx, st.accountID, createdID)
	if err != nil {
		t.Fatalf("read created form: %v", err)
	}
	if got, _ := stored.Config["mode"].(string); got != customform.ModeResponse {
		t.Fatalf("stored config.mode after a raw-config CREATE = %q, want %q — the form carries a "+
			"custom body and calls itself a payment form: %v", got, customform.ModeResponse, stored.Config)
	}
	if dbstore.FormMode(stored) != dbstore.FormModeResponse {
		t.Errorf("dbstore.FormMode of the stored row = %q, want %q",
			dbstore.FormMode(stored), dbstore.FormModeResponse)
	}

	// Update arm: the same door on an existing payment form.
	formID := st.seedForm(t, "i-cf-mode-"+randHex(4))
	if _, _, err := st.srv.upsertForm(st.ctx, nil, upsertFormIn{
		FormID: formID, Title: "Borang tersuai", Config: customCfgNoModeMCP(),
	}); err != nil {
		t.Fatalf("upsert_form update with a raw custom_html config: %v", err)
	}
	stored, err = forms.GetByID(st.ctx, st.accountID, formID)
	if err != nil {
		t.Fatalf("read updated form: %v", err)
	}
	if got, _ := stored.Config["mode"].(string); got != customform.ModeResponse {
		t.Fatalf("stored config.mode after a raw-config UPDATE = %q, want %q", got, customform.ModeResponse)
	}

	// Control arm: a plain payment config is stored as sent — the guard must not
	// label every form a response form.
	plainID := st.seedForm(t, "i-cf-plain-"+randHex(4))
	if _, _, err := st.srv.upsertForm(st.ctx, nil, upsertFormIn{
		FormID: plainID, Title: "Borang bayaran",
		Config: map[string]any{"mode": "payment", "blocks": []any{}},
	}); err != nil {
		t.Fatalf("upsert_form a plain payment form: %v", err)
	}
	plain, err := forms.GetByID(st.ctx, st.accountID, plainID)
	if err != nil {
		t.Fatalf("read plain form: %v", err)
	}
	if got, _ := plain.Config["mode"].(string); got != "payment" {
		t.Errorf("stored mode of a plain payment form = %q, want \"payment\"", got)
	}
}
