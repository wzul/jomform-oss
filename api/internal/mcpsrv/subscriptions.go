package mcpsrv

import (
	"context"
	"log/slog"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// cancelChipToken best-effort deletes the recurring token at CHIP when a
// subscription is cancelled. Never fails the cancellation flow.
func (s *Server) cancelChipToken(sub *dbstore.Subscription) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("cancel_chip_token_panic", "err", r)
		}
	}()
	acc, err := s.deps.Accounts.ByID(context.Background(), sub.AccountID)
	if err != nil || acc.ChipSecretKeyEnc == "" {
		return
	}
	secret, err := s.decrypt(acc.ChipSecretKeyEnc)
	if err != nil {
		return
	}
	if s.chip != nil && sub.ChipTokenPurchaseID != "" {
		if err := s.chip.DeleteRecurringToken(context.Background(), secret, sub.ChipTokenPurchaseID); err != nil {
			slog.Warn("delete_recurring_token_failed", "err", err, "purchase", sub.ChipTokenPurchaseID)
		}
	}
}
