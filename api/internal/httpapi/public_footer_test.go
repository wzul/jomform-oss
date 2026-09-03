package httpapi

import (
	"bytes"
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
		Footer                   footerInfo
	}{
		Slug: "demo", Title: "Demo", Description: "desc",
		Products: []tmplProduct{{ID: "p1", Name: "Item", PriceSen: 100}},
		Footer:   footerInfo{},
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
