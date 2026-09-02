// Suppression list store (issue #56): SES bounce/complaint feedback is
// persisted here so no outbound send ever targets a suppressed address.
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

// SuppressedEmail mirrors a suppressed_emails row.
type SuppressedEmail struct {
	Email     string
	Reason    string
	Source    string
	CreatedAt time.Time
}

// SuppressionStore wraps pgx for the suppressed_emails table.
type SuppressionStore struct {
	pool *pgxpool.Pool
}

// NewSuppressionStore builds a SuppressionStore over the given pool.
func NewSuppressionStore(pool *pgxpool.Pool) *SuppressionStore {
	return &SuppressionStore{pool: pool}
}

// Upsert records (or refreshes) a suppression for an address. Idempotent: a
// repeat bounce/complaint for the same address keeps the row's provenance but
// updates reason/source/created_at. Returns the email normalised to lower-case.
func (s *SuppressionStore) Upsert(ctx context.Context, email string, reason string, source SuppressionSource) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	_, err := s.pool.Exec(ctx, `
		INSERT INTO suppressed_emails (email, reason, source, created_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (email) DO UPDATE
			SET reason = EXCLUDED.reason,
			    source = EXCLUDED.source,
			    created_at = now()`,
		email, reason, string(source))
	return email, err
}

// IsSuppressed reports whether an address is on the suppression list.
func (s *SuppressionStore) IsSuppressed(ctx context.Context, email string) (bool, error) {
	email = strings.ToLower(strings.TrimSpace(email))
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

// Remove deletes a suppression (ops run-book: un-suppress a false positive).
func (s *SuppressionStore) Remove(ctx context.Context, email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	_, err := s.pool.Exec(ctx, `DELETE FROM suppressed_emails WHERE email = $1`, email)
	return err
}

// List returns all current suppressions oldest-first (ops visibility).
func (s *SuppressionStore) List(ctx context.Context, limit int) ([]SuppressedEmail, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx,
		`SELECT email, reason, source, created_at FROM suppressed_emails ORDER BY created_at LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SuppressedEmail
	for rows.Next() {
		var se SuppressedEmail
		if err := rows.Scan(&se.Email, &se.Reason, &se.Source, &se.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, se)
	}
	return out, rows.Err()
}
