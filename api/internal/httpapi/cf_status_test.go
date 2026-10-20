// The Cloudflare status answer the dashboard branches on.
//
// cfStatus returns {"linked":…,"configured":…} and the settings UI offers the
// Cloudflare auto-setup path exactly when configured is true; when it is false
// the operator is told to add the CNAME by hand. That makes this one boolean a
// product decision, not a diagnostic, and the defect it must never have again is
// the two halves disagreeing: a deployment that reports configured=true while
// the flow cannot complete, or false while it could.
//
// WHY THIS DRIVES THE REAL HANDLER OVER A REAL DATABASE (not cfOAuthConfigured
// alone): a helper test stays green if the handler hard-codes the answer, reads
// a different env var, or inverts it. The card this file answers was raised
// against the endpoint's answer, so the endpoint is what is exercised.
//
// Integration against a real Postgres; skipped without JOMFORM_TEST_DATABASE_URL.
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

func TestCFStatusReportsConfiguredFromTheClientIDAlone(t *testing.T) {
	pool := bundleTestPool(t)
	ctx := context.Background()
	accountID, _ := bundleFixture(t, ctx, pool)
	h := newBundleHandlers(pool)

	cases := []struct {
		name   string
		id     string
		secret string
		want   bool
	}{
		// The production shape, measured against Cloudflare's token endpoint: a
		// public PKCE client needs no secret, so the endpoint must say the
		// auto-setup path is available.
		{"id only — public PKCE client, auto-setup offered", "test-client-id", "", true},
		// No client id: the flow cannot even start, so the answer must be false
		// and the UI falls back to "add the CNAME record manually".
		{"no client id — not configured, manual CNAME", "", "", false},
		// A secret alone is not a configuration: the client id is the switch, so
		// an operator who pasted only the secret must still be told to add the
		// CNAME by hand rather than sent into a broken flow.
		{"secret without id — still not configured", "", "test-secret", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("JF_CF_OAUTH_CLIENT_ID", c.id)
			t.Setenv("JF_CF_OAUTH_CLIENT_SECRET", c.secret)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/v1/rest/cf/status", nil)
			h.cfStatus(rec, req, &AccountCtx{Account: &dbstore.Account{ID: accountID}})

			if rec.Code != http.StatusOK {
				t.Fatalf("status: got %d want 200; body: %s", rec.Code, rec.Body.String())
			}
			var body struct {
				Linked     bool `json:"linked"`
				Configured bool `json:"configured"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode status body %q: %v", rec.Body.String(), err)
			}
			if body.Configured != c.want {
				t.Fatalf("configured = %v, want %v (client id %q, secret set: %v) — the dashboard offers the auto-setup path on true",
					body.Configured, c.want, c.id, c.secret != "")
			}
			if body.Linked {
				t.Errorf("linked = true for an account with no stored Cloudflare credential")
			}
		})
	}
}
