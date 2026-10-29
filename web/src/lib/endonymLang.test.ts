// The language switcher names each locale in that locale's OWN language, so the
// element that holds the name must declare which language it is in — otherwise
// a screen reader pronounces 中文 with the voice of the surrounding page
// (WCAG 3.1.2 Language of Parts).
//
// THE DEFECT, measured live on jomform.com/mcp (prod v1.21.190, revision
// 645f31f7, fresh browser state, `jf_lang=en`): three `<button>`s whose own text
// is 中文 / தமிழ் / عربي, each with NO `lang`, each resolving through
// `closest("[lang]")` to the document's "en". With `jf_lang=ar` the same three
// resolved to "ar". The strings live in `components/i18n.tsx` (`LANG_SHORT`) and
// were ALSO copied into `app/mcp/mcp-client.tsx` as a private ternary chain —
// which is why "fix the component" alone would have left the page measured here
// untouched.
//
// WHAT IS PINNED HERE (both halves are load-bearing)
//   1. THE DECISION, executed: `endonymLangProps` maps 中文→zh, தமிழ்→ta,
//      عربي→ar and adds NOTHING for the Latin endonyms (EN/BM are the same
//      scripts the pages that render the switcher are written in — the rule the
//      /docs island already applies to its English label). It is imported and
//      CALLED, so this is an executed assertion, not a text scan.
//   2. THE CALL SITES, source: every element that renders an endonym spreads
//      that function. React cannot be rendered by this repo's unit runner (no
//      vitest/jest — `node --test` over `src/lib/*.test.ts`), so this leg is a
//      source pin; the EXECUTED proof that the served DOM carries the attribute
//      is the browser sweep in `e2e/a11y-switcher-lang.spec.ts`, run by the
//      `a11y` gate.
//
// WHY COMMENTS ARE STRIPPED FIRST. This repo has measured, more than once, that
// a raw-substring guard is satisfied by moving the code into a comment (measured in this repo's own guard history). Every rule below is applied to the file
// with comments removed, and the self-check at the bottom proves the strip is
// load-bearing: the decoy holds the exact text in a comment and the rule must
// NOT match it.
//
// Run with:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { ENDONYM_LANG, endonymLangProps } from "./endonymLang.ts";
import { LANGS } from "../components/i18nDict.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

/**
 * Remove `//`, `/* *\/` and JSX `{/* *\/}` comments from TS/JSX source, leaving
 * string and template literals intact (a `//` inside a URL literal is not a
 * comment). Enough for the pins below, and the self-check at the bottom proves
 * it does what they depend on.
 */
export function stripComments(src: string): string {
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

/** The body of a top-level function declaration, up to the next one. */
function functionBody(src: string, name: string, next: string): string {
  const start = src.indexOf(`export function ${name}(`);
  assert.notEqual(start, -1, `components/i18n.tsx no longer declares ${name}()`);
  const end = src.indexOf(`export function ${next}(`, start);
  assert.notEqual(end, -1, `components/i18n.tsx no longer declares ${next}() after ${name}()`);
  return src.slice(start, end);
}

const i18n = stripComments(read("../components/i18n.tsx"));
const mcpClient = stripComments(read("../app/mcp/mcp-client.tsx"));
// The two LANG_LABEL pickers (/profile and /settings). They render the FULL
// endonym — 中文, தமிழ், العربية — rather than the switcher's short label, and
// they are data-gated pages no unit test can render, so this source leg is the
// only thing between a refactor and an undeclared endonym on either page; the
// executed proof that the SERVED <option> carries the attribute is
// `e2e/a11y-lang-picker-option.spec.ts`, run by the `a11y` gate.
const profilePage = stripComments(read("../app/profile/page.tsx"));
const settingsPage = stripComments(read("../app/settings/page.tsx"));

/**
 * Every `<option>` in `src` that renders `LANG_LABEL[l]`, as the text from the
 * opening tag through the label expression.
 *
 * Anchored on the LABEL and scanned backwards to the nearest `<option`, not on a
 * `<option[^>]*>` regex: `=>` inside an attribute contains the `>` such a scan
 * stops at, and the captured "opening tag" would be truncated before the spread
 * it was supposed to inspect. The count is asserted by the caller, so a page that
 * stopped rendering the picker fails loudly instead of satisfying every loop over
 * an empty list.
 */
function optionSitesRenderingEndonym(src: string, file: string): string[] {
  const needle = "{LANG_LABEL[l]}";
  const sites: string[] = [];
  let at = src.indexOf(needle);
  while (at !== -1) {
    const open = src.lastIndexOf("<option", at);
    assert.notEqual(open, -1, `${file}: {LANG_LABEL[l]} is rendered outside an <option>`);
    sites.push(src.slice(open, at + needle.length));
    at = src.indexOf(needle, at + needle.length);
  }
  return sites;
}

test("every endonym declares the language it is written in (executed)", () => {
  // Non-vacuity: every locale in the dictionary has an entry, so adding a
  // sixth locale forces a decision here instead of silently rendering an
  // undeclared endonym.
  assert.deepEqual(
    Object.keys(ENDONYM_LANG).sort(),
    [...LANGS].sort(),
    "ENDONYM_LANG and the dictionary's LANGS disagree — a locale would render its endonym with no language of its own",
  );

  // The three the card measured, on the page whose document is EN: each endonym
  // is in a script the document is not written in, so each must declare itself.
  for (const [locale, tag] of [
    ["zh", "zh"],
    ["ta", "ta"],
    ["ar", "ar"],
  ] as const) {
    assert.deepEqual(
      endonymLangProps(locale),
      { lang: tag },
      `the ${locale} endonym (中文 / தமிழ் / عربي) must declare lang="${tag}" — on /mcp with jf_lang=en it was read with an English voice`,
    );
  }

  // The Latin endonyms are the same scripts as the pages that render them, so
  // they add nothing — the rule the /docs island applies to its English label.
  for (const locale of ["en", "ms"] as const) {
    assert.deepEqual(
      endonymLangProps(locale),
      {},
      `the ${locale} endonym is Latin and must add no lang — a declaration here would restate the document's own language`,
    );
  }

  // Whatever a future entry declares must be a BCP 47 language tag, not a
  // sentence or a locale name.
  for (const [locale, tag] of Object.entries(ENDONYM_LANG)) {
    if (tag === null) continue;
    assert.match(tag, /^[a-z]{2,3}(-[A-Za-z0-9]{2,8})*$/, `${locale} declares "${tag}", which is not a language tag`);
  }
});

test("LangToggle spreads the declaration onto the button that holds the endonym", () => {
  const body = functionBody(i18n, "LangToggle", "LangSelect");

  // The label really is the shared endonym list — not a literal that could
  // drift from the map above.
  assert.match(body, /\{LANG_SHORT\[l\]\}/, "LangToggle no longer renders LANG_SHORT[l]");

  assert.match(
    body,
    /<button\b[\s\S]{0,400}?\{\.\.\.endonymLangProps\(l\)\}/,
    "a LangToggle <button> renders an endonym with no `{...endonymLangProps(l)}` — 中文 on an English page would be undeclared text (WCAG 3.1.2)",
  );
});

test("LangSelect spreads the declaration onto the option that holds the endonym", () => {
  const start = i18n.indexOf("export function LangSelect(");
  assert.notEqual(start, -1, "components/i18n.tsx no longer declares LangSelect()");
  const body = i18n.slice(start);

  assert.match(body, /\{LANG_SHORT\[l\]\}/, "LangSelect no longer renders LANG_SHORT[l]");
  assert.match(
    body,
    /<option\b[\s\S]{0,200}?\{\.\.\.endonymLangProps\(l\)\}/,
    "a LangSelect <option> renders an endonym with no `{...endonymLangProps(l)}` — an <option> holds text only, so the declaration belongs on the option itself",
  );
});

test("/mcp renders the SHARED endonym list and spreads the declaration", () => {
  // The regression that made this card necessary: /mcp had its own copy of the
  // five labels. A private copy can be fixed at the component and still ship
  // undeclared, so the literals are refused here outright.
  for (const endonym of ["中文", "தமிழ்", "عربي"]) {
    assert.ok(
      !mcpClient.includes(endonym),
      `/mcp carries its own copy of the endonym "${endonym}" — it must render LANG_SHORT so the declaration cannot be forgotten on this surface`,
    );
  }

  assert.match(mcpClient, /\{LANG_SHORT\[l\]\}/, "/mcp no longer renders LANG_SHORT[l]");
  assert.match(
    mcpClient,
    /<button\b[\s\S]{0,400}?\{\.\.\.endonymLangProps\(l\)\}/,
    "the /mcp switcher renders an endonym with no `{...endonymLangProps(l)}` — this is the exact element the card measured with no lang, resolving to the document's \"en\"",
  );
});

test("/profile and /settings spread the declaration onto the option that holds the endonym", () => {
  // The `LANG_LABEL` sites the /mcp card did not touch. Both render the FULL
  // endonym into an <option>, so both need the declaration on the option itself
  // — an <option>'s content model is text, and its nearest ancestor is the
  // <select>, which declares nothing.
  for (const [file, src] of [
    ["app/profile/page.tsx", profilePage],
    ["app/settings/page.tsx", settingsPage],
  ] as const) {
    const sites = optionSitesRenderingEndonym(src, file);
    assert.ok(
      sites.length >= 1,
      `${file} no longer renders an <option> holding LANG_LABEL[l] — this pin would pass over an empty list`,
    );
    for (const site of sites) {
      assert.match(
        site,
        /\{\s*\.\.\.endonymLangProps\(l\)\s*\}/,
        `${file}: an <option> renders an endonym with no \`{...endonymLangProps(l)}\` — ` +
          `on an English page 中文 / தமிழ் / العربية would be text in another language with nothing ` +
          `declaring it (WCAG 3.1.2), exactly the defect measured on the shipped pickers. Site: ` +
          site.replace(/\s+/g, " ").slice(0, 120),
      );
    }

    // A private copy of the endonyms is how /mcp stayed undeclared after the
    // shared components were fixed: the literals moved, the fix did not.
    for (const endonym of ["中文", "தமிழ்", "العربية"]) {
      assert.ok(
        !src.includes(endonym),
        `${file} carries its own copy of the endonym "${endonym}" — it must render LANG_LABEL so the ` +
          `declaration cannot be forgotten on this surface`,
      );
    }
  }
});

test("the guards read code, not comments (self-check)", () => {
  // A decoy shaped like the real call site, with the declaration only inside a
  // comment — the shape several of this repo's older guards were fooled by.
  const decoy = [
    "export function LangToggle() {",
    "  return (",
    "    <button",
    "      onClick={() => setLang(l)}",
    "      // {...endonymLangProps(l)}",
    "    >",
    "      {LANG_SHORT[l]}",
    "    </button>",
    "  );",
    "}",
  ].join("\n");

  const rule = /<button\b[\s\S]{0,400}?\{\.\.\.endonymLangProps\(l\)\}/;
  // The RAW text satisfies the rule (which is why stripping is mandatory)…
  assert.match(decoy, rule, "the self-check decoy is wrong: the raw text should satisfy the rule");
  // …and the stripped text does not. If this ever goes green, every rule above
  // has become comment-satisfiable.
  assert.doesNotMatch(stripComments(decoy), rule, "a commented-out declaration must not satisfy the pin");

  // And the stripper keeps code it must keep: a real declaration beside a
  // comment survives.
  const real = "// {...endonymLangProps(l)}\n<button {...endonymLangProps(l)}>{LANG_SHORT[l]}</button>";
  assert.match(stripComments(real), rule, "the stripper removed a real declaration");

  // The SAME self-check for the <option> pins, which read their site by slicing
  // back from `{LANG_LABEL[l]}` — a decoy whose declaration sits in a comment
  // inside the option must not satisfy them either.
  const optionDecoy = [
    "<option key={l} value={l}>",
    "  // {...endonymLangProps(l)}",
    "  {LANG_LABEL[l]}",
    "</option>",
  ].join("\n");
  const optionRule = /\{\s*\.\.\.endonymLangProps\(l\)\s*\}/;
  assert.match(optionDecoy, optionRule, "the option decoy is wrong: its raw text should satisfy the rule");
  const decoySites = optionSitesRenderingEndonym(stripComments(optionDecoy), "decoy");
  assert.equal(decoySites.length, 1, "the option site scan did not find the decoy's one site");
  assert.doesNotMatch(
    decoySites[0],
    optionRule,
    "a commented-out declaration must not satisfy the option pin",
  );
});
