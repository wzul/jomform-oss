package dbstore

// Integration tests for the extended ProductStore.Update (#124) covering the
// full editable field set: name, description, price_sen, images[], stock,
// active, type, billing_period, trial_days. Money stays integer sen.
// Skipped unless JOMFORM_TEST_DATABASE_URL is set; migrations are applied first.

import (
	"context"
	"testing"
)

// ptr helpers for test clarity.
func strPtr(s string) *string          { return &s }
func i64Ptr(v int64) *int64            { return &v }
func intPtr(v int) *int                { return &v }
func boolPtr(b bool) *bool             { return &b }
func strSlicePtr(s []string) *[]string { return &s }

func TestProductUpdateAllFields(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)

	acc := uniqueAccount(t, ctx, p)
	prod := &Product{
		AccountID: acc, Name: "Orig", Description: "desc", PriceSen: 1000,
		Images: []string{"https://a/img1.png"}, Type: SaleTypeSubscription,
		BillingPeriod: "month", TrialDays: 7, Active: true,
		Stock: intPtr(5),
	}
	if err := NewProductStore(p).Create(ctx, prod); err != nil {
		t.Fatalf("create product: %v", err)
	}
	store := NewProductStore(p)

	newName := "Updated"
	newDesc := "new desc"
	newPrice := int64(2500)
	newImages := []string{"https://a/i1.png", "https://a/i2.png"}
	newStock := 0
	newActive := false
	newTrial := 30
	if err := store.Update(ctx, acc, prod.ID,
		&newName, &newDesc, &newPrice, &newImages, &newStock, &newActive,
		strPtr(SaleTypeOneTime), nil, &newTrial, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("update product: %v", err)
	}

	got, err := store.Get(ctx, acc, prod.ID)
	if err != nil {
		t.Fatalf("get product: %v", err)
	}
	if got.Name != newName {
		t.Errorf("name = %q, want %q", got.Name, newName)
	}
	if got.Description != newDesc {
		t.Errorf("description = %q, want %q", got.Description, newDesc)
	}
	if got.PriceSen != newPrice {
		t.Errorf("price_sen = %d, want %d", got.PriceSen, newPrice)
	}
	if len(got.Images) != 2 || got.Images[0] != newImages[0] || got.Images[1] != newImages[1] {
		t.Errorf("images = %v, want %v", got.Images, newImages)
	}
	if got.Stock == nil || *got.Stock != newStock {
		t.Errorf("stock = %v, want %d", got.Stock, newStock)
	}
	if got.Active != newActive {
		t.Errorf("active = %v, want %v", got.Active, newActive)
	}
	if got.Type != SaleTypeOneTime {
		t.Errorf("type = %q, want one_time", got.Type)
	}
	// Switching to one_time forces billing_period NULL.
	if got.BillingPeriod != "" {
		t.Errorf("billing_period = %q, want empty when one_time", got.BillingPeriod)
	}
	if got.TrialDays != newTrial {
		t.Errorf("trial_days = %d, want %d", got.TrialDays, newTrial)
	}
}

func TestProductUpdatePartialAndImagesNilKeeps(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)

	acc := uniqueAccount(t, ctx, p)
	prod := &Product{
		AccountID: acc, Name: "Orig", Description: "d", PriceSen: 1000,
		Images: []string{"https://a/keep.png"}, Type: SaleTypeOneTime,
		Active: true, Stock: intPtr(3),
	}
	if err := NewProductStore(p).Create(ctx, prod); err != nil {
		t.Fatalf("create product: %v", err)
	}
	store := NewProductStore(p)

	// Partial update — only name. images == nil must keep stored images.
	newName := "Renamed"
	if err := store.Update(ctx, acc, prod.ID, &newName, nil, nil, nil, nil, nil,
		nil, nil, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("update product: %v", err)
	}
	got, err := store.Get(ctx, acc, prod.ID)
	if err != nil {
		t.Fatalf("get product: %v", err)
	}
	if got.Name != newName {
		t.Errorf("name = %q, want %q", got.Name, newName)
	}
	if len(got.Images) != 1 || got.Images[0] != "https://a/keep.png" {
		t.Errorf("images = %v, want kept [https://a/keep.png]", got.Images)
	}
	if got.Description != "d" {
		t.Errorf("description = %q, want kept 'd'", got.Description)
	}

	// Non-nil empty images slice clears the images.
	empty := []string{}
	if err := store.Update(ctx, acc, prod.ID, nil, nil, nil, &empty, nil, nil,
		nil, nil, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("update images: %v", err)
	}
	got2, err := store.Get(ctx, acc, prod.ID)
	if err != nil {
		t.Fatalf("get product: %v", err)
	}
	if len(got2.Images) != 0 {
		t.Errorf("images = %v, want empty", got2.Images)
	}
}

func TestProductUpdateNotOwned(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)

	acc1 := uniqueAccount(t, ctx, p)
	acc2 := uniqueAccount(t, ctx, p)
	prod := &Product{
		AccountID: acc1, Name: "A", PriceSen: 500, Type: SaleTypeOneTime,
	}
	if err := NewProductStore(p).Create(ctx, prod); err != nil {
		t.Fatalf("create product: %v", err)
	}
	if err := NewProductStore(p).Update(ctx, acc2, prod.ID,
		strPtr("x"), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil); err != ErrProductNotFound {
		t.Fatalf("update across account = %v, want ErrProductNotFound", err)
	}
}
