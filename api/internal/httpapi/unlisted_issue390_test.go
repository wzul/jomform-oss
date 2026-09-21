package httpapi

// Issue #390: an 'unlisted' form is live at its direct /{slug} URL (served,
// submissions work) but is excluded from the workspace storefront / published
// listing. draft/private/published behavior is unchanged.
//
// Runs against a real Postgres (JOMFORM_TEST_DATABASE_URL); skipped when unset.

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// unlistedFixture seeds a workspace with one published form and one unlisted form
// and returns the handlers, the workspace slug, and both form slugs.
func unlistedFixture(t *testing.T) (*PublicHandlers, string, string, string) {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)

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

	fs := dbstore.NewFormStore(pool)
	pubSlug := "it-pub-" + accountID[:8]
	unlSlug := "it-unl-" + accountID[:8]
	for _, f := range []*dbstore.Form{
		{AccountID: accountID, Slug: pubSlug, Title: "Published form", Status: dbstore.FormStatusPublished,
			Config: map[string]any{"blocks": []any{map[string]any{"type": "text", "value": "Hello"}}},
			Theme:  map[string]any{"primary": "#5e6ad2"}},
		{AccountID: accountID, Slug: unlSlug, Title: "Unlisted form", Status: dbstore.FormStatusUnlisted,
			Config: map[string]any{"blocks": []any{map[string]any{"type": "text", "value": "Secret"}}},
			Theme:  map[string]any{"primary": "#5e6ad2"}},
	} {
		if err := fs.Create(ctx, f); err != nil {
			t.Fatalf("create form %s: %v", f.Slug, err)
		}
	}

	h := &PublicHandlers{
		Forms:      fs,
		Accounts:   dbstore.NewAccountStore(pool),
		Workspaces: dbstore.NewWorkspaceStore(pool),
		BaseURL:    "http://localhost:8080",
	}
	return h, workspaceSlug, pubSlug, unlSlug
}

// TestUnlistedFormServedAtDirectURL: an unlisted form is live at its direct
// /{slug} URL on the workspace subdomain (submissions work — it renders).
func TestUnlistedFormServedAtDirectURL(t *testing.T) {
	h, workspaceSlug, _, unlSlug := unlistedFixture(t)

	rec := publicServe(t, h, workspaceSlug+".jomform.com", "/"+unlSlug)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /%s (unlisted) status = %d, want 200 (body %s)", unlSlug, rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); body == "" {
		t.Fatal("unlisted form render served an empty body")
	}
}

// TestUnlistedFormExcludedFromStorefront: the workspace storefront root lists the
// published form but NOT the unlisted form.
func TestUnlistedFormExcludedFromStorefront(t *testing.T) {
	h, workspaceSlug, pubSlug, unlSlug := unlistedFixture(t)

	rec := publicServe(t, h, workspaceSlug+".jomform.com", "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / storefront status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, pubSlug) {
		t.Errorf("storefront missing published form slug %q (body %s)", pubSlug, body)
	}
	if strings.Contains(body, unlSlug) {
		t.Errorf("storefront must NOT list unlisted form slug %q (body %s)", unlSlug, body)
	}
}

// TestUnlistedFormExcludedFromPublishedList: ListPublishedByWorkspace returns only
// the published form, never the unlisted one.
func TestUnlistedFormExcludedFromPublishedList(t *testing.T) {
	h, workspaceSlug, pubSlug, unlSlug := unlistedFixture(t)

	forms, err := h.Forms.ListPublishedByWorkspace(context.Background(), workspaceSlug)
	if err != nil {
		t.Fatalf("ListPublishedByWorkspace: %v", err)
	}
	seen := map[string]bool{}
	for _, f := range forms {
		seen[f.Slug] = true
	}
	if !seen[pubSlug] {
		t.Errorf("ListPublishedByWorkspace missing published form %q", pubSlug)
	}
	if seen[unlSlug] {
		t.Errorf("ListPublishedByWorkspace must NOT include unlisted form %q", unlSlug)
	}
}
