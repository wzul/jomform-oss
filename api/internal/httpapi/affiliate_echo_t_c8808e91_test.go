package httpapi

// Card t_c8808e91 — issue #444: a sale made through a renderer that does not
// echo ?aff=CODE is attributed to nobody, so the promoter who sent the buyer is
// never paid.
//
// The LAST-RESORT layout (`renderLegacy` -> `formTmpl`, used for a published
// form whose config has no `blocks`) built its order body from client/items/
// payment_method/fields/table_number and never read ?aff= off the URL. The
// custom-HTML response body (`render_custom.go`) carried the same omission: it
// echoed ?table= and dropped ?aff=. The block renderers and the menu page did
// both, which is what kept the gap invisible — the affiliate echo existed, just
// not in every body that posts an order.
//
// Two guards:
//
//  1. TEMPLATE. Every order-submitting template must echo the affiliate code,
//     and — the property that makes the defect class catchable — any body that
//     echoes ?table= must also echo ?aff=. That asymmetry is exactly this bug.
//  2. END TO END. The legacy layout, served for a form with no blocks, must
//     carry that echo, and the code it echoes must land in
//     sales.affiliate_code. The submit half is a DB test; skipped unless
//     JOMFORM_TEST_DATABASE_URL is set (bundleTestPool).

import (
	"bytes"
	"context"
	"encoding/json"
	"html/template"
	"net/http"
	"strings"
	"testing"
)

// The affiliate echo, in the two spacings the templates use (the block
// renderers print it tight; the legacy body and render_custom use spaces).
const (
	affEchoTight  = "affiliate_code:(new URLSearchParams(location.search).get('aff')||'')"
	affEchoSpaced = "affiliate_code: (new URLSearchParams(location.search).get('aff') || '')"
	// The table echo, same two shapes, used for the reverse-direction guard.
	tableEchoTight  = "table_number:(new URLSearchParams(location.search).get('table')||'')"
	tableEchoSpaced = "table_number: (new URLSearchParams(location.search).get('table') || '')"
)

// orderBodyTemplates renders each renderer that POSTs to /order and returns its
// emitted page body. Rendering the template (not reading its source) keeps the
// assertion on what a buyer's browser actually receives.
func orderBodyTemplates(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}

	render := func(name string, tmpl *template.Template, data any) {
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			t.Fatalf("%s.Execute: %v", name, err)
		}
		out[name] = buf.String()
	}

	render("block product form (render_blocks.go)", blockTmpl, map[string]any{
		"Slug": "demo", "Title": "Demo", "Blocks": []struct{}{},
		"Lang": "en", "Ls": formLangs["en"],
		"HeightJS": template.HTML(iframeHeightJS),
	})
	render("response-only block form (render_blocks.go)", responseTmpl, map[string]any{
		"Slug": "demo", "Title": "Demo", "Fields": []struct{}{},
		"Lang": "en", "Ls": formLangs["en"],
		"HeightJS": template.HTML(iframeHeightJS),
	})
	render("legacy product form (public.go)", formTmpl, legacyFormView{
		Slug: "demo", Title: "Demo", Description: "desc",
		Products: []tmplProduct{{ID: "p1", Name: "Item", PriceSen: 100}},
		OGMeta:   template.HTML(""),
		Footer:   footerInfo{},
		AntiSpam: template.HTML(antiSpamRenderFields()),
		NoIndex:  true,
		HeightJS: template.HTML(iframeHeightJS),
		Lang:     "en",
		Ls:       langStringsForCode("en"),
	})
	render("custom-HTML response form (render_custom.go)", customFormTmpl, map[string]any{
		"Slug": "demo", "Title": "Demo",
		"BodyJS":   template.JS(`"<form></form>"`),
		"Lang":     "en",
		"Ls":       formLangs["en"],
		"AntiSpam": template.HTML(antiSpamRenderFields()),
		"HeightJS": template.HTML(iframeHeightJS),
	})
	return out
}

// TestOrderBodiesEchoAffiliateCode is the direct guard: no order body may omit
// the affiliate echo, or a buyer who arrived on ?aff=CODE is charged normally
// and the promoter is silently unpaid.
func TestOrderBodiesEchoAffiliateCode(t *testing.T) {
	for name, body := range orderBodyTemplates(t) {
		if !strings.Contains(body, affEchoTight) && !strings.Contains(body, affEchoSpaced) {
			t.Errorf("%s does not echo ?aff=CODE into its order body — a sale through it records an empty affiliate_code (issue #444, card t_c8808e91)\nwant either:\n  %s\n  %s",
				name, affEchoTight, affEchoSpaced)
		}
	}
}

// TestOrderBodiesEchoAffiliateWhereverTheyEchoTable pins the ASYMMETRY that
// caused this defect: the table echo and the affiliate echo were added to
// different renderers, so a body could carry one and drop the other. Any body
// that echoes ?table= must echo ?aff=.
func TestOrderBodiesEchoAffiliateWhereverTheyEchoTable(t *testing.T) {
	for name, body := range orderBodyTemplates(t) {
		hasTable := strings.Contains(body, tableEchoTight) || strings.Contains(body, tableEchoSpaced)
		hasAff := strings.Contains(body, affEchoTight) || strings.Contains(body, affEchoSpaced)
		if hasTable && !hasAff {
			t.Errorf("%s echoes ?table= but not ?aff= — the one-sided wiring that lost affiliate attribution (card t_c8808e91)", name)
		}
	}
}

// TestLegacyNoBlocksPageEchoesAndRecordsAffiliateCode drives the layout the card
// names: a published form with no `blocks`, served by renderLegacy. Its page
// must carry the affiliate echo, and a submit carrying the code that echo reads
// must persist sales.affiliate_code.
func TestLegacyNoBlocksPageEchoesAndRecordsAffiliateCode(t *testing.T) {
	h, host, formSlug, ids := legacyTotalFixture(t)

	page := issue461Serve(t, h, host, http.MethodGet, "/"+formSlug, "").Body.String()
	// If the fixture ever grew a `blocks` key this case would be measuring the
	// block renderer instead of renderLegacy.
	if !strings.Contains(page, `class="pname"`) || !strings.Contains(page, `id="total"`) {
		t.Fatalf("the served page is not the legacy product layout — this case no longer measures renderLegacy")
	}
	if !strings.Contains(page, affEchoTight) && !strings.Contains(page, affEchoSpaced) {
		t.Fatalf("the legacy layout's order body does not echo ?aff=CODE — a block-less form attributes no sale (card t_c8808e91)\npage:\n%s", page)
	}

	// What the echo sends for ?aff=PROMO77 is exactly this string; the server
	// half is what the sale must record.
	const code = "PROMO77"
	body := `{"client":{"full_name":"Ali","email":"ali@example.test"},` +
		`"items":[{"product_id":"` + ids[0] + `","quantity":1}],` +
		`"payment_method":"bank_direct","fields":{},` +
		`"affiliate_code":"` + code + `"}`
	rec := issue461Serve(t, h, host, http.MethodPost, "/"+formSlug+"/order", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		RefCode string `json:"ref_code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	sale, err := h.Sales.ByRefCode(context.Background(), resp.RefCode)
	if err != nil {
		t.Fatalf("fetch sale: %v", err)
	}
	if sale.AffiliateCode != code {
		t.Errorf("sale recorded affiliate_code = %q, want %q — the promoter who sent the buyer through ?aff= is attributed to nobody (issue #444)", sale.AffiliateCode, code)
	}
}
