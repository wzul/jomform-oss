package mcpsrv

// The MCP door into the orphan-form defect (card t_7546eda5).
//
// THE DEFECT, measured on a clean self-host install behind t_6c0badbe. A signup
// creates an account and NO workspace (issue #718, ask-first onboarding), and
// `accounts.workspace_id` is nullable. POST /v1/rest/forms now refuses that
// account with 409 `workspace.required`, but the MCP write path into the SAME
// store did not: `upsert_form` (create half) and `use_template` (the clone)
// both built `&dbstore.Form{AccountID: acc, …}` and called Forms.Create with no
// workspace check. The forms_sync_workspace_id trigger (migration 000045) copied
// the NULL onto the row, the form could then be PUBLISHED, and it was
// unreachable forever — every public serving path resolves a form through its
// workspace, so /f/<slug> is 404 with no error at create or publish time.
//
// WHAT THIS PINS
//   1. `upsert_form` create and `use_template` are REFUSED for an account with
//      no live workspace, with the sentence the REST 409 uses — which is the key
//      of httpapi's `workspace.required` code table entry, so the agent gets the
//      same machine code the REST client gets
//      (httpapi/workspace_required_parity_t_7546eda5_test.go pins the code).
//   2. They refuse BEFORE storing: no form row exists afterwards. A refusal with
//      an INSERT already behind it is still exactly the orphan this card is
//      about.
//   3. The other direction, on the SAME account: once it is linked to a workspace
//      (the ask-first flow, WorkspaceStore.CreateForAccount), the same calls
//      succeed and the stored row carries that workspace_id. Without this the
//      guard could be a blanket refusal and still pass case 1.
//   4. The gate is CREATE-ONLY: updating an existing (pre-fix, orphaned) form is
//      still allowed. Refusing that would strand a merchant's existing forms and
//      fix nothing — the row already exists.
//
// BITE: delete the requireLiveWorkspace call in upsertForm's create branch and
// case 1 answers {"status":"created"} with a stored row whose workspace_id is
// NULL; delete the one in useTemplateMCP and the clone does the same.
//
// Runs against a real Postgres (JOMFORM_TEST_DATABASE_URL); skipped when unset.

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

func TestFormMintingToolsRefuseAnAccountWithNoWorkspace(t *testing.T) {
	requireTestDB(t)
	ctx := context.Background()
	pool := testdb.Open(t)

	// The state a fresh signup is in: a user, an account, NO workspace.
	// scopeTestAccount inserts exactly that (no workspace_id) and scopeTestKey
	// mints a real readwrite key on it.
	accountID, userID := scopeTestAccount(t, pool, "nows7546")
	keys := dbstore.NewAPIKeyStore(pool)
	rawKey := scopeTestKey(t, ctx, keys, accountID, "nows7546-rw", []string{dbstore.ScopeReadWrite})
	httpSrv := wsMCPFixture(t, pool)

	flat := strings.ReplaceAll(accountID, "-", "")
	slug := "mcp-nows-" + flat[:10]
	fs := dbstore.NewFormStore(pool)

	// A template the clone can name. Seeded through the store (which is what the
	// dashboard's save_template does) so the clone door is exercised on its own
	// guard rather than on a missing template.
	tpl := &dbstore.Form{
		AccountID: accountID,
		Slug:      "tpl-nows-" + flat[:10],
		Title:     "No-workspace template",
		Config:    map[string]any{"mode": "response", "blocks": []any{}},
		Theme:     map[string]any{},
	}
	if err := fs.CreateTemplate(ctx, tpl); err != nil {
		t.Fatalf("seed template: %v", err)
	}

	countForms := func(where string, arg any) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM forms WHERE `+where, arg).Scan(&n); err != nil {
			t.Fatalf("count forms (%s): %v", where, err)
		}
		return n
	}

	// ── 1. upsert_form CREATE (form_id omitted) must refuse ─────────────────
	res := mcpCall(t, httpSrv.URL, rawKey, "upsert_form", map[string]any{
		"title": "clean-test-mcp",
		"slug":  slug,
	})
	isErr, msg := refused(res)
	if !isErr {
		t.Fatalf("upsert_form CREATE for an account with no workspace answered %s with NO "+
			"isError.\nThat IS the carded defect: the row is stored with workspace_id NULL, it can be "+
			"published, and /f/<slug> then 404s forever.", toolResultText(t, res))
	}
	if !strings.Contains(msg, WorkspaceRequiredMessage) {
		t.Fatalf("upsert_form CREATE refusal = %q, want it to carry the REST door's sentence %q — "+
			"the two write paths must not disagree about this state", msg, WorkspaceRequiredMessage)
	}
	if n := countForms("account_id=$1 AND NOT is_template", accountID); n != 0 {
		t.Fatalf("the refused upsert_form create still stored %d form(s) — the guard runs after the "+
			"INSERT, so the orphan form exists anyway", n)
	}

	// ── 2. use_template (the clone) must refuse too ─────────────────────────
	res = mcpCall(t, httpSrv.URL, rawKey, "use_template", map[string]any{
		"template_id": tpl.ID,
	})
	isErr, msg = refused(res)
	if !isErr {
		t.Fatalf("use_template for an account with no workspace answered %s with NO isError — "+
			"the clone mints a real, publishable form, so it is the same orphan through a second "+
			"MCP door", toolResultText(t, res))
	}
	if !strings.Contains(msg, WorkspaceRequiredMessage) {
		t.Fatalf("use_template refusal = %q, want it to carry the same sentence %q", msg, WorkspaceRequiredMessage)
	}
	if n := countForms("account_id=$1 AND NOT is_template", accountID); n != 0 {
		t.Fatalf("the refused clone stored %d form(s)", n)
	}

	// ── 3. The other direction: the SAME account, once linked, creates ──────
	// WorkspaceStore.CreateForAccount is the dashboard's ask-first hop: it makes
	// the workspace and links this existing account (and syncs its rows).
	ws, werr := dbstore.NewWorkspaceStore(pool).CreateForAccount(ctx, userID,
		"WS nows7546", "nows7546-"+flat[:8], accountID)
	if werr != nil {
		t.Fatalf("link account to a workspace: %v", werr)
	}

	created := succeeded(t, mcpCall(t, httpSrv.URL, rawKey, "upsert_form", map[string]any{
		"title": "clean-test-mcp",
		"slug":  slug,
	}))
	if created["status"] != "created" {
		t.Fatalf("upsert_form CREATE for a LINKED account returned %v, want status=created — the "+
			"guard must refuse the account's STATE, not the feature", created)
	}
	formID, _ := created["form_id"].(string)
	if formID == "" {
		t.Fatalf("upsert_form CREATE returned no form_id: %v", created)
	}
	// The row itself is the evidence: a created form must carry a workspace, or
	// the guard only moved the orphan one call later.
	var storedWS *string
	if err := pool.QueryRow(ctx,
		`SELECT workspace_id::text FROM forms WHERE id=$1`, formID).Scan(&storedWS); err != nil {
		t.Fatalf("read stored form workspace_id: %v", err)
	}
	if storedWS == nil || *storedWS != ws.ID {
		t.Fatalf("stored form workspace_id = %v, want %q — the created form is still unscoped",
			storedWS, ws.ID)
	}

	cloned := succeeded(t, mcpCall(t, httpSrv.URL, rawKey, "use_template", map[string]any{
		"template_id": tpl.ID,
	}))
	cloneID, _ := cloned["form_id"].(string)
	if cloned["status"] != "created" || cloneID == "" {
		t.Fatalf("use_template for a LINKED account returned %v, want status=created + form_id", cloned)
	}
	if err := pool.QueryRow(ctx,
		`SELECT workspace_id::text FROM forms WHERE id=$1`, cloneID).Scan(&storedWS); err != nil {
		t.Fatalf("read cloned form workspace_id: %v", err)
	}
	if storedWS == nil || *storedWS != ws.ID {
		t.Fatalf("cloned form workspace_id = %v, want %q", storedWS, ws.ID)
	}

	// ── 4. The gate is CREATE-ONLY ─────────────────────────────────────────
	// A pre-fix orphan (a form stored before this guard existed) must still be
	// updatable: refusing the update would leave the merchant unable to fix or
	// migrate their own row, and would not un-orphan anything.
	orphanAccount, _ := scopeTestAccount(t, pool, "nowsupd")
	orphanKey := scopeTestKey(t, ctx, keys, orphanAccount, "nowsupd-rw", []string{dbstore.ScopeReadWrite})
	orphan := &dbstore.Form{
		AccountID: orphanAccount,
		Slug:      "mcp-orphan-" + strings.ReplaceAll(orphanAccount, "-", "")[:10],
		Title:     "Orphan before the fix",
		Config:    map[string]any{"mode": "response", "blocks": []any{}},
		Theme:     map[string]any{},
		Status:    dbstore.FormStatusDraft,
	}
	if err := fs.Create(ctx, orphan); err != nil {
		t.Fatalf("seed orphan form: %v", err)
	}
	var wsOfOrphan *string
	if err := pool.QueryRow(ctx, `SELECT workspace_id::text FROM forms WHERE id=$1`, orphan.ID).Scan(&wsOfOrphan); err != nil {
		t.Fatalf("read orphan workspace_id: %v", err)
	}
	if wsOfOrphan != nil {
		t.Fatalf("fixture is wrong: the orphan form already carries workspace_id %v", *wsOfOrphan)
	}
	upd := succeeded(t, mcpCall(t, httpSrv.URL, orphanKey, "upsert_form", map[string]any{
		"form_id": orphan.ID,
		"title":   "Orphan renamed",
	}))
	if upd["status"] != "updated" {
		t.Fatalf("updating an existing orphan form returned %v, want status=updated — the guard must "+
			"gate form CREATION, not every write to an account with no workspace", upd)
	}
	var title string
	if err := pool.QueryRow(ctx, `SELECT title FROM forms WHERE id=$1`, orphan.ID).Scan(&title); err != nil {
		t.Fatalf("read back orphan title: %v", err)
	}
	if title != "Orphan renamed" {
		t.Fatalf("stored title = %q, want the update to have landed", title)
	}
}

// TestMCPDepsWorkspacesReachesMainGo pins the hop requireLiveWorkspace depends
// on. Deps.Workspaces is an OPTIONAL field, so the composite literal in main.go
// compiles just as happily without it — and then requireLiveWorkspace's nil
// branch silently allows every workspace-less create again, with nothing red.
// That is the same failure mode httpapi's
// TestWorkspacesReachesRESTHandlersInMainGo pins for the REST door (card
// t_6c0badbe); this is the MCP half.
func TestMCPDepsWorkspacesReachesMainGo(t *testing.T) {
	// Relative to this package's directory (api/internal/mcpsrv) ->
	// api/cmd/jomform-api/main.go.
	const mainGo = "../../cmd/jomform-api/main.go"
	src, err := os.ReadFile(mainGo)
	if err != nil {
		t.Fatalf("read %s: %v", mainGo, err)
	}
	text := string(src)
	if !strings.Contains(text, "mcpsrv.Deps{") {
		t.Fatalf("no mcpsrv.Deps{ literal in %s — this guard is looking at code that no longer "+
			"exists and would pass vacuously; fix the guard, not the test", mainGo)
	}
	// Assert the field is assigned inside that literal, not merely mentioned
	// somewhere in the file: take the span from the literal to its closing brace.
	i := strings.Index(text, "mcpsrv.Deps{")
	if i < 0 {
		t.Fatalf("mcpsrv.Deps{ vanished between reads of %s", mainGo)
	}
	depth, end := 0, -1
	for j := i + len("mcpsrv.Deps{"); j < len(text); j++ {
		switch text[j] {
		case '{':
			depth++
		case '}':
			if depth == 0 {
				end = j
			} else {
				depth--
			}
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		t.Fatalf("could not find the end of the mcpsrv.Deps literal in %s", mainGo)
	}
	if !strings.Contains(text[i:end], "Workspaces:") {
		t.Fatalf("the mcpsrv.Deps literal in %s never sets Workspaces — requireLiveWorkspace then "+
			"answers \"no guard\" for every account and the MCP door mints orphan forms again. "+
			"Wire the hop; do not delete this test.", mainGo)
	}
}
