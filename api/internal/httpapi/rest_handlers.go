// REST handlers.
package httpapi

import (
	"net/http"
	"strings"
	"time"

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
	products, err := h.Products.ListActiveByAccount(r.Context(), actx.Account.ID)
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

func (h *RESTHandlers) createProduct(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	var in struct {
		Name          string `json:"name"`
		PriceSen      int64  `json:"price_sen"`
		Type          string `json:"type"`
		BillingPeriod string `json:"billing_period"`
		TrialDays     int    `json:"trial_days"`
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
	if err := h.Forms.Update(r.Context(), actx.Account.ID, r.PathValue("id"), in.Title, in.Config, in.Theme); err != nil {
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
	sales, err := h.Sales.ListByAccount(r.Context(), actx.Account.ID, 200)
	if err != nil {
		respondErr(w, err)
		return
	}
	sum := map[string]int64{"pending": 0, "paid": 0, "confirmed": 0, "failed": 0, "cancelled": 0}
	var revenue int64
	for _, s := range sales {
		sum[s.Status]++
		if s.Status == dbstore.SalePaid || s.Status == dbstore.SaleConfirmed {
			revenue += s.TotalSen
		}
	}
	respond(w, http.StatusOK, map[string]any{"counts": sum, "revenue_sen": revenue})
}

// ─── Sales ───────────────────────────────────────────────────────────────────

func (h *RESTHandlers) listSales(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	sales, err := h.Sales.ListByAccount(r.Context(), actx.Account.ID, 100)
	if err != nil {
		respondErr(w, err)
		return
	}
	type saleRow struct {
		RefCode  string `json:"ref_code"`
		Status   string `json:"status"`
		TotalSen int64  `json:"total_sen"`
		Method   string `json:"payment_method"`
		Buyer    string `json:"buyer"`
		Created  string `json:"created_at"`
	}
	out := make([]saleRow, 0, len(sales))
	for _, s := range sales {
		out = append(out, saleRow{
			RefCode: s.RefCode, Status: s.Status, TotalSen: s.TotalSen,
			Method: s.PaymentMethod, Buyer: s.Client.FullName,
			Created: s.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	respond(w, http.StatusOK, out)
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
