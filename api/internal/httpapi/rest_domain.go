package httpapi

import (
	"io"
	"net/http"
	"strings"
	"time"
)

// setCustomDomain claims a custom domain for the account.
func (h *RESTHandlers) setCustomDomain(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
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
		respondErr(w, r, err)
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
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"domain": acc.CustomDomain, "verified": acc.CustomDomainVerified,
	})
}

// probeServesJomForm checks https://domain/f/<known-slug> renders our app.
// This is the AUTHORITATIVE verification check (issue #237): a custom domain
// is only verified when it actually serves a JomForm page, not merely because
// its DNS points at Cloudflare edge IPs shared with jomform.com.
func probeServesJomForm(domain string) bool {
	return probeServesJomFormURL("https://" + domain + "/f/test")
}

// probeServesJomFormURL is the testable core of probeServesJomForm: it fetches
// url and reports whether it returns 200 with a body containing "JomForm".
func probeServesJomFormURL(url string) bool {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodGet, url, nil)
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
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	if err := h.Accounts.ClearCustomDomain(r.Context(), actx.Account.ID); err != nil {
		respondErr(w, r, err)
		return
	}
	// Issue #92: audit custom-domain removal.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "settings.domain_cleared", nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}
