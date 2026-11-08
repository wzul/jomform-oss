package httpapi

// Card t_5c35bfeb — the template door was a bypass around the custom_js scan.
//
// Every other guard in createOwnTemplate/updateOwnTemplate is justified in its
// own comment by "a template's config/theme is later cloned onto a real form"
// (urlguard.CheckTheme, CheckConfig, CheckConfigShape, UnsupportedBlock,
// ValidateAndNormalizeConfig) — but customform.ScanCustomJS was missing, and
// useOwnTemplate copies tpl.Theme verbatim onto the new form. Measured on the
// tree before this fix, one throwaway account, one value `eval('9*9');`:
//
//	POST /v1/rest/templates        {"config":{"blocks":[...]},"theme":{"custom_js":"eval('9*9');"}}
//	  -> 201, template row stores `eval('9*9');` verbatim
//	POST /v1/rest/templates/{id}/use {}
//	  -> 201, the cloned form row reads `eval('9*9');`
//	GET  /f/<clone>  (Host: <workspace>.jomform.com, after publish)
//	  -> 200, and the buyer's page serves it as live code:
//	       <script>
//	       eval('9*9');
//	       </script>
//
// while PATCH /v1/rest/forms/{id} and both MCP form doors already answered the
// same value with 400 `custom_js rejected: <reason>`.
//
// These tests drive the REAL mux with a real API key against real Postgres and
// assert on the ROW (what was written) as well as the status, because a guard
// that answers 400 after the INSERT would still ship the defect.
//
// BITE: delete the `refuseBlockedCustomJS` arm from createOwnTemplate and
// TestCreateOwnTemplateRefusesBlockedCustomJS gets 201 (the carded defect) while
// the tree still compiles; delete it from useOwnTemplate and
// TestUseOwnTemplateRefusesCloningBlockedCustomJS gets 201 and a live form
// carrying the blocked value. The no-custom_js controls keep passing either way,
// so a failure names the scan and not the route.

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// blockedCustomJSCases is the same set the form write paths pin: one marker
// from each scan class, so a template-side reuse of a narrower rule is caught.
func blockedCustomJSCases() []struct {
	name       string
	js         string
	wantReason string
} {
	return []struct {
		name       string
		js         string
		wantReason string
	}{
		{"eval", "eval('2+2');", "active code execution marker: eval("},
		{"atob", "var x = atob('ZXZhbCgyKzIp');", "obfuscation/beacon marker: atob("},
		{"external url", "fetch('https://example.test/collect');", "reference to external URL from custom JS"},
	}
}

// templateBody builds a template create/update body. theme is passed as given so
// a test can omit the key (`theme == nil`), send an empty map, or carry a value;
// config always carries a renderable block so the clone renders in block mode
// (the layout that serves theme.custom_js).
func templateBody(t *testing.T, title string, theme map[string]any) string {
	t.Helper()
	body := map[string]any{
		"title": title,
		"config": map[string]any{
			"mode":   "response",
			"blocks": []any{map[string]any{"id": "b1", "type": "text", "value": "hello"}},
		},
	}
	if theme != nil {
		body["theme"] = theme
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal template body: %v", err)
	}
	return string(raw)
}

// templateCount counts the account's live template rows — the assertion a
// status-only test cannot make.
func templateCount(t *testing.T, fx renameFixture) int {
	t.Helper()
	var n int
	if err := fx.pool.QueryRow(fx.ctx,
		`SELECT count(*) FROM forms WHERE account_id=$1 AND is_template AND deleted_at IS NULL`,
		fx.accountID).Scan(&n); err != nil {
		t.Fatalf("count templates: %v", err)
	}
	return n
}

// liveForms counts the account's live FORMS (templates excluded), so a clone
// that should not have happened is visible.
func liveForms(t *testing.T, fx renameFixture) int {
	t.Helper()
	var n int
	if err := fx.pool.QueryRow(fx.ctx,
		`SELECT count(*) FROM forms WHERE account_id=$1 AND NOT is_template AND deleted_at IS NULL`,
		fx.accountID).Scan(&n); err != nil {
		t.Fatalf("count forms: %v", err)
	}
	return n
}

// storedCustomJS reads theme->>'custom_js' straight out of the row. A missing
// value returns ("", false) rather than failing, because for a refused write the
// absence of the row IS the expectation.
func storedCustomJS(t *testing.T, fx renameFixture, id string) (string, bool) {
	t.Helper()
	var js *string
	err := fx.pool.QueryRow(fx.ctx,
		`SELECT theme->>'custom_js' FROM forms WHERE id=$1 AND account_id=$2`, id, fx.accountID).Scan(&js)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false
	}
	if err != nil {
		t.Fatalf("read stored theme.custom_js for %s: %v", id, err)
	}
	if js == nil {
		return "", false
	}
	return *js, true
}

func decodeErrField(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var out struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode error body %s: %v", raw, err)
	}
	return out.Error
}

// TestCreateOwnTemplateRefusesBlockedCustomJS is the card's first acceptance
// criterion: POST /v1/rest/templates must refuse a blocked custom_js with the
// same sentence the form write paths use, and must store nothing.
func TestCreateOwnTemplateRefusesBlockedCustomJS(t *testing.T) {
	fx := newRenameFixture(t, "it-tpl-cjs-"+randHex(4))

	for _, tc := range blockedCustomJSCases() {
		code, raw := tplReq(t, fx.h, fx.key, http.MethodPost, "/v1/rest/templates",
			templateBody(t, "blocked "+tc.name, map[string]any{"custom_js": tc.js}))
		if code != http.StatusBadRequest {
			t.Fatalf("POST /v1/rest/templates with custom_js %q = %d (body %s), want 400.\n"+
				"A 201 here is the carded defect: useOwnTemplate clones that value onto a real "+
				"form and the buyer's page serves it as live code.", tc.js, code, raw)
		}
		want := "custom_js rejected: " + tc.wantReason
		if got := decodeErrField(t, raw); got != want {
			t.Errorf("refusal for %s = %q, want %q — one rule, one wording", tc.name, got, want)
		}
		if n := templateCount(t, fx); n != 0 {
			t.Fatalf("%d template rows stored after the refused create (%s), want 0 — the value "+
				"was written anyway", n, tc.name)
		}
	}
}

// TestUpdateOwnTemplateRefusesBlockedCustomJS covers the second door named on the
// card: PUT can re-introduce a blocked value into a template that `use` clones.
func TestUpdateOwnTemplateRefusesBlockedCustomJS(t *testing.T) {
	fx := newRenameFixture(t, "it-tpl-cjs-put-"+randHex(4))
	const benign = "document.addEventListener('click', function () { return true; });"

	code, raw := tplReq(t, fx.h, fx.key, http.MethodPost, "/v1/rest/templates",
		templateBody(t, "benign template", map[string]any{"custom_js": benign}))
	if code != http.StatusCreated {
		t.Fatalf("POST with a benign custom_js = %d, want 201 (body %s)", code, raw)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &created); err != nil || created.ID == "" {
		t.Fatalf("decode created template (%v): %s", err, raw)
	}
	if js, ok := storedCustomJS(t, fx, created.ID); !ok || js != benign {
		t.Fatalf("stored custom_js = %q (present=%v), want the benign script verbatim", js, ok)
	}

	for _, tc := range blockedCustomJSCases() {
		code, raw = tplReq(t, fx.h, fx.key, http.MethodPut, "/v1/rest/templates/"+created.ID,
			templateBody(t, "benign template", map[string]any{"custom_js": tc.js}))
		if code != http.StatusBadRequest {
			t.Fatalf("PUT with custom_js %q = %d (body %s), want 400 — an update that stores it "+
				"re-arms the clone door", tc.js, code, raw)
		}
		if got, want := decodeErrField(t, raw), "custom_js rejected: "+tc.wantReason; got != want {
			t.Errorf("PUT refusal for %s = %q, want %q", tc.name, got, want)
		}
		if js, _ := storedCustomJS(t, fx, created.ID); js != benign {
			t.Fatalf("stored custom_js = %q after a refused update (%s), want the previous value "+
				"%q unchanged", js, tc.name, benign)
		}
	}

	// A benign update must still land, so the guard refuses the value and not PUT.
	const second = "window.addEventListener('load', function () { return 1; });"
	code, raw = tplReq(t, fx.h, fx.key, http.MethodPut, "/v1/rest/templates/"+created.ID,
		templateBody(t, "benign template", map[string]any{"custom_js": second}))
	if code != http.StatusOK {
		t.Fatalf("PUT with a benign custom_js = %d, want 200 (body %s)", code, raw)
	}
	if js, _ := storedCustomJS(t, fx, created.ID); js != second {
		t.Fatalf("stored custom_js = %q after a benign update, want %q", js, second)
	}
}

// TestUseOwnTemplateRefusesCloningBlockedCustomJS closes the clone itself, not
// just the write: a template row that already carries a blocked value — written
// before this guard existed, or by a door this change does not cover — must not
// be cloneable onto a real form. The row is inserted directly through the store
// so the test cannot be satisfied by the create-side arm alone.
func TestUseOwnTemplateRefusesCloningBlockedCustomJS(t *testing.T) {
	fx := newRenameFixture(t, "it-tpl-use-"+randHex(4))
	const blocked = "eval('9*9');"

	forms := dbstore.NewFormStore(fx.pool)
	tpl := &dbstore.Form{
		AccountID: fx.accountID,
		Slug:      "it-tpl-legacy-" + randHex(6),
		Title:     "legacy template",
		Config: map[string]any{
			"mode":   "response",
			"blocks": []any{map[string]any{"id": "b1", "type": "text", "value": "hello"}},
		},
		Theme: map[string]any{"custom_js": blocked},
	}
	if err := forms.CreateTemplate(fx.ctx, tpl); err != nil {
		t.Fatalf("insert template row: %v", err)
	}
	before := liveForms(t, fx)

	code, raw := tplReq(t, fx.h, fx.key, http.MethodPost, "/v1/rest/templates/"+tpl.ID+"/use", `{}`)
	if code != http.StatusBadRequest {
		t.Fatalf("POST /v1/rest/templates/{id}/use on a template carrying %q = %d (body %s), "+
			"want 400.\nA 201 here puts the blocked value on a real form, whose buyer page serves "+
			"it as live code.", blocked, code, raw)
	}
	if got, want := decodeErrField(t, raw), "custom_js rejected: active code execution marker: eval("; got != want {
		t.Errorf("use refusal = %q, want %q", got, want)
	}
	if after := liveForms(t, fx); after != before {
		t.Fatalf("live forms went %d -> %d across a refused clone, want no new form", before, after)
	}

	// Once the template is cleaned the clone works again, so the refusal is the
	// value and not the template.
	const clean = "document.addEventListener('click', function () { return true; });"
	code, raw = tplReq(t, fx.h, fx.key, http.MethodPut, "/v1/rest/templates/"+tpl.ID,
		templateBody(t, "legacy template", map[string]any{"custom_js": clean}))
	if code != http.StatusOK {
		t.Fatalf("PUT cleaning the template = %d, want 200 (body %s)", code, raw)
	}
	code, raw = tplReq(t, fx.h, fx.key, http.MethodPost, "/v1/rest/templates/"+tpl.ID+"/use", `{}`)
	if code != http.StatusCreated {
		t.Fatalf("use after cleaning the template = %d, want 201 (body %s)", code, raw)
	}
	var cloned struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &cloned); err != nil || cloned.ID == "" {
		t.Fatalf("decode clone (%v): %s", err, raw)
	}
	if js, ok := storedCustomJS(t, fx, cloned.ID); !ok || js != clean {
		t.Fatalf("cloned form custom_js = %q (present=%v), want %q", js, ok, clean)
	}
	if after := liveForms(t, fx); after != before+1 {
		t.Fatalf("live forms = %d after a successful clone, want %d", after, before+1)
	}
}

// TestTemplateDoorAnswersBlockedCustomJSLikeTheFormDoor pins the parity: one
// value, three write routes (form PATCH, template POST, template PUT), one
// answer. It fails if a future template-side check drifts to a different
// sentence, which would leave the API describing one rule two ways.
func TestTemplateDoorAnswersBlockedCustomJSLikeTheFormDoor(t *testing.T) {
	fx := newRenameFixture(t, "it-tpl-cjs-parity-"+randHex(4))
	const js = "eval('2+2');"
	const want = "custom_js rejected: active code execution marker: eval("

	patchCode, patchRaw := fx.patch(t, `{"theme":{"custom_js":"`+js+`"}}`)
	if patchCode != http.StatusBadRequest {
		t.Fatalf("PATCH /v1/rest/forms/{id} with %q = %d (body %s), want 400 — this is the "+
			"behaviour the template door must match", js, patchCode, patchRaw)
	}

	postCode, postRaw := tplReq(t, fx.h, fx.key, http.MethodPost, "/v1/rest/templates",
		templateBody(t, "parity", map[string]any{"custom_js": js}))
	if postCode != http.StatusBadRequest {
		t.Fatalf("POST /v1/rest/templates with %q = %d (body %s), want 400 — the carded defect",
			js, postCode, postRaw)
	}

	for _, route := range []struct {
		name string
		raw  json.RawMessage
	}{
		{"POST /v1/rest/templates", postRaw},
		{"PATCH /v1/rest/forms/{id}", patchRaw},
	} {
		if got := decodeErrField(t, route.raw); got != want {
			t.Errorf("%s refusal = %q, want %q", route.name, got, want)
		}
	}
}

// TestTemplateWithoutCustomJSStillCreatesAndClones is the card's "no other
// behaviour change" arm: a template that carries no custom_js (or a benign one)
// still creates and still clones, and the {form_id} branch is untouched.
func TestTemplateWithoutCustomJSStillCreatesAndClones(t *testing.T) {
	fx := newRenameFixture(t, "it-tpl-cjs-none-"+randHex(4))

	cases := []struct {
		name  string
		theme map[string]any
		want  string
	}{
		{"no theme at all", nil, ""},
		{"empty theme", map[string]any{}, ""},
		{"theme without custom_js", map[string]any{"primary": "#111111"}, ""},
		{"empty custom_js", map[string]any{"custom_js": ""}, ""},
		{"null custom_js", map[string]any{"custom_js": nil}, ""},
		{"benign custom_js", map[string]any{"custom_js": "window.addEventListener('load', function () { return 1; });"},
			"window.addEventListener('load', function () { return 1; });"},
	}
	for _, tc := range cases {
		code, raw := tplReq(t, fx.h, fx.key, http.MethodPost, "/v1/rest/templates",
			templateBody(t, "tpl "+tc.name, tc.theme))
		if code != http.StatusCreated {
			t.Fatalf("POST /v1/rest/templates with %s = %d (body %s), want 201", tc.name, code, raw)
		}
		var created struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &created); err != nil || created.ID == "" {
			t.Fatalf("decode created template (%v): %s", err, raw)
		}
		if js, ok := storedCustomJS(t, fx, created.ID); ok && js != tc.want {
			t.Fatalf("%s: stored custom_js = %q, want %q", tc.name, js, tc.want)
		}

		code, raw = tplReq(t, fx.h, fx.key, http.MethodPost, "/v1/rest/templates/"+created.ID+"/use", `{}`)
		if code != http.StatusCreated {
			t.Fatalf("use of the %s template = %d (body %s), want 201", tc.name, code, raw)
		}
		var cloned struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &cloned); err != nil || cloned.ID == "" {
			t.Fatalf("decode clone (%v): %s", err, raw)
		}
		if js, ok := storedCustomJS(t, fx, cloned.ID); ok && js != tc.want {
			t.Fatalf("%s: cloned form custom_js = %q, want %q", tc.name, js, tc.want)
		}
	}

	// The {form_id} branch (save an existing form as a template) is unchanged and
	// its clone still lands.
	code, raw := tplReq(t, fx.h, fx.key, http.MethodPost, "/v1/rest/templates",
		`{"form_id":"`+fx.formID+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("save-as-template status = %d, want 201 (body %s)", code, raw)
	}
	var fromForm struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &fromForm); err != nil || fromForm.ID == "" {
		t.Fatalf("decode save-as-template (%v): %s", err, raw)
	}
	code, raw = tplReq(t, fx.h, fx.key, http.MethodPost, "/v1/rest/templates/"+fromForm.ID+"/use", `{}`)
	if code != http.StatusCreated {
		t.Fatalf("use of a form-derived template = %d, want 201 (body %s)", code, raw)
	}
}
