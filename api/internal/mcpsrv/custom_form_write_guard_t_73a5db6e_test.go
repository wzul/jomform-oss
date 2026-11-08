package mcpsrv

// Card t_73a5db6e — the MCP half. `upsert_form` is the path an agent uses to
// write a custom-HTML response form (issue #255), and it is the path that could
// store an unsatisfiable contract: customform.Validate checked the field list
// but never the HTML, so `fields:[{field_id:"nama", required:true}]` with no
// name="nama" control was accepted. Every submit from the buyer's page then
// answered 400 `medan wajib tidak diisi: Nama`, and nothing on the buyer side
// could satisfy it.
//
// Two doors are pinned here, because they reach the guard through different
// code: `custom_html`+`custom_fields` (applyCustomFormConfig) and a raw
// `config.custom_html` map (the config rules beside the other urlguard/
// formfields checks). A guard on only one of them would leave the other open.
// The template writers are pinned too: a template's config is cloned onto a
// real form by use_template, so an unsatisfiable contract stored there reaches a
// buyer's page by the back door.
//
// Assertions read the DATABASE where a write is refused: the defect class is a
// config that is stored while the caller is told it was not.
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL); skipped
// when unset.

import (
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/customform"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// unsatisfiableCustomCfg is the card's shape as a raw config map: one required
// field, and a body whose only control has a different name.
func unsatisfiableCustomCfg() map[string]any {
	return map[string]any{
		"mode": "response",
		customform.ConfigKey: map[string]any{
			"html":   `<div><label>Emel</label><input type="email" name="emel"></div>`,
			"fields": []any{map[string]any{"field_id": "nama", "type": "text", "label": "Nama", "required": true}},
		},
	}
}

func mustRejectUnsubmittableCustom(t *testing.T, what string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: an unsatisfiable custom-form contract was ACCEPTED — the form is stored "+
			"and every submit answers 400 `medan wajib tidak diisi`", what)
	}
	if !strings.Contains(err.Error(), "required field missing from the HTML") ||
		!strings.Contains(err.Error(), `name="nama"`) {
		t.Errorf("%s: refusal = %q, want it to name the missing control (name=\"nama\")", what, err)
	}
}

func TestUpsertFormRefusesUnsubmittableCustomForm(t *testing.T) {
	st := issue501MCPFixture(t)
	forms := dbstore.NewFormStore(st.pool)
	formID := st.seedForm(t, "i-cf-"+randHex(4))
	required := []customform.Field{{ID: "nama", Type: "text", Label: "Nama", Required: true}}

	// Door 1: the documented custom_html + custom_fields inputs.
	_, _, err := st.srv.upsertForm(st.ctx, nil, upsertFormIn{
		FormID: formID, Title: "Borang tersuai",
		CustomHTML:   `<div><label>Emel</label><input type="email" name="emel"></div>`,
		CustomFields: required,
	})
	mustRejectUnsubmittableCustom(t, "upsert_form (custom_html)", err)
	stored, gerr := forms.GetByID(st.ctx, st.accountID, formID)
	if gerr != nil {
		t.Fatalf("read back form: %v", gerr)
	}
	if customform.Enabled(stored.Config) {
		t.Fatalf("the refused contract was stored anyway: config = %v", stored.Config)
	}

	// Door 2: config.custom_html sent directly (no custom_html/custom_fields
	// inputs) — a different call site of the same rule, on both halves of
	// upsert_form: a create carries the config through the create branch, an
	// update through the update branch.
	_, _, err = st.srv.upsertForm(st.ctx, nil, upsertFormIn{
		Title:  "Borang tersuai cipta",
		Config: unsatisfiableCustomCfg(),
	})
	mustRejectUnsubmittableCustom(t, "upsert_form (create, config.custom_html)", err)
	var created int
	if qerr := st.pool.QueryRow(st.ctx,
		`SELECT count(*) FROM forms WHERE account_id=$1 AND title='Borang tersuai cipta'`,
		st.accountID).Scan(&created); qerr != nil {
		t.Fatalf("count forms: %v", qerr)
	}
	if created != 0 {
		t.Errorf("the refused upsert_form create stored %d form(s)", created)
	}
	_, _, err = st.srv.upsertForm(st.ctx, nil, upsertFormIn{
		FormID: formID, Title: "Borang tersuai", Config: unsatisfiableCustomCfg(),
	})
	mustRejectUnsubmittableCustom(t, "upsert_form (update, config.custom_html)", err)

	// A satisfiable contract is accepted and lands in the stored config, so the
	// guard refuses the contract and not the feature.
	_, _, err = st.srv.upsertForm(st.ctx, nil, upsertFormIn{
		FormID: formID, Title: "Borang tersuai",
		CustomHTML:   `<div><input type="text" name="nama"></div>`,
		CustomFields: required,
	})
	if err != nil {
		t.Fatalf("upsert_form with a satisfiable custom form: %v", err)
	}
	stored, gerr = forms.GetByID(st.ctx, st.accountID, formID)
	if gerr != nil {
		t.Fatalf("read back form: %v", gerr)
	}
	cc := customform.FromConfig(stored.Config)
	if cc == nil || !strings.Contains(cc.HTML, `name="nama"`) || len(cc.Fields) != 1 {
		t.Fatalf("stored custom config = %+v, want the html and the one field", cc)
	}
	if dbstore.FormMode(stored) != dbstore.FormModeResponse {
		t.Errorf("stored mode = %q, want response", dbstore.FormMode(stored))
	}
}

// TestMCPTemplateWritesRefuseUnsubmittableCustomForm: a template's config is
// cloned onto a real form by use_template, so a guard only on the form writers
// is a bypass.
func TestMCPTemplateWritesRefuseUnsubmittableCustomForm(t *testing.T) {
	st := issue501MCPFixture(t)
	bad := unsatisfiableCustomCfg()

	_, _, err := st.srv.createTemplateMCP(st.ctx, nil, createTemplateIn{Title: "tpl trap", Config: bad})
	mustRejectUnsubmittableCustom(t, "create_template", err)

	// A clean template, then the update path.
	_, out, err := st.srv.createTemplateMCP(st.ctx, nil, createTemplateIn{Title: "clean tpl"})
	if err != nil {
		t.Fatalf("create_template with no config rejected: %v", err)
	}
	tplID, _ := out["template_id"].(string)
	if tplID == "" {
		t.Fatalf("create_template returned no template_id: %v", out)
	}
	_, _, err = st.srv.updateTemplateMCP(st.ctx, nil, updateTemplateIn{TemplateID: tplID, Config: bad})
	mustRejectUnsubmittableCustom(t, "update_template", err)

	// Nothing was saved silently by the refused update.
	var section *string
	if qerr := st.pool.QueryRow(st.ctx,
		`SELECT config->'custom_html'->>'html' FROM forms WHERE id=$1`, tplID).Scan(&section); qerr != nil {
		t.Fatalf("read stored template config: %v", qerr)
	}
	if section != nil {
		t.Fatalf("the refused update_template stored custom_html.html = %q", *section)
	}

	// The same tool still stores a satisfiable contract.
	good := map[string]any{
		"mode": "response",
		customform.ConfigKey: map[string]any{
			"html":   `<div><input type="text" name="nama"></div>`,
			"fields": []any{map[string]any{"field_id": "nama", "type": "text", "required": true}},
		},
	}
	if _, _, err := st.srv.updateTemplateMCP(st.ctx, nil, updateTemplateIn{TemplateID: tplID, Config: good}); err != nil {
		t.Fatalf("update_template with a satisfiable custom form rejected: %v", err)
	}
}
