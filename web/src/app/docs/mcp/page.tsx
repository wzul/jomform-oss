import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "MCP tool reference — JomForm Docs",
  description: "Every MCP tool JomForm exposes to AI agents: name, purpose, inputs and outputs. Kept in sync with the tool registry in api/internal/mcpsrv/tools.go.",
  alternates: { canonical: "https://jomform.com/docs/mcp" },
};

// Auto-generated from api/internal/mcpsrv/tools.go (issue #395).
// Keep in sync with the MCP tool registry when tools are added or changed.
const TOOL_GROUPS: { group: string; tools: { name: string; desc: string }[] }[] = [
  {
    group: "Forms",
    tools: [
      { name: "upsert_form", desc: "Cipta/kemaskini borang (form_id kosong = baru). Config blocks ikut get_form. Theme (pilihan) = gaya/branding tersuai." },
      { name: "set_form_theme", desc: "Set gaya/branding tersuai borang (issue #254): primary, background, text, card_background, card_border_radius, font_family, logo_url, layout (centered|full_width|split), spacing (compact|comfortable|spacious), custom_css. Agent bebas set CSS; JomForm kekal kuasa ke atas harga & pembayaran." },
      { name: "register_form_fields", desc: "Daftar medan input borang (issue #254) dengan kontrak ketat: {field_id, type, label, required, validation?, product_id?}. type mesti jenis input JomForm (name|email|phone|textarea|number|select|radio|checkbox|date|time|website|address|consent|hidden|list|repeater|image-choice|captcha). Ganti semua medan sedia ada. Harga kekal dari DB produk (server authority)." },
      { name: "generate_form", desc: "Jana konfigurasi borang (blok + gaya) dari prompt semula jadi (issue #256) via deepseek-v4-flash. Output disahkan server-side terhadap jenis blok JomForm; harga produk kekal dari DB (server authority). Semak/edit dulu, kemudian simpan guna upsert_form." },
      { name: "publish_form", desc: "Publish borang \u2014 live di <space>.jomform.com/<slug> atau custom domain. status=private (dengan password) menjadikannya borang ber-kata laluan; status=draft menarik balik." },
      { name: "unpublish_form", desc: "Tarik balik borang dari live (jadikan draft)." },
      { name: "set_form_password", desc: "Set/buang kata laluan akses borang private (argon2id-hashed; tidak pernah dipulangkan). Password kosong = buang kata laluan." },
      { name: "get_form_restrictions", desc: "Baca sekatan borang (issue #207): jadual mula/tamat (start_at/end_at) + had penyertaan (entry_limit) + mesej peringkat." },
      { name: "set_form_restrictions", desc: "Set sekatan borang (issue #207): jadual mula/tamat + had penyertaan + mesej. Kosong = buang sekatan." },
      { name: "delete_form", desc: "Soft-delete borang (pergi ke trash dashboard; slug dilepaskan). Boleh dipulihkan." },
      { name: "bulk_forms", desc: "Tindakan pukal atas banyak borang sekali gus: action=publish|draft|delete dengan ids[] (maks 500)." },
    ],
  },
  {
    group: "Products",
    tools: [
      { name: "list_products", desc: "Senarai semua produk aktif dalam akaun JomForm anda." },
      { name: "create_product", desc: "Cipta produk baharu. Untuk langganan, set type='subscription' dan billing_period='month'|'year'." },
      { name: "archive_product", desc: "Nyahaktifkan produk (dulang tanpa padam data)." },
      { name: "list_product_variations", desc: "Senarai variasi produk (name, price_sen, stock, images, sort_order) untuk satu produk. Produk simple = senarai kosong." },
      { name: "update_product", desc: "Kemaskini produk: nama, harga (price_sen), stok, jenis (type), billing_period (langganan), aktif/tidak. Omit medan tak berubah." },
      { name: "bulk_products", desc: "Arkibkan banyak produk sekali gus (active=false). ids[] maks 500." },
    ],
  },
  {
    group: "Sales",
    tools: [
      { name: "list_sales", desc: "Senarai jualan terkini (terbaru dahulu)." },
      { name: "get_sales_summary", desc: "Ringkasan jualan: bilangan mengikut status dan jumlah nilai." },
      { name: "confirm_sale", desc: "Sahkan jualan (contoh: selepas semak bukti bank transfer)." },
      { name: "get_sale_email_status", desc: "Status emel resit untuk satu jualan: Sent \u2192 Delivered \u2192 Opened \u2192 (Clicked) dengan masa. Guna ref_code dari list_sales." },
      { name: "export_sales_csv", desc: "Eksport jualan akaun sebagai CSV (ref_code, status, payment_method, buyer, total_rm, created_at, purchase_id). Boleh tapis form_id. Pulangkan kandungan csv." },
    ],
  },
  {
    group: "Skip-capture",
    tools: [
      { name: "capture_sale", desc: "Capture authorisasi skip_capture (kad buyer dicaj sekarang). amount_sen optional untuk partial." },
      { name: "release_sale", desc: "Release (void) authorisasi skip_capture \u2014 kad buyer TIDAK dicaj." },
      { name: "capture_all_pending_sales", desc: "Capture SEMUA authorisasi skip_capture yang masih menunggu. Satu kegagalan tidak hentikan yang lain; balas per-ref." },
    ],
  },
  {
    group: "Subscriptions",
    tools: [
      { name: "list_subscriptions", desc: "Senarai langganan akaun: pelanggan (emel), produk, jumlah/period, status (active/past_due/cancelled), tarikh caj seterusnya. Tapis ?status, page/limit." },
      { name: "cancel_subscription", desc: "Batalkan langganan pelanggan. Kad token kekal pada merchant \u2014 boleh aktif semula." },
      { name: "update_subscription_card", desc: "Kemas kini kad pada langganan sedia ada (issue #154): cipta token purchase baharu supaya pelanggan boleh sahkan kad baharu. Pulangkan checkout_url untuk dihantar kepada pelanggan. Token lama kekal di CHIP (tidak dibatalkan) tetapi tidak lagi digunakan." },
    ],
  },
  {
    group: "CHIP per-form",
    tools: [
      { name: "set_form_chip_override", desc: "Set kredensial CHIP khusus untuk SATU borang (brand_id + secret_key). Bila set, borang tersebut guna kredensial ini, bukan default akaun. Secret disimpan terenkripsi." },
      { name: "clear_form_chip_override", desc: "Kosongkan override sebuah borang \u2014 kembali guna kredensial default akaun." },
      { name: "get_form_chip_feeds", desc: "Baca senarai feed CHIP borang (issue #208). Setiap feed ada brand_id + secret (tersembunyi) + logik bersyarat. Feed tanpa condition = lalai (sentiasa padan)." },
      { name: "set_form_chip_feeds", desc: "Set senarai feed CHIP borang (issue #208). Feed pertama yang padan (ikut nilai medan borang) dipakai untuk pembayaran; feed tanpa condition = lalai. Secret kosong pada feed sedia ada = kekalkan kunci lama." },
      { name: "get_form_chip_mapping", desc: "Baca peta medan CHIP borang (medan CHIP \u2192 id blok borang). Kosong = guna lalai (name\u2192full_name, email\u2192email, phone\u2192phone)." },
      { name: "set_form_chip_mapping", desc: "Set peta medan CHIP borang (medan CHIP \u2192 id blok borang), cth {\"full_name\":\"block_abc\",\"email\":\"block_def\"}. Mapping kosong = guna lalai." },
    ],
  },
  {
    group: "CHIP account",
    tools: [
      { name: "get_chip_settings", desc: "Baca status kredensial CHIP akaun: configured, verified, mode, brand_id, secret_key_masked, email_fallback, whitelist. Secret penuh tidak pernah dipulangkan. Read-only." },
      { name: "set_chip_settings", desc: "Set kredensial CHIP akaun (brand_id + secret_key). Secret kosong = kekalkan kunci sedia ada. Disahkan terhadap CHIP sebelum disimpan. Admin sahaja." },
      { name: "clear_chip_settings", desc: "Kosongkan kredensial CHIP akaun. Admin sahaja." },
    ],
  },
  {
    group: "Form email & notifications",
    tools: [
      { name: "get_form_email_settings", desc: "Baca tetapan emel notifikasi SATU borang (issue #177): subjek/badan resit, hantar resit, notifikasi merchant, HTML. Kosong = guna lalai space." },
      { name: "set_form_email_settings", desc: "Set tetapan emel notifikasi SATU borang (issue #177): receipt_subject, receipt_body, receipt_enabled, notify_merchant, merchant_notify_email, html. Kosong subjek/badan = guna lalai space. Omit medan = kekal." },
      { name: "get_form_notifications", desc: "Baca senarai peraturan notifikasi emel per-borang (issue #193): event, penerima, subjek/badan, logik bersyarat, aktif. Termasuk notifikasi lalai (notify merchant on sale.paid)." },
      { name: "set_form_notifications", desc: "Ganti SEMUA peraturan notifikasi emel per-borang (issue #193). notifications[] penuh: {id?, name, event, to_email, subject, body, condition?, enabled?}. event: sale.paid|sale.failed|response.submitted|subscription.charged. to_email: emel, {admin_email}, atau {field_id}. condition: {field_id, op, value}|null (op: equals|not_equals|contains|gt|lt|not_empty). Senarai kosong = tiada emel dihantar." },
      { name: "get_form_confirmations", desc: "Baca pengesahan borang (issue #179): senarai confirmation (message/redirect) + logik bersyarat. Pengesahan lalai sentiasa wujud." },
      { name: "set_form_confirmations", desc: "Set pengesahan borang (issue #179): ganti semua confirmation (name, type message|redirect, message/redirect_url, condition). Pengesahan lalai sentiasa dikekalkan. Mesej HTML disanitasi (tiada XSS)." },
      { name: "get_form_failure_message", desc: "Baca mesej pembayaran-gagal borang (issue #393): apa yang pembeli lihat bila pembayaran gagal. Kosong = guna mesej lalai." },
      { name: "set_form_failure_message", desc: "Set mesej pembayaran-gagal borang (issue #393): HTML yang pembeli lihat bila pembayaran gagal (disanitasi, tiada XSS). Kosong = guna mesej lalai." },
    ],
  },
  {
    group: "Email settings",
    tools: [
      { name: "get_email_settings", desc: "Baca tetapan emel satu space: subjek/badan resit (placeholder), hantar resit, notifikasi merchant, HTML." },
      { name: "set_email_settings", desc: "Set tetapan emel space: receipt_subject, receipt_body ({{ref_code}}, {{amount}}, {{items}}, {{form_url}}, {{buyer_name}}, {{date}}), receipt_enabled, notify_merchant, merchant_notify_email, html. Omit medan = kekal." },
    ],
  },
  {
    group: "e-Invoice",
    tools: [
      { name: "set_einvoice", desc: "Set TIN + MSIC LHDN untuk resit e-Invoice (B2B)." },
      { name: "get_einvoice_settings", desc: "Baca TIN + MSIC LHDN semasa untuk resit e-Invoice." },
    ],
  },
  {
    group: "Webhooks",
    tools: [
      { name: "create_webhook", desc: "Daftar endpoint webhook (events: sale.paid|sale.failed|subscription.charged|*). HMAC-signed. form_id pilihan: set = webhook hanya untuk borang itu; kosong = seluruh space/akaun." },
      { name: "list_webhooks", desc: "Senarai outbound webhooks akaun. form_id pilihan: set = webhook borang itu sahaja; kosong = webhook seluruh space/akaun." },
      { name: "delete_webhook", desc: "Padam satu webhook (id dari list_webhooks)." },
      { name: "update_webhook", desc: "Kemas kini webhook (url, events, secret pilihan \u2014 kosong = kekal sedia ada). events: sale.paid|sale.failed|subscription.charged|*." },
      { name: "webhook_deliveries", desc: "Log penghantaran terkini webhook (status_code, attempt, error)." },
      { name: "retry_webhook_delivery", desc: "Cuba semula satu penghantaran webhook yang gagal dengan segera (id dari webhook_deliveries). Ia dienqueue semula dan dihantar semula pada sweep seterusnya." },
      { name: "get_webhook_signing_key", desc: "Baca kunci tandatangan Ed25519 akaun (key_id, public_key, alg) untuk mengesahkan tandatangan webhook X-JomForm-Signature-Ed25519. Read-only." },
    ],
  },
  {
    group: "API keys",
    tools: [
      { name: "list_api_keys", desc: "Senarai semua API key akaun (id, nama, scopes, created_at, last_used). Key material tidak pernah dikembalikan." },
      { name: "create_api_key", desc: "Cipta API key baharu (nama, scopes? readonly|readwrite, default readonly). Key penuh dipapar SEKALI sahaja \u2014 simpan segera; store simpan hash sahaja. SCOPES: baca untuk readonly, baca+tulis untuk readwrite." },
      { name: "revoke_api_key", desc: "Padam API key secara PERMANEN (id dari list_api_keys; tiada disable/enable semula \u2014 cipta key baru). AMARAN: jika id ialah key yang sedang mengesahkan sesi ini, ia akan memutuskan akses sendiri serta-merta \u2014 set confirm=true untuk sahkan; sebaliknya panggilan ditolak." },
    ],
  },
  {
    group: "Spaces & storefronts",
    tools: [
      { name: "list_spaces", desc: "Senarai semua space (storefront) anda \u2014 id, nama, slug dan URL." },
      { name: "create_space", desc: "Cipta space baharu. Slug 3-40 aksara (a-z, 0-9, dash); URL jadi https://<slug>.jomform.com." },
      { name: "get_space_settings", desc: "Baca tetapan storefront satu space: footer, company_reg, privacy/terms/refund URL, contact, menu." },
      { name: "set_space_settings", desc: "Set tetapan storefront space: footer_text, company_reg, privacy_url/terms_url/refund_url (mesti https://), contact_phone, contact_address, business_name, menu_json. Omit medan = kekal." },
      { name: "get_space_landing_page", desc: "Baca konfigurasi 'landing page' space (issue #360): hero, subtitle, body, image_url, theme colors, logo, CTA (label + form_slug). Kosong jika tiada landing page ditetapkan." },
      { name: "set_space_landing_page", desc: "Set 'landing page' storefront space (issue #360): enabled (true = tunjuk, false/kosong = jatuh ke senarai borang), hero, subtitle, body, image_url, primary_color, background_color, logo_url, ctas ([{label, form_slug}]). Hantar enabled:false untuk pulang ke senarai borang." },
      { name: "get_currency_rates", desc: "Kadar tukaran wang BNM (Ringgit per unit mata wang asing), dicache harian. Guna untuk paparan/penukaran amaun baharu." },
      { name: "set_space_currency", desc: "Set mata wang space (default MYR). Perubahan terpakai pada produk/jualan baharu; jualan lama kekal mata wang asal (tidak ditukar balik)." },
    ],
  },
  {
    group: "Space members",
    tools: [
      { name: "invite_space_member", desc: "Jemput ahli ke space (admin sahaja). Jika emel sudah berdaftar, tambah terus sebagai ahli; jika tidak, cipta jemputan menunggu. Role: read_only|manage|admin." },
      { name: "list_space_members", desc: "Senarai ahli space + peranan (admin sahaja)." },
      { name: "update_space_member_role", desc: "Tukar peranan ahli space (admin sahaja). Role: read_only|manage|admin." },
      { name: "remove_space_member", desc: "Buang ahli dari space (admin sahaja). Pemilik tidak boleh dibuang." },
      { name: "list_space_invites", desc: "Senarai jemputan menunggu untuk space (admin sahaja)." },
      { name: "cancel_space_invite", desc: "Batal jemputan menunggu untuk space (admin sahaja)." },
      { name: "set_default_space", desc: "Set space lalai anda (space yang anda masuk terus selepas log masuk, tanpa prompt). space_id kosong = buang default. Space mesti milik anda." },
    ],
  },
  {
    group: "Profile & security",
    tools: [
      { name: "update_profile_phone", desc: "Set nombor telefon pilihan pada profil anda (kosong = buang). Format longgar: digit, +, ruang, 8-15 digit." },
      { name: "update_profile_timezone", desc: "Set zona waktu IANA pilihan pada profil anda (kosong = Auto, ikut pelayar/PC). Contoh: Asia/Kuala_Lumpur, UTC." },
      { name: "update_profile_avatar", desc: "Set avatar profil (URL mesti imej yang dihoskan JomForm dari upload bucket). Kosong = buang avatar." },
      { name: "list_passkeys", desc: "Senarai passkey (WebAuthn) akaun anda: id. Read-only." },
      { name: "delete_passkey", desc: "Padam satu passkey (id dari list_passkeys)." },
      { name: "list_social_accounts", desc: "Senarai akaun sosial yang dipautkan (google|azure_ad|github|cloudflare): provider, emel, nama, picture, linked_at. Read-only." },
      { name: "unlink_social_account", desc: "Nyah-pautkan akaun sosial (provider dari list_social_accounts)." },
      { name: "get_2fa_status", desc: "Baca status 2FA akaun anda: enabled (true/false). Read-only." },
      { name: "setup_2fa", desc: "Jana rahsia TOTP untuk 2FA (belum aktif). Pulangkan secret + otpauth_url untuk diimbas aplikasi authenticator, kemudian panggil enable_2fa dengan kod." },
      { name: "enable_2fa", desc: "Aktifkan 2FA dengan kod 6-digit dari aplikasi authenticator (selepas setup_2fa)." },
      { name: "disable_2fa", desc: "Matikan 2FA (perlu kata laluan akaun untuk pengesahan)." },
    ],
  },
  {
    group: "Legal pages",
    tools: [
      { name: "generate_legal_page", desc: "Jana draf halaman undang-undang (privacy|terms|refund) untuk space melalui LLM (deepseek-v4-flash) dengan template yang diisi butiran perniagaan. Pulangkan draf untuk semakan \u2014 belum disimpan sehingga set_legal_page dipanggil." },
      { name: "get_legal_page", desc: "Baca satu halaman undang-undang space (privacy|terms|refund): mode (auto/manual) + kandungan." },
      { name: "set_legal_page", desc: "Simpan kandungan halaman undang-undang space (privacy|terms|refund) dengan mode auto|manual. Kandungan dipaparkan di {space}.jomform.com/privacy|terms|refund." },
    ],
  },
  {
    group: "Event tracking",
    tools: [
      { name: "list_events", desc: "Log audit/tingkah-laku akaun (terbaru dahulu). Boleh tapis event_name, from/to (RFC3339), dan page/limit. Read-only." },
    ],
  },
  {
    group: "Email delivery",
    tools: [
      { name: "list_email_deliveries", desc: "Status penghantaran emel akaun (terbaru dahulu): delivered/bounce/complaint/open/click dengan emel, masa, sale_ref dan detail. Boleh tapis form_id, sale_ref, event_type, dan page/limit. Read-only." },
    ],
  },
  {
    group: "Form submissions",
    tools: [
      { name: "list_form_submissions", desc: "Senarai respons (submission) untuk satu borang respons-sahaja (mode 'response', tanpa bayaran). Pulangkan data medan + masa. Tapis form_id, page/limit." },
    ],
  },
  {
    group: "Spam review",
    tools: [
      { name: "list_spam_submissions", desc: "Senarai submission yang ditanda spam oleh AI (issue #245): data medan, sebab spam, masa. Boleh tapis page/limit. Read-only." },
      { name: "unspam_submission", desc: "Nyah-tanda spam pada submission (false positive). Write tool (peranan manage/admin)." },
      { name: "list_spam_sales", desc: "Senarai jualan yang ditanda spam oleh AI (issue #245): ref, status, jumlah, pembeli, sebab spam, masa. Boleh tapis page/limit. Read-only." },
      { name: "unspam_sale", desc: "Nyah-tanda spam pada jualan (false positive). Write tool (peranan manage/admin)." },
    ],
  },
  {
    group: "Media",
    tools: [
      { name: "list_media", desc: "Senarai media yang dimuat naik akaun (perpustakaan media): thumbnail/url, nama, tarikh, saiz. Boleh tapis form_id dan source (merchant|customer). Read-only." },
      { name: "upload_media", desc: "Muat naik media/aset ke space anda (issue #363): kind='image' (png/jpeg/webp/gif \u2014 dioptimumkan, strip EXIF, terus boleh guna; png/jpeg/webp dapat saiz thumbnail/medium/full), kind='asset' (css/js/html/svg/woff2 \u2014 disimpan asli, dikaji sebelum dipapar) atau kind='pdf' (disimpan asli, terus boleh guna). hantar content fail sebagai base64 di 'data'. Pulangkan URL untuk diguna sebagai image_url/logo_url landing atau <link>/<script> landing page." },
    ],
  },
  {
    group: "Templates",
    tools: [
      { name: "list_templates", desc: "Senarai template borang sistem (gallery): slug, tajuk, kategori, mode (payment/response). Guna dengan use_template untuk quickstart." },
      { name: "use_template", desc: "Clone template borang (slug dari list_templates) menjadi draft baharu dalam akaun anda. Pulangkan form_id + slug untuk dibuka dalam builder." },
    ],
  },
  {
    group: "Analytics",
    tools: [
      { name: "get_form_analytics", desc: "Analitik per-borang: bilangan submission, paid/confirmed/failed/pending, dan hasil (RM). Merentas semua space anda. Read-only." },
      { name: "get_product_analytics", desc: "Analitik per-produk: unit terjual (paid), hasil (RM). Merentas semua space anda. Read-only." },
    ],
  },
  {
    group: "Custom domains",
    tools: [
      { name: "list_domains", desc: "Senarai domain tersuai akaun: id, domain, provider, verified, auto_cname. Read-only." },
      { name: "add_domain", desc: "Tuntut domain tersuai baharu (cth: kedai.com). Kemudian set CNAME \u2192 custom.jomform.com dan panggil verify_domain." },
      { name: "verify_domain", desc: "Sahkan domain tersuai (id dari list_domains). Hanya disahkan bila domain benar-benar memaparkan halaman JomForm." },
      { name: "remove_domain", desc: "Buang tuntutan domain tersuai (id dari list_domains)." },
      { name: "auto_cname_domain", desc: "Cipta rekod CNAME Cloudflare secara automatik untuk domain (perlu Cloudflare dipautkan). Pulangkan auto_cname=created|failed." },
    ],
  },
  {
    group: "Tracking",
    tools: [
      { name: "get_tracking", desc: "Baca skrip penjejakan global akaun (Google Analytics, Facebook Pixel, dll) yang disuntik ke <head> borang awam. Read-only." },
      { name: "set_tracking", desc: "Set skrip penjejakan global akaun. Hanya <script src=https://...>, <meta> dan komen dibenarkan (disanitasi)." },
    ],
  },
  {
    group: "System settings",
    tools: [
      { name: "get_system_settings", desc: "Baca semua tetapan sistem platform (site_*, landing_*). Read-only." },
      { name: "set_system_setting", desc: "Set satu tetapan sistem (key mesti bermula site_ atau landing_)." },
    ],
  },
  {
    group: "Cloudflare",
    tools: [
      { name: "get_cloudflare_status", desc: "Baca status pautan Cloudflare akaun: linked, configured. Read-only." },
      { name: "connect_cloudflare_api_token", desc: "Pautkan Cloudflare via API token (disahkan serta-merta, disimpan terenkripsi)." },
      { name: "connect_cloudflare_global_key", desc: "Pautkan Cloudflare via global API key (disahkan serta-merta, disimpan terenkripsi)." },
      { name: "disconnect_cloudflare", desc: "Nyah-pautkan Cloudflare dari akaun." },
    ],
  },
  {
    group: "Account",
    tools: [
      { name: "get_account", desc: "Maklumat asas akaun (nama, slug, CHIP mode)." },
    ],
  },
  {
    group: "Admin (super-admin, platform-wide)",
    tools: [
      { name: "purge_unverified_accounts", desc: "Laporkan bilangan akaun belum disahkan (emel) yang lebih lama daripada ambang dan akan dipadam automatik oleh sweep. Admin sahaja; read-only \u2014 tidak memadam apa-apa." },
      { name: "admin_list_spaces", desc: "Senarai SEMUA space (storefront) platform: id, nama, slug, emel pemilik, tarikh cipta, bilangan borang + produk. Super-admin sahaja; read-only; diaudit." },
      { name: "admin_media_usage", desc: "Penggunaan storan media per space (bait) berbanding kuota 1GB setiap space. Super-admin sahaja; read-only; diaudit." },
      { name: "admin_list_sales", desc: "Senarai jualan merentas SEMUA space, boleh tapis (date_from/date_to, space_id, status). Super-admin sahaja; read-only; diaudit." },
      { name: "admin_spam_queue", desc: "Senarai semua item spam (submission + jualan) merentas semua space. Super-admin sahaja; read-only; diaudit." },
      { name: "admin_email_deliveries", desc: "Status penghantaran emel merentas SEMUA space (delivered/bounce/complaint/open/click), boleh tapis sale_ref/event_type/form_id. Super-admin sahaja; read-only; diaudit." },
      { name: "admin_system_health", desc: "Kesihatan sistem: uptime, bilangan ralat MCP terkini (24j), kedalaman queue emel. Super-admin sahaja; read-only; diaudit." },
    ],
  },
];

export default function McpReferencePage() {
  return (
    <div className="max-w-3xl">
      <h1 className="text-3xl font-bold tracking-tight text-slate-900">MCP tool reference</h1>
      <p className="mt-3 text-sm leading-relaxed text-slate-600">
        Every tool JomForm exposes to connected AI agents via the Model Context Protocol. Connect any MCP client to <code className="rounded bg-slate-100 px-1">https://mcp.jomform.com/mcp</code> and agents discover these tools automatically. This reference is kept in sync with the tool registry.
      </p>
      <p className="mt-2 text-sm text-slate-500">
        {TOOL_GROUPS.reduce((n, g) => n + g.tools.length, 0)} tools across {TOOL_GROUPS.length} categories.
      </p>

      {TOOL_GROUPS.map((g) => (
        <section key={g.group} className="mt-8">
          <h2 className="text-lg font-semibold text-slate-900">{g.group}</h2>
          <div className="mt-3 space-y-3">
            {g.tools.map((t) => (
              <div key={t.name} className="rounded-lg border border-slate-200 p-4">
                <code className="font-mono text-sm font-semibold text-[var(--jf-primary)]">{t.name}</code>
                <p className="mt-1 text-sm leading-relaxed text-slate-600">{t.desc}</p>
              </div>
            ))}
          </div>
        </section>
      ))}
    </div>
  );
}
