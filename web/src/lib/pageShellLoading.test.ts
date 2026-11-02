// Regression guard for the "navigation feels like a full page reload" report.
//
// THE BUG: five dashboard pages early-returned a BARE `<main>` while their
// list fetch was outstanding —
//
//   if (checking) return <main className="flex min-h-screen items-center
//                              justify-center text-slate-400">…</main>;
//
// — in forms/page.tsx, media/page.tsx, products/page.tsx,
// subscriptions/page.tsx and templates/page.tsx. That returned tree contains no
// <Shell>, so React unmounted the sidebar and the top bar for the duration of
// the fetch and mounted them again afterwards. Nothing reloaded the browser:
// the route change is an ordinary client-side <Link> transition. But the shell
// vanishing and coming back is exactly what reads as a full refresh, and it is
// why /sales -> /forms felt slow while the reverse felt instant (/sales has no
// such gate — it paints the Shell immediately and fills the table in).
//
// This repo has no JSX/DOM test runner (no vitest/jest — see
// src/lib/i18n.test.ts, src/lib/adminResponsive.test.ts for the same approach),
// so the rule is asserted against the page SOURCE, plus against the one shared
// component that now carries the loading state. That granularity matches the
// defect: the bug WAS a returned tree that omitted the shell, and a source scan
// of returned trees is what sees it.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
//
// WHAT IS PINNED, AND WHY
//   1. THE CLASS, not the instance: any page.tsx that renders a Shell may not
//      return a full-viewport `<main>` (a returned tree carrying min-h-screen)
//      unless that same tree also renders the shell — either `<Shell>` directly
//      or the shared shell-preserving <PageLoading>. A sixth page written the
//      same way fails here.
//   2. The FIVE pages that were broken pin the fix explicitly, with the active
//      nav key they pass. A page that keeps the shell but highlights the wrong
//      sidebar entry is a different regression, so the loading key is checked
//      against the page's own <Shell active="...">.
//   3. The shared loader itself must render <Shell>: the class rule in (1)
//      accepts `<PageLoading>`, so a loader that quietly dropped the shell
//      would make every page pass while the browser still flashed. This is the
//      assertion that stops the fix from being able to regress behind rule 1.
//   4. The loader is a real loading state, not a blank frame: role="status" +
//      aria-busy so assistive tech is told, and an sr-only "common.loading"
//      line so a screen reader gets a name (the skeleton bars are decorative
//      and read as nothing). `dashboardStats`-style key reuse is avoided — this
//      key is checked to exist in all five locales, because a page-loading
//      state that shows English to a Malay user is a defect of its own.
//
// WHAT IT DOES NOT COVER (stated honestly)
//   * It cannot measure the browser. It proves no page RETURN omits the shell;
//     it does not prove React kept the DOM across a navigation. That was
//     verified in a production build (see the PR description).
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative } from "node:path";
import { fileURLToPath } from "node:url";

import { DICT, LANGS } from "../components/i18nDict.ts";

const APP_DIR = fileURLToPath(new URL("../app", import.meta.url));
const WEB_DIR = fileURLToPath(new URL("..", import.meta.url));

const loader = readFileSync(
  fileURLToPath(new URL("../components/PageLoading.tsx", import.meta.url)),
  "utf8",
);
// The loader's own comment quotes the buggy `<main className="flex min-h-screen …">`
// markup it was written to replace, so every rule about what the loader RENDERS
// is asserted against comment-stripped code.
const loaderCode = codeOnly(loader, false);

/** The five pages this defect was reported against, with the nav key each uses. */
const FIXED_PAGES: Array<{ rel: string; active: string }> = [
  { rel: "app/forms/page.tsx", active: "forms" },
  { rel: "app/media/page.tsx", active: "media" },
  { rel: "app/products/page.tsx", active: "products" },
  { rel: "app/subscriptions/page.tsx", active: "subscriptions" },
  { rel: "app/templates/page.tsx", active: "templates" },
];

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

const read = (abs: string) => readFileSync(abs, "utf8");

/**
 * Blank out comments — and, when `blankStrings` is set, string/template
 * CONTENTS — while keeping every byte's offset (and every newline). Brace/paren
 * balance is then computed on code only, and a slice of the ORIGINAL source at
 * those offsets is still the real code.
 *
 * `blankStrings` is OFF for assertions about rendered markup: an attribute
 * value (`role="status"`, a className) lives INSIDE a string literal in the
 * source, and blanking it would hide the very thing being checked.
 */
function codeOnly(src: string, blankStrings = true): string {
  let out = "";
  let i = 0;
  let state: "code" | "line" | "block" | '"' | "'" | "`" = "code";
  while (i < src.length) {
    const c = src[i];
    const n = src[i + 1];
    if (state === "code") {
      if (c === "/" && n === "/") { state = "line"; out += "  "; i += 2; continue; }
      if (c === "/" && n === "*") { state = "block"; out += "  "; i += 2; continue; }
      if (blankStrings && (c === '"' || c === "'" || c === "`")) {
        state = c;
        out += c;
        i += 1;
        continue;
      }
      out += c; i += 1; continue;
    }
    if (state === "line") { if (c === "\n") { state = "code"; out += c; } else out += " "; i += 1; continue; }
    if (state === "block") {
      if (c === "*" && n === "/") { state = "code"; out += "  "; i += 2; continue; }
      out += c === "\n" ? "\n" : " ";
      i += 1;
      continue;
    }
    // inside a string/template literal
    if (c === "\\") { out += "  "; i += 2; continue; }
    if (c === state) { state = "code"; out += c; i += 1; continue; }
    out += c === "\n" ? "\n" : " ";
    i += 1;
  }
  return out;
}

/**
 * Every `return <expr>` in the file, as the raw source text of that expression.
 * A parenthesised return is balanced to its closing paren (so a multi-line JSX
 * tree comes back whole); a bare `return <X/>;` stops at the `;` or line end.
 */
function returnedTrees(src: string): Array<{ line: number; expr: string }> {
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
      // No wrapping parens: the expression ends at the `;` or the line break.
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

/** A returned tree that brings up a full-viewport <main> (the bug's own shape). */
const FULL_VIEWPORT_MAIN = /<main\b[^>]*\bmin-h-screen\b/;
/** A returned tree that keeps the app chrome mounted. */
const KEEPS_SHELL = /<(Shell|PageLoading)\b/;

const shellPages = pageFiles().filter((f) => /import\s*\{[^}]*\bShell\b[^}]*\}\s*from/.test(read(f)));

test("the guard is scanning the real dashboard pages (it cannot pass vacuously)", () => {
  // If the walk stops finding page.tsx files, or the Shell-import filter stops
  // matching, every assertion below would iterate nothing and report green.
  assert.ok(
    shellPages.length >= 13,
    `expected the dashboard's Shell pages, got ${shellPages.length}: ${shellPages
      .map((f) => relative(WEB_DIR, f))
      .join(", ")}`,
  );
  for (const { rel } of FIXED_PAGES) {
    const abs = join(WEB_DIR, rel);
    assert.ok(
      shellPages.includes(abs),
      `${rel} was not picked up by the scan — the guard is not looking at the page that was broken`,
    );
    // A page with no return at all would make the rule below vacuous.
    assert.ok(
      returnedTrees(read(abs)).length > 0,
      `the return-extractor found no returns in ${rel} — the guard would pass without checking anything`,
    );
  }
});

test("no page returns a full-viewport <main> without the shell", () => {
  // THE CLASS. The old loading branch returned `<main className="flex
  // min-h-screen …">…</main>` and nothing else, so the sidebar and top bar were
  // unmounted for the length of the fetch. Any returned tree that carries a
  // min-h-screen <main> must also render the shell.
  const offenders: string[] = [];
  for (const abs of shellPages) {
    const rel = relative(WEB_DIR, abs);
    for (const { line, expr } of returnedTrees(read(abs))) {
      if (!FULL_VIEWPORT_MAIN.test(expr)) continue;
      if (KEEPS_SHELL.test(expr)) continue;
      offenders.push(
        `${rel}:${line} returns a full-viewport <main> with no <Shell> — the sidebar and ` +
          `top bar unmount during this state, which reads as a full page reload`,
      );
    }
  }
  assert.deepEqual(offenders, []);
});

test("the five pages that dropped the shell now load through the shell", () => {
  // THE INSTANCE. Each of these five had an early return whose tree omitted
  // <Shell>; each now renders the shared loader, under its own nav key.
  for (const { rel, active } of FIXED_PAGES) {
    const src = read(join(WEB_DIR, rel));
    // Comment-stripped: a page may legitimately QUOTE the old markup in a
    // comment explaining what it replaced (templates/page.tsx does), and that
    // must not read as the markup being back.
    const code = codeOnly(src, false);
    assert.match(
      code,
      new RegExp(`<PageLoading active="${active}"`),
      `${rel} no longer renders its in-flight state inside the shell (expected ` +
        `<PageLoading active="${active}" />)`,
    );
    assert.doesNotMatch(
      code,
      FULL_VIEWPORT_MAIN,
      `${rel} has a full-viewport <main> again — that is the bare loading gate that dropped ` +
        `the shell on every navigation to this page`,
    );
    // The loading key and the full render must agree, or the sidebar entry
    // highlights one page while another is on screen for the duration of the
    // fetch (a flicker of the wrong nav item).
    const shellKey = code.match(/<Shell active="([a-z]+)"/);
    assert.ok(shellKey, `${rel} no longer renders <Shell active="…"> at all`);
    assert.equal(
      shellKey![1],
      active,
      `${rel} loads under "${active}" but renders the page under "${shellKey![1]}" — the sidebar highlight moves twice per visit`,
    );
  }
});

test("the shared loader keeps the shell mounted", () => {
  // Rule (1) accepts `<PageLoading>` as shell-preserving. If the loader itself
  // stopped rendering <Shell>, every page would still pass while the browser
  // flashed exactly as before — so the loader is pinned here, not assumed.
  assert.match(
    loaderCode,
    /<Shell\s+active=\{active\}>/,
    "PageLoading no longer renders inside <Shell> — accepting it as shell-preserving would be a lie",
  );
  assert.doesNotMatch(
    loaderCode,
    /<main\b/,
    "PageLoading renders a <main> of its own; the page's content root belongs to the page, and a full-viewport <main> here re-creates the old bare loading gate",
  );
  // The loading affordance a screen reader actually receives.
  assert.match(loaderCode, /role="status"/, "PageLoading is not announced as a status");
  assert.match(loaderCode, /aria-busy="true"/, "PageLoading does not mark itself busy");
  assert.match(
    loaderCode,
    /className="sr-only">\{t\("common\.loading"\)\}/,
    "PageLoading's skeleton bars are decorative with no accessible name — without the sr-only line a screen reader gets nothing",
  );
  // Decorative bars must stay hidden from assistive tech.
  assert.match(loaderCode, /aria-hidden="true"/, "PageLoading's skeleton bars are exposed to assistive tech");
  // The requirement is "a skeleton, not a blank screen" — the loading state has
  // to actually paint something. Card t_d5336ec6 kept that requirement and
  // changed the class that satisfies it: the bare `animate-pulse` was infinite
  // and immediate, so a fast response showed half a flash before the content
  // arrived. `.jf-skeleton` is the same painted skeleton with a finite, delayed
  // pulse (globals.css).
  assert.match(loaderCode, /jf-skeleton/, "PageLoading paints no skeleton at all — an empty content area is the blank frame this card complains about");
});

test("the loader's status text exists in all five locales", () => {
  const entry = DICT["common.loading" as keyof typeof DICT] as Record<string, unknown> | undefined;
  assert.ok(entry, "common.loading is missing from the dictionary — PageLoading would render its raw key");
  const missing = LANGS.filter((l) => typeof entry[l] !== "string" || String(entry[l]).trim() === "");
  assert.deepEqual(
    missing,
    [],
    `common.loading is untranslated for ${missing.join("/")} — a page-loading state that silently falls back to English`,
  );
});
