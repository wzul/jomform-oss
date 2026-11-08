package cryptoutil

// Card t_56a2bd7f — the CHIP secret was published almost in full.
//
// Both copies of the mask (httpapi.maskSecret and mcpsrv.maskSecretMCP)
// returned `s[:len(s)-4] + "••••" + s[len(s)-4:]`: every character of the
// secret except the last four. A configured store's settings page therefore
// rendered 84 of an 88-character key, and the same value went to every
// workspace member through the dashboard bundle and to any MCP caller through
// get_chip_settings.
//
// This test pins the shape the surfaces are allowed to publish. On the old
// implementation every case below with a long secret fails: the output carries
// interior characters and its length is len(secret)+3, i.e. it grows with the
// secret.

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// The budget: a public mode prefix, the bullet run, and the four-character
// tail. Nothing else may be published.
const (
	maskTailLen = 4
	maskBudget  = len("sk_live_") + maskBullets + maskTailLen
	maskBullets = 4
)

// stripeKey assembles a Stripe-shaped fixture at RUN TIME. Do not inline one as a
// single literal: a contiguous `sk_live_` plus 48 token characters is
// indistinguishable from a real key to a secret scanner, and GitHub push
// protection refuses to publish it. It refused the public mirror of v1.25.0 for
// exactly that reason, pointing at this file — so the release's mirror step
// failed AFTER the deploy had already shipped and the mirror went stale. The
// repo's own gitleaks gate cannot see this class of fixture, because
// .gitleaks.toml allowlists `api/internal/.*_test\.go` by path; push protection
// has no such allowlist.
//
// These tests pin the SHAPE — the mode prefix, a 48-character body, and the last
// four characters — so building the value here costs them nothing. The same
// shape is what `deploy/sync-oss.sh` scans the released tree for before it
// pushes.
func stripeKey(prefix, tail string) string {
	body := strings.Repeat("0123456789abcdef", 3) // 48 characters
	return prefix + body[:len(body)-len(tail)] + tail
}

func TestMaskSecretKeepsOnlyModePrefixAndTail(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"live key", stripeKey("sk_live_", "76ea"), "sk_live_••••76ea"},
		{"test key", stripeKey("sk_test_", "e83c"), "sk_test_••••e83c"},
		// No recognised prefix: the whole key is body, so the mask hides all
		// but the tail and the merchant gets no mode hint (the mode field is
		// published separately).
		{"no known prefix", "abcdefghijklmnop1234", "••••1234"},
		// Shorter than the tail: showing its only characters would defeat the
		// mask, so the whole value is masked.
		{"shorter than tail", "abc", "••••"},
		{"exactly the tail", "1234", "••••"},
		{"only a prefix", "sk_test_", "sk_test_••••"},
		{"prefix plus a short body", "sk_test_abc", "sk_test_••••"},
		{"empty", "", ""},
		{"only whitespace", "   ", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MaskSecret(tc.in); got != tc.want {
				t.Fatalf("MaskSecret(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestMaskSecretDoesNotLeakTheBody is the leak assertion: no interior window of
// the secret may survive masking. It compares against the secret itself rather
// than only against the expected string, so a rewrite that keeps the shape but
// slices the wrong end still fails.
func TestMaskSecretDoesNotLeakTheBody(t *testing.T) {
	// A realistic CHIP-style key: base64ish body.
	secret := stripeKey("sk_live_", "76ea")

	got := MaskSecret(secret)
	body := strings.TrimPrefix(secret, "sk_live_")
	interior := body[:len(body)-maskTailLen] // everything the mask must hide

	for i := 0; i+4 <= len(interior); i++ {
		if window := interior[i : i+4]; strings.Contains(got, window) {
			t.Fatalf("masked value %q leaks interior window %q at offset %d of the secret", got, window, i)
		}
	}
	if !strings.HasSuffix(got, body[len(body)-maskTailLen:]) {
		t.Fatalf("masked value %q does not end with the secret's last 4 characters", got)
	}
	if !strings.HasPrefix(got, "sk_live_") {
		t.Fatalf("masked value %q lost the live-mode hint", got)
	}
}

// TestMaskSecretLengthDoesNotGrowWithTheSecret is the regression that bites
// hardest on the old implementation: it grew by (len(secret) - 4) bytes, so a
// longer key leaked more.
func TestMaskSecretLengthDoesNotGrowWithTheSecret(t *testing.T) {
	var prev int
	for _, n := range []int{8, 16, 32, 64, 88, 256, 4096} {
		secret := "sk_test_" + strings.Repeat("a", n)
		got := MaskSecret(secret)
		if size := utf8.RuneCountInString(got); size > maskBudget {
			t.Fatalf("MaskSecret for a %d-byte secret returned %d runes (%q), over the %d-rune budget: the mask must not grow with the secret",
				len(secret), size, got, maskBudget)
		}
		if prev != 0 && utf8.RuneCountInString(got) != prev {
			t.Fatalf("mask length changed from %d to %d as the secret grew to %d bytes", prev, utf8.RuneCountInString(got), len(secret))
		}
		prev = utf8.RuneCountInString(got)
	}
}

// TestMaskSecretTailStaysValidUTF8 guards the tail slice: a BYTE slice can cut a
// multi-byte rune in half, and json.Marshal then sends U+FFFD — a different
// tail than the one promised, and invalid UTF-8 in the stored value.
func TestMaskSecretTailStaysValidUTF8(t *testing.T) {
	for _, in := range []string{
		"sk_test_éàüñ€12",
		"日本語のテストキー1234",
	} {
		got := MaskSecret(in)
		if !utf8.ValidString(got) {
			t.Fatalf("MaskSecret(%q) = %q is not valid UTF-8", in, got)
		}
		if utf8.RuneCountInString(got) > maskBudget {
			t.Fatalf("MaskSecret(%q) = %q over the %d-rune budget", in, got, maskBudget)
		}
	}
}
