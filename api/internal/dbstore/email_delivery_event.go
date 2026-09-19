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
	"strings"
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
	// SaleRef is the sale's ref_code this event correlates to (issue #106).
	// "" when the event could not be tied to a sale (system mail, or the SES
	// MessageID did not match a stored receipt).
	SaleRef string
	// FormID is the sale's form_id this event correlates to (issue #194),
	// resolved via the sale_ref join. "" when the event has no sale.
	FormID    string
	CreatedAt time.Time
}

// EmailDeliveryEventStore wraps pgx for the email_delivery_events table.
type EmailDeliveryEventStore struct{ pool *pgxpool.Pool }

// NewEmailDeliveryEventStore builds the store over the given pool.
func NewEmailDeliveryEventStore(pool *pgxpool.Pool) *EmailDeliveryEventStore {
	return &EmailDeliveryEventStore{pool: pool}
}

// Record appends one delivery event. accountID may be empty (system mail →
// NULL). bounceType is only meaningful for bounce events. saleRef is the
// sale's ref_code this event correlates to (issue #106); empty → NULL. detail
// is stored as JSONB (nil → {}).
func (s *EmailDeliveryEventStore) Record(ctx context.Context, accountID, email, eventType, bounceType, saleRef string, detail map[string]any) error {
	if detail == nil {
		detail = map[string]any{}
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO email_delivery_events (account_id, email, event_type, bounce_type, sale_ref, detail)
		VALUES (NULLIF($1,'')::uuid, $2, $3, NULLIF($4,''), NULLIF($5,''), $6)`,
		accountID, email, eventType, bounceType, saleRef, detail)
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
		       COALESCE(bounce_type,''), COALESCE(sale_ref,''), detail, created_at
		FROM email_delivery_events ORDER BY created_at DESC, id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EmailDeliveryEvent{}
	for rows.Next() {
		var e EmailDeliveryEvent
		if err := rows.Scan(&e.ID, &e.AccountID, &e.Email, &e.EventType,
			&e.BounceType, &e.SaleRef, &e.Detail, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListBySaleRef returns the delivery events correlated to one sale (issue
// #106), newest first. Used by the per-sale email status view to render the
// Sent → Delivered → Opened → (Clicked) timeline.
func (s *EmailDeliveryEventStore) ListBySaleRef(ctx context.Context, saleRef string) ([]EmailDeliveryEvent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, COALESCE(account_id::text,''), email, event_type,
		       COALESCE(bounce_type,''), COALESCE(sale_ref,''), detail, created_at
		FROM email_delivery_events
		WHERE sale_ref = $1
		ORDER BY created_at ASC, id ASC`, saleRef)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EmailDeliveryEvent{}
	for rows.Next() {
		var e EmailDeliveryEvent
		if err := rows.Scan(&e.ID, &e.AccountID, &e.Email, &e.EventType,
			&e.BounceType, &e.SaleRef, &e.Detail, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// LatestBySaleRefs returns, for each of the given sale refs, the most recent
// delivery event (newest first). Used to surface a compact per-sale email
// status (e.g. "Opened ✓") in the sales list without an N+1 query. The result
// is keyed by sale_ref; refs with no events are simply absent.
func (s *EmailDeliveryEventStore) LatestBySaleRefs(ctx context.Context, saleRefs []string) (map[string]EmailDeliveryEvent, error) {
	out := map[string]EmailDeliveryEvent{}
	if len(saleRefs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT ON (sale_ref) sale_ref, id, COALESCE(account_id::text,''), email,
		       event_type, COALESCE(bounce_type,''), COALESCE(sale_ref,''), detail, created_at
		FROM email_delivery_events
		WHERE sale_ref = ANY($1)
		ORDER BY sale_ref, created_at DESC, id DESC`, saleRefs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var e EmailDeliveryEvent
		if err := rows.Scan(&e.SaleRef, &e.ID, &e.AccountID, &e.Email, &e.EventType,
			&e.BounceType, &e.SaleRef, &e.Detail, &e.CreatedAt); err != nil {
			return nil, err
		}
		out[e.SaleRef] = e
	}
	return out, rows.Err()
}

// CountRecentBounces counts the bounce events for one address in a trailing
// window, optionally narrowed to a bounce type. It backs the soft-bounce rule:
// SES already retries a transient bounce internally, so a single Transient event
// is not worth suppressing — but repeated transient bounces for the same mailbox
// mean it is effectively dead, and the address should stop being mailed.
//
// eventTypes is required (e.g. []string{"bounce"}); bounceType is "" to count
// every bounce type or "Transient"/"Permanent" to narrow. The email argument is
// lower-cased here so a mixed-case caller cannot miss rows — every event is
// written lower-cased (see suppressionWriter.recordDeliveryEvent). Served by the
// idx_ede_email_time btree on (email, created_at DESC).
func (s *EmailDeliveryEventStore) CountRecentBounces(ctx context.Context, email string, eventTypes []string, bounceType string, since time.Time) (int, error) {
	if len(eventTypes) == 0 {
		return 0, nil
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return 0, nil
	}
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM email_delivery_events
		 WHERE email = $1
		   AND event_type = ANY($2)
		   AND ($3 = '' OR bounce_type = $3)
		   AND created_at >= $4`,
		email, eventTypes, bounceType, since).Scan(&n)
	return n, err
}

// DeliveryFilter narrows an account-scoped delivery-event query (issue #194).
// All fields are optional; empty values are ignored. AccountIDs is required
// (account-scoped reads). FormID is matched against the sale's form_id via a
// join on sale_ref, so a merchant can see every email delivery for a form's
// sales. SaleRef filters to one sale's events; EventType to one event type
// (delivered | bounce | complaint | open | click).
type DeliveryFilter struct {
	AccountIDs []string
	FormID     string
	SaleRef    string
	EventType  string
	Page       int
	Limit      int
}

// deliveryWhere builds the WHERE clause + args for an account-scoped delivery
// filter. Returns the clause (with leading " WHERE ") and the bound args.
func deliveryWhere(f DeliveryFilter) (string, []any) {
	args := []any{f.AccountIDs}
	clause := " WHERE e.account_id = ANY($1)"
	if f.SaleRef != "" {
		args = append(args, f.SaleRef)
		clause += " AND e.sale_ref = $" + itoa(len(args))
	}
	if f.EventType != "" {
		args = append(args, f.EventType)
		clause += " AND e.event_type = $" + itoa(len(args))
	}
	if f.FormID != "" {
		args = append(args, f.FormID)
		clause += " AND s.form_id::text = $" + itoa(len(args))
	}
	return clause, args
}

// ListByFilter returns one page of delivery events for the given accounts,
// newest first, plus the total row count for the same filter (for paging).
// Account-scoped; optionally narrowed by form (via the sale's form_id), sale
// ref, and/or event type. Used by GET /v1/rest/email-deliveries and the
// list_email_deliveries MCP tool (issue #194).
func (s *EmailDeliveryEventStore) ListByFilter(ctx context.Context, f DeliveryFilter) ([]EmailDeliveryEvent, int64, error) {
	if len(f.AccountIDs) == 0 {
		return []EmailDeliveryEvent{}, 0, nil
	}
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit <= 0 {
		f.Limit = 20
	}
	if f.Limit > 100 {
		f.Limit = 100
	}
	offset := (f.Page - 1) * f.Limit

	where, args := deliveryWhere(f)
	var total int64
	if err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM email_delivery_events e
		LEFT JOIN sales s ON s.ref_code = e.sale_ref`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT e.id, COALESCE(e.account_id::text,''), e.email, e.event_type,
		       COALESCE(e.bounce_type,''), COALESCE(e.sale_ref,''), e.detail, e.created_at,
		       COALESCE(s.form_id::text,'')
		FROM email_delivery_events e
		LEFT JOIN sales s ON s.ref_code = e.sale_ref`+where+`
		ORDER BY e.created_at DESC, e.id DESC LIMIT $`+itoa(len(args)+1)+` OFFSET $`+itoa(len(args)+2),
		append(args, f.Limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []EmailDeliveryEvent{}
	for rows.Next() {
		var e EmailDeliveryEvent
		if err := rows.Scan(&e.ID, &e.AccountID, &e.Email, &e.EventType,
			&e.BounceType, &e.SaleRef, &e.Detail, &e.CreatedAt, &e.FormID); err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
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
