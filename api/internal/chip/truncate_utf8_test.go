package chip

// The cap that bounds an upstream body before it is rendered into an
// operator-facing error message must cut on a RUNE boundary. A byte slice lands
// mid-rune on any multi-byte UTF-8 body (a localised CHIP message, a non-ASCII
// product or domain name), so the operator reads a broken character where the
// provider's own text should be — in the very message they use to diagnose the
// upstream failure.
//
// These tests feed a body whose cap offset falls inside a multi-byte rune and
// assert on the RENDERED error string, which is what a human sees: it must be
// valid UTF-8, still bounded, and it must still carry the rune that straddles
// the cap (a fix that merely dropped the tail would lose the provider's text).

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
)

// straddleRuneChip is 3 bytes wide (E4 B8 AD), so a cap offset one byte into it
// produces invalid UTF-8 under a byte slice.
const straddleRuneChip = "中"

// bodyStraddlingChip returns a body of capBytes-1 ASCII bytes, the multi-byte
// rune, then tail ASCII bytes: a byte slice [:capBytes] cuts the rune in half.
func bodyStraddlingChip(capBytes, tail int) string {
	return strings.Repeat("x", capBytes-1) + straddleRuneChip + strings.Repeat("y", tail)
}

// assertRenderedValidAndBounded asserts the properties of the message an
// operator reads: valid UTF-8, the expected prefix, the body capped at
// capRunes CHARACTERS, and the straddling rune present in full.
func assertRenderedValidAndBounded(t *testing.T, msg, prefix string, capRunes int) {
	t.Helper()
	if !utf8.ValidString(msg) {
		t.Fatalf("rendered error is not valid UTF-8 — the operator reads a broken rune: %q", msg)
	}
	if !strings.HasPrefix(msg, prefix) {
		t.Fatalf("rendered error = %q, want it to start with %q", msg, prefix)
	}
	body := strings.TrimPrefix(msg, prefix)
	if got := utf8.RuneCountInString(body); got > capRunes {
		t.Errorf("body part is %d characters, want it capped at %d", got, capRunes)
	}
	if !strings.Contains(body, straddleRuneChip) {
		t.Errorf("body part %q lost the rune that straddles the cap", body)
	}
}

// TestChipHTTPErrorBodyIsValidUTF8 drives the real request path
// (Client.do → "http %d: %s" with the truncated body) against a server whose
// 500 body has byte 512 inside a multi-byte rune.
func TestChipHTTPErrorBodyIsValidUTF8(t *testing.T) {
	const capBytes = 512
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, bodyStraddlingChip(capBytes, 40))
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.CreatePurchase(context.Background(), "sk_test", PurchaseRequest{BrandID: "brand-1"})
	if err == nil {
		t.Fatal("expected an error from the 500 response")
	}
	// apiError.Error() prefixes "chip: ", so the operator reads
	// "chip: http 500: <body>".
	assertRenderedValidAndBounded(t, err.Error(), "chip: http 500: ", capBytes)
}

// TestChipPublicKeyErrorBodyIsValidUTF8 drives the second call site
// (PublicKey → "unrecognised public key response: %s"), the one an operator
// reads when CHIP answers with something the client cannot recognise at all.
//
// The body must be well-formed JSON to get past Client.do's decode (and then be
// neither a bare JSON string nor an object carrying `public_key`), so it is a
// JSON array holding a string whose byte 200 falls inside the multi-byte rune.
func TestChipPublicKeyErrorBodyIsValidUTF8(t *testing.T) {
	const capBytes = 200
	// `["` = 2 bytes, so 197 ASCII bytes put the rune at bytes 199-201.
	body := `["` + strings.Repeat("x", 197) + straddleRuneChip + strings.Repeat("y", 40) + `"]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.PublicKey(context.Background(), "sk_test")
	if err == nil {
		t.Fatal("expected an error for a body that is neither a JSON string nor an object")
	}
	assertRenderedValidAndBounded(t, err.Error(), "chip: unrecognised public key response: ", capBytes)
}
