// The two /brand images carry DICTIONARY alts (`brand.logoAlt` / `brand.bannerAlt`,
// five locales), and `/brand` is an English-only prose surface: its document
// declares `lang="en"` however the visitor's dashboard is set (card t_4d3691b9).
// An Arabic ALT inside that document is a language CHANGE that nothing declares,
// so a screen reader reads "شعار JomForm" with an English voice — WCAG 3.1.2
// (Language of Parts), the class `docsChrome.tsx`'s `T` island fixed for the
// translated /docs chrome labels.
//
// WHAT IS PINNED HERE (both halves are load-bearing)
//   1. THE DECISION, executed: `ownLangProps` declares every non-English locale
//      and adds NOTHING for English (the document's own language). Imported and
//      CALLED, so this is an executed assertion, not a text scan.
//   2. THE CALL SITES, source: `/brand` reads `lang` from `useI18n()` — the
//      RESOLVED locale, not a literal — and BOTH image elements spread
//      `ownLangProps(lang)`. React cannot be rendered by this repo's unit runner
//      (no vitest/jest — `node --test` over `src/lib/*.test.ts`), so this leg is a
//      source pin; the EXECUTED proof that the served DOM carries the attribute
//      is `e2e/a11y-docs-lang.spec.ts`, run by the `a11y` gate.
//
// WHY COMMENTS ARE STRIPPED FIRST. A raw-substring pin is satisfied by moving the
// code into a comment (measured in this repo's guard history more than once), so
// the positive pins below read the file with comments removed; the self-check at
// the bottom proves the stripper really strips.
//
// Run with:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { ownLangProps } from "./ownLang.ts";
import { LANGS } from "../components/i18nDict.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

/**
 * Remove `//`, `/* *\/` and JSX `{/* *\/}` comments from TS/JSX source, leaving
 * string and template literals intact (a `//` inside a URL literal is not a
 * comment). Enough for the pins below; the self-check proves it.
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
          i++;
          if (i < src.length) out += src[i];
          i++;
          continue;
        }
        if (src[i] === quote) break;
        i++;
      }
      i++;
      continue;
    }
    out += c;
    i++;
  }
  return out;
}

test("ownLangProps declares every non-English locale and adds nothing for English", () => {
  assert.deepEqual(
    ownLangProps("en"),
    {},
    "English is the document's own language on an English-only prose surface — it must add no lang of its own",
  );
  for (const l of LANGS) {
    if (l === "en") continue;
    assert.deepEqual(
      ownLangProps(l),
      { lang: l },
      `an element holding ${l} text must declare ${l}, not the document's English`,
    );
  }
});

test("both /brand image alts take their language from the resolved locale", () => {
  const src = stripComments(read("../app/brand/brand-client.tsx"));

  assert.match(
    src,
    /const\s*\{\s*t\s*,\s*lang\s*\}\s*=\s*useI18n\(\)/,
    "app/brand/brand-client.tsx no longer reads `lang` from useI18n() — the ALTs' declaration must be the RESOLVED locale, never a literal",
  );

  const imageTags = src.match(/<(?:Image|img)\b[^>]*\/>/g) ?? [];
  assert.equal(
    imageTags.length,
    2,
    "app/brand/brand-client.tsx should render exactly two images (the logo mark and the banner); the pin below judges every one it finds",
  );
  for (const tag of imageTags) {
    assert.match(
      tag,
      /alt=\{t\("brand\.(logoAlt|bannerAlt)"\)\}/,
      `an image on /brand lost its dictionary alt: ${tag.slice(0, 80)}`,
    );
    assert.match(
      tag,
      /\{\.\.\.ownLangProps\(lang\)\}/,
      `an image on /brand carries a translated alt but no language of its own (WCAG 3.1.2): ${tag.slice(0, 80)}`,
    );
  }

  assert.doesNotMatch(
    src,
    /\blang="/,
    "app/brand/brand-client.tsx hard-codes a lang= literal — it is wrong on every other locale and unreadable on this one",
  );
});

test("the comment stripper really strips (this guard's self-check)", () => {
  const stripped = stripComments(`// alt={t("brand.logoAlt")} {...ownLangProps(lang)}\nconst a = "keep // me";`);
  assert.doesNotMatch(stripped, /ownLangProps/, "a comment satisfied the pin — the stripper is broken");
  assert.match(stripped, /keep \/\/ me/, "the stripper ate a string literal — the pin would read mangled source");
});
