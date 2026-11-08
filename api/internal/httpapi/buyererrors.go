// Buyer-facing refusals on the public surface: the envelope, the localization
// plumbing, and the fixed-sentence values the public handlers answer with.
//
// THE DEFECT THIS CLOSES (t_874021eb). The public buyer surface is
// language-aware — the page chrome follows `form.config.lang ??
// workspace.default_lang ?? "en"` (formLang.go) — but every refusal was a
// hardcoded Malay literal and the page's own JavaScript printed the server prose
// verbatim. Measured live: an English form answered
//
//	POST /fee-e2e-abc/order {"items":[]}
//	→ 400 {"code":"request.badRequest","error":"butiran tidak lengkap"}
//
// and the buyer read `Error: butiran tidak lengkap`. The class code said nothing
// about the refusal either, so a client could not localize from it.
//
// THE ENVELOPE. A buyer refusal now carries three facts about ONE sentence:
//
//	{"code":"order.incomplete",
//	 "error":"Incomplete details",      // English — wire prose for curl/MCP/non-JS
//	 "message":"Butiran tidak lengkap"} // the buyer's resolved language
//
// `message` is what the page prints (see the renderers' JS: `d.message||d.error`).
// The server already knows the form's language when it refuses, so it renders the
// sentence itself rather than shipping a dictionary to every page.
package httpapi

import (
	"net/http"

	"github.com/wzul/jomform/api/internal/buyertext"
)

// buyerErrorBody is the buyer refusal envelope. Kept out of the call sites so the
// prose-only scan (TestIssue6177NoHandlerSendsProseOnly) cannot read a hand-built
// map literal as a violation, and so the shape has exactly one definition.
func buyerErrorBody(code, english, localized string) map[string]string {
	return map[string]string{"code": code, "error": english, "message": localized}
}

// buyerErr answers a refusal in the buyer's resolved language: the English wire
// prose in `error`, the same sentence in `message`, and the stable per-refusal
// `code`. `lang` comes from buyerLang/buyerLangForHost; an unknown language falls
// back to English inside buyertext.
func buyerErr(w http.ResponseWriter, lang string, status int, m *buyertext.Message) {
	if m == nil {
		writeErr(w, status, "internal error")
		return
	}
	writeJSON(w, status, buyerErrorBody(m.Code, m.Error(), buyertext.Render(lang, m)))
}

// buyerErrOf answers err as a buyer refusal when it carries a code
// (formfields/shipping/payment_fee return typed messages), and falls back to the
// plain class envelope for an error that is not one — an internal fault still has
// to answer the wire contract.
func buyerErrOf(w http.ResponseWriter, lang string, status int, err error) {
	if m := buyertext.As(err); m != nil {
		buyerErr(w, lang, status, m)
		return
	}
	writeErr(w, status, err.Error())
}

// plainErr writes a localized PLAIN-TEXT error page (the 404/500 bodies that are
// not JSON: the form/order/workspace routes use http.Error's contract). Plain
// text declares no <html lang>, which is why only the words matter here.
func plainErr(w http.ResponseWriter, status int, lang, code string) {
	http.Error(w, buyertext.Text(lang, code, nil), status)
}

// The fixed-sentence refusals. Values, not literals at the call sites, so the
// English prose and the code cannot drift apart across the four order surfaces.
var (
	errOrderIncomplete        = buyertext.New(buyertext.CodeOrderIncomplete, nil)
	errOrderNameEmailRequired = buyertext.New(buyertext.CodeOrderNameEmailRequired, nil)
	errOrderEmailInvalid      = buyertext.New(buyertext.CodeOrderEmailInvalid, nil)
	errOrderRateLimited       = buyertext.New(buyertext.CodeOrderRateLimited, nil)
	errOrderPaymentMethodBad  = buyertext.New(buyertext.CodeOrderPaymentMethodBad, nil)
	errOrderNothingToPay      = buyertext.New(buyertext.CodeOrderNothingToPay, nil)
	errOrderDiscountTooLarge  = buyertext.New(buyertext.CodeOrderDiscountTooLarge, nil)
	errOrderShopNotFound      = buyertext.New(buyertext.CodeOrderShopNotFound, nil)
	errOrderFormSlugRequired  = buyertext.New(buyertext.CodeOrderFormSlugRequired, nil)
	errOrderMenuNotFound      = buyertext.New(buyertext.CodeOrderMenuNotFound, nil)
	errOrderNotFound          = buyertext.New(buyertext.CodeOrderNotFound, nil)

	errFormNotFound          = buyertext.New(buyertext.CodeFormNotFound, nil)
	errFormUnavailable       = buyertext.New(buyertext.CodeFormUnavailable, nil)
	errFormPasswordRequired  = buyertext.New(buyertext.CodeFormPasswordRequired, nil)
	errFormAccessUnavailable = buyertext.New(buyertext.CodeFormAccessUnavailable, nil)
	errFormRequestInvalid    = buyertext.New(buyertext.CodeFormRequestInvalid, nil)
	errFormWrongPassword     = buyertext.New(buyertext.CodeFormWrongPassword, nil)
	errFormUnlockFailed      = buyertext.New(buyertext.CodeFormUnlockFailed, nil)

	errMediaUnconfigured = buyertext.New(buyertext.CodeMediaUnconfigured, nil)
	errMediaInvalidKey   = buyertext.New(buyertext.CodeMediaInvalidKey, nil)
	errMediaNotFound     = buyertext.New(buyertext.CodeMediaNotFound, nil)
)
