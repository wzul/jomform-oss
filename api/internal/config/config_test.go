package config

import "testing"

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
	t.Setenv("SESSION_SECRET", "x")
	t.Setenv("ENCRYPTION_KEY", "short")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for short ENCRYPTION_KEY")
	}

	t.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if _, err := Load(); err != nil {
		t.Fatalf("unexpected error for valid key: %v", err)
	}
}
