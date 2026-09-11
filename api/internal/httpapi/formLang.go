package httpapi

import (
	"context"
	"encoding/json"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// Buyer-facing system strings for the public form renderer. Only SYSTEM
// strings live here — merchant-authored block .Value text (names, phone,
// checkbox labels) is rendered verbatim and is never translated.
//
// Issue #57: the effective language is form.config.lang ?? workspace defaultLang
// ?? 'en' (maintainer decision 2026-09-01). Keys use the same 5-language set
// as the dashboard i18n dictionary (web/src/components/i18n.tsx).
type langStrings struct {
	Lang           string // resolved language code ("en","ms","zh","ta","ar")
	EmailLabel     string
	CouponLabel    string
	Total          string
	Submit         string
	ErrorAlert     string
	NoProductAlert string
	Required       string
	// Issue #153: variation selector strings.
	SelectVariation      string
	SelectVariationAlert string
	InStock              string
	// Issue #171: response-only form strings.
	SubmitResponse   string
	ThankYou         string
	ResponseReceived string
	// Issue #173: pricing/choice field strings.
	SelectOption   string
	SelectShipping string
	// Issue #249: per-product quantity selector label.
	Quantity string
	// Issue #183: optional public-form footer link labels.
	PrivacyLabel string
	TermsLabel   string
	RefundLabel  string
	// Issue #207: form restriction messages (defaults; per-form overrides win).
	PendingMessage string // before start_at
	ExpiredMessage string // after end_at
	LimitMessage   string // entry limit reached
	// Issue #485: restaurant-menu page strings (header, filter row, sections).
	MenuTable             string // "Table" label, e.g. "Table 8"
	MenuInfo              string // info (i) affordance aria/title
	MenuBack              string // back arrow aria label
	MenuBestSeller        string // best-seller section heading + filter option
	MenuBestBadge         string // badge on a best-seller item
	MenuAll               string // "All" section (products in no category)
	MenuSearchPlaceholder string // search input placeholder
	MenuFilterLabel       string // category <select> aria label
	MenuEmpty             string // "no menu yet" hint
	MenuPoweredBy         string // footer
}

// formLangs maps every supported language code to its buyer-facing strings.
// 'ms' preserves the pre-#57 hardcoded BM wording so merchants who set BM keep
// today's labels.
var formLangs = map[string]langStrings{
	"en": {
		Lang: "en", EmailLabel: "Email", CouponLabel: "Coupon",
		Total: "Total", Submit: "Proceed to Payment", ErrorAlert: "Error",
		NoProductAlert:  "Please select at least one product",
		SelectVariation: "Select an option", SelectVariationAlert: "Please choose a variation for each product", InStock: "in stock",
		SubmitResponse: "Submit", ThankYou: "Thank you!", ResponseReceived: "Your response has been received.",
		SelectOption: "Select an option", SelectShipping: "Select shipping",
		Quantity:     "Quantity",
		PrivacyLabel: "Privacy Policy", TermsLabel: "Terms of Service", RefundLabel: "Refund Policy",
		PendingMessage: "This form is not open yet. Please check back later.",
		ExpiredMessage: "This form has closed. Thank you for your interest.",
		LimitMessage:   "Sorry, this form has reached its entry limit and is no longer accepting responses.",
		MenuTable:      "Table", MenuInfo: "Cafe info", MenuBack: "Back",
		MenuBestSeller: "Best Seller", MenuBestBadge: "Best-seller", MenuAll: "All",
		MenuSearchPlaceholder: "Search menu…", MenuFilterLabel: "Filter by category",
		MenuEmpty: "No menu yet.", MenuPoweredBy: "Powered by JomForm",
	},
	"ms": {
		Lang: "ms", EmailLabel: "Emel", CouponLabel: "Kod kupon",
		Total: "Jumlah", Submit: "Teruskan ke Pembayaran", ErrorAlert: "Ralat",
		NoProductAlert:  "Pilih sekurang-kurangnya satu produk",
		SelectVariation: "Pilih variasi", SelectVariationAlert: "Sila pilih variasi untuk setiap produk", InStock: "stok",
		SubmitResponse: "Hantar", ThankYou: "Terima kasih!", ResponseReceived: "Respons anda telah diterima.",
		SelectOption: "Pilih pilihan", SelectShipping: "Pilih penghantaran",
		Quantity:     "Kuantiti",
		PrivacyLabel: "Polisi Privasi", TermsLabel: "Terma & Syarat", RefundLabel: "Polisi Bayaran Balik",
		PendingMessage: "Borang ini belum dibuka lagi. Sila kembali semula kemudian.",
		ExpiredMessage: "Borang ini telah ditutup. Terima kasih atas minat anda.",
		LimitMessage:   "Maaf, borang ini telah mencapai had penyertaan dan tidak lagi menerima respons.",
		MenuTable:      "Meja", MenuInfo: "Maklumat kafe", MenuBack: "Kembali",
		MenuBestSeller: "Paling Laris", MenuBestBadge: "Paling laris", MenuAll: "Semua",
		MenuSearchPlaceholder: "Cari menu…", MenuFilterLabel: "Tapis mengikut kategori",
		MenuEmpty: "Belum ada menu.", MenuPoweredBy: "Dikuasakan oleh JomForm",
	},
	"zh": {
		Lang: "zh", EmailLabel: "电子邮件", CouponLabel: "优惠券",
		Total: "总计", Submit: "前往付款", ErrorAlert: "错误",
		NoProductAlert:  "请至少选择一种商品",
		SelectVariation: "请选择规格", SelectVariationAlert: "请为每件商品选择规格", InStock: "有货",
		SubmitResponse: "提交", ThankYou: "谢谢！", ResponseReceived: "您的回复已收到。",
		SelectOption: "请选择", SelectShipping: "选择配送方式",
		Quantity:     "数量",
		PrivacyLabel: "隐私政策", TermsLabel: "服务条款", RefundLabel: "退款政策",
		PendingMessage: "此表单尚未开放，请稍后再来。",
		ExpiredMessage: "此表单已关闭，感谢您的关注。",
		LimitMessage:   "抱歉，此表单已达到提交上限，不再接受提交。",
		MenuTable:      "桌号", MenuInfo: "咖啡馆信息", MenuBack: "返回",
		MenuBestSeller: "最畅销", MenuBestBadge: "畅销", MenuAll: "全部",
		MenuSearchPlaceholder: "搜索菜单…", MenuFilterLabel: "按分类筛选",
		MenuEmpty: "暂无菜单。", MenuPoweredBy: "由 JomForm 提供支持",
	},
	"ta": {
		Lang: "ta", EmailLabel: "மின்னஞ்சல்", CouponLabel: "கூப்பன்",
		Total: "மொத்தம்", Submit: "கட்டணத்திற்குச் செல்லவும்", ErrorAlert: "பிழை",
		NoProductAlert:  "குறைந்தது ஒரு பொருளையாவது தேர்ந்தெடுக்கவும்",
		SelectVariation: "மாறுபாட்டைத் தேர்ந்தெடுக்கவும்", SelectVariationAlert: "ஒவ்வொரு பொருளுக்கும் மாறுபாட்டைத் தேர்ந்தெடுக்கவும்", InStock: "கையிருப்பு",
		SubmitResponse: "சமர்ப்பி", ThankYou: "நன்றி!", ResponseReceived: "உங்கள் பதில் பெறப்பட்டது.",
		SelectOption: "ஒரு விருப்பத்தைத் தேர்ந்தெடுக்கவும்", SelectShipping: "ஷிப்பிங்கைத் தேர்ந்தெடுக்கவும்",
		Quantity:     "அளவு",
		PrivacyLabel: "தனியுரிமைக் கொள்கை", TermsLabel: "சேவை விதிமுறைகள்", RefundLabel: "பணத்திருப்புக் கொள்கை",
		PendingMessage: "இந்த படிவம் இன்னும் திறக்கப்படவில்லை. பின்னர் மீண்டும் வரவும்.",
		ExpiredMessage: "இந்த படிவம் மூடப்பட்டது. உங்கள் ஆர்வத்திற்கு நன்றி.",
		LimitMessage:   "மன்னிக்கவும், இந்த படிவம் அதன் பதிவு வரம்பை அடைந்துவிட்டது, இனி பதில்களை ஏற்காது.",
		MenuTable:      "மேசை", MenuInfo: "கஃபே தகவல்", MenuBack: "பின்",
		MenuBestSeller: "அதிகம் விற்பனையாகும்", MenuBestBadge: "சிறந்த விற்பனை", MenuAll: "அனைத்தும்",
		MenuSearchPlaceholder: "மெனுவைத் தேடு…", MenuFilterLabel: "வகைப்படி வடிகட்டு",
		MenuEmpty: "இன்னும் மெனு இல்லை.", MenuPoweredBy: "JomForm வழங்குகிறது",
	},
	"ar": {
		Lang: "ar", EmailLabel: "البريد الإلكتروني", CouponLabel: "قسيمة",
		Total: "المجموع", Submit: "الانتقال إلى الدفع", ErrorAlert: "خطأ",
		NoProductAlert:  "يرجى اختيار منتج واحد على الأقل",
		SelectVariation: "اختر خيارًا", SelectVariationAlert: "يرجى اختيار خيار لكل منتج", InStock: "متوفر",
		SubmitResponse: "إرسال", ThankYou: "شكراً لك!", ResponseReceived: "تم استلام ردك.",
		SelectOption: "اختر خيارًا", SelectShipping: "اختر الشحن",
		Quantity:     "الكمية",
		PrivacyLabel: "سياسة الخصوصية", TermsLabel: "شروط الخدمة", RefundLabel: "سياسة الاسترداد",
		PendingMessage: "هذا النموذج غير مفتوح بعد. يرجى العودة لاحقاً.",
		ExpiredMessage: "تم إغلاق هذا النموذج. شكراً لاهتمامك.",
		LimitMessage:   "عذراً، بلغ هذا النموذج الحد الأقصى للردود ولم يعد يقبل الردود.",
		MenuTable:      "طاولة", MenuInfo: "معلومات المقهى", MenuBack: "رجوع",
		MenuBestSeller: "الأكثر مبيعاً", MenuBestBadge: "الأكثر مبيعاً", MenuAll: "الكل",
		MenuSearchPlaceholder: "ابحث في القائمة…", MenuFilterLabel: "تصفية حسب الفئة",
		MenuEmpty: "لا توجد قائمة بعد.", MenuPoweredBy: "مدعوم بواسطة JomForm",
	},
}

// resolveWorkspaceLang returns the workspace's default language for a form's account,
// or "en" when the workspace/account is unresolvable.
func (h *PublicHandlers) resolveWorkspaceLang(ctx context.Context, f *dbstore.Form) string {
	if h.Workspaces == nil {
		return "en"
	}
	workspaceID, err := h.Workspaces.WorkspaceIDByAccount(ctx, f.AccountID)
	if err != nil || workspaceID == "" {
		return "en"
	}
	st, err := h.Workspaces.Settings(ctx, workspaceID)
	if err != nil {
		return "en"
	}
	return st.WorkspaceLang()
}

// effectiveLangStrings resolves the buyer-facing dictionary for a form.
// Resolution chain: form.config.lang ?? workspace defaultLang ?? 'en'.
func (h *PublicHandlers) effectiveLangStrings(ctx context.Context, f *dbstore.Form) langStrings {
	// 1. Per-form override wins when present and valid.
	if raw, err := json.Marshal(f.Config); err == nil {
		var cfg struct {
			Lang string `json:"lang"`
		}
		if json.Unmarshal(raw, &cfg) == nil && validFormLang(cfg.Lang) {
			return formLangs[cfg.Lang]
		}
	}
	// 2. Otherwise inherit the workspace default.
	lang := h.resolveWorkspaceLang(ctx, f)
	if st, ok := formLangs[lang]; ok {
		return st
	}
	// 3. Fallback.
	return formLangs["en"]
}

// validFormLang reports whether code is one of the supported languages.
func validFormLang(code string) bool {
	switch code {
	case "en", "ms", "zh", "ta", "ar":
		return true
	}
	return false
}
