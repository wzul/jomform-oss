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

	chatpkg "github.com/wzul/jomform/api/internal/chat"
	"github.com/wzul/jomform/api/internal/chip"
	"github.com/wzul/jomform/api/internal/currency"
	"github.com/wzul/jomform/api/internal/llm"
	"github.com/wzul/jomform/api/internal/payments"

	"github.com/wzul/jomform/api/internal/assetgate"
	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/config"
	"github.com/wzul/jomform/api/internal/db"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/email"
	"github.com/wzul/jomform/api/internal/emailqueue"
	"github.com/wzul/jomform/api/internal/feedbacksvc"
	"github.com/wzul/jomform/api/internal/httpapi"
	"github.com/wzul/jomform/api/internal/httpapi/middleware"
	"github.com/wzul/jomform/api/internal/mcpsrv"
	"github.com/wzul/jomform/api/internal/metrics"
	"github.com/wzul/jomform/api/internal/spamjudge"
	"github.com/wzul/jomform/api/internal/tlsmgr"
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

	// Issue #211: one shared metrics instance so the DB query tracer and the
	// HTTP middleware record into the same registry, exposed on /metrics.
	metricsInst := metrics.New()

	pool, err := db.ConnectWithTracer(ctx, cfg.DatabaseURL, metricsInst, log, cfg.DBSlowQueryMS)
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

	// Issue #185: BNM exchange-rate cache (Valkey). The daily job below
	// refreshes it; the REST/MCP endpoints read from it. Nil when Valkey is
	// absent (dev mode) — endpoints then return empty rates.
	var curStore *currency.Store
	if sessions != nil {
		curStore = currency.New(sessions.Client(), log)
	}

	// AES-GCM key for at-rest encryption AND email-verification link signing.
	decryptKey := mustHexKey(cfg.EncryptionKey)
	// Transactional sender (SESv2 API), nil when SMTP env is absent. Reused by
	// payment receipts and the email-verification flow.
	//
	// The Sender carries the suppression list itself so that
	// SendReceipt/SendReceiptWithAttachment — the two and only sesv2.SendEmail
	// calls in the repo — consult it before every send. That is the single
	// pre-send choke point: it covers buyer receipts, merchant notifications,
	// per-form notifications and auth/invite mail, sent synchronously or by the
	// background email worker, without each caller having to remember the check.
	var mailer *email.Sender
	if cfg.SESSMTPHost != "" {
		mailer = email.New(email.SMTPConfig{
			Host: cfg.SESSMTPHost, Port: cfg.SESSMTPPort,
			Username: cfg.SESSMTPUser, Password: cfg.SESSMTPPass, From: cfg.SESFrom,
			Region: cfg.SESRegion,
		})
	}

	// Issue #199: the auth mailer is a *email.Sender assigned into the
	// VerifyMailer INTERFACE (deps.VerifyMailer, dispatcher.AuthMailer,
	// rest.Mailer). A nil *email.Sender stored in an interface is NOT a nil
	// interface, so every `if mailer == nil` guard downstream is false and the
	// call walks into a nil receiver — SendReceipt → suppressed → SIGSEGV on
	// the email worker goroutine, which is not recovered, so the whole API
	// process died on the first registration. Only assign when SES is really
	// configured; with SES absent the fields stay nil interfaces and every
	// guard drops the job as designed.
	makeAuthMailer := func() httpapi.VerifyMailer {
		if mailer == nil {
			return nil
		}
		return mailer
	}

	deps := &httpapi.Deps{}
	// Issue #211: share the metrics instance with the HTTP server so the DB
	// query tracer and HTTP middleware record into the same registry.
	deps.Metrics = metricsInst
	// Issue #212: auto-TLS at origin for DNS-only custom domains. Issue
	// #398: the manager is always created so per-domain ACME emails (default
	// to the account email) can drive provisioning even without a global
	// ACME_EMAIL. The TLS listener is only started when auto-TLS is
	// configured (a global ACME_EMAIL or a cert dir for per-domain emails);
	// otherwise the origin serves plain HTTP and Cloudflare-proxied domains
	// keep using Cloudflare's edge TLS.
	tlsMgr := tlsmgr.New(tlsmgr.Options{
		Email:     cfg.ACMEEmail,
		Directory: cfg.ACMEDirectory,
		CertDir:   cfg.ACMECertDir,
		Primary:   primaryDomain(cfg.AppBaseURL),
		Log:       log,
	})
	deps.TLS = tlsMgr
	// Issue #398: the TLS listener + ACME challenge handler are only started
	// when auto-TLS is configured — a global ACME_EMAIL or a cert dir so
	// per-domain emails can drive provisioning.
	autoTLSEnabled := cfg.ACMEEmail != "" || cfg.ACMECertDir != ""
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
		// Nil interface when SES is unconfigured — see makeAuthMailer.
		deps.VerifyMailer = makeAuthMailer()
		// Issue #56: SES reputation hardening over the same Valkey client.
		// Suppression store + user store come from the pool below; breaker and
		// MX checker are Valkey-backed so they're wired when Valkey is up.
		deps.Breaker = email.NewBreaker(sessions.Client(), cfg.BreakerMinSends, cfg.BreakerRatio)
		deps.MXChecker = email.NewMXChecker(sessions.Client())
		// Issue #199: persistent background email queue over the same Valkey
		// client. The worker is started below once the dispatcher is wired.
		deps.EmailQueue = emailqueue.NewQueue(sessions.Client())
	}
	if pool != nil {
		deps.Suppressions = dbstore.NewSuppressionStore(pool)
		// Issue #92: event-tracking stores (audit log + SES delivery events).
		deps.Events = dbstore.NewEventStore(pool)
		deps.EmailDeliveryEvents = dbstore.NewEmailDeliveryEventStore(pool)
		// The auth mailer built above sends through the same choke point, so it
		// gets the suppression list too. Wired here because the store needs the
		// pool, which is created before the sender.
		if mailer != nil {
			mailer.Suppressor = deps.Suppressions
		}
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
		workspacesStore := dbstore.NewWorkspaceStore(pool)
		usersStore := dbstore.NewUserStore(pool)
		passkeysStore := dbstore.NewPasskeyStore(pool)
		// Issue #171: response-only form submissions.
		submissionsStore := dbstore.NewSubmissionStore(pool)
		// In-app feedback threads (system-owned, two-way conversation). NOT
		// account-scoped: a thread belongs to the user who filed it.
		feedbackStore := dbstore.NewFeedbackThreadStore(pool)
		// Issue #180: media library metadata store.
		mediaStore := dbstore.NewMediaStore(pool)
		// Issue #161: workspace membership + invites.
		workspaceMembersStore := dbstore.NewWorkspaceMemberStore(pool)
		deps.WorkspaceMembers = workspaceMembersStore
		// Issue #444: affiliate marketing.
		affiliateStore := dbstore.NewAffiliateStore(pool)
		deps.Affiliates = affiliateStore
		pay := payments.New(chip.New(cfg.ChipAPIBaseURL), accounts, sales, subs, decryptKey, cfg.AppBaseURL)
		pay.Products = products
		// Issue #623: a subscription bought as a product variation must resolve
		// that variation's own cycle on every later charge, exactly as the
		// checkout did — so the recurring engine gets the variation store too.
		pay.ProductVariations = dbstore.NewProductVariationStore(pool)
		pay.Forms = formsStore
		pay.Workspaces = workspacesStore
		pay.Users = usersStore
		// Issue #193: per-form notification rules (evaluated on form events).
		notificationsStore := dbstore.NewFormNotificationStore(pool)
		pay.Notifications = notificationsStore
		if cfg.SESSMTPHost != "" {
			receiptSender := email.New(email.SMTPConfig{
				Host: cfg.SESSMTPHost, Port: cfg.SESSMTPPort,
				Username: cfg.SESSMTPUser, Password: cfg.SESSMTPPass, From: cfg.SESFrom,
				Region: cfg.SESRegion,
			})
			// Issue #56: receipts (buyers) consult suppression but not the
			// verification breaker. The Sender now enforces that itself, at the
			// pre-send choke point, so every receipt/notification path is
			// guarded — including the merchant notification, whose old
			// exemption is deliberately removed.
			receiptSender.Suppressor = deps.Suppressions
			pay.Receipts = &payments.Receipts{
				Sender: receiptSender,
				Base:   cfg.AppBaseURL,
				// Kept as the pre-render early exit (avoids rendering a receipt
				// we will not send); the choke point is the load-bearing guard.
				Suppression: deps.Suppressions,
			}
		}
		// Issue #199: receipts + merchant notifications go through the
		// background email queue (nil when Valkey is absent → synchronous).
		pay.EmailQueue = deps.EmailQueue
		// Issue #557: EventLookup is REQUIRED. The dynamic callback-key path
		// resolves the owning account from the callback payload's purchase id
		// before it can fetch that account's CHIP verification key. Without it
		// the guard below is permanently false, no key is ever fetched, and
		// EVERY signed callback is rejected 503 by the fail-closed check — the
		// customer is charged while JomForm records the sale as pending forever.
		deps.Payments = &httpapi.PaymentsHandlers{
			Service:     pay,
			Events:      events,
			EventLookup: httpapi.NewCallbackLookup(sales, subs),
		}

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
		// The store is a *httpapi.ObjectStore assigned into INTERFACE fields
		// (rest.Store, public.Store, dispatcher.AssetScan.Store). A nil
		// *ObjectStore stored in an interface is NOT a nil interface, so every
		// `if store == nil` guard downstream is false and the call walks into a
		// nil receiver — GET /media/u/<id>/<file> answered 500 + a recovered
		// panic instead of the intended 503 "media storage tidak
		// dikonfigurasi". Only assign when S3 really is configured; with S3
		// absent the fields stay nil interfaces and every guard works as
		// designed. Same shape as makeAuthMailer above.
		makeStore := func() httpapi.ObjectStoreIface {
			if store == nil {
				return nil
			}
			return store
		}
		makeAssetFetcher := func() assetgate.ObjectFetcher {
			if store == nil {
				return nil
			}
			return store
		}
		// Issue #447: realtime notifications — in-memory pub/sub hub backing
		// the SSE stream (GET /v1/rest/events/stream). The payments OnPaid +
		// response.submitted paths publish sale/response events here so a
		// merchant's open dashboard updates live (no manual refresh).
		realtimeHub := httpapi.NewRealtimeHub()
		rest := &httpapi.RESTHandlers{
			Accounts: accounts, Products: products, Forms: formsStore,
			ProductVariations: dbstore.NewProductVariationStore(pool),
			Keys:              keys, Sales: sales, Subs: subs, Sessions: sessions, ChipProbe: chip.New(cfg.ChipAPIBaseURL),
			SecretsKey: decryptKey, Pay: pay, Webhooks: whStore, Domains: dbstore.NewDomainStore(pool),
			SystemSettings:   dbstore.NewSystemSettingStore(pool),
			Workspaces:       workspacesStore,
			WorkspaceMembers: workspaceMembersStore,
			Affiliates:       affiliateStore,
			Passkeys:         passkeysStore,
			// Issue #161: invite email delivery + signup link base.
			// Nil interface when SES is unconfigured — see makeAuthMailer.
			Mailer: makeAuthMailer(),
			// Issue #199: invite emails go through the background queue.
			EmailQueue: deps.EmailQueue,
			BaseURL:    cfg.AppBaseURL,
			Users:      usersStore,
			// Nil interface when S3 is unconfigured — see makeStore. Never a
			// nil *ObjectStore inside the interface: that shape skipped every
			// `h.Store == nil` guard and panicked the media handler.
			Store: makeStore(),
			// Issue #140: shared Domain so passkey sessions stay valid on
			// product subdomains (mcp.jomform.com).
			CookieDomain: httpapi.SharedCookieDomain(cfg.AppBaseURL),
			// Issue #92: audit/behaviour log.
			Events: deps.Events,
			// Issue #106: per-sale receipt email status (Sent/Delivered/Opened/Clicked).
			EmailDeliveryEvents: deps.EmailDeliveryEvents,
			// Issue #579: dead-letter view for the admin dashboard. `events` is
			// the webhook event store (NOT deps.Events, which is the audit log).
			WebhookEvents: events,
			// Issue #601: suppression list view/manage for the admin dashboard.
			Suppressions: dbstore.NewSuppressionStore(pool),
			// Issue #171: response-only form submissions.
			Submissions: submissionsStore,
			// Issue #193: per-form notification rules.
			Notifications: notificationsStore,
			// Issue #180: media library metadata store.
			Media: mediaStore,
			// Issue #364: background asset-scan queue. Code-asset uploads
			// enqueue an asset_scan job here; the worker classifies them
			// (deterministic + LLM) and flips scan_status before the asset
			// is served. Nil (no Valkey) keeps assets unserved (fail-closed).
			AssetQueue: deps.EmailQueue,
			// Issue #184: LLM client for auto-generating legal pages.
			LLM: llm.New(cfg.LLMBaseURL, cfg.LLMModel),
			// Issue #185: BNM exchange-rate cache.
			Currency: curStore,
			// Issue #212: auto-TLS at origin for DNS-only custom domains.
			TLS: tlsMgr,
			// Issue #385/#387: site super-admin allowlist (SUPER_ADMIN_EMAILS).
			// Gates the /v1/admin/overview endpoint (issue #387) so
			// platform-wide usage metrics are only visible to the owner.
			SuperAdmin: cfg.IsSuperAdmin,
			// Issue #447: realtime notifications — in-memory pub/sub hub backing
			// the SSE stream (GET /v1/rest/events/stream). The payments OnPaid +
			// response.submitted paths publish sale/response events here so a
			// merchant's open dashboard updates live (no manual refresh).
			Realtime: realtimeHub,
			// In-app feedback threads (system-owned form + admin triage).
			Feedback: feedbackStore,
			// The same super-admin allowlist, as a list: the feedback
			// notification addresses the platform team through the existing
			// email lane, so there is one place to change who is notified.
			SuperAdminEmails: cfg.SuperAdminEmails,
		}
		pay.OnPaid = func(ctx context.Context, accountID, event string, payload map[string]any) {
			// Issue #196: form-scoped webhooks fire for the sale's form; the
			// account-wide ones always fire. form_id is carried in the payload.
			formID, _ := payload["form_id"].(string)
			httpapi.DispatchWebhooks(ctx, rest, whStore, accountID, formID, event, payload)
			// Issue #447: push the event to the merchant's connected dashboards.
			realtimeHub.Publish(accountID, event, payload)
		}
		deps.REST = rest

		// MCP server — merchants' AI agents control their store here.
		mcpSrv := mcpsrv.New(&mcpsrv.Deps{
			Accounts: accounts, Products: products, Sales: sales,
			ProductVariations: dbstore.NewProductVariationStore(pool),
			Subs:              subs, Keys: keys, Forms: formsStore,
			Webhooks:         dbstore.NewWebhookStore(pool),
			Workspaces:       workspacesStore,
			WorkspaceMembers: workspaceMembersStore,
			Affiliates:       affiliateStore, // issue #444: affiliate marketing
			Users:            usersStore,     // issue #164: set_default_workspace tool
			Events:           deps.Events,    // issue #92: list_events tool
			// Issue #106: per-sale receipt email status tool.
			EmailDeliveryEvents: deps.EmailDeliveryEvents,
			WebhookEvents:       events,
			// Issue #601: suppression list tools (admin_list_suppressions /
			// admin_add_suppression / admin_remove_suppression).
			Suppressions: dbstore.NewSuppressionStore(pool),
			// Issue #171: response-only form submissions tool.
			Submissions: submissionsStore,
			// Issue #193: per-form notification rules tool.
			Notifications: notificationsStore,
			// Issue #180: media library tool.
			Media: mediaStore,
			// Issue #361/#363: object store backing media upload (upload_media
			// tool). Shared with the dashboard REST upload; nil when S3 unset.
			// This field is a CONCRETE *objstore.ObjectStore, so a plain nil
			// check is correct here — the typed-nil trap only exists where the
			// pointer is stored in an interface.
			Store: store,
			// Issue #364/#363: background asset-scan queue (upload_media code
			// assets enqueue an asset_scan job).
			AssetQueue: deps.EmailQueue,
			// Issue #185: BNM exchange-rate cache (get_currency_rates tool).
			Currency: curStore,
			// Issue #396: MCP-parity batch — custom domains, system settings,
			// passkeys, and the session store backing the 2FA pending-TOTP flow.
			Domains:        dbstore.NewDomainStore(pool),
			SystemSettings: dbstore.NewSystemSettingStore(pool),
			Passkeys:       passkeysStore,
			Sessions:       deps.Sessions,
			// Issue #121: allow OAuth access tokens to authenticate /mcp too.
			OAuth:      deps.OAuth,
			DecryptKey: decryptKey,
			// Issue #184: LLM client for auto-generating legal pages.
			LLM: llm.New(cfg.LLMBaseURL, cfg.LLMModel),
			// Issue #385: site super-admin allowlist (SUPER_ADMIN_EMAILS). Gates
			// platform-wide monitoring on explicit admin tools/pages only.
			SuperAdmin: cfg.IsSuperAdmin,
			// In-app feedback threads: the user's own threads, and every thread
			// for a super-admin (triage). User-scoped, not account-scoped.
			Feedback: feedbackStore,
			// The same notification the /feedback page sends when a thread is
			// filed: one lane, one place to change who is notified. A filing
			// through the MCP submit_feedback tool notifies exactly like the
			// page does.
			NotifyFeedback: rest.NotifyFeedbackAdmins,
			// The retry path for a notification that failed. Wired to the REST
			// handler's own function so the MCP retry_feedback_notify tool and
			// the dashboard's retry button are one implementation: both pick
			// the message to re-send and both go out through the same channel.
			RetryFeedbackNotify: rest.RetryFeedbackNotify,
		}, chip.New(cfg.ChipAPIBaseURL), decryptKey, pay)
		deps.MCP = mcpSrv

		// In-app chat (phase 1): the assistant answers questions about the
		// caller's OWN data and can file feedback — nothing else. It runs its
		// tool calls over the MCP server above under a READONLY identity
		// (wired in httpapi), so no sentence typed into the box can reach a
		// write. Wired with the same LLM client the legal-page generator uses:
		// there is no second LLM client in this repo.
		//
		// The runner is ALWAYS constructed; whether the assistant is offered is
		// the runner's own report (chat.Runner.Ready: a model with a credential),
		// which GET /v1/rest/chat publishes as `available`. Constructing it
		// unconditionally is why the flag could not be inferred from the object:
		// a deployment with no OLLAMA_API_KEY gets a runner that exists and
		// cannot answer, and the UI must hide the box rather than offer it.
		rest.Chat = chatpkg.New(llm.New(cfg.LLMBaseURL, cfg.LLMModel), mcpSrv, &feedbacksvc.Service{
			Store:  feedbackStore,
			Users:  usersStore,
			Notify: rest.NotifyFeedbackAdmins,
		})
		// Per-user message limit: every chat message is at least one LLM call.
		rest.ChatLimiter = httpapi.NewChatLimiter()
		// Phase 2: the proposal store and its own per-user confirmation limit.
		// The store is what makes a proposal stay WHAT THE USER WAS SHOWN
		// between propose and confirm (see dbstore/chat_proposal.go); without
		// it propose_write refuses rather than pretending, which is why wiring
		// is explicit here rather than optional.
		rest.Chat.(*chatpkg.Runner).SetProposalStore(dbstore.NewChatWriteProposalStore(pool))
		rest.ChatConfirmLimiter = httpapi.NewChatConfirmLimiter()

		// Public order-form renderer + submit.
		deps.Public = &httpapi.PublicHandlers{
			Forms: formsStore, Sales: sales, Accounts: accounts,
			// Issue #236: resolve custom domains via the account_domains table.
			Domains:  dbstore.NewDomainStore(pool),
			Products: products, ProductVariations: dbstore.NewProductVariationStore(pool),
			Pay: pay, BaseURL: cfg.AppBaseURL,
			SystemSettings: dbstore.NewSystemSettingStore(pool),
			Workspaces:     workspacesStore,
			// Issue #171: response-only form submissions.
			Submissions: submissionsStore,
			// Issue #176: sign the private-form unlock cookie.
			AccessKey: decryptKey,
			// Issue #244: deterministic anti-spam layers. Sessions/Users/
			// WorkspaceMembers resolve whether the submitter is a logged-in owner/admin
			// of the form's workspace (skips the per-form rate limit + time check).
			// FormLimiter is the per-form-per-IP token bucket (15/hour). MXChecker
			// validates the submitted email's domain accepts mail.
			Sessions:         deps.Sessions,
			Users:            deps.Users,
			WorkspaceMembers: deps.WorkspaceMembers,
			MXChecker:        deps.MXChecker,
			FormLimiter:      middleware.NewRateLimiter(15.0/3600.0, 15),
			// Issue #245: background AI spam judge. SpamQueue is the same
			// Valkey-backed queue as email (#199); the worker below routes
			// spam_judge jobs to the AI judge. Nil when Valkey is absent (dev).
			SpamQueue: deps.EmailQueue,
			// Issue #361: same media bucket serves merchant code assets on the
			// workspace/custom domain via /assets/{key}. A nil interface
			// disables the route (see makeStore).
			Store: makeStore(),
			// Issue #364: media metadata store gates code-asset serving on the
			// background scan status (only 'ok' is served).
			Media: mediaStore,
			// Issue #447: realtime notifications — publish response.submitted
			// events to the merchant's connected dashboards.
			Realtime: realtimeHub,
		}

		// Issue #199: start the background email worker. It polls the
		// Valkey-backed email queue and sends via SES, so a slow SES call never
		// blocks the payment/submit/auth response. The dispatcher routes each
		// job type to the right sender (auth mailer / payments receipt sender).
		// Issue #245: the same worker also routes spam_judge jobs to the AI
		// judge, so it starts whenever the queue is wired (even without SES).
		if deps.EmailQueue != nil && (mailer != nil || pay.Receipts != nil || submissionsStore != nil) {
			dispatcher := &httpapi.EmailDispatcher{
				// Nil interface when SES is unconfigured — see makeAuthMailer.
				AuthMailer: makeAuthMailer(),
				Breaker:    deps.Breaker,
				Pay:        pay,
				// Issue #245: the same background worker also routes
				// spam_judge jobs to the AI judge (Ollama Cloud gemma4). The
				// judge reads OLLAMA_API_KEY from the environment (never
				// hardcoded). Nil-safe: when the LLM client is absent the
				// spam_judge jobs are dropped.
				SpamJudge: &spamjudge.Handler{
					LLM:   llm.New(cfg.LLMBaseURL, cfg.LLMModel),
					Subs:  submissionsStore,
					Sales: sales,
				},
				// Issue #364: the same worker also routes asset_scan jobs to
				// the deterministic + LLM code-asset gate (Ollama Cloud). The
				// handler flips scan_status on the media row; only 'ok' assets
				// are served. Nil-safe: without a store or LLM the handler
				// leaves the asset unserved (fail-closed). The store is handed
				// over as a nil INTERFACE when S3 is unset (see makeAssetFetcher)
				// so the handler's `h.Store != nil` guard is not defeated by a
				// typed nil.
				AssetScan: &assetgate.Handler{
					Media: mediaStore,
					Store: makeAssetFetcher(),
					LLM:   llm.New(cfg.LLMBaseURL, cfg.LLMModel),
				},
			}
			deps.EmailWorker = emailqueue.NewWorker(deps.EmailQueue, dispatcher, log)
			go deps.EmailWorker.Run(ctx)
			log.Info("email_worker_started")
		}

		// Issue #197: start the webhook retry sweep. It polls the persistent
		// webhook_retries table and re-delivers due retries with exponential
		// backoff (1m/5m/30m/1h/6h). Retries survive an API restart because
		// they live in Postgres. A short interval keeps manual retries snappy.
		go httpapi.RunWebhookRetrySweep(ctx, rest, whStore, 30*time.Second)
		log.Info("webhook_retry_sweep_started")

		// Issue #517: the inbound CHIP callback claims its event row before
		// running the business logic, and a failed attempt releases it. This
		// sweep is the retry path for a claim whose attempt is gone (crash,
		// deploy restart, DB blip): it releases dead claims and replays
		// unprocessed events, so a paid sale can never be stranded.
		if deps.Payments != nil && deps.Payments.Events != nil && deps.Payments.Service != nil {
			go payments.RunInboundCallbackSweep(ctx, deps.Payments.Service, deps.Payments.Events, 5*time.Minute)
			log.Info("chip_callback_reclaim_sweep_started")
		}

		// Issue #185: daily BNM exchange-rate refresh (once daily, cached in
		// Valkey with a 24h TTL). Runs at startup and then every 24h. Failures are
		// logged and retried on the next tick — the cache keeps serving the last
		// good rates meanwhile.
		if curStore != nil {
			go func() {
				refresh := func() {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					if n, err := curStore.Refresh(ctx); err != nil {
						slog.Warn("bnm_rates_refresh", "err", err)
					} else {
						slog.Info("bnm_rates_refresh", "rates", n)
					}
				}
				refresh()
				ticker := time.NewTicker(24 * time.Hour)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						refresh()
					}
				}
			}()
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
				// Issue #190: purge unverified accounts (never confirmed their
				// email) older than UnverifiedPurgeDays. Only verified=false
				// rows are ever deleted; the store also cleans up the
				// auto-provisioned account/workspace so no orphans remain.
				//
				// Each candidate is its own transaction, so one user that
				// cannot be deleted (an owner account holding sales) no longer
				// rolls back the whole sweep. `blocked` reports those
				// candidates, so "nothing to purge" and "every candidate is
				// blocked" are distinguishable in this log line.
				if deps.Users != nil {
					cutoff := time.Now().Add(-time.Duration(cfg.UnverifiedPurgeDays) * 24 * time.Hour)
					if res, err := deps.Users.DeleteUnverifiedOlderThan(ctx, cutoff); err != nil {
						slog.Warn("unverified_purge_sweep", "err", err)
					} else if res.Deleted > 0 || res.Blocked > 0 {
						slog.Info("unverified_purge_sweep", "deleted", res.Deleted, "blocked", res.Blocked)
					}
				}
				// Issue #245: spam retention sweep. Spam-flagged submissions
				// and sales older than SpamRetentionDays (default 30) are
				// auto-deleted permanently. Un-spammed entries are never
				// touched.
				if submissionsStore != nil || sales != nil {
					spamCutoff := time.Now().Add(-time.Duration(cfg.SpamRetentionDays) * 24 * time.Hour)
					if submissionsStore != nil {
						if n, err := submissionsStore.DeleteSpamOlderThan(ctx, spamCutoff); err != nil {
							slog.Warn("spam_retention_sweep_submissions", "err", err)
						} else if n > 0 {
							slog.Info("spam_retention_sweep_submissions", "deleted", n)
						}
					}
					if sales != nil {
						if n, err := sales.DeleteSpamOlderThan(ctx, spamCutoff); err != nil {
							slog.Warn("spam_retention_sweep_sales", "err", err)
						} else if n > 0 {
							slog.Info("spam_retention_sweep_sales", "deleted", n)
						}
					}
				}
				cancel()
				<-ticker.C
			}
		}()
	}

	handler := httpapi.NewServer(cfg, log, deps).Handler()
	// Issue #212: mount the ACME HTTP-01 challenge handler so the origin can
	// answer Let's Encrypt challenges for DNS-only custom domains. No-op when
	// auto-TLS is disabled.
	if autoTLSEnabled {
		acmeHandler := tlsMgr.HTTPHandler()
		base := handler
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/.well-known/acme-challenge/") {
				acmeHandler.ServeHTTP(w, r)
				return
			}
			base.ServeHTTP(w, r)
		})
	}

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
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

	// Issue #212: origin TLS listener with SNI-based cert selection. Serves
	// the same handler over TLS on TLSPort, using autocert to provision +
	// renew Let's Encrypt certs for verified custom domains (and the primary
	// domain). Only started when auto-TLS is configured.
	var tlsSrv *http.Server
	if autoTLSEnabled {
		tlsSrv = &http.Server{
			Addr:              ":" + cfg.TLSPort,
			Handler:           handler,
			TLSConfig:         tlsMgr.TLSConfig(),
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      60 * time.Second,
			IdleTimeout:       120 * time.Second,
		}
		go func() {
			log.Info("jomform-api tls starting", "port", cfg.TLSPort)
			// ListenAndServeTLS with empty cert/key uses the TLSConfig's
			// GetCertificate (autocert SNI-based selection).
			if err := tlsSrv.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error("tls_listen", "err", err)
				stop()
			}
		}()
	}

	<-ctx.Done()
	log.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("shutdown", "err", err)
	}
	if tlsSrv != nil {
		if err := tlsSrv.Shutdown(shutdownCtx); err != nil {
			log.Error("tls_shutdown", "err", err)
		}
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

// primaryDomain extracts the bare hostname from an app base URL
// (e.g. "https://jomform.com" → "jomform.com"). Used as the fallback
// cert target for the origin auto-TLS listener (issue #212).
func primaryDomain(appBaseURL string) string {
	u := strings.TrimSpace(appBaseURL)
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	u = strings.TrimSuffix(u, "/")
	if i := strings.IndexByte(u, '/'); i >= 0 {
		u = u[:i]
	}
	if i := strings.IndexByte(u, ':'); i >= 0 {
		u = u[:i]
	}
	return u
}

// mustHexKey decodes the 64-char hex ENCRYPTION_KEY into 32 bytes.
func mustHexKey(hex64 string) []byte {
	b, err := hex.DecodeString(hex64)
	if err != nil || len(b) != 32 {
		return nil
	}
	return b
}
