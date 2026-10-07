// Guard: a component a page renders must be DECLARED AT MODULE SCOPE, not inside
// another component's body.
//
// THE DEFECT (found by the `lint` gate the first time it ran — react-hooks v7's
// `static-components`)
//   `Shell.tsx` declared `UserAvatar` and `NavList` inside `export function
//   Shell(...)`, and `products/page.tsx` declared `SortHeader` inside the page
//   component. React compares element TYPES by identity, so a component type
//   created during a render is a NEW type on every render: React unmounts the
//   whole subtree and mounts a fresh one. The avatar `<img>` reloads, the nav
//   loses its DOM state, and each `<th>` is re-created — on every state change,
//   including the ones these components themselves cause.
//
//   It is invisible to `tsc`, to `next build` and to every test: the JSX is
//   valid, the props are valid, the render output is byte-identical. Only a
//   linter that models React's reconciliation sees it, which is why adding the
//   linter is what surfaced it (three sites, fixed in this change).
//
// WHAT THIS PINS
//   The rule is structural and is read off the tree rather than listed by hand,
//   so it cannot rot when a file is added:
//
//     a declaration that RETURNS JSX and is INDENTED inside another function
//     body is a component created during render.
//
//   Both forms are covered: `function Name(...) { ... return (<div/>) }` and the
//   arrow form `const Name = (...) => ( <div/> )`. A declaration at column 0 is
//   at module scope and is the shape this guard requires.
//
// WHAT IT DOES NOT COVER (stated honestly)
//   * A component produced by a factory call during render (`motion.create(...)`),
//     or one returned conditionally — the `lint` gate's
//     `react-hooks/static-components` covers those; this file is the cheap
//     regression guard for the sites that WERE in the tree, and it runs inside
//     `npm run test:unit` where the linter does not.
//   * Non-`.tsx`/`.jsx` files: a component with no JSX in it cannot be seen here.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url)); // <repo>/web/src/lib
const WEB_DIR = resolve(HERE, "..", ".."); //            <repo>/web
const SRC_DIR = join(WEB_DIR, "src");

const SKIP_DIRS = new Set(["node_modules", ".next", "out", "coverage", "test-results", "playwright-report"]);

function walkTsx(dir: string, out: string[] = []): string[] {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (entry.isDirectory()) {
      if (SKIP_DIRS.has(entry.name)) continue;
      walkTsx(join(dir, entry.name), out);
    } else if (entry.isFile() && (entry.name.endsWith(".tsx") || entry.name.endsWith(".jsx"))) {
      out.push(join(dir, entry.name));
    }
  }
  return out;
}

/** Strip line comments and string/template literal contents, keeping the code
 *  shape (and the line count) so an index into the copy is an index into the
 *  original. A `// <div/>` in prose must not read as JSX. */
function maskNonCode(src: string): string {
  const out: string[] = [];
  for (const line of src.split(/\r?\n/)) {
    out.push(line.replace(/\/\/.*$/, ""));
  }
  return out.join("\n");
}

/**
 * The source span of the declaration that starts at `declStart` — its body.
 *
 * Two shapes, told apart the way the parser would:
 *   * a `function`/braced body: the first `{` that is NOT inside the parameter
 *     list (paren depth 0), through its matching `}`. Skipping the parens matters
 *     — `function UserAvatar({ className }: { className: string }) {` has its
 *     FIRST brace inside the destructured parameter, and stopping there yields a
 *     span with no body in it (which is exactly how the first version of this
 *     guard failed to bite).
 *   * an arrow with an EXPRESSION body (`const X = () => ( <div/> )`): from the
 *     `=>` to the end of the statement — taken as the matching `)` of the
 *     wrapping paren, else to the first line at or below the declaration's
 *     indentation.
 *
 * Returns null when neither shape can be delimited (the guard then stays silent
 * about it rather than reporting a declaration it cannot read).
 */
function bodySpan(masked: string, declStart: number, indent: number): string | null {
  let paren = 0;
  let arrowAt = -1;
  for (let i = declStart; i < masked.length; i++) {
    const ch = masked[i];
    if (ch === "(") paren++;
    else if (ch === ")") paren--;
    else if (ch === "=" && masked[i + 1] === ">" && paren === 0) {
      arrowAt = i + 2;
    } else if (ch === "{" && paren === 0) {
      // Braced body: walk to the matching close.
      let depth = 0;
      for (let j = i; j < masked.length; j++) {
        if (masked[j] === "{") depth++;
        else if (masked[j] === "}") {
          depth--;
          if (depth === 0) return masked.slice(i, j + 1);
        }
      }
      return null;
    } else if (ch === "\n" && arrowAt >= 0) {
      // An arrow whose body never opened a brace — take the statement.
      const tail = masked.slice(arrowAt, i);
      if (tail.trim() !== "") return tail;
    }
  }
  if (arrowAt >= 0) {
    // Expression body: through the matching `)` of a wrapping paren, else the
    // next line at or below this declaration's indentation.
    const after = masked.slice(arrowAt);
    const open = after.indexOf("(");
    const openBrace = after.indexOf("{");
    if (open !== -1 && (openBrace === -1 || open < openBrace)) {
      let depth = 0;
      for (let j = open; j < after.length; j++) {
        if (after[j] === "(") depth++;
        else if (after[j] === ")") {
          depth--;
          if (depth === 0) return after.slice(0, j + 1);
        }
      }
    }
    const lines = after.split("\n");
    let end = after.length;
    let at = 0;
    for (const line of lines) {
      at += line.length + 1;
      if (line.trim() !== "" && line.match(/^[ \t]*/)?.[0].length === indent) {
        end = at;
        break;
      }
    }
    return after.slice(0, end);
  }
  return null;
}

/**
 * Every nested component: a declaration whose BODY RETURNS JSX and which sits at
 * brace depth >= 1, i.e. inside another function.
 *
 * Depth is counted over the comment-stripped source, so a module-scope
 * declaration (depth 0) is never reported and a declaration inside `Shell` is.
 */
function componentsCreatedDuringRender(src: string): { line: number; name: string }[] {
  const masked = maskNonCode(src);
  // Offsets of each line start, so an offset gives a line number.
  const lineStarts: number[] = [0];
  for (let i = 0; i < masked.length; i++) if (masked[i] === "\n") lineStarts.push(i + 1);
  const lineOf = (offset: number): number => {
    let lo = 0;
    let hi = lineStarts.length - 1;
    while (lo < hi) {
      const mid = (lo + hi + 1) >> 1;
      if (lineStarts[mid] <= offset) lo = mid;
      else hi = mid - 1;
    }
    return lo + 1;
  };

  // Both declaration forms, both requiring indentation (so they are nested).
  const patterns = [
    /^([ \t]+)function\s+([A-Z][A-Za-z0-9_]*)\s*(?:<[^>{}]*>)?\s*\(/gm,
    /^([ \t]+)(?:export\s+)?const\s+([A-Z][A-Za-z0-9_]*)\s*(?::[^=\n]*)?=\s*(?:async\s*)?\(/gm,
    /^([ \t]+)(?:export\s+)?const\s+([A-Z][A-Za-z0-9_]*)\s*(?::[^=\n]*)?=\s*(?:async\s*)?\w+\s*=>/gm,
  ];

  const seen = new Set<string>();
  const offenders: { line: number; name: string }[] = [];
  for (const re of patterns) {
    re.lastIndex = 0;
    for (;;) {
      const m = re.exec(masked);
      if (!m) break;
      const start = m.index;
      const indent = m[1].length;
      const name = m[2];
      // Depth at the declaration itself.
      let depth = 0;
      for (let i = 0; i < start; i++) {
        if (masked[i] === "{") depth++;
        else if (masked[i] === "}") depth--;
      }
      if (depth < 1) continue;
      const span = bodySpan(masked, start, indent);
      if (!span) continue;
      // A body that returns JSX. `return (` on one line and `<Tag` on the next
      // is this repo's formatting, so the match spans lines; the arrow form's
      // expression body starts with the JSX directly.
      const returnsJsx = /\breturn\b[\s\S]{0,120}?<\s*(?:[A-Za-z][\w.]*|>)/.test(span)
        || /=>\s*\(?\s*<\s*[A-Za-z]/.test(span);
      if (!returnsJsx) continue;
      const key = `${name}@${lineOf(start)}`;
      if (seen.has(key)) continue;
      seen.add(key);
      offenders.push({ line: lineOf(start), name });
    }
  }
  return offenders.sort((a, b) => a.line - b.line);
}

test("no component is declared inside another component (module scope, or it remounts on every render)", () => {
  const files = walkTsx(SRC_DIR);
  // The walk must have found the tree, or this check passes vacuously.
  assert.ok(
    files.length >= 40,
    `the .tsx walk found only ${files.length} file(s) under ${SRC_DIR} — the sweep is broken and an empty set would pass vacuously`,
  );
  const offenders: string[] = [];
  for (const file of files) {
    const src = readFileSync(file, "utf8");
    for (const o of componentsCreatedDuringRender(src)) {
      offenders.push(
        `  ${relative(WEB_DIR, file)}:${o.line} declares component \`${o.name}\` inside another function — ` +
          `React sees a new type every render and unmounts/remounts its subtree (react-hooks/static-components). ` +
          `Move it to module scope and pass what it closes over as props.`,
      );
    }
  }
  assert.deepEqual(
    offenders,
    [],
    "a component is created during render, so its subtree is destroyed and re-created on every render " +
      "(the avatar reloads, the nav loses DOM state, each header cell is rebuilt):\n" +
      offenders.join("\n"),
  );
});
