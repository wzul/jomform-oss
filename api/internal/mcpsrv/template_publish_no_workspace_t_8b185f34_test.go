package mcpsrv

// Card t_8b185f34, MCP half: the workspace gate refused a class it was never
// about.
//
// Card t_fadfe9f7 closed "publishing a form with no workspace" on both doors.
// The gate is the ACCOUNT's state (HasLiveWorkspace) and never looked at the
// row, so it also locked out `is_template` rows — whose public address is the
// GALLERY (REST GET /v1/templates serves `is_template AND status='published'`
// with no account or workspace filter), not a workspace. The account that owns
// the gallery, templates@system.jomform, must NEVER have a workspace
// (dbstore.LegacyProvisionedEmails, migration 000059), so that door was closed
// by accident on the one account it can never open. REST's half is
// httpapi/template_publish_no_workspace_t_8b185f34_test.go; the two doors must
// agree about the class.
//
// WHAT THIS PINS
//  1. `publish_form` publishes a workspace-less `is_template` row (200-equivalent
//     result, stored status published) and the row is served by ListTemplates,
//     which is the query behind the public gallery.
//  2. An ORDINARY form on the same workspace-less account is still refused with
//     WorkspaceRequiredMessage, and its stored status stays draft.
//  3. `bulk_forms` action=publish is exempt only for an all-template batch; an
//     ordinary id is still refused before any row is written.
//  4. The boundary is `published`: `private` on a template is still refused,
//     because a private template is in no public list and is served only
//     through a workspace.
//
// BITE: drop the hasGalleryAddress() term in publishFormMCP (and the
// AllGalleryTemplates() exemption in bulkFormsMCP) and case 1 is refused with
// "this account has no workspace yet" over a row the gallery would have served.
//
// Runs against a real Postgres (JOMFORM_TEST_DATABASE_URL); skipped when unset.

import (
	"context"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// mcpGalleryReadyConfig is a config the gallery predicate (issue #594) accepts:
// a category and one non-empty description block.
func mcpGalleryReadyConfig() map[string]any {
	return map[string]any{
		"mode":     "response",
		"category": "Test",
		"blocks": []any{
			map[string]any{"id": "b1", "type": "text", "value": "a template's intro line"},
		},
	}
}

func TestGalleryTemplatesPublishWithoutAWorkspaceMCP(t *testing.T) {
	requireTestDB(t)
	ctx := context.Background()
	pool := testdb.Open(t)

	// The state the gallery account is REQUIRED to be in: an account with no
	// workspace (dbstore.LegacyProvisionedEmails).
	accountID, _ := scopeTestAccount(t, pool, "tplnows")
	keys := dbstore.NewAPIKeyStore(pool)
	rawKey := scopeTestKey(t, ctx, keys, accountID, "tplnows-rw", []string{dbstore.ScopeReadWrite})
	httpSrv := wsMCPFixture(t, pool)
	fs := dbstore.NewFormStore(pool)
	flat := strings.ReplaceAll(accountID, "-", "")

	var wsOfAccount *string
	if err := pool.QueryRow(ctx, `SELECT workspace_id::text FROM accounts WHERE id=$1`, accountID).Scan(&wsOfAccount); err != nil {
		t.Fatalf("read account workspace: %v", err)
	}
	if wsOfAccount != nil {
		t.Fatalf("fixture is wrong: the account already carries workspace_id %v", *wsOfAccount)
	}

	statusOf := func(id string) string {
		t.Helper()
		var st string
		if err := pool.QueryRow(ctx, `SELECT status FROM forms WHERE id=$1`, id).Scan(&st); err != nil {
			t.Fatalf("read stored status: %v", err)
		}
		return st
	}
	inGallery := func(id string) bool {
		t.Helper()
		rows, err := fs.ListTemplates(ctx) // the public gallery query
		if err != nil {
			t.Fatalf("list templates: %v", err)
		}
		for _, f := range rows {
			if f.ID == id {
				return true
			}
		}
		return false
	}

	tpl := &dbstore.Form{
		AccountID: accountID, Slug: "mcp-tplnows-" + flat[:8], Title: "Gallery template",
		Config: mcpGalleryReadyConfig(), Theme: map[string]any{},
	}
	if err := fs.CreateTemplate(ctx, tpl); err != nil {
		t.Fatalf("create template: %v", err)
	}
	tplBulk := &dbstore.Form{
		AccountID: accountID, Slug: "mcp-tplnows-bulk-" + flat[:8], Title: "Gallery template (bulk)",
		Config: mcpGalleryReadyConfig(), Theme: map[string]any{},
	}
	if err := fs.CreateTemplate(ctx, tplBulk); err != nil {
		t.Fatalf("create bulk template: %v", err)
	}
	ordinary := &dbstore.Form{
		AccountID: accountID, Slug: "mcp-nows-" + flat[:8], Title: "Ordinary form",
		Config: map[string]any{"mode": "response", "blocks": []any{}},
		Theme:  map[string]any{}, Status: dbstore.FormStatusDraft,
	}
	if err := fs.Create(ctx, ordinary); err != nil {
		t.Fatalf("seed ordinary form: %v", err)
	}

	// ── 1. publish_form publishes the gallery template ─────────────────────
	out := succeeded(t, mcpCall(t, httpSrv.URL, rawKey, "publish_form", map[string]any{
		"form_id": tpl.ID,
	}))
	if out["status"] != dbstore.FormStatusPublished {
		t.Fatalf("publish_form on a workspace-less template returned %v, want status=published — "+
			"the gallery is this row's address, so the workspace gate must not fire", out)
	}
	if got := statusOf(tpl.ID); got != dbstore.FormStatusPublished {
		t.Fatalf("stored status = %q, want published", got)
	}
	if !inGallery(tpl.ID) {
		t.Fatalf("the published template is not returned by ListTemplates — publish_form answered " +
			"published for a row the public gallery does not serve")
	}

	// ── 2. The same account still refuses an ordinary form ─────────────────
	res := mcpCall(t, httpSrv.URL, rawKey, "publish_form", map[string]any{"form_id": ordinary.ID})
	isErr, msg := refused(res)
	if !isErr {
		t.Fatalf("publish_form on an ordinary workspace-less form answered %s with NO isError — "+
			"that row would be live and 404 at every address", toolResultText(t, res))
	}
	if !strings.Contains(msg, WorkspaceRequiredMessage) {
		t.Fatalf("ordinary-form refusal = %q, want it to carry %q", msg, WorkspaceRequiredMessage)
	}
	if got := statusOf(ordinary.ID); got != dbstore.FormStatusDraft {
		t.Fatalf("ordinary form status = %q, want draft — the refusal ran after the write", got)
	}

	// ── 3. bulk_forms: all-template batch exempt, ordinary batch refused ───
	out = succeeded(t, mcpCall(t, httpSrv.URL, rawKey, "bulk_forms", map[string]any{
		"action": "publish",
		"ids":    []string{tplBulk.ID},
	}))
	if out["updated"] != float64(1) {
		t.Fatalf("bulk_forms publish over one gallery template returned %v, want updated=1 — "+
			"the single tool is exempt, so the bulk door must agree", out)
	}
	if !inGallery(tplBulk.ID) {
		t.Fatalf("the bulk-published template is not returned by ListTemplates")
	}
	res = mcpCall(t, httpSrv.URL, rawKey, "bulk_forms", map[string]any{
		"action": "publish",
		"ids":    []string{ordinary.ID},
	})
	isErr, msg = refused(res)
	if !isErr {
		t.Fatalf("bulk_forms publish over an ordinary workspace-less form answered %s with NO isError", toolResultText(t, res))
	}
	if !strings.Contains(msg, WorkspaceRequiredMessage) {
		t.Fatalf("bulk refusal = %q, want %q", msg, WorkspaceRequiredMessage)
	}
	if got := statusOf(ordinary.ID); got != dbstore.FormStatusDraft {
		t.Fatalf("ordinary form status after the refused bulk publish = %q, want draft", got)
	}

	// ── 4. The boundary is `published` ─────────────────────────────────────
	res = mcpCall(t, httpSrv.URL, rawKey, "publish_form", map[string]any{
		"form_id":  tpl.ID,
		"status":   dbstore.FormStatusPrivate,
		"password": "rahasia123",
	})
	isErr, msg = refused(res)
	if !isErr {
		t.Fatalf("publish_form status=private on a template answered %s with NO isError — a private "+
			"template is in no public list and has no address without a workspace", toolResultText(t, res))
	}
	if !strings.Contains(msg, WorkspaceRequiredMessage) {
		t.Fatalf("private-template refusal = %q, want %q", msg, WorkspaceRequiredMessage)
	}
	if got := statusOf(tpl.ID); got != dbstore.FormStatusPublished {
		t.Fatalf("template status after the private refusal = %q, want the unchanged published", got)
	}
}
