// The 2FA step's OWN error sentence (card t_adf8e1e6).
//
// THE DEFECT THIS EXISTS FOR
//   `/login` is one card with two steps. Step 1 posts email + password; a 2FA
//   account then answers `401 next_step="totp_required"` and the card swaps to
//   the code input. Step 2 posts the SAME `/api/auth/login` with `totp_code`.
//
//   The server answers a bad code exactly as it answers a bad password — `401`
//   with code `login.invalid` — and that is deliberate (api/internal/httpapi/
//   auth.go's `failLogin`: the body is identical for a known and an unknown
//   email, so the refusal cannot be used to enumerate accounts). The wire
//   contract stays as it is.
//
//   The surface, though, is not the same. `page.tsx`'s `submitTotp` mapped that
//   code through the shared dictionary and rendered
//
//     "Email or password is incorrect."
//
//   to a user whose password had been accepted one screen earlier. Measured in a
//   real browser on the v1.21.242 build: a replayed code, a wrong 6-digit code, a
//   correct code submitted after the real 10-minute TTL, and a superseded code
//   (a second request issued) all rendered that sentence. It names the wrong
//   credential and never mentions the two things the user can actually do —
//   re-read the code, or request a new one.
//
// WHY A RE-MAP AND NOT A NEW SERVER CODE
//   A distinct `login.totpInvalid` code on the wire would hand an attacker the
//   split the uniform 401 exists to hide ("this password is right, the code was
//   wrong") — that is an enumeration oracle on the credential half. Nothing about
//   the risk changes client-side: the code step only renders after the password
//   was proven, on this IP, and code issuance is gated by `jf:2fa_pending:*`.
//   So the sentence is chosen by the STEP, which knows what it just sent, and the
//   API is untouched.
//
// `login.genericError` remains the answer for everything else on this step —
// a non-JSON body, a 429's plain text, an unknown code — exactly as before, so
// this re-map cannot swallow a transport failure into a sentence about a code.
//
// Kept out of the .tsx tree for the same reason `apiErr.ts` is: the repo's Node
// test runner executes TypeScript but cannot load `.tsx`, and the mapping is the
// thing worth testing.

import type { Key } from "../components/i18n";
import { apiErr, type SurfaceErrorKeys } from "./apiErr.ts";

/**
 * The codes whose sentence depends on WHICH step of the login flow refused.
 *
 * `login.invalid` is the only one today: it is the deliberate both-cases code.
 * A second entry belongs here only if the step-2 surface really knows something
 * the code does not, never as a place to restate the dictionary.
 */
export const TOTP_STEP_ERR_KEYS: SurfaceErrorKeys = {
  "login.invalid": "login.totpInvalid",
};

/**
 * The alert text for a failed 2FA-code submission.
 *
 * `body` is the whole parsed response body (never `body.error` — see apiErr.ts:
 * taking the prose would make the server's Malay sentence reachable again).
 * Without a parsed body, `{}` lands on the caller's own generic sentence.
 */
export function totpErr(body: unknown, t: (k: Key) => string): string {
  return apiErr(body, t("login.genericError"), t, TOTP_STEP_ERR_KEYS);
}
