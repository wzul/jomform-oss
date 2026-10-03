// Issue #645 — REST enforcement of the API-key write SCOPE.
//
// The MCP half is covered by internal/mcpsrv/scopegate_enforcement_test.go. This
// file covers the other transport, where the defect was worse: RESTHandlers
// .resolve() read key.AccountID and NEVER read key.Scopes at all, so a readonly
// Bearer key could write every account endpoint (delete forms, archive products,
// cancel subscriptions, mint itself a readwrite key) while requireWriteRole
// checked only scope.Role.
//
// Everything below goes through the real mux (RegisterRoutes) with a real
// api_keys row and a real Bearer header, so it exercises resolve() → authed() →
// the handler exactly as prod does. The assertions are on the response status
// AND on the database row, because a 403 that still wrote is not a fix.
package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// scopeRESTFixture builds a RESTHandlers wired to a real db, plus a real API-key
// Bearer token for the given scopes. No session store: these requests are
// API-key authenticated, which is the path under test.
func scopeRESTFixture(t *testing.T, pool *pgxpool.Pool, accountID, name string, scopes []string) (*RESTHandlers, string) {
	t.Helper()
	keys := dbstore.NewAPIKeyStore(pool)
	raw := fmt.Sprintf("jf_rest_scope_%s_%s_000000", name, accountID[:8])
	if _, err := keys.Create(context.Background(), accountID, auth.HashAPIKey(raw), name, scopes); err != nil {
		t.Fatalf("create api key %s: %v", name, err)
	}
	h := &RESTHandlers{
		Accounts:          dbstore.NewAccountStore(pool),
		Products:          dbstore.NewProductStore(pool),
		ProductVariations: dbstore.NewProductVariationStore(pool),
		Forms:             dbstore.NewFormStore(pool),
		Keys:              keys,
		SystemSettings:    dbstore.NewSystemSettingStore(pool),
		Workspaces:        dbstore.NewWorkspaceStore(pool),
		WorkspaceMembers:  dbstore.NewWorkspaceMemberStore(pool),
	}
	return h, raw
}

// bearerReq issues one request with a Bearer key against the real route table.
func bearerReq(t *testing.T, h *RESTHandlers, raw, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+raw)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// TestRESTReadonlyKeyIsRefusedWrites is the REST acceptance criterion: a
// readonly Bearer key must be refused on every mutating account endpoint, with
// 403, and the row must be untouched.
func TestRESTReadonlyKeyIsRefusedWrites(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST scope test in -short mode")
	}
	ctx := context.Background()
	pool := testdb.Open(t)
	accountID, _ := scopeTestRESTAccount(t, pool, t.Name())
	formID, productID := scopeTestRESTFormAndProduct(t, pool, accountID)

	h, readonlyKey := scopeRESTFixture(t, pool, accountID, "readonly", []string{dbstore.ScopeReadonly})

	// ── The positive control FIRST: a readonly key must still be able to READ. ──
	// A run where everything is 403 would otherwise pass the refusal assertions
	// while proving nothing about scope.
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/v1/rest/forms"},
		{http.MethodGet, "/v1/rest/products"},
		{http.MethodGet, "/v1/rest/keys"},
	} {
		if rec := bearerReq(t, h, readonlyKey, tc.method, tc.path, ""); rec.Code != http.StatusOK {
			t.Errorf("a readonly key was refused on a READ: %s %s = %d (%s) — the fix must not "+
				"turn into \"everything fails\"", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}

	// ── Every mutating route reachable with a Bearer key must be refused. ──
	for _, tc := range []struct{ method, path, body, what string }{
		{http.MethodDelete, "/v1/rest/forms/" + formID, "", "DELETE /forms/{id} (the card's delete_form)"},
		{http.MethodPatch, "/v1/rest/forms/" + formID, `{"title":"hijacked"}`,
			"PATCH /forms/{id}"},
		{http.MethodPost, "/v1/rest/forms/" + formID + "/publish", "", "POST /forms/{id}/publish"},
		{http.MethodPut, "/v1/rest/products/" + productID,
			`{"name":"HIJACKED BY READONLY KEY"}`, "PUT /products/{id} (the card's update_product)"},
		{http.MethodDelete, "/v1/rest/products/" + productID, "", "DELETE /products/{id}"},
		{http.MethodPut, "/v1/system-settings", `{"key":"site_name","value":"pwned"}`,
			"PUT /system-settings (had NO gate at all)"},
		{http.MethodPost, "/v1/rest/keys", `{"name":"escalated","scopes":["readwrite"]}`,
			"POST /keys (the escalation)"},
	} {
		rec := bearerReq(t, h, readonlyKey, tc.method, tc.path, tc.body)
		if rec.Code != http.StatusForbidden {
			t.Errorf("issue #645 REST REGRESSION: a readonly key got %d on %s — want 403. Body: %s",
				rec.Code, tc.what, rec.Body.String())
			continue
		}
		if !strings.Contains(rec.Body.String(), "scope") {
			t.Errorf("%s was refused with %d but not for the scope reason: %s",
				tc.what, rec.Code, rec.Body.String())
		}
	}

	// ── The database must be untouched. ──
	var deleted bool
	if err := pool.QueryRow(ctx, `SELECT deleted_at IS NOT NULL FROM forms WHERE id=$1`, formID).Scan(&deleted); err != nil {
		t.Fatalf("read form back: %v", err)
	}
	if deleted {
		t.Error("a readonly key soft-deleted a form over REST — the gate ran after the write")
	}
	var name string
	if err := pool.QueryRow(ctx, `SELECT name FROM products WHERE id=$1`, productID).Scan(&name); err != nil {
		t.Fatalf("read product back: %v", err)
	}
	if name == "HIJACKED BY READONLY KEY" {
		t.Error("a readonly key renamed a product over REST — the card's exact reproduction still succeeds")
	}
	var setting string
	if err := pool.QueryRow(ctx,
		`SELECT COALESCE(value,'') FROM system_settings WHERE key='site_name'`).Scan(&setting); err != nil {
		// Absent is the expected state; only a real error is fatal here.
		setting = ""
	}
	if setting == "pwned" {
		t.Error("a readonly key wrote a platform system setting over REST")
	}
	keys, err := dbstore.NewAPIKeyStore(pool).ListByAccount(ctx, accountID)
	if err != nil {
		t.Fatalf("list keys: %v", err)
	}
	if len(keys) != 1 {
		t.Errorf("key count = %d, want 1 (only the readonly fixture): a readonly key minted a key over REST",
			len(keys))
	}
}

// TestRESTReadwriteKeyStillWrites is the no-regression half on the REST side: a
// normal readwrite key keeps full write access. Without it, refusing everyone
// would satisfy the test above.
func TestRESTReadwriteKeyStillWrites(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST scope test in -short mode")
	}
	ctx := context.Background()
	pool := testdb.Open(t)
	accountID, _ := scopeTestRESTAccount(t, pool, t.Name())
	formID, productID := scopeTestRESTFormAndProduct(t, pool, accountID)

	h, rwKey := scopeRESTFixture(t, pool, accountID, "readwrite", []string{dbstore.ScopeReadWrite})

	if rec := bearerReq(t, h, rwKey, http.MethodPut, "/v1/rest/products/"+productID,
		`{"name":"renamed by a readwrite key"}`); rec.Code != http.StatusOK {
		t.Fatalf("a readwrite key was refused PUT /products/{id}: %d (%s)", rec.Code, rec.Body.String())
	}
	var name string
	if err := pool.QueryRow(ctx, `SELECT name FROM products WHERE id=$1`, productID).Scan(&name); err != nil {
		t.Fatalf("read product back: %v", err)
	}
	if name != "renamed by a readwrite key" {
		t.Errorf("product name = %q, want the readwrite key's write to have landed", name)
	}

	if rec := bearerReq(t, h, rwKey, http.MethodDelete, "/v1/rest/forms/"+formID, ""); rec.Code != http.StatusOK {
		t.Fatalf("a readwrite key was refused DELETE /forms/{id}: %d (%s)", rec.Code, rec.Body.String())
	}
	var deleted bool
	if err := pool.QueryRow(ctx, `SELECT deleted_at IS NOT NULL FROM forms WHERE id=$1`, formID).Scan(&deleted); err != nil {
		t.Fatalf("read form back: %v", err)
	}
	if !deleted {
		t.Error("a readwrite key's DELETE /forms/{id} did not soft-delete the form — the scope gate " +
			"is now too strict and would break normal keys")
	}
}

// TestRESTAdminScopeKeyStillWrites pins the prod-legacy `admin` scope as a
// superset, so existing keys do not start failing (dbstore.ScopeHas treats
// "admin" as satisfying every scope).
func TestRESTAdminScopeKeyStillWrites(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST scope test in -short mode")
	}
	pool := testdb.Open(t)
	accountID, _ := scopeTestRESTAccount(t, pool, t.Name())
	formID, _ := scopeTestRESTFormAndProduct(t, pool, accountID)

	h, adminKey := scopeRESTFixture(t, pool, accountID, "admin", []string{dbstore.ScopeAdmin})
	if rec := bearerReq(t, h, adminKey, http.MethodDelete, "/v1/rest/forms/"+formID, ""); rec.Code != http.StatusOK {
		t.Fatalf("an admin-scoped key (prod legacy) was refused a write: %d (%s)", rec.Code, rec.Body.String())
	}
}

// ── fixtures ─────────────────────────────────────────────────────────────────

func scopeTestRESTAccount(t *testing.T, pool *pgxpool.Pool, tag string) (accountID, userID string) {
	t.Helper()
	slug := strings.ToLower(strings.NewReplacer("_", "-", "/", "-").Replace(tag))
	if len(slug) > 40 {
		slug = slug[:40]
	}
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO users (email, password_hash) VALUES ($1, 'it') RETURNING id`,
		"rest-scope-"+slug+"-"+runKey()+"@example.test",
	).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO accounts (owner_user_id, name, slug)
		 VALUES ($1, $2, $3) RETURNING id`,
		userID, "rest-scope-"+slug, "rest-scope-"+slug+"-"+runKey(),
	).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	return accountID, userID
}

func scopeTestRESTFormAndProduct(t *testing.T, pool *pgxpool.Pool, accountID string) (formID, productID string) {
	t.Helper()
	ctx := context.Background()
	f := &dbstore.Form{
		AccountID: accountID,
		Slug:      "rest-scope-" + accountID[:8],
		Title:     "REST Scope Sample",
		Config:    map[string]any{"mode": "payment", "blocks": []any{}},
		Theme:     map[string]any{},
		Status:    dbstore.FormStatusDraft,
	}
	if err := dbstore.NewFormStore(pool).Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}
	p := &dbstore.Product{AccountID: accountID, Name: "REST Scope Product", PriceSen: 1000, Type: "one_time"}
	if err := dbstore.NewProductStore(pool).Create(ctx, p); err != nil {
		t.Fatalf("create product: %v", err)
	}
	return f.ID, p.ID
}

// TestRESTWebhookKeyIsGatedAsAWrite is card t_e9562a9c's behavioural half: the
// published contract calls GET /v1/rest/settings/webhook-key a `write` (the page
// renders that as "readwrite key; manage/admin role inside a ?workspace= scope"),
// and this drives the real mux with a real readonly Bearer key to prove the API
// answers exactly that — 403 before any keypair exists, because the handler
// GENERATES and SAVES one.
//
// Why it exists next to the parity test: TestOpenAPIAccessMatchesTheHandlerGate
// reads the handler's SOURCE. A source check cannot tell whether the gate it
// finds is on the path a caller takes, so this asserts the wire, and it pins the
// fact the contract's description now states rather than leaving it as prose.
func TestRESTWebhookKeyIsGatedAsAWrite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST scope test in -short mode")
	}
	ctx := context.Background()
	pool := testdb.Open(t)
	accountID, _ := scopeTestRESTAccount(t, pool, t.Name())

	// PRECONDITION, asserted rather than assumed: a fresh workspace has no
	// keypair, which is the state the 403 is about.
	var keyless int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM accounts WHERE id=$1 AND COALESCE(wh_sign_pub,'')='' AND COALESCE(wh_sign_priv,'')=''`,
		accountID).Scan(&keyless); err != nil {
		t.Fatalf("precondition query: %v", err)
	}
	if keyless != 1 {
		t.Fatalf("precondition: the fixture account already has a signing key (%d keyless rows, want 1) — "+
			"this test would then be asserting the wrong state", keyless)
	}

	h, readonlyKey := scopeRESTFixture(t, pool, accountID, "readonly-webhookkey", []string{dbstore.ScopeReadonly})

	rec := bearerReq(t, h, readonlyKey, http.MethodGet, "/v1/rest/settings/webhook-key", "")
	if rec.Code != http.StatusForbidden {
		t.Errorf("a readonly key got %d on GET /v1/rest/settings/webhook-key for an account with no "+
			"signing key; the contract says this route is gated as a write, so want 403. Body: %s",
			rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "scope") {
		t.Errorf("refused with %d but not for the scope reason: %s", rec.Code, rec.Body.String())
	}
	// A refused call must not have minted the key anyway — a 403 that still
	// wrote is not the behaviour the contract describes.
	var after int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM accounts WHERE id=$1 AND COALESCE(wh_sign_pub,'')='' AND COALESCE(wh_sign_priv,'')=''`,
		accountID).Scan(&after); err != nil {
		t.Fatalf("read account back: %v", err)
	}
	if after != 1 {
		t.Error("the refused GET created the account's signing keypair anyway — the gate ran after the write")
	}

	// The control that keeps this honest: a readwrite key on the SAME account is
	// served, so the 403 above is a scope refusal and not a broken route.
	h2, readwriteKey := scopeRESTFixture(t, pool, accountID, "readwrite-webhookkey", []string{dbstore.ScopeReadWrite})
	rec2 := bearerReq(t, h2, readwriteKey, http.MethodGet, "/v1/rest/settings/webhook-key", "")
	if rec2.Code != http.StatusOK {
		t.Fatalf("a readwrite key was refused GET /v1/rest/settings/webhook-key: %d (%s)",
			rec2.Code, rec2.Body.String())
	}
	if !strings.Contains(rec2.Body.String(), `"alg":"ed25519"`) {
		t.Errorf("the 200 body is not the signing-key payload: %s", rec2.Body.String())
	}

	// And the state-dependent half, documented in the contract's own description:
	// once a keypair exists, the SAME readonly key is served.
	pub, priv, err := dbstore.NewAccountStore(pool).SigningKeys(ctx, accountID)
	if err != nil {
		t.Fatalf("read signing keys: %v", err)
	}
	if pub == "" || priv == "" {
		t.Fatalf("the readwrite call above did not persist a keypair (pub=%q) — the second half of "+
			"this test would assert nothing", pub)
	}
	if rec3 := bearerReq(t, h, readonlyKey, http.MethodGet, "/v1/rest/settings/webhook-key", ""); rec3.Code != http.StatusOK {
		t.Errorf("with a keypair already present the readonly key got %d; the contract's description "+
			"says it is served in that state (%s)", rec3.Code, rec3.Body.String())
	}
}
