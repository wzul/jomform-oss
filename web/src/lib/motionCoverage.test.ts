// Guard for the motion layer's WIRING (card t_ea568318).
//
// THE DEFECT THIS EXISTS FOR
//   The motion layer (card t_d5336ec6) declared five patterns in
//   `web/src/app/globals.css` and applied TWO of them. `.jf-enter`, `.jf-ok-in`
//   and `.jf-toast-in` were defined, styled, guarded and shipped — and no
//   component ever put them on an element, so no user ever saw them. The owner
//   clicked "New form" and moved between dashboard pages and reported "no
//   motion effect", which was accurate: the dashboard route container had no
//   animation at all and the toast appeared as a jump.
//
//   `motion.test.ts` cannot see that. It guards the stylesheet's CONTENT (tokens,
//   ceilings, the reduced-motion baseline); a class nobody applies is perfectly
//   valid CSS, so every one of its assertions stayed green through the defect.
//   This file guards the other half: is each declared pattern wired to a
//   surface, and does the reduced-motion block switch off everything that
//   animates?
//
// THE RULES, and why each is a rule
//   1. every `.jf-*` class the stylesheet gives motion to (an `animation:` or
//      `transition:` declaration) must be APPLIED in a SHIPPED web file — a
//      `.tsx`/`.ts` under `web/src` that is not a test. A class no component
//      applies is dead weight that reads like a shipped feature. The Go-rendered
//      public page (`/f/*`) is deliberately NOT part of this corpus: it carries
//      its OWN copy of the motion rules in `publicMotionCSS`
//      (api/internal/httpapi/render_blocks.go) and is pinned there by
//      `api/internal/httpapi/motion_test.go`, so a globals.css class applied
//      only in Go would still ship dead in the web bundle.
//   2. every class that ANIMATES must appear in the reduced-motion block's
//      `animation: none !important` list. That is the branch a new route
//      transition is most likely to be added without, and the generic
//      `animation-duration: 0.01ms` collapse is NOT enough: a 0.01ms animation
//      still paints its progress-0 frame, i.e. `opacity: 0` for one frame.
//   3. the dashboard route container exists and is keyed on the pathname, so the
//      entrance fires on a ROUTE CHANGE (a new node) and not on a re-render (the
//      same node — a CSS animation would not replay).
//
// The two checkers take their input as arguments on purpose: the control tests
// hand them a doctored stylesheet / corpus and require them to name the defect.
// Without that, "the real tree is clean" could mean only that the checker finds
// nothing anywhere.

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = fileURLToPath(new URL(".", import.meta.url)); // web/src/lib/
const WEB_SRC = join(HERE, ".."); // web/src/

const CSS = readFileSync(join(WEB_SRC, "app", "globals.css"), "utf8");

/**
 * Strip `//` and block comments while LEAVING STRING LITERALS INTACT.
 *
 * A plain regex cannot do this: `https://…` is not a comment, and a comment's
 * prose about `.jf-enter { animation: … }` must not read as a rule. The scanner
 * walks the source, and inside a string it only tracks the closing quote (with
 * escapes) so a `//` in a URL is kept as data.
 */
export function stripComments(src: string): string {
  let out = "";
  let i = 0;
  let mode: "code" | "line" | "block" | "sq" | "dq" | "tpl" = "code";
  while (i < src.length) {
    const c = src[i];
    const n = src[i + 1];
    if (mode === "code") {
      if (c === "/" && n === "/") {
        mode = "line";
        i += 2;
        continue;
      }
      if (c === "/" && n === "*") {
        mode = "block";
        i += 2;
        continue;
      }
      if (c === '"') mode = "dq";
      else if (c === "'") mode = "sq";
      else if (c === "`") mode = "tpl";
      out += c;
      i++;
      continue;
    }
    if (mode === "line") {
      if (c === "\n") {
        mode = "code";
        out += c;
      }
      i++;
      continue;
    }
    if (mode === "block") {
      if (c === "*" && n === "/") {
        mode = "code";
        i += 2;
        continue;
      }
      i++;
      continue;
    }
    // inside a string literal
    out += c;
    if (c === "\\") {
      if (n !== undefined) out += n;
      i += 2;
      continue;
    }
    if ((mode === "dq" && c === '"') || (mode === "sq" && c === "'") || (mode === "tpl" && c === "`")) {
      mode = "code";
    }
    i++;
  }
  return out;
}

const STRING_LITERAL = /"(?:[^"\\\n]|\\.)*"|'(?:[^'\\\n]|\\.)*'|`(?:[^`\\]|\\.)*`/g;

/** Every motion declaration in a stylesheet, as `{ cls, kind }`. */
export function motionRules(css: string): { cls: string; kind: "animation" | "transition" }[] {
  const src = stripComments(css);
  const out: { cls: string; kind: "animation" | "transition" }[] = [];
  for (const rule of src.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    const body = rule[2];
    const animates = /(?:^|;)\s*animation\s*:\s*(?!none\b)[^;]+/.test(body);
    const transits = /(?:^|;)\s*transition\s*:\s*(?!none\b)[^;]+/.test(body);
    if (!animates && !transits) continue;
    for (const m of rule[1].matchAll(/\.jf-[A-Za-z0-9_-]+/g)) {
      out.push({ cls: m[0].slice(1), kind: animates ? "animation" : "transition" });
    }
  }
  return out;
}

/** The distinct classes `motionRules` finds. */
export function motionClasses(css: string): string[] {
  return [...new Set(motionRules(css).map((r) => r.cls))].sort();
}

function tokenRe(cls: string): RegExp {
  return new RegExp(`(?<![\\w-])${cls}(?![\\w-])`);
}

/**
 * True when a TS/TSX file applies `cls`: the name occurs INSIDE A STRING
 * LITERAL (a `className`, a `classList` call, a class-string constant). Prose in
 * a comment is stripped first, and a bare word in code is not an application.
 */
export function jsApplies(text: string, cls: string): boolean {
  const code = stripComments(text);
  const re = tokenRe(cls);
  for (const m of code.matchAll(STRING_LITERAL)) if (re.test(m[0])) return true;
  return false;
}

export type Source = { path: string; text: string };

/** Classes the stylesheet gives motion to that no shipped web file applies. */
export function motionCoverageFindings(css: string, sources: Source[]): string[] {
  const out: string[] = [];
  for (const cls of motionClasses(css)) {
    if (!sources.some((f) => jsApplies(f.text, cls))) {
      out.push(`.${cls} is defined in the stylesheet and applied in no component`);
    }
  }
  return out;
}

/** Animating classes the reduced-motion block does not switch off. */
export function reducedMotionFindings(css: string): string[] {
  const src = stripComments(css);
  const at = src.indexOf("@media (prefers-reduced-motion: reduce)");
  if (at < 0) return ["no @media (prefers-reduced-motion: reduce) block"];
  const tail = src.slice(at);

  const offRule = /([^{}]+)\{\s*animation:\s*none\s*!important;?\s*\}/.exec(tail);
  if (!offRule) return ["the reduced-motion block never switches an animation off (`animation: none !important`)"];

  const switched = new Set<string>();
  for (const m of offRule[1].matchAll(/\.jf-[A-Za-z0-9_-]+/g)) switched.add(m[0].slice(1));

  const out: string[] = [];
  for (const { cls, kind } of motionRules(css)) {
    if (kind !== "animation") continue;
    if (!switched.has(cls)) out.push(`.${cls} animates but the reduced-motion block does not switch it off`);
  }
  if (!tail.includes("transition-duration")) {
    out.push("the reduced-motion block never collapses transition-duration");
  }
  return out;
}

/** Every shipped file under `root` that passes `accept`, as `{ path, text }`. */
function walk(root: string, accept: (p: string) => boolean, out: string[] = []): string[] {
  for (const entry of readdirSync(root)) {
    if (entry === "node_modules" || entry === ".next") continue;
    const p = join(root, entry);
    const st = statSync(p);
    if (st.isDirectory()) walk(p, accept, out);
    else if (accept(p)) out.push(p);
  }
  return out;
}

function isShippedJs(p: string): boolean {
  return (p.endsWith(".ts") || p.endsWith(".tsx")) && !/\.test\.(ts|tsx)$/.test(p) && !p.endsWith(".d.ts");
}

function realCorpus(): Source[] {
  return walk(WEB_SRC, isShippedJs).map((path) => ({ path, text: readFileSync(path, "utf8") }));
}

test("every motion class the stylesheet defines is applied on a shipped surface", () => {
  const findings = motionCoverageFindings(CSS, realCorpus());
  assert.deepEqual(findings, []);
});

test("the coverage checker BITES: a class applied nowhere is named", () => {
  // (a) The exact pre-card tree: the toast class is defined and Toast.tsx does
  // not carry it. This is the defect the card was filed for.
  const corpus = realCorpus();
  const withoutToast = corpus.filter((f) => !f.path.endsWith("Toast.tsx"));
  const got = motionCoverageFindings(CSS, withoutToast);
  assert.ok(
    got.some((f) => f.includes("jf-toast-in")),
    `dropping the toast's application did NOT bite: ${JSON.stringify(got)}`,
  );

  // (b) A pattern added to the stylesheet and wired nowhere at all.
  const extra =
    CSS + "\n.jf-new-pattern { animation: jf-rise-in var(--jf-dur-base) var(--jf-ease-out) both; }\n";
  const got2 = motionCoverageFindings(extra, corpus);
  assert.ok(
    got2.some((f) => f.includes("jf-new-pattern")),
    `a class no component applies did NOT bite: ${JSON.stringify(got2)}`,
  );
});

test("every animating class is switched off by the reduced-motion block", () => {
  assert.deepEqual(reducedMotionFindings(CSS), []);
});

test("the reduced-motion checker BITES: a route transition with no branch is named", () => {
  // A route transition added WITHOUT the reduced-motion branch — the case the
  // card asks the guard to catch.
  const withRoute =
    CSS + "\n.jf-route-in { animation: jf-rise-in var(--jf-dur-base) var(--jf-ease-out) both; }\n";
  const got = reducedMotionFindings(withRoute);
  assert.ok(
    got.some((f) => f.includes("jf-route-in")),
    `a route transition with no reduced-motion branch was NOT named: ${JSON.stringify(got)}`,
  );

  // The other plant: the real route container's class dropped from the
  // switch-off list while still animating.
  const noEnter = CSS.replace(/\n\s*\.jf-enter,/, "");
  assert.notEqual(noEnter, CSS, "the control could not find `.jf-enter,` in the switch-off list");
  const got2 = reducedMotionFindings(noEnter);
  assert.ok(
    got2.some((f) => f.includes("jf-enter")),
    `dropping .jf-enter from the reduced-motion list was NOT named: ${JSON.stringify(got2)}`,
  );
});

test("the dashboard route container is keyed on the pathname", () => {
  const shell = readFileSync(join(WEB_SRC, "components", "Shell.tsx"), "utf8");
  assert.match(shell, /usePathname/, "Shell does not read the pathname — a route entrance cannot fire per route");
  // The key and the entrance class must be on the SAME element, and that element
  // must be the content container: a `key` on an ancestor would remount the
  // chrome (sidebar/topbar state) and an animation class on a wrapper that is
  // not the route's own node would animate the chrome on every navigation.
  assert.match(
    shell,
    /<main\b[^>]*\bkey=\{pathname\}[^>]*className="jf-enter\b[^"]*"[^>]*>\s*\{children\}\s*<\/main>/,
    "the dashboard's content container is not a <main> keyed on the pathname and carrying .jf-enter around {children} — " +
      "the route entrance would either never replay (no key) or fire on a re-render (not keyed), and the owner's " +
      "\"no motion effect\" on navigation is back",
  );
});
