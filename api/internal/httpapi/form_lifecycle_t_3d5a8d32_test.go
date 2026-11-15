package httpapi

// Card t_3d5a8d32 — the forms aggregate's state machine and the ONE derived
// "can this form take an order right now" predicate, measured at the surfaces
// that read it.
//
// THE DEFECT THE READ SIDE HAD. Three readers answered "is this form taking
// orders" three ways: the public page asked the schedule and the entry limit,
// the dashboard list asked only the product rule (so a form whose window had not
// opened, or one at its entry limit, read as healthy), and MCP asked nothing at
// all. The row also had no way to say what could be DONE with it, so the
// dashboard's buttons were guessed from `status`.
//
// These tests pin the unified answer on the two REST reads and on the public
// page, and the actions each row reports.

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/lifecycle"
)

// formReadRow is the subset of the REST payload these tests read.
type formReadRow struct {
	ID                 string                     `json:"id"`
	Status             string                     `json:"status"`
	AcceptingOrders    bool                       `json:"accepting_orders"`
	Reason             string                     `json:"reason"`
	Code               string                     `json:"code"`
	NotAcceptingOrders bool                       `json:"not_accepting_orders"`
	NoProductBlockID   string                     `json:"no_product_block_id"`
	AvailableEvents    []lifecycle.AvailableEvent `json:"available_events"`
}

func eventNames(events []lifecycle.AvailableEvent) map[string]lifecycle.AvailableEvent {
	out := make(map[string]lifecycle.AvailableEvent, len(events))
	for _, ev := range events {
		out[ev.Event] = ev
	}
	return out
}

// 1. A live form whose schedule window has PASSED is refused by the ONE
// predicate, and the buyer is served the expired page. Before the card the
// renderer asked its own schedule rule and the dashboard list asked none.
func TestClosedFormServesTheExpiredPageAndReadsAsNotAccepting(t *testing.T) {
	closed := time.Now().Add(-time.Hour)
	f := &dbstore.Form{
		ID: "f_closed", AccountID: "acc_closed", Slug: "closed-form",
		Title: "Closed form", Status: dbstore.FormStatusPublished,
		EndAt:  &closed,
		Config: map[string]any{"blocks": []any{map[string]any{"id": "b1", "type": "name"}}},
	}
	body, rec := serveFormBody(t, f)
	if rec.Code != http.StatusOK {
		t.Fatalf("serveForm status = %d, want 200 (body: %s)", rec.Code, body)
	}
	if !strings.Contains(body, formLangs["en"].ExpiredMessage) {
		t.Errorf("a closed form did not serve the expired page — the page and the read must ask one rule; page: %s", body)
	}
	if strings.Contains(body, "Proceed to Payment") {
		t.Error("a closed form painted the payment layout")
	}

	order := lifecycle.FormOrders(f, lifecycle.FormOrderEnv{Entries: -1, Now: time.Now()})
	if order.Accepting || order.Code != lifecycle.FormOrderClosed {
		t.Errorf("derived answer = %+v, want code %q", order, lifecycle.FormOrderClosed)
	}
}

// 2. The REST list carries the derived answer AND the row's actions, so the
// dashboard can render Publish/Unpublish/Delete from the server instead of
// guessing from `status`.
func TestFormListCarriesTheBuyerStateAndTheRowsActions(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping form-read integration test")
	}
	ctx, h, forms, acc, key, _ := guardFixture(t)

	// A PUBLISHED payment form with nothing sellable: the shape the publish
	// route only allows with `acknowledge`. Its product block names no product,
	// so the derived answer carries the block to fix AND
	// no_product_selected — the same code the publish refusal uses. Its row must
	// read not-accepting, and must NOT offer publish (it is already there).
	dead := &dbstore.Form{
		AccountID: acc, Slug: "dead-" + acc[:8], Title: "Derma Surau",
		Config: map[string]any{"blocks": []any{
			map[string]any{"id": "nm1", "type": "name"},
			map[string]any{"id": "q5uhvoy", "type": "product", "value": ""},
		}},
		Theme: map[string]any{}, Status: dbstore.FormStatusDraft,
	}
	if err := forms.Create(ctx, dead); err != nil {
		t.Fatalf("create dead form: %v", err)
	}
	if code, raw := tplReq(t, h, key, http.MethodPost, "/v1/rest/forms/"+dead.ID+"/publish", `{"acknowledge":true}`); code != http.StatusOK {
		t.Fatalf("acknowledged publish = %d, want 200 (body=%s)", code, raw)
	}

	// A DRAFT response form: publishable, deletable, and it must not offer
	// unpublish (it is already a draft — a no-op, not an action).
	rsvp := &dbstore.Form{
		AccountID: acc, Slug: "rsvp-" + acc[:8], Title: "RSVP",
		Config: map[string]any{"mode": "response", "blocks": []any{
			map[string]any{"id": "nm1", "type": "name"},
		}},
		Theme: map[string]any{}, Status: dbstore.FormStatusDraft,
	}
	if err := forms.Create(ctx, rsvp); err != nil {
		t.Fatalf("create response form: %v", err)
	}

	code, raw := tplReq(t, h, key, http.MethodGet, "/v1/rest/forms", "")
	if code != http.StatusOK {
		t.Fatalf("GET /v1/rest/forms = %d (body=%s)", code, raw)
	}
	var rows []formReadRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("decode list: %v (%s)", err, raw)
	}
	byID := map[string]formReadRow{}
	for _, r := range rows {
		byID[r.ID] = r
	}

	d, ok := byID[dead.ID]
	if !ok {
		t.Fatalf("the published form is missing from the list (%s)", raw)
	}
	if d.Status != dbstore.FormStatusPublished {
		t.Errorf("dead form status = %q, want published", d.Status)
	}
	if d.AcceptingOrders {
		t.Error("a published payment form with no sellable block reads as accepting orders")
	}
	if d.Code != lifecycle.FormOrderNoProductSelected {
		t.Errorf("dead form code = %q, want %q (a product block with no product)", d.Code, lifecycle.FormOrderNoProductSelected)
	}
	if d.NoProductBlockID != "q5uhvoy" {
		t.Errorf("dead form no_product_block_id = %q, want q5uhvoy — the builder links straight to that block", d.NoProductBlockID)
	}
	if !d.NotAcceptingOrders {
		t.Error("the legacy not_accepting_orders flag no longer marks the dead form — the dashboard pill would disappear")
	}
	if d.Reason == "" {
		t.Error("a refused form carries no reason for the agent/person reading it")
	}
	dev := eventNames(d.AvailableEvents)
	if _, offered := dev[lifecycle.FormEventPublish]; offered {
		t.Error("the published form offers publish — a no-op must not be a button")
	}
	for _, want := range []string{lifecycle.FormEventUnpublish, lifecycle.FormEventDelete} {
		ev, offered := dev[want]
		if !offered {
			t.Errorf("the published form does not offer %s", want)
			continue
		}
		if !ev.Enabled {
			t.Errorf("%s is disabled on a published form: %s", want, ev.Reason)
		}
	}

	r, ok := byID[rsvp.ID]
	if !ok {
		t.Fatalf("the draft form is missing from the list (%s)", raw)
	}
	if r.AcceptingOrders {
		t.Error("a draft reads as accepting orders — a draft is not served at all")
	}
	if r.Code != lifecycle.FormOrderNotPublished {
		t.Errorf("draft code = %q, want %q", r.Code, lifecycle.FormOrderNotPublished)
	}
	rev := eventNames(r.AvailableEvents)
	if ev, offered := rev[lifecycle.FormEventPublish]; !offered || !ev.Enabled {
		t.Error("a draft does not offer an enabled publish")
	}
	if _, offered := rev[lifecycle.FormEventUnpublish]; offered {
		t.Error("a draft offers unpublish — it is already a draft")
	}

	// 3. The single-form GET carries the SAME answer, so the builder and the list
	// cannot disagree about one row.
	code, raw = tplReq(t, h, key, http.MethodGet, "/v1/rest/forms/"+dead.ID, "")
	if code != http.StatusOK {
		t.Fatalf("GET /v1/rest/forms/{id} = %d (body=%s)", code, raw)
	}
	var one formReadRow
	if err := json.Unmarshal(raw, &one); err != nil {
		t.Fatalf("decode single form: %v (%s)", err, raw)
	}
	if one.AcceptingOrders != d.AcceptingOrders || one.Code != d.Code || one.Reason != d.Reason {
		t.Errorf("single form answer %+v disagrees with the list row %+v", one, d)
	}
	if len(one.AvailableEvents) != len(d.AvailableEvents) {
		t.Errorf("single form offers %d actions, list offers %d", len(one.AvailableEvents), len(d.AvailableEvents))
	}
}

// 4. Every refusal the derived predicate can report for a LIVE form has a page
// state, so the renderer can never fall through to a payment layout.
func TestEveryLiveFormRefusalCodeHasARestrictionPage(t *testing.T) {
	for _, code := range []string{
		lifecycle.FormOrderScheduled,
		lifecycle.FormOrderClosed,
		lifecycle.FormOrderEntryLimitReached,
		lifecycle.FormOrderNoProducts,
		lifecycle.FormOrderNoProductSelected,
	} {
		if st, ok := restrictionStateForOrder(code); !ok || st == restrictionOpen {
			t.Errorf("code %q has no restriction page (state=%v ok=%v) — a live form refused for that reason would render the payment layout", code, st, ok)
		}
	}
	// The accepting answer is not a restriction, and neither is "not published"
	// (the status checks answer that with a 404 before the renderer runs).
	if _, ok := restrictionStateForOrder(lifecycle.FormOrderAccepting); ok {
		t.Error("the accepting code maps to a restriction page")
	}
	if _, ok := restrictionStateForOrder(lifecycle.FormOrderNotPublished); ok {
		t.Error("a draft maps to a restriction page — it must be a 404, not a message page")
	}
}

// guardFixture is declared in product_block_publish_guard_t_983425e5_test.go;
// this file reuses it so both cards measure the same production wiring. It is
// reused rather than re-declared on purpose: a second fixture would drift from
// the handler set the real routes are built with.

// 5. The CREATE response carries the same answer, so a caller that keeps the
// 201 body in hand is not told a different story about the row it just minted
// (the response used to carry accepting_orders=false with a null
// available_events, which a strict client has to special-case).
func TestCreatedFormCarriesTheBuyerStateAndItsActions(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping form-read integration test")
	}
	_, h, _, _, key, _ := guardFixture(t)

	code, raw := tplReq(t, h, key, http.MethodPost, "/v1/rest/forms",
		`{"title":"Create probe","config":{"mode":"response","blocks":[{"id":"nm1","type":"name"}]}}`)
	if code != http.StatusCreated {
		t.Fatalf("POST /v1/rest/forms = %d, want 201 (body=%s)", code, raw)
	}
	var created formReadRow
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatalf("decode create response: %v (%s)", err, raw)
	}
	if created.AcceptingOrders {
		t.Error("a freshly created draft reads as accepting orders")
	}
	if created.Code != lifecycle.FormOrderNotPublished {
		t.Errorf("create response code = %q, want %q", created.Code, lifecycle.FormOrderNotPublished)
	}
	if created.AvailableEvents == nil {
		t.Errorf("create response available_events is null, not an array: %s", raw)
	}
	ev, offered := eventNames(created.AvailableEvents)[lifecycle.FormEventPublish]
	if !offered || !ev.Enabled {
		t.Errorf("a freshly created draft does not offer an enabled publish: %s", raw)
	}
	if _, offered := eventNames(created.AvailableEvents)[lifecycle.FormEventUnpublish]; offered {
		t.Error("a freshly created draft offers unpublish — it is already a draft")
	}
}
