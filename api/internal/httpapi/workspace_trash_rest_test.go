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
		"rest-trash-"+tag+"-"+t.Name()[:6]+"@example.test",
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

	// No confirmation → refused, row intact.
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
	var n int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM workspaces WHERE id=$1`, first.ID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatal("an unconfirmed workspace delete removed the row")
	}

	// The pre-flight read agrees it is deletable, and names the last-workspace rule.
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

	// With two workspaces and the confirmation → deleted.
	rec = sessionReq(t, h, token, http.MethodDelete, "/v1/workspaces/"+first.ID,
		`{"confirm_slug":"`+first.Slug+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("confirmed DELETE /v1/workspaces/{id} = %d (%s)", rec.Code, rec.Body.String())
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM workspaces WHERE id=$1`, first.ID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatal("confirmed delete returned 200 but the workspace row is still there")
	}

	// The LAST workspace is refused, and the pre-flight says so.
	rec = sessionReq(t, h, token, http.MethodDelete, "/v1/workspaces/"+second.ID,
		`{"confirm_slug":"`+second.Slug+`"}`)
	if rec.Code == http.StatusOK {
		t.Error("the LAST workspace was deleted — the \"at least one workspace\" rule is not enforced on REST")
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

func mkWsUser(t *testing.T, pool *pgxpool.Pool, tag string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO users (email, password_hash) VALUES ($1,'it') RETURNING id`,
		tag+"-"+t.Name()+"@example.test").Scan(&id); err != nil {
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

// TestRESTWorkspaceDeleteRefusalCarriesItsCode is the third converted site: the
// DELETE /v1/workspaces/{id} 409 for a workspace that still owns content. It is a
// writeJSON literal map (not writeErr) because it also carries `blockers`, so the
// `code` had to be added alongside them — and both must survive together, or the
// UI either loses the reason or loses the list of obstacles.
func TestRESTWorkspaceDeleteRefusalCarriesItsCode(t *testing.T) {
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
	keep, err := sp.Create(ctx, userID, "REST WS Keep", "rest-ws-k-"+userID[:6])
	if err != nil {
		t.Fatalf("create keep workspace: %v", err)
	}
	target, err := sp.Create(ctx, userID, "REST WS Full", "rest-ws-f-"+userID[:6])
	if err != nil {
		t.Fatalf("create target workspace: %v", err)
	}
	// A live form in the target is a blocker (`forms` counts trashed ones too).
	var targetAccount string
	if err := pool.QueryRow(ctx, `SELECT id FROM accounts WHERE workspace_id=$1`, target.ID).Scan(&targetAccount); err != nil {
		t.Fatalf("read target account: %v", err)
	}
	fs := dbstore.NewFormStore(pool)
	if err := fs.Create(ctx, &dbstore.Form{
		AccountID: targetAccount, Slug: "rest-ws-block-" + userID[:6], Title: "Blocker", Status: dbstore.FormStatusDraft,
	}); err != nil {
		t.Fatalf("create blocking form: %v", err)
	}
	token, err := sess.Create(ctx, userID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	_ = keep

	rec := sessionReq(t, h, token, http.MethodDelete, "/v1/workspaces/"+target.ID,
		`{"confirm_slug":"`+target.Slug+`"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("DELETE a non-empty workspace = %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("workspace refusal is not JSON: %v (%s)", err, rec.Body.String())
	}
	if got := body["code"]; got != CodeWorkspaceNotEmpty {
		t.Errorf("workspace refusal code = %v, want %q — without it the dashboard "+
			"prints \"we couldn't delete this workspace\" and never says why", got, CodeWorkspaceNotEmpty)
	}
	// The blockers list is what the page renders beside the sentence; `code` was
	// added NEXT TO it.
	blockers, ok := body["blockers"].([]any)
	if !ok || len(blockers) == 0 {
		t.Errorf("workspace refusal blockers = %v, want the non-empty obstacle list "+
			"(the code was added alongside it, not instead of it)", body["blockers"])
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM workspaces WHERE id=$1`, target.ID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Error("the refused workspace delete removed the row anyway")
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
