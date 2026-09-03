// MCP tool: list media library (issue #180).
package mcpsrv

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mediaOut struct {
	ID        string `json:"id"`
	FormID    string `json:"form_id,omitempty"`
	Key       string `json:"key"`
	URL       string `json:"url"`
	Mime      string `json:"mime"`
	Size      int64  `json:"size"`
	Source    string `json:"source"`
	CreatedAt string `json:"created_at"`
}

type listMediaIn struct {
	FormID string `json:"form_id,omitempty" jsonschema:"Optional — tapis media mengikut borang (id dari list_forms)"`
	Source string `json:"source,omitempty" jsonschema:"Optional — merchant|customer (lalai: semua)"`
	Page   int    `json:"page,omitempty" jsonschema:"Optional — muka surat (mulai 1)"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Optional — had setiap muka (1-100, lalai 20)"`
}

// listMedia returns one page of the account's uploaded media, newest first,
// optionally filtered by form and source. Account-scoped (ownership enforced
// inside the store query). Read-only.
func (s *Server) listMedia(ctx context.Context, _ *mcp.CallToolRequest, in listMediaIn) (*mcp.CallToolResult, map[string]any, error) {
	accountID, err := s.accountAndScope(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	if s.deps.Media == nil {
		return nil, nil, fmt.Errorf("media tidak tersedia")
	}
	if in.Source != "" && in.Source != "merchant" && in.Source != "customer" {
		return nil, nil, fmt.Errorf("source mesti merchant|customer")
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
	items, total, err := s.deps.Media.ListPage(ctx, accountID, in.FormID, in.Source, page, limit)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	out := make([]mediaOut, 0, len(items))
	for _, m := range items {
		out = append(out, mediaOut{
			ID: m.ID, FormID: m.FormID, Key: m.Key, URL: m.URL,
			Mime: m.Mime, Size: m.Size, Source: m.Source,
			CreatedAt: m.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	return nil, map[string]any{
		"items": out, "page": page, "limit": limit, "total": total,
	}, nil
}
