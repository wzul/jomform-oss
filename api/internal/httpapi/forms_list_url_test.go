package httpapi

// Regression for the /forms "View" link (Wan's report: clicking it loaded the
// /forms page again instead of opening the form), now guarding the RULE the fix
// ended on: a workspace is served on its workspace subdomain and nothing else.
//
// THE BUG, end to end: GET /v1/rest/forms fills each row's `url` through
// formURLForAccount, which fell back to formURL(workspaceSlugFor(account)) — and
// that resolved the slug with WorkspaceSlugByAccount, a join on
// accounts.workspace_id. AccountStore.Create (the insert the SIGNUP path uses)
// never set that column, so it was NULL for every signup-created merchant
// account, the join returned no row, the error was swallowed and the payload
// carried url: "". The dashboard then rendered `<a href="">`, which the browser
// resolves to the CURRENT document — with target="_blank", /forms opened again
// in a new tab. Exactly the report.
//
// The first fix taught the serving path to accept the account slug as a second
// hostname so the two agreed. That left every workspace reachable at two
// addresses, the second one invisible (`<workspace>-<8 hex>`). The fix now is
// to remove that second hostname: a workspace is served on its workspace
// subdomain and nothing else, so the URL builder is correct with ONE host.
//
// The workspace comes from the user's ask-first create, NOT from signup — a
// signup creates an account and no workspace (#718), which is why the fixture
// below links one the way the ask-first flow does.
//
// So this drives the REAL REST list handler over a real Postgres, with an
// account created the way signup creates it, and asserts the payload's url is a
// host that actually SERVES the form — checking the host against the serving
// path's own lookup rather than against a hard-coded string.
//
// Skipped without JOMFORM_TEST_DATABASE_URL.
import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

func TestFormsListURLIsServedForASignupCreatedAccount(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)

	// ── an account created through the SIGNUP path ─────────────────────────
	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-view-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	acc := &dbstore.Account{
		OwnerUserID: userID,
		Name:        "it-view-store",
		Slug:        "it-view-" + userID[:8],
	}
	if err := dbstore.NewAccountStore(pool).Create(ctx, acc); err != nil {
		t.Fatalf("create account via the signup path: %v", err)
	}
	// Fixture precondition, asserted: the signup insert links NO workspace. That
	// is #718's ask-first boundary, so the host the payload must name comes from
	// the ask-first create below rather than from the signup insert.
	var wid *string
	if err := pool.QueryRow(ctx, `SELECT workspace_id::text FROM accounts WHERE id=$1`, acc.ID).Scan(&wid); err != nil {
		t.Fatalf("read workspace_id: %v", err)
	}
	if wid != nil {
		t.Fatalf("the signup path linked account %s to workspace %s — a signup creates an account and NO "+
			"workspace (#718); this fixture must exercise the ask-first flow, not a signup-minted workspace",
			acc.ID, *wid)
	}
	// ...linked to its first workspace the way the ask-first flow links it.
	workspaces := dbstore.NewWorkspaceStore(pool)
	wantSlug := "it-view-ws-" + userID[:8]
	if _, err := workspaces.CreateForAccount(ctx, userID, "it-view-ws", wantSlug, acc.ID); err != nil {
		t.Fatalf("ask-first create: %v", err)
	}
	// Rewrite the account slug to the SYNTHETIC form production carries
	// (`<slug>-<8 hex of the account id>`, what WorkspaceStore.Create gives each
	// new workspace's account), so the two names really differ and the
	// "account slug is not a host" assertion below is not comparing a string to
	// itself.
	synth := acc.Slug + "-" + acc.ID[:8]
	if _, err := pool.Exec(ctx, `UPDATE accounts SET slug=$2 WHERE id=$1`, acc.ID, synth); err != nil {
		t.Fatalf("rewrite account slug to the synthetic production shape: %v", err)
	}
	acc.Slug = synth
	// The host the dashboard displays, read from the same resolver the URL
	// builder uses. The payload below must name this host.
	workspaceSlug, err := workspaces.WorkspaceSlugByAccount(ctx, acc.ID)
	if err != nil || workspaceSlug != wantSlug {
		t.Fatalf("WorkspaceSlugByAccount = %q/%v, want the account's workspace slug %q", workspaceSlug, err, wantSlug)
	}
	if workspaceSlug == synth {
		t.Fatalf("fixture is not discriminating: account slug == workspace slug (%q)", synth)
	}

	form := &dbstore.Form{
		AccountID: acc.ID, Slug: "it-view-form-" + acc.ID[:8], Title: "Liyana-style form",
		Status: dbstore.FormStatusPublished, Config: map[string]any{"blocks": []any{}},
	}
	if err := dbstore.NewFormStore(pool).Create(ctx, form); err != nil {
		t.Fatalf("create form: %v", err)
	}

	h := newBundleHandlers(pool)
	key := bundleAPIKey(t, ctx, pool, acc.ID)
	code, raw := tplReq(t, h, key, http.MethodGet, "/v1/rest/forms", "")
	if code != http.StatusOK {
		t.Fatalf("list forms status = %d, body=%s", code, raw)
	}
	var rows []struct {
		ID   string `json:"id"`
		Slug string `json:"slug"`
		URL  string `json:"url"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("decode forms list: %v (%s)", err, raw)
	}
	var got string
	for _, r := range rows {
		if r.ID == form.ID {
			got = r.URL
		}
	}
	if got == "" {
		t.Fatalf("the forms payload carries url=\"\" for a published form on a signup-created account.\n"+
			"That empty string is what renders as `<a href=\"\">` on /forms — a browser resolves it to the "+
			"CURRENT page, so the \"View\" click reloads /forms instead of opening the form (the report). "+
			"body=%s", raw)
	}
	// A relative or scheme-less value is not a public link either.
	if !strings.HasPrefix(got, "https://") {
		t.Errorf("form url = %q, want an absolute https:// URL", got)
	}
	wantSuffix := "/" + form.Slug
	if !strings.HasSuffix(got, wantSuffix) {
		t.Errorf("form url = %q, want it to end in %q", got, wantSuffix)
	}
	// No custom domain is configured here, so the host must be the WORKSPACE
	// subdomain — the host the dashboard displays (settings/page.tsx renders
	// `https://${activeWorkspaceSlug}.jomform.com`). One host, not two.
	if !strings.Contains(got, "https://"+workspaceSlug+".jomform.com/") {
		t.Errorf("form url = %q, want the workspace host https://%s.jomform.com/%s — the host the "+
			"dashboard shows and the only host the public server serves", got, workspaceSlug, form.Slug)
	}

	// ── the decisive check: the emitted host actually SERVES the form ────────
	// Derived from the url itself rather than from the slug literal, so this
	// cannot pass by comparing two copies of the same string.
	host := strings.TrimPrefix(got, "https://")
	host = host[:strings.Index(host, ".jomform.com")]
	served, err := dbstore.NewFormStore(pool).ByWorkspaceSlug(ctx, host, form.Slug)
	if err != nil {
		t.Fatalf("the URL this payload hands the merchant does not resolve on the serving path "+
			"(ByWorkspaceSlug(%q, %q)): %v", host, form.Slug, err)
	}
	if served.ID != form.ID {
		t.Fatalf("host %q resolved form %s, want %s", host, served.ID, form.ID)
	}

	// ── and the second, invisible host is gone ──────────────────────────────
	// The account slug served this form before this change. It must not now: a
	// workspace has one address, and it is the one the UI shows.
	if _, err := dbstore.NewFormStore(pool).ByWorkspaceSlug(ctx, acc.Slug, form.Slug); err == nil {
		t.Errorf("the ACCOUNT slug host (%s.jomform.com) still serves this form — a workspace must be "+
			"reachable only on its workspace subdomain (%s.jomform.com)", acc.Slug, workspaceSlug)
	}
}
