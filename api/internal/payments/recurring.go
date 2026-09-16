// Subscription recurring charges + retry ladder (mirrors Woo's collector):
// attempt every 24h, up to 3 attempts → sub becomes past_due → cancel at day 7.
package payments

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/wzul/jomform/api/internal/chip"
	"github.com/wzul/jomform/api/internal/cryptoutil"
	"github.com/wzul/jomform/api/internal/dbstore"
)

const (
	maxChargeAttempts = 3
	retryEvery        = 24 * time.Hour
	cancelAfterDays   = 7
)

// RunDueCharges processes every due subscription: charges the card via the
// stored recurring token, retries per the ladder, and drives status.
// Called by the ticker loop started in main; safe for zero due subs.
func (s *Service) RunDueCharges(ctx context.Context) error {
	due, err := s.Subs.DueActive(ctx, time.Now().Add(-time.Minute), 200)
	if err != nil {
		return err
	}
	if len(due) == 0 {
		return dbstore.ErrNoDueSubs
	}
	for _, sub := range due {
		if err := s.chargeSubscription(ctx, sub); err != nil {
			slog.Warn("charge_subscription_failed", "sub", sub.ID, "err", err)
		}
	}
	return nil
}

// chargeLease is how long a claimed subscription stays out of the due window
// while its charge is in flight. It must exceed a normal gateway round-trip and
// the callback latency, so a healthy attempt is never double-charged; it only
// has to be long enough that a crashed attempt is retried later rather than
// immediately (issue #534).
const chargeLease = 1 * time.Hour

func (s *Service) chargeSubscription(ctx context.Context, sub *dbstore.Subscription) error {
	// Issue #152: a fixed billing count (subscription_charges = N) stops the
	// subscription after the Nth charge. The initial token purchase is always
	// charge #1, so total charges consumed = 1 + paid recurring charges. When
	// the limit is reached, mark the subscription completed (removes it from
	// the due set — no further charges scheduled) and skip.
	if done, err := s.maybeCompleteSubscription(ctx, sub); err != nil {
		return err
	} else if done {
		return nil
	}

	// Issue #534: claim the subscription BEFORE charging. next_charge_at is only
	// advanced by the purchase.paid callback, so without a lease the next sweep
	// (hourly, and immediately on process start) sees the subscription as still
	// due while the first charge is pending and charges the customer's card again
	// for the same period. The claim is a conditional UPDATE, so exactly one
	// caller can win — concurrent sweeps cannot both charge.
	claimed, err := s.Subs.ClaimDue(ctx, sub.ID, chargeLease)
	if err != nil {
		return fmt.Errorf("claim due: %w", err)
	}
	if !claimed {
		// Either another worker/instance holds the lease, or an earlier charge is
		// still pending, or the subscription is no longer due. All mean: do not
		// charge now.
		slog.Info("sub_charge_skipped_not_claimed", "sub", sub.ID)
		return nil
	}

	acc, err := s.Accounts.ByID(ctx, sub.AccountID)
	if err != nil || acc.ChipBrandID == "" || acc.ChipSecretKeyEnc == "" {
		return fmt.Errorf("account/creds: %w", err)
	}
	secret, err := cryptoutil.Decrypt(s.DecryptKey, acc.ChipSecretKeyEnc)
	if err != nil {
		return fmt.Errorf("decrypt: %w", err)
	}

	// attempt number = previous failed attempts + 1
	prior, err := s.Subs.FailedAttempts(ctx, sub.ID)
	if err != nil {
		// Issue #535: never guess the attempt number. A wrong value disarms the
		// retry ladder (past_due / card-update / auto-cancel become unreachable),
		// so abort this cycle and try again on the next sweep.
		return fmt.Errorf("failed attempts: %w", err)
	}
	attempt := prior + 1

	req := chip.PurchaseRequest{
		CreatorAgent:    "JomForm",
		BrandID:         acc.ChipBrandID,
		Client:          &chip.ClientDetails{Email: sub.ClientEmail},
		ForceRecurring:  true,
		SuccessCallback: s.BaseURL + "/v1/payments/chip/callback",
		Reference:       "SUB-" + sub.ID[:8] + "-" + time.Now().UTC().Format("060102"),
		Purchase: chip.PurchaseDetails{
			Currency: s.workspaceCurrency(ctx, sub.AccountID),
			Products: []chip.Product{{
				Name:     fmt.Sprintf("Langganan #%s", sub.ProductID),
				Price:    sub.AmountSen,
				Quantity: "1",
			}},
		},
	}
	// charge() creates the purchase AND posts /charge/ with the stored token;
	// final result arrives async via purchase.paid / payment_failure callbacks.
	p, err := s.Chip.Charge(ctx, string(secret), req, sub.ChipTokenPurchaseID)
	if err != nil {
		_ = s.Subs.AddCharge(ctx, sub.ID, "", sub.AmountSen, dbstore.ChargeFailed, int(attempt))
		return s.afterFailedCharge(ctx, sub, attempt)
	}
	_ = s.Subs.AddCharge(ctx, sub.ID, p.ID, sub.AmountSen, dbstore.ChargePending, int(attempt))
	slog.Info("sub_charge_created", "sub", sub.ID, "purchase", p.ID, "attempt", attempt)
	return nil
}

// maybeCompleteSubscription marks a subscription completed once it has reached
// its fixed billing count (subscription_charges = N, NULL = infinite). Called
// after the FIRST payment (activateSubscription) and at the start of every
// charge attempt, so the Nth payment completes the subscription immediately and
// no (N+1)th charge is ever scheduled.
//
// ChargeCount counts PAID charge rows, and the first payment is recorded as a
// paid row, so the total consumed is `paid` — NOT `1 + paid`, which counted the
// first payment twice. Returns true when the subscription was just completed.
func (s *Service) maybeCompleteSubscription(ctx context.Context, sub *dbstore.Subscription) (bool, error) {
	if s.Products == nil {
		return false, nil
	}
	prod, err := s.Products.Get(ctx, sub.AccountID, sub.ProductID)
	if err != nil {
		return false, err
	}
	if prod.SubscriptionCharges == nil {
		return false, nil // infinite
	}
	paid, err := s.Subs.ChargeCount(ctx, sub.ID)
	if err != nil {
		// Issue #535: never treat a failed count as "0 charges consumed" — that
		// would keep a fixed-billing subscription charging past its Nth charge.
		return false, err
	}
	if paid >= int64(*prod.SubscriptionCharges) {
		if err := s.Subs.SetStatus(ctx, sub.ID, dbstore.SubCompleted); err != nil {
			return false, err
		}
		slog.Info("sub_completed_charge_limit", "sub", sub.ID, "charges", paid)
		return true, nil
	}
	return false, nil
}

// afterFailedCharge applies the retry ladder.
func (s *Service) afterFailedCharge(ctx context.Context, sub *dbstore.Subscription, attempt int64) error {
	if attempt >= maxChargeAttempts {
		// past_due; CancelOverdueSubs cancels after the 7-day grace window.
		_ = s.Subs.SetStatus(ctx, sub.ID, dbstore.SubPastDue)
		// Issue #154: surface a "card needs update" state so the merchant /
		// customer knows to re-tokenize with a new card before the 7-day
		// cancel. While this flag is set, CancelOverdueSubs pauses the
		// auto-cancel so the subscription is not doomed once the card dies.
		_ = s.Subs.SetCardUpdatePending(ctx, sub.ID, "")
		slog.Warn("sub_past_due", "sub", sub.ID, "attempts", attempt)
		// The past-due transition is the actionable moment for the merchant.
		s.OnPastDue(ctx, sub)
		return nil
	}
	// round 13c (Wan): tell someone. A failed recurring charge used to notify
	// nobody — no event, no webhook, no email — so a card that died went
	// unnoticed until the customer complained. Fire once per failure cycle
	// (attempt 1), not on every retry: three identical emails per billing cycle
	// are how a merchant learns to ignore them.
	if attempt <= 1 {
		s.fireSubscriptionChargeFailed(ctx, sub, attempt)
	}
	_ = s.Subs.ScheduleNextCharge(ctx, sub.ID, time.Now().Add(retryEvery))
	slog.Info("sub_retry_scheduled", "sub", sub.ID, "attempt", attempt)
	return nil
}

// CancelOverdueSubs cancels subs past_due longer than the grace period and
// revokes their CHIP card tokens.
func (s *Service) CancelOverdueSubs(ctx context.Context) error {
	subs, err := s.Subs.PastDueBefore(ctx, time.Now().AddDate(0, 0, -cancelAfterDays))
	if err != nil {
		return err
	}
	// NOTE: per Wan — do NOT auto-revoke the card token on cancellation.
	// The token stays valid in CHIP; merchants can re-activate or charge
	// manually. Only the subscription row is cancelled locally.
	for _, sub := range subs {
		// Issue #154: pause the auto-cancel while a card update is pending —
		// the merchant has been asked to re-tokenize and should get the full
		// grace window to do so before the subscription is cancelled.
		if sub.CardUpdatePending {
			slog.Info("sub_cancel_paused_card_update", "sub", sub.ID)
			continue
		}
		if err := s.Subs.Cancel(ctx, sub.ID); err != nil {
			return err
		}
		slog.Info("sub_cancelled_overdue", "sub", sub.ID)
	}
	return nil
}
