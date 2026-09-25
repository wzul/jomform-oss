// Guard: every dictionary key the CODE actually calls must exist in the
// dictionary — the direction `src/lib/i18n.test.ts` cannot see.
//
// THE DEFECT THIS EXISTS FOR
//   `t()` in ../components/i18n.tsx falls back to the KEY ITSELF when the entry is
//   absent:
//
//     let s = (!entry ? k : entry[lang] || entry.en) as string;
//
//   src/lib/i18n.test.ts guards the other direction only — for each key IN the
//   dictionary, all five locales must be set. A key that DISAPPEARS (dropped in a
//   merge resolution, renamed, or typed wrong at the call site) is invisible to it:
//   `workspace.askTitle` is rendered literally to the user in all five locales,
//   HTTP is 200, nothing errors. Same shape as the #499 `#ZgotmplZ` rewrite: only
//   visible by looking at the page.
//
//   THE EXACT HOLE, MEASURED (this file corrects the card's premise, so read this
//   before believing "the type system would have caught it"):
//
//     * `next build` DOES typecheck, and for a call site typed through the context
//       (`t("does.not.exist")`) it already fails:
//         error TS2345: Argument of type '"does.not.exist"' is not assignable to
//         parameter of type '"nav.dashboard" | … | "forms.loadTrashFail"'.
//       So that shape was already gated, and a bare-literal plant is NOT the gap.
//
//     * The gap is where the KEY REACHES A DIFFERENTLY-TYPED `t`, because the
//       dictionary type is then not the thing being checked. Measured on this
//       tree: planting a bare `t("does.not.exist")` inside
//       components/MembersTab.tsx's `function roleLabel(t: (k: any) => string, …)`
//       leaves `next build` at exit 0, `Compiled successfully`, with i18n.test.ts
//       at 6/6 — the raw key ships. This guard reports it:
//         does.not.exist  <-  src/components/MembersTab.tsx:20
//       The same hole covers a template the compiler cannot reduce
//       (`fb.status.${s}` — all four fb.* keys that exist today are reachable only
//       that way) and any future non-`Key` parameter added by a helper.
//
//   So this guard is the net for the shapes a type-checked build cannot see. It is
//   also STRICTER than the compiler: it resolves every call site in the tree, so it
//   covers the whole `t` population rather than the fraction typed through the
//   context, and it names the call site for each missing key.
//
//   The live PR-689 evidence still stands as the reason the hole is worth closing:
//   the conflicted `i18nDict.ts` hunk left NINE keys absent from the head that main
//   had — docs.linkAPI, workspace.askBody/askHost/askPageSubtitle/askPageTitle/
//   askTitle, workspace.docs, workspace.slugReserved, workspace.whichNames. The
//   compiler catches the seven of those with live call sites; the guard catches
//   them too, and keeps catching them if the call site ever moves behind a helper.
//
//   Re-measured against the real pair (main e2ba313's call sites scanned against
//   PR 689 head 2eb6e4d's dictionary): the guard names SEVEN of those nine, with
//   the call site for each — workspace.askBody/askHost/askPageSubtitle/askPageTitle/
//   askTitle and workspace.docs in app/workspace-picker/page.tsx,
//   workspace.whichNames in components/WorkspaceSwitcher.tsx. It does NOT name
//   docs.linkAPI or workspace.slugReserved, because main's code has no live call
//   site for either — they are dictionary entries nothing renders. That is the
//   honest boundary of a call-site scan: a REMOVED key is caught when something
//   asks for it, and the two dead keys are a separate (harmless-for-users) kind of
//   drift this guard deliberately does not police.
//
// WHAT IS PINNED HERE
//   1. The scan is looking at the real tree and resolving the real bulk of it:
//      a floor on files/call sites/keys, a ceiling on the unresolved remainder,
//      and a shape check so a scanner bug cannot manufacture keys that then
//      "pass" against nothing.
//   2. THE GUARD ITSELF: every key a call site asks for is a key in DICT, and the
//      failure names the FULL missing list with every call site — not just the
//      first, because the reader has to fix the whole merge resolution at once.
//   3. HERMETIC BITE PROOFS (no fixture directory, no browser): the scanner
//      reports a planted missing key, reports every one of a set of planted
//      missing keys, and does NOT invent call sites out of comments, plain string
//      literals, or `at(`/`foo.t(` lookalikes; the reverse direction (a present
//      key) is not reported.
//   4. The renderer's raw-key fallback is still the fallback (if that ever
//      changes to a visible marker or a throw, this file's premise — and its
//      message — must be revisited rather than left describing a behaviour that
//      no longer exists).
//
// WHAT IT DOES NOT COVER (stated honestly)
//   * DYNAMIC KEYS. 41 of the tree's call sites build the key from runtime data —
//     `t(`fb.status.${s}`)`, `t(entries.labelKey)`, `t(roleLabel(role))` — so no
//     static scan can prove those resolve. They are COUNTED here (test 1 pins the
//     ceiling) and the i18nKeys.ts header lists them; proving them needs the
//     callers' own value sets, which is a different guard.
//   * DEAD KEYS — a dictionary entry nothing calls. That is not this defect: a
//     dead key costs a translation, a missing key costs the user's label. 272
//     dictionary keys are referenced only dynamically, so a dead-key check would
//     be mostly false positives.
//   * Translation QUALITY (see i18n.test.ts).
//
// Run with:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { DICT } from "../components/i18nDict.ts";
import { scanSource, scanTree, sourceFiles } from "./i18nKeys.ts";

const SRC_DIR = fileURLToPath(new URL("..", import.meta.url));
const dictKeys = new Set(Object.keys(DICT));

/**
 * Keys known to be absent from the dictionary TODAY. Empty on purpose.
 *
 * A missing key is the defect this guard exists for, so this list must stay
 * EMPTY: it is not a place to park a merge casualty. If a future change makes a
 * call site genuinely unfixable here (e.g. it must ship ahead of its dictionary
 * entry), add the key, an inline reason and the card that removes it — never
 * silence the guard by deleting the call site.
 */
const ALLOW_MISSING: string[] = [];

const rel = (file: string) => file.replace(`${SRC_DIR}/`, "");

/** `key <- file:line, file:line` lines for the failure message. */
function byKey(sites: { key: string; file: string; line: number }[]): string[] {
  const m = new Map<string, string[]>();
  for (const s of sites) {
    const list = m.get(s.key) ?? [];
    list.push(`${rel(s.file)}:${s.line}`);
    m.set(s.key, list);
  }
  return [...m].sort(([a], [b]) => a.localeCompare(b)).map(([k, l]) => `${k}  <-  ${[...new Set(l)].join(", ")}`);
}

const tree = scanTree(SRC_DIR, (f) => readFileSync(f, "utf8"));
const called = new Map<string, { key: string; file: string; line: number }[]>();
for (const s of tree.static) called.set(s.key, [...(called.get(s.key) ?? []), s]);

test("the hole this guard covers still exists (the type-checked build cannot see it)", () => {
  // The card's premise was "no gate fails". Measured while building this: that is
  // true only where the key reaches a `t` that is NOT typed as `Key`. A call site
  // typed through the context is already caught by `next build`'s typecheck
  // (TS2345), so THIS is the residual hole — and if it ever closes (every `t`
  // parameter typed `Key`, every template resolvable), this guard becomes a
  // readable duplicate of the compiler and should be folded into i18n.test.ts
  // rather than left asserting a reason that no longer holds.
  //
  // The evidence, all of it measured on this tree:
  //   * `function roleLabel(t: (k: any) => string, …)` in components/MembersTab.tsx
  //     — a literal miss planted inside it leaves `next build` at exit 0.
  //   * `fb.status.${s}` in app/admin/page.tsx — a template no compiler can reduce;
  //     every fb.status.* key in the dictionary is reachable ONLY this way, so if
  //     one were dropped, this guard's resolution of the key list is the only thing
  //     that could notice.
  // Read every source file the scan itself walked, so a file rename does not turn
  // this into a spurious "readFileSync: no such file" failure.
  const loose: string[] = [];
  for (const file of sourceFiles(SRC_DIR)) {
    const src = readFileSync(file, "utf8");
    for (const m of src.matchAll(/\bt:\s*\(k:\s*([A-Za-z_$][\w$.]*)/g)) {
      if (m[1] !== "Key") loose.push(`${rel(file)}: t(k: ${m[1]})`);
    }
  }
  assert.ok(
    loose.length > 0,
    "every `t` parameter in the tree is now typed `Key` — the compiler may fully cover this; " +
      "re-measure and, if so, fold this guard into src/lib/i18n.test.ts instead of keeping it",
  );
  // And the guard must still RESOLVE call sites inside those loose helpers, not
  // skip them: the whole point is that it looks past the type. `role.owner` is a
  // literal call site that lives inside MembersTab's any-typed helper.
  assert.ok(
    [...called.keys()].includes("role.owner"),
    "literal call sites inside the loose-typed helpers are no longer being resolved",
  );
  // The fb.status population is reachable only as a template; the scanner must
  // produce the keys, since nothing else in the repo can check them.
  const fbStatusKeys = [...dictKeys].filter((k) => k.startsWith("fb.status."));
  assert.ok(
    fbStatusKeys.length >= 4,
    `expected the fb.status.* keys to still exist (found ${fbStatusKeys.length})`,
  );
});

test("the scan is looking at a real tree and resolving the bulk of it", () => {
  // A guard that checks nothing passes (#507). These floors are the tree as
  // measured when this file was written: 84 source files, 1840 static call sites,
  // 1352 distinct keys. They may rise freely; a drop means the scan stopped
  // walking something, not that the dashboard got smaller.
  assert.ok(tree.files >= 80, `only ${tree.files} source files were scanned — the walk is skipping the tree`);
  assert.ok(tree.static.length >= 1500, `only ${tree.static.length} static call sites were resolved`);
  assert.ok(called.size >= 1200, `only ${called.size} distinct keys were resolved`);
  assert.ok(tree.resolved.length >= 10, `only ${tree.resolved.length} interpolated call sites were resolved`);
  // A CEILING, not a target: the unresolved remainder must shrink, never grow.
  // Above this, the scanner has degraded into "we only check the easy half" and
  // the honest thing is to fix the scanner or say so in this file's header.
  assert.ok(
    tree.dynamic.length <= 60,
    `${tree.dynamic.length} call sites are no longer statically resolved (ceiling 60). ` +
      `The guard covers that many fewer keys than it did — see the dynamic list in src/lib/i18nKeys.ts:\n  ` +
      tree.dynamic.map((d) => `${rel(d.file)}:${d.line} ${d.expr.trim()}`).join("\n  "),
  );
  // A scanner bug that mangles a resolved key (swallowing a `.`, keeping a `${`)
  // would otherwise "pass" test 2 against a key no dictionary could contain.
  const malformed = [...called.keys()].filter((k) => k === "" || /\s|\$\{|["'`]/.test(k));
  assert.deepEqual(malformed, [], `the scan produced keys that are not keys: ${JSON.stringify(malformed)}`);
});

test("every key the code calls exists in the dictionary", () => {
  // THE GUARD. Full list, every call site — a merge resolution has to be fixed as
  // a whole, and "the first one" sends the reader back for N more rounds.
  const missing = [...called.keys()].filter((k) => !dictKeys.has(k) && !ALLOW_MISSING.includes(k));
  const lines = byKey(missing.flatMap((k) => called.get(k) ?? []));
  assert.deepEqual(
    lines,
    [],
    `the code calls ${missing.length} key(s) that are NOT in components/i18nDict.ts.\n` +
      `t() falls back to the key itself, so each of these renders literally — e.g. "${missing[0]}" —\n` +
      `to EVERY user in EVERY locale, and nothing else fails:\n  ` +
      lines.join("\n  ") +
      `\n(If a key was dropped in a merge, restore it — do not delete the call site.)`,
  );
});

test("the reverse direction is caught too: a key that disappears IS reported", () => {
  // Hermetic bite proof. `t("does.not.exist")` is exactly the planted call site
  // this card asks for, kept in-file so the proof travels with the guard.
  const src = [
    "const a = t(\"nav.dashboard\");", // present in DICT
    "const b = t(\"does.not.exist\");", // the plant
  ].join("\n");
  const r = scanSource(src, "fixture.tsx");
  const keys = r.static.map((s) => s.key);
  assert.deepEqual(keys, ["nav.dashboard", "does.not.exist"], "the scanner did not resolve both call sites");
  const missing = keys.filter((k) => !dictKeys.has(k));
  assert.deepEqual(missing, ["does.not.exist"], "the guard reported the wrong missing set for the fixture");
});

test("the full missing list is reported, not just the first", () => {
  const src = ["t(\"a.one\")", "t('a.two')", "t(\"a.three\")", "t(`a.four`)", "t(\"nav.dashboard\")"].join("; ");
  const r = scanSource(src, "fixture.tsx");
  const missing = [...new Set(r.static.map((s) => s.key))].filter((k) => !dictKeys.has(k));
  assert.deepEqual(
    missing,
    ["a.one", "a.two", "a.three", "a.four"],
    "a planted set of missing keys must be reported in full — reporting one at a time makes the gate a queue, not a gate",
  );
});

test("comments, plain strings and lookalike identifiers are not call sites", () => {
  const src = [
    "// t(\"commented.out\") — a note about a key, not a call",
    "/* t(\"blockCommented.out\") */",
    "const doc = 't(\"inString.out\")';",
    "const tpl = `t(\"inTemplateText.out\")`;",
    "const at = (k) => k; at(\"notAT.out\");",
    "obj.t(\"methodNamedT.out\");",
    "const real = t(\"does.not.exist\");",
  ].join("\n");
  const r = scanSource(src, "fixture.tsx");
  assert.deepEqual(
    r.static.map((s) => s.key),
    ["does.not.exist"],
    "the scan invented a call site out of a comment, a string literal or a lookalike identifier",
  );
  // The reverse failure is worse: prose that HIDES a real call site. A JSX
  // apostrophe (`It's`) is not a string opener, and reading it as one would blank
  // everything after it on the line — the guard would keep passing while seeing
  // fewer and fewer keys. This is the shape measured 536 times in this tree.
  const jsx = 'return <p>It\'s time to {t("nav.dashboard")} — don\'t wait for {t("nav.forms")}</p>;';
  assert.deepEqual(
    scanSource(jsx, "fixture.tsx").static.map((s) => s.key),
    ["nav.dashboard", "nav.forms"],
    "a prose apostrophe on the line swallowed the real call sites after it",
  );
  // A comment that OPENS mid-expression must not blank the rest of the line either.
  const trailing = 'const x = list.map((a) => a); // t("commented.after") and t("nav.dashboard") is prose';
  assert.deepEqual(
    scanSource(trailing, "fixture.tsx").static,
    [],
    "a trailing `//` comment must hide the `t(` calls written inside it",
  );
});

test("interpolated call sites are resolved where they can be, and disclosed where they cannot", () => {
  // The two shapes the tree actually uses. Resolved: a template whose `${…}` is a
  // ternary of string literals (form-settings' prodImgLayout). Unresolved: a key
  // built from runtime data (admin's `fb.status.${s}`).
  const resolved = scanSource('t(`fs.prodImgLayout${ly === "image-top" ? "Top" : "Left"}` as Key)', "fixture.tsx");
  assert.deepEqual(
    resolved.static.map((s) => s.key).sort(),
    ["fs.prodImgLayoutLeft", "fs.prodImgLayoutTop"],
    "a template key of ternary literals must resolve to each key it can produce",
  );
  assert.deepEqual(resolved.dynamic, [], "that template is fully resolvable and must not be reported as dynamic");

  for (const [src, why] of [
    ["t(`fb.status.${s}` as Key)", "a template built from runtime data"],
    ["t(entries.labelKey)", "an identifier"],
    ["t(`prof.${id}` as const)", "a template built from runtime data"],
  ] as const) {
    const r = scanSource(src, "fixture.tsx");
    assert.deepEqual(r.static, [], `${src} must NOT be reported as a resolved key`);
    assert.equal(r.dynamic.length, 1, `${src} (${why}) must be reported as dynamic, so the blind spot is visible`);
  }
});

test("the renderer still falls back to the raw key (this guard's premise)", () => {
  // If the fallback ever becomes a visible marker or a throw, the raw-key symptom
  // changes and this file's header/message must be rewritten, not left describing
  // a behaviour the app no longer has. i18n.test.ts pins the English fallback;
  // this pins the missing-entry one.
  const renderer = readFileSync(`${SRC_DIR}/components/i18n.tsx`, "utf8");
  assert.match(
    renderer,
    /!\s*entry\s*\?\s*k\s*:/,
    "i18n.tsx no longer falls back to the key itself on a missing entry — revisit src/lib/i18nKeys.test.ts",
  );
  // And the dictionary this guard checks is the one the renderer uses (the same
  // second-copy protection i18n.test.ts applies, asserted from this side too so
  // the two guards cannot be checking different dictionaries).
  assert.match(renderer, /import\s*\{[^}]*\bDICT\b[^}]*\}\s*from\s*["']\.\/i18nDict(\.ts)?["']/);
});
