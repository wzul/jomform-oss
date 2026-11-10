package dbstore

import "strings"

// Card t_8f38dc34: the CHIP mode is a merchant CHOICE, never a guess.
//
// The two REST/MCP write doors used to infer it from the secret key's prefix
// (`sk_live_…` → live, `sk_test_…` → test) on the theory that CHIP keys work
// like Stripe's. They do not, and CHIP's own documentation says so: the
// merchant portal issues a SEPARATE key for test and for live, and the key
// itself is an opaque bearer token with no published prefix (docs.chip-in.asia
// → CHIP Collect → Authentication → "Test vs live keys"). So a real merchant
// key matched neither prefix, every workspace was stored as chip_mode='test',
// and the settings page showed "Test" beside a live key.
//
// The mode is now what the merchant picks in the UI. These two functions are
// the ONE rule both doors share, so REST and MCP cannot drift apart again.

// The only values accounts.chip_mode accepts — see the CHECK constraint in
// 000001_init.up.sql.
const (
	ChipModeTest = "test"
	ChipModeLive = "live"
)

// NormaliseChipMode canonicalises a mode the caller stated. ok is false when
// it is not one of the two stored values, so a caller can REJECT it instead of
// quietly storing something else.
func NormaliseChipMode(mode string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case ChipModeTest:
		return ChipModeTest, true
	case ChipModeLive:
		return ChipModeLive, true
	}
	return "", false
}

// ResolveChipMode returns the mode a credential save should store.
//
// choice is what the caller sent (empty when it did not send one); stored is
// what the account already has ("" when it has none).
//
//   - An explicit choice wins. It is never overridden by the shape of the
//     secret: a merchant who says "test" gets test even when their key happens
//     to start with sk_live.
//   - An empty choice KEEPS the stored mode. A save that only edits the
//     fallback email or the method whitelist must not relabel live
//     credentials as test.
//   - With nothing stored, the default is test. That is the safe label for a
//     merchant who has not said which portal mode their key belongs to.
//
// ok is false when choice is non-empty and is neither test nor live; the
// caller must reject that request rather than guess.
func ResolveChipMode(choice, stored string) (mode string, ok bool) {
	if strings.TrimSpace(choice) == "" {
		if canon, valid := NormaliseChipMode(stored); valid {
			return canon, true
		}
		return ChipModeTest, true
	}
	return NormaliseChipMode(choice)
}
