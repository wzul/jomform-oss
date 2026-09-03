// MCP request history (issue #195): every MCP tool call is recorded as an
// `mcp.tool_call` event in the #92 events table so the Settings → MCP tab can
// show a per-request history (tool, agent, timestamp, success/failure) and
// agents can query it via list_events / GET /v1/rest/events?event_name=mcp.*.
package mcpsrv

import (
	"context"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// addTool registers an MCP tool exactly like mcp.AddTool, but wraps the
// handler so every CallTool is recorded as an mcp.tool_call event. The event
// carries {tool, agent, account_id, success, error} and is best-effort: a nil
// Events store or a write error is logged, never fatal to the tool call.
func addTool[In, Out any](s *Server, t *mcp.Tool, h mcp.ToolHandlerFor[In, Out]) {
	mcp.AddTool(s.mcp, t, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		res, out, err := h(ctx, req, in)
		s.recordMCPEvent(ctx, t.Name, err != nil || (res != nil && res.IsError))
		return res, out, err
	})
}

// recordMCPEvent appends an mcp.tool_call event for the authenticated account
// (issue #195). The agent name comes from the AuthIdentity (OAuth client_name
// or API-key name). Best-effort — never breaks the primary tool call.
func (s *Server) recordMCPEvent(ctx context.Context, tool string, failed bool) {
	if s.deps.Events == nil {
		return
	}
	id := IdentityFromContext(ctx)
	if id == nil {
		return
	}
	props := map[string]any{
		"tool":       tool,
		"agent":      id.Agent,
		"account_id": id.AccountID,
		"success":    !failed,
	}
	if failed {
		props["error"] = "tool call failed"
	}
	if err := s.deps.Events.Record(ctx, id.AccountID, "", "mcp.tool_call", props, "", ""); err != nil {
		slog.Error("mcp_tool_call_event", "err", err, "tool", tool, "account", id.AccountID)
	}
}
