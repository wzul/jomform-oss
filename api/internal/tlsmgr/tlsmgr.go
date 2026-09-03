// Package tlsmgr provides automatic TLS provisioning at the origin for
// DNS-only (grey-cloud) custom domains (issue #212). It wraps
// golang.org/x/crypto/acme/autocert so the origin can issue + renew
// Let's Encrypt certs for verified custom domains and serve them via
// SNI-based cert selection, alongside the default cert for the primary
// domain (jomform.com).
package tlsmgr

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

// Manager provisions and serves per-host TLS certs. It is safe for
// concurrent use.
type Manager struct {
	m        *autocert.Manager
	mu       sync.RWMutex
	verified map[string]struct{} // verified custom domains (lowercased)
	primary  string              // primary domain (e.g. jomform.com)
	log      *slog.Logger
}

// Options configures a Manager.
type Options struct {
	// Email is the ACME account contact (Let's Encrypt notifications).
	Email string
	// Directory is the ACME directory URL. Empty uses the Let's Encrypt
	// production endpoint (autocert default).
	Directory string
	// CertDir is the directory where issued certs + account keys are
	// cached for renewal persistence. Empty disables disk caching.
	CertDir string
	// Primary is the primary domain (e.g. jomform.com). Its cert is served
	// as the fallback for any hostname that has no dedicated cert.
	Primary string
	// Log is the logger used for provisioning events. May be nil.
	Log *slog.Logger
}

// New builds a Manager. If Email is empty, it returns nil — the caller
// should then keep serving plain HTTP (auto-TLS disabled).
func New(o Options) *Manager {
	if o.Email == "" {
		return nil
	}
	log := o.Log
	if log == nil {
		log = slog.Default()
	}
	mg := &Manager{
		verified: map[string]struct{}{},
		primary:  strings.ToLower(o.Primary),
		log:      log,
	}
	m := &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		Email:      o.Email,
		Cache:      autocert.DirCache(o.CertDir),
		HostPolicy: mg.hostPolicy,
	}
	if o.Directory != "" {
		m.Client = &acme.Client{DirectoryURL: o.Directory}
	}
	mg.m = m
	return mg
}

// hostPolicy allows the primary domain, its subdomains, and any verified
// custom domain. This prevents the origin from issuing certs for arbitrary
// hostnames (rate-limit + abuse protection).
func (mg *Manager) hostPolicy(_ context.Context, host string) error {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if mg.primary != "" && (host == mg.primary || strings.HasSuffix(host, "."+mg.primary)) {
		return nil
	}
	mg.mu.RLock()
	_, ok := mg.verified[host]
	mg.mu.RUnlock()
	if ok {
		return nil
	}
	return fmt.Errorf("acme/autocert: host %q not allowed", host)
}

// AddVerified registers a verified custom domain so the origin will
// provision + renew a cert for it. Idempotent.
func (mg *Manager) AddVerified(domain string) {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	if domain == "" {
		return
	}
	mg.mu.Lock()
	mg.verified[domain] = struct{}{}
	mg.mu.Unlock()
	mg.log.Info("acme_domain_registered", "domain", domain)
}

// RemoveVerified drops a custom domain from the provisioning set.
func (mg *Manager) RemoveVerified(domain string) {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	mg.mu.Lock()
	delete(mg.verified, domain)
	mg.mu.Unlock()
}

// TLSConfig returns a *tls.Config whose GetCertificate performs SNI-based
// cert selection via autocert, falling back to the primary domain's cert.
func (mg *Manager) TLSConfig() *tls.Config {
	return mg.m.TLSConfig()
}

// HTTPHandler returns the HTTP-01 challenge handler that must be mounted
// on the plain-HTTP listener (path /.well-known/acme-challenge/).
func (mg *Manager) HTTPHandler() http.Handler {
	return mg.m.HTTPHandler(nil)
}
