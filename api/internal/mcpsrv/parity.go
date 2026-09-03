// Feature-parity tools — dashboard operations exposed to merchant agents.
package mcpsrv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// ── Products update ──────────────────────────────────────────────────────

type updateProductIn struct {
	ProductID           string        `json:"product_id" jsonschema:"ID produk"`
	Name                string        `json:"name,omitempty" jsonschema:"Nama baharu (optional)"`
	Description         string        `json:"description,omitempty" jsonschema:"Penerangan baharu (optional)"`
	PriceSen            *int64        `json:"price_sen,omitempty" jsonschema:"Harga baharu dalam sen (optional)"`
	Images              []string      `json:"images,omitempty" jsonschema:"Gambar (URL) — set penuh; kosong = kosongkan" `
	Stock               *int          `json:"stock,omitempty" jsonschema:"Stok baharu (optional); omit = tak berubah" `
	Active              *bool         `json:"active,omitempty" jsonschema:"Aktifkan/Nyahaktif"`
	Type                string        `json:"type,omitempty" jsonschema:"one_time atau subscription (optional)"`
	BillingPeriod       string        `json:"billing_period,omitempty" jsonschema:"month atau year untuk subscription (optional)"`
	TrialDays           *int          `json:"trial_days,omitempty" jsonschema:"Hari percubaan (subscription; optional)"`
	Variations          []variationIn `json:"variations,omitempty" jsonschema:"Set penuh variasi (name, price_sen, stock, images, sort_order). Kosong = produk simple; omit = tak berubah."`
	SubscriptionCharges **int         `json:"subscription_charges,omitempty" jsonschema:"Bilangan caj (subscription; omit = tak berubah, null = infinite, N = caj N kali)"`
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
	var descPtr *string
	if in.Description != "" {
		descPtr = &in.Description
	}
	stockPtr := in.Stock
	if stockPtr != nil && *stockPtr < 0 {
		*stockPtr = 0
	}
	var typPtr *string
	if in.Type != "" {
		if in.Type != dbstore.SaleTypeOneTime && in.Type != dbstore.SaleTypeSubscription {
			return nil, nil, fmt.Errorf("type mesti one_time|subscription")
		}
		typPtr = &in.Type
	}
	var periodPtr *string
	if in.BillingPeriod != "" {
		if in.BillingPeriod != "month" && in.BillingPeriod != "year" {
			return nil, nil, fmt.Errorf("billing_period mesti month|year")
		}
		periodPtr = &in.BillingPeriod
	}
	if in.TrialDays != nil && *in.TrialDays < 0 {
		return nil, nil, fmt.Errorf("trial_days mesti >= 0")
	}
	if in.SubscriptionCharges != nil && *in.SubscriptionCharges != nil && **in.SubscriptionCharges < 1 {
		return nil, nil, fmt.Errorf("subscription_charges mesti >= 1 (atau null untuk infinite)")
	}
	var imagesPtr *[]string
	if in.Images != nil {
		imagesPtr = &in.Images
	}
	// Validate variations (money stays integer sen; stock nil = unlimited).
	for _, v := range in.Variations {
		if v.Name == "" {
			return nil, nil, fmt.Errorf("variation name wajib")
		}
		if v.PriceSen < 0 {
			return nil, nil, fmt.Errorf("variation price_sen mesti >= 0")
		}
		if v.Stock != nil && *v.Stock < 0 {
			return nil, nil, fmt.Errorf("variation stok mesti >= 0")
		}
	}
	if err := s.deps.Products.Update(ctx, acc, in.ProductID, namePtr, descPtr,
		in.PriceSen, imagesPtr, stockPtr, in.Active, typPtr, periodPtr, in.TrialDays, in.SubscriptionCharges); err != nil {
		return nil, nil, err
	}
	// Wholesale-replace variations when the caller sent them (nil = keep).
	if in.Variations != nil && s.deps.ProductVariations != nil {
		vs := make([]*dbstore.ProductVariation, 0, len(in.Variations))
		for i, v := range in.Variations {
			order := v.SortOrder
			if order == 0 {
				order = i
			}
			vs = append(vs, &dbstore.ProductVariation{
				Name: v.Name, PriceSen: v.PriceSen, Stock: v.Stock,
				Images: v.Images, SortOrder: order,
			})
		}
		if err := s.deps.ProductVariations.ReplaceAll(ctx, in.ProductID, vs); err != nil {
			return nil, nil, err
		}
	}
	return nil, map[string]string{"status": "updated", "product_id": in.ProductID}, nil
}

// listProductVariationsIn is the input for the list_product_variations tool.
type listProductVariationsIn struct {
	ProductID string `json:"product_id" jsonschema:"ID produk"`
}

// listProductVariations returns a product's variations (per-variation
// image/price/stock) — MCP parity for the dashboard editor (issue #151).
func (s *Server) listProductVariations(
	ctx context.Context, _ *mcp.CallToolRequest, in listProductVariationsIn,
) (*mcp.CallToolResult, []variationOut, error) {
	acc, err := s.accountAndScope(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	if in.ProductID == "" {
		return nil, nil, fmt.Errorf("product_id wajib")
	}
	// Ownership check: the product must belong to the account.
	if _, err := s.deps.Products.Get(ctx, acc, in.ProductID); err != nil {
		return nil, nil, fmt.Errorf("produk tidak dijumpai")
	}
	if s.deps.ProductVariations == nil {
		return nil, []variationOut{}, nil
	}
	vs, err := s.deps.ProductVariations.ListByProduct(ctx, in.ProductID)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, toVariationOuts(vs), nil
}

// variationOut is the MCP output shape for one product variation.
type variationOut struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	PriceSen  int64    `json:"price_sen"`
	Stock     *int     `json:"stock,omitempty"`
	Images    []string `json:"images,omitempty"`
	SortOrder int      `json:"sort_order"`
}

// toVariationOuts maps store variations to the MCP output shape.
func toVariationOuts(vs []*dbstore.ProductVariation) []variationOut {
	if len(vs) == 0 {
		return nil
	}
	out := make([]variationOut, 0, len(vs))
	for _, v := range vs {
		out = append(out, variationOut{
			ID: v.ID, Name: v.Name, PriceSen: v.PriceSen, Stock: v.Stock,
			Images: v.Images, SortOrder: v.SortOrder,
		})
	}
	return out
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

// deleteFormMCPSoft soft-deletes a form (parity with REST DELETE
// /v1/rest/forms/{id} and the dashboard trash). Registered as delete_form.
func (s *Server) deleteFormMCPSoft(
	ctx context.Context, _ *mcp.CallToolRequest, in formRefIn,
) (*mcp.CallToolResult, map[string]string, error) {
	acc, err := s.accountAndScope(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	if err := s.deps.Forms.SoftDelete(ctx, acc, in.FormID); err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"status": "deleted", "form_id": in.FormID}, nil
}

// bulkFormsMCP mirrors POST /v1/rest/forms/bulk: publish|draft|delete over
// many ids in one call (dedup, ≤500), returning how many actually changed.
type bulkFormsIn struct {
	Action string   `json:"action" jsonschema:"publish | draft | delete"`
	IDs    []string `json:"ids" jsonschema:"Senarai form id (maks 500)"`
}

func (s *Server) bulkFormsMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in bulkFormsIn,
) (*mcp.CallToolResult, map[string]any, error) {
	acc, err := s.accountAndScope(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	if in.Action != "publish" && in.Action != "draft" && in.Action != "delete" {
		return nil, nil, errors.New("action mesti publish|draft|delete")
	}
	if len(in.IDs) == 0 {
		return nil, nil, errors.New("ids wajib")
	}
	if len(in.IDs) > 500 {
		return nil, nil, errors.New("ids terlalu banyak (maks 500)")
	}
	seen := make(map[string]struct{}, len(in.IDs))
	ids := make([]string, 0, len(in.IDs))
	for _, id := range in.IDs {
		if _, dup := seen[id]; dup || id == "" {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, nil, errors.New("ids mesti diisi")
	}
	var updated int64
	switch in.Action {
	case "publish", "draft":
		status := "published"
		if in.Action == "draft" {
			status = "draft"
		}
		updated, err = s.deps.Forms.SetStatusMany(ctx, acc, ids, status)
	default:
		updated, err = s.deps.Forms.SoftDeleteMany(ctx, acc, ids)
	}
	if err != nil {
		return nil, nil, err
	}
	return nil, map[string]any{"status": "ok", "action": in.Action, "updated": updated}, nil
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

// bulkProductsMCP archives many products in one call (parity with
// POST /v1/rest/products/bulk). Returns how many the account actually owned.
type bulkProductsIn struct {
	IDs []string `json:"ids" jsonschema:"Senarai produk id (maks 500)"`
}

func (s *Server) bulkProductsMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in bulkProductsIn,
) (*mcp.CallToolResult, map[string]any, error) {
	acc, err := s.accountAndScope(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	if len(in.IDs) == 0 {
		return nil, nil, errors.New("ids wajib")
	}
	if len(in.IDs) > 500 {
		return nil, nil, errors.New("ids terlalu banyak (maks 500)")
	}
	seen := make(map[string]struct{}, len(in.IDs))
	ids := make([]string, 0, len(in.IDs))
	for _, id := range in.IDs {
		if _, dup := seen[id]; dup || id == "" {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, nil, errors.New("ids mesti diisi")
	}
	updated, err := s.deps.Products.ArchiveMany(ctx, acc, ids)
	if err != nil {
		return nil, nil, err
	}
	return nil, map[string]any{"status": "ok", "action": "archive", "updated": updated}, nil
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
