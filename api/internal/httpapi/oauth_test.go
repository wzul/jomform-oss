package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// ─── in-memory fakes ─────────────────────────────────────────────────────────

// fakeOAuthStore implements oauthStore in-process to test the full
// authorize→token→refresh→revoke flow without Postgres.
type fakeOAuthStore struct {
	mu      sync.Mutex
	clients map[string]*dbstore.OAuthClient
	codes   map[string]*dbstore.OAuthAuthCode // code_hash → code
	tokens  []*dbstore.OAuthToken             // ordered; latest at end
}

func newFakeOAuthStore() *fakeOAuthStore {
	return &fakeOAuthStore{clients: map[string]*dbstore.OAuthClient{}, codes: map[string]*dbstore.OAuthAuthCode{}}
}

func (f *fakeOAuthStore) ClientByID(_ context.Context, clientID string) (*dbstore.OAuthClient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.clients[clientID]
	if !ok {
		return nil, dbstore.ErrOAuthClientMissing
	}
	return c, nil
}

func (f *fakeOAuthStore) CreateClient(_ context.Context, c *dbstore.OAuthClient) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clients[c.ClientID] = c
	return nil
}

func (f *fakeOAuthStore) StoreAuthCode(_ context.Context, codeHash string, c *dbstore.OAuthAuthCode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.codes[codeHash] = c
	return nil
}

func (f *fakeOAuthStore) ConsumeAuthCode(_ context.Context, codeHash string) (*dbstore.OAuthAuthCode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.codes[codeHash]
	if !ok || c.ExpiresAt.Before(time.Now()) {
		return nil, dbstore.ErrAuthCodeInvalid
	}
	delete(f.codes, codeHash) // single-use
	return c, nil
}

func (f *fakeOAuthStore) CreateToken(_ context.Context, t *dbstore.OAuthToken) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokens = append(f.tokens, t)
	return nil
}

// findActiveRefresh returns an unexpired, non-revoked token with the refresh hash.
func (f *fakeOAuthStore) findRefresh(refreshHash string) *dbstore.OAuthToken {
	for i := len(f.tokens) - 1; i >= 0; i-- {
		t := f.tokens[i]
		if t.RefreshHash == refreshHash && t.RevokedAt == nil &&
			(t.RefreshExpiresAt == nil || t.RefreshExpiresAt.After(time.Now())) {
			return t
		}
	}
	return nil
}

func (f *fakeOAuthStore) AccessTokenByHash(_ context.Context, tokenHash string) (*dbstore.OAuthToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.tokens) - 1; i >= 0; i-- {
		t := f.tokens[i]
		if t.TokenHash == tokenHash && t.RevokedAt == nil && t.AccessExpiresAt.After(time.Now()) {
			return t, nil
		}
	}
	return nil, dbstore.ErrAccessTokenInvalid
}

func (f *fakeOAuthStore) RefreshGrant(_ context.Context, refreshHash, newAccessHash, newRefreshHash string, accessExpires, refreshExpires time.Time) (*dbstore.OAuthToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	old := f.findRefresh(refreshHash)
	if old == nil {
		return nil, dbstore.ErrRefreshInvalid
	}
	old.RevokedAt = &time.Time{} // revoke old pair
	nt := &dbstore.OAuthToken{
		TokenHash: newAccessHash, RefreshHash: newRefreshHash,
		ClientID: old.ClientID, UserID: old.UserID, AccountID: old.AccountID, Scope: old.Scope,
		AccessExpiresAt: accessExpires, RefreshExpiresAt: &refreshExpires,
	}
	f.tokens = append(f.tokens, nt)
	return nt, nil
}

func (f *fakeOAuthStore) RevokeAccessToken(_ context.Context, tokenHash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.tokens {
		if t.TokenHash == tokenHash && t.RevokedAt == nil {
			t.RevokedAt = &time.Time{}
		}
	}
	return nil
}

func (f *fakeOAuthStore) RevokeByRefreshHash(_ context.Context, refreshHash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t := f.findRefresh(refreshHash); t != nil {
		t.RevokedAt = &time.Time{}
	}
	return nil
}

// fakeOAuthUsers satisfies oauthUserStore.
type fakeOAuthUsers struct{ byID map[string]*dbstore.User }

func (f *fakeOAuthUsers) ByID(_ context.Context, id string) (*dbstore.User, error) {
	if u, ok := f.byID[id]; ok {
		return u, nil
	}
	return nil, errors.New("not found")
}

// fakeOAuthSessions satisfies oauthSessionStore.
type fakeOAuthSessions struct{ userID string }

func (f *fakeOAuthSessions) GetCreated(_ context.Context, _ string) (string, time.Time, error) {
	return f.userID, time.Now(), nil
}
func (f *fakeOAuthSessions) Destroy(_ context.Context, _ string) error { return nil }

// fakeOAuthAccounts satisfies oauthAccountStore.
type fakeOAuthAccounts struct{ ownerAccounts []*dbstore.Account }

func (f *fakeOAuthAccounts) ByOwner(_ context.Context, _ string) ([]*dbstore.Account, error) {
	return f.ownerAccounts, nil
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func pkcePair(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func testOAuthHandlers(t *testing.T) (*OAuthHandlers, *fakeOAuthStore, *fakeOAuthSessions) {
	t.Helper()
	store := newFakeOAuthStore()
	users := &fakeOAuthUsers{byID: map[string]*dbstore.User{"user-1": {ID: "user-1", Email: "a@b.co", Verified: true}}}
	sessions := &fakeOAuthSessions{userID: "user-1"}
	accounts := &fakeOAuthAccounts{ownerAccounts: []*dbstore.Account{{ID: "acct-1", Slug: "merchant"}}}
	return &OAuthHandlers{
		Store:    store,
		Sessions: sessions,
		Users:    users,
		Accounts: accounts,
		BaseURL:  "http://localhost:8080",
		CodeTTL:  10 * time.Minute, AccessTokenTTL: time.Hour, RefreshTokenTTL: 30 * 24 * time.Hour,
	}, store, sessions
}

func registerTestClient(t *testing.T, o *OAuthHandlers, redirect string) string {
	t.Helper()
	body := strings.NewReader(`{"client_name":"claude","redirect_uris":["` + redirect + `"]}`)
	req := httptest.NewRequest("POST", "/oauth/register", body)
	rec := httptest.NewRecorder()
	o.register(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register status = %d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		ClientID string `json:"client_id"`
	}
	decodeJSONBody(t, rec, &out)
	if out.ClientID == "" {
		t.Fatal("no client_id returned")
	}
	return out.ClientID
}

// decodeJSONBody decodes a recorded JSON response into v.
func decodeJSONBody(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("decode json: %v body=%s", err, rec.Body.String())
	}
}

// performAuthorize runs a GET /oauth/authorize with a session cookie set.
func performAuthorize(t *testing.T, o *OAuthHandlers, clientID, redirect, verifier, state string) *httptest.ResponseRecorder {
	t.Helper()
	challenge := pkcePair(verifier)
	u := "/oauth/authorize?client_id=" + url.QueryEscape(clientID) +
		"&redirect_uri=" + url.QueryEscape(redirect) +
		"&response_type=code&code_challenge=" + url.QueryEscape(challenge) +
		"&code_challenge_method=S256&state=" + url.QueryEscape(state)
	req := httptest.NewRequest("GET", u, nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "sess-1"})
	rec := httptest.NewRecorder()
	o.authorize(rec, req)
	return rec
}

// ─── tests ───────────────────────────────────────────────────────────────────

func TestAuthorizeRequiresLoginRedirects(t *testing.T) {
	o, _, sessions := testOAuthHandlers(t)
	sessions.userID = "" // anonymous
	clientID := registerTestClient(t, o, "https://client.example.com/cb")
	req := httptest.NewRequest("GET", "/oauth/authorize?client_id="+clientID+
		"&redirect_uri="+url.QueryEscape("https://client.example.com/cb")+
		"&response_type=code&code_challenge=x&code_challenge_method=S256&state=abc", nil)
	rec := httptest.NewRecorder()
	o.authorize(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 to login", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "/login?next=") || !strings.Contains(loc, url.QueryEscape("oauth/authorize")) {
		t.Errorf("unexpected login redirect: %q", loc)
	}
}

func TestAuthorizeUnknownClient400NoRedirect(t *testing.T) {
	o, _, sessions := testOAuthHandlers(t)
	sessions.userID = "user-1"
	req := httptest.NewRequest("GET", "/oauth/authorize?client_id=nope&redirect_uri="+
		url.QueryEscape("https://evil.example.com/steal")+"&response_type=code&code_challenge=x&state=y", nil)
	rec := httptest.NewRecorder()
	o.authorize(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Errorf("must NOT redirect for unknown client: %q", loc)
	}
}

func TestAuthorizeRejectsUnregisteredRedirect(t *testing.T) {
	o, _, sessions := testOAuthHandlers(t)
	sessions.userID = "user-1"
	clientID := registerTestClient(t, o, "https://client.example.com/cb")
	// Attacker tries an unregistered redirect_uri → must 400, no redirect.
	req := httptest.NewRequest("GET", "/oauth/authorize?client_id="+clientID+
		"&redirect_uri="+url.QueryEscape("https://evil.example.com/steal")+
		"&response_type=code&code_challenge=x&code_challenge_method=S256&state=s", nil)
	rec := httptest.NewRecorder()
	o.authorize(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (open redirect guard)", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Errorf("must NOT redirect to unregistered uri: %q", loc)
	}
}

func TestAuthorizeRejectsMissingPKCE(t *testing.T) {
	o, _, sessions := testOAuthHandlers(t)
	sessions.userID = "user-1"
	clientID := registerTestClient(t, o, "https://client.example.com/cb")
	req := httptest.NewRequest("GET", "/oauth/authorize?client_id="+clientID+
		"&redirect_uri="+url.QueryEscape("https://client.example.com/cb")+
		"&response_type=code&state=s", nil)
	rec := httptest.NewRecorder()
	o.authorize(rec, req)
	// PKCE missing → redirect back with error (client/redirect are valid).
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want redirect", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "error=invalid_request") {
		t.Errorf("expected invalid_request error redirect, got %q", loc)
	}
}

func TestAuthorizeConsentReturnsCode(t *testing.T) {
	o, store, sessions := testOAuthHandlers(t)
	sessions.userID = "user-1"
	clientID := registerTestClient(t, o, "https://client.example.com/cb")
	rec := performAuthorize(t, o, clientID, "https://client.example.com/cb", "verifier-123", "state-xyz")
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse location: %v", err)
	}
	q := u.Query()
	if q.Get("state") != "state-xyz" {
		t.Errorf("state did not round-trip: %q", q.Get("state"))
	}
	code := q.Get("code")
	if code == "" {
		t.Fatal("no authorization code returned")
	}
	// Code must be stored hashed (raw code must NOT appear in the store).
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, ok := store.codes[hashToken(code)]; !ok {
		t.Error("code not found in store by hash")
	}
}

func TestTokenExchangeFullPKCEAndRefresh(t *testing.T) {
	o, _, sessions := testOAuthHandlers(t)
	sessions.userID = "user-1"
	clientID := registerTestClient(t, o, "https://client.example.com/cb")
	verifier := "random-verifier-value-42"
	state := "csrf-token"
	rec := performAuthorize(t, o, clientID, "https://client.example.com/cb", verifier, state)
	code := mustQuery(t, rec.Header().Get("Location")).Get("code")

	// 1. Token exchange with correct verifier → access + refresh.
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", "https://client.example.com/cb")
	form.Set("client_id", clientID)
	form.Set("code_verifier", verifier)
	trec := httptest.NewRecorder()
	treq := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	treq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	o.token(trec, treq)
	if trec.Code != http.StatusOK {
		t.Fatalf("token status = %d body=%s", trec.Code, trec.Body.String())
	}
	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
	}
	decodeJSONBody(t, trec, &tok)
	if tok.AccessToken == "" || tok.RefreshToken == "" || tok.TokenType != "Bearer" {
		t.Fatalf("incomplete token response: %+v", tok)
	}

	// 2. Single-use code: a second exchange must fail.
	trec2 := httptest.NewRecorder()
	treq2 := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	treq2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	o.token(trec2, treq2)
	if trec2.Code == http.StatusOK {
		t.Fatal("authorization code was NOT single-use (replay succeeded)")
	}
}

func TestTokenRejectsWrongPKCEVerifier(t *testing.T) {
	o, _, sessions := testOAuthHandlers(t)
	sessions.userID = "user-1"
	clientID := registerTestClient(t, o, "https://client.example.com/cb")
	rec := performAuthorize(t, o, clientID, "https://client.example.com/cb", "correct-verifier", "s")
	code := mustQuery(t, rec.Header().Get("Location")).Get("code")

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("client_id", clientID)
	form.Set("code_verifier", "wrong-verifier")
	trec := httptest.NewRecorder()
	treq := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	treq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	o.token(trec, treq)
	if trec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for wrong PKCE", trec.Code)
	}
	var out map[string]string
	decodeJSONBody(t, trec, &out)
	if out["error"] != "invalid_grant" {
		t.Errorf("expected invalid_grant, got %q", out["error"])
	}
}

// ─── loopback redirect_uri (issue #505, RFC 8252 §7.3) ───────────────────────

// setProdMode forces the package-global isDev to false (prod semantics) for the
// duration of a test and restores the previous value with t.Cleanup. isDev is
// set once by NewServer at wiring time and is flipped by other tests in this
// package — a test that asserts prod-only behaviour without pinning it passes
// alone and fails in the full suite (the leak noted in ogimage_test.go /
// qr_stand_issue499_test.go, issue #499). Always use this instead of writing
// isDev directly.
func setProdMode(t *testing.T) {
	t.Helper()
	prev := isDev
	isDev = false
	t.Cleanup(func() { isDev = prev })
}

// registerRaw posts redirect_uris to /oauth/register and returns the recorder.
func registerRaw(t *testing.T, o *OAuthHandlers, redirectURIs ...string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]any{"client_name": "loopback-probe", "redirect_uris": redirectURIs})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest("POST", "/oauth/register", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	o.register(rec, req)
	return rec
}

// TestRegisterAcceptsLoopbackRedirectInProd pins prod mode (isDev=false) and
// asserts the loopback shapes native MCP clients send are accepted. Before
// issue #505 every one of these was a 400 in prod, so no native client could
// even register.
func TestRegisterAcceptsLoopbackRedirectInProd(t *testing.T) {
	setProdMode(t)

	accepted := []struct {
		name string
		uri  string
	}{
		{"ipv4 loopback literal", "http://127.0.0.1:5555/callback"},
		{"ipv4 loopback any address in 127/8", "http://127.9.9.9:61234/callback"},
		{"localhost exact name", "http://localhost:5555/callback"},
		{"localhost no port", "http://localhost/callback"},
		{"ipv6 loopback", "http://[::1]:5555/callback"},
		{"https non-loopback unchanged", "https://claude.ai/api/mcp/auth_callback"},
		{"https loopback unchanged", "https://127.0.0.1:5555/callback"},
	}
	for _, tc := range accepted {
		t.Run(tc.name, func(t *testing.T) {
			o, _, _ := testOAuthHandlers(t)
			rec := registerRaw(t, o, tc.uri)
			if rec.Code != http.StatusCreated {
				t.Fatalf("register(%q) = %d, want 201 (body %s)", tc.uri, rec.Code, rec.Body.String())
			}
		})
	}
}

// TestRegisterRejectsNonLoopbackHTTPInProd is the no-open-redirect regression:
// http (and anything else that is not absolute https) to a non-loopback host
// stays rejected, including lookalike hosts that a naive string prefix match
// would wave through.
func TestRegisterRejectsNonLoopbackHTTPInProd(t *testing.T) {
	setProdMode(t)

	rejected := []struct {
		name string
		uri  string
	}{
		{"plain http host", "http://example.com/callback"},
		{"http host suffixing loopback literal", "http://127.0.0.1.evil.com/cb"},
		{"http localhost subdomain", "http://localhost.evil.com/cb"},
		{"http userinfo trick", "http://127.0.0.1@evil.com/cb"},
		{"http userinfo on localhost name", "http://localhost@evil.com/cb"},
		{"other scheme ftp", "ftp://127.0.0.1/cb"},
		{"no scheme", "127.0.0.1:5555/callback"},
		{"empty host https", "https:///callback"},
		{"empty string", ""},
	}
	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			o, _, _ := testOAuthHandlers(t)
			rec := registerRaw(t, o, tc.uri)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("register(%q) = %d, want 400 (body %s)", tc.uri, rec.Code, rec.Body.String())
			}
			var out map[string]string
			decodeJSONBody(t, rec, &out)
			if out["error"] != "invalid_redirect_uri" {
				t.Errorf("error = %q, want invalid_redirect_uri", out["error"])
			}
			if strings.Contains(strings.ToLower(out["error_description"]), "hanya dalam dev") {
				t.Errorf("stale dev-only message: %q", out["error_description"])
			}
			if !strings.Contains(out["error_description"], "https") {
				t.Errorf("message must say https is required for non-loopback: %q", out["error_description"])
			}
		})
	}
}

// TestRegisterRejectsMixedLoopbackAndNonLoopback asserts one bad URI in the
// list rejects the whole request (no partial registration).
func TestRegisterRejectsMixedLoopbackAndNonLoopback(t *testing.T) {
	setProdMode(t)
	o, _, _ := testOAuthHandlers(t)
	rec := registerRaw(t, o, "http://127.0.0.1:5555/callback", "http://example.com/callback")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("register = %d, want 400 when any URI is non-loopback http", rec.Code)
	}
}

// TestAuthorizeAcceptsChangedLoopbackPort is defect 2 (RFC 8252 §7.3): a
// registered loopback redirect with a DIFFERENT port must be accepted, because
// native clients pick a random ephemeral port on every run.
func TestAuthorizeAcceptsChangedLoopbackPort(t *testing.T) {
	setProdMode(t)

	cases := []struct {
		name       string
		registered string
		requested  string
	}{
		{"ipv4 registered port 5555, request 61234", "http://127.0.0.1:5555/callback", "http://127.0.0.1:61234/callback"},
		{"ipv4 no port registered, request with port", "http://127.0.0.1/callback", "http://127.0.0.1:61234/callback"},
		{"localhost name, changed port", "http://localhost:5555/callback", "http://localhost:61234/callback"},
		{"ipv6 no port registered, request with port", "http://[::1]/callback", "http://[::1]:61234/callback"},
		{"ipv6 loopback, changed port", "http://[::1]:5555/callback", "http://[::1]:61234/callback"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o, _, sessions := testOAuthHandlers(t)
			sessions.userID = "user-1"
			clientID := registerTestClient(t, o, tc.registered)
			rec := performAuthorize(t, o, clientID, tc.requested, "verifier-loopback", "state-lb")
			if rec.Code != http.StatusFound {
				t.Fatalf("authorize(%q) = %d, want 302 (body %s)", tc.requested, rec.Code, rec.Body.String())
			}
			loc := rec.Header().Get("Location")
			if !strings.HasPrefix(loc, tc.requested+"?") {
				t.Errorf("redirect target = %q, want it to start with %q", loc, tc.requested)
			}
			if code := mustQuery(t, loc).Get("code"); code == "" {
				t.Error("no authorization code in redirect")
			}
		})
	}
}

// TestAuthorizeRejectsLoopbackWithDifferentHostOrPath is the scoping guard for
// the port relaxation: only the PORT may differ on a loopback URI.
func TestAuthorizeRejectsLoopbackWithDifferentHostOrPath(t *testing.T) {
	setProdMode(t)

	cases := []struct {
		name       string
		registered string
		requested  string
	}{
		{"different path", "http://127.0.0.1:5555/callback", "http://127.0.0.1:5555/steal"},
		{"different path prefix not enough", "http://127.0.0.1:5555/callback", "http://127.0.0.1:5555/callback/extra"},
		{"different host literal", "http://127.0.0.1:5555/callback", "http://127.0.0.2:5555/callback"},
		{"name vs literal are different hosts", "http://127.0.0.1:5555/callback", "http://localhost:5555/callback"},
		{"literal vs name are different hosts", "http://localhost:5555/callback", "http://127.0.0.1:5555/callback"},
		{"non-loopback host", "http://127.0.0.1:5555/callback", "http://evil.com:5555/callback"},
		{"lookalike host", "http://127.0.0.1:5555/callback", "http://127.0.0.1.evil.com:5555/callback"},
		{"different scheme https", "http://127.0.0.1:5555/callback", "https://127.0.0.1:5555/callback"},
		{"different userinfo", "http://127.0.0.1:5555/callback", "http://user:pass@127.0.0.1:5555/callback"},
		{"different userinfo on registered side", "http://user:pass@127.0.0.1:5555/callback", "http://127.0.0.1:5555/callback"},
		{"userinfo trick", "http://127.0.0.1:5555/callback", "http://127.0.0.1:5555@evil.com/callback"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o, _, sessions := testOAuthHandlers(t)
			sessions.userID = "user-1"
			clientID := registerTestClient(t, o, tc.registered)
			rec := performAuthorize(t, o, clientID, tc.requested, "verifier-loopback", "state-lb")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("authorize(%q) = %d, want 400 (body %s)", tc.requested, rec.Code, rec.Body.String())
			}
			if loc := rec.Header().Get("Location"); loc != "" {
				t.Errorf("must NOT redirect to a non-allowed uri: %q", loc)
			}
		})
	}
}

// TestAuthorizeNonLoopbackKeepsExactMatch is the regression that the RFC 8252
// port relaxation did not leak into non-loopback URIs: https stays exact.
func TestAuthorizeNonLoopbackKeepsExactMatch(t *testing.T) {
	setProdMode(t)
	o, _, sessions := testOAuthHandlers(t)
	sessions.userID = "user-1"
	clientID := registerTestClient(t, o, "https://client.example.com:5555/cb")

	// Same host+path, different port → REJECTED (port freedom is loopback-only).
	rec := performAuthorize(t, o, clientID, "https://client.example.com:61234/cb", "v", "s")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("non-loopback port change = %d, want 400 (exact match)", rec.Code)
	}
	// Different path → REJECTED.
	rec = performAuthorize(t, o, clientID, "https://client.example.com:5555/other", "v", "s")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("non-loopback path change = %d, want 400 (exact match)", rec.Code)
	}
	// Exact URI still works.
	rec = performAuthorize(t, o, clientID, "https://client.example.com:5555/cb", "v", "s")
	if rec.Code != http.StatusFound {
		t.Fatalf("exact non-loopback uri = %d, want 302", rec.Code)
	}
}

// TestOAuthLoopbackEndToEndProd runs register → authorize → token over a real
// httptest server (routed through RegisterRoutes, prod mode) with a loopback
// redirect and PKCE, and decodes the token response.
func TestOAuthLoopbackEndToEndProd(t *testing.T) {
	setProdMode(t)

	o, store, sessions := testOAuthHandlers(t)
	sessions.userID = "user-1"
	mux := http.NewServeMux()
	o.RegisterRoutes(mux, 1000, 1000) // rate limit is not the subject here
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// 1. Register a native-client-style loopback redirect.
	registered := "http://127.0.0.1:5555/callback"
	regBody, _ := json.Marshal(map[string]any{"client_name": "Claude Code", "redirect_uris": []string{registered}})
	resp, err := http.Post(srv.URL+"/oauth/register", "application/json", strings.NewReader(string(regBody)))
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	regRaw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register status = %d body=%s", resp.StatusCode, regRaw)
	}
	var reg struct {
		ClientID string `json:"client_id"`
	}
	if err := json.Unmarshal(regRaw, &reg); err != nil || reg.ClientID == "" {
		t.Fatalf("register response: %v %s", err, regRaw)
	}

	// 2. Authorize with a DIFFERENT random port (the whole point of #505) and
	//    a logged-in session cookie.
	verifier := "e2e-loopback-code-verifier-0123456789"
	state := "e2e-state"
	requested := "http://127.0.0.1:61234/callback"
	authURL := srv.URL + "/oauth/authorize?client_id=" + url.QueryEscape(reg.ClientID) +
		"&redirect_uri=" + url.QueryEscape(requested) +
		"&response_type=code&code_challenge=" + url.QueryEscape(pkcePair(verifier)) +
		"&code_challenge_method=S256&state=" + url.QueryEscape(state)
	req, _ := http.NewRequest("GET", authURL, nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "sess-1"})
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	aresp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	aresp.Body.Close()
	if aresp.StatusCode != http.StatusFound {
		t.Fatalf("authorize status = %d, want 302", aresp.StatusCode)
	}
	loc := aresp.Header.Get("Location")
	if !strings.HasPrefix(loc, requested+"?") {
		t.Fatalf("authorize redirect = %q, want prefix %q", loc, requested)
	}
	code := mustQuery(t, loc).Get("code")
	if code == "" {
		t.Fatalf("no code in authorize redirect: %q", loc)
	}

	// 3. Exchange the code for tokens (same requested loopback URI, PKCE).
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", requested)
	form.Set("client_id", reg.ClientID)
	form.Set("code_verifier", verifier)
	tresp, err := http.Post(srv.URL+"/oauth/token", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	tokRaw, _ := io.ReadAll(tresp.Body)
	tresp.Body.Close()
	if tresp.StatusCode != http.StatusOK {
		t.Fatalf("token status = %d body=%s", tresp.StatusCode, tokRaw)
	}
	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
	}
	if err := json.Unmarshal(tokRaw, &tok); err != nil {
		t.Fatalf("decode token response: %v body=%s", err, tokRaw)
	}
	// Evidence trail for the local run (no secret material printed).
	t.Logf("POST /oauth/register  -> 201 client_id=%s", reg.ClientID)
	t.Logf("GET  /oauth/authorize -> 302 Location=%s?code=<redacted>&state=%s", requested, state)
	t.Logf("POST /oauth/token     -> 200 token_type=%s expires_in=%d scope=%s access_token=%d chars refresh_token=%d chars",
		tok.TokenType, tok.ExpiresIn, tok.Scope, len(tok.AccessToken), len(tok.RefreshToken))
	if tok.AccessToken == "" || tok.RefreshToken == "" || tok.TokenType != "Bearer" || tok.ExpiresIn <= 0 {
		t.Fatalf("incomplete token response: %+v", tok)
	}
	// The returned raw token must authenticate, and only its hash is stored.
	if _, err := store.AccessTokenByHash(context.Background(), hashToken(tok.AccessToken)); err != nil {
		t.Fatalf("issued access token does not resolve in the store: %v", err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, st := range store.tokens {
		if st.TokenHash == tok.AccessToken {
			t.Error("raw access token was stored — secret material must be hashed at rest")
		}
	}
}

func TestRegisterRejectsNonHTTPSRedirect(t *testing.T) {
	setProdMode(t) // pin prod semantics: http to a non-loopback host must 400
	o, _, _ := testOAuthHandlers(t)
	body := strings.NewReader(`{"client_name":"x","redirect_uris":["http://example.com/cb"]}`)
	req := httptest.NewRequest("POST", "/oauth/register", body)
	rec := httptest.NewRecorder()
	o.register(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for non-https redirect", rec.Code)
	}
}

func TestRevokeInvalidatesAccessToken(t *testing.T) {
	o, store, sessions := testOAuthHandlers(t)
	sessions.userID = "user-1"
	clientID := registerTestClient(t, o, "https://client.example.com/cb")
	aurl := mustPerformAuthorize(t, o, clientID, "https://client.example.com/cb", "v1", "s").Header().Get("Location")
	code := mustQuery(t, aurl).Get("code")

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("client_id", clientID)
	form.Set("code_verifier", "v1")
	trec := httptest.NewRecorder()
	treq := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	treq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	o.token(trec, treq)
	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	decodeJSONBody(t, trec, &tok)

	// Revoke the access token.
	rrec := httptest.NewRecorder()
	rev := url.Values{}
	rev.Set("token", tok.AccessToken)
	rev.Set("token_type_hint", "access_token")
	rreq := httptest.NewRequest("POST", "/oauth/revoke", strings.NewReader(rev.Encode()))
	rreq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	o.revoke(rrec, rreq)
	if rrec.Code != http.StatusOK {
		t.Fatalf("revoke status = %d", rrec.Code)
	}
	// Access token now invalid.
	if _, err := store.AccessTokenByHash(context.Background(), hashToken(tok.AccessToken)); err == nil {
		t.Error("revoked access token still valid")
	}
}

// mustPerformAuthorize wraps performAuthorize and asserts a redirect result.
func mustPerformAuthorize(t *testing.T, o *OAuthHandlers, clientID, redirect, verifier, state string) *httptest.ResponseRecorder {
	t.Helper()
	rec := performAuthorize(t, o, clientID, redirect, verifier, state)
	if rec.Code != http.StatusFound {
		t.Fatalf("authorize status = %d body=%s", rec.Code, rec.Body.String())
	}
	return rec
}

func mustQuery(t *testing.T, location string) url.Values {
	t.Helper()
	u, err := url.Parse(location)
	if err != nil {
		t.Fatalf("parse %q: %v", location, err)
	}
	return u.Query()
}
