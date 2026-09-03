package httpapi

// Issue #124: PUT /v1/rest/products/{id} must persist ALL editable fields
// (name, description, price_sen, images[], trial_days, type, billing_period,
// stock, active) with validation, and return the updated product. Integration
// against a real Postgres; skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

func productPut(t *testing.T, h *RESTHandlers, rawKey, path, body string) (int, map[string]any) {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest("PUT", path, bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+rawKey)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var parsed map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &parsed)
	return rec.Code, parsed
}

func TestProductPutPersistsAllEditableFields(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, _ := bundleFixture(t, ctx, pool)

	// Seed a subscription product so we can mutate every field.
	prod := &dbstore.Product{
		AccountID: accountID, Name: "Start", Description: "start desc", PriceSen: 1000,
		Images: []string{"https://a/old.png"}, Type: dbstore.SaleTypeSubscription,
		BillingPeriod: "month", TrialDays: 7, Active: true, Stock: intPtr5(),
	}
	products := dbstore.NewProductStore(pool)
	if err := products.Create(ctx, prod); err != nil {
		t.Fatalf("create product: %v", err)
	}

	h := newBundleHandlers(pool)
	raw := bundleAPIKey(t, ctx, pool, accountID)
	path := "/v1/rest/products/" + prod.ID

	// Full update of every editable field.
	body := `{
		"name":"Updated Tee","description":"A nice tee","price_sen":2500,
		"images":["https://a/i1.png","https://a/i2.png"],"trial_days":30,
		"type":"subscription","billing_period":"year","stock":3,"active":false
	}`
	code, resp := productPut(t, h, raw, path, body)
	if code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200 (body=%v)", code, resp)
	}
	if resp["id"] != prod.ID {
		t.Fatalf("PUT response missing id: %v", resp)
	}
	// Verify the returned updated product matches.
	if n, _ := resp["name"].(string); n != "Updated Tee" {
		t.Errorf("resp.name = %q, want Updated Tee", n)
	}
	if d, _ := resp["description"].(string); d != "A nice tee" {
		t.Errorf("resp.description = %q, want 'A nice tee'", d)
	}
	if price, _ := resp["price_sen"].(float64); int64(price) != 2500 {
		t.Errorf("resp.price_sen = %v, want 2500", price)
	}
	if trial, _ := resp["trial_days"].(float64); int(trial) != 30 {
		t.Errorf("resp.trial_days = %v, want 30", trial)
	}
	if typ, _ := resp["type"].(string); typ != "subscription" {
		t.Errorf("resp.type = %q, want subscription", typ)
	}
	if bp, _ := resp["billing_period"].(string); bp != "year" {
		t.Errorf("resp.billing_period = %q, want year", bp)
	}
	if active, _ := resp["active"].(bool); active {
		t.Errorf("resp.active = true, want false")
	}
	if stock, _ := resp["stock"].(float64); int(stock) != 3 {
		t.Errorf("resp.stock = %v, want 3", stock)
	}
	if imgs, ok := resp["images"].([]any); !ok || len(imgs) != 2 {
		t.Errorf("resp.images = %v, want 2 images", resp["images"])
	}

	// Verify the DB actually persisted via the store.
	got, err := products.Get(ctx, accountID, prod.ID)
	if err != nil {
		t.Fatalf("get product: %v", err)
	}
	if got.Name != "Updated Tee" || got.Description != "A nice tee" || got.PriceSen != 2500 {
		t.Errorf("db mismatch: %+v", got)
	}
	if len(got.Images) != 2 || got.BillingPeriod != "year" || got.TrialDays != 30 || *got.Stock != 3 {
		t.Errorf("db mismatch detail: %+v", got)
	}
	if got.Active != false {
		t.Errorf("db active = %v, want false", got.Active)
	}
}

func TestProductPutValidation(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, _ := bundleFixture(t, ctx, pool)
	prod := &dbstore.Product{
		AccountID: accountID, Name: "V", PriceSen: 500, Type: dbstore.SaleTypeOneTime,
	}
	if err := dbstore.NewProductStore(pool).Create(ctx, prod); err != nil {
		t.Fatalf("create product: %v", err)
	}
	h := newBundleHandlers(pool)
	raw := bundleAPIKey(t, ctx, pool, accountID)
	path := "/v1/rest/products/" + prod.ID

	cases := []struct {
		name string
		body string
		want int
	}{
		{"empty name", `{"name":""}`, http.StatusBadRequest},
		{"negative price", `{"price_sen":-1}`, http.StatusBadRequest},
		{"bad type", `{"type":"bogus"}`, http.StatusBadRequest},
		{"bad period", `{"type":"subscription","billing_period":"week"}`, http.StatusBadRequest},
		{"negative trial", `{"trial_days":-1}`, http.StatusBadRequest},
		{"negative stock", `{"stock":-3}`, http.StatusBadRequest},
	}
	for _, c := range cases {
		code, resp := productPut(t, h, raw, path, c.body)
		if code != c.want {
			t.Errorf("%s: status = %d (want %d), body=%v", c.name, code, c.want, resp)
		}
	}
}

func intPtr5() *int {
	v := 5
	return &v
}

// Issue #151: POST/PUT /v1/rest/products persist variations wholesale and
// GET /v1/rest/products/{id} returns them for the editor.
func TestProductVariationsPersistAndReturn(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, _ := bundleFixture(t, ctx, pool)

	h := newBundleHandlers(pool)
	raw := bundleAPIKey(t, ctx, pool, accountID)

	// Use a direct POST for create.
	postReq := httptest.NewRequest("POST", "/v1/rest/products", bytes.NewBufferString(`{
		"name":"Tee","price_sen":1000,"type":"one_time",
		"variations":[
			{"name":"Size M","price_sen":1200,"stock":5,"images":["https://a/m.png"]},
			{"name":"Size L","price_sen":1300,"stock":null,"images":["https://a/l.png"]}
		]
	}`))
	postReq.Header.Set("Authorization", "Bearer "+raw)
	postReq.Header.Set("Content-Type", "application/json")
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, postReq)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST status = %d, want 201 (body=%s)", rec.Code, rec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("parse create resp: %v", err)
	}
	pid, _ := created["id"].(string)
	if pid == "" {
		t.Fatalf("create resp missing id: %v", created)
	}
	if vs, ok := created["variations"].([]any); !ok || len(vs) != 2 {
		t.Fatalf("create resp variations = %v, want 2", created["variations"])
	}

	// GET returns the variations for the editor.
	code, got := bundleGet(t, h, raw, "/v1/rest/products/"+pid)
	if code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", code)
	}
	vs, ok := got["variations"].([]any)
	if !ok || len(vs) != 2 {
		t.Fatalf("GET variations = %v, want 2", got["variations"])
	}
	first := vs[0].(map[string]any)
	if first["name"] != "Size M" || int64(first["price_sen"].(float64)) != 1200 {
		t.Errorf("first variation = %v, want Size M @1200", first)
	}

	// PUT replaces variations wholesale (switch to a single variation).
	putBody := `{"variations":[{"name":"One Size","price_sen":1500,"stock":2,"images":["https://a/one.png"]}]}`
	code, resp := productPut(t, h, raw, "/v1/rest/products/"+pid, putBody)
	if code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200 (body=%v)", code, resp)
	}
	if vs, ok := resp["variations"].([]any); !ok || len(vs) != 1 {
		t.Fatalf("PUT resp variations = %v, want 1", resp["variations"])
	}

	// Verify via GET.
	_, got2 := bundleGet(t, h, raw, "/v1/rest/products/"+pid)
	vs2, _ := got2["variations"].([]any)
	if len(vs2) != 1 {
		t.Fatalf("GET after PUT variations = %v, want 1", got2["variations"])
	}
	if v := vs2[0].(map[string]any); v["name"] != "One Size" || int64(v["price_sen"].(float64)) != 1500 {
		t.Errorf("variation after PUT = %v, want One Size @1500", v)
	}

	// PUT with empty variations clears them (switch back to simple).
	code, _ = productPut(t, h, raw, "/v1/rest/products/"+pid, `{"variations":[]}`)
	if code != http.StatusOK {
		t.Fatalf("PUT clear status = %d, want 200", code)
	}
	_, got3 := bundleGet(t, h, raw, "/v1/rest/products/"+pid)
	if vs3, ok := got3["variations"].([]any); ok && len(vs3) != 0 {
		t.Fatalf("GET after clear variations = %v, want empty", got3["variations"])
	}
}
