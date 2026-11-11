package dbstore

// Dormancy report (card t_94e5cca3, phase 1 of the approved three-phase
// dormancy policy).
//
// WHAT THIS IS. One read-only statement that answers, for every account, "how
// long has this account been quiet, and is it safe to treat it as dormant?".
// It stores nothing: there is no dormancy table, no flag, no queue row. Phase 1
// of the policy is a report and a nudge; phases 2 (unpublish) and 3 (delete) are
// separate cards and must NOT be able to act on anything written here.
//
// WHY IT IS NOT KEYED ON events.user_id. Measured on production 2026-11-10:
// 761 of 3,151 events (24%) carry a NULL user_id — system and API-key actions.
// A rule keyed on the *user* would call an account dormant while its forms were
// still being read and submitted to. The activity signal is therefore the
// LATEST of four ACCOUNT-scoped facts:
//
//	events.created_at     WHERE account_id = …
//	forms.updated_at      (live forms only; deleting one writes an event anyway)
//	sales.created_at
//	submissions.created_at
//	subscriptions.created_at  (a live subscription is activity by definition —
//	                           the hourly charge runner is working for it)
//
// Each is aggregated ONCE, grouped by account_id, in its own CTE, and then
// joined. That is the "no N+1" requirement: the number of table scans is fixed
// at five, independent of how many accounts the report returns, and `sales` is
// never read per account row. dormancy_test.go pins both halves (one statement
// issued, one scan node over `sales`) and measures the runtime on a seeded
// 10,000-account fixture.
//
// SUBSCRIPTIONS ARE READ, NOT EXCLUDED, HERE. An account with a subscription is
// excluded from candidacy in the policy layer (dormancy.Build), because the card
// says so explicitly; the subscription is also folded into last_activity because
// a monthly charge is real activity and an account being billed is not dormant.

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DormancyReportOptions bounds one report run.
type DormancyReportOptions struct {
	// Limit is the HARD row cap on the single statement (see
	// DormancyReportLimitMax). The report is ordered most-dormant-first, so a
	// truncated report keeps the rows that matter.
	Limit int
	// Now is the reference instant the 90-day window and every dormancy age are
	// measured from. Zero means time.Now(). Tests pass an explicit instant so a
	// fixture does not drift with the wall clock.
	Now time.Time
	// SubmissionsWindowDays is the "recent buyer activity" window the policy
	// excludes on (90 days per the card). Zero means 90.
	SubmissionsWindowDays int
}

// Report bounds. The cap is hard (the SQL is `LIMIT $n`): a report that silently
// grew to the whole table would be a different feature, and the admin page
// renders every row it receives.
const (
	DormancyReportLimitDefault = 200
	DormancyReportLimitMax     = 500
)

// DormancyRow is one account's dormancy facts. It carries FACTS only — the
// bucket, the exclusion verdict and the reason strings are policy and live in
// internal/dormancy, so this store never decides what "dormant" means.
type DormancyRow struct {
	AccountID string
	// WorkspaceSlug is "" for a legacy workspace-less account and for an
	// account whose workspace is in the trash (see WorkspaceTrashed).
	WorkspaceSlug string
	// WorkspaceTrashed is true when the account's workspace is in the bin. The
	// policy excludes such an account: its forms are not served, so "your form
	// is still live" would be a lie.
	WorkspaceTrashed bool

	OwnerEmail   string
	OwnerUserID  string
	OwnerLang    string
	OwnerIsAdmin bool

	AccountCreatedAt time.Time
	// LastActivity is the latest account-scoped signal, floored at the account's
	// own creation time so it is never NULL and never "epoch".
	LastActivity time.Time

	FormsPublished    int64
	Submissions90d    int64
	SalesEver         int64
	SubscriptionsEver int64

	// LastNudgeAt is when this account was last sent the "your form is still
	// live" nudge (NULL = never). It is read from the events log rather than a
	// new table: the nudge is an audit-worthy action and the log already exists.
	LastNudgeAt *time.Time
}

// DormancyNudgeEvent is the event name the nudge job records and the report
// reads back. It is declared here because the query and the writer must agree on
// the string; dormancy.NudgeEventName aliases it.
//
// IMPORTANT: this event name is EXCLUDED from the activity measure (the `ev`
// CTE filters `event_name NOT LIKE 'dormancy.%'`). Without that filter, sending
// the nudge would reset the account's own last_activity to "today", the account
// would drop out of every bucket, and it could never be nudged again — the job
// would quietly make its own subject disappear.
const DormancyNudgeEvent = "dormancy.nudge_sent"

// DormancyStore serves the report. It owns no table of its own.
type DormancyStore struct {
	pool *pgxpool.Pool
	// q is the seam a test uses to count statements. It is the pool in
	// production.
	q interface {
		Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	}
}

// NewDormancyStore builds the store over the API pool.
func NewDormancyStore(pool *pgxpool.Pool) *DormancyStore {
	return &DormancyStore{pool: pool, q: pool}
}

// dormancyReportSQL is the whole report: ONE statement, five fixed aggregate
// scans, a hard LIMIT.
//
// $1 = cutoff for the recent-buyer-activity window, $2 = the row cap.
//
// The `ev` CTE deliberately drops `dormancy.*` events: those are this job's own
// audit rows, and counting them as merchant activity would let the nudge erase
// its own subject (see DormancyNudgeEvent).
const dormancyReportSQL = `
WITH ev AS (
    SELECT account_id,
           MAX(created_at) FILTER (WHERE event_name NOT LIKE 'dormancy.%') AS last_event_at,
           MAX(created_at) FILTER (WHERE event_name = '` + DormancyNudgeEvent + `') AS last_nudge_at
      FROM events
     GROUP BY account_id
),
fm AS (
    SELECT account_id,
           MAX(updated_at) AS last_updated_at,
           COUNT(*) FILTER (WHERE status = 'published') AS forms_published
      FROM forms
     WHERE deleted_at IS NULL AND NOT is_template
     GROUP BY account_id
),
sl AS (
    SELECT account_id,
           MAX(created_at) AS last_sale_at,
           COUNT(*) AS sales_ever
      FROM sales
     GROUP BY account_id
),
sb AS (
    SELECT account_id,
           MAX(created_at) AS last_submission_at,
           COUNT(*) FILTER (WHERE created_at >= $1) AS submissions_window
      FROM submissions
     GROUP BY account_id
),
su AS (
    SELECT account_id,
           MAX(created_at) AS last_subscription_at,
           COUNT(*) AS subscriptions_ever
      FROM subscriptions
     GROUP BY account_id
)
SELECT a.id::text,
       COALESCE(w.slug, ''),
       (w.deleted_at IS NOT NULL),
       u.email::text,
       u.id::text,
       COALESCE(u.lang, ''),
       u.is_admin,
       a.created_at,
       GREATEST(COALESCE(ev.last_event_at, 'epoch'::timestamptz),
                COALESCE(fm.last_updated_at, 'epoch'::timestamptz),
                COALESCE(sl.last_sale_at, 'epoch'::timestamptz),
                COALESCE(sb.last_submission_at, 'epoch'::timestamptz),
                COALESCE(su.last_subscription_at, 'epoch'::timestamptz),
                a.created_at) AS last_activity,
       COALESCE(fm.forms_published, 0),
       COALESCE(sb.submissions_window, 0),
       COALESCE(sl.sales_ever, 0),
       COALESCE(su.subscriptions_ever, 0),
       ev.last_nudge_at
  FROM accounts a
  JOIN users u ON u.id = a.owner_user_id
  LEFT JOIN workspaces w ON w.id = a.workspace_id
  LEFT JOIN ev ON ev.account_id = a.id
  LEFT JOIN fm ON fm.account_id = a.id
  LEFT JOIN sl ON sl.account_id = a.id
  LEFT JOIN sb ON sb.account_id = a.id
  LEFT JOIN su ON su.account_id = a.id
 ORDER BY last_activity ASC, a.id ASC
 LIMIT $2`

// DormancyReport returns the per-account dormancy facts, most dormant first.
//
// It issues exactly ONE statement (pinned by the statement-counting test), so a
// caller cannot accidentally turn the report into a per-row query by adding a
// loop here.
func (s *DormancyStore) DormancyReport(ctx context.Context, opts DormancyReportOptions) ([]DormancyRow, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = DormancyReportLimitDefault
	}
	if limit > DormancyReportLimitMax {
		limit = DormancyReportLimitMax
	}
	windowDays := opts.SubmissionsWindowDays
	if windowDays <= 0 {
		windowDays = 90
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	cutoff := now.Add(-time.Duration(windowDays) * 24 * time.Hour)

	rows, err := s.q.Query(ctx, dormancyReportSQL, cutoff, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]DormancyRow, 0, 32)
	for rows.Next() {
		var r DormancyRow
		var lastNudge *time.Time
		if err := rows.Scan(
			&r.AccountID,
			&r.WorkspaceSlug,
			&r.WorkspaceTrashed,
			&r.OwnerEmail,
			&r.OwnerUserID,
			&r.OwnerLang,
			&r.OwnerIsAdmin,
			&r.AccountCreatedAt,
			&r.LastActivity,
			&r.FormsPublished,
			&r.Submissions90d,
			&r.SalesEver,
			&r.SubscriptionsEver,
			&lastNudge,
		); err != nil {
			return nil, err
		}
		r.LastNudgeAt = lastNudge
		out = append(out, r)
	}
	return out, rows.Err()
}

// RecordDormancyNudge appends the audit row that both documents the send and
// rate-limits the next one (never twice within 90 days, read by the `nudge` CTE
// above). A nil Events store is a no-op, matching Record's best-effort contract.
func (s *DormancyStore) RecordDormancyNudge(ctx context.Context, accountID, userID, bucket string, email string) error {
	if s.pool == nil {
		return nil
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO events (account_id, user_id, event_name, properties)
		VALUES ($1, NULLIF($2,'')::uuid, $3, $4)`,
		accountID, userID, DormancyNudgeEvent,
		map[string]any{"bucket": bucket, "to": email})
	return err
}
