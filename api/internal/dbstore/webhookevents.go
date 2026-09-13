package dbstore

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Inbound provider webhook events (issue #517).
//
// The dedup row used to be written BEFORE the business logic ran, and
// processed_at was only stamped on success. A processing failure therefore left
// a row whose dedup_key was already taken with processed_at NULL, so every
// provider redelivery was answered 200 "duplicate" and the paid sale was never
// settled — there was no path back. The row is now a claim/complete record:
//
//	claim    — INSERT ... ON CONFLICT DO NOTHING, status 'pending'
//	process  — the business logic (ProcessCallback) runs
//	complete — MarkProcessed: status 'processed', processed_at now()
//	fail     — MarkFailed: status 'failed' (immediately re-claimable)
//
// A claim is owned by one attempt for DefaultClaimTimeout. A second delivery
// that arrives while the claim is live is a genuine CONCURRENT duplicate: it is
// answered 2xx without processing, so the sale is settled by exactly one
// attempt. A claim whose attempt died (row untouched for longer than the
// timeout — deploy restart, connection blip, lock timeout) is reclaimable, so
// the work is retried rather than swallowed. Only a row with
// processed_at IS NOT NULL is a permanent duplicate and must never re-fire a
// webhook, a receipt, or a subscription activation.
const (
	// WebhookEventPending: claimed, no successful attempt yet. Re-claimable.
	WebhookEventPending = "pending"
	// WebhookEventProcessing: an attempt holds the claim. Protected by the
	// claim timeout, after which it is re-claimable.
	WebhookEventProcessing = "processing"
	// WebhookEventProcessed: business logic committed — permanent duplicate.
	WebhookEventProcessed = "processed"
	// WebhookEventFailed: an attempt returned an error. Immediately re-claimable.
	WebhookEventFailed = "failed"
)

// DefaultClaimTimeout is how long one attempt may own a claim before another
// delivery (or the reclaim sweep) may retake it. It must comfortably exceed a
// slow ProcessCallback — which holds a per-sale Postgres advisory lock across a
// transaction plus the synchronous transitions — while staying short enough
// that a crashed attempt is retried promptly. It is deliberately well inside
// CHIP's redelivery window.
const DefaultClaimTimeout = 2 * time.Minute

// WebhookEvent models the webhook_events table (inbound provider events).
type WebhookEvent struct {
	ID          string
	AccountID   string
	Source      string
	EventType   string
	Payload     []byte
	DedupKey    string
	Status      string
	Attempts    int
	ProcessedAt *time.Time
	CreatedAt   time.Time
}

// WebhookEventStore wraps pgx for inbound webhook events.
type WebhookEventStore struct{ pool *pgxpool.Pool }

// NewWebhookEventStore builds the store.
func NewWebhookEventStore(pool *pgxpool.Pool) *WebhookEventStore {
	return &WebhookEventStore{pool: pool}
}

// ClaimTx records the event as a claim inside the caller's transaction and
// reports whether THIS attempt may process it. It is the only gate between a
// delivery and the business logic, so a failure can never consume a dedup key:
// if the caller's transaction rolls back (because processing failed), the claim
// rolls back with it.
//
// It returns inserted=true when the attempt won the claim — either a new
// dedup_key, or a dead claim (a 'processing' row untouched for longer than
// claimTimeout, or one left 'pending'/'failed') that is retaken. inserted=false
// means another attempt holds the claim ('processed' permanently — never
// re-fire; 'processing' within the timeout — a concurrent duplicate).
func (s *WebhookEventStore) ClaimTx(ctx context.Context, tx pgx.Tx, e *WebhookEvent, claimTimeout time.Duration) (bool, error) {
	if claimTimeout <= 0 {
		claimTimeout = DefaultClaimTimeout
	}
	tag, err := tx.Exec(ctx,
		`INSERT INTO webhook_events (source, event_type, payload, dedup_key, status)
		 VALUES ($1,$2,$3,$4,$5)
		 ON CONFLICT (dedup_key) DO UPDATE
		    SET status     = $5,
		        attempts   = webhook_events.attempts + 1,
		        updated_at = now()
		  WHERE webhook_events.processed_at IS NULL
		    AND (webhook_events.updated_at < now() - $6::interval
		         OR webhook_events.status IN ($7, $8))`,
		e.Source, e.EventType, e.Payload, e.DedupKey, WebhookEventProcessing,
		claimTimeout.String(), WebhookEventPending, WebhookEventFailed)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ClaimStandalone takes the claim in its own short transaction, for events
// with no direct sale to lock (subscription charges, card updates). The caller
// must complete it with MarkProcessed or release it with MarkFailed.
func (s *WebhookEventStore) ClaimStandalone(ctx context.Context, e *WebhookEvent, claimTimeout time.Duration) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	claimed, err := s.ClaimTx(ctx, tx, e, claimTimeout)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return claimed, nil
}

// TryInsert returns true when the event is new (dedup_key unseen). It relies on
// the unique index over (dedup_key); a conflict means duplicate. Deprecated for
// the callback path — use ClaimTx, which can tell a dead claim from a settled
// event (issue #517).
func (s *WebhookEventStore) TryInsert(ctx context.Context, e *WebhookEvent) (bool, error) {
	status := e.Status
	if status == "" {
		status = WebhookEventPending
	}
	tag, err := s.pool.Exec(ctx,
		`INSERT INTO webhook_events (source, event_type, payload, dedup_key, status)
		 VALUES ($1,$2,$3,$4,$5)
		 ON CONFLICT (dedup_key) DO NOTHING`,
		e.Source, e.EventType, e.Payload, e.DedupKey, status)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// MarkProcessed stamps the event as successfully processed: status 'processed'
// plus processed_at. Only this state is a permanent duplicate.
func (s *WebhookEventStore) MarkProcessed(ctx context.Context, dedupKey string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE webhook_events SET processed_at=now(), status=$2 WHERE dedup_key=$1`,
		dedupKey, WebhookEventProcessed)
	return err
}

// MarkFailed records that an attempt returned an error, making the event
// immediately re-claimable instead of waiting out the claim timeout. Best
// effort: the event is retried by CHIP redelivery / the reclaim sweep either
// way.
func (s *WebhookEventStore) MarkFailed(ctx context.Context, dedupKey string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE webhook_events SET status=$2 WHERE dedup_key=$1 AND processed_at IS NULL`,
		dedupKey, WebhookEventFailed)
	return err
}

// ReclaimStale releases dead claims (issue #517): a 'processing' row untouched
// for longer than olderThan belongs to an attempt that is gone (crash, restart,
// DB blip), so it is flipped to 'pending' and becomes claimable again. A
// 'processed' row is never touched. Returns the number of rows released.
func (s *WebhookEventStore) ReclaimStale(ctx context.Context, olderThan time.Duration) (int64, error) {
	if olderThan <= 0 {
		olderThan = DefaultClaimTimeout
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE webhook_events
		    SET status=$3, updated_at=now()
		  WHERE status=$1
		    AND processed_at IS NULL
		    AND updated_at < now() - $2::interval`,
		WebhookEventProcessing, olderThan.String(), WebhookEventPending)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// Reclaimable lists events that have not been successfully processed and are
// due for a retry: 'pending' (never completed, or released by ReclaimStale) and
// 'failed'. They are returned oldest first, capped at limit.
func (s *WebhookEventStore) Reclaimable(ctx context.Context, limit int) ([]*WebhookEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, COALESCE(account_id::text,''), source, COALESCE(event_type,''),
		        payload, dedup_key, status, attempts, processed_at, created_at
		   FROM webhook_events
		  WHERE processed_at IS NULL AND status IN ($1, $2)
		  ORDER BY created_at ASC
		  LIMIT $3`,
		WebhookEventPending, WebhookEventFailed, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*WebhookEvent
	for rows.Next() {
		e := &WebhookEvent{}
		if err := rows.Scan(&e.ID, &e.AccountID, &e.Source, &e.EventType,
			&e.Payload, &e.DedupKey, &e.Status, &e.Attempts, &e.ProcessedAt, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// EventState looks up an event's dedup state (empty when unknown). Used by
// tests and by the callback handler's logging to distinguish a settled
// duplicate from a dead claim.
func (s *WebhookEventStore) EventState(ctx context.Context, dedupKey string) (status string, processedAt *time.Time, err error) {
	err = s.pool.QueryRow(ctx,
		`SELECT status, processed_at FROM webhook_events WHERE dedup_key=$1`, dedupKey).
		Scan(&status, &processedAt)
	if err == pgx.ErrNoRows {
		return "", nil, nil
	}
	return status, processedAt, err
}

// UnprocessedCount counts inbound events that have been claimed but never
// successfully processed — the #517 backlog. A non-zero value older than the
// claim timeout is the production signal that deliveries are failing.
func (s *WebhookEventStore) UnprocessedCount(ctx context.Context) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM webhook_events WHERE processed_at IS NULL`).Scan(&n)
	return n, err
}
