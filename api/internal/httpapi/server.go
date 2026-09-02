package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/config"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/email"
	"github.com/wzul/jomform/api/internal/httpapi/middleware"
	"github.com/wzul/jomform/api/internal/mcpsrv"
	"github.com/wzul/jomform/api/internal/metrics"
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

	// Issue #106: sale lookup by receipt MessageID, so the SNS endpoint can
	// correlate Open/Click/Delivered events to a specific sale. Nil in
	// bare-mode tests; events are then recorded without a sale_ref.
	Sales *dbstore.SaleStore
}

// VerifyMailer delivers an email-confirmation message. The concrete email.Sender
// already satisfies this (Send(to, subject, body error)); it exists so tests can
// inject a recorder without pulling in AWS credentials.
type VerifyMailer interface {
	Send(to, subject, body string) error
}

// Server wires dependencies into HTTP handlers.
type Server struct {
	cfg     *config.Config
	log     *slog.Logger
	deps    *Deps
	auth    *AuthHandlers
	proxy   *middleware.TrustedProxy
	metrics *metrics.Metrics
}

// NewServer builds the HTTP API server. deps may be nil (health-only mode).
func NewServer(cfg *config.Config, log *slog.Logger, deps *Deps) *Server {
	if deps == nil {
		deps = &Deps{}
	}
	s := &Server{cfg: cfg, log: log, deps: deps, metrics: metrics.New()}
	setGlobalMetrics(s.metrics)
	isDev = cfg.IsDev()

	if deps.Users != nil && deps.Sessions != nil {
		s.auth = &AuthHandlers{
			Sessions:     deps.Sessions,
			Users:        deps.Users,
			Accounts:     deps.Accounts,
			BaseURL:      cfg.AppBaseURL,
			VerifyKey:    deps.VerifyKey,
			VerifyMailer: deps.VerifyMailer,
			LoginLimiter: auth.NewLoginLimiter(deps.Sessions.Client()),
			// Issue #56: SES reputation hardening.
			Suppressions: deps.Suppressions,
			Breaker:      deps.Breaker,
			MXChecker:    deps.MXChecker,
			// Issue #92: audit/behaviour log for auth actions.
			Events: deps.Events,
		}
		s.proxy = middleware.NewTrustedProxy()
		_ = s.proxy.FetchCloudflare(context.Background())
	}
	return s
}

// Handler returns the fully-wired root router.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /readyz", s.handleReady)
	mux.HandleFunc("GET /version", s.handleVersion)
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
		// OAuth 2.1 discovery + dynamic client registration (RFC 8414/7591).
		mux.HandleFunc("/.well-known/oauth-authorization-server", wellKnownOAuth)
		mux.HandleFunc("/.well-known/oauth-authorization-server/mcp", wellKnownOAuth)
		mux.HandleFunc("POST /oauth/register", oauthRegister)
		mux.Handle("GET /mcp", s.deps.MCP.HTTPHandler())
		mux.Handle("DELETE /mcp", s.deps.MCP.HTTPHandler())
	}

	var h http.Handler = mux
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

// ─── handlers ────────────────────────────────────────────────────────────────

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
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

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
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
