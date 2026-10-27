package httpapi

// Issue #173: the block form renderer must support the new Gravity-Forms-style
// field types (select, radio, number, date, time, website, address, consent,
// hidden, list, repeater, image-choice, captcha, section, html) and the
// pricing fields (quantity, option, shipping). Pure template test — no DB.

import (
	"bytes"
	"strings"
	"testing"
)

func TestBlockFormRendersNewFieldTypes(t *testing.T) {
	type choiceView struct {
		Label    string
		Value    string
		PriceSen int64
	}
	type block struct {
		ID, Type, Value, Src string
		ProductID            string
		PriceSen             int64
		Required             bool
		HasPrice             bool
		CondJSON             string
		Image                string
		// Issue #471: product image size + layout + full-size fallback URL.
		ImgSize      string
		ImgTop       bool
		BaseImage    string
		Variations   []struct{}
		Placeholder  string
		Description  string
		CSSClass     string
		Style        string
		MaxLength    int
		DefaultValue string
		Min, Max     string
		Step         string
		Format       string
		Pattern      string
		RequiredMsg  string
		Choices      []choiceView
		// Managed zone rates (the shipping block's "zones" source). The template
		// reads .Zones for a shipping block; a static shipping block (this
		// fixture) leaves it empty and renders its own priced choices.
		Zones []struct {
			Value       string
			Label       string
			PriceSen    int64
			FreeOverSen int64
			Note        string
		}
		HasQuantity bool
	}
	var buf bytes.Buffer
	err := blockTmpl.Execute(&buf, map[string]any{
		"Slug": "demo", "Title": "Demo", "Blocks": []block{
			{Type: "product", Value: "Yuran", ProductID: "p1", PriceSen: 300, HasPrice: true, HasQuantity: true,
				Image: "https://cdn.example/p1.jpg", ImgSize: "small", BaseImage: "https://cdn.example/p1.jpg"},
			{Type: "select", Value: "Colour", Required: true, Choices: []choiceView{{Label: "Red", Value: "red"}, {Label: "Blue", Value: "blue"}}},
			{Type: "radio", Value: "Size", Choices: []choiceView{{Label: "S", Value: "s"}, {Label: "M", Value: "m"}}},
			{Type: "number", Value: "Qty", Min: "1", Max: "10", Step: "1"},
			{Type: "date", Value: "Date"},
			{Type: "time", Value: "Time"},
			{Type: "website", Value: "Website"},
			{Type: "address", Value: "Address"},
			{Type: "consent", Value: "I agree", Required: true},
			{Type: "hidden", Value: "utm", DefaultValue: "abc"},
			{Type: "list", Value: "List"},
			{Type: "repeater", Value: "Repeater"},
			{Type: "image-choice", Value: "Pick", Choices: []choiceView{{Label: "A", Value: "a"}}},
			{Type: "captcha", Value: "Captcha"},
			{Type: "section", Value: "Section title"},
			{Type: "html", Value: "<b>rich</b>"},
			{Type: "quantity", Value: "Qty", DefaultValue: "2"},
			{Type: "option", Value: "Add-on", ProductID: "opt1", PriceSen: 500, HasPrice: true, Choices: []choiceView{{Label: "Extra", Value: "x", PriceSen: 500}}},
			{Type: "shipping", Value: "Shipping", ProductID: "ship1", PriceSen: 800, HasPrice: true, Choices: []choiceView{{Label: "Express", Value: "e", PriceSen: 800}}},
		},
		"TrackHead": "", "ThemePrimary": "", "ThemeBg": "", "LogoURL": "",
		"Lang": "ms",
		"Ls":   formLangs["ms"],
	})
	if err != nil {
		t.Fatalf("blockTmpl.Execute: %v", err)
	}
	body := buf.String()

	// Standard fields render their input types.
	for _, want := range []string{
		`<select name="notes" data-id="" required>`, // select
		`type="radio"`, // radio
		`type="number" name="notes" data-id="" min="1" max="10" step="1"`, // number
		`type="date"`, `type="time"`, `type="url"`, // date/time/website
		`<textarea name="notes" data-id="" rows="2">`,                 // address
		`type="checkbox" class="extra" data-id="" value="1" required`, // consent
		`type="hidden" name="notes" data-id="" value="abc"`,           // hidden
		`<textarea name="notes" data-id="" rows="3">`,                 // list/repeater
		`class="section"`,    // section
		`<div class="text">`, // html
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in rendered block form", want)
		}
	}

	// Pricing fields: per-product quantity input, option + shipping selects with prices.
	if !strings.Contains(body, `class="pq"`) {
		t.Error("per-product quantity input missing")
	}
	if !strings.Contains(body, `class="opt"`) {
		t.Error("option select missing")
	}
	if !strings.Contains(body, `class="ship"`) {
		t.Error("shipping select missing")
	}
	if !strings.Contains(body, `data-price="500"`) {
		t.Error("option price not rendered")
	}
	if !strings.Contains(body, `data-price="800"`) {
		t.Error("shipping price not rendered")
	}
	// The total-update JS must reference the pricing elements.
	if !strings.Contains(body, "select.opt") || !strings.Contains(body, "select.ship") || !strings.Contains(body, "input.pq") {
		t.Error("pricing JS wiring missing")
	}
	// Issue #249: the product block carries its own quantity selector scoped to
	// that product (data-id), and the total JS multiplies per-product.
	if !strings.Contains(body, `class="pq" data-id="p1"`) {
		t.Error("per-product quantity not scoped to product id")
	}
	if !strings.Contains(body, "pqty(b)") {
		t.Error("per-product quantity not used in total calc")
	}
	// Issue #439: product image thumbnail next to the name. The product with an
	// image renders <img class="pimg">; the pname span follows it inside the label.
	// Issue #471: the image now carries data-size (the merchant-configurable
	// size; default small) and data-base (the full-size fallback URL).
	if !strings.Contains(body, `<img class="pimg" data-size="small" src="https://cdn.example/p1.jpg" alt="Yuran" data-base="https://cdn.example/p1.jpg">`) {
		t.Error("product image thumbnail not rendered")
	}
	if !strings.Contains(body, `.pimg {`) {
		t.Error(".pimg CSS not present")
	}
	// Fallback: an option/shipping block has no image, so no <img class="pimg">
	// may appear before the option select — ensure the img only ever appears in
	// product blocks (count = 1).
	if n := strings.Count(body, `class="pimg"`); n != 1 {
		t.Errorf("pimg occurrences = %d, want 1 (product image only, no fallback img for others)", n)
	}
}

func TestResponseFormRendersNewFieldTypes(t *testing.T) {
	type choiceView struct {
		Label string
		Value string
	}
	type field struct {
		ID, Type, Value string
		Required        bool
		CondJSON        string
		Placeholder     string
		Description     string
		// Card t_23289ce3: the per-field settings responseTmpl now paints.
		CSSClass     string
		Style        string
		Format       string
		RequiredMsg  string
		MaxLength    int
		DefaultValue string
		Min, Max     string
		Step         string
		Pattern      string
		Choices      []choiceView
	}
	var buf bytes.Buffer
	err := responseTmpl.Execute(&buf, map[string]any{
		"Slug": "demo", "Title": "Demo", "Fields": []field{
			{ID: "f1", Type: "select", Value: "Colour", Required: true, Choices: []choiceView{{Label: "Red", Value: "red"}}},
			{ID: "f2", Type: "radio", Value: "Size", Choices: []choiceView{{Label: "S", Value: "s"}}},
			{ID: "f3", Type: "number", Value: "Qty", Min: "1", Max: "5"},
			{ID: "f4", Type: "date", Value: "Date"},
			{ID: "f5", Type: "consent", Value: "I agree", Required: true},
			{ID: "f6", Type: "hidden", Value: "utm", DefaultValue: "x"},
		},
		"TrackHead": "", "ThemePrimary": "", "ThemeBg": "", "LogoURL": "",
		"Lang": "en",
		"Ls":   formLangs["en"],
	})
	if err != nil {
		t.Fatalf("responseTmpl.Execute: %v", err)
	}
	body := buf.String()
	for _, want := range []string{
		`name="f1" required`,           // select
		`name="f2"`,                    // radio
		`name="f3" min="1" max="5"`,    // number
		`name="f4"`,                    // date
		`name="f5" value="1" required`, // consent
		`name="f6" value="x"`,          // hidden
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in rendered response form", want)
		}
	}
}
