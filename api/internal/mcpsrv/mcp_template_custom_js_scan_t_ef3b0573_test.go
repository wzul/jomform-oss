package mcpsrv

// Card t_ef3b0573 — the MCP half of the theme.custom_js scan. The REST
// template routes were closed by t_5c35bfeb; the MCP template tools were the
// same bypass in another subsystem.
//
// A template's theme is cloned verbatim onto a real form by use_template, and
// since v1.21.283 the block-rendered buyer page serves theme.custom_js as live
// code. create_template / update_template ran every sibling guard (scheme,
// config shape, unsupported block, unsatisfiable custom form — each justified
// by that clone) but never customform.ScanCustomJS, so a value the form write
// paths refuse with `custom_js ditolak: <reason>` was storable on a template
// over MCP and then cloned onto a real form.
//
// Assertions read the DATABASE where a write is refused: the defect class is a
// value that is stored while the caller is told it was not.
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL); skipped
// when unset.
//
// BITE: delete the refuseBlockedCustomJS arm from createTemplateMCP → the
// create half stores eval('9*9'); from updateTemplateMCP → the update half
// replaces the benign value; from useTemplateMCP → the legacy blocked row is
// cloned and only the clone test goes red. Make the helper refuse every value
// → the benign / no-custom_js controls go red.

import (
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// blockedCustomJSSample is a value ScanCustomJS refuses and the form write
// paths answer 400 for. `eval(` is a hard active-code-execution marker.
const blockedCustomJSSample = "eval('9*9');"

// benignCustomJSSample is real form-behaviour JS: the scan must still accept
// it, so the guard is pinned on the value, not on "any custom_js is refused".
const benignCustomJSSample = "document.addEventListener('click', function () { return true; });"

// wantCustomJSRefusal pins the sentence door-to-door: the same prefix and the
// same reason ScanCustomJS produces, not merely "an error happened".
func wantCustomJSRefusal(t *testing.T, what string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: a blocked theme.custom_js was ACCEPTED — it can be cloned onto a "+
			"real form and served as live code", what)
	}
	if !strings.Contains(err.Error(), "custom_js ditolak: ") ||
		!strings.Contains(err.Error(), "active code execution marker: eval(") {
		t.Fatalf("%s: refusal = %q, want the form paths' sentence "+
			"`custom_js ditolak: active code execution marker: eval(`", what, err)
	}
}

// storedTemplateJS reads theme->>'custom_js' for a template row ("" when the
// key is absent).
func storedTemplateJS(t *testing.T, st issue501MCPState, id string) string {
	t.Helper()
	var js *string
	if err := st.pool.QueryRow(st.ctx,
		`SELECT theme->>'custom_js' FROM forms WHERE id=$1`, id).Scan(&js); err != nil {
		t.Fatalf("read stored theme.custom_js: %v", err)
	}
	if js == nil {
		return ""
	}
	return *js
}

// countFormsTitle counts the account's non-deleted rows with this title: a
// refused write must leave nothing behind.
func countFormsTitle(t *testing.T, st issue501MCPState, title string) int {
	t.Helper()
	var n int
	if err := st.pool.QueryRow(st.ctx,
		`SELECT count(*) FROM forms WHERE account_id=$1 AND title=$2 AND deleted_at IS NULL`,
		st.accountID, title).Scan(&n); err != nil {
		t.Fatalf("count forms: %v", err)
	}
	return n
}

// TestMCPTemplateWritesRefuseBlockedCustomJS pins create_template and
// update_template: a blocked value is refused with the form paths' sentence and
// nothing is stored, while a template with no custom_js and a benign one still
// create and update.
func TestMCPTemplateWritesRefuseBlockedCustomJS(t *testing.T) {
	st := issue501MCPFixture(t)

	// ── create: blocked → refused, no row ──────────────────────────────────
	_, _, err := st.srv.createTemplateMCP(st.ctx, nil, createTemplateIn{
		Title: "tpl blocked",
		Theme: map[string]any{"custom_js": blockedCustomJSSample},
	})
	wantCustomJSRefusal(t, "create_template", err)
	if n := countFormsTitle(t, st, "tpl blocked"); n != 0 {
		t.Fatalf("the refused create_template stored %d template row(s)", n)
	}

	// Same sentence as the form door that already scans (set_form_theme), so
	// the wording cannot drift door to door.
	formID := st.seedForm(t, "mcp-js-control-"+randHex(4))
	_, _, formErr := st.srv.setFormTheme(st.ctx, nil, setFormThemeIn{
		FormID: formID, CustomJS: blockedCustomJSSample,
	})
	if formErr == nil {
		t.Fatalf("set_form_theme accepted the blocked value — the reference door moved")
	}
	if got, want := err.Error(), formErr.Error(); got != want {
		t.Fatalf("create_template refusal = %q, form door refusal = %q — one rule, one sentence", got, want)
	}

	// ── create: benign → accepted and stored verbatim ──────────────────────
	_, out, err := st.srv.createTemplateMCP(st.ctx, nil, createTemplateIn{
		Title: "tpl benign",
		Theme: map[string]any{"custom_js": benignCustomJSSample},
	})
	if err != nil {
		t.Fatalf("create_template with benign custom_js rejected: %v", err)
	}
	tplID, _ := out["template_id"].(string)
	if tplID == "" {
		t.Fatalf("create_template returned no template_id: %v", out)
	}
	if got := storedTemplateJS(t, st, tplID); got != benignCustomJSSample {
		t.Fatalf("stored theme.custom_js = %q, want the benign script", got)
	}

	// ── update: blocked → refused, stored value unchanged ──────────────────
	_, _, err = st.srv.updateTemplateMCP(st.ctx, nil, updateTemplateIn{
		TemplateID: tplID,
		Theme:      map[string]any{"custom_js": blockedCustomJSSample},
	})
	wantCustomJSRefusal(t, "update_template", err)
	if got := storedTemplateJS(t, st, tplID); got != benignCustomJSSample {
		t.Fatalf("the refused update_template changed theme.custom_js to %q", got)
	}

	// ── update: nil theme (leave unchanged) → accepted ─────────────────────
	if _, _, err := st.srv.updateTemplateMCP(st.ctx, nil, updateTemplateIn{
		TemplateID: tplID, Title: "tpl benign renamed",
	}); err != nil {
		t.Fatalf("update_template with no theme rejected: %v", err)
	}
	if got := storedTemplateJS(t, st, tplID); got != benignCustomJSSample {
		t.Fatalf("a title-only update lost theme.custom_js: %q", got)
	}

	// ── update: benign → accepted and replaced ─────────────────────────────
	benign2 := "document.addEventListener('submit', function () { return true; });"
	if _, _, err := st.srv.updateTemplateMCP(st.ctx, nil, updateTemplateIn{
		TemplateID: tplID,
		Theme:      map[string]any{"custom_js": benign2},
	}); err != nil {
		t.Fatalf("update_template with a benign custom_js rejected: %v", err)
	}
	if got := storedTemplateJS(t, st, tplID); got != benign2 {
		t.Fatalf("stored theme.custom_js = %q, want the updated benign script", got)
	}

	// ── create: no theme at all → accepted (no behaviour change) ───────────
	_, out, err = st.srv.createTemplateMCP(st.ctx, nil, createTemplateIn{Title: "tpl no js"})
	if err != nil {
		t.Fatalf("create_template with no theme rejected: %v", err)
	}
	noJSID, _ := out["template_id"].(string)
	if noJSID == "" {
		t.Fatalf("create_template (no theme) returned no template_id: %v", out)
	}
	if got := storedTemplateJS(t, st, noJSID); got != "" {
		t.Fatalf("a template created without theme carries custom_js = %q", got)
	}
}

// TestMCPUseTemplateRefusesCloningBlockedCustomJS pins the clone door. The row
// is inserted through the STORE, not the write tool, so it stands for a
// template that predates this guard (or was written by any other door): the
// clone must refuse it, and must still clone a clean or no-js template.
func TestMCPUseTemplateRefusesCloningBlockedCustomJS(t *testing.T) {
	st := issue501MCPFixture(t)

	seedLegacyTemplate := func(title string, theme map[string]any) string {
		t.Helper()
		f := &dbstore.Form{
			AccountID: st.accountID,
			Slug:      "legacy-" + randHex(6),
			Title:     title,
			Config:    map[string]any{},
			Theme:     theme,
		}
		if err := st.srv.deps.Forms.CreateTemplate(st.ctx, f); err != nil {
			t.Fatalf("seed legacy template: %v", err)
		}
		return f.ID
	}

	countClonedForms := func() int {
		t.Helper()
		var n int
		if err := st.pool.QueryRow(st.ctx,
			`SELECT count(*) FROM forms WHERE account_id=$1 AND NOT is_template AND deleted_at IS NULL`,
			st.accountID).Scan(&n); err != nil {
			t.Fatalf("count clone-able forms: %v", err)
		}
		return n
	}

	// ── the legacy blocked row cannot become live code ────────────────────
	blockedTpl := seedLegacyTemplate("legacy blocked", map[string]any{
		"custom_js": blockedCustomJSSample,
	})
	if got := storedTemplateJS(t, st, blockedTpl); got != blockedCustomJSSample {
		t.Fatalf("seed did not store the blocked value: %q", got)
	}
	before := countClonedForms()
	_, _, err := st.srv.useTemplateMCP(st.ctx, nil, useTemplateIn{TemplateID: blockedTpl})
	wantCustomJSRefusal(t, "use_template", err)
	if after := countClonedForms(); after != before {
		t.Fatalf("the refused use_template created %d form(s)", after-before)
	}
	// No form anywhere in the account carries the blocked value.
	var leaked int
	if qerr := st.pool.QueryRow(st.ctx,
		`SELECT count(*) FROM forms WHERE account_id=$1 AND NOT is_template
		   AND theme->>'custom_js' = $2`, st.accountID, blockedCustomJSSample).Scan(&leaked); qerr != nil {
		t.Fatalf("count leaked forms: %v", qerr)
	}
	if leaked != 0 {
		t.Fatalf("%d non-template form(s) carry the blocked custom_js", leaked)
	}

	// ── control: a benign legacy template still clones, value carried ──────
	benignTpl := seedLegacyTemplate("legacy benign", map[string]any{
		"custom_js": benignCustomJSSample,
	})
	_, out, err := st.srv.useTemplateMCP(st.ctx, nil, useTemplateIn{TemplateID: benignTpl, Name: "benign clone"})
	if err != nil {
		t.Fatalf("use_template on a benign template rejected: %v", err)
	}
	cloneID := out["form_id"]
	if cloneID == "" {
		t.Fatalf("use_template returned no form_id: %v", out)
	}
	if got := storedTemplateJS(t, st, cloneID); got != benignCustomJSSample {
		t.Fatalf("cloned form theme.custom_js = %q, want the benign script", got)
	}

	// ── control: a template with no custom_js still clones ─────────────────
	noJSTpl := seedLegacyTemplate("legacy no js", map[string]any{"primary": "#5e6ad2"})
	_, out, err = st.srv.useTemplateMCP(st.ctx, nil, useTemplateIn{TemplateID: noJSTpl, Name: "no-js clone"})
	if err != nil {
		t.Fatalf("use_template on a template without custom_js rejected: %v", err)
	}
	noJSCLone := out["form_id"]
	if noJSCLone == "" {
		t.Fatalf("use_template (no js) returned no form_id: %v", out)
	}
	if got := storedTemplateJS(t, st, noJSCLone); got != "" {
		t.Fatalf("a template without custom_js cloned custom_js = %q", got)
	}
}
