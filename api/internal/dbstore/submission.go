package dbstore

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Submission models one response-only form submission (issue #171). A
// response-mode form has no products and no payment — the buyer's field
// answers are stored in Data as a JSONB map {block_id: value}.
//
// Issue #245: IsSpam + SpamReason record the background AI spam-judge
// result. A submission is accepted immediately (is_spam=false), then judged
// in the background; a spam verdict flips IsSpam=true with a reason. Spam
// entries are auto-deleted after 30 days by a retention sweep; un-spammed
// entries are never auto-deleted.
type Submission struct {
	ID         string
	FormID     string
	AccountID  string
	Data       map[string]any
	IsSpam     bool
	SpamReason *string
	CreatedAt  time.Time
}

// SubmissionStore wraps pgx for the submissions table.
type SubmissionStore struct{ pool *pgxpool.Pool }

// submissionSelect is the column list every submissions read scans. It is a
// constant for the same reason saleSelect is: the migration/scan-drift guard in
// this package asserts that a read's column count matches the struct it scans,
// and a hand-repeated SELECT (which this read used to carry) is exactly how
// such a list drifts.
const submissionSelect = `SELECT id, form_id, account_id, data, is_spam, spam_reason, created_at
	FROM submissions`

// NewSubmissionStore builds a SubmissionStore.
func NewSubmissionStore(pool *pgxpool.Pool) *SubmissionStore { return &SubmissionStore{pool: pool} }

// Create inserts a submission and returns its id + created_at.
func (s *SubmissionStore) Create(ctx context.Context, sub *Submission) error {
	if sub.Data == nil {
		sub.Data = map[string]any{}
	}
	return s.pool.QueryRow(ctx,
		`INSERT INTO submissions (form_id, account_id, data)
		 VALUES ($1, $2, $3)
		 RETURNING id, created_at`,
		sub.FormID, sub.AccountID, sub.Data,
	).Scan(&sub.ID, &sub.CreatedAt)
}

// ByID fetches a submission by its uuid string.
func (s *SubmissionStore) ByID(ctx context.Context, id string) (*Submission, error) {
	return scanSubmission(s.pool.QueryRow(ctx,
		`SELECT id, form_id, account_id, data, is_spam, spam_reason, created_at FROM submissions WHERE id = $1`, id))
}

// ListByAccountsFormPage returns one page of submissions for a single form
// across a set of accounts, newest first, plus the total row count for the
// same filter (for paging). Ownership is enforced inside the query.
func (s *SubmissionStore) ListByAccountsFormPage(ctx context.Context, accountIDs []string, formID string, page, limit int) ([]*Submission, int64, error) {
	if len(accountIDs) == 0 || formID == "" {
		return []*Submission{}, 0, nil
	}
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	offset := (page - 1) * limit
	var total int64
	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM submissions WHERE account_id = ANY($1) AND form_id = $2`,
		accountIDs, formID).Scan(&total); err != nil {
		if isNoSuchRow(err) {
			// The form id cannot name a row at all (SQLSTATE 22P02), which is
			// the same answer a well-formed id naming no form already gets on
			// this route: no submissions. It must not surface as a driver
			// error and become a 500 on a caller-supplied bad id.
			return []*Submission{}, 0, nil
		}
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, form_id, account_id, data, is_spam, spam_reason, created_at
		 FROM submissions
		 WHERE account_id = ANY($1) AND form_id = $2
		 ORDER BY created_at DESC LIMIT $3 OFFSET $4`,
		accountIDs, formID, limit, offset)
	if isNoSuchRow(err) {
		return []*Submission{}, total, nil
	}
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []*Submission{}
	for rows.Next() {
		sub, err := scanSubmission(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, sub)
	}
	return out, total, rows.Err()
}

// SubmissionFilter is the optional filter set for the per-form responses view
// (the Sales page's response-mode branch). A submission has no status column,
// so only the date range and the free-text search over the recorded field
// values apply; the form + account scope is always enforced by the caller.
// Date fields are YYYY-MM-DD inclusive day bounds, matching SaleFilter.
//
// FormIDs carries a SET of response-mode forms: the merged read (records.go)
// has to match the submissions of every response form in the caller's scope,
// since an unfiltered read covers all of them. FormID remains for the
// single-form reads (a per-form deep link, an MCP call with form_id).
type SubmissionFilter struct {
	FormID   string
	FormIDs  []string // when set, takes precedence over FormID
	DateFrom string   // YYYY-MM-DD, inclusive
	DateTo   string   // YYYY-MM-DD, inclusive
	Q        string   // matches any recorded field value (case-insensitive)
}

// formClause builds the form predicate + its argument for a submissions read:
// one form id, or a set of them. An empty filter matches no rows (the caller
// must always say which form(s) it is reading) — never the whole table.
func (f SubmissionFilter) formClause(col string, start int) (string, []any) {
	if len(f.FormIDs) > 0 {
		return col + " = ANY($" + strconv.Itoa(start) + ")", []any{f.FormIDs}
	}
	if f.FormID != "" {
		return col + " = $" + strconv.Itoa(start), []any{f.FormID}
	}
	return "false", nil
}

// submissionWhere builds the shared WHERE clause + args for a submissions read.
// $start is the position the account-id array occupies. One definition, used by
// the per-form page read and by the merged record read (records.go).
func submissionWhere(accountIDs []string, f SubmissionFilter, start int) (string, []any) {
	where := []string{"account_id = ANY($" + strconv.Itoa(start) + ")"}
	args := []any{accountIDs}
	next := func() int { return start + len(args) }

	fc, fa := f.formClause("form_id", next())
	where = append(where, fc)
	args = append(args, fa...)

	if f.DateFrom != "" {
		args = append(args, f.DateFrom+" 00:00:00")
		where = append(where, "created_at >= $"+strconv.Itoa(next()-1))
	}
	if f.DateTo != "" {
		args = append(args, f.DateTo+" 23:59:59")
		where = append(where, "created_at <= $"+strconv.Itoa(next()-1))
	}
	if f.Q != "" {
		args = append(args, "%"+f.Q+"%")
		// Search the recorded answers as text: the merchant searches for a
		// value they saw in a response, not for a column name.
		where = append(where, "data::text ILIKE $"+strconv.Itoa(next()-1))
	}
	return strings.Join(where, " AND "), args
}

// ListFilteredPage returns one page of submissions for a form (or a set of
// forms) across a set of accounts, honouring the optional date range +
// free-text filter, newest first, plus the total row count for the same filter
// (for paging). Ownership is enforced inside the query.
//
// The row shape is the same as ListByAccountsFormPage (one submissions row per
// entry); only the WHERE clause differs, so the scan stays column-aligned with
// scanSubmission.
func (s *SubmissionStore) ListFilteredPage(ctx context.Context, accountIDs []string, f SubmissionFilter, page, limit int) ([]*Submission, int64, error) {
	if len(accountIDs) == 0 || (f.FormID == "" && len(f.FormIDs) == 0) {
		return []*Submission{}, 0, nil
	}
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	clause, args := submissionWhere(accountIDs, f, 1)
	offset := (page - 1) * limit

	var total int64
	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM submissions WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	fullArgs := append(append([]any{}, args...), limit, offset)
	rows, err := s.pool.Query(ctx,
		submissionSelect+` WHERE `+clause+
			` ORDER BY created_at DESC, id DESC LIMIT $`+strconv.Itoa(len(args)+1)+
			` OFFSET $`+strconv.Itoa(len(args)+2),
		fullArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []*Submission{}
	for rows.Next() {
		sub, err := scanSubmission(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, sub)
	}
	return out, total, rows.Err()
}

// ByIDs hydrates a set of submissions by id, scoped to an account set. Used by
// the merged record read (records.go) so the submissions row shape and its scan
// stay defined once, in this file.
func (s *SubmissionStore) ByIDs(ctx context.Context, accountIDs []string, ids []string) ([]*Submission, error) {
	if len(accountIDs) == 0 || len(ids) == 0 {
		return []*Submission{}, nil
	}
	rows, err := s.pool.Query(ctx,
		submissionSelect+` WHERE account_id = ANY($1) AND id = ANY($2)`, accountIDs, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Submission{}
	for rows.Next() {
		sub, err := scanSubmission(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

// ListSpamByAccountsPage returns one page of spam-flagged submissions across
// a set of accounts, newest first, plus the total count (issue #245 spam
// review queue). Ownership is enforced inside the query.
func (s *SubmissionStore) ListSpamByAccountsPage(ctx context.Context, accountIDs []string, page, limit int) ([]*Submission, int64, error) {
	if len(accountIDs) == 0 {
		return []*Submission{}, 0, nil
	}
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	offset := (page - 1) * limit
	var total int64
	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM submissions WHERE account_id = ANY($1) AND is_spam = true`,
		accountIDs).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, form_id, account_id, data, is_spam, spam_reason, created_at
		 FROM submissions
		 WHERE account_id = ANY($1) AND is_spam = true
		 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		accountIDs, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []*Submission{}
	for rows.Next() {
		sub, err := scanSubmission(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, sub)
	}
	return out, total, rows.Err()
}

// MarkSpam flags a submission as spam with a reason (issue #245). The AI
// judge calls this on a spam verdict.
func (s *SubmissionStore) MarkSpam(ctx context.Context, id, reason string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE submissions SET is_spam = true, spam_reason = $2 WHERE id = $1`,
		id, reason)
	return err
}

// UnSpam clears the spam flag + reason on a submission (issue #245). Called
// by the merchant review queue (false positive) or when a spam-flagged entry
// turns out to be legit. Un-spammed entries are never auto-deleted.
func (s *SubmissionStore) UnSpam(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE submissions SET is_spam = false, spam_reason = NULL WHERE id = $1`,
		id)
	return err
}

// DeleteSpamOlderThan permanently deletes spam-flagged submissions older than
// cutoff (issue #245 30-day retention sweep). Un-spammed entries are never
// touched. Returns the number of rows deleted.
func (s *SubmissionStore) DeleteSpamOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM submissions WHERE is_spam = true AND created_at < $1`, cutoff)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// CountByAccountsForm returns the number of submissions for a single form
// across a set of accounts (account-scoped).
func (s *SubmissionStore) CountByAccountsForm(ctx context.Context, accountIDs []string, formID string) (int64, error) {
	if len(accountIDs) == 0 || formID == "" {
		return 0, nil
	}
	var total int64
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM submissions WHERE account_id = ANY($1) AND form_id = $2`,
		accountIDs, formID).Scan(&total)
	if isNoSuchRow(err) {
		// Same as ListByAccountsFormPage: an id that cannot be a uuid counts
		// zero submissions rather than raising a 500 (see isNoSuchRow).
		return 0, nil
	}
	return total, err
}

func scanSubmission(row pgx.Row) (*Submission, error) {
	sub := &Submission{}
	err := row.Scan(&sub.ID, &sub.FormID, &sub.AccountID, &sub.Data, &sub.IsSpam, &sub.SpamReason, &sub.CreatedAt)
	return sub, err
}
