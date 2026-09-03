// Outbound webhook store (outbound_webhooks table from 000001).
package dbstore

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// OutboundWebhook models one endpoint. Secret is stored plaintext here (it
// already IS the shared secret; never displayed after create).
type OutboundWebhook struct {
	ID        string
	AccountID string
	// FormID scopes the webhook to a single form (issue #196). Empty = NULL =
	// space/account-wide (fires for all forms' events); set = fires only for
	// that form's events.
	FormID    string
	URL       string
	Secret    string
	Events    []string
	Active    bool
	CreatedAt time.Time
}

// WebhookStore wraps pgx for outbound webhooks.
type WebhookStore struct{ pool *pgxpool.Pool }

// NewWebhookStore builds one.
func NewWebhookStore(pool *pgxpool.Pool) *WebhookStore { return &WebhookStore{pool: pool} }

// Create registers a webhook, returning its id+url. A non-empty formID scopes
// the webhook to that form (issue #196); empty formID = space/account-wide.
func (s *WebhookStore) Create(ctx context.Context, accountID, formID, url, secret string, events []string) (*OutboundWebhook, error) {
	wh := &OutboundWebhook{AccountID: accountID, FormID: formID, URL: url, Secret: secret, Events: events, Active: true}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO outbound_webhooks (account_id, form_id, url, secret, events)
		 VALUES ($1,NULLIF($2,'')::uuid,$3,$4,$5) RETURNING id, created_at`,
		accountID, formID, url, secret, events).Scan(&wh.ID, &wh.CreatedAt)
	return wh, err
}

// ListByAccount lists url+id (secrets never returned to the UI). A non-empty
// formID narrows to that form's webhooks; empty formID returns the
// space/account-wide webhooks (form_id IS NULL).
func (s *WebhookStore) ListByAccount(ctx context.Context, accountID, formID string) ([]*OutboundWebhook, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, url, events FROM outbound_webhooks
		 WHERE account_id=$1 AND active AND form_id IS NOT DISTINCT FROM NULLIF($2,'')::uuid
		 ORDER BY created_at`, accountID, formID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*OutboundWebhook
	for rows.Next() {
		wh := &OutboundWebhook{}
		if err := rows.Scan(&wh.ID, &wh.URL, &wh.Events); err != nil {
			return nil, err
		}
		out = append(out, wh)
	}
	return out, rows.Err()
}

// Delete soft-disables a webhook owned by the account.
func (s *WebhookStore) Delete(ctx context.Context, accountID, webhookID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE outbound_webhooks SET active=false WHERE id=$2 AND account_id=$1`,
		accountID, webhookID)
	return err
}

// Update edits a webhook owned by the account. If secret is "" it is left
// unchanged; url and events are always replaced with the provided values.
// Returns pgx.ErrNoRows when the webhook does not exist or is not owned.
func (s *WebhookStore) Update(ctx context.Context, accountID, webhookID, url, secret string, events []string) error {
	var res string
	err := s.pool.QueryRow(ctx,
		`UPDATE outbound_webhooks
		   SET url=$3,
		       secret=CASE WHEN $4='' THEN secret ELSE $4 END,
		       events=$5
		 WHERE id=$2 AND account_id=$1
		 RETURNING id`,
		accountID, webhookID, url, secret, events).Scan(&res)
	return err
}

// ActiveForEvent lists webhooks of the account subscribed to the event.
func (s *WebhookStore) ActiveForEvent(ctx context.Context, accountID, event string) ([]*OutboundWebhook, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, url, secret, events FROM outbound_webhooks
		 WHERE account_id=$1 AND active AND $2 = ANY(events)`, accountID, event)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*OutboundWebhook
	for rows.Next() {
		wh := &OutboundWebhook{}
		if err := rows.Scan(&wh.ID, &wh.URL, &wh.Secret, &wh.Events); err != nil {
			return nil, err
		}
		out = append(out, wh)
	}
	return out, rows.Err()
}

// ListActiveByAccount returns active webhooks for dispatch. A non-empty
// formID narrows to that form's webhooks; empty formID returns the
// space/account-wide webhooks (form_id IS NULL).
func (s *WebhookStore) ListActiveByAccount(ctx context.Context, accountID, formID string) ([]*OutboundWebhook, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, account_id, COALESCE(form_id::text,''), url, secret, events FROM outbound_webhooks
		 WHERE account_id=$1 AND active AND form_id IS NOT DISTINCT FROM NULLIF($2,'')::uuid`,
		accountID, formID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*OutboundWebhook
	for rows.Next() {
		w := &OutboundWebhook{}
		if err := rows.Scan(&w.ID, &w.AccountID, &w.FormID, &w.URL, &w.Secret, &w.Events); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// ListActiveForDispatch returns the webhooks to fire for one event on one
// form: the form-scoped webhooks (form_id = formID) plus the account-wide ones
// (form_id IS NULL). Both are account-scoped and active.
func (s *WebhookStore) ListActiveForDispatch(ctx context.Context, accountID, formID string) ([]*OutboundWebhook, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, account_id, COALESCE(form_id::text,''), url, secret, events FROM outbound_webhooks
		 WHERE account_id=$1 AND active AND (form_id IS NULL OR form_id::text=$2)`,
		accountID, formID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*OutboundWebhook
	for rows.Next() {
		w := &OutboundWebhook{}
		if err := rows.Scan(&w.ID, &w.AccountID, &w.FormID, &w.URL, &w.Secret, &w.Events); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// RecordDelivery persists one delivery attempt (log + audit).
func (s *WebhookStore) RecordDelivery(ctx context.Context, webhookID, event, payload string, statusCode int, success bool, attempt int, errMsg string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO outbound_webhook_deliveries (webhook_id, event, payload, status_code, success, attempt, error)
		 VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''))`,
		webhookID, event, payload, statusCode, success, attempt, errMsg)
	return err
}

// ListDeliveries latest N for a webhook (settings UI + REST + MCP parity).
// Payload is truncated so the summary log stays light over the wire.
func (s *WebhookStore) ListDeliveries(ctx context.Context, accountID, webhookID string, limit int) ([]map[string]any, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT d.id, d.event, LEFT(d.payload, 500), d.status_code, d.success, d.attempt, COALESCE(d.error,''), d.created_at
		 FROM outbound_webhook_deliveries d
		 JOIN outbound_webhooks w ON w.id = d.webhook_id
		 WHERE d.webhook_id=$2 AND w.account_id=$1 ORDER BY d.created_at DESC LIMIT $3`,
		accountID, webhookID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id string
		var event string
		var payload string
		var code *int
		var ok bool
		var attempt int
		var errMsg string
		var ts time.Time
		if err := rows.Scan(&id, &event, &payload, &code, &ok, &attempt, &errMsg, &ts); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"id": id, "event": event, "payload": payload,
			"status_code": code, "success": ok, "attempt": attempt,
			"error": errMsg, "created_at": ts,
		})
	}
	return out, rows.Err()
}

// ── Issue #197: persistent retry queue ──────────────────────────────────────

// WebhookRetry is one queued retry for a webhook delivery. It is durable in
// the webhook_retries table so retries survive an API restart.
type WebhookRetry struct {
	ID           string
	WebhookID    string
	Event        string
	Payload      string
	Attempt      int
	NextRetryAt  time.Time
	DeadLettered bool
	CreatedAt    time.Time
}

// EnqueueRetry inserts a retry into the persistent queue. nextRetryAt is when
// the sweep should re-deliver it. A manual retry passes time.Now() so it is
// picked up on the next sweep tick.
func (s *WebhookStore) EnqueueRetry(ctx context.Context, webhookID, event, payload string, attempt int, nextRetryAt time.Time) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO webhook_retries (webhook_id, event, payload, attempt, next_retry_at)
		 VALUES ($1,$2,$3,$4,$5)`,
		webhookID, event, payload, attempt, nextRetryAt)
	return err
}

// DueRetries returns the non-dead-lettered retries whose next_retry_at has
// passed, oldest first. The sweep re-delivers these.
func (s *WebhookStore) DueRetries(ctx context.Context, limit int) ([]*WebhookRetry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, webhook_id, event, payload, attempt, next_retry_at, dead_lettered, created_at
		 FROM webhook_retries
		 WHERE NOT dead_lettered AND next_retry_at <= now()
		 ORDER BY next_retry_at ASC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*WebhookRetry
	for rows.Next() {
		r := &WebhookRetry{}
		if err := rows.Scan(&r.ID, &r.WebhookID, &r.Event, &r.Payload, &r.Attempt, &r.NextRetryAt, &r.DeadLettered, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DeleteRetry removes a retry from the queue (called on success).
func (s *WebhookStore) DeleteRetry(ctx context.Context, retryID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM webhook_retries WHERE id=$1`, retryID)
	return err
}

// MarkDeadLetter flags a retry as dead-lettered after the final failed attempt
// so it stays visible (not silently dropped) and can be manually retried.
func (s *WebhookStore) MarkDeadLetter(ctx context.Context, retryID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE webhook_retries SET dead_lettered=true WHERE id=$1`, retryID)
	return err
}

// ScheduleRetry updates a retry's attempt count and next delivery time. Used by
// the sweep to advance the backoff ladder.
func (s *WebhookStore) ScheduleRetry(ctx context.Context, retryID string, attempt int, nextRetryAt time.Time) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE webhook_retries SET attempt=$2, next_retry_at=$3 WHERE id=$1`,
		retryID, attempt, nextRetryAt)
	return err
}

// RetryNow re-enqueues a dead-lettered (or any) retry immediately so the next
// sweep tick re-delivers it. Returns pgx.ErrNoRows when the retry does not
// exist or is not owned by the account (via the webhook join).
func (s *WebhookStore) RetryNow(ctx context.Context, accountID, retryID string) error {
	var res string
	err := s.pool.QueryRow(ctx,
		`UPDATE webhook_retries r
		   SET dead_lettered=false, next_retry_at=now()
		   FROM outbound_webhooks w
		  WHERE r.id=$2 AND r.webhook_id=w.id AND w.account_id=$1
		  RETURNING r.id`,
		accountID, retryID).Scan(&res)
	return err
}

// ListDeadLettered returns the dead-lettered retries for one webhook
// (account-scoped via the webhook join), newest first. Surfaced in the
// delivery log so merchants can see and manually retry dropped deliveries.
func (s *WebhookStore) ListDeadLettered(ctx context.Context, accountID, webhookID string, limit int) ([]map[string]any, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx,
		`SELECT r.id, r.event, LEFT(r.payload, 500), r.attempt, r.next_retry_at, r.created_at
		 FROM webhook_retries r
		 JOIN outbound_webhooks w ON w.id = r.webhook_id
		 WHERE r.webhook_id=$2 AND w.account_id=$1 AND r.dead_lettered
		 ORDER BY r.created_at DESC LIMIT $3`,
		accountID, webhookID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, event, payload string
		var attempt int
		var next, created time.Time
		if err := rows.Scan(&id, &event, &payload, &attempt, &next, &created); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"id": id, "event": event, "payload": payload,
			"attempt": attempt, "next_retry_at": next, "created_at": created,
		})
	}
	return out, rows.Err()
}

// GetByID returns one webhook by id, account-scoped. Returns pgx.ErrNoRows
// when not found or not owned.
func (s *WebhookStore) GetByID(ctx context.Context, accountID, webhookID string) (*OutboundWebhook, error) {
	wh := &OutboundWebhook{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, account_id, COALESCE(form_id::text,''), url, secret, events, active, created_at
		 FROM outbound_webhooks WHERE id=$2 AND account_id=$1`,
		accountID, webhookID).Scan(&wh.ID, &wh.AccountID, &wh.FormID, &wh.URL, &wh.Secret, &wh.Events, &wh.Active, &wh.CreatedAt)
	if err != nil {
		return nil, err
	}
	return wh, nil
}

// GetByIDAny returns one webhook by id regardless of account. Used by the
// retry sweep, which re-delivers queued retries across all accounts.
func (s *WebhookStore) GetByIDAny(ctx context.Context, webhookID string) (*OutboundWebhook, error) {
	wh := &OutboundWebhook{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, account_id, COALESCE(form_id::text,''), url, secret, events, active, created_at
		 FROM outbound_webhooks WHERE id=$1`,
		webhookID).Scan(&wh.ID, &wh.AccountID, &wh.FormID, &wh.URL, &wh.Secret, &wh.Events, &wh.Active, &wh.CreatedAt)
	if err != nil {
		return nil, err
	}
	return wh, nil
}

// GetDelivery returns one delivery row by id, account-scoped via the webhook
// join. Returns pgx.ErrNoRows when not found or not owned.
func (s *WebhookStore) GetDelivery(ctx context.Context, accountID, deliveryID string) (map[string]any, error) {
	var id, event, payload, errMsg string
	var code *int
	var ok bool
	var attempt int
	var ts time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT d.id, d.event, d.payload, d.status_code, d.success, d.attempt, COALESCE(d.error,''), d.created_at
		 FROM outbound_webhook_deliveries d
		 JOIN outbound_webhooks w ON w.id = d.webhook_id
		 WHERE d.id=$2 AND w.account_id=$1`,
		accountID, deliveryID).Scan(&id, &event, &payload, &code, &ok, &attempt, &errMsg, &ts)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"id": id, "event": event, "payload": payload,
		"status_code": code, "success": ok, "attempt": attempt,
		"error": errMsg, "created_at": ts,
	}, nil
}
