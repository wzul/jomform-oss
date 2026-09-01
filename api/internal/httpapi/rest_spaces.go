// Space management: list/create/switch + per-space settings.
package httpapi

import (
	"net/http"

	"github.com/wzul/jomform/api/internal/dbstore"
	"strings"
)

// listSpaces: GET /v1/spaces — spaces owned by the current user.
func (h *RESTHandlers) listSpaces(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	userID := h.sessionOwnerID(r)
	if userID == "" {
		writeErr(w, http.StatusUnauthorized, "auth diperlukan")
		return
	}
	spaces, err := h.Spaces.ByOwner(r.Context(), userID)
	if err != nil {
		respondErr(w, err)
		return
	}
	type row struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	out := make([]row, 0, len(spaces))
	for _, s := range spaces {
		out = append(out, row{ID: s.ID, Name: s.Name, Slug: s.Slug})
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
		respondErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{
		"id": sp.ID, "name": sp.Name, "slug": sp.Slug,
		"url": "https://" + sp.Slug + ".jomform.com",
	})
}

// spaceSettings GET/PUT for the storefront builder.
func (h *RESTHandlers) getSpaceSettings(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	sID := r.PathValue("id")
	s, err := h.Spaces.ByID(r.Context(), sID)
	if err != nil || s == nil {
		writeErr(w, http.StatusNotFound, "space tidak dijumpai")
		return
	}
	set, err := h.Spaces.Settings(r.Context(), sID)
	if err != nil {
		respondErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, set)
}

// saveSpaceSettings: PUT /v1/spaces/{id}/settings
func (h *RESTHandlers) saveSpaceSettings(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	var in struct {
		FooterText string `json:"footer_text"`
		CompanyReg string `json:"company_reg"`
		PrivacyURL string `json:"privacy_url"`
		TermsURL   string `json:"terms_url"`
		MenuJSON   string `json:"menu_json"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "payload tidak sah")
		return
	}
	for _, u := range []string{in.PrivacyURL, in.TermsURL} {
		if u != "" && !strings.HasPrefix(u, "https://") {
			writeErr(w, http.StatusBadRequest, "URL mesti https://")
			return
		}
	}
	st := &dbstore.SpaceSettings{
		SpaceID:    r.PathValue("id"),
		FooterText: in.FooterText,
		CompanyReg: in.CompanyReg,
		PrivacyURL: in.PrivacyURL,
		TermsURL:   in.TermsURL,
		MenuJSON:   in.MenuJSON,
	}
	if strings.TrimSpace(st.MenuJSON) == "" {
		st.MenuJSON = "[]"
	}
	if err := h.Spaces.SaveSettings(r.Context(), st); err != nil {
		respondErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}
