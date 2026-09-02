package httpapi

// Issue #98 regression integration test for #77 slug scoping at the HTTP layer:
//   - bare `/f/{slug}` on the apex host (jomform.com) must 404 — a public
//     payment link is reachable only via its space subdomain or a custom domain.
//   - a space subdomain ({space}.jomform.com/{slug}) serves the published form.
//   - the same form slug in two different spaces resolves per-space.
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
// user + space + account + published form. Returns the handlers, the space
// slug, and the form slug.
func publicFixture(t *testing.T) (*PublicHandlers, string, string) {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)

	// user + space + account linked to the space.
	var userID, spaceSlug, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO spaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-space-' || md5(random()::text), 'it-' || md5(random()::text))
		 RETURNING slug`, userID).Scan(&spaceSlug); err != nil {
		t.Fatalf("create space: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, space_id)
		 SELECT $1, 'it-acc-' || md5(random()::text), 'it-' || md5(random()::text), id
		 FROM spaces WHERE slug = $2
		 RETURNING id`, userID, spaceSlug).Scan(&accountID); err != nil {
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
		Forms:    fs,
		Accounts: dbstore.NewAccountStore(pool),
		Spaces:   dbstore.NewSpaceStore(pool),
		BaseURL:  "http://localhost:8080",
	}
	return h, spaceSlug, formSlug
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

// TestSlugScopingSpaceSubdomainServes: the same form IS served on its space
// subdomain at /{slug} (no /f/ prefix).
func TestSlugScopingSpaceSubdomainServes(t *testing.T) {
	h, spaceSlug, formSlug := publicFixture(t)
	rec := publicServe(t, h, spaceSlug+".jomform.com", "/"+formSlug)
	if rec.Code != http.StatusOK {
		t.Fatalf("space subdomain /%s status = %d, want 200 (body %s)", formSlug, rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); body == "" {
		t.Fatal("space subdomain served an empty body")
	}
}

// TestSlugScopingSameSlugTwoSpaces: two spaces may share the same form slug;
// each space subdomain resolves its own form (issue #77 per-space uniqueness).
func TestSlugScopingSameSlugTwoSpaces(t *testing.T) {
	pool := bundleTestPool(t)
	fs := dbstore.NewFormStore(pool)

	// Seed two independent space+account+form pairs sharing one slug.
	const sharedSlug = "it-shared-slug"
	spaceA, formA := seedSpaceForm(t, pool, sharedSlug)
	spaceB, formB := seedSpaceForm(t, pool, sharedSlug)
	if spaceA == spaceB {
		t.Fatalf("spaces must differ, got %q", spaceA)
	}
	if formA == formB {
		t.Fatalf("forms must differ, got %q", formA)
	}

	h := &PublicHandlers{
		Forms:    fs,
		Accounts: dbstore.NewAccountStore(pool),
		Spaces:   dbstore.NewSpaceStore(pool),
		BaseURL:  "http://localhost:8080",
	}

	// Space A serves its own form.
	recA := publicServe(t, h, spaceA+".jomform.com", "/"+sharedSlug)
	if recA.Code != http.StatusOK {
		t.Fatalf("space A status = %d, want 200", recA.Code)
	}
	// Space B serves its own form.
	recB := publicServe(t, h, spaceB+".jomform.com", "/"+sharedSlug)
	if recB.Code != http.StatusOK {
		t.Fatalf("space B status = %d, want 200", recB.Code)
	}
	// The two rendered pages must differ (different titles).
	if recA.Body.String() == recB.Body.String() {
		t.Fatal("two spaces with the same slug rendered identical pages — per-space scoping broken")
	}
}

// seedSpaceForm creates a user + space + account + published form with the
// given slug, returning the space slug and form id.
func seedSpaceForm(t *testing.T, pool *pgxpool.Pool, slug string) (spaceSlug, formID string) {
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
		`INSERT INTO spaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-space-' || md5(random()::text), 'it-' || md5(random()::text))
		 RETURNING slug`, userID).Scan(&spaceSlug); err != nil {
		t.Fatalf("create space: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, space_id)
		 SELECT $1, 'it-acc-' || md5(random()::text), 'it-' || md5(random()::text), id
		 FROM spaces WHERE slug = $2
		 RETURNING id`, userID, spaceSlug).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	f := &dbstore.Form{
		AccountID: accountID, Slug: slug, Title: "Form in " + spaceSlug, Status: "published",
		Config: map[string]any{"blocks": []any{map[string]any{"type": "text", "value": "Hi"}}},
		Theme:  map[string]any{"primary": "#5e6ad2"},
	}
	if err := dbstore.NewFormStore(pool).Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}
	return spaceSlug, f.ID
}
