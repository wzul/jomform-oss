package httpapi

// Issue #131: merchant subscription management via REST.
//   - GET /v1/rest/subscriptions lists account-scoped subscriptions with
//     customer/product/amount/status/next-charge, paginated + status filter.
//   - POST /v1/rest/subscriptions/{id}/cancel cancels one (account-scoped,
//     token retained — Wan rule, no revoke).
// Integration against a real Postgres; skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/dbstore"
)

func subRequest(t *testing.T, h *RESTHandlers, rawKey, method, path, body string) (int, map[string]any) {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set("Authorization", "Bearer "+rawKey)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var parsed map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &parsed)
	return rec.Code, parsed
}

// seedSubscriptionStores creates a subscription product + a subscription row
// for the account and returns the created product + subscription.
func seedSubscriptionStores(t *testing.T, ctx context.Context, pool *pgxpool.Pool, accountID string) (*dbstore.Product, *dbstore.Subscription) {
	t.Helper()
	products := dbstore.NewProductStore(pool)
	prod := &dbstore.Product{
		AccountID: accountID, Name: "Sub Tee", PriceSen: 1500,
		Type: dbstore.SaleTypeSubscription, BillingPeriod: "month", Active: true,
	}
	if err := products.Create(ctx, prod); err != nil {
		t.Fatalf("create product: %v", err)
	}
	subs := dbstore.NewSubscriptionStore(pool)
	sub := &dbstore.Subscription{
		AccountID: accountID, ClientEmail: "buyer@example.com", ProductID: prod.ID,
		ChipTokenPurchaseID: "tok-purchase-1", Status: dbstore.SubActive, Period: "month",
		AmountSen: 1500, NextChargeAt: time.Now().Add(30 * 24 * time.Hour),
	}
	if err := subs.Create(ctx, sub); err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	return prod, sub
}

func TestListSubscriptionsAccountScopedPaginated(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, _ := bundleFixture(t, ctx, pool)
	_, sub := seedSubscriptionStores(t, ctx, pool, accountID)

	h := newBundleHandlers(pool)
	raw := bundleAPIKey(t, ctx, pool, accountID)

	// Status filter active.
	code, resp := subRequest(t, h, raw, "GET", "/v1/rest/subscriptions?status=active", "")
	if code != http.StatusOK {
		t.Fatalf("list status = %d, want 200 (body=%v)", code, resp)
	}
	items, _ := resp["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 subscription, got %d (%v)", len(items), resp)
	}
	row := items[0].(map[string]any)
	if row["id"] != sub.ID {
		t.Errorf("item.id = %v, want %s", row["id"], sub.ID)
	}
	if row["customer"] != "buyer@example.com" {
		t.Errorf("item.customer = %v, want buyer@example.com", row["customer"])
	}
	if np, _ := row["product_name"].(string); np != "Sub Tee" {
		t.Errorf("item.product_name = %q, want 'Sub Tee'", np)
	}
	if amt, _ := row["amount_sen"].(float64); int64(amt) != 1500 {
		t.Errorf("item.amount_sen = %v, want 1500", amt)
	}
	if st, _ := row["status"].(string); st != "active" {
		t.Errorf("item.status = %q, want active", st)
	}
	if nc, _ := row["next_charge_at"].(string); nc == "" {
		t.Errorf("item.next_charge_at should be set, got empty")
	}
	if total, _ := resp["total"].(float64); int64(total) != 1 {
		t.Errorf("resp.total = %v, want 1", resp["total"])
	}

	// Status filter cancelled yields none.
	code2, resp2 := subRequest(t, h, raw, "GET", "/v1/rest/subscriptions?status=cancelled", "")
	if code2 != http.StatusOK {
		t.Fatalf("list cancelled status = %d, want 200", code2)
	}
	if items2, _ := resp2["items"].([]any); len(items2) != 0 {
		t.Errorf("cancelled filter returned %d, want 0", len(items2))
	}
	// pagination metadata present
	if _, ok := resp["page"]; !ok {
		t.Errorf("expected page metadata in response")
	}
}

func TestCancelSubscriptionNoTokenRevokeAndAccountScoped(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, _ := bundleFixture(t, ctx, pool)
	_, sub := seedSubscriptionStores(t, ctx, pool, accountID)

	h := newBundleHandlers(pool)
	raw := bundleAPIKey(t, ctx, pool, accountID)

	// Cancel own subscription.
	code, resp := subRequest(t, h, raw, "POST", "/v1/rest/subscriptions/"+sub.ID+"/cancel", "")
	if code != http.StatusOK {
		t.Fatalf("cancel status = %d, want 200 (body=%v)", code, resp)
	}
	if st, _ := resp["status"].(string); st != dbstore.SubCanceled {
		t.Errorf("resp.status = %q, want cancelled", st)
	}

	// Verify the subscription row became cancelled.
	got, err := dbstore.NewSubscriptionStore(pool).ByIDForAccount(ctx, accountID, sub.ID)
	if err != nil {
		t.Fatalf("read back sub: %v", err)
	}
	if got.Status != dbstore.SubCanceled {
		t.Errorf("sub.status after cancel = %q, want cancelled — token must be retained (re-activatable)", got.Status)
	}

	// Non-owned subscription (another account) must 404.
	otherID, _ := bundleFixture(t, ctx, pool)
	_, otherSub := seedSubscriptionStores(t, ctx, pool, otherID)
	rawOther := bundleAPIKey(t, ctx, pool, otherID)
	code2, _ := subRequest(t, h, rawOther, "POST", "/v1/rest/subscriptions/"+sub.ID+"/cancel", "")
	if code2 != http.StatusNotFound {
		t.Errorf("cross-account cancel = %d, want 404", code2)
	}
	_ = otherSub
}
