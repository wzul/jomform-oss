package mcpsrv

// Card t_67db0213 — get_cloudflare_status published `configured` as a
// HARD-CODED false (`func cfOAuthConfiguredMCP() bool { return false }`), while
// GET /v1/rest/cf/status publishes cfOAuthConfigured() under the same name. On
// a deployment where the REST answer is true (JF_CF_OAUTH_CLIENT_ID set) an MCP
// client asking the same question got false, and because the value was a
// literal no configuration change could ever make the two agree.
//
// The tool now answers from Deps.CFOAuthConfigured, which cmd/jomform-api/
// main.go wires to httpapi.CFOAuthConfigured — the REST rule itself. This test
// drives the REAL tool handler with the rule saying true, false and "not wired",
// and asserts the published value follows the rule instead of a literal.
//
// The handler is invoked directly, not over the in-memory MCP transport: that
// transport does not carry the per-call auth scope (the same reason
// variation_write_issue623_test.go invokes its handlers directly), and this tool
// needs an account identity. What the tool REGISTERS is covered separately by
// mcp_parity_test.go over the protocol.
//
// Coverage split, stated so this test is not over-claimed:
//   - WHAT the rule returns for a given env is cfOAuthConfigured's contract,
//     pinned in httpapi/cf_oauth_test.go + cf_status_surface_agreement_test.go.
//   - THAT main.go wires that rule into this tool is pinned by
//     httpapi/cf_mcp_wiring_guard_test.go — without it the field stays nil and
//     the tool answers configured:false again, which this file alone cannot
//     catch because it builds its own Deps.
//   - THAT nobody re-implements the env test inside this package is pinned by
//     cf_status_env_single_source_test.go.
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL).

import (
	"context"
	"os"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

func TestGetCloudflareStatusConfiguredFollowsTheDeploymentRule(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping MCP get_cloudflare_status test")
	}
	ctx := context.Background()
	pool := testdb.Open(t)

	var userID, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('cf-status-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 VALUES ($1, 'cf-status-' || md5(random()::text), 'cf-status-' || md5(random()::text))
		 RETURNING id`, userID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM accounts WHERE id=$1`, accountID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})

	store := dbstore.NewAccountStore(pool)
	callCtx := WithAuthIdentity(ctx, &AuthIdentity{AccountID: accountID, Scopes: []string{"readwrite"}})

	cases := []struct {
		name string
		rule func() bool
		want bool
	}{
		// The deployment HAS the Cloudflare OAuth app: REST answers true here,
		// so the MCP tool must too. This is the case the hard-coded false broke.
		{"deployment_has_the_oauth_app", func() bool { return true }, true},
		// No OAuth app on this deployment: REST answers false, MCP must agree.
		{"deployment_has_no_oauth_app", func() bool { return false }, false},
		// Nothing wired (bare-mode Deps): the documented fallback. Pinned so
		// the branch is deliberate rather than accidental.
		{"rule_not_wired", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := &Server{deps: &Deps{Accounts: store, CFOAuthConfigured: tc.rule}}
			_, out, err := srv.getCloudflareStatus(callCtx, nil, emptyIn{})
			if err != nil {
				t.Fatalf("get_cloudflare_status: %v", err)
			}
			if out.Configured != tc.want {
				t.Errorf("configured = %v, want %v (the tool must publish the deployment rule, not a literal)", out.Configured, tc.want)
			}
			// No Cloudflare credential was ever stored for this account.
			if out.Linked {
				t.Errorf("linked = true, want false: no Cloudflare credential was saved")
			}
		})
	}
}
