package mcpsrv

// MCP half of the social-card edge purge.
//
// THE DEFECT. /og/{slug}.png draws the form's title, workspace name and host into a
// public, cacheable PNG. The status gate made the origin honest (draft -> 404) but
// nothing drops what the CDN already holds, so a card a crawler fetched while the
// form was live kept being served after the page 404ed — measured on prod
// 2026-10-26 on a DRAFT form: `cf-cache-status: HIT`, `age: 1243`, while the
// cache-busted origin answered 404.
//
// Why an MCP test and not just the REST one: an agent unpublishes forms through
// publish_form / unpublish_form, and a fix wired only into the REST route would be
// bypassed by the tool the agent is told to use. These tests drive the real handler
// over a real Postgres, against a fake CDN, and read the reply an agent receives.
//
//	TestMCPUnpublishPurgesTheCachedSocialCard
//	    — publish_form status=draft on a published form asks the CDN for the card's
//	      prefix (on the workspace host) in the same call, and reports `edge`.
//	TestMCPUnpublishStillAnswersWhenThePurgeIsNotConfigured
//	    — no CDN credentials: the tool still unpublishes, and NAMES the outcome plus
//	      the window that bounds how long the old card survives.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/wzul/jomform/api/internal/cdnpurge"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// mcpPurgeRecorder is a fake Cloudflare purge endpoint: it records the request and
// the prefixes it was asked to drop.
type mcpPurgeRecorder struct {
	mu       sync.Mutex
	calls    int
	method   string
	path     string
	auth     string
	prefixes []string
	bodyErr  string
}

func (p *mcpPurgeRecorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Prefixes []string `json:"prefixes"`
		Files    []string `json:"files"`
	}
	err := json.NewDecoder(r.Body).Decode(&body)
	p.mu.Lock()
	p.calls++
	p.method = r.Method
	p.path = r.URL.Path
	p.auth = r.Header.Get("Authorization")
	p.prefixes = append([]string(nil), body.Prefixes...)
	if err != nil {
		p.bodyErr = err.Error()
	} else if len(body.Files) > 0 {
		p.bodyErr = "request used files[] (exact URLs), which leaves the query variants cached"
	}
	p.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"success":true,"errors":[],"messages":[],"result":{"id":"purge-ok"}}`))
}

func (p *mcpPurgeRecorder) snapshot() (calls int, method, path, auth string, prefixes []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls, p.method, p.path, p.auth, append([]string(nil), p.prefixes...)
}

// mcpCardFixture is one account + workspace + published form, and the Server under
// test wired with the stores the card purge needs (Accounts for a verified custom
// domain, Workspaces for the subdomain host).
type mcpCardFixture struct {
	srv       *Server
	forms     *dbstore.FormStore
	ctx       context.Context
	accountID string
	wsHost    string
	formID    string
	slug      string
}

func newMCPCardFixture(t *testing.T, slug string) mcpCardFixture {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Open(t)

	var userID, wsSlug, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('mcp-og-' || md5(random()::text) || '@example.test', 'it') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'mcp-og-' || md5(random()::text), 'mcp-og-' || md5(random()::text))
		 RETURNING slug`, userID).Scan(&wsSlug); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 SELECT $1, 'mcp-og-' || md5(random()::text), 'mcp-og-' || md5(random()::text), id
		 FROM workspaces WHERE slug = $2
		 RETURNING id`, userID, wsSlug).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	forms := dbstore.NewFormStore(pool)
	f := &dbstore.Form{
		AccountID: accountID, Slug: slug, Title: "Contest Entry",
		Config: map[string]any{"mode": "response", "blocks": []any{
			map[string]any{"id": "b1", "type": "email", "value": "Email"},
		}},
		Theme: map[string]any{}, Status: dbstore.FormStatusPublished,
	}
	if err := forms.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}

	return mcpCardFixture{
		srv: &Server{deps: &Deps{
			Forms:      forms,
			Accounts:   dbstore.NewAccountStore(pool),
			Workspaces: dbstore.NewWorkspaceStore(pool),
		}},
		forms:     forms,
		ctx:       ctx,
		accountID: accountID,
		wsHost:    wsSlug + ".jomform.com",
		formID:    f.ID,
		slug:      f.Slug,
	}
}

// storedStatus reads the status from the database: the reply is not evidence that
// the row changed.
func (fx mcpCardFixture) storedStatus(t *testing.T) string {
	t.Helper()
	f, err := fx.forms.GetByID(fx.ctx, fx.accountID, fx.formID)
	if err != nil {
		t.Fatalf("read form back: %v", err)
	}
	return f.Status
}

// callCtx is the identity an agent's tool call carries.
func (fx mcpCardFixture) callCtx() context.Context {
	return WithAuthIdentity(fx.ctx,
		&AuthIdentity{AccountID: fx.accountID, Scopes: []string{"readwrite"}})
}

// pointTheMCPCardPurgeAt makes cdnpurge.FromEnv reach the fake CDN for the tenant
// subdomains (a wildcard: they are per-workspace, so they cannot be enumerated).
func pointTheMCPCardPurgeAt(t *testing.T, srv *httptest.Server) {
	t.Helper()
	t.Setenv(cdnpurge.EnvToken, "test-token")
	t.Setenv(cdnpurge.EnvZoneID, "zone-123")
	t.Setenv(cdnpurge.EnvAPIBase, srv.URL)
	t.Setenv(cdnpurge.EnvHosts, "*.jomform.com")
}

func TestMCPUnpublishPurgesTheCachedSocialCard(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping MCP card-purge test")
	}
	fx := newMCPCardFixture(t, "mcp-og-card-"+randHex(4))

	rec := &mcpPurgeRecorder{}
	cdn := httptest.NewServer(rec)
	defer cdn.Close()
	pointTheMCPCardPurgeAt(t, cdn)

	_, out, err := fx.srv.publishFormMCP(fx.callCtx(), nil, publishFormIn{
		FormID: fx.formID, Status: dbstore.FormStatusDraft,
	})
	if err != nil {
		t.Fatalf("publish_form status=draft: %v", err)
	}
	if got := fx.storedStatus(t); got != dbstore.FormStatusDraft {
		t.Fatalf("stored status = %q, want draft", got)
	}
	calls, method, path, auth, prefixes := rec.snapshot()
	if calls != 1 {
		t.Fatalf("the card purge was requested %d times, want 1 (prefixes %v)", calls, prefixes)
	}
	if method != http.MethodPost || path != "/zones/zone-123/purge_cache" {
		t.Errorf("purge call = %s %s, want POST /zones/zone-123/purge_cache", method, path)
	}
	if auth != "Bearer test-token" {
		t.Errorf("purge Authorization = %q, want the configured token", auth)
	}
	want := "https://" + fx.wsHost + "/og/" + fx.slug + ".png"
	if len(prefixes) != 1 || prefixes[0] != want {
		t.Fatalf("purged prefixes = %v, want exactly [%q]", prefixes, want)
	}
	if out["edge"] != cdnpurge.StatusPurged {
		t.Errorf("tool result edge = %v, want %q (result %v)", out["edge"], cdnpurge.StatusPurged, out)
	}
	if _, ok := out["edge_max_age_s"]; ok {
		t.Errorf("a successful purge reported a residual window: %v", out)
	}
}

func TestMCPUnpublishStillAnswersWhenThePurgeIsNotConfigured(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping MCP card-purge test")
	}
	fx := newMCPCardFixture(t, "mcp-og-noconf-"+randHex(4))
	// The supported "no CDN credential" deploy — and prod's actual state today.
	t.Setenv(cdnpurge.EnvToken, "")
	t.Setenv(cdnpurge.EnvZoneID, "")

	_, out, err := fx.srv.publishFormMCP(fx.callCtx(), nil, publishFormIn{
		FormID: fx.formID, Status: dbstore.FormStatusDraft,
	})
	if err != nil {
		t.Fatalf("publish_form status=draft without CDN credentials: %v — the status write has "+
			"already happened and a purge cannot undo it", err)
	}
	if got := fx.storedStatus(t); got != dbstore.FormStatusDraft {
		t.Fatalf("stored status = %q, want draft — the tool answered but did not change the row", got)
	}
	if out["edge"] != cdnpurge.StatusNotConfigured {
		t.Errorf("tool result edge = %v, want %q — an unconfigured purge must be NAMED (result %v)",
			out["edge"], cdnpurge.StatusNotConfigured, out)
	}
	if got, ok := out["edge_max_age_s"].(int); !ok || got != cdnpurge.OGImageMaxAgeSeconds {
		t.Errorf("tool result edge_max_age_s = %v, want %d — the window that now bounds how long "+
			"the old card can be served (result %v)", out["edge_max_age_s"], cdnpurge.OGImageMaxAgeSeconds, out)
	}
}
