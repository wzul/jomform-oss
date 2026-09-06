// "Login with Cloudflare" OAuth flow (issue #22 — restored after #15 Option B
// removed too much). JomForm is a Cloudflare OAuth app; the merchant approves
// Zone:Read + DNS:Edit, we store the token pair sealed, and the custom-domain
// flow uses it to auto-create the CNAME record. No tunnel/connector code
// lives here — that flow is dead permanently (see #15).
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
	"strings"
	"time"

	"github.com/wzul/jomform/api/internal/cryptoutil"
)

const (
	cfAuthURL  = "https://dash.cloudflare.com/oauth2/auth"
	cfTokenURL = "https://dash.cloudflare.com/oauth2/token"
	cfAPIBase  = "https://api.cloudflare.com/client/v4"
	// cfOAuthScope is the Cloudflare OAuth scope set requested on the consent
	// screen. Without a scope param Cloudflare shows "0 total permissions" and
	// disables the Authorize button (issue #288). For auto-CNAME (#22) we need:
	//   zone.read  — list zones (resolve the CNAME target zone)
	//   dns.write  — create/edit the CNAME record
	// NOTE: Cloudflare OAuth scope IDs use dot-notation (zone.read, dns.write,
	// account.read). The old colon form (zone:read dns:edit) was invalid and
	// Cloudflare rejected every flow with "OAuth 2.0 Client is not allowed to
	// request scope 'zone:read'". account.read is not required for the zone/DNS
	// calls below, so it is omitted.
	cfOAuthScope = "zone.read dns.write"
)

func cfClientID() string     { return os.Getenv("JF_CF_OAUTH_CLIENT_ID") }
func cfClientSecret() string { return os.Getenv("JF_CF_OAUTH_CLIENT_SECRET") }

func cfOAuthConfigured() bool { return cfClientID() != "" }

// publicBase is the externally-reachable origin, used as the OAuth
// redirect_uri. Must match the CF app registration exactly.
func publicBase() string {
	if v := os.Getenv("APP_BASE_URL"); v != "" {
		return v
	}
	return "https://jomform.com"
}

// cfRedirectURI is the OAuth redirect target — served through the dashboard
// proxy as /api/cf/callback → Go /v1/cf/callback. Identical value in BOTH the
// authorize URL and the token exchange, or CF rejects with invalid_redirect.
func cfRedirectURI() string { return publicBase() + "/api/cf/callback" }

// cfCredKind distinguishes how the stored Cloudflare credential authenticates.
type cfCredKind string

const (
	cfKindOAuth     cfCredKind = "oauth"     // OAuth access+refresh token pair
	cfKindAPIToken  cfCredKind = "apitoken"  // manual API token (Bearer)
	cfKindGlobalKey cfCredKind = "globalkey" // manual global API key (X-Auth-Email/Key)
)

// cfCred is the persisted (after sealing) Cloudflare credential blob. The
// same accounts.cf_token_enc column stores all kinds; cfAPICall branches on
// Kind to pick the right auth headers. Manual tokens/keys never auto-refresh
// (they persist until the merchant revokes them) — only kind=oauth refreshes.
type cfCred struct {
	Kind         cfCredKind `json:"kind"`
	Access       string     `json:"access,omitempty"`        // oauth access token OR api token OR global key
	Refresh      string     `json:"refresh,omitempty"`       // oauth refresh token only
	AccountEmail string     `json:"account_email,omitempty"`  // manual kinds: the CF account email
	AccountName  string     `json:"account_name,omitempty"`   // optional label / zones hint
}

// cfTokenPair is the OAuth token set returned by the token endpoint. Access
// tokens live ~1h; the refresh token is what keeps the link alive.
type cfTokenPair struct {
	Access  string `json:"access"`
	Refresh string `json:"refresh"`
}

// cfStart redirects the merchant to Cloudflare's consent page.
func (h *RESTHandlers) cfStart(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if !cfOAuthConfigured() {
		writeErr(w, http.StatusNotImplemented,
			"Cloudflare auto-setup belum dikonfigur (JF_CF_OAUTH_CLIENT_ID/SECRET). Tambah CNAME manual sementara.")
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
	q.Set("redirect_uri", cfRedirectURI())
	q.Set("response_type", "code")
	q.Set("state", state)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("scope", cfOAuthScope)
	http.Redirect(w, r, cfAuthURL+"?"+q.Encode(), http.StatusFound)
}

// cfCallback exchanges code → token pair, verifies the token, seals both
// access and refresh tokens against the logged-in account.
func (h *RESTHandlers) cfCallback(w http.ResponseWriter, r *http.Request) {
	if !cfOAuthConfigured() {
		writeErr(w, http.StatusNotImplemented, "cloudflare oauth not configured")
		return
	}
	cs, err := r.Cookie("cf_oauth_state")
	if err != nil || cs.Value == "" || cs.Value != r.URL.Query().Get("state") {
		writeErr(w, http.StatusBadRequest, "state tidak sah")
		return
	}
	verifierCookie, verr := r.Cookie("cf_pkce_v")
	if verr != nil || verifierCookie.Value == "" {
		writeErr(w, http.StatusBadRequest, "pkce verifier hilang")
		return
	}
	// On a failed OAuth flow Cloudflare redirects back with error /
	// error_description instead of code (e.g. invalid_scope). Surface the real
	// cause rather than the misleading "code hilang" (issue #315).
	if oauthErr := r.URL.Query().Get("error"); oauthErr != "" {
		desc := r.URL.Query().Get("error_description")
		if desc == "" {
			desc = oauthErr
		}
		writeErr(w, http.StatusBadRequest, "Cloudflare OAuth ditolak: "+desc)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		writeErr(w, http.StatusBadRequest, "code hilang")
		return
	}

	tok, err := cfExchangeCode(r.Context(), code, verifierCookie.Value)
	if err != nil {
		slog.Error("cf_callback_exchange", "err", err)
		writeErr(w, http.StatusBadGateway, "token exchange gagal")
		return
	}
	if tok.Access == "" {
		writeErr(w, http.StatusBadGateway, "no access token")
		return
	}
	if err := cfVerifyToken(r.Context(), tok.Access); err != nil {
		writeErr(w, http.StatusForbidden, "token tidak aktif")
		return
	}

	// Seal + persist against the logged-in account (Wan: jangan tak store).
	uid := h.sessionOwnerID(r)
	if uid == "" {
		writeErr(w, http.StatusUnauthorized, "login dahulu")
		return
	}
	accs, aerr := h.Accounts.ByOwner(r.Context(), uid)
	if aerr != nil || len(accs) == 0 {
		writeErr(w, http.StatusForbidden, "tiada akaun vendor")
		return
	}
	blob, _ := json.Marshal(cfCred{Kind: cfKindOAuth, Access: tok.Access, Refresh: tok.Refresh})
	sealed, err := cryptoutil.Encrypt(h.SecretsKey, blob)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	if err := h.Accounts.SaveCFToken(r.Context(), accs[0].ID, sealed); err != nil {
		respondErr(w, r, err)
		return
	}
	slog.Info("cf_oauth_linked", "account", accs[0].ID)
	http.Redirect(w, r, "/settings?cloudflare=linked", http.StatusFound)
}

// cfStatus tells the settings UI whether Cloudflare is linked (decryptable
// token present). The swapped Decrypt-arg bug from the original flow is gone:
// key first, sealed second.
func (h *RESTHandlers) cfStatus(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	linked := false
	if sealed, err := h.Accounts.CFToken(r.Context(), actx.Account.ID); err == nil && sealed != "" {
		if _, derr := cfUnseal(h.SecretsKey, sealed); derr == nil {
			linked = true
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"linked": linked, "configured": cfOAuthConfigured()})
}

// cfDisconnect removes the stored token.
func (h *RESTHandlers) cfDisconnect(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	if err := h.Accounts.SaveCFToken(r.Context(), actx.Account.ID, ""); err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "disconnected"})
}

// cfConnectAPIToken links Cloudflare via a manual API token (issue #82). The
// token is validated against CF at save time (GET /user/tokens/verify) so an
// invalid/expired token is rejected immediately with a clear 400, never
// discovered later when the auto-CNAME fails. The token is sealed and never
// returned to the client.
func (h *RESTHandlers) cfConnectAPIToken(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	var in struct {
		AccountEmail string `json:"account_email"`
		APIToken     string `json:"api_token"`
		AccountName  string `json:"account_name"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "payload tidak sah")
		return
	}
	in.AccountEmail = strings.TrimSpace(in.AccountEmail)
	in.APIToken = strings.TrimSpace(in.APIToken)
	if in.AccountEmail == "" || in.APIToken == "" {
		writeErr(w, http.StatusBadRequest, "account email dan API token wajib")
		return
	}
	if err := cfVerifyAPIToken(r.Context(), in.APIToken); err != nil {
		writeErr(w, http.StatusBadRequest, "API token tidak sah atau tamat tempoh: "+err.Error())
		return
	}
	blob, _ := json.Marshal(cfCred{Kind: cfKindAPIToken, Access: in.APIToken, AccountEmail: in.AccountEmail, AccountName: in.AccountName})
	sealed, err := cryptoutil.Encrypt(h.SecretsKey, blob)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	if err := h.Accounts.SaveCFToken(r.Context(), actx.Account.ID, sealed); err != nil {
		respondErr(w, r, err)
		return
	}
	slog.Info("cf_api_token_linked", "account", actx.Account.ID)
	writeJSON(w, http.StatusOK, map[string]bool{"linked": true})
}

// cfConnectGlobalKey links Cloudflare via a manual global API key (issue #82).
// Validated at save time via GET /user (X-Auth-Email/X-Auth-Key); the key is
// sealed and never returned to the client.
func (h *RESTHandlers) cfConnectGlobalKey(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	var in struct {
		AccountEmail string `json:"account_email"`
		APIKey       string `json:"api_key"`
		AccountName  string `json:"account_name"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "payload tidak sah")
		return
	}
	in.AccountEmail = strings.TrimSpace(in.AccountEmail)
	in.APIKey = strings.TrimSpace(in.APIKey)
	if in.AccountEmail == "" || in.APIKey == "" {
		writeErr(w, http.StatusBadRequest, "account email dan global API key wajib")
		return
	}
	if err := cfVerifyGlobalKey(r.Context(), in.AccountEmail, in.APIKey); err != nil {
		writeErr(w, http.StatusBadRequest, "global API key tidak sah: "+err.Error())
		return
	}
	blob, _ := json.Marshal(cfCred{Kind: cfKindGlobalKey, Access: in.APIKey, AccountEmail: in.AccountEmail, AccountName: in.AccountName})
	sealed, err := cryptoutil.Encrypt(h.SecretsKey, blob)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	if err := h.Accounts.SaveCFToken(r.Context(), actx.Account.ID, sealed); err != nil {
		respondErr(w, r, err)
		return
	}
	slog.Info("cf_global_key_linked", "account", actx.Account.ID)
	writeJSON(w, http.StatusOK, map[string]bool{"linked": true})
}

// cfVerifyAPIToken confirms a manual API token works by calling CF's
// /user/tokens/verify endpoint (Bearer auth).
func cfVerifyAPIToken(ctx context.Context, token string) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, cfAPIBase+"/user/tokens/verify", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("token ditolak oleh Cloudflare")
	}
	var vr struct {
		Success bool `json:"success"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&vr)
	if !vr.Success {
		return fmt.Errorf("token tidak sah")
	}
	return nil
}

// cfVerifyGlobalKey confirms a global API key works by calling CF's /user
// endpoint with X-Auth-Email/X-Auth-Key headers.
func cfVerifyGlobalKey(ctx context.Context, email, key string) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, cfAPIBase+"/user", nil)
	req.Header.Set("X-Auth-Email", email)
	req.Header.Set("X-Auth-Key", key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("email atau key ditolak oleh Cloudflare")
	}
	var ur struct {
		Success bool `json:"success"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&ur)
	if !ur.Success {
		return fmt.Errorf("email atau key tidak sah")
	}
	return nil
}

// ─── CF API client helpers (shared with the domain auto-CNAME flow) ─────────

func cfUnseal(key []byte, sealed string) (*cfCred, error) {
	plain, err := cryptoutil.Decrypt(key, sealed)
	if err != nil {
		return nil, err
	}
	var cred cfCred
	if err := json.Unmarshal(plain, &cred); err != nil {
		return nil, err
	}
	// Backward compat: blobs sealed before issue #82 have no "kind" field —
	// they are OAuth token pairs. Default to oauth so existing links keep
	// working unchanged.
	if cred.Kind == "" {
		cred.Kind = cfKindOAuth
	}
	return &cred, nil
}

// cfExchangeCode swaps the OAuth code for the access+refresh token pair.
func cfExchangeCode(ctx context.Context, code, verifier string) (*cfTokenPair, error) {
	form := url.Values{}
	form.Set("client_id", cfClientID())
	if cfClientSecret() != "" {
		form.Set("client_secret", cfClientSecret())
	}
	form.Set("code", code)
	form.Set("grant_type", "authorization_code")
	form.Set("redirect_uri", cfRedirectURI())
	form.Set("code_verifier", verifier)
	return cfTokenRequest(ctx, form)
}

// cfRefresh swaps a refresh token for a fresh pair.
func cfRefresh(ctx context.Context, refreshToken string) (*cfTokenPair, error) {
	form := url.Values{}
	form.Set("client_id", cfClientID())
	if cfClientSecret() != "" {
		form.Set("client_secret", cfClientSecret())
	}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	return cfTokenRequest(ctx, form)
}

func cfTokenRequest(ctx context.Context, form url.Values) (*cfTokenPair, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var tok struct {
		AccessToken      string `json:"access_token"`
		RefreshToken     string `json:"refresh_token"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	if tok.Error != "" {
		return nil, fmt.Errorf("cf oauth: %s: %s", tok.Error, tok.ErrorDescription)
	}
	return &cfTokenPair{Access: tok.AccessToken, Refresh: tok.RefreshToken}, nil
}

func cfVerifyToken(ctx context.Context, accessToken string) error {
	// GET /user with a Bearer OAuth access token confirms the token is valid
	// and active. Do NOT use /user/tokens/verify — that endpoint only accepts
	// Cloudflare API tokens (not OAuth access tokens) and returns success=false
	// ("inactive") for an otherwise-valid OAuth token.
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, cfAPIBase+"/user", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("token ditolak oleh Cloudflare")
	}
	var vr struct {
		Success bool `json:"success"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&vr)
	if !vr.Success {
		return fmt.Errorf("cf token verify: success=false")
	}
	return nil
}

// cfAPICall performs an authenticated Cloudflare API request. It branches on
// the stored credential kind: oauth sends `Authorization: Bearer <access>` and
// transparently refreshes once on a stale token; apitoken sends
// `Authorization: Bearer <token>`; globalkey sends `X-Auth-Email` +
// `X-Auth-Key`. Manual kinds never auto-refresh (they persist until the
// merchant revokes them). Returns the response body and the (possibly rotated)
// credential for re-sealing.
func (h *RESTHandlers) cfAPICall(ctx context.Context, accountID, method, path string, body io.Reader) ([]byte, *cfCred, error) {
	sealed, err := h.Accounts.CFToken(ctx, accountID)
	if err != nil || sealed == "" {
		return nil, nil, fmt.Errorf("cloudflare not linked")
	}
	cred, err := cfUnseal(h.SecretsKey, sealed)
	if err != nil {
		return nil, nil, fmt.Errorf("unseal cf token: %w", err)
	}

	call := func(cred *cfCred) (int, []byte, error) {
		req, err := http.NewRequestWithContext(ctx, method, cfAPIBase+path, body)
		if err != nil {
			return 0, nil, err
		}
		switch cred.Kind {
		case cfKindGlobalKey:
			req.Header.Set("X-Auth-Email", cred.AccountEmail)
			req.Header.Set("X-Auth-Key", cred.Access)
		default: // oauth + apitoken both use Bearer
			req.Header.Set("Authorization", "Bearer "+cred.Access)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0, nil, err
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return resp.StatusCode, b, err
	}

	status, out, err := call(cred)
	// Auth-class failure → refresh once (oauth only), re-seal, retry once.
	if err == nil && status == http.StatusUnauthorized && cred.Kind == cfKindOAuth && cred.Refresh != "" {
		fresh, rerr := cfRefresh(ctx, cred.Refresh)
		if rerr != nil {
			return nil, nil, fmt.Errorf("cf refresh: %w", rerr)
		}
		if fresh.Refresh == "" {
			fresh.Refresh = cred.Refresh // CF sometimes omits rotation
		}
		blob, _ := json.Marshal(cfCred{Kind: cfKindOAuth, Access: fresh.Access, Refresh: fresh.Refresh})
		if ns, eerr := cryptoutil.Encrypt(h.SecretsKey, blob); eerr == nil {
			if serr := h.Accounts.SaveCFToken(ctx, accountID, ns); serr == nil {
				cred = &cfCred{Kind: cfKindOAuth, Access: fresh.Access, Refresh: fresh.Refresh}
			}
		}
		status, out, err = call(cred)
	}
	if err != nil {
		return nil, nil, err
	}
	if status >= 300 {
		return nil, cred, fmt.Errorf("cf api %s: %s: %s", path, http.StatusText(status), strings.TrimSpace(string(out)))
	}
	return out, cred, nil
}

// cfResolveZone finds the zone id for a domain (exact, then closest suffix).
func (h *RESTHandlers) cfResolveZone(ctx context.Context, accountID, domain string) (string, error) {
	body, _, err := h.cfAPICall(ctx, accountID, http.MethodGet, "/zones?per_page=50", nil)
	if err != nil {
		return "", err
	}
	var zr struct {
		Result []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &zr); err != nil {
		return "", err
	}
	var best, bestName string
	for _, z := range zr.Result {
		if z.Name == domain {
			return z.ID, nil
		}
		if strings.HasSuffix(domain, "."+z.Name) && len(z.Name) > len(bestName) {
			best, bestName = z.ID, z.Name
		}
	}
	if best == "" {
		return "", fmt.Errorf("no CF zone covers %s", domain)
	}
	return best, nil
}

// cfCreateCNAME creates a proxied CNAME record domain → target in the zone.
func (h *RESTHandlers) cfCreateCNAME(ctx context.Context, accountID, zoneID, domain, target string) error {
	payload := fmt.Sprintf(`{"type":"CNAME","name":%q,"content":%q,"proxied":true}`, domain, target)
	_, _, err := h.cfAPICall(ctx, accountID, http.MethodPost, "/zones/"+url.PathEscape(zoneID)+"/dns_records", strings.NewReader(payload))
	return err
}

var _ = time.Second
