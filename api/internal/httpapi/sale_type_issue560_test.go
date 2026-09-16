package httpapi

// Issue #560: the public checkout hardcoded sale_type='one_time', so a
// subscription product could NEVER create a subscription.
//
//	SaleType: dbstore.SaleTypeOneTime,     // order.go — for every order
//
// Two things followed from that, both on the customer-facing path:
//   - activateSubscription only runs for a subscription sale, so no subscription
//     row was ever created and the recurring engine had nothing to charge;
//   - the card is only tokenized when the sale is a subscription
//     (ForceRecurring: sale.SaleType == SaleTypeSubscription), so no recurring
//     token was ever saved either.
//
// The existing coverage drove activateSubscription directly with a hand-built
// subscription sale, which is exactly why this was invisible: it proved the
// activation works GIVEN a subscription sale, and nothing proved a buyer's
// order ever produces one.

import (
	"context"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// TestIssue560CheckoutResolvesSaleTypeFromTheProduct drives the REAL rule used by
// submitOrder. An earlier version of this test re-implemented the decision in the
// test body, so it still passed with the production rule deleted — the
// neutralisation check caught that. It now calls resolveSubscriptionTerms.
func TestIssue560CheckoutResolvesSaleTypeFromTheProduct(t *testing.T) {
	oneTime := &dbstore.Product{Type: dbstore.SaleTypeOneTime, PriceSen: 500}
	monthly := &dbstore.Product{ID: "p-month", Type: dbstore.SaleTypeSubscription, PriceSen: 10000, BillingPeriod: "month"}
	yearly := &dbstore.Product{ID: "p-year", Type: dbstore.SaleTypeSubscription, PriceSen: 120000, BillingPeriod: "year"}

	sub := func(priceSen int64, qty int64) []dbstore.SaleItem {
		return []dbstore.SaleItem{{ProductID: "p-month", PriceSen: priceSen, Qty: qty}}
	}

	cases := []struct {
		name       string
		product    *dbstore.Product
		subItems   []dbstore.SaleItem
		wantType   string
		wantAmount int64
		wantPeriod string
	}{
		{"no product resolved (pricing block)", nil, nil, "", 0, ""},
		{"one-time product", oneTime, nil, "", 0, ""},
		{"monthly subscription", monthly, sub(10000, 1), dbstore.SaleTypeSubscription, 10000, "month"},
		{"yearly subscription", yearly, []dbstore.SaleItem{{ProductID: "p-year", PriceSen: 120000, Qty: 1}}, dbstore.SaleTypeSubscription, 120000, "year"},
		// The decided rule for a mixed cart: the SUBSCRIPTION product drives the
		// schedule and the recurring amount is its own price x qty. The one-time
		// item is charged once, in the first payment only.
		{"subscription qty 2 + a one-time item in the cart", monthly, sub(10000, 2), dbstore.SaleTypeSubscription, 20000, "month"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotType, gotAmount, gotPeriod := resolveSubscriptionTerms(tc.product, tc.subItems)
			if gotType != tc.wantType {
				t.Errorf("sale_type = %q, want %q", gotType, tc.wantType)
			}
			if gotAmount != tc.wantAmount {
				t.Errorf("amount = %d, want %d — the recurring amount is the subscription product's own price x qty, never the order total", gotAmount, tc.wantAmount)
			}
			if gotPeriod != tc.wantPeriod {
				t.Errorf("period = %q, want %q — a yearly product billed monthly is a real billing error", gotPeriod, tc.wantPeriod)
			}
		})
	}
}

// TestIssue560YearlyProductIsNotBilledMonthly pins the hardcoded-period bug.
// activateSubscription used `period := "month"` with the comment "product lookup
// lands with products store wiring" — a TODO never finished — so a yearly
// product would have been charged every month.
func TestIssue560YearlyProductIsNotBilledMonthly(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, _ := bundleFixture(t, ctx, pool)

	prod := &dbstore.Product{
		AccountID: accountID, Name: "Tahunan", PriceSen: 120000,
		Type: dbstore.SaleTypeSubscription, BillingPeriod: "year", Active: true,
	}
	if err := dbstore.NewProductStore(pool).Create(ctx, prod); err != nil {
		t.Fatalf("product: %v", err)
	}

	sale := &dbstore.Sale{
		AccountID: accountID, RefCode: "R560Y-" + prod.ID[:8],
		Client:      dbstore.SaleClient{Email: "yearly@example.test"},
		Items:       []dbstore.SaleItem{{ProductID: prod.ID, Name: "Tahunan", Qty: 1, PriceSen: 120000}},
		SubtotalSen: 120000, TotalSen: 120000, Currency: "MYR",
		SaleType: dbstore.SaleTypeSubscription, PaymentMethod: "chip",
		Status: dbstore.SalePaid, ChipPurchaseID: "pur-560y-" + prod.ID[:8],
		// The terms the checkout records (issue #560):
		SubscriptionProductID: prod.ID,
		SubscriptionAmountSen: 120000,
		SubscriptionPeriod:    "year",
	}
	if err := dbstore.NewSaleStore(pool).Create(ctx, sale); err != nil {
		t.Fatalf("sale: %v", err)
	}

	// The terms must survive the round trip — activation reads them from the sale.
	got, err := dbstore.NewSaleStore(pool).ByID(ctx, sale.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.SubscriptionPeriod != "year" {
		t.Errorf("stored subscription_period = %q, want %q", got.SubscriptionPeriod, "year")
	}
	if got.SubscriptionAmountSen != 120000 {
		t.Errorf("stored subscription_amount_sen = %d, want 120000", got.SubscriptionAmountSen)
	}
	if got.SubscriptionProductID != prod.ID {
		t.Errorf("stored subscription_product_id = %q, want %q", got.SubscriptionProductID, prod.ID)
	}
}

// TestIssue560MixedCartDoesNotBillTheOneTimeItem keeps the decided rule honest:
// with a subscription + a one-time item, the recurring amount is the
// subscription product's share, never total_sen.
func TestIssue560MixedCartDoesNotBillTheOneTimeItem(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, _ := bundleFixture(t, ctx, pool)

	sub := &dbstore.Product{
		AccountID: accountID, Name: "Kelas", PriceSen: 10000,
		Type: dbstore.SaleTypeSubscription, BillingPeriod: "month", Active: true,
	}
	book := &dbstore.Product{
		AccountID: accountID, Name: "Buku", PriceSen: 2500,
		Type: dbstore.SaleTypeOneTime, Active: true,
	}
	prods := dbstore.NewProductStore(pool)
	for _, p := range []*dbstore.Product{sub, book} {
		if err := prods.Create(ctx, p); err != nil {
			t.Fatalf("product: %v", err)
		}
	}

	// Cart: RM100 subscription + RM25 book => total RM125, recurring RM100.
	sale := &dbstore.Sale{
		AccountID: accountID, RefCode: "R560M-" + sub.ID[:8],
		Client: dbstore.SaleClient{Email: "mixed@example.test"},
		Items: []dbstore.SaleItem{
			{ProductID: sub.ID, Name: "Kelas", Qty: 1, PriceSen: 10000},
			{ProductID: book.ID, Name: "Buku", Qty: 1, PriceSen: 2500},
		},
		SubtotalSen: 12500, TotalSen: 12500, Currency: "MYR",
		SaleType: dbstore.SaleTypeSubscription, PaymentMethod: "chip",
		Status: dbstore.SalePaid, ChipPurchaseID: "pur-560m-" + sub.ID[:8],
		SubscriptionProductID: sub.ID,
		SubscriptionAmountSen: 10000,
		SubscriptionPeriod:    "month",
	}
	if err := dbstore.NewSaleStore(pool).Create(ctx, sale); err != nil {
		t.Fatalf("sale: %v", err)
	}
	got, err := dbstore.NewSaleStore(pool).ByID(ctx, sale.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.TotalSen != 12500 {
		t.Fatalf("total_sen = %d, want 12500 (the buyer pays for everything once)", got.TotalSen)
	}
	if got.SubscriptionAmountSen != 10000 {
		t.Errorf("subscription_amount_sen = %d, want 10000 — the RM25 book must NOT be re-billed every month", got.SubscriptionAmountSen)
	}
	if got.SubscriptionAmountSen == got.TotalSen {
		t.Errorf("subscription_amount_sen equals total_sen; the recurring amount must be the subscription product's own share")
	}
}
