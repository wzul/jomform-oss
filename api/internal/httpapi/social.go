// Social login (OAuth2 OIDC) — provider-generic. Configure via env:
//
//	JF_GOOGLE_CLIENT_ID / JF_GOOGLE_CLIENT_SECRET
//	JF_GOOGLE_REDIRECT   (default https://jomform.com/api/auth/callback/google)
//
// Cloudflare Access (SaaS OIDC app, one per team):
//
//	JF_CF_OIDC_CLIENT_ID / JF_CF_OIDC_CLIENT_SECRET
//	JF_CF_OIDC_ISSUER    (https://<TEAM>.cloudflareaccess.com)
//
// Access endpoints are derived from the issuer + client id (the app's AUD tag):
// <issuer>/cdn-cgi/access/sso/oidc/<client-id>/{authorization,token,userinfo}.
//
// Adding a provider later = add metadata + env keys (azure_ad, github, …).
package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/wzul/jomform/api/internal/auth"
)

type oidcProvider struct {
	AuthURL   string
	TokenURL  string
	UserURL   string
	Scope     string
	IDEnv     string
	SecretEnv string
}

var oidcProviders = map[string]oidcProvider{
	"google": {
		AuthURL:   "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:  "https://oauth2.googleapis.com/token",
		UserURL:   "https://openidconnect.googleapis.com/v1/userinfo",
		SecretEnv: "JF_GOOGLE_CLIENT_SECRET",
		IDEnv:     "JF_GOOGLE_CLIENT_ID",
		Scope:     "openid email profile",
	},
	"azure_ad": {
		AuthURL:   "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
		TokenURL:  "https://login.microsoftonline.com/common/oauth2/v2.0/token",
		UserURL:   "https://graph.microsoft.com/oidc/userinfo",
		IDEnv:     "JF_AZURE_CLIENT_ID",
		SecretEnv: "JF_AZURE_CLIENT_SECRET",
		Scope:     "openid email profile",
	},
	"github": {
		AuthURL:   "https://github.com/login/oauth/authorize",
		TokenURL:  "https://github.com/login/oauth/access_token",
		UserURL:   "https://api.github.com/user",
		IDEnv:     "JF_GITHUB_CLIENT_ID",
		SecretEnv: "JF_GITHUB_CLIENT_SECRET",
		Scope:     "read:user user:email",
	},
	// Cloudflare Access SaaS OIDC app: endpoints embed the client id (the
	// app's AUD tag), so URLs are built at request time in resolveProvider.
	"cloudflare": {
		IDEnv:     "JF_CF_OIDC_CLIENT_ID",
		SecretEnv: "JF_CF_OIDC_CLIENT_SECRET",
		Scope:     "openid email profile",
	},
}

// providerConfigured reports whether a provider's env config is present.
// Cloudflare Access additionally needs the client secret + team issuer.
func providerConfigured(provider string, p oidcProvider) bool {
	if os.Getenv(p.IDEnv) == "" {
		return false
	}
	if provider == "cloudflare" {
		return cfOidcConfigured()
	}
	return true
}

// resolveProvider returns p with Cloudflare Access endpoints filled in —
// its URLs embed the client id (the app's AUD tag), so they are built at
// request time instead of living in the static registry.
func resolveProvider(p oidcProvider) (oidcProvider, error) {
	if p.AuthURL != "" && p.TokenURL != "" {
		return p, nil
	}
	authURL, tokenURL, userURL, err := cfOidcEndpoints(os.Getenv(p.IDEnv))
	if err != nil {
		return p, err
	}
	p.AuthURL, p.TokenURL, p.UserURL = authURL, tokenURL, userURL
	return p, nil
}

// cfOidcIssuer returns the Cloudflare Access team issuer ("" when unset).
func cfOidcIssuer() string { return strings.TrimSpace(os.Getenv("JF_CF_OIDC_ISSUER")) }

// cfOidcConfigured reports whether the Cloudflare Access provider is active:
// client id, client secret and team issuer must all be set.
func cfOidcConfigured() bool {
	p := oidcProviders["cloudflare"]
	return os.Getenv(p.IDEnv) != "" && os.Getenv(p.SecretEnv) != "" && cfOidcIssuer() != ""
}

// cfOidcEndpoints resolves the Cloudflare Access OIDC URLs for this client.
// Per Cloudflare's SaaS app docs the paths embed the client id (AUD):
// <issuer>/cdn-cgi/access/sso/oidc/<client-id>/{authorization,token,userinfo}
func cfOidcEndpoints(clientID string) (auth, token, user string, err error) {
	issuer := strings.TrimRight(cfOidcIssuer(), "/")
	base := issuer + "/cdn-cgi/access/sso/oidc/" + clientID
	if issuer == "" || clientID == "" {
		return "", "", "", fmt.Errorf("JF_CF_OIDC_ISSUER/client id belum di set")
	}
	return base + "/authorization", base + "/token", base + "/userinfo", nil
}

// socialProvidersAvailable lists provider ids with their env config present —
// the login UI uses this to decide which buttons to render.
func socialProvidersAvailable() []string {
	out := []string{}
	for id, p := range oidcProviders {
		if id == "cloudflare" {
			if cfOidcConfigured() {
				out = append(out, id)
			}
			continue
		}
		if os.Getenv(p.IDEnv) != "" {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// socialStart redirects to the provider's OAuth authorize URL.
func (a *AuthHandlers) socialStart(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	p, ok := oidcProviders[provider]
	if !ok || !providerConfigured(provider, p) {
		writeErr(w, http.StatusNotFound, "provider tidak tersedia")
		return
	}
	p, err := resolveProvider(p)
	if err != nil {
		writeErr(w, http.StatusNotImplemented, err.Error())
		return
	}
	state := randTokenHex(16)
	http.SetCookie(w, &http.Cookie{Name: "oauth_state", Value: state, Path: "/", HttpOnly: true, MaxAge: 600, Secure: true})
	q := url.Values{}
	q.Set("client_id", os.Getenv(p.IDEnv))
	q.Set("redirect_uri", a.BaseURL+"/api/auth/callback/"+provider)
	q.Set("response_type", "code")
	q.Set("scope", p.Scope)
	q.Set("state", state)
	http.Redirect(w, r, p.AuthURL+"?"+q.Encode(), http.StatusFound)
}

// socialCallback exchanges the code, upserts identity, links to session user
// (or logs in when a link already exists).
func (a *AuthHandlers) socialCallback(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	p, ok := oidcProviders[provider]
	if !ok {
		writeErr(w, http.StatusNotFound, "provider tidak dikenali")
		return
	}
	p, rerr := resolveProvider(p)
	if rerr != nil {
		writeErr(w, http.StatusNotImplemented, rerr.Error())
		return
	}
	cs, err := r.Cookie("oauth_state")
	if err != nil || cs.Value == "" || cs.Value != r.URL.Query().Get("state") {
		writeErr(w, http.StatusBadRequest, "state tidak sah")
		return
	}
	code := r.URL.Query().Get("code")

	tok, err := exchangeCode(p, a.BaseURL+"/api/auth/callback/"+provider, code)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "token exchange gagal: "+err.Error())
		return
	}
	up, err := fetchUserInfo(p, tok)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "userinfo gagal: "+err.Error())
		return
	}

	// 1) existing identity → log the user in
	userID, err := a.Users.FindIdentity(r.Context(), provider, up.UID)
	if err == nil && userID != "" {
		_ = a.startSession(w, r, userID, "", false, false)
		// Issue #285: route through the frontend post-login resolution
		// (resolvePostLogin syncs jf_space and sends multi-space users to the
		// space-picker) instead of dropping the user on the Landing page.
		http.Redirect(w, r, "/post-login", http.StatusFound)
		return
	}

	// 2) logged-in session → LINK this provider to current user
	if uid := a.currentUserID(r); uid != "" {
		_ = a.Users.LinkIdentity(r.Context(), uid, provider, up.UID, up.Email, up.Name, up.Picture)
		http.Redirect(w, r, "/settings?linked="+provider, http.StatusFound)
		return
	}

	// 3) unauthenticated + email matches an existing user → auto-link (verify email claim)
	if up.Email != "" && up.EmailVerified {
		if existing, e2 := a.Users.ByEmail(r.Context(), up.Email); e2 == nil && existing != nil {
			_ = a.Users.LinkIdentity(r.Context(), existing.ID, provider, up.UID, up.Email, up.Name, up.Picture)
			_ = a.startSession(w, r, existing.ID, "", false, false)
			http.Redirect(w, r, "/post-login", http.StatusFound)
			return
		}
	}

	// 4) brand-new social user → create account (email scope required)
	newID, err := a.Users.CreateFromSocial(r.Context(), up.Email, up.Name, provider, up.UID, up.Picture)
	if err != nil {
		writeErr(w, http.StatusConflict, "tidak boleh cipta akaun: "+err.Error())
		return
	}
	// Account row auto-created next login via ensureAccount path
	_ = a.startSession(w, r, newID, "", false, false)
	http.Redirect(w, r, "/post-login", http.StatusFound)
}

// unlink detaches a social identity from the profile.
func (a *AuthHandlers) unlinkSocial(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	if err := a.Users.UnlinkIdentity(r.Context(), a.currentUserID(r), provider); err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "unlinked"})
}

// listSocial shows linked providers (profile manage page).
func (a *AuthHandlers) listSocial(w http.ResponseWriter, r *http.Request) {
	userID := a.currentUserID(r)
	if userID == "" {
		writeErr(w, http.StatusUnauthorized, "auth diperlukan")
		return
	}
	links, err := a.Users.ListIdentities(r.Context(), userID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	out := []map[string]any{}
	for _, l := range links {
		out = append(out, map[string]any{
			"provider": l.Provider, "email": l.Email, "name": l.DisplayName,
			"picture": l.Picture, "linked_at": l.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// socialProviders is a public (pre-login) listing of which social providers
// are configured via env, so the login page only renders live buttons.
// Returns ids + display names; contains no secrets.
func (a *AuthHandlers) socialProviders(w http.ResponseWriter, r *http.Request) {
	names := map[string]string{
		"google":     "Google",
		"azure_ad":   "Microsoft",
		"github":     "GitHub",
		"cloudflare": "Cloudflare",
	}
	out := []map[string]string{}
	for _, id := range socialProvidersAvailable() {
		out = append(out, map[string]string{"id": id, "name": names[id]})
	}
	writeJSON(w, http.StatusOK, out)
}

// ── helpers ──────────────────────────────────────────────────────────────────

type socialProfile struct {
	UID, Email, Name, Picture string
	EmailVerified             bool
}

// currentUserID resolves the current session user ("" when anonymous).
func (a *AuthHandlers) currentUserID(r *http.Request) string {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return ""
	}
	uid, issued, err := a.Sessions.GetCreated(r.Context(), c.Value)
	if err != nil {
		return ""
	}
	// Password reset invalidation (issue #62): a session issued before the
	// user's last password reset is dead.
	if u, e := a.Users.ByID(r.Context(), uid); e == nil && u != nil && u.PwResetAt != nil && !auth.ValidAfter(issued, *u.PwResetAt) {
		_ = a.Sessions.Destroy(r.Context(), c.Value)
		return ""
	}
	return uid
}

func exchangeCode(p oidcProvider, redirect, code string) (string, error) {
	form := url.Values{}
	form.Set("client_id", os.Getenv(p.IDEnv))
	form.Set("client_secret", os.Getenv(p.SecretEnv))
	form.Set("code", code)
	form.Set("grant_type", "authorization_code")
	form.Set("redirect_uri", redirect)
	resp, err := http.PostForm(p.TokenURL, form)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.AccessToken == "" {
		return "", fmt.Errorf("no access token")
	}
	return out.AccessToken, nil
}

func fetchUserInfo(p oidcProvider, token string) (*socialProfile, error) {
	req, _ := http.NewRequest(http.MethodGet, p.UserURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.Get(req.URL.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var raw struct {
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		Picture       string `json:"picture"`
		// github fallback
		Login  string `json:"login"`
		Avatar string `json:"avatar_url"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	uid := raw.Sub
	if uid == "" {
		uid = raw.Login
	}
	pic := raw.Picture
	if pic == "" {
		pic = raw.Avatar
	}
	return &socialProfile{UID: uid, Email: raw.Email, Name: raw.Name, Picture: pic, EmailVerified: raw.EmailVerified}, nil
}

var _ = strings.TrimSpace
