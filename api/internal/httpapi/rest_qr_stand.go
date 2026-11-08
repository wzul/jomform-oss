// The merchant's own printable QR stand, behind the dashboard's session.
//
// WHY THIS ROUTE EXISTS. The public /qr/{slug}/stand route applies the same
// status gate the form's page applies (qr.go, qrPublicAccess): a DRAFT form's
// stand answers 404 because the stand prints the form's title, and an
// unpublished form's title must not be public. The dashboard's "Print stand"
// action used to open that public URL, so gating it alone would have taken the
// print artefact away from the merchant whose form is not live yet — the one
// person entitled to it.
//
// So the print action opens THIS route instead: it is behind the normal REST
// auth (session cookie or API key), it reads the form through the account scope
// (another account's id is a 404, never a stand), and it serves a draft, a
// private form, an unlisted form and a published one alike, because the caller
// has already proved they own it. The document itself is built by the ONE
// shared builder, writeQRStand, so the authed print and the public print cannot
// become two different artefacts.
//
// It deliberately does NOT sit under /qr/{slug}: that path space is the public
// one, and a slug is not an authorisation.
package httpapi

import (
	"net/http"
	"strings"
)

// getFormQRStand renders the printable QR stand for a form the caller owns,
// whatever its status. Route: GET /v1/rest/forms/{id}/qr-stand?table=N.
//
// Served as text/html so the dashboard can open it in a new tab and print it;
// Cache-Control is private, no-store because the document names the merchant's
// unpublished form (the same reason the unlocked og:image is private).
func (h *RESTHandlers) getFormQRStand(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	f, err := h.Forms.GetByID(r.Context(), actx.Account.ID, r.PathValue("id"))
	if err != nil {
		respondErr(w, r, err)
		return
	}
	// The public URL the printed QR must carry: the SAME canonical rule the
	// dashboard shows on the form (a verified custom domain first, then the
	// workspace subdomain — formURLForAccount).
	target := h.formURLForAccount(r.Context(), actx.Account, f.Slug)
	if target == "" {
		writeErr(w, http.StatusConflict, "the form's public URL cannot be resolved")
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeQRStand(w, f.Title, strings.TrimSpace(r.URL.Query().Get("table")), target)
}
