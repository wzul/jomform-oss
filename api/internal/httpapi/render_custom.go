// Custom HTML response-only form rendering (issue #255). A merchant's AI agent
// supplies raw HTML for the form body plus a field-registration contract. The
// HTML is sanitized and rendered inside a sandboxed iframe with a strict CSP so
// it can never execute scripts or exfiltrate data. On submit the buyer's
// values are POSTed to /order as {client, fields:{field_id:value}} and the
// server records the submission from the registered fields only.
package httpapi

import (
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"

	"github.com/wzul/jomform/api/internal/customform"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// renderCustomForm renders a custom-HTML response-only form (issue #255). The
// agent-authored HTML is sanitized and embedded in a sandboxed iframe with a
// strict Content-Security-Policy. Returns false when the form has no custom
// HTML (caller falls back to the block renderer).
func (h *PublicHandlers) renderCustomForm(w http.ResponseWriter, r *http.Request, f *dbstore.Form) bool {
	cfg := customform.FromConfig(f.Config)
	if cfg == nil || !customform.Enabled(f.Config) {
		return false
	}
	// Sanitize at render time — never trust the stored HTML verbatim.
	safeHTML := customform.SanitizeHTML(cfg.HTML)
	// bodyJS is the sanitized body encoded as a JSON string literal, safe to
	// embed directly in the page's JS (json.Marshal escapes quotes, backslashes
	// and control chars, and HTML-escapes <, >, & for script contexts).
	bodyJS, _ := json.Marshal(safeHTML)

	ls := h.effectiveLangStrings(r.Context(), f)
	themePrimary, themeBg := "", ""
	if f.Theme != nil {
		if v, ok := f.Theme["primary"].(string); ok {
			themePrimary = template.HTMLEscapeString(v)
		}
		if v, ok := f.Theme["background"].(string); ok {
			themeBg = template.HTMLEscapeString(v)
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Issue #255: the custom form body is rendered in a sandboxed iframe. The
	// CSP forbids external scripts, inline JS, remote resources and form
	// submission to other origins; the iframe sandbox (no scripts, no
	// same-origin, no top navigation) is a second layer. The iframe cannot
	// exfiltrate data.
	w.Header().Set("Content-Security-Policy",
		"default-src 'none'; style-src 'unsafe-inline'; img-src data:; form-action 'self'; frame-ancestors 'self'")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")

	if err := customFormTmpl.Execute(w, map[string]any{
		"Slug": f.Slug, "Title": f.Title,
		"BodyJS":       template.JS(bodyJS),
		"ThemePrimary": themePrimary, "ThemeBg": themeBg, "LogoURL": f.LogoURL,
		"Lang": ls.Lang,
		"Ls":   ls,
		// Issue #244: hidden honeypot + render-timestamp fields ride on the
		// fields map so the deterministic anti-spam layers still apply.
		"AntiSpam": template.HTML(antiSpamRenderFields()),
	}); err != nil {
		slog.Error("form_render_custom", "err", err)
	}
	return true
}

// customFormTmpl renders the custom-HTML response form. The sanitized body is
// placed inside a sandboxed iframe; the surrounding page owns the submit
// button and the anti-spam fields. The iframe's form is submitted via
// postMessage to the parent, which POSTs {client, fields} to /order.
var customFormTmpl = template.Must(template.New("custom").Parse(`<!doctype html>
<html lang="{{.Lang}}"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}}</title>
<style>
  {{if .ThemePrimary}}:root { --jf-primary: {{.ThemePrimary}}; }{{end}}
  {{if .ThemeBg}}body { background: {{.ThemeBg}}; }{{end}}
</style>
<style>
  :root { color-scheme: dark; }
  * { box-sizing: border-box; }
  body { margin:0; font-family: system-ui,-apple-system,"Segoe UI",sans-serif;
         background:#f7f8f8; color:#17181c; min-height:100vh;
         display:flex; flex-direction:column; align-items:center; justify-content:center; padding:24px; }
  .logo { max-height:56px; max-width:180px; object-fit:contain; margin:0 auto 12px; display:block; }
  .card { background:#ffffff; border:1px solid #e6e8ec; border-radius:16px;
          max-width:560px; width:100%; padding:24px; }
  h1 { margin:0 0 8px; font-size:24px; letter-spacing:-0.02em; }
  .frame-wrap { border:1px solid #e6e8ec; border-radius:12px; overflow:hidden; }
  iframe#jf-custom { width:100%; height:520px; border:0; display:block; background:#ffffff; }
  button { width:100%; padding:12px; border:0; border-radius:10px; font-size:15px; font-weight:600;
           color:#fff; cursor:pointer; margin-top:12px; background:#5e6ad2; }
  button:hover { background:#828fff; }
  .ok { text-align:center; padding:24px 0; }
  .ok h1 { color:#10b981; }
  .ok p { color:#6b7280; font-size:14px; }
  .ok .msg { color:#17181c; font-size:14px; line-height:1.6; margin-top:8px; }
  .ok .msg a { color:#4f46e5; }
</style></head><body>
<div class="card">
  <h1>{{.Title}}</h1>
{{if .LogoURL}}<img class="logo" src="{{.LogoURL}}" alt="">{{end}}
  <div class="frame-wrap"><iframe id="jf-custom" sandbox="allow-forms" title="{{.Title}}"></iframe></div>
  {{.AntiSpam}}
  <button type="button" id="jf-submit">{{$.Ls.SubmitResponse}}</button>
</div>
<script>
(function(){
  var frame=document.getElementById('jf-custom');
  var form=document.getElementById('jf-submit');
  // The sanitized body is written into the iframe via srcdoc. The iframe is
  // sandboxed (allow-forms only) and its CSP forbids scripts, so the body can
  // never execute JS or load remote resources.
  frame.srcdoc='<!doctype html><html><head><meta charset="utf-8">'+
    '<meta http-equiv="Content-Security-Policy" content="default-src \'none\'; style-src \'unsafe-inline\'; img-src data:; form-action \'self\'">'+
    '</head><body>'+{{.BodyJS}}+'</body></html>';
  // The iframe's own <form> submits to a data: URL (form-action 'self' is
  // blocked by the sandbox), so we intercept it and forward the values to the
  // parent, which POSTs them to /order.
  frame.addEventListener('load',function(){
    try{
      var doc=frame.contentDocument;
      var f=doc.querySelector('form');
      if(!f)return;
      f.addEventListener('submit',function(e){
        e.preventDefault();
        var fields={};
        var els=f.querySelectorAll('[name]');
        els.forEach(function(el){
          if(el.type==='checkbox'){fields[el.name]=el.checked?'1':'';}
          else if(el.type==='radio'){if(el.checked)fields[el.name]=el.value;}
          else if(el.type==='file'){fields[el.name]=el.files&&el.files[0]?el.files[0].name:'';}
          else{fields[el.name]=el.value;}
        });
        window.parent.postMessage({jfCustomSubmit:true,fields:fields},'*');
      });
    }catch(err){}
  });
  window.addEventListener('message',function(ev){
    if(!ev.data||!ev.data.jfCustomSubmit)return;
    var fields=ev.data.fields||{};
    var body={client:{full_name:'',email:'',phone:''},fields:fields};
    var nm=frame.contentDocument&&frame.contentDocument.querySelector('input[type=text]');
    if(nm)body.client.full_name=nm.value;
    var em=frame.contentDocument&&frame.contentDocument.querySelector('input[type=email]');
    if(em)body.client.email=em.value;
    var ph=frame.contentDocument&&frame.contentDocument.querySelector('input[type=tel]');
    if(ph)body.client.phone=ph.value;
    fetch(location.pathname+'/order',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)})
      .then(function(r){return r.json()})
      .then(function(d){
        if(d.status==='submitted'){
          if(d.confirmation==='redirect'&&d.redirect_url){
            if(window.self!==window.top){window.top.location.href=d.redirect_url;}
            else{location.href=d.redirect_url;}
            return;
          }
          var msg=d.message||'{{$.Ls.ResponseReceived}}';
          var card=document.querySelector('.card');
          card.innerHTML='<div class="ok"><h1>✓ {{$.Ls.ThankYou}}</h1><div class="msg">'+msg+'</div></div>';
        } else {
          alert('{{$.Ls.ErrorAlert}}'+(d.error?': '+d.error:''));
        }
      })
      .catch(function(){alert('{{$.Ls.ErrorAlert}}');});
  });
  form.addEventListener('click',function(){
    try{
      var doc=frame.contentDocument;
      var f=doc&&doc.querySelector('form');
      if(f){f.requestSubmit?f.requestSubmit():f.submit();}
    }catch(err){}
  });
})();
</script>
</body></html>`))
