package httpapi

// Buyer-facing refusals are language-disciplined (card t_874021eb).
//
// THE DEFECT. The public buyer surface IS language-aware — `form.config.lang ??
// workspace.default_lang ?? "en"`, formLangs in five locales — but every refusal
// was a hardcoded Malay literal and the page's own JavaScript printed the server
// prose verbatim. Measured live on prod (v1.23.2):
//
//	POST /fee-e2e-abc/order {"items":[]}
//	→ 400 {"code":"request.badRequest","error":"butiran tidak lengkap"}
//
// on a page that rendered `<html lang="en">` with English chrome. An English
// buyer read `Error: butiran tidak lengkap`, and the class code said nothing a
// client could localize from.
//
// WHAT THESE PIN. The envelope (`error` English + `message` localized + a
// specific `code`), the resolution per locale on the exact reproducer above, the
// private-gate refusal, the workspace storefront root, and the three packages
// that produce refusals off the HTTP path (formfields, shipping).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/buyertext"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/shipping"
)

// decodeBuyerErr reads the failure envelope.
func decodeBuyerErr(t *testing.T, rec *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not the JSON failure envelope: %v (body=%q)", err, rec.Body.String())
	}
	return body
}

// TestOrderRefusalSpeaksTheFormsLanguage is the card's headline, on the exact
// request QA measured: an empty cart POSTed to /{slug}/order.
//
// PRE-FIX every arm answered 400 {"code":"request.badRequest","error":"butiran
// tidak lengkap"} regardless of the form's language, and the page printed that
// prose. The English arm would read `Incomplete details` while the ms arm read
// `Butiran tidak lengkap` — the wire `error` is English in BOTH, because it is
// the prose curl/API-key/non-JS clients read.
func TestOrderRefusalSpeaksTheFormsLanguage(t *testing.T) {
	cases := []struct {
		lang string
		want string
	}{
		{"en", "Incomplete details"},
		{"ms", "Butiran tidak lengkap"},
		{"zh", "信息不完整"},
		{"ta", "விவரங்கள் முழுமையடையவில்லை"},
		{"ar", "التفاصيل غير مكتملة"},
	}
	for _, tc := range cases {
		t.Run(tc.lang, func(t *testing.T) {
			h := &PublicHandlers{} // no stores: the refusal fires before any read
			f := &dbstore.Form{AccountID: "acc", Config: map[string]any{"lang": tc.lang}}
			req := httptest.NewRequest(http.MethodPost, "/shop-form/order", strings.NewReader(`{"items":[]}`))
			rec := httptest.NewRecorder()

			h.submitOrder(rec, req, orderTarget{AccountID: "acc", Form: f})

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
			}
			body := decodeBuyerErr(t, rec)
			if got, want := body["code"], buyertext.CodeOrderIncomplete; got != want {
				t.Errorf("code = %q, want %q — a class code (request.badRequest) is what this "+
					"defect looked like; the client cannot localize from it", got, want)
			}
			if got := body["error"]; got != "Incomplete details" {
				t.Errorf("wire error = %q, want the English prose (curl/API-key/non-JS clients "+
					"read it)", got)
			}
			if got := body["message"]; got != tc.want {
				t.Errorf("message = %q, want %q — the sentence the page prints in the buyer's "+
					"language", got, tc.want)
			}
		})
	}
}

// TestOrderFieldRefusalCarriesCodeAndLocalizes pins the formfields hand-off: the
// validation error is a typed buyer message, so the route can emit its code and
// the same sentence localized — with the merchant's own label interpolated
// verbatim (a label is merchant-authored text and is never translated).
func TestOrderFieldRefusalCarriesCodeAndLocalizes(t *testing.T) {
	h := &PublicHandlers{}
	f := &dbstore.Form{
		AccountID: "acc",
		Config: map[string]any{
			"lang": "zh",
			"blocks": []any{
				map[string]any{"id": "nm", "type": "name", "value": "Nama Penuh", "required": true},
			},
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/shop-form/order",
		strings.NewReader(`{"items":[{"product_id":"p1","quantity":1}],"client":{"email":"a@b.test"}}`))
	rec := httptest.NewRecorder()

	h.submitOrder(rec, req, orderTarget{AccountID: "acc", Form: f, FieldsForm: f})

	body := decodeBuyerErr(t, rec)
	if got := body["code"]; got != buyertext.CodeFieldRequired {
		t.Errorf("code = %q, want %q", got, buyertext.CodeFieldRequired)
	}
	if got := body["error"]; got != "Nama Penuh is required" {
		t.Errorf("error = %q, want the English prose naming the merchant's label", got)
	}
	if got := body["message"]; got != "请填写Nama Penuh" {
		t.Errorf("message = %q, want the Chinese arm with the label verbatim", got)
	}
}

// TestPrivateGateRefusalFollowsTheFormLanguage covers the second surface the
// card names: a wrong password on a private form. The gate page also has to
// PREFER the server's localized `message` over the English wire prose — before
// this its JS read `d.error`, which was Malay on every locale.
func TestPrivateGateRefusalFollowsTheFormLanguage(t *testing.T) {
	h := &PublicHandlers{}
	f := &dbstore.Form{
		ID: "f1", AccountID: "acc", Title: "Borang Sulit",
		Status: dbstore.FormStatusPrivate,
		Config: map[string]any{"lang": "zh"},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/borang-sulit", nil)
	h.renderPrivateGate(rec, req, f)

	page := rec.Body.String()
	if !strings.Contains(page, `lang="zh"`) {
		t.Errorf("gate page does not declare the form's language:\n%s", page)
	}
	if !strings.Contains(page, "此表单受密码保护。") {
		t.Errorf("gate page fallback is not the Chinese UnlockHelp:\n%s", page)
	}
	if strings.Contains(page, "Kata laluan") {
		t.Errorf("gate page still carries Malay copy on a zh form:\n%s", page)
	}
	if !strings.Contains(page, jsSingleQuoted(formLangs["zh"].UnlockError)) {
		t.Errorf("gate JS fallback is not the Chinese UnlockError (%q):\n%s",
			jsSingleQuoted(formLangs["zh"].UnlockError), page)
	}
	if !strings.Contains(page, "d.message||d.error") {
		t.Errorf("gate JS still prints the wire prose in preference to the localized sentence:\n%s", page)
	}
}

// TestUnlockRefusalLocalized proves the unlock ROUTE (not just the page) answers
// the buyer's language — the page's own JS prints `message`, so a route that only
// translated `error` would still show the buyer English.
func TestUnlockRefusalLocalized(t *testing.T) {
	h := &PublicHandlers{} // AccessKey nil → the access-unavailable arm
	f := &dbstore.Form{
		ID: "f1", AccountID: "acc", Status: dbstore.FormStatusPrivate,
		Config: map[string]any{"lang": "ms"},
	}
	req := httptest.NewRequest(http.MethodPost, "/borang/borang/unlock",
		strings.NewReader(`{"password":"x"}`))
	rec := httptest.NewRecorder()

	h.unlockResolvedForm(rec, req, f)

	body := decodeBuyerErr(t, rec)
	if got := body["code"]; got != buyertext.CodeFormAccessUnavailable {
		t.Errorf("code = %q, want %q", got, buyertext.CodeFormAccessUnavailable)
	}
	if got := body["message"]; got != "Akses tidak tersedia" {
		t.Errorf("message = %q, want the Malay arm", got)
	}
	if got := body["error"]; got != "Access unavailable" {
		t.Errorf("error = %q, want the English wire prose", got)
	}
}

// TestWorkspaceStorefrontRootFollowsDefaultLang covers the fourth acceptance
// bullet: the workspace root page had no language dimension at all — a workspace
// set to English served `<h1>Kedai abc</h1>` with no `lang` attribute.
func TestWorkspaceStorefrontRootFollowsDefaultLang(t *testing.T) {
	forms := []*dbstore.Form{{Slug: "satu", Title: "Borang Satu"}}
	cases := []struct {
		lang string
		h1   string
		body string
		dir  string
	}{
		{"en", "Shop abc", "No forms yet.", ""},
		{"ms", "Kedai abc", "Belum ada borang.", ""},
		{"zh", "abc 的商店", "还没有表单。", ""},
		{"ar", "متجر abc", "لا توجد نماذج بعد.", ` dir="rtl"`},
	}
	for _, tc := range cases {
		t.Run(tc.lang, func(t *testing.T) {
			rec := httptest.NewRecorder()
			writeWorkspaceListing(rec, "abc", tc.lang, nil)
			page := rec.Body.String()
			if !strings.Contains(page, `<html lang="`+tc.lang+`"`+tc.dir) {
				t.Errorf("root page does not declare lang=%q%s:\n%s", tc.lang, tc.dir, page)
			}
			if !strings.Contains(page, "<h1 style='font-size:22px'>"+tc.h1+"</h1>") {
				t.Errorf("heading is not %q:\n%s", tc.h1, page)
			}
			if !strings.Contains(page, "<p>"+tc.body+"</p>") {
				t.Errorf("empty state is not %q:\n%s", tc.body, page)
			}
		})
	}

	// A merchant-authored form title is caller-supplied text: it must be escaped,
	// not concatenated into the page (the listing used to do exactly that).
	rec := httptest.NewRecorder()
	writeWorkspaceListing(rec, "abc", "en", forms)
	if strings.Contains(rec.Body.String(), "<script>") {
		t.Errorf("a form title was not escaped:\n%s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Borang Satu") {
		t.Errorf("the published form is missing from the listing:\n%s", rec.Body.String())
	}
}

// TestShippingRefusalCarriesCodeAndLocalizes pins the shipping package hand-off:
// its errors are typed buyer messages, so a checkout refusal answers a specific
// code and the buyer's sentence rather than Malay prose with a class code.
func TestShippingRefusalCarriesCodeAndLocalizes(t *testing.T) {
	_, err := shipping.Resolve(shipping.ZoneSet{}, shipping.Address{State: "Nowhere"},
		1000, shipping.Options{OnNoMatch: shipping.OnNoMatchReject})
	if err == nil {
		t.Fatal("an address no zone covers was accepted")
	}
	m := buyertext.As(err)
	if m == nil {
		t.Fatalf("shipping refusal is not a buyer message: %v", err)
	}
	if m.Code != buyertext.CodeShipNoDelivery {
		t.Errorf("code = %q, want %q", m.Code, buyertext.CodeShipNoDelivery)
	}
	if got := m.Error(); got != "We do not deliver to this address" {
		t.Errorf("wire prose = %q, want English", got)
	}
	if got := buyertext.Text("ms", m.Code, m.Params); got != "Tiada penghantaran ke alamat ini" {
		t.Errorf("ms = %q, want the Malay arm", got)
	}
	if got := buyertext.Text("zh", m.Code, m.Params); got != "我们不配送到此地址" {
		t.Errorf("zh = %q, want the Chinese arm", got)
	}
}

// TestBuyerRefusalSentencesExistInEnglishAndMalay is the dictionary pin the card
// asks for: every refusal sentence is asserted in at least en + ms. A blank arm
// renders as empty text on a real page; a ms arm that is a copy of the English
// one means the translation was never made (the exact shape of the reported
// defect, in reverse).
func TestBuyerRefusalSentencesExistInEnglishAndMalay(t *testing.T) {
	for _, code := range buyertext.Codes() {
		en := buyertext.Text("en", code, nil)
		ms := buyertext.Text("ms", code, nil)
		zh := buyertext.Text("zh", code, nil)
		if strings.TrimSpace(en) == "" {
			t.Errorf("%s has no English sentence", code)
		}
		if strings.TrimSpace(ms) == "" {
			t.Errorf("%s has no Malay sentence", code)
		}
		if strings.TrimSpace(zh) == "" {
			t.Errorf("%s has no Chinese sentence", code)
		}
		if en == ms {
			t.Errorf("%s reads %q in BOTH en and ms — the Malay arm was never translated",
				code, en)
		}
	}
}
