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
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/dbstore"
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
		"note":          "Sambungkan ke stream_url (SSE, autentikasi sesi/API key) untuk menerima peristiwa jualan/respon secara langsung. sound_enabled mengawal bunyi beep pada dashboard.",
	}, nil
}

// setRealtimeSound toggles the per-workspace realtime notification sound
// (issue #447). Write tool (peranan manage/admin).
func (s *Server) setRealtimeSound(
	ctx context.Context, _ *mcp.CallToolRequest, in setRealtimeSoundIn,
) (*mcp.CallToolResult, map[string]any, error) {
	_, role, err := s.accessibleWorkspace(ctx, in.WorkspaceID)
	if err != nil {
		return nil, nil, err
	}
	if role != "" && role != dbstore.RoleManage && role != dbstore.RoleAdmin {
		return nil, nil, errors.New("perlu peranan manage atau admin")
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
	WorkspaceID string `json:"workspace_id" jsonschema:"ID workspace (dari list_workspaces)"`
	Enabled     bool   `json:"enabled" jsonschema:"true = mainkan bunyi beep pada dashboard bila jualan/respon baharu tiba; false = senyap"`
}
