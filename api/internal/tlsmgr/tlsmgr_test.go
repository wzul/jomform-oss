package tlsmgr

import (
	"context"
	"log/slog"
	"testing"
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
