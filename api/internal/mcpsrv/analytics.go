// MCP tools: form & product analytics for AI agents.
package mcpsrv

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type formStatsOut struct {
	Title string `json:"title"`
	Slug  string `json:"slug"`
	// Mode is the form's collection mode ("payment" | "response", issue #598).
	// It is carried so an agent labels `submissions` honestly instead of
	// reading the figure as literal submission rows for every form.
	Mode           string  `json:"mode"`
	Submissions    int64   `json:"submissions"`
	PaidCount      int64   `json:"paid_count"`
	ConfirmedCount int64   `json:"confirmed_count"`
	FailedCount    int64   `json:"failed_count"`
	PendingCount   int64   `json:"pending_count"`
	// Issue #518: refundedCount + refundedRM surface refunds, and revenueRM is
	// NET of refunds (a partial refund reduces reported revenue).
	RefundedCount int64   `json:"refunded_count"`
	RefundedRM    float64 `json:"refunded_rm"`
	RevenueRM     float64 `json:"revenue_rm"`
}

type productStatsOut struct {
	Name          string  `json:"name"`
	UnitsSold     int64   `json:"units_sold"`
	PaidUnits     int64   `json:"paid_units"`
	RevenueRM     float64 `json:"revenue_rm"`
	PaidRevenueRM float64 `json:"paid_revenue_rm"`
}

func (s *Server) formAnalytics(
	ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn,
) (*mcp.CallToolResult, *listOutput[formStatsOut], error) {
	accs, err := s.accountScopes(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	// Issue #307: analytics span all of the user's workspaces.
	out := make([]formStatsOut, 0)
	for _, acc := range accs {
		stats, err := s.deps.Sales.FormAnalytics(ctx, acc)
		if err != nil {
			return nil, nil, err
		}
		for _, f := range stats {
			out = append(out, formStatsOut{
				Title: f.Title, Slug: f.Slug, Mode: f.Mode,
				Submissions: f.Submissions, PaidCount: f.PaidCount,
				ConfirmedCount: f.ConfirmedCnt, FailedCount: f.FailedCount,
				PendingCount: f.PendingCount, RefundedCount: f.RefundedCount,
				RefundedRM: float64(f.RefundedSen) / 100, RevenueRM: float64(f.RevenueSen) / 100,
			})
		}
	}
	return nil, newListOut(out), nil
}

func (s *Server) productAnalytics(
	ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn,
) (*mcp.CallToolResult, *listOutput[productStatsOut], error) {
	accs, err := s.accountScopes(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	// Issue #307: analytics span all of the user's workspaces.
	out := make([]productStatsOut, 0)
	for _, acc := range accs {
		stats, err := s.deps.Sales.ProductAnalytics(ctx, acc)
		if err != nil {
			return nil, nil, err
		}
		for _, p := range stats {
			out = append(out, productStatsOut{
				Name: p.Name, UnitsSold: p.UnitsSold, PaidUnits: p.PaidUnits,
				RevenueRM: float64(p.RevenueSen) / 100, PaidRevenueRM: float64(p.PaidRevenue) / 100,
			})
		}
	}
	return nil, newListOut(out), nil
}
