package httpapi

import (
	"net"
	"net/http"
	"strings"
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
	// Resolve our own apex and compare edge IPs (works proxied or DNS-only).
	target, _ := net.LookupHost("jomform.com")
	okNet := false
	for _, ip := range ips {
		for _, t := range target {
			if ip == t {
				okNet = true
			}
		}
	}
	if !okNet {
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

// deleteCustomDomain clears the claim.
func (h *RESTHandlers) deleteCustomDomain(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if err := h.Accounts.ClearCustomDomain(r.Context(), actx.Account.ID); err != nil {
		respondErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}
