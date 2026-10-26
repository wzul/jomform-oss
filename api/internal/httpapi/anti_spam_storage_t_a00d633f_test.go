package httpapi

// A block-form submission stored the renderer's own anti-spam
// controls (`website` honeypot, `jf_ts` render timestamp) as if they were
// answers, while the custom-HTML path dropped them (customform.Collect keeps
// only registered fields). The row is what the merchant sees: submissions.data,
// the sale's field snapshot, notifications, and any export of submissions.data.
//
// These are the guards:
//
//  1. the helper drops EXACTLY the controls the renderer emits — read out of
//     antiSpamRenderFields() itself, so a renamed control cannot be dropped by
//     the storage rule and kept by the page (or vice versa), and a real answer
//     whose id merely resembles a control survives;
//  2. a real POST to a published block form's submit route stores b1/b2 and
//     neither control, asserted on the ROW read back from the store — not on the
//     in-memory map, which is the thing that used to be dirty;
//  3. the same submit on a payment form persists the sale's field snapshot
//     without them (order.go builds its own copy and used to write it straight
//     into the sale).
//
// The notification renderer and the confirmation matcher read the same map one
// line after the row, so (2) covers them structurally; the judge has its own
// independent filter (spamjudge.JudgeableData) and its own guard.
//
// Integration tests; skipped unless JOMFORM_TEST_DATABASE_URL is set.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// TestDropAntiSpamBookkeepingRemovesExactlyTheRenderedControls pins the storage
// rule to the renderer. Both map types are exercised because the two write paths
// do not agree on one: the response path carries map[string]any, the order path
// map[string]string.
func TestDropAntiSpamBookkeepingRemovesExactlyTheRenderedControls(t *testing.T) {
	names := regexp.MustCompile(`name="([^"]+)"`).
		FindAllStringSubmatch(antiSpamRenderFields(), -1)
	if len(names) != 2 {
		t.Fatalf("the renderer emits %d named controls, want the honeypot + the render timestamp: %s",
			len(names), antiSpamRenderFields())
	}

	anyMap := map[string]any{"b1": "Ali"}
	strMap := map[string]string{"b1": "Ali"}
	for _, m := range names {
		anyMap[m[1]] = "control"
		strMap[m[1]] = "control"
	}
	// A real answer whose id only RESEMBLES a control is the sender's, not the
	// renderer's, and must survive: a prefix/substring rule would silently eat
	// it. Same edge the judge filter pins.
	anyMap["website_url"] = "ali.example.test"
	anyMap["jf_ts_note"] = "not a timestamp"
	strMap["website_url"] = "ali.example.test"
	strMap["jf_ts_note"] = "not a timestamp"

	dropAntiSpamBookkeeping(anyMap)
	dropAntiSpamBookkeeping(strMap)

	for _, tc := range []struct {
		name  string
		keys  func(string) bool
		value func(string) any
	}{
		{"map[string]any",
			func(k string) bool { _, ok := anyMap[k]; return ok },
			func(k string) any { return anyMap[k] }},
		{"map[string]string",
			func(k string) bool { _, ok := strMap[k]; return ok },
			func(k string) any { return strMap[k] }},
	} {
		for _, m := range names {
			if tc.keys(m[1]) {
				t.Errorf("%s: the control %q survived the drop", tc.name, m[1])
			}
		}
		if tc.value("b1") != "Ali" {
			t.Errorf("%s: the real answer b1 was dropped or changed: %v", tc.name, tc.value("b1"))
		}
		if tc.value("website_url") != "ali.example.test" || tc.value("jf_ts_note") != "not a timestamp" {
			t.Errorf("%s: a real answer that only resembles a control was dropped: website_url=%v jf_ts_note=%v",
				tc.name, tc.value("website_url"), tc.value("jf_ts_note"))
		}
	}
}

// TestBlockFormSubmissionStoresOnlyTheSendersAnswers is the card's own guard: a
// published block form, the real public submit route, the renderer's controls
// riding the fields map, and the assertion on the stored row.
func TestBlockFormSubmissionStoresOnlyTheSendersAnswers(t *testing.T) {
	f := issue622New(t)
	ctx := context.Background()

	// What the page's own script posts. The timestamp is old enough to clear
	// anti-spam layer 3, exactly like a page that has been open a moment.
	body := `{"client":{"full_name":"Ali","email":"ali@example.test"},"fields":{` +
		`"b1":"Ali","b2":"ali@example.test","` + antiSpamHoneypotField + `":"",` +
		`"` + antiSpamRenderField + `":"1700000000"}}`
	mux := http.NewServeMux()
	f.h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, "/"+f.formSlug+"/order", bytes.NewBufferString(body))
	req.Host = f.workspaceSl + ".jomform.com"
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.ID == "" {
		t.Fatalf("decode submit response (err %v): %s", err, rec.Body.String())
	}

	sub, err := f.h.Submissions.ByID(ctx, resp.ID)
	if err != nil {
		t.Fatalf("read the stored submission: %v", err)
	}
	for _, control := range []string{antiSpamHoneypotField, antiSpamRenderField} {
		if v, ok := sub.Data[control]; ok {
			t.Errorf("the stored submission carries the renderer's control %q=%v — it is not an answer "+
				"the sender gave (stored data: %v)", control, v, sub.Data)
		}
	}
	if sub.Data["b1"] != "Ali" || sub.Data["b2"] != "ali@example.test" {
		t.Errorf("the stored submission lost a real answer: %v", sub.Data)
	}
}

// orderFormFixture adds a payment form (a product block plus name/email) to the
// fixture's account, so the SAME account submits through the order route.
func orderFormFixture(t *testing.T, f issue622Fixture) (slug, prodID string) {
	t.Helper()
	ctx := context.Background()
	prod := &dbstore.Product{
		AccountID: f.accountID, Name: "Tiket", PriceSen: 1500,
		Type: dbstore.SaleTypeOneTime, Active: true,
	}
	if err := dbstore.NewProductStore(f.pool).Create(ctx, prod); err != nil {
		t.Fatalf("create product: %v", err)
	}
	slug = "i622-order-" + f.accountID[:8]
	form := &dbstore.Form{
		AccountID: f.accountID, Slug: slug, Title: "I622 order form",
		Status: dbstore.FormStatusPublished,
		Config: map[string]any{
			"blocks": []any{
				map[string]any{"id": "p1", "type": "product", "product_id": prod.ID, "value": "Tiket"},
				map[string]any{"id": "b1", "type": "name", "value": "Full name", "required": true},
				map[string]any{"id": "b2", "type": "email", "value": "Email", "required": true},
			},
		},
	}
	if err := f.h.Forms.Create(ctx, form); err != nil {
		t.Fatalf("create order form: %v", err)
	}
	f.h.ProductVariations = dbstore.NewProductVariationStore(f.pool)
	return slug, prod.ID
}

// TestFormOrderSaleFieldsStoreOnlyTheSendersAnswers guards the second write path
// the card names: order.go snapshots in.Fields onto the sale.
func TestFormOrderSaleFieldsStoreOnlyTheSendersAnswers(t *testing.T) {
	f := issue622New(t)
	ctx := context.Background()
	orderSlug, prodID := orderFormFixture(t, f)

	body := `{"client":{"full_name":"Ali","email":"ali@example.test"},"fields":{` +
		`"b1":"Ali","b2":"ali@example.test","` + antiSpamHoneypotField + `":"",` +
		`"` + antiSpamRenderField + `":"1700000000"},` +
		`"items":[{"product_id":"` + prodID + `","quantity":1}],"payment_method":"bank_direct"}`
	mux := http.NewServeMux()
	f.h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, "/"+orderSlug+"/order", bytes.NewBufferString(body))
	req.Host = f.workspaceSl + ".jomform.com"
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("order status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}

	sales, err := f.h.Sales.ListByAccount(ctx, f.accountID, 10)
	if err != nil || len(sales) == 0 {
		t.Fatalf("read the created sale (err %v, %d sales)", err, len(sales))
	}
	fields, err := f.h.Sales.SaleFields(ctx, sales[0].ID)
	if err != nil {
		t.Fatalf("read the sale's field snapshot: %v", err)
	}
	for _, control := range []string{antiSpamHoneypotField, antiSpamRenderField} {
		if v, ok := fields[control]; ok {
			t.Errorf("the sale's field snapshot carries the renderer's control %q=%v — it is not an "+
				"answer the buyer gave (sale fields: %v)", control, v, fields)
		}
	}
	if fields["b1"] != "Ali" || fields["b2"] != "ali@example.test" {
		t.Errorf("the sale's field snapshot lost a real answer: %v", fields)
	}
}
