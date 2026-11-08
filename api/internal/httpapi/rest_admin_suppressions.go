// Super-admin suppression list endpoints (issue #601).
//
// The suppression list existed since issue #56 but had no surface: SES
// ingestion could add an address and nothing could read one back, so a
// super-admin could not see which addresses the platform had stopped mailing,
// could not add one by hand (a verbal complaint, an off-platform request), and
// could not remove a false positive without a psql session.
//
// These are the three endpoints behind the /admin suppression section, mirroring
// the admin_dead_letters handlers in rest_admin.go: every handler calls
// requireSuperAdmin FIRST (403 for anyone else), audits the access, and only
// then touches the store. The dashboard reaches them through the Next proxy
// (/api/admin/suppressions → /v1/admin/suppressions).
package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// adminSuppressionOut is one row of the suppression list as the dashboard
// consumes it. created_by is the actor's users.id for a manual add and is
// omitted for ingestion; created_by_email is the resolved display form and is
// also omitted when the actor is unknown, so the UI renders its own "—" rather
// than an empty string it might mistake for a value.
type adminSuppressionOut struct {
	Email          string `json:"email"`
	Reason         string `json:"reason"`
	Source         string `json:"source"`
	Note           string `json:"note,omitempty"`
	CreatedBy      string `json:"created_by,omitempty"`
	CreatedByEmail string `json:"created_by_email,omitempty"`
	CreatedAt      string `json:"created_at"`
}

func toAdminSuppressionOut(se dbstore.SuppressedEmail) adminSuppressionOut {
	return adminSuppressionOut{
		Email:          se.Email,
		Reason:         se.Reason,
		Source:         se.Source,
		Note:           se.Note,
		CreatedBy:      se.CreatedBy,
		CreatedByEmail: se.CreatedByEmail,
		CreatedAt:      se.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// handleAdminListSuppressions: GET /v1/admin/suppressions?q=&page=&limit= —
// one page of the list, newest first, with the total for the pager.
// Super-admin only, read-only, audited.
func (h *RESTHandlers) handleAdminListSuppressions(w http.ResponseWriter, r *http.Request, _ *AccountCtx) {
	if !h.requireSuperAdmin(r) {
		writeErr(w, http.StatusForbidden, "super-admin is required for this action")
		return
	}
	h.auditSuperAdminAccess(r, "admin.suppressions.list")
	if h.Suppressions == nil {
		writeErr(w, http.StatusServiceUnavailable, "the suppression list is not configured")
		return
	}
	page, limit := parsePageLimit(r)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	rows, total, err := h.Suppressions.Search(r.Context(), q, page, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to read the suppression list")
		return
	}
	out := make([]adminSuppressionOut, 0, len(rows))
	for _, se := range rows {
		out = append(out, toAdminSuppressionOut(se))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": out, "page": page, "limit": limit, "total": total,
	})
}

type adminAddSuppressionIn struct {
	Email  string `json:"email"`
	Reason string `json:"reason"`
	Note   string `json:"note"`
}

// handleAdminAddSuppression: POST /v1/admin/suppressions — add an address to
// the list by hand. Super-admin only, audited.
//
// The address is lowercased and trimmed before validation, so " B@Example.com "
// is stored as "b@example.com" and cannot create a second row beside the
// existing lower-case one. Idempotent: adding an address already on the list
// returns 200 with created=false rather than an error, because from the
// operator's point of view the address IS suppressed either way.
func (h *RESTHandlers) handleAdminAddSuppression(w http.ResponseWriter, r *http.Request, _ *AccountCtx) {
	if !h.requireSuperAdmin(r) {
		writeErr(w, http.StatusForbidden, "super-admin is required for this action")
		return
	}
	h.auditSuperAdminAccess(r, "admin.suppressions.add")
	if h.Suppressions == nil {
		writeErr(w, http.StatusServiceUnavailable, "the suppression list is not configured")
		return
	}
	var in adminAddSuppressionIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	addr := strings.ToLower(strings.TrimSpace(in.Email))
	if !auth.ValidEmail(addr) {
		writeErr(w, http.StatusBadRequest, "invalid email")
		return
	}
	reason := strings.ToLower(strings.TrimSpace(in.Reason))
	if reason == "" {
		reason = dbstore.SuppressionReasonManual
	}
	if !dbstore.IsCanonicalReason(reason) {
		writeErr(w, http.StatusBadRequest, "invalid reason (hard_bounce | complaint | manual)")
		return
	}

	// Attribute the row to the super-admin who added it. A manual add with no
	// actor would be indistinguishable from an ingestion row.
	actor := h.sessionOwnerID(r)
	created, err := h.Suppressions.Add(r.Context(), addr, reason, dbstore.SuppressionSourceManual,
		strings.TrimSpace(in.Note), actor)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to store the suppression")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "suppressed", "email": addr, "created": created,
	})
}

// handleAdminRemoveSuppression: DELETE /v1/admin/suppressions/{email} — remove
// an address so mail to it resumes. Super-admin only, audited.
//
// 204 when a row was removed, 404 when the address was not on the list. The
// address arrives percent-encoded (the "@" of an address is escaped by the
// caller), and net/http has already decoded the path value by the time
// PathValue returns it.
func (h *RESTHandlers) handleAdminRemoveSuppression(w http.ResponseWriter, r *http.Request, _ *AccountCtx) {
	if !h.requireSuperAdmin(r) {
		writeErr(w, http.StatusForbidden, "super-admin is required for this action")
		return
	}
	h.auditSuperAdminAccess(r, "admin.suppressions.remove")
	if h.Suppressions == nil {
		writeErr(w, http.StatusServiceUnavailable, "the suppression list is not configured")
		return
	}
	addr := strings.ToLower(strings.TrimSpace(r.PathValue("email")))
	if addr == "" {
		writeErr(w, http.StatusBadRequest, "invalid email")
		return
	}
	removed, err := h.Suppressions.RemoveIfPresent(r.Context(), addr)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to remove the suppression")
		return
	}
	if !removed {
		writeErr(w, http.StatusNotFound, "not in the list")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
