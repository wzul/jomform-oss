package httpapi

// Issue #153: the buyer-facing block form must render a variation selector
// (with per-variation price/stock) for variable products, and sold-out
// variations must be disabled. Pure template test — no DB needed.

import (
	"bytes"
	"strings"
	"testing"
)

func TestBlockFormRendersVariationSelector(t *testing.T) {
	type variationView struct {
		ID       string
		Name     string
		PriceSen int64
		Stock    *int
		SoldOut  bool
	}
	type block struct {
		Type, Value, Src string
		ProductID        string
		PriceSen         int64
		Required         bool
		HasPrice         bool
		HasQuantity      bool
		CondJSON         string
		Image            string
		// Issue #471: product image size + layout + full-size fallback URL.
		ImgSize    string
		ImgTop     bool
		BaseImage  string
		Variations []variationView
		Style      string
	}
	stockZero := 0
	stockFive := 5
	var buf bytes.Buffer
	err := blockTmpl.Execute(&buf, map[string]any{
		"Slug": "demo", "Title": "Demo", "Blocks": []block{
			{
				Type: "product", ProductID: "p1", Value: "Tee", PriceSen: 1000, HasPrice: true,
				Variations: []variationView{
					{ID: "v1", Name: "Size M", PriceSen: 1200, Stock: &stockFive},
					{ID: "v2", Name: "Size L", PriceSen: 1300, Stock: &stockZero, SoldOut: true},
				},
			},
		},
		"TrackHead": "", "ThemePrimary": "", "ThemeBg": "", "LogoURL": "",
		"Lang": "ms",
		"Ls":   formLangs["ms"],
	})
	if err != nil {
		t.Fatalf("blockTmpl.Execute: %v", err)
	}
	body := buf.String()

	// A <select class="v"> must be present with both variations.
	if !strings.Contains(body, `class="v"`) {
		t.Error("variation selector <select class=\"v\"> missing")
	}
	if !strings.Contains(body, `value="v1"`) || !strings.Contains(body, `value="v2"`) {
		t.Error("variation options missing")
	}
	// Sold-out variation (stock 0) must be disabled.
	if !strings.Contains(body, `value="v2" data-price="1300" disabled`) {
		t.Error("sold-out variation (v2) not disabled")
	}
	// In-stock variation must NOT be disabled.
	if strings.Contains(body, `value="v1" data-price="1200" disabled`) {
		t.Error("in-stock variation (v1) incorrectly disabled")
	}
	// The selector placeholder label must be present.
	if !strings.Contains(body, "Pilih variasi") {
		t.Error("variation placeholder label missing")
	}
}
