package httpapi

// Card t_67db0213 — the two surfaces that publish Cloudflare `configured` must
// answer the SAME question, and here that is asserted against the env.
//
//	GET  /v1/rest/cf/status    -> {"linked":…, "configured": cfOAuthConfigured()}
//	MCP  get_cloudflare_status -> {"linked":…, "configured": deps.CFOAuthConfigured()}
//
// Before the fix the MCP value was a literal false (encoding "the OAuth link is
// browser-only" — true, but not what the word means on any other surface), so a
// deployment with JF_CF_OAUTH_CLIENT_ID set — production — answered true over
// REST and false over MCP, forever.
//
// This file pins the REST half and the seam: the real cfStatus handler is driven
// over real Postgres in both directions (client id set / unset), and the value
// main.go hands the MCP tool — CFOAuthConfigured(), the exported delegate of the
// very rule cfStatus publishes — is asserted to be exactly that answer. A
// hard-coded or reversed answer on either side fails here.
//
// Why the MCP HANDLER is not driven from this file: it is unexported, and
// mcpsrv cannot import httpapi (cycle). Driving it over the in-memory MCP
// transport would not work either — that transport does not carry the per-call
// auth scope this tool needs (see mcpsrv/variation_write_issue623_test.go for
// the same constraint). The tool handler itself is therefore pinned in
// mcpsrv/cf_status_configured_mcp_test.go (rule true / false / not wired), and
// the chain is closed by cf_mcp_wiring_guard_test.go, which asserts main.go
// passes THIS function into the MCP Deps — so the value asserted here is the
// value the tool publishes.
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL).

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// cfStatusRESTConfigured drives the real REST handler and returns its
// `configured`.
func cfStatusRESTConfigured(t *testing.T, h *RESTHandlers, accountID string) bool {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/rest/cf/status", nil)
	h.cfStatus(rec, req, &AccountCtx{Account: &dbstore.Account{ID: accountID}})
	if rec.Code != 200 {
		t.Fatalf("cfStatus: status %d, body %s", rec.Code, rec.Body.String())
	}
	var out map[string]bool
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode cfStatus body %q: %v", rec.Body.String(), err)
	}
	v, ok := out["configured"]
	if !ok {
		t.Fatalf("cfStatus body has no `configured`: %s", rec.Body.String())
	}
	return v
}

func TestCloudflareConfiguredRESTAndMCPSeamAnswerTheSameQuestion(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping Cloudflare configured agreement test")
	}
	ctx := context.Background()
	pool := testdb.Open(t)

	var userID, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('cf-agree-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 VALUES ($1, 'cf-agree-' || md5(random()::text), 'cf-agree-' || md5(random()::text))
		 RETURNING id`, userID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM accounts WHERE id=$1`, accountID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})

	h := &RESTHandlers{Accounts: dbstore.NewAccountStore(pool), SecretsKey: testSealKey()}

	cases := []struct {
		name     string
		clientID string
		want     bool
	}{
		// Production's shape: the OAuth app is registered on the deployment, so
		// both surfaces must say "configured". The hard-coded false — and a
		// rule that also required the client SECRET (card t_a192b88e's trap:
		// this is a public PKCE client) — fail exactly here.
		{"client_id_set", "test-client-id", true},
		// A deployment without the OAuth app: both must say "not configured".
		{"client_id_unset", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("JF_CF_OAUTH_CLIENT_ID", tc.clientID)

			rest := cfStatusRESTConfigured(t, h, accountID)
			// The exact value cmd/jomform-api/main.go wires into
			// mcpsrv.Deps.CFOAuthConfigured, i.e. what get_cloudflare_status
			// publishes.
			asMCP := CFOAuthConfigured()

			if rest != tc.want {
				t.Errorf("REST /v1/rest/cf/status configured = %v, want %v", rest, tc.want)
			}
			if asMCP != tc.want {
				t.Errorf("the MCP tool's rule reports configured = %v, want %v", asMCP, tc.want)
			}
			if rest != asMCP {
				t.Errorf("the two surfaces disagree: REST configured = %v, MCP-rule configured = %v (same deployment)", rest, asMCP)
			}
		})
	}
}
