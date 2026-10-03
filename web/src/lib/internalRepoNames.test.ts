// Guard: user-facing copy must not name JomForm's INTERNAL plugin repos
// (card t_1f09073d).
//
// WHY THIS FILE EXISTS
//
// Wan, 2026-09-21:
//
//   "pasal ni, x perlu mention 'like chip for gravityforms'. general users tak tahu
//    pun apa benda chip for gravityforms tu. haiya"
//
// `fs.chipHint` — rendered on the form settings page next to the per-form CHIP
// credential fields — told the merchant the feature was "a feed, like
// chip-for-gravity-forms". Nobody using JomForm has heard of that repository, cannot
// install it, cannot look it up, and it is OUR internal plugin repo name besides. The
// hint sent a merchant to compare their product against a thing they cannot obtain.
//
// It shipped in ALL FIVE locales, so an `en`-only fix would have left four of them
// pointing at the same repo.
//
// WHAT IS PINNED HERE, and why it can fail
//
//   1. No internal repo name appears anywhere in the dictionary — the locale strings
//      a merchant actually reads. The pattern is a family, not one literal, because
//      the next leak of this class will name a different sibling.
//   2. The hint still DESCRIBES the feature, in every locale, and still says what
//      happens when it is left blank. Deleting the sentence is not the fix; the
//      merchant still has to know what the fields are for.
//   3. THE BITE PROOF (last test) puts the pre-fix wording back IN MEMORY and
//      requires the scan to go RED naming the line. A guard whose failure mode is
//      never exercised inside the suite that runs it rots with every gate green.

import { test } from "node:test";
import assert from "node:assert/strict";

import { DICT } from "../components/i18nDict.ts";

type Dict = Record<string, Record<string, string>>;

// JomForm's own plugin-repo family, plus the sibling internal names. Matched
// case-insensitively: the pre-fix copy used lower-case, but a future edit may not.
const INTERNAL_REPO = /chip-for-[a-z-]+|chip-send|chip-db-mcp|CHIP-COS|CHIP-Asia\/[a-z-]+/i;

// scanRepoNames returns "key[locale]: <match>" for every value that names an internal
// repo. Split out so the bite proof runs the identical judgement on a mutated copy.
function scanRepoNames(d: Dict): string[] {
  const bad: string[] = [];
  for (const [key, entry] of Object.entries(d)) {
    for (const [loc, text] of Object.entries(entry)) {
      if (typeof text !== "string") continue;
      const m = text.match(INTERNAL_REPO);
      if (m) bad.push(`${key}[${loc}]: ${m[0]}`);
    }
  }
  return bad;
}

test("no user-facing copy names an internal plugin repo", () => {
  const bad = scanRepoNames(DICT as unknown as Dict);
  assert.deepEqual(
    bad,
    [],
    `a merchant read this string and has never heard of the repo it names — they cannot install ` +
      `it, look it up, or compare against it, and it is JomForm's internal plugin repo besides ` +
      `(Wan, 2026-09-21):\n  ${bad.join("\n  ")}`,
  );
});

test("fs.chipHint still explains the feature, in every locale", () => {
  const entry = ((DICT as unknown as Dict)["fs.chipHint"] ?? {}) as Record<string, string>;
  const missing: string[] = [];
  for (const loc of ["en", "ms", "zh", "ta", "ar"]) {
    const text = entry[loc];
    if (typeof text !== "string" || text === "") {
      missing.push(`${loc}: locale absent`);
      continue;
    }
    // It must still say WHAT the fields are (per-form credentials) and what leaving
    // them blank does. A hint gutted to a bare label tells the merchant nothing.
    if (!text.includes("CHIP")) missing.push(`${loc}: no longer mentions CHIP`);
    if (!/blank|kosong|留空|காலியாக|فارغ/i.test(text)) {
      missing.push(`${loc}: no longer says what leaving it blank does`);
    }
  }
  assert.deepEqual(
    missing,
    [],
    `removing the internal repo name is not the whole fix — the hint still has to explain the ` +
      `fields, or the merchant loses the information the sentence was carrying:\n  ` +
      missing.join("\n  "),
  );
});

test("BITES: the pre-fix wording put back into the dictionary is caught, by key", () => {
  const mutated = JSON.parse(JSON.stringify(DICT)) as Dict;
  mutated["fs.chipHint"].en =
    "Per-form CHIP credentials (a feed, like chip-for-gravity-forms). When set, this " +
    "form uses these keys instead of the account defaults. Leave blank to use the " +
    "account-level CHIP settings in Workspace settings.";

  assert.ok(
    // eslint-disable-next-line no-restricted-syntax -- asserting the fixture landed
    (mutated["fs.chipHint"].en as string).includes("chip-for-gravity-forms"),
    "the pre-fix wording was not substituted, so this proof is vacuous",
  );
  const bad = scanRepoNames(mutated);
  assert.ok(
    bad.length > 0,
    "with the pre-fix wording restored, the scan found nothing — so the live check above " +
      "cannot fail on a regression and this guard is decorative",
  );
  assert.ok(
    bad.join(" ").includes("fs.chipHint"),
    `the scan must name the offending key; it reported: ${bad.join(", ")}`,
  );
});
