package mcpsrv

// Issue #509: the /mcp 401 must carry an RFC 6750 Bearer challenge with the
// RFC 9728 `resource_metadata` parameter. Production returned a bare 401 with
// no WWW-Authenticate at all, so a native MCP client had to guess the
// protected-resource metadata URL (the documented flow is 401 → read the
// challenge → fetch the resource metadata → fetch AS metadata → OAuth →
// retry).

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/oauthmeta"
)

// TestUnauthenticatedMCP401CarriesResourceMetadata drives the real transport
// handler with no Authorization header. No stores are wired: the guard clause
// returns before the credential lookup, which is exactly the unauthenticated
// shape production sees.
func TestUnauthenticatedMCP401CarriesResourceMetadata(t *testing.T) {
	srv := &Server{deps: &Deps{}}
	h := srv.HTTPHandler()

	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","method":"tools/list"}`))
	req.Host = "mcp.jomform.com"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}
	challenge := rec.Header().Get("WWW-Authenticate")
	if challenge == "" {
		t.Fatal("no WWW-Authenticate header on the /mcp 401 (issue #509)")
	}
	if !strings.HasPrefix(challenge, "Bearer ") {
		t.Errorf("challenge = %q, want a Bearer scheme", challenge)
	}
	wantURL := oauthmeta.MetadataURL("mcp.jomform.com")
	if !strings.Contains(challenge, `resource_metadata="`+wantURL+`"`) {
		t.Errorf("challenge = %q, want resource_metadata=%q", challenge, wantURL)
	}
	// No credentials were presented, so RFC 6750's error code must NOT be set:
	// error="invalid_token" is only accurate when a token was actually rejected.
	if strings.Contains(challenge, "error=") {
		t.Errorf("challenge = %q must not carry an error code for a request with no credentials", challenge)
	}

	// The advertised metadata URL must be a path this server actually serves,
	// otherwise the challenge points clients at a 404 — the production symptom.
	if !strings.HasSuffix(wantURL, oauthmeta.ProtectedResourcePath) {
		t.Errorf("challenge URL %q does not end in the RFC 9728 well-known path %q", wantURL, oauthmeta.ProtectedResourcePath)
	}
}

// TestMCP401ChallengeUsesRequestHost pins that the challenge URL follows the
// host the client called, which is the same host the PRM document is served
// from (so the URL a client is told to fetch is the URL whose document names
// this very endpoint).
func TestMCP401ChallengeUsesRequestHost(t *testing.T) {
	srv := &Server{deps: &Deps{}}
	h := srv.HTTPHandler()

	req := httptest.NewRequest("POST", "/mcp", nil)
	req.Host = "mcp.staging.example"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("WWW-Authenticate"); !strings.Contains(got,
		`resource_metadata="https://mcp.staging.example`+oauthmeta.ProtectedResourcePath+`"`) {
		t.Errorf("challenge = %q, want the request host", got)
	}
}
