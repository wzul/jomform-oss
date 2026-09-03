// REST handlers.
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// ─── Products ────────────────────────────────────────────────────────────────

// restVariation is the REST JSON shape of one product variation (money stays
// integer sen; stock nil = unlimited).
type restVariation struct {
	ID        string   `json:"id,omitempty"`
	Name      string   `json:"name"`
	PriceSen  int64    `json:"price_sen"`
	Stock     *int     `json:"stock,omitempty"` // nil = unlimited
	Images    []string `json:"images,omitempty"`
	SortOrder int      `json:"sort_order,omitempty"`
}

type restProduct struct {
	ID                  string           `json:"id"`
	Name                string           `json:"name"`
	Description         string           `json:"description,omitempty"`
	PriceSen            int64            `json:"price_sen"`
	Images              []string         `json:"images,omitempty"`
	Type                string           `json:"type"`
	BillingPeriod       string           `json:"billing_period,omitempty"`
	TrialDays           int              `json:"trial_days"`
	SubscriptionCharges *int             `json:"subscription_charges,omitempty"` // nil = infinite
	Active              bool             `json:"active"`
	Stock               *int             `json:"stock,omitempty"` // nil = unlimited
	Variations          []restVariation  `json:"variations,omitempty"`
	CreatedAt           string           `json:"created_at,omitempty"`
	UpdatedAt           string           `json:"updated_at,omitempty"`
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
		CreatedAt:           p.CreatedAt.UTC().Format(time.RFC3339),
	}
	if !p.UpdatedAt.IsZero() {
		out.UpdatedAt = p.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return out
}

// toRestVariations maps store variations to the REST JSON shape.
func toRestVariations(vs []*dbstore.ProductVariation) []restVariation {
	if len(vs) == 0 {
		return nil
	}
	out := make([]restVariation, 0, len(vs))
	for _, v := range vs {
		out = append(out, restVariation{
			ID: v.ID, Name: v.Name, PriceSen: v.PriceSen, Stock: v.Stock,
			Images: v.Images, SortOrder: v.SortOrder,
		})
	}
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
		Variations          *[]restVariation `json:"variations"` // nil = keep; [] = clear (simple)
		SubscriptionCharges **int            `json:"subscription_charges"` // nil = keep; &nil = infinite; &N = fixed count
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
	// Validate variations (money stays integer sen; stock nil = unlimited).
	if in.Variations != nil {
		for _, v := range *in.Variations {
			if v.Name == "" {
				writeErr(w, http.StatusBadRequest, "variation name wajib")
				return
			}
			if v.PriceSen < 0 {
				writeErr(w, http.StatusBadRequest, "variation price_sen mesti >= 0")
				return
			}
			if v.Stock != nil && *v.Stock < 0 {
				writeErr(w, http.StatusBadRequest, "variation stok mesti >= 0")
				return
			}
		}
	}
	if in.SubscriptionCharges != nil && *in.SubscriptionCharges != nil && **in.SubscriptionCharges < 1 {
		writeErr(w, http.StatusBadRequest, "subscription_charges mesti >= 1 (atau null untuk infinite)")
		return
	}
	err := h.Products.Update(r.Context(), actx.Account.ID, r.PathValue("id"),
		in.Name, in.Description, in.PriceSen, in.Images, in.Stock, in.Active,
		typPtr, periodPtr, in.TrialDays, in.SubscriptionCharges)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	// Wholesale-replace variations when the editor sent them (nil = keep).
	if in.Variations != nil && h.ProductVariations != nil {
		vs := make([]*dbstore.ProductVariation, 0, len(*in.Variations))
		for i, v := range *in.Variations {
			vs = append(vs, &dbstore.ProductVariation{
				Name: v.Name, PriceSen: v.PriceSen, Stock: v.Stock,
				Images: v.Images, SortOrder: v.SortOrder,
			})
			if v.SortOrder == 0 {
				vs[len(vs)-1].SortOrder = i
			}
		}
		if err := h.ProductVariations.ReplaceAll(r.Context(), r.PathValue("id"), vs); err != nil {
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

func (h *RESTHandlers) deleteProduct(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	if err := h.Products.Archive(r.Context(), actx.Account.ID, r.PathValue("id")); err != nil {
		respondErr(w, r, err)
		return
	}
	// Issue #92: audit product archive (delete).
	h.recordEvent(r, actx, h.sessionOwnerID(r), "product.deleted", map[string]any{"product_id": r.PathValue("id")})
	respond(w, http.StatusOK, map[string]string{"status": "archived"})
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
		PriceSen            int64           `json:"price_sen"`
		Type                string          `json:"type"`
		BillingPeriod       string          `json:"billing_period"`
		TrialDays           int             `json:"trial_days"`
		Stock               *int            `json:"stock"` // nil = unlimited
		Images              []string        `json:"images"`
		Variations          []restVariation `json:"variations"`
		SubscriptionCharges *int            `json:"subscription_charges"` // nil = infinite
	}
	if err := decodeJSON(r, &in); err != nil || in.Name == "" || in.PriceSen < 0 {
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
	for _, v := range in.Variations {
		if v.Name == "" {
			writeErr(w, http.StatusBadRequest, "variation name wajib")
			return
		}
		if v.PriceSen < 0 {
			writeErr(w, http.StatusBadRequest, "variation price_sen mesti >= 0")
			return
		}
		if v.Stock != nil && *v.Stock < 0 {
			writeErr(w, http.StatusBadRequest, "variation stok mesti >= 0")
			return
		}
	}
	p := &dbstore.Product{
		AccountID: actx.Account.ID, Name: in.Name, Description: in.Description,
		PriceSen: in.PriceSen, Images: in.Images, Type: pType,
		BillingPeriod: period, TrialDays: in.TrialDays, Active: true,
		Stock: in.Stock, SubscriptionCharges: in.SubscriptionCharges,
	}
	if err := h.Products.Create(r.Context(), p); err != nil {
		respondErr(w, r, err)
		return
	}
	// Persist variations wholesale when the editor sent them.
	if len(in.Variations) > 0 && h.ProductVariations != nil {
		vs := make([]*dbstore.ProductVariation, 0, len(in.Variations))
		for i, v := range in.Variations {
			order := v.SortOrder
			if order == 0 {
				order = i
			}
			vs = append(vs, &dbstore.ProductVariation{
				Name: v.Name, PriceSen: v.PriceSen, Stock: v.Stock,
				Images: v.Images, SortOrder: order,
			})
		}
		if err := h.ProductVariations.ReplaceAll(r.Context(), p.ID, vs); err != nil {
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
	ID        string         `json:"id"`
	Slug      string         `json:"slug"`
	Title     string         `json:"title"`
	Status    string         `json:"status"`
	URL       string         `json:"url"`
	Config    map[string]any `json:"config,omitempty"`
	Theme     map[string]any `json:"theme,omitempty"`
	UpdatedAt string         `json:"updated_at"`
}

// formURL builds the canonical public URL for a form. Issue #77: a public
// payment link is reachable only via its registered space subdomain
// ({space}.jomform.com/{slug}) or a custom domain — the legacy /f/{slug} form
// is gone. spaceSlug is the account's space slug ("" when unresolvable, in
// which case no URL is emitted).
func formURL(spaceSlug, slug string) string {
	if spaceSlug == "" {
		return ""
	}
	return "https://" + spaceSlug + ".jomform.com/" + slug
}

// formURLForAccount builds the canonical public URL for a form, preferring a
// verified custom domain over the space subdomain (issue #83). Returns ""
// when neither is resolvable.
func (h *RESTHandlers) formURLForAccount(ctx context.Context, acc *dbstore.Account, slug string) string {
	if acc == nil {
		return ""
	}
	if acc.CustomDomain != "" && acc.CustomDomainVerified {
		return "https://" + strings.TrimRight(acc.CustomDomain, "/") + "/" + slug
	}
	return formURL(h.spaceSlugFor(ctx, acc.ID), slug)
}

// spaceSlugFor resolves the space slug an account's forms live under, for
// building space-scoped public URLs. Returns "" when the account has no
// linked space (no public URL).
func (h *RESTHandlers) spaceSlugFor(ctx context.Context, accountID string) string {
	if h.Spaces == nil {
		return ""
	}
	slug, err := h.Spaces.SpaceSlugByAccount(ctx, accountID)
	if err != nil {
		return ""
	}
	return slug
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
	respond(w, http.StatusOK, restForm{
		ID: f.ID, Slug: f.Slug, Title: f.Title, Status: f.Status,
		URL: h.formURLForAccount(r.Context(), actx.Account, f.Slug), Config: f.Config, Theme: f.Theme,
		UpdatedAt: f.UpdatedAt.UTC().Format(time.RFC3339),
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
	if err := decodeJSON(r, &in); err != nil || in.Slug == "" || in.Title == "" {
		writeErr(w, http.StatusBadRequest, "slug dan title wajib")
		return
	}
	f := &dbstore.Form{
		AccountID: actx.Account.ID, Slug: strings.ToLower(in.Slug), Title: in.Title,
		Config: orEmptyMap(in.Config), Theme: orEmptyMap(in.Theme), Status: "draft",
	}
	if err := h.Forms.Create(r.Context(), f); err != nil {
		respondErr(w, r, err)
		return
	}
	// Issue #92: audit form creation.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "form.created", map[string]any{"form_id": f.ID, "slug": f.Slug})
	respond(w, http.StatusCreated, restForm{
		ID: f.ID, Slug: f.Slug, Title: f.Title, Status: f.Status,
		URL: h.formURLForAccount(r.Context(), actx.Account, f.Slug), UpdatedAt: f.UpdatedAt.UTC().Format(time.RFC3339),
	})
}

func (h *RESTHandlers) updateForm(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	var in struct {
		Title  string         `json:"title"`
		Config map[string]any `json:"config"`
		Theme  map[string]any `json:"theme"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	// Pass config/theme as-is: Forms.Update COALESCEs nil over the stored
	// values, so a PATCH that omits either key keeps what's stored instead
	// of writing NULL into a NOT NULL jsonb column (SQLSTATE 23502, issue
	// #16). Callers that want to clear a map send {} explicitly.
	if err := h.Forms.Update(r.Context(), actx.Account.ID, r.PathValue("id"),
		in.Title, in.Config, in.Theme); err != nil {
		respondErr(w, r, err)
		return
	}
	// Issue #92: audit form update.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "form.updated", map[string]any{"form_id": r.PathValue("id")})
	respond(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *RESTHandlers) publishForm(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	// Publishing requires CHIP configured OR at least one product; CHIP-only store
	// means a form without CHIP setup can still publish for bank-direct? Per spec:
	// CHIP-only. Warn but allow (bank_direct exists).
	if err := h.Forms.SetStatus(r.Context(), actx.Account.ID, r.PathValue("id"), "published"); err != nil {
		respondErr(w, r, err)
		return
	}
	// Issue #92: audit form publish.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "form.published", map[string]any{"form_id": r.PathValue("id")})
	respond(w, http.StatusOK, map[string]string{"status": "published"})
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
	switch in.Action {
	case "publish", "draft":
		status := "published"
		if in.Action == "draft" {
			status = "draft"
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
		"action": in.Action, "ids": ids, "updated": updated,
	})
	respond(w, http.StatusOK, map[string]int64{"updated": updated})
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
		EmailStatus: emailStatus,
		FormID:      s.FormID,
		SaleType:    s.SaleType,
		InvoiceNo:   s.InvoiceNo,
		BuyerEmail:  s.Client.Email,
		BuyerPhone:  s.Client.Phone,
		SubtotalSen: s.SubtotalSen,
		ShippingSen: s.ShippingSen,
		DiscountSen: s.DiscountSen,
		Updated:     s.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if s.CapturedAt != nil {
		row.CapturedAt = s.CapturedAt.UTC().Format(time.RFC3339)
	}
	if len(s.Items) > 0 {
		items := make([]itemRow, 0, len(s.Items))
		for _, it := range s.Items {
			items = append(items, itemRow{
				ProductID: it.ProductID,
				PriceSen:  it.PriceSen,
				Qty:       it.Qty,
				Name:      it.Name,
				VariationID:   it.VariationID,
				VariationName: it.VariationName,
			})
		}
		row.Items = items
	}
	return row
}

func (h *RESTHandlers) listSales(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	scope, ok := h.scopeOr403(w, r, actx)
	if !ok {
		return
	}
	capturedAt := func(s *dbstore.Sale) string {
		if s.CapturedAt == nil {
			return ""
		}
		return s.CapturedAt.UTC().Format(time.RFC3339)
	}
	writeRows := func(sales []*dbstore.Sale) {
		// Issue #106: surface the most recent receipt delivery event per sale.
		emailStatus := h.latestEmailStatuses(r.Context(), sales)
		out := make([]saleRow, 0, len(sales))
		for _, s := range sales {
			out = append(out, saleRow{
				RefCode: s.RefCode, Status: s.Status, TotalSen: s.TotalSen,
				Method: s.PaymentMethod, Buyer: s.Client.FullName,
				Created:     s.CreatedAt.UTC().Format(time.RFC3339),
				SkipCapture: s.SkipCapture, Authorized: s.Authorized, CapturedAt: capturedAt(s),
				EmailStatus: emailStatus[s.RefCode],
			})
		}
		respond(w, http.StatusOK, out)
	}

	// Legacy shape: plain array when the caller does not ask for paging.
	if r.URL.Query().Get("page") == "" {
		var sales []*dbstore.Sale
		var err error
		if formID := r.URL.Query().Get("form_id"); formID != "" {
			sales, err = h.Sales.ListByAccountForm(r.Context(), actx.Account.ID, formID, 100)
		} else {
			sales, err = h.Sales.ListByAccount(r.Context(), actx.Account.ID, 100)
		}
		if err != nil {
			respondErr(w, r, err)
			return
		}
		writeRows(sales)
		return
	}

	// Paginated shape: { items, page, limit, total }.
	page, limit := parsePageLimit(r)
	formID := r.URL.Query().Get("form_id")
	var sales []*dbstore.Sale
	var total int64
	var err error
	f := dbstore.SaleFilter{
		FormID:   formID,
		Status:   r.URL.Query().Get("status"),
		DateFrom: r.URL.Query().Get("date_from"),
		DateTo:   r.URL.Query().Get("date_to"),
		Q:        r.URL.Query().Get("q"),
	}
	// The dedicated /sales page (issue #126) always pages and uses the unified
	// filtered query whenever any filter is present; a bare form_id keeps the
	// existing per-account query path (cheap, already covered by tests).
	if f.Status != "" || f.DateFrom != "" || f.DateTo != "" || f.Q != "" {
		sales, total, err = h.Sales.ListFilteredPage(r.Context(), scope.Accounts, f, page, limit)
	} else if formID != "" {
		sales, total, err = h.Sales.ListByAccountsFormPage(r.Context(), scope.Accounts, formID, page, limit)
	} else {
		sales, total, err = h.Sales.ListByAccountsPage(r.Context(), scope.Accounts, page, limit)
	}
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
