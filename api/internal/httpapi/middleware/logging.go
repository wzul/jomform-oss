package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/wzul/jomform/api/internal/metrics"
)

// statusRecorderMiddleware records the response status for logging/metrics while
// passing the response through to the real writer.
//
// THE BUG IT FIXES (issue #615): this type embeds http.ResponseWriter, which
// forwards Write/WriteHeader — but an embedded INTERFACE only promotes the
// methods that interface declares, so the wrapper structurally DROPS the
// optional interfaces the concrete writer implements. net/http's type
// assertions then fail on the wrapper:
//
//	flusher, ok := w.(http.Flusher)   // api/internal/httpapi/realtime.go, pre-fix
//
// The SSE endpoint (/v1/rest/events/stream, issue #447) answered 500 on every
// authenticated request because of exactly that, and it had never worked in
// production.
//
// WHY THERE IS NO Flush() METHOD HERE — this is the fix, and it is deliberately
// the opposite of the obvious one. Adding a forwarding Flush() does not work,
// because a Go method cannot be conditionally absent: defining Flush() makes
// this type ALWAYS satisfy http.Flusher, whatever the writer underneath can do.
// A caller's `w.(http.Flusher)` then succeeds against a wrapper that may flush
// nothing, and http.ResponseController.Flush() finds this method first and
// reports success without ever reaching the real writer — turning a loud
// failure into a silent one.
//
// So this type exposes only Unwrap(). That leaves the optional interfaces absent
// from the wrapper's method set, and http.ResponseController — the supported way
// to reach optional behaviour through wrapper layers (Go 1.20+) — walks past
// this layer to the real writer and answers honestly:
//
//	controller.Flush()  ->  nil              when the real writer can flush
//	                    ->  ErrNotSupported  when nothing in the chain can
//
// One Unwrap() covers Flush, Hijack and Push with no per-interface forwarding
// and no false advertising. Handlers must use http.NewResponseController(w)
// rather than a bare type assertion — see realtime.go.
func (r *statusRecorderMiddleware) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

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
