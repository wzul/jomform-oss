package httpapi

// Issue #297 regression integration tests: the GET /{slug} form-delivery path
// (handleWorkspaceRootForm) previously resolved ONLY workspace subdomains and 404'd on
// a verified custom domain, even though the form existed + was published in the
// owning workspace. These tests verify a verified custom domain serves its forms
// end-to-end and that resolution is consistent across ALL buyer-facing routes:
//   - GET  /{slug}          render           (was 404 — the bug)
//   - POST /{slug}/order    submit           (already worked)
//   - POST /{slug}/unlock   private unlock   (already worked)
//   - GET  /sitemap.xml     sitemap          (already worked)
//   - GET  /{$}             storefront root  (already worked, issue #286)
//
// Runs against a real Postgres (JOMFORM_TEST_DATABASE_URL); skipped when unset.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// verifiedCustomDomainFixture seeds a user + workspace + account + published form,
// claims + verifies a custom domain for that account, and returns the handlers,
// the domain, the workspace slug, the account slug, and the form slug.
func verifiedCustomDomainFixture(t *testing.T) (*PublicHandlers, string, string, string, string) {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)

	var userID, workspaceSlug, accountID, accountSlug string
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
		 RETURNING id, slug`, userID, workspaceSlug).Scan(&accountID, &accountSlug); err != nil {
		t.Fatalf("create account: %v", err)
	}

	fs := dbstore.NewFormStore(pool)
	formSlug := "it-form-" + accountID[:8]
	f := &dbstore.Form{
		AccountID: accountID, Slug: formSlug, Title: "IT form", Status: dbstore.FormStatusPublished,
		Config: map[string]any{
			"mode": "response",
			"blocks": []any{
				map[string]any{"id": "b1", "type": "name", "value": "Full name", "required": true},
				map[string]any{"id": "b2", "type": "email", "value": "Email", "required": true},
				map[string]any{"id": "b3", "type": "textarea", "value": "Feedback"},
			},
		},
		Theme: map[string]any{"primary": "#5e6ad2"},
	}
	if err := fs.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}

	h := &PublicHandlers{
		Forms:       fs,
		Sales:       dbstore.NewSaleStore(pool),
		Accounts:    dbstore.NewAccountStore(pool),
		Domains:     dbstore.NewDomainStore(pool),
		Products:    dbstore.NewProductStore(pool),
		Workspaces:      dbstore.NewWorkspaceStore(pool),
		Submissions: dbstore.NewSubmissionStore(pool),
		BaseURL:     "http://localhost:8080",
	}

	domain := "it-" + accountSlug + ".example.test"
	if _, err := h.Domains.AddDomain(ctx, accountID, domain, "cname"); err != nil {
		t.Fatalf("add domain: %v", err)
	}
	if err := h.Domains.MarkVerified(ctx, accountID, domain); err != nil {
		t.Fatalf("mark verified: %v", err)
	}
	return h, domain, workspaceSlug, accountSlug, formSlug
}

// postForm submits the given method/body to a path on a host through the mux.
func postForm(t *testing.T, h *PublicHandlers, host, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Host = host
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// TestCustomDomainRootFormServesRender: GET /{slug} on a verified custom domain
// must serve the published form (the issue #297 regression — previously 404).
func TestCustomDomainRootFormServesRender(t *testing.T) {
	h, domain, workspaceSlug, _, formSlug := verifiedCustomDomainFixture(t)

	// The custom domain renders the published form (the bug: was 404).
	rec := publicServe(t, h, domain, "/"+formSlug)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /%s on custom domain status = %d, want 200 (body %s)", formSlug, rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); body == "" {
		t.Fatal("custom domain render served an empty body")
	}

	// The matching workspace subdomain still serves the same form (unchanged).
	rec = publicServe(t, h, workspaceSlug+".jomform.com", "/"+formSlug)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /%s on workspace subdomain status = %d, want 200 (body %s)", formSlug, rec.Code, rec.Body.String())
	}

	// Bare apex (no subdomain, no custom domain) still 404s (issue #77).
	rec = publicServe(t, h, "jomform.com", "/"+formSlug)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /%s on bare apex status = %d, want 404", formSlug, rec.Code)
	}
}

// TestCustomDomainSubmit: POST /{slug}/order through a verified custom domain
// must accept the submission exactly as it does on the workspace subdomain.
func TestCustomDomainSubmit(t *testing.T) {
	h, domain, _, _, formSlug := verifiedCustomDomainFixture(t)

	body := `{"client":{"full_name":"Ali","email":"ali@example.test","phone":"0123"},
	          "fields":{"b1":"Ali","b2":"ali@example.test","b3":"Great form"}}`
	rec := postForm(t, h, domain, "/"+formSlug+"/order", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /%s/order on custom domain status = %d, want 201 (body %s)", formSlug, rec.Code, rec.Body.String())
	}
	var resp struct {
		Status string `json:"status"`
		ID     string `json:"id"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(rec.Body.String())), &resp); err != nil {
		t.Fatalf("decode resp: %v (body %s)", err, rec.Body.String())
	}
	if resp.Status != "submitted" || resp.ID == "" {
		t.Fatalf("resp = %+v, want submitted + id", resp)
	}
	if sub, err := h.Submissions.ByID(context.Background(), resp.ID); err != nil {
		t.Fatalf("fetch submission: %v", err)
	} else if sub.Data["b3"] != "Great form" {
		t.Errorf("data[b3] = %v, want Great form", sub.Data["b3"])
	}
}

// TestCustomDomainPrivateUnlock: a private form served via a verified custom
// domain shows the password gate and unlocks with the correct access password.
func TestCustomDomainPrivateUnlock(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	h, domain, _, _, formSlug := verifiedCustomDomainFixture(t)

	// Fetch the form id so we can set a password + flip it to private.
	var formID, formAccountID string
	if err := pool.QueryRow(ctx,
		`SELECT id, account_id FROM forms WHERE slug = $1 ORDER BY created_at DESC LIMIT 1`,
		formSlug).Scan(&formID, &formAccountID); err != nil {
		t.Fatalf("lookup form id: %v", err)
	}
	hash, err := auth.HashPassword("secret123")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if err := h.Forms.SetAccessPassword(ctx, formAccountID, formID, hash); err != nil {
		t.Fatalf("set access password: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE forms SET status = 'private' WHERE id = $1 AND account_id = $2`,
		formID, formAccountID); err != nil {
		t.Fatalf("set private: %v", err)
	}

	h.AccessKey = []byte(`0123456789abcdef0123456789abcdef`)

	// Without unlocking, the custom domain shows the password gate.
	rec := publicServe(t, h, domain, "/"+formSlug)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /%s (private gate) status = %d, want 200 (body %s)", formSlug, rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "dilindungi kata laluan") {
		t.Fatalf("private gate not rendered for custom domain (body %s)", rec.Body.String())
	}

	// Wrong password is rejected via the custom domain.
	bad := postForm(t, h, domain, "/"+formSlug+"/unlock", `{"password":"wrong"}`)
	if bad.Code != http.StatusForbidden {
		t.Fatalf("wrong-password unlock status = %d, want 403 (body %s)", bad.Code, bad.Body.String())
	}

	// Correct password unlocks via the custom domain.
	good := postForm(t, h, domain, "/"+formSlug+"/unlock", `{"password":"secret123"}`)
	if good.Code != http.StatusOK {
		t.Fatalf("correct-password unlock status = %d, want 200 (body %s)", good.Code, good.Body.String())
	}
	// After unlocking (cookie present), the form renders instead of the gate.
	req := httptest.NewRequest(http.MethodGet, "/"+formSlug, nil)
	req.Host = domain
	for _, c := range good.Result().Cookies() {
		req.AddCookie(c)
	}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusOK {
		t.Fatalf("GET /%s after unlock status = %d, want 200 (body %s)", formSlug, rec2.Code, rec2.Body.String())
	}
	if strings.Contains(rec2.Body.String(), "dilindungi kata laluan") {
		t.Fatalf("gate still shown after unlock on custom domain (body %s)", rec2.Body.String())
	}
}

// TestCustomDomainSitemap: a verified custom domain's sitemap.xml lists the
// workspace's published forms (issue #276).
func TestCustomDomainSitemap(t *testing.T) {
	h, domain, _, _, formSlug := verifiedCustomDomainFixture(t)

	rec := publicServe(t, h, domain, "/sitemap.xml")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /sitemap.xml on custom domain status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), formSlug) {
		t.Fatalf("sitemap for custom domain missing form slug %q (body %s)", formSlug, rec.Body.String())
	}
}

// TestCustomDomainStorefrontRoot: a verified custom domain's root "/" serves
// the workspace storefront list including the form slug (issue #286).
func TestCustomDomainStorefrontRoot(t *testing.T) {
	h, domain, _, _, formSlug := verifiedCustomDomainFixture(t)

	rec := publicServe(t, h, domain, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / on custom domain status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), formSlug) {
		t.Fatalf("storefront root for custom domain missing form slug %q (body %s)", formSlug, rec.Body.String())
	}
}
