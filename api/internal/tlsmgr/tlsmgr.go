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
	custom   map[string]*tls.Certificate
	primary  string // primary domain (e.g. jomform.com)
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
		custom:   map[string]*tls.Certificate{},
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
// hostnames (rate-limit + abuse protection). A domain with a merchant-supplied
// custom cert is also allowed (it is served from the custom map, never
// re-issued via ACME).
func (mg *Manager) hostPolicy(_ context.Context, host string) error {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if mg.primary != "" && (host == mg.primary || strings.HasSuffix(host, "."+mg.primary)) {
		return nil
	}
	mg.mu.RLock()
	_, ok := mg.verified[host]
	_, hasCustom := mg.custom[host]
	mg.mu.RUnlock()
	if ok || hasCustom {
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
	delete(mg.custom, domain)
	mg.mu.Unlock()
}

// SetCustomCert registers a merchant-supplied certificate for a domain
// (bring-your-own-cert, issue #397). The domain is served this cert via SNI
// and is never re-issued through ACME. Idempotent.
func (mg *Manager) SetCustomCert(domain string, cert *tls.Certificate) {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	if domain == "" || cert == nil {
		return
	}
	mg.mu.Lock()
	mg.custom[domain] = cert
	mg.mu.Unlock()
	mg.log.Info("custom_cert_registered", "domain", domain)
}

// RemoveCustomCert drops a merchant-supplied cert for a domain (issue #397).
// The domain falls back to auto-TLS provisioning (if verified).
func (mg *Manager) RemoveCustomCert(domain string) {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	mg.mu.Lock()
	delete(mg.custom, domain)
	mg.mu.Unlock()
}

// HasCert reports whether a cert is already cached on disk for the domain
// (issue #397). It is used to surface the TLS provisioning status: a cached
// cert means the domain is actively serving (status "active"); otherwise it is
// still provisioning ("pending"). Returns false when auto-TLS is disabled or
// the domain has a merchant-supplied cert.
func (mg *Manager) HasCert(domain string) bool {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	if domain == "" || mg.m.Cache == nil {
		return false
	}
	mg.mu.RLock()
	_, hasCustom := mg.custom[domain]
	mg.mu.RUnlock()
	if hasCustom {
		return false
	}
	_, err := mg.m.Cache.Get(context.Background(), domain)
	return err == nil
}

// TLSConfig returns a *tls.Config whose GetCertificate performs SNI-based
// cert selection. A merchant-supplied custom cert (issue #397) takes
// precedence; otherwise autocert provisions + serves a Let's Encrypt cert,
// falling back to the primary domain's cert.
func (mg *Manager) TLSConfig() *tls.Config {
	cfg := mg.m.TLSConfig()
	acmeGet := cfg.GetCertificate
	cfg.GetCertificate = func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		host := strings.ToLower(strings.TrimSuffix(hello.ServerName, "."))
		mg.mu.RLock()
		cert, ok := mg.custom[host]
		mg.mu.RUnlock()
		if ok {
			return cert, nil
		}
		return acmeGet(hello)
	}
	return cfg
}

// HTTPHandler returns the HTTP-01 challenge handler that must be mounted
// on the plain-HTTP listener (path /.well-known/acme-challenge/).
func (mg *Manager) HTTPHandler() http.Handler {
	return mg.m.HTTPHandler(nil)
}
