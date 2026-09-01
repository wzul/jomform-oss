// Feature-parity tools — dashboard operations exposed to merchant agents.
package mcpsrv

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// ── Products update ──────────────────────────────────────────────────────

type updateProductIn struct {
	ProductID string `json:"product_id" jsonschema:"ID produk"`
	Name      string `json:"name,omitempty" jsonschema:"Nama baharu (optional)"`
	PriceSen  *int64 `json:"price_sen,omitempty" jsonschema:"Harga baharu dalam sen (optional)"`
	Stock     *int   `json:"stock,omitempty" jsonschema:"Stok baharu (optional); omit = tak berubah"`
	Active    *bool  `json:"active,omitempty" jsonschema:"Aktifkan/Nyahaktif"`
}

func (s *Server) updateProduct(
	ctx context.Context, _ *mcp.CallToolRequest, in updateProductIn,
) (*mcp.CallToolResult, map[string]string, error) {
	acc, err := s.accountAndScope(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	var namePtr *string
	if in.Name != "" {
		namePtr = &in.Name
	}
	stockPtr := in.Stock
	if stockPtr != nil && *stockPtr < 0 {
		*stockPtr = 0
	}
	if err := s.deps.Products.Update(ctx, acc, in.ProductID, namePtr, in.PriceSen, stockPtr, in.Active); err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"status": "updated", "product_id": in.ProductID}, nil
}

// ── Forms upsert + publish + delete ─────────────────────────────────────

type upsertFormIn struct {
	FormID string         `json:"form_id,omitempty" jsonschema:"Kosong = borang baru; isi = kemaskini"`
	Slug   string         `json:"slug" jsonschema:"Slug URL unik"`
	Title  string         `json:"title" jsonschema:"Tajuk borang"`
	Config map[string]any `json:"config,omitempty" jsonschema:"Block config — ambil contoh dari get_form"`
}

func (s *Server) upsertForm(
	ctx context.Context, _ *mcp.CallToolRequest, in upsertFormIn,
) (*mcp.CallToolResult, map[string]string, error) {
	acc, err := s.accountAndScope(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	if in.Slug == "" || in.Title == "" {
		return nil, nil, errors.New("slug dan title wajib")
	}
	if in.FormID == "" {
		f := &dbstore.Form{
			AccountID: acc,
			Slug:      in.Slug,
			Title:     in.Title,
			Config:    orEmptyMap(in.Config),
			Theme:     orEmptyMap(nil), // never NULL: theme is NOT NULL
			Status:    "draft",
		}
		if err := s.deps.Forms.Create(ctx, f); err != nil {
			return nil, nil, err
		}
		return nil, map[string]string{"status": "created", "form_id": f.ID, "slug": f.Slug}, nil
	}
	// Update store treats nil maps as "leave unchanged", so an omitted
	// config (and the nil theme below) keep the stored values intact.
	if err := s.deps.Forms.Update(ctx, acc, in.FormID, in.Title, in.Config, nil); err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"status": "updated", "form_id": in.FormID}, nil
}

// orEmptyMap normalises a nil map to an empty one.
func orEmptyMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// jsonValid reports whether s parses as JSON (used for space menu_json).
func jsonValid(s string) bool {
	var v any
	return json.Unmarshal([]byte(s), &v) == nil
}

func (s *Server) publishFormMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in formRefIn,
) (*mcp.CallToolResult, map[string]string, error) {
	acc, err := s.accountAndScope(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	if err := s.deps.Forms.SetStatus(ctx, acc, in.FormID, "published"); err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"status": "published", "form_id": in.FormID}, nil
}

func (s *Server) deleteFormMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in formRefIn,
) (*mcp.CallToolResult, map[string]string, error) {
	acc, err := s.accountAndScope(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	if err := s.deps.Forms.SetStatus(ctx, acc, in.FormID, "draft"); err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"status": "unpublished", "form_id": in.FormID}, nil
}

// delete product alias to archive
func (s *Server) archiveProductMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in struct {
		ProductID string `json:"product_id" jsonschema:"ID produk"`
	},
) (*mcp.CallToolResult, map[string]string, error) {
	acc, err := s.accountAndScope(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	if err := s.deps.Products.Archive(ctx, acc, in.ProductID); err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"status": "archived", "product_id": in.ProductID}, nil
}

// ── Pass-through analytics aliases (agents get the same numbers as UI) ──

func (s *Server) formAnalyticsAlias(
	ctx context.Context, req *mcp.CallToolRequest, in emptyIn,
) (*mcp.CallToolResult, []formStatsOut, error) {
	return s.formAnalytics(ctx, req, in)
}

func (s *Server) productAnalyticsAlias(
	ctx context.Context, req *mcp.CallToolRequest, in emptyIn,
) (*mcp.CallToolResult, []productStatsOut, error) {
	return s.productAnalytics(ctx, req, in)
}

var _ = json.Marshal
