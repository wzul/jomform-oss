// Guard for card t_563a7d45 — the builder's AI-generate surface must state the
// window the API's throttle actually chose, not "wait a moment".
//
// THE DEFECT (measured on prod v1.22.84, web+api e9591980)
//   POST /api/rest/forms/ai-generate is bounded by
//   `middleware.RateLimitWith(aiGenerateRatePerHour/3600.0, aiGenerateBurst)`
//   (api/internal/httpapi/rest.go): 5 an hour, burst 3. A drained client is
//   therefore refused with
//
//     429, content-type: text/plain
//     Retry-After: 720                     <- 3600/5, twelve minutes
//     "Terlalu banyak percubaan. Cuba lagi sebentar. (429)"
//
//   and the builder answered every 429 with one fixed dictionary sentence,
//
//     setAiMsg(r.status === 429 ? t("bld.aiRateLimited") : t("bld.aiError", ...))
//
//   i.e. "Too many requests. Please wait a moment and try again." The header
//   was never read: a twelve-minute refusal was reported as a moment, by an
//   order of magnitude.
//
// WHY THE SHARED MAPPER AND NOT A SECOND SENTENCE
//   lib/rateLimitErr.ts already builds the localized sentence from the status
//   and the response's own `Retry-After` (card t_e50a9680), and the dashboard
//   hop relays that header verbatim (lib/proxyResponseHeaders.ts). The defect
//   was a caller that did not hand it the header, not a missing capability.
//
// WHAT THIS FILE PINS, AND WHICH ASSERTION BREAKS WHEN IT REGRESSES
//   1. The BITE for the reported case: a 429 carrying `retry-after: 720` renders
//      `apiErr.tooManyRequestsInMinutes` with 12, in all five locales. Neutralise
//      the header read, or revert the page's 429 branch to a fixed sentence, and
//      this is the assertion that fails.
//   2. The other end of the same rule: a one-second window renders the existing
//      generic `apiErr.rateLimited` row, never "1 seconds" and never a minute
//      count. So the generic sentence stays the <= 1 s answer, not the answer for
//      every refusal — which is exactly what /media's 2/s upload bucket produces
//      and why that surface is deliberately NOT changed by this card.
//   3. The CALL SITE, read as source: `aiGenerate()` hands the response's own
//      status and `retry-after` to `rateLimitErr`, composes it with `||` so a
//      non-429 still reaches the surface's own `bld.aiError` sentence, and has no
//      status-equality test left that could shadow the mapper. A helper nobody
//      calls passes (1) and (2) while the merchant still reads the old line — the
//      shape this repo has been bitten by before.
//
// A source pin proves the SHAPE, not the rendered screen; the rendering was
// driven for real in the built page (see the card's completion evidence).
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
import { rateLimitErr } from "./rateLimitErr.ts";

const dict = DICT as Record<string, Record<string, unknown>>;

/** A translator over the REAL dictionary that asserts the key exists, so a
 *  mistyped key fails here instead of rendering the key itself, and substitutes
 *  `{placeholders}` exactly as `t()` does. */
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

const BUILDER_PAGE = fileURLToPath(new URL("../app/builder/page.tsx", import.meta.url));
const PAGE_SOURCE = readFileSync(BUILDER_PAGE, "utf8");

/** The raw text of `aiGenerate`'s declaration, located with the TS parser so a
 *  reflowed line or a comment cannot make this guard silently match nothing. */
function aiGenerateBody(): string {
  const sf = ts.createSourceFile(BUILDER_PAGE, PAGE_SOURCE, ts.ScriptTarget.ESNext, true, ts.ScriptKind.TSX);
  const found: string[] = [];
  const visit = (node: ts.Node) => {
    if (ts.isFunctionDeclaration(node) && node.name?.text === "aiGenerate") found.push(node.getText(sf));
    node.forEachChild(visit);
  };
  visit(sf);
  assert.equal(found.length, 1, `expected exactly one function declaration named aiGenerate, found ${found.length}`);
  return found[0];
}

const aiGenerate = aiGenerateBody();

test("the AI-generate 429 names the server's 12-minute window, in every locale", () => {
  // The exact refusal the route produces once the burst of 3 is spent: the
  // limiter rounds UP to the time one token needs, 1 / (5/3600) = 720 seconds.
  // The body is the plain-text sentence the middleware writes, so it carries no
  // JSON `code` — the status and the header are the whole signal, which is why
  // the mapper must be handed both.
  for (const lang of LANGS) {
    const t = tIn(lang);
    const rendered = rateLimitErr(429, "720", {}, t);

    // THE BITE: replace the call site's header read with a fixed sentence (or
    // `null`) and this fails, because the sentence then names no window.
    assert.match(rendered, /12/, `lang=${lang}: the AI-generate 429 does not name the 12-minute window: ${JSON.stringify(rendered)}`);
    assert.equal(
      rendered,
      t("apiErr.tooManyRequestsInMinutes", { minutes: 12 }),
      `lang=${lang}: not the dictionary's minutes sentence for the window the server chose`,
    );
    // And it is not the "wait a moment" answer the defect printed.
    assert.notEqual(rendered, t("apiErr.rateLimited"), `lang=${lang}: a 12-minute refusal still reads as a short wait`);
  }

  // Asserted in words as well as by key: a translation that dropped "minutes"
  // would still contain "12".
  assert.equal(rateLimitErr(429, "720", {}, tIn("en")), "Too many attempts — try again in 12 minutes.");
});

test("a one-second window stays the generic sentence — the <= 1 s answer, not every refusal", () => {
  // The upload bucket's derived window (uploadRatePerSecond = 2.0 -> 1 s) and
  // any drained 1/s limiter answer this way. That surface is deliberately not
  // changed by this card; this arm keeps the generic row honest for it.
  for (const lang of LANGS) {
    const t = tIn(lang);
    const rendered = rateLimitErr(429, "1", {}, t);
    assert.equal(rendered, t("apiErr.rateLimited"), `lang=${lang}: a one-second window did not render the generic throttle row`);
    assert.notEqual(rendered, t("apiErr.tooManyRequestsInMinutes", { minutes: 1 }), `lang=${lang}: a one-second window was rounded to a minute it does not have`);
  }
  assert.doesNotMatch(rateLimitErr(429, "1", {}, tIn("en")), /\b1 seconds?\b/, "a one-second window rendered its number");
});

test("aiGenerate() hands the response's own status and Retry-After to the shared mapper", () => {
  const startAt = aiGenerate.indexOf("/api/rest/forms/ai-generate");
  assert.ok(startAt > 0, "aiGenerate no longer calls /api/rest/forms/ai-generate — this guard must be re-pointed");

  // 1. The header is read off the RESPONSE, not a constant and not re-derived.
  assert.match(
    aiGenerate,
    /rateLimitErr\(\s*r\.status\s*,\s*r\.headers\.get\(\s*"retry-after"\s*\)/,
    "the AI-generate refusal does not hand the response's retry-after to rateLimitErr",
  );

  // 2. Composed with `||`, so a non-429 keeps this surface's own failure line.
  assert.match(
    aiGenerate,
    /rateLimitErr\([\s\S]{0,160}?\|\|/,
    "the mapper is not composed with `||` — a non-throttle answer would lose its own sentence",
  );
  assert.match(aiGenerate, /t\(\s*"bld\.aiError"/, "the AI-generate failure branch lost its own fallback sentence");

  // 3. No status-equality shortcut left that could shadow the mapper. The
  //    pre-fix shape was `r.status === 429 ? t("bld.aiRateLimited") : ...`.
  assert.doesNotMatch(
    aiGenerate,
    /r\.status\s*===\s*429\s*\?/,
    "the 429 branch is still a status test with a fixed sentence — the header would be dropped again",
  );
  assert.equal(
    PAGE_SOURCE.includes("bld.aiRateLimited"),
    false,
    "the builder still references `bld.aiRateLimited`, the fixed 'wait a moment' sentence this card removed",
  );
});
