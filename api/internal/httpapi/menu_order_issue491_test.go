package httpapi

// Issue #491 (part 1 of 3) + Issue #497: restaurant-menu checkout.
//
// #491 introduced a workspace-scoped order endpoint so the menu could check out
// WITHOUT a form. #497 moved the menu onto the FORM, so the checkout now posts
// to the menu form's OWN order route (POST /{slug}/order) and the sale is
// attributed to that form. These tests pin the acceptance criteria:
//   - the menu form's route accepts items[] (with quantity), client{name,email},
//     optional fields{}, table_number, affiliate_code;
//   - missing name or email → 400 (CHIP needs both);
//   - prices resolve LIVE from the products table, subtotal = Σ(price × qty);
//   - anti-spam still applies (honeypot + timestamp + per-form rate limit);
//   - the optional attached order form's field contract is enforced when
//     present, and skipped when absent;
//   - the non-menu form path (handleSubmit) is unchanged: name still optional,
//     same sale.form_id;
//   - the endpoint returns a CHIP checkout URL.
//
// Integration tests run against the local test Postgres; skipped unless
// JOMFORM_TEST_DATABASE_URL is set. The rate-limit and route-shape tests are
// pure (no DB).

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wzul/jomform/api/internal/chip"
	"github.com/wzul/jomform/api/internal/cryptoutil"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/httpapi/middleware"
	"github.com/wzul/jomform/api/internal/payments"
)

// menuOrderFixture seeds a user + workspace + account + one live product AND a
// published menu form whose own product block references that product, then
// returns the handlers, the workspace slug, the account id, the product id and
// the menu form's public slug. Issue #497: the menu is a form, so the checkout
// target is a form URL.
func menuOrderFixture(t *testing.T) (h *PublicHandlers, workspaceSlug, accountID, prodID, menuSlug string) {
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

	// The live product row is the price authority (#461). Its DB price is
	// deliberately different from any snapshot the form would have carried.
	prod := &dbstore.Product{
		AccountID: accountID, Name: "Nasi Lemak Ayam Basmathi", PriceSen: 2100,
		Type: dbstore.SaleTypeOneTime, Active: true,
	}
	if err := dbstore.NewProductStore(pool).Create(ctx, prod); err != nil {
		t.Fatalf("create product: %v", err)
	}

	h = &PublicHandlers{
		Forms:             dbstore.NewFormStore(pool),
		Sales:             dbstore.NewSaleStore(pool),
		Accounts:          dbstore.NewAccountStore(pool),
		Products:          dbstore.NewProductStore(pool),
		ProductVariations: dbstore.NewProductVariationStore(pool),
		Workspaces:        dbstore.NewWorkspaceStore(pool),
		BaseURL:           "http://localhost:8080",
	}

	// The menu form: a product block referencing the live product + an enabled
	// per-form menu config.
	menuSlug = "it-497-menu-" + accountID[:8]
	mf := &dbstore.Form{
		AccountID: accountID, Slug: menuSlug, Title: "Warung Ali", Status: "published",
		Config: map[string]any{
			"blocks": []any{
				map[string]any{"id": "p1", "type": "product", "product_id": prod.ID, "value": "Nasi Lemak"},
			},
			"menu": map[string]any{"enabled": true, "categories": []any{}},
		},
	}
	if err := h.Forms.Create(ctx, mf); err != nil {
		t.Fatalf("create menu form: %v", err)
	}
	return h, workspaceSlug, accountID, prod.ID, menuSlug
}

// menuOrderPost posts a body to the menu form's own order route on the
// workspace's subdomain and returns the recorder.
func menuOrderPost(t *testing.T, h *PublicHandlers, host, menuSlug, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, "/"+menuSlug+"/order", bytes.NewBufferString(body))
	req.Host = host
	req.RemoteAddr = uniqueTestIP() + ":1234"
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestIssue491MenuOrderWithoutForm(t *testing.T) {
	h, workspaceSlug, _, prodID, menuSlug := menuOrderFixture(t)
	host := workspaceSlug + ".jomform.com"

	body := `{"client":{"full_name":"Ali","email":"ali@example.test"},` +
		`"items":[{"product_id":"` + prodID + `","quantity":1}],` +
		`"payment_method":"bank_direct","table_number":"8","affiliate_code":"AFF1"}`

	rec := menuOrderPost(t, h, host, menuSlug, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("menu order status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		RefCode string `json:"ref_code"`
		Status  string `json:"status"`
		Next    string `json:"next"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	if resp.RefCode == "" {
		t.Fatalf("menu order returned no ref_code (body %s)", rec.Body.String())
	}

	sale, err := h.Sales.ByRefCode(context.Background(), resp.RefCode)
	if err != nil {
		t.Fatalf("fetch sale: %v", err)
	}
	// Issue #497: the menu is a form, so the sale is attributed to it.
	if sale.FormID == "" {
		t.Errorf("sale.form_id = %q, want the menu form id (the menu is a form now)", sale.FormID)
	}
	// #489 / #444 wiring still works end-to-end.
	if sale.TableNumber != "8" {
		t.Errorf("sale.table_number = %q, want \"8\"", sale.TableNumber)
	}
	if sale.AffiliateCode != "AFF1" {
		t.Errorf("sale.affiliate_code = %q, want \"AFF1\"", sale.AffiliateCode)
	}
}

func TestIssue491MenuOrderRequiresNameAndEmail(t *testing.T) {
	h, workspaceSlug, _, prodID, menuSlug := menuOrderFixture(t)
	host := workspaceSlug + ".jomform.com"

	cases := []struct {
		name string
		body string
	}{
		{"missing name", `{"client":{"email":"ali@example.test"},"items":[{"product_id":"` + prodID + `","quantity":1}],"payment_method":"bank_direct"}`},
		{"blank name", `{"client":{"full_name":"   ","email":"ali@example.test"},"items":[{"product_id":"` + prodID + `","quantity":1}],"payment_method":"bank_direct"}`},
		{"missing email", `{"client":{"full_name":"Ali"},"items":[{"product_id":"` + prodID + `","quantity":1}],"payment_method":"bank_direct"}`},
		{"blank email", `{"client":{"full_name":"Ali","email":"  "},"items":[{"product_id":"` + prodID + `","quantity":1}],"payment_method":"bank_direct"}`},
		{"no items", `{"client":{"full_name":"Ali","email":"ali@example.test"},"items":[],"payment_method":"bank_direct"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := menuOrderPost(t, h, host, menuSlug, c.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("%s: status = %d, want 400 (body %s)", c.name, rec.Code, rec.Body.String())
			}
		})
	}
}

// TestIssue491MenuOrderResolvesLivePriceAndSumsQuantity asserts the menu
// checkout charges the CURRENT product price (not a form snapshot) and that
// subtotal = Σ(price × qty).
func TestIssue491MenuOrderResolvesLivePriceAndSumsQuantity(t *testing.T) {
	h, workspaceSlug, accountID, prodID, menuSlug := menuOrderFixture(t)
	host := workspaceSlug + ".jomform.com"

	// The product's live price is 2100. Re-price it to 1800 to prove the submit
	// path reads the products table rather than any cached snapshot.
	price := int64(1800)
	if err := h.Products.Update(context.Background(), accountID, prodID,
		nil, nil, &price, nil, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("re-price product: %v", err)
	}

	body := `{"client":{"full_name":"Ali","email":"ali@example.test"},` +
		`"items":[{"product_id":"` + prodID + `","quantity":3}],` +
		`"payment_method":"bank_direct"}`
	rec := menuOrderPost(t, h, host, menuSlug, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
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
	if it.PriceSen != 1800 {
		t.Errorf("item price_sen = %d, want 1800 (LIVE product price)", it.PriceSen)
	}
	if it.Qty != 3 {
		t.Errorf("item qty = %d, want 3", it.Qty)
	}
	if it.Name != "Nasi Lemak Ayam Basmathi" {
		t.Errorf("item name = %q, want the live product name", it.Name)
	}
	if sale.SubtotalSen != 5400 {
		t.Errorf("sale.subtotal_sen = %d, want 5400 (1800 × 3)", sale.SubtotalSen)
	}
	if sale.TotalSen != 5400 {
		t.Errorf("sale.total_sen = %d, want 5400", sale.TotalSen)
	}
}

// TestIssue491MenuOrderRejectsUnknownProduct guards the menu path against an
// unknown product id: with no live row and no snapshot there is nothing to
// fall back to, so the order is rejected rather than sold at zero.
func TestIssue491MenuOrderRejectsUnknownProduct(t *testing.T) {
	h, workspaceSlug, _, _, menuSlug := menuOrderFixture(t)
	host := workspaceSlug + ".jomform.com"
	body := `{"client":{"full_name":"Ali","email":"ali@example.test"},` +
		`"items":[{"product_id":"00000000-0000-0000-0000-000000000000","quantity":1}],` +
		`"payment_method":"bank_direct"}`
	rec := menuOrderPost(t, h, host, menuSlug, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestIssue491MenuOrderHoneypotStillApplies proves layer 1 survives the menu
// path: a filled honeypot field rejects the order.
func TestIssue491MenuOrderHoneypotStillApplies(t *testing.T) {
	h, workspaceSlug, _, prodID, menuSlug := menuOrderFixture(t)
	host := workspaceSlug + ".jomform.com"
	body := `{"client":{"full_name":"Ali","email":"ali@example.test"},` +
		`"items":[{"product_id":"` + prodID + `","quantity":1}],` +
		`"payment_method":"bank_direct","fields":{"` + antiSpamHoneypotField + `":"http://spam.example"}}`
	rec := menuOrderPost(t, h, host, menuSlug, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("filled honeypot status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestIssue491MenuOrderRateLimitStillApplies proves layer 2 survives the menu
// path, keyed per menu FORM. The handler is called directly so the per-IP
// middleware burst does not mask the result.
func TestIssue491MenuOrderRateLimitStillApplies(t *testing.T) {
	h, workspaceSlug, _, prodID, menuSlug := menuOrderFixture(t)
	h.FormLimiter = middleware.NewRateLimiter(0, 1) // one token, no refill

	body := `{"client":{"full_name":"Ali","email":"ali@example.test"},` +
		`"items":[{"product_id":"` + prodID + `","quantity":1}],` +
		`"payment_method":"bank_direct"}`
	ip := uniqueTestIP()
	post := func() int {
		req := httptest.NewRequest(http.MethodPost, "/"+menuSlug+"/order", bytes.NewBufferString(body))
		req.SetPathValue("slug", menuSlug)
		req.Host = workspaceSlug + ".jomform.com"
		req.RemoteAddr = ip + ":1234"
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.handleSubmit(rec, req)
		return rec.Code
	}
	if got := post(); got != http.StatusCreated {
		t.Fatalf("first menu order = %d, want 201 (body %s)", got, body)
	}
	if got := post(); got != http.StatusTooManyRequests {
		t.Fatalf("second menu order = %d, want 429 (per-form rate limit must apply)", got)
	}
}

// TestIssue491RateLimitKeyIsPerForm is the pure counterpart: the menu rate
// limit keys on the menu form, so one menu form's traffic never throttles
// another's, and a nil limiter / empty key never throttles.
func TestIssue491RateLimitKeyIsPerForm(t *testing.T) {
	h := &PublicHandlers{FormLimiter: middleware.NewRateLimiter(0, 1)}
	req := httptest.NewRequest(http.MethodPost, "/menu/order", nil)
	req.RemoteAddr = "203.0.113.9:1234"

	if h.antiSpamCheckRateLimitKey(req, "form-one") {
		t.Fatalf("first submit for form-one should be allowed")
	}
	if !h.antiSpamCheckRateLimitKey(req, "form-one") {
		t.Fatalf("second submit for form-one should be over the limit")
	}
	if h.antiSpamCheckRateLimitKey(req, "form-two") {
		t.Fatalf("a different menu form must not be throttled by form-one's counter")
	}
	// An empty key (unresolvable target) never throttles.
	if h.antiSpamCheckRateLimitKey(req, "") {
		t.Fatalf("empty key must never throttle")
	}
	if (&PublicHandlers{}).antiSpamCheckRateLimitKey(req, "form-one") {
		t.Fatalf("nil limiter must never throttle")
	}
}

// TestIssue491MenuOrderValidatesAttachedFormFields covers the part-3 design
// hook: when the merchant attaches a published form to the menu form for extra
// order info, the submitted fields are validated against that form's
// registered-field contract and persisted on the sale. With no form attached
// the field validation is skipped entirely.
func TestIssue491MenuOrderValidatesAttachedFormFields(t *testing.T) {
	h, workspaceSlug, accountID, prodID, menuSlug := menuOrderFixture(t)
	ctx := context.Background()
	host := workspaceSlug + ".jomform.com"

	// Published extra-info form with one required registered field.
	formSlug := "it-491-extra-" + accountID[:8]
	f := &dbstore.Form{
		AccountID: accountID, Slug: formSlug, Title: "Extra info", Status: "published",
		Config: map[string]any{
			"registered_fields": []any{
				map[string]any{"field_id": "notes", "type": "textarea", "label": "Nota", "required": true},
			},
		},
	}
	if err := h.Forms.Create(ctx, f); err != nil {
		t.Fatalf("create extra form: %v", err)
	}

	// No form attached yet → fields are not validated, the order goes through.
	base := `{"client":{"full_name":"Ali","email":"ali@example.test"},` +
		`"items":[{"product_id":"` + prodID + `","quantity":1}],` +
		`"payment_method":"bank_direct"`
	rec := menuOrderPost(t, h, host, menuSlug, base+`}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("unattached form: status = %d, want 201 (field validation must be skipped)", rec.Code)
	}

	// Attach the extra form to the MENU FORM's config (part 3 setting; JSON
	// blob on the form, no migration — issue #497).
	menuForm, err := h.Forms.ByAccountSlug(ctx, workspaceSlug, menuSlug)
	if err != nil || menuForm == nil {
		// ByAccountSlug resolves by account slug; fall back to a direct read.
		menuForm = findFormBySlug(t, h, accountID, menuSlug)
	}
	menuForm.Config = dbstore.WithMenu(menuForm.Config,
		&dbstore.MenuPage{Enabled: true, OrderFormSlug: formSlug})
	if err := h.Forms.Update(ctx, accountID, menuForm.ID, menuForm.Title, menuForm.Config, nil); err != nil {
		t.Fatalf("attach extra form: %v", err)
	}

	// Missing the required field → 400 from the attached form's contract.
	rec = menuOrderPost(t, h, host, menuSlug, base+`}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("attached form, missing required field: status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}

	// Providing the field → 201 and the values persist on the sale.
	rec = menuOrderPost(t, h, host, menuSlug, base+`,"fields":{"notes":"pedas sikit"}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("attached form, field supplied: status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
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
	fields, err := h.Sales.SaleFields(ctx, sale.ID)
	if err != nil {
		t.Fatalf("sale fields: %v", err)
	}
	if fields["notes"] != "pedas sikit" {
		t.Errorf("sale fields[notes] = %v, want \"pedas sikit\"", fields["notes"])
	}
	// The attached form is used for validation only — the sale is attributed to
	// the MENU form, never to the attached extra-info form.
	if sale.FormID != menuForm.ID {
		t.Errorf("sale.form_id = %q, want the menu form id %q", sale.FormID, menuForm.ID)
	}
}

// TestIssue491MenuOrderReturnsChipCheckoutURL asserts the endpoint returns a
// CHIP checkout URL the caller can redirect to, using a stub CHIP API.
func TestIssue491MenuOrderReturnsChipCheckoutURL(t *testing.T) {
	h, workspaceSlug, accountID, prodID, menuSlug := menuOrderFixture(t)
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "pur_menu_1", "status": "created",
			"checkout_url": "https://gate.example/checkout/pur_menu_1",
		})
	}))
	t.Cleanup(srv.Close)

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	sealed, err := cryptoutil.Encrypt(key, []byte("sk_test_menu_order"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	accounts := h.Accounts
	if err := accounts.SetChipCredentials(ctx, accountID, "br_menu", sealed, "test", "", nil, true); err != nil {
		t.Fatalf("set chip creds: %v", err)
	}
	pay := payments.New(chip.New(srv.URL), accounts, h.Sales, dbstore.NewSubscriptionStore(bundleTestPool(t)), key, "http://localhost:8080")
	pay.Workspaces = h.Workspaces
	h.Pay = pay

	body := `{"client":{"full_name":"Ali","email":"ali@example.test"},` +
		`"items":[{"product_id":"` + prodID + `","quantity":1}],` +
		`"payment_method":"chip"}`
	rec := menuOrderPost(t, h, workspaceSlug+".jomform.com", menuSlug, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("chip menu order status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		CheckoutURL string `json:"checkout_url"`
		Status      string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	if resp.CheckoutURL != "https://gate.example/checkout/pur_menu_1" {
		t.Fatalf("checkout_url = %q, want the CHIP checkout URL", resp.CheckoutURL)
	}
	if resp.Status != "redirect" {
		t.Fatalf("status = %q, want \"redirect\"", resp.Status)
	}
}

// TestIssue491FormPathUnchanged is the regression guard for existing forms:
// the shared core must not tighten the non-menu form path. A form order with an
// email but NO name still succeeds and records sale.form_id, exactly as before.
func TestIssue491FormPathUnchanged(t *testing.T) {
	h, workspaceSlug, accountID, prodID, _ := menuOrderFixture(t)
	ctx := context.Background()
	formSlug := "it-491-form-" + accountID[:8]
	f := &dbstore.Form{
		AccountID: accountID, Slug: formSlug, Title: "Legacy form", Status: "published",
		Config: map[string]any{
			"products": []any{map[string]any{"id": prodID, "name": "Nasi Lemak", "price_sen": 2100}},
		},
	}
	if err := h.Forms.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	// Email only, no name — the historical rule (name optional on the form path).
	body := `{"client":{"email":"ali@example.test"},` +
		`"items":[{"product_id":"` + prodID + `","quantity":2}],` +
		`"payment_method":"bank_direct"}`
	req := httptest.NewRequest(http.MethodPost, "/"+formSlug+"/order", bytes.NewBufferString(body))
	req.Host = workspaceSlug + ".jomform.com"
	req.RemoteAddr = uniqueTestIP() + ":1234"
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("form order status = %d, want 201 (name must stay optional on the form path; body %s)", rec.Code, rec.Body.String())
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
	if sale.FormID != f.ID {
		t.Errorf("sale.form_id = %q, want the form id %q", sale.FormID, f.ID)
	}
	if sale.SubtotalSen != 4200 {
		t.Errorf("sale.subtotal_sen = %d, want 4200 (2100 × 2)", sale.SubtotalSen)
	}
}

// TestIssue491MenuOrderRouteIsUnambiguous asserts a form slugged "menu-order"
// is not shadowed: the literal legacy route still resolves the menu FORM from
// the body's form_slug and rejects a non-menu form. Issue #497 kept the route
// as a compatibility shim.
func TestIssue491MenuOrderRouteIsUnambiguous(t *testing.T) {
	h, workspaceSlug, accountID, prodID, _ := menuOrderFixture(t)
	ctx := context.Background()
	// A NON-menu form slugged "menu-order".
	if err := h.Forms.Create(ctx, &dbstore.Form{
		AccountID: accountID, Slug: "menu-order", Title: "Decoy form", Status: "published",
		Config: map[string]any{},
	}); err != nil {
		t.Fatalf("create decoy form: %v", err)
	}

	// The legacy route with a form_slug pointing at the NON-menu decoy must
	// 404 (it is not a menu) — the literal route wins over the {slug} wildcard.
	body := `{"form_slug":"menu-order","client":{"full_name":"Ali","email":"ali@example.test"},` +
		`"items":[{"product_id":"` + prodID + `","quantity":1}],` +
		`"payment_method":"bank_direct"}`
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, "/menu-order", bytes.NewBufferString(body))
	req.Host = workspaceSlug + ".jomform.com"
	req.RemoteAddr = uniqueTestIP() + ":1234"
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("POST /menu-order with a non-menu form slug = %d, want 404 (only a menu form may check out here; body %s)",
			rec.Code, rec.Body.String())
	}
}

// findFormBySlug reads a form by slug within an account.
func findFormBySlug(t *testing.T, h *PublicHandlers, accountID, slug string) *dbstore.Form {
	t.Helper()
	forms, err := h.Forms.ListByAccount(context.Background(), accountID)
	if err != nil {
		t.Fatalf("list forms: %v", err)
	}
	for _, f := range forms {
		if f.Slug == slug {
			return f
		}
	}
	t.Fatalf("form slug %q not found in account %s", slug, accountID)
	return nil
}
