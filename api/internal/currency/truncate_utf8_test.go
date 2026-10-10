package currency

// The BNM error path renders a bounded copy of the upstream body into
// "bnm http %d: %s". The bound must cut on a RUNE boundary: a byte slice lands
// mid-rune on a multi-byte body (a localised BNM message), so the operator reads
// a broken character where the provider's own text should be.
//
// This asserts on the RENDERED error string — valid UTF-8, still bounded, and
// still carrying the rune that straddles the cap — because that string is what
// a human diagnoses from.

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"
)

// bnmStraddleRune is 3 bytes wide, so a cap offset one byte into it produces
// invalid UTF-8 under a byte slice.
const bnmStraddleRune = "中"

// roundTripFunc lets a test answer any request with a canned response, so the
// BNM URL constant stays untouched.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBNMHTTPErrorBodyIsValidUTF8(t *testing.T) {
	const capBytes = 256
	body := strings.Repeat("x", capBytes-1) + bnmStraddleRune + strings.Repeat("y", 40)

	s := &Store{http: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Header:     http.Header{"Content-Type": []string{"text/plain; charset=utf-8"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})}}

	_, err := s.FetchRates(context.Background())
	if err == nil {
		t.Fatal("expected an error from the 500 response")
	}
	msg := err.Error()
	if !utf8.ValidString(msg) {
		t.Fatalf("rendered error is not valid UTF-8 — the operator reads a broken rune: %q", msg)
	}
	const prefix = "bnm http 500: "
	if !strings.HasPrefix(msg, prefix) {
		t.Fatalf("rendered error = %q, want it to start with %q", msg, prefix)
	}
	got := strings.TrimPrefix(msg, prefix)
	if n := utf8.RuneCountInString(got); n > capBytes {
		t.Errorf("body part is %d characters, want it capped at %d", n, capBytes)
	}
	if !strings.Contains(got, bnmStraddleRune) {
		t.Errorf("body part %q lost the rune that straddles the cap", got)
	}
}
