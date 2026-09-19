package httpapi

// Issue #623: per-variation subscription cycles, end-to-end on a real Postgres.
//
// The card's acceptance criteria:
//
//   - a subscription product can hold two variations with DIFFERENT cycles and
//     each bills on its own;
//   - ONE purchase of a variation produces a subscription on THAT VARIATION's
//     cycle, not the product's;
//   - a variation with NO cycle set still inherits the product's cycle, exactly
//     as before this change (no regression on existing products);
//   - removing a variation's cycle (set back to unset) restores inheritance.
//
// The bite requirement: the variation's cycle is DELIBERATELY DIFFERENT from the
// product's in every overriding case, so reverting the purchase path to the
// product's cycle makes these tests FAIL and name the discrepancy. A test whose
// expected value equals the product's cycle would pass either way and would be
// no evidence at all.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// variationCycleFixture seeds a user + workspace + account + a published form and
// a SUBSCRIPTION product whose product-level cycle differs from every variation
// cycle used below (product: month / trial 0 / infinite).
func variationCycleFixture(t *testing.T) (*PublicHandlers, string, string, string, *dbstore.Product) {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)

	var userID, workspaceSlug, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it623-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'it623-' || md5(random()::text), 'it623-' || md5(random()::text))
		 RETURNING slug`, userID).Scan(&workspaceSlug); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 SELECT $1, 'it623-acc-' || md5(random()::text), 'it623-' || md5(random()::text), id
		 FROM workspaces WHERE slug = $2
		 RETURNING id`, userID, workspaceSlug).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	// PRODUCT cycle: monthly, no trial, infinite charges. Every variation cycle
	// under test differs from this, so a test cannot pass by accident.
	prod := &dbstore.Product{
		AccountID: accountID, Name: "Kelas", PriceSen: 10000,
		Type: dbstore.SaleTypeSubscription, BillingPeriod: "month", Active: true,
	}
	if err := dbstore.NewProductStore(pool).Create(ctx, prod); err != nil {
		t.Fatalf("create product: %v", err)
	}

	// Two variations with DIFFERENT cycles: yearly (trial 30) and monthly-with-a
	// 3-charge limit. Plus one with no cycle at all (the inherit case).
	three := 3
	trial30 := 30
	vs := dbstore.NewProductVariationStore(pool)
	if err := vs.ReplaceAll(ctx, prod.ID, []*dbstore.ProductVariation{
		{Name: "Size L — yearly", PriceSen: 120000, BillingPeriod: "year", TrialDays: &trial30},
		{Name: "Size M — 3 charges", PriceSen: 10000, Charges: &three},
		{Name: "One Size — inherits", PriceSen: 9000},
	}); err != nil {
		t.Fatalf("replace variations: %v", err)
	}

	fs := dbstore.NewFormStore(pool)
	formSlug := "it623-" + accountID[:8]
	f := &dbstore.Form{
		AccountID: accountID, Slug: formSlug, Title: "Kelas form", Status: "published",
		Config: map[string]any{
			"blocks":   []any{map[string]any{"type": "product", "product_id": prod.ID}},
			"products": []any{map[string]any{"id": prod.ID, "name": "Kelas", "price_sen": 10000}},
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
		Workspaces:        dbstore.NewWorkspaceStore(pool),
		BaseURL:           "http://localhost:8080",
	}
	return h, workspaceSlug, formSlug, accountID, prod
}

// submitVariation buys one variation through the real public order route and
// returns the recorded sale.
func submitVariation(t *testing.T, h *PublicHandlers, host, formSlug, prodID, variationID string) *dbstore.Sale {
	t.Helper()
	body := `{"client":{"full_name":"Ali","email":"ali@example.test"},` +
		`"items":[{"product_id":"` + prodID + `","variation_id":"` + variationID + `","quantity":1}],` +
		`"payment_method":"bank_direct"}`
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, "/"+formSlug+"/order", bytes.NewBufferString(body))
	req.Host = host
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
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
	return sale
}

// TestIssue623VariationCycleDrivesTheSale is the core requirement: the sale's
// recorded subscription terms come from the CHOSEN VARIATION, not the product.
// Product cycle is month/trial-0/infinite; this variation is year/trial-30, so
// reverting to the product's cycle fails this test and names the discrepancy.
func TestIssue623VariationCycleDrivesTheSale(t *testing.T) {
	h, workspaceSlug, formSlug, _, prod := variationCycleFixture(t)
	ctx := context.Background()

	vs := h.ProductVariations
	all, err := vs.ListByProduct(ctx, prod.ID)
	if err != nil {
		t.Fatalf("list variations: %v", err)
	}
	var yearly *dbstore.ProductVariation
	for _, v := range all {
		if v.Name == "Size L — yearly" {
			yearly = v
		}
	}
	if yearly == nil {
		t.Fatal("yearly variation not found")
	}

	sale := submitVariation(t, h, workspaceSlug+".jomform.com", formSlug, prod.ID, yearly.ID)

	if sale.SaleType != dbstore.SaleTypeSubscription {
		t.Fatalf("sale_type = %q, want %q", sale.SaleType, dbstore.SaleTypeSubscription)
	}
	if sale.SubscriptionPeriod != "year" {
		t.Errorf("sale subscription_period = %q, want %q — the product is monthly, so %q means the purchase path used the PRODUCT's cycle instead of the chosen variation's",
			sale.SubscriptionPeriod, "year", sale.SubscriptionPeriod)
	}
	if sale.SubscriptionAmountSen != 120000 {
		t.Errorf("subscription_amount_sen = %d, want 120000 (the variation's own price)", sale.SubscriptionAmountSen)
	}
	if sale.SubscriptionVariationID != yearly.ID {
		t.Errorf("subscription_variation_id = %q, want %q — activation needs it to resolve this variation's cycle on every later charge",
			sale.SubscriptionVariationID, yearly.ID)
	}
	if len(sale.Items) != 1 || sale.Items[0].PriceSen != 120000 {
		t.Errorf("line item = %+v, want the variation's 120000 sen", sale.Items)
	}
}

// TestIssue623VariationWithoutCycleInheritsTheProduct is the no-regression
// criterion: this is the state EVERY variation in production is in today, so it
// must bill on the product's cycle exactly as before.
func TestIssue623VariationWithoutCycleInheritsTheProduct(t *testing.T) {
	h, workspaceSlug, formSlug, _, prod := variationCycleFixture(t)
	ctx := context.Background()

	all, err := h.ProductVariations.ListByProduct(ctx, prod.ID)
	if err != nil {
		t.Fatalf("list variations: %v", err)
	}
	var inherits *dbstore.ProductVariation
	for _, v := range all {
		if v.Name == "One Size — inherits" {
			inherits = v
		}
	}
	if inherits == nil {
		t.Fatal("inheriting variation not found")
	}
	// Precondition: it really carries no cycle of its own.
	if inherits.BillingPeriod != "" || inherits.TrialDays != nil || inherits.Charges != nil {
		t.Fatalf("the inheriting variation must have no cycle of its own, got %+v", inherits)
	}

	sale := submitVariation(t, h, workspaceSlug+".jomform.com", formSlug, prod.ID, inherits.ID)

	if sale.SubscriptionPeriod != "month" {
		t.Errorf("sale subscription_period = %q, want the PRODUCT's %q — a variation with no cycle of its own must inherit it, exactly as before issue #623",
			sale.SubscriptionPeriod, "month")
	}
	if sale.SubscriptionAmountSen != 9000 {
		t.Errorf("subscription_amount_sen = %d, want 9000 (the variation's own price still applies)", sale.SubscriptionAmountSen)
	}
}

// TestIssue623TwoVariationsOfOneProductBillDifferently is the card's headline
// acceptance criterion: two variations, different cycles, each charging its own
// rate — from ONE product.
func TestIssue623TwoVariationsOfOneProductBillDifferently(t *testing.T) {
	h, workspaceSlug, formSlug, _, prod := variationCycleFixture(t)
	ctx := context.Background()

	all, err := h.ProductVariations.ListByProduct(ctx, prod.ID)
	if err != nil {
		t.Fatalf("list variations: %v", err)
	}
	var yearly, threeCharges *dbstore.ProductVariation
	for _, v := range all {
		switch v.Name {
		case "Size L — yearly":
			yearly = v
		case "Size M — 3 charges":
			threeCharges = v
		}
	}
	if yearly == nil || threeCharges == nil {
		t.Fatalf("expected both variations, got %d", len(all))
	}

	host := workspaceSlug + ".jomform.com"
	a := submitVariation(t, h, host, formSlug, prod.ID, yearly.ID)
	b := submitVariation(t, h, host, formSlug, prod.ID, threeCharges.ID)

	if a.SubscriptionPeriod == b.SubscriptionPeriod {
		t.Errorf("both variations resolved period %q — two variations of one product must be able to bill on DIFFERENT cycles",
			a.SubscriptionPeriod)
	}
	if a.SubscriptionPeriod != "year" {
		t.Errorf("yearly variation period = %q, want year", a.SubscriptionPeriod)
	}
	if b.SubscriptionPeriod != "month" {
		t.Errorf("3-charge variation period = %q, want the product's month (it set no period)", b.SubscriptionPeriod)
	}
	if a.SubscriptionAmountSen != 120000 || b.SubscriptionAmountSen != 10000 {
		t.Errorf("amounts = %d / %d, want 120000 / 10000 — each variation charges its own rate",
			a.SubscriptionAmountSen, b.SubscriptionAmountSen)
	}
}

// TestIssue623ClearingAVariationCycleRestoresInheritance is the state-transition
// proof the operator note called for: "not set" and "set to the product's
// value" must stay distinguishable, so a variation whose cycle is REMOVED starts
// inheriting again.
func TestIssue623ClearingAVariationCycleRestoresInheritance(t *testing.T) {
	h, workspaceSlug, formSlug, _, prod := variationCycleFixture(t)
	ctx := context.Background()

	pool := bundleTestPool(t)
	vs := h.ProductVariations
	all, err := vs.ListByProduct(ctx, prod.ID)
	if err != nil {
		t.Fatalf("list variations: %v", err)
	}
	var yearly *dbstore.ProductVariation
	for _, v := range all {
		if v.Name == "Size L — yearly" {
			yearly = v
		}
	}
	if yearly == nil {
		t.Fatal("yearly variation not found")
	}

	host := workspaceSlug + ".jomform.com"
	before := submitVariation(t, h, host, formSlug, prod.ID, yearly.ID)
	if before.SubscriptionPeriod != "year" {
		t.Fatalf("precondition failed: period = %q, want year (the variation's own cycle)", before.SubscriptionPeriod)
	}

	// Clear the variation's cycle (store level — the same call the MCP tool's
	// clear_* flags and the editor's empty field make).
	if err := vs.Update(ctx, prod.ID, yearly.ID,
		nil, nil, nil, nil, nil,
		nil, true, // clear billing_period
		nil, true, // clear trial_days
		nil, true); err != nil { // clear charges
		t.Fatalf("clear variation cycle: %v", err)
	}

	cleared, err := vs.GetByProduct(ctx, prod.ID, yearly.ID)
	if err != nil {
		t.Fatalf("reload variation: %v", err)
	}
	if cleared.BillingPeriod != "" || cleared.TrialDays != nil || cleared.Charges != nil {
		t.Fatalf("after clearing, the variation must have no cycle of its own, got %+v", cleared)
	}
	// The stored state must now be distinguishable from a variation that was
	// never set — and it is, because both are NULL/empty, which is the point.
	var nullCount int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM product_variations
		  WHERE id=$1 AND billing_period IS NULL AND trial_days IS NULL AND subscription_charges IS NULL`,
		yearly.ID).Scan(&nullCount); err != nil {
		t.Fatalf("check nulls: %v", err)
	}
	if nullCount != 1 {
		t.Errorf("a cleared cycle must be stored as NULL (the inherit state), not as zero values")
	}

	after := submitVariation(t, h, host, formSlug, prod.ID, yearly.ID)
	if after.SubscriptionPeriod != "month" {
		t.Errorf("after clearing its cycle the variation must inherit the product's %q, got %q — otherwise 'unset' and 'set' are indistinguishable",
			"month", after.SubscriptionPeriod)
	}
}

// TestIssue623OneTimeProductVariationIsUnchanged guards the blast radius: a
// variation of a ONE-TIME product must not become a subscription because it
// carries cycle-looking columns.
func TestIssue623OneTimeProductVariationIsUnchanged(t *testing.T) {
	h, workspaceSlug, formSlug, accountID, _ := variationCycleFixture(t)
	ctx := context.Background()

	pool := bundleTestPool(t)
	oneTime := &dbstore.Product{
		AccountID: accountID, Name: "Tee", PriceSen: 1000,
		Type: dbstore.SaleTypeOneTime, Active: true,
	}
	if err := dbstore.NewProductStore(pool).Create(ctx, oneTime); err != nil {
		t.Fatalf("create one-time product: %v", err)
	}
	vs := h.ProductVariations
	if err := vs.ReplaceAll(ctx, oneTime.ID, []*dbstore.ProductVariation{
		{Name: "Size M", PriceSen: 1200, BillingPeriod: "year"},
	}); err != nil {
		t.Fatalf("replace variations: %v", err)
	}
	all, err := vs.ListByProduct(ctx, oneTime.ID)
	if err != nil || len(all) != 1 {
		t.Fatalf("list variations: %v (n=%d)", err, len(all))
	}

	sale := submitVariation(t, h, workspaceSlug+".jomform.com", formSlug, oneTime.ID, all[0].ID)
	if sale.SaleType != dbstore.SaleTypeOneTime {
		t.Errorf("sale_type = %q, want %q — a one-time product must never become a subscription",
			sale.SaleType, dbstore.SaleTypeOneTime)
	}
	if sale.SubscriptionPeriod != "" {
		t.Errorf("subscription_period = %q, want empty for a one-time sale", sale.SubscriptionPeriod)
	}
}
