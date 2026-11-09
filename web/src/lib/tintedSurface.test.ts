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
//     sites are pinned by NAME **and COUNT** in `UNREADABLE_PIN` below, with the
//     reason, and any NEW unreadable className fails the guard instead of
//     joining a blind spot — a sixth site, a SECOND one on an expression that is
//     already pinned (card t_f5641ea5: the key used to be `<file> :: <expr>`
//     with no count, so a duplicate site rode in on an existing pin), or a shape
//     the reader cannot follow.
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
 * Keyed on `<file> :: <expression>` — deliberately NOT on a line number, so an
 * edit anywhere above one of these does not turn the guard red, while an edit to
 * the expression itself does: at which point either the reader can now see it
 * (delete the pin) or the shape moved and the pin has to be re-read. Both
 * directions are asserted.
 *
 * ...AND A COUNT (card t_f5641ea5). `<file> :: <expr>` alone does not name a
 * SITE, so a file that was already pinned could grow a SECOND unreadable
 * `className` with the same expression — the filter dropped both and the guard
 * stayed green while a new site joined the blind spot. Proven before the count
 * existed: one extra `<div className={className}>` in `components/OperatorNotice.tsx`
 * left this file 4/4 GREEN, while the same element in an UNPINNED file went RED.
 * Every pinned expression today is `className` or a prop-derived ternary, i.e.
 * exactly the shape a new prop-forward reuses. So each entry carries the number
 * of sites that key may cover, and a key that now has MORE sites than its pin
 * allows is a failure (`pinVerdict(...).extra`). Lowering a count is the same
 * kind of deliberate edit as adding a pin: it is how the next site is accepted,
 * in the open, instead of by accident.
 *
 * Measured on the tree this was written against: 5 unreadable sites in 5 files,
 * every one a distinct key with exactly ONE site — hence every `count: 1` here.
 */
const UNREADABLE_PIN: UnreadablePin[] = [
  {
    rel: "app/products/page.tsx",
    expr: "className",
    count: 1,
    why:
      "the sortable table header's `<th className={className}>`: a PROP, so the " +
      "caller owns the class list and this file cannot judge it",
  },
  {
    rel: "components/OperatorNotice.tsx",
    expr: "className",
    count: 1,
    why:
      "`<p className={className}>`: a PROP (the legal-notice paragraph takes the " +
      "caller's classes), so the list is chosen outside this file",
  },
  {
    rel: "components/SaveButton.tsx",
    expr: "saved ? savedClassName : className",
    count: 1,
    why:
      "both branches are PROPS (`savedClassName`, `className`) — the saved/resting " +
      "pair is the caller's, and a scan of this file cannot see either",
  },
  {
    rel: "components/Shell.tsx",
    expr: "`${className} flex items-center justify-center rounded-full bg-indigo-100 text-xs font-semibold text-[var(--jf-primary-on-tint)]`",
    count: 1,
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
    count: 1,
    why:
      "the static half and the width branch are read; the `className` PROP the " +
      "caller fills in is not",
  },
  // THE PRIMITIVE LAYER (card t_f2e231d3). Every entry below is the same shape and
  // the same reason, and it is the one the paragraph above sanctions: the class
  // list genuinely belongs to the CALLER, because a primitive's whole contract is
  // "the base look from tokens, plus whatever the site adds". `className` is a
  // prop, and a prop is not resolvable by construction — so these sites are
  // pinned rather than rewritten into a shape the reader can follow. What the
  // reader DOES resolve is the static half of each list, which is where a base
  // text token on a tint would have to appear.
  {
    rel: "app/design/page.tsx",
    expr: "`${step.utility} block text-[var(--jf-text)]`",
    count: 1,
    why:
      "the type-scale sample: the size utility comes from the `TYPE_SCALE` row being " +
      "rendered, so the class list belongs to the data table, not to this element",
  },
  {
    rel: "components/ui/Alert.tsx",
    expr:
      "[ \"flex gap-jf-3 rounded-jf-md border px-jf-4 py-jf-3 text-jf-read-sm\", t.box, className, ] .filter(Boolean) .join(\" \")",
    count: 1,
    why: "the tone box is picked from `tone`; the caller's `className` is merged in",
  },
  {
    rel: "components/ui/Button.tsx",
    expr: "cls",
    count: 2,
    why:
      "`cls` is BASE + the variant and size class maps + the caller's `className`: the " +
      "static BASE and both maps are read, the prop is not (it covers the <a> and the <button>)",
  },
  {
    rel: "components/ui/EmptyState.tsx",
    expr:
      "[\"flex flex-col items-center gap-jf-2 px-jf-6 py-jf-12 text-center\", className] .filter(Boolean) .join(\" \")",
    count: 1,
    why: "a fixed centred column plus the caller's `className`",
  },
  {
    rel: "components/ui/Field.tsx",
    expr: "[\"flex flex-col gap-jf-1\", className].filter(Boolean).join(\" \")",
    count: 1,
    why: "the field's column plus the caller's `className`",
  },
  {
    rel: "components/ui/Field.tsx",
    expr:
      "[\"text-jf-read-sm font-medium text-[var(--jf-text)]\", labelClassName].filter(Boolean).join(\" \")",
    count: 1,
    why: "the label's own type token plus the caller's `labelClassName`",
  },
  {
    rel: "components/ui/Select.tsx",
    expr:
      "[CONTROL_CLASS, \"appearance-none pr-jf-8\", className].filter(Boolean).join(\" \")",
    count: 1,
    why: "`CONTROL_CLASS` is read; the caret padding is static; the caller's `className` is not",
  },
  {
    rel: "components/ui/Table.tsx",
    expr: "className",
    count: 1,
    why: "`<tr className={className}>`: the row's classes are the caller's (a table row has no base look)",
  },
  {
    rel: "components/ui/Tabs.tsx",
    expr: "className",
    count: 1,
    why: "the tablist `<div className={className}>`: the list's layout belongs to the page",
  },
  {
    rel: "components/ui/Tabs.tsx",
    expr: "[\"pt-jf-4\", panelClassName].filter(Boolean).join(\" \")",
    count: 1,
    why: "the panel's top rhythm plus the caller's `panelClassName`",
  },
];

/**
 * One `className` the reader could not fully resolve, at a known site.
 * `line` is where the element's `<` sits, so a failure names the site the way a
 * reviewer has to look at it. It is NEVER part of a key: a pin has to survive an
 * edit anywhere above it, which is exactly why the key is `<file> :: <expr>` —
 * and exactly why that key needs the count below.
 */
type UnreadableSite = { rel: string; line: number; tag: string; expr: string };

/** A deliberate exception: ONE expression, in ONE file, covering `count` sites. */
type UnreadablePin = { rel: string; expr: string; count: number; why: string };

/** The key a pin and a finding are matched on: the file and the expression. */
function siteKey(rel: string, expr: string): string {
  return `${rel} :: ${expr}`;
}

/** Every `className` the reader could not resolve in ONE file's source. */
function unreadableSitesIn(rel: string, src: string): UnreadableSite[] {
  const code = codeOnly(src);
  const out: UnreadableSite[] = [];
  for (const e of jsxTags(code)) {
    if (!e.open || !e.tag.classNameUnreadable) continue;
    out.push({
      rel,
      line: code.slice(0, e.tag.openIdx).split("\n").length,
      tag: e.tag.name,
      expr: e.tag.classNameExpr,
    });
  }
  return out;
}

/** Every `className` under the tree that the reader could not fully resolve. */
function unreadableSites(): UnreadableSite[] {
  const out: UnreadableSite[] = [];
  for (const rel of FILES) out.push(...unreadableSitesIn(rel, readFileSync(`${SRC}${rel}`, "utf8")));
  return out;
}

/** `<rel>:<line> — <tag> className={<expr>}` — one site, as a reviewer reads it. */
function siteLabel(s: UnreadableSite): string {
  return `${s.rel}:${s.line} — <${s.tag}> className={${s.expr}}`;
}

/**
 * Compare the unreadable sites a walk found against the pins that permit them,
 * in the three directions this guard cares about (cards t_10266084, t_f5641ea5):
 *
 *   unpinned  a site whose `<file> :: <expr>` is not on the list at all — "an
 *             element I cannot read is not one I cleared".
 *   extra     a site BEYOND the count its key pins: a SECOND unreadable
 *             className riding in on an existing exception. This direction did
 *             not exist while the key carried no count, and the planted
 *             duplicate in the bite proof below passed 4/4 green without it.
 *   stale     a pin whose sites the walk no longer finds (fewer than `count`):
 *             the expression moved shape, or the reader resolves it now. A stale
 *             pin is how the NEXT blind spot would look green.
 *
 * Pure (no I/O), so the bite proof can drive it with a planted fixture instead of
 * a mutated tree.
 */
function pinVerdict(sites: UnreadableSite[], pins: UnreadablePin[]) {
  const allowed = new Map<string, number>();
  for (const p of pins) {
    const k = siteKey(p.rel, p.expr);
    allowed.set(k, (allowed.get(k) ?? 0) + p.count);
  }
  const found = new Map<string, UnreadableSite[]>();
  for (const s of sites) {
    const k = siteKey(s.rel, s.expr);
    const list = found.get(k);
    if (list) list.push(s);
    else found.set(k, [s]);
  }
  const unpinned: UnreadableSite[] = [];
  const extra: UnreadableSite[] = [];
  for (const [k, list] of found) {
    const allow = allowed.get(k);
    if (allow === undefined) {
      unpinned.push(...list);
      continue;
    }
    extra.push(...list.slice(allow));
  }
  const stale: Array<{ pin: UnreadablePin; found: number }> = [];
  for (const p of pins) {
    const n = found.get(siteKey(p.rel, p.expr))?.length ?? 0;
    if (n < p.count) stale.push({ pin: p, found: n });
  }
  return { unpinned, extra, stale };
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
  const { unpinned, extra, stale } = pinVerdict(found, UNREADABLE_PIN);
  assert.deepEqual(
    unpinned.map(siteLabel),
    [],
    `This guard COULD NOT READ the className of the element(s) below, and an element ` +
      `it cannot read is not one it has judged. Resolve the shape in \`./jsxSource.ts\` ` +
      `(\`resolveClassExpr\`: a template, a ternary, a \`+\` chain, an array, an inline ` +
      `class map, a local const and a local helper are all readable), or — when the ` +
      `class list genuinely belongs to the CALLER, as a className prop does — pin it in ` +
      `UNREADABLE_PIN with the reason and the COUNT of sites it covers. ` +
      `Unreadable sites found:\n  ${unpinned.map(siteLabel).join("\n  ")}`,
  );

  // Card t_f5641ea5. The key alone did not name a SITE: a file that was already
  // pinned could grow a SECOND unreadable className with the same expression,
  // both were filtered out, and the guard stayed green — a new site joining the
  // blind spot this list exists to bound. The count is what makes "a new
  // unreadable className fails" true per site and not merely per expression.
  assert.deepEqual(
    extra.map(siteLabel),
    [],
    `These unreadable classNames are the SECOND (or later) site for an expression that ` +
      `\`UNREADABLE_PIN\` already covers — a NEW blind spot riding in on an old exception, ` +
      `which is exactly what the pin's COUNT exists to stop. Either make the shape readable ` +
      `in \`./jsxSource.ts\`, or accept the extra site deliberately by RAISING that pin's ` +
      `\`count\` (and saying why in its \`why\`), or PIN it as its own entry when it is a ` +
      `different prop. Sites past their pin's count:\n  ${extra.map(siteLabel).join("\n  ")}`,
  );

  // The other direction: a pin that no longer matches hides the NEXT site that
  // lands on the same shape, so a stale pin is a failure too — including a pin
  // whose count is now higher than the sites that exist.
  assert.deepEqual(
    stale.map((s) => `${s.pin.rel} :: ${s.pin.expr} — pinned ${s.pin.count}, found ${s.found}`),
    [],
    `These entries are pinned as UNREADABLE and the walk finds too few sites for them: ` +
      `either the expression changed shape (re-read it and update the pin), the site was ` +
      `removed (lower the \`count\`), or the reader resolves it now (delete the pin). A ` +
      `stale pin is how the next blind spot would look green.`,
  );
});

test("a SECOND unreadable className in an already-pinned file fails (in-file bite proof)", () => {
  // Card t_f5641ea5. Before the pin carried a COUNT, the key `<file> :: <expr>`
  // named an expression and not a site, so this exact plant — ONE extra
  // `<div className={className}>` in an already-pinned file — was filtered out
  // twice and the whole file stayed 4/4 GREEN. Reproduced on a scratch copy of
  // the tree before this changed (same reader, same command: 4 pass / 0 fail),
  // while the SAME element in a file that is NOT pinned went red naming both
  // sites. So this is not a description of the gap, it is the gap: the plant
  // below is driven through `codeOnly` → `jsxTags` → `pinVerdict`, the real path
  // the guard takes, against the real pinned file.
  const rel = "components/OperatorNotice.tsx";
  const real = readFileSync(`${SRC}${rel}`, "utf8");
  const realSites = unreadableSitesIn(rel, real);
  assert.ok(
    realSites.length >= 1,
    `${rel} carries no unreadable className at all — this bite proof anchors on the file ` +
      `that IS pinned for one; re-read it rather than deleting the case`,
  );
  // The fixture pin covers exactly what the tree already has, so the PLANT is the
  // only thing below that can fail. (A hard-coded `count: 1` would make this a
  // second copy of the real pin list, free to disagree with it.)
  const pins: UnreadablePin[] = [
    {
      rel,
      expr: realSites[0].expr,
      count: realSites.length,
      why: "fixture: the sites this file already holds",
    },
  ];

  // Control — the tree as it stands is accepted. Without this, "the plant fails"
  // could be satisfied by a rule that fails everything (or by a fixture whose
  // pin never matched in the first place).
  assert.deepEqual(
    pinVerdict(realSites, pins),
    { unpinned: [], extra: [], stale: [] },
    `the pin built from this file's own sites does not accept them — the fixture, not ` +
      `the guard, is wrong`,
  );

  // The plant: a second element whose expression is character-for-character the
  // one the pin names, inserted straight after the pinned element — the exact
  // shape the card proved GREEN. Anchored on the pinned element's own closing
  // tag, so a comment above it or a new sibling elsewhere does not break this.
  const close = real.indexOf("</p>");
  assert.ok(
    close > 0 && real.split("</p>").length - 1 === 1,
    `${rel} no longer holds exactly one \`</p>\` — this plant anchors on the pinned ` +
      `element's own closing tag; re-read the plant instead of deleting the case`,
  );
  const planted =
    real.slice(0, close + 4) +
    "\n    <div className={className}>\n      <span>{t(OPERATOR_KEY)}</span>\n    </div>" +
    real.slice(close + 4);
  const plantIdx = planted.indexOf("<div className={className}>");
  assert.ok(plantIdx > close, `the plant did not land after the pinned element`);
  const plantLine = planted.slice(0, plantIdx).split("\n").length;

  const plantedVerdict = pinVerdict(unreadableSitesIn(rel, planted), pins);
  assert.deepEqual(
    plantedVerdict.unpinned.map(siteLabel),
    [],
    `the planted site carries the SAME expression as the existing pin, so the old ` +
      `key-alone filter dropped it as well — it must be reported as a site past the ` +
      `pin's count, not as unpinned`,
  );
  assert.deepEqual(
    plantedVerdict.extra.map(siteLabel),
    [`${rel}:${plantLine} — <div> className={className}`],
    `the SECOND unreadable className planted in an already-pinned ${rel} was accepted: ` +
      `this is the card's defect (a new site riding in on an old pin)`,
  );
  assert.deepEqual(plantedVerdict.stale, [], `the original pinned site is still there`);

  // Raising the count is how a duplicate site is accepted on purpose, so the
  // failure above is "the pin was not widened", not a blanket ban on duplicates.
  assert.deepEqual(
    pinVerdict(unreadableSitesIn(rel, planted), [{ ...pins[0], count: 2 }]),
    { unpinned: [], extra: [], stale: [] },
    `a pin that deliberately covers two sites must accept them`,
  );

  // And the stale direction with a count: a pin covering more sites than exist
  // is a failure of its own (that is the site that was re-read or removed).
  assert.deepEqual(
    pinVerdict(realSites, [{ ...pins[0], count: pins[0].count + 1 }]).stale.map(
      (s) => `${s.pin.rel} :: ${s.pin.expr} — pinned ${s.pin.count}, found ${s.found}`,
    ),
    [`${rel} :: ${pins[0].expr} — pinned ${pins[0].count + 1}, found ${pins[0].count}`],
    `a count larger than the sites that exist must be reported, not silently summed`,
  );

  // Two pins on the same key would have their counts SUMMED, which hides the
  // same "one entry for two sites" confusion one layer up.
  const keys = UNREADABLE_PIN.map((p) => siteKey(p.rel, p.expr));
  assert.deepEqual(
    keys.filter((k, i) => keys.indexOf(k) !== i),
    [],
    `UNREADABLE_PIN has two entries for the same file+expression; their counts would be ` +
      `added together, so one entry is dead and which one is not obvious`,
  );
  const badCounts = UNREADABLE_PIN.filter((p) => !Number.isInteger(p.count) || p.count < 1);
  assert.deepEqual(
    badCounts.map((p) => `${p.rel} :: ${p.expr} — count=${p.count}`),
    [],
    `every pin must name a whole number of sites of at least 1`,
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
  // to make impossible.
  //
  // Measured twice, on purpose:
  //   3228 resolved of 3233 className attributes before the /docs prose rewrite
  //   2998 after it, because 19 docs pages stopped hand-writing their markup and
  //        now render through app/docs/docsProse (one component, counted once).
  // The floor follows the tree: it is the post-rewrite measurement rounded down,
  // not a loosened one — a reader that stops following a shape still drops far
  // below it, and the tinted-files floor above still proves the walk found the app.
  let resolved = 0;
  for (const rel of FILES) {
    const code = codeOnly(readFileSync(`${SRC}${rel}`, "utf8"));
    for (const e of jsxTags(code)) {
      if (e.open && e.tag.classNameExpr !== "" && !e.tag.classNameUnreadable) resolved += 1;
    }
  }
  assert.ok(
    resolved >= 2950,
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
