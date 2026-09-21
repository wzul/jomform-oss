package httpapi

// Issue #98 regression integration test for #77 slug scoping at the HTTP layer:
//   - bare `/f/{slug}` on the apex host (jomform.com) must 404 — a public
//     payment link is reachable only via its workspace subdomain or a custom domain.
//   - a workspace subdomain ({workspace}.jomform.com/{slug}) serves the published form.
//   - the same form slug in two different workspaces resolves per-workspace.
//
// Runs against a real Postgres (JOMFORM_TEST_DATABASE_URL); skipped when unset.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// publicFixture wires a PublicHandlers over the shared test pool and seeds a
// user + workspace + account + published form. Returns the handlers, the workspace
// slug, and the form slug.
func publicFixture(t *testing.T) (*PublicHandlers, string, string) {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)

	// user + workspace + account linked to the workspace.
	var userID, workspaceSlug, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-workspace-' || md5(random()::text), 'it-' || md5(random()::text))
		 RETURNING slug`, userID).Scan(&workspaceSlug); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 SELECT $1, 'it-acc-' || md5(random()::text), 'it-' || md5(random()::text), id
		 FROM workspaces WHERE slug = $2
		 RETURNING id`, userID, workspaceSlug).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	// A published form with a block so it renders via renderBlocks.
	fs := dbstore.NewFormStore(pool)
	formSlug := "it-form-" + accountID[:8]
	f := &dbstore.Form{
		AccountID: accountID, Slug: formSlug, Title: "IT form", Status: "published",
		Config: map[string]any{"blocks": []any{map[string]any{"type": "text", "value": "Hello"}}},
		Theme:  map[string]any{"primary": "#5e6ad2"},
	}
	if err := fs.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}

	h := &PublicHandlers{
		Forms:      fs,
		Accounts:   dbstore.NewAccountStore(pool),
		Workspaces: dbstore.NewWorkspaceStore(pool),
		BaseURL:    "http://localhost:8080",
	}
	return h, workspaceSlug, formSlug
}

// publicServe routes one request through the public mux with the given Host.
func publicServe(t *testing.T, h *PublicHandlers, host, path string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// TestSlugScopingBareApex404: on the apex host (no subdomain, no custom
// domain) a published form is NOT served at /f/{slug} — it 404s (issue #77).
func TestSlugScopingBareApex404(t *testing.T) {
	h, _, formSlug := publicFixture(t)
	rec := publicServe(t, h, "jomform.com", "/f/"+formSlug)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("bare apex /f/%s status = %d, want 404", formSlug, rec.Code)
	}
}

// TestSlugScopingWorkspaceSubdomainServes: the same form IS served on its workspace
// subdomain at /{slug} (no /f/ prefix).
func TestSlugScopingWorkspaceSubdomainServes(t *testing.T) {
	h, workspaceSlug, formSlug := publicFixture(t)
	rec := publicServe(t, h, workspaceSlug+".jomform.com", "/"+formSlug)
	if rec.Code != http.StatusOK {
		t.Fatalf("workspace subdomain /%s status = %d, want 200 (body %s)", formSlug, rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); body == "" {
		t.Fatal("workspace subdomain served an empty body")
	}
}

// TestSlugScopingSameSlugTwoWorkspaces: two workspaces may share the same form slug;
// each workspace subdomain resolves its own form (issue #77 per-workspace uniqueness).
func TestSlugScopingSameSlugTwoWorkspaces(t *testing.T) {
	pool := bundleTestPool(t)
	fs := dbstore.NewFormStore(pool)

	// Seed two independent workspace+account+form pairs sharing one slug.
	const sharedSlug = "it-shared-slug"
	workspaceA, formA := seedWorkspaceForm(t, pool, sharedSlug)
	workspaceB, formB := seedWorkspaceForm(t, pool, sharedSlug)
	if workspaceA == workspaceB {
		t.Fatalf("workspaces must differ, got %q", workspaceA)
	}
	if formA == formB {
		t.Fatalf("forms must differ, got %q", formA)
	}

	h := &PublicHandlers{
		Forms:      fs,
		Accounts:   dbstore.NewAccountStore(pool),
		Workspaces: dbstore.NewWorkspaceStore(pool),
		BaseURL:    "http://localhost:8080",
	}

	// Workspace A serves its own form.
	recA := publicServe(t, h, workspaceA+".jomform.com", "/"+sharedSlug)
	if recA.Code != http.StatusOK {
		t.Fatalf("workspace A status = %d, want 200", recA.Code)
	}
	// Workspace B serves its own form.
	recB := publicServe(t, h, workspaceB+".jomform.com", "/"+sharedSlug)
	if recB.Code != http.StatusOK {
		t.Fatalf("workspace B status = %d, want 200", recB.Code)
	}
	// The two rendered pages must differ (different titles).
	if recA.Body.String() == recB.Body.String() {
		t.Fatal("two workspaces with the same slug rendered identical pages — per-workspace scoping broken")
	}
}

// seedWorkspaceForm creates a user + workspace + account + published form with the
// given slug, returning the workspace slug and form id.
func seedWorkspaceForm(t *testing.T, pool *pgxpool.Pool, slug string) (workspaceSlug, formID string) {
	t.Helper()
	ctx := context.Background()
	var userID, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-workspace-' || md5(random()::text), 'it-' || md5(random()::text))
		 RETURNING slug`, userID).Scan(&workspaceSlug); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 SELECT $1, 'it-acc-' || md5(random()::text), 'it-' || md5(random()::text), id
		 FROM workspaces WHERE slug = $2
		 RETURNING id`, userID, workspaceSlug).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	f := &dbstore.Form{
		AccountID: accountID, Slug: slug, Title: "Form in " + workspaceSlug, Status: "published",
		Config: map[string]any{"blocks": []any{map[string]any{"type": "text", "value": "Hi"}}},
		Theme:  map[string]any{"primary": "#5e6ad2"},
	}
	if err := dbstore.NewFormStore(pool).Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}
	return workspaceSlug, f.ID
}
