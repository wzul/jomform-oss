package httpapi

// Card t_82fe9cb7 — the sibling of t_589dc006 (#1249): a form that carries a
// custom-HTML body AND leftover builder blocks.
//
// handleResponseSubmit validates a response-mode submit against the form's OWN
// contract. #1249 made the non-custom arm derive that contract from
// config.blocks[].required; applying the same derivation to a custom-HTML form
// refuses EVERY submit, because that form's page is rendered from
// custom_html, not from blocks (renderCustomForm wins, issue #255) and posts
// only the names in custom_html.fields. Every required block is then absent
// from the payload and the buyer gets 400 "medan wajib tidak diisi: <block
// label>" forever.
//
// The shape is reachable through the documented MCP door: upsert_form merges
// custom_html into an EXISTING config and deliberately preserves blocks
// (mcpsrv/parity.go), so a merchant/agent that adds a custom body to a form
// that already has blocks gets a form nobody can submit.
//
// The contract for such a form is custom_html.fields — the decision written
// into the submit handler: the custom arm validates the REGISTERED CUSTOM
// FIELDS instead of the blocks (a form's contract is the set of controls its
// page actually paints), never both and never neither.
//
// This test pins that shape. The behaviour it asserts already holds on main
// (#1357 / a7cddd2c), but nothing covered the MIXED config: the existing custom
// fixtures carry no blocks at all, so the block-derived contract is empty
// there and a regression that applies BOTH contracts would keep them green.
//
//   - leftover blocks + custom_html: the custom page's own payload is accepted
//     (201), the row is stored, the value round-trips, and no block key is
//     invented — i.e. the block contract is NOT applied;
//   - the same payload against the same blocks WITHOUT custom_html is refused
//     naming the first required block, so the acceptance above hinges on the
//     config shape and not on some other rejection (the neutralising arm, in
//     required_blocks_submit_test.go's style);
//   - omitting the required custom field is still refused by the custom
//     contract — skipping the block contract must not turn the route into a
//     no-op.
//
// Integration test; skipped unless JOMFORM_TEST_DATABASE_URL is set.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// leftoverRequiredBlocks is what a blocks-made form keeps in its config after
// upsert_form merges a custom_html section into it: the block contract is still
// declared, and every required marker is still true. The labels are the ones
// the submit route would name if it wrongly applied the block contract.
func leftoverRequiredBlocks() []any {
	return []any{
		map[string]any{"id": "nm", "type": "name", "value": "Nama", "required": true},
		map[string]any{"id": "em", "type": "email", "value": "Emel", "required": true},
	}
}

// customHTMLLeftover is the custom body the merchant's agent registered: one
// required field `cf`, which the iframe bridge posts under exactly that name.
func customHTMLLeftover() map[string]any {
	return map[string]any{
		"html": `<form><label>Nickname<input name="cf" required></label>` +
			`<button type="submit">Go</button></form>`,
		"fields": []any{
			map[string]any{"field_id": "cf", "type": "text", "label": "Nickname", "required": true},
		},
	}
}

// leftoverBlocksFixture seeds one workspace with TWO published response forms
// that differ in exactly one thing: leftover carries custom_html beside the
// required blocks (the shape upsert_form produces), blocksOnly carries the same
// blocks and no custom_html. Same account, same blocks, same status — so a
// status difference between the two can only come from the config shape.
func leftoverBlocksFixture(t *testing.T) (h *PublicHandlers, host, leftoverSlug, blocksOnlySlug, leftoverFormID, blocksOnlyFormID, accountID string) {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)

	var userID, workspaceSlug string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-ws-' || md5(random()::text), 'it-' || md5(random()::text))
		 RETURNING slug`, userID).Scan(&workspaceSlug); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 SELECT $1, 'it-acc-' || md5(random()::text), 'it-' || md5(random()::text), id
		 FROM workspaces WHERE slug = $2
		 RETURNING id`, userID, workspaceSlug).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	fs := dbstore.NewFormStore(pool)
	h = &PublicHandlers{
		Forms:       fs,
		Sales:       dbstore.NewSaleStore(pool),
		Accounts:    dbstore.NewAccountStore(pool),
		Products:    dbstore.NewProductStore(pool),
		Workspaces:  dbstore.NewWorkspaceStore(pool),
		Submissions: dbstore.NewSubmissionStore(pool),
		BaseURL:     "http://localhost:8080",
	}
	host = workspaceSlug + ".jomform.com"

	leftoverSlug = "it-custom-blocks-" + accountID[:8]
	lf := &dbstore.Form{
		AccountID: accountID, Slug: leftoverSlug, Title: "Custom with leftover blocks",
		Status: dbstore.FormStatusPublished,
		Config: map[string]any{
			"mode":        "response",
			"blocks":      leftoverRequiredBlocks(),
			"custom_html": customHTMLLeftover(),
		},
	}
	if err := fs.Create(ctx, lf); err != nil {
		t.Fatalf("create leftover-blocks form: %v", err)
	}
	leftoverFormID = lf.ID

	blocksOnlySlug = "it-blocks-only-" + accountID[:8]
	bf := &dbstore.Form{
		AccountID: accountID, Slug: blocksOnlySlug, Title: "Blocks only",
		Status: dbstore.FormStatusPublished,
		Config: map[string]any{"mode": "response", "blocks": leftoverRequiredBlocks()},
	}
	if err := fs.Create(ctx, bf); err != nil {
		t.Fatalf("create blocks-only form: %v", err)
	}
	blocksOnlyFormID = bf.ID
	return h, host, leftoverSlug, blocksOnlySlug, leftoverFormID, blocksOnlyFormID, accountID
}

// TestCustomFormLeftoverRequiredBlocksAcceptsItsOwnPayload is the card's
// acceptance: the custom page's own payload is accepted on a form that still
// declares required builder blocks, and the row it stores carries the custom
// field — not a block key.
func TestCustomFormLeftoverRequiredBlocksAcceptsItsOwnPayload(t *testing.T) {
	h, host, leftoverSlug, blocksOnlySlug, leftoverFormID, blocksOnlyFormID, accountID := leftoverBlocksFixture(t)
	ctx := context.Background()

	// The exact payload the custom page posts: the registered custom field and
	// nothing else. No block id (nm/em) is present — applying the block contract
	// to this form refuses it with 400 "medan wajib tidak diisi: Nama".
	const payload = `{"client":{},"fields":{"cf":"hello"}}`

	rec := postSlug(t, h, host, leftoverSlug, payload)
	if rec.Code != http.StatusCreated {
		t.Fatalf("custom_html + required blocks: status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Status string `json:"status"`
		ID     string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	if resp.Status != "submitted" || resp.ID == "" {
		t.Fatalf("resp = %+v, want status submitted with an id", resp)
	}
	if n := submissionCount(t, h, accountID, leftoverFormID); n != 1 {
		t.Fatalf("submissions stored = %d, want 1", n)
	}

	sub, err := h.Submissions.ByID(ctx, resp.ID)
	if err != nil {
		t.Fatalf("fetch submission: %v", err)
	}
	if sub.Data["cf"] != "hello" {
		t.Errorf("stored data[cf] = %v, want the submitted value", sub.Data["cf"])
	}
	// The block contract must not have been applied: neither by refusing the
	// submit (asserted above) nor by inventing block answers in the row.
	for _, blockID := range []string{"nm", "em"} {
		if _, ok := sub.Data[blockID]; ok {
			t.Errorf("stored data carries block key %q, want only the registered custom fields: %v", blockID, sub.Data)
		}
	}

	// Neutralising arm: the SAME payload against the SAME required blocks
	// without a custom_html section is refused, and refused for the block. The
	// 201 above therefore hinges on the config shape — the page being rendered
	// from custom_html — and not on the fixture getting past validation for
	// some unrelated reason.
	rec = postSlug(t, h, host, blocksOnlySlug, payload)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("blocks-only form, same payload: status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "medan wajib tidak diisi: Nama") {
		t.Errorf("blocks-only form, same payload: body %s, want the first required block's message", body)
	}
	if n := submissionCount(t, h, accountID, blocksOnlyFormID); n != 0 {
		t.Fatalf("blocks-only form stored %d rows for a refused POST, want 0", n)
	}
}

// TestCustomFormLeftoverBlocksStillEnforcesItsOwnContract pins the other half
// of the decision: skipping the block contract on a custom form must not turn
// the submit route into a no-op. The registered custom field's required marker
// is what is enforced instead.
func TestCustomFormLeftoverBlocksStillEnforcesItsOwnContract(t *testing.T) {
	h, host, leftoverSlug, _, leftoverFormID, _, accountID := leftoverBlocksFixture(t)

	// The custom page cannot post without `cf` either — but a direct POST can,
	// and that is the one the server must refuse.
	rec := postSlug(t, h, host, leftoverSlug, `{"client":{},"fields":{}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("custom required field omitted: status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "medan wajib tidak diisi: Nickname") {
		t.Errorf("custom required field omitted: body %s, want it to name the custom field", body)
	}
	if n := submissionCount(t, h, accountID, leftoverFormID); n != 0 {
		t.Fatalf("a refused POST stored %d rows, want 0", n)
	}
}
