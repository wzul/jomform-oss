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

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "list_product_variations",
		Description: "Senarai variasi produk (name, price_sen, stock, images, sort_order) untuk satu produk. Produk simple = senarai kosong.",
	}, s.listProductVariations)

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

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "get_sale_email_status",
		Description: "Status emel resit untuk satu jualan: Sent → Delivered → Opened → (Clicked) dengan masa. Guna ref_code dari list_sales.",
	}, s.saleEmailStatusMCP)

	// ── Subscriptions ───────────────────────────────────────────────────────
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "list_subscriptions",
		Description: "Senarai langganan akaun: pelanggan (emel), produk, jumlah/period, status (active/past_due/cancelled), tarikh caj seterusnya. Tapis ?status, page/limit.",
	}, s.listSubscriptions)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "cancel_subscription",
		Description: "Batalkan langganan pelanggan. Kad token kekal pada merchant — boleh aktif semula.",
	}, s.cancelSubscription)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "update_subscription_card",
		Description: "Kemas kini kad pada langganan sedia ada (issue #154): cipta token purchase baharu supaya pelanggan boleh sahkan kad baharu. Pulangkan checkout_url untuk dihantar kepada pelanggan. Token lama kekal di CHIP (tidak dibatalkan) tetapi tidak lagi digunakan.",
	}, s.updateSubscriptionCard)

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
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "capture_all_pending_sales",
		Description: "Capture SEMUA authorisasi skip_capture yang masih menunggu. Satu kegagalan tidak hentikan yang lain; balas per-ref.",
	}, s.captureAllPendingMCP)

	// ── Parity: forms/products (dashboard coverage) ────────────────────────
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "update_product",
		Description: "Kemaskini produk: nama, harga (price_sen), stok, jenis (type), billing_period (langganan), aktif/tidak. Omit medan tak berubah.",
	}, s.updateProduct)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "bulk_products",
		Description: "Arkibkan banyak produk sekali gus (active=false). ids[] maks 500.",
	}, s.bulkProductsMCP)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "upsert_form",
		Description: "Cipta/kemaskini borang (form_id kosong = baru). Config blocks ikut get_form.",
	}, s.upsertForm)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "publish_form",
		Description: "Publish borang — live di <space>.jomform.com/<slug> atau custom domain.",
	}, s.publishFormMCP)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "unpublish_form",
		Description: "Tarik balik borang dari live (jadikan draft).",
	}, s.deleteFormMCP)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "delete_form",
		Description: "Soft-delete borang (pergi ke trash dashboard; slug dilepaskan). Boleh dipulihkan.",
	}, s.deleteFormMCPSoft)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "bulk_forms",
		Description: "Tindakan pukal atas banyak borang sekali gus: action=publish|draft|delete dengan ids[] (maks 500).",
	}, s.bulkFormsMCP)
	// ── Parity: settings/webhooks ──────────────────────────────────────────
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "set_einvoice",
		Description: "Set TIN + MSIC LHDN untuk resit e-Invoice (B2B).",
	}, s.setEinvoice)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "get_einvoice_settings",
		Description: "Baca TIN + MSIC LHDN semasa untuk resit e-Invoice.",
	}, s.getEinvoiceSettings)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "create_webhook",
		Description: "Daftar endpoint webhook (events: sale.paid|sale.failed|subscription.charged|*). HMAC-signed.",
	}, s.createWebhookMCP)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "list_webhooks",
		Description: "Senarai semua outbound webhooks akaun.",
	}, s.listWebhooksMCP)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "delete_webhook",
		Description: "Padam satu webhook (id dari list_webhooks).",
	}, s.deleteWebhookMCP)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "update_webhook",
		Description: "Kemas kini webhook (url, events, secret pilihan — kosong = kekal sedia ada). events: sale.paid|sale.failed|subscription.charged|*.",
	}, s.updateWebhookMCP)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "webhook_deliveries",
		Description: "Log penghantaran terkini webhook (status_code, attempt, error).",
	}, s.webhookDeliveriesMCP)
	// ── Parity: API-key self-service ────────────────────────────────────────
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "list_api_keys",
		Description: "Senarai semua API key akaun (id, nama, scopes, created_at, last_used). Key material tidak pernah dikembalikan.",
	}, s.listAPIKeys)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "create_api_key",
		Description: "Cipta API key baharu (nama, scopes? readonly|readwrite, default readonly). Key penuh dipapar SEKALI sahaja — simpan segera; store simpan hash sahaja. SCOPES: baca untuk readonly, baca+tulis untuk readwrite.",
	}, s.createAPIKey)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "revoke_api_key",
		Description: "Padam API key secara PERMANEN (id dari list_api_keys; tiada disable/enable semula — cipta key baru). AMARAN: jika id ialah key yang sedang mengesahkan sesi ini, ia akan memutuskan akses sendiri serta-merta — set confirm=true untuk sahkan; sebaliknya panggilan ditolak.",
	}, s.revokeAPIKey)

	// ── Spaces (multi-tenant storefronts) ───────────────────────────────────
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "list_spaces",
		Description: "Senarai semua space (storefront) anda — id, nama, slug dan URL.",
	}, s.listSpaces)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "create_space",
		Description: "Cipta space baharu. Slug 3-40 aksara (a-z, 0-9, dash); URL jadi https://<slug>.jomform.com.",
	}, s.createSpace)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "get_space_settings",
		Description: "Baca tetapan storefront satu space: footer, company_reg, privacy/terms URL, menu.",
	}, s.getSpaceSettings)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "set_space_settings",
		Description: "Set tetapan storefront space: footer_text, company_reg, privacy_url/terms_url (mesti https://), menu_json. Omit medan = kekal.",
	}, s.setSpaceSettings)
	// Issue #161: space membership + invites (admin only).
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "invite_space_member",
		Description: "Jemput ahli ke space (admin sahaja). Jika emel sudah berdaftar, tambah terus sebagai ahli; jika tidak, cipta jemputan menunggu. Role: read_only|manage|admin.",
	}, s.inviteSpaceMember)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "list_space_members",
		Description: "Senarai ahli space + peranan (admin sahaja).",
	}, s.listSpaceMembers)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "update_space_member_role",
		Description: "Tukar peranan ahli space (admin sahaja). Role: read_only|manage|admin.",
	}, s.updateSpaceMemberRole)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "remove_space_member",
		Description: "Buang ahli dari space (admin sahaja). Pemilik tidak boleh dibuang.",
	}, s.removeSpaceMember)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "list_space_invites",
		Description: "Senarai jemputan menunggu untuk space (admin sahaja).",
	}, s.listSpaceInvites)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "cancel_space_invite",
		Description: "Batal jemputan menunggu untuk space (admin sahaja).",
	}, s.cancelSpaceInvite)
	// Issue #164: default space (multi-space login picker) parity.
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "set_default_space",
		Description: "Set space lalai anda (space yang anda masuk terus selepas log masuk, tanpa prompt). space_id kosong = buang default. Space mesti milik anda.",
	}, s.setDefaultSpace)
	// Issue #181: optional profile phone number parity.
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "update_profile_phone",
		Description: "Set nombor telefon pilihan pada profil anda (kosong = buang). Format longgar: digit, +, ruang, 8-15 digit.",
	}, s.updateProfilePhone)

	// ── Parity: per-space email templates (issue #107) ────────────────────
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "get_email_settings",
		Description: "Baca tetapan emel satu space: subjek/badan resit (placeholder), hantar resit, notifikasi merchant, HTML.",
	}, s.getEmailSettings)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "set_email_settings",
		Description: "Set tetapan emel space: receipt_subject, receipt_body ({{ref_code}}, {{amount}}, {{items}}, {{form_url}}, {{buyer_name}}, {{date}}), receipt_enabled, notify_merchant, merchant_notify_email, html. Omit medan = kekal.",
	}, s.setEmailSettings)

	// ── Parity: event tracking (issue #92) ────────────────────────────────
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "list_events",
		Description: "Log audit/tingkah-laku akaun (terbaru dahulu). Boleh tapis event_name, from/to (RFC3339), dan page/limit. Read-only.",
	}, s.listEvents)

	// ── Parity: response-only form submissions (issue #171) ───────────────
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "list_form_submissions",
		Description: "Senarai respons (submission) untuk satu borang respons-sahaja (mode 'response', tanpa bayaran). Pulangkan data medan + masa. Tapis form_id, page/limit.",
	}, s.listFormSubmissions)

	// ── Parity: form template gallery (issue #174) ────────────────────────
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "list_templates",
		Description: "Senarai template borang sistem (gallery): slug, tajuk, kategori, mode (payment/response). Guna dengan use_template untuk quickstart.",
	}, s.listTemplatesMCP)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "use_template",
		Description: "Clone template borang (slug dari list_templates) menjadi draft baharu dalam akaun anda. Pulangkan form_id + slug untuk dibuka dalam builder.",
	}, s.useTemplateMCP)

}

// helper: require a write-capable key.
func (s *Server) accountAndScope(ctx context.Context, write bool) (accountID string, err error) {
	id := IdentityFromContext(ctx)
	if id == nil {
		return "", fmt.Errorf("token tiada atau tidak sah")
	}
	if write && !dbstore.ScopeHas(id.Scopes, "readwrite") {
		return "", fmt.Errorf("scope tidak mencukupi (perlu readwrite)")
	}
	return id.AccountID, nil
}

// accountAndRole resolves the account id + the caller's role in the space
// that owns it (issue #162). The owner is always an admin; a member's role
// comes from space_members. role is "" when the account has no linked space
// (legacy single-account callers are treated as owners).
func (s *Server) accountAndRole(ctx context.Context) (accountID, role string, err error) {
	accountID, err = s.accountAndScope(ctx, false)
	if err != nil {
		return "", "", err
	}
	if s.deps.Spaces == nil || s.deps.SpaceMembers == nil {
		return accountID, "", nil
	}
	owner, err := s.spaceOwner(ctx)
	if err != nil {
		return "", "", err
	}
	role, err = s.deps.Spaces.RoleByAccount(ctx, accountID, owner)
	if err != nil {
		return "", "", fmt.Errorf("db: %w", err)
	}
	return accountID, role, nil
}

// requireWriteRole enforces the manage-or-admin gate on a write tool. A
// read_only member's agent cannot write (issue #162 MCP parity).
func (s *Server) requireWriteRole(ctx context.Context) (accountID string, err error) {
	accountID, role, err := s.accountAndRole(ctx)
	if err != nil {
		return "", err
	}
	if role != "" && role != dbstore.RoleManage && role != dbstore.RoleAdmin {
		return "", fmt.Errorf("perlu peranan manage atau admin")
	}
	return accountID, nil
}

// requireAdminRole enforces the admin-only gate (CHIP credentials + member
// management). A manage/read_only member's agent cannot manage these.
func (s *Server) requireAdminRole(ctx context.Context) (accountID string, err error) {
	accountID, role, err := s.accountAndRole(ctx)
	if err != nil {
		return "", err
	}
	if role != "" && role != dbstore.RoleAdmin {
		return "", fmt.Errorf("perlu admin space")
	}
	return accountID, nil
}
