// Package textutil holds small text-shaping rules that more than one package
// needs to apply identically.
//
// It exists because the SAME defect was copy-pasted into three packages at once:
// chip, currency and mcpsrv each bounded an upstream HTTP body for an
// operator-facing error message with a byte slice (`string(b[:n])`). A byte slice
// cuts the Nth byte even when that byte is the first of a multi-byte rune, so a
// localised provider message (or a non-ASCII domain or product name) left the
// operator reading a broken character where the provider's text should be — in
// the very message they diagnosed the upstream failure from. One rule, one
// implementation, one place to fix it.
package textutil

import (
	"strings"
	"unicode/utf8"
)

// SameTypedValue reports whether a caller's TYPED confirmation and the STORED
// value it is asked to name back are the same value, ignoring leading and
// trailing whitespace on BOTH sides.
//
// The typed-confirmation gates (a product's NAME, a form's or a workspace's
// slug) all ask the caller to repeat the stored value before something
// irreversible happens, and all of them trimmed only the CALLER's side. That
// direction is safe on its own, but the comparison was then
// `TrimSpace(typed) == stored`, which no padded STORED value can ever satisfy:
// TrimSpace never returns a string with a leading or trailing space, so a row
// whose stored value carries padding is unconfirmed-able by everyone — the exact
// stored bytes included. A product name is stored VERBATIM
// (httpapi.createProduct and the MCP upsert both take the name as given), so a
// paste or a name typed with a trailing space is enough to reach that state, and
// the only escape left to the merchant is renaming the product first.
//
// Both sides therefore go through the same trimming here. The gate keeps its
// meaning — the caller still has to name the thing being destroyed, and a
// different value is still a mismatch — while staying reachable for every stored
// value.
func SameTypedValue(typed, stored string) bool {
	return strings.TrimSpace(typed) == strings.TrimSpace(stored)
}

// Truncate caps s to at most n CHARACTERS (runes), cutting on a rune boundary.
//
// The result is always valid UTF-8, including when s itself is not: an invalid
// input byte becomes U+FFFD rather than surviving into a message a human reads.
// n <= 0 returns "".
//
// This is the bound for an error message built from an upstream response body.
// A BYTE slice (`s[:n]`, the shape this replaces) lands mid-rune on any
// multi-byte body; the rune is then emitted one byte short and the rendered
// message — e.g. `chip: unrecognised public key response: ...\xe4` — is invalid
// UTF-8. The cap stays the same number, now counted in characters; a character
// is 1..4 bytes, so the byte length of the result is not capped.
func Truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	// ASCII-or-short fast path: a byte length within the cap implies a rune
	// count within the cap.
	if len(s) <= n && utf8.ValidString(s) {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n])
}
