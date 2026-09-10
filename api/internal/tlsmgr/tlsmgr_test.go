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

func TestNewDisabledWithoutEmail(t *testing.T) {
	if m := New(Options{}); m != nil {
		t.Fatal("expected nil manager when ACME email is empty")
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
	m.AddVerified("shop.example.com")
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
	m.AddVerified("  Shop.Example.com. ")
	if err := m.hostPolicy(context.Background(), "shop.example.com"); err != nil {
		t.Errorf("hostPolicy after AddVerified = %v, want nil", err)
	}
	// Idempotent.
	m.AddVerified("shop.example.com")
	if err := m.hostPolicy(context.Background(), "shop.example.com"); err != nil {
		t.Errorf("hostPolicy after second AddVerified = %v, want nil", err)
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
