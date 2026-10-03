package dbstore

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Subscription lifecycle values.
const (
	SubActive    = "active"
	SubPastDue   = "past_due"
	SubCanceled  = "cancelled"
	SubCompleted = "completed"

	ChargePending = "pending"
	ChargePaid    = "paid"
	ChargeFailed  = "failed"
)

// ErrNoDueSubs returned when the due-subscriptions scan finds nothing.
var ErrNoDueSubs = errors.New("no due subscriptions")

// ErrNotPastDue is returned by Reactivate when the subscription is not in the
// past_due state, so there is nothing to revive (round 13c). Cancelled and
// completed subscriptions are deliberately not resurrectable.
var ErrNotPastDue = errors.New("langganan bukan dalam keadaan past_due")

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
	// Issue #623: the product variation this subscription was bought as (empty
	// for a simple-product subscription). The recurring engine resolves its
	// fixed billing count and cycle through the variation when set, so a
	// variation's own terms keep governing every later charge — not just the
	// first payment. Empty = no variation, the product's cycle fully applies.
	VariationID string
	// Issue #154: update-card flow. CardUpdatePending is set when a charge
	// fails so the merchant/customer knows to re-tokenize; while true the
	// 7-day overdue auto-cancel is paused. CardUpdatePurchaseID holds the
	// CHIP token purchase created by the update-card flow; on payment it
	// atomically replaces ChipTokenPurchaseID.
	CardUpdatePending    bool
	CardUpdatePurchaseID string
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
	card_update_pending, card_update_purchase_id, COALESCE(variation_id::text,'')
	FROM subscriptions`

func scanSub(row pgx.Row) (*Subscription, error) {
	sub := &Subscription{}
	err := row.Scan(&sub.ID, &sub.AccountID, &sub.ClientEmail, &sub.ProductID, &sub.SaleID,
		&sub.ChipTokenPurchaseID, &sub.Status, &sub.Period, &sub.AmountSen, &sub.NextChargeAt,
		&sub.CancelledAt, &sub.CreatedAt, &sub.UpdatedAt,
		&sub.CardUpdatePending, &sub.CardUpdatePurchaseID, &sub.VariationID)
	return sub, err
}

// Create records a subscription after the first (token-creating) payment.
func (s *SubscriptionStore) Create(ctx context.Context, sub *Subscription) error {
	return s.pool.QueryRow(ctx,
		`INSERT INTO subscriptions (account_id, client_email, product_id, sale_id, chip_token_purchase_id,
		   status, period, amount_sen, next_charge_at, variation_id)
		 VALUES ($1,$2,$3,NULLIF($4,'')::uuid,$5,$6,$7,$8,$9,NULLIF($10,'')::uuid)
		 RETURNING id, created_at, updated_at`,
		sub.AccountID, sub.ClientEmail, sub.ProductID, sub.SaleID, sub.ChipTokenPurchaseID,
		sub.Status, sub.Period, sub.AmountSen, sub.NextChargeAt, sub.VariationID,
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
// DueActive returns the subscriptions of LIVE workspaces that are due to be
// charged.
//
// WHY THE WORKSPACE GUARD IS HERE AND NOT ON STATUS (card t_3f895712). Binning a
// workspace marks `workspaces.deleted_at`; it does NOT touch the subscription,
// which stays `active` with its `next_charge_at` in the past. Nothing on the
// billing path could see that state, because until this predicate there was no
// route from a workspace to its subscription — the only edge was
// `subscriptions.account_id → accounts`, and the account hangs off the workspace.
// So the scheduler kept charging a customer for a workspace the user had
// "deleted": a ghost charge.
//
// Wan's instruction is "hentikan sahaja" — STOP BILLING, not cancel. That is why
// the status column is deliberately NOT written here and the paused subscription
// is left intact: Restore brings the workspace back with `deleted_at = NULL` and
// this predicate matches again, so billing RESUMES by itself. Marking the
// subscription cancelled/paused instead would need a compensating write on
// Restore, and every way of doing that can disagree with the trash state it is
// meant to mirror. One predicate over the live state cannot drift from it.
//
// WHY `NOT EXISTS (… trashed …)` AND NOT `EXISTS (… live …)`. The positive form —
// "only charge when the account has a LIVE workspace" — is the one that reads
// naturally and it is WRONG: it also stops billing accounts with **no workspace at
// all**, which is not a trashed workspace, it is the pre-workspace era. Measured:
// the positive form turned `payments` red
// (`TestRound13bFixedCountCollectsExactlyNThenStops`,
// `TestIssue562FailedChargeRecordsTheGatewayReason`), whose fixture seeds an
// account with a NULL `workspace_id` — the same shape as the 6 orphan accounts
// the design note counts on prod. Those accounts still bill today, and a fix for
// the ghost charge has no business changing that. So the guard names the ONE state
// it is about: an account whose workspace is in the bin. An account with no
// workspace has no trash state to be in, and is left exactly as it was.
func (s *SubscriptionStore) DueActive(ctx context.Context, now time.Time, limit int) ([]*Subscription, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx,
		subSelect+` WHERE status = 'active' AND next_charge_at <= $1
		              AND NOT EXISTS (
		                SELECT 1 FROM accounts a
		                JOIN workspaces w ON w.id = a.workspace_id
		                 WHERE a.id = subscriptions.account_id
		                   AND w.deleted_at IS NOT NULL)
		            ORDER BY next_charge_at LIMIT $2`,
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

// ClaimDue atomically takes ownership of a due subscription before a charge is
// attempted, and reports whether THIS caller won the claim.
//
// Issue #534: a due subscription used to be charged again on the next sweep while
// its previous charge was still pending, because next_charge_at is only advanced
// by the purchase.paid callback and nothing leased the subscription in between.
// The customer's card was charged twice for one period.
//
// The claim is a conditional UPDATE whose WHERE clause carries the guard, so it is
// correct under concurrency (the database serialises competing claimants and
// RowsAffected reports exactly one winner) and needs no application-level lock.
// Advancing next_charge_at IS the lease: it both removes the subscription from the
// due window and records when the lease expires.
//
//   - A successful charge later corrects next_charge_at to the true period via
//     ScheduleNextCharge.
//   - A failed charge shortens it to the retry backoff.
//   - If the process dies mid-attempt, the lease (not the guard) is what expires,
//     so a crashed attempt is retried after leaseFor rather than never.
func (s *SubscriptionStore) ClaimDue(ctx context.Context, subID string, leaseFor time.Duration) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE subscriptions
		    SET next_charge_at = now() + $2::interval, updated_at = now()
		  WHERE id = $1
		    AND status = 'active'
		    AND next_charge_at <= now()
		    AND NOT EXISTS (
		          SELECT 1 FROM subscription_charges c
		           WHERE c.subscription_id = $1 AND c.status = 'pending')`,
		subID, leaseDurationSQL(leaseFor))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// leaseDurationSQL renders a duration as a Postgres interval literal, avoiding a
// dependency on pgtype for a single parameter.
func leaseDurationSQL(d time.Duration) string {
	if d <= 0 {
		d = time.Hour
	}
	return strconv.FormatInt(int64(d/time.Second), 10) + " seconds"
}

// HasPendingCharge reports whether the subscription has an outstanding charge.
// The claim above already refuses such a subscription, so this is a read-only
// companion for callers that want to explain why a claim was skipped.
func (s *SubscriptionStore) HasPendingCharge(ctx context.Context, subID string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM subscription_charges
		                WHERE subscription_id=$1 AND status='pending')`, subID).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
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
//
// Issue #535: this used to read a `created_at` column that does not exist on
// subscription_charges (the column is `charged_at`), and discarded the resulting
// error with `_ =`. It therefore returned 0 forever, so the caller's
// `attempt := FailedAttempts(...) + 1` was always 1 and the retry ladder never
// reached maxChargeAttempts — leaving past_due, the card-update prompt and the
// overdue auto-cancel unreachable. The error is now returned, so a future
// mismatch fails loudly instead of silently disarming the ladder.
func (s *SubscriptionStore) FailedAttempts(ctx context.Context, subID string) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM subscription_charges
		 WHERE subscription_id=$1 AND status='failed'
		   AND charged_at > COALESCE((SELECT MAX(updated_at) FROM subscription_charges
		                              WHERE subscription_id=$1 AND status='paid'), 'epoch')`,
		subID).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// ChargeCount returns the number of paid charges recorded for a subscription.
// This is the "charge count reached" used to enforce a fixed billing count
// (subscription_charges = N stops after the Nth paid charge).
//
// Issue #535: like FailedAttempts, this used to discard its query error and
// return 0. Here a swallowed error is not benign: 0 means "no charges consumed
// yet", so a fixed-billing subscription would keep charging past its Nth charge
// instead of completing. The error is returned so callers can fail safely.
func (s *SubscriptionStore) ChargeCount(ctx context.Context, subID string) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM subscription_charges
		 WHERE subscription_id=$1 AND status='paid'`, subID).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// SubByChargePurchase is ByChargePurchase under the name the payment-callback
// key resolver uses (issue #564). A recurring charge exists only as a
// subscription_charges row — never a sale — so the callback must be able to
// resolve the owning account from it, or every renewal is unverifiable.
func (s *SubscriptionStore) SubByChargePurchase(ctx context.Context, purchaseID string) (*Subscription, error) {
	return s.ByChargePurchase(ctx, purchaseID)
}

// ByChargePurchase finds the subscription owning a child charge purchase id
// (a recurring charge, distinct from the token-creating purchase). Used by
// the callback to advance the charge count / mark completion after a paid
// recurring charge.
func (s *SubscriptionStore) ByChargePurchase(ctx context.Context, purchaseID string) (*Subscription, error) {
	return scanSub(s.pool.QueryRow(ctx,
		subSelect+` WHERE id = (SELECT subscription_id FROM subscription_charges WHERE chip_purchase_id = $1)`,
		purchaseID))
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
			s.card_update_pending, s.card_update_purchase_id, COALESCE(s.variation_id::text,''),
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
			s.card_update_pending, s.card_update_purchase_id, COALESCE(s.variation_id::text,''),
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
			&sub.CardUpdatePending, &sub.CardUpdatePurchaseID, &sub.VariationID,
			&sw.ProductName, &sw.RetryAttempts); err != nil {
			return nil, 0, err
		}
		sw.Subscription = sub
		out = append(out, sw)
	}
	return out, total, rows.Err()
}

// ByIDForAccount returns a subscription by id, but only when it belongs to
// the given account (used by per-subscription mutations to keep them
// account-scoped).
// A miss — no such subscription, another account's, or an id that cannot be a
// UUID at all (Postgres raises SQLSTATE 22P02) — is pgx.ErrNoRows, the
// not-found signal this store already uses and its callers already map to 404.
// Without it the malformed-id shape escaped as a driver error and the routes
// that compare errors.Is(err, pgx.ErrNoRows) fell through to a 500 (see
// isNoSuchRow).
func (s *SubscriptionStore) ByIDForAccount(ctx context.Context, accountID, subID string) (*Subscription, error) {
	sub, err := scanSub(s.pool.QueryRow(ctx, subSelect+" WHERE id=$1 AND account_id=$2", subID, accountID))
	if isNoSuchRow(err) {
		return nil, pgx.ErrNoRows
	}
	return sub, err
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

// ── Round 13c (Wan): subscription recovery ───────────────────────────────────

// LastPaidChargeAt returns when the most recent PAID charge row for this
// subscription was created (its lease claim). Used to tell an arrears payment
// from an on-time one when computing a fixed billing schedule.
func (s *SubscriptionStore) LastPaidChargeAt(ctx context.Context, subID string) (time.Time, error) {
	var t time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(MAX(charged_at), 'epoch'::timestamptz)
		   FROM subscription_charges WHERE subscription_id=$1 AND status='paid'`, subID).Scan(&t)
	if err != nil {
		return time.Time{}, err
	}
	return t, nil
}

// ReactivateOnPayment returns a past_due subscription to active once a charge
// for it has PAID, and clears card_update_pending.
//
// Round 13c (Wan): a subscription that failed its 3 attempts became past_due and
// could only ever leave that state through a completed card update — so a
// customer who paid (or whose card was fixed) left the subscription stuck and
// one grace-window expiry away from auto-cancel. A settled payment is proof the
// card works, so the subscription resumes.
//
// Scoped by purchase id and guarded on status so it is a no-op for a
// subscription that is already active, completed or cancelled — it must never
// resurrect a cancelled subscription, and it must not undo a completion.
func (s *SubscriptionStore) ReactivateOnPayment(ctx context.Context, chipPurchaseID string) error {
	if chipPurchaseID == "" {
		return nil
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE subscriptions
		    SET status='active', card_update_pending=FALSE, card_update_purchase_id='', updated_at=now()
		  WHERE status = 'past_due'
		    AND id = (SELECT subscription_id FROM subscription_charges WHERE chip_purchase_id=$1)`,
		chipPurchaseID)
	return err
}

// Reactivate returns a past_due subscription to active WITHOUT a payment, and
// clears card_update_pending. This is the merchant/admin "retry this
// subscription now" action: it re-arms the subscription so the next sweep
// charges it again with the (possibly updated) card token.
//
// Only past_due is eligible. A cancelled subscription stays cancelled, and a
// completed one is a finished fixed-term plan — neither is resurrectable.
func (s *SubscriptionStore) Reactivate(ctx context.Context, accountID, subID string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE subscriptions
		    SET status='active', card_update_pending=FALSE, card_update_purchase_id='', updated_at=now()
		  WHERE id=$1 AND account_id=$2 AND status='past_due'`,
		subID, accountID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotPastDue
	}
	return nil
}

// AddChargeFailedWithReason records a failed recurring charge together with the
// gateway's reason (issue #562).
//
// The plain AddCharge left the cause unknown — the engine discarded the CHIP
// error — so a merchant could not tell an expired card from an invalid recurring
// token from a gateway outage. The reason is truncated to keep a pathological
// upstream message from bloating the row.
func (s *SubscriptionStore) AddChargeFailedWithReason(ctx context.Context, subID string, amountSen int64, attempt int, reason string) error {
	if len(reason) > 500 {
		reason = reason[:500]
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO subscription_charges (subscription_id, chip_purchase_id, amount_sen, status, attempt, failure_reason)
		 VALUES ($1, NULL, $2, 'failed', $3, NULLIF($4,''))`,
		subID, amountSen, attempt, reason)
	return err
}

// FailStalePendingCharges marks pending charge rows older than the cutoff as
// failed, and returns how many it settled (issue #568).
//
// A charge row is created 'pending' and is resolved ONLY by the CHIP callback
// (MarkChargePaid / MarkChargeFailed). afterFailedCharge — the retry ladder — is
// reached only when Chip.Charge returns an error, i.e. when the gateway rejects
// the attempt synchronously. So a charge CHIP accepted whose callback is later
// lost, undelivered, or rejected stays 'pending' FOREVER.
//
// That is not a cosmetic problem: ClaimDue refuses to charge a subscription that
// has any pending row, so one lost callback silently disables the subscription
// for good. It still reads 'active', the engine finds it due, the claim is
// declined, and nothing surfaces the reason. The merchant has no action that
// clears it (retry_subscription only revives past_due).
//
// Ageing the row out restores the normal path: the charge is recorded as failed
// with the reason, so it is visible to the merchant, and the subscription's retry
// ladder takes over on the next sweep.
func (s *SubscriptionStore) FailStalePendingCharges(ctx context.Context, olderThan time.Time, reason string) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE subscription_charges
		    SET status='failed', failure_reason=$2, updated_at=now()
		  WHERE status='pending' AND charged_at < $1`,
		olderThan, reason)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
