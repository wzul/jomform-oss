package mcpsrv

// The MCP door into the "published, but unreachable forever" defect (card
// t_fadfe9f7).
//
// THE DEFECT. An account with no workspace (issue #718, ask-first onboarding)
// owns forms whose `workspace_id` is NULL — the forms_sync_workspace_id trigger
// copies accounts.workspace_id onto the row. Since t_6c0badbe the CREATE doors
// refuse to MINT such a form, but the rows that predate that guard are still
// there, and `publish_form` accepted one and answered `status:published`. Every
// public serving path resolves a form through its workspace (subdomain, custom
// domain, or BySlugAnyWorkspace on a self-hosted install, which joins
// accounts→workspaces), and the apex /f/<slug> is refused by design (issue
// #77) — so the caller was told the page was live while it 404'd at every
// address, forever.
//
// WHAT THIS PINS
//   1. `publish_form` on such a row is REFUSED with the sentence the REST 409
//      uses (the key of httpapi's `workspace.required` code table entry — the
//      REST half of this guard is
//      httpapi/form_publish_no_workspace_t_fadfe9f7_test.go), and the stored
//      status is unchanged. A refusal AFTER the write is still the same lie.
//   2. It is refused for EVERY live status (published, private, unlisted), not
//      just the default: they all need an address.
//   3. `bulk_forms` with action=publish is the same door and is refused too,
//      before any row is written.
//   4. The guard is not a blanket refusal: moving the orphan back to draft is
//      still allowed (that is the remedy), and once the account is linked to a
//      workspace the SAME call publishes and the row carries that workspace.
//
// BITE: delete the IsLiveStatus/requireLiveWorkspace block in publishFormMCP
// and case 1 answers {"status":"published"} over a row whose workspace_id is
// NULL; delete the block in bulkFormsMCP and case 3 answers updated:1 over the
// same row. Both were measured on the un-fixed tree.
//
// Runs against a real Postgres (JOMFORM_TEST_DATABASE_URL); skipped when unset.

import (
	"context"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

func TestPublishFormRefusesAFormWithNoWorkspace(t *testing.T) {
	requireTestDB(t)
	ctx := context.Background()
	pool := testdb.Open(t)

	// The state a legacy signup is in: a user, an account, NO workspace.
	accountID, userID := scopeTestAccount(t, pool, "pubnows")
	keys := dbstore.NewAPIKeyStore(pool)
	rawKey := scopeTestKey(t, ctx, keys, accountID, "pubnows-rw", []string{dbstore.ScopeReadWrite})
	httpSrv := wsMCPFixture(t, pool)

	fs := dbstore.NewFormStore(pool)
	flat := strings.ReplaceAll(accountID, "-", "")
	orphan := &dbstore.Form{
		AccountID: accountID,
		Slug:      "mcp-pubnows-" + flat[:10],
		Title:     "Legacy orphan",
		Config:    map[string]any{"mode": "response", "blocks": []any{}},
		Theme:     map[string]any{},
		Status:    dbstore.FormStatusDraft,
	}
	if err := fs.Create(ctx, orphan); err != nil {
		t.Fatalf("seed orphan form: %v", err)
	}

	// Fixture sanity: the seeded row really is the state the card measured.
	var wsOfForm *string
	if err := pool.QueryRow(ctx, `SELECT workspace_id::text FROM forms WHERE id=$1`, orphan.ID).Scan(&wsOfForm); err != nil {
		t.Fatalf("read orphan workspace_id: %v", err)
	}
	if wsOfForm != nil {
		t.Fatalf("fixture is wrong: the seeded form already carries workspace_id %v", *wsOfForm)
	}

	statusOf := func() string {
		t.Helper()
		var st string
		if err := pool.QueryRow(ctx, `SELECT status FROM forms WHERE id=$1`, orphan.ID).Scan(&st); err != nil {
			t.Fatalf("read stored status: %v", err)
		}
		return st
	}

	// ── 1. publish_form (the default status) must refuse ────────────────────
	res := mcpCall(t, httpSrv.URL, rawKey, "publish_form", map[string]any{
		"form_id": orphan.ID,
	})
	isErr, msg := refused(res)
	if !isErr {
		t.Fatalf("publish_form on a form with workspace_id NULL answered %s with NO isError.\n"+
			"That IS the carded defect: the row is stored published and every public serving path "+
			"resolves a form through its workspace, so the page has no address at all.",
			toolResultText(t, res))
	}
	if !strings.Contains(msg, WorkspaceRequiredMessage) {
		t.Fatalf("publish_form refusal = %q, want it to carry the REST door's sentence %q — "+
			"the two write paths must not disagree about this state", msg, WorkspaceRequiredMessage)
	}
	if got := statusOf(); got != dbstore.FormStatusDraft {
		t.Fatalf("status after the refusal = %q, want %q — the refusal ran AFTER the status write", got, dbstore.FormStatusDraft)
	}

	// ── 2. every live status needs an address, not just `published` ─────────
	for _, live := range []struct {
		status string
		extra  map[string]any
	}{
		{dbstore.FormStatusPrivate, map[string]any{"password": "rahasia123"}},
		{dbstore.FormStatusUnlisted, nil},
	} {
		in := map[string]any{"form_id": orphan.ID, "status": live.status}
		for k, v := range live.extra {
			in[k] = v
		}
		res := mcpCall(t, httpSrv.URL, rawKey, "publish_form", in)
		isErr, msg := refused(res)
		if !isErr {
			t.Fatalf("publish_form status=%s on a workspace-less form answered %s with NO isError — "+
				"a private/unlisted form is served at its workspace address too, so it is the same "+
				"unreachable page", live.status, toolResultText(t, res))
		}
		if !strings.Contains(msg, WorkspaceRequiredMessage) {
			t.Fatalf("status=%s refusal = %q, want %q", live.status, msg, WorkspaceRequiredMessage)
		}
		if got := statusOf(); got != dbstore.FormStatusDraft {
			t.Fatalf("status after the status=%s refusal = %q, want draft", live.status, got)
		}
	}

	// ── 3. bulk_forms action=publish is the same door ──────────────────────
	res = mcpCall(t, httpSrv.URL, rawKey, "bulk_forms", map[string]any{
		"action": "publish",
		"ids":    []string{orphan.ID},
	})
	isErr, msg = refused(res)
	if !isErr {
		t.Fatalf("bulk_forms publish over a workspace-less form answered %s with NO isError — "+
			"the bulk door would leave the same unreachable published row", toolResultText(t, res))
	}
	if !strings.Contains(msg, WorkspaceRequiredMessage) {
		t.Fatalf("bulk_forms publish refusal = %q, want %q", msg, WorkspaceRequiredMessage)
	}
	if got := statusOf(); got != dbstore.FormStatusDraft {
		t.Fatalf("status after the refused bulk publish = %q, want draft (the gate must run before the write)", got)
	}

	// ── 4. Not a blanket refusal: draft is still allowed on the orphan ──────
	// Taking a live orphan back to draft is the remedy, not a second orphan.
	out := succeeded(t, mcpCall(t, httpSrv.URL, rawKey, "publish_form", map[string]any{
		"form_id": orphan.ID,
		"status":  dbstore.FormStatusDraft,
	}))
	if out["status"] != dbstore.FormStatusDraft {
		t.Fatalf("publish_form status=draft on an orphan returned %v, want status=draft — the guard "+
			"must gate entering a LIVE status, not every write to the account", out)
	}

	// ── 5. The exit: link the account, then the same publish lands ─────────
	// WorkspaceStore.CreateForAccount is the dashboard's ask-first hop: it makes
	// the workspace, links the account and backfills every NULL row it owns.
	ws, werr := dbstore.NewWorkspaceStore(pool).CreateForAccount(ctx, userID,
		"WS pubnows", "pubnows-"+flat[:8], accountID)
	if werr != nil {
		t.Fatalf("link account to a workspace: %v", werr)
	}
	published := succeeded(t, mcpCall(t, httpSrv.URL, rawKey, "publish_form", map[string]any{
		"form_id": orphan.ID,
	}))
	if published["status"] != dbstore.FormStatusPublished {
		t.Fatalf("publish_form after the account got a workspace returned %v, want status=published", published)
	}
	var wsAfter *string
	if err := pool.QueryRow(ctx, `SELECT workspace_id::text FROM forms WHERE id=$1`, orphan.ID).Scan(&wsAfter); err != nil {
		t.Fatalf("read workspace_id after the publish: %v", err)
	}
	if wsAfter == nil || *wsAfter != ws.ID {
		t.Fatalf("published form workspace_id = %v, want %q — the published page would still have no address",
			wsAfter, ws.ID)
	}
	if got := statusOf(); got != dbstore.FormStatusPublished {
		t.Fatalf("stored status after the linked publish = %q, want published", got)
	}
}
