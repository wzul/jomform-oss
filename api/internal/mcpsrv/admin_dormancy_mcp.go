// admin_dormancy_report — the MCP mirror of GET /v1/admin/dormancy (card
// t_94e5cca3).
//
// AGENTS.md MCP FEATURE PARITY: every dashboard feature has a matching tool. The
// /admin dormancy section reads the same report this tool returns, from the same
// store and the same policy function (dormancy.Build), so an agent and an operator
// can never disagree about who is dormant.
//
// READ-ONLY, AND NAMED SO. Phase 1 of the dormancy policy deletes nothing and
// unpublishes nothing; the payload carries `read_only: true` and the policy
// sentence, so a caller cannot read this report as an instruction to act.
package mcpsrv

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/dormancy"
)

type adminDormancyReportIn struct {
	Limit int `json:"limit,omitempty" jsonschema:"Optional — rows to return (1-500, default 200), most dormant first"`
}

type adminDormancyReportOut struct {
	GeneratedAt time.Time      `json:"generated_at"`
	ReadOnly    bool           `json:"read_only"`
	Policy      string         `json:"policy"`
	Limit       int            `json:"limit"`
	Truncated   bool           `json:"truncated"`
	Candidates  int            `json:"candidates"`
	Excluded    int            `json:"excluded"`
	Nudgeable   int            `json:"nudgeable"`
	Counts      map[string]int `json:"counts"`
	Rows        []dormancy.Row `json:"rows"`
}

// adminDormancyReport returns the per-account dormancy report. Super-admin only,
// read-only, audited.
func (s *Server) adminDormancyReport(ctx context.Context, _ *mcp.CallToolRequest, in adminDormancyReportIn) (*mcp.CallToolResult, map[string]any, error) {
	if err := s.adminGate(ctx, "admin.dormancy_report"); err != nil {
		return nil, nil, err
	}
	if s.deps.Dormancy == nil {
		return nil, nil, fmt.Errorf("the dormancy store is not configured")
	}
	limit := in.Limit
	if limit <= 0 {
		limit = dbstore.DormancyReportLimitDefault
	}
	now := time.Now()
	rows, err := s.deps.Dormancy.DormancyReport(ctx, dbstore.DormancyReportOptions{Limit: limit, Now: now})
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	rep := dormancy.Build(rows, dormancy.Options{Now: now, Limit: limit})
	if rep.Limit > dbstore.DormancyReportLimitMax {
		rep.Limit = dbstore.DormancyReportLimitMax
	}
	counts := make(map[string]int, len(rep.Counts))
	for k, v := range rep.Counts {
		counts[string(k)] = v
	}
	return nil, map[string]any{
		"generated_at": rep.GeneratedAt.UTC().Format(time.RFC3339),
		"read_only":    true,
		"policy": "phase 1 of the dormancy policy: a read-only report and one nudge email. " +
			"No account is deleted, unpublished or flagged. Deletion (phase 3) and unpublishing (phase 2) " +
			"are separate, unimplemented decisions.",
		"limit":      rep.Limit,
		"truncated":  rep.Truncated,
		"candidates": rep.Candidates,
		"excluded":   rep.Excluded,
		"nudgeable":  rep.Nudgeable,
		"counts":     counts,
		"rows":       rep.Rows,
	}, nil
}
