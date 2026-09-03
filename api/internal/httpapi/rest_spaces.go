// Space management: list/create/switch + per-space settings.
package httpapi

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/wzul/jomform/api/internal/dbstore"
	"strings"
)

// listSpaces: GET /v1/spaces — spaces owned by OR a member of the current
// user (issue #162). Each row carries the caller's role so the frontend can
// gate UI on it.
func (h *RESTHandlers) listSpaces(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	userID := h.sessionOwnerID(r)
	if userID == "" {
		writeErr(w, http.StatusUnauthorized, "auth diperlukan")
		return
	}
	spaces, err := h.Spaces.ByMember(r.Context(), userID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	type row struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Slug string `json:"slug"`
		Role string `json:"role"`
	}
	out := make([]row, 0, len(spaces))
	for _, s := range spaces {
		out = append(out, row{ID: s.SpaceID, Name: s.Name, Slug: s.Slug, Role: s.Role})
	}
	writeJSON(w, http.StatusOK, out)
}

// createSpace: POST /v1/spaces
func (h *RESTHandlers) createSpace(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	userID := h.sessionOwnerID(r)
	if userID == "" {
		writeErr(w, http.StatusUnauthorized, "auth diperlukan")
		return
	}
	var in struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if err := decodeJSON(r, &in); err != nil || in.Name == "" || in.Slug == "" {
		writeErr(w, http.StatusBadRequest, "name dan slug wajib")
		return
	}
	slug := strings.ToLower(strings.TrimSpace(in.Slug))
	if len(slug) < 3 || len(slug) > 40 {
		writeErr(w, http.StatusBadRequest, "slug 3-40 aksara")
		return
	}
	for _, c := range slug {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			writeErr(w, http.StatusBadRequest, "slug: a-z, 0-9, dash sahaja")
			return
		}
	}
	if strings.HasSuffix(slug, "jomform") {
		writeErr(w, http.StatusBadRequest, "slug tidak sah")
		return
	}
	sp, err := h.Spaces.Create(r.Context(), userID, strings.TrimSpace(in.Name), slug)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeErr(w, http.StatusConflict, "slug sudah digunakan")
			return
		}
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{
		"id": sp.ID, "name": sp.Name, "slug": sp.Slug,
		"url": "https://" + sp.Slug + ".jomform.com",
	})
}

// spaceAccessByID resolves a space by id and verifies the caller is the owner
// or a member (issue #162). Returns the space + the caller's role.
func (h *RESTHandlers) spaceAccessByID(w http.ResponseWriter, r *http.Request) (*dbstore.Space, string, bool) {
	sID := r.PathValue("id")
	if _, perr := uuid.Parse(sID); perr != nil {
		writeErr(w, http.StatusBadRequest, "id tidak sah")
		return nil, "", false
	}
	userID := h.sessionOwnerID(r)
	if userID == "" {
		writeErr(w, http.StatusUnauthorized, "auth diperlukan")
		return nil, "", false
	}
	s, err := h.Spaces.ByID(r.Context(), sID)
	if err != nil || s == nil {
		writeErr(w, http.StatusNotFound, "space tidak dijumpai")
		return nil, "", false
	}
	acc, ok, err := h.Spaces.AccessByID(r.Context(), sID, userID)
	if err != nil || !ok {
		writeErr(w, http.StatusForbidden, "bukan space anda")
		return nil, "", false
	}
	return s, acc.Role, true
}

// getSpaceSettings GET/PUT for the storefront builder.
func (h *RESTHandlers) getSpaceSettings(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	s, _, ok := h.spaceAccessByID(w, r)
	if !ok {
		return
	}
	set, err := h.Spaces.Settings(r.Context(), s.ID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, set)
}

// saveSpaceSettings: PUT /v1/spaces/{id}/settings
func (h *RESTHandlers) saveSpaceSettings(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	var in struct {
		FooterText     string `json:"footer_text"`
		CompanyReg     string `json:"company_reg"`
		PrivacyURL     string `json:"privacy_url"`
		TermsURL       string `json:"terms_url"`
		RefundURL      string `json:"refund_url"`
		ContactPhone   string `json:"contact_phone"`
		ContactAddress string `json:"contact_address"`
		BusinessName   string `json:"business_name"`
		MenuJSON       string `json:"menu_json"`
		DefaultLang    string `json:"default_lang"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "payload tidak sah")
		return
	}
	for _, u := range []string{in.PrivacyURL, in.TermsURL, in.RefundURL} {
		if u != "" && !strings.HasPrefix(u, "https://") {
			writeErr(w, http.StatusBadRequest, "URL mesti https://")
			return
		}
	}
	s, role, ok := h.spaceAccessByID(w, r)
	if !ok {
		return
	}
	if !roleAtLeast(role, dbstore.RoleManage) {
		writeErr(w, http.StatusForbidden, "perlu peranan manage atau admin")
		return
	}
	st := &dbstore.SpaceSettings{
		SpaceID:        s.ID,
		FooterText:     in.FooterText,
		CompanyReg:     in.CompanyReg,
		PrivacyURL:     in.PrivacyURL,
		TermsURL:       in.TermsURL,
		RefundURL:      in.RefundURL,
		ContactPhone:   in.ContactPhone,
		ContactAddress: in.ContactAddress,
		BusinessName:   in.BusinessName,
		MenuJSON:       in.MenuJSON,
		DefaultLang:    in.DefaultLang,
	}
	if strings.TrimSpace(st.MenuJSON) == "" {
		st.MenuJSON = "[]"
	}
	if err := h.Spaces.SaveSettings(r.Context(), st); err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

// ── Per-space email settings (issue #107) ────────────────────────────────────

// getSpaceEmailSettings: GET /v1/spaces/{id}/email-settings
func (h *RESTHandlers) getSpaceEmailSettings(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	s, _, ok := h.spaceAccessByID(w, r)
	if !ok {
		return
	}
	set, err := h.Spaces.EmailSettings(r.Context(), s.ID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, set)
}

// saveSpaceEmailSettings: PUT /v1/spaces/{id}/email-settings
func (h *RESTHandlers) saveSpaceEmailSettings(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	var in struct {
		ReceiptSubject      string `json:"receipt_subject"`
		ReceiptBody         string `json:"receipt_body"`
		ReceiptEnabled      *bool  `json:"receipt_enabled"`
		NotifyMerchant      *bool  `json:"notify_merchant"`
		MerchantNotifyEmail string `json:"merchant_notify_email"`
		HTML                *bool  `json:"html"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "payload tidak sah")
		return
	}
	s, role, ok := h.spaceAccessByID(w, r)
	if !ok {
		return
	}
	if !roleAtLeast(role, dbstore.RoleManage) {
		writeErr(w, http.StatusForbidden, "perlu peranan manage atau admin")
		return
	}
	// Read-modify-write: omitted fields keep their current value.
	st, err := h.Spaces.EmailSettings(r.Context(), s.ID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	st.ReceiptSubject = in.ReceiptSubject
	st.ReceiptBody = in.ReceiptBody
	st.MerchantNotifyEmail = in.MerchantNotifyEmail
	if in.ReceiptEnabled != nil {
		st.ReceiptEnabled = *in.ReceiptEnabled
	}
	if in.NotifyMerchant != nil {
		st.NotifyMerchant = *in.NotifyMerchant
	}
	if in.HTML != nil {
		st.HTML = *in.HTML
	}
	if err := h.Spaces.SaveEmailSettings(r.Context(), st); err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}
