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
//      that function — and the set of files that can render one is DERIVED from
//      the tree (`web/src`, every file that renders `LANG_SHORT[`/`LANG_LABEL[`),
//      never listed here. React cannot be rendered by this repo's unit runner
//      (no vitest/jest — `node --test` over `src/lib/*.test.ts`), so this leg is
//      a source pin; the EXECUTED proof that the served DOM carries the
//      attribute is the browser sweep in `e2e/a11y-switcher-lang.spec.ts` and
//      `e2e/a11y-lang-picker-option.spec.ts`, run by the `a11y` gate.
//
// WHY DERIVED AND NOT LISTED. This guard used to name its files. A list is a
// promise about the tree that nothing checks: when the switcher was fixed, the
// commit said "one map for every render site" and `/profile`'s `<option>`s
// shipped with no `lang` at all — 3 of 5 options resolving to the document's
// "en" — while this file stayed green, because the page was not on the list.
// A render site outside the list is a site the rule cannot see; deriving the
// list is the only shape that closes the class rather than one instance of it.
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
import { readFileSync, readdirSync } from "node:fs";
import { join, relative, sep } from "node:path";
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

/**
 * `LANG_SHORT[l]` / `LANG_LABEL[l]` — the maps, as RENDERED.
 *
 * Only the indexing shape matches: the definitions are `export const LANG_SHORT:
 * Record<Lang, string> = {` (a `:` where an index would be) and an import is a
 * bare identifier, so neither counts as a render site. Kept non-global; callers
 * build their own global copy, so no `lastIndex` state can leak between them.
 */
const ENDONYM_USE = /LANG_(SHORT|LABEL)\[\s*([A-Za-z_$][\w$]*)\s*\]/;

/** `web/src`, derived from this file's own location — never a hard-coded root. */
const SRC_ROOT = fileURLToPath(new URL("../", import.meta.url));

const toPosix = (p: string) => p.split(sep).join("/");

/**
 * Every file under `web/src` that renders one of the endonym maps, with its
 * comments already stripped.
 *
 * DERIVED, not listed — see "WHY DERIVED AND NOT LISTED" above. A file that is
 * not in the returned set renders no endonym, so there is nothing for the rule
 * below to miss; a call site added anywhere under `web/src` is in scope the
 * moment it exists, with no list to remember to extend.
 *
 * Test files are skipped: they talk ABOUT the maps (`/LANG_SHORT\[l\]/`) rather
 * than rendering them, and folding this file into its own scan would let the
 * regex that implements a rule satisfy that rule.
 */
function derivedRenderSites(root: string): { file: string; src: string }[] {
  const found: { file: string; src: string }[] = [];
  const walk = (dir: string) => {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      const full = join(dir, entry.name);
      if (entry.isDirectory()) {
        if (entry.name === "node_modules" || entry.name.startsWith(".")) continue;
        walk(full);
        continue;
      }
      if (!/\.(ts|tsx)$/.test(entry.name) || /\.test\.(ts|tsx)$/.test(entry.name)) continue;
      const src = stripComments(readFileSync(full, "utf8"));
      if (ENDONYM_USE.test(src)) found.push({ file: toPosix(relative(root, full)), src });
    }
  };
  walk(root);
  return found.sort((a, b) => a.file.localeCompare(b.file));
}

/**
 * Every place `src` renders an endonym, as the opening tag of the element that
 * HOLDS it.
 *
 * Anchored on the label expression and scanned BACKWARDS to the `<` that opens
 * the holding element — not on a `<tag[^>]*>` regex, because `=>` inside an
 * attribute contains the `>` such a scan stops at and the captured "opening tag"
 * would be truncated before the spread it was supposed to inspect.
 *
 * The element is the unit, not the file: an `<option>` holds text only, so its
 * content model admits no wrapper and an ancestor's `lang` is the document's.
 */
export function endonymSites(
  src: string,
  file: string,
): { file: string; expr: string; arg: string; holds: string }[] {
  const sites: { file: string; expr: string; arg: string; holds: string }[] = [];
  for (const m of src.matchAll(new RegExp(ENDONYM_USE.source, "g"))) {
    const at = m.index ?? 0;
    const open = src.lastIndexOf("<", at);
    assert.notEqual(open, -1, `${file}: ${m[0]} is rendered outside any element — nothing can declare its language`);
    const holds = src.slice(open, at);
    assert.match(
      holds,
      /^<[A-Za-z]/,
      `${file}: could not find the element that holds ${m[0]} — the scan backwards from the label reached source that is not an element`,
    );
    sites.push({ file, expr: m[0], arg: m[2], holds });
  }
  return sites;
}

/** The spread that declares `arg`'s own language, as source. */
function declFor(arg: string): RegExp {
  return new RegExp(`\\{\\s*\\.\\.\\.\\s*endonymLangProps\\(\\s*${arg.replace(/\$/g, "\\$&")}\\s*\\)\\s*\\}`);
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

test("every file under web/src that renders an endonym spreads its declaration (derived)", () => {
  const files = derivedRenderSites(SRC_ROOT);
  assert.ok(
    files.length > 0,
    "the walk over web/src found no file rendering LANG_SHORT / LANG_LABEL — the derivation is broken, and every rule below it would pass over an empty list",
  );

  const sites = files.flatMap((f) => endonymSites(f.src, f.file));

  // Non-vacuity beyond "the list is not empty": an endonym can only be rendered
  // into text, and the release covers the two elements that hold it — a switcher
  // `<button>` and a picker `<option>`. Losing either surface (or the walk losing
  // the tree) fails here instead of passing over a shorter list.
  assert.ok(
    sites.some((s) => s.holds.startsWith("<button")),
    `no <button> in web/src renders an endonym any more — the switcher this guard was written for is gone, so a green run below would prove nothing: ${JSON.stringify(files.map((f) => f.file))}`,
  );
  assert.ok(
    sites.some((s) => s.holds.startsWith("<option")),
    `no <option> in web/src renders an endonym any more — the /profile / /settings pickers are the sites this card measured undeclared, so a green run below would prove nothing: ${JSON.stringify(files.map((f) => f.file))}`,
  );

  for (const site of sites) {
    assert.match(
      site.holds,
      declFor(site.arg),
      `${site.file}: the element holding ${site.expr} has no \`{...endonymLangProps(${site.arg})}\` in its opening tag — ` +
        `on an English page 中文 / தமிழ் / العربية would be text in another language with nothing declaring it ` +
        `(WCAG 3.1.2), which is exactly the shape measured on the shipped pickers. ` +
        `Element: ${site.holds.replace(/\s+/g, " ").slice(0, 120)}`,
    );
  }

  // A private copy of an endonym is how /mcp stayed undeclared after the shared
  // components were fixed: the literals moved, the fix did not. Every file that
  // RENDERS a map must take its label from one — except the file that DEFINES
  // them, which necessarily contains them.
  const definers = files.filter((f) => /export const LANG_(SHORT|LABEL)\s*:/.test(f.src)).map((f) => f.file);
  assert.ok(
    definers.length > 0,
    `no file under web/src defines LANG_SHORT / LANG_LABEL any more — the exemption below would then apply to nobody and the literal rule would be unchecked: ${JSON.stringify(files.map((f) => f.file))}`,
  );
  for (const f of files) {
    if (definers.includes(f.file)) continue;
    for (const endonym of ["中文", "தமிழ்", "عربي", "العربية"]) {
      assert.ok(
        !f.src.includes(endonym),
        `${f.file} carries its own copy of the endonym "${endonym}" — it must render LANG_SHORT / LANG_LABEL so the ` +
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

  // The SAME self-check for the derived <option> rule, which reads its element by
  // slicing back from `{LANG_LABEL[l]}` — a decoy whose declaration sits in a
  // comment inside the option must not satisfy it either.
  const optionDecoy = [
    "<option key={l} value={l}>",
    "  // {...endonymLangProps(l)}",
    "  {LANG_LABEL[l]}",
    "</option>",
  ].join("\n");
  const decl = declFor("l");
  assert.match(optionDecoy, decl, "the option decoy is wrong: its raw text should satisfy the rule");
  const decoySites = endonymSites(stripComments(optionDecoy), "decoy");
  assert.equal(decoySites.length, 1, "the option site scan did not find the decoy's one site");
  assert.doesNotMatch(
    decoySites[0].holds,
    decl,
    "a commented-out declaration must not satisfy the derived pin",
  );
});
