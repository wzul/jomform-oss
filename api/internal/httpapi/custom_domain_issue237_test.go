package httpapi

// Issue #237: custom-domain verification must require the domain to actually
// serve a JomForm page. Sharing Cloudflare edge IPs with jomform.com is NOT
// sufficient — a domain behind Cloudflare that serves non-JomForm content must
// NOT be verified. These are pure unit tests (no Postgres/Valkey needed).

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbeServesJomForm(t *testing.T) {
	// A server that serves a real JomForm page (200 + body contains "JomForm").
	jomform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><title>JomForm</title></html>"))
	}))
	defer jomform.Close()

	// A server that serves a non-JomForm page (e.g. a Wanzul Hosting page) —
	// even though it may be behind the same Cloudflare edge IPs as jomform.com.
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><title>Wanzul Hosting</title></html>"))
	}))
	defer other.Close()

	// A server that returns an error status (e.g. 404) — not JomForm.
	notFound := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("not found"))
	}))
	defer notFound.Close()

	cases := []struct {
		name string
		url  string
		want bool
	}{
		{"serves JomForm page is verified", jomform.URL, true},
		{"non-JomForm content is NOT verified", other.URL, false},
		{"error status is NOT verified", notFound.URL, false},
		{"unreachable is NOT verified", "http://127.0.0.1:1/f/test", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := probeServesJomFormURL(c.url); got != c.want {
				t.Fatalf("probeServesJomFormURL(%q) = %v, want %v", c.url, got, c.want)
			}
		})
	}
}
