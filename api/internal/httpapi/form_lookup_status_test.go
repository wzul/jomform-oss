package httpapi

// Regression: a form lookup that cannot resolve a row the caller may see must
// answer 404, never 500.
//
// Every per-form REST route authorises by resolving the form through
// FormStore.GetByID (account-scoped). A miss — an id that does not exist, a
// form owned by another account, a gallery template owned by the system
// account, or an id that is not a UUID at all — is a NOT-FOUND, not an
// internal failure. When the store returned the raw pgx.ErrNoRows, respondErr
// fell through to its default branch: HTTP 500 + an error_class=internal
// counter, which defeats client error handling and floods the error metrics.
//
// Integration against a real Postgres; skipped without
// JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// formLookupBody decodes a JSON object body, tolerating an empty one.
func formLookupBody(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var body map[string]any
	if len(raw) == 0 {
		return body
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode body %q: %v", raw, err)
	}
	return body
}

func TestFormRoutesAnswer404ForUnseenFormIDs(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping form lookup status test")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	accA, _ := bundleFixture(t, ctx, pool)
	accB, _ := bundleFixture(t, ctx, pool)
	keyA := bundleAPIKey(t, ctx, pool, accA)
	h := newBundleHandlers(pool)
	forms := dbstore.NewFormStore(pool)

	// The caller's own form: the no-over-correction control.
	own := &dbstore.Form{
		AccountID: accA, Slug: "nf-own-" + accA[:8], Title: "Owned Form",
		Config: map[string]any{"mode": "response", "blocks": []any{
			map[string]any{"id": "b1", "type": "text", "value": "hello"},
		}},
		Theme:  map[string]any{"primary": "#0f0f0f"},
		Status: dbstore.FormStatusPublished,
	}
	if err := forms.Create(ctx, own); err != nil {
		t.Fatalf("create own form: %v", err)
	}
	// Another account's form: exists, must be invisible.
	foreign := &dbstore.Form{
		AccountID: accB, Slug: "nf-foreign-" + accB[:8], Title: "Foreign Form",
		Status: dbstore.FormStatusPublished,
	}
	if err := forms.Create(ctx, foreign); err != nil {
		t.Fatalf("create foreign form: %v", err)
	}

	// A gallery template id (system-owned, seeded by the template migration).
	var galleryID string
	if err := pool.QueryRow(ctx,
		`SELECT id FROM forms WHERE is_template AND status='published' LIMIT 1`).Scan(&galleryID); err != nil {
		t.Fatalf("no seeded gallery template: %v", err)
	}

	missingID := uuid.NewString()

	// 1. Own form still resolves with its full payload.
	code, raw := tplReq(t, h, keyA, http.MethodGet, "/v1/rest/forms/"+own.ID, "")
	if code != http.StatusOK {
		t.Fatalf("own form status = %d, want 200 (body=%s)", code, raw)
	}
	body := formLookupBody(t, raw)
	if body["id"] != own.ID || body["title"] != "Owned Form" {
		t.Errorf("own form payload = %s, want id/title of the owned form", raw)
	}
	if cfg, ok := body["config"].(map[string]any); !ok || cfg["mode"] != "response" {
		t.Errorf("own form config missing/incorrect: %s", raw)
	}
	if th, ok := body["theme"].(map[string]any); !ok || th["primary"] != "#0f0f0f" {
		t.Errorf("own form theme missing/incorrect: %s", raw)
	}

	// 2. Every unresolvable id is a 404 — never a 500.
	for _, tc := range []struct{ name, id string }{
		{"nonexistent uuid", missingID},
		{"other account's form", foreign.ID},
		{"gallery template id", galleryID},
		{"non-uuid id", "not-a-uuid"},
	} {
		code, raw := tplReq(t, h, keyA, http.MethodGet, "/v1/rest/forms/"+tc.id, "")
		if code != http.StatusNotFound {
			t.Errorf("GET form (%s) = %d, want 404 (body=%s)", tc.name, code, raw)
			continue
		}
		if got := formLookupBody(t, raw)["error"]; got != "not found" {
			t.Errorf("GET form (%s) error = %v, want \"tidak dijumpai\"", tc.name, got)
		}
	}

	// 3. The per-form sub-resources authorise through the same lookup.
	for _, suffix := range []string{"/notifications", "/confirmations", "/menu", "/email-settings"} {
		code, raw := tplReq(t, h, keyA, http.MethodGet, "/v1/rest/forms/"+foreign.ID+suffix, "")
		if code != http.StatusNotFound {
			t.Errorf("GET %s on a foreign form = %d, want 404 (body=%s)", suffix, code, raw)
		}
	}

	// 4. Creating a template from a form the caller cannot see is a 404/400.
	for _, tc := range []struct{ name, formID string }{
		{"nonexistent", missingID},
		{"foreign", foreign.ID},
		{"non-uuid", "not-a-uuid"},
	} {
		code, raw := tplReq(t, h, keyA, http.MethodPost, "/v1/rest/templates",
			`{"form_id":"`+tc.formID+`"}`)
		if code != http.StatusNotFound && code != http.StatusBadRequest {
			t.Errorf("POST /v1/rest/templates (form_id %s) = %d, want 404 or 400 (body=%s)",
				tc.name, code, raw)
		}
	}

	// 5. The error class of a plain not-found lookup is not "internal".
	for _, id := range []string{missingID, foreign.ID, galleryID, "not-a-uuid"} {
		_, serr := forms.GetByID(ctx, accA, id)
		if serr == nil {
			t.Errorf("GetByID(%s) returned a row for a form the account does not own", id)
			continue
		}
		if !errors.Is(serr, dbstore.ErrFormNotFound) {
			t.Errorf("GetByID(%s) err = %v, want dbstore.ErrFormNotFound", id, serr)
		}
		if class := errorClass(serr); class != "not_found" {
			t.Errorf("errorClass(GetByID(%s)) = %q, want not_found", id, class)
		}
	}

	// 6. The sibling accessor the same audit found: ProductStore.Get carried
	// the identical omission, so GET /v1/rest/products/{id} answered 500 for an
	// unknown/foreign/malformed id too. Same rule, same sentinel.
	products := dbstore.NewProductStore(pool)
	mine := &dbstore.Product{AccountID: accA, Name: "Lookup Control", PriceSen: 500, Type: "one_time"}
	if err := products.Create(ctx, mine); err != nil {
		t.Fatalf("create product: %v", err)
	}
	theirs := &dbstore.Product{AccountID: accB, Name: "Foreign Product", PriceSen: 500, Type: "one_time"}
	if err := products.Create(ctx, theirs); err != nil {
		t.Fatalf("create foreign product: %v", err)
	}
	code, raw = tplReq(t, h, keyA, http.MethodGet, "/v1/rest/products/"+mine.ID, "")
	if code != http.StatusOK {
		t.Fatalf("own product status = %d, want 200 (body=%s)", code, raw)
	}
	for _, tc := range []struct{ name, id string }{
		{"nonexistent uuid", uuid.NewString()},
		{"other account's product", theirs.ID},
		{"non-uuid id", "not-a-uuid"},
	} {
		code, raw := tplReq(t, h, keyA, http.MethodGet, "/v1/rest/products/"+tc.id, "")
		if code != http.StatusNotFound {
			t.Errorf("GET product (%s) = %d, want 404 (body=%s)", tc.name, code, raw)
		}
		_, perr := products.Get(ctx, accA, tc.id)
		if !errors.Is(perr, dbstore.ErrProductNotFound) {
			t.Errorf("Products.Get(%s) err = %v, want dbstore.ErrProductNotFound", tc.name, perr)
		}
		if class := errorClass(perr); class != "not_found" {
			t.Errorf("errorClass(Products.Get(%s)) = %q, want not_found", tc.name, class)
		}
	}
}
