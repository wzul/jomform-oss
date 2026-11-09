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
//   2. THE RENDER SITES, source: `components/Endonym.tsx` puts the declaration on
//      the element that holds the text, and no /docs page renders a non-Latin
//      endonym without declaring it.
//
//      WHAT THE PROSE REWRITE CHANGED, stated rather than hidden: a converted
//      page's Section body is a STRING rendered by <DocsPage/>, so an `<Endonym>`
//      call site cannot live in the prose at all — the two guides this card fixed
//      now name the supported languages in ENGLISH ("English, Bahasa Melayu,
//      Chinese, Tamil and Arabic"). The literals are gone from every page, so the
//      pins below moved to the requirement that still holds and still bites: any
//      page whose code DOES render a non-Latin endonym must declare it. That rule
//      is driven on synthetic pages, so it cannot pass because the scan looks at
//      nothing. React cannot be rendered by this repo's unit runner (no
//      vitest/jest — `node --test` over `src/lib/*.test.ts`), so this leg is a
//      source pin; the EXECUTED proof that the SERVED DOM carries the attribute is
//      the sweep in `e2e/a11y-docs-lang.spec.ts`, run by the `a11y` gate.
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
import { readFileSync, readdirSync, statSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { join } from "node:path";

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

/** Every /docs page source, keyed by its path relative to app/docs. */
function docsPages(): Map<string, string> {
  const base = fileURLToPath(new URL("../app/docs/", import.meta.url));
  const out = new Map<string, string>();
  const walk = (dir: string) => {
    for (const name of readdirSync(join(base, dir))) {
      const rel = dir ? `${dir}/${name}` : name;
      if (statSync(join(base, rel)).isDirectory()) walk(rel);
      else if (name === "page.tsx") out.set(rel, readFileSync(join(base, rel), "utf8"));
    }
  };
  walk("");
  return out;
}

/**
 * A page whose CODE renders a non-Latin endonym without declaring it.
 *
 * Read from the comment-stripped source: a name in a string still renders, and a
 * `<Endonym>` call site inside a comment declares nothing. A page that renders the
 * shared map's value (`ENDONYM_LABEL.zh`) is caught too — it is the same text
 * without the declaration. The accepted declarations are the component, a spread
 * of `endonymLangProps(…)`, and a hand-written `lang="<tag>"`; the component's own
 * test below pins that the FIRST of those puts the attribute on the element that
 * holds the text.
 */
function undeclaredEndonyms(file: string, raw: string): string[] {
  const code = stripComments(raw);
  const out: string[] = [];
  for (const l of LANGS as readonly Lang[]) {
    const tag = ENDONYM_LANG[l];
    if (tag === null) continue;
    const name = ENDONYM_LABEL[l];
    const renders = code.includes(name) || code.includes(`ENDONYM_LABEL.${l}`);
    if (!renders) continue;
    const declared =
      new RegExp(`<Endonym\\b[^>]*\\bl="${l}"`).test(code) ||
      code.includes(`endonymLangProps(${JSON.stringify(l)})`) ||
      new RegExp(`\\blang="${tag}"`).test(code) ||
      // The prose renderer's marker: `[[zh|中文]]` becomes <span lang="zh">, and the
      // renderer is asserted to emit it (docsProseRender.test.ts).
      new RegExp(`\\[\\[${tag}\\|${name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}\\]\\]`).test(code);
    if (!declared) {
      out.push(`${file}: renders ${JSON.stringify(name)} with nothing declaring lang="${tag}"`);
    }
  }
  return out;
}

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

/**
 * A non-Latin endonym that a page carries WITHOUT declaring its language.
 *
 * The prose renderer learned a marker for exactly this: `[[zh|中文]]` renders
 * `<span lang="zh">中文</span>`, so a converted page CAN carry the real name and
 * the WCAG 3.1.2 declaration together. That makes the rule sharper than the
 * absence it used to be: the literal is allowed, an undeclared literal is not —
 * counted, so a page that writes 中文 twice and declares it once is reported.
 */
function undeclaredEndonymLiterals(src: string): string[] {
  const out: string[] = [];
  for (const l of LANGS as readonly Lang[]) {
    const tag = ENDONYM_LANG[l];
    if (tag === null) continue;
    const name = ENDONYM_LABEL[l];
    const total = src.split(name).length - 1;
    if (total === 0) continue;
    const declared = src.split(`[[${tag}|${name}]]`).length - 1;
    if (total > declared) out.push(`${l} (${JSON.stringify(name)}) x${total - declared}`);
  }
  return out;
}

test("every non-Latin endonym a docs page carries declares its own language", () => {
  // The shape that shipped: the names as text in JSX, unreachable by any fix at
  // the switcher or the picker. The two guides this card measured are now strings
  // in the shared prose renderer, so the fix moved into the grammar
  // (`[[lang|name]]` → `<span lang>`) and the E2E sweep in a11y-docs-lang.spec.ts
  // is what proves the SERVED DOM carries it. This pin is the source half: no page
  // may carry the name without the marker, on ANY docs page, not just the two.
  const pages = docsPages();
  assert.ok(pages.size >= 10, `walked only ${pages.size} docs page(s) — the walk is broken`);
  const bare = [...pages].flatMap(([file, src]) =>
    undeclaredEndonymLiterals(src).map((x) => `${file}: ${x}`),
  );
  assert.deepEqual(
    bare,
    [],
    `these docs pages carry a non-Latin endonym with nothing declaring its language:\n  ${bare.join("\n  ")}\n` +
      "Write it as [[<lang>|name]] so the renderer emits <span lang=…>, or render it through " +
      '<Endonym l="…" /> if the page is still hand-written JSX (WCAG 3.1.2)',
  );

  // The rule bites, and it does not over-bite: the same name without the marker is
  // reported, with the marker it is not, and a page that declares one of two
  // occurrences is still reported.
  assert.deepEqual(undeclaredEndonymLiterals('const b = ["It speaks 中文."];'), ['zh ("中文") x1']);
  assert.deepEqual(undeclaredEndonymLiterals('const b = ["It speaks [[zh|中文]]."];'), []);
  assert.deepEqual(
    undeclaredEndonymLiterals('const b = ["中文 and 中文", "[[zh|中文]]"];'),
    // Three occurrences, one of them inside the marker: two are undeclared.
    ['zh ("中文") x2'],
  );

  // Non-vacuity in the other direction: the two guides must still NAME every
  // supported language, either by its English name or by a declared marker — a page
  // that dropped the sentence would satisfy the rule by saying nothing at all.
  for (const [file, src] of [
    [CHAT_PAGE, chatRaw],
    [CUSTOMIZE_PAGE, customizeRaw],
  ] as const) {
    const named: [string, string][] = [
      ["English", "en"],
      ["Bahasa Melayu", "ms"],
      ["Chinese", "zh"],
      ["Tamil", "ta"],
      ["Arabic", "ar"],
    ];
    for (const [english, l] of named) {
      const declares = src.includes(`[[${ENDONYM_LANG[l as Lang]}\|`);
      assert.ok(
        src.includes(english) || declares,
        `${file} no longer names ${english} among the supported languages — the guide still has ` +
          "to tell a reader which languages it supports",
      );
    }
  }
});

test("a page that renders a non-Latin script still declares it", () => {
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

  // THE REQUIREMENT, WHERE IT CAN HOLD. A converted page's prose is a string, so
  // `<Endonym>` cannot be placed in it; the rewrite answered that by naming the
  // languages in English, which removes the literal (asserted above) rather than
  // the requirement. So the rule over the tree is conditional and enforced for
  // every page: IF a page's code renders a non-Latin endonym, that page must also
  // declare the language. This bites the day a hand-written page — or a renderer
  // that learns a marker — puts 中文 back without a `lang`.
  const undeclared = [...docsPages()].flatMap(([file, src]) => undeclaredEndonyms(file, src));
  assert.deepEqual(
    undeclared,
    [],
    `these docs pages render a non-Latin language name with nothing declaring its language ` +
      `(WCAG 3.1.2):\n  ${undeclared.join("\n  ")}\n` +
      "Render the name through <Endonym l=\"…\" />, spread endonymLangProps(…), or put the text " +
      "in an element carrying its own lang.",
  );

  // Non-vacuity, in three directions, so "no page is undeclared" cannot be the
  // answer of a scan that reads nothing: the detector reports a literal with no
  // declaration, accepts the same literal beside a real call site, and is not
  // fooled by a call site that is only a comment (the same rule the self-check at
  // the bottom drives).
  const decoyUndeclared = 'export default function P() { return <p>{["It speaks 中文."]}</p>; }';
  assert.equal(
    undeclaredEndonyms("decoy.tsx", decoyUndeclared).length,
    1,
    "the endonym scan did not report a page that renders 中文 with no declaration — the rule " +
      "is blind, and the tree-wide absences above prove nothing",
  );
  const decoyDeclared =
    'export default function P() { return <p><Endonym l="zh" /> 中文</p>; }';
  assert.deepEqual(
    undeclaredEndonyms("decoy.tsx", decoyDeclared),
    [],
    "the endonym scan rejected the declaring element — a correct page would be reported",
  );
  const decoyCommented =
    'export default function P() { return <p>{["It speaks 中文."]} /* <Endonym l="zh" /> */</p>; }';
  assert.equal(
    undeclaredEndonyms("decoy.tsx", decoyCommented).length,
    1,
    "a commented-out <Endonym> satisfied the declaration rule — a comment declares nothing",
  );
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
