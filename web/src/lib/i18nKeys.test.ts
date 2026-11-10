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
//       The same hole covers a template the compiler cannot reduce — and every
//       `fb.status.*` key in the dictionary is reachable ONLY that way (the admin
//       filter chips and each thread's badge both call `t(`fb.status.${s}`)`), so
//       no call-site scan can name it either: the scanner reports that call site
//       as DYNAMIC, and test 1 pins the POPULATION it renders through against the
//       Go status set instead (see the comment there).
//       And any future non-`Key` parameter added by a helper.
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
//   * DYNAMIC KEYS — 53 of the tree's call sites build the key from runtime data
//     (`t(`fb.status.${s}`)`, `t(entries.labelKey)`, `t(roleLabel(role))`), so no
//     static scan can NAME them; they are counted, and the ceiling in test 2 is
//     what stops the resolved half from shrinking unnoticed. One of those families
//     is still pinned exactly, from the other end: every `fb.status.*` key the API
//     can produce must exist in the dictionary, cross-checked against
//     `dbstore.FeedbackStatuses` (test 1) — the call site cannot be named, but the
//     VALUE SET it renders through can be checked, and a rename is then caught.
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
import ts from "typescript";

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

/**
 * The dictionary's `fb.status.*` keys without their prefix, sorted.
 *
 * These are the keys the admin triage UI resolves through `t(`fb.status.${s}`)`
 * — `s` is a status, so the only thing that can carry the translations is a
 * dictionary entry per status.
 */
const fbStatusDictKeys = (): string[] =>
  [...dictKeys]
    .filter((k) => k.startsWith("fb.status."))
    .map((k) => k.slice("fb.status.".length))
    .sort();

/**
 * The statuses the API accepts, read from the Go source
 * (`api/internal/dbstore/feedback.go`): the `FeedbackStatus<X> = "<value>"` consts
 * resolved through the `FeedbackStatuses` list. The DB CHECK constraint
 * (`feedback_threads_status_chk`, migration 000063) pins the same set, and the
 * REST/MCP layers validate against this list, so a thread's status can only ever
 * be one of these — which is what makes the dictionary check in test 1 exact
 * rather than a count.
 */
function goFeedbackStatuses(): string[] {
  const go = readFileSync(`${SRC_DIR}/../../api/internal/dbstore/feedback.go`, "utf8");
  const values = new Map<string, string>();
  for (const m of go.matchAll(/\b(FeedbackStatus[A-Za-z]+)\s*=\s*"([^"]*)"/g)) values.set(m[1], m[2]);
  const list = go.match(/var\s+FeedbackStatuses\s*=\s*\[\]string\{([^}]*)\}/);
  assert.ok(list, "could not find `var FeedbackStatuses = []string{…}` in api/internal/dbstore/feedback.go");
  assert.ok(values.size >= 4, `could not read the FeedbackStatus* consts in api/internal/dbstore/feedback.go (found ${values.size})`);
  return [...list![1].matchAll(/[A-Za-z_]\w*/g)]
    .map((m) => {
      const v = values.get(m[0]);
      assert.ok(v, `FeedbackStatuses names ${m[0]}, which has no string value in api/internal/dbstore/feedback.go`);
      return v!;
    })
    .sort();
}

/**
 * The statuses the admin page's own filter list iterates —
 * `const FB_STATUSES = [...] as const` in app/admin/page.tsx, the values `s`
 * takes at the chip call site. Read as source, not imported: the page is a client
 * component and this is a guard on the literal staying level with the server.
 */
function pageStatuses(): string[] {
  const page = readFileSync(`${SRC_DIR}/app/admin/page.tsx`, "utf8");
  const list = page.match(/const\s+FB_STATUSES\s*=\s*\[([^\]]*)\]/);
  assert.ok(
    list,
    "app/admin/page.tsx no longer declares `const FB_STATUSES = [...]` — the filter chips' value set must stay cross-checked against the server's statuses",
  );
  return [...list![1].matchAll(/"([^"]*)"/g)].map((m) => m[1]).sort();
}

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
  //     every fb.status.* key in the dictionary is reachable ONLY this way. The
  //     scanner reports that call site as DYNAMIC, so it cannot NAME the keys —
  //     what is pinned below is the POPULATION it renders through (see there).
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
  // The fb.status population, pinned EXACTLY rather than by count.
  //
  // `s` in `t(`fb.status.${s}`)` is a status the API accepts: the admin filter
  // chips iterate the page's own FB_STATUSES list and each thread's badge renders
  // the status the API returned, which the DB CHECK constraint
  // (feedback_threads_status_chk) has already restricted to that same set. So the
  // dictionary must carry an entry for every accepted status and no others — and
  // this is the only thing in the repo that can notice a rename, because no
  // call-site scan can name a dynamically-built key.
  //
  // A count (`>= 4`) was the earlier shape and it cannot see a rename: swap
  // `fb.status.open` for `fb.status.pending` in the dictionary and add one more,
  // the count stays 4, this file stays green, and every open thread's badge
  // renders the raw `fb.status.open` in all five locales.
  const fromGo = goFeedbackStatuses();
  assert.deepEqual(
    fbStatusDictKeys(),
    fromGo,
    "the fb.status.* dictionary keys must be EXACTLY the statuses the API accepts " +
      "(api/internal/dbstore/feedback.go FeedbackStatuses, mirrored by the feedback_threads_status_chk " +
      "CHECK in migration 000063). t(`fb.status.${s}`) falls back to the key itself, so a status that " +
      `exists on one side only is a raw "fb.status.…" in the admin chips and every thread badge:\n` +
      `  dictionary:  ${JSON.stringify(fbStatusDictKeys())}\n` +
      `  API accepts: ${JSON.stringify(fromGo)}\n` +
      "(Move the translations WITH the status — never leave an entry for a status the API cannot return.)",
  );
  // The same set again, from the page side: the chips' value list is what `s`
  // takes at the call site, so it drifting from the server's set renders chips for
  // statuses that cannot exist (or silently omits one that can).
  assert.deepEqual(
    pageStatuses(),
    fromGo,
    "app/admin/page.tsx's FB_STATUSES must be the same set as api/internal/dbstore/feedback.go " +
      `FeedbackStatuses:\n  page: ${JSON.stringify(pageStatuses())}\n  API:  ${JSON.stringify(fromGo)}`,
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
  //
  // RAISED 60 → 63, from three sites that arrived on two branches and all still
  // exist after the merge. A ceiling raised without a reason is a guard being
  // walked backwards, so each one names the guard that pays for it:
  //
  //   * 60 → 61 (card t_ec87d6c7): toolLabel in src/lib/chatProposal.ts asks for
  //     `chat.tool.<name>` built from the tool name the server sent, so the key
  //     exists only at runtime and no static scan can name it. That trade is paid
  //     for by a guard written for exactly this set: src/lib/chatProposal.test.ts
  //     reads the tool lists out of api/internal/mcpsrv/chat_tools.go and asserts
  //     every one of them has a `chat.tool.*` entry in all five locales, so the
  //     drift this scanner would have caught is caught there instead.
  //   * 61 → 63 (card t_148d7100), two sites, one each in:
  //     app/chat/page.tsx — the empty state's starter chips, one `t(key)` inside
  //     `CHAT_STARTERS.map(...)`. The list is a const tuple, so the four keys are
  //     fixed; the scanner cannot see through the `.map`. Those four keys are not
  //     unguarded: src/lib/chatStarters.test.ts asserts every chip key exists in
  //     all five locales AND that its question is one the Go read allowlist can
  //     answer.
  //     lib/chatSteps.ts — one `t(nounKey)`, the noun of a chat read tool, whose
  //     key is chosen from the tool's name on the wire. Which is the point: the
  //     tool list lives in Go. src/lib/chatSteps.test.ts reads
  //     api/internal/mcpsrv/chat_tools.go and fails when a tool has no noun, and
  //     asserts every noun key in all five locales.
  //     Both were reduced to ONE site each before this raise (a chip resolves its
  //     label once and sends that same string; the noun is resolved once and
  //     interpolated into both tenses), so the growth is the irreducible part only.
  //   * 63 → 65 (card t_52429d6f), the two action buttons in app/sales/page.tsx
  //     (the row cell and the detail drawer), each `t(a.labelKey as Key)` where
  //     `a.labelKey` is the `label_key` the SERVER sends on a sale's
  //     available_events. Which events exist was moved into the Go lifecycle
  //     table precisely so the page stops guessing; the key therefore arrives on
  //     the wire, and no static scan can name it. Paid for by
  //     src/lib/saleActions.test.ts: it reads every `LabelKey:` out of
  //     api/internal/lifecycle/sales.go and fails when one is missing from any of
  //     the five locales — the same trade the chatProposal/chatsSteps sites make,
  //     so a new button cannot ship as a raw key or a blank label.
  //
  //   * card t_804040d3 took the last of that headroom, with no raise: app/activity/
  //     page.tsx renders `t(labelKey)`, where `labelKey` is the `act.event.*` key
  //     src/lib/activityEvents.ts resolves from the event name the SERVER sent.
  //     The event vocabulary lives in Go (the `Events.Record(...)` sites), so no
  //     static scan can name these keys. Paid for by the guard appended to
  //     src/lib/activityPage.test.ts: it reads every recorded event name out of
  //     api/internal, expands the three names built at the call site
  //     (`form.status_*`, `form.bulk_*`, `product.bulk_*`), and fails when one has
  //     no `act.event.*` entry in any of the five locales — and in the other
  //     direction when a label names an event nothing records. The count is 65 of
  //     65, so the NEXT dynamic site fails this assertion until someone pays for it.
  //
  // 65 is MEASURED on the merged tree (main + this branch), not the sum guessed in
  // the card: the assertion below names tree.dynamic.length when it is exceeded.
  assert.ok(
    tree.dynamic.length <= 65,
    `${tree.dynamic.length} call sites are no longer statically resolved (ceiling 65). ` +
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

test("a regex literal is skipped, so a backtick in its body cannot open a template", () => {
  // The measured defect (card t_816ca78f). components/Markdown.ts:65 is
  //
  //   const FENCE = /^\s*(```|~~~)\s*[\w+-]*\s*$/;
  //
  // The old reader opened a TEMPLATE at the first of those three backticks and
  // closed it at the first backtick of the next backtick-carrying regex — the
  // `const CODE = /`([^`\n]+)`/y` 92 lines below. Everything between was fed to
  // the scanner as template TEXT, so no `t("…")` written in that window was seen:
  // `npm run test:unit` stayed green with the planted key appearing 0 times in the
  // output, and Markdown.ts has 0 call sites today, so the window was latent
  // rather than open. The shape is reproduced here first, then pinned in the real
  // file.
  const fixture = [
    "const FENCE = /^\\s*(```|~~~)\\s*[\\w+-]*\\s*$/;",
    'const a = t("qa.head.missing");',
    "const b = t(`qa.mid.missing`);",
    "const mid = {",
    "  re: /`([^`\\n]+)`/y,",
    "};",
    'const c = t("qa.tail.missing");',
  ].join("\n");
  assert.deepEqual(
    scanSource(fixture, "fixture.tsx").static.map((s) => s.key),
    ["qa.head.missing", "qa.mid.missing", "qa.tail.missing"],
    "a regex literal carrying backticks still hides the call sites written around it — the scanner is reading " +
      "regex text as code, so the window this file pins is open again",
  );
  // The card's own reproducer, in the real file: plant a key immediately after the
  // FENCE line and require the guard to see it. The line is located by its shape
  // rather than by number so this does not rot when the file moves (it already
  // moved once: PR 744 shifted it from 59 to 65).
  const mdPath = `${SRC_DIR}/components/Markdown.ts`;
  const md = readFileSync(mdPath, "utf8").split("\n");
  const fence = md.findIndex((l) => /^\s*const FENCE\s*=/.test(l));
  assert.ok(
    fence >= 0,
    "components/Markdown.ts no longer declares FENCE — if that backtick regex really is gone, replace this " +
      "pin with the reason the scanner no longer needs to skip regex literals, do not just delete it",
  );
  const planted = [
    ...md.slice(0, fence + 1),
    'export const qaMid = (t: (k: any) => string) => t("qa.mid.missing");',
    ...md.slice(fence + 1),
  ].join("\n");
  assert.deepEqual(
    scanSource(planted, mdPath)
      .static.filter((s) => s.key === "qa.mid.missing")
      .map((s) => `${rel(mdPath)}:${s.line}`),
    [`${rel(mdPath)}:${fence + 2}`],
    "a call site planted just after components/Markdown.ts's FENCE regex is invisible to the scan — the next " +
      "`t(\"…\")` written in that file would ship unguarded with the whole suite green",
  );
});

test("no regex literal in the tree can blind the scanner (AST cross-check)", () => {
  // The scanner decides "regex or division" from the character before the `/`
  // (REGEX_AFTER in i18nKeys.ts), which is a heuristic. The TypeScript parser is
  // the authority on which bytes ARE regex literals, so this cross-checks: every
  // regex literal whose BODY carries a quote or a backtick must be skipped by the
  // scanner. A backtick that is not skipped re-opens the Markdown.ts window; a
  // quote is what `lib/publishResult.ts`'s `/^https:\/\/[^\s"'<>]+$/i` carries and
  // must not derail the scan either. Behavioural, not structural: a call site
  // planted after that regex's own line must be resolved.
  const dangerous: { file: string; line: number }[] = [];
  for (const file of sourceFiles(SRC_DIR)) {
    const src = readFileSync(file, "utf8");
    const sf = ts.createSourceFile(
      file,
      src,
      ts.ScriptTarget.Latest,
      true,
      file.endsWith("x") ? ts.ScriptKind.TSX : ts.ScriptKind.TS,
    );
    const visit = (n: ts.Node) => {
      if (n.kind === ts.SyntaxKind.RegularExpressionLiteral) {
        const raw = n.getText(sf);
        const body = raw.slice(1, raw.lastIndexOf("/"));
        if (/["'`]/.test(body)) {
          dangerous.push({ file, line: sf.getLineAndCharacterOfPosition(n.getStart(sf)).line + 1 });
        }
      }
      ts.forEachChild(n, visit);
    };
    visit(sf);
  }
  assert.ok(
    dangerous.length > 0,
    "no regex literal in the tree carries a quote or a backtick any more — the Markdown.ts FENCE/CODE pair is " +
      "gone. Re-measure and replace this pin (a regex literal is still something the scanner has to skip); do " +
      "not leave it asserting a case the tree no longer has",
  );
  const blind: string[] = [];
  for (const d of dangerous) {
    const lines = readFileSync(d.file, "utf8").split("\n");
    const planted = [
      ...lines.slice(0, d.line),
      'const qaProbe = t("qa.regex.missing");',
      ...lines.slice(d.line),
    ].join("\n");
    if (!scanSource(planted, d.file).static.some((s) => s.key === "qa.regex.missing")) {
      blind.push(`${rel(d.file)}:${d.line}`);
    }
  }
  assert.deepEqual(
    blind,
    [],
    `a call site written after the regex literal at ${blind.join(", ")} is invisible to the scan: that regex ` +
      "carries a quote or a backtick the scanner is not skipping, so everything after it on the page reads as " +
      "literal text (see REGEX_AFTER in src/lib/i18nKeys.ts)",
  );
});

test("the scanner sees every literal call site the TypeScript parser sees", () => {
  // The independent check, kept as a test rather than a one-off measurement: the
  // parser is the authority on call sites, so a scanner that has gone blind
  // ANYWHERE — a regex literal read as code, a template opened on a stray
  // backtick, a quote mistaken for prose — shows up here as a literal `t("…")`
  // the parser sees and the scan does not report. Measured 0 missed when this was
  // written, so this is an exact invariant, not a floor.
  const scanned = new Set(tree.static.map((s) => `${s.file}:${s.line}:${s.key}`));
  const missed: string[] = [];
  for (const file of sourceFiles(SRC_DIR)) {
    const src = readFileSync(file, "utf8");
    const sf = ts.createSourceFile(
      file,
      src,
      ts.ScriptTarget.Latest,
      true,
      file.endsWith("x") ? ts.ScriptKind.TSX : ts.ScriptKind.TS,
    );
    const visit = (n: ts.Node) => {
      if (
        ts.isCallExpression(n) &&
        ts.isIdentifier(n.expression) &&
        n.expression.text === "t" &&
        n.arguments.length > 0
      ) {
        const arg = n.arguments[0];
        if (ts.isStringLiteral(arg) || ts.isNoSubstitutionTemplateLiteral(arg)) {
          const line = sf.getLineAndCharacterOfPosition(n.getStart(sf)).line + 1;
          if (!scanned.has(`${file}:${line}:${arg.text}`)) missed.push(`${rel(file)}:${line} ${arg.text}`);
        }
      }
      ts.forEachChild(n, visit);
    };
    visit(sf);
  }
  assert.deepEqual(
    missed,
    [],
    `${missed.length} literal call site(s) the TypeScript parser sees are invisible to the scan — the scanner is ` +
      "reading code as text somewhere (a regex literal, a stray backtick, a quote taken for prose) and every key " +
      "written in that region ships unguarded",
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
