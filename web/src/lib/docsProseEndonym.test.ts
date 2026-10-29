// The /docs GUIDES name each supported language in that language's own script,
// inside a document that declares `lang="en"` — so the element holding the name
// must declare the name's language (WCAG 3.1.2 Language of Parts), or a screen
// reader pronounces 中文 with an English voice.
//
// THE DEFECT, measured live on prod v1.21.333 (fresh browser state, document
// `lang="en"`):
//
//   /docs/customize  <strong>English, Bahasa Melayu, 中文, தமிழ்</strong>  → nearest lang "en"
//   /docs/customize  <strong>العربية</strong>                              → nearest lang "en"
//   /docs/chat       "…English, Bahasa Melayu, 中文, தமிழ் or العربية."    → nearest lang "en"
//
// 3 elements per page, every one of them resolving to the document's "en". The
// /docs CHROME labels had already been fixed (docsChrome.tsx); the prose bodies
// carried the names as LITERALS in JSX, which is why a fix at the shared
// switcher/picker components could not reach them.
//
// WHAT IS PINNED HERE (both halves are load-bearing)
//   1. THE DECISION, executed: every locale has a name in `ENDONYM_LABEL`, the
//      name is written in the script `ENDONYM_LANG` says it is, and the two
//      Latin names (English, Bahasa Melayu) declare nothing. This is imported
//      and CALLED, so it is an executed assertion, not a text scan.
//   2. THE CALL SITES, source: the guides contain no endonym LITERAL at all, and
//      each one renders the three non-Latin names through `<Endonym l="…" />`,
//      the one element that puts the declaration on the text it holds. React
//      cannot be rendered by this repo's unit runner (no vitest/jest — `node
//      --test` over `src/lib/*.test.ts`), so this leg is a source pin; the
//      EXECUTED proof that the SERVED DOM carries the attribute is the sweep in
//      `e2e/a11y-docs-lang.spec.ts`, run by the `a11y` gate.
//
// WHY THE LITERAL RULE READS THE RAW FILE and the other rules read stripped
// source. A raw-substring guard is satisfied by moving code into a comment (this
// repo has measured that more than once), so every POSITIVE pin runs on comments
// stripped. The ABSENCE pin runs on the RAW text on purpose: an endonym in a
// comment is refused too, which is the conservative direction (it can only turn a
// green run red, never hide a literal).
//
// WHY THE STRIPPER IS LOCAL TO THIS FILE. Importing it from
// `endonymLang.test.ts` would register that module's own tests a second time in
// this process, because it IS a test file. The two copies are 25 lines of the same
// scanner; see the self-check at the bottom, which proves this one really strips.
//
// Run with:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { ENDONYM_LABEL, ENDONYM_LANG, endonymLangProps } from "./endonymLang.ts";
import { LANGS, type Lang } from "../components/i18nDict.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

/** The script each endonym's own text must be written in, keyed by `lang` tag. */
const SCRIPT: Readonly<Record<string, RegExp>> = {
  zh: /[\u3400-\u4dbf\u4e00-\u9fff\uf900-\ufaff]/,
  ta: /[\u0b80-\u0bff]/,
  ar: /[\u0600-\u06ff]/,
};

/**
 * Remove `//`, `/* *\/` and JSX `{/* *\/}` comments from TS/JSX source, leaving
 * string and template literals intact (a `//` inside a URL literal is not a
 * comment). Enough for the pins below, and the self-check at the bottom proves it
 * does what they depend on.
 */
function stripComments(src: string): string {
  let out = "";
  let i = 0;
  while (i < src.length) {
    const c = src[i];
    const n = src[i + 1];
    if (c === "/" && n === "/") {
      while (i < src.length && src[i] !== "\n") i++;
      continue;
    }
    if (c === "/" && n === "*") {
      i += 2;
      while (i < src.length && !(src[i] === "*" && src[i + 1] === "/")) i++;
      i += 2;
      continue;
    }
    if (c === '"' || c === "'" || c === "`") {
      const quote = c;
      out += c;
      i++;
      while (i < src.length) {
        out += src[i];
        if (src[i] === "\\") {
          out += src[i + 1] ?? "";
          i += 2;
          continue;
        }
        if (src[i] === quote) {
          i++;
          break;
        }
        i++;
      }
      continue;
    }
    out += c;
    i++;
  }
  return out;
}

// The two guide pages this card measured, as paths from this file's directory.
const CHAT_PAGE = "../app/docs/chat/page.tsx";
const CUSTOMIZE_PAGE = "../app/docs/customize/page.tsx";
const ENDONYM_ELEMENT = "../components/Endonym.tsx";

const chatRaw = read(CHAT_PAGE);
const customizeRaw = read(CUSTOMIZE_PAGE);

test("every locale's endonym is written in the script it declares (executed)", () => {
  // Non-vacuity: the names the guides render are entries of this map, so a locale
  // added to the dictionary without a name here fails instead of rendering
  // `undefined` inside an English sentence.
  assert.deepEqual(
    Object.keys(ENDONYM_LABEL).sort(),
    [...LANGS].sort(),
    "ENDONYM_LABEL and the dictionary's LANGS disagree — a locale would render its guide name as undefined",
  );

  for (const l of LANGS as readonly Lang[]) {
    const name = ENDONYM_LABEL[l];
    assert.ok(name.trim().length > 0, `${l} has an empty endonym`);

    const tag = ENDONYM_LANG[l];
    if (tag === null) {
      // English / Bahasa Melayu: Latin letters only, so they render bare on an
      // English page (a `lang` there would restate the document's own language).
      assert.match(
        name,
        /^[A-Za-z][A-Za-z ]*$/,
        `${l} declares no language, so its name must be plain Latin text — got ${JSON.stringify(name)}`,
      );
      assert.deepEqual(endonymLangProps(l), {}, `${l} must add no lang of its own`);
      continue;
    }

    // The executed mapping the guides depend on: the NAME really is in the script
    // the tag claims. A name swapped between locales (中文 labelled `ta`) would
    // render a correct-looking declaration over the wrong text, and only this
    // assertion reads the characters themselves.
    const script = SCRIPT[tag];
    assert.ok(script, `ENDONYM_LANG["${l}"] = "${tag}" has no script the guard can check`);
    assert.match(
      name,
      script,
      `the ${l} endonym ${JSON.stringify(name)} is not written in the script "${tag}" declares — the declaration the guides render would describe the wrong text`,
    );
    assert.deepEqual(
      endonymLangProps(l),
      { lang: tag },
      `${l} must declare lang="${tag}" wherever its name appears in an English document`,
    );
  }

  // Three distinct names: a copy-paste that made two locales share one name would
  // otherwise pass every assertion above.
  const names = (LANGS as readonly Lang[]).map((l) => ENDONYM_LABEL[l]);
  assert.equal(new Set(names).size, names.length, `two locales share one endonym: ${JSON.stringify(names)}`);
});

test("neither guide page carries an endonym literal (raw source)", () => {
  // The shape that shipped: the names as text in JSX, unreachable by any fix at
  // the switcher or the picker. Read RAW, so a comment cannot be used to keep one
  // "just for reference" either.
  for (const [file, src] of [
    [CHAT_PAGE, chatRaw],
    [CUSTOMIZE_PAGE, customizeRaw],
  ] as const) {
    // Non-vacuity in the other direction: this page must still NAME the languages —
    // a page that dropped the sentence entirely would satisfy the absence rule
    // below without telling a reader anything.
    for (const l of ["zh", "ta", "ar"] as const) {
      assert.ok(
        src.includes(`<Endonym l="${l}" />`),
        `${file} no longer renders <Endonym l="${l}" /> — this guide names the supported languages, and the name must come from the shared map`,
      );
    }
    for (const l of LANGS as readonly Lang[]) {
      const name = ENDONYM_LABEL[l];
      // Latin names are words of the English sentence too, so they are pinned
      // through the component but cannot be refused as literals without refusing
      // whole paragraphs; only the three non-Latin names are unique enough.
      if (ENDONYM_LANG[l] === null) continue;
      assert.ok(
        !src.includes(name),
        `${file} carries its own copy of the endonym "${name}" — it moved the fix to the page instead of rendering the shared map, which is exactly how these three stayed undeclared after the switcher was fixed (WCAG 3.1.2)`,
      );
    }
  }
});

test("the guides render the names through the one element that declares them", () => {
  const element = stripComments(read(ENDONYM_ELEMENT));

  // The declaration is on the element that HOLDS the text, and the text comes from
  // the shared map — not from a literal and not from a second copy.
  assert.match(
    element,
    /<span\b[\s\S]{0,200}?\{\.\.\.(props|endonymLangProps\([^)]*\))\}/,
    "components/Endonym.tsx no longer spreads `endonymLangProps(...)` onto the element that holds the name — 中文 on an English page would be undeclared text",
  );
  assert.match(
    element,
    /ENDONYM_LABEL\[/,
    "components/Endonym.tsx no longer renders the shared endonym map — a private copy of the name would drift from the pickers' labels",
  );

  // And each guide's call sites are inside the prose paragraph, read from the
  // page with comments stripped so a commented-out `<Endonym>` cannot stand in
  // for a real one.
  for (const [file, raw] of [
    [CHAT_PAGE, chatRaw],
    [CUSTOMIZE_PAGE, customizeRaw],
  ] as const) {
    const src = stripComments(raw);
    for (const l of ["zh", "ta", "ar"] as const) {
      assert.match(
        src,
        new RegExp(`<Endonym\\s+l="${l}"\\s*/>`),
        `${file}: the ${l} name is no longer rendered by <Endonym l="${l}" /> in code — a comment does not declare anything`,
      );
    }
  }
});

test("the guards read code, not comments (self-check)", () => {
  // A decoy shaped like a real call site, holding the endonym ONLY in a comment.
  const decoy = [
    "export default function Page() {",
    "  return (",
    "    <p>",
    "      // <Endonym l=\"zh\" />",
    "      中文",
    "    </p>",
    "  );",
    "}",
  ].join("\n");

  const site = /<Endonym\s+l="zh"\s*\/>/;
  // The raw text satisfies the call-site rule (which is why stripping matters)…
  assert.match(decoy, site, "the self-check decoy is wrong: its raw text should satisfy the call-site rule");
  // …and the stripped text does not.
  assert.doesNotMatch(
    stripComments(decoy),
    site,
    "a commented-out <Endonym> must not satisfy the call-site pin",
  );

  // The absence rule is the other direction: raw text, so a literal cannot be kept
  // alive inside a comment. Both facts together are why the two rules use
  // different views of the file.
  const kept = "// we used to write 中文 here\n<p>plain</p>";
  assert.ok(kept.includes(ENDONYM_LABEL.zh), "the absence rule's raw view must see a literal in a comment");
});
