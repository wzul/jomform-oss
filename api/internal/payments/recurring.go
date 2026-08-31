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

func (s *Service) chargeSubscription(ctx context.Context, sub *dbstore.Subscription) error {
	acc, err := s.Accounts.ByID(ctx, sub.AccountID)
	if err != nil || acc.ChipBrandID == "" || acc.ChipSecretKeyEnc == "" {
		return fmt.Errorf("account/creds: %w", err)
	}
	secret, err := cryptoutil.Decrypt(s.DecryptKey, acc.ChipSecretKeyEnc)
	if err != nil {
		return fmt.Errorf("decrypt: %w", err)
	}

	// attempt number = previous failed attempts + 1
	attempt := s.Subs.FailedAttempts(ctx, sub.ID) + 1

	req := chip.PurchaseRequest{
		CreatorAgent:    "JomForm",
		BrandID:         acc.ChipBrandID,
		Client:          &chip.ClientDetails{Email: sub.ClientEmail},
		ForceRecurring:  true,
		SuccessCallback: s.BaseURL + "/v1/payments/chip/callback",
		Reference:       "SUB-" + sub.ID[:8] + "-" + time.Now().UTC().Format("060102"),
		Purchase: chip.PurchaseDetails{
			Currency: acc.Currency,
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

// afterFailedCharge applies the retry ladder.
func (s *Service) afterFailedCharge(ctx context.Context, sub *dbstore.Subscription, attempt int64) error {
	if attempt >= maxChargeAttempts {
		// past_due; CancelOverdueSubs cancels after the 7-day grace window.
		_ = s.Subs.SetStatus(ctx, sub.ID, dbstore.SubPastDue)
		slog.Warn("sub_past_due", "sub", sub.ID, "attempts", attempt)
		return nil
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
		if err := s.Subs.Cancel(ctx, sub.ID); err != nil {
			return err
		}
		slog.Info("sub_cancelled_overdue", "sub", sub.ID)
	}
	return nil
}
