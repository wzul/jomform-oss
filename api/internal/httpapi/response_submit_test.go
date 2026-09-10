package httpapi

// Issue #171: response-only forms (no payment). The submit path must branch on
// the form mode: a response-mode form stores the submitted field values as a
// submission (no sale, no CHIP, no checkout URL) and returns a success/thank-you.
// Integration test; skipped unless JOMFORM_TEST_DATABASE_URL is set.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// responseFormFixture seeds a user + workspace + account + a published response-mode
// form with input blocks, and returns a PublicHandlers wired with the
// submission store plus the workspace slug + form slug.
func responseFormFixture(t *testing.T) (*PublicHandlers, string, string) {
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
	formSlug := "it-resp-" + accountID[:8]
	f := &dbstore.Form{
		AccountID: accountID, Slug: formSlug, Title: "Response form", Status: "published",
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
		Forms:        fs,
		Sales:        dbstore.NewSaleStore(pool),
		Accounts:     dbstore.NewAccountStore(pool),
		Products:     dbstore.NewProductStore(pool),
		Workspaces:       dbstore.NewWorkspaceStore(pool),
		Submissions:  dbstore.NewSubmissionStore(pool),
		BaseURL:      "http://localhost:8080",
	}
	return h, workspaceSlug, formSlug
}

func TestResponseSubmitStoresSubmissionNoSale(t *testing.T) {
	h, workspaceSlug, formSlug := responseFormFixture(t)
	host := workspaceSlug + ".jomform.com"
	path := "/" + formSlug + "/order"

	body := `{"client":{"full_name":"Ali","email":"ali@example.test","phone":"0123"},
	          "fields":{"b1":"Ali","b2":"ali@example.test","b3":"Great form"}}`
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Host = host
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Status string `json:"status"`
		ID     string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	if resp.Status != "submitted" {
		t.Fatalf("status = %q, want submitted", resp.Status)
	}
	if resp.ID == "" {
		t.Fatalf("no submission id returned")
	}

	// The submission must be stored with the field values + buyer identity.
	sub, err := h.Submissions.ByID(context.Background(), resp.ID)
	if err != nil {
		t.Fatalf("fetch submission: %v", err)
	}
	if sub.Data["b3"] != "Great form" {
		t.Errorf("data[b3] = %v, want Great form", sub.Data["b3"])
	}
	if sub.Data["full_name"] != "Ali" {
		t.Errorf("data[full_name] = %v, want Ali", sub.Data["full_name"])
	}
	if sub.Data["email"] != "ali@example.test" {
		t.Errorf("data[email] = %v, want ali@example.test", sub.Data["email"])
	}

	// No sale may be created for a response-mode form.
	sales, err := h.Sales.ListByAccount(context.Background(), sub.AccountID, 10)
	if err != nil {
		t.Fatalf("list sales: %v", err)
	}
	if len(sales) != 0 {
		t.Fatalf("response-mode submit created %d sales, want 0", len(sales))
	}
}
