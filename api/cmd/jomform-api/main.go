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
	}

	// Payments wiring (CHIP): needs DB; decrypt key from config.
	if pool != nil {
		accounts := dbstore.NewAccountStore(pool)
		sales := dbstore.NewSaleStore(pool)
		subs := dbstore.NewSubscriptionStore(pool)
		events := dbstore.NewWebhookEventStore(pool)
		products := dbstore.NewProductStore(pool)
		keys := dbstore.NewAPIKeyStore(pool)

		formsStore := dbstore.NewFormStore(pool)
		spacesStore := dbstore.NewSpaceStore(pool)
		usersStore := dbstore.NewUserStore(pool)
		passkeysStore := dbstore.NewPasskeyStore(pool)
		pay := payments.New(chip.New(""), accounts, sales, subs, decryptKey, cfg.AppBaseURL)
		pay.Forms = formsStore
		if cfg.SESSMTPHost != "" {
			pay.Receipts = &payments.Receipts{
				Sender: email.New(email.SMTPConfig{
					Host: cfg.SESSMTPHost, Port: cfg.SESSMTPPort,
					Username: cfg.SESSMTPUser, Password: cfg.SESSMTPPass, From: cfg.SESFrom,
					Region: cfg.SESRegion,
				}),
				Base: cfg.AppBaseURL,
			}
		}
		deps.Payments = &httpapi.PaymentsHandlers{Service: pay, Events: events}

		// REST API for the web dashboard.
		whStore := dbstore.NewWebhookStore(pool)
		// Issue #61: S3/MinIO media store for image-block uploads. Nil when
		// S3 creds are absent (upload endpoint returns 503, app still boots).
		var store *httpapi.ObjectStore
		if cfg.S3AccessKey != "" && cfg.S3SecretKey != "" {
			store, err = httpapi.NewObjectStore(cfg.S3Endpoint, cfg.S3AccessKey,
				cfg.S3SecretKey, cfg.S3Bucket, cfg.S3Region)
			if err != nil {
				log.Error("s3 object store", "err", err)
				store = nil
			}
		} else {
			log.Warn("S3_ACCESS_KEY/S3_SECRET_KEY unset — image uploads disabled")
		}
		rest := &httpapi.RESTHandlers{
			Accounts: accounts, Products: products, Forms: formsStore,
			Keys: keys, Sales: sales, Sessions: sessions, ChipProbe: chip.New(""),
			SecretsKey: decryptKey, Pay: pay, Webhooks: whStore, Domains: dbstore.NewDomainStore(pool),
			SystemSettings: dbstore.NewSystemSettingStore(pool),
			Spaces:         spacesStore,
			Passkeys:       passkeysStore,
			Users:          usersStore,
			Store:          store,
		}
		pay.OnPaid = func(ctx context.Context, accountID, event string, payload map[string]any) {
			httpapi.DispatchWebhooks(ctx, rest, whStore, accountID, event, payload)
		}
		deps.REST = rest

		// MCP server — merchants' AI agents control their store here.
		mcpSrv := mcpsrv.New(&mcpsrv.Deps{
			Accounts: accounts, Products: products, Sales: sales,
			Subs: subs, Keys: keys, Forms: formsStore,
			Webhooks:   dbstore.NewWebhookStore(pool),
			Spaces:     spacesStore,
			DecryptKey: decryptKey,
		}, chip.New(""), decryptKey, pay)
		deps.MCP = mcpSrv

		// Public order-form renderer + submit.
		deps.Public = &httpapi.PublicHandlers{
			Forms: formsStore, Sales: sales, Accounts: accounts,
			Products: products, Pay: pay, BaseURL: cfg.AppBaseURL,
			SystemSettings: dbstore.NewSystemSettingStore(pool),
			Spaces:         spacesStore,
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
