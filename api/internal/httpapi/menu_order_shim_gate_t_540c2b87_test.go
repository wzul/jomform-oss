package httpapi

// Card t_540c2b87: the deprecated POST /menu-order shim must apply the same
// submit gates as POST /{slug}/order.
//
// The shim (issue #491, kept for a cached menu page or an old QR stand) resolved
// the menu form itself and called submitMenuOrderForForm DIRECTLY, so it never
// reached submitResolvedForm and applied none of the gates that live there: not
// the schedule window, not the entry limit, not the draft/private checks. The
// measured consequence was that a closed menu form — whose page refuses to
// render the menu and whose canonical route answers 409 — still recorded a sale
// when a cached page (or a stale QR stand) posted to /menu-order an hour after
// end_at.
//
// Every test here proves three things, because the defect was a missing refusal
// rather than a wrong message: the status code, the sentence (the same one the
// page shows for that state), and the ROW COUNT — a 409 that still wrote a sale
// would not be a fix. The open-form test is the control: routing the shim
// through the shared gates must not turn into a blanket refusal.
//
// Integration tests (real Postgres, real route table through RegisterRoutes);
// they skip unless JOMFORM_TEST_DATABASE_URL is set. The fixture, the request
// shape (JSON body with form_slug + client + items) and the sale rows are the
// ones the #491/#497/#492 suites already use, so this card measures the same
// wire and the same table.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// menuShimFixture is menuOrderFixture (a published menu form + one live
// product) plus the form ROW itself, which the restriction write and the sale
// count need.
func menuShimFixture(t *testing.T) (h *PublicHandlers, ctx context.Context, form *dbstore.Form, wsSlug, prodID string) {
	t.Helper()
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping /menu-order shim gate test")
	}
	h, wsSlug, accountID, prodID, menuSlug := menuOrderFixture(t)
	ctx = context.Background()
	form = findFormBySlug(t, h, accountID, menuSlug)
	return h, ctx, form, wsSlug, prodID
}

// menuShimBody is the body a cached menu page posts to the shim: the menu
// form's slug plus the same checkout payload the form-scoped route takes.
func menuShimBody(formSlug, prodID string) string {
	return `{"form_slug":"` + formSlug + `","client":{"full_name":"Ali","email":"ali@example.test"},` +
		`"items":[{"product_id":"` + prodID + `","quantity":1}],"payment_method":"bank_direct"}`
}

// menuShimPost posts to the deprecated shim — the literal route, on the
// workspace's subdomain, exactly as an old menu page does.
func menuShimPost(t *testing.T, h *PublicHandlers, host, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, "/menu-order", bytes.NewBufferString(body))
	req.Host = host
	req.RemoteAddr = uniqueTestIP() + ":1234"
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// menuShimSaleCount is the number of sales attributed to the menu form — the
// fact a missing refusal changes.
func menuShimSaleCount(t *testing.T, h *PublicHandlers, form *dbstore.Form) int64 {
	t.Helper()
	n, err := h.Sales.CountByAccountsForm(context.Background(), []string{form.AccountID}, form.ID)
	if err != nil {
		t.Fatalf("count sales for form %s: %v", form.ID, err)
	}
	return n
}

// pageSentence is the sentence the menu form's PAGE shows for a restriction
// state; the shim's refusal must use that same one, not a new wording.
func pageSentence(t *testing.T, h *PublicHandlers, form *dbstore.Form, st restrictionState) string {
	t.Helper()
	return h.restrictionMessage(httptest.NewRequest(http.MethodGet, "/"+form.Slug, nil), form, st)
}

// TestMenuOrderShimRefusesClosedMenuForm: end_at in the past. The menu page
// refuses to render and POST /{slug}/order answers 409 (card t_305be5b1); the
// shim must answer the same, and store nothing.
func TestMenuOrderShimRefusesClosedMenuForm(t *testing.T) {
	h, ctx, form, wsSlug, prodID := menuShimFixture(t)
	past := time.Now().Add(-time.Hour)
	if err := h.Forms.UpdateRestrictions(ctx, form.AccountID, form.ID, nil, &past, nil, "", "", ""); err != nil {
		t.Fatalf("close the menu form: %v", err)
	}
	form = reloadForm(t, h, ctx, form)

	before := menuShimSaleCount(t, h, form)
	rec := menuShimPost(t, h, wsSlug+".jomform.com", menuShimBody(form.Slug, prodID))

	if rec.Code != http.StatusConflict {
		t.Fatalf("POST /menu-order on a closed menu form = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
	if want := pageSentence(t, h, form, restrictionExpired); wireError(t, rec) != want {
		t.Errorf("refusal = %q, the page says %q", wireError(t, rec), want)
	}
	if after := menuShimSaleCount(t, h, form); after != before {
		t.Errorf("a refused /menu-order POST stored a sale: sales %d -> %d", before, after)
	}
}

// TestMenuOrderShimRefusesScheduledMenuForm: start_at in the future — the other
// end of the same window, and the same claim.
func TestMenuOrderShimRefusesScheduledMenuForm(t *testing.T) {
	h, ctx, form, wsSlug, prodID := menuShimFixture(t)
	future := time.Now().Add(2 * time.Hour)
	if err := h.Forms.UpdateRestrictions(ctx, form.AccountID, form.ID, &future, nil, nil, "", "", ""); err != nil {
		t.Fatalf("schedule the menu form: %v", err)
	}
	form = reloadForm(t, h, ctx, form)

	before := menuShimSaleCount(t, h, form)
	rec := menuShimPost(t, h, wsSlug+".jomform.com", menuShimBody(form.Slug, prodID))

	if rec.Code != http.StatusConflict {
		t.Fatalf("POST /menu-order before start_at = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
	if want := pageSentence(t, h, form, restrictionPending); wireError(t, rec) != want {
		t.Errorf("refusal = %q, the page says %q", wireError(t, rec), want)
	}
	if after := menuShimSaleCount(t, h, form); after != before {
		t.Errorf("a refused /menu-order POST stored a sale: sales %d -> %d", before, after)
	}
}

// TestMenuOrderShimAppliesTheEntryLimit: the same bypass meant the entry limit
// had never applied to this route either. A limit of 1 accepts the first order
// and must refuse the second — with the page's limit sentence and no second row.
func TestMenuOrderShimAppliesTheEntryLimit(t *testing.T) {
	h, ctx, form, wsSlug, prodID := menuShimFixture(t)
	limit := 1
	if err := h.Forms.UpdateRestrictions(ctx, form.AccountID, form.ID, nil, nil, &limit, "", "", ""); err != nil {
		t.Fatalf("set the entry limit: %v", err)
	}
	form = reloadForm(t, h, ctx, form)
	body := menuShimBody(form.Slug, prodID)

	first := menuShimPost(t, h, wsSlug+".jomform.com", body)
	if first.Code != http.StatusCreated {
		t.Fatalf("first /menu-order POST under the limit = %d, want 201 (body %s)", first.Code, first.Body.String())
	}
	if n := menuShimSaleCount(t, h, form); n != 1 {
		t.Fatalf("sales after the accepted POST = %d, want 1", n)
	}

	second := menuShimPost(t, h, wsSlug+".jomform.com", body)
	if second.Code != http.StatusConflict {
		t.Fatalf("second /menu-order POST at the limit = %d, want 409 (body %s)", second.Code, second.Body.String())
	}
	if want := pageSentence(t, h, form, restrictionLimitReached); wireError(t, second) != want {
		t.Errorf("refusal = %q, the page says %q", wireError(t, second), want)
	}
	if n := menuShimSaleCount(t, h, form); n != 1 {
		t.Errorf("a refused /menu-order POST at the limit stored a sale: sales = %d, want 1", n)
	}
}

// TestMenuOrderShimOpenMenuFormStillSells is the CONTROL: the shim keeps selling
// for an open menu form, and the sale is still attributed to the menu form (the
// issue #497 shape) — routing it through the shared gates must not break the one
// caller the route exists for.
func TestMenuOrderShimOpenMenuFormStillSells(t *testing.T) {
	h, _, form, wsSlug, prodID := menuShimFixture(t)

	before := menuShimSaleCount(t, h, form)
	rec := menuShimPost(t, h, wsSlug+".jomform.com", menuShimBody(form.Slug, prodID))

	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /menu-order on an open menu form = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		RefCode string `json:"ref_code"`
		Status  string `json:"status"`
		Next    string `json:"next"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode success body %q: %v", rec.Body.String(), err)
	}
	if resp.RefCode == "" {
		t.Fatalf("open menu form returned no ref_code (body %s)", rec.Body.String())
	}
	sale, err := h.Sales.ByRefCode(context.Background(), resp.RefCode)
	if err != nil {
		t.Fatalf("fetch sale %s: %v", resp.RefCode, err)
	}
	if sale.FormID != form.ID {
		t.Errorf("sale.form_id = %q, want the menu form id %q", sale.FormID, form.ID)
	}
	if after := menuShimSaleCount(t, h, form); after != before+1 {
		t.Errorf("an accepted /menu-order POST stored %d sales, want %d", after-before, 1)
	}
}

// TestMenuOrderShimClosingBetweenLoadAndSubmit is the vector the defect was
// measured through: a cached menu page loads inside the window, the window
// closes, and the same tab posts. The first POST stores; the second must not.
func TestMenuOrderShimClosingBetweenLoadAndSubmit(t *testing.T) {
	h, ctx, form, wsSlug, prodID := menuShimFixture(t)
	body := menuShimBody(form.Slug, prodID)

	inside := menuShimPost(t, h, wsSlug+".jomform.com", body)
	if inside.Code != http.StatusCreated {
		t.Fatalf("POST /menu-order inside the window = %d, want 201 (body %s)", inside.Code, inside.Body.String())
	}
	stored := menuShimSaleCount(t, h, form)
	if stored != 1 {
		t.Fatalf("sales after the accepted POST = %d, want 1", stored)
	}

	// The window closes while the cached page is still open.
	past := time.Now().Add(-time.Minute)
	if err := h.Forms.UpdateRestrictions(ctx, form.AccountID, form.ID, nil, &past, nil, "", "", ""); err != nil {
		t.Fatalf("close the menu form: %v", err)
	}

	late := menuShimPost(t, h, wsSlug+".jomform.com", body)
	if late.Code != http.StatusConflict {
		t.Fatalf("POST /menu-order after the window closed = %d, want 409 (body %s)", late.Code, late.Body.String())
	}
	if after := menuShimSaleCount(t, h, form); after != stored {
		t.Errorf("a POST after the window closed stored a sale: sales %d -> %d", stored, after)
	}
}
