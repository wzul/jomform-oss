package httpapi

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/wzul/jomform/api/internal/ssrfguard"
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

// probeServesJomForm checks https://domain/healthz renders our app.
// This is the AUTHORITATIVE verification check (issue #237): a custom domain
// is only verified when it actually serves a JomForm page, not merely because
// its DNS points at Cloudflare edge IPs shared with jomform.com.
// Issue #246: probe a STABLE, always-present resource (/healthz) instead of
// /f/test — a literal slug that 404s on correctly-serving domains that have
// no form slugged "test".
//
// Issue #366 (SSRF): the domain is merchant-supplied, so before dialing we
// resolve it once, reject loopback/link-local/private/cloud-metadata
// addresses (failing closed), and pin the connection to a single validated IP
// so a rebinding domain cannot reach a private/metadata address. A disallowed
// or unresolvable domain simply fails the probe.
func probeServesJomForm(domain string) bool {
	rawURL := "https://" + domain + "/healthz"
	pin, err := ssrfguard.Pin(nil, domain)
	if err != nil {
		return false
	}
	defer pin.Close()
	return probeServesJomFormURL(rawURL, pin.DialContext)
}

// probeServesJomFormURL is the testable core of probeServesJomForm: it fetches
// url and reports whether it returns 200 with a body containing the JomForm
// marker (case-insensitive). dial, when non-nil, is supplied to the client's
// transport; the caller (probeServesJomForm) passes a SSRF-pinned dial, while
// tests pass nil to reach the loopback httptest server.
//
// Issue #268: the probe uses an insecure HTTP client (InsecureSkipVerify) so a
// custom domain can verify even when its Let's Encrypt cert isn't yet
// trusted/issued. This resolves the chicken-and-egg where the TLS handshake
// failed ("unknown CA") before the probe could check the JomForm content. The
// content check below remains the authoritative verification — skipping cert
// verification only lets the probe reach the origin.
func probeServesJomFormURL(url string, dial func(ctx context.Context, network, addr string) (net.Conn, error)) bool {
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DialContext:     dial,
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // probe must reach origin before cert is trusted
		},
	}
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
	return resp.StatusCode == http.StatusOK && strings.Contains(strings.ToLower(string(body)), "jomform")
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
