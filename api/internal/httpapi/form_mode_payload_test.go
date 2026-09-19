package httpapi

// The /forms list must be able to tell a sales form from a
// response-only form.
//
// THE BUG (reported by Wan): every row on https://jomform.com/forms carried a
// "Sales" button, including a non-sales form — his example was an RSVP form.
// The dashboard could not do better, because GET /v1/rest/forms returned no
// mode: `restForm` had no such field, so the page had nothing to branch on and
// drew the Sales action for every row.
//
// This drives the REAL REST list handler over a real Postgres and asserts the
// payload the dashboard actually receives:
// - a response-mode form comes back mode="response" (so the fix can hide the
// Sales action),
// - a payment-mode form comes back mode="payment",
// - a LEGACY form with no `mode` key at all comes back mode="payment".
//
// That third case is the one that matters: those are the pre-#171 forms, they
// are genuine payment forms, and several of them have real sales in production.
// A naive "default to response" implementation would hide the Sales action on
// exactly the rows that need it — so this asserts the default explicitly.
//
// It also asserts GET /v1/rest/forms/{id} (the single-form read the Form
// Settings page uses) carries the same mode, since that page hides the same
// action.
//
// Skipped without JOMFORM_TEST_DATABASE_URL.
import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

func TestFormsListPayloadCarriesTheCollectionMode(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping forms-mode payload test")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	acc, _ := bundleFixture(t, ctx, pool)
	key := bundleAPIKey(t, ctx, pool, acc)
	h := newBundleHandlers(pool)

	forms := dbstore.NewFormStore(pool)

	// An RSVP/response form — the reported case. This must NOT look like sales.
	rsvp := &dbstore.Form{
		AccountID: acc, Slug: "it-rsvp-" + acc[:8], Title: "RSVP Form",
		Config: map[string]any{"mode": "response", "blocks": []any{
			map[string]any{"id": "b1", "type": "name", "value": "Full name"},
		}},
		Status: dbstore.FormStatusPublished,
	}
	// A payment/order form — the case that must KEEP its sales action.
	order := &dbstore.Form{
		AccountID: acc, Slug: "it-order-" + acc[:8], Title: "Order Form",
		Config: map[string]any{"mode": "payment", "blocks": []any{
			map[string]any{"id": "b1", "type": "product", "value": "Widget"},
		}},
		Status: dbstore.FormStatusPublished,
	}
	// A legacy form: NO `mode` key. This is the pre-#171 shape and it is a
	// payment form — production has rows like this WITH real sales.
	legacy := &dbstore.Form{
		AccountID: acc, Slug: "it-legacy-" + acc[:8], Title: "Legacy Order Form",
		Config: map[string]any{"blocks": []any{}},
		Status: dbstore.FormStatusPublished,
	}
	for _, f := range []*dbstore.Form{rsvp, order, legacy} {
		if err := forms.Create(ctx, f); err != nil {
			t.Fatalf("create form %s: %v", f.Slug, err)
		}
	}

	// ── the LIST payload the dashboard reads ────────────────────────────────
	code, raw := tplReq(t, h, key, http.MethodGet, "/v1/rest/forms", "")
	if code != http.StatusOK {
		t.Fatalf("list forms status = %d, body=%s", code, raw)
	}
	var list []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Mode  string `json:"mode"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatalf("decode forms list: %v (%s)", err, raw)
	}
	byID := map[string]string{}
	for _, r := range list {
		byID[r.ID] = r.Mode
	}

	want := map[string]struct {
		mode string
		why  string
	}{
		rsvp.ID:   {dbstore.FormModeResponse, "an RSVP form is a response-only form and must not be offered a Sales action"},
		order.ID:  {dbstore.FormModePayment, "an order form is a sales form and keeps its Sales action"},
		legacy.ID: {dbstore.FormModePayment, "a legacy form with no mode key is a payment form (and has real sales) — defaulting it to response would hide the Sales action"},
	}
	for id, w := range want {
		got, ok := byID[id]
		if !ok {
			t.Errorf("form %s missing from the list payload entirely", id)
			continue
		}
		if got != w.mode {
			t.Errorf("form %s: list payload mode = %q, want %q — %s", id, got, w.mode, w.why)
		}
	}

	// The mode must be present on EVERY row (an omitted field is the bug
	// returning): `json:"mode"` has no omitempty, so a missing key means the
	// field was dropped from the struct or never populated.
	var rawRows []map[string]any
	if err := json.Unmarshal(raw, &rawRows); err != nil {
		t.Fatalf("decode raw rows: %v", err)
	}
	for _, r := range rawRows {
		if _, ok := r["mode"]; !ok {
			t.Errorf("a forms-list row has no `mode` key at all: %v", r)
		}
	}

	// ── the SINGLE-form read (Form Settings hides the same action) ───────────
	code, raw = tplReq(t, h, key, http.MethodGet, "/v1/rest/forms/"+rsvp.ID, "")
	if code != http.StatusOK {
		t.Fatalf("get form status = %d, body=%s", code, raw)
	}
	var one struct {
		Mode string `json:"mode"`
	}
	if err := json.Unmarshal(raw, &one); err != nil {
		t.Fatalf("decode single form: %v (%s)", err, raw)
	}
	if one.Mode != dbstore.FormModeResponse {
		t.Errorf("single-form GET mode = %q, want %q — the Form Settings page also hides the Sales action on it", one.Mode, dbstore.FormModeResponse)
	}
}
