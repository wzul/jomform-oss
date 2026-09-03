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
// Issue #57: the effective language is form.config.lang ?? space defaultLang
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
}

// formLangs maps every supported language code to its buyer-facing strings.
// 'ms' preserves the pre-#57 hardcoded BM wording so merchants who set BM keep
// today's labels.
var formLangs = map[string]langStrings{
	"en": {
		Lang: "en", EmailLabel: "Email", CouponLabel: "Coupon",
		Total: "Total", Submit: "Proceed to Payment", ErrorAlert: "Error",
		NoProductAlert: "Please select at least one product",
		SelectVariation: "Select an option", SelectVariationAlert: "Please choose a variation for each product", InStock: "in stock",
	},
	"ms": {
		Lang: "ms", EmailLabel: "Emel", CouponLabel: "Kod kupon",
		Total: "Jumlah", Submit: "Teruskan ke Pembayaran", ErrorAlert: "Ralat",
		NoProductAlert: "Pilih sekurang-kurangnya satu produk",
		SelectVariation: "Pilih variasi", SelectVariationAlert: "Sila pilih variasi untuk setiap produk", InStock: "stok",
	},
	"zh": {
		Lang: "zh", EmailLabel: "电子邮件", CouponLabel: "优惠券",
		Total: "总计", Submit: "前往付款", ErrorAlert: "错误",
		NoProductAlert: "请至少选择一种商品",
		SelectVariation: "请选择规格", SelectVariationAlert: "请为每件商品选择规格", InStock: "有货",
	},
	"ta": {
		Lang: "ta", EmailLabel: "மின்னஞ்சல்", CouponLabel: "கூப்பன்",
		Total: "மொத்தம்", Submit: "கட்டணத்திற்குச் செல்லவும்", ErrorAlert: "பிழை",
		NoProductAlert: "குறைந்தது ஒரு பொருளையாவது தேர்ந்தெடுக்கவும்",
		SelectVariation: "மாறுபாட்டைத் தேர்ந்தெடுக்கவும்", SelectVariationAlert: "ஒவ்வொரு பொருளுக்கும் மாறுபாட்டைத் தேர்ந்தெடுக்கவும்", InStock: "கையிருப்பு",
	},
	"ar": {
		Lang: "ar", EmailLabel: "البريد الإلكتروني", CouponLabel: "قسيمة",
		Total: "المجموع", Submit: "الانتقال إلى الدفع", ErrorAlert: "خطأ",
		NoProductAlert: "يرجى اختيار منتج واحد على الأقل",
		SelectVariation: "اختر خيارًا", SelectVariationAlert: "يرجى اختيار خيار لكل منتج", InStock: "متوفر",
	},
}

// resolveSpaceLang returns the space's default language for a form's account,
// or "en" when the space/account is unresolvable.
func (h *PublicHandlers) resolveSpaceLang(ctx context.Context, f *dbstore.Form) string {
	if h.Spaces == nil {
		return "en"
	}
	spaceID, err := h.Spaces.SpaceIDByAccount(ctx, f.AccountID)
	if err != nil || spaceID == "" {
		return "en"
	}
	st, err := h.Spaces.Settings(ctx, spaceID)
	if err != nil {
		return "en"
	}
	return st.SpaceLang()
}

// effectiveLangStrings resolves the buyer-facing dictionary for a form.
// Resolution chain: form.config.lang ?? space defaultLang ?? 'en'.
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
	// 2. Otherwise inherit the space default.
	lang := h.resolveSpaceLang(ctx, f)
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
