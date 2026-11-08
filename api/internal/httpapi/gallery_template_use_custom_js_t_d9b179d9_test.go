package httpapi

// Card t_d9b179d9 — the GALLERY clone door had no custom_js scan.
//
// t_5c35bfeb added the scan to the three /v1/rest/templates doors and to
// useOwnTemplate, but the older clone door POST /v1/templates/use (registered at
// rest.go:292, handler useTemplate at rest_templates.go:54) copies tpl.Theme
// verbatim with no refusal. Measured on prod v1.21.303 @ adc2534a against the
// same template row, 2026-10-23T19:39Z:
//
//	POST /v1/rest/templates/<id>/use   -> 400 form.customJsRejected
//	POST /v1/templates/use {"template_id":"<id>"} -> 201, and the clone row read
//	  back theme->>'custom_js' = "eval(7*7);"
//
// The clone is a normal draft form, and custom_js is scanned only at SAVE time,
// so publishing it serves the blocked script verbatim on the buyer's page
// (render_blocks.go customJSFromTheme / render_custom.go — no scan at publish or
// render). That is the same hole class t_5c35bfeb closed on the REST door.
//
// WHAT THIS FILE PINS
//  1. POST /v1/templates/use {"template_id"} — the reachable branch, because it
//     names a row the caller owns — refuses a blocked custom_js with the SAME
//     code and sentence as the form/REST-template doors, and creates no form.
//     The row is seeded directly through the store (both write doors refuse the
//     value now), reproducing the state the card was measured in: a row written
//     before the scan existed, or by a writer that never scanned.
//  2. The gallery {"slug"} branch is closed by the same arm. It resolves a
//     published template — which is not merchant-writable today, but is the same
//     copy of tpl.Theme, so leaving it open would re-arm the defect the moment a
//     published row carries the value (a legacy row, a bulk import, a future
//     writer). Both shapes resolve into one `tpl` in the handler, so one guard
//     covers both; this test drives both so a future split of the branches
//     cannot silently drop one.
//  3. The control: a template with no custom_js, or a benign one, still clones
//     201, the clone still carries the benign script, and — the part a
//     status-only test cannot see — publishing that clone SERVES the benign
//     script on the buyer's page. So the guard refuses the VALUE, not the door.
//
// BITE: delete the `refuseBlockedCustomJS(w, tpl.Theme)` arm from useTemplate and
// TestGalleryUseTemplateRefusesBlockedCustomJSByTemplateID /
// ...BySlug get 201 plus a form row carrying the blocked value, while the
// control test keeps passing — a failure names the scan, not the route.
//
// Runs the real mux with a real API key against real Postgres
// (JOMFORM_TEST_DATABASE_URL), like the rest of the template-door tests.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// cleanupProbeForms removes every form row this test created. It is not
// tidiness: ListTemplates (GET /v1/templates) is NOT account-scoped, so a
// published is_template row left behind is visible to every other test in the
// package — the gallery-metadata guard then reports it as a gap and fails on a
// fixture the test under judgement never created. The draft templates and the
// clone are removed for the same reason.
func cleanupProbeForms(t *testing.T, fx renameFixture) {
	t.Helper()
	t.Cleanup(func() {
		if _, err := fx.pool.Exec(fx.ctx, `DELETE FROM forms WHERE account_id=$1`, fx.accountID); err != nil {
			t.Errorf("clean up probe form rows for %s: %v", fx.accountID, err)
		}
	})
}

// seedUseTemplateRow inserts one template row carrying `theme` straight through
// the store. published=true makes it resolvable by slug (TemplateBySlug requires
// is_template AND status='published'); otherwise it stays the draft row
// CreateTemplate forces, which is what the {template_id} branch resolves.
func seedUseTemplateRow(t *testing.T, fx renameFixture, theme map[string]any, published bool) *dbstore.Form {
	t.Helper()
	tpl := &dbstore.Form{
		AccountID: fx.accountID,
		Slug:      "it-gal-tpl-" + randHex(6),
		Title:     "gallery template probe",
		Config: map[string]any{
			"mode":   "response",
			"blocks": []any{map[string]any{"id": "b1", "type": "text", "value": "hello"}},
		},
		Theme: theme,
	}
	if err := dbstore.NewFormStore(fx.pool).CreateTemplate(fx.ctx, tpl); err != nil {
		t.Fatalf("insert template row with theme %v: %v", theme, err)
	}
	if published {
		if _, err := fx.pool.Exec(fx.ctx,
			`UPDATE forms SET status='published' WHERE id=$1 AND account_id=$2`,
			tpl.ID, fx.accountID); err != nil {
			t.Fatalf("publish template row %s: %v", tpl.ID, err)
		}
	}
	return tpl
}

// galleryUseTemplate drives the older clone door through the real mux with a
// real API key — the route the dashboard's TemplatePicker posts to.
func galleryUseTemplate(t *testing.T, fx renameFixture, body string) (int, json.RawMessage) {
	t.Helper()
	return tplReq(t, fx.h, fx.key, http.MethodPost, "/v1/templates/use", body)
}

// formsCarryingCustomJS counts the account's live FORMS whose theme still holds
// exactly this custom_js value. A refusal that answered 400 after the INSERT
// would leave the count above zero and pass a status-only assertion.
func formsCarryingCustomJS(t *testing.T, fx renameFixture, js string) int {
	t.Helper()
	var n int
	if err := fx.pool.QueryRow(fx.ctx,
		`SELECT count(*) FROM forms
		 WHERE account_id=$1 AND NOT is_template AND deleted_at IS NULL
		   AND theme->>'custom_js' = $2`, fx.accountID, js).Scan(&n); err != nil {
		t.Fatalf("count forms carrying custom_js %q: %v", js, err)
	}
	return n
}

// TestGalleryUseTemplateRefusesBlockedCustomJSByTemplateID is the card's
// measured door: the {template_id} branch names a merchant-owned row, which is
// the reachable way a stored blocked value reaches a real form.
func TestGalleryUseTemplateRefusesBlockedCustomJSByTemplateID(t *testing.T) {
	fx := newRenameFixture(t, "it-gal-cjs-id-"+randHex(4))
	cleanupProbeForms(t, fx)

	for _, tc := range blockedCustomJSCases() {
		tpl := seedUseTemplateRow(t, fx, map[string]any{"custom_js": tc.js}, false)
		before := liveForms(t, fx)

		body := `{"template_id":` + jsonString(tpl.ID) + `}`
		code, raw := galleryUseTemplate(t, fx, body)
		if code != http.StatusBadRequest {
			t.Fatalf("POST /v1/templates/use {template_id} on a template carrying %q = %d (body %s), "+
				"want 400.\nA 201 here is the carded defect: the clone copies theme verbatim, and the "+
				"buyer's page serves it as live code once the form is published.", tc.js, code, raw)
		}
		if got := customJSRefusalCode(t, raw); got != CodeFormCustomJSRejected {
			t.Errorf("the gallery clone refusal carries code %q, want %q (body %s).\n"+
				"Without it the dashboard's template picker renders its own generic failure sentence "+
				"and names neither Custom JS nor the reason.", got, CodeFormCustomJSRejected, raw)
		}
		if got, want := decodeErrField(t, raw), "custom_js rejected: "+tc.wantReason; got != want {
			t.Errorf("refusal for %s = %q, want %q — one rule, one wording", tc.name, got, want)
		}
		if after := liveForms(t, fx); after != before {
			t.Fatalf("live forms went %d -> %d across a refused clone (%s), want no new form",
				before, after, tc.name)
		}
		if n := formsCarryingCustomJS(t, fx, tc.js); n != 0 {
			t.Fatalf("%d form rows carry %q after the refused clone (%s), want 0 — the value was "+
				"written anyway", n, tc.js, tc.name)
		}
	}
}

// TestGalleryUseTemplateRefusesBlockedCustomJSBySlug covers the gallery branch.
// It is not merchant-writable today, but it copies the same tpl.Theme, so an arm
// on the resolved template covers it at no extra cost — and a future writer that
// publishes a row carrying the value must not re-arm the defect.
func TestGalleryUseTemplateRefusesBlockedCustomJSBySlug(t *testing.T) {
	fx := newRenameFixture(t, "it-gal-cjs-slug-"+randHex(4))
	cleanupProbeForms(t, fx)
	const blocked = "eval('7*7');"

	tpl := seedUseTemplateRow(t, fx, map[string]any{"custom_js": blocked}, true)
	before := liveForms(t, fx)

	code, raw := galleryUseTemplate(t, fx, `{"slug":`+jsonString(tpl.Slug)+`}`)
	if code != http.StatusBadRequest {
		t.Fatalf("POST /v1/templates/use {slug} on a published template carrying %q = %d (body %s), "+
			"want 400 — the slug branch copies the same theme verbatim", blocked, code, raw)
	}
	if got := customJSRefusalCode(t, raw); got != CodeFormCustomJSRejected {
		t.Errorf("the gallery-slug refusal carries code %q, want %q (body %s)",
			got, CodeFormCustomJSRejected, raw)
	}
	if after := liveForms(t, fx); after != before {
		t.Fatalf("live forms went %d -> %d across a refused clone, want no new form", before, after)
	}
	if n := formsCarryingCustomJS(t, fx, blocked); n != 0 {
		t.Fatalf("%d form rows carry %q after the refused clone, want 0", n, blocked)
	}
}

// TestGalleryUseTemplateStillClonesBenignCustomJS is the control: the refusal is
// about the VALUE. A template with no custom_js, or a benign one, still clones,
// the clone stores exactly that script, and a published clone SERVES it — so a
// guard that started refusing the route (or stripping the script) fails here.
func TestGalleryUseTemplateStillClonesBenignCustomJS(t *testing.T) {
	fx := newRenameFixture(t, "it-gal-cjs-ok-"+randHex(4))
	cleanupProbeForms(t, fx)
	const benign = "document.addEventListener('click', function () { return true; });"

	cases := []struct {
		name  string
		theme map[string]any
		want  string
	}{
		{"no custom_js", map[string]any{"primary": "#111111"}, ""},
		{"empty custom_js", map[string]any{"custom_js": ""}, ""},
		{"benign custom_js", map[string]any{"custom_js": benign}, benign},
	}
	for _, tc := range cases {
		tpl := seedUseTemplateRow(t, fx, tc.theme, false)
		before := liveForms(t, fx)

		code, raw := galleryUseTemplate(t, fx, `{"template_id":`+jsonString(tpl.ID)+`}`)
		if code != http.StatusCreated {
			t.Fatalf("POST /v1/templates/use of a template with %s = %d (body %s), want 201",
				tc.name, code, raw)
		}
		var cloned struct {
			ID   string `json:"id"`
			Slug string `json:"slug"`
		}
		if err := json.Unmarshal(raw, &cloned); err != nil || cloned.ID == "" {
			t.Fatalf("decode clone (%v): %s", err, raw)
		}
		if got, ok := storedCustomJS(t, fx, cloned.ID); ok && got != tc.want {
			t.Fatalf("%s: cloned form custom_js = %q, want %q", tc.name, got, tc.want)
		}
		if after := liveForms(t, fx); after != before+1 {
			t.Fatalf("live forms = %d after a successful clone (%s), want %d", after, tc.name, before+1)
		}

		if tc.want == "" {
			continue
		}
		// The clone is a draft; publish it and read the buyer's page. This is what
		// the defect would have served — and what the benign value must still
		// serve after the guard lands.
		if _, err := fx.pool.Exec(fx.ctx,
			`UPDATE forms SET status='published' WHERE id=$1 AND account_id=$2`,
			cloned.ID, fx.accountID); err != nil {
			t.Fatalf("publish clone %s: %v", cloned.ID, err)
		}
		rec := publicServe(t, fx.pub, fx.host, "/"+cloned.Slug)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /%s for the published clone = %d, want 200 (body %s)",
				cloned.Slug, rec.Code, rec.Body.String())
		}
		if body := rec.Body.String(); !strings.Contains(body, benign) {
			t.Fatalf("the published clone does not serve the benign custom_js.\n"+
				"The guard must refuse the VALUE, not strip the theme: want the page to contain %q.\n"+
				"Got page of %d bytes.", benign, len(body))
		}
	}

	// The gallery {"slug"} branch still clones a benign published template too.
	benignTpl := seedUseTemplateRow(t, fx, map[string]any{"custom_js": benign}, true)
	code, raw := galleryUseTemplate(t, fx, `{"slug":`+jsonString(benignTpl.Slug)+`}`)
	if code != http.StatusCreated {
		t.Fatalf("POST /v1/templates/use {slug} of a benign published template = %d (body %s), want 201",
			code, raw)
	}
	var slugClone struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &slugClone); err != nil || slugClone.ID == "" {
		t.Fatalf("decode slug clone (%v): %s", err, raw)
	}
	if got, ok := storedCustomJS(t, fx, slugClone.ID); !ok || got != benign {
		t.Fatalf("the slug clone's custom_js = %q (present=%v), want %q", got, ok, benign)
	}
}
