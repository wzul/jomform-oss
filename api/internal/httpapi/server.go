package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/config"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/email"
	"github.com/wzul/jomform/api/internal/emailqueue"
	"github.com/wzul/jomform/api/internal/httpapi/middleware"
	"github.com/wzul/jomform/api/internal/mcpsrv"
	"github.com/wzul/jomform/api/internal/metrics"
	"github.com/wzul/jomform/api/internal/oauthmeta"
	"github.com/wzul/jomform/api/internal/tlsmgr"
	"github.com/wzul/jomform/api/internal/version"
)

// Deps carries wired-in infrastructure (nil in bare-mode tests).
type Deps struct {
	Payments *PaymentsHandlers
	Accounts *dbstore.AccountStore
	REST     *RESTHandlers
	Public   *PublicHandlers
	MCP      *mcpsrv.Server
	Users    *dbstore.UserStore
	Sessions *auth.Sessions
	// Issue #161: workspace membership + invites (redeemed on signup).
	WorkspaceMembers *dbstore.WorkspaceMemberStore
	// Issue #444: affiliate marketing.
	Affiliates *dbstore.AffiliateStore
	// Issue #42: brute-force + registration hardening.
	//   Sessions exposes the shared go-redis client used for the failed-login
	//   counters. VerifyKey is the 32-byte AES-GCM key used to sign/verify the
	//   email-confirmation links. VerifyMailer delivers the confirmation email
	//   (nil in bare-mode tests where sends aren't exercised).
	VerifyKey    []byte
	VerifyMailer VerifyMailer

	// Issue #56: SES reputation hardening. Suppressions is the suppression
	// store (bounce/complaint → never resend). Breaker gates verification
	// sends on bounce ratio. MXChecker resolves recipient domains pre-email.
	// All are nil in bare-mode tests; register/send handle nil defensively.
	Suppressions *dbstore.SuppressionStore
	Breaker      *email.Breaker
	MXChecker    *email.MXChecker

	// Issue #92: event-tracking audit/behaviour log. Nil in bare-mode tests;
	// Record calls are no-ops when nil.
	Events *dbstore.EventStore

	// Issue #92: SES delivery-event log (bounce/complaint). Nil in bare-mode
	// tests; the SNS endpoint skips the write when nil.
	EmailDeliveryEvents *dbstore.EmailDeliveryEventStore

	// Card t_94e5cca3: the read-only phase-1 dormancy report store. Nil when
	// there is no database; the daily job and the admin surface then stay off.
	Dormancy *dbstore.DormancyStore

	// Issue #106: sale lookup by receipt MessageID, so the SNS endpoint can
	// correlate Open/Click/Delivered events to a specific sale. Nil in
	// bare-mode tests; events are then recorded without a sale_ref.
	Sales *dbstore.SaleStore

	// Issue #121: OAuth 2.1 authorization-code + PKCE store for MCP clients.
	// Nil in bare-mode tests; the OAuth endpoints are only registered when wired.
	OAuth *dbstore.OAuthStore

	// Issue #211: shared Prometheus metrics instance. When set, NewServer uses
	// it (so the DB query tracer and the HTTP middleware record into the same
	// registry); when nil, NewServer creates its own.
	Metrics *metrics.Metrics

	// Issue #212: auto-TLS at origin for DNS-only custom domains. Nil when
	// ACME is not configured (plain HTTP only). Handlers register verified
	// custom domains here so the origin provisions + renews their certs.
	TLS *tlsmgr.Manager

	// Issue #199: persistent background email queue + worker. EmailQueue is the
	// Valkey-backed queue that request handlers enqueue to; EmailWorker is the
	// goroutine that polls it and sends via SES. Both nil in bare-mode tests
	// (handlers then send synchronously).
	EmailQueue  *emailqueue.Queue
	EmailWorker *emailqueue.Worker
}

// VerifyMailer delivers an email-confirmation message. The concrete email.Sender
// already satisfies this (Send(to, subject, body, lang string) error); it exists
// so tests can inject a recorder without pulling in AWS credentials.
//
// lang is the language the message was RENDERED IN (language consistency): the
// sender declares it on the branded HTML part, so a mailer that ignored the
// parameter was how every email came to announce Malay. An empty or unknown
// code resolves to English.
type VerifyMailer interface {
	Send(to, subject, body, lang string) error
}

// Server wires dependencies into HTTP handlers.
type Server struct {
	cfg     *config.Config
	log     *slog.Logger
	deps    *Deps
	auth    *AuthHandlers
	oauth   *OAuthHandlers
	proxy   *middleware.TrustedProxy
	metrics *metrics.Metrics

	// routesOnce/routesMux hold the ONE route table this server serves (see
	// routes()). Handler() wraps exactly the mux routes() returns, and the
	// table is built once so the served table is a single object: the handler
	// Handler() returns is a stack of closures around the mux, which nothing
	// outside this package can see through, and the route-class guards
	// (unlock_ratelimit_class_test.go) enumerate the table that is actually
	// served rather than a hand-kept copy of it.
	routesOnce sync.Once
	routesMux  *http.ServeMux
}

// NewServer builds the HTTP API server. deps may be nil (health-only mode).
func NewServer(cfg *config.Config, log *slog.Logger, deps *Deps) *Server {
	if deps == nil {
		deps = &Deps{}
	}
	s := &Server{cfg: cfg, log: log, deps: deps, metrics: deps.Metrics}
	if s.metrics == nil {
		s.metrics = metrics.New()
	}
	setGlobalMetrics(s.metrics)
	isDev = cfg.IsDev()

	if deps.Users != nil && deps.Sessions != nil {
		s.auth = &AuthHandlers{
			Sessions:     deps.Sessions,
			Users:        deps.Users,
			Accounts:     deps.Accounts,
			Workspaces:   deps.REST.Workspaces,
			BaseURL:      cfg.AppBaseURL,
			CookieDomain: SharedCookieDomain(cfg.AppBaseURL),
			VerifyKey:    deps.VerifyKey,
			VerifyMailer: deps.VerifyMailer,
			// Self-hosted installs have no email sender; this is how their
			// first account can be confirmed. Off unless the operator set it.
			LogVerifyLink: cfg.LogVerifyLink,
			LoginLimiter:  auth.NewLoginLimiter(deps.Sessions.Client()),
			// Issue #56: SES reputation hardening.
			Suppressions: deps.Suppressions,
			Breaker:      deps.Breaker,
			MXChecker:    deps.MXChecker,
			// Issue #199: background email queue (nil → synchronous sends).
			EmailQueue: deps.EmailQueue,
			// Issue #161: redeem pending invites on signup.
			WorkspaceMembers: deps.WorkspaceMembers,
			// Issue #92: audit/behaviour log for auth actions.
			Events: deps.Events,
			// Issue #307: the user-level connected-agents (MCP) store.
			OAuth: deps.OAuth,
			// Card t_6ae7f586: server-held token that marks a QA test account at
			// signup (empty = channel closed, the prod default).
			QASignupToken: cfg.QASignupToken,
		}
		// Issue #121: OAuth 2.1 flow needs the same session + user identity for
		// the human authorize step, plus the store. Wired alongside auth.
		s.oauth = newOAuthHandlers(deps.OAuth, deps.Sessions, deps.Users, deps.Accounts, cfg.AppBaseURL, cfg)
		s.proxy = middleware.NewTrustedProxy()
		_ = s.proxy.FetchCloudflare(context.Background())
	}
	return s
}

// Handler returns the fully-wired root router.
func (s *Server) Handler() http.Handler {
	mux := s.routes()

	var h http.Handler = mux
	// Former workspace hosts (migration 000068): a request on a slug a workspace
	// has RENAMED AWAY from is answered with a permanent, method-preserving
	// redirect (HTTP 308, no-store) to its current host, so a printed QR stand or
	// a shared link never dies — and a page left open across the rename still
	// SUBMITS (a 301 would let a browser re-issue its POST as a GET).
	// Wrapped around the whole router (not per route), because a dozen public
	// handlers read the host independently — see alias_hosts.go. It is a no-op
	// for every host that is live, unknown, the apex, or a custom domain.
	if s.deps.Public != nil {
		h = s.deps.Public.AliasHostRedirect(h)
	}
	// Real client IP: strip trusted Cloudflare proxy hops first so logs,
	// rate limiting and audit rows see the true buyer IP.
	if s.proxy != nil {
		h = s.proxy.RealIP(h)
	}
	h = middleware.Secure(h)
	h = recoverer(h)
	h = middleware.Logger(s.log, s.metrics, h)
	h = middleware.RequestID(h)
	return h
}

// routes returns the ONE route table this server serves: every registration
// Server.Handler() dispatches from lives here, and Handler() is a stack of
// middleware wrapped around exactly this object.
//
// It is a separate method, and the mux is built once and kept on the receiver,
// because the handler Handler() returns is a tower of closure-based
// middlewares — `http.HandlerFunc` values with no exported field naming the
// handler underneath. A route guard that needs the table itself (the route
// CLASS guards: `unlock_ratelimit_class_test.go`) therefore cannot unwrap it
// by reflection without reaching into net/http's internals; before this seam
// existed such a guard kept its own hand-written copy of these registrations,
// and a route added to Handler() inline — the natural place for a one-line
// route addition — was outside the guard while it still reported green.
//
// So: registrations belong HERE, never inline in Handler() — and that is now
// enforced rather than documented. Handler() registers onto the very mux this
// method returns, so an inline registration joins the same object; but it only
// RUNS when Handler() is called. A caller that took routes() without ever
// calling Handler() therefore fired the memoising Once BEFORE those inline
// registrations ran and cached the pre-handler table. MEASURED (card
// t_48fd3de2): an unwrapped `.../unlock` route registered inline in Handler()
// was live and unlimited on the served chain (403 x12, no 429) and ABSENT from
// that walk (193 patterns, 3 unlock routes), while the class guard reported
// green; building Handler() first enumerated it (194 patterns, 4 unlock
// routes) and the guard named it RED.
//
// Both ends are pinned by tests, so neither can regress silently:
//   - TestServerHandlerRegistersNoRouteOutsideRoutes fails BY NAME if a
//     registration appears inline in Handler(), which is the shape above.
//   - TestUnlockClassGuardWalkTakesTheProductionPath requires the walk's own
//     server (servedMux) to build Handler() before the table is walked, so the
//     guard's subject stays the table production serves even if a registration
//     ever reaches Handler() through a spelling this file's scan cannot see.
//   - TestServerHandlerServesFromTheTableTheGuardWalks requires Handler() to
//     dispatch from the table this method returns.
func (s *Server) routes() *http.ServeMux {
	s.routesOnce.Do(func() {
		mux := http.NewServeMux()

		mux.HandleFunc("GET /healthz", s.handleHealth)
		mux.HandleFunc("GET /readyz", s.handleReady)
		mux.HandleFunc("GET /version", s.handleVersion)
		// Issue #142: serve the JomForm brand icon as the MCP host favicon so
		// Claude Desktop shows a real icon for the connector.
		mux.HandleFunc("GET /favicon.ico", s.handleFaviconICO)
		mux.HandleFunc("GET /icon.svg", s.handleIconSVG)
		mux.HandleFunc("GET /icon-192.png", s.handleIconPNG)
		// Issue #100: Prometheus scrape endpoint. Exposed on the same listener;
		// ops can restrict it at the edge (Cloudflare/nginx) if needed.
		mux.Handle("GET /metrics", s.metrics.Handler())

		if s.auth != nil {
			s.auth.RegisterRoutes(mux)
		}
		// Issue #56: SES bounce/complaint feedback endpoint. Registered whenever the
		// suppression store is wired AND SNS notifications are enabled via env — it
		// should not be exposed (even as a 401-trap) on deployments that don't
		// subscribe an SNS topic to it.
		if s.deps.Suppressions != nil && s.cfg.SNSNotifEnabled {
			mux.HandleFunc("POST /v1/internal/ses/notifications", s.handleSESNotification)
		}
		if s.deps.REST != nil {
			s.deps.REST.RegisterRoutes(mux)
		}
		if s.deps.Public != nil {
			s.deps.Public.RegisterRoutes(mux)
		}
		if s.deps.Payments != nil {
			s.deps.Payments.RegisterRoutes(mux)
		}
		if s.deps.MCP != nil {
			mux.Handle("POST /mcp", s.deps.MCP.HTTPHandler())
			// OAuth 2.1 discovery + dynamic client registration (RFC 8414/7591) +
			// authorization-code/PKCE flow (issue #121).
			mux.HandleFunc("/.well-known/oauth-authorization-server", wellKnownOAuth)
			mux.HandleFunc("/.well-known/oauth-authorization-server/mcp", wellKnownOAuth)
			// Issue #509: OAuth 2.0 Protected Resource Metadata (RFC 9728) is
			// MANDATORY for MCP servers, and it is how MCP clients discover the
			// authorization server. Served at the RFC's default location plus the
			// /mcp path-inserted form, because the protected resource is the /mcp
			// endpoint. Clients reach it from the /mcp 401 WWW-Authenticate
			// challenge (resource_metadata) as well.
			mux.HandleFunc(oauthmeta.ProtectedResourcePath, wellKnownProtectedResource)
			mux.HandleFunc(oauthmeta.ProtectedResourcePathSuffixed, wellKnownProtectedResource)
			if s.oauth != nil {
				// Defaults when a bare config (tests) left rate/burst at 0.
				rate, burst := s.cfg.OAuthDCRRate, s.cfg.OAuthDCRBurst
				if rate <= 0 {
					rate = 1.0 / 3600.0
				}
				if burst <= 0 {
					burst = 5
				}
				s.oauth.RegisterRoutes(mux, rate, burst)
			}
			mux.Handle("GET /mcp", s.deps.MCP.HTTPHandler())
			mux.Handle("DELETE /mcp", s.deps.MCP.HTTPHandler())
		}

		s.routesMux = mux
	})
	return s.routesMux
}

// ─── handlers ────────────────────────────────────────────────────────────────

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	// Issue #246: include a JomForm-specific marker so the custom-domain
	// verification probe (/healthz) can authoritatively distinguish a
	// JomForm-serving domain from a non-JomForm one.
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "jomform"})
}

// handleReady checks real dependencies (DB + Valkey) with short timeouts.
// A dependency that is not wired counts as degraded, not a crash.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	checks := map[string]string{"database": "not wired", "valkey": "not wired"}
	ok := true
	if err := s.checkDatabase(r.Context()); err != nil {
		checks["database"] = "unavailable"
	} else {
		checks["database"] = "ok"
	}
	if err := s.checkValkey(r.Context()); err != nil {
		checks["valkey"] = "unavailable"
	} else {
		checks["valkey"] = "ok"
	}
	for _, v := range checks {
		if v != "ok" {
			ok = false
			break
		}
	}
	writeJSON(w, statusFor(ok), map[string]any{"status": boolString(ok), "checks": checks})
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"version": version.Version,
		"commit":  version.Commit,
		"date":    version.Date,
	})
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic_recovered",
					"err", rec,
					"path", r.URL.Path,
					"request_id", middleware.GetRequestID(r.Context()),
					"account_id", middleware.GetAccountID(r.Context()),
					"error_class", "panic",
				)
				writeJSON(w, http.StatusInternalServerError, errorBody(http.StatusInternalServerError, "internal error"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr answers one failure with BOTH the human-readable wire message and a
// stable machine code (see errcode.go):
//
//	writeErr(w, 403, "workspace tidak dijumpai")
//	→ {"error":"workspace tidak dijumpai","code":"workspace.notFound"}
//
// The message stays because API-key callers, curl/MCP clients and the public
// buyer-form JavaScript consume it directly and cannot look up an i18n key. The
// dashboard reads `code` and renders its own message in the user's language, so
// it never prints this prose (issue #6177).
func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, errorBody(code, msg))
}

// errorBody is the one error envelope shape. Kept separate from writeErr so a
// handler that must answer a richer body (e.g. CHIP's {error, verified}) can
// still emit the same `error` + `code` pair.
func errorBody(status int, msg string) map[string]string {
	return map[string]string{"error": msg, "code": codeFor(status, msg)}
}

func statusFor(ok bool) int {
	if ok {
		return http.StatusOK
	}
	return http.StatusServiceUnavailable
}

func boolString(b bool) string {
	if b {
		return "ok"
	}
	return "degraded"
}
