// Multi-tenant cloudflared connectors HOSTED BY JOMFORM (per account).
//
// Flow (zero merchant infra):
//  1. Merchant pastes ONLY their Cloudflare API token + Account ID once.
//  2. JomForm provisions: cfd_tunnel (their account) + ingress → our api.
//  3. JomForm stores the tunnel RUNNER TOKEN (encrypted) and spawns/keeps a
//     per-merchant `cloudflared tunnel run --token …` connector ON THIS HOST,
//     as a docker container on the shared dokploy network.
//  4. Verify flips when the connector is online (probe the hostname).
//
// The merchant never runs anything. Each connector is isolated; revoking a
// domain = delete the tunnel + its container.
package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

type cfClient struct {
	token string
	http  *http.Client
}

func newCF(token string) *cfClient {
	return &cfClient{token: token, http: &http.Client{Timeout: 25 * time.Second}}
}

func (c *cfClient) do(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://api.cloudflare.com/client/v4"+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var wrap struct {
		Success bool            `json:"success"`
		Result  json.RawMessage `json:"result"`
		Errors  []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, err
	}
	if !wrap.Success {
		msg := "cloudflare error"
		if len(wrap.Errors) > 0 {
			msg = wrap.Errors[0].Message
		}
		return nil, fmt.Errorf("%s", msg)
	}
	return wrap.Result, nil
}

// hostConnector runs `cloudflared tunnel run --token T` as a local docker
// container dedicated to this merchant. Network = same dokploy overlay so
// it reaches the api directly.
func startConnector(containerName, runToken string) error {
	// remove stale container with same name
	_ = exec.Command("docker", "rm", "-f", containerName).Run()
	cmd := exec.Command("docker", "run", "-d",
		"--name", containerName,
		"--restart", "unless-stopped",
		"--network", "dokploy-network",
		"cloudflare/cloudflared:latest",
		"tunnel", "--no-autoupdate", "run", "--token", runToken)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("connector start: %v: %s", err, strings.TrimSpace(string(out)))
	}
	slog.Info("cf_connector_started", "container", containerName)
	return nil
}

func stopConnector(containerName string) {
	_ = exec.Command("docker", "rm", "-f", containerName).Run()
	slog.Info("cf_connector_stopped", "container", containerName)
}

// connectCFHosted provisions everything server-side; merchant does nothing
// beyond pasting their token once.
func (h *RESTHandlers) connectCFHosted(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	var in struct {
		Domain      string `json:"domain"`
		CFApiToken  string `json:"cf_api_token"`
		CFAccountID string `json:"cf_account_id"`
	}
	if err := decodeJSON(r, &in); err != nil || in.Domain == "" || in.CFApiToken == "" || in.CFAccountID == "" {
		writeErr(w, http.StatusBadRequest, "domain, cf_api_token dan cf_account_id wajib")
		return
	}
	domain := normalizeDomain(in.Domain)
	if domain == "" || strings.HasSuffix(domain, "jomform.com") {
		writeErr(w, http.StatusBadRequest, "domain tidak sah")
		return
	}

	cf := newCF(in.CFApiToken)
	ctx := r.Context()

	// 1. Create tunnel in the merchant's account.
	res, err := cf.do(ctx, http.MethodPost,
		"/accounts/"+in.CFAccountID+"/cfd_tunnel",
		map[string]any{"name": "jomform-" + strings.ReplaceAll(domain, ".", "-")})
	if err != nil {
		writeErr(w, http.StatusBadGateway, "cloudflare: "+err.Error())
		return
	}
	var tun struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(res, &tun); err != nil {
		writeErr(w, http.StatusBadGateway, "cloudflare response parse gagal")
		return
	}

	// 2. Ingress: this hostname → our public URL (CF terminates TLS at edge;
	// tunnel egress hits jomform.com with Host preservation).
	_, err = cf.do(ctx, http.MethodPut,
		"/accounts/"+in.CFAccountID+"/cfd_tunnel/"+tun.ID+"/configurations",
		map[string]any{
			"config": map[string]any{
				"ingress": []map[string]any{
					{
						"hostname":       domain,
						"service":        "https://jomform.com",
						"origin_request": map[string]any{"no_tls_verify": true, "http_host_header": domain},
					},
					{"service": "http_status:404"},
				},
			},
		})
	if err != nil {
		writeErr(w, http.StatusBadGateway, "cloudflare ingress config gagal: "+err.Error())
		return
	}

	// 3. Fetch the runner token for THIS tunnel.
	rtRes, err := cf.do(ctx, http.MethodGet,
		"/accounts/"+in.CFAccountID+"/cfd_tunnel/"+tun.ID+"/token", nil)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "cloudflare token fetch gagal: "+err.Error())
		return
	}
	var rt struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rtRes, &rt)

	// 4. Start the hosted connector here.
	id := strings.NewReplacer(".", "-", "/", "").Replace(domain)
	if err := startConnector("jomform-cd-"+id, rt.Token); err != nil {
		writeErr(w, http.StatusInternalServerError, "connector start gagal: "+err.Error())
		return
	}

	// 5. Claim + DNS hint (merchant still needs the CNAME record OR CF will
	//    auto-route if the zone token allows us to create it — best effort).
	if _, err := cf.do(ctx, http.MethodPost,
		"/zones/"+zoneIDFor(domain, cf)+"/dns_records",
		map[string]any{"type": "CNAME", "name": domain, "content": tun.ID + ".cfargotunnel.com", "proxied": true}); err != nil {
		slog.Warn("cf_dns_autocreate_failed", "domain", domain, "err", err)
	}

	if err := h.Accounts.SetCustomDomain(r.Context(), actx.Account.ID, domain); err != nil {
		respondErr(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"tunnel_id": tun.ID,
		"domain":    domain,
		"hosted_by": "jomform",
		"connector": "jomform-cd-" + id,
		"next":      "Tunggu 30-60 saat connector online, kemudian Verify.",
	})
}

// zoneIDFor resolves the zone id for a domain using the merchant's token.
func zoneIDFor(domain string, cf *cfClient) string {
	parts := strings.Split(domain, ".")
	if len(parts) < 2 {
		return ""
	}
	zone := strings.Join(parts[len(parts)-2:], ".")
	res, err := cf.do(context.Background(), http.MethodGet,
		"/zones?name="+zone, nil)
	if err != nil {
		return ""
	}
	var zs []struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(res, &zs)
	if len(zs) == 0 {
		return ""
	}
	return zs[0].ID
}

// normalizeDomain strips scheme/slash and lowercases.
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
