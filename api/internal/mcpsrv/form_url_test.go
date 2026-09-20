package mcpsrv

// MCP parity for the public form URL — same root cause as the dashboard's dead
// "View" link.
//
// `list_forms` returns each form's `url` so an agent can hand the merchant a
// working link. That URL was built from WorkspaceStore.WorkspaceSlugByAccount, a
// join on accounts.workspace_id — NULL for every account the SIGNUP path
// creates (AccountStore.Create does not set it) — so the agent received an empty
// url for every merchant account, while the public server was happily serving
// the form at {account-slug}.jomform.com/{slug} the whole time.
//
// This drives the REAL handlers (s.listForms / s.getForm) over a real Postgres
// and, crucially, checks the host against the form SERVING path's own lookup
// (FormStore.ByWorkspaceSlug) instead of a copied slug literal.
//
// Skipped without JOMFORM_TEST_DATABASE_URL.
import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

func TestMCPFormURLIsServedForASignupCreatedAccount(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping MCP form-url test")
	}
	ctx := context.Background()
	pool := testdb.Open(t)

	// A user + account created the way SIGNUP creates them: through
	// AccountStore.Create, which sets no workspace_id.
	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('mcp-url-' || md5(random()::text) || '@example.test', 'it') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	acc := &dbstore.Account{
		OwnerUserID: userID,
		Name:        "mcp-url-store",
		Slug:        "mcp-url-" + userID[:8],
	}
	if err := dbstore.NewAccountStore(pool).Create(ctx, acc); err != nil {
		t.Fatalf("create account via the signup path: %v", err)
	}
	// Fixture preconditions, asserted: the NULL workspace_id shape, and the
	// pre-fix resolver really answering "". Without these a green run here would
	// prove nothing about the defect.
	var wid *string
	if err := pool.QueryRow(ctx, `SELECT workspace_id::text FROM accounts WHERE id=$1`, acc.ID).Scan(&wid); err != nil {
		t.Fatalf("read workspace_id: %v", err)
	}
	if wid != nil {
		t.Fatalf("fixture is not discriminating: signup-created account %s has workspace_id=%s", acc.ID, *wid)
	}
	if slug, err := dbstore.NewWorkspaceStore(pool).WorkspaceSlugByAccount(ctx, acc.ID); err == nil || slug != "" {
		t.Fatalf("the workspace-only resolver answered slug=%q err=%v — the premise of this regression "+
			"test no longer holds", slug, err)
	}

	forms := dbstore.NewFormStore(pool)
	draft := &dbstore.Form{
		AccountID: acc.ID, Slug: "mcp-url-draft-" + acc.ID[:8], Title: "Draft form",
		Config: map[string]any{"blocks": []any{}}, Status: dbstore.FormStatusDraft,
	}
	published := &dbstore.Form{
		AccountID: acc.ID, Slug: "mcp-url-pub-" + acc.ID[:8], Title: "Published form",
		Config: map[string]any{"blocks": []any{}}, Status: dbstore.FormStatusPublished,
	}
	for _, f := range []*dbstore.Form{draft, published} {
		if err := forms.Create(ctx, f); err != nil {
			t.Fatalf("create form %s: %v", f.Slug, err)
		}
	}

	srv := &Server{deps: &Deps{
		Forms:      forms,
		Accounts:   dbstore.NewAccountStore(pool),
		Workspaces: dbstore.NewWorkspaceStore(pool),
	}}
	callCtx := WithAuthIdentity(ctx, &AuthIdentity{AccountID: acc.ID, Scopes: []string{"readwrite"}})

	// ── list_forms ──────────────────────────────────────────────────────────
	_, page, err := srv.listForms(callCtx, nil, emptyIn{})
	if err != nil {
		t.Fatalf("listForms: %v", err)
	}
	raw, err := json.Marshal(page.Items)
	if err != nil {
		t.Fatalf("marshal listForms output: %v", err)
	}
	var rows []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		URL    string `json:"url"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("decode listForms JSON: %v (%s)", err, raw)
	}
	byID := map[string]string{}
	for _, r := range rows {
		byID[r.ID] = r.URL
	}

	got, ok := byID[published.ID]
	if !ok {
		t.Fatalf("the published form is missing from list_forms entirely (%s)", raw)
	}
	if got == "" {
		t.Fatalf("list_forms returned an EMPTY url for a published form on a signup-created account "+
			"(accounts.workspace_id is NULL). An agent handing the merchant this link hands them nothing, "+
			"and the same empty answer is what rendered href=\"\" on the dashboard. output=%s", raw)
	}
	if !strings.HasPrefix(got, "https://") {
		t.Errorf("MCP form url = %q, want an absolute https:// URL", got)
	}
	// A draft form is not publicly reachable, so url stays empty by design —
	// asserted so a future change cannot quietly start advertising drafts.
	if u := byID[draft.ID]; u != "" {
		t.Errorf("a draft form must not advertise a public url, got %q", u)
	}

	// ── the decisive check: the emitted host actually serves the form ───────
	host := strings.TrimPrefix(got, "https://")
	host = host[:strings.Index(host, ".jomform.com")]
	served, err := forms.ByWorkspaceSlug(ctx, host, published.Slug)
	if err != nil {
		t.Fatalf("the url this tool hands an agent does not resolve on the serving path "+
			"(ByWorkspaceSlug(%q, %q)): %v", host, published.Slug, err)
	}
	if served.ID != published.ID {
		t.Fatalf("host %q resolved form %s, want %s", host, served.ID, published.ID)
	}
	if served.AccountID != acc.ID {
		t.Fatalf("host %q resolved a form owned by another account (%s)", host, served.AccountID)
	}

	// ── get_form ────────────────────────────────────────────────────────────
	_, detail, err := srv.getForm(callCtx, nil, formRefIn{FormID: published.ID})
	if err != nil {
		t.Fatalf("getForm: %v", err)
	}
	if detail.URL == "" {
		t.Errorf("get_form returned an empty url for a published form — Form Settings and any agent "+
			"reading one form get the same dead link the dashboard had")
	}
	if !strings.HasSuffix(detail.URL, "/"+published.Slug) {
		t.Errorf("get_form url = %q, want it to end in /%s", detail.URL, published.Slug)
	}
}
