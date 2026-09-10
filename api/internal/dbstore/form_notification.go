// Per-form email notifications with conditional logic + default (issue #193,
// Gravity-Forms-style). Replaces the flat per-workspace email settings for form
// events: each form owns a list of notification rules; on an event the
// matching rules (by event + condition) fire and enqueue an email via the
// #199 background queue.
package dbstore

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Notification events (mirror the outbound webhook event set).
const (
	NotifyEventSalePaid            = "sale.paid"
	NotifyEventSaleFailed          = "sale.failed"
	NotifyEventResponseSubmitted   = "response.submitted"
	NotifyEventSubscriptionCharged = "subscription.charged"
)

// NotificationCondition is the optional conditional logic on a notification
// rule. Ops: equals | not_equals | contains | gt | lt | not_empty. The
// condition is evaluated against the submitted field values (block_id → value).
type NotificationCondition struct {
	FieldID string `json:"field_id"`
	Op      string `json:"op"`
	Value   string `json:"value"`
}

// FormNotification is one per-form email notification rule.
type FormNotification struct {
	ID        string `json:"id"`
	FormID    string `json:"form_id"`
	Name      string `json:"name"`
	Event     string `json:"event"`
	// ToEmail is the recipient: a literal address, `{admin_email}` (the
	// merchant's account email, resolved at send time), or `{field_id}` (the
	// submitted value of a form field, e.g. a buyer email field).
	ToEmail string `json:"to_email"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
	// Condition is the optional conditional logic; nil = always fire on event.
	Condition *NotificationCondition `json:"condition,omitempty"`
	Enabled   bool                   `json:"enabled"`
	// IsDefault marks the pre-created default notification (notify merchant on
	// sale.paid). The merchant can edit or delete it per form.
	IsDefault bool      `json:"is_default"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// FormNotificationStore wraps pgx for form_notifications.
type FormNotificationStore struct{ pool *pgxpool.Pool }

// NewFormNotificationStore builds a FormNotificationStore.
func NewFormNotificationStore(pool *pgxpool.Pool) *FormNotificationStore {
	return &FormNotificationStore{pool: pool}
}

// ListByForm returns a form's notification rules, newest first.
func (s *FormNotificationStore) ListByForm(ctx context.Context, formID string) ([]*FormNotification, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, form_id, name, event, COALESCE(to_email,''), COALESCE(subject,''),
		        COALESCE(body,''), condition, enabled, is_default, created_at, updated_at
		   FROM form_notifications WHERE form_id=$1 ORDER BY created_at ASC`, formID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*FormNotification
	for rows.Next() {
		n, serr := scanNotification(rows)
		if serr != nil {
			return nil, serr
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ListByFormEvent returns a form's enabled notification rules for one event.
// Used by the notification engine at send time.
func (s *FormNotificationStore) ListByFormEvent(ctx context.Context, formID, event string) ([]*FormNotification, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, form_id, name, event, COALESCE(to_email,''), COALESCE(subject,''),
		        COALESCE(body,''), condition, enabled, is_default, created_at, updated_at
		   FROM form_notifications WHERE form_id=$1 AND event=$2 AND enabled=true
		   ORDER BY created_at ASC`, formID, event)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*FormNotification
	for rows.Next() {
		n, serr := scanNotification(rows)
		if serr != nil {
			return nil, serr
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func scanNotification(row interface{ Scan(...any) error }) (*FormNotification, error) {
	n := &FormNotification{}
	var cond []byte
	err := row.Scan(&n.ID, &n.FormID, &n.Name, &n.Event, &n.ToEmail, &n.Subject,
		&n.Body, &cond, &n.Enabled, &n.IsDefault, &n.CreatedAt, &n.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if len(cond) > 0 {
		var c NotificationCondition
		if json.Unmarshal(cond, &c) == nil {
			n.Condition = &c
		}
	}
	return n, nil
}

// Create inserts a notification rule.
func (s *FormNotificationStore) Create(ctx context.Context, n *FormNotification) error {
	var cond []byte
	if n.Condition != nil {
		cond, _ = json.Marshal(n.Condition)
	}
	return s.pool.QueryRow(ctx,
		`INSERT INTO form_notifications (form_id, name, event, to_email, subject, body, condition, enabled, is_default)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		 RETURNING id, created_at, updated_at`,
		n.FormID, n.Name, n.Event, n.ToEmail, n.Subject, n.Body, cond, n.Enabled, n.IsDefault,
	).Scan(&n.ID, &n.CreatedAt, &n.UpdatedAt)
}

// Update mutates a notification rule (scoped to form so a caller can't touch
// another form's rule).
func (s *FormNotificationStore) Update(ctx context.Context, formID, id string, n *FormNotification) error {
	var cond []byte
	if n.Condition != nil {
		cond, _ = json.Marshal(n.Condition)
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE form_notifications SET name=$3, event=$4, to_email=$5, subject=$6, body=$7,
		        condition=$8, enabled=$9, updated_at=now()
		 WHERE id=$1 AND form_id=$2`,
		id, formID, n.Name, n.Event, n.ToEmail, n.Subject, n.Body, cond, n.Enabled)
	return err
}

// Delete removes a notification rule (scoped to form).
func (s *FormNotificationStore) Delete(ctx context.Context, formID, id string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM form_notifications WHERE id=$1 AND form_id=$2`, id, formID)
	return err
}

// EnsureDefault pre-creates the default notification (notify merchant on
// sale.paid) for a form when it has none. Called on form creation so a new
// form gets the workspace-default behaviour with zero configuration. A form whose
// merchant deleted every notification keeps zero (opt-out).
func (s *FormNotificationStore) EnsureDefault(ctx context.Context, formID string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO form_notifications (form_id, name, event, to_email, subject, body, is_default)
		 SELECT $1, 'Admin Notification', 'sale.paid', '{admin_email}',
		        'Jualan baharu — {{ref_code}}',
		        'Jualan baharu diterima!

Rujukan : {{ref_code}}
Jumlah  : RM {{amount}}
Item    : {{items}}
Pembeli : {{buyer_name}}
Borang  : {{form_url}}',
		        true
		 WHERE NOT EXISTS (SELECT 1 FROM form_notifications WHERE form_id=$1)`,
		formID)
	return err
}
