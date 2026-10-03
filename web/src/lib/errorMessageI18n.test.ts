// Regression guard: an error MESSAGE a user reads must come from the dictionary,
// never from a string literal written into the component.
//
// THE BUG. The builder's image-upload pre-flight had two Malay sentences written
// straight into the handler:
//
//     setLoadErr("Jenis imej tidak disokong (png/jpeg/webp/gif sahaja).");
//     setLoadErr("Imej melebihi had 20MB.");
//
// A merchant whose dashboard language is English (or Chinese/Tamil/Arabic)
// uploaded a file the browser refuses and read Malay. Every guard in the repo
// was green while that happened, and each was green BY CONSTRUCTION:
//
//   * src/lib/i18n.test.ts covers the DICTIONARY, and its own header says it
//     "does not see strings hardcoded in components" — the dictionary is only
//     as complete as the code's willingness to use it.
//   * src/lib/apiErr.test.ts (issue #6177) covers the SERVER's prose reaching
//     the screen. A literal never touches a Response, so it is not that shape.
//
// This file pins the third, missing half: the sentence the user READS has to be
// producible only from the dictionary.
//
// WHY THE SCANNER IS AN AST AND NOT A REGEX (card t_284f2569). The first cut read
// the source with a regex and masked the literals it found. The single-quote
// branch of that regex matched ANY apostrophe pair, and this repo's TSX is full of
// apostrophes in JSX text (`your form's name`, `the buyer's card`). Two of them
// read as one opening and closing quote and formed a bogus thousand-character
// "string": the mask then replaced its interior — NEWLINES INCLUDED — and every
// real call site inside it became invisible. Measured on the tree it shipped
// against: 327 bogus spans across 51 of 82 files, 7388 of 26436 lines (28%) inside
// one; the two builder call sites this guard exists for sat inside a span of 8803
// characters, and the sales site inside a span of 4948. The class scanner reported
// ZERO offenders with both bugs on disk — green by construction, the very failure
// this file's header warns about. The reported line numbers were wrong too (that
// same masking dropped 1332 lines in sales/page.tsx, so the only message a future
// worker gets pointed ~313 lines away from the defect).
//
// `ts.createSourceFile` removes the regex, the masking and the line drift in one
// move: the parser knows where literals and comments really begin and end, and a
// node's line number comes from the source. No text is rewritten, so no line can
// shift.
//
// WHAT IS PINNED
//   1. No error-state setter is called with a bare string literal. The walk has
//      a floor and names the file the defect was reported from, so a walk that
//      silently matches nothing cannot pass vacuously.
//   2. The reported call sites are FIXED, not merely absent — asserted by
//      reading builder/page.tsx, including that the Malay sentence is gone from
//      the code.
//   3. The dictionary keys those call sites use exist and define all five
//      locales. Checks 2 + 3 together make a rename catchable: swapping the key
//      for a typo renders the bare key string to every user.
//
// WHAT IT DOES NOT COVER (stated honestly)
//   * Translation QUALITY — a fluent review is the only thing that can judge it.
//   * A hardcoded literal in a JSX text node, a `placeholder=` attribute or a
//     `title=` attribute. Those are a real, adjacent defect (three Malay sample
//     placeholders ship today) but they are a DIFFERENT channel from an error
//     message, with a different fix, so they are not folded in here.
//   * It does not execute React: this repo has no JSX/DOM test runner — node
//     --test executes TypeScript but cannot load .tsx (see the header of
//     src/lib/i18n.test.ts). A source walk is the only granularity available for
//     "which literal reaches the screen", and it is the right one here, because
//     the defect IS a literal at a call site.
//
// Run with:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { createRequire } from "node:module";
import type * as TS from "typescript";

import { DICT, LANGS } from "../components/i18nDict.ts";

// `typescript` is a dependency of this package and this file is executed from
// src/lib/, so a require rooted here resolves it. It is loaded through
// createRequire because the test runner executes this file as ESM.
const require = createRequire(new URL("./", import.meta.url));
const ts = require("typescript") as typeof TS;

const SRC = fileURLToPath(new URL("..", import.meta.url));

/** Every source file under src/, minus tests and the dictionary itself. */
function walk(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    if (name === "node_modules" || name === ".next") continue;
    const p = `${dir}/${name}`;
    if (statSync(p).isDirectory()) walk(p, out);
    else if (name.endsWith(".tsx") || name.endsWith(".ts")) out.push(p);
  }
  return out;
}

// The dictionary is the one file whose job is to hold sentences in five locales.
const sources = walk(SRC).filter(
  (p) =>
    !p.endsWith("i18nDict.ts") && !p.endsWith(".test.ts") && !p.endsWith(".test.tsx"),
);

// `fileURLToPath` drops the URL's trailing slash, so the slice leaves a leading
// one; normalise it so the ratchet's paths read like repo paths.
const rel = (p: string) => p.slice(SRC.length).replace(/^\//, "");

/**
 * The states that publish a message the user reads. This predicate is the SAME
 * one src/lib/apiErr.test.ts uses to find an error state (issue #6177), on
 * purpose: two guards for the same screen must not disagree about what an error
 * state is. Every such state in this repo already ends in Err/Error/Msg/Fail/
 * Warn/Notice/Message.
 */
const isErrorState = (s: string) => /(err|error|msg|fail|warn|notice|message)$/i.test(s);

/**
 * The sentence an argument IS, or null when it is not a baked literal.
 *
 * Accepted: a plain string, a no-substitution template, and a literal
 * concatenation of either (`"a" + "b"`, which reaches the screen whole).
 *
 * Everything else is correctly NOT this defect, and each has a name in the
 * self-test: a template with `${…}` (its text is assembled at the call), `t("k")`
 * (it IS the dictionary), a fallback expression, and `""` (the legitimate "clear
 * this message").
 *
 * A single token with no whitespace is refused by the caller: that is a CSS
 * class, a key or an identifier, not a sentence.
 */
function literalSentence(n: TS.Node | undefined): string | null {
  if (!n) return null;
  if (ts.isStringLiteral(n) || ts.isNoSubstitutionTemplateLiteral(n)) return n.text;
  if (ts.isParenthesizedExpression(n)) return literalSentence(n.expression);
  if (ts.isBinaryExpression(n) && n.operatorToken.kind === ts.SyntaxKind.PlusToken) {
    const l = literalSentence(n.left);
    const r = literalSentence(n.right);
    if (l === null || r === null) return null;
    return l + r;
  }
  return null;
}

/** Every bare-literal error message in one file, with its real source line. */
function offendersIn(
  fileName: string,
  text: string,
): { line: number; setter: string; lit: string }[] {
  const sf = ts.createSourceFile(fileName, text, ts.ScriptTarget.Latest, true);
  const out: { line: number; setter: string; lit: string }[] = [];
  const visit = (node: TS.Node): void => {
    if (
      ts.isCallExpression(node) &&
      ts.isIdentifier(node.expression) &&
      /^set/.test(node.expression.text) &&
      isErrorState(node.expression.text.slice(3)) &&
      node.arguments.length > 0
    ) {
      const lit = literalSentence(node.arguments[0]);
      if (lit !== null && lit.trim() !== "" && /\s/.test(lit.trim())) {
        // The line comes from the SOURCE the parser read, which is the whole point:
        // the previous regex scanner reported line numbers from a masked copy and
        // pointed a future worker ~313 lines away from the defect it named.
        const lc = sf.getLineAndCharacterOfPosition(node.getStart(sf));
        out.push({ line: lc.line + 1, setter: node.expression.text, lit });
      }
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);
  return out;
}

test("the source walk is non-empty, minus the dictionary itself", () => {
  assert.ok(
    sources.length > 40,
    `expected to walk the whole dashboard, got ${sources.length} source files`,
  );
  // Anchor: the file the bug was reported from must be in the walk, or the
  // guard is looking somewhere else entirely.
  assert.ok(
    sources.some((p) => p.endsWith("app/builder/page.tsx")),
    "builder/page.tsx is not in the walked set",
  );
  assert.ok(
    !sources.some((p) => p.endsWith("components/i18nDict.ts")),
    "the dictionary must stay OUT of the walk (it is where the five locales live)",
  );
});

// SITES THAT ARE THE SAME DEFECT BUT NOT THIS CARD'S JOB.
//
// The two /connect sentences are fixed on the t_a686f0e9 branch, which is
// STACKED on the verified-but-unmerged #6177 head — its fix calls apiErr(), a
// module that does not exist on main, so carrying it here would drag that whole
// branch in and make this diff unreviewable. That card owns them and deletes the
// literals.
//
// This list is a SUBSET ratchet, not a waiver:
//   * an offender that is NOT listed here fails the suite, so a new defect can
//     never hide behind an existing entry;
//   * an entry whose fix lands elsewhere simply stops matching — it is matched
//     by FILE + literal TEXT, not by line number, and a stale entry is not an
//     error on purpose: this card is based on main, the #6177 merge is not, and
//     a guard that went red the moment an unrelated branch merged would block
//     the repo for no reason.
//
// /admin is deliberately NOT listed: its four candidate sites pass a `fallback`
// argument to the file's own `serverError()` helper rather than a literal to a
// setter, so they are not this defect's shape. Their story is on card
// t_74c46aaa.
const KNOWN_SAME_CLASS: { file: string; lit: string; owner: string }[] = [
  {
    file: "app/connect/page.tsx",
    lit: "Unable to connect this account.",
    owner: "t_a686f0e9",
  },
  {
    file: "app/connect/page.tsx",
    lit: "Unable to send a verification email.",
    owner: "t_a686f0e9",
  },
];

test("the scanner sees through the shapes that fooled the regex (self-test)", () => {
  // The scanner is the guard's own instrument, so its failure modes are worth
  // pinning directly. The third case is the one the REGEX scanner got wrong — an
  // apostrophe pair in JSX text forming a bogus multi-line "string" that
  // swallowed real call sites — and the rest are the negatives that must never
  // be reported, because reporting them would make the guard red on a healthy
  // tree.
  const cases: { name: string; src: string; want: string[] }[] = [
    {
      name: "plain one-liner (the reported bug)",
      src: 'setLoadErr("Jenis imej tidak disokong.");',
      want: ["Jenis imej tidak disokong."],
    },
    {
      name: "wrapped across lines",
      src: 'setErr(\n  "Template gagal dipadam.",\n);',
      want: ["Template gagal dipadam."],
    },
    {
      name: "an apostrophe in JSX text before it (the regex's blind spot)",
      src: "const t = <>Check your form's name and the buyer's card.</>;\nsetMsg(\"Ralat besar.\");",
      want: ["Ralat besar."],
    },
    {
      name: "a comment quoting the bug",
      src: '// it used to say setLoadErr("Jenis imej tidak disokong.");\nsetLoadErr(t("bld.x"));',
      want: [],
    },
    {
      name: 'setMsg("") — the legitimate clear',
      src: 'setMsg("");',
      want: [],
    },
    {
      name: "a translated call, and one with a fallback",
      src: 'setErr(t("a.b"));\nsetErr(d.error || t("a.b"));',
      want: [],
    },
    {
      name: "a multi-line translated call",
      src: 'setLoadErr(\n  apiErr(d, t("a.b"), t),\n);',
      want: [],
    },
    {
      name: "a non-error setter (a data column, not a message)",
      src: 'setItems("a sentence in a data field");',
      want: [],
    },
    {
      name: "a template literal with interpolation is not a baked sentence",
      src: "setErr(`Capture gagal ${ref}.`);",
      want: [],
    },
    {
      name: "a literal concatenation reaches the screen whole, so it counts",
      src: 'setErr("Imej melebihi " + "had 20MB.");',
      want: ["Imej melebihi had 20MB."],
    },
  ];
  for (const c of cases) {
    const got = offendersIn("t.tsx", c.src).map((o) => o.lit);
    assert.deepEqual(got, c.want, `${c.name}: got ${JSON.stringify(got)}`);
  }
});

test("the scanner reports TRUE source lines, not lines from a rewritten copy", () => {
  // The regex scanner reported line numbers from the MASKED text, so a bogus
  // apostrophe span earlier in the file dropped 1332 lines from sales/page.tsx
  // and its message pointed a worker ~313 lines from the defect. Line numbers
  // are the guard's entire message to whoever has to fix the finding, so they
  // are pinned here rather than assumed.
  const src = ["// heading", "", "const x = 1;", "", 'setMsg("Baris sebenar.");', ""].join("\n");
  const got = offendersIn("t.tsx", src);
  assert.equal(got.length, 1);
  assert.equal(got[0].line, 5, `expected the true line 5, got ${got[0].line}`);

  // The same offender AFTER the shape that used to destroy line numbering: an
  // unpaired apostrophe in JSX text, which paired with another one and masked a
  // multi-line span — newlines included.
  const withApostrophes = [
    "const t = <>Check your form's name</>;",
    "const u = <>and the buyer's card</>;",
    "",
    'setMsg("Baris sebenar.");',
  ].join("\n");
  const got2 = offendersIn("t.tsx", withApostrophes);
  assert.equal(got2.length, 1, "the JSX apostrophes must not hide the call site");
  assert.equal(got2[0].line, 4, `expected the true line 4, got ${got2[0].line}`);
});

test("no error-state setter is handed a bare string literal", () => {
  // THE guard, aimed at the exact bug shape:
  //
  //     setLoadErr("Jenis imej tidak disokong (png/jpeg/webp/gif sahaja).");
  //
  // `t(...)` is required, because the sentence reaches a screen whose language
  // is the user's dashboard language and not the author's. Note this catches a
  // literal in ANY language, English included: an English sentence baked into a
  // component is the same defect for the other four locales.
  const offenders = sources.flatMap((p) =>
    offendersIn(p, readFileSync(p, "utf8")).map((o) => ({ file: rel(p), ...o })),
  );
  const unexpected = offenders.filter(
    (o) => !KNOWN_SAME_CLASS.some((k) => k.file === o.file && k.lit === o.lit),
  );
  assert.deepEqual(
    unexpected.map((o) => `${o.file}:${o.line}: ${o.setter}(${JSON.stringify(o.lit)})`),
    [],
    `these sites render a hardcoded sentence, so a user on any other dashboard ` +
      `language reads the author's language — add a dictionary key and call ` +
      `t("that.key"):\n  ` +
      unexpected.map((o) => `${o.file}:${o.line}: ${o.setter}(${JSON.stringify(o.lit)})`).join("\n  "),
  );
});

/**
 * A comment line, for the by-name assertions below (which read the builder and
 * sales sources directly and must not trip on their own explanatory comments).
 */
const isCommentLine = (l: string): boolean => /^\s*(\/\/|\*|\/\*)/.test(l);

test("the builder's image-upload refusals are fixed, not merely absent", () => {
  // A walk reporting zero offenders can also mean the handler was deleted or
  // guarded away, so the corrected call sites are pinned by name.
  const builder = readFileSync(`${SRC}app/builder/page.tsx`, "utf8");
  assert.match(
    builder,
    /setLoadErr\(t\("bld\.imageTypeUnsupported"\)\)/,
    "the image-type pre-flight refusal is no longer translated",
  );
  assert.match(
    builder,
    /setLoadErr\(t\("media\.uploadTooLarge"\)\)/,
    "the 20MB pre-flight refusal is no longer translated",
  );
  // The Malay sentences themselves must be gone from the CODE. A comment may
  // still quote them to explain the defect, so only non-comment lines count.
  const malay = builder
    .split("\n")
    .filter((l) => !isCommentLine(l))
    .filter((l) => /Jenis imej tidak disokong|Imej melebihi had/.test(l));
  assert.deepEqual(
    malay,
    [],
    `the hardcoded Malay sentence is back in the builder:\n  ${malay.join("\n  ")}`,
  );
});

test("the sales capture refusal is fixed too (the guard's other finding)", () => {
  // Running this guard on the tree it was written on found a third instance of
  // the same class in the same release wave: `setMsg("Jumlah capture tidak
  // sah.")` on the capture-amount pre-check, left behind when the other four
  // messages in that handler were converted. It is fixed rather than
  // allowlisted — a class guard that exempts a live instance of its own class
  // proves nothing.
  const sales = readFileSync(`${SRC}app/sales/page.tsx`, "utf8");
  assert.match(
    sales,
    /setMsg\(t\("sales\.captureInvalid"\)\)/,
    "the capture-amount refusal is no longer translated",
  );
  const malay = sales
    .split("\n")
    .filter((l) => !isCommentLine(l))
    .filter((l) => /Jumlah capture tidak sah/.test(l));
  assert.deepEqual(malay, [], `the hardcoded Malay sentence is back in /sales:\n  ${malay.join("\n  ")}`);
});

test("every key those call sites use is real and carries all five locales", () => {
  // The fixes can break silently in two ways: a typo'd key renders the bare key
  // string ("bld.imageTypeUnsupported") to EVERY user, and a key with only `en`
  // silently shows English to ms/zh/ta/ar. i18n.test.ts covers the
  // dictionary-wide version of the second; this pins the keys this change added.
  for (const key of ["bld.imageTypeUnsupported", "sales.captureInvalid", "media.uploadTooLarge"]) {
    const entry = (DICT as Record<string, Record<string, unknown>>)[key];
    assert.ok(entry, `missing dictionary key ${key}`);
    for (const lang of LANGS) {
      const v = entry[lang];
      assert.ok(
        typeof v === "string" && v.trim() !== "",
        `${key}.${lang} is missing — a user on that locale sees the fallback`,
      );
    }
  }
  // The English text specifically, so an entry that quietly became Malay for
  // `en` is caught rather than merely "not absent".
  const en = (DICT as Record<string, Record<string, unknown>>)["bld.imageTypeUnsupported"].en;
  assert.equal(typeof en, "string");
  assert.match(en as string, /not supported/i);
  assert.doesNotMatch(
    en as string,
    /tidak disokong/,
    "the en value is the Malay sentence again",
  );
});
