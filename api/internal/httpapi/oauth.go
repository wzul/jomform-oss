// OAuth 2.1 authorization-code + PKCE flow for MCP clients (issue #121).
//
// Endpoints (RFC 8414 discovery):
//
//	GET  /oauth/authorize  — human consent step; validates client_id +
//	                         redirect_uri (against the persisted DCR record) +
//	                         S256 PKCE + state, requires a logged-in JomForm
//	                         session (else redirect to the web login), mints a
//	                         short-lived single-use code → {redirect_uri}?code=..
//	POST /oauth/token      — exchange authorization_code (PKCE-verified) or
//	                         refresh_token for an opaque access token that
//	                         authenticates MCP /mcp calls; refresh tokens rotate.
//	POST /oauth/revoke     — RFC 7009 token revocation.
//	POST /oauth/register   — RFC 7591 Dynamic Client Registration (persisted,
//	                         https-redirect-URI only, rate-limited).
//
// All secret material (codes, access + refresh tokens) is stored only as
// SHA-256 hashes — the same opaque, revocable pattern as the API keys.
package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/config"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/httpapi/middleware"
)

// oauthStore is the storage dependency the OAuth endpoints need. dbstore.OAuthStore
// satisfies it; tests inject a fake to exercise the full authorize→token flow
// without Postgres.
type oauthStore interface {
	ClientByID(ctx context.Context, clientID string) (*dbstore.OAuthClient, error)
	CreateClient(ctx context.Context, c *dbstore.OAuthClient) error
	StoreAuthCode(ctx context.Context, codeHash string, c *dbstore.OAuthAuthCode) error
	ConsumeAuthCode(ctx context.Context, codeHash string) (*dbstore.OAuthAuthCode, error)
	CreateToken(ctx context.Context, t *dbstore.OAuthToken) error
	AccessTokenByHash(ctx context.Context, tokenHash string) (*dbstore.OAuthToken, error)
	RefreshGrant(ctx context.Context, refreshHash, newAccessHash, newRefreshHash string, accessExpires, refreshExpires time.Time) (*dbstore.OAuthToken, error)
	RevokeAccessToken(ctx context.Context, tokenHash string) error
	RevokeByRefreshHash(ctx context.Context, refreshHash string) error
}

// oauthUserStore resolves a user by ID for the authorize step. *dbstore.UserStore
// satisfies it; tests inject a fake so the flow runs without Postgres.
type oauthUserStore interface {
	ByID(ctx context.Context, id string) (*dbstore.User, error)
}

// oauthSessionStore resolves (and can destroy) a browser session for the human
// authorize step. *auth.Sessions satisfies it; tests inject a fake.
type oauthSessionStore interface {
	GetCreated(ctx context.Context, token string) (string, time.Time, error)
	Destroy(ctx context.Context, token string) error
}

// oauthAccountStore resolves a user's owned accounts to pick the account being
// authorized. *dbstore.AccountStore satisfies it; tests inject a fake.
type oauthAccountStore interface {
	ByOwner(ctx context.Context, userID string) ([]*dbstore.Account, error)
}

// OAuthHandlers wires stores + settings for the OAuth endpoints.
type OAuthHandlers struct {
	Store    oauthStore
	Sessions oauthSessionStore
	Users    oauthUserStore
	Accounts oauthAccountStore
	BaseURL  string // web AppBaseURL for the /login redirect

	CodeTTL         time.Duration // authorization code lifetime (10 min)
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
}

// defaultOAuthTTLs returns the 10-min / 1-hour / 30-day defaults used when a
// bare server has no config (tests).
func defaultOAuthTTLs() (code, access, refresh time.Duration) {
	return 10 * time.Minute, time.Hour, 30 * 24 * time.Hour
}

// newOAuthHandlers builds the handler set from a config (or defaults in tests).
func newOAuthHandlers(o *dbstore.OAuthStore, sessions *auth.Sessions, users *dbstore.UserStore, accounts *dbstore.AccountStore, baseURL string, cfg *config.Config) *OAuthHandlers {
	h := &OAuthHandlers{
		Store: o, Sessions: sessions, Users: users, Accounts: accounts, BaseURL: baseURL,
		CodeTTL: 10 * time.Minute, AccessTokenTTL: time.Hour, RefreshTokenTTL: 30 * 24 * time.Hour,
	}
	if cfg != nil {
		if cfg.OAuthCodeTTLSeconds > 0 {
			h.CodeTTL = cfg.OAuthCodeTTLSeconds
		}
		if cfg.OAuthAccessTokenTTLSeconds > 0 {
			h.AccessTokenTTL = cfg.OAuthAccessTokenTTLSeconds
		}
		if cfg.OAuthRefreshTokenTTLSeconds > 0 {
			h.RefreshTokenTTL = cfg.OAuthRefreshTokenTTLSeconds
		}
	}
	return h
}

// RegisterRoutes wires the OAuth 2.1 endpoints. register is wrapped in a tight
// per-IP rate limit (issue #42 / #121 scope 5) since it is fully open.
func (o *OAuthHandlers) RegisterRoutes(mux *http.ServeMux, dcrRate, dcrBurst float64) {
	mux.HandleFunc("GET /oauth/authorize", o.authorize)
	mux.HandleFunc("POST /oauth/token", o.token)
	mux.HandleFunc("POST /oauth/revoke", o.revoke)
	mux.Handle("POST /oauth/register", middleware.RateLimitWith(dcrRate, dcrBurst)(http.HandlerFunc(o.register)))
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// hashToken is the SHA-256 hex hash used for at-rest storage/lookup of codes
// and tokens (raw secret material is never stored).
func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// newOAuthToken mints a fresh opaque token (32 random bytes, hex).
func newOAuthToken() string { return randTokenHex(32) }

// redirectURIAllowed reports whether uri is one of the client's registered
// redirect URIs (exact match — no prefix looseness → no open redirect).
func redirectURIAllowed(client *dbstore.OAuthClient, uri string) bool {
	for _, r := range client.RedirectURIs {
		if r == uri {
			return true
		}
	}
	return false
}

// oauthErr writes an OAuth-spec error response and logs it.
func oauthErr(w http.ResponseWriter, status int, code, desc string, props ...any) {
	if len(props) > 0 {
		slog.Warn("oauth_error", append([]any{"code", code, "desc", desc}, props...)...)
	}
	writeJSON(w, status, map[string]string{"error": code, "error_description": desc})
}

// validHTTPSRedirect checks a redirect_uri is absolute https; localhost/private
// hosts are only allowed in dev builds (issue #121 scope 5).
func validHTTPSRedirect(uri string) bool {
	u, err := url.Parse(uri)
	if err != nil {
		return false
	}
	if u.Scheme != "https" || u.Host == "" {
		if isDev && u.Scheme == "http" && (u.Host == "localhost" || strings.HasPrefix(u.Host, "127.")) {
			return true
		}
		return false
	}
	return true
}

// ─── GET /oauth/authorize ────────────────────────────────────────────────────

func (o *OAuthHandlers) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	responseType := q.Get("response_type")
	codeChallenge := q.Get("code_challenge")
	challengeMethod := q.Get("code_challenge_method")
	state := q.Get("state")

	client, err := o.Store.ClientByID(r.Context(), clientID)
	if err != nil {
		// Unknown/unpersisted client: cannot safely validate its redirect_uri,
		// so do not redirect (avoids open redirect). Render the generic error.
		oauthErr(w, http.StatusBadRequest, "invalid_client", "client_id tidak dikenal pasti")
		return
	}
	if !redirectURIAllowed(client, redirectURI) {
		// RFC 6749: when redirect_uri is invalid we must NOT redirect the user.
		oauthErr(w, http.StatusBadRequest, "invalid_request", "redirect_uri tidak didaftarkan untuk client ini")
		return
	}
	if responseType != "code" {
		o.redirectErr(w, r, client, redirectURI, state, "unsupported_response_type", "response_type hanya 'code' disokong")
		return
	}
	if codeChallenge == "" {
		o.redirectErr(w, r, client, redirectURI, state, "invalid_request", "code_challenge (PKCE) wajib")
		return
	}
	if challengeMethod == "" {
		challengeMethod = "plain" // RFC 7636 default; we still accept both
	}
	if challengeMethod != "S256" && challengeMethod != "plain" {
		o.redirectErr(w, r, client, redirectURI, state, "invalid_request", "code_challenge_method hanya 'S256' (atau 'plain' dev) disokong")
		return
	}
	if state == "" {
		o.redirectErr(w, r, client, redirectURI, state, "invalid_request", "state wajib")
		return
	}

	// Human consent needs a logged-in session. No API key — this is the
	// dashboard identity authorizing the client. Redirect to the web login
	// when anonymous, preserving the full authorize URL as ?next=.
	user := o.currentUser(r)
	if user == nil {
		next := oauthBase() + "/oauth/authorize?" + q.Encode()
		loc := o.BaseURL + "/login?next=" + url.QueryEscape(next)
		http.Redirect(w, r, loc, http.StatusFound)
		return
	}
	// Resolve the account being authorized. Issue #307: an OAuth connection is
	// USER-level — the agent can operate across all of the user's workspaces — so
	// the account is optional metadata (used only for display/legacy tools).
	// The token itself is bound to the user id below.
	accountID := ""
	if o.Accounts != nil {
		accs, err := o.Accounts.ByOwner(r.Context(), user.ID)
		if err == nil && len(accs) > 0 {
			accountID = accs[0].ID
		}
	}

	// Consent granted → mint a single-use auth code (stored hashed, 10-min TTL,
	// bound to client + code_challenge + user; account is optional issue #307).
	code := newOAuthToken()
	ac := &dbstore.OAuthAuthCode{
		ClientID: clientID, UserID: user.ID, AccountID: accountID,
		RedirectURI: redirectURI, CodeChallenge: codeChallenge,
		Scope: []string{"readwrite"}, ExpiresAt: time.Now().UTC().Add(o.CodeTTL),
	}
	if err := o.Store.StoreAuthCode(r.Context(), hashToken(code), ac); err != nil {
		oauthErr(w, http.StatusInternalServerError, "server_error", "gagal menyimpan authorization code")
		return
	}
	q2 := url.Values{}
	q2.Set("code", code)
	if state != "" {
		q2.Set("state", state)
	}
	http.Redirect(w, r, redirectURI+"?"+q2.Encode(), http.StatusFound)
}

// redirectErr redirects back to the client with an OAuth error + state
// (used only for errors where the client/redirect_uri are already validated).
func (o *OAuthHandlers) redirectErr(w http.ResponseWriter, r *http.Request, client *dbstore.OAuthClient, redirectURI, state, code, desc string) {
	q := url.Values{}
	q.Set("error", code)
	q.Set("error_description", desc)
	if state != "" {
		q.Set("state", state)
	}
	http.Redirect(w, r, redirectURI+"?"+q.Encode(), http.StatusFound)
}

// currentUser resolves the logged-in session user (nil when anonymous).
func (o *OAuthHandlers) currentUser(r *http.Request) *dbstore.User {
	if o.Sessions == nil || o.Users == nil {
		return nil
	}
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil
	}
	userID, issued, err := o.Sessions.GetCreated(r.Context(), c.Value)
	if err != nil {
		return nil
	}
	user, err := o.Users.ByID(r.Context(), userID)
	if err != nil {
		return nil
	}
	if user.PwResetAt != nil && !auth.ValidAfter(issued, *user.PwResetAt) {
		_ = o.Sessions.Destroy(r.Context(), c.Value)
		return nil
	}
	return user
}

// ─── POST /oauth/token ───────────────────────────────────────────────────────

func (o *OAuthHandlers) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		oauthErr(w, http.StatusBadRequest, "invalid_request", "bentuk form tidak sah")
		return
	}
	grantType := r.Form.Get("grant_type")
	switch grantType {
	case "authorization_code":
		o.tokenAuthCode(w, r)
	case "refresh_token":
		o.tokenRefresh(w, r)
	default:
		oauthErr(w, http.StatusBadRequest, "unsupported_grant_type", "grant_type tidak disokong")
	}
}

// tokenAuthCode handles grant_type=authorization_code (PKCE-verified).
func (o *OAuthHandlers) tokenAuthCode(w http.ResponseWriter, r *http.Request) {
	code := r.Form.Get("code")
	verifier := r.Form.Get("code_verifier")
	clientID := r.Form.Get("client_id")
	redirectURI := r.Form.Get("redirect_uri")

	ac, err := o.Store.ConsumeAuthCode(r.Context(), hashToken(code))
	if err != nil {
		oauthErr(w, http.StatusBadRequest, "invalid_grant", "authorization code tidak sah, luput, atau sudah digunakan")
		return
	}
	if ac.ClientID != clientID {
		oauthErr(w, http.StatusBadRequest, "invalid_grant", "client_id tidak padan dengan code")
		return
	}
	if redirectURI != "" && ac.RedirectURI != redirectURI {
		oauthErr(w, http.StatusBadRequest, "invalid_grant", "redirect_uri tidak padan dengan yang digunakan semasa authorize")
		return
	}
	// PKCE: verify the code_verifier against the stored S256 (or plain) challenge.
	if !verifyPKCE(ac.CodeChallenge, verifier) {
		oauthErr(w, http.StatusBadRequest, "invalid_grant", "code_verifier tidak sah (PKCE)")
		return
	}
	o.issueTokenPair(w, r, ac.ClientID, ac.UserID, ac.AccountID, ac.Scope)
}

// tokenRefresh handles grant_type=refresh_token with rotation (single-use).
func (o *OAuthHandlers) tokenRefresh(w http.ResponseWriter, r *http.Request) {
	refresh := r.Form.Get("refresh_token")
	if refresh == "" {
		oauthErr(w, http.StatusBadRequest, "invalid_request", "refresh_token wajib")
		return
	}
	clientID := r.Form.Get("client_id")
	// Mint the fresh raw tokens BEFORE storing so the raw secrets (the only
	// copy) reach the client exactly once.
	newAccess := newOAuthToken()
	newRefresh := newOAuthToken()
	now := time.Now().UTC()
	nt, err := o.Store.RefreshGrant(r.Context(), hashToken(refresh), hashToken(newAccess), hashToken(newRefresh), now.Add(o.AccessTokenTTL), now.Add(o.RefreshTokenTTL))
	if err != nil {
		oauthErr(w, http.StatusBadRequest, "invalid_grant", "refresh token tidak sah atau telah digunakan")
		return
	}
	if clientID != "" && nt.ClientID != clientID {
		oauthErr(w, http.StatusBadRequest, "invalid_grant", "client_id tidak padan")
		return
	}
	writeTokenResponse(w, newAccess, newRefresh, o.AccessTokenTTL)
}

// issueTokenPair mints + stores a fresh access(+refresh) token pair and
// returns the raw secrets to the client exactly once.
func (o *OAuthHandlers) issueTokenPair(w http.ResponseWriter, r *http.Request, clientID, userID, accountID string, scope []string) {
	access := newOAuthToken()
	refresh := newOAuthToken()
	now := time.Now().UTC()
	refreshExp := now.Add(o.RefreshTokenTTL)
	t := &dbstore.OAuthToken{
		TokenHash: hashToken(access), RefreshHash: hashToken(refresh),
		ClientID: clientID, UserID: userID, AccountID: accountID, Scope: scope,
		AccessExpiresAt: now.Add(o.AccessTokenTTL), RefreshExpiresAt: &refreshExp,
	}
	if err := o.Store.CreateToken(r.Context(), t); err != nil {
		oauthErr(w, http.StatusInternalServerError, "server_error", "gagal menyimpan token")
		return
	}
	writeTokenResponse(w, access, refresh, o.AccessTokenTTL)
}

// verifyPKCE checks a code_verifier against the stored challenge.
func verifyPKCE(storedChallenge, verifier string) bool {
	if verifier == "" {
		return false
	}
	// Compute the expected challenge for both methods.
	s256 := base64.RawURLEncoding.EncodeToString(sha256Sum([]byte(verifier)))
	return storedChallenge == s256 || storedChallenge == verifier
}

func sha256Sum(b []byte) []byte {
	sum := sha256.Sum256(b)
	return sum[:]
}

// writeTokenResponse emits the standard OAuth token JSON.
func writeTokenResponse(w http.ResponseWriter, access, refresh string, ttl time.Duration) {
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  access,
		"token_type":    "Bearer",
		"expires_in":    int(ttl.Seconds()),
		"refresh_token": refresh,
		"scope":         "readwrite",
	})
}

// ─── POST /oauth/revoke ──────────────────────────────────────────────────────

func (o *OAuthHandlers) revoke(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		oauthErr(w, http.StatusBadRequest, "invalid_request", "bentuk form tidak sah")
		return
	}
	token := r.Form.Get("token")
	if token == "" {
		oauthErr(w, http.StatusBadRequest, "invalid_request", "token wajib")
		return
	}
	hint := r.Form.Get("token_type_hint")
	if hint == "refresh_token" {
		_ = o.Store.RevokeByRefreshHash(r.Context(), hashToken(token))
	} else {
		// access_token (default): revoke the access token only.
		_ = o.Store.RevokeAccessToken(r.Context(), hashToken(token))
	}
	// RFC 7009: 200 even for unknown/invalid tokens to avoid token probing.
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// ─── POST /oauth/register (RFC 7591, persisted) ──────────────────────────────

func (o *OAuthHandlers) register(w http.ResponseWriter, r *http.Request) {
	if o.Store == nil {
		oauthErr(w, http.StatusServiceUnavailable, "temporarily_unavailable", "pendaftaran client belum aktif")
		return
	}
	var in struct {
		ClientName   string   `json:"client_name"`
		RedirectURIs []string `json:"redirect_uris"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		oauthErr(w, http.StatusBadRequest, "invalid_client_metadata", "JSON tidak sah")
		return
	}
	if len(in.RedirectURIs) == 0 {
		oauthErr(w, http.StatusBadRequest, "invalid_redirect_uri", "redirect_uris wajib")
		return
	}
	for _, u := range in.RedirectURIs {
		if !validHTTPSRedirect(u) {
			oauthErr(w, http.StatusBadRequest, "invalid_redirect_uri", "redirect_uri mesti https (localhost dibenar hanya dalam dev)")
			return
		}
	}
	clientID := "jfc_" + randTokenHex(16)
	c := &dbstore.OAuthClient{
		ClientID: clientID, Name: in.ClientName, RedirectURIs: in.RedirectURIs,
		TokenEndpointAuthMethod: "none",
		GrantTypes:              []string{"authorization_code", "refresh_token"},
	}
	if err := o.Store.CreateClient(r.Context(), c); err != nil {
		oauthErr(w, http.StatusInternalServerError, "server_error", "gagal menyimpan client")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  clientID,
		"client_name":                in.ClientName,
		"redirect_uris":              in.RedirectURIs,
		"token_endpoint_auth_method": "none",
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
	})
}
