// Outbound webhook store (outbound_webhooks table from 000001).
package dbstore

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OutboundWebhook models one endpoint. Secret is stored plaintext here (it
// already IS the shared secret; never displayed after create).
type OutboundWebhook struct {
	ID        string
	AccountID string
	// FormID scopes the webhook to a single form (issue #196). Empty = NULL =
	// workspace/account-wide (fires for all forms' events); set = fires only for
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

// ErrWebhookNotFound means the id named no webhook this account may see:
// it exists nowhere, it belongs to another account, or it cannot be a UUID at
// all (Postgres raises SQLSTATE 22P02). The delete/update/deliveries routes
// answer 404 from it, so a caller-supplied bad id is never a 500.
var ErrWebhookNotFound = errors.New("webhook not found")

// NewWebhookStore builds one.
func NewWebhookStore(pool *pgxpool.Pool) *WebhookStore { return &WebhookStore{pool: pool} }

// Create registers a webhook, returning its id+url. A non-empty formID scopes
// the webhook to that form (issue #196); empty formID = workspace/account-wide.
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
// workspace/account-wide webhooks (form_id IS NULL).
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

// Delete soft-disables a webhook owned by the account. A miss — no such
// webhook, another account's, or an id that cannot be a UUID at all (SQLSTATE
// 22P02) — is ErrWebhookNotFound, so DELETE /v1/rest/webhooks/{id} answers 404
// rather than 200 ("deleted") for an endpoint it did not disable or 500 for a
// malformed id (see isNoSuchRow).
func (s *WebhookStore) Delete(ctx context.Context, accountID, webhookID string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE outbound_webhooks SET active=false WHERE id=$2 AND account_id=$1`,
		accountID, webhookID)
	if isNoSuchRow(err) {
		return ErrWebhookNotFound
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrWebhookNotFound
	}
	return nil
}

// Update edits a webhook owned by the account. If secret is "" it is left
// unchanged; url and events are always replaced with the provided values.
// A miss — no such webhook, another account's, or an id that cannot be a UUID
// at all — is ErrWebhookNotFound (see isNoSuchRow).
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
	if isNoSuchRow(err) {
		return ErrWebhookNotFound
	}
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
// workspace/account-wide webhooks (form_id IS NULL).
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
//
// Account-scoped, and the webhook id must resolve for the caller: an id that
// names no webhook this account owns — never existed, another account's, or
// not a UUID at all (SQLSTATE 22P02) — is ErrWebhookNotFound, so the route
// answers 404 instead of an empty 200 list or a 500 (see isNoSuchRow). The
// ownership check is a separate lookup rather than a filter on the delivery
// join, because a webhook with no deliveries yet must still report "found".
func (s *WebhookStore) ListDeliveries(ctx context.Context, accountID, webhookID string, limit int) ([]map[string]any, error) {
	var owned string
	if err := s.pool.QueryRow(ctx,
		`SELECT id FROM outbound_webhooks WHERE id=$2 AND account_id=$1`,
		accountID, webhookID).Scan(&owned); err != nil {
		if isNoSuchRow(err) {
			return nil, ErrWebhookNotFound
		}
		return nil, err
	}
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
//
// Issue #522: the enqueue is account-scoped at the SQL level — the webhook id
// must belong to accountID, and the row is only inserted when it does. There is
// deliberately no unscoped variant: a retry row is delivered by the sweep with
// the webhook's own URL AND its own signing secret, so enqueuing against an id
// the caller does not own is a cross-tenant signed-injection primitive. Callers
// that legitimately enqueue for an account they are acting as (the dispatcher,
// which enqueues for the account owning the form/sale) pass that account id.
// Returns pgx.ErrNoRows when the webhook does not exist or is not owned by
// accountID — no row is created.
func (s *WebhookStore) EnqueueRetry(ctx context.Context, accountID, webhookID, event, payload string, attempt int, nextRetryAt time.Time) error {
	tag, err := s.pool.Exec(ctx,
		`INSERT INTO webhook_retries (webhook_id, event, payload, attempt, next_retry_at)
		 SELECT w.id, $2, $3, $4, $5
		   FROM outbound_webhooks w
		  WHERE w.id=$1 AND w.account_id=$6`,
		webhookID, event, payload, attempt, nextRetryAt, accountID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
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
// exist, is not owned by the account (via the webhook join), or cannot be a
// UUID at all — a malformed id is the same "no such row" to the caller, and
// without the mapping it escaped as SQLSTATE 22P02 and became a 500 (see
// isNoSuchRow).
func (s *WebhookStore) RetryNow(ctx context.Context, accountID, retryID string) error {
	var res string
	err := s.pool.QueryRow(ctx,
		`UPDATE webhook_retries r
		   SET dead_lettered=false, next_retry_at=now()
		   FROM outbound_webhooks w
		  WHERE r.id=$2 AND r.webhook_id=w.id AND w.account_id=$1
		  RETURNING r.id`,
		accountID, retryID).Scan(&res)
	if isNoSuchRow(err) {
		return pgx.ErrNoRows
	}
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

// GetByIDAny returns one webhook by id regardless of account. It is
// SWEEPER-ONLY (issue #522): it exists for the retry sweep, which re-delivers
// queued retries across all accounts and has no request context to scope
// against. It must NEVER be called from an HTTP/MCP handler with an id taken
// from the request — handlers authorise through GetByID (account-scoped) or by
// comparing GetDelivery's webhook_id. The Any suffix (and the sweeper-only name
// at the call site) is the warning.
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
// join. Returns pgx.ErrNoRows when not found, not owned, or when the id cannot
// be a UUID at all — all three are the same answer to the caller, and without
// the mapping the malformed id raised SQLSTATE 22P02 into a 500 on the manual
// retry routes (see isNoSuchRow).
//
// The returned map includes webhook_id (issue #522) so a caller can authorise
// the webhook id it was given against the delivery's actual owner in one round
// trip, instead of trusting a separate id from the request.
func (s *WebhookStore) GetDelivery(ctx context.Context, accountID, deliveryID string) (map[string]any, error) {
	var id, webhookID, event, payload, errMsg string
	var code *int
	var ok bool
	var attempt int
	var ts time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT d.id, d.webhook_id, d.event, d.payload, d.status_code, d.success, d.attempt, COALESCE(d.error,''), d.created_at
		 FROM outbound_webhook_deliveries d
		 JOIN outbound_webhooks w ON w.id = d.webhook_id
		 WHERE d.id=$2 AND w.account_id=$1`,
		accountID, deliveryID).Scan(&id, &webhookID, &event, &payload, &code, &ok, &attempt, &errMsg, &ts)
	if isNoSuchRow(err) {
		return nil, pgx.ErrNoRows
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"id": id, "webhook_id": webhookID, "event": event, "payload": payload,
		"status_code": code, "success": ok, "attempt": attempt,
		"error": errMsg, "created_at": ts,
	}, nil
}
