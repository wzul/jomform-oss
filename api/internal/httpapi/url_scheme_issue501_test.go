package httpapi

// Issue #501: caller-supplied image/URL fields were written WITHOUT a
// consistent scheme rule, so an unsafe scheme (`data:`, `javascript:`, `http:`,
// …) that reached `<img src>` was silently rewritten by html/template to the
// `#ZgotmplZ` sentinel — the image never loads and the page still returns HTTP
// 200. Same silent symptom class as #499, but the source is the OPPOSITE: these
// values are CALLER input, so they are REJECTED at write time (a clear 400),
// never trusted (`template.URL` on caller input would let a merchant put an
// arbitrary scheme in the buyer's page).
//
// Every write-path test below therefore asserts TWO things:
//
//  1. the write is REJECTED (400) for a non-https value, while a valid https
//     URL and an empty/omitted value still pass (empty = "keep existing"), and
//  2. the EMITTED attribute is never `#ZgotmplZ` — a status-only test would
//     have passed on the broken page (the #499 lesson).
//
// The scheme rule itself is the ONE shared validator in internal/urlguard;
// every REST path under test calls it.

import (
	"bytes"
	"context"
	"encoding/json"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// issue501Fixture seeds a user + workspace + account and returns the REST
// handlers, an API key for that account, and the account id.
func issue501Fixture(t *testing.T) (*RESTHandlers, string, string, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, _ := bundleFixture(t, ctx, pool)
	return newBundleHandlers(pool), bundleAPIKey(t, ctx, pool, accountID), accountID, pool
}

// issue501Do routes one API-key request through the REST mux.
func issue501Do(t *testing.T, h *RESTHandlers, rawKey, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+rawKey)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// issue501JSON decodes a response body into a map.
func issue501JSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("response is not JSON: %v (%s)", err, rec.Body.String())
	}
	return out
}

// ── REST: product images (create + update) ──────────────────────────────────

func TestIssue501RESTProductImagesRejectNonHTTPS(t *testing.T) {
	h, raw, _, _ := issue501Fixture(t)

	rec := issue501Do(t, h, raw, http.MethodPost, "/v1/rest/products",
		`{"name":"Tee","price_sen":1000,"images":["data:image/png;base64,AAAA"]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST product with a data: image = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	// The message must name the offending value/field so the UI can show it.
	if !strings.Contains(rec.Body.String(), "https://") {
		t.Errorf("400 body does not explain the https:// rule: %s", rec.Body.String())
	}

	// A valid https image still works…
	if rec := issue501Do(t, h, raw, http.MethodPost, "/v1/rest/products",
		`{"name":"Tee","price_sen":1000,"images":["https://jomform.com/media/u/acct/uuid.jpg"]}`); rec.Code != http.StatusCreated {
		t.Fatalf("POST with an https image = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	// …and so does a product with no images at all.
	if rec := issue501Do(t, h, raw, http.MethodPost, "/v1/rest/products",
		`{"name":"Tee2","price_sen":1000,"images":[]}`); rec.Code != http.StatusCreated {
		t.Fatalf("POST with no images = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestIssue501RESTProductUpdateImagesRejectNonHTTPS(t *testing.T) {
	h, raw, _, _ := issue501Fixture(t)
	create := issue501Do(t, h, raw, http.MethodPost, "/v1/rest/products",
		`{"name":"Tee","price_sen":1000,"images":["https://jomform.com/media/u/a/x.jpg"]}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("seed product: %d (%s)", create.Code, create.Body.String())
	}
	path := "/v1/rest/products/" + issue501JSON(t, create)["id"].(string)

	// An explicit non-https list is rejected.
	if rec := issue501Do(t, h, raw, http.MethodPut, path, `{"images":["javascript:alert(1)"]}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT with a javascript: image = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	// An OMITTED images key keeps the stored images and is still accepted.
	if rec := issue501Do(t, h, raw, http.MethodPut, path, `{"name":"Tee renamed"}`); rec.Code != http.StatusOK {
		t.Fatalf("PUT omitting images = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	// A valid https replacement still works.
	if rec := issue501Do(t, h, raw, http.MethodPut, path,
		`{"images":["https://jomform.com/media/u/a/y.jpg"]}`); rec.Code != http.StatusOK {
		t.Fatalf("PUT with an https image = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}

// ── REST: form theme logo_url (PUT /forms/{id}/settings) ────────────────────

func TestIssue501RESTFormSettingsLogoRejectsNonHTTPS(t *testing.T) {
	h, raw, _, _ := issue501Fixture(t)
	create := issue501Do(t, h, raw, http.MethodPost, "/v1/rest/forms", `{"title":"Logo form"}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("seed form: %d (%s)", create.Code, create.Body.String())
	}
	path := "/v1/rest/forms/" + issue501JSON(t, create)["id"].(string) + "/settings"

	rec := issue501Do(t, h, raw, http.MethodPut, path, `{"logo_url":"data:image/png;base64,AAAA"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT form settings with a data: logo = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "logo_url") {
		t.Errorf("400 body does not name the offending field: %s", rec.Body.String())
	}
	// A valid https logo passes.
	if rec := issue501Do(t, h, raw, http.MethodPut, path,
		`{"logo_url":"https://jomform.com/media/u/a/logo.png"}`); rec.Code != http.StatusOK {
		t.Fatalf("PUT with an https logo = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	// An empty logo (clear) passes.
	if rec := issue501Do(t, h, raw, http.MethodPut, path, `{"logo_url":""}`); rec.Code != http.StatusOK {
		t.Fatalf("PUT with an empty logo = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}

// ── REST: builder image-block src, product snapshot images, theme logo ──────

func TestIssue501RESTFormConfigAndThemeURLsRejectNonHTTPS(t *testing.T) {
	h, raw, _, _ := issue501Fixture(t)

	cases := []struct {
		name string
		body string
		want string
	}{
		{
			"image block src",
			`{"title":"Builder form","config":{"blocks":[{"id":"b1","type":"image","src":"data:image/png;base64,AA"}]}}`,
			"blocks[].src",
		},
		{
			"product snapshot images",
			`{"title":"Snapshot form","config":{"products":[{"id":"p1","images":["ftp://x/y.png"]}]}}`,
			"products[].images",
		},
		{
			"theme logo_url",
			`{"title":"Theme form","theme":{"logo_url":"javascript:alert(1)"}}`,
			"logo_url",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := issue501Do(t, h, raw, http.MethodPost, "/v1/rest/forms", tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("POST /v1/rest/forms = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.want) {
				t.Errorf("400 body does not name %q: %s", tc.want, rec.Body.String())
			}
		})
	}

	// The same shapes with https values still save…
	ok := `{"title":"Good form","config":{"blocks":[{"id":"b1","type":"image","src":"https://jomform.com/media/u/a/x.png"}]},"theme":{"logo_url":"https://jomform.com/media/u/a/logo.png"}}`
	if rec := issue501Do(t, h, raw, http.MethodPost, "/v1/rest/forms", ok); rec.Code != http.StatusCreated {
		t.Fatalf("POST with https config/theme = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	// …and so does a form with neither config nor theme.
	if rec := issue501Do(t, h, raw, http.MethodPost, "/v1/rest/forms", `{"title":"Bare form"}`); rec.Code != http.StatusCreated {
		t.Fatalf("POST with no config/theme = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestIssue501RESTFormUpdateConfigURLRejectsNonHTTPS covers the PATCH path
// (the builder's save), which previously accepted these maps verbatim.
func TestIssue501RESTFormUpdateConfigURLRejectsNonHTTPS(t *testing.T) {
	h, raw, _, _ := issue501Fixture(t)
	create := issue501Do(t, h, raw, http.MethodPost, "/v1/rest/forms", `{"title":"Patch form"}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("seed form: %d (%s)", create.Code, create.Body.String())
	}
	path := "/v1/rest/forms/" + issue501JSON(t, create)["id"].(string)

	rec := issue501Do(t, h, raw, http.MethodPatch, path,
		`{"config":{"blocks":[{"id":"b1","type":"image","src":"javascript:alert(1)"}]}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PATCH with a javascript: image-block src = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	rec = issue501Do(t, h, raw, http.MethodPatch, path, `{"theme":{"logo_url":"http://cdn.example.com/logo.png"}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PATCH with an http logo_url = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	// The legitimate patch still works.
	if rec := issue501Do(t, h, raw, http.MethodPatch, path,
		`{"config":{"blocks":[{"id":"b1","type":"image","src":"https://jomform.com/media/u/a/x.png"}]}}`); rec.Code != http.StatusOK {
		t.Fatalf("PATCH with an https image-block src = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}

// ── REST: workspace landing image_url / logo_url ─────────────────────────────

func TestIssue501RESTWorkspaceLandingURLsRejectNonHTTPS(t *testing.T) {
	// The workspace-settings endpoint is session-scoped (it resolves the
	// caller from the session cookie, not an API key), so this test uses the
	// RBAC session fixture: an owner + workspace + account.
	ctx := context.Background()
	pool := bundleTestPool(t)
	owner := rbacUser(t, ctx, pool)
	ws, err := dbstore.NewWorkspaceStore(pool).Create(ctx, owner, "Issue 501 WS", "i501-"+randSuffix())
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	h := rbacHandlers(t, pool)
	tok := rbacSessionCookie(t, h, owner)
	path := "/v1/workspaces/" + ws.ID + "/settings"

	for _, bad := range []struct {
		field string
		body  map[string]any
	}{
		{"image_url", map[string]any{"landing_page": map[string]any{"enabled": true, "image_url": "data:image/png;base64,AA"}}},
		{"logo_url", map[string]any{"landing_page": map[string]any{"enabled": true, "logo_url": "http://cdn.example.com/logo.png"}}},
	} {
		rec := rbacDo(t, h, http.MethodPut, path, tok, bad.body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: PUT landing page = %d, want 400 (body %s)", bad.field, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), bad.field) {
			t.Errorf("%s: 400 body does not name the field: %s", bad.field, rec.Body.String())
		}
	}
	// A valid https landing page saves…
	okBody := map[string]any{"landing_page": map[string]any{
		"enabled": true, "hero": "Hi",
		"image_url": "https://jomform.com/media/u/a/banner.jpg",
		"logo_url":  "https://jomform.com/media/u/a/logo.png",
	}}
	if rec := rbacDo(t, h, http.MethodPut, path, tok, okBody); rec.Code != http.StatusOK {
		t.Fatalf("PUT https landing page = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	// …and an omitted landing_page keeps the stored one.
	if rec := rbacDo(t, h, http.MethodPut, path, tok, map[string]any{"footer_text": "hi"}); rec.Code != http.StatusOK {
		t.Fatalf("PUT without landing_page = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}

// ── REST: template config/theme (cloned onto real forms) ────────────────────

func TestIssue501RESTTemplateConfigURLsRejectNonHTTPS(t *testing.T) {
	h, raw, _, _ := issue501Fixture(t)
	create := issue501Do(t, h, raw, http.MethodPost, "/v1/rest/templates", `{"title":"Tpl"}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("seed template: %d (%s)", create.Code, create.Body.String())
	}
	path := "/v1/rest/templates/" + issue501JSON(t, create)["id"].(string)

	rec := issue501Do(t, h, raw, http.MethodPut, path,
		`{"config":{"blocks":[{"id":"b1","type":"image","src":"data:image/png;base64,AA"}]}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT template with a data: image-block src = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if rec := issue501Do(t, h, raw, http.MethodPost, "/v1/rest/templates",
		`{"title":"Tpl2","theme":{"logo_url":"javascript:alert(1)"}}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("POST template with a javascript: logo = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	// The legitimate template still saves.
	if rec := issue501Do(t, h, raw, http.MethodPut, path,
		`{"config":{"blocks":[{"id":"b1","type":"image","src":"https://jomform.com/media/u/a/x.png"}]}}`); rec.Code != http.StatusOK {
		t.Fatalf("PUT template with an https src = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}

// ── The emitted attributes (the #499 lesson) ────────────────────────────────

// TestIssue501EmittedAttributesNeverZgotmplZ pins the rendered output for every
// field in scope: a legitimate https value — including an absolute
// media-library URL of the shape objstore.PublicURL returns — must appear
// verbatim in the emitted attribute and must never be replaced by the
// html/template sentinel.
func TestIssue501EmittedAttributesNeverZgotmplZ(t *testing.T) {
	mediaURL := "https://jomform.com/media/u/00000000-0000-0000-0000-000000000000/11111111-2222-3333-4444-555555555555.jpg"

	t.Run("form theme logo", func(t *testing.T) {
		body := issue501Render(blockTmpl, map[string]any{
			"Slug": "demo", "Title": "Demo", "LogoURL": mediaURL,
			"Blocks": []any{}, "Ls": formLangs["en"],
		})
		issue501AssertEmitted(t, body, `src="`+mediaURL+`"`)
	})

	t.Run("builder image block src", func(t *testing.T) {
		body := issue501Render(blockTmpl, map[string]any{
			"Slug": "demo", "Title": "Demo", "Ls": formLangs["en"],
			"Blocks": []any{map[string]any{
				"ID": "b1", "Type": "image", "Src": mediaURL, "Value": "", "Style": "",
				"MaxLength": 0, "PriceSen": int64(0), "Choices": []any{}, "Variations": []any{},
			}},
		})
		issue501AssertEmitted(t, body, `class="img" src="`+mediaURL+`"`)
	})

	t.Run("landing logo and banner", func(t *testing.T) {
		body := issue501Render(landingPageTmpl, workspaceLandingData{
			Slug: "shop", HasForms: true,
			Lp: &dbstore.LandingPage{Enabled: true, Hero: "Hi", ImageURL: mediaURL, LogoURL: mediaURL},
		})
		issue501AssertEmitted(t, body, `class="logo" src="`+mediaURL+`"`)
		issue501AssertEmitted(t, body, `class="banner" src="`+mediaURL+`"`)
	})

	t.Run("menu product image", func(t *testing.T) {
		h := &PublicHandlers{}
		v := h.menuProductView("p1", "Nasi Lemak", "Pedes", 800, mediaURL, false, "Best-seller", "Add")
		body := issue501Render(menuPageTmpl, workspaceMenuData{
			Slug: "menu", Menu: &dbstore.MenuPage{Enabled: true},
			Th:         menuTheme{Hero: "Warung Ali"},
			Ls:         formLangs["en"],
			Categories: []menuCategoryView{{Name: "Makanan", Products: []menuProductView{v}}},
			HasItems:   true,
		})
		issue501AssertEmitted(t, body, mediaURL)
	})

	t.Run("product image in the form renderer", func(t *testing.T) {
		body := issue501Render(blockTmpl, map[string]any{
			"Slug": "demo", "Title": "Demo", "Ls": formLangs["en"],
			"Blocks": []any{map[string]any{
				"ID": "b1", "Type": "product", "Value": "Nasi Lemak", "ProductID": "p1",
				"PriceSen": int64(800), "HasPrice": true, "Image": mediaURL, "BaseImage": mediaURL,
				"ImgSize": "small", "HasQuantity": true, "Style": "",
				"MaxLength": 0, "Choices": []any{}, "Variations": []any{},
			}},
		})
		issue501AssertEmitted(t, body, mediaURL)
	})
}

// issue501Render executes a template with data and returns the HTML.
func issue501Render(tmpl *template.Template, data any) string {
	buf := &bytes.Buffer{}
	if err := tmpl.Execute(buf, data); err != nil {
		// Failing here is a test bug, not a product bug — surface it loudly.
		panic("issue501Render: " + err.Error())
	}
	return buf.String()
}

// issue501AssertEmitted asserts the attribute value landed verbatim and the
// html/template sentinel is absent. A status-only assertion would have passed
// on the broken page (#499).
func issue501AssertEmitted(t *testing.T, body, want string) {
	t.Helper()
	if strings.Contains(body, "ZgotmplZ") {
		t.Fatalf("rendered HTML contains the html/template sentinel #ZgotmplZ — the image will not load\nbody:\n%s", body)
	}
	if !strings.Contains(body, want) {
		t.Fatalf("emitted attribute missing %q\nbody:\n%s", want, body)
	}
}
