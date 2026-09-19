// Template gallery: browse system templates, clone into your account.
package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/urlguard"
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
		ID          string         `json:"id"`
		Slug        string         `json:"slug"`
		Title       string         `json:"title"`
		Description string         `json:"description,omitempty"`
		Category    string         `json:"category,omitempty"`
		Mode        string         `json:"mode,omitempty"`
		Theme       map[string]any `json:"theme"`
	}
	out := make([]row, 0, len(forms))
	for _, f := range forms {
		desc, cat, mode := "", "", ""
		if c, ok := f.Config["category"].(string); ok {
			cat = c
		}
		if m, ok := f.Config["mode"].(string); ok {
			mode = m
		}
		// Description = the first text block's value (a short intro line).
		if blocks, ok := f.Config["blocks"].([]any); ok {
			for _, b := range blocks {
				if bm, ok := b.(map[string]any); ok && bm["type"] == "text" {
					if s, ok := bm["value"].(string); ok {
						desc = s
						break
					}
				}
			}
		}
		out = append(out, row{ID: f.ID, Slug: f.Slug, Title: f.Title, Description: desc, Category: cat, Mode: mode, Theme: f.Theme})
	}
	writeJSON(w, http.StatusOK, out)
}

// useTemplate clones a template config/theme into a new draft for the caller.
// Accepts either a gallery `slug` (published system template) or a
// `template_id` naming one of the caller's own templates (issue #457).
func (h *RESTHandlers) useTemplate(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	var in struct {
		Slug       string `json:"slug"`
		TemplateID string `json:"template_id"`
		Name       string `json:"name,omitempty"`
	}
	if err := decodeJSON(r, &in); err != nil || (in.Slug == "" && in.TemplateID == "") {
		writeErr(w, http.StatusBadRequest, "slug atau template_id wajib")
		return
	}
	var tpl *dbstore.Form
	var err error
	if in.TemplateID != "" {
		// Issue #457: owner-scoped — another account's template is not found.
		tpl, err = h.Forms.TemplateByIDForAccount(r.Context(), actx.Account.ID, in.TemplateID)
	} else {
		tpl, err = h.Forms.TemplateBySlug(r.Context(), in.Slug)
	}
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
		Slug:            h.uniqueFormSlug(r.Context(), actx.Account.ID, tpl.Title),
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
	// Issue #193: a cloned form gets the default notification for its own mode
	// (issue #597).
	if h.Notifications != nil {
		_ = h.Notifications.EnsureDefault(r.Context(), f)
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": f.ID, "slug": f.Slug, "title": f.Title,
	})
}

// ── Issue #457: merchant-managed templates ──────────────────────────────────
//
// The dashboard Templates view manages the caller's OWN templates: save an
// existing form as a reusable template, list/edit/delete them, and clone one
// into a new draft. All reads and writes are account-scoped, so a template can
// never be read, edited or used across tenants. Merchant templates are stored
// as draft forms with is_template=true (see dbstore.FormStore), which keeps
// them out of the public gallery and out of the Forms list.

// restTemplate is the wire shape for a managed template.
type restTemplate struct {
	ID        string         `json:"id"`
	Slug      string         `json:"slug"`
	Title     string         `json:"title"`
	Config    map[string]any `json:"config,omitempty"`
	Theme     map[string]any `json:"theme,omitempty"`
	UpdatedAt string         `json:"updated_at"`
}

// listOwnTemplates: GET /v1/rest/templates
func (h *RESTHandlers) listOwnTemplates(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	rows, err := h.Forms.ListTemplatesByAccount(r.Context(), actx.Account.ID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	out := make([]restTemplate, 0, len(rows))
	for _, f := range rows {
		out = append(out, restTemplate{
			ID: f.ID, Slug: f.Slug, Title: f.Title,
			Config: f.Config, Theme: f.Theme,
			UpdatedAt: f.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// getOwnTemplate: GET /v1/rest/templates/{id}
func (h *RESTHandlers) getOwnTemplate(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	f, err := h.Forms.TemplateByIDForAccount(r.Context(), actx.Account.ID, r.PathValue("id"))
	if err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, restTemplate{
		ID: f.ID, Slug: f.Slug, Title: f.Title, Config: f.Config, Theme: f.Theme,
		UpdatedAt: f.UpdatedAt.UTC().Format(time.RFC3339),
	})
}

// createOwnTemplate: POST /v1/rest/templates
// Two ways to create a template:
//   - {form_id: "..."}            — save an existing form as a reusable template
//   - {title, config, theme}      — create one from scratch
//
// When both are sent the form wins and title/config/theme are ignored.
func (h *RESTHandlers) createOwnTemplate(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	var in struct {
		FormID string         `json:"form_id"`
		Title  string         `json:"title"`
		Config map[string]any `json:"config"`
		Theme  map[string]any `json:"theme"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "payload tidak sah")
		return
	}
	// Issue #501: a template's config/theme is later cloned onto a real form
	// (use_template / useOwnTemplate), so a non-https image URL stored here
	// would reach a buyer's page as #ZgotmplZ. Reject it at write time with
	// the ONE shared scheme rule.
	if field, _ := urlguard.CheckTheme(in.Theme); field != "" {
		writeErr(w, http.StatusBadRequest, urlguard.InvalidMessage("theme."+field))
		return
	}
	if field, _ := urlguard.CheckConfig(in.Config); field != "" {
		writeErr(w, http.StatusBadRequest, urlguard.InvalidMessage("config."+field))
		return
	}

	title := strings.TrimSpace(in.Title)
	config := in.Config
	theme := in.Theme

	if in.FormID != "" {
		src, err := h.Forms.GetByID(r.Context(), actx.Account.ID, in.FormID)
		if err != nil {
			respondErr(w, r, err)
			return
		}
		config = src.Config
		theme = src.Theme
		if title == "" {
			title = src.Title
		}
	}
	if title == "" {
		writeErr(w, http.StatusBadRequest, "title atau form_id wajib")
		return
	}

	f := &dbstore.Form{
		AccountID: actx.Account.ID,
		Slug:      h.uniqueFormSlug(r.Context(), actx.Account.ID, "tpl-"+title),
		Title:     title,
		Config:    orEmptyMap(config),
		Theme:     orEmptyMap(theme),
	}
	if err := h.Forms.CreateTemplate(r.Context(), f); err != nil {
		respondErr(w, r, err)
		return
	}
	// Issue #92: audit template creation.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "template.created", map[string]any{"template_id": f.ID})
	writeJSON(w, http.StatusCreated, restTemplate{
		ID: f.ID, Slug: f.Slug, Title: f.Title, Config: f.Config, Theme: f.Theme,
		UpdatedAt: f.UpdatedAt.UTC().Format(time.RFC3339),
	})
}

// updateOwnTemplate: PUT /v1/rest/templates/{id}
func (h *RESTHandlers) updateOwnTemplate(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	var in struct {
		Title  string         `json:"title"`
		Config map[string]any `json:"config"`
		Theme  map[string]any `json:"theme"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "payload tidak sah")
		return
	}
	// Issue #501: a template's config/theme is cloned onto a real form
	// (use_template / useOwnTemplate), so a non-https image URL stored here
	// would later reach a buyer's page as #ZgotmplZ. Reject it at write time
	// with the ONE shared scheme rule.
	if field, _ := urlguard.CheckTheme(in.Theme); field != "" {
		writeErr(w, http.StatusBadRequest, urlguard.InvalidMessage("theme."+field))
		return
	}
	if field, _ := urlguard.CheckConfig(in.Config); field != "" {
		writeErr(w, http.StatusBadRequest, urlguard.InvalidMessage("config."+field))
		return
	}
	existing, err := h.Forms.TemplateByIDForAccount(r.Context(), actx.Account.ID, r.PathValue("id"))
	if err != nil {
		respondErr(w, r, err)
		return
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = existing.Title
	}
	// nil config/theme mean "leave unchanged" (COALESCE in the store).
	if err := h.Forms.UpdateTemplate(r.Context(), actx.Account.ID, existing.ID, title, in.Config, in.Theme); err != nil {
		respondErr(w, r, err)
		return
	}
	// Issue #92: audit template update.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "template.updated", map[string]any{"template_id": existing.ID})
	f, err := h.Forms.TemplateByIDForAccount(r.Context(), actx.Account.ID, existing.ID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, restTemplate{
		ID: f.ID, Slug: f.Slug, Title: f.Title, Config: f.Config, Theme: f.Theme,
		UpdatedAt: f.UpdatedAt.UTC().Format(time.RFC3339),
	})
}

// deleteOwnTemplate: DELETE /v1/rest/templates/{id}
func (h *RESTHandlers) deleteOwnTemplate(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	if err := h.Forms.SoftDeleteTemplate(r.Context(), actx.Account.ID, r.PathValue("id")); err != nil {
		respondErr(w, r, err)
		return
	}
	// Issue #92: audit template delete.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "template.deleted", map[string]any{"template_id": r.PathValue("id")})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// useOwnTemplate: POST /v1/rest/templates/{id}/use
// Clones one of the caller's own templates into a new draft form.
func (h *RESTHandlers) useOwnTemplate(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	var in struct {
		Name string `json:"name,omitempty"`
	}
	_ = decodeJSON(r, &in)
	tpl, err := h.Forms.TemplateByIDForAccount(r.Context(), actx.Account.ID, r.PathValue("id"))
	if err != nil {
		respondErr(w, r, err)
		return
	}
	title := in.Name
	if title == "" {
		title = tpl.Title
	}
	f := &dbstore.Form{
		AccountID:       actx.Account.ID,
		Slug:            h.uniqueFormSlug(r.Context(), actx.Account.ID, tpl.Title),
		Title:           title,
		Config:          tpl.Config,
		Theme:           tpl.Theme,
		Status:          dbstore.FormStatusDraft,
		AllowEmbed:      true,
		LogoURL:         tpl.LogoURL,
		RedirectSuccess: tpl.RedirectSuccess,
		RedirectFailure: tpl.RedirectFailure,
	}
	if err := h.Forms.Create(r.Context(), f); err != nil {
		respondErr(w, r, err)
		return
	}
	if h.Notifications != nil {
		_ = h.Notifications.EnsureDefault(r.Context(), f)
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": f.ID, "slug": f.Slug, "title": f.Title,
	})
}
