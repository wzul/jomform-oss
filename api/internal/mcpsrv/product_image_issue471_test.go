package mcpsrv

// Issue #471: MCP parity for the product image size + layout options exposed
// by set_form_theme. Pure unit tests — the allowlist mirrors the renderer's
// (httpapi/product_image_issue471.go) without a database.

import "testing"

func TestValidProductImageSizeMCP(t *testing.T) {
	for _, ok := range []string{"", "small", "medium", "large", "hidden"} {
		if !validProductImageSize(ok) {
			t.Errorf("validProductImageSize(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"tiny", "SMALL", "52px", "xl", "none"} {
		if validProductImageSize(bad) {
			t.Errorf("validProductImageSize(%q) = true, want false", bad)
		}
	}
}

func TestValidProductImageLayoutMCP(t *testing.T) {
	for _, ok := range []string{"", "thumbnail-left", "image-top"} {
		if !validProductImageLayout(ok) {
			t.Errorf("validProductImageLayout(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"left", "top", "IMAGE-TOP", "card"} {
		if validProductImageLayout(bad) {
			t.Errorf("validProductImageLayout(%q) = true, want false", bad)
		}
	}
}

// TestSetThemeStrProductImageKeys confirms the theme map plumbing: a value is
// stored, and an empty value clears the key (restoring the default).
func TestSetThemeStrProductImageKeys(t *testing.T) {
	theme := map[string]any{}
	setThemeStr(theme, "product_image_size", "large")
	setThemeStr(theme, "product_image_layout", "image-top")
	if theme["product_image_size"] != "large" || theme["product_image_layout"] != "image-top" {
		t.Fatalf("theme keys not set: %+v", theme)
	}
	setThemeStr(theme, "product_image_size", "")
	if _, ok := theme["product_image_size"]; ok {
		t.Error("empty value must clear the theme key")
	}
}
