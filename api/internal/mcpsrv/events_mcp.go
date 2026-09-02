// MCP parity tool: list_events — account-scoped, read-only audit/behaviour log
// (issue #92). Mirrors GET /v1/rest/events for merchant agents.
package mcpsrv

import (
	"context"
	"errors"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/dbstore"
)

type listEventsIn struct {
	EventName string `json:"event_name,omitempty" jsonschema:"Optional — hadkan kepada satu jenis event (cth: form.created, sale.confirmed, auth.login)"`
	From      string `json:"from,omitempty" jsonschema:"Optional — RFC3339 mula (cth: 2026-01-01T00:00:00Z)"`
	To        string `json:"to,omitempty" jsonschema:"Optional — RFC3339 tamat"`
	Page      int    `json:"page,omitempty" jsonschema:"Muka surat (1-based, default 1)"`
	Limit     int    `json:"limit,omitempty" jsonschema:"Bilangan per muka (default 20, maks 100)"`
}

type eventOut struct {
	ID         int64          `json:"id"`
	EventName  string         `json:"event_name"`
	Properties map[string]any `json:"properties"`
	UserID     string         `json:"user_id,omitempty"`
	IP         string         `json:"ip,omitempty"`
	UserAgent  string         `json:"user_agent,omitempty"`
	CreatedAt  string         `json:"created_at"`
}

// listEvents returns the account's audit/behaviour log, newest first,
// paginated and filterable. Read-only (read scope is enough).
func (s *Server) listEvents(
	ctx context.Context, _ *mcp.CallToolRequest, in listEventsIn,
) (*mcp.CallToolResult, map[string]any, error) {
	acc, err := s.accountAndScope(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	if s.deps.Events == nil {
		return nil, nil, errors.New("event store tidak dikonfigurasi")
	}
	f := dbstore.EventFilter{AccountIDs: []string{acc}, EventName: in.EventName}
	if in.From != "" {
		if t, err := time.Parse(time.RFC3339, in.From); err == nil {
			f.From = &t
		}
	}
	if in.To != "" {
		if t, err := time.Parse(time.RFC3339, in.To); err == nil {
			f.To = &t
		}
	}
	f.Page, f.Limit = in.Page, in.Limit
	events, total, err := s.deps.Events.List(ctx, f)
	if err != nil {
		return nil, nil, err
	}
	out := make([]eventOut, 0, len(events))
	for _, e := range events {
		out = append(out, eventOut{
			ID: e.ID, EventName: e.EventName, Properties: e.Properties,
			UserID: e.UserID, IP: e.IP, UserAgent: e.UserAgent,
			CreatedAt: e.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	return nil, map[string]any{"items": out, "page": f.Page, "limit": f.Limit, "total": total}, nil
}
