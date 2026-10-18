// Package config loads runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime settings for jomform-api.
type Config struct {
	Port        string
	DatabaseURL string
	ValkeyURL   string
	S3Endpoint  string
	S3AccessKey string
	S3SecretKey string
	S3Bucket    string
	S3Region    string
	// S3PublicBase is the browser-reachable base URL for uploaded media
	// (issue #145). It must point at the public media route (e.g.
	// https://jomform.com/media), NOT the internal S3/MinIO endpoint. When
	// empty, the API derives it from AppBaseURL + "/media".
	S3PublicBase  string
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
	// SESReplyTo (SES_REPLY_TO, comma-separated) is the monitored mailbox a
	// recipient's Reply button must reach. It exists because every message is
	// From noreply@jomform.com — an address with no reader, so a reply sent
	// there is lost silently and the conversation looks abandoned. Written as
	// the Reply-To header on EVERY message the SES sender emits (both the
	// Simple and the raw-MIME path), so no individual call site has to
	// remember it. Empty = no header.
	SESReplyTo []string

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

	// UnverifiedPurgeDays (issue #190): unverified accounts (never confirmed
	// their email) older than this many days are auto-deleted by the hourly
	// sweep. Defaults to 7.
	UnverifiedPurgeDays int

	// SpamRetentionDays (issue #245): spam-flagged submissions/sales older
	// than this many days are auto-deleted permanently by the hourly sweep.
	// Defaults to 30. Un-spammed entries are never auto-deleted.
	SpamRetentionDays int

	// OAuth 2.1 (issue #121): authorization-code TTL and access/refresh-token
	// lifetimes for MCP clients. Defaults match the issue (10-min code,
	// 1-hour access, 30-day refresh).
	OAuthCodeTTLSeconds         time.Duration
	OAuthAccessTokenTTLSeconds  time.Duration
	OAuthRefreshTokenTTLSeconds time.Duration
	// OAuthDCRBurst caps how many dynamic clients one IP can register in a
	// burst (RegistrationLimit-style guard, issue #42 pattern).
	OAuthDCRRate  float64
	OAuthDCRBurst float64

	// LLM (issue #184): Ollama Cloud OpenAI-compatible endpoint + model used
	// to auto-generate per-workspace legal pages. Empty base URL/model use the
	// defaults (https://ollama.com/v1, deepseek-v4.1-flash:1017).
	LLMBaseURL string
	LLMModel   string

	// DBSlowQueryMS (issue #211): queries taking longer than this many
	// milliseconds are logged as slow and counted in
	// jomform_db_slow_queries_total. Defaults to 200ms.
	DBSlowQueryMS time.Duration

	// Auto-TLS at origin (issue #212): Let's Encrypt/ACME provisioning for
	// DNS-only (grey-cloud) custom domains that point straight at the origin.
	//   ACMEEmail: contact for Let's Encrypt expiry/revocation notices.
	//     Empty disables the origin TLS listener (default: plain HTTP only).
	//   ACMEDirectory: ACME directory URL. Empty uses the Let's Encrypt
	//     production endpoint (autocert default).
	//   ACMECertDir: directory where issued certs + account keys are cached
	//     for renewal persistence (autocert.DirCache).
	//   TLSPort: port for the origin TLS listener (SNI-based cert selection).
	ACMEEmail     string
	ACMEDirectory string
	ACMECertDir   string
	TLSPort       string

	// SuperAdminEmails (issue #385): comma-separated email allowlist of site
	// super-admins. A user whose email matches is a super-admin, unlocking
	// platform-wide monitoring across all workspaces — but ONLY on explicitly
	// gated admin tools/pages (MCP admin tools #386, admin dashboard #387),
	// never implicitly on normal tools. Empty = no super-admins.
	SuperAdminEmails []string
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
		S3PublicBase:   os.Getenv("S3_PUBLIC_BASE"),
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
		// The monitored mailbox a recipient's Reply reaches. Deliberately NOT
		// defaulted to SES_FROM: every message JomForm sends is From
		// noreply@jomform.com, which nothing reads, so a default here would
		// re-create the black hole this variable exists to close. Unset = no
		// Reply-To header (unchanged behaviour) rather than a plausible-looking
		// address nobody monitors.
		SESReplyTo: parseEmailList(os.Getenv("SES_REPLY_TO")),

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

		// Unverified-account purge (issue #190): default 7 days.
		UnverifiedPurgeDays: parsePositiveInt(os.Getenv("UNVERIFIED_PURGE_DAYS"), 7),

		// Spam retention (issue #245): default 30 days.
		SpamRetentionDays: parsePositiveInt(os.Getenv("SPAM_RETENTION_DAYS"), 30),

		// OAuth 2.1 (issue #121): sensible defaults, overridable per env.
		OAuthCodeTTLSeconds:         time.Duration(parsePositiveInt64(os.Getenv("OAUTH_CODE_TTL_SECONDS"), 600)) * time.Second,
		OAuthAccessTokenTTLSeconds:  time.Duration(parsePositiveInt64(os.Getenv("OAUTH_ACCESS_TOKEN_TTL_SECONDS"), 3600)) * time.Second,
		OAuthRefreshTokenTTLSeconds: time.Duration(parsePositiveInt64(os.Getenv("OAUTH_REFRESH_TOKEN_TTL_SECONDS"), 2592000)) * time.Second,
		OAuthDCRRate:                parseRatio(os.Getenv("OAUTH_DCR_RATE"), 1.0/3600.0),
		OAuthDCRBurst:               parsePositiveFloat(os.Getenv("OAUTH_DCR_BURST"), 5.0),

		// Issue #184: LLM legal-page generation (Ollama Cloud).
		LLMBaseURL: os.Getenv("LLM_BASE_URL"),
		LLMModel:   os.Getenv("LLM_MODEL"),

		// Issue #211: slow-query threshold (default 200ms).
		DBSlowQueryMS: time.Duration(parsePositiveInt64(os.Getenv("DB_SLOW_QUERY_MS"), 200)) * time.Millisecond,

		// Issue #212: auto-TLS at origin for DNS-only custom domains.
		ACMEEmail:     os.Getenv("ACME_EMAIL"),
		ACMEDirectory: os.Getenv("ACME_DIRECTORY"),
		ACMECertDir:   getEnv("ACME_CERT_DIR", "/var/lib/jomform/acme"),
		TLSPort:       getEnv("TLS_PORT", "8443"),

		// Issue #385: site super-admin email allowlist (comma-separated).
		// Trimmed + lowercased so matching is robust to whitespace/case.
		SuperAdminEmails: parseEmailList(os.Getenv("SUPER_ADMIN_EMAILS")),
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

// IsSuperAdmin reports whether the given email is on the site super-admin
// allowlist (issue #385). Matching is case-insensitive and trims whitespace.
// An empty allowlist (or empty email) is never a super-admin.
func (c *Config) IsSuperAdmin(email string) bool {
	if email == "" || len(c.SuperAdminEmails) == 0 {
		return false
	}
	want := strings.ToLower(strings.TrimSpace(email))
	for _, e := range c.SuperAdminEmails {
		if e == want {
			return true
		}
	}
	return false
}

// parseEmailList splits a comma-separated email allowlist into trimmed,
// lowercased entries, dropping empties. Returns nil for an empty/unset value.
func parseEmailList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(s, ",") {
		if e := strings.ToLower(strings.TrimSpace(part)); e != "" {
			out = append(out, e)
		}
	}
	return out
}

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

// parsePositiveFloat parses a positive float, falling back on empty/invalid.
func parsePositiveFloat(s string, fallback float64) float64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f <= 0 {
		return fallback
	}
	return f
}
