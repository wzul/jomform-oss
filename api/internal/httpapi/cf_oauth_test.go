// Unit tests for the Cloudflare credential blob (issue #82) and for the
// client-authentication contract of the Cloudflare OAuth *flow*. These are pure
// (no DB) — they exercise the sealed-blob kind round-trip, the backward
// compatibility of blobs sealed before the "kind" field existed, and the exact
// form the token endpoints receive.
package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/cryptoutil"
)

func testSealKey() []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	return key
}

// TestCFUnsealKindRoundTrip seals each credential kind and confirms cfUnseal
// returns the same kind + secret material.
func TestCFUnsealKindRoundTrip(t *testing.T) {
	key := testSealKey()
	cases := []cfCred{
		{Kind: cfKindOAuth, Access: "acc", Refresh: "ref"},
		{Kind: cfKindAPIToken, Access: "tok", AccountEmail: "a@b.c", AccountName: "acct"},
		{Kind: cfKindGlobalKey, Access: "gkey", AccountEmail: "a@b.c"},
	}
	for _, c := range cases {
		blob, _ := json.Marshal(c)
		sealed, err := cryptoutil.Encrypt(key, blob)
		if err != nil {
			t.Fatalf("encrypt %s: %v", c.Kind, err)
		}
		got, err := cfUnseal(key, sealed)
		if err != nil {
			t.Fatalf("unseal %s: %v", c.Kind, err)
		}
		if got.Kind != c.Kind {
			t.Errorf("kind: got %q want %q", got.Kind, c.Kind)
		}
		if got.Access != c.Access {
			t.Errorf("access: got %q want %q", got.Access, c.Access)
		}
		if got.AccountEmail != c.AccountEmail {
			t.Errorf("account_email: got %q want %q", got.AccountEmail, c.AccountEmail)
		}
	}
}

// TestCFUnsealLegacyBlobDefaultsToOAuth seals a blob WITHOUT the "kind" field
// (the pre-#82 format) and confirms cfUnseal defaults it to oauth so existing
// OAuth links keep working unchanged.
func TestCFUnsealLegacyBlobDefaultsToOAuth(t *testing.T) {
	key := testSealKey()
	legacy := map[string]string{"access": "acc", "refresh": "ref"} // no "kind"
	blob, _ := json.Marshal(legacy)
	sealed, err := cryptoutil.Encrypt(key, blob)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	got, err := cfUnseal(key, sealed)
	if err != nil {
		t.Fatalf("unseal: %v", err)
	}
	if got.Kind != cfKindOAuth {
		t.Errorf("legacy blob kind: got %q want %q", got.Kind, cfKindOAuth)
	}
	if got.Access != "acc" || got.Refresh != "ref" {
		t.Errorf("legacy blob material lost: got %+v", got)
	}
}

// TestCFCallbackSurfacesOAuthError verifies issue #315: when Cloudflare
// redirects back with error/error_description (e.g. invalid_scope), cfCallback
// surfaces the real cause instead of the misleading "code is missing".
func TestCFCallbackSurfacesOAuthError(t *testing.T) {
	os.Setenv("JF_CF_OAUTH_CLIENT_ID", "test-client-id")
	t.Cleanup(func() { os.Unsetenv("JF_CF_OAUTH_CLIENT_ID") })

	h := &RESTHandlers{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET",
		"/v1/rest/cf/callback?error=invalid_scope&error_description=The+requested+scope+zone%3Aread+is+invalid&state=st",
		nil)
	req.AddCookie(&http.Cookie{Name: "cf_oauth_state", Value: "st", Path: "/"})
	req.AddCookie(&http.Cookie{Name: "cf_pkce_v", Value: "verifier", Path: "/"})

	h.cfCallback(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "code is missing") {
		t.Errorf("response still shows misleading 'code is missing': %s", body)
	}
	if !strings.Contains(body, "Cloudflare OAuth was rejected") || !strings.Contains(body, "invalid") {
		t.Errorf("response does not surface the OAuth error: %s", body)
	}
}

// TestCFCallbackMissingCodeStill400 verifies the fallback stays intact: with
// neither code nor error params, cfCallback still returns 400 "code is missing".
func TestCFCallbackMissingCodeStill400(t *testing.T) {
	os.Setenv("JF_CF_OAUTH_CLIENT_ID", "test-client-id")
	t.Cleanup(func() { os.Unsetenv("JF_CF_OAUTH_CLIENT_ID") })

	h := &RESTHandlers{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/rest/cf/callback?state=st", nil)
	req.AddCookie(&http.Cookie{Name: "cf_oauth_state", Value: "st", Path: "/"})
	req.AddCookie(&http.Cookie{Name: "cf_pkce_v", Value: "verifier", Path: "/"})

	h.cfCallback(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "code is missing") {
		t.Errorf("expected 'code is missing' fallback, got: %s", body)
	}
}

// TestCFOAuthConfiguredIsClientIDOnly pins the deployment rule that the status
// endpoint (cfStatus) and every flow handler (cfStart, cfCallback, and the
// domain auto-CNAME paths) share: the Cloudflare OAuth client ID alone turns the
// auto-setup feature on, and the client SECRET is not part of "configured".
//
// This is a REGRESSION NET, not a fix for a past bug — the id-only test was
// already there and was measured correct against Cloudflare. It exists because
// the tempting change (require the secret the way the sibling Cloudflare *login*
// provider cfOidcConfigured does) silently disables a working deployment:
// JomForm's OAuth client is a public PKCE client whose token endpoint refuses a
// client secret, so an id-only deployment is complete, not half-configured.
func TestCFOAuthConfiguredIsClientIDOnly(t *testing.T) {
	cases := []struct {
		name   string
		id     string
		secret string
		want   bool
	}{
		{"id only — public PKCE client, complete", "test-client-id", "", true},
		{"id and secret — confidential client registration", "test-client-id", "test-secret", true},
		{"neither — the flow is off", "", "", false},
		{"secret without id — still off, the id is the switch", "", "test-secret", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("JF_CF_OAUTH_CLIENT_ID", c.id)
			t.Setenv("JF_CF_OAUTH_CLIENT_SECRET", c.secret)
			if got := cfOAuthConfigured(); got != c.want {
				t.Fatalf("cfOAuthConfigured() = %v, want %v (client id %q, secret set: %v)",
					got, c.want, c.id, c.secret != "")
			}
		})
	}
}

// TestCFTokenRequestsCarryASecretOnlyWhenConfigured drives the REAL exchange and
// refresh paths against a stub token endpoint and asserts the form that reaches
// the wire, because the client-authentication rule is what makes a public-client
// deployment work or fail:
//
//   - public PKCE client (the production shape): NO client_secret key at all.
//     Cloudflare answers invalid_client for a secret presented by such a client,
//     so its absence is the requirement — an "always send it, empty when unset"
//     refactor breaks the flow while a helper-only test stays green.
//   - confidential client registration: the secret is sent, alongside the PKCE
//     verifier the authorize URL's code_challenge committed to.
func TestCFTokenRequestsCarryASecretOnlyWhenConfigured(t *testing.T) {
	t.Setenv("APP_BASE_URL", "https://forms.example.test")

	var got []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("stub token endpoint: parse form: %v", err)
		}
		got = append(got, r.PostForm)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"acc-tok","refresh_token":"ref-tok"}`)
	}))
	defer srv.Close()
	prev := cfTokenURL
	cfTokenURL = srv.URL
	t.Cleanup(func() { cfTokenURL = prev })

	const verifier = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMN"

	// oneForm runs fn against the stub and returns the single form it posted.
	oneForm := func(t *testing.T, fn func() (*cfTokenPair, error)) url.Values {
		t.Helper()
		got = nil
		pair, err := fn()
		if err != nil {
			t.Fatalf("token request: %v", err)
		}
		if pair == nil || pair.Access != "acc-tok" || pair.Refresh != "ref-tok" {
			t.Fatalf("token pair not parsed from the response: %+v", pair)
		}
		if len(got) != 1 {
			t.Fatalf("stub received %d requests, want exactly 1", len(got))
		}
		return got[0]
	}

	t.Run("exchange for a public client posts no client_secret", func(t *testing.T) {
		t.Setenv("JF_CF_OAUTH_CLIENT_ID", "public-client-id")
		t.Setenv("JF_CF_OAUTH_CLIENT_SECRET", "")
		f := oneForm(t, func() (*cfTokenPair, error) {
			return cfExchangeCode(context.Background(), "the-code", verifier)
		})
		if f.Has("client_secret") {
			t.Errorf("public-client exchange posted client_secret=%q — Cloudflare refuses this with invalid_client", f.Get("client_secret"))
		}
		if f.Get("client_id") != "public-client-id" {
			t.Errorf("client_id = %q, want public-client-id", f.Get("client_id"))
		}
		if f.Get("grant_type") != "authorization_code" || f.Get("code") != "the-code" {
			t.Errorf("grant_type/code = %q/%q, want authorization_code/the-code", f.Get("grant_type"), f.Get("code"))
		}
		if f.Get("code_verifier") != verifier {
			t.Errorf("code_verifier = %q, want the PKCE verifier", f.Get("code_verifier"))
		}
		if f.Get("redirect_uri") != "https://forms.example.test/api/cf/callback" {
			t.Errorf("redirect_uri = %q, want the app origin + /api/cf/callback", f.Get("redirect_uri"))
		}
	})

	t.Run("exchange for a confidential client posts the secret", func(t *testing.T) {
		t.Setenv("JF_CF_OAUTH_CLIENT_ID", "confidential-client-id")
		t.Setenv("JF_CF_OAUTH_CLIENT_SECRET", "confidential-secret")
		f := oneForm(t, func() (*cfTokenPair, error) {
			return cfExchangeCode(context.Background(), "the-code", verifier)
		})
		if f.Get("client_secret") != "confidential-secret" {
			t.Errorf("client_secret = %q, want the configured secret", f.Get("client_secret"))
		}
		if f.Get("client_id") != "confidential-client-id" {
			t.Errorf("client_id = %q, want confidential-client-id", f.Get("client_id"))
		}
	})

	t.Run("refresh for a public client posts no client_secret", func(t *testing.T) {
		t.Setenv("JF_CF_OAUTH_CLIENT_ID", "public-client-id")
		t.Setenv("JF_CF_OAUTH_CLIENT_SECRET", "")
		f := oneForm(t, func() (*cfTokenPair, error) {
			return cfRefresh(context.Background(), "the-refresh-token")
		})
		if f.Has("client_secret") {
			t.Errorf("public-client refresh posted client_secret — Cloudflare refuses this with invalid_client")
		}
		if f.Get("grant_type") != "refresh_token" || f.Get("refresh_token") != "the-refresh-token" {
			t.Errorf("grant_type/refresh_token = %q/%q, want refresh_token/the-refresh-token", f.Get("grant_type"), f.Get("refresh_token"))
		}
	})

	t.Run("refresh for a confidential client posts the secret", func(t *testing.T) {
		t.Setenv("JF_CF_OAUTH_CLIENT_ID", "confidential-client-id")
		t.Setenv("JF_CF_OAUTH_CLIENT_SECRET", "confidential-secret")
		f := oneForm(t, func() (*cfTokenPair, error) {
			return cfRefresh(context.Background(), "the-refresh-token")
		})
		if f.Get("client_secret") != "confidential-secret" {
			t.Errorf("client_secret = %q, want the configured secret", f.Get("client_secret"))
		}
	})
}
