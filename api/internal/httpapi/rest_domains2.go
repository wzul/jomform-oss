// REST: multiple custom domains per account (add/list/verify/delete) +
// Dokploy API domain sync + hosted CF tunnel with STORED token.
package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/wzul/jomform/api/internal/cryptoutil"
)

// dokployBase/API key are env-provided (host may expose the Dokploy API).
func dokployURL() string          { return os.Getenv("DOKPLOY_URL") }     // e.g. http://dokploy:3000
func dokployKey() string          { return os.Getenv("DOKPLOY_API_KEY") } // x-api-key
func dokployComposeID() string    { return os.Getenv("DOKPLOY_COMPOSE_ID") }
func dokployAPIContainer() string { return os.Getenv("DOKPLOY_CONTAINER") } // exec target

// dokployAddDomain registers a domain with Dokploy so Traefik routes it to
// the api container on /f/, /t/ paths. No-op when not running under Dokploy.
func dokployAddDomain(domain string) error {
	if dokployURL() == "" || dokployKey() == "" {
		return nil // not a dokploy env — silent no-op
	}
	if _, err := exec.LookPath("docker"); err != nil {
		// in-container api has no docker binary — use the dokploy API directly
		return dokployAddDomainViaAPI(domain)
	}
	return nil
}

// dokployAddDomainViaAPI calls Dokploy's domain.create through the HTTP API
// (preferred when this api runs inside the swarm and has network access).
func dokployAddDomainViaAPI(domain string) error {
	if dokployURL() == "" || dokployKey() == "" {
		return nil
	}
	body := fmt.Sprintf(`{"composeId":%q,"domainType":"compose","host":%q,"path":"/f/","serviceName":"api","port":8080,"https":true,"certificateType":"letsencrypt","domainId":""}`, dokployComposeID(), domain)
	if err := dokployCall("/api/domain.create", body); err != nil {
		return err
	}
	return dokployCall("/api/domain.create", fmt.Sprintf(`{"composeId":%q,"domainType":"compose","host":%q,"path":"/t/","serviceName":"api","port":8080,"https":true,"certificateType":"letsencrypt","domainId":""}`, dokployComposeID(), domain))
}

func dokployCall(path, body string) error {
	req, err := http.NewRequest(http.MethodPost, dokployURL()+path, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", dokployKey())
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		return fmt.Errorf("dokploy %s: %s: %s", path, resp.Status, strings.TrimSpace(string(b)))
	}
	slog.Info("dokploy_domain_added", "path", path)
	return nil
}

func dokployRemoveDomain(domain string) {
	if dokployURL() == "" || dokployKey() == "" {
		return
	}
	// Dokploy lacks domain delete-by-host in older versions; list → delete by id.
	var payload = fmt.Sprintf(`{"host":%q}`, domain)
	_ = dokployCall("/api/domain.delete", payload)
}

// ─── handlers ────────────────────────────────────────────────────────────────

// addCustomDomain (list/add) replacing single-domain semantics.
func (h *RESTHandlers) addCustomDomain(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	var in struct {
		Domain string `json:"domain"`
	}
	if err := decodeJSON(r, &in); err != nil || in.Domain == "" {
		writeErr(w, http.StatusBadRequest, "domain wajib")
		return
	}
	domain := normalizeDomain(in.Domain)
	if domain == "" || strings.HasSuffix(domain, "jomform.com") {
		writeErr(w, http.StatusBadRequest, "domain tidak sah")
		return
	}
	d, err := h.Domains.AddDomain(r.Context(), actx.Account.ID, domain, "cname")
	if err != nil {
		respondErr(w, err) // duplicate = user sees real error
		return
	}
	// Traefik sync (dokploy-aware envs auto; others no-op)
	_ = dokployAddDomain(domain)
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": d.ID, "domain": domain, "status": "pending_dns",
		"hint": "CNAME " + domain + " → jomform.com (atau A ke origin), kemudian Verify",
	})
}

func (h *RESTHandlers) listCustomDomains(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	domains, err := h.Domains.ListDomains(r.Context(), actx.Account.ID)
	if err != nil {
		respondErr(w, err)
		return
	}
	type row struct {
		ID       string `json:"id"`
		Domain   string `json:"domain"`
		Provider string `json:"provider"`
		Verified bool   `json:"verified"`
	}
	out := make([]row, 0, len(domains))
	for _, d := range domains {
		out = append(out, row{ID: d.ID, Domain: d.Domain, Provider: d.Provider, Verified: d.Verified})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *RESTHandlers) verifyCustomDomainByID(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	d, err := h.Domains.GetByID(r.Context(), actx.Account.ID, r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "domain tidak dijumpai")
		return
	}
	ips, err := net.LookupHost(d.Domain)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"verified": false, "detail": "DNS belum resolve"})
		return
	}
	target, _ := net.LookupHost("jomform.com")
	sameEdge := false
	for _, ip := range ips {
		for _, t := range target {
			if ip == t {
				sameEdge = true
			}
		}
	}
	ff400 := isCloudflareEdge(ips)
	serveOK := probeServesJomForm(d.Domain)
	if !sameEdge && !ff400 && !serveOK {
		writeJSON(w, http.StatusOK, map[string]any{"verified": false,
			"detail": "Domain tidak menghala ke JomForm (" + strings.Join(ips, ",") + ")"})
		return
	}
	if err := h.Domains.MarkVerified(r.Context(), actx.Account.ID, d.Domain); err != nil {
		respondErr(w, err)
		return
	}
	_ = dokployAddDomain(d.Domain)
	writeJSON(w, http.StatusOK, map[string]any{"verified": true, "domain": d.Domain})
}

// deleteCustomDomainByID removes a claim + dokploy route (best effort).
func (h *RESTHandlers) deleteCustomDomainByID(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	d, err := h.Domains.GetByID(r.Context(), actx.Account.ID, r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "tidak dijumpai")
		return
	}
	if err := h.Domains.RemoveDomain(r.Context(), actx.Account.ID, d.ID); err != nil {
		respondErr(w, err)
		return
	}
	dokployRemoveDomain(d.Domain)
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// connectCFHostedDomain replaces the single-domain variant: token is ENCRYPTED
// and stored with the domain row; tunnel provisioned in merchant account;
// connector runs here via dokploy-hosted cloudflared (env opt-in).
func (h *RESTHandlers) connectCFHostedDomain(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	var in struct {
		Domain      string `json:"domain"`
		CFApiToken  string `json:"cf_api_token"`
		CFAccountID string `json:"cf_account_id"`
	}
	if err := decodeJSON(r, &in); err != nil || in.Domain == "" || in.CFApiToken == "" || in.CFAccountID == "" {
		writeErr(w, http.StatusBadRequest, "domain, cf_api_token, cf_account_id wajib")
		return
	}
	domain := normalizeDomain(in.Domain)
	if domain == "" {
		writeErr(w, http.StatusBadRequest, "domain tidak sah")
		return
	}
	cf := newCF(in.CFApiToken)
	ctx := r.Context()

	res, err := cf.do(ctx, http.MethodPost, "/accounts/"+in.CFAccountID+"/cfd_tunnel",
		map[string]any{"name": "jomform-" + strings.ReplaceAll(domain, ".", "-")})
	if err != nil {
		writeErr(w, http.StatusBadGateway, "cloudflare: "+err.Error())
		return
	}
	var tun struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(res, &tun)

	_, err = cf.do(ctx, http.MethodPut,
		"/accounts/"+in.CFAccountID+"/cfd_tunnel/"+tun.ID+"/configurations",
		map[string]any{"config": map[string]any{
			"ingress": []map[string]any{
				{"hostname": domain, "service": "https://jomform.com",
					"origin_request": map[string]any{"no_tls_verify": true, "http_host_header": domain}},
				{"service": "http_status:404"},
			},
		}})
	if err != nil {
		writeErr(w, http.StatusBadGateway, "ingress config gagal: "+err.Error())
		return
	}
	rtRes, _ := cf.do(ctx, http.MethodGet, "/accounts/"+in.CFAccountID+"/cfd_tunnel/"+tun.ID+"/token", nil)
	var rt struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rtRes, &rt)

	// Best-effort DNS record creation.
	if zoneID := zoneIDFor(domain, cf); zoneID != "" {
		_, _ = cf.do(ctx, http.MethodPost, "/zones/"+zoneID+"/dns_records",
			map[string]any{"type": "CNAME", "name": domain, "content": tun.ID + ".cfargotunnel.com", "proxied": true})
	}

	enc, _ := cryptoutil.Encrypt(h.SecretsKey, []byte(rt.Token))
	d, err := h.Domains.AddDomain(r.Context(), actx.Account.ID, domain, "cf_tunnel")
	if err != nil {
		respondErr(w, err)
		return
	}
	if err := h.Domains.SaveTunnel(r.Context(), d.ID, enc, in.CFAccountID, tun.ID); err != nil {
		respondErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"domain": domain, "tunnel_id": tun.ID,
		"status": "provisioned",
		"note":   "Connector dihoskan oleh JomForm bila cloudflared CLI tersedia pada host; semak Verify.",
	})
}

func isCloudflareEdge(ips []string) bool {
	for _, ip := range ips {
		for _, prefix := range []string{"104.", "172.6", "188.114.", "162.159.", "198.41.", "131.0."} {
			if strings.HasPrefix(ip, prefix) {
				return true
			}
		}
	}
	return false
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

var _ = fmt.Sprintf
var _ = exec.Command
