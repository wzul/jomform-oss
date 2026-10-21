package httpapi

// Card t_73a5db6e: a custom-HTML form's registered `required` fields are
// enforced at POST /<slug>/order (card t_50a3f60d), and the buyer's page can
// only produce a value for a control that exists in the sanitized HTML (the
// iframe bridge posts doc.querySelectorAll('[name]')). Nothing refused the
// mismatch at WRITE time, so a form could be saved with
// fields:[{field_id:"nama", required:true}] and no name="nama" control — and it
// was then permanently unsubmittable: every submit from the page answers
// 400 `medan wajib tidak diisi: Nama`, and the buyer can do nothing about it.
//
// These tests pin the two REST write paths (POST creates, PATCH rewrites the
// config) to the same refusal the shared customform rule gives, and reproduce
// the trap itself: a form that holds that state answers 400 to the maximal
// payload its own page can produce.
//
// Integration tests; skipped unless JOMFORM_TEST_DATABASE_URL is set.

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

// customFormConfigBody builds a form create body whose config carries a
// custom-HTML section, so the request shape is the one an agent/console sends.
func customFormConfigBody(t *testing.T, title, slug, html string, fields []map[string]any) string {
	t.Helper()
	body := map[string]any{
		"title": title,
		"config": map[string]any{
			"mode": "response",
			customform.ConfigKey: map[string]any{
				"html":   html,
				"fields": fields,
			},
		},
	}
	if slug != "" {
		body["slug"] = slug
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal create body: %v", err)
	}
	return string(raw)
}

// formConfig reads a form's stored config straight out of the database — the
// assertion that matters is what was WRITTEN, not what a response claimed.
func formConfig(t *testing.T, fx renameFixture, slug string) map[string]any {
	t.Helper()
	var raw []byte
	if err := fx.pool.QueryRow(fx.ctx,
		`SELECT config FROM forms WHERE slug=$1 AND account_id=$2 AND deleted_at IS NULL`,
		slug, fx.accountID).Scan(&raw); err != nil {
		t.Fatalf("read stored config for %q: %v", slug, err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("decode stored config: %v (%s)", err, raw)
	}
	return cfg
}

func liveFormCount(t *testing.T, fx renameFixture, slug string) int {
	t.Helper()
	var n int
	if err := fx.pool.QueryRow(fx.ctx,
		`SELECT count(*) FROM forms WHERE slug=$1 AND account_id=$2 AND deleted_at IS NULL`,
		slug, fx.accountID).Scan(&n); err != nil {
		t.Fatalf("count forms on %q: %v", slug, err)
	}
	return n
}

// requiredNama is the exact contract from the card: one required text field.
func requiredNama() []map[string]any {
	return []map[string]any{
		{"field_id": "nama", "type": "text", "label": "Nama", "required": true},
	}
}

func TestCreateFormRefusesUnsubmittableCustomForm(t *testing.T) {
	fx := newRenameFixture(t, "it-cf-guard-"+randHex(4))
	slug := "it-cf-trap-" + randHex(4)

	// The card's shape: required "nama", HTML with no such control.
	code, raw := tplReq(t, fx.h, fx.key, http.MethodPost, "/v1/rest/forms",
		customFormConfigBody(t, "Trap", slug, `<div><label>Emel</label><input type="email" name="emel"></div>`, requiredNama()))
	if code != http.StatusBadRequest {
		t.Fatalf("POST /v1/rest/forms with an unsatisfiable custom form = %d, want 400 (body %s).\n"+
			"Storing it is the carded defect: the form answers 400 `medan wajib tidak diisi: Nama` "+
			"on every submit and no buyer can ever satisfy it.", code, raw)
	}
	if body := string(raw); !strings.Contains(body, "medan wajib tiada dalam HTML") ||
		!strings.Contains(body, `name=\"nama\"`) {
		t.Errorf("400 body = %s, want it to name the missing control (name=\"nama\")", body)
	}
	// A refused write must leave nothing behind.
	if n := liveFormCount(t, fx, slug); n != 0 {
		t.Fatalf("%d live forms on %q after a refused create, want 0 — the unsatisfiable "+
			"contract was stored anyway", n, slug)
	}

	// The same form WITH the control is accepted, so the guard refuses the
	// contract and not custom forms.
	okSlug := "it-cf-ok-" + randHex(4)
	code, raw = tplReq(t, fx.h, fx.key, http.MethodPost, "/v1/rest/forms",
		customFormConfigBody(t, "Ok", okSlug, `<div><input type="text" name="nama"></div>`, requiredNama()))
	if code != http.StatusCreated {
		t.Fatalf("POST with a satisfiable custom form = %d, want 201 (body %s)", code, raw)
	}
	cfg := formConfig(t, fx, okSlug)
	section, _ := cfg[customform.ConfigKey].(map[string]any)
	if section == nil {
		t.Fatalf("stored config = %v, want a custom_html section", cfg)
	}
	if html, _ := section["html"].(string); !strings.Contains(html, `name="nama"`) {
		t.Fatalf("stored custom_html.html = %q, want it to carry the control", html)
	}
}

func TestUpdateFormRefusesUnsubmittableCustomForm(t *testing.T) {
	fx := newRenameFixture(t, "it-cf-upd-"+randHex(4))
	slug := "it-cf-upd-" + randHex(4)

	const goodHTML = `<div><input type="text" name="nama"></div>`
	code, raw := tplReq(t, fx.h, fx.key, http.MethodPost, "/v1/rest/forms",
		customFormConfigBody(t, "Update me", slug, goodHTML, requiredNama()))
	if code != http.StatusCreated {
		t.Fatalf("seed create = %d, want 201 (body %s)", code, raw)
	}
	var created map[string]any
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatalf("decode create response: %v (%s)", err, raw)
	}
	formID, _ := created["id"].(string)
	if formID == "" {
		t.Fatalf("create response carries no id: %s", raw)
	}

	// PATCH the config to a contract with no control — the second write path.
	badBody, err := json.Marshal(map[string]any{
		"title": "Update me",
		"config": map[string]any{
			"mode": "response",
			customform.ConfigKey: map[string]any{
				"html":   `<div><p>tiada apa</p></div>`,
				"fields": requiredNama(),
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal patch body: %v", err)
	}
	code, raw = tplReq(t, fx.h, fx.key, http.MethodPatch, "/v1/rest/forms/"+formID, string(badBody))
	if code != http.StatusBadRequest {
		t.Fatalf("PATCH /v1/rest/forms/{id} introducing an unsatisfiable custom form = %d, want 400 "+
			"(body %s). The update path is the second way that contract reaches storage.", code, raw)
	}
	// Nothing was saved silently: the stored html is still the satisfiable one.
	cfg := formConfig(t, fx, slug)
	section, _ := cfg[customform.ConfigKey].(map[string]any)
	if section == nil {
		t.Fatalf("stored config lost its custom_html section: %v", cfg)
	}
	if html, _ := section["html"].(string); html != goodHTML {
		t.Fatalf("stored custom_html.html = %q after a refused PATCH, want the untouched %q", html, goodHTML)
	}

	// A satisfiable PATCH still lands, so the path is refused for the contract
	// and not disabled.
	const newHTML = `<div><input type="text" name="nama" placeholder="Nama penuh"></div>`
	okBody, err := json.Marshal(map[string]any{
		"title": "Update me",
		"config": map[string]any{
			"mode": "response",
			customform.ConfigKey: map[string]any{
				"html":   newHTML,
				"fields": requiredNama(),
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal patch body: %v", err)
	}
	if code, raw = tplReq(t, fx.h, fx.key, http.MethodPatch, "/v1/rest/forms/"+formID, string(okBody)); code != http.StatusOK {
		t.Fatalf("PATCH with a satisfiable custom form = %d, want 200 (body %s)", code, raw)
	}
	cfg = formConfig(t, fx, slug)
	section, _ = cfg[customform.ConfigKey].(map[string]any)
	if section == nil {
		t.Fatalf("stored config lost its custom_html section after a valid PATCH: %v", cfg)
	}
	if html, _ := section["html"].(string); html != newHTML {
		t.Fatalf("stored custom_html.html = %q after a valid PATCH, want %q", html, newHTML)
	}
}

// TestTemplateWritesRefuseUnsubmittableCustomForm: a template's config is
// cloned onto a real form by POST /v1/rest/templates/{id}/use, so the template
// writers are the back door around the form refusal.
func TestTemplateWritesRefuseUnsubmittableCustomForm(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	acc, _ := bundleFixture(t, ctx, pool)
	key := bundleAPIKey(t, ctx, pool, acc)
	h := newBundleHandlers(pool)

	badCfg := map[string]any{
		"mode": "response",
		customform.ConfigKey: map[string]any{
			"html":   `<div><label>Emel</label><input type="email" name="emel"></div>`,
			"fields": requiredNama(),
		},
	}
	badBody, err := json.Marshal(map[string]any{"title": "tpl trap", "config": badCfg})
	if err != nil {
		t.Fatalf("marshal template body: %v", err)
	}
	code, raw := tplReq(t, h, key, http.MethodPost, "/v1/rest/templates", string(badBody))
	if code != http.StatusBadRequest {
		t.Fatalf("POST /v1/rest/templates with an unsatisfiable custom form = %d, want 400 (body %s)",
			code, raw)
	}
	if body := string(raw); !strings.Contains(body, "medan wajib tiada dalam HTML") {
		t.Errorf("400 body = %s, want the customform refusal", body)
	}
	// Nothing was stored by the refused create.
	code, raw = tplReq(t, h, key, http.MethodGet, "/v1/rest/templates", "")
	if code != http.StatusOK {
		t.Fatalf("list templates = %d (body %s)", code, raw)
	}
	var list []map[string]any
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatalf("decode template list: %v (%s)", err, raw)
	}
	if len(list) != 0 {
		t.Fatalf("refused create_template left %d template(s) behind: %s", len(list), raw)
	}

	// A clean template, then the update path.
	goodCfg := map[string]any{
		"mode": "response",
		customform.ConfigKey: map[string]any{
			"html":   `<div><input type="text" name="nama"></div>`,
			"fields": requiredNama(),
		},
	}
	goodBody, err := json.Marshal(map[string]any{"title": "tpl ok", "config": goodCfg})
	if err != nil {
		t.Fatalf("marshal template body: %v", err)
	}
	code, raw = tplReq(t, h, key, http.MethodPost, "/v1/rest/templates", string(goodBody))
	if code != http.StatusCreated {
		t.Fatalf("create a satisfiable template = %d, want 201 (body %s)", code, raw)
	}
	var created map[string]any
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatalf("decode created template: %v (%s)", err, raw)
	}
	tplID, _ := created["id"].(string)
	if tplID == "" {
		t.Fatalf("created template has no id: %s", raw)
	}

	code, raw = tplReq(t, h, key, http.MethodPut, "/v1/rest/templates/"+tplID, string(badBody))
	if code != http.StatusBadRequest {
		t.Fatalf("PUT /v1/rest/templates/{id} with an unsatisfiable custom form = %d, want 400 (body %s)",
			code, raw)
	}
	// The stored template is untouched.
	code, raw = tplReq(t, h, key, http.MethodGet, "/v1/rest/templates/"+tplID, "")
	if code != http.StatusOK {
		t.Fatalf("read template = %d (body %s)", code, raw)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode template: %v (%s)", err, raw)
	}
	cfg, _ := got["config"].(map[string]any)
	section, _ := cfg[customform.ConfigKey].(map[string]any)
	if section == nil {
		t.Fatalf("stored template lost its custom_html section: %v", got["config"])
	}
	if html, _ := section["html"].(string); !strings.Contains(html, `name="nama"`) {
		t.Fatalf("stored template html = %q after a refused PUT, want the untouched satisfiable one", html)
	}
}

// TestStoredUnsubmittableCustomFormIsATrap reproduces the reported symptom: the
// state the write guard refuses really is permanently unsubmittable. The form is
// written through FormStore (the store has no contract knowledge, and accepts it
// exactly as every row written before this fix did), then the page is fetched
// and the MAXIMAL payload that page can produce is POSTed.
func TestStoredUnsubmittableCustomFormIsATrap(t *testing.T) {
	h, workspaceSlug, formSlug := customFormFixture(t)
	pool := bundleTestPool(t)
	ctx := context.Background()

	var formID, accountID string
	if err := pool.QueryRow(ctx,
		`SELECT id, account_id FROM forms WHERE slug=$1`, formSlug).Scan(&formID, &accountID); err != nil {
		t.Fatalf("read fixture form: %v", err)
	}

	// The trap: required "name" registered, and a served body with no such
	// control.
	cfg := map[string]any{
		"mode": "response",
		customform.ConfigKey: map[string]any{
			"html": `<div><label>Emel</label><input type="email" name="email"></div>`,
			"fields": []any{
				map[string]any{"field_id": "name", "type": "text", "label": "Name", "required": true},
				map[string]any{"field_id": "email", "type": "email", "label": "Email", "required": true},
			},
		},
	}
	if err := dbstore.NewFormStore(pool).Update(ctx, accountID, formID, "Custom form", cfg, nil); err != nil {
		t.Fatalf("store the unsatisfiable config: %v", err)
	}

	// What the buyer's browser actually receives: no name="name" control.
	rec := publicServe(t, h, workspaceSlug+".jomform.com", "/"+formSlug)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /%s = %d, want 200 (body %s)", formSlug, rec.Code, rec.Body.String())
	}
	if page := rec.Body.String(); strings.Contains(page, `name=\"name\"`) ||
		strings.Contains(page, `name="name"`) {
		t.Fatalf("the served page carries a name=\"name\" control, so this is NOT the trap shape")
	}

	before := customFormSubmissions(t, formSlug)
	// The most the page can send: the one control it has (the bridge fills
	// client.full_name from the first text input, and there is none here).
	req := httptest.NewRequest(http.MethodPost, "/"+formSlug+"/order",
		bytes.NewBufferString(`{"client":{},"fields":{"email":"ali@example.test"}}`))
	req.Host = workspaceSlug + ".jomform.com"
	req.Header.Set("Content-Type", "application/json")
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("submit of the page's own maximal payload = %d, want 400 (body %s).\n"+
			"A 201 would mean the trap is not real; the 400 is the reported symptom that the "+
			"write-time refusal prevents.", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "medan wajib tidak diisi") ||
		!strings.Contains(body, "Name") {
		t.Errorf("400 body = %s, want `medan wajib tidak diisi: Name` — the server enforces the "+
			"registered required field the page cannot supply", body)
	}
	if got := customFormSubmissions(t, formSlug); got != before {
		t.Fatalf("refused submit stored a row: %d before, %d after", before, got)
	}
}
