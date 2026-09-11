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

	qrcode "github.com/skip2/go-qrcode"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// ── generate_qr ────────────────────────────────────────────────────────────────

type generateQRIn struct {
	// FormSlug is the published form's slug within the workspace (e.g. "menu").
	FormSlug string `json:"form_slug" jsonschema:"Slug borang diterbitkan (cth 'menu')"`
	// Table is an optional table number to encode into the QR (?table=N) so
	// the order carries the table and the merchant knows where to serve.
	Table string `json:"table,omitempty" jsonschema:"Nombor meja (pilihan) untuk dikodkan dalam QR, cth '8'"`
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
		return nil, generateQROut{}, errors.New("form_slug diperlukan")
	}
	// Verify the form exists + is published in this account.
	if s.deps.Forms == nil {
		return nil, generateQROut{}, errors.New("forms tidak tersedia")
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
		return nil, generateQROut{}, errors.New("borang tidak dijumpai")
	}
	if form.Status != dbstore.FormStatusPublished {
		return nil, generateQROut{}, errors.New("borang mesti diterbitkan dahulu")
	}
	target := s.formPublicURLForAccount(ctx, accountID, in.FormSlug)
	if target == "" {
		return nil, generateQROut{}, errors.New("URL awam borang tidak dapat diselesaikan")
	}
	if table := strings.TrimSpace(in.Table); table != "" {
		target = target + "?table=" + urlQueryEscape(table)
	}
	png, qerr := qrcode.Encode(target, qrcode.Medium, 512)
	if qerr != nil {
		return nil, generateQROut{}, fmt.Errorf("gagal menjana QR: %w", qerr)
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
	FormID string `json:"form_id" jsonschema:"ID borang (dari list_forms atau upsert_form)"`
}

func (s *Server) getMenuPage(
	ctx context.Context, _ *mcp.CallToolRequest, in menuPageRefIn,
) (*mcp.CallToolResult, *dbstore.MenuPage, error) {
	if strings.TrimSpace(in.FormID) == "" {
		return nil, nil, errors.New("form_id diperlukan")
	}
	// Issue #307: resolve the owning account across the user's workspaces (read
	// scope is enforced by resolveFormAccount).
	acc, err := s.resolveFormAccount(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	if s.deps.Forms == nil {
		return nil, nil, errors.New("forms tidak tersedia")
	}
	f, err := s.deps.Forms.GetByID(ctx, acc, in.FormID)
	if err != nil || f == nil {
		return nil, nil, errors.New("borang tidak dijumpai")
	}
	if menu := dbstore.MenuFromConfig(f.Config); menu != nil {
		return nil, menu, nil
	}
	return nil, &dbstore.MenuPage{}, nil
}

type setMenuPageIn struct {
	FormID  string `json:"form_id" jsonschema:"ID borang (dari list_forms atau upsert_form)"`
	Enabled bool   `json:"enabled" jsonschema:"true = borang ini jadi menu restoran (dipapar di URL borang); false = borang kembali normal"`
	// Categories groups product IDs under display names, e.g.
	// [{name:"Makanan", product_ids:["...","..."]}].
	Categories []dbstore.MenuCategory `json:"categories,omitempty" jsonschema:"Kategori menu, cth [{\"name\":\"Makanan\",\"product_ids\":[\"id1\",\"id2\"]}]. Hanya produk yang dirujuk oleh blok produk borang ini dipaparkan — produk akaun lain tidak boleh bocor masuk."`
	// BestSeller is an optional product ID highlighted as the best-seller.
	BestSeller string `json:"best_seller,omitempty" jsonschema:"ID produk yang ditonjolkan sebagai best-seller (pilihan; mesti dirujuk oleh blok produk borang ini)"`
	// BestSellerIDs is an optional list of product IDs for the "Best Seller"
	// section (issue #485). When set it wins over the legacy single BestSeller
	// id. The menu renders these as 2-column square-image cards.
	BestSellerIDs []string `json:"best_seller_ids,omitempty" jsonschema:"Senarai ID produk untuk seksyen Best Seller (pilihan; mengatasi best_seller)"`
	// OrderFormSlug is an optional published form in the same account attached
	// to the menu so the menu checkout can collect extra order info (issue
	// #491/#493). Its field blocks define the extra checkout fields; the server
	// re-validates them. The buyer's name + email are always collected.
	OrderFormSlug string `json:"order_form_slug,omitempty" jsonschema:"Slug borang diterbitkan (dalam akaun sama) untuk maklumat tambahan pesanan (pilihan; kosong = checkout nama + emel sahaja). Medan borang itu dirender dalam checkout menu dan disahkan server-side."`
}

func (s *Server) setMenuPage(
	ctx context.Context, _ *mcp.CallToolRequest, in setMenuPageIn,
) (*mcp.CallToolResult, map[string]string, error) {
	if strings.TrimSpace(in.FormID) == "" {
		return nil, nil, errors.New("form_id diperlukan")
	}
	acc, err := s.requireFormWrite(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	if s.deps.Forms == nil {
		return nil, nil, errors.New("forms tidak tersedia")
	}
	f, err := s.deps.Forms.GetByID(ctx, acc, in.FormID)
	if err != nil || f == nil {
		return nil, nil, errors.New("borang tidak dijumpai")
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
