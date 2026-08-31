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
		`SELECT id, url FROM outbound_webhooks WHERE account_id=$1 AND active ORDER BY created_at`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*OutboundWebhook
	for rows.Next() {
		wh := &OutboundWebhook{}
		if err := rows.Scan(&wh.ID, &wh.URL); err != nil {
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
