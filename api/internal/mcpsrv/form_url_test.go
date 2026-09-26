package mcpsrv

// MCP parity for the public form URL — same root cause as the dashboard's dead
// "View" link, and the same rule.
//
// `list_forms` returns each form's `url` so an agent can hand the merchant a
// working link. That URL was built from WorkspaceStore.WorkspaceSlugByAccount, a
// join on accounts.workspace_id — NULL for every account the SIGNUP path
// creates (AccountStore.Create did not set it) — so the agent received an empty
// url for every merchant account, while the public server was happily serving
// the form at the account slug the whole time.
//
// The first fix taught the serving path to accept that second hostname. The fix
// now is on the SERVING side and it removes the second host outright: a form is
// served on the workspace subdomain, and nothing else. The workspace comes from
// the user's ask-first create, NOT from signup — a signup creates an account and
// no workspace (#718), which is why this fixture links one the way the ask-first
// flow does. This drives the REAL handlers (s.listForms / s.getForm) over a real
// Postgres and, crucially, checks the host against the form SERVING path's own
// lookup (FormStore.ByWorkspaceSlug) instead of a copied slug literal.
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

	// A user + account created the way SIGNUP creates them — an account and NO
	// workspace (#718)...
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
	// Fixture precondition, asserted: the signup insert links NO workspace. That
	// is #718's ask-first boundary, and it is why the host this tool names comes
	// from the ask-first create below.
	var wid *string
	if err := pool.QueryRow(ctx, `SELECT workspace_id::text FROM accounts WHERE id=$1`, acc.ID).Scan(&wid); err != nil {
		t.Fatalf("read workspace_id: %v", err)
	}
	if wid != nil {
		t.Fatalf("the signup path linked account %s to workspace %s — a signup creates an account and NO "+
			"workspace (#718); this fixture must exercise the ask-first flow, not a signup-minted workspace",
			acc.ID, *wid)
	}
	// ...then linked to its first workspace the way the ask-first flow links it
	// (WorkspaceStore.CreateForAccount — the MCP create_workspace tool and
	// rest_workspaces.go both go through this).
	workspaces := dbstore.NewWorkspaceStore(pool)
	wantSlug := "mcp-url-ws-" + userID[:8]
	if _, err := workspaces.CreateForAccount(ctx, userID, "mcp-url-ws", wantSlug, acc.ID); err != nil {
		t.Fatalf("ask-first create: %v", err)
	}
	// Rewrite the account slug to the SYNTHETIC form production carries
	// (`<slug>-<8 hex of the account id>`), so the two names really differ and
	// the "account slug is not a host" assertion below is meaningful.
	synth := acc.Slug + "-" + acc.ID[:8]
	if _, err := pool.Exec(ctx, `UPDATE accounts SET slug=$2 WHERE id=$1`, acc.ID, synth); err != nil {
		t.Fatalf("rewrite account slug to the synthetic production shape: %v", err)
	}
	acc.Slug = synth
	workspaceSlug, err := workspaces.WorkspaceSlugByAccount(ctx, acc.ID)
	if err != nil || workspaceSlug != wantSlug {
		t.Fatalf("WorkspaceSlugByAccount = %q/%v, want the account's workspace slug %q", workspaceSlug, err, wantSlug)
	}
	if workspaceSlug == synth {
		t.Fatalf("fixture is not discriminating: account slug == workspace slug (%q)", synth)
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
		t.Fatalf("list_forms returned an EMPTY url for a published form on a signup-created account. "+
			"An agent handing the merchant this link hands them nothing, and the same empty answer is "+
			"what rendered href=\"\" on the dashboard. output=%s", raw)
	}
	if !strings.HasPrefix(got, "https://") {
		t.Errorf("MCP form url = %q, want an absolute https:// URL", got)
	}
	// The host is the workspace subdomain — the one the dashboard displays.
	if !strings.Contains(got, "https://"+workspaceSlug+".jomform.com/") {
		t.Errorf("MCP form url = %q, want the WORKSPACE host https://%s.jomform.com/%s — the host the "+
			"dashboard shows and the only host the public server serves", got, workspaceSlug, published.Slug)
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
	// The account-slug host this tool used to advertise must be gone.
	if _, err := forms.ByWorkspaceSlug(ctx, acc.Slug, published.Slug); err == nil {
		t.Errorf("the ACCOUNT slug host (%s.jomform.com) still serves this form — a workspace must be "+
			"reachable only on its workspace subdomain (%s.jomform.com)", acc.Slug, workspaceSlug)
	}

	// ── get_form ────────────────────────────────────────────────────────────
	_, detail, err := srv.getForm(callCtx, nil, formRefIn{FormID: published.ID})
	if err != nil {
		t.Fatalf("getForm: %v", err)
	}
	if detail.URL == "" {
		t.Errorf("get_form returned an empty url for a published form — Form Settings and any agent " +
			"reading one form get the same dead link the dashboard had")
	}
	if !strings.HasSuffix(detail.URL, "/"+published.Slug) {
		t.Errorf("get_form url = %q, want it to end in /%s", detail.URL, published.Slug)
	}
}
