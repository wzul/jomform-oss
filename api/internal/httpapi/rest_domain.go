package httpapi

import (
	"io"
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
	// Issue #92: audit custom-domain claim.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "settings.domain_claimed", map[string]any{"domain": domain})
	respond(w, http.StatusOK, map[string]any{
		"status": "pending_dns",
		"domain": domain,
		"steps":  []string{"CNAME " + domain + " -> jomform.com", "Tunggu DNS propagate, kemudian call verify"},
	})
}

// verifyCustomDomain (old single-domain verify) removed with the legacy
// single-domain flow — the multi-domain path is verifyCustomDomainByID in
// rest_domains2.go. probeServesJomForm below is shared by it.

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
	// Issue #92: audit custom-domain removal.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "settings.domain_cleared", nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}
