package httpapi

// Regression: a template lookup that cannot resolve a row the caller may see
// must answer 404, never 500.
//
// The template routes authorise by resolving the row through
// FormStore.TemplateByIDForAccount / SoftDeleteTemplate (account-scoped). A
// miss has two shapes, and both are a NOT-FOUND rather than an internal
// failure: an id that names no row this query could return (never existed,
// another account's template, a non-template form, a gallery template), and an
// id that cannot be a UUID at all — Postgres raises SQLSTATE 22P02 for it.
// RespondErr only maps the store sentinels, so an unmapped driver error fell
// through to its default branch: HTTP 500 + an error_class=internal counter on
// what is really a client-side bad id.
//
// Driven through the real REST mux against a real Postgres; skipped without
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

// tplLookupBody decodes a JSON object body, tolerating an empty one.
func tplLookupBody(t *testing.T, raw json.RawMessage) map[string]any {
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

// assertTemplateMiss asserts a 404 with the store's not-found message. The
// message is asserted (not just the status) so a 404 that comes from somewhere
// else on the route cannot satisfy the test.
func assertTemplateMiss(t *testing.T, h *RESTHandlers, key, method, path, body, what string) {
	t.Helper()
	code, raw := tplReq(t, h, key, method, path, body)
	if code != http.StatusNotFound {
		t.Errorf("%s %s (%s) = %d, want 404 (body=%s)", method, path, what, code, raw)
		return
	}
	if got := tplLookupBody(t, raw)["error"]; got != "not found" {
		t.Errorf("%s %s (%s) error = %v, want \"tidak dijumpai\" (body=%s)", method, path, what, got, raw)
	}
}

func TestTemplateRoutesAnswer404ForUnseenTemplateIDs(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping template lookup status test")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	accA, _ := bundleFixture(t, ctx, pool)
	accB, _ := bundleFixture(t, ctx, pool)
	// Card t_d07c8fbb: the /use route MINTS a form, so a workspace-less account
	// is refused 409 before the template is even resolved. This test is about
	// the LOOKUP status on that route, so both accounts must be able to complete
	// a clone — otherwise every "want 404" case below would be answered by the
	// workspace gate instead, and the test would pass without checking a lookup.
	bundleLinkWorkspace(t, ctx, pool, accA)
	bundleLinkWorkspace(t, ctx, pool, accB)
	keyA := bundleAPIKey(t, ctx, pool, accA)
	h := newBundleHandlers(pool)
	forms := dbstore.NewFormStore(pool)

	// Account A's own template: the no-over-correction control.
	code, raw := tplReq(t, h, keyA, http.MethodPost, "/v1/rest/templates",
		`{"title":"Owned Template","config":{"mode":"response","blocks":[{"id":"b1","type":"text","value":"hello"}]},"theme":{"primary":"#0f0f0f"}}`)
	if code != http.StatusCreated {
		t.Fatalf("create own template = %d, want 201 (body=%s)", code, raw)
	}
	var own struct {
		ID     string `json:"id"`
		Slug   string `json:"slug"`
		Title  string `json:"title"`
		Config struct {
			Mode string `json:"mode"`
		} `json:"config"`
		Theme struct {
			Primary string `json:"primary"`
		} `json:"theme"`
	}
	if err := json.Unmarshal(raw, &own); err != nil {
		t.Fatalf("decode created template: %v (%s)", err, raw)
	}
	if own.ID == "" {
		t.Fatalf("created template has no id: %s", raw)
	}

	// Account B's template: it exists, so it must be invisible rather than gone.
	theirs := &dbstore.Form{
		AccountID: accB, Slug: "tpl-foreign-" + accB[:8], Title: "Foreign Template",
		Config: map[string]any{"category": "x"}, Theme: map[string]any{},
	}
	if err := forms.CreateTemplate(ctx, theirs); err != nil {
		t.Fatalf("create foreign template: %v", err)
	}

	// A gallery template id (system-owned, seeded by the template migration).
	var galleryID string
	if err := pool.QueryRow(ctx,
		`SELECT id FROM forms WHERE is_template AND status='published' LIMIT 1`).Scan(&galleryID); err != nil {
		t.Fatalf("no seeded gallery template: %v", err)
	}

	// A NON-template form the caller owns: the row exists and is the caller's,
	// so it is excluded for a third reason (the is_template predicate) and must
	// still be a miss rather than a leak or a 500.
	plain := &dbstore.Form{
		AccountID: accA, Slug: "tpl-plain-" + accA[:8], Title: "Plain Form",
		Status: dbstore.FormStatusDraft,
	}
	if err := forms.Create(ctx, plain); err != nil {
		t.Fatalf("create plain form: %v", err)
	}

	// 1. Own template still resolves through GET with its full payload.
	code, raw = tplReq(t, h, keyA, http.MethodGet, "/v1/rest/templates/"+own.ID, "")
	if code != http.StatusOK {
		t.Fatalf("own template GET = %d, want 200 (body=%s)", code, raw)
	}
	body := tplLookupBody(t, raw)
	if body["id"] != own.ID || body["slug"] != own.Slug || body["title"] != "Owned Template" {
		t.Errorf("own template payload = %s, want id/slug/title of the created template", raw)
	}
	if cfg, ok := body["config"].(map[string]any); !ok || cfg["mode"] != "response" {
		t.Errorf("own template config missing/incorrect: %s", raw)
	}
	if th, ok := body["theme"].(map[string]any); !ok || th["primary"] != "#0f0f0f" {
		t.Errorf("own template theme missing/incorrect: %s", raw)
	}
	if body["updated_at"] == nil {
		t.Errorf("own template payload has no updated_at: %s", raw)
	}

	// The same control on the two writing routes.
	code, raw = tplReq(t, h, keyA, http.MethodPut, "/v1/rest/templates/"+own.ID,
		`{"title":"Renamed Template"}`)
	if code != http.StatusOK {
		t.Fatalf("own template PUT = %d, want 200 (body=%s)", code, raw)
	}
	if got := tplLookupBody(t, raw)["title"]; got != "Renamed Template" {
		t.Errorf("own template PUT title = %v, want Renamed Template (body=%s)", got, raw)
	}
	code, raw = tplReq(t, h, keyA, http.MethodPost, "/v1/rest/templates/"+own.ID+"/use", `{}`)
	if code != http.StatusCreated {
		t.Fatalf("own template use = %d, want 201 (body=%s)", code, raw)
	}

	// 2. Every unresolvable id is a 404 — never a 500 — on all four routes.
	// Each id shape is a separate case because the accessors reach them
	// through different branches: a well-formed uuid goes to Postgres and
	// comes back ErrNoRows (or no rows affected), a non-uuid never gets that
	// far and raises SQLSTATE 22P02 instead.
	cases := []struct{ name, id string }{
		{"nonexistent uuid", uuid.NewString()},
		{"other account's template", theirs.ID},
		{"gallery template id", galleryID},
		{"caller's own non-template form", plain.ID},
		{"non-uuid id", "not-a-uuid"},
	}
	for _, tc := range cases {
		assertTemplateMiss(t, h, keyA, http.MethodGet, "/v1/rest/templates/"+tc.id, "", tc.name)
		assertTemplateMiss(t, h, keyA, http.MethodPut, "/v1/rest/templates/"+tc.id, `{"title":"hijacked"}`, tc.name)
		assertTemplateMiss(t, h, keyA, http.MethodPost, "/v1/rest/templates/"+tc.id+"/use", `{}`, tc.name)
		assertTemplateMiss(t, h, keyA, http.MethodDelete, "/v1/rest/templates/"+tc.id, "", tc.name)
	}

	// 3. POST /v1/templates/use names its own message on that path, so assert
	// the status: a malformed template_id is a miss, not a server fault.
	code, raw = tplReq(t, h, keyA, http.MethodPost, "/v1/templates/use",
		`{"template_id":"not-a-uuid"}`)
	if code != http.StatusNotFound {
		t.Errorf("POST /v1/templates/use (non-uuid) = %d, want 404 (body=%s)", code, raw)
	}
	// ... and the control: the caller's own template still clones.
	code, raw = tplReq(t, h, keyA, http.MethodPost, "/v1/templates/use",
		`{"template_id":"`+own.ID+`"}`)
	if code != http.StatusCreated {
		t.Errorf("POST /v1/templates/use (own template) = %d, want 201 (body=%s)", code, raw)
	}

	// 4. The store contract behind those statuses: every miss is the sentinel
	// and its error class is not_found (that is the error_class field of the
	// log line and the metric bucket respondErr would otherwise count as
	// internal).
	for _, tc := range cases {
		_, gerr := forms.TemplateByIDForAccount(ctx, accA, tc.id)
		if gerr == nil {
			t.Errorf("TemplateByIDForAccount(%s) returned a row the account does not own", tc.name)
			continue
		}
		if !errors.Is(gerr, dbstore.ErrFormNotFound) {
			t.Errorf("TemplateByIDForAccount(%s) err = %v, want dbstore.ErrFormNotFound", tc.name, gerr)
		}
		if class := errorClass(gerr); class != "not_found" {
			t.Errorf("errorClass(TemplateByIDForAccount(%s)) = %q, want not_found", tc.name, class)
		}

		derr := forms.SoftDeleteTemplate(ctx, accA, tc.id)
		if derr == nil {
			t.Errorf("SoftDeleteTemplate(%s) succeeded for a template the account does not own", tc.name)
			continue
		}
		if !errors.Is(derr, dbstore.ErrFormNotFound) {
			t.Errorf("SoftDeleteTemplate(%s) err = %v, want dbstore.ErrFormNotFound", tc.name, derr)
		}
		if class := errorClass(derr); class != "not_found" {
			t.Errorf("errorClass(SoftDeleteTemplate(%s)) = %q, want not_found", tc.name, class)
		}
	}

	// A gallery template is not deletable either — the DELETE must not have
	// soft-deleted the system-owned row on its way to the 404.
	var galleryDeleted *string
	if err := pool.QueryRow(ctx,
		`SELECT deleted_at::text FROM forms WHERE id=$1`, galleryID).Scan(&galleryDeleted); err != nil {
		t.Fatalf("re-read gallery template: %v", err)
	}
	if galleryDeleted != nil {
		t.Errorf("gallery template %s was soft-deleted by a caller who does not own it", galleryID)
	}
	// Same for the caller's own NON-template form: the DELETE route is scoped to
	// templates, so a 404 must not be a soft delete of a real form.
	var plainDeleted *string
	if err := pool.QueryRow(ctx,
		`SELECT deleted_at::text FROM forms WHERE id=$1`, plain.ID).Scan(&plainDeleted); err != nil {
		t.Fatalf("re-read plain form: %v", err)
	}
	if plainDeleted != nil {
		t.Errorf("plain (non-template) form %s was soft-deleted by the template DELETE route", plain.ID)
	}

	// 5. Over-correction control on DELETE itself: the caller's own template
	// still deletes for real, and is then gone. Done last because it removes
	// the fixture the earlier assertions read.
	code, raw = tplReq(t, h, keyA, http.MethodDelete, "/v1/rest/templates/"+own.ID, "")
	if code != http.StatusOK {
		t.Fatalf("own template DELETE = %d, want 200 (body=%s)", code, raw)
	}
	if err := forms.SoftDeleteTemplate(ctx, accA, own.ID); !errors.Is(err, dbstore.ErrFormNotFound) {
		t.Errorf("re-delete of the caller's own template err = %v, want ErrFormNotFound after the first delete", err)
	}
	assertTemplateMiss(t, h, keyA, http.MethodGet, "/v1/rest/templates/"+own.ID, "", "deleted own template")
}
