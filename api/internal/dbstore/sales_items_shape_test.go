package dbstore

// The `sales.items` JSONB shape class.
//
// `sales.items` is `JSONB NOT NULL` with no CHECK constraint, so the column can
// hold any JSON — an object, a string, a number. Every app write path serialises
// a Go slice (always an array), so a non-array requires a direct/external write.
// But Postgres raises SQLSTATE 22023 ("cannot extract elements from an object")
// the moment `jsonb_array_elements` is handed one.
//
// Why that is worse than one bad row: the product bin calls
// SalesCountsByProduct ONCE for the WHOLE account and fails the request on its
// error, so a single such row makes the bin unreadable for every product the
// account owns — not merely the product the row happens to mention. The
// analytics rollup has the same shape.
//
// These tests seed exactly that row and assert the reads still answer for the
// REST of the account. "Does not panic" is not the assertion: the other
// products must still be counted, and a purge of an unrelated clean product
// must still succeed.
//
// Integration against a real Postgres; skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// seedSaleWithRawItems writes a sale whose `items` is set from JSON TEXT, so a
// test can put a shape on the column that no Go write path can produce (every
// app path serialises a slice, i.e. an array).
func seedSaleWithRawItems(t *testing.T, ctx context.Context, acc, itemsJSON string) {
	t.Helper()
	p := testPool(t)
	if _, err := p.Exec(ctx,
		`INSERT INTO sales (account_id, ref_code, client, items, total_sen)
		 VALUES ($1, 'JF-IT-' || md5(random()::text), '{}'::jsonb, $2::jsonb, 0)`,
		acc, itemsJSON); err != nil {
		t.Fatalf("seed sale with items=%s: %v", itemsJSON, err)
	}
}

// assertNoExtractError fails with the measured error when a read raised
// SQLSTATE 22023, so a regression names the defect instead of only its symptom.
func assertNoExtractError(t *testing.T, what string, err error) {
	t.Helper()
	if err == nil {
		return
	}
	if strings.Contains(err.Error(), "cannot extract elements") ||
		strings.Contains(err.Error(), "22023") {
		t.Fatalf("%s raised the non-array JSONB error: %v — the whole read fails on ONE odd `sales.items` value", what, err)
	}
	t.Fatalf("%s: %v", what, err)
}

// TestBinListingSurvivesANonArraySalesItems is the card's case: one sale whose
// `items` is an object must not take the account's product bin down.
func TestBinListingSurvivesANonArraySalesItems(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	acc := uniqueAccount(t, ctx, p)
	ps := NewProductStore(p)

	// The product the bin must still report on, plus a clean trashed product a
	// purge must still be able to remove despite the odd row in the account.
	good := trashProduct(t, ctx, acc, "IT non-array good")
	clean := trashProduct(t, ctx, acc, "IT non-array clean")
	if _, err := p.Exec(ctx,
		`INSERT INTO sales (account_id, ref_code, client, items, total_sen)
		 VALUES ($1, 'JF-IT-' || md5(random()::text), '{}'::jsonb,
		         jsonb_build_array(jsonb_build_object('product_id', $2::text, 'qty', 1)), 1200)`,
		acc, good.ID); err != nil {
		t.Fatalf("seed the good sale: %v", err)
	}

	// ONE row, written the only way it can be written: directly. Cover the whole
	// class — object, string and number all fail the same extraction.
	for _, items := range []string{`{"oops":true}`, `"oops"`, `42`} {
		seedSaleWithRawItems(t, ctx, acc, items)
	}

	counts, err := ps.SalesCountsByProduct(ctx, []string{acc})
	assertNoExtractError(t, "SalesCountsByProduct (the bin listing, once for the whole account)", err)
	if counts[good.ID] != 1 {
		t.Errorf("bin sales_count for the well-formed sale = %d, want 1 — a non-array row in the account must be SKIPPED, not lose the other products' counts", counts[good.ID])
	}
	if _, present := counts[clean.ID]; present {
		t.Errorf("the clean product got a sales_count entry (%d) from a row that does not reference it", counts[clean.ID])
	}

	refusal, err := ps.CountSalesByProduct(ctx, acc, good.ID)
	assertNoExtractError(t, "CountSalesByProduct (the purge refusal's number)", err)
	if refusal != 1 {
		t.Errorf("purge refusal sales_count for the well-formed sale = %d, want 1", refusal)
	}

	// The refusal count for an UNRELATED product must be 0, and the purge must
	// therefore go through. The guard query runs before the DELETE, so a raise
	// here is a clean product that cannot be removed for good.
	unrelated, err := ps.CountSalesByProduct(ctx, acc, clean.ID)
	assertNoExtractError(t, "CountSalesByProduct (an unrelated clean product)", err)
	if unrelated != 0 {
		t.Errorf("a product no sale references reports sales_count=%d, want 0", unrelated)
	}
	if err := ps.Purge(ctx, acc, clean.ID); err != nil {
		t.Fatalf("Purge of a clean product failed because another row's `items` is not an array: %v", err)
	}
	if _, err := ps.GetTrashed(ctx, acc, clean.ID); !errors.Is(err, ErrProductNotFound) {
		t.Errorf("after Purge the product should be gone; GetTrashed err = %v", err)
	}

	// And the product the odd rows DO sit alongside is still refused, for the
	// right reason.
	if err := ps.Purge(ctx, acc, good.ID); !errors.Is(err, ErrProductHasSales) {
		t.Errorf("Purge of the sold product = %v, want ErrProductHasSales", err)
	}
}

// TestProductAnalyticsSurvivesANonArraySalesItems covers the same class at
// dbstore/analytics.go, which expands `sales.items` inline in a cross join.
func TestProductAnalyticsSurvivesANonArraySalesItems(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	acc := uniqueAccount(t, ctx, p)
	ps := NewProductStore(p)
	ss := NewSaleStore(p)

	sold := &Product{AccountID: acc, Name: "IT analytics sold", PriceSen: 1200, Currency: "MYR", Type: "one_time"}
	if err := ps.Create(ctx, sold); err != nil {
		t.Fatalf("create product: %v", err)
	}
	if _, err := p.Exec(ctx,
		`INSERT INTO sales (account_id, ref_code, client, items, total_sen, status)
		 VALUES ($1, 'JF-IT-' || md5(random()::text), '{}'::jsonb,
		         jsonb_build_array(jsonb_build_object('product_id', $2::text, 'name', 'IT analytics sold',
		                                              'qty', 2, 'price_sen', 1200)), 2400, 'paid')`,
		acc, sold.ID); err != nil {
		t.Fatalf("seed the good sale: %v", err)
	}
	seedSaleWithRawItems(t, ctx, acc, `{"oops":true}`)

	stats, err := ss.ProductAnalytics(ctx, acc)
	assertNoExtractError(t, "ProductAnalytics", err)
	if len(stats) != 1 {
		t.Fatalf("ProductAnalytics returned %d product rows, want 1 (the non-array sale must be skipped, not lose the account's stats)", len(stats))
	}
	if stats[0].ProductID != sold.ID || stats[0].UnitsSold != 2 {
		t.Errorf("ProductAnalytics[0] = %+v, want product %s with 2 units", stats[0], sold.ID)
	}
}
