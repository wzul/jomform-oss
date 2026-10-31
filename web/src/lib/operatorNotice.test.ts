// Guard for the platform's own operator identity — who runs JomForm.
//
// THE GAP: JomForm never named its operator anywhere. The landing/home footer
// said only "JomForm — Made in Malaysia.", and /legal/privacy and /legal/terms
// named nobody at all — a privacy policy with no data controller on it. The
// company (WANZUL HOSTING ENTERPRISE, registration 201403298654, formerly
// PC0012291-H) is now stated on all three surfaces.
//
// WHY THIS IS A SOURCE SCAN
//   Three surfaces must show the same legal sentence, and the way this breaks is
//   not "the feature vanished" — it is "someone edited one surface". There is no
//   JSX/DOM runner in this repo (no vitest/jest — see src/lib/i18n.test.ts,
//   src/lib/homeServerContent.test.ts), so the surface rules are asserted
//   against source and the data rules against the modules they render from.
//   Rendered-text evidence (including the RTL claim, which no source scan can
//   make) is in the PR.
//
// WHAT IS PINNED, AND WHY EACH ONE MATTERS
//   1. All three surfaces still render the shared OperatorNotice component. A
//      per-surface literal is what this file exists to prevent: the card's
//      requirement is one definition, so a change to the number cannot leave
//      one page stale. Removing the block from ANY ONE surface fails here.
//   2. The legal identifiers exist in exactly ONE non-test module
//      (src/lib/operator.ts). A copy of the number in a page, a dictionary or a
//      second component fails, because that copy is a definition that no longer
//      tracks the others.
//   3. The sentence lives once, in the `land.operator` dictionary entry, and
//      every surface reads it through the shared component — no surface carries
//      the sentence as a literal.
//   4. Each of the five locales is real: non-empty, different from English (a
//      non-English locale left English-only is the failure the card names), and
//      carrying the legal name and BOTH numbers verbatim and in the documented
//      order (current first, former in parentheses).
//   5. operatorParts() splits at the current number, so the registration clause
//      is the part the component isolates for RTL; it reassembles exactly, and
//      a locale that lost the number degrades instead of throwing.
//   6. The component wraps that clause in <bdi dir="ltr">. Measured in
//      Chromium: under the app's dir="rtl" for Arabic, the raw string renders
//      the parenthesis pair INVERTED — `. (PC0012291-H` — and strands the
//      sentence-final period at the start of the visual line. This is why the
//      isolation is here and why a "tidy-up" that drops it is a regression.
//   7. The merchant-facing public form footer is a DIFFERENT thing (a merchant
//      showing their own company, issue #183). The platform identity must not
//      leak into that render path.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { DICT } from "../components/i18nDict.ts";
import {
  LOCALE_BASE_DIR,
  OPERATOR_FORMER_REG_NO,
  OPERATOR_KEY,
  OPERATOR_LEGAL_NAME,
  OPERATOR_LOCALES,
  OPERATOR_REG_NO,
  operatorDirProps,
  operatorParts,
} from "./operator.ts";
import { SITE_NAME } from "./siteName.ts";

const HERE = new URL(".", import.meta.url);
const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, HERE)), "utf8");

/** Every source file under web/src, as paths relative to src/. */
function srcSources(dir = fileURLToPath(new URL("..", HERE))): string[] {
  const base = dir.replace(/\/+$/, "");
  return readdirSync(base).flatMap((name) => {
    const full = `${base}/${name}`;
    if (statSync(full).isDirectory()) return srcSources(full);
    return /\.tsx?$/.test(name) ? [full] : [];
  });
}

// The three surfaces that must name the operator. The landing page is also the
// logged-out home page (app/page.tsx renders <Landing />), so it is listed once.
// `metadata` is false for the landing page: it is a client component in the
// dashboard shell, NOT a route with its own Metadata export.
const SURFACES: { name: string; file: string; metadata: boolean }[] = [
  { name: "landing/home footer", file: "../app/landing/page.tsx", metadata: false },
  { name: "privacy policy", file: "../app/legal/privacy/page.tsx", metadata: true },
  { name: "terms of service", file: "../app/legal/terms/page.tsx", metadata: true },
];

const COMPONENT = "../components/OperatorNotice.tsx";
const DICT_MODULE = "../components/i18nDict.ts";

/** The module allowed to hold the identifiers: the one definition. */
const OPERATOR_MODULE = "/lib/operator.ts";

/** Modules allowed to CONTAIN the identifiers as text without defining them. */
const IDENTITY_ALLOWED = [OPERATOR_MODULE, "/components/i18nDict.ts", "/components/OperatorNotice.tsx"];

/** A per-surface literal of the identity — the drift this guard exists to stop. */
const IDENTITY_LITERAL = new RegExp(
  `${OPERATOR_REG_NO}|${OPERATOR_FORMER_REG_NO}|${OPERATOR_LEGAL_NAME}`,
);

test("the guard is looking at real files (it cannot pass vacuously)", () => {
  // If a path moves, fail here rather than asserting against nothing (the
  // failure mode #507 was filed about).
  for (const s of SURFACES) {
    const src = read(s.file);
    assert.ok(src.includes("export default function"), `${s.file} is not the ${s.name} page`);
    if (s.metadata) assert.ok(src.includes("export const metadata"), `${s.file} is not a Next page`);
  }
  const all = srcSources();
  assert.ok(all.length > 40, `the src/ scan found implausibly few sources (${all.length})`);
  assert.ok(all.some((p) => p.endsWith(OPERATOR_MODULE)), "src/lib/operator.ts is gone");
  assert.ok(all.some((p) => p.endsWith("/components/OperatorNotice.tsx")), "the component is gone");
});

test("the identifiers exist in exactly one non-test module that DEFINES them", () => {
  // One definition, not three: a copy of the number in a page or a second
  // component is a definition that stops tracking the others. The dictionary
  // and the component are allowed to CONTAIN the sentence — they render it —
  // but they must not spell the identity out as a second source: the dictionary
  // strings are asserted against the module's constants in the locale test below.
  const offenders = srcSources()
    .filter((p) => !/\.test\.tsx?$/.test(p))
    .filter((p) => !IDENTITY_ALLOWED.some((allowed) => p.endsWith(allowed)))
    .filter((p) => IDENTITY_LITERAL.test(readFileSync(p, "utf8")));
  assert.deepEqual(
    offenders,
    [],
    "the operator's legal identifiers must be defined only in src/lib/operator.ts — render them through " +
      "components/OperatorNotice.tsx instead of copying them into these files",
  );
});

test("every one of the three surfaces renders the shared operator block", () => {
  // The bite requirement: dropping the block from ANY ONE surface fails here.
  const missing: string[] = [];
  for (const s of SURFACES) {
    const src = read(s.file);
    if (!/<OperatorNotice\b/.test(src)) missing.push(`${s.name} (${s.file})`);
    // And it must come from the shared component, not a look-alike.
    else if (!/import\s+OperatorNotice\s+from\s+["'][^"']*components\/OperatorNotice["']/.test(src)) {
      missing.push(`${s.name}: OperatorNotice is not imported from components/OperatorNotice`);
    }
  }
  assert.deepEqual(
    missing,
    [],
    `these surfaces no longer render the operator identity — JomForm must name who runs it everywhere ` +
      `it has a footer:\n  ${missing.join("\n  ")}`,
  );
});

test("the landing footer block is inside the <footer>, not somewhere unrelated", () => {
  // A surface check on the word alone would pass on a block parked in a hidden
  // div; the home footer is where the identity is legible, so anchor it.
  const landmark = read("../app/landing/page.tsx");
  const at = landmark.indexOf("<OperatorNotice");
  const footer = landmark.indexOf("<footer");
  assert.ok(footer > 0 && at > footer, "the operator block must sit inside the landing's <footer>");
  assert.ok(
    landmark.indexOf("</footer>") > at,
    "the operator block escaped the landing's <footer>",
  );
});

test("both legal pages state it in their own footer, below the policy text", () => {
  for (const s of SURFACES.slice(1)) {
    const src = read(s.file);
    const at = src.indexOf("<OperatorNotice");
    const lastSection = src.lastIndexOf("</section>");
    assert.ok(
      at > lastSection,
      `${s.name}: the operator block must follow the policy sections (it is the controller statement, ` +
        `not another clause)`,
    );
  }
});

test("each surface reads the sentence from the dictionary, not from a literal", () => {
  // The sentence is a translated string; a literal in a page could not be
  // localized, and would silently be the English one for every locale.
  assert.equal(OPERATOR_KEY, "land.operator");
  for (const s of SURFACES) {
    assert.doesNotMatch(
      read(s.file),
      /is operated by|dikendalikan oleh|由 WANZUL|ஆல் இயக்கப்படுகிறது|يُدار/,
      `${s.name} carries its own copy of the operator sentence instead of rendering the shared block`,
    );
  }
  assert.match(
    read(COMPONENT),
    /t\(OPERATOR_KEY\)/,
    "the component no longer reads the operator sentence from the dictionary",
  );
  assert.match(read(DICT_MODULE), /"land\.operator":\s*\{/, "the land.operator entry is gone");
});

test("the operator sentence is real in all five locales and none is English-only", () => {
  const entry = DICT[OPERATOR_KEY] as Record<string, string>;
  const problems: string[] = [];

  for (const lang of OPERATOR_LOCALES) {
    const v = entry[lang];
    if (typeof v !== "string" || v.trim() === "") {
      problems.push(`${lang}: missing/empty`);
      continue;
    }
    // A non-English locale that still reads English is the card's named failure.
    if (lang !== "en" && v === entry.en) problems.push(`${lang}: identical to the English string`);
    // The legal identifiers are not translatable.
    for (const id of [OPERATOR_LEGAL_NAME, OPERATOR_REG_NO, OPERATOR_FORMER_REG_NO]) {
      if (!v.includes(id)) problems.push(`${lang}: does not contain ${id} verbatim`);
    }
    // Legibility: the current number first, the former number parenthesized.
    const cur = v.indexOf(OPERATOR_REG_NO);
    const former = v.indexOf(OPERATOR_FORMER_REG_NO);
    if (cur > former) problems.push(`${lang}: the former number appears before the current one`);
    if (!/[（(][^()（）]*PC0012291-H/.test(v)) {
      problems.push(`${lang}: the former number is not written in parentheses after the current one`);
    }
    if (!v.includes(SITE_NAME)) problems.push(`${lang}: does not name ${SITE_NAME}`);
  }

  assert.deepEqual(problems, [], `the operator sentence is incomplete:\n  ${problems.join("\n  ")}`);
});

test("every locale's sentence splits at the registration number", () => {
  for (const lang of OPERATOR_LOCALES) {
    const text = DICT[OPERATOR_KEY][lang];
    const { lead, regClause } = operatorParts(text);
    assert.notEqual(regClause, "", `${lang}: operatorParts could not find the registration number`);
    // The split is a split, not a rewrite: it reassembles to the original. The
    // separator between the halves is spelled out because the component renders
    // that space itself (see OperatorNotice) — it is dropped from `lead`, not
    // from the sentence.
    assert.equal(
      `${lead.trimEnd()} ${regClause}`,
      text,
      `${lang}: operatorParts lost text (lead + " " + clause must be the original string)`,
    );
    assert.ok(regClause.startsWith(OPERATOR_REG_NO), `${lang}: the clause must start at the number`);
    assert.ok(
      regClause.includes(OPERATOR_FORMER_REG_NO),
      `${lang}: the former number fell outside the clause that gets isolated for RTL`,
    );
    assert.ok(lead.trim() !== "", `${lang}: the sentence would render as the number alone`);
  }
});

test("operatorParts degrades instead of throwing when the number is gone", () => {
  // The component runs on a server-rendered page: a throw takes the page down,
  // so a drifted string must render (un-isolated) and let the test above fail.
  const noNumber = "JomForm is operated by WANZUL HOSTING ENTERPRISE.";
  assert.deepEqual(operatorParts(noNumber), { lead: noNumber, regClause: "" });
});

test("the registration clause is isolated for RTL in the shared component", () => {
  const component = read(COMPONENT);
  // Measured in Chromium under dir="rtl": without this, the Arabic sentence
  // renders its parenthesis pair inverted and strands the final period.
  assert.match(
    component,
    /<bdi dir="ltr">\{regClause\}<\/bdi>/,
    "the registration clause must be wrapped in <bdi dir=\"ltr\"> — under the app's dir=\"rtl\" for " +
      "Arabic the raw clause renders its parentheses inverted, and the sentence-final period lands " +
      "at the start of the visual line (measured, not assumed)",
  );
  assert.match(component, /lead\.trim\(\)/, "the lead must be trimmed or a locale doubles the gap in RTL");
  // The locale is per-visitor (jf_lang), so the block must read the shared
  // context: a `lang` prop could only ever be the caller's compile-time default,
  // and the two legal pages are static server components — they would print the
  // English sentence to every visitor, in every locale, forever.
  assert.match(component, /"use client";/, "the operator block must be a client component");
  assert.match(
    component,
    /const \{ lang, t \} = useI18n\(\)/,
    "the operator block must take the visitor's locale from useI18n(), not from a prop — the `lang` " +
      "it declares on the sentence is that same resolved locale (card t_b13ee0cc)",
  );
  assert.match(component, /t\(OPERATOR_KEY\)/, "the component no longer translates the sentence");
  // A `lang` PROP is the shape this must never be: it can only ever be the
  // caller's compile-time default, and the two legal pages are static server
  // components with nothing to supply one. The component declares the language
  // of its own passage from the CONTEXT instead (card t_b13ee0cc), so the check
  // is anchored on the props type and on the call sites — pinning the bare token
  // `lang` would now redden on the fix itself.
  assert.doesNotMatch(
    component,
    /lang\?:\s*(Lang|string)/,
    "the component takes a lang prop again — that pins it to the server default and leaves four " +
      "locales English-only on the legal pages",
  );
  assert.doesNotMatch(
    component,
    /\{\s*className\s*,\s*lang\s*\}/,
    "the component destructures a lang prop again",
  );
  for (const s of SURFACES) {
    assert.doesNotMatch(
      read(s.file),
      /<OperatorNotice[^>]*\slang=/,
      `${s.name} passes a lang prop to the operator block — the language of a passage is the locale ` +
        `the shared component resolves, never the caller's default`,
    );
  }
  for (const s of SURFACES.slice(1)) {
    const src = read(s.file);
    assert.doesNotMatch(
      src,
      /"use client"/,
      `${s.name}: the legal page became a client component — it must stay a static server page`,
    );
  }
});

test("the operator block is not the merchant's own company footer", () => {
  // Issue #183's footer is a DIFFERENT feature: the per-workspace company name /
  // registration a merchant shows on their own public form (api/internal/httpapi
  // public.go, editable at /settings). The platform identity must not be wired
  // into it — Wan scoped the public form footer out of this change.
  const go = readFileSync(
    fileURLToPath(new URL("../../../api/internal/httpapi/public.go", HERE)),
    "utf8",
  );
  assert.doesNotMatch(
    go,
    IDENTITY_LITERAL,
    "the platform's operator identity leaked into the merchant public-form footer — that footer shows " +
      "the MERCHANT's company (issue #183) and is not where JomForm identifies itself",
  );
  const rest = readFileSync(
    fileURLToPath(new URL("../../../api/internal/httpapi/rest_workspaces.go", HERE)),
    "utf8",
  );
  assert.doesNotMatch(
    rest,
    IDENTITY_LITERAL,
    "the platform's operator identity was folded into the per-workspace company_reg setting",
  );
});

// ══ the operator sentence declares its own language (card t_b13ee0cc) ══════
//
// THE DEFECT, measured on a local production build of main with `jf_lang=ar`
// (and filed from card t_0e2d71f4, which found it and could not fix it there):
// /legal/privacy and /legal/terms declare `lang="en"` — PR 1201 pins their prose
// to English, correctly — and their footer then renders THIS component's
// sentence in the visitor's locale. With `jf_lang=ar` that is a full Arabic
// sentence inside an English-declared document with nothing declaring its
// language: 2 elements per page (the <p> and the <bdi> inside it), nearest
// `lang="en"`. WCAG 3.1.2 (Language of Parts); a screen reader reads
// "يُدار JomForm …" with an English voice.
//
// WHY THIS IS INVISIBLE TO A PRE-HYDRATION READING: both legal pages are static
// server components, so the server render is the English sentence and the
// provider swaps it on mount. That is how the premise in t_0e2d71f4's body
// ("/legal/privacy, /legal/terms: 0 Arabic elements — nothing to mark") was
// formed; the DOM leg in `web/e2e/a11y-docs-lang.spec.ts` waits for the ARABIC
// sentence instead of for the page.
//
// axe has no rule for this class, so the gate cannot see it. The executed proof
// is the DOM leg; this leg pins the SOURCE so a revert fails `npm run test:unit`
// without a browser.
//
// READ WITH COMMENTS STRIPPED, like the /docs chrome guard in docsA11y.test.ts:
// the component's header names `lang` and the criterion in prose, and a rule a
// comment can satisfy is the shape this repo's guards already refuse.
const COMPONENT_SRC = read(COMPONENT);

const stripJsComments = (src: string): string =>
  src.replace(/\/\*[\s\S]*?\*\//g, " ").replace(/(^|[^:])\/\/.*$/gm, "$1");

const NOTICE_AT = COMPONENT_SRC.indexOf("export default function OperatorNotice(");
const NOTICE = NOTICE_AT === -1 ? "" : stripJsComments(COMPONENT_SRC.slice(NOTICE_AT));

test("the sentence-language guard is reading the component's render (it cannot pass vacuously)", () => {
  assert.notEqual(NOTICE_AT, -1, `${COMPONENT} no longer declares OperatorNotice — this guard would prove nothing`);
  assert.ok(NOTICE.includes("useI18n"), `the slice is not the component: ${JSON.stringify(NOTICE.slice(0, 80))}`);
  assert.ok(NOTICE.includes("return ("), "the slice holds no render, so the rules below could pass without reading it");
  assert.ok(NOTICE.includes("t(OPERATOR_KEY)"), "the slice no longer looks the sentence up in the dictionary");
  // The component must still emit the <p> that carries the passage: a fix that
  // dropped the element would leave nothing to declare a language ON.
  assert.match(NOTICE, /<p\b/, "the component no longer renders a <p> for the sentence");
});

test("the operator sentence declares the language it is written in (WCAG 3.1.2)", () => {
  // The passage is carried by an element whose `lang` is BOUND to a value, not a
  // literal: a literal would freeze one language onto all five locales. Anchored
  // on the `<p>` and on the ATTRIBUTE (whitespace-delimited), not on the token —
  // `data-…-lang={…}` is not a declaration.
  assert.match(
    NOTICE,
    /<p\b[^>]*\slang=\{/,
    "the operator sentence renders with no `lang` of its own — inside the English-declared /legal pages " +
      "a sentence in the visitor's locale is a language change nothing announces (WCAG 3.1.2; measured " +
      "with jf_lang=ar: the <p> and its <bdi> read as English, 2 elements per page)",
  );
  assert.doesNotMatch(
    NOTICE,
    /lang="(en|ms|zh|ta|ar)"/,
    "the component hard-codes a language — the sentence's language is the RESOLVED locale, so a " +
      "constant would declare an Arabic sentence Chinese (or the reverse) for four of the five locales",
  );
  // English is the document's own language on the legal pages and on the landing
  // page (whose document declaration follows the visitor), so an English
  // sentence must add nothing at all.
  assert.match(
    NOTICE,
    /===?\s*"en"/,
    "the component no longer treats English differently — every surface would then carry a lang " +
      "attribute over text that is already the document's language",
  );
});

test("the sentence-language rules cannot be satisfied by a comment", () => {
  // Non-vacuity for the strip, and self-contained: the component's own header
  // names the criterion, so if the strip were a no-op the rules above could be
  // satisfied by PROSE. This fails only if that stops being true — never merely
  // because the fix is gone, which the test above reports.
  assert.match(
    COMPONENT_SRC,
    /WCAG 3\.1\.2/,
    "the component's header no longer names the criterion the strip exists to remove — re-point this " +
      "check at whatever comment the rules above could be satisfied by",
  );
  assert.doesNotMatch(
    stripJsComments(COMPONENT_SRC),
    /WCAG 3\.1\.2/,
    "a comment survived the strip, so the rules above could be satisfied by prose instead of code",
  );
});

// ══ the sentence is LAID OUT in its own direction (card t_d2f2e21f) ═════════
//
// THE DEFECT, measured on prod v1.21.332 (revision a3661d1e) with `jf_lang=ar`
// at 1440x900, per-word client rects on /legal/privacy: the <p> declared
// `lang="ar"` but carried no `dir`, and the legal pages keep `dir="ltr"` on
// <html> (their prose is English, PR 1201 — correctly). So the Arabic sentence's
// runs were laid out in LOGICAL order, left to right: the leftmost run was
// "يُدار", the sentence's FIRST word, and the rightmost was the registration
// clause, its LAST. An Arabic reader scanning right-to-left met the sentence back
// to front. Same on /legal/terms; /landing read correctly only because its
// document is already `dir="rtl"`.
//
// The language declaration the block above pins says how the passage is
// PRONOUNCED. It says nothing about how its runs are LAID OUT, which is the
// separate fact this block pins — and the two are exactly the pair that
// disagreed on the legal pages.
test("operatorDirProps declares a direction for an RTL locale and nothing for an LTR one", () => {
  // Arabic is the dictionary's only RTL language, so this is the whole decision
  // table: an implementation that emits `dir="rtl"` unconditionally mirrors the
  // English sentence into an RTL paragraph on every page, and one that emits
  // nothing leaves the reported defect in place.
  assert.deepEqual(operatorDirProps("ar"), { dir: "rtl" }, "an Arabic sentence must carry dir=\"rtl\"");
  for (const lang of OPERATOR_LOCALES.filter((l) => l !== "ar")) {
    assert.deepEqual(
      operatorDirProps(lang),
      {},
      `${lang}: an LTR locale must emit NO dir — the document already declares LTR, and repeating it ` +
        `is a declaration the passage does not need (the same rule as its lang)`,
    );
  }
  // Non-vacuity, and the shape a locale addition must not slip through: every
  // locale in the dictionary has a decision, and Arabic is the only RTL one.
  assert.deepEqual(
    Object.keys(LOCALE_BASE_DIR).sort(),
    [...OPERATOR_LOCALES].sort(),
    "LOCALE_BASE_DIR covers a different set of locales than the dictionary — a locale added without a " +
      "direction decision would render its sentence in whatever base the page happens to have",
  );
  assert.deepEqual(
    Object.entries(LOCALE_BASE_DIR).filter(([, d]) => d === "rtl").map(([l]) => l),
    ["ar"],
    "Arabic is this dictionary's only right-to-left language; if that changed, the direction rule above " +
      "has to follow it",
  );
});

test("the passage spreads that direction from the resolved locale in the shared component", () => {
  // Anchored on the <p> that HOLDS the passage: the <bdi dir="ltr"> inside it is a
  // different, deliberate declaration, so the rule is scoped to the opening tag.
  const pTag = NOTICE.match(/<p\b[^>]*>/)?.[0] ?? "";
  assert.ok(pTag, `the component renders no <p> opening tag for the sentence — this rule would prove nothing`);

  assert.match(
    pTag,
    /\{\.\.\.operatorDirProps\(lang\)\}/,
    "the operator sentence renders with no base direction of its own — on the two legal pages, whose " +
      "document is dir=\"ltr\", an Arabic sentence then lays its runs out left-to-right and reads back " +
      "to front (card t_d2f2e21f; measured per-word rects, v1.21.332)",
  );
  assert.doesNotMatch(
    pTag,
    /dir="(ltr|rtl)"/,
    "the passage hard-codes a base direction — a literal dir=\"rtl\" mirrors the English sentence into " +
      "an RTL paragraph for the four LTR locales, and a literal dir=\"ltr\" is the reported defect",
  );
  // The same locale drives both declarations, so the language and its direction
  // cannot be resolved independently and drift.
  assert.equal(
    (NOTICE.match(/operatorDirProps\(lang\)/g) ?? []).length,
    1,
    "the passage must take its direction from the resolved locale exactly once",
  );
});

