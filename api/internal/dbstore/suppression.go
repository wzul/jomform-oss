// Suppression list store (issue #56): SES bounce/complaint feedback is
// persisted here so no outbound send ever targets a suppressed address.
//
// Issue #601 (admin surface): the list was write-only from the platform's point
// of view — SES ingestion could add a row, but nothing could read, search, add
// manually or remove one. This file gains the store API that backs the
// super-admin screen, the REST endpoints and the MCP tools, and it normalises
// the free-text reasons the old ingestion path used to write into the three
// canonical codes the migration 000061 CHECK now enforces.
package dbstore

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SuppressionSource identifies where a suppression record came from.
type SuppressionSource string

const (
	SuppressionSourceBounce    SuppressionSource = "bounce"
	SuppressionSourceComplaint SuppressionSource = "complaint"
	SuppressionSourceManual    SuppressionSource = "manual"
)

// The canonical reason codes. suppressed_emails_reason_chk (migration 000061)
// constrains the column to exactly these three, so every write path must map
// its input through CanonicalReason before it reaches SQL.
const (
	SuppressionReasonHardBounce = "hard_bounce"
	SuppressionReasonComplaint  = "complaint"
	SuppressionReasonManual     = "manual"
)

// IsCanonicalReason reports whether reason is one of the three codes the
// database CHECK accepts.
func IsCanonicalReason(reason string) bool {
	switch reason {
	case SuppressionReasonHardBounce, SuppressionReasonComplaint, SuppressionReasonManual:
		return true
	}
	return false
}

// CanonicalReason maps a written reason onto the code the CHECK accepts.
//
// The SNS ingestion path (api/internal/httpapi/sesnotif.go) used to write free
// text such as "permanent bounce (Permanent)", and callers of the old Upsert
// wrote prose ("hard bounce"). Migration 000061 backfilled those rows and its
// CHECK now rejects them, so any non-canonical value is resolved by source:
// a bounce is a hard bounce, a complaint is a complaint, and anything else is a
// manual entry. The caller keeps the original text in `note`, which is what
// preserves the provenance the CHECK would otherwise erase.
func CanonicalReason(reason string, source SuppressionSource) string {
	if IsCanonicalReason(reason) {
		return reason
	}
	switch source {
	case SuppressionSourceBounce:
		return SuppressionReasonHardBounce
	case SuppressionSourceComplaint:
		return SuppressionReasonComplaint
	default:
		return SuppressionReasonManual
	}
}

// SuppressedEmail mirrors a suppressed_emails row as the admin surface shows it.
type SuppressedEmail struct {
	Email     string // stored lower-case (the column is CITEXT)
	Reason    string // hard_bounce | complaint | manual
	Source    string // bounce | complaint | manual
	Note      string // detail behind the reason; '' when none was recorded
	CreatedBy string // super-admin users.id for a manual add; '' when NULL
	// CreatedByEmail is the resolved actor's email for display. '' when the
	// actor is unknown (bounce/complaint ingestion has none, and a deleted user
	// leaves the FK NULL via ON DELETE SET NULL).
	CreatedByEmail string
	CreatedAt      time.Time
}

// SuppressionStore wraps pgx for the suppressed_emails table.
type SuppressionStore struct {
	pool *pgxpool.Pool
}

// NewSuppressionStore builds a SuppressionStore over the given pool.
func NewSuppressionStore(pool *pgxpool.Pool) *SuppressionStore {
	return &SuppressionStore{pool: pool}
}

// normaliseEmail lowercases and trims an address. Every method applies it
// before touching SQL, so a lookup is case-insensitive in the app layer as well
// as in the CITEXT column.
func normaliseEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// Add records (or refreshes) a suppression and reports whether a new row was
// created. It is the ONE write path: the SNS ingestion path, the super-admin
// REST endpoint and the admin_add_suppression MCP tool all go through it.
//
//   - reason is mapped to a canonical code via CanonicalReason, so a caller
//     passing prose cannot violate the CHECK (see Upsert).
//   - note is the human-readable detail (bounce sub-type, soft-bounce tally, or
//     a free-text note a super-admin typed).
//   - createdBy is the super-admin's users.id for a manual add; pass "" for
//     ingestion, which has no actor. On a refresh an empty actor preserves the
//     original one rather than clearing it, so a later bounce event cannot
//     erase the record of who added the address by hand.
//
// Idempotent on the CITEXT primary key: a repeat for the same address refreshes
// the row, never errors, and never creates a second row.
func (s *SuppressionStore) Add(ctx context.Context, email, reason string, source SuppressionSource, note, createdBy string) (created bool, err error) {
	email = normaliseEmail(email)
	reason = CanonicalReason(reason, source)
	var actor any
	if createdBy = strings.TrimSpace(createdBy); createdBy != "" {
		actor = createdBy
	}
	err = s.pool.QueryRow(ctx, `
		INSERT INTO suppressed_emails (email, reason, source, note, created_by, created_at)
		VALUES ($1, $2, $3, $4, $5, now())
		ON CONFLICT (email) DO UPDATE
			SET reason     = EXCLUDED.reason,
			    source     = EXCLUDED.source,
			    -- An incoming empty note never erases one already recorded; a
			    -- populated one is newer detail and replaces it.
			    note       = COALESCE(NULLIF(EXCLUDED.note, ''), suppressed_emails.note),
			    created_by = COALESCE(EXCLUDED.created_by, suppressed_emails.created_by),
			    created_at = now()
		RETURNING (xmax = 0)`,
		email, reason, string(source), strings.TrimSpace(note), actor).Scan(&created)
	return created, err
}

// Upsert is the pre-admin-surface write path, kept so callers that have not
// adopted Add keep working. It is a thin wrapper over Add:
//
//   - a free-text reason is mapped to a canonical code (CanonicalReason) and the
//     original text is preserved in note, so the reason CHECK added by migration
//     000061 can never be violated by an older call site;
//   - an already-canonical reason keeps an empty note.
//
// Returns the email normalised to lower-case. New code should call Add.
func (s *SuppressionStore) Upsert(ctx context.Context, email string, reason string, source SuppressionSource) (string, error) {
	note := ""
	if !IsCanonicalReason(strings.TrimSpace(reason)) {
		note = strings.TrimSpace(reason)
	}
	email = normaliseEmail(email)
	if _, err := s.Add(ctx, email, reason, source, note, ""); err != nil {
		return email, err
	}
	return email, nil
}

// IsSuppressed reports whether an address is on the suppression list.
func (s *SuppressionStore) IsSuppressed(ctx context.Context, email string) (bool, error) {
	email = normaliseEmail(email)
	var one int
	err := s.pool.QueryRow(ctx,
		`SELECT 1 FROM suppressed_emails WHERE email = $1`, email).Scan(&one)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Remove deletes a suppression (the super-admin's "not actually a bad address"
// action, and the ops run-book's un-suppress). Removing an address that is not
// on the list is a no-op, not an error — the caller that wants to know whether
// anything was removed should read the row first.
func (s *SuppressionStore) Remove(ctx context.Context, email string) error {
	email = normaliseEmail(email)
	_, err := s.pool.Exec(ctx, `DELETE FROM suppressed_emails WHERE email = $1`, email)
	return err
}

// RemoveIfPresent deletes a suppression and reports whether a row was actually
// removed. The admin DELETE endpoint needs the distinction to answer 204 for a
// real removal and 404 when the address was never on the list.
func (s *SuppressionStore) RemoveIfPresent(ctx context.Context, email string) (bool, error) {
	email = normaliseEmail(email)
	tag, err := s.pool.Exec(ctx, `DELETE FROM suppressed_emails WHERE email = $1`, email)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// suppressionSelect is the column list every read of this table uses, so the
// list, the search page and the single-row read cannot drift apart.
const suppressionSelect = `
	SELECT s.email,
	       s.reason,
	       s.source,
	       s.note,
	       COALESCE(s.created_by::text, ''),
	       COALESCE(u.email, ''),
	       s.created_at
	  FROM suppressed_emails s
	  LEFT JOIN users u ON u.id = s.created_by`

// scanSuppressedEmail scans one suppressionSelect row.
func scanSuppressedEmail(row pgx.Row) (SuppressedEmail, error) {
	var se SuppressedEmail
	err := row.Scan(&se.Email, &se.Reason, &se.Source, &se.Note,
		&se.CreatedBy, &se.CreatedByEmail, &se.CreatedAt)
	return se, err
}

// List returns all current suppressions oldest-first (ops visibility).
func (s *SuppressionStore) List(ctx context.Context, limit int) ([]SuppressedEmail, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, suppressionSelect+` ORDER BY s.created_at LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SuppressedEmail
	for rows.Next() {
		se, err := scanSuppressedEmail(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, se)
	}
	return out, rows.Err()
}

// likeEscape escapes the LIKE metacharacters in a user-supplied search term so
// a query of "%" searches for a literal percent sign instead of matching the
// whole list. Paired with `ESCAPE '\'` in the query below.
func likeEscape(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(q)
}

// Search returns one page of suppressions, newest first, plus the total row
// count for the pager. An empty q lists everything; otherwise q is matched
// case-insensitively against the email and the note (so "bounce" finds both a
// bounce-sourced row and a manual entry whose note says so). The address is
// normalised first, so searching " B@C.com " behaves like searching "b@c.com".
func (s *SuppressionStore) Search(ctx context.Context, q string, page, limit int) ([]SuppressedEmail, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	q = likeEscape(normaliseEmail(q))

	// A nil pattern means "no filter"; the same WHERE serves both the page and
	// the COUNT so they can never disagree.
	var pattern any
	if q != "" {
		pattern = "%" + q + "%"
	}
	const where = `
	 WHERE ($1::text IS NULL
	        OR s.email ILIKE $1 ESCAPE '\'
	        OR s.note  ILIKE $1 ESCAPE '\')`

	var total int64
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM suppressed_emails s`+where, pattern).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := s.pool.Query(ctx,
		suppressionSelect+where+` ORDER BY s.created_at DESC, s.email LIMIT $2 OFFSET $3`,
		pattern, limit, (page-1)*limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]SuppressedEmail, 0, limit)
	for rows.Next() {
		se, err := scanSuppressedEmail(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, se)
	}
	return out, total, rows.Err()
}

// ByEmail returns one suppression, and (nil, nil) when the address is not on
// the list.
func (s *SuppressionStore) ByEmail(ctx context.Context, email string) (*SuppressedEmail, error) {
	email = normaliseEmail(email)
	se, err := scanSuppressedEmail(s.pool.QueryRow(ctx, suppressionSelect+` WHERE s.email = $1`, email))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &se, nil
}
