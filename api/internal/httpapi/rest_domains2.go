// REST: multiple custom domains per account (add/list/verify/delete).
// Manual CNAME → Verify → auto Traefik file-provider route (issue #236).
package httpapi

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net/http"
	"strings"

	"github.com/wzul/jomform/api/internal/cryptoutil"
)

// verifyFailDetail is the stable error code returned when a custom domain
// fails the JomForm probe (issue #248). It is intentionally NOT a human
// message: the frontend maps this code to the merchant's selected UI
// language via i18n (issue #261), so the guidance (CNAME to
// custom.jomform.com, or Cloudflare proxy) is shown in en/ms/zh/ta/ar instead
// of a hardcoded Malay string.
const verifyFailDetail = "verify_failed"

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
		writeErr(w, http.StatusBadRequest, "domain is required")
		return
	}
	domain := normalizeDomain(in.Domain)
	if domain == "" || strings.HasSuffix(domain, "jomform.com") {
		writeErr(w, http.StatusBadRequest, "invalid domain")
		return
	}
	d, err := h.Domains.AddDomain(r.Context(), actx.Account.ID, domain, "cname")
	if err != nil {
		respondErr(w, r, err) // duplicate = user sees real error
		return
	}
	// Traefik file-provider route is handled by the plain deployment (issue #236).
	// Auto-CNAME: when the merchant linked Cloudflare, create the proxied
	// CNAME record in their zone (issue #22). The result is surfaced to the
	// UI (issue #322) instead of being silent best-effort: on success the
	// merchant sees "CNAME created automatically"; on failure they see the
	// real reason and can fall back to the manual path.
	autoCNAME := "not_cloudflare"
	if cfOAuthConfigured() {
		if err := h.cfCreateCNAMEForDomain(r.Context(), actx.Account.ID, domain); err != nil {
			slog.Info("cf_auto_cname_failed", "domain", domain, "err", err)
			autoCNAME = "failed"
			_ = h.Domains.SetAutoCNAME(r.Context(), actx.Account.ID, domain, "failed")
		} else {
			autoCNAME = "created"
			_ = h.Domains.SetAutoCNAME(r.Context(), actx.Account.ID, domain, "created")
		}
	}
	// Issue #92: audit custom-domain add.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "settings.domain_added", map[string]any{"domain": domain})
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": d.ID, "domain": domain, "status": "pending_dns",
		// Card t_48a93084: this hint is the server twin of the settings copy —
		// on a self-hosted install the domain has to point at THIS install, not
		// at the hosted product's target.
		"hint":       "CNAME " + domain + " → " + h.cnameHost() + " (or A to origin), then Verify",
		"cloudflare": cfNote(autoCNAME),
		"auto_cname": autoCNAME,
	})
}

// cfNote returns a short human note for the auto-CNAME result (issue #322).
func cfNote(status string) string {
	switch status {
	case "created":
		return "CNAME record created automatically via Cloudflare."
	case "failed":
		return "Couldn't auto-create the CNAME via Cloudflare — set it manually."
	default:
		return ""
	}
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
		ID        string `json:"id"`
		Domain    string `json:"domain"`
		Provider  string `json:"provider"`
		Verified  bool   `json:"verified"`
		AutoCNAME string `json:"auto_cname"`
		// Issue #397: per-domain TLS config + status.
		TLSMode   string `json:"tls_mode"`
		TLSStatus string `json:"tls_status"`
		TLSError  string `json:"tls_error,omitempty"`
		// Issue #398: per-domain ACME contact email ("" = fall back to the
		// account email, then the global ACME_EMAIL env).
		TLSEmail string `json:"tls_email,omitempty"`
	}
	out := make([]row, 0, len(domains))
	for _, d := range domains {
		out = append(out, row{ID: d.ID, Domain: d.Domain, Provider: d.Provider, Verified: d.Verified, AutoCNAME: d.AutoCNAME,
			TLSMode: d.TLSMode, TLSStatus: d.TLSStatus, TLSError: d.TLSError, TLSEmail: d.TLSEmail})
	}
	writeJSON(w, http.StatusOK, out)
}

// autoCreateCNAMEForDomain (issue #322): one-click action for a domain that
// was added without Cloudflare linked (or whose auto-CNAME failed). It
// creates the proxied CNAME via the linked Cloudflare account and returns
// the result so the UI can show success/failure instead of silent fallback.
func (h *RESTHandlers) autoCreateCNAMEForDomain(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	d, err := h.Domains.GetByID(r.Context(), actx.Account.ID, r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "domain not found")
		return
	}
	if !cfOAuthConfigured() {
		writeJSON(w, http.StatusOK, map[string]any{"auto_cname": "not_cloudflare", "error": "Cloudflare not linked"})
		return
	}
	if err := h.cfCreateCNAMEForDomain(r.Context(), actx.Account.ID, d.Domain); err != nil {
		_ = h.Domains.SetAutoCNAME(r.Context(), actx.Account.ID, d.Domain, "failed")
		writeJSON(w, http.StatusOK, map[string]any{"auto_cname": "failed", "error": err.Error()})
		return
	}
	_ = h.Domains.SetAutoCNAME(r.Context(), actx.Account.ID, d.Domain, "created")
	writeJSON(w, http.StatusOK, map[string]any{"auto_cname": "created"})
}

func (h *RESTHandlers) verifyCustomDomainByID(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	d, err := h.Domains.GetByID(r.Context(), actx.Account.ID, r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "domain not found")
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
		// The correct setup is a CNAME to this deployment's target (or Cloudflare
		// proxy), so the message says exactly that.
		writeJSON(w, http.StatusOK, map[string]any{"verified": false,
			"detail": verifyFailDetail})
		return
	}
	if err := h.Domains.MarkVerified(r.Context(), actx.Account.ID, d.Domain); err != nil {
		respondErr(w, r, err)
		return
	}
	// Issue #398: default the per-domain ACME email to the domain owner's
	// account email when the domain is verified (unless the merchant already
	// set one). The merchant can change it later in the config page.
	acmeEmail := d.TLSEmail
	if acmeEmail == "" {
		acmeEmail = h.accountOwnerEmail(r, actx)
		if acmeEmail != "" {
			_ = h.Domains.SetTLSEmail(r.Context(), actx.Account.ID, d.Domain, acmeEmail)
		}
	}
	// Issue #212: register the verified domain with the origin auto-TLS
	// manager so it provisions + renews a Let's Encrypt cert (SNI-based),
	// using the domain's ACME email (issue #398). No-op when auto-TLS is not
	// configured (Cloudflare-proxied domains keep using Cloudflare's edge
	// TLS).
	if h.TLS != nil {
		h.TLS.AddVerified(d.Domain, acmeEmail)
	}
	// Issue #397: surface the TLS provisioning status. A cached cert means
	// the domain is already actively serving; otherwise it is still
	// provisioning. Custom-cert domains keep their "custom" status.
	if d.TLSMode == "custom" {
		_ = h.Domains.SetTLSStatus(r.Context(), actx.Account.ID, d.Domain, "custom", "")
	} else if h.TLS != nil && h.TLS.HasCert(d.Domain) {
		_ = h.Domains.SetTLSStatus(r.Context(), actx.Account.ID, d.Domain, "active", "")
	} else {
		_ = h.Domains.SetTLSStatus(r.Context(), actx.Account.ID, d.Domain, "pending", "")
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
		writeErr(w, http.StatusNotFound, "not found")
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

// setDomainTLSMode (issue #397): choose the per-domain TLS mode — "auto"
// (default, Let's Encrypt via tlsmgr) or "custom" (bring-your-own-cert).
// Switching to "auto" clears any stored custom cert so the origin provisions
// a Let's Encrypt cert for the verified domain.
func (h *RESTHandlers) setDomainTLSMode(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	d, err := h.Domains.GetByID(r.Context(), actx.Account.ID, r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "domain not found")
		return
	}
	var in struct {
		Mode string `json:"mode"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "mode is required")
		return
	}
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	if mode != "auto" && mode != "custom" {
		writeErr(w, http.StatusBadRequest, "invalid mode (auto|custom)")
		return
	}
	if err := h.Domains.SetTLSMode(r.Context(), actx.Account.ID, d.Domain, mode); err != nil {
		respondErr(w, r, err)
		return
	}
	// Keep the TLS manager in sync: a custom-cert domain is served its own
	// cert (if one is stored); switching to auto drops any custom cert and
	// re-registers the domain for ACME provisioning (issue #398: with the
	// domain's ACME email).
	if h.TLS != nil {
		if mode == "custom" {
			h.TLS.RemoveCustomCert(d.Domain)
		} else {
			h.TLS.RemoveCustomCert(d.Domain)
			if d.Verified {
				h.TLS.AddVerified(d.Domain, d.TLSEmail)
			}
		}
	}
	// Issue #92: audit the TLS-mode change.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "settings.domain_tls_mode", map[string]any{"domain": d.Domain, "mode": mode})
	writeJSON(w, http.StatusOK, map[string]any{"tls_mode": mode})
}

// setDomainTLSEmail (issue #398): set the per-domain ACME/Let's Encrypt
// contact email used to provision + renew the domain's auto-TLS cert. An
// empty email clears it, so the domain falls back to the account email, then
// the global ACME_EMAIL env. The TLS manager is re-registered with the new
// email so the next renewal uses it.
func (h *RESTHandlers) setDomainTLSEmail(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	d, err := h.Domains.GetByID(r.Context(), actx.Account.ID, r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "domain not found")
		return
	}
	var in struct {
		Email string `json:"email"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "email is required")
		return
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if email != "" && !strings.Contains(email, "@") {
		writeErr(w, http.StatusBadRequest, "invalid email")
		return
	}
	if err := h.Domains.SetTLSEmail(r.Context(), actx.Account.ID, d.Domain, email); err != nil {
		respondErr(w, r, err)
		return
	}
	// Re-register the domain with the TLS manager so the new email drives
	// provisioning/renewal (issue #398).
	if h.TLS != nil && d.Verified {
		h.TLS.AddVerified(d.Domain, email)
	}
	// Issue #92: audit the ACME email change.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "settings.domain_tls_email", map[string]any{"domain": d.Domain})
	writeJSON(w, http.StatusOK, map[string]any{"tls_email": email})
}

// uploadCustomCert (issue #397): store a merchant-supplied cert + key
// (bring-your-own-cert) for a verified domain and serve it via SNI. The PEM
// blocks are validated and the private key must match the certificate before
// anything is persisted.
func (h *RESTHandlers) uploadCustomCert(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	d, err := h.Domains.GetByID(r.Context(), actx.Account.ID, r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "domain not found")
		return
	}
	var in struct {
		Cert string `json:"cert"`
		Key  string `json:"key"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "cert and key are required")
		return
	}
	certPEM := strings.TrimSpace(in.Cert)
	keyPEM := strings.TrimSpace(in.Key)
	if certPEM == "" || keyPEM == "" {
		writeErr(w, http.StatusBadRequest, "cert and key are required")
		return
	}
	// Validate the pair before persisting: parse the cert chain + key and
	// confirm the key matches the leaf certificate.
	cert, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid cert/key — check the PEM format and that the key matches")
		return
	}
	if len(cert.Certificate) == 0 {
		writeErr(w, http.StatusBadRequest, "invalid certificate")
		return
	}
	// Seal the PEM blocks with the account encryption key.
	certEnc, err := cryptoutil.Encrypt(h.SecretsKey, []byte(certPEM))
	if err != nil {
		respondErr(w, r, err)
		return
	}
	keyEnc, err := cryptoutil.Encrypt(h.SecretsKey, []byte(keyPEM))
	if err != nil {
		respondErr(w, r, err)
		return
	}
	if err := h.Domains.SetCustomCert(r.Context(), actx.Account.ID, d.Domain, certEnc, keyEnc); err != nil {
		respondErr(w, r, err)
		return
	}
	// Serve the custom cert via SNI immediately.
	if h.TLS != nil {
		h.TLS.SetCustomCert(d.Domain, &cert)
	}
	// Issue #92: audit the custom-cert upload.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "settings.domain_custom_cert", map[string]any{"domain": d.Domain})
	writeJSON(w, http.StatusOK, map[string]any{"tls_mode": "custom", "tls_status": "custom"})
}

// accountOwnerEmail resolves the email of the account's owner user (the
// session user). Used to default a custom domain's ACME contact email to the
// domain owner's account email (issue #398). Returns "" when the owner can't
// be resolved.
func (h *RESTHandlers) accountOwnerEmail(r *http.Request, actx *AccountCtx) string {
	if h.Users == nil {
		return ""
	}
	uid := h.sessionOwnerID(r)
	if uid == "" {
		return ""
	}
	u, err := h.Users.ByID(r.Context(), uid)
	if err != nil || u == nil {
		return ""
	}
	return u.Email
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
