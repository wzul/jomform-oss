// Super-admin REST endpoints — platform-wide usage monitoring (issue #387).
//
// GET /v1/admin/overview returns platform-wide usage metrics (workspaces, forms,
// products, sales, storage, email, spam, recent signups, active workspaces) so
// the platform owner can visually monitor usage across all workspaces once
// JomForm is public. It is gated on the super-admin role (#385): every
// handler calls requireSuperAdmin first and audits the access afterwards.
// It is read-only — no destructive actions exist on the admin surface.
package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/objstore"
)

// isSuperAdmin reports whether the authenticated session user is a site
// super-admin (issue #385). It resolves the session user's email and checks
// it against the allowlist. A nil allowlist (SuperAdmin unset) or an
// unresolvable user is never a super-admin — fail closed.
func (h *RESTHandlers) isSuperAdmin(r *http.Request) bool {
	if h.SuperAdmin == nil || h.Users == nil {
		return false
	}
	uid := h.sessionOwnerID(r)
	if uid == "" {
		return false
	}
	u, err := h.Users.ByID(r.Context(), uid)
	if err != nil || u == nil {
		return false
	}
	return h.SuperAdmin(strings.TrimSpace(u.Email))
}

// requireSuperAdmin gates an admin-only endpoint. It returns false when the
// caller is not a super-admin. Every gated handler MUST call this before
// doing any work, and MUST call auditSuperAdminAccess afterwards.
func (h *RESTHandlers) requireSuperAdmin(r *http.Request) bool {
	return h.isSuperAdmin(r)
}

// auditSuperAdminAccess records a super-admin page/endpoint access to the
// audit log (issue #387 acceptance: audit admin page access/actions). It
// records who (user id + email), what (action), and when (created_at) as an
// `admin.super_admin_action` event. Best-effort: a nil Events store or a
// write error is logged, never fatal to the primary action.
func (h *RESTHandlers) auditSuperAdminAccess(r *http.Request, action string) {
	if h.Events == nil {
		return
	}
	uid := h.sessionOwnerID(r)
	if uid == "" {
		return
	}
	email := ""
	if u, err := h.Users.ByID(r.Context(), uid); err == nil && u != nil {
		email = u.Email
	}
	// Attribute the audit row to the super-admin's own account (every user
	// auto-provisions one on signup); fall back to the resolved account.
	accountID := ""
	if h.Accounts != nil {
		if accs, err := h.Accounts.ByOwner(r.Context(), uid); err == nil && len(accs) > 0 {
			accountID = accs[0].ID
		}
	}
	if accountID == "" {
		return
	}
	_ = h.Events.Record(r.Context(), accountID, uid, "admin.super_admin_action",
		map[string]any{"action": action, "email": email}, "", "")
}

// adminOverview is the response shape of GET /v1/admin/overview (issue #387).
type adminOverview struct {
	Totals           dbstore.PlatformTotals          `json:"totals"`
	Growth24h        dbstore.GrowthCounts            `json:"growth_24h"`
	Growth7d         dbstore.GrowthCounts            `json:"growth_7d"`
	Email            dbstore.EmailDeliverySummary    `json:"email"`
	Spam             dbstore.SpamQueueSize           `json:"spam"`
	RecentSignups    []dbstore.RecentSignup          `json:"recent_signups"`
	ActiveWorkspaces []dbstore.ActiveWorkspace       `json:"active_workspaces"`
	Storage          []dbstore.MediaUsageByWorkspace `json:"storage"`
	QuotaBytes       int64                           `json:"quota_bytes"`
}

// handleAdminOverview: GET /v1/admin/overview — platform-wide usage metrics.
// Super-admin only, read-only, audited (issue #387).
func (h *RESTHandlers) handleAdminOverview(w http.ResponseWriter, r *http.Request, _ *AccountCtx) {
	if !h.requireSuperAdmin(r) {
		writeErr(w, http.StatusForbidden, "perlu super-admin untuk tindakan ini")
		return
	}
	h.auditSuperAdminAccess(r, "admin.overview")

	ctx := r.Context()
	// Issue #545: reference the shared constant rather than a literal, so the
	// reported quota can never drift from the enforced one.
	out := adminOverview{QuotaBytes: objstore.MaxWorkspaceBytes} // 1GB per-workspace (#384)

	if h.Workspaces != nil {
		if t, err := h.Workspaces.PlatformTotals(ctx); err == nil {
			out.Totals = t
		}
		now := time.Now()
		if g, err := h.Workspaces.GrowthSince(ctx, now.Add(-24*time.Hour)); err == nil {
			out.Growth24h = g
		}
		if g, err := h.Workspaces.GrowthSince(ctx, now.Add(-7*24*time.Hour)); err == nil {
			out.Growth7d = g
		}
		if a, err := h.Workspaces.ActiveWorkspaces(ctx, 7); err == nil {
			out.ActiveWorkspaces = a
		}
	}
	if h.EmailDeliveryEvents != nil {
		if e, err := h.EmailDeliveryEvents.EmailDeliverySummary(ctx); err == nil {
			out.Email = e
		}
	}
	if h.Submissions != nil {
		if q, err := h.Submissions.SpamQueueSize(ctx); err == nil {
			out.Spam = q
		}
	}
	if h.Users != nil {
		if s, err := h.Users.RecentSignups(ctx, 10); err == nil {
			out.RecentSignups = s
		}
	}
	if h.Media != nil {
		if s, err := h.Media.UsageByWorkspace(ctx); err == nil {
			out.Storage = s
		}
	}

	respond(w, http.StatusOK, out)
}

// ── Dead-letter endpoints (issue #579) ──────────────────────────────────────

type adminDeadLetterOut struct {
	DedupKey  string `json:"dedup_key"`
	AccountID string `json:"account_id,omitempty"`
	EventType string `json:"event_type"`
	Attempts  int    `json:"attempts"`
	LastError string `json:"last_error"`
	CreatedAt string `json:"created_at"`
	DeadSince string `json:"dead_since"`
}

// handleAdminDeadLetters: GET /v1/admin/dead-letters — inbound callback events
// parked as dead. Super-admin only, read-only, audited (issue #579).
//
// Mirrors the admin_dead_letter_events MCP tool: the same store, the same gate.
// A parked event is a real gateway callback that was never processed, so it
// belongs on the admin page an operator already watches rather than only behind
// an MCP client.
func (h *RESTHandlers) handleAdminDeadLetters(w http.ResponseWriter, r *http.Request, _ *AccountCtx) {
	if !h.requireSuperAdmin(r) {
		writeErr(w, http.StatusForbidden, "perlu super-admin untuk tindakan ini")
		return
	}
	h.auditSuperAdminAccess(r, "admin.dead_letters")
	if h.WebhookEvents == nil {
		writeErr(w, http.StatusServiceUnavailable, "store peristiwa webhook tidak dikonfigurasi")
		return
	}
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	events, err := h.WebhookEvents.ListDead(r.Context(), limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membaca dead letter")
		return
	}
	out := make([]adminDeadLetterOut, 0, len(events))
	for _, e := range events {
		out = append(out, adminDeadLetterOut{
			DedupKey:  e.DedupKey,
			AccountID: e.AccountID,
			EventType: e.EventType,
			Attempts:  e.Attempts,
			LastError: e.LastError,
			CreatedAt: e.CreatedAt.UTC().Format(time.RFC3339),
			DeadSince: e.DeadSince.UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "total": len(out)})
}

type adminRequeueDeadLetterIn struct {
	DedupKey string `json:"dedup_key"`
}

// handleAdminRequeueDeadLetter: POST /v1/admin/dead-letters/requeue — revive a
// parked event so the replay retries it. Super-admin only, audited (issue #579).
//
// The store resets the attempt counter, so the event is not buried again by the
// same sweep pass before it has had a chance to run.
func (h *RESTHandlers) handleAdminRequeueDeadLetter(w http.ResponseWriter, r *http.Request, _ *AccountCtx) {
	if !h.requireSuperAdmin(r) {
		writeErr(w, http.StatusForbidden, "perlu super-admin untuk tindakan ini")
		return
	}
	h.auditSuperAdminAccess(r, "admin.requeue_dead_letter")
	if h.WebhookEvents == nil {
		writeErr(w, http.StatusServiceUnavailable, "store peristiwa webhook tidak dikonfigurasi")
		return
	}
	var in adminRequeueDeadLetterIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "badan permintaan tidak sah")
		return
	}
	if strings.TrimSpace(in.DedupKey) == "" {
		writeErr(w, http.StatusBadRequest, "dedup_key wajib diisi")
		return
	}
	ok, err := h.WebhookEvents.RequeueDead(r.Context(), in.DedupKey)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal menghidupkan semula peristiwa")
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, "peristiwa tidak dijumpai atau bukan dalam keadaan dead")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"requeued": true, "dedup_key": in.DedupKey})
}
