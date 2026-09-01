// REST handlers.
package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// ─── Products ────────────────────────────────────────────────────────────────

type restProduct struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	PriceSen      int64  `json:"price_sen"`
	Type          string `json:"type"`
	BillingPeriod string `json:"billing_period,omitempty"`
	Active        bool   `json:"active"`
}

func (h *RESTHandlers) listProducts(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	scope, ok := h.scopeOr403(w, r, actx)
	if !ok {
		return
	}
	products, err := h.Products.ListActiveByAccounts(r.Context(), scope.Accounts)
	if err != nil {
		respondErr(w, err)
		return
	}
	out := make([]restProduct, 0, len(products))
	for _, p := range products {
		out = append(out, restProduct{
			ID: p.ID, Name: p.Name, PriceSen: p.PriceSen,
			Type: p.Type, BillingPeriod: p.BillingPeriod, Active: p.Active,
		})
	}
	respond(w, http.StatusOK, out)
}

func (h *RESTHandlers) deleteProduct(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if err := h.Products.Archive(r.Context(), actx.Account.ID, r.PathValue("id")); err != nil {
		respondErr(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]string{"status": "archived"})
}

func (h *RESTHandlers) createProduct(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	var in struct {
		Name          string `json:"name"`
		PriceSen      int64  `json:"price_sen"`
		Type          string `json:"type"`
		BillingPeriod string `json:"billing_period"`
		TrialDays     int    `json:"trial_days"`
		Stock         *int   `json:"stock"` // nil = unlimited
	}
	if err := decodeJSON(r, &in); err != nil || in.Name == "" || in.PriceSen < 0 {
		writeErr(w, http.StatusBadRequest, "nama dan price_sen wajib")
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
	p := &dbstore.Product{
		AccountID: actx.Account.ID, Name: in.Name, PriceSen: in.PriceSen,
		Type: pType, BillingPeriod: period, TrialDays: in.TrialDays, Active: true,
		Stock: in.Stock,
	}
	if err := h.Products.Create(r.Context(), p); err != nil {
		respondErr(w, err)
		return
	}
	respond(w, http.StatusCreated, restProduct{
		ID: p.ID, Name: p.Name, PriceSen: p.PriceSen, Type: p.Type, BillingPeriod: p.BillingPeriod, Active: true,
	})
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

func formURL(base, slug string) string { return base + "/f/" + slug }

func (h *RESTHandlers) listForms(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	forms, err := h.Forms.ListByAccount(r.Context(), actx.Account.ID)
	if err != nil {
		respondErr(w, err)
		return
	}
	out := make([]restForm, 0, len(forms))
	for _, f := range forms {
		out = append(out, restForm{
			ID: f.ID, Slug: f.Slug, Title: f.Title, Status: f.Status,
			URL:       formURL("", f.Slug),
			UpdatedAt: f.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
	respond(w, http.StatusOK, out)
}

func (h *RESTHandlers) getForm(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	f, err := h.Forms.GetByID(r.Context(), actx.Account.ID, r.PathValue("id"))
	if err != nil {
		respondErr(w, err)
		return
	}
	respond(w, http.StatusOK, restForm{
		ID: f.ID, Slug: f.Slug, Title: f.Title, Status: f.Status,
		URL: formURL("", f.Slug), Config: f.Config, Theme: f.Theme,
		UpdatedAt: f.UpdatedAt.UTC().Format(time.RFC3339),
	})
}

func (h *RESTHandlers) createForm(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
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
		respondErr(w, err)
		return
	}
	respond(w, http.StatusCreated, restForm{
		ID: f.ID, Slug: f.Slug, Title: f.Title, Status: f.Status,
		URL: formURL("", f.Slug), UpdatedAt: f.UpdatedAt.UTC().Format(time.RFC3339),
	})
}

func (h *RESTHandlers) updateForm(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
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
		respondErr(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *RESTHandlers) publishForm(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	// Publishing requires CHIP configured OR at least one product; CHIP-only store
	// means a form without CHIP setup can still publish for bank-direct? Per spec:
	// CHIP-only. Warn but allow (bank_direct exists).
	if err := h.Forms.SetStatus(r.Context(), actx.Account.ID, r.PathValue("id"), "published"); err != nil {
		respondErr(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]string{"status": "published"})
}

// deleteForm soft-deletes a single form (owner-checked via account scope).
func (h *RESTHandlers) deleteForm(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if err := h.Forms.SoftDelete(r.Context(), actx.Account.ID, r.PathValue("id")); err != nil {
		respondErr(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// bulkForms applies an action to many forms at once:
// POST /v1/rest/forms/bulk {"action":"publish|draft|delete","ids":["uuid",...]}
// Returns {"updated": n} — how many forms the caller's account actually owns
// and that were still eligible (not already soft-deleted).
func (h *RESTHandlers) bulkForms(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
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
			respondErr(w, err)
			return
		}
		updated = tag
	case "delete":
		n, err := h.Forms.SoftDeleteMany(r.Context(), actx.Account.ID, ids)
		if err != nil {
			respondErr(w, err)
			return
		}
		updated = n
	}
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
		respondErr(w, err)
		return
	}
	out := make([]restKey, 0, len(keys))
	for _, k := range keys {
		out = append(out, restKey{ID: k.ID, Name: k.Name, Scopes: k.Scopes})
	}
	respond(w, http.StatusOK, out)
}

func (h *RESTHandlers) createKey(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
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
		respondErr(w, err)
		return
	}
	k, err := h.Keys.Create(r.Context(), actx.Account.ID, hash, in.Name, in.Scopes)
	if err != nil {
		respondErr(w, err)
		return
	}
	// The raw key is shown exactly once.
	respond(w, http.StatusCreated, map[string]any{
		"id": k.ID, "name": k.Name, "scopes": k.Scopes, "key": raw,
		"note": "Simpan key ini sekarang — ia tidak akan dipapar lagi.",
	})
}

func (h *RESTHandlers) revokeKey(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if err := h.Keys.Revoke(r.Context(), actx.Account.ID, r.PathValue("id")); err != nil {
		respondErr(w, err)
		return
	}
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
		respondErr(w, err)
		return
	}
	respond(w, http.StatusOK, sum)
}

// ─── Sales ───────────────────────────────────────────────────────────────────

// restSaleRows maps sales to the dashboard JSON shape. Shared by
// GET /v1/rest/sales and the dashboard bundle (issue #29).
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
}

func restSaleRows(sales []*dbstore.Sale) []saleRow {
	out := make([]saleRow, 0, len(sales))
	for _, s := range sales {
		row := saleRow{
			RefCode: s.RefCode, Status: s.Status, TotalSen: s.TotalSen,
			Method: s.PaymentMethod, Buyer: s.Client.FullName,
			Created:     s.CreatedAt.UTC().Format(time.RFC3339),
			SkipCapture: s.SkipCapture, Authorized: s.Authorized,
		}
		if s.CapturedAt != nil {
			row.CapturedAt = s.CapturedAt.UTC().Format(time.RFC3339)
		}
		out = append(out, row)
	}
	return out
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
		out := make([]saleRow, 0, len(sales))
		for _, s := range sales {
			out = append(out, saleRow{
				RefCode: s.RefCode, Status: s.Status, TotalSen: s.TotalSen,
				Method: s.PaymentMethod, Buyer: s.Client.FullName,
				Created:     s.CreatedAt.UTC().Format(time.RFC3339),
				SkipCapture: s.SkipCapture, Authorized: s.Authorized, CapturedAt: capturedAt(s),
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
			respondErr(w, err)
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
	if formID != "" {
		sales, total, err = h.Sales.ListByAccountsFormPage(r.Context(), scope.Accounts, formID, page, limit)
	} else {
		sales, total, err = h.Sales.ListByAccountsPage(r.Context(), scope.Accounts, page, limit)
	}
	if err != nil {
		respondErr(w, err)
		return
	}
	out := restSaleRows(sales)
	respond(w, http.StatusOK, map[string]any{
		"items": out, "page": page, "limit": limit, "total": total,
	})
}

func (h *RESTHandlers) captureSale(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
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
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.Sales.MarkCaptured(r.Context(), sale.ID); err != nil {
		respondErr(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]string{"status": "captured", "ref_code": ref})
}

func (h *RESTHandlers) releaseSale(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
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
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.Sales.SetStatus(r.Context(), sale.ID, dbstore.SaleCancelled); err != nil {
		respondErr(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]string{"status": "released", "ref_code": ref})
}

func (h *RESTHandlers) confirmSale(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	ref := r.PathValue("ref")
	sale, err := h.Sales.ByRefCode(r.Context(), ref)
	if err != nil || sale.AccountID != actx.Account.ID {
		writeErr(w, http.StatusNotFound, "jualan tidak dijumpai")
		return
	}
	if err := h.Sales.SetStatus(r.Context(), sale.ID, dbstore.SaleConfirmed); err != nil {
		respondErr(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]string{"status": "confirmed", "ref_code": ref})
}

func orEmptyMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}
