// Unit tests for the Cloudflare credential blob (issue #82). These are pure
// (no DB) — they exercise the sealed-blob kind round-trip and the backward
// compatibility of blobs sealed before the "kind" field existed.
package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
// surfaces the real cause instead of the misleading "code hilang".
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
	if strings.Contains(body, "code hilang") {
		t.Errorf("response still shows misleading 'code hilang': %s", body)
	}
	if !strings.Contains(body, "Cloudflare OAuth ditolak") || !strings.Contains(body, "invalid") {
		t.Errorf("response does not surface the OAuth error: %s", body)
	}
}

// TestCFCallbackMissingCodeStill400 verifies the fallback stays intact: with
// neither code nor error params, cfCallback still returns 400 "code hilang".
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
	if body := rec.Body.String(); !strings.Contains(body, "code hilang") {
		t.Errorf("expected 'code hilang' fallback, got: %s", body)
	}
}
