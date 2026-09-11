package httpapi

// Issue #456: postMessage-based auto-height for the iframe embed. The public
// form renderers must include the height reporter, which only fires when the
// page is actually framed (window.self !== window.top) and posts
// {type:"jomform:height", height:<scrollHeight>} to the parent. Pure template
// test — no DB.

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
)

// TestIframeHeightScriptFramedOnly asserts the reporter is a no-op on a direct
// visit and posts the jomform:height message only from inside a frame.
func TestIframeHeightScriptFramedOnly(t *testing.T) {
	js := iframeHeightJS
	// The reporter must be a complete <script> block emitted verbatim — an
	// earlier bug shipped it bare and html/template escaped the JS, so it never
	// ran. Guard against that regression.
	if !strings.HasPrefix(strings.TrimSpace(js), "<script>") || !strings.Contains(js, "</script>") {
		t.Fatal("height script must be wrapped in a <script>...</script> block")
	}
	// Direct visits must return before any postMessage.
	if !strings.Contains(js, "window.self===window.top") {
		t.Error("height script must detect a direct visit (window.self===window.top)")
	}
	if !strings.Contains(js, "return;") {
		t.Error("height script must bail out on a direct visit")
	}
	// The framed path posts the documented message shape.
	if !strings.Contains(js, "window.parent.postMessage") {
		t.Error("height script must postMessage to the parent")
	}
	if !strings.Contains(js, "'jomform:height'") {
		t.Error("height script must post the jomform:height type")
	}
	if !strings.Contains(js, "scrollHeight") {
		t.Error("height script must report document scrollHeight")
	}
	// Tracking dynamic content: resize + observers.
	for _, want := range []string{"addEventListener('load'", "addEventListener('resize'", "ResizeObserver", "MutationObserver"} {
		if !strings.Contains(js, want) {
			t.Errorf("height script missing %q (load/resize/mutation tracking)", want)
		}
	}
}

// TestBlockFormRendersHeightScript asserts the block renderer emits the height
// reporter so an embedded payment form auto-resizes.
func TestBlockFormRendersHeightScript(t *testing.T) {
	var buf bytes.Buffer
	err := blockTmpl.Execute(&buf, map[string]any{
		"Slug": "demo", "Title": "Demo", "Blocks": []struct{}{},
		"Lang": "en", "Ls": formLangs["en"],
		"HeightJS": template.HTML(iframeHeightJS),
	})
	if err != nil {
		t.Fatalf("blockTmpl.Execute: %v", err)
	}
	body := buf.String()
	// The <script> block must be emitted verbatim (not HTML-escaped).
	if !strings.Contains(body, "<script>(function(){") {
		t.Error("block form height reporter is not emitted verbatim inside a <script> block")
	}
	if strings.Contains(body, "&lt;script&gt;") {
		t.Error("block form height reporter was HTML-escaped (would never execute)")
	}
	if !strings.Contains(body, "jomform:height") {
		t.Error("block form does not include the iframe height reporter")
	}
}

// TestResponseFormRendersHeightScript asserts the response-only renderer emits
// the height reporter too.
func TestResponseFormRendersHeightScript(t *testing.T) {
	var buf bytes.Buffer
	err := responseTmpl.Execute(&buf, map[string]any{
		"Slug": "demo", "Title": "Demo", "Fields": []struct{}{},
		"Lang": "en", "Ls": formLangs["en"],
		"HeightJS": template.HTML(iframeHeightJS),
	})
	if err != nil {
		t.Fatalf("responseTmpl.Execute: %v", err)
	}
	body := buf.String()
	if !strings.Contains(body, "<script>(function(){") {
		t.Error("response form height reporter is not emitted verbatim inside a <script> block")
	}
	if !strings.Contains(body, "jomform:height") {
		t.Error("response form does not include the iframe height reporter")
	}
}
