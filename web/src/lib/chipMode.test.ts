// Card t_8f38dc34 — the CHIP test/live mode is a CHOICE, not a reading of the
// secret key.
//
// THE DEFECT
//   The settings page derived the mode from the key's first characters
//   (`sk_live_…` → Live, `sk_test_…` → Test, everything else → Test) and the
//   save sent no mode at all, because the API "auto-derived" it from the same
//   prefixes. CHIP keys are opaque bearer tokens: the merchant portal issues a
//   separate key per mode and publishes no prefix
//   (docs.chip-in.asia → CHIP Collect → Authentication → "Test vs live keys").
//   So every real merchant key matched nothing, every workspace was stored as
//   chip_mode='test', and the page said Test beside a live key. Prod measurement
//   on the card: 24 test rows, zero live.
//
// WHY A SOURCE GUARD AND A PURE MODULE
//   The repo has no JSX/DOM unit runner (see `src/lib/i18n.test.ts`), so the
//   control's behaviour is not unit-renderable here. The parts that CAN be
//   pinned cheaply are: the pure defaulting rule (below, exercised directly) and
//   the shapes the page must keep — a control that offers both modes, a save body
//   that carries the choice, and no prefix inference left anywhere in the file or
//   in the copy. The rendered proof is the browser pass recorded on the card.
//
// Run with: npm run test:unit

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { codeOnly } from "./jsxSource.ts";
import { CHIP_MODE_OPTIONS, chipModeFromStored, chipModeLabelKey } from "./chipMode.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");
// Comments stripped, so a sentence describing the rule cannot be mistaken for
// the rule (this repo's guards all read code this way).
const settings = codeOnly(read("../app/settings/page.tsx"));
const dict = read("../components/i18nDict.ts");

test("the control starts at Test for every stored value but live", () => {
  // Test is the safe default: it is what the API stores when no mode has ever
  // been chosen, and it cannot misdescribe a live key as test the other way
  // round without the merchant having said so.
  const cases: Array<[unknown, string]> = [
    [undefined, "test"],
    [null, "test"],
    ["", "test"],
    ["test", "test"],
    ["live", "live"],
    ["LIVE", "test"], // stored values are lower-case; anything else is not a claim we honour
    ["production", "test"],
    [1, "test"],
  ];
  for (const [stored, want] of cases) {
    assert.equal(chipModeFromStored(stored), want, `stored=${JSON.stringify(stored)}`);
  }
});

test("both modes are offered, Test first", () => {
  assert.deepEqual([...CHIP_MODE_OPTIONS], ["test", "live"]);
  // Labels come from the dictionary, so the control and the badge cannot drift.
  assert.equal(chipModeLabelKey("test"), "set.test");
  assert.equal(chipModeLabelKey("live"), "set.live");
});

test("the settings page no longer infers the mode from the secret", () => {
  // The exact shapes that shipped. `detectMode` and a prefix test on the secret
  // are the defect; a stray `sk_live` in this file would be a comment or a
  // placeholder teaching the same false rule.
  assert.ok(!/function\s+detectMode/.test(settings), "detectMode() is back in the settings page");
  assert.ok(!/startsWith\(\s*["']sk_(live|test)/.test(settings), "the page reads a key prefix again");
  assert.ok(!/sk_(live|test)_/.test(settings), "the settings page still mentions an sk_ prefix");
});

test("the save carries the chosen mode", () => {
  // The save body is what the API stores. Without `mode`, the server falls back
  // to the stored mode and a first save is stored as Test.
  const put = settings.slice(settings.indexOf("/api/rest/settings/chip"));
  const body = put.slice(0, put.indexOf("body: JSON.stringify(") + 400);
  assert.ok(/mode:\s*chipMode/.test(body), `the PUT body carries no mode:\n${body.slice(0, 400)}`);
  // …and the control the merchant uses to set it exists, driven by the same list.
  assert.ok(settings.includes("CHIP_MODE_OPTIONS.map"), "the mode control is not rendered");
  assert.ok(/checked=\{chipMode === m\}/.test(settings), "the radio is not bound to the state");
});

test("the ✓ badge still comes from the API, not from the chosen label", () => {
  // A chosen mode is a label, never a verification (issue #325). The badge is
  // fed by `status.verified`, which only the server's live CHIP check sets.
  assert.ok(!/verified:\s*chipMode|verified:\s*true\b/.test(settings), "the page invents the verified flag");
  assert.ok(/setVerified\(!!\s*s\.verified\)/.test(settings), "the stored verified flag is not applied from the API");
});

test("no locale claims the mode is auto-detected or keyed to a prefix", () => {
  const line = (key: string) => {
    const at = dict.indexOf(`"${key}":`);
    assert.ok(at > 0, `${key} is missing from the dictionary`);
    const end = dict.indexOf("\n", at);
    return dict.slice(at, end);
  };
  for (const key of ["set.mode", "set.modeHint"]) {
    const entry = line(key);
    assert.ok(!/auto[- ]?detect|auto-dikesan|自动检测|தானாகக் கண்டறி|مكتشف تلقائ/i.test(entry), `${key} still claims auto-detection`);
    assert.ok(!/sk_(live|test)/.test(entry), `${key} still claims a key prefix`);
  }
  // The key field's placeholder is neutral too: it used to print
  // "sk_live_… / sk_test_…", i.e. it taught the false prefix on the field the
  // merchant pastes their real (prefix-less) key into.
  assert.ok(!/sk_(live|test)_/.test(settings), "the key placeholder still shows a prefix");
});
