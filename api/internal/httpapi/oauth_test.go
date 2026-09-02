package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
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

func TestRegisterRejectsNonHTTPSRedirect(t *testing.T) {
	o, _, _ := testOAuthHandlers(t)
	body := strings.NewReader(`{"client_name":"x","redirect_uris":["http://localhost:8080/cb"]}`)
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
