package urlguard

// Issue #501: unit tests for the ONE shared scheme rule used by every
// caller-supplied image/URL write path (REST + MCP). The rule is an
// https-only allowlist: empty (unset / "keep the stored value") is allowed,
// an absolute https:// URL is allowed, and every other scheme is rejected.

import "testing"

func TestValidAllowsEmptyAndHTTPSOnly(t *testing.T) {
	for _, ok := range []string{
		"",    // unset — "keep the stored value"
		"   ", // whitespace-only is still unset
		"https://cdn.example.com/a.png",
		"https://jomform.com/media/u/acct/uuid.jpg",
		"  https://cdn.example.com/a.png  ", // pasted with a stray space
		"HTTPS://CDN.example.com/a.png",     // scheme prefix is what matters
	} {
		if !Valid(ok) {
			t.Errorf("Valid(%q) = false, want true", ok)
		}
	}
}

func TestValidRejectsUnsafeSchemes(t *testing.T) {
	// Every one of these reaches an <img src> or <a href> and is silently
	// rewritten by html/template to #ZgotmplZ, or worse (javascript:).
	for _, bad := range []string{
		"data:image/png;base64,AAAA",
		"data:text/html,<script>alert(1)</script>",
		"javascript:alert(1)",
		"JavaScript:alert(1)",
		"http://cdn.example.com/a.png", // http is NOT accepted (buyer-facing page)
		"ftp://cdn.example.com/a.png",
		"//cdn.example.com/a.png", // scheme-relative — no https:// prefix
		"/u/acct/uuid.jpg",        // relative path: no exception, media URLs are absolute
		"u/acct/uuid.jpg",
		"cdn.example.com/a.png",
		"vbscript:msgbox(1)",
		"file:///etc/passwd",
	} {
		if Valid(bad) {
			t.Errorf("Valid(%q) = true, want false", bad)
		}
	}
}

func TestInvalidMessageNamesTheField(t *testing.T) {
	msg := InvalidMessage("logo_url")
	if msg == "" {
		t.Fatal("InvalidMessage returned empty string")
	}
	for _, want := range []string{"logo_url", "https://"} {
		if !contains(msg, want) {
			t.Errorf("InvalidMessage(%q) = %q, must mention %q", "logo_url", msg, want)
		}
	}
}

func TestValidListAndFirstInvalidList(t *testing.T) {
	if !ValidList(nil) || !ValidList([]string{}) {
		t.Error("nil/empty list must be valid (no images configured)")
	}
	if !ValidList([]string{"", "https://cdn.example.com/a.png", ""}) {
		t.Error("empty entries are 'no image' and must be skipped, not rejected")
	}
	if ValidList([]string{"https://cdn.example.com/a.png", "data:image/png;base64,AA"}) {
		t.Error("a single unsafe scheme must make the whole list invalid")
	}
	got, bad := FirstInvalidList([]string{"", "javascript:alert(1)"})
	if !bad || got != "javascript:alert(1)" {
		t.Errorf("FirstInvalidList = (%q, %v), want (javascript:alert(1), true)", got, bad)
	}
	if _, bad := FirstInvalidList([]string{"", "https://ok.example.com/a.png"}); bad {
		t.Error("FirstInvalidList reported an invalid value for an all-good list")
	}
}

func TestCheckThemeOnlyFlagsLogoURL(t *testing.T) {
	// The other theme keys are colours/fonts/layout/CSS and must never be
	// judged as URLs.
	clean := map[string]any{
		"primary": "#5e6ad2", "background": "#ffffff", "font_family": "system-ui",
		"custom_css": "body{background:url(data:image/png;base64,AA)}", // CSS, not our URL field
		"logo_url":   "https://jomform.com/media/u/acct/uuid.png",
	}
	if f, v := CheckTheme(clean); f != "" {
		t.Errorf("CheckTheme flagged %q=%q on a clean theme", f, v)
	}
	for _, bad := range []string{"data:image/png;base64,AA", "http://x/y.png", "javascript:alert(1)"} {
		f, v := CheckTheme(map[string]any{"logo_url": bad})
		if f != "logo_url" || v != bad {
			t.Errorf("CheckTheme(logo_url=%q) = (%q, %q), want (logo_url, %q)", bad, f, v, bad)
		}
	}
	// Empty / wrong-typed values are not violations.
	if f, _ := CheckTheme(map[string]any{"logo_url": ""}); f != "" {
		t.Errorf("empty logo_url flagged as %q", f)
	}
	if f, _ := CheckTheme(map[string]any{"logo_url": 42}); f != "" {
		t.Errorf("non-string logo_url flagged as %q", f)
	}
	if f, _ := CheckTheme(nil); f != "" {
		t.Errorf("nil theme flagged as %q", f)
	}
}

func TestCheckConfigFlagsBlockSrcAndSnapshotImages(t *testing.T) {
	clean := map[string]any{
		"blocks": []any{
			map[string]any{"id": "b1", "type": "image", "src": "https://jomform.com/media/u/a/x.png"},
			map[string]any{"id": "b2", "type": "text", "value": "data: not a src"},
		},
		"products": []any{
			map[string]any{"id": "p1", "images": []any{"https://jomform.com/media/u/a/y.png", ""}},
		},
	}
	if f, v := CheckConfig(clean); f != "" {
		t.Errorf("CheckConfig flagged %q=%q on a clean config", f, v)
	}
	f, v := CheckConfig(map[string]any{
		"blocks": []any{map[string]any{"id": "b1", "type": "image", "src": "data:image/png;base64,AA"}},
	})
	if f != "blocks[].src" || v != "data:image/png;base64,AA" {
		t.Errorf("block src: got (%q, %q)", f, v)
	}
	f, v = CheckConfig(map[string]any{
		"products": []any{map[string]any{"id": "p1", "images": []any{"javascript:alert(1)"}}},
	})
	if f != "products[].images" || v != "javascript:alert(1)" {
		t.Errorf("snapshot images: got (%q, %q)", f, v)
	}
	// Wrong-typed internals are ignored (the renderer drops them too).
	if f, _ := CheckConfig(map[string]any{"blocks": []any{"not-a-map"}}); f != "" {
		t.Errorf("non-map block flagged as %q", f)
	}
	if f, _ := CheckConfig(map[string]any{"products": []any{map[string]any{"images": "not-a-list"}}}); f != "" {
		t.Errorf("non-list images flagged as %q", f)
	}
	if f, _ := CheckConfig(nil); f != "" {
		t.Errorf("nil config flagged as %q", f)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
