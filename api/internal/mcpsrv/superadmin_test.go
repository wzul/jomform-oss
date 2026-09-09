package mcpsrv

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestRequireSuperAdminFailsClosed verifies the super-admin gate (issue #385)
// rejects callers when no allowlist is configured (Deps.SuperAdmin nil) and
// when the caller is not a super-admin. This is the security-critical path:
// a missing SUPER_ADMIN_EMAILS env must never grant platform-wide access.
func TestRequireSuperAdminFailsClosed(t *testing.T) {
	srv := &Server{
		mcp:  mcp.NewServer(&mcp.Implementation{Name: "jomform", Version: "test"}, nil),
		deps: &Deps{},
	}

	// No allowlist configured → never a super-admin.
	if srv.isSuperAdmin(context.Background()) {
		t.Fatal("isSuperAdmin should be false when Deps.SuperAdmin is nil")
	}
	if err := srv.requireSuperAdmin(context.Background()); err == nil {
		t.Fatal("requireSuperAdmin should error when no allowlist is configured")
	}

	// Allowlist configured but no authenticated identity → rejected.
	srv.deps.SuperAdmin = func(email string) bool { return email == "owner@example.com" }
	if err := srv.requireSuperAdmin(context.Background()); err == nil {
		t.Fatal("requireSuperAdmin should error with no authenticated identity")
	}
}

// TestAuditSuperAdminActionNilSafe verifies the audit helper (issue #385)
// never panics or breaks when the Events store is nil (dev mode).
func TestAuditSuperAdminActionNilSafe(t *testing.T) {
	srv := &Server{
		mcp:  mcp.NewServer(&mcp.Implementation{Name: "jomform", Version: "test"}, nil),
		deps: &Deps{},
	}
	// Must not panic with a nil Events store.
	srv.auditSuperAdminAction(context.Background(), "test.action", nil)
}
