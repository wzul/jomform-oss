package mcpsrv

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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
