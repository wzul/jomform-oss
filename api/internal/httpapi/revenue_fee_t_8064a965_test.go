package httpapi

// Card t_8064a965 — the buyer-borne processing fee is inside sale.total_sen, so
// every aggregate that summed the total reported money the merchant never
// receives as revenue:
//
//	GET /v1/rest/summary   revenue_sen = SUM(total_sen - refunded_sen)
//	                       → measured 106061 on this fixture, 105051 earned
//	the dashboard bundle   same rule for the revenue stat, the 14-day chart and
//	                       the per-form panel
//	the CSV export         no fee or net column at all
//
// THE RULE (one rule for every surface, stated on /docs/payment-fees#reporting):
//
//	collected  revenue_sen        = what buyers paid, net of refunds
//	fee        fees_sen           = the buyer-borne processing fee inside it
//	kept       net_revenue_sen    = revenue_sen - fees_sen
//
// revenue_sen keeps its old meaning so a consumer that already reads it is not
// silently redefined; the surfaces a merchant reads as INCOME (the dashboard
// stat and chart, the per-form panel, the CSV `net`) use the kept figure.
//
// Product revenue is item prices (price_sen * qty) and never contained the fee,
// so it must NOT be reduced a second time — asserted here too, because the
// obvious-looking "fix" (subtract the fee from product revenue as well) would
// understate every product by the fee and still look green on the summary.

import (
	"context"
	"encoding/csv"
	"net/http"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// feeRefFor makes a sale reference unique to one test account: sales.ref_code is
// globally unique and every test in this package shares one database/schema.
func feeRefFor(base, accountID string) string { return base + "-" + accountID[:8] }

// seedFeeSale creates one sale with an explicit fee and line item.
func seedFeeSale(t *testing.T, h *RESTHandlers, accountID, formID, ref string, subtotal, fee int64, status, product string) {
	t.Helper()
	sale := &dbstore.Sale{
		AccountID: accountID, FormID: formID, RefCode: ref,
		Client:      dbstore.SaleClient{FullName: "Fee buyer " + ref, Email: ref + "@example.test"},
		Items:       []dbstore.SaleItem{{ProductID: product, Name: "Item " + product, PriceSen: subtotal, Qty: 1}},
		SubtotalSen: subtotal,
		FeeSen:      fee,
		TotalSen:    subtotal + fee,
		Currency:    "MYR",
		SaleType:    dbstore.SaleTypeOneTime, PaymentMethod: "chip", Status: status,
	}
	if err := h.Sales.Create(context.Background(), sale); err != nil {
		t.Fatalf("create sale %s: %v", ref, err)
	}
}

// num reads a JSON number out of a decoded map, failing when the field is
// missing (a missing field must not read as 0 and pass).
func num(t *testing.T, m map[string]any, key string) float64 {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Fatalf("summary has no %q field: %v", key, m)
	}
	f, ok := v.(float64)
	if !ok {
		t.Fatalf("summary %q = %#v, want a number", key, v)
	}
	return f
}

func TestSummaryReportsWhatTheMerchantKeeps(t *testing.T) {
	ctx := context.Background()
	h, pool, accountID, formID := salesRowFixture(t, "payment")
	raw := bundleAPIKey(t, ctx, pool, accountID)

	// The card's measured fixture: subtotal 100000 + fee 1010 = 101010 paid, and
	// a second, fee-free sale of 5051. sales.ref_code is globally unique, so the
	// references carry this account's suffix.
	feeRef, plainRef := feeRefFor("JF-FEE0001", accountID), feeRefFor("JF-PLAIN01", accountID)
	seedFeeSale(t, h, accountID, formID, feeRef, 100000, 1010, dbstore.SalePaid, "prod-fee-t8064a965")
	seedFeeSale(t, h, accountID, formID, plainRef, 5051, 0, dbstore.SaleConfirmed, "prod-plain-t8064a965")

	code, sum := bundleGet(t, h, raw, "/v1/rest/summary")
	if code != http.StatusOK {
		t.Fatalf("summary status = %d, want 200", code)
	}
	if got := num(t, sum, "revenue_sen"); got != 106061 {
		t.Errorf("revenue_sen = %v, want 106061 (what buyers paid)", got)
	}
	if got := num(t, sum, "fees_sen"); got != 1010 {
		t.Errorf("fees_sen = %v, want 1010 (the buyer-borne fee inside revenue_sen)", got)
	}
	if got := num(t, sum, "net_revenue_sen"); got != 105051 {
		t.Errorf("net_revenue_sen = %v, want 105051 (106061 collected - 1010 fee) — the fee is not income", got)
	}
	byCur, _ := sum["revenue_by_currency"].(map[string]any)
	if byCur == nil || byCur["MYR"] != float64(106061) {
		t.Errorf("revenue_by_currency = %v, want {MYR: 106061}", sum["revenue_by_currency"])
	}
	feesCur, _ := sum["fees_by_currency"].(map[string]any)
	if feesCur == nil || feesCur["MYR"] != float64(1010) {
		t.Errorf("fees_by_currency = %v, want {MYR: 1010}", sum["fees_by_currency"])
	}
	netCur, _ := sum["net_revenue_by_currency"].(map[string]any)
	if netCur == nil || netCur["MYR"] != float64(105051) {
		t.Errorf("net_revenue_by_currency = %v, want {MYR: 105051}", sum["net_revenue_by_currency"])
	}

	// The dashboard reads the bundle, and the bundle's summary is the same
	// function — assert the money reached it, not just that the two endpoints
	// agree with each other.
	code, bundle := bundleGet(t, h, raw, "/v1/rest/dashboard-bundle")
	if code != http.StatusOK {
		t.Fatalf("bundle status = %d, want 200", code)
	}
	bundleSum, _ := bundle["summary"].(map[string]any)
	if bundleSum == nil {
		t.Fatalf("bundle carried no summary: %v", bundle)
	}
	if got := num(t, bundleSum, "net_revenue_sen"); got != 105051 {
		t.Errorf("bundle summary net_revenue_sen = %v, want 105051", got)
	}
}

func TestFormAnalyticsReportsNetOfTheBuyerFee(t *testing.T) {
	ctx := context.Background()
	h, pool, accountID, formID := salesRowFixture(t, "payment")
	raw := bundleAPIKey(t, ctx, pool, accountID)

	feeRef, plainRef := feeRefFor("JF-FEE0001", accountID), feeRefFor("JF-PLAIN01", accountID)
	seedFeeSale(t, h, accountID, formID, feeRef, 100000, 1010, dbstore.SalePaid, "prod-fee-t8064a965")
	seedFeeSale(t, h, accountID, formID, plainRef, 5051, 0, dbstore.SaleConfirmed, "prod-plain-t8064a965")

	code, bundle := bundleGet(t, h, raw, "/v1/rest/dashboard-bundle")
	if code != http.StatusOK {
		t.Fatalf("bundle status = %d, want 200", code)
	}
	rows, _ := bundle["form_analytics"].([]any)
	if len(rows) == 0 {
		t.Fatalf("bundle carried no form_analytics: %v", bundle)
	}
	var form map[string]any
	for _, r := range rows {
		m, _ := r.(map[string]any)
		if m["form_id"] == formID {
			form = m
		}
	}
	if form == nil {
		t.Fatalf("form %s missing from form_analytics: %v", formID, rows)
	}
	if got := num(t, form, "revenue_sen"); got != 106061 {
		t.Errorf("form revenue_sen = %v, want 106061 (collected)", got)
	}
	if got := num(t, form, "fees_sen"); got != 1010 {
		t.Errorf("form fees_sen = %v, want 1010", got)
	}
	if got := num(t, form, "net_revenue_sen"); got != 105051 {
		t.Errorf("form net_revenue_sen = %v, want 105051 — the panel would otherwise count the fee as income", got)
	}
	formNet, _ := form["net_revenue_by_currency"].(map[string]any)
	if formNet == nil || formNet["MYR"] != float64(105051) {
		t.Errorf("form net_revenue_by_currency = %v, want {MYR: 105051}", form["net_revenue_by_currency"])
	}

	// Product revenue is item prices, so the fee was never in it. 100000 + 5051
	// must stay 105051; subtracting the fee here would report 104041.
	prods, _ := bundle["product_analytics"].([]any)
	paid := map[string]float64{}
	for _, r := range prods {
		m, _ := r.(map[string]any)
		if id, ok := m["product_id"].(string); ok {
			paid[id], _ = m["paid_revenue_sen"].(float64)
		}
	}
	if got := paid["prod-fee-t8064a965"]; got != 100000 {
		t.Errorf("product paid_revenue_sen = %v, want 100000 (item price; the fee is not part of it)", got)
	}
	if got := paid["prod-plain-t8064a965"]; got != 5051 {
		t.Errorf("fee-free product paid_revenue_sen = %v, want 5051", got)
	}
}

func TestCSVExportCarriesTheFeeAndTheNet(t *testing.T) {
	ctx := context.Background()
	h, pool, accountID, formID := salesRowFixture(t, "payment")
	raw := bundleAPIKey(t, ctx, pool, accountID)

	feeRef, plainRef := feeRefFor("JF-FEE0001", accountID), feeRefFor("JF-PLAIN01", accountID)
	seedFeeSale(t, h, accountID, formID, feeRef, 100000, 1010, dbstore.SalePaid, "prod-fee-t8064a965")
	seedFeeSale(t, h, accountID, formID, plainRef, 5051, 0, dbstore.SaleConfirmed, "prod-plain-t8064a965")

	code, body := salesGet(t, h, raw, "/v1/rest/sales/export.csv")
	if code != http.StatusOK {
		t.Fatalf("export status = %d, want 200 (body %s)", code, body)
	}
	rows, err := csv.NewReader(strings.NewReader(string(body))).ReadAll()
	if err != nil {
		t.Fatalf("export is not valid CSV: %v (%s)", err, body)
	}
	if len(rows) < 3 {
		t.Fatalf("export has %d rows, want header + 2 sales:\n%s", len(rows), body)
	}
	header := map[string]int{}
	for i, name := range rows[0] {
		header[name] = i
	}
	for _, want := range []string{"fee", "net"} {
		if _, ok := header[want]; !ok {
			t.Fatalf("export has no %q column — a merchant reconciling from this file counts the fee as income: %v", want, rows[0])
		}
	}
	find := func(ref string) []string {
		for _, r := range rows[1:] {
			if r[header["ref_code"]] == ref {
				return r
			}
		}
		return nil
	}
	fee := find(feeRef)
	if fee == nil {
		t.Fatalf("JF-FEE0001 missing from the export:\n%s", body)
	}
	for col, want := range map[string]string{"total": "1010.10", "fee": "10.10", "net": "1000.00"} {
		if got := fee[header[col]]; got != want {
			t.Errorf("fee sale %s = %q, want %q", col, got, want)
		}
	}
	plain := find(plainRef)
	if plain == nil {
		t.Fatalf("JF-PLAIN01 missing from the export:\n%s", body)
	}
	for col, want := range map[string]string{"total": "50.51", "fee": "0.00", "net": "50.51"} {
		if got := plain[header[col]]; got != want {
			t.Errorf("fee-free sale %s = %q, want %q", col, got, want)
		}
	}
}
