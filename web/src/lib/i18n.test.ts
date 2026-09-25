// Issue #514 guard — locale completeness for the dashboard dictionary.
//
// `t()` in ../components/i18n.tsx falls back deliberately:
//
//   let s = (!entry ? k : entry[lang] || entry.en) as string;
//
// So a key that ships with only `en` does NOT crash and does NOT render a raw
// key — it silently shows ENGLISH to ms/zh/ta/ar users. That is why the gap
// survived: nothing failed. AGENTS.md requires i18n in 5 locales, every other
// class of drift in this repo got a guard after it bit us (#472 route parity,
// #477 MCP parity, #499 URL audit, #513 docs parity); i18n never did.
//
// Run with the repo's Node test runner (NOT vitest — a vitest run reports a
// false "No test suite found" on these files):
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
//
// The dictionary is imported from ../components/i18nDict.ts (a .ts module) on
// purpose: node --test executes TypeScript but cannot load the .tsx renderer.
//
// WHAT THIS GUARD COVERS
//   1. Every key defines all five locales (en, ms, zh, ta, ar).
//   2. No locale value is empty or whitespace-only.
//   3. No locale value is placeholder-only (e.g. just "{n}").
//   4. Every {placeholder} in `en` also appears in each other locale — a dropped
//      placeholder is a real bug: the value would be substituted with nothing.
//   5. No unknown per-entry property (e.g. a typo'd `zhh:` or an accidental
//      `fr:`), so a misspelled locale cannot masquerade as "translated".
//   6. The renderer still imports this one dictionary (no second, unguarded copy).
//
// WHAT IT DOES NOT COVER (stated honestly)
//   * A key the CODE calls that is MISSING from the dictionary. This file iterates
//     `Object.keys(DICT)`, so a key that disappears (a merge casualty, a rename,
//     a typo at the call site) is outside what it can see — and `t()` renders the
//     key itself, so the user gets "workspace.askTitle". That direction is
//     src/lib/i18nKeys.test.ts (card t_c2744da3).
//   * Translation QUALITY. It cannot judge whether the Tamil actually says what
//     the English says, nor whether the register/terminology matches sibling
//     keys. A fluent review is the only thing that can.
//   * It does not see strings hardcoded in components (the dictionary is only
//     as complete as the code's willingness to use it), nor keys the code builds
//     at runtime (`t(`fb.status.${s}`)`) — those are counted and disclosed by
//     src/lib/i18nKeys.test.ts.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { DICT, LANGS } from "../components/i18nDict.ts";

// Per-entry non-text metadata that is NOT a locale and must not be read as one.
// `nav.forms` carries `icon: undefined` for the sidebar; that is metadata, not a
// missing translation, so the completeness check iterates LANGS (never the
// entry's own keys) and the unknown-property check allowlists this.
const METADATA_KEYS = ["icon"];

const placeholders = (s: string): string[] =>
  [...new Set(s.match(/\{[a-zA-Z0-9_]+\}/g) ?? [])].sort();

const keys = Object.keys(DICT) as (keyof typeof DICT)[];

test("the dictionary is non-empty (the guard cannot pass vacuously)", () => {
  // A guard that checks nothing is worthless (#507). If the import silently
  // resolves to {} the assertions below would all pass — pin a floor instead.
  assert.ok(
    keys.length > 1000,
    `expected the full dashboard dictionary, got ${keys.length} keys`,
  );
  assert.deepEqual([...LANGS], ["en", "ms", "zh", "ta", "ar"]);
});

test("every dictionary key defines all five locales", () => {
  const missing: string[] = [];
  for (const k of keys) {
    const entry = DICT[k] as Record<string, unknown>;
    for (const lang of LANGS) {
      const v = entry[lang];
      if (typeof v !== "string" || v.trim() === "") missing.push(`${k}.${lang}`);
    }
  }
  assert.deepEqual(
    missing,
    [],
    `these keys are missing an ${LANGS.join("/")} translation (a user on that ` +
      `locale silently sees English):\n  ${missing.join("\n  ")}`,
  );
});

test("no locale value is placeholder-only", () => {
  // "{n}" as an entire translation means the untranslated token was pasted in.
  const placeholderOnly: string[] = [];
  for (const k of keys) {
    const entry = DICT[k] as Record<string, unknown>;
    for (const lang of LANGS) {
      const v = entry[lang];
      if (typeof v !== "string") continue;
      if (v.trim() !== "" && v.replace(/\{[a-zA-Z0-9_]+\}/g, "").trim() === "") {
        placeholderOnly.push(`${k}.${lang} = ${JSON.stringify(v)}`);
      }
    }
  }
  assert.deepEqual(placeholderOnly, []);
});

test("every {placeholder} in en is preserved in every other locale", () => {
  const dropped: string[] = [];
  for (const k of keys) {
    const entry = DICT[k] as Record<string, unknown>;
    const en = entry.en;
    if (typeof en !== "string") continue;
    const want = placeholders(en);
    if (want.length === 0) continue;
    for (const lang of LANGS) {
      if (lang === "en") continue;
      const v = entry[lang];
      if (typeof v !== "string") continue;
      const got = placeholders(v);
      const lost = want.filter((p) => !got.includes(p));
      if (lost.length) dropped.push(`${k}.${lang} is missing ${lost.join(", ")}`);
    }
  }
  assert.deepEqual(dropped, []);
});

test("no entry carries an unknown locale-like property", () => {
  // Metadata (`icon`) is allowed; anything else means a locale was misspelled
  // or an unsupported language was added without wiring it into LANGS.
  const allowed = new Set<string>([...LANGS, ...METADATA_KEYS]);
  const unknown: string[] = [];
  for (const k of keys) {
    for (const prop of Object.keys(DICT[k] as object)) {
      if (!allowed.has(prop)) unknown.push(`${k}.${prop}`);
    }
  }
  assert.deepEqual(unknown, []);
});

test("the renderer imports this one dictionary (no unguarded second copy)", () => {
  // If someone reintroduces a local `const DICT = {` inside i18n.tsx the guard
  // above would keep passing against a dictionary the app no longer uses.
  const renderer = readFileSync(
    fileURLToPath(new URL("../components/i18n.tsx", import.meta.url)),
    "utf8",
  );
  assert.match(renderer, /import\s*\{[^}]*\bDICT\b[^}]*\}\s*from\s*["']\.\/i18nDict(\.ts)?["']/);
  assert.doesNotMatch(renderer, /const\s+DICT\s*=/);
  // The English fallback is correct and safe — pin it so it is not "fixed" away.
  assert.match(renderer, /entry\[lang\]\s*\|\|\s*entry\.en/);
});
