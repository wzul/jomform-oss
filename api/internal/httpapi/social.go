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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
	emailpkg "github.com/wzul/jomform/api/internal/email"
	"github.com/wzul/jomform/api/internal/emailqueue"
	"github.com/wzul/jomform/api/internal/httpapi/middleware"
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
		a.ensureAccountFor(r, userID, up.Email)
		_ = a.startSession(w, r, userID, "", false, false)
		// Successful social login resets the failure counters (issue #345) so
		// a user locked out on the password path isn't re-locked by stale
		// history after signing in via OAuth. Fail-open (log only, never block).
		if a.LoginLimiter != nil {
			if lerr := a.LoginLimiter.Reset(r.Context(), middleware.ClientIP(r), up.Email); lerr != nil {
				slog.Warn("loginfail_reset_error_social", "err", lerr, "ip", middleware.ClientIP(r))
			}
		}
		// Issue #285: route through the frontend post-login resolution
		// (resolvePostLogin syncs jf_workspace and sends multi-workspace users to the
		// workspace-picker) instead of dropping the user on the Landing page.
		http.Redirect(w, r, "/post-login", http.StatusFound)
		return
	}

	// 2) logged-in session → LINK this provider to current user
	if uid := a.currentUserID(r); uid != "" {
		_ = a.Users.LinkIdentity(r.Context(), uid, provider, up.UID, up.Email, up.Name, up.Picture)
		// Issue #310: connected accounts live on the Profile page (moved there
		// in #301), so the social link callback must return the user to
		// /profile instead of the old /settings redirect.
		http.Redirect(w, r, "/profile?linked="+provider, http.StatusFound)
		return
	}

	// 3) unauthenticated + email matches an existing user → require an
	//    EXPLICIT owner-confirmed connect, never a silent auto-link (issue
	//    #302). Auto-linking on email-match alone would let an attacker's
	//    OAuth silently attach to someone else's account. We email an
	//    ownership-verification link to the claimed address; only that emailed
	//    link carries the capability token. The user lands on /connect with a
	//    "check your email" step (no usable token in the URL).
	if up.Email != "" {
		if existing, e2 := a.Users.ByEmail(r.Context(), up.Email); e2 == nil && existing != nil {
			if len(a.VerifyKey) != 32 {
				writeErr(w, http.StatusInternalServerError, "internal error")
				return
			}
			ct, cerr := auth.SignConnectToken(a.VerifyKey, provider, up.UID, up.Email)
			if cerr != nil {
				writeErr(w, http.StatusInternalServerError, "internal error")
				return
			}
			link := strings.TrimRight(a.BaseURL, "/") + "/connect?token=" + url.QueryEscape(ct)
			if serr := a.sendConnectEmail(r.Context(), up.Email, link); serr != nil {
				slog.Warn("social_connect_email_send_failed", "err", serr, "email", obfuscateEmail(up.Email))
			}
			// Never place the capability token directly in the URL — only the
			// emailed link can complete the connect.
			http.Redirect(w, r, "/connect?email="+url.QueryEscape(up.Email)+"&provider="+provider+"&uid="+url.QueryEscape(up.UID), http.StatusFound)
			return
		}
	}

	// 4) brand-new social user → create account (email scope required)
	newID, err := a.Users.CreateFromSocial(r.Context(), up.Email, up.Name, provider, up.UID, up.Picture)
	if err != nil {
		writeErr(w, http.StatusConflict, "tidak boleh cipta akaun: "+err.Error())
		return
	}
	// Auto-provision the default account so a brand-new social user can reach
	// the dashboard immediately. Without it the session cookie validates
	// (/auth/me returns the user) but every authed() dashboard route
	// (/workspaces, /rest/dashboard-bundle) 401s because resolveSessionAccount
	// finds no account (issue #298). Idempotent for existing users.
	a.ensureAccountFor(r, newID, up.Email)
	_ = a.startSession(w, r, newID, "", false, false)
	// Reset failure counters on a successful social signup-login too (issue
	// #345). Fail-open (log only, never block).
	if a.LoginLimiter != nil {
		if lerr := a.LoginLimiter.Reset(r.Context(), middleware.ClientIP(r), up.Email); lerr != nil {
			slog.Warn("loginfail_reset_error_social_new", "err", lerr, "ip", middleware.ClientIP(r))
		}
	}
	http.Redirect(w, r, "/post-login", http.StatusFound)
}

// ensureAccountFor guarantees the user owns at least one account. Mirror of
// the default-account auto-provision in handleRegister + createMagicLinkUser
// (issue #278): brand-new social signups only insert a users row, so without
// this the dashboard's authed() routes 401 after Google login even though the
// session cookie is valid. Idempotent — skips when an account already exists.
func (a *AuthHandlers) ensureAccountFor(r *http.Request, userID, email string) {
	if a.Accounts == nil {
		return
	}
	accs, err := a.Accounts.ByOwner(r.Context(), userID)
	if err != nil || len(accs) > 0 {
		return
	}
	name := "Store"
	if email != "" {
		name = strings.Split(strings.ToLower(strings.TrimSpace(email)), "@")[0]
		if name == "" {
			name = "Store"
		}
	}
	acc := &dbstore.Account{
		OwnerUserID: userID,
		Name:        name + " Store",
		Slug:        slugify(strings.ToLower(strings.TrimSpace(email)), userID),
	}
	if err := a.Accounts.Create(r.Context(), acc); err != nil {
		// Slug collision: append random suffix and retry once.
		acc.Slug = acc.Slug + "-" + shortRand()
		if err2 := a.Accounts.Create(r.Context(), acc); err2 != nil {
			slog.Error("social_default_account_create", "err", err2, "user", userID)
		}
	}
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

// socialConnect confirms an explicit account connect (issue #302). The user is
// redirected here from the web /connect page after proving ownership of the
// email via the emailed connect link. The signed token binds provider+uid+email
// to the existing account — never a silent email-match auto-link.
func (a *AuthHandlers) socialConnect(w http.ResponseWriter, r *http.Request) {
	if len(a.VerifyKey) != 32 {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	var req struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Token == "" {
		writeErr(w, http.StatusBadRequest, "token diperlukan")
		return
	}
	provider, uid, email, err := auth.VerifyConnectToken(a.VerifyKey, req.Token)
	if err != nil {
		if errors.Is(err, auth.ErrConnectTokenExpired) {
			writeErr(w, http.StatusBadRequest, "pautan penghubung telah tamat tempoh")
			return
		}
		writeErr(w, http.StatusBadRequest, "pautan penghubung tidak sah")
		return
	}
	// The email must still belong to an existing account, and the provider+
	// uid from the token must not already be linked to a different user.
	existing, eerr := a.Users.ByEmail(r.Context(), email)
	if eerr != nil || existing == nil {
		writeErr(w, http.StatusBadRequest, "akaun tidak dijumpai")
		return
	}
	if linkedTo, ferr := a.Users.FindIdentity(r.Context(), provider, uid); ferr == nil && linkedTo != "" && linkedTo != existing.ID {
		writeErr(w, http.StatusConflict, "identiti sudah dipautkan ke akaun lain")
		return
	}
	if err := a.Users.LinkIdentity(r.Context(), existing.ID, provider, uid, email, "", ""); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	a.ensureAccountFor(r, existing.ID, email)
	if err := a.startSession(w, r, existing.ID, "", false, false); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "connected"})
}

// connectResend re-emits the ownership-verification email for a pending social
// connect (issue #302). The email's signed ConnectToken is regenerated. Always
// replies generically so we never disclose anything about the address or
// whether a link was delivered.
func (a *AuthHandlers) connectResend(w http.ResponseWriter, r *http.Request) {
	defer writeJSON(w, http.StatusOK, map[string]string{"status": "sent"})
	if len(a.VerifyKey) != 32 {
		return
	}
	var req struct {
		Email    string `json:"email"`
		Provider string `json:"provider"`
		UID      string `json:"uid"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Email == "" || req.Provider == "" || req.UID == "" {
		return
	}
	if !auth.ValidEmail(req.Email) {
		return
	}
	ct, cerr := auth.SignConnectToken(a.VerifyKey, req.Provider, req.UID, req.Email)
	if cerr != nil {
		return
	}
	link := strings.TrimRight(a.BaseURL, "/") + "/connect?token=" + url.QueryEscape(ct)
	// The capability token only travels by email — never echoed in the response.
	if err := a.sendConnectEmail(r.Context(), req.Email, link); err != nil {
		slog.Warn("social_connect_resend_email_failed", "err", err, "email", obfuscateEmail(req.Email))
	}
}

// sendConnectEmail delivers the "link your account" verification link (issue
// #302). Issue #199: enqueued when a queue is wired; otherwise sent
// synchronously via VerifyMailer.
func (a *AuthHandlers) sendConnectEmail(ctx context.Context, email, link string) error {
	if a.VerifyMailer == nil {
		return errors.New("connect mailer not configured")
	}
	if a.Suppressions != nil {
		if sup, serr := emailpkg.ShouldSuppress(ctx, a.Suppressions, email); serr != nil || sup {
			// Suppressed address never receives a connect email.
			return errors.New("connect email suppressed")
		}
	}
	subject := "Pautan penghubung akaun JomForm anda"
	body := "Sila sahkan alamat emel anda untuk menghubungkan akaun log masuk sosial anda ke akaun JomForm anda.\n\n" +
		"Tap pautan berikut (sah selama 24 jam):\n\n" +
		link + "\n\n" +
		"Jika anda tidak meminta ini, anda boleh abaikan emel ini.\n"
	if a.EmailQueue != nil {
		if err := a.EmailQueue.Enqueue(ctx, emailqueue.Job{
			Type: "connect", To: email, Subject: subject, Body: body,
		}); err != nil {
			return err
		}
		return nil
	}
	if err := a.VerifyMailer.Send(email, subject, body); err != nil {
		return err
	}
	recordSESSend("connect")
	if a.Breaker != nil {
		if err := a.Breaker.RecordSend(ctx); err != nil {
			slog.Warn("breaker_record_send_connect", "err", err)
		}
	}
	return nil
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
