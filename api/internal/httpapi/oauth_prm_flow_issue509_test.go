package httpapi

// Issue #509 — end-to-end native-client-style OAuth walk, exercised against
// the REAL router (NewServer), the REAL dbstore.OAuthStore and the REAL
// mcpsrv transport over Postgres + Valkey. Nothing about the flow is faked:
//
//	1. discovery      GET /.well-known/oauth-protected-resource  (RFC 9728)
//	                  GET /.well-known/oauth-authorization-server (RFC 8414)
//	2. unauthenticated POST /mcp → 401 + WWW-Authenticate(resource_metadata)
//	3. DCR            POST /oauth/register          (RFC 7591)
//	4. authorize      GET  /oauth/authorize with the `resource` parameter,
//	                  as a browser carrying the user's session cookie
//	5. token          POST /oauth/token (authorization_code + PKCE)
//	6. authenticated  POST /mcp with the access token, driven by the official
//	                  MCP Go SDK client (initialize → tools/list → tools/call)
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL) AND Valkey
// (JOMFORM_TEST_VALKEY_URL); skipped when either is unset.
//
// The point of doing it end to end is that the previous bugs in this flow
// (#505/#507) were invisible to the fake-store unit tests: a 500 from the real
// INSERT, or a redirect_uri prod rule, only shows up when the real objects run.
// The same applies here — the PRM document is only useful if the URL it
// advertises is served, the challenge URL is fetchable, and the token it leads
// to actually opens /mcp.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/config"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/mcpsrv"
	"github.com/wzul/jomform/api/internal/oauthmeta"
	"github.com/wzul/jomform/api/internal/testdb"
)

// issue509Env is the live server + the seeded identity it authenticates.
type issue509Env struct {
	http     *httptest.Server
	pool     *pgxpool.Pool
	userID   string
	acctID   string
	sessions *auth.Sessions
	sess     string
}

// newIssue509Env boots the full server: real stores, real Valkey sessions, an
// MCP server, the OAuth handlers and the discovery routes. A session is minted
// for the seeded user so the human authorize step can be driven.
func newIssue509Env(t *testing.T) *issue509Env {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Open(t)

	valkeyURL := os.Getenv("JOMFORM_TEST_VALKEY_URL")
	if valkeyURL == "" {
		t.Skip("JOMFORM_TEST_VALKEY_URL not set; skipping end-to-end OAuth walk")
	}
	sessions, err := auth.NewSessions(valkeyURL, testSessionOptions()...)
	if err != nil {
		t.Fatalf("valkey: %v", err)
	}
	t.Cleanup(func() { _ = sessions.Close() })
	if err := sessions.Ping(ctx); err != nil {
		t.Fatalf("valkey ping: %v", err)
	}

	// Identity: one user owning one workspace + account (the account is what
	// the seeded MCP product hangs off).
	var userID, workspaceID, acctID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('i509-' || md5(random()::text) || '@example.test', 'it') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'i509-ws-' || md5(random()::text), 'i509-' || md5(random()::text))
		 RETURNING id`, userID).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 VALUES ($1, 'i509-acc-' || md5(random()::text), 'i509-' || md5(random()::text), $2)
		 RETURNING id`, userID, workspaceID).Scan(&acctID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	// A seeded product proves the authenticated MCP call really reached the
	// account's data (list_products is account/workspace scoped).
	if err := dbstore.NewProductStore(pool).Create(ctx, &dbstore.Product{
		AccountID: acctID, Name: "Issue 509 Widget", PriceSen: 1500, Type: dbstore.SaleTypeOneTime, Active: true,
	}); err != nil {
		t.Fatalf("seed product: %v", err)
	}

	cfg := &config.Config{
		Port: "0", DatabaseURL: os.Getenv(testdb.DSNEnv), AppBaseURL: "http://localhost:8080", Env: "dev",
	}
	mcpSrv := mcpsrv.New(&mcpsrv.Deps{
		Accounts:   dbstore.NewAccountStore(pool),
		Products:   dbstore.NewProductStore(pool),
		Workspaces: dbstore.NewWorkspaceStore(pool),
		OAuth:      dbstore.NewOAuthStore(pool),
	}, nil, nil, nil)

	// The auth wiring reads REST.Workspaces (workspace-aware login), so the
	// REST handler set must be present — as it is in production.
	rest := &RESTHandlers{
		Accounts:          dbstore.NewAccountStore(pool),
		Products:          dbstore.NewProductStore(pool),
		ProductVariations: dbstore.NewProductVariationStore(pool),
		Forms:             dbstore.NewFormStore(pool),
		Keys:              dbstore.NewAPIKeyStore(pool),
		Sales:             dbstore.NewSaleStore(pool),
		Subs:              dbstore.NewSubscriptionStore(pool),
		Workspaces:        dbstore.NewWorkspaceStore(pool),
		Users:             dbstore.NewUserStore(pool),
		Sessions:          sessions,
		BaseURL:           "http://localhost:8080",
		SecretsKey:        make([]byte, 32),
	}

	srv := NewServer(cfg, testLogger(), &Deps{
		MCP:      mcpSrv,
		Users:    dbstore.NewUserStore(pool),
		Sessions: sessions,
		Accounts: dbstore.NewAccountStore(pool),
		OAuth:    dbstore.NewOAuthStore(pool),
		REST:     rest,
	})

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	sess, err := sessions.Create(ctx, userID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return &issue509Env{http: ts, pool: pool, userID: userID, acctID: acctID, sessions: sessions, sess: sess}
}

// issue509Client is an http.Client that keeps cookies (the human authorize step
// is a browser redirect carrying the session) and never follows redirects (the
// tests assert on the Location header themselves).
func issue509Client() *http.Client {
	jar := newSimpleJar()
	return &http.Client{
		Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// TestOAuth509NativeClientFlow is the acceptance walk of the whole documented
// flow, with every hop performed over HTTP against the real server.
func TestOAuth509NativeClientFlow(t *testing.T) {
	env := newIssue509Env(t)
	client := issue509Client()

	// ── 1. Discovery ────────────────────────────────────────────────────────
	prm := getJSON(t, client, env.http.URL+oauthmeta.ProtectedResourcePath)
	// The protected resource is always https (the AS metadata's mcp_endpoint
	// has always been built as "https://"+Host, and production terminates TLS
	// at the edge): the test listener is plain http, so only its host is used.
	wantResource := "https://" + strings.TrimPrefix(env.http.URL, "http://") + "/mcp"
	if prm["resource"] != wantResource {
		t.Fatalf("PRM resource = %v, want %q", prm["resource"], wantResource)
	}
	servers, _ := prm["authorization_servers"].([]any)
	if len(servers) != 1 || servers[0] != oauthmeta.Base() {
		t.Fatalf("PRM authorization_servers = %v, want [%s]", servers, oauthmeta.Base())
	}

	// The path-inserted form must serve the same document.
	prmSuffixed := getJSON(t, client, env.http.URL+oauthmeta.ProtectedResourcePathSuffixed)
	if prmSuffixed["resource"] != prm["resource"] {
		t.Errorf("path-suffixed PRM resource = %v != %v", prmSuffixed["resource"], prm["resource"])
	}

	as := getJSON(t, client, env.http.URL+oauthmeta.AuthorizationServerPath)
	if as["issuer"] != oauthmeta.Base() {
		t.Fatalf("AS issuer = %v, want %q", as["issuer"], oauthmeta.Base())
	}
	if as["mcp_endpoint"] != prm["resource"] {
		t.Errorf("AS mcp_endpoint = %v != PRM resource = %v", as["mcp_endpoint"], prm["resource"])
	}

	// ── 2. Unauthenticated /mcp → 401 + challenge ───────────────────────────
	resp := postMCP(t, client, env.http.URL, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated /mcp = %d, want 401", resp.StatusCode)
	}
	challenge := resp.Header.Get("WWW-Authenticate")
	if !strings.Contains(challenge, "resource_metadata=") {
		t.Fatalf("401 challenge = %q, want resource_metadata", challenge)
	}
	metaURL := extractResourceMetadata(t, challenge)
	// The URL the challenge advertises must be fetchable and describe this
	// very endpoint — otherwise the client is sent to a 404.
	assertChallengeURLServed(t, env, metaURL, wantResource)

	// ── 3. DCR ──────────────────────────────────────────────────────────────
	regBody := `{"client_name":"issue-509-e2e","redirect_uris":["` + env.http.URL + `/callback"]}`
	regResp, err := client.Post(env.http.URL+"/oauth/register", "application/json", strings.NewReader(regBody))
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	defer regResp.Body.Close()
	if regResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(regResp.Body)
		t.Fatalf("register = %d body=%s", regResp.StatusCode, body)
	}
	var reg struct {
		ClientID string `json:"client_id"`
	}
	if err := json.NewDecoder(regResp.Body).Decode(&reg); err != nil || reg.ClientID == "" {
		t.Fatalf("register response: %v (%q)", err, reg.ClientID)
	}

	// ── 4. Authorize (with the RFC 8707 resource parameter) ─────────────────
	verifier := "issue-509-verifier-value"
	authURL := env.http.URL + "/oauth/authorize?" + url.Values{
		"client_id":             {reg.ClientID},
		"redirect_uri":          {env.http.URL + "/callback"},
		"response_type":         {"code"},
		"code_challenge":        {pkcePair(verifier)},
		"code_challenge_method": {"S256"},
		"state":                 {"issue509"},
		"resource":              {prm["resource"].(string)},
	}.Encode()
	// The session cookie is set at the OAuth issuer's host scope; carry it
	// explicitly (the http.Client jar does the rest for the redirect hop).
	authReq, _ := http.NewRequest("GET", authURL, nil)
	authReq.AddCookie(&http.Cookie{Name: sessionCookie, Value: env.sess})
	authResp, err := client.Do(authReq)
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	defer authResp.Body.Close()
	if authResp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(authResp.Body)
		t.Fatalf("authorize = %d body=%s", authResp.StatusCode, body)
	}
	loc, err := url.Parse(authResp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("authorize location: %v", err)
	}
	code := loc.Query().Get("code")
	if code == "" || loc.Query().Get("state") != "issue509" {
		t.Fatalf("authorize redirect = %q, want code + state", authResp.Header.Get("Location"))
	}

	// ── 5. Token (PKCE, resource param echoed) ──────────────────────────────
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {env.http.URL + "/callback"},
		"client_id":     {reg.ClientID},
		"code_verifier": {verifier},
		"resource":      {prm["resource"].(string)},
	}
	tokResp, err := client.PostForm(env.http.URL+"/oauth/token", form)
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	defer tokResp.Body.Close()
	if tokResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(tokResp.Body)
		t.Fatalf("token = %d body=%s", tokResp.StatusCode, body)
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		Scope       string `json:"scope"`
	}
	if err := json.NewDecoder(tokResp.Body).Decode(&tok); err != nil {
		t.Fatalf("token decode: %v", err)
	}
	if tok.AccessToken == "" || tok.TokenType != "Bearer" {
		t.Fatalf("token response incomplete: %+v", tok)
	}
	if tok.Scope != oauthmeta.ScopeReadWrite {
		t.Errorf("token scope = %q, want %q", tok.Scope, oauthmeta.ScopeReadWrite)
	}

	// ── 6. Authenticated /mcp via the official MCP SDK client ───────────────
	callCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sdkClient := sdk.NewClient(&sdk.Implementation{Name: "issue-509-e2e", Version: "1.0"}, nil)
	transport := &sdk.StreamableClientTransport{
		Endpoint: env.http.URL + "/mcp",
		HTTPClient: &http.Client{Transport: &bearerRoundTripper{
			base:  http.DefaultTransport,
			token: tok.AccessToken,
		}},
		DisableStandaloneSSE: true,
	}
	session, err := sdkClient.Connect(callCtx, transport, nil)
	if err != nil {
		t.Fatalf("MCP connect (authenticated /mcp) failed: %v", err)
	}
	defer session.Close()

	tools, err := session.ListTools(callCtx, nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	if len(tools.Tools) == 0 {
		t.Fatal("tools/list returned no tools")
	}

	// A real tool call proves the identity resolved to the seeded account.
	res, err := session.CallTool(callCtx, &sdk.CallToolParams{Name: "list_products"})
	if err != nil {
		t.Fatalf("tools/call list_products: %v", err)
	}
	var text strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			text.WriteString(tc.Text)
		}
	}
	if !strings.Contains(text.String(), "Issue 509 Widget") {
		t.Errorf("list_products did not return the seeded product for the authenticated account; got %q", text.String())
	}
}

// bearerRoundTripper adds the Bearer access token to every MCP request, which
// is what a native client does after the OAuth flow completes.
type bearerRoundTripper struct {
	base  http.RoundTripper
	token string
}

func (b *bearerRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	r2 := r.Clone(r.Context())
	r2.Header.Set("Authorization", "Bearer "+b.token)
	return b.base.RoundTrip(r2)
}

// TestOAuth509ResourceParameterValidatedOverHTTP pins the RFC 8707 check at the
// HTTP boundary (the unit tests cover the handlers directly): a `resource` that
// is not this MCP endpoint must be refused on both authorize and token, so no
// token is ever minted for a resource this server does not serve.
func TestOAuth509ResourceParameterValidatedOverHTTP(t *testing.T) {
	env := newIssue509Env(t)
	client := issue509Client()

	regResp, err := client.Post(env.http.URL+"/oauth/register", "application/json",
		strings.NewReader(`{"client_name":"i509-resource","redirect_uris":["`+env.http.URL+`/callback"]}`))
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	defer regResp.Body.Close()
	var reg struct {
		ClientID string `json:"client_id"`
	}
	_ = json.NewDecoder(regResp.Body).Decode(&reg)

	// authorize with a foreign resource → 400 invalid_target, no redirect.
	authURL := env.http.URL + "/oauth/authorize?" + url.Values{
		"client_id":             {reg.ClientID},
		"redirect_uri":          {env.http.URL + "/callback"},
		"response_type":         {"code"},
		"code_challenge":        {pkcePair("v")},
		"code_challenge_method": {"S256"},
		"state":                 {"s"},
		"resource":              {"https://evil.example/mcp"},
	}.Encode()
	req, _ := http.NewRequest("GET", authURL, nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: env.sess})
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("authorize(foreign resource) = %d, want 400", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "" {
		t.Errorf("must not redirect on a rejected resource: %q", loc)
	}
	var oerr map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&oerr)
	if oerr["error"] != "invalid_target" {
		t.Errorf("error = %q, want invalid_target", oerr["error"])
	}

	// token with a foreign resource → 400 invalid_target.
	tokResp, err := client.PostForm(env.http.URL+"/oauth/token", url.Values{
		"grant_type": {"authorization_code"},
		"code":       {"irrelevant"},
		"resource":   {"https://evil.example/mcp"},
	})
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	defer tokResp.Body.Close()
	if tokResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("token(foreign resource) = %d, want 400", tokResp.StatusCode)
	}
	var terr map[string]string
	_ = json.NewDecoder(tokResp.Body).Decode(&terr)
	if terr["error"] != "invalid_target" {
		t.Errorf("token error = %q, want invalid_target", terr["error"])
	}
}

// ─── helpers ────────────────────────────────────────────────────────────────

func getJSON(t *testing.T, c *http.Client, rawURL string) map[string]any {
	t.Helper()
	resp, err := c.Get(rawURL)
	if err != nil {
		t.Fatalf("GET %s: %v", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET %s = %d body=%s", rawURL, resp.StatusCode, body)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("GET %s decode: %v", rawURL, err)
	}
	return out
}

// postMCP performs the MCP streamable-HTTP handshake POST shape (JSON content
// type + both accept types) with an optional Bearer token.
func postMCP(t *testing.T, c *http.Client, base, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest("POST", base+"/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	if err != nil {
		t.Fatalf("build mcp request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	return resp
}

// extractResourceMetadata pulls the resource_metadata URL out of a
// WWW-Authenticate challenge value.
func extractResourceMetadata(t *testing.T, challenge string) string {
	t.Helper()
	const k = `resource_metadata="`
	i := strings.Index(challenge, k)
	if i < 0 {
		t.Fatalf("no resource_metadata in challenge %q", challenge)
	}
	rest := challenge[i+len(k):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		t.Fatalf("unterminated resource_metadata in challenge %q", challenge)
	}
	return rest[:j]
}

// assertChallengeURLServed rewrites the challenge URL onto the test server's
// origin (the challenge advertises the host the client called, which in tests
// is the httptest listener) and asserts it really serves the PRM document for
// that very resource.
func assertChallengeURLServed(t *testing.T, env *issue509Env, metaURL, wantResource string) {
	t.Helper()
	u, err := url.Parse(metaURL)
	if err != nil {
		t.Fatalf("parse challenge URL %q: %v", metaURL, err)
	}
	req, err := http.NewRequest("GET", env.http.URL+u.Path, nil)
	if err != nil {
		t.Fatalf("build PRM request: %v", err)
	}
	// The document is derived from the Host header, so send the host the
	// challenge advertised — the client does exactly this.
	req.Host = u.Host
	resp, err := env.http.Client().Do(req)
	if err != nil {
		t.Fatalf("fetch challenge URL: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("challenge URL %s = %d, want 200 (a 404 here is the issue #509 symptom)", metaURL, resp.StatusCode)
	}
	var doc map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatalf("decode challenge PRM: %v", err)
	}
	if doc["resource"] != wantResource {
		t.Errorf("PRM behind the challenge advertises resource %v, want %q", doc["resource"], wantResource)
	}
}

// simpleJar is a minimal cookie jar for the test client.
type simpleJar struct{ cookies []*http.Cookie }

func newSimpleJar() *simpleJar { return &simpleJar{} }

func (j *simpleJar) SetCookies(_ *url.URL, cs []*http.Cookie) {
	j.cookies = append(j.cookies, cs...)
}

func (j *simpleJar) Cookies(_ *url.URL) []*http.Cookie { return j.cookies }
