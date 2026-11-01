package httpapi

// A SELF-HOSTED install must be able to serve the forms it publishes.
//
// Public form addressing is host-scoped by design (issue #77/#297): a hosted
// tenant is served on <workspace>.jomform.com, and the apex serves none. A
// self-hosted install has no such domain — it has ONE hostname for everyone — so
// before this change every published form answered 404 on the only origin the
// operator had. Measured on a clean one-command install (card t_28a06494):
// register → verify → create workspace → create form → publish all succeed, the
// dashboard prints the form as live, and GET /f/<slug> answers 404 "Borang tidak
// dijumpai" — the product cannot take a single response.
//
// This drives the REAL public mux with a real Host header over a real Postgres,
// so it pins behaviour a browser sees, and it asserts BOTH directions:
//
//   - the install's own origin serves its published form (200) — but ONLY
//     because the handler declares SelfHosted, which is what JOMFORM_SELF_HOSTED
//     sets. With the flag off, the very same host answers 404: host equality
//     with APP_BASE_URL is not evidence of a self-hosted install, because the
//     hosted product's APP_BASE_URL is its own apex (issue #77, and the defect
//     this flag was introduced for — card t_4ddab217);
//   - the SAME origin does NOT serve a slug two workspaces both hold (404) —
//     form slugs are unique per workspace (migration 000019), so serving either
//     would put one merchant's form in front of another's buyer;
//   - a host that is NOT the install's origin still gets nothing (404), even
//     with the flag on;
//   - a workspace subdomain keeps serving as before (200).
//
// Skipped without JOMFORM_TEST_DATABASE_URL, like its siblings.
import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

func TestSelfHostedOriginServesItsOwnForms(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)

	forms := dbstore.NewFormStore(pool)
	accounts := dbstore.NewAccountStore(pool)
	workspaces := dbstore.NewWorkspaceStore(pool)

	// ── workspace A, with a published form ──────────────────────────────────
	mkUser := func(tag string) string {
		var userID string
		if err := pool.QueryRow(ctx,
			`INSERT INTO users (email, password_hash)
			 VALUES ('it-selfhost-' || $1 || '-' || md5(random()::text) || '@example.test', 'it')
			 RETURNING id`, tag).Scan(&userID); err != nil {
			t.Fatalf("create user %s: %v", tag, err)
		}
		return userID
	}
	mkWS := func(userID, name, slug string) *dbstore.Account {
		acc := &dbstore.Account{OwnerUserID: userID, Name: name, Slug: slug}
		if err := accounts.Create(ctx, acc); err != nil {
			t.Fatalf("create account %s: %v", slug, err)
		}
		if _, err := workspaces.CreateForAccount(ctx, userID, name, slug, acc.ID); err != nil {
			t.Fatalf("create workspace %s: %v", slug, err)
		}
		return acc
	}

	userA := mkUser("a")
	accA := mkWS(userA, "Selfhost A", "it-sh-a-"+userA[:6])
	slugA := "it-sh-form-" + userA[:8]
	if err := forms.Create(ctx, &dbstore.Form{
		AccountID: accA.ID, Slug: slugA, Title: "Self-host form", Status: dbstore.FormStatusPublished,
		Config: map[string]any{"blocks": []any{map[string]any{"type": "text", "value": "Hello"}}},
		Theme:  map[string]any{"primary": "#5e6ad2"},
	}); err != nil {
		t.Fatalf("create form: %v", err)
	}

	// The install's own origin. This is the whole point: one host, no tenant
	// subdomain, no custom domain.
	h := &PublicHandlers{
		Forms:      forms,
		Accounts:   accounts,
		Workspaces: workspaces,
		BaseURL:    "http://localhost:18080",
		// The DECLARATION: this install is the origin its buyers load
		// (JOMFORM_SELF_HOSTED, written by deploy/selfhost/bootstrap.sh).
		SelfHosted: true,
	}

	// ── the flag is what turns this on, not host equality ───────────────────
	// The same request against a handler that does NOT declare itself
	// self-hosted must 404. Without this direction the flag could be deleted
	// and the suite would stay green while the hosted apex served forms
	// (measured on PR #1516: jomform.com/f/<slug> answered 200).
	noFlag := &PublicHandlers{
		Forms: forms, Accounts: accounts, Workspaces: workspaces,
		BaseURL: "http://localhost:18080",
	}
	rec := publicServe(t, noFlag, "localhost:18080", "/f/"+slugA)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /f/%s on the install's own origin with SelfHosted OFF = %d, want 404 — "+
			"serving from the own origin must be an explicit mode: on the hosted product that "+
			"host is the apex and serves no form (issue #77)", slugA, rec.Code)
	}

	rec = publicServe(t, h, "localhost:18080", "/f/"+slugA)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /f/%s on the install's own origin (localhost:18080) = %d, want 200 — a "+
			"self-hosted install must serve the forms it publishes (body %s)",
			slugA, rec.Code, rec.Body.String())
	}
	if !strings.Contains(strings.ToLower(rec.Body.String()), "self-host form") {
		t.Fatalf("the served page is not the published form: %s", rec.Body.String())
	}

	// The port is not part of the tenant's identity: APP_BASE_URL may carry one
	// and a probe may not.
	rec = publicServe(t, h, "localhost", "/f/"+slugA)
	if rec.Code != http.StatusOK {
		t.Fatalf("the same origin without the port = %d, want 200", rec.Code)
	}

	// ── the SUBMIT resolver agrees ──────────────────────────────────────────
	// GET /f/{slug} renders through handleRender, but every write (POST
	// /f/{slug}/order, /unlock) resolves through findPublicForm. A page that
	// renders while its submit 404s is worse than one that never rendered, so
	// the resolver is asserted directly rather than inferred from the page.
	probe := httptest.NewRequest(http.MethodGet, "/f/"+slugA, nil)
	probe.Host = "localhost:18080"
	if f := h.findPublicForm(ctx, probe, slugA); f == nil || f.ID == "" {
		t.Fatalf("findPublicForm (the submit/unlock resolver) returned no form on the install's own " +
			"origin — the page would render and the submission would 404")
	}

	// ── a DIFFERENT host gets nothing ───────────────────────────────────────
	// This is the boundary that keeps the hosted product unchanged: only the
	// deployment's own origin may answer this way.
	for _, host := range []string{"jomform.com", "www.jomform.com", "other.example.com"} {
		rec := publicServe(t, h, host, "/f/"+slugA)
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET /f/%s on host %q = %d, want 404 — only the install's own origin may serve "+
				"an unscoped form", slugA, host, rec.Code)
		}
	}

	// ── a workspace subdomain still serves the way it always did ────────────
	wsSlug, err := workspaces.PublicHostSlugByAccount(ctx, accA.ID)
	if err != nil || wsSlug == "" {
		t.Fatalf("PublicHostSlugByAccount: %q/%v", wsSlug, err)
	}
	rec = publicServe(t, h, wsSlug+".jomform.com", "/f/"+slugA)
	if rec.Code != http.StatusOK {
		t.Errorf("%s.jomform.com/f/%s = %d, want 200 — the subdomain path must not regress",
			wsSlug, slugA, rec.Code)
	}

	// ── AMBIGUITY IS A MISS ─────────────────────────────────────────────────
	// A second workspace reusing the SAME slug. Both are published; the install
	// origin cannot know which one the buyer meant, so it must refuse rather
	// than pick one.
	userB := mkUser("b")
	accB := mkWS(userB, "Selfhost B", "it-sh-b-"+userB[:6])
	if err := forms.Create(ctx, &dbstore.Form{
		AccountID: accB.ID, Slug: slugA, Title: "Someone else's form", Status: dbstore.FormStatusPublished,
		Config: map[string]any{"blocks": []any{map[string]any{"type": "text", "value": "Hi"}}},
		Theme:  map[string]any{"primary": "#111111"},
	}); err != nil {
		t.Fatalf("create the colliding form: %v", err)
	}
	rec = publicServe(t, h, "localhost:18080", "/f/"+slugA)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /f/%s with TWO workspaces holding that slug = %d, want 404 — serving either "+
			"would show one merchant's form to another's buyer (body %s)",
			slugA, rec.Code, rec.Body.String())
	}

	// ── a slug nobody owns is still a 404 ───────────────────────────────────
	rec = publicServe(t, h, "localhost:18080", "/f/no-such-form-"+userA[:8])
	if rec.Code != http.StatusNotFound {
		t.Fatalf("an unknown slug = %d, want 404", rec.Code)
	}

	// ── and a self-hosted deployment with no APP_BASE_URL never guesses ─────
	noBase := &PublicHandlers{Forms: forms, Accounts: accounts, Workspaces: workspaces, SelfHosted: true}
	rec = publicServe(t, noBase, "localhost:18080", "/f/"+slugA)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("with no BaseURL a form must not be served unscoped (got %d)", rec.Code)
	}
}
