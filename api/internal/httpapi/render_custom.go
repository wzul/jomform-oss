// Custom HTML response-only form rendering (issue #255). A merchant's AI agent
// supplies raw HTML for the form body plus a field-registration contract. The
// HTML is sanitized and rendered inside a sandboxed iframe with a strict CSP so
// it can never execute scripts or exfiltrate data. On submit the buyer's
// values are POSTed to /order as {client, fields:{field_id:value}} and the
// server records the submission from the registered fields only.
package httpapi

import (
	"crypto/rand"
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"
	"strings"

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
	// Card t_8f5d6fb5: same theme wiring as the block renderers — the CTA is
	// painted from the merchant's brand colour, with hover/active/label derived
	// from the same hue (formtheme.go). The inner iframe body is merchant HTML
	// inside a sandbox and keeps its own styling.
	cta := themePrimaryValues(f)
	themeBg := ""
	if f.Theme != nil {
		if v, ok := f.Theme["background"].(string); ok {
			themeBg = template.HTMLEscapeString(v)
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Issue #255: the custom form body is rendered in a sandboxed iframe. The
	// CSP forbids external scripts, inline JS, remote resources and form
	// submission to other origins; the iframe sandbox (no scripts, no top
	// navigation) is a second layer. The iframe cannot execute or navigate.
	//
	// Card t_c5a278c0: the page's OWN scripts must run. Until this card the
	// header carried no script-src at all, so `default-src 'none'` applied to
	// scripts and the browser blocked all three page-owned blocks — the
	// payment-failure renderer (jfPayFail), the custom-form bridge that writes
	// srcdoc and forwards the submit, and the embed height reporter. Measured
	// on prod: jfPayFail undefined, iframe srcdoc empty, zero requests on a
	// Submit click. Two directives fix it without loosening anything the
	// merchant's HTML lives under:
	//
	//   * script-src 'nonce-…' — a fresh nonce per response, carried by exactly
	//     the three page-owned <script> tags. Not 'unsafe-inline': this page
	//     must never admit script by shape. The merchant's HTML is sanitized,
	//     lives in a sandbox="allow-forms allow-same-origin" iframe that
	//     inherits this policy, and carries its own default-src 'none' meta CSP
	//     — with no allow-scripts it cannot execute, and it never learns the
	//     nonce (a script element's nonce attribute is hidden from the DOM).
	//   * connect-src 'self' — connect-src falls back to default-src, which
	//     would block the page's fetch() to /<slug>/order even with the
	//     scripts running. 'self' is the narrowest source that admits its own
	//     route; the bridge never calls another origin.
	nonce := cspNonce()
	w.Header().Set("Content-Security-Policy",
		"default-src 'none'; script-src 'nonce-"+nonce+"'; style-src 'unsafe-inline'; "+
			"img-src data:; connect-src 'self'; form-action 'self'; frame-ancestors 'self'")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")

	if err := customFormTmpl.Execute(w, map[string]any{
		"Slug": f.Slug, "Title": f.Title,
		"BodyJS":  template.JS(bodyJS),
		"ThemeBg": themeBg, "LogoURL": f.LogoURL,
		"CtaPrimary": cta.Primary, "CtaHover": cta.Hover, "CtaActive": cta.Active,
		"CtaLabel": cta.Label, "CtaText": cta.Text,
		"Lang": ls.Lang,
		"Ls":   ls,
		// Card t_c5a278c0: the nonce this response's CSP names; every
		// page-owned <script> must carry it or the browser drops the block.
		"Nonce": nonce,
		// Issue #244: hidden honeypot + render-timestamp fields ride on the
		// fields map so the deterministic anti-spam layers still apply.
		"AntiSpam": template.HTML(antiSpamRenderFields()),
		// Issue #276: noindex on jomform.com subdomains / when the workspace opts
		// out of indexing.
		"NoIndex": h.formNoIndex(r, f),
		// Issue #456: postMessage auto-height for the iframe embed. No-op on a
		// direct visit (the script only runs when framed).
		"HeightJS": template.HTML(noncedScript(iframeHeightJS, nonce)),
		// Card t_e2024698: the buyer-visible payment-failure renderer shared by
		// every public order surface (payfail.go).
		"PayFailJS": payFailJS(ls),
	}); err != nil {
		slog.Error("form_render_custom", "err", err)
	}
	return true
}

// cspNonce returns the nonce for ONE rendered page (card t_c5a278c0).
//
// crypto/rand.Text is 26 base32 characters derived from the OS CSPRNG — well
// over the 128 bits CSP requires of a nonce, and drawn from the alphabet a CSP
// source expression allows, so it needs no attribute escaping. It cannot
// return a predictable value: since Go 1.24 a failing entropy source panics
// inside crypto/rand rather than yielding something guessable.
func cspNonce() string { return rand.Text() }

// noncedScript stamps the response's CSP nonce onto the opening <script> tag of
// a script block the page OWNS (card t_c5a278c0).
//
// The shared constants (iframeHeightJS and friends) are emitted verbatim by the
// renderers that send no script-src, so they must not carry a nonce themselves.
// A renderer that sends `script-src 'nonce-…'` has to put the same nonce on the
// tag, or the browser drops the block — which is exactly the defect this card
// fixes, so this is the one line that keeps the header and the markup in step.
func noncedScript(js string, nonce string) string {
	return strings.Replace(js, "<script>", `<script nonce="`+nonce+`">`, 1)
}

// customFormTmpl renders the custom-HTML response form. The sanitized body is
// placed inside a sandboxed iframe; the surrounding page owns the submit
// button and the anti-spam fields. This page's script hooks the frame's form
// (building one when the merchant wrote none), collects the frame's [name]
// controls and POSTs {client, fields} to /order. The page's own three scripts
// carry the response's CSP nonce, because this page also sends the strict CSP
// that admits them.
var customFormTmpl = template.Must(template.New("custom").Parse(`<!doctype html>
<html lang="{{.Lang}}"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
{{if .NoIndex}}<meta name="robots" content="noindex, nofollow">{{end}}
<title>{{.Title}}</title>
<style>
  /* Card t_8f5d6fb5: the merchant's brand triple, same as the block renderer. */
  :root { --jf-primary: {{.CtaPrimary}}; --jf-primary-hover: {{.CtaHover}};
          --jf-primary-active: {{.CtaActive}}; --jf-primary-label: {{.CtaLabel}};
          --jf-primary-text: {{.CtaText}}; }
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
  /* Card t_8f5d6fb5: the submit CTA is painted from the merchant's brand triple
     declared above, with the label colour the derived pairing measured. */
  button { width:100%; padding:12px; border:0; border-radius:10px; font-size:15px; font-weight:600;
           color:var(--jf-primary-label); cursor:pointer; margin-top:12px;
           background:var(--jf-primary);
           transition:background .15s ease, box-shadow .15s ease; }
  button:hover { background:var(--jf-primary-hover); box-shadow:0 2px 8px rgba(23,24,28,0.12); }
  button:active { background:var(--jf-primary-active); }
  .ok { text-align:center; padding:24px 0; }
  .ok h1 { color:#10b981; }
  .ok p { color:#6b7280; font-size:14px; }
  .ok .msg { color:#17181c; font-size:14px; line-height:1.6; margin-top:8px; }
  .ok .msg a { color:var(--jf-primary-text); }
</style></head><body>
<div class="card">
  <h1>{{.Title}}</h1>
{{if .LogoURL}}<img class="logo" src="{{.LogoURL}}" alt="">{{end}}
  {{/* Card t_c5a278c0: allow-same-origin, NOT allow-scripts. The bridge reads
     this frame's document (contentDocument) to collect the buyer's values and
     to hook the form's submit; a sandbox without allow-same-origin gives the
     frame an opaque origin where contentDocument is null — measured in Chrome,
     which is why the CTA fired no request. Scripts stay blocked (no
     allow-scripts, plus the frame's own meta CSP and the inherited page CSP),
     so the merchant's HTML is still inert. */}}
  <div class="frame-wrap"><iframe id="jf-custom" sandbox="allow-forms allow-same-origin" title="{{.Title}}"></iframe></div>
  {{.AntiSpam}}
  <button type="button" id="jf-submit">{{$.Ls.SubmitResponse}}</button>
</div>
<script nonce="{{.Nonce}}">{{.PayFailJS}}</script>
<script nonce="{{.Nonce}}">
(function(){
  var frame=document.getElementById('jf-custom');
  var form=document.getElementById('jf-submit');
  // The sanitized body is written into the iframe via srcdoc. The iframe is
  // sandboxed (forms only, never scripts) and its CSP forbids scripts, so the
  // body can never execute JS or load remote resources.
  frame.srcdoc='<!doctype html><html><head><meta charset="utf-8">'+
    '<meta http-equiv="Content-Security-Policy" content="default-src \'none\'; style-src \'unsafe-inline\'; img-src data:; form-action \'self\'">'+
    '</head><body>'+{{.BodyJS}}+'</body></html>';
  // Card t_c5a278c0: this submit path needed THREE things to hold and the
  // shipped page satisfied none of them — in a browser the CTA fired zero
  // requests even with the merchant's HTML rendered and the CSP fixed:
  //
  //   1. this page's own scripts have to RUN (the response CSP now admits them
  //      by nonce; before that the browser dropped every block below);
  //   2. this page has to be able to REACH the frame's document. A sandbox
  //      without allow-same-origin gives the frame an opaque origin, where
  //      frame.contentDocument is null — every read below threw, so no submit
  //      hook was ever installed;
  //   3. that document has to contain a <form> to hook. The sanitizer keeps the
  //      merchant's controls but not a <form> element, so one is built here
  //      when the merchant wrote none.
  //
  // Scripts stay forbidden inside the frame (no allow-scripts, plus the frame's
  // own meta CSP and the inherited page CSP), which is what keeps the
  // merchant's HTML inert; allow-same-origin grants only THIS page the read
  // access the bridge has always assumed.
  function frameDoc(){
    try{return frame.contentDocument||null;}catch(err){return null;}
  }
  // ensureForm returns the frame's form, wrapping its body content in one when
  // the merchant wrote none: a submit event (Enter, a merchant
  // <button type=submit>, or this page's CTA) only exists on a form, and a form
  // is also what makes the browser enforce required / pattern / type=email.
  function ensureForm(){
    var doc=frameDoc();
    if(!doc||!doc.body)return null;
    var f=doc.querySelector('form');
    if(!f){
      f=doc.createElement('form');
      while(doc.body.firstChild){f.appendChild(doc.body.firstChild);}
      doc.body.appendChild(f);
    }
    return f;
  }
  function collectFields(){
    var doc=frameDoc(),fields={};
    if(!doc)return fields;
    doc.querySelectorAll('[name]').forEach(function(el){
      if(el.type==='checkbox'){fields[el.name]=el.checked?'1':'';}
      else if(el.type==='radio'){if(el.checked)fields[el.name]=el.value;}
      else if(el.type==='file'){fields[el.name]=el.files&&el.files[0]?el.files[0].name:'';}
      else{fields[el.name]=el.value;}
    });
    return fields;
  }
  // submitOrder POSTs the buyer's values. The submit hook below is installed by
  // THIS script, so it already runs in this realm — the fields are passed
  // straight through. (The old path bounced them via
  // window.parent.postMessage(…,'*') back to this same window: on a direct visit
  // that only addressed itself, and on an EMBEDDED page it would have posted the
  // buyer's values to the embedding site.)
  function submitOrder(){
    var doc=frameDoc();
    var body={client:{full_name:'',email:'',phone:''},fields:collectFields(),
      // Issue #489: QR ordering — echo the ?table=N the QR stand / menu page
      // carried into the form URL so sales.table_number is populated.
      table_number:(new URLSearchParams(location.search).get('table')||'')};
    var nm=doc&&doc.querySelector('input[type=text]');
    if(nm)body.client.full_name=nm.value;
    var em=doc&&doc.querySelector('input[type=email]');
    if(em)body.client.email=em.value;
    var ph=doc&&doc.querySelector('input[type=tel]');
    if(ph)body.client.phone=ph.value;
    fetch(location.pathname+'/order',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)})
      .then(function(r){return r.json()})
      .then(function(d){
        // Card t_e2024698: the payment could not be started — paint the
        // failure in-app with the reference instead of a bare alert.
        if(d&&d.status==='payment_error'){jfPayFail(d);return;}
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
      // Card t_e2024698: a non-JSON answer (an edge error page, a proxy 5xx)
      // used to reach only this generic alert — report it as the same
      // actionable failure now.
      .catch(function(){jfPayFail({});});
  }
  frame.addEventListener('load',function(){
    var f=ensureForm();
    if(!f)return;
    f.addEventListener('submit',function(e){e.preventDefault();submitOrder();});
  });
  form.addEventListener('click',function(){
    var f=ensureForm();
    if(!f)return;
    // requestSubmit runs the frame's own constraint validation, so a required
    // field the buyer left empty keeps the page put (with the browser's message
    // inside the frame) instead of POSTing an incomplete order.
    try{if(f.requestSubmit){f.requestSubmit();}else{f.submit();}}catch(err){}
  });
})();
</script>
{{.HeightJS}}
</body></html>`))
