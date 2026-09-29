package httpapi

import (
	"errors"
	"fmt"
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
		// The workspace sentinel belongs in this family: it is a not-found, and
		// card t_a71ee62b is about a route that answered 500 because the RAW
		// driver miss (pgx.ErrNoRows / SQLSTATE 22P02) never became it. Any path
		// that lets this sentinel reach respondErr — listWorkspaces, the
		// settings reads on a stale id — must answer 404, not the default 500.
		{dbstore.ErrWorkspaceNotFound, http.StatusNotFound, "tidak dijumpai"},
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

// TestWorkspaceNotFoundIsClassedNotFound pins the metric/log bucket, which is
// the half of card t_a71ee62b's acceptance that a status assertion cannot see.
// The card requires error_class=not_found, not the internal class the defect
// produced: errorClass is what both the structured log field and the Prometheus
// error counter are labelled with, so a sentinel that is not named here keeps
// counting a caller's stale id as a server fault — and every not-found test
// would still pass, because they assert the STATUS.
//
// The two raw miss shapes of the same defect are asserted alongside it: the
// class of the error that actually reaches this function must be not_found only
// once the store has mapped it, which is why the store half is pinned in
// dbstore/workspace_delete_test.go.
func TestWorkspaceNotFoundIsClassedNotFound(t *testing.T) {
	if got := errorClass(dbstore.ErrWorkspaceNotFound); got != "not_found" {
		t.Errorf("errorClass(ErrWorkspaceNotFound) = %q, want not_found — a workspace that does "+
			"not exist is a client-side miss, and this label is the error_class log field and "+
			"the jomform_errors_total bucket", got)
	}
	// The sentinel must still be RECOGNISED through wrapping, so a store that
	// returns fmt.Errorf(\"...: %w\", ErrWorkspaceNotFound) keeps its class.
	if got := errorClass(fmt.Errorf("lookup workspace: %w", dbstore.ErrWorkspaceNotFound)); got != "not_found" {
		t.Errorf("errorClass(wrapped ErrWorkspaceNotFound) = %q, want not_found", got)
	}
	// And the classification must not have been bought by swallowing everything:
	// a real internal failure stays internal.
	if got := errorClass(errors.New("pq: relation \"secrets\" does not exist")); got != "internal" {
		t.Errorf("errorClass(generic failure) = %q, want internal", got)
	}
}
