package dbstore

// Card t_8f38dc34 — the CHIP mode is a merchant CHOICE, never a reading of the
// secret key.
//
// The rule both write doors (REST setChipCreds, MCP set_chip_settings) call is
// pinned here, without a database: an explicit choice wins, an omitted mode
// keeps the stored one, nothing stored defaults to test, and a value that is
// not test/live is REJECTED rather than coerced. The bug this replaces read the
// secret's first characters (`sk_live_…` → live, `sk_test_…` → test), which
// CHIP keys do not have, so every real workspace was stored as test.

import "testing"

func TestResolveChipMode(t *testing.T) {
	cases := []struct {
		name   string
		choice string
		stored string
		want   string
		wantOK bool
	}{
		// The merchant picked a mode and it is stored as chosen.
		{"explicit live", "live", "", ChipModeLive, true},
		{"explicit test", "test", "", ChipModeTest, true},
		{"choice beats a different stored mode", "live", "test", ChipModeLive, true},
		{"choice beats a different stored mode, the other way", "test", "live", ChipModeTest, true},
		// A choice is canonicalised, not pattern-matched against a key shape.
		{"trimmed and case-folded", "  LiVe \n", "", ChipModeLive, true},
		// An omitted mode keeps what the account already says, so editing the
		// fallback email or the method whitelist cannot relabel live as test.
		{"omitted keeps stored live", "", "live", ChipModeLive, true},
		{"omitted keeps stored test", "", "test", ChipModeTest, true},
		// Nothing stored and nothing chosen: the safe label is test.
		{"omitted with nothing stored", "", "", ChipModeTest, true},
		{"omitted with an unreadable stored value", "   ", "garbage", ChipModeTest, true},
		// A value that is neither mode is refused. Guessing one is how a live
		// key ends up labelled test with no error anywhere.
		{"unknown choice is refused", "production", "", "", false},
		{"a secret pasted as the mode is refused", "sk_live_abc123", "", "", false},
		{"a prefix is not a mode", "sk_test", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := ResolveChipMode(c.choice, c.stored)
			if ok != c.wantOK {
				t.Fatalf("ResolveChipMode(%q, %q) ok = %v, want %v", c.choice, c.stored, ok, c.wantOK)
			}
			if got != c.want {
				t.Fatalf("ResolveChipMode(%q, %q) = %q, want %q", c.choice, c.stored, got, c.want)
			}
		})
	}
}

// The stored column only accepts these two values (000001_init.up.sql). A new
// caller that invents a third mode would fail at the database with a constraint
// error and a 500; pin the set so the constant pair cannot drift from the CHECK.
func TestChipModeValues(t *testing.T) {
	if ChipModeTest != "test" || ChipModeLive != "live" {
		t.Fatalf("stored chip_mode values are %q/%q, want test/live", ChipModeTest, ChipModeLive)
	}
}
