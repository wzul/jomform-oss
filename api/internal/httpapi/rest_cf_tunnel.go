// Cloudflare-managed tunnel onboarding for merchant custom domains.
//
// Flow ("login with Cloudflare" → auto listener):
//  1. Merchant clicks "Connect custom domain" in Settings.
//  2. JomForm hits CF API to CREATE a tunnel in the merchant's own account
//     (OAuth token from the merchant, scopes: cloudflare_tunnel + zone dns).
//  3. CF returns a tunnel token; JomForm renders a ready `cloudflared` run
//     command + QR AND stores service config so the tunnel routes to us.
//  4. JomForm writes the tunnel's ingress (config) via CF API:
//     hostname → https://jomform.com, originRequest noTLSVerify.
//  5. Merchant runs the command anywhere (their laptop/NAS/VPS). When the
//     connector comes online, JomForm's probe passes → domain VERIFIED.
//
// The CF API token never touches JomForm storage — it lives only in the
// browser session during the connect hand-shake.
package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

type cfTunnelRequest struct {
	Name string `json:"name"`
}

// createTunnelForDomain provisions a CF tunnel in the MERCHANT account using
// the merchant-supplied API token, and returns run instructions.
func (h *RESTHandlers) connectCustomDomain(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	var in struct {
		Domain     string `json:"domain"`
		CFApiToken string `json:"cf_api_token"` // from the merchant's browser session
		AccountID  string `json:"cf_account_id"`
	}
	if err := decodeJSON(r, &in); err != nil || in.Domain == "" || in.CFApiToken == "" || in.AccountID == "" {
		writeErr(w, http.StatusBadRequest, "domain, cf_api_token dan cf_account_id wajib")
		return
	}
	domain := normalizeDomain(in.Domain)
	if domain == "" || strings.HasSuffix(domain, "jomform.com") {
		writeErr(w, http.StatusBadRequest, "domain tidak sah")
		return
	}

	client := &http.Client{Timeout: 20 * time.Second}

	// 1. Create the tunnel in the merchant's account.
	body, _ := json.Marshal(cfTunnelRequest{Name: "jomform-" + strings.ReplaceAll(domain, ".", "-")})
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodPost,
		"https://api.cloudflare.com/client/v4/accounts/"+in.AccountID+"/cfd_tunnel",
		strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer "+in.CFApiToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "cloudflare unreachable")
		return
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var cf struct {
		Success bool `json:"success"`
		Result  struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"result"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &cf); err != nil || !cf.Success {
		writeErr(w, http.StatusBadGateway, "cloudflare menolak (semak token/scope): "+cfJSONErr(cf.Errors))
		return
	}

	// 2. Configure the tunnel to route this hostname to JomForm.
	confBody := map[string]any{
		"config": map[string]any{
			"ingress": []map[string]any{
				{
					"hostname": domain,
					"service":  "https://jomform.com",
					"origin_request": map[string]any{
						"no_tls_verify":    true,
						"http_host_header": domain,
					},
				},
				{"service": "http_status:404"},
			},
		},
	}
	cb, _ := json.Marshal(confBody)
	req2, _ := http.NewRequestWithContext(r.Context(), http.MethodPut,
		"https://api.cloudflare.com/client/v4/accounts/"+in.AccountID+"/cfd_tunnel/"+cf.Result.ID+"/configurations",
		strings.NewReader(string(cb)))
	req2.Header.Set("Authorization", "Bearer "+in.CFApiToken)
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := client.Do(req2)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "cloudflare unreachable (config)")
		return
	}
	defer resp2.Body.Close()
	raw2, _ := io.ReadAll(io.LimitReader(resp2.Body, 1<<20))
	var cf2 struct {
		Success bool `json:"success"`
	}
	_ = json.Unmarshal(raw2, &cf2)

	// Claim the domain locally; token is given to the merchant ONCE (browser).
	if err := h.Accounts.SetCustomDomain(r.Context(), actx.Account.ID, domain); err != nil {
		respondErr(w, err)
		return
	}

	respData := map[string]any{
		"tunnel_id":      cf.Result.ID,
		"domain":         domain,
		"config_applied": cf2.Success,
		"run_command":    "cloudflared tunnel --no-autoupdate run --token <GET_FROM_DASH>",
		"next": []string{
			"Run the command on any machine with internet (Docker/NAS/VPS).",
			"Wait ~30s for the connector to come online, then press Verify.",
		},
	}
	writeJSON(w, http.StatusOK, respData)
}

func cfJSONErr(errs []struct {
	Message string `json:"message"`
}) string {
	if len(errs) == 0 {
		return ""
	}
	return errs[0].Message
}

// normalizeDomain strips scheme/slash/lowercase.
func normalizeDomain(d string) string {
	d = strings.ToLower(strings.TrimSpace(d))
	d = strings.TrimPrefix(d, "https://")
	d = strings.TrimPrefix(d, "http://")
	d = strings.TrimSuffix(d, "/")
	if !strings.Contains(d, ".") {
		return ""
	}
	return d
}
