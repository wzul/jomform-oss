// Workspace management: list/create/switch + per-workspace settings.
package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/urlguard"
)

// listWorkspaces: GET /v1/workspaces — workspaces owned by OR a member of the current
// user (issue #162). Each row carries the caller's role so the frontend can
// gate UI on it.
func (h *RESTHandlers) listWorkspaces(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	userID := h.sessionOwnerID(r)
	if userID == "" {
		writeErr(w, http.StatusUnauthorized, "auth diperlukan")
		return
	}
	workspaces, err := h.Workspaces.ByMember(r.Context(), userID)
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
	out := make([]row, 0, len(workspaces))
	for _, s := range workspaces {
		out = append(out, row{ID: s.WorkspaceID, Name: s.Name, Slug: s.Slug, Role: s.Role})
	}
	writeJSON(w, http.StatusOK, out)
}

// createWorkspace: POST /v1/workspaces
func (h *RESTHandlers) createWorkspace(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
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
	sp, err := h.Workspaces.Create(r.Context(), userID, strings.TrimSpace(in.Name), slug)
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

// workspaceAccessByID resolves a workspace by id and verifies the caller is the owner
// or a member (issue #162). Returns the workspace + the caller's role.
func (h *RESTHandlers) workspaceAccessByID(w http.ResponseWriter, r *http.Request) (*dbstore.Workspace, string, bool) {
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
	s, err := h.Workspaces.ByID(r.Context(), sID)
	if err != nil || s == nil {
		writeErr(w, http.StatusNotFound, "workspace tidak dijumpai")
		return nil, "", false
	}
	acc, ok, err := h.Workspaces.AccessByID(r.Context(), sID, userID)
	if err != nil || !ok {
		writeErr(w, http.StatusForbidden, "bukan workspace anda")
		return nil, "", false
	}
	return s, acc.Role, true
}

// getWorkspaceSettings GET/PUT for the storefront builder.
func (h *RESTHandlers) getWorkspaceSettings(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	s, _, ok := h.workspaceAccessByID(w, r)
	if !ok {
		return
	}
	set, err := h.Workspaces.Settings(r.Context(), s.ID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, set)
}

// saveWorkspaceSettings: PUT /v1/workspaces/{id}/settings
func (h *RESTHandlers) saveWorkspaceSettings(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
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
		Currency       string `json:"currency"`
		// Issue #276: per-workspace SEO indexing toggle. Omitted (nil) = keep
		// current value; explicit true/false sets it.
		Indexing *bool `json:"indexing"`
		// Issue #360: customizable landing-page config. Omitted (nil) = keep
		// current value; provided = replace. Enabled=false (or empty) falls
		// back to the auto-generated bare listing.
		LandingPage *dbstore.LandingPage `json:"landing_page"`
		// Issue #447: per-workspace realtime notification sound toggle.
		// Omitted (nil) = keep current value; explicit true/false sets it.
		SoundEnabled *bool `json:"sound_enabled"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "payload tidak sah")
		return
	}
	// Issue #501: ONE shared scheme rule for every caller-supplied URL field
	// (see internal/urlguard). The landing-page image_url/logo_url are
	// rendered into buyer-facing `<img src>` attributes (public.go), so an
	// unsafe scheme must be rejected on this path too — previously only the
	// MCP landing tool validated them.
	for _, f := range []struct{ name, u string }{
		{"privacy_url", in.PrivacyURL},
		{"terms_url", in.TermsURL},
		{"refund_url", in.RefundURL},
	} {
		if !urlguard.Valid(f.u) {
			writeErr(w, http.StatusBadRequest, urlguard.InvalidMessage(f.name))
			return
		}
	}
	if in.LandingPage != nil {
		for _, f := range []struct{ name, u string }{
			{"image_url", in.LandingPage.ImageURL},
			{"logo_url", in.LandingPage.LogoURL},
		} {
			if !urlguard.Valid(f.u) {
				writeErr(w, http.StatusBadRequest, urlguard.InvalidMessage("landing_page."+f.name))
				return
			}
		}
	}
	// Issue #185: currency must be a supported code (default MYR). Empty is
	// treated as "keep current" by the read-modify-write below.
	if in.Currency != "" && !dbstore.IsSupportedCurrency(in.Currency) {
		writeErr(w, http.StatusBadRequest, "mata wang tidak disokong")
		return
	}
	s, role, ok := h.workspaceAccessByID(w, r)
	if !ok {
		return
	}
	if !roleAtLeast(role, dbstore.RoleManage) {
		writeErr(w, http.StatusForbidden, "perlu peranan manage atau admin")
		return
	}
	// Read-modify-write: omitted fields keep their current value (issue #185
	// currency change applies to new products/sales going forward; historical
	// sales keep their original currency).
	cur, err := h.Workspaces.Settings(r.Context(), s.ID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	st := &dbstore.WorkspaceSettings{
		WorkspaceID:        s.ID,
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
		Currency:       cur.Currency,
		Indexing:       cur.Indexing,
		// Issue #360: preserve the current landing page unless one is supplied.
		LandingPage: cur.LandingPage,
		// Issue #447: preserve the current sound toggle unless one is supplied.
		SoundEnabled: cur.SoundEnabled,
	}
	if in.Currency != "" {
		st.Currency = in.Currency
	}
	if in.Indexing != nil {
		st.Indexing = *in.Indexing
	}
	if in.LandingPage != nil {
		st.LandingPage = in.LandingPage
	}
	if in.SoundEnabled != nil {
		st.SoundEnabled = *in.SoundEnabled
	}
	if strings.TrimSpace(st.MenuJSON) == "" {
		st.MenuJSON = "[]"
	}
	if err := h.Workspaces.SaveSettings(r.Context(), st); err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

// ── Per-workspace email settings (issue #107) ────────────────────────────────────

// getWorkspaceEmailSettings: GET /v1/workspaces/{id}/email-settings
func (h *RESTHandlers) getWorkspaceEmailSettings(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	s, _, ok := h.workspaceAccessByID(w, r)
	if !ok {
		return
	}
	set, err := h.Workspaces.EmailSettings(r.Context(), s.ID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, set)
}

// saveWorkspaceEmailSettings: PUT /v1/workspaces/{id}/email-settings
func (h *RESTHandlers) saveWorkspaceEmailSettings(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
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
	s, role, ok := h.workspaceAccessByID(w, r)
	if !ok {
		return
	}
	if !roleAtLeast(role, dbstore.RoleManage) {
		writeErr(w, http.StatusForbidden, "perlu peranan manage atau admin")
		return
	}
	// Read-modify-write: omitted fields keep their current value.
	st, err := h.Workspaces.EmailSettings(r.Context(), s.ID)
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
	if err := h.Workspaces.SaveEmailSettings(r.Context(), st); err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}
