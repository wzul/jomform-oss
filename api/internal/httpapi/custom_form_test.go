package httpapi

// Issue #255: fully-custom HTML response-only form. The agent-authored HTML
// body is rendered in a sandboxed iframe (CSP + sanitized) and only the
// registered fields are recorded on submit. Integration test; skipped unless
// JOMFORM_TEST_DATABASE_URL is set.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/customform"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// customFormFixture seeds a user + space + account + a published custom-HTML
// response form, and returns a PublicHandlers plus the space slug + form slug.
func customFormFixture(t *testing.T) (*PublicHandlers, string, string) {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)

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

	fs := dbstore.NewFormStore(pool)
	formSlug := "it-custom-" + accountID[:8]
	f := &dbstore.Form{
		AccountID: accountID, Slug: formSlug, Title: "Custom form", Status: "published",
		Config: map[string]any{
			"mode": "response",
			customform.ConfigKey: map[string]any{
				"html": `<div><label>Name</label><input type="text" name="name" required></div>
					<div><label>Email</label><input type="email" name="email" required></div>
					<div><label>Feedback</label><textarea name="feedback"></textarea></div>
					<button type="submit">Send</button>`,
				"fields": []any{
					map[string]any{"field_id": "name", "type": "text", "label": "Name", "required": true},
					map[string]any{"field_id": "email", "type": "email", "label": "Email", "required": true},
					map[string]any{"field_id": "feedback", "type": "textarea", "label": "Feedback"},
				},
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
		Products:    dbstore.NewProductStore(pool),
		Spaces:      dbstore.NewSpaceStore(pool),
		Submissions: dbstore.NewSubmissionStore(pool),
		BaseURL:     "http://localhost:8080",
	}
	return h, spaceSlug, formSlug
}

func TestCustomFormSubmitStoresOnlyRegisteredFields(t *testing.T) {
	h, spaceSlug, formSlug := customFormFixture(t)
	host := spaceSlug + ".jomform.com"
	path := "/" + formSlug + "/order"

	// The submitted data includes an unregistered "hack" field that must be
	// dropped, plus the anti-spam fields that are consumed and excluded.
	body := `{"client":{"full_name":"Ali","email":"ali@example.test","phone":"0123"},
	          "fields":{"name":"Ali","email":"ali@example.test","feedback":"Great form",
	                    "hack":"injected","website":"","jf_ts":"0"}}`
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
	if resp.Status != "submitted" || resp.ID == "" {
		t.Fatalf("resp = %+v", resp)
	}

	sub, err := h.Submissions.ByID(context.Background(), resp.ID)
	if err != nil {
		t.Fatalf("fetch submission: %v", err)
	}
	if sub.Data["name"] != "Ali" || sub.Data["email"] != "ali@example.test" || sub.Data["feedback"] != "Great form" {
		t.Errorf("registered fields not stored: %v", sub.Data)
	}
	if _, ok := sub.Data["hack"]; ok {
		t.Errorf("unregistered field stored: %v", sub.Data)
	}
	if _, ok := sub.Data["website"]; ok {
		t.Errorf("honeypot field stored: %v", sub.Data)
	}
	if _, ok := sub.Data["jf_ts"]; ok {
		t.Errorf("render-timestamp field stored: %v", sub.Data)
	}
}

func TestCustomFormRenderSanitizesAndSetsCSP(t *testing.T) {
	h, spaceSlug, formSlug := customFormFixture(t)
	host := spaceSlug + ".jomform.com"
	path := "/" + formSlug

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("render status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	// The page must set a strict CSP and render a sandboxed iframe.
	if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "default-src 'none'") {
		t.Errorf("CSP not set: %q", rec.Header().Get("Content-Security-Policy"))
	}
	if !strings.Contains(body, `sandbox="allow-forms"`) {
		t.Errorf("iframe not sandboxed")
	}
	// The sanitized body is embedded as a JSON string literal in the page JS
	// (json.Marshal HTML-escapes < as \u003c and quotes as \"). Verify the
	// registered input's name attribute survives the round-trip.
	if !strings.Contains(body, `name=\\\"name\\\"`) && !strings.Contains(body, `name=\"name\"`) {
		t.Errorf("custom HTML body not rendered")
	}
}
