package dbstore

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Subscription lifecycle values.
const (
	SubActive   = "active"
	SubPastDue  = "past_due"
	SubCanceled = "cancelled"

	ChargePending = "pending"
	ChargePaid    = "paid"
	ChargeFailed  = "failed"
)

// ErrNoDueSubs returned when the due-subscriptions scan finds nothing.
var ErrNoDueSubs = errors.New("no due subscriptions")

// Subscription models the subscriptions table.
type Subscription struct {
	ID                  string
	AccountID           string
	ClientEmail         string
	ProductID           string
	SaleID              string
	ChipTokenPurchaseID string
	Status              string
	Period              string
	AmountSen           int64
	NextChargeAt        time.Time
	CancelledAt         time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
	// Issue #154: update-card flow. CardUpdatePending is set when a charge
	// fails so the merchant/customer knows to re-tokenize; while true the
	// 7-day overdue auto-cancel is paused. CardUpdatePurchaseID holds the
	// CHIP token purchase created by the update-card flow; on payment it
	// atomically replaces ChipTokenPurchaseID.
	CardUpdatePending     bool
	CardUpdatePurchaseID  string
}

// SubscriptionStore wraps pgx for subscriptions.
type SubscriptionStore struct{ pool *pgxpool.Pool }

// NewSubscriptionStore builds a SubscriptionStore.
func NewSubscriptionStore(pool *pgxpool.Pool) *SubscriptionStore {
	return &SubscriptionStore{pool: pool}
}

const subSelect = `SELECT id, account_id, client_email, product_id, COALESCE(sale_id::text,''),
	chip_token_purchase_id, status, period, amount_sen, next_charge_at,
	COALESCE(cancelled_at, 'epoch'::timestamptz), created_at, updated_at,
	card_update_pending, card_update_purchase_id
	FROM subscriptions`

func scanSub(row pgx.Row) (*Subscription, error) {
	sub := &Subscription{}
	err := row.Scan(&sub.ID, &sub.AccountID, &sub.ClientEmail, &sub.ProductID, &sub.SaleID,
		&sub.ChipTokenPurchaseID, &sub.Status, &sub.Period, &sub.AmountSen, &sub.NextChargeAt,
		&sub.CancelledAt, &sub.CreatedAt, &sub.UpdatedAt,
		&sub.CardUpdatePending, &sub.CardUpdatePurchaseID)
	return sub, err
}

// Create records a subscription after the first (token-creating) payment.
func (s *SubscriptionStore) Create(ctx context.Context, sub *Subscription) error {
	return s.pool.QueryRow(ctx,
		`INSERT INTO subscriptions (account_id, client_email, product_id, sale_id, chip_token_purchase_id,
		   status, period, amount_sen, next_charge_at)
		 VALUES ($1,$2,$3,NULLIF($4,'')::uuid,$5,$6,$7,$8,$9)
		 RETURNING id, created_at, updated_at`,
		sub.AccountID, sub.ClientEmail, sub.ProductID, sub.SaleID, sub.ChipTokenPurchaseID,
		sub.Status, sub.Period, sub.AmountSen, sub.NextChargeAt,
	).Scan(&sub.ID, &sub.CreatedAt, &sub.UpdatedAt)
}

// ByTokenPurchase finds the subscription owning a token purchase id.
func (s *SubscriptionStore) ByTokenPurchase(ctx context.Context, purchaseID string) (*Subscription, error) {
	return scanSub(s.pool.QueryRow(ctx, subSelect+" WHERE chip_token_purchase_id = $1", purchaseID))
}

// ByEmail finds the (single) live subscription for a client email.
func (s *SubscriptionStore) ByEmail(ctx context.Context, accountID, email string) (*Subscription, error) {
	return scanSub(s.pool.QueryRow(ctx,
		subSelect+" WHERE account_id=$1 AND client_email=$2 AND status IN ('active','past_due') ORDER BY created_at DESC LIMIT 1",
		accountID, email))
}

// DueActive lists active subscriptions whose next_charge_at has passed.
func (s *SubscriptionStore) DueActive(ctx context.Context, now time.Time, limit int) ([]*Subscription, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx,
		subSelect+" WHERE status = 'active' AND next_charge_at <= $1 ORDER BY next_charge_at LIMIT $2",
		now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Subscription
	for rows.Next() {
		sub, err := scanSub(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

// ScheduleNextCharge advances next_charge_at by the period.
func (s *SubscriptionStore) ScheduleNextCharge(ctx context.Context, subID string, next time.Time) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE subscriptions SET next_charge_at=$2, updated_at=now() WHERE id=$1`, subID, next)
	return err
}

// SetStatus transitions subscription status.
func (s *SubscriptionStore) SetStatus(ctx context.Context, subID, status string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE subscriptions SET status=$2, updated_at=now() WHERE id=$1`, subID, status)
	return err
}

// Cancel marks the subscription cancelled.
func (s *SubscriptionStore) Cancel(ctx context.Context, subID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE subscriptions SET status='cancelled', cancelled_at=now(), updated_at=now() WHERE id=$1`, subID)
	return err
}

// AddCharge records a subscription charge attempt.
func (s *SubscriptionStore) AddCharge(ctx context.Context, subID, chipPurchaseID string, amountSen int64, status string, attempt int) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO subscription_charges (subscription_id, chip_purchase_id, amount_sen, status, attempt)
		 VALUES ($1, NULLIF($2,''), $3, $4, $5)`,
		subID, chipPurchaseID, amountSen, status, attempt)
	return err
}

// MarkChargePaid updates a charge record by CHIP purchase id.
func (s *SubscriptionStore) MarkChargePaid(ctx context.Context, chipPurchaseID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE subscription_charges SET status='paid', updated_at=now() WHERE chip_purchase_id=$1`, chipPurchaseID)
	return err
}

// MarkChargeFailed likewise.
func (s *SubscriptionStore) MarkChargeFailed(ctx context.Context, chipPurchaseID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE subscription_charges SET status='failed', updated_at=now() WHERE chip_purchase_id=$1`, chipPurchaseID)
	return err
}

// FailedAttempts counts failed charges for the current retry cycle.
func (s *SubscriptionStore) FailedAttempts(ctx context.Context, subID string) int64 {
	var n int64
	_ = s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM subscription_charges
		 WHERE subscription_id=$1 AND status='failed'
		   AND created_at > COALESCE((SELECT MAX(updated_at) FROM subscription_charges
		                             WHERE subscription_id=$1 AND status='paid'), 'epoch')`,
		subID).Scan(&n)
	return n
}

// PastDueBefore lists past_due subscriptions since before the cutoff.
func (s *SubscriptionStore) PastDueBefore(ctx context.Context, cutoff time.Time) ([]*Subscription, error) {
	rows, err := s.pool.Query(ctx,
		subSelect+" WHERE status=$1 AND next_charge_at < $2", SubPastDue, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Subscription
	for rows.Next() {
		sub, err := scanSub(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

// SubscriptionWithProduct is a subscription joined with its product's name,
// for merchant-facing table/list views.
type SubscriptionWithProduct struct {
	Subscription
	ProductName   string
	RetryAttempts int64 // failed charges in the current retry cycle (0 when none)
}

// ListByAccountsPage returns one page of subscriptions across a set of
// accounts, newest first, plus the total row count for the same filter.
// status limits to a single lifecycle status when non-empty (active /
// past_due / cancelled); otherwise all statuses are returned.
func (s *SubscriptionStore) ListByAccountsPage(ctx context.Context, accountIDs []string, status string, page, limit int) ([]*SubscriptionWithProduct, int64, error) {
	if len(accountIDs) == 0 {
		return []*SubscriptionWithProduct{}, 0, nil
	}
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	offset := (page - 1) * limit
	where := " WHERE s.account_id = ANY($1)"
	args := []any{accountIDs}
	if status != "" {
		where += " AND s.status = $2"
		args = append(args, status)
	}
	countQ := "SELECT COUNT(*) FROM subscriptions s" + where
	var total int64
	if err := s.pool.QueryRow(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	// The list must expose the product name next to each subscription, so a
	// merchant can act on a row without joining the product table by hand.
	// Placeholders: when filter=="" they are $1 account, $2 limit, $3 offset;
	// when filter!="" they are $1 account, $2 status, $3 limit, $4 offset.
	var q string
	if status != "" {
		q = `SELECT s.id, s.account_id, s.client_email, s.product_id, COALESCE(s.sale_id::text,''),
			s.chip_token_purchase_id, s.status, s.period, s.amount_sen, s.next_charge_at,
			COALESCE(s.cancelled_at, 'epoch'::timestamptz), s.created_at, s.updated_at,
			s.card_update_pending, s.card_update_purchase_id,
			COALESCE(p.name, ''),
			COALESCE((SELECT COUNT(*) FROM subscription_charges sc
			          WHERE sc.subscription_id = s.id AND sc.status = 'failed'
			            AND sc.charged_at > COALESCE((SELECT MAX(sc2.charged_at) FROM subscription_charges sc2
			                                        WHERE sc2.subscription_id = s.id AND sc2.status = 'paid'), 'epoch'::timestamptz)), 0)
		FROM subscriptions s
		LEFT JOIN products p ON p.id = s.product_id` + where + " ORDER BY s.created_at DESC LIMIT $3 OFFSET $4"
		args = append(args, limit, offset)
	} else {
		q = `SELECT s.id, s.account_id, s.client_email, s.product_id, COALESCE(s.sale_id::text,''),
			s.chip_token_purchase_id, s.status, s.period, s.amount_sen, s.next_charge_at,
			COALESCE(s.cancelled_at, 'epoch'::timestamptz), s.created_at, s.updated_at,
			s.card_update_pending, s.card_update_purchase_id,
			COALESCE(p.name, ''),
			COALESCE((SELECT COUNT(*) FROM subscription_charges sc
			          WHERE sc.subscription_id = s.id AND sc.status = 'failed'
			            AND sc.charged_at > COALESCE((SELECT MAX(sc2.charged_at) FROM subscription_charges sc2
			                                        WHERE sc2.subscription_id = s.id AND sc2.status = 'paid'), 'epoch'::timestamptz)), 0)
		FROM subscriptions s
		LEFT JOIN products p ON p.id = s.product_id` + where + " ORDER BY s.created_at DESC LIMIT $2 OFFSET $3"
		args = append(args, limit, offset)
	}
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []*SubscriptionWithProduct{}
	for rows.Next() {
		sw := &SubscriptionWithProduct{}
		var sub Subscription
		if err := rows.Scan(&sub.ID, &sub.AccountID, &sub.ClientEmail, &sub.ProductID, &sub.SaleID,
			&sub.ChipTokenPurchaseID, &sub.Status, &sub.Period, &sub.AmountSen, &sub.NextChargeAt,
			&sub.CancelledAt, &sub.CreatedAt, &sub.UpdatedAt,
			&sub.CardUpdatePending, &sub.CardUpdatePurchaseID, &sw.ProductName, &sw.RetryAttempts); err != nil {
			return nil, 0, err
		}
		sw.Subscription = sub
		out = append(out, sw)
	}
	return out, total, rows.Err()
}

// ByIDForAccount returns a subscription by id, but only when it belongs to
// the given account (used by per-subscription mutations to keep them
// account-scoped). Returns pgx.ErrNoRows when not owned.
func (s *SubscriptionStore) ByIDForAccount(ctx context.Context, accountID, subID string) (*Subscription, error) {
	return scanSub(s.pool.QueryRow(ctx, subSelect+" WHERE id=$1 AND account_id=$2", subID, accountID))
}

// UpdateToken atomically replaces the recurring token on a subscription,
// scoped to the owning account. The old token is left in CHIP (Wan rule —
// never auto-revoke) but is no longer used for charges. Clears the
// card-update-pending flag and the pending purchase id, and returns the
// subscription to active so the schedule continues without restarting the
// billing period. Returns pgx.ErrNoRows when the subscription is not owned.
func (s *SubscriptionStore) UpdateToken(ctx context.Context, accountID, subID, newTokenPurchaseID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE subscriptions
		   SET chip_token_purchase_id=$3, card_update_pending=FALSE,
		       card_update_purchase_id='', status='active', updated_at=now()
		 WHERE id=$1 AND account_id=$2`,
		subID, accountID, newTokenPurchaseID)
	return err
}

// SetCardUpdatePending flags a subscription as needing a card update (set
// when a charge fails) and records the pending re-tokenize purchase id.
// While pending, the overdue auto-cancel is paused.
func (s *SubscriptionStore) SetCardUpdatePending(ctx context.Context, subID, pendingPurchaseID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE subscriptions
		   SET card_update_pending=TRUE, card_update_purchase_id=$2, updated_at=now()
		 WHERE id=$1`,
		subID, pendingPurchaseID)
	return err
}

// ClearCardUpdatePending clears the card-update-pending flag (e.g. after a
// successful re-tokenize or when the merchant cancels the update).
func (s *SubscriptionStore) ClearCardUpdatePending(ctx context.Context, subID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE subscriptions
		   SET card_update_pending=FALSE, card_update_purchase_id='', updated_at=now()
		 WHERE id=$1`,
		subID)
	return err
}

// ByCardUpdatePurchase finds the subscription awaiting a card update for a
// given pending token purchase id (used by the callback to complete the swap).
func (s *SubscriptionStore) ByCardUpdatePurchase(ctx context.Context, purchaseID string) (*Subscription, error) {
	return scanSub(s.pool.QueryRow(ctx,
		subSelect+" WHERE card_update_purchase_id = $1 AND card_update_pending = TRUE", purchaseID))
}

// PendingCardUpdate lists subscriptions flagged as needing a card update
// (used to surface the "update payment method" state and to pause the
// overdue auto-cancel while an update is pending).
func (s *SubscriptionStore) PendingCardUpdate(ctx context.Context) ([]*Subscription, error) {
	rows, err := s.pool.Query(ctx, subSelect+" WHERE card_update_pending = TRUE ORDER BY updated_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Subscription
	for rows.Next() {
		sub, err := scanSub(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}
