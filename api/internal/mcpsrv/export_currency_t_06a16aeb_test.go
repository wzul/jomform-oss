package mcpsrv

// Card t_06a16aeb — export_sales_csv carried the same wrong money labels as the
// REST export: `total_rm` / `refunded_rm` / `outstanding_rm` over values that
// are in each sale's own currency, and nothing that named the currency at all.
//
// The MCP export is a separate writer from rest_export.go (it assembles the CSV
// itself), so fixing the REST file alone would leave an agent receiving the old
// headers from the same product. This guards the tool an agent actually calls:
// the header has `currency` and no `_rm` name, the USD sale's row says USD, and
// a response row keeps the columns positionally aligned (it has no money, so its
// currency cell is empty — not shifted by one).

import (
	"context"
	"encoding/csv"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

func TestMCPExportCSVNamesTheSalesOwnCurrency(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)

	var userID, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('expcur-' || md5(random()::text) || '@example.test', 'it') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 SELECT $1, 'expcur-' || md5(random()::text), 'expcur-' || md5(random()::text) RETURNING id`,
		userID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	forms := dbstore.NewFormStore(pool)
	payment := &dbstore.Form{
		AccountID: accountID, Slug: "expcur-pay-" + accountID[:8], Title: "Pay",
		Status: "published", Config: map[string]any{"mode": "payment"}, Theme: map[string]any{},
	}
	if err := forms.Create(ctx, payment); err != nil {
		t.Fatalf("create payment form: %v", err)
	}
	response := &dbstore.Form{
		AccountID: accountID, Slug: "expcur-resp-" + accountID[:8], Title: "Resp",
		Status: "published", Config: map[string]any{"mode": "response"}, Theme: map[string]any{},
	}
	if err := forms.Create(ctx, response); err != nil {
		t.Fatalf("create response form: %v", err)
	}

	sales := dbstore.NewSaleStore(pool)
	if err := sales.Create(ctx, &dbstore.Sale{
		AccountID: accountID, FormID: payment.ID, RefCode: "EXPCUR-" + accountID[:8],
		Client:   dbstore.SaleClient{FullName: "US buyer", Email: "us@example.test"},
		Items:    []dbstore.SaleItem{{ProductID: "p", Name: "Tee", PriceSen: 2490, Qty: 1}},
		TotalSen: 2490, Currency: "USD", SaleType: dbstore.SaleTypeOneTime,
		PaymentMethod: "chip", Status: dbstore.SalePaid,
	}); err != nil {
		t.Fatalf("create sale: %v", err)
	}
	subs := dbstore.NewSubmissionStore(pool)
	if err := subs.Create(ctx, &dbstore.Submission{
		FormID: response.ID, AccountID: accountID,
		Data: map[string]any{"full_name": "Responder", "email": "r@example.test"},
	}); err != nil {
		t.Fatalf("create submission: %v", err)
	}

	srv := New(&Deps{
		Accounts:    dbstore.NewAccountStore(pool),
		Sales:       sales,
		Forms:       forms,
		Submissions: subs,
	}, nil, nil, nil)
	callCtx := WithAuthIdentity(ctx, &AuthIdentity{AccountID: accountID, Scopes: []string{"readwrite"}})

	_, out, err := srv.exportSalesCSV(callCtx, nil, exportSalesIn{})
	if err != nil {
		t.Fatalf("export_sales_csv: %v", err)
	}
	rows, err := csv.NewReader(strings.NewReader(out["csv"])).ReadAll()
	if err != nil {
		t.Fatalf("export is not valid CSV: %v (%s)", err, out["csv"])
	}
	if len(rows) < 3 {
		t.Fatalf("export has %d rows, want a header + a sale + a response:\n%s", len(rows), out["csv"])
	}
	header := map[string]int{}
	for i, name := range rows[0] {
		header[name] = i
	}
	for _, gone := range []string{"total_rm", "refunded_rm", "outstanding_rm"} {
		if _, ok := header[gone]; ok {
			t.Errorf("MCP export still heads a money column %q — its values are in the sale's own currency", gone)
		}
	}
	for _, want := range []string{"currency", "total", "refunded", "outstanding"} {
		if _, ok := header[want]; !ok {
			t.Fatalf("MCP export has no %q column: %v", want, rows[0])
		}
	}

	curIdx, totalIdx := header["currency"], header["total"]
	for _, row := range rows[1:] {
		switch row[header["record_type"]] {
		case dbstore.RecordKindSale:
			if got := row[curIdx]; got != "USD" {
				t.Errorf("sale row currency = %q, want USD", got)
			}
			if got := row[totalIdx]; got != "24.90" {
				t.Errorf("sale row total = %q, want 24.90", got)
			}
		case dbstore.RecordKindSubmission:
			// A response has no money and therefore no currency; the cell must be
			// EMPTY rather than shifted, or every later column is off by one.
			if got := row[curIdx]; got != "" {
				t.Errorf("response row currency = %q, want empty (positional alignment)", got)
			}
			if got := row[totalIdx]; got != "0.00" {
				t.Errorf("response row total = %q, want 0.00", got)
			}
		default:
			t.Errorf("unknown record_type %q in row %v", row[header["record_type"]], row)
		}
	}
}
