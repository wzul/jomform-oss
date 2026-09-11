// Issue #197: POST /v1/rest/webhooks/{id}/deliveries/{deliveryId}/retry
// re-enqueues a failed delivery immediately. Integration against a real
// Postgres; skipped without JOMFORM_TEST_DATABASE_URL.
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

func TestWebhookRetryREST(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, _ := bundleFixture(t, ctx, pool)

	// The test DB is shared across runs; stale due retries from earlier runs
	// can fill DueRetries(limit) and hide this run's row. Clear the queue so
	// the assertion below is deterministic (issue: flaky TestWebhookRetryREST).
	if _, err := pool.Exec(ctx, `DELETE FROM webhook_retries`); err != nil {
		t.Fatalf("clear webhook_retries: %v", err)
	}

	whs := dbstore.NewWebhookStore(pool)
	wh, err := whs.Create(ctx, accountID, "", "https://example.test/hook", "secret1234", []string{"sale.paid"})
	if err != nil {
		t.Fatalf("create webhook: %v", err)
	}
	// Seed a failed delivery.
	if err := whs.RecordDelivery(ctx, wh.ID, "sale.paid", `{"ok":1}`, 500, false, 1, "boom"); err != nil {
		t.Fatalf("record failed delivery: %v", err)
	}
	deliveries, err := whs.ListDeliveries(ctx, accountID, wh.ID, 10)
	if err != nil || len(deliveries) != 1 {
		t.Fatalf("list deliveries: %v (n=%d)", err, len(deliveries))
	}
	deliveryID := deliveries[0]["id"].(string)

	h := newBundleHandlers(pool)
	raw := bundleAPIKey(t, ctx, pool, accountID)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Retry the failed delivery → 200 queued.
	req := httptest.NewRequest("POST", "/v1/rest/webhooks/"+wh.ID+"/deliveries/"+deliveryID+"/retry", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("retry status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("retry bad json: %v", err)
	}
	if body["status"] != "queued" {
		t.Errorf("retry status body = %v, want queued", body["status"])
	}

	// The retry is now in the queue (due now), for this webhook.
	due, err := whs.DueRetries(ctx, 10)
	if err != nil {
		t.Fatalf("due retries: %v", err)
	}
	found := false
	for _, r := range due {
		if r.WebhookID == wh.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no due retry for webhook %s after manual retry", wh.ID)
	}

	// Account scoping: a different account cannot retry this delivery.
	otherID, _ := bundleFixture(t, ctx, pool)
	otherRaw := bundleAPIKey(t, ctx, pool, otherID)
	req2 := httptest.NewRequest("POST", "/v1/rest/webhooks/"+wh.ID+"/deliveries/"+deliveryID+"/retry", nil)
	req2.Header.Set("Authorization", "Bearer "+otherRaw)
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("other account retry status = %d, want 404 (account scoping failed)", rec2.Code)
	}
}
