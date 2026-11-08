package httpapi

// Issue #457: merchant-managed templates. An own template is an
// owner-scoped forms row (is_template=true, status='draft'). These
// integration tests prove the CRUD surface is account-scoped (a template is
// never readable/editable/deletable/usable across tenants), that saving an
// existing form as a template copies its config, and that templates stay out
// of the merchant's Forms list. Skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// tplReq issues an API-key-authed request through the REST mux.
func tplReq(t *testing.T, h *RESTHandlers, rawKey, method, path, body string) (int, json.RawMessage) {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	var rdr *bytes.Buffer
	if body == "" {
		rdr = bytes.NewBufferString("")
	} else {
		rdr = bytes.NewBufferString(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec.Code, json.RawMessage(rec.Body.Bytes())
}

func TestTemplateCRUDIsOwnerScoped(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping template CRUD test")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	accA, _ := bundleFixture(t, ctx, pool)
	accB, _ := bundleFixture(t, ctx, pool)
	// Card t_d07c8fbb: cloning a template mints a form, which an account with no
	// workspace cannot do (409). Both accounts clone here — A for the control,
	// B to prove another tenant's template is a 404 rather than the workspace
	// refusal.
	bundleLinkWorkspace(t, ctx, pool, accA)
	bundleLinkWorkspace(t, ctx, pool, accB)
	keyA := bundleAPIKey(t, ctx, pool, accA)
	keyB := bundleAPIKey(t, ctx, pool, accB)
	h := newBundleHandlers(pool)

	// A source form owned by account A, with a distinctive config block.
	forms := dbstore.NewFormStore(pool)
	src := &dbstore.Form{
		AccountID: accA, Slug: "tpl-src-" + accA[:8], Title: "Order Form",
		Config: map[string]any{"mode": "response", "blocks": []any{
			map[string]any{"id": "b1", "type": "text", "value": "hello"},
		}},
		Theme: map[string]any{"primary": "#123456"}, Status: dbstore.FormStatusDraft,
	}
	if err := forms.Create(ctx, src); err != nil {
		t.Fatalf("create source form: %v", err)
	}

	// Save the form as a template.
	code, raw := tplReq(t, h, keyA, http.MethodPost, "/v1/rest/templates",
		`{"form_id":"`+src.ID+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("create template status = %d, want 201 (body=%s)", code, raw)
	}
	var created struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Config struct {
			Mode string `json:"mode"`
		} `json:"config"`
	}
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatalf("decode created: %v (%s)", err, raw)
	}
	if created.ID == "" {
		t.Fatalf("created template has no id: %s", raw)
	}
	if created.Title != "Order Form" {
		t.Errorf("template title = %q, want the source form's title", created.Title)
	}
	if created.Config.Mode != "response" {
		t.Errorf("template config.mode = %q, want response (copied from form)", created.Config.Mode)
	}

	// The template must NOT show up in the Forms list (it is not a form).
	code, raw = tplReq(t, h, keyA, http.MethodGet, "/v1/rest/forms", "")
	if code != http.StatusOK {
		t.Fatalf("list forms status = %d", code)
	}
	var list []map[string]any
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatalf("decode forms: %v", err)
	}
	for _, f := range list {
		if f["id"] == created.ID {
			t.Fatalf("template leaked into the Forms list: %s", raw)
		}
	}

	// Account A sees its own template.
	code, raw = tplReq(t, h, keyA, http.MethodGet, "/v1/rest/templates", "")
	if code != http.StatusOK {
		t.Fatalf("list own templates status = %d", code)
	}
	var own []map[string]any
	if err := json.Unmarshal(raw, &own); err != nil {
		t.Fatalf("decode own templates: %v", err)
	}
	if len(own) != 1 || own[0]["id"] != created.ID {
		t.Fatalf("account A own templates = %s, want exactly the created one", raw)
	}

	// Account B must NOT see account A's template.
	code, raw = tplReq(t, h, keyB, http.MethodGet, "/v1/rest/templates", "")
	if code != http.StatusOK {
		t.Fatalf("B list templates status = %d", code)
	}
	var bOwn []map[string]any
	_ = json.Unmarshal(raw, &bOwn)
	if len(bOwn) != 0 {
		t.Fatalf("account B sees %d templates, want 0 (cross-tenant leak): %s", len(bOwn), raw)
	}

	// Account B must not read / update / delete / use account A's template.
	for _, tc := range []struct {
		method, path, body string
		want               int
	}{
		{http.MethodGet, "/v1/rest/templates/" + created.ID, "", http.StatusNotFound},
		{http.MethodPut, "/v1/rest/templates/" + created.ID, `{"title":"hijacked"}`, http.StatusNotFound},
		{http.MethodDelete, "/v1/rest/templates/" + created.ID, "", http.StatusNotFound},
		{http.MethodPost, "/v1/rest/templates/" + created.ID + "/use", `{}`, http.StatusNotFound},
	} {
		code, raw = tplReq(t, h, keyB, tc.method, tc.path, tc.body)
		if code != tc.want {
			t.Errorf("B %s %s = %d, want %d (body=%s)", tc.method, tc.path, code, tc.want, raw)
		}
	}

	// Account A can edit its template.
	code, raw = tplReq(t, h, keyA, http.MethodPut, "/v1/rest/templates/"+created.ID,
		`{"title":"Renamed Template"}`)
	if code != http.StatusOK {
		t.Fatalf("update status = %d, want 200 (body=%s)", code, raw)
	}
	var updated struct {
		Title string `json:"title"`
	}
	_ = json.Unmarshal(raw, &updated)
	if updated.Title != "Renamed Template" {
		t.Errorf("updated title = %q, want Renamed Template", updated.Title)
	}

	// Account A can clone its template into a new draft form.
	code, raw = tplReq(t, h, keyA, http.MethodPost, "/v1/rest/templates/"+created.ID+"/use", `{}`)
	if code != http.StatusCreated {
		t.Fatalf("use template status = %d, want 201 (body=%s)", code, raw)
	}
	var cloned struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	_ = json.Unmarshal(raw, &cloned)
	if cloned.ID == "" || cloned.ID == created.ID {
		t.Fatalf("clone did not create a new form: %s", raw)
	}
	// The clone IS a form, so it must appear in the Forms list.
	code, raw = tplReq(t, h, keyA, http.MethodGet, "/v1/rest/forms", "")
	if code != http.StatusOK {
		t.Fatalf("list forms after clone status = %d", code)
	}
	found := false
	if err := json.Unmarshal(raw, &list); err == nil {
		for _, f := range list {
			if f["id"] == cloned.ID {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("cloned form %s missing from the Forms list: %s", cloned.ID, raw)
	}

	// Account A can delete its template; afterwards it is gone from the list.
	code, raw = tplReq(t, h, keyA, http.MethodDelete, "/v1/rest/templates/"+created.ID, "")
	if code != http.StatusOK {
		t.Fatalf("delete status = %d, want 200 (body=%s)", code, raw)
	}
	code, raw = tplReq(t, h, keyA, http.MethodGet, "/v1/rest/templates", "")
	if code != http.StatusOK {
		t.Fatalf("list after delete status = %d", code)
	}
	_ = json.Unmarshal(raw, &own)
	if len(own) != 0 {
		t.Fatalf("template still listed after delete: %s", raw)
	}
}

func TestCreateTemplateFromScratch(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping template create test")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	acc, _ := bundleFixture(t, ctx, pool)
	key := bundleAPIKey(t, ctx, pool, acc)
	h := newBundleHandlers(pool)

	code, raw := tplReq(t, h, key, http.MethodPost, "/v1/rest/templates",
		`{"title":"Blank Starter","config":{"mode":"response","blocks":[]}}`)
	if code != http.StatusCreated {
		t.Fatalf("create-from-scratch status = %d, want 201 (body=%s)", code, raw)
	}
	var created struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	_ = json.Unmarshal(raw, &created)
	if created.ID == "" || created.Title != "Blank Starter" {
		t.Fatalf("unexpected created template: %s", raw)
	}

	// Missing title AND form_id is a 400.
	code, _ = tplReq(t, h, key, http.MethodPost, "/v1/rest/templates", `{}`)
	if code != http.StatusBadRequest {
		t.Errorf("create with no title/form_id = %d, want 400", code)
	}
}
