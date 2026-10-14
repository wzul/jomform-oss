package mcpsrv

// Card t_4086b8f0 — the MCP half of the unsupported-block refusal.
//
// A response form's `file` block is refused at write time (see
// formfields.UnsupportedBlock for the whole argument: no upload path exists for
// an anonymous buyer, so a stored block would promise an attachment that can
// never arrive). The agent's write paths store a form/template config verbatim,
// so they must apply the SAME rule as the REST routes — otherwise `upsert_form`
// is a way around the refusal.
//
// Assertions are read from the DATABASE where a write is being refused: the
// defect class is a config that is stored while the caller is told it was not
// (card t_0b7980f5), so the error message alone is not evidence.
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL); skipped
// when unset.

import (
	"context"
	"strings"
	"testing"
)

// mustRejectUnsupportedBlock asserts the tool refused a config carrying a block
// type no renderer can honour, with a message that NAMES the type (the
// merchant/agent has to know which block to delete).
func mustRejectUnsupportedBlock(t *testing.T, what string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: a config with a `file` block was ACCEPTED — the block is stored and the "+
			"buyer's page cannot serve it", what)
	}
	if !strings.Contains(err.Error(), "file") {
		t.Errorf("%s: the refusal does not name the block type: %v", what, err)
	}
}

// TestMCPUpsertFormRefusesAFileBlock covers both halves of upsert_form: a new
// form carrying the block is refused (and nothing is inserted), and an update
// that would add it to an existing form is refused with the stored config
// untouched.
func TestMCPUpsertFormRefusesAFileBlock(t *testing.T) {
	st := issue501MCPFixture(t)
	fileCfg := map[string]any{"mode": "response", "blocks": []any{
		map[string]any{"id": "fl1", "type": "file", "value": "Resit bank"},
	}}

	_, _, err := st.srv.upsertForm(st.ctx, nil, upsertFormIn{
		Title: "With attachment", Config: fileCfg,
	})
	mustRejectUnsupportedBlock(t, "upsert_form (create)", err)

	var stored int
	if qerr := st.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM forms WHERE account_id=$1 AND title='With attachment'`,
		st.accountID).Scan(&stored); qerr != nil {
		t.Fatalf("count forms: %v", qerr)
	}
	if stored != 0 {
		t.Errorf("the refused upsert_form create still stored %d form(s)", stored)
	}

	// The update path: the form keeps the config it had.
	formID := st.seedForm(t, "i-blk-"+randHex(4))
	_, _, err = st.srv.upsertForm(st.ctx, nil, upsertFormIn{
		FormID: formID, Title: "Issue 501 file", Config: fileCfg,
	})
	mustRejectUnsupportedBlock(t, "upsert_form (update)", err)

	var blocks int
	if qerr := st.pool.QueryRow(context.Background(),
		`SELECT COALESCE(jsonb_array_length(config->'blocks'), 0) FROM forms WHERE id=$1`,
		formID).Scan(&blocks); qerr != nil {
		t.Fatalf("read stored blocks: %v", qerr)
	}
	if blocks != 0 {
		t.Errorf("the refused upsert_form update stored %d block(s) — the form now carries a block "+
			"the buyer's page cannot serve", blocks)
	}

	// The same tool still accepts a servable config, so the rule is not a
	// blanket refusal of the write path.
	if _, _, err := st.srv.upsertForm(st.ctx, nil, upsertFormIn{
		FormID: formID, Title: "Issue 501 file",
		Config: map[string]any{"mode": "response", "blocks": []any{
			map[string]any{"id": "tx1", "type": "text", "value": "Nama"},
		}},
	}); err != nil {
		t.Fatalf("upsert_form with a servable text block rejected: %v", err)
	}
}

// TestMCPTemplateWritesRefuseAFileBlock: a template's config is cloned onto a
// real form by use_template, so a file block stored in a template would bypass
// the refusal on the form write paths entirely.
func TestMCPTemplateWritesRefuseAFileBlock(t *testing.T) {
	st := issue501MCPFixture(t)
	fileCfg := map[string]any{"blocks": []any{
		map[string]any{"id": "fl1", "type": "file", "value": "Resit bank"},
	}}

	_, _, err := st.srv.createTemplateMCP(st.ctx, nil, createTemplateIn{Title: "tpl with file", Config: fileCfg})
	mustRejectUnsupportedBlock(t, "create_template", err)

	_, out, err := st.srv.createTemplateMCP(st.ctx, nil, createTemplateIn{Title: "clean tpl"})
	if err != nil {
		t.Fatalf("create_template with no config rejected: %v", err)
	}
	tplID, _ := out["template_id"].(string)
	if tplID == "" {
		t.Fatalf("create_template returned no template_id: %v", out)
	}

	_, _, err = st.srv.updateTemplateMCP(st.ctx, nil, updateTemplateIn{TemplateID: tplID, Config: fileCfg})
	mustRejectUnsupportedBlock(t, "update_template", err)

	var blocks int
	if qerr := st.pool.QueryRow(context.Background(),
		`SELECT COALESCE(jsonb_array_length(config->'blocks'), 0) FROM forms WHERE id=$1`,
		tplID).Scan(&blocks); qerr != nil {
		t.Fatalf("read stored template blocks: %v", qerr)
	}
	if blocks != 0 {
		t.Errorf("the refused update_template stored %d block(s)", blocks)
	}
}
