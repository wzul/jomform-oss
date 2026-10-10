package textutil

// Guard for the shared rule behind every "bounded upstream body in an error
// message" call site (chip client.go + methods.go, currency, mcpsrv).
//
// The defect it replaces used a BYTE slice, so the offset that matters is the
// one landing inside a multi-byte rune: under a byte slice the result is invalid
// UTF-8. Each case below asserts the three properties an operator-facing string
// must have: valid UTF-8, within the cap when counted in CHARACTERS, and a
// PREFIX of the input (the text is kept, not mangled).

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateKeepsARuneBoundary(t *testing.T) {
	cases := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"short ASCII passes through untouched", "boom", 10, "boom"},
		{"ASCII over the cap is cut at the cap", strings.Repeat("x", 12), 5, "xxxxx"},
		{"exactly at the cap is untouched", strings.Repeat("x", 5), 5, strings.Repeat("x", 5)},
		{"one rune shorter than the cap is untouched", "中中", 3, "中中"},
		{"cap exactly on the last rune is untouched", "中中", 2, "中中"},
		{
			"a 3-byte rune counts as one character",
			strings.Repeat("x", 3) + "中" + strings.Repeat("y", 4),
			5,
			strings.Repeat("x", 3) + "中y",
		},
		{
			"the cap falls just after a 3-byte rune",
			strings.Repeat("x", 3) + "中" + strings.Repeat("y", 4),
			4,
			strings.Repeat("x", 3) + "中",
		},
		{
			"a 4-byte rune counts as one character",
			strings.Repeat("x", 2) + "\U0001F600" + strings.Repeat("y", 3),
			3,
			strings.Repeat("x", 2) + "\U0001F600",
		},
		{"n <= 0 returns empty", "boom", 0, ""},
		{"negative n returns empty rather than panicking", "boom", -1, ""},
		{"single rune cap", "中中中", 1, "中"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Truncate(tc.in, tc.n)
			if got != tc.want {
				t.Fatalf("Truncate(%q, %d) = %q, want %q", tc.in, tc.n, got, tc.want)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("Truncate(%q, %d) = %q is not valid UTF-8", tc.in, tc.n, got)
			}
			if tc.n > 0 {
				if n := utf8.RuneCountInString(got); n > tc.n {
					t.Fatalf("Truncate(%q, %d) = %q is %d characters, over the cap", tc.in, tc.n, got, n)
				}
			}
			if !strings.HasPrefix(tc.in, got) {
				t.Fatalf("Truncate(%q, %d) = %q is not a prefix of the input", tc.in, tc.n, got)
			}
		})
	}
}

// TestTruncateNormalisesInvalidInputUTF8 pins the second half of the contract: a
// body that is not valid UTF-8 at all (a provider answering in Latin-1, a
// half-written response) must still not put invalid bytes into a message a human
// reads. Every invalid byte becomes U+FFFD.
func TestTruncateNormalisesInvalidInputUTF8(t *testing.T) {
	cases := []struct {
		name string
		in   string
		n    int
	}{
		{"invalid byte inside the cap", "abc\xe4def", 32},
		{"invalid byte cut by the cap", "abc\xe4def", 4},
		{"lone continuation byte", "abc\xb8def", 32},
		{"truncated multi-byte sequence", "abc\xe4\xb8", 32},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Truncate(tc.in, tc.n)
			if !utf8.ValidString(got) {
				t.Fatalf("Truncate(%q, %d) = %q is not valid UTF-8", tc.in, tc.n, got)
			}
			if n := utf8.RuneCountInString(got); n > tc.n {
				t.Fatalf("Truncate(%q, %d) = %q is %d characters, over the cap", tc.in, tc.n, got, n)
			}
		})
	}
}

// TestTruncateStraddlingBodyIsValidUTF8 is the shape of the real defect, spelled
// out at the default caps the three call sites use: a body whose byte at n-1 is
// the first byte of a multi-byte rune. The byte slice this replaces returned a
// string ending in the broken lead byte (verified: it ended "\xe4").
func TestTruncateStraddlingBodyIsValidUTF8(t *testing.T) {
	for _, cap := range []int{200, 256, 512} {
		body := strings.Repeat("x", cap-1) + "中" + strings.Repeat("y", 40)
		got := Truncate(body, cap)
		if !utf8.ValidString(got) {
			t.Errorf("Truncate(body, %d) is not valid UTF-8: %q", cap, got)
		}
		if !strings.Contains(got, "中") {
			t.Errorf("Truncate(body, %d) = %q lost the rune that straddles the cap", cap, got)
		}
		if n := utf8.RuneCountInString(got); n != cap {
			t.Errorf("Truncate(body, %d) = %d characters, want the cap exactly", cap, n)
		}
	}
}
