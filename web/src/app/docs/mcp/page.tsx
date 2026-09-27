import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "MCP tool reference — JomForm Docs",
  description: "Every MCP tool JomForm exposes to AI agents: name, purpose, inputs and outputs. Kept in sync with the tool registry in api/internal/mcpsrv/tools.go.",
  alternates: { canonical: "https://jomform.com/docs/mcp" },
};

// Mirrors api/internal/mcpsrv/tools.go (issue #395). It is NOT auto-generated:
// this list is edited by hand, which is exactly how it fell ten tools behind the
// registry. Keep it in step when tools are added or changed — and
// src/lib/mcpToolInventory.test.ts fails the build if you forget.
//
// Every row here must be a REGISTERED tool, and this page lists the registry in
// full: both directions are enforced by src/lib/mcpToolInventory.test.ts, which
// derives both sets and fails on either kind of drift. A group may carry a `note`
// for prose that is not a tool; a note is NOT a tool row (the old
// "Keselamatan sesi" group's em-dash row inflated the rendered count and listed
// nothing).
//
// NOTE for anyone editing these comments: src/lib/docsToolClaims.test.ts scans
// this file for a name presented as a tool, so an identifier in brackets a few
// words after the word "tool"/"tools" reads as a phantom claim. Keep card ids and
// issue references out of a bracketed aside right after that word.
const TOOL_GROUPS: { group: string; note?: string; tools: { name: string; desc: string }[] }[] = [
  {
    group: "Forms",
    tools: [
      { name: "list_forms", desc: "Senarai borang akaun anda: id, slug, tajuk, status (draft|private|published|unlisted), mode (payment|response) dan URL awam bila ia live. Read-only \u2014 guna untuk penemuan id borang sebelum membaca atau menulis." },
      { name: "get_form", desc: "Baca SATU borang sepenuhnya (read-only): id, slug, tajuk, status, mode, config \u2014 TERMASUK blocks semasa \u2014 theme, registered_fields, URL awam, created_at dan updated_at. Baca borang dahulu, ubah hanya medan yang dikehendaki, kemudian hantar semula config penuh melalui upsert_form. Menulis tanpa membaca dahulu boleh memusnahkan blok sedia ada." },
      { name: "upsert_form", desc: "Cipta/kemaskini borang (form_id kosong = baru). Baca sedia ada dengan get_form dahulu dan hantar config penuh supaya blok sedia ada tidak hilang. Theme (pilihan) = gaya/branding tersuai. slug (pilihan) = laluan URL borang: pada cipta ia digunakan bila diisi; pada kemaskini ia MENAMAKAN SEMULA borang (a-z, 0-9, sengkang; 1-40 aksara), URL lama kekal berfungsi melalui ubah hala sementara (301), dan slug yang sudah dipakai borang lain dalam workspace ini ditolak dengan ralat. Kosongkan slug untuk kekalkan yang sedia ada." },
      { name: "set_form_theme", desc: "Set gaya/branding tersuai borang (issue #254): primary, background, text, card_background, card_border_radius, font_family, logo_url (mesti https:// — issue #501), layout (centered|full_width|split), spacing (compact|comfortable|spacious), product_image_size (small|medium|large|hidden, issue #471), product_image_layout (thumbnail-left|image-top — kad menu), custom_css, custom_js (issue #388 — JS tersuai untuk tingkah laku borang; dikawal pemilik + diimbas). Agent bebas set CSS/JS; JomForm kekal kuasa ke atas harga & pembayaran." },
      { name: "register_form_fields", desc: "Daftar medan input borang (issue #254) dengan kontrak ketat: {field_id, type, label, required, validation?, product_id?}. type mesti jenis input JomForm (name|email|phone|textarea|number|select|radio|checkbox|date|time|website|address|consent|hidden|list|repeater|image-choice|captcha). Ganti semua medan sedia ada. Harga kekal dari DB produk (server authority)." },
      { name: "generate_form", desc: "Jana konfigurasi borang (blok + gaya) dari prompt semula jadi (issue #256) via deepseek-v4.1-flash. Output disahkan server-side terhadap jenis blok JomForm; harga produk kekal dari DB (server authority). Semak/edit dulu, kemudian simpan guna upsert_form." },
      { name: "publish_form", desc: "Publish borang \u2014 live di <workspace>.jomform.com/<slug> atau custom domain. status=private (dengan password) menjadikannya borang ber-kata laluan; status=draft menarik balik. Borang bayaran tanpa produk ditolak SEKALI dengan mesej jelas (ia tidak boleh menerima bayaran) \u2014 hantar acknowledge=true untuk teruskan. Borang response dan borang yang sudah live tidak pernah ditanya." },
      { name: "unpublish_form", desc: "Tarik balik borang dari live (jadikan draft)." },
      { name: "set_form_password", desc: "Set/buang kata laluan akses borang private (argon2id-hashed; tidak pernah dipulangkan). Password kosong = buang kata laluan." },
      { name: "get_form_restrictions", desc: "Baca sekatan borang (issue #207): jadual mula/tamat (start_at/end_at) + had penyertaan (entry_limit) + mesej peringkat." },
      { name: "set_form_restrictions", desc: "Set sekatan borang (issue #207): jadual mula/tamat + had penyertaan + mesej. Kosong = buang sekatan." },
      { name: "get_form_og_image", desc: "Baca imej Open Graph (og:image) borang (issue #411): URL imej tersuai bila ditetapkan, kosong = guna lalai janaan /og/{slug}.png." },
      { name: "set_form_og_image", desc: "Set imej Open Graph (og:image) borang (issue #411): URL imej tersuai (mesti https://) untuk pratonton sosial bila borang dikongsi. Kosong = guna lalai janaan." },
      { name: "delete_form", desc: "Soft-delete borang (pergi ke trash dashboard; slug dilepaskan). Boleh dipulihkan \u2014 guna restore_form." },
      { name: "list_deleted_forms", desc: "Senarai borang dalam TONG SAMPAH (deleted_at IS NOT NULL): id, slug, title, deleted_at, sales_count dan can_purge. Borang dengan jualan tidak boleh dipadam kekal (can_purge=false)." },
      { name: "restore_form", desc: "Pulihkan borang dari tong sampah (deleted_at=NULL). Tiada apa-apa dimusnahkan \u2014 borang kembali dengan slug asalnya. Jika slug sudah diambil borang lain, pulihan DITOLAK (tiada penamaan semula automatik) \u2014 tukar slug borang itu dahulu." },
      { name: "purge_form", desc: "Padam borang secara KEKAL dari tong sampah (submissions/notifikasi/confirmations/tetapan emel borang/webhook borang gugur bersama). AMARAN: tidak boleh dibatalkan. Wajib confirm_slug = slug borang itu. Borang yang ADA JUALAN ditolak \u2014 sales.form_id ialah ON DELETE SET NULL, jadi padam kekal akan menjadikan jualan yatim secara senyap. Hanya borang dalam tong sampah \u2014 padam dulu guna delete_form." },
      { name: "bulk_forms", desc: "Tindakan pukal atas banyak borang sekali gus: action=publish|draft|delete dengan ids[] (maks 500). Borang bayaran tanpa produk DIKECUALIKAN dari publish dan dipulangkan dalam needs_products \u2014 hantar acknowledge=true untuk menerbitkannya juga." },
    ],
  },
  {
    group: "Products",
    tools: [
      { name: "list_products", desc: "Senarai semua produk aktif dalam akaun JomForm anda." },
      { name: "create_product", desc: "Cipta produk baharu. Untuk langganan, set type='subscription' dan billing_period='month'|'year'." },
      { name: "archive_product", desc: "Nyahaktifkan produk (dulang tanpa padam data)." },
      { name: "list_product_variations", desc: "Senarai variasi produk (name, price_sen, stock, images, sort_order, dan kitaran langganan sendiri bila ada) untuk satu produk. Produk simple = senarai kosong. billing_period/trial_days/subscription_charges HANYA hadir bila variasi menetapkannya sendiri; jika tiada, variasi itu mewarisi kitaran produk." },
      { name: "create_product_variation", desc: "Tambah SATU variasi pada produk (cth: Saiz M), dengan harga, stok, gambar dan kitaran langganannya sendiri (billing_period month|year, trial_days, subscription_charges). Biarkan kosong = variasi mewarisi kitaran produk." },
      { name: "update_product_variation", desc: "Kemaskini SATU variasi (nama, harga, stok, gambar, kitaran langganan). Medan yang tidak diisi kekal. Set clear_billing_period / clear_trial_days / clear_subscription_charges = true untuk BUANG kitaran variasi supaya ia mewarisi produk semula." },
      { name: "delete_product_variation", desc: "Padam SATU variasi dari produk. Variasi terakhir yang dipadam menjadikan produk itu produk simple semula." },
      { name: "update_product", desc: "Kemaskini produk: nama, harga (price_sen), stok, jenis (type), billing_period (langganan), aktif/tidak, images (URL imej mesti https:// — issue #501; omit = tak berubah). Omit medan tak berubah." },
      { name: "bulk_products", desc: "Arkibkan banyak produk sekali gus (active=false). ids[] maks 500." },
      { name: "delete_product", desc: "Hantar produk ke TONG SAMPAH (deleted_at diisi) \u2014 ia keluar dari senarai produk dan boleh dipulihkan guna restore_product. Ini berbeza daripada archive_product (active=false), yang kekal dalam senarai. Untuk nyahaktifkan sahaja, guna archive_product." },
      { name: "list_deleted_products", desc: "Senarai produk dalam TONG SAMPAH: id, name, price_sen, deleted_at, sales_count, subscription_count dan can_purge. can_purge=false bermakna produk itu masih dirujuk jualan (sejarah) atau subscription (bil masih berjalan) \u2014 padam kekal akan DITOLAK." },
      { name: "restore_product", desc: "Pulihkan produk dari tong sampah (deleted_at=NULL). Tiada apa-apa dimusnahkan dan tiada nama yang perlu diselesaikan \u2014 produk tiada slug \u2014 jadi pulihan tidak boleh berlanggar." },
      { name: "purge_product", desc: "Padam produk secara KEKAL dari tong sampah (variasinya gugur bersama melalui CASCADE). AMARAN: tidak boleh dibatalkan. Wajib confirm_name = nama produk itu. DITOLAK bila produk ada jualan (sejarah jualan tidak dimusnahkan) atau masih ada subscription (FK RESTRICT). Hanya produk dalam tong sampah \u2014 hantar ke bin dulu guna delete_product." },
    ],
  },
  {
    group: "Sales",
    tools: [
      { name: "list_sales", desc: "Senarai rekod terkini (terbaru dahulu). Tanpa form_id ini ialah SELURUH set rekod akaun: jualan DAN respons borang mode 'response' (sale_type 'response', submission_id + data) — sama seperti halaman Sales, bukan senarai kosong atau separa. Dengan form_id, mode borang itu menentukan: borang bayaran → jualan, borang respons → respons. Setiap jualan: refunded_sen (telah direfund), outstanding_sen (baki boleh refund), refund_status (none|partial|full) — issue #518. Status ialah soalan bayaran, jadi menetapkan status mengecualikan bahagian respons." },
      { name: "get_sales_summary", desc: "Ringkasan jualan: bilangan mengikut status (termasuk refunded), refunded_sen (jumlah direfund) dan revenue_sen (nilai bersih selepas refund, issue #518)." },
      { name: "confirm_sale", desc: "Sahkan jualan (contoh: selepas semak bukti bank transfer)." },
      { name: "get_sale_email_status", desc: "Status emel resit untuk satu jualan: Sent \u2192 Delivered \u2192 Opened \u2192 (Clicked) dengan masa. Guna ref_code dari list_sales." },
      { name: "export_sales_csv", desc: "Eksport set rekod akaun sebagai CSV. Tanpa form_id ia mengandungi jualan DAN respons (satu lajur record_type menandakan setiap baris, serta lajur jawapan yang direkodkan), sepadan dengan senarai yang dipaparkan. Boleh tapis form_id/status/date_from/date_to/q — penapis yang sama seperti senarai, supaya fail dan halaman sepadan. Kolum refund mendedahkan refund sebahagian (issue #518). Pulangkan kandungan csv." },
    ],
  },
  {
    group: "Skip-capture",
    tools: [
      { name: "capture_sale", desc: "Capture authorisasi skip_capture (kad buyer dicaj sekarang). amount_sen optional untuk partial." },
      { name: "release_sale", desc: "Release (void) authorisasi skip_capture \u2014 kad buyer TIDAK dicaj." },
      { name: "refund_sale", desc: "Refund jualan yang telah dibayar (paid/confirmed). amount_sen = jumlah untuk refund separa; kosong = refund penuh baki yang tinggal. Jumlah mesti > 0 dan refunded_sen + amount_sen <= total_sen — refund melebihi baki ditolak sebelum CHIP dipanggil (issue #518). Refund separa MEREKOD jumlahnya dan jualan kekal 'paid'/'confirmed' dengan refund_status='partial'; hanya apabila baki penuh direfund barulah status jadi 'refunded'. Balasan: refunded_sen, outstanding_sen, refund_status, fully_refunded." },
      { name: "capture_all_pending_sales", desc: "Capture SEMUA authorisasi skip_capture yang masih menunggu. Satu kegagalan tidak hentikan yang lain; balas per-ref." },
    ],
  },
  {
    group: "Subscriptions",
    tools: [
      { name: "list_subscriptions", desc: "Senarai langganan akaun: pelanggan (emel), produk, jumlah/period, status (active/past_due/cancelled), tarikh caj seterusnya. Tapis ?status, page/limit." },
      { name: "cancel_subscription", desc: "Batalkan langganan pelanggan. Kad token kekal pada merchant \u2014 boleh aktif semula." },
      { name: "retry_subscription", desc: "Cuba semula langganan yang past_due (round 13c): kembalikan status kepada active dan tetapkan ia due supaya sweep seterusnya mencaj kad semula. Guna selepas pelanggan top-up atau betulkan kad. Hanya past_due layak; cancelled/completed ditolak. Tidak mencaj serta-merta." },
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
      { name: "get_form_email_settings", desc: "Baca tetapan emel SATU borang (issue #453): override per-borang (subjek/badan resit, hantar resit, notifikasi merchant, HTML), tetapan workspace yang diwarisi, dan nilai efektif (resolusi per-borang -> workspace -> lalai)." },
      { name: "set_form_email_settings", desc: "Set override emel SATU borang (issue #453): receipt_subject, receipt_body, receipt_enabled_override, notify_merchant_override, merchant_notify_email, html_override. Toggle tri-state — true/false = override, null = ikut workspace. Omit medan = kekal." },
      { name: "clear_form_email_overrides", desc: "Padam SEMUA override emel per-borang supaya borang kembali mewarisi tetapan emel workspace (issue #453)." },
      { name: "get_form_notifications", desc: "Baca senarai peraturan notifikasi emel per-borang (issue #193): event, penerima, subjek/badan, logik bersyarat, aktif. Setiap borang baharu disertakan satu notifikasi lalai kepada merchant: event sale.paid untuk borang pembayaran, response.submitted untuk borang respons (issue #597)." },
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
      { name: "get_email_settings", desc: "Baca tetapan emel satu workspace: subjek/badan resit (placeholder), hantar resit, notifikasi merchant, HTML." },
      { name: "set_email_settings", desc: "Set tetapan emel workspace: receipt_subject, receipt_body ({{ref_code}}, {{amount}}, {{items}}, {{form_url}}, {{buyer_name}}, {{date}}), receipt_enabled, notify_merchant, merchant_notify_email, html. Omit medan = kekal." },
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
      { name: "create_webhook", desc: "Daftar endpoint webhook (events: sale.paid|sale.failed|subscription.charged|*). HMAC-signed. form_id pilihan: set = webhook hanya untuk borang itu; kosong = seluruh workspace/akaun." },
      { name: "list_webhooks", desc: "Senarai outbound webhooks akaun. form_id pilihan: set = webhook borang itu sahaja; kosong = webhook seluruh workspace/akaun." },
      { name: "delete_webhook", desc: "Padam satu webhook (id dari list_webhooks)." },
      { name: "update_webhook", desc: "Kemas kini webhook (url, events, secret pilihan \u2014 kosong = kekal sedia ada). events: sale.paid|sale.failed|subscription.charged|*." },
      { name: "webhook_deliveries", desc: "Log penghantaran terkini webhook (status_code, attempt, error)." },
      { name: "retry_webhook_delivery", desc: "Cuba semula satu penghantaran webhook yang gagal dengan segera (id dari webhook_deliveries). Ia dienqueue semula dan dihantar semula pada sweep seterusnya." },
      { name: "get_webhook_signing_key", desc: "Baca kunci tandatangan Ed25519 akaun (key_id, public_key, alg) untuk mengesahkan tandatangan webhook X-JomForm-Signature-Ed25519. Membaca kunci sedia ada tidak memerlukan skop tulis; JANA kunci baharu (kali pertama, atau selepas dipadam) memerlukan readwrite." },
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
    // t_cde2e73a: the affiliate tools (#444) had no row of their own on this
    // page, so the reference could not be "kept in sync with the tool
    // registry" while claiming to be.
    group: "Affiliates",
    tools: [
      { name: "create_affiliate", desc: "Cipta affiliate untuk workspace: kod rujukan (?aff=CODE), nama, emel, kadar komisen. Jualan yang dibawa affiliate dikreditkan kepadanya." },
      { name: "list_affiliates", desc: "Senarai semua affiliate dalam satu workspace \u2014 id, kod, nama, komisen, URL rujukan." },
      { name: "get_affiliate_sales", desc: "Senarai jualan yang dibawa oleh satu affiliate (read-only, skop workspace)." },
      { name: "set_affiliate_commission", desc: "Set kadar komisen affiliate (cth: 10 = 10%)." },
    ],
  },
  {
    group: "Workspaces & storefronts",
    tools: [
      { name: "list_workspaces", desc: "Senarai semua workspace (storefront) anda \u2014 id, nama, slug dan URL." },
      { name: "create_workspace", desc: "Cipta workspace baharu. Slug 3-40 aksara (a-z, 0-9, dash); URL jadi https://<slug>.jomform.com. Nama yang dikhaskan untuk JomForm (cth: www, api, app, mail, s3, docs, mcp, templates, custom, support, help, status, billing, admin, account, login, payments, checkout, cdn, assets) ditolak — nama itu milik JomForm atau melindungi pengguna daripada penipuan (cth: billing.jomform.com yang menyamar sebagai saluran rasmi JomForm)." },
      { name: "get_workspace_settings", desc: "Baca tetapan storefront satu workspace: footer, company_reg, privacy/terms/refund URL, contact, menu, indexing, sound_enabled, spam_judge_enabled (AI spam detection, this change — true = hidup/lalai)." },
      { name: "set_workspace_settings", desc: "Set tetapan storefront workspace: footer_text, company_reg, privacy_url/terms_url/refund_url (mesti https://), contact_phone, contact_address, business_name, menu_json, default_lang (bahasa lalai borang AWAM untuk workspace ini — en, ms, zh, ta atau ar; ini bahasa pembeli, bukan bahasa dashboard anda), indexing, sound_enabled, spam_judge_enabled (this change: false = MATIKAN AI spam detection untuk workspace ini — tiada job AI dihantar langsung dan is_spam tidak lagi ditetapkan, jadi /spam kekal kosong). Omit medan = kekal." },
      { name: "get_workspace_landing_page", desc: "Baca konfigurasi 'landing page' workspace (issue #360): hero, subtitle, body, image_url, theme colors, logo, CTA (label + form_slug). Kosong jika tiada landing page ditetapkan." },
      { name: "set_workspace_landing_page", desc: "Set 'landing page' storefront workspace (issue #360): enabled (true = tunjuk, false/kosong = jatuh ke senarai borang), hero, subtitle, body, image_url (mesti https:// — issue #501), primary_color, background_color, logo_url (mesti https:// — issue #501), ctas ([{label, form_slug}]). Hantar enabled:false untuk pulang ke senarai borang." },
      { name: "generate_qr", desc: "Jana QR code untuk borang diterbitkan (issue #438): form_slug + table pilihan (nombor meja, dikodkan sebagai ?table=N). Pulangkan qr_url, data_url (PNG base64) dan target_url. Guna untuk stand QR di meja restoran." },
      { name: "get_menu_page", desc: "Baca konfigurasi menu restoran satu BORANG (issue #497): form_id. Pulangkan enabled, categories ([{name, product_ids}]), best_seller, best_seller_ids (senarai, #485), order_form_slug (borang maklumat tambahan pesanan, #491/#493). Menu ialah tetapan PER-BORANG dan dipapar di URL borang itu sendiri. Kosong jika borang tiada menu." },
      { name: "set_menu_page", desc: "Set menu restoran untuk satu BORANG (issue #497): form_id, enabled (true = borang ini jadi menu di URL-nya; false = borang kembali normal), categories ([{name, product_ids}]), best_seller (ID produk) atau best_seller_ids (senarai ID untuk seksyen Best Seller, #485 — mengatasi best_seller), order_form_slug (pilihan, #491/#493 — borang diterbitkan dalam akaun sama yang medan bloknya menjadi medan tambahan semasa checkout menu; medan dirender dengan label/required borang itu, disahkan server-side, dan disimpan pada jualan; kosong = checkout menu nama + emel sahaja). Produk menu datang dari blok produk BORANG itu sendiri (bukan semua produk akaun), jadi produk akaun lain tidak boleh bocor masuk; kategori/best-seller mesti merujuk produk yang dirujuk borang. Halaman menu menunjukkan nombor meja dari ?table=N, ada penapis kategori + carian, dan checkout terus dari halaman menu (nama + emel pembeli wajib untuk CHIP — tidak boleh dibuang walaupun borang tambahan tidak menyenaraikannya)." },
      { name: "get_currency_rates", desc: "Kadar tukaran wang BNM (Ringgit per unit mata wang asing), dicache harian. Guna untuk paparan/penukaran amaun baharu." },
      { name: "set_workspace_currency", desc: "Set mata wang workspace (default MYR). Perubahan terpakai pada produk/jualan baharu; jualan lama kekal mata wang asal (tidak ditukar balik)." },
      { name: "check_workspace_deletion", desc: "Semak sama ada satu workspace boleh dipadam (read-only, tiada kesan). Pulangkan can_delete, empty, last_workspace, is_owner dan blockers[] (setiap kategori + kiraan). Guna ini dahulu supaya anda boleh menyatakan sebabnya, bukan gagal selepas menekan padam." },
      { name: "delete_workspace", desc: "PADAM workspace secara kekal (pemilik sahaja \u2014 ahli admin tidak boleh; mereka keluar melalui remove_workspace_member). TIGA syarat: (1) confirm_slug wajib sepadan dengan slug workspace \u2014 ini tidak boleh dibatalkan, jadi anda mesti namakan apa yang anda musnahkan; (2) anda mesti ada sekurang-kurangnya satu workspace LAIN \u2014 workspace terakhir tidak boleh dipadam, kerana satu pengguna wajib ada satu workspace; (3) workspace mesti KOSONG \u2014 borang (termasuk yang dalam tong sampah), produk, jualan, domain tersuai, webhook, kunci API aktif dan langganan aktif semuanya menyekat, dan penolakan menyenaraikan setiap kategori dengan kiraannya. Jualan tidak pernah dimusnahkan: selesaikan dahulu. Akaun CHIP workspace dipadam BERSAMA (kredential tidak pernah dikongsi atau ditinggalkan yatim). Untuk borang: delete_form (trash) kemudian purge_form (kekal)." },
    ],
  },
  {
    group: "Workspace members",
    tools: [
      { name: "invite_workspace_member", desc: "Jemput ahli ke workspace (admin sahaja). Jika emel sudah berdaftar, tambah terus sebagai ahli; jika tidak, cipta jemputan menunggu. Role: read_only|manage|admin." },
      { name: "list_workspace_members", desc: "Senarai ahli workspace + peranan (admin sahaja)." },
      { name: "update_workspace_member_role", desc: "Tukar peranan ahli workspace (admin sahaja). Role: read_only|manage|admin." },
      { name: "remove_workspace_member", desc: "Buang ahli dari workspace (admin sahaja). Pemilik tidak boleh dibuang." },
      { name: "list_workspace_invites", desc: "Senarai jemputan menunggu untuk workspace (admin sahaja)." },
      { name: "cancel_workspace_invite", desc: "Batal jemputan menunggu untuk workspace (admin sahaja)." },
      { name: "set_default_workspace", desc: "Set workspace lalai anda (workspace yang anda masuk terus selepas log masuk, tanpa prompt). workspace_id kosong = buang default. Workspace mesti milik anda." },
    ],
  },
  {
    group: "Profile & security",
    tools: [
      { name: "update_profile_phone", desc: "Set nombor telefon pilihan pada profil anda (kosong = buang). Format longgar: digit, +, ruang, 8-15 digit." },
      { name: "update_profile_timezone", desc: "Set zona waktu IANA pilihan pada profil anda (kosong = Auto, ikut pelayar/PC). Contoh: Asia/Kuala_Lumpur, UTC." },
      { name: "update_profile_language", desc: "Set bahasa DASHBOARD pilihan pada profil anda (per-user, ikut anda ke mana-mana pelayar): en, ms, zh, ta atau ar (kosong = Auto, ikut nilai terakhir pelayar itu, kemudian en). Ini bahasa anda sendiri di dashboard, BUKAN bahasa pembeli pada borang awam — itu ialah default_lang workspace (set_workspace_settings) atau config.lang per borang." },
      { name: "update_profile_avatar", desc: "Set avatar profil (URL mesti imej yang dihoskan JomForm dari upload bucket). Kosong = buang avatar." },
      { name: "list_passkeys", desc: "Senarai passkey (WebAuthn) akaun anda: id. Read-only." },
      { name: "delete_passkey", desc: "Padam satu passkey (id dari list_passkeys)." },
      { name: "list_social_accounts", desc: "Senarai akaun sosial yang dipautkan (google|azure_ad|github|cloudflare): provider, emel, nama, picture, linked_at. Read-only." },
      { name: "unlink_social_account", desc: "Nyah-pautkan akaun sosial (provider dari list_social_accounts)." },
      { name: "get_2fa_status", desc: "Baca status 2FA akaun anda: enabled (true/false). Read-only." },
      { name: "setup_2fa", desc: "Jana rahsia TOTP untuk 2FA (belum aktif). Pulangkan secret + otpauth_url untuk diimbas aplikasi authenticator, kemudian panggil enable_2fa dengan kod." },
      { name: "enable_2fa", desc: "Aktifkan 2FA dengan kod 6-digit dari aplikasi authenticator (selepas setup_2fa)." },
      { name: "disable_2fa", desc: "Matikan 2FA akaun anda. TEPAT SATU bukti diperlukan, bukan kedua-duanya: kod 2FA semasa (totp_code — kod 6 digit dari aplikasi authenticator, disahkan sekali sahaja dan tidak memerlukan kata laluan) ATAU kata laluan akaun (password). Menghantar kedua-duanya ditolak oleh skema, begitu juga tanpa kedua-duanya, dan kunci API sahaja tidak pernah memadai. Borang di dashboard (Profil \u2192 Keselamatan) meminta kod 2FA semasa. Mematikan 2FA juga membatalkan SEMUA peranti dipercayai akaun — peranti yang melangkau faktor yang kini dibuang tidak boleh kekal dikecualikan, sama seperti borang profil dashboard; guna list_trusted_devices untuk semak baki peranti." },
      { name: "list_trusted_devices", desc: "Senarai peranti dipercayai 2FA akaun anda (label, ip, first_seen, expires_at). Peranti dipercayai melangkau kod 2FA selama 30 hari. Read-only." },
      { name: "revoke_trusted_device", desc: "Batalkan satu peranti dipercayai (device_id dari list_trusted_devices). Log masuk seterusnya pada peranti itu akan meminta kod 2FA semula — serta-merta." },
      { name: "revoke_all_trusted_devices", desc: "Batalkan SEMUA peranti dipercayai akaun anda (\"log keluar di semua tempat\"). Setiap peranti akan meminta kod 2FA semula." },
    ],
  },
  {
    // t_cde2e73a: the feedback tools (#639) are user-scoped and were documented
    // only in /docs/feedback. The reference claimed to be the complete registry,
    // so they belong here too. Kept beside Profile & security: both are USER
    // scoped, not workspace scoped.
    group: "Feedback",
    tools: [
      { name: "list_feedback", desc: "Senarai thread maklum balas anda sendiri (terbaru dahulu), boleh tapis status (open|in_progress|resolved|closed) dan berhalaman (page/limit). Skop ialah PENGGUNA yang disahkan, bukan workspace \u2014 maklum balas bukan data akaun. Super-admin melihat SEMUA thread (triage). Read-only." },
      { name: "get_feedback_thread", desc: "Baca satu thread maklum balas beserta semua mesejnya (author_kind: user|admin), terawal dahulu. Thread milik pengguna lain menjawab 'tidak dijumpai' \u2014 sama seperti id yang tidak wujud. Super-admin boleh membaca mana-mana thread. Read-only." },
      { name: "reply_feedback", desc: "Balas dalam thread maklum balas. Penulis ditentukan oleh pemanggil: super-admin membalas sebagai admin (dan thread 'open' bergerak ke in_progress), pengguna lain membalas sebagai pemilik thread. Body minimum 10 aksara. Skop pengguna \u2014 tidak boleh membalas thread orang lain." },
      { name: "set_feedback_status", desc: "Tukar status thread maklum balas (open|in_progress|resolved|closed). Keputusan triage \u2014 super-admin sahaja; diaudit. Status adalah per-thread, bukan per-mesej." },
      { name: "retry_feedback_notify", desc: "Hantar semula notifikasi emel yang GAGAL untuk satu thread maklum balas (medan notify_failed_at pada thread). Ini laluan pemulihan untuk kegagalan penghantaran: emel yang tidak sempat masuk baris gilir ditandakan pada thread supaya kelihatan, dan tool ini cuba semula melalui saluran yang sama. Keputusan: queued|sent|failed|none ('none' bermaksud tiada kegagalan direkodkan). Super-admin sahaja; diaudit." },
      { name: "submit_feedback", desc: "Failkan maklum balas atau permintaan ciri baharu daripada perbualan chat. Sama seperti menghantar di halaman /feedback: disimpan sebagai thread yang boleh anda baca dan balas, dan pasukan JomForm dimaklumkan. Body minimum 10 aksara, maksimum 5000; maksimum 10 penghantaran sehari bagi setiap pengguna. Memerlukan token PENGGUNA (OAuth/sesi) dan skop readwrite \u2014 API key ditolak kerana maklum balas ialah data peribadi pengguna." },
    ],
  },
  {
    // The magic-link / step-up rule is prose, not a tool: this tree registers no
    // tool for it, so it is a group NOTE. It shipped as a `{ name: "—" }` row,
    // which the rendered count counted as a tool.
    group: "Keselamatan sesi",
    note: "Sesi log masuk melalui pautan emel (magic link) ditandakan magic. Jika 2FA aktif, tindakan sensitif (tukar kata laluan, nyahaktif 2FA, tambah passkey) memerlukan kod 2FA baharu. Pengguna tanpa 2FA atau sesi log masuk biasa tidak terjejas.",
    tools: [],
  },
  {
    group: "Legal pages",
    tools: [
      { name: "generate_legal_page", desc: "Jana draf halaman undang-undang (privacy|terms|refund) untuk workspace melalui LLM (deepseek-v4.1-flash) dengan template yang diisi butiran perniagaan. Pulangkan draf untuk semakan \u2014 belum disimpan sehingga set_legal_page dipanggil." },
      { name: "get_legal_page", desc: "Baca satu halaman undang-undang workspace (privacy|terms|refund): mode (auto/manual) + kandungan." },
      { name: "set_legal_page", desc: "Simpan kandungan halaman undang-undang workspace (privacy|terms|refund) dengan mode auto|manual. Kandungan dipaparkan di {workspace}.jomform.com/privacy|terms|refund." },
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
      { name: "upload_media", desc: "Muat naik media/aset ke workspace anda (issue #363): kind='image' (png/jpeg/webp/gif \u2014 dioptimumkan, strip EXIF, terus boleh guna; png/jpeg/webp dapat saiz thumbnail/medium/full), kind='asset' (css/js/html/svg/woff2 \u2014 disimpan asli, dikaji sebelum dipapar) atau kind='pdf' (disimpan asli, terus boleh guna). hantar content fail sebagai base64 di 'data'. Pulangkan URL untuk diguna sebagai image_url/logo_url landing atau <link>/<script> landing page." },
      { name: "delete_media", desc: "Hantar media ke TONG SAMPAH (deleted_at diisi). Byte TIDAK dimusnahkan dan kuota TIDAK dilepaskan \u2014 objek masih di storan, jadi ia masih dikira terhadap had 1GB. URL yang sama terus berfungsi selepas restore_media. Guna purge_media untuk memusnahkan byte secara kekal." },
      { name: "list_deleted_media", desc: "Senarai media dalam TONG SAMPAH: id, key, url, mime, size, form_id, deleted_at dan object_present. object_present=false bermakna byte sudah tiada dalam storan (dipadam operator atau lifecycle rule) \u2014 restore akan DITOLAK." },
      { name: "restore_media", desc: "Pulihkan media dari tong sampah (deleted_at=NULL). URL yang SAMA terus berfungsi. DITOLAK (409) bila objek sudah tiada dalam storan \u2014 memulihkan baris itu hanya akan memberi URL yang 404." },
      { name: "purge_media", desc: "Padam media secara KEKAL dari tong sampah. AMARAN: tidak boleh dibatalkan. Inilah satu-satunya tempat byte dipadam dari storan DAN kuota 1GB dilepaskan (varians saiz + pratonton PDF turut dipadam). Hanya media dalam tong sampah \u2014 hantar ke bin dulu guna delete_media." },
    ],
  },
  {
    group: "Templates",
    tools: [
      { name: "list_templates", desc: "Senarai template borang sistem (gallery): slug, tajuk, kategori, mode (payment/response). Guna dengan use_template untuk quickstart." },
      { name: "use_template", desc: "Clone template borang menjadi draft baharu dalam akaun anda. Isi slug (dari list_templates) untuk template sistem, atau template_id (dari list_own_templates) untuk template anda sendiri. Pulangkan form_id + slug untuk dibuka dalam builder." },
      { name: "list_own_templates", desc: "Senarai template anda sendiri (issue #457): id, slug, tajuk, config, theme. Berbeza daripada list_templates (gallery sistem)." },
      { name: "create_template", desc: "Simpan borang sedia ada sebagai template boleh guna semula (issue #457): isi form_id (dari list_forms) untuk salin borang itu, atau title + config untuk template baharu. Template dimiliki akaun anda sahaja." },
      { name: "update_template", desc: "Kemaskini template anda sendiri (issue #457): tajuk, config dan/atau theme. Omit medan yang tak berubah. template_id dari list_own_templates." },
      { name: "delete_template", desc: "Padam (soft-delete) template anda sendiri (issue #457). template_id dari list_own_templates. Boleh dicipta semula bila-bila masa." },
    ],
  },
  {
    group: "Analytics",
    tools: [
      { name: "get_form_analytics", desc: "Analitik per-borang: bilangan entri (submissions), paid/confirmed/failed/pending, dan hasil (RM) — NET selepas refund. `submissions` = entri yang direkod untuk borang itu, dibaca dari jadual yang borang tulis: mode=response → jadual `submissions`; borang bayaran → `sales` (semua status). `mode` disertakan supaya label betul. Merentas semua workspace anda. Read-only." },
      { name: "get_product_analytics", desc: "Analitik per-produk: unit terjual (paid), hasil (RM). Merentas semua workspace anda. Read-only." },
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
      { name: "get_domain_tls_email", desc: "Baca emel ACME/Let's Encrypt per-domain untuk auto-TLS. Kosong = kembali ke emel akaun, kemudian ACME_EMAIL global. Read-only." },
      { name: "set_domain_tls_email", desc: "Tetapkan emel ACME/Let's Encrypt per-domain untuk auto-TLS. Kosong = kembali ke emel akaun, kemudian ACME_EMAIL global." },
      { name: "set_domain_tls_mode", desc: "Tetapkan mode TLS per-domain: auto (Let's Encrypt, lalai) atau custom (bawa sijil sendiri). Dalam mode custom, muat naik sijil dengan set_domain_custom_cert." },
      { name: "set_domain_custom_cert", desc: "Muat naik sijil TLS sendiri (bring-your-own-cert) untuk domain: cert + key dalam format PEM. Domain disajikan sijil ini." },
    ],
  },
  {
    // Issue #513: the realtime channel (#447) was documented only in the tool
    // description. It gets its own reference group so the docs-coverage guard
    // passes on the shipped behaviour rather than on an exemption.
    group: "Realtime notifications",
    tools: [
      { name: "get_realtime_status", desc: "Baca saluran notifikasi masa nyata satu workspace: URL stream SSE (GET /v1/rest/events/stream), peristiwa yang dihantar (sale.paid, sale.failed, response.submitted, subscription.charged, sale.refunded) dan togol bunyi. Read-only. Sambungkan ke stream untuk notifikasi segera pada skrin dapur/pesanan." },
      { name: "set_realtime_sound", desc: "Hidup/matikan bunyi beep notifikasi masa nyata satu workspace (true = bunyi bila jualan/respons baharu tiba; false = senyap)." },
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
      { name: "admin_list_workspaces", desc: "Senarai SEMUA workspace (storefront) platform: id, nama, slug, emel pemilik, tarikh cipta, bilangan borang + produk. Super-admin sahaja; read-only; diaudit." },
      { name: "admin_media_usage", desc: "Penggunaan storan media per workspace (bait) berbanding kuota 1GB setiap workspace. Super-admin sahaja; read-only; diaudit." },
      { name: "admin_list_sales", desc: "Senarai jualan merentas SEMUA workspace, boleh tapis (date_from/date_to, workspace_id, status). Super-admin sahaja; read-only; diaudit." },
      { name: "admin_spam_queue", desc: "Senarai semua item spam (submission + jualan) merentas semua workspace. Super-admin sahaja; read-only; diaudit." },
      { name: "admin_email_deliveries", desc: "Status penghantaran emel merentas SEMUA workspace (delivered/bounce/complaint/open/click), boleh tapis sale_ref/event_type/form_id. Super-admin sahaja; read-only; diaudit." },
      { name: "admin_dead_letter_events", desc: "Senarai peristiwa callback masuk yang di-park sebagai dead letter (issue #574): dedup_key, event_type, bilangan cubaan, sebab dan masa. Selepas #569 peristiwa yang tak boleh diproses di-park 'dead' dan berhenti dicuba — sebelum ini ia tidak kelihatan langsung. Super-admin sahaja; read-only; diaudit. Di halaman /admin senarai yang sama dipaparkan dengan butang requeue setiap baris (GET /v1/admin/dead-letters, issue #579), supaya operator nampak callback yang gagal tanpa perlu klien MCP." },
      { name: "admin_requeue_dead_letter_event", desc: "Hidupkan semula peristiwa dead letter supaya sweep replay mencubanya lagi (issue #574), selepas punca dibetulkan. Kiraan cubaan di-reset supaya ia tidak di-park semula oleh sweep yang sama. Super-admin sahaja; diaudit. Dari halaman /admin tindakan yang sama boleh dijalankan (POST /v1/admin/dead-letters/requeue, issue #579)." },
      { name: "admin_system_health", desc: "Kesihatan sistem: uptime, bilangan ralat MCP terkini (24j), kedalaman queue emel. Super-admin sahaja; read-only; diaudit." },
      { name: "admin_list_suppressions", desc: "Senarai alamat yang disekat daripada dihantar emel (issue #601): emel, sebab (hard_bounce|complaint|manual), sumber, nota, siapa tambah dan bila. Boleh cari (q) dan berhalaman (page/limit). Super-admin sahaja; read-only; diaudit. Di halaman /admin senarai yang sama dipaparkan dengan kotak carian dan pager (GET /v1/admin/suppressions, issue #601)." },
      { name: "admin_add_suppression", desc: "Tambah alamat ke senarai sekatan secara manual supaya tiada emel dihantar kepadanya (issue #601). reason: hard_bounce|complaint|manual (lalai manual), note pilihan. Idempotent. Super-admin sahaja; diaudit. Dari halaman /admin ia ditambah melalui borang manual (POST /v1/admin/suppressions, issue #601)." },
      { name: "admin_remove_suppression", desc: "Buang alamat dari senarai sekatan supaya penghantaran emel bermula semula (issue #601) \u2014 untuk kes positif palsu. Super-admin sahaja; diaudit. Dari halaman /admin ia dibuang dengan butang + pengesahan (DELETE /v1/admin/suppressions/{email}, issue #601)." },
    ],
  },
];

export default function McpReferencePage() {
  return (
    <div className="max-w-3xl">
      <h1 className="text-3xl font-bold tracking-tight text-slate-900">MCP tool reference</h1>
      <p className="mt-3 text-sm leading-relaxed text-slate-600">
        Every tool JomForm exposes to connected AI agents via the Model Context Protocol. Connect any MCP client to <code className="rounded bg-slate-100 px-1">https://mcp.jomform.com/mcp</code> and agents discover these tools automatically. This page lists the whole registry — the <code className="rounded bg-slate-100 px-1">api/internal/mcpsrv</code> package at this release — and a guard fails the build if the two drift apart in either direction.
      </p>
      <p className="mt-2 text-sm text-[var(--jf-muted)]">
        {TOOL_GROUPS.reduce((n, g) => n + g.tools.length, 0)} tools across {TOOL_GROUPS.filter((g) => g.tools.length > 0).length} categories.
      </p>

      {/* Issue #509: the OAuth discovery surface. Every MCP client is required
          to discover the authorization server through the protected-resource
          metadata, so the documents a client will fetch (and the 401 challenge
          that points at them) are documented here rather than left implicit. */}
      <section className="mt-8">
        <h2 className="text-lg font-semibold text-slate-900">OAuth discovery</h2>
        <p className="mt-2 text-sm leading-relaxed text-slate-600">
          The connector authenticates with OAuth 2.1 (authorization code + PKCE, no API keys). A client
          discovers how to authenticate from the server itself:
        </p>
        <ol className="mt-3 list-decimal space-y-2 pl-5 text-sm leading-relaxed text-slate-600">
          <li>
            An unauthenticated request to <code className="rounded bg-slate-100 px-1">https://mcp.jomform.com/mcp</code>{" "}
            is refused with <code className="rounded bg-slate-100 px-1">401</code> and a{" "}
            <code className="rounded bg-slate-100 px-1">WWW-Authenticate: Bearer resource_metadata=&quot;…&quot;</code>{" "}
            header naming the protected-resource metadata URL.
          </li>
          <li>
            The client fetches <code className="rounded bg-slate-100 px-1">/.well-known/oauth-protected-resource</code>{" "}
            (RFC 9728). It names the protected resource (<code className="rounded bg-slate-100 px-1">https://mcp.jomform.com/mcp</code>),
            the authorization server (<code className="rounded bg-slate-100 px-1">https://mcp.jomform.com</code>), the scopes{" "}
            (<code className="rounded bg-slate-100 px-1">read</code> and{" "}
            <code className="rounded bg-slate-100 px-1">readwrite</code>) and that the bearer token is presented in the{" "}
            <code className="rounded bg-slate-100 px-1">Authorization</code> header.
          </li>
          <li>
            It then fetches the authorization-server metadata at{" "}
            <code className="rounded bg-slate-100 px-1">/.well-known/oauth-authorization-server</code> (RFC 8414) for the
            authorize, token, revoke and registration endpoints.
          </li>
          <li>
            It registers itself dynamically, opens the authorize URL in the browser (you log in and grant access), and
            exchanges the code for an access token — sending the{" "}
            <code className="rounded bg-slate-100 px-1">resource</code> parameter on both the authorize and token
            requests. JomForm validates it: a value that is not this MCP endpoint is rejected and no token is issued.
          </li>
          <li>Every later call to /mcp carries the access token as a Bearer credential.</li>
        </ol>
      </section>

      {/* Every list tool returns the same object envelope. This is documented
          because a client that validates tools/list rejects the WHOLE list
          when one tool advertises a non-object output schema, so the shape is
          a compatibility contract, not an implementation detail. */}
      <section className="mt-8">
        <h2 className="text-lg font-semibold text-slate-900">Structured output</h2>
        <p className="mt-2 text-sm leading-relaxed text-slate-600">
          Tool results are returned as structured content, and every tool declares the shape of that
          result in its <code className="rounded bg-slate-100 px-1">outputSchema</code>. All tools
          declare an <em>object</em> schema, which is what makes the server usable with a client that
          validates <code className="rounded bg-slate-100 px-1">tools/list</code> strictly.
        </p>
        <p className="mt-2 text-sm leading-relaxed text-slate-600">
          Every listing tool therefore wraps its array in an object: the records are under{" "}
          <code className="rounded bg-slate-100 px-1">items</code>, and paginated listings also carry{" "}
          <code className="rounded bg-slate-100 px-1">page</code>,{" "}
          <code className="rounded bg-slate-100 px-1">limit</code> and{" "}
          <code className="rounded bg-slate-100 px-1">total</code>. So{" "}
          <code className="rounded bg-slate-100 px-1">list_products</code> returns{" "}
          <code className="rounded bg-slate-100 px-1">{`{"items": [...]}`}</code>, not a bare array.
        </p>
        <p className="mt-2 text-sm leading-relaxed text-slate-600">
          Batch tools report their summary next to the per-item outcomes:{" "}
          <code className="rounded bg-slate-100 px-1">capture_all_pending_sales</code> returns{" "}
          <code className="rounded bg-slate-100 px-1">
            {`{"requested", "captured", "failed", "results": [...]}`}
          </code>
          . Single-record tools return their fields at the top level of the object.
        </p>
      </section>

      {/* Issue #645: the scope model. Documented because the two axes are easy
          to conflate, and conflating them is exactly what let a readonly key
          write (the API-key scope was silently dropped from the write gate). */}
      <section className="mt-8">
        <h2 className="text-lg font-semibold text-slate-900">OAuth connector scopes</h2>
        <p className="mt-2 text-sm leading-relaxed text-slate-600">
          A connector authenticates with OAuth 2.1, and the grant it receives carries one of two
          scopes. The MCP client asks for the scope it wants on the authorize request (
          <code className="rounded bg-slate-100 px-1">?scope=…</code>, space-delimited); JomForm
          grants exactly what was asked for and records it on the token it issues. Both are
          advertised in the protected-resource and authorization-server metadata, alongside the
          other fields a client needs.
        </p>
        <ul className="mt-3 list-disc space-y-2 pl-5 text-sm leading-relaxed text-slate-600">
          <li>
            <code className="rounded bg-slate-100 px-1">read</code> — read-only. The agent can
            call every listing and getter; every tool that creates, updates, deletes, archives,
            cancels, refunds or generates a stored key is refused with a scope error. A read
            grant also cannot upgrade itself: <code className="rounded bg-slate-100 px-1">create_api_key</code>{" "}
            is a write, so a read-only connector can never mint a wider credential.
          </li>
          <li>
            <code className="rounded bg-slate-100 px-1">readwrite</code> — read and write. This is
            the grant for a client that does not ask for a scope: the request omitting{" "}
            <code className="rounded bg-slate-100 px-1">scope</code> defaults to readwrite, so an
            already-connected client is never silently downgraded.
          </li>
        </ul>
        <p className="mt-2 text-sm leading-relaxed text-slate-600">
          The scope is fixed for the life of the connection: refreshing an access token returns the
          same grant as the token it replaces, so a read grant never widens. A request naming a
          scope this server does not advertise is refused with{" "}
          <code className="rounded bg-slate-100 px-1">invalid_scope</code> — it is never silently
          widened to readwrite or narrowed to read. The token response echoes the issued grant, not
          the advertised list, so a client can read back what it actually holds.
        </p>
      </section>

      <section className="mt-8">
        <h2 className="text-lg font-semibold text-slate-900">API-key scopes</h2>
        <p className="mt-2 text-sm leading-relaxed text-slate-600">
          An API key carries a <em>scope</em>, which says what that credential may do at all. It is
          one of:
        </p>
        <ul className="mt-3 list-disc space-y-2 pl-5 text-sm leading-relaxed text-slate-600">
          <li>
            <code className="rounded bg-slate-100 px-1">readonly</code> — read tools only. Every
            read works; every tool that creates, updates, deletes, archives, cancels, revokes or
            generates a stored key is refused with a scope error. A readonly key cannot mint another
            key, so it cannot escalate itself to readwrite.
          </li>
          <li>
            <code className="rounded bg-slate-100 px-1">readwrite</code> — read and write. This is
            the default grant for an OAuth connector, unless it asks for{" "}
            <code className="rounded bg-slate-100 px-1">read</code>.
          </li>
          <li>
            <code className="rounded bg-slate-100 px-1">admin</code> — kept for existing keys; it
            satisfies every scope, so it behaves as readwrite.
          </li>
        </ul>
        <p className="mt-2 text-sm leading-relaxed text-slate-600">
          Scope is a separate axis from your <em>workspace role</em> (
          <code className="rounded bg-slate-100 px-1">read_only</code>,{" "}
          <code className="rounded bg-slate-100 px-1">manage</code>,{" "}
          <code className="rounded bg-slate-100 px-1">admin</code>). A write has to satisfy both: a{" "}
          <code className="rounded bg-slate-100 px-1">readwrite</code> key held by a{" "}
          <code className="rounded bg-slate-100 px-1">read_only</code> member is still refused, and
          so is a <code className="rounded bg-slate-100 px-1">readonly</code> key held by the owner.
          Reading never requires the write scope.
        </p>
        <p className="mt-2 text-sm leading-relaxed text-slate-600">
          A few tools are worth calling out because their name suggests otherwise:{" "}
          <code className="rounded bg-slate-100 px-1">get_webhook_signing_key</code> needs{" "}
          <code className="rounded bg-slate-100 px-1">readwrite</code> only when it has to{" "}
          <em>create</em> the account&apos;s keypair on first use — reading an existing key does not.
        </p>
      </section>

      {TOOL_GROUPS.map((g) => (
        <section key={g.group} className="mt-8">
          <h2 className="text-lg font-semibold text-slate-900">{g.group}</h2>
          {g.note && (
            <p className="mt-2 text-sm leading-relaxed text-slate-600">{g.note}</p>
          )}
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
