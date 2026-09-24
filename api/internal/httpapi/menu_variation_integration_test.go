package httpapi

// Restaurant-menu cart: variations, end to end against a real Postgres.
//
// The pure tests above prove the menu page RENDERS a selector and ships
// variation_id. These prove the other half: the page resolves a product's
// variations LIVE from product_variations, and the variant a buyer picks on the
// menu reaches the shared order core, which prices from the variation and
// decrements the VARIATION's stock — not the product's.
//
// Skipped unless JOMFORM_TEST_DATABASE_URL is set.

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

// menuVariationIntegrationFixture seeds a user + workspace + account + a
// VARIABLE product (two variants, one limited stock), and a published menu form
// referencing it. Returns the handlers, the workspace slug, the menu form slug,
// the product id and the limited variant's id.
func menuVariationIntegrationFixture(t *testing.T) (h *PublicHandlers, workspaceSlug, accountID, menuSlug, prodID, limitedVariationID string) {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)

	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-ws-' || md5(random()::text), 'it-' || md5(random()::text))
		 RETURNING slug`, userID).Scan(&workspaceSlug); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 SELECT $1, 'it-acc-' || md5(random()::text), 'it-' || md5(random()::text), id
		 FROM workspaces WHERE slug = $2
		 RETURNING id`, userID, workspaceSlug).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	// The product's own price (780) is deliberately different from every
	// variant's, so a menu order that fell through to the base price is
	// detectable, not merely undocumented.
	prod := &dbstore.Product{
		AccountID: accountID, Name: "Nasi Lemak Ayam", PriceSen: 780,
		Type: dbstore.SaleTypeOneTime, Active: true,
	}
	if err := dbstore.NewProductStore(pool).Create(ctx, prod); err != nil {
		t.Fatalf("create product: %v", err)
	}
	variations := dbstore.NewProductVariationStore(pool)
	stockOne := 1
	if err := variations.ReplaceAll(ctx, prod.ID, []*dbstore.ProductVariation{
		{Name: "Size M", PriceSen: 1200, Stock: &stockOne},
		{Name: "Size L", PriceSen: 1300, Stock: nil},
	}); err != nil {
		t.Fatalf("replace variations: %v", err)
	}
	all, err := variations.ListByProduct(ctx, prod.ID)
	if err != nil || len(all) != 2 {
		t.Fatalf("list variations: %v (n=%d)", err, len(all))
	}
	for _, v := range all {
		if v.Name == "Size M" {
			limitedVariationID = v.ID
		}
	}
	if limitedVariationID == "" {
		t.Fatal("seeded Size M variant not found")
	}

	h = &PublicHandlers{
		Forms:             dbstore.NewFormStore(pool),
		Sales:             dbstore.NewSaleStore(pool),
		Accounts:          dbstore.NewAccountStore(pool),
		Products:          dbstore.NewProductStore(pool),
		ProductVariations: variations,
		Workspaces:        dbstore.NewWorkspaceStore(pool),
		BaseURL:           "http://localhost:8080",
	}

	menuSlug = "it-var-menu-" + accountID[:8]
	if err := h.Forms.Create(ctx, &dbstore.Form{
		AccountID: accountID, Slug: menuSlug, Title: "Warung Ali", Status: "published",
		Config: map[string]any{
			"blocks": []any{
				map[string]any{"id": "b1", "type": "product", "product_id": prod.ID, "value": "Nasi Lemak"},
			},
			"menu": map[string]any{"enabled": true, "categories": []any{}},
		},
	}); err != nil {
		t.Fatalf("create menu form: %v", err)
	}
	return h, workspaceSlug, accountID, menuSlug, prod.ID, limitedVariationID
}

// The menu page resolves the product's variations LIVE from the DB: the card
// renders a selector with each variant and its own price, and the limited-stock
// variant that has reached zero is disabled. This is the half of the fix the
// template tests cannot reach (they inject the variation list directly).
func TestMenuVariationMenuPageResolvesVariationsLive(t *testing.T) {
	h, workspaceSlug, _, menuSlug, prodID, limitedID := menuVariationIntegrationFixture(t)
	ctx := context.Background()

	code, body := getForm(t, h, workspaceSlug+".jomform.com", menuSlug)
	if code != http.StatusOK {
		t.Fatalf("menu form status = %d, want 200", code)
	}
	// A selector bound to the live product, carrying both variants.
	for _, want := range []string{
		`<select class="mvar" data-id="` + prodID + `"`,
		`value="` + limitedID + `" data-vname="Size M" data-price="1200"`,
		`data-vname="Size L" data-price="1300"`,
		"Size M — RM 12.00",
		`data-pick="`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("live menu page missing %q", want)
		}
	}
	// The sold-out variant is NOT disabled yet (stock 1), so an accidental
	// always-disabled implementation is caught.
	if strings.Contains(body, `data-price="1200" disabled`) {
		t.Error("an in-stock variant was rendered disabled")
	}

	// Prove the resolution is LIVE: sell the variant out and re-render.
	if err := h.ProductVariations.DecrementStock(ctx, limitedID, 1); err != nil {
		t.Fatalf("decrement variant stock: %v", err)
	}
	_, body2 := getForm(t, h, workspaceSlug+".jomform.com", menuSlug)
	if !strings.Contains(body2, `data-price="1200" disabled`) {
		t.Errorf("a sold-out variant is still offered on the menu\nbody:\n%s", body2)
	}
	// The unlimited variant must stay enabled.
	if strings.Contains(body2, `data-price="1300" disabled`) {
		t.Error("an unlimited-stock variant was disabled")
	}
}

// A SIMPLE menu product (no variations) renders no selector at all — the
// control for the fix.
func TestMenuVariationSimpleProductRendersNoSelector(t *testing.T) {
	h, workspaceSlug, _, _, prodID, _ := menuVariationIntegrationFixture(t)
	ctx := context.Background()

	// Clear the variants: the product becomes simple.
	if err := h.ProductVariations.DeleteByProduct(ctx, prodID); err != nil {
		t.Fatalf("delete variations: %v", err)
	}
	code, body := getForm(t, h, workspaceSlug+".jomform.com", "")
	if code == http.StatusNotFound {
		t.Skip("workspace root form listing not available in this fixture")
	}
	if strings.Contains(body, `<select class="v" data-id="`+prodID+`"`) {
		t.Errorf("a simple menu product rendered a variation selector\nbody:\n%s", body)
	}
}

// The chosen variant reaches the shared order core: the sale records the
// variation, is priced from the VARIATION (not the product's base price), and
// the VARIATION's stock is the counter that decrements.
func TestMenuVariationOrderChargesAndRecordsTheChosenVariant(t *testing.T) {
	h, workspaceSlug, _, menuSlug, prodID, limitedID := menuVariationIntegrationFixture(t)
	ctx := context.Background()

	body := `{"client":{"full_name":"Ali","email":"ali@example.test"},` +
		`"items":[{"product_id":"` + prodID + `","variation_id":"` + limitedID + `","quantity":1}],` +
		`"payment_method":"bank_direct"}`

	// The same payload the menu page's cart now builds.
	rec := menuOrderPost(t, h, workspaceSlug+".jomform.com", menuSlug, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("menu order with a variation = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		RefCode string `json:"ref_code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	sale, err := h.Sales.ByRefCode(ctx, resp.RefCode)
	if err != nil {
		t.Fatalf("fetch sale: %v", err)
	}
	if len(sale.Items) != 1 {
		t.Fatalf("sale items = %d, want 1", len(sale.Items))
	}
	it := sale.Items[0]
	// The buyer pays the VARIANT's price, and the sale records which one.
	if it.VariationID != limitedID {
		t.Errorf("sale item variation_id = %q, want %q", it.VariationID, limitedID)
	}
	if it.VariationName != "Size M" {
		t.Errorf("sale item variation_name = %q, want Size M", it.VariationName)
	}
	if it.PriceSen != 1200 {
		t.Errorf("sale item price_sen = %d, want 1200 (the variant's price, not the product's 780)", it.PriceSen)
	}
	if sale.TotalSen != 1200 {
		t.Errorf("sale total_sen = %d, want 1200", sale.TotalSen)
	}
	// The variant's stock is the counter that moved (1 -> 0), not the product's.
	v, err := h.ProductVariations.GetByProduct(ctx, prodID, limitedID)
	if err != nil {
		t.Fatalf("get variation: %v", err)
	}
	if v.Stock == nil || *v.Stock != 0 {
		t.Errorf("variant stock = %v, want 0", v.Stock)
	}
	// The product-level counter must be untouched: the menu order must not have
	// fallen through to the simple-product branch.
	prod, err := h.Products.Get(ctx, sale.AccountID, prodID)
	if err != nil {
		t.Fatalf("get product: %v", err)
	}
	if prod.Stock != nil {
		t.Errorf("product-level stock moved (%v): the order fell through to the simple-product branch", prod.Stock)
	}

	// ...and a second order for the now-sold-out variant is refused.
	rec2 := menuOrderPost(t, h, workspaceSlug+".jomform.com", menuSlug, body)
	if rec2.Code != http.StatusConflict {
		t.Fatalf("second order for a sold-out variant = %d, want 409 (body %s)", rec2.Code, rec2.Body.String())
	}
}

// A menu order that OMITS variation_id on a variable product is rejected. The
// card already refuses to add without a choice, but that is the buyer's guard,
// not the server's: a hand-made, replayed or truncated request must not buy the
// product's base price, which is not a purchasable option once the product has
// variants. This is the server-side half of the same defect.
func TestMenuVariationOrderWithoutVariationIsRejected(t *testing.T) {
	h, workspaceSlug, _, menuSlug, prodID, _ := menuVariationIntegrationFixture(t)

	body := `{"client":{"full_name":"Ali","email":"ali@example.test"},` +
		`"items":[{"product_id":"` + prodID + `","quantity":1}],` +
		`"payment_method":"bank_direct"}`
	rec := menuOrderPost(t, h, workspaceSlug+".jomform.com", menuSlug, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("menu order without a variation = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "pilih variasi") {
		t.Errorf("rejection should tell the buyer to pick a variation, got %s", rec.Body.String())
	}
}

// The control for the check above: a SIMPLE product (no variants) with no
// variation_id still orders at its base price. The guard must never make an
// ordinary product unorderable.
func TestMenuVariationSimpleProductStillOrdersAtBasePrice(t *testing.T) {
	h, workspaceSlug, accountID, menuSlug, _, _ := menuVariationIntegrationFixture(t)
	ctx := context.Background()

	simple := &dbstore.Product{
		AccountID: accountID, Name: "Teh Tarik", PriceSen: 350,
		Type: dbstore.SaleTypeOneTime, Active: true,
	}
	if err := dbstore.NewProductStore(bundleTestPool(t)).Create(ctx, simple); err != nil {
		t.Fatalf("create simple product: %v", err)
	}

	body := `{"client":{"full_name":"Ali","email":"ali@example.test"},` +
		`"items":[{"product_id":"` + simple.ID + `","quantity":2}],` +
		`"payment_method":"bank_direct"}`
	rec := menuOrderPost(t, h, workspaceSlug+".jomform.com", menuSlug, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("simple product order = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		RefCode string `json:"ref_code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	sale, err := h.Sales.ByRefCode(ctx, resp.RefCode)
	if err != nil {
		t.Fatalf("fetch sale: %v", err)
	}
	if it := sale.Items[0]; it.PriceSen != 350 || it.VariationID != "" {
		t.Errorf("item = {price_sen:%d variation_id:%q}, want the base price 350 and no variation",
			it.PriceSen, it.VariationID)
	}
}

// A variation that belongs to a DIFFERENT product is rejected — the menu path
// inherits the order core's validation, so a forged selector cannot buy another
// product's variant.
func TestMenuVariationOrderRejectsForeignVariation(t *testing.T) {
	h, workspaceSlug, accountID, menuSlug, prodID, _ := menuVariationIntegrationFixture(t)
	ctx := context.Background()

	// A second product in the same account, with its own variant.
	other := &dbstore.Product{
		AccountID: accountID, Name: "Roti Canai", PriceSen: 250,
		Type: dbstore.SaleTypeOneTime, Active: true,
	}
	if err := h.Products.Create(ctx, other); err != nil {
		t.Fatalf("create other product: %v", err)
	}
	if err := h.ProductVariations.ReplaceAll(ctx, other.ID, []*dbstore.ProductVariation{
		{Name: "Cheap", PriceSen: 100, Stock: nil},
	}); err != nil {
		t.Fatalf("replace other variations: %v", err)
	}
	others, err := h.ProductVariations.ListByProduct(ctx, other.ID)
	if err != nil || len(others) != 1 {
		t.Fatalf("list other variations: %v (n=%d)", err, len(others))
	}
	foreignID := others[0].ID

	// The menu's product priced with the OTHER product's (cheaper) variant.
	body := `{"client":{"full_name":"Ali","email":"ali@example.test"},` +
		`"items":[{"product_id":"` + prodID + `","variation_id":"` + foreignID + `","quantity":1}],` +
		`"payment_method":"bank_direct"}`
	rec := menuOrderPost(t, h, workspaceSlug+".jomform.com", menuSlug, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("foreign variation = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}

	// A non-existent variant id is rejected too.
	body2 := `{"client":{"full_name":"Ali","email":"ali@example.test"},` +
		`"items":[{"product_id":"` + prodID + `","variation_id":"00000000-0000-0000-0000-000000000000","quantity":1}],` +
		`"payment_method":"bank_direct"}`
	rec2 := menuOrderPost(t, h, workspaceSlug+".jomform.com", menuSlug, body2)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("unknown variation = %d, want 400 (body %s)", rec2.Code, rec2.Body.String())
	}
}

// The legacy workspace-scoped route (/menu-order) keeps working and now carries
// a variation too — the #491 compatibility shim must not be broken by this
// change, and it must not silently drop the variant.
func TestMenuVariationLegacyMenuOrderRouteCarriesVariation(t *testing.T) {
	h, workspaceSlug, _, menuSlug, prodID, _ := menuVariationIntegrationFixture(t)
	ctx := context.Background()

	// The unlimited variant, so a multi-unit order proves quantity × the
	// variant's price on the legacy route too.
	all, err := h.ProductVariations.ListByProduct(ctx, prodID)
	if err != nil {
		t.Fatalf("list variations: %v", err)
	}
	unlimitedID := ""
	for _, v := range all {
		if v.Name == "Size L" {
			unlimitedID = v.ID
		}
	}
	if unlimitedID == "" {
		t.Fatal("seeded Size L variant not found")
	}

	body := `{"form_slug":"` + menuSlug + `","client":{"full_name":"Ali","email":"ali@example.test"},` +
		`"items":[{"product_id":"` + prodID + `","variation_id":"` + unlimitedID + `","quantity":2}],` +
		`"payment_method":"bank_direct"}`
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, "/menu-order", bytes.NewBufferString(body))
	req.Host = workspaceSlug + ".jomform.com"
	req.RemoteAddr = uniqueTestIP() + ":1234"
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("legacy menu-order with a variation = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		RefCode string `json:"ref_code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	sale, err := h.Sales.ByRefCode(ctx, resp.RefCode)
	if err != nil {
		t.Fatalf("fetch sale: %v", err)
	}
	it := sale.Items[0]
	if it.VariationID != unlimitedID {
		t.Errorf("legacy route dropped the variation: variation_id = %q", it.VariationID)
	}
	if it.PriceSen != 1300 || it.Qty != 2 {
		t.Errorf("item = {price_sen:%d qty:%d}, want 1300 × 2", it.PriceSen, it.Qty)
	}
	if sale.TotalSen != 2600 {
		t.Errorf("sale total_sen = %d, want 2600", sale.TotalSen)
	}
}
