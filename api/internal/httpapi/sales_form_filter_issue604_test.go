package httpapi

// Issue #604: a response-mode form's entry must be visible on the sales page.
//
// THE BUG (one line): the entry is persisted to the `submissions` table (issue
// #171), but `GET /v1/rest/sales?form_id=<id>` reads only the `sales` table
// (`SaleStore.ListByAccountsFormPage`, predicate `form_id::text = $2` over
// `saleSelect ... FROM sales`). So `/sales?form=<a response form>` returned
// `total: 0` for every status, date range, spam flag and account scope — the page
// looked like nothing had ever been recorded while a submission existed.
//
// These tests are integration tests against the real test Postgres (they skip
// without JOMFORM_TEST_DATABASE_URL) because the defect lives in the WHERE
// clause: a unit test over a stub store cannot observe which table the handler
// read. They drive the REAL public submit route to create the row and the REAL
// authed REST route to read it back, so the writer and the reader are both the
// production code paths (a fixture that inserted the row by hand could not
// reproduce the reported symptom).

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// salesRowFixture seeds a user + workspace + account + a published form with the
// given mode, and returns RESTHandlers wired over the same pool, the account id
// and the form id. Every call creates fresh rows so tests never collide.
func salesRowFixture(t *testing.T, mode string) (*RESTHandlers, *pgxpool.Pool, string, string) {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)

	accountID, userID := bundleFixture(t, ctx, pool)
	workspace, err := dbstore.NewWorkspaceStore(pool).Create(ctx, userID,
		"it-ws-"+accountID[:8], "it-ws-"+accountID[:8])
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE accounts SET workspace_id=$1 WHERE id=$2`, workspace.ID, accountID); err != nil {
		t.Fatalf("link account to workspace: %v", err)
	}

	forms := dbstore.NewFormStore(pool)
	f := &dbstore.Form{
		AccountID: accountID, Slug: "it-sales-" + accountID[:8], Title: "Sales row form",
		Status: "published",
		Config: map[string]any{
			"mode": mode,
			"blocks": []any{
				map[string]any{"id": "b1", "type": "name", "value": "Full name", "required": true},
				map[string]any{"id": "b2", "type": "email", "value": "Email", "required": true},
				map[string]any{"id": "b3", "type": "textarea", "value": "Feedback"},
			},
		},
		Theme: map[string]any{"primary": "#5e6ad2"},
	}
	if err := forms.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}

	h := newBundleHandlers(pool)
	h.Forms = forms
	h.Submissions = dbstore.NewSubmissionStore(pool)
	return h, pool, accountID, f.ID
}

// salesGet calls the authed sales route with the account's API key.
func salesGet(t *testing.T, h *RESTHandlers, rawKey, path string) (int, []byte) {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

// postSubmission drives the real public submit route for a response-mode form,
// exactly as the buyer's browser does. It returns the created submission id.
func postSubmission(t *testing.T, pool *pgxpool.Pool, workspaceSlug, formSlug, body string) string {
	t.Helper()
	h := &PublicHandlers{
		Forms:       dbstore.NewFormStore(pool),
		Sales:       dbstore.NewSaleStore(pool),
		Accounts:    dbstore.NewAccountStore(pool),
		Products:    dbstore.NewProductStore(pool),
		Workspaces:  dbstore.NewWorkspaceStore(pool),
		Submissions: dbstore.NewSubmissionStore(pool),
		BaseURL:     "http://localhost:8080",
	}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, "/"+formSlug+"/order", bytes.NewBufferString(body))
	req.Host = workspaceSlug + ".jomform.com"
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode submit response: %v", err)
	}
	if resp.ID == "" {
		t.Fatalf("submit created no submission id: %s", rec.Body.String())
	}
	return resp.ID
}

// TestSalesFormFilterShowsResponseSubmission is the reported scenario: an entry
// submitted to a response-mode form must appear in the sales page result set for
// that form id. It fails on the pre-fix code (the sales query never touches the
// submissions table, so total stayed 0).
func TestSalesFormFilterShowsResponseSubmission(t *testing.T) {
	ctx := context.Background()
	h, pool, accountID, formID := salesRowFixture(t, "response")
	raw := bundleAPIKey(t, ctx, pool, accountID)

	var workspaceSlug, formSlug string
	if err := pool.QueryRow(ctx,
		`SELECT w.slug, f.slug FROM forms f JOIN accounts a ON a.id=f.account_id
		 JOIN workspaces w ON w.id=a.workspace_id WHERE f.id=$1`, formID).Scan(&workspaceSlug, &formSlug); err != nil {
		t.Fatalf("resolve slugs: %v", err)
	}

	subID := postSubmission(t, pool, workspaceSlug, formSlug,
		`{"client":{"full_name":"Ahmad pintu","email":"buyer00@example.com","phone":"+60123456789"},
		  "fields":{"b1":"Ahmad pintu","b2":"buyer00@example.com","b3":"xda","website":"","jf_ts":"1"}}`)

	// The sales page's exact request: paginated + the form filter.
	code, body := salesGet(t, h, raw, "/v1/rest/sales?page=1&limit=15&form_id="+formID)
	if code != http.StatusOK {
		t.Fatalf("sales status = %d, want 200 (body %s)", code, body)
	}
	var page struct {
		Items []struct {
			SubmissionID string         `json:"submission_id"`
			SaleType     string         `json:"sale_type"`
			FormID       string         `json:"form_id"`
			Buyer        string         `json:"buyer"`
			BuyerEmail   string         `json:"buyer_email"`
			Data         map[string]any `json:"data"`
		} `json:"items"`
		Page  int   `json:"page"`
		Limit int   `json:"limit"`
		Total int64 `json:"total"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("decode sales page: %v (%s)", err, body)
	}

	// The regression: this was 0 before the fix.
	if page.Total != 1 {
		t.Fatalf("sales page total = %d, want 1 (the submission exists) — body %s", page.Total, body)
	}
	if len(page.Items) != 1 {
		t.Fatalf("sales page items = %d, want 1 — body %s", len(page.Items), body)
	}
	row := page.Items[0]
	if row.SubmissionID != subID {
		t.Errorf("row submission_id = %q, want %q", row.SubmissionID, subID)
	}
	if row.SaleType != "response" {
		t.Errorf("row sale_type = %q, want response (the page renders the answers view off this)", row.SaleType)
	}
	if row.FormID != formID {
		t.Errorf("row form_id = %q, want %q", row.FormID, formID)
	}
	// The recorded answers must come through, or the row is unreadable.
	if row.Data["b3"] != "xda" {
		t.Errorf("row data[b3] = %v, want xda", row.Data["b3"])
	}
	if row.Buyer != "Ahmad pintu" {
		t.Errorf("row buyer = %q, want Ahmad pintu (the buyer column reads full_name)", row.Buyer)
	}
	if row.BuyerEmail != "buyer00@example.com" {
		t.Errorf("row buyer_email = %q, want the submitted email", row.BuyerEmail)
	}
}

// TestSalesFormFilterResponseSubmissionScoping pins the ways this read could
// leak or over-match: another form's submissions must not appear under this
// form's filter, and a submission whose account does not match the scoped set
// must not appear even when the form id matches.
//
// The account half is seeded at the query layer rather than through the public
// submit handler, and that is deliberate: the handler derives account_id from
// form.AccountID, and nothing in the codebase re-parents a form to another
// account (grep: the only `UPDATE forms SET account_id` is the 000059 template
// gallery migration, and form.go keeps `AND account_id=$1` on every update), so
// a mismatched pair is not reachable from a production writer. It is still
// reachable as a ROW, and it is exactly what the account predicate exists to
// refuse — so the fixture writes the row directly and says why.
func TestSalesFormFilterResponseSubmissionScoping(t *testing.T) {
	ctx := context.Background()
	h, pool, accountID, formID := salesRowFixture(t, "response")
	// A second response form under the SAME account, and a submission in it.
	other := &dbstore.Form{
		AccountID: accountID, Slug: "it-other-forms-" + accountID[:8], Title: "Other",
		Status: "published",
		Config: map[string]any{
			"mode": "response",
			"blocks": []any{
				map[string]any{"id": "c1", "type": "textarea", "value": "Note"},
			},
		},
		Theme: map[string]any{},
	}
	if err := h.Forms.Create(ctx, other); err != nil {
		t.Fatalf("create other form: %v", err)
	}
	// A submission in a DIFFERENT account.
	_, _, otherAccountID, otherAccountFormID := salesRowFixture(t, "response")

	subs := dbstore.NewSubmissionStore(pool)
	for _, seed := range []struct{ formID, accountID, note string }{
		{formID, accountID, "want-this"},
		{other.ID, accountID, "other-form"},
		{otherAccountFormID, otherAccountID, "other-account"},
	} {
		if err := subs.Create(ctx, &dbstore.Submission{
			FormID: seed.formID, AccountID: seed.accountID,
			Data: map[string]any{"c1": seed.note},
		}); err != nil {
			t.Fatalf("seed submission: %v", err)
		}
	}
	// Same form, foreign account: only a query with the account predicate can
	// refuse this one.
	if _, err := pool.Exec(ctx,
		`INSERT INTO submissions (form_id, account_id, data) VALUES ($1, $2, $3)`,
		formID, otherAccountID, map[string]any{"c1": "foreign-account-same-form"}); err != nil {
		t.Fatalf("seed foreign-account row: %v", err)
	}

	raw := bundleAPIKey(t, ctx, pool, accountID)
	code, body := salesGet(t, h, raw, "/v1/rest/sales?page=1&limit=15&form_id="+formID)
	if code != http.StatusOK {
		t.Fatalf("sales status = %d, want 200 (body %s)", code, body)
	}
	var page struct {
		Items []struct {
			Data map[string]any `json:"data"`
		} `json:"items"`
		Total int64 `json:"total"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("decode sales page: %v (%s)", err, body)
	}
	if page.Total != 1 {
		t.Fatalf("total = %d, want exactly 1 (this form, this account) — body %s", page.Total, body)
	}
	if got := page.Items[0].Data["c1"]; got != "want-this" {
		t.Errorf("row data[c1] = %v, want want-this — another form's or account's submission leaked in", got)
	}
}

// TestSalesFormFilterPaymentFormUnchanged is the discriminating control: the fix
// picks the table from the form's mode, so a payment-mode form must still read
// `sales` — and a response row must never be emitted for it.
func TestSalesFormFilterPaymentFormUnchanged(t *testing.T) {
	ctx := context.Background()
	h, pool, accountID, formID := salesRowFixture(t, "payment")
	raw := bundleAPIKey(t, ctx, pool, accountID)

	sales := dbstore.NewSaleStore(pool)
	if err := sales.Create(ctx, &dbstore.Sale{
		AccountID: accountID, FormID: formID, RefCode: "IT-" + accountID[:8],
		Client:   dbstore.SaleClient{FullName: "Buyer One", Email: "buyer@example.test"},
		Items:    []dbstore.SaleItem{{ProductID: "it-p1", Name: "Tee", PriceSen: 1000, Qty: 1}},
		TotalSen: 1000, SaleType: dbstore.SaleTypeOneTime,
		PaymentMethod: "chip", Status: dbstore.SaleConfirmed,
	}); err != nil {
		t.Fatalf("create sale: %v", err)
	}

	code, body := salesGet(t, h, raw, "/v1/rest/sales?page=1&limit=15&form_id="+formID)
	if code != http.StatusOK {
		t.Fatalf("sales status = %d, want 200 (body %s)", code, body)
	}
	var page struct {
		Items []struct {
			RefCode  string `json:"ref_code"`
			SaleType string `json:"sale_type"`
		} `json:"items"`
		Total int64 `json:"total"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("decode sales page: %v (%s)", err, body)
	}
	if page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("payment form total = %d items = %d, want 1/1 — body %s", page.Total, len(page.Items), body)
	}
	if page.Items[0].RefCode == "" {
		t.Errorf("payment form row lost its ref_code: %s", body)
	}
	// A payment form's rows keep sale_type as the API emits it (empty for a
	// one-time sale) — never the response marker. This is the assertion that
	// fails if the mode branch is applied to every form instead of the
	// response ones.
	if page.Items[0].SaleType == "response" {
		t.Errorf("payment form row was rendered as a response: %s", body)
	}
}

// TestFormAnalyticsCountsResponseSubmissions covers the second count surface the
// diagnosis flagged: the dashboard's per-form "Submissions" counter read
// `COUNT(s.id) ... FROM forms LEFT JOIN sales s`, so a response form reported 0
// while its submissions existed — making "nothing was recorded" look confirmed.
func TestFormAnalyticsCountsResponseSubmissions(t *testing.T) {
	ctx := context.Background()
	h, pool, accountID, formID := salesRowFixture(t, "response")

	subs := dbstore.NewSubmissionStore(pool)
	for i := 0; i < 3; i++ {
		if err := subs.Create(ctx, &dbstore.Submission{
			FormID: formID, AccountID: accountID,
			Data: map[string]any{"b3": "answer"},
		}); err != nil {
			t.Fatalf("seed submission: %v", err)
		}
	}

	stats, err := h.Sales.FormAnalyticsAccounts(ctx, []string{accountID})
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
		t.Fatalf("form analytics has no row for %s: %+v", formID, stats)
	}
	if found.Submissions != 3 {
		t.Errorf("analytics submissions = %d, want 3 (the counter must read submissions for a response form)",
			found.Submissions)
	}
}

// TestSalesExportFollowsFormMode covers the export button, which lives on the
// same page as the list: exporting a response form used to run the SALES query
// and emit a header-only file while the page above it listed entries.
func TestSalesExportFollowsFormMode(t *testing.T) {
	ctx := context.Background()
	h, pool, accountID, formID := salesRowFixture(t, "response")

	subs := dbstore.NewSubmissionStore(pool)
	if err := subs.Create(ctx, &dbstore.Submission{
		FormID: formID, AccountID: accountID,
		Data: map[string]any{"b3": "=HYPERLINK(\"http://evil\",\"x\")", "n1": 3, "ok": true},
	}); err != nil {
		t.Fatalf("seed submission: %v", err)
	}

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	raw := bundleAPIKey(t, ctx, pool, accountID)
	req := httptest.NewRequest(http.MethodGet, "/v1/rest/sales/export.csv?form_id="+formID, nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("export status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// The old (sales-only) export produced this header — it must not appear.
	if strings.Contains(body, "ref_code,status,payment_method") {
		t.Errorf("response export used the SALES columns (header-only file): %s", body)
	}
	if !strings.Contains(body, "submission_id,created_at") {
		t.Errorf("response export did not emit the response columns: %s", body)
	}
	rows := strings.Split(strings.TrimSpace(body), "\n")
	if len(rows) != 2 {
		t.Fatalf("export rows = %d, want a header + exactly 1 entry: %s", len(rows), body)
	}
	if !strings.Contains(rows[1], "b3") && !strings.Contains(body, "n1") {
		t.Errorf("export dropped the recorded answer keys: %s", body)
	}
	// The answer values are present, and csvguard neutralises the formula lead
	// (a response's answers are buyer-controlled text too).
	if !strings.Contains(body, "'=HYPERLINK") {
		t.Errorf("export did not neutralise a formula-lead answer: %s", body)
	}
	// A number must not gain a trailing ".0"; a bool renders as true/false.
	if !strings.Contains(body, ",3,") {
		t.Errorf("numeric answer not rendered as 3: %s", body)
	}
	if !strings.Contains(body, "true") {
		t.Errorf("boolean answer not rendered as true: %s", body)
	}
}

// TestSalesExportPaymentFormUnchanged is the control for the export branch: a
// payment form's export must keep its sales columns and content.
func TestSalesExportPaymentFormUnchanged(t *testing.T) {
	ctx := context.Background()
	h, pool, accountID, formID := salesRowFixture(t, "payment")
	sales := dbstore.NewSaleStore(pool)
	if err := sales.Create(ctx, &dbstore.Sale{
		AccountID: accountID, FormID: formID, RefCode: "IT-EXP-" + accountID[:8],
		Client:   dbstore.SaleClient{FullName: "Buyer One", Email: "buyer@example.test"},
		Items:    []dbstore.SaleItem{{ProductID: "it-p1", Name: "Tee", PriceSen: 2500, Qty: 1}},
		TotalSen: 2500, SaleType: dbstore.SaleTypeOneTime,
		PaymentMethod: "chip", Status: dbstore.SaleConfirmed,
	}); err != nil {
		t.Fatalf("create sale: %v", err)
	}

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	raw := bundleAPIKey(t, ctx, pool, accountID)
	req := httptest.NewRequest(http.MethodGet, "/v1/rest/sales/export.csv?form_id="+formID, nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("export status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "ref_code,status,payment_method") {
		t.Errorf("payment export lost its sales columns: %s", body)
	}
	if !strings.Contains(body, "IT-EXP-") {
		t.Errorf("payment export lost the sale: %s", body)
	}
	if strings.Contains(body, "submission_id,created_at") {
		t.Errorf("payment export used the RESPONSE columns: %s", body)
	}
}
