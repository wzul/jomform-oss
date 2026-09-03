// Issue #130: GET /v1/rest/webhooks/{id}/deliveries returns the latest
// deliveries (account-scoped, bounded). Integration against a real Postgres;
// skipped without JOMFORM_TEST_DATABASE_URL.
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

func TestWebhookDeliveriesREST(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, _ := bundleFixture(t, ctx, pool)

	whs := dbstore.NewWebhookStore(pool)
	wh, err := whs.Create(ctx, accountID, "", "https://example.test/hook", "secret1234", []string{"sale.paid"})
	if err != nil {
		t.Fatalf("create webhook: %v", err)
	}

	// Seed two deliveries: one failed attempt, one success. The second row
	// (created later) is the "latest" and must sort first (created_at DESC).
	if err := whs.RecordDelivery(ctx, wh.ID, "sale.paid", `{"ok":1}`, 500, false, 1, "boom"); err != nil {
		t.Fatalf("record failed delivery: %v", err)
	}
	if err := whs.RecordDelivery(ctx, wh.ID, "sale.paid", `{"ok":1}`, 200, true, 2, ""); err != nil {
		t.Fatalf("record success delivery: %v", err)
	}

	h := newBundleHandlers(pool)
	raw := bundleAPIKey(t, ctx, pool, accountID)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	send := func(path string, code int) []map[string]any {
		t.Helper()
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer "+raw)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != code {
			t.Fatalf("%s status = %d, want %d (%s)", path, rec.Code, code, rec.Body.String())
		}
		var out []map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("%s bad json: %v (%s)", path, err, rec.Body.String())
		}
		return out
	}

	// Default limit returns both rows, newest first.
	rows := send("/v1/rest/webhooks/"+wh.ID+"/deliveries", http.StatusOK)
	if len(rows) != 2 {
		t.Fatalf("deliveries count = %d, want 2", len(rows))
	}
	first := rows[0]
	if first["success"] != true {
		t.Errorf("first (latest) delivery success = %v, want true", first["success"])
	}
	if first["attempt"].(float64) != 2 {
		t.Errorf("latest attempt = %v, want 2", first["attempt"])
	}
	if first["status_code"].(float64) != 200 {
		t.Errorf("latest status_code = %v, want 200", first["status_code"])
	}
	// id + event are surfaced so the UI can render history rows.
	if _, ok := first["id"].(string); !ok {
		t.Errorf("delivery missing id: %v", first)
	}
	if first["event"] != "sale.paid" {
		t.Errorf("delivery event = %v, want sale.paid", first["event"])
	}
	failed := rows[1]
	if failed["success"] != false {
		t.Errorf("older delivery success = %v, want false", failed["success"])
	}
	if failed["error"] != "boom" {
		t.Errorf("failed delivery error = %v, want boom", failed["error"])
	}

	// limit bounds the window.
	rows = send("/v1/rest/webhooks/"+wh.ID+"/deliveries?limit=1", http.StatusOK)
	if len(rows) != 1 {
		t.Fatalf("limit=1 returned %d rows, want 1", len(rows))
	}

	// Account scoping: a different account's key must not see these deliveries.
	otherID, _ := bundleFixture(t, ctx, pool)
	otherRaw := bundleAPIKey(t, ctx, pool, otherID)
	req := httptest.NewRequest("GET", "/v1/rest/webhooks/"+wh.ID+"/deliveries", nil)
	req.Header.Set("Authorization", "Bearer "+otherRaw)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var out []map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out) != 0 {
		t.Fatalf("other account saw %d deliveries, want 0 (account scoping failed)", len(out))
	}
}
