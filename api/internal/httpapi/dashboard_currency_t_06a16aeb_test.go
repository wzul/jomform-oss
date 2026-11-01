package httpapi

// Card t_06a16aeb — the dashboard's revenue figures, its 14-day chart and its
// recent-sales rows printed a literal "RM", and the CSV export labelled the
// money columns `total_rm` / `refunded_rm` / `outstanding_rm`.
//
// THE DEFECT: `dashboard-client.tsx` defined its own `money()` as
// `RM${(sen/100).toLocaleString(...)}` and used it for the revenue stat, the
// chart tooltip and axis, the per-form and per-product revenue, and the recent
// rows — so a workspace whose sales were recorded in USD (Settings → Currency,
// issue #185) showed "RM24.90" for a US$24.90 sale. The export had the mirror
// problem: the values are in each sale's own currency, so a `_rm` header was a
// wrong label and a mixed-currency file gave a reader no way to tell two 24.90
// values apart.
//
// WHAT THESE GUARDS COVER
//   1. End to end through the real bundle endpoint: a USD sale and a MYR sale on
//      one form produce `revenue_by_currency` {USD: 2490, MYR: 1500} on the
//      summary, the form's analytics row and the product's analytics row, while
//      the legacy scalar (`revenue_sen`) stays the sum it always was. A form
//      whose only sale is an unpaid USD one still names USD (value 0), so a zero
//      is never labelled RM.
//   2. The recent-sales rows in the bundle carry each sale's own currency.
//   3. The exported CSV has a `currency` column and no `_rm` header, and its
//      money cell is the sale's amount under that currency (24.90 + USD).
//
// WHAT IT DOES NOT COVER: the rendered pixels (web/src/lib/dashboardMoney.test.ts
// covers the formatter wiring; the browser check in the handoff covers paint).

import (
	"context"
	"encoding/csv"
	"net/http"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// currencySale is one row the fixtures below create.
type currencySale struct {
	ref     string
	amount  int64
	status  string
	item    bool
	product string
}

// seedCurrencySale creates a sale on the form, in the given currency, and
// returns it. `item` adds a line so the sale shows up in product analytics.
func seedCurrencySale(t *testing.T, h *RESTHandlers, accountID, formID, currency string, s currencySale) *dbstore.Sale {
	t.Helper()
	sale := &dbstore.Sale{
		AccountID: accountID, FormID: formID, RefCode: s.ref,
		Client: dbstore.SaleClient{FullName: "Currency buyer " + s.ref, Email: s.ref + "@example.test"},
		// sales.items is NOT NULL: an empty array, never a nil slice.
		Items:       []dbstore.SaleItem{},
		SubtotalSen: s.amount, TotalSen: s.amount, Currency: currency,
		SaleType: dbstore.SaleTypeOneTime, PaymentMethod: "chip", Status: s.status,
	}
	if s.item {
		sale.Items = []dbstore.SaleItem{{
			ProductID: s.product, Name: "Currency widget", PriceSen: s.amount, Qty: 1,
		}}
	}
	if err := h.Sales.Create(context.Background(), sale); err != nil {
		t.Fatalf("create sale %s: %v", s.ref, err)
	}
	return sale
}

// TestDashboardBundleReportsRevenuePerCurrency is the card's defect as the
// dashboard sees it: the three money panels must be able to label the sale's own
// currency instead of every figure wearing "RM".
func TestDashboardBundleReportsRevenuePerCurrency(t *testing.T) {
	ctx := context.Background()
	h, pool, accountID, formID := salesRowFixture(t, "payment")
	raw := bundleAPIKey(t, ctx, pool, accountID)
	suffix := accountID[:8]
	const product = "prod-06a16aeb"

	// Paid money on one form, in two currencies, plus a sale created with a BLANK
	// currency code: the write path stores the MYR fallback (the code the row was
	// charged in), so it must land in the MYR bucket, not in an empty label.
	seedCurrencySale(t, h, accountID, formID, "USD", currencySale{
		ref: "JF-USD-" + suffix, amount: 2490, status: dbstore.SalePaid, item: true, product: product,
	})
	seedCurrencySale(t, h, accountID, formID, "MYR", currencySale{
		ref: "JF-MYR-" + suffix, amount: 1000, status: dbstore.SaleConfirmed, item: true, product: product,
	})
	seedCurrencySale(t, h, accountID, formID, "", currencySale{
		ref: "JF-OLD-" + suffix, amount: 500, status: dbstore.SalePaid, item: true, product: product,
	})
	// An unpaid USD sale: no revenue, but the form still has USD money behind it.
	seedCurrencySale(t, h, accountID, formID, "USD", currencySale{
		ref: "JF-USDFAIL-" + suffix, amount: 300, status: dbstore.SaleFailed,
	})

	// A second form whose ONLY sale is unpaid USD: its map must name USD at 0 so
	// a zero revenue is not labelled RM on a USD workspace.
	failedOnly := &dbstore.Form{
		AccountID: accountID, Slug: "it-usd-zero-" + suffix, Title: "USD zero form",
		Status: "published", Config: map[string]any{"mode": "payment"}, Theme: map[string]any{},
	}
	if err := h.Forms.Create(ctx, failedOnly); err != nil {
		t.Fatalf("create failed-only form: %v", err)
	}
	seedCurrencySale(t, h, accountID, failedOnly.ID, "USD", currencySale{
		ref: "JF-USDFAIL2-" + suffix, amount: 700, status: dbstore.SaleFailed,
	})

	code, body := bundleGet(t, h, raw, "/v1/rest/dashboard-bundle")
	if code != http.StatusOK {
		t.Fatalf("bundle status = %d, want 200", code)
	}

	// ── summary ────────────────────────────────────────────────────────────
	summary, _ := body["summary"].(map[string]any)
	if summary == nil {
		t.Fatalf("bundle carried no summary: %v", body)
	}
	got := currencyMap(t, summary["revenue_by_currency"], "summary.revenue_by_currency")
	assertCurrency(t, got, "USD", 2490)
	assertCurrency(t, got, "MYR", 1500)
	if len(got) != 2 {
		t.Errorf("summary.revenue_by_currency = %v, want exactly USD and MYR — a stray currency splits one amount into two labels", got)
	}
	// The scalar stays additive for existing consumers.
	if got := summary["revenue_sen"]; got != float64(3990) {
		t.Errorf("summary.revenue_sen = %v, want 3990 (2490 + 1000 + 500) — the scalar must not change shape", got)
	}

	// ── per-form ───────────────────────────────────────────────────────────
	formRows := analyticsRows(t, body, "form_analytics")
	row := formRowByID(t, formRows, formID)
	got = currencyMap(t, row["revenue_by_currency"], "form_analytics.revenue_by_currency")
	assertCurrency(t, got, "USD", 2490)
	assertCurrency(t, got, "MYR", 1500)
	if got := row["revenue_sen"]; got != float64(3990) {
		t.Errorf("form revenue_sen = %v, want 3990", got)
	}
	zeroRow := formRowByID(t, formRows, failedOnly.ID)
	got = currencyMap(t, zeroRow["revenue_by_currency"], "form_analytics(failed-only).revenue_by_currency")
	assertCurrency(t, got, "USD", 0)
	if format := got["MYR"]; format != nil {
		t.Errorf("a USD-only form reported MYR money (%v) — the label would fall back to RM", format)
	}

	// ── per-product ────────────────────────────────────────────────────────
	prodRows := analyticsRows(t, body, "product_analytics")
	var prod map[string]any
	for _, p := range prodRows {
		if p["product_id"] == product {
			prod = p
		}
	}
	if prod == nil {
		t.Fatalf("product_analytics has no row for %s: %v", product, prodRows)
	}
	got = currencyMap(t, prod["paid_revenue_by_currency"], "product_analytics.paid_revenue_by_currency")
	assertCurrency(t, got, "USD", 2490)
	assertCurrency(t, got, "MYR", 1500)
	got = currencyMap(t, prod["revenue_by_currency"], "product_analytics.revenue_by_currency")
	assertCurrency(t, got, "USD", 2490)
	assertCurrency(t, got, "MYR", 1500)
	if got := prod["paid_revenue_sen"]; got != float64(3990) {
		t.Errorf("product paid_revenue_sen = %v, want 3990", got)
	}

	// ── recent-sales rows ──────────────────────────────────────────────────
	salesSection, _ := body["sales"].(map[string]any)
	items, _ := salesSection["items"].([]any)
	if len(items) == 0 {
		t.Fatalf("bundle carried no recent rows: %v", salesSection)
	}
	seen := map[string]bool{}
	for _, it := range items {
		r, _ := it.(map[string]any)
		ref, _ := r["ref_code"].(string)
		if !strings.HasPrefix(ref, "JF-") || !strings.HasSuffix(ref, suffix) {
			continue
		}
		cur, _ := r["currency"].(string)
		if cur == "" {
			t.Errorf("recent row %s carries no currency — the dashboard can only print a hardcoded symbol", ref)
			continue
		}
		seen[cur] = true
	}
	if !seen["USD"] || !seen["MYR"] {
		t.Errorf("recent rows carried currencies %v, want both USD and MYR — the page must label each row on its own", seen)
	}
}

// TestCSVExportNamesTheSalesOwnCurrency is the export half: a `_rm` header over
// a USD amount states the wrong money, and the file needs a currency column to
// be readable at all when a workspace's history spans currencies.
func TestCSVExportNamesTheSalesOwnCurrency(t *testing.T) {
	ctx := context.Background()
	h, pool, accountID, formID := salesRowFixture(t, "payment")
	raw := bundleAPIKey(t, ctx, pool, accountID)
	suffix := accountID[:8]

	usdRef := "JF-USDCSV-" + suffix
	legacyRef := "JF-OLDCSV-" + suffix
	seedCurrencySale(t, h, accountID, formID, "USD", currencySale{
		ref: usdRef, amount: 2490, status: dbstore.SalePaid,
	})
	seedCurrencySale(t, h, accountID, formID, "", currencySale{
		ref: legacyRef, amount: 500, status: dbstore.SalePaid,
	})

	code, body := salesGet(t, h, raw, "/v1/rest/sales/export.csv")
	if code != http.StatusOK {
		t.Fatalf("export status = %d, want 200 (body %s)", code, body)
	}
	rows, err := csv.NewReader(strings.NewReader(string(body))).ReadAll()
	if err != nil {
		t.Fatalf("export is not valid CSV: %v (%s)", err, body)
	}
	if len(rows) < 2 {
		t.Fatalf("export has no data rows: %s", body)
	}
	header := map[string]int{}
	for i, name := range rows[0] {
		header[name] = i
	}
	for _, gone := range []string{"total_rm", "refunded_rm", "outstanding_rm"} {
		if _, ok := header[gone]; ok {
			t.Errorf("export still heads a money column %q — the values are in the sale's own currency, so _rm is a wrong label", gone)
		}
	}
	for _, want := range []string{"currency", "total", "refunded", "outstanding"} {
		if _, ok := header[want]; !ok {
			t.Fatalf("export has no %q column: %v", want, rows[0])
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
	usd := find(usdRef)
	if usd == nil {
		t.Fatalf("%s not found in the export:\n%s", usdRef, body)
	}
	if got := usd[header["currency"]]; got != "USD" {
		t.Errorf("USD sale exported currency = %q, want USD", got)
	}
	if got := usd[header["total"]]; got != "24.90" {
		t.Errorf("USD sale exported total = %q, want 24.90 (major units, the sale's own currency)", got)
	}
	legacy := find(legacyRef)
	if legacy == nil {
		t.Fatalf("%s not found in the export:\n%s", legacyRef, body)
	}
	if got := legacy[header["currency"]]; got != "MYR" {
		t.Errorf("a sale created with a blank currency exported currency = %q, want MYR (the write path's fallback, the money it was charged in)", got)
	}
}

// ── helpers ────────────────────────────────────────────────────────────────

func currencyMap(t *testing.T, v any, what string) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("%s is %T (%v), want a currency→minor-units object", what, v, v)
	}
	return m
}

func assertCurrency(t *testing.T, m map[string]any, code string, want float64) {
	t.Helper()
	got, ok := m[code]
	if !ok {
		t.Errorf("no %s entry in %v", code, m)
		return
	}
	if got != want {
		t.Errorf("%s = %v, want %v", code, got, want)
	}
}

func analyticsRows(t *testing.T, body map[string]any, key string) []map[string]any {
	t.Helper()
	raw, ok := body[key].([]any)
	if !ok {
		t.Fatalf("bundle has no %s list: %T", key, body[key])
	}
	out := make([]map[string]any, 0, len(raw))
	for _, r := range raw {
		if row, ok := r.(map[string]any); ok {
			out = append(out, row)
		}
	}
	return out
}

func formRowByID(t *testing.T, rows []map[string]any, formID string) map[string]any {
	t.Helper()
	for _, r := range rows {
		if r["form_id"] == formID {
			return r
		}
	}
	t.Fatalf("form_analytics has no row for %s: %v", formID, rows)
	return nil
}
