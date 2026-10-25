package dbstore

// QA test-account purge (card t_6ae7f586).
//
// WHY THIS EXISTS. Twenty test accounts — created by a QA lane that registered a
// fresh account on PRODUCTION for every card that verified signup/onboarding —
// accumulated as 56% of all users, and NOTHING could remove them: every one was
// `verified`, so `purge_unverified_accounts` (which deletes only verified=false)
// was blind to all of them, and no admin route or MCP tool could delete a user
// at all. Removing them needed hand-written SQL with a pg_dump in front.
//
// The account is marked at INSERT time by the QA signup channel (users.is_test,
// migration 000078) rather than remembered by the card that created it: a card
// that fails, is killed or is superseded must still leave an account this purge
// can find. That is the whole design, and it is why "the lane deletes it at the
// end" was rejected — it fails on exactly the common path.
//
// THE GUARDS, all enforced below the tool layer:
//   - only `is_test = true` rows are ever candidates; an unmarked account with
//     the same email shape is never looked at (the card's lookalike test);
//   - a protected account (ProtectedAccountEmails) is refused even if marked;
//   - an account that still holds sales or subscriptions is BLOCKED, reported by
//     name, and left exactly as it was (the same RESTRICT verdict the unverified
//     sweep uses, from the same per-user transaction);
//   - a dry run runs the REAL delete in a transaction that is always rolled
//     back, so the report cannot disagree with what the delete would do;
//   - idempotent: a second run finds nothing and returns an empty report.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// TestAccountRef names one test account the purge removed (or would remove).
// The report NAMES what happened rather than reporting a bare count.
type TestAccountRef struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Verified  bool   `json:"verified"`
	CreatedAt string `json:"created_at"`
}

// TestAccountBlocked names one marked account the purge REFUSED, and why.
type TestAccountBlocked struct {
	ID     string `json:"id"`
	Email  string `json:"email"`
	Reason string `json:"reason"`
}

// Block reasons. "sales" and "subscriptions" are the two the card names; any
// other RESTRICT/NO ACTION reference is reported honestly as fk_restricted
// rather than being folded into one of them.
const (
	BlockReasonSales         = "sales"
	BlockReasonSubscriptions = "subscriptions"
	BlockReasonFKRestricted  = "fk_restricted"
)

// TestPurgeOptions selects what one purge run does.
type TestPurgeOptions struct {
	// DryRun reports exactly what would be removed and commits nothing.
	DryRun bool
	// Email, when set, scopes the run to ONE account. Asking for an account that
	// is not marked as a test account is REFUSED (ErrNotATestAccount) rather
	// than silently returning an empty report.
	Email string
}

// TestPurgeResult is the report: what was removed, what was refused, and how
// many marked accounts were considered.
type TestPurgeResult struct {
	DryRun  bool
	Removed []TestAccountRef
	Blocked []TestAccountBlocked
	Scanned int
}

// testAccountRow is one candidate as the report needs to name it, plus the two
// predicates used to say WHY a blocked candidate was blocked.
type testAccountRow struct {
	ID        string
	Email     string
	Verified  bool
	CreatedAt time.Time
	HasSales  bool
	HasSubs   bool
}

// PurgeTestAccounts removes every account carrying the is_test marker, with the
// guards described at the top of this file.
//
// No age cutoff and no creator filter: an account is removable because it is
// MARKED, however old it is and whoever created it. That is what makes the purge
// work for a card that died mid-verification.
func (s *UserStore) PurgeTestAccounts(ctx context.Context, opt TestPurgeOptions) (TestPurgeResult, error) {
	res := TestPurgeResult{DryRun: opt.DryRun, Removed: []TestAccountRef{}, Blocked: []TestAccountBlocked{}}

	rows, err := s.testAccountCandidates(ctx, opt.Email)
	if err != nil {
		return res, err
	}
	res.Scanned = len(rows)

	for _, row := range rows {
		// The DRY RUN is the sweep's own delete inside a transaction that is
		// always rolled back, so "would remove" and "removed" cannot diverge.
		deleted, perr := s.purgeUserTx(ctx, row.ID, opt.DryRun, purgeRuleTestMarked)
		switch {
		case perr == nil:
			if !deleted {
				// Gone under us (a concurrent run), or no longer marked: nothing
				// was deleted and the report must not claim otherwise.
				continue
			}
			res.Removed = append(res.Removed, TestAccountRef{
				ID: row.ID, Email: row.Email, Verified: row.Verified,
				CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339),
			})
		case isBlockedPurgeErr(perr):
			res.Blocked = append(res.Blocked, TestAccountBlocked{
				ID: row.ID, Email: row.Email, Reason: blockReason(row),
			})
		default:
			// A refusal (protected / not a test account) or a real failure: the
			// caller must see it, not a partial report presented as complete.
			return res, fmt.Errorf("purge test account %s: %w", row.Email, perr)
		}
	}
	return res, nil
}

// blockReason names the obstacle a blocked candidate hit, from the same two
// account-level facts the RESTRICT guard is about.
func blockReason(row testAccountRow) string {
	switch {
	case row.HasSales:
		return BlockReasonSales
	case row.HasSubs:
		return BlockReasonSubscriptions
	default:
		return BlockReasonFKRestricted
	}
}

// testAccountCandidates returns the accounts this run may consider.
//
// With no email it is every marked account EXCEPT a protected one. With an
// email it answers about THAT account, including the refusals the card requires:
// not found is an empty result (idempotent re-run), a protected address is
// ErrProtectedAccount, and an unmarked address is ErrNotATestAccount — a purge
// asked to remove a real merchant's account must say no, not "0 removed".
func (s *UserStore) testAccountCandidates(ctx context.Context, email string) ([]testAccountRow, error) {
	const cols = `SELECT u.id, u.email, u.verified, u.created_at,
	                     EXISTS (SELECT 1 FROM accounts a JOIN sales sl ON sl.account_id = a.id
	                              WHERE a.owner_user_id = u.id),
	                     EXISTS (SELECT 1 FROM accounts a JOIN subscriptions sb ON sb.account_id = a.id
	                              WHERE a.owner_user_id = u.id)`

	if email != "" {
		var row testAccountRow
		var isTest bool
		err := s.pool.QueryRow(ctx,
			`SELECT u.id, u.email, u.verified, u.created_at, u.is_test,
			        EXISTS (SELECT 1 FROM accounts a JOIN sales sl ON sl.account_id = a.id
			                 WHERE a.owner_user_id = u.id),
			        EXISTS (SELECT 1 FROM accounts a JOIN subscriptions sb ON sb.account_id = a.id
			                 WHERE a.owner_user_id = u.id)
			   FROM users u WHERE u.email = $1`, email,
		).Scan(&row.ID, &row.Email, &row.Verified, &row.CreatedAt, &isTest, &row.HasSales, &row.HasSubs)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, nil
			}
			return nil, err
		}
		if s.isProtectedEmail(row.Email) {
			return nil, ErrProtectedAccount
		}
		if !isTest {
			return nil, ErrNotATestAccount
		}
		return []testAccountRow{row}, nil
	}

	rows, err := s.pool.Query(ctx,
		cols+`
		  FROM users u
		 WHERE u.is_test
		   AND lower(u.email::text) <> ALL($1::text[])
		 ORDER BY u.created_at, u.id`, s.protectedLower())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []testAccountRow
	for rows.Next() {
		var row testAccountRow
		if err := rows.Scan(&row.ID, &row.Email, &row.Verified, &row.CreatedAt, &row.HasSales, &row.HasSubs); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
