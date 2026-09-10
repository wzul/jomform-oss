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

// ── get_menu_page / set_menu_page ────────────────────────────────────────────

type menuPageRefIn struct {
	WorkspaceID string `json:"workspace_id" jsonschema:"ID workspace (dari list_workspaces)"`
}

func (s *Server) getMenuPage(
	ctx context.Context, _ *mcp.CallToolRequest, in menuPageRefIn,
) (*mcp.CallToolResult, *dbstore.MenuPage, error) {
	if _, _, err := s.accessibleWorkspace(ctx, in.WorkspaceID); err != nil {
		return nil, nil, err
	}
	sp, err := s.workspacesStore()
	if err != nil {
		return nil, nil, err
	}
	st, err := sp.Settings(ctx, in.WorkspaceID)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	if st.LandingPage == nil || st.LandingPage.Menu == nil {
		return nil, &dbstore.MenuPage{}, nil
	}
	return nil, st.LandingPage.Menu, nil
}

type setMenuPageIn struct {
	WorkspaceID string `json:"workspace_id" jsonschema:"ID workspace (dari list_workspaces)"`
	Enabled     bool   `json:"enabled" jsonschema:"true = papar menu restoran; false = jatuh ke landing page / senarai borang"`
	// Categories groups product IDs under display names, e.g.
	// [{name:"Makanan", product_ids:["...","..."]}].
	Categories []dbstore.MenuCategory `json:"categories,omitempty" jsonschema:"Kategori menu, cth [{\"name\":\"Makanan\",\"product_ids\":[\"id1\",\"id2\"]}]"`
	// BestSeller is an optional product ID highlighted as the best-seller.
	BestSeller string `json:"best_seller,omitempty" jsonschema:"ID produk yang ditonjolkan sebagai best-seller (pilihan)"`
}

func (s *Server) setMenuPage(
	ctx context.Context, _ *mcp.CallToolRequest, in setMenuPageIn,
) (*mcp.CallToolResult, map[string]string, error) {
	_, role, err := s.accessibleWorkspace(ctx, in.WorkspaceID)
	if err != nil {
		return nil, nil, err
	}
	if role != "" && role != dbstore.RoleManage && role != dbstore.RoleAdmin {
		return nil, nil, errors.New("perlu peranan manage atau admin")
	}
	sp, err := s.workspacesStore()
	if err != nil {
		return nil, nil, err
	}
	st, err := sp.Settings(ctx, in.WorkspaceID)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	// Ensure a landing page exists so the menu template has a hero/theme to
	// build on. If none, seed a minimal one.
	if st.LandingPage == nil {
		st.LandingPage = &dbstore.LandingPage{Enabled: true}
	}
	if in.Enabled {
		// Enabling the menu implies the landing page is on.
		st.LandingPage.Enabled = true
	}
	menu := &dbstore.MenuPage{
		Enabled:     in.Enabled,
		Categories:  in.Categories,
		BestSeller:  in.BestSeller,
	}
	if menu.Categories == nil {
		menu.Categories = []dbstore.MenuCategory{}
	}
	st.LandingPage.Menu = menu
	if err := sp.SaveSettings(ctx, st); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{"status": "saved", "workspace_id": in.WorkspaceID}, nil
}

// urlQueryEscape percent-encodes a value for a query string.
func urlQueryEscape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(s, " ", "%20"), "&", "%26"), "=", "%3D")
}

// urlPathEscape percent-encodes a value for a URL path segment.
func urlPathEscape(s string) string {
	return strings.ReplaceAll(s, "/", "%2F")
}
