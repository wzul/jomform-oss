package httpapi

// Card t_3d11f98d — BUG 2: "a form with no blocks shows a static RM0.00".
//
// The card's premise was that nothing updates `#total` on the LAST-RESORT
// layout (`renderLegacy` -> `formTmpl`). Measured in Chromium against a real
// API and a real form: the layout DOES track the checked products
// (RM0.00 -> RM21.00 -> RM29.00 -> RM21.00), and the sale records the same
// amount. No code change was needed; what was missing was a guard, which is
// what this test is.
//
// Two things are pinned, because "the total is right" has two halves and only
// one of them is visible to `go test`:
//
//  1. THE SCRIPT. The layout prints a static seed (`#total` = RM0.00) and then
//     rewrites it from the checked `.p` boxes' `data-price`. Deleting that
//     assignment (or the change listener) leaves the buyer looking at RM0.00 on
//     a form that charges RM29.00 — the carded symptom — and no Go assertion
//     could see it. So the wiring itself is asserted here, from the rendered
//     page, not from a hand-written string.
//  2. THE CHARGE MATCHES THE MARKUP. The expected total is computed from the
//     prices the page PRINTED (each `input.p`'s data-price), then compared with
//     `sales.total_sen`. A renderer that prints one price and a submit path
//     that charges another is the defect class this card is about; deriving the
//     expectation from the markup is what makes that comparison meaningful
//     rather than a hard-coded sum.

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// legacyTotalFixture seeds the pre-builder shape: a published payment form with
// NO `blocks` key (so serveForm falls through to renderLegacy) whose
// `config.products[]` carries the meal AND the shipping fee, which is how those
// forms kept their delivery charge. Returns the handlers, host, form slug and
// the product ids in config order.
func legacyTotalFixture(t *testing.T) (h *PublicHandlers, host, formSlug string, ids []string) {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)

	var userID, accountID, workspaceSlug string
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

	ps := dbstore.NewProductStore(pool)
	meal := &dbstore.Product{AccountID: accountID, Name: "Nasi Lemak", PriceSen: 2100, Type: dbstore.SaleTypeOneTime, Active: true}
	ship := &dbstore.Product{AccountID: accountID, Name: "Penghantaran", PriceSen: 800, Type: dbstore.SaleTypeOneTime, Active: true}
	for _, p := range []*dbstore.Product{meal, ship} {
		if err := ps.Create(ctx, p); err != nil {
			t.Fatalf("create product: %v", err)
		}
	}

	fs := dbstore.NewFormStore(pool)
	formSlug = "it-t3d11leg-" + accountID[:8]
	f := &dbstore.Form{
		AccountID: accountID, Slug: formSlug, Title: "Legacy total form", Status: "published",
		Config: map[string]any{
			"description": "Legacy no-blocks form",
			"products": []any{
				map[string]any{"id": meal.ID, "name": meal.Name, "price_sen": meal.PriceSen},
				map[string]any{"id": ship.ID, "name": ship.Name, "price_sen": ship.PriceSen},
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
		Products:   ps,
		Workspaces: dbstore.NewWorkspaceStore(pool),
		BaseURL:    "http://localhost:8080",
	}
	return h, workspaceSlug + ".jomform.com", formSlug, []string{meal.ID, ship.ID}
}

var legacyPriceRe = regexp.MustCompile(`<input type="checkbox" class="p" data-id="([^"]+)" data-price="(\d+)"`)

// TestLegacyFormTotalFollowsItsProducts is the guard for acceptance 2.
func TestLegacyFormTotalFollowsItsProducts(t *testing.T) {
	h, host, formSlug, ids := legacyTotalFixture(t)

	rec := issue461Serve(t, h, host, http.MethodGet, "/"+formSlug, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("render status = %d, want 200", rec.Code)
	}
	page := rec.Body.String()
	// The page must really be the legacy layout, or this case measures the
	// block renderer and proves nothing about renderLegacy.
	if !strings.Contains(page, `class="pname"`) || !strings.Contains(page, `id="total"`) {
		t.Fatalf("the served page is not the legacy product layout — this case is no longer measuring renderLegacy\nbody:\n%s", page)
	}

	// 1. The script that keeps #total honest. Asserted on the rendered page so
	// the template, the view and the wiring are all on the hook: the static
	// seed stays RM0.00 unless something rewrites it, and the only thing that
	// can is this assignment driven by the boxes' data-price.
	for _, want := range []string{
		`document.getElementById('total').textContent`,
		`boxes.forEach(b => b.addEventListener('change', update))`,
		`total += Number(b.dataset.price)`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("legacy page is missing %q — the buyer would be left looking at the static RM0.00 seed while the form charges real money (card t_3d11f98d, acceptance 2)", want)
		}
	}

	// 2. The total the page PRINTS for "everything checked" must equal what the
	// server CHARGES for exactly those items. The expectation is derived from
	// the markup, so a page that shows one price and a server that charges
	// another cannot pass by sharing a stale fixture.
	matches := legacyPriceRe.FindAllStringSubmatch(page, -1)
	if len(matches) != 2 {
		t.Fatalf("rendered %d priced product boxes, want 2 — the shipping fee is not on the page at all\nbody:\n%s", len(matches), page)
	}
	var shownCents int64
	seen := map[string]bool{}
	for _, m := range matches {
		cents, err := strconv.ParseInt(m[2], 10, 64)
		if err != nil {
			t.Fatalf("data-price %q is not an integer sen amount: %v", m[2], err)
		}
		shownCents += cents
		seen[m[1]] = true
	}
	for _, id := range ids {
		if !seen[id] {
			t.Fatalf("product %s is charged-able but not rendered — buyer cannot see or pick the fee", id)
		}
	}
	if shownCents != 2900 {
		t.Fatalf("rendered prices sum to %d sen, want 2900 (RM29.00) — the fixture no longer matches the card's shape", shownCents)
	}

	body := `{"client":{"full_name":"Ali","email":"ali@example.test"},"items":[` +
		`{"product_id":"` + ids[0] + `","quantity":1},` +
		`{"product_id":"` + ids[1] + `","quantity":1}],` +
		`"payment_method":"bank_direct","fields":{}}`
	orec := issue461Serve(t, h, host, http.MethodPost, "/"+formSlug+"/order", body)
	if orec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d, want 201 (body %s)", orec.Code, orec.Body.String())
	}
	var resp struct {
		RefCode string `json:"ref_code"`
	}
	if err := json.Unmarshal(orec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	sale, err := h.Sales.ByRefCode(context.Background(), resp.RefCode)
	if err != nil {
		t.Fatalf("fetch sale: %v", err)
	}
	if sale.TotalSen != shownCents {
		t.Errorf("sale total = %d sen, the page showed %d sen (RM%.2f) — the buyer is charged something other than the total they read", sale.TotalSen, shownCents, float64(shownCents)/100)
	}
}
