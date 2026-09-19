package mcpsrv

// Regression: `list_domains` was dead over a real MCP session for every real
// claim. The tool goes through `DomainStore.ListDomains`, which scanned the
// nullable account_domains columns (auto_cname, tls_error, tls_cert_enc,
// tls_key_enc, tls_email) into plain strings; the production write path
// (`add_domain`) inserts only (account_id, domain, provider), so those five
// are NULL on every newly claimed row and the scan failed with
//
//	db: can't scan into dest[6] (col: auto_cname): cannot scan NULL into *string
//
// The tool errored before returning a single record. This test drives the
// REAL MCP transport (SDK client → streamable HTTP → API-key auth → tool
// handlers) against a real Postgres and asserts the claim it just made through
// `add_domain` comes back from `list_domains` as {"items":[...]}.
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL); skipped
// when unset.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// domainMCPEnv is the running MCP server plus the account it authenticates as.
type domainMCPEnv struct {
	url       string
	accountID string
	apiKey    string
}

// newDomainMCPEnv boots the MCP server with only the stores the domain tools
// need, authenticates with a real API key row, and leaves the server running
// for the duration of the test.
func newDomainMCPEnv(t *testing.T) domainMCPEnv {
	t.Helper()
	if strings.TrimSpace(os.Getenv(testdb.DSNEnv)) == "" {
		t.Skipf("%s not set; skipping MCP domain integration test", testdb.DSNEnv)
	}
	ctx := context.Background()
	pool := testdb.Open(t)

	var userID, workspaceID, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('dom-mcp-' || md5(random()::text) || '@example.test', 'it') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'dom-mcp-ws-' || md5(random()::text), 'dom-mcp-' || md5(random()::text))
		 RETURNING id`, userID).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 VALUES ($1, 'dom-mcp-acc-' || md5(random()::text), 'dom-mcp-' || md5(random()::text), $2)
		 RETURNING id`, userID, workspaceID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	srv := New(&Deps{
		Accounts:   dbstore.NewAccountStore(pool),
		Keys:       dbstore.NewAPIKeyStore(pool),
		Workspaces: dbstore.NewWorkspaceStore(pool),
		Domains:    dbstore.NewDomainStore(pool),
	}, nil, make([]byte, 32), nil)
	srv.registerTools()

	raw, hash, err := auth.GenerateAPIKey()
	if err != nil {
		t.Fatalf("generate api key: %v", err)
	}
	if _, err := dbstore.NewAPIKeyStore(pool).Create(ctx, accountID, hash, "dom-mcp-it", []string{"readwrite"}); err != nil {
		t.Fatalf("store api key: %v", err)
	}

	ts := httptest.NewServer(srv.HTTPHandler())
	t.Cleanup(ts.Close)
	return domainMCPEnv{url: ts.URL, accountID: accountID, apiKey: raw}
}

// domainMCPClient is a session connected to the test server as the seeded
// account. `DisableStandaloneSSE` keeps the client to plain POSTs, which is
// what a native MCP client does.
func domainMCPClient(t *testing.T, env domainMCPEnv) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	client := mcp.NewClient(&mcp.Implementation{Name: "domains-it", Version: "1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: env.url + "/mcp",
		HTTPClient: &http.Client{Transport: &apiKeyRoundTripper{
			base: http.DefaultTransport, token: env.apiKey,
		}},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("MCP connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// apiKeyRoundTripper carries the Bearer API key on every MCP request, which is
// how a client authenticates after the transport layer resolves the key.
type apiKeyRoundTripper struct {
	base  http.RoundTripper
	token string
}

func (b *apiKeyRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	r2 := r.Clone(r.Context())
	r2.Header.Set("Authorization", "Bearer "+b.token)
	return b.base.RoundTrip(r2)
}

// mcpToolText flattens a tool result's text content, so a failure message
// names what the tool actually answered.
func mcpToolText(res *mcp.CallToolResult) string {
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

// TestListDomainsOverRealMCPSession is the acceptance path: claim a domain with
// `add_domain` over the protocol, then read it back with `list_domains`. The
// raw columns are asserted NULL first, so a fixture that stopped reproducing
// the reported state fails instead of silently certifying the bug as absent.
func TestListDomainsOverRealMCPSession(t *testing.T) {
	env := newDomainMCPEnv(t)
	session := domainMCPClient(t, env)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const domain = "mcp-livedomain.example.test"

	call, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "add_domain",
		Arguments: map[string]any{"domain": domain},
	})
	if err != nil {
		t.Fatalf("add_domain call: %v", err)
	}
	if call.IsError {
		t.Fatalf("add_domain failed: %s", mcpToolText(call))
	}

	// The claim the tool just made must be the reported shape: the five
	// nullable columns NULL, because add_domain inserts only three columns.
	assertNullableClaimColumns(t, env.accountID, domain)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_domains"})
	if err != nil {
		t.Fatalf("list_domains call: %v", err)
	}
	if res.IsError {
		t.Fatalf("list_domains returned an error for a real claim: %s", mcpToolText(res))
	}

	items := structuredItems(t, res)
	if len(items) != 1 {
		t.Fatalf("list_domains returned %d items, want 1 (%v)", len(items), items)
	}
	got := items[0]
	if got["domain"] != domain {
		t.Errorf("item domain = %v, want %q", got["domain"], domain)
	}
	if got["provider"] != "cname" {
		t.Errorf("item provider = %v, want cname", got["provider"])
	}
	// auto_cname always rides the wire (no omitempty) and must be the empty
	// string a NULL normalised to — that is the value the reported error
	// stopped the tool from ever producing.
	if v, ok := got["auto_cname"]; !ok || v != "" {
		t.Errorf("item auto_cname = %v (present=%v), want an empty string", v, ok)
	}
	// tls_error / tls_email carry `omitempty` on domainOut, so an empty value
	// is dropped from the payload rather than sent as "". Pin that contract:
	// absent, or present-and-empty — never a stale value.
	for _, key := range []string{"tls_error", "tls_email"} {
		if v, ok := got[key]; ok && v != "" {
			t.Errorf("item %s = %v, want it omitted or empty", key, v)
		}
	}
	if got["tls_mode"] != "auto" {
		t.Errorf("item tls_mode = %v, want auto", got["tls_mode"])
	}
	if got["verified"] != false {
		t.Errorf("item verified = %v, want false for a fresh claim", got["verified"])
	}
}

// TestListDomainsEmptyAccountOverMCPSession: an account with no claims must
// return an empty object envelope, not an error and not a bare array — the
// outputSchema this tool advertises is an object.
func TestListDomainsEmptyAccountOverMCPSession(t *testing.T) {
	env := newDomainMCPEnv(t)
	session := domainMCPClient(t, env)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_domains"})
	if err != nil {
		t.Fatalf("list_domains call: %v", err)
	}
	if res.IsError {
		t.Fatalf("list_domains on an empty account returned an error: %s", mcpToolText(res))
	}
	items := structuredItems(t, res)
	if items == nil {
		t.Fatalf("list_domains did not return an `items` array: %s", mcpToolText(res))
	}
	if len(items) != 0 {
		t.Errorf("items = %v, want an empty array", items)
	}
}

// structuredItems extracts the `items` array from a tool result, accepting
// either the decoded structured content or the JSON text content (a client
// without structured-output support sees the latter).
func structuredItems(t *testing.T, res *mcp.CallToolResult) []map[string]any {
	t.Helper()
	raw := res.StructuredContent
	if raw == nil {
		var envelope struct {
			Items []map[string]any `json:"items"`
		}
		if err := json.Unmarshal([]byte(mcpToolText(res)), &envelope); err != nil {
			t.Fatalf("tool result is neither structured content nor an items object: %s", mcpToolText(res))
		}
		return envelope.Items
	}
	blob, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var envelope struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(blob, &envelope); err != nil {
		t.Fatalf("structured content is not an items object: %v (%s)", err, blob)
	}
	return envelope.Items
}

// assertNullableClaimColumns fails when the fixture can no longer reproduce the
// reported state — i.e. when a claimed domain no longer has NULL in the five
// columns the scan could not handle.
func assertNullableClaimColumns(t *testing.T, accountID, domain string) {
	t.Helper()
	pool := testdb.Open(t)
	ctx := context.Background()
	var autoCNAME, tlsError, tlsCert, tlsKey, tlsEmail *string
	if err := pool.QueryRow(ctx,
		`SELECT auto_cname, tls_error, tls_cert_enc, tls_key_enc, tls_email
		   FROM account_domains WHERE account_id=$1 AND domain=$2`, accountID, domain).
		Scan(&autoCNAME, &tlsError, &tlsCert, &tlsKey, &tlsEmail); err != nil {
		t.Fatalf("read raw claim columns: %v", err)
	}
	if autoCNAME != nil || tlsError != nil || tlsCert != nil || tlsKey != nil || tlsEmail != nil {
		t.Fatalf("add_domain no longer leaves NULLs (auto_cname=%v tls_error=%v tls_cert_enc=%v tls_key_enc=%v tls_email=%v) — fixture stopped reproducing the reported state",
			autoCNAME, tlsError, tlsCert, tlsKey, tlsEmail)
	}
}
