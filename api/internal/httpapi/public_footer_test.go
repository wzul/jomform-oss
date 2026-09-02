package httpapi

import (
	"bytes"
	"strings"
	"testing"
)

// Issue #80: public forms must render NO footer line — neither the
// "Dikuasakan oleh JomForm" default nor any space footer. These tests render
// the two public form templates directly and assert no footer/branding markup
// survives. The Footer-text field in Space settings stays (inert), so only the
// render call sites were removed.
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
	})
	if err != nil {
		t.Fatalf("blockTmpl.Execute: %v", err)
	}
	body := buf.String()
	for _, banned := range []string{"Dikuasakan", "JomForm", "brand", "footer", "Footer"} {
		if strings.Contains(body, banned) {
			t.Errorf("block form output contains %q (footer must be removed)", banned)
		}
	}
}

func TestLegacyFormRendersNoFooter(t *testing.T) {
	var buf bytes.Buffer
	err := formTmpl.Execute(&buf, struct {
		Slug, Title, Description string
		Products                 []tmplProduct
	}{
		Slug: "demo", Title: "Demo", Description: "desc",
		Products: []tmplProduct{{ID: "p1", Name: "Item", PriceSen: 100}},
	})
	if err != nil {
		t.Fatalf("formTmpl.Execute: %v", err)
	}
	body := buf.String()
	for _, banned := range []string{"Dikuasakan", "JomForm", "brand", "footer", "Footer"} {
		if strings.Contains(body, banned) {
			t.Errorf("legacy form output contains %q (footer must be removed)", banned)
		}
	}
}
