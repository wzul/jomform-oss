// Command jomform-api is the JomForm backend: REST API today, MCP + worker as
// milestones land. Single binary, config from environment.
package main

import (
	"context"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/wzul/jomform/api/internal/chip"
	"github.com/wzul/jomform/api/internal/payments"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/config"
	"github.com/wzul/jomform/api/internal/db"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/email"
	"github.com/wzul/jomform/api/internal/httpapi"
	"github.com/wzul/jomform/api/internal/mcpsrv"
	"github.com/wzul/jomform/api/internal/version"
)

func main() {
	log := slog.Default()

	cfg, err := config.Load()
	if err != nil {
		log.Error("config", "err", err)
		osExit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("database", "err", err)
		if !cfg.IsDev() {
			osExit(1)
		}
		log.Warn("continuing without database (dev mode)")
	} else if err := db.Migrate(ctx, pool, log); err != nil {
		log.Error("migrations", "err", err)
		osExit(1)
	} else if pool != nil {
		// Issue #145: rewrite already-stored internal media URLs
		// (http://minio:9000/jomform-media/...) to the browser-reachable
		// public base. Idempotent — no-op once no stored URL has the old
		// internal prefix.
		oldPrefix := strings.TrimRight(cfg.S3Endpoint, "/") + "/" + cfg.S3Bucket
		newBase := cfg.S3PublicBase
		if newBase == "" {
			newBase = strings.TrimRight(cfg.AppBaseURL, "/") + "/media"
		}
		if n, merr := dbstore.MigrateMediaURLs(ctx, pool, oldPrefix, newBase); merr != nil {
			log.Error("media_url_migration", "err", merr)
			osExit(1)
		} else if n > 0 {
			log.Info("media_url_migration", "rows", n)
		}
	}

	sessions, err := auth.NewSessions(cfg.ValkeyURL)
	if err != nil {
		log.Error("valkey", "err", err)
		if !cfg.IsDev() {
			osExit(1)
		}
		log.Warn("continuing without valkey (dev mode)")
	}

	// AES-GCM key for at-rest encryption AND email-verification link signing.
	decryptKey := mustHexKey(cfg.EncryptionKey)
	// Transactional sender (SESv2 API), nil when SMTP env is absent. Reused by
	// payment receipts and the email-verification flow.
	var mailer *email.Sender
	if cfg.SESSMTPHost != "" {
		mailer = email.New(email.SMTPConfig{
			Host: cfg.SESSMTPHost, Port: cfg.SESSMTPPort,
			Username: cfg.SESSMTPUser, Password: cfg.SESSMTPPass, From: cfg.SESFrom,
			Region: cfg.SESRegion,
		})
	}

	deps := &httpapi.Deps{}
	if pool != nil {
		accounts := dbstore.NewAccountStore(pool)
		deps.Users = dbstore.NewUserStore(pool)
		deps.Accounts = accounts
	}
	if sessions != nil {
		deps.Sessions = sessions
		// Issue #42: the AES-GCM key signs email-verification links; the SES
		// sender delivers them (nil when unconfigured — registration still
		// completes, verification just can't be emailed).
		deps.VerifyKey = decryptKey
		deps.VerifyMailer = mailer
		// Issue #56: SES reputation hardening over the same Valkey client.
		// Suppression store + user store come from the pool below; breaker and
		// MX checker are Valkey-backed so they're wired when Valkey is up.
		deps.Breaker = email.NewBreaker(sessions.Client(), cfg.BreakerMinSends, cfg.BreakerRatio)
		deps.MXChecker = email.NewMXChecker(sessions.Client())
	}
	if pool != nil {
		deps.Suppressions = dbstore.NewSuppressionStore(pool)
		// Issue #92: event-tracking stores (audit log + SES delivery events).
		deps.Events = dbstore.NewEventStore(pool)
		deps.EmailDeliveryEvents = dbstore.NewEmailDeliveryEventStore(pool)
	}

	// Payments wiring (CHIP): needs DB; decrypt key from config.
	if pool != nil {
		accounts := dbstore.NewAccountStore(pool)
		sales := dbstore.NewSaleStore(pool)
		subs := dbstore.NewSubscriptionStore(pool)
		events := dbstore.NewWebhookEventStore(pool)
		products := dbstore.NewProductStore(pool)
		keys := dbstore.NewAPIKeyStore(pool)
		// Issue #106: SNS endpoint correlates Open/Click/Delivered to a sale
		// via the receipt's SES MessageID.
		deps.Sales = sales
		// Issue #121: OAuth 2.1 store for MCP clients (DCR + auth codes +
		// opaque access/refresh tokens). Wired whenever the DB is up.
		deps.OAuth = dbstore.NewOAuthStore(pool)

		formsStore := dbstore.NewFormStore(pool)
		spacesStore := dbstore.NewSpaceStore(pool)
		usersStore := dbstore.NewUserStore(pool)
		passkeysStore := dbstore.NewPasskeyStore(pool)
		// Issue #171: response-only form submissions.
		submissionsStore := dbstore.NewSubmissionStore(pool)
		// Issue #180: media library metadata store.
		mediaStore := dbstore.NewMediaStore(pool)
		// Issue #161: space membership + invites.
		spaceMembersStore := dbstore.NewSpaceMemberStore(pool)
		deps.SpaceMembers = spaceMembersStore
		pay := payments.New(chip.New(cfg.ChipAPIBaseURL), accounts, sales, subs, decryptKey, cfg.AppBaseURL)
		pay.Products = products
		pay.Forms = formsStore
		pay.Spaces = spacesStore
		pay.Users = usersStore
		if cfg.SESSMTPHost != "" {
			pay.Receipts = &payments.Receipts{
				Sender: email.New(email.SMTPConfig{
					Host: cfg.SESSMTPHost, Port: cfg.SESSMTPPort,
					Username: cfg.SESSMTPUser, Password: cfg.SESSMTPPass, From: cfg.SESFrom,
					Region: cfg.SESRegion,
				}),
				Base: cfg.AppBaseURL,
				// Issue #56: receipts (buyers) consult suppression but not the
				// verification breaker.
				Suppression: dbstore.NewSuppressionStore(pool),
			}
		}
		deps.Payments = &httpapi.PaymentsHandlers{Service: pay, Events: events}

		// REST API for the web dashboard.
		whStore := dbstore.NewWebhookStore(pool)
		// Issue #61: S3/MinIO media store for image-block uploads. Nil when
		// S3 creds are absent (upload endpoint returns 503, app still boots).
		var store *httpapi.ObjectStore
		if cfg.S3AccessKey != "" && cfg.S3SecretKey != "" {
			// Issue #145: publicBase is the browser-reachable media base
			// (S3_PUBLIC_BASE, defaulting to {AppBaseURL}/media) so uploaded
			// media URLs are loadable by browsers instead of pointing at the
			// internal MinIO host.
			publicBase := cfg.S3PublicBase
			if publicBase == "" {
				publicBase = strings.TrimRight(cfg.AppBaseURL, "/") + "/media"
			}
			store, err = httpapi.NewObjectStore(cfg.S3Endpoint, cfg.S3AccessKey,
				cfg.S3SecretKey, cfg.S3Bucket, cfg.S3Region, publicBase)
			if err != nil {
				log.Error("s3 object store", "err", err)
				store = nil
			} else {
				// Issue #79: provision the media bucket (create + public-read)
				// at startup so a fresh MinIO/S3 can't silently regress into the
				// NoSuchBucket 500 that broke builder image uploads. Idempotent —
				// no-op when the bucket already exists.
				if err := store.EnsureBucket(ctx); err != nil {
					log.Error("s3 bucket bootstrap", "err", err)
					store = nil
				}
			}
		} else {
			log.Warn("S3_ACCESS_KEY/S3_SECRET_KEY unset — image uploads disabled")
		}
		rest := &httpapi.RESTHandlers{
			Accounts: accounts, Products: products, Forms: formsStore,
			ProductVariations: dbstore.NewProductVariationStore(pool),
			Keys: keys, Sales: sales, Subs: subs, Sessions: sessions, ChipProbe: chip.New(cfg.ChipAPIBaseURL),
			SecretsKey: decryptKey, Pay: pay, Webhooks: whStore, Domains: dbstore.NewDomainStore(pool),
			SystemSettings: dbstore.NewSystemSettingStore(pool),
			Spaces:         spacesStore,
			SpaceMembers:   spaceMembersStore,
			Passkeys:       passkeysStore,
			// Issue #161: invite email delivery + signup link base.
			Mailer:   mailer,
			BaseURL:  cfg.AppBaseURL,
			Users:          usersStore,
			Store:          store,
			// Issue #140: shared Domain so passkey sessions stay valid on
			// product subdomains (mcp.jomform.com).
			CookieDomain: httpapi.SharedCookieDomain(cfg.AppBaseURL),
			// Issue #92: audit/behaviour log.
			Events: deps.Events,
			// Issue #106: per-sale receipt email status (Sent/Delivered/Opened/Clicked).
			EmailDeliveryEvents: deps.EmailDeliveryEvents,
			// Issue #171: response-only form submissions.
			Submissions: submissionsStore,
			// Issue #180: media library metadata store.
			Media: mediaStore,
		}
		pay.OnPaid = func(ctx context.Context, accountID, event string, payload map[string]any) {
			httpapi.DispatchWebhooks(ctx, rest, whStore, accountID, event, payload)
		}
		deps.REST = rest

		// MCP server — merchants' AI agents control their store here.
		mcpSrv := mcpsrv.New(&mcpsrv.Deps{
			Accounts: accounts, Products: products, Sales: sales,
			ProductVariations: dbstore.NewProductVariationStore(pool),
			Subs: subs, Keys: keys, Forms: formsStore,
			Webhooks: dbstore.NewWebhookStore(pool),
			Spaces:   spacesStore,
			SpaceMembers: spaceMembersStore,
			Users:    usersStore, // issue #164: set_default_space tool
			Events:   deps.Events, // issue #92: list_events tool
			// Issue #106: per-sale receipt email status tool.
			EmailDeliveryEvents: deps.EmailDeliveryEvents,
			// Issue #171: response-only form submissions tool.
			Submissions: submissionsStore,
			// Issue #180: media library tool.
			Media: mediaStore,
			// Issue #121: allow OAuth access tokens to authenticate /mcp too.
			OAuth:      deps.OAuth,
			DecryptKey: decryptKey,
		}, chip.New(cfg.ChipAPIBaseURL), decryptKey, pay)
		deps.MCP = mcpSrv

		// Public order-form renderer + submit.
		deps.Public = &httpapi.PublicHandlers{
			Forms: formsStore, Sales: sales, Accounts: accounts,
			Products: products, ProductVariations: dbstore.NewProductVariationStore(pool),
			Pay: pay, BaseURL: cfg.AppBaseURL,
			SystemSettings: dbstore.NewSystemSettingStore(pool),
			Spaces:         spacesStore,
			// Issue #171: response-only form submissions.
			Submissions: submissionsStore,
			// Issue #176: sign the private-form unlock cookie.
			AccessKey: decryptKey,
		}

		// Subscription charge runner: hourly sweep for due charges + overdue cancels.
		go func() {
			ticker := time.NewTicker(1 * time.Hour)
			defer ticker.Stop()
			for {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				if err := pay.RunDueCharges(ctx); err != nil && err != dbstore.ErrNoDueSubs {
					slog.Warn("due_charges_sweep", "err", err)
				}
				if err := pay.CancelOverdueSubs(ctx); err != nil {
					slog.Warn("overdue_subs_sweep", "err", err)
				}
				// Issue #92: event-tracking retention sweep. Per-class TTL: the
				// audit-trail classes (auth.*, settings.*) keep longer than the
				// default behaviour log; email_delivery_events has its own TTL.
				if deps.Events != nil {
					ttlByClass := map[string]time.Duration{
						"auth.":     time.Duration(cfg.EventAuditRetentionDays) * 24 * time.Hour,
						"settings.": time.Duration(cfg.EventAuditRetentionDays) * 24 * time.Hour,
					}
					if n, err := deps.Events.Sweep(ctx, ttlByClass, time.Duration(cfg.EventRetentionDays)*24*time.Hour); err != nil {
						slog.Warn("event_retention_sweep", "err", err)
					} else if n > 0 {
						slog.Info("event_retention_sweep", "deleted", n)
					}
				}
				if deps.EmailDeliveryEvents != nil {
					if n, err := deps.EmailDeliveryEvents.Sweep(ctx, time.Duration(cfg.DeliveryRetentionDays)*24*time.Hour); err != nil {
						slog.Warn("delivery_event_retention_sweep", "err", err)
					} else if n > 0 {
						slog.Info("delivery_event_retention_sweep", "deleted", n)
					}
				}
				cancel()
				<-ticker.C
			}
		}()
	}

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           httpapi.NewServer(cfg, log, deps).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		log.Info("jomform-api starting",
			"version", version.Version, "commit", version.Commit,
			"env", cfg.Env, "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("listen", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("shutdown", "err", err)
	}
	if sessions != nil {
		_ = sessions.Close()
	}
	if pool != nil {
		pool.Close()
	}
	log.Info("jomform-api stopped")
}

// osExit is swappable in tests.
var osExit = os.Exit

// mustHexKey decodes the 64-char hex ENCRYPTION_KEY into 32 bytes.
func mustHexKey(hex64 string) []byte {
	b, err := hex.DecodeString(hex64)
	if err != nil || len(b) != 32 {
		return nil
	}
	return b
}
