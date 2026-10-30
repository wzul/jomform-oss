package httpapi

// t_71c67471 — /sales hardcoded "RM" and the REST payload dropped
// sales.currency, so a non-MYR workspace's sales were mislabelled.
//
// THE DEFECT (measured on prod while verifying v1.22.21): `GET /api/rest/sales`
// rows carried total_sen/subtotal_sen/refunded_sen/outstanding_sen and no
// currency, while `order.go` records `sales.currency` from the owning
// workspace's currency and Settings → Currency offers USD/SGD/EUR… So a USD
// workspace's sale rendered "RM24.90" in the Sales page, its detail drawer and
// its refund/capture prompts: the number right, the money wrong. The v1.22.21
// copy promises "historical sales keep their original currency" — unobservable
// in the UI while the payload has no currency to show.
//
// WHAT THIS GUARD COVERS
//   1. toSaleRow emits the SALE's own currency (not the workspace's current
//      setting) and the JSON key is `currency`, asserted through the marshalled
//      payload rather than the struct field alone — the page reads JSON.
//   2. A row written before the currency column existed (empty) falls back to
//      MYR, which is what those rows were charged in; no row renders a blank
//      label.
//   3. End to end: a USD sale created in the database comes back from
//      GET /v1/rest/sales with currency "USD" and total_sen 2490 — the exact
//      row the Sales page must render as US$24.90.
//
// WHAT IT DOES NOT COVER: the rendered page (that is the web-side guard in
// web/src/lib/money.test.ts plus the browser check).

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

func TestSaleRowCarriesTheSalesOwnCurrency(t *testing.T) {
	row := toSaleRow(&dbstore.Sale{
		RefCode: "JF-usd-row", TotalSen: 2490, Currency: "USD",
	}, "")

	if row.Currency != "USD" {
		t.Fatalf("sale row currency = %q, want USD — the client would label US$24.90 as RM", row.Currency)
	}

	// The page reads the JSON key, not the Go field: assert the wire shape.
	body, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshal sale row: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(body, &wire); err != nil {
		t.Fatalf("decode sale row: %v", err)
	}
	if got := wire["currency"]; got != "USD" {
		t.Fatalf("JSON currency = %v, want \"USD\" (payload: %s)", got, body)
	}
	// The money fields stay integer minor units — currency labels them, it does
	// not convert them.
	if got := wire["total_sen"]; got != float64(2490) {
		t.Fatalf("JSON total_sen = %v, want 2490 (money stays integer sen)", got)
	}
}

func TestSaleRowCurrencyDefaultsToMYRAndNormalises(t *testing.T) {
	// A sale written before the currency column existed carries ""; MYR is what
	// it was charged in, so the row must not render an empty label.
	if got := (&dbstore.Sale{RefCode: "JF-old", TotalSen: 2490}).CurrencyOrMYR(); got != "MYR" {
		t.Fatalf("CurrencyOrMYR() on an empty currency = %q, want MYR", got)
	}
	if got := toSaleRow(&dbstore.Sale{RefCode: "JF-old", TotalSen: 2490}, "").Currency; got != "MYR" {
		t.Fatalf("sale row currency = %q, want the MYR fallback", got)
	}
	// Stored codes are upper-case ISO; a lower-case or padded value is the same
	// currency and must not split into two labels on the page.
	if got := (&dbstore.Sale{Currency: " usd "}).CurrencyOrMYR(); got != "USD" {
		t.Fatalf("CurrencyOrMYR() on \" usd \" = %q, want USD", got)
	}
}

// TestRestSalesReturnsEachSalesCurrency is the endpoint-level guard: the list
// the Sales page actually requests must carry the currency of each row.
func TestRestSalesReturnsEachSalesCurrency(t *testing.T) {
	ctx := context.Background()
	h, pool, accountID, formID := salesRowFixture(t, "payment")
	raw := bundleAPIKey(t, ctx, pool, accountID)

	// Two rows in DIFFERENT currencies: a workspace that switched from MYR to USD
	// keeps its old sale in MYR, and the list must label each row on its own. A
	// single-currency fixture would pass even if the handler read one currency for
	// the whole page.
	usd := &dbstore.Sale{
		AccountID: accountID, FormID: formID,
		RefCode: "JF-USD-" + accountID[:8],
		Client:  dbstore.SaleClient{FullName: "US buyer", Email: "us@example.test"},
		Items: []dbstore.SaleItem{{
			Name: "Widget", Qty: 1, PriceSen: 2490,
		}},
		SubtotalSen: 2490, TotalSen: 2490, Currency: "USD",
		SaleType: dbstore.SaleTypeOneTime, PaymentMethod: "chip", Status: dbstore.SalePaid,
	}
	myr := &dbstore.Sale{
		AccountID: accountID, FormID: formID,
		RefCode: "JF-MYR-" + accountID[:8],
		Client:  dbstore.SaleClient{FullName: "MY buyer", Email: "my@example.test"},
		Items: []dbstore.SaleItem{{
			Name: "Widget", Qty: 1, PriceSen: 2490,
		}},
		SubtotalSen: 2490, TotalSen: 2490, Currency: "MYR",
		SaleType: dbstore.SaleTypeOneTime, PaymentMethod: "chip", Status: dbstore.SalePaid,
	}
	for _, s := range []*dbstore.Sale{usd, myr} {
		if err := dbstore.NewSaleStore(pool).Create(ctx, s); err != nil {
			t.Fatalf("create sale %s: %v", s.RefCode, err)
		}
	}

	code, body := salesGet(t, h, raw, "/v1/rest/sales?page=1&limit=15&form_id="+formID)
	if code != http.StatusOK {
		t.Fatalf("sales status = %d, want 200 (body %s)", code, body)
	}
	var page struct {
		Items []struct {
			RefCode  string `json:"ref_code"`
			TotalSen int64  `json:"total_sen"`
			Currency string `json:"currency"`
		} `json:"items"`
		Total int64 `json:"total"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("decode sales page: %v (%s)", err, body)
	}
	if page.Total != 2 || len(page.Items) != 2 {
		t.Fatalf("sales page total=%d items=%d, want 2 rows — body %s", page.Total, len(page.Items), body)
	}

	want := map[string]string{usd.RefCode: "USD", myr.RefCode: "MYR"}
	for _, row := range page.Items {
		cur, ok := want[row.RefCode]
		if !ok {
			t.Fatalf("unexpected row %q in the sales page: %s", row.RefCode, body)
		}
		if row.Currency != cur {
			t.Errorf("row %s currency = %q, want %q (body %s)", row.RefCode, row.Currency, cur, body)
		}
		if row.TotalSen != 2490 {
			t.Errorf("row %s total_sen = %d, want 2490", row.RefCode, row.TotalSen)
		}
		// The key must be PRESENT on the wire, not merely present in the struct:
		// an empty string here is the pre-fix payload, where the page had nothing
		// to format with.
		if strings.TrimSpace(row.Currency) == "" {
			t.Errorf("row %s shipped without a currency — the page can only hardcode a symbol", row.RefCode)
		}
	}
}
