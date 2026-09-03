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
	addTool(s, &mcp.Tool{
		Name:        "list_products",
		Description: "Senarai semua produk aktif dalam akaun JomForm anda.",
	}, s.listProducts)

	addTool(s, &mcp.Tool{
		Name:        "create_product",
		Description: "Cipta produk baharu. Untuk langganan, set type='subscription' dan billing_period='month'|'year'.",
	}, s.createProduct)

	addTool(s, &mcp.Tool{
		Name:        "archive_product",
		Description: "Nyahaktifkan produk (dulang tanpa padam data).",
	}, s.archiveProduct)

	addTool(s, &mcp.Tool{
		Name:        "list_product_variations",
		Description: "Senarai variasi produk (name, price_sen, stock, images, sort_order) untuk satu produk. Produk simple = senarai kosong.",
	}, s.listProductVariations)

	// ── Sales ───────────────────────────────────────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "list_sales",
		Description: "Senarai jualan terkini (terbaru dahulu).",
	}, s.listSales)

	addTool(s, &mcp.Tool{
		Name:        "get_sales_summary",
		Description: "Ringkasan jualan: bilangan mengikut status dan jumlah nilai.",
	}, s.salesSummary)

	addTool(s, &mcp.Tool{
		Name:        "confirm_sale",
		Description: "Sahkan jualan (contoh: selepas semak bukti bank transfer).",
	}, s.confirmSale)

	addTool(s, &mcp.Tool{
		Name:        "get_sale_email_status",
		Description: "Status emel resit untuk satu jualan: Sent → Delivered → Opened → (Clicked) dengan masa. Guna ref_code dari list_sales.",
	}, s.saleEmailStatusMCP)

	// ── Subscriptions ───────────────────────────────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "list_subscriptions",
		Description: "Senarai langganan akaun: pelanggan (emel), produk, jumlah/period, status (active/past_due/cancelled), tarikh caj seterusnya. Tapis ?status, page/limit.",
	}, s.listSubscriptions)
	addTool(s, &mcp.Tool{
		Name:        "cancel_subscription",
		Description: "Batalkan langganan pelanggan. Kad token kekal pada merchant — boleh aktif semula.",
	}, s.cancelSubscription)
	addTool(s, &mcp.Tool{
		Name:        "update_subscription_card",
		Description: "Kemas kini kad pada langganan sedia ada (issue #154): cipta token purchase baharu supaya pelanggan boleh sahkan kad baharu. Pulangkan checkout_url untuk dihantar kepada pelanggan. Token lama kekal di CHIP (tidak dibatalkan) tetapi tidak lagi digunakan.",
	}, s.updateSubscriptionCard)

	// ── Account ─────────────────────────────────────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "get_account",
		Description: "Maklumat asas akaun (nama, slug, CHIP mode).",
	}, s.getAccount)

	// ── Per-form CHIP override ──────────────────────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "set_form_chip_override",
		Description: "Set kredensial CHIP khusus untuk SATU borang (brand_id + secret_key). Bila set, borang tersebut guna kredensial ini, bukan default akaun. Secret disimpan terenkripsi.",
	}, s.setFormChipOverride)

	addTool(s, &mcp.Tool{
		Name:        "clear_form_chip_override",
		Description: "Kosongkan override sebuah borang — kembali guna kredensial default akaun.",
	}, s.clearFormChipOverride)

	// ── Per-form CHIP feeds (issue #208) ────────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "get_form_chip_feeds",
		Description: "Baca senarai feed CHIP borang (issue #208). Setiap feed ada brand_id + secret (tersembunyi) + logik bersyarat. Feed tanpa condition = lalai (sentiasa padan).",
	}, s.getFormChipFeedsMCP)
	addTool(s, &mcp.Tool{
		Name:        "set_form_chip_feeds",
		Description: "Set senarai feed CHIP borang (issue #208). Feed pertama yang padan (ikut nilai medan borang) dipakai untuk pembayaran; feed tanpa condition = lalai. Secret kosong pada feed sedia ada = kekalkan kunci lama.",
	}, s.setFormChipFeedsMCP)

	// ── Per-form CHIP field mapping (issue #186) ─────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "get_form_chip_mapping",
		Description: "Baca peta medan CHIP borang (medan CHIP → id blok borang). Kosong = guna lalai (name→full_name, email→email, phone→phone).",
	}, s.getFormChipMapping)
	addTool(s, &mcp.Tool{
		Name:        "set_form_chip_mapping",
		Description: "Set peta medan CHIP borang (medan CHIP → id blok borang), cth {\"full_name\":\"block_abc\",\"email\":\"block_def\"}. Mapping kosong = guna lalai.",
	}, s.setFormChipMapping)

	// ── Capture flow ────────────────────────────────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "capture_sale",
		Description: "Capture authorisasi skip_capture (kad buyer dicaj sekarang). amount_sen optional untuk partial.",
	}, s.captureSaleMCP)

	addTool(s, &mcp.Tool{
		Name:        "release_sale",
		Description: "Release (void) authorisasi skip_capture — kad buyer TIDAK dicaj.",
	}, s.releaseSaleMCP)
	addTool(s, &mcp.Tool{
		Name:        "capture_all_pending_sales",
		Description: "Capture SEMUA authorisasi skip_capture yang masih menunggu. Satu kegagalan tidak hentikan yang lain; balas per-ref.",
	}, s.captureAllPendingMCP)

	// ── Parity: forms/products (dashboard coverage) ────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "update_product",
		Description: "Kemaskini produk: nama, harga (price_sen), stok, jenis (type), billing_period (langganan), aktif/tidak. Omit medan tak berubah.",
	}, s.updateProduct)
	addTool(s, &mcp.Tool{
		Name:        "bulk_products",
		Description: "Arkibkan banyak produk sekali gus (active=false). ids[] maks 500.",
	}, s.bulkProductsMCP)
	addTool(s, &mcp.Tool{
		Name:        "upsert_form",
		Description: "Cipta/kemaskini borang (form_id kosong = baru). Config blocks ikut get_form.",
	}, s.upsertForm)
	addTool(s, &mcp.Tool{
		Name:        "publish_form",
		Description: "Publish borang — live di <space>.jomform.com/<slug> atau custom domain. status=private (dengan password) menjadikannya borang ber-kata laluan; status=draft menarik balik.",
	}, s.publishFormMCP)
	addTool(s, &mcp.Tool{
		Name:        "unpublish_form",
		Description: "Tarik balik borang dari live (jadikan draft).",
	}, s.deleteFormMCP)
	addTool(s, &mcp.Tool{
		Name:        "set_form_password",
		Description: "Set/buang kata laluan akses borang private (argon2id-hashed; tidak pernah dipulangkan). Password kosong = buang kata laluan.",
	}, s.setFormPassword)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "get_form_restrictions",
		Description: "Baca sekatan borang (issue #207): jadual mula/tamat (start_at/end_at) + had penyertaan (entry_limit) + mesej peringkat.",
	}, s.getFormRestrictions)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "set_form_restrictions",
		Description: "Set sekatan borang (issue #207): jadual mula/tamat + had penyertaan + mesej. Kosong = buang sekatan.",
	}, s.setFormRestrictions)
	addTool(s, &mcp.Tool{
		Name:        "delete_form",
		Description: "Soft-delete borang (pergi ke trash dashboard; slug dilepaskan). Boleh dipulihkan.",
	}, s.deleteFormMCPSoft)
	addTool(s, &mcp.Tool{
		Name:        "bulk_forms",
		Description: "Tindakan pukal atas banyak borang sekali gus: action=publish|draft|delete dengan ids[] (maks 500).",
	}, s.bulkFormsMCP)
	// ── Parity: settings/webhooks ──────────────────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "set_einvoice",
		Description: "Set TIN + MSIC LHDN untuk resit e-Invoice (B2B).",
	}, s.setEinvoice)
	addTool(s, &mcp.Tool{
		Name:        "get_einvoice_settings",
		Description: "Baca TIN + MSIC LHDN semasa untuk resit e-Invoice.",
	}, s.getEinvoiceSettings)
	addTool(s, &mcp.Tool{
		Name:        "create_webhook",
		Description: "Daftar endpoint webhook (events: sale.paid|sale.failed|subscription.charged|*). HMAC-signed. form_id pilihan: set = webhook hanya untuk borang itu; kosong = seluruh space/akaun.",
	}, s.createWebhookMCP)
	addTool(s, &mcp.Tool{
		Name:        "list_webhooks",
		Description: "Senarai outbound webhooks akaun. form_id pilihan: set = webhook borang itu sahaja; kosong = webhook seluruh space/akaun.",
	}, s.listWebhooksMCP)
	addTool(s, &mcp.Tool{
		Name:        "delete_webhook",
		Description: "Padam satu webhook (id dari list_webhooks).",
	}, s.deleteWebhookMCP)
	addTool(s, &mcp.Tool{
		Name:        "update_webhook",
		Description: "Kemas kini webhook (url, events, secret pilihan — kosong = kekal sedia ada). events: sale.paid|sale.failed|subscription.charged|*.",
	}, s.updateWebhookMCP)
	addTool(s, &mcp.Tool{
		Name:        "webhook_deliveries",
		Description: "Log penghantaran terkini webhook (status_code, attempt, error).",
	}, s.webhookDeliveriesMCP)
	// ── Parity: API-key self-service ────────────────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "list_api_keys",
		Description: "Senarai semua API key akaun (id, nama, scopes, created_at, last_used). Key material tidak pernah dikembalikan.",
	}, s.listAPIKeys)
	addTool(s, &mcp.Tool{
		Name:        "create_api_key",
		Description: "Cipta API key baharu (nama, scopes? readonly|readwrite, default readonly). Key penuh dipapar SEKALI sahaja — simpan segera; store simpan hash sahaja. SCOPES: baca untuk readonly, baca+tulis untuk readwrite.",
	}, s.createAPIKey)
	addTool(s, &mcp.Tool{
		Name:        "revoke_api_key",
		Description: "Padam API key secara PERMANEN (id dari list_api_keys; tiada disable/enable semula — cipta key baru). AMARAN: jika id ialah key yang sedang mengesahkan sesi ini, ia akan memutuskan akses sendiri serta-merta — set confirm=true untuk sahkan; sebaliknya panggilan ditolak.",
	}, s.revokeAPIKey)

	// ── Spaces (multi-tenant storefronts) ───────────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "list_spaces",
		Description: "Senarai semua space (storefront) anda — id, nama, slug dan URL.",
	}, s.listSpaces)
	addTool(s, &mcp.Tool{
		Name:        "create_space",
		Description: "Cipta space baharu. Slug 3-40 aksara (a-z, 0-9, dash); URL jadi https://<slug>.jomform.com.",
	}, s.createSpace)
	addTool(s, &mcp.Tool{
		Name:        "get_space_settings",
		Description: "Baca tetapan storefront satu space: footer, company_reg, privacy/terms/refund URL, contact, menu.",
	}, s.getSpaceSettings)
	addTool(s, &mcp.Tool{
		Name:        "set_space_settings",
		Description: "Set tetapan storefront space: footer_text, company_reg, privacy_url/terms_url/refund_url (mesti https://), contact_phone, contact_address, business_name, menu_json. Omit medan = kekal.",
	}, s.setSpaceSettings)
	// Issue #185: per-space currency + BNM rates (MCP parity).
	addTool(s, &mcp.Tool{
		Name:        "get_currency_rates",
		Description: "Kadar tukaran wang BNM (Ringgit per unit mata wang asing), dicache harian. Guna untuk paparan/penukaran amaun baharu.",
	}, s.getCurrencyRates)
	addTool(s, &mcp.Tool{
		Name:        "set_space_currency",
		Description: "Set mata wang space (default MYR). Perubahan terpakai pada produk/jualan baharu; jualan lama kekal mata wang asal (tidak ditukar balik).",
	}, s.setSpaceCurrency)
	// Issue #161: space membership + invites (admin only).
	addTool(s, &mcp.Tool{
		Name:        "invite_space_member",
		Description: "Jemput ahli ke space (admin sahaja). Jika emel sudah berdaftar, tambah terus sebagai ahli; jika tidak, cipta jemputan menunggu. Role: read_only|manage|admin.",
	}, s.inviteSpaceMember)
	addTool(s, &mcp.Tool{
		Name:        "list_space_members",
		Description: "Senarai ahli space + peranan (admin sahaja).",
	}, s.listSpaceMembers)
	addTool(s, &mcp.Tool{
		Name:        "update_space_member_role",
		Description: "Tukar peranan ahli space (admin sahaja). Role: read_only|manage|admin.",
	}, s.updateSpaceMemberRole)
	addTool(s, &mcp.Tool{
		Name:        "remove_space_member",
		Description: "Buang ahli dari space (admin sahaja). Pemilik tidak boleh dibuang.",
	}, s.removeSpaceMember)
	addTool(s, &mcp.Tool{
		Name:        "list_space_invites",
		Description: "Senarai jemputan menunggu untuk space (admin sahaja).",
	}, s.listSpaceInvites)
	addTool(s, &mcp.Tool{
		Name:        "cancel_space_invite",
		Description: "Batal jemputan menunggu untuk space (admin sahaja).",
	}, s.cancelSpaceInvite)
	// Issue #164: default space (multi-space login picker) parity.
	addTool(s, &mcp.Tool{
		Name:        "set_default_space",
		Description: "Set space lalai anda (space yang anda masuk terus selepas log masuk, tanpa prompt). space_id kosong = buang default. Space mesti milik anda.",
	}, s.setDefaultSpace)
	// Issue #181: optional profile phone number parity.
	addTool(s, &mcp.Tool{
		Name:        "update_profile_phone",
		Description: "Set nombor telefon pilihan pada profil anda (kosong = buang). Format longgar: digit, +, ruang, 8-15 digit.",
	}, s.updateProfilePhone)
	// Issue #191: optional profile timezone parity.
	addTool(s, &mcp.Tool{
		Name:        "update_profile_timezone",
		Description: "Set zona waktu IANA pilihan pada profil anda (kosong = Auto, ikut pelayar/PC). Contoh: Asia/Kuala_Lumpur, UTC.",
	}, s.updateProfileTimezone)

	// ── Parity: per-space email templates (issue #107) ────────────────────
	addTool(s, &mcp.Tool{
		Name:        "get_email_settings",
		Description: "Baca tetapan emel satu space: subjek/badan resit (placeholder), hantar resit, notifikasi merchant, HTML.",
	}, s.getEmailSettings)
	addTool(s, &mcp.Tool{
		Name:        "set_email_settings",
		Description: "Set tetapan emel space: receipt_subject, receipt_body ({{ref_code}}, {{amount}}, {{items}}, {{form_url}}, {{buyer_name}}, {{date}}), receipt_enabled, notify_merchant, merchant_notify_email, html. Omit medan = kekal.",
	}, s.setEmailSettings)

	// ── Parity: per-form notification email (issue #177) ─────────────────
	addTool(s, &mcp.Tool{
		Name:        "get_form_email_settings",
		Description: "Baca tetapan emel notifikasi SATU borang (issue #177): subjek/badan resit, hantar resit, notifikasi merchant, HTML. Kosong = guna lalai space.",
	}, s.getFormEmailSettings)
	addTool(s, &mcp.Tool{
		Name:        "set_form_email_settings",
		Description: "Set tetapan emel notifikasi SATU borang (issue #177): receipt_subject, receipt_body, receipt_enabled, notify_merchant, merchant_notify_email, html. Kosong subjek/badan = guna lalai space. Omit medan = kekal.",
	}, s.setFormEmailSettings)

	// ── Parity: per-form notification rules (issue #193) ────────────────
	addTool(s, &mcp.Tool{
		Name:        "get_form_notifications",
		Description: "Baca senarai peraturan notifikasi emel per-borang (issue #193): event, penerima, subjek/badan, logik bersyarat, aktif. Termasuk notifikasi lalai (notify merchant on sale.paid).",
	}, s.getFormNotifications)
	addTool(s, &mcp.Tool{
		Name:        "set_form_notifications",
		Description: "Ganti SEMUA peraturan notifikasi emel per-borang (issue #193). notifications[] penuh: {id?, name, event, to_email, subject, body, condition?, enabled?}. event: sale.paid|sale.failed|response.submitted|subscription.charged. to_email: emel, {admin_email}, atau {field_id}. condition: {field_id, op, value}|null (op: equals|not_equals|contains|gt|lt|not_empty). Senarai kosong = tiada emel dihantar.",
	}, s.setFormNotifications)
	// ── Parity: per-form confirmations (issue #179) ─────────────────────
	addTool(s, &mcp.Tool{
		Name:        "get_form_confirmations",
		Description: "Baca pengesahan borang (issue #179): senarai confirmation (message/redirect) + logik bersyarat. Pengesahan lalai sentiasa wujud.",
	}, s.getFormConfirmations)
	addTool(s, &mcp.Tool{
		Name:        "set_form_confirmations",
		Description: "Set pengesahan borang (issue #179): ganti semua confirmation (name, type message|redirect, message/redirect_url, condition). Pengesahan lalai sentiasa dikekalkan. Mesej HTML disanitasi (tiada XSS).",
	}, s.setFormConfirmations)

	// ── Parity: per-space hosted legal pages (issue #184) ────────────────
	addTool(s, &mcp.Tool{
		Name:        "generate_legal_page",
		Description: "Jana draf halaman undang-undang (privacy|terms|refund) untuk space melalui LLM (deepseek-v4-flash) dengan template yang diisi butiran perniagaan. Pulangkan draf untuk semakan — belum disimpan sehingga set_legal_page dipanggil.",
	}, s.generateLegalPage)
	addTool(s, &mcp.Tool{
		Name:        "get_legal_page",
		Description: "Baca satu halaman undang-undang space (privacy|terms|refund): mode (auto/manual) + kandungan.",
	}, s.getLegalPage)
	addTool(s, &mcp.Tool{
		Name:        "set_legal_page",
		Description: "Simpan kandungan halaman undang-undang space (privacy|terms|refund) dengan mode auto|manual. Kandungan dipaparkan di {space}.jomform.com/privacy|terms|refund.",
	}, s.setLegalPage)

	// ── Parity: event tracking (issue #92) ────────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "list_events",
		Description: "Log audit/tingkah-laku akaun (terbaru dahulu). Boleh tapis event_name, from/to (RFC3339), dan page/limit. Read-only.",
	}, s.listEvents)

	// ── Parity: email delivery status (issue #194) ────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "list_email_deliveries",
		Description: "Status penghantaran emel akaun (terbaru dahulu): delivered/bounce/complaint/open/click dengan emel, masa, sale_ref dan detail. Boleh tapis form_id, sale_ref, event_type, dan page/limit. Read-only.",
	}, s.listEmailDeliveries)

	// ── Parity: response-only form submissions (issue #171) ───────────────
	addTool(s, &mcp.Tool{
		Name:        "list_form_submissions",
		Description: "Senarai respons (submission) untuk satu borang respons-sahaja (mode 'response', tanpa bayaran). Pulangkan data medan + masa. Tapis form_id, page/limit.",
	}, s.listFormSubmissions)

	// ── Parity: media library (issue #180) ───────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "list_media",
		Description: "Senarai media yang dimuat naik akaun (perpustakaan media): thumbnail/url, nama, tarikh, saiz. Boleh tapis form_id dan source (merchant|customer). Read-only.",
	}, s.listMedia)

	// ── Parity: form template gallery (issue #174) ────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "list_templates",
		Description: "Senarai template borang sistem (gallery): slug, tajuk, kategori, mode (payment/response). Guna dengan use_template untuk quickstart.",
	}, s.listTemplatesMCP)
	addTool(s, &mcp.Tool{
		Name:        "use_template",
		Description: "Clone template borang (slug dari list_templates) menjadi draft baharu dalam akaun anda. Pulangkan form_id + slug untuk dibuka dalam builder.",
	}, s.useTemplateMCP)

	// ── Parity: unverified-account purge (issue #190) ───────────────────
	// The auto-deletion runs on the hourly sweep; this admin tool surfaces
	// the backlog count (read-only) so agents can observe the purge.
	addTool(s, &mcp.Tool{
		Name:        "purge_unverified_accounts",
		Description: "Laporkan bilangan akaun belum disahkan (emel) yang lebih lama daripada ambang dan akan dipadam automatik oleh sweep. Admin sahaja; read-only — tidak memadam apa-apa.",
	}, s.purgeUnverifiedAccounts)

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
