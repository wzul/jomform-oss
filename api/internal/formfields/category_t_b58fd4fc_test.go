package formfields

// Card t_b58fd4fc — Category is the read-side authority for a gallery card's
// category chip, so these cases pin what "a category" means in Go, without a
// database: the publish gate (galleryMetadataMissingSQL) is stated in terms of
// this same trim, and both gallery read paths (REST + MCP) return its value.

import (
	"testing"
	"unicode"
)

func TestCategoryTrimsWhatTheCardCannotShow(t *testing.T) {
	cases := []struct {
		name string
		cfg  map[string]any
		want string
	}{
		{"nil config", nil, ""},
		{"no category key", map[string]any{"mode": "response"}, ""},
		{"empty string", map[string]any{"category": ""}, ""},
		{"spaces only", map[string]any{"category": "   "}, ""},
		{"tab, newline and carriage return", map[string]any{"category": "\t\n  \r\n"}, ""},
		{"no-break space", map[string]any{"category": "\u00a0"}, ""},
		{"a number is not a category", map[string]any{"category": 5}, ""},
		{"an object is not a category", map[string]any{"category": map[string]any{"name": "Contact"}}, ""},
		{"surrounding whitespace is not content", map[string]any{"category": "\t  Contact \n "}, "Contact"},
		{"a clean category is unchanged", map[string]any{"category": "Food & Beverage"}, "Food & Beverage"},
	}
	for _, tc := range cases {
		if got := Category(tc.cfg); got != tc.want {
			t.Errorf("%s: Category = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestCategoryAgreesWithTrimSpaceOnEveryWhitespaceCodePoint derives the set
// from unicode.IsSpace instead of listing it, so the read side cannot drift
// from what Go calls whitespace — the set the SQL trim is spelled for.
func TestCategoryAgreesWithTrimSpaceOnEveryWhitespaceCodePoint(t *testing.T) {
	var n int
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if !unicode.IsSpace(r) {
			continue
		}
		n++
		if got := Category(map[string]any{"category": string(r)}); got != "" {
			t.Errorf("U+%04X (%q): Category = %q, want \"\" — a whitespace-only category is not a "+
				"chip the card can show", r, string(r), got)
		}
		// And whitespace AROUND a real name is trimmed, not dropped.
		if got := Category(map[string]any{"category": string(r) + "Contact" + string(r)}); got != "Contact" {
			t.Errorf("U+%04X: Category(padded) = %q, want \"Contact\"", r, got)
		}
	}
	if n < 20 {
		t.Fatalf("derived only %d unicode.IsSpace code points — the derivation is broken, so no "+
			"verdict above would mean anything", n)
	}
}
