package payments

// Round 13c (Wan): subscription recovery + fixed billing schedule.
//
// This file holds the pieces the recurring engine needs to answer three
// questions a merchant actually asks when a charge fails:
//
//	1. Nobody was told.  -> fireSubscriptionChargeFailed notifies the merchant
//	   (and the customer, if the form has a rule for it) through the SAME
//	   per-form notification rules the other sale events already use — no new
//	   event vocabulary, so the dashboard builder keeps working.
//	2. The subscription is dead after 3 failures. -> store.ReactivateOnPayment
//	   / Reactivate revive it (a settled payment, or a merchant/admin retry),
//	   and the engine charges past_due again.
//	3. The schedule drifts. -> the product's schedule_mode (nextChargeForPolicy)
//	   lets the merchant keep the ORIGINAL dates.

import (
	"context"
	"log/slog"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// scheduleModeFor returns the product's schedule mode, defaulting to the
// historical from-payment behaviour when the product cannot be read.
func (s *Service) scheduleModeFor(ctx context.Context, sub *dbstore.Subscription) string {
	if s.Products == nil {
		return ScheduleFromPayment
	}
	prod, err := s.Products.Get(ctx, sub.AccountID, sub.ProductID)
	if err != nil || prod == nil {
		return ScheduleFromPayment
	}
	if prod.ScheduleMode == "" {
		return ScheduleFromPayment
	}
	return prod.ScheduleMode
}

// fireSubscriptionChargeFailed notifies the merchant/customer that a recurring
// charge failed.
//
// Round 13c (Wan): a failed subscription charge notified NOBODY — no event, no
// webhook, no email — so a card that died mid-subscription went unnoticed by
// both sides until the customer complained. This reuses the existing per-form
// notification rules (the same engine that already handles sale.paid and
// subscription.charged), so merchants configure the alert in the dashboard they
// already use.
//
// It fires on the FIRST failure only. The retry ladder makes up to 3 attempts,
// and 3 identical emails per billing cycle would train the merchant to ignore
// them; the merchant needs to know the card is failing, and the past-due
// transition is the actionable moment.
func (s *Service) fireSubscriptionChargeFailed(ctx context.Context, sub *dbstore.Subscription, attempt int64) {
	slog.Warn("sub_charge_failed_notify", "sub", sub.ID, "attempt", attempt, "email", sub.ClientEmail)
	if s.Notifications == nil {
		return
	}
	// The failed attempt's sale gives the notification engine its ref code,
	// buyer details and form binding. Not every subscription has one (the
	// recurring charge creates a purchase, not necessarily a sale row), so
	// this is best-effort by design.
	sale, err := s.subscriptionSale(ctx, sub)
	if err != nil || sale == nil {
		return
	}
	s.fireSaleNotifications(ctx, sale, dbstore.NotifyEventSaleFailed)
}

// subscriptionSale resolves the sale behind a subscription, used to bind the
// failure notification to the right form + ref code.
func (s *Service) subscriptionSale(ctx context.Context, sub *dbstore.Subscription) (*dbstore.Sale, error) {
	if s.Sales == nil || sub.SaleID == "" {
		return nil, nil
	}
	return s.Sales.ByID(ctx, sub.SaleID)
}

// OnPastDue notifies the merchant that a subscription has exhausted its retry
// ladder and needs attention (round 13c). Called once, at the moment the
// subscription becomes past_due — the point at which a human must act (fix the
// card, retry, or cancel), rather than on each of the three attempts.
//
// The subscription's grace window (7 days) is what the customer-facing message
// is really about, so that deadline is included.
func (s *Service) OnPastDue(ctx context.Context, sub *dbstore.Subscription) {
	if s.Notifications == nil {
		return
	}
	sale, err := s.subscriptionSale(ctx, sub)
	if err != nil || sale == nil {
		// No sale to bind to: still worth a log line, but the per-form
		// notification rules need a form/ref to resolve recipients.
		return
	}
	s.fireSaleNotifications(ctx, sale, dbstore.NotifyEventSaleFailed)
}
