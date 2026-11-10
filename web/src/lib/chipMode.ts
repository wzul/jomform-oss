// Card t_8f38dc34 — the CHIP test/live mode is a CHOICE, not a guess.
//
// CHIP issues a SEPARATE key for test and for live in the merchant portal, and
// the key itself is an opaque bearer token with no published prefix
// (docs.chip-in.asia → CHIP Collect → Authentication → "Test vs live keys").
// The settings page used to read the key's first characters anyway
// (`sk_live_…` → Live, `sk_test_…` → Test, everything else → Test), so a real
// merchant key matched nothing and the page, like every stored row, said Test.
//
// The merchant now says which mode the key belongs to. This module is the one
// place the two facts the page needs about that choice live, so the control and
// its default cannot drift apart.

/** The only two values accounts.chip_mode accepts (the column's CHECK). */
export type ChipMode = "test" | "live";

/**
 * The choices, in the order the control renders them. Test is FIRST because it
 * is the default: a merchant who has not said which portal mode their key came
 * from is shown the label that cannot misdescribe a live key as test.
 */
export const CHIP_MODE_OPTIONS: readonly ChipMode[] = ["test", "live"];

/**
 * The control's starting value for a status row read from the API.
 *
 * "live" is honoured only as the exact stored value; anything else (absent,
 * empty, an unexpected string from an older row) falls back to "test". That is
 * the same default the API applies when no mode has ever been stored.
 */
export function chipModeFromStored(stored: unknown): ChipMode {
  return stored === "live" ? "live" : "test";
}

/** Human label key for an option — keeps the control and the badge in step. */
export function chipModeLabelKey(mode: ChipMode): "set.test" | "set.live" {
  return mode === "live" ? "set.live" : "set.test";
}
