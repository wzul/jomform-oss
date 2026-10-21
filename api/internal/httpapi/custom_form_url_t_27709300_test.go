package httpapi

// A custom-HTML form's registered `type:"url"` field maps onto the builder's
// `website` check (customform.validationType), which used to be
// `strings.HasPrefix(val, "http://") || strings.HasPrefix(val, "https://")`.
// The page the buyer fills in is a real `<input type="url">`, whose own
// constraint is "the value parses as an absolute URL", so Chrome accepts
// ftp://example.com/x and the server answered 400 `URL tidak sah: Website`.
//
// This drives the handler rather than the helper: the unit test in
// internal/formfields pins ValidWebsiteURL, and this one proves the submit path
// actually calls it (a correct helper left unwired would pass the unit test and
// still refuse the buyer's answer).
//
// Integration test; skipped unless JOMFORM_TEST_DATABASE_URL is set.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wzul/jomform/api/internal/customform"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// customFormURLFixture creates a published custom-HTML response form whose
// registered contract carries a `type:"url"` field named `site`, plus the two
// required fields the anti-spam/dedupe paths expect to be present.
func customFormURLFixture(t *testing.T) (*PublicHandlers, string, string) {
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
	formSlug := "it-custom-url-" + accountID[:8]
	f := &dbstore.Form{
		AccountID: accountID, Slug: formSlug, Title: "Custom form", Status: "published",
		Config: map[string]any{
			"mode": "response",
			customform.ConfigKey: map[string]any{
				"html": `<div><label>Name</label><input type="text" name="name" required></div>
					<div><label>Email</label><input type="email" name="email" required></div>
					<div><label>Website</label><input type="url" name="site"></div>
					<button type="submit">Send</button>`,
				"fields": []any{
					map[string]any{"field_id": "name", "type": "text", "label": "Name", "required": true},
					map[string]any{"field_id": "email", "type": "email", "label": "Email", "required": true},
					map[string]any{"field_id": "site", "type": "url", "label": "Website"},
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
		Workspaces:  dbstore.NewWorkspaceStore(pool),
		Submissions: dbstore.NewSubmissionStore(pool),
		BaseURL:     "http://localhost:8080",
	}
	return h, workspaceSlug, formSlug
}

func TestCustomFormURLFieldAcceptsWhatTheBrowserAccepts(t *testing.T) {
	h, workspaceSlug, formSlug := customFormURLFixture(t)
	host := workspaceSlug + ".jomform.com"
	path := "/" + formSlug + "/order"

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	post := func(site string) *httptest.ResponseRecorder {
		body, err := json.Marshal(map[string]any{
			"client": map[string]any{},
			"fields": map[string]string{"name": "Ali", "email": "ali@example.test", "site": site},
		})
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewBuffer(body))
		req.Host = host
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// The value this card is about: the browser's own `<input type="url">`
	// reports ftp://example.com/x valid, so the server must store it, not 400.
	rec := post("ftp://example.com/x")
	if rec.Code != http.StatusCreated {
		t.Fatalf("ftp:// submit status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}

	// The value both sides refuse: no scheme.
	rec = post("jomform.com")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("scheme-less submit status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}

	// The one deliberate divergence: the browser accepts the string, the server
	// refuses to store a script-bearing URI.
	rec = post("javascript:alert(1)")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("javascript: submit status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}

	// Only the 201 above may have stored a row, and it must hold the ftp URL
	// verbatim — the answer the buyer typed, not a rewritten http(s) one.
	if got := customFormSubmissions(t, formSlug); got != 1 {
		t.Fatalf("stored submissions = %d, want 1 (the rejected posts must store nothing)", got)
	}
	pool := bundleTestPool(t)
	var raw []byte
	if err := pool.QueryRow(context.Background(),
		`SELECT s.data FROM submissions s JOIN forms f ON f.id = s.form_id
		 WHERE f.slug = $1`, formSlug).Scan(&raw); err != nil {
		t.Fatalf("read stored submission: %v", err)
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("decode stored data: %v", err)
	}
	if data["site"] != "ftp://example.com/x" {
		t.Errorf("stored site = %v, want the ftp:// value the buyer typed", data["site"])
	}
}
