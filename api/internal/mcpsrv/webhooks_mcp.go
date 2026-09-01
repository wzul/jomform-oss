// MCP parity tools: e-invoice settings + webhook CRUD + deliveries.
package mcpsrv

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ── e-Invoice settings ──────────────────────────────────────────────────────

type setEinvoiceIn struct {
	TIN  string `json:"tin" jsonschema:"Nombor TIN LHDN (kosong = clear)"`
	MSIC string `json:"msic" jsonschema:"Kod MSIC 5 digit"`
}

func (s *Server) setEinvoice(
	ctx context.Context, _ *mcp.CallToolRequest, in setEinvoiceIn,
) (*mcp.CallToolResult, map[string]string, error) {
	acc, err := s.accountAndScope(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	if err := s.deps.Accounts.SetEinvoice(ctx, acc, in.TIN, in.MSIC); err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"status": "saved", "tin": in.TIN, "msic": in.MSIC}, nil
}

// ── Webhooks CRUD ────────────────────────────────────────────────────────────

type webhookIn struct {
	URL    string   `json:"url"`
	Secret string   `json:"secret"`
	Events []string `json:"events,omitempty" jsonschema:"sale.paid|sale.failed|subscription.charged|*"`
}

func (s *Server) createWebhookMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in webhookIn,
) (*mcp.CallToolResult, map[string]string, error) {
	acc, err := s.accountAndScope(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	if len(in.URL) < 8 || len(in.Secret) < 8 {
		return nil, nil, errors.New("url dan secret (min 8 char) wajib")
	}
	events := in.Events
	if len(events) == 0 {
		events = []string{"sale.paid"}
	}
	wh, err := s.deps.Webhooks.Create(ctx, acc, in.URL, in.Secret, events)
	if err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"id": wh.ID, "status": "created"}, nil
}

type webhookListOut struct {
	ID     string   `json:"id"`
	URL    string   `json:"url"`
	Events []string `json:"events"`
	Active bool     `json:"active"`
}

func (s *Server) listWebhooksMCP(
	ctx context.Context, _ *mcp.CallToolRequest, _ struct{},
) (*mcp.CallToolResult, []webhookListOut, error) {
	acc, err := s.accountAndScope(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	hooks, err := s.deps.Webhooks.ListByAccount(ctx, acc)
	if err != nil {
		return nil, nil, err
	}
	out := []webhookListOut{}
	for _, w := range hooks {
		out = append(out, webhookListOut{ID: w.ID, URL: w.URL, Events: w.Events, Active: w.Active})
	}
	return nil, out, nil
}

type webhookRef struct {
	ID string `json:"id"`
}

func (s *Server) deleteWebhookMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in webhookRef,
) (*mcp.CallToolResult, map[string]string, error) {
	acc, err := s.accountAndScope(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	if err := s.deps.Webhooks.Delete(ctx, acc, in.ID); err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"status": "deleted"}, nil
}

type webhookDeliveriesIn struct {
	WebhookID string `json:"webhook_id"`
	Limit     int    `json:"limit,omitempty"`
}

func (s *Server) webhookDeliveriesMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in webhookDeliveriesIn,
) (*mcp.CallToolResult, []map[string]any, error) {
	acc, err := s.accountAndScope(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	limit := in.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	deliveries, err := s.deps.Webhooks.ListDeliveries(ctx, acc, in.WebhookID, limit)
	if err != nil {
		return nil, nil, err
	}
	return nil, deliveries, nil
}
