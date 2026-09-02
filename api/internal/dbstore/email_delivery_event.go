// EmailDeliveryEventStore — SES delivery/bounce/complaint feedback (issue #92).
//
// The `email_delivery_events` table is append-only and immutable (no UPDATE/
// DELETE except the retention sweep), matching ClickHouse's model for a future
// export. The SNS endpoint writes Bounce/Complaint here in addition to the
// suppression list; delivered/open/click can be added when the SES config set
// emits them (issue #76).
package dbstore

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// EmailDeliveryEvent mirrors one email_delivery_events row.
type EmailDeliveryEvent struct {
	ID         int64
	AccountID  string // "" when null (system mail)
	Email      string
	EventType  string // delivered | bounce | complaint | open | click
	BounceType string // Permanent | Transient (when bounce)
	Detail     map[string]any
	CreatedAt  time.Time
}

// EmailDeliveryEventStore wraps pgx for the email_delivery_events table.
type EmailDeliveryEventStore struct{ pool *pgxpool.Pool }

// NewEmailDeliveryEventStore builds the store over the given pool.
func NewEmailDeliveryEventStore(pool *pgxpool.Pool) *EmailDeliveryEventStore {
	return &EmailDeliveryEventStore{pool: pool}
}

// Record appends one delivery event. accountID may be empty (system mail →
// NULL). bounceType is only meaningful for bounce events. detail is stored as
// JSONB (nil → {}).
func (s *EmailDeliveryEventStore) Record(ctx context.Context, accountID, email, eventType, bounceType string, detail map[string]any) error {
	if detail == nil {
		detail = map[string]any{}
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO email_delivery_events (account_id, email, event_type, bounce_type, detail)
		VALUES (NULLIF($1,'')::uuid, $2, $3, NULLIF($4,''), $5)`,
		accountID, email, eventType, bounceType, detail)
	return err
}

// List returns the most recent delivery events, newest first, capped at limit.
// Used by the admin/ops view and the #56 breaker's durable bounce-ratio history.
func (s *EmailDeliveryEventStore) List(ctx context.Context, limit int) ([]EmailDeliveryEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, COALESCE(account_id::text,''), email, event_type,
		       COALESCE(bounce_type,''), detail, created_at
		FROM email_delivery_events ORDER BY created_at DESC, id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EmailDeliveryEvent{}
	for rows.Next() {
		var e EmailDeliveryEvent
		if err := rows.Scan(&e.ID, &e.AccountID, &e.Email, &e.EventType,
			&e.BounceType, &e.Detail, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Sweep deletes delivery events older than ttl. Returns rows deleted. This is
// the ONLY DELETE allowed on the table.
func (s *EmailDeliveryEventStore) Sweep(ctx context.Context, ttl time.Duration) (int64, error) {
	if ttl <= 0 {
		return 0, nil
	}
	cutoff := time.Now().UTC().Add(-ttl)
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM email_delivery_events WHERE created_at < $1`, cutoff)
	return tag.RowsAffected(), err
}
