// "Login with Cloudflare" OAuth-style flow: one-click setup.
// JomForm is a Cloudflare OAuth app; merchant clicks Continue → approves
// scopes (zone DNS edit + account read) → we get an access token bound to
// THEIR account, use it once to add the domain routes, and store ONLY the
// refresh token sealed (per Wan: jangan tak store). No manual token paste.
package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"

	"github.com/wzul/jomform/api/internal/cryptoutil"
)

const cfAuthURL = "https://dash.cloudflare.com/oauth2/auth"
const cfTokenURL = "https://dash.cloudflare.com/oauth2/token"
const cfAPIBase = "https://api.cloudflare.com/client/v4"

func cfClientID() string     { return os.Getenv("JF_CF_OAUTH_CLIENT_ID") }
func cfClientSecret() string { return os.Getenv("JF_CF_OAUTH_CLIENT_SECRET") }

func cfOAuthConfigured() bool { return cfClientID() != "" }

// cfStart redirects the merchant to Cloudflare's consent page.
func (h *RESTHandlers) cfStart(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if !cfOAuthConfigured() {
		writeErr(w, http.StatusNotImplemented,
			"Cloudflare auto-setup belum dikonfigur (JF_CF_OAUTH_CLIENT_ID/SECRET).Tambah CNAME manual sementara.")
		return
	}
	state := randTokenHex(16)
	verifier := randTokenHex(48) // RFC 7636: 43-128 chars
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	http.SetCookie(w, &http.Cookie{Name: "cf_oauth_state", Value: state, Path: "/", HttpOnly: true, Secure: true, MaxAge: 600})
	http.SetCookie(w, &http.Cookie{Name: "cf_pkce_v", Value: verifier, Path: "/", HttpOnly: true, Secure: true, MaxAge: 600})

	q := url.Values{}
	q.Set("client_id", cfClientID())
	q.Set("redirect_uri", publicBase()+"/api/v1/cf/callback")
	q.Set("response_type", "code")
	q.Set("state", state)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	// Cloudflare app scopes: zone DNS edit + account read (set in app config;
	// requested implicitly by app registration).
	http.Redirect(w, r, cfAuthURL+"?"+q.Encode(), http.StatusFound)
}

// cfCallback exchanges code → token, provisions route via Dokploy, stores
// the sealed token so future domains auto-create records.
func (h *RESTHandlers) cfCallback(w http.ResponseWriter, r *http.Request) {
	// resolve session user (merchant must already be logged in)
	if !cfOAuthConfigured() {
		writeErr(w, http.StatusNotImplemented, "cloudflare oauth not configured")
		return
	}
	cs, err := r.Cookie("cf_oauth_state")
	if err != nil || cs.Value != r.URL.Query().Get("state") {
		writeErr(w, http.StatusBadRequest, "state tidak sah")
		return
	}
	verifierCookie, verr := r.Cookie("cf_pkce_v")
	if verr != nil || verifierCookie.Value == "" {
		writeErr(w, http.StatusBadRequest, "pkce verifier hilang")
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		writeErr(w, http.StatusBadRequest, "code hilang")
		return
	}

	form := url.Values{}
	form.Set("client_id", cfClientID())
	if cfClientSecret() != "" {
		form.Set("client_secret", cfClientSecret())
	}
	form.Set("code", code)
	form.Set("grant_type", "authorization_code")
	form.Set("redirect_uri", publicBase()+"/api/v1/cf/callback")
	form.Set("code_verifier", verifierCookie.Value)
	resp, err := http.PostForm(cfTokenURL, form)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "token exchange gagal")
		return
	}
	defer resp.Body.Close()
	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&tok)
	if tok.AccessToken == "" {
		writeErr(w, http.StatusBadGateway, "no access token")
		return
	}

	// whoami → account id
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, cfAPIBase+"/user/tokens/verify", nil)
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	verResp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "verify gagal")
		return
	}
	defer verResp.Body.Close()
	bodyJSON, _ := io.ReadAll(verResp.Body)
	var vr struct {
		Success bool `json:"success"`
		Result  struct {
			Status string `json:"status"`
		} `json:"result"`
	}
	_ = json.Unmarshal(bodyJSON, &vr)
	if !vr.Success {
		writeErr(w, http.StatusForbidden, "token tidak aktif")
		return
	}

	// Seal + persist against the logged-in account (Wan: store it).
	userID := h.aCurrentUser(r)
	if userID == "" {
		writeErr(w, http.StatusUnauthorized, "login dahulu")
		return
	}
	accs, aerr := h.Accounts.ByOwner(r.Context(), userID)
	if aerr != nil || len(accs) == 0 {
		writeErr(w, http.StatusForbidden, "tiada akaun vendor")
		return
	}
	acc := accs[0]
	sealed, _ := cryptoutil.Encrypt(h.SecretsKey, []byte(tok.AccessToken))
	if err := h.Accounts.SaveCFToken(r.Context(), acc.ID, sealed); err != nil {
		respondErr(w, err)
		return
	}
	slog.Info("cf_oauth_linked", "account", acc.ID)
	http.Redirect(w, r, "/settings?cloudflare=linked", http.StatusFound)
}

// cfStatus tells the settings UI whether Cloudflare is linked.
func (h *RESTHandlers) cfStatus(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	sealed, err := h.Accounts.CFToken(r.Context(), actx.Account.ID)
	if err != nil || sealed == "" {
		writeJSON(w, http.StatusOK, map[string]bool{"linked": false})
		return
	}
	_, derr := cryptoutil.Decrypt([]byte(sealed), string(h.SecretsKey))
	writeJSON(w, http.StatusOK, map[string]bool{"linked": derr == nil})
}

// cfDisconnect removes the stored token.
func (h *RESTHandlers) cfDisconnect(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if err := h.Accounts.SaveCFToken(r.Context(), actx.Account.ID, ""); err != nil {
		respondErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "disconnected"})
}

// aCurrentUser resolves the session user id for cfCallback ("" anon).
func (h *RESTHandlers) aCurrentUser(r *http.Request) string {
	c, err := r.Cookie("jf_session")
	if err != nil || c.Value == "" {
		return ""
	}
	if h.Sessions == nil {
		return ""
	}
	uid, gerr := h.Sessions.Get(r.Context(), c.Value)
	if gerr != nil {
		return ""
	}
	return uid
}

func publicBase() string {
	if v := os.Getenv("APP_BASE_URL"); v != "" {
		return v
	}
	return "https://jomform.com"
}

var _ = context.Background
var _ = fmt.Sprintf
var _ = slog.Info
