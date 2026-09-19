package httpapi

// Issue #598: the dashboard's per-form "Submissions" figure, and what it means
// for each form mode.
//
// THE DEFECT (as reported): the dashboard rendered one label — "Submissions:" —
// next to a figure that was `COUNT(sales.id) ... FROM forms LEFT JOIN sales s`
// for EVERY form. A response-mode form therefore reported 0 forever (it writes
// `submissions`, never `sales`, so the LEFT JOIN had nothing to count), and for
// a payment form the label named a count of ORDERS "Submissions".
//
// THE FIX (PR #603/#604) made the figure follow the table the form actually
// writes: a response form counts `submissions`, a payment form counts `sales`.
// #598's residual, covered here, is the second half: nothing pinned that CASE,
// and the only existing test (`sales_form_filter_issue604_test.go`'s
// TestFormAnalyticsCountsResponseSubmissions) seeded its rows through the STORE
// (`subs.Create`) and read the figure off `h.Sales.FormAnalyticsAccounts`
// directly.
//
// WHY THAT WAS NOT ENOUGH, and what these tests do instead:
//   * A store-level fixture writes rows that the query may or may not be able
//     to see depending on which table it reads — but it never proves the
//     PUBLIC WRITE PATH produces them. These tests drive the real public submit
//     route (POST /{slug}/order on the workspace subdomain, the same handler a
//     buyer's browser hits) so the rows are created by production code.
//   * Reading `FormAnalyticsAccounts` asserts the store's answer, not the
//     figure the dashboard actually shows. These tests read the figure through
//     the real authed route (GET /v1/rest/dashboard-bundle) — the exact request
//     the dashboard page issues — and through the standalone analytics route,
//     and require the two to agree.
//
// MUTATION PROOF (see the PR body for the table): reverting the `CASE` in
// dbstore/analytics.go to a plain `COUNT(s.id)` must make the response test go
// RED naming the count, and restoring it must go green. Every assertion below
// is written so that a sales-only count cannot satisfy it.
//
// Integration tests against the real test Postgres; skipped without
// JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// analyticsBundleRow returns the form_analytics row for one form, as the
// dashboard reads it. It fails the test when the row is absent, so a query
// that silently returns nothing cannot pass as "0".
func analyticsBundleRow(t *testing.T, body map[string]any, formID string) map[string]any {
	t.Helper()
	rows, ok := body["form_analytics"].([]any)
	if !ok {
		t.Fatalf("form_analytics is not a list: %v", body["form_analytics"])
	}
	for _, r := range rows {
		row, ok := r.(map[string]any)
		if ok && row["form_id"] == formID {
			return row
		}
	}
	t.Fatalf("form_analytics has no row for form %s — the dashboard would render nothing for it: %v", formID, rows)
	return nil
}

// countSubmissionsRoute reads the independent submission count through the real
// authed REST route (GET /v1/rest/forms/{id}/submissions/count), which is the
// cross-check #598's acceptance used on prod.
func countSubmissionsRoute(t *testing.T, h *RESTHandlers, rawKey, formID string) int64 {
	t.Helper()
	code, body := salesGet(t, h, rawKey, "/v1/rest/forms/"+formID+"/submissions/count")
	if code != http.StatusOK {
		t.Fatalf("submissions/count status = %d, want 200 (body %s)", code, body)
	}
	var resp struct {
		Count int64 `json:"count"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode submissions/count: %v (%s)", err, body)
	}
	return resp.Count
}

// TestDashboardFormEntriesCountsResponseSubmissions is the regression guard for
// the response half. Its rows come from the real public submit route and its
// figure comes from the real authed dashboard route — so it fails both when the
// CASE is removed (the count falls to the sales table, which is empty for this
// form) and when the writer stops recording submissions.
func TestDashboardFormEntriesCountsResponseSubmissions(t *testing.T) {
	ctx := context.Background()
	h, pool, accountID, formID := salesRowFixture(t, "response")
	raw := bundleAPIKey(t, ctx, pool, accountID)

	var workspaceSlug, formSlug string
	if err := pool.QueryRow(ctx,
		`SELECT w.slug, f.slug FROM forms f JOIN accounts a ON a.id=f.account_id
		 JOIN workspaces w ON w.id=a.workspace_id WHERE f.id=$1`, formID).Scan(&workspaceSlug, &formSlug); err != nil {
		t.Fatalf("resolve slugs: %v", err)
	}

	// Four entries through the PUBLIC submit route (a buyer's path), not the
	// store — a store-level fixture cannot observe which table the read uses,
	// and cannot prove the public writer produces these rows at all.
	const entries = 4
	for i := 0; i < entries; i++ {
		postSubmission(t, pool, workspaceSlug, formSlug,
			`{"client":{"full_name":"Respondent `+itoaSmall(i)+`","email":"it598+`+itoaSmall(i)+`@example.test"},
			  "fields":{"b1":"Respondent `+itoaSmall(i)+`","b2":"it598+`+itoaSmall(i)+`@example.test","b3":"answer-`+itoaSmall(i)+`"}}`)
	}

	// No sale exists for a response form — this is what makes the pre-fix
	// (sales-based) count read 0, and it is asserted so the test cannot pass
	// vacuously against a fixture that accidentally has sales.
	var salesRows int64
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM sales WHERE form_id=$1`, formID).Scan(&salesRows); err != nil {
		t.Fatalf("count sales: %v", err)
	}
	if salesRows != 0 {
		t.Fatalf("fixture has %d sales rows for a response form; the pre-fix count is then not 0 and this test proves nothing", salesRows)
	}

	// The figure the dashboard reads, through the REAL authed route.
	code, bundle := bundleGet(t, h, raw, "/v1/rest/dashboard-bundle")
	if code != http.StatusOK {
		t.Fatalf("dashboard-bundle status = %d, want 200", code)
	}
	row := analyticsBundleRow(t, bundle, formID)

	if got := row["submissions"].(float64); int64(got) != entries {
		t.Errorf("dashboard form_analytics.submissions = %v, want %d — the counter must count the `submissions` table for a response form (0 means it read `sales`, which is empty for this form)",
			row["submissions"], entries)
	}
	// The mode must ride along, or the dashboard cannot label the figure.
	if row["mode"] != dbstore.FormModeResponse {
		t.Errorf("dashboard form_analytics.mode = %v, want %q — the dashboard labels the counter off this", row["mode"], dbstore.FormModeResponse)
	}

	// The standalone analytics route is the other reader of the same query and
	// must report the identical number. It answers a bare JSON ARRAY (not an
	// object), so it is read raw rather than through bundleGet.
	code, rawBody := salesGet(t, h, raw, "/v1/rest/analytics/forms")
	if code != http.StatusOK {
		t.Fatalf("/v1/rest/analytics/forms status = %d, want 200 (body %s)", code, rawBody)
	}
	var direct []map[string]any
	if err := json.Unmarshal(rawBody, &direct); err != nil {
		t.Fatalf("decode /v1/rest/analytics/forms: %v (%s)", err, rawBody)
	}
	var found bool
	for _, rr := range direct {
		if rr["form_id"] != formID {
			continue
		}
		found = true
		if got := rr["submissions"].(float64); int64(got) != entries {
			t.Errorf("/v1/rest/analytics/forms submissions = %v, want %d (it must agree with the dashboard bundle)", rr["submissions"], entries)
		}
	}
	if !found {
		t.Fatalf("/v1/rest/analytics/forms has no row for %s: %s", formID, rawBody)
	}

	// And the independent counter the acceptance criteria cross-check against.
	if got := countSubmissionsRoute(t, h, raw, formID); got != entries {
		t.Errorf("/v1/rest/forms/{id}/submissions/count = %d, want %d — the dashboard figure and this counter must agree", got, entries)
	}
}

// TestDashboardFormEntriesCountsPaymentFormSales is the discriminating control
// and the double-count guard.
//
// A payment form writes SALES and nothing to `submissions`. Its figure must
// therefore count sales rows — every status, which is what "entries recorded"
// means — and must be exactly that count, not the sum of both tables. The
// fixture also seeds a stray `submissions` row for the payment form (a row a
// response-mode submit would have written before the form's mode was switched,
// which is the only way both tables can describe one form): if the query ever
// adds the two together, this test reads 3 and fails.
func TestDashboardFormEntriesCountsPaymentFormSales(t *testing.T) {
	ctx := context.Background()
	h, pool, accountID, formID := salesRowFixture(t, "payment")
	raw := bundleAPIKey(t, ctx, pool, accountID)

	sales := dbstore.NewSaleStore(pool)
	for i, status := range []string{dbstore.SaleConfirmed, dbstore.SaleFailed} {
		if err := sales.Create(ctx, &dbstore.Sale{
			AccountID: accountID, FormID: formID, RefCode: "IT598-" + itoaSmall(i) + "-" + accountID[:8],
			Client:   dbstore.SaleClient{FullName: "Buyer One", Email: "buyer@example.test"},
			Items:    []dbstore.SaleItem{{ProductID: "it-p1", Name: "Tee", PriceSen: 1000, Qty: 1}},
			TotalSen: 1000, SaleType: dbstore.SaleTypeOneTime,
			PaymentMethod: "chip", Status: status,
		}); err != nil {
			t.Fatalf("create sale: %v", err)
		}
	}
	// The double-count trap: a submission row that belongs to this payment
	// form. Written directly because the public path can no longer produce one
	// for a payment form — that is the point.
	if _, err := pool.Exec(ctx,
		`INSERT INTO submissions (form_id, account_id, data) VALUES ($1, $2, $3)`,
		formID, accountID, map[string]any{"b3": "stale-response-row"}); err != nil {
		t.Fatalf("seed stray submission: %v", err)
	}

	code, bundle := bundleGet(t, h, raw, "/v1/rest/dashboard-bundle")
	if code != http.StatusOK {
		t.Fatalf("dashboard-bundle status = %d, want 200", code)
	}
	row := analyticsBundleRow(t, bundle, formID)

	if got := row["submissions"].(float64); int64(got) != 2 {
		t.Errorf("dashboard form_analytics.submissions = %v, want 2 (the two sales rows) — 3 means the two tables were summed (double counting), 0 means a payment form's real entries were reported as none",
			row["submissions"])
	}
	if row["mode"] != dbstore.FormModePayment {
		t.Errorf("dashboard form_analytics.mode = %v, want %q", row["mode"], dbstore.FormModePayment)
	}
	// A payment form's money figures stay their own separately named fields —
	// the entries count must not have swallowed them.
	//
	// paid_count is the PAID SET (status IN ('paid','confirmed')) and
	// confirmed_count is the SUBSET with status='confirmed', so 1 + 1 = 1 of
	// this form's 2 sales reached the paid set, and they are disjoint from the
	// failed sale. This is the same shape the dashboard adds up.
	if got := row["paid_count"].(float64); int64(got) != 1 {
		t.Errorf("paid_count = %v, want 1 (paid set = the one confirmed sale)", row["paid_count"])
	}
	if got := row["confirmed_count"].(float64); int64(got) != 1 {
		t.Errorf("confirmed_count = %v, want 1 (the confirmed sale; it is a SUBSET of paid_count, not a separate bucket)", row["confirmed_count"])
	}
	if got := row["failed_count"].(float64); int64(got) != 1 {
		t.Errorf("failed = %v, want 1 (the failed sale)", row["failed_count"])
	}
	// The entries count is the whole table for this form — the two sales — and
	// must be bigger than any single status bucket, so a figure that collapsed
	// onto one bucket (or onto the stray submission row) fails here.
	if subs := row["submissions"].(float64); int64(subs) != 2 {
		t.Errorf("submissions = %v, want 2 — the entries figure counts every status", row["submissions"])
	}
}

// TestDashboardFormEntriesLegacyFormHasNoModeKey pins the load-bearing default
// on the read path: a form created before response mode existed carries no
// `mode` key at all, is a payment form (dbstore.FormMode), and must therefore
// read its sales — never 0, and never labelled as submissions.
func TestDashboardFormEntriesLegacyFormHasNoModeKey(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, _ := bundleFixture(t, ctx, pool)

	forms := dbstore.NewFormStore(pool)
	f := &dbstore.Form{
		AccountID: accountID, Slug: "it-598-legacy-" + accountID[:8], Title: "Legacy order form",
		Status: "published",
		// No "mode" key: the pre-#171 shape.
		Config: map[string]any{"blocks": []any{map[string]any{"id": "b1", "type": "name", "value": "Name"}}},
	}
	if err := forms.Create(ctx, f); err != nil {
		t.Fatalf("create legacy form: %v", err)
	}
	if err := dbstore.NewSaleStore(pool).Create(ctx, &dbstore.Sale{
		AccountID: accountID, FormID: f.ID, RefCode: "IT598-LEG-" + accountID[:8],
		Client:   dbstore.SaleClient{FullName: "Buyer", Email: "buyer@example.test"},
		Items:    []dbstore.SaleItem{{ProductID: "it-p1", Name: "Tee", PriceSen: 500, Qty: 1}},
		TotalSen: 500, SaleType: dbstore.SaleTypeOneTime,
		PaymentMethod: "chip", Status: dbstore.SaleConfirmed,
	}); err != nil {
		t.Fatalf("create sale: %v", err)
	}

	h := newBundleHandlers(pool)
	h.Submissions = dbstore.NewSubmissionStore(pool)
	raw := bundleAPIKey(t, ctx, pool, accountID)

	code, bundle := bundleGet(t, h, raw, "/v1/rest/dashboard-bundle")
	if code != http.StatusOK {
		t.Fatalf("dashboard-bundle status = %d, want 200", code)
	}
	row := analyticsBundleRow(t, bundle, f.ID)

	if row["mode"] != dbstore.FormModePayment {
		t.Errorf("legacy form mode = %v, want %q — a missing mode key must read as payment, or every pre-#171 form is mislabelled",
			row["mode"], dbstore.FormModePayment)
	}
	if got := row["submissions"].(float64); int64(got) != 1 {
		t.Errorf("legacy form submissions = %v, want 1 (its one sale) — the pre-#171 default is a payment form", row["submissions"])
	}
}

// itoa is a tiny local integer formatter so the fixture strings stay readable
// without threading strconv through every call.
func itoaSmall(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}
