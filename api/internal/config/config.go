// Package config loads runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
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

	// ChipAPIBaseURL overrides the CHIP Collect API root. Empty (default)
	// uses the production endpoint. Load tests point this at a local mock
	// so the payment create-purchase path can be exercised without hitting
	// the live CHIP sandbox.
	ChipAPIBaseURL string

	// SES SMTP (transactional email). Empty host disables email sends
	// (e.g. password-reset falls back to logged instructions).
	SESSMTPHost string
	SESSMTPPort string
	SESSMTPUser string
	SESSMTPPass string
	SESFrom     string
	SESRegion   string

	// SES reputation hardening (issue #56).
	//   BreakerMinSends: minimum verification sends in the trailing hour
	//     before the bounce-ratio breaker can trip (default 20).
	//   BreakerRatio: bounce ratio above which verification mail is paused
	//     (default 0.05 = 5%).
	//   SNSConfigSet: the SES configuration-set name that emits Bounce and
	//     Complaint notifications to the SNS topic → our notifications endpoint
	//     (documentation only; the endpoint does not need it to verify).
	//   SNSNotifEnabled: false (default) disables the /v1/internal/ses/notifications
	//     endpoint entirely; set to "true" in the deployment that receives SNS.
	BreakerMinSends int64
	BreakerRatio    float64
	SNSConfigSet    string
	SNSNotifEnabled bool

	// Event-tracking retention (issue #92). The periodic sweep deletes events
	// older than their TTL. EventRetentionDays is the default for the `events`
	// table; EventAuditRetentionDays overrides it for audit-trail event classes
	// (auth.*, settings.*) which warrant longer retention. DeliveryRetentionDays
	// applies to email_delivery_events. All default to 90 days.
	EventRetentionDays      int
	EventAuditRetentionDays int
	DeliveryRetentionDays   int
}

// Load reads configuration from the environment and validates it.
func Load() (*Config, error) {
	cfg := &Config{
		Port:           getEnv("PORT", "8080"),
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		ValkeyURL:      getEnv("VALKEY_URL", "valkey://localhost:6379/0"),
		S3Endpoint:     getEnv("S3_ENDPOINT", "http://localhost:9000"),
		S3AccessKey:    os.Getenv("S3_ACCESS_KEY"),
		S3SecretKey:    os.Getenv("S3_SECRET_KEY"),
		S3Bucket:       getEnv("S3_BUCKET", "jomform-media"),
		S3Region:       getEnv("S3_REGION", "us-east-1"),
		SessionSecret:  os.Getenv("SESSION_SECRET"),
		EncryptionKey:  os.Getenv("ENCRYPTION_KEY"),
		AppBaseURL:     getEnv("APP_BASE_URL", "http://localhost:8080"),
		Env:            getEnv("APP_ENV", "dev"),
		ChipAPIBaseURL: os.Getenv("CHIP_API_BASE_URL"),

		SESSMTPHost: os.Getenv("SES_SMTP_HOST"),
		SESSMTPPort: getEnv("SES_SMTP_PORT", "587"),
		SESSMTPUser: os.Getenv("SES_SMTP_USER"),
		SESSMTPPass: os.Getenv("SES_SMTP_PASS"),
		SESFrom:     os.Getenv("SES_FROM"),
		SESRegion:   getEnv("SES_REGION", "ap-southeast-1"),

		// SES reputation hardening (issue #56). Breaker ratio/min parse
		// leniently: unparseable values fall back to the SES-recommended
		// defaults rather than crash the server.
		BreakerMinSends: parsePositiveInt64(os.Getenv("SES_BREAKER_MIN_SENDS"), 20),
		BreakerRatio:    parseRatio(os.Getenv("SES_BREAKER_RATIO"), 0.05),
		SNSConfigSet:    os.Getenv("SES_CONFIG_SET"),
		SNSNotifEnabled: strings.EqualFold(os.Getenv("SES_SNS_NOTIF_ENABLED"), "true"),

		// Event-tracking retention (issue #92). Defaults to 90 days; the
		// audit-trail class (auth.*, settings.*) defaults to 180 days.
		EventRetentionDays:      parsePositiveInt(os.Getenv("EVENT_RETENTION_DAYS"), 90),
		EventAuditRetentionDays: parsePositiveInt(os.Getenv("EVENT_AUDIT_RETENTION_DAYS"), 180),
		DeliveryRetentionDays:   parsePositiveInt(os.Getenv("DELIVERY_RETENTION_DAYS"), 90),
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

// parsePositiveInt64 parses a positive integer, falling back on empty/invalid.
func parsePositiveInt64(s string, fallback int64) int64 {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

// parsePositiveInt parses a positive int, falling back on empty/invalid.
func parsePositiveInt(s string, fallback int) int {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

// parseRatio parses a ratio in [0,1] (e.g. "0.05"), falling back on error.
func parseRatio(s string, fallback float64) float64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f <= 0 || f > 1 {
		return fallback
	}
	return f
}
