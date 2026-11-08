package httpapi

// Card t_06851d88: the REST write path for a pay-what-you-want product.
//
// The dashboard and the MCP tools both write through these columns, so the
// limits must be validated HERE (not only in the buyer's checkout): a product
// the merchant cannot price is a product whose form 400s every gift.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

func productRequest(t *testing.T, h *RESTHandlers, rawKey, method, path, body string) (int, map[string]any) {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+rawKey)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var parsed map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &parsed)
	return rec.Code, parsed
}

func TestRESTCreateCustomAmountProduct(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, _ := bundleFixture(t, ctx, pool)
	h := newBundleHandlers(pool)
	raw := bundleAPIKey(t, ctx, pool, accountID)

	code, resp := productRequest(t, h, raw, http.MethodPost, "/v1/rest/products",
		`{"name":"Derma Masjid","type":"custom_amount","min_price_sen":500,`+
			`"max_price_sen":50000,"suggested_price_sen":1000}`)
	if code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("POST status = %d, want 2xx: %v", code, resp)
	}
	if resp["type"] != dbstore.SaleTypeCustomAmount {
		t.Errorf("type = %v, want %q", resp["type"], dbstore.SaleTypeCustomAmount)
	}
	// price_sen is derived (the suggestion), never the client's price.
	if got, _ := resp["price_sen"].(float64); int64(got) != 1000 {
		t.Errorf("price_sen = %v, want the suggested 1000", resp["price_sen"])
	}
	if got, _ := resp["min_price_sen"].(float64); int64(got) != 500 {
		t.Errorf("min_price_sen = %v, want 500", resp["min_price_sen"])
	}

	// The stored row must be readable and priced by the checkout too.
	id, _ := resp["id"].(string)
	stored, err := dbstore.NewProductStore(pool).Get(ctx, accountID, id)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if stored.Type != dbstore.SaleTypeCustomAmount || stored.MinPriceSen != 500 ||
		stored.MaxPriceSen == nil || *stored.MaxPriceSen != 50000 {
		t.Errorf("stored = %+v, want custom_amount 500..50000", stored)
	}
}

func TestRESTCreateCustomAmountRefusals(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, _ := bundleFixture(t, ctx, pool)
	h := newBundleHandlers(pool)
	raw := bundleAPIKey(t, ctx, pool, accountID)

	cases := []struct {
		name string
		body string
		want string
	}{
		{"missing minimum", `{"name":"D","type":"custom_amount"}`, "min_price_sen"},
		{"below the platform floor", `{"name":"D","type":"custom_amount","min_price_sen":50}`, "at least 100 sen"},
		{"maximum below minimum", `{"name":"D","type":"custom_amount","min_price_sen":1000,"max_price_sen":999}`, "max_price_sen"},
		{"suggestion above maximum", `{"name":"D","type":"custom_amount","min_price_sen":500,"max_price_sen":1000,"suggested_price_sen":1001}`, "suggested_price_sen"},
		{"limits on a fixed-price product", `{"name":"D","type":"one_time","price_sen":1000,"min_price_sen":500}`, "only to type custom_amount"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, resp := productRequest(t, h, raw, http.MethodPost, "/v1/rest/products", tc.body)
			if code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %v", code, resp)
			}
			msg, _ := resp["error"].(string)
			if !strings.Contains(strings.ToLower(msg), strings.ToLower(tc.want)) {
				t.Errorf("error = %q, want it to mention %q", msg, tc.want)
			}
		})
	}
}

// TestRESTUpdateSwitchesToCustomAmount covers the merged-triple rule: a partial
// update cannot be judged on its own, so the stored row is read and the write
// carries the whole triple plus the derived display price.
func TestRESTUpdateSwitchesToCustomAmount(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, _ := bundleFixture(t, ctx, pool)
	h := newBundleHandlers(pool)
	raw := bundleAPIKey(t, ctx, pool, accountID)

	prod := &dbstore.Product{AccountID: accountID, Name: "Derma", PriceSen: 1000, Type: dbstore.SaleTypeOneTime, Active: true}
	if err := dbstore.NewProductStore(pool).Create(ctx, prod); err != nil {
		t.Fatalf("create: %v", err)
	}
	path := "/v1/rest/products/" + prod.ID

	// Switch to custom_amount with only a minimum: the derived price is the floor.
	code, resp := productRequest(t, h, raw, http.MethodPut, path,
		`{"type":"custom_amount","min_price_sen":800}`)
	if code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200: %v", code, resp)
	}
	stored, err := dbstore.NewProductStore(pool).Get(ctx, accountID, prod.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if stored.Type != dbstore.SaleTypeCustomAmount || stored.MinPriceSen != 800 {
		t.Fatalf("stored type/min = %q/%d, want custom_amount/800", stored.Type, stored.MinPriceSen)
	}
	if stored.PriceSen != 800 {
		t.Errorf("price_sen = %d, want the derived floor 800", stored.PriceSen)
	}

	// Raising the minimum above an existing suggestion must be refused as a
	// MERGED triple, not silently stored.
	if code, resp := productRequest(t, h, raw, http.MethodPut, path,
		`{"suggested_price_sen":1200}`); code != http.StatusOK {
		t.Fatalf("set suggestion: status = %d: %v", code, resp)
	}
	code, resp = productRequest(t, h, raw, http.MethodPut, path, `{"min_price_sen":2000}`)
	if code != http.StatusBadRequest {
		t.Fatalf("PUT min above suggestion: status = %d, want 400: %v", code, resp)
	}
	if msg, _ := resp["error"].(string); !strings.Contains(msg, "suggested_price_sen") {
		t.Errorf("error = %q, want it to name suggested_price_sen", msg)
	}
}
