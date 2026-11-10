// The Markdown renderer for assistant output — the guard for the reported defect:
// an assistant answer showed its raw `**` markers instead of bold text.
//
// WHAT THIS FILE OWNS
//   1. THE ORIGINAL COMPLAINT, first: `**bold**` becomes <strong>, and no `**`
//      survives into the rendered text. The pre-fix page rendered `{m.content}`
//      into a `whitespace-pre-wrap` div, so the markers were the visible text.
//   2. Every element the card's table requires is an ELEMENT: italic, inline
//      code, a fenced block, ul/ol lists, links, and blank-line paragraphs.
//   3. It renders for real, not by source-scanning: react-dom/server
//      (renderToStaticMarkup) drives the component's actual element tree, and
//      the assertions read the HTML it produced. formPublicURL.test.ts already
//      uses this shape, so it runs under the existing gate
//      (`node --test src/lib/*.test.ts`) — no new runner, no vitest.
//   4. SECURITY. Assistant output is not trusted: the merchant's data (a form
//      answer, a product name, a customer's message) is echoed into replies, and
//      a question can contain anything. So a `<script>` or `<img onerror=…>`
//      inside the text must arrive in the DOM as characters, never as an element
//      — and a `javascript:` link must never become an anchor href.
//   5. The two surfaces share ONE renderer, and the raw text is what the server
//      stores (no "clean-up" on write), so a future renderer fix needs no
//      migration.
//
// WHY THE RENDERER IS Markdown.ts AND NOT .tsx: node --test strips types but
// cannot parse JSX, so a .tsx component would be unreachable from this file —
// and a guard that cannot import what it guards is decoration. The component is
// written with createElement for exactly this reason; converting it back to .tsx
// breaks these assertions.

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

import { Markdown, parseInline, parseMarkdown, __withScanProbe } from "../components/Markdown.ts";
// The repo's shared comment/JSX-text-aware blanker: the three PAGES below are read
// through it, because this test makes positive claims about a render site and a
// `//` line naming `<Markdown` satisfied them (t_9b40f272 — see the proof at the
// bottom of this file).
import { codeOnly } from "./jsxSource.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

const render = (text: string, variant?: "assistant" | "user") =>
  renderToStaticMarkup(createElement(Markdown, variant ? { text, variant } : { text }));

// The visible text of the rendered tree, with tags stripped — this is what a
// reader actually sees, which is how "no ** survives" is checked.
const visible = (html: string) =>
  html
    .replace(/<[^>]*>/g, "")
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&quot;/g, '"')
    .replace(/&#x27;/g, "'")
    .replace(/&amp;/g, "&");

// ── HOW THE TWO PERF GUARDS MEASURE TIME ─────────────────────────────────────
//
// Both perf guards below (the `_`-heavy answer, the `[`-heavy answer) are
// pass/fail GATE assertions — `web` is gate 6 of 19 in scripts/preflight.sh and
// that script is fail-fast — so what they must measure is the RENDERER's cost.
// performance.now() does not: the build host is shared, so other work is using
// these cores while this file runs, and a wall-clock delta then measures the
// scheduler instead.
//
// The "every sample is a best-of-N (min), because host load only ever ADDS time,
// so the minimum is the stable estimator" argument those ceilings were built on
// is FALSE for work this small. That was measured, not argued (filed as its own
// card): the `[`-heavy test redded at 42.5 ms against its 40 ms ceiling at
// `load average: 11.84` on a 6-core host, and passed on the SAME bytes at load
// 7.08. The minimum does not save it because one sample here is ~2-3 ms, i.e.
// SHORTER than a scheduler timeslice: under oversubscription every one of the N
// samples is preempted, so the minimum is preempted too. Reproduced with the
// whole suite pinned to ONE core with competing spinners on it: at 6 spinners
// `'[' x 16000` measured a wall minimum of 37.0 ms against a CPU minimum of
// 3.26 ms, the wall-clock version of this test red on its ratio assertion
// (24.9 ms against 0.6 ms), and the 33-bubble case measured 206-264 ms of wall
// time against its 300 ms ceiling; at 12 spinners the wall-clock version red
// 4 runs out of 4 (ratio assertion again: 34.5-42.2 ms against ~25 ms), while
// the CPU-time version of this file red 0 of 4 on that same core.
//
// process.cpuUsage() is the load-invariant estimator of the same work: time a
// process is DESCHEDULED does not accrue to it, so contention cancels out of the
// number instead of being added to it. Same bytes, idle vs that 6-spinner
// contention: `'[' x 16000` 3.72 -> 3.26 ms of CPU. The ceilings were left alone
// in that change, and the separation they exist for was unchanged: the fix costs
// ~2-7 ms of CPU at 16000 characters where the pre-fix patterns burn 176-368 ms
// of it at the same sizes.
//
// THE FIRST SAMPLE OF A SHAPE IS NOT THE RENDERER'S COST, AND A CEILING THAT
// TAKES ONE SAMPLE MEASURES THE WARM-UP. That is the gate flake this file was
// then carded for: `webimage` (the `docker build` of web/Dockerfile, i.e. the
// shipping artifact, run with the SAME `npm run build`) redded at 335 ms against
// the 300 ms transcript ceiling below, while the same bytes passed on the host
// and the same tree passed the gate on an immediate re-run. Measured in
// `node:24-alpine` (that Dockerfile's base image) with this file's own renderer:
//
//     one 33-bubble transcript, in order    566.0 ms  <- the sample the test took
//                                           44.3     58.4     277.8    104.7
//     the same render after ONE warm-up      30.0  (min of 5)
//     same bytes, host, first sample        198.8 ms
//
// So the number the ceiling rejected was V8's first-touch cost for a NEW shape —
// compiling the paths it reaches, first-touch heap pages, and the concurrent GC
// threads whose CPU is billed to this process — not the renderer's work. Host
// load was not the cause: those samples were taken with nothing else on the box
// and still range from 44 to 278 ms against a warm 30 ms, and over 100 draws in
// one process the single-sample estimator reached 441.6 ms (2 of 100 draws over
// the 300 ms ceiling) where a warm best-of-3 never exceeded 62.6 ms. Nothing in
// the fix below raises a ceiling: every CPU measurement warms the shape first and
// then takes a best-of-N minimum, which is what the per-render measurements in
// this file already did — the two WHOLE-TRANSCRIPT cases were the only
// single-sample timings in the file, and they are now measured the same way as
// the rest.
const cpuMillis = (fn: () => void): number => {
  const before = process.cpuUsage();
  fn();
  const delta = process.cpuUsage(before);
  // user + system, microseconds -> milliseconds. V8's concurrent GC threads are
  // part of the process's CPU time too, which is work this renderer also caused.
  return (delta.user + delta.system) / 1000;
};

/**
 * Best-of-N (min) of the process CPU milliseconds one render of `src` costs,
 * after ONE DISCARDED WARM-UP render of the same shape (see the header: the
 * first sample of a new shape carries ~10x the steady-state cost, and it is the
 * warm-up, not the renderer).
 */
const bestOfCpu = (src: string, reps: number): number => {
  render(src); // discarded warm-up — never part of the measurement
  let best = Infinity;
  for (let r = 0; r < reps; r++) best = Math.min(best, cpuMillis(() => render(src)));
  return best;
};

/** How many <Markdown> this file's chat-transcript fixture mounts (see below). */
const TRANSCRIPT_BUBBLES = 33;

/**
 * Best-of-N (min) of the CPU milliseconds one FULL transcript costs: 33 bubbles
 * of `src` rendered in one synchronous pass, which is what the /chat page does.
 * Warm-up and minimum for the same reason as `bestOfCpu` — this is the case that
 * used to take ONE sample, and one sample is what redded the 300 ms ceiling at
 * 335 ms on bytes whose warm cost is 30 ms.
 */
const transcriptCpu = (src: string, reps = 3): number => {
  for (let k = 0; k < TRANSCRIPT_BUBBLES; k++) render(src); // discarded warm-up
  let best = Infinity;
  for (let r = 0; r < reps; r++) {
    best = Math.min(
      best,
      cpuMillis(() => {
        for (let k = 0; k < TRANSCRIPT_BUBBLES; k++) render(src);
      }),
    );
  }
  return best;
};

test("the guard is looking at real files and the renderer really runs (it cannot pass vacuously)", () => {
  const src = read("../components/Markdown.ts");
  assert.ok(src.includes("export function Markdown"), "Markdown.ts is not the renderer source");
  // The element path is the reason this component exists; a switch to innerHTML
  // would make every security assertion below pass vacuously. Match the PROPERTY
  // form only (the file's header comment discusses the name in prose).
  assert.doesNotMatch(
    src,
    /dangerouslySetInnerHTML\s*[:=]/,
    "the renderer now uses dangerouslySetInnerHTML — the XSS guard in this file would be meaningless",
  );
  assert.ok(
    render("plain teks").includes("plain teks"),
    "the renderer produced no output for plain text",
  );
});

// ── 1. the reported defect ───────────────────────────────────────────────────

test("**bold** renders as <strong> and no ** survives into the visible text", () => {
  const html = render("Jumlah jualan minggu ini **RM1,250.00** sahaja.");
  assert.match(
    html,
    /<strong[^>]*>RM1,250\.00<\/strong>/,
    "**bold** did not become <strong> — this is the exact defect the card reports",
  );
  assert.doesNotMatch(visible(html), /\*\*/, "a raw ** is still visible in the rendered answer");
  assert.doesNotMatch(html, /\*\*/, "a raw ** is still in the output at all");
});

test("italic renders as <em> for both markers, and arithmetic stays arithmetic", () => {
  const star = render("ini *condong* ya");
  assert.match(star, /<em>condong<\/em>/, "*text* did not become <em>");
  const under = render("ini _condong_ ya");
  assert.match(under, /<em>condong<\/em>/, "_text_ did not become <em>");
  // `2 * 3 * 4` has spaces around the markers; emphasis must not swallow it.
  const math = render("kira 2 * 3 * 4 = 24");
  assert.ok(visible(math).includes("2 * 3 * 4 = 24"), `arithmetic was mangled: ${visible(math)}`);
  assert.doesNotMatch(math, /<em>/, "a spaced * became emphasis");
});

test("bold wins over italic in the ** ordering (the markers do not half-consume)", () => {
  const html = render("**tebal** dan *serong*");
  assert.match(html, /<strong[^>]*>tebal<\/strong>/);
  assert.match(html, /<em>serong<\/em>/);
});

// ── 1b. intra-word underscores are TEXT, not emphasis ────────────────────────
//
// The defect this file's second round fixes. `_` was read as emphasis whenever
// \S sat inside it, so an identifier LOST its underscores in the rendered text:
// `form_answers_value` showed as `formanswersvalue`, `report_final dan
// nota_akhir` became `report<em>final dan nota</em>akhir`. These strings are the
// assistant's ordinary vocabulary in this repo (`jf_2fa_pending` is a flag in
// api/internal/auth/session.go, `feedback_threads`/`form_answers_value` are real
// table and column names), so the assertion is the strong one — the visible text
// must EQUAL the source, character for character.
//
// CommonMark 0.31.2 §6.2 restricts intraword emphasis to asterisks
// (https://spec.commonmark.org/0.31.2/#emphasis-and-strong-emphasis): a `_`
// delimiter run must be left-flanking to open and right-flanking to close, so a
// run inside a word can do neither. The negative controls below pin that
// `_condong_` still italicises and that `*` was NOT touched.

test("an intra-word underscore loses nothing: the text is unchanged and no <em> appears", () => {
  // The exact rows the report names, plus the reported live reproduction. The
  // expected value is the SOURCE text for the identifier rows — with underscores
  // emphasis gone, nothing at all may be removed from them. The last row carries
  // deliberate `**` and a backtick span, so its expected text has those markers
  // rendered away (that is the bold/code behaviour, asserted elsewhere).
  const cases: [string, string, string][] = [
    ["Flag jf_2fa_pending sedang hidup.", "Flag jf_2fa_pending sedang hidup.", "the jf_2fa_pending flag name"],
    ["Kolum form_answers_value ada.", "Kolum form_answers_value ada.", "a column name with two underscores"],
    [
      "Jadual feedback_threads ada 3 baris.",
      "Jadual feedback_threads ada 3 baris.",
      "the shape that already survived — must not regress",
    ],
    ["Nama fail report_final dan nota_akhir.", "Nama fail report_final dan nota_akhir.", "one intra-word underscore per word"],
    ["Guna a_b_c sebagai contoh.", "Guna a_b_c sebagai contoh.", "a_b_c"],
    ["Baca fail ini_setting_2 dan itu.", "Baca fail ini_setting_2 dan itu.", "two identifiers on one line"],
    // Each half of the flanking rule is the ONLY thing that rejects these two: a
    // run inside a word cannot OPEN emphasis even when another run later in the
    // line could close it, and it cannot CLOSE one either.
    ["a_b c_", "a_b c_", "an intra-word run cannot open, though a later run could close"],
    ["_a b_c", "_a b_c", "an intra-word run cannot close, though an earlier run could open"],
    [
      "Aduan asal: assistant tulis **RM1,250.00** dan `jf_2fa_pending` gitu.\n\nKolum form_answers_value dan jadual feedback_threads memang ada.",
      "Aduan asal: assistant tulis RM1,250.00 dan jf_2fa_pending gitu.\n\nKolum form_answers_value dan jadual feedback_threads memang ada.",
      "the reproduction as filed on /feedback",
    ],
  ];
  for (const [src, expected, what] of cases) {
    const html = render(src);
    // Paragraph boundaries are structure, not text: restore them before the tag
    // strip so a two-paragraph body can still be compared character for
    // character (the block split is asserted separately further down).
    const text = visible(html.replace(/<\/p>\s*<p[^>]*>/g, "\n\n"));
    assert.equal(
      text,
      expected,
      `${what}: the rendered text is not the expected text — characters were lost to ` +
        `underscore emphasis. Got: ${JSON.stringify(text)}`,
    );
    assert.doesNotMatch(html, /<em>/, `${what}: an intra-word underscore was rendered as <em>`);
  }
});

test("underscore emphasis still works where CommonMark allows it (the negative control)", () => {
  // `_condong_` works today and its docs promise it; the fix must not remove it.
  for (const [src, inner] of [
    ["Ini _condong_ ya.", "condong"],
    ["kata _condong_ kata", "condong"],
    ["_condong_", "condong"],
    ["set semula _2fa_pending_ ya", "2fa_pending"],
  ] as const) {
    const html = render(src);
    assert.match(
      html,
      new RegExp(`<em>${inner}</em>`),
      `${JSON.stringify(src)} no longer renders <em>${inner}</em> — underscore emphasis was dropped ` +
        `instead of restricted, and /docs/chat still promises it`,
    );
  }
  // `__bold__` and `___`-style runs must keep pairing as strong.
  const strong = render("__tebal__ di sini");
  assert.match(strong, /<strong[^>]*>tebal<\/strong>/, "__text__ lost its strong emphasis");
  // A pair written around an identifier is still emphasis, and the identifier's
  // own underscores survive inside it.
  assert.match(render("_a_b_c_"), /<em>a_b_c<\/em>/, "an outer pair no longer emphasises the identifier");
});

test("asterisk emphasis is unchanged, including the intra-word form CommonMark allows", () => {
  // The rule is about `_` only. CommonMark's own example: internal emphasis with
  // `*` IS emphasis (`foo*bar*baz`), while `foo_bar_baz` is not.
  const star = render("foo*bar*baz");
  assert.match(star, /foo<em>bar<\/em>baz/, "intra-word * emphasis was lost — the fix over-reached into *");
  assert.equal(visible(render("foo_bar_baz")), "foo_bar_baz", "foo_bar_baz gained or lost characters");
  // A lone underscore next to a space is a literal underscore, not a delimiter.
  assert.equal(visible(render("a_ b _c")), "a_ b _c", "an underscore beside a space was consumed");
});

// ── 1c. RENDERING IS LINEAR IN THE LENGTH OF THE ANSWER ──────────────────────
//
// The defect this section guards. Pairing a `_` opener with the next `_` run that
// can close was originally done by re-walking the string from each opener. When an
// opener has NO closer ahead (`'_a '`, `'_private _internal'`), that walk runs to
// the end and returns null, and the parser then advances ONE character and does it
// again — O(n²) in the length of the answer. Measured through the real component
// (react-dom/server, production build) at the chat reply cap of 4000 characters
// (api/internal/chat/chat.go MaxReplyChars): 164 ms, and ~8 s for the 33 bubbles
// this file mounts in one synchronous render (the chat page mounts its whole
// transcript; 33 is the fixture).
// The same shape in the fix is ~4 ms, and the 33-bubble case ~66 ms.
//
// TWO ASSERTIONS, because one of them is not enough on its own:
//   * the WORK assertion is exact and load-independent — it counts the characters
//     the delimiter scan actually walks, and refuses to let that grow faster than
//     the input. This is the one that cannot flake.
//   * the TIME assertion is a coarse backstop for a regression that does its
//     quadratic work somewhere the counter does not see (a `for` loop inside the
//     render, say). Its ceiling is deliberately generous — see the ratio bound.
// Restoring the unbounded forward scan makes BOTH fail.

// The adversarial inputs, at and beyond the real caps. Each is a long run of
// opener-ish `_` with no closer anywhere ahead, which is the worst case.
const ADVERSARIAL: [string, string][] = [
  ["'_a ' repeated", "_a ".repeat(1400).slice(0, 4200)],
  ["'a _' repeated", "a _".repeat(1400).slice(0, 4200)],
  ["'__a ' repeated", "__a ".repeat(1050).slice(0, 4200)],
  ["'_private _internal _tmp' repeated", "_private _internal _tmp ".repeat(170).slice(0, 4200)],
  ["'_' x 4200 (one giant run)", "_".repeat(4200)],
  ["'__' x 4200", "__".repeat(2100)],
];

test("the delimiter scan does not walk the answer once per underscore (the O(n²) guard)", () => {
  // WHAT THIS COUNTS. Every `_` delimiter the renderer examines, via flanking().
  // A scan that looks at each delimiter once — this one — makes O(n) such calls;
  // the per-underscore rescan the card reports made O(n²). Measured on this host
  // at the chat reply cap (4000 chars of '_a ', api/internal/chat/chat.go
  // MaxReplyChars) and at 8x that:
  //
  //     length   this scan   the rescan it replaced
  //       1000        668          56 279   (56x the length)
  //       2000      1 334         223 445
  //       4000      2 668         891 779  (223x the length)
  //       8000      5 334       3 560 445  (445x the length)
  //      16000     10 668      14 233 779  (890x the length)
  //
  // So 2x the length is a bound the fix passes with 3x to spare at every size, and
  // the rescan fails by 28x at the SMALLEST input — before any timing can flake on
  // a loaded host. This is the assertion that cannot lie; the clock below is the
  // coarse backstop for a quadratic loop the counter cannot see.
  const callsAt = (len: number) =>
    __withScanProbe(() => render("_a ".repeat(Math.ceil(len / 3)).slice(0, len))).work;

  for (const len of [1000, 2000, 4000, 8000]) {
    const work = callsAt(len);
    // Vacuity: plain prose has no delimiter at all, so a non-zero reading here
    // proves the hook is wired to the scan and this test is not passing on zero.
    assert.ok(
      work > 0,
      `the scan reported no delimiter work at all for a ${len}-character answer of underscore ` +
        `runs — the probe is not measuring the scan, so every bound below is vacuous`,
    );
    assert.ok(
      work <= 2 * len,
      `the scan examined ${work} delimiters to render a ${len}-character answer (${(work / len).toFixed(1)}x ` +
        `the length; a single pass is under 1x). Rendering is no longer linear in the length of the ` +
        `answer — this is the O(n²) underscore scan the card reports.`,
    );
  }
  // 8x the input: a single pass costs 8x the calls (measured 668 -> 5334), the
  // rescan costs ~63x (56 279 -> 3 560 445). The bound sits between them.
  const w1000 = callsAt(1000);
  const w8000 = callsAt(8000);
  assert.ok(
    w8000 <= 16 * w1000,
    `8x the input cost ${(w8000 / Math.max(w1000, 1)).toFixed(1)}x the delimiter work ` +
      `(1000 chars: ${w1000}, 8000 chars: ${w8000}) — growth faster than linear, i.e. the ` +
      `quadratic shape is back.`,
  );

  // Every adversarial shape, at and beyond the cap: a single pass is all the work
  // permitted, so none of these can hide a per-underscore rescan.
  for (const [what, src] of ADVERSARIAL) {
    const { work } = __withScanProbe(() => render(src));
    assert.ok(
      work <= 2 * src.length,
      `${what}: rendering ${src.length} characters examined ${work} delimiters (> 2x the length)`,
    );
  }

  // The severity case from the card: the whole conversation. Each bubble is one
  // pass, so 33 of them are 33 passes — measured 88 044 calls for 33 x 4000
  // characters (0.67x), against 29 428 707 (223x) for the rescan.
  const bubble = "_a ".repeat(1334).slice(0, 4000);
  const { work } = __withScanProbe(() => {
    for (let k = 0; k < 33; k++) render(bubble);
  });
  assert.ok(
    work <= 2 * 33 * bubble.length,
    `33 bubbles of ${bubble.length} characters examined ${work} delimiters (> 2x the ` +
      `${33 * bubble.length} characters present) — a full chat history is O(n²) again`,
  );
});

test("an adversarial answer at the cap renders in milliseconds, not tenths of a second", () => {
  // The symptom the merchant sees, so the clock is asserted directly. The inputs
  // are the card's adversarial shapes: long runs of opener-ish `_` with no closer
  // ahead, at and beyond the real caps (chat reply 4000,
  // api/internal/chat/chat.go MaxReplyChars; feedback body 5000,
  // api/internal/feedbacksvc/feedbacksvc.go BodyMax).
  //
  // Every sample is a best-of-N (min) of the process CPU time, not the wall
  // clock — see "HOW THE TWO PERF GUARDS MEASURE TIME" at the top of this file
  // for why the wall-clock version of exactly this assertion is a gate flake, and
  // for the measurements. Measured through this component, in CPU ms: fix 4000
  // chars ~1.7-4.0, 16000 chars ~4.1-4.6, 33 bubbles ~45-54. The per-opener
  // re-walk this replaced measured 140.8 ms of CPU at 4000 characters — the run
  // that proved this assertion bites — against the 75 ms ceiling below (and
  // 164 ms / 3200 ms / 8032 ms of wall time on this host before the rewrite). The
  // ceilings below sit between the two by a wide margin on the side that matters:
  // ~19x headroom over the worst sample of the fix, and still well BELOW what the
  // quadratic shape measured, so restoring it fails rather than squeaks by.
  const CAP = 4000;
  const small = "_a ".repeat(Math.ceil(CAP / 3)).slice(0, CAP);
  const huge = "_a ".repeat(Math.ceil((CAP * 4) / 3)).slice(0, CAP * 4);
  const tCap = bestOfCpu(small, 5);
  const tHuge = bestOfCpu(huge, 3);

  assert.ok(
    tCap < 75,
    `a ${CAP}-character adversarial answer took ${tCap.toFixed(1)} ms of CPU to render (ceiling 75 ms). ` +
      `The fix measures ~2-4 ms here; the O(n²) scan measured 164 ms.`,
  );
  // 4x the input. Linear growth is ~4x the time; the quadratic shape was ~19x
  // (164 ms -> 3200 ms). The bound is 10x, with a constant so a near-zero
  // denominator cannot decide the test.
  assert.ok(
    tHuge <= 10 * tCap + 50,
    `4x the input took ${tHuge.toFixed(1)} ms of CPU against ${tCap.toFixed(1)} ms for ${CAP} characters ` +
      `— growth faster than linear. The quadratic scan measured 3200 ms / 164 ms here.`,
  );

  // The severity case from the card: the /chat page mounting its whole transcript
  // (33 <Markdown> in one synchronous render — this file's fixture; the page's
  // history cap bounds what it SENDS, not what it renders). 8000 ms of frozen tab
  // was the symptom; the fix is ~40-57 ms of CPU warm (measured in
  // node:24-alpine, idle and under 8 host spinners). `transcriptCpu` warms the
  // shape first and takes a best-of-3 minimum — a single sample measures V8's
  // first-touch cost, see "HOW THE TWO PERF GUARDS MEASURE TIME" above.
  const total = transcriptCpu(small);
  assert.ok(
    total < 3000,
    `33 bubbles of ${CAP} adversarial characters took ${total.toFixed(0)} ms of CPU (ceiling 3000 ms) — ` +
      `a full conversation freezes the tab. The quadratic scan measured ~8000 ms, the fix ~40-57 ms.`,
  );
});

// ── 1d. THE LINK/IMAGE PATTERNS DO NOT RESCAN THE ANSWER PER `[` ────────────
//
// The second quadratic in this file (filed as its own card: same renderer, other
// code path). `IMAGE`/`LINK` were applied at EVERY position from parseInline's
// loop, and a rejected attempt was only rejected once its `[^\]\n]*` text group
// had walked to the next `]` or to the end of the line — while parseInline
// advances one character on a miss, so nothing bounded it. The block splitter has
// already joined the answer into one paragraph, so the whole reply can be one
// such line.
//
// Measured through this component (production build, this host, min/median/max of
// ten best-of-3 samples) at the chat reply cap (4000 characters,
// api/internal/chat/chat.go MaxReplyChars) and 4x it:
//
//     input           4000                16000            ratio (2 = linear)
//     '[' x N     15.1 / 17.5 / 20.6 ms   266 / 281 / 301 ms   17.6x   <- before
//     '![' x N    16.2 / 20.6 / 24.7 ms   273 / 320 / 388 ms   16.8x
//     '[a' x N     8.5 / 11.0 / 11.6 ms   141 / 160 / 172 ms   16.5x
//     ----------------------------------------------------------------
//     '[' x N      0.7 /  1.0 /  1.3 ms   2.0 / 2.6 / 3.1 ms     2.6x   <- after
//     '![' x N     0.7 /  0.8 /  1.1 ms   1.5 / 3.2 / 3.4 ms     3.1x
//     '[a' x N     0.6 /  0.7 /  0.8 ms   2.2 / 2.6 / 2.8 ms     3.3x
//
// The severity case is the same one 1c cites: the chat page mounts 33 <Markdown>
// in one synchronous render (its whole transcript, this file's fixture), so 33 `[`
// characters was 635-876 ms of frozen tab and is 21-30 ms after the fix.
//
// TWO ASSERTIONS, the same split as 1c, because one is not enough:
//   * the WORK assertion counts the characters the link/image delimiter scan
//     walks. One pass is 1.0x the length of the answer at every size (measured),
//     so the bound cannot flake on a loaded host.
//   * the TIME assertion is the backstop for quadratic work the counter cannot
//     see — and here that is precisely the pre-fix shape, whose walk happens
//     inside the regex engine and therefore reports NO counted work at all. That
//     is why the vacuity check below is not decoration: a renderer that reports
//     zero link-delimiter work while producing this output is not instrumented,
//     which is exactly what the pre-fix code did.

// Long runs of `[`/`![` with no closer, at and beyond the caps (chat reply 4000
// — api/internal/chat/chat.go MaxReplyChars; feedback body 5000 —
// api/internal/feedbacksvc/feedbacksvc.go BodyMax), plus the shapes whose closer
// is far away.
const LINK_ADVERSARIAL: [string, string][] = [
  ["'[' x 4200", "[".repeat(4200)],
  ["'![' x 4200", "![".repeat(2100)],
  ["'[a' x 4200", "[a".repeat(2100)],
  ["'[' x N then one closer", "[".repeat(4198) + "]("],
  ["'[](a' repeated", "[](a".repeat(1050)],
];

test("the link/image delimiters are resolved once, not at every `[` (the O(n²) guard)", () => {
  // WHAT THIS COUNTS. Every character the LINK/IMAGE delimiter scan examines, one
  // unit each. A single left-to-right pass — this one — costs 1.0x the length of
  // the answer (measured 8000 for 8000 characters at every shape below); the
  // pre-fix shape resolved the closer inside the regex engine at every `[`, which
  // is O(n²) work and is not visible here at all (it reported 0).
  const linkWorkAt = (src: string) => __withScanProbe(() => render(src)).linkWork;

  for (const len of [1000, 2000, 4000, 8000]) {
    const src = "[".repeat(len);
    const work = linkWorkAt(src);
    // Vacuity: the scan must actually be measuring something.
    assert.ok(
      work > 0,
      `the renderer reported no link-delimiter work at all for a ${len}-character answer of '[' — ` +
        `the probe is not wired to the scan, so every bound below is vacuous`,
    );
    assert.ok(
      work <= 2 * len,
      `rendering ${len} characters of '[' examined ${work} characters of link delimiters ` +
        `(${(work / len).toFixed(1)}x the length; a single pass is 1.0x). The closer is being resolved ` +
        `per '[' again, not once per answer — that is the O(n²) LINK/IMAGE scan this card reports.`,
    );
  }
  // 8x the input: one pass is 8x the work (measured 1000 -> 8000 exactly), the
  // per-`[` rescan is ~64x. The bound sits between them.
  const w1000 = linkWorkAt("[".repeat(1000));
  const w8000 = linkWorkAt("[".repeat(8000));
  assert.ok(
    w8000 <= 16 * w1000,
    `8x the input cost ${(w8000 / Math.max(w1000, 1)).toFixed(1)}x the link-delimiter work ` +
      `(1000 characters: ${w1000}, 8000 characters: ${w8000}) — growth faster than linear.`,
  );

  // Every adversarial shape at and beyond the cap.
  for (const [what, src] of LINK_ADVERSARIAL) {
    const work = __withScanProbe(() => render(src)).linkWork;
    assert.ok(
      work > 0,
      `${what}: no link-delimiter work was counted — the guard is not seeing the scan`,
    );
    assert.ok(
      work <= 2 * src.length,
      `${what}: rendering ${src.length} characters examined ${work} characters of link delimiters ` +
        `(> 2x the length) — the per-'\[' closer search is back.`,
    );
  }

  // The severity case: the whole conversation, each bubble its own answer.
  const bubble = "[".repeat(4000);
  const { linkWork } = __withScanProbe(() => {
    for (let k = 0; k < 33; k++) render(bubble);
  });
  assert.ok(
    linkWork <= 2 * 33 * bubble.length,
    `33 bubbles of ${bubble.length} characters examined ${linkWork} link delimiters (2x bound: ` +
      `${2 * 33 * bubble.length}) — a full chat history is O(n²) again`,
  );
});

test("an adversarial `[`-heavy answer renders in milliseconds, not a frozen tab", () => {
  // The symptom the merchant sees, asserted directly — on the clock that measures
  // the renderer and not the host ("HOW THE TWO PERF GUARDS MEASURE TIME" above).
  // The ceilings are UNCHANGED from the wall-clock version this replaced, at
  // exactly the size that redded it: 40 ms at 16000 characters where the fix
  // costs ~2-7 ms of CPU and the pre-fix patterns burn 176-368 ms, and 300 ms for
  // the full conversation where the fix costs ~30-52 ms warm and the pre-fix
  // patterns burn ~900-1057 ms. Restoring the pre-fix patterns fails rather than
  // squeaks by.
  //
  // WHAT MADE THIS GATE FLAKE WAS THE MEASUREMENT, NOT THE CEILING. The
  // whole-transcript case below used to be the ONE single-sample timing in this
  // file, and one sample of a new shape is V8's first-touch cost: measured in
  // node:24-alpine (the `webimage` gate's base image) the first 33-bubble render
  // of these bytes took 566.0 ms and the warm minimum 30.0 ms, which redded the
  // 300 ms ceiling at 335 ms on an operation whose steady-state cost is 30 ms.
  // `transcriptCpu` warms the shape and takes a best-of-3 minimum; the ceiling
  // stays where it is, so the net is not weakened for the flake.
  const CAP = 4000;
  // The shapes the wall-clock ceiling guarded, each also asserted as COUNTED
  // link-delimiter work (the sibling test above): the count is the half that
  // cannot flake. The vacuity check is not decoration — the pre-fix patterns did
  // their walk inside the regex engine and reported ZERO counted work, so a zero
  // reading here means the quadratic is back and hiding.
  const shapes = ["[".repeat(CAP * 4), "![".repeat(CAP * 2), "[a".repeat(CAP * 2)];
  for (const src of shapes) {
    const { linkWork } = __withScanProbe(() => render(src));
    assert.ok(
      linkWork > 0,
      `rendering ${src.length} characters of '['-heavy text counted no link-delimiter work at all — ` +
        `the scan is not being measured (the pre-fix patterns reported 0 here), so the bound below is vacuous`,
    );
    assert.ok(
      linkWork <= 2 * src.length,
      `${src.length} characters of '['-heavy text examined ${linkWork} characters of link delimiters ` +
        `(${(linkWork / src.length).toFixed(1)}x the length; a single pass is 1.0x) — the per-'[' closer search is back`,
    );
    // Best of 5 (warm, see `bestOfCpu`): measured in node:24-alpine over 12
    // trials, idle and under 8 host spinners, the worst best-of-5 was 7.5 ms
    // against the 40 ms ceiling, while a best-of-3 occasionally caught a GC
    // spike (24.6 ms). The pre-fix patterns measure 206-481 ms here, so the
    // margin on the side that matters is unchanged.
    const t = bestOfCpu(src, 5);
    assert.ok(
      t < 40,
      `a ${src.length}-character '['-heavy answer took ${t.toFixed(1)} ms of CPU to render (ceiling 40 ms). ` +
        `The fix measures ~2-8 ms here; the pre-fix patterns measured 176-368 ms.`,
    );
  }

  // 4x the input. Linear growth is ~2.6-3.3x the time; the pre-fix shape was
  // ~17x. The bound is 6x with a constant so a near-zero denominator cannot
  // decide the test.
  const tCap = bestOfCpu("[".repeat(CAP), 5);
  const tHuge = bestOfCpu("[".repeat(CAP * 4), 3);
  assert.ok(
    tHuge <= 6 * tCap + 20,
    `4x the input took ${tHuge.toFixed(1)} ms of CPU against ${tCap.toFixed(1)} ms for ${CAP} characters ` +
      `— growth faster than linear. The pre-fix patterns measured 336 ms / 22 ms here.`,
  );

  // The severity case: a full transcript (33 <Markdown> in one synchronous render,
  // this file's fixture).
  //
  // TWO BOUNDS, because the absolute one alone is what flaked. The RELATIVE bound
  // is the environment-invariant half: an ordinary answer of the same length and
  // the same bubble count is measured with the same warm-up and the same
  // best-of-3, so a slower CPU, a dev-build of react-dom, or a container whose
  // background GC is heavier moves both numbers together — while the pre-fix
  // patterns, whose cost is quadratic in the answer, cannot stay inside a small
  // multiple of it. Measured (node:24-alpine, idle and under 8 host spinners):
  //     fix     adversarial 29.5-32.6 ms   ordinary 32.3-45.0 ms   ratio 0.66-1.01
  //     pre-fix adversarial 899-941 ms     ordinary 31.8-39.0 ms   ratio 24.2-28.2
  // The absolute 300 ms ceiling stays as the "frozen tab" backstop it always was;
  // warm, the fix measures 26-52 ms over 12 trials (6x margin) and the pre-fix
  // patterns ~900 ms (3x over).
  const bubble = "[".repeat(CAP);
  const adversarial = transcriptCpu(bubble);
  // Same size, same bubble count, no Markdown delimiter in it at all.
  const ordinary = transcriptCpu("laporan biasa tanpa sebarang penanda ".repeat(200).slice(0, CAP));
  assert.ok(
    ordinary > 0,
    "the ordinary-answer control measured 0 ms of CPU — the relative bound below would be vacuous",
  );
  assert.ok(
    adversarial <= 3 * ordinary + 100,
    `33 bubbles of ${CAP} '['-heavy characters took ${adversarial.toFixed(0)} ms of CPU against ` +
      `${ordinary.toFixed(0)} ms for 33 ordinary answers of the same length — an adversarial answer ` +
      `costs more than 3x an ordinary one of the same size. The pre-fix patterns measured ~900 ms ` +
      `against ~35 ms (25x) here; the fix ~30 ms against ~35 ms.`,
  );
  assert.ok(
    adversarial < 300,
    `33 bubbles of ${CAP} '['-heavy characters took ${adversarial.toFixed(0)} ms of CPU (ceiling 300 ms) — ` +
      `a full conversation freezes the tab. The pre-fix patterns measured ~900-1057 ms, the fix ~30-52 ms.`,
  );
});

test("the link/image guard changes nothing about which shapes are links (the boundaries)", () => {
  // The guard only decides WHETHER the pattern runs; these are the shapes where
  // the two delimiter rules meet, pinned so a future change to the scan cannot
  // quietly move the boundary.
  const anchored = "Baca [panduan](https://jomform.com/docs/chat) ini.";
  assert.match(render(anchored), /href="https:\/\/jomform\.com\/docs\/chat"/);
  // A space inside the LABEL is fine (the URL group's whitespace rule only
  // applies after the `]`) — the case a naive "scan for whitespace" would break.
  assert.match(render("[a b](https://x.com)"), /href="https:\/\/x\.com"/);
  assert.match(visible(render("[a b](https://x.com)")), /a b/);
  // The text group may not cross a line...
  assert.doesNotMatch(render("[a\nb](https://x.com)"), /<a[\s>]/);
  // ...nor may `](` be split by a space, and the URL must close.
  assert.doesNotMatch(render("[a] (https://x.com)"), /<a[\s>]/);
  assert.doesNotMatch(render("[a](https://x.com"), /<a[\s>]/);
  // A `(` inside the URL ends the URL group before it can close.
  assert.doesNotMatch(render("[a](https://x.com/(y))"), /<a[\s>]/);
  // The URL group is closed by the FIRST `)`, and an empty label falls back to
  // the href — the two edges of `m[1] || href`.
  assert.match(render("[](https://x.com)"), /<a[^>]*href="https:\/\/x\.com"[^>]*>https:\/\/x\.com<\/a>/);
  assert.match(render("[a](https://x.com)"), /<a[^>]*>a<\/a>/);
  // An image is its alt text (unsupported on purpose), and `![` is checked
  // before LINK so a stray `!` does not swallow the link.
  assert.equal(visible(render("![p](https://x.com/a.png)")), "p");
  assert.match(render("![p](https://x.com/a.png)[b](https://y.com)"), /href="https:\/\/y\.com"/);
  // Nothing here may become an anchor even though the delimiters are complete.
  assert.doesNotMatch(render("[klik](javascript:alert(1))"), /<a[\s>]/);
  assert.doesNotMatch(render("[x](/relative)"), /<a[\s>]/);
});

// ── 2. the required element set ──────────────────────────────────────────────

test("inline code becomes <code>, fenced code becomes <pre><code>", () => {
  const inline = render("jalankan `npm run build` dulu");
  assert.match(inline, /<code[^>]*>npm run build<\/code>/, "inline code is not an element");
  assert.doesNotMatch(visible(inline), /`/, "the backticks are still visible");

  const fenced = render("Contoh:\n\n```bash\ncurl -X POST https://example.com\n```\n\nSelesai.");
  assert.match(fenced, /<pre[^>]*>/, "a fenced block did not become <pre>");
  assert.match(fenced, /<code[^>]*>curl -X POST https:\/\/example\.com<\/code>/, "the code block body is not code");
  assert.doesNotMatch(visible(fenced), /```/, "the fence markers are still visible");
  // Markdown inside a code block is NOT formatted — the span is verbatim.
  const literal = render("```\n**bukan bold**\n```");
  assert.ok(visible(literal).includes("**bukan bold**"), "the code block was markdown-formatted, not verbatim");
  assert.doesNotMatch(literal, /<strong/, "markdown inside a code fence was applied");
});

test("code blocks scroll inside the card instead of widening it", () => {
  // The layout half of this claim (no horizontal page overflow at 412px) is
  // measured in a real browser and reported in the PR; what is pinned here is
  // the class that carries it, so it cannot be dropped silently.
  const html = render("```\n" + "x".repeat(400) + "\n```");
  assert.match(
    html,
    /<pre class="[^"]*overflow-x-auto[^"]*"/,
    "the code block lost overflow-x-auto — a wide block now pushes the card past the viewport",
  );
  assert.match(html, /<code class="[^"]*whitespace-pre[^"]*/, "the code block cannot preserve its own lines");
  const long = render("tengok `" + "y".repeat(200) + "` ini");
  assert.match(long, /<code class="[^"]*break-words[^"]*"/, "a long inline code span cannot break on a phone");
});

test("- and 1. lists become <ul>/<ol> with list markers", () => {
  const ul = render("Borang anda:\n\n- Daftar Ahli\n- Bayaran\n- Maklum balas");
  assert.match(ul, /<ul[^>]*>/, "a bullet list did not become <ul>");
  assert.equal([...ul.matchAll(/<li[^>]*>/g)].length, 3, "the bullet list lost items");
  assert.match(ul, /<li[^>]*>Daftar Ahli<\/li>/, "a list item's text is not in the <li>");
  assert.doesNotMatch(visible(ul), /^- /m, "the - markers are still visible");

  const ol = render("Langkah:\n\n1. Buka Settings\n2. Pilih Payment\n3. Simpan");
  assert.match(ol, /<ol[^>]*>/, "an ordered list did not become <ol>");
  assert.equal([...ol.matchAll(/<li[^>]*>/g)].length, 3, "the ordered list lost items");
  assert.doesNotMatch(visible(ol), /^\d+\. /m, "the number markers are still visible");
});

test("links become <a> with rel=noopener noreferrer in a new tab", () => {
  const html = render("Baca [panduan](https://jomform.com/docs/chat) ini.");
  const anchor = html.match(/<a\b[^>]*>/)?.[0] ?? "";
  assert.match(anchor, /href="https:\/\/jomform\.com\/docs\/chat"/, "an https link did not become an anchor with its href");
  assert.match(anchor, /rel="noopener noreferrer"/, "the external link has no rel=noopener noreferrer");
  assert.match(anchor, /target="_blank"/, "the external link does not open in a new tab");
  assert.match(visible(html), /panduan/, "the link text was lost");
  assert.doesNotMatch(visible(html), /\]\(/, "the markdown link syntax is still visible");
});

test("a blank line separates paragraphs, and a single newline stays a line break", () => {
  const two = render("Perenggan satu.\n\nPerenggan dua.");
  assert.equal([...two.matchAll(/<p[^>]*>/g)].length, 2, "a blank line did not start a new paragraph");
  // The page used to render with whitespace-pre-wrap; a line break the user (or the
  // model) typed must not disappear.
  assert.match(two, /<p class="[^"]*whitespace-pre-wrap[^"]*"/, "line breaks inside a paragraph are no longer preserved");
  const one = render("Baris satu.\nBaris dua.");
  assert.equal([...one.matchAll(/<p[^>]*>/g)].length, 1, "a single newline became a paragraph break");
  assert.match(visible(one), /Baris satu\.\nBaris dua\./, "the single newline was dropped");

  // ── THE COMPANION, and the reason the rule is scoped rather than deleted (card
  // t_b5ea9103). The same typed newline in DOCS prose must NOT be preserved: the guides'
  // READMEs are hard-wrapped at ~100 columns, and under pre-wrap every one of those wraps
  // became a real line break on the page — the "tiba-tiba ada new line" Wan saw at 390px.
  // Authored prose is wrapped by the browser; user-typed text keeps its line breaks.
  const docs = (text: string) =>
    renderToStaticMarkup(createElement(Markdown, { text, mode: "docs" }));
  const docsPara = docs("Perenggan satu.\nPerenggan dua.");
  assert.doesNotMatch(
    docsPara,
    /whitespace-pre-wrap/,
    "a docs paragraph still preserves the source's hard wrap — the README's ~100-column wrapping renders as broken text on a phone",
  );
  assert.match(docsPara, /<p class="[^"]*break-words[^"]*"/, "a docs paragraph lost its word-breaking class");
  assert.equal(
    visible(docsPara).replace(/\s+/g, " ").trim(),
    "Perenggan satu. Perenggan dua.",
    "a docs paragraph lost its text",
  );
  // The list branch carries its own copy of the class, so it gets its own assertion: a
  // hard-wrapped bullet ("… the couple gets a page\n  with a title …") was ragged too.
  const docsList = docs("- Item satu\n  bersambung\n- Item dua");
  assert.equal([...docsList.matchAll(/<li[^>]*>/g)].length, 2, "a docs list lost an item");
  assert.doesNotMatch(docsList, /whitespace-pre-wrap/, "a docs list item still preserves the source's hard wrap");
});

test("an empty answer renders nothing rather than an empty bubble", () => {
  assert.equal(render(""), "");
  assert.equal(render("   \n  "), "");
});

// ── 3. and 4. security ───────────────────────────────────────────────────────

test("HTML in the text is characters, never elements (no <script>, no <img>)", () => {
  const payload = '**<img src=x onerror=alert(1)>**';
  const html = render(payload);
  assert.ok(!html.includes("<img"), `an <img> element was created from model text: ${html}`);
  // Escaped, i.e. it is a character sequence inside the paragraph — not a tag and
  // not an attribute. (`onerror=` may appear as TEXT; what must not appear is a
  // real tag carrying it, which the <img check above covers.)
  assert.match(html, /&lt;img src=x onerror=alert\(1\)&gt;/, "the payload text was not escaped");
  assert.match(
    html,
    /<strong[^>]*>&lt;img src=x onerror=alert\(1\)&gt;<\/strong>/,
    "the payload was not escaped inside <strong>",
  );
  // It is still shown, as text: hiding it would be its own defect.
  assert.match(visible(html), /<img src=x onerror=alert\(1\)>/, "the offending text was silently dropped");

  const script = render('cuba <script>alert("xss")</script> ini');
  assert.ok(!script.includes("<script"), `a <script> element was created from model text: ${script}`);
  assert.match(script, /&lt;script&gt;alert\(&quot;xss&quot;\)&lt;\/script&gt;/, "the script text was not escaped");
  assert.match(visible(script), /<script>alert\("xss"\)<\/script>/, "the script text was silently dropped");
});

test("an unsafe link scheme stays text — javascript: never becomes an href", () => {
  const html = render("[klik](javascript:alert(1)) sini");
  assert.doesNotMatch(html, /href="javascript:/i, "a javascript: URL reached an href");
  assert.doesNotMatch(html, /<a[\s>]/, "an unsafe-scheme link was still rendered as an anchor");
  assert.match(visible(html), /klik/, "the link text was lost");

  for (const bad of ["data:text/html,<script>alert(1)</script>", "/relative/path", "ftp://example.com/x"]) {
    const out = render(`[x](${bad})`);
    assert.doesNotMatch(out, /<a[\s>]/, `a non-http(s) scheme became an anchor: ${bad}`);
  }
  // http is allowed alongside https (the repo's own public form URLs can be http
  // in local/dev deployments; the strict https-only rule is for <img src>).
  assert.match(render("[x](http://localhost:8080/f/abc)"), /href="http:\/\/localhost:8080\/f\/abc"/);
});

test("an image is reduced to its alt text (images are deliberately unsupported)", () => {
  const html = render("![panduan](https://example.com/a.png) di sini");
  assert.ok(!html.includes("<img"), "an image was rendered — the renderer does not support images");
  assert.match(visible(html), /panduan/, "the alt text was lost");
  assert.doesNotMatch(visible(html), /!\[/, "the image syntax is still visible");
});

// ── 5. the surfaces share one renderer ──────────────────────────────────────

test("both chat bubbles get their own tone, so code stays legible on each", () => {
  const assistant = render("guna `npm ci`", "assistant");
  const user = render("guna `npm ci`", "user");
  assert.match(assistant, /<code class="[^"]*bg-slate-200\/70[^"]*"/, "the assistant bubble's code chip lost its light background");
  assert.match(user, /<code class="[^"]*bg-white\/20 text-white[^"]*"/, "the user bubble's code chip is not readable on the indigo bubble");
  assert.notEqual(assistant, user, "the two bubbles render identically — one of them is illegible");
});

test("the chat page, /feedback and the /admin triage all use this one renderer", () => {
  // Comments blanked (t_9b40f272): the claims below are about what the page
  // RENDERS, and a call site moved into a `//` line or a JSX comment used to
  // satisfy them. Measured on main 81946afc: the chat bubble's `<Markdown …/>`
  // replaced by the raw `{m.content}` this test exists to prevent, the call site
  // left verbatim in a JSX comment, kept this file GREEN (27 pass / 0 fail).
  const chat = codeOnly(read("../app/chat/page.tsx"));
  const feedback = codeOnly(read("../app/feedback/page.tsx"));
  const admin = codeOnly(read("../app/admin/page.tsx"));
  for (const [name, src] of [
    ["chat/page.tsx", chat],
    ["feedback/page.tsx", feedback],
    ["admin/page.tsx", admin],
  ] as const) {
    assert.match(src, /from "\.\.\/\.\.\/components\/Markdown"/, `${name} does not import the shared renderer`);
  }
  // The original defect, pinned at the call site: the chat transcript must not
  // go back to rendering the message object's raw text as the bubble's only
  // child (the `>{m.content}<` shape that produced raw `**` on screen).
  assert.doesNotMatch(
    chat,
    />\s*\{m\.content\}\s*</,
    "chat/page.tsx renders {m.content} raw again — that is the reported defect (raw ** on screen)",
  );
  assert.match(chat, /<Markdown[\s\S]{0,200}?text=\{m\.content\}/, "the chat bubble does not render the message through Markdown");
  assert.doesNotMatch(
    feedback,
    />\s*\{m\.body\}\s*</,
    "feedback/page.tsx renders {m.body} raw again",
  );
  assert.match(feedback, /<Markdown[\s\S]{0,200}?text=\{m\.body\}/, "the feedback thread body does not render through Markdown");
  assert.doesNotMatch(admin, />\s*\{m\.body\}\s*</, "the admin triage view renders {m.body} raw again");
  assert.match(admin, /<Markdown[\s\S]{0,200}?text=\{m\.body\}/, "the admin triage body does not render through Markdown");
  // Exactly one renderer in the tree: a second copy is how the two surfaces drift.
  assert.match(feedback, /components\/Markdown/, "the feedback page does not import the shared component");
});

// ── THE SAME CLAIM, AGAINST THE COMMENT HOLE (t_9b40f272) ────────────────────
//
// A guard satisfied by its own comment is satisfied by nothing. The proof drives the
// IDENTICAL pattern the test above uses, over the real page with the chat bubble
// reverted to the shape that shipped the defect: the raw `{m.content}` rendered, the
// `<Markdown …/>` call site left verbatim in a JSX comment. Both halves are asserted
// — the RAW source still satisfies the pattern (so this is the hole, not a deletion)
// and the comment-blanked source does not.
test("BITES: a commented-out <Markdown/> call cannot satisfy the shared-renderer guard", () => {
  const RENDERS_MARKDOWN = /<Markdown[\s\S]{0,200}?text=\{m\.content\}/;
  const shipped = read("../app/chat/page.tsx");
  assert.ok(
    RENDERS_MARKDOWN.test(codeOnly(shipped)),
    "the shipped chat page does not satisfy the pattern this proof mutates — re-anchor it",
  );

  const SITE = '<Markdown text={m.content} variant={m.role === "user" ? "user" : "assistant"} />';
  assert.ok(shipped.includes(SITE), "the chat bubble's Markdown call moved — re-anchor this proof");
  const mutated = shipped.replace(
    SITE,
    '{m.content}{/* <Markdown text={m.content} variant={m.role === "user" ? "user" : "assistant"} /> */}',
  );
  assert.ok(
    RENDERS_MARKDOWN.test(mutated),
    "the mutation removed the call instead of commenting it out — it would measure a deletion",
  );
  assert.ok(
    !RENDERS_MARKDOWN.test(codeOnly(mutated)),
    "the guard is still satisfied with the bubble rendering raw text and the call site left in a " +
      "comment — the reported defect (literal ** on screen) would ship green",
  );
});

test("the prompt tells the model the subset the renderer supports (the renderer pins the result)", () => {
  // The UI half and the prompt half are one feature: the renderer guarantees the
  // output, the prompt stops the model from emitting what the renderer drops
  // (tables, raw HTML) so answers do not silently lose structure.
  const prompt = read("../../../api/internal/chat/prompt.go");
  assert.match(prompt, /HOW TO ANSWER/, "the prompt's answer rules moved — re-anchor this guard");
  assert.match(prompt, /\*\*bold\*\*/, "the prompt no longer states the supported formatting subset");
  assert.match(prompt, /[Nn]ever (?:use|output|write).{0,60}HTML|[Dd]o not use HTML/, "the prompt no longer forbids raw HTML");
  assert.match(prompt, /table/i, "the prompt no longer forbids tables");
  assert.match(
    prompt,
    /language the user wrote|in Malay, answer/,
    "the language-matching rule was removed while adding the formatting rule",
  );
});

test("the stored text is the raw model text — nothing is cleaned on write", () => {
  // The card requires the raw text to survive, so a renderer fix needs no
  // migration and no data is lost. If a server-side "clean-up" appears, this
  // fails and the decision gets made deliberately instead of by accident.
  //
  // The reply is now written in TWO places: the wire response and the stored
  // assistant turn (chat history, t_6b36e0d7). Both must carry `res.Reply`
  // untouched — the first is what the page renders, the second is what a
  // restored conversation renders, so cleaning either one silently changes the
  // transcript. chatAnswer (rest_chat_sessions.go) is the one builder the
  // one-shot path, the streamed `done` frame and the store all go through.
  const rest = read("../../../api/internal/httpapi/rest_chat.go");
  const history = read("../../../api/internal/httpapi/rest_chat_sessions.go");
  assert.match(
    history,
    /Reply:\s*res\.Reply/,
    "the chat reply is no longer passed straight through — re-anchor this guard",
  );
  assert.match(
    history,
    /"assistant",\s*res\.Reply/,
    "the stored assistant turn is no longer the raw reply — a restored conversation would differ from the one the user read",
  );
  for (const pattern of [/stripMarkdown/i, /sanitizeMarkdown/i, /normalizeMarkdown/i]) {
    assert.doesNotMatch(rest, pattern, "rest_chat.go now cleans the reply before sending it");
    assert.doesNotMatch(history, pattern, "rest_chat_sessions.go now cleans the reply before storing it");
  }
});

test("the docs page documents what is rendered, so the behaviour is discoverable", () => {
  // DOCUMENTATION PARITY (AGENTS.md): a rendered-format feature that is not
  // documented is a surprise. The chat docs carry a section for it, and so does
  // the feedback docs (thread bodies render the same way).
  const chatDocs = read("../app/docs/chat/page.tsx");
  assert.match(chatDocs, /id: "formatting"/, "the chat docs lost the formatting section");
  assert.match(chatDocs, /strong|bold/, "the formatting section no longer names what is rendered");
  const fbDocs = read("../app/docs/feedback/page.tsx");
  assert.match(fbDocs, /id: "formatting"/, "the feedback docs do not mention that a thread body is rendered");
});

test("parseMarkdown/parseInline expose the structure the assertions above rely on", () => {
  // A direct read of the parser, so a change to the block/inline split shows up
  // here rather than only as a mysterious HTML diff.
  const blocks = parseMarkdown("- satu\n- dua\n\n```\ncode\n```\n\npara");
  assert.deepEqual(blocks.map((b) => b.kind), ["list", "code", "p"]);
  const inline = parseInline("a **b** `c` [d](https://e.com)");
  assert.deepEqual(inline.map((n) => n.t), ["text", "strong", "text", "code", "text", "link"]);
});
