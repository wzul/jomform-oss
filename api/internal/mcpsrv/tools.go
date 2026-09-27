// Package mcpsrv — server construction.
package mcpsrv

import (
	"context"
	"errors"
	"fmt"
	"time"

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
		// Issue #386: uptime baseline for admin_system_health.
		startedAt: time.Now(),
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
		Description: "Senarai variasi produk (name, price_sen, stock, images, sort_order, dan kitaran langganan sendiri bila ada) untuk satu produk. Produk simple = senarai kosong. Medan billing_period/trial_days/subscription_charges HANYA hadir bila variasi menetapkannya sendiri; jika tiada, variasi itu mewarisi kitaran produk.",
	}, s.listProductVariations)

	addTool(s, &mcp.Tool{
		Name:        "create_product_variation",
		Description: "Tambah SATU variasi pada produk (cth: Saiz M). Boleh tetapkan kitaran langganan variasi sendiri (billing_period month|year, trial_days, subscription_charges). Biarkan kosong = variasi mewarisi kitaran produk.",
	}, s.createProductVariation)

	addTool(s, &mcp.Tool{
		Name:        "update_product_variation",
		Description: "Kemaskini SATU variasi (nama, harga, stok, gambar, kitaran langganan). Medan yang tidak diisi kekal tidak berubah. Set clear_billing_period / clear_trial_days / clear_subscription_charges = true untuk BUANG kitaran variasi itu supaya ia mewarisi kitaran produk semula.",
	}, s.updateProductVariation)

	addTool(s, &mcp.Tool{
		Name:        "delete_product_variation",
		Description: "Padam SATU variasi dari produk. Variasi terakhir yang dipadam menjadikan produk itu produk simple semula.",
	}, s.deleteProductVariation)

	// ── Sales ───────────────────────────────────────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "list_sales",
		Description: "Senarai rekod terkini (terbaru dahulu). Tanpa form_id ini ialah SELURUH set rekod akaun: jualan DAN respons borang mode 'response' (sale_type 'response', submission_id + data) — sama seperti halaman Sales. Dengan form_id, mode borang itu menentukan jualan atau respons. Setiap jualan: refunded_sen, outstanding_sen, refund_status (none|partial|full) — issue #518. Status ialah soalan bayaran, jadi menetapkan status mengecualikan bahagian respons.",
	}, s.listSales)

	addTool(s, &mcp.Tool{
		Name:        "get_sales_summary",
		Description: "Ringkasan jualan: bilangan mengikut status (termasuk refunded), refunded_sen (jumlah direfund) dan revenue_sen (nilai bersih selepas refund) — issue #518.",
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
	addTool(s, &mcp.Tool{
		Name:        "retry_subscription",
		Description: "Cuba semula langganan yang past_due (round 13c): kembalikan status kepada active dan tetapkan ia due sekarang supaya sweep seterusnya mencaj kad semula. Guna selepas pelanggan top-up akaun atau betulkan kad. Hanya langganan past_due layak — yang cancelled/completed ditolak. Tidak mencaj serta-merta (elak caj dua kali).",
	}, s.retrySubscription)

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
		Name:        "refund_sale",
		Description: "Refund jualan yang telah dibayar (paid/confirmed). amount_sen = jumlah refund separa; kosong = refund penuh baki yang tinggal. Mesti > 0 dan refunded_sen + amount_sen <= total_sen (refund melebihi baki ditolak sebelum CHIP, issue #518). Refund separa merekod jumlah dan jualan kekal paid/confirmed (refund_status=partial); hanya baki penuh direfund barulah status jadi refunded.",
	}, s.refundSaleMCP)
	addTool(s, &mcp.Tool{
		Name:        "capture_all_pending_sales",
		Description: "Capture SEMUA authorisasi skip_capture yang masih menunggu. Satu kegagalan tidak hentikan yang lain; balas per-ref.",
	}, s.captureAllPendingMCP)

	// ── Parity: forms/products (dashboard coverage) ────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "update_product",
		Description: "Kemaskini produk: nama, harga (price_sen), stok, jenis (type), billing_period (langganan), aktif/tidak, images (URL imej mesti https:// — issue #501; omit = tak berubah). Omit medan tak berubah.",
	}, s.updateProduct)
	addTool(s, &mcp.Tool{
		Name:        "bulk_products",
		Description: "Arkibkan banyak produk sekali gus (active=false). ids[] maks 500.",
	}, s.bulkProductsMCP)
	addTool(s, &mcp.Tool{
		Name:        "list_forms",
		Description: "Senarai borang akaun anda: id, slug, tajuk, status (draft|private|published|unlisted), mode (payment|response) dan URL awam bila ia live. Read-only — guna untuk penemuan id borang sebelum membaca atau menulis.",
	}, s.listForms)
	addTool(s, &mcp.Tool{
		Name:        "get_form",
		Description: "Baca SATU borang sepenuhnya (read-only): id, slug, tajuk, status, mode, config — TERMASUK blocks semasa — theme, registered_fields, URL awam, created_at dan updated_at. Baca borang dahulu, ubah hanya medan yang dikehendaki, kemudian hantar semula config penuh melalui upsert_form. Menulis tanpa membaca dahulu boleh memusnahkan blok sedia ada.",
	}, s.getForm)
	addTool(s, &mcp.Tool{
		Name:        "upsert_form",
		Description: "Cipta/kemaskini borang (form_id kosong = baru). Baca sedia ada dengan get_form dahulu dan hantar config penuh supaya blok sedia ada tidak hilang. Theme (pilihan) = gaya/branding tersuai. slug (pilihan) = laluan URL borang: pada cipta ia digunakan bila diisi; pada kemaskini ia MENAMAKAN SEMULA borang (a-z, 0-9, sengkang; 1-40 aksara) dan URL lama kekal berfungsi melalui ubah hala sementara (301) — kosongkan untuk kekalkan slug sedia ada, dan slug yang sudah dipakai borang lain dalam workspace ini ditolak dengan ralat.",
	}, s.upsertForm)
	// Issue #254: MCP-driven form creation — styling freedom + field
	// registration. JomForm keeps authority over field types, prices and the
	// payment flow.
	addTool(s, &mcp.Tool{
		Name:        "set_form_theme",
		Description: "Set gaya/branding tersuai borang (issue #254): primary, background, text, card_background, card_border_radius, font_family, logo_url (mesti https:// — issue #501), layout (centered|full_width|split), spacing (compact|comfortable|spacious), product_image_size (small|medium|large|hidden, issue #471), product_image_layout (thumbnail-left|image-top — kad menu), custom_css, custom_js (issue #388 — JS tersuai untuk tingkah laku borang; dikawal pemilik + diimbas). Agent bebas set CSS/JS; JomForm kekal kuasa ke atas harga & pembayaran.",
	}, s.setFormTheme)
	addTool(s, &mcp.Tool{
		Name:        "register_form_fields",
		Description: "Daftar medan input borang (issue #254) dengan kontrak ketat: {field_id, type, label, required, validation?, product_id?}. type mesti jenis input JomForm (name|email|phone|textarea|number|select|radio|checkbox|date|time|website|address|consent|hidden|list|repeater|image-choice|captcha). Ganti semua medan sedia ada. Harga kekal dari DB produk (server authority).",
	}, s.registerFormFields)
	// Issue #256: AI form builder — MCP parity. The agent types a prompt and
	// gets a validated form config (blocks + styling) drafted by the LLM.
	addTool(s, &mcp.Tool{
		Name:        "generate_form",
		Description: "Jana konfigurasi borang (blok + gaya) dari prompt semula jadi (issue #256) via deepseek-v4-flash. Output disahkan server-side terhadap jenis blok JomForm; harga produk kekal dari DB (server authority). Semak/edit dulu, kemudian simpan guna upsert_form.",
	}, s.generateForm)
	addTool(s, &mcp.Tool{
		Name:        "publish_form",
		Description: "Publish borang — live di <workspace>.jomform.com/<slug> atau custom domain. status=private (dengan password) menjadikannya borang ber-kata laluan; status=draft menarik balik.",
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
		Description: "Soft-delete borang (pergi ke trash dashboard; slug dilepaskan). Boleh dipulihkan — guna restore_form.",
	}, s.deleteFormMCPSoft)
	// Tong sampah: padam kekal + pulihkan (Wan's decision on the delete-workspace
	// card — the bulk-delete copy promises \"can be restored later\", so the
	// restore path has to exist; and the workspace-empty guard is only
	// satisfiable if a trashed form can be destroyed for good).
	addTool(s, &mcp.Tool{
		Name:        "list_deleted_forms",
		Description: "Senarai borang dalam TONG SAMPAH (deleted_at IS NOT NULL): id, slug, title, deleted_at, sales_count dan can_purge. Borang dengan jualan tidak boleh dipadam kekal (can_purge=false).",
	}, s.listDeletedForms)
	addTool(s, &mcp.Tool{
		Name:        "restore_form",
		Description: "Pulihkan borang dari tong sampah (deleted_at=NULL). Tiada apa-apa dimusnahkan — borang kembali dengan slug asalnya. Jika slug sudah diambil borang lain, pulihan DITOLAK (tiada penamaan semula automatik) — tukar slug borang itu dahulu.",
	}, s.restoreFormMCP)
	addTool(s, &mcp.Tool{
		Name:        "purge_form",
		Description: "Padam borang secara KEKAL dari tong sampah (submissions/notifikasi/confirmations/tetapan emel borang/webhook borang gugur bersama). AMARAN: tidak boleh dibatalkan. Wajib confirm_slug = slug borang itu. Borang yang ADA JUALAN ditolak (sales tidak pernah dimusnahkan atau dijadikan yatim). Hanya borang dalam tong sampah — padam dulu guna delete_form.",
	}, s.purgeFormMCP)
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
		Description: "Daftar endpoint webhook (events: sale.paid|sale.failed|subscription.charged|*). HMAC-signed. form_id pilihan: set = webhook hanya untuk borang itu; kosong = seluruh workspace/akaun.",
	}, s.createWebhookMCP)
	addTool(s, &mcp.Tool{
		Name:        "list_webhooks",
		Description: "Senarai outbound webhooks akaun. form_id pilihan: set = webhook borang itu sahaja; kosong = webhook seluruh workspace/akaun.",
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
	// Issue #197: manual retry of a failed webhook delivery (MCP parity).
	addTool(s, &mcp.Tool{
		Name:        "retry_webhook_delivery",
		Description: "Cuba semula satu penghantaran webhook yang gagal dengan segera (id dari webhook_deliveries). Ia dienqueue semula dan dihantar semula pada sweep seterusnya.",
	}, s.retryWebhookDeliveryMCP)
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

	// ── Workspaces (multi-tenant storefronts) ───────────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "list_workspaces",
		Description: "Senarai semua workspace (storefront) anda — id, nama, slug dan URL.",
	}, s.listWorkspaces)
	// ── Affiliates (issue #444) ────────────────────────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "create_affiliate",
		Description: "Cipta affiliate untuk workspace: kod rujukan (?aff=CODE), nama, emel, kadar komisen. Jualan yang dibawa affiliate dikreditkan kepadanya.",
	}, s.createAffiliate)
	addTool(s, &mcp.Tool{
		Name:        "list_affiliates",
		Description: "Senarai semua affiliate dalam satu workspace — id, kod, nama, komisen, URL rujukan.",
	}, s.listAffiliates)
	addTool(s, &mcp.Tool{
		Name:        "get_affiliate_sales",
		Description: "Senarai jualan yang dibawa oleh satu affiliate (read-only, skop workspace).",
	}, s.getAffiliateSales)
	addTool(s, &mcp.Tool{
		Name:        "set_affiliate_commission",
		Description: "Set kadar komisen affiliate (cth: 10 = 10%).",
	}, s.setAffiliateCommission)
	addTool(s, &mcp.Tool{
		Name:        "create_workspace",
		Description: "Cipta workspace baharu. Slug 3-40 aksara (a-z, 0-9, dash); URL jadi https://<slug>.jomform.com. Nama yang dikhaskan untuk JomForm (cth: www, api, app, mail, s3, docs, mcp, templates, custom, support, help, status, billing, admin, account, login, payments, checkout, cdn, assets) ditolak.",
	}, s.createWorkspace)
	addTool(s, &mcp.Tool{
		Name:        "get_workspace_settings",
		Description: "Baca tetapan storefront satu workspace: footer, company_reg, privacy/terms/refund URL, contact, menu.",
	}, s.getWorkspaceSettings)
	addTool(s, &mcp.Tool{
		Name:        "set_workspace_settings",
		Description: "Set tetapan storefront workspace: footer_text, company_reg, privacy_url/terms_url/refund_url (mesti https://), contact_phone, contact_address, business_name, menu_json. Omit medan = kekal.",
	}, s.setWorkspaceSettings)
	// Issue #360: customizable landing page (MCP parity).
	addTool(s, &mcp.Tool{
		Name:        "get_workspace_landing_page",
		Description: "Baca konfigurasi 'landing page' workspace (issue #360): hero, subtitle, body, image_url, theme colors, logo, CTA (label + form_slug). Kosong jika tiada landing page ditetapkan.",
	}, s.getWorkspaceLandingPage)
	addTool(s, &mcp.Tool{
		Name:        "set_workspace_landing_page",
		Description: "Set 'landing page' storefront workspace (issue #360): enabled (true = tunjuk, false/kosong = jatuh ke senarai borang), hero, subtitle, body, image_url (mesti https:// — issue #501), primary_color, background_color, logo_url (mesti https:// — issue #501), ctas ([{label, form_slug}]). Hantar enabled:false untuk pulang ke senarai borang.",
	}, s.setWorkspaceLandingPage)
	// Issue #438: QR ordering flow (MCP parity).
	addTool(s, &mcp.Tool{
		Name:        "generate_qr",
		Description: "Jana QR code untuk borang diterbitkan (issue #438): form_slug (cth 'menu') + table pilihan (nombor meja, dikodkan sebagai ?table=N). Pulangkan qr_url (endpoint /qr/{slug}.png), data_url (PNG base64) dan target_url (URL yang dikodkan). Guna untuk stand QR di meja restoran.",
	}, s.generateQR)
	// Issue #447: realtime notifications (MCP parity). The realtime channel is
	// the authenticated SSE stream GET /v1/rest/events/stream; these tools
	// document it and expose the per-workspace sound toggle.
	addTool(s, &mcp.Tool{
		Name:        "get_realtime_status",
		Description: "Baca status notifikasi masa-nyata workspace (issue #447): stream_url (SSE /v1/rest/events/stream — saluran masa-nyata untuk jualan/respon), senarai event, dan sound_enabled (bunyi beep pada dashboard). Read-only.",
	}, s.getRealtimeStatus)
	addTool(s, &mcp.Tool{
		Name:        "set_realtime_sound",
		Description: "Hidup/matikan bunyi beep notifikasi masa-nyata workspace (issue #447). enabled=true = dashboard berbunyi bila jualan/respon baharu tiba (cth skrin dapur); false = senyap. Write tool (peranan manage/admin).",
	}, s.setRealtimeSound)
	addTool(s, &mcp.Tool{
		Name:        "get_menu_page",
		Description: "Baca konfigurasi menu restoran satu BORANG (issue #497): form_id. Pulangkan enabled, categories ([{name, product_ids}]), best_seller, best_seller_ids (senarai, #485), order_form_slug (borang maklumat tambahan pesanan, #491). Menu ialah tetapan PER-BORANG dan dipapar di URL borang itu sendiri. Kosong jika borang tiada menu.",
	}, s.getMenuPage)
	addTool(s, &mcp.Tool{
		Name:        "set_menu_page",
		Description: "Set menu restoran untuk satu BORANG (issue #497): form_id, enabled (true = borang ini jadi menu di URL-nya; false = borang kembali normal), categories ([{name, product_ids}]), best_seller (ID produk) atau best_seller_ids (senarai ID untuk seksyen Best Seller, #485 — mengatasi best_seller), order_form_slug (pilihan, #491 — borang diterbitkan dalam akaun sama untuk kumpul maklumat tambahan semasa checkout menu; kosong = checkout nama + emel sahaja, kedua-duanya tetap wajib untuk CHIP). Produk menu datang dari blok produk BORANG itu sendiri (bukan semua produk akaun), jadi produk akaun lain tidak boleh bocor masuk; kategori/best-seller mesti merujuk produk yang dirujuk oleh borang. Halaman menu menunjukkan nombor meja dari ?table=N dan ada penapis kategori + carian. Borang tanpa blok produk memaparkan keadaan kosong.",
	}, s.setMenuPage)
	// Issue #185: per-workspace currency + BNM rates (MCP parity).
	addTool(s, &mcp.Tool{
		Name:        "get_currency_rates",
		Description: "Kadar tukaran wang BNM (Ringgit per unit mata wang asing), dicache harian. Guna untuk paparan/penukaran amaun baharu.",
	}, s.getCurrencyRates)
	addTool(s, &mcp.Tool{
		Name:        "set_workspace_currency",
		Description: "Set mata wang workspace (default MYR). Perubahan terpakai pada produk/jualan baharu; jualan lama kekal mata wang asal (tidak ditukar balik).",
	}, s.setWorkspaceCurrency)
	// Issue #161: workspace membership + invites (admin only).
	addTool(s, &mcp.Tool{
		Name:        "invite_workspace_member",
		Description: "Jemput ahli ke workspace (admin sahaja). Jika emel sudah berdaftar, tambah terus sebagai ahli; jika tidak, cipta jemputan menunggu. Role: read_only|manage|admin.",
	}, s.inviteWorkspaceMember)
	addTool(s, &mcp.Tool{
		Name:        "list_workspace_members",
		Description: "Senarai ahli workspace + peranan (admin sahaja).",
	}, s.listWorkspaceMembers)
	addTool(s, &mcp.Tool{
		Name:        "update_workspace_member_role",
		Description: "Tukar peranan ahli workspace (admin sahaja). Role: read_only|manage|admin.",
	}, s.updateWorkspaceMemberRole)
	addTool(s, &mcp.Tool{
		Name:        "remove_workspace_member",
		Description: "Buang ahli dari workspace (admin sahaja). Pemilik tidak boleh dibuang.",
	}, s.removeWorkspaceMember)
	addTool(s, &mcp.Tool{
		Name:        "list_workspace_invites",
		Description: "Senarai jemputan menunggu untuk workspace (admin sahaja).",
	}, s.listWorkspaceInvites)
	addTool(s, &mcp.Tool{
		Name:        "cancel_workspace_invite",
		Description: "Batal jemputan menunggu untuk workspace (admin sahaja).",
	}, s.cancelWorkspaceInvite)
	// Issue #164: default workspace (multi-workspace login picker) parity.
	addTool(s, &mcp.Tool{
		Name:        "set_default_workspace",
		Description: "Set workspace lalai anda (workspace yang anda masuk terus selepas log masuk, tanpa prompt). workspace_id kosong = buang default. Workspace mesti milik anda.",
	}, s.setDefaultWorkspace)
	addTool(s, &mcp.Tool{
		Name:        "delete_workspace",
		Description: "PADAM workspace secara kekal (pemilik sahaja). TIGA syarat: (1) confirm_slug wajib sepadan dengan slug workspace; (2) anda mesti ada sekurang-kurangnya satu workspace lain — workspace terakhir tidak boleh dipadam; (3) workspace mesti KOSONG — borang (termasuk dalam tong sampah), produk, jualan, domain, webhook, kunci API aktif dan langganan aktif semuanya menyekat, dan penolakan menyenaraikan setiap kategori dengan kiraannya. Jualan tidak pernah dimusnahkan: jualan mesti diselesaikan dahulu. Akaun CHIP workspace dipadam BERSAMA (kredential tidak pernah dikongsi atau ditinggalkan yatim). Guna check_workspace_deletion dahulu untuk lihat penghalang. Tidak boleh dibatalkan.",
	}, s.deleteWorkspaceMCP)
	addTool(s, &mcp.Tool{
		Name:        "check_workspace_deletion",
		Description: "Semak sama ada satu workspace boleh dipadam (baca sahaja, tiada kesan). Pulangkan can_delete, empty, last_workspace, is_owner dan blockers[] (setiap kategori + kiraan) — takrifan 'kosong' yang SAMA yang dikuatkuasa oleh delete_workspace.",
	}, s.checkWorkspaceDeletionMCP)
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
	// Language placement: the dashboard UI language is per-PROFILE.
	addTool(s, &mcp.Tool{
		Name:        "update_profile_language",
		Description: "Set bahasa UI dashboard anda: en, ms, zh, ta, ar (kosong = buang, kembali ikut pelayar). Ini bahasa DASHBOARD sahaja dan ia per-profil — bahasa borang awam ditetapkan per-borang atau per-workspace, bukan di sini.",
	}, s.updateProfileLanguage)

	// ── Parity: per-workspace email templates (issue #107) ────────────────────
	addTool(s, &mcp.Tool{
		Name:        "get_email_settings",
		Description: "Baca tetapan emel satu workspace: subjek/badan resit (placeholder), hantar resit, notifikasi merchant, HTML.",
	}, s.getEmailSettings)
	addTool(s, &mcp.Tool{
		Name:        "set_email_settings",
		Description: "Set tetapan emel workspace: receipt_subject, receipt_body ({{ref_code}}, {{amount}}, {{items}}, {{form_url}}, {{buyer_name}}, {{date}}), receipt_enabled, notify_merchant, merchant_notify_email, html. Omit medan = kekal.",
	}, s.setEmailSettings)

	// ── Parity: per-form notification email (issue #177, overrides #107/#453) ──
	addTool(s, &mcp.Tool{
		Name:        "get_form_email_settings",
		Description: "Baca tetapan emel SATU borang: override per-borang (subjek/badan resit, hantar resit, notifikasi merchant, HTML), tetapan workspace yang diwarisi, dan nilai efektif (resolusi per-borang -> workspace -> lalai). Kosong/null = ikut tetapan workspace.",
	}, s.getFormEmailSettings)
	addTool(s, &mcp.Tool{
		Name:        "set_form_email_settings",
		Description: "Set override emel SATU borang (issue #453): receipt_subject, receipt_body, receipt_enabled_override, notify_merchant_override, merchant_notify_email, html_override. Toggle adalah tri-state — true/false = override eksplisit, null = ikut tetapan workspace. Kosong subjek/badan = ikut workspace. Omit medan = kekal.",
	}, s.setFormEmailSettings)
	addTool(s, &mcp.Tool{
		Name:        "clear_form_email_overrides",
		Description: "Padam SEMUA override emel per-borang supaya borang ini kembali mewarisi tetapan emel workspace (issue #453).",
	}, s.clearFormEmailOverrides)

	// ── Parity: per-form notification rules (issue #193) ────────────────
	addTool(s, &mcp.Tool{
		Name:        "get_form_notifications",
		Description: "Baca senarai peraturan notifikasi emel per-borang (issue #193): event, penerima, subjek/badan, logik bersyarat, aktif. Setiap borang baharu disertakan satu notifikasi lalai kepada merchant: event sale.paid untuk borang pembayaran, response.submitted untuk borang respons (issue #597).",
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
	// ── Parity: per-form payment-failed message (issue #393) ────────────
	addTool(s, &mcp.Tool{
		Name:        "get_form_failure_message",
		Description: "Baca mesej pembayaran-gagal borang (issue #393): apa yang pembeli lihat bila pembayaran gagal. Kosong = guna mesej lalai.",
	}, s.getFormFailureMessage)
	addTool(s, &mcp.Tool{
		Name:        "set_form_failure_message",
		Description: "Set mesej pembayaran-gagal borang (issue #393): HTML yang pembeli lihat bila pembayaran gagal (disanitasi, tiada XSS). Kosong = guna mesej lalai.",
	}, s.setFormFailureMessage)
	// ── Parity: per-form Open Graph image (issue #411) ──────────────────
	addTool(s, &mcp.Tool{
		Name:        "get_form_og_image",
		Description: "Baca imej Open Graph (og:image) borang (issue #411): URL imej tersuai bila ditetapkan, kosong = guna lalai janaan /og/{slug}.png.",
	}, s.getFormOGImage)
	addTool(s, &mcp.Tool{
		Name:        "set_form_og_image",
		Description: "Set imej Open Graph (og:image) borang (issue #411): URL imej tersuai (mesti https://) untuk pratonton sosial bila borang dikongsi. Kosong = guna lalai janaan.",
	}, s.setFormOGImage)

	// ── Parity: per-workspace hosted legal pages (issue #184) ────────────────
	addTool(s, &mcp.Tool{
		Name:        "generate_legal_page",
		Description: "Jana draf halaman undang-undang (privacy|terms|refund) untuk workspace melalui LLM (deepseek-v4-flash) dengan template yang diisi butiran perniagaan. Pulangkan draf untuk semakan — belum disimpan sehingga set_legal_page dipanggil.",
	}, s.generateLegalPage)
	addTool(s, &mcp.Tool{
		Name:        "get_legal_page",
		Description: "Baca satu halaman undang-undang workspace (privacy|terms|refund): mode (auto/manual) + kandungan.",
	}, s.getLegalPage)
	addTool(s, &mcp.Tool{
		Name:        "set_legal_page",
		Description: "Simpan kandungan halaman undang-undang workspace (privacy|terms|refund) dengan mode auto|manual. Kandungan dipaparkan di {workspace}.jomform.com/privacy|terms|refund.",
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

	// ── Parity: AI spam review queue (issue #245) ─────────────────────────
	// The merchant's agent can list spam-flagged submissions/sales and
	// un-spam a false positive. A spam-flagged sale that receives a successful
	// payment is auto-un-spammed by the payment callback (no agent action).
	addTool(s, &mcp.Tool{
		Name:        "list_spam_submissions",
		Description: "Senarai submission yang ditanda spam oleh AI (issue #245): data medan, sebab spam, masa. Boleh tapis page/limit. Read-only.",
	}, s.listSpamSubmissions)
	addTool(s, &mcp.Tool{
		Name:        "unspam_submission",
		Description: "Nyah-tanda spam pada submission (false positive). Write tool (peranan manage/admin).",
	}, s.unspamSubmission)
	addTool(s, &mcp.Tool{
		Name:        "list_spam_sales",
		Description: "Senarai jualan yang ditanda spam oleh AI (issue #245): ref, status, jumlah, pembeli, sebab spam, masa. Boleh tapis page/limit. Read-only.",
	}, s.listSpamSales)
	addTool(s, &mcp.Tool{
		Name:        "unspam_sale",
		Description: "Nyah-tanda spam pada jualan (false positive). Write tool (peranan manage/admin).",
	}, s.unspamSale)

	// ── Parity: media library (issue #180) ───────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "list_media",
		Description: "Senarai media yang dimuat naik akaun (perpustakaan media): thumbnail/url, nama, tarikh, saiz. Boleh tapis form_id dan source (merchant|customer). Read-only.",
	}, s.listMedia)
	// Issue #363: media upload parity — the agent pushes image + code assets
	// + PDFs over MCP, mirroring the dashboard upload. Images serve
	// immediately (png/jpeg/webp get thumbnail/medium/full sizes); code assets
	// (css/js/html/svg/woff2) wait for the #364 scan gate; PDFs serve
	// immediately.
	addTool(s, &mcp.Tool{
		Name:        "upload_media",
		Description: "Muat naik media/aset ke workspace anda (issue #363): kind='image' (png/jpeg/webp/gif — dioptimumkan, strip EXIF, terus boleh guna; png/jpeg/webp dapat saiz thumbnail/medium/full), kind='asset' (css/js/html/svg/woff2 — disimpan asli, dikaji sebelum dipapar) atau kind='pdf' (disimpan asli, terus boleh guna). hantar content fail sebagai base64 di 'data'. Pulangkan URL untuk diguna sebagai image_url/logo_url landing atau <link>/<script> landing page.",
	}, s.uploadMedia)

	// ── Parity: form template gallery (issue #174) ────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "list_templates",
		Description: "Senarai template borang sistem (gallery): slug, tajuk, kategori, mode (payment/response). Guna dengan use_template untuk quickstart.",
	}, s.listTemplatesMCP)
	addTool(s, &mcp.Tool{
		Name:        "use_template",
		Description: "Clone template borang menjadi draft baharu dalam akaun anda. Isi slug (dari list_templates) untuk template sistem, atau template_id (dari list_own_templates) untuk template anda sendiri. Pulangkan form_id + slug untuk dibuka dalam builder.",
	}, s.useTemplateMCP)
	// ── Parity: merchant-managed templates (issue #457) ───────────────────
	// Dashboard Templates view: save a form as a reusable template, then
	// list/edit/delete/clone your own templates. Account-scoped.
	addTool(s, &mcp.Tool{
		Name:        "list_own_templates",
		Description: "Senarai template anda sendiri (issue #457): id, slug, tajuk, config, theme. Berbeza daripada list_templates (gallery sistem).",
	}, s.listOwnTemplatesMCP)
	addTool(s, &mcp.Tool{
		Name:        "create_template",
		Description: "Simpan borang sedia ada sebagai template boleh guna semula (issue #457): isi form_id (dari list_forms) untuk salin borang itu, atau title + config untuk template baharu. Template dimiliki akaun anda sahaja.",
	}, s.createTemplateMCP)
	addTool(s, &mcp.Tool{
		Name:        "update_template",
		Description: "Kemaskini template anda sendiri (issue #457): tajuk, config dan/atau theme. Omit medan yang tak berubah. template_id dari list_own_templates.",
	}, s.updateTemplateMCP)
	addTool(s, &mcp.Tool{
		Name:        "delete_template",
		Description: "Padam (soft-delete) template anda sendiri (issue #457). template_id dari list_own_templates. Boleh dicipta semula bila-bila masa.",
	}, s.deleteTemplateMCP)

	// ── Parity: unverified-account purge (issue #190) ───────────────────
	// The auto-deletion runs on the hourly sweep; this admin tool surfaces
	// the backlog count (read-only) so agents can observe the purge.
	addTool(s, &mcp.Tool{
		Name:        "purge_unverified_accounts",
		Description: "Laporkan bilangan akaun belum disahkan (emel) yang lebih lama daripada ambang dan akan dipadam automatik oleh sweep. Admin sahaja; read-only — tidak memadam apa-apa.",
	}, s.purgeUnverifiedAccounts)

	// ── Super-admin: platform-wide monitoring (issue #386) ─────────────
	// These tools are gated on the super-admin role (#385) and are read-only
	// by default. They let the platform owner's AI agent monitor usage across
	// ALL workspaces once JomForm is public. Destructive admin writes are separate
	// and explicit (none exist yet). Every call is audited.
	addTool(s, &mcp.Tool{
		Name:        "admin_list_workspaces",
		Description: "Senarai SEMUA workspace (storefront) platform: id, nama, slug, emel pemilik, tarikh cipta, bilangan borang + produk. Super-admin sahaja; read-only; diaudit.",
	}, s.adminListWorkspaces)
	addTool(s, &mcp.Tool{
		Name:        "admin_media_usage",
		Description: "Penggunaan storan media per workspace (bait) berbanding kuota 1GB setiap workspace. Super-admin sahaja; read-only; diaudit.",
	}, s.adminMediaUsage)
	addTool(s, &mcp.Tool{
		Name:        "admin_list_sales",
		Description: "Senarai jualan merentas SEMUA workspace, boleh tapis (date_from/date_to, workspace_id, status). Termasuk refunded_sen/outstanding_sen/refund_status (issue #518). Super-admin sahaja; read-only; diaudit.",
	}, s.adminListSales)
	addTool(s, &mcp.Tool{
		Name:        "admin_spam_queue",
		Description: "Senarai semua item spam (submission + jualan) merentas semua workspace. Super-admin sahaja; read-only; diaudit.",
	}, s.adminSpamQueue)
	addTool(s, &mcp.Tool{
		Name:        "admin_email_deliveries",
		Description: "Status penghantaran emel merentas SEMUA workspace (delivered/bounce/complaint/open/click), boleh tapis sale_ref/event_type/form_id. Super-admin sahaja; read-only; diaudit.",
	}, s.adminEmailDeliveries)
	addTool(s, &mcp.Tool{
		Name:        "admin_dead_letter_events",
		Description: "Senarai peristiwa callback masuk yang di-park sebagai dead letter (issue #574): dedup_key, event_type, bilangan cubaan, sebab, masa. Selepas #569 peristiwa yang tak boleh diproses di-park sebagai 'dead' dan BERHENTI dicuba — sebelum ini ia tidak kelihatan sama sekali (tiada MCP/REST membaca webhook_events). Super-admin sahaja; read-only; diaudit.",
	}, s.adminDeadLetterEvents)
	addTool(s, &mcp.Tool{
		Name:        "admin_requeue_dead_letter_event",
		Description: "Hidupkan semula peristiwa dead letter supaya sweep replay mencubanya lagi (issue #574), selepas punca dibetulkan. dedup_key dari admin_dead_letter_events. Kiraan cubaan di-reset supaya ia tidak di-park semula oleh sweep yang sama sebelum sempat berjalan. Super-admin sahaja; diaudit.",
	}, s.adminRequeueDeadLetterEvent)
	addTool(s, &mcp.Tool{
		Name:        "admin_system_health",
		Description: "Kesihatan sistem: uptime, bilangan ralat MCP terkini (24j), kedalaman queue emel. Super-admin sahaja; read-only; diaudit.",
	}, s.adminSystemHealth)

	// ── Super-admin: suppression list (issue #601) ──────────────────────
	// The pre-send suppression list (issue #56) had no surface at all: SES
	// ingestion could add an address, but nothing could read the list back,
	// add one by hand, or remove a false positive without psql. These three
	// tools back the /admin suppression section; the dashboard uses the
	// matching REST endpoints (GET/POST/DELETE /v1/admin/suppressions).
	addTool(s, &mcp.Tool{
		Name:        "admin_list_suppressions",
		Description: "Senarai alamat yang disekat daripada dihantar emel (issue #601): emel, sebab (hard_bounce|complaint|manual), sumber, nota, siapa tambah dan bila. Boleh cari (q, padan emel atau nota) dan berhalaman (page/limit). Super-admin sahaja; read-only; diaudit.",
	}, s.adminListSuppressions)
	addTool(s, &mcp.Tool{
		Name:        "admin_add_suppression",
		Description: "Tambah alamat ke senarai sekatan secara manual supaya tiada emel dihantar kepadanya (issue #601). reason: hard_bounce|complaint|manual (lalai manual), note pilihan. Idempotent — alamat yang sudah dalam senarai tidak menghasilkan ralat. Super-admin sahaja; diaudit.",
	}, s.adminAddSuppression)
	addTool(s, &mcp.Tool{
		Name:        "admin_remove_suppression",
		Description: "Buang alamat dari senarai sekatan supaya penghantaran emel kepadanya bermula semula (issue #601) — untuk kes positif palsu. Super-admin sahaja; diaudit.",
	}, s.adminRemoveSuppression)

	// ── Parity: in-app feedback (system-owned, two-way thread) ──────────
	// The dashboard's feedback form + admin triage, over MCP. Unlike the rest
	// of the registry, these tools are scoped to the authenticated USER, not to
	// a workspace: a feedback thread belongs to the person who filed it and
	// carries no account_id, so naming a workspace cannot widen the read. A
	// super-admin additionally sees every thread (triage) from the same tools.
	addTool(s, &mcp.Tool{
		Name:        "list_feedback",
		Description: "Senarai thread maklum balas anda sendiri (terbaru dahulu), boleh tapis status (open|in_progress|resolved|closed) dan berhalaman (page/limit). Skop ialah PENGGUNA yang disahkan, bukan workspace — maklum balas bukan data akaun. Super-admin melihat SEMUA thread (triage). Read-only.",
	}, s.listFeedback)
	addTool(s, &mcp.Tool{
		Name:        "get_feedback_thread",
		Description: "Baca satu thread maklum balas beserta semua mesejnya (author_kind: user|admin), terawal dahulu. Thread milik pengguna lain menjawab 'tidak dijumpai' — sama seperti id yang tidak wujud. Super-admin boleh membaca mana-mana thread. Read-only.",
	}, s.getFeedbackThread)
	addTool(s, &mcp.Tool{
		Name:        "reply_feedback",
		Description: "Balas dalam thread maklum balas. Penulis ditentukan oleh pemanggil: super-admin membalas sebagai admin (dan thread 'open' bergerak ke in_progress), pengguna lain membalas sebagai pemilik thread. Body minimum 10 aksara. Skop pengguna — tidak boleh membalas thread orang lain.",
	}, s.replyFeedback)
	addTool(s, &mcp.Tool{
		Name:        "set_feedback_status",
		Description: "Tukar status thread maklum balas (open|in_progress|resolved|closed). Keputusan triage — super-admin sahaja; diaudit. Status adalah per-thread, bukan per-mesej.",
	}, s.setFeedbackStatus)
	addTool(s, &mcp.Tool{
		Name:        "retry_feedback_notify",
		Description: "Hantar semula notifikasi emel yang GAGAL untuk satu thread maklum balas (medan notify_failed_at pada thread). Ini laluan pemulihan untuk kegagalan penghantaran: emel yang tidak sempat masuk baris gilir ditandakan pada thread supaya kelihatan, dan tool ini cuba semula melalui saluran yang sama. Keputusan: queued|sent|failed|none ('none' bermaksud tiada kegagalan direkodkan). Super-admin sahaja; diaudit.",
	}, s.retryFeedbackNotify)

	// In-app chat (phase 1): the ONE write the chat has — file a feedback thread
	// from the conversation. Registered here so the dashboard capability has its
	// MCP counterpart (AGENTS.md parity), and it goes through the same
	// feedbacksvc as the /feedback page: same validation, same per-user daily
	// limit, same notification. User-scoped + write-scoped (see the file).
	addTool(s, &mcp.Tool{
		Name:        "submit_feedback",
		Description: "Failkan maklum balas atau permintaan ciri baharu daripada perbualan chat. Sama seperti menghantar di halaman /feedback: disimpan sebagai thread yang boleh anda baca dan balas, dan pasukan JomForm dimaklumkan. Body minimum 10 aksara, maksimum 5000; maksimum 10 penghantaran sehari bagi setiap pengguna. Memerlukan token PENGGUNA (OAuth/sesi) dan skop readwrite — API key ditolak kerana maklum balas ialah data peribadi pengguna.",
	}, s.submitFeedback)

	// ── Parity: analytics (issue #396) ────────────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "get_form_analytics",
		Description: "Analitik per-borang: bilangan entri (submissions), paid/confirmed/failed/pending, dan hasil (RM) — NET selepas refund. `submissions` bermaksud entri yang direkod untuk borang itu, dibaca dari jadual yang borang itu tulis: borang respons (mode=response) dikira dari `submissions`, borang bayaran dari `sales` (semua status). Medan `mode` disertakan supaya label betul. Merentas semua workspace anda. Read-only.",
	}, s.formAnalytics)
	addTool(s, &mcp.Tool{
		Name:        "get_product_analytics",
		Description: "Analitik per-produk: unit terjual (paid), hasil (RM). Merentas semua workspace anda. Read-only.",
	}, s.productAnalytics)

	// ── Parity: custom domains (issue #396) ──────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "list_domains",
		Description: "Senarai domain tersuai akaun: id, domain, provider, verified, auto_cname. Read-only.",
	}, s.listDomains)
	addTool(s, &mcp.Tool{
		Name:        "add_domain",
		Description: "Tuntut domain tersuai baharu (cth: kedai.com). Kemudian set CNAME → custom.jomform.com dan panggil verify_domain.",
	}, s.addDomain)
	addTool(s, &mcp.Tool{
		Name:        "verify_domain",
		Description: "Sahkan domain tersuai (id dari list_domains). Hanya disahkan bila domain benar-benar memaparkan halaman JomForm.",
	}, s.verifyDomain)
	addTool(s, &mcp.Tool{
		Name:        "remove_domain",
		Description: "Buang tuntutan domain tersuai (id dari list_domains).",
	}, s.removeDomain)
	addTool(s, &mcp.Tool{
		Name:        "auto_cname_domain",
		Description: "Cipta rekod CNAME Cloudflare secara automatik untuk domain (perlu Cloudflare dipautkan). Pulangkan auto_cname=created|failed.",
	}, s.autoCNAMEDomain)
	// Issue #397: per-domain TLS config + status.
	addTool(s, &mcp.Tool{
		Name:        "set_domain_tls_mode",
		Description: "Tetapkan mode TLS per-domain: auto (Let's Encrypt, default) atau custom (bring-your-own-cert).",
	}, s.setDomainTLSModeMCP)
	addTool(s, &mcp.Tool{
		Name:        "set_domain_custom_cert",
		Description: "Muat naik sijil TLS sendiri (bring-your-own-cert) untuk domain: cert + key dalam format PEM. Domain disajikan sijil ini.",
	}, s.setDomainCustomCertMCP)
	// Issue #398: per-domain ACME contact email (get/set).
	addTool(s, &mcp.Tool{
		Name:        "get_domain_tls_email",
		Description: "Baca emel ACME/Let's Encrypt per-domain untuk auto-TLS. Kosong = kembali ke emel akaun, kemudian ACME_EMAIL global. Read-only.",
	}, s.getDomainTLSEmailMCP)
	addTool(s, &mcp.Tool{
		Name:        "set_domain_tls_email",
		Description: "Tetapkan emel ACME/Let's Encrypt per-domain untuk auto-TLS. Kosong = kembali ke emel akaun, kemudian ACME_EMAIL global.",
	}, s.setDomainTLSEmailMCP)

	// ── Parity: tracking (issue #396) ────────────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "get_tracking",
		Description: "Baca skrip penjejakan global akaun (Google Analytics, Facebook Pixel, dll) yang disuntik ke <head> borang awam. Read-only.",
	}, s.getTracking)
	addTool(s, &mcp.Tool{
		Name:        "set_tracking",
		Description: "Set skrip penjejakan global akaun. Hanya <script src=https://...>, <meta> dan komen dibenarkan (disanitasi).",
	}, s.setTracking)

	// ── Parity: CHIP account settings (issue #396) ──────────────────────
	addTool(s, &mcp.Tool{
		Name:        "get_chip_settings",
		Description: "Baca status kredensial CHIP akaun: configured, verified, mode, brand_id, secret_key_masked, email_fallback, whitelist. Secret penuh tidak pernah dipulangkan. Read-only.",
	}, s.getChipSettings)
	addTool(s, &mcp.Tool{
		Name:        "set_chip_settings",
		Description: "Set kredensial CHIP akaun (brand_id + secret_key). Secret kosong = kekalkan kunci sedia ada. Disahkan terhadap CHIP sebelum disimpan. Admin sahaja.",
	}, s.setChipSettings)
	addTool(s, &mcp.Tool{
		Name:        "clear_chip_settings",
		Description: "Kosongkan kredensial CHIP akaun. Admin sahaja.",
	}, s.clearChipSettings)

	// ── Parity: profile avatar (issue #396) ──────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "update_profile_avatar",
		Description: "Set avatar profil (URL mesti imej yang dihoskan JomForm dari upload bucket). Kosong = buang avatar.",
	}, s.updateProfileAvatar)

	// ── Parity: passkeys (issue #396) ────────────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "list_passkeys",
		Description: "Senarai passkey (WebAuthn) akaun anda: id. Read-only.",
	}, s.listPasskeys)
	addTool(s, &mcp.Tool{
		Name:        "delete_passkey",
		Description: "Padam satu passkey (id dari list_passkeys).",
	}, s.deletePasskey)

	// ── Parity: social accounts (issue #396) ─────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "list_social_accounts",
		Description: "Senarai akaun sosial yang dipautkan (google|azure_ad|github|cloudflare): provider, emel, nama, picture, linked_at. Read-only.",
	}, s.listSocialAccounts)
	addTool(s, &mcp.Tool{
		Name:        "unlink_social_account",
		Description: "Nyah-pautkan akaun sosial (provider dari list_social_accounts).",
	}, s.unlinkSocialAccount)

	// ── Parity: 2FA (issue #396) ─────────────────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "get_2fa_status",
		Description: "Baca status 2FA akaun anda: enabled (true/false). Read-only.",
	}, s.getTwoFAStatus)
	addTool(s, &mcp.Tool{
		Name:        "setup_2fa",
		Description: "Jana rahsia TOTP untuk 2FA (belum aktif). Pulangkan secret + otpauth_url untuk diimbas aplikasi authenticator, kemudian panggil enable_2fa dengan kod.",
	}, s.setupTwoFA)
	addTool(s, &mcp.Tool{
		Name:        "enable_2fa",
		Description: "Aktifkan 2FA dengan kod 6-digit dari aplikasi authenticator (selepas setup_2fa).",
	}, s.enableTwoFA)
	addTool(s, disable2FATool(), s.disableTwoFA)

	// ── Parity: trusted devices (issue #454) ─────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "list_trusted_devices",
		Description: "Senarai peranti dipercayai 2FA akaun anda (label, ip, first_seen, expires_at). Peranti dipercayai melangkau kod 2FA selama 30 hari. Read-only.",
	}, s.listTrustedDevices)
	addTool(s, &mcp.Tool{
		Name:        "revoke_trusted_device",
		Description: "Batalkan satu peranti dipercayai (device_id dari list_trusted_devices). Log masuk seterusnya pada peranti itu akan meminta kod 2FA semula — serta-merta.",
	}, s.revokeTrustedDevice)
	addTool(s, &mcp.Tool{
		Name:        "revoke_all_trusted_devices",
		Description: "Batalkan SEMUA peranti dipercayai akaun anda (\"log keluar di semua tempat\"). Setiap peranti akan meminta kod 2FA semula.",
	}, s.revokeAllTrustedDevices)

	// ── Parity: system settings (issue #396) ─────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "get_system_settings",
		Description: "Baca semua tetapan sistem platform (site_*, landing_*). Read-only.",
	}, s.getSystemSettings)
	addTool(s, &mcp.Tool{
		Name:        "set_system_setting",
		Description: "Set satu tetapan sistem (key mesti bermula site_ atau landing_).",
	}, s.setSystemSetting)

	// ── Parity: sales export CSV (issue #396) ────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "export_sales_csv",
		Description: "Eksport set rekod akaun sebagai CSV. Tanpa form_id ia mengandungi jualan DAN respons (lajur record_type menandakan setiap baris, serta lajur jawapan yang direkodkan), sepadan dengan senarai yang dipaparkan. Boleh tapis form_id/status/date_from/date_to/q — penapis yang sama seperti senarai. Kolum refund mendedahkan refund sebahagian (issue #518). Pulangkan kandungan csv.",
	}, s.exportSalesCSV)

	// ── Parity: Cloudflare (issue #396) ──────────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "get_cloudflare_status",
		Description: "Baca status pautan Cloudflare akaun: linked, configured. Read-only.",
	}, s.getCloudflareStatus)
	addTool(s, &mcp.Tool{
		Name:        "connect_cloudflare_api_token",
		Description: "Pautkan Cloudflare via API token (disahkan serta-merta, disimpan terenkripsi).",
	}, s.connectCloudflareAPIToken)
	addTool(s, &mcp.Tool{
		Name:        "connect_cloudflare_global_key",
		Description: "Pautkan Cloudflare via global API key (disahkan serta-merta, disimpan terenkripsi).",
	}, s.connectCloudflareGlobalKey)
	addTool(s, &mcp.Tool{
		Name:        "disconnect_cloudflare",
		Description: "Nyah-pautkan Cloudflare dari akaun.",
	}, s.disconnectCloudflare)

	// ── Parity: webhook signing key (issue #396) ─────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "get_webhook_signing_key",
		Description: "Baca kunci tandatangan Ed25519 akaun (key_id, public_key, alg) untuk mengesahkan tandatangan webhook X-JomForm-Signature-Ed25519. Membaca kunci sedia ada tidak memerlukan skop tulis; JANA kunci baharu (kali pertama, atau selepas dipadam) memerlukan readwrite.",
	}, s.getWebhookSigningKey)

}

// requireWriteScope enforces the API-key WRITE SCOPE on a mutating tool. It is
// deliberately separate from the role gate (requireWriteRole /
// requireWriteRoleIn): scope and workspace role are two independent axes and a
// write must satisfy both.
//
// WHY IT IS ITS OWN FUNCTION (issue #645). The role gate used to BE the scope
// gate: each write tool called accountAndScope(ctx, true), which tested
// dbstore.ScopeHas(scopes, "readwrite") before doing anything. 5dafe3a (issue
// #162, workspace RBAC) swapped those call sites for requireWriteRole, whose
// chain reaches accountAndScope(ctx, false) — write=false — so the scope test
// was skipped and a readonly key could delete forms, archive products, cancel
// subscriptions and mint itself a readwrite key. Folding the two gates back into
// one function is what caused that: a future RBAC change could drop the scope
// half again with no test noticing. Keeping the scope test in a separately named
// function means every write tool names BOTH gates, and dropping one is a
// visible diff line rather than a changed boolean argument.
//
// OAuth tokens carry the readwrite scope (http.go, issue #121/#307), so agents
// are unaffected. A key with the legacy "admin" scope passes (ScopeHasWrite).
func (s *Server) requireWriteScope(ctx context.Context) error {
	id := IdentityFromContext(ctx)
	if id == nil {
		return errors.New("token tiada atau tidak sah")
	}
	if !dbstore.ScopeHasWrite(id.Scopes) {
		return errors.New("scope tidak mencukupi (perlu readwrite)")
	}
	return nil
}

// requireWriteScopeIn is the per-workspace form of requireWriteScope: the scope
// axis is credential-wide, not per-workspace, so accountID is only there to keep
// the call sites symmetrical with requireWriteRoleIn.
func (s *Server) requireWriteScopeIn(ctx context.Context, _ string) error {
	return s.requireWriteScope(ctx)
}

// accountAndScope resolves the account id for a call, enforcing the WRITE SCOPE
// when write is true.
//
// Deprecated for write gates: the write argument is a boolean a caller can get
// wrong (that is exactly the 5dafe3a regression). Write tools must instead name
// requireWriteScope / requireWriteRole explicitly. Kept because read calls use
// it with write=false and two write call sites still pass true.
func (s *Server) accountAndScope(ctx context.Context, write bool) (accountID string, err error) {
	id := IdentityFromContext(ctx)
	if id == nil {
		return "", fmt.Errorf("token tiada atau tidak sah")
	}
	if write && !dbstore.ScopeHasWrite(id.Scopes) {
		return "", fmt.Errorf("scope tidak mencukupi (perlu readwrite)")
	}
	// Per-call scope: if an explicit single account is requested, use it;
	// otherwise for account-and-write tools we resolve to the caller's first
	// accessible account by default (a workspace_id is honoured too).
	if cs := callScopeFrom(ctx); cs != nil {
		if cs.ExplicitAccountID != "" {
			return cs.ExplicitAccountID, nil
		}
		if len(cs.Accounts) > 0 {
			return cs.Accounts[0], nil
		}
		return id.AccountID, nil
	}
	return id.AccountID, nil
}

// accountAndRole resolves the account id + the caller's role in the workspace
// that owns it (issue #162). It honours the per-call scope (issue #307): when
// the call names a workspace/account, the role is that workspace's role; otherwise the
// caller's first accessible account's role is used. role is "" when the caller
// is the owner (admin) or has no linked workspace.
func (s *Server) accountAndRole(ctx context.Context) (accountID, role string, err error) {
	accountID, err = s.accountAndScope(ctx, false)
	if err != nil {
		return "", "", err
	}
	if s.deps.Workspaces == nil || s.deps.WorkspaceMembers == nil {
		return accountID, "", nil
	}
	userID := s.userIDFor(ctx)
	if userID == "" {
		return "", "", fmt.Errorf("token tiada atau tidak sah")
	}
	// If the call scoped to an explicit workspace, use that workspace's role (already
	// resolved by resolveScope — issue #307).
	if cs := callScopeFrom(ctx); cs != nil && cs.WorkspaceID != "" {
		return accountID, cs.Role, nil
	}
	role, err = s.deps.Workspaces.RoleByAccount(ctx, accountID, userID)
	if err != nil {
		return "", "", fmt.Errorf("db: %w", err)
	}
	return accountID, role, nil
}

// requireWriteRole enforces BOTH gates a write needs, on the account resolved
// for this call:
//
//   - the API-key WRITE SCOPE (requireWriteScope): a readonly key is refused.
//     This is the axis 5dafe3a dropped (issue #645).
//   - the manage-or-admin ROLE in the workspace owning that account (issue #162
//     MCP parity): a read_only member's agent cannot write.
//
// The two are separate calls on purpose — see requireWriteScope for why folding
// them into one function is what caused the regression.
func (s *Server) requireWriteRole(ctx context.Context) (accountID string, err error) {
	if err := s.requireWriteScope(ctx); err != nil {
		return "", err
	}
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
// management), on BOTH axes: the API-key write scope and the admin role.
func (s *Server) requireAdminRole(ctx context.Context) (accountID string, err error) {
	if err := s.requireWriteScope(ctx); err != nil {
		return "", err
	}
	accountID, role, err := s.accountAndRole(ctx)
	if err != nil {
		return "", err
	}
	if role != "" && role != dbstore.RoleAdmin {
		return "", fmt.Errorf("perlu admin workspace")
	}
	return accountID, nil
}

// requireAdminRoleForRead is the ROLE-only form of requireAdminRole, for an
// admin-surface READ.
//
// Issue #645 made the scope axis explicit, which is correct for writes — but it
// also means a tool that only READS an admin view must not be routed through
// requireAdminRole, or a readonly key would be refused on a read (the same
// defect as item 4 of the card, one layer down). Naming the two intents
// separately is what keeps "admin read" and "admin write" from sharing a gate
// whose meaning changed underneath them.
func (s *Server) requireAdminRoleForRead(ctx context.Context) error {
	_, role, err := s.accountAndRole(ctx)
	if err != nil {
		return err
	}
	if role != "" && role != dbstore.RoleAdmin {
		return fmt.Errorf("perlu admin workspace")
	}
	return nil
}
