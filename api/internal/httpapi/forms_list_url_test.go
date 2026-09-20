package httpapi

// Regression for the /forms "View" link (Wan's report: clicking it loaded the
// /forms page again instead of opening the form).
//
// THE BUG, end to end: GET /v1/rest/forms fills each row's `url` through
// formURLForAccount, which fell back to formURL(workspaceSlugFor(account)) — and
// that resolved the slug with WorkspaceSlugByAccount, a join on
// accounts.workspace_id. AccountStore.Create (the insert the SIGNUP path uses)
// never sets that column, so it is NULL for every signup-created merchant
// account, the join returned no row, the error was swallowed and the payload
// carried url: "". The dashboard then rendered `<a href="">`, which the browser
// resolves to the CURRENT document — with target="_blank", /forms opened again
// in a new tab. Exactly the report.
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
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

func TestFormsListURLIsServedForASignupCreatedAccount(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)

	// ── an account created through the SIGNUP path (no workspace_id) ────────
	var userID, accountID, accountSlug string
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
	accountID, accountSlug = acc.ID, acc.Slug
	// Fixture precondition, asserted: this really is the NULL-workspace_id shape
	// the defect is about, and the pre-fix resolver really does answer "".
	var wid *string
	if err := pool.QueryRow(ctx, `SELECT workspace_id::text FROM accounts WHERE id=$1`, accountID).Scan(&wid); err != nil {
		t.Fatalf("read workspace_id: %v", err)
	}
	if wid != nil {
		t.Fatalf("fixture is not discriminating: signup-created account %s has workspace_id=%s", accountID, *wid)
	}
	if slug, err := dbstore.NewWorkspaceStore(pool).WorkspaceSlugByAccount(ctx, accountID); err == nil || slug != "" {
		t.Fatalf("the workspace-only resolver answered slug=%q err=%v — the premise of this regression test "+
			"no longer holds, so a green run would prove nothing", slug, err)
	}

	form := &dbstore.Form{
		AccountID: accountID, Slug: "it-view-form-" + accountID[:8], Title: "Liyana-style form",
		Status: dbstore.FormStatusPublished, Config: map[string]any{"blocks": []any{}},
	}
	if err := dbstore.NewFormStore(pool).Create(ctx, form); err != nil {
		t.Fatalf("create form: %v", err)
	}

	h := newBundleHandlers(pool)
	key := bundleAPIKey(t, ctx, pool, accountID)
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
	// No custom domain is configured here, so the host must be the account's
	// subdomain — the host the pre-fix code could not produce.
	if !strings.Contains(got, "https://"+accountSlug+".jomform.com/") {
		t.Errorf("form url = %q, want the account-slug host https://%s.jomform.com/%s (no workspace is "+
			"linked, and that is the host the public request path serves)", got, accountSlug, form.Slug)
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
	_ = fmt.Sprint // keep fmt imported if the assertions above change
}
