package mcpsrv

// Card t_65a88197, the MCP half of the write door.
//
// The card's acceptance names BOTH doors: a form created through REST or MCP
// with a pricing block that carries no `id` at all must either be refused with
// a 400 naming the block, or take an order. The fix chosen is the second arm —
// the charge side now keys an id-less pricing block by its positional submit key
// (formfields.PricingKey), so no door has to refuse anything.
//
// This pins the MCP door's half: `upsert_form` stores the config it is handed,
// without inventing an `id` for the pricing block (a stored id would key the
// charge differently from a legacy row) and without refusing it as unpriced
// (its choice carries the price). It is the door's contract, and it is
// deliberately a NON-biting arm: the door already accepted this shape; what was
// broken was the charge side, pinned in
// httpapi/pricing_block_idless_id_t_65a88197_test.go.
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL); skipped
// when unset.

import (
	"context"
	"encoding/json"
	"testing"
)

func TestMCPUpsertFormStoresAnIdlessPricingBlockVerbatim(t *testing.T) {
	st := issue501MCPFixture(t)

	cfg := map[string]any{"blocks": []any{
		map[string]any{"type": "option", "value": "Item", "required": true,
			"choices": []any{map[string]any{"label": "One", "value": "one", "price_sen": 1000}}},
		map[string]any{"type": "email", "value": "Emel", "required": true},
	}}

	_, out, err := st.srv.upsertForm(st.ctx, nil, upsertFormIn{
		Title:  "Id-less pricing block",
		Config: cfg,
	})
	if err != nil {
		t.Fatalf("upsert_form with a priced option block that carries NO id: %v.\n"+
			"The card's acceptance is a 400 naming the block or a chargeable form; this shape is storable.", err)
	}
	formID := out["form_id"]
	if formID == "" {
		t.Fatalf("upsert_form returned no form_id: %v", out)
	}

	var raw []byte
	if err := st.pool.QueryRow(context.Background(),
		`SELECT config FROM forms WHERE id=$1 AND account_id=$2`, formID, st.accountID).Scan(&raw); err != nil {
		t.Fatalf("read stored config for %s: %v", formID, err)
	}
	var stored struct {
		Blocks []struct {
			Type  string `json:"type"`
			ID    string `json:"id"`
			Value string `json:"value"`
		} `json:"blocks"`
	}
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatalf("decode stored config: %v (%s)", err, raw)
	}
	if len(stored.Blocks) != 2 {
		t.Fatalf("stored blocks = %+v, want the option and email blocks", stored.Blocks)
	}
	if stored.Blocks[0].Type != "option" || stored.Blocks[0].ID != "" {
		t.Fatalf("stored option block = %+v, want type \"option\" with NO id — the door stores the config verbatim, "+
			"so the charge keys it positionally (formfields.PricingKey)", stored.Blocks[0])
	}
}
