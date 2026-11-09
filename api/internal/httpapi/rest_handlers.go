// REST handlers.
package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/customform"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/formfields"
	"github.com/wzul/jomform/api/internal/lifecycle"
	"github.com/wzul/jomform/api/internal/payments"
	"github.com/wzul/jomform/api/internal/textutil"
	"github.com/wzul/jomform/api/internal/urlguard"
)

// ─── Products ────────────────────────────────────────────────────────────────

// restVariation is the REST JSON shape of one product variation (money stays
// integer sen; stock nil = unlimited).
//
// Issue #623: the subscription cycle fields are POINTERS so nil/omitted is
// distinguishable from a set value — nil means "not set, inherit the product's",
// which is not the same as "set to the same value as the product". Pre-filling
// the product's cycle into every variation would erase that difference and
// silently make the product-level setting inert for those rows.
type restVariation struct {
	ID        string   `json:"id,omitempty"`
	Name      string   `json:"name"`
	PriceSen  int64    `json:"price_sen"`
	Stock     *int     `json:"stock,omitempty"` // nil = unlimited
	Images    []string `json:"images,omitempty"`
	SortOrder int      `json:"sort_order,omitempty"`
	// Issue #623: the variation's own subscription cycle. Omitted/null = inherit
	// the product's for that field.
	BillingPeriod       *string `json:"billing_period,omitempty"`       // month | year
	TrialDays           *int    `json:"trial_days,omitempty"`           // >= 0
	SubscriptionCharges *int    `json:"subscription_charges,omitempty"` // >= 1
}

type restProduct struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Description         string   `json:"description,omitempty"`
	PriceSen            int64    `json:"price_sen"`
	Images              []string `json:"images,omitempty"`
	Type                string   `json:"type"`
	BillingPeriod       string   `json:"billing_period,omitempty"`
	TrialDays           int      `json:"trial_days"`
	SubscriptionCharges *int     `json:"subscription_charges,omitempty"` // nil = infinite
	// ScheduleMode is the next-charge date policy (round 13c). Always present:
	// the column is NOT NULL DEFAULT 'from_payment'.
	ScheduleMode string `json:"schedule_mode"`
	Active       bool   `json:"active"`
	Stock        *int   `json:"stock,omitempty"` // nil = unlimited
	// Card t_06851d88: pay-what-you-want limits (custom_amount only), in
	// integer sen. max_price_sen omitted = no cap; suggested_price_sen omitted
	// = no pre-filled amount.
	MinPriceSen       int64           `json:"min_price_sen,omitempty"`
	MaxPriceSen       *int64          `json:"max_price_sen,omitempty"`
	SuggestedPriceSen *int64          `json:"suggested_price_sen,omitempty"`
	Variations        []restVariation `json:"variations,omitempty"`
	CreatedAt         string          `json:"created_at,omitempty"`
	UpdatedAt         string          `json:"updated_at,omitempty"`
}

// toRestProduct maps a store Product to the REST JSON shape (money stays
// integer sen; stock nil = unlimited; subscription_charges nil = infinite).
// Variations are included when present.
func toRestProduct(p *dbstore.Product) restProduct {
	out := restProduct{
		ID: p.ID, Name: p.Name, Description: p.Description, PriceSen: p.PriceSen,
		Images: p.Images, Type: p.Type, BillingPeriod: p.BillingPeriod,
		TrialDays: p.TrialDays, Active: p.Active,
		Stock:               p.Stock,
		SubscriptionCharges: p.SubscriptionCharges,
		ScheduleMode:        p.ScheduleMode,
		MinPriceSen:         p.MinPriceSen,
		MaxPriceSen:         p.MaxPriceSen,
		SuggestedPriceSen:   p.SuggestedPriceSen,
		CreatedAt:           p.CreatedAt.UTC().Format(time.RFC3339),
	}
	if !p.UpdatedAt.IsZero() {
		out.UpdatedAt = p.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return out
}

// validateRestVariations validates the editor's variation list (money stays
// integer sen; stock nil = unlimited). Shared by POST and PUT so the two write
// paths can never disagree about what a valid variation is.
//
// Issue #623: the variation's OWN subscription cycle is validated here too. An
// unknown period is rejected rather than silently dropped — an ignored cycle is
// how a merchant ends up billing yearly where they chose monthly.
func validateRestVariations(vs []restVariation) string {
	for _, v := range vs {
		if v.Name == "" {
			return "variation name is required"
		}
		if v.PriceSen < 0 {
			return "variation price_sen must be >= 0"
		}
		if v.Stock != nil && *v.Stock < 0 {
			return "variation stock must be >= 0"
		}
		if !urlguard.ValidList(v.Images) {
			return urlguard.InvalidMessage("variations[].images")
		}
		if v.BillingPeriod != nil && *v.BillingPeriod != "" &&
			*v.BillingPeriod != "month" && *v.BillingPeriod != "year" {
			return "variations[].billing_period must be month|year"
		}
		if v.TrialDays != nil && *v.TrialDays < 0 {
			return "variations[].trial_days must be >= 0"
		}
		if v.SubscriptionCharges != nil && *v.SubscriptionCharges < 1 {
			return "variations[].subscription_charges must be >= 1"
		}
	}
	return ""
}

// toStoreVariations maps the editor's variation list to store rows, filling a
// zero sort_order from the list position. Used by the wholesale replace on
// product create/update.
//
// Issue #623: a variation whose cycle field was omitted stores NULL for it,
// which is the "inherit the product's" state — the editor must therefore send
// only what the merchant actually set (see ProductEditor's placeholder hint).
func toStoreVariations(vs []restVariation) []*dbstore.ProductVariation {
	out := make([]*dbstore.ProductVariation, 0, len(vs))
	for i, v := range vs {
		order := v.SortOrder
		if order == 0 {
			order = i
		}
		bp := ""
		if v.BillingPeriod != nil {
			bp = *v.BillingPeriod
		}
		out = append(out, &dbstore.ProductVariation{
			Name: v.Name, PriceSen: v.PriceSen, Stock: v.Stock,
			Images: v.Images, SortOrder: order,
			BillingPeriod: bp, TrialDays: v.TrialDays, Charges: v.SubscriptionCharges,
		})
	}
	return out
}

// toRestVariations maps store variations to the REST JSON shape.
//
// Issue #623: a variation with no cycle of its own reports the cycle fields
// OMITTED (not zero-filled), so the editor can show the product's value as a
// placeholder/hint and keep "not set" distinct from "set to the same value".
func toRestVariations(vs []*dbstore.ProductVariation) []restVariation {
	if len(vs) == 0 {
		return nil
	}
	out := make([]restVariation, 0, len(vs))
	for _, v := range vs {
		out = append(out, toRestVariation(v))
	}
	return out
}

// toRestVariation maps one store variation to the REST JSON shape (see
// toRestVariations for the omitted-means-inherit rule).
func toRestVariation(v *dbstore.ProductVariation) restVariation {
	out := restVariation{
		ID: v.ID, Name: v.Name, PriceSen: v.PriceSen, Stock: v.Stock,
		Images: v.Images, SortOrder: v.SortOrder,
	}
	if v.BillingPeriod != "" {
		bp := v.BillingPeriod
		out.BillingPeriod = &bp
	}
	out.TrialDays = v.TrialDays
	out.SubscriptionCharges = v.Charges
	return out
}

func (h *RESTHandlers) listProducts(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	scope, ok := h.scopeOr403(w, r, actx)
	if !ok {
		return
	}
	includeArchived := r.URL.Query().Get("include_archived") == "1"
	var products []*dbstore.Product
	var err error
	if includeArchived {
		products, err = h.Products.ListAllByAccounts(r.Context(), scope.Accounts)
	} else {
		products, err = h.Products.ListActiveByAccounts(r.Context(), scope.Accounts)
	}
	if err != nil {
		respondErr(w, r, err)
		return
	}
	out := make([]restProduct, 0, len(products))
	for _, p := range products {
		out = append(out, toRestProduct(p))
	}
	respond(w, http.StatusOK, out)
}

// updateProduct handles PUT /v1/rest/products/{id} — partial update of all
// editable fields: name, description, price_sen, images[], trial_days, type,
// billing_period, stock, active (money stays integer sen). Omitted fields are
// left unchanged; an omitted images[] keeps the stored ones.
func (h *RESTHandlers) updateProduct(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	var in struct {
		Name                *string          `json:"name"`
		Description         *string          `json:"description"`
		PriceSen            *int64           `json:"price_sen"`
		Images              *[]string        `json:"images"` // nil = keep; [] = clear
		TrialDays           *int             `json:"trial_days"`
		Type                *string          `json:"type"`
		BillingPeriod       *string          `json:"billing_period"`
		Stock               *int             `json:"stock"` // nil = unlimited, keep-unset if omitted
		Active              *bool            `json:"active"`
		Variations          *[]restVariation `json:"variations"`           // nil = keep; [] = clear (simple)
		SubscriptionCharges **int            `json:"subscription_charges"` // nil = keep; &nil = infinite; &N = fixed count
		// round 13c: next-charge date policy after a successful charge.
		ScheduleMode *string `json:"schedule_mode"` // nil = keep
		// Card t_06851d88: custom-amount (pay-what-you-want) limits, integer
		// sen. nil = keep; ** = distinguish "sent as null" (clear the cap /
		// the suggestion) from "not sent".
		MinPriceSen       *int64  `json:"min_price_sen"`
		MaxPriceSen       **int64 `json:"max_price_sen"`
		SuggestedPriceSen **int64 `json:"suggested_price_sen"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.Name != nil && *in.Name == "" {
		writeErr(w, http.StatusBadRequest, "name cannot be empty")
		return
	}
	if in.PriceSen != nil && *in.PriceSen < 0 {
		writeErr(w, http.StatusBadRequest, "price_sen must be >= 0")
		return
	}
	if in.TrialDays != nil && *in.TrialDays < 0 {
		writeErr(w, http.StatusBadRequest, "trial_days must be >= 0")
		return
	}
	var typPtr, periodPtr *string
	if in.Type != nil {
		if *in.Type != dbstore.SaleTypeOneTime && *in.Type != dbstore.SaleTypeSubscription &&
			*in.Type != dbstore.SaleTypeCustomAmount {
			writeErr(w, http.StatusBadRequest, "type must be one_time|subscription|custom_amount")
			return
		}
		typPtr = in.Type
	}
	if in.BillingPeriod != nil && (typPtr == nil || *typPtr == dbstore.SaleTypeSubscription) {
		if *in.BillingPeriod != "month" && *in.BillingPeriod != "year" {
			writeErr(w, http.StatusBadRequest, "billing_period must be month|year")
			return
		}
		periodPtr = in.BillingPeriod
	}
	if in.Stock != nil && *in.Stock < 0 {
		writeErr(w, http.StatusBadRequest, "stock must be >= 0")
		return
	}
	// Issue #501: product images render into buyer-facing `<img src>`
	// attributes (form renderer, restaurant menu). An unsafe scheme stored
	// here would be silently rewritten to #ZgotmplZ on the buyer's page, so
	// reject it at write time with the ONE shared scheme rule.
	if in.Images != nil && !urlguard.ValidList(*in.Images) {
		writeErr(w, http.StatusBadRequest, urlguard.InvalidMessage("images"))
		return
	}
	// Validate variations (money stays integer sen; stock nil = unlimited).
	// Issue #623 also validates each variation's own subscription cycle.
	if in.Variations != nil {
		if msg := validateRestVariations(*in.Variations); msg != "" {
			writeErr(w, http.StatusBadRequest, msg)
			return
		}
	}
	if in.SubscriptionCharges != nil && *in.SubscriptionCharges != nil && **in.SubscriptionCharges < 1 {
		writeErr(w, http.StatusBadRequest, "subscription_charges must be >= 1 (or null for infinite)")
		return
	}
	// round 13c: reject an unknown schedule mode rather than silently keeping
	// the old one (an ignored setting is how a merchant ends up on the wrong
	// billing schedule without knowing).
	if in.ScheduleMode != nil && *in.ScheduleMode != "" && !payments.ValidScheduleMode(*in.ScheduleMode) {
		writeErr(w, http.StatusBadRequest,
			"invalid schedule_mode (from_payment|catch_up|skip_missed)")
		return
	}
	// Card t_06851d88: custom-amount (pay-what-you-want) limits. A partial
	// update is judged as a MERGED triple against the stored row (raising
	// min_price_sen past an old suggested_price_sen only fails as a triple), so
	// the row is read whenever the type or any limit is actually being written.
	if typPtr != nil || in.MinPriceSen != nil || in.MaxPriceSen != nil || in.SuggestedPriceSen != nil {
		cur, gerr := h.Products.Get(r.Context(), actx.Account.ID, r.PathValue("id"))
		if gerr != nil {
			respondErr(w, r, gerr)
			return
		}
		effType := cur.Type
		if typPtr != nil {
			effType = *typPtr
		}
		if effType == dbstore.SaleTypeCustomAmount {
			var setMax bool
			var newMax *int64
			if in.MaxPriceSen != nil {
				setMax, newMax = true, *in.MaxPriceSen
			}
			var setSug bool
			var newSug *int64
			if in.SuggestedPriceSen != nil {
				setSug, newSug = true, *in.SuggestedPriceSen
			}
			vals, msg := mergeCustomAmountLimits(cur.MinPriceSen, cur.MaxPriceSen, cur.SuggestedPriceSen,
				in.MinPriceSen, setMax, newMax, setSug, newSug)
			if msg != "" {
				writeErr(w, http.StatusBadRequest, msg)
				return
			}
			if in.Variations != nil && len(*in.Variations) > 0 {
				writeErr(w, http.StatusBadRequest, "a custom_amount product cannot have variations")
				return
			}
			// The merged triple is written back whole, and price_sen follows it:
			// the buyer chooses the charge, so price_sen is only the display
			// fallback and must not be editable independently.
			priceSen := vals.PriceSen
			in.PriceSen = &priceSen
			in.MinPriceSen = &vals.MinSen
			in.MaxPriceSen = &vals.MaxSen
			in.SuggestedPriceSen = &vals.SuggestSen
		} else if in.MinPriceSen != nil || in.MaxPriceSen != nil || in.SuggestedPriceSen != nil {
			writeErr(w, http.StatusBadRequest,
				"min_price_sen, max_price_sen and suggested_price_sen apply only to type custom_amount")
			return
		}
	}
	err := h.Products.Update(r.Context(), actx.Account.ID, r.PathValue("id"),
		in.Name, in.Description, in.PriceSen, in.Images, in.Stock, in.Active,
		typPtr, periodPtr, in.TrialDays, in.SubscriptionCharges, in.ScheduleMode,
		in.MinPriceSen, in.MaxPriceSen, in.SuggestedPriceSen)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	// Wholesale-replace variations when the editor sent them (nil = keep).
	if in.Variations != nil && h.ProductVariations != nil {
		if err := h.ProductVariations.ReplaceAll(r.Context(), r.PathValue("id"),
			toStoreVariations(*in.Variations)); err != nil {
			respondErr(w, r, err)
			return
		}
	}
	// Issue #92: audit product update.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "product.updated", map[string]any{"product_id": r.PathValue("id")})
	// Return the freshly-updated product (Issue #124: editor expects it).
	prod, gerr := h.Products.Get(r.Context(), actx.Account.ID, r.PathValue("id"))
	if gerr != nil {
		respond(w, http.StatusOK, map[string]string{"status": "updated"})
		return
	}
	out := toRestProduct(prod)
	if h.ProductVariations != nil {
		if vs, verr := h.ProductVariations.ListByProduct(r.Context(), prod.ID); verr == nil {
			out.Variations = toRestVariations(vs)
		}
	}
	respond(w, http.StatusOK, out)
}

// getProduct handles GET /v1/rest/products/{id} — single-product fetch for the
// editor (account-scoped). Returns the product's variations for the editor.
func (h *RESTHandlers) getProduct(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	prod, err := h.Products.Get(r.Context(), actx.Account.ID, r.PathValue("id"))
	if err != nil {
		respondErr(w, r, err)
		return
	}
	out := toRestProduct(prod)
	if h.ProductVariations != nil {
		if vs, verr := h.ProductVariations.ListByProduct(r.Context(), prod.ID); verr == nil {
			out.Variations = toRestVariations(vs)
		}
	}
	respond(w, http.StatusOK, out)
}

// deleteProduct handles DELETE /v1/rest/products/{id} — moves the product to the
// BIN (soft delete).
//
// THIS IS A CHANGE OF MEANING, and the old meaning was the defect. It used to
// call Products.Archive, i.e. active=false, so "delete" and "take it off sale"
// were the same bit and a product the merchant meant to remove still sat in the
// catalogue labelled "Archived". Taking a product off sale now has its own route
// (PUT /v1/rest/products/{id} {"active":false}); DELETE moves it to the bin,
// where it can be restored or purged.
func (h *RESTHandlers) deleteProduct(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	if err := h.Products.SoftDelete(r.Context(), actx.Account.ID, r.PathValue("id")); err != nil {
		respondErr(w, r, err)
		return
	}
	// Issue #92: audit product delete.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "product.deleted", map[string]any{"product_id": r.PathValue("id")})
	respond(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// restTrashedProduct is a bin row for a product: what the trash view renders,
// plus the pre-computed answer to "may this be destroyed yet".
type restTrashedProduct struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	PriceSen int64  `json:"price_sen"`
	Active   bool   `json:"active"`
	// DeletedAt is when the product entered the bin (RFC3339).
	DeletedAt string `json:"deleted_at"`
	// SalesCount is how many sales reference this product. Non-zero means the
	// purge will refuse (409): sales are history and the sale records the
	// product it sold.
	SalesCount int64 `json:"sales_count"`
	// SubscriptionCount is how many subscriptions still bill this product. The
	// FK is ON DELETE RESTRICT, so a purge is refused (409) while any remain.
	SubscriptionCount int64 `json:"subscription_count"`
	// CanPurge is the pre-computed answer for the UI's permanent-delete button,
	// so it can be disabled with a stated reason instead of failing after a
	// click.
	CanPurge bool `json:"can_purge"`
}

// listTrashedProducts: GET /v1/rest/products/trash
//
// The bin's read side for the workspace(s) the caller selected. It is a
// DEDICATED store query (ListTrashed), deliberately not a relaxed filter on the
// live list: loosening productSelect would expose trashed rows to every ordinary
// read (the issue #98 regression class).
func (h *RESTHandlers) listTrashedProducts(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	scope, ok := h.scopeOr403(w, r, actx)
	if !ok {
		return
	}
	products, err := h.Products.ListTrashed(r.Context(), scope.Accounts)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	sales, err := h.Products.SalesCountsByProduct(r.Context(), scope.Accounts)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	subs, err := h.Products.SubscriptionCountsByProduct(r.Context(), scope.Accounts)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	out := make([]restTrashedProduct, 0, len(products))
	for _, p := range products {
		sn, su := sales[p.ID], subs[p.ID]
		row := restTrashedProduct{
			ID: p.ID, Name: p.Name, PriceSen: p.PriceSen, Active: p.Active,
			SalesCount: sn, SubscriptionCount: su,
			CanPurge: sn == 0 && su == 0,
		}
		if p.DeletedAt != nil {
			row.DeletedAt = p.DeletedAt.UTC().Format(time.RFC3339)
		}
		out = append(out, row)
	}
	respond(w, http.StatusOK, out)
}

// restoreProduct: POST /v1/rest/products/{id}/restore
//
// Brings a trashed product back into the catalogue. No confirmation: it destroys
// nothing and a second delete undoes it. There is no collision case to refuse —
// products have no slug and no unique index — so the only failure is the row not
// being in the bin.
func (h *RESTHandlers) restoreProduct(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	productID := r.PathValue("id")
	if err := h.Products.Restore(r.Context(), actx.Account.ID, productID); err != nil {
		respondErr(w, r, err)
		return
	}
	h.recordEvent(r, actx, h.sessionOwnerID(r), "product.restored", map[string]any{"product_id": productID})
	respond(w, http.StatusOK, map[string]string{"status": "restored", "product_id": productID})
}

// purgeProduct: DELETE /v1/rest/products/{id}/purge
//
// Permanently destroys a TRASHED product (with its variations, by CASCADE).
// Confirmed by typing the product's name into {"confirm_name":"..."} — the same
// level as purgeForm's confirm_slug, because this cannot be undone. A product
// has no slug, so its NAME is the identifier the caller names back.
//
// Order of checks matters: the write gate first (so a readonly key cannot probe),
// then the confirmation, then the store's own guards (bin-only, no sales, no
// subscriptions). A live product is a 404 — the only way in is the bin.
func (h *RESTHandlers) purgeProduct(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	productID := r.PathValue("id")
	var in struct {
		ConfirmName string `json:"confirm_name"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid payload")
		return
	}
	// Read the trashed product to compare the confirmation. GetTrashed is scoped
	// to the caller's account AND to trashed rows, so this cannot be used to
	// probe a live product's existence.
	p, err := h.Products.GetTrashed(r.Context(), actx.Account.ID, productID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	// textutil.SameTypedValue trims BOTH sides. Trimming only the caller's value
	// (the shape here before card t_1ee11d1f) made a padded stored name
	// unconfirmable by anyone, the exact stored bytes included: TrimSpace can
	// never return a string with a leading/trailing space, and the create path
	// below stores the name verbatim. The caller still has to name the product.
	if !textutil.SameTypedValue(in.ConfirmName, p.Name) {
		writeErr(w, http.StatusBadRequest,
			"type the product name ("+p.Name+") to confirm the permanent deletion")
		return
	}
	if err := h.Products.Purge(r.Context(), actx.Account.ID, productID); err != nil {
		switch {
		case errors.Is(err, dbstore.ErrProductHasSales):
			n, cerr := h.Products.CountSalesByProduct(r.Context(), actx.Account.ID, productID)
			if cerr != nil {
				n = 0
			}
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":       "this product has sales — sales history cannot be destroyed by deleting the product. It stays in the bin.",
				"code":        CodeProductHasSales,
				"sales_count": n,
			})
			return
		case errors.Is(err, dbstore.ErrProductHasSubscriptions):
			n, cerr := h.Products.CountSubscriptionsByProduct(r.Context(), actx.Account.ID, productID)
			if cerr != nil {
				n = 0
			}
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":              "this product still has subscriptions — cancel that subscription first. It stays in the bin.",
				"code":               CodeProductHasSubscriptions,
				"subscription_count": n,
			})
			return
		}
		respondErr(w, r, err)
		return
	}
	h.recordEvent(r, actx, h.sessionOwnerID(r), "product.purged",
		map[string]any{"product_id": productID, "name": p.Name})
	respond(w, http.StatusOK, map[string]string{"status": "purged", "product_id": productID, "name": p.Name})
}

// bulkProducts applies an action to many products at once:
// POST /v1/rest/products/bulk {"action":"archive","ids":[uuid,...]}
// Returns {"updated": n} — how many the account actually owns and archived.
// Mirrors the forms bulk pattern (#10).
func (h *RESTHandlers) bulkProducts(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	var in struct {
		Action string   `json:"action"`
		IDs    []string `json:"ids"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.Action != "archive" {
		writeErr(w, http.StatusBadRequest, "action must be archive")
		return
	}
	if len(in.IDs) == 0 {
		writeErr(w, http.StatusBadRequest, "ids are required")
		return
	}
	if len(in.IDs) > 500 {
		writeErr(w, http.StatusBadRequest, "too many ids (max 500)")
		return
	}
	// Dedupe + keep only valid UUIDs; malformed ids are ignored, not fatal.
	seen := make(map[string]struct{}, len(in.IDs))
	ids := make([]string, 0, len(in.IDs))
	for _, raw := range in.IDs {
		id := strings.TrimSpace(raw)
		if _, err := uuid.Parse(id); err != nil {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		writeErr(w, http.StatusBadRequest, "ids must be uuid")
		return
	}
	updated, err := h.Products.ArchiveMany(r.Context(), actx.Account.ID, ids)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	// Issue #92: audit bulk product action.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "product.bulk_"+in.Action, map[string]any{
		"action": in.Action, "ids": ids, "updated": updated,
	})
	respond(w, http.StatusOK, map[string]int64{"updated": updated})
}

func (h *RESTHandlers) createProduct(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	var in struct {
		Name                string          `json:"name"`
		Description         string          `json:"description"`
		PriceSen            *int64          `json:"price_sen"`
		Type                string          `json:"type"`
		BillingPeriod       string          `json:"billing_period"`
		TrialDays           int             `json:"trial_days"`
		Stock               *int            `json:"stock"` // nil = unlimited
		Images              []string        `json:"images"`
		Variations          []restVariation `json:"variations"`
		SubscriptionCharges *int            `json:"subscription_charges"` // nil = infinite
		// round 13c: next-charge date policy after a successful charge.
		ScheduleMode *string `json:"schedule_mode"`
		// Card t_06851d88: pay-what-you-want (type=custom_amount) limits, in
		// integer sen. min_price_sen is required for that type (>= RM1.00);
		// max_price_sen null = no cap; suggested_price_sen is the pre-filled
		// amount. price_sen is DERIVED from them for this type.
		MinPriceSen       *int64 `json:"min_price_sen"`
		MaxPriceSen       *int64 `json:"max_price_sen"`
		SuggestedPriceSen *int64 `json:"suggested_price_sen"`
	}
	if err := decodeJSON(r, &in); err != nil || in.Name == "" {
		writeErr(w, http.StatusBadRequest, "name and price_sen are required")
		return
	}
	pType := in.Type
	if pType == "" {
		pType = dbstore.SaleTypeOneTime
	}
	if pType != dbstore.SaleTypeOneTime && pType != dbstore.SaleTypeSubscription &&
		pType != dbstore.SaleTypeCustomAmount {
		writeErr(w, http.StatusBadRequest, "type must be one_time|subscription|custom_amount")
		return
	}
	// Issue #551: price_sen is a POINTER so a missing or explicit `null` is
	// distinguishable from a legitimate 0. Previously a plain int64 meant
	// `{"price_sen":null}` decoded to 0 with NO error, so a client sending a
	// non-finite price (JSON.stringify(Infinity) === null) silently created a
	// RM 0.00 product which could then be sold and charged at zero.
	//
	// Card t_06851d88: a custom_amount product has NO asking price — its price
	// is derived from the limits below — so the requirement does not apply.
	if in.PriceSen == nil && pType != dbstore.SaleTypeCustomAmount {
		writeErr(w, http.StatusBadRequest, "name and price_sen are required")
		return
	}
	if in.TrialDays < 0 {
		writeErr(w, http.StatusBadRequest, "trial_days must be >= 0")
		return
	}
	if in.SubscriptionCharges != nil && *in.SubscriptionCharges < 1 {
		writeErr(w, http.StatusBadRequest, "subscription_charges must be >= 1 (or null for infinite)")
		return
	}
	// Issue #501: the product's images (and its variations' images) render
	// into buyer-facing `<img src>` attributes, so an unsafe scheme must be
	// rejected at write time — see internal/urlguard for the shared rule.
	if !urlguard.ValidList(in.Images) {
		writeErr(w, http.StatusBadRequest, urlguard.InvalidMessage("images"))
		return
	}
	// Card t_06851d88: pType was resolved above (before the price check), so a
	// custom_amount product is no longer rejected as an unknown type here.
	period := ""
	if pType == dbstore.SaleTypeSubscription {
		period = in.BillingPeriod
		if period != "month" && period != "year" {
			writeErr(w, http.StatusBadRequest, "a subscription needs billing_period month|year")
			return
		}
	}
	// Validate variations (money stays integer sen; stock nil = unlimited).
	// Issue #623 also validates each variation's own subscription cycle.
	if msg := validateRestVariations(in.Variations); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	// Card t_06851d88: a custom_amount product's price comes from its limits,
	// never from the client's price_sen — the buyer's validated amount is what
	// gets charged, and price_sen is only the display fallback (suggested,
	// else the floor).
	priceSen := int64(0)
	if in.PriceSen != nil {
		priceSen = *in.PriceSen
	}
	var minSen int64
	var maxSen, suggestSen *int64
	if pType == dbstore.SaleTypeCustomAmount {
		if in.MinPriceSen == nil {
			writeErr(w, http.StatusBadRequest,
				"custom_amount requires min_price_sen (at least 100 sen / RM1.00)")
			return
		}
		if len(in.Variations) > 0 {
			writeErr(w, http.StatusBadRequest, "a custom_amount product cannot have variations")
			return
		}
		vals, msg := mergeCustomAmountLimits(0, nil, nil,
			in.MinPriceSen, true, in.MaxPriceSen, true, in.SuggestedPriceSen)
		if msg != "" {
			writeErr(w, http.StatusBadRequest, msg)
			return
		}
		minSen, maxSen, suggestSen, priceSen = vals.MinSen, vals.MaxSen, vals.SuggestSen, vals.PriceSen
	} else if in.MinPriceSen != nil || in.MaxPriceSen != nil || in.SuggestedPriceSen != nil {
		writeErr(w, http.StatusBadRequest,
			"min_price_sen, max_price_sen and suggested_price_sen apply only to type custom_amount")
		return
	}
	schedMode := payments.ScheduleFromPayment
	if in.ScheduleMode != nil && *in.ScheduleMode != "" {
		if !payments.ValidScheduleMode(*in.ScheduleMode) {
			writeErr(w, http.StatusBadRequest,
				"invalid schedule_mode (from_payment|catch_up|skip_missed)")
			return
		}
		schedMode = *in.ScheduleMode
	}
	p := &dbstore.Product{
		AccountID: actx.Account.ID, Name: in.Name, Description: in.Description,
		PriceSen: priceSen, Images: in.Images, Type: pType,
		BillingPeriod: period, TrialDays: in.TrialDays, Active: true,
		Stock: in.Stock, SubscriptionCharges: in.SubscriptionCharges,
		ScheduleMode:      schedMode,
		MinPriceSen:       minSen,
		MaxPriceSen:       maxSen,
		SuggestedPriceSen: suggestSen,
	}
	if err := h.Products.Create(r.Context(), p); err != nil {
		respondErr(w, r, err)
		return
	}
	// Persist variations wholesale when the editor sent them.
	if len(in.Variations) > 0 && h.ProductVariations != nil {
		if err := h.ProductVariations.ReplaceAll(r.Context(), p.ID,
			toStoreVariations(in.Variations)); err != nil {
			respondErr(w, r, err)
			return
		}
	}
	out := toRestProduct(p)
	if h.ProductVariations != nil {
		if vs, verr := h.ProductVariations.ListByProduct(r.Context(), p.ID); verr == nil {
			out.Variations = toRestVariations(vs)
		}
	}
	respond(w, http.StatusCreated, out)
}

// ─── Forms ───────────────────────────────────────────────────────────────────

type restForm struct {
	ID     string `json:"id"`
	Slug   string `json:"slug"`
	Title  string `json:"title"`
	Status string `json:"status"`
	// IsPrivate reports whether the form is password-protected (issue #176).
	// The password itself is never returned.
	IsPrivate bool `json:"is_private"`
	// Mode is the form's collection mode: "payment" (default) or "response"
	// (issue #171). Two read surfaces need it in this same payload:
	//   - the forms list, so it can label a non-sales form correctly instead of
	//     offering every row a "Sales" action — before this, the list payload
	//     carried no way to tell an RSVP form from an order form, so the
	//     dashboard had to guess (and guessed "sales" for all of them);
	//   - the dedicated /sales page, which lists a response form's entries by
	//     reading that form's submissions instead of its (always empty) sales,
	//     and loads the filter dropdown from this list — without the mode here
	//     the page would have to fetch every form singly.
	Mode      string         `json:"mode"`
	URL       string         `json:"url"`
	Config    map[string]any `json:"config,omitempty"`
	Theme     map[string]any `json:"theme,omitempty"`
	UpdatedAt string         `json:"updated_at"`

	// Issue #177: display settings + restrictions surfaced on the single-form
	// GET so the dedicated Form Settings view hydrates in one round-trip.
	RedirectSuccess string `json:"redirect_success,omitempty"`
	RedirectFailure string `json:"redirect_failure,omitempty"`
	AllowEmbed      bool   `json:"allow_embed"`
	LogoURL         string `json:"logo_url,omitempty"`
	// Issue #393: per-form payment-failed message (HTML, sanitized on render).
	FailureMessage string `json:"failure_message,omitempty"`
	StartAt        string `json:"start_at,omitempty"`
	EndAt          string `json:"end_at,omitempty"`
	EntryLimit     *int   `json:"entry_limit,omitempty"`
	PendingMessage string `json:"pending_message,omitempty"`
	ExpiredMessage string `json:"expired_message,omitempty"`
	LimitMessage   string `json:"limit_message,omitempty"`

	// Per-form CHIP feed status (issue #177): whether a per-form override is
	// configured. The secret itself is never returned.
	ChipConfigured bool   `json:"chip_configured"`
	ChipBrandID    string `json:"chip_brand_id,omitempty"`
}

// fmtTime formats a *time.Time as RFC3339, or "" when nil (issue #177).
func fmtTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// strOrEmpty dereferences a *string, or "" when nil (issue #177).
func strOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// formURL builds the canonical public URL for a form. Issue #77: a public
// payment link is reachable only via its registered workspace subdomain
// ({workspace}.jomform.com/{slug}) or a custom domain — the legacy /f/{slug} form
// is gone. workspaceSlug is the account's workspace slug ("" when unresolvable, in
// which case no URL is emitted).
func formURL(workspaceSlug, slug string) string {
	if workspaceSlug == "" {
		return ""
	}
	return "https://" + workspaceSlug + ".jomform.com/" + slug
}

// formURLForAccount builds the canonical public URL for a form, preferring a
// verified custom domain over the workspace subdomain (issue #83). Returns ""
// when neither is resolvable.
//
// On a SELF-HOSTED install neither applies: there are no tenant subdomains, and
// the install serves its own forms at {APP_BASE_URL}/f/{slug} (card t_6c0badbe —
// this is the URL the builder's "Your form is live" dialog prints and "Copy
// link" copies, and the old hosted shape was a dead host there).
func (h *RESTHandlers) formURLForAccount(ctx context.Context, acc *dbstore.Account, slug string) string {
	// Self-hosted first and terminal (card t_6c0badbe): there is no tenant
	// subdomain to fall back to on this install, so an unusable origin yields
	// "" rather than the hosted shape — see PublicHandlers.formURLForAccount.
	if h.SelfHosted {
		return installFormURL(h.BaseURL, slug)
	}
	if acc == nil {
		return ""
	}
	host := h.publicHostFor(ctx, acc)
	if host == "" {
		return ""
	}
	return "https://" + host + "/" + slug
}

// publicHostFor returns the host an account's public pages are served from — a
// VERIFIED custom domain first, then {workspace-slug}.jomform.com — the repo's
// one public-URL priority (see PublicHandlers.formURLForAccount). "" when
// neither resolves.
//
// It takes the account row rather than an id because every caller that needs a
// public host for an ASSET already holds the authenticated account (the media
// list, an upload response, a purge), and re-reading it would add a query to a
// path that does not need one. A caller with only an id reads the row first;
// see mcpsrv.Server.publicHostForID.
func (h *RESTHandlers) publicHostFor(ctx context.Context, acc *dbstore.Account) string {
	if acc == nil {
		return ""
	}
	if acc.CustomDomain != "" && acc.CustomDomainVerified {
		return strings.TrimRight(acc.CustomDomain, "/")
	}
	slug := h.publicHostSlugFor(ctx, acc.ID)
	if slug == "" {
		return ""
	}
	return slug + ".jomform.com"
}

// publicHostSlugFor resolves the subdomain host an account's forms are publicly
// served under, for building {host}.jomform.com/{slug} URLs. It delegates to the
// shared resolver so every URL builder agrees with the public serving path;
// see WorkspaceStore.PublicHostSlugByAccount for why that resolver is
// workspace-only (an account-slug fallback made every workspace reachable at a
// second, invisible host).
func (h *RESTHandlers) publicHostSlugFor(ctx context.Context, accountID string) string {
	if h.Workspaces == nil {
		return ""
	}
	slug, err := h.Workspaces.PublicHostSlugByAccount(ctx, accountID)
	if err != nil {
		return ""
	}
	return slug
}

// uniqueFormSlug generates a URL-safe slug for a new form (issue #176). The
// derivation, the per-workspace uniqueness loop and the reserved-label avoidance
// live in dbstore.FormStore.UniqueSlug — ONE generator for both create doors
// (card t_f76f81eb). REST and MCP used to carry identical copies, which is how
// a generator can drift into minting a value its own validator refuses.
func (h *RESTHandlers) uniqueFormSlug(ctx context.Context, accountID, title string) string {
	return h.Forms.UniqueSlug(ctx, accountID, title)
}

// randHex returns n random bytes hex-encoded (lowercase).
func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (h *RESTHandlers) listForms(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	forms, err := h.Forms.ListByAccount(r.Context(), actx.Account.ID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	out := make([]restForm, 0, len(forms))
	for _, f := range forms {
		out = append(out, restForm{
			ID: f.ID, Slug: f.Slug, Title: f.Title, Status: f.Status,
			IsPrivate: f.Status == dbstore.FormStatusPrivate,
			Mode:      formMode(f),
			URL:       h.formURLForAccount(r.Context(), actx.Account, f.Slug),
			UpdatedAt: f.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
	respond(w, http.StatusOK, out)
}

func (h *RESTHandlers) getForm(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	f, err := h.Forms.GetByID(r.Context(), actx.Account.ID, r.PathValue("id"))
	if err != nil {
		respondErr(w, r, err)
		return
	}
	// Issue #177: surface the per-form CHIP feed status (configured / not).
	chipConfigured := false
	chipBrandID := ""
	if b, s, _, _, oerr := h.Forms.ChipOverrideFor(r.Context(), actx.Account.ID, f.ID); oerr == nil && b != "" && s != "" {
		chipConfigured = true
		chipBrandID = b
	}
	respond(w, http.StatusOK, restForm{
		ID: f.ID, Slug: f.Slug, Title: f.Title, Status: f.Status,
		IsPrivate: f.Status == dbstore.FormStatusPrivate,
		URL:       h.formURLForAccount(r.Context(), actx.Account, f.Slug), Config: f.Config, Theme: f.Theme,
		UpdatedAt:       f.UpdatedAt.UTC().Format(time.RFC3339),
		RedirectSuccess: f.RedirectSuccess, RedirectFailure: f.RedirectFailure,
		AllowEmbed: f.AllowEmbed, LogoURL: f.LogoURL, FailureMessage: f.FailureMessage,
		StartAt: fmtTime(f.StartAt), EndAt: fmtTime(f.EndAt), EntryLimit: f.EntryLimit,
		PendingMessage: strOrEmpty(f.PendingMessage), ExpiredMessage: strOrEmpty(f.ExpiredMessage),
		LimitMessage:   strOrEmpty(f.LimitMessage),
		ChipConfigured: chipConfigured, ChipBrandID: chipBrandID,
		Mode: dbstore.FormMode(f),
	})
}

// workspaceRequiredMessage is the wire refusal for a form CREATE from an account
// with no live workspace (card t_6c0badbe). Declared as a constant so the
// error-code table in errcode.go resolves it by the same literal the handler
// writes — a re-wording cannot silently detach the code.
const workspaceRequiredMessage = "this account has no workspace yet — create a workspace before creating a form"

// requireFormMint is the ONE gate every REST door that MINTS a form row calls:
// the write role, then a live workspace for the account. It reports whether the
// handler may continue, and it writes the refusal itself.
//
// Card t_6c0badbe put the workspace half inside createForm; card t_d07c8fbb
// found the same hole on the two REST template-CLONE doors
// (POST /v1/templates/use and POST /v1/rest/templates/{id}/use), which also call
// h.Forms.Create. Rather than copy a 20-line guard a third time, both halves now
// live here and the three handlers name ONE gate — which is also the shape this
// package's contract parity expects (openapi_parity_test.go classifies a route
// by the single gate its handler calls; requireFormMint is registered there as
// `write`). The two MCP twins (upsert_form's create half, use_template) answer
// the identical sentence via mcpsrv.requireLiveWorkspace, so one account state
// gets one answer on every door.
//
// ORDER: the write gate runs FIRST, so a readonly key still gets the 403 the
// published contract promises for a write route; the account's workspace state
// is only consulted for a caller who may write at all.
//
// `accounts.workspace_id` is nullable — a signup creates an account and no
// workspace (#718, ask-first onboarding) — and the forms_sync_workspace_id
// trigger (migration 000045) copies that NULL onto the form row. A door that
// accepted the write anyway produced a form that could be PUBLISHED and was then
// unreachable forever, because every public serving path resolves a form through
// its workspace (host, or the workspace behind the slug on a self-hosted
// install). Measured on a clean self-host run: two forms (`clean-test`,
// `clean-test-2`) left `published` with `workspace_id IS NULL`, both 404 at
// /f/<slug>.
//
// REFUSE rather than silently pick a workspace: choosing one for the caller
// would put a merchant's form in a workspace they never named, and the
// dashboard's own flow always creates the workspace first. 409 (not 400) — the
// request is well formed; the ACCOUNT's state is what cannot carry it out, and
// the client can retry unchanged once a workspace exists.
//
// A nil store (only the package's bare-mode handlers omit it) is not resolvable
// here; the production literal always sets it, and
// TestWorkspacesReachesRESTHandlersInMainGo pins that hop, the same way
// selfhost_flag_wiring_test.go pins PublicHandlers.SelfHosted.
func (h *RESTHandlers) requireFormMint(w http.ResponseWriter, r *http.Request, actx *AccountCtx) bool {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return false
	}
	return h.refuseWithoutLiveWorkspace(w, r, actx)
}

// refuseWithoutLiveWorkspace is the workspace half of the address gate: it
// reports whether the account can give a form a public ADDRESS today, and
// writes the refusal when it cannot. It is the same rule requireFormMint uses
// for a CREATE, split out because a PUBLISH needs it too — and needs it for the
// same reason (card t_fadfe9f7).
//
// WHY PUBLISH NEEDS IT. A form row whose `workspace_id` is NULL has no address
// at all: every public serving path resolves a form through its workspace
// (subdomain, custom domain, or — on a self-hosted install — BySlugAnyWorkspace,
// which joins accounts→workspaces), and the apex /f/<slug> is refused by design
// (issue #77). `publish_form` and the REST publish/status routes accepted such a
// row and answered `status: published`, so the merchant was told the page was
// live while it was 404 everywhere, forever. No new orphan can be MINTED since
// t_6c0badbe, but the rows that predate it are still one call away from that
// outcome.
//
// WHY THE ACCOUNT, NOT THE ROW. `forms.workspace_id` is maintained from the
// account: the forms_sync_workspace_id trigger copies accounts.workspace_id on
// every insert (migration 000045) and WorkspaceStore.CreateForAccount backfills
// every NULL row when the account is linked. So "the account has no live
// workspace" is the state that makes the row (and every row it owns)
// unaddressable, and asking it here also refuses a form whose workspace was
// SOFT-DELETED — publishing that one is the same dead end, and HasLiveWorkspace
// is the one query that answers both.
//
// The status gate is the caller's: refuse only when the form is ENTERING a live
// status (dbstore.IsLiveStatus). Moving an orphan back to draft stays allowed —
// that is the remedy, not a second orphan.
//
// THE ONE CLASS WITH AN ADDRESS BUT NO WORKSPACE (card t_8b185f34). A gallery
// template is NOT an orphan. Its public address is the GALLERY —
// GET /v1/templates serves `is_template AND status='published'` with no
// account or workspace filter — and the account that owns the gallery
// (templates@system.jomform) must never have a workspace
// (dbstore.LegacyProvisionedEmails, migration 000059: giving it one would
// re-expose every template as a public storefront and empty the picker). So a
// template entering `published` skips this check via hasGalleryAddress; every
// other row, and every other live status on a template (unlisted/private are
// in no public list and are served only through a workspace), still needs one.
// Refusing the class outright was the accident this card fixes: the refusal's
// own remedy ("buat workspace dahulu") is the one thing that account may never
// do.
//
// Nil store (bare-mode test fixtures only) means "no guard here"; the production
// literal always sets Workspaces and TestWorkspacesReachesRESTHandlersInMainGo
// pins that hop. The name deliberately does not start with `require`: the
// openapi-parity classifier treats every gate a contracted handler names
// directly as that route's access level, and this route must keep answering to
// requireWriteRole (a readonly key is still 403, not 409). This check runs
// AFTER the form is resolved, so a missing/foreign id keeps answering 404.
func (h *RESTHandlers) refuseWithoutLiveWorkspace(w http.ResponseWriter, r *http.Request, actx *AccountCtx) bool {
	if h.Workspaces == nil {
		return true
	}
	has, werr := h.Workspaces.HasLiveWorkspace(r.Context(), actx.Account.ID)
	if werr != nil {
		// A failing lookup is not "no workspace": refuse loudly instead of
		// telling the merchant to create one they may already have.
		writeErr(w, http.StatusInternalServerError, "failed to check the workspace")
		return false
	}
	if !has {
		writeErr(w, http.StatusConflict, workspaceRequiredMessage)
		return false
	}
	return true
}

// hasGalleryAddress reports whether this write gives the row a public address
// WITHOUT a workspace. Exactly one class does: an `is_template` row entering
// `published`, because the gallery is a template's address (GET /v1/templates
// lists `is_template AND status='published'` with no account or workspace
// filter). The account that owns the gallery — templates@system.jomform — must
// NEVER have a workspace (dbstore.LegacyProvisionedEmails, migration 000059),
// so without this exemption the workspace gate locks out the one account whose
// rows are addressable without one (card t_8b185f34).
//
// Every other row, and every other live status on a template, still needs a
// workspace: `unlisted`/`private` templates are in no public list and are
// served only through their workspace (PublishedByWorkspaceSlug does not filter
// is_template).
func hasGalleryAddress(f *dbstore.Form, target string) bool {
	return f != nil && f.IsTemplate && target == dbstore.FormStatusPublished
}

func (h *RESTHandlers) createForm(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	// Card t_6c0badbe: an account with NO live workspace cannot mint a form.
	if !h.requireFormMint(w, r, actx) {
		return
	}
	var in struct {
		Slug   string         `json:"slug"`
		Title  string         `json:"title"`
		Config map[string]any `json:"config"`
		Theme  map[string]any `json:"theme"`
	}
	if err := decodeJSON(r, &in); err != nil || in.Title == "" {
		writeErr(w, http.StatusBadRequest, "title is required")
		return
	}
	// Issue #388 (parity with updateForm, card t_62190342): power-user custom JS
	// is scanned here too, with the same deterministic scan and the same
	// sentence. createForm is the only write path that MINTS a form, and it was
	// the only one that skipped the scan — a POST stored a value that the PATCH
	// route (and both MCP doors) refuse with 400, so one rule answered two ways
	// on the two halves of the same REST surface. Since v1.21.283 the
	// block-rendered buyer page serves that value as live code, so an unscanned
	// create is served code, not a quoted no-op.
	if in.Theme != nil {
		if js, ok := in.Theme["custom_js"].(string); ok {
			if reason := customform.ScanCustomJS(js); reason != "" {
				writeErr(w, http.StatusBadRequest, customform.RejectedMessage(reason))
				return
			}
		}
	}
	// Issue #501: a form created with a non-https theme logo_url or a
	// non-https config URL (builder image-block src, product snapshot image)
	// would render as #ZgotmplZ on the buyer's page. Apply the ONE shared
	// scheme rule here too — the form create/update route accepts the config
	// and theme maps verbatim.
	if field, _ := urlguard.CheckTheme(in.Theme); field != "" {
		writeErr(w, http.StatusBadRequest, urlguard.InvalidMessage("theme."+field))
		return
	}
	if field, _ := urlguard.CheckConfig(in.Config); field != "" {
		writeErr(w, http.StatusBadRequest, urlguard.InvalidMessage("config."+field))
		return
	}
	// Card t_8fcb546d: a present-but-not-array `config.blocks` is refused here.
	// It used to pass this point unnoticed (the scheme rule's type assertion
	// simply does not match a non-array), and a template carrying it then made
	// the gallery-metadata predicate raise SQLSTATE 22023 for every id in a
	// bulk publish.
	if field, want := urlguard.CheckConfigShape(in.Config); field != "" {
		writeErr(w, http.StatusBadRequest, urlguard.InvalidShapeMessage("config."+field, want))
		return
	}
	// Card t_4086b8f0: a block type no renderer can honour (`file` — see
	// formfields.UnsupportedBlock for why there is no upload path) is refused
	// here rather than stored, because the buyer's page either drops the block
	// silently (response mode) or paints a control whose bytes go nowhere
	// (payment mode).
	if bt := formfields.UnsupportedBlock(in.Config); bt != "" {
		writeErr(w, http.StatusBadRequest, formfields.UnsupportedBlockMessage(bt))
		return
	}
	// Card t_3d11f98d: an option/shipping block whose price the config cannot
	// resolve would be served as a free add-on and charged 0 sen — the buyer
	// sees no price and the sale records none. Refused at storage, like the
	// unsupported-block rule above (see formfields.UnpricedPricingBlock).
	if msg := formfields.UnpricedPricingBlock(in.Config); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	// Card t_73a5db6e: a custom-HTML form's registered `required` fields are
	// enforced at /order, and the buyer's page can only supply a value for a
	// control that exists in the sanitized HTML. A config whose required field
	// has no such control is therefore unsubmittable, and storing it silently
	// is the merchant trap this refuses (REST half).
	if err := customform.ValidateAndNormalizeConfig(in.Config); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// Issue #176: auto-generate a unique slug when the merchant leaves it
	// blank. The slug is unique per workspace (#77), so a generated slug that
	// collides gets a random suffix appended until it is free.
	slug := strings.ToLower(strings.TrimSpace(in.Slug))
	if slug == "" {
		slug = h.uniqueFormSlug(r.Context(), actx.Account.ID, in.Title)
	}
	f := &dbstore.Form{
		AccountID: actx.Account.ID, Slug: slug, Title: in.Title,
		Config: orEmptyMap(in.Config), Theme: orEmptyMap(in.Theme), Status: dbstore.FormStatusDraft,
	}
	if err := h.Forms.Create(r.Context(), f); err != nil {
		respondErr(w, r, err)
		return
	}
	// Issue #193: pre-create the default notification for the form's mode —
	// sale.paid for a payment form, response.submitted for a response form
	// (issue #597) — so a new form sends the merchant an email with zero
	// configuration. Best-effort.
	if h.Notifications != nil {
		_ = h.Notifications.EnsureDefault(r.Context(), f)
	}
	// Issue #92: audit form creation.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "form.created", map[string]any{"form_id": f.ID, "slug": f.Slug})
	respond(w, http.StatusCreated, restForm{
		ID: f.ID, Slug: f.Slug, Title: f.Title, Status: f.Status,
		URL: h.formURLForAccount(r.Context(), actx.Account, f.Slug), UpdatedAt: f.UpdatedAt.UTC().Format(time.RFC3339),
		Mode: dbstore.FormMode(f),
	})
}

func (h *RESTHandlers) updateForm(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	var in struct {
		Title  string         `json:"title"`
		Slug   string         `json:"slug"`
		Config map[string]any `json:"config"`
		Theme  map[string]any `json:"theme"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	// Issue #388: power-user custom JS is owner-gated (write role above) and
	// scanned with the deterministic code-asset scan (#364) so obviously
	// malicious JS is never served on the buyer's page.
	if in.Theme != nil {
		if js, ok := in.Theme["custom_js"].(string); ok {
			if reason := customform.ScanCustomJS(js); reason != "" {
				writeErr(w, http.StatusBadRequest, customform.RejectedMessage(reason))
				return
			}
		}
		// Issue #471: product image size + layout are validated against the
		// renderer's allowlist so a bad value never reaches the buyer's page.
		if v, ok := in.Theme["product_image_size"].(string); ok && !ValidProductImageSize(v) {
			writeErr(w, http.StatusBadRequest, "product_image_size must be small|medium|large|hidden")
			return
		}
		if v, ok := in.Theme["product_image_layout"].(string); ok && !ValidProductImageLayout(v) {
			writeErr(w, http.StatusBadRequest, "product_image_layout must be thumbnail-left|image-top")
			return
		}
		// Issue #501: the theme's logo_url is rendered into a buyer-facing
		// `<img src>`; reject a non-https scheme with the shared rule.
		if field, _ := urlguard.CheckTheme(in.Theme); field != "" {
			writeErr(w, http.StatusBadRequest, urlguard.InvalidMessage("theme."+field))
			return
		}
	}
	// Issue #501: the config carries the builder image-block src and the
	// product snapshot images — both render into `<img src>` attributes
	// (render_blocks.go). Reject an unsafe scheme with the shared rule.
	if field, _ := urlguard.CheckConfig(in.Config); field != "" {
		writeErr(w, http.StatusBadRequest, urlguard.InvalidMessage("config."+field))
		return
	}
	// Card t_8fcb546d: same present-but-not-array `config.blocks` refusal as
	// createForm.
	if field, want := urlguard.CheckConfigShape(in.Config); field != "" {
		writeErr(w, http.StatusBadRequest, urlguard.InvalidShapeMessage("config."+field, want))
		return
	}
	// Card t_4086b8f0: same unsupported-block refusal as createForm — an update
	// that stores a `file` block reintroduces the block the buyer's page cannot
	// serve (and, on an already-stored form, is how the merchant clears it).
	if bt := formfields.UnsupportedBlock(in.Config); bt != "" {
		writeErr(w, http.StatusBadRequest, formfields.UnsupportedBlockMessage(bt))
		return
	}
	// Card t_3d11f98d: same unresolved-pricing refusal as createForm — an
	// update that stores an option/shipping block with no usable price
	// reintroduces the free-but-shown-as-priced add-on.
	if msg := formfields.UnpricedPricingBlock(in.Config); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	// Card t_73a5db6e: same unsatisfiable-custom-form refusal as createForm. A
	// PATCH that rewrites the config is the second way an unsubmittable
	// contract reaches storage; an omitted config (nil) leaves the stored
	// custom_html alone and is not re-validated.
	if err := customform.ValidateAndNormalizeConfig(in.Config); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// The slug write runs FIRST, and the order IS the point. RenameSlug
	// validates the whole rename before it writes anything — a malformed slug
	// is 400, a collision 409 (respondErr maps ErrSlugTaken), a missing form
	// 404 — so a REFUSED rename now leaves the save untouched. Issued second it
	// did not: the 409 came back after the title/config write had already
	// committed, so the dashboard's own save (form-settings page.tsx always
	// sends a title) changed the form's title and then told the merchant the
	// save had failed, with the new title still in the page's input beside the
	// refusal — read as "not saved" while the row had moved (card t_0b7980f5,
	// browser-observed on prod). The two writes are still two statements rather
	// than one transaction; what this removes is the only refusal that can
	// happen BETWEEN them.
	//
	// An OMITTED slug keeps the stored value — the same "leave unchanged"
	// contract config/theme (and title, below) have — because the empty string
	// is skipped here. That is what the console's form-settings save sends for
	// every other field, and what makes the documented PATCH a partial update
	// rather than a full replace.
	//
	// Before the slug was read here at all, the field was ignored: the console
	// POSTed a slug, the endpoint dropped it, and the save reported
	// {status:"updated"} while the form's URL had not moved. A merchant
	// concluded the rename worked and his new URL existed — it 404'd (the
	// carded defect).
	formID := r.PathValue("id")
	if in.Slug != "" {
		oldSlug, err := h.Forms.RenameSlug(r.Context(), actx.Account.ID, formID, in.Slug)
		if err != nil {
			// A collision is 409 and an invalid slug 400 — never a 200 that
			// changed nothing. Because this runs BEFORE the field write, the
			// refusal also leaves title/config/theme untouched.
			respondErr(w, r, err)
			return
		}
		if oldSlug != "" {
			h.recordEvent(r, actx, h.sessionOwnerID(r), "form.slug_renamed",
				map[string]any{"form_id": formID, "from": oldSlug, "to": strings.ToLower(strings.TrimSpace(in.Slug))})
		}
	}
	// Pass config/theme as-is: Forms.Update COALESCEs nil over the stored
	// values, so a PATCH that omits either key keeps what's stored instead
	// of writing NULL into a NOT NULL jsonb column (SQLSTATE 23502, issue
	// #16). Callers that want to clear a map send {} explicitly.
	//
	// TITLE follows the same contract (card t_0b7980f5): an omitted title
	// decodes to "" and the statement keeps the stored value for an empty or
	// whitespace-only one, so a slug-only body can no longer return 200 and
	// empty a live form's title. This endpoint therefore cannot clear a title
	// at all; a form is created titleless (or not) by createForm.
	if err := h.Forms.Update(r.Context(), actx.Account.ID, formID,
		in.Title, in.Config, in.Theme); err != nil {
		respondErr(w, r, err)
		return
	}
	// Issue #92: audit form update.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "form.updated", map[string]any{"form_id": formID})
	// The response reports the slug the form actually owns now (read back, not
	// echoed) so a client can render the moved URL without a second round trip,
	// and so a caller can never be told about a URL that does not exist.
	respond(w, http.StatusOK, map[string]string{
		"status": "updated",
		"slug":   h.currentFormSlug(r.Context(), actx.Account.ID, formID),
	})
}

// currentFormSlug reads a form's stored slug for a response payload. It reports
// the value the database holds rather than the value the request carried, so a
// caller is never told about a URL the form does not serve. "" when the form
// cannot be read (the update above already proved it exists, so this is only a
// best-effort enrichment and never an error path).
func (h *RESTHandlers) currentFormSlug(ctx context.Context, accountID, id string) string {
	f, err := h.Forms.GetByID(ctx, accountID, id)
	if err != nil || f == nil {
		return ""
	}
	return f.Slug
}

func (h *RESTHandlers) publishForm(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	// Publishing requires CHIP configured OR at least one product; CHIP-only store
	// means a form without CHIP setup can still publish for bank-direct? Per spec:
	// CHIP-only. Warn but allow (bank_direct exists).
	id := r.PathValue("id")
	// Onboarding gap: a PAYMENT form with nothing to sell publishes fine and can
	// never take money. It is not blocked — bank-direct and a later product edit
	// are both legitimate — but it may not publish SILENTLY: the caller is told
	// and has to acknowledge (`{"acknowledge":true}` body or `?acknowledge=1`).
	// A response form and an enabled-menu form are never affected (see
	// dbstore.PaymentFormWithoutProducts).
	//
	// Gated on the TRANSITION (dbstore.PublishNeedsAck), not on the raw rule: a
	// form that is already live must not ask again on every subsequent publish
	// call, or the dashboard would nag a merchant who publishes an empty payment
	// form on purpose.
	f, err := h.Forms.GetByID(r.Context(), actx.Account.ID, id)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	// Card t_fadfe9f7: a form with no workspace has no address, so a publish
	// that answered 200 would be a lie — the row would be `published` and 404
	// at every URL forever. Refuse with the sentence the create door uses.
	// AFTER the read, so a missing/foreign id still answers 404, and before the
	// acknowledgement, so a payment form with nothing to sell is not asked to
	// confirm a publish that cannot happen.
	// A gallery template is the exception (t_8b185f34): publishing it is what
	// puts it in GET /v1/templates, so it has an address with no workspace.
	if !hasGalleryAddress(f, dbstore.FormStatusPublished) && !h.refuseWithoutLiveWorkspace(w, r, actx) {
		return
	}
	if dbstore.PublishNeedsAck(f, dbstore.FormStatusPublished) && !publishAcknowledged(r) {
		writePublishNeedsAck(w)
		return
	}
	if err := h.Forms.SetStatus(r.Context(), actx.Account.ID, id, dbstore.FormStatusPublished); err != nil {
		respondErr(w, r, err)
		return
	}
	// Issue #92: audit form publish.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "form.published", map[string]any{"form_id": id})
	// No edge purge here, deliberately: publishing moves the form INTO the
	// publicly-cached set, where nothing cacheable was before — the edge holds
	// the 404 the card answered while the form was not live, and Cloudflare
	// expires that on its own few-minute default (measured: the 200 takes over
	// 1m09s–3m23s after a publish). A purge would only make that flip faster at
	// the cost of a CDN call on every publish.
	respond(w, http.StatusOK, map[string]string{"status": "published"})
}

// writePublishNeedsAck answers a publish that a payment form with no products
// cannot complete silently. The status is 409 because nothing was changed: the
// caller asked for a state it has to confirm first. `code` is the stable token
// the dashboard matches on (web/src/lib/publishResult.ts holds the same value),
// so it never has to parse the message.
//
// The prose says BOTH halves of the truth, because an agent/curl client reads
// it and has no dictionary to fall back on (t_39906c0a): such a form cannot take
// money AND collects nothing (both the legacy and the block payment layouts gate
// submit on a selected product), so acknowledging the warning is not a way to
// collect answers. The dashboard renders its own 5-locale sentence from
// `bld.noProductsWarn` and never prints this prose.
func writePublishNeedsAck(w http.ResponseWriter) {
	writeJSON(w, http.StatusConflict, map[string]any{
		"error": "This payment form has no products — it cannot accept payment " +
			"and will not collect anything. Add a product first, or confirm to continue.",
		"code":         dbstore.PublishWarningNoProducts,
		"requires_ack": true,
	})
}

// publishAcknowledged reports whether the caller explicitly accepted the
// "payment form with no products" warning: `{"acknowledge":true}` in the body
// (the dashboard's path) or `?acknowledge=1` on the URL (a curl/agent path that
// has no body to send).
func publishAcknowledged(r *http.Request) bool {
	if r.URL.Query().Get("acknowledge") == "1" {
		return true
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil || len(b) == 0 {
		return false
	}
	var in struct {
		Acknowledge bool `json:"acknowledge"`
	}
	return json.Unmarshal(b, &in) == nil && in.Acknowledge
}

// setFormStatus sets a form's status (draft | private | published) and, when
// private, its access password (issue #176). The password is hashed with
// argon2id before storage; it is never returned by any endpoint.
// POST /v1/rest/forms/{id}/status {"status":"private","password":"..."}
func (h *RESTHandlers) setFormStatus(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	var in struct {
		Status   string `json:"status"`
		Password string `json:"password"`
		// Acknowledge accepts the "payment form with nothing to sell" warning.
		// The publish route reads it from the request body instead, because it
		// takes no JSON input (see publishAcknowledged).
		Acknowledge bool `json:"acknowledge"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	id := r.PathValue("id")
	// ONE read of the CURRENT row, before the write. It decides two things: the
	// "payment form with nothing to sell" acknowledgement is a TRANSITION rule
	// (dbstore.PublishNeedsAck), and whether the edge holds publicly cached
	// artefacts (share card, QR PNG) for this form depends on the status it is
	// LEAVING (dbstore.OGImagePubliclyCached) — reading after the write would lose
	// that.
	f, ferr := h.Forms.GetByID(r.Context(), actx.Account.ID, id)
	if ferr != nil {
		respondErr(w, r, ferr)
		return
	}
	// Card t_fadfe9f7: entering a LIVE status (published/unlisted/private) is a
	// publish by another name — the dashboard's status control reaches THIS
	// route — so the same workspace precondition applies here, or the guard on
	// /publish is one header away from being bypassed. Back to draft stays
	// allowed: that is how an existing orphan is taken out of a live state.
	// A gallery template entering `published` is exempt (t_8b185f34): the
	// gallery is its address. Private/unlisted templates stay gated — neither
	// status is in any public list, so without a workspace they have no address.
	if dbstore.IsLiveStatus(in.Status) && !hasGalleryAddress(f, in.Status) && !h.refuseWithoutLiveWorkspace(w, r, actx) {
		return
	}
	// Onboarding gap: entering a LIVE status with a payment form that has
	// nothing to sell is the same silent mistake the publish route refuses, and
	// the dashboard's status control reaches THIS route. Gated on the
	// transition, so re-saving a form that is already live is never blocked.
	if dbstore.IsLiveStatus(in.Status) {
		if dbstore.PublishNeedsAck(f, in.Status) && !in.Acknowledge && r.URL.Query().Get("acknowledge") != "1" {
			writePublishNeedsAck(w)
			return
		}
	}
	switch in.Status {
	case dbstore.FormStatusDraft, dbstore.FormStatusPublished, dbstore.FormStatusUnlisted:
		// Leaving private clears the stored access password.
		if err := h.Forms.SetStatusWithPassword(r.Context(), actx.Account.ID, id, in.Status, ""); err != nil {
			respondErr(w, r, err)
			return
		}
	case dbstore.FormStatusPrivate:
		if len(in.Password) < 4 {
			writeErr(w, http.StatusBadRequest, "the password must be at least 4 characters")
			return
		}
		hash, err := auth.HashPassword(in.Password)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to hash the password")
			return
		}
		if err := h.Forms.SetStatusWithPassword(r.Context(), actx.Account.ID, id, in.Status, hash); err != nil {
			respondErr(w, r, err)
			return
		}
	default:
		writeErr(w, http.StatusBadRequest, "status must be draft|private|published")
		return
	}
	// THE EDGE. The row is written above; the artefacts the CDN cached while this
	// form was published or unlisted — its share card and its QR PNG — are
	// dropped here, in the same request. Without this they outlive the status
	// change by their own freshness lifetime: measured on prod as a DRAFT form's
	// card still answering `cf-cache-status: HIT, age: 1243` while the
	// cache-busted origin answered 404 (naming the form's title, workspace and
	// host), and its QR PNG answering `HIT, age: 1566` while the origin answered
	// 404 (confirming the form exists at that slug). See og_edge_purge.go.
	var leaving []string
	if dbstore.OGImagePubliclyCached(f.Status) && !dbstore.OGImagePubliclyCached(in.Status) {
		leaving = []string{f.Slug}
	}
	edge := h.purgeLeftPublicArtefacts(r.Context(), actx.Account, leaving)
	// Issue #92: audit form status change. A purge that did not happen is
	// recorded too, so the activity log can tell a cleared edge from one that
	// is still serving the old card.
	audit := map[string]any{"form_id": id}
	if edge.Status != "" {
		audit["edge"] = edge.Status
	}
	h.recordEvent(r, actx, h.sessionOwnerID(r), "form.status_"+in.Status, audit)
	respond(w, http.StatusOK, withEdgeFields(map[string]any{"status": in.Status}, edge))
}

// deleteForm soft-deletes a single form (owner-checked via account scope).
func (h *RESTHandlers) deleteForm(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	id := r.PathValue("id")
	// The status it was in decides whether the edge holds a publicly cached
	// social card for it; a TRASHED form stops resolving entirely
	// (findPublicForm scopes on deleted_at IS NULL), so its card 404s at the
	// origin and must be dropped from the edge (see og_edge_purge.go).
	f, ferr := h.Forms.GetByID(r.Context(), actx.Account.ID, id)
	if ferr != nil {
		respondErr(w, r, ferr)
		return
	}
	if err := h.Forms.SoftDelete(r.Context(), actx.Account.ID, id); err != nil {
		respondErr(w, r, err)
		return
	}
	var leaving []string
	if dbstore.OGImagePubliclyCached(f.Status) {
		leaving = []string{f.Slug}
	}
	edge := h.purgeLeftPublicArtefacts(r.Context(), actx.Account, leaving)
	// Issue #92: audit form delete.
	audit := map[string]any{"form_id": id}
	if edge.Status != "" {
		audit["edge"] = edge.Status
	}
	h.recordEvent(r, actx, h.sessionOwnerID(r), "form.deleted", audit)
	respond(w, http.StatusOK, withEdgeFields(map[string]any{"status": "deleted"}, edge))
}

// ── Tong sampah (trash): restore + purge ─────────────────────────────────────
//
// Wan's decision on the card: keep the "can be restored later" promise the bulk
// delete already makes, and make it TRUE. So a trashed form has two exits:
//
//	restore  — clears deleted_at. Touches no CASCADE at all, so it is completely
//	           safe and reversible; a second click can trash it again.
//	purge    — destroys the row for good, taking submissions, notifications,
//	           confirmations, per-form email settings and form-scoped webhooks
//	           with it by CASCADE. Irreversible, so it is confirmed by TYPING THE
//	           SLUG (same level as revoke_api_key's confirm), and it refuses
//	           outright when the form has ever been sold.
//
// WHY THE PURGE GUARD EXISTS: sales.form_id is ON DELETE SET NULL, so purging a
// sold form does NOT fail — it silently orphans the sale, detaching the payment
// record from the form that produced it. Sales are history; deleting the form
// that produced them must never destroy or detach them.

// restTrashedForm is a trash row: the form fields the trash list renders, plus
// when it was trashed and whether a purge is currently allowed.
type restTrashedForm struct {
	ID    string `json:"id"`
	Slug  string `json:"slug"`
	Title string `json:"title"`
	// DeletedAt is when the form entered the trash (RFC3339).
	DeletedAt string `json:"deleted_at"`
	// SalesCount is how many sales reference this form. Non-zero means purge
	// will refuse (409): those sales would be silently orphaned.
	SalesCount int64 `json:"sales_count"`
	// CanPurge is the pre-computed answer for the UI's "Padam kekal" button, so
	// it can be disabled with a stated reason instead of failing after a click.
	CanPurge bool `json:"can_purge"`
}

// listTrashedForms: GET /v1/rest/forms/trash
//
// The trash listing — the read side of restore/purge. It is a DEDICATED store
// query (ListTrashed), deliberately not a relaxed filter on the normal list:
// loosening formSelect would expose soft-deleted rows to every ordinary read
// (the issue #98 regression class).
func (h *RESTHandlers) listTrashedForms(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	forms, err := h.Forms.ListTrashed(r.Context(), actx.Account.ID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	counts, err := h.Forms.SalesCountsByForm(r.Context(), actx.Account.ID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	out := make([]restTrashedForm, 0, len(forms))
	for _, f := range forms {
		n := counts[f.ID]
		out = append(out, restTrashedForm{
			ID: f.ID, Slug: f.Slug, Title: f.Title,
			DeletedAt:  f.DeletedAt.UTC().Format(time.RFC3339),
			SalesCount: n,
			CanPurge:   n == 0,
		})
	}
	respond(w, http.StatusOK, out)
}

// restoreForm: POST /v1/rest/forms/{id}/restore
//
// Brings a trashed form back. No confirmation: it destroys nothing and a second
// click undoes it.
//
// A 409 with a clear message when the slug was re-claimed while the form sat in
// the trash — the per-workspace unique index covers live rows only, so trashing
// releases the slug and a new form may take it. Nothing is renamed behind the
// merchant's back (that would silently move a public URL they were told about);
// they are told which obstacle to clear.
func (h *RESTHandlers) restoreForm(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	formID := r.PathValue("id")
	if err := h.Forms.Restore(r.Context(), actx.Account.ID, formID); err != nil {
		if errors.Is(err, dbstore.ErrFormSlugTaken) {
			writeErr(w, http.StatusConflict,
				"cannot be restored: this form's slug has been taken by another form — change that form's slug first, then try restoring again")
			return
		}
		respondErr(w, r, err)
		return
	}
	h.recordEvent(r, actx, h.sessionOwnerID(r), "form.restored", map[string]any{"form_id": formID})
	respond(w, http.StatusOK, map[string]string{"status": "restored", "form_id": formID})
}

// purgeForm: DELETE /v1/rest/forms/{id}/purge
//
// Permanently destroys a TRASHED form. Confirmed by typing the form's slug into
// {"confirm_slug":"..."} — the same level as revoke_api_key, because this one
// cannot be undone and it takes the form's CASCADE children with it.
//
// Order of checks matters: the write gate first (so a readonly key cannot probe),
// then the confirmation, then the store's own two guards (trashed-only, and
// never a sold form). A live form is a 404 here — the only way in is trash.
func (h *RESTHandlers) purgeForm(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	formID := r.PathValue("id")
	var in struct {
		ConfirmSlug string `json:"confirm_slug"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid payload")
		return
	}
	// Read the trashed form to compare the confirmation slug. GetTrashed is
	// scoped to the caller's account AND to deleted rows, so this cannot be
	// used to probe a live form's existence.
	f, err := h.Forms.GetTrashed(r.Context(), actx.Account.ID, formID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	if strings.TrimSpace(in.ConfirmSlug) != f.Slug {
		writeErr(w, http.StatusBadRequest,
			"type the form slug ("+f.Slug+") to confirm the permanent deletion")
		return
	}
	if err := h.Forms.Purge(r.Context(), actx.Account.ID, formID); err != nil {
		if errors.Is(err, dbstore.ErrFormHasSales) {
			n, cerr := h.Forms.CountSalesByForm(r.Context(), actx.Account.ID, formID)
			if cerr != nil {
				n = 0
			}
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":       "this form has sales — sales history cannot be destroyed by deleting the form. It stays in the bin.",
				"code":        CodeFormHasSales,
				"sales_count": n,
			})
			return
		}
		respondErr(w, r, err)
		return
	}
	h.recordEvent(r, actx, h.sessionOwnerID(r), "form.purged",
		map[string]any{"form_id": formID, "slug": f.Slug, "title": f.Title})
	// The row is gone, so the origin can no longer serve this form's card at all;
	// the edge may still hold the copy a crawler fetched while it was live (a form
	// is trashed with its status unchanged, so that card can be a public one from
	// before the trash). Asked for unconditionally: a prefix that matches nothing
	// costs nothing, and this is the one request that destroys the form for good.
	edge := h.purgeLeftPublicArtefacts(r.Context(), actx.Account, []string{f.Slug})
	respond(w, http.StatusOK, withEdgeFields(
		map[string]any{"status": "purged", "form_id": formID, "slug": f.Slug}, edge))
}

// bulkForms applies an action to many forms at once:
// POST /v1/rest/forms/bulk {"action":"publish|draft|delete","ids":["uuid",...]}
// Returns {"updated": n} — how many forms the caller's account actually owns
// and that were still eligible (not already soft-deleted).
func (h *RESTHandlers) bulkForms(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	var in struct {
		Action string   `json:"action"`
		IDs    []string `json:"ids"`
		// Acknowledge accepts the "payment form with nothing to sell" warning
		// (see publishForm / dbstore.PublishNeedsAck).
		Acknowledge bool `json:"acknowledge"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.Action != "publish" && in.Action != "draft" && in.Action != "delete" {
		writeErr(w, http.StatusBadRequest, "action must be publish|draft|delete")
		return
	}
	if len(in.IDs) == 0 {
		writeErr(w, http.StatusBadRequest, "ids are required")
		return
	}
	if len(in.IDs) > 500 {
		writeErr(w, http.StatusBadRequest, "too many ids (max 500)")
		return
	}
	// Dedupe + keep only valid UUIDs; malformed ids are ignored, not fatal.
	seen := make(map[string]struct{}, len(in.IDs))
	ids := make([]string, 0, len(in.IDs))
	for _, raw := range in.IDs {
		id := strings.TrimSpace(raw)
		if _, err := uuid.Parse(id); err != nil {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		writeErr(w, http.StatusBadRequest, "ids must be uuid")
		return
	}
	// Card t_fadfe9f7: a bulk publish must not be a way around the workspace
	// precondition the single publish applies. The whole call is refused (and
	// nothing is written) rather than silently skipping the ids: every id in
	// this batch belongs to the caller's account, so the account's missing
	// workspace is the one thing that has to change before any of them can be
	// live. `draft` and `delete` stay allowed — they are how an existing
	// orphan is taken out of a live state.
	//
	// Exception (card t_8b185f34): a batch made ONLY of gallery templates has
	// an address — the gallery — so it is exempt like the single publish. One
	// ordinary form in the batch puts the whole call back under the gate,
	// because that row would go live with no address at all.
	if in.Action == "publish" {
		onlyTemplates, terr := h.Forms.AllGalleryTemplates(r.Context(), actx.Account.ID, ids)
		if terr != nil {
			respondErr(w, r, terr)
			return
		}
		if !onlyTemplates && !h.refuseWithoutLiveWorkspace(w, r, actx) {
			return
		}
	}

	var updated int64
	var skipped []string
	// Onboarding gap: ids published WITHOUT anything to sell, and which the
	// caller acknowledged. Reported alongside `skipped` so the merchant can see
	// which live forms still cannot take money.
	var needsProducts []string
	// The slugs whose publicly cached social card this action invalidates. Read
	// BEFORE the write: the status a form is LEAVING is what decides whether the
	// edge holds a card for it, and after the statement every row carries the new
	// status (see og_edge_purge.go).
	var leaving []string
	if in.Action != "publish" {
		cards, cerr := h.Forms.CardFormsForIDs(r.Context(), actx.Account.ID, ids)
		if cerr != nil {
			respondErr(w, r, cerr)
			return
		}
		for _, c := range cards {
			if dbstore.OGImagePubliclyCached(c.Status) {
				leaving = append(leaving, c.Slug)
			}
		}
	}
	switch in.Action {
	case "publish", "draft":
		status := "published"
		if in.Action == "draft" {
			status = "draft"
		}
		// Issue #594: a bulk publish skips templates that carry no category or
		// no description block (they would reach the gallery as a bare title),
		// while still publishing the ordinary forms in the same batch. Collect
		// the skipped ids so the caller is told, instead of the skip looking
		// like a no-op. The block types that count are the store's rule
		// (galleryMetadataMissingSQL) — a Section intro, or a legacy Text
		// block (t_6a23c70d).
		if status == dbstore.FormStatusPublished {
			var err error
			skipped, err = h.Forms.BlockedGalleryTemplates(r.Context(), actx.Account.ID, ids)
			if err != nil {
				respondErr(w, r, err)
				return
			}
			// Onboarding gap: the same rule as the single publish. A bulk
			// "publish" must not be a way around the "payment form with nothing
			// to sell" guard, so those ids are held back. `in.Acknowledge` (not
			// publishAcknowledged) because decodeJSON above already consumed the
			// body.
			needsProd, perr := h.Forms.PublishNeedsAckIDs(r.Context(), actx.Account.ID, ids)
			if perr != nil {
				respondErr(w, r, perr)
				return
			}
			if len(needsProd) > 0 {
				if !in.Acknowledge {
					writeJSON(w, http.StatusConflict, map[string]any{
						"error": "Some payment forms have no products, so they are not " +
							"published. The form does not collect anything until a product is " +
							"added — add a product first, or confirm to continue.",
						"code":           dbstore.PublishWarningNoProducts,
						"requires_ack":   true,
						"needs_products": needsProd,
					})
					return
				}
				// Acknowledged: these publish too, and are reported separately
				// (a form with nothing to sell is not a template skip, and the
				// two have different fixes).
				needsProducts = needsProd
			}
		}
		tag, err := h.Forms.SetStatusMany(r.Context(), actx.Account.ID, ids, status)
		if err != nil {
			respondErr(w, r, err)
			return
		}
		updated = tag
	case "delete":
		n, err := h.Forms.SoftDeleteMany(r.Context(), actx.Account.ID, ids)
		if err != nil {
			respondErr(w, r, err)
			return
		}
		updated = n
	}
	// Issue #92: audit bulk form action.
	audit := map[string]any{
		"action": in.Action, "ids": ids, "updated": updated, "skipped": len(skipped),
	}
	edge := h.purgeLeftPublicArtefacts(r.Context(), actx.Account, leaving)
	if edge.Status != "" {
		audit["edge"] = edge.Status
	}
	h.recordEvent(r, actx, h.sessionOwnerID(r), "form.bulk_"+in.Action, audit)
	out := map[string]any{"updated": updated}
	if len(skipped) > 0 {
		out["skipped"] = skipped
		out["skipped_reason"] = templateNeedMetadataWire
	}
	if len(needsProducts) > 0 {
		out["needs_products"] = needsProducts
		out["needs_products_reason"] = "a payment form without a product is still published, " +
			"but cannot accept payment and does not collect anything until a product is added"
	}
	writeJSON(w, http.StatusOK, withEdgeFields(out, edge))
}

// ─── API keys ────────────────────────────────────────────────────────────────

type restKey struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
}

func (h *RESTHandlers) listKeys(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	keys, err := h.Keys.ListByAccount(r.Context(), actx.Account.ID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	out := make([]restKey, 0, len(keys))
	for _, k := range keys {
		out = append(out, restKey{ID: k.ID, Name: k.Name, Scopes: k.Scopes})
	}
	respond(w, http.StatusOK, out)
}

func (h *RESTHandlers) createKey(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	var in struct {
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
	}
	if err := decodeJSON(r, &in); err != nil || in.Name == "" {
		writeErr(w, http.StatusBadRequest, "key name is required")
		return
	}
	if len(in.Scopes) == 0 {
		in.Scopes = []string{"readonly"}
	}
	raw, hash, err := auth.GenerateAPIKey()
	if err != nil {
		respondErr(w, r, err)
		return
	}
	k, err := h.Keys.Create(r.Context(), actx.Account.ID, hash, in.Name, in.Scopes)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	// Issue #92: audit API-key creation.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "settings.api_key_created", map[string]any{"key_id": k.ID, "name": in.Name, "scopes": in.Scopes})
	// The raw key is shown exactly once.
	respond(w, http.StatusCreated, map[string]any{
		"id": k.ID, "name": k.Name, "scopes": k.Scopes, "key": raw,
		"note": "Save this key now — it will not be shown again.",
	})
}

func (h *RESTHandlers) revokeKey(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	if err := h.Keys.Revoke(r.Context(), actx.Account.ID, r.PathValue("id")); err != nil {
		respondErr(w, r, err)
		return
	}
	// Issue #92: audit API-key revocation.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "settings.api_key_revoked", map[string]any{"key_id": r.PathValue("id")})
	respond(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// ─── Summary ─────────────────────────────────────────────────────────────────

func (h *RESTHandlers) summary(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	scope, ok := h.scopeOr403(w, r, actx)
	if !ok {
		return
	}
	sum, err := h.summaryFor(r.Context(), scope.Accounts, actx.Account.ID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	respond(w, http.StatusOK, sum)
}

// ─── Sales ───────────────────────────────────────────────────────────────────

// restSaleRows maps sales to the dashboard JSON shape. Shared by
// GET /v1/rest/sales and the dashboard bundle (issue #29). The rich fields
// (items, amounts, buyer contact) are additive and drive the dedicated /sales
// detail drawer (issue #126); the dashboard Recent-sales table reads only the
// subset it renders, so extras are backward-compatible.
type saleRow struct {
	RefCode  string `json:"ref_code"`
	Status   string `json:"status"`
	TotalSen int64  `json:"total_sen"`
	// Currency is the ISO code every money field on this row is denominated in
	// (issue #185): the currency the sale was transacted in, recorded on the
	// sale itself and never re-derived from the workspace's CURRENT setting.
	// Emitted because the client cannot label an amount without it — the Sales
	// page used to print a literal "RM" for every row, so a USD sale read
	// RM24.90. Always set on a sale row (MYR for rows written before the
	// column existed); a response row is a different struct and carries none.
	Currency    string `json:"currency,omitempty"`
	Method      string `json:"payment_method"`
	Buyer       string `json:"buyer"`
	Created     string `json:"created_at"`
	SkipCapture bool   `json:"skip_capture"`
	Authorized  bool   `json:"authorized"`
	CapturedAt  string `json:"captured_at,omitempty"`
	// EmailStatus is the most recent receipt delivery event for this sale
	// (issue #106): "" (no event yet) | delivered | open | click | bounce |
	// complaint. Populated when the delivery-event store is wired.
	EmailStatus string `json:"email_status,omitempty"`

	// Issue #126: detail-drawer fields (additive for the /sales page).
	FormID      string `json:"form_id,omitempty"`
	SaleType    string `json:"sale_type,omitempty"`
	InvoiceNo   string `json:"invoice_no,omitempty"`
	BuyerEmail  string `json:"buyer_email,omitempty"`
	BuyerPhone  string `json:"buyer_phone,omitempty"`
	SubtotalSen int64  `json:"subtotal_sen,omitempty"`
	ShippingSen int64  `json:"shipping_sen,omitempty"`
	// Shipping phase 2: WHICH source priced the delivery fee, so the dashboard
	// can state the provenance of a disputed charge on the sale itself instead of
	// leaving it in the database record alone. ShippingSource is the source that
	// WON ("courier" | "zones") and ShippingSourceDetail names it (the courier
	// provider, or the zone the address matched); ShippingFallbackReason is the
	// CLASS of failure that left a courier-named block on the zone rate
	// ("unavailable" | "error" | "timeout") and is empty on a sale no courier
	// failed to price.
	ShippingSource         string `json:"shipping_source,omitempty"`
	ShippingSourceDetail   string `json:"shipping_source_detail,omitempty"`
	ShippingFallbackReason string `json:"shipping_fallback_reason,omitempty"`
	DiscountSen            int64  `json:"discount_sen,omitempty"`
	// Card t_135c3c72: the processing fee the BUYER bore, and the rate that was
	// in force when they paid. Frozen on the sale, so a later rate change cannot
	// rewrite what a past buyer was charged. FeeSen is part of TotalSen.
	FeeSen       int64     `json:"fee_sen,omitempty"`
	FeeMethod    string    `json:"fee_method,omitempty"`
	FeeFixedSen  int64     `json:"fee_fixed_sen,omitempty"`
	FeePercentBp int64     `json:"fee_percent_bp,omitempty"`
	Items        []itemRow `json:"items,omitempty"`
	Updated      string    `json:"updated_at,omitempty"`
	// Issue #438: QR ordering flow — the table number the QR stand encoded
	// (e.g. "8"). Empty = a regular online order with no table.
	TableNumber string `json:"table_number,omitempty"`

	// Issue #518: partial-refund accounting. RefundedSen is the running total
	// already returned, OutstandingSen the balance still refundable, and
	// RefundStatus the derived "none"|"partial"|"full" label. Exposing both
	// numbers means every consumer (Sales page, CSV, agents) can tell a partial
	// refund from a full one instead of reading the status alone.
	RefundedSen    int64  `json:"refunded_sen"`
	OutstandingSen int64  `json:"outstanding_sen"`
	RefundStatus   string `json:"refund_status"`

	// AvailableEvents is the server's answer to "what may I do with this sale":
	// every event the lifecycle table accepts from its current state, in a
	// stable order, each with the dashboard's label key and — when a guard
	// refuses it — enabled=false plus the English reason. The dashboard builds
	// its action buttons from THIS list instead of re-deriving the rule from
	// `status`, which is how the "the UI offers an action the API refuses"
	// class of bug stops being possible. Emitted for every sale row, as [] when
	// the state accepts nothing, so an absent field can never be mistaken for
	// "no restrictions".
	AvailableEvents []lifecycle.AvailableEvent `json:"available_events"`
}

type itemRow struct {
	ProductID string `json:"product_id"`
	PriceSen  int64  `json:"price_sen"`
	Qty       int64  `json:"qty"`
	Name      string `json:"name"`
	// Issue #153: which variation was bought (empty for simple products).
	VariationID   string `json:"variation_id,omitempty"`
	VariationName string `json:"variation_name,omitempty"`
}

// latestEmailStatuses returns, for each sale ref, the most recent delivery
// event type (issue #106). Best-effort: a nil store or a query error yields an
// empty map (the sales list simply shows no email status).
func (h *RESTHandlers) latestEmailStatuses(ctx context.Context, sales []*dbstore.Sale) map[string]string {
	out := map[string]string{}
	if h.EmailDeliveryEvents == nil || len(sales) == 0 {
		return out
	}
	refs := make([]string, 0, len(sales))
	for _, s := range sales {
		refs = append(refs, s.RefCode)
	}
	latest, err := h.EmailDeliveryEvents.LatestBySaleRefs(ctx, refs)
	if err != nil {
		return out
	}
	for ref, e := range latest {
		out[ref] = e.EventType
	}
	return out
}

// saleAvailableEvents is the ONE place a sale row's available_events are
// derived, so the list, the detail read and the dashboard bundle cannot answer
// differently. A nil sale (or an unknown state) yields an empty list, never
// nil: the JSON field must always be an array, because the UI treats a MISSING
// field as "the server did not say" and renders no actions at all.
func saleAvailableEvents(s *dbstore.Sale) []lifecycle.AvailableEvent {
	if s == nil {
		return []lifecycle.AvailableEvent{}
	}
	out := lifecycle.SalesMachine.Available(s)
	if out == nil {
		return []lifecycle.AvailableEvent{}
	}
	return out
}

func restSaleRows(sales []*dbstore.Sale) []saleRow {
	out := make([]saleRow, 0, len(sales))
	for _, s := range sales {
		out = append(out, toSaleRow(s, ""))
	}
	return out
}

// toSaleRow converts a Sale into its JSON row. emailStatus is the receipt
// delivery event type for this sale (issue #106), or "".
func toSaleRow(s *dbstore.Sale, emailStatus string) saleRow {
	row := saleRow{
		RefCode: s.RefCode, Status: s.Status, TotalSen: s.TotalSen,
		// The sale's OWN currency, so every amount on the row is labelled
		// correctly for a non-MYR workspace (and old MYR rows stay RM after a
		// workspace switches).
		Currency: s.CurrencyOrMYR(),
		Method:   s.PaymentMethod, Buyer: s.Client.FullName,
		Created:     s.CreatedAt.UTC().Format(time.RFC3339),
		SkipCapture: s.SkipCapture, Authorized: s.Authorized,
		EmailStatus:    emailStatus,
		FormID:         s.FormID,
		SaleType:       s.SaleType,
		InvoiceNo:      s.InvoiceNo,
		BuyerEmail:     s.Client.Email,
		BuyerPhone:     s.Client.Phone,
		SubtotalSen:    s.SubtotalSen,
		ShippingSen:    s.ShippingSen,
		DiscountSen:    s.DiscountSen,
		FeeSen:         s.FeeSen,
		FeeMethod:      s.FeeMethod,
		FeeFixedSen:    s.FeeFixedSen,
		FeePercentBp:   s.FeePercentBp,
		Updated:        s.UpdatedAt.UTC().Format(time.RFC3339),
		TableNumber:    s.TableNumber,
		RefundedSen:    s.RefundedSen,
		OutstandingSen: s.OutstandingSen(),
		RefundStatus:   s.RefundStatus(),
		// The server's own answer to "what may this sale do next". Derived here,
		// at the one place a sale becomes a row, so the list, the detail read and
		// the dashboard bundle cannot disagree.
		AvailableEvents: saleAvailableEvents(s),
	}
	// Shipping phase 2: a sale that carries a delivery fee also carries where the
	// fee CAME FROM. Read from the stored record, never re-derived: the record is
	// what the submit path wrote when it priced the order.
	if s.ShippingSen != 0 {
		row.ShippingSource, row.ShippingSourceDetail, row.ShippingFallbackReason = shippingSourceSummary(s.ShippingDetail)
	}
	if s.CapturedAt != nil {
		row.CapturedAt = s.CapturedAt.UTC().Format(time.RFC3339)
	}
	if len(s.Items) > 0 {
		items := make([]itemRow, 0, len(s.Items))
		for _, it := range s.Items {
			items = append(items, itemRow{
				ProductID:     it.ProductID,
				PriceSen:      it.PriceSen,
				Qty:           it.Qty,
				Name:          it.Name,
				VariationID:   it.VariationID,
				VariationName: it.VariationName,
			})
		}
		row.Items = items
	}
	return row
}

// ─── Issue #171: response-mode rows on the sales surface ─────────────────────

// restSubmissionRow is one response-mode entry rendered in the sales list
// envelope. A response form has no sale, so the money/status columns are
// meaningless; the row carries the recorded field values instead and is marked
// sale_type "response" so every consumer can tell the two apart.
type restSubmissionRow struct {
	// RefCode is empty for a response (there is no payment reference); the id
	// is surfaced as submission_id.
	RefCode    string `json:"ref_code"`
	Status     string `json:"status"`
	SaleType   string `json:"sale_type"`
	Created    string `json:"created_at"`
	FormID     string `json:"form_id,omitempty"`
	Buyer      string `json:"buyer,omitempty"`
	BuyerEmail string `json:"buyer_email,omitempty"`
	BuyerPhone string `json:"buyer_phone,omitempty"`
	// Issue #171/#604: the submission identity + recorded answers.
	SubmissionID string         `json:"submission_id"`
	Data         map[string]any `json:"data"`
	// Money is integer sen everywhere; a response has none, so these stay 0 so
	// a table that renders them unconditionally shows zero rather than a NaN.
	TotalSen       int64  `json:"total_sen"`
	RefundedSen    int64  `json:"refunded_sen"`
	OutstandingSen int64  `json:"outstanding_sen"`
	RefundStatus   string `json:"refund_status"`
	Updated        string `json:"updated_at,omitempty"`
}

// submissionsAsRows maps response entries into the sales-list row shape.
func submissionsAsRows(subs []*dbstore.Submission) []restSubmissionRow {
	out := make([]restSubmissionRow, 0, len(subs))
	for _, s := range subs {
		row := restSubmissionRow{
			Status:       "response",
			SaleType:     "response",
			Created:      s.CreatedAt.UTC().Format(time.RFC3339),
			FormID:       s.FormID,
			SubmissionID: s.ID,
			Data:         s.Data,
			Updated:      s.CreatedAt.UTC().Format(time.RFC3339),
		}
		// Buyer identity is recorded alongside the answers by the submit
		// handler (public.go handleResponseSubmit), so surface it in the same
		// columns a sale uses.
		if s.Data != nil {
			row.Buyer = strField(s.Data, "full_name")
			row.BuyerEmail = strField(s.Data, "email")
			row.BuyerPhone = strField(s.Data, "phone")
		}
		out = append(out, row)
	}
	return out
}

// strField reads a string value out of a submission's recorded answers.
func strField(data map[string]any, key string) string {
	if v, ok := data[key].(string); ok {
		return v
	}
	return ""
}

// listResponseRows writes one page of a response-mode form's submissions in the
// same envelope the sales list uses, so /sales?form=<response form> shows the
// form's entries instead of an always-empty sales page.
//
// A submission has no status column and no payment: a status filter therefore
// matches nothing (the merchant asked for a state responses cannot be in), while
// the date range and the free-text search apply to the recorded values.
//
// The caller passes a formID only for the single-response-form case, which the
// scope decision (dbstore.ResolveRecordScope.OnlyResponses) has already
// established — this function does not re-decide which table to read.
func (h *RESTHandlers) listResponseRows(w http.ResponseWriter, r *http.Request, scope *scopeResult, formID string, page, limit int) {
	if r.URL.Query().Get("status") != "" {
		respond(w, http.StatusOK, map[string]any{
			"items": []restSubmissionRow{}, "page": page, "limit": limit, "total": 0,
		})
		return
	}
	subs, total, err := h.Submissions.ListFilteredPage(r.Context(), scope.Accounts, dbstore.SubmissionFilter{
		FormID:   formID,
		DateFrom: r.URL.Query().Get("date_from"),
		DateTo:   r.URL.Query().Get("date_to"),
		Q:        r.URL.Query().Get("q"),
	}, page, limit)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{
		"items": submissionsAsRows(subs), "page": page, "limit": limit, "total": total,
	})
}

// legacyPageLimit reads the legacy array shape's ?limit= (which has always
// honoured a caller-supplied limit for the response branch) with the shape's
// historical default cap of 100 when none is given.
func legacyPageLimit(r *http.Request) (page, limit int) {
	page, limit = 1, legacyArrayLimit
	if v, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && v > 0 {
		page = v
	}
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
		limit = v
	}
	if limit > maxRecordPageLimit {
		limit = maxRecordPageLimit
	}
	return page, limit
}

// legacyArrayLimit is the row cap of the paging-less array shape. It is kept at
// the value that shape has always used so an existing API-key consumer does not
// silently receive a shorter list.
const legacyArrayLimit = 100

func (h *RESTHandlers) listSales(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	scope, ok := h.scopeOr403(w, r, actx)
	if !ok {
		return
	}
	formID := r.URL.Query().Get("form_id")
	// ONE decision, over the whole account scope: which tables does this read
	// cover? (dbstore.ResolveRecordScope.) An unfiltered read spans both tables
	// whenever the scope owns a response-mode form — that is the fix for the
	// reported defect, and it is resolved here for every shape below.
	sc, err := h.recordScope(r.Context(), scope, formID)
	if err != nil {
		respondErr(w, r, err)
		return
	}

	// Legacy shape: plain array when the caller does not ask for paging.
	if r.URL.Query().Get("page") == "" {
		page, limit := legacyPageLimit(r)
		switch {
		case sc.OnlyResponses:
			// Issue #171: a response-mode form has no sales at all, so the
			// array shape reads its submissions — otherwise every caller of
			// this form (the dashboard's recent-list included) sees an empty
			// array as if nothing had ever been recorded.
			subs, _, err := h.Submissions.ListFilteredPage(r.Context(), scope.Accounts,
				recordFilter(r).SubmissionFilter(sc.ResponseFormIDs), page, limit)
			if err != nil {
				respondErr(w, r, err)
				return
			}
			respond(w, http.StatusOK, submissionsAsRows(subs))
			return
		case sc.IncludeSubs:
			// Unfiltered (or a filter that keeps both halves): the array shape
			// must carry the same record set as the paginated list, in the same
			// order — the dashboard reads this shape, and a different answer
			// per shape is how the counts drifted.
			records, _, err := h.Sales.MergedPage(r.Context(), scope.Accounts, sc, recordFilter(r), page, limit)
			if err != nil {
				respondErr(w, r, err)
				return
			}
			respond(w, http.StatusOK, h.recordRows(r.Context(), records))
			return
		}
		// Sales-only scope (no response form in play). The array shape applies
		// the same filters as the paginated one: it used to ignore
		// status/date/q entirely, so two callers of the same URL got different
		// semantics for the same query string.
		rows, _, err := h.Sales.ListFilteredPage(r.Context(), scope.Accounts, recordFilter(r).SaleFilter(), page, limit)
		if err != nil {
			respondErr(w, r, err)
			return
		}
		// Issue #106: surface the most recent receipt delivery event per sale.
		emailStatus := h.latestEmailStatuses(r.Context(), rows)
		out := make([]saleRow, 0, len(rows))
		for _, s := range rows {
			// Issue #518: build the full row (not a subset) so the legacy
			// array shape carries refunded_sen / outstanding_sen /
			// refund_status too — a partial refund must be visible on EVERY
			// read surface, and the legacy shape is the dashboard's.
			row := toSaleRow(s, emailStatus[s.RefCode])
			if s.CapturedAt != nil {
				row.CapturedAt = s.CapturedAt.UTC().Format(time.RFC3339)
			}
			out = append(out, row)
		}
		respond(w, http.StatusOK, out)
		return
	}

	// Paginated shape: { items, page, limit, total }.
	page, limit := parsePageLimit(r)
	switch {
	case sc.OnlyResponses:
		// Issue #171: the Sales page's per-form deep link must show a
		// response-mode form's entries. Its entries live in `submissions`, not
		// `sales`, so the sales-only query can never match them (the reported
		// defect). The form's own mode picks the table.
		h.listResponseRows(w, r, scope, formID, page, limit)
		return
	case sc.IncludeSubs:
		// The unfiltered list (and any filter that keeps both halves): sales
		// AND the scope's response entries, merged newest-first with one
		// combined total.
		if err := h.listRecordRows(w, r, scope, sc, page, limit); err != nil {
			respondErr(w, r, err)
		}
		return
	}

	sales, total, err := h.Sales.ListFilteredPage(r.Context(), scope.Accounts, recordFilter(r).SaleFilter(), page, limit)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	out := restSaleRows(sales)
	// Issue #106: surface the most recent receipt delivery event per sale.
	emailStatus := h.latestEmailStatuses(r.Context(), sales)
	for i := range out {
		out[i].EmailStatus = emailStatus[out[i].RefCode]
	}
	respond(w, http.StatusOK, map[string]any{
		"items": out, "page": page, "limit": limit, "total": total,
	})
}

// getSale returns ONE sale, including available_events (the lifecycle table's
// answer for this record). Additive read, same ownership rule as the write
// handlers: a sale outside the caller's account is a 404, never another
// account's row.
//
// It exists because the dashboard and an agent both need to re-read a single
// record after an action (the list is paged and filtered), and because
// "what may I do with this sale" should not require fetching a page of them.
func (h *RESTHandlers) getSale(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	ref := r.PathValue("ref")
	sale, err := h.Sales.ByRefCode(r.Context(), ref)
	if err != nil || sale.AccountID != actx.Account.ID {
		writeErr(w, http.StatusNotFound, "sale not found")
		return
	}
	row := toSaleRow(sale, "")
	if statuses := h.latestEmailStatuses(r.Context(), []*dbstore.Sale{sale}); statuses != nil {
		row.EmailStatus = statuses[sale.RefCode]
	}
	respond(w, http.StatusOK, row)
}

// saleEmailStatus returns the per-sale receipt email delivery timeline
// (issue #106): Sent → Delivered → Opened → (Clicked), with timestamps,
// sourced from email_delivery_events correlated to this sale's ref. The
// "sent" step is implied by the sale having a receipt_message_id (the SES
// MessageID captured at send time); the rest come from SES events.
func (h *RESTHandlers) saleEmailStatus(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	ref := r.PathValue("ref")
	sale, err := h.Sales.ByRefCode(r.Context(), ref)
	if err != nil || sale.AccountID != actx.Account.ID {
		writeErr(w, http.StatusNotFound, "sale not found")
		return
	}
	type eventOut struct {
		EventType string `json:"event_type"`
		At        string `json:"at"`
	}
	resp := map[string]any{
		"ref_code": sale.RefCode,
		"sent":     sale.ReceiptMessageID != "",
		"events":   []eventOut{},
	}
	if h.EmailDeliveryEvents != nil {
		events, err := h.EmailDeliveryEvents.ListBySaleRef(r.Context(), sale.RefCode)
		if err != nil {
			respondErr(w, r, err)
			return
		}
		out := make([]eventOut, 0, len(events))
		for _, e := range events {
			out = append(out, eventOut{
				EventType: e.EventType,
				At:        e.CreatedAt.UTC().Format(time.RFC3339),
			})
		}
		resp["events"] = out
	}
	respond(w, http.StatusOK, resp)
}

func (h *RESTHandlers) captureSale(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	if h.Pay == nil {
		writeErr(w, http.StatusServiceUnavailable, "payment is not active")
		return
	}
	ref := r.PathValue("ref")
	sale, err := h.Sales.ByRefCode(r.Context(), ref)
	if err != nil || sale.AccountID != actx.Account.ID {
		writeErr(w, http.StatusNotFound, "sale not found")
		return
	}
	var in struct {
		AmountSen *int64 `json:"amount_sen,omitempty"`
	}
	_ = decodeJSON(r, &in)
	// The table decides whether this sale accepts a capture at all (and its
	// guard requires an uncaptured authorization), so an illegal move never
	// reaches the gateway.
	if _, lerr := lifecycle.SalesMachine.Apply(sale, lifecycle.SaleEventCapture); lerr != nil {
		recordPaymentFailure("capture")
		writePaymentErr(w, r, lerr)
		return
	}
	if err := h.Pay.CaptureSale(r.Context(), sale, in.AmountSen); err != nil {
		recordPaymentFailure("capture")
		writePaymentErr(w, r, err)
		return
	}
	if err := h.Sales.MarkCaptured(r.Context(), sale.ID); err != nil {
		respondErr(w, r, err)
		return
	}
	// Issue #92: audit sale capture.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "sale.captured", map[string]any{"ref_code": ref, "sale_id": sale.ID})
	respond(w, http.StatusOK, map[string]string{"status": "captured", "ref_code": ref})
}

func (h *RESTHandlers) releaseSale(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	if h.Pay == nil {
		writeErr(w, http.StatusServiceUnavailable, "payment is not active")
		return
	}
	ref := r.PathValue("ref")
	sale, err := h.Sales.ByRefCode(r.Context(), ref)
	if err != nil || sale.AccountID != actx.Account.ID {
		writeErr(w, http.StatusNotFound, "sale not found")
		return
	}
	// A release only means something on an uncaptured authorization; the table
	// says so, so the check is the guard rather than an `if` in this handler.
	res, lerr := lifecycle.SalesMachine.Apply(sale, lifecycle.SaleEventRelease)
	if lerr != nil {
		recordPaymentFailure("release")
		writePaymentErr(w, r, lerr)
		return
	}
	if err := h.Pay.ReleaseSale(r.Context(), sale); err != nil {
		recordPaymentFailure("release")
		writePaymentErr(w, r, err)
		return
	}
	if err := h.Sales.SetStatus(r.Context(), sale.ID, res.To); err != nil {
		respondErr(w, r, err)
		return
	}
	// Issue #92: audit sale release.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "sale.released", map[string]any{"ref_code": ref, "sale_id": sale.ID})
	respond(w, http.StatusOK, map[string]string{"status": "released", "ref_code": ref})
}

// refundSale issues a full or partial refund against a paid sale (issue #407):
// POST /v1/rest/sales/{ref}/refund {"amount_sen": N}. amount_sen omitted = a
// full refund of the OUTSTANDING balance. Validates the amount against the
// sale's remaining balance, calls the CHIP refund API (amount in sen), records
// the refunded total on the sale and audits who/what/when (issue #518).
//
// Issue #518: a PARTIAL refund no longer flips the sale to the terminal
// `refunded` status. The refunded amount + balance guard and the increment are
// atomic inside payments.RefundSale (per-sale lock), so a partial leaves the
// sale reading as partial and only a refund that clears the whole total marks
// it fully refunded.
//
// Card t_135c3c72 (buyer-borne processing fee): a refund NEVER recalculates the
// fee. The refundable balance is the sale's total, which already includes
// fee_sen — a full refund therefore returns what the buyer actually paid (fee
// included), while the merchant still bears the acquirer's own charge (CHIP does
// not return it) and the sale keeps showing that fee at the rate frozen when the
// buyer paid. That is the documented policy, not an accident: recomputing a fee
// here would charge the buyer twice for a refund, and adding it back to the
// merchant's side would be a number no payment provider ever moved.
func (h *RESTHandlers) refundSale(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	if h.Pay == nil {
		writeErr(w, http.StatusServiceUnavailable, "payment is not active")
		return
	}
	ref := r.PathValue("ref")
	sale, err := h.Sales.ByRefCode(r.Context(), ref)
	if err != nil || sale.AccountID != actx.Account.ID {
		writeErr(w, http.StatusNotFound, "sale not found")
		return
	}
	var in struct {
		AmountSen *int64 `json:"amount_sen,omitempty"`
	}
	_ = decodeJSON(r, &in)
	res, err := h.Pay.RefundSale(r.Context(), sale, in.AmountSen)
	if err != nil {
		recordPaymentFailure("refund")
		writePaymentErr(w, r, err)
		return
	}
	// Issue #407: audit refunds (who, amount, when). Issue #518: extend the
	// props with the running refunded total and the partial/full outcome so the
	// audit trail can reconstruct the outstanding balance.
	props := map[string]any{
		"ref_code":        ref,
		"sale_id":         sale.ID,
		"amount_sen":      res.AmountSen,
		"refunded_sen":    res.RefundedSen,
		"total_sen":       sale.TotalSen,
		"outstanding_sen": res.OutstandingSen,
		"fully_refunded":  res.FullyRefunded,
	}
	h.recordEvent(r, actx, h.sessionOwnerID(r), "sale.refunded", props)
	respond(w, http.StatusOK, map[string]any{
		"status": res.Status, "ref_code": ref,
		"amount_sen": res.AmountSen, "refunded_sen": res.RefundedSen,
		"outstanding_sen": res.OutstandingSen, "fully_refunded": res.FullyRefunded,
		"refund_status": refundStatusFor(sale.TotalSen, res.RefundedSen),
	})
}

// refundStatusFor derives the partial/full refund label for a REST response
// from the sale total + the recorded refunded amount (issue #518). It mirrors
// dbstore.Sale.RefundStatus() for call sites that only have the two numbers.
func refundStatusFor(totalSen, refundedSen int64) string {
	switch {
	case refundedSen <= 0:
		return dbstore.RefundNone
	case refundedSen >= totalSen:
		return dbstore.RefundFull
	default:
		return dbstore.RefundPartial
	}
}

// bulkCaptureSales captures awaiting skip_capture authorizations in batch
// (issue #38, the #12 addendum): POST /v1/rest/sales/bulk-capture
// {"refs":["JF-…", …]} or {} / {"refs":"all"} for everything awaiting.
// Per-ref results — one CHIP rejection never aborts the batch.
func (h *RESTHandlers) bulkCaptureSales(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	if h.Pay == nil {
		writeErr(w, http.StatusServiceUnavailable, "payment is not active")
		return
	}
	var in struct {
		Refs json.RawMessage `json:"refs,omitempty"`
	}
	_ = decodeJSON(r, &in)

	var sales []*dbstore.Sale
	if len(in.Refs) > 0 && string(in.Refs) != `"all"` {
		var refs []string
		if err := json.Unmarshal(in.Refs, &refs); err != nil {
			writeErr(w, http.StatusBadRequest, "refs must be an array or \"all\"")
			return
		}
		if len(refs) > 200 {
			writeErr(w, http.StatusBadRequest, "too many refs (max 200)")
			return
		}
		for _, ref := range refs {
			sale, err := h.Sales.ByRefCode(r.Context(), strings.TrimSpace(ref))
			if err != nil || sale.AccountID != actx.Account.ID {
				continue // unknown/foreign ref = per-ref failure, not fatal
			}
			sales = append(sales, sale)
		}
	} else {
		var err error
		sales, err = h.Sales.ListAwaitingCapture(r.Context(), actx.Account.ID)
		if err != nil {
			respondErr(w, r, err)
			return
		}
	}

	type result struct {
		RefCode string `json:"ref_code"`
		OK      bool   `json:"ok"`
		Error   string `json:"error,omitempty"`
	}
	results := make([]result, 0, len(sales))
	ok, failed := 0, 0
	for _, sale := range sales {
		res := result{RefCode: sale.RefCode}
		if _, lerr := lifecycle.SalesMachine.Apply(sale, lifecycle.SaleEventCapture); lerr != nil {
			// The per-row sentence is what a merchant reads in the result
			// panel, so a guard refusal keeps its own wording instead of
			// gaining the machine-code suffix.
			if refused, ok := lerr.(*lifecycle.TransitionRefusedError); ok {
				res.Error = refused.Reason
			} else {
				res.Error = lerr.Error()
			}
		} else if err := h.Pay.CaptureSale(r.Context(), sale, nil); err != nil {
			recordPaymentFailure("bulk_capture")
			res.Error = paymentErrText(err)
		} else if err := h.Sales.MarkCaptured(r.Context(), sale.ID); err != nil {
			res.Error = err.Error()
		} else {
			res.OK = true
			ok++
		}
		if !res.OK {
			failed++
		}
		results = append(results, res)
	}
	// Issue #92: audit bulk capture.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "sale.bulk_captured", map[string]any{
		"requested": len(sales), "captured": ok, "failed": failed,
	})
	respond(w, http.StatusOK, map[string]any{
		"requested": len(sales), "captured": ok, "failed": failed, "results": results,
	})
}

func (h *RESTHandlers) confirmSale(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	ref := r.PathValue("ref")
	sale, err := h.Sales.ByRefCode(r.Context(), ref)
	if err != nil || sale.AccountID != actx.Account.ID {
		writeErr(w, http.StatusNotFound, "sale not found")
		return
	}
	// The table decides which states accept a confirmation (pending for a bank
	// transfer, paid once the CHIP payment settled) and treats re-confirming a
	// confirmed sale as a no-op, so the endpoint keeps its 200 without a second
	// write.
	res, lerr := lifecycle.SalesMachine.Apply(sale, lifecycle.SaleEventConfirm)
	if lerr != nil {
		writeErr(w, http.StatusBadRequest, lerr.Error())
		return
	}
	if res.Changed {
		if err := h.Sales.SetStatus(r.Context(), sale.ID, res.To); err != nil {
			respondErr(w, r, err)
			return
		}
	}
	// Issue #92: audit sale confirm.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "sale.confirmed", map[string]any{"ref_code": ref, "sale_id": sale.ID})
	respond(w, http.StatusOK, map[string]string{"status": "confirmed", "ref_code": ref})
}

func orEmptyMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}
