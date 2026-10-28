// Template gallery: browse system templates, clone into your account.
package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/wzul/jomform/api/internal/customform"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/formfields"
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
		// Description = the form's intro line (card t_7fb80479): the first
		// section/html block's value, falling back to the first text block's
		// value for a row written while `text` still meant prose. Never a
		// field's label — `text` is an input now, and its value is the label.
		desc = formfields.Description(f.Config)
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
	// Card t_d9b179d9: this older door copies tpl.Theme verbatim onto a real
	// form, so it is the same bypass useOwnTemplate closes — a template row that
	// already carries a blocked custom_js (written before the scan existed, or by
	// a writer that never scanned) would reach a buyer's page as live code by
	// being cloned. Both body shapes resolve into `tpl` above, so this one arm
	// covers the account-scoped {template_id} clone and the gallery {slug} clone.
	if refuseBlockedCustomJS(w, tpl.Theme) {
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

// refuseBlockedCustomJS answers the same 400 (`custom_js ditolak: <reason>`) the
// form write paths (updateForm, createForm) and both MCP form doors answer, and
// reports whether it refused.
//
// Card t_5c35bfeb: every other guard on the template write paths below is
// justified by "a template's config/theme is later cloned onto a real form"
// (use_template / useOwnTemplate copies tpl.Theme verbatim), yet custom_js was
// never scanned there — so a value the form write paths refuse was storable on a
// template and then cloned onto a real form, whose block-rendered buyer page
// serves theme.custom_js verbatim. One rule, one wording, every door.
//
// A nil theme means "leave unchanged" on update and "nothing supplied" on
// create, so it is not a refusal; a non-string custom_js is not JS and is left
// to the renderer's type handling, exactly as on the form paths.
func refuseBlockedCustomJS(w http.ResponseWriter, theme map[string]any) bool {
	if theme == nil {
		return false
	}
	js, ok := theme["custom_js"].(string)
	if !ok {
		return false
	}
	if reason := customform.ScanCustomJS(js); reason != "" {
		writeErr(w, http.StatusBadRequest, customform.RejectedMessage(reason))
		return true
	}
	return false
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
	// Card t_5c35bfeb: a template's theme is cloned onto a real form
	// (useOwnTemplate) and the block-rendered buyer page serves theme.custom_js
	// verbatim, so a value the form write paths refuse must not be storable here
	// — otherwise this route is a bypass around that refusal. NOTE the {form_id}
	// branch below replaces theme with src.Theme, which the form write paths
	// already scanned; this covers the from-scratch `theme` input.
	if refuseBlockedCustomJS(w, in.Theme) {
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
	// Card t_8fcb546d: a PRESENT but non-array `config.blocks` makes the
	// gallery-metadata predicate's jsonb_array_elements raise SQLSTATE 22023,
	// which fails the bulk publish and the skip listing for every id in the
	// batch. The scheme check above silently skipped it (a type assertion that
	// does not match falls through), so the shape is refused here instead.
	if field, want := urlguard.CheckConfigShape(in.Config); field != "" {
		writeErr(w, http.StatusBadRequest, urlguard.InvalidShapeMessage("config."+field, want))
		return
	}
	// Card t_4086b8f0: a template's config is cloned onto a real form
	// (use_template / useOwnTemplate), so a block type no renderer can honour
	// must not be storable here either — otherwise the refusal on the form
	// write paths is simply bypassed through a template.
	if bt := formfields.UnsupportedBlock(in.Config); bt != "" {
		writeErr(w, http.StatusBadRequest, formfields.UnsupportedBlockMessage(bt))
		return
	}
	// Card t_3d11f98d: a template's config is cloned onto a real form, so an
	// option/shipping block with no resolvable price must not be storable here
	// either — same refusal as the form write paths.
	if msg := formfields.UnpricedPricingBlock(in.Config); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	// Card t_73a5db6e: a template's config is cloned onto a real form
	// (useOwnTemplate), so an unsatisfiable custom-form contract stored here
	// would reach a buyer's page by the back door — the same refusal the form
	// write paths apply.
	if err := customform.ValidateAndNormalizeConfig(in.Config); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
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
	// Card t_5c35bfeb: same custom_js refusal as createOwnTemplate — an update
	// that stores a blocked value re-arms the clone door (useOwnTemplate copies
	// tpl.Theme onto a real form). nil theme = leave unchanged, so it is skipped.
	if refuseBlockedCustomJS(w, in.Theme) {
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
	// Card t_8fcb546d: same present-but-not-array `config.blocks` refusal as
	// createOwnTemplate — an update that stores the shape reintroduces the
	// SQLSTATE 22023 failure the read side now tolerates.
	if field, want := urlguard.CheckConfigShape(in.Config); field != "" {
		writeErr(w, http.StatusBadRequest, urlguard.InvalidShapeMessage("config."+field, want))
		return
	}
	// Card t_4086b8f0: same unsupported-block refusal as createOwnTemplate.
	if bt := formfields.UnsupportedBlock(in.Config); bt != "" {
		writeErr(w, http.StatusBadRequest, formfields.UnsupportedBlockMessage(bt))
		return
	}
	// Card t_3d11f98d: same unresolved-pricing refusal as createOwnTemplate.
	if msg := formfields.UnpricedPricingBlock(in.Config); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	// Card t_73a5db6e: same unsatisfiable-custom-form refusal as
	// createOwnTemplate (nil config = leave unchanged, and is skipped).
	if err := customform.ValidateAndNormalizeConfig(in.Config); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
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
	// Card t_5c35bfeb: the clone is where a template's theme becomes a real
	// form's theme, so the refusal is enforced here too — not only on the two
	// write paths above. That closes the door for a row that predates the scan,
	// or that another writer stored: whatever put the value in the template, it
	// must not reach a buyer's page as live code by being cloned.
	if refuseBlockedCustomJS(w, tpl.Theme) {
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
