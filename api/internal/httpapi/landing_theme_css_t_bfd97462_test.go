// Card t_bfd97462 — a landing page themed with an `rgb(...)` colour was served
// as `ZgotmplZ`, so the `.cta` brand fill (and the hero / embed heading colours)
// never applied.
//
// The landing template interpolated the merchant's colours as plain strings
// (dbstore.LandingPage.PrimaryColor/.BackgroundColor are `string`). In CSS TEXT
// state html/template runs cssValueFilter over anything that is not
// template.CSS, and that filter rejects `(` — so `rgb(180, 83, 9)`, a colour the
// dashboard and the MCP write path both accept, was replaced by the sentinel
// while the page still returned 200 and nothing logged. A hex value passes the
// same filter, which is why a hex theme never showed the defect.
//
// These tests read the SERVED page: they store the landing page through the
// store and drive handleWorkspaceRoot with the workspace's Host, then take the
// `<style>` block out of the response. A test that executed landingPageTmpl with
// a hand-built payload could pass while the handler forgot to populate the
// typed view, which is exactly how the plain-string typing survived.
package httpapi

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// landingServedRule returns the declaration block of ONE selector in the
// served landing page's `<style>` sheet, with whitespace collapsed so a test can
// name the rule (`.cta`) instead of the whole sheet. It fails when the selector
// is absent — a sheet-wide "no sentinel" assertion must not be able to pass
// while the CTA lost its fill.
func landingServedRule(t *testing.T, body, selector string) string {
	t.Helper()
	i := strings.Index(body, "<style>")
	j := strings.Index(body, "</style>")
	if i < 0 || j <= i {
		t.Fatalf("served page has no <style>…</style> block\nbody:\n%s", body)
	}
	flat := strings.Join(strings.Fields(body[i+len("<style>"):j]), " ")
	re := regexp.MustCompile(`(?:^|[\s}])` + regexp.QuoteMeta(selector) + ` \{([^}]*)\}`)
	m := re.FindStringSubmatch(flat)
	if m == nil {
		t.Fatalf("served stylesheet has no %q rule\nsheet:\n%s", selector, flat)
	}
	return m[1]
}

// landingPageServed stores lp as the workspace's landing page, with one CTA
// pointing at the fixture's published form, and returns the body the real
// storefront root serves for that workspace plus the CTA's form slug.
//
// Runs against a real Postgres (JOMFORM_TEST_DATABASE_URL); skipped when unset.
func landingPageServed(t *testing.T, lp *dbstore.LandingPage) (body, formSlug string) {
	t.Helper()
	h, ws, formSlug := publicFixture(t)
	ctx := context.Background()
	wss := dbstore.NewWorkspaceStore(bundleTestPool(t))
	id, err := wss.IDBySlug(ctx, ws)
	if err != nil {
		t.Fatalf("resolve workspace id for %q: %v", ws, err)
	}
	st, err := wss.Settings(ctx, id)
	if err != nil {
		t.Fatalf("read workspace settings: %v", err)
	}
	lp.CTAs = append(lp.CTAs, dbstore.LandingCTA{Label: "Daftar", FormSlug: formSlug})
	st.LandingPage = lp
	if err := wss.SaveSettings(ctx, st); err != nil {
		t.Fatalf("save the landing page: %v", err)
	}
	// Read the config back so the assertion is about what the store serves,
	// not about the value this test handed it.
	back, err := wss.Settings(ctx, id)
	if err != nil || back.LandingPage == nil || back.LandingPage.PrimaryColor != lp.PrimaryColor {
		t.Fatalf("landing page did not round-trip through the store (err %v, got %+v)", err, back.LandingPage)
	}
	rec := publicServe(t, h, ws+".jomform.com", "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / on %s.jomform.com -> %d, want 200 (body %.400s)", ws, rec.Code, rec.Body.String())
	}
	served := rec.Body.String()
	if !strings.Contains(served, `class="cta" href="/`+formSlug+`"`) {
		t.Fatalf("served root is not the landing page with its CTA — body:\n%s", served)
	}
	return served, formSlug
}

// TestLandingCtaKeepsItsRGBThemeBackground is the card's repro: the merchant's
// `rgb(180, 83, 9)` must be the fill of the `.cta` rule in the served sheet, the
// hero heading's colour, and the embed heading's colour — and the page must
// carry neither the html/template sentinel nor a URL-escaped `#` (the menu
// page's sibling `%23` defect).
func TestLandingCtaKeepsItsRGBThemeBackground(t *testing.T) {
	body, _ := landingPageServed(t, &dbstore.LandingPage{
		Enabled:         true,
		Hero:            "Ali & Siti",
		PrimaryColor:    "rgb(180, 83, 9)",
		BackgroundColor: "#fafafa",
	})

	if strings.Contains(body, "ZgotmplZ") {
		t.Errorf("served landing page carries the html/template sentinel — the merchant's colour was replaced, "+
			"the page still answers 200 and nothing logs\nbody:\n%s", body)
	}
	if strings.Contains(body, "%23") {
		t.Errorf("served landing page URL-escapes a hex colour to %%23 — the colour is being read in a CSS "+
			"string state instead of CSS text\nbody:\n%s", body)
	}

	for _, tc := range []struct{ selector, want string }{
		{".cta", "background:rgb(180, 83, 9)"},
		{".hero h1", "color:rgb(180, 83, 9)"},
		{".embed h2", "color:rgb(180, 83, 9)"},
		{"body", "background:#fafafa"},
	} {
		if rule := landingServedRule(t, body, tc.selector); !strings.Contains(rule, tc.want) {
			t.Errorf("served %s rule = %q, want it to carry %q", tc.selector, rule, tc.want)
		}
	}
}

// TestLandingThemeColourCannotCloseTheStyleElement: verbatim emission is what
// makes an `rgb(...)` colour survive, so the `<style` breakout guard has to
// come with it. A colour ending in `</style><script>…` must be truncated at the
// end tag: exactly one `</style>` in the page, no injected element, and the
// valid prefix still applied to the CTA.
func TestLandingThemeColourCannotCloseTheStyleElement(t *testing.T) {
	body, _ := landingPageServed(t, &dbstore.LandingPage{
		Enabled:      true,
		Hero:         "Ali & Siti",
		PrimaryColor: `rgb(180, 83, 9)</style><script>alert(1)</script>`,
	})

	if n := strings.Count(body, "</style>"); n != 1 {
		t.Errorf("served page has %d </style> tags, want exactly 1 — the theme colour closed the style element", n)
	}
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Errorf("served page contains the injected script from the theme colour\nbody:\n%s", body)
	}
	if rule := landingServedRule(t, body, ".cta"); !strings.Contains(rule, "background:rgb(180, 83, 9)") {
		t.Errorf("served .cta rule = %q, want the valid prefix of the colour (%q) to still apply", rule, "background:rgb(180, 83, 9)")
	}
}

// TestLandingThemeHexColourStillServedVerbatim is the control: a hex theme — the
// only shape the colour picker used to produce, and the shape that never showed
// the defect — still reaches the served sheet byte-for-byte.
func TestLandingThemeHexColourStillServedVerbatim(t *testing.T) {
	body, _ := landingPageServed(t, &dbstore.LandingPage{
		Enabled:         true,
		Hero:            "Ali & Siti",
		PrimaryColor:    "#4f46e5",
		BackgroundColor: "#fafafa",
	})
	if strings.Contains(body, "ZgotmplZ") || strings.Contains(body, "%23") {
		t.Errorf("a hex theme is not served verbatim (sentinel or %%23 present)\nbody:\n%s", body)
	}
	if rule := landingServedRule(t, body, ".cta"); !strings.Contains(rule, "background:#4f46e5") {
		t.Errorf("served .cta rule = %q, want it to carry %q", rule, "background:#4f46e5")
	}
}

// TestLandingThemeFromTypesTheColoursForCSS pins the view itself, including the
// cases the served-page tests cannot reach without a database: a nil config and
// an unset colour both yield the empty template.CSS the template's `{{if}}`
// reads as "use the default".
func TestLandingThemeFromTypesTheColoursForCSS(t *testing.T) {
	v := landingThemeFrom(&dbstore.LandingPage{
		PrimaryColor:    "  rgb(180, 83, 9) ",
		BackgroundColor: "#fafafa</style><b>x</b>",
	})
	if got, want := string(v.PrimaryColor), "rgb(180, 83, 9)"; got != want {
		t.Errorf("PrimaryColor = %q, want %q (trimmed, otherwise verbatim)", got, want)
	}
	if got, want := string(v.BackgroundColor), "#fafafa"; got != want {
		t.Errorf("BackgroundColor = %q, want %q (truncated at the </style end tag)", got, want)
	}
	if v := landingThemeFrom(nil); v.PrimaryColor != "" || v.BackgroundColor != "" {
		t.Errorf("landingThemeFrom(nil) = %+v, want both colours empty so the template keeps its defaults", v)
	}
	if v := landingThemeFrom(&dbstore.LandingPage{}); v.PrimaryColor != "" || v.BackgroundColor != "" {
		t.Errorf("landingThemeFrom(empty) = %+v, want both colours empty so the template keeps its defaults", v)
	}
}
