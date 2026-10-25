// Regression guard for the signed-in page bodies rendering NO `main` landmark
// (card t_25a8fc88).
//
// THE BUG. Every dashboard route renders its page body inside the app chrome
// (`components/Shell.tsx`), and that slot was a plain `<div id="main-content">`.
// `/profile` and `/settings` happened to carry their own `<main>` INSIDE it, so
// they were the only signed-in routes a full-document axe 4.13.0 scan found a
// landmark on. `/products`, `/media`, `/forms`, `/activity`, `/sales`, … rendered
// `<div className="w-full flex-1 px-6 py-8">`, and the scan measured
// `landmark-one-main` (moderate: the document has no `main`) plus `region`
// (moderate: one node per block of page content outside every landmark) on each
// of them.
//
// THE FIX, AND WHY IT IS IN THE SHELL. The Shell already owns the one content
// slot every dashboard route passes through — the same argument that put the
// session-fault surface there. The slot's body is now a `<main>`; the page bodies
// stay `<div>`s, because a page that kept its own `<main>` would nest two (that
// is invalid, and axe reports `landmark-main-is-top-level`). The `SessionFault`
// branch is the one exception: it is a full-page `<main>` of its own (it also
// renders from `/`, where no Shell exists), so the Shell does not wrap it — the
// document still has exactly one landmark.
//
// WHY A SOURCE RULE WHEN THE REAL PROOF IS A BROWSER SCAN. The card's acceptance
// is a signed-in axe scan, and that lives in `web/e2e/a11y-authed-pages.spec.ts`
// (one `main`, and neither `landmark-one-main` nor `region` on the route). This
// file is the fast half that runs in `npm run test:unit`, before a server or a
// browser exists: it fails BY NAME when the Shell stops rendering the landmark,
// or when a page puts a `<main>` back inside a `<Shell>` tree.
//
// WHAT IS PINNED
//   1. The Shell renders exactly ONE `<main>`, and it wraps `{children}` — the
//      page, not the chrome. A Shell whose `<main>` wrapped the sidebar and top
//      bar instead would satisfy a count but not this clause.
//   2. NO tree that renders `<Shell>` also renders a `<main>`: the nested
//      landmark class, asserted over every `.tsx` under `web/src` rather than a
//      list of the three files that had it. A twelfth page written the old way
//      fails here.
//   3. The fault branch keeps its own landmark, because (1) deliberately does not
//      wrap it.
//   4. Non-vacuity: the walk must find the app's files, the Shell, and the three
//      page surfaces the fix had to change.
//
// WHAT IT CANNOT SEE (stated honestly): the rendered DOM. It proves the source
// shape, not that a browser painted one landmark — the axe arm does that.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { codeOnly, tsxFiles } from "./jsxSource.ts";

// The base this file walks, written as a `new URL("<lit>", import.meta.url)`
// constant on purpose: `src/lib/buildContext.test.ts` resolves literal read
// targets through exactly this shape, so this walk stays visible to the
// image-build guard (web/Dockerfile copies `web/`, which is what makes the read
// legal inside the web image).
const SRC = new URL("../", import.meta.url); // web/src/

const srcAt = (rel: string) => readFileSync(fileURLToPath(new URL(rel, SRC)), "utf8");

/** `web/src`-relative path -> comment-stripped source (strings kept: classNames are assertions). */
const codeAt = (rel: string) => codeOnly(srcAt(rel));

const SHELL = "components/Shell.tsx";
const FAULT = "components/SessionFault.tsx";

/** The page surfaces the fix had to change, and that rule (2) is really looking at. */
const IN_SHELL_BODIES = [
  "app/profile/page.tsx",
  "app/settings/page.tsx",
  "app/products/ProductEditor.tsx",
] as const;

const files = tsxFiles(fileURLToPath(SRC)).map((rel) => (rel.startsWith("/") ? rel.slice(1) : rel));

const lineOf = (code: string, at: number) => code.slice(0, at).split("\n").length;

/**
 * Every `<Shell …> … </Shell>` range in one file, by offset.
 *
 * A page's own `<main>` may live in a branch that returns WITHOUT the Shell (the
 * `/` bootstrap gate does exactly that, and it is the only landmark on that
 * page), so the rule is about the tree, not the file: a `<main>` INSIDE a Shell
 * element is a second landmark nested in the first.
 */
function shellRanges(code: string): Array<[number, number]> {
  const ranges: Array<[number, number]> = [];
  let from = 0;
  for (;;) {
    const open = code.indexOf("<Shell", from);
    if (open === -1) break;
    const close = code.indexOf("</Shell>", open);
    if (close === -1) break; // self-closing or unterminated: not a wrapper
    ranges.push([open, close]);
    from = close + "</Shell>".length;
  }
  return ranges;
}

test("the guard is walking the real tree (it cannot pass vacuously)", () => {
  assert.ok(
    files.length >= 50,
    `the walk found only ${files.length} .tsx files under web/src (measured 73 when this guard was ` +
      "written) — the rule below would be checking nothing",
  );
  for (const rel of [SHELL, FAULT, ...IN_SHELL_BODIES]) {
    assert.ok(files.includes(rel), `${rel} was not picked up by the walk — the guard is blind to it`);
    assert.ok(srcAt(rel).length > 500, `${rel} read back empty — the read is broken, not the file`);
  }
  // Rule (1) and (2) are only meaningful if these three really render the Shell
  // today: if one of them stopped (the page moved), the rule silently stops
  // covering the surface the card was about.
  for (const rel of IN_SHELL_BODIES) {
    assert.match(
      codeAt(rel),
      /<Shell\b/,
      `${rel} no longer renders <Shell> — update IN_SHELL_BODIES, or the nested-landmark rule ` +
        "has stopped looking at the page the defect was reported against",
    );
  }
});

test("the Shell wraps the page body in exactly one <main> landmark", () => {
  const code = codeAt(SHELL);
  const opens = [...code.matchAll(/<main\b/g)].length;
  const closes = [...code.matchAll(/<\/main>/g)].length;
  assert.equal(
    opens,
    closes,
    `Shell.tsx has ${opens} <main> opening tag(s) and ${closes} closing tag(s) — an unbalanced ` +
      "landmark does not render",
  );
  assert.equal(
    opens,
    1,
    `Shell.tsx renders ${opens} <main> element(s): a signed-in document must have exactly ONE, ` +
      "and this is the component every dashboard route renders through — with none the page has " +
      "no landmark at all (axe landmark-one-main), with two they are nested (landmark-main-is-top-level)",
  );

  const open = code.indexOf("<main");
  const close = code.indexOf("</main>", open);
  const inner = code.slice(open, close);
  assert.match(
    inner,
    /\{children\}/,
    "the Shell's <main> does not wrap {children} — the landmark must contain the PAGE's body. " +
      "An empty or chrome-wrapping <main> satisfies a tag count and leaves the page content outside " +
      "every landmark, which is the `region` violation this fix removed",
  );
});

test("no tree that renders the Shell also renders a <main> (nested landmarks)", () => {
  const offenders: string[] = [];
  let sawShellTree = 0;
  for (const rel of files) {
    const code = codeAt(rel);
    const ranges = shellRanges(code);
    if (ranges.length === 0) continue;
    sawShellTree += 1;
    for (const m of code.matchAll(/<main\b/g)) {
      const at = m.index ?? 0;
      if (ranges.some(([open, close]) => at > open && at < close)) {
        offenders.push(`${rel}:${lineOf(code, at)}`);
      }
    }
  }
  assert.ok(
    sawShellTree >= 10,
    `only ${sawShellTree} file(s) render a <Shell> tree — the walk or the marker drifted, so this ` +
      "rule would pass without looking at the pages",
  );
  assert.deepEqual(
    offenders,
    [],
    "a <main> is nested inside a <Shell> tree at: " +
      `${offenders.join(", ")}. The Shell already renders the document's one <main> (it wraps ` +
      "{children}), so a page body that renders its own makes two — invalid HTML, and axe reports " +
      "landmark-main-is-top-level. Keep the page body a <div>.",
  );
});

test("the session-fault branch keeps a landmark of its own", () => {
  // Rule (1) deliberately does not wrap this branch (the fault surface also
  // renders from `/`, with no Shell around it), so its own <main> is the ONLY
  // landmark a faulted page has. Losing it would put the fault state back to
  // zero landmarks while every healthy route stayed green.
  const fault = codeAt(FAULT);
  assert.equal(
    [...fault.matchAll(/<main\b/g)].length,
    1,
    "components/SessionFault.tsx must render exactly one <main>: it is the document's only " +
      "landmark on a faulted route (the Shell does not wrap it in one)",
  );
  assert.match(
    codeAt(SHELL),
    /sessionError \? \(\s*<SessionFault \/>\s*\)\s*:\s*\(/,
    "Shell.tsx no longer renders <SessionFault /> as the alternative to its content branch — the " +
      "rule above is then guarding a component the Shell stopped mounting",
  );
});
