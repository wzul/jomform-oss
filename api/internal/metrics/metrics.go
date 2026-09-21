// Package metrics exposes Prometheus instrumentation for the JomForm API.
//
// It is a leaf package (no imports from httpapi/middleware) so both the HTTP
// middleware and the REST/auth/payment handlers can record counters and
// histograms without import cycles. All functions are nil-safe: a nil
// *Metrics is a no-op, which keeps bare-mode tests and health-only servers
// working without wiring a registry.
package metrics

import (
	"net/http"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics bundles the Prometheus collectors for the API.
type Metrics struct {
	registry *prometheus.Registry

	// HTTP request rate + latency, labelled by route group, method, status.
	httpRequestsTotal *prometheus.CounterVec
	httpDuration      *prometheus.HistogramVec

	// Internal error rate, labelled by error class (see httpapi errorClass).
	errorsTotal *prometheus.CounterVec

	// Auth failures, labelled by reason (bad_credentials, unverified, ...).
	authFailuresTotal *prometheus.CounterVec

	// Payment failures, labelled by operation (capture, release, callback, ...).
	paymentFailuresTotal *prometheus.CounterVec

	// Upload failures, labelled by reason (too_large, bad_type, store, ...).
	uploadFailuresTotal *prometheus.CounterVec

	// SES send + suppression counts.
	sesSendsTotal        *prometheus.CounterVec
	sesSuppressionsTotal *prometheus.CounterVec

	// DB query timing (issue #211): per-query duration histogram + slow-query
	// counter, labelled by query class (normalised SQL operation).
	dbQueryDuration    *prometheus.HistogramVec
	dbSlowQueriesTotal *prometheus.CounterVec
}

// New builds a Metrics with all collectors registered on a fresh registry.
func New() *Metrics {
	reg := prometheus.NewRegistry()
	m := &Metrics{registry: reg}

	m.httpRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "jomform_http_requests_total",
		Help: "Total HTTP requests by route group, method and status code.",
	}, []string{"route", "method", "status"})

	m.httpDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "jomform_http_request_duration_seconds",
		Help:    "HTTP request latency in seconds by route group and method.",
		Buckets: prometheus.DefBuckets,
	}, []string{"route", "method"})

	m.errorsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "jomform_errors_total",
		Help: "Internal errors by error class.",
	}, []string{"error_class"})

	m.authFailuresTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "jomform_auth_failures_total",
		Help: "Authentication failures by reason.",
	}, []string{"reason"})

	m.paymentFailuresTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "jomform_payment_failures_total",
		Help: "Payment operation failures by operation.",
	}, []string{"operation"})

	m.uploadFailuresTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "jomform_upload_failures_total",
		Help: "Image upload failures by reason.",
	}, []string{"reason"})

	m.sesSendsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "jomform_ses_sends_total",
		Help: "SES email sends by kind (verification, reset, recovery, receipt).",
	}, []string{"kind"})

	m.sesSuppressionsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "jomform_ses_suppressions_total",
		Help: "SES sends skipped due to suppression by reason.",
	}, []string{"reason"})

	// Issue #211: DB query timing. The histogram uses a wider bucket set than
	// the HTTP latency default so slow queries (100ms–10s) are well resolved.
	m.dbQueryDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "jomform_db_query_duration_seconds",
		Help:    "Database query duration in seconds by query class.",
		Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.2, 0.5, 1, 2.5, 5, 10},
	}, []string{"query_class"})

	m.dbSlowQueriesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "jomform_db_slow_queries_total",
		Help: "Database queries exceeding the slow-query threshold by query class.",
	}, []string{"query_class"})

	reg.MustRegister(
		m.httpRequestsTotal, m.httpDuration, m.errorsTotal,
		m.authFailuresTotal, m.paymentFailuresTotal, m.uploadFailuresTotal,
		m.sesSendsTotal, m.sesSuppressionsTotal,
		m.dbQueryDuration, m.dbSlowQueriesTotal,
	)
	return m
}

// Handler returns the Prometheus scrape handler for /metrics.
func (m *Metrics) Handler() http.Handler {
	if m == nil || m.registry == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("metrics not configured\n"))
		})
	}
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// RouteLabel normalises a request path into a bounded route-group label so
// Prometheus cardinality stays low even for dynamic segments like
// /v1/rest/forms/{id}. It keeps the first three segments.
func RouteLabel(path string) string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return "/"
	}
	segs := strings.Split(trimmed, "/")
	if len(segs) > 3 {
		segs = segs[:3]
	}
	return "/" + strings.Join(segs, "/")
}

// ObserveRequest records request rate + latency for a completed request.
func (m *Metrics) ObserveRequest(route, method string, status int, seconds float64) {
	if m == nil || m.httpRequestsTotal == nil {
		return
	}
	m.httpRequestsTotal.WithLabelValues(route, method, statusCodeLabel(status)).Inc()
	m.httpDuration.WithLabelValues(route, method).Observe(seconds)
}

// IncError records an internal error by class.
func (m *Metrics) IncError(class string) {
	if m == nil || m.errorsTotal == nil {
		return
	}
	m.errorsTotal.WithLabelValues(class).Inc()
}

// IncAuthFailure records an authentication failure by reason.
func (m *Metrics) IncAuthFailure(reason string) {
	if m == nil || m.authFailuresTotal == nil {
		return
	}
	m.authFailuresTotal.WithLabelValues(reason).Inc()
}

// IncPaymentFailure records a payment failure by operation.
func (m *Metrics) IncPaymentFailure(operation string) {
	if m == nil || m.paymentFailuresTotal == nil {
		return
	}
	m.paymentFailuresTotal.WithLabelValues(operation).Inc()
}

// IncUploadFailure records an upload failure by reason.
func (m *Metrics) IncUploadFailure(reason string) {
	if m == nil || m.uploadFailuresTotal == nil {
		return
	}
	m.uploadFailuresTotal.WithLabelValues(reason).Inc()
}

// IncSESSend records a successful SES send by kind.
func (m *Metrics) IncSESSend(kind string) {
	if m == nil || m.sesSendsTotal == nil {
		return
	}
	m.sesSendsTotal.WithLabelValues(kind).Inc()
}

// IncSESSuppression records a send skipped due to suppression by reason.
func (m *Metrics) IncSESSuppression(reason string) {
	if m == nil || m.sesSuppressionsTotal == nil {
		return
	}
	m.sesSuppressionsTotal.WithLabelValues(reason).Inc()
}

// ObserveDBQuery records a completed database query's duration by query class.
func (m *Metrics) ObserveDBQuery(queryClass string, seconds float64) {
	if m == nil || m.dbQueryDuration == nil {
		return
	}
	m.dbQueryDuration.WithLabelValues(queryClass).Observe(seconds)
}

// IncDBSlowQuery records a database query that exceeded the slow-query
// threshold, by query class.
func (m *Metrics) IncDBSlowQuery(queryClass string) {
	if m == nil || m.dbSlowQueriesTotal == nil {
		return
	}
	m.dbSlowQueriesTotal.WithLabelValues(queryClass).Inc()
}

func statusCodeLabel(code int) string {
	return http.StatusText(code)
}
