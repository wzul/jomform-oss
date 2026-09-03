package dbstore

import (
	"context"
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
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, form_id, account_id, data, is_spam, spam_reason, created_at
		 FROM submissions
		 WHERE account_id = ANY($1) AND form_id = $2
		 ORDER BY created_at DESC LIMIT $3 OFFSET $4`,
		accountIDs, formID, limit, offset)
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
	return total, err
}

func scanSubmission(row pgx.Row) (*Submission, error) {
	sub := &Submission{}
	err := row.Scan(&sub.ID, &sub.FormID, &sub.AccountID, &sub.Data, &sub.IsSpam, &sub.SpamReason, &sub.CreatedAt)
	return sub, err
}
