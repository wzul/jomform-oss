package httpapi

// Card t_7879f397 — the `image` arm of the response template is GUARDED:
//
//	{{else if eq .Type "image"}}{{if .Src}}  <img class="img…" src="{{.Src}}" alt=""…>
//	{{end}}{{else if eq .Type "divider"}}  <hr…>
//
// An image block with no src is a normal merchant state (the builder offers the
// block before an upload, and clearing an upload leaves an empty src), so it must
// paint NOTHING rather than `<img src="">` — a broken-image glyph on the buyer's
// page, HTTP 200, nothing in the log. No shipped test pinned that arm: the
// static-block tests all carry an image WITH a src, so deleting the `{{if .Src}}`
// guard together with its matching `{{end}}` (the template stays parseable — a
// dangling `{{end}}` would panic at init and the suite does catch that) left
// every response-form test GREEN while a no-src block started emitting
// `<img class="img" src="" alt="">`.
//
// This test drives the real call site (renderResponseForm) with one image that
// HAS a src and two that do not, and asserts both halves on the SAME page: the
// real image is still painted — the guard must not over-reach and swallow it —
// and the page carries exactly one `<img>` tag, so any second one can only come
// from a no-src block. The `<img` count is exact because the response template
// emits no other image: the only other <img> in it is the theme logo, behind
// `{{if .LogoURL}}`, and themedForm carries no logo.

import (
	"regexp"
	"strings"
	"testing"
)

// imgTags returns every `<img …>` tag on the page, so a failure names what was
// actually emitted instead of dumping the whole document.
func imgTags(body string) []string {
	return regexp.MustCompile(`<img[^>]*>`).FindAllString(body, -1)
}

func TestResponseFormImageBlockWithNoSrcRendersNoImg(t *testing.T) {
	cfg := map[string]any{"mode": "response", "blocks": []any{
		// The merchant's uploaded image: still painted, once.
		map[string]any{"id": "im-src", "type": "image", "src": "https://cdn.example/with.png"},
		// A block with no src key at all — a freshly placed, not-yet-filled block.
		map[string]any{"id": "im-none", "type": "image", "value": "Banner"},
		// An image block whose src was cleared to the empty string.
		map[string]any{"id": "im-empty", "type": "image", "src": ""},
	}}
	body := renderResponsePage(t, "image-src-guard", cfg)

	if !strings.Contains(body, `<img class="img" src="https://cdn.example/with.png" alt="">`) {
		t.Errorf("the image block WITH a src is not on the page — the {{if .Src}} guard over-reaches and swallows a real image (img tags: %v)",
			imgTags(body))
	}
	// Exactly one image tag: the one block above that has a src.
	if tags := imgTags(body); len(tags) != 1 {
		t.Errorf("the page carries %d <img> tags, want exactly 1 (the block that has a src) — a no-src image block painted a broken image: %v",
			len(tags), tags)
	}
	// The mutated shape, named: the empty/broken image a buyer would see.
	if strings.Contains(body, `src=""`) {
		t.Errorf("the page emits an image with an empty src (a broken-image glyph for the buyer, HTTP 200, nothing logged)")
	}
}
