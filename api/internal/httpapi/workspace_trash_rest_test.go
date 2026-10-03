// REST transport for workspace delete + form trash/restore/purge.
//
// The store behaviour is pinned in dbstore's workspace_delete_test.go and
// form_trash_test.go; the MCP surface in mcpsrv's workspace_delete_mcp_test.go.
// THIS file covers the third transport, where the route TABLE itself is the
// risk: a literal segment that a {id} pattern would shadow, a wrong method, or a
// gate that the mux never reaches. Every request below goes through the real
// RegisterRoutes mux with a real Bearer key, and the assertions are on the
// response status AND on the database row — a 200 that wrote nothing, or a 403
// that wrote anyway, both fail here.
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// sessionReq issues one request with a session COOKIE against the real route
// table. Used for the workspace routes, which resolve the owner as a USER
// (sessionOwnerID) and are therefore session-only by design.
func sessionReq(t *testing.T, h *RESTHandlers, token, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// trashRESTFixture wires the handlers these routes need.
func trashRESTFixture(t *testing.T, pool *pgxpool.Pool) *RESTHandlers {
	t.Helper()
	return &RESTHandlers{
		Accounts:         dbstore.NewAccountStore(pool),
		Forms:            dbstore.NewFormStore(pool),
		Keys:             dbstore.NewAPIKeyStore(pool),
		SystemSettings:   dbstore.NewSystemSettingStore(pool),
		Workspaces:       dbstore.NewWorkspaceStore(pool),
		WorkspaceMembers: dbstore.NewWorkspaceMemberStore(pool),
	}
}

// trashRESTWorkspaceAccount creates a user + workspace + its account, so the
// form's account is workspace-linked exactly as it is in production (migration
// 000024). A bare account would make the slug-uniqueness index inert.
func trashRESTWorkspaceAccount(t *testing.T, pool *pgxpool.Pool, tag string) (workspaceID, accountID, userID string) {
	t.Helper()
	ctx := context.Background()
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1,'it') RETURNING id`,
		"rest-trash-"+tag+"-"+t.Name()[:6]+"-"+runKey()+"@example.test",
	).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	sp := dbstore.NewWorkspaceStore(pool)
	w, err := sp.Create(ctx, userID, "REST Trash "+tag, "rest-trash-"+tag+"-"+userID[:6])
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM accounts WHERE workspace_id=$1`, w.ID).Scan(&accountID); err != nil {
		t.Fatalf("read workspace account: %v", err)
	}
	return w.ID, accountID, userID
}

// TestRESTTrashRouteIsNotShadowedByFormID is the routing risk this change
// introduces: GET /v1/rest/forms/trash must reach the trash listing, not be
// swallowed by GET /v1/rest/forms/{id} with id="trash". If it were, the response
// would be a 404/400 about an invalid uuid rather than a JSON item list.
func TestRESTTrashRouteIsNotShadowedByFormID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST trash test in -short mode")
	}
	ctx := context.Background()
	pool := testdb.Open(t)
	_, accountID, _ := trashRESTWorkspaceAccount(t, pool, "shadow")
	h := trashRESTFixture(t, pool)
	key := scopeTestKey(t, pool, accountID, "trash-shadow")

	fs := dbstore.NewFormStore(pool)
	trashed := &dbstore.Form{AccountID: accountID, Slug: "rest-shadow-" + accountID[:8], Title: "T", Status: dbstore.FormStatusDraft}
	if err := fs.Create(ctx, trashed); err != nil {
		t.Fatalf("create form: %v", err)
	}
	if err := fs.SoftDelete(ctx, accountID, trashed.ID); err != nil {
		t.Fatalf("trash form: %v", err)
	}

	rec := bearerReq(t, h, key, http.MethodGet, "/v1/rest/forms/trash", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/rest/forms/trash = %d (%s) — the literal route did not win over "+
			"/v1/rest/forms/{id}", rec.Code, rec.Body.String())
	}
	// The listing is a BARE ARRAY (respond(w, 200, out) with out a slice), not
	// an {"items":...} envelope — unlike GET /v1/rest/forms.
	var out []struct {
		ID        string `json:"id"`
		Slug      string `json:"slug"`
		DeletedAt string `json:"deleted_at"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode trash listing %s: %v", rec.Body.String(), err)
	}
	var found bool
	for _, it := range out {
		if it.ID == trashed.ID {
			found = true
			if it.DeletedAt == "" {
				t.Error("the trashed form has no deleted_at in the REST listing")
			}
		}
	}
	if !found {
		t.Fatalf("the trashed form is missing from the REST trash listing: %s", rec.Body.String())
	}

	// The trash listing must NOT contain live forms — it is the complement of
	// the normal listing, not a superset.
	live := &dbstore.Form{AccountID: accountID, Slug: "rest-shadow-live-" + accountID[:8], Title: "L", Status: dbstore.FormStatusDraft}
	if err := fs.Create(ctx, live); err != nil {
		t.Fatalf("create live form: %v", err)
	}
	rec = bearerReq(t, h, key, http.MethodGet, "/v1/rest/forms/trash", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode second listing: %v", err)
	}
	for _, it := range out {
		if it.ID == live.ID {
			t.Error("a LIVE form appeared in the trash listing — the two listings overlap")
		}
	}
}

// TestRESTTrashLifecycle drives trash → restore → trash → purge over the real
// mux and checks the ROW after each step, so a 200 that did nothing fails.
func TestRESTTrashLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST trash test in -short mode")
	}
	ctx := context.Background()
	pool := testdb.Open(t)
	_, accountID, _ := trashRESTWorkspaceAccount(t, pool, "lifecycle")
	h := trashRESTFixture(t, pool)
	key := scopeTestKey(t, pool, accountID, "trash-lifecycle")

	fs := dbstore.NewFormStore(pool)
	f := &dbstore.Form{AccountID: accountID, Slug: "rest-life-" + accountID[:8], Title: "Lifecycle", Status: dbstore.FormStatusDraft}
	if err := fs.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}
	deletedAt := func() *string {
		var d *string
		if err := pool.QueryRow(ctx, `SELECT deleted_at::text FROM forms WHERE id=$1`, f.ID).Scan(&d); err != nil {
			t.Fatalf("read deleted_at: %v", err)
		}
		return d
	}

	// The endpoint that trashes is the EXISTING DELETE /forms/{id} (delete_form).
	if rec := bearerReq(t, h, key, http.MethodDelete, "/v1/rest/forms/"+f.ID, ""); rec.Code != http.StatusOK {
		t.Fatalf("DELETE /v1/rest/forms/{id} = %d (%s)", rec.Code, rec.Body.String())
	}
	if deletedAt() == nil {
		t.Fatal("DELETE /v1/rest/forms/{id} did not trash the form (deleted_at still NULL)")
	}

	// restore → deleted_at cleared, slug unchanged.
	if rec := bearerReq(t, h, key, http.MethodPost, "/v1/rest/forms/"+f.ID+"/restore", ""); rec.Code != http.StatusOK {
		t.Fatalf("POST /forms/{id}/restore = %d (%s)", rec.Code, rec.Body.String())
	}
	if deletedAt() != nil {
		t.Error("restore returned 200 but deleted_at is still set")
	}
	got, err := fs.GetByID(ctx, accountID, f.ID)
	if err != nil {
		t.Fatalf("GetByID after restore: %v", err)
	}
	if got.Slug != f.Slug {
		t.Errorf("slug changed across a restore: %q → %q", f.Slug, got.Slug)
	}

	// purge a LIVE form → refused (trash is the only way in).
	if rec := bearerReq(t, h, key, http.MethodDelete, "/v1/rest/forms/"+f.ID+"/purge",
		`{"confirm_slug":"`+f.Slug+`"}`); rec.Code == http.StatusOK {
		t.Error("DELETE /forms/{id}/purge accepted a LIVE form")
	}

	// trash again, then purge with the confirmation → row gone. The confirmation
	// travels in the BODY (decodeJSON), not a query string.
	if rec := bearerReq(t, h, key, http.MethodDelete, "/v1/rest/forms/"+f.ID, ""); rec.Code != http.StatusOK {
		t.Fatalf("second trash = %d (%s)", rec.Code, rec.Body.String())
	}
	if rec := bearerReq(t, h, key, http.MethodDelete, "/v1/rest/forms/"+f.ID+"/purge",
		`{"confirm_slug":"`+f.Slug+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("purge = %d (%s)", rec.Code, rec.Body.String())
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM forms WHERE id=$1`, f.ID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Error("purge returned 200 but the row is still there")
	}
}

// TestRESTPurgeRequiresConfirmSlug pins the confirmation on the REST transport.
// A purge without (or with a wrong) confirm_slug must not delete, because a
// query-string confirmation is exactly the kind of thing that gets dropped by a
// client refactor and would then silently stop confirming.
func TestRESTPurgeRequiresConfirmSlug(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST trash test in -short mode")
	}
	ctx := context.Background()
	pool := testdb.Open(t)
	_, accountID, _ := trashRESTWorkspaceAccount(t, pool, "confirm")
	h := trashRESTFixture(t, pool)
	key := scopeTestKey(t, pool, accountID, "trash-confirm")

	fs := dbstore.NewFormStore(pool)
	f := &dbstore.Form{AccountID: accountID, Slug: "rest-conf-" + accountID[:8], Title: "C", Status: dbstore.FormStatusDraft}
	if err := fs.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}
	if err := fs.SoftDelete(ctx, accountID, f.ID); err != nil {
		t.Fatalf("trash form: %v", err)
	}

	for _, tc := range []struct{ name, body string }{
		{"no confirm_slug at all", ""},
		{"a mismatched confirm_slug", `{"confirm_slug":"definitely-not-the-slug"}`},
	} {
		rec := bearerReq(t, h, key, http.MethodDelete, "/v1/rest/forms/"+f.ID+"/purge", tc.body)
		if rec.Code == http.StatusOK {
			t.Errorf("purge with %s returned 200, want a refusal", tc.name)
		}
		var n int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM forms WHERE id=$1`, f.ID).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		if n != 1 {
			t.Fatalf("purge with %s destroyed the form anyway", tc.name)
		}
	}
}

// TestRESTPurgeRefusedForReadonlyKeyAndStillReads is the scope axis on the new
// routes, with the read control so the test cannot pass by breaking everything.
func TestRESTPurgeRefusedForReadonlyKeyAndStillReads(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST trash test in -short mode")
	}
	ctx := context.Background()
	pool := testdb.Open(t)
	_, accountID, _ := trashRESTWorkspaceAccount(t, pool, "ro")

	fs := dbstore.NewFormStore(pool)
	f := &dbstore.Form{AccountID: accountID, Slug: "rest-ro-" + accountID[:8], Title: "RO", Status: dbstore.FormStatusDraft}
	if err := fs.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}
	if err := fs.SoftDelete(ctx, accountID, f.ID); err != nil {
		t.Fatalf("trash form: %v", err)
	}

	roHandler, roKey := scopeRESTFixture(t, pool, accountID, "trash-ro", []string{dbstore.ScopeReadonly})
	// Read control: the trash listing is a read and must succeed.
	if rec := bearerReq(t, roHandler, roKey, http.MethodGet, "/v1/rest/forms/trash", ""); rec.Code != http.StatusOK {
		t.Fatalf("a readonly key was refused the trash READ: %d (%s)", rec.Code, rec.Body.String())
	}

	for _, tc := range []struct{ what, method, path, body string }{
		{"purge", http.MethodDelete, "/v1/rest/forms/" + f.ID + "/purge", `{"confirm_slug":"` + f.Slug + `"}`},
		{"restore", http.MethodPost, "/v1/rest/forms/" + f.ID + "/restore", ""},
	} {
		rec := bearerReq(t, roHandler, roKey, tc.method, tc.path, tc.body)
		if rec.Code != http.StatusForbidden {
			t.Errorf("a readonly key got %d on %s — want 403 (%s)", rec.Code, tc.what, rec.Body.String())
			continue
		}
		if !strings.Contains(rec.Body.String(), "scope") {
			t.Errorf("%s was refused but not for the scope reason: %s", tc.what, rec.Body.String())
		}
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM forms WHERE id=$1`, f.ID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Error("a readonly key's refused purge/restore changed the row")
	}
}

// TestRESTWorkspaceDeleteRequiresOwnerAndConfirm drives DELETE /v1/workspaces/{id}
// through the real mux: the confirmation, the last-workspace rule, and the row.
//
// SESSION, not a Bearer key. These routes resolve the caller with
// sessionOwnerID (the owner is a USER), so an API-key request is 401 by design
// — a workspace delete over REST is a dashboard action, while the API-key path
// is the MCP tools. The cookie comes from the real session store so this drives
// the same code prod does.
//
// THE DELETE BINS, IT DOES NOT DESTROY. Every "the row is still there"
// assertion below is paired with a deleted_at assertion, because a trash and a
// delete are indistinguishable from the status code: the only way to tell the
// card's promise ("Keep every row") from the old behaviour is to read the
// column.
func TestRESTWorkspaceDeleteRequiresOwnerAndConfirm(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST workspace test in -short mode")
	}
	ctx := context.Background()
	pool := testdb.Open(t)
	sess := authTestValkey(t)
	h := trashRESTFixture(t, pool)
	h.Sessions = sess
	h.Users = dbstore.NewUserStore(pool)

	userID := mkWsUser(t, pool, "rest-ws-owner")
	sp := dbstore.NewWorkspaceStore(pool)
	first, err := sp.Create(ctx, userID, "REST WS One", "rest-ws-one-"+userID[:6])
	if err != nil {
		t.Fatalf("create workspace 1: %v", err)
	}
	second, err := sp.Create(ctx, userID, "REST WS Two", "rest-ws-two-"+userID[:6])
	if err != nil {
		t.Fatalf("create workspace 2: %v", err)
	}
	token, err := sess.Create(ctx, userID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	// No confirmation → refused, row intact and LIVE.
	rec := sessionReq(t, h, token, http.MethodDelete, "/v1/workspaces/"+first.ID, "")
	if rec.Code == http.StatusOK {
		t.Error("DELETE /v1/workspaces/{id} without confirm_slug returned 200")
	}
	// Wrong confirmation → refused, row intact.
	rec = sessionReq(t, h, token, http.MethodDelete, "/v1/workspaces/"+first.ID,
		`{"confirm_slug":"nope"}`)
	if rec.Code == http.StatusOK {
		t.Error("DELETE /v1/workspaces/{id} with a mismatched confirm_slug returned 200")
	}
	live, binned := 0, 0
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FILTER (WHERE deleted_at IS NULL), COUNT(*) FILTER (WHERE deleted_at IS NOT NULL)
		   FROM workspaces WHERE id=$1`, first.ID).Scan(&live, &binned); err != nil {
		t.Fatalf("count: %v", err)
	}
	if live != 1 || binned != 0 {
		t.Fatal("an unconfirmed workspace delete changed the row")
	}

	// The pre-flight read agrees it is binnable, and names the last-workspace rule.
	rec = sessionReq(t, h, token, http.MethodGet, "/v1/workspaces/"+first.ID+"/deletion-check", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/workspaces/{id}/deletion-check = %d (%s)", rec.Code, rec.Body.String())
	}
	var chk struct {
		CanDelete     bool `json:"can_delete"`
		Empty         bool `json:"empty"`
		LastWorkspace bool `json:"last_workspace"`
		IsOwner       bool `json:"is_owner"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &chk); err != nil {
		t.Fatalf("decode pre-flight %s: %v", rec.Body.String(), err)
	}
	if !chk.CanDelete || !chk.Empty || chk.LastWorkspace || !chk.IsOwner {
		t.Errorf("pre-flight = %+v, want can_delete/empty/is_owner true and last_workspace false", chk)
	}

	// With two workspaces and the confirmation → BINNED, and the response says so.
	rec = sessionReq(t, h, token, http.MethodDelete, "/v1/workspaces/"+first.ID,
		`{"confirm_slug":"`+first.Slug+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("confirmed DELETE /v1/workspaces/{id} = %d (%s)", rec.Code, rec.Body.String())
	}
	var delBody map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &delBody); err != nil {
		t.Fatalf("delete response is not JSON: %v (%s)", err, rec.Body.String())
	}
	if got := delBody["status"]; got != "trashed" {
		t.Errorf("delete status = %v, want \"trashed\" — the route moves the workspace to the bin, "+
			"it does not destroy it", got)
	}
	if got := delBody["restorable"]; got != true {
		t.Errorf("delete response restorable = %v, want true — the dashboard renders this", got)
	}
	// THE ROW IS STILL THERE, and marked. This is the card's whole point.
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FILTER (WHERE deleted_at IS NULL), COUNT(*) FILTER (WHERE deleted_at IS NOT NULL)
		   FROM workspaces WHERE id=$1`, first.ID).Scan(&live, &binned); err != nil {
		t.Fatalf("count after delete: %v", err)
	}
	if live != 0 || binned != 1 {
		t.Fatalf("workspaces live/binned = %d/%d after the delete, want 0/1 — the delete must "+
			"MARK the workspace (keep every row), not remove it", live, binned)
	}
	if n := countRowsREST(t, pool, `SELECT COUNT(*) FROM accounts WHERE workspace_id=$1`, first.ID); n != 1 {
		t.Errorf("accounts left on the binned workspace = %d, want 1 — its CHIP account must survive "+
			"the move to the bin", n)
	}

	// The binned workspace is out of the live list and in the trash listing.
	rec = sessionReq(t, h, token, http.MethodGet, "/v1/workspaces/trash", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/workspaces/trash = %d (%s) — the literal route did not win", rec.Code, rec.Body.String())
	}
	var bin []struct {
		ID        string `json:"id"`
		Slug      string `json:"slug"`
		DeletedAt string `json:"deleted_at"`
		CanPurge  bool   `json:"can_purge"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &bin); err != nil {
		t.Fatalf("decode trash listing %s: %v", rec.Body.String(), err)
	}
	var found bool
	for _, it := range bin {
		if it.ID == first.ID {
			found = true
			if it.DeletedAt == "" {
				t.Error("the binned workspace has no deleted_at in the REST trash listing")
			}
			if !it.CanPurge {
				t.Error("a never-sold binned workspace reported can_purge=false")
			}
		}
		if it.ID == second.ID {
			t.Error("a LIVE workspace appeared in the workspace trash listing")
		}
	}
	if !found {
		t.Fatalf("the binned workspace is missing from the REST trash listing: %s", rec.Body.String())
	}

	// Restore brings it back, live again.
	rec = sessionReq(t, h, token, http.MethodPost, "/v1/workspaces/"+first.ID+"/restore", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /v1/workspaces/{id}/restore = %d (%s)", rec.Code, rec.Body.String())
	}
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FILTER (WHERE deleted_at IS NULL), COUNT(*) FILTER (WHERE deleted_at IS NOT NULL)
		   FROM workspaces WHERE id=$1`, first.ID).Scan(&live, &binned); err != nil {
		t.Fatalf("count after restore: %v", err)
	}
	if live != 1 || binned != 0 {
		t.Errorf("workspaces live/binned = %d/%d after the restore, want 1/0 — restore returned 200 "+
			"but the workspace is still in the bin", live, binned)
	}

	// The LAST workspace is refused, and the pre-flight says so. `first` was
	// restored above, so binning it again first is what makes `second` the last
	// LIVE workspace — the guard counts live ones.
	rec = sessionReq(t, h, token, http.MethodDelete, "/v1/workspaces/"+first.ID,
		`{"confirm_slug":"`+first.Slug+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("re-bin first = %d (%s)", rec.Code, rec.Body.String())
	}
	rec = sessionReq(t, h, token, http.MethodDelete, "/v1/workspaces/"+second.ID,
		`{"confirm_slug":"`+second.Slug+`"}`)
	if rec.Code == http.StatusOK {
		t.Error("the LAST live workspace was deleted — the \"at least one workspace\" rule is not enforced on REST")
	}
	rec = sessionReq(t, h, token, http.MethodGet, "/v1/workspaces/"+second.ID+"/deletion-check", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &chk); err != nil {
		t.Fatalf("decode second pre-flight: %v", err)
	}
	if chk.CanDelete || !chk.LastWorkspace {
		t.Errorf("pre-flight on the last workspace = %+v, want can_delete=false + last_workspace=true", chk)
	}
}

// TestRESTWorkspaceDeleteRefusedForNonOwner covers the ownership rule on the
// REST transport: an admin MEMBER may manage the workspace but not destroy it.
func TestRESTWorkspaceDeleteRefusedForNonOwner(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST workspace test in -short mode")
	}
	ctx := context.Background()
	pool := testdb.Open(t)
	sess := authTestValkey(t)
	h := trashRESTFixture(t, pool)
	h.Sessions = sess
	h.Users = dbstore.NewUserStore(pool)

	ownerID := mkWsUser(t, pool, "rest-ws-own2")
	sp := dbstore.NewWorkspaceStore(pool)
	target, err := sp.Create(ctx, ownerID, "REST WS Target", "rest-ws-tgt-"+ownerID[:6])
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := sp.Create(ctx, ownerID, "REST WS Keep", "rest-ws-keep-"+ownerID[:6]); err != nil {
		t.Fatalf("create second workspace: %v", err)
	}

	// The member: their own workspace (so they still have somewhere to land)
	// plus admin rights on the target.
	memberID := mkWsUser(t, pool, "rest-ws-mem2")
	if _, err := sp.Create(ctx, memberID, "REST WS Member", "rest-ws-mem-"+memberID[:6]); err != nil {
		t.Fatalf("create member workspace: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1,$2,'admin')`,
		target.ID, memberID); err != nil {
		t.Fatalf("add admin member: %v", err)
	}
	token, err := sess.Create(ctx, memberID)
	if err != nil {
		t.Fatalf("create member session: %v", err)
	}

	rec := sessionReq(t, h, token, http.MethodDelete, "/v1/workspaces/"+target.ID,
		`{"confirm_slug":"`+target.Slug+`"}`)
	if rec.Code < 400 {
		t.Errorf("an ADMIN MEMBER deleted the owner's workspace over REST: %d (%s)", rec.Code, rec.Body.String())
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM workspaces WHERE id=$1`, target.ID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatal("the refused delete removed the workspace anyway")
	}
}

// countRowsREST counts one row set through the test pool — the assertion
// primitive for every "this row survived" claim in this file, since a trash and
// a delete are indistinguishable from a status code.
func countRowsREST(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("count (%s): %v", sql, err)
	}
	return n
}

func mkWsUser(t *testing.T, pool *pgxpool.Pool, tag string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO users (email, password_hash) VALUES ($1,'it') RETURNING id`,
		tag+"-"+t.Name()+"-"+runKey()+"@example.test").Scan(&id); err != nil {
		t.Fatalf("create user %s: %v", tag, err)
	}
	return id
}

// TestRESTTrashRefusalsCarryTheirMachineCode pins the SEVER-side half of the
// dashboard fix for the two refusals main grew after #6177's base (#682):
// restoring a form whose slug another form took, and purging a form that was
// ever sold. Both sites are hand-rolled writeJSON bodies rather than writeErr, so
// nothing else in the package couples their message to a code — without this
// test the `code` field can be dropped in a refactor and the dashboard silently
// falls back to its generic sentence, hiding the obstacle the user has to clear.
//
// The assertions are on the REAL mux response body after the REAL store refusal,
// so a test that only checked CodeFormSlugTaken's constant would not bite.
func TestRESTTrashRefusalsCarryTheirMachineCode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST trash test in -short mode")
	}
	ctx := context.Background()
	pool := testdb.Open(t)
	_, accountID, _ := trashRESTWorkspaceAccount(t, pool, "codes")
	h := trashRESTFixture(t, pool)
	key := scopeTestKey(t, pool, accountID, "trash-codes")

	fs := dbstore.NewFormStore(pool)

	// ── form.slugTaken: a restore whose slug was re-claimed while trashed.
	trashed := &dbstore.Form{AccountID: accountID, Slug: "rest-code-a-" + accountID[:8], Title: "A", Status: dbstore.FormStatusDraft}
	if err := fs.Create(ctx, trashed); err != nil {
		t.Fatalf("create form: %v", err)
	}
	if err := fs.SoftDelete(ctx, accountID, trashed.ID); err != nil {
		t.Fatalf("trash form: %v", err)
	}
	// A live form takes the slug the trashed one released (the unique index
	// covers live rows only, so this succeeds and the restore must then fail).
	taker := &dbstore.Form{AccountID: accountID, Slug: trashed.Slug, Title: "Taker", Status: dbstore.FormStatusDraft}
	if err := fs.Create(ctx, taker); err != nil {
		t.Fatalf("create slug-taker: %v", err)
	}
	rec := bearerReq(t, h, key, http.MethodPost, "/v1/rest/forms/"+trashed.ID+"/restore", "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("restore over a re-claimed slug = %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
	// The restore body. A fresh map per decode — see the purge decode below for
	// why reusing one is a trap.
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("restore refusal is not JSON: %v (%s)", err, rec.Body.String())
	}
	if got := body["code"]; got != CodeFormSlugTaken {
		t.Errorf("restore refusal code = %v, want %q — without it the dashboard "+
			"prints a generic sentence and never says which slug to change", got, CodeFormSlugTaken)
	}
	if s, _ := body["error"].(string); s == "" {
		t.Error("restore refusal lost its wire prose — API-key/curl clients render that string")
	}

	// ── form.hasSales: a purge of a form that was ever sold.
	sold := &dbstore.Form{AccountID: accountID, Slug: "rest-code-b-" + accountID[:8], Title: "B", Status: dbstore.FormStatusDraft}
	if err := fs.Create(ctx, sold); err != nil {
		t.Fatalf("create sold form: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO sales (account_id, form_id, ref_code, client, items, subtotal_sen, total_sen, sale_type, payment_method, status)
		 VALUES ($1, $2, $3, '{}'::jsonb, '[]'::jsonb, 100, 100, 'one_time', 'chip', 'paid')`,
		accountID, sold.ID, "IT-CODES-"+accountID[:8]); err != nil {
		t.Fatalf("insert sale: %v", err)
	}
	if err := fs.SoftDelete(ctx, accountID, sold.ID); err != nil {
		t.Fatalf("trash sold form: %v", err)
	}
	rec = bearerReq(t, h, key, http.MethodDelete, "/v1/rest/forms/"+sold.ID+"/purge",
		`{"confirm_slug":"`+sold.Slug+`"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("purge of a sold form = %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
	// A SEPARATE map for the second decode. json.Unmarshal MERGES into a non-nil
	// map, so reusing `body` would carry the restore body's `code` into this
	// assertion and a MISSING field would read as present — which is exactly
	// what a first draft of this test did.
	var purgeBody map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &purgeBody); err != nil {
		t.Fatalf("purge refusal is not JSON: %v (%s)", err, rec.Body.String())
	}
	if got := purgeBody["code"]; got != CodeFormHasSales {
		t.Errorf("purge refusal code = %v, want %q — without it the dashboard "+
			"never says the form has sales", got, CodeFormHasSales)
	}
	// The count is part of the refusal (the UI reads it); adding `code` must not
	// have displaced it.
	if _, ok := purgeBody["sales_count"]; !ok {
		t.Error("purge refusal lost its sales_count — the code was added next to it, not instead of it")
	}
	// And the form must still be there: a refusal that deleted anyway would be
	// the worst shape of this bug.
	var n int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM forms WHERE id=$1`, sold.ID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Error("the refused purge destroyed the form anyway")
	}
}

// TestRESTWorkspacePurgeRefusalCarriesItsCode replaces the old
// workspace.notEmpty assertion. That code retired with its guard: the DELETE no
// longer refuses on content at all, so there is no 409 to carry it. The refusal
// that still needs a machine code on this transport is the permanent PURGE of a
// workspace that has been SOLD — a writeJSON literal map (not writeErr) because
// it also carries `sales_count`, so the `code` had to be added alongside it and
// both must survive together.
//
// The route is SESSION-only (an API key has no user, so a key cannot drive it),
// and the tool in front of the store is never proposable to the in-app chat —
// but purge_workspace IS a registered MCP tool an owner's write-scoped key can
// call, the same class as purge_form/purge_product/purge_media. That distinction
// is what purgeWorkspace's comment states, and
// mcpsrv/purge_reachability_claim_test.go is the guard that keeps it stated.
func TestRESTWorkspacePurgeRefusalCarriesItsCode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST workspace test in -short mode")
	}
	ctx := context.Background()
	pool := testdb.Open(t)
	sess := authTestValkey(t)
	h := trashRESTFixture(t, pool)
	h.Sessions = sess
	h.Users = dbstore.NewUserStore(pool)

	userID := mkWsUser(t, pool, "rest-ws-code")
	sp := dbstore.NewWorkspaceStore(pool)
	if _, err := sp.Create(ctx, userID, "REST WS Keep", "rest-ws-k-"+userID[:6]); err != nil {
		t.Fatalf("create keep workspace: %v", err)
	}
	target, err := sp.Create(ctx, userID, "REST WS Sold", "rest-ws-f-"+userID[:6])
	if err != nil {
		t.Fatalf("create target workspace: %v", err)
	}
	var targetAccount string
	if err := pool.QueryRow(ctx, `SELECT id FROM accounts WHERE workspace_id=$1`, target.ID).Scan(&targetAccount); err != nil {
		t.Fatalf("read target account: %v", err)
	}
	// A SALE in the target: this is what refuses the permanent delete.
	if _, err := pool.Exec(ctx,
		`INSERT INTO sales (account_id, ref_code, client, items, subtotal_sen, total_sen, sale_type, payment_method, status)
		 VALUES ($1, $2, '{}'::jsonb, '[]'::jsonb, 100, 100, 'one_time', 'chip', 'paid')`,
		targetAccount, "IT-WSPURGE-"+targetAccount[:8]); err != nil {
		t.Fatalf("seed sale: %v", err)
	}
	token, err := sess.Create(ctx, userID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	// A LIVE workspace cannot be purged at all — the only way in is trash.
	rec := sessionReq(t, h, token, http.MethodDelete, "/v1/workspaces/"+target.ID+"/purge",
		`{"confirm_slug":"`+target.Slug+`"}`)
	if rec.Code == http.StatusOK {
		t.Fatalf("DELETE /v1/workspaces/{id}/purge accepted a LIVE workspace: %d (%s)",
			rec.Code, rec.Body.String())
	}

	// Bin it, then try to destroy it. The refusal must carry the machine code and
	// the count, and must destroy nothing.
	rec = sessionReq(t, h, token, http.MethodDelete, "/v1/workspaces/"+target.ID,
		`{"confirm_slug":"`+target.Slug+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("bin the workspace = %d (%s)", rec.Code, rec.Body.String())
	}
	rec = sessionReq(t, h, token, http.MethodDelete, "/v1/workspaces/"+target.ID+"/purge",
		`{"confirm_slug":"`+target.Slug+`"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("purge of a sold workspace = %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("workspace purge refusal is not JSON: %v (%s)", err, rec.Body.String())
	}
	if got := body["code"]; got != CodeWorkspaceHasSales {
		t.Errorf("purge refusal code = %v, want %q — without it the dashboard prints "+
			"\"we couldn't delete this workspace\" and never says the sales are why", got, CodeWorkspaceHasSales)
	}
	// The count is part of the refusal (the UI reads it); `code` was added NEXT
	// TO it, not instead of it.
	if got, ok := body["sales_count"].(float64); !ok || got < 1 {
		t.Errorf("purge refusal sales_count = %v, want >= 1 (the code was added alongside it)", body["sales_count"])
	}
	// An unconfirmed purge is refused too, and for the confirmation — the row
	// must not be destroyed by a purge that never named it.
	rec = sessionReq(t, h, token, http.MethodDelete, "/v1/workspaces/"+target.ID+"/purge",
		`{"confirm_slug":"nope"}`)
	if rec.Code == http.StatusOK {
		t.Error("an unconfirmed workspace purge returned 200")
	}
	if n := countRowsREST(t, pool, `SELECT COUNT(*) FROM workspaces WHERE id=$1`, target.ID); n != 1 {
		t.Error("a refused workspace purge removed the row anyway")
	}
	if n := countRowsREST(t, pool, `SELECT COUNT(*) FROM sales WHERE account_id=$1`, targetAccount); n != 1 {
		t.Error("a refused workspace purge destroyed the sale anyway")
	}
	// The trash listing reports it as unpurgable, so the UI never offers the
	// button in the first place.
	rec = sessionReq(t, h, token, http.MethodGet, "/v1/workspaces/trash", "")
	var bin []struct {
		ID       string `json:"id"`
		CanPurge bool   `json:"can_purge"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &bin); err != nil {
		t.Fatalf("decode trash listing: %v (%s)", err, rec.Body.String())
	}
	for _, it := range bin {
		if it.ID == target.ID && it.CanPurge {
			t.Error("a SOLD binned workspace reported can_purge=true — the UI would offer a " +
				"permanent delete the server refuses")
		}
	}
}

// TestRESTWorkspacePurgeNotFoundIDsAnswer404 is the card's regression guard: an
// id that names NO trashed workspace must answer 404 «workspace tidak dijumpai
// dalam tong sampah», never 500.
//
// THE DEFECT THIS PINS (card t_a71ee62b, measured live on v1.19.1). All three
// shapes below answered 500 {"code":"internal","error":"internal error"} with
// error_class=internal, logged with a stack trace, while the SIBLING routes on
// the same session (`POST .../restore`, `DELETE /v1/workspaces/{id}`) answered
// 404. Root cause: TrashedByID goes through scanTrashedWorkspace, which maps a
// missing row AND SQLSTATE 22P02 to pgx.ErrNoRows, and this handler tested only
// errors.Is(err, dbstore.ErrWorkspaceNotFound) — so the miss fell through to
// respondErr's default branch.
//
// WHY ALL THREE SHAPES, and why the LIVE one is the important one:
//
//   - an unknown uuid — the plain miss;
//   - a NON-uuid — Postgres raises 22P02 before any row is considered, and that
//     must be a 404 for the caller's bad id, not a server-fault metric;
//   - a REAL, LIVE workspace id — the case the comment above purgeWorkspace
//     asserts ("a LIVE workspace is a 404 here") and the one a merchant is
//     most likely to hit by pasting their own workspace id. It must be a 404
//     AND leave the row completely untouched, so a fix that turned it into a
//     200 (or that let the purge act on a live row) fails here.
//
// The 404 is asserted on the BODY, not just the status: a 404 produced
// elsewhere on the route cannot satisfy the message assertion, and the body is
// the same one the sibling restore route answers, so a client can tell the two
// apart from the status and the prose together.
//
// WHY NOT A BUG-PROOF BY REVERTING THE MAPPING: the store-level fix means a
// revert would make this an integration-only red (it needs a real Postgres to
// produce the 22P02/ErrNoRows miss at all), which is exactly what this test is.
// The dbstore half — TrashedByID returning the SENTINEL rather than the raw
// driver error — is pinned separately in
// dbstore/workspace_delete_test.go:TestWorkspaceTrashedByIDNotFoundIsTheStoreSentinel.
func TestRESTWorkspacePurgeNotFoundIDsAnswer404(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST workspace test in -short mode")
	}
	ctx := context.Background()
	pool := testdb.Open(t)
	sess := authTestValkey(t)
	h := trashRESTFixture(t, pool)
	h.Sessions = sess
	h.Users = dbstore.NewUserStore(pool)

	userID := mkWsUser(t, pool, "rest-ws-404")
	sp := dbstore.NewWorkspaceStore(pool)
	// A second workspace so the LIVE id below is not the caller's last one:
	// the last_workspace rule is about DELETE /v1/workspaces/{id}, not about
	// this route, but a fixture that cannot be binned would hide a regression
	// in the sibling delete path.
	if _, err := sp.Create(ctx, userID, "REST WS Keep", "rest-ws-404k-"+userID[:6]); err != nil {
		t.Fatalf("create keep workspace: %v", err)
	}
	live, err := sp.Create(ctx, userID, "REST WS Live", "rest-ws-404l-"+userID[:6])
	if err != nil {
		t.Fatalf("create live workspace: %v", err)
	}
	token, err := sess.Create(ctx, userID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	const trashNotFound = "workspace tidak dijumpai dalam tong sampah"

	for _, tc := range []struct{ what, id string }{
		{"an unknown uuid", "00000000-0000-0000-0000-0000000000ff"},
		{"a non-uuid", "not-a-uuid"},
		{"a REAL, LIVE workspace id", live.ID},
	} {
		rec := sessionReq(t, h, token, http.MethodDelete,
			"/v1/workspaces/"+tc.id+"/purge", `{"confirm_slug":"nope"}`)
		if rec.Code != http.StatusNotFound {
			t.Errorf("purge with %s (%s) = %d, want 404 — body=%s. A not-found id is not a "+
				"server fault: the raw pgx miss (pgx.ErrNoRows, or SQLSTATE 22P02 for a "+
				"non-uuid) must be mapped to the store's not-found sentinel before it reaches "+
				"respondErr, or it lands on the default branch as 500 + error_class=internal.",
				tc.what, tc.id, rec.Code, rec.Body.String())
			continue
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Errorf("purge with %s (%s): body is not JSON: %v (%s)", tc.what, tc.id, err, rec.Body.String())
			continue
		}
		if got := body["error"]; got != trashNotFound {
			t.Errorf("purge with %s (%s): error = %v, want %q — the same body the sibling "+
				"restore route answers, so a client can distinguish the two", tc.what, tc.id, got, trashNotFound)
		}
		if got := body["code"]; got == ClassInternal {
			t.Errorf("purge with %s (%s): code = %q — a not-found answered the INTERNAL class "+
				"code, which is the 500 this card is about, wearing a 404", tc.what, tc.id, got)
		}
	}

	// The LIVE row must be untouched: never destroyed, never binned.
	if n := countRowsREST(t, pool,
		`SELECT COUNT(*) FROM workspaces WHERE id=$1 AND deleted_at IS NULL`, live.ID); n != 1 {
		t.Error("a purge aimed at a LIVE workspace disturbed the row — the trashed-only " +
			"guard is what makes trash → purge the only path to data loss")
	}

	// Control: the sibling bin route on the SAME session answers 404 for the same
	// ghost id. Without it a guard that 404s everything (including a route that
	// should act) would pass the loop above.
	rec := sessionReq(t, h, token, http.MethodDelete,
		"/v1/workspaces/00000000-0000-0000-0000-0000000000ff", `{"confirm_slug":"nope"}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("control: DELETE /v1/workspaces/<ghost> = %d (%s), want 404 — the sibling "+
			"route was correct at the shipped revision, so this is the reference the purge "+
			"route must match", rec.Code, rec.Body.String())
	}

	// The taxonomy below the miss is UNCHANGED by this fix, and that is the point
	// of asserting it: a stranger's TRASHED workspace is FOUND by the trash read
	// (it is not owner-scoped), so it lands on the owner check and answers 403 —
	// not 404, and not the 500 the miss shapes answered. A fix that had swallowed
	// every error into "not found" would pass the loop above and fail here.
	strangerID := mkWsUser(t, pool, "rest-ws-stranger")
	stranger, err := sp.Create(ctx, strangerID, "REST WS Stranger", "rest-ws-str-"+strangerID[:6])
	if err != nil {
		t.Fatalf("create stranger workspace: %v", err)
	}
	// Two workspaces, so the stranger may bin one (the last_workspace rule).
	if _, err := sp.Create(ctx, strangerID, "REST WS Stranger 2", "rest-ws-str2-"+strangerID[:6]); err != nil {
		t.Fatalf("create stranger workspace 2: %v", err)
	}
	if _, err := sp.Delete(ctx, stranger.ID, strangerID, ""); err != nil {
		t.Fatalf("bin the stranger's workspace: %v", err)
	}
	rec = sessionReq(t, h, token, http.MethodDelete,
		"/v1/workspaces/"+stranger.ID+"/purge", `{"confirm_slug":"`+stranger.Slug+`"}`)
	if rec.Code != http.StatusForbidden {
		t.Errorf("purging a STRANGER's trashed workspace = %d (%s), want 403 — the row is "+
			"found, so this is the owner refusal, not the not-found the miss shapes answer",
			rec.Code, rec.Body.String())
	}
	if n := countRowsREST(t, pool,
		`SELECT COUNT(*) FROM workspaces WHERE id=$1 AND deleted_at IS NOT NULL`, stranger.ID); n != 1 {
		t.Error("a stranger's refused purge destroyed/restored the row — the 403 must write nothing")
	}
}

// scopeTestKey makes a readwrite-scoped Bearer key for accountID.
func scopeTestKey(t *testing.T, pool *pgxpool.Pool, accountID, name string) string {
	t.Helper()
	keys := dbstore.NewAPIKeyStore(pool)
	raw := "jf_rest_trash_" + name + "_" + accountID[:8] + "0000"
	if _, err := keys.Create(context.Background(), accountID, auth.HashAPIKey(raw), name, []string{dbstore.ScopeReadWrite}); err != nil {
		t.Fatalf("create key %s: %v", name, err)
	}
	return raw
}
