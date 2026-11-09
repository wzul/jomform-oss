package httpapi

// `available_events` on the sales read surface (card t_52429d6f).
//
// WHY THESE TESTS. The dashboard used to derive its action buttons from `status`
// (`disabled={s.status !== "paid"}`), which is a client-side copy of a rule the
// server owns. The card replaces it with the server's own answer: every sale row
// carries available_events, computed by the same lifecycle table the write paths
// apply. These tests pin the WIRE contract:
//
//   - the field is always present on a sale row (an absent field is what the UI
//     must treat as "the server did not say" — the fail-closed case);
//   - it is [] (not null) for a terminal state, so "nothing to do" and "no
//     answer" stay distinguishable;
//   - its contents are exactly Machine.Available, so the API cannot disagree
//     with the table or with the MCP surface.
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL); skipped
// without it.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wzul/jomform/api/internal/lifecycle"
)

func (f salesLifecycleFixture) getJSON(t *testing.T, path string) (int, any) {
	t.Helper()
	mux := http.NewServeMux()
	f.h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+f.key)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var body any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("GET %s returned a non-JSON body (%d): %s", path, rec.Code, rec.Body.String())
	}
	return rec.Code, body
}

// eventsOf reads the available_events array off one row, asserting the field is
// PRESENT (a missing key is a contract failure: the UI would render nothing and
// the failure would look like "this sale has no actions").
func eventsOf(t *testing.T, row map[string]any) []map[string]any {
	t.Helper()
	raw, ok := row["available_events"]
	if !ok {
		t.Fatalf("the sale row has NO available_events key: %v", row)
	}
	arr, ok := raw.([]any)
	if !ok {
		t.Fatalf("available_events is %T, want an array", raw)
	}
	out := make([]map[string]any, 0, len(arr))
	for _, e := range arr {
		m, ok := e.(map[string]any)
		if !ok {
			t.Fatalf("available_events entry is %T, want an object", e)
		}
		out = append(out, m)
	}
	return out
}

func findEvent(events []map[string]any, name string) map[string]any {
	for _, e := range events {
		if e["event"] == name {
			return e
		}
	}
	return nil
}

func TestSaleDetailAndListCarryTheTablesAvailableEvents(t *testing.T) {
	f := seedSalesLifecycleFixture(t)
	ref := f.newSale(t, "paid", false)

	sale, err := f.h.Sales.ByRefCode(context.Background(), ref)
	if err != nil {
		t.Fatalf("read the seeded sale: %v", err)
	}
	want := lifecycle.SalesMachine.Available(sale)
	if len(want) == 0 {
		t.Fatal("the table says a paid sale accepts nothing — the fixture is wrong")
	}

	// Detail read.
	code, body := f.getJSON(t, "/v1/rest/sales/"+ref)
	if code != http.StatusOK {
		t.Fatalf("GET /v1/rest/sales/%s = %d, want 200", ref, code)
	}
	detail, ok := body.(map[string]any)
	if !ok {
		t.Fatalf("detail body is %T, want an object", body)
	}
	got := eventsOf(t, detail)
	if len(got) != len(want) {
		t.Fatalf("detail available_events has %d entries, the table says %d: %v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i]["event"] != w.Event {
			t.Errorf("entry %d event = %v, want %q (ordered by event name)", i, got[i]["event"], w.Event)
		}
		if got[i]["enabled"] != w.Enabled {
			t.Errorf("%s enabled = %v, want %v", w.Event, got[i]["enabled"], w.Enabled)
		}
		reason, _ := got[i]["reason"].(string)
		if reason != w.Reason {
			t.Errorf("%s reason = %q, want %q", w.Event, reason, w.Reason)
		}
		if w.LabelKey != "" {
			if key, _ := got[i]["label_key"].(string); key != w.LabelKey {
				t.Errorf("%s label_key = %q, want %q", w.Event, key, w.LabelKey)
			}
		}
	}

	// The action a person can take is there and enabled.
	confirm := findEvent(got, lifecycle.SaleEventConfirm)
	if confirm == nil || confirm["enabled"] != true {
		t.Errorf("a paid sale must offer an ENABLED confirm: %v", got)
	}
	// The guarded money moves are present but disabled, WITH the reason — that is
	// how the dashboard can explain why Capture is not offered.
	for _, event := range []string{lifecycle.SaleEventCapture, lifecycle.SaleEventRelease} {
		e := findEvent(got, event)
		if e == nil {
			t.Errorf("available_events omits %s; a disabled guard refusal must still be reported: %v", event, got)
			continue
		}
		if e["enabled"] != false {
			t.Errorf("%s enabled = %v, want false on a non-authorization sale", event, e["enabled"])
		}
		if reason, _ := e["reason"].(string); reason == "" {
			t.Errorf("%s is disabled with no reason — the UI has nothing to explain", event)
		}
	}

	// The list shape carries the SAME list (one derivation, two surfaces).
	code, listBody := f.getJSON(t, "/v1/rest/sales?page=1&limit=50")
	if code != http.StatusOK {
		t.Fatalf("GET /v1/rest/sales = %d, want 200", code)
	}
	page, ok := listBody.(map[string]any)
	if !ok {
		t.Fatalf("list body is %T, want an object", listBody)
	}
	items, _ := page["items"].([]any)
	var row map[string]any
	for _, it := range items {
		m, _ := it.(map[string]any)
		if m["ref_code"] == ref {
			row = m
			break
		}
	}
	if row == nil {
		t.Fatalf("the seeded sale %s is not in the list", ref)
	}
	listEvents := eventsOf(t, row)
	if len(listEvents) != len(got) {
		t.Fatalf("list available_events has %d entries, detail has %d — two derivations", len(listEvents), len(got))
	}
	for i := range got {
		if got[i]["event"] != listEvents[i]["event"] || got[i]["enabled"] != listEvents[i]["enabled"] {
			t.Errorf("entry %d differs between list and detail: %v vs %v", i, listEvents[i], got[i])
		}
	}
}

func TestTerminalSaleCarriesAnEmptyArrayNotNull(t *testing.T) {
	f := seedSalesLifecycleFixture(t)
	ref := f.newSale(t, "cancelled", false)

	code, body := f.getJSON(t, "/v1/rest/sales/"+ref)
	if code != http.StatusOK {
		t.Fatalf("GET cancelled sale = %d, want 200", code)
	}
	row, ok := body.(map[string]any)
	if !ok {
		t.Fatalf("body is %T, want an object", body)
	}
	raw, present := row["available_events"]
	if !present {
		t.Fatal("a cancelled sale row omits available_events; the UI cannot tell \"no actions\" from \"no answer\"")
	}
	if raw == nil {
		t.Fatal("available_events is null; it must be [] so the contract is an array")
	}
	if arr, ok := raw.([]any); !ok || len(arr) != 0 {
		t.Fatalf("available_events = %v, want an empty array", raw)
	}
}

// TestPendingAuthorizationOffersCaptureAndConfirm is the "server decides, not
// status" arm: on a pending skip_capture authorization the table enables the two
// money moves (capture/release) AND confirm, and the API must say so even though
// the UI's old predicate offered no Confirm on the row at all.
func TestPendingAuthorizationOffersCaptureAndConfirm(t *testing.T) {
	f := seedSalesLifecycleFixture(t)
	ref := f.newSale(t, "pending", true)

	sale, err := f.h.Sales.ByRefCode(context.Background(), ref)
	if err != nil {
		t.Fatalf("read the seeded sale: %v", err)
	}

	code, body := f.getJSON(t, "/v1/rest/sales/"+ref)
	if code != http.StatusOK {
		t.Fatalf("GET pending sale = %d, want 200", code)
	}
	row := body.(map[string]any)
	events := eventsOf(t, row)
	confirm := findEvent(events, lifecycle.SaleEventConfirm)
	if confirm == nil || confirm["enabled"] != true {
		t.Errorf("a pending sale accepts confirm (bank transfer) — the API must say so: %v", events)
	}
	// Whether capture is enabled depends on the guard (SkipCapture + purchase id
	// + uncaptured). The table is the authority; compare with it directly rather
	// than re-deriving the predicate here.
	want := lifecycle.SalesMachine.Available(sale)
	if len(events) != len(want) {
		t.Fatalf("available_events has %d entries, the table says %d", len(events), len(want))
	}
}
