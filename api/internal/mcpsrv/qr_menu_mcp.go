// Issue #438: QR ordering flow — MCP parity tools.
//
// Dashboard features → MCP:
//   - QR generation (GET /qr/{slug}.png + form-settings button) → generate_qr
//   - Restaurant-menu landing template (workspace root) → get_menu_page /
//     set_menu_page
//
// generate_qr builds the form's canonical public URL (workspace subdomain or
// custom domain), optionally folds in a ?table=N query param, and returns
// both the QR endpoint URL and a base64 PNG data URL the agent can render.
package mcpsrv

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	qrcode "github.com/skip2/go-qrcode"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// ── generate_qr ────────────────────────────────────────────────────────────────

type generateQRIn struct {
	// FormSlug is the published form's slug within the workspace (e.g. "menu").
	FormSlug string `json:"form_slug" jsonschema:"Slug of the published form (e.g. 'menu')"`
	// Table is an optional table number to encode into the QR (?table=N) so
	// the order carries the table and the merchant knows where to serve.
	Table string `json:"table,omitempty" jsonschema:"Table number (optional) to encode in the QR, e.g. '8'"`
}

type generateQROut struct {
	// QRURL is the on-platform QR endpoint (GET /qr/{slug}.png) the merchant
	// can embed/print.
	QRURL string `json:"qr_url"`
	// DataURL is a base64 PNG data URL of the QR code, ready to render.
	DataURL string `json:"data_url"`
	// TargetURL is the URL the QR encodes (the form's public URL, with
	// ?table=N when a table was given).
	TargetURL string `json:"target_url"`
}

func (s *Server) generateQR(
	ctx context.Context, _ *mcp.CallToolRequest, in generateQRIn,
) (*mcp.CallToolResult, generateQROut, error) {
	accountID, err := s.requireWriteRole(ctx)
	if err != nil {
		return nil, generateQROut{}, err
	}
	if strings.TrimSpace(in.FormSlug) == "" {
		return nil, generateQROut{}, errors.New("form_slug is required")
	}
	// Verify the form exists + is published in this account.
	if s.deps.Forms == nil {
		return nil, generateQROut{}, errors.New("forms not available")
	}
	forms, ferr := s.deps.Forms.ListByAccount(ctx, accountID)
	if ferr != nil {
		return nil, generateQROut{}, fmt.Errorf("db: %w", ferr)
	}
	var form *dbstore.Form
	for _, f := range forms {
		if f.Slug == in.FormSlug {
			form = f
			break
		}
	}
	if form == nil {
		return nil, generateQROut{}, errors.New("form not found")
	}
	if form.Status != dbstore.FormStatusPublished {
		return nil, generateQROut{}, errors.New("the form must be published first")
	}
	target := s.formPublicURLForAccount(ctx, accountID, in.FormSlug)
	if target == "" {
		return nil, generateQROut{}, errors.New("the form's public URL could not be resolved")
	}
	if table := strings.TrimSpace(in.Table); table != "" {
		target = target + "?table=" + urlQueryEscape(table)
	}
	png, qerr := qrcode.Encode(target, qrcode.Medium, 512)
	if qerr != nil {
		return nil, generateQROut{}, fmt.Errorf("failed to generate the QR: %w", qerr)
	}
	// The on-platform QR endpoint (same origin as the form's public URL).
	origin := target
	if i := strings.Index(origin, "/"); i > 0 {
		origin = origin[:i]
	}
	qrURL := origin + "/qr/" + urlPathEscape(in.FormSlug) + ".png"
	if table := strings.TrimSpace(in.Table); table != "" {
		qrURL = qrURL + "?table=" + urlQueryEscape(table)
	}
	return nil, generateQROut{
		QRURL:     qrURL,
		DataURL:   "data:image/png;base64," + base64.StdEncoding.EncodeToString(png),
		TargetURL: target,
	}, nil
}

// ── get_menu_page / set_menu_page (per-form, issue #497) ─────────────────────
//
// Before #497 the menu was a per-workspace landing template (get/set_menu_page
// took a workspace_id). It is now a PER-FORM setting: the config lives on the
// form (forms.config.menu) and the menu is served at the form's own public URL.
// Both tools therefore take a form_id.

type menuPageRefIn struct {
	FormID string `json:"form_id" jsonschema:"Form ID (from list_forms or upsert_form)"`
}

func (s *Server) getMenuPage(
	ctx context.Context, _ *mcp.CallToolRequest, in menuPageRefIn,
) (*mcp.CallToolResult, *dbstore.MenuPage, error) {
	if strings.TrimSpace(in.FormID) == "" {
		return nil, nil, errors.New("form_id is required")
	}
	// Issue #307: resolve the owning account across the user's workspaces (read
	// scope is enforced by resolveFormAccount).
	acc, err := s.resolveFormAccount(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	if s.deps.Forms == nil {
		return nil, nil, errors.New("forms not available")
	}
	f, err := s.deps.Forms.GetByID(ctx, acc, in.FormID)
	if err != nil || f == nil {
		return nil, nil, errors.New("form not found")
	}
	if menu := dbstore.MenuFromConfig(f.Config); menu != nil {
		return nil, menu, nil
	}
	return nil, &dbstore.MenuPage{}, nil
}

type setMenuPageIn struct {
	FormID  string `json:"form_id" jsonschema:"Form ID (from list_forms or upsert_form)"`
	Enabled bool   `json:"enabled" jsonschema:"true = this form becomes a restaurant menu (rendered at the form URL); false = form returns to normal"`
	// Categories groups product IDs under display names, e.g.
	// [{name:"Makanan", product_ids:["...","..."]}].
	Categories []dbstore.MenuCategory `json:"categories,omitempty" jsonschema:"Menu categories, e.g. [{\"name\":\"Food\",\"product_ids\":[\"id1\",\"id2\"]}]. Only products referenced by this form's product block are shown — products from other accounts cannot leak in."`
	// BestSeller is an optional product ID highlighted as the best-seller.
	BestSeller string `json:"best_seller,omitempty" jsonschema:"ID of the product featured as best-seller (optional; must be referenced by this form's product block)"`
	// BestSellerIDs is an optional list of product IDs for the "Best Seller"
	// section (issue #485). When set it wins over the legacy single BestSeller
	// id. The menu renders these as 2-column square-image cards.
	BestSellerIDs []string `json:"best_seller_ids,omitempty" jsonschema:"List of product IDs for the Best Seller section (optional; overrides best_seller)"`
	// OrderFormSlug is an optional published form in the same account attached
	// to the menu so the menu checkout can collect extra order info (issue
	// #491/#493). Its field blocks define the extra checkout fields; the server
	// re-validates them. The buyer's name + email are always collected.
	OrderFormSlug string `json:"order_form_slug,omitempty" jsonschema:"Slug of a published form (in the same account) for extra order information (optional; empty = name + email only at checkout). That form's fields are rendered in the menu checkout and validated server-side."`
}

func (s *Server) setMenuPage(
	ctx context.Context, _ *mcp.CallToolRequest, in setMenuPageIn,
) (*mcp.CallToolResult, map[string]string, error) {
	if strings.TrimSpace(in.FormID) == "" {
		return nil, nil, errors.New("form_id is required")
	}
	acc, err := s.requireFormWrite(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	if s.deps.Forms == nil {
		return nil, nil, errors.New("forms not available")
	}
	f, err := s.deps.Forms.GetByID(ctx, acc, in.FormID)
	if err != nil || f == nil {
		return nil, nil, errors.New("form not found")
	}
	menu := &dbstore.MenuPage{
		Enabled:       in.Enabled,
		Categories:    in.Categories,
		BestSeller:    in.BestSeller,
		BestSellerIDs: in.BestSellerIDs,
		OrderFormSlug: strings.TrimSpace(in.OrderFormSlug),
	}
	if menu.Categories == nil {
		menu.Categories = []dbstore.MenuCategory{}
	}
	// Write the menu under forms.config.menu, preserving the form's blocks and
	// every other config key (issue #497 — the menu is one more key on the
	// form, never a replacement for it). Disabling the menu removes the key so
	// the form renders normally again.
	next := dbstore.WithMenu(f.Config, menu)
	if err := s.deps.Forms.Update(ctx, acc, in.FormID, f.Title, next, nil); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{"status": "saved", "form_id": in.FormID}, nil
}

// urlQueryEscape percent-encodes a value for a query string.
func urlQueryEscape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(s, " ", "%20"), "&", "%26"), "=", "%3D")
}

// urlPathEscape percent-encodes a value for a URL path segment.
func urlPathEscape(s string) string {
	return strings.ReplaceAll(s, "/", "%2F")
}
