// Issue #154: update-card / replace-token flow for subscriptions.
//
// When a customer's card expires or is terminated, the stored CHIP token
// keeps failing and the subscription would otherwise be doomed to auto-cancel.
// This flow lets a merchant re-tokenize with a new card:
//
//   - CreateUpdateCardPurchase creates a new CHIP token purchase (the
//     customer pays $0 / authorizes with the new card) and records it as the
//     subscription's pending card-update purchase. The old token is left in
//     CHIP (Wan rule — never auto-revoke) but is no longer used.
//   - When the customer completes the new-card checkout, the purchase.paid
//     callback (CompleteCardUpdate) atomically replaces the subscription's
//     chip_token_purchase_id with the new token purchase and clears the
//     card-update-pending flag. The billing schedule is untouched, so the
//     period does not restart.
package payments

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/wzul/jomform/api/internal/chip"
	"github.com/wzul/jomform/api/internal/cryptoutil"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// ErrNoCardUpdate signals a callback for a purchase that is not a pending
// card-update re-tokenize (e.g. a stale/duplicate event).
var ErrNoCardUpdate = errors.New("bukan kemas kini kad yang belum selesai")

// CreateUpdateCardPurchase creates a new CHIP token purchase for the
// subscription's customer so they can authorize a new card. On success the
// purchase id is recorded as the subscription's pending card-update purchase
// and the returned purchase carries the checkout_url the merchant should
// redirect the customer to. The old token is retained in CHIP (Wan rule).
func (s *Service) CreateUpdateCardPurchase(ctx context.Context, sub *dbstore.Subscription) (*chip.Purchase, error) {
	acc, err := s.Accounts.ByID(ctx, sub.AccountID)
	if err != nil {
		return nil, fmt.Errorf("account: %w", err)
	}
	if acc.ChipBrandID == "" || acc.ChipSecretKeyEnc == "" {
		return nil, ErrNotConfigured
	}
	secret, err := cryptoutil.Decrypt(s.DecryptKey, acc.ChipSecretKeyEnc)
	if err != nil {
		return nil, fmt.Errorf("decrypt key: %w", err)
	}

	// A $0 / authorize-only purchase that saves the card as a recurring
	// token. The customer pays nothing; the new token replaces the old one.
	req := chip.PurchaseRequest{
		CreatorAgent:   "JomForm",
		BrandID:        acc.ChipBrandID,
		Client:         &chip.ClientDetails{Email: sub.ClientEmail},
		ForceRecurring: true,
		SkipCapture:    true,
		Purchase: chip.PurchaseDetails{
			Currency: s.spaceCurrency(ctx, sub.AccountID),
			Products: []chip.Product{{
				Name:     fmt.Sprintf("Kemas kini kad — Langganan #%s", sub.ProductID),
				Price:    0,
				Quantity: "1",
			}},
		},
		SuccessRedirect: s.BaseURL + "/subscriptions",
		FailureRedirect: s.BaseURL + "/subscriptions",
		CancelRedirect:  s.BaseURL + "/subscriptions",
		SuccessCallback: s.BaseURL + "/v1/payments/chip/callback",
		Reference:       "UPDCARD-" + sub.ID[:8] + "-" + time.Now().UTC().Format("060102"),
	}

	p, err := s.Chip.CreatePurchase(ctx, string(secret), req)
	if err != nil {
		return nil, fmt.Errorf("chip create: %w", err)
	}
	if err := s.Subs.SetCardUpdatePending(ctx, sub.ID, p.ID); err != nil {
		return nil, fmt.Errorf("record pending update: %w", err)
	}
	return p, nil
}

// CompleteCardUpdate is called from the purchase.paid callback when the
// customer has authorized the new card. It atomically replaces the
// subscription's recurring token with the new token purchase and clears the
// card-update-pending flag. The old token is left in CHIP (Wan rule) but is
// no longer used for charges. The billing schedule is unchanged.
func (s *Service) CompleteCardUpdate(ctx context.Context, p *chip.Purchase) error {
	sub, err := s.Subs.ByCardUpdatePurchase(ctx, p.ID)
	if err != nil {
		return ErrNoCardUpdate
	}
	if err := s.Subs.UpdateToken(ctx, sub.AccountID, sub.ID, p.ID); err != nil {
		return fmt.Errorf("replace token: %w", err)
	}
	return nil
}
