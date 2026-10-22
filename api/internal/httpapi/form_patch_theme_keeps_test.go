package httpapi

// Regression guard for card t_106955f6 — the Form Settings General Save used to
// delete every theme key it does not own.
//
// THE DEFECT, measured on prod (form 08a43fd7-6a31-4d94-b844-8a9382fca605,
// v1.21.259, one click on the General tab's visible Save):
//
//	before  {"layout":"centered","primary":"#b45309","spacing":"comfortable",
//	         "product_image_size":"large","product_image_layout":"image-top"}
//	after   {"primary":"#b45309","product_image_size":"large",
//	         "product_image_layout":"image-top"}                       layout/spacing GONE
//
// The dashboard's payload carried a theme built from the four keys that tab owns
// (primary, product_image_size, product_image_layout, custom_js) and this route
// writes a PRESENT theme as-is (FormStore.Update COALESCEs only NULL — the
// documented partial-update contract the title/config arms beside it share). So
// a map that named four keys REPLACED the document and deleted the rest.
//
// It is not cosmetic. The block renderer emits its layout/spacing CSS only when
// those keys exist:
//
//	{{if .ThemeLayout}} body { justify-content: …; padding: … } .card { max-width: … } {{end}}
//	{{if .ThemeSpacing}} .card { padding: … } {{end}}        (render_blocks.go)
//
// so a routine save changed the BUYER-FACING page with no error and nothing in
// the UI. `set_form_theme`/`create_form` and POST /templates/use are where those
// keys enter, so a form created with layout/spacing is exactly the row at risk.
//
// WHERE THE FIX LIVES, AND WHY THIS FILE STILL MATTERS
// The web client now sends back the theme it READ, laid over the keys it owns
// (web/src/lib/formSettingsTheme.ts — pinned by its own unit test and by the
// browser case in web/e2e/form-settings-general-save-persists-logo.spec.ts).
// That is deliberate: only the client knows which keys it owns, and the API
// cannot tell "delete this key" from "never had it" in a bare map. This file
// pins the two halves the client's own tests cannot reach — that the ROUND TRIP
// really lands in the ROW (asserted on the database, not on the 200) and that
// the public page really keeps serving the layout/spacing rules afterwards.
//
// Both arms run the real mux and the real Postgres (JOMFORM_TEST_DATABASE_URL);
// the page is read out of the real renderer. A fake store would let the guard
// pass while the statement still wrote exactly what it was handed.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// themeStoredOnTheRow reads a form's theme jsonb straight out of the database.
// Every assertion about what was saved goes through here rather than through the
// PATCH response, because a 200 that lied was half of the original defect class
// (see form_patch_title_keeps_test.go).
func (fx renameFixture) themeStoredOnTheRow(t *testing.T) map[string]any {
	t.Helper()
	var raw []byte
	if err := fx.pool.QueryRow(fx.ctx,
		`SELECT theme FROM forms WHERE id=$1 AND account_id=$2`, fx.formID, fx.accountID).Scan(&raw); err != nil {
		t.Fatalf("read stored theme: %v", err)
	}
	var theme map[string]any
	if err := json.Unmarshal(raw, &theme); err != nil {
		t.Fatalf("decode stored theme %s: %v", raw, err)
	}
	return theme
}

// ThemeLayout/ThemeSpacing CSS the block renderer emits for the seeded values.
// Written out rather than derived: the point is that these EXACT rules are
// served before and after the save, and a helper that re-derived them could
// drift with the template without anything going red.
var (
	layoutCenterCSS = "body { justify-content: center; }"
	layoutWidthCSS  = ".card { max-width: 480px; }"
	spacingCSS      = ".card { padding: 32px; }"
	// customCSSRule is the merchant's own stylesheet (theme key `custom_css`).
	// Written out for the same reason as the three above, and shared with the
	// seeded row below so the page assertion and the seed cannot drift.
	customCSSRule = ".card { box-shadow: none; }"
)

// TestFormPatchRoundTrippedThemeKeepsUnownedKeysAndTheirCSS is the card's
// acceptance in one path: seed a form carrying the keys the General tab does
// not own (as `create_form`/`templates/use` do), PATCH with the payload the
// General tab now sends, then assert BOTH the row and the rendered page.
func TestFormPatchRoundTrippedThemeKeepsUnownedKeysAndTheirCSS(t *testing.T) {
	fx := newRenameFixture(t, "it-theme-keep-"+randHex(4))

	// The row `templates/use` / `set_form_theme` would have produced: the prod
	// document that lost two keys, plus the other unowned keys the renderer reads
	// so a fix that kept only layout/spacing is still caught.
	seeded := map[string]any{
		"layout":               "centered",
		"spacing":              "comfortable",
		"background":           "#eeeeee",
		"text":                 "#111111",
		"card_background":      "#ffffff",
		"card_border_radius":   "12px",
		"font_family":          "Georgia, serif",
		"custom_css":           customCSSRule,
		"custom_js":            "window.__jfOld = true;",
		"primary":              "#5e6ad2",
		"product_image_size":   "small",
		"product_image_layout": "thumbnail-left",
	}
	raw, err := json.Marshal(seeded)
	if err != nil {
		t.Fatalf("encode seeded theme: %v", err)
	}
	if _, err := fx.pool.Exec(fx.ctx, `UPDATE forms SET theme=$2::jsonb WHERE id=$1`, fx.formID, raw); err != nil {
		t.Fatalf("seed the theme: %v", err)
	}

	// PRE-CONDITION: the page already serves the layout/spacing rules. Without
	// this, "the rules are still there afterwards" could be satisfied by a page
	// that rendered them from somewhere else — or by nothing at all.
	before := publicServe(t, fx.pub, fx.host, "/"+fx.slug)
	if before.Code != http.StatusOK {
		t.Fatalf("GET /%s before the save = %d, want 200 (body %s)", fx.slug, before.Code, before.Body.String())
	}
	for _, rule := range []string{layoutCenterCSS, layoutWidthCSS, spacingCSS} {
		if !strings.Contains(before.Body.String(), rule) {
			t.Fatalf("pre-condition: the page did not serve %q before the save, so this case would prove nothing (body %s)",
				rule, before.Body.String())
		}
	}
	// The seeded custom_js must already be served as RUNNABLE code, verbatim
	// inside its own <script> — not as a JSON-marshalled quoted string literal,
	// which is a valid no-op statement the browser evaluates and discards (card
	// t_cae9c98e). Asserted on this path as well as DB-free in
	// custom_js_script_block_test.go because this one is the end-to-end route: a
	// real row, a real public GET.
	if !strings.Contains(before.Body.String(), "<script>\nwindow.__jfOld = true;\n</script>") {
		t.Fatalf("pre-condition: the page does not serve the stored custom_js verbatim inside a <script> "+
			"element, so the seeded row's JS is dead on the page (card t_cae9c98e). body %s",
			before.Body.String())
	}
	if strings.Contains(before.Body.String(), `"window.__jfOld = true;"`) {
		t.Fatalf("pre-condition: the page serves custom_js as a quoted string literal — the browser " +
			"evaluates a no-op and the merchant's code never runs (card t_cae9c98e)")
	}

	// The General tab's payload, exactly as the page builds it (see
	// web/src/lib/formSettingsTheme.ts): the theme it READ, laid over the keys it
	// owns; a cleared custom-JS field DROPS its key instead of writing "".
	payloadTheme := map[string]any{}
	for k, v := range seeded {
		payloadTheme[k] = v
	}
	payloadTheme["primary"] = "#b45309"
	payloadTheme["product_image_size"] = "large"
	payloadTheme["product_image_layout"] = "image-top"
	delete(payloadTheme, "custom_js")
	body, err := json.Marshal(map[string]any{
		"slug":  fx.slug,
		"title": "Western menu",
		"theme": payloadTheme,
	})
	if err != nil {
		t.Fatalf("encode PATCH body: %v", err)
	}

	code, resp := fx.patch(t, string(body))
	if code != http.StatusOK {
		t.Fatalf("General-tab PATCH = %d, want 200 (body %s)", code, resp)
	}

	// ── 1. THE ROW. Every key the payload carried is what the row now holds —
	// asserted on the database, because the defect was a save that answered 200
	// while the document under it had lost entries.
	got := fx.themeStoredOnTheRow(t)
	for key, want := range payloadTheme {
		if got[key] != want {
			t.Errorf("forms.theme[%q] = %v after the save, want %v (row: %v)", key, got[key], want, got)
		}
	}
	for _, kept := range []string{"layout", "spacing", "background", "text", "card_background", "card_border_radius", "font_family", "custom_css"} {
		if _, ok := got[kept]; !ok {
			t.Errorf("forms.theme lost %q on a General-tab save (row: %v).\n"+
				"The API writes a present theme as-is, so the client must round-trip the document it read — "+
				"otherwise one click deletes keys this tab has no control for, and the block renderer stops "+
				"emitting the CSS they drive.", kept, got)
		}
	}
	// The owned key that was CLEARED must really be gone (the round trip must not
	// make the custom-JS field unclearable).
	if _, ok := got["custom_js"]; ok {
		t.Errorf("forms.theme still holds custom_js after the field was cleared (row: %v)", got)
	}

	// ── 2. THE BUYER-FACING PAGE. The layout/spacing rules the renderer emits
	// only when those keys exist must be exactly as they were, and the colour the
	// merchant DID change must have landed.
	after := publicServe(t, fx.pub, fx.host, "/"+fx.slug)
	if after.Code != http.StatusOK {
		t.Fatalf("GET /%s after the save = %d, want 200 (body %s)", fx.slug, after.Code, after.Body.String())
	}
	page := after.Body.String()
	for _, rule := range []string{layoutCenterCSS, layoutWidthCSS, spacingCSS} {
		if !strings.Contains(page, rule) {
			t.Errorf("the public page no longer serves %q after a General-tab save.\n"+
				"That is the carded defect: the save deleted the theme keys the rule is conditional on, so a "+
				"routine edit silently re-styled the buyer's page.", rule)
		}
	}
	// The merchant's stylesheet must reach the page too. This was the one theme
	// key the renderer could not serve: it was typed template.HTML inside a
	// `<style>` element, so html/template's CSS-context filter replaced it with
	// the `ZgotmplZ` sentinel (card t_a7bf0fb2, fixed in blockThemeView). Asserted
	// here as well as in custom_css_style_block_test.go because this path is the
	// end-to-end one — a real row, a real public GET.
	if !strings.Contains(page, customCSSRule) {
		t.Errorf("the public page does not serve the stored custom_css rule %q — the merchant's stylesheet "+
			"is stored but never applied (card t_a7bf0fb2)", customCSSRule)
	}
	if strings.Contains(page, "ZgotmplZ") {
		t.Errorf("the public page serves the html/template `ZgotmplZ` sentinel — the CSS context filtered the " +
			"merchant's custom_css instead of serving it (card t_a7bf0fb2)")
	}
	if !strings.Contains(page, "--jf-primary: #b45309") {
		t.Errorf("the page does not serve the colour this save set (--jf-primary: #b45309) — the control that "+
			"keeps the assertions above from passing on a save that wrote nothing (body %s)", page)
	}

	// ── 3. The stored row is readable through the API the page hydrates from, so
	// the NEXT save round-trips the SAME document rather than a reconstruction.
	_, detail := tplReq(t, fx.h, fx.key, http.MethodGet, "/v1/rest/forms/"+fx.formID, "")
	var formDetail struct {
		Theme map[string]any `json:"theme"`
	}
	if err := json.Unmarshal(detail, &formDetail); err != nil {
		t.Fatalf("decode form GET: %v (%s)", err, detail)
	}
	if formDetail.Theme["layout"] != "centered" || formDetail.Theme["spacing"] != "comfortable" {
		t.Errorf("GET /v1/rest/forms/{id} theme = %v, want layout/spacing preserved — the console hydrates the "+
			"next General save from this payload, so a lossy read re-introduces the loss on the save after",
			formDetail.Theme)
	}
}

// TestFormPatchThemeReplacesTheDocumentWhenPresent states the API contract this
// fix is built on, in both directions, so the client-side round trip cannot be
// removed on the assumption that the server merges.
//
// It is not a preference: FormStore.Update COALESCEs only NULL, and the same
// contract is what lets a caller clear a key by leaving it out of a full map
// (the dashboard's "clear the custom JS" flow). If this ever becomes a merge,
// this arm goes red and the change is deliberate — which is the point: a client
// that stops round-tripping because "the server merges now" would be relying on
// a behaviour nothing had pinned.
func TestFormPatchThemeReplacesTheDocumentWhenPresent(t *testing.T) {
	fx := newRenameFixture(t, "it-theme-replace-"+randHex(4))

	raw, _ := json.Marshal(map[string]any{"layout": "centered", "primary": "#5e6ad2"})
	if _, err := fx.pool.Exec(fx.ctx, `UPDATE forms SET theme=$2::jsonb WHERE id=$1`, fx.formID, raw); err != nil {
		t.Fatalf("seed the theme: %v", err)
	}

	// An OMITTED theme keeps the stored document (issue #16's contract).
	if code, resp := fx.patch(t, `{"title":"Western menu"}`); code != http.StatusOK {
		t.Fatalf("title-only PATCH = %d, want 200 (body %s)", code, resp)
	}
	if got := fx.themeStoredOnTheRow(t); got["layout"] != "centered" {
		t.Errorf("an omitted theme must keep the stored document, got %v", got)
	}

	// A PRESENT theme is written as-is: this is why the General tab must send the
	// full document, and it is the measured behaviour the prod defect rode on.
	if code, resp := fx.patch(t, `{"theme":{"primary":"#b45309"}}`); code != http.StatusOK {
		t.Fatalf("theme-only PATCH = %d, want 200 (body %s)", code, resp)
	}
	if got := fx.themeStoredOnTheRow(t); got["layout"] != nil {
		t.Errorf("a PRESENT theme is documented as a replacement, but the row still holds layout=%v. "+
			"If the API now merges, update web/src/lib/formSettingsTheme.ts and this expectation in the "+
			"same change — do not leave the client round-tripping against a contract nobody pins.", got["layout"])
	}

	// ...and the ROW is what the page serves: with `layout` gone, the renderer's
	// layout rules are gone too. That is the buyer-visible half of the prod
	// measurement, pinned here so the damage is reproducible without prod.
	rec := publicServe(t, fx.pub, fx.host, "/"+fx.slug)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /%s = %d, want 200", fx.slug, rec.Code)
	}
	if strings.Contains(rec.Body.String(), layoutCenterCSS) {
		t.Errorf("the page still serves %q after the theme lost `layout` — the renderer's conditional is "+
			"what makes this defect visible to a buyer, so this arm cannot be trusted", layoutCenterCSS)
	}
}
