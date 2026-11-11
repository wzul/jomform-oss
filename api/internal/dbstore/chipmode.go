package dbstore

import "strings"

// accounts.chip_mode is WRITE-ONLY LEGACY (card t_9efc5ed7).
//
// History, kept short so the column is not "cleaned up" by mistake:
//
//   - It once inferred the mode from the secret key's first characters
//     (`sk_live_…` → live, `sk_test_…` → test). CHIP keys are opaque bearer
//     tokens with no published prefix, so every real workspace was stored as
//     `test` while holding live keys (card t_8f38dc34, measured: 24 test rows,
//     zero live).
//   - It then asked the merchant to pick Test or Live. That question changes
//     nothing: the API endpoint is chosen by ChipAPIBaseUrl, nothing blocks on
//     the label, and the key itself decides what CHIP does — so the merchant was
//     answering for a badge nobody could act on (card t_9efc5ed7).
//
// Both write doors still WRITE it (the CHECK constraint in 000001_init.up.sql
// accepts only the two values below, and a save must not leave a NULL), and the
// dashboard/MCP still read it back for older integrations, but NO code path
// decides anything from it. It is kept rather than dropped because dropping a
// column is a separate, reviewable migration; see the card's handoff.
//
// The only values accounts.chip_mode accepts.
const (
	ChipModeTest = "test"
	ChipModeLive = "live"
)

// StoredChipMode is what a credential save writes into chip_mode: whatever the
// account already holds, canonicalised, and "test" when it holds nothing usable.
//
// It deliberately takes NO caller input. An older REST or MCP client that still
// sends `mode` is accepted and ignored (rejecting it would break clients that
// follow the published contract), and the value stored is the one the account
// already had — never the value in the request, because nothing acts on it.
func StoredChipMode(stored string) string {
	if strings.EqualFold(strings.TrimSpace(stored), ChipModeLive) {
		return ChipModeLive
	}
	return ChipModeTest
}
