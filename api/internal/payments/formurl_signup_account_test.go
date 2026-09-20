package payments

// Regression for the BUYER-FACING money path: the receipt's "view your order"
// link.
//
// `formPublicURL` builds the URL a buyer is told to revisit (receipt email +
// receipt PDF, notifications.go:196 / pdf.go:53). It resolved the host through
// WorkspaceStore.WorkspaceSlugByAccount — a join on accounts.workspace_id — and
// that column is NULL for every account the SIGNUP path creates
// (AccountStore.Create does not set it). The error was swallowed, the base came
// back "", and the caller fell back to the APEX thank-you page. So a paying
// merchant's customers were pointed at the wrong place — for every merchant who
// signed up normally, not for an edge case.
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

	// A user + account created through the SIGNUP path (no workspace_id).
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
	// Fixture preconditions, asserted: the NULL workspace_id shape and the
	// pre-fix resolver really answering "". Without them a green run proves
	// nothing about the defect.
	var wid *string
	if err := pool.QueryRow(ctx, `SELECT workspace_id::text FROM accounts WHERE id=$1`, acc.ID).Scan(&wid); err != nil {
		t.Fatalf("read workspace_id: %v", err)
	}
	if wid != nil {
		t.Fatalf("fixture is not discriminating: signup-created account %s has workspace_id=%s", acc.ID, *wid)
	}
	workspaces := dbstore.NewWorkspaceStore(pool)
	if slug, err := workspaces.WorkspaceSlugByAccount(ctx, acc.ID); err == nil || slug != "" {
		t.Fatalf("the workspace-only resolver answered slug=%q err=%v — the premise of this regression "+
			"test no longer holds", slug, err)
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

	got := svc.formPublicURL(ctx, acc.ID, form.ID)
	if got == "" {
		t.Fatalf("formPublicURL returned \"\" for a paid form on a signup-created account.\n"+
			"That empty result is what makes the receipt fall back to the APEX thank-you page, so a paying "+
			"buyer is told to revisit the wrong place (account %s, form %s).", acc.ID, form.Slug)
	}
	if !strings.HasPrefix(got, "https://") {
		t.Errorf("receipt form url = %q, want an absolute https:// URL", got)
	}
	if !strings.Contains(got, "https://"+acc.Slug+".jomform.com/") {
		t.Errorf("receipt form url = %q, want the account-slug host https://%s.jomform.com/ (no workspace "+
			"is linked, and that host is the one the public request path serves)", got, acc.Slug)
	}
	if !strings.HasSuffix(got, "/"+form.Slug) {
		t.Errorf("receipt form url = %q, want it to end in /%s", got, form.Slug)
	}

	// The decisive check: the host in the receipt actually serves the form,
	// verified through the serving path's own lookup rather than a copied slug.
	host := strings.TrimPrefix(got, "https://")
	host = host[:strings.Index(host, ".jomform.com")]
	served, err := forms.ByWorkspaceSlug(ctx, host, form.Slug)
	if err != nil {
		t.Fatalf("the URL a buyer's receipt points at does not resolve on the serving path "+
			"(ByWorkspaceSlug(%q, %q)): %v", host, form.Slug, err)
	}
	if served.ID != form.ID {
		t.Fatalf("host %q resolved form %s, want %s", host, served.ID, form.ID)
	}

	// And the same host must be what the receipt's OTHER surface uses — the
	// thank-you base (purchases.go) is built by workspaceBaseURL, so a paid
	// buyer's redirect lands on the same served host.
	if base := svc.workspaceBaseURL(ctx, acc.ID); base == "" {
		t.Errorf("workspaceBaseURL returned \"\" — the checkout's success/failure redirect falls back to "+
			"the apex base, landing the buyer outside their merchant's storefront")
	} else if !strings.Contains(got, base) {
		t.Errorf("receipt url %q is not under the thank-you base %q — the two buyer-facing surfaces disagree",
			got, base)
	}
}
