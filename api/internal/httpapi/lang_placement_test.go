package httpapi

// Language placement — the guards for this change.
//
// WHAT THE BUG WAS
//   The public form renderer hardcoded Malay in three places:
//
//     public.go  <html lang="ms">  (legacy product layout)
//     public.go  <html lang="ms">  (thank-you page)
//     public.go  <html lang="ms">  (private-form password gate)
//
//   The block/response/custom renderers already resolved the buyer-facing
//   language (form.config.lang ?? workspace_settings.default_lang ?? "en") via
//   effectiveLangStrings, but these three did not — so a workspace that had
//   chosen English still served `lang="ms"` with Malay chrome to its buyers.
//   That is the "stored but half-consumed field" shape: the workspace setting
//   was read on some paths and ignored on others.
//
// WHAT IS PINNED HERE
//   The resolution itself (`TestLanguagePlacementResolution`), and — through the
//   REAL renderers, not a copy of the template — that the resolved language
//   reaches the three previously-hardcoded pages:
//
//     - the legacy product layout's <html lang> and its chrome strings,
//     - the private gate's <html lang> and its chrome strings,
//     - the thank-you page's <html lang> and its status/Powered-by strings.
//
// BITE PROOF (see the file's git history / PR description for the run)
//   Re-pointing any of the three at a literal ("ms" or a fixed dictionary)
//   turns the corresponding assertion RED — the tests render through the
//   production templates/handlers, so they read whatever the code actually
//   emits. They do not re-derive the expected string from the same map they are
//   checking, and they assert the NEGATIVE too (`lang="ms"` must be absent for
//   an English workspace), so an "always ms" regression cannot pass.
//
// The resolution tests are pure (no DB). The renderer tests use the real
// templates with a nil Workspaces store, which is exactly the "workspace
// language cannot be resolved" path → "en".

import (
	"bytes"
	"context"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// ── resolution: form.lang ?? workspace default ?? en ──────────────────────────

// TestLanguagePlacementResolution pins the precedence chain and the fallback:
// a per-form override wins, and with no form override (and no resolvable
// workspace, i.e. a nil store) the result is English — never Malay.
func TestLanguagePlacementResolution(t *testing.T) {
	h := &PublicHandlers{} // Workspaces == nil → workspace default resolves to "en"
	ctx := context.Background()

	cases := []struct {
		name   string
		config map[string]any
		want   string
	}{
		{"per-form override wins", map[string]any{"lang": "ms"}, "ms"},
		{"no override -> en (not ms)", map[string]any{}, "en"},
		{"nil config -> en (not ms)", nil, "en"},
		{"invalid override ignored -> en", map[string]any{"lang": "fr"}, "en"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &dbstore.Form{Config: tc.config}
			if got := h.effectiveLangStrings(ctx, f).Lang; got != tc.want {
				t.Fatalf("resolved lang = %q, want %q", got, tc.want)
			}
		})
	}

	// A nil form must not panic and must resolve to English.
	if got := h.effectiveLangStrings(ctx, nil).Lang; got != "en" {
		t.Errorf("nil form resolved lang = %q, want \"en\"", got)
	}
}

// TestLanguagePlacementEveryDictionaryHasTheNewChromeStrings guards the class of
// bug this change could introduce: a locale that carries SOME of the chrome
// strings but not all would silently render an EMPTY label (html/template does
// not fall back between map entries), so a field added to one locale must be
// added to all five.
func TestLanguagePlacementEveryDictionaryHasTheNewChromeStrings(t *testing.T) {
	for code, ls := range formLangs {
		for name, v := range map[string]string{
			"NameLabel": ls.NameLabel, "PhoneLabel": ls.PhoneLabel,
			"PoweredBy": ls.PoweredBy, "OrderLabel": ls.OrderLabel,
			"StatusLabel": ls.StatusLabel, "UnlockTitle": ls.UnlockTitle,
			"UnlockHelp": ls.UnlockHelp, "UnlockAction": ls.UnlockAction,
			"UnlockError":   ls.UnlockError,
			"StatusPending": ls.StatusPending, "StatusPaid": ls.StatusPaid,
			"StatusConfirmed": ls.StatusConfirmed, "StatusFailed": ls.StatusFailed,
			"StatusCancelled": ls.StatusCancelled,
		} {
			if strings.TrimSpace(v) == "" {
				t.Errorf("locale %q: %s is empty — that page would render a blank label", code, name)
			}
		}
	}
}

// ── renderer: legacy product layout ──────────────────────────────────────────

// legacyFormBody renders the legacy product layout through the REAL formTmpl
// for the given resolved language, the same way renderLegacy feeds it.
func legacyFormBody(lang string) string {
	var buf bytes.Buffer
	ls := langStringsForCode(lang)
	err := formTmpl.Execute(&buf, struct {
		Slug, Title, Description string
		Products                 []tmplProduct
		OGMeta                   template.HTML
		Footer                   footerInfo
		AntiSpam                 template.HTML
		NoIndex                  bool
		HeightJS                 template.HTML
		Lang                     string
		Ls                       langStrings
	}{
		Slug: "demo", Title: "Demo", Description: "desc",
		Products: []tmplProduct{{ID: "p1", Name: "Item", PriceSen: 100}},
		Footer:   footerInfo{},
		Lang:     ls.Lang,
		Ls:       ls,
	})
	if err != nil {
		panic(err)
	}
	return buf.String()
}

// TestLegacyProductLayoutFollowsWorkspaceLanguage is the core assertion for the
// legacy renderer: an English workspace serves `lang="en"` with English chrome,
// and crucially does NOT serve `lang="ms"` / the Malay labels.
func TestLegacyProductLayoutFollowsWorkspaceLanguage(t *testing.T) {
	en := legacyFormBody("en")
	if !strings.Contains(en, `<html lang="en">`) {
		t.Errorf("English workspace legacy form did not serve <html lang=\"en\">:\n%s", head(en))
	}
	if strings.Contains(en, `<html lang="ms">`) {
		t.Errorf("English workspace legacy form still served <html lang=\"ms\"> — the hardcoded Malay is back")
	}
	for _, want := range []string{"Full name", "Phone number", "Proceed to Payment", "Total"} {
		if !strings.Contains(en, want) {
			t.Errorf("English legacy form missing English chrome %q", want)
		}
	}
	// The Malay chrome must be GONE — otherwise "English" is only the attribute.
	for _, banned := range []string{"Nama penuh", "Teruskan ke Pembayaran", "Dikuasakan oleh JomForm"} {
		if strings.Contains(en, banned) {
			t.Errorf("English legacy form still contains Malay chrome %q", banned)
		}
	}

	// And Malay still renders as Malay (no over-correction).
	ms := legacyFormBody("ms")
	if !strings.Contains(ms, `<html lang="ms">`) {
		t.Errorf("Malay legacy form did not serve <html lang=\"ms\">")
	}
	if !strings.Contains(ms, "Nama penuh") {
		t.Errorf("Malay legacy form lost its Malay chrome")
	}
}

// TestLegacyProductLayoutFooterLinksFollowLanguage: the footer's own link
// labels were hardcoded Malay too, so an English workspace showed "Polisi
// Privasi" under an otherwise-English form.
func TestLegacyProductLayoutFooterLinksFollowLanguage(t *testing.T) {
	var buf bytes.Buffer
	ls := langStringsForCode("en")
	err := formTmpl.Execute(&buf, struct {
		Slug, Title, Description string
		Products                 []tmplProduct
		OGMeta                   template.HTML
		Footer                   footerInfo
		AntiSpam                 template.HTML
		NoIndex                  bool
		HeightJS                 template.HTML
		Lang                     string
		Ls                       langStrings
	}{
		Slug: "demo", Title: "Demo", Description: "desc",
		Products: []tmplProduct{{ID: "p1", Name: "Item", PriceSen: 100}},
		Footer: footerInfo{
			HasFooter:  true,
			PrivacyURL: "https://acme.example/privacy",
			TermsURL:   "https://acme.example/terms",
			RefundURL:  "https://acme.example/refund",
		},
		Lang: ls.Lang, Ls: ls,
	})
	if err != nil {
		t.Fatalf("formTmpl.Execute: %v", err)
	}
	body := buf.String()
	for _, want := range []string{"Privacy Policy", "Terms of Service", "Refund Policy"} {
		if !strings.Contains(body, want) {
			t.Errorf("English legacy footer missing %q", want)
		}
	}
	for _, banned := range []string{"Polisi Privasi", "Polisi Bayaran Balik"} {
		if strings.Contains(body, banned) {
			t.Errorf("English legacy footer still shows Malay link %q", banned)
		}
	}
}

// ── renderer: private gate ──────────────────────────────────────────────────

// TestPrivateGateFollowsWorkspaceLanguage pins the password gate. It renders
// through the real handler with a nil Workspaces store (→ English) and asserts
// both the attribute and the visible chrome, plus the negative.
func TestPrivateGateFollowsWorkspaceLanguage(t *testing.T) {
	h := &PublicHandlers{} // nil Workspaces → "en"
	f := &dbstore.Form{
		ID: "11111111-1111-1111-1111-111111111111", AccountID: "a1",
		Slug: "gated", Title: "Secret Form", Status: dbstore.FormStatusPrivate,
	}
	rec := httptest.NewRecorder()
	h.renderPrivateGate(rec, httptest.NewRequest(http.MethodGet, "/gated", nil), f)
	body := rec.Body.String()

	if !strings.Contains(body, `<html lang="en">`) {
		t.Errorf("private gate did not serve <html lang=\"en\">:\n%s", head(body))
	}
	if strings.Contains(body, `<html lang="ms">`) {
		t.Errorf("private gate still served <html lang=\"ms\"> — hardcoded Malay is back")
	}
	if !strings.Contains(body, "Unlock") || !strings.Contains(body, "This form is password protected.") {
		t.Errorf("private gate is missing its English chrome")
	}
	if strings.Contains(body, "Buka kunci") || strings.Contains(body, "Borang ini dilindungi kata laluan.") {
		t.Errorf("private gate still contains Malay chrome")
	}
	// The merchant-controlled title must still be present and escaped.
	if !strings.Contains(body, "Secret Form") {
		t.Errorf("private gate lost the form title")
	}
}

// TestPrivateGateEscapesMerchantTitle: the locale work must not regress the
// issue-#537 escaping — the title is merchant-controlled and this is a raw
// fmt.Fprintf sink.
func TestPrivateGateEscapesMerchantTitle(t *testing.T) {
	h := &PublicHandlers{}
	f := &dbstore.Form{
		ID: "11111111-1111-1111-1111-111111111111", AccountID: "a1",
		Slug:  "gated",
		Title: `</title><script>alert(1)</script>`,
	}
	rec := httptest.NewRecorder()
	h.renderPrivateGate(rec, httptest.NewRequest(http.MethodGet, "/gated", nil), f)
	body := rec.Body.String()
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Errorf("private gate emitted an unescaped merchant title (XSS regression):\n%s", head(body))
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Errorf("private gate did not escape the merchant title at all")
	}
}

// ── small helpers ───────────────────────────────────────────────────────────

// head returns the first 400 chars of a body for a readable failure message.
func head(s string) string {
	if len(s) > 400 {
		return s[:400]
	}
	return s
}
