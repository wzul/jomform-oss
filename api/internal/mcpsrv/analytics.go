// MCP tools: form & product analytics for AI agents.
package mcpsrv

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/dbstore"
)

type formStatsOut struct {
	Title string `json:"title"`
	Slug  string `json:"slug"`
	// Mode is the form's collection mode ("payment" | "response", issue #598).
	// It is carried so an agent labels `submissions` honestly instead of
	// reading the figure as literal submission rows for every form.
	Mode           string `json:"mode"`
	Submissions    int64  `json:"submissions"`
	PaidCount      int64  `json:"paid_count"`
	ConfirmedCount int64  `json:"confirmed_count"`
	FailedCount    int64  `json:"failed_count"`
	PendingCount   int64  `json:"pending_count"`
	// Issue #518: refundedCount + refundedSen surface refunds, and revenueSen is
	// NET of refunds (a partial refund reduces reported revenue).
	//
	// Issue #185 (card t_93844723): money leaves this tool in integer MINOR
	// units in the currency each sale was RECORDED in — never a float under an
	// `_rm` name, which asserted ringgit over a figure that can be US dollars.
	// revenueSen and refundedSen are sums ACROSS currencies (the tool spans all
	// of the caller's workspaces), so they carry no single label and an agent
	// must not put one symbol over them; the per-currency maps hold the same
	// figures keyed by ISO code (e.g. {"USD": 2490}). A workspace that switched
	// currency keeps each sale in the money it was paid in, which is why one
	// form can hold more than one entry.
	RefundedCount int64 `json:"refunded_count"`
	RevenueSen    int64 `json:"revenue_sen"`
	RefundedSen   int64 `json:"refunded_sen"`
	// Card t_8064a965: the fee inside revenue_sen and what the merchant keeps
	// (revenue_sen − fees_sen), so an agent reading the same rollup the
	// dashboard shows cannot quote the buyer-paid total as income. Both are
	// integer minor units and split per currency like revenue_sen.
	FeesSen              int64            `json:"fees_sen"`
	NetRevenueSen        int64            `json:"net_revenue_sen"`
	RevenueByCurrency    map[string]int64 `json:"revenue_by_currency,omitempty"`
	FeesByCurrency       map[string]int64 `json:"fees_by_currency,omitempty"`
	NetRevenueByCurrency map[string]int64 `json:"net_revenue_by_currency,omitempty"`
	RefundedByCurrency   map[string]int64 `json:"refunded_by_currency,omitempty"`
}

type productStatsOut struct {
	Name      string `json:"name"`
	UnitsSold int64  `json:"units_sold"`
	PaidUnits int64  `json:"paid_units"`
	// Same money rule as formStatsOut (issue #185): integer minor units, split
	// by the currency each sale was recorded in. revenueSen is the GROSS line
	// value of every sale the product appears in (any status) and paidRevenueSen
	// is the paid/confirmed total NET of refunds — the two figures the dashboard
	// and GET /v1/rest/analytics/products report.
	RevenueSen            int64            `json:"revenue_sen"`
	PaidRevenueSen        int64            `json:"paid_revenue_sen"`
	RevenueByCurrency     map[string]int64 `json:"revenue_by_currency,omitempty"`
	PaidRevenueByCurrency map[string]int64 `json:"paid_revenue_by_currency,omitempty"`
}

// formStatsOutFrom maps the store's rollup onto the wire shape. Pure, so the
// payload an agent receives can be pinned by a test without a server or a DB.
func formStatsOutFrom(f dbstore.FormStats) formStatsOut {
	return formStatsOut{
		Title: f.Title, Slug: f.Slug, Mode: f.Mode,
		Submissions: f.Submissions, PaidCount: f.PaidCount,
		ConfirmedCount: f.ConfirmedCnt, FailedCount: f.FailedCount,
		PendingCount: f.PendingCount, RefundedCount: f.RefundedCount,
		RevenueSen: f.RevenueSen, RefundedSen: f.RefundedSen,
		FeesSen: f.FeesSen, NetRevenueSen: f.NetRevenueSen,
		RevenueByCurrency:    f.RevenueByCurrency,
		FeesByCurrency:       f.FeesByCurrency,
		NetRevenueByCurrency: f.NetRevenueByCurrency,
		RefundedByCurrency:   f.RefundedByCurrency,
	}
}

// productStatsOutFrom is formStatsOutFrom for the per-product rollup.
func productStatsOutFrom(p dbstore.ProductStats) productStatsOut {
	return productStatsOut{
		Name: p.Name, UnitsSold: p.UnitsSold, PaidUnits: p.PaidUnits,
		RevenueSen: p.RevenueSen, PaidRevenueSen: p.PaidRevenue,
		RevenueByCurrency:     p.RevenueByCurrency,
		PaidRevenueByCurrency: p.PaidRevenueByCurrency,
	}
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
			out = append(out, formStatsOutFrom(f))
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
			out = append(out, productStatsOutFrom(p))
		}
	}
	return nil, newListOut(out), nil
}
