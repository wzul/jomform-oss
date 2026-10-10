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

import "unicode/utf8"

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
