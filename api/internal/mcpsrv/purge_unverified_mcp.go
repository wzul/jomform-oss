// MCP parity tool: purge_unverified_accounts — reports the backlog of unverified
// accounts (never confirmed their email) older than the purge cutoff (issue #190),
// SPLIT into what the sweep can purge and what it can only leave blocked
// (card t_b8446b61). The actual deletion runs automatically on the hourly sweep;
// this tool surfaces the backlog so an admin agent can observe the purge without a
// dangerous cross-account delete.
package mcpsrv

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// purgeUnverifiedIn is the input for purge_unverified_accounts.
type purgeUnverifiedIn struct {
	// Days is the age threshold (default 7). Accounts created before
	// now-Days that are still unverified are counted.
	Days int `json:"days,omitempty" jsonschema:"Minimum age (days) of an unverified account to be counted. Default 7."`
}

// purgeUnverifiedAccounts reports how many unverified accounts are older than
// the purge cutoff, how many of those the next sweep would actually delete, and
// how many are blocked by protected data. Read-only and admin-gated: it never
// deletes anything itself — the classification runs the sweep's own delete
// inside a transaction that is always rolled back.
func (s *Server) purgeUnverifiedAccounts(
	ctx context.Context, _ *mcp.CallToolRequest, in purgeUnverifiedIn,
) (*mcp.CallToolResult, map[string]any, error) {
	// Issue #645: this tool only REPORTS a count (a read), so it gates on the
	// ROLE axis only. requireAdminRole now also demands the write scope, which
	// would refuse a readonly key on a read — the item-4 defect one layer down.
	// The description says "Admin sahaja; read-only — tidak memadam apa-apa", and
	// the handler only calls CountUnverifiedOlderThan; the role gate is intact.
	if err := s.requireAdminRoleForRead(ctx); err != nil {
		return nil, nil, err
	}
	if s.deps.Users == nil {
		return nil, nil, errors.New("user store not configured")
	}
	days := in.Days
	if days <= 0 {
		days = 7
	}
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	n, err := s.deps.Users.CountUnverifiedOlderThan(ctx, cutoff)
	if err != nil {
		return nil, nil, err
	}
	// THE BREAKDOWN, not just the bare count (card t_b8446b61). A candidate whose
	// account still holds sales or subscriptions is skipped as Blocked and can
	// NEVER be purged — on production the one older-than-cutoff unverified row
	// owned an account with 36 sales, and `count: 1` read as "the sweep will clear
	// this" when the truth was that this row is exactly what wedged the sweep.
	// The classification is the store's own verdict (the sweep's delete, run in a
	// transaction that is rolled back), so this read commits nothing.
	//
	// If the store cannot classify, the tool still answers — it reports the count
	// it has and SAYS the breakdown is unavailable, rather than failing a read or,
	// worse, presenting a classification it did not get. `blocked: null` is that
	// statement, and the note says so.
	var blocked any
	note := "Unverified accounts are deleted automatically by the sweep; this tool only reports counts."
	if cls, cerr := s.deps.Users.CountUnverifiedOlderThanClassified(ctx, cutoff); cerr == nil {
		blocked = cls.Blocked
		purgeable := cls.Deleted
		note = fmt.Sprintf(
			"%d can be deleted by the sweep; %d candidates BLOCKED (accounts still hold sales or "+
				"subscriptions) and will NOT be deleted — this is a read only, nothing is deleted.",
			purgeable, cls.Blocked)
	} else {
		blocked = nil
		note = "Unverified accounts are deleted automatically by the sweep; this tool only reports " +
			"counts. The BLOCKED breakdown is not available (classification failed)."
	}
	return nil, map[string]any{
		"unverified_older_than_days": days,
		"count":                      n,
		"blocked":                    blocked,
		"note":                       note,
	}, nil
}
