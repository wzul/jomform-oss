package dbstore

// Issue #153: buyer-facing variation selector — the variation stock must be
// decremented atomically (no oversell) and a sold-out variation must reject.
// Integration test; skipped unless JOMFORM_TEST_DATABASE_URL is set.

import (
	"context"
	"testing"
)

func TestVariationDecrementStockAtomic(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)

	acc := uniqueAccount(t, ctx, p)
	prod := &Product{
		AccountID: acc, Name: "Tee", PriceSen: 1000, Type: SaleTypeOneTime, Active: true,
	}
	if err := NewProductStore(p).Create(ctx, prod); err != nil {
		t.Fatalf("create product: %v", err)
	}

	vs := NewProductVariationStore(p)
	// Two variations: one limited (stock 2), one unlimited (NULL).
	limited := &ProductVariation{Name: "Size M", PriceSen: 1200, Stock: intPtr(2)}
	unlimited := &ProductVariation{Name: "Size L", PriceSen: 1300, Stock: nil}
	if err := vs.ReplaceAll(ctx, prod.ID, []*ProductVariation{limited, unlimited}); err != nil {
		t.Fatalf("replace variations: %v", err)
	}

	// Fetch the persisted variations to get their ids.
	all, err := vs.ListByProduct(ctx, prod.ID)
	if err != nil || len(all) != 2 {
		t.Fatalf("list variations: %v (n=%d)", err, len(all))
	}
	var limID, unlimID string
	for _, v := range all {
		if v.Name == "Size M" {
			limID = v.ID
		} else {
			unlimID = v.ID
		}
	}

	// Decrement the limited variation twice (stock 2 -> 0).
	if err := vs.DecrementStock(ctx, limID, 1); err != nil {
		t.Fatalf("decrement 1: %v", err)
	}
	if err := vs.DecrementStock(ctx, limID, 1); err != nil {
		t.Fatalf("decrement 2: %v", err)
	}
	// Third decrement must reject (sold out).
	if err := vs.DecrementStock(ctx, limID, 1); err != ErrOutOfStock {
		t.Fatalf("decrement 3 = %v, want ErrOutOfStock", err)
	}

	// Unlimited variation never rejects.
	for i := 0; i < 3; i++ {
		if err := vs.DecrementStock(ctx, unlimID, 1); err != nil {
			t.Fatalf("unlimited decrement %d: %v", i, err)
		}
	}

	// GetByProduct validates ownership: a variation from another product must
	// not resolve under this product.
	other := &Product{AccountID: acc, Name: "Other", PriceSen: 500, Type: SaleTypeOneTime, Active: true}
	if err := NewProductStore(p).Create(ctx, other); err != nil {
		t.Fatalf("create other product: %v", err)
	}
	if _, err := vs.GetByProduct(ctx, other.ID, limID); err != ErrVariationNotFound {
		t.Fatalf("GetByProduct cross-product = %v, want ErrVariationNotFound", err)
	}
	// The correct product resolves it.
	got, err := vs.GetByProduct(ctx, prod.ID, limID)
	if err != nil {
		t.Fatalf("GetByProduct own product: %v", err)
	}
	if got.Name != "Size M" {
		t.Errorf("variation name = %q, want Size M", got.Name)
	}
}
