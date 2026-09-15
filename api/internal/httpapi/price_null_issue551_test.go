package httpapi

// Issue #551: a non-finite price must not silently create a zero-priced product.
//
// The web UI computes `Math.round(parseFloat(price) * 100)`, which is Infinity for
// an input like "1e999"; JSON.stringify serialises a non-finite number as null. The
// handler typed price_sen as a plain int64, and Go's encoding/json decodes null into
// a non-pointer int64 as 0 WITH NO ERROR — so the `price_sen < 0` guard passed and
// the product was created at RM 0.00.
//
// These tests drive the REAL authed REST handler and FAIL against the pre-fix code.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func postProduct(t *testing.T, h *RESTHandlers, rawKey, body string) (int, map[string]any) {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, "/v1/rest/products", bytes.NewReader([]byte(body)))
	req.Header.Set("Authorization", "Bearer "+rawKey)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var parsed map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &parsed)
	return rec.Code, parsed
}

// TestIssue551NullPriceIsRejected is the core guard.
func TestIssue551NullPriceIsRejected(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, _ := bundleFixture(t, ctx, pool)
	h := newBundleHandlers(pool)
	raw := bundleAPIKey(t, ctx, pool, accountID)

	// Exactly what the UI sends for a non-finite price: JSON.stringify(NaN) === null.
	code, body := postProduct(t, h, raw, `{"name":"I551 null","price_sen":null,"type":"one_time"}`)
	if code == http.StatusOK || code == http.StatusCreated {
		t.Errorf("issue #551: a null price_sen was ACCEPTED with %d and created a product "+
			"(body %v) — null decodes to 0 in a plain int64, so a non-finite price silently "+
			"became a RM 0.00 product that can be sold and charged at zero", code, body)
	}

	// A missing price must be rejected too.
	code, body = postProduct(t, h, raw, `{"name":"I551 missing","type":"one_time"}`)
	if code == http.StatusOK || code == http.StatusCreated {
		t.Errorf("issue #551: a MISSING price_sen was accepted with %d (body %v)", code, body)
	}
}

// TestIssue551ZeroPriceStillAllowed proves the fix did not break a legitimate free
// product (0 is a real value and must stay valid).
func TestIssue551ZeroPriceStillAllowed(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, _ := bundleFixture(t, ctx, pool)
	h := newBundleHandlers(pool)
	raw := bundleAPIKey(t, ctx, pool, accountID)

	code, body := postProduct(t, h, raw, `{"name":"I551 free","price_sen":0,"type":"one_time"}`)
	if code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("issue #551: an explicit zero price must remain valid (a free product), got %d (%v)", code, body)
	}
	if got, ok := body["price_sen"].(float64); !ok || got != 0 {
		t.Errorf("price_sen = %v, want 0", body["price_sen"])
	}
}

// TestIssue551NormalPriceIsStoredUnchanged pins the happy path so the pointer change
// did not alter how a real price is stored.
func TestIssue551NormalPriceIsStoredUnchanged(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, _ := bundleFixture(t, ctx, pool)
	h := newBundleHandlers(pool)
	raw := bundleAPIKey(t, ctx, pool, accountID)

	code, body := postProduct(t, h, raw, `{"name":"I551 normal","price_sen":2500,"type":"one_time"}`)
	if code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("create failed: %d (%v)", code, body)
	}
	if got, ok := body["price_sen"].(float64); !ok || got != 2500 {
		t.Errorf("issue #551: price_sen = %v, want 2500 (RM 25.00 must be stored as 2500 sen)", body["price_sen"])
	}
}

// TestIssue551NullInt64DecodesToZero documents the underlying Go behaviour that made
// this a silent bug, so the reason for the pointer type is recorded executably.
func TestIssue551NullInt64DecodesToZero(t *testing.T) {
	var plain struct {
		PriceSen int64 `json:"price_sen"`
	}
	if err := json.Unmarshal([]byte(`{"price_sen":null}`), &plain); err != nil {
		t.Fatalf("expected the documented silent decode, got error %v", err)
	}
	if plain.PriceSen != 0 {
		t.Fatalf("price_sen = %d, want 0 (this is the silent behaviour being guarded)", plain.PriceSen)
	}

	// With a pointer, null is distinguishable, which is why the handler uses one.
	var ptr struct {
		PriceSen *int64 `json:"price_sen"`
	}
	if err := json.Unmarshal([]byte(`{"price_sen":null}`), &ptr); err != nil {
		t.Fatalf("unmarshal into pointer: %v", err)
	}
	if ptr.PriceSen != nil {
		t.Errorf("a null price must decode to a nil pointer so it can be rejected, got %v", *ptr.PriceSen)
	}
}
