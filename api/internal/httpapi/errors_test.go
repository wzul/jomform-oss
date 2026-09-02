package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/payments"
)

// TestMetricsEndpoint verifies /metrics is exposed and serves Prometheus text.
func TestMetricsEndpoint(t *testing.T) {
	srv := newTestServer()
	// Populate counters so the scrape output is non-empty (Prometheus omits
	// zero-value counters until first observed).
	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	req = httptest.NewRequest("GET", "/metrics", nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	// Populate the remaining counters so every collector appears in the
	// scrape output (Prometheus omits zero-value counters until observed).
	recordAuthFailure("bad_credentials")
	recordPaymentFailure("capture")
	recordUploadFailure("store")
	recordSESSend("verify")
	recordSESSuppression("verify")
	recordError("internal")
	req = httptest.NewRequest("GET", "/metrics", nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"jomform_http_requests_total",
		"jomform_http_request_duration_seconds",
		"jomform_errors_total",
		"jomform_auth_failures_total",
		"jomform_payment_failures_total",
		"jomform_upload_failures_total",
		"jomform_ses_sends_total",
		"jomform_ses_suppressions_total",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics body missing %q", want)
		}
	}
}

// TestMetricsObserveRequest verifies a request through the pipeline records a
// counter + histogram entry.
func TestMetricsObserveRequest(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	mreq := httptest.NewRequest("GET", "/metrics", nil)
	mrec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(mrec, mreq)
	body := mrec.Body.String()
	if !strings.Contains(body, `jomform_http_requests_total{method="GET",route="/healthz",status="OK"} 1`) {
		t.Errorf("expected healthz request counter, got:\n%s", body)
	}
}

// TestRespondErrMasksInternal verifies internal errors never leak internals to
// the client but are logged with structured detail.
func TestRespondErrMasksInternal(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/rest/forms", nil)
	rec := httptest.NewRecorder()

	// An internal error (SQL-ish message) must be masked to a generic 500.
	respondErr(rec, req, errors.New("pq: relation \"secrets\" does not exist"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "pq:") || strings.Contains(body, "secrets") {
		t.Errorf("internal error leaked to client: %q", body)
	}
	if !strings.Contains(body, "internal error") {
		t.Errorf("expected generic message, got %q", body)
	}
}

// TestRespondErrUserActionable verifies user-actionable errors map to clear,
// localized messages with the right status codes.
func TestRespondErrUserActionable(t *testing.T) {
	cases := []struct {
		err      error
		wantCode int
		wantMsg  string
	}{
		{dbstore.ErrFormNotFound, http.StatusNotFound, "tidak dijumpai"},
		{dbstore.ErrProductNotFound, http.StatusNotFound, "tidak dijumpai"},
		{dbstore.ErrAPIKeyInvalid, http.StatusNotFound, "tidak dijumpai"},
		{payments.ErrNotFound, http.StatusNotFound, "tidak dijumpai"},
		{dbstore.ErrEmailTaken, http.StatusConflict, "emel sudah didaftarkan"},
		{dbstore.ErrOutOfStock, http.StatusConflict, "produk telah habis (out of stock)"},
		{payments.ErrNotConfigured, http.StatusBadRequest, "chip belum dikonfigurasi akaun ini"},
	}
	for _, c := range cases {
		req := httptest.NewRequest("GET", "/v1/rest/forms", nil)
		rec := httptest.NewRecorder()
		respondErr(rec, req, c.err)
		if rec.Code != c.wantCode {
			t.Errorf("%v: status = %d, want %d", c.err, rec.Code, c.wantCode)
		}
		if !strings.Contains(rec.Body.String(), c.wantMsg) {
			t.Errorf("%v: body %q missing %q", c.err, rec.Body.String(), c.wantMsg)
		}
	}
}
