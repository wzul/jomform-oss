package mcpsrv

// Issue #645 — the API-key write-SCOPE enforcement test.
//
// THIS FILE PROVES ENFORCEMENT ON THE SERVER SIDE, over the real MCP transport
// with real API keys. It is deliberately NOT a "the tool is not listed" test:
// the card calls that out as non-evidence, because a tool can be listed and
// still refuse (or be hidden and still callable). Every call below goes through
// HTTPHandler → authenticate → the tool handler, exactly as a configured MCP
// client does, and the assertion is on the returned call result.
//
// The bug this pins (issue #645): a `readonly` API key could call every write
// tool. The card's reproduction, which these tests re-create:
//
//	readonly  delete_form      -> {"status":"deleted"}   forms.deleted_at set
//	readonly  archive_product  -> {"status":"archived"}  products.active=false
//	readonly  cancel_subscription -> {"status":"cancelled"}
//	readonly  update_product   -> products.name = "HIJACKED BY READONLY KEY"
//	STEP 1 readonly -> create_api_key(scopes=["readwrite"]) -> OK
//	STEP 2 new key  -> delete_form -> OK
//
// Covered here:
//   - every write class is refused for a readonly key (form, product, api key)
//   - the escalation is closed: a readonly key cannot mint a readwrite key
//   - a readonly key CAN still read (positive control — the fix must not be
//     "everything fails")
//   - a readwrite key still writes (no regression)
//   - an `admin`-scoped key still writes (prod legacy scope keeps working)
//
// Skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// mcpCall runs one tools/call over the real streamable-HTTP transport with the
// given raw API key as the Bearer credential, and returns the result.
//
// A transport-level failure is fatal (it means the credential was rejected at
// auth, which is a different assertion); the interesting answer is whether the
// TOOL refused, so the error flag/content is returned for the caller to judge.
func mcpCall(t *testing.T, httpSrvURL, rawKey, tool string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "scope-test", Version: "test"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint: httpSrvURL + "/mcp",
		HTTPClient: &http.Client{Transport: &bearerRoundTripper{
			base: http.DefaultTransport, token: rawKey,
		}},
		DisableStandaloneSSE: true,
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	session, err := client.Connect(callCtx, transport, nil)
	if err != nil {
		t.Fatalf("MCP connect over HTTP with key %q: %v", redactKey(rawKey), err)
	}
	defer session.Close()
	res, err := session.CallTool(callCtx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("tools/call %s (key %s): transport error: %v", tool, redactKey(rawKey), err)
	}
	return res
}

// redactKey keeps a key out of test logs while still distinguishing the three
// fixtures in a failure message.
func redactKey(raw string) string {
	if len(raw) <= 16 {
		return "…"
	}
	return raw[:11] + "…"
}

// refused reports whether a tools/call came back as an error, and the message.
// "Refused" is the ONLY acceptable outcome for a readonly key on a write.
func refused(res *mcp.CallToolResult) (bool, string) {
	if res == nil {
		return true, "(no result)"
	}
	if !res.IsError {
		return false, ""
	}
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return true, sb.String()
}

// succeeded reports whether a tools/call returned a non-error result, and the
// decoded payload.
func succeeded(t *testing.T, res *mcp.CallToolResult) map[string]any {
	t.Helper()
	if res.IsError {
		t.Fatalf("call was refused but should have succeeded: %s", toolResultText(t, res))
	}
	out := map[string]any{}
	if txt := toolResultText(t, res); txt != "" {
		_ = json.Unmarshal([]byte(txt), &out)
	}
	return out
}

// TestReadonlyKeyIsRefusedEveryWriteAndStillReads is the card's acceptance
// criterion, driven over the real transport.
func TestReadonlyKeyIsRefusedEveryWriteAndStillReads(t *testing.T) {
	requireTestDB(t)
	ctx := context.Background()
	pool := testdb.Open(t)
	accountID, _ := scopeTestAccount(t, pool, "readonly")
	formID, productID := scopeTestFormAndProduct(t, pool, accountID)

	keys := dbstore.NewAPIKeyStore(pool)
	readonlyKey := scopeTestKey(t, ctx, keys, accountID, "readonly-key", []string{dbstore.ScopeReadonly})

	srv := New(&Deps{
		Forms: formsFor(pool), Accounts: dbstore.NewAccountStore(pool), Keys: keys,
		Products: dbstore.NewProductStore(pool),
		Sales:    dbstore.NewSaleStore(pool), Subs: dbstore.NewSubscriptionStore(pool),
		Webhooks:          dbstore.NewWebhookStore(pool),
		Workspaces:        dbstore.NewWorkspaceStore(pool),
		WorkspaceMembers:  dbstore.NewWorkspaceMemberStore(pool),
		ProductVariations: dbstore.NewProductVariationStore(pool),
	}, nil, nil, nil)
	httpSrv := httptest.NewServer(srv.HTTPHandler())
	defer httpSrv.Close()

	// ── Positive control FIRST: a readonly key must still be able to READ. ──
	// Without this the whole test could be satisfied by a key that cannot do
	// anything at all.
	read := mcpCall(t, httpSrv.URL, readonlyKey, "list_forms", map[string]any{})
	succeeded(t, read)

	// ── Every write class the card lists must be REFUSED. ──
	// One representative per class, named after the card's own reproduction.
	for _, tc := range []struct {
		tool string
		args map[string]any
		what string
	}{
		{"delete_form", map[string]any{"form_id": formID}, "delete_form (card: deleted a form)"},
		{"archive_product", map[string]any{"product_id": productID}, "archive_product (card: set active=false)"},
		{"update_product", map[string]any{"product_id": productID, "name": "HIJACKED BY READONLY KEY"}, "update_product (card: renamed a product)"},
		{"upsert_form", map[string]any{"slug": "scope-readonly-should-not-exist", "title": "readonly should not create this"}, "upsert_form (form creation)"},
		{"create_product", map[string]any{"name": "readonly should not create this", "price_sen": 100, "type": "one_time"}, "create_product"},
		{"create_webhook", map[string]any{"url": "https://example.test/hook", "secret": "shhh", "events": []string{"sale.paid"}}, "create_webhook"},
		{"revoke_api_key", map[string]any{"id": "00000000-0000-0000-0000-000000000000"}, "revoke_api_key"},
		{"set_workspace_settings", map[string]any{"workspace_id": "00000000-0000-0000-0000-000000000000"}, "set_workspace_settings"},
		{"set_system_setting", map[string]any{"key": "site_name", "value": "pwned"}, "set_system_setting"},
		{"update_profile_phone", map[string]any{"phone": "+60123456789"}, "update_profile_phone"},
	} {
		res := mcpCall(t, httpSrv.URL, readonlyKey, tc.tool, tc.args)
		ok, msg := refused(res)
		if !ok {
			t.Errorf("issue #645 REGRESSION: a readonly API key was ALLOWED to call %s — "+
				"the scope gate is not enforced on this tool. Result: %s", tc.what, toolResultText(t, res))
			continue
		}
		if !strings.Contains(msg, "scope") {
			t.Errorf("%s was refused, but not for the scope reason (want a scope error, got %q) — "+
				"a refusal for the wrong reason would leave the hole open on a key that does not "+
				"trip this one", tc.what, msg)
		}
	}

	// ── The DB must be untouched: the refusal has to happen BEFORE the write. ──
	var deleted bool
	if err := pool.QueryRow(ctx, `SELECT deleted_at IS NOT NULL FROM forms WHERE id=$1`, formID).Scan(&deleted); err != nil {
		t.Fatalf("read form back: %v", err)
	}
	if deleted {
		t.Error("the form was soft-deleted by a readonly key — the gate ran after the write, not before it")
	}
	var name string
	if err := pool.QueryRow(ctx, `SELECT name FROM products WHERE id=$1`, productID).Scan(&name); err != nil {
		t.Fatalf("read product back: %v", err)
	}
	if name == "HIJACKED BY READONLY KEY" {
		t.Error("a readonly key renamed a product — the card's exact reproduction still succeeds")
	}
}

// TestReadonlyKeyCannotEscalateToReadwrite closes the two-call escalation from
// the card. Without this, the fix above is defeatable: mint a readwrite key with
// the readonly key and write with that.
func TestReadonlyKeyCannotEscalateToReadwrite(t *testing.T) {
	requireTestDB(t)
	ctx := context.Background()
	pool := testdb.Open(t)
	accountID, _ := scopeTestAccount(t, pool, "escalate")
	keys := dbstore.NewAPIKeyStore(pool)
	readonlyKey := scopeTestKey(t, ctx, keys, accountID, "escalate-readonly", []string{dbstore.ScopeReadonly})

	before, err := keys.ListByAccount(ctx, accountID)
	if err != nil {
		t.Fatalf("list keys: %v", err)
	}

	srv := New(&Deps{Keys: keys, Accounts: dbstore.NewAccountStore(pool),
		Forms: formsFor(pool)}, nil, nil, nil)
	httpSrv := httptest.NewServer(srv.HTTPHandler())
	defer httpSrv.Close()

	res := mcpCall(t, httpSrv.URL, readonlyKey, "create_api_key", map[string]any{
		"name": "escalated", "scopes": []string{dbstore.ScopeReadWrite},
	})
	ok, msg := refused(res)
	if !ok {
		t.Errorf("issue #645 ESCALATION: a readonly key minted a readwrite key. Result: %s",
			toolResultText(t, res))
	} else if !strings.Contains(msg, "scope") {
		t.Errorf("create_api_key was refused but not for the scope reason: %q", msg)
	}

	// And the key table must not have grown.
	after, err := keys.ListByAccount(ctx, accountID)
	if err != nil {
		t.Fatalf("list keys: %v", err)
	}
	if len(after) != len(before) {
		t.Errorf("key count went %d → %d: a readonly key created a key despite the refusal "+
			"(the gate ran after the insert)", len(before), len(after))
	}
}

// TestReadwriteKeyCanStillCreateAPIKeys is the positive control the card names
// explicitly: create_api_key must be gated on the CALLER'S SCOPES, not on the
// role, so a normal readwrite key must still be able to create keys.
//
// TestReadonlyKeyCannotEscalateToReadwrite proves the refusal half; on its own
// that half is satisfiable by disabling key creation for EVERYONE, which would
// break every normal key. This is the same no-regression discipline as
// TestReadwriteAndAdminKeysStillWrite, applied to the tool that is easiest to
// break that way.
func TestReadwriteKeyCanStillCreateAPIKeys(t *testing.T) {
	requireTestDB(t)
	ctx := context.Background()
	pool := testdb.Open(t)
	accountID, _ := scopeTestAccount(t, pool, "rwcreate")
	keys := dbstore.NewAPIKeyStore(pool)
	readwriteKey := scopeTestKey(t, ctx, keys, accountID, "rw-create", []string{dbstore.ScopeReadWrite})

	srv := New(&Deps{Keys: keys, Accounts: dbstore.NewAccountStore(pool),
		Forms: formsFor(pool)}, nil, nil, nil)
	httpSrv := httptest.NewServer(srv.HTTPHandler())
	defer httpSrv.Close()

	out := succeeded(t, mcpCall(t, httpSrv.URL, readwriteKey, "create_api_key", map[string]any{
		"name": "created-by-readwrite", "scopes": []string{dbstore.ScopeReadonly},
	}))
	if raw, _ := out["key"].(string); raw == "" {
		t.Fatalf("create_api_key returned no raw key for a readwrite caller (out=%v) — the gate is "+
			"on the role, or key creation is broken for everyone", out)
	}

	// The api_keys ROW is what makes it a real creation, not the handler's
	// return value.
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM api_keys WHERE account_id=$1 AND name='created-by-readwrite'`,
		accountID).Scan(&n); err != nil {
		t.Fatalf("count created keys: %v", err)
	}
	if n != 1 {
		t.Errorf("api_keys rows named created-by-readwrite = %d, want 1 — a readwrite key could not "+
			"create a key, so create_api_key is not gated on the caller's scopes", n)
	}
}

// TestReadwriteAndAdminKeysStillWrite is the no-regression half. A refusal test
// alone can be satisfied by breaking the feature ("let everything fail"), and
// prod keys carry the legacy `admin` scope, which must keep passing.
func TestReadwriteAndAdminKeysStillWrite(t *testing.T) {
	requireTestDB(t)
	ctx := context.Background()
	pool := testdb.Open(t)
	accountID, _ := scopeTestAccount(t, pool, "rwkeys")
	keys := dbstore.NewAPIKeyStore(pool)

	readwriteKey := scopeTestKey(t, ctx, keys, accountID, "rw-key", []string{dbstore.ScopeReadWrite})
	adminKey := scopeTestKey(t, ctx, keys, accountID, "admin-key", []string{dbstore.ScopeAdmin})

	srv := New(&Deps{
		Forms: formsFor(pool), Accounts: dbstore.NewAccountStore(pool), Keys: keys,
		Products: dbstore.NewProductStore(pool),
	}, nil, nil, nil)
	httpSrv := httptest.NewServer(srv.HTTPHandler())
	defer httpSrv.Close()

	for _, tc := range []struct {
		name, key string
	}{
		{"readwrite", readwriteKey},
		{"admin (prod legacy scope)", adminKey},
	} {
		// A fresh form + product per key: the previous iteration's write is
		// supposed to have landed, so reusing it would test the second key
		// against already-mutated rows.
		formID, productID := scopeTestFormAndProduct(t, pool, accountID)

		// archive → the row must flip, so the write really happened.
		out := succeeded(t, mcpCall(t, httpSrv.URL, tc.key, "archive_product",
			map[string]any{"product_id": productID}))
		if out["status"] != "archived" {
			t.Errorf("%s key: archive_product returned %v, want status=archived", tc.name, out)
		}
		var active bool
		if err := pool.QueryRow(ctx, `SELECT active FROM products WHERE id=$1`, productID).Scan(&active); err != nil {
			t.Fatalf("read product back: %v", err)
		}
		if active {
			t.Errorf("%s key: products.active is still true after archive_product", tc.name)
		}

		// And a form write, so the gate is exercised on a second write class.
		succeeded(t, mcpCall(t, httpSrv.URL, tc.key, "delete_form", map[string]any{"form_id": formID}))
		var deleted bool
		if err := pool.QueryRow(ctx, `SELECT deleted_at IS NOT NULL FROM forms WHERE id=$1`, formID).Scan(&deleted); err != nil {
			t.Fatalf("read form back: %v", err)
		}
		if !deleted {
			t.Errorf("%s key: the form was NOT soft-deleted — the write gate is now too strict "+
				"(a fix that refuses everyone passes the refusal tests while breaking the feature)", tc.name)
		}
	}
}

// ── fixtures ─────────────────────────────────────────────────────────────────

func requireTestDB(t *testing.T) {
	t.Helper()
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping API-key scope enforcement test")
	}
}

func formsFor(pool *pgxpool.Pool) *dbstore.FormStore {
	return dbstore.NewFormStore(pool)
}

// scopeTestAccount creates an isolated user + account and returns their ids.
func scopeTestAccount(t *testing.T, pool *pgxpool.Pool, tag string) (accountID, userID string) {
	t.Helper()
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO users (email, password_hash) VALUES ('scope-`+tag+`-' || md5(random()::text) || '@example.test', 'it') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO accounts (owner_user_id, name, slug)
		 SELECT $1, 'scope-`+tag+`-' || md5(random()::text), 'scope-`+tag+`-' || md5(random()::text) RETURNING id`,
		userID,
	).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	return accountID, userID
}

// scopeTestFormAndProduct creates a draft form and an active product.
func scopeTestFormAndProduct(t *testing.T, pool *pgxpool.Pool, accountID string) (formID, productID string) {
	t.Helper()
	ctx := context.Background()
	f := &dbstore.Form{
		AccountID: accountID,
		Slug:      "scope-" + accountID[:8],
		Title:     "Scope Gate Sample",
		Config:    map[string]any{"mode": "payment", "blocks": []any{}},
		Theme:     map[string]any{},
		Status:    dbstore.FormStatusDraft,
	}
	if err := dbstore.NewFormStore(pool).Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}
	p := &dbstore.Product{AccountID: accountID, Name: "Scope Product", PriceSen: 1000, Type: "one_time"}
	if err := dbstore.NewProductStore(pool).Create(ctx, p); err != nil {
		t.Fatalf("create product: %v", err)
	}
	return f.ID, p.ID
}

// scopeTestKey mints a real api_keys row for a raw key value.
func scopeTestKey(t *testing.T, ctx context.Context, keys *dbstore.APIKeyStore, accountID, name string, scopes []string) string {
	t.Helper()
	raw := "jf_scope_" + name + "_" + accountID[:8] + "0000000000"
	if _, err := keys.Create(ctx, accountID, auth.HashAPIKey(raw), name, scopes); err != nil {
		t.Fatalf("create api key %s: %v", name, err)
	}
	return raw
}
