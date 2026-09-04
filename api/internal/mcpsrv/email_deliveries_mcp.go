// MCP parity tool: list_email_deliveries — account-scoped, read-only email
// delivery status view (issue #194). Mirrors GET /v1/rest/email-deliveries
// for merchant agents: list delivery events (delivered/bounce/complaint/
// open/click) with email, event type, timestamp, sale ref and detail,
// filterable by form/sale/status.
package mcpsrv

import (
	"context"
	"errors"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/dbstore"
)

type listEmailDeliveriesIn struct {
	FormID    string `json:"form_id,omitempty" jsonschema:"Optional — hadkan kepada jualan satu borang (id borang)"`
	SaleRef   string `json:"sale_ref,omitempty" jsonschema:"Optional — hadkan kepada satu jualan (ref_code)"`
	EventType string `json:"event_type,omitempty" jsonschema:"Optional — delivered | bounce | complaint | open | click"`
	Page      int    `json:"page,omitempty" jsonschema:"Muka surat (1-based, default 1)"`
	Limit     int    `json:"limit,omitempty" jsonschema:"Bilangan per muka (default 20, maks 100)"`
}

type emailDeliveryOut struct {
	ID         int64          `json:"id"`
	Email      string         `json:"email"`
	EventType  string         `json:"event_type"`
	BounceType string         `json:"bounce_type,omitempty"`
	SaleRef    string         `json:"sale_ref,omitempty"`
	FormID     string         `json:"form_id,omitempty"`
	Detail     map[string]any `json:"detail"`
	CreatedAt  string         `json:"created_at"`
}

// listEmailDeliveries returns the user's email delivery events, newest
// first, paginated and filterable by form/sale/status. Read-only (read scope
// is enough). Issue #307: spans all of the user's spaces.
func (s *Server) listEmailDeliveries(
	ctx context.Context, _ *mcp.CallToolRequest, in listEmailDeliveriesIn,
) (*mcp.CallToolResult, map[string]any, error) {
	accs, err := s.accountScopes(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	if s.deps.EmailDeliveryEvents == nil {
		return nil, nil, errors.New("store penghantaran emel tidak dikonfigurasi")
	}
	f := dbstore.DeliveryFilter{
		AccountIDs: accs,
		FormID:     in.FormID,
		SaleRef:    in.SaleRef,
		EventType:  in.EventType,
		Page:       in.Page,
		Limit:      in.Limit,
	}
	events, total, err := s.deps.EmailDeliveryEvents.ListByFilter(ctx, f)
	if err != nil {
		return nil, nil, err
	}
	out := make([]emailDeliveryOut, 0, len(events))
	for _, e := range events {
		out = append(out, emailDeliveryOut{
			ID: e.ID, Email: e.Email, EventType: e.EventType,
			BounceType: e.BounceType, SaleRef: e.SaleRef, FormID: e.FormID, Detail: e.Detail,
			CreatedAt: e.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	return nil, map[string]any{"items": out, "page": f.Page, "limit": f.Limit, "total": total}, nil
}
