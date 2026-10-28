package mcpsrv

// Workspace delete + form trash over the REAL MCP transport.
//
// The store-level behaviour is proven in dbstore's tests (workspace_delete_test.go,
// form_trash_test.go). THIS file proves the MCP surface adds what a store test
// cannot: the confirmation gate, the refusal SHAPES an agent has to act on
// (which categories block, "you need at least one workspace"), the ownership
// rule (owner, not admin-member), and the write-scope gate on both new tools.
//
// Every call goes through HTTPHandler → authenticate → the tool handler with a
// real API key, exactly as a configured MCP client does. There is no
// "the tool is not listed" assertion: a tool can be listed and still refuse (or
// hidden and still callable), so the assertion is always on the call result.
//
// Skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// wsMCPFixture wires a server over the shared test pool with the stores these
// tools need, and returns it plus a live httptest server.
func wsMCPFixture(t *testing.T, pool *pgxpool.Pool) *httptest.Server {
	t.Helper()
	srv := New(&Deps{
		Forms:            formsFor(pool),
		Accounts:         dbstore.NewAccountStore(pool),
		Keys:             dbstore.NewAPIKeyStore(pool),
		Workspaces:       dbstore.NewWorkspaceStore(pool),
		WorkspaceMembers: dbstore.NewWorkspaceMemberStore(pool),
		Products:         dbstore.NewProductStore(pool),
		Sales:            dbstore.NewSaleStore(pool),
		Subs:             dbstore.NewSubscriptionStore(pool),
		Users:            dbstore.NewUserStore(pool),
		Affiliates:       dbstore.NewAffiliateStore(pool),
	}, nil, nil, nil)
	httpSrv := httptest.NewServer(srv.HTTPHandler())
	t.Cleanup(httpSrv.Close)
	return httpSrv
}

// wsMCPUserWorkspaces creates a user and N workspaces (each with the account
// WorkspaceStore.Create makes) and mints a readwrite key on the LAST
// workspace's account.
//
// WHY THE KEY GOES ON THE LAST ONE. Deleting a workspace revokes every API key
// on ITS accounts — by design. If the fixture minted the caller's key on the
// workspace under test, that workspace's own delete would revoke the key and
// every later call in the test would fail to authenticate, which looks exactly
// like a broken handler. The key therefore lives on a workspace the tests never
// delete, and the tests delete wsIDs[0].
//
// Returns the user id, the workspace ids, the FIRST account id (the one the
// tests seed content into and then delete), and the raw key.
func wsMCPUserWorkspaces(t *testing.T, pool *pgxpool.Pool, tag string, n int) (userID string, wsIDs []string, targetAccID, rawKey string) {
	t.Helper()
	ctx := context.Background()
	sp := dbstore.NewWorkspaceStore(pool)
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ('wsmcp-`+tag+`-' || md5(random()::text) || '@example.test', 'it') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	for i := 0; i < n; i++ {
		w, err := sp.Create(ctx, userID, "WS "+tag, "wsmcp-"+tag+"-"+string(rune('a'+i))+"-"+userID[:6])
		if err != nil {
			t.Fatalf("create workspace %d: %v", i, err)
		}
		wsIDs = append(wsIDs, w.ID)
	}
	accOf := func(wsID string) string {
		var a string
		if err := pool.QueryRow(ctx, `SELECT id FROM accounts WHERE workspace_id=$1`, wsID).Scan(&a); err != nil {
			t.Fatalf("read account for workspace %s: %v", wsID, err)
		}
		return a
	}
	targetAccID = accOf(wsIDs[0])
	keyAccID := accOf(wsIDs[n-1])
	rawKey = "jf_wsmcp_" + tag + "_" + userID[:8] + "0000000000"
	if _, err := dbstore.NewAPIKeyStore(pool).Create(ctx, keyAccID, auth.HashAPIKey(rawKey), "wsmcp-"+tag, []string{dbstore.ScopeReadWrite}); err != nil {
		t.Fatalf("create api key: %v", err)
	}
	return userID, wsIDs, targetAccID, rawKey
}

// TestMCPWorkspaceDeleteRequiresConfirmSlug pins the confirmation gate: without
// the workspace's slug typed back, the delete is REFUSED as a result (not a
// transport error) and the workspace survives. This is the same level as
// revoke_api_key's confirm=true.
func TestMCPWorkspaceDeleteRequiresConfirmSlug(t *testing.T) {
	requireTestDB(t)
	ctx := context.Background()
	pool := testdb.Open(t)
	_, wsIDs, _, rawKey := wsMCPUserWorkspaces(t, pool, "confirm", 2)
	httpSrv := wsMCPFixture(t, pool)

	res := mcpCall(t, httpSrv.URL, rawKey, "delete_workspace", map[string]any{"workspace_id": wsIDs[0]})
	out := succeeded(t, res)
	if out["status"] != "refused" {
		t.Fatalf("delete_workspace without confirm_slug returned %v, want status=refused — a destructive "+
			"call must require the caller to name what it is destroying", out)
	}
	if !strings.Contains(strings.ToLower(str(out["error"])), "slug") {
		t.Errorf("refusal does not name the confirmation: %v", out["error"])
	}

	// A WRONG slug is refused too, and nothing is deleted.
	bad := succeeded(t, mcpCall(t, httpSrv.URL, rawKey, "delete_workspace",
		map[string]any{"workspace_id": wsIDs[0], "confirm_slug": "not-the-slug"}))
	if bad["status"] != "refused" {
		t.Errorf("delete_workspace with a MISMATCHED confirm_slug returned %v, want refused", bad)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM workspaces WHERE id=$1`, wsIDs[0]).Scan(&n); err != nil {
		t.Fatalf("count workspace: %v", err)
	}
	if n != 1 {
		t.Error("the workspace was deleted despite the confirmation being refused")
	}
}

// TestMCPWorkspaceDeleteRefusesLastWorkspaceThenSucceeds is Wan's rule over the
// protocol: the last workspace cannot go, and with two it can. It drives
// check_workspace_deletion for the pre-flight answer the agent is told to use.
func TestMCPWorkspaceDeleteRefusesLastWorkspaceThenSucceeds(t *testing.T) {
	requireTestDB(t)
	ctx := context.Background()
	pool := testdb.Open(t)
	_, wsIDs, _, rawKey := wsMCPUserWorkspaces(t, pool, "lastws", 2)
	httpSrv := wsMCPFixture(t, pool)
	sp := dbstore.NewWorkspaceStore(pool)

	slug := func(id string) string {
		w, err := sp.ByID(ctx, id)
		if err != nil {
			t.Fatalf("ByID: %v", err)
		}
		return w.Slug
	}

	// Pre-flight: the target is deletable (two workspaces, empty).
	chk := succeeded(t, mcpCall(t, httpSrv.URL, rawKey, "check_workspace_deletion",
		map[string]any{"workspace_id": wsIDs[0]}))
	if chk["can_delete"] != true {
		t.Errorf("check_workspace_deletion can_delete = %v, want true (2 workspaces, empty)", chk["can_delete"])
	}
	if chk["last_workspace"] != false {
		t.Errorf("check_workspace_deletion last_workspace = %v, want false", chk["last_workspace"])
	}

	// Delete the first: it goes to the BIN. status=trashed, and the row is
	// still there with deleted_at set — the whole point of this card.
	done := succeeded(t, mcpCall(t, httpSrv.URL, rawKey, "delete_workspace",
		map[string]any{"workspace_id": wsIDs[0], "confirm_slug": slug(wsIDs[0])}))
	if done["status"] != "trashed" {
		t.Fatalf("delete_workspace returned %v, want status=trashed — the tool moves the "+
			"workspace to the bin, it does not destroy it", done)
	}
	var n, binned int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FILTER (WHERE deleted_at IS NULL), COUNT(*) FILTER (WHERE deleted_at IS NOT NULL)
		   FROM workspaces WHERE id=$1`, wsIDs[0]).Scan(&n, &binned); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 || binned != 1 {
		t.Errorf("delete_workspace reported trashed but live/binned = %d/%d, want 0/1 — every row "+
			"must be kept", n, binned)
	}
	// Its account survives the move: the CHIP credentials are untouched.
	var accs int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM accounts WHERE workspace_id=$1`, wsIDs[0]).Scan(&accs); err != nil {
		t.Fatalf("count accounts: %v", err)
	}
	if accs != 1 {
		t.Errorf("accounts on the binned workspace = %d, want 1", accs)
	}

	// The remaining one is now the LAST LIVE workspace — refused, and it says so.
	// (A workspace already in the bin is not somewhere the user can still go.)
	last := succeeded(t, mcpCall(t, httpSrv.URL, rawKey, "delete_workspace",
		map[string]any{"workspace_id": wsIDs[1], "confirm_slug": slug(wsIDs[1])}))
	if last["status"] != "refused" || last["last_workspace"] != true {
		t.Fatalf("deleting the last live workspace returned %v, want status=refused + last_workspace=true", last)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM workspaces WHERE id=$1 AND deleted_at IS NULL`, wsIDs[1]).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Error("the last workspace was deleted — the \"at least one workspace\" rule is not enforced over MCP")
	}

	// And the pre-flight agrees.
	chk = succeeded(t, mcpCall(t, httpSrv.URL, rawKey, "check_workspace_deletion",
		map[string]any{"workspace_id": wsIDs[1]}))
	if chk["can_delete"] != false || chk["last_workspace"] != true {
		t.Errorf("check_workspace_deletion on the last workspace = %v, want can_delete=false + last_workspace=true", chk)
	}
}

// TestMCPWorkspaceDeleteOnAFullWorkspaceBinsIt replaces the old
// "lists blockers per category" test. Those blockers no longer block anything:
// Wan's decision was to let a FULL workspace go to the bin ("Buang guard —
// workspace penuh boleh masuk bin, restore bila perlu (data kekal 100%)"), so a
// refusal here would be the old behaviour masquerading as the new.
//
// What still has to hold is the READ half: check_workspace_deletion must keep
// reporting the census honestly (categories + counts), because the dashboard
// renders it beside the move — and the trash listing must keep reporting
// can_purge, which is the guard that DOES survive (sales).
func TestMCPWorkspaceDeleteOnAFullWorkspaceBinsIt(t *testing.T) {
	requireTestDB(t)
	ctx := context.Background()
	pool := testdb.Open(t)
	_, wsIDs, accID, rawKey := wsMCPUserWorkspaces(t, pool, "blockers", 2)
	httpSrv := wsMCPFixture(t, pool)
	sp := dbstore.NewWorkspaceStore(pool)

	// Two forms (one live, one trashed — BOTH count in the census) and one
	// product.
	fs := dbstore.NewFormStore(pool)
	live := &dbstore.Form{AccountID: accID, Slug: "wsmcp-blk-live-" + accID[:8], Title: "live", Status: "draft"}
	if err := fs.Create(ctx, live); err != nil {
		t.Fatalf("create live form: %v", err)
	}
	trashed := &dbstore.Form{AccountID: accID, Slug: "wsmcp-blk-trash-" + accID[:8], Title: "trashed", Status: "draft"}
	if err := fs.Create(ctx, trashed); err != nil {
		t.Fatalf("create trashed form: %v", err)
	}
	if err := fs.SoftDelete(ctx, accID, trashed.ID); err != nil {
		t.Fatalf("trash form: %v", err)
	}
	if err := dbstore.NewProductStore(pool).Create(ctx, &dbstore.Product{
		AccountID: accID, Name: "Blocker product", PriceSen: 500, Type: "one_time",
	}); err != nil {
		t.Fatalf("create product: %v", err)
	}

	w, err := sp.ByID(ctx, wsIDs[0])
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}

	// The READ half still reports the census: the trashed form counts (it is
	// still a `forms` row until it is purged), and the product shows up.
	chk := succeeded(t, mcpCall(t, httpSrv.URL, rawKey, "check_workspace_deletion",
		map[string]any{"workspace_id": wsIDs[0]}))
	var chkBlockers []struct {
		Kind  string `json:"kind"`
		Count int64  `json:"count"`
		Label string `json:"label"`
	}
	b, _ := json.Marshal(chk["blockers"])
	if err := json.Unmarshal(b, &chkBlockers); err != nil {
		t.Fatalf("decode pre-flight blockers %s: %v", b, err)
	}
	byKind := map[string]int64{}
	for _, x := range chkBlockers {
		byKind[x.Kind] = x.Count
		if x.Label == "" {
			t.Errorf("census entry %q has an empty label — the dashboard would name no category", x.Kind)
		}
	}
	if byKind["forms"] != 2 {
		t.Errorf("census[forms] = %d, want 2 (a trashed form is still a forms row until it is purged)", byKind["forms"])
	}
	if byKind["products"] != 1 {
		t.Errorf("census[products] = %d, want 1", byKind["products"])
	}
	// can_delete must now be TRUE: a full workspace is binnable. This is the
	// assertion that fails if the not_empty guard is still in place somewhere.
	if chk["can_delete"] != true {
		t.Fatalf("check_workspace_deletion on a FULL workspace = %v, want can_delete=true — the "+
			"not_empty guard must be gone: a full workspace goes to the bin", chk)
	}

	// And the write half agrees: the delete SUCCEEDS and keeps every row.
	out := succeeded(t, mcpCall(t, httpSrv.URL, rawKey, "delete_workspace",
		map[string]any{"workspace_id": wsIDs[0], "confirm_slug": w.Slug}))
	if out["status"] != "trashed" {
		t.Fatalf("delete_workspace on a FULL workspace returned %v, want status=trashed", out)
	}
	var binned int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM workspaces WHERE id=$1 AND deleted_at IS NOT NULL`, wsIDs[0]).Scan(&binned); err != nil {
		t.Fatalf("count workspace: %v", err)
	}
	if binned != 1 {
		t.Error("the full workspace was not binned")
	}
	// Every seeded row survived the move.
	var forms, prods int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM forms WHERE account_id=$1`, accID).Scan(&forms); err != nil {
		t.Fatalf("count forms: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM products WHERE account_id=$1`, accID).Scan(&prods); err != nil {
		t.Fatalf("count products: %v", err)
	}
	if forms != 2 {
		t.Errorf("forms after the trash = %d, want 2 — the trash destroys nothing", forms)
	}
	if prods != 1 {
		t.Errorf("products after the trash = %d, want 1 — the trash destroys nothing", prods)
	}
}

// TestMCPWorkspaceDeleteRefusesNonOwnerAdminMember pins the ownership rule: an
// admin MEMBER manages the workspace but may not destroy it. A manage-or-admin
// role gate alone would have allowed this, which is exactly why the handler
// checks ownership separately.
func TestMCPWorkspaceDeleteRefusesNonOwnerAdminMember(t *testing.T) {
	requireTestDB(t)
	ctx := context.Background()
	pool := testdb.Open(t)
	_, wsIDs, _, ownerKey := wsMCPUserWorkspaces(t, pool, "owner", 2)
	httpSrv := wsMCPFixture(t, pool)

	// A second user, added as an ADMIN member of the owner's workspace, with a
	// readwrite key on one of their OWN accounts (so the refusal is about
	// ownership, not about the credential).
	memberID, _, _, memberKey := wsMCPUserWorkspaces(t, pool, "member", 1)
	if _, err := pool.Exec(ctx,
		`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1,$2,'admin')`,
		wsIDs[0], memberID); err != nil {
		t.Fatalf("add admin member: %v", err)
	}
	// Sanity: the member IS an admin of that workspace, so the role gate passes
	// and only the ownership rule can refuse.
	var role string
	if err := pool.QueryRow(ctx,
		`SELECT role FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`,
		wsIDs[0], memberID).Scan(&role); err != nil {
		t.Fatalf("read member role: %v", err)
	}
	if role != dbstore.RoleAdmin {
		t.Fatalf("member role = %q, want admin — the fixture does not set up the case", role)
	}

	sp := dbstore.NewWorkspaceStore(pool)
	w, err := sp.ByID(ctx, wsIDs[0])
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	res := mcpCall(t, httpSrv.URL, memberKey, "delete_workspace",
		map[string]any{"workspace_id": wsIDs[0], "confirm_slug": w.Slug})
	ok, msg := refused(res)
	if !ok {
		t.Fatalf("an ADMIN MEMBER (not the owner) deleted the owner's workspace — %s", toolResultText(t, res))
	}
	if !strings.Contains(strings.ToLower(msg), "pemilik") {
		t.Errorf("refusal message %q does not say it is owner-only", msg)
	}

	// The owner may (two workspaces, empty). The fixture key is already the
	// owner's and lives on the workspace NOT being binned, so it survives the
	// move — the trash keeps every row, including the key's account.
	done := succeeded(t, mcpCall(t, httpSrv.URL, ownerKey, "delete_workspace",
		map[string]any{"workspace_id": wsIDs[0], "confirm_slug": w.Slug}))
	if done["status"] != "trashed" {
		t.Fatalf("the OWNER's delete returned %v, want trashed", done)
	}
}

// TestMCPWorkspaceDeleteRefusedForReadonlyKey is the write-scope axis on the
// destructive tool. A readonly key must be refused BEFORE anything is examined.
func TestMCPWorkspaceDeleteRefusedForReadonlyKey(t *testing.T) {
	requireTestDB(t)
	ctx := context.Background()
	pool := testdb.Open(t)
	_, wsIDs, accID, _ := wsMCPUserWorkspaces(t, pool, "ro", 2)
	httpSrv := wsMCPFixture(t, pool)
	sp := dbstore.NewWorkspaceStore(pool)
	w, err := sp.ByID(ctx, wsIDs[0])
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}

	roKey := scopeTestKey(t, ctx, dbstore.NewAPIKeyStore(pool), accID, "ws-ro", []string{dbstore.ScopeReadonly})
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"delete_workspace", map[string]any{"workspace_id": wsIDs[0], "confirm_slug": w.Slug}},
		{"purge_form", map[string]any{"form_id": "00000000-0000-0000-0000-000000000000", "confirm_slug": "x"}},
		{"restore_form", map[string]any{"form_id": "00000000-0000-0000-0000-000000000000"}},
	} {
		res := mcpCall(t, httpSrv.URL, roKey, tc.tool, tc.args)
		ok, msg := refused(res)
		if !ok {
			t.Errorf("a readonly key was allowed to call %s — %s", tc.tool, toolResultText(t, res))
			continue
		}
		if !strings.Contains(msg, "scope") {
			t.Errorf("%s was refused but not for the scope reason (%q)", tc.tool, msg)
		}
	}
	// list_deleted_forms is a READ: a readonly key must still be able to call it.
	// Without this control the test is satisfiable by breaking reads too.
	succeeded(t, mcpCall(t, httpSrv.URL, roKey, "list_deleted_forms", map[string]any{}))
}

// TestMCPFormTrashLifecycle is the card's sequence over MCP: create → trash →
// list_deleted_forms → restore (same slug, nothing destroyed) → trash → purge
// (row gone) — plus the two refusals that guard the purge.
func TestMCPFormTrashLifecycle(t *testing.T) {
	requireTestDB(t)
	ctx := context.Background()
	pool := testdb.Open(t)
	_, _, accID, rawKey := wsMCPUserWorkspaces(t, pool, "trash", 1)
	httpSrv := wsMCPFixture(t, pool)
	fs := dbstore.NewFormStore(pool)

	f := &dbstore.Form{AccountID: accID, Slug: "wsmcp-trash-" + accID[:8], Title: "Trash sample", Status: "draft"}
	if err := fs.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}

	// trash via the MCP tool (parity with the dashboard's bulk delete).
	succeeded(t, mcpCall(t, httpSrv.URL, rawKey, "delete_form", map[string]any{"form_id": f.ID}))

	// It must appear in the trash listing, with can_purge=true (no sales).
	lst := succeeded(t, mcpCall(t, httpSrv.URL, rawKey, "list_deleted_forms", map[string]any{}))
	var items []struct {
		ID         string `json:"id"`
		Slug       string `json:"slug"`
		SalesCount int64  `json:"sales_count"`
		CanPurge   bool   `json:"can_purge"`
		DeletedAt  string `json:"deleted_at"`
	}
	raw, _ := json.Marshal(lst["items"])
	if err := json.Unmarshal(raw, &items); err != nil {
		t.Fatalf("decode trash items %s: %v", raw, err)
	}
	var found bool
	for _, it := range items {
		if it.ID == f.ID {
			found = true
			if !it.CanPurge || it.SalesCount != 0 {
				t.Errorf("trash row = %v, want can_purge=true and sales_count=0", it)
			}
			if it.DeletedAt == "" {
				t.Error("trash row has no deleted_at — the trash view cannot show when it was trashed")
			}
		}
	}
	if !found {
		t.Fatalf("the trashed form %s is missing from list_deleted_forms", f.ID)
	}

	// purge without the confirmation → refused, form untouched.
	bad := succeeded(t, mcpCall(t, httpSrv.URL, rawKey, "purge_form", map[string]any{"form_id": f.ID}))
	if bad["status"] != "refused" {
		t.Fatalf("purge_form without confirm_slug returned %v, want refused", bad)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM forms WHERE id=$1`, f.ID).Scan(&n); err != nil {
		t.Fatalf("count form: %v", err)
	}
	if n != 1 {
		t.Error("the unconfirmed purge destroyed the form anyway")
	}

	// restore → back with the SAME slug.
	rest := succeeded(t, mcpCall(t, httpSrv.URL, rawKey, "restore_form", map[string]any{"form_id": f.ID}))
	if rest["status"] != "restored" {
		t.Fatalf("restore_form returned %v, want restored", rest)
	}
	got, err := fs.GetByID(ctx, accID, f.ID)
	if err != nil {
		t.Fatalf("GetByID after restore: %v", err)
	}
	if got.Slug != f.Slug {
		t.Errorf("slug after restore = %q, want %q (a restore must not rename)", got.Slug, f.Slug)
	}

	// trash again, then purge with the confirmation → gone for good.
	succeeded(t, mcpCall(t, httpSrv.URL, rawKey, "delete_form", map[string]any{"form_id": f.ID}))
	done := succeeded(t, mcpCall(t, httpSrv.URL, rawKey, "purge_form",
		map[string]any{"form_id": f.ID, "confirm_slug": f.Slug}))
	if done["status"] != "purged" {
		t.Fatalf("purge_form with the confirmation returned %v, want purged", done)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM forms WHERE id=$1`, f.ID).Scan(&n); err != nil {
		t.Fatalf("count form: %v", err)
	}
	if n != 0 {
		t.Error("purge_form reported purged but the row is still there")
	}

	// A LIVE form is invisible to the whole trash surface: purge and restore
	// both refuse, so neither can be used as a side door onto a live form.
	live := &dbstore.Form{AccountID: accID, Slug: "wsmcp-live-" + accID[:8], Title: "Live", Status: "draft"}
	if err := fs.Create(ctx, live); err != nil {
		t.Fatalf("create live form: %v", err)
	}
	if ok, _ := refused(mcpCall(t, httpSrv.URL, rawKey, "purge_form",
		map[string]any{"form_id": live.ID, "confirm_slug": live.Slug})); !ok {
		t.Error("purge_form accepted a LIVE form — the only way in must be trash → purge")
	}
	if ok, _ := refused(mcpCall(t, httpSrv.URL, rawKey, "restore_form",
		map[string]any{"form_id": live.ID})); !ok {
		t.Error("restore_form accepted a LIVE form")
	}
}

// TestMCPPurgeFormRefusesFormWithSales is the guard the card calls out as the
// important one, over the protocol. sales.form_id is ON DELETE SET NULL, so a
// purge would silently orphan the sale; the tool must refuse, name the count,
// and leave the sale's form_id intact.
func TestMCPPurgeFormRefusesFormWithSales(t *testing.T) {
	requireTestDB(t)
	ctx := context.Background()
	pool := testdb.Open(t)
	_, _, accID, rawKey := wsMCPUserWorkspaces(t, pool, "soldform", 1)
	httpSrv := wsMCPFixture(t, pool)
	fs := dbstore.NewFormStore(pool)
	ss := dbstore.NewSaleStore(pool)

	f := &dbstore.Form{AccountID: accID, Slug: "wsmcp-sold-" + accID[:8], Title: "Sold", Status: "published"}
	if err := fs.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}
	sale := &dbstore.Sale{
		AccountID: accID, FormID: f.ID, RefCode: "JF-WSMCP-" + accID[:8],
		Client: dbstore.SaleClient{FullName: "A", Email: "a@example.test"},
		Items:  []dbstore.SaleItem{}, Status: dbstore.SalePaid, PaymentMethod: "chip",
		SaleType: dbstore.SaleTypeOneTime,
	}
	if err := ss.Create(ctx, sale); err != nil {
		t.Fatalf("create sale: %v", err)
	}
	succeeded(t, mcpCall(t, httpSrv.URL, rawKey, "delete_form", map[string]any{"form_id": f.ID}))

	// The trash listing must already say "not purgable", so a caller is not
	// invited to try.
	lst := succeeded(t, mcpCall(t, httpSrv.URL, rawKey, "list_deleted_forms", map[string]any{}))
	var items []struct {
		ID         string `json:"id"`
		SalesCount int64  `json:"sales_count"`
		CanPurge   bool   `json:"can_purge"`
	}
	raw, _ := json.Marshal(lst["items"])
	if err := json.Unmarshal(raw, &items); err != nil {
		t.Fatalf("decode trash items: %v", err)
	}
	for _, it := range items {
		if it.ID == f.ID && (it.CanPurge || it.SalesCount != 1) {
			t.Errorf("sold form trash row = %v, want can_purge=false and sales_count=1", it)
		}
	}

	out := succeeded(t, mcpCall(t, httpSrv.URL, rawKey, "purge_form",
		map[string]any{"form_id": f.ID, "confirm_slug": f.Slug}))
	if out["status"] != "refused" {
		t.Fatalf("purge_form on a form with a sale returned %v, want refused — the sale would be orphaned", out)
	}
	if c, _ := out["sales_count"].(float64); c != 1 {
		t.Errorf("refusal sales_count = %v, want 1 (the message must state the number)", out["sales_count"])
	}
	// The sale still points at the form.
	got, err := ss.ByID(ctx, sale.ID)
	if err != nil {
		t.Fatalf("fetch sale: %v", err)
	}
	if got.FormID != f.ID {
		t.Errorf("sale.form_id = %q after the refused purge, want %q — the sale was orphaned", got.FormID, f.ID)
	}
}

// TestMCPRestoreFormRefusesWhenSlugWasRetaken keeps the "never a silent rename"
// rule on the MCP surface: a refusal as DATA (not a transport error), naming the
// obstacle, with the form left in the trash and its slug unchanged.
func TestMCPRestoreFormRefusesWhenSlugWasRetaken(t *testing.T) {
	requireTestDB(t)
	ctx := context.Background()
	pool := testdb.Open(t)
	_, _, accID, rawKey := wsMCPUserWorkspaces(t, pool, "reclaim", 1)
	httpSrv := wsMCPFixture(t, pool)
	fs := dbstore.NewFormStore(pool)

	slug := "wsmcp-reclaim-" + accID[:8]
	old := &dbstore.Form{AccountID: accID, Slug: slug, Title: "Old", Status: "draft"}
	if err := fs.Create(ctx, old); err != nil {
		t.Fatalf("create old form: %v", err)
	}
	succeeded(t, mcpCall(t, httpSrv.URL, rawKey, "delete_form", map[string]any{"form_id": old.ID}))
	// A new form takes the released slug.
	if err := fs.Create(ctx, &dbstore.Form{AccountID: accID, Slug: slug, Title: "New", Status: "draft"}); err != nil {
		t.Fatalf("create replacement: %v", err)
	}

	out := succeeded(t, mcpCall(t, httpSrv.URL, rawKey, "restore_form", map[string]any{"form_id": old.ID}))
	if out["status"] != "refused" {
		t.Fatalf("restore_form onto a re-taken slug returned %v, want refused", out)
	}
	if !strings.Contains(strings.ToLower(str(out["error"])), "slug") {
		t.Errorf("refusal does not explain the slug collision: %v", out["error"])
	}
	var oldSlug string
	if err := pool.QueryRow(ctx, `SELECT slug FROM forms WHERE id=$1`, old.ID).Scan(&oldSlug); err != nil {
		t.Fatalf("read slug: %v", err)
	}
	if oldSlug != slug {
		t.Errorf("refused restore renamed the form to %q — a silent rename moves a public URL", oldSlug)
	}
	var deleted *string
	if err := pool.QueryRow(ctx, `SELECT deleted_at::text FROM forms WHERE id=$1`, old.ID).Scan(&deleted); err != nil {
		t.Fatalf("read deleted_at: %v", err)
	}
	if deleted == nil {
		t.Error("the refused restore cleared deleted_at anyway")
	}
}

// str renders an `any` for message assertions without panicking on nil.
func str(v any) string {
	s, _ := v.(string)
	return s
}
