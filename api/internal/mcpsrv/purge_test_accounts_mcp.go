// MCP parity tool: purge_test_accounts — remove the QA TEST accounts a lane
// created through the marking channel (card t_6ae7f586).
//
// WHY THIS TOOL EXISTS. Twenty verified test accounts accumulated on production
// because every card that verified the signup/onboarding flow registered a fresh
// account and nothing could remove them: `purge_unverified_accounts` deletes only
// verified=false rows, and no admin route or MCP tool could delete a user at all.
// A human removed them in SQL, with a pg_dump and a dry-run transaction in front.
//
// The accounts are found by a PERSISTED MARKER (users.is_test, set at INSERT time
// through the QA signup channel), never by the shape of the email address — so
// the purge works whoever created the account, whether that card finished or
// died, and however old the account is.
//
// SAFETY, in the store and not only in the description:
//   - only is_test = true rows are candidates; an unmarked account with the same
//     email shape is never touched;
//   - a protected account (the ONE reused production test login) is refused;
//   - an account still holding sales or subscriptions is REFUSED by name, not
//     force-deleted (the same RESTRICT verdict the unverified sweep uses);
//   - a dry run is the DEFAULT (`confirm` absent) and runs the real delete in a
//     rolled-back transaction, so the report cannot disagree with what would
//     happen;
//   - idempotent: a second run finds nothing and says so.
package mcpsrv

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// purgeTestAccountsIn is the input for purge_test_accounts.
type purgeTestAccountsIn struct {
	// Confirm is the destructive switch. Absent/false = DRY RUN (the default):
	// report exactly what would be removed and commit nothing. The tool deletes
	// nothing unless the caller explicitly says confirm=true.
	Confirm bool `json:"confirm,omitempty" jsonschema:"Default false = DRY RUN: report what WILL be deleted, nothing is deleted. Set true to actually delete."`
	// Email scopes the run to ONE account. An account that is not marked as a
	// test account is REFUSED, not silently skipped.
	Email string `json:"email,omitempty" jsonschema:"Optional — limit to ONE account (email). An account not marked is_test will be REJECTED."`
}

// purgeTestAccountOut names one account the run removed (or, in a dry run,
// would remove).
type purgeTestAccountOut struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Verified  bool   `json:"verified"`
	CreatedAt string `json:"created_at"`
}

// purgeTestBlockedOut names one marked account the run refused, and why.
type purgeTestBlockedOut struct {
	ID     string `json:"id"`
	Email  string `json:"email"`
	Reason string `json:"reason"`
}

// purgeTestAccountsOut is the report: NAMES what was removed, what was refused
// and the counts — never a bare "done".
type purgeTestAccountsOut struct {
	DryRun        bool                  `json:"dry_run"`
	RemovedEmails []string              `json:"removed_emails"`
	Removed       []purgeTestAccountOut `json:"removed"`
	RemovedCount  int                   `json:"removed_count"`
	Blocked       []purgeTestBlockedOut `json:"blocked"`
	BlockedCount  int                   `json:"blocked_count"`
	Scanned       int                   `json:"scanned"`
	Note          string                `json:"note"`
}

// purgeTestAccounts removes accounts carrying the is_test marker. Super-admin
// only (audited); the destructive form additionally needs the caller's write
// scope.
func (s *Server) purgeTestAccounts(
	ctx context.Context, _ *mcp.CallToolRequest, in purgeTestAccountsIn,
) (*mcp.CallToolResult, purgeTestAccountsOut, error) {
	out := purgeTestAccountsOut{DryRun: !in.Confirm}
	if err := s.requireSuperAdmin(ctx); err != nil {
		return nil, out, err
	}
	// Issue #645: super-admin is the ROLE axis; the key's write scope is a
	// separate axis. A dry run reads (it deletes nothing), the destructive form
	// must hold the write scope.
	if in.Confirm {
		if err := s.requireWriteScope(ctx); err != nil {
			return nil, out, err
		}
	}
	if s.deps.Users == nil {
		return nil, out, errors.New("user store not configured")
	}

	res, err := s.deps.Users.PurgeTestAccounts(ctx, dbstore.TestPurgeOptions{
		DryRun: !in.Confirm,
		Email:  in.Email,
	})
	if err != nil {
		switch {
		case errors.Is(err, dbstore.ErrNotATestAccount):
			return nil, out, fmt.Errorf("%w — this tool only deletes accounts flagged is_test; "+
				"real customer accounts are never touched", err)
		case errors.Is(err, dbstore.ErrProtectedAccount):
			return nil, out, fmt.Errorf("%w — this account is protected and must stay alive", err)
		}
		return nil, out, err
	}

	out.RemovedEmails = make([]string, 0, len(res.Removed))
	out.Removed = make([]purgeTestAccountOut, 0, len(res.Removed))
	for _, a := range res.Removed {
		out.RemovedEmails = append(out.RemovedEmails, a.Email)
		out.Removed = append(out.Removed, purgeTestAccountOut{
			ID: a.ID, Email: a.Email, Verified: a.Verified, CreatedAt: a.CreatedAt,
		})
	}
	out.RemovedCount = len(out.Removed)
	out.Blocked = make([]purgeTestBlockedOut, 0, len(res.Blocked))
	for _, b := range res.Blocked {
		out.Blocked = append(out.Blocked, purgeTestBlockedOut{ID: b.ID, Email: b.Email, Reason: b.Reason})
	}
	out.BlockedCount = len(out.Blocked)
	out.Scanned = res.Scanned

	verb := "WILL be deleted"
	if in.Confirm {
		verb = "ALREADY deleted"
	}
	out.Note = fmt.Sprintf("%d test accounts %s, %d BLOCKED (still holding sales/subscriptions or "+
		"referenced by other data — not touched), %d scanned. Protected accounts and non-test accounts "+
		"are never touched.", out.RemovedCount, verb, out.BlockedCount, out.Scanned)
	if in.Confirm && out.RemovedCount == 0 && out.BlockedCount == 0 {
		out.Note = "No test accounts flagged — nothing deleted (safe to run repeatedly)."
	}

	// Every super-admin action is audited; the report fields are part of the
	// record so the log names what was removed too.
	s.auditSuperAdminAction(ctx, "admin.purge_test_accounts", map[string]any{
		"dry_run":       !in.Confirm,
		"scanned":       out.Scanned,
		"removed_count": out.RemovedCount,
		"blocked_count": out.BlockedCount,
		"removed":       out.RemovedEmails,
		"email_filter":  in.Email,
	})
	return nil, out, nil
}
