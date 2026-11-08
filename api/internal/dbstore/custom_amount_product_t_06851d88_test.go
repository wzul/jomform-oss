package dbstore

// Card t_06851d88: custom_amount (pay-what-you-want) products.
//
// These are the STORE-level guards for the columns the feature depends on. The
// productSelect/trashedProductSelect column lists are scanned positionally, so a
// migration that adds min/max/suggested without the matching Scan destination
// would silently shift every later field (name into description, type into
// price). The round trip below is what catches that.

import (
	"context"
	"testing"
)

func TestCustomAmountProductRoundTrip(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	acc := uniqueAccount(t, ctx, p)
	store := NewProductStore(p)

	minSen := int64(500)
	maxSen := int64(50000)
	suggested := int64(1000)
	prod := &Product{
		AccountID: acc, Name: "Derma Masjid", PriceSen: suggested,
		Type: SaleTypeCustomAmount, Active: true,
		MinPriceSen: minSen, MaxPriceSen: &maxSen, SuggestedPriceSen: &suggested,
	}
	if err := store.Create(ctx, prod); err != nil {
		t.Fatalf("create custom_amount product: %v", err)
	}

	got, err := store.Get(ctx, acc, prod.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Type != SaleTypeCustomAmount {
		t.Errorf("type = %q, want %q — the products.type CHECK must accept it", got.Type, SaleTypeCustomAmount)
	}
	if got.Name != "Derma Masjid" {
		t.Errorf("name = %q, want \"Derma Masjid\" — a shifted scan reads the wrong column", got.Name)
	}
	if got.MinPriceSen != minSen {
		t.Errorf("min_price_sen = %d, want %d", got.MinPriceSen, minSen)
	}
	if got.MaxPriceSen == nil || *got.MaxPriceSen != maxSen {
		t.Errorf("max_price_sen = %v, want %d", got.MaxPriceSen, maxSen)
	}
	if got.SuggestedPriceSen == nil || *got.SuggestedPriceSen != suggested {
		t.Errorf("suggested_price_sen = %v, want %d", got.SuggestedPriceSen, suggested)
	}
	if got.PriceSen != suggested {
		t.Errorf("price_sen = %d, want the suggested %d (display fallback)", got.PriceSen, suggested)
	}

	// Clearing the cap and the suggestion must write SQL NULL, not 0 — a 0 cap
	// would make every amount too high, and a 0 suggestion would pre-fill RM0.00.
	var nilSen *int64
	newMin := int64(800)
	newPrice := int64(800)
	if err := store.Update(ctx, acc, prod.ID,
		nil, nil, &newPrice, nil, nil, nil, nil, nil, nil, nil, nil,
		&newMin, &nilSen, &nilSen); err != nil {
		t.Fatalf("update limits: %v", err)
	}
	got2, err := store.Get(ctx, acc, prod.ID)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if got2.MinPriceSen != newMin {
		t.Errorf("min_price_sen = %d, want %d", got2.MinPriceSen, newMin)
	}
	if got2.MaxPriceSen != nil {
		t.Errorf("max_price_sen = %v, want NULL (no cap)", *got2.MaxPriceSen)
	}
	if got2.SuggestedPriceSen != nil {
		t.Errorf("suggested_price_sen = %v, want NULL (no suggestion)", *got2.SuggestedPriceSen)
	}

	// The buyer page resolves a block's product through this list, so a scan
	// mismatch here silently degrades a custom_amount product to fixed price.
	list, lerr := store.ListActiveByAccount(ctx, acc)
	if lerr != nil {
		t.Fatalf("ListActiveByAccount: %v", lerr)
	}
	found := false
	for _, p := range list {
		if p.ID != prod.ID {
			continue
		}
		found = true
		if p.Type != SaleTypeCustomAmount {
			t.Errorf("listed type = %q, want %q", p.Type, SaleTypeCustomAmount)
		}
		if p.MinPriceSen != newMin {
			t.Errorf("listed min_price_sen = %d, want %d", p.MinPriceSen, newMin)
		}
	}
	if !found {
		t.Error("the custom_amount product is missing from ListActiveByAccount")
	}
}

// TestCustomAmountProductFloorIsEnforcedByTheSchema is the second half of the
// server-side limit rule: the API refuses min_price_sen < RM1.00, and the schema
// must refuse it too, or a direct write could create a product the checkout
// cannot price.
func TestCustomAmountProductFloorIsEnforcedByTheSchema(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	acc := uniqueAccount(t, ctx, p)

	tooLow := int64(50) // RM0.50, below the RM1.00 floor
	bad := &Product{
		AccountID: acc, Name: "Below floor", PriceSen: 50,
		Type: SaleTypeCustomAmount, Active: true, MinPriceSen: tooLow,
	}
	if err := NewProductStore(p).Create(ctx, bad); err == nil {
		t.Fatal("creating a custom_amount product with min_price_sen=50 succeeded — the schema CHECK is missing")
	}

	// A max below the min is also nonsense and must be refused.
	minSen := int64(100)
	maxSen := int64(90)
	bad2 := &Product{
		AccountID: acc, Name: "Inverted limits", PriceSen: 100,
		Type: SaleTypeCustomAmount, Active: true, MinPriceSen: minSen, MaxPriceSen: &maxSen,
	}
	if err := NewProductStore(p).Create(ctx, bad2); err == nil {
		t.Fatal("creating a custom_amount product with max_price_sen < min_price_sen succeeded")
	}
}

// TestSaleTypeCustomAmountIsStorable pins the OTHER half of the migration: the
// order core records sale_type='custom_amount', so sales.sale_type must accept
// it. Without this the INSERT fails and the buyer's sale is lost.
func TestSaleTypeCustomAmountIsStorable(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	acc := uniqueAccount(t, ctx, p)

	sales := NewSaleStore(p)
	sale := &Sale{
		AccountID: acc,
		RefCode:   "JF-CA-" + acc[:8],
		Client:    SaleClient{FullName: "Penderma", Email: "derma@example.test"},
		Items: []SaleItem{
			{ProductID: "11111111-1111-1111-1111-111111111111", Name: "Derma", PriceSen: 12345, Qty: 1},
		},
		SubtotalSen: 12345, TotalSen: 12345, Currency: "MYR",
		SaleType: SaleTypeCustomAmount, PaymentMethod: "chip", Status: SalePending,
	}
	if err := sales.Create(ctx, sale); err != nil {
		t.Fatalf("create custom_amount sale: %v", err)
	}
	got, err := sales.ByID(ctx, sale.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.SaleType != SaleTypeCustomAmount {
		t.Errorf("sale_type = %q, want %q", got.SaleType, SaleTypeCustomAmount)
	}
	if len(got.Items) != 1 || got.Items[0].PriceSen != 12345 {
		t.Errorf("items = %+v, want the chosen amount 12345 as the line price", got.Items)
	}
}
