// REST: multiple custom domains per account (add/list/verify/delete).
// Manual CNAME → Verify → auto Traefik file-provider route (issue #236).
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
)

// verifyFailDetail is the message returned when a custom domain fails the
// JomForm probe (issue #248). It guides the merchant to the correct DNS setup
// (CNAME to custom.jomform.com, or Cloudflare proxy) instead of dumping the
// domain's resolved IPs — those are Cloudflare edge IPs and dumping them
// misleadingly implies an A Record, which is wrong.
const verifyFailDetail = "Domain tidak menghala ke JomForm. Set CNAME dari domain anda ke custom.jomform.com (atau gunakan Cloudflare proxy), kemudian tunggu DNS propagate dan Verify semula."

// ─── handlers ────────────────────────────────────────────────────────────────

// addCustomDomain (list/add) replacing single-domain semantics.
func (h *RESTHandlers) addCustomDomain(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
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
		respondErr(w, r, err) // duplicate = user sees real error
		return
	}
	// Traefik file-provider route is handled by the plain deployment (issue #236).
	// Auto-CNAME: when the merchant linked Cloudflare, create the proxied
	// CNAME record in their zone (issue #22). Best-effort — the manual
	// CNAME path below never blocks on CF failures.
	cfNote := ""
	if cfOAuthConfigured() {
		if err := h.cfCreateCNAMEForDomain(r.Context(), actx.Account.ID, domain); err != nil {
			slog.Info("cf_auto_cname_skipped", "domain", domain, "err", err)
		} else {
			cfNote = "CNAME record created automatically via Cloudflare."
		}
	}
	// Issue #92: audit custom-domain add.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "settings.domain_added", map[string]any{"domain": domain})
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": d.ID, "domain": domain, "status": "pending_dns",
		"hint":       "CNAME " + domain + " → jomform.com (atau A ke origin), kemudian Verify",
		"cloudflare": cfNote,
	})
}

// cfCreateCNAMEForDomain resolves the merchant's CF zone for domain and
// creates the proxied CNAME → jomform.com (the Traefik wildcard target).
func (h *RESTHandlers) cfCreateCNAMEForDomain(ctx context.Context, accountID, domain string) error {
	zoneID, err := h.cfResolveZone(ctx, accountID, domain)
	if err != nil {
		return err
	}
	return h.cfCreateCNAME(ctx, accountID, zoneID, domain, "jomform.com")
}

func (h *RESTHandlers) listCustomDomains(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	domains, err := h.Domains.ListDomains(r.Context(), actx.Account.ID)
	if err != nil {
		respondErr(w, r, err)
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
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	d, err := h.Domains.GetByID(r.Context(), actx.Account.ID, r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "domain tidak dijumpai")
		return
	}
	// Issue #237: probeServesJomForm is the AUTHORITATIVE check. A domain is
	// verified ONLY if it actually serves a JomForm page. sameEdge/ff400 are
	// hints that DNS points at Cloudflare, but they are NOT sufficient on
	// their own — a domain behind Cloudflare with the same edge IPs as
	// jomform.com must still serve JomForm to be verified.
	serveOK := probeServesJomForm(d.Domain)
	if !serveOK {
		// Issue #248: guide the merchant to the correct DNS setup instead of
		// dumping the domain's resolved IPs. Those are Cloudflare edge IPs
		// (104.21.x, 172.67.x, 2606:4700:...) and dumping them misleadingly
		// implies the merchant should set an A Record to them — which is wrong.
		// The correct setup is a CNAME to custom.jomform.com (or Cloudflare
		// proxy), so the message says exactly that.
		writeJSON(w, http.StatusOK, map[string]any{"verified": false,
			"detail": verifyFailDetail})
		return
	}
	if err := h.Domains.MarkVerified(r.Context(), actx.Account.ID, d.Domain); err != nil {
		respondErr(w, r, err)
		return
	}
	// Issue #212: register the verified domain with the origin auto-TLS
	// manager so it provisions + renews a Let's Encrypt cert (SNI-based).
	// No-op when ACME is not configured (Cloudflare-proxied domains keep
	// using Cloudflare's edge TLS).
	if h.TLS != nil {
		h.TLS.AddVerified(d.Domain)
	}
	// Issue #92: audit custom-domain verify.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "settings.domain_verified", map[string]any{"domain": d.Domain})
	writeJSON(w, http.StatusOK, map[string]any{"verified": true, "domain": d.Domain})
}

// deleteCustomDomainByID removes a claim (best effort).
func (h *RESTHandlers) deleteCustomDomainByID(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	d, err := h.Domains.GetByID(r.Context(), actx.Account.ID, r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "tidak dijumpai")
		return
	}
	if err := h.Domains.RemoveDomain(r.Context(), actx.Account.ID, d.ID); err != nil {
		respondErr(w, r, err)
		return
	}
	// Issue #212: stop provisioning the cert for a removed domain.
	if h.TLS != nil {
		h.TLS.RemoveVerified(d.Domain)
	}
	// Issue #92: audit custom-domain removal.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "settings.domain_removed", map[string]any{"domain": d.Domain})
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
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

// isCloudflareEdge was removed with issue #237: sharing Cloudflare edge IPs
// with jomform.com is no longer sufficient to verify a custom domain — the
// domain must actually serve a JomForm page (probeServesJomForm).
