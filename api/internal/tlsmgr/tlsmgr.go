// Package tlsmgr provides automatic TLS provisioning at the origin for
// DNS-only (grey-cloud) custom domains (issue #212). It wraps
// golang.org/x/crypto/acme/autocert so the origin can issue + renew
// Let's Encrypt certs for verified custom domains and serve them via
// SNI-based cert selection, alongside the default cert for the primary
// domain (jomform.com).
//
// Issue #398: each custom domain's cert is provisioned/renewed with its own
// ACME contact email (defaulting to the domain owner's account email, then
// the global ACME_EMAIL env). Because autocert binds one email per Manager,
// the Manager lazily creates one autocert.Manager per distinct email and
// dispatches SNI + HTTP-01 challenges to the manager for the requesting
// host's email.
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
	mu       sync.RWMutex
	verified map[string]string // verified custom domain (lowercased) -> ACME email ("" = default)
	custom   map[string]*tls.Certificate
	primary  string // primary domain (e.g. jomform.com)
	log      *slog.Logger
	// certDir is the disk cache dir shared by every per-email autocert
	// manager (renewal persistence). Empty disables disk caching.
	certDir string
	// directory is the ACME directory URL. Empty uses the Let's Encrypt
	// production endpoint (autocert default).
	directory string
	// defaultEmail is the fallback ACME contact (global ACME_EMAIL env),
	// used for the primary domain and any verified domain without its own
	// per-domain email.
	defaultEmail string
	// managers holds one autocert.Manager per distinct ACME email,
	// created lazily on first use.
	managers map[string]*autocert.Manager
}

// Options configures a Manager.
type Options struct {
	// Email is the default ACME account contact (Let's Encrypt
	// notifications), used for the primary domain and any verified domain
	// without its own per-domain email (issue #398). Empty disables
	// provisioning for domains that have no per-domain email either.
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

// New builds a Manager. Unlike the pre-#398 behaviour it always returns a
// non-nil manager so per-domain ACME emails can drive provisioning even when
// no global ACME_EMAIL is set; a domain only provisions when it resolves to
// a non-empty email (per-domain → account → global). The caller should still
// only start the TLS listener when auto-TLS is configured.
func New(o Options) *Manager {
	log := o.Log
	if log == nil {
		log = slog.Default()
	}
	return &Manager{
		verified:     map[string]string{},
		custom:       map[string]*tls.Certificate{},
		primary:      strings.ToLower(o.Primary),
		log:          log,
		certDir:      o.CertDir,
		directory:    o.Directory,
		defaultEmail: o.Email,
		managers:     map[string]*autocert.Manager{},
	}
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

// managerForEmail returns (creating lazily) the autocert.Manager for a given
// ACME email. Returns nil when email is empty — a domain with no resolved
// email never provisions (issue #398 fallback: skip auto-TLS).
func (mg *Manager) managerForEmail(email string) *autocert.Manager {
	if email == "" {
		return nil
	}
	mg.mu.Lock()
	defer mg.mu.Unlock()
	if m, ok := mg.managers[email]; ok {
		return m
	}
	m := &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		Email:      email,
		Cache:      autocert.DirCache(mg.certDir),
		HostPolicy: mg.hostPolicy,
	}
	if mg.directory != "" {
		m.Client = &acme.Client{DirectoryURL: mg.directory}
	}
	mg.managers[email] = m
	return m
}

// managerForHost resolves the autocert.Manager that should serve a host:
// the primary domain (and its subdomains) use the default email; a verified
// custom domain uses its stored per-domain email (falling back to the
// default email when unset). Returns nil when the host is not allowed or has
// no resolvable email.
func (mg *Manager) managerForHost(host string) *autocert.Manager {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	mg.mu.RLock()
	email, ok := mg.verified[host]
	mg.mu.RUnlock()
	if !ok {
		if mg.primary != "" && (host == mg.primary || strings.HasSuffix(host, "."+mg.primary)) {
			return mg.managerForEmail(mg.defaultEmail)
		}
		return nil
	}
	if email == "" {
		email = mg.defaultEmail
	}
	return mg.managerForEmail(email)
}

// AddVerified registers a verified custom domain so the origin will
// provision + renew a cert for it, using the given ACME email (issue #398).
// An empty email falls back to the default (global) email; if that is also
// empty the domain is registered but never provisions. Idempotent.
func (mg *Manager) AddVerified(domain, email string) {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	if domain == "" {
		return
	}
	mg.mu.Lock()
	mg.verified[domain] = email
	mg.mu.Unlock()
	mg.log.Info("acme_domain_registered", "domain", domain, "email", email)
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
	if domain == "" || mg.certDir == "" {
		return false
	}
	mg.mu.RLock()
	_, hasCustom := mg.custom[domain]
	mg.mu.RUnlock()
	if hasCustom {
		return false
	}
	_, err := autocert.DirCache(mg.certDir).Get(context.Background(), domain)
	return err == nil
}

// TLSConfig returns a *tls.Config whose GetCertificate performs SNI-based
// cert selection. A merchant-supplied custom cert (issue #397) takes
// precedence; otherwise the per-email autocert manager for the host's ACME
// email provisions + serves a Let's Encrypt cert, falling back to the primary
// domain's cert.
func (mg *Manager) TLSConfig() *tls.Config {
	return &tls.Config{
		GetCertificate: mg.getCertificate,
		NextProtos:     []string{"h2", "http/1.1", acme.ALPNProto},
	}
}

func (mg *Manager) getCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	host := strings.ToLower(strings.TrimSuffix(hello.ServerName, "."))
	mg.mu.RLock()
	cert, ok := mg.custom[host]
	mg.mu.RUnlock()
	if ok {
		return cert, nil
	}
	m := mg.managerForHost(host)
	if m == nil {
		return nil, fmt.Errorf("acme/autocert: host %q not allowed", host)
	}
	return m.GetCertificate(hello)
}

// HTTPHandler returns the HTTP-01 challenge handler that must be mounted
// on the plain-HTTP listener (path /.well-known/acme-challenge/). It
// dispatches each challenge to the autocert.Manager for the requesting
// host's ACME email (issue #398).
func (mg *Manager) HTTPHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/.well-known/acme-challenge/") {
			http.NotFound(w, r)
			return
		}
		host := strings.ToLower(strings.TrimSuffix(r.Host, "."))
		m := mg.managerForHost(host)
		if m == nil {
			http.Error(w, "acme/autocert: host not allowed", http.StatusForbidden)
			return
		}
		m.HTTPHandler(nil).ServeHTTP(w, r)
	})
}
