package payments

// Regression for the BUYER-FACING money path: the receipt's "view your order"
// link — now guarding the RULE the fix ended on (one host per workspace).
//
// `formPublicURL` builds the URL a buyer is told to revisit (receipt email +
// receipt PDF, notifications.go:196 / pdf.go:53). It resolved the host through
// WorkspaceStore.WorkspaceSlugByAccount — a join on accounts.workspace_id — and
// that column was NULL for every account the SIGNUP path creates
// (AccountStore.Create did not set it). The error was swallowed, the base came
// back "", and the caller fell back to the APEX thank-you page. So a paying
// merchant's customers were pointed at the wrong place — for every merchant who
// signed up normally, not for an edge case.
//
// The first fix gave the resolver an account-slug fallback, which made the
// receipt agree with a serving path that accepted two hostnames. The fix now is
// to remove the second hostname outright: the receipt names the workspace host,
// the one the dashboard displays, and that host is the only one the server
// serves. The workspace comes from the user's ask-first create, not from signup
// (#718: a signup creates an account and no workspace), which is why this
// fixture links one the way the ask-first flow does.
//
// This asserts formPublicURL DIRECTLY (not the rendered email template): an
// empty string here is the defect, and it is what makes the fallback fire.
//
// Skipped without JOMFORM_TEST_DATABASE_URL.
import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

func TestReceiptFormURLForASignupCreatedAccount(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping receipt form-url test")
	}
	ctx := context.Background()
	pool := testdb.Open(t)

	// A user + account created through the SIGNUP path, which links NO workspace
	// (#718: a signup creates an account and no workspace, so the user is ASKED
	// to create one)...
	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-receipt-' || md5(random()::text) || '@example.test', 'it') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	acc := &dbstore.Account{
		OwnerUserID: userID,
		Name:        "it-receipt-store",
		Slug:        "it-receipt-" + userID[:8],
	}
	if err := dbstore.NewAccountStore(pool).Create(ctx, acc); err != nil {
		t.Fatalf("create account via the signup path: %v", err)
	}
	// Fixture precondition, asserted: signup linked no workspace. That is #718's
	// boundary, and it is why the workspace has to come from the ask-first
	// create below rather than from the signup insert.
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
	// (WorkspaceStore.CreateForAccount, rest_workspaces.go / MCP create_workspace).
	workspaces := dbstore.NewWorkspaceStore(pool)
	workspaceSlug := "it-receipt-ws-" + userID[:8]
	if _, err := workspaces.CreateForAccount(ctx, userID, "it-receipt-ws", workspaceSlug, acc.ID); err != nil {
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
	readback, err := workspaces.WorkspaceSlugByAccount(ctx, acc.ID)
	if err != nil || readback != workspaceSlug {
		t.Fatalf("WorkspaceSlugByAccount = %q/%v, want the account's workspace slug %q", readback, err, workspaceSlug)
	}
	if workspaceSlug == synth {
		t.Fatalf("fixture is not discriminating: account slug == workspace slug (%q)", synth)
	}

	forms := dbstore.NewFormStore(pool)
	form := &dbstore.Form{
		AccountID: acc.ID, Slug: "it-receipt-form-" + acc.ID[:8], Title: "Paid form",
		Status: dbstore.FormStatusPublished, Config: map[string]any{"blocks": []any{}},
	}
	if err := forms.Create(ctx, form); err != nil {
		t.Fatalf("create form: %v", err)
	}

	svc := &Service{
		Accounts:   dbstore.NewAccountStore(pool),
		Forms:      forms,
		Workspaces: workspaces,
	}

	formURL := svc.formPublicURL(ctx, acc.ID, form.ID)
	if formURL == "" {
		t.Fatalf("formPublicURL returned \"\" for a paid form on a signup-created account.\n"+
			"That empty result is what makes the receipt fall back to the APEX thank-you page, so a paying "+
			"buyer is told to revisit the wrong place (account %s, form %s).", acc.ID, form.Slug)
	}
	if !strings.HasPrefix(formURL, "https://") {
		t.Errorf("receipt form url = %q, want an absolute https:// URL", formURL)
	}
	if !strings.Contains(formURL, "https://"+workspaceSlug+".jomform.com/") {
		t.Errorf("receipt form url = %q, want the WORKSPACE host https://%s.jomform.com/%s — the host "+
			"the dashboard shows and the only host the public server serves", formURL, workspaceSlug, form.Slug)
	}
	if !strings.HasSuffix(formURL, "/"+form.Slug) {
		t.Errorf("receipt form url = %q, want it to end in /%s", formURL, form.Slug)
	}

	// The decisive check: the host in the receipt actually serves the form,
	// verified through the serving path's own lookup rather than a copied slug.
	host := strings.TrimPrefix(formURL, "https://")
	host = host[:strings.Index(host, ".jomform.com")]
	served, err := forms.ByWorkspaceSlug(ctx, host, form.Slug)
	if err != nil {
		t.Fatalf("the URL a buyer's receipt points at does not resolve on the serving path "+
			"(ByWorkspaceSlug(%q, %q)): %v", host, form.Slug, err)
	}
	if served.ID != form.ID {
		t.Fatalf("host %q resolved form %s, want %s", host, served.ID, form.ID)
	}

	// And the invisible second host is gone: the account slug served this form
	// before the change, when a workspace answered on two addresses.
	if _, err := forms.ByWorkspaceSlug(ctx, acc.Slug, form.Slug); err == nil {
		t.Errorf("the ACCOUNT slug host (%s.jomform.com) still serves this form — a workspace must be "+
			"reachable only on its workspace subdomain (%s.jomform.com)", acc.Slug, workspaceSlug)
	}

	// And the same host must be what the receipt's OTHER surface uses — the
	// thank-you base (purchases.go) is built by workspaceBaseURL, so a paid
	// buyer's redirect lands on the same served host.
	if base := svc.workspaceBaseURL(ctx, acc.ID); base == "" {
		t.Errorf("workspaceBaseURL returned \"\" — the checkout's success/failure redirect falls back to " +
			"the apex base, landing the buyer outside their merchant's storefront")
	} else if !strings.Contains(formURL, base) {
		t.Errorf("receipt url %q is not under the thank-you base %q — the two buyer-facing surfaces disagree",
			formURL, base)
	}
}
