package mcpsrv

// Issue #645 — the write-SCOPE gate on `update_profile_language`, over the REAL
// MCP transport with REAL API keys.
//
// WHY THIS FILE EXISTS. `lang_placement_mcp_test.go` drives the handler directly
// with an identity put into the context, and its fixture always carries
// `Scopes: ["readwrite"]`. That proves the language round-trips; it cannot prove
// the tool is REFUSED for a credential that may not write, because no readonly
// caller is ever constructed. The static class guard
// (scope_gate_guard_test.go, #650) catches the missing gate on the call graph,
// but a call-graph assertion is not an enforcement assertion: it does not prove
// the gate actually runs before the write.
//
// This file adds the enforcement half. Every call goes through HTTPHandler →
// authenticate → the tool handler, i.e. exactly what a configured MCP client
// does, and the assertion is on the returned call result plus the users.lang
// COLUMN — not on the handler's return value.
//
// Both directions are asserted, because a refusal test alone is satisfiable by
// "let everything fail":
//
//	readonly  update_profile_language -> REFUSED, users.lang unchanged
//	readwrite update_profile_language -> SUCCEEDS, users.lang actually set
//
// The positive control is the point: `update_profile_language` writes the
// caller's OWN user row and takes no workspace_id, so a normal readwrite key
// must keep working. A role gate bolted on here would be a permission model
// that does not fit, and this test would catch it.
//
// Skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// profileLangFixture wires a Server with the user store and mints a real API key
// row for the given scopes, returning the raw key plus the ids the test needs to
// read the column back.
func profileLangFixture(t *testing.T, scopes []string, tag string) (httpSrv *httptest.Server, rawKey, userID, accountID string, users *dbstore.UserStore) {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Open(t)

	accountID, userID = scopeTestAccount(t, pool, tag)
	keys := dbstore.NewAPIKeyStore(pool)
	rawKey = scopeTestKey(t, ctx, keys, accountID, tag+"-key", scopes)
	users = dbstore.NewUserStore(pool)

	srv := New(&Deps{
		Users: users, Keys: keys, Accounts: dbstore.NewAccountStore(pool),
		Forms: formsFor(pool),
	}, nil, nil, nil)
	httpSrv = httptest.NewServer(srv.HTTPHandler())
	t.Cleanup(httpSrv.Close)
	return httpSrv, rawKey, userID, accountID, users
}

// storedProfileLang reads users.lang for a user — the effect, not the reply.
func storedProfileLang(t *testing.T, users *dbstore.UserStore, userID string) string {
	t.Helper()
	u, err := users.ByID(context.Background(), userID)
	if err != nil {
		t.Fatalf("read user %s: %v", userID, err)
	}
	return u.Lang
}

// TestReadonlyKeyIsRefusedProfileLanguageWrite is the refusal half of the card's
// acceptance criterion: a readonly key must be refused on
// `update_profile_language`, and the refusal has to happen BEFORE the write.
//
// A `list_forms` read with the SAME key runs first as the control that the key
// works at all — otherwise a key that cannot authenticate would satisfy the
// refusal assertion for the wrong reason.
func TestReadonlyKeyIsRefusedProfileLanguageWrite(t *testing.T) {
	requireTestDB(t)
	httpSrv, rawKey, userID, _, users := profileLangFixture(t,
		[]string{dbstore.ScopeReadonly}, "langreadonly")

	// Positive control: the readonly key CAN read. Without this the test below
	// is satisfiable by a credential that does nothing.
	if res := mcpCall(t, httpSrv.URL, rawKey, "list_forms", map[string]any{}); res.IsError {
		t.Fatalf("a readonly key could not even read (list_forms): %s — the refusal below "+
			"would then prove nothing", toolResultText(t, res))
	}

	if got := storedProfileLang(t, users, userID); got != "" {
		t.Fatalf("fixture precondition: users.lang = %q, want empty before the write", got)
	}

	res := mcpCall(t, httpSrv.URL, rawKey, "update_profile_language", map[string]any{"lang": "ar"})
	ok, msg := refused(res)
	if !ok {
		t.Errorf("issue #645 REGRESSION: a readonly API key was ALLOWED to call "+
			"update_profile_language. Result: %s", toolResultText(t, res))
	} else if !strings.Contains(msg, "scope") {
		t.Errorf("update_profile_language was refused, but not for the scope reason (got %q) — a "+
			"refusal for the wrong reason leaves the hole open on a key that does not trip this one", msg)
	}

	// The write must not have landed: the gate has to run BEFORE UpdateLang.
	if got := storedProfileLang(t, users, userID); got != "" {
		t.Errorf("a readonly key changed users.lang to %q — the gate ran after the write, "+
			"not before it", got)
	}
}

// TestReadwriteKeyStillWritesProfileLanguage is the positive control. It fails if
// the fix is "refuse everything", and it fails if a workspace ROLE gate is bolted
// onto a user-row write (there is no workspace to take a role from, so a role
// gate would refuse this legitimate caller).
func TestReadwriteKeyStillWritesProfileLanguage(t *testing.T) {
	requireTestDB(t)
	httpSrv, rawKey, userID, _, users := profileLangFixture(t,
		[]string{dbstore.ScopeReadWrite}, "langreadwrite")

	res := mcpCall(t, httpSrv.URL, rawKey, "update_profile_language", map[string]any{"lang": "ta"})
	if res.IsError {
		t.Fatalf("a readwrite key was REFUSED on update_profile_language: %s — the feature is "+
			"broken for legitimate callers (an over-broad gate, or a role gate on a user-row write)",
			toolResultText(t, res))
	}

	// The column is the proof, not the reply.
	if got := storedProfileLang(t, users, userID); got != "ta" {
		t.Errorf("update_profile_language replied success but users.lang = %q, want \"ta\"", got)
	}
}

// TestUpdateProfileLanguageAcceptsTheLegacyAdminScope keeps prod's legacy `admin`
// scope working on this tool: ScopeHas treats it as satisfying every check, and a
// gate that read the scope literal instead of ScopeHasWrite would break prod keys.
func TestUpdateProfileLanguageAcceptsTheLegacyAdminScope(t *testing.T) {
	requireTestDB(t)
	httpSrv, rawKey, userID, _, users := profileLangFixture(t,
		[]string{dbstore.ScopeAdmin}, "langadmin")

	res := mcpCall(t, httpSrv.URL, rawKey, "update_profile_language", map[string]any{"lang": "zh"})
	if res.IsError {
		t.Fatalf("a legacy admin-scoped key was refused on update_profile_language: %s — prod "+
			"keys carry this scope and must keep working", toolResultText(t, res))
	}
	if got := storedProfileLang(t, users, userID); got != "zh" {
		t.Errorf("admin-scoped write replied success but users.lang = %q, want \"zh\"", got)
	}
}
