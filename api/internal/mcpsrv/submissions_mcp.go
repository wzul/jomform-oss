// MCP tool: list response-only form submissions (issue #171).
package mcpsrv

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type submissionOut struct {
	ID        string         `json:"id"`
	FormID    string         `json:"form_id"`
	Data      map[string]any `json:"data"`
	CreatedAt string         `json:"created_at"`
}

type listFormSubmissionsIn struct {
	FormID string `json:"form_id" jsonschema:"ID borang (dari list_forms/get_form)"`
	Page   int    `json:"page,omitempty" jsonschema:"Optional — muka surat (mulai 1)"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Optional — had setiap muka (1-100, lalai 20)"`
}

// listFormSubmissions returns one page of response-only submissions for a
// form, newest first, plus the total count. Issue #307: the form may live in
// any of the user's workspaces (ownership enforced per store query). Read-only.
func (s *Server) listFormSubmissions(ctx context.Context, _ *mcp.CallToolRequest, in listFormSubmissionsIn) (*mcp.CallToolResult, map[string]any, error) {
	if s.deps.Submissions == nil {
		return nil, nil, fmt.Errorf("submissions tidak tersedia")
	}
	if in.FormID == "" {
		return nil, nil, fmt.Errorf("form_id wajib")
	}
	acc, err := s.resolveFormAccount(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
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
	subs, total, err := s.deps.Submissions.ListByAccountsFormPage(ctx, []string{acc}, in.FormID, page, limit)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	out := make([]submissionOut, 0, len(subs))
	for _, sub := range subs {
		out = append(out, submissionOut{
			ID: sub.ID, FormID: sub.FormID, Data: sub.Data,
			CreatedAt: sub.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	return nil, map[string]any{
		"items": out, "page": page, "limit": limit, "total": total,
	}, nil
}
