package mcpsrv

// purge_product's confirmation over the REAL MCP transport, for a product whose
// STORED name carries leading/trailing whitespace (card t_1ee11d1f).
//
// THE DEFECT. purgeProductMCP compared `strings.TrimSpace(in.ConfirmName)` with
// the RAW `p.Name`, so a padded stored name had NO satisfying input: TrimSpace
// never returns a string with a leading or trailing space. A product name is
// stored verbatim on every write path, so `{"name":"Kopi "}` is ordinary data —
// and the row was then un-purgeable from every surface (the dashboard's mirror of
// the same rule at products/page.tsx could not enable its button either). The fix
// is one shared normalisation, textutil.SameTypedValue, applied on BOTH sides and
// by BOTH surfaces (REST + MCP) so the two cannot drift apart again.
//
// WHAT THIS FILE PINS, over HTTPHandler → authenticate → the registered tool,
// with a real readwrite key — the surface an owner's MCP client actually uses:
//
//  1. the exact stored bytes purge a padded row (before the fix: status=refused);
//  2. the name a MERCHANT can see and type purges it too, because the padding is
//     invisible in the dialog and in the refusal prose, so "only the stored bytes
//     work" would still be a trap;
//  3. a made-up value is still REFUSED with the row left in the bin — trimming
//     both sides must not turn the gate into a formality.
//
// Skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// purgeNameFixture seeds one user + workspace + account with an API key on it,
// wires the MCP server over the shared test pool, and returns the account, the
// raw key and its httptest server.
//
// The key lives on the SAME account the products belong to: purge_product
// resolves the owning account from the caller's scope, so a key on another
// account would answer "not found" and the test would prove nothing.
func purgeNameFixture(t *testing.T, tag string) (accountID, rawKey string, httpSrv *httptest.Server) {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Open(t)

	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('purge-name-`+tag+`-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	var wsID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'purge-name-ws-' || md5(random()::text), 'purge-name-' || md5(random()::text))
		 RETURNING id`, userID).Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 VALUES ($1, 'purge-name-acc-' || md5(random()::text), 'purge-name-' || md5(random()::text), $2)
		 RETURNING id`, userID, wsID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	rawKey = "jf_purgename_" + tag + "_" + accountID[:8] + "0000000000"
	if _, err := dbstore.NewAPIKeyStore(pool).Create(ctx, accountID, auth.HashAPIKey(rawKey),
		"purge-name-"+tag, []string{dbstore.ScopeReadWrite}); err != nil {
		t.Fatalf("create api key: %v", err)
	}

	srv := New(&Deps{
		Accounts:         dbstore.NewAccountStore(pool),
		Keys:             dbstore.NewAPIKeyStore(pool),
		Users:            dbstore.NewUserStore(pool),
		Products:         dbstore.NewProductStore(pool),
		Workspaces:       dbstore.NewWorkspaceStore(pool),
		WorkspaceMembers: dbstore.NewWorkspaceMemberStore(pool),
	}, nil, nil, nil)
	httpSrv = httptest.NewServer(srv.HTTPHandler())
	t.Cleanup(httpSrv.Close)
	return accountID, rawKey, httpSrv
}

// TestMCPProductPurgeAcceptsAPaddedStoredName is the regression guard.
func TestMCPProductPurgeAcceptsAPaddedStoredName(t *testing.T) {
	requireTestDB(t)
	ctx := context.Background()
	pool := testdb.Open(t)
	accountID, rawKey, httpSrv := purgeNameFixture(t, "padded")
	ps := dbstore.NewProductStore(pool)

	seed := func(name string) string {
		p := &dbstore.Product{AccountID: accountID, Name: name, PriceSen: 1100,
			Currency: "MYR", Type: dbstore.SaleTypeOneTime}
		if err := ps.Create(ctx, p); err != nil {
			t.Fatalf("create product %q: %v", name, err)
		}
		if err := ps.SoftDelete(ctx, accountID, p.ID); err != nil {
			t.Fatalf("trash product %q: %v", name, err)
		}
		return p.ID
	}

	// 1. The EXACT stored bytes. Before this card: status=refused.
	const padded = "Kopi Padded "
	first := seed(padded)
	out := mcpResult(t, httpSrv.URL, rawKey, "purge_product",
		map[string]any{"product_id": first, "confirm_name": padded})
	if out["status"] != "purged" {
		t.Fatalf("purge_product with the EXACT stored name %q returned %v, want status=purged — "+
			"the confirmation is unreachable for a padded row over MCP too", padded, out)
	}
	if _, err := ps.GetTrashed(ctx, accountID, first); err == nil {
		t.Error("purge_product reported purged but the row is still in the bin")
	}

	// 2. The visible name, which is what the dashboard's field asks for (its
	//    own guard must use the same rule — see web/src/lib/typedConfirm.ts).
	second := seed(" Kopi Padded Kedua ")
	out = mcpResult(t, httpSrv.URL, rawKey, "purge_product",
		map[string]any{"product_id": second, "confirm_name": "Kopi Padded Kedua"})
	if out["status"] != "purged" {
		t.Fatalf("purge_product with the VISIBLE name of a padded row returned %v, want "+
			"status=purged — the padding is invisible to the caller, so the stored bytes "+
			"cannot be the only accepted value", out)
	}
	if _, err := ps.GetTrashed(ctx, accountID, second); err == nil {
		t.Error("purge_product reported purged but the padded row is still in the bin")
	}

	// 3. The gate still gates.
	third := seed("Kopi Padded Ketiga ")
	out = mcpResult(t, httpSrv.URL, rawKey, "purge_product",
		map[string]any{"product_id": third, "confirm_name": "definitely not the name"})
	if out["status"] != "refused" {
		t.Fatalf("purge_product with a WRONG name returned %v, want status=refused — trimming "+
			"both sides must not make the confirmation a formality", out)
	}
	if _, err := ps.GetTrashed(ctx, accountID, third); err != nil {
		t.Fatalf("the REFUSED purge destroyed the product anyway: %v", err)
	}
}

// mcpResult calls a tool through the authenticated HTTP handler and decodes the
// structured content, failing loudly if the call itself errored (a transport or
// scope failure must never be mistaken for the tool's own answer).
func mcpResult(t *testing.T, httpSrvURL, rawKey, tool string, args map[string]any) map[string]any {
	t.Helper()
	res := mcpCall(t, httpSrvURL, rawKey, tool, args)
	if res.IsError {
		t.Fatalf("%s returned an error: %s", tool, contentText(res))
	}
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("%s: marshal result: %v", tool, err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("%s: decode result: %v", tool, err)
	}
	return out
}
