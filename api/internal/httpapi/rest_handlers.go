// REST handlers.
package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
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
type restVariation struct {
	ID        string   `json:"id,omitempty"`
	Name      string   `json:"name"`
	PriceSen  int64    `json:"price_sen"`
	Stock     *int     `json:"stock,omitempty"` // nil = unlimited
	Images    []string `json:"images,omitempty"`
	SortOrder int      `json:"sort_order,omitempty"`
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
			if !urlguard.ValidList(v.Images) {
				writeErr(w, http.StatusBadRequest, urlguard.InvalidMessage("variations[].images"))
				return
			}
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
		if !urlguard.ValidList(v.Images) {
			writeErr(w, http.StatusBadRequest, urlguard.InvalidMessage("variations[].images"))
			return
		}
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
	ID     string `json:"id"`
	Slug   string `json:"slug"`
	Title  string `json:"title"`
	Status string `json:"status"`
	// IsPrivate reports whether the form is password-protected (issue #176).
	// The password itself is never returned.
	IsPrivate bool           `json:"is_private"`
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
	return formURL(h.workspaceSlugFor(ctx, acc.ID), slug)
}

// workspaceSlugFor resolves the workspace slug an account's forms live under, for
// building workspace-scoped public URLs. Returns "" when the account has no
// linked workspace (no public URL).
func (h *RESTHandlers) workspaceSlugFor(ctx context.Context, accountID string) string {
	if h.Workspaces == nil {
		return ""
	}
	slug, err := h.Workspaces.WorkspaceSlugByAccount(ctx, accountID)
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
	// Issue #193: pre-create the default notification (notify merchant on
	// sale.paid) so a new form sends the merchant a new-sale email with zero
	// configuration. Best-effort.
	if h.Notifications != nil {
		_ = h.Notifications.EnsureDefault(r.Context(), f.ID)
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
	if err := h.Forms.SetStatus(r.Context(), actx.Account.ID, r.PathValue("id"), dbstore.FormStatusPublished); err != nil {
		respondErr(w, r, err)
		return
	}
	// Issue #92: audit form publish.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "form.published", map[string]any{"form_id": r.PathValue("id")})
	respond(w, http.StatusOK, map[string]string{"status": "published"})
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
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
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
	var skipped []string
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
			// Issue #518: build the full row (not a subset) so the legacy
			// array shape carries refunded_sen / outstanding_sen /
			// refund_status too — a partial refund must be visible on EVERY
			// read surface, and the legacy shape is the dashboard's.
			row := toSaleRow(s, emailStatus[s.RefCode])
			row.CapturedAt = capturedAt(s)
			out = append(out, row)
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
