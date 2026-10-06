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

	"github.com/wzul/jomform/api/internal/emailtext"
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
	ID     string `json:"id"`
	FormID string `json:"form_id"`
	Name   string `json:"name"`
	Event  string `json:"event"`
	// ToEmail is the recipient: a literal address, `{admin_email}` (the
	// merchant's account email, resolved at send time), or `{field_id}` (the
	// submitted value of a form field, e.g. a buyer email field).
	ToEmail string `json:"to_email"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
	// Condition is the optional conditional logic; nil = always fire on event.
	Condition *NotificationCondition `json:"condition,omitempty"`
	Enabled   bool                   `json:"enabled"`
	// IsDefault marks the pre-created default notification: the merchant is
	// notified of a new sale (payment form) or a new response (response form).
	// The merchant can edit or delete it per form.
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

// Default notification content, by form mode.
//
// A PAYMENT form records a sale, so its default rule keys off sale.paid and can
// use the sale placeholders (ref_code / amount / items / buyer_name).
//
// The templates come from the shared transactional dictionary rather than being
// spelled out here. They were duplicated before: the same Malay literal lived in
// this file AND in that dictionary, and the dictionary's job is to recognise a
// never-customised rule by comparing the stored value against it. Two copies
// drift silently — one added space and the comparison stops matching, so a
// Malay receipt goes on being mailed to an English buyer while every test still
// passes. One definition, one writer, one recogniser.
var (
	defaultSaleNotificationSubject, defaultSaleNotificationBody         = emailtext.LegacyDefault(emailtext.NotificationSale)
	defaultResponseNotificationSubject, defaultResponseNotificationBody = emailtext.LegacyDefault(emailtext.NotificationResponse)
)

// The notification NAME is not email content — it is a label in the dashboard's
// rule list — so it stays here.
const (
	defaultSaleNotificationName     = "Admin Notification"
	defaultResponseNotificationName = "Admin Notification"
)

// defaultNotificationRecipient is the recipient EVERY default rule carries: the
// merchant's account email, resolved at send time (issue #606). ONE definition,
// because it is also what ReconcileDefault compares a stored rule against to
// tell a default it may repair from a rule the merchant re-addressed.
const defaultNotificationRecipient = "{admin_email}"

// A RESPONSE form never records a sale — it records a submission — so a
// sale.paid rule on it can never fire, and a sale-flavoured message would be
// meaningless anyway: a response has no reference code, no amount and no items,
// so {{ref_code}}/{{amount}}/{{items}}/{{buyer_name}} would all render empty
// ({{amount}} as a misleading "0.00"). The response default therefore uses only
// placeholders that carry a real value for a submission: the responder's
// identity, which the public submit path always captures on the fields map
// (`full_name` / `email`), plus {{date}} and {{form_url}}. Its template, like
// the sale default above, comes from the shared dictionary.
//
// defaultNotificationFor returns the name, event, subject and body of the
// default notification rule for a form in the given mode.
func defaultNotificationFor(mode string) (name, event, subject, body string) {
	if mode == FormModeResponse {
		return defaultResponseNotificationName, NotifyEventResponseSubmitted,
			defaultResponseNotificationSubject, defaultResponseNotificationBody
	}
	return defaultSaleNotificationName, NotifyEventSalePaid,
		defaultSaleNotificationSubject, defaultSaleNotificationBody
}

// DefaultNotification exposes the default notification rule for a form mode:
// its name, event, subject template and body template. EnsureDefault creates
// exactly this rule, so a caller that needs to know what a new form will get —
// the migration that repaired response forms in place (issue #597) and the tests
// that pin both modes — reads it from the one definition rather than a copy.
func DefaultNotification(mode string) (name, event, subject, body string) {
	return defaultNotificationFor(mode)
}

// EnsureDefault pre-creates the default notification for a form when it has
// none. Called on form creation so a new form gets a working notification with
// zero configuration. A form whose merchant deleted every notification keeps
// zero (opt-out).
//
// The rule FOLLOWS THE FORM'S MODE (issue #597) and the form carries its own
// mode in `config.mode`, so it takes the form rather than a bare id. Before
// this, the event was hardcoded `sale.paid` for EVERY form: a response form
// (an RSVP, an event registration, a contact form) therefore got a rule that
// was created, enabled, and could never fire — the merchant was never told
// anyone had responded. Payment forms keep the sale.paid rule unchanged.
func (s *FormNotificationStore) EnsureDefault(ctx context.Context, f *Form) error {
	if f == nil || f.ID == "" {
		return nil
	}
	name, event, subject, body := defaultNotificationFor(FormMode(f))
	_, err := s.pool.Exec(ctx,
		`INSERT INTO form_notifications (form_id, name, event, to_email, subject, body, is_default)
		 SELECT $1, $2, $3, $4, $5, $6, true
		 WHERE NOT EXISTS (SELECT 1 FROM form_notifications WHERE form_id=$1)`,
		f.ID, name, event, defaultNotificationRecipient, subject, body)
	return err
}

// ReconcileDefault re-points a form's UNEDITED default notification rule onto
// the event its CURRENT mode fires, and returns whether it changed the row.
//
// THE DEFECT THIS CLOSES (carded from t_f37d710d, the mode-change half of
// #597). EnsureDefault picks the rule's event from the form's mode AT INSERT
// TIME ONLY. A form that was created as a payment form and LATER switched to
// response mode — the builder's "Form mode" control, a plain PATCH through
// FormStore.Update — therefore keeps the sale.paid default it was born with,
// and FireFormNotifications' ListByFormEvent(formID, "response.submitted")
// matches zero rows: the merchant is never told anyone responded. Issue #597's
// repair migration (000062) fixed the rows that existed when it ran; it cannot
// reach a row a mode change creates afterwards.
//
// WHEN IT IS CALLED, and why that is the right seam: from FormStore.Update,
// whose only callers are the surfaces that write config/theme — REST
// PATCH /v1/rest/forms/{id}, MCP upsert_form's update path, and the
// config-merging writers (register_form_fields, set_form_theme,
// set_form_chip_mapping, the menu writer, and MCP's custom-HTML form, whose
// applyCustomFormConfig is what SETS mode=response). Putting it at the store
// seam rather than at each call site is deliberate: a caller that writes a
// mode cannot forget to reconcile, and a future write path inherits the same
// contract for free.
//
// THE SCOPE IS THE MERCHANT'S, so nothing he decided is seized. Exactly one
// shape is touched (the same contract 000062 used, so a repaired row and a row
// created after this release end up identical):
//
//   - the row must be the form's DEFAULT (`is_default`),
//   - it must still carry the system's own bytes for the event it holds —
//     its recipient, subject and body must all equal what the system wrote for
//     the OTHER mode's default (recognised through emailtext, the same
//     one-definition dictionary EnsureDefault writes from, so the two cannot
//     drift),
//   - and its event must actually be the one the form's mode can never fire.
//
// THE NAME IS NOT PART OF THAT COMPARISON, and that is deliberate (card
// `t_4f439f55`). `name` is a label in the dashboard's rule list, not a decision
// about the MESSAGE — the recipient, subject and body are — and 000062 never
// constrained it either: the migration SETS `name = 'Admin Notification'` and
// its WHERE carries no `name` predicate, so it repaired a re-labelled default
// and this reconcile has to agree with it. Comparing the name made the reconcile
// STRICTER than the migration it claims to match, and the gap was reachable: a
// merchant who renamed the rule (form settings → the rule's Name field → PUT
// .../notifications/{id}, which never sets is_default) and THEN switched the
// form's mode kept a dead `sale.paid` rule on a response form, so the merchant
// was never told anyone responded — the exact symptom this reconcile exists to
// close. A row the merchant re-labelled is still the system's rule by every
// other compared byte; the SET re-points the event and restores the system's own
// name for that event with it, so the label and the event cannot end up
// disagreeing.
//
// A merchant-authored or even partially-edited rule fails that comparison and
// is left exactly as it is — including a rule he re-addressed to his own
// inbox, which is why ToEmail is part of the comparison. A form with zero
// rules stays at zero: the opt-out contract of EnsureDefault is not reversed
// here (nothing is INSERTed), so a merchant who deleted his default rule keeps
// silence.
func (s *FormNotificationStore) ReconcileDefault(ctx context.Context, f *Form) (bool, error) {
	if f == nil || f.ID == "" {
		return false, nil
	}
	wantName, wantEvent, wantSubject, wantBody := defaultNotificationFor(FormMode(f))
	// How the system's default for the OTHER mode reads for the bytes this
	// actually compares: recipient, subject and body. The name is deliberately
	// not one of them (see THE NAME IS NOT PART OF THAT COMPARISON above); the
	// SET re-points it to wantName, so it is discarded here rather than tested.
	_, otherEvent, otherSubject, otherBody := defaultNotificationFor(otherFormMode(FormMode(f)))
	if otherEvent == wantEvent {
		return false, nil
	}
	// ONE statement, so the row that is judged is the row that is written: a
	// SELECT-then-UPDATE would leave a window in which the merchant edits his
	// rule and this pass overwrites the edit it never saw. Every predicate in
	// the WHERE is part of the merchant's-rule test, and the RETURNING makes the
	// "did it change anything" answer come from the same write.
	rows, err := s.pool.Query(ctx,
		`UPDATE form_notifications
		    SET event=$2, name=$3, subject=$4, body=$5, updated_at=now()
		  WHERE form_id=$1
		    AND is_default=true
		    AND COALESCE(to_email,'')=$6
		    AND event=$7 AND subject=$8 AND body=$9
		  RETURNING id`,
		f.ID, wantEvent, wantName, wantSubject, wantBody,
		defaultNotificationRecipient, otherEvent, otherSubject, otherBody)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	changed := false
	for rows.Next() {
		changed = true
	}
	if err := rows.Err(); err != nil {
		return changed, err
	}
	return changed, nil
}

// otherFormMode returns the mode a form is NOT in, so a caller can ask what the
// system's default reads for the mode the form has moved away from. The set of
// modes is exactly two (see form_mode.go), and the "anything not response is
// payment" default there is the reason this is a total function rather than a
// switch with a third answer.
func otherFormMode(mode string) string {
	if mode == FormModeResponse {
		return FormModePayment
	}
	return FormModeResponse
}
