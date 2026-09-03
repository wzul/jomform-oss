package httpapi

// Issue #153: buyer-facing variation selector — the submit path must accept a
// chosen variation_id, validate it belongs to the product, use the variation's
// price, decrement the variation's stock (not the product-level stock), and
// persist the variation on the sale. Integration test; skipped unless
// JOMFORM_TEST_DATABASE_URL is set.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// variationSubmitFixture seeds a user + space + account + published form with a
// variable product (two variations) and returns a PublicHandlers wired with the
// variation store, plus the product id and the limited-variation id.
func variationSubmitFixture(t *testing.T) (*PublicHandlers, string, string, string, string) {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)

	var userID, spaceSlug, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO spaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-space-' || md5(random()::text), 'it-' || md5(random()::text))
		 RETURNING slug`, userID).Scan(&spaceSlug); err != nil {
		t.Fatalf("create space: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, space_id)
		 SELECT $1, 'it-acc-' || md5(random()::text), 'it-' || md5(random()::text), id
		 FROM spaces WHERE slug = $2
		 RETURNING id`, userID, spaceSlug).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	// A variable product with two variations (one limited stock 1, one unlimited).
	prod := &dbstore.Product{
		AccountID: accountID, Name: "Tee", PriceSen: 1000, Type: dbstore.SaleTypeOneTime, Active: true,
	}
	if err := dbstore.NewProductStore(pool).Create(ctx, prod); err != nil {
		t.Fatalf("create product: %v", err)
	}
	vs := dbstore.NewProductVariationStore(pool)
	stockOne := 1
	if err := vs.ReplaceAll(ctx, prod.ID, []*dbstore.ProductVariation{
		{Name: "Size M", PriceSen: 1200, Stock: &stockOne},
		{Name: "Size L", PriceSen: 1300, Stock: nil},
	}); err != nil {
		t.Fatalf("replace variations: %v", err)
	}
	all, err := vs.ListByProduct(ctx, prod.ID)
	if err != nil || len(all) != 2 {
		t.Fatalf("list variations: %v (n=%d)", err, len(all))
	}
	var limID string
	for _, v := range all {
		if v.Name == "Size M" {
			limID = v.ID
		}
	}

	// Published form whose config carries the product (name + price).
	fs := dbstore.NewFormStore(pool)
	formSlug := "it-var-" + accountID[:8]
	f := &dbstore.Form{
		AccountID: accountID, Slug: formSlug, Title: "Var form", Status: "published",
		Config: map[string]any{
			"blocks": []any{map[string]any{"type": "product", "product_id": prod.ID}},
			"products": []any{map[string]any{"id": prod.ID, "name": "Tee", "price_sen": 1000}},
		},
		Theme: map[string]any{"primary": "#5e6ad2"},
	}
	if err := fs.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}

	h := &PublicHandlers{
		Forms:             fs,
		Sales:             dbstore.NewSaleStore(pool),
		Accounts:          dbstore.NewAccountStore(pool),
		Products:          dbstore.NewProductStore(pool),
		ProductVariations: vs,
		Spaces:            dbstore.NewSpaceStore(pool),
		BaseURL:           "http://localhost:8080",
	}
	return h, spaceSlug, formSlug, prod.ID, limID
}

// variationSubmit posts an order to the public mux and returns the response.
func variationSubmit(t *testing.T, h *PublicHandlers, host, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Host = host
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestVariationSubmitUsesVariationPriceAndStock(t *testing.T) {
	h, spaceSlug, formSlug, prodID, limID := variationSubmitFixture(t)
	host := spaceSlug + ".jomform.com"
	path := "/" + formSlug + "/order"
	body := `{"client":{"full_name":"Ali","email":"ali@example.test"},"items":[{"product_id":"` + prodID + `","variation_id":"` + limID + `","quantity":1}],"payment_method":"bank_direct"}`

	rec := variationSubmit(t, h, host, path, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}

	// The sale must record the variation and use the variation's price.
	var resp struct {
		RefCode string `json:"ref_code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	sale, err := h.Sales.ByRefCode(context.Background(), resp.RefCode)
	if err != nil {
		t.Fatalf("fetch sale: %v", err)
	}
	if len(sale.Items) != 1 {
		t.Fatalf("sale items = %d, want 1", len(sale.Items))
	}
	it := sale.Items[0]
	if it.VariationID != limID {
		t.Errorf("sale item variation_id = %q, want %q", it.VariationID, limID)
	}
	if it.VariationName != "Size M" {
		t.Errorf("sale item variation_name = %q, want Size M", it.VariationName)
	}
	if it.PriceSen != 1200 {
		t.Errorf("sale item price_sen = %d, want 1200 (variation price)", it.PriceSen)
	}
	if sale.TotalSen != 1200 {
		t.Errorf("sale total_sen = %d, want 1200", sale.TotalSen)
	}

	// The variation stock must have decremented (1 -> 0).
	got, err := h.ProductVariations.GetByProduct(context.Background(), prodID, limID)
	if err != nil {
		t.Fatalf("get variation: %v", err)
	}
	if got.Stock == nil || *got.Stock != 0 {
		t.Errorf("variation stock = %v, want 0", got.Stock)
	}

	// A second submit of the same (now sold-out) variation must be rejected.
	rec2 := variationSubmit(t, h, host, path, body)
	if rec2.Code != http.StatusConflict {
		t.Fatalf("second submit status = %d, want 409 (sold out)", rec2.Code)
	}
}

func TestVariationSubmitRejectsForeignVariation(t *testing.T) {
	h, spaceSlug, formSlug, prodID, _ := variationSubmitFixture(t)
	host := spaceSlug + ".jomform.com"
	path := "/" + formSlug + "/order"

	// A variation id that does not belong to the product must be rejected.
	body := `{"client":{"full_name":"Ali","email":"ali@example.test"},"items":[{"product_id":"` + prodID + `","variation_id":"00000000-0000-0000-0000-000000000000","quantity":1}],"payment_method":"bank_direct"}`
	rec := variationSubmit(t, h, host, path, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("foreign variation status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}
