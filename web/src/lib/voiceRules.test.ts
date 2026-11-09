// Enforces docs/VOICE.md on the English side of the UI dictionary.
//
// The rules live here, not only in the document, because a document drifts:
// 208 of 2,153 English strings carried an em dash and the same file spelled
// "colour" and "customize". Injecting one em dash fails this run.
//
// The harness proves it saw the dictionary before it checks it. An earlier
// audit under-counted the dashed strings four-fold because its key pattern
// excluded capitals, and a rule that passes on an empty extraction is nothing.

import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { test } from "node:test";

const here = dirname(fileURLToPath(import.meta.url));
const web = join(here, "..", "..");
const repo = join(web, "..");

const DICT_PATH = join(web, "src", "components", "i18nDict.ts");
const source = readFileSync(DICT_PATH, "utf8");

// Keys may contain capitals (`login.pwOptional`, `set.lpPrimaryColor`,
// `chat.proposal.value.variationSet`). An earlier audit used a lowercase-only
// key pattern and under-counted the dashed strings by 4x, so the class here is
// explicit and the count is asserted below.
//
// Each key's window ENDS where the next key begins. It used to be a fixed 4,000
// characters, which reached into the following entries: when the `en` pattern
// could not read a value (it only understood double quotes) the extractor
// returned the NEXT key's English and attributed it to this one. That is worse
// than skipping it. The next key was counted twice, the real value never
// reached a rule, and four strings passed every check while the pages rendered
// them: land.mcpbody ("… RM25 —") and the em dashes in set.domainsHelp and
// set.pendingDns.
function keyWindows(source: string): Array<{ key: string; window: string }> {
  const key = /"([A-Za-z0-9._]+)"\s*:\s*\{/g;
  const starts: Array<{ key: string; at: number }> = [];
  let m: RegExpExecArray | null;
  while ((m = key.exec(source)) !== null) {
    starts.push({ key: m[1], at: m.index + m[0].length });
  }
  return starts.map((entry, i) => ({
    key: entry.key,
    window: source.slice(entry.at, i + 1 < starts.length ? starts[i + 1].at : source.length),
  }));
}

// A value is double-quoted, single-quoted (what a string containing `\"` uses)
// or backticked (what a string spanning lines uses). The opening quote is
// captured once and required again by the backreference, so an apostrophe
// inside a single-quoted value ("we'll") cannot end it early, and `\\.` steps
// over an escaped quote.
const EN_VALUE = /\ben\b\s*:\s*(?<open>['"`])(?<value>(?:[^\\]|\\.)*?)\k<open>/s;

// Whether an entry defines English at all, independent of whether the extractor
// could read it. The coverage test compares the two counts, so a quote style
// the pattern stops understanding fails instead of quietly vanishing.
const DEFINES_EN = /\ben\b\s*:/;

function englishStrings(source: string): Array<{ key: string; text: string }> {
  const out: Array<{ key: string; text: string }> = [];
  for (const { key, window } of keyWindows(source)) {
    const en = EN_VALUE.exec(window);
    if (en) out.push({ key, text: en.groups!.value });
  }
  return out;
}

const strings = englishStrings(source);

// A string that IS code, not prose: the CSS placeholder shows `color:#e11d48`.
const CODE_KEYS = new Set(["bld.inlineStylePh"]);

test("the harness sees the whole dictionary", () => {
  // 2,153 English strings on the day this was written. A hard floor, not an
  // exact count, so adding strings never breaks the build but emptying the file
  // (or breaking the extractor) does.
  assert.ok(
    strings.length > 2000,
    `expected the extractor to find every English string, found ${strings.length}`,
  );
  // The floor above cannot tell "2,170 strings" from "2,170 strings, four of
  // them a later key's". This can: every entry that defines `en` must yield a
  // value, so an unread quote style fails loudly instead of shifting the next
  // key's text into this key's rules.
  const windows = keyWindows(source);
  const defining = windows.filter(({ window }) => DEFINES_EN.test(window)).length;
  assert.equal(
    strings.length,
    defining,
    `${defining} keys define an English value but the extractor read ${strings.length}: ` +
      windows
        .filter(({ window }) => DEFINES_EN.test(window) && !EN_VALUE.exec(window))
        .map(({ key }) => key)
        .join(", "),
  );
});

test("a value in any quote style is read, not borrowed from the next key", () => {
  // Written the way the dictionary writes them. The old extractor returned the
  // NEXT key's English for the first two, so the rules never saw them.
  const tick = "`";
  const sample = [
    '  "sample.single": { en: \'Say \\"hello\\" then stop\', ms: "x" },',
    `  "sample.backtick": { en: ${tick}{domain} added${tick}, ms: "y" },`,
    '  "sample.double": { en: "Dashboard", ms: "z" },',
  ].join("\n");
  assert.deepEqual(englishStrings(sample), [
    { key: "sample.single", text: 'Say \\"hello\\" then stop' },
    { key: "sample.backtick", text: "{domain} added" },
    { key: "sample.double", text: "Dashboard" },
  ]);
});

test("no English string carries an em dash", () => {
  const bad = strings.filter((s) => s.text.includes("—"));
  assert.deepEqual(
    bad.map((s) => s.key),
    [],
    "use a full stop, a comma, or a colon: " + bad.map((s) => `${s.key}: ${s.text}`).join(" | "),
  );
});

test("the dictionary is British English, not mixed", () => {
  // Only the American spellings. `colou?r` would match "colour" as well, so
  // each form is written out: the test must fail on the wrong spelling only.
  const us = /\b(color|behavior|customize|customized|customizes|customizing|favorite|favorites|organize|organizes|organized|organizing)\b/gi;
  const bad = strings
    .filter((s) => !CODE_KEYS.has(s.key))
    .map((s) => ({ key: s.key, hits: s.text.match(us) ?? [] }))
    .filter((s) => s.hits.length > 0)
    .map((s) => `${s.key}: ${s.hits.join(", ")}`);
  assert.deepEqual(bad, [], "use colour, behaviour, customise, favourite, organise, licence");
});

test("money in an English string carries two decimals", () => {
  const bare = /RM\d+(?![\d.,])/g;
  const bad = strings
    .filter((s) => !CODE_KEYS.has(s.key))
    .flatMap((s) => (s.text.match(bare) ?? []).map((hit) => `${s.key}: ${hit}`));
  assert.deepEqual(bad, [], "write RM25.00, not RM25");
});

test("no English sentence runs past 30 words", () => {
  const bad: string[] = [];
  for (const s of strings) {
    for (const sentence of s.text.split(/(?<=[.!?])\s+/)) {
      const words = sentence.trim().split(/\s+/).filter(Boolean).length;
      if (words > 30) bad.push(`${s.key}: ${words} words — ${sentence.slice(0, 60)}…`);
    }
  }
  assert.deepEqual(bad, [], "split the sentence");
});

test("docs/VOICE.md is still there", (t) => {
  // THE WEB IMAGE BUILD DOES NOT SHIP THIS FILE, so the check must not run there.
  // `web/Dockerfile` copies `web/`, `api/` and `MCP-PARITY.md` and NOT the repo-root
  // `docs/` tree (`.dockerignore` drops `docs`), while `npm run build` (= this suite,
  // via `test:unit`) runs inside that image — so the document is genuinely absent and
  // this assertion failed on it (measured: the `webimage` gate RED on this PR with
  // "docs/VOICE.md is still there"). That is not a regression in the guard and not a
  // reason to drop it: the subject does not exist in that environment.
  //
  // The skip is identified the SAME way the sibling guards that read checkout-only
  // files do (`web/src/lib/a11yScanProse.test.ts`, `nextManagedFiles.test.ts`,
  // `harnessLock.test.ts`, card `t_10065a1e`): no `.git` AND no checkout-only root
  // entry. The conjunction is load-bearing in both directions — a real checkout whose
  // git metadata was lost still carries `.gitignore` and therefore still FAILS here
  // rather than quietly skipping.
  const CHECKOUT_ONLY_ROOT_ENTRIES = [".gitignore", "Makefile", "AGENTS.md"];
  if (!existsSync(join(repo, "docs", "VOICE.md"))) {
    const rootEntries = CHECKOUT_ONLY_ROOT_ENTRIES.filter((e) => existsSync(join(repo, e)));
    if (!existsSync(join(repo, ".git")) && rootEntries.length === 0) {
      t.skip(
        `the web image build has no repository and no repo-root docs/ tree: ${repo} has no ` +
          `.git and no checkout-only root entry (${CHECKOUT_ONLY_ROOT_ENTRIES.join(", ")}), ` +
          `and web/Dockerfile copies web/, api/ and MCP-PARITY.md but not docs/`,
      );
      return;
    }
  }
  assert.ok(existsSync(join(repo, "docs", "VOICE.md")), "the rules the dictionary is checked against");
});
