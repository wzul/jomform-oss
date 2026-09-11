package httpapi

// Issue #471: product image size + layout.
//
// The product image used to be a hardcoded 52x52 thumbnail (.pimg) with the
// image inline inside the <label> next to the product name. A merchant — in
// particular a restaurant menu (QR ordering, #438/#439) — wants a bigger food
// photo, or a menu-card style image above the name.
//
// Two per-form theme keys drive it (set through Form Settings -> General, the
// REST PATCH /v1/rest/forms/{id} theme map, or MCP set_form_theme):
//
//   product_image_size   small (52px, default) | medium (~96px) | large (~160px) | hidden
//   product_image_layout thumbnail-left (default) | image-top (menu card)
//
// The rendered <img> src is resolved to the media variant that matches the
// chosen size (thumbnail / medium / full), which the upload pipeline already
// generates (#382). The image URL itself keeps coming from the LIVE product row
// (#461) — this file only rewrites the variant suffix of that URL.
//
// Values are validated against a small allowlist so an unknown string can never
// reach the renderer; an unknown value falls back to the default (small /
// thumbnail-left), which is exactly the pre-#471 look.

import (
	"strings"

	"github.com/wzul/jomform/api/internal/objstore"
)

const (
	// ProductImageSizeSmall is the default 52px thumbnail (pre-#471 look).
	ProductImageSizeSmall = "small"
	// ProductImageSizeMedium renders a ~96px image.
	ProductImageSizeMedium = "medium"
	// ProductImageSizeLarge renders a ~160px image.
	ProductImageSizeLarge = "large"
	// ProductImageSizeHidden renders no product image at all.
	ProductImageSizeHidden = "hidden"

	// ProductImageLayoutThumbnailLeft is the default: image inline to the left
	// of the product name inside the label.
	ProductImageLayoutThumbnailLeft = "thumbnail-left"
	// ProductImageLayoutImageTop is the menu-card style: a full-width image
	// above the name/price row.
	ProductImageLayoutImageTop = "image-top"
)

// ValidProductImageSize reports whether v is an allowed product_image_size.
// The empty string is allowed and means "unset" (defaults to small).
func ValidProductImageSize(v string) bool {
	switch v {
	case "", ProductImageSizeSmall, ProductImageSizeMedium, ProductImageSizeLarge, ProductImageSizeHidden:
		return true
	}
	return false
}

// ValidProductImageLayout reports whether v is an allowed product_image_layout.
// The empty string is allowed and means "unset" (defaults to thumbnail-left).
func ValidProductImageLayout(v string) bool {
	switch v {
	case "", ProductImageLayoutThumbnailLeft, ProductImageLayoutImageTop:
		return true
	}
	return false
}

// productImageSizeOrDefault maps an unset/invalid size to the default small.
func productImageSizeOrDefault(v string) string {
	if ValidProductImageSize(v) && v != "" {
		return v
	}
	return ProductImageSizeSmall
}

// productImageLayoutOrDefault maps an unset/invalid layout to thumbnail-left.
func productImageLayoutOrDefault(v string) string {
	if ValidProductImageLayout(v) && v != "" {
		return v
	}
	return ProductImageLayoutThumbnailLeft
}

// productImageVariantName maps a size to the media variant that fits it
// (issue #382): thumbnail for small, medium for medium, full for large.
func productImageVariantName(size string) string {
	switch productImageSizeOrDefault(size) {
	case ProductImageSizeMedium:
		return string(objstore.SizeMedium)
	case ProductImageSizeLarge:
		return string(objstore.SizeFull)
	default:
		return string(objstore.SizeThumb)
	}
}

// ProductImageVariantURL rewrites a product image URL to the media variant for
// the chosen size. It only touches URLs that point at a canonical JomForm media
// object (`.../u/{account}/{uuid}.{png|jpg|jpeg|webp}`), because only those get
// generated variants (#382). Every other URL — an external image, a GIF/SVG
// (stored as-is, no variants), a URL with a query string, or an unset/hidden
// size — is returned unchanged, so nothing is ever broken by the rewrite.
//
// The renderer additionally ships a client-side fallback that strips the
// variant suffix when a variant object is missing (e.g. an image uploaded
// before #382), so an older image still displays.
func ProductImageVariantURL(imageURL, size string) string {
	if imageURL == "" || strings.ContainsAny(imageURL, "?#") {
		return imageURL
	}
	idx := strings.Index(imageURL, "/u/")
	if idx < 0 {
		return imageURL
	}
	key := imageURL[idx+1:]
	if objstore.AccountIDFromKey(key) == "" {
		return imageURL
	}
	dot := strings.LastIndex(key, ".")
	if dot <= 0 {
		return imageURL
	}
	if !sizedImageExt(strings.ToLower(key[dot:])) {
		return imageURL
	}
	return imageURL[:len(imageURL)-(len(key)-dot)] + "-" + productImageVariantName(size) + key[dot:]
}

// productImageForSize returns the image URL to render for a size: the matching
// variant, or "" when the merchant chose hidden.
func productImageForSize(imageURL, size string) string {
	if productImageSizeOrDefault(size) == ProductImageSizeHidden {
		return ""
	}
	return ProductImageVariantURL(imageURL, size)
}

// sizedImageExt reports whether an extension belongs to a raster image type
// that gets generated thumbnail/medium/full variants (issue #382). GIFs are
// stored as-is and SVG is a code asset, so neither has variants.
func sizedImageExt(ext string) bool {
	switch ext {
	case ".png", ".jpg", ".jpeg", ".webp":
		return true
	}
	return false
}
