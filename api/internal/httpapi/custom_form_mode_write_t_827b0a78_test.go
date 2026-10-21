package httpapi

// Card t_827b0a78 — the write side, REST half. A POST/PATCH whose raw config
// carries an ENABLED custom_html section but no `"mode":"response"` is what
// stored a form that described itself as a payment form; the public page then
// rendered the payment/legacy layout. The write guard now stores the mode beside
// the section (customform.NormalizeConfig), so the row is self-describing.
//
// Assertions read the STORED bytes: the defect is about what reached the
// database, not what a response claimed.
//
// Integration tests; skipped unless JOMFORM_TEST_DATABASE_URL is set.

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/wzul/jomform/api/internal/customform"
)

// customCfgNoMode is the raw config an agent following /docs/custom-forms sends:
// the section, and no mode key.
func customCfgNoMode() map[string]any {
	return map[string]any{
		customform.ConfigKey: map[string]any{
			"html": `<div><label>E-mel</label><input type="email" name="email"></div>`,
			"fields": []any{
				map[string]any{"field_id": "email", "type": "email", "label": "E-mel"},
			},
		},
	}
}

func modeReqBody(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}
	return string(raw)
}

func TestCreateFormStoresResponseModeBesideCustomSection(t *testing.T) {
	fx := newRenameFixture(t, "it-cf-mode-"+randHex(4))
	slug := "it-cf-mode-" + randHex(4)

	code, raw := tplReq(t, fx.h, fx.key, http.MethodPost, "/v1/rest/forms",
		modeReqBody(t, map[string]any{
			"title":  "Borang tersuai",
			"slug":   slug,
			"config": customCfgNoMode(),
		}))
	if code != http.StatusCreated {
		t.Fatalf("POST /v1/rest/forms with a raw custom_html config = %d, want 201 (body %s)",
			code, raw)
	}

	cfg := formConfig(t, fx, slug)
	if got, _ := cfg["mode"].(string); got != customform.ModeResponse {
		t.Fatalf("stored config.mode = %q, want %q — the row carries a custom body and calls "+
			"itself a payment form: %v", got, customform.ModeResponse, cfg)
	}
	if _, ok := cfg[customform.ConfigKey]; !ok {
		t.Fatalf("stored config lost the custom_html section: %v", cfg)
	}

	// The create response must describe the form the same way the stored bytes
	// do, or the dashboard labels it wrongly from the first render.
	var created map[string]any
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatalf("decode create response: %v (%s)", err, raw)
	}
	if got, _ := created["mode"].(string); got != customform.ModeResponse {
		t.Errorf("create response mode = %q, want %q (body %s)", got, customform.ModeResponse, raw)
	}

	// Control arm: an ordinary payment config is stored exactly as sent — the
	// guard must not invent a mode for a form with no custom section.
	plainSlug := "it-cf-plain-" + randHex(4)
	code, raw = tplReq(t, fx.h, fx.key, http.MethodPost, "/v1/rest/forms",
		modeReqBody(t, map[string]any{
			"title":  "Borang bayaran",
			"slug":   plainSlug,
			"config": map[string]any{"mode": "payment", "blocks": []any{}},
		}))
	if code != http.StatusCreated {
		t.Fatalf("POST an ordinary payment form = %d, want 201 (body %s)", code, raw)
	}
	if got, _ := formConfig(t, fx, plainSlug)["mode"].(string); got != "payment" {
		t.Errorf("stored mode of an ordinary payment form = %q, want \"payment\"", got)
	}
}

func TestUpdateFormStoresResponseModeBesideCustomSection(t *testing.T) {
	fx := newRenameFixture(t, "it-cf-mode-upd-"+randHex(4))
	slug := "it-cf-mode-upd-" + randHex(4)

	// Seed an ordinary payment form.
	code, raw := tplReq(t, fx.h, fx.key, http.MethodPost, "/v1/rest/forms",
		modeReqBody(t, map[string]any{
			"title":  "Borang",
			"slug":   slug,
			"config": map[string]any{"mode": "payment", "blocks": []any{}},
		}))
	if code != http.StatusCreated {
		t.Fatalf("seed create = %d, want 201 (body %s)", code, raw)
	}
	var created map[string]any
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatalf("decode create response: %v (%s)", err, raw)
	}
	formID, _ := created["id"].(string)
	if formID == "" {
		t.Fatalf("create response carries no id: %s", raw)
	}

	// PATCH it onto a custom form WITHOUT the mode — the second raw-config door.
	code, raw = tplReq(t, fx.h, fx.key, http.MethodPatch, "/v1/rest/forms/"+formID,
		modeReqBody(t, map[string]any{"title": "Borang", "config": customCfgNoMode()}))
	if code != http.StatusOK {
		t.Fatalf("PATCH with a raw custom_html config = %d, want 200 (body %s)", code, raw)
	}
	if got, _ := formConfig(t, fx, slug)["mode"].(string); got != customform.ModeResponse {
		t.Fatalf("stored config.mode after the PATCH = %q, want %q — the update path stored the "+
			"section without the mode it needs", got, customform.ModeResponse)
	}

	// And the dashboard's own save of that form round-trips: sending back the
	// stored config keeps the mode (the builder carries what it loaded).
	code, raw = tplReq(t, fx.h, fx.key, http.MethodPatch, "/v1/rest/forms/"+formID,
		modeReqBody(t, map[string]any{"title": "Borang", "config": formConfig(t, fx, slug)}))
	if code != http.StatusOK {
		t.Fatalf("PATCH the stored config back = %d, want 200 (body %s)", code, raw)
	}
	if got, _ := formConfig(t, fx, slug)["mode"].(string); got != customform.ModeResponse {
		t.Errorf("stored config.mode after a round-trip save = %q, want %q", got, customform.ModeResponse)
	}
}
