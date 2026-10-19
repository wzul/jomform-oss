// Guard for card t_adf8e1e6 — the 2FA step must not answer a bad CODE with the
// step-1 sentence "Email or password is incorrect."
//
// THE DEFECT (measured in a real browser on the v1.21.242 build)
//   `/login` step 2 posts the code to the SAME `/api/auth/login` endpoint, and the
//   server answers a bad code with the same deliberately uniform `401` +
//   `login.invalid` it uses for a bad password (api/internal/httpapi/auth.go's
//   `failLogin` — identical body for a known and an unknown email, so the refusal
//   cannot enumerate accounts). That wire contract is correct and stays.
//
//   The page mapped that body through the shared dictionary, so four different
//   code failures all read:
//
//     role=alert "Email or password is incorrect."
//
//   — a replayed code, a wrong 6-digit code, a correct code submitted after the
//   real 10-minute TTL, and a superseded code (a second request issued). The user
//   had passed the password check one screen earlier; the sentence blamed the
//   credential that was not the problem and never mentioned expiry or
//   re-requesting the code.
//
// WHAT THIS FILE PINS, AND WHY EACH ONE IS THE THING THAT BREAKS
//   1. The step-2 re-map answers `login.invalid` with a sentence about the CODE, in
//      all five locales, and that sentence is NOT the dictionary's credential
//      sentence. This is the test the card asks to bite: neutralising the mapping
//      (its value changed back to `apiErr.loginInvalid`) turns it RED.
//   2. The English sentence actually says what went wrong — it mentions the code
//      and expiry, and it never says "password". A re-map to a DIFFERENT
//      wrong-credential sentence would pass a `!==` check, so the wording is
//      asserted, not just the inequality.
//   3. Everything else on this step still lands on the step's OWN generic sentence:
//      an uncoded body, a non-JSON body (`{}` from `r.json().catch(() => ({}))`), a
//      code this build does not know, and a body carrying server prose. This is
//      what "keep login.genericError for transport failures" means, and it is also
//      the property that stops the re-map from swallowing a 429 or a proxy error
//      page into a sentence about a code.
//   4. A code the dictionary ALREADY answers keeps its own sentence — the re-map
//      overrides the codes it names and nothing else.
//   5. The override names a code the server can actually send, read out of
//      errcode.go (not restated), so a re-map for a retired/renamed code cannot sit
//      here looking live.
//   6. `submitTotp` is the call site that uses it, and step 1 is NOT switched to it.
//      A helper nothing calls passes every assertion above while the page keeps the
//      old line — the shape this repo has been bitten by before (`errReachability`
//      says the same thing about an unmounted banner).
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
import { TOTP_STEP_ERR_KEYS, totpErr } from "./loginTotpErr.ts";

const dict = DICT as Record<string, Record<string, unknown>>;

/**
 * A translator over the REAL dictionary that asserts the key exists, so a
 * mistyped key fails this test instead of silently rendering the key itself
 * (`t()` in components/i18n.tsx falls back to the key when an entry is absent).
 */
function tIn(lang: string) {
  return (k: string): string => {
    const entry = dict[k];
    assert.ok(entry, `the ${lang} translator was asked for ${k}, which is not in DICT`);
    const v = entry[lang];
    return typeof v === "string" ? v : (entry.en as string);
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

test("a refused 2FA code is answered with the CODE sentence, in all five locales", () => {
  // The exact body the API returns for a bad code: the uniform 401 envelope.
  const body = { error: "maklumat log masuk tidak sah", code: "login.invalid" };

  for (const lang of LANGS) {
    const t = tIn(lang);
    const rendered = totpErr(body, t);

    assert.equal(
      rendered,
      t("login.totpInvalid"),
      `lang=${lang}: the 2FA step did not use its own sentence for login.invalid`,
    );
    // THE BITE (card t_adf8e1e6): neutralise the mapping — set
    // TOTP_STEP_ERR_KEYS["login.invalid"] back to "apiErr.loginInvalid" — and this
    // assertion fails in every locale, because the step then re-renders the
    // step-1 credential sentence.
    assert.notEqual(
      rendered,
      t("apiErr.loginInvalid"),
      `lang=${lang}: the 2FA step rendered the WRONG-CREDENTIAL sentence ` +
        `("${t("apiErr.loginInvalid")}") for a refused code`,
    );
    // Neither sentence may be blank: an empty entry would satisfy `!==` above.
    assert.ok(rendered.trim() !== "", `lang=${lang}: the 2FA refusal rendered nothing`);
  }
});

test("the English refusal names the code and the expiry, and never the password", () => {
  const en = tIn("en");
  const rendered = totpErr({ code: "login.invalid" }, en);

  assert.equal(rendered, "That code is invalid or expired. Request a new one.");
  assert.match(rendered, /code/i, `the refusal must say it is about the CODE: ${JSON.stringify(rendered)}`);
  assert.match(rendered, /expired/i, `the refusal must mention EXPIRY: ${JSON.stringify(rendered)}`);
  // The action the user can take. "Request a new one" is what the step's
  // "Send a code via email" control does.
  assert.match(rendered, /new one|request a new/i, `the refusal must point at re-requesting: ${JSON.stringify(rendered)}`);
  // The defect itself: the word the old sentence blamed.
  assert.doesNotMatch(rendered, /password/i, `the refusal must not blame the password: ${JSON.stringify(rendered)}`);
  assert.doesNotMatch(rendered, /e-?mail or/i, `the refusal must not blame the email: ${JSON.stringify(rendered)}`);
});

test("every non-code failure on the step keeps the step's generic sentence", () => {
  const t = tIn("en");
  const fallback = t("login.genericError");
  assert.ok(fallback.trim() !== "");

  // A non-JSON body — `r.json().catch(() => ({}))` in submitTotp — and the
  // no-body cases. A transport failure must never be reported as a bad code.
  for (const [label, body] of [
    ["an empty body", {}],
    ["null", null],
    ["undefined", undefined],
    ["a body with prose but no code", { error: "some proxied prose" }],
    // A code a later server adds: the page's own sentence must win, or the
    // re-map becomes a "translate nothing new" trap.
    ["a code this build does not know", { error: "sesuatu", code: "future.code.notInThisBuild" }],
  ] as const) {
    assert.equal(totpErr(body, t), fallback, `${label} did not fall back to login.genericError`);
  }

  // A non-string code must not be looked up as a key either.
  assert.equal(totpErr({ code: 42 }, t), fallback);
});

test("the server's prose is never rendered on this step either", () => {
  const t = tIn("en");
  const rendered = totpErr({ error: "maklumat log masuk tidak sah", code: "login.invalid" }, t);
  assert.doesNotMatch(rendered, /maklumat log masuk tidak sah/, "the Malay wire prose reached the screen");
  // Same for an uncoded body that carries prose.
  assert.doesNotMatch(totpErr({ error: "maklumat log masuk tidak sah" }, t), /maklumat/);
});

test("a code the dictionary already answers keeps ITS sentence (the re-map is narrow)", () => {
  const t = tIn("en");
  // The login limiter's 429 is JSON with a code, and it is the ONE failure on
  // this step whose cause is neither the code nor the credentials.
  assert.equal(totpErr({ code: "rate.limited" }, t), t("apiErr.rateLimited"));
  assert.notEqual(totpErr({ code: "rate.limited" }, t), t("login.totpInvalid"));
});

test("the override names a code the server can really send", () => {
  // Read the code table out of the Go source rather than restating it: an
  // override keyed on a renamed or retired code can never fire, and would look
  // perfectly live in this file.
  const errcode = readFileSync(
    fileURLToPath(new URL("../../../api/internal/httpapi/errcode.go", import.meta.url)),
    "utf8",
  );
  const serverCodes = new Set([...errcode.matchAll(/^\s*Code\w+\s*=\s*"([^"]+)"/gm)].map((m) => m[1]));
  assert.ok(serverCodes.has("login.invalid"), "errcode.go no longer declares login.invalid — this guard must be re-pointed");

  for (const code of Object.keys(TOTP_STEP_ERR_KEYS)) {
    assert.ok(serverCodes.has(code), `TOTP_STEP_ERR_KEYS names "${code}", which the server never sends`);
  }
  // And every sentence it points at is a real, fully translated key.
  for (const key of Object.values(TOTP_STEP_ERR_KEYS)) {
    const entry = dict[key];
    assert.ok(entry, `TOTP_STEP_ERR_KEYS → "${key}", which is not in DICT`);
    for (const lang of LANGS) {
      const v = entry[lang];
      assert.equal(typeof v, "string", `${key} has no ${lang} translation`);
      assert.ok((v as string).trim() !== "", `${key} has an empty ${lang} translation`);
    }
  }
});

test("step 2 renders it and step 1 does NOT", () => {
  // The call-site half. A helper nobody calls passes every assertion above while
  // the page keeps `apiErr(d, t("login.genericError"), t)` on step 2.
  const totp = functionBody(LOGIN_PAGE, "submitTotp");
  assert.match(totp, /\btotpErr\(/, "submitTotp no longer routes its failure through totpErr");
  assert.doesNotMatch(totp, /\bapiErr\(/, "submitTotp still maps the body through the step-1 dictionary");

  // Step 1 keeps the credential sentence: a bad PASSWORD must still read
  // "Email or password is incorrect." — the fix is about which step the user is
  // on, not about softening the credential refusal.
  const step1 = functionBody(LOGIN_PAGE, "submit");
  assert.match(step1, /setError\(apiErr\(d, t\("login\.genericError"\), t\)\)/, "step 1 no longer uses the shared dictionary sentence");
  assert.doesNotMatch(step1, /\btotpErr\(/, "step 1 was switched to the 2FA sentence");
});
