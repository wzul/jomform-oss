// The 429 sentence: WHAT the visitor should do, and WHEN (card t_e50a9680).
//
// THE DEFECT THIS EXISTS FOR
//   `/login` answered a locked-out visitor with one generic sentence, in every
//   locale, for a refusal that carries the one fact they need — when the window
//   clears:
//
//     POST /api/auth/login -> 429
//       Retry-After: 900
//       {"code":"request.tooManyRequests","error":"Terlalu banyak percubaan. Sila cuba lagi dalam 15 minit."}
//
//     role=alert: "Failed. Please try again."
//
//   Two decisions produced that, and both were right on their own:
//
//     * apiErr.ts NEVER renders the server's prose — the sentence has to come
//       from the dictionary so a `lang=en` user does not read Malay. Correct, and
//       unchanged here.
//     * `request.tooManyRequests` is a CLASS code (api/internal/httpapi/
//       errcode.go's ClassTooManyRequests, derived from the status by classCode),
//       and the class codes are deliberately absent from API_ERROR_KEYS so an
//       uncoded 4xx falls through to the page's own message. Correct too.
//
//   The consequence was that the ONLY number that answers "when may I try
//   again?" — `Retry-After: 900`, which the dashboard hop relays verbatim
//   (web/src/lib/proxyResponseHeaders.ts) — was discarded, and the page said
//   "try again" with no when.
//
// WHY A SEPARATE MAPPING AND NOT A NEW API_ERROR_KEYS ROW
//   The window is not in the code: it is in the response HEADER, per-response
//   data that a code->key table cannot carry. So the sentence is built from what
//   the answer actually contains (the status, the header, the body's code) and
//   the dictionary supplies the words, in all five locales.
//
//   `request.tooManyRequests` is therefore NOT mapped globally: mapping a class
//   code would replace every page's own failure sentence with a generic one.
//   The pages that can answer a 429 opt in by calling this, and the sentence is
//   the only thing they change.
//
//   When the refusal carries no usable window the sentence is the dictionary's
//   existing `apiErr.rateLimited` — the same meaning, the same row, rather than a
//   second near-duplicate string to keep in step.
//
// NOTHING HERE PRINTS THE SERVER'S PROSE. `body` is the whole parsed answer and
// is read only through apiErrorCode, i.e. for its machine `code`.
//
// Kept out of the .tsx tree for the same reason apiErr.ts is: the repo's Node
// test runner executes TypeScript but cannot load `.tsx`, and the mapping is the
// thing worth testing.

import type { Key } from "../components/i18n";
import { apiErrorCode } from "./apiErr.ts";

/** A translator: the dictionary lookup, optionally with `{placeholder}` params. */
export type Translate = (k: Key, params?: Record<string, string | number>) => string;

/**
 * The API's class code for every 429 it writes (api/internal/httpapi/errcode.go,
 * `ClassTooManyRequests`, returned by `classCode(429)` for a message with no
 * specific code — which is exactly the login lockout's
 * `auth.LoginLockoutMessage`).
 *
 * Named here rather than inferred from the status alone because a body can also
 * carry it behind a status the hop rewrote; either half is enough to know this
 * is a throttle refusal.
 */
export const TOO_MANY_REQUESTS_CODE = "request.tooManyRequests";

/** Below this many seconds the sentence counts seconds instead of minutes. */
const SECONDS_CEILING = 120;

/**
 * The number of seconds a `Retry-After` header asks the caller to wait, or
 * `null` when the header is absent or cannot be read.
 *
 * RFC 9110 §10.2.3 (https://www.rfc-editor.org/rfc/rfc9110#field.retry-after)
 * allows exactly two forms, and both are handled:
 *
 *   * `Retry-After: 900` — delta-seconds (`1*DIGIT`), what the API writes
 *     (`LoginLockoutRetryAfter = "900"`, the per-IP throttle's `"30"`);
 *   * `Retry-After: Wed, 21 Oct 2015 07:28:00 GMT` — an HTTP-date, which
 *     `Date.parse` reads.
 *
 * `null` (not 0) for "unreadable" so an explicit `Retry-After: 0` — "retry
 * now", a legitimate value — is not confused with a missing header. A date in
 * the past is clamped to 0 for the same reason.
 *
 * Nothing here decides when the window clears; the value the SERVER chose is
 * the value that is shown. A second clock on this side would contradict it.
 */
export function retryAfterSeconds(value: string | null | undefined, now: number = Date.now()): number | null {
  if (typeof value !== "string") return null;
  const raw = value.trim();
  if (raw === "") return null;
  if (/^\d+$/.test(raw)) return Number(raw);
  // The date branch is taken only for something that at least LOOKS like a date:
  // `Date.parse` is lenient enough to read "-5" and "1.5" as years, and a header
  // the API never wrote must not become "retry now" (0) by that accident.
  if (!/[A-Za-z]/.test(raw)) return null;
  const at = Date.parse(raw);
  if (Number.isNaN(at)) return null;
  return Math.max(0, Math.ceil((at - now) / 1000));
}

/**
 * The localized sentence for a throttle refusal, or "" when this answer is not
 * one — in which case the caller renders exactly what it rendered before.
 *
 * Returning "" rather than swallowing the fallback is deliberate: the login page
 * has a per-STEP sentence for a 401 (`totpErr`, whose 2FA step re-maps
 * `login.invalid`), and a helper that took the fallback as a parameter could not
 * be composed with it. The call sites read
 *
 *     rateLimitErr(r.status, r.headers.get("retry-after"), d, t) || totpErr(d, t)
 *
 * so a 401 never reaches this function's opinion and a 429 never reaches a
 * wrong-credentials sentence.
 *
 * The window is expressed as the LARGER of the two units that stay honest: under
 * two minutes it counts seconds ("try again in 75 seconds"), at two minutes and
 * above it rounds UP to whole minutes ("try again in 15 minutes"). Rounding up is
 * the safe direction — a sentence that rounds DOWN sends the visitor back inside
 * the lockout to be refused again — and it keeps singular minutes out of the
 * dictionary, where "1 minutes" would need a third row per locale.
 */
export function rateLimitErr(
  status: number,
  retryAfter: string | null | undefined,
  body: unknown,
  t: Translate,
): string {
  if (status !== 429 && apiErrorCode(body) !== TOO_MANY_REQUESTS_CODE) return "";
  const seconds = retryAfterSeconds(retryAfter);
  // `null`/0 = no usable window. 1 = a real window that cannot be phrased: the
  // coarse per-IP limiter derives Retry-After from its bucket (rate 1/s, burst
  // 10 — api/internal/httpapi/middleware/ratelimit.go, card t_5bed7cab), so a
  // drained client is really answered "1", and "try again in 1 seconds" is not a
  // sentence. A one-second wait is precisely what the existing generic row says
  // ("Please try again shortly"), which is why this reuses it instead of adding a
  // singular row per locale.
  if (seconds === null || seconds <= 1) return t("apiErr.rateLimited");
  if (seconds < SECONDS_CEILING) return t("apiErr.tooManyRequestsInSeconds", { seconds });
  return t("apiErr.tooManyRequestsInMinutes", { minutes: Math.ceil(seconds / 60) });
}
