package config

import (
	"os"
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

// TestLoadSelfHosted pins JOMFORM_SELF_HOSTED's contract, in both directions.
//
// The flag is what permits a deployment to serve its own published forms from
// its own origin. It MUST default off: the hosted product's APP_BASE_URL is its
// own apex, so a path keyed on "the request host is this install's origin"
// serves merchant forms on the apex (issue #77; regression measured on PR #1516
// with BaseURL=https://jomform.com and Host: jomform.com -> 200).
//
// And it must actually turn ON from the value the self-host bootstrap writes
// ("1"), or the one-command install comes up serving nothing — a guard that is
// an outage. isTruthy is shared with JOMFORM_LOG_VERIFY_LINK, so this also
// keeps "0" from reading as true.
func TestLoadSelfHosted(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"", false},      // compose's `${VAR:-}` when nobody set it
		{"0", false},     // explicitly off
		{"no", false},    // not a truthy spelling
		{"1", true},      // what deploy/selfhost/bootstrap.sh writes
		{"true", true},   // the other accepted spelling
		{" TRUE ", true}, // whitespace/case tolerated, like its sibling switch
	} {
		t.Run("value="+tc.value, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://localhost/jomform_test")
			t.Setenv("APP_ENV", "dev")
			t.Setenv("JOMFORM_SELF_HOSTED", tc.value)
			cfg, err := Load()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.SelfHosted != tc.want {
				t.Errorf("SelfHosted = %v for JOMFORM_SELF_HOSTED=%q, want %v", cfg.SelfHosted, tc.value, tc.want)
			}
		})
	}

	// Unset is the hosted product's shape. os.Unsetenv rather than "", because
	// "" is what a compose `${VAR:-}` hands over — and the default has to hold
	// for a deployment that never heard of the variable at all.
	t.Run("unset", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "postgres://localhost/jomform_test")
		t.Setenv("APP_ENV", "dev")
		os.Unsetenv("JOMFORM_SELF_HOSTED")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.SelfHosted {
			t.Error("SelfHosted is TRUE with JOMFORM_SELF_HOSTED unset — serving from the install's " +
				"own origin must be opt-in, or the hosted apex serves merchant forms (#77)")
		}
	})
}
