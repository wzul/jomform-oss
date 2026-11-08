// MCP tools: AI spam review queue (issue #245).
//
// Merchants' agents can list spam-flagged submissions/sales and un-spam a
// false positive. A spam-flagged sale that receives a successful payment is
// automatically un-spammed by the payment callback (no agent action needed).
package mcpsrv

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type spamSubmissionOut struct {
	ID         string         `json:"id"`
	FormID     string         `json:"form_id"`
	Data       map[string]any `json:"data"`
	SpamReason string         `json:"spam_reason,omitempty"`
	CreatedAt  string         `json:"created_at"`
}

type spamSaleOut struct {
	RefCode    string `json:"ref_code"`
	Status     string `json:"status"`
	TotalSen   int64  `json:"total_sen"`
	Buyer      string `json:"buyer"`
	BuyerEmail string `json:"buyer_email,omitempty"`
	FormID     string `json:"form_id,omitempty"`
	SpamReason string `json:"spam_reason,omitempty"`
	CreatedAt  string `json:"created_at"`
}

type listSpamIn struct {
	Page  int `json:"page,omitempty" jsonschema:"Optional — page (starting at 1)"`
	Limit int `json:"limit,omitempty" jsonschema:"Optional — limit per page (1-100, default 20)"`
}

// listSpamSubmissions returns one page of spam-flagged response submissions,
// newest first, plus the total count. Issue #307: spans all the user's workspaces.
// Read-only.
func (s *Server) listSpamSubmissions(ctx context.Context, _ *mcp.CallToolRequest, in listSpamIn) (*mcp.CallToolResult, map[string]any, error) {
	accs, err := s.accountScopes(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	if s.deps.Submissions == nil {
		return nil, nil, fmt.Errorf("submissions not available")
	}
	page, limit := in.Page, in.Limit
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	subs, total, err := s.deps.Submissions.ListSpamByAccountsPage(ctx, accs, page, limit)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	out := make([]spamSubmissionOut, 0, len(subs))
	for _, sub := range subs {
		reason := ""
		if sub.SpamReason != nil {
			reason = *sub.SpamReason
		}
		out = append(out, spamSubmissionOut{
			ID: sub.ID, FormID: sub.FormID, Data: sub.Data,
			SpamReason: reason, CreatedAt: sub.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	return nil, map[string]any{
		"items": out, "page": page, "limit": limit, "total": total,
	}, nil
}

type unspamSubmissionIn struct {
	ID string `json:"id" jsonschema:"Submission ID (from list_spam_submissions)"`
}

// unspamSubmission clears the spam flag on a false-positive submission.
// Write tool (manage/admin role). Issue #307: the submission may live in any
// of the user's workspaces (up to the caller's role per workspace).
func (s *Server) unspamSubmission(ctx context.Context, _ *mcp.CallToolRequest, in unspamSubmissionIn) (*mcp.CallToolResult, map[string]any, error) {
	if s.deps.Submissions == nil {
		return nil, nil, fmt.Errorf("submissions not available")
	}
	if in.ID == "" {
		return nil, nil, fmt.Errorf("id is required")
	}
	acc, err := s.resolveEntityAccount(ctx, func(acc string) error {
		sub, err := s.deps.Submissions.ByID(ctx, in.ID)
		if err != nil || sub == nil {
			return errNotOwned
		}
		if sub.AccountID != acc {
			return errNotOwned
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if err := s.requireWriteRoleIn(ctx, acc); err != nil {
		return nil, nil, err
	}
	if err := s.deps.Submissions.UnSpam(ctx, in.ID); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]any{"status": "unspammed", "id": in.ID}, nil
}

// listSpamSales returns one page of spam-flagged sales, newest first, plus
// the total count. Issue #307: spans all the user's workspaces. Read-only.
func (s *Server) listSpamSales(ctx context.Context, _ *mcp.CallToolRequest, in listSpamIn) (*mcp.CallToolResult, map[string]any, error) {
	accs, err := s.accountScopes(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	if s.deps.Sales == nil {
		return nil, nil, fmt.Errorf("sales not available")
	}
	page, limit := in.Page, in.Limit
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	sales, total, err := s.deps.Sales.ListSpamByAccountsPage(ctx, accs, page, limit)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	out := make([]spamSaleOut, 0, len(sales))
	for _, sale := range sales {
		reason := ""
		if sale.SpamReason != nil {
			reason = *sale.SpamReason
		}
		out = append(out, spamSaleOut{
			RefCode: sale.RefCode, Status: sale.Status, TotalSen: sale.TotalSen,
			Buyer: sale.Client.FullName, BuyerEmail: sale.Client.Email, FormID: sale.FormID,
			SpamReason: reason, CreatedAt: sale.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	return nil, map[string]any{
		"items": out, "page": page, "limit": limit, "total": total,
	}, nil
}

type unspamSaleIn struct {
	RefCode string `json:"ref_code" jsonschema:"Sale reference (from list_spam_sales)"`
}

// unspamSale clears the spam flag on a false-positive sale. Write tool
// (manage/admin role). Issue #307: the sale may live in any of the user's workspaces.
func (s *Server) unspamSale(ctx context.Context, _ *mcp.CallToolRequest, in unspamSaleIn) (*mcp.CallToolResult, map[string]any, error) {
	if s.deps.Sales == nil {
		return nil, nil, fmt.Errorf("sales not available")
	}
	if in.RefCode == "" {
		return nil, nil, fmt.Errorf("ref_code is required")
	}
	acc, err := s.requireSaleWrite(ctx, in.RefCode)
	if err != nil {
		return nil, nil, err
	}
	sale, err := s.deps.Sales.ByRefCode(ctx, in.RefCode)
	if err != nil || sale.AccountID != acc {
		return nil, nil, fmt.Errorf("sale not found")
	}
	if err := s.deps.Sales.UnSpam(ctx, sale.ID); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]any{"status": "unspammed", "ref_code": in.RefCode}, nil
}
