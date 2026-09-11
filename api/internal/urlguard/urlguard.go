// Package urlguard owns the ONE scheme rule for caller-supplied URL fields.
//
// Issue #501. A merchant (or their agent, over MCP) can supply image/URL values
// that end up in a buyer-facing `<img src>`, a `<a href>` or a CSS `url(...)`:
// the form theme's logo_url, the workspace landing page's image_url/logo_url,
// a product's images, and a builder image-block src. Go's html/template escapes
// by CONTEXT, so a value carrying a scheme it does not consider safe (`data:`,
// `javascript:`, `ftp:`, ...) is silently replaced with the sentinel
// `#ZgotmplZ` in the emitted attribute — the page still returns 200, the image
// never loads, and nothing errors. That is the same silent symptom class as
// issue #499, but the source is the OPPOSITE: the #499 value was
// server-generated (so it is trusted by type), while these values are caller
// input (so they must be REJECTED at write time, never trusted).
//
// This package is deliberately small and dependency-free so both write layers
// (internal/httpapi and internal/mcpsrv) import the SAME rule and it cannot
// drift into hand-rolled copies again. It is NOT internal/ssrfguard: that
// validator accepts http as well as https and resolves DNS, because it guards
// outbound fetching (webhooks). Here we only care about the scheme a browser
// will see inside the buyer's page, so a strict `https://` prefix check with no
// DNS is the correct — and sufficient — tool.
//
// Media-library compatibility: uploaded assets are addressed by
// objstore.ObjectStore.PublicURL, which returns `{S3_PUBLIC_BASE}/{key}` — an
// ABSOLUTE, config-derived `https://` URL (e.g.
// https://jomform.com/media/u/{account}/{uuid}.jpg). https-only therefore does
// not break a single uploaded asset, and no relative-path exception is needed
// (or wanted — it would weaken the rule).
package urlguard

import (
	"fmt"
	"strings"
)

// Scheme is the only URL scheme accepted for caller-supplied image/URL fields.
const Scheme = "https://"

// Valid reports whether raw is an acceptable value for a caller-supplied
// image/URL field: the empty string (meaning "unset" or "keep the stored
// value") or an absolute https:// URL. Surrounding whitespace is ignored, so a
// value pasted with a stray space is judged on its scheme.
//
// The scheme comparison is case-insensitive — URL schemes are case-insensitive
// (RFC 3986 §3.1) and html/template's own URL filter lowercases the scheme
// before deciding whether it is safe, so `HTTPS://…` renders exactly like
// `https://…` and must not be rejected.
func Valid(raw string) bool {
	v := strings.TrimSpace(raw)
	if v == "" {
		return true
	}
	return strings.HasPrefix(strings.ToLower(v), Scheme)
}

// InvalidMessage is the user-facing rejection message for one field. It is the
// single wording used by every write path (REST returns it as the 400 body, MCP
// returns it as the tool error, the dashboard shows it inline), so a merchant
// sees the same explanation wherever they entered the value. The wording is
// Malay like every other API validation message in this repo.
func InvalidMessage(field string) string {
	return fmt.Sprintf("%s mesti bermula dengan https:// — skema lain (data:, http:, javascript:, …) ditolak supaya imej tidak rosak pada halaman pembeli", field)
}

// FirstInvalidList returns the first non-empty value in values that is not an
// absolute https:// URL, and whether one was found. An empty entry is skipped:
// it means "no image", not an unsafe scheme.
func FirstInvalidList(values []string) (string, bool) {
	for _, v := range values {
		if strings.TrimSpace(v) == "" {
			continue
		}
		if !Valid(v) {
			return v, true
		}
	}
	return "", false
}

// ValidList reports whether every non-empty value in values passes Valid.
func ValidList(values []string) bool {
	_, bad := FirstInvalidList(values)
	return !bad
}

// CheckConfig returns the caller-supplied URL field in a form config map that
// violates the rule, plus the offending value. It covers the two config shapes
// that reach a rendered URL context:
//
//   - blocks[].src — the builder image block, emitted as `<img src="{{.Src}}">`
//     (render_blocks.go).
//   - products[].images — the per-form snapshot of each referenced product's
//     images. The snapshot is the fallback the renderer uses for a product row
//     that is archived or gone (#461), so an unsafe scheme stored here can
//     still reach the buyer's page.
//
// field is "" when the config is clean. Non-string / wrong-typed values are
// ignored: the renderer type-asserts and drops them, so they cannot reach a URL
// context.
func CheckConfig(cfg map[string]any) (field, value string) {
	if cfg == nil {
		return "", ""
	}
	if raw, ok := cfg["blocks"].([]any); ok {
		for _, b := range raw {
			bm, ok := b.(map[string]any)
			if !ok {
				continue
			}
			src, ok := bm["src"].(string)
			if !ok {
				continue
			}
			if !Valid(src) {
				return "blocks[].src", src
			}
		}
	}
	if raw, ok := cfg["products"].([]any); ok {
		for _, p := range raw {
			pm, ok := p.(map[string]any)
			if !ok {
				continue
			}
			rawImgs, ok := pm["images"].([]any)
			if !ok {
				continue
			}
			for _, im := range rawImgs {
				s, ok := im.(string)
				if !ok {
					continue
				}
				if !Valid(s) {
					return "products[].images", s
				}
			}
		}
	}
	return "", ""
}

// CheckTheme returns the caller-supplied URL field in a form theme map that
// violates the rule, plus the offending value. Only `logo_url` is a URL in the
// theme; the other keys are colours, fonts, layout and CSS/JS. "" when clean.
func CheckTheme(theme map[string]any) (field, value string) {
	if theme == nil {
		return "", ""
	}
	logo, ok := theme["logo_url"].(string)
	if !ok {
		return "", ""
	}
	if !Valid(logo) {
		return "logo_url", logo
	}
	return "", ""
}
