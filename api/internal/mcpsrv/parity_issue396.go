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

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/cryptoutil"
	"github.com/wzul/jomform/api/internal/csvguard"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/ssrfguard"
	"github.com/wzul/jomform/api/internal/textutil"
	"github.com/wzul/jomform/api/internal/totpconsume"
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
		return nil, nil, errors.New("domain store not available")
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
	Domain string `json:"domain" jsonschema:"Domain name (e.g. kedai.com). Do not include https:// or a slash."`
}

func (s *Server) addDomain(ctx context.Context, _ *mcp.CallToolRequest, in addDomainIn) (*mcp.CallToolResult, map[string]any, error) {
	acc, err := s.requireWriteRole(ctx)
	if err != nil {
		return nil, nil, err
	}
	if s.deps.Domains == nil {
		return nil, nil, errors.New("domain store not available")
	}
	domain := normalizeDomainMCP(in.Domain)
	if domain == "" || strings.HasSuffix(domain, "jomform.com") {
		return nil, nil, errors.New("invalid domain")
	}
	d, err := s.deps.Domains.AddDomain(ctx, acc, domain, "cname")
	if err != nil {
		return nil, nil, err
	}
	return nil, map[string]any{
		"id": d.ID, "domain": domain, "status": "pending_dns",
		"hint": "CNAME " + domain + " → " + s.cnameHost() + ", then call verify_domain",
	}, nil
}

type domainIDIn struct {
	DomainID string `json:"domain_id" jsonschema:"Domain ID (from list_domains)"`
}

func (s *Server) verifyDomain(ctx context.Context, _ *mcp.CallToolRequest, in domainIDIn) (*mcp.CallToolResult, map[string]any, error) {
	acc, err := s.requireWriteRole(ctx)
	if err != nil {
		return nil, nil, err
	}
	if s.deps.Domains == nil {
		return nil, nil, errors.New("domain store not available")
	}
	d, err := s.deps.Domains.GetByID(ctx, acc, in.DomainID)
	if err != nil {
		return nil, nil, errors.New("domain not found")
	}
	// Authoritative check (issue #237): the domain must actually serve a
	// JomForm page. SSRF-pinned probe (issue #366).
	if !probeServesJomFormMCP(d.Domain) {
		return nil, map[string]any{"verified": false, "detail": "verify_failed",
			"hint": "Make sure the CNAME " + d.Domain + " → " + s.cnameHost() + " is in place (or Cloudflare proxy), then try again"}, nil
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
		return nil, nil, errors.New("domain store not available")
	}
	if _, err := s.deps.Domains.GetByID(ctx, acc, in.DomainID); err != nil {
		return nil, nil, errors.New("domain not found")
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
		return nil, nil, errors.New("domain store not available")
	}
	d, err := s.deps.Domains.GetByID(ctx, acc, in.DomainID)
	if err != nil {
		return nil, nil, errors.New("domain not found")
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
		return nil, nil, errors.New("domain store not available")
	}
	d, err := s.deps.Domains.GetByID(ctx, acc, in.DomainID)
	if err != nil {
		return nil, nil, errors.New("domain not found")
	}
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	if mode != "auto" && mode != "custom" {
		return nil, nil, errors.New("invalid mode (auto|custom)")
	}
	if err := s.deps.Domains.SetTLSMode(ctx, acc, d.Domain, mode); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]any{"tls_mode": mode, "domain": d.Domain}, nil
}

type domainTLSModeIn struct {
	DomainID string `json:"domain_id" jsonschema:"Domain ID (from list_domains)"`
	Mode     string `json:"mode" jsonschema:"TLS mode: auto (Let's Encrypt, default) or custom (bring-your-own-cert)"`
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
		return nil, nil, errors.New("domain store not available")
	}
	d, err := s.deps.Domains.GetByID(ctx, acc, in.DomainID)
	if err != nil {
		return nil, nil, errors.New("domain not found")
	}
	return nil, map[string]any{"domain": d.Domain, "tls_email": d.TLSEmail}, nil
}

type domainTLSEmailIn struct {
	DomainID string `json:"domain_id" jsonschema:"Domain ID (from list_domains)"`
	Email    string `json:"email" jsonschema:"ACME/Let's Encrypt email for the domain. Empty = fall back to the account email, then the global ACME_EMAIL."`
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
		return nil, nil, errors.New("domain store not available")
	}
	d, err := s.deps.Domains.GetByID(ctx, acc, in.DomainID)
	if err != nil {
		return nil, nil, errors.New("domain not found")
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if email != "" && !strings.Contains(email, "@") {
		return nil, nil, errors.New("invalid email")
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
		return nil, nil, errors.New("domain store not available")
	}
	d, err := s.deps.Domains.GetByID(ctx, acc, in.DomainID)
	if err != nil {
		return nil, nil, errors.New("domain not found")
	}
	certPEM := strings.TrimSpace(in.Cert)
	keyPEM := strings.TrimSpace(in.Key)
	if certPEM == "" || keyPEM == "" {
		return nil, nil, errors.New("cert and key are required")
	}
	cert, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return nil, nil, errors.New("invalid cert/key — check the PEM format and that the key matches")
	}
	if len(cert.Certificate) == 0 {
		return nil, nil, errors.New("invalid cert")
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
	DomainID string `json:"domain_id" jsonschema:"Domain ID (from list_domains)"`
	Cert     string `json:"cert" jsonschema:"Certificate PEM (chain) for the domain"`
	Key      string `json:"key" jsonschema:"Matching private key PEM"`
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
	Head string `json:"head" jsonschema:"<head> script (Google Analytics, Facebook Pixel, etc.). Only <script src=https://...>, <meta> and comments are allowed."`
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
			// Card t_56a2bd7f: cryptoutil.MaskSecret, not a local copy. The
			// copy that lived here kept every character but the last four, so
			// this tool answered with nearly the whole CHIP secret even though
			// its own description promises the full secret is never returned.
			out.SecretKeyMasked = cryptoutil.MaskSecret(string(secret))
		}
	}
	return nil, out, nil
}

type setChipSettingsIn struct {
	BrandID   string `json:"brand_id" jsonschema:"CHIP Brand ID (required)"`
	SecretKey string `json:"secret_key,omitempty" jsonschema:"CHIP Secret Key. Empty = keep the existing key."`
	// Mode is accepted for compatibility with clients written against the old
	// contract, and IGNORED (card t_9efc5ed7): nothing changes a payment based
	// on it, so there is nothing for the caller to choose. Rejecting it would
	// break clients that follow the published docs.
	Mode            string   `json:"mode,omitempty" jsonschema:"Ignored. Accepted for compatibility with older clients; the stored value is kept and no payment behaviour depends on it."`
	EmailFallback   string   `json:"email_fallback,omitempty" jsonschema:"Fallback email when the buyer does not provide an email"`
	MethodWhitelist []string `json:"payment_method_whitelist,omitempty" jsonschema:"List of CHIP payment methods, empty = all. fpx (FPX B2C, personal) and fpx_b2b1 (FPX B2B1, corporate) are two separate methods with their own fees."`
}

func (s *Server) setChipSettings(ctx context.Context, _ *mcp.CallToolRequest, in setChipSettingsIn) (*mcp.CallToolResult, map[string]any, error) {
	acc, err := s.requireAdminRole(ctx)
	if err != nil {
		return nil, nil, err
	}
	if in.BrandID == "" {
		return nil, nil, errors.New("brand_id is required")
	}
	secretEnc := ""
	existingMode := ""
	existingVerified := false
	// Card t_9efc5ed7: the stored mode + verified state are still read, because
	// the column survives a save (nothing in the request decides it) and a save
	// that only edits the fallback email must not reset the ✓.
	if a, aerr := s.deps.Accounts.ByID(ctx, acc); aerr == nil {
		existingMode = a.ChipMode
		existingVerified = a.ChipVerified
		if in.SecretKey == "" {
			secretEnc = a.ChipSecretKeyEnc
		}
	}
	if in.SecretKey != "" {
		enc, eerr := cryptoutil.Encrypt(s.decKey, []byte(in.SecretKey))
		if eerr != nil {
			return nil, nil, eerr
		}
		secretEnc = enc
	}
	if secretEnc == "" {
		return nil, nil, errors.New("secret_key is required")
	}
	// Card t_9efc5ed7: the Test/Live choice is gone from BOTH doors. `mode` is
	// accepted (an older client may still send it) and ignored; the value
	// stored is the one the account already holds (dbstore.StoredChipMode).
	mode := dbstore.StoredChipMode(existingMode)
	verified := false
	if s.chip != nil && in.SecretKey != "" {
		if verr := s.chip.ValidateCredentials(ctx, in.SecretKey); verr != nil {
			return nil, map[string]any{"error": "invalid credentials — check the Brand ID / Secret Key", "verified": false}, nil
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

type testChipConnectionIn struct {
	SecretKey string `json:"secret_key,omitempty" jsonschema:"Secret Key to test. Empty = test the stored key."`
}

// testChipConnection runs the same live CHIP credential check the save path
// runs, without writing anything (card t_9efc5ed7 — the "Test connection" action
// the settings card carries). It is the MCP twin of POST
// /v1/rest/settings/chip/test.
func (s *Server) testChipConnection(ctx context.Context, _ *mcp.CallToolRequest, in testChipConnectionIn) (*mcp.CallToolResult, map[string]any, error) {
	acc, err := s.requireAdminRole(ctx)
	if err != nil {
		return nil, nil, err
	}
	if s.chip == nil {
		return nil, nil, errors.New("the CHIP connection check is not configured on this server")
	}
	secret := in.SecretKey
	if secret == "" {
		a, aerr := s.deps.Accounts.ByID(ctx, acc)
		if aerr != nil {
			return nil, nil, fmt.Errorf("db: %w", aerr)
		}
		if a.ChipSecretKeyEnc == "" {
			return nil, nil, errors.New("secret_key is required")
		}
		plain, derr := cryptoutil.Decrypt(s.decKey, a.ChipSecretKeyEnc)
		if derr != nil {
			return nil, nil, derr
		}
		secret = string(plain)
	}
	if verr := s.chip.ValidateCredentials(ctx, secret); verr != nil {
		return nil, map[string]any{"ok": false, "verified": false, "error": "invalid credentials — check the Brand ID / Secret Key"}, nil
	}
	return nil, map[string]any{"ok": true, "verified": true}, nil
}

// ─── Profile avatar (issue #396) ──────────────────────────────────────────

type updateAvatarIn struct {
	AvatarURL string `json:"avatar_url" jsonschema:"Avatar URL (must be an image hosted by JomForm from the upload bucket). Empty = remove avatar."`
}

func (s *Server) updateProfileAvatar(ctx context.Context, _ *mcp.CallToolRequest, in updateAvatarIn) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #645: user-row write — API-key write scope is the gate.
	if err := s.requireWriteScope(ctx); err != nil {
		return nil, nil, err
	}
	if s.deps.Users == nil {
		return nil, nil, errors.New("service not available")
	}
	userID := s.userIDFor(ctx)
	if userID == "" {
		return nil, nil, errors.New("authentication required")
	}
	u := strings.TrimSpace(in.AvatarURL)
	if u != "" && !jomformMediaURLMCP(u) {
		return nil, nil, errors.New("invalid avatar URL (the image must come from the JomForm server)")
	}
	if err := s.deps.Users.UpdateAvatar(ctx, userID, u); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{"avatar_url": u}, nil
}

// ─── Passkeys (issue #396) ───────────────────────────────────────────────

type passkeyOut struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
}

// listPasskeys lists the caller's passkeys. `created_at` carries the row's real
// timestamp (RFC 3339, UTC), read through PasskeyStore.ListCreatedAt.
//
// It used to be declared with omitempty and never set: ByUser reads no
// timestamp, so the field was silently dropped from every response — the MCP
// half of the same defect as REST `GET /v1/auth/passkeys` answering
// `"created_at": ""` (card t_96c7c227). The two surfaces report the same field
// from the same query, and the parity rule keeps them in step; omitempty is
// gone so a regression here is a wrong value rather than a vanished key.
func (s *Server) listPasskeys(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, *listOutput[passkeyOut], error) {
	if s.deps.Passkeys == nil {
		return nil, nil, errors.New("passkey store not available")
	}
	userID := s.userIDFor(ctx)
	if userID == "" {
		return nil, nil, errors.New("authentication required")
	}
	creds, err := s.deps.Passkeys.ListCreatedAt(ctx, userID)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	out := make([]passkeyOut, 0, len(creds))
	for _, c := range creds {
		out = append(out, passkeyOut{ID: base64URLMCP(c.ID), CreatedAt: c.CreatedAt.UTC().Format(time.RFC3339)})
	}
	return nil, newListOut(out), nil
}

type deletePasskeyIn struct {
	PasskeyID string `json:"passkey_id" jsonschema:"Passkey ID (from list_passkeys)"`
}

func (s *Server) deletePasskey(ctx context.Context, _ *mcp.CallToolRequest, in deletePasskeyIn) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #645: user-row write (a passkey belongs to the person) — API-key
	// write scope is the gate.
	if err := s.requireWriteScope(ctx); err != nil {
		return nil, nil, err
	}
	if s.deps.Passkeys == nil {
		return nil, nil, errors.New("passkey store not available")
	}
	userID := s.userIDFor(ctx)
	if userID == "" {
		return nil, nil, errors.New("authentication required")
	}
	raw, err := base64URLDecodeMCP(in.PasskeyID)
	if err != nil {
		return nil, nil, errors.New("invalid id")
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
		return nil, nil, errors.New("service not available")
	}
	userID := s.userIDFor(ctx)
	if userID == "" {
		return nil, nil, errors.New("authentication required")
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
	Provider string `json:"provider" jsonschema:"Social provider (google|azure_ad|github|cloudflare)"`
}

func (s *Server) unlinkSocialAccount(ctx context.Context, _ *mcp.CallToolRequest, in unlinkSocialIn) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #645: unlinking a social identity is a WRITE on the person's own
	// account. The scope axis is the gate here (there is no workspace role on a
	// user-level write); without it a readonly key could strip a login method.
	if err := s.requireWriteScope(ctx); err != nil {
		return nil, nil, err
	}
	if s.deps.Users == nil {
		return nil, nil, errors.New("service not available")
	}
	userID := s.userIDFor(ctx)
	if userID == "" {
		return nil, nil, errors.New("authentication required")
	}
	if in.Provider == "" {
		return nil, nil, errors.New("provider is required")
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
		return nil, twoFAStatusOut{}, errors.New("service not available")
	}
	userID := s.userIDFor(ctx)
	if userID == "" {
		return nil, twoFAStatusOut{}, errors.New("authentication required")
	}
	u, err := s.deps.Users.ByID(ctx, userID)
	if err != nil || u == nil {
		return nil, twoFAStatusOut{}, errors.New("user not found")
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
		return nil, twoFASetupOut{}, errors.New("service not available")
	}
	userID := s.userIDFor(ctx)
	if userID == "" {
		return nil, twoFASetupOut{}, errors.New("authentication required")
	}
	u, err := s.deps.Users.ByID(ctx, userID)
	if err != nil || u == nil {
		return nil, twoFASetupOut{}, errors.New("user not found")
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
	Code string `json:"code" jsonschema:"6-digit code from the authenticator app (after setup_2fa)"`
}

func (s *Server) enableTwoFA(ctx context.Context, _ *mcp.CallToolRequest, in enableTwoFAIn) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #645: enabling 2FA is a credential change — API-key write scope gate.
	if err := s.requireWriteScope(ctx); err != nil {
		return nil, nil, err
	}
	if s.deps.Users == nil || s.deps.Sessions == nil {
		return nil, nil, errors.New("service not available")
	}
	userID := s.userIDFor(ctx)
	if userID == "" {
		return nil, nil, errors.New("authentication required")
	}
	if in.Code == "" {
		return nil, nil, errors.New("code is required")
	}
	pending, err := s.deps.Sessions.PendingTOTP().Get(ctx, userID)
	if err != nil || pending == "" {
		return nil, nil, errors.New("2FA not set up, or an error occurred")
	}
	if !auth.ValidateTOTPSecret(pending, in.Code) {
		return nil, nil, errors.New("invalid code")
	}
	if err := s.deps.Users.EnableTOTP(ctx, userID, pending); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	_ = s.deps.Sessions.PendingTOTP().Delete(ctx, userID)
	return nil, map[string]string{"status": "enabled"}, nil
}

// disable2FAIn is the input of the disable_2fa MCP tool.
//
// Issue #666/#665: both fields exist because the two credential kinds a
// non-browser MCP client can present are different, and `password` alone cannot
// serve a magic-link signup — its password is a random UUID the person has never
// seen, so such an account could not turn its own 2FA off through MCP at all.
//
// Exactly ONE of the two fields is the proof; see disable2FATool for the schema
// that enforces it before a handler ever runs.
type disableTwoFAIn struct {
	// Password is the account password — the baseline confirmation.
	Password string `json:"password,omitempty" jsonschema:"Account password for verification"`
	// TOTPCode is a CURRENT code from the authenticator app. It is not a second
	// factor on top of the password: MCP has no session at all and an API key is
	// a credential, not a second factor, so the code *replaces* the password as
	// the proof that the caller really holds this account.
	TOTPCode string `json:"totp_code,omitempty" jsonschema:"Current 2FA code (6 digits) from the authenticator app. Replaces the password as proof, not an added second factor — MCP has no session and the API key is not a second factor."`
}

// disable2FATool is the registered definition of `disable_2fa`.
//
// The input schema is written BY HAND instead of being inferred from
// disableTwoFAIn. Inference cannot express the contract in the Description — "one
// of the two, not both" — because a struct can only mark a field required or
// omit it entirely: with `password` required, a client sending only `totp_code`
// is rejected by schema validation before the handler runs (the exact failure
// that made the reported code path impossible); with both optional, a caller
// proving NOTHING reaches the handler.
//
// `oneOf` states it exactly: exactly one of {password} | {totp_code}. Measured
// against this SDK, a call with both fields, with neither, or with an unknown
// field is rejected by validation, and a call with either one alone reaches the
// handler:
//
//	{"password":"…"}               -> allowed
//	{"totp_code":"123456"}         -> allowed
//	{"password":"…","totp_code":…} -> refused (oneOf: validated against both)
//	{}                             -> refused (did not validate against any)
//	{"nope":"x"}                   -> refused (additionalProperties:false)
//
// The fields are still described in `properties` (not only inside oneOf) so an
// agent — and the chat assistant, which hands the raw schema to the model — sees
// them and their descriptions in the normal shape.
func disable2FATool() *mcp.Tool {
	str := func(desc string) *jsonschema.Schema {
		return &jsonschema.Schema{Type: "string", Description: desc}
	}
	return &mcp.Tool{
		Name: "disable_2fa",
		Description: "Disable 2FA on your account. Accepted proof — EXACTLY ONE of the two, not " +
			"both: the current 2FA code in totp_code (a 6-digit code from the authenticator " +
			"app, checked once only and not requiring a password) OR the account password in " +
			"password (for accounts that joined through an email link and have no usable " +
			"password). Sending both is rejected by the schema, and without either the call is " +
			"rejected. An API key alone is never enough to turn off the second factor. " +
			"(The dashboard form (Profile → Security) asks for the current 2FA code.) " +
			// Issue #63/#713: this sentence arrived on main from PR 713, which
			// changed the tool's plain Description. This tool's definition was
			// written before that landed (the branch was cut from #686's head),
			// so the union has to carry the clause here too — otherwise the
			// revocation behaviour is undocumented while it still happens.
			"Turning off 2FA also revokes EVERY trusted device on the account — a device that " +
			"skipped the factor that has now been removed cannot stay exempt (same as REST and " +
			"the dashboard profile form; use list_trusted_devices to review the remaining devices).",
		InputSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"password":  str("Account password, for verification"),
				"totp_code": str("Current 2FA code (6 digits) from the authenticator app. Replaces the password as proof, it does not add a second factor."),
			},
			// No unknown fields: the declared set is the whole contract, so a
			// typo is a refusal rather than a silently ignored argument.
			AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}},
			// Exactly one of the two proofs.
			OneOf: []*jsonschema.Schema{
				{Required: []string{"password"}},
				{Required: []string{"totp_code"}},
			},
		},
	}
}

// disableTwoFA turns 2FA off for the calling account.
//
// THE PROOF (one rule, and the same rule the dashboard UI follows):
//
//   - `totp_code` supplied → it must be a CURRENT code for this account's own
//     secret, verified through checkAndConsumeTOTP so it cannot be replayed
//     (#528), and consumed by the same call. No password is required alongside
//     it. This is the path a caller takes when the account signs in by emailed
//     link and never chose a usable password.
//   - no code → the account password confirms the action, verified whenever it
//     is used. An API key alone never disables a second factor.
//
// The MCP surface has no session, so unlike the REST handler there is no
// magic/non-magic distinction here: "no code" always means the password path.
//
// The single-use rule lives in checkAndConsumeTOTP, not in a copy. A second
// hand-rolled copy of it is how one surface silently loses the check.
func (s *Server) disableTwoFA(ctx context.Context, _ *mcp.CallToolRequest, in disableTwoFAIn) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #645: disabling 2FA weakens a credential — API-key write scope gate.
	if err := s.requireWriteScope(ctx); err != nil {
		return nil, nil, err
	}
	// The trust set lives in the Valkey keyspace the session store owns, so a
	// nil Sessions means the revocation below cannot run. Refuse the whole call
	// instead of clearing the factor and leaving the live trust exemptions
	// behind — that is precisely the state this tool must never create, and it
	// is the same answer the sibling revoke_all_trusted_devices gives on a nil
	// store.
	if s.deps.Users == nil || s.deps.Sessions == nil {
		return nil, nil, errors.New("service not available")
	}
	userID := s.userIDFor(ctx)
	if userID == "" {
		return nil, nil, errors.New("authentication required")
	}
	u, err := s.deps.Users.ByID(ctx, userID)
	if err != nil || u == nil {
		return nil, nil, errors.New("user not found")
	}
	if code := strings.TrimSpace(in.TOTPCode); code != "" {
		if !checkAndConsumeTOTP(ctx, s.deps.Users, u, code) {
			return nil, nil, errors.New("invalid 2FA code")
		}
	} else {
		ok, err := auth.VerifyPassword(in.Password, u.PasswordHash)
		if err != nil || !ok {
			return nil, nil, errors.New("invalid password")
		}
	}
	// Issue #63 (REST parity — httpapi handle2FADisable, api/internal/httpapi/
	// auth.go): a trusted device waives the 2FA code for 30 days, so once the
	// factor is gone every exemption minted while it was on has to go with it.
	// MCP is not cookie-based, so there is no trust cookie to clear here: the
	// Valkey trust SET is the entire effect.
	//
	// Order matters. Revoking FIRST means a failure can only ever leave the
	// harmless partial state (2FA still on, trust emptied — the next login just
	// asks for a code again); the other order can leave 2FA off with a live
	// trust exemption, i.e. exactly the hole this card is about. The proof
	// above has already been checked, so this is not reachable unauthenticated.
	if err := s.deps.Sessions.TrustedDevices().RevokeAll(ctx, userID); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	if err := s.deps.Users.DisableTOTP(ctx, userID); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{"status": "disabled"}, nil
}

// checkAndConsumeTOTP verifies a TOTP code against the account's OWN secret and
// atomically consumes its time-step, returning true only when the code is valid
// and had not already been used.
//
// The rule itself is NOT implemented here. It lives once in
// internal/totpconsume, shared with the REST surface (httpapi), because it needs
// both auth (validate the code and learn which step matched) and dbstore
// (consume that step atomically) and neither of those packages may import the
// other (#528).
//
// This used to be a hand-rolled copy of the httpapi rule, kept in step by a
// comment. A comment is not a guard: on the 2FA-disable path, drift between two
// copies means a code seen once can turn a second factor off twice. Both
// surfaces now CALL the one rule, and only_one_copy_test.go fails the build if a
// second copy comes back.
//
// A false return is a REJECTED code (wrong code, replayed code, or 2FA not
// enabled). It is never a reason to fall back to the password check.
func checkAndConsumeTOTP(ctx context.Context, users *dbstore.UserStore, u *dbstore.User, code string) bool {
	return totpconsume.ConsumeCode(ctx, users, u, code)
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
		return nil, nil, errors.New("service not available")
	}
	userID := s.userIDFor(ctx)
	if userID == "" {
		return nil, nil, errors.New("authentication required")
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
	DeviceID string `json:"device_id" jsonschema:"Trusted device ID (from list_trusted_devices)"`
}

// revokeTrustedDevice (issue #454): revoke ONE trusted device by id. The next
// login on that device is challenged for a 2FA code again (immediate).
func (s *Server) revokeTrustedDevice(ctx context.Context, _ *mcp.CallToolRequest, in revokeTrustedDeviceIn) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #645: revoking a trusted device is a write on the person's account.
	if err := s.requireWriteScope(ctx); err != nil {
		return nil, nil, err
	}
	if s.deps.Sessions == nil {
		return nil, nil, errors.New("service not available")
	}
	userID := s.userIDFor(ctx)
	if userID == "" {
		return nil, nil, errors.New("authentication required")
	}
	if strings.TrimSpace(in.DeviceID) == "" {
		return nil, nil, errors.New("device_id is required")
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
		return nil, nil, errors.New("service not available")
	}
	userID := s.userIDFor(ctx)
	if userID == "" {
		return nil, nil, errors.New("authentication required")
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
		return nil, nil, errors.New("system settings not available")
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
	Key   string `json:"key" jsonschema:"Settings key (must start with site_ or landing_)"`
	Value string `json:"value" jsonschema:"Settings value"`
}

func (s *Server) setSystemSetting(ctx context.Context, _ *mcp.CallToolRequest, in setSystemSettingIn) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #645: platform-wide settings are a write — API-key write scope gate.
	if err := s.requireWriteScope(ctx); err != nil {
		return nil, nil, err
	}
	if s.deps.SystemSettings == nil {
		return nil, nil, errors.New("system settings not available")
	}
	if in.Key == "" {
		return nil, nil, errors.New("key is required")
	}
	if !strings.HasPrefix(in.Key, "site_") && !strings.HasPrefix(in.Key, "landing_") {
		return nil, nil, errors.New("key not allowed")
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
	FormID   string `json:"form_id,omitempty" jsonschema:"Optional — limit the export to sales of this form"`
	Status   string `json:"status,omitempty" jsonschema:"Optional — limit to status: pending|paid|confirmed|failed|cancelled"`
	DateFrom string `json:"date_from,omitempty" jsonschema:"Optional — YYYY-MM-DD, including that day"`
	DateTo   string `json:"date_to,omitempty" jsonschema:"Optional — YYYY-MM-DD, including that day"`
	Q        string `json:"q,omitempty" jsonschema:"Optional — search on reference code/buyer name (sales) or answer value (responses)"`
}

func (s *Server) exportSalesCSV(ctx context.Context, _ *mcp.CallToolRequest, in exportSalesIn) (*mcp.CallToolResult, map[string]string, error) {
	accs, err := s.accountScopes(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	if s.deps.Sales == nil {
		return nil, nil, fmt.Errorf("sales not available")
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
	//
	// Issue #185: the money columns are in the SALE's own currency, so the
	// `_rm` suffix was a wrong label for a USD sale and the file carried nothing
	// that named the money at all. Same columns as the REST export: `currency`
	// beside renamed `total` / `refunded` / `outstanding`.
	//
	// Card t_8064a965: `fee` and `net` (total − fee) sit beside `total`, because
	// the buyer's total includes the buyer-borne charge, which the merchant never
	// receives. An agent reconciling from this CSV used to count the fee as
	// income. Card t_1a17d22d adds `platform_fee`: the flat line JomForm added to
	// the CHIP purchase (0.00 on a legacy gross-up sale).
	header := []string{"ref_code", "status", "payment_method", "buyer_name", "buyer_email",
		"currency", "total", "fee", "platform_fee", "net", "created_at", "purchase_id", "table_number",
		"refunded", "outstanding", "refund_status"}
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
				sl.CurrencyOrMYR(),
				fmt.Sprintf("%.2f", float64(sl.TotalSen)/100),
				fmt.Sprintf("%.2f", float64(sl.FeeSen)/100),
				fmt.Sprintf("%.2f", float64(sl.PlatformFeeSen)/100),
				// net = total − fee (what the merchant keeps); refunded and
				// outstanding stay their own columns, as on /sales.
				fmt.Sprintf("%.2f", float64(sl.TotalSen-sl.FeeSen)/100),
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
			"", // a response has no money and therefore no currency
			"0.00",
			// fee, platform_fee and net: no payment, so all are zero, but the
			// columns stay present so every row of the file has the same shape.
			"0.00", "0.00", "0.00",
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
	Linked bool `json:"linked"`
	// Configured answers the DEPLOYMENT question — "this installation can
	// offer the Cloudflare OAuth (auto-CNAME) flow" — which is exactly what
	// GET /v1/rest/cf/status reports under the same name, from the same rule
	// (deps.CFOAuthConfigured ← httpapi.CFOAuthConfigured ←
	// JF_CF_OAUTH_CLIENT_ID). It is NOT "the OAuth link can be completed
	// through MCP": that flow is browser-only, which is why there is no
	// connect_cloudflare_oauth tool and why the tool description points at the
	// two manual kinds. It used to be a hard-coded false, so every deployment
	// answered false here while REST answered true — one word, two meanings.
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
	configured := false
	if s.deps.CFOAuthConfigured != nil {
		configured = s.deps.CFOAuthConfigured()
	}
	return nil, cloudflareStatusOut{Linked: linked, Configured: configured}, nil
}

type connectCFAPITokenIn struct {
	AccountEmail string `json:"account_email" jsonschema:"Cloudflare account email"`
	APIToken     string `json:"api_token" jsonschema:"Cloudflare API token (verified immediately, stored encrypted)"`
	AccountName  string `json:"account_name,omitempty" jsonschema:"Optional label"`
}

func (s *Server) connectCloudflareAPIToken(ctx context.Context, _ *mcp.CallToolRequest, in connectCFAPITokenIn) (*mcp.CallToolResult, map[string]bool, error) {
	acc, err := s.requireWriteRole(ctx)
	if err != nil {
		return nil, nil, err
	}
	in.AccountEmail = strings.TrimSpace(in.AccountEmail)
	in.APIToken = strings.TrimSpace(in.APIToken)
	if in.AccountEmail == "" || in.APIToken == "" {
		return nil, nil, errors.New("account email and API token are required")
	}
	if err := cfVerifyAPITokenMCP(ctx, in.APIToken); err != nil {
		return nil, nil, fmt.Errorf("invalid or expired API token: %w", err)
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
	AccountEmail string `json:"account_email" jsonschema:"Cloudflare account email"`
	APIKey       string `json:"api_key" jsonschema:"Cloudflare Global API key (verified immediately, stored encrypted)"`
	AccountName  string `json:"account_name,omitempty" jsonschema:"Optional label"`
}

func (s *Server) connectCloudflareGlobalKey(ctx context.Context, _ *mcp.CallToolRequest, in connectCFGlobalKeyIn) (*mcp.CallToolResult, map[string]bool, error) {
	acc, err := s.requireWriteRole(ctx)
	if err != nil {
		return nil, nil, err
	}
	in.AccountEmail = strings.TrimSpace(in.AccountEmail)
	in.APIKey = strings.TrimSpace(in.APIKey)
	if in.AccountEmail == "" || in.APIKey == "" {
		return nil, nil, errors.New("account email and global API key are required")
	}
	if err := cfVerifyGlobalKeyMCP(ctx, in.AccountEmail, in.APIKey); err != nil {
		return nil, nil, fmt.Errorf("invalid global API key: %w", err)
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
		return nil, webhookKeyOut{}, errors.New("failed to generate signing key")
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

// maskSecretMCP used to live here: a byte-for-byte copy of httpapi.maskSecret,
// with the same defect (it kept every character BUT the last four, card
// t_56a2bd7f). Both doors now call cryptoutil.MaskSecret so the rule cannot
// drift between them again.

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
		return errors.New("token rejected by Cloudflare")
	}
	var vr struct {
		Success bool `json:"success"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&vr)
	if !vr.Success {
		return errors.New("invalid token")
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
		return errors.New("email or key rejected by Cloudflare")
	}
	var ur struct {
		Success bool `json:"success"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&ur)
	if !ur.Success {
		return errors.New("invalid email or key")
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
		// textutil cuts on a rune boundary: a multi-byte body must not render
		// as a broken character in the message an operator diagnoses from.
		return nil, fmt.Errorf("cloudflare http %d: %s", resp.StatusCode, textutil.Truncate(string(b), 512))
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
