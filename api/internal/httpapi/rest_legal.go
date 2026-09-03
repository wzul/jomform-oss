// Per-space hosted legal pages (issue #184): privacy / terms / refund.
// The merchant can auto-generate a draft via the LLM (deepseek-v4-flash) with
// a prompt template filled with their business details, preview/edit it, then
// save. They can also write their own manually. The stored content is rendered
// on the public pages ({space}.jomform.com/privacy, /terms, /refund).
package httpapi

import (
	"errors"
	"fmt"
	"html"
	"net/http"
	"strings"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/llm"
)

// validLegalPageType reports whether a page type is one of the three hosted
// legal pages.
func validLegalPageType(t string) bool {
	return t == dbstore.LegalPagePrivacy || t == dbstore.LegalPageTerms || t == dbstore.LegalPageRefund
}

// getSpaceLegalPages: GET /v1/spaces/{id}/legal — returns all three legal
// pages (privacy/terms/refund) with their mode and content.
func (h *RESTHandlers) getSpaceLegalPages(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	s, _, ok := h.spaceAccessByID(w, r)
	if !ok {
		return
	}
	pages, err := h.Spaces.LegalPages(r.Context(), s.ID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, pages)
}

// generateSpaceLegalPage: POST /v1/spaces/{id}/legal/generate
// {type, business_name, registration_no, contact, address, data_collected,
//  payment_terms, refund_terms, website, effective_date}
// Calls the LLM with a prompt template filled with the merchant's business
// details and returns the generated draft for review (not yet saved).
func (h *RESTHandlers) generateSpaceLegalPage(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	_, role, ok := h.spaceAccessByID(w, r)
	if !ok {
		return
	}
	if !roleAtLeast(role, dbstore.RoleManage) {
		writeErr(w, http.StatusForbidden, "perlu peranan manage atau admin")
		return
	}
	if h.LLM == nil {
		writeErr(w, http.StatusServiceUnavailable, "penjanaan LLM tidak tersedia")
		return
	}
	var in struct {
		Type           string `json:"type"`
		BusinessName   string `json:"business_name"`
		RegistrationNo string `json:"registration_no"`
		Contact        string `json:"contact"`
		Address        string `json:"address"`
		DataCollected  string `json:"data_collected"`
		PaymentTerms   string `json:"payment_terms"`
		RefundTerms    string `json:"refund_terms"`
		Website        string `json:"website"`
		EffectiveDate  string `json:"effective_date"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "payload tidak sah")
		return
	}
	if !validLegalPageType(in.Type) {
		writeErr(w, http.StatusBadRequest, "type mesti privacy|terms|refund")
		return
	}
	content, err := h.LLM.GenerateLegalPage(r.Context(), in.Type, llm.BusinessDetails{
		BusinessName:   in.BusinessName,
		RegistrationNo: in.RegistrationNo,
		Contact:        in.Contact,
		Address:        in.Address,
		DataCollected:  in.DataCollected,
		PaymentTerms:   in.PaymentTerms,
		RefundTerms:    in.RefundTerms,
		Website:        in.Website,
		EffectiveDate:  in.EffectiveDate,
	})
	if err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"type":    in.Type,
		"content": content,
		"mode":    dbstore.LegalModeAuto,
	})
}

// saveSpaceLegalPage: PUT /v1/spaces/{id}/legal
// {type, content, mode: auto|manual} — stores the page content.
func (h *RESTHandlers) saveSpaceLegalPage(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	s, role, ok := h.spaceAccessByID(w, r)
	if !ok {
		return
	}
	if !roleAtLeast(role, dbstore.RoleManage) {
		writeErr(w, http.StatusForbidden, "perlu peranan manage atau admin")
		return
	}
	var in struct {
		Type    string `json:"type"`
		Content string `json:"content"`
		Mode    string `json:"mode"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "payload tidak sah")
		return
	}
	if !validLegalPageType(in.Type) {
		writeErr(w, http.StatusBadRequest, "type mesti privacy|terms|refund")
		return
	}
	if in.Mode != dbstore.LegalModeAuto && in.Mode != dbstore.LegalModeManual {
		writeErr(w, http.StatusBadRequest, "mode mesti auto|manual")
		return
	}
	if err := h.Spaces.SaveLegalPage(r.Context(), &dbstore.LegalPage{
		SpaceID:  s.ID,
		PageType: in.Type,
		Mode:     in.Mode,
		Content:  in.Content,
	}); err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

// ── Public legal pages ───────────────────────────────────────────────────────

// handlePublicLegalPage renders a space's hosted legal page
// ({space}.jomform.com/privacy, /terms, /refund). 404 when the space is
// unknown or the page has no stored content.
func (h *PublicHandlers) handlePublicLegalPage(w http.ResponseWriter, r *http.Request) {
	sub := accountSubdomain(r.Host)
	if sub == "" {
		http.NotFound(w, r)
		return
	}
	// The page type is the last path segment (/privacy, /terms, /refund).
	pageType := strings.TrimPrefix(r.URL.Path, "/")
	if !validLegalPageType(pageType) {
		http.NotFound(w, r)
		return
	}
	spaceID, err := h.Spaces.IDBySlug(r.Context(), sub)
	if err != nil || spaceID == "" {
		http.NotFound(w, r)
		return
	}
	page, err := h.Spaces.LegalPage(r.Context(), spaceID, pageType)
	if err != nil {
		if errors.Is(err, dbstore.ErrLegalPageNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "ralat", http.StatusInternalServerError)
		return
	}
	title := map[string]string{
		dbstore.LegalPagePrivacy: "Privacy Policy",
		dbstore.LegalPageTerms:   "Terms of Service",
		dbstore.LegalPageRefund:  "Refund Policy",
	}[pageType]
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	writeLegalPageHTML(w, title, sub, page.Content)
}

// writeLegalPageHTML renders a hosted legal page. The content is merchant
// text/HTML; it is escaped so no scripts can run (no XSS on the public page).
func writeLegalPageHTML(w http.ResponseWriter, title, spaceSlug, content string) {
	esc := html.EscapeString(content)
	// Render simple line breaks as paragraphs so plain-text drafts read well.
	paras := strings.Split(esc, "\n\n")
	var body strings.Builder
	for _, p := range paras {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		body.WriteString("<p>" + strings.ReplaceAll(p, "\n", "<br>") + "</p>\n")
	}
	fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>%s — %s</title>
<style>body{background:#f7f8f8;color:#17181c;font-family:system-ui,sans-serif;margin:0;padding:40px 16px}
.c{max-width:720px;margin:0 auto;background:#fff;border:1px solid #e6e8ec;border-radius:16px;padding:40px}
h1{font-size:24px;margin:0 0 4px}.s{color:#9ca3af;font-size:12px;margin:0 0 24px}
p{font-size:14px;line-height:1.7;color:#374151;margin:0 0 14px}
.f{color:#9ca3af;font-size:12px;margin-top:28px;text-align:center}</style></head><body><div class="c">
<h1>%s</h1><p class="s">%s.jomform.com</p>
%s
<p class="f">Dikuasakan oleh JomForm</p></div></body></html>`,
		title, spaceSlug, title, spaceSlug, body.String())
}
