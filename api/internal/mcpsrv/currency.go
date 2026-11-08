// MCP parity tools: currency (issue #185). get_currency_rates exposes the
// BNM daily-cached exchange rates; convert_currency converts one amount with
// those rates (the only production caller of currency.Store.ConvertMinor, card
// t_467614db); set_workspace_currency sets a workspace's settlement currency
// (default MYR). Historical sales keep their original currency — a change
// applies to new products/sales going forward.
package mcpsrv

import (
	"context"
	"errors"
	"fmt"
	"strings"

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

// ── convert_currency ─────────────────────────────────────────────────────────

type convertCurrencyIn struct {
	From        string `json:"from" jsonschema:"ISO 4217 code of the source currency (e.g. JPY, USD, MYR)"`
	To          string `json:"to" jsonschema:"ISO 4217 code of the target currency (e.g. MYR, USD)"`
	AmountMinor int64  `json:"amount_minor" jsonschema:"Amount in the currency's smallest unit (hundredths): 10000 = 100.00. Integer, not a decimal."`
}

// normalizeCurrencyCode upper-cases an ISO 4217 code and rejects anything that is
// not three ASCII letters. The rate store upper-cases on lookup anyway; the shape
// check is here so a caller gets "from/to mesti kod mata wang" instead of a
// generic "rate not available" for what is really a malformed argument.
func normalizeCurrencyCode(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	if len(code) != 3 {
		return ""
	}
	for i := 0; i < len(code); i++ {
		if code[i] < 'A' || code[i] > 'Z' {
			return ""
		}
	}
	return code
}

// convertCurrency converts an amount with the cached BNM rates.
//
// This is the ONE production caller of currency.Store.ConvertMinor (card
// t_467614db). Before it, the rate store had no consumer that multiplied an
// amount by a rate at all, while the Settings hint, this package's neighbour
// description and the docs all claimed conversion worked — so the claim was
// false in every direction. ConvertMinor already divides the published per-100
// quote by its unit exactly once (currency.toRates); this handler must multiply
// by the rate only and must never divide by unit again.
//
// Display/reporting only: nothing here writes, and no stored sale is re-converted.
func (s *Server) convertCurrency(
	ctx context.Context, _ *mcp.CallToolRequest, in convertCurrencyIn,
) (*mcp.CallToolResult, map[string]any, error) {
	from, to := normalizeCurrencyCode(in.From), normalizeCurrencyCode(in.To)
	if from == "" {
		return nil, nil, errors.New("from must be a three-letter ISO 4217 currency code (e.g. JPY, USD, MYR)")
	}
	if to == "" {
		return nil, nil, errors.New("to must be a three-letter ISO 4217 currency code (e.g. MYR, USD)")
	}
	if s.deps.Currency == nil {
		return nil, nil, errors.New("currency rates not available: BNM cache not connected")
	}
	converted, ok := s.deps.Currency.ConvertMinor(ctx, from, to, in.AmountMinor)
	if !ok {
		return nil, nil, fmt.Errorf("rate %s → %s is not in the BNM cache — use get_currency_rates for the list of available codes", from, to)
	}
	return nil, map[string]any{
		"from":            from,
		"to":              to,
		"amount_minor":    in.AmountMinor,
		"converted_minor": converted,
	}, nil
}

// ── set_workspace_currency ──────────────────────────────────────────────────────

type setWorkspaceCurrencyIn struct {
	WorkspaceID string `json:"workspace_id" jsonschema:"Workspace ID (from list_workspaces)"`
	Currency    string `json:"currency" jsonschema:"Currency code, e.g. MYR, USD, SGD (default MYR)"`
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
		return nil, nil, errors.New("currency not supported")
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
