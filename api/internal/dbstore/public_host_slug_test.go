package dbstore

// Regression: the public form URL must resolve to a host that actually SERVES
// the form, for every account — including the ones the signup path creates.
//
// THE BUG: form URLs were built from WorkspaceSlugByAccount, which joins
// `accounts a JOIN workspaces sp ON sp.id = a.workspace_id`. AccountStore.Create
// — the insert the signup path uses (auth.go register, social.go) — does not set
// workspace_id, so that column is NULL for every merchant account created by
// signup and the join returned no row. Every caller swallowed the error and
// answered "": the dashboard's "View" action rendered `href=""` (so the click
// reloaded /forms), MCP list_forms returned an empty url, and a buyer's receipt
// fell back to the apex thank-you page.
//
// The serving path never had that constraint: FormStore.ByWorkspaceSlug (reached
// from accountSubdomain + handleWorkspaceRootForm) resolves by ACCOUNT slug
// first and falls back to the workspace join. So the two agreed only by luck —
// and disagreed for every account the signup path created.
//
// PublicHostSlugByAccount resolves the way the server does: workspace slug
// first, account slug second. This test pins that, and pins the two properties
// that make it the right fix rather than a guess:
//
//   1. it never answers "" for an account that exists (the pre-fix answer), and
//   2. the slug it returns really resolves the form through the serving path's
//      own lookup — asserted by CALLING ByWorkspaceSlug, not by comparing
//      strings to a literal.
//
// Skipped without JOMFORM_TEST_DATABASE_URL.
import (
	"context"
	"testing"
)

// signupAccount creates an account the way the SIGNUP path does — via
// AccountStore.Create, which sets no workspace_id — and returns its id + slug.
func signupAccount(t *testing.T, ctx context.Context, slugPrefix string) (accountID, accountSlug string) {
	t.Helper()
	pool := testPool(t)
	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-pub-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	acc := &Account{
		OwnerUserID: userID,
		Name:        "it-signup-store",
		Slug:        slugPrefix + "-" + userID[:8],
	}
	if err := NewAccountStore(pool).Create(ctx, acc); err != nil {
		t.Fatalf("create account via the signup path: %v", err)
	}
	// Fixture precondition, asserted rather than assumed: this account really is
	// the NULL-workspace_id shape the defect is about. If a future change makes
	// the signup insert link a workspace, a green run here proves nothing and
	// this test must be re-cut against the new shape.
	var wid *string
	if err := pool.QueryRow(ctx, `SELECT workspace_id::text FROM accounts WHERE id=$1`, acc.ID).Scan(&wid); err != nil {
		t.Fatalf("read workspace_id: %v", err)
	}
	if wid != nil {
		t.Fatalf("fixture is not discriminating: signup-created account %s has workspace_id=%s", acc.ID, *wid)
	}
	return acc.ID, acc.Slug
}

func TestPublicHostSlugResolvesForASignupCreatedAccount(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	ws := NewWorkspaceStore(pool)
	fs := NewFormStore(pool)

	accountID, accountSlug := signupAccount(t, ctx, "it-signup")

	// The pre-fix resolution: it MUST still fail to resolve, or this test is not
	// measuring the regression. Asserted, not assumed.
	if slug, err := ws.WorkspaceSlugByAccount(ctx, accountID); err == nil || slug != "" {
		t.Fatalf("the workspace-only resolver answered slug=%q err=%v for a NULL-workspace_id account; "+
			"the premise of this regression test no longer holds", slug, err)
	}

	slug, err := ws.PublicHostSlugByAccount(ctx, accountID)
	if err != nil {
		t.Fatalf("PublicHostSlugByAccount: %v", err)
	}
	if slug == "" {
		t.Fatalf("PublicHostSlugByAccount returned \"\" for account %s — a form URL built from this is "+
			"empty, which renders href=\"\" on the dashboard and points a buyer's receipt at the apex "+
			"thank-you page", accountID)
	}
	if slug != accountSlug {
		t.Errorf("PublicHostSlugByAccount = %q, want the account slug %q (no workspace is linked, so the "+
			"account slug is the only host there is)", slug, accountSlug)
	}

	// The decisive property: the slug resolves the form through the SERVING
	// path's own lookup. A slug that merely looks plausible would 404 for the
	// merchant; this is what makes the URL a real one.
	form := &Form{AccountID: accountID, Slug: "it-pub-form-" + accountID[:8], Title: "Signup form", Status: FormStatusPublished}
	if err := fs.Create(ctx, form); err != nil {
		t.Fatalf("create form: %v", err)
	}
	served, err := fs.ByWorkspaceSlug(ctx, slug, form.Slug)
	if err != nil {
		t.Fatalf("ByWorkspaceSlug(%q, %q) — the host this resolver emits does not serve the form: %v",
			slug, form.Slug, err)
	}
	if served.ID != form.ID {
		t.Fatalf("ByWorkspaceSlug resolved form %s, want %s", served.ID, form.ID)
	}
}

func TestPublicHostSlugPrefersTheWorkspaceSlugWhenLinked(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	ws := NewWorkspaceStore(pool)

	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-pub-ws-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	// WorkspaceStore.Create makes the workspace AND a linked account (the
	// explicit "new workspace" path, rest_workspaces.go).
	workspace, err := ws.Create(ctx, userID, "it-pub-workspace", "it-pub-ws-"+userID[:8])
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	acc, err := NewAccountStore(pool).ByWorkspaceID(ctx, workspace.ID)
	if err != nil {
		t.Fatalf("account for workspace: %v", err)
	}

	slug, err := ws.PublicHostSlugByAccount(ctx, acc.ID)
	if err != nil {
		t.Fatalf("PublicHostSlugByAccount: %v", err)
	}
	if slug != workspace.Slug {
		t.Errorf("PublicHostSlugByAccount = %q, want the WORKSPACE slug %q — a merchant who named a "+
			"workspace keeps that host; the account slug is only the fallback", slug, workspace.Slug)
	}
	// Both hosts are served today; the workspace one is the preference.
	if got, err := ws.WorkspaceSlugByAccount(ctx, acc.ID); err != nil || got != workspace.Slug {
		t.Fatalf("WorkspaceSlugByAccount = %q/%v, want %q — the linked case must be unchanged",
			got, err, workspace.Slug)
	}
}
