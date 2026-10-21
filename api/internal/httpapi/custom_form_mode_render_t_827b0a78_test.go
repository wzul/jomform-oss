package httpapi

// Card t_827b0a78 — the buyer-visible half. A form stored with an ENABLED
// custom_html section but no `"mode":"response"` rendered the payment/legacy
// layout: serveForm reached the custom renderer only inside the
// `formMode(f) == response` branch, so the merchant's body was never painted and
// the buyer got a dead-end payment page (no products, nothing to submit).
//
// The row here is written through FormStore — the store has no opinion about
// the mode, which is exactly how every such row got into storage (a raw-config
// REST/MCP write before the write side stored the mode beside the section) and
// how a template written before it would clone one. The fix is in dbstore's
// FormMode rule, so the page AND the submit dispatch (and the entry limit, and
// the dashboard label) answer the same question.
//
// Integration tests; skipped unless JOMFORM_TEST_DATABASE_URL is set.

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/customform"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// customBodyNoMode is the shape a raw config write left behind: an enabled
// section with a registered field, and no mode key at all. The marker sentence
// has no attribute quotes, so it survives being embedded in the page's JS
// string and can be searched for verbatim.
func customBodyNoMode() map[string]any {
	return map[string]any{
		customform.ConfigKey: map[string]any{
			"html": `<div><p>Pendaftaran bengkel</p>` +
				`<label>E-mel</label><input type="email" name="email" required></div>`,
			"fields": []any{
				map[string]any{"field_id": "email", "type": "email", "label": "E-mel", "required": true},
			},
		},
	}
}

// storeFormConfig writes a form's config straight through the store, bypassing
// the write guard — the state every row written before this fix is in.
func storeFormConfig(t *testing.T, formSlug, title string, cfg map[string]any) (formID, accountID string) {
	t.Helper()
	pool := bundleTestPool(t)
	ctx := context.Background()
	if err := pool.QueryRow(ctx,
		`SELECT id, account_id FROM forms WHERE slug=$1 AND deleted_at IS NULL`,
		formSlug).Scan(&formID, &accountID); err != nil {
		t.Fatalf("read fixture form %q: %v", formSlug, err)
	}
	if err := dbstore.NewFormStore(pool).Update(ctx, accountID, formID, title, cfg, nil); err != nil {
		t.Fatalf("store config: %v", err)
	}
	return formID, accountID
}

// TestCustomFormWithoutModeRendersTheCustomPage is the card's acceptance test:
// the page the buyer sees must be the merchant's custom form (iframe#jf-custom
// carrying their body), not the payment/legacy layout.
//
// Red before the fix: serveForm asks formMode first, formMode said "payment",
// so the page carried id="of" (the legacy payment form) and no jf-custom iframe.
func TestCustomFormWithoutModeRendersTheCustomPage(t *testing.T) {
	h, workspaceSlug, formSlug := customFormFixture(t)
	storeFormConfig(t, formSlug, "Borang tersuai", customBodyNoMode())

	rec := publicServe(t, h, workspaceSlug+".jomform.com", "/"+formSlug)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /%s = %d, want 200 (body %s)", formSlug, rec.Code, rec.Body.String())
	}
	page := rec.Body.String()

	// The custom page's own markup: the sandboxed iframe and the page's CTA.
	if !strings.Contains(page, `id="jf-custom"`) {
		t.Fatalf("the served page has no custom-form iframe (id=\"jf-custom\").\n"+
			"The config carries an ENABLED custom_html section, so the buyer must get the "+
			"merchant's form — it rendered:\n%s", firstChunk(page))
	}
	if !strings.Contains(page, `id="jf-submit"`) {
		t.Errorf("the served page has no custom-form submit button (id=\"jf-submit\")")
	}
	// The merchant's body rides into the page as the iframe's srcdoc.
	if !strings.Contains(page, "Pendaftaran bengkel") {
		t.Fatalf("the served page does not carry the merchant's body (\"Pendaftaran bengkel\") — "+
			"the custom renderer was not reached.\n%s", firstChunk(page))
	}
	// The control: the payment/legacy layout must NOT be what the buyer got.
	if strings.Contains(page, `id="of"`) {
		t.Fatalf("the served page IS the legacy payment form (id=\"of\") — a custom-HTML form "+
			"with no mode key rendered as a payment form.\n%s", firstChunk(page))
	}
}

// TestCustomFormWithoutModeSubmitsAsAResponse pins the OTHER end: the page and
// the server must agree about what this form is. If the page renders custom but
// the submit path still treats the form as a payment order, the buyer fills the
// merchant's form and the POST is answered 400 "butiran tidak lengkap" (the
// custom page posts {client, fields} and no items), which is a worse dead end
// than the bug itself. The submission is what must be recorded — not a sale.
func TestCustomFormWithoutModeSubmitsAsAResponse(t *testing.T) {
	h, workspaceSlug, formSlug := customFormFixture(t)
	formID, _ := storeFormConfig(t, formSlug, "Borang tersuai", customBodyNoMode())

	before := customFormSubmissions(t, formSlug)
	pool := bundleTestPool(t)
	var salesBefore int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM sales WHERE form_id=$1`, formID).Scan(&salesBefore); err != nil {
		t.Fatalf("count sales: %v", err)
	}

	// Exactly what the custom page's bridge posts: a client block and the
	// frame's named controls.
	req := httptest.NewRequest(http.MethodPost, "/"+formSlug+"/order",
		bytes.NewBufferString(`{"client":{"email":"ali@example.test","full_name":"Ali"},`+
			`"fields":{"email":"ali@example.test"}}`))
	req.Host = workspaceSlug + ".jomform.com"
	req.Header.Set("Content-Type", "application/json")
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("submit of the page's own payload = %d, want 201 (body %s).\n"+
			"A 400 here means the page asks for one contract while /order enforces another.",
			rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, `"status":"submitted"`) {
		t.Fatalf("submit response = %s, want status \"submitted\" (a response-form entry)", body)
	}
	if got := customFormSubmissions(t, formSlug); got != before+1 {
		t.Errorf("submissions for %s = %d, want %d — the entry was not recorded", formSlug, got, before+1)
	}
	var salesAfter int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM sales WHERE form_id=$1`, formID).Scan(&salesAfter); err != nil {
		t.Fatalf("count sales: %v", err)
	}
	if salesAfter != salesBefore {
		t.Errorf("sales for %s went %d -> %d — a custom form must never create a sale",
			formSlug, salesBefore, salesAfter)
	}
}

// TestPaymentFormWithoutCustomSectionStillRendersPaymentPage is the control arm:
// the fix must not turn ordinary payment forms into custom forms. A form with NO
// custom section (here the plain payment config every builder form has) keeps the
// payment layout.
func TestPaymentFormWithoutCustomSectionStillRendersPaymentPage(t *testing.T) {
	h, workspaceSlug, formSlug := customFormFixture(t)
	storeFormConfig(t, formSlug, "Borang bayaran", map[string]any{"mode": "payment", "blocks": []any{}})

	rec := publicServe(t, h, workspaceSlug+".jomform.com", "/"+formSlug)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /%s = %d, want 200 (body %s)", formSlug, rec.Code, rec.Body.String())
	}
	page := rec.Body.String()
	if strings.Contains(page, `id="jf-custom"`) {
		t.Fatalf("a payment form with no custom section rendered the custom-form iframe — the "+
			"mode rule is now claiming every form.\n%s", firstChunk(page))
	}
	if !strings.Contains(page, `id="of"`) {
		t.Fatalf("a payment form with no custom section did not render the payment layout "+
			"(no id=\"of\").\n%s", firstChunk(page))
	}
}

// TestFormAnalyticsLabelsCustomFormWithoutModeAsResponse covers the dashboard
// half: FormAnalyticsAccounts carries its own SQL copy of the mode rule, so a
// row this fix reclassifies must be counted as a response form there too —
// otherwise the rollup reports a payment form with 0 sales while its entries sit
// in submissions.
func TestFormAnalyticsLabelsCustomFormWithoutModeAsResponse(t *testing.T) {
	h, _, formSlug := customFormFixture(t)
	formID, accountID := storeFormConfig(t, formSlug, "Borang tersuai", customBodyNoMode())

	subs := dbstore.NewSubmissionStore(bundleTestPool(t))
	if err := subs.Create(context.Background(), &dbstore.Submission{
		FormID: formID, AccountID: accountID,
		Data: map[string]any{"email": "ali@example.test"},
	}); err != nil {
		t.Fatalf("seed submission: %v", err)
	}

	stats, err := h.Sales.FormAnalyticsAccounts(context.Background(), []string{accountID})
	if err != nil {
		t.Fatalf("form analytics: %v", err)
	}
	var found *dbstore.FormStats
	for i := range stats {
		if stats[i].FormID == formID {
			found = &stats[i]
		}
	}
	if found == nil {
		t.Fatalf("analytics has no row for %s: %+v", formID, stats)
	}
	if found.Mode != dbstore.FormModeResponse {
		t.Errorf("analytics mode = %q, want %q — the rollup disagrees with dbstore.FormMode about a "+
			"config that carries an enabled custom_html section", found.Mode, dbstore.FormModeResponse)
	}
	if found.Submissions != 1 {
		t.Errorf("analytics submissions = %d, want 1 (read from the table a response form writes)",
			found.Submissions)
	}
}

// firstChunk keeps a failure readable: the pages are tens of kilobytes.
func firstChunk(page string) string {
	if len(page) > 1200 {
		return page[:1200] + "\n…"
	}
	return page
}
