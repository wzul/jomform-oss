package httpapi

// Card t_5c8fd63e: Arabic public order pages were served LTR.
//
// THE DEFECT. Every public buyer page resolves a language (form.config.lang ??
// workspace default_lang ?? "en") and declares it on <html lang="…">, and the
// copy is localised — but only the MENU page ever declared the DIRECTION. So an
// Arabic buyer got Arabic words laid out left-to-right on the custom-HTML page,
// both block renderers, the legacy product layout, the private gate, the
// restriction page and the post-payment page. Measured on prod (v1.21.267,
// custom-HTML response form with config.lang="ar"): the CTA read "إرسال" while
// document.documentElement.getAttribute('dir') was null.
//
// WHAT IS PINNED HERE
//   1. Every server-rendered surface that declares a resolved language declares
//      that language's direction too — Arabic gets dir="rtl", and NOTHING else
//      does. The surfaces are driven through their REAL renderers (not a copy of
//      the template) so a renderer that stops passing the language is a red.
//   2. The custom page passes the direction into the merchant IFRAME as well.
//      An iframe is a separate document: the parent's dir does not inherit into
//      it, so without this the merchant's Arabic form body still laid out LTR
//      inside an otherwise-RTL page.
//   3. The custom page's CSP is untouched by this change: it must still admit
//      the logo it paints (v1.21.267's exact-origin img-src, card t_8ef93e64).
//   4. Card t_3788c2df: the footer of these pages follows the language the page
//      declares. The restriction page shipped `<p class="f">Dikuasakan oleh
//      JomForm</p>` as a constant, so an Arabic page carried Malay chrome; the
//      hosted legal page declared lang="en" and carried the same Malay footer.
//      TestPublicPagesLocaliseThePoweredByFooter pins both against the
//      dictionary, per locale.
//
// BITE PROOF (run for this card, re-runnable): removing the `{{if isRTL …}}` arm
// from any one template — or the dirAttr call from one Fprintf page — turns the
// matching subtest RED with `dir=…` absent; see the completion summary for the
// measured run.

import (
	"bytes"
	"html"
	"html/template"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// dirSurface is one buyer-facing page under test. render returns the bytes the
// product actually writes for that language.
type dirSurface struct {
	name   string
	render func(t *testing.T, lang string) string
}

// dirSurfaces lists every server-rendered public page that declares a resolved
// language. A new one belongs here: that is the whole point of the table.
func dirSurfaces() []dirSurface {
	sale := func() *dbstore.Sale { return &dbstore.Sale{RefCode: "JF-AR-1", Status: dbstore.SalePaid} }
	gatedForm := func(lang string) *dbstore.Form {
		return &dbstore.Form{
			ID: "11111111-1111-1111-1111-111111111111", AccountID: "a1",
			Slug: "gated", Title: "Secret Form", Status: dbstore.FormStatusPrivate,
			Config: map[string]any{"lang": lang},
		}
	}
	return []dirSurface{
		{
			// The payment (block) order page — render_blocks.go blockTmpl.
			name: "blocks-payment",
			render: func(t *testing.T, lang string) string {
				cfg := map[string]any{
					"lang":   lang,
					"blocks": []any{map[string]any{"type": "text", "value": "Hello"}},
				}
				h := &PublicHandlers{}
				rec := httptest.NewRecorder()
				if !h.renderBlocks(rec, httptest.NewRequest("GET", "http://demo.jomform.com/pay", nil), themedForm("pay", "#5e6ad2", cfg)) {
					t.Fatalf("renderBlocks declined a block form (lang %q) — this surface is no longer under test", lang)
				}
				return rec.Body.String()
			},
		},
		{
			// The response-only block form — render_blocks.go responseTmpl.
			name: "blocks-response",
			render: func(t *testing.T, lang string) string {
				cfg := map[string]any{
					"lang":   lang,
					"mode":   "response",
					"blocks": []any{map[string]any{"type": "email", "value": "Email"}},
				}
				h := &PublicHandlers{}
				rec := httptest.NewRecorder()
				if !h.renderResponseForm(rec, httptest.NewRequest("GET", "http://demo.jomform.com/rsvp", nil), themedForm("rsvp", "#5e6ad2", cfg)) {
					t.Fatalf("renderResponseForm declined a response form (lang %q) — this surface is no longer under test", lang)
				}
				return rec.Body.String()
			},
		},
		{
			// The custom-HTML order page — render_custom.go customFormTmpl.
			name: "custom-html",
			render: func(t *testing.T, lang string) string {
				cfg := map[string]any{
					"lang": lang,
					"mode": "response",
					"custom_html": map[string]any{
						"html":   `<input type="text" name="name">`,
						"fields": []any{map[string]any{"field_id": "name", "type": "text", "label": "Name"}},
					},
				}
				h := &PublicHandlers{}
				rec := httptest.NewRecorder()
				if !h.renderCustomForm(rec, httptest.NewRequest("GET", "http://demo.jomform.com/checkout", nil), themedForm("checkout", "#5e6ad2", cfg)) {
					t.Fatalf("renderCustomForm declined a custom-HTML form (lang %q) — this surface is no longer under test", lang)
				}
				return rec.Body.String()
			},
		},
		{
			// The last-resort product-checkbox layout — public.go formTmpl.
			name: "legacy-layout",
			render: func(t *testing.T, lang string) string {
				h := &PublicHandlers{}
				rec := httptest.NewRecorder()
				h.renderLegacy(rec, httptest.NewRequest("GET", "http://demo.jomform.com/legacy", nil), themedForm("legacy", "#5e6ad2", map[string]any{"lang": lang}))
				return rec.Body.String()
			},
		},
		{
			// The private-form password gate — public.go renderPrivateGate.
			name: "private-gate",
			render: func(t *testing.T, lang string) string {
				h := &PublicHandlers{}
				rec := httptest.NewRecorder()
				h.renderPrivateGate(rec, httptest.NewRequest("GET", "http://demo.jomform.com/gated", nil), gatedForm(lang))
				return rec.Body.String()
			},
		},
		{
			// The schedule/limit message page — restrictions.go renderRestriction.
			name: "restriction-page",
			render: func(t *testing.T, lang string) string {
				h := &PublicHandlers{}
				rec := httptest.NewRecorder()
				h.renderRestriction(rec, httptest.NewRequest("GET", "http://demo.jomform.com/gated", nil), gatedForm(lang), restrictionExpired)
				return rec.Body.String()
			},
		},
		{
			// The post-payment page — public.go writeThankYouPage.
			name: "thank-you",
			render: func(t *testing.T, lang string) string {
				rec := httptest.NewRecorder()
				writeThankYouPage(rec, langStringsForCode(lang), sale(), "", "message", "status")
				return rec.Body.String()
			},
		},
		{
			// The restaurant menu — menu.go menuPageTmpl. This page was
			// ALREADY correct; it is here so moving it onto the shared rule
			// (formLang.go isRTL) cannot regress the one page that had it right.
			name: "menu",
			render: func(t *testing.T, lang string) string {
				d := menu485Fixture(t)
				d.Ls = langStringsForCode(lang)
				return renderMenu485(t, d)
			},
		},
	}
}

// resolvedLangs is every language the platform can resolve a page to, sorted so
// a failure message is stable. It is derived from formLangs, not hardcoded, so a
// sixth locale cannot be added without this guard exercising it.
func resolvedLangs() []string {
	langs := make([]string, 0, len(formLangs))
	for code := range formLangs {
		langs = append(langs, code)
	}
	sort.Strings(langs)
	return langs
}

// htmlOpenTag returns the page's opening <html …> tag — the element that carries
// lang and dir.
func htmlOpenTag(t *testing.T, body string) string {
	t.Helper()
	i := strings.Index(body, "<html")
	if i < 0 {
		t.Fatalf("the response has no <html> tag at all:\n%s", head(body))
	}
	j := strings.Index(body[i:], ">")
	if j < 0 {
		t.Fatalf("<html> tag is unterminated:\n%s", head(body[i:]))
	}
	return body[i : i+j+1]
}

// TestPublicOrderPagesDeclareTheirLanguageDirection is the card's defect as one
// invariant: the direction of the <html> tag follows the language the page
// declares. Arabic is RTL; the four LTR locales must not gain the attribute
// (their markup is unchanged apart from nothing at all).
func TestPublicOrderPagesDeclareTheirLanguageDirection(t *testing.T) {
	langs := resolvedLangs()
	if len(langs) < 5 {
		t.Fatalf("resolvedLangs() = %v — the five shipped locales are no longer all in formLangs", langs)
	}
	for _, surf := range dirSurfaces() {
		for _, lang := range langs {
			t.Run(surf.name+"/"+lang, func(t *testing.T) {
				tag := htmlOpenTag(t, surf.render(t, lang))

				if want := `lang="` + lang + `"`; !strings.Contains(tag, want) {
					t.Fatalf("the page declares %s, want %s — the language and its direction must agree", tag, want)
				}
				if n := strings.Count(tag, "dir="); lang == "ar" {
					if !strings.Contains(tag, `dir="rtl"`) {
						t.Fatalf("Arabic page served %s — Arabic copy laid out left-to-right is the defect this card fixes", tag)
					}
					if n != 1 {
						t.Errorf("Arabic page served %s with %d dir attributes, want exactly 1", tag, n)
					}
				} else if n != 0 {
					t.Errorf("%s page served %s — only Arabic is RTL (formLang.go isRTL); an LTR locale must carry no dir attribute", lang, tag)
				}
			})
		}
	}
}

// TestCustomHTMLPageCarriesDirectionIntoTheMerchantFrame: the merchant's HTML is
// rendered in a sandboxed iframe, and an iframe is a separate DOCUMENT — dir on
// the parent does not inherit into it. Without the attribute on the srcdoc's own
// <html>, the Arabic form body (the part the buyer actually fills in) keeps
// laying out LTR inside an RTL page.
func TestCustomHTMLPageCarriesDirectionIntoTheMerchantFrame(t *testing.T) {
	customBody := func(t *testing.T, lang string) string {
		cfg := map[string]any{
			"lang": lang, "mode": "response",
			"custom_html": map[string]any{
				"html":   `<input type="text" name="name">`,
				"fields": []any{map[string]any{"field_id": "name", "type": "text", "label": "Name"}},
			},
		}
		h := &PublicHandlers{}
		rec := httptest.NewRecorder()
		if !h.renderCustomForm(rec, httptest.NewRequest("GET", "http://demo.jomform.com/checkout", nil), themedForm("checkout", "#5e6ad2", cfg)) {
			t.Fatalf("renderCustomForm declined a custom-HTML form (lang %q)", lang)
		}
		return rec.Body.String()
	}

	// The srcdoc is written as a JS single-quoted string literal; the attributes
	// inside it are plain double quotes (only ACTIONS are escaped in JS context,
	// and lang flows through one).
	const arFrame = `frame.srcdoc='<!doctype html><html lang="ar" dir="rtl">`
	if body := customBody(t, "ar"); !strings.Contains(body, arFrame) {
		t.Fatalf("the Arabic custom page does not put lang/dir on the iframe's own <html> (want %q) — the merchant's form body still lays out LTR.\nsrcdoc line:\n%s",
			arFrame, srcdocLine(body))
	}

	const enFrame = `frame.srcdoc='<!doctype html><html lang="en">`
	body := customBody(t, "en")
	if !strings.Contains(body, enFrame) {
		t.Fatalf("the English custom page does not declare lang on the iframe's own <html> (want %q).\nsrcdoc line:\n%s", enFrame, srcdocLine(body))
	}
	if strings.Contains(srcdocLine(body), `dir=`) {
		t.Errorf("the English custom page put a dir attribute in the frame: %s", srcdocLine(body))
	}
}

// TestCustomHTMLPageCSPUnchangedForArabic: the direction fix must not move the
// CSP v1.21.267 shipped. The page paints the merchant's logo as an <img>, so the
// policy it sends has to admit that logo's exact origin — and still not one
// thing more.
func TestCustomHTMLPageCSPUnchangedForArabic(t *testing.T) {
	const logo = "https://cdn.example.com/logo.png"
	cfg := map[string]any{
		"lang": "ar", "mode": "response",
		"custom_html": map[string]any{
			"html":   `<input type="text" name="name">`,
			"fields": []any{map[string]any{"field_id": "name", "type": "text", "label": "Name"}},
		},
	}
	f := themedForm("checkout", "#5e6ad2", cfg)
	f.LogoURL = logo
	h := &PublicHandlers{}
	rec := httptest.NewRecorder()
	if !h.renderCustomForm(rec, httptest.NewRequest("GET", "http://demo.jomform.com/checkout", nil), f) {
		t.Fatal("renderCustomForm declined a custom-HTML form")
	}
	csp := rec.Header().Get("Content-Security-Policy")
	d := parseCustomCSP(t, csp)
	imgSrc, ok := d["img-src"]
	if !ok {
		t.Fatalf("the Arabic page sent no img-src directive: %q", csp)
	}
	if got, want := strings.Join(imgSrc, " "), "data: https://cdn.example.com"; got != want {
		t.Errorf("Arabic page img-src = %q, want %q — the direction fix must not move the logo policy", got, want)
	}
	if !strings.Contains(rec.Body.String(), `<img class="logo" src="`+logo+`"`) {
		t.Error("the Arabic page stopped painting the merchant logo it admits")
	}
	// And the direction really is on the page this policy accompanies.
	if tag := htmlOpenTag(t, rec.Body.String()); !strings.Contains(tag, `dir="rtl"`) {
		t.Errorf("Arabic page served %s with the logo admitted — direction missing", tag)
	}
}

// srcdocLine returns the line of the body that writes the merchant frame, for a
// readable failure message.
func srcdocLine(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "frame.srcdoc=") {
			return line
		}
	}
	return "<no frame.srcdoc= line — the custom-form bridge is gone>"
}

// TestPublicPagesLocaliseThePoweredByFooter (card t_3788c2df). The footer of a
// server-rendered page must be the dictionary's PoweredBy string for the
// language THAT PAGE declares — not a constant.
//
// The defect: restrictions.go wrote `<p class="f">Dikuasakan oleh JomForm</p>`
// into the Fprintf with no argument, so the schedule/limit page carried Malay
// chrome in all five locales (measured on prod v1.21.272: an Arabic restriction
// page served `<html lang="ar" dir="rtl">` with Arabic body copy and the Malay
// footer). rest_legal.go carried the same literal on a page that declares
// lang="en".
//
// Driven through the REAL renderers, and asserted in BOTH directions: the
// locale's own footer must be present, and no OTHER locale's footer may appear
// — an "always ms" regression fails the same way a blank footer does.
func TestPublicPagesLocaliseThePoweredByFooter(t *testing.T) {
	restrictionBody := func(t *testing.T, lang string) string {
		t.Helper()
		f := &dbstore.Form{
			ID: "11111111-1111-1111-1111-111111111111", AccountID: "a1",
			Slug: "gated", Title: "Secret Form", Status: dbstore.FormStatusPrivate,
			Config: map[string]any{"lang": lang},
		}
		h := &PublicHandlers{}
		rec := httptest.NewRecorder()
		h.renderRestriction(rec, httptest.NewRequest("GET", "http://demo.jomform.com/gated", nil), f, restrictionExpired)
		return rec.Body.String()
	}

	langs := resolvedLangs()
	if len(langs) < 5 {
		t.Fatalf("resolvedLangs() = %v — the five shipped locales are no longer all in formLangs", langs)
	}
	for _, lang := range langs {
		t.Run("restriction-page/"+lang, func(t *testing.T) {
			body := restrictionBody(t, lang)
			if want := `<p class="f">` + htmlEsc(formLangs[lang].PoweredBy) + `</p>`; !strings.Contains(body, want) {
				t.Fatalf("restriction page for %q does not carry its own footer (want %q):\n%s", lang, want, head(body))
			}
			for _, other := range langs {
				if other == lang {
					continue
				}
				if strings.Contains(body, htmlEsc(formLangs[other].PoweredBy)) {
					t.Errorf("restriction page for %q carries the %q footer %q — the footer must follow the page's own language",
						lang, other, formLangs[other].PoweredBy)
				}
			}
		})
	}

	// The card's two named arms, spelled out so a dictionary edit cannot quietly
	// make the table above agree with the bug.
	if body := restrictionBody(t, "ar"); !strings.Contains(body, "مدعوم بواسطة JomForm") {
		t.Errorf("Arabic restriction page does not carry the Arabic footer:\n%s", head(body))
	} else if strings.Contains(body, "Dikuasakan oleh JomForm") {
		t.Errorf("Arabic restriction page still carries the hardcoded Malay footer:\n%s", head(body))
	}
	if body := restrictionBody(t, "en"); !strings.Contains(body, "Powered by JomForm") {
		t.Errorf("English restriction page does not carry the English footer:\n%s", head(body))
	} else if strings.Contains(body, "Dikuasakan oleh JomForm") {
		t.Errorf("English restriction page still carries the hardcoded Malay footer:\n%s", head(body))
	}
	// Malay still renders as Malay (no over-correction).
	if body := restrictionBody(t, "ms"); !strings.Contains(body, "Dikuasakan oleh JomForm") {
		t.Errorf("Malay restriction page lost its Malay footer:\n%s", head(body))
	}

	// The hosted legal page shares the literal but not the template: it declares
	// lang="en" and every other string on it is English, so its footer must be
	// the English one too.
	t.Run("legal-page/en", func(t *testing.T) {
		rec := httptest.NewRecorder()
		writeLegalPageHTML(rec, "Terms of Service", "demo", "These are the terms.")
		body := rec.Body.String()
		if tag := htmlOpenTag(t, body); !strings.Contains(tag, `lang="en"`) {
			t.Fatalf("the legal page no longer declares lang=\"en\" (%s) — re-check whether its footer must follow a resolved language instead", tag)
		}
		if want := `<p class="f">` + html.EscapeString(formLangs["en"].PoweredBy) + `</p>`; !strings.Contains(body, want) {
			t.Errorf("the legal page does not carry the English footer (want %q):\n%s", want, head(body))
		}
		if strings.Contains(body, "Dikuasakan oleh JomForm") {
			t.Errorf("the legal page still carries the hardcoded Malay footer:\n%s", head(body))
		}
	})
}

// TestDirectionRuleToleratesPageDataWithoutALang pins the CONTRACT of the rule
// as a template function: a payload that omits Lang renders an LTR page, it does
// not fail. The first cut of this fix took a plain `string`, which turned a
// missing key into `executing "blocks" at <.Lang>: invalid value; expected
// string` — an execution error that blanks the page where the old
// `lang="{{.Lang}}"` had merely rendered empty. An existing guard
// (url_scheme_issue501_test.go) executes blockTmpl with exactly such a payload
// and caught it.
func TestDirectionRuleToleratesPageDataWithoutALang(t *testing.T) {
	for name, tmpl := range map[string]*template.Template{
		"blocks": blockTmpl, "response": responseTmpl, "custom": customFormTmpl,
	} {
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, map[string]any{}); err != nil {
			t.Errorf("%s template failed on a payload with no Lang: %v — a language we cannot read must mean LTR, not a broken page", name, err)
			continue
		}
		tag := htmlOpenTag(t, buf.String())
		if strings.Contains(tag, "dir=") {
			t.Errorf("%s template emitted %s for a payload with no Lang", name, tag)
		}
	}
}
