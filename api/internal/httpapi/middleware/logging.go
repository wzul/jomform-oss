package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/wzul/jomform/api/internal/metrics"
)

type statusRecorderMiddleware struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorderMiddleware) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Logger logs each request: method, path, status, duration, request ID, and
// (when present) account ID. It also records Prometheus request rate/latency
// metrics by route group + status. m may be nil (no-op).
func Logger(log *slog.Logger, m *metrics.Metrics, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorderMiddleware{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		attrs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", GetRequestID(r.Context()),
		}
		if acct := GetAccountID(r.Context()); acct != "" {
			attrs = append(attrs, "account_id", acct)
		}
		log.Info("http_request", attrs...)
		if m != nil {
			m.ObserveRequest(metrics.RouteLabel(r.URL.Path), r.Method, rec.status, time.Since(start).Seconds())
		}
	})
}
