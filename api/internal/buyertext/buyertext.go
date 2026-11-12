// Package buyertext holds the SYSTEM refusal sentences of the public buyer
// surface (order submit, response submit, private-form unlock, shipping, the
// plain-text 404/500 pages, media routes) in every language a form can be
// served in.
//
// THE BUG THIS EXISTS FOR. The public buyer surface IS language-aware — the
// page chrome follows `form.config.lang ?? workspace.default_lang ?? "en"`
// (httpapi formLang.go) — but every refusal was a hardcoded Malay literal, and
// the pages print the server prose verbatim:
//
//	POST /{slug}/order {"items":[]}
//	→ 400 {"code":"request.badRequest","error":"butiran tidak lengkap"}
//
// An English buyer therefore read `Error: butiran tidak lengkap`, and a buyer
// on a `config.lang=zh` form read Malay too. The chrome spoke their language and
// the refusal did not.
//
// THREE FACTS, ONE SENTENCE. Each refusal is identified by a stable CODE and
// carries two renderings of the same sentence:
//
//   - `error`   — English. It is the wire prose for API-key / curl / MCP /
//     non-JS clients, which cannot look up a code. It is also the only place a
//     developer-facing reader sees the refusal, so it must not be Malay.
//   - `message` — the sentence in the buyer's resolved language, rendered by the
//     server (it already knows the form's language) and printed by the page's
//     own JavaScript in preference to `error`.
//   - `code`    — a stable per-refusal identifier, so a client can branch
//     without parsing prose. Before this, every one of these answered the HTTP
//     class code `request.badRequest`, which says nothing about the refusal.
//
// Codes are a public contract (they ship in JSON): renaming one is a breaking
// change.
//
// WHAT IS NOT HERE. Merchant-authored text is never translated: block labels,
// option names, confirmation messages, restriction overrides, product names.
// A `{label}` param is interpolated VERBATIM because it is the merchant's own
// words (and may be in any language the merchant typed).
//
// THIS PACKAGE IS LEAF-LIKE ON PURPOSE: formfields and shipping import it so
// their validation/quote refusals carry the same codes and sentences as the
// httpapi's own. It must never import httpapi, dbstore or any store.
package buyertext

import (
	"errors"
	"sort"
	"strings"
)

// Refusal codes. Domain-prefixed and stable; see the package comment.
const (
	// Order submit (the shared order core, form and menu checkout alike).
	CodeOrderIncomplete        = "order.incomplete"
	CodeOrderNameEmailRequired = "order.nameEmailRequired"
	CodeOrderEmailInvalid      = "order.emailInvalid"
	CodeOrderRateLimited       = "order.rateLimited"
	CodeOrderPaymentMethodBad  = "order.paymentMethodInvalid"
	CodeOrderProductInvalid    = "order.productInvalid"
	CodeOrderChoiceInvalid     = "order.choiceInvalid"
	CodeOrderVariationRequired = "order.variationRequired"
	CodeOrderVariationInvalid  = "order.variationInvalid"
	CodeOrderOutOfStockVariant = "order.outOfStockVariant"
	CodeOrderOutOfStock        = "order.outOfStock"
	CodeOrderNothingToPay      = "order.nothingToPay"
	CodeOrderDiscountTooLarge  = "order.discountTooLarge"
	CodeOrderTotalTooLarge     = "order.totalTooLarge"
	CodeOrderTotalChanged      = "order.totalChanged"
	// Buyer-chosen amount (pay-what-you-want / custom_amount products). The
	// client sends `amount_sen`; these are the four ways the server refuses it.
	CodeOrderAmountRequired    = "order.amountRequired"
	CodeOrderAmountInvalid     = "order.amountInvalid"
	CodeOrderAmountTooLow      = "order.amountTooLow"
	CodeOrderAmountTooHigh     = "order.amountTooHigh"
	CodeOrderFeeMethodRequired = "order.feeMethodRequired"
	CodeOrderFeeMethodInvalid  = "order.feeMethodInvalid"
	CodeOrderFeeInactive       = "order.feeInactive"
	// Card t_453489b9: the buyer picked a CHIP payment method the checkout
	// cannot honour (not enabled for this workspace/form, or the brand cannot
	// take it). Refused rather than silently paying with another method.
	CodeOrderPayMethodUnavailable = "order.payMethodUnavailable"
	CodeOrderShopNotFound         = "order.shopNotFound"
	CodeOrderFormSlugRequired     = "order.formSlugRequired"
	CodeOrderMenuNotFound         = "order.menuNotFound"
	CodeOrderNotFound             = "order.notFound"
	// Merchant-declared checkout fields (formfields.ValidateFields). The label
	// is the merchant's text and is interpolated verbatim.
	CodeFieldRequired      = "field.required"
	CodeFieldEmailInvalid  = "field.emailInvalid"
	CodeFieldURLInvalid    = "field.urlInvalid"
	CodeFieldNumberInvalid = "field.numberInvalid"
	CodeFieldChoiceInvalid = "field.choiceInvalid"
	CodeFieldValueInvalid  = "field.valueInvalid"
	// Managed shipping rate sources (internal/shipping and the httpapi
	// resolvers that turn their errors into buyer refusals).
	CodeShipNoDelivery            = "shipping.noDelivery"
	CodeShipNoZoneNoDefault       = "shipping.noZoneNoDefault"
	CodeShipZoneInvalid           = "shipping.zoneInvalid"
	CodeShipZoneMismatch          = "shipping.zoneMismatch"
	CodeShipZoneNoRate            = "shipping.zoneNoRate"
	CodeShipNoRateForWeight       = "shipping.noRateForWeight"
	CodeShipSourceUnsupported     = "shipping.sourceUnsupported"
	CodeShipProviderUnknown       = "shipping.providerUnknown"
	CodeShipProviderNotConnected  = "shipping.providerNotConnected"
	CodeShipProviderNoCoverage    = "shipping.providerNoCoverage"
	CodeShipProviderFailed        = "shipping.providerFailed"
	CodeShipQuoteFailedNoFallback = "shipping.quoteFailedNoFallback"
	CodeShipAddressIncomplete     = "shipping.addressIncomplete"
	CodeShipFormNotFound          = "shipping.formNotFound"
	CodeShipZonesNotConfigured    = "shipping.zonesNotConfigured"
	CodeShipBlockNotLinked        = "shipping.blockNotLinked"
	CodeShipZoneSetNotFound       = "shipping.zoneSetNotFound"
	// Form access and page-level refusals.
	CodeFormNotFound          = "form.notFound"
	CodeFormUnavailable       = "form.unavailable"
	CodeFormPasswordRequired  = "form.passwordRequired"
	CodeFormAccessUnavailable = "form.accessUnavailable"
	CodeFormRequestInvalid    = "form.requestInvalid"
	CodeFormWrongPassword     = "form.wrongPassword"
	CodeFormUnlockFailed      = "form.unlockFailed"
	CodePageError             = "page.error"
	// Workspace storefront root.
	CodeShopTitle   = "shop.title"
	CodeShopNoForms = "shop.noForms"
	// Buyer media / code-asset routes.
	CodeMediaUnconfigured = "media.unconfigured"
	CodeMediaInvalidKey   = "media.invalidKey"
	CodeMediaNotFound     = "media.notFound"
)

// langs is the language set every sentence must carry. It is the same five the
// page dictionaries use (httpapi formLangs, web i18nDict) — a form can be
// served in any of them, so a refusal in fewer is a refusal some buyer cannot
// read.
var langs = []string{"en", "ms", "zh", "ta", "ar"}

// catalog maps a refusal code to its sentence in each supported language.
// Templates use `{name}` placeholders, replaced by the caller's params.
var catalog = map[string]map[string]string{
	CodeOrderIncomplete: {
		"en": "Incomplete details",
		"ms": "Butiran tidak lengkap",
		"zh": "信息不完整",
		"ta": "விவரங்கள் முழுமையடையவில்லை",
		"ar": "التفاصيل غير مكتملة",
	},
	CodeOrderNameEmailRequired: {
		"en": "Name and email are required",
		"ms": "Nama dan emel wajib diisi",
		"zh": "姓名和电子邮件为必填",
		"ta": "பெயரும் மின்னஞ்சலும் அவசியம்",
		"ar": "الاسم والبريد الإلكتروني مطلوبان",
	},
	CodeOrderEmailInvalid: {
		"en": "Invalid email address",
		"ms": "Alamat emel tidak sah",
		"zh": "电子邮件地址无效",
		"ta": "மின்னஞ்சல் முகவரி தவறானது",
		"ar": "عنوان البريد الإلكتروني غير صالح",
	},
	CodeOrderRateLimited: {
		"en": "Too many attempts. Please try again later.",
		"ms": "Terlalu banyak cubaan. Cuba lagi kemudian.",
		"zh": "尝试次数过多，请稍后再试。",
		"ta": "அதிக முயற்சிகள். பின்னர் மீண்டும் முயற்சிக்கவும்.",
		"ar": "محاولات كثيرة جداً. يرجى المحاولة لاحقاً.",
	},
	CodeOrderPaymentMethodBad: {
		"en": "Invalid payment method",
		"ms": "Kaedah pembayaran tidak sah",
		"zh": "付款方式无效",
		"ta": "கட்டண முறை தவறானது",
		"ar": "طريقة الدفع غير صالحة",
	},
	CodeOrderProductInvalid: {
		"en": "Invalid product: {id}",
		"ms": "Produk tidak sah: {id}",
		"zh": "商品无效：{id}",
		"ta": "தவறான பொருள்: {id}",
		"ar": "منتج غير صالح: {id}",
	},
	CodeOrderChoiceInvalid: {
		"en": "Invalid choice for {name} — reload the page and pick again",
		"ms": "Pilihan tidak sah untuk {name} — muat semula halaman dan pilih sekali lagi",
		"zh": "{name} 的选项无效 — 请刷新页面后重新选择",
		"ta": "{name}க்கான தேர்வு தவறானது — பக்கத்தை மீண்டும் ஏற்றி மீண்டும் தேர்ந்தெடுக்கவும்",
		"ar": "اختيار غير صالح لـ {name} — أعد تحميل الصفحة واختر مرة أخرى",
	},
	CodeOrderVariationRequired: {
		"en": "Choose a variation for {name}",
		"ms": "Pilih variasi untuk {name}",
		"zh": "请为 {name} 选择规格",
		"ta": "{name}க்கான மாறுபாட்டைத் தேர்ந்தெடுக்கவும்",
		"ar": "اختر خيارًا لـ {name}",
	},
	CodeOrderVariationInvalid: {
		"en": "Invalid variation: {id}",
		"ms": "Variasi tidak sah: {id}",
		"zh": "规格无效：{id}",
		"ta": "தவறான மாறுபாடு: {id}",
		"ar": "خيار غير صالح: {id}",
	},
	CodeOrderOutOfStockVariant: {
		"en": "{name} ({variant}) is out of stock",
		"ms": "{name} ({variant}) kehabisan stok",
		"zh": "{name}（{variant}）已售罄",
		"ta": "{name} ({variant}) கையிருப்பில் இல்லை",
		"ar": "{name} ({variant}) نفد من المخزون",
	},
	CodeOrderOutOfStock: {
		"en": "{name} is out of stock",
		"ms": "{name} kehabisan stok",
		"zh": "{name} 已售罄",
		"ta": "{name} கையிருப்பில் இல்லை",
		"ar": "{name} نفد من المخزون",
	},
	CodeOrderNothingToPay: {
		"en": "There are no products to pay for",
		"ms": "Tiada produk untuk dibayar",
		"zh": "没有可付款的商品",
		"ta": "பணம் செலுத்த பொருட்கள் இல்லை",
		"ar": "لا توجد منتجات للدفع",
	},
	CodeOrderDiscountTooLarge: {
		"en": "The discount is larger than the order total",
		"ms": "Diskaun melebihi jumlah pesanan",
		"zh": "折扣大于订单总额",
		"ta": "தள்ளுபடி ஆர்டர் மொத்தத்தை விட அதிகம்",
		"ar": "الخصم أكبر من إجمالي الطلب",
	},
	CodeOrderTotalTooLarge: {
		"en": "The order total is too large",
		"ms": "Jumlah pesanan terlalu besar",
		"zh": "订单总额过大",
		"ta": "ஆர்டர் மொத்தம் மிக அதிகம்",
		"ar": "إجمالي الطلب كبير جداً",
	},
	CodeOrderTotalChanged: {
		"en": "The total has changed. Please reload this page.",
		"ms": "Jumlah pembayaran telah berubah. Muat semula halaman ini.",
		"zh": "付款总额已变化，请重新载入此页面。",
		"ta": "கட்டண மொத்தம் மாறிவிட்டது. இந்தப் பக்கத்தை மீண்டும் ஏற்றவும்.",
		"ar": "تغيّر المبلغ الإجمالي. يرجى إعادة تحميل الصفحة.",
	},
	CodeOrderAmountRequired: {
		"en": "Please enter an amount",
		"ms": "Sila masukkan jumlah",
		"zh": "请输入金额",
		"ta": "தயவுசெய்து ஒரு தொகையை உள்ளிடவும்",
		"ar": "يرجى إدخال المبلغ",
	},
	CodeOrderAmountInvalid: {
		"en": "The amount must be a whole number of sen",
		"ms": "Jumlah mesti nombor bulat dalam sen",
		"zh": "金额必须是整数（以仙为单位）",
		"ta": "தொகை சেনில் முழு எண்ணாக இருக்க வேண்டும்",
		"ar": "يجب أن يكون المبلغ عددًا صحيحًا من السنت",
	},
	CodeOrderAmountTooLow: {
		"en": "The amount must be at least {min}",
		"ms": "Jumlah mesti sekurang-kurangnya {min}",
		"zh": "金额至少须为 {min}",
		"ta": "தொகை குறைந்தது {min} ஆக இருக்க வேண்டும்",
		"ar": "يجب أن يكون المبلغ {min} على الأقل",
	},
	CodeOrderAmountTooHigh: {
		"en": "The amount must not exceed {max}",
		"ms": "Jumlah tidak boleh melebihi {max}",
		"zh": "金额不得超过 {max}",
		"ta": "தொகை {max} ஐ விட அதிகமாக இருக்கக் கூடாது",
		"ar": "يجب ألا يتجاوز المبلغ {max}",
	},
	CodeOrderFeeMethodRequired: {
		"en": "Please choose a payment method",
		"ms": "Sila pilih kaedah bayaran",
		"zh": "请选择付款方式",
		"ta": "கட்டண முறையைத் தேர்ந்தெடுக்கவும்",
		"ar": "يرجى اختيار طريقة الدفع",
	},
	CodeOrderFeeMethodInvalid: {
		"en": "That payment method cannot carry a platform fee",
		"ms": "Kaedah bayaran itu tidak boleh dikenakan caj platform",
		"zh": "该付款方式无法收取平台费",
		"ta": "அந்தக் கட்டண முறைக்குத் தளக் கட்டணம் விதிக்க முடியாது",
		"ar": "لا يمكن تطبيق رسوم المنصة على طريقة الدفع هذه",
	},
	CodeOrderFeeInactive: {
		"en": "No platform fee is enabled for this form",
		"ms": "Tiada caj platform diaktifkan untuk borang ini",
		"zh": "此表单未启用平台费",
		"ta": "இந்தப் படிவத்தில் தளக் கட்டணம் இயக்கப்படவில்லை",
		"ar": "لا توجد رسوم منصة مفعّلة لهذا النموذج",
	},
	CodeOrderPayMethodUnavailable: {
		"en": "That payment method is not available. Please choose another one.",
		"ms": "Kaedah bayaran itu tidak tersedia. Sila pilih kaedah lain.",
		"zh": "该付款方式不可用，请选择其他方式。",
		"ta": "அந்தக் கட்டண முறை கிடைக்கவில்லை. வேறு ஒன்றைத் தேர்ந்தெடுக்கவும்.",
		"ar": "طريقة الدفع هذه غير متاحة. يرجى اختيار طريقة أخرى.",
	},
	CodeOrderShopNotFound: {
		"en": "Shop not found",
		"ms": "Kedai tidak dijumpai",
		"zh": "找不到商店",
		"ta": "கடை கிடைக்கவில்லை",
		"ar": "المتجر غير موجود",
	},
	CodeOrderFormSlugRequired: {
		"en": "form_slug is required",
		"ms": "form_slug diperlukan",
		"zh": "需要 form_slug",
		"ta": "form_slug தேவை",
		"ar": "form_slug مطلوب",
	},
	CodeOrderMenuNotFound: {
		"en": "Menu not found",
		"ms": "Menu tidak dijumpai",
		"zh": "找不到菜单",
		"ta": "மெனு கிடைக்கவில்லை",
		"ar": "القائمة غير موجودة",
	},
	CodeOrderNotFound: {
		"en": "Order not found",
		"ms": "Tempahan tidak dijumpai",
		"zh": "找不到订单",
		"ta": "ஆர்டர் கிடைக்கவில்லை",
		"ar": "الطلب غير موجود",
	},

	CodeFieldRequired: {
		"en": "{label} is required",
		"ms": "{label} wajib diisi",
		"zh": "请填写{label}",
		"ta": "{label} அவசியம்",
		"ar": "{label} مطلوب",
	},
	CodeFieldEmailInvalid: {
		"en": "Invalid email: {label}",
		"ms": "Emel tidak sah: {label}",
		"zh": "电子邮件无效：{label}",
		"ta": "தவறான மின்னஞ்சல்: {label}",
		"ar": "بريد إلكتروني غير صالح: {label}",
	},
	CodeFieldURLInvalid: {
		"en": "Invalid URL: {label}",
		"ms": "URL tidak sah: {label}",
		"zh": "网址无效：{label}",
		"ta": "தவறான URL: {label}",
		"ar": "URL غير صالح: {label}",
	},
	CodeFieldNumberInvalid: {
		"en": "Invalid number: {label}",
		"ms": "Nombor tidak sah: {label}",
		"zh": "数字无效：{label}",
		"ta": "தவறான எண்: {label}",
		"ar": "رقم غير صالح: {label}",
	},
	CodeFieldChoiceInvalid: {
		"en": "Invalid choice: {label}",
		"ms": "Pilihan tidak sah: {label}",
		"zh": "选项无效：{label}",
		"ta": "தவறான தேர்வு: {label}",
		"ar": "اختيار غير صالح: {label}",
	},
	CodeFieldValueInvalid: {
		"en": "Invalid value: {label}",
		"ms": "Nilai tidak sah: {label}",
		"zh": "值无效：{label}",
		"ta": "தவறான மதிப்பு: {label}",
		"ar": "قيمة غير صالحة: {label}",
	},

	CodeShipNoDelivery: {
		"en": "We do not deliver to this address",
		"ms": "Tiada penghantaran ke alamat ini",
		"zh": "我们不配送到此地址",
		"ta": "இந்த முகவரிக்கு நாங்கள் டெலிவரி செய்யவில்லை",
		"ar": "لا نوصل إلى هذا العنوان",
	},
	CodeShipNoZoneNoDefault: {
		"en": "No shipping zone covers this address, and the zone set has no default",
		"ms": "Alamat ini tiada zon penghantaran dan set zon tiada zon lalai",
		"zh": "此地址没有匹配的配送区域，且区域组没有默认区域",
		"ta": "இந்த முகவரிக்கு எந்த ஷிப்பிங் மண்டலமும் இல்லை, மண்டலத் தொகுப்பில் இயல்பு மண்டலமும் இல்லை",
		"ar": "لا توجد منطقة شحن تغطي هذا العنوان، ولا توجد منطقة افتراضية في مجموعة المناطق",
	},
	CodeShipZoneInvalid: {
		"en": "Invalid shipping zone",
		"ms": "Zon penghantaran tidak sah",
		"zh": "配送区域无效",
		"ta": "தவறான ஷிப்பிங் மண்டலம்",
		"ar": "منطقة شحن غير صالحة",
	},
	CodeShipZoneMismatch: {
		"en": "The shipping zone does not match this address",
		"ms": "Zon penghantaran tidak sepadan dengan alamat",
		"zh": "配送区域与此地址不匹配",
		"ta": "ஷிப்பிங் மண்டலம் இந்த முகவரியுடன் பொருந்தவில்லை",
		"ar": "منطقة الشحن لا تطابق هذا العنوان",
	},
	CodeShipZoneNoRate: {
		"en": "This shipping zone has no rate yet",
		"ms": "Zon penghantaran ini belum ada kadar",
		"zh": "此配送区域尚无费率",
		"ta": "இந்த ஷிப்பிங் மண்டலத்திற்கு இன்னும் கட்டணம் இல்லை",
		"ar": "لا يوجد سعر لهذه المنطقة بعد",
	},
	CodeShipNoRateForWeight: {
		"en": "No shipping rate for this weight",
		"ms": "Tiada kadar penghantaran untuk berat ini",
		"zh": "此重量没有配送费率",
		"ta": "இந்த எடைக்கு ஷிப்பிங் கட்டணம் இல்லை",
		"ar": "لا يوجد سعر شحن لهذا الوزن",
	},
	CodeShipSourceUnsupported: {
		"en": "This shipping rate source is not supported yet",
		"ms": "Sumber kadar penghantaran ini belum disokong",
		"zh": "尚不支持此配送费率来源",
		"ta": "இந்த ஷிப்பிங் கட்டண ஆதாரம் இன்னும் ஆதரிக்கப்படவில்லை",
		"ar": "مصدر سعر الشحن هذا غير مدعوم بعد",
	},
	CodeShipProviderUnknown: {
		"en": "Unknown delivery provider",
		"ms": "Pembekal penghantaran tidak dikenali",
		"zh": "未知的配送服务商",
		"ta": "தெரியாத டெலிவரி வழங்குநர்",
		"ar": "مزوّد توصيل غير معروف",
	},
	CodeShipProviderNotConnected: {
		"en": "The delivery provider is not connected",
		"ms": "Pembekal penghantaran ini belum disambung",
		"zh": "配送服务商尚未连接",
		"ta": "டெலிவரி வழங்குநர் இன்னும் இணைக்கப்படவில்லை",
		"ar": "مزوّد التوصيل غير متصل",
	},
	CodeShipProviderNoCoverage: {
		"en": "The delivery provider does not cover this address",
		"ms": "Pembekal ini tidak meliputi alamat ini",
		"zh": "该服务商不覆盖此地址",
		"ta": "இந்த வழங்குநர் இந்த முகவரியை உள்ளடக்கவில்லை",
		"ar": "هذا المزوّد لا يغطي هذا العنوان",
	},
	CodeShipProviderFailed: {
		"en": "The delivery provider could not quote this delivery",
		"ms": "Pembekal penghantaran gagal memberi sebut harga",
		"zh": "配送服务商未能报价",
		"ta": "டெலிவரி வழங்குநர் விலையை வழங்க முடியவில்லை",
		"ar": "تعذّر على مزوّد التوصيل تقديم عرض سعر",
	},
	CodeShipQuoteFailedNoFallback: {
		"en": "The delivery quote failed and there is no default zone rate",
		"ms": "Sebut harga penghantaran gagal dan tiada kadar zon lalai",
		"zh": "配送报价失败，且没有默认区域费率",
		"ta": "டெலிவரி விலைப்புள்ளி தோல்வியடைந்தது, இயல்பு மண்டலக் கட்டணமும் இல்லை",
		"ar": "فشل عرض سعر التوصيل ولا يوجد سعر منطقة افتراضي",
	},
	CodeShipAddressIncomplete: {
		"en": "The delivery address is incomplete for a quote",
		"ms": "Alamat penghantaran tidak lengkap untuk sebut harga",
		"zh": "配送地址不完整，无法报价",
		"ta": "விலைப்புள்ளிக்கு டெலிவரி முகவரி முழுமையடையவில்லை",
		"ar": "عنوان التوصيل غير مكتمل لعرض السعر",
	},
	CodeShipFormNotFound: {
		"en": "Form not found for the shipping rate",
		"ms": "Borang tidak dijumpai untuk kadar penghantaran",
		"zh": "找不到配送费率所属的表单",
		"ta": "ஷிப்பிங் கட்டணத்திற்கான படிவம் கிடைக்கவில்லை",
		"ar": "النموذج الخاص بسعر الشحن غير موجود",
	},
	CodeShipZonesNotConfigured: {
		"en": "Shipping rates by zone are not configured",
		"ms": "Kadar penghantaran mengikut zon tidak dikonfigurasi",
		"zh": "未配置按区域计算的配送费率",
		"ta": "மண்டல வாரியான ஷிப்பிங் கட்டணங்கள் அமைக்கப்படவில்லை",
		"ar": "أسعار الشحن حسب المنطقة غير مهيأة",
	},
	CodeShipBlockNotLinked: {
		"en": "This shipping block is not linked to a zone set yet",
		"ms": "Blok penghantaran ini belum dipautkan ke set zon",
		"zh": "此配送区块尚未关联区域组",
		"ta": "இந்த ஷிப்பிங் தொகுதி இன்னும் மண்டலத் தொகுப்புடன் இணைக்கப்படவில்லை",
		"ar": "كتلة الشحن هذه غير مرتبطة بمجموعة مناطق بعد",
	},
	CodeShipZoneSetNotFound: {
		"en": "Shipping zone set not found",
		"ms": "Set zon penghantaran tidak dijumpai",
		"zh": "找不到配送区域组",
		"ta": "ஷிப்பிங் மண்டலத் தொகுப்பு கிடைக்கவில்லை",
		"ar": "مجموعة مناطق الشحن غير موجودة",
	},

	CodeFormNotFound: {
		"en": "Form not found",
		"ms": "Borang tidak dijumpai",
		"zh": "找不到表单",
		"ta": "படிவம் கிடைக்கவில்லை",
		"ar": "النموذج غير موجود",
	},
	CodeFormUnavailable: {
		"en": "This form could not be displayed",
		"ms": "Borang tidak dapat dipaparkan",
		"zh": "无法显示此表单",
		"ta": "இந்தப் படிவத்தைக் காட்ட முடியவில்லை",
		"ar": "تعذّر عرض هذا النموذج",
	},
	CodeFormPasswordRequired: {
		"en": "Password required",
		"ms": "Kata laluan diperlukan",
		"zh": "需要密码",
		"ta": "கடவுச்சொல் தேவை",
		"ar": "كلمة المرور مطلوبة",
	},
	CodeFormAccessUnavailable: {
		"en": "Access unavailable",
		"ms": "Akses tidak tersedia",
		"zh": "无法访问",
		"ta": "அணுகல் கிடைக்கவில்லை",
		"ar": "الوصول غير متاح",
	},
	CodeFormRequestInvalid: {
		"en": "Invalid request",
		"ms": "Permintaan tidak sah",
		"zh": "请求无效",
		"ta": "தவறான கோரிக்கை",
		"ar": "طلب غير صالح",
	},
	CodeFormWrongPassword: {
		"en": "Wrong password",
		"ms": "Kata laluan salah",
		"zh": "密码错误",
		"ta": "கடவுச்சொல் தவறு",
		"ar": "كلمة المرور خاطئة",
	},
	CodeFormUnlockFailed: {
		"en": "Could not unlock this form",
		"ms": "Gagal membuka kunci borang ini",
		"zh": "无法解锁此表单",
		"ta": "இந்தப் படிவத்தைத் திறக்க முடியவில்லை",
		"ar": "تعذّر فتح قفل هذا النموذج",
	},
	CodePageError: {
		"en": "error",
		"ms": "ralat",
		"zh": "错误",
		"ta": "பிழை",
		"ar": "خطأ",
	},

	CodeShopTitle: {
		"en": "Shop {slug}",
		"ms": "Kedai {slug}",
		"zh": "{slug} 的商店",
		"ta": "{slug} கடை",
		"ar": "متجر {slug}",
	},
	CodeShopNoForms: {
		"en": "No forms yet.",
		"ms": "Belum ada borang.",
		"zh": "还没有表单。",
		"ta": "இன்னும் படிவங்கள் இல்லை.",
		"ar": "لا توجد نماذج بعد.",
	},

	CodeMediaUnconfigured: {
		"en": "Media storage is not configured",
		"ms": "Storan media tidak dikonfigurasi",
		"zh": "未配置媒体存储",
		"ta": "மீடியா சேமிப்பு அமைக்கப்படவில்லை",
		"ar": "تخزين الوسائط غير مهيأ",
	},
	CodeMediaInvalidKey: {
		"en": "Invalid media key",
		"ms": "Kunci media tidak sah",
		"zh": "媒体键无效",
		"ta": "தவறான மீடியா விசை",
		"ar": "مفتاح وسائط غير صالح",
	},
	CodeMediaNotFound: {
		"en": "Media not found",
		"ms": "Media tidak dijumpai",
		"zh": "找不到媒体",
		"ta": "மீடியா கிடைக்கவில்லை",
		"ar": "الوسائط غير موجودة",
	},
}

// Message is a buyer-facing refusal: a stable code, the params its sentence
// interpolates, and (through Error) the English wire prose.
//
// It implements error so formfields/shipping can return one from a validation or
// quote path and the HTTP layer can recover the code and localize it. Error()
// is deliberately English: it is the `error` field of the wire envelope.
type Message struct {
	Code   string
	Params map[string]string
}

// New builds a refusal message. params may be nil.
func New(code string, params map[string]string) *Message {
	return &Message{Code: code, Params: params}
}

// Error is the English wire prose.
func (m *Message) Error() string {
	if m == nil {
		return ""
	}
	return Text("en", m.Code, m.Params)
}

// As recovers the buyer message behind err (formfields/shipping return one),
// or nil when err is not a buyer refusal.
func As(err error) *Message {
	var m *Message
	if errors.As(err, &m) {
		return m
	}
	return nil
}

// Text renders code in lang, interpolating params. An unknown language falls
// back to English (the wire language); an unknown code renders as the code
// itself, which is a visible defect rather than silent empty text — a blank
// sentence is exactly how a missing translation used to reach a buyer.
func Text(lang, code string, params map[string]string) string {
	byLang, ok := catalog[code]
	if !ok {
		return code
	}
	s, ok := byLang[lang]
	if !ok || s == "" {
		s = byLang["en"]
	}
	for k, v := range params {
		s = strings.ReplaceAll(s, "{"+k+"}", v)
	}
	return s
}

// Render localizes m; nil renders empty (a caller with no message writes the
// generic class envelope instead).
func Render(lang string, m *Message) string {
	if m == nil {
		return ""
	}
	return Text(lang, m.Code, m.Params)
}

// Codes lists every refusal code, sorted. Used by the completeness guard.
func Codes() []string {
	out := make([]string, 0, len(catalog))
	for c := range catalog {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}
