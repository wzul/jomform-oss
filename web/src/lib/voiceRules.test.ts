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
function englishStrings(): Array<{ key: string; text: string }> {
  const out: Array<{ key: string; text: string }> = [];
  const key = /"([A-Za-z0-9._]+)"\s*:\s*\{/g;
  let m: RegExpExecArray | null;
  while ((m = key.exec(source)) !== null) {
    const window = source.slice(m.index + m[0].length, m.index + m[0].length + 4000);
    const en = /\ben\s*:\s*"((?:[^"\\]|\\.)*)"/s.exec(window);
    if (en) out.push({ key: m[1], text: en[1] });
  }
  return out;
}

const strings = englishStrings();

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
