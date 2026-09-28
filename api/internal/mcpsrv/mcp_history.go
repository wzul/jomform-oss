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
//
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
//
// The attribution account must still EXIST when the row lands:
// events.account_id is `REFERENCES accounts(id) ON DELETE CASCADE`, so an event
// written for an account the same call destroyed is refused by Postgres
// (SQLSTATE 23503) — and because recording is best-effort that refusal is
// swallowed to a log line. delete_workspace is exactly that case (it removes the
// account its own call scope names), so the account is re-resolved against what
// survives instead of being trusted blindly.
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
	// Never attribute an event to the row this call destroyed (or to anything
	// else gone): fall back to an account the user still has, and skip the
	// history row entirely when there is none.
	if !s.accountSurvives(ctx, accountID) {
		accountID = s.survivingAccountFor(ctx)
	}
	if accountID == "" {
		slog.Warn("mcp_tool_call_event_skipped", "tool", tool,
			"reason", "no surviving account to attribute the event to")
		return
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

// accountSurvives reports whether accountID is still usable as an attribution
// target: the row exists AND it is not parked on a workspace that is in the bin.
//
// Existence alone is not enough once a delete is a move to the bin. Every account
// row survives its workspace's move to the trash, so an existence check would
// file a delete_workspace history row against an account inside the workspace
// that was just put away — and `mcp.tool_call` history is read through the
// account scope, so the caller would never see their own call. That is the same
// "looks recorded, is not" defect the hard delete produced through the FK
// (events_account_id_fkey, 23503, swallowed by the best-effort write) wearing
// the bin's clothes instead.
//
// A store error is treated as "does not survive" so the caller falls back rather
// than gambling on the row.
func (s *Server) accountSurvives(ctx context.Context, accountID string) bool {
	if accountID == "" || s.deps == nil || s.deps.Accounts == nil {
		return false
	}
	a, err := s.deps.Accounts.ByID(ctx, accountID)
	if err != nil || a == nil {
		return false
	}
	if s.deps.Workspaces == nil {
		// No workspace store wired: fall back to the existence answer rather than
		// refusing every attribution.
		return true
	}
	live, err := s.deps.Workspaces.AccountIsLive(ctx, accountID)
	return err == nil && live
}

// survivingAccountFor returns an account the caller still has, preferring any
// account other than the call's own scope. Returns "" when the user has nothing
// left — the one case where the call cannot be recorded.
//
// Owned accounts ONLY. mcp.tool_call history is account-scoped, and
// Workspaces.ByMember lists workspaces the caller owns OR IS A MEMBER OF, so
// walking it would file a caller's own delete_workspace history into a stranger's
// account-scoped history whenever that stranger's (older) workspace sorted first
// — the stranger reads the caller's calls, the caller cannot. Membership in
// someone else's workspace is not a tenancy to record our events in.
//
// Every candidate is also re-checked for existence: a ByOwner row is live by
// construction, but the re-check keeps the FK contract explicit rather than
// trusted.
func (s *Server) survivingAccountFor(ctx context.Context) string {
	cs := callScopeFrom(ctx)
	var doomed string
	if cs != nil && len(cs.Accounts) > 0 {
		doomed = cs.Accounts[0]
	}
	userID := s.userIDFor(ctx)
	if userID == "" || s.deps == nil || s.deps.Accounts == nil {
		return ""
	}
	if accs, err := s.deps.Accounts.ByOwner(ctx, userID); err == nil {
		for _, a := range accs {
			if a == nil || a.ID == "" || a.ID == doomed {
				continue
			}
			if s.accountSurvives(ctx, a.ID) {
				return a.ID
			}
		}
	}
	return ""
}
