package httpapi

// Card t_d07c8fbb — the two REST template-CLONE doors still minted orphan forms.
//
// THE DEFECT. Card t_6c0badbe closed POST /v1/rest/forms for an account with no
// workspace, and t_7546eda5 closed its two MCP twins (upsert_form's create half,
// use_template). The two REST clone doors were left open, and they call the same
// h.Forms.Create:
//
//	POST /v1/templates/use          (useTemplate; accepts a gallery `slug` or a
//	                                 caller-owned `template_id`)
//	POST /v1/rest/templates/{id}/use (useOwnTemplate)
//
// `accounts.workspace_id` is nullable — a signup creates an account and no
// workspace (#718, ask-first onboarding) — and the forms_sync_workspace_id
// trigger (migration 000045) copies that NULL onto the clone. The clone could be
// PUBLISHED and was then 404 at every address, because every public serving path
// resolves a form through its workspace. Both handlers gated only on
// requireWriteRole, which a workspace-less account passes.
//
// WHAT THIS FILE PINS
//  1. Both doors, all three body shapes (gallery slug, own template_id through
//     the older door, own id through the /v1/rest door), answer 409 with the
//     machine code `workspace.required` and the same prose POST /v1/rest/forms
//     answers — driven through the real mux with a real API key.
//  2. They refuse BEFORE storing: no live form row exists for the account
//     afterwards. A 409 with a row behind it is still the orphan this card is
//     about.
//  3. They refuse BEFORE resolving the template: an id that names nothing answers
//     the workspace refusal, not 404. The account's state cannot carry the call,
//     so the answer must not depend on WHICH template was named (the same
//     ordering decision the MCP twin made in t_7546eda5). This is also what keeps
//     the two doors consistent with each other on a miss.
//  4. The other direction: link the SAME account to a workspace and all three
//     clones succeed 201, and every stored clone carries that `workspace_id` —
//     i.e. the guard refuses the account's STATE, not the door. Without this a
//     blanket refusal of every clone would pass 1.
//  5. The minting gate still applies the WRITE axis: a readonly key is refused
//     403 (for the scope reason) on both doors. The gate composes the write role
//     with the workspace check, and a composition that dropped the role half
//     would pass 1 unchanged, because a workspace-less account is refused either
//     way.
//
// BITE: delete either h.requireFormMint call (useTemplate / useOwnTemplate)
// and that door's cases answer 201 with a stored row whose workspace_id is NULL,
// while the other door's cases keep passing — a failure names the route, not the
// guard. Ablating the workspace half of the shared gate flips all of them,
// together with TestCreateFormWithoutWorkspaceIsRefused; deleting the role half
// reddens only arm 5.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// seedPublishedGalleryTemplate stores one published is_template row owned by a
// SEPARATE, workspace-linked account — the real gallery shape: TemplateBySlug is
// not account-scoped, so any caller can clone a published template. It is given
// a category and an intro text block because the gallery-metadata guard
// (template_gallery_metadata_issue594_test.go) inspects every published template
// in the database, and it is deleted on cleanup for the same reason
// (cleanupProbeForms documents the failure that leaves behind).
func seedPublishedGalleryTemplate(t *testing.T, ctx context.Context, pool *pgxpool.Pool) *dbstore.Form {
	t.Helper()
	owner, _ := bundleFixture(t, ctx, pool)
	_ = bundleLinkWorkspace(t, ctx, pool, owner)
	tpl := &dbstore.Form{
		AccountID: owner,
		Slug:      "it-clone-gal-" + randHex(8),
		Title:     "Clone door probe template",
		Config: map[string]any{
			"mode":     "response",
			"category": "Booking",
			"blocks": []any{
				map[string]any{"id": "b0", "type": "text", "value": "An intro line for the probe card."},
				map[string]any{"id": "b1", "type": "text", "value": "Reach us at the form below."},
			},
		},
		Theme: map[string]any{"primary": "#123456"},
	}
	forms := dbstore.NewFormStore(pool)
	if err := forms.CreateTemplate(ctx, tpl); err != nil {
		t.Fatalf("seed published gallery template: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE forms SET status='published' WHERE id=$1`, tpl.ID); err != nil {
		t.Fatalf("publish gallery template %s: %v", tpl.ID, err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM forms WHERE id=$1`, tpl.ID); err != nil {
			t.Errorf("clean up gallery probe template %s: %v", tpl.ID, err)
		}
	})
	return tpl
}

// clonePost drives a clone door through the registered mux and decodes the JSON
// envelope (error or success).
func clonePost(t *testing.T, h *RESTHandlers, rawKey, path, payload string) (int, map[string]any, json.RawMessage) {
	t.Helper()
	code, raw := tplReq(t, h, rawKey, http.MethodPost, path, payload)
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode POST %s response: %v (%s)", path, err, raw)
	}
	return code, out, raw
}

// bundleReadonlyAPIKey mints a READONLY key for the account — the shape
// bundleAPIKey cannot produce (it mints admin). Used to prove the composed
// minting gate still applies the write axis: a readonly key must be refused by
// the SCOPE, not merely told to create a workspace.
func bundleReadonlyAPIKey(t *testing.T, ctx context.Context, pool *pgxpool.Pool, accountID string) string {
	t.Helper()
	raw, hash, err := auth.GenerateAPIKey()
	if err != nil {
		t.Fatalf("generate readonly api key: %v", err)
	}
	if _, err := dbstore.NewAPIKeyStore(pool).Create(ctx, accountID, hash, "it-bundle-ro", []string{dbstore.ScopeReadonly}); err != nil {
		t.Fatalf("store readonly api key: %v", err)
	}
	return raw
}

func TestRESTTemplateCloneWithoutWorkspaceIsRefused(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)

	// The state a fresh signup is in: a user, an account, NO workspace.
	accountID, _ := bundleFixture(t, ctx, pool)
	h := newBundleHandlers(pool)
	key := bundleAPIKey(t, ctx, pool, accountID)
	forms := dbstore.NewFormStore(pool)

	gallery := seedPublishedGalleryTemplate(t, ctx, pool)

	// The caller's own template: the shape both doors can clone (owner-scoped
	// id, plus the older door's template_id branch). Stored directly through the
	// store with is_template forced true, which is exactly what the template
	// routes write.
	own := &dbstore.Form{
		AccountID: accountID,
		Slug:      "it-own-tpl-" + randHex(8),
		Title:     "Own probe template",
		Config: map[string]any{"mode": "response", "blocks": []any{
			map[string]any{"id": "b1", "type": "text", "value": "hello"},
		}},
		Theme: map[string]any{"primary": "#654321"},
	}
	if err := forms.CreateTemplate(ctx, own); err != nil {
		t.Fatalf("seed own template: %v", err)
	}
	t.Cleanup(func() {
		// Removes the own template, any clone, and every other row this test
		// stored for the probe account.
		if _, err := pool.Exec(context.Background(), `DELETE FROM forms WHERE account_id=$1`, accountID); err != nil {
			t.Errorf("clean up probe forms for %s: %v", accountID, err)
		}
	})

	missingID := uuid.NewString()

	// ── 1/2/3: both doors refuse, store nothing, and do it before lookup ─────
	refused := []struct{ name, path, body string }{
		{"POST /v1/templates/use {slug} (gallery template)",
			"/v1/templates/use", `{"slug":"` + gallery.Slug + `"}`},
		{"POST /v1/templates/use {template_id} (own template)",
			"/v1/templates/use", `{"template_id":"` + own.ID + `"}`},
		{"POST /v1/rest/templates/{id}/use (own template)",
			"/v1/rest/templates/" + own.ID + "/use", `{}`},
		// The guard runs BEFORE the template is resolved, so an id that names
		// nothing is still the workspace refusal — not a 404 that would tell the
		// caller to fix the wrong thing.
		{"POST /v1/templates/use {template_id} (unknown id)",
			"/v1/templates/use", `{"template_id":"` + missingID + `"}`},
		{"POST /v1/rest/templates/{id}/use (unknown id)",
			"/v1/rest/templates/" + missingID + "/use", `{}`},
	}
	for _, tc := range refused {
		code, body, raw := clonePost(t, h, key, tc.path, tc.body)
		if code != http.StatusConflict {
			t.Errorf("%s for an account with no workspace = %d (body %s), want 409 — the clone is a real "+
				"form that can be published and is then 404 at every address", tc.name, code, raw)
			continue
		}
		if got := body["code"]; got != CodeWorkspaceRequired {
			t.Errorf("%s code = %v, want %q — the dashboard cannot tell this refusal from a generic "+
				"conflict without it (body %s)", tc.name, got, CodeWorkspaceRequired, raw)
		}
		if got, _ := body["error"].(string); got != workspaceRequiredMessage {
			t.Errorf("%s error = %v, want the wire prose %q — the four form-minting doors must answer "+
				"one sentence", tc.name, body["error"], workspaceRequiredMessage)
		}
	}

	// ── the write axis is still applied BY the minting gate ──────────────────
	// requireFormMint composes the write role with the workspace check, so a
	// readonly key is still refused with the scope's 403 — and on an account with
	// NO workspace it is refused with 403 rather than 409, which is what pins the
	// ORDER: the write gate runs first, and the account's workspace state is only
	// consulted for a caller who may write at all. Without this arm the
	// composition could drop the role half and every case above would still pass,
	// because a workspace-less account is refused either way.
	readonlyKey := bundleReadonlyAPIKey(t, ctx, pool, accountID)
	for _, tc := range []struct{ name, path, body string }{
		{"POST /v1/rest/templates/{id}/use", "/v1/rest/templates/" + own.ID + "/use", `{}`},
		{"POST /v1/templates/use {template_id}", "/v1/templates/use", `{"template_id":"` + own.ID + `"}`},
	} {
		code, body, raw := clonePost(t, h, readonlyKey, tc.path, tc.body)
		if code != http.StatusForbidden {
			t.Errorf("%s with a readonly key = %d (body %s), want 403 — the minting gate must keep "+
				"the write scope, not just the workspace check", tc.name, code, raw)
			continue
		}
		if msg, _ := body["error"].(string); !strings.Contains(msg, "scope") {
			t.Errorf("%s refused a readonly key with %d but not for the scope reason: %s",
				tc.name, code, raw)
		}
	}

	// Nothing may be stored behind a refusal. Only live forms count: the own
	// template row is a form row too, and it is not a clone.
	var stored int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM forms WHERE account_id=$1 AND NOT is_template AND deleted_at IS NULL`,
		accountID).Scan(&stored); err != nil {
		t.Fatalf("count live forms: %v", err)
	}
	if stored != 0 {
		t.Fatalf("the refused clones still stored %d live form(s) — a guard that runs after the INSERT "+
			"leaves exactly the orphan this card is about", stored)
	}

	// ── 4: the other direction — the same account, once linked ───────────────
	wsID := bundleLinkWorkspace(t, ctx, pool, accountID)

	accepted := []struct{ name, path, body string }{
		{"POST /v1/templates/use {slug} (gallery template)",
			"/v1/templates/use", `{"slug":"` + gallery.Slug + `"}`},
		{"POST /v1/templates/use {template_id} (own template)",
			"/v1/templates/use", `{"template_id":"` + own.ID + `"}`},
		{"POST /v1/rest/templates/{id}/use (own template)",
			"/v1/rest/templates/" + own.ID + "/use", `{}`},
	}
	for _, tc := range accepted {
		code, body, raw := clonePost(t, h, key, tc.path, tc.body)
		if code != http.StatusCreated {
			t.Errorf("%s for the SAME account once linked = %d (body %s), want 201 — the guard must "+
				"refuse the account's state, not every clone", tc.name, code, raw)
			continue
		}
		cloneID, _ := body["id"].(string)
		if cloneID == "" {
			t.Errorf("%s answered 201 with no id (body %s)", tc.name, raw)
			continue
		}
		var wsOnClone *string
		if err := pool.QueryRow(ctx,
			`SELECT workspace_id::text FROM forms WHERE id=$1`, cloneID).Scan(&wsOnClone); err != nil {
			t.Fatalf("%s: read stored clone %s: %v", tc.name, cloneID, err)
		}
		if wsOnClone == nil || *wsOnClone != wsID {
			t.Errorf("%s: stored clone's workspace_id = %v, want %q — a clone must land in the "+
				"workspace that now owns the account", tc.name, wsOnClone, wsID)
		}
	}

	// And the success path mints no orphan either: every live form the account
	// now holds carries a workspace.
	var orphans int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM forms WHERE account_id=$1 AND NOT is_template AND workspace_id IS NULL`,
		accountID).Scan(&orphans); err != nil {
		t.Fatalf("count orphan forms: %v", err)
	}
	if orphans != 0 {
		t.Fatalf("%d clone(s) were stored with workspace_id NULL — a clone minted after the link must "+
			"carry the workspace, otherwise the guard only moves the orphan", orphans)
	}
}
