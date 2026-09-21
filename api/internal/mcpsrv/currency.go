// MCP parity tools: currency (issue #185). get_currency_rates exposes the
// BNM daily-cached exchange rates; set_workspace_currency sets a workspace's
// settlement currency (default MYR). Historical sales keep their original
// currency — a change applies to new products/sales going forward.
package mcpsrv

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// ── get_currency_rates ──────────────────────────────────────────────────────

// getCurrencyRates returns the cached BNM exchange rates (Ringgit per unit of
// foreign currency) plus whether they came from the Valkey cache. When the
// currency store is not wired, returns an empty list.
func (s *Server) getCurrencyRates(
	ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn,
) (*mcp.CallToolResult, map[string]any, error) {
	if s.deps.Currency == nil {
		return nil, map[string]any{"rates": []any{}, "cached": false}, nil
	}
	rates, cached, err := s.deps.Currency.Rates(ctx)
	if err != nil {
		return nil, map[string]any{"rates": []any{}, "cached": false}, nil
	}
	return nil, map[string]any{"rates": rates, "cached": cached}, nil
}

// ── set_workspace_currency ──────────────────────────────────────────────────────

type setWorkspaceCurrencyIn struct {
	WorkspaceID string `json:"workspace_id" jsonschema:"ID workspace (dari list_workspaces)"`
	Currency    string `json:"currency" jsonschema:"Kod mata wang, cth MYR, USD, SGD (default MYR)"`
}

// setWorkspaceCurrency sets a workspace's settlement currency. The change applies to
// new products/sales going forward; historical sales keep their original
// currency (never auto-converted — legal/accounting requirement).
func (s *Server) setWorkspaceCurrency(
	ctx context.Context, _ *mcp.CallToolRequest, in setWorkspaceCurrencyIn,
) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #645: write gate = API-key write scope AND manage-or-admin role.
	if _, err := s.requireWorkspaceWriteRole(ctx, in.WorkspaceID); err != nil {
		return nil, nil, err
	}
	if !dbstore.IsSupportedCurrency(in.Currency) {
		return nil, nil, errors.New("mata wang tidak disokong")
	}
	sp, err := s.workspacesStore()
	if err != nil {
		return nil, nil, err
	}
	st, err := sp.Settings(ctx, in.WorkspaceID)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	st.Currency = in.Currency
	if err := sp.SaveSettings(ctx, st); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{"status": "saved", "workspace_id": in.WorkspaceID, "currency": in.Currency}, nil
}
