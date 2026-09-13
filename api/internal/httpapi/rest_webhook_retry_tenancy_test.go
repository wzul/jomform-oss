// Issue #522: cross-tenant signing bypass in the webhook delivery retry
// endpoint. POST /v1/rest/webhooks/{id}/deliveries/{deliveryId}/retry must
// authorise {id} against the caller's account, not just {deliveryId}: the
// sweep re-resolves the webhook id with an any-account lookup and delivers the
// caller's payload to that webhook's endpoint, HMAC-signed with that webhook's
// own secret.
//
// Integration test against a real Postgres; skipped without
// JOMFORM_TEST_DATABASE_URL (mirrors tenancy_isolation_test.go's fixture style).
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// TestWebhookRetryCrossTenantRefused is the issue #522 regression test: a
// merchant may only retry a delivery against a webhook THEIR OWN account owns.
// Merchant B authenticates, passes B's own delivery id together with merchant
// A's webhook id, and must be refused with 404 while no retry row is created
// for A's webhook (which the sweep would otherwise deliver to A's endpoint,
// signed with A's secret).
func TestWebhookRetryCrossTenantRefused(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)

	// Merchant A: owns webhook W_A. Merchant B: owns webhook W_B + delivery D_B.
	accA, _ := bundleFixture(t, ctx, pool)
	accB, _ := bundleFixture(t, ctx, pool)
	if accA == accB {
		t.Fatalf("fixtures must be distinct accounts (both %q)", accA)
	}

	// The scratch schema is shared by every test in this package, so clear the
	// queue at entry and on cleanup: the "no retry row for W_A" assertion must
	// be deterministic on every -count.
	testdb.Reset(t, pool, "webhook_retries")

	whs := dbstore.NewWebhookStore(pool)
	whA, err := whs.Create(ctx, accA, "", "https://a.example.test/hook", "secretA123", []string{"sale.paid"})
	if err != nil {
		t.Fatalf("create A webhook: %v", err)
	}
	whB, err := whs.Create(ctx, accB, "", "https://b.example.test/hook", "secretB123", []string{"sale.paid"})
	if err != nil {
		t.Fatalf("create B webhook: %v", err)
	}
	// B seeds a failed delivery of its own — its payload text is attacker
	// controlled, so a cross-tenant enqueue injects arbitrary content.
	const attackerPayload = `{"forged":"attacker-controlled"}`
	if err := whs.RecordDelivery(ctx, whB.ID, "sale.paid", attackerPayload, 500, false, 1, "boom"); err != nil {
		t.Fatalf("record B delivery: %v", err)
	}
	bDeliveries, err := whs.ListDeliveries(ctx, accB, whB.ID, 10)
	if err != nil || len(bDeliveries) != 1 {
		t.Fatalf("list B deliveries: %v (n=%d)", err, len(bDeliveries))
	}
	bDeliveryID := bDeliveries[0]["id"].(string)

	h := newBundleHandlers(pool)
	rawB := bundleAPIKey(t, ctx, pool, accB)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	retryCount := func(webhookID string) int {
		var n int
		if err := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM webhook_retries WHERE webhook_id=$1`, webhookID).Scan(&n); err != nil {
			t.Fatalf("count retries for %s: %v", webhookID, err)
		}
		return n
	}
	postRetry := func(webhookID, deliveryID, rawKey string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST",
			"/v1/rest/webhooks/"+webhookID+"/deliveries/"+deliveryID+"/retry", nil)
		req.Header.Set("Authorization", "Bearer "+rawKey)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// ── the attack: A's webhook id + B's (own, valid) delivery id ───────────
	rec := postRetry(whA.ID, bDeliveryID, rawB)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant retry status = %d, want 404 (%s)", rec.Code, rec.Body.String())
	}
	if n := retryCount(whA.ID); n != 0 {
		t.Fatalf("cross-tenant retry created %d retry row(s) for another tenant's webhook, want 0", n)
	}

	// ── the happy path: B's own webhook + B's own delivery still queues ─────
	rec = postRetry(whB.ID, bDeliveryID, rawB)
	if rec.Code != http.StatusOK {
		t.Fatalf("own retry status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("retry bad json: %v", err)
	}
	if body["status"] != "queued" {
		t.Errorf("own retry body status = %v, want queued", body["status"])
	}
	if n := retryCount(whB.ID); n != 1 {
		t.Fatalf("own retry created %d retry row(s), want 1", n)
	}
	// A's webhook must still have nothing queued against it.
	if n := retryCount(whA.ID); n != 0 {
		t.Fatalf("own retry leaked into another tenant's webhook: %d row(s), want 0", n)
	}
}

// TestGetDeliveryExposesWebhookID pins the store contract the authorisation
// check depends on: GetDelivery must return webhook_id so a caller can compare
// the delivery's owning webhook against the one in the request path.
func TestGetDeliveryExposesWebhookID(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, _ := bundleFixture(t, ctx, pool)

	whs := dbstore.NewWebhookStore(pool)
	wh, err := whs.Create(ctx, accountID, "", "https://example.test/hook", "secret1234", []string{"sale.paid"})
	if err != nil {
		t.Fatalf("create webhook: %v", err)
	}
	if err := whs.RecordDelivery(ctx, wh.ID, "sale.paid", `{"ok":1}`, 500, false, 1, "boom"); err != nil {
		t.Fatalf("record delivery: %v", err)
	}
	ds, err := whs.ListDeliveries(ctx, accountID, wh.ID, 10)
	if err != nil || len(ds) != 1 {
		t.Fatalf("list deliveries: %v (n=%d)", err, len(ds))
	}
	d, err := whs.GetDelivery(ctx, accountID, ds[0]["id"].(string))
	if err != nil {
		t.Fatalf("get delivery: %v", err)
	}
	got, _ := d["webhook_id"].(string)
	if got != wh.ID {
		t.Fatalf("GetDelivery webhook_id = %q, want %q", got, wh.ID)
	}
}
