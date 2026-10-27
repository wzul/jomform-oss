package httpapi

// Card t_754ed4b4 — a custom-HTML response form's derived buyer identity.
//
// THE DEFECT. render_custom.go mapped the submitter's identity by POSITION and
// input TYPE, exactly as the palette renderer did before card t_98d67234:
//
//     var nm=doc.querySelector('input[type=text]'); if(nm) body.client.full_name=nm.value;
//
// On a custom-HTML form the body is the MERCHANT'S OWN markup, so the first
// input[type=text] is whatever they put first — a company field, a subject
// line — not necessarily the responder's name. Worse than the palette surface:
// handleResponseSubmit folds client.full_name onto the recorded answers AFTER
// customform.Collect (public.go), so a merchant who DID name their control
// name="full_name" had that correct answer OVERWRITTEN by the first text
// control's value. From there the value is the responses list's Buyer column
// (rest_handlers.go) and the export (rest_export.go).
//
// WHAT THIS FILE DRIVES. The page is rendered by the handler that serves it
// (renderCustomForm), and its OWN bridge script is executed in the shared goja
// + cascadia harness (menu_dom_test.go) against that same served markup: the
// script writes the sanitized body into the iframe's srcdoc, the harness's
// contentDocument exposes that frame document, the frame's load event makes the
// bridge build the form, an answer is typed into each control, and the page's
// own submit handler is fired. The JSON body the PAGE built is then posted
// through the real public submit route and read back from the submission store.
//
// THE BITE. Restoring the pre-fix lookup (type only, no name step) reds
// TestCustomResponseFormIdentityFollowsTheDeclaredName and the stored-submission
// test: full_name becomes the company control's answer instead of the
// name="full_name" answer.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/andybalholm/cascadia"

	"github.com/wzul/jomform/api/internal/customform"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// The answers are deliberately distinct strings, so a wrong arm cannot pass by
// coincidence: the COMPANY control is the first input[type=text] on the page,
// and the work/mobile controls are the first input[type=email] / input[type=tel].
const (
	customCompanyAnswer   = "ACME-t754ed4b4"
	customNameAnswer      = "QANAME-t754ed4b4"
	customEmailAnswer     = "qaname-t754ed4b4@example.test"
	customPhoneAnswer     = "60123456789"
	customWorkEmailAnswer = "work-t754ed4b4@example.test"
	customMobileAnswer    = "60111111111"
)

// customIdentityConfig is the shape the card names — a text control the body
// does NOT declare as the identity, followed by name="full_name" — plus the
// same trap on the other two arms: the declared email/phone controls are plain
// text inputs (ordinary markup) while the typed controls that follow them are
// type=email / type=tel, so only a name-first lookup answers correctly.
func customIdentityConfig() map[string]any {
	return map[string]any{
		"mode": customform.ModeResponse,
		customform.ConfigKey: map[string]any{
			"html": `<div><label>Company</label><input type="text" name="company"></div>` +
				`<div><label>Your name</label><input type="text" name="full_name"></div>` +
				`<div><label>Email</label><input type="text" name="email"></div>` +
				`<div><label>Work email</label><input type="email" name="work_email"></div>` +
				`<div><label>Phone</label><input type="text" name="phone"></div>` +
				`<div><label>Mobile</label><input type="tel" name="mobile"></div>`,
			"fields": []any{
				map[string]any{"field_id": "company", "type": "text", "label": "Company"},
				map[string]any{"field_id": "full_name", "type": "text", "label": "Your name"},
				map[string]any{"field_id": "email", "type": "text", "label": "Email"},
				map[string]any{"field_id": "work_email", "type": "email", "label": "Work email"},
				map[string]any{"field_id": "phone", "type": "text", "label": "Phone"},
				map[string]any{"field_id": "mobile", "type": "tel", "label": "Mobile"},
			},
		},
	}
}

// customIdentityAnswers fills every control above with its own distinct answer.
func customIdentityAnswers() map[string]string {
	return map[string]string{
		"company":    customCompanyAnswer,
		"full_name":  customNameAnswer,
		"email":      customEmailAnswer,
		"work_email": customWorkEmailAnswer,
		"phone":      customPhoneAnswer,
		"mobile":     customMobileAnswer,
	}
}

// renderCustomPage serves the custom-HTML page a buyer gets, through the handler
// that renders it, for an in-memory form — no database needed.
func renderCustomPage(t *testing.T, slug string, cfg map[string]any) string {
	t.Helper()
	f := &dbstore.Form{
		AccountID: "it-account", Slug: slug, Title: "Borang tersuai",
		Status: dbstore.FormStatusPublished, Config: cfg,
		Theme: map[string]any{"primary": "#5e6ad2"},
	}
	h := &PublicHandlers{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://demo.jomform.com/"+slug, nil)
	if !h.renderCustomForm(rec, req, f) {
		t.Fatalf("renderCustomForm declined a form carrying custom_html: %v", cfg)
	}
	return rec.Body.String()
}

// customPageSubmit runs the served custom page's own bridge script against that
// same served markup and returns the JSON body the PAGE built after a responder
// typed one answer per control and submitted.
func customPageSubmit(t *testing.T, page, slug string, answers map[string]string) string {
	t.Helper()
	d := newPageDOM(t, page)
	d.pathname = "/" + slug
	// The page replaces the card with its confirmation on success; answer with
	// the success shape so a failure alert is never mistaken for a broken page.
	d.orderResponse = map[string]any{"status": "submitted", "message": "ok"}
	d.runPageScript(page, responseSubmitScriptMarker)

	frame := d.byID["jf-custom"]
	if frame == nil {
		t.Fatalf("the served custom page has no #jf-custom iframe (page: %s)", page)
	}
	frameDoc := d.frameDocs[frame]
	if frameDoc == nil {
		t.Fatalf("the page's bridge wrote no srcdoc into the frame — the merchant body never reached it")
	}

	// The browser fires load once srcdoc is in place; that is what makes the
	// bridge build the frame's form and hook its submit.
	d.dispatch("load", frame)

	for name, val := range answers {
		n := cascadia.Query(frameDoc, cascadia.MustCompile(`[name="`+name+`"]`))
		if n == nil {
			t.Fatalf("the frame carries no control named %q (srcdoc: %s)", name, d.frameSrc[frame])
		}
		setMenuAttr(n, "value", val)
	}

	form := cascadia.Query(frameDoc, cascadia.MustCompile("form"))
	if form == nil {
		t.Fatalf("the bridge did not build a form inside the frame")
	}
	d.dispatch("submit", form)

	posted := d.storage["__posted_body"]
	if strings.TrimSpace(posted) == "" {
		t.Fatalf("the served custom page's own script posted no body (requests: %v)", d.fetches)
	}
	return posted
}

// The served script must select by DECLARATION first and by type second, so the
// mapping cannot drift back to a positional guess unnoticed. This is the same
// source-level half the palette renderer's guard carries.
func TestCustomResponseFormScriptMapsByDeclaredNameFirst(t *testing.T) {
	page := renderCustomPage(t, "custom-identity", customIdentityConfig())

	for _, want := range []string{
		"doc.querySelector('[name=full_name]')||doc.querySelector('input[type=text]')",
		"doc.querySelector('[name=email]')||doc.querySelector('input[type=email]')",
		"doc.querySelector('[name=phone]')||doc.querySelector('input[type=tel]')",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the served bridge script does not map by declared name first: missing %q", want)
		}
	}
}

// The card's item 2 on the rendered page: with a company control BEFORE the
// name control, the derived client identity is the name the body declares — for
// all three arms — and the collected answers are untouched.
func TestCustomResponseFormIdentityFollowsTheDeclaredName(t *testing.T) {
	page := renderCustomPage(t, "custom-identity", customIdentityConfig())
	posted := customPageSubmit(t, page, "custom-identity", customIdentityAnswers())
	got := decodeServedSubmitBody(t, posted)

	if got.Client.FullName != customNameAnswer {
		t.Errorf("client.full_name = %q, want %q (the name=\"full_name\" control). The first "+
			"input[type=text] on this page is the company control, whose answer is %q — a "+
			"company field is not the responder's name (body: %s)",
			got.Client.FullName, customNameAnswer, customCompanyAnswer, posted)
	}
	if got.Client.Email != customEmailAnswer {
		t.Errorf("client.email = %q, want %q (the name=\"email\" control, which is a plain text "+
			"input); the typed control on this page is work_email = %q",
			got.Client.Email, customEmailAnswer, customWorkEmailAnswer)
	}
	if got.Client.Phone != customPhoneAnswer {
		t.Errorf("client.phone = %q, want %q (the name=\"phone\" control, which is a plain text "+
			"input); the typed control on this page is mobile = %q",
			got.Client.Phone, customPhoneAnswer, customMobileAnswer)
	}
	// The fix changes the DERIVED identity, never the collected answers.
	if got.Fields["company"] != customCompanyAnswer {
		t.Errorf("fields.company = %q, want %q — the company answer must be untouched",
			got.Fields["company"], customCompanyAnswer)
	}
	if got.Fields["full_name"] != customNameAnswer {
		t.Errorf("fields.full_name = %q, want %q — the declared name answer must be untouched",
			got.Fields["full_name"], customNameAnswer)
	}
}

// The fallback must survive: a body that declares no identity-named control keeps
// exactly the mapping it had before this card (first input[type=text] /
// [type=email]), and each arm falls back independently.
func TestCustomResponseFormWithoutADeclaredIdentityKeepsTheTypedMapping(t *testing.T) {
	for _, tc := range []struct {
		name  string
		html  string
		want  map[string]string
		fills map[string]string
	}{
		{
			name: "no identity-named control at all",
			html: `<div><label>Company</label><input type="text" name="company"></div>` +
				`<div><label>Work email</label><input type="email" name="work_email"></div>`,
			fills: map[string]string{"company": customCompanyAnswer, "work_email": customWorkEmailAnswer},
			want:  map[string]string{"full_name": customCompanyAnswer, "email": customWorkEmailAnswer},
		},
		{
			// Only the name arm is declared: identity is per-arm, so email keeps
			// falling back to input[type=email].
			name: "only full_name is declared",
			html: `<div><label>Company</label><input type="text" name="company"></div>` +
				`<div><label>Your name</label><input type="text" name="full_name"></div>` +
				`<div><label>Work email</label><input type="email" name="work_email"></div>`,
			fills: map[string]string{
				"company": customCompanyAnswer, "full_name": customNameAnswer,
				"work_email": customWorkEmailAnswer,
			},
			want: map[string]string{"full_name": customNameAnswer, "email": customWorkEmailAnswer},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := map[string]any{
				"mode":               customform.ModeResponse,
				customform.ConfigKey: map[string]any{"html": tc.html, "fields": []any{}},
			}
			page := renderCustomPage(t, "custom-fallback", cfg)
			got := decodeServedSubmitBody(t, customPageSubmit(t, page, "custom-fallback", tc.fills))
			if got.Client.FullName != tc.want["full_name"] {
				t.Errorf("client.full_name = %q, want %q — a body with no declared identity keeps "+
					"the pre-card mapping", got.Client.FullName, tc.want["full_name"])
			}
			if got.Client.Email != tc.want["email"] {
				t.Errorf("client.email = %q, want %q — the type-based arm is the per-arm fallback",
					got.Client.Email, tc.want["email"])
			}
		})
	}
}

// The card's item 2 end-to-end: the identity the PAGE derived is what the public
// submit route STORES, and the declared name answer is not overwritten by the
// fold at public.go. Real Postgres; skipped without JOMFORM_TEST_DATABASE_URL.
func TestCustomResponseFormDeclaredIdentityReachesTheStoredSubmission(t *testing.T) {
	h, workspaceSlug, formSlug := customFormFixture(t)
	storeFormConfig(t, formSlug, "Borang tersuai", customIdentityConfig())
	host := workspaceSlug + ".jomform.com"

	rec := publicServe(t, h, host, "/"+formSlug)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /%s = %d, want 200 (body %s)", formSlug, rec.Code, rec.Body.String())
	}
	posted := customPageSubmit(t, rec.Body.String(), formSlug, customIdentityAnswers())

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, "/"+formSlug+"/order", bytes.NewBufferString(posted))
	req.Host = host
	req.Header.Set("Content-Type", "application/json")
	submitRec := httptest.NewRecorder()
	mux.ServeHTTP(submitRec, req)
	if submitRec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d, want 201 (body %s; posted %s)",
			submitRec.Code, submitRec.Body.String(), posted)
	}
	var resp struct {
		Status string `json:"status"`
		ID     string `json:"id"`
	}
	if err := json.Unmarshal(submitRec.Body.Bytes(), &resp); err != nil || resp.ID == "" {
		t.Fatalf("decode submit response: %v (body %s)", err, submitRec.Body.String())
	}

	sub, err := h.Submissions.ByID(context.Background(), resp.ID)
	if err != nil {
		t.Fatalf("fetch stored submission: %v", err)
	}
	stored, _ := sub.Data["full_name"].(string)
	if stored != customNameAnswer {
		t.Errorf("stored data[full_name] = %q, want %q — the value behind the responses Buyer "+
			"column and the export. %q is the company control's answer, which the positional "+
			"lookup used to fold over the correctly named one (stored data: %v)",
			stored, customNameAnswer, customCompanyAnswer, sub.Data)
	}
	if company, _ := sub.Data["company"].(string); company != customCompanyAnswer {
		t.Errorf("stored data[company] = %q, want %q — the company answer must stay its own field",
			company, customCompanyAnswer)
	}
}
