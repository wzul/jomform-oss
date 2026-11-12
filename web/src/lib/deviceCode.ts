// RFC 8628 user-code normalisation (card t_4f3ab16e).
//
// The consent mail and the CLI both print the code as `KX9M-X5BS`, and a person
// retyping it will produce exactly what the keyboard gives them: `kx9mx5bs`,
// ` kx9m-x5bs `, `KX9MX5BS`, `KX9M X5BS`. None of those is a different code, so
// none of them may be answered with "not valid".
//
// THE RULE IS THE SERVER'S, AND IT ALREADY EXISTS. `normalizeUserCode` in
// api/internal/httpapi/oauth_device.go upper-cases, strips `-`, space and tab,
// requires exactly 8 characters and re-inserts the canonical dash before
// hashing — so every one of those shapes hashes to the same row. This module
// mirrors that rule for the one thing the server cannot do: reject a string that
// cannot be a code BEFORE a round trip, and show the canonical code once it is
// understood.
//
// Kept deliberately tiny and dependency-free because the two copies have to be
// read side by side: src/lib/deviceCode.test.ts pins the same five shapes the Go
// test (api/internal/httpapi/oauth_device_typecode_t_4f3ab16e_test.go) resolves
// through the real handler, so a change to one rule that is not made to the
// other shows up as a red test rather than as a "not valid" for a code the user
// is reading off their screen.

/** Length of a user code WITHOUT its dash (`KX9M-X5BS` → 8). */
export const DEVICE_CODE_LEN = 8;

/**
 * Normalise whatever a person typed into the canonical `XXXX-XXXX`, or "" when
 * the string cannot be a user code at all.
 *
 * "Cannot be" means "is not 8 characters once the separators are gone" — the
 * same test the server makes, no stricter. A code that is 8 characters but
 * unknown is still submitted: only the server knows which codes exist, and
 * rejecting locally would turn "expired" into "invalid" for a real code.
 */
export function normalizeDeviceCode(raw: string): string {
  const compact = (raw ?? "").replace(/[-\s]/g, "").toUpperCase();
  if (compact.length !== DEVICE_CODE_LEN) return "";
  return compact.slice(0, 4) + "-" + compact.slice(4);
}
