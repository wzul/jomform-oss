package mcpsrv

// Card t_8064a965 — MCP parity for the fee-in-revenue rule (AGENTS.md "MCP
// FEATURE PARITY"). The dashboard now reports what the merchant KEEPS; an agent
// reading the same account through get_sales_summary / export_sales_csv must not
// be told the buyer-paid total is income.
//
// Same rule and same names as the REST summary and the dashboard:
//
//	revenue_sen      what buyers paid, net of refunds (unchanged meaning)
//	fees_sen         the buyer-borne processing fee inside it
//	net_revenue_sen  revenue_sen - fees_sen — what the merchant keeps
//	CSV: total / fee / net, net = total - fee

import (
	"context"
	"encoding/csv"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

func TestMCPReportsWhatTheMerchantKeeps(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)

	var userID, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('feesum-' || md5(random()::text) || '@example.test', 'it') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 SELECT $1, 'feesum-' || md5(random()::text), 'feesum-' || md5(random()::text) RETURNING id`,
		userID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	forms := dbstore.NewFormStore(pool)
	form := &dbstore.Form{
		AccountID: accountID, Slug: "feesum-" + accountID[:8], Title: "Fee form",
		Status: "published", Config: map[string]any{"mode": "payment"}, Theme: map[string]any{},
	}
	if err := forms.Create(ctx, form); err != nil {
		t.Fatalf("create form: %v", err)
	}

	sales := dbstore.NewSaleStore(pool)
	feeSale := &dbstore.Sale{
		AccountID: accountID, FormID: form.ID, RefCode: "JF-FEE0001",
		Client:      dbstore.SaleClient{FullName: "Fee buyer", Email: "fee@example.test"},
		Items:       []dbstore.SaleItem{{ProductID: "p-fee", Name: "Fee item", PriceSen: 100000, Qty: 1}},
		SubtotalSen: 100000, FeeSen: 1010, TotalSen: 101010, Currency: "MYR",
		SaleType: dbstore.SaleTypeOneTime, PaymentMethod: "chip", Status: dbstore.SalePaid,
	}
	plainSale := &dbstore.Sale{
		AccountID: accountID, FormID: form.ID, RefCode: "JF-PLAIN01",
		Client:      dbstore.SaleClient{FullName: "Plain buyer", Email: "plain@example.test"},
		Items:       []dbstore.SaleItem{},
		SubtotalSen: 5051, TotalSen: 5051, Currency: "MYR",
		SaleType: dbstore.SaleTypeOneTime, PaymentMethod: "chip", Status: dbstore.SaleConfirmed,
	}
	for _, s := range []*dbstore.Sale{feeSale, plainSale} {
		if err := sales.Create(ctx, s); err != nil {
			t.Fatalf("create sale %s: %v", s.RefCode, err)
		}
	}

	srv := New(&Deps{
		Accounts: dbstore.NewAccountStore(pool),
		Sales:    sales,
		Forms:    forms,
	}, nil, nil, nil)
	callCtx := WithAuthIdentity(ctx, &AuthIdentity{AccountID: accountID, Scopes: []string{"readwrite"}})

	_, sum, err := srv.salesSummary(callCtx, nil, emptyIn{})
	if err != nil {
		t.Fatalf("get_sales_summary: %v", err)
	}
	if sum.RevenueSen != 106061 {
		t.Errorf("revenue_sen = %d, want 106061 (what buyers paid)", sum.RevenueSen)
	}
	if sum.FeesSen != 1010 {
		t.Errorf("fees_sen = %d, want 1010", sum.FeesSen)
	}
	if sum.NetRevenueSen != 105051 {
		t.Errorf("net_revenue_sen = %d, want 105051 — an agent quoting revenue_sen as income overstates it by the fee", sum.NetRevenueSen)
	}

	_, out, err := srv.exportSalesCSV(callCtx, nil, exportSalesIn{})
	if err != nil {
		t.Fatalf("export_sales_csv: %v", err)
	}
	rows, err := csv.NewReader(strings.NewReader(out["csv"])).ReadAll()
	if err != nil {
		t.Fatalf("export is not valid CSV: %v (%s)", err, out["csv"])
	}
	if len(rows) < 3 {
		t.Fatalf("export has %d rows, want header + 2 sales:\n%s", len(rows), out["csv"])
	}
	header := map[string]int{}
	for i, name := range rows[0] {
		header[name] = i
	}
	for _, want := range []string{"fee", "net"} {
		if _, ok := header[want]; !ok {
			t.Fatalf("MCP export has no %q column: %v", want, rows[0])
		}
	}
	var feeRow []string
	for _, r := range rows[1:] {
		if r[header["ref_code"]] == "JF-FEE0001" {
			feeRow = r
		}
	}
	if feeRow == nil {
		t.Fatalf("JF-FEE0001 missing from the MCP export:\n%s", out["csv"])
	}
	for col, want := range map[string]string{"total": "1010.10", "fee": "10.10", "net": "1000.00"} {
		if got := feeRow[header[col]]; got != want {
			t.Errorf("MCP export %s = %q, want %q", col, got, want)
		}
	}
}
