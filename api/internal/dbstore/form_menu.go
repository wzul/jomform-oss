// Issue #497: the restaurant menu is a PER-FORM setting, not a per-workspace
// landing template.
//
// Before #497 the menu config lived in workspace_settings.landing_page_json
// (LandingPage.Menu) and the workspace ROOT rendered it, replacing the whole
// storefront: a merchant who turned the menu on lost their form listing, the
// menu sold EVERY active product in the account (ListActiveByAccount), and one
// workspace could only ever have one menu. Wan's call: the menu belongs to a
// FORM, the same shape as the optional order form in #493.
//
// The config now lives on the FORM (forms.config.menu), so:
//   - the workspace root goes back to the normal landing page / form listing;
//   - a menu form renders the products ITS OWN product blocks reference — no
//     account-wide leakage, no "All" section swallowing unrelated products;
//   - two menu forms in one workspace coexist with different categories;
//   - non-menu forms (RSVP/wedding) are completely untouched.
//
// The JSON shape is unchanged from #438/#485/#493 (enabled, categories,
// best_seller, best_seller_ids, order_form_slug) so the stored config, the
// dashboard and the MCP tools all keep one vocabulary. No migration is needed:
// the config is a JSON blob and production had zero workspaces with a menu
// enabled when this shipped.
//
// A stray/legacy landing_page_json.menu is IGNORED (never rendered) rather
// than crashed on — see LandingPage.Menu in workspace.go.
package dbstore

import "encoding/json"

// MenuConfigKey is the form config key holding the per-form menu config.
const MenuConfigKey = "menu"

// MenuPage is the per-form restaurant menu config (issue #438, redesigned in
// #485, cart in #492/#493, moved onto the form in #497). It groups the products
// the form's OWN product blocks reference into merchant-named categories and
// highlights best-sellers. A merchant generates a QR stand pointing at the
// form's public URL so a customer scans it at the table and orders.
type MenuPage struct {
	// Enabled toggles the menu. When false the form renders normally (its own
	// blocks), so a merchant can keep a menu configured but switched off.
	Enabled bool `json:"enabled"`
	// Categories groups product IDs under display names, e.g.
	// [{name:"Makanan", product_ids:["...","..."]}]. Only product IDs the form
	// itself references (via its product blocks) are rendered; a category
	// entry for an unreferenced product is dropped rather than pulling that
	// product into the menu (issue #497).
	Categories []MenuCategory `json:"categories"`
	// BestSeller is an optional product ID highlighted as the best-seller
	// (legacy single-id form; BestSellerIDs wins when set).
	BestSeller string `json:"best_seller,omitempty"`
	// BestSellerIDs is an optional list of product IDs for the "Best Seller"
	// section (issue #485). When set it wins over the legacy single
	// BestSeller id.
	BestSellerIDs []string `json:"best_seller_ids,omitempty"`
	// OrderFormSlug is an optional published form in the same account whose
	// field blocks define the EXTRA fields the menu checkout collects (issue
	// #491/#493). When set, the form-scoped order endpoint validates the
	// submitted fields against that form's field contract and persists them on
	// the sale; when empty the menu checks out with name + email only.
	OrderFormSlug string `json:"order_form_slug,omitempty"`
}

// IsBestSeller reports whether a product ID is highlighted as a best-seller:
// the list (issue #485) when set, otherwise the legacy single id.
func (m *MenuPage) IsBestSeller(productID string) bool {
	if m == nil || productID == "" {
		return false
	}
	if len(m.BestSellerIDs) > 0 {
		for _, id := range m.BestSellerIDs {
			if id == productID {
				return true
			}
		}
		return false
	}
	return m.BestSeller == productID
}

// BestSellerProductIDs returns the ordered best-seller product IDs for the menu
// page (issue #485): the explicit list when set, else the legacy single id as a
// one-element list. Duplicates are dropped.
func (m *MenuPage) BestSellerProductIDs() []string {
	if m == nil {
		return nil
	}
	if len(m.BestSellerIDs) == 0 {
		if m.BestSeller == "" {
			return nil
		}
		return []string{m.BestSeller}
	}
	seen := map[string]bool{}
	var out []string
	for _, id := range m.BestSellerIDs {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// MenuCategory is one named group of products on the menu page.
type MenuCategory struct {
	Name       string   `json:"name"`
	ProductIDs []string `json:"product_ids"`
}

// MenuFromConfig reads the per-form restaurant-menu config from a form's config
// blob (issue #497). Returns nil when the form has no menu configured or the
// payload is malformed — callers then render the form normally.
func MenuFromConfig(config map[string]any) *MenuPage {
	if config == nil {
		return nil
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return nil
	}
	var cfg struct {
		Menu *MenuPage `json:"menu"`
	}
	if json.Unmarshal(raw, &cfg) != nil {
		return nil
	}
	return cfg.Menu
}

// WithMenu returns the form config with the menu config written under
// MenuConfigKey (issue #497). A nil menu removes the key, so disabling the menu
// in the dashboard clears it instead of leaving a stale blob behind. The rest
// of the config (blocks, registered_fields, chip_mapping, …) is preserved
// verbatim — the menu is one more key, never a replacement for the form.
func WithMenu(config map[string]any, menu *MenuPage) map[string]any {
	out := map[string]any{}
	for k, v := range config {
		out[k] = v
	}
	if menu == nil {
		delete(out, MenuConfigKey)
		return out
	}
	if menu.Categories == nil {
		menu.Categories = []MenuCategory{}
	}
	out[MenuConfigKey] = menu
	return out
}
