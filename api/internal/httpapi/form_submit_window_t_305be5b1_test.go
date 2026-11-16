package httpapi

// Card t_305be5b1: a schedule window must bind the SUBMIT path, not only the
// page. Before this, `submitResolvedForm` refused a draft, a locked private form
// and a reached entry limit, and then stored the submission — the window was
// never asked. A buyer who loaded the page inside the window could POST after it
// closed and the response/order was stored while the page, the REST form read and
// the MCP form read all said "not accepting".
//
// These are integration tests (real Postgres, real route table through
// RegisterRoutes); they skip unless JOMFORM_TEST_DATABASE_URL is set. Each one
// proves three things, because the defect was a missing refusal rather than a
// wrong message: the status code, the sentence (the same one the page shows for
// that state), and the ROW COUNT — a 409 that still wrote a submission would not
// be a fix.
//
// The POST goes through postSlug and the count through submissionCount, both
// declared in required_blocks_submit_test.go, so this card measures the same
// request shape and the same stored rows as the entry-contract tests.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// windowFixture is the response-mode fixture from response_submit_test.go, plus
// the form row itself (its account id and id are what UpdateRestrictions and the
// submission count need).
func windowFixture(t *testing.T) (h *PublicHandlers, ctx context.Context, form *dbstore.Form, wsSlug string) {
	t.Helper()
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping submit-window integration test")
	}
	h, wsSlug, formSlug := responseFormFixture(t)
	ctx = context.Background()
	var err error
	form, err = h.Forms.ByWorkspaceSlug(ctx, wsSlug, formSlug)
	if err != nil {
		t.Fatalf("load fixture form %q: %v", formSlug, err)
	}
	return h, ctx, form, wsSlug
}

// windowBody is a complete, valid response body: name, email and free text, which
// is the contract the fixture form declares.
const windowBody = `{"client":{"full_name":"Ali","email":"ali@example.test"},
          "fields":{"b1":"Ali","b2":"ali@example.test","b3":"submitted"}}`

// postWindowForm posts the body above to the form's public order route — the
// exact request a buyer's browser makes — and returns the recorder.
func postWindowForm(t *testing.T, h *PublicHandlers, wsSlug, formSlug string) *httptest.ResponseRecorder {
	t.Helper()
	return postSlug(t, h, wsSlug+".jomform.com", formSlug, windowBody)
}

// windowSubmissionCount is the number of stored responses for this form — the
// fact a missing refusal changes.
func windowSubmissionCount(t *testing.T, h *PublicHandlers, form *dbstore.Form) int64 {
	t.Helper()
	return submissionCount(t, h, form.AccountID, form.ID)
}

// wireError reads the refusal envelope's `error` field — the sentence the buyer's
// client shows.
func wireError(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode refusal body %q: %v", rec.Body.String(), err)
	}
	return body.Error
}

// reloadForm re-reads the row after UpdateRestrictions wrote the window, so the
// assertions compare against the row the handler will actually see.
func reloadForm(t *testing.T, h *PublicHandlers, ctx context.Context, form *dbstore.Form) *dbstore.Form {
	t.Helper()
	f, err := h.Forms.GetByID(ctx, form.AccountID, form.ID)
	if err != nil {
		t.Fatalf("reload form %s: %v", form.Slug, err)
	}
	return f
}

// TestClosedFormSubmitIsRefused: end_at in the past. The page shows the expired
// message and paints no form; the POST must refuse with that same sentence and
// store nothing.
func TestClosedFormSubmitIsRefused(t *testing.T) {
	h, ctx, form, wsSlug := windowFixture(t)
	past := time.Now().Add(-time.Hour)
	if err := h.Forms.UpdateRestrictions(ctx, form.AccountID, form.ID, nil, &past, nil, "", "", ""); err != nil {
		t.Fatalf("close the form: %v", err)
	}
	form = reloadForm(t, h, ctx, form)

	before := windowSubmissionCount(t, h, form)
	rec := postWindowForm(t, h, wsSlug, form.Slug)

	if rec.Code != http.StatusConflict {
		t.Fatalf("POST on a closed form = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
	// The same sentence the page uses for this state — not a new wording.
	pageReq := httptest.NewRequest(http.MethodGet, "/"+form.Slug, nil)
	if want := h.restrictionMessage(pageReq, form, restrictionExpired); wireError(t, rec) != want {
		t.Errorf("refusal = %q, the page says %q", wireError(t, rec), want)
	}
	if after := windowSubmissionCount(t, h, form); after != before {
		t.Errorf("a refused POST stored a response: submissions %d -> %d", before, after)
	}
}

// TestScheduledFormSubmitIsRefused: start_at in the future — the other end of the
// same window, and the same claim.
func TestScheduledFormSubmitIsRefused(t *testing.T) {
	h, ctx, form, wsSlug := windowFixture(t)
	future := time.Now().Add(2 * time.Hour)
	if err := h.Forms.UpdateRestrictions(ctx, form.AccountID, form.ID, &future, nil, nil, "", "", ""); err != nil {
		t.Fatalf("schedule the form: %v", err)
	}
	form = reloadForm(t, h, ctx, form)

	before := windowSubmissionCount(t, h, form)
	rec := postWindowForm(t, h, wsSlug, form.Slug)

	if rec.Code != http.StatusConflict {
		t.Fatalf("POST before start_at = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
	pageReq := httptest.NewRequest(http.MethodGet, "/"+form.Slug, nil)
	if want := h.restrictionMessage(pageReq, form, restrictionPending); wireError(t, rec) != want {
		t.Errorf("refusal = %q, the page says %q", wireError(t, rec), want)
	}
	if after := windowSubmissionCount(t, h, form); after != before {
		t.Errorf("a refused POST stored a response: submissions %d -> %d", before, after)
	}
}

// TestOpenFormSubmitStillStores is the CONTROL: the window gate must not become a
// blanket refusal. A form with no window accepts, and the row is written.
func TestOpenFormSubmitStillStores(t *testing.T) {
	h, _, form, wsSlug := windowFixture(t)

	before := windowSubmissionCount(t, h, form)
	rec := postWindowForm(t, h, wsSlug, form.Slug)

	if rec.Code != http.StatusCreated {
		t.Fatalf("POST on an open form = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Status string `json:"status"`
		ID     string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode success body %q: %v", rec.Body.String(), err)
	}
	if resp.Status != "submitted" || resp.ID == "" {
		t.Errorf("success body = %+v, want status=submitted with an id", resp)
	}
	if after := windowSubmissionCount(t, h, form); after != before+1 {
		t.Errorf("an accepted POST stored %d responses, want %d", after-before, 1)
	}
}

// TestWindowClosingBetweenLoadAndSubmit is the vector the defect was measured
// through: the page was rendered inside the window, then the window closed, then
// the POST arrives. The first POST (inside) stores; the second (after end_at)
// must not — even though it is the same form, the same row and the same tab.
func TestWindowClosingBetweenLoadAndSubmit(t *testing.T) {
	h, ctx, form, wsSlug := windowFixture(t)

	// 1. Inside the window (the fixture has no window, so it is open) the POST
	// stores. This is also the control for the second half.
	rec := postWindowForm(t, h, wsSlug, form.Slug)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST inside the window = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	inside := windowSubmissionCount(t, h, form)
	if inside != 1 {
		t.Fatalf("submissions after the accepted POST = %d, want 1", inside)
	}

	// 2. The window closes while the buyer's page is still open.
	past := time.Now().Add(-time.Minute)
	if err := h.Forms.UpdateRestrictions(ctx, form.AccountID, form.ID, nil, &past, nil, "", "", ""); err != nil {
		t.Fatalf("close the form: %v", err)
	}

	// 3. The same POST, arriving after end_at, is refused and stores nothing.
	rec = postWindowForm(t, h, wsSlug, form.Slug)
	if rec.Code != http.StatusConflict {
		t.Fatalf("POST after the window closed = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
	if after := windowSubmissionCount(t, h, form); after != inside {
		t.Errorf("a POST after the window closed stored a response: submissions %d -> %d", inside, after)
	}
}
