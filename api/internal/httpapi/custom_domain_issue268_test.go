package httpapi

// Issue #268: the verify probe must skip TLS cert verification so a custom
// domain can verify even when its Let's Encrypt cert isn't yet trusted/issued.
// These are pure unit tests (no Postgres/Valkey needed). httptest.NewTLSServer
// serves a self-signed (untrusted) cert, which a cert-verifying client would
// reject with "unknown CA" — exactly the production failure this fixes.

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbeServesJomFormSkipsTLSCertVerification(t *testing.T) {
	// A TLS server with an untrusted (self-signed) cert that serves a real
	// JomForm page. A cert-verifying client would fail the handshake; the
	// insecure probe must reach it and verify based on content.
	jomform := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","service":"jomform"}`))
	}))
	defer jomform.Close()

	// A TLS server with an untrusted cert that serves non-JomForm content —
	// must NOT verify even though the probe can now reach it.
	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><title>Wanzul Hosting</title></html>"))
	}))
	defer other.Close()

	cases := []struct {
		name string
		url  string
		want bool
	}{
		{"untrusted cert serving JomForm IS verified", jomform.URL + "/healthz", true},
		{"untrusted cert serving non-JomForm is NOT verified", other.URL + "/healthz", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// SSRF dial is nil here (loopback httptest server); the guard lives
			// in the production entrypoint probeServesJomForm.
			if got := probeServesJomFormURL(c.url, nil); got != c.want {
				t.Fatalf("probeServesJomFormURL(%q) = %v, want %v", c.url, got, c.want)
			}
		})
	}
}
