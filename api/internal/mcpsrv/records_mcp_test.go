package mcpsrv

// MCP parity for the sales ⇄ submission record set.
//
// list_sales and export_sales_csv each carried their OWN copy of the "is this a
// response form?" rule, keyed on a single form id — so an agent asking for "all
// sales" never saw a single response entry, while the dashboard listed them.
// Both now call the same dbstore.ResolveRecordScope the REST list uses.
//
// These tests exist because a mutation that restored the sales-only behaviour in
// list_sales left the suite GREEN (mutation M10 in the implementer's harness):
// the tool had no test that covered the unfiltered case, which is precisely the
// case that was broken.
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL).

import (
	"context"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// recordSetMCPFixture seeds an account with a payment form (one sale) and a
// response form (three submissions) and returns a Server scoped to it.
func recordSetMCPFixture(t *testing.T) (*Server, context.Context, string, string, string) {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Open(t)

	var userID, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('rec-' || md5(random()::text) || '@example.test', 'it') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 SELECT $1, 'rec-acc-' || md5(random()::text), 'rec-' || md5(random()::text)
		 RETURNING id`, userID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	forms := dbstore.NewFormStore(pool)
	payment := &dbstore.Form{
		AccountID: accountID, Slug: "rec-pay-" + accountID[:8], Title: "Payment form",
		Status: "published", Config: map[string]any{"mode": "payment"}, Theme: map[string]any{},
	}
	if err := forms.Create(ctx, payment); err != nil {
		t.Fatalf("create payment form: %v", err)
	}
	response := &dbstore.Form{
		AccountID: accountID, Slug: "rec-resp-" + accountID[:8], Title: "Response form",
		Status: "published", Config: map[string]any{"mode": "response"}, Theme: map[string]any{},
	}
	if err := forms.Create(ctx, response); err != nil {
		t.Fatalf("create response form: %v", err)
	}

	sales := dbstore.NewSaleStore(pool)
	if err := sales.Create(ctx, &dbstore.Sale{
		AccountID: accountID, FormID: payment.ID, RefCode: "REC-" + accountID[:8],
		Client:   dbstore.SaleClient{FullName: "Rec Buyer", Email: "rec@example.test"},
		Items:    []dbstore.SaleItem{{ProductID: "p", Name: "Tee", PriceSen: 500, Qty: 1}},
		TotalSen: 500, SaleType: dbstore.SaleTypeOneTime,
		PaymentMethod: "chip", Status: dbstore.SalePaid,
	}); err != nil {
		t.Fatalf("create sale: %v", err)
	}
	subs := dbstore.NewSubmissionStore(pool)
	for i := 0; i < 3; i++ {
		if err := subs.Create(ctx, &dbstore.Submission{
			FormID: response.ID, AccountID: accountID,
			Data: map[string]any{"note": "n", "full_name": "Responder"},
		}); err != nil {
			t.Fatalf("create submission %d: %v", i, err)
		}
	}

	deps := &Deps{
		Accounts:    dbstore.NewAccountStore(pool),
		Sales:       sales,
		Forms:       forms,
		Submissions: subs,
	}
	srv := New(deps, nil, nil, nil)
	callCtx := WithAuthIdentity(ctx, &AuthIdentity{AccountID: accountID, Scopes: []string{"readwrite"}})
	return srv, callCtx, accountID, payment.ID, response.ID
}

// TestMCPListSalesUnfilteredIncludesResponses is the reported defect as an agent
// sees it: without form_id the tool must return the account's whole record set.
// It fails on the pre-fix code, where every row had a payment type.
func TestMCPListSalesUnfilteredIncludesResponses(t *testing.T) {
	srv, ctx, _, _, _ := recordSetMCPFixture(t)

	_, out, err := srv.listSales(ctx, nil, listSalesIn{})
	if err != nil {
		t.Fatalf("list_sales: %v", err)
	}
	if out == nil {
		t.Fatal("list_sales returned no output")
	}
	var sales, responses int
	for _, row := range out.Items {
		if row.SaleType == "response" || row.SubmissionID != "" {
			responses++
			if row.SubmissionID == "" {
				t.Errorf("a response row carries no submission_id: %+v", row)
			}
			if row.Data == nil {
				t.Errorf("a response row carries no recorded answers: %+v", row)
			}
			continue
		}
		sales++
	}
	if sales != 1 || responses != 3 {
		t.Errorf("list_sales returned %d sales / %d responses, want 1/3 — the tool is hiding the "+
			"responses again (rows: %+v)", sales, responses, out.Items)
	}
}

// The form filter keeps working: a response form id returns its submissions, a
// payment form id returns its sale.
func TestMCPListSalesFormFilterStillDecidesTheTable(t *testing.T) {
	srv, ctx, _, paymentFormID, responseFormID := recordSetMCPFixture(t)

	_, resp, err := srv.listSales(ctx, nil, listSalesIn{FormID: responseFormID})
	if err != nil {
		t.Fatalf("list_sales(response form): %v", err)
	}
	if len(resp.Items) != 3 {
		t.Fatalf("response-form list_sales returned %d rows, want 3", len(resp.Items))
	}
	for _, row := range resp.Items {
		if row.SaleType != "response" {
			t.Errorf("response-form list_sales leaked a sale row: %+v", row)
		}
	}

	_, pay, err := srv.listSales(ctx, nil, listSalesIn{FormID: paymentFormID})
	if err != nil {
		t.Fatalf("list_sales(payment form): %v", err)
	}
	if len(pay.Items) != 1 {
		t.Fatalf("payment-form list_sales returned %d rows, want 1", len(pay.Items))
	}
	if pay.Items[0].SaleType == "response" {
		t.Errorf("payment-form list_sales returned a response row: %+v", pay.Items[0])
	}
}

// A status filter is a payment-status question: on the merged read it must
// EXCLUDE the responses rather than report rows a status they cannot hold.
func TestMCPListSalesStatusFilterExcludesResponses(t *testing.T) {
	srv, ctx, _, _, _ := recordSetMCPFixture(t)

	_, paid, err := srv.listSales(ctx, nil, listSalesIn{Status: dbstore.SalePaid})
	if err != nil {
		t.Fatalf("list_sales(status=paid): %v", err)
	}
	if len(paid.Items) != 1 {
		t.Fatalf("status=paid returned %d rows, want just the paid sale", len(paid.Items))
	}
	if paid.Items[0].SaleType == "response" {
		t.Errorf("status=paid returned a response row: %+v", paid.Items[0])
	}

	_, confirmed, err := srv.listSales(ctx, nil, listSalesIn{Status: dbstore.SaleConfirmed})
	if err != nil {
		t.Fatalf("list_sales(status=confirmed): %v", err)
	}
	if len(confirmed.Items) != 0 {
		t.Errorf("status=confirmed returned %d rows, want 0 (no sale is confirmed)", len(confirmed.Items))
	}
}

// export_sales_csv must mirror the list: the same record set, and it must honour
// the filters (it used to accept only form_id, so an agent could not export the
// set its own list call had just returned).
func TestMCPExportSalesCSVMirrorsTheList(t *testing.T) {
	srv, ctx, _, _, _ := recordSetMCPFixture(t)

	_, all, err := srv.exportSalesCSV(ctx, nil, exportSalesIn{})
	if err != nil {
		t.Fatalf("export_sales_csv: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(all["csv"]), "\n")
	if len(lines)-1 != 4 {
		t.Fatalf("export has %d data rows, want 4 (1 sale + 3 responses):\n%s", len(lines)-1, all["csv"])
	}
	if !strings.Contains(lines[0], "record_type") {
		t.Errorf("a combined export must name the record kind, header was: %s", lines[0])
	}
	if all["count"] != "4" {
		t.Errorf("export count = %s, want 4", all["count"])
	}
	var saleRows, responseRows int
	for _, line := range lines[1:] {
		switch {
		case strings.HasPrefix(line, dbstore.RecordKindSale+","):
			saleRows++
		case strings.HasPrefix(line, dbstore.RecordKindSubmission+","):
			responseRows++
		}
	}
	if saleRows != 1 || responseRows != 3 {
		t.Errorf("export held %d sale rows / %d response rows, want 1/3:\n%s",
			saleRows, responseRows, all["csv"])
	}

	// The status filter must reach the export, so the file matches a filtered
	// list call (the tool used to ignore every filter but form_id).
	_, filtered, err := srv.exportSalesCSV(ctx, nil, exportSalesIn{Status: dbstore.SalePaid})
	if err != nil {
		t.Fatalf("export_sales_csv(status=paid): %v", err)
	}
	if filtered["count"] != "1" {
		t.Errorf("status=paid export count = %s, want 1 — the export is ignoring the filter (csv %q)",
			filtered["count"], filtered["csv"])
	}
}

// A payment-only account keeps the historical export columns (no record_type),
// so an agent parsing that file is not broken by the merged shape.
func TestMCPExportSalesCSVPaymentOnlyColumnsUnchanged(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	var userID, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('recpo-' || md5(random()::text) || '@example.test', 'it') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 SELECT $1, 'recpo-' || md5(random()::text), 'recpo-' || md5(random()::text) RETURNING id`,
		userID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	forms := dbstore.NewFormStore(pool)
	f := &dbstore.Form{
		AccountID: accountID, Slug: "recpo-" + accountID[:8], Title: "Pay",
		Status: "published", Config: map[string]any{"mode": "payment"}, Theme: map[string]any{},
	}
	if err := forms.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}
	sales := dbstore.NewSaleStore(pool)
	if err := sales.Create(ctx, &dbstore.Sale{
		AccountID: accountID, FormID: f.ID, RefCode: "RECPO-" + accountID[:8],
		Client:   dbstore.SaleClient{FullName: "Solo", Email: "solo@example.test"},
		Items:    []dbstore.SaleItem{{ProductID: "p", Name: "Tee", PriceSen: 500, Qty: 1}},
		TotalSen: 500, SaleType: dbstore.SaleTypeOneTime,
		PaymentMethod: "chip", Status: dbstore.SalePaid,
	}); err != nil {
		t.Fatalf("create sale: %v", err)
	}
	srv := New(&Deps{
		Accounts:    dbstore.NewAccountStore(pool),
		Sales:       sales,
		Forms:       forms,
		Submissions: dbstore.NewSubmissionStore(pool),
	}, nil, nil, nil)
	callCtx := WithAuthIdentity(ctx, &AuthIdentity{AccountID: accountID, Scopes: []string{"readwrite"}})

	_, out, err := srv.exportSalesCSV(callCtx, nil, exportSalesIn{})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if strings.Contains(out["csv"], "record_type") {
		t.Errorf("a payment-only export gained the merged-list columns:\n%s", out["csv"])
	}
	if !strings.Contains(out["csv"], "ref_code,status,payment_method") {
		t.Errorf("a payment-only export lost its sales columns:\n%s", out["csv"])
	}
}
