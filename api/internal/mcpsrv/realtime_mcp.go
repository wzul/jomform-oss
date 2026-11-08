// MCP tool: realtime notifications status (issue #447 parity).
//
// The dashboard's realtime channel is an authenticated SSE stream
// (GET /v1/rest/events/stream) that pushes sale.paid / sale.failed /
// response.submitted / subscription.charged events to the merchant's
// connected clients. This tool documents that channel and exposes the
// per-workspace sound toggle so an agent can read/configure the audio beep
// that plays on a kitchen/order screen when a new order arrives.
package mcpsrv

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// getRealtimeStatus reports the realtime notification channel for a workspace:
// the SSE stream URL (the realtime channel) and the per-workspace sound toggle
// (issue #447). Read-only.
func (s *Server) getRealtimeStatus(
	ctx context.Context, _ *mcp.CallToolRequest, in workspaceRefIn,
) (*mcp.CallToolResult, map[string]any, error) {
	if _, _, err := s.accessibleWorkspace(ctx, in.WorkspaceID); err != nil {
		return nil, nil, err
	}
	sp, err := s.workspacesStore()
	if err != nil {
		return nil, nil, err
	}
	st, err := sp.Settings(ctx, in.WorkspaceID)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]any{
		"stream_url":    "/v1/rest/events/stream",
		"events":        []string{"sale.paid", "sale.failed", "response.submitted", "subscription.charged", "sale.refunded"},
		"sound_enabled": st.SoundEnabled,
		"note":          "Connect to stream_url (SSE, session/API key auth) to receive sale/response events live. sound_enabled controls the beep on the dashboard.",
	}, nil
}

// setRealtimeSound toggles the per-workspace realtime notification sound
// (issue #447). Write tool (peranan manage/admin).
func (s *Server) setRealtimeSound(
	ctx context.Context, _ *mcp.CallToolRequest, in setRealtimeSoundIn,
) (*mcp.CallToolResult, map[string]any, error) {
	// Issue #645: write gate = API-key write scope AND manage-or-admin role.
	if _, err := s.requireWorkspaceWriteRole(ctx, in.WorkspaceID); err != nil {
		return nil, nil, err
	}
	sp, err := s.workspacesStore()
	if err != nil {
		return nil, nil, err
	}
	st, err := sp.Settings(ctx, in.WorkspaceID)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	st.SoundEnabled = in.Enabled
	if err := sp.SaveSettings(ctx, st); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]any{"sound_enabled": st.SoundEnabled, "workspace_id": in.WorkspaceID}, nil
}

type setRealtimeSoundIn struct {
	WorkspaceID string `json:"workspace_id" jsonschema:"Workspace ID (from list_workspaces)"`
	Enabled     bool   `json:"enabled" jsonschema:"true = play a beep on the dashboard when a new sale/response arrives; false = silent"`
}
