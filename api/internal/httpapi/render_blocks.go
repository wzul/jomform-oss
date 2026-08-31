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
			Type      string `json:"type"`
			Value     string `json:"value"`
			Src       string `json:"src"`
			ProductID string `json:"product_id"`
			Required  bool   `json:"required"`
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

	type blockView struct {
		Type, Value, Src string
		ProductID        string
		PriceSen         int64
		Required         bool
		HasPrice         bool
	}
	blocks := make([]blockView, 0, len(cfg.Blocks))
	for _, b := range cfg.Blocks {
		vb := blockView{Type: b.Type, Value: b.Value, Src: b.Src, ProductID: b.ProductID, Required: b.Required}
		if b.ProductID != "" {
			if price, ok := priceOf[b.ProductID]; ok {
				vb.PriceSen = price
				vb.HasPrice = true
			}
			if vb.Value == "" {
				vb.Value = nameOf[b.ProductID]
			}
		}
		blocks = append(blocks, vb)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := blockTmpl.Execute(w, map[string]any{
		"Slug": f.Slug, "Title": f.Title, "Blocks": blocks,
	}); err != nil {
		slog.Error("form_render_blocks", "err", err)
	}
	return true
}

// blockTmpl renders each builder block. Prices ride on data-price for the
// client total; the server remains authoritative via lookupProduct on submit.
var blockTmpl = template.Must(template.New("blocks").Parse(`<!doctype html>
<html lang="ms"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}}</title>
<style>
  :root { color-scheme: dark; }
  * { box-sizing: border-box; }
  body { margin:0; font-family: system-ui,-apple-system,"Segoe UI",sans-serif;
         background:#08090a; color:#f7f8f8; min-height:100vh;
         display:flex; align-items:center; justify-content:center; padding:24px; }
  .card { background:#0f1011; border:1px solid rgba(255,255,255,0.08); border-radius:16px;
          max-width:480px; width:100%; padding:32px; }
  h1 { margin:0 0 8px; font-size:24px; letter-spacing:-0.02em; }
  .text { color:#8a8f98; font-size:14px; line-height:1.6; margin:0 0 16px; }
  .img { max-width:100%; border-radius:12px; border:1px solid rgba(255,255,255,0.08); margin-bottom:14px; }
  .prod { display:flex; justify-content:space-between; align-items:center; gap:12px; padding:12px 14px;
          border:1px solid rgba(255,255,255,0.08); border-radius:10px; margin-bottom:10px; background:rgba(255,255,255,0.02); }
  .prod label { display:flex; gap:10px; align-items:center; flex:1; font-size:14px; }
  .price { color:#a5b4fc; font-weight:600; font-size:14px; }
  .lbl { display:block; font-size:12px; color:#8a8f98; margin:12px 0 4px; }
  .chk { display:flex; align-items:center; gap:8px; font-size:14px; margin:8px 0; color:#d0d6e0; }
  input[type=text],input[type=email],input[type=tel],textarea,.coupon {
    width:100%; padding:10px 12px; border-radius:8px; border:1px solid rgba(255,255,255,0.08);
    background:rgba(255,255,255,0.02); color:#f7f8f8; font-size:14px; margin-bottom:10px; }
  hr { border:0; border-top:1px solid rgba(255,255,255,0.08); margin:16px 0; }
  .total { display:flex; justify-content:space-between; padding-top:12px; margin-top:8px;
           border-top:1px dashed rgba(255,255,255,0.12); font-weight:600; }
  button { width:100%; padding:12px; border:0; border-radius:10px; font-size:15px; font-weight:600;
           color:#fff; cursor:pointer; margin-top:12px; background:#5e6ad2; }
  button:hover { background:#828fff; }
  .brand { text-align:center; color:#62666d; font-size:11px; margin-top:16px; }
</style></head><body>
<form class="card" id="of" data-slug="{{.Slug}}">
  <h1>{{.Title}}</h1>
{{range .Blocks}}
{{if eq .Type "text"}}  <p class="text">{{.Value}}</p>
{{else if eq .Type "image"}}{{if .Src}}  <img class="img" src="{{.Src}}" alt="">
{{end}}{{else if eq .Type "divider"}}  <hr>
{{else if eq .Type "product"}}  <div class="prod"><label><input type="checkbox" class="p" data-id="{{.ProductID}}" data-price="{{.PriceSen}}"> {{.Value}}</label><span class="price">{{if .HasPrice}}RM{{printf "%.2f" (divf .PriceSen 100)}}{{end}}</span></div>
{{else if eq .Type "name"}}  <label class="lbl">{{.Value}}{{if .Required}} *{{end}}</label><input type="text" name="full_name"{{if .Required}} required{{end}}>
{{else if eq .Type "email"}}  <label class="lbl">Emel{{if .Required}} *{{end}}</label><input type="email" name="email" required>
{{else if eq .Type "phone"}}  <label class="lbl">{{.Value}}</label><input type="tel" name="phone">
{{else if eq .Type "textarea"}}  <label class="lbl">{{.Value}}</label><textarea name="notes" rows="2"></textarea>
{{else if eq .Type "checkbox"}}  <label class="chk"><input type="checkbox" class="extra" value="{{.Value}}"> {{.Value}}</label>
{{else if eq .Type "coupon"}}  <label class="lbl">Kod kupon</label><input type="text" name="coupon" class="coupon">
{{end}}
{{end}}
  <div class="total"><span>Jumlah</span><span id="total">RM0.00</span></div>
  <button type="submit" form="of">Teruskan ke Pembayaran</button>
  <p class="brand">Dikuasakan oleh <strong>JomForm</strong></p>
</form>
<script>
(function(){
  var boxes=document.querySelectorAll('.p');
  function upd(){var t=0;boxes.forEach(function(b){if(b.checked)t+=Number(b.dataset.price||0)});
    document.getElementById('total').textContent='RM'+(t/100).toFixed(2);}
  boxes.forEach(function(b){b.addEventListener('change',upd)});
  document.getElementById('of').addEventListener('submit',function(e){
    e.preventDefault();
    var items=[];boxes.forEach(function(b){if(b.checked)items.push({product_id:b.dataset.id,quantity:1})});
    if(!items.length){alert('Pilih sekurang-kurangnya satu produk');return;}
    var body={client:{full_name:(e.target.full_name||{}).value||(document.querySelector('[name=full_name]')||{}).value||'',
      email:(document.querySelector('[name=email]')||{}).value||'',
      phone:(document.querySelector('[name=phone]')||{}).value||''},
      items:items,payment_method:'chip'};
    fetch(location.pathname+'/order',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)})
      .then(function(r){return r.json()}).then(function(d){
        if(d.checkout_url){location.href=d.checkout_url;}else if(d.next){location.href=d.next;}
        else{alert(d.error||'Ralat');}});
  });
})();
</script>
</body></html>`))
