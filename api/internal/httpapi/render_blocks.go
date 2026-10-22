package httpapi

import (
	"encoding/json"

	"html/template"
	"log/slog"
	"net/http"
	"strings"

	"github.com/wzul/jomform/api/internal/customform"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// variationView is the buyer-facing shape of one product variation. It is
// PACKAGE-scoped (not local to renderBlocks) because the restaurant menu
// renders the same selector for a variable product (issue #665) — one shape,
// one convention, so the form path and the menu path cannot drift apart.
type variationView struct {
	ID       string
	Name     string
	PriceSen int64
	Stock    *int
	SoldOut  bool
	// Price is the display form of PriceSen ("12.50"). The menu template has
	// no arithmetic helper of its own (issue #665), so the formatted value is
	// carried on the view instead of being computed in the template.
	Price string
}

// soldOutStock is the ONE stock-to-buyability rule: a variation or product with
// a limited stock that has reached zero is sold out. An unlimited (NULL) stock
// is never sold out. It is used by every renderer AND by the menu page's
// live-stock read (handleMenuStock), so a page cannot offer a variant the read
// reports as unbuyable, or the other way round.
func soldOutStock(stock *int) bool { return stock != nil && *stock <= 0 }

// variationViews maps a product's stored variations to the buyer-facing view
// shared by the form block renderer and the restaurant menu (issue #665). The
// caller passes the result of ProductVariations.ListByProduct; a sold-out
// variation (limited stock that has reached zero) is flagged so every renderer
// disables it rather than selling it.
func variationViews(vs []*dbstore.ProductVariation) []variationView {
	if len(vs) == 0 {
		return nil
	}
	out := make([]variationView, 0, len(vs))
	for _, v := range vs {
		out = append(out, variationView{
			ID: v.ID, Name: v.Name, PriceSen: v.PriceSen, Stock: v.Stock,
			SoldOut: soldOutStock(v.Stock),
			Price:   formatPriceSen(v.PriceSen),
		})
	}
	return out
}

// renderBlocks renders a form from its builder blocks snapshot. Returns false
// when the form has no blocks (caller falls back to legacy product layout).
// Submit contract stays identical: POST /f/<slug>/order {client, items[]}.
func (h *PublicHandlers) renderBlocks(w http.ResponseWriter, r *http.Request, f *dbstore.Form) bool {
	raw, err := json.Marshal(f.Config)
	if err != nil {
		return false
	}
	var cfg struct {
		Blocks []struct {
			ID        string     `json:"id"`
			Type      string     `json:"type"`
			Value     string     `json:"value"`
			Src       string     `json:"src"`
			ProductID string     `json:"product_id"`
			Required  bool       `json:"required"`
			Condition *Condition `json:"condition"`
			// Issue #173: per-field settings.
			Placeholder string `json:"placeholder"`
			Description string `json:"description"`
			CSSClass    string `json:"css_class"`
			// Issue #388: per-field inline style (sanitized server-side).
			Style        string `json:"style"`
			MaxLength    int    `json:"max_length"`
			DefaultValue string `json:"default_value"`
			Min          string `json:"min"`
			Max          string `json:"max"`
			Step         string `json:"step"`
			Format       string `json:"format"`
			Pattern      string `json:"pattern"`
			RequiredMsg  string `json:"required_message"`
			PriceSen     int64  `json:"price_sen"`
			// Issue #249: per-product quantity selector flag. Pointer so we can
			// distinguish "absent" (default on) from an explicit merchant "off".
			Quantity *bool `json:"quantity"`
			Choices  []struct {
				Label    string `json:"label"`
				Value    string `json:"value"`
				PriceSen int64  `json:"price_sen"`
			} `json:"choices"`
		} `json:"blocks"`
		Products []productSnapshot `json:"products"`
	}
	if json.Unmarshal(raw, &cfg) != nil || len(cfg.Blocks) == 0 {
		return false
	}

	// Issue #461: resolve each product block's display data (name, price_sen,
	// images) LIVE from the products table, keyed by product_id, instead of the
	// stale config snapshot. The snapshot is only the fallback for an archived
	// or deleted product so a removed product still renders (never blanks).
	resolver := newProductResolver(cfg.Products, h.liveProductsForForm(r.Context(), f))

	// choiceView is one option of a select/radio/checkbox/image-choice field,
	// or one choice of an option/shipping pricing block (where it carries its
	// own price_sen — the template renders it as the per-choice add-on).
	type choiceView struct {
		Label    string
		Value    string
		PriceSen int64
	}

	type blockView struct {
		ID, Type, Value, Src string
		ProductID            string
		PriceSen             int64
		Required             bool
		HasPrice             bool
		CondJSON             string
		// Issue #439: first product image (thumbnail) rendered next to the
		// product name. Empty string = no image (fallback to name only).
		Image string
		// Issue #471: the product image size/layout the merchant chose. ImgSize
		// is the chosen size (a data-size value: small|medium|large) and ImgTop
		// selects the menu-card placement (image above the name/price row).
		ImgSize string
		ImgTop  bool
		// BaseImage is the full-size image URL. The template ships it as a
		// client-side fallback (onerror) for an image uploaded before #382,
		// which has no generated variants.
		BaseImage string
		// Issue #153: variations selector.
		Variations []variationView
		// Issue #173: per-field settings surfaced to the template.
		Placeholder string
		Description string
		CSSClass    string
		// Issue #388: per-field inline style, sanitized to safe CSS only.
		Style        string
		MaxLength    int
		DefaultValue string
		Min, Max     string
		Step         string
		Format       string
		Pattern      string
		RequiredMsg  string
		Choices      []choiceView
		// Issue #249: per-product quantity selector (Gravity-Forms-style).
		HasQuantity bool
	}

	// Issue #471: product image size + layout, read from the form theme.
	// Defaults are the pre-#471 look (small thumbnail, left of the name).
	// Declared here (before the theme map is parsed below) so the per-product
	// loop can resolve each image to the matching media variant.
	themeProductImageSize := ProductImageSizeSmall
	themeProductImageLayout := ProductImageLayoutThumbnailLeft
	if f.Theme != nil {
		if v, ok := f.Theme["product_image_size"].(string); ok {
			themeProductImageSize = productImageSizeOrDefault(v)
		}
		if v, ok := f.Theme["product_image_layout"].(string); ok {
			themeProductImageLayout = productImageLayoutOrDefault(v)
		}
	}
	imgHidden := themeProductImageSize == ProductImageSizeHidden
	imgTop := themeProductImageLayout == ProductImageLayoutImageTop

	blocks := make([]blockView, 0, len(cfg.Blocks))
	for _, b := range cfg.Blocks {
		vb := blockView{
			ID: b.ID, Type: b.Type, Value: b.Value, Src: b.Src, ProductID: b.ProductID, Required: b.Required,
			Placeholder: b.Placeholder, Description: b.Description, CSSClass: b.CSSClass,
			Style:     customform.SanitizeStyle(b.Style),
			MaxLength: b.MaxLength, DefaultValue: b.DefaultValue, Min: b.Min, Max: b.Max,
			Step: b.Step, Format: b.Format, Pattern: b.Pattern, RequiredMsg: b.RequiredMsg,
		}
		vb.CondJSON = marshalCondition(b.Condition)
		for _, c := range b.Choices {
			vb.Choices = append(vb.Choices, choiceView{Label: c.Label, Value: c.Value, PriceSen: c.PriceSen})
		}
		// Pricing fields (product/option/shipping) carry a price. Product
		// blocks resolve price/name/image live from the products table (issue
		// #461, snapshot only as archived fallback); option/shipping blocks
		// carry their own price_sen and stay snapshot-based.
		switch b.Type {
		case "product":
			if b.ProductID != "" {
				// Issue #249: per-product quantity selector. Defaults to on
				// (matches the builder default) unless the merchant disabled it.
				vb.HasQuantity = b.Quantity == nil || *b.Quantity
				liveName, livePrice, liveImage, found := resolver.resolve(b.ProductID)
				if found {
					vb.PriceSen = livePrice
					vb.HasPrice = true
					// Issue #471: resolve the LIVE product image (#461) to the
					// media variant that matches the merchant's chosen size,
					// unless the merchant hid the image. An empty image stays
					// empty (no <img> is rendered).
					if !imgHidden && liveImage != "" {
						vb.Image = productImageForSize(liveImage, themeProductImageSize)
						vb.BaseImage = liveImage
						vb.ImgSize = themeProductImageSize
						vb.ImgTop = imgTop
					}
					// The product name always follows the live product, except
					// when the merchant typed an explicit per-form label that
					// differs from the snapshotted product name (that override
					// is intentional and stays).
					if b.Value == "" || b.Value == resolver.snapshotName(b.ProductID) {
						vb.Value = liveName
					}
				}
				// Issue #153: load the product's variations for the selector.
				if h.ProductVariations != nil {
					if vs, verr := h.ProductVariations.ListByProduct(r.Context(), b.ProductID); verr == nil {
						vb.Variations = variationViews(vs)
					}
				}
			}
		case "option", "shipping":
			if b.PriceSen > 0 {
				vb.PriceSen = b.PriceSen
				vb.HasPrice = true
			}
		}
		blocks = append(blocks, vb)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	globalTr := ""
	if h.Accounts != nil {
		if v, terr := h.Accounts.Tracking(r.Context(), f.AccountID); terr == nil {
			globalTr = v
		}
	}
	// Card t_8f5d6fb5: the merchant's brand colour was declared as
	// `:root { --jf-primary }` (below) and consumed by nothing — the submit CTA
	// was a hard-coded #5e6ad2 and the checkbox/radio accent a hard-coded
	// #4f46e5. The CTA is now painted from the theme, and its hover/active fills
	// are derived from the same hue so a themed button does not get an indigo
	// hover.
	cta := themePrimaryValues(f)
	ls := h.effectiveLangStrings(r.Context(), f)
	view := map[string]any{
		"Slug": f.Slug, "Title": f.Title, "Blocks": blocks,
		"TrackHead":  headInjection(globalTr, formTrackingHead(f)),
		"LogoURL":    f.LogoURL,
		"CtaPrimary": cta.Primary, "CtaHover": cta.Hover, "CtaActive": cta.Active,
		"CtaLabel": cta.Label, "CtaText": cta.Text,
		"Lang": ls.Lang,
		"Ls":   ls,
		// Issue #411: Open Graph + Twitter meta for social sharing.
		"OGMeta": h.formOGMeta(r, f),
		// Issue #183: optional per-workspace footer.
		"Footer": h.workspaceFooter(r.Context(), f),
		// Issue #244: hidden honeypot + render-timestamp fields.
		"AntiSpam": template.HTML(antiSpamRenderFields()),
		// Issue #276: noindex on jomform.com subdomains / when the workspace opts
		// out of indexing.
		"NoIndex": h.formNoIndex(r, f),
		// Issue #456: postMessage auto-height for the iframe embed. No-op on a
		// direct visit (the script only runs when framed).
		"HeightJS": template.HTML(iframeHeightJS),
		// Card t_e2024698: the buyer-visible payment-failure renderer every
		// order surface shares (payfail.go). template.JS so the function body
		// is emitted verbatim.
		"PayFailJS": payFailJS(ls),
		// Card t_9e875bca: the conditional-logic script. Both renderers emit the
		// SAME constant so a payment form and a response form cannot drift.
		"ConditionJS": template.HTML(blockConditionJS),
	}
	// Card t_a7bf0fb2: the theme keys live in ONE function both renderers call
	// (see blockThemeView) — a copy in each renderer is how the custom-CSS
	// escaping bug ended up needing the same fix twice.
	for k, v := range blockThemeView(f) {
		view[k] = v
	}
	if err := blockTmpl.Execute(w, view); err != nil {
		slog.Error("form_render_blocks", "err", err)
	}
	return true
}

// iframeHeightJS reports the rendered page's height to the parent frame so a
// merchant's embed snippet can auto-resize the iframe (issue #456). It is a
// no-op on a direct visit: the whole script returns early unless the page is
// actually framed (window.self !== window.top). It re-posts on load, on
// viewport resize, and on DOM mutation / resize (ResizeObserver + fallback
// MutationObserver) so it tracks dynamic content (conditional fields, product
// selection, the confirmation message after submit). When the parent does not
// listen, nothing changes: the merchant's own iframe CSS height (the
// documented 640px) stays the fallback.
//
// The full <script> wrapper is part of the constant so the renderers can emit
// it verbatim (template.HTML) without html/template escaping the JS. The body
// is a fixed constant with no "</script>" sequence, so it is safe to inline.
const iframeHeightJS = `<script>(function(){
  if(window.self===window.top)return;
  var last=0;
  function post(){
    var de=document.documentElement;
    var h=Math.max(de?de.scrollHeight:0,(document.body?document.body.scrollHeight:0));
    if(h>0&&h!==last){last=h;window.parent.postMessage({type:'jomform:height',height:h},'*');}
  }
  window.addEventListener('load',post);
  window.addEventListener('resize',post);
  if(window.ResizeObserver){try{new ResizeObserver(post).observe(document.documentElement);}catch(e){}}
  if(window.MutationObserver){try{new MutationObserver(post).observe(document.documentElement,{subtree:true,childList:true,attributes:true});}catch(e){}}
  post();
})();</script>`

// blockConditionJS is the client half of the builder's conditional logic. BOTH
// public renderers emit it verbatim (template.HTML) so a payment form and a
// response form behave identically — before card t_9e875bca the payment
// renderer carried a private copy that watched an attribute no template
// emitted, and the response renderer had no script at all.
//
// The block wrapper (the element carrying `data-condition`) is the unit that is
// shown or hidden, and a hidden block's controls are DISABLED rather than merely
// hidden: a `required`, `pattern` or `type=email` constraint on a display:none
// control still runs and blocks the submit in Chrome ("An invalid form control
// with name=… is not focusable"), which is exactly the failure a hidden
// conditional field must not cause. Disabling also keeps the hidden answer out
// of the payload, matching what the server stores
// (formfields.VisibleAnswers).
//
// Both renderers interpolate nothing into this script and the body contains no
// "</script>" sequence, so it is safe to inline.
const blockConditionJS = `<script>(function(){
  var blocks=document.querySelectorAll('[data-condition]');
  if(!blocks.length)return;
  // fieldVal reads the answer a condition references. Every rendered arm tags
  // its own control with the block id (data-id), so that lookup comes first;
  // name/id are the fallback for a hand-written page. A radio or checkbox block
  // is one answer made of many controls: the CHECKED control answers, and a
  // control that belongs to a hidden block (disabled) contributes nothing —
  // conditions are evaluated over the answers the submission will carry.
  function fieldVal(id){
    var els=document.querySelectorAll('[data-id="'+id+'"]');
    if(!els.length)els=document.querySelectorAll('[name="'+id+'"]');
    if(!els.length)return '';
    var el=els[0];
    if(els.length>1||el.type==='radio'||el.type==='checkbox'){
      var group=false;
      for(var i=0;i<els.length;i++){
        var t=els[i].type;
        if(t!=='radio'&&t!=='checkbox')continue;
        group=true;
        if(els[i].disabled)continue;
        if(els[i].checked)return els[i].value;
      }
      if(group)return '';
    }
    if(el.disabled)return '';
    return el.value;
  }
  function evalCond(c){
    var v=String(fieldVal(c.field_id)).trim();
    var w=String(c.value).trim();
    switch(c.op){
      // The Go evaluator (formfields.Condition.Met) is the authority; this
      // mirrors it op for op so the page cannot hide a block the server keeps.
      case 'equals':return c.value===''?v!=='':v.toLowerCase()===w.toLowerCase();
      case 'not_equals':return c.value===''?v==='':v.toLowerCase()!==w.toLowerCase();
      case 'contains':return v.toLowerCase().indexOf(w.toLowerCase())>=0;
      case 'gt':return parseFloat(v)>parseFloat(c.value);
      case 'lt':return parseFloat(v)<parseFloat(c.value);
      case 'not_empty':return v!=='';
    }
    return true;
  }
  function refresh(){
    // Two passes: a condition may reference a field whose own block is
    // conditional, and hiding that block changes the answer this one reads.
    for(var pass=0;pass<2;pass++){
      blocks.forEach(function(el){
        try{
          var show=evalCond(JSON.parse(el.dataset.condition));
          el.style.display=show?'':'none';
          el.querySelectorAll('input,select,textarea').forEach(function(x){x.disabled=!show;});
        }catch(e){}
      });
    }
  }
  document.addEventListener('input',refresh);
  document.addEventListener('change',refresh);
  refresh();
})();</script>`

// blockThemeView is the theme half of both public renderers' view map: the keys
// blockTmpl and responseTmpl interpolate into their <style> / <script> blocks.
//
// It is ONE function, not a copy in each renderer, because the copies are how
// the custom-CSS defect (card t_a7bf0fb2) ended up needing the same diagnosis
// and the same fix twice, in two places that could drift apart with nothing
// red to show for it.
//
// The two context-typed values are the reason the typing is done HERE:
//
//   - ThemeCSS is `template.CSS`, the documented type for known-safe CSS. The
//     value lands inside a `<style>` ELEMENT, where html/template is in CSS
//     text state and runs cssValueFilter. That filter does not honour
//     `template.HTML` — an HTML-context type — so the previous
//     `template.HTML(template.HTMLEscapeString(v))` shipped the literal
//     `ZgotmplZ` sentinel in place of any multi-declaration stylesheet, while
//     every other theme key (background/layout/spacing/font) applied, the page
//     returned 200, and nothing logged. customCSSForStyleBlock carries the
//     breakout guard that raw emission makes necessary.
//
//   - ThemeJS is `template.JS`, the documented type for known-safe JS. The
//     value lands inside a `<script>` ELEMENT, where html/template is in JS
//     state and runs jsValEscaper — and that escaper does not honour
//     `template.HTML`, an HTML-context type: it JSON-marshals it. Measured
//     end-to-end on this path (theme seeded in Postgres, public GET), the
//     previous `template.HTML(template.HTMLEscapeString(v))` shipped —
//
//     <script>
//     "window.__jfMeasured = 1;"
//     </script>
//
//     a quoted string expression, i.e. a valid no-op statement. The page
//     returned 200, nothing logged, and every other theme key applied, so the
//     only signal was that a merchant's `custom_js` never ran on a
//     block-rendered page (card t_cae9c98e). customJSForScriptBlock carries the
//     breakout guard that verbatim emission makes necessary.
//
// Every other key is emitted as a plain string and therefore goes through the
// CSS filter on the way out — which is the safe direction for a single value.
func blockThemeView(f *dbstore.Form) map[string]any {
	view := map[string]any{
		"ThemeBg": "", "ThemeText": "", "ThemeCardBg": "", "ThemeRadius": "",
		"ThemeFont": "", "ThemeLayout": "", "ThemeSpacing": "",
		"ThemeCSS": template.CSS(""), "ThemeJS": template.JS(""),
	}
	if f == nil || f.Theme == nil {
		return view
	}
	// Single-value theme keys: HTML-escaped, because the filter that follows
	// needs a value it can accept (a stray quote/bracket must not survive into
	// the declaration it is interpolated into).
	str := func(key string) string {
		if v, ok := f.Theme[key].(string); ok {
			return template.HTMLEscapeString(v)
		}
		return ""
	}
	view["ThemeBg"] = str("background")
	view["ThemeText"] = str("text")
	view["ThemeCardBg"] = str("card_background")
	view["ThemeRadius"] = str("card_border_radius")
	view["ThemeFont"] = str("font_family")
	view["ThemeLayout"] = str("layout")
	view["ThemeSpacing"] = str("spacing")
	// The merchant's whole stylesheet — the ONE value that must NOT be
	// HTML-escaped, because it is served as CSS, not as text in HTML.
	if v, ok := f.Theme["custom_css"].(string); ok {
		view["ThemeCSS"] = customCSSForStyleBlock(v)
	}
	// Issue #388: power-user custom JS — served as JS, not as text in HTML, so
	// it is the ONE value that must NOT be HTML-escaped.
	if v, ok := f.Theme["custom_js"].(string); ok {
		view["ThemeJS"] = customJSForScriptBlock(v)
	}
	return view
}

// customJSForScriptBlock types the merchant's `custom_js` for the `<script>`
// element it is served in, and neutralises the one sequence that could leave
// that element.
//
// Why a type and not an escape: inside `<script>` html/template is in JS state
// and runs jsValEscaper, which rejects a multi-statement script unless the value
// is typed for that context. `template.JS` is the documented type for known-safe
// JS and jsValEscaper emits it verbatim; `template.HTML` is an HTML-context type
// and was JSON-marshalled into a quoted string literal — a valid no-op statement
// — so a merchant's `custom_js` never ran while the page returned 200 and
// nothing logged (card t_cae9c98e).
//
// Why a guard as well: verbatim means verbatim, so the protection
// HTMLEscapeString gave for free has to be explicit. A `<script>` is an HTML
// *raw text* element — markup is not parsed inside it and the ONLY thing that
// ends it is an end tag, i.e. a case-insensitive `</script`. The template
// follows the value with a newline and its own `</script>`, and `</script` is a
// valid end tag with no `>` after it, so the guard truncates on the bare prefix
// rather than on a full tag. Everything from such a sequence onward is dropped:
// the merchant's code is served up to the first attempt to close its own block
// and never past it, so injected text after it cannot become markup.
//
// Dropping (rather than emitting an escaped `<\/script`) is deliberate and
// matches customCSSForStyleBlock: whatever follows an injected end tag is
// attacker-chosen text rather than the merchant's code, and a script that wants
// a literal `</script` inside a string must write `<\/script` — precisely
// because the browser would have ended the element there too. A value that does
// NOT try to close the block is served byte-for-byte.
func customJSForScriptBlock(js string) template.JS {
	if js == "" {
		return ""
	}
	if i := indexScriptEndTag(js); i >= 0 {
		js = js[:i]
	}
	return template.JS(js)
}

// customCSSForStyleBlock types the merchant's `custom_css` for the `<style>`
// element it is served in, and neutralises the one sequence that could leave
// that element.
//
// Why a type and not an escape: inside `<style>` html/template runs
// cssValueFilter, which rejects a multi-declaration stylesheet and substitutes
// the `ZgotmplZ` sentinel (card t_a7bf0fb2: the page returned 200, the row held
// the CSS, and the buyer got no styling). `template.CSS` is the documented type
// for known-safe CSS; cssValueFilter passes it through verbatim.
//
// Why a guard as well: verbatim means verbatim, so the protection
// HTMLEscapeString gave for free has to be explicit. A `<style>` is an HTML
// *raw text* element — markup is not parsed inside it and the ONLY thing that
// ends it is an end tag, i.e. a case-insensitive `</style`. Note that `</style`
// ends the element even with no `>` after it (the template follows the value
// with a newline and its own `</style>`), so the guard truncates on the bare
// prefix rather than on a full tag. Everything from such a sequence onward is
// dropped: a stylesheet is served up to the first attempt to close its own
// block and never past it, so injected text after it cannot become markup.
// Dropping (rather than emitting an escaped `<\/style`) is deliberate: CSS has
// no portable in-value escape for `/` outside a string, and whatever follows an
// injected end tag is attacker-chosen text rather than the merchant's rules.
//
// A value that does NOT try to close the block is served byte-for-byte, so
// ordinary CSS — selectors, at-rules, strings, url(data:...) — is unaffected.
func customCSSForStyleBlock(css string) template.CSS {
	if css == "" {
		return ""
	}
	if i := indexStyleEndTag(css); i >= 0 {
		css = css[:i]
	}
	return template.CSS(css)
}

// indexStyleEndTag returns the byte offset of the first case-insensitive
// "</style" in s, or -1.
func indexStyleEndTag(s string) int { return indexEndTag(s, "</style") }

// indexScriptEndTag returns the byte offset of the first case-insensitive
// "</script" in s, or -1.
func indexScriptEndTag(s string) int { return indexEndTag(s, "</script") }

// indexEndTag returns the byte offset in s of the first case-insensitive
// occurrence of tag, or -1. tag must start with '<' (both end tags this file
// guards do).
//
// Written out instead of strings.ToLower-then-Index because Unicode case folding
// can change the LENGTH of the string, which would make the offset it reported
// point at the wrong byte in the original — and a guard that truncates at the
// wrong byte does not guard anything.
func indexEndTag(s, tag string) int {
	for i := 0; i+len(tag) <= len(s); i++ {
		if s[i] == tag[0] && strings.EqualFold(s[i:i+len(tag)], tag) {
			return i
		}
	}
	return -1
}

// styleAttr renders a sanitized per-field inline style attribute (issue #388).
// Returns template.HTMLAttr (a trusted attribute) so html/template does not run
// the value through its CSS-context filter (which would emit ZgotmplZ for valid
// CSS). The value is sanitized server-side (customform.SanitizeStyle) and
// HTML-escaped here — one rule, used by both public renderers.
func styleAttr(s string) template.HTMLAttr {
	if s == "" {
		return ""
	}
	return template.HTMLAttr(` style="` + template.HTMLEscapeString(s) + `"`)
}

// dateInputType maps a date/time block's merchant-chosen `format` to the HTML
// input type the buyer gets. The panel offers Date / Date & time / Time; an
// unset format keeps the block's own default (a `date` block is a date picker,
// a `time` block a time picker) — which is what those two blocks rendered when
// the format option had no reader at all.
func dateInputType(blockType, format string) string {
	switch format {
	case "date", "time":
		return format
	case "datetime":
		return "datetime-local"
	}
	if blockType == "time" {
		return "time"
	}
	return "date"
}

// blockTmpl renders each builder block. Prices ride on data-price for the
// client total; the server remains the price authority on submit.
//
// Card t_4086b8f0: the `file` arm is deliberately NOT a file control. It used
// to be `<input type="file">`, and the submit JS drops the picked file on the
// floor ("files not sent as text") — the buyer believed an attachment had been
// sent and the merchant received nothing but the rest of the line items. There
// is no upload path for an anonymous buyer (formfields.UnsupportedBlock has the
// whole argument), so the block renders its label and says so. The write paths
// now refuse a config carrying `file`; this keeps a form stored BEFORE that
// refusal honest on its own live page.
//
// Card t_4975927e: the same treatment for `coupon`, which was withdrawn from
// the palette, from formgen's allowlist and from this renderer. A coupon block
// painted a real text input (`name="coupon"`) whose typed code NOTHING on the
// submit/order path ever applied — order.go still charges
// `payments.SumItems(subtotal, 0, 0)` — so the buyer typed a code, got no error
// and paid full price. There is no coupon table and no server-side validation,
// so the block can only be withdrawn; a form that stored one BEFORE the
// withdrawal renders its label and says the discount is not applied, never an
// input that promises one it cannot honour.
var blockTmpl = template.Must(template.New("blocks").
	Funcs(template.FuncMap{
		"divf": func(total, div int64) float64 { return float64(total) / float64(div) },
		// Card t_23289ce3: the date/time format picker's reader.
		"dateType":  dateInputType,
		"styleAttr": styleAttr,
		// Card t_5c8fd63e: Arabic is the RTL locale (formLang.go isRTL).
		"isRTL": isRTL,
	}).
	Parse(`<!doctype html>
<html lang="{{.Lang}}"{{if isRTL .Lang}} dir="rtl"{{end}}><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
{{if .NoIndex}}<meta name="robots" content="noindex, nofollow">{{end}}
<title>{{.Title}}</title>
{{.OGMeta}}
{{.TrackHead}}
<style>
  /* Card t_8f5d6fb5: the merchant's brand colour lives here as a TOKEN TRIPLE
     and every accent below consumes it. Before this, the ":root { --jf-primary }"
     declaration had no consumer anywhere in api/ — the submit CTA was a
     hard-coded #5e6ad2 and the checkbox/radio accent #4f46e5 — so a merchant who
     picked a colour on /form-settings got the default indigo.
     The triple is resolved server-side (formtheme.go): CtaPrimary is the
     merchant's colour, CtaHover/CtaActive are the same hue and chroma at a
     different OKLCH lightness, each measured so the button's label still clears
     AA 4.5:1, and CtaLabel is the label colour that pairing measured.
     Unconditional, not an if-guard on ThemePrimary: an unthemed form gets the
     same defaults the web CTA uses (#5e6ad2 / #616dd5 / #454ea8), so there is
     exactly one place the button's colour can come from. */
  :root { --jf-primary: {{.CtaPrimary}}; --jf-primary-hover: {{.CtaHover}};
          --jf-primary-active: {{.CtaActive}}; --jf-primary-label: {{.CtaLabel}};
          --jf-primary-text: {{.CtaText}}; }
  {{if .ThemeBg}}body { background: {{.ThemeBg}}; }{{end}}
  {{if .ThemeText}}body { color: {{.ThemeText}}; }{{end}}
  {{if .ThemeFont}}body { font-family: {{.ThemeFont}}; }{{end}}
  {{if .ThemeCardBg}}.card { background: {{.ThemeCardBg}}; }{{end}}
  {{if .ThemeRadius}}.card { border-radius: {{.ThemeRadius}}; }{{end}}
  {{if .ThemeLayout}}body { justify-content: {{if eq .ThemeLayout "full_width"}}flex-start{{else}}center{{end}}; }{{end}}
  {{if .ThemeLayout}}body { padding: {{if eq .ThemeLayout "full_width"}}0{{else}}24px{{end}}; }{{end}}
  {{if .ThemeLayout}}.card { max-width: {{if eq .ThemeLayout "full_width"}}100%{{else}}480px{{end}}; }{{end}}
  {{if .ThemeSpacing}}.card { padding: {{if eq .ThemeSpacing "compact"}}20px{{else if eq .ThemeSpacing "spacious"}}48px{{else}}32px{{end}}; }{{end}}
  {{if .ThemeCSS}}{{.ThemeCSS}}{{end}}
</style>
<style>
  :root { color-scheme: dark; }
  * { box-sizing: border-box; }
  body { margin:0; font-family: system-ui,-apple-system,"Segoe UI",sans-serif;
         background:#f7f8f8; color:#17181c; min-height:100vh;
         display:flex; flex-direction:column; align-items:center; justify-content:center; padding:24px; }
  .logo { max-height:56px; max-width:180px; object-fit:contain; margin:0 auto 12px; display:block; }
  .card { background:#ffffff; border:1px solid #e6e8ec; border-radius:16px;
          max-width:480px; width:100%; padding:32px; }
  h1 { margin:0 0 8px; font-size:24px; letter-spacing:-0.02em; }
  .text { color:#6b7280; font-size:14px; line-height:1.6; margin:0 0 16px; }
  .img { max-width:100%; border-radius:12px; border:1px solid #e6e8ec; margin-bottom:14px; }
  .prod { display:block; padding:0; border:1px solid #e6e8ec; border-radius:14px;
          margin:0 0 12px; background:#ffffff; box-shadow:0 1px 2px rgba(23,24,28,0.04);
          transition:border-color .15s ease, box-shadow .15s ease; }
  .prod:hover { border-color:var(--jf-primary); box-shadow:0 2px 8px rgba(79,70,229,0.12); }
  .prod:active { background:#eef2ff; }
  .prod:has(input[type=checkbox]:checked) { border-color:var(--jf-primary); box-shadow:0 2px 8px rgba(79,70,229,0.12); background:#eef2ff; }
  .prod label { display:flex; gap:12px; align-items:center; width:100%;
                min-height:44px; padding:12px 14px; font-size:15px; color:#17181c;
                cursor:pointer; -webkit-tap-highlight-color:transparent; }
  .prod input[type=checkbox] { width:20px; height:20px; flex:0 0 auto; accent-color:var(--jf-primary); margin:0; }
  /* Issue #439: product image next to the name (menu-item card).
     Issue #471: the size is merchant-configurable — data-size=small (52px,
     default) | medium (96px) | large (160px) — and the layout can be
     thumbnail-left (default) or image-top (menu card). The base rule is the
     52px default so an image with no data-size still renders correctly. */
  .pimg { width:52px; height:52px; flex:0 0 52px; border-radius:10px;
          object-fit:cover; background:#f3f4f6; }
  .pimg[data-size=medium] { width:96px; height:96px; flex-basis:96px; }
  .pimg[data-size=large] { width:160px; height:160px; flex-basis:160px; }
  /* Issue #471: menu-card layout — a full-width image above the name/price
     row (what a restaurant menu wants). */
  .prod.menu-card label { flex-direction:column; align-items:stretch; gap:10px; }
  .prod.menu-card .pimg { width:100%; height:auto; flex-basis:auto; aspect-ratio:16/9; }
  .prod.menu-card .pimg[data-size=medium] { aspect-ratio:3/2; }
  .prod.menu-card .pimg[data-size=large] { aspect-ratio:4/3; }
  .prod.menu-card .prow { display:flex; align-items:center; gap:12px; width:100%; }
  /* Issue #58: ALL checkboxes (product .p and custom .extra) share one bright
     unchecked state + indigo accent. appearance:none + explicit border/background
     overrides UA dark defaults so Android Chrome draws a white unchecked box. */
  input[type=checkbox].extra, input[type=checkbox].p {
    -webkit-appearance:none; -moz-appearance:none; appearance:none;
    accent-color:var(--jf-primary); width:20px; height:20px; flex:0 0 auto;
    border:2px solid #d1d5db; border-radius:5px; background:#fff;
    cursor:pointer; margin:0;
  }
  input[type=checkbox].extra:checked, input[type=checkbox].p:checked {
    border-color:var(--jf-primary); background:var(--jf-primary) url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 20 20'%3E%3Cpath fill='none' stroke='%23fff' stroke-width='3' stroke-linecap='round' stroke-linejoin='round' d='M5 10l3 3l6-6'/%3E%3C/svg%3E") center/14px no-repeat;
  }
  .pname { flex:1; font-weight:500; }
  .price { color:var(--jf-primary-text); font-weight:600; font-size:14px; white-space:nowrap;
           margin-left:auto; text-align:right; }
  /* Issue #153: variation selector inside a product block. */
  .varwrap { padding:0 14px 12px; }
  .varwrap select { width:100%; padding:9px 10px; border-radius:8px;
           border:1px solid #e6e8ec; background:#ffffff; color:#17181c; font-size:14px; }
  .varstock { font-size:12px; color:#6b7280; margin-top:6px; }
  .lbl { display:block; font-size:12px; color:#6b7280; margin:12px 0 4px; }
  .desc { display:block; font-size:12px; color:#9ca3af; margin:-2px 0 6px; }
  .chk { display:flex; align-items:center; gap:8px; font-size:14px; margin:8px 0; color:#374151; }
  .chk input[type=checkbox] { margin:0; }
  .rad { display:flex; align-items:center; gap:8px; font-size:14px; margin:8px 0; color:#374151; }
  .rad input[type=radio] { margin:0; accent-color:var(--jf-primary); }
  input[type=text],input[type=email],input[type=tel],input[type=number],
  input[type=date],input[type=time],input[type=url],input[type=password],
  select,textarea {
    width:100%; padding:10px 12px; border-radius:8px; border:1px solid #e6e8ec;
    background:#ffffff; color:#17181c; font-size:14px; margin-bottom:10px; }
  select { appearance:auto; }
  .qtyrow { display:flex; align-items:center; gap:10px; margin-bottom:10px; }
  .qtyrow input[type=number] { width:80px; margin-bottom:0; }
  /* Issue #249: per-product quantity selector inside each product block. */
  .pqrow { display:flex; align-items:center; gap:10px; padding:0 14px 12px; }
  .pqrow input[type=number] { width:80px; margin-bottom:0; }
  .section { font-size:18px; font-weight:600; margin:18px 0 6px; color:#17181c; }
  hr { border:0; border-top:1px solid rgba(255,255,255,0.08); margin:16px 0; }
  .total { display:flex; justify-content:space-between; padding-top:12px; margin-top:8px;
           border-top:1px dashed rgba(255,255,255,0.12); font-weight:600; }
  /* The submit CTA (card t_8f5d6fb5). Fill, hover and pressed all come from the
     form's brand triple declared in the :root block above; the LABEL colour is
     the one the derived pairing measured, so a light brand colour gets dark text
     instead of a white label it cannot carry at AA. The three states are the
     same construction .jf-btn-primary uses on the dashboard: rest -> hover
     (lighter for indigo, darker for a light brand) -> pressed (darker), with a
     shadow on hover so the hover is visible even before it is read as a colour. */
  button { width:100%; padding:12px; border:0; border-radius:10px; font-size:15px; font-weight:600;
           color:var(--jf-primary-label); cursor:pointer; margin-top:12px;
           background:var(--jf-primary);
           transition:background .15s ease, box-shadow .15s ease; }
  button:hover { background:var(--jf-primary-hover); box-shadow:0 2px 8px rgba(23,24,28,0.12); }
  button:active { background:var(--jf-primary-active); }
  /* Issue #183: optional per-workspace footer. */
  .jf-footer { max-width:480px; width:100%; margin:16px auto 0; padding:0 8px;
               color:#6b7280; font-size:12px; line-height:1.6; text-align:center;
               align-self:center; }
  .jf-fbiz { font-weight:600; color:#374151; }
  .jf-ftext { margin-top:4px; }
  .jf-flinks { display:flex; width:100%; gap:14px; justify-content:center;
               flex-wrap:wrap; margin-top:6px; text-align:center; }
  .jf-flinks a { color:var(--jf-primary-text); text-decoration:none; }
  .jf-flinks a:hover { text-decoration:underline; }
</style></head><body>
<form class="card" id="of" data-slug="{{.Slug}}">
  <h1>{{.Title}}</h1>
{{if .LogoURL}}<img class="logo" src="{{.LogoURL}}" alt="">{{end}}
{{range $b := .Blocks}}{{if .CondJSON}}<div class="jf-block" data-condition="{{.CondJSON}}">{{end}}
{{if eq .Type "text"}}  <label class="lbl"{{styleAttr .Style}}>{{.Value}}{{if .Required}} {{$.Ls.Required}}{{end}}</label><input type="text" name="{{.ID}}" data-id="{{.ID}}"{{if .CSSClass}} class="{{.CSSClass}}"{{end}}{{if .Placeholder}} placeholder="{{.Placeholder}}"{{end}}{{if .MaxLength}} maxlength="{{.MaxLength}}"{{end}}{{if .DefaultValue}} value="{{.DefaultValue}}"{{end}}{{if .Pattern}} pattern="{{.Pattern}}"{{end}}{{if .Required}} required{{end}}{{if .RequiredMsg}} oninvalid="this.setCustomValidity('{{.RequiredMsg}}')" oninput="this.setCustomValidity('')"{{end}}>
{{else if eq .Type "image"}}{{if .Src}}  <img class="img{{if .CSSClass}} {{.CSSClass}}{{end}}" src="{{.Src}}" alt=""{{styleAttr .Style}}>
{{end}}{{else if eq .Type "divider"}}  <hr{{if .CSSClass}} class="{{.CSSClass}}"{{end}}{{styleAttr .Style}}>
{{else if eq .Type "section"}}  <div class="section{{if .CSSClass}} {{.CSSClass}}{{end}}"{{styleAttr .Style}}>{{.Value}}</div>
{{else if eq .Type "html"}}  <div class="text{{if .CSSClass}} {{.CSSClass}}{{end}}"{{styleAttr .Style}}>{{.Value}}</div>
{{else if eq .Type "total"}}  <div class="total{{if .CSSClass}} {{.CSSClass}}{{end}}"{{styleAttr .Style}}><span>{{.Value}}</span><span class="total-val">RM0.00</span></div>
{{else if eq .Type "product"}}  <div class="prod{{if .ImgTop}} menu-card{{end}}{{if .CSSClass}} {{.CSSClass}}{{end}}"{{styleAttr .Style}}><label>{{if .ImgTop}}{{if .Image}}<img class="pimg" data-size="{{.ImgSize}}" src="{{.Image}}" alt="{{.Value}}" data-base="{{.BaseImage}}">{{end}}<span class="prow">{{end}}<input type="checkbox" class="p" data-id="{{.ProductID}}" data-price="{{.PriceSen}}">{{if not .ImgTop}}{{if .Image}}<img class="pimg" data-size="{{.ImgSize}}" src="{{.Image}}" alt="{{.Value}}" data-base="{{.BaseImage}}">{{end}}{{end}}<span class="pname">{{.Value}}</span><span class="price">{{if .HasPrice}}RM{{printf "%.2f" (divf .PriceSen 100)}}{{end}}</span>{{if .ImgTop}}</span>{{end}}</label>{{if .Variations}}<div class="varwrap"><select class="v" data-id="{{.ProductID}}"><option value="" disabled selected>{{$.Ls.SelectVariation}}</option>{{range .Variations}}<option value="{{.ID}}" data-price="{{.PriceSen}}"{{if .SoldOut}} disabled{{end}}>{{.Name}} — RM{{printf "%.2f" (divf .PriceSen 100)}}{{if .Stock}} ({{.Stock}} {{$.Ls.InStock}}){{end}}</option>{{end}}</select><div class="varstock"></div></div>{{end}}{{if .HasQuantity}}<div class="pqrow"><label class="lbl" style="margin:0">{{$.Ls.Quantity}}</label><input type="number" class="pq" data-id="{{.ProductID}}" min="1" value="1"></div>{{end}}</div>
{{else if eq .Type "quantity"}}  <div class="qtyrow{{if .CSSClass}} {{.CSSClass}}{{end}}"{{styleAttr .Style}}><label class="lbl" style="margin:0">{{.Value}}</label><input type="number" class="qty" min="1" value="{{if .DefaultValue}}{{.DefaultValue}}{{else}}1{{end}}"></div>
{{else if eq .Type "option"}}  <label class="lbl"{{styleAttr .Style}}>{{.Value}}{{if .Required}} {{$.Ls.Required}}{{end}}</label><select class="opt{{if .CSSClass}} {{.CSSClass}}{{end}}" data-id="{{.ProductID}}"{{if .Required}} required{{end}}{{if .RequiredMsg}} oninvalid="this.setCustomValidity('{{.RequiredMsg}}')" oninput="this.setCustomValidity('')"{{end}}><option value="" selected>{{$.Ls.SelectOption}}</option>{{range .Choices}}<option value="{{.Value}}" data-price="{{.PriceSen}}">{{.Label}}{{if .PriceSen}} (+RM{{printf "%.2f" (divf .PriceSen 100)}}){{end}}</option>{{end}}</select>
{{else if eq .Type "shipping"}}  <label class="lbl"{{styleAttr .Style}}>{{.Value}}{{if .Required}} {{$.Ls.Required}}{{end}}</label><select class="ship{{if .CSSClass}} {{.CSSClass}}{{end}}" data-id="{{.ProductID}}"{{if .Required}} required{{end}}{{if .RequiredMsg}} oninvalid="this.setCustomValidity('{{.RequiredMsg}}')" oninput="this.setCustomValidity('')"{{end}}><option value="" selected>{{$.Ls.SelectShipping}}</option>{{range .Choices}}<option value="{{.Value}}" data-price="{{.PriceSen}}">{{.Label}}{{if .PriceSen}} (RM{{printf "%.2f" (divf .PriceSen 100)}}){{end}}</option>{{end}}</select>
{{else if eq .Type "name"}}  <label class="lbl"{{styleAttr .Style}}>{{.Value}}{{if .Required}} {{$.Ls.Required}}{{end}}</label><input type="text" name="full_name" data-id="{{.ID}}"{{if .CSSClass}} class="{{.CSSClass}}"{{end}}{{if .Required}} required{{end}}{{if .RequiredMsg}} oninvalid="this.setCustomValidity('{{.RequiredMsg}}')" oninput="this.setCustomValidity('')"{{end}}>
{{else if eq .Type "email"}}  <label class="lbl"{{styleAttr .Style}}>{{$.Ls.EmailLabel}}{{if .Required}} {{$.Ls.Required}}{{end}}</label><input type="email" name="email" data-id="{{.ID}}"{{if .CSSClass}} class="{{.CSSClass}}"{{end}}{{if .Placeholder}} placeholder="{{.Placeholder}}"{{end}}{{if .Pattern}} pattern="{{.Pattern}}"{{end}} required{{if .RequiredMsg}} oninvalid="this.setCustomValidity('{{.RequiredMsg}}')" oninput="this.setCustomValidity('')"{{end}}>
{{else if eq .Type "phone"}}  <label class="lbl"{{styleAttr .Style}}>{{.Value}}{{if .Required}} {{$.Ls.Required}}{{end}}</label><input type="tel" name="phone" data-id="{{.ID}}"{{if .CSSClass}} class="{{.CSSClass}}"{{end}}{{if .Placeholder}} placeholder="{{.Placeholder}}"{{end}}{{if .Pattern}} pattern="{{.Pattern}}"{{end}}{{if .Required}} required{{end}}{{if .RequiredMsg}} oninvalid="this.setCustomValidity('{{.RequiredMsg}}')" oninput="this.setCustomValidity('')"{{end}}>
{{else if eq .Type "textarea"}}  <label class="lbl"{{styleAttr .Style}}>{{.Value}}{{if .Required}} {{$.Ls.Required}}{{end}}</label><textarea name="notes" data-id="{{.ID}}"{{if .CSSClass}} class="{{.CSSClass}}"{{end}} rows="2"{{if .Placeholder}} placeholder="{{.Placeholder}}"{{end}}{{if .MaxLength}} maxlength="{{.MaxLength}}"{{end}}{{if .Required}} required{{end}}{{if .RequiredMsg}} oninvalid="this.setCustomValidity('{{.RequiredMsg}}')" oninput="this.setCustomValidity('')"{{end}}>{{.DefaultValue}}</textarea>
{{else if eq .Type "number"}}  <label class="lbl"{{styleAttr .Style}}>{{.Value}}{{if .Required}} {{$.Ls.Required}}{{end}}</label><input type="number" name="notes" data-id="{{.ID}}"{{if .CSSClass}} class="{{.CSSClass}}"{{end}}{{if .Placeholder}} placeholder="{{.Placeholder}}"{{end}}{{if .Min}} min="{{.Min}}"{{end}}{{if .Max}} max="{{.Max}}"{{end}}{{if .Step}} step="{{.Step}}"{{end}}{{if .Required}} required{{end}}{{if .RequiredMsg}} oninvalid="this.setCustomValidity('{{.RequiredMsg}}')" oninput="this.setCustomValidity('')"{{end}}>
{{else if eq .Type "select"}}  <label class="lbl"{{styleAttr .Style}}>{{.Value}}{{if .Required}} {{$.Ls.Required}}{{end}}</label><select name="notes" data-id="{{.ID}}"{{if .CSSClass}} class="{{.CSSClass}}"{{end}}{{if .Required}} required{{end}}{{if .RequiredMsg}} oninvalid="this.setCustomValidity('{{.RequiredMsg}}')" oninput="this.setCustomValidity('')"{{end}}><option value="" disabled selected>{{$.Ls.SelectOption}}</option>{{range .Choices}}<option value="{{.Value}}">{{.Label}}</option>{{end}}</select>
{{else if eq .Type "radio"}}  <label class="lbl"{{styleAttr .Style}}>{{.Value}}{{if .Required}} {{$.Ls.Required}}{{end}}</label>{{range .Choices}}<label class="rad{{if $b.CSSClass}} {{$b.CSSClass}}{{end}}"><input type="radio" name="notes" data-id="{{$b.ID}}" value="{{.Value}}"{{if $b.Required}} required{{end}}{{if $b.RequiredMsg}} oninvalid="this.setCustomValidity('{{$b.RequiredMsg}}')" oninput="this.setCustomValidity('')"{{end}}> {{.Label}}</label>{{end}}
{{else if eq .Type "checkbox"}}  <label class="chk"{{styleAttr .Style}}><input type="checkbox" class="extra{{if .CSSClass}} {{.CSSClass}}{{end}}" data-id="{{.ID}}" value="{{.Value}}"{{if .Required}} required{{end}}{{if .RequiredMsg}} oninvalid="this.setCustomValidity('{{.RequiredMsg}}')" oninput="this.setCustomValidity('')"{{end}}> {{.Value}}</label>
{{else if eq .Type "date"}}  <label class="lbl"{{styleAttr .Style}}>{{.Value}}{{if .Required}} {{$.Ls.Required}}{{end}}</label><input type="{{dateType .Type .Format}}" name="notes" data-id="{{.ID}}"{{if .CSSClass}} class="{{.CSSClass}}"{{end}}{{if .Required}} required{{end}}{{if .RequiredMsg}} oninvalid="this.setCustomValidity('{{.RequiredMsg}}')" oninput="this.setCustomValidity('')"{{end}}>
{{else if eq .Type "time"}}  <label class="lbl"{{styleAttr .Style}}>{{.Value}}{{if .Required}} {{$.Ls.Required}}{{end}}</label><input type="{{dateType .Type .Format}}" name="notes" data-id="{{.ID}}"{{if .CSSClass}} class="{{.CSSClass}}"{{end}}{{if .Required}} required{{end}}{{if .RequiredMsg}} oninvalid="this.setCustomValidity('{{.RequiredMsg}}')" oninput="this.setCustomValidity('')"{{end}}>
{{else if eq .Type "website"}}  <label class="lbl"{{styleAttr .Style}}>{{.Value}}{{if .Required}} {{$.Ls.Required}}{{end}}</label><input type="url" name="notes" data-id="{{.ID}}"{{if .CSSClass}} class="{{.CSSClass}}"{{end}}{{if .Placeholder}} placeholder="{{.Placeholder}}"{{end}}{{if .Pattern}} pattern="{{.Pattern}}"{{end}}{{if .Required}} required{{end}}{{if .RequiredMsg}} oninvalid="this.setCustomValidity('{{.RequiredMsg}}')" oninput="this.setCustomValidity('')"{{end}}>
{{else if eq .Type "address"}}  <label class="lbl"{{styleAttr .Style}}>{{.Value}}{{if .Required}} {{$.Ls.Required}}{{end}}</label><textarea name="notes" data-id="{{.ID}}"{{if .CSSClass}} class="{{.CSSClass}}"{{end}} rows="2"{{if .Placeholder}} placeholder="{{.Placeholder}}"{{end}}{{if .Required}} required{{end}}{{if .RequiredMsg}} oninvalid="this.setCustomValidity('{{.RequiredMsg}}')" oninput="this.setCustomValidity('')"{{end}}></textarea>
{{else if eq .Type "file"}}  <label class="lbl"{{styleAttr .Style}}>{{.Value}}</label><div class="desc">{{$.Ls.FileUnsupported}}</div>
{{else if eq .Type "consent"}}  <label class="chk"{{styleAttr .Style}}><input type="checkbox" class="extra{{if .CSSClass}} {{.CSSClass}}{{end}}" data-id="{{.ID}}" value="1"{{if .Required}} required{{end}}{{if .RequiredMsg}} oninvalid="this.setCustomValidity('{{.RequiredMsg}}')" oninput="this.setCustomValidity('')"{{end}}> {{.Value}}</label>
{{else if eq .Type "hidden"}}  <input type="hidden" name="notes" data-id="{{.ID}}" value="{{.DefaultValue}}"{{if .CSSClass}} class="{{.CSSClass}}"{{end}}{{styleAttr .Style}}>
{{else if eq .Type "list"}}  <label class="lbl"{{styleAttr .Style}}>{{.Value}}{{if .Required}} {{$.Ls.Required}}{{end}}</label><textarea name="notes" data-id="{{.ID}}"{{if .CSSClass}} class="{{.CSSClass}}"{{end}} rows="3"{{if .Placeholder}} placeholder="{{.Placeholder}}"{{end}}{{if .Required}} required{{end}}{{if .RequiredMsg}} oninvalid="this.setCustomValidity('{{.RequiredMsg}}')" oninput="this.setCustomValidity('')"{{end}}></textarea>
{{else if eq .Type "repeater"}}  <label class="lbl"{{styleAttr .Style}}>{{.Value}}{{if .Required}} {{$.Ls.Required}}{{end}}</label><textarea name="notes" data-id="{{.ID}}"{{if .CSSClass}} class="{{.CSSClass}}"{{end}} rows="3"{{if .Placeholder}} placeholder="{{.Placeholder}}"{{end}}{{if .Required}} required{{end}}{{if .RequiredMsg}} oninvalid="this.setCustomValidity('{{.RequiredMsg}}')" oninput="this.setCustomValidity('')"{{end}}></textarea>
{{else if eq .Type "image-choice"}}  <label class="lbl"{{styleAttr .Style}}>{{.Value}}{{if .Required}} {{$.Ls.Required}}{{end}}</label>{{range .Choices}}<label class="rad{{if $b.CSSClass}} {{$b.CSSClass}}{{end}}"><input type="radio" name="notes" data-id="{{$b.ID}}" value="{{.Value}}"{{if $b.Required}} required{{end}}{{if $b.RequiredMsg}} oninvalid="this.setCustomValidity('{{$b.RequiredMsg}}')" oninput="this.setCustomValidity('')"{{end}}> {{.Label}}</label>{{end}}
{{else if eq .Type "captcha"}}  <label class="lbl"{{styleAttr .Style}}>{{.Value}}{{if .Required}} {{$.Ls.Required}}{{end}}</label><input type="text" name="notes" data-id="{{.ID}}"{{if .CSSClass}} class="{{.CSSClass}}"{{end}}{{if .Placeholder}} placeholder="{{.Placeholder}}"{{end}}{{if .Required}} required{{end}}{{if .RequiredMsg}} oninvalid="this.setCustomValidity('{{.RequiredMsg}}')" oninput="this.setCustomValidity('')"{{end}}>
{{else if eq .Type "coupon"}}  <label class="lbl"{{styleAttr .Style}}>{{.Value}}</label><div class="desc">{{$.Ls.CouponUnsupported}}</div>
{{end}}</span>{{if .Description}}<div class="desc">{{.Description}}</div>{{end}}{{if .CondJSON}}</div>{{end}}
{{end}}
  <div class="total"><span>{{$.Ls.Total}}</span><span id="total">RM0.00</span></div>
  {{.AntiSpam}}
  <button type="submit" form="of">{{$.Ls.Submit}}</button>
</form>
{{if .Footer.HasFooter}}<footer class="jf-footer">
  {{if .Footer.BusinessName}}<div class="jf-fbiz">{{.Footer.BusinessName}}{{if .Footer.CompanyReg}} · {{.Footer.CompanyReg}}{{end}}</div>{{end}}
  {{if .Footer.ContactPhone}}<div class="jf-frow">{{.Footer.ContactPhone}}</div>{{end}}
  {{if .Footer.ContactAddress}}<div class="jf-frow">{{.Footer.ContactAddress}}</div>{{end}}
  {{if .Footer.FooterText}}<div class="jf-ftext">{{.Footer.FooterText}}</div>{{end}}
  {{if or .Footer.PrivacyURL .Footer.TermsURL .Footer.RefundURL}}<div class="jf-flinks">
    {{if .Footer.PrivacyURL}}<a href="{{.Footer.PrivacyURL}}" target="_blank" rel="noopener">{{$.Ls.PrivacyLabel}}</a>{{end}}
    {{if .Footer.TermsURL}}<a href="{{.Footer.TermsURL}}" target="_blank" rel="noopener">{{$.Ls.TermsLabel}}</a>{{end}}
    {{if .Footer.RefundURL}}<a href="{{.Footer.RefundURL}}" target="_blank" rel="noopener">{{$.Ls.RefundLabel}}</a>{{end}}
  </div>{{end}}
</footer>{{end}}
<script>{{.PayFailJS}}</script>
<script>
(function(){
  var boxes=document.querySelectorAll('.p');
  // Issue #471: product image variant fallback. When the merchant chose a
  // larger size the <img> points at the medium/full media variant (#382). An
  // image uploaded before #382 has no generated variants, so if that variant
  // object 404s we fall back to the base image (data-base) — one attempt only,
  // so a genuinely broken URL cannot loop.
  document.querySelectorAll('img.pimg[data-base]').forEach(function(im){
    im.addEventListener('error',function(){
      var base=im.getAttribute('data-base');
      if(base&&im.getAttribute('src')!==base){im.setAttribute('src',base);}
    },{once:true});
  });
  // Issue #153: variation selectors. Each variable product has a <select class="v">
  // whose options carry data-price; sold-out options are disabled server-side.
  var sels=document.querySelectorAll('select.v');
  // Issue #173: pricing fields — option add-ons, shipping.
  // Issue #249: quantity is per-product. Each product block carries its own
  // <input class="pq" data-id="..."> selector; there is no global quantity.
  var opts=document.querySelectorAll('select.opt');
  var shipEl=document.querySelector('select.ship');
  function effPrice(b){
    var sel=document.querySelector('select.v[data-id="'+b.dataset.id+'"]');
    if(sel&&sel.value){var o=sel.options[sel.selectedIndex];if(o&&o.dataset.price)return Number(o.dataset.price);}
    return Number(b.dataset.price||0);
  }
  function pqty(b){
    var el=document.querySelector('input.pq[data-id="'+b.dataset.id+'"]');
    if(el){var q=parseInt(el.value,10); return (q>0)?q:1;}
    return 1;
  }
  function addons(){
    var t=0;
    opts.forEach(function(s){var o=s.options[s.selectedIndex];if(o&&o.dataset.price)t+=Number(o.dataset.price);});
    if(shipEl){var so=shipEl.options[shipEl.selectedIndex];if(so&&so.dataset.price)t+=Number(so.dataset.price);}
    return t;
  }
  function upd(){var t=0;boxes.forEach(function(b){if(b.checked)t+=effPrice(b)*pqty(b)});t+=addons();
    var s='RM'+(t/100).toFixed(2);
    var tv=document.querySelectorAll('.total-val');tv.forEach(function(el){el.textContent=s});
    var t1=document.getElementById('total');if(t1)t1.textContent=s;}
  boxes.forEach(function(b){b.addEventListener('change',upd)});
  sels.forEach(function(s){s.addEventListener('change',upd)});
  document.querySelectorAll('input.pq').forEach(function(el){el.addEventListener('input',upd)});
  opts.forEach(function(s){s.addEventListener('change',upd)});
  if(shipEl)shipEl.addEventListener('change',upd);
  document.getElementById('of').addEventListener('submit',function(e){
    e.preventDefault();
    var items=[];var ok=true;
    boxes.forEach(function(b){
      if(!b.checked)return;
      var sel=document.querySelector('select.v[data-id="'+b.dataset.id+'"]');
      var item={product_id:b.dataset.id,quantity:pqty(b)};
      if(sel){
        if(!sel.value){ok=false;return;}
        item.variation_id=sel.value;
      }
      items.push(item);
    });
    // Pricing add-ons: option + shipping selections become line items whose
    // price the server resolves from the block config (server is authority).
    opts.forEach(function(s){
      var o=s.options[s.selectedIndex];
      if(o&&o.value)items.push({product_id:s.dataset.id,quantity:1});
    });
    if(shipEl&&shipEl.value)items.push({product_id:shipEl.dataset.id,quantity:1});
    if(!items.length){alert('{{.Ls.NoProductAlert}}');return;}
    if(!ok){alert('{{.Ls.SelectVariationAlert}}');return;}
    // Issue #186: collect every input block's value keyed by its block id so
    // the server can resolve the merchant's chip_mapping (CHIP client field →
    // form block). The client object stays for backward compatibility.
    var fields={};
    document.querySelectorAll('[data-id]').forEach(function(el){
      if(!el.dataset.id)return;
      // A control inside a HIDDEN conditional block is disabled by the
      // conditional-logic script, and its answer is not part of this
      // submission (the server drops it too, formfields.VisibleAnswers).
      if(el.disabled)return;
      if(el.type==='checkbox'){if(el.checked)fields[el.dataset.id]=el.value;}
      else if(el.type==='radio'){if(el.checked)fields[el.dataset.id]=el.value;}
      else if(el.type==='file'){/* files not sent as text */}
      else{fields[el.dataset.id]=el.value;}
    });
    var body={client:{full_name:(e.target.full_name||{}).value||(document.querySelector('[name=full_name]')||{}).value||'',
      email:(document.querySelector('[name=email]')||{}).value||'',
      phone:(document.querySelector('[name=phone]')||{}).value||''},
      fields:fields,items:items,payment_method:'chip',
      // Issue #444: affiliate marketing — echo the ?aff=CODE the buyer landed
      // with so the sale is attributed to that affiliate.
      affiliate_code:(new URLSearchParams(location.search).get('aff')||''),
      // Issue #489: QR ordering — echo the ?table=N the QR stand / menu page
      // carried into the form URL so sales.table_number is populated.
      table_number:(new URLSearchParams(location.search).get('table')||'')};
    fetch(location.pathname+'/order',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)})
      .then(function(r){return r.json()}).then(function(d){
        // Card t_e2024698: the payment could not be started. Paint it in-app —
        // the api answers this state with a status the edge passes through, so
        // this branch is what the buyer actually reads. Anything else that is
        // not a checkout keeps the old alert.
        if(d&&d.status==='payment_error'){jfPayFail(d);return;}
        if(d.checkout_url||d.next){
          var url=d.checkout_url||d.next;
          // Issue #60: when embedded in a merchant site, CHIP checkout and the
          // /t/ thank-you page refuse to frame (X-Frame-Options/anti-clickjacking).
          // Escape to the TOP frame so the buyer completes payment in their own tab;
          // direct visits keep today's same-frame navigation.
          if(window.self!==window.top){window.top.location.href=url;}
          else{location.href=url;}
        }
        else{alert('{{.Ls.ErrorAlert}}'+(d.error?': '+d.error:''));}})
      // Card t_e2024698: without this, a non-JSON answer (an edge error page, a
      // proxy 5xx) made r.json() reject and the buyer's click did NOTHING — no
      // message at all. Any unreadable answer is reported as the same
      // actionable failure.
      .catch(function(){jfPayFail({});});
  });
})();
</script>
{{.ConditionJS}}
{{if .ThemeJS}}<script>
{{.ThemeJS}}
</script>
{{end}}
{{.HeightJS}}
</body></html>`))

// formTrackingHead extracts per-form tracking_head from config JSON.
func formTrackingHead(f *dbstore.Form) string {
	raw, err := json.Marshal(f.Config)
	if err != nil {
		return ""
	}
	var cfg struct {
		TrackingHead string `json:"tracking_head"`
	}
	_ = json.Unmarshal(raw, &cfg)
	return cfg.TrackingHead
}

// renderResponseForm renders a response-only form (issue #171): input blocks
// only, a submit button, no product list and no payment step. On submit the
// buyer's field values are POSTed to /order as {client, fields:{block_id:value}}
// and the server stores a submission.
//
// A response form with ZERO blocks is still a response form, and it is rendered
// here (card t_2612f421). The guard used to return false on an empty block
// list, "so the caller falls back to the legacy layout" — but the only layout
// that fallback can reach is the legacy PAYMENT template, whose submit handler
// refuses to post unless a product is ticked. A response form has no products
// by design, so the served page was a "Proceed to Payment" button whose only
// reachable outcome was the NoProductAlert: no request, no submission, no way
// for the visitor to respond. The page for an empty config is therefore the
// response layout with its title, its submit button and no fields to fill;
// false is returned only when the config cannot be read at all, which a
// JSON-decoded map cannot trigger.
func (h *PublicHandlers) renderResponseForm(w http.ResponseWriter, r *http.Request, f *dbstore.Form) bool {
	raw, err := json.Marshal(f.Config)
	if err != nil {
		return false
	}
	var cfg struct {
		Blocks []struct {
			ID        string     `json:"id"`
			Type      string     `json:"type"`
			Value     string     `json:"value"`
			Src       string     `json:"src"` // image blocks (card t_99c3a08b)
			Required  bool       `json:"required"`
			Condition *Condition `json:"condition"`
			// Issue #173: per-field settings.
			Placeholder string `json:"placeholder"`
			Description string `json:"description"`
			// Card t_23289ce3: per-field settings this template never read.
			CSSClass     string `json:"css_class"`
			Style        string `json:"style"`
			Format       string `json:"format"`
			RequiredMsg  string `json:"required_message"`
			MaxLength    int    `json:"max_length"`
			DefaultValue string `json:"default_value"`
			Min          string `json:"min"`
			Max          string `json:"max"`
			Step         string `json:"step"`
			Pattern      string `json:"pattern"`
			Choices      []struct {
				Label string `json:"label"`
				Value string `json:"value"`
			} `json:"choices"`
		} `json:"blocks"`
	}
	// Card t_2612f421: an EMPTY block list is not a reason to decline — the
	// caller would fall through to the legacy PAYMENT layout, which a response
	// form can never submit. See the function comment.
	if json.Unmarshal(raw, &cfg) != nil {
		return false
	}

	type choiceView struct {
		Label string
		Value string
	}
	type fieldView struct {
		ID, Type, Value string
		// Src is an image block's validated https URL (card t_99c3a08b).
		Src         string
		Required    bool
		CondJSON    string
		Placeholder string
		Description string
		// Card t_23289ce3: the per-field settings the response page now paints.
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
	fields := make([]fieldView, 0, len(cfg.Blocks))
	for _, b := range cfg.Blocks {
		// Card t_99c3a08b: every block the merchant placed is emitted, in the
		// merchant's own order. A block is either an INPUT the responder fills
		// in (collectable — the submitted value round-trips as
		// {block_id: value}) or STATIC content that carries no answer. The
		// switch below used to drop the static group AND `text`, so a response
		// form whose only block was `text` rendered as a heading, a Submit
		// button and nothing else — the shape Wan reported on
		// test-wan.jomform.com/new-form-ddb3.
		switch b.Type {
		// Input blocks — rendered as a control and collected on submit.
		case "text", "name", "email", "phone", "textarea", "checkbox", "number",
			"select", "radio", "date", "time", "website", "address",
			"consent", "hidden", "list", "repeater", "image-choice", "captcha":
			fv := fieldView{
				ID: b.ID, Type: b.Type, Value: b.Value, Required: b.Required,
				CondJSON:    marshalCondition(b.Condition),
				Placeholder: b.Placeholder, Description: b.Description,
				CSSClass: b.CSSClass, Style: customform.SanitizeStyle(b.Style),
				Format: b.Format, RequiredMsg: b.RequiredMsg,
				MaxLength: b.MaxLength, DefaultValue: b.DefaultValue,
				Min: b.Min, Max: b.Max, Step: b.Step, Pattern: b.Pattern,
			}
			for _, c := range b.Choices {
				fv.Choices = append(fv.Choices, choiceView{Label: c.Label, Value: c.Value})
			}
			fields = append(fields, fv)
		// Static blocks — rendered exactly as the payment renderer renders
		// them (render_blocks.go:blockTmpl), never collected. `text` is NOT
		// one of them: the builder's "Text / label" block is the palette's
		// only single-line text input (placeholder, max length, default value
		// and pattern are offered for it), so it is registered above and
		// painted by responseTmpl's own `text` arm.
		case "section", "html", "image", "divider":
			fields = append(fields, fieldView{ID: b.ID, Type: b.Type, Value: b.Value, Src: b.Src})
		// Card t_4086b8f0: `file` is neither an input this page can collect nor
		// static content, and it must NOT be dropped silently — that is the
		// whole defect. The write path now refuses a config carrying it
		// (formfields.UnsupportedBlock, because no upload path exists for an
		// anonymous buyer), but a form stored BEFORE that refusal can still be
		// served; it renders the merchant's label and says the attachment
		// cannot be collected, so the buyer never picks a file that would be
		// thrown away.
		case "file":
			fields = append(fields, fieldView{ID: b.ID, Type: b.Type, Value: b.Value})
		// Card t_4975927e: same rule for the withdrawn `coupon` block. It is
		// not collectable and there is no discount to apply, but a form stored
		// before the block was withdrawn must not lose the merchant's block
		// silently — responseTmpl paints its label plus the "not applied" note.
		case "coupon":
			fields = append(fields, fieldView{ID: b.ID, Type: b.Type, Value: b.Value})
		// Card t_ec5fdf4b: the pricing blocks are the ONE group this page
		// deliberately does not paint, and the case is written out so the
		// decision is in the code rather than in the absence of an arm. Each
		// one is meaningless without the payment step (a price, a quantity, an
		// add-on, a shipping choice, an order total), so a response page has
		// nothing honest to render for it. Unlike `coupon` — a placed block
		// that still shows as its label plus the not-applied note, never as
		// an input — these carry a price and are payment-mode-only. A form
		// that reaches this page carrying one is the mode-flip case (the
		// builder filters the pricing group out of the response palette, but
		// does not remove blocks already placed), and the page must not
		// invent a price for a form that cannot be paid.
		case "product", "quantity", "option", "shipping", "total":
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	globalTr := ""
	if h.Accounts != nil {
		if v, terr := h.Accounts.Tracking(r.Context(), f.AccountID); terr == nil {
			globalTr = v
		}
	}
	// Card t_8f5d6fb5: same theme wiring as the block renderer above — the
	// CTA is painted from the merchant's brand colour with hover/active derived
	// from the same hue.
	cta := themePrimaryValues(f)
	ls := h.effectiveLangStrings(r.Context(), f)
	view := map[string]any{
		"Slug": f.Slug, "Title": f.Title, "Fields": fields,
		"TrackHead":  headInjection(globalTr, formTrackingHead(f)),
		"LogoURL":    f.LogoURL,
		"CtaPrimary": cta.Primary, "CtaHover": cta.Hover, "CtaActive": cta.Active,
		"CtaLabel": cta.Label, "CtaText": cta.Text,
		"Lang": ls.Lang,
		"Ls":   ls,
		// Issue #411: Open Graph + Twitter meta for social sharing.
		"OGMeta": h.formOGMeta(r, f),
		// Issue #183: optional per-workspace footer.
		"Footer": h.workspaceFooter(r.Context(), f),
		// Issue #244: hidden honeypot + render-timestamp fields.
		"AntiSpam": template.HTML(antiSpamRenderFields()),
		// Issue #276: noindex on jomform.com subdomains / when the workspace opts
		// out of indexing.
		"NoIndex": h.formNoIndex(r, f),
		// Issue #456: postMessage auto-height for the iframe embed. No-op on a
		// direct visit (the script only runs when framed).
		"HeightJS": template.HTML(iframeHeightJS),
		// Card t_e2024698: see the payment renderer's map above.
		"PayFailJS": payFailJS(ls),
		// Card t_9e875bca: the same conditional-logic script the payment
		// renderer emits — a response form's block carries data-condition too.
		"ConditionJS": template.HTML(blockConditionJS),
	}
	// Card t_a7bf0fb2: the SAME theme view the payment renderer merges — see
	// blockThemeView, including the CSS-context typing of `custom_css`.
	for k, v := range blockThemeView(f) {
		view[k] = v
	}
	if err := responseTmpl.Execute(w, view); err != nil {
		slog.Error("form_render_response", "err", err)
	}
	return true
}

// responseTmpl renders a response-only form. No product list, no total, no
// payment — just the input blocks and a submit button. On success the form is
// replaced with a thank-you message (no /t/{ref} payment status).
var responseTmpl = template.Must(template.New("response").
	Funcs(template.FuncMap{
		// Card t_23289ce3: the same per-field settings the payment renderer
		// emits — the two renderers must not disagree about a block.
		"styleAttr": styleAttr,
		"dateType":  dateInputType,
		// Card t_5c8fd63e: Arabic is the RTL locale (formLang.go isRTL).
		"isRTL": isRTL,
	}).
	Parse(`<!doctype html>
<html lang="{{.Lang}}"{{if isRTL .Lang}} dir="rtl"{{end}}><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
{{if .NoIndex}}<meta name="robots" content="noindex, nofollow">{{end}}
<title>{{.Title}}</title>
{{.OGMeta}}
{{.TrackHead}}
<style>
  /* Card t_8f5d6fb5: the merchant's brand colour lives here as a TOKEN TRIPLE
     and every accent below consumes it. Before this, the ":root { --jf-primary }"
     declaration had no consumer anywhere in api/ — the submit CTA was a
     hard-coded #5e6ad2 and the checkbox/radio accent #4f46e5 — so a merchant who
     picked a colour on /form-settings got the default indigo.
     The triple is resolved server-side (formtheme.go): CtaPrimary is the
     merchant's colour, CtaHover/CtaActive are the same hue and chroma at a
     different OKLCH lightness, each measured so the button's label still clears
     AA 4.5:1, and CtaLabel is the label colour that pairing measured.
     Unconditional, not an if-guard on ThemePrimary: an unthemed form gets the
     same defaults the web CTA uses (#5e6ad2 / #616dd5 / #454ea8), so there is
     exactly one place the button's colour can come from. */
  :root { --jf-primary: {{.CtaPrimary}}; --jf-primary-hover: {{.CtaHover}};
          --jf-primary-active: {{.CtaActive}}; --jf-primary-label: {{.CtaLabel}};
          --jf-primary-text: {{.CtaText}}; }
  {{if .ThemeBg}}body { background: {{.ThemeBg}}; }{{end}}
  {{if .ThemeText}}body { color: {{.ThemeText}}; }{{end}}
  {{if .ThemeFont}}body { font-family: {{.ThemeFont}}; }{{end}}
  {{if .ThemeCardBg}}.card { background: {{.ThemeCardBg}}; }{{end}}
  {{if .ThemeRadius}}.card { border-radius: {{.ThemeRadius}}; }{{end}}
  {{if .ThemeLayout}}body { justify-content: {{if eq .ThemeLayout "full_width"}}flex-start{{else}}center{{end}}; }{{end}}
  {{if .ThemeLayout}}body { padding: {{if eq .ThemeLayout "full_width"}}0{{else}}24px{{end}}; }{{end}}
  {{if .ThemeLayout}}.card { max-width: {{if eq .ThemeLayout "full_width"}}100%{{else}}480px{{end}}; }{{end}}
  {{if .ThemeSpacing}}.card { padding: {{if eq .ThemeSpacing "compact"}}20px{{else if eq .ThemeSpacing "spacious"}}48px{{else}}32px{{end}}; }{{end}}
  {{if .ThemeCSS}}{{.ThemeCSS}}{{end}}
</style>
<style>
  :root { color-scheme: dark; }
  * { box-sizing: border-box; }
  body { margin:0; font-family: system-ui,-apple-system,"Segoe UI",sans-serif;
         background:#f7f8f8; color:#17181c; min-height:100vh;
         display:flex; flex-direction:column; align-items:center; justify-content:center; padding:24px; }
  .logo { max-height:56px; max-width:180px; object-fit:contain; margin:0 auto 12px; display:block; }
  .card { background:#ffffff; border:1px solid #e6e8ec; border-radius:16px;
          max-width:480px; width:100%; padding:32px; }
  h1 { margin:0 0 8px; font-size:24px; letter-spacing:-0.02em; }
  .lbl { display:block; font-size:12px; color:#6b7280; margin:12px 0 4px; }
  .desc { display:block; font-size:12px; color:#9ca3af; margin:-2px 0 6px; }
  /* Card t_99c3a08b: the static blocks (section / html / image / divider) are
     rendered now, so the response page needs their rules — the same shapes the
     payment renderer paints in blockTmpl. */
  .section { font-size:18px; font-weight:600; margin:18px 0 6px; color:#17181c; }
  .text { color:#6b7280; font-size:14px; line-height:1.6; margin:0 0 16px; }
  .img { max-width:100%; border-radius:12px; border:1px solid #e6e8ec; margin-bottom:14px; }
  hr { border:0; border-top:1px solid #e6e8ec; margin:16px 0; }
  .chk { display:flex; align-items:center; gap:8px; font-size:14px; margin:8px 0; color:#374151; }
  .chk input[type=checkbox] { width:18px; height:18px; accent-color:var(--jf-primary); margin:0; }
  .rad { display:flex; align-items:center; gap:8px; font-size:14px; margin:8px 0; color:#374151; }
  .rad input[type=radio] { margin:0; accent-color:var(--jf-primary); }
  input[type=text],input[type=email],input[type=tel],input[type=number],
  input[type=date],input[type=time],input[type=url],input[type=password],
  select,textarea {
    width:100%; padding:10px 12px; border-radius:8px; border:1px solid #e6e8ec;
    background:#ffffff; color:#17181c; font-size:14px; margin-bottom:10px; }
  /* The submit CTA (card t_8f5d6fb5). Fill, hover and pressed all come from the
     form's brand triple declared in the :root block above; the LABEL colour is
     the one the derived pairing measured, so a light brand colour gets dark text
     instead of a white label it cannot carry at AA. The three states are the
     same construction .jf-btn-primary uses on the dashboard: rest -> hover
     (lighter for indigo, darker for a light brand) -> pressed (darker), with a
     shadow on hover so the hover is visible even before it is read as a colour. */
  button { width:100%; padding:12px; border:0; border-radius:10px; font-size:15px; font-weight:600;
           color:var(--jf-primary-label); cursor:pointer; margin-top:12px;
           background:var(--jf-primary);
           transition:background .15s ease, box-shadow .15s ease; }
  button:hover { background:var(--jf-primary-hover); box-shadow:0 2px 8px rgba(23,24,28,0.12); }
  button:active { background:var(--jf-primary-active); }
  /* Issue #183: optional per-workspace footer. */
  .jf-footer { max-width:480px; width:100%; margin:16px auto 0; padding:0 8px;
               color:#6b7280; font-size:12px; line-height:1.6; text-align:center;
               align-self:center; }
  .jf-fbiz { font-weight:600; color:#374151; }
  .jf-ftext { margin-top:4px; }
  .jf-flinks { display:flex; width:100%; gap:14px; justify-content:center;
               flex-wrap:wrap; margin-top:6px; text-align:center; }
  .jf-flinks a { color:var(--jf-primary-text); text-decoration:none; }
  .jf-flinks a:hover { text-decoration:underline; }
  .ok { text-align:center; padding:24px 0; }
  .ok h1 { color:#10b981; }
  .ok p { color:#6b7280; font-size:14px; }
  .ok .msg { color:#17181c; font-size:14px; line-height:1.6; margin-top:8px; }
  .ok .msg a { color:var(--jf-primary-text); }
</style></head><body>
<form class="card" id="of" data-slug="{{.Slug}}">
  <h1>{{.Title}}</h1>
{{if .LogoURL}}<img class="logo" src="{{.LogoURL}}" alt="">{{end}}
{{range $f := .Fields}}{{if .CondJSON}}<div class="jf-block" data-condition="{{.CondJSON}}">{{end}}
{{if eq .Type "text"}}  <label class="lbl"{{styleAttr .Style}}>{{.Value}}{{if .Required}} {{$.Ls.Required}}{{end}}</label><input type="text" name="{{.ID}}" data-id="{{.ID}}"{{if .CSSClass}} class="{{.CSSClass}}"{{end}}{{if .Placeholder}} placeholder="{{.Placeholder}}"{{end}}{{if .MaxLength}} maxlength="{{.MaxLength}}"{{end}}{{if .DefaultValue}} value="{{.DefaultValue}}"{{end}}{{if .Pattern}} pattern="{{.Pattern}}"{{end}}{{if .Required}} required{{end}}{{if .RequiredMsg}} oninvalid="this.setCustomValidity('{{.RequiredMsg}}')" oninput="this.setCustomValidity('')"{{end}}>
{{else if eq .Type "section"}}  <div class="section{{if .CSSClass}} {{.CSSClass}}{{end}}"{{styleAttr .Style}}>{{.Value}}</div>
{{else if eq .Type "html"}}  <div class="text{{if .CSSClass}} {{.CSSClass}}{{end}}"{{styleAttr .Style}}>{{.Value}}</div>
{{else if eq .Type "image"}}{{if .Src}}  <img class="img{{if .CSSClass}} {{.CSSClass}}{{end}}" src="{{.Src}}" alt=""{{styleAttr .Style}}>
{{end}}{{else if eq .Type "divider"}}  <hr{{if .CSSClass}} class="{{.CSSClass}}"{{end}}{{styleAttr .Style}}>
{{else if eq .Type "file"}}  <label class="lbl"{{styleAttr .Style}}>{{.Value}}</label><div class="desc">{{$.Ls.FileUnsupported}}</div>
{{else if eq .Type "coupon"}}  <label class="lbl"{{styleAttr .Style}}>{{.Value}}</label><div class="desc">{{$.Ls.CouponUnsupported}}</div>
{{else if eq .Type "name"}}  <label class="lbl"{{styleAttr .Style}}>{{.Value}}{{if .Required}} {{$.Ls.Required}}{{end}}</label><input type="text" name="{{.ID}}" data-jf-client="full_name"{{if .CSSClass}} class="{{.CSSClass}}"{{end}}{{if .Required}} required{{end}}{{if .RequiredMsg}} oninvalid="this.setCustomValidity('{{.RequiredMsg}}')" oninput="this.setCustomValidity('')"{{end}}>
{{else if eq .Type "email"}}  <label class="lbl"{{styleAttr .Style}}>{{$.Ls.EmailLabel}}{{if .Required}} {{$.Ls.Required}}{{end}}</label><input type="email" name="{{.ID}}" data-jf-client="email"{{if .CSSClass}} class="{{.CSSClass}}"{{end}}{{if .Placeholder}} placeholder="{{.Placeholder}}"{{end}}{{if .Pattern}} pattern="{{.Pattern}}"{{end}}{{if .Required}} required{{end}}{{if .RequiredMsg}} oninvalid="this.setCustomValidity('{{.RequiredMsg}}')" oninput="this.setCustomValidity('')"{{end}}>
{{else if eq .Type "phone"}}  <label class="lbl"{{styleAttr .Style}}>{{.Value}}{{if .Required}} {{$.Ls.Required}}{{end}}</label><input type="tel" name="{{.ID}}" data-jf-client="phone"{{if .CSSClass}} class="{{.CSSClass}}"{{end}}{{if .Placeholder}} placeholder="{{.Placeholder}}"{{end}}{{if .Pattern}} pattern="{{.Pattern}}"{{end}}{{if .Required}} required{{end}}{{if .RequiredMsg}} oninvalid="this.setCustomValidity('{{.RequiredMsg}}')" oninput="this.setCustomValidity('')"{{end}}>
{{else if eq .Type "textarea"}}  <label class="lbl"{{styleAttr .Style}}>{{.Value}}{{if .Required}} {{$.Ls.Required}}{{end}}</label><textarea name="{{.ID}}"{{if .CSSClass}} class="{{.CSSClass}}"{{end}} rows="3"{{if .Placeholder}} placeholder="{{.Placeholder}}"{{end}}{{if .MaxLength}} maxlength="{{.MaxLength}}"{{end}}{{if .Required}} required{{end}}{{if .RequiredMsg}} oninvalid="this.setCustomValidity('{{.RequiredMsg}}')" oninput="this.setCustomValidity('')"{{end}}>{{.DefaultValue}}</textarea>
{{else if eq .Type "checkbox"}}  <label class="chk{{if .CSSClass}} {{.CSSClass}}{{end}}"{{styleAttr .Style}}><input type="checkbox" name="{{.ID}}" value="1"{{if .Required}} required{{end}}{{if .RequiredMsg}} oninvalid="this.setCustomValidity('{{.RequiredMsg}}')" oninput="this.setCustomValidity('')"{{end}}> {{.Value}}</label>
{{else if eq .Type "consent"}}  <label class="chk{{if .CSSClass}} {{.CSSClass}}{{end}}"{{styleAttr .Style}}><input type="checkbox" name="{{.ID}}" value="1"{{if .Required}} required{{end}}{{if .RequiredMsg}} oninvalid="this.setCustomValidity('{{.RequiredMsg}}')" oninput="this.setCustomValidity('')"{{end}}> {{.Value}}</label>
{{else if eq .Type "number"}}  <label class="lbl"{{styleAttr .Style}}>{{.Value}}{{if .Required}} {{$.Ls.Required}}{{end}}</label><input type="number" name="{{.ID}}"{{if .CSSClass}} class="{{.CSSClass}}"{{end}}{{if .Placeholder}} placeholder="{{.Placeholder}}"{{end}}{{if .Min}} min="{{.Min}}"{{end}}{{if .Max}} max="{{.Max}}"{{end}}{{if .Step}} step="{{.Step}}"{{end}}{{if .Required}} required{{end}}{{if .RequiredMsg}} oninvalid="this.setCustomValidity('{{.RequiredMsg}}')" oninput="this.setCustomValidity('')"{{end}}>
{{else if eq .Type "select"}}  <label class="lbl"{{styleAttr .Style}}>{{.Value}}{{if .Required}} {{$.Ls.Required}}{{end}}</label><select name="{{.ID}}"{{if .CSSClass}} class="{{.CSSClass}}"{{end}}{{if .Required}} required{{end}}{{if .RequiredMsg}} oninvalid="this.setCustomValidity('{{.RequiredMsg}}')" oninput="this.setCustomValidity('')"{{end}}><option value="" disabled selected>{{$.Ls.SelectOption}}</option>{{range .Choices}}<option value="{{.Value}}">{{.Label}}</option>{{end}}</select>
{{else if eq .Type "radio"}}  <label class="lbl"{{styleAttr .Style}}>{{.Value}}{{if .Required}} {{$.Ls.Required}}{{end}}</label>{{range .Choices}}<label class="rad{{if $f.CSSClass}} {{$f.CSSClass}}{{end}}"><input type="radio" name="{{$f.ID}}" value="{{.Value}}"{{if $f.Required}} required{{end}}{{if $f.RequiredMsg}} oninvalid="this.setCustomValidity('{{$f.RequiredMsg}}')" oninput="this.setCustomValidity('')"{{end}}> {{.Label}}</label>{{end}}
{{else if eq .Type "image-choice"}}  <label class="lbl"{{styleAttr .Style}}>{{.Value}}{{if .Required}} {{$.Ls.Required}}{{end}}</label>{{range .Choices}}<label class="rad{{if $f.CSSClass}} {{$f.CSSClass}}{{end}}"><input type="radio" name="{{$f.ID}}" value="{{.Value}}"{{if $f.Required}} required{{end}}{{if $f.RequiredMsg}} oninvalid="this.setCustomValidity('{{$f.RequiredMsg}}')" oninput="this.setCustomValidity('')"{{end}}> {{.Label}}</label>{{end}}
{{else if eq .Type "date"}}  <label class="lbl"{{styleAttr .Style}}>{{.Value}}{{if .Required}} {{$.Ls.Required}}{{end}}</label><input type="{{dateType .Type .Format}}" name="{{.ID}}"{{if .CSSClass}} class="{{.CSSClass}}"{{end}}{{if .Required}} required{{end}}{{if .RequiredMsg}} oninvalid="this.setCustomValidity('{{.RequiredMsg}}')" oninput="this.setCustomValidity('')"{{end}}>
{{else if eq .Type "time"}}  <label class="lbl"{{styleAttr .Style}}>{{.Value}}{{if .Required}} {{$.Ls.Required}}{{end}}</label><input type="{{dateType .Type .Format}}" name="{{.ID}}"{{if .CSSClass}} class="{{.CSSClass}}"{{end}}{{if .Required}} required{{end}}{{if .RequiredMsg}} oninvalid="this.setCustomValidity('{{.RequiredMsg}}')" oninput="this.setCustomValidity('')"{{end}}>
{{else if eq .Type "website"}}  <label class="lbl"{{styleAttr .Style}}>{{.Value}}{{if .Required}} {{$.Ls.Required}}{{end}}</label><input type="url" name="{{.ID}}"{{if .CSSClass}} class="{{.CSSClass}}"{{end}}{{if .Placeholder}} placeholder="{{.Placeholder}}"{{end}}{{if .Pattern}} pattern="{{.Pattern}}"{{end}}{{if .Required}} required{{end}}{{if .RequiredMsg}} oninvalid="this.setCustomValidity('{{.RequiredMsg}}')" oninput="this.setCustomValidity('')"{{end}}>
{{else if eq .Type "address"}}  <label class="lbl"{{styleAttr .Style}}>{{.Value}}{{if .Required}} {{$.Ls.Required}}{{end}}</label><textarea name="{{.ID}}"{{if .CSSClass}} class="{{.CSSClass}}"{{end}} rows="2"{{if .Placeholder}} placeholder="{{.Placeholder}}"{{end}}{{if .Required}} required{{end}}{{if .RequiredMsg}} oninvalid="this.setCustomValidity('{{.RequiredMsg}}')" oninput="this.setCustomValidity('')"{{end}}></textarea>
{{else if eq .Type "list"}}  <label class="lbl"{{styleAttr .Style}}>{{.Value}}{{if .Required}} {{$.Ls.Required}}{{end}}</label><textarea name="{{.ID}}"{{if .CSSClass}} class="{{.CSSClass}}"{{end}} rows="3"{{if .Placeholder}} placeholder="{{.Placeholder}}"{{end}}{{if .Required}} required{{end}}{{if .RequiredMsg}} oninvalid="this.setCustomValidity('{{.RequiredMsg}}')" oninput="this.setCustomValidity('')"{{end}}></textarea>
{{else if eq .Type "repeater"}}  <label class="lbl"{{styleAttr .Style}}>{{.Value}}{{if .Required}} {{$.Ls.Required}}{{end}}</label><textarea name="{{.ID}}"{{if .CSSClass}} class="{{.CSSClass}}"{{end}} rows="3"{{if .Placeholder}} placeholder="{{.Placeholder}}"{{end}}{{if .Required}} required{{end}}{{if .RequiredMsg}} oninvalid="this.setCustomValidity('{{.RequiredMsg}}')" oninput="this.setCustomValidity('')"{{end}}></textarea>
{{else if eq .Type "captcha"}}  <label class="lbl"{{styleAttr .Style}}>{{.Value}}{{if .Required}} {{$.Ls.Required}}{{end}}</label><input type="text" name="{{.ID}}"{{if .CSSClass}} class="{{.CSSClass}}"{{end}}{{if .Placeholder}} placeholder="{{.Placeholder}}"{{end}}{{if .Required}} required{{end}}{{if .RequiredMsg}} oninvalid="this.setCustomValidity('{{.RequiredMsg}}')" oninput="this.setCustomValidity('')"{{end}}>
{{else if eq .Type "hidden"}}  <input type="hidden" name="{{.ID}}" value="{{.DefaultValue}}"{{if .CSSClass}} class="{{.CSSClass}}"{{end}}{{styleAttr .Style}}>
{{end}}
{{if .Description}}<div class="desc">{{.Description}}</div>{{end}}{{if .CondJSON}}</div>{{end}}
{{end}}
  {{.AntiSpam}}
  <button type="submit">{{$.Ls.SubmitResponse}}</button>
</form>
{{if .Footer.HasFooter}}<footer class="jf-footer">
  {{if .Footer.BusinessName}}<div class="jf-fbiz">{{.Footer.BusinessName}}{{if .Footer.CompanyReg}} · {{.Footer.CompanyReg}}{{end}}</div>{{end}}
  {{if .Footer.ContactPhone}}<div class="jf-frow">{{.Footer.ContactPhone}}</div>{{end}}
  {{if .Footer.ContactAddress}}<div class="jf-frow">{{.Footer.ContactAddress}}</div>{{end}}
  {{if .Footer.FooterText}}<div class="jf-ftext">{{.Footer.FooterText}}</div>{{end}}
  {{if or .Footer.PrivacyURL .Footer.TermsURL .Footer.RefundURL}}<div class="jf-flinks">
    {{if .Footer.PrivacyURL}}<a href="{{.Footer.PrivacyURL}}" target="_blank" rel="noopener">{{$.Ls.PrivacyLabel}}</a>{{end}}
    {{if .Footer.TermsURL}}<a href="{{.Footer.TermsURL}}" target="_blank" rel="noopener">{{$.Ls.TermsLabel}}</a>{{end}}
    {{if .Footer.RefundURL}}<a href="{{.Footer.RefundURL}}" target="_blank" rel="noopener">{{$.Ls.RefundLabel}}</a>{{end}}
  </div>{{end}}
</footer>{{end}}
<script>{{.PayFailJS}}</script>
<script>
(function(){
  var form=document.getElementById('of');
  form.addEventListener('submit',function(e){
    e.preventDefault();
    var fields={};
    var els=form.querySelectorAll('[name]');
    els.forEach(function(el){
      // A control inside a HIDDEN conditional block is disabled by the
      // conditional-logic script; its answer is not part of this submission
      // (the server drops it too, formfields.VisibleAnswers).
      if(el.disabled)return;
      if(el.type==='checkbox'){fields[el.name]=el.checked?'1':'';}
      else if(el.type==='radio'){if(el.checked)fields[el.name]=el.value;}
      else if(el.type==='file'){fields[el.name]=el.files&&el.files[0]?el.files[0].name:'';}
      else{fields[el.name]=el.value;}
    });
    var body={client:{
      full_name:(document.querySelector('[name]')||{}).value||'',
      email:'',phone:''
    },fields:fields,
    // Issue #444: affiliate marketing — echo the ?aff=CODE the buyer landed
    // with so the sale is attributed to that affiliate.
    affiliate_code:(new URLSearchParams(location.search).get('aff')||''),
    // Issue #489: QR ordering — echo the ?table=N the QR stand / menu page
    // carried into the form URL so sales.table_number is populated.
    table_number:(new URLSearchParams(location.search).get('table')||'')};
    // Map the name/email/phone BLOCKS into the client object. Each of those
    // three arms tags its own control with data-jf-client, so the mapping
    // follows the block the merchant placed — never a position, and never an
    // input type. The type was a proxy for "the name field" and stopped being
    // one once a 'text' block rendered a real input[type=text] (card
    // t_99c3a08b): "the first text input" is then the text/label field, so
    // client.full_name carried the wrong answer into data["full_name"], the
    // submissions Buyer column and the CSV export (card t_98d67234). The
    // "|| input[type=...]" half is the OLD lookup, kept only for a form that
    // has no name/email/phone block at all, so that case maps as it always did.
    var nm=form.querySelector('[data-jf-client=full_name]')||form.querySelector('input[type=text]'); if(nm) body.client.full_name=nm.value;
    var em=form.querySelector('[data-jf-client=email]')||form.querySelector('input[type=email]'); if(em) body.client.email=em.value;
    var ph=form.querySelector('[data-jf-client=phone]')||form.querySelector('input[type=tel]'); if(ph) body.client.phone=ph.value;
    fetch(location.pathname+'/order',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)})
      .then(function(r){return r.json()})
      .then(function(d){
        if(d.status==='submitted'){
          // Issue #179: render the resolved confirmation. A redirect sends the
          // buyer to the configured URL; a message replaces the form with the
          // (server-sanitized) confirmation text.
          if(d.confirmation==='redirect'&&d.redirect_url){
            if(window.self!==window.top){window.top.location.href=d.redirect_url;}
            else{location.href=d.redirect_url;}
            return;
          }
          var msg=d.message||'{{$.Ls.ResponseReceived}}';
          form.innerHTML='<div class="ok"><h1>✓ {{$.Ls.ThankYou}}</h1><div class="msg">'+msg+'</div></div>';
        } else if(d&&d.status==='payment_error'){
          // Card t_e2024698: the payment could not be started — say so in-app,
          // with the reference, instead of an alert the buyer can dismiss.
          jfPayFail(d);
        } else {
          alert('{{$.Ls.ErrorAlert}}'+(d.error?': '+d.error:''));
        }
      })
      // Card t_e2024698: a non-JSON answer must not leave the buyer with
      // nothing at all.
      .catch(function(){jfPayFail({});});
  });
})();
</script>
{{.ConditionJS}}
{{if .ThemeJS}}<script>
{{.ThemeJS}}
</script>
{{end}}
{{.HeightJS}}
</body></html>`))
