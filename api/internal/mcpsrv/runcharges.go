package mcpsrv

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/dbstore"
)

type emptyOut struct{}

// runDueCharges lets an agent trigger the recurring-charge sweep on demand.
func (s *Server) runDueCharges(
	ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn,
) (*mcp.CallToolResult, emptyOut, error) {
	if _, err := s.accountAndScope(ctx, true); err != nil {
		return nil, emptyOut{}, err
	}
	if s.pay == nil {
		return nil, emptyOut{}, errors.New("payments tidak aktif")
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
	Email string `json:"email" jsonschema:"emel pelanggan yang ada langganan"`
}

func (s *Server) updateSubscriptionCard(
	ctx context.Context, _ *mcp.CallToolRequest, in updateSubCardIn,
) (*mcp.CallToolResult, map[string]string, error) {
	accountID, err := s.accountAndScope(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	if in.Email == "" {
		return nil, nil, errors.New("email wajib")
	}
	if s.pay == nil {
		return nil, nil, errors.New("payments tidak aktif")
	}
	sub, err := s.deps.Subs.ByEmail(ctx, accountID, in.Email)
	if err != nil {
		return nil, nil, errors.New("langganan tidak dijumpai")
	}
	if sub.Status == dbstore.SubCanceled {
		return nil, nil, errors.New("langganan telah dibatalkan")
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
