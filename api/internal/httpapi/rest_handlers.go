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
	"github.com/wzul/jomform/api/internal/payments"
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
	ScheduleMode string          `json:"schedule_mode"`
	Active       bool            `json:"active"`
	Stock        *int            `json:"stock,omitempty"` // nil = unlimited
	Variations   []restVariation `json:"variations,omitempty"`
	CreatedAt    string          `json:"created_at,omitempty"`
	UpdatedAt    string          `json:"updated_at,omitempty"`
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
			return "variation name wajib"
		}
		if v.PriceSen < 0 {
			return "variation price_sen mesti >= 0"
		}
		if v.Stock != nil && *v.Stock < 0 {
			return "variation stok mesti >= 0"
		}
		if !urlguard.ValidList(v.Images) {
			return urlguard.InvalidMessage("variations[].images")
		}
		if v.BillingPeriod != nil && *v.BillingPeriod != "" &&
			*v.BillingPeriod != "month" && *v.BillingPeriod != "year" {
			return "variations[].billing_period mesti month|year"
		}
		if v.TrialDays != nil && *v.TrialDays < 0 {
			return "variations[].trial_days mesti >= 0"
		}
		if v.SubscriptionCharges != nil && *v.SubscriptionCharges < 1 {
			return "variations[].subscription_charges mesti >= 1"
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
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.Name != nil && *in.Name == "" {
		writeErr(w, http.StatusBadRequest, "nama tidak boleh kosong")
		return
	}
	if in.PriceSen != nil && *in.PriceSen < 0 {
		writeErr(w, http.StatusBadRequest, "price_sen mesti >= 0")
		return
	}
	if in.TrialDays != nil && *in.TrialDays < 0 {
		writeErr(w, http.StatusBadRequest, "trial_days mesti >= 0")
		return
	}
	var typPtr, periodPtr *string
	if in.Type != nil {
		if *in.Type != dbstore.SaleTypeOneTime && *in.Type != dbstore.SaleTypeSubscription {
			writeErr(w, http.StatusBadRequest, "type mesti one_time|subscription")
			return
		}
		typPtr = in.Type
	}
	if in.BillingPeriod != nil && (typPtr == nil || *typPtr == dbstore.SaleTypeSubscription) {
		if *in.BillingPeriod != "month" && *in.BillingPeriod != "year" {
			writeErr(w, http.StatusBadRequest, "billing_period mesti month|year")
			return
		}
		periodPtr = in.BillingPeriod
	}
	if in.Stock != nil && *in.Stock < 0 {
		writeErr(w, http.StatusBadRequest, "stok mesti >= 0")
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
		writeErr(w, http.StatusBadRequest, "subscription_charges mesti >= 1 (atau null untuk infinite)")
		return
	}
	// round 13c: reject an unknown schedule mode rather than silently keeping
	// the old one (an ignored setting is how a merchant ends up on the wrong
	// billing schedule without knowing).
	if in.ScheduleMode != nil && *in.ScheduleMode != "" && !payments.ValidScheduleMode(*in.ScheduleMode) {
		writeErr(w, http.StatusBadRequest,
			"schedule_mode tidak sah (from_payment|catch_up|skip_missed)")
		return
	}
	err := h.Products.Update(r.Context(), actx.Account.ID, r.PathValue("id"),
		in.Name, in.Description, in.PriceSen, in.Images, in.Stock, in.Active,
		typPtr, periodPtr, in.TrialDays, in.SubscriptionCharges, in.ScheduleMode)
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
		writeErr(w, http.StatusBadRequest, "payload tidak sah")
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
	if strings.TrimSpace(in.ConfirmName) != p.Name {
		writeErr(w, http.StatusBadRequest,
			"taip nama produk ("+p.Name+") untuk sahkan pemadaman kekal")
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
				"error":       "produk ini mempunyai jualan — sejarah jualan tidak boleh dimusnahkan dengan memadam produk. Ia kekal dalam tong sampah.",
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
				"error":              "produk ini masih ada langganan — batalkan langganan itu dahulu. Ia kekal dalam tong sampah.",
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
		writeErr(w, http.StatusBadRequest, "action mesti archive")
		return
	}
	if len(in.IDs) == 0 {
		writeErr(w, http.StatusBadRequest, "ids wajib")
		return
	}
	if len(in.IDs) > 500 {
		writeErr(w, http.StatusBadRequest, "ids terlalu banyak (maks 500)")
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
		writeErr(w, http.StatusBadRequest, "ids mesti uuid")
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
	}
	// Issue #551: price_sen is a POINTER so a missing or explicit `null` is
	// distinguishable from a legitimate 0. Previously a plain int64 meant
	// `{"price_sen":null}` decoded to 0 with NO error, so a client sending a
	// non-finite price (JSON.stringify(Infinity) === null) silently created a
	// RM 0.00 product which could then be sold and charged at zero.
	if err := decodeJSON(r, &in); err != nil || in.Name == "" || in.PriceSen == nil || *in.PriceSen < 0 {
		writeErr(w, http.StatusBadRequest, "nama dan price_sen wajib")
		return
	}
	if in.TrialDays < 0 {
		writeErr(w, http.StatusBadRequest, "trial_days mesti >= 0")
		return
	}
	if in.SubscriptionCharges != nil && *in.SubscriptionCharges < 1 {
		writeErr(w, http.StatusBadRequest, "subscription_charges mesti >= 1 (atau null untuk infinite)")
		return
	}
	// Issue #501: the product's images (and its variations' images) render
	// into buyer-facing `<img src>` attributes, so an unsafe scheme must be
	// rejected at write time — see internal/urlguard for the shared rule.
	if !urlguard.ValidList(in.Images) {
		writeErr(w, http.StatusBadRequest, urlguard.InvalidMessage("images"))
		return
	}
	pType := in.Type
	if pType == "" {
		pType = dbstore.SaleTypeOneTime
	}
	if pType != dbstore.SaleTypeOneTime && pType != dbstore.SaleTypeSubscription {
		writeErr(w, http.StatusBadRequest, "type mesti one_time|subscription")
		return
	}
	period := ""
	if pType == dbstore.SaleTypeSubscription {
		period = in.BillingPeriod
		if period != "month" && period != "year" {
			writeErr(w, http.StatusBadRequest, "subscription perlu billing_period month|year")
			return
		}
	}
	// Validate variations (money stays integer sen; stock nil = unlimited).
	// Issue #623 also validates each variation's own subscription cycle.
	if msg := validateRestVariations(in.Variations); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	schedMode := payments.ScheduleFromPayment
	if in.ScheduleMode != nil && *in.ScheduleMode != "" {
		if !payments.ValidScheduleMode(*in.ScheduleMode) {
			writeErr(w, http.StatusBadRequest,
				"schedule_mode tidak sah (from_payment|catch_up|skip_missed)")
			return
		}
		schedMode = *in.ScheduleMode
	}
	p := &dbstore.Product{
		AccountID: actx.Account.ID, Name: in.Name, Description: in.Description,
		PriceSen: *in.PriceSen, Images: in.Images, Type: pType,
		BillingPeriod: period, TrialDays: in.TrialDays, Active: true,
		Stock: in.Stock, SubscriptionCharges: in.SubscriptionCharges,
		ScheduleMode: schedMode,
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
func (h *RESTHandlers) formURLForAccount(ctx context.Context, acc *dbstore.Account, slug string) string {
	if acc == nil {
		return ""
	}
	if acc.CustomDomain != "" && acc.CustomDomainVerified {
		return "https://" + strings.TrimRight(acc.CustomDomain, "/") + "/" + slug
	}
	return formURL(h.publicHostSlugFor(ctx, acc.ID), slug)
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

// uniqueFormSlug generates a URL-safe slug for a new form (issue #176). It
// prefers a slugified form title, falling back to a random token when the
// title yields nothing usable, and appends a random suffix until the slug is
// free within the account's workspace (uniqueness is per-workspace, #77).
func (h *RESTHandlers) uniqueFormSlug(ctx context.Context, accountID, title string) string {
	base := strings.ToLower(strings.TrimSpace(title))
	base = slugUnsafe.ReplaceAllString(base, "-")
	base = strings.Trim(base, "-")
	if base == "" {
		base = "f-" + randHex(4)
	}
	if len(base) > 40 {
		base = base[:40]
	}
	base = strings.Trim(base, "-")
	if base == "" {
		base = "f-" + randHex(4)
	}
	// Ensure uniqueness within the workspace; append a random suffix on collision.
	for i := 0; i < 5; i++ {
		taken, err := h.Forms.SlugExists(ctx, accountID, base)
		if err != nil || !taken {
			return base
		}
		base = base + "-" + randHex(2)
	}
	return base + "-" + randHex(4)
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

func (h *RESTHandlers) createForm(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	var in struct {
		Slug   string         `json:"slug"`
		Title  string         `json:"title"`
		Config map[string]any `json:"config"`
		Theme  map[string]any `json:"theme"`
	}
	if err := decodeJSON(r, &in); err != nil || in.Title == "" {
		writeErr(w, http.StatusBadRequest, "title wajib")
		return
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
				writeErr(w, http.StatusBadRequest, "custom_js ditolak: "+reason)
				return
			}
		}
		// Issue #471: product image size + layout are validated against the
		// renderer's allowlist so a bad value never reaches the buyer's page.
		if v, ok := in.Theme["product_image_size"].(string); ok && !ValidProductImageSize(v) {
			writeErr(w, http.StatusBadRequest, "product_image_size mesti small|medium|large|hidden")
			return
		}
		if v, ok := in.Theme["product_image_layout"].(string); ok && !ValidProductImageLayout(v) {
			writeErr(w, http.StatusBadRequest, "product_image_layout mesti thumbnail-left|image-top")
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
	// Pass config/theme as-is: Forms.Update COALESCEs nil over the stored
	// values, so a PATCH that omits either key keeps what's stored instead
	// of writing NULL into a NOT NULL jsonb column (SQLSTATE 23502, issue
	// #16). Callers that want to clear a map send {} explicitly.
	formID := r.PathValue("id")
	if err := h.Forms.Update(r.Context(), actx.Account.ID, formID,
		in.Title, in.Config, in.Theme); err != nil {
		respondErr(w, r, err)
		return
	}
	// The slug is a separate write because a rename is one atomic operation
	// that must also leave the temporary 301 for the old URL behind it (see
	// FormStore.RenameSlug). An OMITTED slug keeps the stored value — the same
	// "leave unchanged" contract config/theme have — because the empty string
	// is skipped here. That is what the console's form-settings save sends for
	// every other field, and it is what makes the documented PATCH a partial
	// update rather than a full replace.
	//
	// Before this, the field was not read at all: the console POSTed a slug,
	// the endpoint ignored it, and the save reported {status:"updated"} while
	// the form's URL had not moved. A merchant concluded the rename worked and
	// his new URL existed — it 404'd (the carded defect).
	if in.Slug != "" {
		oldSlug, err := h.Forms.RenameSlug(r.Context(), actx.Account.ID, formID, in.Slug)
		if err != nil {
			// A collision is 409 (respondErr maps ErrSlugTaken) and an
			// invalid slug 400 — never a 200 that changed nothing.
			respondErr(w, r, err)
			return
		}
		if oldSlug != "" {
			h.recordEvent(r, actx, h.sessionOwnerID(r), "form.slug_renamed",
				map[string]any{"form_id": formID, "from": oldSlug, "to": strings.ToLower(strings.TrimSpace(in.Slug))})
		}
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
	respond(w, http.StatusOK, map[string]string{"status": "published"})
}

// writePublishNeedsAck answers a publish that a payment form with no products
// cannot complete silently. The status is 409 because nothing was changed: the
// caller asked for a state it has to confirm first. `code` is the stable token
// the dashboard matches on (web/src/lib/publishResult.ts holds the same value),
// so it never has to parse the message.
func writePublishNeedsAck(w http.ResponseWriter) {
	writeJSON(w, http.StatusConflict, map[string]any{
		"error": "Borang bayaran ini tiada produk — ia tidak boleh menerima bayaran. " +
			"Tambah produk dahulu, atau sahkan untuk teruskan.",
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
	// Onboarding gap: entering a LIVE status with a payment form that has
	// nothing to sell is the same silent mistake the publish route refuses, and
	// the dashboard's status control reaches THIS route. Gated on the
	// transition, so re-saving a form that is already live is never blocked.
	if dbstore.IsLiveStatus(in.Status) {
		f, ferr := h.Forms.GetByID(r.Context(), actx.Account.ID, r.PathValue("id"))
		if ferr != nil {
			respondErr(w, r, ferr)
			return
		}
		if dbstore.PublishNeedsAck(f, in.Status) && !in.Acknowledge && r.URL.Query().Get("acknowledge") != "1" {
			writePublishNeedsAck(w)
			return
		}
	}
	switch in.Status {
	case dbstore.FormStatusDraft, dbstore.FormStatusPublished, dbstore.FormStatusUnlisted:
		// Leaving private clears the stored access password.
		if err := h.Forms.SetStatusWithPassword(r.Context(), actx.Account.ID, r.PathValue("id"), in.Status, ""); err != nil {
			respondErr(w, r, err)
			return
		}
	case dbstore.FormStatusPrivate:
		if len(in.Password) < 4 {
			writeErr(w, http.StatusBadRequest, "password mesti sekurang-kurangnya 4 aksara")
			return
		}
		hash, err := auth.HashPassword(in.Password)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "gagal hash password")
			return
		}
		if err := h.Forms.SetStatusWithPassword(r.Context(), actx.Account.ID, r.PathValue("id"), in.Status, hash); err != nil {
			respondErr(w, r, err)
			return
		}
	default:
		writeErr(w, http.StatusBadRequest, "status mesti draft|private|published")
		return
	}
	// Issue #92: audit form status change.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "form.status_"+in.Status, map[string]any{"form_id": r.PathValue("id")})
	respond(w, http.StatusOK, map[string]string{"status": in.Status})
}

// deleteForm soft-deletes a single form (owner-checked via account scope).
func (h *RESTHandlers) deleteForm(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	if err := h.Forms.SoftDelete(r.Context(), actx.Account.ID, r.PathValue("id")); err != nil {
		respondErr(w, r, err)
		return
	}
	// Issue #92: audit form delete.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "form.deleted", map[string]any{"form_id": r.PathValue("id")})
	respond(w, http.StatusOK, map[string]string{"status": "deleted"})
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
				"tidak boleh dipulihkan: slug borang ini telah diambil borang lain — tukar slug borang itu dahulu, kemudian cuba pulihkan semula")
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
		writeErr(w, http.StatusBadRequest, "payload tidak sah")
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
			"taip slug borang ("+f.Slug+") untuk sahkan pemadaman kekal")
		return
	}
	if err := h.Forms.Purge(r.Context(), actx.Account.ID, formID); err != nil {
		if errors.Is(err, dbstore.ErrFormHasSales) {
			n, cerr := h.Forms.CountSalesByForm(r.Context(), actx.Account.ID, formID)
			if cerr != nil {
				n = 0
			}
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":       "borang ini mempunyai jualan — sejarah jualan tidak boleh dimusnahkan dengan memadam borang. Ia kekal dalam tong sampah.",
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
	respond(w, http.StatusOK, map[string]string{"status": "purged", "form_id": formID, "slug": f.Slug})
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
		writeErr(w, http.StatusBadRequest, "action mesti publish|draft|delete")
		return
	}
	if len(in.IDs) == 0 {
		writeErr(w, http.StatusBadRequest, "ids wajib")
		return
	}
	if len(in.IDs) > 500 {
		writeErr(w, http.StatusBadRequest, "ids terlalu banyak (maks 500)")
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
		writeErr(w, http.StatusBadRequest, "ids mesti uuid")
		return
	}

	var updated int64
	var skipped []string
	// Onboarding gap: ids published WITHOUT anything to sell, and which the
	// caller acknowledged. Reported alongside `skipped` so the merchant can see
	// which live forms still cannot take money.
	var needsProducts []string
	switch in.Action {
	case "publish", "draft":
		status := "published"
		if in.Action == "draft" {
			status = "draft"
		}
		// Issue #594: a bulk publish skips templates that carry no category or
		// no description text block (they would reach the gallery as a bare
		// title), while still publishing the ordinary forms in the same batch.
		// Collect the skipped ids so the caller is told, instead of the skip
		// looking like a no-op.
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
						"error": "Sebahagian borang bayaran tiada produk, jadi ia tidak " +
							"diterbitkan. Tambah produk dahulu, atau sahkan untuk teruskan.",
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
	h.recordEvent(r, actx, h.sessionOwnerID(r), "form.bulk_"+in.Action, map[string]any{
		"action": in.Action, "ids": ids, "updated": updated, "skipped": len(skipped),
	})
	out := map[string]any{"updated": updated}
	if len(skipped) > 0 {
		out["skipped"] = skipped
		out["skipped_reason"] = "template perlu category dan satu blok text (description) " +
			"sebelum diterbitkan ke galeri"
	}
	if len(needsProducts) > 0 {
		out["needs_products"] = needsProducts
		out["needs_products_reason"] = "borang bayaran tanpa produk — ia diterbitkan, " +
			"tetapi tidak boleh menerima bayaran sehingga produk ditambah"
	}
	writeJSON(w, http.StatusOK, out)
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
		writeErr(w, http.StatusBadRequest, "nama key wajib")
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
		"note": "Simpan key ini sekarang — ia tidak akan dipapar lagi.",
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
	sum, err := h.summaryData(r.Context(), scope.Accounts)
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
	RefCode     string `json:"ref_code"`
	Status      string `json:"status"`
	TotalSen    int64  `json:"total_sen"`
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
	FormID      string    `json:"form_id,omitempty"`
	SaleType    string    `json:"sale_type,omitempty"`
	InvoiceNo   string    `json:"invoice_no,omitempty"`
	BuyerEmail  string    `json:"buyer_email,omitempty"`
	BuyerPhone  string    `json:"buyer_phone,omitempty"`
	SubtotalSen int64     `json:"subtotal_sen,omitempty"`
	ShippingSen int64     `json:"shipping_sen,omitempty"`
	DiscountSen int64     `json:"discount_sen,omitempty"`
	Items       []itemRow `json:"items,omitempty"`
	Updated     string    `json:"updated_at,omitempty"`
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
		Method: s.PaymentMethod, Buyer: s.Client.FullName,
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
		Updated:        s.UpdatedAt.UTC().Format(time.RFC3339),
		TableNumber:    s.TableNumber,
		RefundedSen:    s.RefundedSen,
		OutstandingSen: s.OutstandingSen(),
		RefundStatus:   s.RefundStatus(),
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

// saleEmailStatus returns the per-sale receipt email delivery timeline
// (issue #106): Sent → Delivered → Opened → (Clicked), with timestamps,
// sourced from email_delivery_events correlated to this sale's ref. The
// "sent" step is implied by the sale having a receipt_message_id (the SES
// MessageID captured at send time); the rest come from SES events.
func (h *RESTHandlers) saleEmailStatus(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	ref := r.PathValue("ref")
	sale, err := h.Sales.ByRefCode(r.Context(), ref)
	if err != nil || sale.AccountID != actx.Account.ID {
		writeErr(w, http.StatusNotFound, "jualan tidak dijumpai")
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
		writeErr(w, http.StatusServiceUnavailable, "pembayaran tidak aktif")
		return
	}
	ref := r.PathValue("ref")
	sale, err := h.Sales.ByRefCode(r.Context(), ref)
	if err != nil || sale.AccountID != actx.Account.ID {
		writeErr(w, http.StatusNotFound, "jualan tidak dijumpai")
		return
	}
	var in struct {
		AmountSen *int64 `json:"amount_sen,omitempty"`
	}
	_ = decodeJSON(r, &in)
	if err := h.Pay.CaptureSale(r.Context(), sale, in.AmountSen); err != nil {
		recordPaymentFailure("capture")
		writeErr(w, http.StatusBadRequest, err.Error())
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
		writeErr(w, http.StatusServiceUnavailable, "pembayaran tidak aktif")
		return
	}
	ref := r.PathValue("ref")
	sale, err := h.Sales.ByRefCode(r.Context(), ref)
	if err != nil || sale.AccountID != actx.Account.ID {
		writeErr(w, http.StatusNotFound, "jualan tidak dijumpai")
		return
	}
	if err := h.Pay.ReleaseSale(r.Context(), sale); err != nil {
		recordPaymentFailure("release")
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.Sales.SetStatus(r.Context(), sale.ID, dbstore.SaleCancelled); err != nil {
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
func (h *RESTHandlers) refundSale(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	if h.Pay == nil {
		writeErr(w, http.StatusServiceUnavailable, "pembayaran tidak aktif")
		return
	}
	ref := r.PathValue("ref")
	sale, err := h.Sales.ByRefCode(r.Context(), ref)
	if err != nil || sale.AccountID != actx.Account.ID {
		writeErr(w, http.StatusNotFound, "jualan tidak dijumpai")
		return
	}
	var in struct {
		AmountSen *int64 `json:"amount_sen,omitempty"`
	}
	_ = decodeJSON(r, &in)
	res, err := h.Pay.RefundSale(r.Context(), sale, in.AmountSen)
	if err != nil {
		recordPaymentFailure("refund")
		writeErr(w, http.StatusBadRequest, err.Error())
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
		writeErr(w, http.StatusServiceUnavailable, "pembayaran tidak aktif")
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
			writeErr(w, http.StatusBadRequest, "refs mesti array atau \"all\"")
			return
		}
		if len(refs) > 200 {
			writeErr(w, http.StatusBadRequest, "refs terlalu banyak (maks 200)")
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
		if !sale.SkipCapture || sale.CapturedAt != nil || sale.ChipPurchaseID == "" {
			res.Error = "bukan authorisasi yang menunggu capture"
		} else if err := h.Pay.CaptureSale(r.Context(), sale, nil); err != nil {
			recordPaymentFailure("bulk_capture")
			res.Error = err.Error()
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
		writeErr(w, http.StatusNotFound, "jualan tidak dijumpai")
		return
	}
	if err := h.Sales.SetStatus(r.Context(), sale.ID, dbstore.SaleConfirmed); err != nil {
		respondErr(w, r, err)
		return
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
