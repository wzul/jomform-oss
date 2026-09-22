package mcpsrv

// disable_2fa over MCP must revoke every trusted device, exactly like the REST
// handler does (issue #63: "disabling 2FA invalidates every trusted device —
// there must be no trust cookie lingering that would waive a (now-removed) 2FA
// step").
//
// WHAT WAS BROKEN. The MCP tool cleared the factor and stopped there:
//
//	httpapi handle2FADisable (api/internal/httpapi/auth.go)
//	    Sessions.TrustedDevices().RevokeAll(...) + clearTrustCookie(w)
//	mcpsrv disableTwoFA (api/internal/mcpsrv/parity_issue396.go)
//	    Users.DisableTOTP, and nothing else
//
// (the REST reference is named by symbol, not line: the line drifted once
// already when main advanced, and a stale number sends the next reader to the
// wrong place.)
//
// A trusted device waives the 2FA code for 30 days (auth.TrustTTL). After an MCP
// disable the code was gone but every exemption minted while it was on stayed
// live, which is the exact state issue #63 exists to prevent. The card's
// reproduction, re-created here:
//
//	TrustedDevices().IssueDevice(ctx, userID, ...) x2
//	tools/call disable_2fa {"password": "<correct>"}   -> status disabled
//	trusted devices before=2 after=2                   <- the defect
//
// WHY THIS FILE, AND WHY OVER HTTP. A test that calls the handler directly
// proves the store call is reachable; it does not prove a configured MCP client
// reaches it. Every call below therefore goes through HTTPHandler →
// authenticate → the tool handler with a real API key, and every assertion is on
// the RESULT plus the row/keyspace read back — not on the handler's return value
// (the tool returned "disabled" while the bug was live, so its own answer is not
// evidence).
//
// BITE PROOF. Delete the RevokeAll call from disableTwoFA and
// TestMCPDisable2FARevokesTrustedDevices fails on the post-read (2 devices, not
// 0). Delete only the post-read and the fixture-precondition assertion fails
// first, so the test cannot pass vacuously.
//
// Integration test: needs Postgres (JOMFORM_TEST_DATABASE_URL) and Valkey
// (JOMFORM_TEST_VALKEY_URL), skipped when either is unset — the trust set lives
// in the Valkey keyspace the session store owns, so a Postgres-only run could
// not observe the defect at all.

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// disable2faTrustFixture wires a Server with a real user store, the real
// (Valkey-backed) session store and a real API key, and exposes the raw ids the
// assertions read back.
type disable2faTrustFixture struct {
	httpSrv   *httptest.Server
	rawKey    string
	userID    string
	accountID string
	otherID   string
	users     *dbstore.UserStore
	sessions  *auth.Sessions
	// noSessions is the same wiring WITHOUT the session store, for the
	// nil-Sessions contract.
	noSessions    *httptest.Server
	noSessionsKey string
}

const issue63Password = "issue63-correct-horse-battery"

func disable2faTrustNew(t *testing.T, tag string) disable2faTrustFixture {
	t.Helper()
	requireTestDB(t)
	valkeyURL := strings.TrimSpace(os.Getenv("JOMFORM_TEST_VALKEY_URL"))
	if valkeyURL == "" {
		t.Skip("JOMFORM_TEST_VALKEY_URL not set; the trust set lives in Valkey, " +
			"so without it this test could not observe the defect")
	}
	ctx := context.Background()
	pool := testdb.Open(t)

	sessions, err := auth.NewSessions(valkeyURL)
	if err != nil {
		t.Fatalf("valkey sessions: %v", err)
	}
	t.Cleanup(func() { _ = sessions.Close() })

	accountID, userID := scopeTestAccount(t, pool, tag)
	// A second account, owned by someone else, whose trusted device must
	// SURVIVE the call: the revocation is scoped to the caller's own account,
	// not "everything in Valkey".
	_, otherID := scopeTestAccount(t, pool, tag+"other")

	hash, err := auth.HashPassword(issue63Password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	// 2FA ON with the right password: the tool's proof succeeds, so the call
	// must reach the revocation instead of stopping at a rejected credential.
	if _, err := pool.Exec(ctx,
		`UPDATE users SET password_hash = $2, totp_enabled = true, totp_secret = 'JBSWY3DPEHPK3PXP'
		 WHERE id = $1`, userID, hash); err != nil {
		t.Fatalf("arm 2FA on the fixture user: %v", err)
	}

	keys := dbstore.NewAPIKeyStore(pool)
	users := dbstore.NewUserStore(pool)
	accounts := dbstore.NewAccountStore(pool)

	rawKey := scopeTestKey(t, ctx, keys, accountID, tag+"-key",
		[]string{dbstore.ScopeReadWrite})
	srv := New(&Deps{
		Users: users, Keys: keys, Accounts: accounts,
		Forms: formsFor(pool), Sessions: sessions,
	}, nil, nil, nil)
	httpSrv := httptest.NewServer(srv.HTTPHandler())
	t.Cleanup(httpSrv.Close)

	// Same wiring, Sessions deliberately nil (a deployment with no Valkey).
	noSessionsKey := scopeTestKey(t, ctx, keys, accountID, tag+"-nosess",
		[]string{dbstore.ScopeReadWrite})
	noSessSrv := New(&Deps{
		Users: users, Keys: keys, Accounts: accounts, Forms: formsFor(pool),
	}, nil, nil, nil)
	noSessHTTP := httptest.NewServer(noSessSrv.HTTPHandler())
	t.Cleanup(noSessHTTP.Close)

	return disable2faTrustFixture{
		httpSrv: httpSrv, rawKey: rawKey,
		userID: userID, accountID: accountID, otherID: otherID,
		users: users, sessions: sessions,
		noSessions: noSessHTTP, noSessionsKey: noSessionsKey,
	}
}

// trustCount issues `n` trusted devices for userID and returns their raw tokens,
// asserting the count actually landed (so a later "0 devices" assertion cannot
// be satisfied by a fixture that never wrote any).
func (f disable2faTrustFixture) trustCount(t *testing.T, userID, label string, n int) []string {
	t.Helper()
	ctx := context.Background()
	ts := f.sessions.TrustedDevices()
	tokens := make([]string, 0, n)
	for i := 0; i < n; i++ {
		tok, err := ts.IssueDevice(ctx, userID, label, "10.0.0.1")
		if err != nil {
			t.Fatalf("issue trusted device: %v", err)
		}
		tokens = append(tokens, tok)
	}
	if got := len(f.liveDevices(t, userID)); got != n {
		t.Fatalf("fixture precondition: %d trusted device(s) for %s, want %d", got, userID, n)
	}
	return tokens
}

// liveDevices lists the devices the Profile → Security page would show.
func (f disable2faTrustFixture) liveDevices(t *testing.T, userID string) []auth.TrustDevice {
	t.Helper()
	devices, err := f.sessions.TrustedDevices().List(context.Background(), userID, "")
	if err != nil {
		t.Fatalf("list trusted devices: %v", err)
	}
	return devices
}

// totpEnabled reads the users column — the effect, not the reply.
func (f disable2faTrustFixture) totpEnabled(t *testing.T, userID string) bool {
	t.Helper()
	u, err := f.users.ByID(context.Background(), userID)
	if err != nil {
		t.Fatalf("read user %s: %v", userID, err)
	}
	return u.TOTPEnabled
}

// TestMCPDisable2FARevokesTrustedDevices is the regression this card exists for:
// the tool must leave the account with NO trusted device, and must not touch a
// different account's trust set.
//
// BITE: with the RevokeAll call removed from disableTwoFA, the post-read below
// reports 2 devices instead of 0 and this test fails.
func TestMCPDisable2FARevokesTrustedDevices(t *testing.T) {
	f := disable2faTrustNew(t, "d63trust")

	tokens := f.trustCount(t, f.userID, "Fixture laptop", 2)
	otherTokens := f.trustCount(t, f.otherID, "Other person's phone", 1)

	// The tokens really belong to this user, so the revocation target below is
	// the account owner and not something else.
	ts := f.sessions.TrustedDevices()
	for _, tok := range tokens {
		if got := ts.UserIDFor(context.Background(), tok); got != f.userID {
			t.Fatalf("fixture precondition: trust token maps to %q, want %q", got, f.userID)
		}
	}

	if !f.totpEnabled(t, f.userID) {
		t.Fatalf("fixture precondition: 2FA is not enabled for %s", f.userID)
	}

	res := mcpCall(t, f.httpSrv.URL, f.rawKey, "disable_2fa",
		map[string]any{"password": issue63Password})
	ok, msg := refused(res)
	if ok {
		t.Fatalf("disable_2fa was refused: %s", msg)
	}
	if got := succeeded(t, res)["status"]; got != "disabled" {
		t.Fatalf("disable_2fa status = %v, want disabled (result: %s)", got, toolResultText(t, res))
	}

	// The factor is gone...
	u, err := f.users.ByID(context.Background(), f.userID)
	if err != nil {
		t.Fatalf("read user %s: %v", f.userID, err)
	}
	if u.TOTPEnabled || u.TOTPSecret != "" {
		t.Errorf("2FA still armed after disable_2fa: enabled=%v secret=%q", u.TOTPEnabled, u.TOTPSecret)
	}

	// ...and so is every trust exemption minted while it was on. A device that
	// skipped a factor that no longer exists must not keep the exemption
	// (issue #63), and this is what the REST handler already did.
	if left := f.liveDevices(t, f.userID); len(left) != 0 {
		t.Errorf("issue #63 REGRESSION: disable_2fa over MCP left %d trusted device(s) alive "+
			"for user %s (want 0) — the tool cleared the factor but kept the 30-day "+
			"exemptions that waive it, while the REST handler (httpapi "+
			"handle2FADisable) has revoked them since issue #63. "+
			"Devices still trusted: %v", len(left), f.userID, left)
	}
	// Every raw token is dead too, i.e. the trust KEYS went, not only the set
	// members: a stale cookie must not resolve to the user any more.
	for _, tok := range tokens {
		if got := ts.UserIDFor(context.Background(), tok); got != "" {
			t.Errorf("after disable_2fa a trust token still resolves to user %q — the "+
				"cookie would keep waiving a 2FA step that no longer exists", got)
		}
	}

	// Positive control: the OTHER account's trusted device is untouched. A fix
	// that flushed the whole keyspace would pass the assertion above while
	// logging every merchant's devices out.
	if left := f.liveDevices(t, f.otherID); len(left) != 1 {
		t.Errorf("another account's trusted device was revoked too (%d left, want 1) — "+
			"the revocation must be scoped to the caller's own account", len(left))
	}
	for _, tok := range otherTokens {
		if got := ts.UserIDFor(context.Background(), tok); got != f.otherID {
			t.Errorf("another account's trust token no longer resolves to %q (got %q)", f.otherID, got)
		}
	}
}

// TestMCPDisable2FAKeepsTrustOnRejectedProof is the other direction: a call that
// fails the credential check must change NOTHING. Revoking before the proof is
// checked (or on any error path) would let an unauthenticated caller log every
// device out of an account with a wrong password.
func TestMCPDisable2FAKeepsTrustOnRejectedProof(t *testing.T) {
	f := disable2faTrustNew(t, "d63reject")

	f.trustCount(t, f.userID, "Fixture laptop", 2)

	res := mcpCall(t, f.httpSrv.URL, f.rawKey, "disable_2fa",
		map[string]any{"password": "definitely-not-the-password"})
	if ok, msg := refused(res); !ok {
		t.Fatalf("disable_2fa accepted a WRONG password (result: %s)", toolResultText(t, res))
	} else if !strings.Contains(msg, "kata laluan") {
		t.Errorf("disable_2fa was refused, but not for the credential reason (got %q)", msg)
	}

	if !f.totpEnabled(t, f.userID) {
		t.Errorf("a rejected disable_2fa turned 2FA OFF")
	}
	if left := f.liveDevices(t, f.userID); len(left) != 2 {
		t.Errorf("a rejected disable_2fa revoked trusted devices (%d left, want 2) — the "+
			"proof must be checked before anything is mutated", len(left))
	}
}

// TestMCPDisable2FAWithoutSessionsIsRefused pins the documented nil-Sessions
// contract. The tool cannot revoke the trust set it does not own (the set lives
// in the session store's Valkey keyspace), and clearing the factor without
// revoking is the exact hole this card fixes — so the call is refused WHOLE, the
// same "perkhidmatan tidak tersedia" the sibling revoke_all_trusted_devices
// returns, and 2FA stays on.
func TestMCPDisable2FAWithoutSessionsIsRefused(t *testing.T) {
	f := disable2faTrustNew(t, "d63nil")

	if !f.totpEnabled(t, f.userID) {
		t.Fatalf("fixture precondition: 2FA is not enabled for %s", f.userID)
	}

	res := mcpCall(t, f.noSessions.URL, f.noSessionsKey, "disable_2fa",
		map[string]any{"password": issue63Password})
	ok, msg := refused(res)
	if !ok {
		t.Fatalf("disable_2fa succeeded with NO session store (result: %s) — it cannot revoke "+
			"the trust set, so it must refuse rather than leave the exemptions alive",
			toolResultText(t, res))
	}
	if !strings.Contains(msg, "perkhidmatan tidak tersedia") {
		t.Errorf("refusal message = %q, want the perkhidmatan tidak tersedia contract shared "+
			"with revoke_all_trusted_devices", msg)
	}
	if !f.totpEnabled(t, f.userID) {
		t.Errorf("the refused call still turned 2FA OFF — nothing may be mutated on this path")
	}
}
