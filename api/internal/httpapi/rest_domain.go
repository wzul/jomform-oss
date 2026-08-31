package httpapi

import (
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// setCustomDomain claims a custom domain for the account.
func (h *RESTHandlers) setCustomDomain(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	var in struct {
		Domain string `json:"domain"`
	}
	if err := decodeJSON(r, &in); err != nil || in.Domain == "" {
		writeErr(w, http.StatusBadRequest, "domain diperlukan")
		return
	}
	domain := strings.ToLower(strings.TrimSpace(in.Domain))
	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.TrimPrefix(domain, "http://")
	domain = strings.TrimSuffix(domain, "/")
	if domain == "" || !strings.Contains(domain, ".") || strings.HasSuffix(domain, "jomform.com") {
		writeErr(w, http.StatusBadRequest, "domain tidak sah")
		return
	}
	if err := h.Accounts.SetCustomDomain(r.Context(), actx.Account.ID, domain); err != nil {
		respondErr(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{
		"status": "pending_dns",
		"domain": domain,
		"steps":  []string{"CNAME " + domain + " -> jomform.com", "Tunggu DNS propagate, kemudian call verify"},
	})
}

// verifyCustomDomain checks DNS CNAME points to jomform.com and marks verified.
func (h *RESTHandlers) verifyCustomDomain(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	acc, err := h.Accounts.ByID(r.Context(), actx.Account.ID)
	if err != nil || acc.CustomDomain == "" {
		writeErr(w, http.StatusBadRequest, "tiada custom domain dikonfigur")
		return
	}
	ips, err := net.LookupHost(acc.CustomDomain)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"verified": false, "detail": "DNS belum resolve"})
		return
	}
	// Verification: the domain must SERVE our app. Cloudflare-fronted
	// domains resolve to different edge IPs than jomform.com, so raw IP
	// equality is wrong (both sides proxied). Instead, hit the domain and
	// look for our subdomain-routing behaviour: a published form slug on
	// this host must render. DNS check remains a fast pre-filter.
	target, _ := net.LookupHost("jomform.com")
	sameEdge := false
	for _, ip := range ips {
		for _, t := range target {
			if ip == t {
				sameEdge = true
			}
		}
	}
	if !sameEdge {
		// Proxied (different edge IPs) — fall through to HTTP probe; only
		// fail fast when DNS does not resolve at all (handled above).
	}
	serveOK := probeServesJomForm(acc.CustomDomain)
	if !serveOK {
		writeJSON(w, http.StatusOK, map[string]any{"verified": false, "detail": "CNAME menunjuk tempat lain (" + acc.CustomDomain + " -> " + strings.Join(ips, ",") + ")"})
		return
	}
	if err := h.Accounts.MarkCustomDomainVerified(r.Context(), actx.Account.ID); err != nil {
		respondErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"verified": true, "domain": acc.CustomDomain})
}

// getCustomDomain reports current claim + verification.
func (h *RESTHandlers) getCustomDomain(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	acc, err := h.Accounts.ByID(r.Context(), actx.Account.ID)
	if err != nil {
		respondErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"domain": acc.CustomDomain, "verified": acc.CustomDomainVerified,
	})
}

// probeServesJomForm checks https://domain/f/<known-slug> renders our app.
func probeServesJomForm(domain string) bool {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodGet, "https://"+domain+"/f/test", nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	return resp.StatusCode == http.StatusOK && strings.Contains(string(body), "JomForm")
}

// deleteCustomDomain clears the claim.
func (h *RESTHandlers) deleteCustomDomain(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if err := h.Accounts.ClearCustomDomain(r.Context(), actx.Account.ID); err != nil {
		respondErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}
