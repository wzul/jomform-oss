package httpapi

// Integration tests for workspace RBAC role enforcement (issue #162): a member's
// session resolves to the workspace's account, read_only members get 403 on write
// endpoints, manage members can write products/forms/sales/webhooks/settings
// but not members/CHIP, and admin can do everything. Skipped unless
// JOMFORM_TEST_DATABASE_URL is set.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// rbacFixture seeds an owner + workspace + a member with the given role, and
// returns the workspace id, the workspace slug, the member's user id, and the workspace's
// account id.
func rbacFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, role string) (workspaceID, workspaceSlug, memberUserID, accountID string) {
	t.Helper()
	sp := dbstore.NewWorkspaceStore(pool)
	sm := dbstore.NewWorkspaceMemberStore(pool)

	owner := rbacUser(t, ctx, pool)
	workspace, err := sp.Create(ctx, owner, "RBAC Workspace", "rbac-"+randSuffix())
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM accounts WHERE workspace_id=$1`, workspace.ID).Scan(&accountID); err != nil {
		t.Fatalf("get workspace account: %v", err)
	}
	member := rbacUser(t, ctx, pool)
	if err := sm.AddMember(ctx, workspace.ID, member, role); err != nil {
		t.Fatalf("add member: %v", err)
	}
	return workspace.ID, workspace.Slug, member, accountID
}

func rbacUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ('rbac-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&id); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return id
}

// rbacHandlers wires a RESTHandlers over the test pool with WorkspaceMembers and
// Sessions wired (the session path is what carries the member's role).
func rbacHandlers(t *testing.T, pool *pgxpool.Pool) *RESTHandlers {
	t.Helper()
	h := newBundleHandlers(pool)
	h.WorkspaceMembers = dbstore.NewWorkspaceMemberStore(pool)
	h.Sessions = authTestValkey(t)
	return h
}

// rbacSessionCookie issues a session cookie token for the given user id so the
// request resolves as that user (session path, not API key).
func rbacSessionCookie(t *testing.T, h *RESTHandlers, userID string) string {
	t.Helper()
	tok, err := h.Sessions.CreateRememberedWithTTL(context.Background(), userID, rememberLongTTL, true)
	if err != nil {
		t.Fatalf("create session token: %v", err)
	}
	return tok
}

func rbacDo(t *testing.T, h *RESTHandlers, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// TestRBACReadOnlyMemberBlockedFromWrites: a read_only member can read but
// every write endpoint returns 403.
func TestRBACReadOnlyMemberBlockedFromWrites(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	workspaceID, workspaceSlug, member, _ := rbacFixture(t, ctx, pool, dbstore.RoleReadOnly)
	h := rbacHandlers(t, pool)
	tok := rbacSessionCookie(t, h, member)

	// Read endpoints succeed (workspace-scoped).
	if rec := rbacDo(t, h, "GET", "/v1/rest/products?workspace="+workspaceSlug, tok, nil); rec.Code != http.StatusOK {
		t.Fatalf("read_only GET products = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if rec := rbacDo(t, h, "GET", "/v1/rest/sales?workspace="+workspaceSlug, tok, nil); rec.Code != http.StatusOK {
		t.Fatalf("read_only GET sales = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	// Write endpoints return 403.
	writes := []struct {
		method, path string
		body         any
	}{
		{"POST", "/v1/rest/products?workspace=" + workspaceSlug, map[string]any{"name": "Tee", "price_sen": 1000}},
		{"POST", "/v1/rest/forms?workspace=" + workspaceSlug, map[string]any{"slug": "f", "title": "F"}},
		{"POST", "/v1/rest/webhooks?workspace=" + workspaceSlug, map[string]any{"url": "https://x.example/h", "secret": "12345678"}},
		{"PUT", "/v1/rest/settings/chip?workspace=" + workspaceSlug, map[string]any{"brand_id": "b", "secret_key": "sk_test_x"}},
		{"POST", "/v1/workspaces/" + workspaceID + "/invites?workspace=" + workspaceSlug, map[string]any{"email": "x@example.test"}},
	}
	for _, w := range writes {
		if rec := rbacDo(t, h, w.method, w.path, tok, w.body); rec.Code != http.StatusForbidden {
			t.Errorf("%s %s = %d, want 403 (body %s)", w.method, w.path, rec.Code, rec.Body.String())
		}
	}
}

// TestRBACManageMemberCanWriteButNotAdmin: a manage member can write
// products/forms/sales/webhooks/settings but not members or CHIP creds.
func TestRBACManageMemberCanWriteButNotAdmin(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	workspaceID, workspaceSlug, member, _ := rbacFixture(t, ctx, pool, dbstore.RoleManage)
	h := rbacHandlers(t, pool)
	tok := rbacSessionCookie(t, h, member)

	// Manage can create a product.
	if rec := rbacDo(t, h, "POST", "/v1/rest/products?workspace="+workspaceSlug, tok, map[string]any{"name": "Tee", "price_sen": 1000}); rec.Code != http.StatusCreated {
		t.Fatalf("manage POST products = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	// Manage can create a form.
	if rec := rbacDo(t, h, "POST", "/v1/rest/forms?workspace="+workspaceSlug, tok, map[string]any{"slug": "f", "title": "F"}); rec.Code != http.StatusCreated {
		t.Fatalf("manage POST forms = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	// Manage can write settings (einvoice).
	if rec := rbacDo(t, h, "PUT", "/v1/rest/settings/einvoice?workspace="+workspaceSlug, tok, map[string]any{"tin": "T", "msic": "M"}); rec.Code != http.StatusOK {
		t.Fatalf("manage PUT einvoice = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	// Manage CANNOT manage members (admin-only).
	if rec := rbacDo(t, h, "POST", "/v1/workspaces/"+workspaceID+"/invites?workspace="+workspaceSlug, tok, map[string]any{"email": "x@example.test"}); rec.Code != http.StatusForbidden {
		t.Fatalf("manage POST invites = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if rec := rbacDo(t, h, "GET", "/v1/workspaces/"+workspaceID+"/members?workspace="+workspaceSlug, tok, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("manage GET members = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	// Manage CANNOT set CHIP creds (admin-only).
	if rec := rbacDo(t, h, "PUT", "/v1/rest/settings/chip?workspace="+workspaceSlug, tok, map[string]any{"brand_id": "b", "secret_key": "sk_test_x"}); rec.Code != http.StatusForbidden {
		t.Fatalf("manage PUT chip = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestRBACAdminCanDoEverything: an admin member can write products AND manage
// members AND set CHIP creds.
func TestRBACAdminCanDoEverything(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	workspaceID, workspaceSlug, member, _ := rbacFixture(t, ctx, pool, dbstore.RoleAdmin)
	h := rbacHandlers(t, pool)
	tok := rbacSessionCookie(t, h, member)

	if rec := rbacDo(t, h, "POST", "/v1/rest/products?workspace="+workspaceSlug, tok, map[string]any{"name": "Tee", "price_sen": 1000}); rec.Code != http.StatusCreated {
		t.Fatalf("admin POST products = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if rec := rbacDo(t, h, "POST", "/v1/workspaces/"+workspaceID+"/invites?workspace="+workspaceSlug, tok, map[string]any{"email": "x@example.test"}); rec.Code != http.StatusCreated {
		t.Fatalf("admin POST invites = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if rec := rbacDo(t, h, "GET", "/v1/workspaces/"+workspaceID+"/members?workspace="+workspaceSlug, tok, nil); rec.Code != http.StatusOK {
		t.Fatalf("admin GET members = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	// CHIP creds: ChipProbe is nil in the test fixture, so validation is
	// skipped and the save proceeds (admin allowed).
	if rec := rbacDo(t, h, "PUT", "/v1/rest/settings/chip?workspace="+workspaceSlug, tok, map[string]any{"brand_id": "b", "secret_key": "sk_test_x"}); rec.Code != http.StatusOK {
		t.Fatalf("admin PUT chip = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestRBACMemberSeesOnlyOwnWorkspace: a member's workspace-scoped read only returns
// their workspace's data, and a foreign workspace slug is rejected (no cross-workspace
// leakage, issue #158).
func TestRBACMemberSeesOnlyOwnWorkspace(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	_, workspaceSlug, member, accountID := rbacFixture(t, ctx, pool, dbstore.RoleReadOnly)
	h := rbacHandlers(t, pool)
	tok := rbacSessionCookie(t, h, member)

	// Seed a product in the member's workspace.
	products := dbstore.NewProductStore(pool)
	if err := products.Create(ctx, &dbstore.Product{AccountID: accountID, Name: "Member Tee", PriceSen: 1000, Type: "one_time"}); err != nil {
		t.Fatalf("create product: %v", err)
	}

	// The member sees their workspace's product.
	rec := rbacDo(t, h, "GET", "/v1/rest/products?workspace="+workspaceSlug, tok, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET products = %d, want 200", rec.Code)
	}
	var prods []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &prods); err != nil {
		t.Fatalf("decode products: %v", err)
	}
	if len(prods) != 1 || prods[0]["name"] != "Member Tee" {
		t.Fatalf("products = %v, want exactly the member's product", prods)
	}

	// A foreign workspace slug is rejected with 403 (no leakage).
	if rec := rbacDo(t, h, "GET", "/v1/rest/products?workspace=no-such-workspace", tok, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("foreign workspace GET products = %d, want 403", rec.Code)
	}
}
