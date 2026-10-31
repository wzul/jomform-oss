// Guard: every WEB-side surface states JomForm's licence the same way (MIT).
//
// WHY THIS EXISTS
//   The project was made private in 2026-09 and seven public surfaces were
//   rewritten to claim it was proprietary — the landing badge and footer in all
//   five locales, the Terms of Service clause on intellectual property, and the
//   agent-facing llms.txt / llms-full.txt. The project is MIT again. Each of those
//   is a plain string or a hand-written file, so nothing compiles, type-checks or
//   renders differently when one is left behind: the app keeps working and simply
//   tells the reader a licence it does not have.
//
//   This file is the WEB half of the class. The repo-root surfaces are pinned in
//   Go, where a test may read them (api/internal/httpapi/licence_surfaces_mit_test.go
//   covers README and LICENSE, and the OpenAPI artifacts, because README/docs are
//   excluded from the web build context — see buildContext.test.ts and the root
//   .dockerignore).
//
// WHAT IT PINS
//   1. llms.txt and llms-full.txt — the machine-readable context an AI agent
//      reads — name the MIT licence instead of calling the service proprietary.
//   2. The landing badge and footer (land.badge / land.footer) say open source +
//      MIT in ALL FIVE locales, because the proprietary wording was translated
//      into each of them and an English-only fix would leave four wrong.
//   3. The Terms of Service clause on intellectual property describes the MIT
//      grant, and no section of the ToS reserves all rights.
//
// WHAT IT DOES NOT COVER
//   * It reads source text; it cannot render the landing page or the ToS, so it
//     sees the strings the pages draw from, not the painted DOM. A surface that
//     stops rendering one of these keys is a different defect (the landing page's
//     own guards pin that land.badge / land.footer are rendered).
//   * It cannot prove the hosted instance's own legal pages have been redeployed.

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { DICT, LANGS } from "../components/i18nDict.ts";
import { TERMS_SECTIONS } from "../app/legal/terms-content.ts";

const PROPRIETARY = /proprietary|all rights reserved/i;
const MIT = /\bMIT\b/;

test("llms.txt tells an agent the code is MIT, not proprietary", () => {
  const body = readFileSync(fileURLToPath(new URL("../../public/llms.txt", import.meta.url)), "utf8");
  // Anchor: the file is really the published context, not an empty file that
  // would make every check below vacuous.
  assert.ok(body.length > 2_000, `llms.txt is only ${body.length} bytes — that is not the published context`);
  assert.doesNotMatch(body, PROPRIETARY, "llms.txt still calls JomForm a proprietary service");
  assert.match(body, MIT, "llms.txt does not name the MIT licence anywhere");
});

test("llms-full.txt tells an agent the code is MIT, not proprietary", () => {
  const body = readFileSync(fileURLToPath(new URL("../../public/llms-full.txt", import.meta.url)), "utf8");
  assert.ok(body.length > 2_000, `llms-full.txt is only ${body.length} bytes — that is not the published context`);
  assert.doesNotMatch(body, PROPRIETARY, "llms-full.txt still calls JomForm a proprietary service");
  assert.match(body, MIT, "llms-full.txt does not name the MIT licence anywhere");
});

test("the landing badge and footer say open source (MIT) in all five locales", () => {
  assert.equal(LANGS.length, 5, `the dictionary declares ${LANGS.length} locales; this guard was written for the five shipped locales`);
  for (const key of ["land.badge", "land.footer"] as const) {
    for (const lang of LANGS) {
      const copy = DICT[key][lang];
      assert.ok(copy && copy.trim() !== "", `${key}.${lang} is empty`);
      assert.match(copy, MIT, `${key}.${lang} does not name MIT: ${JSON.stringify(copy)}`);
      assert.doesNotMatch(
        copy,
        PROPRIETARY,
        `${key}.${lang} still carries the proprietary wording: ${JSON.stringify(copy)}`,
      );
    }
  }
  // The badge is also the product's one-line identity claim, so the licence is
  // not the only thing it must state: it still has to say the service is free.
  assert.match(DICT["land.badge"].en, /free/i, "the landing badge no longer says the service is free");
});

test("the Terms of Service describe the MIT grant and reserve no rights", () => {
  assert.ok(TERMS_SECTIONS.length > 5, "the ToS parsed to almost nothing — the clause lookup below would check nothing");
  const ip = TERMS_SECTIONS.find((s) => /intellectual property/i.test(s.title));
  if (!ip) throw new Error("the ToS has no intellectual-property clause any more");
  const body = ip.body.join(" ");
  assert.match(body, MIT, "the ToS §7 does not name the MIT licence");
  assert.match(body, /LICENSE/, "the ToS §7 does not point at the LICENSE file");
  for (const s of TERMS_SECTIONS) {
    assert.doesNotMatch(
      s.body.join(" "),
      PROPRIETARY,
      `the ToS section ${JSON.stringify(s.title)} still claims proprietary rights`,
    );
  }
});
