package httpapi

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
)

// Issue #80: public forms must render NO footer line by default — neither the
// "Dikuasakan oleh JomForm" default nor any space footer. Issue #183 restores
// an OPTIONAL per-space footer: it is rendered ONLY when the merchant sets at
// least one footer field. These tests render the public form templates directly
// and assert (a) no footer content survives when nothing is set, and (b) the
// footer renders when fields are set.
func TestBlockFormRendersNoFooter(t *testing.T) {
	type block struct {
		Type, Value, Src string
		ProductID        string
		PriceSen         int64
		Required         bool
		HasPrice         bool
		CondJSON         string
		Style            string
	}
	var buf bytes.Buffer
	err := blockTmpl.Execute(&buf, map[string]any{
		"Slug": "demo", "Title": "Demo", "Blocks": []block{
			{Type: "text", Value: "Hello"},
		},
		"TrackHead": "", "ThemePrimary": "", "ThemeBg": "", "LogoURL": "",
		"Lang": "ms",
		"Ls":   formLangs["ms"],
		// Issue #183: no footer fields set -> no footer rendered.
		"Footer": footerInfo{},
	})
	if err != nil {
		t.Fatalf("blockTmpl.Execute: %v", err)
	}
	body := buf.String()
	for _, banned := range []string{"Dikuasakan", "<footer", "Polisi Privasi", "Privacy Policy"} {
		if strings.Contains(body, banned) {
			t.Errorf("block form output contains %q (footer must be absent when nothing is set)", banned)
		}
	}
}

func TestLegacyFormRendersNoFooter(t *testing.T) {
	var buf bytes.Buffer
	err := formTmpl.Execute(&buf, struct {
		Slug, Title, Description string
		Products                 []tmplProduct
		OGMeta                   template.HTML
		Footer                   footerInfo
		AntiSpam                 template.HTML
		NoIndex                  bool
	}{
		Slug: "demo", Title: "Demo", Description: "desc",
		Products: []tmplProduct{{ID: "p1", Name: "Item", PriceSen: 100}},
		OGMeta:   template.HTML(""),
		Footer:   footerInfo{},
		AntiSpam: template.HTML(antiSpamRenderFields()),
		NoIndex:  true,
	})
	if err != nil {
		t.Fatalf("formTmpl.Execute: %v", err)
	}
	body := buf.String()
	for _, banned := range []string{"Dikuasakan", "<footer", "Polisi Privasi", "Privacy Policy"} {
		if strings.Contains(body, banned) {
			t.Errorf("legacy form output contains %q (footer must be absent when nothing is set)", banned)
		}
	}
}

// Issue #183: when the merchant sets footer fields, the public form renders an
// optional footer with business info + privacy/terms/refund links.
func TestBlockFormRendersOptionalFooter(t *testing.T) {
	type block struct {
		Type, Value, Src string
		ProductID        string
		PriceSen         int64
		Required         bool
		HasPrice         bool
		CondJSON         string
		Style            string
	}
	var buf bytes.Buffer
	err := blockTmpl.Execute(&buf, map[string]any{
		"Slug": "demo", "Title": "Demo", "Blocks": []block{
			{Type: "text", Value: "Hello"},
		},
		"TrackHead": "", "ThemePrimary": "", "ThemeBg": "", "LogoURL": "",
		"Lang": "ms",
		"Ls":   formLangs["ms"],
		"Footer": footerInfo{
			HasFooter:      true,
			BusinessName:   "Acme Sdn Bhd",
			CompanyReg:     "202001012345",
			ContactPhone:   "+60 12 345 6789",
			ContactAddress: "1 Jalan Test, Kuala Lumpur",
			PrivacyURL:     "https://acme.example/privacy",
			TermsURL:       "https://acme.example/terms",
			RefundURL:      "https://acme.example/refund",
		},
	})
	if err != nil {
		t.Fatalf("blockTmpl.Execute: %v", err)
	}
	body := buf.String()
	for _, want := range []string{
		"jf-footer", "Acme Sdn Bhd", "202001012345", "60 12 345 6789",
		"1 Jalan Test, Kuala Lumpur", "https://acme.example/privacy",
		"https://acme.example/terms", "https://acme.example/refund",
		"Polisi Privasi", "Terma &amp; Syarat", "Polisi Bayaran Balik",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("block form output missing %q (optional footer should render when set)", want)
		}
	}
}

// Issue #232: the public form body is a flex container. It must be a COLUMN
// flex so the form .card and the <footer class="jf-footer"> stack vertically
// (footer below the card) instead of sitting side-by-side (footer to the right
// of the card). Assert the body CSS is column and the footer is a sibling that
// appears AFTER the closing </form> tag.
func TestBlockFormFooterBelowCard(t *testing.T) {
	type block struct {
		Type, Value, Src string
		ProductID        string
		PriceSen         int64
		Required         bool
		HasPrice         bool
		CondJSON         string
		Style            string
	}
	var buf bytes.Buffer
	err := blockTmpl.Execute(&buf, map[string]any{
		"Slug": "demo", "Title": "Demo", "Blocks": []block{
			{Type: "text", Value: "Hello"},
		},
		"TrackHead": "", "ThemePrimary": "", "ThemeBg": "", "LogoURL": "",
		"Lang": "ms",
		"Ls":   formLangs["ms"],
		"Footer": footerInfo{
			HasFooter:    true,
			BusinessName: "Acme Sdn Bhd",
			PrivacyURL:   "https://acme.example/privacy",
			TermsURL:     "https://acme.example/terms",
		},
	})
	if err != nil {
		t.Fatalf("blockTmpl.Execute: %v", err)
	}
	body := buf.String()

	// Body must be a column flex so .card and .jf-footer stack vertically.
	if !strings.Contains(body, "display:flex; flex-direction:column;") {
		t.Errorf("body CSS is not a column flex; footer would sit to the right of the card")
	}

	// The footer must be a sibling rendered BELOW the card: it must appear
	// after the closing </form> tag, not before it.
	formEnd := strings.Index(body, "</form>")
	footerIdx := strings.Index(body, `<footer class="jf-footer">`)
	if formEnd < 0 {
		t.Fatalf("rendered output missing </form>")
	}
	if footerIdx < 0 {
		t.Fatalf("rendered output missing <footer class=\"jf-footer\">")
	}
	if footerIdx < formEnd {
		t.Errorf("footer renders before the card closes (footerIdx=%d < formEnd=%d); it must be below the card", footerIdx, formEnd)
	}
}
