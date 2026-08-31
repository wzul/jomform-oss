package dbstore

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// WebhookEvent models the webhook_events table (inbound provider events).
type WebhookEvent struct {
	ID          string
	AccountID   string
	Source      string
	EventType   string
	Payload     []byte
	DedupKey    string
	ProcessedAt *time.Time
	CreatedAt   time.Time
}

// WebhookEventStore wraps pgx for inbound webhook events.
type WebhookEventStore struct{ pool *pgxpool.Pool }

// NewWebhookEventStore builds the store.
func NewWebhookEventStore(pool *pgxpool.Pool) *WebhookEventStore {
	return &WebhookEventStore{pool: pool}
}

// TryInsert returns true when the event is new (dedup_key unseen). It relies
// on the unique index over (dedup_key); a conflict means duplicate.
func (s *WebhookEventStore) TryInsert(ctx context.Context, e *WebhookEvent) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`INSERT INTO webhook_events (source, event_type, payload, dedup_key)
		 VALUES ($1,$2,$3,$4)
		 ON CONFLICT (dedup_key) DO NOTHING`,
		e.Source, e.EventType, e.Payload, e.DedupKey)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// MarkProcessed stamps processed_at for a dedup key.
func (s *WebhookEventStore) MarkProcessed(ctx context.Context, dedupKey string) error {
	_, err := s.pool.Exec(ctx, `UPDATE webhook_events SET processed_at=now() WHERE dedup_key=$1`, dedupKey)
	return err
}
