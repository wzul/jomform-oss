// Issue #497: per-form restaurant-menu settings (REST).
//
// The restaurant menu is a PER-FORM setting: the config lives on the form
// (forms.config.menu) and the menu is served at the form's own public URL. The
// Form Settings UI reads/writes it through these two endpoints, alongside the
// other per-form settings (theme, email, CHIP feeds).
//
// Before #497 the menu was a per-workspace landing template configured in
// Settings → Storefront; these endpoints are the per-form replacement.
package httpapi

import (
	"net/http"
	"strings"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// getFormMenu: GET /v1/rest/forms/{id}/menu
// Returns the form's menu config (an empty config when none is set) so the UI
// can hydrate in one round-trip.
func (h *RESTHandlers) getFormMenu(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	f, err := h.Forms.GetByID(r.Context(), actx.Account.ID, r.PathValue("id"))
	if err != nil {
		respondErr(w, r, err)
		return
	}
	menu := dbstore.MenuFromConfig(f.Config)
	if menu == nil {
		menu = &dbstore.MenuPage{}
	}
	if menu.Categories == nil {
		menu.Categories = []dbstore.MenuCategory{}
	}
	writeJSON(w, http.StatusOK, menu)
}

// putFormMenu: PUT /v1/rest/forms/{id}/menu
// Writes the menu config under forms.config.menu, preserving the form's blocks
// and every other config key (issue #497 — the menu is one more key on the
// form, never a replacement for it). enabled=false clears the menu so the form
// renders normally again.
func (h *RESTHandlers) putFormMenu(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	var in struct {
		Enabled       bool                   `json:"enabled"`
		Categories    []dbstore.MenuCategory `json:"categories"`
		BestSeller    string                 `json:"best_seller"`
		BestSellerIDs []string               `json:"best_seller_ids"`
		OrderFormSlug string                 `json:"order_form_slug"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "payload tidak sah")
		return
	}
	f, err := h.Forms.GetByID(r.Context(), actx.Account.ID, r.PathValue("id"))
	if err != nil {
		respondErr(w, r, err)
		return
	}
	var menu *dbstore.MenuPage
	if in.Enabled {
		menu = &dbstore.MenuPage{
			Enabled:       true,
			Categories:    in.Categories,
			BestSeller:    in.BestSeller,
			BestSellerIDs: in.BestSellerIDs,
			OrderFormSlug: strings.TrimSpace(in.OrderFormSlug),
		}
		if menu.Categories == nil {
			menu.Categories = []dbstore.MenuCategory{}
		}
	}
	next := dbstore.WithMenu(f.Config, menu)
	if err := h.Forms.Update(r.Context(), actx.Account.ID, f.ID, f.Title, next, nil); err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}
