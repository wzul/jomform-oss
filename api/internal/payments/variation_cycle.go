package payments

// Issue #623: one resolution site for a subscription's billing cycle.
//
// A product variation may carry its own subscription cycle (billing period,
// trial days, fixed charge count), and a field the variation sets wins over the
// product's. The checkout resolves it once (see httpapi.resolveSubscriptionTerms)
// and records the result on the sale; the recurring engine must apply the SAME
// rule from the second charge onward, or a variation-fixed plan would bill on
// the product's terms — the exact bug class this issue fixes, merely moved one
// step later.
//
// dbstore.EffectiveCycle is that single rule. This helper is only the lookup
// around it (product + variation), so there is still exactly one place the
// precedence is decided.

import (
	"context"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// subscriptionCycleFor resolves the effective cycle for a subscription: the
// product's cycle overridden, field by field, by the variation the subscription
// was bought as (sub.VariationID, empty for a simple-product subscription).
//
// ok is false when no product could be resolved — callers must treat that as
// "terms unknown" and skip rather than assume an infinite plan (assuming would
// silently un-limit a fixed-billing subscription). A subscription always names a
// product, so a miss means the row was removed; s.Products nil is a bare-mode
// test service.
func (s *Service) subscriptionCycleFor(ctx context.Context, sub *dbstore.Subscription) (dbstore.SubscriptionCycle, bool, error) {
	if s.Products == nil || sub == nil {
		return dbstore.SubscriptionCycle{}, false, nil
	}
	prod, err := s.Products.Get(ctx, sub.AccountID, sub.ProductID)
	if err != nil {
		return dbstore.SubscriptionCycle{}, false, err
	}
	var variation *dbstore.ProductVariation
	if sub.VariationID != "" && s.ProductVariations != nil {
		// GetByProduct scopes the lookup to the product, so a stale/foreign
		// variation id can never pull another product's cycle into this
		// subscription. A miss falls back to the product's cycle — the same
		// state as a variation that sets nothing.
		v, verr := s.ProductVariations.GetByProduct(ctx, sub.ProductID, sub.VariationID)
		if verr == nil {
			variation = v
		} else if verr != dbstore.ErrVariationNotFound {
			return dbstore.SubscriptionCycle{}, false, verr
		}
	}
	return dbstore.EffectiveCycle(prod, variation), true, nil
}
