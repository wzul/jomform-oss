// Guard for card t_e50a9680 — the login page must tell a locked-out visitor WHEN,
// not only that, it may try again.
//
// THE DEFECT
//   While the visitor's address was locked out the API answered
//
//     POST /api/auth/login -> 429
//       Retry-After: 900
//       {"code":"request.tooManyRequests","error":"Terlalu banyak percubaan. Sila cuba lagi dalam 15 minit."}
//
//   and the shipped page rendered, in every locale,
//
//     role=alert "Failed. Please try again."
//
//   `request.tooManyRequests` is a CLASS code (api/internal/httpapi/errcode.go
//   `classCode(429)`, which is what `auth.LoginLockoutMessage` gets because it
//   matches no specific code in `errCodeByMessage`), and apiErr.ts's dictionary
//   deliberately does not map class codes — so the code fell through to
//   `login.genericError` by design. The window the server HAD chosen was in the
//   `Retry-After` header, relayed verbatim by the dashboard hop
//   (web/src/lib/proxyResponseHeaders.ts), and nothing in the web tree read it.
//
// WHAT THIS FILE PINS, AND WHY EACH ONE IS THE THING THAT BREAKS
//   1. `Retry-After` is read in BOTH forms RFC 9110 allows — delta-seconds and an
//      HTTP-date — and "unreadable" is `null`, never 0, so an explicit
//      `Retry-After: 0` is not mistaken for a missing header. A header the app
//      cannot parse must not become "0 seconds".
//   2. A 429 WITH a window renders the localized sentence carrying that number,
//      in all five locales. Neutralising the header read (returning null) or the
//      page's call site turns this RED — see the bite note on the test.
//   3. The unit is honest in both directions: seconds below two minutes (so a
//      lockout of 75s is not rounded to a minute it does not have) and whole
//      minutes rounded UP above it (a sentence that rounds DOWN sends the visitor
//      back into the lockout). "1 minutes" can therefore never render — pinned.
//      A window of exactly one second is reachable since card t_5bed7cab made
//      the header follow the bucket (the coarse per-IP guard refills at 1/s), so
//      it renders the generic `apiErr.rateLimited` row rather than "1 seconds".
//   4. A 429 WITHOUT a usable window still says "too many attempts" — the
//      dictionary's existing `apiErr.rateLimited` row, not a second near-duplicate
//      sentence and not the page's generic failure line.
//   5. A 401 is NOT a throttle: the helper returns "" so the page's own sentence
//      (the dictionary's "Email or password is incorrect.", or the 2FA step's own
//      remap) still renders. This is the "must not regress" half of the card.
//   6. The server's prose is still never rendered — the body is read for its
//      machine `code` only.
//   7. The page's call sites really use it, and read the header off the RESPONSE
//      (`r.headers.get("retry-after")`), not a constant. A helper nobody calls
//      passes every assertion above while the page keeps the old line — the shape
//      this repo has been bitten by before.
//
// Run with the repo's Node test runner:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import ts from "typescript";

import { DICT, LANGS } from "../components/i18nDict.ts";
import { apiErr } from "./apiErr.ts";
import { TOO_MANY_REQUESTS_CODE, rateLimitErr, retryAfterSeconds } from "./rateLimitErr.ts";

const dict = DICT as Record<string, Record<string, unknown>>;

/**
 * A translator over the REAL dictionary that asserts the key exists, so a
 * mistyped key fails this test instead of silently rendering the key itself
 * (`t()` in components/i18n.tsx falls back to the key when an entry is absent),
 * and substitutes `{placeholders}` exactly as `t()` does.
 */
function tIn(lang: string) {
  return (k: string, params?: Record<string, string | number>): string => {
    const entry = dict[k];
    assert.ok(entry, `the ${lang} translator was asked for ${k}, which is not in DICT`);
    const v = entry[lang];
    let s = typeof v === "string" ? v : (entry.en as string);
    if (params) for (const [name, value] of Object.entries(params)) s = s.replaceAll(`{${name}}`, String(value));
    return s;
  };
}

const LOGIN_PAGE = fileURLToPath(new URL("../app/login/page.tsx", import.meta.url));

/** The raw text of a named function declaration in a source file, via the TS
 *  parser (a regex would read the wrong body the first time someone nests one). */
function functionBody(file: string, name: string): string {
  const src = readFileSync(file, "utf8");
  const sf = ts.createSourceFile(file, src, ts.ScriptTarget.ESNext, true, ts.ScriptKind.TSX);
  const found: string[] = [];
  const visit = (node: ts.Node) => {
    if (ts.isFunctionDeclaration(node) && node.name?.text === name) found.push(node.getText(sf));
    node.forEachChild(visit);
  };
  visit(sf);
  assert.equal(found.length, 1, `expected exactly one function declaration named ${name}, found ${found.length}`);
  return found[0];
}

test("Retry-After is read in both RFC 9110 forms, and 'unreadable' is null rather than 0", () => {
  // The two values the API actually writes: the login lockout (900) and the
  // coarse per-IP throttle (30) — auth.LoginLockoutRetryAfter,
  // middleware.RateLimitWith.
  assert.equal(retryAfterSeconds("900"), 900);
  assert.equal(retryAfterSeconds("30"), 30);
  // Whitespace from a header value is trimmed, not treated as garbage.
  assert.equal(retryAfterSeconds(" 900 "), 900);
  // An explicit zero is a real answer ("retry now") and must be preserved — this
  // is why the "unknown" value is null.
  assert.equal(retryAfterSeconds("0"), 0);
  // Absent / empty / non-numeric junk.
  assert.equal(retryAfterSeconds(null), null);
  assert.equal(retryAfterSeconds(undefined), null);
  assert.equal(retryAfterSeconds(""), null);
  assert.equal(retryAfterSeconds("soon"), null);
  assert.equal(retryAfterSeconds("-5"), null);
  // The HTTP-date form. Fixed `now` so the assertion cannot drift with the clock.
  const now = Date.parse("Wed, 21 Oct 2015 07:27:00 GMT");
  assert.equal(retryAfterSeconds("Wed, 21 Oct 2015 07:28:00 GMT", now), 60);
  // A date already in the past is 0 ("now"), not a negative wait.
  assert.equal(retryAfterSeconds("Wed, 21 Oct 2015 07:00:00 GMT", now), 0);
});

test("a locked-out 429 names the window the server chose — 900s reads 15 minutes in every locale", () => {
  // The exact answer the hop relays: status 429, retry-after 900, the class code
  // the API derives from the status.
  const body = { code: TOO_MANY_REQUESTS_CODE, error: "Terlalu banyak percubaan. Sila cuba lagi dalam 15 minit." };

  for (const lang of LANGS) {
    const t = tIn(lang);
    const rendered = rateLimitErr(429, "900", body, t);

    // THE BITE: neutralise the header read (make retryAfterSeconds return null) or
    // revert the page's call site to `apiErr(...)` — this is the assertion that
    // fails, because the sentence then carries no number at all.
    assert.match(rendered, /15/, `lang=${lang}: the 429 sentence does not name the 15-minute window: ${JSON.stringify(rendered)}`);
    assert.equal(rendered, t("apiErr.tooManyRequestsInMinutes", { minutes: 15 }), `lang=${lang}: not the dictionary's window sentence`);
    // And it is NOT the generic failure line the defect produced.
    assert.notEqual(rendered, t("login.genericError"), `lang=${lang}: the page still renders its generic failure sentence`);
    assert.ok(rendered.trim() !== "", `lang=${lang}: the 429 rendered nothing`);
  }

  // The English wording, asserted rather than only pattern-matched, because a
  // translation that drops "minutes" would still contain "15".
  assert.equal(
    rateLimitErr(429, "900", body, tIn("en")),
    "Too many attempts — try again in 15 minutes.",
  );
});

test("the unit stays honest: seconds below two minutes, whole minutes rounded UP above it", () => {
  const t = tIn("en");
  const body = { code: TOO_MANY_REQUESTS_CODE };

  // The per-IP throttle's 30s.
  assert.equal(rateLimitErr(429, "30", body, t), "Too many attempts — try again in 30 seconds.");
  // 75s is not "1 minute": rounding it down would tell the visitor to try again
  // 15 seconds before the window clears.
  assert.equal(rateLimitErr(429, "75", body, t), "Too many attempts — try again in 75 seconds.");
  // 150s rounds UP to 3, never down to 2.
  assert.equal(rateLimitErr(429, "150", body, t), "Too many attempts — try again in 3 minutes.");
  // The boundary, and the singular that must never render: 119s stays seconds,
  // 120s is the first minute count and is already plural.
  assert.equal(rateLimitErr(429, "119", body, t), "Too many attempts — try again in 119 seconds.");
  assert.equal(rateLimitErr(429, "120", body, t), "Too many attempts — try again in 2 minutes.");
  for (const secs of ["60", "61", "90", "119", "120", "121"]) {
    assert.doesNotMatch(rateLimitErr(429, secs, body, t), /\b1 minutes\b/, `Retry-After: ${secs} rendered a singular/plural fault`);
  }
});

test("a one-second window says it in words, never as \"1 seconds\"", () => {
  // The coarse per-IP guard's header now comes from its bucket (card t_5bed7cab):
  // rate 1 token/s, burst 10, so a drained client is really answered "1". That is
  // the measured /login case — before the fix the header was the constant 30.
  const body = { code: TOO_MANY_REQUESTS_CODE };
  for (const lang of LANGS) {
    const t = tIn(lang);
    assert.equal(
      rateLimitErr(429, "1", body, t),
      t("apiErr.rateLimited"),
      `lang=${lang}: a one-second window must not render the seconds row — "try again in 1 seconds" is not a sentence`,
    );
  }
  assert.doesNotMatch(rateLimitErr(429, "1", body, tIn("en")), /\b1 seconds?\b/, "a one-second window rendered its number as a plural");
  // The control: the next second up IS phrased with its number, so the fallback
  // above is the one-second case and not a blanket loss of the window.
  assert.equal(rateLimitErr(429, "2", body, tIn("en")), "Too many attempts — try again in 2 seconds.");
});

test("a 429 that names no window still says too many attempts (the existing row, not a new sentence)", () => {
  const body = { code: TOO_MANY_REQUESTS_CODE };
  for (const lang of LANGS) {
    const t = tIn(lang);
    for (const header of [null, undefined, "", "soon"]) {
      assert.equal(
        rateLimitErr(429, header, body, t),
        t("apiErr.rateLimited"),
        `lang=${lang}: a 429 with no usable Retry-After (${JSON.stringify(header)}) did not fall back to apiErr.rateLimited`,
      );
    }
    // The page's generic failure line is exactly what the card says is wrong, so
    // it must not be the answer here either.
    assert.notEqual(rateLimitErr(429, null, body, t), t("login.genericError"));
    // `Retry-After: 0` means "retry now" — still a throttle refusal, still says so.
    assert.equal(rateLimitErr(429, "0", body, t), t("apiErr.rateLimited"));
  }
});

test("a 401 is NOT a throttle — the wrong-credentials sentence survives", () => {
  const t = tIn("en");
  // The uniform 401 the API answers a bad password with.
  const body = { code: "login.invalid", error: "maklumat log masuk tidak sah" };
  assert.equal(rateLimitErr(401, null, body, t), "", "a 401 was answered with a rate-limit sentence");

  // And the page's own composition — `rateLimitErr(...) || apiErr(...)` — still
  // renders the credential sentence the card says must not change.
  const rendered =
    rateLimitErr(401, null, body, t) || apiErr(body, t("login.genericError"), t);
  assert.equal(rendered, "Email or password is incorrect.");
  assert.notEqual(rendered, t("login.genericError"));
});

test("the body is read for its machine code only — the server's prose never renders", () => {
  const t = tIn("en");
  const prose = "Terlalu banyak percubaan. Sila cuba lagi dalam 15 minit.";
  // With a window the sentence comes from the dictionary.
  const withHeader = rateLimitErr(429, "900", { code: TOO_MANY_REQUESTS_CODE, error: prose }, t);
  assert.doesNotMatch(withHeader, /percubaan/i, `the Malay wire prose reached the screen: ${JSON.stringify(withHeader)}`);
  assert.doesNotMatch(withHeader, /Sila cuba/i, `the Malay wire prose reached the screen: ${JSON.stringify(withHeader)}`);
  // Without one, the dictionary's generic throttle row — still not the prose.
  assert.doesNotMatch(rateLimitErr(429, null, { code: TOO_MANY_REQUESTS_CODE, error: prose }, t), /percubaan/i);
  // A body that carries the code but not the status (the hop or an intermediate
  // proxy rewriting the status) is still recognised as a throttle refusal.
  assert.equal(rateLimitErr(500, "900", { code: TOO_MANY_REQUESTS_CODE }, t), "Too many attempts — try again in 15 minutes.");
  // ...and a body with neither is not this helper's business.
  assert.equal(rateLimitErr(500, "900", { code: "internal" }, t), "");
  assert.equal(rateLimitErr(429, "900", { code: "internal" }, t), "Too many attempts — try again in 15 minutes.", "the status half alone must be enough");
});

test("the window keys are real, translated, and keep their placeholder", () => {
  for (const key of ["apiErr.tooManyRequestsInMinutes", "apiErr.tooManyRequestsInSeconds"] as const) {
    const entry = dict[key];
    assert.ok(entry, `${key} is not in DICT`);
    const want = key.endsWith("Minutes") ? "{minutes}" : "{seconds}";
    for (const lang of LANGS) {
      const v = entry[lang];
      assert.equal(typeof v, "string", `${key} has no ${lang} translation`);
      assert.match(v as string, new RegExp(want.replace(/[{}]/g, "\\$&")), `${key}.${lang} dropped ${want}`);
    }
  }
});

test("the login page really reads the header off the response, on every surface a 429 can reach", () => {
  // The call-site half. A helper nobody calls passes every assertion above while
  // the page keeps `apiErr(d, t("login.genericError"), t)`.
  const submit = functionBody(LOGIN_PAGE, "submit");
  assert.match(submit, /\brateLimitErr\(r\.status, r\.headers\.get\("retry-after"\)/, "submit no longer reads Retry-After off the response");
  // The pre-existing per-mode sentences are still underneath it: a non-429 is
  // untouched.
  assert.match(submit, /apiErr\(errBody, t\("login\.genericError"\), t\)/, "the magic-link/forgot branch lost its own sentence");
  assert.match(submit, /setError\(\s*rateLimitErr\(r\.status, r\.headers\.get\("retry-after"\), d, t\) \|\| apiErr\(d, t\("login\.genericError"\), t\)\s*\)/, "the password branch lost its own sentence");

  // The 2FA step: a 429 there is still a throttle, and the code sentence stays
  // the fallback for a 401 (`totpErr`, card t_adf8e1e6).
  const totp = functionBody(LOGIN_PAGE, "submitTotp");
  assert.match(totp, /\brateLimitErr\(r\.status, r\.headers\.get\("retry-after"\), d, t\) \|\| totpErr\(d, t\)/, "submitTotp lost the throttle sentence or the 2FA code sentence");
  // The emailed-backup-code control, a throttled POST of its own.
  const emailCode = functionBody(LOGIN_PAGE, "requestEmailCode");
  assert.match(emailCode, /\brateLimitErr\(r\.status, r\.headers\.get\("retry-after"\), \{\}, t\)/, "requestEmailCode ignores a 429 window");
});
