package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/config"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/httpapi/middleware"
	"github.com/wzul/jomform/api/internal/mcpsrv"
	"github.com/wzul/jomform/api/internal/version"
)

// Deps carries wired-in infrastructure (nil in bare-mode tests).
type Deps struct {
	Payments *PaymentsHandlers
	MCP      *mcpsrv.Server
	Users    *dbstore.UserStore
	Sessions *auth.Sessions
}

// Server wires dependencies into HTTP handlers.
type Server struct {
	cfg  *config.Config
	log  *slog.Logger
	deps *Deps
	auth *AuthHandlers
}

// NewServer builds the HTTP API server. deps may be nil (health-only mode).
func NewServer(cfg *config.Config, log *slog.Logger, deps *Deps) *Server {
	if deps == nil {
		deps = &Deps{}
	}
	s := &Server{cfg: cfg, log: log, deps: deps}
	isDev = cfg.IsDev()

	if deps.Users != nil && deps.Sessions != nil {
		s.auth = &AuthHandlers{Sessions: deps.Sessions, Users: deps.Users}
	}
	return s
}

// Handler returns the fully-wired root router.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /readyz", s.handleReady)
	mux.HandleFunc("GET /version", s.handleVersion)

	if s.auth != nil {
		s.auth.RegisterRoutes(mux)
	}
	if s.deps.Payments != nil {
		s.deps.Payments.RegisterRoutes(mux)
	}
	if s.deps.MCP != nil {
		mux.Handle("POST /mcp", s.deps.MCP.HTTPHandler())
		mux.Handle("GET /mcp", s.deps.MCP.HTTPHandler())
		mux.Handle("DELETE /mcp", s.deps.MCP.HTTPHandler())
	}

	var h http.Handler = mux
	h = recoverer(h)
	h = middleware.Logger(s.log, h)
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
				slog.Error("panic_recovered", "err", rec, "path", r.URL.Path)
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
