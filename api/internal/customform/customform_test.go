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
