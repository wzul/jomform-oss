// Package mcpsrv — server construction.
package mcpsrv

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/chip"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/payments"
)

// version reported in the MCP implementation info.
const version = "0.1.0"

// New builds the JomForm MCP server with all tools registered.
func New(deps *Deps, chipClient *chip.Client, decryptKey []byte, pay *payments.Service) *Server {
	s := &Server{
		mcp: mcp.NewServer(&mcp.Implementation{
			Name:    "jomform",
			Version: version,
		}, nil),
		deps:   deps,
		chip:   chipClient,
		decKey: decryptKey,
		pay:    pay,
	}
	s.registerTools()
	return s
}

// Handler returns the root MCP server (for the streamable HTTP transport).
func (s *Server) Raw() *mcp.Server { return s.mcp }

func (s *Server) registerTools() {
	// ── Products ────────────────────────────────────────────────────────────
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "list_products",
		Description: "Senarai semua produk aktif dalam akaun JomForm anda.",
	}, s.listProducts)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "create_product",
		Description: "Cipta produk baharu. Untuk langganan, set type='subscription' dan billing_period='month'|'year'.",
	}, s.createProduct)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "archive_product",
		Description: "Nyahaktifkan produk (dulang tanpa padam data).",
	}, s.archiveProduct)

	// ── Sales ───────────────────────────────────────────────────────────────
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "list_sales",
		Description: "Senarai jualan terkini (terbaru dahulu).",
	}, s.listSales)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "get_sales_summary",
		Description: "Ringkasan jualan: bilangan mengikut status dan jumlah nilai.",
	}, s.salesSummary)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "confirm_sale",
		Description: "Sahkan jualan (contoh: selepas semak bukti bank transfer).",
	}, s.confirmSale)

	// ── Subscriptions ───────────────────────────────────────────────────────
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "cancel_subscription",
		Description: "Batalkan langganan pelanggan (token kad dipadam di CHIP).",
	}, s.cancelSubscription)

	// ── Account ─────────────────────────────────────────────────────────────
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "get_account",
		Description: "Maklumat asas akaun (nama, slug, CHIP mode).",
	}, s.getAccount)

	// ── Per-form CHIP override ──────────────────────────────────────────────
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "set_form_chip_override",
		Description: "Set kredensial CHIP khusus untuk SATU borang (brand_id + secret_key). Bila set, borang tersebut guna kredensial ini, bukan default akaun. Secret disimpan terenkripsi.",
	}, s.setFormChipOverride)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "clear_form_chip_override",
		Description: "Kosongkan override sebuah borang — kembali guna kredensial default akaun.",
	}, s.clearFormChipOverride)

	// ── Capture flow ────────────────────────────────────────────────────────
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "capture_sale",
		Description: "Capture authorisasi skip_capture (kad buyer dicaj sekarang). amount_sen optional untuk partial.",
	}, s.captureSaleMCP)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "release_sale",
		Description: "Release (void) authorisasi skip_capture — kad buyer TIDAK dicaj.",
	}, s.releaseSaleMCP)
}

// helper: require a write-capable key.
func (s *Server) accountAndScope(ctx context.Context, write bool) (accountID string, err error) {
	key := KeyFromContext(ctx)
	if key == nil {
		return "", fmt.Errorf("api key tidak sah")
	}
	if write && !dbstore.ScopeHas(key.Scopes, "readwrite") {
		return "", fmt.Errorf("scope tidak mencukupi (perlu readwrite)")
	}
	return key.AccountID, nil
}
