package httpapi

// Issue #497: the restaurant menu is a PER-FORM setting, not a per-workspace
// landing template.
//
// These tests pin the root-cause fix:
//   - the menu config lives on the FORM (forms.config.menu) and is resolved
//     from the form, not from workspace_settings.landing_page_json;
//   - the menu's products come from the form's OWN product blocks — an
//     unrelated account product can never leak into the menu;
//   - the workspace root renders the normal landing page / form listing again
//     (a legacy landing_page_json.menu is ignored, not rendered);
//   - the menu is reachable at the form's own URL and ?table=N still flows;
//   - two menu forms in one workspace coexist with different categories and
//     products;
//   - a menu form with no product blocks shows an empty-state instead of
//     falling back to "every account product".
//
// The pure tests need no DB. The integration tests (a real Postgres) prove the
// two-menus-in-one-workspace case end-to-end.

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

// ── pure: per-form config resolution ────────────────────────────────────────

// A menu config round-trips on the form: written under forms.config.menu,
// preserving the form's blocks, and read back by MenuFromConfig.
func TestIssue497MenuConfigLivesOnTheForm(t *testing.T) {
	config := map[string]any{
		"blocks": []any{
			map[string]any{"id": "b1", "type": "product", "product_id": "prod-a", "value": "Nasi Lemak"},
			map[string]any{"id": "b2", "type": "product", "product_id": "prod-b"},
			map[string]any{"id": "b3", "type": "text", "value": "Welcome"},
		},
	}
	next := dbstore.WithMenu(config, &dbstore.MenuPage{
		Enabled:    true,
		Categories: []dbstore.MenuCategory{{Name: "Makanan", ProductIDs: []string{"prod-a"}}},
	})
	menu := dbstore.MenuFromConfig(next)
	if menu == nil || !menu.Enabled {
		t.Fatalf("menu did not resolve from the form config: %+v", menu)
	}
	if len(menu.Categories) != 1 || menu.Categories[0].Name != "Makanan" {
		t.Errorf("menu categories = %+v, want the Makanan category", menu.Categories)
	}
	// The form's own blocks survive — the menu is one more key, never a
	// replacement for the form.
	blocks, _ := next["blocks"].([]any)
	if len(blocks) != 3 {
		t.Errorf("WithMenu dropped the form's blocks: %+v", next["blocks"])
	}
	// A form with no menu key resolves to nil (renders normally).
	if dbstore.MenuFromConfig(map[string]any{"blocks": []any{}}) != nil {
		t.Error("a form with no menu key must resolve to nil")
	}
}

// formMenuItems reads ONLY the form's product blocks: their product_id and the
// merchant's per-item label. Non-product blocks and blanks are skipped, and a
// duplicate reference collapses.
func TestIssue497MenuItemsComeFromTheFormBlocks(t *testing.T) {
	items := formMenuItems(map[string]any{
		"blocks": []any{
			map[string]any{"id": "b1", "type": "product", "product_id": "prod-a", "value": "Nasi Lemak Ayam"},
			map[string]any{"id": "b2", "type": "text", "value": "Not a product"},
			map[string]any{"id": "b3", "type": "product", "product_id": "  prod-b  "},
			map[string]any{"id": "b4", "type": "product"},                         // no product_id → skipped
			map[string]any{"id": "b5", "type": "product", "product_id": "prod-a"}, // dup → skipped
		},
	})
	if len(items) != 2 {
		t.Fatalf("formMenuItems returned %d items, want 2 (%+v)", len(items), items)
	}
	if items[0].ProductID != "prod-a" || items[0].Label != "Nasi Lemak Ayam" {
		t.Errorf("item[0] = %+v, want prod-a + its label", items[0])
	}
	if items[1].ProductID != "prod-b" || items[1].Label != "" {
		t.Errorf("item[1] = %+v, want prod-b with a trimmed blank label", items[1])
	}
	// A form with no product blocks yields nothing — never account products.
	if got := formMenuItems(map[string]any{"blocks": []any{
		map[string]any{"id": "b1", "type": "text", "value": "hi"},
	}}); len(got) != 0 {
		t.Errorf("a form with no product blocks produced %d menu items", len(got))
	}
}

// A menu form with no product blocks renders the empty-state, NOT the account's
// products (issue #497 root cause: the old menu used ListActiveByAccount).
func TestIssue497MenuFormWithNoProductBlocksShowsEmptyState(t *testing.T) {
	data := workspaceMenuData{
		Slug: "menu", Menu: &dbstore.MenuPage{Enabled: true},
		Th: menuTheme{Hero: "Warung Ali"}, Ls: formLangs["en"],
		HasItems: false,
	}
	body := renderMenu497(t, data)
	if !strings.Contains(body, formLangs["en"].MenuEmpty) {
		t.Errorf("empty menu form did not render the empty-state hint")
	}
	// No item and no "All" junk section is present.
	if strings.Contains(body, `class="card"`) || strings.Contains(body, `class="row"`) {
		t.Errorf("empty menu form rendered items:\n%s", body)
	}
	// The basket/checkout script still guards on the sheet (no crash without
	// items) and the page is a valid menu page.
	if !strings.Contains(body, "var sheet=document.getElementById('menuSheet');") {
		t.Error("empty menu form lost the cart guard")
	}
}

// The legacy landing_page_json.menu blob is parsed but never rendered: the
// workspace root falls through to the landing page / bare listing.
func TestIssue497LegacyLandingMenuIsIgnored(t *testing.T) {
	// The deprecated field exists so the payload parses safely...
	lp := &dbstore.LandingPage{}
	if err := json.Unmarshal([]byte(`{"enabled":true,"hero":"Old","menu":{"enabled":true}}`), lp); err != nil {
		t.Fatalf("a legacy landing_page_json.menu must not crash the parse: %v", err)
	}
	if lp.Hero != "Old" || !lp.Enabled {
		t.Errorf("legacy landing page did not parse its other fields: %+v", lp)
	}
	// ...but it is NOT a live menu: the renderer resolves menus only from a
	// form config (MenuFromConfig), so the workspace root can never become a
	// menu again.
	if dbstore.MenuFromConfig(map[string]any{"menu": nil}) != nil {
		t.Error("a nil menu must resolve to nil")
	}
}

// Every locale still carries the menu dictionary after the #497 move — no
// blank label can leak, and a non-English menu page localises the
// uncategorised section heading (MenuAll).
func TestIssue497MenuStringsInFiveLocales(t *testing.T) {
	if len(formLangs) != 5 {
		t.Fatalf("formLangs has %d entries, want 5", len(formLangs))
	}
	fields := map[string]func(langStrings) string{
		"MenuAll":        func(l langStrings) string { return l.MenuAll },
		"MenuEmpty":      func(l langStrings) string { return l.MenuEmpty },
		"MenuAdd":        func(l langStrings) string { return l.MenuAdd },
		"MenuCheckout":   func(l langStrings) string { return l.MenuCheckout },
		"MenuExtraHelp":  func(l langStrings) string { return l.MenuExtraHelp },
		"MenuExtraEdit":  func(l langStrings) string { return l.MenuExtraEdit },
		"MenuBestSeller": func(l langStrings) string { return l.MenuBestSeller },
	}
	for code, ls := range formLangs {
		for name, get := range fields {
			if get(ls) == "" {
				t.Errorf("lang %q has empty %s", code, name)
			}
		}
	}
	// The uncategorised section heading localises.
	if got := langStringsForCode("ms").MenuAll; got != "Semua" {
		t.Errorf("ms MenuAll = %q, want Semua", got)
	}
}

// renderMenu497 executes the menu template with the given payload.
func renderMenu497(t *testing.T, data workspaceMenuData) string {
	t.Helper()
	var buf bytes.Buffer
	if err := menuPageTmpl.Execute(&buf, data); err != nil {
		t.Fatalf("menuPageTmpl.Execute: %v", err)
	}
	return buf.String()
}

// ── integration: per-form menu served at the form URL, two menus coexist ────

// menu497Fixture seeds a user + workspace + account, N products, and returns
// the handlers plus the ids needed to build menu forms.
func menu497Fixture(t *testing.T, productNames ...string) (h *PublicHandlers, workspaceSlug, accountID string, prodIDs []string) {
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

	products := dbstore.NewProductStore(pool)
	for i, name := range productNames {
		p := &dbstore.Product{
			AccountID: accountID, Name: name, PriceSen: int64(1000 + i*100),
			Type: dbstore.SaleTypeOneTime, Active: true,
		}
		if err := products.Create(ctx, p); err != nil {
			t.Fatalf("create product %q: %v", name, err)
		}
		prodIDs = append(prodIDs, p.ID)
	}

	h = &PublicHandlers{
		Forms:      dbstore.NewFormStore(pool),
		Sales:      dbstore.NewSaleStore(pool),
		Accounts:   dbstore.NewAccountStore(pool),
		Products:   products,
		Workspaces: dbstore.NewWorkspaceStore(pool),
		BaseURL:    "http://localhost:8080",
	}
	return h, workspaceSlug, accountID, prodIDs
}

// createMenuForm creates a published form whose product blocks reference the
// given products, with an enabled per-form menu config.
func createMenuForm(t *testing.T, h *PublicHandlers, accountID, slug, title string, prodIDs []string) *dbstore.Form {
	t.Helper()
	blocks := make([]any, 0, len(prodIDs))
	for i, pid := range prodIDs {
		blocks = append(blocks, map[string]any{
			"id": "b" + slug + string(rune('a'+i)), "type": "product", "product_id": pid,
		})
	}
	f := &dbstore.Form{
		AccountID: accountID, Slug: slug, Title: title, Status: "published",
		Config: map[string]any{
			"blocks": blocks,
			"menu":   map[string]any{"enabled": true, "categories": []any{}},
		},
	}
	if err := h.Forms.Create(context.Background(), f); err != nil {
		t.Fatalf("create menu form %q: %v", slug, err)
	}
	return f
}

// getForm fetches a form page on the workspace subdomain and returns the body.
func getForm(t *testing.T, h *PublicHandlers, host, slug string) (int, string) {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/"+slug, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// getRoot fetches the workspace root and returns the body.
func getRoot(t *testing.T, h *PublicHandlers, host string) (int, string) {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// A menu form renders at its OWN URL and shows the products it references; a
// product the form does NOT reference can never appear.
func TestIssue497MenuFormRendersOnlyItsOwnProducts(t *testing.T) {
	h, workspaceSlug, accountID, prodIDs := menu497Fixture(t, "Nasi Lemak", "Roti Canai", "Honor Magic 8")
	host := workspaceSlug + ".jomform.com"
	// The menu form references only the two food products; the gadget product
	// (prodIDs[2]) is an unrelated account product that must not leak.
	createMenuForm(t, h, accountID, "menu-lunch", "Lunch Menu", prodIDs[:2])

	code, body := getForm(t, h, host, "menu-lunch")
	if code != http.StatusOK {
		t.Fatalf("menu form status = %d, want 200", code)
	}
	for _, want := range []string{"Nasi Lemak", "Roti Canai", `id="menuSheet"`} {
		if !strings.Contains(body, want) {
			t.Errorf("menu form missing %q", want)
		}
	}
	if strings.Contains(body, "Honor Magic 8") {
		t.Errorf("menu form leaked an unrelated account product (Honor Magic 8)")
	}
}

// Two menu forms in one workspace coexist with different categories and
// products — the per-form config is fully independent.
func TestIssue497TwoMenuFormsCoexist(t *testing.T) {
	h, workspaceSlug, accountID, prodIDs := menu497Fixture(t,
		"Nasi Lemak", "Roti Canai", "Espresso", "Matcha Latte")
	host := workspaceSlug + ".jomform.com"

	// Two independent menu forms.
	createMenuForm(t, h, accountID, "menu-food", "Food Menu", prodIDs[:2])
	createMenuForm(t, h, accountID, "menu-drinks", "Drinks Menu", prodIDs[2:])

	// Give each its own categories.
	food := findFormBySlug(t, h, accountID, "menu-food")
	food.Config = dbstore.WithMenu(food.Config, &dbstore.MenuPage{
		Enabled:    true,
		Categories: []dbstore.MenuCategory{{Name: "Makanan", ProductIDs: prodIDs[:2]}},
	})
	if err := h.Forms.Update(context.Background(), accountID, food.ID, food.Title, food.Config, nil); err != nil {
		t.Fatalf("update food menu: %v", err)
	}
	drinks := findFormBySlug(t, h, accountID, "menu-drinks")
	drinks.Config = dbstore.WithMenu(drinks.Config, &dbstore.MenuPage{
		Enabled:    true,
		Categories: []dbstore.MenuCategory{{Name: "Minuman", ProductIDs: prodIDs[2:]}},
	})
	if err := h.Forms.Update(context.Background(), accountID, drinks.ID, drinks.Title, drinks.Config, nil); err != nil {
		t.Fatalf("update drinks menu: %v", err)
	}

	codeFood, foodBody := getForm(t, h, host, "menu-food")
	if codeFood != http.StatusOK {
		t.Fatalf("food menu status = %d, want 200", codeFood)
	}
	codeDrinks, drinksBody := getForm(t, h, host, "menu-drinks")
	if codeDrinks != http.StatusOK {
		t.Fatalf("drinks menu status = %d, want 200", codeDrinks)
	}

	// Each menu shows its own category heading and products, and not the
	// other's.
	if !strings.Contains(foodBody, "Makanan") || !strings.Contains(foodBody, "Nasi Lemak") {
		t.Errorf("food menu missing its own category/products")
	}
	if strings.Contains(foodBody, "Minuman") || strings.Contains(foodBody, "Espresso") {
		t.Errorf("food menu leaked the drinks menu's content")
	}
	if !strings.Contains(drinksBody, "Minuman") || !strings.Contains(drinksBody, "Espresso") {
		t.Errorf("drinks menu missing its own category/products")
	}
	if strings.Contains(drinksBody, "Makanan") || strings.Contains(drinksBody, "Nasi Lemak") {
		t.Errorf("drinks menu leaked the food menu's content")
	}
}

// The workspace root renders the normal landing page / form listing — a menu
// form never hijacks it (issue #497 consequence 1).
func TestIssue497WorkspaceRootRendersLandingNotMenu(t *testing.T) {
	h, workspaceSlug, accountID, prodIDs := menu497Fixture(t, "Nasi Lemak", "Roti Canai")
	host := workspaceSlug + ".jomform.com"
	createMenuForm(t, h, accountID, "menu-lunch", "Lunch Menu", prodIDs)

	// A landing page is configured, and a LEGACY landing_page_json.menu is
	// present (as a stray #438 blob would be).
	wsID, err := h.Workspaces.IDBySlug(context.Background(), workspaceSlug)
	if err != nil || wsID == "" {
		t.Fatalf("workspace id for slug %q: %v", workspaceSlug, err)
	}
	st, err := h.Workspaces.Settings(context.Background(), wsID)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	st.LandingPage = &dbstore.LandingPage{
		Enabled: true, Hero: "Warung Ali", Subtitle: "Bangi",
	}
	if err := h.Workspaces.SaveSettings(context.Background(), st); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	code, body := getRoot(t, h, host)
	if code != http.StatusOK {
		t.Fatalf("workspace root status = %d, want 200", code)
	}
	// The landing page renders (hero), NOT the menu page.
	if !strings.Contains(body, "Warung Ali") {
		t.Errorf("workspace root did not render the landing page hero")
	}
	if strings.Contains(body, `id="menuSheet"`) || strings.Contains(body, `class="catsel"`) {
		t.Errorf("workspace root rendered the restaurant menu instead of the landing page")
	}

	// With NO landing page configured the root falls back to the bare form
	// listing, which lists the workspace's published form (the pre-#438
	// behavior). This is the strongest proof the menu no longer hijacks it.
	st.LandingPage = nil
	if err := h.Workspaces.SaveSettings(context.Background(), st); err != nil {
		t.Fatalf("clear landing page: %v", err)
	}
	code, body = getRoot(t, h, host)
	if code != http.StatusOK {
		t.Fatalf("bare workspace root status = %d, want 200", code)
	}
	if !strings.Contains(body, "/menu-lunch") {
		t.Errorf("bare workspace root did not list the workspace's published form:\n%s", body)
	}
	if strings.Contains(body, `id="menuSheet"`) || strings.Contains(body, `class="catsel"`) {
		t.Errorf("bare workspace root rendered the restaurant menu instead of the form listing")
	}
}

// ?table=N still reaches the menu form and is echoed into the page (issue #489).
func TestIssue497TableNumberStillFlows(t *testing.T) {
	h, workspaceSlug, accountID, prodIDs := menu497Fixture(t, "Nasi Lemak")
	host := workspaceSlug + ".jomform.com"
	createMenuForm(t, h, accountID, "menu-lunch", "Lunch Menu", prodIDs)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/menu-lunch?table=8", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("menu form?table=8 status = %d, want 200", rec.Code)
	}
	for _, want := range []string{`data-table="8"`, `<input type="hidden" name="table" value="8">`} {
		if !strings.Contains(body, want) {
			t.Errorf("menu form did not carry ?table=8 (%q missing)", want)
		}
	}
}

// A non-menu form (RSVP/wedding) is completely unaffected: it renders its
// normal form layout and never the cart.
func TestIssue497NonMenuFormUnaffected(t *testing.T) {
	h, workspaceSlug, accountID, prodIDs := menu497Fixture(t, "Nasi Lemak")
	host := workspaceSlug + ".jomform.com"
	// A plain response form (RSVP-style) with a name block, no menu config.
	if err := h.Forms.Create(context.Background(), &dbstore.Form{
		AccountID: accountID, Slug: "rsvp", Title: "Majlis Kahwin RSVP", Status: "published",
		Config: map[string]any{
			"mode": "response",
			"blocks": []any{
				map[string]any{"id": "nm", "type": "name", "value": "Nama"},
				map[string]any{"id": "pax", "type": "number", "value": "Bilangan"},
			},
		},
	}); err != nil {
		t.Fatalf("create rsvp form: %v", err)
	}
	_ = prodIDs

	code, body := getForm(t, h, host, "rsvp")
	if code != http.StatusOK {
		t.Fatalf("rsvp form status = %d, want 200", code)
	}
	// It renders its own fields and no menu cart.
	if !strings.Contains(body, "Majlis Kahwin RSVP") {
		t.Errorf("rsvp form did not render its title")
	}
	if strings.Contains(body, `id="menuSheet"`) || strings.Contains(body, "jf.menu.cart") {
		t.Errorf("a non-menu form leaked the menu cart")
	}
}
