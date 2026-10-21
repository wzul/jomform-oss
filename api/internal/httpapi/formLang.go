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
	Total          string
	Submit         string
	ErrorAlert     string
	NoProductAlert string
	Required       string
	// Language placement: the legacy product-checkbox renderer and the
	// thank-you / private-gate pages were hardcoded Malay; these are the chrome
	// strings they need so a workspace set to English really serves English.
	NameLabel       string // "Full name"
	PhoneLabel      string // "Phone number"
	PoweredBy       string // "Powered by JomForm"
	OrderLabel      string // "Order"
	StatusLabel     string // "Status"
	UnlockTitle     string // private gate heading text
	UnlockHelp      string // private gate explanation
	UnlockAction    string // private gate button
	UnlockError     string // private gate fallback error
	StatusPending   string
	StatusPaid      string
	StatusConfirmed string
	StatusFailed    string
	StatusCancelled string
	// Issue #153: variation selector strings.
	SelectVariation      string
	SelectVariationAlert string
	InStock              string
	// Issue #171: response-only form strings.
	SubmitResponse   string
	ThankYou         string
	ResponseReceived string
	// Card t_0da81d94: the built-in body of a confirmation / thank-you page is
	// a DICTIONARY lookup, not prose the server owns. PaymentFailedBody is the
	// failed-payment arm (issue #393's per-form failure_message still wins when
	// the merchant set one); ResponseReceived above is both.
	PaymentFailedBody string
	// Card t_e2024698: the payment could not even be STARTED — the CHIP
	// purchase creation failed before the buyer ever reached a checkout page
	// (rejected merchant key, provider outage). That is a different situation
	// from PaymentFailedBody (a payment that was attempted and declined), so it
	// carries different copy: it says nothing was charged and tells the buyer to
	// contact the merchant with the reference. The four public order surfaces
	// render it in-app through payFailJS; before, the api answered 502 and the
	// edge substituted its own error page, so the buyer saw nothing actionable.
	PaymentUnavailable string
	// Card t_4086b8f0: a `file` block stored BEFORE the write-side refusal
	// (formfields.UnsupportedBlock) still has to be served honestly — its label
	// plus this note, instead of a picker whose file could go nowhere.
	FileUnsupported string
	// Card t_4975927e: a `coupon` block stored before the block was withdrawn
	// still has to be served honestly — its label plus this note, instead of a
	// text input whose typed code nothing on the server ever applied.
	CouponUnsupported string
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
	// A variant that sold out while the menu was already open (the page is
	// JS-driven, so the disabled attribute the option was rendered with is
	// stale by then). Flagged when a pick on a sold-out variant is refused.
	// Shown in the item's own pick hint.
	MenuSoldOut string // "sold out" — flagged when a sold-out variant is picked
	// t_aa6e3596: a basket line the buyer's own basket still held when the page
	// was opened again, whose dish has since sold out. Shown in the sheet, which
	// is opened for it, because the basket changed without the buyer touching
	// it. The dropped line's name is appended to this sentence.
	MenuStockChanged string // "no longer available, removed from your basket"
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
		Lang: "en", EmailLabel: "Email",
		Total: "Total", Submit: "Proceed to Payment", ErrorAlert: "Error",
		NoProductAlert:  "Please select at least one product",
		SelectVariation: "Select an option", SelectVariationAlert: "Please choose a variation for each product", InStock: "in stock",
		SubmitResponse: "Submit", ThankYou: "Thank you!", ResponseReceived: "Your response has been received.",
		PaymentFailedBody: "We could not complete your payment. Please try again.",
		PaymentUnavailable: "We could not start the payment, so nothing was charged. " +
			"Please contact the merchant and quote the reference below.",
		// Card t_4086b8f0: a `file` block stored before the write-side refusal
		// (formfields.UnsupportedBlock). It renders its label and this note
		// rather than a file picker, because the picked file could not go
		// anywhere — no upload path exists for an anonymous buyer.
		FileUnsupported:   "Attachments cannot be uploaded on this form.",
		CouponUnsupported: "Discount codes are not applied on this form.",
		SelectOption:      "Select an option", SelectShipping: "Select shipping",
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
		MenuSubmitting:   "Placing order…",
		MenuSubmitError:  "Could not place the order. Please try again.",
		MenuRequired:     "Please enter your name and email.",
		MenuSoldOut:      "Sold out",
		MenuStockChanged: "No longer available, removed from your basket",
		Required:         "(required)",
		MenuExtraHelp:    "Extra details requested by the merchant — these fields come from the order form attached in this form's menu settings.",
		MenuExtraEdit:    "Edit this form",
		NameLabel:        "Full name",
		PhoneLabel:       "Phone number",
		PoweredBy:        "Powered by JomForm",
		OrderLabel:       "Order",
		StatusLabel:      "Status",
		UnlockTitle:      "Password protected",
		UnlockHelp:       "This form is password protected.",
		UnlockAction:     "Unlock",
		UnlockError:      "Wrong password",
		StatusPending:    "pending confirmation",
		StatusPaid:       "payment received",
		StatusConfirmed:  "confirmed",
		StatusFailed:     "payment failed",
		StatusCancelled:  "cancelled",
	},
	"ms": {
		Lang: "ms", EmailLabel: "Emel",
		Total: "Jumlah", Submit: "Teruskan ke Pembayaran", ErrorAlert: "Ralat",
		NoProductAlert:  "Pilih sekurang-kurangnya satu produk",
		SelectVariation: "Pilih variasi", SelectVariationAlert: "Sila pilih variasi untuk setiap produk", InStock: "stok",
		SubmitResponse: "Hantar", ThankYou: "Terima kasih!", ResponseReceived: "Respons anda telah diterima.",
		// The pre-#57 hardcoded BM wording is preserved for `ms` (see the note
		// at the top of this dictionary).
		PaymentFailedBody: "Pembayaran gagal. Sila cuba lagi.",
		PaymentUnavailable: "Kami tidak dapat memulakan pembayaran, jadi tiada caj dikenakan. " +
			"Sila hubungi penjual dan nyatakan rujukan di bawah.",
		// Card t_4086b8f0 — see the `en` block above.
		FileUnsupported:   "Lampiran tidak boleh dimuat naik pada borang ini.",
		CouponUnsupported: "Kod diskaun tidak digunakan pada borang ini.",
		SelectOption:      "Pilih pilihan", SelectShipping: "Pilih penghantaran",
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
		MenuSubmitting:   "Menghantar pesanan…",
		MenuSubmitError:  "Pesanan tidak berjaya dihantar. Sila cuba lagi.",
		MenuRequired:     "Sila masukkan nama dan emel anda.",
		MenuSoldOut:      "Habis stok",
		MenuStockChanged: "Tidak lagi tersedia, dibuang dari bakul anda",
		Required:         "wajib",
		MenuExtraHelp:    "Butiran tambahan yang diminta peniaga — medan ini datang dari borang pesanan yang dilampirkan dalam tetapan menu borang ini.",
		MenuExtraEdit:    "Edit borang ini",
		NameLabel:        "Nama penuh",
		PhoneLabel:       "No. telefon",
		PoweredBy:        "Dikuasakan oleh JomForm",
		OrderLabel:       "Tempahan",
		StatusLabel:      "Status",
		UnlockTitle:      "Dilindungi kata laluan",
		UnlockHelp:       "Borang ini dilindungi kata laluan.",
		UnlockAction:     "Buka kunci",
		UnlockError:      "Kata laluan salah",
		StatusPending:    "menunggu pengesahan",
		StatusPaid:       "pembayaran diterima",
		StatusConfirmed:  "disahkan",
		StatusFailed:     "pembayaran gagal",
		StatusCancelled:  "dibatalkan",
	},
	"zh": {
		Lang: "zh", EmailLabel: "电子邮件",
		Total: "总计", Submit: "前往付款", ErrorAlert: "错误",
		NoProductAlert:  "请至少选择一种商品",
		SelectVariation: "请选择规格", SelectVariationAlert: "请为每件商品选择规格", InStock: "有货",
		SubmitResponse: "提交", ThankYou: "谢谢！", ResponseReceived: "您的回复已收到。",
		PaymentFailedBody: "无法完成付款，请重试。",
		PaymentUnavailable: "无法开始付款，因此未收取任何费用。" +
			"请联系商家并提供下方的参考编号。",
		FileUnsupported:   "此表单不支持上传附件。",
		CouponUnsupported: "此表单不适用折扣码。",
		SelectOption:      "请选择", SelectShipping: "选择配送方式",
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
		MenuSubmitting:   "正在提交订单…",
		MenuSubmitError:  "无法提交订单，请重试。",
		MenuRequired:     "请输入您的姓名和电子邮件。",
		MenuSoldOut:      "已售罄",
		MenuStockChanged: "已不再供售，已从您的购物篮中移除",
		Required:         "必填",
		MenuExtraHelp:    "商家要求的额外信息 — 这些字段来自此表单菜单设置中附加的订单表单。",
		MenuExtraEdit:    "编辑此表单",
		NameLabel:        "全名",
		PhoneLabel:       "电话号码",
		PoweredBy:        "由 JomForm 提供支持",
		OrderLabel:       "订单",
		StatusLabel:      "状态",
		UnlockTitle:      "受密码保护",
		UnlockHelp:       "此表单受密码保护。",
		UnlockAction:     "解锁",
		UnlockError:      "密码错误",
		StatusPending:    "等待确认",
		StatusPaid:       "已收到付款",
		StatusConfirmed:  "已确认",
		StatusFailed:     "付款失败",
		StatusCancelled:  "已取消",
	},
	"ta": {
		Lang: "ta", EmailLabel: "மின்னஞ்சல்",
		Total: "மொத்தம்", Submit: "கட்டணத்திற்குச் செல்லவும்", ErrorAlert: "பிழை",
		NoProductAlert:  "குறைந்தது ஒரு பொருளையாவது தேர்ந்தெடுக்கவும்",
		SelectVariation: "மாறுபாட்டைத் தேர்ந்தெடுக்கவும்", SelectVariationAlert: "ஒவ்வொரு பொருளுக்கும் மாறுபாட்டைத் தேர்ந்தெடுக்கவும்", InStock: "கையிருப்பு",
		SubmitResponse: "சமர்ப்பி", ThankYou: "நன்றி!", ResponseReceived: "உங்கள் பதில் பெறப்பட்டது.",
		PaymentFailedBody: "கட்டணத்தை முடிக்க முடியவில்லை. மீண்டும் முயற்சிக்கவும்.",
		PaymentUnavailable: "கட்டணத்தைத் தொடங்க முடியவில்லை, எனவே எந்தக் கட்டணமும் வசூலிக்கப்படவில்லை. " +
			"கீழே உள்ள குறிப்பு எண்ணுடன் வணிகரைத் தொடர்பு கொள்ளுங்கள்.",
		FileUnsupported:   "இந்தப் படிவத்தில் இணைப்புகளை பதிவேற்ற முடியாது.",
		CouponUnsupported: "இந்தப் படிவத்தில் தள்ளுபடிக் குறியீடுகள் பயன்படுத்தப்படாது.",
		SelectOption:      "ஒரு விருப்பத்தைத் தேர்ந்தெடுக்கவும்", SelectShipping: "ஷிப்பிங்கைத் தேர்ந்தெடுக்கவும்",
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
		MenuSubmitting:   "ஆர்டர் அனுப்பப்படுகிறது…",
		MenuSubmitError:  "ஆர்டரை அனுப்ப முடியவில்லை. மீண்டும் முயற்சிக்கவும்.",
		MenuRequired:     "உங்கள் பெயர் மற்றும் மின்னஞ்சலை உள்ளிடவும்.",
		MenuSoldOut:      "விற்றுத் தீர்ந்தது",
		MenuStockChanged: "இனி கிடைக்காது, உங்கள் கூடையிலிருந்து நீக்கப்பட்டது",
		Required:         "கட்டாயம்",
		MenuExtraHelp:    "வணிகர் கேட்கும் கூடுதல் விவரங்கள் — இந்தப் புலங்கள் இந்தப் படிவத்தின் மெனு அமைப்புகளில் இணைக்கப்பட்ட ஆர்டர் படிவத்திலிருந்து வருகின்றன.",
		MenuExtraEdit:    "இந்தப் படிவத்தைத் திருத்து",
		NameLabel:        "முழுப் பெயர்",
		PhoneLabel:       "தொலைபேசி எண்",
		PoweredBy:        "JomForm வழங்குகிறது",
		OrderLabel:       "ஆர்டர்",
		StatusLabel:      "நிலை",
		UnlockTitle:      "கடவுச்சொல் பாதுகாப்பு",
		UnlockHelp:       "இந்தப் படிவம் கடவுச்சொல்லால் பாதுகாக்கப்பட்டுள்ளது.",
		UnlockAction:     "திற",
		UnlockError:      "கடவுச்சொல் தவறு",
		StatusPending:    "உறுதிப்படுத்தல் நிலுவையில்",
		StatusPaid:       "பணம் பெறப்பட்டது",
		StatusConfirmed:  "உறுதிப்படுத்தப்பட்டது",
		StatusFailed:     "பணம் செலுத்துவது தோல்வி",
		StatusCancelled:  "ரத்துசெய்யப்பட்டது",
	},
	"ar": {
		Lang: "ar", EmailLabel: "البريد الإلكتروني",
		Total: "المجموع", Submit: "الانتقال إلى الدفع", ErrorAlert: "خطأ",
		NoProductAlert:  "يرجى اختيار منتج واحد على الأقل",
		SelectVariation: "اختر خيارًا", SelectVariationAlert: "يرجى اختيار خيار لكل منتج", InStock: "متوفر",
		SubmitResponse: "إرسال", ThankYou: "شكراً لك!", ResponseReceived: "تم استلام ردك.",
		PaymentFailedBody: "تعذّر إتمام الدفع. يرجى المحاولة مرة أخرى.",
		PaymentUnavailable: "تعذّر بدء الدفع، ولم يتم خصم أي مبلغ. " +
			"يرجى التواصل مع التاجر وذكر الرقم المرجعي أدناه.",
		FileUnsupported:   "لا يمكن تحميل المرفقات في هذا النموذج.",
		CouponUnsupported: "لا تُطبَّق رموز الخصم في هذا النموذج.",
		SelectOption:      "اختر خيارًا", SelectShipping: "اختر الشحن",
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
		MenuSubmitting:   "جارٍ إرسال الطلب…",
		MenuSubmitError:  "تعذّر إرسال الطلب. يرجى المحاولة مرة أخرى.",
		MenuRequired:     "يرجى إدخال اسمك وبريدك الإلكتروني.",
		MenuSoldOut:      "نفدت الكمية",
		MenuStockChanged: "لم يعد متوفراً، تمت إزالته من سلتك",
		Required:         "مطلوب",
		MenuExtraHelp:    "تفاصيل إضافية يطلبها التاجر — تأتي هذه الحقول من نموذج الطلب المرفق في إعدادات قائمة هذا النموذج.",
		MenuExtraEdit:    "تعديل هذا النموذج",
		NameLabel:        "الاسم الكامل",
		PhoneLabel:       "رقم الهاتف",
		PoweredBy:        "مدعوم بواسطة JomForm",
		OrderLabel:       "الطلب",
		StatusLabel:      "الحالة",
		UnlockTitle:      "محمي بكلمة مرور",
		UnlockHelp:       "هذا النموذج محمي بكلمة مرور.",
		UnlockAction:     "فتح",
		UnlockError:      "كلمة المرور خاطئة",
		StatusPending:    "بانتظار التأكيد",
		StatusPaid:       "تم استلام الدفع",
		StatusConfirmed:  "مؤكد",
		StatusFailed:     "فشل الدفع",
		StatusCancelled:  "ملغى",
	},
}

// resolveWorkspaceLang returns the workspace's default language for a form's account,
// or "en" when the workspace/account is unresolvable.
func (h *PublicHandlers) resolveWorkspaceLang(ctx context.Context, f *dbstore.Form) string {
	if f == nil {
		return "en"
	}
	return h.resolveWorkspaceLangByAccount(ctx, f.AccountID)
}

// resolveWorkspaceLangByAccount is the account-scoped form of
// resolveWorkspaceLang, for buyer-facing pages that are reached with a sale
// (and therefore an account) but not necessarily a resolvable form — the
// thank-you page is the case that needs it (language placement).
func (h *PublicHandlers) resolveWorkspaceLangByAccount(ctx context.Context, accountID string) string {
	if h.Workspaces == nil || accountID == "" {
		return "en"
	}
	workspaceID, err := h.Workspaces.WorkspaceIDByAccount(ctx, accountID)
	if err != nil || workspaceID == "" {
		return "en"
	}
	st, err := h.Workspaces.Settings(ctx, workspaceID)
	if err != nil {
		return "en"
	}
	return st.WorkspaceLang()
}

// effectiveLangStringsByAccount resolves the buyer-facing dictionary for an
// account when no form is available (thank-you page), honouring the workspace
// default. There is no per-form override on this path: the form is the only
// carrier of config.lang, and when it cannot be resolved the workspace default
// (then "en") is the correct answer.
func (h *PublicHandlers) effectiveLangStringsByAccount(ctx context.Context, accountID string) langStrings {
	if st, ok := formLangs[h.resolveWorkspaceLangByAccount(ctx, accountID)]; ok {
		return st
	}
	return formLangs["en"]
}

// effectiveLangStrings resolves the buyer-facing dictionary for a form.
// Resolution chain: form.config.lang ?? workspace defaultLang ?? 'en'.
// A nil form is safe and resolves to the workspace default chain — the
// thank-you page legitimately reaches this code when the sale's form can no
// longer be resolved (deleted form), and must not panic.
func (h *PublicHandlers) effectiveLangStrings(ctx context.Context, f *dbstore.Form) langStrings {
	// 1. Per-form override wins when present and valid.
	if f != nil {
		if raw, err := json.Marshal(f.Config); err == nil {
			var cfg struct {
				Lang string `json:"lang"`
			}
			if json.Unmarshal(raw, &cfg) == nil && validFormLang(cfg.Lang) {
				return formLangs[cfg.Lang]
			}
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

// isRTL reports whether a resolved buyer-facing language lays out
// right-to-left. Arabic is the only RTL locale among the five the platform
// ships (formLangs), and the dashboard already declares
// documentElement.dir = "rtl" for it (web/src/components/i18n.tsx). A public
// page that renders Arabic copy inside an LTR document is the same defect shape
// as rendering the wrong language: the words are right and the page still reads
// wrong to a buyer.
//
// This is the ONE place the rule lives for the public renderers. The menu page
// applied it first (menu.go's `<html dir="rtl">`); the order pages — custom
// HTML, blocks, response, the legacy layout, the private gate, the restriction
// page and the thank-you page — never did, so an Arabic buyer got Arabic copy
// laid out left-to-right on every one of them.
//
// The argument is `any` on purpose: this function is called from templates
// (`{{if isRTL .Lang}}`), and a template executed with data that omits Lang
// hands over an untyped nil. A `func(string) bool` turns that missing key into
// `invalid value; expected string` — an execution error that blanks the whole
// page — where the pre-existing `lang="{{.Lang}}"` merely rendered empty. A
// language we cannot read is simply not Arabic.
func isRTL(lang any) bool {
	s, _ := lang.(string)
	return s == "ar"
}

// dirAttr is the <html> direction attribute isRTL implies, for the renderers
// that build their page with fmt.Fprintf (no template to hold the literal).
// Empty for every LTR locale, so their markup stays byte-for-byte as it was.
func dirAttr(lang string) string {
	if isRTL(lang) {
		return ` dir="rtl"`
	}
	return ""
}
