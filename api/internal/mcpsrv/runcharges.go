package mcpsrv

import (
	"context"
	"errors"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/dbstore"
)

type emptyOut struct{}

// runDueCharges lets an agent trigger the recurring-charge sweep on demand.
func (s *Server) runDueCharges(
	ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn,
) (*mcp.CallToolResult, emptyOut, error) {
	if _, err := s.requireWriteRole(ctx); err != nil {
		return nil, emptyOut{}, err
	}
	if s.pay == nil {
		return nil, emptyOut{}, errors.New("payments not active")
	}
	if err := s.pay.RunDueCharges(ctx); err != nil && err.Error() != "no due subscriptions" {
		return nil, emptyOut{}, err
	}
	return nil, emptyOut{}, nil
}

// updateSubscriptionCard lets an agent re-tokenize a subscription with a new
// card (issue #154). It creates a new CHIP token purchase and returns the
// checkout_url the merchant should send to the customer. The old token is
// retained in CHIP (Wan rule) but no longer used once the new card is paid.
type updateSubCardIn struct {
	Email string `json:"email" jsonschema:"email of the customer who has a subscription"`
}

func (s *Server) updateSubscriptionCard(
	ctx context.Context, _ *mcp.CallToolRequest, in updateSubCardIn,
) (*mcp.CallToolResult, map[string]string, error) {
	accountID, err := s.requireWriteRole(ctx)
	if err != nil {
		return nil, nil, err
	}
	if in.Email == "" {
		return nil, nil, errors.New("email is required")
	}
	if s.pay == nil {
		return nil, nil, errors.New("payments not active")
	}
	sub, err := s.deps.Subs.ByEmail(ctx, accountID, in.Email)
	if err != nil {
		return nil, nil, errors.New("subscription not found")
	}
	if sub.Status == dbstore.SubCanceled {
		return nil, nil, errors.New("subscription has been cancelled")
	}
	p, err := s.pay.CreateUpdateCardPurchase(ctx, sub)
	if err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{
		"status":       "checkout_created",
		"email":        in.Email,
		"checkout_url": p.CheckoutURL,
		"purchase_id":  p.ID,
	}, nil
}

// retrySubscriptionIn identifies the subscription to revive by email (the same
// addressing update_subscription_card uses) or by id.
type retrySubscriptionIn struct {
	SubscriptionID string `json:"subscription_id,omitempty" jsonschema:"Subscription ID (optional; use email if empty)"`
	Email          string `json:"email,omitempty" jsonschema:"Email of the customer who has a subscription (optional)"`
}

// retrySubscription revives a past_due subscription so the recurring engine
// charges it again (round 13c). It does NOT charge immediately — the next sweep
// does, so money movement stays in one code path.
func (s *Server) retrySubscription(
	ctx context.Context, _ *mcp.CallToolRequest, in retrySubscriptionIn,
) (*mcp.CallToolResult, map[string]string, error) {
	accountID, err := s.requireWriteRole(ctx)
	if err != nil {
		return nil, nil, err
	}
	if in.SubscriptionID == "" && in.Email == "" {
		return nil, nil, errors.New("subscription_id or email is required")
	}
	var sub *dbstore.Subscription
	if in.SubscriptionID != "" {
		sub, err = s.deps.Subs.ByIDForAccount(ctx, accountID, in.SubscriptionID)
	} else {
		sub, err = s.deps.Subs.ByEmail(ctx, accountID, in.Email)
	}
	if err != nil || sub == nil {
		return nil, nil, errors.New("subscription not found")
	}
	was := sub.Status
	if err := s.deps.Subs.Reactivate(ctx, accountID, sub.ID); err != nil {
		if err == dbstore.ErrNotPastDue {
			return nil, nil, errors.New("only past_due subscriptions can be retried (status: " + was + ")")
		}
		return nil, nil, err
	}
	// Due now, so the next sweep picks it up without waiting a full period.
	if err := s.deps.Subs.ScheduleNextCharge(ctx, sub.ID, time.Now().Add(-time.Minute)); err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{
		"status":          "retry_queued",
		"was":             was,
		"email":           sub.ClientEmail,
		"subscription_id": sub.ID,
	}, nil
}
