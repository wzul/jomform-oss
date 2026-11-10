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
//   * A hardcoded literal in a JSX TEXT NODE (`<p>Some sentence</p>`). That is a
//     real, adjacent channel, but it is not this guard's: a text node carries no
//     attribute name to key on, and the dashboard's prose lives in attributes and
//     in the dictionary. (Text-attribute literals — `placeholder`, `aria-label`,
//     `title`, `alt` — WERE in this list until t_541a31d5 folded them in; see
//     "THE SECOND CHANNEL" below.)
//   * A SINGLE-TOKEN value in a text attribute that is Latin script and not a
//     Malay word — `placeholder="000000"`, `"0.00"`, `"slug-url"`, `"number"`.
//     Those are format hints, not prose, and a phrase test cannot see them. A
//     single-token MALAY value (`"anda@kedai.my"`) IS caught, by the language
//     test rather than the phrase test — that was one of the strings t_541a31d5
//     fixed.
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
  // The SETTER is deliberately not pinned: the refusal carries a failure tone
  // since card t_c88c854d (`showMsgFail(t(...))`), and this assertion is about
  // the SENTENCE being a dictionary key rather than the Malay prose. Pinning
  // `showMsgFail` here too would make a future, equally-valid tone change look
  // like a translation regression.
  assert.match(
    sales,
    /(?:showMsgFail|setMsg)\(t\("sales\.captureInvalid"\)\)/,
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

// ===========================================================================
// THE SECOND CHANNEL: text attributes and the dictionary's own language
// (card t_541a31d5)
// ===========================================================================
//
// THE DEFECT. A QA audit of the shipped dashboard found six merchant-facing
// strings that were Malay on an ENGLISH dashboard (default `en`, per-profile
// `users.lang`):
//
//   * the fee-acknowledgement sentence, served by the API and printed verbatim
//     (`{feeAckText || t("set.feesAck")}`) — the API cannot know the caller's
//     language (issue #6177), so the page must render its own arm;
//   * `login.forgotSent`, whose `en` arm WAS the Malay sentence (proven in the
//     prod chunk: `"login.forgotSent":{en:"Jika emel tersebut berdaftar …"}`);
//   * four `placeholder=` literals written straight into components
//     ("Selamat datang ke kedai kami", "Barang-barang istimewa untuk anda",
//     "Resit pembayaran — {{ref_code}}" twice, "Jualan baharu — {{ref_code}}")
//     plus a Malay e-mail sample ("anda@kedai.my");
//   * a Malay chat fallback sentence in api/internal/chat/chat.go.
//
// WHY THE EXISTING GUARDS WERE ALL GREEN. src/lib/i18n.test.ts checks every key
// in five locales but cannot judge whether an `en` arm is ENGLISH — a value that
// exists and is non-empty passes. The scanner above checks error-state setters
// only; a `placeholder=` is not an error state, so the six literals were in no
// guard's field of view. Both gaps were green by construction.
//
// WHAT IS ADDED HERE
//   1. THE TEXT-ATTRIBUTE SCANNER. `placeholder`, `aria-label`, `title` and `alt`
//      values are text a person reads. A literal in one of them is either a
//      dictionary key (`t("…")`, or a bare key passed through) or an offender.
//      Two predicates, because the two defects have different shapes:
//        * a MULTI-WORD literal is prose someone wrote in one language — that is
//          the `placeholder="Selamat datang…"` shape;
//        * a literal that CONTAINS A MALAY WORD is a Malay leak whatever its
//          shape — that is the `"anda@kedai.my"` shape, which has no whitespace
//          at all and which a phrase test can never see.
//   2. THE DICTIONARY'S LANGUAGE. No `en` arm may contain a Malay word. This is
//      the half `i18n.test.ts` structurally cannot do, and it is what
//      `login.forgotSent` needed: its `en` arm was a DUPLICATE of `ms`.
//   3. THE TWO-HALVES PIN for the fee sentence: the dictionary's `en` arm and
//      api/internal/payments/FeeAcknowledgementText must be the SAME sentence,
//      because the docs quote the English one and the API serves it to agents.
//
// THE RATCHET, AND WHAT IT ADMITS. A phrase-shaped literal that is NOT prose —
// a sample value ("Acme Sdn Bhd", "+60 12 345 6789"), a code snippet — cannot be
// told from prose by shape. Those are listed in KNOWN_TEXT_ATTR with a reason.
// It is a SUBSET ratchet, like KNOWN_SAME_CLASS above: an offender that is not
// listed fails the suite, so a new literal can never hide behind an existing
// entry. The Malay-word rule has NO ratchet — a Malay literal in a text
// attribute is the defect itself, and there is no legitimate instance of it.
//
// WHAT IS STILL NOT COVERED: a single-token Latin value ("number", "0.00",
// "000000", "slug-url") — a format hint, not prose. Stated in the header above.

/** Attributes whose value is text a person reads (or a screen reader reads). */
const TEXT_ATTRS = new Set(["placeholder", "aria-label", "title", "alt"]);

/**
 * A page that is prose by declaration, not a merchant surface: the docs site (the
 * knowledge base, English, no `t()` at all) and the design gallery (the internal
 * manifest at /design — its strings are SAMPLE VALUES of primitives, marked
 * `robots: { index: false }`, and no merchant's language setting reaches it). A
 * literal here is content, not an untranslated sentence, so scanning it would
 * report page-structure names and example data as defects.
 */
const isReferencePage = (relPath: string): boolean =>
  relPath.startsWith("app/docs/") || relPath.startsWith("app/design/");

/**
 * Malay function words and nouns that English does not use. Deliberately short
 * words with an English homograph are LEFT OUT (`dan`, `ada`, `ini`, `di`, `ke`,
 * `ia`, `tak`) so this cannot fire on a name or an abbreviation; every word here
 * is Malay-only, so a hit is a Malay string and not a guess.
 *
 * NOT global: a /g/ regex carries lastIndex between .test() calls and would miss
 * every second string.
 */
const MALAY_WORDS =
  /\b(yang|anda|tidak|untuk|dengan|saya|boleh|dalam|adalah|kepada|atau|juga|sudah|belum|kami|mereka|kita|jika|semak|emel|bila|bagi|oleh|tersebut|sesuatu|kerana|daripada|sahaja|tetapi|selepas|sebelum|semula|sila|mesti|telah|pernah|memang|semua|banyak|jualan|borang|tetapan|pesanan|produk|kedai|bahasa|pengguna|pembayaran|resit|caj|butiran|medan|simpan|hantar|padam|tambah|cari|pilih|tukar|muat|ralat)\b/i;

/** True when `s` carries a Malay word — i.e. it is not English prose. */
const looksMalay = (s: string): boolean => MALAY_WORDS.test(s);

/** True when a literal is a dictionary KEY, not text for a person. */
const isDictKey = (s: string): boolean => Object.prototype.hasOwnProperty.call(DICT, s);

/** Every string literal under a text attribute, with its real source line. */
function textAttrLits(
  fileName: string,
  text: string,
): { line: number; attr: string; lit: string }[] {
  const sf = ts.createSourceFile(fileName, text, ts.ScriptTarget.Latest, true);
  const out: { line: number; attr: string; lit: string }[] = [];
  const visit = (node: TS.Node): void => {
    if (ts.isJsxAttribute(node) && ts.isIdentifier(node.name) && TEXT_ATTRS.has(node.name.text)) {
      const lits: string[] = [];
      const collect = (n: TS.Node): void => {
        if (ts.isStringLiteral(n) || ts.isNoSubstitutionTemplateLiteral(n)) lits.push(n.text);
        ts.forEachChild(n, collect);
      };
      if (node.initializer) collect(node.initializer);
      const lc = sf.getLineAndCharacterOfPosition(node.getStart(sf));
      for (const lit of lits) out.push({ line: lc.line + 1, attr: node.name.text, lit });
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);
  return out;
}

/**
 * Text-attribute literals that need a reason to be there:
 *
 *   * MALAY — the defect itself. `MALAY` entries are never allowed and must not
 *     be ratcheted: an empty list is the invariant.
 *   * PROSE — a multi-word literal, which is a sentence someone wrote in one
 *     language. Ratcheted only when it is a sample value or a code sample.
 *
 * Single-token Latin values ("0.00", "number", "https://…") are not prose and are
 * not reported; see the header's honest gap.
 */
type TextAttrFinding = { line: number; attr: string; lit: string; kind: "malay" | "prose" };

function textAttrOffenders(fileName: string, text: string): TextAttrFinding[] {
  const out: TextAttrFinding[] = [];
  for (const { line, attr, lit } of textAttrLits(fileName, text)) {
    const v = lit.trim();
    if (v === "" || isDictKey(lit)) continue;
    if (looksMalay(v)) out.push({ line, attr, lit, kind: "malay" });
    else if (/\s/.test(v)) out.push({ line, attr, lit, kind: "prose" });
  }
  return out;
}

/**
 * A text-attribute literal that is NOT prose — a sample value or a code sample.
 * Matched by FILE + LITERAL TEXT (never by line number), so an unrelated edit
 * above it cannot silently move the waiver onto a different string.
 *
 * There is no `chrome` reason any more. The English chrome this list used to
 * ratchet — Shell's a11y labels, "Brand ID", "Email fallback (one-time forms)",
 * "QR code", the brand alt text — became dictionary keys in card t_b432c9ca, so
 * a literal coming back now fails the PROSE rule above instead of needing a
 * waiver here.
 */
const KNOWN_TEXT_ATTR: { file: string; lit: string; why: "sample value" | "code sample" }[] = [
  // Sample values: an example of DATA the field accepts, not a sentence anyone
  // wrote for the merchant to read. The same string on every locale is correct.
  { file: "app/settings/page.tsx", lit: "e.g. 5f2b…-uuid", why: "sample value" },
  { file: "app/settings/page.tsx", lit: "e.g. C1234567890", why: "sample value" },
  { file: "app/settings/page.tsx", lit: "e.g. 62010", why: "sample value" },
  { file: "app/settings/page.tsx", lit: "Acme Sdn Bhd", why: "sample value" },
  { file: "app/settings/page.tsx", lit: "+60 12 345 6789", why: "sample value" },
  { file: "app/settings/page.tsx", lit: "1 Jalan Test, Kuala Lumpur", why: "sample value" },
  { file: "app/form-settings/page.tsx", lit: "Admin Notification", why: "sample value" },
  // Code samples: the value is a snippet the merchant copies, not a sentence.
  { file: "app/settings/page.tsx", lit: '<script src="https://www.googletagmanager.com/gtag/js?id=G-XXXX"></-script>', why: "code sample" },
  { file: "app/form-settings/page.tsx", lit: "// e.g. document.querySelector('#of').addEventListener('submit', ...)", why: "code sample" },
];

test("the text-attribute scanner sees the shapes that shipped (self-test)", () => {
  // The scanner is this half of the guard's instrument, so its failure modes are
  // pinned the same way the error-setter scanner's are above. Every PRE-FIX
  // shape from the card is fed in, plus the negatives that must stay clean —
  // reporting one of those would make the guard red on a healthy tree.
  const cases: { name: string; src: string; want: string[] }[] = [
    {
      name: "the landing-page hero placeholder (Malay + multi-word)",
      src: '<input placeholder="Selamat datang ke kedai kami" />',
      want: ["Selamat datang ke kedai kami"],
    },
    {
      name: "the receipt-subject placeholder",
      src: '<input placeholder="Resit pembayaran — {{ref_code}}" />',
      want: ["Resit pembayaran — {{ref_code}}"],
    },
    {
      name: "the Malay e-mail sample — NO whitespace, caught by the language test",
      src: '<input placeholder="anda@kedai.my" />',
      want: ["anda@kedai.my"],
    },
    {
      name: "a literal inside a fallback expression still reaches the field",
      src: '<input placeholder={ws?.receipt_subject || "Resit pembayaran — {{ref_code}}"} />',
      want: ["Resit pembayaran — {{ref_code}}"],
    },
    {
      name: "a translated attribute, and one with a translated fallback",
      src: '<input placeholder={t("login.emailPh")} title={t("a.b")} />',
      want: [],
    },
    {
      name: "a data-value example is not prose",
      src: '<input placeholder="https://yourapp.com/thanks" />',
      want: [],
    },
    {
      name: "an aria-label nobody can see is still read out loud",
      src: '<button aria-label="Open menu" />',
      want: ["Open menu"],
    },
    {
      name: "a non-text attribute is not this defect",
      src: '<input className="jf-input mt-1" type="email" />',
      want: [],
    },
    {
      name: "an empty attribute is the legitimate clear",
      src: '<input placeholder="" />',
      want: [],
    },
  ];
  for (const c of cases) {
    const got = textAttrOffenders("t.tsx", c.src).map((o) => o.lit);
    assert.deepEqual(got, c.want, `${c.name}: got ${JSON.stringify(got)}`);
  }
});

test("the Malay detector does not fire on English, and does on the shipped arm", () => {
  // The dictionary half below is only worth anything if the detector is neither
  // blind (it must catch the exact `en` arm that shipped) nor noisy (it must not
  // flag English, or the guard would be red on a healthy dictionary).
  assert.equal(
    looksMalay("Jika emel tersebut berdaftar dan disahkan, semak emel anda untuk pautan set semula."),
    true,
    "the detector misses the Malay sentence that shipped as login.forgotSent.en",
  );
  assert.equal(
    looksMalay("If that email is registered and verified, check your inbox for a reset link."),
    false,
    "the detector fires on the English sentence that replaced it",
  );
  for (const english of [
    "Special items for you",
    "Claude / Claude Desktop",
    "Troubleshooting",
    "Card 2.00% + RM1.00",
    "you@yourshop.my",
  ]) {
    assert.equal(looksMalay(english), false, `the detector fires on English: ${JSON.stringify(english)}`);
  }
});

test("no text attribute hands the user a Malay literal", () => {
  // No ratchet on purpose: a Malay literal in a text attribute IS the defect this
  // card is about (t_541a31d5), on every locale that is not Malay.
  const offenders = sources
    .filter((p) => !isReferencePage(rel(p)))
    .flatMap((p) =>
      textAttrOffenders(p, readFileSync(p, "utf8"))
        .filter((o) => o.kind === "malay")
        .map((o) => ({ file: rel(p), ...o })),
    );
  assert.deepEqual(
    offenders.map((o) => `${o.file}:${o.line}: ${o.attr}=${JSON.stringify(o.lit)}`),
    [],
    "these text attributes show Malay text on a dashboard whose language is not Malay — " +
      "move the string into src/components/i18nDict.ts (five locales) and pass t(\"that.key\")",
  );
});

test("no text attribute hands the user un-translated prose without a recorded reason", () => {
  const offenders = sources
    .filter((p) => !isReferencePage(rel(p)))
    .flatMap((p) =>
      textAttrOffenders(p, readFileSync(p, "utf8"))
        .filter((o) => o.kind === "prose")
        .filter(
          (o) =>
            !KNOWN_TEXT_ATTR.some(
              (k) => k.file === rel(p) && k.lit === o.lit,
            ),
        )
        .map((o) => ({ file: rel(p), ...o })),
    );
  assert.deepEqual(
    offenders.map((o) => `${o.file}:${o.line}: ${o.attr}=${JSON.stringify(o.lit)}`),
    [],
    "these text attributes carry a phrase written into the component, so every locale " +
      "reads the author's language — add a dictionary key and call t(\"that.key\"); if the " +
      "string is a sample VALUE or a code sample, add it to KNOWN_TEXT_ATTR with its reason",
  );
});

test("the nine English text attributes are dictionary keys, not deleted or literal", () => {
  // t_b432c9ca removed the `chrome` waivers, which leaves the PROSE rule as the
  // only automated guard — and that rule cannot see an attribute someone
  // DELETED. An image with no alt and a drawer with no label are worse for a
  // screen reader than an English label, so the call sites are pinned by name:
  // the key must be rendered, and each shared string must be rendered on BOTH
  // of its fields (the two CHIP feed inputs).
  const pinned = (file: string, needle: string, want: number, what: string) => {
    const src = readFileSync(`${SRC}${file}`, "utf8");
    const got = src.split(needle).length - 1;
    assert.equal(got, want, `${file}: ${what} — expected ${want} x ${needle}, found ${got}`);
  };
  pinned("components/Shell.tsx", 'aria-label={t("nav.openMenu")}', 1, "the mobile hamburger lost its label");
  pinned("components/Shell.tsx", 'aria-label={t("nav.closeMenu")}', 1, "the drawer close button lost its label");
  pinned("components/Shell.tsx", 'aria-label={t("nav.menu")}', 2, "the drawer dialog and its nav landmark lost their labels");
  pinned("app/form-settings/page.tsx", 'placeholder={t("fs.chipBrandIdPh")}', 2, "a Brand ID placeholder is no longer translated");
  pinned("app/form-settings/page.tsx", 'placeholder={t("fs.emailFallbackPh")}', 2, "an Email fallback placeholder is no longer translated");
  pinned("app/profile/page.tsx", 'alt={t("prof.qrAlt")}', 1, "the enrolment QR image lost its translated alt");
  pinned("app/brand/brand-client.tsx", 'alt={t("brand.logoAlt")}', 1, "the logo lost its translated alt");
  pinned("app/brand/brand-client.tsx", 'alt={t("brand.bannerAlt")}', 1, "the banner lost its translated alt");
});

test("the five reported placeholders are fixed, not merely absent", () => {
  // A walk reporting zero offenders can also mean the attribute was deleted, so
  // the corrected call sites are pinned by name — the same shape as the builder
  // and sales pins above. Both receipt-subject fields are pinned: they show the
  // SAME sentence and must therefore come from the SAME key.
  const pinned = (file: string, needle: string, what: string) => {
    const src = readFileSync(`${SRC}${file}`, "utf8");
    assert.ok(
      src.includes(needle),
      `${file}: ${what} is no longer translated — expected ${needle}`,
    );
    const code = src.split("\n").filter((l) => !isCommentLine(l));
    for (const malay of [
      "Selamat datang ke kedai kami",
      "Barang-barang istimewa untuk anda",
      "Barang istimewa untuk anda",
      "Resit pembayaran — {{ref_code}}",
      "Jualan baharu — {{ref_code}}",
      "anda@kedai.my",
    ]) {
      assert.ok(
        !code.some((l) => l.includes(malay)),
        `${file}: the Malay placeholder ${JSON.stringify(malay)} is back in the code`,
      );
    }
  };
  pinned("app/settings/page.tsx", 'placeholder={t("set.lpHeroPh")}', "the landing hero placeholder");
  pinned("app/settings/page.tsx", 'placeholder={t("set.lpSubtitlePh")}', "the landing subtitle placeholder");
  pinned("app/settings/page.tsx", 'placeholder={t("set.emelSubjectPh")}', "the receipt-subject placeholder");
  pinned("app/form-settings/page.tsx", 't("set.emelSubjectPh")', "the form receipt-subject placeholder");
  // The rule editor's subject suggestion is chosen by EVENT: a response rule
  // never records a sale, so it must not suggest the sale subject (t_1768cb11).
  pinned("app/form-settings/page.tsx", 't("fs.notifSubjectPh")', "the notification-subject placeholder");
  pinned("app/form-settings/page.tsx", 't("fs.notifSubjectPhResponse")', "the response notification-subject placeholder");
  // An empty rule body is not "no body": it is JomForm's own message in the
  // form's language, so the field says so instead of starting with the system's
  // Malay text (t_1768cb11).
  pinned("app/form-settings/page.tsx", 't("fs.notifBodyPh")', "the notification-body placeholder");
  pinned("app/login/page.tsx", 'placeholder={t("login.emailPh")}', "the login email placeholder");
});

test("every key those placeholders use is real and carries all five locales", () => {
  for (const key of [
    "set.lpHeroPh",
    "set.lpSubtitlePh",
    "set.emelSubjectPh",
    "fs.notifSubjectPh",
    "login.emailPh",
  ]) {
    const entry = (DICT as Record<string, Record<string, unknown>>)[key];
    assert.ok(entry, `missing dictionary key ${key}`);
    for (const lang of LANGS) {
      const v = entry[lang];
      assert.ok(
        typeof v === "string" && v.trim() !== "",
        `${key}.${lang} is missing — a user on that locale sees the fallback`,
      );
    }
    // The `ms` arm must still carry the Malay suggestion: the point of moving
    // these into the dictionary is that a Malay merchant keeps Malay, not that
    // the phrase was deleted.
    assert.equal(
      looksMalay(String(entry.ms)),
      true,
      `${key}.ms is not Malay any more — the merchant on a Malay dashboard lost their suggestion`,
    );
    assert.equal(
      looksMalay(String(entry.en)),
      false,
      `${key}.en is Malay — that is the defect (t_541a31d5), not a translation`,
    );
  }
});

test("no dictionary EN arm is Malay", () => {
  // THE HALF i18n.test.ts CANNOT DO. That guard proves every key HAS an `en` arm
  // and that it is non-empty; a value that exists and is Malay passes it. The
  // shipped defect was exactly this shape — `login.forgotSent.en` was a
  // duplicate of `ms` — and the prod chunk was served with it.
  //
  // The check is the LANGUAGE, not `en === ms`: 75 keys legitimately share their
  // two arms because the term is the same in both languages ("Troubleshooting",
  // "Claude / Claude Desktop", "Slug (URL)", "Status"), and an `en === ms` test
  // would have to waive all of them — which is how a real duplicate would hide.
  const offenders = Object.entries(DICT)
    .filter(([, entry]) => looksMalay(String((entry as Record<string, unknown>).en)))
    .map(([key, entry]) => `${key}: ${JSON.stringify((entry as Record<string, unknown>).en)}`);
  assert.deepEqual(
    offenders,
    [],
    "these `en` arms are Malay, so an English dashboard shows Malay — translate the arm " +
      "(the `ms` arm keeps the Malay text)",
  );
});

test("the fee acknowledgement sentence is ONE sentence across the API and the dashboard", () => {
  // THE REPORTED DEFECT (t_541a31d5 #1). The API served Malay; the dashboard
  // printed whatever the API sent (`{feeAckText || t("set.feesAck")}`), so an
  // English Settings page showed Malay — and the docs at /docs/payment-fees
  // already quoted the ENGLISH sentence and said "the sentence is served by the
  // API", so the two surfaces contradicted each other on prod.
  //
  // Two things are pinned together:
  //   1. the served constant IS the English canonical sentence (so an agent, or a
  //      client with no dictionary, reads the disclosure in the language the
  //      docs quote);
  //   2. the dashboard's `en` arm is that SAME sentence, and the page renders the
  //      arm rather than the wire value, so a merchant reads their own language.
  const goFee = readFileSync(
    fileURLToPath(new URL("../../../api/internal/payments/fee.go", import.meta.url)),
    "utf8",
  );
  const m = goFee.match(/const FeeAcknowledgementText = "((?:[^"\\]|\\.)*)"/);
  assert.ok(m, "FeeAcknowledgementText is no longer a single-line string literal in fee.go");
  const served = m[1].replace(/\\"/g, '"');
  assert.equal(
    looksMalay(served),
    false,
    `the API still serves a Malay acknowledgement sentence: ${JSON.stringify(served)}`,
  );
  const arm = (DICT as Record<string, Record<string, unknown>>)["set.feesAck"].en;
  assert.equal(
    arm,
    served,
    "the dashboard's `en` arm and the sentence the API serves have drifted apart — the docs " +
      "quote the English one, and a merchant on either surface must read the same disclosure",
  );

  const settings = readFileSync(`${SRC}app/settings/page.tsx`, "utf8");
  assert.match(
    settings,
    /<span>\{t\("set\.feesAck"\)\}<\/span>/,
    "the fee acknowledgement checkbox no longer renders the dashboard's own arm",
  );
  assert.equal(
    settings.includes("acknowledgement_text"),
    false,
    "the settings page reads acknowledgement_text off the wire again — that field is English, " +
      "so a ms/zh/ta/ar merchant reads English (or, before t_541a31d5, Malay on an English page)",
  );

  // THE THIRD COPY WAS UNGUARDED (card t_ca62f9d6). fee.go's comment says a
  // reword is "a deliberate act in three places (here, the dictionary's `en`
  // arm, the docs)" — but only the first two were pinned. /docs/payment-fees
  // quotes the sentence in a prose body, so it is a COPY, and nothing noticed
  // if a reword left it behind: the docs would keep telling merchants they
  // accepted a sentence the checkbox no longer shows. Pin the copy to the
  // constant, which is what the page itself claims ("the sentence is served by
  // the API").
  const feeDocs = readFileSync(`${SRC}app/docs/payment-fees/page.tsx`, "utf8");
  assert.ok(
    feeDocs.includes(served),
    "/docs/payment-fees no longer quotes the acknowledgement sentence the API serves — " +
      `expected the page to contain ${JSON.stringify(served)}. The docs page is the third ` +
      "place fee.go says a reword must touch; update it with the constant",
  );
});

test("the chat's terminal fallback names a key, not a sentence", () => {
  // The API never learns the caller's language on a JSON call (issue #6177), so a
  // sentence written in api/internal/chat/chat.go reaches every dashboard locale
  // in one language. The step-budget exit already answers with the key
  // (keyReplyNoAnswer); the terminal line did not, and it was Malay.
  const goChat = readFileSync(
    fileURLToPath(new URL("../../../api/internal/chat/chat.go", import.meta.url)),
    "utf8",
  );
  // EVERY assignment to res.Reply must go through bound(), which trims and caps
  // the MODEL's own words. A bare literal there is prose the runner wrote, and a
  // runner-written sentence is the defect: it reaches all five locales in one
  // language. Pinned as "no literal assignment exists" rather than "the Malay one
  // is gone", so a reworded sentence cannot slip back in.
  const assignments = [...goChat.matchAll(/res\.Reply = ([^\n]*)/g)].map((m) => m[1].trim());
  assert.ok(assignments.length > 0, "chat.go no longer assigns res.Reply — this guard is looking elsewhere");
  assert.deepEqual(
    assignments.filter((a) => !a.startsWith("bound(")),
    [],
    "chat.go assigns a sentence to res.Reply — the answer must come from the model (bound()) " +
      "or name a dictionary key (answerOrKey), never prose the runner wrote",
  );
  assert.match(
    goChat,
    /res\.answerOrKey\(""\)\n\treturn res, nil\n\}/,
    "the chat runner's terminal branch no longer goes through answerOrKey, so a spent budget " +
      "would answer with nothing the page can render",
  );
});
