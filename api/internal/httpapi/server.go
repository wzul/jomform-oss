// Package httpapi contains HTTP handlers and router wiring for jomform-api.
package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/wzul/jomform/api/internal/config"
	"github.com/wzul/jomform/api/internal/httpapi/middleware"
	"github.com/wzul/jomform/api/internal/version"
)

// Server wires dependencies into HTTP handlers.
type Server struct {
	cfg *config.Config
	log *slog.Logger
}

// NewServer builds the HTTP API server.
func NewServer(cfg *config.Config, log *slog.Logger) *Server {
	return &Server{cfg: cfg, log: log}
}

// Handler returns the fully-wired root router.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /readyz", s.handleReady)
	mux.HandleFunc("GET /version", s.handleVersion)

	var h http.Handler = mux
	h = recoverer(h)
	h = middleware.Logger(s.log, h)
	h = middleware.RequestID(h)
	return h
}

// --- handlers ---

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleReady reports dependency readiness. DB/Valkey/S3 pings land in the
// next milestone when their clients are wired; for now it reports ok and the
// configured environment.
func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"checks": map[string]string{
			"database": "deferred_to_m2",
			"valkey":   "deferred_to_m2",
			"s3":       "deferred_to_m2",
		},
	})
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"version": version.Version,
		"commit":  version.Commit,
		"date":    version.Date,
	})
}

// --- helpers ---

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