package mcpsrv

// A `read`-scoped OAuth access token must be REFUSED on every write tool.
//
// Wan's request was a scope a merchant can CHOOSE. The trap the card names is a
// scope that is DISPLAYED but not ENFORCED: if the authorize step were taught to
// record `read` while the write gates still accepted any token, a merchant would
// see "Read-only", trust it, and the agent would still be able to delete forms
// and move money. So this file is the deliverable, not the metadata edit.
//
// WHAT IS PROVEN HERE, AND HOW
//  1. Over the REAL transport, with a REAL OAuth token row holding
//     scope = {read}: every write class in the sample is refused with a scope
//     error, and the database is untouched (the gate ran BEFORE the write).
//  2. The same identity CAN still read (positive control) — otherwise the whole
//     file could be satisfied by a token that can do nothing at all.
//  3. A {readwrite} token still writes (no-regression control) — the fix must
//     not be "refuse everyone".
//  4. BOTH gate SHAPES are exercised directly, because the card calls out that
//     the boolean form is "exactly the 5dafe3a regression":
//       - requireWriteScope / requireWriteRole  (the named gate),
//       - accountScopes(ctx, true)              (callscope.go's boolean form),
//       - accountAndScope(ctx, true)            (tools.go's boolean form, which
//                                                upload_media still reaches).
//
// BITES. Neutering any one of the three checks makes the corresponding test
// fail — the transport sample for the named gate, and each direct gate test
// individually — so a fix that repairs only one path does not go green.
//
// Skipped without JOMFORM_TEST_DATABASE_URL (the token is a real DB row).
//
// NOTE ON THE FIXTURE: it mints token rows directly rather than driving the
// authorize endpoint, because this file is about ENFORCEMENT. The authorize →
// token wiring (the scope is resolved and recorded) is proven in
// internal/httpapi (TestAuthorizeHonoursRequestedScope, TestTokenResponse*).

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/oauthmeta"
	"github.com/wzul/jomform/api/internal/testdb"
)

// oauthScopeFixture builds the MCP server over the real DB with OAuth wired, and
// returns the live server URL, the pool, the seeded ids, and a helper that mints
// a token row for a given scope set.
func oauthScopeFixture(t *testing.T) (srvURL string, pool *pgxpool.Pool, accountID, userID string, mint func(scope []string) string) {
	t.Helper()
	requireTestDB(t)
	ctx := context.Background()
	pool = testdb.Open(t)
	accountID, userID = scopeTestAccount(t, pool, "oauthscope")

	keys := dbstore.NewAPIKeyStore(pool)
	oauthStore := dbstore.NewOAuthStore(pool)
	srv := New(&Deps{
		Forms: formsFor(pool), Accounts: dbstore.NewAccountStore(pool), Keys: keys,
		Products: dbstore.NewProductStore(pool),
		Sales:    dbstore.NewSaleStore(pool), Subs: dbstore.NewSubscriptionStore(pool),
		Webhooks:         dbstore.NewWebhookStore(pool),
		Workspaces:       dbstore.NewWorkspaceStore(pool),
		WorkspaceMembers: dbstore.NewWorkspaceMemberStore(pool),
		Users:            dbstore.NewUserStore(pool),
		SystemSettings:   dbstore.NewSystemSettingStore(pool),
		OAuth:            oauthStore,
	}, nil, nil, nil)
	httpSrv := httptest.NewServer(srv.HTTPHandler())
	t.Cleanup(httpSrv.Close)

	mint = func(scope []string) string {
		t.Helper()
		raw := "jf_oauthscope_" + randHex(16)
		if err := oauthStore.CreateToken(ctx, &dbstore.OAuthToken{
			TokenHash: auth.HashAPIKey(raw), RefreshHash: auth.HashAPIKey(raw + "-refresh"),
			ClientID: "jfc_scope_test", UserID: userID, AccountID: accountID,
			Scope:           scope,
			AccessExpiresAt: time.Now().UTC().Add(time.Hour),
		}); err != nil {
			t.Fatalf("create oauth token (scope %v): %v", scope, err)
		}
		return raw
	}
	return httpSrv.URL, pool, accountID, userID, mint
}

// TestReadScopeOAuthTokenIsRefusedEveryWrite is the card's acceptance criterion,
// driven end to end over the real MCP transport with a real `read` token.
func TestReadScopeOAuthTokenIsRefusedEveryWrite(t *testing.T) {
	srvURL, pool, accountID, _, mint := oauthScopeFixture(t)
	readToken := mint([]string{oauthmeta.ScopeRead})

	// ── Positive control FIRST: a read token must still READ. Without this the
	// rest of the file is satisfiable by a token that cannot do anything.
	succeeded(t, mcpCall(t, srvURL, readToken, "list_forms", map[string]any{}))

	formID, productID := scopeTestFormAndProduct(t, pool, accountID)

	// ── Every write class. The `via` column names which gate the tool routes
	// through, so a failure says WHICH shape was not enforced.
	for _, tc := range []struct {
		tool string
		via  string
		args map[string]any
	}{
		{"delete_form", "requireWriteScope via requireFormWrite", map[string]any{"form_id": formID}},
		{"update_product", "requireWriteScope via requireProductWrite", map[string]any{"product_id": productID, "name": "RENAMED BY READ TOKEN"}},
		{"archive_product", "requireWriteScope via requireProductWrite", map[string]any{"product_id": productID}},
		{"create_product", "requireWriteScope via requireWriteRole", map[string]any{"name": "read token must not create this", "price_sen": 100, "type": "one_time"}},
		{"upsert_form", "requireWriteScope via requireWriteRole", map[string]any{"slug": "read-token-should-not-exist", "title": "read token should not create this"}},
		{"create_api_key", "requireWriteScope via requireWriteRole", map[string]any{"name": "escalated-by-read-token"}},
		{"create_webhook", "requireWriteScope via requireWriteRole", map[string]any{"url": "https://example.com/hook", "secret": "shhh-secret-8", "events": []string{"sale.paid"}}},
		{"set_system_setting", "requireWriteScope via requireAdminRole", map[string]any{"key": "site_name", "value": "pwned-by-read-token"}},
		{"update_profile_phone", "requireWriteScope via requireWriteRole", map[string]any{"phone": "+60123456789"}},
		// The boolean form: upload_media resolves its account through
		// accountAndScope(ctx, true) — the shape 5dafe3a got wrong.
		{"upload_media", "accountAndScope(ctx, true) — boolean form", map[string]any{"kind": "image", "data": "aGVsbG8="}},
	} {
		res := mcpCall(t, srvURL, readToken, tc.tool, tc.args)
		ok, msg := refused(res)
		if !ok {
			t.Errorf("a `read` OAuth token was ALLOWED to call %s (%s) — the granted scope is "+
				"DISPLAYED but NOT ENFORCED. Result: %s", tc.tool, tc.via, toolResultText(t, res))
			continue
		}
		if !strings.Contains(msg, "scope") {
			t.Errorf("%s (%s) was refused, but not for the scope reason (want a scope error, "+
				"got %q) — a refusal for the wrong reason leaves the hole open on a token that "+
				"does not trip this one", tc.tool, tc.via, msg)
		}
	}

	// ── The writes must not have landed. A refusal that still wrote is not a
	// gate, it is a race.
	ctx := context.Background()
	var renamed string
	if err := pool.QueryRow(ctx, `SELECT name FROM products WHERE id=$1`, productID).Scan(&renamed); err != nil {
		t.Fatalf("read product back: %v", err)
	}
	if renamed == "RENAMED BY READ TOKEN" {
		t.Error("a `read` token renamed a product — the gate ran after the write")
	}
	var deleted bool
	if err := pool.QueryRow(ctx, `SELECT deleted_at IS NOT NULL FROM forms WHERE id=$1`, formID).Scan(&deleted); err != nil {
		t.Fatalf("read form back: %v", err)
	}
	if deleted {
		t.Error("a `read` token soft-deleted a form — the gate ran after the write")
	}
	if n := countKeysNamed(t, pool, accountID, "escalated-by-read-token"); n != 0 {
		t.Errorf("a `read` token minted %d API key(s) — a read grant can escalate itself", n)
	}
}

// TestReadWriteScopeOAuthTokenStillWrites is the no-regression half: a
// readwrite token still writes, and the row actually changed.
func TestReadWriteScopeOAuthTokenStillWrites(t *testing.T) {
	srvURL, pool, accountID, _, mint := oauthScopeFixture(t)
	rwToken := mint([]string{oauthmeta.ScopeReadWrite})
	formID, productID := scopeTestFormAndProduct(t, pool, accountID)

	out := succeeded(t, mcpCall(t, srvURL, rwToken, "archive_product",
		map[string]any{"product_id": productID}))
	if out["status"] != "archived" {
		t.Errorf("readwrite OAuth token: archive_product returned %v, want status=archived", out)
	}
	var active bool
	if err := pool.QueryRow(context.Background(),
		`SELECT active FROM products WHERE id=$1`, productID).Scan(&active); err != nil {
		t.Fatalf("read product back: %v", err)
	}
	if active {
		t.Error("readwrite OAuth token: products.active is still true after archive_product — " +
			"the scope change tightened the write gate for a token that should still be able to write")
	}
	succeeded(t, mcpCall(t, srvURL, rwToken, "delete_form", map[string]any{"form_id": formID}))
}

// TestLegacyOAuthTokenScopeStillGrantsWrites pins the migration case the card
// insists on: every client that connected BEFORE this change holds a token row
// recorded by the old single-scope flow, which wrote `readwrite`. That token
// must keep full write access — a change that made a pre-existing row mean "no
// access" would break every already-connected agent at once.
//
// It mints exactly what the old flow recorded (the DB row is the fixture) so the
// assertion is about the ROW, not about a code path.
func TestLegacyOAuthTokenScopeStillGrantsWrites(t *testing.T) {
	srvURL, pool, accountID, _, mint := oauthScopeFixture(t)
	legacy := mint([]string{oauthmeta.ScopeReadWrite})
	_, productID := scopeTestFormAndProduct(t, pool, accountID)

	succeeded(t, mcpCall(t, srvURL, legacy, "archive_product",
		map[string]any{"product_id": productID}))
}

// ─── the gate shapes, called DIRECTLY ────────────────────────────────────────
//
// The transport sample above proves the wiring; these prove each gate in
// isolation, so removing ONE check is caught with a message that names it. That
// matters because the three shapes are separate code paths: a fix that only
// repaired the named gate would leave the boolean ones open (5dafe3a).

// readIdentityCtx is what authenticate() builds for a `read` OAuth token: the
// account + user are resolved, and the scope set is exactly {read}.
func readIdentityCtx() context.Context {
	return WithAuthIdentity(context.Background(), &AuthIdentity{
		AccountID: "00000000-0000-0000-0000-0000000000aa",
		UserID:    "00000000-0000-0000-0000-0000000000bb",
		Scopes:    []string{oauthmeta.ScopeRead},
		Agent:     "scope-test",
	})
}

// readwriteIdentityCtx is the same identity holding the full grant, used as the
// positive control so these gates cannot be "fixed" by refusing everything.
func readwriteIdentityCtx() context.Context {
	return WithAuthIdentity(context.Background(), &AuthIdentity{
		AccountID: "00000000-0000-0000-0000-0000000000aa",
		UserID:    "00000000-0000-0000-0000-0000000000bb",
		Scopes:    []string{oauthmeta.ScopeReadWrite},
		Agent:     "scope-test",
	})
}

// TestRequireWriteScopeRefusesReadScope is gate shape 1: the named function
// every write tool must reach.
func TestRequireWriteScopeRefusesReadScope(t *testing.T) {
	s := &Server{deps: &Deps{}}
	ctx := readIdentityCtx()
	err := s.requireWriteScope(ctx)
	if err == nil {
		t.Fatal("requireWriteScope accepted a `read` scope — every write tool that reaches this " +
			"gate (delete_form, create_api_key, archive_product, …) is open to a read-only token")
	}
	if !strings.Contains(err.Error(), "scope") {
		t.Errorf("refused for the wrong reason: %q", err.Error())
	}
	// The role-gate wrapper must agree: write tools call IT, not the gate.
	if _, err := s.requireWriteRole(ctx); err == nil {
		t.Fatal("requireWriteRole accepted a `read` scope — the role gate wraps the scope gate, " +
			"so a read token reaching any requireWriteRole tool can write")
	}
	// Positive control: readwrite passes the scope axis.
	if err := s.requireWriteScope(readwriteIdentityCtx()); err != nil {
		t.Errorf("requireWriteScope refused a readwrite identity: %v", err)
	}
}

// TestAccountScopesWriteBooleanRefusesReadScope is gate shape 2 (callscope.go):
// the boolean `write` argument. No production caller passes true today, which is
// exactly why it needs a test — an unreferenced hole is still a hole the moment
// a caller reappears.
func TestAccountScopesWriteBooleanRefusesReadScope(t *testing.T) {
	s := &Server{deps: &Deps{}}
	ctx := readIdentityCtx()
	if _, err := s.accountScopes(ctx, true); err == nil {
		t.Fatal("accountScopes(ctx, write=true) accepted a `read` scope — this is the boolean " +
			"form the card names as the 5dafe3a regression; the write axis is not enforced here")
	} else if !strings.Contains(err.Error(), "scope") {
		t.Errorf("refused for the wrong reason: %q", err.Error())
	}
	if _, err := s.accountScopes(readwriteIdentityCtx(), true); err != nil {
		t.Errorf("accountScopes(ctx, write=true) refused a readwrite identity: %v", err)
	}
}

// TestAccountAndScopeWriteBooleanRefusesReadScope is gate shape 3 (tools.go):
// the deprecated-but-live boolean that upload_media still reaches.
func TestAccountAndScopeWriteBooleanRefusesReadScope(t *testing.T) {
	s := &Server{deps: &Deps{}}
	ctx := readIdentityCtx()
	if _, err := s.accountAndScope(ctx, true); err == nil {
		t.Fatal("accountAndScope(ctx, write=true) accepted a `read` scope — upload_media reaches " +
			"the write sink through this call, so a read-only token could upload")
	} else if !strings.Contains(err.Error(), "scope") {
		t.Errorf("refused for the wrong reason: %q", err.Error())
	}
	if _, err := s.accountAndScope(readwriteIdentityCtx(), true); err != nil {
		t.Errorf("accountAndScope(ctx, write=true) refused a readwrite identity: %v", err)
	}
}

// ─── helpers ────────────────────────────────────────────────────────────────

// countKeysNamed counts api_keys rows with the given name in the account.
func countKeysNamed(t *testing.T, pool *pgxpool.Pool, accountID, name string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM api_keys WHERE account_id=$1 AND name=$2`, accountID, name).Scan(&n); err != nil {
		t.Fatalf("count api_keys: %v", err)
	}
	return n
}
