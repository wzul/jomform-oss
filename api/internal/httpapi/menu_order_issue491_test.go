package httpapi

// Issue #491 (part 1 of 3): workspace-level order endpoint — the restaurant
// menu page checks out WITHOUT a form.
//
// These tests pin down the acceptance criteria:
//   - the workspace-scoped route accepts items[] (with quantity), client{name,
//     email}, optional fields{}, table_number, affiliate_code with no form;
//   - missing name or email → 400 (CHIP needs both);
//   - prices resolve LIVE from the products table, subtotal = Σ(price × qty);
//   - anti-spam still applies on the formless path (honeypot + timestamp +
//     per-workspace rate limit);
//   - the optional attached extra-info form's registered-field contract is
//     enforced when present, and skipped when absent;
//   - the existing form path (handleSubmit) is unchanged: same validation,
//     same sale.form_id, name still optional;
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

// menuOrderFixture seeds a user + workspace + account + one live product and
// returns the handlers, the workspace slug, the account id and the product id.
// No form is created: the menu checkout must work without one.
func menuOrderFixture(t *testing.T) (h *PublicHandlers, workspaceSlug, accountID, prodID string) {
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
	// deliberately different from any snapshot a form would have carried.
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
	return h, workspaceSlug, accountID, prod.ID
}

// menuOrderPost posts a body to the workspace-scoped order route on the
// workspace's subdomain and returns the recorder.
func menuOrderPost(t *testing.T, h *PublicHandlers, host, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, "/menu-order", bytes.NewBufferString(body))
	req.Host = host
	req.RemoteAddr = uniqueTestIP() + ":1234"
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestIssue491MenuOrderWithoutForm(t *testing.T) {
	h, workspaceSlug, _, prodID := menuOrderFixture(t)
	host := workspaceSlug + ".jomform.com"

	body := `{"client":{"full_name":"Ali","email":"ali@example.test"},` +
		`"items":[{"product_id":"` + prodID + `","quantity":1}],` +
		`"payment_method":"bank_direct","table_number":"8","affiliate_code":"AFF1"}`

	rec := menuOrderPost(t, h, host, body)
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
	// No form in the path → no form on the sale. sales.form_id is nullable and
	// every downstream consumer tolerates it.
	if sale.FormID != "" {
		t.Errorf("sale.form_id = %q, want empty for a formless menu order", sale.FormID)
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
	h, workspaceSlug, _, prodID := menuOrderFixture(t)
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
			rec := menuOrderPost(t, h, host, c.body)
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
	h, workspaceSlug, accountID, prodID := menuOrderFixture(t)
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
	rec := menuOrderPost(t, h, host, body)
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

// TestIssue491MenuOrderRejectsUnknownProduct guards the formless path against
// an unknown product id: with no form snapshot there is nothing to fall back
// to, so the order is rejected rather than sold at zero.
func TestIssue491MenuOrderRejectsUnknownProduct(t *testing.T) {
	h, workspaceSlug, _, _ := menuOrderFixture(t)
	host := workspaceSlug + ".jomform.com"
	body := `{"client":{"full_name":"Ali","email":"ali@example.test"},` +
		`"items":[{"product_id":"00000000-0000-0000-0000-000000000000","quantity":1}],` +
		`"payment_method":"bank_direct"}`
	rec := menuOrderPost(t, h, host, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestIssue491MenuOrderHoneypotStillApplies proves layer 1 survives the
// formless path: a filled honeypot field rejects the order.
func TestIssue491MenuOrderHoneypotStillApplies(t *testing.T) {
	h, workspaceSlug, _, prodID := menuOrderFixture(t)
	host := workspaceSlug + ".jomform.com"
	body := `{"client":{"full_name":"Ali","email":"ali@example.test"},` +
		`"items":[{"product_id":"` + prodID + `","quantity":1}],` +
		`"payment_method":"bank_direct","fields":{"` + antiSpamHoneypotField + `":"http://spam.example"}}`
	rec := menuOrderPost(t, h, host, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("filled honeypot status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestIssue491MenuOrderRateLimitStillApplies proves layer 2 survives the
// formless path, keyed per workspace (there is no form id). The handler is
// called directly so the per-IP middleware burst does not mask the result.
func TestIssue491MenuOrderRateLimitStillApplies(t *testing.T) {
	h, workspaceSlug, _, prodID := menuOrderFixture(t)
	h.FormLimiter = middleware.NewRateLimiter(0, 1) // one token, no refill

	body := `{"client":{"full_name":"Ali","email":"ali@example.test"},` +
		`"items":[{"product_id":"` + prodID + `","quantity":1}],` +
		`"payment_method":"bank_direct"}`
	ip := uniqueTestIP()
	post := func() int {
		req := httptest.NewRequest(http.MethodPost, "/menu-order", bytes.NewBufferString(body))
		req.Host = workspaceSlug + ".jomform.com"
		req.RemoteAddr = ip + ":1234"
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.handleMenuOrder(rec, req)
		return rec.Code
	}
	if got := post(); got != http.StatusCreated {
		t.Fatalf("first menu order = %d, want 201 (body %s)", got, body)
	}
	if got := post(); got != http.StatusTooManyRequests {
		t.Fatalf("second menu order = %d, want 429 (per-workspace rate limit must apply)", got)
	}
}

// TestIssue491RateLimitKeyIsPerWorkspace is the pure counterpart: the formless
// rate limit keys on the workspace, so one workspace's traffic never throttles
// another's, and a nil limiter / empty key never throttles.
func TestIssue491RateLimitKeyIsPerWorkspace(t *testing.T) {
	h := &PublicHandlers{FormLimiter: middleware.NewRateLimiter(0, 1)}
	req := httptest.NewRequest(http.MethodPost, "/menu-order", nil)
	req.RemoteAddr = "203.0.113.9:1234"

	if h.antiSpamCheckRateLimitKey(req, "ws:one") {
		t.Fatalf("first submit for ws:one should be allowed")
	}
	if !h.antiSpamCheckRateLimitKey(req, "ws:one") {
		t.Fatalf("second submit for ws:one should be over the limit")
	}
	if h.antiSpamCheckRateLimitKey(req, "ws:two") {
		t.Fatalf("a different workspace must not be throttled by ws:one's counter")
	}
	// An empty key (unresolvable target) never throttles.
	if h.antiSpamCheckRateLimitKey(req, "") {
		t.Fatalf("empty key must never throttle")
	}
	if (&PublicHandlers{}).antiSpamCheckRateLimitKey(req, "ws:one") {
		t.Fatalf("nil limiter must never throttle")
	}
}

// TestIssue491MenuOrderValidatesAttachedFormFields covers the part-3 design
// hook: when the merchant attaches a published form to the menu for extra
// order info, the submitted fields are validated against that form's
// registered-field contract and persisted on the sale. With no form attached
// the field validation is skipped entirely.
func TestIssue491MenuOrderValidatesAttachedFormFields(t *testing.T) {
	h, workspaceSlug, accountID, prodID := menuOrderFixture(t)
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
	rec := menuOrderPost(t, h, host, base+`}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("unattached form: status = %d, want 201 (field validation must be skipped)", rec.Code)
	}

	// Attach the form to the menu (part 3 setting; JSON blob, no migration).
	st, err := h.Workspaces.Settings(ctx, workspaceIDForSlug(t, h, workspaceSlug))
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	st.LandingPage = &dbstore.LandingPage{
		Enabled: true,
		Menu:    &dbstore.MenuPage{Enabled: true, OrderFormSlug: formSlug},
	}
	if err := h.Workspaces.SaveSettings(ctx, st); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	// Missing the required field → 400 from the attached form's contract.
	rec = menuOrderPost(t, h, host, base+`}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("attached form, missing required field: status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}

	// Providing the field → 201 and the values persist on the sale.
	rec = menuOrderPost(t, h, host, base+`,"fields":{"notes":"pedas sikit"}}`)
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
	// The attached form is used for validation only — the sale stays formless.
	if sale.FormID != "" {
		t.Errorf("sale.form_id = %q, want empty (the extra form is not the order target)", sale.FormID)
	}
}

// TestIssue491MenuOrderReturnsChipCheckoutURL asserts the endpoint returns a
// CHIP checkout URL the caller can redirect to, using a stub CHIP API.
func TestIssue491MenuOrderReturnsChipCheckoutURL(t *testing.T) {
	h, workspaceSlug, accountID, prodID := menuOrderFixture(t)
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
	rec := menuOrderPost(t, h, workspaceSlug+".jomform.com", body)
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
// the shared core must not tighten the form path. A form order with an email
// but NO name still succeeds and records sale.form_id, exactly as before.
func TestIssue491FormPathUnchanged(t *testing.T) {
	h, workspaceSlug, accountID, prodID := menuOrderFixture(t)
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

// TestIssue491MenuOrderRouteIsUnambiguous asserts the chosen path cannot be
// hijacked by a form slug. A form slugged "menu-order" must not capture
// POST /menu-order — the literal route wins and the workspace checkout still
// runs (and still requires name + email).
func TestIssue491MenuOrderRouteIsUnambiguous(t *testing.T) {
	h, workspaceSlug, accountID, prodID := menuOrderFixture(t)
	ctx := context.Background()
	if err := h.Forms.Create(ctx, &dbstore.Form{
		AccountID: accountID, Slug: "menu-order", Title: "Decoy form", Status: "published",
		Config: map[string]any{},
	}); err != nil {
		t.Fatalf("create decoy form: %v", err)
	}

	// No name → the workspace checkout's mandatory-field rule rejects it. A
	// form-scoped handler would have accepted it (name is optional there),
	// so a 400 proves the literal /menu-order route won.
	body := `{"client":{"email":"ali@example.test"},` +
		`"items":[{"product_id":"` + prodID + `","quantity":1}],` +
		`"payment_method":"bank_direct"}`
	rec := menuOrderPost(t, h, workspaceSlug+".jomform.com", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /menu-order with a decoy form slug = %d, want 400 (the literal workspace route must win; body %s)",
			rec.Code, rec.Body.String())
	}
}

// workspaceIDForSlug resolves a workspace id from its slug.
func workspaceIDForSlug(t *testing.T, h *PublicHandlers, slug string) string {
	t.Helper()
	id, err := h.Workspaces.IDBySlug(context.Background(), slug)
	if err != nil || id == "" {
		t.Fatalf("workspace id for slug %q: %v", slug, err)
	}
	return id
}
