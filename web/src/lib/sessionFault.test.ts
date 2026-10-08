// Regression guard for card t_d1c9a43a: a SETTLED session-bootstrap FAULT must
// be recoverable on EVERY dashboard route, not only on `/`.
//
// THE DEFECT (measured by QA on the released build — v1.20.75, tree 3308cc72 —
// and re-measured on prod from the outside by the deploy lane's follow-up)
//
//   #1041 (card t_48319be9) split the session bootstrap's failure states
//   correctly: `session.tsx` reports a read that produced NO ANSWER — a 5xx, a
//   dropped connection, a 200 that is not JSON, or the bounded hung read — as
//   `sessionError` with `bundle === undefined`, and reserves `anon` for an
//   AUTHORITATIVE refusal (401, or an uncoded 403). It then offered the
//   fault + retry surface on ONE route: `/`, whose page is
//   `dashboard-client.tsx:169`.
//
//   Every other dashboard route read `anon` from `useSession()` and nothing
//   else. So on a settled fault `bundle` stayed `undefined`, each route stayed
//   on its own loading branch, and the visitor got a sentence-less "…" with no
//   control to retry. Measured with `dashboard-bundle` stubbed 500, `goto()`,
//   wait 8s:
//
//     PROBE /chat     faultSentence=0 retryBtn=0 landH1=0
//     PROBE /forms    faultSentence=0 retryBtn=0 landH1=0
//     PROBE /settings faultSentence=0 retryBtn=0 landH1=0
//     PROBE /products faultSentence=0 retryBtn=0 landH1=0
//     PROBE /sales    faultSentence=0 retryBtn=0 landH1=0
//
//   `/` under the identical probe read faultSentence=1 retryBtn=1 — the
//   contrast, and the reason this is a coverage defect and not a broken surface.
//   The bounded hung read (card t_f955c2f3) lands in the SAME `sessionError`, so
//   it does not rescue these routes either.
//
// THE FIX, AND WHY IT IS A SOURCE GUARD
//
//   The surface is now ONE component (`src/components/SessionFault.tsx`) rendered
//   from the ONE place every dashboard route already passes through: the app
//   chrome (`Shell`), which shows it INSTEAD of `children` when the bootstrap
//   faulted. A route therefore cannot forget it — it does not have to remember
//   it — and a new page is covered the moment it renders inside the shell. `/`
//   renders the same component from its own branch (a fault on `/` never reaches
//   Shell because dashboard-client returns before rendering it).
//
//   That structural claim is what this file pins, and it is the granularity the
//   defect lives at: the bug was "this route's tree never reads the fault state",
//   which is a property of the SOURCE (which tree is rendered, and what it reads
//   from the session context). The repo has no DOM/JSX unit runner (no
//   vitest/jest — see src/lib/pageShellLoading.test.ts and src/lib/sidebarNav.test.ts
//   for the same approach), so the rules are asserted against the component
//   source and the page inventory. The EXECUTED proof is the browser arm in
//   `web/e2e/session-bootstrap-anon.spec.ts`, which drives a 5xx on `/chat` and
//   measures the sentence, the retry, and the final URL — and which the `e2e`
//   preflight gate runs.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative } from "node:path";
import { fileURLToPath } from "node:url";

import { DICT, LANGS } from "../components/i18nDict.ts";

const APP_DIR = fileURLToPath(new URL("../app", import.meta.url));
const WEB_DIR = fileURLToPath(new URL("..", import.meta.url));
const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

const shell = read("../components/Shell.tsx");
const surface = read("../components/SessionFault.tsx");
const dashboard = read("../app/dashboard-client.tsx");

/**
 * Blank out comments while keeping every byte's offset (and every newline), for
 * the ABSENCE assertions. The components' own comments quote the very shapes
 * this file forbids — `SessionFault.tsx` explains why it must never be keyed on
 * `anon`, and `Shell.tsx` quotes `{sessionError ? … : children}` in prose — and a
 * raw-source match would read those as the defect being back.
 */
function codeOnly(src: string): string {
  let out = "";
  let i = 0;
  let state: "code" | "line" | "block" = "code";
  while (i < src.length) {
    const c = src[i];
    const n = src[i + 1];
    if (state === "code") {
      if (c === "/" && n === "/") { state = "line"; out += "  "; i += 2; continue; }
      if (c === "/" && n === "*") { state = "block"; out += "  "; i += 2; continue; }
      out += c; i += 1; continue;
    }
    if (state === "line") { if (c === "\n") { state = "code"; out += c; } else out += " "; i += 1; continue; }
    if (c === "*" && n === "/") { state = "code"; out += "  "; i += 2; continue; }
    out += c === "\n" ? "\n" : " ";
    i += 1;
  }
  return out;
}

const shellCode = codeOnly(shell);
const surfaceCode = codeOnly(surface);

/** Every `page.tsx` under `app/`. */
function pageFiles(): string[] {
  const out: string[] = [];
  const walk = (dir: string) => {
    for (const entry of readdirSync(dir)) {
      const p = join(dir, entry);
      if (statSync(p).isDirectory()) walk(p);
      else if (entry === "page.tsx") out.push(p);
    }
  };
  walk(APP_DIR);
  return out;
}

const pages = pageFiles();
/** The dashboard routes: every page whose tree renders the app chrome. */
const shellPages = pages.filter((f) => /<Shell\b/.test(codeOnly(readFileSync(f, "utf8"))));

/** A page's source, relative to `web/`, for a legible failure message. */
const rel = (abs: string) => relative(WEB_DIR, abs);

function codeOnlyFile(abs: string): string {
  return codeOnly(readFileSync(abs, "utf8"));
}

/**
 * Every `return <expr>` in the file, as the raw source text of that expression —
 * the same extraction `pageShellLoading.test.ts` uses, so this file can ask
 * "which returned trees render the shell, and which do not".
 */
function returnedTrees(abs: string): Array<{ line: number; expr: string }> {
  const src = readFileSync(abs, "utf8");
  const code = codeOnly(src);
  const out: Array<{ line: number; expr: string }> = [];
  const re = /\breturn\b/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(code))) {
    let j = m.index + "return".length;
    while (j < code.length && (code[j] === " " || code[j] === "\t")) j += 1;
    let end: number;
    if (code[j] === "(") {
      let depth = 0;
      let k = j;
      for (; k < code.length; k += 1) {
        if (code[k] === "(") depth += 1;
        else if (code[k] === ")") {
          depth -= 1;
          if (depth === 0) { k += 1; break; }
        }
      }
      end = k;
    } else {
      let k = j;
      while (k < code.length && code[k] !== ";" && code[k] !== "\n") k += 1;
      end = k;
    }
    const expr = src.slice(m.index, end);
    if (expr.trim().length <= "return".length) continue;
    out.push({ line: src.slice(0, m.index).split("\n").length, expr });
  }
  return out;
}

/**
 * A returned tree that replaces the whole viewport (the loading / gate shape),
 * as opposed to a tree the shell renders inside.
 */
const FULL_VIEWPORT = /<main\b[^>]*\bmin-h-screen\b|<div\b[^>]*\bmin-h-screen\b/;
/** A returned tree that renders the fault surface. */
const RENDERS_FAULT = /<SessionFault\b/;

/** Every dashboard page that reads the session context. */
const sessionPages = pages.filter((f) => /useSession\(\)/.test(codeOnlyFile(f)));

/** The five routes the defect was MEASURED on (see this file's header). */
const MEASURED_ROUTES: Array<{ rel: string; path: string }> = [
  { rel: "app/chat/page.tsx", path: "/chat" },
  { rel: "app/forms/page.tsx", path: "/forms" },
  { rel: "app/settings/page.tsx", path: "/settings" },
  { rel: "app/products/page.tsx", path: "/products" },
  { rel: "app/sales/page.tsx", path: "/sales" },
];

test("the guard is looking at the real app (it cannot pass vacuously)", () => {
  // If the walk stops finding page.tsx files, or the Shell filter stops
  // matching, every coverage assertion below would iterate nothing and report
  // green — which is the exact shape this guard exists to prevent.
  assert.ok(
    shellPages.length >= 13,
    `expected the dashboard's Shell pages, got ${shellPages.length}: ${shellPages.map(rel).join(", ")}`,
  );
  assert.ok(surfaceCode.length > 500, `SessionFault.tsx is suspiciously small — did the read break?`);
  for (const { rel: r } of MEASURED_ROUTES) {
    const abs = join(WEB_DIR, r);
    assert.ok(
      shellPages.includes(abs),
      `${r} is not detected as a Shell page — the guard is not looking at a route the defect was measured on`,
    );
  }
  assert.ok(
    Object.keys(DICT).some((k) => k === "common.sessionUnavailable"),
    "the dictionary is not the one this guard reads",
  );
});

test("the app chrome renders the fault surface for EVERY route it wraps", () => {
  // THE CLASS. A route cannot opt out of the fault surface because it does not
  // render the surface at all — the chrome it renders into does. This is the
  // structural reason a NEW page is covered on the day it lands, and the
  // assertion that fails if the branch is dropped from Shell.
  assert.match(
    shellCode,
    /<SessionFault\s*\/>/,
    "Shell.tsx no longer renders <SessionFault /> — the fault + retry surface is back to existing only on `/` " +
      "(card t_d1c9a43a), so a settled 5xx on /chat, /forms, /settings, /products or /sales is an " +
      "un-Shell'd \"…\" with no sentence and no control again",
  );
  // …and it is the element that REPLACES the page's content, in the slot every
  // page is rendered into — not a decoration beside it.
  assert.match(
    shellCode,
    /\{sessionError\s*\?\s*<SessionFault\s*\/>\s*:\s*children\}/,
    "Shell.tsx does not swap its content slot for the fault surface when `sessionError` is set — the page's own " +
      "loading branch would stay on screen underneath a Retry that has nothing to retry",
  );
  // The chrome stays mounted: the fault is about the page's DATA, and dropping
  // the sidebar/topbar is the \"reads as a full page reload\" defect
  // PageLoading exists to prevent.
  assert.match(
    shellCode,
    /id="main-content"/,
    "Shell.tsx lost #main-content — the skip-to-content target and the slot the fault surface renders into",
  );
});

test("the surface is keyed on the FAULT, never on `anon`", () => {
  // The whole point of #1041 (card t_48319be9) is that these two states are NOT
  // interchangeable: `anon` is the authoritative-401 gate, and every page runs
  // `if (anon) router.replace(\"/landing\")`. Rendering a fault surface for
  // `anon` would replace that wanted bounce with a dead page whose Retry can
  // never succeed — the sign-out the server never asked for, one layer in.
  assert.match(
    surfaceCode,
    /sessionError/,
    "SessionFault.tsx no longer reads `sessionError` — it cannot tell a fault from a live session",
  );
  assert.doesNotMatch(
    surfaceCode,
    /\banon\b/,
    "SessionFault.tsx reads `anon`: rendering the fault surface for an AUTHORITATIVE 401 would replace the " +
      "/landing bounce with a dead page offering a retry that can never succeed (card t_48319be9)",
  );
  assert.doesNotMatch(
    shellCode,
    /\banon\b/,
    "Shell.tsx now reads `anon` — the fault branch must be keyed on `sessionError` alone, or an anonymous " +
      "visitor is offered a Retry instead of the landing page",
  );
  // The fault branch is guarded by the fault state and nothing else.
  assert.match(
    shellCode,
    /\{sessionError\s*\?/,
    "Shell.tsx's fault branch is no longer guarded by `sessionError`",
  );
});

test("the surface offers the sentence AND the control that can recover it", () => {
  // A surface that says \"we couldn't start your session\" without a way out is
  // the same dead end as the \"…\" it replaced, one sentence longer.
  assert.match(
    surfaceCode,
    /\{t\("common\.sessionUnavailable"\)\}/,
    "SessionFault.tsx does not render the fault sentence through t() — the visitor is told nothing",
  );
  assert.match(
    surfaceCode,
    /onClick=\{retry\}/,
    "SessionFault.tsx's control is not wired to `retry()` — it would be a button that does nothing",
  );
  assert.match(
    surfaceCode,
    /\{t\("common\.retry"\)\}/,
    "SessionFault.tsx's control is not labelled with the localised retry text",
  );
  // `retry` is the bootstrap re-attempt (session.tsx), distinct from `reload`:
  // it drops only the in-flight entry and re-runs the bootstrap effect.
  assert.match(
    surfaceCode,
    /const\s*\{[^}]*\bretry\b[^}]*\}\s*=\s*useSession\(\)/,
    "SessionFault.tsx no longer takes `retry` from the session context",
  );
  // role=status/alert is not asserted: the sentence is a plain <p> in the same
  // shape `/` already shipped, and inventing an announcement here would make the
  // two surfaces differ (the drift this change exists to remove).
});

test("`/` renders the SAME surface, so the two cannot drift apart", () => {
  // `/` is the one route whose fault never reaches Shell (dashboard-client
  // returns its own tree before rendering it). If it kept a second, inline copy
  // the sentence and the retry could diverge between `/` and everywhere else —
  // the drift this change removes rather than re-creates.
  assert.match(
    codeOnly(dashboard),
    /if \(sessionError\) return <SessionFault \/>/,
    "dashboard-client.tsx no longer renders the shared <SessionFault /> on a fault — `/` has a second, " +
      "independent copy of the surface again",
  );
  const copies = [...codeOnly(dashboard).matchAll(/t\("common\.sessionUnavailable"\)/g)].length;
  assert.equal(
    copies,
    0,
    "dashboard-client.tsx renders the fault sentence itself — the sentence must come from the ONE surface " +
      "(components/SessionFault.tsx) so `/` and every other route cannot drift",
  );
});

test("every route gated on the session bundle is recoverable — from the chrome or its own branch", () => {
  // THE CLASS, IN BOTH DIRECTIONS. A route whose content is gated on the
  // session bundle must reach the fault surface ONE of two ways:
  //
  //   * it renders its tree inside <Shell>, so the chrome's branch covers it
  //     (the rule asserted above), or
  //   * it returns a full-viewport tree of its OWN before ever reaching the
  //     chrome (an early GATE — /admin's permission branch is the live one, and
  //     it runs first because `user` comes from the bundle), and then the page
  //     must render the fault surface itself.
  //
  // The second shape is exactly how the original defect hid: the fault state is
  // not on the page's main return, it is on a branch that runs before it. A page
  // in neither bucket is a route where a settled fault is unrecoverable again.
  //
  // NOTE the gate trees are collected even on a page that ALSO renders <Shell>
  // (/admin does both). Skipping those was a hole in the first cut of this rule:
  // /admin renders the chrome for the happy path AND returns a permission tree
  // before it, so "has <Shell>" was not sufficient evidence of coverage.
  const offenders: string[] = [];
  for (const abs of sessionPages) {
    const src = codeOnlyFile(abs);
    const hasShell = /<Shell\b/.test(src);
    const gateTrees = returnedTrees(abs).filter(
      ({ expr }) => FULL_VIEWPORT.test(expr) && !/<(Shell|PageLoading)\b/.test(expr),
    );
    if (hasShell && gateTrees.length === 0) continue; // covered by the chrome
    if (RENDERS_FAULT.test(src)) continue; // has its own fault branch
    const where = gateTrees.length
      ? gateTrees.map(({ line }) => `${rel(abs)}:${line}`).join(", ")
      : `${rel(abs)} (no <Shell> at all)`;
    offenders.push(
      `${where} — this route reads the session bundle and can return a full-viewport tree that renders neither ` +
        `the chrome nor <SessionFault />, so a settled fault leaves the visitor on it with no sentence and no ` +
        `retry (card t_d1c9a43a)`,
    );
  }
  assert.deepEqual(offenders, [], offenders.join("\n"));
  // A vacuity floor in BOTH directions: if the session-read filter stopped
  // matching, or the gate-tree extractor stopped finding /admin's permission
  // branch, this rule would iterate nothing and report green.
  assert.ok(
    sessionPages.length >= 12,
    `expected the dashboard's session-reading pages, got ${sessionPages.length}`,
  );
  assert.ok(
    returnedTrees(join(WEB_DIR, "app/admin/page.tsx")).length > 0 &&
      /min-h-screen/.test(codeOnlyFile(join(WEB_DIR, "app/admin/page.tsx"))),
    "the gate-tree extractor no longer sees /admin's early-return tree — this rule is checking nothing",
  );
});

test("every route the defect was measured on renders inside the shell", () => {
  // The coverage claim is grounded on the measured routes, not assumed: each one
  // renders `<Shell>`, and the shell renders the surface (asserted above). A
  // page that stopped rendering the shell would drop out of this class silently
  // (the `pageShellLoading.test.ts` rule is about a different defect — a bare
  // loading <main> — and its KEEPS_SHELL pattern would still be satisfied by
  // PageLoading).
  for (const { rel: r, path } of MEASURED_ROUTES) {
    const src = codeOnly(readFileSync(join(WEB_DIR, r), "utf8"));
    assert.match(
      src,
      /<Shell\s+active="/,
      `${r} (${path}) no longer renders <Shell active="…"> — it has left the class of routes the app chrome ` +
        `covers, so a settled fault on it is unrecoverable again (card t_d1c9a43a)`,
    );
    assert.doesNotMatch(
      src,
      /t\("common\.sessionUnavailable"\)/,
      `${r} (${path}) renders its own fault sentence — the surface must be the shared one, or the routes can ` +
        `disagree about what a fault looks like`,
    );
  }
});

test("the fault sentence and the retry exist in all five locales", () => {
  // A surface that shows English to a Malay user is a defect of its own, and
  // this one is the ONLY thing the visitor sees on a fault. The deploy lane
  // measured `common.sessionUnavailable` in ms as \"Kami tidak dapat memulakan
  // sesi anda. Sila cuba lagi.\" — the completeness rule is what keeps it true
  // for the routes this card adds.
  for (const key of ["common.sessionUnavailable", "common.retry"]) {
    const entry = DICT[key as keyof typeof DICT] as Record<string, unknown> | undefined;
    assert.ok(entry, `${key} is missing from the dictionary — the fault surface would render its raw key`);
    const missing = LANGS.filter((l) => typeof entry[l] !== "string" || String(entry[l]).trim() === "");
    assert.deepEqual(missing, [], `${key} is untranslated for ${missing.join("/")}`);
  }
  // The surface reads those exact keys — a rename on one side is caught here
  // rather than by a raw key on screen.
  for (const key of ["common.sessionUnavailable", "common.retry"]) {
    assert.ok(
      surface.includes(`t("${key}")`),
      `SessionFault.tsx no longer renders ${key}`,
    );
  }
});
