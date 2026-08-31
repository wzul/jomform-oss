// Package config loads runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strings"
)

// Config holds all runtime settings for jomform-api.
type Config struct {
	Port          string
	DatabaseURL   string
	ValkeyURL     string
	S3Endpoint    string
	S3AccessKey   string
	S3SecretKey   string
	S3Bucket      string
	S3Region      string
	SessionSecret string
	// EncryptionKey must decode to exactly 32 bytes (hex) — used for AES-GCM
	// encryption of per-account CHIP secret keys at rest.
	EncryptionKey string
	AppBaseURL    string
	Env           string // dev | prod
}

// Load reads configuration from the environment and validates it.
func Load() (*Config, error) {
	cfg := &Config{
		Port:          getEnv("PORT", "8080"),
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		ValkeyURL:     getEnv("VALKEY_URL", "valkey://localhost:6379/0"),
		S3Endpoint:    getEnv("S3_ENDPOINT", "http://localhost:9000"),
		S3AccessKey:   os.Getenv("S3_ACCESS_KEY"),
		S3SecretKey:   os.Getenv("S3_SECRET_KEY"),
		S3Bucket:      getEnv("S3_BUCKET", "jomform-media"),
		S3Region:      getEnv("S3_REGION", "us-east-1"),
		SessionSecret: os.Getenv("SESSION_SECRET"),
		EncryptionKey: os.Getenv("ENCRYPTION_KEY"),
		AppBaseURL:    getEnv("APP_BASE_URL", "http://localhost:8080"),
		Env:           getEnv("APP_ENV", "dev"),
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.SessionSecret == "" && !cfg.IsDev() {
		return nil, fmt.Errorf("SESSION_SECRET is required outside dev")
	}
	if cfg.EncryptionKey != "" {
		if len(cfg.EncryptionKey) != 64 {
			return nil, fmt.Errorf("ENCRYPTION_KEY must be 64 hex chars (32 bytes)")
		}
	} else if !cfg.IsDev() {
		return nil, fmt.Errorf("ENCRYPTION_KEY is required outside dev")
	}
	return cfg, nil
}

// IsDev reports whether the app runs in development mode.
func (c *Config) IsDev() bool { return strings.EqualFold(c.Env, "dev") }

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}