package middleware

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Issue #615: the logging middleware embedded http.ResponseWriter, which
// forwarded Write/WriteHeader but DROPPED the optional interfaces the real
// writer implements. net/http's type assertions then failed on the wrapper, so
// the SSE endpoint (/v1/rest/events/stream) answered 500 on every request and
// realtime notifications never worked in production.
//
// The fix is deliberately NOT a forwarding Flush() method: defining one would
// make the wrapper always satisfy http.Flusher, so a caller could not tell it
// from a real flusher. The wrapper exposes only Unwrap(), and callers reach the
// real writer through http.ResponseController. These tests pin that contract
// from both directions — a writer that CAN flush must be reachable through the
// wrapper, and one that cannot must be reported as such rather than silently
// swallowed.

// flusherRecorder supports flushing (like net/http's real *http.response) and
// records whether a flush actually reached it.
type flusherRecorder struct {
	*httptest.ResponseRecorder
	flushed bool
}

func (f *flusherRecorder) Flush() { f.flushed = true }

// hijackRecorder additionally supports Hijack (websocket-style upgrades).
type hijackRecorder struct {
	*httptest.ResponseRecorder
	hijacked bool
}

func (h *hijackRecorder) Flush() { h.ResponseRecorder.Flush() }
func (h *hijackRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h.hijacked = true
	return nil, nil, nil
}

// plainWriter implements ONLY the base http.ResponseWriter — no Flush, no
// Hijack. It stands in for a writer that genuinely cannot stream.
type plainWriter struct {
	header http.Header
	code   int
}

func (p *plainWriter) Header() http.Header {
	if p.header == nil {
		p.header = http.Header{}
	}
	return p.header
}
func (p *plainWriter) Write(b []byte) (int, error) { return len(b), nil }
func (p *plainWriter) WriteHeader(code int)        { p.code = code }

// wrap replicates exactly what Logger() hands to the handler.
func wrap(w http.ResponseWriter) http.ResponseWriter {
	return &statusRecorderMiddleware{ResponseWriter: w, status: http.StatusOK}
}

func TestResponseControllerReachesTheRealFlusher(t *testing.T) {
	under := &flusherRecorder{ResponseRecorder: httptest.NewRecorder()}
	w := wrap(under)

	if err := http.NewResponseController(w).Flush(); err != nil {
		t.Fatalf("ResponseController.Flush() = %v, want nil — the wrapper hides the real "+
			"writer's Flush, which is issue #615: the SSE stream answers 500 and realtime "+
			"notifications never reach the dashboard", err)
	}
	if !under.flushed {
		t.Fatal("Flush() never reached the underlying writer")
	}
}

func TestResponseControllerReportsWhenNothingCanFlush(t *testing.T) {
	under := &plainWriter{header: http.Header{}}
	w := wrap(under)

	err := http.NewResponseController(w).Flush()
	if err == nil {
		t.Fatal("ResponseController.Flush() reported success although nothing in the chain can " +
			"flush — the handler would believe streaming works and silently never stream")
	}
	if !errors.Is(err, http.ErrNotSupported) {
		t.Fatalf("ResponseController.Flush() error = %v, want http.ErrNotSupported", err)
	}
}

func TestWrapperDoesNotClaimOptionalInterfaces(t *testing.T) {
	// The wrapper must NOT satisfy Flusher/Hijacker by itself, or a caller's
	// type assertion could not distinguish it from a real streaming writer.
	w := wrap(&plainWriter{header: http.Header{}})

	if _, ok := w.(http.Flusher); ok {
		t.Fatal("the wrapper advertises http.Flusher although the writer underneath cannot " +
			"flush — a caller's assertion would pass and then silently not stream")
	}
	if _, ok := w.(http.Hijacker); ok {
		t.Fatal("the wrapper advertises http.Hijacker although the writer underneath cannot hijack")
	}
}

func TestResponseControllerReachesTheRealHijacker(t *testing.T) {
	under := &hijackRecorder{ResponseRecorder: httptest.NewRecorder()}
	w := wrap(under)

	conn, _, err := http.NewResponseController(w).Hijack()
	if err != nil {
		t.Fatalf("ResponseController.Hijack() = %v, want nil", err)
	}
	_ = conn
	if !under.hijacked {
		t.Fatal("Hijack() never reached the underlying writer")
	}
}

func TestLoggerStillRecordsStatus(t *testing.T) {
	rec := &statusRecorderMiddleware{ResponseWriter: httptest.NewRecorder(), status: http.StatusOK}
	rec.WriteHeader(http.StatusTeapot)
	if rec.status != http.StatusTeapot {
		t.Fatalf("status = %d, want %d — the logging/metrics behaviour must survive the fix",
			rec.status, http.StatusTeapot)
	}
}
