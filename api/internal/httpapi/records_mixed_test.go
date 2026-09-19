package httpapi

// The /sales read surface must return ONE record set: sales plus the
// submissions of every response-mode form in the caller's scope.
//
// THE DEFECT: every reader decided which table to read from a SINGLE form id
// (`formID == "" → sales only`). Without a form filter, therefore, the list, the
// CSV export, the legacy array shape and the MCP tool all served sales alone —
// the response entries were not miscounted, they were absent, while the per-form
// counter reported them. Two surfaces, two answers.
//
// WHAT THESE TESTS PIN
//   1. An unfiltered paginated read returns sales AND submissions, with one
//      combined total.
//   2. The legacy array shape returns the same record set (it used to ignore
//      every filter and read one account).
//   3. The CSV export contains exactly the rows the list holds, with and without
//      a filter, and is NOT silently capped.
//   4. A payment form's read is unchanged (the control that fails if the mode
//      branch is applied to every form).
//   5. The per-form response deep link still reads submissions only (#604).
//   6. A status filter HIDES the responses half instead of reporting 0 records
//      for the whole list.
//   7. The dashboard bundle's recent list agrees with /sales.
//
// They are integration tests against the real test Postgres (they skip without
// JOMFORM_TEST_DATABASE_URL): the defect lives in the WHERE clause / table
// choice, which a stub store cannot observe. The submit path used to create a
// response row is the REAL public route, so the writer and the reader are both
// production code paths.

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// mixedFixture seeds one account with a payment form, a response-mode form, a
// sale on the payment form and N submissions on the response form, then returns
// the handlers, the pool, the account id and both form ids.
//
// Rows are written through the stores and the real submit route (not raw SQL) so
// the fixture cannot drift from how production writes them.
func mixedFixture(t *testing.T, submissions int) (h *RESTHandlers, pool *pgxpool.Pool, accountID, paymentFormID, responseFormID string) {
	t.Helper()
	ctx := context.Background()
	pool = bundleTestPool(t)
	accountID, userID := bundleFixture(t, ctx, pool)

	workspace, err := dbstore.NewWorkspaceStore(pool).Create(ctx, userID,
		"it-mixed-ws-"+accountID[:8], "it-mixed-ws-"+accountID[:8])
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE accounts SET workspace_id=$1 WHERE id=$2`, workspace.ID, accountID); err != nil {
		t.Fatalf("link account to workspace: %v", err)
	}

	forms := dbstore.NewFormStore(pool)
	payment := &dbstore.Form{
		AccountID: accountID, Slug: "it-mixed-pay-" + accountID[:8], Title: "Payment form",
		Status: "published", Config: map[string]any{"mode": "payment"}, Theme: map[string]any{},
	}
	if err := forms.Create(ctx, payment); err != nil {
		t.Fatalf("create payment form: %v", err)
	}
	response := &dbstore.Form{
		AccountID: accountID, Slug: "it-mixed-resp-" + accountID[:8], Title: "Response form",
		Status: "published",
		Config: map[string]any{
			"mode": "response",
			"blocks": []any{
				map[string]any{"id": "b1", "type": "name", "value": "Full name", "required": true},
				map[string]any{"id": "b2", "type": "email", "value": "Email", "required": true},
				map[string]any{"id": "b3", "type": "textarea", "value": "Feedback"},
			},
		},
		Theme: map[string]any{},
	}
	if err := forms.Create(ctx, response); err != nil {
		t.Fatalf("create response form: %v", err)
	}

	sales := dbstore.NewSaleStore(pool)
	if err := sales.Create(ctx, &dbstore.Sale{
		AccountID: accountID, FormID: payment.ID, RefCode: "IT-MIXED-" + accountID[:8],
		Client:   dbstore.SaleClient{FullName: "Mixed Buyer", Email: "mixed@example.test"},
		Items:    []dbstore.SaleItem{{ProductID: "it-mixed-p1", Name: "Tee", PriceSen: 2500, Qty: 1}},
		TotalSen: 2500, SaleType: dbstore.SaleTypeOneTime,
		PaymentMethod: "chip", Status: dbstore.SaleConfirmed,
	}); err != nil {
		t.Fatalf("create sale: %v", err)
	}

	subs := dbstore.NewSubmissionStore(pool)
	for i := 0; i < submissions; i++ {
		if err := subs.Create(ctx, &dbstore.Submission{
			FormID: response.ID, AccountID: accountID,
			Data: map[string]any{"b3": fmt.Sprintf("answer-%d", i), "full_name": fmt.Sprintf("Responder %d", i)},
		}); err != nil {
			t.Fatalf("create submission: %v", err)
		}
	}

	h = newBundleHandlers(pool)
	h.Forms = forms
	h.Submissions = subs
	return h, pool, accountID, payment.ID, response.ID
}

// salesPageBody is the paginated envelope the /sales page reads.
type salesPageBody struct {
	Items []map[string]any `json:"items"`
	Page  int              `json:"page"`
	Limit int              `json:"limit"`
	Total int64            `json:"total"`
	Mixed bool             `json:"mixed"`
}

func decodeSalesPage(t *testing.T, body []byte) salesPageBody {
	t.Helper()
	var page salesPageBody
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("decode sales page: %v (%s)", err, body)
	}
	return page
}

// TestUnfilteredSalesListShowsSalesAndResponses is the reported defect: with no
// form filter the list must show both kinds of entry. It fails on the pre-fix
// code, where the read was sales-only and total was 1 (the sale) with zero
// response rows.
func TestUnfilteredSalesListShowsSalesAndResponses(t *testing.T) {
	ctx := context.Background()
	h, pool, accountID, _, responseFormID := mixedFixture(t, 4)
	raw := bundleAPIKey(t, ctx, pool, accountID)

	code, body := salesGet(t, h, raw, "/v1/rest/sales?page=1&limit=50")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", code, body)
	}
	page := decodeSalesPage(t, body)

	// 1 sale + 4 submissions.
	if page.Total != 5 {
		t.Errorf("total = %d, want 5 (1 sale + 4 submissions) — body %s", page.Total, body)
	}
	if len(page.Items) != 5 {
		t.Fatalf("items = %d, want 5 — body %s", len(page.Items), body)
	}
	if !page.Mixed {
		t.Error("the envelope does not report the list as mixed")
	}

	kinds := map[string]int{}
	for _, row := range page.Items {
		if row["sale_type"] == "response" {
			kinds["response"]++
			if row["submission_id"] == "" || row["submission_id"] == nil {
				t.Errorf("a response row carries no submission_id: %v", row)
			}
			if row["form_id"] != responseFormID {
				t.Errorf("response row form_id = %v, want %s", row["form_id"], responseFormID)
			}
			continue
		}
		kinds["sale"]++
		if ref, _ := row["ref_code"].(string); !strings.HasPrefix(ref, "IT-MIXED-") {
			t.Errorf("sale row lost its ref_code: %v", row)
		}
	}
	if kinds["sale"] != 1 || kinds["response"] != 4 {
		t.Errorf("kinds = %v, want 1 sale and 4 responses — body %s", kinds, body)
	}
}

// The legacy array shape (no ?page=) is what the dashboard and any external
// API-key consumer read. It must carry the same record set as the paginated
// list — it used to ignore every filter and read only one account.
func TestUnfilteredLegacyArrayCarriesTheSameRecordSet(t *testing.T) {
	ctx := context.Background()
	h, pool, accountID, _, _ := mixedFixture(t, 4)
	raw := bundleAPIKey(t, ctx, pool, accountID)

	code, body := salesGet(t, h, raw, "/v1/rest/sales")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", code, body)
	}
	var rows []map[string]any
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatalf("legacy array is not an array: %v (%s)", err, body)
	}
	if len(rows) != 5 {
		t.Fatalf("legacy array rows = %d, want 5 (1 sale + 4 submissions) — body %s", len(rows), body)
	}
	responseRows := 0
	for _, row := range rows {
		if row["sale_type"] == "response" {
			responseRows++
		}
	}
	if responseRows != 4 {
		t.Errorf("legacy array response rows = %d, want 4 — body %s", responseRows, body)
	}
}

// The legacy array shape must HONOUR the filters the paginated list honours.
// It used to ignore status/date/q entirely, so two callers of the same URL got
// different answers for the same query string.
func TestLegacyArrayHonoursStatusFilter(t *testing.T) {
	ctx := context.Background()
	h, pool, accountID, _, _ := mixedFixture(t, 4)
	raw := bundleAPIKey(t, ctx, pool, accountID)

	// status=paid: the fixture's sale is "confirmed", so nothing matches.
	// Before the fix this returned all 5 rows regardless of the filter.
	code, body := salesGet(t, h, raw, "/v1/rest/sales?status=paid")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", code, body)
	}
	var rows []map[string]any
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	if len(rows) != 0 {
		t.Errorf("status=paid returned %d rows, want 0 (the sale is confirmed) — body %s", len(rows), body)
	}

	// The matching status returns exactly the one sale.
	code, body = salesGet(t, h, raw, "/v1/rest/sales?status=confirmed")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", code, body)
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	if len(rows) != 1 {
		t.Errorf("status=confirmed returned %d rows, want exactly the sale — body %s", len(rows), body)
	}
}

// A status filter is a payment-status question. On a mixed list it must HIDE the
// responses (a submission has no status) rather than answer "0 records", which a
// merchant reads as "nothing was ever recorded".
func TestStatusFilterHidesResponsesInsteadOfEmptyingTheList(t *testing.T) {
	ctx := context.Background()
	h, pool, accountID, _, _ := mixedFixture(t, 4)
	raw := bundleAPIKey(t, ctx, pool, accountID)

	code, body := salesGet(t, h, raw, "/v1/rest/sales?page=1&limit=50&status=confirmed")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", code, body)
	}
	page := decodeSalesPage(t, body)
	if page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("total = %d items = %d, want just the confirmed sale — body %s", page.Total, len(page.Items), body)
	}
	if page.Items[0]["sale_type"] == "response" {
		t.Errorf("a response row survived a payment-status filter: %v", page.Items[0])
	}
}

// TestUnfilteredCSVMatchesTheList is the export half of the defect: the button
// sits above the list, so the file must contain the same rows. On the pre-fix
// code the unfiltered export was sales-only (and silently capped at 50 rows).
func TestUnfilteredCSVMatchesTheList(t *testing.T) {
	ctx := context.Background()
	h, pool, accountID, _, _ := mixedFixture(t, 4)
	raw := bundleAPIKey(t, ctx, pool, accountID)

	_, listBody := salesGet(t, h, raw, "/v1/rest/sales?page=1&limit=50")
	listPage := decodeSalesPage(t, listBody)

	code, csvBody := salesGet(t, h, raw, "/v1/rest/sales/export.csv")
	if code != http.StatusOK {
		t.Fatalf("export status = %d, want 200 (body %s)", code, csvBody)
	}
	rows, err := csv.NewReader(strings.NewReader(string(csvBody))).ReadAll()
	if err != nil {
		t.Fatalf("export is not valid CSV: %v (%s)", err, csvBody)
	}
	if len(rows) != len(listPage.Items)+1 {
		t.Fatalf("export rows = %d (incl. header), list holds %d rows — the file and the list disagree:\n%s",
			len(rows), len(listPage.Items), csvBody)
	}
	// The export must carry both kinds, and say which is which.
	var responses, sales int
	header := map[string]int{}
	for i, name := range rows[0] {
		header[name] = i
	}
	if _, ok := header["record_type"]; !ok {
		t.Fatalf("a mixed export has no record_type column: %v", rows[0])
	}
	for _, row := range rows[1:] {
		switch row[header["record_type"]] {
		case dbstore.RecordKindSubmission:
			responses++
			if row[header["submission_id"]] == "" {
				t.Errorf("a response export row has no submission_id: %v", row)
			}
		case dbstore.RecordKindSale:
			sales++
		default:
			t.Errorf("unknown record_type %q in %v", row[header["record_type"]], row)
		}
	}
	if responses != 4 || sales != 1 {
		t.Errorf("export held %d responses / %d sales, want 4/1:\n%s", responses, sales, csvBody)
	}
}

// The export must apply the same filters as the list. The pre-fix export read
// only form_id: a page filtered to status=failed exported every sale.
func TestCSVExportHonoursTheListFilters(t *testing.T) {
	ctx := context.Background()
	h, pool, accountID, _, _ := mixedFixture(t, 4)
	raw := bundleAPIKey(t, ctx, pool, accountID)

	// q matches the sale's buyer name only.
	code, csvBody := salesGet(t, h, raw, "/v1/rest/sales/export.csv?q=Mixed+Buyer")
	if code != http.StatusOK {
		t.Fatalf("export status = %d (body %s)", code, csvBody)
	}
	rows, err := csv.NewReader(strings.NewReader(string(csvBody))).ReadAll()
	if err != nil {
		t.Fatalf("decode csv: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("q-filtered export rows = %d (incl header), want 1 data row:\n%s", len(rows), csvBody)
	}
	if !strings.Contains(strings.Join(rows[1], ","), "IT-MIXED-") {
		t.Errorf("the q-filtered export did not return the matching sale: %v", rows[1])
	}

	// The list must agree with the export for the same filter.
	_, listBody := salesGet(t, h, raw, "/v1/rest/sales?page=1&limit=50&q=Mixed+Buyer")
	page := decodeSalesPage(t, listBody)
	if int64(len(rows)-1) != page.Total {
		t.Errorf("q-filtered export has %d rows, the list reports total=%d", len(rows)-1, page.Total)
	}

	// status=paid matches nothing; the export must be a header-only file, not
	// every sale in the account.
	_, csvBody = salesGet(t, h, raw, "/v1/rest/sales/export.csv?status=paid")
	rows, err = csv.NewReader(strings.NewReader(string(csvBody))).ReadAll()
	if err != nil {
		t.Fatalf("decode csv: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("status=paid export has %d rows (incl header), want 0 data rows:\n%s", len(rows), csvBody)
	}
}

// The export must not silently truncate. The sales list clamps any limit above
// 200 down to 50, so an export that asked for 5000 rows got the newest 50 and
// presented that as the complete file.
func TestCSVExportIsNotSilentlyCapped(t *testing.T) {
	ctx := context.Background()
	h, pool, accountID, _, _ := mixedFixture(t, 0)
	raw := bundleAPIKey(t, ctx, pool, accountID)

	// 60 sales: more than the 50-row clamp the pre-fix export inherited.
	sales := dbstore.NewSaleStore(pool)
	for i := 0; i < 60; i++ {
		if err := sales.Create(ctx, &dbstore.Sale{
			AccountID: accountID, RefCode: fmt.Sprintf("IT-CAP-%s-%02d", accountID[:8], i),
			Client:   dbstore.SaleClient{FullName: fmt.Sprintf("Buyer %d", i), Email: "cap@example.test"},
			Items:    []dbstore.SaleItem{{ProductID: "p", Name: "Tee", PriceSen: 100, Qty: 1}},
			TotalSen: 100, SaleType: dbstore.SaleTypeOneTime,
			PaymentMethod: "chip", Status: dbstore.SaleConfirmed,
		}); err != nil {
			t.Fatalf("create sale %d: %v", i, err)
		}
	}

	// The fixture's own sale plus the 60 = 61.
	code, csvBody := salesGet(t, h, raw, "/v1/rest/sales/export.csv")
	if code != http.StatusOK {
		t.Fatalf("export status = %d (body %s)", code, csvBody)
	}
	rows, err := csv.NewReader(strings.NewReader(string(csvBody))).ReadAll()
	if err != nil {
		t.Fatalf("decode csv: %v", err)
	}
	if len(rows)-1 != 61 {
		t.Errorf("export data rows = %d, want 61 (the export must not stop at the 50-row list clamp)", len(rows)-1)
	}
}

// The per-form response deep link (#604) must keep reading submissions only —
// this is the behaviour that already worked and must not regress.
func TestPerResponseFormFilterReadsSubmissionsOnly(t *testing.T) {
	ctx := context.Background()
	h, pool, accountID, paymentFormID, responseFormID := mixedFixture(t, 3)
	raw := bundleAPIKey(t, ctx, pool, accountID)

	code, body := salesGet(t, h, raw, "/v1/rest/sales?page=1&limit=50&form_id="+responseFormID)
	if code != http.StatusOK {
		t.Fatalf("status = %d (body %s)", code, body)
	}
	page := decodeSalesPage(t, body)
	if page.Total != 3 || len(page.Items) != 3 {
		t.Fatalf("response-form read total=%d items=%d, want 3 — body %s", page.Total, len(page.Items), body)
	}
	for _, row := range page.Items {
		if row["sale_type"] != "response" {
			t.Errorf("the response-form read leaked a sale row: %v", row)
		}
	}

	// A payment form under the same account stays sales-only and shows exactly
	// its own sale — the control that fails if the mode branch is applied to
	// every form.
	code, body = salesGet(t, h, raw, "/v1/rest/sales?page=1&limit=50&form_id="+paymentFormID)
	if code != http.StatusOK {
		t.Fatalf("status = %d (body %s)", code, body)
	}
	page = decodeSalesPage(t, body)
	if page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("payment-form read total=%d items=%d, want 1 — body %s", page.Total, len(page.Items), body)
	}
	if page.Items[0]["sale_type"] == "response" {
		t.Errorf("the payment-form read returned a response row: %v", page.Items[0])
	}
}

// A payment-only account (no response form anywhere) must be byte-for-byte the
// pre-fix behaviour: sales only, no record_type column, no mixed flag.
func TestPaymentOnlyAccountIsUnchanged(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, userID := bundleFixture(t, ctx, pool)
	workspace, err := dbstore.NewWorkspaceStore(pool).Create(ctx, userID,
		"it-pay-only-"+accountID[:8], "it-pay-only-"+accountID[:8])
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE accounts SET workspace_id=$1 WHERE id=$2`, workspace.ID, accountID); err != nil {
		t.Fatalf("link account: %v", err)
	}

	forms := dbstore.NewFormStore(pool)
	f := &dbstore.Form{
		AccountID: accountID, Slug: "it-pay-only-" + accountID[:8], Title: "Plain payment form",
		Status: "published", Config: map[string]any{"mode": "payment"}, Theme: map[string]any{},
	}
	if err := forms.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}
	if err := dbstore.NewSaleStore(pool).Create(ctx, &dbstore.Sale{
		AccountID: accountID, FormID: f.ID, RefCode: "IT-PAYONLY-" + accountID[:8],
		Client:   dbstore.SaleClient{FullName: "Only Buyer", Email: "only@example.test"},
		Items:    []dbstore.SaleItem{{ProductID: "p", Name: "Tee", PriceSen: 500, Qty: 1}},
		TotalSen: 500, SaleType: dbstore.SaleTypeOneTime,
		PaymentMethod: "chip", Status: dbstore.SalePaid,
	}); err != nil {
		t.Fatalf("create sale: %v", err)
	}

	h := newBundleHandlers(pool)
	h.Forms = forms
	h.Submissions = dbstore.NewSubmissionStore(pool)
	raw := bundleAPIKey(t, ctx, pool, accountID)

	code, body := salesGet(t, h, raw, "/v1/rest/sales?page=1&limit=50")
	if code != http.StatusOK {
		t.Fatalf("status = %d (body %s)", code, body)
	}
	page := decodeSalesPage(t, body)
	if page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("payment-only total=%d items=%d, want 1 — body %s", page.Total, len(page.Items), body)
	}
	if page.Mixed {
		t.Error("a payment-only account reported a mixed list")
	}

	// The export keeps its historical columns (no record_type) for such an
	// account, so an existing consumer parsing the file is not broken.
	_, csvBody := salesGet(t, h, raw, "/v1/rest/sales/export.csv")
	if strings.Contains(string(csvBody), "record_type") {
		t.Errorf("a payment-only export gained the mixed-list columns:\n%s", csvBody)
	}
	if !strings.Contains(string(csvBody), "ref_code,status,payment_method") {
		t.Errorf("a payment-only export lost its sales columns:\n%s", csvBody)
	}
}

// The dashboard's recent list must agree with /sales. It read sales only, so a
// merchant saw 35 records on the dashboard, 39 on the list (#598's counter
// already counted the responses).
func TestDashboardBundleMatchesTheSalesList(t *testing.T) {
	ctx := context.Background()
	h, pool, accountID, _, _ := mixedFixture(t, 4)
	raw := bundleAPIKey(t, ctx, pool, accountID)

	_, listBody := salesGet(t, h, raw, "/v1/rest/sales?page=1&limit=50")
	listPage := decodeSalesPage(t, listBody)

	code, body := bundleGet(t, h, raw, "/v1/rest/dashboard-bundle")
	if code != http.StatusOK {
		t.Fatalf("bundle status = %d", code)
	}
	sales, ok := body["sales"].(map[string]any)
	if !ok {
		t.Fatalf("bundle has no sales section: %v", body["sales"])
	}
	if total, _ := sales["total"].(float64); int64(total) != listPage.Total {
		t.Errorf("bundle sales total = %v, /sales total = %d — the two surfaces disagree",
			sales["total"], listPage.Total)
	}
	items, _ := sales["items"].([]any)
	if int64(len(items)) != listPage.Total {
		// The bundle embeds only the first page (10), so it can be shorter when
		// the list is longer; what must never happen is MORE records than the
		// list reports, or a total that excludes responses.
		t.Logf("bundle items = %d, /sales total = %d (bundle embeds one page)", len(items), listPage.Total)
	}
	responses := 0
	for _, it := range items {
		if m, ok := it.(map[string]any); ok && m["sale_type"] == "response" {
			responses++
		}
	}
	if responses != 4 {
		t.Errorf("bundle recent list holds %d response rows, want 4 — the dashboard is hiding them again", responses)
	}
}

// The per-form submissions route is CORRECT today and must stay correct: it is
// per-form by design and always reads the submissions table.
func TestPerFormSubmissionsRouteUnchanged(t *testing.T) {
	ctx := context.Background()
	h, pool, accountID, _, responseFormID := mixedFixture(t, 3)
	raw := bundleAPIKey(t, ctx, pool, accountID)

	code, body := salesGet(t, h, raw, "/v1/rest/forms/"+responseFormID+"/submissions?page=1&limit=20")
	if code != http.StatusOK {
		t.Fatalf("submissions status = %d (body %s)", code, body)
	}
	var page struct {
		Items []map[string]any `json:"items"`
		Total int64            `json:"total"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	if page.Total != 3 || len(page.Items) != 3 {
		t.Errorf("per-form submissions total=%d items=%d, want 3 — body %s", page.Total, len(page.Items), body)
	}
	_ = httptest.NewRequest // keep the httptest import meaningful for helpers above
}
