package httpapi

// Card t_cbeafc7c — after a builder Publish the stored
// `config.registered_fields` contract must still name the block ids the public
// page emits, so a submit that was accepted before the save is accepted after
// it.
//
// THE DEFECT, measured on prod v1.21.279 / 4b8fa0dc by card t_bf407d24 with a
// throwaway response form in `test-wan`: the builder's Publish kept
// `registered_fields` verbatim (card t_59bc3055's fix) but wrote the blocks back
// with NEW ids, because hydrateBlock assigned `id: uid()` on every load
// (web/src/app/builder/page.tsx). formfields.Contract PREFERS a non-empty
// registered_fields list over the blocks, so the contract kept requiring
// `field_id: "b2"` while the page emitted `name="db8x0qh"`:
//
//	400 {"code":"request.badRequest","error":"medan wajib tidak diisi: Nama"}
//
// Removing registered_fields from the same row and resubmitting the very same
// page payload answered 201 — the stale contract was the whole cause.
//
// These tests pin the invariant at the two halves that live in the API, using
// the configs the two builder versions actually write:
//
//   - the response page emits a control named by every contract field_id for the
//     POST-SAVE config (stable ids), and no control named by it for the pre-fix
//     save's config (renamed ids) — that arm is what makes the first one a test
//     of the agreement rather than of the renderer in general;
//   - the submit route accepts the fields the page emits for the post-save
//     config (201) and refuses the pre-fix one (400 with the card's own
//     message), so "still submittable after a save" is measured on the route
//     rather than inferred.
//
// The builder's own half — that a Publish really writes the stored ids — is
// web/e2e/builder-publish-keeps-block-ids.spec.ts (drives the built page and
// reads the PATCH body).
//
// WHAT THIS HALF DOES NOT DO: the fix lives in the builder, not in the API, so
// these tests are a REGRESSION NET, not a bug-fix test — they pass on the
// pre-fix tree too. What they pin is the server-side rule the builder must
// satisfy (a contract field_id is only submittable while a control carries that
// name), and the second arm reproduces the reported 400 in-test so the rule is
// measured rather than asserted in the abstract. The bite proof for the fix
// itself is the e2e spec's first arm, which fails on a builder that renames.
//
// Integration tests; the submit arms skip unless JOMFORM_TEST_DATABASE_URL is set.

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/formfields"
)

// blockIDContractBlocks is the fixture the QA card used: one optional text field
// and one required one whose id the contract names.
func blockIDContractBlocks() []any {
	return []any{
		map[string]any{"id": "b1", "type": "text", "value": "Salam"},
		map[string]any{"id": "b2", "type": "text", "value": "Nama", "required": true},
	}
}

// blockIDContractRegistered is the contract an agent registers with
// register_form_fields: its field_id IS a block id.
func blockIDContractRegistered() []any {
	return []any{map[string]any{
		"field_id": "b2", "type": "text", "label": "Nama", "required": true,
	}}
}

// blockIDConfigAfterSave is the config a FIXED builder writes on Publish: the
// stored ids, the contract carried verbatim.
func blockIDConfigAfterSave() map[string]any {
	return map[string]any{
		"mode":              "response",
		"description":       "Salam",
		"chip_mapping":      map[string]any{},
		"blocks":            blockIDContractBlocks(),
		"products":          []any{},
		"registered_fields": blockIDContractRegistered(),
	}
}

// blockIDConfigBeforeFix is the config the PRE-FIX builder wrote on the same
// save: identical except that every block was renamed, while registered_fields
// still names "b2".
func blockIDConfigBeforeFix() map[string]any {
	return map[string]any{
		"mode":         "response",
		"description":  "Salam",
		"chip_mapping": map[string]any{},
		"blocks": []any{
			map[string]any{"id": "265kivw", "type": "text", "value": "Salam"},
			map[string]any{"id": "db8x0qh", "type": "text", "value": "Nama", "required": true},
		},
		"products":          []any{},
		"registered_fields": blockIDContractRegistered(),
	}
}

// TestResponsePageEmitsAControlForEveryRegisteredFieldID is the rendered half:
// for the config a fixed save writes, every field_id the submit route will
// require has a control on the page carrying that exact name.
func TestResponsePageEmitsAControlForEveryRegisteredFieldID(t *testing.T) {
	cfg := blockIDConfigAfterSave()
	contract := formfields.Contract(cfg)
	if len(contract) != 1 || contract[0].FieldID != "b2" {
		t.Fatalf("formfields.Contract = %+v, want the registered b2 field (the contract is the subject here)", contract)
	}

	body := renderResponsePage(t, "block-ids-after-save", cfg)
	for _, f := range contract {
		if firstInputNamed(body, f.FieldID) == "" {
			t.Errorf("the contract requires %q but the page emits no control with that name — its own submit would answer 400", f.FieldID)
		}
	}

	// The pre-fix save's config: same contract, renamed blocks. No control
	// carries the contract's name, which is exactly the reported symptom and
	// what makes the assertion above hinge on the id agreement.
	broken := blockIDConfigBeforeFix()
	brokenBody := renderResponsePage(t, "block-ids-before-fix", broken)
	for _, f := range formfields.Contract(broken) {
		if got := firstInputNamed(brokenBody, f.FieldID); got != "" {
			t.Errorf("the renamed-blocks config still emitted a control named %q (%s) — this arm no longer reproduces the card's mismatch", f.FieldID, got)
		}
	}
}

// blockIDContractFixture seeds one user + workspace + account and the two
// response forms this card compares: one carrying the config a fixed save
// writes, one carrying the pre-fix save's output. Both are published, so the
// public submit route is the one under test.
func blockIDContractFixture(t *testing.T) (h *PublicHandlers, host, fixedSlug, brokenSlug, accountID, fixedFormID, brokenFormID string) {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)

	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	var workspaceSlug string
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

	h = &PublicHandlers{
		Forms:       dbstore.NewFormStore(pool),
		Sales:       dbstore.NewSaleStore(pool),
		Accounts:    dbstore.NewAccountStore(pool),
		Products:    dbstore.NewProductStore(pool),
		Workspaces:  dbstore.NewWorkspaceStore(pool),
		Submissions: dbstore.NewSubmissionStore(pool),
		BaseURL:     "http://localhost:8080",
	}
	host = workspaceSlug + ".jomform.com"

	fixedSlug = "it-ids-fixed-" + accountID[:8]
	ff := &dbstore.Form{
		AccountID: accountID, Slug: fixedSlug, Title: "Block ids after save",
		Status: dbstore.FormStatusPublished, Config: blockIDConfigAfterSave(),
	}
	if err := h.Forms.Create(ctx, ff); err != nil {
		t.Fatalf("create fixed form: %v", err)
	}
	fixedFormID = ff.ID

	brokenSlug = "it-ids-broken-" + accountID[:8]
	bf := &dbstore.Form{
		AccountID: accountID, Slug: brokenSlug, Title: "Block ids before fix",
		Status: dbstore.FormStatusPublished, Config: blockIDConfigBeforeFix(),
	}
	if err := h.Forms.Create(ctx, bf); err != nil {
		t.Fatalf("create pre-fix form: %v", err)
	}
	brokenFormID = bf.ID
	return h, host, fixedSlug, brokenSlug, accountID, fixedFormID, brokenFormID
}

// TestResponseSubmitAcceptsTheContractThePageEmits: the payload the public page
// produces for the config a fixed save writes is ACCEPTED (201, one submission);
// the same visitor payload against the pre-fix save's config is refused with the
// card's own 400 and stores nothing. The pair is what makes "still submittable
// after the save" a measurement instead of an inference.
func TestResponseSubmitAcceptsTheContractThePageEmits(t *testing.T) {
	h, host, fixedSlug, brokenSlug, accountID, fixedFormID, brokenFormID := blockIDContractFixture(t)

	// What the page emits for the fixed config: both block ids, the required one
	// answered.
	rec := postSlug(t, h, host, fixedSlug, `{"client":{"full_name":"QA"},"fields":{"b1":"","b2":"QA submit probe"}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("post-save submit status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if n := submissionCount(t, h, accountID, fixedFormID); n != 1 {
		t.Errorf("submissions stored = %d for the accepted submit, want 1", n)
	}

	// What the same page emitted after the PRE-FIX save: the renamed block ids.
	// The stored contract still demands "b2", which no control carries.
	rec = postSlug(t, h, host, brokenSlug, `{"client":{"full_name":"QA"},"fields":{"265kivw":"","db8x0qh":"QA submit probe"}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("pre-fix save submit status = %d, want 400 (body %s) — this arm no longer reproduces the card", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "medan wajib tidak diisi: Nama") {
		t.Errorf("pre-fix save submit body = %s, want the card's %q", rec.Body.String(), "medan wajib tidak diisi: Nama")
	}
	if n := submissionCount(t, h, accountID, brokenFormID); n != 0 {
		t.Errorf("submissions stored = %d for the refused submit, want 0", n)
	}
}
