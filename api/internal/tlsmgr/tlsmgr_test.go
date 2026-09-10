package tlsmgr

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"log/slog"
	"math/big"
	"testing"
	"time"
)

func TestNewAlwaysNonNil(t *testing.T) {
	// Issue #398: New always returns a manager so per-domain ACME emails can
	// drive provisioning even without a global ACME_EMAIL. A domain only
	// provisions when it resolves to a non-empty email.
	if m := New(Options{}); m == nil {
		t.Fatal("expected non-nil manager even without a global email")
	}
}

func TestHostPolicy(t *testing.T) {
	m := New(Options{
		Email:   "ops@example.com",
		Primary: "jomform.com",
		Log:     slog.Default(),
	})
	if m == nil {
		t.Fatal("expected non-nil manager")
	}

	// Primary domain + subdomains always allowed.
	for _, host := range []string{"jomform.com", "www.jomform.com", "mcp.jomform.com"} {
		if err := m.hostPolicy(context.Background(), host); err != nil {
			t.Errorf("hostPolicy(%q) = %v, want nil", host, err)
		}
	}

	// Unverified custom domain rejected.
	if err := m.hostPolicy(context.Background(), "shop.example.com"); err == nil {
		t.Error("hostPolicy(unverified) = nil, want error")
	}

	// Verified custom domain allowed.
	m.AddVerified("shop.example.com", "owner@example.com")
	if err := m.hostPolicy(context.Background(), "shop.example.com"); err != nil {
		t.Errorf("hostPolicy(verified) = %v, want nil", err)
	}

	// Removed custom domain rejected again.
	m.RemoveVerified("shop.example.com")
	if err := m.hostPolicy(context.Background(), "shop.example.com"); err == nil {
		t.Error("hostPolicy(removed) = nil, want error")
	}
}

func TestAddVerifiedIdempotentAndNormalizes(t *testing.T) {
	m := New(Options{Email: "ops@example.com", Primary: "jomform.com"})
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
	m.AddVerified("  Shop.Example.com. ", "owner@example.com")
	if err := m.hostPolicy(context.Background(), "shop.example.com"); err != nil {
		t.Errorf("hostPolicy after AddVerified = %v, want nil", err)
	}
	// Idempotent.
	m.AddVerified("shop.example.com", "owner@example.com")
	if err := m.hostPolicy(context.Background(), "shop.example.com"); err != nil {
		t.Errorf("hostPolicy after second AddVerified = %v, want nil", err)
	}
}

func TestPerDomainEmailManagerSelection(t *testing.T) {
	// Issue #398: a verified domain with its own email provisions via a
	// manager bound to that email; a domain without one falls back to the
	// default (global) email; a domain with no resolvable email never
	// provisions.
	m := New(Options{Email: "ops@example.com", Primary: "jomform.com"})
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
	m.AddVerified("shop.example.com", "owner@example.com")
	m.AddVerified("plain.example.com", "") // falls back to default email

	if mgr := m.managerForHost("shop.example.com"); mgr == nil || mgr.Email != "owner@example.com" {
		t.Errorf("managerForHost(shop) = %+v, want email owner@example.com", mgr)
	}
	if mgr := m.managerForHost("plain.example.com"); mgr == nil || mgr.Email != "ops@example.com" {
		t.Errorf("managerForHost(plain) = %+v, want fallback email ops@example.com", mgr)
	}
	if mgr := m.managerForHost("jomform.com"); mgr == nil || mgr.Email != "ops@example.com" {
		t.Errorf("managerForHost(primary) = %+v, want default email ops@example.com", mgr)
	}
	// Unverified host → no manager.
	if mgr := m.managerForHost("nope.example.com"); mgr != nil {
		t.Errorf("managerForHost(unverified) = %+v, want nil", mgr)
	}
}

func TestNoEmailNeverProvisions(t *testing.T) {
	// Issue #398 fallback: no per-domain email and no global email → the
	// domain is registered but never provisions (managerForHost returns nil).
	m := New(Options{Primary: "jomform.com"})
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
	m.AddVerified("shop.example.com", "")
	if mgr := m.managerForHost("shop.example.com"); mgr != nil {
		t.Errorf("managerForHost(no email) = %+v, want nil", mgr)
	}
	if err := m.hostPolicy(context.Background(), "shop.example.com"); err != nil {
		t.Errorf("hostPolicy(verified, no email) = %v, want nil (still allowed)", err)
	}
}

func TestTLSConfigAndHTTPHandler(t *testing.T) {
	m := New(Options{Email: "ops@example.com", Primary: "jomform.com"})
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
	if m.TLSConfig() == nil {
		t.Error("TLSConfig() = nil, want non-nil")
	}
	if m.HTTPHandler() == nil {
		t.Error("HTTPHandler() = nil, want non-nil")
	}
}

func TestCustomCertServing(t *testing.T) {
	m := New(Options{Email: "ops@example.com", Primary: "jomform.com"})
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
	// A custom-cert domain is allowed by hostPolicy even without verification.
	if err := m.hostPolicy(context.Background(), "shop.example.com"); err == nil {
		t.Error("hostPolicy(unverified, no custom cert) = nil, want error")
	}
	// Register a self-signed cert for the domain.
	cert := testSelfSignedCert(t, "shop.example.com")
	m.SetCustomCert("shop.example.com", cert)
	if err := m.hostPolicy(context.Background(), "shop.example.com"); err != nil {
		t.Errorf("hostPolicy(custom cert) = %v, want nil", err)
	}
	// HasCert reports false for a custom-cert domain (it is not ACME-managed).
	if m.HasCert("shop.example.com") {
		t.Error("HasCert(custom) = true, want false")
	}
	// RemoveCustomCert drops it back to unverified.
	m.RemoveCustomCert("shop.example.com")
	if err := m.hostPolicy(context.Background(), "shop.example.com"); err == nil {
		t.Error("hostPolicy(after RemoveCustomCert) = nil, want error")
	}
}

func TestHasCertNoCache(t *testing.T) {
	// No CertDir → no disk cache → HasCert always false.
	m := New(Options{Email: "ops@example.com", Primary: "jomform.com"})
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
	if m.HasCert("shop.example.com") {
		t.Error("HasCert(no cache) = true, want false")
	}
}

// testSelfSignedCert builds a throwaway self-signed cert + key for a host.
func testSelfSignedCert(t *testing.T, host string) *tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{host},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	return &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}
