// Test for issue #288: the Cloudflare authorize URL must include the OAuth
// scope parameter. Without it Cloudflare's consent screen shows "0 total
// permissions" and disables the Authorize button.
package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
)

func TestCFStartAuthorizeURLIncludesScope(t *testing.T) {
	// cfOAuthConfigured() requires a client id set — and deliberately NOT the
	// secret: JomForm's Cloudflare client is a public PKCE client, so the
	// exchange must not present one. See cfOAuthConfigured's comment and
	// TestCFOAuthConfiguredIsClientIDOnly.
	os.Setenv("JF_CF_OAUTH_CLIENT_ID", "test-client-id")
	t.Cleanup(func() { os.Unsetenv("JF_CF_OAUTH_CLIENT_ID") })

	h := &RESTHandlers{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/rest/cf/start", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "x"})

	// actx is unused by cfStart but still passed through.
	actx := &AccountCtx{}
	h.cfStart(rec, req, actx)

	if rec.Code != 302 {
		t.Fatalf("status: got %d want 302 (redirect to Cloudflare)", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if loc == "" {
		t.Fatal("Location header missing")
	}
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse redirect location: %v", err)
	}
	if u.Host != "dash.cloudflare.com" {
		t.Errorf("redirect host: got %q want dash.cloudflare.com", u.Host)
	}
	gotScope := u.Query().Get("scope")
	if gotScope == "" {
		t.Fatalf("scope param missing from authorize URL %q", loc)
	}
	if gotScope != cfOAuthScope {
		t.Errorf("scope: got %q want %q", gotScope, cfOAuthScope)
	}
}
