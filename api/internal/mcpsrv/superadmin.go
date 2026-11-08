// Site super-admin RBAC foundation (issue #385).
//
// A super-admin is a user whose email is on the SUPER_ADMIN_EMAILS allowlist
// (wired from config into Deps.SuperAdmin). Super-admin unlocks platform-wide
// monitoring across all workspaces — but ONLY on explicitly gated admin tools and
// pages (#386 MCP admin tools, #387 admin dashboard), never implicitly on
// normal tools. This file provides the identification check and the audit
// helper that every super-admin action must call.
package mcpsrv

import (
	"context"
	"log/slog"
	"strings"
)

// isSuperAdmin reports whether the authenticated principal is a site
// super-admin. It resolves the caller's user email (OAuth user, or the
// account owner for API-key callers) and checks it against the allowlist.
// A nil allowlist (Deps.SuperAdmin unset) or an unresolvable user is never
// a super-admin — fail closed.
func (s *Server) isSuperAdmin(ctx context.Context) bool {
	if s.deps == nil || s.deps.SuperAdmin == nil {
		return false
	}
	email := s.userEmail(ctx)
	if email == "" {
		return false
	}
	return s.deps.SuperAdmin(email)
}

// userEmail resolves the authenticated user's email. For OAuth tokens the
// identity carries UserID; for API-key callers the account owner is resolved.
// Returns "" when the user cannot be resolved.
func (s *Server) userEmail(ctx context.Context) string {
	userID := s.userIDFor(ctx)
	if userID == "" || s.deps == nil || s.deps.Users == nil {
		return ""
	}
	u, err := s.deps.Users.ByID(ctx, userID)
	if err != nil || u == nil {
		return ""
	}
	return strings.TrimSpace(u.Email)
}

// requireSuperAdmin gates an admin-only tool/page. It returns an error when
// the caller is not a super-admin. Every gated handler MUST call this before
// doing any work, and MUST call auditSuperAdminAction afterwards.
func (s *Server) requireSuperAdmin(ctx context.Context) error {
	if !s.isSuperAdmin(ctx) {
		return errSuperAdminRequired
	}
	return nil
}

// errSuperAdminRequired is returned when a non-super-admin calls an admin tool.
var errSuperAdminRequired = &superAdminError{}

type superAdminError struct{}

func (*superAdminError) Error() string { return "super-admin is required for this action" }

// auditSuperAdminAction records a super-admin action to the audit log
// (issue #385, acceptance: every super-admin action is audited). It records
// who (user id + email), what (action), and when (created_at) as an
// `admin.super_admin_action` event. Best-effort: a nil Events store or a
// write error is logged, never fatal to the primary action.
func (s *Server) auditSuperAdminAction(ctx context.Context, action string, props map[string]any) {
	if s.deps == nil || s.deps.Events == nil {
		return
	}
	id := IdentityFromContext(ctx)
	if id == nil {
		return
	}
	// The events table requires a non-null account_id. Attribute the audit
	// row to the super-admin's own account (every user auto-provisions one
	// on signup); fall back to the identity's account when the user cannot
	// be resolved.
	accountID := id.AccountID
	userID := id.UserID
	if userID == "" {
		userID = s.userIDFor(ctx)
	}
	if accountID == "" && userID != "" && s.deps.Accounts != nil {
		if accs, err := s.deps.Accounts.ByOwner(ctx, userID); err == nil && len(accs) > 0 {
			accountID = accs[0].ID
		}
	}
	if accountID == "" {
		slog.Warn("super_admin_audit_skipped", "action", action, "reason", "no account")
		return
	}
	if props == nil {
		props = map[string]any{}
	}
	props["action"] = action
	props["email"] = s.userEmail(ctx)
	if err := s.deps.Events.Record(ctx, accountID, userID, "admin.super_admin_action", props, "", ""); err != nil {
		slog.Error("super_admin_audit", "err", err, "action", action, "account", accountID)
	}
}
