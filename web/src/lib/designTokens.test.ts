// The design-system gate (card t_f2e231d3).
//
// WHY A UNIT TEST AND NOT ONLY `scripts/design-guard.py`. That gate already fails a
// raw hex or a `text-[Npx]` in the tree, and it is the right place for those two
// rules. What it cannot see is the layer that did not exist when it was written:
//
//   1. the PRIMITIVE LAYER must itself be literal-free — `components/ui/**` is
//      excluded from the general scan (a primitive may one day need a value no
//      token covers), so without this file 15 new components could carry hex
//      through a gate that had been told to skip the directory;
//   2. every colour TOKEN must have a measured contrast pairing, and the numbers
//      the /design page prints must be recomputed from `globals.css` rather than
//      trusted — a palette change that leaves a stale ratio on a documentation
//      page is the exact rot this test exists to stop;
//   3. every primitive that exists must be SHOWN at /design, so the page stays a
//      manifest an agent can read instead of a snapshot of the day it was written;
//   4. a `--jf-*` declared anywhere but `globals.css` is a second token layer, and
//      two token layers drift by construction.
//
// THE RULES ARE PURE FUNCTIONS OVER (path, text) PAIRS (`scanHex`,
// `scanTokenDeclarations`) and each has a control test below that hands it a
// planted defect and requires the finding. Without those controls, "the tree is
// clean" could mean only that the scanners find nothing anywhere.
//
// Run with:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

import { ARBITRARY_PX_ALLOWLIST, DESIGN_TOKEN_ALLOWLIST, type CountAllowlist } from "./designTokenAllowlist.ts";
import { BARREL_FAMILY, PRIMITIVE_STATES, STATES } from "./designStates.ts";
import {
  DARK_CONTRASTS,
  FORBIDDEN_TEXT_PAIRS,
  LIGHT_CONTRASTS,
  type ContrastPair,
} from "./designTokenRatios.ts";
import { RADIUS_SCALE, SHADOW_SCALE, SPACE_SCALE, TYPE_SCALE } from "./designTokenScale.ts";

const HERE = fileURLToPath(new URL(".", import.meta.url)); // web/src/lib/
const SRC = join(HERE, ".."); // web/src/
const WEB = join(SRC, ".."); // web/
const GLOBALS = join(SRC, "app", "globals.css");
const UI_DIR = join(SRC, "components", "ui");
const GALLERY = join(SRC, "app", "design", "page.tsx");

const read = (path: string) => readFileSync(path, "utf8");

type SourceFile = { path: string; text: string };

/** Every file under `dir` whose name matches `want`, as (repo-relative path, text). */
function filesUnder(dir: string, want: (name: string) => boolean): SourceFile[] {
  const out: SourceFile[] = [];
  const walk = (current: string) => {
    for (const entry of readdirSync(current, { withFileTypes: true })) {
      const full = join(current, entry.name);
      if (entry.isDirectory()) {
        if (entry.name === "node_modules" || entry.name === ".next") continue;
        walk(full);
      } else if (want(entry.name)) {
        out.push({ path: full.slice(SRC.length + 1).replaceAll("\\", "/"), text: read(full) });
      }
    }
  };
  walk(dir);
  return out;
}

/** Files as the scanner sees them: repo-relative under `web/src`, forward slashes. */
const shippedTsx = () => filesUnder(SRC, (n) => n.endsWith(".tsx") && !n.endsWith(".test.tsx"));

// ─── comments and exemptions ─────────────────────────────────────────────────

const EXEMPT = /design-tokens-exempt:(?!-end)\s*(.*)$/;
const END = /design-tokens-exempt-end\b/;
const MIN_REASON = 8;
const STANDALONE = /^(?:\/\/|\/\*|\{\/\*|\*|\/\*\*)?\s*$/;

/**
 * Line numbers (1-based) the documented exemption directive covers, plus any
 * structural error. Same two forms the Python gate honours — a trailing directive
 * exempts its own line, a standalone one opens a block that runs to
 * `design-tokens-exempt-end` — so the two gates cannot disagree about which
 * literal is justified.
 */
function exemptions(text: string): { covered: Set<number>; errors: string[] } {
  const covered = new Set<number>();
  const errors: string[] = [];
  const lines = text.split("\n");
  let open: number | null = null;
  lines.forEach((line, i) => {
    const n = i + 1;
    if (END.test(line)) {
      if (open === null) errors.push(`line ${n}: \`design-tokens-exempt-end\` with no open block`);
      else {
        for (let k = open; k <= n; k += 1) covered.add(k);
        open = null;
      }
      return;
    }
    const m = EXEMPT.exec(line);
    if (!m) return;
    const reason = m[1].replace(/[\s*/}]+$/, "");
    if (reason.length < MIN_REASON) {
      errors.push(`line ${n}: \`design-tokens-exempt:\` with no reason of ${MIN_REASON}+ chars`);
      return;
    }
    if (STANDALONE.test(line.slice(0, m.index).trim())) {
      if (open !== null) errors.push(`line ${n}: nested exemption block (opened before line ${open})`);
      else open = n + 1;
    } else {
      covered.add(n);
    }
  });
  if (open !== null) errors.push(`line ${open}: an exemption block is never closed`);
  return { covered, errors };
}

/** Blank out comment TEXT, keeping every line and column where the author wrote it. */
function stripComments(text: string): string[] {
  const out: string[] = [];
  let inBlock = false;
  for (const line of text.split("\n")) {
    const chars = [...line];
    let i = 0;
    let inString: string | null = null;
    while (i < line.length) {
      if (inBlock) {
        const end = line.indexOf("*/", i);
        if (end < 0) {
          for (let k = i; k < line.length; k += 1) chars[k] = " ";
          i = line.length;
        } else {
          for (let k = i; k < end + 2; k += 1) chars[k] = " ";
          i = end + 2;
          inBlock = false;
        }
        continue;
      }
      const ch = line[i];
      if (inString) {
        if (ch === "\\") i += 2;
        else {
          if (ch === inString) inString = null;
          i += 1;
        }
        continue;
      }
      if (ch === '"' || ch === "'" || ch === "`") {
        inString = ch;
        i += 1;
        continue;
      }
      if (ch === "/" && line[i + 1] === "*") {
        inBlock = true;
        chars[i] = chars[i + 1] = " ";
        i += 2;
        continue;
      }
      if (ch === "/" && line[i + 1] === "/" && line[i - 1] !== ":") {
        for (let k = i; k < line.length; k += 1) chars[k] = " ";
        i = line.length;
        continue;
      }
      i += 1;
    }
    out.push(chars.join(""));
  }
  return out;
}

// ─── rule 1: raw colour literals ─────────────────────────────────────────────

const HEX6 = /#([0-9a-fA-F]{6})(?![0-9a-fA-F])/g;
const HEX3 = /(?<![&\w])#([0-9a-fA-F]{3})(?![0-9a-zA-Z_-])/g;
/** `bg-[#fff]`, `text-[#17181c]`, `border-[#…]` — a colour utility by literal. */
const ARBITRARY_COLOUR =
  /\b(?:bg|text|border|ring|fill|stroke|from|via|to|outline|shadow|decoration|divide|placeholder|caret|accent)-\[#/g;

export type Finding = { path: string; line: number; what: string };

/**
 * Rule 1 over a set of files.
 *
 * `skip` is a path prefix (the primitive layer). The ALLOWLIST is not an argument:
 * allowlisting is a per-file COUNT policy applied by `checkShrinkOnly` after the
 * scan, so the scanner answers "what is here" and the allowlist answers "how much
 * of it is already paid for". Two questions, two functions — a scanner that skips
 * allowlisted files cannot report the count it skipped, and a count that is never
 * reported is a debt that cannot shrink visibly.
 */
export function scanHex(
  files: readonly SourceFile[],
  { skip = [] }: { skip?: readonly string[] } = {},
): Finding[] {
  const findings: Finding[] = [];
  for (const file of files) {
    if (skip.some((p) => file.path.startsWith(p))) continue;
    const { covered, errors } = exemptions(file.text);
    for (const e of errors) findings.push({ path: file.path, line: 0, what: `broken exemption: ${e}` });
    stripComments(file.text).forEach((line, i) => {
      const n = i + 1;
      if (covered.has(n)) return;
      const hexes = [...line.matchAll(HEX6)].map((m) => `#${m[1]}`.toLowerCase());
      for (const m of line.matchAll(HEX3)) {
        if (/[a-f]/i.test(m[1])) hexes.push(`#${m[1]}`.toLowerCase());
      }
      if (hexes.length > 0) findings.push({ path: file.path, line: n, what: `raw hex ${hexes.join(" ")}` });
      for (const m of line.matchAll(ARBITRARY_COLOUR)) {
        findings.push({ path: file.path, line: n, what: `arbitrary colour utility ${m[0]}…]` });
      }
      if (/(^|\s)text-\[\d+px\]/.test(line)) {
        findings.push({ path: file.path, line: n, what: "arbitrary font size text-[<n>px]" });
      }
    });
  }
  return findings;
}

// ─── rule 2: one token declaration site ──────────────────────────────────────

const DECLARATION = /(^|[\s;{,'"])(--jf-[a-z0-9-]+)\s*:/;

/** Rule 2 over a set of files: a `--jf-*` declared anywhere but `app/globals.css`. */
export function scanTokenDeclarations(files: readonly SourceFile[]): Finding[] {
  const findings: Finding[] = [];
  for (const file of files) {
    if (file.path === "app/globals.css") continue;
    const text = file.path.endsWith(".css") ? file.text.replace(/\/\*[\s\S]*?\*\//g, "") : stripComments(file.text).join("\n");
    text.split("\n").forEach((line, i) => {
      const m = DECLARATION.exec(line);
      if (m) findings.push({ path: file.path, line: i + 1, what: `declares ${m[2]}` });
    });
  }
  return findings;
}

// ─── rule 3: an arbitrary length a scale step already names ──────────────────

/** `[44px]`, `[-260px]` — an arbitrary length in any utility (`min-h`, `w`, `rounded`). */
const PX_LITERAL = /\[(-?\d+)px\]/g;

/**
 * The pixel values a scale step already names, as a set.
 *
 * Built from the scale DATA, not typed here: add a step to `designTokenScale.ts`
 * (and its token to `globals.css`) and this rule starts covering it in the same
 * commit. The set deliberately does NOT include every number the app uses — see
 * `ARBITRARY_PX_ALLOWLIST` for why `w-[720px]` is none of this rule's business.
 */
const STEP_PX: ReadonlySet<number> = new Set([
  ...SPACE_SCALE.map((step) => step.px),
  ...TYPE_SCALE.map((step) => step.px),
]);

/**
 * Rule 3 over a set of files: `[<n>px]` where the scale already names `<n>px`.
 *
 * `steps` is an argument so the control test can hand it a planted step and prove
 * the scanner bites with the real step set in place.
 */
export function scanScalePx(
  files: readonly SourceFile[],
  steps: ReadonlySet<number> = STEP_PX,
): Finding[] {
  const findings: Finding[] = [];
  for (const file of files) {
    const { covered, errors } = exemptions(file.text);
    for (const e of errors) findings.push({ path: file.path, line: 0, what: `broken exemption: ${e}` });
    stripComments(file.text).forEach((line, i) => {
      const n = i + 1;
      if (covered.has(n)) return;
      for (const m of line.matchAll(PX_LITERAL)) {
        const px = Math.abs(Number(m[1]));
        if (!steps.has(px)) continue;
        findings.push({
          path: file.path,
          line: n,
          what: `arbitrary ${m[0]} — the scale already names ${px}px (use the token utility)`,
        });
      }
    });
  }
  return findings;
}

// ─── the shrink-only allowlist ───────────────────────────────────────────────

export type ShrinkReport = {
  /** One line per file with a finding, sorted — the printed proof that the count moved. */
  lines: string[];
  /** The one-line total, printed on every run. */
  total: string;
  /** Everything wrong with the counts. Empty means the list is honest. */
  errors: string[];
};

/**
 * Apply a per-file count allowlist to a scan's findings. THE THREE RULES OF A
 * SHRINK-ONLY LIST, in one place so both lists cannot drift apart:
 *
 *   1. a file with findings that is NOT on the list is an error (a new deviation
 *      fails the build — an allowlist entry is a seeded debt, not a parking space);
 *   2. a file on the list with MORE findings than it was seeded with is an error
 *      (it may get better, never worse);
 *   3. a file on the list with ZERO findings is an error (delete the entry — a
 *      whitelist that outlives its drift is how a cleaned file silently becomes
 *      re-open for new drift without anyone deciding that).
 *
 * The returned `total` is printed by the caller, which is what makes the shrink
 * visible in the gate's output instead of something a reviewer has to infer from
 * a diff.
 */
export function checkShrinkOnly(
  findings: readonly Finding[],
  seeded: CountAllowlist,
  label: string,
): ShrinkReport {
  const counts = new Map<string, number>();
  for (const finding of findings) counts.set(finding.path, (counts.get(finding.path) ?? 0) + 1);

  const errors: string[] = [];
  const lines: string[] = [];
  let allowlisted = 0;

  for (const path of [...counts.keys()].sort()) {
    const count = counts.get(path)!;
    const cap = seeded[path];
    if (cap === undefined) {
      errors.push(
        `${path}: ${count} finding(s) and it is not on the allowlist — use the token, or justify THIS line with ` +
          "`design-tokens-exempt: <reason>`; the allowlist is a seeded debt and may only shrink",
      );
      lines.push(`${path}: ${count} (not allowlisted)`);
      continue;
    }
    allowlisted += count;
    if (count > cap) errors.push(`${path}: ${count} finding(s), seeded at ${cap} — the allowlist may only SHRINK`);
    lines.push(`${path}: ${count}/${cap}`);
  }

  for (const path of Object.keys(seeded).sort()) {
    if ((counts.get(path) ?? 0) === 0) {
      errors.push(`${path}: seeded at ${seeded[path]} but now carries NONE — delete the entry; a cleaned file must not stay whitelisted`);
    }
  }

  const seededTotal = Object.values(seeded).reduce((a, b) => a + b, 0);
  return {
    lines,
    errors,
    total: `${label}: ${findings.length} finding(s) in ${counts.size} file(s), ${allowlisted} allowlisted (seeded ${seededTotal})`,
  };
}

// ─── colour maths ────────────────────────────────────────────────────────────

type Rgba = [number, number, number, number];

function hexToRgba(value: string): Rgba {
  const m = /^#([0-9a-fA-F]{3,8})$/.exec(value.trim());
  assert.ok(m, `not a hex colour: ${value}`);
  let h = m[1];
  if (h.length === 3) h = [...h].map((c) => c + c).join("");
  const at = (i: number) => parseInt(h.slice(i, i + 2), 16);
  if (h.length === 6) return [at(0), at(2), at(4), 255];
  if (h.length === 8) return [at(0), at(2), at(4), at(6)];
  throw new Error(`unsupported hex length: ${value}`);
}

const over = (fg: Rgba, bg: Rgba): Rgba => {
  const a = fg[3] / 255;
  return [
    Math.round(fg[0] * a + bg[0] * (1 - a)),
    Math.round(fg[1] * a + bg[1] * (1 - a)),
    Math.round(fg[2] * a + bg[2] * (1 - a)),
    255,
  ];
};

function luminance(c: Rgba): number {
  const ch = c.slice(0, 3).map((v) => {
    const s = v / 255;
    return s <= 0.04045 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * ch[0] + 0.7152 * ch[1] + 0.0722 * ch[2];
}

function contrast(a: Rgba, b: Rgba): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

// ─── the stylesheet, parsed ──────────────────────────────────────────────────

const CSS = read(GLOBALS).replace(/\/\*[\s\S]*?\*\//g, "");
const DARK_AT = CSS.indexOf('[data-theme="dark"]');
assert.ok(DARK_AT > 0, "globals.css has no [data-theme=\"dark\"] block — the dark palette is part of this card");

function tokensIn(css: string): Map<string, string> {
  const out = new Map<string, string>();
  for (const m of css.matchAll(/(--jf-[a-z0-9-]+)\s*:\s*([^;]+);/g)) out.set(m[1], m[2].trim());
  return out;
}

const LIGHT = tokensIn(CSS.slice(0, DARK_AT));
const DARK = tokensIn(CSS.slice(DARK_AT, CSS.indexOf("\n}", DARK_AT)));
const THEME = CSS.slice(CSS.indexOf("@theme"));

const isColour = (v: string) => /^#[0-9a-fA-F]{3,8}$/.test(v);
const colourTokens = (tokens: Map<string, string>) =>
  [...tokens.entries()].filter(([, v]) => isColour(v)).map(([k]) => k);

function resolved(theme: Map<string, string>): Map<string, string> {
  return new Map([...LIGHT, ...theme]);
}

/** The ratio a gallery row stands for, recomputed from the stylesheet. */
function measure(row: ContrastPair, theme: Map<string, string>): number {
  const table = resolved(theme);
  const fg = table.get(row.fg);
  const bg = table.get(row.bg);
  assert.ok(fg, `${row.fg} is not declared in globals.css — the gallery row is stale`);
  assert.ok(bg, `${row.bg} is not declared in globals.css — the gallery row is stale`);
  const surface = hexToRgba(table.get("--jf-surface")!);
  // An 8-digit backdrop does not exist on its own: it composites over the surface
  // behind it, which is what the browser paints.
  const bgRgba = hexToRgba(bg!);
  const backdrop = bgRgba[3] === 255 ? bgRgba : over(bgRgba, surface);
  const fgRgba = hexToRgba(fg!);
  return contrast(fgRgba[3] === 255 ? fgRgba : over(fgRgba, backdrop), backdrop);
}

// ─── 1. the tree is literal-free ─────────────────────────────────────────────

test("no .tsx outside the primitive layer carries a raw colour, and the hex allowlist may only shrink (counts printed)", () => {
  const findings = scanHex(shippedTsx(), { skip: ["components/ui/"] });
  const report = checkShrinkOnly(findings, DESIGN_TOKEN_ALLOWLIST, "design-tokens: raw colour");
  // PRINTED, not buried: the gate's own output has to show the number the list is
  // holding, or the shrink is only visible to whoever reads the diff.
  console.log(report.total);
  for (const line of report.lines) console.log(`  ${line}`);
  assert.deepEqual(
    report.errors,
    [],
    `raw colour literals in shipped .tsx:\n${report.errors.join("\n")}\n` +
      "Use var(--jf-…) (declare the token in app/globals.css) or — for genuine DATA, like the brand " +
      "page's palette — a `// design-tokens-exempt: <reason>` directive.",
  );
});

test("the hex allowlist is empty and stays empty (the shrink already happened)", () => {
  // This is the END STATE of the shrink, so it is asserted directly: the tree was
  // measured at 0 in shipped code, and a single entry would mean a new deviation
  // was parked instead of fixed or justified.
  assert.deepEqual(
    Object.keys(DESIGN_TOKEN_ALLOWLIST),
    [],
    "DESIGN_TOKEN_ALLOWLIST has an entry: a new deviation is fixed or justified (design-tokens-exempt), never parked.",
  );
});

test("no .tsx carries an arbitrary [Npx] that a scale step already names (allowlist seeded, shrink-only, count printed)", () => {
  const findings = scanScalePx(shippedTsx());
  const report = checkShrinkOnly(findings, ARBITRARY_PX_ALLOWLIST, "design-tokens: arbitrary px");
  console.log(report.total);
  for (const line of report.lines) console.log(`  ${line}`);
  assert.deepEqual(
    report.errors,
    [],
    `arbitrary lengths a scale step already names:\n${report.errors.join("\n")}\n` +
      "44px is --jf-space-11, so the token form is min-h-jf-11 / w-jf-11 — the same pixels. A length NO " +
      "step names (w-[720px], top-[-260px]) is outside this rule on purpose.",
  );
  // The seed only means anything if the step set still contains what it was
  // written against: a scale that lost 44px would silently empty this rule.
  assert.ok(STEP_PX.has(44), "the space scale no longer contains 44px — the allowlist's seed was measured against it");
  assert.ok(STEP_PX.has(16), "the space scale no longer contains 16px");
});

test("the primitive layer itself is literal-free (it is excluded from the general scan, not from this rule)", () => {
  const ui = filesUnder(UI_DIR, (n) => n.endsWith(".tsx"));
  assert.ok(ui.length >= 15, `walked only ${ui.length} primitive files under components/ui — the walk is not reading the layer`);
  const findings = scanHex(ui);
  assert.deepEqual(
    findings,
    [],
    `primitives must use tokens only:\n${findings.map((f) => `  ${f.path}:${f.line}  ${f.what}`).join("\n")}`,
  );
});

test("every primitive is a forwardRef component", () => {
  const ui = filesUnder(UI_DIR, (n) => n.endsWith(".tsx"));
  const missing = ui.filter((f) => !/forwardRef\s*[<(]/.test(f.text)).map((f) => f.path);
  assert.deepEqual(missing, [], "these primitives do not use forwardRef:");
});

// ─── the scanners bite (controls) ────────────────────────────────────────────

test("control: scanHex names a planted hex, an arbitrary colour utility and a px size", () => {
  const planted: SourceFile[] = [
    {
      path: "app/planted.tsx",
      text: [
        "export function A() {",
        '  return <div className="bg-[#ff00ff] text-[13px]">x</div>;',
        "}",
        'const c = "#123456";',
      ].join("\n"),
    },
  ];
  const found = scanHex(planted).map((f) => f.what);
  assert.ok(found.some((w) => w.includes("#ff00ff") || w.includes("#123456")), `no hex finding: ${JSON.stringify(found)}`);
  assert.ok(found.some((w) => w.includes("arbitrary colour utility")), `no arbitrary-utility finding: ${JSON.stringify(found)}`);
  assert.ok(found.some((w) => w.includes("arbitrary font size")), `no px finding: ${JSON.stringify(found)}`);
});

test("control: scanHex ignores a hex in a comment, in a .test.tsx fixture, and honours a justified exemption", () => {
  const files: SourceFile[] = [
    { path: "app/comment.tsx", text: "// #abcdef is prose\nexport const a = 1;" },
    {
      path: "app/exempt.tsx",
      text: [
        '// design-tokens-exempt: the palette IS the content here',
        'const C = ["#111111"];',
        "// design-tokens-exempt-end",
      ].join("\n"),
    },
  ];
  assert.deepEqual(scanHex(files), []);
});

test("control: scanHex fails a reasonless exemption instead of silently honouring it", () => {
  const files: SourceFile[] = [{ path: "app/x.tsx", text: 'const c = "#101010"; // design-tokens-exempt:' }];
  const found = scanHex(files);
  assert.ok(found.some((f) => f.what.includes("broken exemption")), JSON.stringify(found));
  assert.ok(found.some((f) => f.what.includes("#101010")), "a reasonless exemption must not exempt the literal");
});

test("control: scanTokenDeclarations names a token declared outside globals.css", () => {
  const planted: SourceFile[] = [
    { path: "app/planted.css", text: ":root { --jf-planted: #fff; }" },
    { path: "components/Thing.tsx", text: 'const style = "--jf-planted: 1;";' },
    { path: "app/globals.css", text: ":root { --jf-allowed: #fff; }" },
  ];
  const found = scanTokenDeclarations(planted);
  assert.equal(found.length, 2, `expected the css and the tsx, got ${JSON.stringify(found)}`);
  assert.ok(found.some((f) => f.path === "components/Thing.tsx"));
});

// ─── 2. one declaration site ─────────────────────────────────────────────────

test("no --jf-* token is declared outside app/globals.css", () => {
  const files = filesUnder(SRC, (n) => !n.endsWith(".test.ts") && !n.endsWith(".test.tsx"));
  const findings = scanTokenDeclarations(files);
  assert.deepEqual(
    findings,
    [],
    `a second token declaration site is a second token layer:\n${findings.map((f) => `  ${f.path}:${f.line}  ${f.what}`).join("\n")}`,
  );
});

test("control: scanScalePx names a planted [44px] and leaves a length no scale step names alone", () => {
  const planted: SourceFile[] = [
    {
      path: "app/planted.tsx",
      text: [
        "export function A() {",
        '  return <button className="min-h-[44px] w-[720px]">x</button>;',
        "}",
        'const r = <div className="rounded-[10px] top-[-260px]" />;',
      ].join("\n"),
    },
    // The comment form is prose, not a class: it must not be a finding, or every
    // rationale comment in the repo that QUOTES a fix would fail the build.
    { path: "app/prose.tsx", text: "// the fix was min-h-[44px] -> min-h-jf-11\nexport const a = 1;" },
  ];
  const found = scanScalePx(planted).map((f) => `${f.path}:${f.what}`);
  assert.equal(found.length, 1, `expected only the 44px literal, got ${JSON.stringify(found)}`);
  assert.match(found[0], /44px/);
});

test("control: the shrink-only checker fails a grown count, an unlisted file, and a stale entry — and passes a shrink", () => {
  const findings = (paths: string[]) => paths.map((p) => ({ path: p, line: 1, what: "planted" }));

  const grown = checkShrinkOnly(findings(["a.tsx", "a.tsx"]), { "a.tsx": 1 }, "control");
  assert.ok(grown.errors.some((e) => /only SHRINK/.test(e)), `a grown count must fail: ${JSON.stringify(grown.errors)}`);

  const unlisted = checkShrinkOnly(findings(["b.tsx"]), { "a.tsx": 1 }, "control");
  assert.ok(unlisted.errors.some((e) => /not on the allowlist/.test(e)), JSON.stringify(unlisted.errors));
  assert.ok(unlisted.errors.some((e) => /delete the entry/.test(e)), "a stale entry must also fail");

  const clean = checkShrinkOnly(findings([]), { "a.tsx": 1 }, "control");
  assert.equal(clean.errors.length, 1, `an entry that outlived its drift must fail: ${JSON.stringify(clean.errors)}`);
  assert.match(clean.errors[0], /delete the entry/);

  const shrunk = checkShrinkOnly(findings(["a.tsx"]), { "a.tsx": 2 }, "control");
  assert.deepEqual(shrunk.errors, [], "a count BELOW the seed is the point of the list and must pass");
  assert.match(shrunk.total, /1 allowlisted \(seeded 2\)/, "the total names both numbers so the shrink is visible");
});

// ─── 3. every colour token is measured ───────────────────────────────────────

function checkPairs(rows: readonly ContrastPair[], theme: Map<string, string>, label: string) {
  assert.ok(rows.length >= 20, `${label}: only ${rows.length} pairs — the gallery would show a fragment of the palette`);
  for (const row of rows) {
    const value = measure(row, theme);
    assert.ok(
      value >= row.min,
      `${label}: ${row.fg} on ${row.bg} measures ${value.toFixed(2)}:1, under its ${row.min}:1 floor — ` +
        "fix the token, or re-state the floor with a reason",
    );
    assert.equal(
      value.toFixed(2),
      row.ratio.toFixed(2),
      `${label}: ${row.fg} on ${row.bg} is now ${value.toFixed(2)}:1 but ${"designTokenRatios.ts"} says ` +
        `${row.ratio.toFixed(2)}:1 — re-measure and update the data (the /design page prints it)`,
    );
  }
}

test("every light colour token has a measured pair, and every pair still measures what the gallery prints", () => {
  checkPairs(LIGHT_CONTRASTS, LIGHT, "light");
  const seen = new Set(LIGHT_CONTRASTS.flatMap((r) => [r.fg, r.bg]));
  const missing = colourTokens(LIGHT).filter((t) => !seen.has(t));
  assert.deepEqual(missing, [], `these colour tokens have no contrast pairing in designTokenRatios.ts: ${missing.join(", ")}`);
});

test("every dark colour token has a measured pair, and every pair still measures what the gallery prints", () => {
  checkPairs(DARK_CONTRASTS, DARK, "dark");
  const seen = new Set(DARK_CONTRASTS.flatMap((r) => [r.fg, r.bg]));
  const missing = colourTokens(DARK).filter((t) => !seen.has(t));
  assert.deepEqual(missing, [], `these dark colour tokens have no contrast pairing: ${missing.join(", ")}`);
});

test("the dark block overrides colour only (the scales are theme-invariant by design)", () => {
  const geometry = [...DARK.keys()].filter(
    (k) => !isColour(DARK.get(k)!) && !k.startsWith("--jf-shadow-"),
  );
  assert.deepEqual(
    geometry,
    [],
    `the dark theme declares non-colour tokens (${geometry.join(", ")}) — the space/type/radius scales have ONE ` +
      "declaration site, or the two themes drift",
  );
});

test("the five token GROUPS exist, in light and in dark, and each is a real scale (not five names)", () => {
  // 1. a type scale WITH a paired line-height. The pairing is the point: a size
  //    written at a call site never carried one, so folding it into a step would
  //    have moved every line box.
  assert.ok(TYPE_SCALE.length >= 7, `the reading type scale has ${TYPE_SCALE.length} steps`);
  for (const step of TYPE_SCALE) {
    assert.ok(step.lineHeightPx > step.px, `${step.token}: line-height ${step.lineHeightPx}px must exceed the size ${step.px}px`);
    assert.equal(TYPE_SCALE.filter((s) => s.px === step.px).length, 1, `${step.px}px is named twice — two steps for one size is not a scale`);
  }

  // 2. a 4px grid: step N is N x 4, contiguous from 4 to 48. A gap would be a step
  //    the app silently cannot use, and an off-grid step is a second spacing idea.
  assert.equal(SPACE_SCALE.length, 12, "the space scale is 12 steps (4px … 48px)");
  SPACE_SCALE.forEach((step, i) => {
    assert.equal(step.px, (i + 1) * 4, `${step.token} must be ${(i + 1) * 4}px on a 4px grid, is ${step.px}px`);
  });

  // 3. radius: an ascending ladder, with one pill step at the end.
  const radii = RADIUS_SCALE.map((s) => s.px);
  assert.deepEqual(
    radii,
    [...radii].sort((a, b) => a - b),
    "the radius ladder must ascend",
  );

  // 4. elevation: three steps, and every one of them re-tuned for the dark page —
  //    a shadow mixed for a white page is invisible on #0f1115, which is why this
  //    is the one group the dark block is allowed (and required) to override.
  assert.equal(SHADOW_SCALE.length, 3, "three elevation steps: resting, raised, dialog");
  for (const step of SHADOW_SCALE) {
    assert.ok(LIGHT.has(step.token), `${step.token} is not declared in :root`);
    assert.ok(DARK.has(step.token), `${step.token} has no dark counterpart — a light-page shadow disappears on the dark page`);
  }

  // 5. the semantic dark pairs: every colour role in `:root` has a dark value,
  //    except the ones that are deliberately theme-invariant. The exceptions are
  //    NAMED here, with the reason, so "it is not overridden" is a decision a
  //    reader can audit rather than an omission nobody noticed.
  const THEME_INVARIANT = new Set([
    // The ink painted ON a coloured fill. The fills stay dark enough for white in
    // both themes, so the ink does not move (globals.css says so in place).
    "--jf-on-color",
    // The landing hero's fixed dark slab: it is a dark surface in BOTH themes, so
    // its ink is measured against the slab, not against the page.
    "--jf-ink",
    "--jf-ink-raised",
    "--jf-ink-border",
    "--jf-ink-text",
    "--jf-ink-muted",
    "--jf-ink-accent",
    "--jf-ink-ok",
  ]);
  const missingDark = colourTokens(LIGHT).filter((t) => !DARK.has(t) && !THEME_INVARIANT.has(t));
  assert.deepEqual(
    missingDark,
    [],
    `these colour tokens have no dark pair: ${missingDark.join(", ")} — add one, or add it to THEME_INVARIANT with a reason`,
  );
  const staleException = [...THEME_INVARIANT].filter((t) => !LIGHT.has(t));
  assert.deepEqual(staleException, [], `THEME_INVARIANT names tokens that do not exist: ${staleException.join(", ")}`);
});

test("every forbidden text pair still FAILS its floor on its real surface, and its replacement still CLEARS it there", () => {
  assert.ok(FORBIDDEN_TEXT_PAIRS.length >= 6, `${FORBIDDEN_TEXT_PAIRS.length} forbidden pairs — the table would describe a corner of the palette`);
  for (const row of FORBIDDEN_TEXT_PAIRS) {
    const measured = measure({ fg: row.fg, bg: row.bg, min: row.min, ratio: row.ratio, role: row.why }, LIGHT);
    // The row exists BECAUSE it fails: a pairing that now passes has to move up
    // into LIGHT_CONTRASTS, not linger here as advice nobody needs.
    assert.ok(
      measured < row.min,
      `${row.fg} on ${row.bg} now measures ${measured.toFixed(2)}:1 (floor ${row.min}) — it PASSES, so it is no longer ` +
        "a forbidden pair: move it into LIGHT_CONTRASTS",
    );
    assert.equal(
      measured.toFixed(2),
      row.ratio.toFixed(2),
      `${row.fg} on ${row.bg} measures ${measured.toFixed(2)}:1 but designTokenRatios.ts records ${row.ratio.toFixed(2)} — re-measure`,
    );

    // The advice has to be right ON THE SAME SURFACE. A replacement measured
    // against white is exactly the mistake this table exists to stop.
    const replacement = measure({ fg: row.use, bg: row.bg, min: 4.5, ratio: row.useRatio, role: row.why }, LIGHT);
    assert.ok(
      replacement >= 4.5,
      `${row.use} on ${row.bg} measures ${replacement.toFixed(2)}:1 — the replacement this table recommends must clear ` +
        "4.5:1 on the same surface, or the advice is worse than none",
    );
    assert.equal(
      replacement.toFixed(2),
      row.useRatio.toFixed(2),
      `${row.use} on ${row.bg} measures ${replacement.toFixed(2)}:1 but designTokenRatios.ts records ${row.useRatio.toFixed(2)}`,
    );
  }
});

// ─── 4. the scales the gallery describes are the scales the CSS declares ─────

test("the type scale data matches globals.css and the @theme aliases", () => {
  for (const step of TYPE_SCALE) {
    const suffix = step.token.replace("--jf-text-", "");
    assert.equal(LIGHT.get(step.token), `${step.px}px`, `${step.token} no longer resolves to ${step.px}px`);
    assert.equal(
      LIGHT.get(step.lineHeightToken),
      `${step.lineHeightPx}px`,
      `${step.lineHeightToken} no longer resolves to ${step.lineHeightPx}px`,
    );
    assert.ok(
      THEME.includes(`--text-jf-read-${suffix}: var(${step.token});`),
      `@theme has no \`--text-jf-read-${suffix}\` alias for ${step.token}`,
    );
    assert.ok(
      THEME.includes(`--text-jf-read-${suffix}--line-height: var(${step.lineHeightToken});`),
      `the \`text-jf-read-${suffix}\` utility has no paired line-height`,
    );
  }
});

test("the space, radius and shadow data matches globals.css", () => {
  for (const step of SPACE_SCALE) {
    assert.equal(LIGHT.get(step.token), `${step.px}px`, `${step.token} no longer resolves to ${step.px}px`);
  }
  for (const step of RADIUS_SCALE) {
    assert.equal(LIGHT.get(step.token), `${step.px}px`, `${step.token} no longer resolves to ${step.px}px`);
  }
  for (const step of SHADOW_SCALE) {
    assert.ok(LIGHT.has(step.token), `${step.token} is not declared`);
    const name = step.token.replace("--jf-shadow-", "");
    assert.ok(THEME.includes(`--shadow-jf-${name}: var(${step.token});`), `@theme has no \`shadow-jf-${name}\` alias`);
  }
});

// ─── 5. the gallery is a manifest of the layer ──────────────────────────────

test("/design is not indexed and shows every primitive exported by components/ui", () => {
  const page = read(GALLERY);
  assert.match(page, /robots:\s*\{\s*index:\s*false\s*\}/, "/design must not be indexed");

  const barrel = read(join(UI_DIR, "index.ts"));
  const exported = [...barrel.matchAll(/export \{([^}]+)\}/g)]
    .flatMap((m) => m[1].split(","))
    .map((s) => s.trim())
    .filter(Boolean);
  assert.ok(exported.length >= 15, `components/ui/index.ts exports only ${exported.length} names — the walk is not reading it`);

  const missing = exported.filter((name) => !new RegExp(`\\b${name}\\b`).test(page));
  assert.deepEqual(
    missing,
    [],
    `these primitives are exported but never shown at /design: ${missing.join(", ")} — the page is the manifest ` +
      "an agent reads to know what exists",
  );
});

test("/design renders every scale table", () => {
  const page = read(GALLERY);
  for (const table of ["TYPE_SCALE", "SPACE_SCALE", "RADIUS_SCALE", "SHADOW_SCALE"]) {
    assert.ok(page.includes(table), `/design does not render ${table} — the scale would be documented nowhere`);
  }
  for (const table of ["LIGHT_CONTRASTS", "DARK_CONTRASTS", "FORBIDDEN_TEXT_PAIRS"]) {
    assert.ok(page.includes(table), `/design does not render ${table} — the palette would be documented nowhere`);
  }
  assert.ok(page.includes('data-theme="dark"'), "/design must show the dark palette in a real data-theme subtree");
});

// ─── 6. every primitive, in every state it can be in (card t_46ae7ad7) ──────

test("every primitive the barrel exports has a state-matrix entry (a new export must decide its states)", () => {
  const barrel = read(join(UI_DIR, "index.ts"));
  const exported = [...barrel.matchAll(/export \{([^}]+)\}/g)]
    .flatMap((m) => m[1].split(","))
    .map((s) => s.trim())
    .filter(Boolean);
  assert.ok(exported.length >= 20, `components/ui/index.ts exports only ${exported.length} names — the walk is not reading it`);

  const unmapped = exported.filter((name) => !(name in BARREL_FAMILY));
  assert.deepEqual(
    unmapped,
    [],
    `these barrel exports have no state-matrix entry: ${unmapped.join(", ")} — add it to BARREL_FAMILY and decide what ` +
      "every state means for it in designStates.ts",
  );
  for (const [name, primitive] of Object.entries(BARREL_FAMILY)) {
    assert.ok(
      primitive in PRIMITIVE_STATES,
      `BARREL_FAMILY maps ${name} to ${primitive}, which is not a PRIMITIVE_STATES entry`,
    );
  }
  const unmappedPrimitives = Object.keys(PRIMITIVE_STATES).filter(
    (primitive) => !Object.values(BARREL_FAMILY).includes(primitive as never),
  );
  assert.deepEqual(unmappedPrimitives, [], `these matrix entries are not reachable from any barrel export: ${unmappedPrimitives.join(", ")}`);
});

test("every (primitive, state) cell is answered: a null renders, a reason of 12+ chars explains why not", () => {
  for (const [primitive, cells] of Object.entries(PRIMITIVE_STATES)) {
    assert.deepEqual(
      Object.keys(cells).sort(),
      [...STATES].sort(),
      `${primitive} must answer all six states — a missing key is a state nobody decided`,
    );
    assert.equal(cells.default, null, `${primitive} must render its DEFAULT state; a primitive with no default is not a primitive`);
    for (const state of STATES) {
      const cell = cells[state];
      if (cell === null) continue;
      assert.ok(
        typeof cell === "string" && cell.length >= 12,
        `${primitive}/${state}: "not applicable" needs a reason of 12+ chars (got ${JSON.stringify(cell)}) — ` +
          "a blank cell is a state someone could not be bothered to decide",
      );
      assert.ok(
        !/^(n\/?a|none|not applicable|no)\.?$/i.test(cell.trim()),
        `${primitive}/${state}: "${cell}" is a shrug, not a reason`,
      );
    }
  }
});

test("/design renders the matrix itself — a marker per primitive and a marker per state cell", () => {
  const page = read(GALLERY);
  assert.ok(page.includes("PRIMITIVE_STATES"), "/design must read the matrix from lib/designStates.ts, not keep a second copy");
  assert.ok(page.includes("STATES"), "/design must iterate the six states, not a hand-typed list");
  assert.ok(
    page.includes("data-primitive={primitive}") && page.includes("data-state={state}"),
    "/design must mark every rendered cell (data-primitive / data-state) so the coverage is inspectable in the DOM",
  );
  // The page's specimens are TYPE-CHECKED against the matrix (a cell the matrix
  // calls applicable with no specimen is a tsc error), so this test only has to
  // prove the page is driven by the data rather than by a parallel list.
  assert.ok(page.includes("SPECIMENS"), "/design must hold the specimens the matrix type-checks");
});

test("a primitive that applies a state has a specimen for it (the compile-time half is still the compile-time half)", () => {
  const page = read(GALLERY);
  // The strict mapped type in the page is what makes a missing specimen a build
  // error: it keeps ONLY the states `PRIMITIVE_STATES` calls applicable (the
  // key remapping) and requires a ReactNode for each. This asserts the type is
  // still there, because a `Partial<…>` over all six states would silently
  // re-open the hole the card exists to close.
  assert.match(
    page,
    /\[S in State as \(typeof PRIMITIVE_STATES\)\[P\]\[S\] extends null \? S : never\]/,
    "/design must derive the applicable states from PRIMITIVE_STATES by key remapping, not list them by hand",
  );
  assert.match(
    page,
    /const SPECIMENS: \{ \[P in PrimitiveName\]: Applies<P> \}/,
    "the specimens must be a COMPLETE map over PrimitiveName whose cells are the derived Applies<P> records",
  );
  assert.ok(!/as unknown as/.test(page), "/design must not cast its way past the specimen type");
});
