// Super-admin REST endpoints — platform-wide usage monitoring (issue #387).
//
// GET /v1/admin/overview returns platform-wide usage metrics (spaces, forms,
// products, sales, storage, email, spam, recent signups, active spaces) so
// the platform owner can visually monitor usage across all spaces once
// JomForm is public. It is gated on the super-admin role (#385): every
// handler calls requireSuperAdmin first and audits the access afterwards.
// It is read-only — no destructive actions exist on the admin surface.
package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/wzul/jomform/api/internal/dbstore"
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
	Totals       dbstore.PlatformTotals     `json:"totals"`
	Growth24h    dbstore.GrowthCounts       `json:"growth_24h"`
	Growth7d     dbstore.GrowthCounts       `json:"growth_7d"`
	Email        dbstore.EmailDeliverySummary `json:"email"`
	Spam         dbstore.SpamQueueSize      `json:"spam"`
	RecentSignups []dbstore.RecentSignup    `json:"recent_signups"`
	ActiveSpaces  []dbstore.ActiveSpace     `json:"active_spaces"`
	Storage      []dbstore.MediaUsageBySpace `json:"storage"`
	QuotaBytes   int64                      `json:"quota_bytes"`
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
	out := adminOverview{QuotaBytes: 1 << 30} // 1GB per-space quota (#384)

	if h.Spaces != nil {
		if t, err := h.Spaces.PlatformTotals(ctx); err == nil {
			out.Totals = t
		}
		now := time.Now()
		if g, err := h.Spaces.GrowthSince(ctx, now.Add(-24*time.Hour)); err == nil {
			out.Growth24h = g
		}
		if g, err := h.Spaces.GrowthSince(ctx, now.Add(-7*24*time.Hour)); err == nil {
			out.Growth7d = g
		}
		if a, err := h.Spaces.ActiveSpaces(ctx, 7); err == nil {
			out.ActiveSpaces = a
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
		if s, err := h.Media.UsageBySpace(ctx); err == nil {
			out.Storage = s
		}
	}

	respond(w, http.StatusOK, out)
}
