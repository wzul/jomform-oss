// Repo-wide guard: a BASE text token must not be painted on a TINTED surface.
//
// WHAT WAS WRONG (measured by axe on a production build, not a source grep)
//   Tailwind's `bg-indigo-50` (#eef2ff) is the app's "active / selected / hover"
//   surface. The two base text tokens do not clear WCAG AA on it:
//     --jf-primary (#5e6ad2) = 4.20:1
//     --jf-muted   (#6b7280) = 4.32:1
//   both under the 4.5:1 floor for the 14px/12px text these chips and buttons
//   use. They DO pass on white (4.70:1 / 4.83:1), which is exactly why the
//   defect hides: the same class list is fine until the tint appears.
//
//   Found in three rounds, each time in a file the previous guard did not own:
//     1. the sidebar nav row            (Shell.tsx, PR 667)
//     2. the workspace switcher's item  (WorkspaceSwitcher.tsx)
//     3. the page-level chips           (this file's card): the builder's
//        form-mode toggle at 4.2:1 and ProductEditor's Simple/Variable chip at
//        4.2:1, both axe-confirmed `impact: serious`, plus ~14 more sites.
//   A rule scoped to a hand-listed set of files is what let this class survive
//   three rounds, so this guard walks the whole `web/src` tree. Adding a new
//   page cannot add an unguarded site — with ONE exception, and it is now a
//   LOUD failure rather than a skip (card t_10266084):
//
//     A class list can be built by a local helper
//     (`className={`base ${tint(active)}`}`), a local constant
//     (`className={chipCls}`), a template, a ternary, a `+` chain, an array
//     joined with `.join(" ")`, or an inline class map read by key
//     (`({ paid: "…" })[status]`). `resolveClassExpr` in `./jsxSource.ts`
//     resolves ALL of those. Before it did, each one came back as an empty or
//     partial class list and the element was SKIPPED — this guard was green on a
//     tree that rendered the defect, and a sabotage built through a helper was
//     proven green while the identical defect written as an inline literal went
//     red.
//
//     What the reader still cannot read is a class list THIS FILE does not own:
//     a `className` PROP the caller fills in, and a prop-derived ternary. Those
//     sites are pinned by NAME in `UNREADABLE_PIN` below, with the reason, and
//     any NEW unreadable className — a sixth site, or a shape the reader cannot
//     follow — fails the guard instead of joining a blind spot.
//
//   A FOURTH round came at the same defect from a different surface and is NOT
//   covered by this file (card t_5f315891): the builder canvas price spans use
//   the base `--jf-primary` on `bg-slate-50` (#f8fafc), where it measures
//   4.492:1 — 0.008 under the floor, while the same token on white measures
//   4.700:1. This guard cannot own it for two reasons: the tint it looks for is
//   indigo-50, and a source scan cannot measure a margin that small (the whole
//   question is the ratio, not the class list). It is guarded instead by
//   `web/e2e/builder-canvas-price-contrast.spec.ts`, which measures the ratio in
//   a real browser. If a fifth round appears on another surface, prefer a
//   measured guard over a class-name rule: this file is the fallback for
//   surfaces the browser gate cannot reach.
//
// THE FIX IS THE TOKEN, NOT THE CLASS
//   `--jf-primary-on-tint` (#4338ca) measures 7.07:1 on the tint and
//   `--jf-muted-on-tint` (#5b6472) 5.35:1; both also pass OFF the tint
//   (7.90:1 / 5.98:1 on white), so a site whose tint is conditional can use them
//   unconditionally. Do NOT darken `--jf-primary` itself — that was rejected as
//   a decision because it shifts every other use of the token.
//
// SCOPE OF *THIS* RULE
//   Only SOLID tints (`bg-indigo-50`) are flagged. The translucent variants
//   (`bg-indigo-50/40|/50|/60`) composite over white to #f8faff / #f7f9ff /
//   #f5f7ff, where --jf-primary measures 4.50 / 4.46 / 4.39:1 — i.e. at the
//   floor rather than clearly under it, and one of the three actually passes.
//   A source guard cannot know what is behind a translucent tint, so flagging
//   them here would be a guess; they are measured per site in the browser
//   instead. `bg-indigo-100` (#e0e7ff) is likewise a DIFFERENT, darker tint
//   (3.81:1 with --jf-primary) and is tracked separately, not folded in.
//
// WHY A SOURCE GUARD AND NOT THE BROWSER GATE
//   `web/e2e/a11y.spec.ts` scans PUBLIC routes only (/login, /landing,
//   /reset-password, /magic-link, /brand, /legal/*). Every page above is behind
//   a session, so `npm run a11y` cannot reach any of them. The EXECUTED proof is
//   the axe run quoted on the card; this is the part that stops it coming back.
//
// The class-list / JSX-shape parser (`codeOnly`, `jsxTags`, `tsxFiles`, …) is
// shared from `web/src/lib/jsxSource.ts`. It used to be a byte-identical copy of
// the one in `mutedText.test.ts`, and that copy was not harmless: the duplicated
// `codeOnly()` read a `/`+`*` or `/`+`/` sitting in JSX TEXT as a comment opener,
// so `web/src/app/docs/api/page.tsx:175` (`... <code>/v1/rest/*</code>`) made it
// blank 41 lines of that page — including the `</code>` that closes the tag. The
// fix lives in the module; this file now uses it instead of its own copy.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { codeOnly, jsxTags, tsxFiles, tintedSurfaceFindings } from "./jsxSource.ts";

const SRC = fileURLToPath(new URL("../", import.meta.url)); // web/src/

// The RULE itself — which base tokens a class list paints on a SOLID
// `bg-indigo-50` tint, per state, and the (a) same-element / (b) inherited
// findings it produces — lives in `./jsxSource.ts` as `tintedSurfaceFindings`.
// It used to be defined here AND, in a flattened state-UNaware form, in
// `sidebarNav.test.ts`; the two then disagreed about one class list (card
// t_306dcc1e). One implementation, called by both guards, is the property that
// makes that impossible rather than merely fixed.

const FILES = tsxFiles(SRC);

/**
 * The `className` expressions the reader could NOT resolve, and why each one is
 * a deliberate exception rather than a blind spot (card t_10266084).
 *
 * A guard that passes over an element it cannot read is the "silently matches
 * nothing" failure shape the test below exists to prevent — at whole-tree
 * granularity the second test catches it, but an element the scanner skipped
 * still satisfies "the walk found files and tints". Pinning the exceptions turns
 * that into a per-site question: every unreadable className is on this list with
 * a reason, and a NEW one FAILS.
 *
 * Keyed on `<file> :: <expression>` and NOT on a line number, so an edit
 * anywhere above one of these does not turn the guard red, while an edit to the
 * expression itself does — at which point either the reader can now see it
 * (delete the pin) or the shape moved and the pin has to be re-read. Both
 * directions are asserted.
 */
const UNREADABLE_PIN: Array<{ rel: string; expr: string; why: string }> = [
  {
    rel: "app/products/page.tsx",
    expr: "className",
    why:
      "the sortable table header's `<th className={className}>`: a PROP, so the " +
      "caller owns the class list and this file cannot judge it",
  },
  {
    rel: "components/OperatorNotice.tsx",
    expr: "className",
    why:
      "`<p className={className}>`: a PROP (the legal-notice paragraph takes the " +
      "caller's classes), so the list is chosen outside this file",
  },
  {
    rel: "components/SaveButton.tsx",
    expr: "saved ? savedClassName : className",
    why:
      "both branches are PROPS (`savedClassName`, `className`) — the saved/resting " +
      "pair is the caller's, and a scan of this file cannot see either",
  },
  {
    rel: "components/Shell.tsx",
    expr: "`${className} flex items-center justify-center rounded-full bg-indigo-100 text-xs font-semibold text-[var(--jf-primary-on-tint)]`",
    why:
      "the avatar's own classes ARE read here (that is the `bg-indigo-100` half, " +
      "which is not this guard's tint and is carded separately); the `className` " +
      "PROP the caller passes is what the read cannot complete. Re-read by card " +
      "`t_44c9a306` when it re-cut PR 1102 onto current `main`: card `t_86755e43` " +
      "moved this site's own token to `--jf-primary-on-tint` (a DARKER tint is " +
      "tracked by `src/lib/indigo100Tint.test.ts`), which changed the literal here " +
      "and therefore moved the pin key — the shape did not move, so the pin is " +
      "kept with the new text rather than deleted",
  },
  {
    rel: "components/WorkspaceSwitcher.tsx",
    expr: "`relative ${fullWidth ? \"w-full\" : \"\"} ${className || \"\"}`",
    why:
      "the static half and the width branch are read; the `className` PROP the " +
      "caller fills in is not",
  },
];

/** Every `className` under the tree that the reader could not fully resolve. */
function unreadableSites(): Array<{ rel: string; line: number; tag: string; expr: string }> {
  const out: Array<{ rel: string; line: number; tag: string; expr: string }> = [];
  for (const rel of FILES) {
    const code = codeOnly(readFileSync(`${SRC}${rel}`, "utf8"));
    for (const e of jsxTags(code)) {
      if (!e.open || !e.tag.classNameUnreadable) continue;
      out.push({
        rel,
        line: code.slice(0, e.tag.openIdx).split("\n").length,
        tag: e.tag.name,
        expr: e.tag.classNameExpr,
      });
    }
  }
  return out;
}

function allFindings(): string[] {
  const out: string[] = [];
  for (const rel of FILES) {
    out.push(...tintedSurfaceFindings(rel, readFileSync(`${SRC}${rel}`, "utf8")));
  }
  return out;
}

test("no base text token is painted on a tinted surface (repo-wide)", () => {
  const findings = allFindings();
  assert.deepEqual(
    findings,
    [],
    `Text on a bg-indigo-50 surface must use the on-tint tokens ` +
      `(--jf-primary-on-tint / --jf-muted-on-tint). --jf-primary measures 4.20:1 and ` +
      `--jf-muted 4.32:1 on #eef2ff (#eef2ff is Tailwind indigo-50), both under the ` +
      `4.5:1 AA floor for the 12px/14px text these chips and buttons use, while ` +
      `#4338ca / #5b6472 measure 7.07:1 / 5.35:1:\n  ${findings.join("\n  ")}`,
  );
});

test("no className is silently skipped: every unreadable one is a pinned exception", () => {
  // An element the reader cannot read is NOT an element it has cleared, so a
  // guard that walks past one reports a clean tree it did not measure (card
  // t_10266084: a defect built through a local helper was proven GREEN while the
  // identical defect written inline went RED).
  const found = unreadableSites();
  const key = (rel: string, expr: string) => `${rel} :: ${expr}`;
  const pinned = new Set(UNREADABLE_PIN.map((p) => key(p.rel, p.expr)));
  const unexpected = found.filter((f) => !pinned.has(key(f.rel, f.expr)));
  assert.deepEqual(
    unexpected.map((f) => `${f.rel}:${f.line} — <${f.tag}> className={${f.expr}}`),
    [],
    `This guard COULD NOT READ the className of the element(s) below, and an element ` +
      `it cannot read is not one it has judged. Resolve the shape in \`./jsxSource.ts\` ` +
      `(\`resolveClassExpr\`: a template, a ternary, a \`+\` chain, an array, an inline ` +
      `class map, a local const and a local helper are all readable), or — when the ` +
      `class list genuinely belongs to the CALLER, as a className prop does — pin it in ` +
      `UNREADABLE_PIN with the reason. Unreadable sites found:\n  ` +
      unexpected
        .map((f) => `${f.rel}:${f.line} — <${f.tag}> className={${f.expr}}`)
        .join("\n  "),
  );

  // The other direction: a pin that no longer matches hides the NEXT site that
  // lands on the same shape, so a stale pin is a failure too.
  const seen = new Set(found.map((f) => key(f.rel, f.expr)));
  const stale = UNREADABLE_PIN.filter((p) => !seen.has(key(p.rel, p.expr)));
  assert.deepEqual(
    stale.map((p) => `${p.rel} :: ${p.expr}`),
    [],
    `These entries are pinned as UNREADABLE and the walk no longer finds them: either the ` +
      `expression changed shape (re-read it and update the pin) or the reader resolves it ` +
      `now (delete the pin). A stale pin is how the next blind spot would look green.`,
  );
});

test("the shapes that used to be skipped are all caught (in-file bite proof)", () => {
  // Card t_10266084. Every fixture below renders a BASE token on a SOLID
  // bg-indigo-50 tint, in a shape the reader used to return as an empty or
  // partial class list — so the guard passed over the element and reported a
  // clean tree. A defect built through a helper was proven GREEN while the
  // identical defect written as an inline literal went RED; these are the
  // shapes, not a description of them, so a reader regression is caught here
  // even on a tree that happens to hold no helper-built tint today.
  const cases: Array<{ name: string; src: string[] }> = [
    {
      name: "a local helper call",
      src: [
        "const tintCls = (on: boolean) =>",
        '  on ? "bg-indigo-50 text-[var(--jf-primary)]" : "bg-white";',
        "export function P() {",
        "  return <button className={`rounded px-2 ${tintCls(true)}`}>x</button>;",
        "}",
      ],
    },
    {
      name: "a local const",
      src: [
        'const chipCls = "bg-indigo-50 text-[var(--jf-primary)]";',
        "export function P() {",
        "  return <span className={chipCls}>x</span>;",
        "}",
      ],
    },
    {
      name: "a template with no island",
      src: [
        "export function P() {",
        "  return <span className={`bg-indigo-50 text-[var(--jf-primary)]`}>x</span>;",
        "}",
      ],
    },
    {
      // Also pins the className VALUE extraction: a ternary in the container is
      // followed here by another attribute, and reading the whole attribute list
      // instead of the value made this element unreadable.
      name: "a ternary in the container, with an attribute after it",
      src: [
        "export function P() {",
        "  return (",
        "    <span",
        '      className={active ? "bg-indigo-50 text-[var(--jf-primary)]" : "bg-white"}',
        '      title="x"',
        "    >",
        "      x",
        "    </span>",
        "  );",
        "}",
      ],
    },
    {
      name: "a `+` chain",
      src: [
        "export function P() {",
        '  return <span className={"bg-indigo-50 " + (active ? "text-[var(--jf-primary)]" : "")}>x</span>;',
        "}",
      ],
    },
    {
      name: "an array joined with .join(\" \")",
      src: [
        "export function P() {",
        '  return <span className={["bg-indigo-50", active ? "text-[var(--jf-primary)]" : "text-white"].join(" ")}>x</span>;',
        "}",
      ],
    },
    {
      name: "an inline class map read by key",
      src: [
        "const badge = (s: string) =>",
        '  ({ paid: "bg-indigo-50 text-[var(--jf-primary)]", other: "bg-white" })[s] || "bg-white";',
        "export function P() {",
        "  return <span className={`rounded ${badge(s)}`}>x</span>;",
        "}",
      ],
    },
    {
      name: "a switch helper's returns",
      src: [
        "function badge(s: string) {",
        "  switch (s) {",
        '    case "paid":',
        '      return "bg-indigo-50 text-[var(--jf-primary)]";',
        "    default:",
        '      return "bg-white";',
        "  }",
        "}",
        "export function P() {",
        "  return <span className={`rounded ${badge(s)}`}>x</span>;",
        "}",
      ],
    },
  ];

  const missed: string[] = [];
  for (const c of cases) {
    const found = tintedSurfaceFindings("fixture.tsx", c.src.join("\n"));
    if (!found.some((f) => f.includes("paints text-[var(--jf-primary)] on bg-indigo-50"))) {
      missed.push(`${c.name} — reported ${JSON.stringify(found)}`);
    }
  }
  assert.deepEqual(
    missed,
    [],
    `these shapes render the defect and the guard did not report it (an element it did not ` +
      `parse is not an element it cleared):\n  ${missed.join("\n  ")}`,
  );

  // The other direction, so "everything is reported" cannot be the way this
  // passes: the pair is SAFE when the hover state overrides the text (this is
  // the outline-button shape the rule's own comment describes), and a ternary
  // whose branches never co-paint is not a finding either.
  const negatives: Array<{ name: string; src: string[] }> = [
    {
      name: "hover tint with a hover text override",
      src: [
        "export function P() {",
        '  return <button className="text-[var(--jf-primary)] hover:bg-indigo-50 hover:text-[var(--jf-primary-on-tint)]">x</button>;',
        "}",
      ],
    },
    {
      name: "a ternary whose branches never co-paint",
      src: [
        "export function P() {",
        '  return <span className={active ? "text-[var(--jf-primary)]" : "bg-indigo-50"}>x</span>;',
        "}",
      ],
    },
  ];
  const wrong: string[] = [];
  for (const c of negatives) {
    const found = tintedSurfaceFindings("fixture.tsx", c.src.join("\n"));
    if (found.length > 0) wrong.push(`${c.name} — ${JSON.stringify(found)}`);
  }
  assert.deepEqual(wrong, [], `these are NOT defects and must not be reported:\n  ${wrong.join("\n  ")}`);
});

test("the repo-wide rule is looking at real, reachable markup", () => {
  // The guard above passes on an empty file list, so pin that the walk found the
  // app, that tinted surfaces still exist in it, and that the on-tint token is
  // still what those surfaces use. Without this, a broken walk (or a refactor
  // that drops the tint entirely) reads as a clean pass.
  assert.ok(
    FILES.length >= 40,
    `the walk found only ${FILES.length} .tsx files under web/src — it is not reading the app`,
  );
  assert.ok(
    FILES.includes("components/Shell.tsx") && FILES.includes("app/builder/page.tsx"),
    `the walk is missing core files (found ${FILES.length})`,
  );

  const tintedFiles = FILES.filter((rel) => {
    const code = codeOnly(readFileSync(`${SRC}${rel}`, "utf8"));
    return [...code.matchAll(/(?:^|[\s"'`])bg-indigo-50(?![\w/.-])/g)].length > 0;
  });
  assert.ok(
    tintedFiles.length >= 8,
    `only ${tintedFiles.length} files paint a solid bg-indigo-50 tint — if the tinted ` +
      `surface moved or the tint class changed, re-anchor this guard rather than deleting it`,
  );

  // The rule above is also green for an element whose class list came back EMPTY,
  // so count what the reader RESOLVED. A reader that stopped following a shape
  // (a template, a helper call, a ternary) would leave every rule judging fewer
  // elements than this file claims, which is the failure card t_10266084 exists
  // to make impossible. Measured on the tree this was written against:
  // 3228 resolved of 3233 className attributes.
  let resolved = 0;
  for (const rel of FILES) {
    const code = codeOnly(readFileSync(`${SRC}${rel}`, "utf8"));
    for (const e of jsxTags(code)) {
      if (e.open && e.tag.classNameExpr !== "" && !e.tag.classNameUnreadable) resolved += 1;
    }
  }
  assert.ok(
    resolved >= 3000,
    `the reader resolved only ${resolved} className attributes under web/src — a shape it ` +
      `used to follow is no longer followed, and every rule above is judging fewer elements ` +
      `than it claims (a skipped element is not a cleared one)`,
  );

  const globals = readFileSync(`${SRC}app/globals.css`, "utf8");
  assert.match(
    globals,
    /--jf-primary-on-tint:\s*#4338ca/,
    `globals.css lost --jf-primary-on-tint — the AA-safe token the tinted surfaces need`,
  );
  assert.match(
    globals,
    /--jf-muted-on-tint:\s*#5b6472/,
    `globals.css lost --jf-muted-on-tint — the AA-safe token a tinted surface's muted line needs`,
  );
});
