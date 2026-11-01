package httpapi

// Card t_0f18e155, requirement 3: what an account-only host DOES must be
// asserted, not left to a join returning nothing.
//
// A workspace must be served on its workspace subdomain and nothing else. The
// account slug is a namespace (a verified custom domain resolves through it —
// #236/#297), NOT a second hostname. Before this change the public serving path
// accepted it as a fallback, so every workspace answered on two subdomains and
// the second one (`<workspace>-<8 hex of the account id>`) was never displayed
// anywhere in the UI.
//
// So this drives the REAL public mux with a real Host header, over a real
// Postgres, and asserts the outcome for each host shape:
//
//   - {workspace}.jomform.com/{slug}          -> 200, and the page is the form
//   - {account-slug}.jomform.com/{slug}       -> 404  (this card's rule)
//   - jomform.com/{slug}                      -> 404  (apex: #77, already law)
//   - www.jomform.com/{slug}                  -> 404  (reserved subdomain)
//
// The account-slug host is the one that changes here. Asserting 404 — rather
// than merely that the lookup returns an error — is what makes the rule visible
// to a merchant, because a 404 is what the browser actually shows.
//
// Skipped without JOMFORM_TEST_DATABASE_URL.
import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

func TestAccountSlugHostNoLongerServesAWorkspace(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)

	// A workspace + its linked account. The account is created the way SIGNUP
	// creates it — an account and NO workspace (#718) — and then linked to its
	// first workspace the way the ASK-FIRST flow links it (CreateForAccount). The
	// account slug is rewritten to the SYNTHETIC production shape so the two names
	// really differ (otherwise this test would compare a string to itself).
	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
	 VALUES ('it-onehost-' || md5(random()::text) || '@example.test', 'it')
	 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	acc := &dbstore.Account{
		OwnerUserID: userID,
		Name:        "it-onehost-store",
		Slug:        "it-onehost",
	}
	if err := dbstore.NewAccountStore(pool).Create(ctx, acc); err != nil {
		t.Fatalf("create account via the signup path: %v", err)
	}
	// Fixture precondition, asserted: the signup insert linked NO workspace — that
	// is #718's ask-first boundary. The host this test serves on comes from the
	// ask-first create below, NOT from signup.
	var wid *string
	if err := pool.QueryRow(ctx, `SELECT workspace_id::text FROM accounts WHERE id=$1`, acc.ID).Scan(&wid); err != nil {
		t.Fatalf("read workspace_id: %v", err)
	}
	if wid != nil {
		t.Fatalf("the signup path linked account %s to workspace %s — a signup creates an account and NO "+
			"workspace (#718); this fixture must exercise the ask-first flow, not a signup-minted workspace",
			acc.ID, *wid)
	}
	workspaces := dbstore.NewWorkspaceStore(pool)
	wantSlug := "it-onehost-ws-" + userID[:8]
	if _, err := workspaces.CreateForAccount(ctx, userID, "it-onehost-ws", wantSlug, acc.ID); err != nil {
		t.Fatalf("ask-first create: %v", err)
	}
	synth := acc.Slug + "-" + acc.ID[:8]
	if _, err := pool.Exec(ctx, `UPDATE accounts SET slug=$2 WHERE id=$1`, acc.ID, synth); err != nil {
		t.Fatalf("rewrite account slug to the synthetic production shape: %v", err)
	}

	workspaceSlug, err := workspaces.PublicHostSlugByAccount(ctx, acc.ID)
	if err != nil || workspaceSlug != wantSlug {
		t.Fatalf("PublicHostSlugByAccount = %q/%v, want the workspace slug %q", workspaceSlug, err, wantSlug)
	}
	if workspaceSlug == synth {
		t.Fatalf("fixture is not discriminating: account slug == workspace slug (%q)", synth)
	}

	formSlug := "it-onehost-form-" + acc.ID[:8]
	form := &dbstore.Form{
		AccountID: acc.ID, Slug: formSlug, Title: "One host only", Status: dbstore.FormStatusPublished,
		Config: map[string]any{"blocks": []any{map[string]any{"type": "text", "value": "Hello"}}},
		Theme:  map[string]any{"primary": "#5e6ad2"},
	}
	if err := dbstore.NewFormStore(pool).Create(ctx, form); err != nil {
		t.Fatalf("create form: %v", err)
	}

	h := &PublicHandlers{
		Forms:      dbstore.NewFormStore(pool),
		Accounts:   dbstore.NewAccountStore(pool),
		Workspaces: dbstore.NewWorkspaceStore(pool),
		BaseURL:    "http://localhost:8080",
	}

	// ── the ONE host that serves ────────────────────────────────────────────
	rec := publicServe(t, h, workspaceSlug+".jomform.com", "/"+formSlug)
	if rec.Code != http.StatusOK {
		t.Fatalf("workspace subdomain %s.jomform.com/%s status = %d, want 200 (body %s) — the "+
			"workspace must keep serving on its own subdomain",
			workspaceSlug, formSlug, rec.Code, rec.Body.String())
	}

	// ── the account-slug host: the rule this card enforces ──────────────────
	// It answered 200 before this change. A merchant reaching it now gets a 404
	// page, which is the visible half of "one host per workspace".
	rec = publicServe(t, h, synth+".jomform.com", "/"+formSlug)
	if rec.Code != http.StatusNotFound {
		t.Errorf("%s.jomform.com/%s status = %d, want 404 — this is the ACCOUNT-slug host that used to "+
			"serve the same form. A workspace must be served on its workspace subdomain (%s.jomform.com) "+
			"and nothing else; the account slug is the namespace a verified custom domain resolves "+
			"through, not a hostname", synth, formSlug, rec.Code, workspaceSlug)
	}

	// ── the reserved / apex host shapes stay 404 ────────────────────────────
	// accountSubdomain returns "" for these, so the request never reaches a
	// workspace lookup. Pinned here so a future "make it serve on the apex"
	// change cannot pass silently.
	for _, host := range []string{"jomform.com", "www.jomform.com", "app.jomform.com", "api.jomform.com"} {
		rec := publicServe(t, h, host, "/"+formSlug)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s/%s status = %d, want 404 — reserved and apex hosts never serve a workspace form",
				host, formSlug, rec.Code)
		}
	}

	// ── THE PRODUCTION SHAPE: the apex IS APP_BASE_URL's host ───────────────
	// THIS IS THE ASSERTION THAT WAS MISSING. The handler above carries
	// BaseURL http://localhost:8080, so an apex request never matches "this
	// install's own origin" and every assertion above is vacuous for the shape
	// the hosted product actually runs: deploy/vps/compose.prod.yml sets
	// APP_BASE_URL to the product's own apex. Measured (card t_4ddab217): with
	// BASE_URL = https://jomform.com, /f/<formSlug> answered 200 and rendered
	// the merchant's form — a slug exactly ONE workspace holds resolved across
	// the install. Serving from the install's own origin is therefore an
	// EXPLICIT, default-OFF switch (JOMFORM_SELF_HOSTED), never an inference
	// from host equality: on the hosted product the apex is that origin.
	prod := &PublicHandlers{
		Forms:      dbstore.NewFormStore(pool),
		Accounts:   dbstore.NewAccountStore(pool),
		Workspaces: dbstore.NewWorkspaceStore(pool),
		BaseURL:    "https://jomform.com",
	}
	for _, path := range []string{"/f/" + formSlug, "/" + formSlug} {
		for _, host := range []string{"jomform.com", "jomform.com:443", "www.jomform.com"} {
			rec := publicServe(t, prod, host, path)
			if rec.Code != http.StatusNotFound {
				t.Errorf("%s%s (APP_BASE_URL=https://jomform.com) status = %d, want 404 — on the "+
					"hosted product the apex IS the install's own origin, and it serves no form "+
					"(issue #77). Serving it needs the explicit self-host switch, not host equality "+
					"(body %.120s)", host, path, rec.Code, rec.Body.String())
			}
		}
	}
	// The submit path agrees: a page that renders while its POST 404s is worse
	// than one that never rendered, so the resolver is asserted directly.
	probe := httptest.NewRequest(http.MethodPost, "/f/"+formSlug+"/order", nil)
	probe.Host = "jomform.com"
	if f := prod.findPublicForm(ctx, probe, formSlug); f != nil {
		t.Errorf("findPublicForm resolved %q from the hosted apex (id %s) — the submit path must "+
			"refuse it exactly like the page does", formSlug, f.ID)
	}
}
