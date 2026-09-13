package dbstore

// Fixture for the issue #534/#535 subscription tests: a real product plus a due
// active subscription on it (subscriptions.product_id is a uuid FK, so the
// product row must exist).

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// newDueSubscriptionFixture creates a subscription product and a due active
// subscription on it, returning the subscription id.
func newDueSubscriptionFixture(t *testing.T, ctx context.Context, p *pgxpool.Pool, acc, email string) string {
	t.Helper()
	prod := &Product{
		AccountID: acc, Name: "Sub product", PriceSen: 1500,
		Type: SaleTypeSubscription, BillingPeriod: "month", Active: true,
	}
	if err := NewProductStore(p).Create(ctx, prod); err != nil {
		t.Fatalf("create product: %v", err)
	}
	sub := &Subscription{
		AccountID: acc, ClientEmail: email, ProductID: prod.ID,
		ChipTokenPurchaseID: "tok-534-" + prod.ID[:8], Status: SubActive, Period: "month",
		AmountSen: 1500, NextChargeAt: time.Now().Add(-time.Hour),
	}
	if err := NewSubscriptionStore(p).Create(ctx, sub); err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	return sub.ID
}
