// MCP tools: form & product analytics for AI agents.
package mcpsrv

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type formStatsOut struct {
	Title          string  `json:"title"`
	Slug           string  `json:"slug"`
	Submissions    int64   `json:"submissions"`
	PaidCount      int64   `json:"paid_count"`
	ConfirmedCount int64   `json:"confirmed_count"`
	FailedCount    int64   `json:"failed_count"`
	PendingCount   int64   `json:"pending_count"`
	RevenueRM      float64 `json:"revenue_rm"`
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
) (*mcp.CallToolResult, []formStatsOut, error) {
	acc, err := s.accountAndScope(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	stats, err := s.deps.Sales.FormAnalytics(ctx, acc)
	if err != nil {
		return nil, nil, err
	}
	out := make([]formStatsOut, 0, len(stats))
	for _, f := range stats {
		out = append(out, formStatsOut{
			Title: f.Title, Slug: f.Slug,
			Submissions: f.Submissions, PaidCount: f.PaidCount,
			ConfirmedCount: f.ConfirmedCnt, FailedCount: f.FailedCount,
			PendingCount: f.PendingCount, RevenueRM: float64(f.RevenueSen) / 100,
		})
	}
	return nil, out, nil
}

func (s *Server) productAnalytics(
	ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn,
) (*mcp.CallToolResult, []productStatsOut, error) {
	acc, err := s.accountAndScope(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	stats, err := s.deps.Sales.ProductAnalytics(ctx, acc)
	if err != nil {
		return nil, nil, err
	}
	out := make([]productStatsOut, 0, len(stats))
	for _, p := range stats {
		out = append(out, productStatsOut{
			Name: p.Name, UnitsSold: p.UnitsSold, PaidUnits: p.PaidUnits,
			RevenueRM: float64(p.RevenueSen) / 100, PaidRevenueRM: float64(p.PaidRevenue) / 100,
		})
	}
	return nil, out, nil
}
