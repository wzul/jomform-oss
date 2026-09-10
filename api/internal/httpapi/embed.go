// Embed endpoint: iframe-friendly form rendering (CSP frame-ancestors *).
package httpapi

import (
	"net/http"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// embedForm serves the public form with permissive framing for merchant sites.
// Path pattern is registered as GET /embed/f/{accslug}/{slug}.
func (h *PublicHandlers) embedForm(w http.ResponseWriter, r *http.Request) {
	accSlug := r.PathValue("accslug")
	slug := r.PathValue("slug")
	f, err := h.Forms.ByWorkspaceSlug(r.Context(), accSlug, slug)
	if err != nil {
		writeErr(w, http.StatusNotFound, "borang tidak dijumpai")
		return
	}
	if f.Status == dbstore.FormStatusDraft {
		writeErr(w, http.StatusNotFound, "borang tidak dijumpai")
		return
	}
	if !f.AllowEmbed {
		writeErr(w, http.StatusForbidden, "embed dinyahaktifkan")
		return
	}
	w.Header().Set("Content-Security-Policy", "frame-ancestors *")
	w.Header().Set("X-Frame-Options", "ALLOWALL")
	if !h.renderBlocks(w, r, f) {
		// Legacy form (no builder blocks): render the legacy layout.
		h.serveForm(w, r, f)
	}
}
