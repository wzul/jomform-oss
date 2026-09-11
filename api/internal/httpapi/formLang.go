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
	// Issue #492: restaurant-menu cart + checkout strings.
	MenuAdd         string // add-to-cart affordance / aria label
	MenuBasket      string // basket sheet title
	MenuItems       string // "item" unit label on the sticky bar (e.g. "3 item")
	MenuViewBasket  string // sticky-bar call to action ("View basket")
	MenuSubtotal    string // running subtotal label
	MenuCheckout    string // checkout / pay button
	MenuClear       string // clear-cart action
	MenuBasketEmpty string // empty-basket message
	MenuName        string // buyer name field label
	MenuRemove      string // remove line item
	MenuClose       string // close the basket sheet
	MenuInc         string // quantity increment aria label
	MenuDec         string // quantity decrement aria label
	MenuSubmitting  string // button label while the order is being submitted
	MenuSubmitError string // generic submit-failure message
	MenuRequired    string // name/email mandatory validation message
	// Issue #493: merchant-configured extra checkout fields (from the attached
	// form). The field labels themselves are merchant-authored and never
	// translated; only these two system strings are.
	MenuExtraHelp string // helper line: where the extra fields come from
	MenuExtraEdit string // link label: edit the attached form
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
		MenuAdd: "Add", MenuBasket: "Your basket", MenuItems: "item",
		MenuViewBasket: "View basket", MenuSubtotal: "Subtotal",
		MenuCheckout: "Checkout", MenuClear: "Clear basket",
		MenuBasketEmpty: "Your basket is empty.", MenuName: "Name",
		MenuRemove: "Remove", MenuClose: "Close",
		MenuInc: "Increase quantity", MenuDec: "Decrease quantity",
		MenuSubmitting:  "Placing order…",
		MenuSubmitError: "Could not place the order. Please try again.",
		MenuRequired:    "Please enter your name and email.",
		Required:        "(required)",
		MenuExtraHelp:   "Extra details requested by the merchant — these fields come from the order form attached in this form's menu settings.",
		MenuExtraEdit:   "Edit this form",
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
		MenuAdd: "Tambah", MenuBasket: "Bakul anda", MenuItems: "item",
		MenuViewBasket: "Lihat bakul", MenuSubtotal: "Jumlah kecil",
		MenuCheckout: "Bayar", MenuClear: "Kosongkan bakul",
		MenuBasketEmpty: "Bakul anda kosong.", MenuName: "Nama",
		MenuRemove: "Buang", MenuClose: "Tutup",
		MenuInc: "Tambah kuantiti", MenuDec: "Kurang kuantiti",
		MenuSubmitting:  "Menghantar pesanan…",
		MenuSubmitError: "Pesanan tidak berjaya dihantar. Sila cuba lagi.",
		MenuRequired:    "Sila masukkan nama dan emel anda.",
		Required:        "wajib",
		MenuExtraHelp:   "Butiran tambahan yang diminta peniaga — medan ini datang dari borang pesanan yang dilampirkan dalam tetapan menu borang ini.",
		MenuExtraEdit:   "Edit borang ini",
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
		MenuAdd: "加入", MenuBasket: "您的购物篮", MenuItems: "件",
		MenuViewBasket: "查看购物篮", MenuSubtotal: "小计",
		MenuCheckout: "结账", MenuClear: "清空购物篮",
		MenuBasketEmpty: "您的购物篮是空的。", MenuName: "姓名",
		MenuRemove: "移除", MenuClose: "关闭",
		MenuInc: "增加数量", MenuDec: "减少数量",
		MenuSubmitting:  "正在提交订单…",
		MenuSubmitError: "无法提交订单，请重试。",
		MenuRequired:    "请输入您的姓名和电子邮件。",
		Required:        "必填",
		MenuExtraHelp:   "商家要求的额外信息 — 这些字段来自此表单菜单设置中附加的订单表单。",
		MenuExtraEdit:   "编辑此表单",
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
		MenuAdd: "சேர்", MenuBasket: "உங்கள் கூடை", MenuItems: "பொருள்",
		MenuViewBasket: "கூடையைப் பார்", MenuSubtotal: "கூட்டுத்தொகை",
		MenuCheckout: "பணம் செலுத்து", MenuClear: "கூடையை காலி செய்",
		MenuBasketEmpty: "உங்கள் கூடை காலியாக உள்ளது.", MenuName: "பெயர்",
		MenuRemove: "நீக்கு", MenuClose: "மூடு",
		MenuInc: "அளவை அதிகரி", MenuDec: "அளவை குறை",
		MenuSubmitting:  "ஆர்டர் அனுப்பப்படுகிறது…",
		MenuSubmitError: "ஆர்டரை அனுப்ப முடியவில்லை. மீண்டும் முயற்சிக்கவும்.",
		MenuRequired:    "உங்கள் பெயர் மற்றும் மின்னஞ்சலை உள்ளிடவும்.",
		Required:        "கட்டாயம்",
		MenuExtraHelp:   "வணிகர் கேட்கும் கூடுதல் விவரங்கள் — இந்தப் புலங்கள் இந்தப் படிவத்தின் மெனு அமைப்புகளில் இணைக்கப்பட்ட ஆர்டர் படிவத்திலிருந்து வருகின்றன.",
		MenuExtraEdit:   "இந்தப் படிவத்தைத் திருத்து",
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
		MenuAdd: "إضافة", MenuBasket: "سلتك", MenuItems: "عنصر",
		MenuViewBasket: "عرض السلة", MenuSubtotal: "المجموع الفرعي",
		MenuCheckout: "الدفع", MenuClear: "إفراغ السلة",
		MenuBasketEmpty: "سلتك فارغة.", MenuName: "الاسم",
		MenuRemove: "إزالة", MenuClose: "إغلاق",
		MenuInc: "زيادة الكمية", MenuDec: "تقليل الكمية",
		MenuSubmitting:  "جارٍ إرسال الطلب…",
		MenuSubmitError: "تعذّر إرسال الطلب. يرجى المحاولة مرة أخرى.",
		MenuRequired:    "يرجى إدخال اسمك وبريدك الإلكتروني.",
		Required:        "مطلوب",
		MenuExtraHelp:   "تفاصيل إضافية يطلبها التاجر — تأتي هذه الحقول من نموذج الطلب المرفق في إعدادات قائمة هذا النموذج.",
		MenuExtraEdit:   "تعديل هذا النموذج",
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
