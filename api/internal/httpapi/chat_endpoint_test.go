// The in-app assistant (phase 1) at the HTTP edge.
//
// THE CRITERIA THIS FILE EXISTS FOR
//
//  1. READ-ONLY IS ENFORCED BY THE ENDPOINT, not by the prompt. Whatever the
//     caller's own session could do, the identity the chat hands to its tools
//     carries the readonly scope — so a write tool refuses. This is the
//     feature's central claim and the one a UI test cannot see.
//  2. A CHAT BELONGS TO A PERSON. An API-key/agent call is refused: the one
//     write (file feedback) is filed on the caller's behalf, and an API key
//     carries no person to file for.
//  3. THE LIMITS ARE REAL. Every message is at least one LLM call, so the
//     per-user bucket must refuse the 61st message in an hour rather than
//     answering it.
//  4. AN UNAVAILABLE ASSISTANT SAYS SO. With no runner wired, GET reports
//     available:false and POST answers 503 — not a panic and not an empty
//     reply, because the UI's whole "hide the box" path reads that flag.
//
// WHY A FAKE RUNNER IS THE RIGHT INSTRUMENT HERE. The real runner's job (call
// the model, dispatch MCP tools through the go-sdk session) is tested in
// internal/chat and internal/mcpsrv; what THIS package owns is the edge: which
// identity is built, which limits apply, and what happens with no configuration.
// A fake keeps the LLM and the network out of these assertions, which is the only
// way they can be deterministic.
//
// Integration against a real Postgres + Valkey (a real Sessions store is needed
// for the session cookie), skipped without the standard test env.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/chat"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/llm"
	"github.com/wzul/jomform/api/internal/mcpsrv"
	"github.com/wzul/jomform/api/internal/testdb"
)

// recordingRunner is a chat.Runner that answers without a model and captures the
// context it was given, so the test can assert on the identity the endpoint
// built.
type recordingRunner struct {
	mu sync.Mutex
	// gotScopes / gotUserID / gotAccountID come from the ctx of the last Run.
	gotScopes    []string
	gotUserID    string
	gotAccountID string
	// gotLang is the DASHBOARD language the endpoint attached to the ctx
	// (chat.WithDashboardLang). The card's one model-written sentence follows it,
	// so a test can assert the endpoint read the right column (t_ec87d6c7).
	gotLang string
	gotRuns int
	// ready is what Ready() reports. Default false so a test that forgets to
	// set it sees the "unavailable" path rather than silently passing through
	// the availability gate; the tests that want a working assistant set it.
	ready bool
	// reply is what Run answers with.
	reply *chat.Result
	// err, when set, is returned instead of reply.
	err error
	// block, when set, makes Run synchronously wait for a release.
	block chan struct{}

	// ── phase 2 (proposal confirmation) ──
	// confirmScopes / confirmUserID / confirmAccountID are the identity of the
	// ctx Confirm was given (the HUMAN's, which must carry the write scope).
	confirmScopes    []string
	confirmUserID    string
	confirmAccountID string
	// confirmCalls records every (token, argsHash) pair Confirm was asked to
	// execute, so a test can assert the endpoint passed on exactly what the
	// client sent.
	confirmCalls []string
	// confirmErr, when set, is what Confirm returns.
	confirmErr error
	// confirmResult is what Confirm answers with on success.
	confirmResult *chat.Confirmed
	// cancelCalls records every Cancel the endpoint made (userID|token), and
	// cancelErr is what Cancel returns.
	cancelCalls []string
	cancelErr   error
}

// Ready is the availability half of ChatRunner. It is a field here because the
// tests that care about availability are about the FLAG, not the runner.
func (r *recordingRunner) Ready() bool { return r.ready }

// Confirm records the identity it was handed and what it was asked to execute.
// The REAL implementation is what enforces the token binding; this fake exists so
// the EDGE can be asserted (which identity, which limiter, which status).
func (r *recordingRunner) Confirm(ctx context.Context, writeCtx context.Context, userID, token, argsHash string) (*chat.Confirmed, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if id := mcpsrv.IdentityFromContext(writeCtx); id != nil {
		r.confirmScopes = append([]string(nil), id.Scopes...)
		r.confirmUserID = id.UserID
		r.confirmAccountID = id.AccountID
	}
	r.confirmCalls = append(r.confirmCalls, token+"|"+argsHash)
	if r.confirmErr != nil {
		return nil, r.confirmErr
	}
	if r.confirmResult != nil {
		return r.confirmResult, nil
	}
	return &chat.Confirmed{Tool: "update_product", Result: `{"status":"updated"}`}, nil
}

func (r *recordingRunner) Cancel(_ context.Context, userID, token string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.confirmCalls = append(r.confirmCalls, "cancel|"+token)
	r.cancelCalls = append(r.cancelCalls, userID+"|"+token)
	return r.cancelErr
}

func (r *recordingRunner) Run(ctx context.Context, history []chat.Turn, message string) (*chat.Result, error) {
	if r.block != nil {
		<-r.block
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gotRuns++
	r.gotLang = chat.DashboardLangFrom(ctx)
	if id := mcpsrv.IdentityFromContext(ctx); id != nil {
		r.gotScopes = append([]string(nil), id.Scopes...)
		r.gotUserID = id.UserID
		r.gotAccountID = id.AccountID
	}
	if r.err != nil {
		return nil, r.err
	}
	if r.reply != nil {
		return r.reply, nil
	}
	return &chat.Result{Reply: "ok"}, nil
}

type chatEnv struct {
	mux  *http.ServeMux
	pool *pgxpool.Pool
	h    *RESTHandlers
	// sessions issues a cookie for a user id under the same store the mux uses.
	sessions interface {
		Create(ctx context.Context, userID string) (string, error)
	}
}

func newChatEnv(t *testing.T, runner ChatRunner) *chatEnv {
	t.Helper()
	pool := bundleTestPool(t)
	sess := authTestValkey(t)
	h := &RESTHandlers{
		Accounts: dbstore.NewAccountStore(pool),
		Users:    dbstore.NewUserStore(pool),
		Forms:    dbstore.NewFormStore(pool),
		Sales:    dbstore.NewSaleStore(pool),
		Sessions: sess,
		Events:   dbstore.NewEventStore(pool),
		Feedback: dbstore.NewFeedbackThreadStore(pool),
		Chat:     runner,
		// A fresh bucket per env: the limiter is per-server in production, and a
		// shared one here would make tests depend on each other's message counts.
		ChatLimiter: NewChatLimiter(),
		// Phase 2: the confirmation bucket, fresh for the same reason.
		ChatConfirmLimiter: NewChatConfirmLimiter(),
		// Chat history: the stored conversations (rest_chat_sessions.go) and the
		// workspace store that resolves their scope. Wired here so every chat test
		// runs against the same server shape production has.
		ChatSessions: dbstore.NewChatSessionStore(pool),
		Workspaces:   dbstore.NewWorkspaceStore(pool),
	}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return &chatEnv{mux: mux, pool: pool, h: h, sessions: sess}
}

func (e *chatEnv) newUser(t *testing.T) (userID, email, token string) {
	t.Helper()
	ctx := context.Background()
	if err := e.pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, verified)
		 VALUES ('chat-' || md5(random()::text) || '@example.test', 'it', true)
		 RETURNING id, email`).Scan(&userID, &email); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := e.pool.Exec(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 VALUES ($1, 'chat-acc-' || md5(random()::text), 'chat-' || md5(random()::text))`,
		userID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	tok, err := e.sessions.Create(ctx, userID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return userID, email, tok
}

func (e *chatEnv) do(t *testing.T, token, method, path, body string) *httptest.ResponseRecorder {
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

// TestChatToolsRunUnderAReadOnlyIdentity is criterion 1.
//
// The scenario the feature exists for: a user asks the assistant to delete a
// form. The assistant CANNOT, and the reason is that the tools it can reach are
// dispatched with the readonly scope. This asserts the scope the endpoint builds
// — the single line that makes the whole feature safe — rather than trusting the
// prompt.
func TestChatToolsRunUnderAReadOnlyIdentity(t *testing.T) {
	runner := &recordingRunner{ready: true}
	e := newChatEnv(t, runner)
	_, _, token := e.newUser(t)

	rec := e.do(t, token, http.MethodPost, "/v1/rest/chat", `{"message":"berapa banyak borang saya?"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /v1/rest/chat = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.gotRuns != 1 {
		t.Fatalf("runner called %d times, want 1", runner.gotRuns)
	}
	if len(runner.gotScopes) != 1 || runner.gotScopes[0] != dbstore.ScopeReadonly {
		t.Fatalf("chat tools ran with scopes %v, want exactly [%s] — the assistant must not be "+
			"able to reach a write, and this scope is what enforces it",
			runner.gotScopes, dbstore.ScopeReadonly)
	}
	if dbstore.ScopeHasWrite(runner.gotScopes) {
		t.Fatal("the identity handed to the chat HAS write scope — a sentence typed into the chat " +
			"box could reach a write tool")
	}
	if runner.gotUserID == "" {
		t.Fatal("the identity carries no user id — the one write (file feedback) would have no filer")
	}
	if runner.gotAccountID == "" {
		t.Fatal("the identity carries no account id — every read tool would resolve no workspace")
	}
}

// TestChatRequiresASession is criterion 2: an unauthenticated call is refused,
// and so is an API key — a chat is a person's conversation.
func TestChatRequiresASession(t *testing.T) {
	e := newChatEnv(t, &recordingRunner{})

	if rec := e.do(t, "", http.MethodPost, "/v1/rest/chat", `{"message":"hai"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous POST /v1/rest/chat = %d, want 401", rec.Code)
	}
	if rec := e.do(t, "", http.MethodGet, "/v1/rest/chat", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous GET /v1/rest/chat = %d, want 401", rec.Code)
	}
}

// TestChatStatusReportsAvailability is criterion 4's read half: the flag the UI's
// "hide the box" path depends on.
func TestChatStatusReportsAvailability(t *testing.T) {
	// Without a runner: available:false, and the message is refused with 503
	// rather than panicking.
	off := newChatEnv(t, nil)
	_, _, tokOff := off.newUser(t)
	rec := off.do(t, tokOff, http.MethodGet, "/v1/rest/chat", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/rest/chat = %d, want 200", rec.Code)
	}
	var out struct {
		Available bool `json:"available"`
		ReadOnly  bool `json:"read_only"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode status: %v (%s)", err, rec.Body.String())
	}
	if out.Available {
		t.Fatal("available = true with no runner wired — the UI would offer a box that only errors")
	}
	if !out.ReadOnly {
		t.Fatal("read_only = false — the status must state the boundary the UI will display")
	}
	if rec := off.do(t, tokOff, http.MethodPost, "/v1/rest/chat", `{"message":"hai"}`); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("POST with no runner = %d, want 503", rec.Code)
	}

	// With a runner: available:true.
	on := newChatEnv(t, &recordingRunner{ready: true})
	_, _, tokOn := on.newUser(t)
	rec = on.do(t, tokOn, http.MethodGet, "/v1/rest/chat", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode status: %v (%s)", err, rec.Body.String())
	}
	if !out.Available {
		t.Fatal("available = false with a working runner wired — the feature would be invisible")
	}
}

// TestChatRejectsBadInput covers the edge validation: an empty message, an
// over-long message and an implausibly long transcript are 400s, and none of
// them reaches the model (the runner must not be called for a refused request —
// billing a user for their own typo is the failure this avoids).
func TestChatRejectsBadInput(t *testing.T) {
	runner := &recordingRunner{}
	e := newChatEnv(t, runner)
	_, _, token := e.newUser(t)

	cases := []struct {
		name string
		body string
	}{
		{"empty message", `{"message":"   "}`},
		{"over-long message", `{"message":` + jsonString(strings.Repeat("a", chat.MaxMessageChars+1)) + `}`},
		{"over-long history", `{"message":"hai","history":[` + strings.Repeat(`{"role":"user","content":"x"},`, chatMaxHistory+1) + `{"role":"user","content":"x"}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if rec := e.do(t, token, http.MethodPost, "/v1/rest/chat", tc.body); rec.Code != http.StatusBadRequest {
				t.Fatalf("%s = %d, want 400 (body %s)", tc.name, rec.Code, rec.Body.String())
			}
		})
	}
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.gotRuns != 0 {
		t.Fatalf("the runner ran %d times for refused requests — an invalid message must not cost "+
			"a model call", runner.gotRuns)
	}
}

// TestChatRateLimitIsPerUser is criterion 3.
//
// The bucket is per USER, not per IP: a household or office behind one address
// must not share an allowance, and one user with several tabs must not multiply
// theirs. Both directions are asserted, because a limiter keyed on the wrong
// thing passes a single-user test either way.
func TestChatRateLimitIsPerUser(t *testing.T) {
	e := newChatEnv(t, &recordingRunner{ready: true})
	_, _, a := e.newUser(t)
	_, _, b := e.newUser(t)

	// The burst is 10; the 11th message from one user is refused.
	for i := 0; i < chatBurst; i++ {
		if rec := e.do(t, a, http.MethodPost, "/v1/rest/chat", `{"message":"hai"}`); rec.Code != http.StatusOK {
			t.Fatalf("message %d for user A = %d, want 200 (body %s)", i+1, rec.Code, rec.Body.String())
		}
	}
	if rec := e.do(t, a, http.MethodPost, "/v1/rest/chat", `{"message":"hai"}`); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("message %d for user A = %d, want 429 — an unlimited chat is an unbounded bill",
			int(chatBurst)+1, rec.Code)
	}
	// User B is unaffected: the limit is keyed on the user, not the address.
	if rec := e.do(t, b, http.MethodPost, "/v1/rest/chat", `{"message":"hai"}`); rec.Code != http.StatusOK {
		t.Fatalf("user B's first message = %d, want 200 — the bucket must be per user, not shared "+
			"by IP (a shared address would starve a household)", rec.Code)
	}
}

// TestChatRefusedRequestsDoNotLeakTheErrorDetail pins the error shape: a model
// failure is reported as an unavailable assistant, and the transport detail is
// not returned to the caller (it can name internal hosts).
func TestChatRefusedRequestsDoNotLeakTheErrorDetail(t *testing.T) {
	e := newChatEnv(t, &recordingRunner{ready: true, err: errChatDown{}})
	_, _, token := e.newUser(t)

	rec := e.do(t, token, http.MethodPost, "/v1/rest/chat", `{"message":"hai"}`)
	if rec.Code == http.StatusOK {
		t.Fatalf("a failing runner answered 200 (body %s)", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "10.0.0.1") {
		t.Fatalf("the response leaks the transport detail: %s", rec.Body.String())
	}
}

type errChatDown struct{}

func (errChatDown) Error() string { return "dial tcp 10.0.0.1:11434: connect: connection refused" }

// TestRespondAIErrDistinguishesCredentialFromInternal pins the mapping itself, in
// both directions. The rejected-key test above drives the real client end to end;
// this one states the rule the mapping exists for: a rejected credential is a 503
// (the same answer the not-configured case gives), and every OTHER model failure
// stays an internal 500 that is logged and not leaked.
//
// Both directions matter. Mapping everything to 503 would hide real outages from
// monitoring; mapping the credential case to 500 is the bug the card found.
func TestRespondAIErrDistinguishesCredentialFromInternal(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"rejected credential", fmt.Errorf("wrapped: %w", llm.ErrCredential), http.StatusServiceUnavailable},
		// Card t_3a112596: a MISSING (404) or RETIRED (410) model is the same KIND of fault
		// as a rejected key — a deployment state — so it answers 503 with the surface's own
		// unavailable copy, under its own log reason (llm_model_unavailable). Before this,
		// the 410 fell to the generic branch and the caller got a bare 500 while the
		// assistant was still offered. The provider's "was retired" text is carried in the
		// error and must NOT reach the body.
		{"retired model", fmt.Errorf("wrapped: %w (status 410): deepseek-v3.2 was retired at 2026-09-25", llm.ErrModelUnavailable), http.StatusServiceUnavailable},
		{"missing model", fmt.Errorf("wrapped: %w (status 404): model does not exist", llm.ErrModelUnavailable), http.StatusServiceUnavailable},
		{"provider down", errChatDown{}, http.StatusInternalServerError},
		{"provider 500", errors.New("llm: status 500: boom"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/v1/rest/chat", nil)
			respondAIErr(rec, req, tc.err, "the chat assistant is not available")
			if rec.Code != tc.want {
				t.Fatalf("respondAIErr(%v) = %d, want %d (body %s)", tc.err, rec.Code, tc.want,
					rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "10.0.0.1") {
				t.Fatalf("the response leaks the transport detail: %s", rec.Body.String())
			}
			// The provider's message names the model and its retirement date; it is for
			// the LOG, never for the caller.
			if strings.Contains(rec.Body.String(), "was retired") {
				t.Fatalf("the response leaks the provider's message: %s", rec.Body.String())
			}
		})
	}
}

// TestChatUnavailableWhenNoCredentialIsConfigured is THE regression this file
// gained: the endpoint used to report available = (h.Chat != nil), i.e. whether a
// runner OBJECT was constructed. A deployment with no LLM key constructed the
// runner anyway, so the UI was offered a chat box whose every message died — the
// exact outcome web/src/app/chat/page.tsx says in its own comment that the design
// prevents ("a box that always fails is worse than no box").
//
// The runner here is REAL (*chat.Runner over the production llm client with no
// key), not a fake. A fake cannot show this bug: the bug was in the interaction
// between the runner's construction and the availability flag, and a fake whose
// Ready() is a struct field would hide it. The keyless client is built the same
// way main.go builds it — llm.New() reads OLLAMA_API_KEY from the environment —
// so the state under test is the state a fresh deploy is in.
func TestChatUnavailableWhenNoCredentialIsConfigured(t *testing.T) {
	t.Setenv("OLLAMA_API_KEY", "")

	runner := chat.New(llm.New("", ""), nil, nil)
	if runner.Ready() {
		t.Fatal("a runner over a model client with NO API key reports Ready — the UI would offer a " +
			"chat box whose every message fails")
	}
	e := newChatEnv(t, runner)
	_, _, token := e.newUser(t)

	rec := e.do(t, token, http.MethodGet, "/v1/rest/chat", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/rest/chat = %d, want 200", rec.Code)
	}
	var out struct {
		Available bool `json:"available"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode status: %v (%s)", err, rec.Body.String())
	}
	if out.Available {
		t.Fatal("available = true for a deployment with no LLM credential — the page's own comment " +
			"promises this is false so the box is hidden, and its 503 branch would be dead code")
	}
	// The POST agrees with the flag: an unavailable assistant says so with the
	// status the page already handles (503 → setAvailable(false)), not with a
	// generic 500 the page has no branch for.
	if rec := e.do(t, token, http.MethodPost, "/v1/rest/chat", `{"message":"hai"}`); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("POST with no credential = %d, want 503 (body %s) — the page's 503 → "+
			"setAvailable(false) branch must be the one that fires", rec.Code, rec.Body.String())
	}
}

// TestChatUnavailableWhenTheProviderRejectsTheKey is the other half, and the one
// prod is actually exposed to: a key that is CONFIGURED but no longer accepted
// (rotated, revoked, expired). Presence alone cannot see this — the provider's
// answer arrives only on a call — so the mapping is what has to be right.
//
// THE REAL CLIENT IS USED, against a local HTTP server standing in for the
// provider. Nothing here is hand-rolled except the provider's 401 response: the
// credential comes from OLLAMA_API_KEY exactly as main.go reads it, the client is
// llm.New (the one the whole app uses), the runner is *chat.Runner over it, and
// the request goes through the registered /v1/rest/chat route. A double that
// synthesised an llm.ErrCredential would prove only that errors.Is works on the
// sentinel I typed; this proves the client CLASSIFIES a provider rejection that
// way and that the endpoint then answers 503.
//
// Without the mapping the answer is the bare 500 the card found: the page's
// unavailable branch never fires and the deployment looks broken without an
// operator ever learning the key is bad.
func TestChatUnavailableWhenTheProviderRejectsTheKey(t *testing.T) {
	// The provider, refusing the key the way Ollama Cloud does.
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	t.Cleanup(provider.Close)

	// Configured: the key is present, so the client reports a credential and the
	// GET flag is true — the deployment looks fine until a message is sent, which
	// is exactly the situation a rotated key creates.
	t.Setenv("OLLAMA_API_KEY", "sk-rotated-and-rejected")
	model := llm.New(provider.URL, "test-model")
	// The real tool path is wired too (an MCP server with no stores): the runner
	// opens its session and builds the tool list BEFORE the model is called, so
	// the failure under test is the model's, not a missing session.
	runner := chat.New(model, mcpsrv.New(&mcpsrv.Deps{}, nil, nil, nil), nil)
	if !runner.Ready() {
		t.Fatal("the configured model reports no credential, so this test is exercising the " +
			"wrong branch (it would pass through the not-configured path)")
	}
	e := newChatEnv(t, runner)
	_, _, token := e.newUser(t)

	rec := e.do(t, token, http.MethodGet, "/v1/rest/chat", "")
	var status struct {
		Available bool `json:"available"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v (%s)", err, rec.Body.String())
	}
	if !status.Available {
		t.Fatal("available = false before any call — presence is all the status can know, and the " +
			"feature must not be invisible for a key that is merely untested")
	}

	rec = e.do(t, token, http.MethodPost, "/v1/rest/chat", `{"message":"hai"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("POST with a REJECTED key = %d, want 503 (body %s) — a rejected credential means "+
			"the assistant is unavailable, not an internal error; the page's 503 branch is the one "+
			"that hides the box", rec.Code, rec.Body.String())
	}
	// The provider's diagnosis is logged for the operator and never returned:
	// the reply is the same copy the not-configured case uses.
	if strings.Contains(rec.Body.String(), "kredensial") || strings.Contains(rec.Body.String(), "api key") {
		t.Fatalf("the response leaks the credential detail: %s", rec.Body.String())
	}
}

// testdb.Reset is referenced so the import is not dropped if every test above is
// skipped in a given environment; the chat tables are reset by the fixtures that
// own them (feedback_isolation_issue615_test.go).
var _ = testdb.Reset

// ── Phase 2: the confirmation lane at the HTTP edge ──────────────────────────

// TestChatConfirmEndpointRequiresASessionAndAToken pins the endpoint's gate before
// anything is executed: the confirmation is a PERSON's click, so it must be
// session-only, and it must be useless without a token.
func TestChatConfirmEndpointRequiresASessionAndAToken(t *testing.T) {
	runner := &recordingRunner{ready: true}
	e := newChatEnv(t, runner)
	_, _, token := e.newUser(t)

	// No session: refused before the runner is consulted at all.
	rec := e.do(t, "", http.MethodPost, "/v1/rest/chat/confirm",
		`{"token":"abc","args_hash":"deadbeef"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("confirm without a session = %d, want 401 (body %s) — a confirmation belongs to "+
			"a person, not to a bearer token", rec.Code, rec.Body.String())
	}
	if len(runner.confirmCalls) != 0 {
		t.Fatalf("the runner was asked to confirm without a session: %v", runner.confirmCalls)
	}

	// A session, but no token: refused, and the runner is still not consulted.
	rec = e.do(t, token, http.MethodPost, "/v1/rest/chat/confirm", `{"args_hash":"deadbeef"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("confirm with no token = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	// A token but NO args_hash: also refused. Without the hash nothing binds the
	// execution to what the user read, so it is not "no change requested".
	rec = e.do(t, token, http.MethodPost, "/v1/rest/chat/confirm", `{"token":"abc"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("confirm with no args_hash = %d, want 400 (body %s) — the hash is the binding, "+
			"and an absent one must not be read as 'unchanged'", rec.Code, rec.Body.String())
	}
	if len(runner.confirmCalls) != 0 {
		t.Fatalf("the runner was asked to confirm a malformed request: %v", runner.confirmCalls)
	}
}

// TestChatConfirmEndpointRunsUnderAHumanWriteIdentity pins the identity the
// confirmed tool runs under.
//
// This is the ONE place the chat gains a write, and it is granted by a human's
// click on a bound proposal — not by loosening the read-only identity every
// message runs under. The test asserts the identity the runner was HANDED, so a
// regression that passed the readonly ctx (making the feature useless) or the
// caller's own credential (making it unbounded) both fail here.
func TestChatConfirmEndpointRunsUnderAHumanWriteIdentity(t *testing.T) {
	runner := &recordingRunner{ready: true}
	e := newChatEnv(t, runner)
	userID, _, token := e.newUser(t)

	rec := e.do(t, token, http.MethodPost, "/v1/rest/chat/confirm",
		`{"token":"tok-1","args_hash":"hash-1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("confirm = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if runner.confirmUserID != userID {
		t.Errorf("the confirmed tool ran under user %q, want the session's user %q",
			runner.confirmUserID, userID)
	}
	if len(runner.confirmScopes) != 1 || runner.confirmScopes[0] != dbstore.ScopeReadWrite {
		t.Errorf("the confirmed tool ran with scopes %v, want exactly [%s] — the write is granted "+
			"for this bound proposal and nowhere else", runner.confirmScopes, dbstore.ScopeReadWrite)
	}
	// And the wire carried exactly what the client sent: no substituted token.
	if len(runner.confirmCalls) != 1 || runner.confirmCalls[0] != "tok-1|hash-1" {
		t.Errorf("the runner was asked to execute %v, want exactly [tok-1|hash-1]", runner.confirmCalls)
	}
}

// TestChatConfirmEndpointMapsStaleTokensTo409 pins the UI contract: a token that
// is spent or expired needs the CARD REPLACED, which the page tells the user by
// asking them to have the assistant prepare it again. A 400 would read as a bad
// request and give them nothing to do.
func TestChatConfirmEndpointMapsStaleTokensTo409(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"consumed", dbstore.ErrChatProposalConsumed, http.StatusConflict},
		{"expired", dbstore.ErrChatProposalExpired, http.StatusConflict},
		{"args changed", chat.ErrProposalArgsChanged, http.StatusConflict},
		// Not this caller's token: the SAME answer as a token that never existed,
		// so the endpoint cannot be used to probe for other people's proposals.
		{"not found", dbstore.ErrChatProposalNotFound, http.StatusNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			runner := &recordingRunner{ready: true, confirmErr: c.err}
			e := newChatEnv(t, runner)
			_, _, token := e.newUser(t)
			rec := e.do(t, token, http.MethodPost, "/v1/rest/chat/confirm",
				`{"token":"tok-x","args_hash":"hash-x"}`)
			if rec.Code != c.want {
				t.Fatalf("confirm with %v = %d, want %d (body %s)", c.err, rec.Code, c.want, rec.Body.String())
			}
			// Nothing internal is echoed back to the browser.
			if strings.Contains(rec.Body.String(), "pgx") || strings.Contains(rec.Body.String(), "SQLSTATE") {
				t.Errorf("the refusal leaks a database detail: %s", rec.Body.String())
			}
		})
	}
}

// TestChatCancelEndpointSpendsTheProposalServerSide pins that the Cancel button
// is a real action: the token is killed on the server, not merely hidden in the
// browser. A card the user believes they declined must not remain confirmable.
func TestChatCancelEndpointSpendsTheProposalServerSide(t *testing.T) {
	runner := &recordingRunner{ready: true}
	e := newChatEnv(t, runner)
	userID, _, token := e.newUser(t)

	// No session → refused, and the runner is not consulted.
	rec := e.do(t, "", http.MethodPost, "/v1/rest/chat/cancel", `{"token":"tok-c"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("cancel without a session = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}
	// No token → refused.
	rec = e.do(t, token, http.MethodPost, "/v1/rest/chat/cancel", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("cancel with no token = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if len(runner.cancelCalls) != 0 {
		t.Fatalf("the runner was asked to cancel without a valid request: %v", runner.cancelCalls)
	}

	// A real cancel reaches the runner, for this user and this token.
	rec = e.do(t, token, http.MethodPost, "/v1/rest/chat/cancel", `{"token":"tok-c"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if len(runner.cancelCalls) != 1 || runner.cancelCalls[0] != userID+"|tok-c" {
		t.Fatalf("the runner cancelled %v, want exactly [%s|tok-c]", runner.cancelCalls, userID)
	}
	if !strings.Contains(rec.Body.String(), "cancelled") {
		t.Errorf("the cancel response does not report the outcome: %s", rec.Body.String())
	}

	// A cancel on an already-spent proposal is a 409 with copy that tells the user
	// what to do, not a 500.
	runner.cancelErr = dbstore.ErrChatProposalConsumed
	rec = e.do(t, token, http.MethodPost, "/v1/rest/chat/cancel", `{"token":"tok-c"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("cancelling a spent proposal = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestChatRoutesAreRegisteredForTheProposalLane pins the two new routes. Without
// them the UI's buttons would post into a 404 and the feature would be a card
// nobody can answer.
func TestChatRoutesAreRegisteredForTheProposalLane(t *testing.T) {
	runner := &recordingRunner{ready: true}
	e := newChatEnv(t, runner)
	_, _, token := e.newUser(t)

	for _, path := range []string{"/v1/rest/chat/confirm", "/v1/rest/chat/cancel"} {
		// A GET on a POST-only route is 405, not 404: the route EXISTS.
		rec := e.do(t, token, http.MethodGet, path, "")
		if rec.Code == http.StatusNotFound {
			t.Errorf("%s is not registered (404) — the UI's button would post into nothing", path)
		}
	}
}
