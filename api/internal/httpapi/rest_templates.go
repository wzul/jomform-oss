// Template gallery: browse system templates, clone into your account.
package httpapi

import (
	"net/http"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// listTemplates exposes published template forms (any visitor; used by
// landing + onboarding). No auth required.
func (h *RESTHandlers) listTemplates(w http.ResponseWriter, r *http.Request) {
	forms, err := h.Forms.ListTemplates(r.Context())
	if err != nil {
		respondErr(w, r, err)
		return
	}
	type row struct {
		ID    string         `json:"id"`
		Slug  string         `json:"slug"`
		Title string         `json:"title"`
		Theme map[string]any `json:"theme"`
	}
	out := make([]row, 0, len(forms))
	for _, f := range forms {
		out = append(out, row{ID: f.ID, Slug: f.Slug, Title: f.Title, Theme: f.Theme})
	}
	writeJSON(w, http.StatusOK, out)
}

// useTemplate clones a template config/theme into a new draft for the caller.
func (h *RESTHandlers) useTemplate(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	var in struct {
		Slug string `json:"slug"`
		Name string `json:"name,omitempty"`
	}
	if err := decodeJSON(r, &in); err != nil || in.Slug == "" {
		writeErr(w, http.StatusBadRequest, "slug wajib")
		return
	}
	tpl, err := h.Forms.TemplateBySlug(r.Context(), in.Slug)
	if err != nil {
		writeErr(w, http.StatusNotFound, "template tidak dijumpai")
		return
	}
	title := in.Name
	if title == "" {
		title = tpl.Title
	}
	f := &dbstore.Form{
		AccountID:       actx.Account.ID,
		Slug:            tpl.Slug + "-" + randTokenHex(3),
		Title:           title,
		Config:          tpl.Config,
		Theme:           tpl.Theme,
		Status:          "draft",
		AllowEmbed:      true,
		LogoURL:         tpl.LogoURL,
		RedirectSuccess: tpl.RedirectSuccess,
		RedirectFailure: tpl.RedirectFailure,
	}
	if err := h.Forms.Create(r.Context(), f); err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": f.ID, "slug": f.Slug, "title": f.Title,
	})
}
