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
// handler so every CallTool:
//   - resolves the optional per-call `workspace_id`/`account_id` argument and
//     applies RBAC-scoping to the request context (issue #307: a connected
//     agent can operate across all of the user's workspaces, up to the user's
//     role in each),
//   - is recorded as an mcp.tool_call event (issue #195).
// The event carries {tool, agent, account_id, success, error} and is
// best-effort: a nil Events store or a write error is logged, never fatal.
func addTool[In, Out any](s *Server, t *mcp.Tool, h mcp.ToolHandlerFor[In, Out]) {
	mcp.AddTool(s.mcp, t, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		// Resolve + scope the call from its raw arguments (optional workspace_id /
		// account_id). A failure (e.g. an inaccessible workspace) blocks the call.
		if req != nil && req.Params != nil {
			raw := req.Params.Arguments
			scope, serr := s.resolveScope(ctx, raw)
			if serr != nil {
				var errRes mcp.CallToolResult
				errRes.SetError(serr)
				s.recordMCPEvent(ctx, t.Name, true)
				return &errRes, zero[Out](), nil
			}
			ctx = withCallScope(ctx, scope)
		}
		res, out, err := h(ctx, req, in)
		s.recordMCPEvent(ctx, t.Name, err != nil || (res != nil && res.IsError))
		return res, out, err
	})
}

// zero returns the zero value of T.
func zero[T any]() T { var z T; return z }

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
	// Use the per-call resolved account for the history row so a user-level
	// agent's calls are attributed to the workspace they acted on (issue #307).
	accountID := id.AccountID
	if cs := callScopeFrom(ctx); cs != nil && len(cs.Accounts) > 0 {
		accountID = cs.Accounts[0]
	}
	props := map[string]any{
		"tool":       tool,
		"agent":      id.Agent,
		"account_id": accountID,
		"success":    !failed,
	}
	if failed {
		props["error"] = "tool call failed"
	}
	if err := s.deps.Events.Record(ctx, accountID, id.UserID, "mcp.tool_call", props, "", ""); err != nil {
		slog.Error("mcp_tool_call_event", "err", err, "tool", tool, "account", accountID)
	}
}
