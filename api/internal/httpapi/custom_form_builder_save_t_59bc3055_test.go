package httpapi

// Card t_59bc3055: the dashboard builder's save on a custom-HTML form no longer
// deletes `config.custom_html` (web/src/app/builder/page.tsx now lays its own
// config over the one it loaded — see web/src/lib/formConfigPreserve.test.ts and
// web/e2e/builder-save-keeps-custom-html.spec.ts). The reason that is safe is
// this precedence: once `custom_html` survives, the `mode` / `blocks` /
// `products` keys the block builder also writes back are INERT, because the
// public page renders the merchant's body and never the blocks.
//
// The claim is a property of two things, and this file pins both:
//
//  1. renderCustomForm accepts a form that carries BOTH keys (it declines only
//     when custom_html is absent or its html is blank — customform.Enabled);
//  2. serveForm reaches it FIRST for a response-mode form and returns, so the
//     block renderers below are never asked (public.go, the
//     `if h.renderCustomForm(w, r, f) { return }` call site).
//
// A stale alternative would be to preserve custom_html and then render the
// blocks — the merchant would see a different form than the one they authored
// and no test above the renderer would notice.

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// customBodyWithBlocks is the config the builder produces from a custom-HTML
// form: the stored section carried forward, plus the block editor's own keys
// (its default blocks — "Full name", a text block — and an empty product list).
const customBodyWithBlocks = `<p>Badan saya</p><input type="text" name="email" required>`

func TestCustomHTMLPrecedenceOverBuilderBlocks(t *testing.T) {
	cfg := map[string]any{
		"mode": "response",
		"custom_html": map[string]any{
			"html":   customBodyWithBlocks,
			"fields": []any{map[string]any{"field_id": "email", "type": "text", "label": "E-mel"}},
		},
		// Exactly what a builder save writes back for a custom form: the block
		// editor's default canvas, which the merchant never composed.
		"blocks": []any{
			map[string]any{"id": "b1", "type": "text", "value": "Nama Penuh"},
			map[string]any{"id": "b2", "type": "email", "value": "E-mel"},
		},
		"products":     []any{},
		"description":  "Nama Penuh",
		"chip_mapping": map[string]any{},
	}
	f := themedForm("checkout", "#5e6ad2", cfg)
	h := &PublicHandlers{}
	rec := httptest.NewRecorder()
	if !h.renderCustomForm(rec, httptest.NewRequest("GET", "http://demo.jomform.com/checkout", nil), f) {
		t.Fatal("renderCustomForm declined a form carrying BOTH custom_html and builder blocks — the public page would render the blocks instead of the merchant's body")
	}
	body := rec.Body.String()
	// The merchant's page, not the builder's.
	if !strings.Contains(body, `<iframe id="jf-custom"`) {
		t.Error("the rendered page has no custom iframe")
	}
	if !strings.Contains(body, "Badan saya") {
		t.Error("the rendered page does not carry the merchant's stored body")
	}
	// The block editor's default blocks must not leak onto the buyer's page as
	// fields — the precedence half that a config-only assertion cannot see.
	if strings.Contains(body, "Nama Penuh") {
		t.Error("the builder's block text reached the custom page")
	}
}

// The control: the same mixed config WITHOUT the custom section is declined by
// the custom renderer, so the test above proves the renderer discriminates
// rather than answering true for everything.
func TestCustomHTMLRendererDeclinesAFormWithoutCustomHTML(t *testing.T) {
	cfg := map[string]any{
		"mode":         "response",
		"blocks":       []any{map[string]any{"id": "b1", "type": "text", "value": "Nama Penuh"}},
		"products":     []any{},
		"description":  "Nama Penuh",
		"chip_mapping": map[string]any{},
	}
	h := &PublicHandlers{}
	rec := httptest.NewRecorder()
	if h.renderCustomForm(rec, httptest.NewRequest("GET", "http://demo.jomform.com/checkout", nil), themedForm("checkout", "#5e6ad2", cfg)) {
		t.Fatal("renderCustomForm claimed a form that has no custom_html — it would render that section for every response form")
	}
}

// And the call site still asks the custom renderer BEFORE the block renderers,
// returning on acceptance. Without this the renderer could be perfect and the
// page would still draw the blocks; with it moved below, both tests above stay
// green while the buyer sees the wrong form.
func TestServeFormAsksTheCustomRendererFirst(t *testing.T) {
	src := apiSource(t, "internal/httpapi/public.go")
	custom := strings.Index(src, "if h.renderCustomForm(w, r, f) {")
	if custom < 0 {
		t.Fatal("serveForm no longer consults renderCustomForm — a custom-HTML form renders through the block path")
	}
	// It must return on acceptance (the block renderers are the fallback).
	after := src[custom:]
	if !strings.HasPrefix(strings.TrimSpace(after[strings.Index(after, "{")+1:]), "return") {
		t.Error("the renderCustomForm branch does not return, so a custom form would ALSO be rendered as blocks")
	}
	blocks := strings.Index(src, "if h.renderResponseForm(w, r, f) {")
	if blocks < 0 {
		t.Fatal("the block renderer call moved — re-anchor this guard")
	}
	if custom > blocks {
		t.Error("renderCustomForm is consulted AFTER the block renderer — the blocks win for a custom form")
	}
}
