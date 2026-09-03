package httpapi

import (
	"encoding/json"

	"html/template"
	"log/slog"
	"net/http"

	"github.com/wzul/jomform/api/internal/dbstore"
)

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
			Type      string     `json:"type"`
			Value     string     `json:"value"`
			Src       string     `json:"src"`
			ProductID string     `json:"product_id"`
			Required  bool       `json:"required"`
			Condition *Condition `json:"condition"`
		} `json:"blocks"`
		Products []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			PriceSen int64  `json:"price_sen"`
		} `json:"products"`
	}
	if json.Unmarshal(raw, &cfg) != nil || len(cfg.Blocks) == 0 {
		return false
	}

	priceOf := map[string]int64{}
	nameOf := map[string]string{}
	for _, p := range cfg.Products {
		priceOf[p.ID] = p.PriceSen
		nameOf[p.ID] = p.Name
	}

	// variationView is the buyer-facing shape of one product variation.
	type variationView struct {
		ID       string
		Name     string
		PriceSen int64
		Stock    *int
		SoldOut  bool
	}

	type blockView struct {
		Type, Value, Src string
		ProductID        string
		PriceSen         int64
		Required         bool
		HasPrice         bool
		CondJSON         string
		// Variations (issue #153): when a product has >=1 variation, render a
		// selector instead of a single price/stock. Each carries its own price
		// and stock; sold-out variations are disabled.
		Variations []variationView
	}

	blocks := make([]blockView, 0, len(cfg.Blocks))
	for _, b := range cfg.Blocks {
		vb := blockView{Type: b.Type, Value: b.Value, Src: b.Src, ProductID: b.ProductID, Required: b.Required}
		vb.CondJSON = marshalCondition(b.Condition)
		if b.ProductID != "" {
			if price, ok := priceOf[b.ProductID]; ok {
				vb.PriceSen = price
				vb.HasPrice = true
			}
			if vb.Value == "" {
				vb.Value = nameOf[b.ProductID]
			}
			// Issue #153: load the product's variations for the selector.
			if h.ProductVariations != nil {
				if vs, verr := h.ProductVariations.ListByProduct(r.Context(), b.ProductID); verr == nil && len(vs) > 0 {
					for _, v := range vs {
						vb.Variations = append(vb.Variations, variationView{
							ID: v.ID, Name: v.Name, PriceSen: v.PriceSen, Stock: v.Stock,
							SoldOut: v.Stock != nil && *v.Stock <= 0,
						})
					}
				}
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
	themePrimary, themeBg := "", ""
	if f.Theme != nil {
		if v, ok := f.Theme["primary"].(string); ok {
			themePrimary = template.HTMLEscapeString(v)
		}
		if v, ok := f.Theme["background"].(string); ok {
			themeBg = template.HTMLEscapeString(v)
		}
	}
	var cfgTracking string
	_ = cfgTracking
	ls := h.effectiveLangStrings(r.Context(), f)
	if err := blockTmpl.Execute(w, map[string]any{
		"Slug": f.Slug, "Title": f.Title, "Blocks": blocks,
		"TrackHead":    headInjection(globalTr, formTrackingHead(f)),
		"ThemePrimary": themePrimary, "ThemeBg": themeBg, "LogoURL": f.LogoURL,
		"Lang": ls.Lang,
		"Ls":   ls,
	}); err != nil {
		slog.Error("form_render_blocks", "err", err)
	}
	return true
}

// blockTmpl renders each builder block. Prices ride on data-price for the
// client total; the server remains the price authority on submit.
var blockTmpl = template.Must(template.New("blocks").
	Funcs(template.FuncMap{
		"divf": func(total, div int64) float64 { return float64(total) / float64(div) },
	}).
	Parse(`<!doctype html>
<html lang="{{.Lang}}"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}}</title>
{{.TrackHead}}
<style>
  {{if .ThemePrimary}}:root { --jf-primary: {{.ThemePrimary}}; }{{end}}
  {{if .ThemeBg}}body { background: {{.ThemeBg}}; }{{end}}
</style>
<style>
  :root { color-scheme: dark; }
  * { box-sizing: border-box; }
  body { margin:0; font-family: system-ui,-apple-system,"Segoe UI",sans-serif;
         background:#f7f8f8; color:#17181c; min-height:100vh;
         display:flex; align-items:center; justify-content:center; padding:24px; }
  .logo { max-height:56px; max-width:180px; object-fit:contain; margin:0 auto 12px; display:block; }
  .card { background:#ffffff; border:1px solid #e6e8ec; border-radius:16px;
          max-width:480px; width:100%; padding:32px; }
  h1 { margin:0 0 8px; font-size:24px; letter-spacing:-0.02em; }
  .text { color:#6b7280; font-size:14px; line-height:1.6; margin:0 0 16px; }
  .img { max-width:100%; border-radius:12px; border:1px solid #e6e8ec; margin-bottom:14px; }
  .prod { display:block; padding:0; border:1px solid #e6e8ec; border-radius:14px;
          margin:0 0 12px; background:#ffffff; box-shadow:0 1px 2px rgba(23,24,28,0.04);
          transition:border-color .15s ease, box-shadow .15s ease; }
  .prod:hover { border-color:#4f46e5; box-shadow:0 2px 8px rgba(79,70,229,0.12); }
  .prod:active { background:#eef2ff; }
  .prod:has(input[type=checkbox]:checked) { border-color:#4f46e5; box-shadow:0 2px 8px rgba(79,70,229,0.12); background:#eef2ff; }
  .prod label { display:flex; gap:12px; align-items:center; width:100%;
                min-height:44px; padding:12px 14px; font-size:15px; color:#17181c;
                cursor:pointer; -webkit-tap-highlight-color:transparent; }
  .prod input[type=checkbox] { width:20px; height:20px; flex:0 0 auto; accent-color:#4f46e5; margin:0; }
  /* Issue #58: ALL checkboxes (product .p and custom .extra) share one bright
     unchecked state + indigo accent. appearance:none + explicit border/background
     overrides UA dark defaults so Android Chrome draws a white unchecked box. */
  input[type=checkbox].extra, input[type=checkbox].p {
    -webkit-appearance:none; -moz-appearance:none; appearance:none;
    accent-color:#4f46e5; width:20px; height:20px; flex:0 0 auto;
    border:2px solid #d1d5db; border-radius:5px; background:#fff;
    cursor:pointer; margin:0;
  }
  input[type=checkbox].extra:checked, input[type=checkbox].p:checked {
    border-color:#4f46e5; background:#4f46e5 url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 20 20'%3E%3Cpath fill='none' stroke='%23fff' stroke-width='3' stroke-linecap='round' stroke-linejoin='round' d='M5 10l3 3 6-6'/%3E%3C/svg%3E") center/14px no-repeat;
  }
  .pname { flex:1; font-weight:500; }
  .price { color:#4f46e5; font-weight:600; font-size:14px; white-space:nowrap;
           margin-left:auto; text-align:right; }
  /* Issue #153: variation selector inside a product block. */
  .varwrap { padding:0 14px 12px; }
  .varwrap select { width:100%; padding:9px 10px; border-radius:8px;
           border:1px solid #e6e8ec; background:#ffffff; color:#17181c; font-size:14px; }
  .varstock { font-size:12px; color:#6b7280; margin-top:6px; }
  .lbl { display:block; font-size:12px; color:#6b7280; margin:12px 0 4px; }
  .chk { display:flex; align-items:center; gap:8px; font-size:14px; margin:8px 0; color:#374151; }
  .chk input[type=checkbox] { margin:0; }
  input[type=text],input[type=email],input[type=tel],textarea,.coupon {
    width:100%; padding:10px 12px; border-radius:8px; border:1px solid #e6e8ec;
    background:#ffffff; color:#17181c; font-size:14px; margin-bottom:10px; }
  hr { border:0; border-top:1px solid rgba(255,255,255,0.08); margin:16px 0; }
  .total { display:flex; justify-content:space-between; padding-top:12px; margin-top:8px;
           border-top:1px dashed rgba(255,255,255,0.12); font-weight:600; }
  button { width:100%; padding:12px; border:0; border-radius:10px; font-size:15px; font-weight:600;
           color:#fff; cursor:pointer; margin-top:12px; background:#5e6ad2; }
  button:hover { background:#828fff; }
</style></head><body>
<form class="card" id="of" data-slug="{{.Slug}}">
  <h1>{{.Title}}</h1>
{{if .LogoURL}}<img class="logo" src="{{.LogoURL}}" alt="">{{end}}
{{range .Blocks}}
{{if eq .Type "text"}}  <p class="text">{{.Value}}</p>
{{else if eq .Type "image"}}{{if .Src}}  <img class="img" src="{{.Src}}" alt="">
{{end}}{{else if eq .Type "divider"}}  <hr>
{{else if eq .Type "product"}}  <div class="prod"><label><input type="checkbox" class="p" data-id="{{.ProductID}}" data-price="{{.PriceSen}}"><span class="pname">{{.Value}}</span><span class="price">{{if .HasPrice}}RM{{printf "%.2f" (divf .PriceSen 100)}}{{end}}</span></label>{{if .Variations}}<div class="varwrap"><select class="v" data-id="{{.ProductID}}"><option value="" disabled selected>{{$.Ls.SelectVariation}}</option>{{range .Variations}}<option value="{{.ID}}" data-price="{{.PriceSen}}"{{if .SoldOut}} disabled{{end}}>{{.Name}} — RM{{printf "%.2f" (divf .PriceSen 100)}}{{if .Stock}} ({{.Stock}} {{$.Ls.InStock}}){{end}}</option>{{end}}</select><div class="varstock"></div></div>{{end}}</div>
{{else if eq .Type "name"}}  <label class="lbl">{{.Value}}{{if .Required}} {{$.Ls.Required}}{{end}}</label><input type="text" name="full_name"{{if .Required}} required{{end}}>
{{else if eq .Type "email"}}  <label class="lbl">{{$.Ls.EmailLabel}}{{if .Required}} {{$.Ls.Required}}{{end}}</label><input type="email" name="email" required>
{{else if eq .Type "phone"}}  <label class="lbl">{{.Value}}</label><input type="tel" name="phone">
{{else if eq .Type "textarea"}}  <label class="lbl">{{.Value}}</label><textarea name="notes" rows="2"></textarea>
{{else if eq .Type "checkbox"}}  <label class="chk"><input type="checkbox" class="extra" value="{{.Value}}"> {{.Value}}</label>
{{else if eq .Type "coupon"}}  <label class="lbl">{{$.Ls.CouponLabel}}</label><input type="text" name="coupon" class="coupon">
{{end}}</span>
{{end}}
  <div class="total"><span>{{$.Ls.Total}}</span><span id="total">RM0.00</span></div>
  <button type="submit" form="of">{{$.Ls.Submit}}</button>
</form>
<script>
(function(){
  var boxes=document.querySelectorAll('.p');
  // Issue #153: variation selectors. Each variable product has a <select class="v">
  // whose options carry data-price; sold-out options are disabled server-side.
  var sels=document.querySelectorAll('select.v');
  function effPrice(b){
    var sel=document.querySelector('select.v[data-id="'+b.dataset.id+'"]');
    if(sel&&sel.value){var o=sel.options[sel.selectedIndex];if(o&&o.dataset.price)return Number(o.dataset.price);}
    return Number(b.dataset.price||0);
  }
  function upd(){var t=0;boxes.forEach(function(b){if(b.checked)t+=effPrice(b)});
    document.getElementById('total').textContent='RM'+(t/100).toFixed(2);}
  boxes.forEach(function(b){b.addEventListener('change',upd)});
  sels.forEach(function(s){s.addEventListener('change',upd)});
  document.getElementById('of').addEventListener('submit',function(e){
    e.preventDefault();
    var items=[];var ok=true;
    boxes.forEach(function(b){
      if(!b.checked)return;
      var sel=document.querySelector('select.v[data-id="'+b.dataset.id+'"]');
      var item={product_id:b.dataset.id,quantity:1};
      if(sel){
        if(!sel.value){ok=false;return;}
        item.variation_id=sel.value;
      }
      items.push(item);
    });
    if(!items.length){alert('{{.Ls.NoProductAlert}}');return;}
    if(!ok){alert('{{.Ls.SelectVariationAlert}}');return;}
    var body={client:{full_name:(e.target.full_name||{}).value||(document.querySelector('[name=full_name]')||{}).value||'',
      email:(document.querySelector('[name=email]')||{}).value||'',
      phone:(document.querySelector('[name=phone]')||{}).value||''},
      items:items,payment_method:'chip'};
    fetch(location.pathname+'/order',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)})
      .then(function(r){return r.json()}).then(function(d){
        if(d.checkout_url||d.next){
          var url=d.checkout_url||d.next;
          // Issue #60: when embedded in a merchant site, CHIP checkout and the
          // /t/ thank-you page refuse to frame (X-Frame-Options/anti-clickjacking).
          // Escape to the TOP frame so the buyer completes payment in their own tab;
          // direct visits keep today's same-frame navigation.
          if(window.self!==window.top){window.top.location.href=url;}
          else{location.href=url;}
        }
        else{alert('{{.Ls.ErrorAlert}}'+(d.error?': '+d.error:''));}});
  // Conditional logic: show/hide blocks with data-condition based on earlier inputs.
  (function(){
    var conds=document.querySelectorAll('[data-condition]');
    if(!conds.length)return;
    function fieldVal(id){
      var el=document.querySelector('[name="'+id+'"],#'+id);
      if(!el)return '';
      return (el.type==='checkbox')?(el.checked?el.value:''):el.value;
    }
    function evalCond(c){
      var v=fieldVal(c.field_id);
      switch(c.op){
        case 'equals':return c.value===''?v.trim()!=='':v.toLowerCase()===String(c.value).toLowerCase();
        case 'not_equals':return !(v.toLowerCase()===String(c.value).toLowerCase());
        case 'contains':return v.toLowerCase().indexOf(String(c.value).toLowerCase())>=0;
        case 'gt':return parseFloat(v)>parseFloat(c.value);
        case 'lt':return parseFloat(v)<parseFloat(c.value);
        case 'not_empty':return v.trim()!=='';
      }
      return true;
    }
    function refresh(){
      conds.forEach(function(el){
        try{
          var c=JSON.parse(el.dataset.condition);
          el.style.display=evalCond(c)?'':'none';
        }catch(e){}
      });
    }
    document.addEventListener('input',refresh);
    document.addEventListener('change',refresh);
    refresh();
  })();
  });
})();
</script>
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
// and the server stores a submission. Returns false when the form has no
// blocks (caller falls back to the legacy layout).
func (h *PublicHandlers) renderResponseForm(w http.ResponseWriter, r *http.Request, f *dbstore.Form) bool {
	raw, err := json.Marshal(f.Config)
	if err != nil {
		return false
	}
	var cfg struct {
		Blocks []struct {
			ID       string     `json:"id"`
			Type     string     `json:"type"`
			Value    string     `json:"value"`
			Required bool       `json:"required"`
			Condition *Condition `json:"condition"`
		} `json:"blocks"`
	}
	if json.Unmarshal(raw, &cfg) != nil || len(cfg.Blocks) == 0 {
		return false
	}

	type fieldView struct {
		ID, Type, Value string
		Required        bool
		CondJSON        string
	}
	fields := make([]fieldView, 0, len(cfg.Blocks))
	for _, b := range cfg.Blocks {
		// Only input blocks are collectable; text/image/divider/product are
		// decorative and skipped in response mode.
		switch b.Type {
		case "name", "email", "phone", "textarea", "checkbox":
			fields = append(fields, fieldView{
				ID: b.ID, Type: b.Type, Value: b.Value, Required: b.Required,
				CondJSON: marshalCondition(b.Condition),
			})
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	globalTr := ""
	if h.Accounts != nil {
		if v, terr := h.Accounts.Tracking(r.Context(), f.AccountID); terr == nil {
			globalTr = v
		}
	}
	themePrimary, themeBg := "", ""
	if f.Theme != nil {
		if v, ok := f.Theme["primary"].(string); ok {
			themePrimary = template.HTMLEscapeString(v)
		}
		if v, ok := f.Theme["background"].(string); ok {
			themeBg = template.HTMLEscapeString(v)
		}
	}
	ls := h.effectiveLangStrings(r.Context(), f)
	if err := responseTmpl.Execute(w, map[string]any{
		"Slug": f.Slug, "Title": f.Title, "Fields": fields,
		"TrackHead":    headInjection(globalTr, formTrackingHead(f)),
		"ThemePrimary": themePrimary, "ThemeBg": themeBg, "LogoURL": f.LogoURL,
		"Lang": ls.Lang,
		"Ls":   ls,
	}); err != nil {
		slog.Error("form_render_response", "err", err)
	}
	return true
}

// responseTmpl renders a response-only form. No product list, no total, no
// payment — just the input blocks and a submit button. On success the form is
// replaced with a thank-you message (no /t/{ref} payment status).
var responseTmpl = template.Must(template.New("response").
	Parse(`<!doctype html>
<html lang="{{.Lang}}"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}}</title>
{{.TrackHead}}
<style>
  {{if .ThemePrimary}}:root { --jf-primary: {{.ThemePrimary}}; }{{end}}
  {{if .ThemeBg}}body { background: {{.ThemeBg}}; }{{end}}
</style>
<style>
  :root { color-scheme: dark; }
  * { box-sizing: border-box; }
  body { margin:0; font-family: system-ui,-apple-system,"Segoe UI",sans-serif;
         background:#f7f8f8; color:#17181c; min-height:100vh;
         display:flex; align-items:center; justify-content:center; padding:24px; }
  .logo { max-height:56px; max-width:180px; object-fit:contain; margin:0 auto 12px; display:block; }
  .card { background:#ffffff; border:1px solid #e6e8ec; border-radius:16px;
          max-width:480px; width:100%; padding:32px; }
  h1 { margin:0 0 8px; font-size:24px; letter-spacing:-0.02em; }
  .lbl { display:block; font-size:12px; color:#6b7280; margin:12px 0 4px; }
  .chk { display:flex; align-items:center; gap:8px; font-size:14px; margin:8px 0; color:#374151; }
  .chk input[type=checkbox] { width:18px; height:18px; accent-color:#4f46e5; margin:0; }
  input[type=text],input[type=email],input[type=tel],textarea {
    width:100%; padding:10px 12px; border-radius:8px; border:1px solid #e6e8ec;
    background:#ffffff; color:#17181c; font-size:14px; margin-bottom:10px; }
  button { width:100%; padding:12px; border:0; border-radius:10px; font-size:15px; font-weight:600;
           color:#fff; cursor:pointer; margin-top:12px; background:#5e6ad2; }
  button:hover { background:#828fff; }
  .ok { text-align:center; padding:24px 0; }
  .ok h1 { color:#10b981; }
  .ok p { color:#6b7280; font-size:14px; }
</style></head><body>
<form class="card" id="of" data-slug="{{.Slug}}">
  <h1>{{.Title}}</h1>
{{if .LogoURL}}<img class="logo" src="{{.LogoURL}}" alt="">{{end}}
{{range .Fields}}
{{if eq .Type "name"}}  <label class="lbl">{{.Value}}{{if .Required}} {{$.Ls.Required}}{{end}}</label><input type="text" name="{{.ID}}"{{if .Required}} required{{end}}>
{{else if eq .Type "email"}}  <label class="lbl">{{$.Ls.EmailLabel}}{{if .Required}} {{$.Ls.Required}}{{end}}</label><input type="email" name="{{.ID}}"{{if .Required}} required{{end}}>
{{else if eq .Type "phone"}}  <label class="lbl">{{.Value}}</label><input type="tel" name="{{.ID}}">
{{else if eq .Type "textarea"}}  <label class="lbl">{{.Value}}</label><textarea name="{{.ID}}" rows="3"></textarea>
{{else if eq .Type "checkbox"}}  <label class="chk"><input type="checkbox" name="{{.ID}}" value="1"> {{.Value}}</label>
{{end}}
{{end}}
  <button type="submit">{{$.Ls.SubmitResponse}}</button>
</form>
<script>
(function(){
  var form=document.getElementById('of');
  form.addEventListener('submit',function(e){
    e.preventDefault();
    var fields={};
    var els=form.querySelectorAll('[name]');
    els.forEach(function(el){
      if(el.type==='checkbox'){fields[el.name]=el.checked?'1':'';}
      else{fields[el.name]=el.value;}
    });
    var body={client:{
      full_name:(document.querySelector('[name]')||{}).value||'',
      email:'',phone:''
    },fields:fields};
    // Map the first name/email/phone blocks into the client object too.
    var nm=form.querySelector('input[type=text]'); if(nm) body.client.full_name=nm.value;
    var em=form.querySelector('input[type=email]'); if(em) body.client.email=em.value;
    var ph=form.querySelector('input[type=tel]'); if(ph) body.client.phone=ph.value;
    fetch(location.pathname+'/order',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)})
      .then(function(r){return r.json()})
      .then(function(d){
        if(d.status==='submitted'){
          form.innerHTML='<div class="ok"><h1>✓ {{$.Ls.ThankYou}}</h1><p>{{$.Ls.ResponseReceived}}</p></div>';
        } else {
          alert('{{$.Ls.ErrorAlert}}'+(d.error?': '+d.error:''));
        }
      })
      .catch(function(){alert('{{$.Ls.ErrorAlert}}');});
  });
})();
</script>
</body></html>`))
