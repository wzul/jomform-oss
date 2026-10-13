package httpapi

// THE WORKSPACE SCOPE OF THE CHAT LANE AT THE HTTP EDGE.
//
// THE DEFECT. /chat called /api/rest/chat, /chat/confirm and /chat/cancel with no
// ?workspace= query string. The server's resolveSessionAccount only resolves the
// account from a workspace slug when the parameter is present, so every chat call
// fell through to Accounts.ByOwner(...)[0] — the session user's FIRST owned
// account, regardless of the workspace selected in the Shell. Phase 1 that read
// the wrong account; phase 2 made it WRITABLE, because the confirmation executes
// the stored proposal under the account resolved by the request. A merchant with
// two workspaces selected the second one, was shown a card naming the first one's
// product, and confirming it edited the first one.
//
// WHY THE REFUSAL AND NOT JUST THE PARAMETER. The page sending ?workspace= fixes
// today's bug; it does not stop the next client from being unscoped, and an
// unscoped chat call is not a harmless read — `roleAtLeast("")==true` means the
// write gate passes for it. So an unscoped chat call from a session that can
// reach more than one workspace is refused outright (see
// requireResolvedWorkspaceScope), and a slug the caller cannot access is refused
// as a 403 rather than silently falling back to the first account, which is what
// resolveSessionAccount does for the rest of the dashboard.
//
// The negative half is the point: a session with exactly ONE accessible workspace
// must still be served (the unscoped account and the scoped one are the same row).
//
// Integration against a real Postgres + Valkey, skipped without the test env.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/chat"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/mcpsrv"
)

// scopeRunner is a runner that records the identity it was handed, so the test can
// assert WHICH account the endpoint resolved for the request. The real token
// machinery is covered elsewhere; what this file owns is the account resolution
// and the refusal.
type scopeRunner struct {
	runAccount     string
	confirmAccount string
	runUser        string
	confirmUser    string
	runs           int
	// confirmErr, when non-nil, is what Confirm answers with instead of a
	// success. It exists for the POST-CLAIM refusal shapes (card t_1900adef): the
	// edge's job is to turn whatever the lane returns into a status, a wire
	// message and a machine code, and that mapping is what those tests own.
	confirmErr error
}

func (r *scopeRunner) Ready() bool { return true }

func (r *scopeRunner) Run(ctx context.Context, _ []chat.Turn, _ string) (*chat.Result, error) {
	r.runs++
	if id := mcpsrv.IdentityFromContext(ctx); id != nil {
		r.runAccount, r.runUser = id.AccountID, id.UserID
	}
	return &chat.Result{Reply: "ok"}, nil
}

func (r *scopeRunner) Confirm(_ context.Context, writeCtx context.Context, userID, _, _ string) (*chat.Confirmed, error) {
	if id := mcpsrv.IdentityFromContext(writeCtx); id != nil {
		r.confirmAccount, r.confirmUser = id.AccountID, id.UserID
	}
	if r.confirmErr != nil {
		return nil, r.confirmErr
	}
	return &chat.Confirmed{Tool: "update_product", Result: `{"status":"updated"}`}, nil
}

func (r *scopeRunner) Cancel(context.Context, string, string) error { return nil }

// scopeEnv owns a mux wired the way production wires it: the chat routes plus the
// workspace store the resolution needs.
type scopeEnv struct {
	mux    *http.ServeMux
	h      *RESTHandlers
	ws     *dbstore.WorkspaceStore
	pool   *pgxpool.Pool
	sess   *auth.Sessions
	runner *scopeRunner
}

func newScopeEnv(t *testing.T) *scopeEnv {
	t.Helper()
	pool := bundleTestPool(t)
	sess := authTestValkey(t)
	runner := &scopeRunner{}
	h := &RESTHandlers{
		Accounts:           dbstore.NewAccountStore(pool),
		Users:              dbstore.NewUserStore(pool),
		Workspaces:         dbstore.NewWorkspaceStore(pool),
		Sessions:           sess,
		Chat:               runner,
		ChatLimiter:        NewChatLimiter(),
		ChatConfirmLimiter: NewChatConfirmLimiter(),
	}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return &scopeEnv{mux: mux, h: h, ws: dbstore.NewWorkspaceStore(pool), pool: pool, sess: sess, runner: runner}
}

// userWithWorkspaces creates a user with N workspaces (each owning its own
// account, created through the store so the ownership rule is the production one)
// and a session cookie. It returns the user id, the session token and the slugs in
// creation order.
func (e *scopeEnv) userWithWorkspaces(t *testing.T, n int) (userID, token string, slugs []string) {
	t.Helper()
	ctx := context.Background()
	if err := e.pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, verified)
		 VALUES ('scope-' || md5(random()::text) || '@example.test', 'it', true)
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	for i := 0; i < n; i++ {
		slug := "scopews-" + string(rune('a'+i)) + "-" + userID[:8]
		if _, err := e.ws.Create(ctx, userID, "Scope WS", slug); err != nil {
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

func (e *scopeEnv) do(t *testing.T, token, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	}
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, r)
	return rec
}

// accountForWorkspace resolves the account a scoped request would pick for a slug,
// read through the same store the endpoint uses (so the expectation cannot drift
// from the resolution rule).
func (e *scopeEnv) accountForWorkspace(t *testing.T, slug string) string {
	t.Helper()
	ctx := context.Background()
	var owner string
	if err := e.pool.QueryRow(ctx,
		`SELECT owner_user_id FROM workspaces WHERE slug=$1`, slug).Scan(&owner); err != nil {
		t.Fatalf("read workspace owner for %s: %v", slug, err)
	}
	acc, ok, err := e.ws.AccessBySlug(ctx, slug, owner)
	if err != nil || !ok || len(acc.Accounts) != 1 {
		t.Fatalf("resolve account for workspace %s: ok=%v accounts=%d err=%v", slug, ok, len(acc.Accounts), err)
	}
	return acc.Accounts[0]
}

// TestChatRefusesAnAmbiguousUnscopedCallAndServesTheScopedOne is the criterion:
// "the refusal is explicit when a session caller's workspace cannot be resolved,
// not a silent fallback to the first account".
//
// The shapes are asserted together because they are one rule:
//   - unscoped + two workspaces → 403 (the guess is refused), on all three routes,
//   - scoped to the SECOND workspace → 200 AND the identity is that workspace's
//     account (the bug's exact reproduction: unscoped resolved the FIRST),
//   - a foreign slug → 403 (not a fallback).
func TestChatRefusesAnAmbiguousUnscopedCallAndServesTheScopedOne(t *testing.T) {
	e := newScopeEnv(t)
	_, token, slugs := e.userWithWorkspaces(t, 2)
	first := e.accountForWorkspace(t, slugs[0])
	selected := e.accountForWorkspace(t, slugs[1])
	if first == selected {
		t.Fatal("the two workspaces resolved to the same account — the fixture cannot distinguish them")
	}

	// 1a. UNSCOPED with two accessible workspaces: refused, and the runner is not
	//     consulted at all (no message is answered about a guessed account).
	rec := e.do(t, token, http.MethodPost, "/v1/rest/chat", `{"message":"berapa produk saya?"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unscoped chat POST from a two-workspace session = %d, want 400 (body %s) — an "+
			"unscoped chat call resolves the user's FIRST account, not the workspace they selected, and "+
			"the confirmation lane would then WRITE there", rec.Code, rec.Body.String())
	}
	if e.runner.runs != 0 {
		t.Errorf("the runner was asked to answer %d message(s) despite the refusal — a refusal that "+
			"arrives after the work is not a refusal", e.runner.runs)
	}

	// 1b. The two proposal-lane routes: a confirmation must not be executable
	//     against a guessed account.
	rec = e.do(t, token, http.MethodPost, "/v1/rest/chat/confirm",
		`{"token":"tok-1","args_hash":"hash-1"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("unscoped /chat/confirm = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	rec = e.do(t, token, http.MethodPost, "/v1/rest/chat/cancel", `{"token":"tok-1"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("unscoped /chat/cancel = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if e.runner.confirmAccount != "" {
		t.Errorf("a confirmation/refusal ran under account %s despite the refusal — this is the "+
			"write-scope half of the defect", e.runner.confirmAccount)
	}

	// 1c. GET: the availability probe belongs to the same lane, so an unscoped one
	//     from an ambiguous session is refused too.
	rec = e.do(t, token, http.MethodGet, "/v1/rest/chat", "")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("unscoped chat GET = %d, want 400", rec.Code)
	}

	// 2. SCOPED to the SECOND workspace: served, and the identity is that
	//    workspace's account — NOT the first one's.
	rec = e.do(t, token, http.MethodPost, "/v1/rest/chat?workspace="+slugs[1],
		`{"message":"berapa produk saya?"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("scoped chat POST = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if e.runner.runAccount != selected {
		t.Fatalf("the scoped chat ran under account %s, want the SELECTED workspace's account %s "+
			"(the first workspace's is %s) — this is the defect: the account came from the fallback, "+
			"not from ?workspace=", e.runner.runAccount, selected, first)
	}
	rec = e.do(t, token, http.MethodPost, "/v1/rest/chat/confirm?workspace="+slugs[1],
		`{"token":"tok-1","args_hash":"hash-1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("scoped chat confirm = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if e.runner.confirmAccount != selected {
		t.Fatalf("the confirmation would WRITE under account %s, want the selected workspace's %s",
			e.runner.confirmAccount, selected)
	}
	rec = e.do(t, token, http.MethodPost, "/v1/rest/chat/cancel?workspace="+slugs[1], `{"token":"tok-1"}`)
	if rec.Code != http.StatusOK {
		t.Errorf("scoped chat cancel = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	// 3. A slug that is NOT the caller's is a 403, not a fallback to the first
	//    account: silently resolving a foreign slug is how a typo became a read of
	//    somebody else's workspace.
	rec = e.do(t, token, http.MethodPost, "/v1/rest/chat?workspace=not-my-workspace",
		`{"message":"berapa produk saya?"}`)
	if rec.Code != http.StatusForbidden {
		t.Errorf("chat with a foreign workspace slug = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestChatServesASingleWorkspaceSessionWithoutAScope is the negative half: the
// refusal must not be ceremony. With exactly one accessible workspace the unscoped
// account and the scoped one are the same row, so refusing would lock a
// single-workspace merchant out of their assistant for no safety gain.
//
// This is the shape that must keep working for every existing user, and it is the
// one a too-broad version of the gate would break.
func TestChatServesASingleWorkspaceSessionWithoutAScope(t *testing.T) {
	e := newScopeEnv(t)
	_, token, slugs := e.userWithWorkspaces(t, 1)
	only := e.accountForWorkspace(t, slugs[0])

	rec := e.do(t, token, http.MethodPost, "/v1/rest/chat", `{"message":"berapa produk saya?"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("unscoped chat POST from a ONE-workspace session = %d, want 200 (body %s) — there is "+
			"nothing to guess, so refusing here would break the ordinary single-workspace merchant",
			rec.Code, rec.Body.String())
	}
	if e.runner.runAccount != only {
		t.Fatalf("the single-workspace chat ran under account %s, want %s", e.runner.runAccount, only)
	}
	// The availability probe and the scoped form must work for the same session,
	// so the page's fixed call is not a new failure mode.
	rec = e.do(t, token, http.MethodGet, "/v1/rest/chat", "")
	if rec.Code != http.StatusOK {
		t.Errorf("unscoped chat GET from a one-workspace session = %d, want 200 (body %s)",
			rec.Code, rec.Body.String())
	}
	rec = e.do(t, token, http.MethodPost, "/v1/rest/chat?workspace="+slugs[0],
		`{"message":"berapa produk saya?"}`)
	if rec.Code != http.StatusOK {
		t.Errorf("scoped chat POST from a one-workspace session = %d, want 200 (body %s)",
			rec.Code, rec.Body.String())
	}
}

// TestChatScopeRefusalCarriesTheTenancyCode is the SERVER half of the client
// contract t_da4b2031 depends on.
//
// THE DEFECT. A stale `jf_workspace` slug — a workspace renamed or deleted while
// it was selected — made every scoped dashboard fetch carry that slug, the server
// answered 403 "workspace tidak dijumpai", and web/src/lib/wsfetch.ts read 403 as
// "the session is no longer valid" and navigated to /login. Measured on prod
// (v1.17.0): `/chat` with `jf_workspace = ws-gone` ended on
// https://jomform.com/login.
//
// WHY THE CODE IS THE CONTRACT. The client now distinguishes the two 403s by the
// machine code in the body (`workspace.notFound` — a tenancy refusal — vs an
// uncoded 403 — an authorization failure), NOT by the status: splitting the
// status would be a public wire change for every API-key, curl and MCP caller, to
// describe a condition the `code` already names. So this test drives the refusal
// through the real handler and reads the code out of the BODY — the thing the
// client branches on. Neutralising the message→code table entry (so the refusal
// answers the generic `request.forbidden`) turns it RED, and with it the web
// suite's cross-language pin.
//
// The other direction is asserted too: the refusal must NOT carry a code that
// means "session gone", or the client would still sign the merchant out.
func TestChatScopeRefusalCarriesTheTenancyCode(t *testing.T) {
	e := newScopeEnv(t)
	_, token, slugs := e.userWithWorkspaces(t, 1)

	rec := e.do(t, token, http.MethodPost, "/v1/rest/chat?workspace="+slugs[0]+"-missing",
		`{"message":"hai"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unresolvable slug = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the refusal body is not the error envelope: %v (body=%s)", err, rec.Body.String())
	}
	if body.Code != CodeWorkspaceNotFound {
		t.Fatalf("the refusal's code = %q, want %q — the dashboard decides whether to sign the user "+
			"out from this code, so a missing/renamed one re-creates the reported sign-out "+
			"(t_da4b2031). Body: %s", body.Code, CodeWorkspaceNotFound, rec.Body.String())
	}
	// The prose survives for the API-key/curl/MCP callers that cannot read a code.
	if body.Error == "" {
		t.Errorf("the refusal lost its wire prose — API-key and MCP callers render it directly")
	}
	// The client's "this is a dead session" statuses must not be involved: a
	// 401 here would redirect a signed-in merchant whose only problem is a slug
	// their browser kept.
	if rec.Code == http.StatusUnauthorized {
		t.Errorf("the foreign-slug refusal answers 401 — the dashboard reads that as an expired " +
			"session and would navigate to /login for a stale localStorage value")
	}
}

// TestChatScopeRefusalNamesTheWorkspaceTheDashboardAlreadyHandles pins the wire
// contract of the refusal for a slug the caller cannot access. The dashboard's
// `runLoad` treats 401/403 as "the session is not valid" and every page already
// handles a 403 "workspace tidak dijumpai" from the list endpoints — a new status
// or a raw English string would be an unhandled failure path.
func TestChatScopeRefusalNamesTheWorkspaceTheDashboardAlreadyHandles(t *testing.T) {
	e := newScopeEnv(t)
	_, token, slugs := e.userWithWorkspaces(t, 1)

	rec := e.do(t, token, http.MethodPost, "/v1/rest/chat?workspace="+slugs[0]+"-missing",
		`{"message":"hai"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unresolvable slug = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "workspace tidak dijumpai") {
		t.Fatalf("the refusal body is %s, want the message the dashboard already handles "+
			"(resolveAccountScope's 403)", rec.Body.String())
	}
}

// TestChatScopeRefusalForAMissingScopeIsNotASessionFailure pins the ONE thing that
// makes the ambiguous-scope refusal safe to ship: it must not look like a dead
// session to the client.
//
// web/src/lib/wsfetch.ts (the module every dashboard fetch goes through, and the
// chat page's own `r.status === 401 || r.status === 403` branch) defines 401 and
// 403 as "the session is no longer valid" and redirects to /login. The
// missing-scope condition is not that: the merchant is signed in and the
// workspace exists — the request simply arrived without a scope. Answering 401/403
// would bounce a signed-in user to the login page for a request they never made by
// hand, which is a worse bug than the one this card fixes.
//
// The foreign-slug case is the opposite: 403 is correct and already handled.
func TestChatScopeRefusalForAMissingScopeIsNotASessionFailure(t *testing.T) {
	e := newScopeEnv(t)
	_, token, _ := e.userWithWorkspaces(t, 2)

	rec := e.do(t, token, http.MethodPost, "/v1/rest/chat", `{"message":"hai"}`)
	if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
		t.Fatalf("an unscoped chat call from a two-workspace session answered %d — the client reads "+
			"401/403 as \"the session is gone\" (wsfetch.ts isUnauthStatus) and would redirect a "+
			"signed-in merchant to /login; the missing scope is a malformed request, not a dead session",
			rec.Code)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("an unscoped chat call from a two-workspace session = %d, want 400", rec.Code)
	}
	// The message must say what to DO. A merchant who hits this has one action
	// available (choose a workspace), and the body is what the page renders.
	if body := rec.Body.String(); !strings.Contains(body, "workspace") {
		t.Fatalf("the refusal body is %s, want copy naming the workspace scope so the user knows what "+
			"to fix", body)
	}
}

// TestChatScopeCountsWorkspacesTheCallerIsAMEMBER_OF is the mixed case the
// resolution rule must not miss: the accessible set is Workspaces.ByMember (owner
// OR member), so a user who reaches two workspaces — one they own and one they were
// invited to — can be a member with a different FIRST owned account. Counting only
// owned accounts would serve the guess for them, so the gate has to count what
// ByMember returns.
//
// The fixture deliberately gives the person TWO OWNED accounts first: that is the
// only arrangement in which the unscoped fallback picks a specific row, and with
// one account it is refused earlier (authed() cannot resolve a session context for
// a user with no owned account — pre-existing, and nothing to do with this gate).
func TestChatScopeCountsWorkspacesTheCallerIsAMEMBER_OF(t *testing.T) {
	e := newScopeEnv(t)
	ctx := context.Background()

	// The invitee: their own account (so the unscoped fallback has a row to pick)
	// plus a membership in somebody else's workspace.
	memberID, memberToken, memberSlugs := e.userWithWorkspaces(t, 1)

	// The inviter's workspace, which the member is added to.
	_, _, ownerSlugs := e.userWithWorkspaces(t, 1)
	ownerSlug := ownerSlugs[0]
	var ownerID, workspaceID string
	if err := e.pool.QueryRow(ctx,
		`SELECT owner_user_id, id FROM workspaces WHERE slug=$1`, ownerSlug).
		Scan(&ownerID, &workspaceID); err != nil {
		t.Fatalf("read the inviter's workspace: %v", err)
	}
	if _, err := e.pool.Exec(ctx,
		`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1,$2,$3)`,
		workspaceID, memberID, dbstore.RoleManage); err != nil {
		t.Fatalf("add the member: %v", err)
	}

	// The premise, checked: the member now reaches TWO workspaces while OWNING
	// one. If ByMember ever stopped counting memberships, the unscoped call would
	// be served again and this test would be measuring the fixture.
	accessible, err := e.ws.ByMember(ctx, memberID)
	if err != nil {
		t.Fatalf("list the member's workspaces: %v", err)
	}
	if len(accessible) != 2 {
		t.Fatalf("the member reaches %d workspaces, want 2 (one owned, one invited) — the gate's "+
			"input is ByMember, so this is the shape it must refuse for", len(accessible))
	}

	rec := e.do(t, memberToken, http.MethodPost, "/v1/rest/chat", `{"message":"hai"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("unscoped chat for a user who reaches two workspaces (one by membership) = %d, want 400 "+
			"(body %s) — counting only OWNED accounts would serve the guess, which is the defect",
			rec.Code, rec.Body.String())
	}

	// Each of their two workspaces is addressable by slug, so nothing is
	// unreachable for a member: the invitee's own, and the inviter's.
	for _, slug := range []string{memberSlugs[0], ownerSlug} {
		rec = e.do(t, memberToken, http.MethodPost, "/v1/rest/chat?workspace="+slug, `{"message":"hai"}`)
		if rec.Code != http.StatusOK {
			t.Errorf("scoped chat for member workspace %s = %d, want 200 (body %s)",
				slug, rec.Code, rec.Body.String())
		}
	}
}
