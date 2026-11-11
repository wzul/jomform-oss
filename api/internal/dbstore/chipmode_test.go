package dbstore

// Card t_9efc5ed7 — accounts.chip_mode is write-only legacy.
//
// The settings page used to ask the merchant to pick Test or Live. That answer
// changed NOTHING (ChipAPIBaseUrl picks the endpoint, the key decides what CHIP
// does), so the control is gone and both write doors now carry only the value
// the account already holds. The rule both doors (REST setChipCreds, MCP
// set_chip_settings) call is pinned here, without a database: a stored "live"
// survives every save, and anything else — absent, empty, garbage — is stored as
// "test", which is what the column's CHECK and the old default allow.

import "testing"

func TestStoredChipMode(t *testing.T) {
	cases := []struct {
		name   string
		stored string
		want   string
	}{
		{"nothing stored defaults to test", "", ChipModeTest},
		{"stored test stays test", ChipModeTest, ChipModeTest},
		{"stored live survives a save", ChipModeLive, ChipModeLive},
		// The database holds lower-case values, but a hand-fixed row should not
		// be relabelled by a save that cannot express a choice any more.
		{"stored live is case-folded", "  LiVe \n", ChipModeLive},
		{"an unreadable stored value falls back to test", "production", ChipModeTest},
		{"a garbage stored value falls back to test", "***", ChipModeTest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := StoredChipMode(c.stored); got != c.want {
				t.Fatalf("StoredChipMode(%q) = %q, want %q", c.stored, got, c.want)
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
