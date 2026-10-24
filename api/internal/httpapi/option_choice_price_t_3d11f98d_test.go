package httpapi

// Card t_3d11f98d — BUG 1: an option/shipping block created in the builder was
// charged RM0.00.
//
// The fixture below is shaped the way the BUILDER writes a config, which is the
// whole point: the pre-existing regression test (product_live_issue461_test.go)
// builds the HAND shape — a block-level `price_sen` beside the choices — which
// the builder never produces. That is why the defect stayed green.
//
// What the builder actually sends (web/src/app/builder/page.tsx):
//   - makeBlock (lines 548-563): an option/shipping block gets `choices` with a
//     per-choice `price_sen`, and `productId = b.id` (the block submits with
//     product_id = its own id).
//   - the save payload (lines 728-751) always carries `price_sen: b.priceSen`.
//     Nothing in the builder UI ever assigns `priceSen` for an option/shipping
//     block — the only price input is the per-choice one, `input` on
//     `c.price_sen` (line 1324) — so that value is `undefined` and JavaScript
//     DROPS the key when the payload is serialised.
//
// So a builder-made pricing block reaches the server with no block-level price
// at all, and the price the buyer is shown lives only in `choices[].price_sen`.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// builderPricingFixture seeds a published payment form whose option and
// shipping blocks are byte-shaped like the builder's save payload: per-choice
// prices, no block-level `price_sen`. Returns the handlers, the workspace host
// slug, the form slug and the product id.
func builderPricingFixture(t *testing.T) (h *PublicHandlers, workspaceSlug, formSlug, prodID string) {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)

	var userID, accountID string
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

	prod := &dbstore.Product{
		AccountID: accountID, Name: "Nasi Lemak", PriceSen: 2100,
		Type: dbstore.SaleTypeOneTime, Active: true,
	}
	if err := dbstore.NewProductStore(pool).Create(ctx, prod); err != nil {
		t.Fatalf("create product: %v", err)
	}

	// The builder-shaped pricing blocks. `product_id` == `id` and there is NO
	// block-level `price_sen` key on either block.
	optID := "p_" + accountID[:8] + "_opt"
	shipID := "p_" + accountID[:8] + "_ship"

	fs := dbstore.NewFormStore(pool)
	formSlug = "it-t3d11-" + accountID[:8]
	f := &dbstore.Form{
		AccountID: accountID, Slug: formSlug, Title: "Builder pricing form", Status: "published",
		Config: map[string]any{
			"blocks": []any{
				map[string]any{"id": "p_" + accountID[:8] + "_prod", "type": "product", "product_id": prod.ID, "value": "Nasi Lemak"},
				map[string]any{
					"id": optID, "type": "option", "value": "Tambahan", "product_id": optID,
					"choices": []any{
						map[string]any{"label": "Kad", "value": "card", "price_sen": 500},
						map[string]any{"label": "Tiada", "value": "none", "price_sen": 0},
					},
				},
				map[string]any{
					"id": shipID, "type": "shipping", "value": "Penghantaran", "product_id": shipID,
					"choices": []any{
						map[string]any{"label": "Standard", "value": "std", "price_sen": 800},
					},
				},
				map[string]any{"id": "p_" + accountID[:8] + "_email", "type": "email", "value": "Email", "required": true},
			},
			"products": []any{
				map[string]any{"id": prod.ID, "name": "Nasi Lemak", "price_sen": 2100},
			},
		},
		Theme: map[string]any{"primary": "#5e6ad2"},
	}
	if err := fs.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}

	h = &PublicHandlers{
		Forms:      fs,
		Sales:      dbstore.NewSaleStore(pool),
		Accounts:   dbstore.NewAccountStore(pool),
		Products:   dbstore.NewProductStore(pool),
		Workspaces: dbstore.NewWorkspaceStore(pool),
		BaseURL:    "http://localhost:8080",
	}
	return h, workspaceSlug, formSlug, prod.ID
}

// TestBuilderShapedOptionAndShippingAreChargedTheChoicePrice is the guard for
// BUG 1: a buyer who picks the RM5 add-on and the RM8 shipping on a
// builder-made form must be charged 500 + 800 sen, exactly matching the total
// the page showed them.
func TestBuilderShapedOptionAndShippingAreChargedTheChoicePrice(t *testing.T) {
	h, workspaceSlug, formSlug, prodID := builderPricingFixture(t)
	host := workspaceSlug + ".jomform.com"
	accountID := issue461AccountID(t, h, workspaceSlug)

	optID := "p_" + accountID[:8] + "_opt"
	shipID := "p_" + accountID[:8] + "_ship"
	emailID := "p_" + accountID[:8] + "_email"

	// The body the block renderer submits for this pick: the two blocks as line
	// items (product_id = block id) plus their answers in `fields`.
	body := `{"client":{"full_name":"Ali","email":"ali@example.test"},"items":[` +
		`{"product_id":"` + prodID + `","quantity":1},` +
		`{"product_id":"` + optID + `","quantity":1},` +
		`{"product_id":"` + shipID + `","quantity":1}],` +
		`"payment_method":"bank_direct",` +
		`"fields":{"` + emailID + `":"ali@example.test","` + optID + `":"card","` + shipID + `":"std"}}`
	rec := issue461Serve(t, h, host, http.MethodPost, "/"+formSlug+"/order", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
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
	if len(sale.Items) != 3 {
		t.Fatalf("sale items = %d, want 3", len(sale.Items))
	}
	byID := map[string]int64{}
	for _, it := range sale.Items {
		byID[it.ProductID] = it.PriceSen
	}
	if got := byID[optID]; got != 500 {
		t.Errorf("option block charged %d sen, want 500 (the buyer was shown +RM5.00)", got)
	}
	if got := byID[shipID]; got != 800 {
		t.Errorf("shipping block charged %d sen, want 800 (the buyer was shown +RM8.00)", got)
	}
	if sale.TotalSen != 3400 {
		t.Errorf("sale total = %d sen, want 3400 (2100 + 500 + 800)", sale.TotalSen)
	}
}

// TestBuilderShapedPricingBlockRefusesAChoiceThatWasNeverOffered: the price
// comes from the choice the answer NAMES, so an answer that names no choice of
// that block cannot be priced at all — it is refused rather than charged as a
// free add-on. Without this, a buyer could pick "Kad (+RM5.00)" and post any
// other value to get the add-on at 0.
func TestBuilderShapedPricingBlockRefusesAChoiceThatWasNeverOffered(t *testing.T) {
	h, workspaceSlug, formSlug, prodID := builderPricingFixture(t)
	host := workspaceSlug + ".jomform.com"
	accountID := issue461AccountID(t, h, workspaceSlug)
	optID := "p_" + accountID[:8] + "_opt"
	emailID := "p_" + accountID[:8] + "_email"

	body := `{"client":{"full_name":"Ali","email":"ali@example.test"},"items":[` +
		`{"product_id":"` + prodID + `","quantity":1},` +
		`{"product_id":"` + optID + `","quantity":1}],` +
		`"payment_method":"bank_direct",` +
		`"fields":{"` + emailID + `":"ali@example.test","` + optID + `":"forged"}}`
	rec := issue461Serve(t, h, host, http.MethodPost, "/"+formSlug+"/order", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("submit with an answer the block never offered = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "pilihan tidak sah") {
		t.Errorf("400 body = %s, want it to say the choice is invalid", rec.Body.String())
	}
}

// TestBuilderShapedFormRendersTheChoicePricesItCharges pins the other half of
// acceptance 1: the page's own numbers. A price the page prints but the server
// does not charge (or the reverse) is the defect this card is about, so the
// rendered select options and the charged amounts must come from one source.
func TestBuilderShapedFormRendersTheChoicePricesItCharges(t *testing.T) {
	h, workspaceSlug, formSlug, _ := builderPricingFixture(t)
	host := workspaceSlug + ".jomform.com"

	rec := issue461Serve(t, h, host, http.MethodGet, "/"+formSlug, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("render status = %d, want 200", rec.Code)
	}
	page := rec.Body.String()
	for _, want := range []string{`data-price="500"`, `data-price="800"`} {
		if !strings.Contains(page, want) {
			t.Errorf("rendered page is missing %s — the buyer must see the price that will be charged", want)
		}
	}
}
