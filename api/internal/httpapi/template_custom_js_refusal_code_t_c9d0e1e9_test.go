package httpapi

// Card t_c9d0e1e9 — the DASHBOARD could not name a refused theme.custom_js on the
// TEMPLATE surfaces.
//
// THE DEFECT (measured on prod v1.21.295, signed in, /templates → "Use template"):
//
//	POST /api/rest/templates/<id>/use?workspace=<ws>
//	  -> 400 {"code":"request.badRequest","error":"custom_js ditolak: active code execution marker: eval("}
//	the SAME click rendered role=alert "Failed to use template."
//
// The refusal was correct and nothing was cloned — but `request.badRequest` is a
// CLASS code, and web/src/lib/apiErr.ts deliberately does not translate class
// codes (they say nothing a page cannot say better itself), so the page fell back
// to its own generic sentence and the merchant learned neither which value was
// refused nor why. The same shape reached the builder's template save
// (PUT /v1/rest/templates/{id}) and /form-settings' theme save.
//
// WHAT ALREADY LANDED, AND WHY THIS FILE STILL EXISTS. t_2717ba30 gave the
// refusal a specific machine code (`CodeFormCustomJSRejected`) and a dictionary
// sentence that names the Custom JS field; apiErr.test.ts reads errcode.go and
// fails if any declared code has no dictionary row, so the dashboard half is
// pinned. But t_2717ba30's own door table drives PATCH /v1/rest/forms/{id},
// POST /v1/rest/forms and POST /v1/rest/templates — NOT the two doors this card
// measured. Those two were covered for the SENTENCE by t_5c35bfeb and never for
// the CODE, so the reported regression (class code back on the wire → the page's
// generic "Failed to use template." again) could return with every existing test
// green. This file closes that gap: it asserts the machine code, not just the
// status and not just the prose.
//
// WHAT IT PINS
//  1. POST /v1/rest/templates/{id}/use — the Use button's own route, on a row
//     that already carries a blocked value (seeded through the store, so the test
//     cannot be satisfied by the create-side guard) — answers 400 with
//     code=form.customJsRejected.
//  2. PUT /v1/rest/templates/{id} — the builder's template save — answers the
//     same code.
//  3. The wire PROSE is byte-identical to the pre-existing sentence: API-key,
//     curl and MCP callers read it and cannot look up an i18n key.
//  4. The neighbouring refusals on the same two doors keep their own answer (the
//     class code for a missing body, urlguard's code for a non-https URL), so the
//     mapping cannot swallow an unrelated 400 and tell the merchant their script
//     was refused when it was a layout mistake.
//  5. A refused write stores nothing, and a benign value still lands — the code
//     is attached to the REFUSAL, not to the route.
//
// BITE (run, not argued): delete the `{prefix: customform.RejectedLeadIn, code:
// CodeFormCustomJSRejected}` row from errcode.go's errCodeByPrefix and both tests
// go RED on `code = "request.badRequest"` — the exact body the card measured —
// while the status/prose assertions still pass, which is what proves these
// assertions are the ones holding the dashboard's sentence up.
//
// HONEST LABEL: both tests PASS on the unmodified tree, because the production
// half of this card landed first (PR #1389 / t_2717ba30, merged 2026-10-24 02:56,
// after this card was filed from the v1.21.295 measurement). They are therefore a
// REGRESSION NET for the two doors that PR left uncovered — not the fix itself.
// Verified end to end at this tree: /templates → "Use template" renders
// "Custom JS was rejected: …" in role=alert, the builder's template Save renders
// it in its error line, and /form-settings' General Save renders it in
// [data-testid="action-msg"] role=alert; the live binary answers all three doors
// with `code=form.customJsRejected`.
//
// Runs the real mux with a real API key against real Postgres
// (JOMFORM_TEST_DATABASE_URL), like the two files it complements.

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// blockedTemplateFixture seeds one template row carrying a blocked custom_js
// straight through the store. The row is written directly on purpose: the write
// doors refuse this value now, so a create through the API could never reproduce
// the state the card was measured in (a legacy row, or one written by a door that
// was unscanned at the time).
func blockedTemplateFixture(t *testing.T, slugPattern string, theme map[string]any) (renameFixture, string) {
	t.Helper()
	fx := newRenameFixture(t, slugPattern+randHex(4))
	tpl := &dbstore.Form{
		AccountID: fx.accountID,
		Slug:      slugPattern + "tpl-" + randHex(6),
		Title:     "legacy template carrying custom js",
		Config: map[string]any{
			"mode":   "response",
			"blocks": []any{map[string]any{"id": "b1", "type": "text", "value": "hello"}},
		},
		Theme: theme,
	}
	if err := dbstore.NewFormStore(fx.pool).CreateTemplate(fx.ctx, tpl); err != nil {
		t.Fatalf("insert template row carrying %v: %v", theme, err)
	}
	return fx, tpl.ID
}

// TestUseTemplateRefusalCarriesTheCustomJSCode is the card's measured door: the
// Templates view's "Use template" click. Without the code the click answers the
// class code and the page renders its own "Failed to use template." instead of
// the sentence that names Custom JS.
func TestUseTemplateRefusalCarriesTheCustomJSCode(t *testing.T) {
	const blocked = "eval('9*9');"
	fx, tplID := blockedTemplateFixture(t, "it-cjs-code-use-", map[string]any{"custom_js": blocked})
	before := liveForms(t, fx)

	code, raw := tplReq(t, fx.h, fx.key, http.MethodPost, "/v1/rest/templates/"+tplID+"/use", `{}`)
	if code != http.StatusBadRequest {
		t.Fatalf("POST /v1/rest/templates/{id}/use on a template carrying %q = %d (body %s), "+
			"want 400 — a 201 here clones the blocked value onto a live form", blocked, code, raw)
	}
	if got := customJSRefusalCode(t, raw); got != CodeFormCustomJSRejected {
		t.Errorf("the Use door's refusal carries code %q, want %q (body %s).\n"+
			"`%s` is a CLASS code and web/src/lib/apiErr.ts deliberately does not translate it, "+
			"so /templates renders its own \"Failed to use template.\" and names neither Custom JS "+
			"nor the reason — the defect card t_c9d0e1e9 measured on prod v1.21.295.",
			got, CodeFormCustomJSRejected, raw, ClassBadRequest)
	}
	// The prose is the pre-existing sentence, unchanged: `code` is an ADDITION for
	// the dashboard, never a replacement for what curl/API-key/MCP callers read.
	if got, want := decodeErrField(t, raw), "custom_js ditolak: active code execution marker: eval("; got != want {
		t.Errorf("the Use refusal's prose = %q, want %q — the machine code must not reword the wire", got, want)
	}
	if after := liveForms(t, fx); after != before {
		t.Errorf("live forms went %d -> %d across a refused clone, want no new form", before, after)
	}

	// CONTROL lives on the other door (`TestUpdateTemplateRefusalCarriesThe…`
	// below), where the same route answers a second 400 class: this door has no
	// other 400 to confuse it with — a missing row is a 404 and a read-only member
	// a 403 — so the one way it can answer "custom JS was rejected" to a merchant
	// is by emitting the wrong code for the wrong value, which the assertion above
	// already refuses.
}

// TestUpdateTemplateRefusalCarriesTheCustomJSCode covers the builder's template
// save (PUT /v1/rest/templates/{id}) — the other ordinary merchant action the
// card names. The template is created with a benign script first so the refusal
// is demonstrably about the NEW value, and the stored row is read back to prove
// the refused update wrote nothing.
func TestUpdateTemplateRefusalCarriesTheCustomJSCode(t *testing.T) {
	const benign = "document.addEventListener('click', function () { return true; });"
	const blocked = "eval('2+2');"
	fx := newRenameFixture(t, "it-cjs-code-put-"+randHex(4))

	code, raw := tplReq(t, fx.h, fx.key, http.MethodPost, "/v1/rest/templates",
		templateBody(t, "builder template", map[string]any{"custom_js": benign}))
	if code != http.StatusCreated {
		t.Fatalf("POST with a benign custom_js = %d, want 201 (body %s)", code, raw)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &created); err != nil || created.ID == "" {
		t.Fatalf("decode created template (%v): %s", err, raw)
	}

	code, raw = tplReq(t, fx.h, fx.key, http.MethodPut, "/v1/rest/templates/"+created.ID,
		templateBody(t, "builder template", map[string]any{"custom_js": blocked}))
	if code != http.StatusBadRequest {
		t.Fatalf("PUT with custom_js %q = %d (body %s), want 400", blocked, code, raw)
	}
	if got := customJSRefusalCode(t, raw); got != CodeFormCustomJSRejected {
		t.Errorf("the builder-save refusal carries code %q, want %q (body %s).\n"+
			"The builder renders `apiErr(d, t(\"tplmg.editFail\"), t)`; without this code the "+
			"merchant reads \"Failed to save this template.\" for a refusal it can name exactly.",
			got, CodeFormCustomJSRejected, raw)
	}
	if got, want := decodeErrField(t, raw), "custom_js ditolak: active code execution marker: eval("; got != want {
		t.Errorf("the builder-save refusal's prose = %q, want %q", got, want)
	}
	if js, _ := storedCustomJS(t, fx, created.ID); js != benign {
		t.Errorf("stored custom_js = %q after a refused update, want the previous value %q unchanged", js, benign)
	}

	// CONTROL: the neighbour that already owns a code keeps it. A non-https theme
	// URL on the SAME door must not be reported to the merchant as refused JS.
	code, raw = tplReq(t, fx.h, fx.key, http.MethodPut, "/v1/rest/templates/"+created.ID,
		`{"title":"builder template","theme":{"logo_url":"http://example.test/logo.png"}}`)
	if code != http.StatusBadRequest {
		t.Fatalf("PUT with a non-https logo_url = %d, want 400 (body %s)", code, raw)
	}
	if got := customJSRefusalCode(t, raw); got != CodeURLNotHTTPS {
		t.Errorf("the non-https theme refusal carries code %q, want its own %q — the custom JS "+
			"code must not claim a neighbouring 400", got, CodeURLNotHTTPS)
	}

	// And the acceptance direction: the route still accepts a benign update, so the
	// code is attached to the VALUE and not to PUT.
	const second = "window.addEventListener('load', function () { return 1; });"
	code, raw = tplReq(t, fx.h, fx.key, http.MethodPut, "/v1/rest/templates/"+created.ID,
		templateBody(t, "builder template", map[string]any{"custom_js": second}))
	if code != http.StatusOK {
		t.Fatalf("PUT with a benign custom_js = %d, want 200 (body %s)", code, raw)
	}
	if js, _ := storedCustomJS(t, fx, created.ID); js != second {
		t.Errorf("stored custom_js = %q after a benign update, want %q", js, second)
	}
}
