package httpapi

// Card t_135c3c72 — the BUYER must see the processing fee, the method and the
// final total BEFORE they commit (Consumer Protection Act 1999: the price shown
// is the price paid). These tests render the real templates, so they fail if the
// chooser is dropped from a layout, if the fee row disappears, or if the page
// ever prices the fee the naive way (which leaves the merchant short).

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
)

// feeChoices is the view data both layouts receive when two methods are active.
func feeChoices() []feeChoiceView {
	return []feeChoiceView{
		{Method: "card", Label: "Card", RateLabel: "Card 1.00%", PercentBP: 100, Selected: true},
		{Method: "fpx", Label: "FPX", RateLabel: "FPX RM1.00", FixedSen: 100},
	}
}

// TestFeeChooserRendersOnEveryOrderLayout: both Go order renderers (the block
// layout and the LAST-RESORT legacy layout) post to the same /order route, so
// both must offer the method choice and show the fee row. A form served by the
// one that does not would price a method the buyer never picked.
func TestFeeChooserRendersOnEveryOrderLayout(t *testing.T) {
	render := func(name string, tmpl *template.Template, data any) string {
		t.Helper()
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			t.Fatalf("%s.Execute: %v", name, err)
		}
		return buf.String()
	}

	blockPage := render("blockTmpl", blockTmpl, map[string]any{
		"Slug": "demo", "Title": "Demo", "Blocks": []struct{}{},
		"Lang": "en", "Ls": formLangs["en"],
		"HeightJS":   template.HTML(iframeHeightJS),
		"FeeOptions": feeChoices(),
	})
	legacyPage := render("formTmpl", formTmpl, legacyFormView{
		Slug: "demo", Title: "Demo", Description: "desc",
		Products: []tmplProduct{{ID: "p1", Name: "Item", PriceSen: 100000}},
		OGMeta:   template.HTML(""), Footer: footerInfo{},
		AntiSpam: template.HTML(antiSpamRenderFields()),
		NoIndex:  true, HeightJS: template.HTML(iframeHeightJS),
		Lang: "en", Ls: langStringsForCode("en"),
		FeeOptions: feeChoices(),
	})

	for name, page := range map[string]string{"block layout": blockPage, "legacy layout": legacyPage} {
		// The chooser: one radio per method, carrying the rate the server will use.
		count := strings.Count(page, `class="feee"`)
		if count != 2 {
			t.Errorf("%s renders %d method radios, want 2 — the buyer cannot choose the method the fee is priced for\npage:\n%s", name, count, page)
		}
		for _, want := range []string{
			`data-fixed="0" data-bp="100"`,
			`data-fixed="100" data-bp="0"`,
			`value="card"`,
			`value="fpx"`,
			// The chooser label is the rate label alone, which already names the
			// method ("Card 1.00%"). Rendering the method name in front of it as
			// well produced "Card — Card 1.00%" — seen on the live page.
			`Card 1.00%`,
			`FPX RM1.00`,
		} {
			if !strings.Contains(page, want) {
				t.Errorf("%s is missing %q", name, want)
			}
		}
		if strings.Contains(page, "Card — Card") || strings.Contains(page, "FPX — FPX") {
			t.Errorf("%s doubles the method name in the chooser label (\"Card — Card 1.00%%\")", name)
		}
		// The breakdown: subtotal + its own fee row, then the total. The buyer
		// must be able to read what the goods cost and what paying cost.
		for _, want := range []string{`id="fee-sub"`, `id="fee-amt"`, `id="total"`} {
			if !strings.Contains(page, want) {
				t.Errorf("%s is missing the fee breakdown element %q", name, want)
			}
		}
		// Card t_69d2dd19 (Wan's rule): the fee ROW is the label + the amount.
		// It used to append the chosen method's rate label ("— FPX RM1.00"), and
		// the method name must never reach a fee line the buyer reads. The
		// picker above the Pay button is where the method is named.
		for _, banned := range []string{`id="fee-rate"`, "'— '+lbl", "'— ' + (fr.parentNode"} {
			if strings.Contains(page, banned) {
				t.Errorf("%s still renders the fee-row method suffix (%q) — the fee line must carry the amount only", name, banned)
			}
		}
		// The JS must GROSS UP, not add `base × rate`: the naive formula is 10 sen
		// short on RM1,000 at 1%, and the page would then show a total the server
		// refuses (or, worse, agree with a merchant-short charge). Whitespace is
		// normalised because the two renderers format the same expression
		// differently.
		tight := strings.ReplaceAll(page, " ", "")
		if !strings.Contains(tight, "Math.floor((v*bp+5000)/10000)") {
			t.Errorf("%s does not mirror the server's round-half-up fee formula", name)
		}
		if !strings.Contains(tight, "feeOf(f)<base") {
			t.Errorf("%s does not walk the total up to the smallest F with F-fee(F) >= base", name)
		}
		// The claim the server re-checks.
		for _, want := range []string{"expected_total_sen", "fee_method"} {
			if !strings.Contains(page, want) {
				t.Errorf("%s does not send %q — the server could not detect a stale page", name, want)
			}
		}
	}
}

// TestNoFeeActiveLeavesThePageUnchanged: with no active method the renderers
// emit no chooser, no fee row and no restriction — the pre-feature page.
func TestNoFeeActiveLeavesThePageUnchanged(t *testing.T) {
	var buf bytes.Buffer
	if err := blockTmpl.Execute(&buf, map[string]any{
		"Slug": "demo", "Title": "Demo", "Blocks": []struct{}{},
		"Lang": "en", "Ls": formLangs["en"],
		"HeightJS": template.HTML(iframeHeightJS),
	}); err != nil {
		t.Fatalf("blockTmpl.Execute: %v", err)
	}
	page := buf.String()
	for _, banned := range []string{`class="feee"`, `id="fee-amt"`, `id="fee-sub"`} {
		if strings.Contains(page, banned) {
			t.Errorf("page with no active fee rendered %q", banned)
		}
	}
	if !strings.Contains(page, `id="total"`) {
		t.Errorf("page lost its total")
	}
}

// TestPlatformFeeRidesOnThePickerRows (card t_1a17d22d): the picker row carries
// the FLAT platform amount the page's script adds to the price. Without it the
// page would paint the item price while the server charged item + fee, which is
// exactly the "amount the buyer never saw" refusal the route exists to prevent.
func TestPlatformFeeRidesOnThePickerRows(t *testing.T) {
	views := []payMethodChoiceView{
		{Key: "fpx", Label: "FPX", PlatformFeeSen: 100, Selected: true},
		{Key: "fpx_b2b1", Label: "FPX B2B1", PlatformFeeSen: 200},
	}
	var buf bytes.Buffer
	if err := blockTmpl.Execute(&buf, map[string]any{
		"Slug": "demo", "Title": "Demo", "Blocks": []struct{}{},
		"Lang": "en", "Ls": formLangs["en"],
		"HeightJS":   template.HTML(iframeHeightJS),
		"PayChoices": views,
	}); err != nil {
		t.Fatalf("blockTmpl.Execute: %v", err)
	}
	page := buf.String()
	for _, want := range []string{
		`value="fpx" data-name="FPX" data-platform="100"`,
		`value="fpx_b2b1" data-name="FPX B2B1" data-platform="200"`,
		"Platform fee", // the fee row's label, from the language table
	} {
		if !strings.Contains(page, want) {
			t.Errorf("picker page does not contain %q — the platform amount or its name is not reaching the buyer\npage:\n%s", want, page)
		}
	}
	// The buyer's path never calls it the acquirer's charge.
	for _, banned := range []string{"Processing fee", "processing fee"} {
		if strings.Contains(page, banned) {
			t.Errorf("buyer page still renders %q", banned)
		}
	}
}
