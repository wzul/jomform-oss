// MCP tools: MCP-parity batch (issue #396). These close the parity gaps
// between the web dashboard and the MCP surface for: analytics, custom
// domains, tracking, CHIP account settings, profile avatar, passkeys, social
// accounts, 2FA, system settings, sales export, Cloudflare, and the webhook
// signing key. Each tool is a thin wrapper over the same stores the REST
// handlers use (one logic, two interfaces).
package mcpsrv

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/cryptoutil"
	"github.com/wzul/jomform/api/internal/csvguard"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/ssrfguard"
)

// sanitizeTrackingMCP regexes (mirror of httpapi/tracking.go).
var (
	scriptSrcReMCP  = regexp.MustCompile(`(?is)^<script\s+src=["']https://[a-z0-9.-]+[^"']*["']\s*(defer|async)?\s*></script>$`)
	metaReMCP       = regexp.MustCompile(`(?is)^<meta\s+[^>]*>$`)
	commentReMCP    = regexp.MustCompile("(?s)^<!--.*-->$")
	disallowedReMCP = regexp.MustCompile(`(?i)(onload|onerror|onclick|document\s*\.\s*cookie|eval\s*\(|javascript:)`)
)

// tlsConfigInsecure returns an insecure TLS config for the custom-domain
// probe (issue #268): the probe must reach the origin before its Let's Encrypt
// cert is trusted. The content check remains the authoritative verification.
func tlsConfigInsecure() *tls.Config {
	return &tls.Config{InsecureSkipVerify: true} //nolint:gosec // probe must reach origin before cert is trusted
}

// ─── Analytics (issue #396) ────────────────────────────────────────────────
// Handlers live in analytics.go (formAnalytics / productAnalytics); they are
// registered in tools.go.

// ─── Custom domains (issue #396) ───────────────────────────────────────────

type domainOut struct {
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

func (s *Server) listDomains(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, *listOutput[domainOut], error) {
	acc, err := s.accountAndScope(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	if s.deps.Domains == nil {
		return nil, nil, errors.New("domain store tidak tersedia")
	}
	rows, err := s.deps.Domains.ListDomains(ctx, acc)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	out := make([]domainOut, 0, len(rows))
	for _, d := range rows {
		out = append(out, domainOut{ID: d.ID, Domain: d.Domain, Provider: d.Provider, Verified: d.Verified, AutoCNAME: d.AutoCNAME,
			TLSMode: d.TLSMode, TLSStatus: d.TLSStatus, TLSError: d.TLSError, TLSEmail: d.TLSEmail})
	}
	return nil, newListOut(out), nil
}

type addDomainIn struct {
	Domain string `json:"domain" jsonschema:"Nama domain (cth: kedai.com). Jangan sertakan https:// atau slash."`
}

func (s *Server) addDomain(ctx context.Context, _ *mcp.CallToolRequest, in addDomainIn) (*mcp.CallToolResult, map[string]any, error) {
	acc, err := s.requireWriteRole(ctx)
	if err != nil {
		return nil, nil, err
	}
	if s.deps.Domains == nil {
		return nil, nil, errors.New("domain store tidak tersedia")
	}
	domain := normalizeDomainMCP(in.Domain)
	if domain == "" || strings.HasSuffix(domain, "jomform.com") {
		return nil, nil, errors.New("domain tidak sah")
	}
	d, err := s.deps.Domains.AddDomain(ctx, acc, domain, "cname")
	if err != nil {
		return nil, nil, err
	}
	return nil, map[string]any{
		"id": d.ID, "domain": domain, "status": "pending_dns",
		"hint": "CNAME " + domain + " → custom.jomform.com, kemudian panggil verify_domain",
	}, nil
}

type domainIDIn struct {
	DomainID string `json:"domain_id" jsonschema:"ID domain (dari list_domains)"`
}

func (s *Server) verifyDomain(ctx context.Context, _ *mcp.CallToolRequest, in domainIDIn) (*mcp.CallToolResult, map[string]any, error) {
	acc, err := s.requireWriteRole(ctx)
	if err != nil {
		return nil, nil, err
	}
	if s.deps.Domains == nil {
		return nil, nil, errors.New("domain store tidak tersedia")
	}
	d, err := s.deps.Domains.GetByID(ctx, acc, in.DomainID)
	if err != nil {
		return nil, nil, errors.New("domain tidak dijumpai")
	}
	// Authoritative check (issue #237): the domain must actually serve a
	// JomForm page. SSRF-pinned probe (issue #366).
	if !probeServesJomFormMCP(d.Domain) {
		return nil, map[string]any{"verified": false, "detail": "verify_failed",
			"hint": "Pastikan CNAME " + d.Domain + " → custom.jomform.com (atau Cloudflare proxy), kemudian cuba lagi"}, nil
	}
	if err := s.deps.Domains.MarkVerified(ctx, acc, d.Domain); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]any{"verified": true, "domain": d.Domain}, nil
}

func (s *Server) removeDomain(ctx context.Context, _ *mcp.CallToolRequest, in domainIDIn) (*mcp.CallToolResult, map[string]string, error) {
	acc, err := s.requireWriteRole(ctx)
	if err != nil {
		return nil, nil, err
	}
	if s.deps.Domains == nil {
		return nil, nil, errors.New("domain store tidak tersedia")
	}
	if _, err := s.deps.Domains.GetByID(ctx, acc, in.DomainID); err != nil {
		return nil, nil, errors.New("domain tidak dijumpai")
	}
	if err := s.deps.Domains.RemoveDomain(ctx, acc, in.DomainID); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{"status": "removed", "domain_id": in.DomainID}, nil
}

// autoCNAMEDomain triggers the one-click Cloudflare CNAME creation for a
// domain (issue #322). Requires a linked Cloudflare credential.
func (s *Server) autoCNAMEDomain(ctx context.Context, _ *mcp.CallToolRequest, in domainIDIn) (*mcp.CallToolResult, map[string]any, error) {
	acc, err := s.requireWriteRole(ctx)
	if err != nil {
		return nil, nil, err
	}
	if s.deps.Domains == nil {
		return nil, nil, errors.New("domain store tidak tersedia")
	}
	d, err := s.deps.Domains.GetByID(ctx, acc, in.DomainID)
	if err != nil {
		return nil, nil, errors.New("domain tidak dijumpai")
	}
	if err := s.cfCreateCNAMEForDomainMCP(ctx, acc, d.Domain); err != nil {
		_ = s.deps.Domains.SetAutoCNAME(ctx, acc, d.Domain, "failed")
		return nil, map[string]any{"auto_cname": "failed", "error": err.Error()}, nil
	}
	_ = s.deps.Domains.SetAutoCNAME(ctx, acc, d.Domain, "created")
	return nil, map[string]any{"auto_cname": "created", "domain": d.Domain}, nil
}

// setDomainTLSModeMCP (issue #397): choose the per-domain TLS mode — "auto"
// (default, Let's Encrypt) or "custom" (bring-your-own-cert).
func (s *Server) setDomainTLSModeMCP(ctx context.Context, _ *mcp.CallToolRequest, in domainTLSModeIn) (*mcp.CallToolResult, map[string]any, error) {
	acc, err := s.requireWriteRole(ctx)
	if err != nil {
		return nil, nil, err
	}
	if s.deps.Domains == nil {
		return nil, nil, errors.New("domain store tidak tersedia")
	}
	d, err := s.deps.Domains.GetByID(ctx, acc, in.DomainID)
	if err != nil {
		return nil, nil, errors.New("domain tidak dijumpai")
	}
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	if mode != "auto" && mode != "custom" {
		return nil, nil, errors.New("mode tidak sah (auto|custom)")
	}
	if err := s.deps.Domains.SetTLSMode(ctx, acc, d.Domain, mode); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]any{"tls_mode": mode, "domain": d.Domain}, nil
}

type domainTLSModeIn struct {
	DomainID string `json:"domain_id" jsonschema:"ID domain (dari list_domains)"`
	Mode     string `json:"mode" jsonschema:"Mode TLS: auto (Let's Encrypt, default) atau custom (bring-your-own-cert)"`
}

// getDomainTLSEmailMCP (issue #398): read the per-domain ACME/Let's Encrypt
// contact email used to provision + renew the domain's auto-TLS cert. Empty
// means the domain falls back to the account email, then the global
// ACME_EMAIL env.
func (s *Server) getDomainTLSEmailMCP(ctx context.Context, _ *mcp.CallToolRequest, in domainIDIn) (*mcp.CallToolResult, map[string]any, error) {
	acc, err := s.accountAndScope(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	if s.deps.Domains == nil {
		return nil, nil, errors.New("domain store tidak tersedia")
	}
	d, err := s.deps.Domains.GetByID(ctx, acc, in.DomainID)
	if err != nil {
		return nil, nil, errors.New("domain tidak dijumpai")
	}
	return nil, map[string]any{"domain": d.Domain, "tls_email": d.TLSEmail}, nil
}

type domainTLSEmailIn struct {
	DomainID string `json:"domain_id" jsonschema:"ID domain (dari list_domains)"`
	Email    string `json:"email" jsonschema:"Emel ACME/Let's Encrypt untuk domain. Kosong = kembali ke emel akaun, kemudian ACME_EMAIL global."`
}

// setDomainTLSEmailMCP (issue #398): set the per-domain ACME/Let's Encrypt
// contact email used to provision + renew the domain's auto-TLS cert. An
// empty email clears it, so the domain falls back to the account email, then
// the global ACME_EMAIL env.
func (s *Server) setDomainTLSEmailMCP(ctx context.Context, _ *mcp.CallToolRequest, in domainTLSEmailIn) (*mcp.CallToolResult, map[string]any, error) {
	acc, err := s.requireWriteRole(ctx)
	if err != nil {
		return nil, nil, err
	}
	if s.deps.Domains == nil {
		return nil, nil, errors.New("domain store tidak tersedia")
	}
	d, err := s.deps.Domains.GetByID(ctx, acc, in.DomainID)
	if err != nil {
		return nil, nil, errors.New("domain tidak dijumpai")
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if email != "" && !strings.Contains(email, "@") {
		return nil, nil, errors.New("email tidak sah")
	}
	if err := s.deps.Domains.SetTLSEmail(ctx, acc, d.Domain, email); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]any{"domain": d.Domain, "tls_email": email}, nil
}

// setDomainCustomCertMCP (issue #397): store a merchant-supplied cert + key
// (bring-your-own-cert) for a verified domain and serve it via SNI.
func (s *Server) setDomainCustomCertMCP(ctx context.Context, _ *mcp.CallToolRequest, in domainCustomCertIn) (*mcp.CallToolResult, map[string]any, error) {
	acc, err := s.requireWriteRole(ctx)
	if err != nil {
		return nil, nil, err
	}
	if s.deps.Domains == nil {
		return nil, nil, errors.New("domain store tidak tersedia")
	}
	d, err := s.deps.Domains.GetByID(ctx, acc, in.DomainID)
	if err != nil {
		return nil, nil, errors.New("domain tidak dijumpai")
	}
	certPEM := strings.TrimSpace(in.Cert)
	keyPEM := strings.TrimSpace(in.Key)
	if certPEM == "" || keyPEM == "" {
		return nil, nil, errors.New("cert dan key wajib")
	}
	cert, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return nil, nil, errors.New("cert/key tidak sah — semak format PEM dan padanan kunci")
	}
	if len(cert.Certificate) == 0 {
		return nil, nil, errors.New("cert tidak sah")
	}
	certEnc, err := cryptoutil.Encrypt(s.decKey, []byte(certPEM))
	if err != nil {
		return nil, nil, err
	}
	keyEnc, err := cryptoutil.Encrypt(s.decKey, []byte(keyPEM))
	if err != nil {
		return nil, nil, err
	}
	if err := s.deps.Domains.SetCustomCert(ctx, acc, d.Domain, certEnc, keyEnc); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]any{"tls_mode": "custom", "tls_status": "custom", "domain": d.Domain}, nil
}

type domainCustomCertIn struct {
	DomainID string `json:"domain_id" jsonschema:"ID domain (dari list_domains)"`
	Cert     string `json:"cert" jsonschema:"PEM sijil (chain) untuk domain"`
	Key      string `json:"key" jsonschema:"PEM kunci peribadi yang sepadan"`
}

// normalizeDomainMCP strips scheme/slash and lowercases (mirror of the REST
// normalizeDomain; kept local to avoid an httpapi import cycle).
func normalizeDomainMCP(d string) string {
	d = strings.ToLower(strings.TrimSpace(d))
	d = strings.TrimPrefix(d, "https://")
	d = strings.TrimPrefix(d, "http://")
	d = strings.TrimSuffix(d, "/")
	if !strings.Contains(d, ".") {
		return ""
	}
	return d
}

// probeServesJomFormMCP is the authoritative custom-domain verification
// (issue #237): the domain must actually serve a JomForm page. SSRF-pinned
// (issue #366). Mirrors httpapi.probeServesJomForm.
func probeServesJomFormMCP(domain string) bool {
	rawURL := "https://" + domain + "/healthz"
	pin, err := ssrfguard.Pin(nil, domain)
	if err != nil {
		return false
	}
	defer pin.Close()
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DialContext:     pin.DialContext,
			TLSClientConfig: tlsConfigInsecure(), //nolint:gosec // probe must reach origin before cert is trusted
		},
	}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
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

// ─── Tracking (issue #396) ────────────────────────────────────────────────

type trackingOut struct {
	Head string `json:"head"`
}

func (s *Server) getTracking(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, trackingOut, error) {
	acc, err := s.accountAndScope(ctx, false)
	if err != nil {
		return nil, trackingOut{}, err
	}
	v, err := s.deps.Accounts.Tracking(ctx, acc)
	if err != nil {
		return nil, trackingOut{}, fmt.Errorf("db: %w", err)
	}
	return nil, trackingOut{Head: v}, nil
}

type setTrackingIn struct {
	Head string `json:"head" jsonschema:"Skrip <head> (Google Analytics, Facebook Pixel, dll). Hanya <script src=https://...>, <meta> dan komen dibenarkan."`
}

func (s *Server) setTracking(ctx context.Context, _ *mcp.CallToolRequest, in setTrackingIn) (*mcp.CallToolResult, map[string]string, error) {
	acc, err := s.requireWriteRole(ctx)
	if err != nil {
		return nil, nil, err
	}
	if err := s.deps.Accounts.SetTracking(ctx, acc, sanitizeTrackingMCP(in.Head)); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{"status": "saved"}, nil
}

// ─── CHIP account settings (issue #396) ───────────────────────────────────

type chipSettingsOut struct {
	Configured      bool     `json:"configured"`
	Verified        bool     `json:"verified"`
	Mode            string   `json:"mode"`
	BrandID         string   `json:"brand_id"`
	SecretKeyMasked string   `json:"secret_key_masked,omitempty"`
	EmailFallback   string   `json:"email_fallback,omitempty"`
	MethodWhitelist []string `json:"payment_method_whitelist,omitempty"`
}

func (s *Server) getChipSettings(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, chipSettingsOut, error) {
	acc, err := s.accountAndScope(ctx, false)
	if err != nil {
		return nil, chipSettingsOut{}, err
	}
	a, err := s.deps.Accounts.ByID(ctx, acc)
	if err != nil {
		return nil, chipSettingsOut{}, fmt.Errorf("db: %w", err)
	}
	out := chipSettingsOut{
		Configured: a.ChipBrandID != "" && a.ChipSecretKeyEnc != "",
		Verified:   a.ChipBrandID != "" && a.ChipSecretKeyEnc != "" && a.ChipVerified,
		Mode:       a.ChipMode, BrandID: a.ChipBrandID,
		EmailFallback: a.ChipEmailFallback, MethodWhitelist: a.ChipMethodWhitelist,
	}
	if out.Configured && s.decKey != nil {
		if secret, derr := cryptoutil.Decrypt(s.decKey, a.ChipSecretKeyEnc); derr == nil {
			out.SecretKeyMasked = maskSecretMCP(string(secret))
		}
	}
	return nil, out, nil
}

type setChipSettingsIn struct {
	BrandID         string   `json:"brand_id" jsonschema:"CHIP Brand ID (wajib)"`
	SecretKey       string   `json:"secret_key,omitempty" jsonschema:"CHIP Secret Key. Kosong = kekalkan kunci sedia ada."`
	Mode            string   `json:"mode,omitempty" jsonschema:"test|live (auto-derive dari prefix sk_test_/sk_live_ bila kosong)"`
	EmailFallback   string   `json:"email_fallback,omitempty" jsonschema:"Fallback emel bila buyer tak bagi emel"`
	MethodWhitelist []string `json:"payment_method_whitelist,omitempty" jsonschema:"Senarai kaedah bayaran, kosong = semua"`
}

func (s *Server) setChipSettings(ctx context.Context, _ *mcp.CallToolRequest, in setChipSettingsIn) (*mcp.CallToolResult, map[string]any, error) {
	acc, err := s.requireAdminRole(ctx)
	if err != nil {
		return nil, nil, err
	}
	if in.BrandID == "" {
		return nil, nil, errors.New("brand_id wajib")
	}
	secretEnc := ""
	existingMode := ""
	existingVerified := false
	if in.SecretKey == "" {
		if a, aerr := s.deps.Accounts.ByID(ctx, acc); aerr == nil {
			secretEnc = a.ChipSecretKeyEnc
			existingMode = a.ChipMode
			existingVerified = a.ChipVerified
		}
	} else {
		enc, eerr := cryptoutil.Encrypt(s.decKey, []byte(in.SecretKey))
		if eerr != nil {
			return nil, nil, eerr
		}
		secretEnc = enc
	}
	if secretEnc == "" {
		return nil, nil, errors.New("secret_key wajib")
	}
	mode := strings.ToLower(in.Mode)
	if in.SecretKey == "" && existingMode != "" {
		mode = existingMode
	} else if strings.HasPrefix(strings.ToLower(in.SecretKey), "sk_live") {
		mode = "live"
	} else if strings.HasPrefix(strings.ToLower(in.SecretKey), "sk_test") {
		mode = "test"
	}
	if mode != "live" && mode != "test" {
		mode = "test"
	}
	verified := false
	if s.chip != nil && in.SecretKey != "" {
		if verr := s.chip.ValidateCredentials(ctx, in.SecretKey); verr != nil {
			return nil, map[string]any{"error": "kredensial tidak sah — semak Brand ID / Secret Key", "verified": false}, nil
		}
		verified = true
	} else if in.SecretKey == "" {
		verified = existingVerified
	}
	if err := s.deps.Accounts.SetChipCredentials(ctx, acc, in.BrandID, secretEnc, mode, in.EmailFallback, in.MethodWhitelist, verified); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]any{"status": "configured", "mode": mode, "verified": verified}, nil
}

func (s *Server) clearChipSettings(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, map[string]string, error) {
	acc, err := s.requireAdminRole(ctx)
	if err != nil {
		return nil, nil, err
	}
	if err := s.deps.Accounts.ClearChipCredentials(ctx, acc); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{"status": "cleared"}, nil
}

// ─── Profile avatar (issue #396) ──────────────────────────────────────────

type updateAvatarIn struct {
	AvatarURL string `json:"avatar_url" jsonschema:"URL avatar (mesti imej yang dihoskan JomForm dari upload bucket). Kosong = buang avatar."`
}

func (s *Server) updateProfileAvatar(ctx context.Context, _ *mcp.CallToolRequest, in updateAvatarIn) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #645: user-row write — API-key write scope is the gate.
	if err := s.requireWriteScope(ctx); err != nil {
		return nil, nil, err
	}
	if s.deps.Users == nil {
		return nil, nil, errors.New("perkhidmatan tidak tersedia")
	}
	userID := s.userIDFor(ctx)
	if userID == "" {
		return nil, nil, errors.New("auth diperlukan")
	}
	u := strings.TrimSpace(in.AvatarURL)
	if u != "" && !jomformMediaURLMCP(u) {
		return nil, nil, errors.New("URL avatar tidak sah (imej daripada pelayan JomForm diperlukan)")
	}
	if err := s.deps.Users.UpdateAvatar(ctx, userID, u); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{"avatar_url": u}, nil
}

// ─── Passkeys (issue #396) ───────────────────────────────────────────────

type passkeyOut struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at,omitempty"`
}

func (s *Server) listPasskeys(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, *listOutput[passkeyOut], error) {
	if s.deps.Passkeys == nil {
		return nil, nil, errors.New("passkey store tidak tersedia")
	}
	userID := s.userIDFor(ctx)
	if userID == "" {
		return nil, nil, errors.New("auth diperlukan")
	}
	creds, err := s.deps.Passkeys.ByUser(ctx, userID)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	out := make([]passkeyOut, 0, len(creds))
	for _, c := range creds {
		out = append(out, passkeyOut{ID: base64URLMCP(c.ID)})
	}
	return nil, newListOut(out), nil
}

type deletePasskeyIn struct {
	PasskeyID string `json:"passkey_id" jsonschema:"ID passkey (dari list_passkeys)"`
}

func (s *Server) deletePasskey(ctx context.Context, _ *mcp.CallToolRequest, in deletePasskeyIn) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #645: user-row write (a passkey belongs to the person) — API-key
	// write scope is the gate.
	if err := s.requireWriteScope(ctx); err != nil {
		return nil, nil, err
	}
	if s.deps.Passkeys == nil {
		return nil, nil, errors.New("passkey store tidak tersedia")
	}
	userID := s.userIDFor(ctx)
	if userID == "" {
		return nil, nil, errors.New("auth diperlukan")
	}
	raw, err := base64URLDecodeMCP(in.PasskeyID)
	if err != nil {
		return nil, nil, errors.New("id tidak sah")
	}
	if err := s.deps.Passkeys.Delete(ctx, userID, raw); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{"status": "deleted"}, nil
}

// ─── Social accounts (issue #396) ─────────────────────────────────────────

type socialAccountOut struct {
	Provider string `json:"provider"`
	Email    string `json:"email"`
	Name     string `json:"name,omitempty"`
	Picture  string `json:"picture,omitempty"`
	LinkedAt string `json:"linked_at"`
}

func (s *Server) listSocialAccounts(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, *listOutput[socialAccountOut], error) {
	if s.deps.Users == nil {
		return nil, nil, errors.New("perkhidmatan tidak tersedia")
	}
	userID := s.userIDFor(ctx)
	if userID == "" {
		return nil, nil, errors.New("auth diperlukan")
	}
	links, err := s.deps.Users.ListIdentities(ctx, userID)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	out := make([]socialAccountOut, 0, len(links))
	for _, l := range links {
		out = append(out, socialAccountOut{
			Provider: l.Provider, Email: l.Email, Name: l.DisplayName,
			Picture: l.Picture, LinkedAt: l.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	return nil, newListOut(out), nil
}

type unlinkSocialIn struct {
	Provider string `json:"provider" jsonschema:"Pembekal sosial (google|azure_ad|github|cloudflare)"`
}

func (s *Server) unlinkSocialAccount(ctx context.Context, _ *mcp.CallToolRequest, in unlinkSocialIn) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #645: unlinking a social identity is a WRITE on the person's own
	// account. The scope axis is the gate here (there is no workspace role on a
	// user-level write); without it a readonly key could strip a login method.
	if err := s.requireWriteScope(ctx); err != nil {
		return nil, nil, err
	}
	if s.deps.Users == nil {
		return nil, nil, errors.New("perkhidmatan tidak tersedia")
	}
	userID := s.userIDFor(ctx)
	if userID == "" {
		return nil, nil, errors.New("auth diperlukan")
	}
	if in.Provider == "" {
		return nil, nil, errors.New("provider wajib")
	}
	if err := s.deps.Users.UnlinkIdentity(ctx, userID, in.Provider); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{"status": "unlinked", "provider": in.Provider}, nil
}

// ─── 2FA (issue #396) ─────────────────────────────────────────────────────

type twoFAStatusOut struct {
	Enabled bool `json:"enabled"`
}

func (s *Server) getTwoFAStatus(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, twoFAStatusOut, error) {
	if s.deps.Users == nil {
		return nil, twoFAStatusOut{}, errors.New("perkhidmatan tidak tersedia")
	}
	userID := s.userIDFor(ctx)
	if userID == "" {
		return nil, twoFAStatusOut{}, errors.New("auth diperlukan")
	}
	u, err := s.deps.Users.ByID(ctx, userID)
	if err != nil || u == nil {
		return nil, twoFAStatusOut{}, errors.New("pengguna tidak dijumpai")
	}
	return nil, twoFAStatusOut{Enabled: u.TOTPEnabled}, nil
}

type twoFASetupOut struct {
	Secret     string `json:"secret"`
	OtpauthURL string `json:"otpauth_url"`
}

func (s *Server) setupTwoFA(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, twoFASetupOut, error) {
	// Issue #645: 2FA is a credential change for the person — API-key write
	// scope is the gate.
	if err := s.requireWriteScope(ctx); err != nil {
		return nil, twoFASetupOut{}, err
	}
	if s.deps.Users == nil || s.deps.Sessions == nil {
		return nil, twoFASetupOut{}, errors.New("perkhidmatan tidak tersedia")
	}
	userID := s.userIDFor(ctx)
	if userID == "" {
		return nil, twoFASetupOut{}, errors.New("auth diperlukan")
	}
	u, err := s.deps.Users.ByID(ctx, userID)
	if err != nil || u == nil {
		return nil, twoFASetupOut{}, errors.New("pengguna tidak dijumpai")
	}
	secret, otpauthURL, err := auth.GenerateTOTPSecret(u.Email)
	if err != nil {
		return nil, twoFASetupOut{}, err
	}
	if err := s.deps.Sessions.PendingTOTP().Set(ctx, userID, secret); err != nil {
		return nil, twoFASetupOut{}, fmt.Errorf("db: %w", err)
	}
	return nil, twoFASetupOut{Secret: secret, OtpauthURL: otpauthURL}, nil
}

type enableTwoFAIn struct {
	Code string `json:"code" jsonschema:"Kod 6-digit dari aplikasi authenticator (selepas setup_2fa)"`
}

func (s *Server) enableTwoFA(ctx context.Context, _ *mcp.CallToolRequest, in enableTwoFAIn) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #645: enabling 2FA is a credential change — API-key write scope gate.
	if err := s.requireWriteScope(ctx); err != nil {
		return nil, nil, err
	}
	if s.deps.Users == nil || s.deps.Sessions == nil {
		return nil, nil, errors.New("perkhidmatan tidak tersedia")
	}
	userID := s.userIDFor(ctx)
	if userID == "" {
		return nil, nil, errors.New("auth diperlukan")
	}
	if in.Code == "" {
		return nil, nil, errors.New("kod diperlukan")
	}
	pending, err := s.deps.Sessions.PendingTOTP().Get(ctx, userID)
	if err != nil || pending == "" {
		return nil, nil, errors.New("2fa belum di-setup atau ralat")
	}
	if !auth.ValidateTOTPSecret(pending, in.Code) {
		return nil, nil, errors.New("kod tidak sah")
	}
	if err := s.deps.Users.EnableTOTP(ctx, userID, pending); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	_ = s.deps.Sessions.PendingTOTP().Delete(ctx, userID)
	return nil, map[string]string{"status": "enabled"}, nil
}

type disableTwoFAIn struct {
	Password string `json:"password" jsonschema:"Kata laluan akaun untuk pengesahan"`
}

func (s *Server) disableTwoFA(ctx context.Context, _ *mcp.CallToolRequest, in disableTwoFAIn) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #645: disabling 2FA weakens a credential — API-key write scope gate.
	if err := s.requireWriteScope(ctx); err != nil {
		return nil, nil, err
	}
	if s.deps.Users == nil {
		return nil, nil, errors.New("perkhidmatan tidak tersedia")
	}
	userID := s.userIDFor(ctx)
	if userID == "" {
		return nil, nil, errors.New("auth diperlukan")
	}
	u, err := s.deps.Users.ByID(ctx, userID)
	if err != nil || u == nil {
		return nil, nil, errors.New("pengguna tidak dijumpai")
	}
	ok, err := auth.VerifyPassword(in.Password, u.PasswordHash)
	if err != nil || !ok {
		return nil, nil, errors.New("kata laluan tidak sah")
	}
	if err := s.deps.Users.DisableTOTP(ctx, userID); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{"status": "disabled"}, nil
}

// ─── Trusted devices (issue #454) ─────────────────────────────────────────
//
// MCP parity for the Profile → Security trusted-devices management list: list
// the account owner's trusted 2FA devices, revoke one by its opaque id, or
// revoke all ("log out everywhere"). Thin wrappers over the same
// auth.TrustedDevices store the REST handlers use.

// trustDeviceMCPOut is one trusted device as returned to an agent.
type trustDeviceMCPOut struct {
	ID        string `json:"id"`
	Label     string `json:"label,omitempty"`
	IP        string `json:"ip,omitempty"`
	FirstSeen string `json:"first_seen,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

// listTrustedDevices (issue #454): list the account owner's trusted devices.
func (s *Server) listTrustedDevices(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, *listOutput[trustDeviceMCPOut], error) {
	if s.deps.Sessions == nil {
		return nil, nil, errors.New("perkhidmatan tidak tersedia")
	}
	userID := s.userIDFor(ctx)
	if userID == "" {
		return nil, nil, errors.New("auth diperlukan")
	}
	devices, err := s.deps.Sessions.TrustedDevices().List(ctx, userID, "")
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	out := make([]trustDeviceMCPOut, 0, len(devices))
	for _, d := range devices {
		out = append(out, trustDeviceMCPOut{
			ID: d.ID, Label: d.Label, IP: d.IP,
			FirstSeen: d.FirstSeen, ExpiresAt: d.ExpiresAt,
		})
	}
	return nil, newListOut(out), nil
}

type revokeTrustedDeviceIn struct {
	DeviceID string `json:"device_id" jsonschema:"ID peranti dipercayai (dari list_trusted_devices)"`
}

// revokeTrustedDevice (issue #454): revoke ONE trusted device by id. The next
// login on that device is challenged for a 2FA code again (immediate).
func (s *Server) revokeTrustedDevice(ctx context.Context, _ *mcp.CallToolRequest, in revokeTrustedDeviceIn) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #645: revoking a trusted device is a write on the person's account.
	if err := s.requireWriteScope(ctx); err != nil {
		return nil, nil, err
	}
	if s.deps.Sessions == nil {
		return nil, nil, errors.New("perkhidmatan tidak tersedia")
	}
	userID := s.userIDFor(ctx)
	if userID == "" {
		return nil, nil, errors.New("auth diperlukan")
	}
	if strings.TrimSpace(in.DeviceID) == "" {
		return nil, nil, errors.New("device_id wajib")
	}
	if err := s.deps.Sessions.TrustedDevices().RevokeHash(ctx, userID, strings.TrimSpace(in.DeviceID)); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{"status": "revoked", "device_id": in.DeviceID}, nil
}

// revokeAllTrustedDevices (issue #454): "log out everywhere" — revoke every
// trusted device for the account owner.
func (s *Server) revokeAllTrustedDevices(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #645: "log out everywhere" is a write on the person's account.
	if err := s.requireWriteScope(ctx); err != nil {
		return nil, nil, err
	}
	if s.deps.Sessions == nil {
		return nil, nil, errors.New("perkhidmatan tidak tersedia")
	}
	userID := s.userIDFor(ctx)
	if userID == "" {
		return nil, nil, errors.New("auth diperlukan")
	}
	if err := s.deps.Sessions.TrustedDevices().RevokeAll(ctx, userID); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{"status": "revoked_all"}, nil
}

// ─── System settings (issue #396) ─────────────────────────────────────────

type systemSettingOut struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func (s *Server) getSystemSettings(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, *listOutput[systemSettingOut], error) {
	if s.deps.SystemSettings == nil {
		return nil, nil, errors.New("system settings tidak tersedia")
	}
	all, err := s.deps.SystemSettings.All(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	out := make([]systemSettingOut, 0, len(all))
	for _, kv := range all {
		out = append(out, systemSettingOut{Key: kv["key"], Value: kv["value"]})
	}
	return nil, newListOut(out), nil
}

type setSystemSettingIn struct {
	Key   string `json:"key" jsonschema:"Kunci tetapan (mesti bermula site_ atau landing_)"`
	Value string `json:"value" jsonschema:"Nilai tetapan"`
}

func (s *Server) setSystemSetting(ctx context.Context, _ *mcp.CallToolRequest, in setSystemSettingIn) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #645: platform-wide settings are a write — API-key write scope gate.
	if err := s.requireWriteScope(ctx); err != nil {
		return nil, nil, err
	}
	if s.deps.SystemSettings == nil {
		return nil, nil, errors.New("system settings tidak tersedia")
	}
	if in.Key == "" {
		return nil, nil, errors.New("key wajib")
	}
	if !strings.HasPrefix(in.Key, "site_") && !strings.HasPrefix(in.Key, "landing_") {
		return nil, nil, errors.New("key tidak dibenarkan")
	}
	if err := s.deps.SystemSettings.Set(ctx, in.Key, in.Value); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{"status": "saved", "key": in.Key}, nil
}

// ─── Sales export CSV (issue #396) ─────────────────────────────────────────

// exportSalesIn is the export_sales_csv input. Status/date/q are accepted so an
// agent can export the SAME set the list_sales call (or the dashboard's filtered
// page) shows — an export that ignored the filters produced a different data set
// from the list it mirrors.
type exportSalesIn struct {
	FormID   string `json:"form_id,omitempty" jsonschema:"Optional — hadkan eksport kepada jualan borang ini"`
	Status   string `json:"status,omitempty" jsonschema:"Optional — hadkan kepada status: pending|paid|confirmed|failed|cancelled"`
	DateFrom string `json:"date_from,omitempty" jsonschema:"Optional — YYYY-MM-DD, termasuk hari itu"`
	DateTo   string `json:"date_to,omitempty" jsonschema:"Optional — YYYY-MM-DD, termasuk hari itu"`
	Q        string `json:"q,omitempty" jsonschema:"Optional — cari pada kod rujukan/nama pembeli (jualan) atau nilai jawapan (respons)"`
}

func (s *Server) exportSalesCSV(ctx context.Context, _ *mcp.CallToolRequest, in exportSalesIn) (*mcp.CallToolResult, map[string]string, error) {
	accs, err := s.accountScopes(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	if s.deps.Sales == nil {
		return nil, nil, fmt.Errorf("sales tidak tersedia")
	}
	// Same scope decision as list_sales and as the REST export: an unfiltered
	// export covers the account's sales AND its response entries, and a status
	// filter excludes the responses half (a submission has no status).
	sc, err := s.recordScope(ctx, accs, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	filter := dbstore.RecordFilter{
		FormID: in.FormID, Status: in.Status,
		DateFrom: in.DateFrom, DateTo: in.DateTo, Q: in.Q,
	}
	records, _, err := s.deps.Sales.MergedAll(ctx, accs, sc, filter)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}

	var sb strings.Builder
	cw := csv.NewWriter(&sb)
	if sc.OnlyResponses {
		// A response form's export is its recorded answers, not empty sales
		// columns — the same choice REST's export makes (issue #604).
		writeSubmissionsCSV(cw, records)
		cw.Flush()
		return nil, map[string]string{"csv": sb.String(), "count": fmt.Sprintf("%d", len(records))}, nil
	}
	// Issue #518: mirror the REST export's refund columns so an agent gets the
	// same partial-refund picture from the MCP CSV.
	header := []string{"ref_code", "status", "payment_method", "buyer_name", "buyer_email",
		"total_rm", "created_at", "purchase_id", "table_number",
		"refunded_rm", "outstanding_rm", "refund_status"}
	keys, haveSubs := recordAnswerKeys(records)
	if haveSubs {
		header = append([]string{"record_type", "submission_id"}, header...)
		header = append(header, keys...)
	}
	_ = cw.Write(header)
	for _, rec := range records {
		if rec.Sale != nil {
			sl := rec.Sale
			row := []string{
				sl.RefCode, sl.Status, sl.PaymentMethod,
				csvguard.Cell(sl.Client.FullName), csvguard.Cell(sl.Client.Email),
				fmt.Sprintf("%.2f", float64(sl.TotalSen)/100),
				sl.CreatedAt.UTC().Format("2006-01-02 15:04:05"), sl.ChipPurchaseID,
				csvguard.Cell(sl.TableNumber),
				fmt.Sprintf("%.2f", float64(sl.RefundedSen)/100),
				fmt.Sprintf("%.2f", float64(sl.OutstandingSen())/100),
				sl.RefundStatus(),
			}
			if haveSubs {
				row = append([]string{dbstore.RecordKindSale, ""}, row...)
				for range keys {
					row = append(row, "")
				}
			}
			_ = cw.Write(row)
			continue
		}
		if rec.Submission == nil {
			continue
		}
		sub := rec.Submission
		row := []string{
			sub.ID, "response", "",
			csvguard.Cell(subStrField(sub, "full_name")), csvguard.Cell(subStrField(sub, "email")),
			"0.00",
			sub.CreatedAt.UTC().Format("2006-01-02 15:04:05"), "", "",
			"0.00", "0.00", dbstore.RefundNone,
		}
		if haveSubs {
			row = append([]string{dbstore.RecordKindSubmission, sub.ID}, row...)
		}
		for _, k := range keys {
			row = append(row, csvguard.Cell(submissionCSVText(sub.Data[k])))
		}
		_ = cw.Write(row)
	}
	cw.Flush()
	return nil, map[string]string{"csv": sb.String(), "count": fmt.Sprintf("%d", len(records))}, nil
}

// recordAnswerKeys collects the distinct answer keys across a record set's
// response rows (sorted, so a form always exports the same columns) and reports
// whether the set contains any response row at all.
func recordAnswerKeys(records []*dbstore.Record) ([]string, bool) {
	keys := []string{}
	seen := map[string]bool{}
	have := false
	for _, rec := range records {
		if rec.Submission == nil {
			continue
		}
		have = true
		for k := range rec.Submission.Data {
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}
	sort.Strings(keys)
	return keys, have
}

func writeSubmissionsCSV(cw *csv.Writer, records []*dbstore.Record) {
	subs := make([]*dbstore.Submission, 0, len(records))
	for _, rec := range records {
		if rec.Submission != nil {
			subs = append(subs, rec.Submission)
		}
	}
	if len(subs) == 0 {
		_ = cw.Write([]string{"submission_id", "created_at"})
		return
	}
	keys := []string{}
	seen := map[string]bool{}
	for _, sub := range subs {
		for k := range sub.Data {
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}
	sort.Strings(keys)
	_ = cw.Write(append([]string{"submission_id", "created_at"}, keys...))
	for _, sub := range subs {
		row := []string{sub.ID, sub.CreatedAt.UTC().Format("2006-01-02 15:04:05")}
		for _, k := range keys {
			row = append(row, csvguard.Cell(submissionCSVText(sub.Data[k])))
		}
		_ = cw.Write(row)
	}
}

func subStrField(sub *dbstore.Submission, key string) string {
	if sub == nil || sub.Data == nil {
		return ""
	}
	v, _ := sub.Data[key].(string)
	return v
}

// submissionCSVText renders one recorded answer as CSV text; non-string values
// are emitted as their JSON text rather than a Go-formatting artifact.
func submissionCSVText(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	default:
		if b, err := json.Marshal(v); err == nil {
			return string(b)
		}
		return ""
	}
}

// ─── Cloudflare (issue #396) ──────────────────────────────────────────────

type cloudflareStatusOut struct {
	Linked     bool `json:"linked"`
	Configured bool `json:"configured"`
}

func (s *Server) getCloudflareStatus(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, cloudflareStatusOut, error) {
	acc, err := s.accountAndScope(ctx, false)
	if err != nil {
		return nil, cloudflareStatusOut{}, err
	}
	linked := false
	if sealed, cerr := s.deps.Accounts.CFToken(ctx, acc); cerr == nil && sealed != "" {
		if _, derr := cfUnsealMCP(s.decKey, sealed); derr == nil {
			linked = true
		}
	}
	return nil, cloudflareStatusOut{Linked: linked, Configured: cfOAuthConfiguredMCP()}, nil
}

type connectCFAPITokenIn struct {
	AccountEmail string `json:"account_email" jsonschema:"Emel akaun Cloudflare"`
	APIToken     string `json:"api_token" jsonschema:"API token Cloudflare (disahkan serta-merta, disimpan terenkripsi)"`
	AccountName  string `json:"account_name,omitempty" jsonschema:"Label pilihan"`
}

func (s *Server) connectCloudflareAPIToken(ctx context.Context, _ *mcp.CallToolRequest, in connectCFAPITokenIn) (*mcp.CallToolResult, map[string]bool, error) {
	acc, err := s.requireWriteRole(ctx)
	if err != nil {
		return nil, nil, err
	}
	in.AccountEmail = strings.TrimSpace(in.AccountEmail)
	in.APIToken = strings.TrimSpace(in.APIToken)
	if in.AccountEmail == "" || in.APIToken == "" {
		return nil, nil, errors.New("account email dan API token wajib")
	}
	if err := cfVerifyAPITokenMCP(ctx, in.APIToken); err != nil {
		return nil, nil, fmt.Errorf("API token tidak sah atau tamat tempoh: %w", err)
	}
	blob, _ := json.Marshal(cfCredMCP{Kind: cfKindAPITokenMCP, Access: in.APIToken, AccountEmail: in.AccountEmail, AccountName: in.AccountName})
	sealed, err := cryptoutil.Encrypt(s.decKey, blob)
	if err != nil {
		return nil, nil, err
	}
	if err := s.deps.Accounts.SaveCFToken(ctx, acc, sealed); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]bool{"linked": true}, nil
}

type connectCFGlobalKeyIn struct {
	AccountEmail string `json:"account_email" jsonschema:"Emel akaun Cloudflare"`
	APIKey       string `json:"api_key" jsonschema:"Global API key Cloudflare (disahkan serta-merta, disimpan terenkripsi)"`
	AccountName  string `json:"account_name,omitempty" jsonschema:"Label pilihan"`
}

func (s *Server) connectCloudflareGlobalKey(ctx context.Context, _ *mcp.CallToolRequest, in connectCFGlobalKeyIn) (*mcp.CallToolResult, map[string]bool, error) {
	acc, err := s.requireWriteRole(ctx)
	if err != nil {
		return nil, nil, err
	}
	in.AccountEmail = strings.TrimSpace(in.AccountEmail)
	in.APIKey = strings.TrimSpace(in.APIKey)
	if in.AccountEmail == "" || in.APIKey == "" {
		return nil, nil, errors.New("account email dan global API key wajib")
	}
	if err := cfVerifyGlobalKeyMCP(ctx, in.AccountEmail, in.APIKey); err != nil {
		return nil, nil, fmt.Errorf("global API key tidak sah: %w", err)
	}
	blob, _ := json.Marshal(cfCredMCP{Kind: cfKindGlobalKeyMCP, Access: in.APIKey, AccountEmail: in.AccountEmail, AccountName: in.AccountName})
	sealed, err := cryptoutil.Encrypt(s.decKey, blob)
	if err != nil {
		return nil, nil, err
	}
	if err := s.deps.Accounts.SaveCFToken(ctx, acc, sealed); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]bool{"linked": true}, nil
}

func (s *Server) disconnectCloudflare(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, map[string]string, error) {
	acc, err := s.requireWriteRole(ctx)
	if err != nil {
		return nil, nil, err
	}
	if err := s.deps.Accounts.SaveCFToken(ctx, acc, ""); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{"status": "disconnected"}, nil
}

// ─── Webhook signing key (issue #396) ──────────────────────────────────────

type webhookKeyOut struct {
	KeyID     string `json:"key_id"`
	PublicKey string `json:"public_key"`
	Alg       string `json:"alg"`
}

func (s *Server) getWebhookSigningKey(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, webhookKeyOut, error) {
	acc, err := s.accountAndScope(ctx, false)
	if err != nil {
		return nil, webhookKeyOut{}, err
	}
	// Issue #645: the tool is a pure READ when the account already has a key,
	// but ensureSigningKeyMCP GENERATES AND SAVES one on first use — a write.
	// A readonly key may read an existing public key and must not create one.
	if pubB64, privB64, kerr := s.deps.Accounts.SigningKeys(ctx, acc); kerr == nil && (pubB64 == "" || privB64 == "") {
		if werr := s.requireWriteScope(ctx); werr != nil {
			return nil, webhookKeyOut{}, werr
		}
	}
	pub, _ := ensureSigningKeyMCP(ctx, s, acc)
	if pub == nil {
		return nil, webhookKeyOut{}, errors.New("gagal jana kunci tandatangan")
	}
	return nil, webhookKeyOut{
		KeyID: acc, PublicKey: base64.StdEncoding.EncodeToString(pub), Alg: "ed25519",
	}, nil
}

// ─── local helpers (mirrors of httpapi; kept here to avoid an import cycle) ─

// sanitizeTrackingMCP allows only whitelisted benign <head> markup.
func sanitizeTrackingMCP(js string) string {
	out := []string{}
	for _, seg := range strings.Split(strings.TrimSpace(js), "\n") {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		if commentReMCP.MatchString(seg) || scriptSrcReMCP.MatchString(seg) || metaReMCP.MatchString(seg) {
			if !disallowedReMCP.MatchString(seg) {
				out = append(out, seg)
			}
		}
	}
	return strings.Join(out, "\n")
}

func maskSecretMCP(secret string) string {
	s := strings.TrimSpace(secret)
	if s == "" {
		return ""
	}
	const tailLen = 4
	if len(s) <= tailLen {
		return "••••"
	}
	return s[:len(s)-tailLen] + "••••" + s[len(s)-tailLen:]
}

func base64URLMCP(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
func base64URLDecodeMCP(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

// jomformMediaURLMCP reports whether u is a URL served from this deployment's
// media public base (mirror of httpapi.jomformMediaURL).
func jomformMediaURLMCP(u string) bool {
	if u == "" {
		return false
	}
	if strings.HasPrefix(u, "/") {
		return strings.Contains(u, "/u/") || strings.Contains(u, "uploads")
	}
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		lower := strings.ToLower(u)
		return strings.Contains(lower, "/u/") || strings.Contains(lower, "upload")
	}
	return false
}

// ensureSigningKeyMCP loads (or creates) the account's Ed25519 keypair
// (mirror of httpapi.ensureSigningKey).
func ensureSigningKeyMCP(ctx context.Context, s *Server, accountID string) (ed25519.PublicKey, ed25519.PrivateKey) {
	pubB64, privB64, err := s.deps.Accounts.SigningKeys(ctx, accountID)
	if err != nil || pubB64 == "" || privB64 == "" {
		_, privKey, gerr := ed25519.GenerateKey(rand.Reader)
		if gerr != nil {
			return nil, nil
		}
		pubB64 = base64.StdEncoding.EncodeToString(privKey.Public().(ed25519.PublicKey))
		privB64 = base64.StdEncoding.EncodeToString(privKey)
		if serr := s.deps.Accounts.SaveSigningKeys(ctx, accountID, pubB64, privB64); serr != nil {
			return nil, nil
		}
	}
	pubB, _ := base64.StdEncoding.DecodeString(pubB64)
	privB, _ := base64.StdEncoding.DecodeString(privB64)
	return ed25519.PublicKey(pubB), ed25519.PrivateKey(privB)
}

// ─── Cloudflare helpers (mirrors of httpapi/cf_oauth.go) ──────────────────

const (
	cfAPIBaseMCP = "https://api.cloudflare.com/client/v4"
)

type cfCredKindMCP string

const (
	cfKindAPITokenMCP  cfCredKindMCP = "apitoken"
	cfKindGlobalKeyMCP cfCredKindMCP = "globalkey"
)

type cfCredMCP struct {
	Kind         cfCredKindMCP `json:"kind"`
	Access       string        `json:"access,omitempty"`
	AccountEmail string        `json:"account_email,omitempty"`
	AccountName  string        `json:"account_name,omitempty"`
}

func cfOAuthConfiguredMCP() bool { return false } // OAuth link is browser-only; manual kinds are MCP-connectable

func cfUnsealMCP(key []byte, sealed string) (*cfCredMCP, error) {
	plain, err := cryptoutil.Decrypt(key, sealed)
	if err != nil {
		return nil, err
	}
	var cred cfCredMCP
	if err := json.Unmarshal(plain, &cred); err != nil {
		return nil, err
	}
	if cred.Kind == "" {
		cred.Kind = cfKindAPITokenMCP
	}
	return &cred, nil
}

func cfVerifyAPITokenMCP(ctx context.Context, token string) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, cfAPIBaseMCP+"/user/tokens/verify", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return errors.New("token ditolak oleh Cloudflare")
	}
	var vr struct {
		Success bool `json:"success"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&vr)
	if !vr.Success {
		return errors.New("token tidak sah")
	}
	return nil
}

func cfVerifyGlobalKeyMCP(ctx context.Context, email, key string) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, cfAPIBaseMCP+"/user", nil)
	req.Header.Set("X-Auth-Email", email)
	req.Header.Set("X-Auth-Key", key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return errors.New("email atau key ditolak oleh Cloudflare")
	}
	var ur struct {
		Success bool `json:"success"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&ur)
	if !ur.Success {
		return errors.New("email atau key tidak sah")
	}
	return nil
}

// cfAPICallMCP performs an authenticated Cloudflare API request using the
// stored manual credential (api token or global key). OAuth links are not
// connectable via MCP (browser-only), so only manual kinds are supported here.
func (s *Server) cfAPICallMCP(ctx context.Context, accountID, method, path string, body io.Reader) ([]byte, error) {
	sealed, err := s.deps.Accounts.CFToken(ctx, accountID)
	if err != nil || sealed == "" {
		return nil, errors.New("cloudflare not linked")
	}
	cred, err := cfUnsealMCP(s.decKey, sealed)
	if err != nil {
		return nil, fmt.Errorf("unseal cf token: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, method, cfAPIBaseMCP+path, body)
	if err != nil {
		return nil, err
	}
	switch cred.Kind {
	case cfKindGlobalKeyMCP:
		req.Header.Set("X-Auth-Email", cred.AccountEmail)
		req.Header.Set("X-Auth-Key", cred.Access)
	default:
		req.Header.Set("Authorization", "Bearer "+cred.Access)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("cloudflare http %d: %s", resp.StatusCode, truncateBytesMCP(b, 512))
	}
	return b, nil
}

func (s *Server) cfResolveZoneMCP(ctx context.Context, accountID, domain string) (string, error) {
	body, err := s.cfAPICallMCP(ctx, accountID, http.MethodGet, "/zones?per_page=50", nil)
	if err != nil {
		return "", err
	}
	var zr struct {
		Result []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &zr); err != nil {
		return "", err
	}
	var best, bestName string
	for _, z := range zr.Result {
		if z.Name == domain {
			return z.ID, nil
		}
		if strings.HasSuffix(domain, "."+z.Name) && len(z.Name) > len(bestName) {
			best, bestName = z.ID, z.Name
		}
	}
	if best == "" {
		return "", fmt.Errorf("no CF zone covers %s", domain)
	}
	return best, nil
}

func (s *Server) cfCreateCNAMEMCP(ctx context.Context, accountID, zoneID, domain, target string) error {
	payload := fmt.Sprintf(`{"type":"CNAME","name":%q,"content":%q,"proxied":true}`, domain, target)
	_, err := s.cfAPICallMCP(ctx, accountID, http.MethodPost, "/zones/"+url.PathEscape(zoneID)+"/dns_records", strings.NewReader(payload))
	return err
}

func (s *Server) cfCreateCNAMEForDomainMCP(ctx context.Context, accountID, domain string) error {
	zoneID, err := s.cfResolveZoneMCP(ctx, accountID, domain)
	if err != nil {
		return err
	}
	return s.cfCreateCNAMEMCP(ctx, accountID, zoneID, domain, "jomform.com")
}

func truncateBytesMCP(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n])
	}
	return string(b)
}
