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

import { DESIGN_TOKEN_ALLOWLIST } from "./designTokenAllowlist.ts";
import {
  DARK_CONTRASTS,
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
 * `skip` is a path prefix (the primitive layer) and `allow` is the file allowlist;
 * both are arguments so the control test can prove the scanner still bites with
 * the real exclusions in place.
 */
export function scanHex(
  files: readonly SourceFile[],
  { skip = [], allow = [] }: { skip?: readonly string[]; allow?: readonly string[] } = {},
): Finding[] {
  const findings: Finding[] = [];
  const allowed = new Set(allow);
  for (const file of files) {
    if (skip.some((p) => file.path.startsWith(p)) || allowed.has(file.path)) continue;
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

test("no .tsx outside the primitive layer and the (empty) allowlist carries a raw colour", () => {
  const findings = scanHex(shippedTsx(), { skip: ["components/ui/"], allow: DESIGN_TOKEN_ALLOWLIST });
  assert.deepEqual(
    findings,
    [],
    `raw colour literals in shipped .tsx:\n${findings.map((f) => `  ${f.path}:${f.line}  ${f.what}`).join("\n")}\n` +
      "Use var(--jf-…) (declare the token in app/globals.css) or — for genuine DATA, like the brand " +
      "page's palette — a `// design-tokens-exempt: <reason>` directive.",
  );
});

test("the allowlist is empty and every entry it might ever hold still carries a hex (it may only shrink)", () => {
  assert.deepEqual(
    [...DESIGN_TOKEN_ALLOWLIST],
    [],
    "DESIGN_TOKEN_ALLOWLIST has an entry: this card ends at zero, and a new deviation is fixed or justified " +
      "(design-tokens-exempt), never parked in the list.",
  );
  for (const rel of DESIGN_TOKEN_ALLOWLIST) {
    const file = [{ path: rel, text: read(join(SRC, rel)) }];
    assert.notDeepEqual(scanHex(file), [], `${rel} is on the allowlist but no longer contains a hex — remove it from the list`);
  }
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
  for (const table of ["LIGHT_CONTRASTS", "DARK_CONTRASTS"]) {
    assert.ok(page.includes(table), `/design does not render ${table} — the palette would be documented nowhere`);
  }
  assert.ok(page.includes('data-theme="dark"'), "/design must show the dark palette in a real data-theme subtree");
});
