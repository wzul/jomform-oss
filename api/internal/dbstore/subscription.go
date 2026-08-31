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
}

// SubscriptionStore wraps pgx for subscriptions.
type SubscriptionStore struct{ pool *pgxpool.Pool }

// NewSubscriptionStore builds a SubscriptionStore.
func NewSubscriptionStore(pool *pgxpool.Pool) *SubscriptionStore {
	return &SubscriptionStore{pool: pool}
}

const subSelect = `SELECT id, account_id, client_email, product_id, COALESCE(sale_id::text,''),
	chip_token_purchase_id, status, period, amount_sen, next_charge_at,
	COALESCE(cancelled_at, 'epoch'::timestamptz), created_at, updated_at
	FROM subscriptions`

func scanSub(row pgx.Row) (*Subscription, error) {
	sub := &Subscription{}
	err := row.Scan(&sub.ID, &sub.AccountID, &sub.ClientEmail, &sub.ProductID, &sub.SaleID,
		&sub.ChipTokenPurchaseID, &sub.Status, &sub.Period, &sub.AmountSen, &sub.NextChargeAt,
		&sub.CancelledAt, &sub.CreatedAt, &sub.UpdatedAt)
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
