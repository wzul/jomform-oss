// Guard for card t_88485157 — "Log in with Cloudflare" wrote the link to the
// session user's FIRST-OWNED account instead of the workspace the merchant was
// on.
//
// THE DEFECT
//
//	`/v1/cf/start` sent the merchant to Cloudflare's consent screen and
//	`/v1/cf/callback` sealed the returned token against `Accounts.ByOwner(uid)[0]`
//	— unconditionally, with no workspace anywhere in the flow. Every other
//	account-scoped route resolves its account from `?workspace=<slug>`
//	(`resolveSessionAccount`, issue #158) because each workspace owns its own
//	account, but the OAuth round trip leaves our origin for Cloudflare and comes
//	back as a plain GET on the registered redirect URI, so a query-string scope
//	could not survive it. The result was silent: a merchant who pressed "Log in
//	with Cloudflare" while the switcher was on a second workspace linked the
//	FIRST workspace's account, saw their own workspace stay "not linked", and
//	auto-CNAME never applied there. Nothing in the UI names the account a link
//	landed on, so the only way to notice was the database.
//
// WHAT THIS FILE DRIVES, AND WHY IT IS THE WHOLE FLOW
//
//	The fix is a WIRING: `cfStart` records which workspace the flow started on
//	(a cookie — the one channel that survives the redirect), and `cfCallback`
//	resolves the target through that workspace instead of the first account.
//	Testing the resolution helper alone would prove a function nothing calls;
//	testing the callback with a seeded request would prove the decision but not
//	that `cfStart` ever recorded the scope. So this file drives BOTH real
//	handlers over a real Postgres (and a real session in Valkey), through the
//	same mux production registers, and then reads the stored credential back
//	from the DATABASE — not from the answer — because the answer says "linked"
//	whichever account it landed on.
//
//	Cloudflare is stubbed, not contacted: `cfTokenURL` and `cfAPIBase` are vars
//	for exactly this (see their declarations). The stub answers the token
//	exchange and the `/zones` verification call the callback makes, so the whole
//	handler runs, including the parts around the decision.
//
// THE ARMS
//  1. start on the SECOND workspace → that workspace's account receives the
//     sealed token and the FIRST account's stays empty. (Mutation that restores
//     the defect — callback seals against `accs[0]` — turns this RED.)
//  2. callback with a workspace the caller is NOT a member of → 403, and NO
//     account receives a token. The fallback to the first account is the bug;
//     a refusal is the fix.
//  3. no scope at all (a flow started before this cookie existed, or an
//     unscoped caller) → the first-owned account, i.e. the documented legacy
//     answer, unchanged.
//
// Integration against a real Postgres + Valkey, skipped without the test env.
package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/cryptoutil"
	"github.com/wzul/jomform/api/internal/dbstore"
)

type cfFlowEnv struct {
	mux  *http.ServeMux
	h    *RESTHandlers
	ws   *dbstore.WorkspaceStore
	pool *pgxpool.Pool
	sess *auth.Sessions
}

func newCFFlowEnv(t *testing.T) *cfFlowEnv {
	t.Helper()
	pool := bundleTestPool(t)
	sess := authTestValkey(t)
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	h := &RESTHandlers{
		Accounts:   dbstore.NewAccountStore(pool),
		Users:      dbstore.NewUserStore(pool),
		Workspaces: dbstore.NewWorkspaceStore(pool),
		Sessions:   sess,
		SecretsKey: key,
	}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return &cfFlowEnv{mux: mux, h: h, ws: dbstore.NewWorkspaceStore(pool), pool: pool, sess: sess}
}

// cfFlowUser creates a user with N workspaces (each owning its own account,
// created through the production store) and a live session token. Returns the
// user id, the session token and the slugs in creation order — so slugs[0] is
// the workspace whose account `Accounts.ByOwner(...)[0]` resolves to.
func (e *cfFlowEnv) cfFlowUser(t *testing.T, n int) (userID, token string, slugs []string) {
	t.Helper()
	ctx := context.Background()
	if err := e.pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, verified)
		 VALUES ('cfws-' || md5(random()::text) || '@example.test', 'it', true)
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	for i := 0; i < n; i++ {
		slug := "cfws-" + string(rune('a'+i)) + "-" + userID[:8]
		if _, err := e.ws.Create(ctx, userID, "CF WS", slug); err != nil {
			t.Fatalf("create workspace %d: %v", i, err)
		}
		slugs = append(slugs, slug)
	}
	tok, err := e.sess.Create(ctx, userID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return userID, tok, slugs
}

// cfFlowAccount is the account a workspace owns, read through the same store the
// handlers use so the expectation cannot drift from the resolution rule.
func (e *cfFlowEnv) cfFlowAccount(t *testing.T, userID, slug string) string {
	t.Helper()
	acc, ok, err := e.ws.AccessBySlug(context.Background(), slug, userID)
	if err != nil || !ok || len(acc.Accounts) != 1 {
		t.Fatalf("resolve account for workspace %s: ok=%v accounts=%d err=%v", slug, ok, len(acc.Accounts), err)
	}
	return acc.Accounts[0]
}

// cfFlowSealed is the stored sealed credential for an account ("" when none).
func (e *cfFlowEnv) cfFlowSealed(t *testing.T, accountID string) string {
	t.Helper()
	sealed, err := e.h.Accounts.CFToken(context.Background(), accountID)
	if err != nil {
		t.Fatalf("read cf token for account %s: %v", accountID, err)
	}
	return sealed
}

// stubCloudflare points the token exchange and the API verification at a local
// stub, so the callback runs end to end without touching Cloudflare.
func stubCloudflare(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/token":
			_, _ = io.WriteString(w, `{"access_token":"cf-access-token","refresh_token":"cf-refresh-token"}`)
		case r.URL.Path == "/zones":
			// cfVerifyToken's call: any success answer will do; the token value is
			// what the assertions are about.
			_, _ = io.WriteString(w, `{"success":true,"result":[]}`)
		default:
			t.Errorf("cloudflare stub: unexpected path %q", r.URL.Path)
			_, _ = io.WriteString(w, `{"success":false}`)
		}
	}))
	t.Cleanup(srv.Close)

	prevTok, prevAPI := cfTokenURL, cfAPIBase
	cfTokenURL = srv.URL + "/token"
	cfAPIBase = srv.URL
	t.Cleanup(func() { cfTokenURL, cfAPIBase = prevTok, prevAPI })
}

// cfGet issues a request with the session cookie plus whatever extra cookies the
// flow has set so far.
func (e *cfFlowEnv) cfGet(t *testing.T, sessionToken, path string, extra map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sessionToken})
	for name, value := range extra {
		req.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	return rec
}

// cfStartFlow runs /v1/cf/start for `path` and returns the cookies it set (the
// OAuth state, the PKCE verifier, and — with the fix — the workspace scope),
// keyed by name.
func (e *cfFlowEnv) cfStartFlow(t *testing.T, sessionToken, path string) (int, map[string]string) {
	t.Helper()
	rec := e.cfGet(t, sessionToken, path, nil)
	cookies := map[string]string{}
	for _, c := range rec.Result().Cookies() {
		if c.Value != "" {
			cookies[c.Name] = c.Value
		}
	}
	return rec.Code, cookies
}

func TestCFOAuthLinksTheWorkspaceTheFlowStartedOn(t *testing.T) {
	t.Setenv("JF_CF_OAUTH_CLIENT_ID", "test-client-id")
	t.Setenv("APP_BASE_URL", "https://forms.example.test")
	stubCloudflare(t)

	e := newCFFlowEnv(t)
	userID, token, slugs := e.cfFlowUser(t, 2)
	first := e.cfFlowAccount(t, userID, slugs[0])
	second := e.cfFlowAccount(t, userID, slugs[1])
	if first == second {
		t.Fatal("the two workspaces resolved to the same account — the fixture cannot distinguish them")
	}

	// 1. START on the SECOND workspace: the scope must travel with the flow.
	code, cookies := e.cfStartFlow(t, token, "/v1/cf/start?workspace="+slugs[1])
	if code != http.StatusFound {
		t.Fatalf("/v1/cf/start = %d, want 302 (body %s)", code, "")
	}
	if got := cookies[cfOAuthWSCookie]; got != slugs[1] {
		t.Fatalf("/v1/cf/start on workspace %s set %s=%q, want the slug — without it the "+
			"callback has nothing to resolve the account from and falls back to the first owned one",
			slugs[1], cfOAuthWSCookie, got)
	}

	// 2. CALLBACK: the token must land on the SELECTED workspace's account.
	rec := e.cfGet(t, token, "/v1/cf/callback?code=test-code&state="+cookies["cf_oauth_state"], cookies)
	if rec.Code != http.StatusFound {
		t.Fatalf("/v1/cf/callback = %d, want 302 (body %s)", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "cloudflare=linked") {
		t.Errorf("callback redirect = %q, want the /settings?cloudflare=linked landing", loc)
	}

	if sealed := e.cfFlowSealed(t, second); sealed == "" {
		t.Errorf("the workspace the flow STARTED on (%s, account %s) has no stored credential — "+
			"the link did not land where the merchant was", slugs[1], second)
	}
	if sealed := e.cfFlowSealed(t, first); sealed != "" {
		t.Errorf("the FIRST-owned account (%s, workspace %s) received the credential for a flow "+
			"started on %s — this is the defect: the merchant links an account they are not looking "+
			"at, and auto-CNAME never applies to their own workspace", first, slugs[0], slugs[1])
	}
	// The stored blob must be this flow's token, not a stale one.
	if cred, err := cfUnseal(e.h.SecretsKey, e.cfFlowSealed(t, second)); err != nil || cred.Access != "cf-access-token" {
		t.Errorf("stored credential on %s does not hold this flow's access token: cred=%+v err=%v",
			second, cred, err)
	}
}

func TestCFOAuthRefusesToLinkAWorkspaceTheCallerDoesNotHave(t *testing.T) {
	t.Setenv("JF_CF_OAUTH_CLIENT_ID", "test-client-id")
	t.Setenv("APP_BASE_URL", "https://forms.example.test")
	stubCloudflare(t)

	e := newCFFlowEnv(t)
	userID, token, slugs := e.cfFlowUser(t, 2)
	first := e.cfFlowAccount(t, userID, slugs[0])
	second := e.cfFlowAccount(t, userID, slugs[1])

	// A STRANGER's workspace, created through the same store.
	_, _, strangers := e.cfFlowUser(t, 1)

	// The flow itself starts legitimately; only the scope cookie is foreign, which
	// is the shape a stale/edited cookie produces when the callback comes back.
	code, cookies := e.cfStartFlow(t, token, "/v1/cf/start?workspace="+slugs[1])
	if code != http.StatusFound {
		t.Fatalf("/v1/cf/start = %d, want 302", code)
	}
	cookies[cfOAuthWSCookie] = strangers[0]

	rec := e.cfGet(t, token, "/v1/cf/callback?code=test-code&state="+cookies["cf_oauth_state"], cookies)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("/v1/cf/callback with a foreign workspace scope = %d, want 403 (body %s) — the "+
			"callback must refuse an unresolvable scope, not seal the credential against the "+
			"first owned account", rec.Code, rec.Body.String())
	}
	for _, acc := range []string{first, second} {
		if sealed := e.cfFlowSealed(t, acc); sealed != "" {
			t.Errorf("account %s received a credential from a REFUSED flow", acc)
		}
	}

	// The other direction: START refuses a foreign slug outright and never sets
	// the scope cookie, so a flow cannot even begin against it.
	code, cookies = e.cfStartFlow(t, token, "/v1/cf/start?workspace="+strangers[0])
	if code != http.StatusForbidden {
		t.Errorf("/v1/cf/start with a foreign workspace = %d, want 403", code)
	}
	if got, ok := cookies[cfOAuthWSCookie]; ok {
		t.Errorf("/v1/cf/start with a foreign workspace set %s=%q — the scope that travels must be "+
			"one the caller actually has", cfOAuthWSCookie, got)
	}
}

func TestCFOAuthWithoutAScopeKeepsTheFirstOwnedAccount(t *testing.T) {
	t.Setenv("JF_CF_OAUTH_CLIENT_ID", "test-client-id")
	t.Setenv("APP_BASE_URL", "https://forms.example.test")
	stubCloudflare(t)

	e := newCFFlowEnv(t)
	userID, token, slugs := e.cfFlowUser(t, 2)
	first := e.cfFlowAccount(t, userID, slugs[0])
	second := e.cfFlowAccount(t, userID, slugs[1])

	// An UNSCOPED flow (a link started before this cookie existed, or an API-style
	// caller). The documented answer is the first-owned account, and it must not
	// change: the fix is about honouring a scope, not about removing the default.
	code, cookies := e.cfStartFlow(t, token, "/v1/cf/start")
	if code != http.StatusFound {
		t.Fatalf("/v1/cf/start = %d, want 302", code)
	}
	if _, ok := cookies[cfOAuthWSCookie]; ok {
		t.Fatalf("an unscoped /v1/cf/start set %s — there is no scope to carry", cfOAuthWSCookie)
	}
	rec := e.cfGet(t, token, "/v1/cf/callback?code=test-code&state="+cookies["cf_oauth_state"], cookies)
	if rec.Code != http.StatusFound {
		t.Fatalf("/v1/cf/callback = %d, want 302 (body %s)", rec.Code, rec.Body.String())
	}
	if sealed := e.cfFlowSealed(t, first); sealed == "" {
		t.Errorf("an unscoped flow left the first-owned account (%s) empty", first)
	}
	if sealed := e.cfFlowSealed(t, second); sealed != "" {
		t.Errorf("an unscoped flow wrote to the second workspace's account (%s) — the legacy answer "+
			"is the first owned account; changing it silently would move existing links", second)
	}
}

// cfSeal stores a Cloudflare credential on an account, through the production
// write path, with an access token that NAMES the workspace so a later read can
// tell which row a write touched.
func (e *cfFlowEnv) cfSeal(t *testing.T, accountID, access string) {
	t.Helper()
	blob, err := json.Marshal(cfCred{Kind: cfKindAPIToken, Access: access, AccountEmail: "cfws@example.test"})
	if err != nil {
		t.Fatalf("marshal credential: %v", err)
	}
	sealed, err := cryptoutil.Encrypt(e.h.SecretsKey, blob)
	if err != nil {
		t.Fatalf("seal credential: %v", err)
	}
	if err := e.h.Accounts.SaveCFToken(context.Background(), accountID, sealed); err != nil {
		t.Fatalf("store credential on %s: %v", accountID, err)
	}
}

// cfPost issues an authenticated POST with the session cookie.
func (e *cfFlowEnv) cfPost(t *testing.T, sessionToken, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sessionToken})
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	return rec
}

// TestCFDisconnectClearsOnlyTheWorkspaceItNames is the DESTRUCTIVE half of card
// t_88485157, driven over the real route + Postgres.
//
// The panel's Disconnect button is the one control in this section that WRITES,
// and before the card it posted `/api/cf/disconnect` with no scope — so the
// account whose sealed token it cleared was the session user's FIRST-OWNED one,
// whatever workspace the merchant was looking at: a link they could not see,
// revoked from a page that was describing a different account.
//
// This arm names the SECOND workspace and asserts the FIRST account's credential
// is still there afterwards, read back from the database. It also pins that the
// route answers 200: it used to answer 500 on EVERY call (`NULLIF` wrote NULL
// into a NOT NULL column), and the panel hid that by clearing its own state
// optimistically, so a broken disconnect looked like a working one.
func TestCFDisconnectClearsOnlyTheWorkspaceItNames(t *testing.T) {
	e := newCFFlowEnv(t)
	userID, token, slugs := e.cfFlowUser(t, 2)
	first := e.cfFlowAccount(t, userID, slugs[0])
	second := e.cfFlowAccount(t, userID, slugs[1])
	if first == second {
		t.Fatal("the two workspaces resolved to the same account — the fixture cannot distinguish them")
	}
	e.cfSeal(t, first, "probe-token-"+slugs[0])
	e.cfSeal(t, second, "probe-token-"+slugs[1])
	if e.cfFlowSealed(t, first) == "" || e.cfFlowSealed(t, second) == "" {
		t.Fatal("fixture did not link both accounts")
	}

	rec := e.cfPost(t, token, "/v1/cf/disconnect?workspace="+slugs[1])
	if rec.Code != http.StatusOK {
		t.Fatalf("/v1/cf/disconnect?workspace=%s = %d (body %s), want 200 — an unlink writes the "+
			"EMPTY STRING because accounts.cf_token_enc is NOT NULL", slugs[1], rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "disconnect") {
		t.Errorf("disconnect body = %s, want the {\"status\":\"disconnected\"} answer", rec.Body.String())
	}

	if sealed := e.cfFlowSealed(t, second); sealed != "" {
		t.Errorf("the workspace the Disconnect named (%s, account %s) still holds a credential — the "+
			"button did not clear the account the merchant was looking at", slugs[1], second)
	}
	if sealed := e.cfFlowSealed(t, first); sealed == "" {
		t.Errorf("the FIRST-owned account (%s, workspace %s) lost its credential to a Disconnect that "+
			"named %s — this is the defect: a link the merchant cannot see is revoked from another "+
			"workspace's panel", first, slugs[0], slugs[1])
	}

	// The legacy unscoped call keeps its documented answer: the first-owned
	// account. Pinned so the scope fix adds a scope rather than moving the
	// default that older callers (and single-workspace users) already get.
	rec = e.cfPost(t, token, "/v1/cf/disconnect")
	if rec.Code != http.StatusOK {
		t.Fatalf("unscoped /v1/cf/disconnect = %d (body %s), want 200", rec.Code, rec.Body.String())
	}
	if sealed := e.cfFlowSealed(t, first); sealed != "" {
		t.Error("an UNSCOPED /v1/cf/disconnect left the first-owned account linked — the legacy answer " +
			"is the first owned account, and single-workspace users rely on it")
	}
}
