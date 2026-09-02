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

// Create registers a webhook, returning its id+url.
func (s *WebhookStore) Create(ctx context.Context, accountID, url, secret string, events []string) (*OutboundWebhook, error) {
	wh := &OutboundWebhook{AccountID: accountID, URL: url, Secret: secret, Events: events, Active: true}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO outbound_webhooks (account_id, url, secret, events)
		 VALUES ($1,$2,$3,$4) RETURNING id, created_at`,
		accountID, url, secret, events).Scan(&wh.ID, &wh.CreatedAt)
	return wh, err
}

// ListByAccount lists url+id (secrets never returned to the UI).
func (s *WebhookStore) ListByAccount(ctx context.Context, accountID string) ([]*OutboundWebhook, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, url, events FROM outbound_webhooks WHERE account_id=$1 AND active ORDER BY created_at`, accountID)
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

// ListActiveByAccount returns active webhooks for dispatch.
func (s *WebhookStore) ListActiveByAccount(ctx context.Context, accountID string) ([]*OutboundWebhook, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, account_id, url, secret, events FROM outbound_webhooks WHERE account_id=$1 AND active`,
		accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*OutboundWebhook
	for rows.Next() {
		w := &OutboundWebhook{}
		if err := rows.Scan(&w.ID, &w.AccountID, &w.URL, &w.Secret, &w.Events); err != nil {
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
