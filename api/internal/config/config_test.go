package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadRequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected error when DATABASE_URL missing")
	}
}

func TestLoadDevDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/jomform_test")
	t.Setenv("APP_ENV", "dev")
	t.Setenv("ENCRYPTION_KEY", "") // optional in dev

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want 8080", cfg.Port)
	}
	if !cfg.IsDev() {
		t.Errorf("expected dev mode")
	}
}

func TestLoadEncryptionKeyValidation(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/jomform_test")
	t.Setenv("APP_ENV", "prod")
	t.Setenv("ENCRYPTION_KEY", "short")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for short ENCRYPTION_KEY")
	}

	t.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if _, err := Load(); err != nil {
		t.Fatalf("unexpected error for valid key: %v", err)
	}
}

// TestProdConfigNeedsNoSessionSecret pins the decision that SESSION_SECRET is
// not part of the contract. It was a startup REQUIREMENT with no reader: config
// loaded and validated it and nothing else in the API touched it. Sessions are
// opaque 32-byte random tokens stored in Valkey (internal/auth/session.go), not
// signed cookies, so there is nothing for a signing secret to sign — demanding
// one only forced every deployment to carry and protect a dead credential.
//
// A prod Load must therefore succeed with no SESSION_SECRET in the environment.
// If the requirement is ever re-added, this fails — and whoever adds it has to
// say what the value signs instead of reinstating a key nobody reads.
func TestProdConfigNeedsNoSessionSecret(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/jomform_test")
	t.Setenv("APP_ENV", "prod")
	t.Setenv("SESSION_SECRET", "")
	t.Setenv("ENCRYPTION_KEY", strings.Repeat("ab", 32))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("prod Load refused a config with no SESSION_SECRET: %v", err)
	}
	if cfg.IsDev() {
		t.Fatalf("expected the config to load as prod, got env %q", cfg.Env)
	}
}

func TestLoadSESReputationDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/jomform_test")
	t.Setenv("APP_ENV", "dev")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BreakerMinSends != 20 {
		t.Errorf("BreakerMinSends = %d, want default 20", cfg.BreakerMinSends)
	}
	if cfg.BreakerRatio != 0.05 {
		t.Errorf("BreakerRatio = %v, want default 0.05", cfg.BreakerRatio)
	}
	if cfg.SNSNotifEnabled {
		t.Error("SNSNotifEnabled should default to false")
	}
}

func TestLoadSESReputationEnvOverrides(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/jomform_test")
	t.Setenv("APP_ENV", "dev")
	t.Setenv("SES_BREAKER_MIN_SENDS", "10")
	t.Setenv("SES_BREAKER_RATIO", "0.03")
	t.Setenv("SES_SNS_NOTIF_ENABLED", "true")
	t.Setenv("SES_CONFIG_SET", "jomform-prod")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BreakerMinSends != 10 {
		t.Errorf("BreakerMinSends = %d, want 10", cfg.BreakerMinSends)
	}
	if cfg.BreakerRatio != 0.03 {
		t.Errorf("BreakerRatio = %v, want 0.03", cfg.BreakerRatio)
	}
	if !cfg.SNSNotifEnabled {
		t.Error("SNSNotifEnabled should be true when env set")
	}
	if cfg.SNSConfigSet != "jomform-prod" {
		t.Errorf("SNSConfigSet = %q, want jomform-prod", cfg.SNSConfigSet)
	}
}

func TestLoadSESReputationIgnoresGarbageEnv(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/jomform_test")
	t.Setenv("APP_ENV", "dev")
	// Unparseable values must fall back to sane defaults, not crash.
	t.Setenv("SES_BREAKER_MIN_SENDS", "abc")
	t.Setenv("SES_BREAKER_RATIO", "not-a-ratio")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BreakerMinSends != 20 {
		t.Errorf("BreakerMinSends = %d, want fallback 20", cfg.BreakerMinSends)
	}
	if cfg.BreakerRatio != 0.05 {
		t.Errorf("BreakerRatio = %v, want fallback 0.05", cfg.BreakerRatio)
	}
}

func TestLoadDBSlowQueryDefault(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/jomform_test")
	t.Setenv("APP_ENV", "dev")
	t.Setenv("DB_SLOW_QUERY_MS", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DBSlowQueryMS != 200*time.Millisecond {
		t.Errorf("DBSlowQueryMS = %v, want default 200ms", cfg.DBSlowQueryMS)
	}
}

func TestLoadDBSlowQueryEnvOverride(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/jomform_test")
	t.Setenv("APP_ENV", "dev")
	t.Setenv("DB_SLOW_QUERY_MS", "500")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DBSlowQueryMS != 500*time.Millisecond {
		t.Errorf("DBSlowQueryMS = %v, want 500ms", cfg.DBSlowQueryMS)
	}
}

func TestLoadDBSlowQueryIgnoresGarbage(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/jomform_test")
	t.Setenv("APP_ENV", "dev")
	t.Setenv("DB_SLOW_QUERY_MS", "abc")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DBSlowQueryMS != 200*time.Millisecond {
		t.Errorf("DBSlowQueryMS = %v, want fallback 200ms", cfg.DBSlowQueryMS)
	}
}

func TestLoadACMEDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/jomform_test")
	t.Setenv("APP_ENV", "dev")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ACMEEmail != "" {
		t.Errorf("ACMEEmail = %q, want empty default", cfg.ACMEEmail)
	}
	if cfg.ACMEDirectory != "" {
		t.Errorf("ACMEDirectory = %q, want empty default", cfg.ACMEDirectory)
	}
	if cfg.ACMECertDir != "/var/lib/jomform/acme" {
		t.Errorf("ACMECertDir = %q, want default /var/lib/jomform/acme", cfg.ACMECertDir)
	}
	if cfg.TLSPort != "8443" {
		t.Errorf("TLSPort = %q, want default 8443", cfg.TLSPort)
	}
}

func TestLoadACMEEnvOverrides(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/jomform_test")
	t.Setenv("APP_ENV", "dev")
	t.Setenv("ACME_EMAIL", "ops@example.com")
	t.Setenv("ACME_DIRECTORY", "https://acme-staging-v02.api.letsencrypt.org/directory")
	t.Setenv("ACME_CERT_DIR", "/data/acme")
	t.Setenv("TLS_PORT", "443")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ACMEEmail != "ops@example.com" {
		t.Errorf("ACMEEmail = %q, want ops@example.com", cfg.ACMEEmail)
	}
	if cfg.ACMEDirectory != "https://acme-staging-v02.api.letsencrypt.org/directory" {
		t.Errorf("ACMEDirectory = %q, want staging URL", cfg.ACMEDirectory)
	}
	if cfg.ACMECertDir != "/data/acme" {
		t.Errorf("ACMECertDir = %q, want /data/acme", cfg.ACMECertDir)
	}
	if cfg.TLSPort != "443" {
		t.Errorf("TLSPort = %q, want 443", cfg.TLSPort)
	}
}

func TestLoadSuperAdminEmails(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/jomform_test")
	t.Setenv("APP_ENV", "dev")

	// Unset → no super-admins.
	t.Setenv("SUPER_ADMIN_EMAILS", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.SuperAdminEmails) != 0 {
		t.Errorf("SuperAdminEmails = %v, want empty when unset", cfg.SuperAdminEmails)
	}
	if cfg.IsSuperAdmin("owner@example.com") {
		t.Error("IsSuperAdmin should be false with empty allowlist")
	}

	// Comma-separated allowlist, trimmed + lowercased.
	t.Setenv("SUPER_ADMIN_EMAILS", " Owner@Example.com , ops@example.com , ")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.SuperAdminEmails) != 2 {
		t.Fatalf("SuperAdminEmails = %v, want 2 entries", cfg.SuperAdminEmails)
	}
	if !cfg.IsSuperAdmin("owner@example.com") {
		t.Error("IsSuperAdmin should match allowlisted email (case-insensitive)")
	}
	if !cfg.IsSuperAdmin("OPS@EXAMPLE.COM") {
		t.Error("IsSuperAdmin should match allowlisted email regardless of case")
	}
	if cfg.IsSuperAdmin("someone@example.com") {
		t.Error("IsSuperAdmin should be false for a non-allowlisted email")
	}
	if cfg.IsSuperAdmin("") {
		t.Error("IsSuperAdmin should be false for an empty email")
	}
}
