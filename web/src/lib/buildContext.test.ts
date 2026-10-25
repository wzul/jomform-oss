// Build context — every file a WEB TEST reads must be copied into the web IMAGE.
//
// WHY THIS EXISTS (the defect it was written for)
//   `web/package.json` runs the unit suite first (`build`: test:unit && next
//   build) and web/Dockerfile builds from the REPO ROOT, so the container is
//   laid out as /repo/web + /repo/api and a web-relative read resolves exactly
//   as it does in a checkout — but ONLY for the directories that Dockerfile
//   copies. Nothing else is in the image.
//
//   `MCP-PARITY.md` lives at the repo root. When langPlacement.test.ts started
//   reading it (the MCP parity checklist is a valid cross-language guard), the
//   host run stayed green — `../../../MCP-PARITY.md` resolves to the real repo
//   root — while the IMAGE build died at import with
//   `ENOENT: no such file or directory, open '/repo/MCP-PARITY.md'`: 225 tests
//   collected, 1 failing, 8 of that file's tests never registered, `npm run
//   build` exit 1. The release tag could not be built and nothing in the repo
//   said why.
//
//   The local preflight (scripts/preflight.sh) cannot see this class at all: it
//   runs the suite on the HOST, where the root file is present. So the guard has
//   to live in the suite itself. `COPY web/ ./` copies this file into the image,
//   which is what makes it verify itself in BOTH environments: a new
//   `read("../../../X")` outside web/ and api/ turns RED in the same run that
//   would otherwise be green, instead of breaking the next release.
//
// THE RULE
//   For every read path written as a LITERAL in web/src/**/*.test.ts:
//     1. its target must exist, and
//     2. the `npm run build` stage of web/Dockerfile must copy it into the
//        image at exactly the path that test will resolve it to.
//   The mirror layout is the contract: a file at <repo>/<rel> must be copied to
//   /repo/<rel>. A file that is not copied at all — or copied somewhere else —
//   fails here, naming the file and the read that wants it.
//
// WHAT IT COVERS — and the shape that used to be invisible
//   A literal is checkable when the path reaches the filesystem from a string
//   written in the test file:
//     - `new URL("<lit>", import.meta.url | HERE | APP)`
//     - `read("<lit>")`, resolved through the file's own `read` helper base
//     - `X("<lit>")` where `const X = (rel: string) => fileURLToPath(new URL(rel, <base>))`
//       — the `pathOf` shape. This is how a test reaches a DIRECTORY
//       (`const E2E_DIR = pathOf("../../e2e")`, then `readdirSync(E2E_DIR)`),
//       which neither of the first two can see: the a11y coverage guard walks
//       web/e2e that way, so a build context that drops web/e2e stayed GREEN
//       here and killed `npm run build` in the image with ENOENT.
//   A directory target is checked AS a directory: the build stage must copy the
//   directory itself (the COPY operand is verified to be a directory on the
//   host, so its CONTENTS land at /repo/<rel>; a sibling file proves nothing).
//
//   The build CONTEXT is checked too, not just the Dockerfile: a `COPY web/ ./`
//   is worthless if the root `.dockerignore` dropped web/e2e from the context
//   the daemon receives. Both of those are how a path can be "copied" in the
//   Dockerfile and still be missing from the image. This guard reads that ignore
//   file itself, so the rule applies to it as well — `web/Dockerfile` copies it
//   to /repo/.dockerignore, and the guard asserts that COPY is still there.
//
// WHAT IT STILL DOES NOT COVER (stated honestly)
//   Paths built from variables that do not come from a literal — `X(rel)` for a
//   value read out of another file, `join(dir, entry.name)` inside a walk — have
//   no string here to resolve, so they remain the host-only risk.
//
// Run with:
//
//   npm run test:unit

import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, join, posix, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const LIB_DIR = dirname(fileURLToPath(import.meta.url));
const WEB_DIR = resolve(LIB_DIR, "..", ".."); // <repo>/web
const REPO_ROOT = resolve(WEB_DIR, ".."); //     <repo>
const WEB_DOCKERFILE = join(WEB_DIR, "Dockerfile");

// The image mirrors the checkout under /repo (web/Dockerfile: COPY web/ ./ with
// WORKDIR /repo/web). Every path below is expressed relative to the repo root so
// host and container agree.
const IMAGE_ROOT = "/repo";

const dockerfile = readFileSync(WEB_DOCKERFILE, "utf8");

/** One `COPY` of the build stage, with its sources and its absolute image dest. */
type CopyInstr = {
  raw: string;
  sources: Array<{ path: string; isDir: boolean }>;
  dest: string;
  destIsDir: boolean;
};

/** Dockerfile lines with comments dropped and `\` continuations joined. */
function logicalLines(src: string): string[] {
  const out: string[] = [];
  let buffer = "";
  for (const raw of src.split(/\r?\n/)) {
    const line = raw.trim();
    if (line === "" || line.startsWith("#")) continue;
    if (line.endsWith("\\")) {
      buffer += `${line.slice(0, -1).trim()} `;
      continue;
    }
    out.push(buffer + line);
    buffer = "";
  }
  if (buffer !== "") out.push(buffer.trim());
  return out;
}

/** An absolute image path: absolute args stand alone, relative ones use WORKDIR. */
function imagePath(arg: string, workdir: string): string {
  return arg.startsWith("/") ? posix.normalize(arg) : posix.normalize(posix.join(workdir, arg));
}

/**
 * The instructions of the stage that runs `npm run build` — the stage whose
 * filesystem the unit suite sees.
 */
function buildStage(src: string): string[] {
  const instrs = logicalLines(src);
  const runAt = instrs.findIndex((i) => /^RUN\b/.test(i) && i.includes("npm run build"));
  assert.notEqual(
    runAt,
    -1,
    "web/Dockerfile no longer runs `npm run build` — this guard cannot tell which stage " +
      "the unit suite executes in (update the guard if the build stage was legitimately reworked)",
  );
  let stageStart = -1;
  for (let i = runAt; i >= 0; i--) {
    if (/^FROM\b/.test(instrs[i])) {
      stageStart = i;
      break;
    }
  }
  assert.notEqual(stageStart, -1, "could not find the FROM line of the build stage in web/Dockerfile");
  return instrs.slice(stageStart, runAt + 1);
}

/**
 * The `COPY` instructions of that stage. Context copies only: a `--from=<stage>`
 * copy comes from another stage's build output, not from the repo, so it can
 * never satisfy a test that reads a source file.
 */
function buildStageCopies(src: string): CopyInstr[] {
  const stage = buildStage(src);
  const copies: CopyInstr[] = [];
  let workdir = "/";
  for (const instr of stage) {
    const tokens = instr.split(/\s+/);
    const word = tokens[0].toUpperCase();
    if (word === "WORKDIR") {
      workdir = imagePath(tokens[1], workdir);
      continue;
    }
    if (word !== "COPY") continue;
    const args = tokens.slice(1);
    if (args.some((a) => a.startsWith("--from"))) continue;
    let operands = args.filter((a) => !a.startsWith("--"));
    if (operands.length === 1 && operands[0].startsWith("[")) {
      // JSON array form: COPY ["src", "dest"]
      operands = JSON.parse(operands[0].replace(/,\s*\]$/, "]")) as string[];
    }
    assert.ok(
      operands.length >= 2,
      `web/Dockerfile has a COPY the guard cannot read: ${instr} (write it in plain form)`,
    );
    const destRaw = operands[operands.length - 1];
    copies.push({
      raw: instr,
      sources: operands.slice(0, -1).map((s) => ({
        path: posix.normalize(s.replace(/\/+$/, "")),
        isDir: s.endsWith("/"),
      })),
      dest: imagePath(destRaw, workdir),
      destIsDir: destRaw.endsWith("/") || operands.length > 2,
    });
  }
  assert.ok(copies.length > 0, "web/Dockerfile copies nothing from the build context any more");
  return copies;
}

/** Where a repo-relative path lands inside the image, or undefined if uncopied. */
function imagePathFor(rel: string, copies: CopyInstr[]): string | undefined {
  for (const c of copies) {
    for (const s of c.sources) {
      if (rel === s.path) {
        if (s.isDir) return c.dest;
        return c.destIsDir ? posix.join(c.dest, posix.basename(s.path)) : c.dest;
      }
      if (rel.startsWith(`${s.path}/`)) return posix.join(c.dest, rel.slice(s.path.length + 1));
    }
  }
  return undefined;
}

const buildCopies = buildStageCopies(dockerfile);

/**
 * `RUN npm ci` in the build stage: web/node_modules is PRODUCED there, not
 * copied from the context — which is exactly why the two-star node_modules rule
 * is in .dockerignore. Two suite files read a file out of that tree
 * (tailwindcss/theme.css), and they are satisfied by the install, not by a COPY,
 * so the context rule below must not fire on them.
 */
const INSTALLS_DEPS = buildStage(dockerfile).some(
  (i) => /^RUN\b/.test(i) && /\bnpm (ci|install)\b/.test(i),
);

/**
 * Declared HOST-ONLY targets: a file the suite reads in a checkout and
 * deliberately does not read in the image because the test skips itself there.
 * Each entry carries the mechanism that makes the absence safe and the guard
 * VERIFIES it is still present in that file — delete the skip and this exception
 * stops applying, so the read fails here instead of silently going unchecked.
 * Not a place to park offenders: nothing may be added without a skip that the
 * test itself documents.
 */
const HOST_ONLY: Array<{ file: string; literal: string; mechanism: RegExp; why: string }> = [
  {
    file: join(WEB_DIR, "src/lib/a11yScanProse.test.ts"),
    literal: "../../../docs/accessibility.md",
    mechanism: /function imageBuildSkipReasonAt\(/,
    why:
      "the repo-root docs/ tree is not copied into the image by design, and the test skips " +
      "itself there through imageBuildSkipReasonAt() (measured: the webimage gate went red with " +
      "ENOENT /repo/docs/accessibility.md before that skip existed)",
  },
];

/**
 * The root build CONTEXT's ignore rules (.dockerignore) — the Dockerfile is only
 * half the answer: `COPY web/ ./` brings in nothing that the ignore file removed
 * from the context before the daemon ever saw it.
 *
 * The rules follow docker's own patternmatcher: patterns are repo-relative, `*`
 * and `?` do not cross `/`, a two-star prefix matches any number of leading
 * directories (including none), `!` re-includes, and the LAST matching rule wins.
 * A pattern that matches a PARENT directory excludes everything under it
 * (BuildKit asks whether the path or any of its parents match).
 *
 * Measured, not assumed: on tree 7d95965d, with .dockerignore:28 carrying the
 * bare `e2e` word, `docker build -f <probe> .` from the repo root uploaded
 * web/e2e — `COPY web/e2e/` counted 56 files — and dropped only the ROOT e2e/.
 * So a pattern with no `/` in it matches the top-level entry only, while the
 * two-star form matches at any depth.
 */
type IgnoreRule = { pattern: string; negated: boolean; re: RegExp };

const DOCKERIGNORE = join(REPO_ROOT, ".dockerignore");

function ignoreRules(): IgnoreRule[] {
  const out: IgnoreRule[] = [];
  for (const raw of readFileSync(DOCKERIGNORE, "utf8").split(/\r?\n/)) {
    const line = raw.trim();
    if (line === "" || line.startsWith("#")) continue;
    const negated = line.startsWith("!");
    const pattern = (negated ? line.slice(1) : line).replace(/^\.?\//, "").replace(/\/+$/, "");
    if (pattern === "") continue;
    let body = "";
    for (let i = 0; i < pattern.length; i++) {
      const c = pattern[i];
      if (c === "*") {
        if (pattern[i + 1] === "*") {
          i++;
          if (pattern[i + 1] === "/") {
            i++;
            body += "(?:.*/)?";
          } else body += ".*";
        } else body += "[^/]*";
      } else if (c === "?") body += "[^/]";
      else body += c.replace(/[.+^${}()|[\]\\]/g, "\\$&");
    }
    out.push({ pattern, negated, re: new RegExp(`^${body}$`) });
  }
  return out;
}

/** The ignore pattern that keeps a repo-relative path out of the build context, if any. */
function excludedFromContext(rel: string, rules: IgnoreRule[]): string | undefined {
  const parts = rel.split("/");
  let last: IgnoreRule | undefined;
  for (const rule of rules) {
    for (let i = 1; i <= parts.length; i++) {
      if (rule.re.test(parts.slice(0, i).join("/"))) {
        last = rule;
        break;
      }
    }
  }
  return last && !last.negated ? last.pattern : undefined;
}

const contextRules = ignoreRules();

/**
 * The COPY that brings a repo DIRECTORY — and therefore everything under it —
 * into the image. A `readdirSync` target is satisfied by a directory operand,
 * never by a COPY of a sibling file, so the operand is also checked to be a
 * directory on the host.
 */
function dirCopyFor(rel: string, copies: CopyInstr[]): string | undefined {
  for (const c of copies) {
    for (const s of c.sources) {
      if (rel !== s.path && !rel.startsWith(`${s.path}/`)) continue;
      const onDisk = join(REPO_ROOT, s.path);
      if (!existsSync(onDisk) || !statSync(onDisk).isDirectory()) continue;
      return rel === s.path ? c.dest : posix.join(c.dest, rel.slice(s.path.length + 1));
    }
  }
  return undefined;
}

/**
 * Every *.test.ts under web/src, EXCEPT this guard itself: its own failure
 * messages quote the shape it looks for (`read("…")`), so scanning itself would
 * make those templates read as literal reads. This file performs no reads.
 */
const SELF = fileURLToPath(import.meta.url);

function testFiles(): string[] {
  const out: string[] = [];
  const walk = (dir: string) => {
    for (const entry of readdirSync(dir)) {
      const p = join(dir, entry);
      if (statSync(p).isDirectory()) walk(p);
      else if (entry.endsWith(".test.ts") && p !== SELF) out.push(p);
    }
  };
  walk(join(WEB_DIR, "src"));
  return out;
}

/**
 * Blank out comment LINES — prose that quotes a path is documentation, not a
 * read. The line itself is kept (as an empty line) so every reported location
 * stays the line number a reader sees in their editor.
 * Deliberately line-based: a `/* … *​/` stripper would corrupt any file that
 * carries a comment-matching regex of its own (operatorNotice.test.ts does, and
 * it ate the definitions this guard resolves reads against).
 */
function stripCommentLines(src: string): string {
  return src
    .split(/\r?\n/)
    .map((l) => {
      const t = l.trim();
      return t.startsWith("//") || t.startsWith("/*") || t.startsWith("*") ? "" : l;
    })
    .join("\n");
}

const URL_LITERAL = /new URL\(\s*"([^"]*)"\s*,\s*(import\.meta\.url|HERE|APP)\s*\)/g;
const READ_LITERAL = /\bread\(\s*"([^"]*)"\s*\)/g;
const READ_HELPER =
  /const read = \([^)]*\)\s*=>[\s\S]{0,200}?new URL\(\s*rel\s*,\s*(import\.meta\.url|HERE|APP)\s*\)/;
/**
 * `const X = (rel: string) => fileURLToPath(new URL(rel, <base>))` — the
 * `pathOf` shape. The literal is one call away from the base, so `X("<lit>")`
 * is a read target exactly like `new URL("<lit>", …)`, and it is the only shape
 * that reaches a DIRECTORY (`readdirSync(X("…"))`). The `read` helper is not
 * matched here: its arrow body starts with `readFileSync(`, and `read("<lit>")`
 * is already handled above (matching it twice would double every target).
 */
const PATH_HELPER =
  /const\s+([A-Za-z_$][\w$]*)\s*=\s*\(\s*([A-Za-z_$][\w$]*)\s*(?::\s*string)?\s*\)\s*=>\s*fileURLToPath\(\s*new URL\(\s*\2\s*,\s*(import\.meta\.url|HERE|APP)\s*\)\s*\)/g;

/** The directory a `new URL("<lit>", <base>)` in this file resolves against. */
function baseDir(src: string, base: string, file: string): string | undefined {
  if (base === "import.meta.url") return dirname(file);
  const def = src.match(
    new RegExp(
      `const ${base}\\s*=\\s*new URL\\(\\s*"([^"]*)"\\s*,\\s*import\\.meta\\.url\\s*,?\\s*\\)|` +
        `const ${base}\\s*=\\s*new URL\\(\\s*"([^"]*)"\\s*,\\s*import\\.meta\\.url\\s*\\)`,
    ),
  );
  if (!def) return undefined; // not a URL constant this guard knows — cannot resolve
  const lit = def[1] ?? def[2];
  return resolve(dirname(file), lit);
}

type Target = {
  file: string;
  line: number;
  literal: string;
  abs: string;
  rel: string;
  /** The `pathOf`-style helper the literal was passed to, when it was one. */
  via?: string;
  /** `!existsSync(X("<lit>"))` — a pin that the path is ABSENT; not a read. */
  absencePin?: boolean;
};
type Scan = { targets: Target[]; unresolved: string[]; helpers: number };

/** 1-based line of an offset in the (line-preserving) source text. */
function lineAt(text: string, index: number): number {
  let line = 1;
  for (let i = 0; i < index; i++) if (text.charCodeAt(i) === 10) line++;
  return line;
}

/** The `pathOf`-style helpers in one file: helper name → the directory it resolves against. */
function pathHelpers(text: string, file: string): Map<string, string> {
  const out = new Map<string, string>();
  for (const m of text.matchAll(PATH_HELPER)) {
    const base = baseDir(text, m[3], file);
    if (base) out.set(m[1], base);
  }
  return out;
}

/**
 * Every literal read target in one test file, resolved to a repo-relative path,
 * plus any literal this guard CANNOT resolve. An unresolvable literal is
 * reported instead of skipped: silently dropping it would make a new
 * `read("../" + x)` shape look like a pass.
 */
function scan(file: string, src: string): Scan {
  const text = stripCommentLines(src);
  const helper = text.match(READ_HELPER);
  const readBase = helper ? baseDir(text, helper[1], file) : undefined;
  const pathOf = pathHelpers(text, file);
  const targets: Target[] = [];
  const unresolved: string[] = [];
  const push = (literal: string, abs: string, index: number, via?: string) => {
    targets.push({
      file,
      line: lineAt(text, index),
      literal,
      abs,
      rel: relative(REPO_ROOT, abs),
      via,
      // `assert.ok(!existsSync(path("…")))` pins that a path does NOT exist (the
      // /feedback/[id] route must stay absent). There is nothing to copy for it —
      // it is absent in the image too — so it is not a read target.
      absencePin: /!\s*existsSync\(\s*$/.test(text.slice(Math.max(0, index - 40), index)),
    });
  };
  for (const m of text.matchAll(URL_LITERAL)) {
    const base = baseDir(text, m[2], file);
    if (base) push(m[1], resolve(base, m[1]), m.index ?? 0);
    else unresolved.push(`new URL("${m[1]}", ${m[2]}) — the guard cannot resolve the base ${m[2]}`);
  }
  for (const m of text.matchAll(READ_LITERAL)) {
    if (readBase) push(m[1], resolve(readBase, m[1]), m.index ?? 0);
    else
      unresolved.push(
        `read("${m[1]}") — the file's \`read\` helper is not ` +
          "`(rel) => readFileSync(fileURLToPath(new URL(rel, <base>)), …)`, so this guard cannot " +
          "tell which directory the path is relative to",
      );
  }
  for (const [name, base] of pathOf) {
    const call = new RegExp(`\\b${name.replace(/[^\w]/g, "\\$&")}\\(\\s*"([^"]*)"\\s*\\)`, "g");
    for (const m of text.matchAll(call)) push(m[1], resolve(base, m[1]), m.index ?? 0, name);
  }
  return { targets, unresolved, helpers: pathOf.size };
}

const files = testFiles();
const scans = files.map((f) => ({ file: f, ...scan(f, readFileSync(f, "utf8")) }));
const allTargets = scans.flatMap((s) => s.targets);
const unresolved = scans.flatMap((s) => s.unresolved);
const escaping = allTargets.filter((t) => !t.abs.startsWith(`${WEB_DIR}/`) && t.abs !== WEB_DIR);
const helperFiles = scans.filter((s) => s.helpers > 0).length;
const helperTargets = allTargets.filter((t) => t.via !== undefined);
const directoryTargets = allTargets.filter((t) => existsSync(t.abs) && statSync(t.abs).isDirectory());

test("the guard is looking at real files (it cannot pass vacuously)", () => {
  assert.ok(
    files.length >= 15,
    `found only ${files.length} test files under web/src — the walk is broken, so the rule below ` +
      "would be checking nothing",
  );
  assert.ok(
    allTargets.length >= 30,
    `extracted only ${allTargets.length} literal read targets from the suite — the literal ` +
      "patterns have drifted, so the rule below would be checking nothing",
  );
  // The class is real, not hypothetical: the suite reads api/ sources and the
  // repo-root parity checklist. If escaping reads ever disappear this guard's
  // premise changed — re-read its header before relaxing the count.
  assert.ok(
    escaping.length >= 10,
    `only ${escaping.length} reads escape web/ — the cross-language reads this guard exists for ` +
      "seem to have moved; re-check the guard instead of trusting a green run",
  );
  // Same argument for the `pathOf` shape: if no helper and no helper call site is
  // found, the shape has drifted and every DIRECTORY read it carries is
  // invisible again — which is the gap this file was extended to close.
  assert.ok(
    helperFiles >= 5,
    `only ${helperFiles} test files define a \`pathOf\`-style helper — the shape the directory ` +
      "reads go through has drifted, so those reads are invisible to this guard again",
  );
  assert.ok(
    helperTargets.length >= 10,
    `only ${helperTargets.length} literals reached the guard through a \`pathOf\`-style helper — ` +
      "the call-site pattern has drifted, so helper-mediated reads are unchecked",
  );
  assert.ok(
    directoryTargets.length >= 1,
    "no read target resolves to a DIRECTORY — the suite does walk e2e/ (the a11y coverage guard " +
      "selects the playwright specs there); if that read moved, re-check this guard",
  );
  // The specific dependency this guard exists for at the moment of writing, and
  // the one a build-context change would silently kill: the a11y coverage suite
  // selects the playwright specs by walking web/e2e.
  assert.ok(
    allTargets.some(
      (t) => t.rel === "web/e2e" && relative(WEB_DIR, t.file) === "src/lib/a11yAuthedCoverage.test.ts",
    ),
    `nothing in src/lib/a11yAuthedCoverage.test.ts reaches web/e2e through a literal any more — ` +
      "that file selects the a11y specs by walking the directory, so a build context that drops " +
      "it must fail HERE. If the read was removed on purpose, delete this pin; if it moved, make " +
      "the new read visible to this guard",
  );
  const covered = buildCopies.filter((c) => c.sources.some((s) => s.path === "api" || s.path === "web"));
  assert.ok(
    covered.length >= 2,
    "web/Dockerfile no longer copies web/ and api/ in the build stage — the whole build context " +
      "rationale (and this guard) is stale",
  );
  // This guard's OWN read, held to its own rule: it reads the root .dockerignore,
  // and `npm run build` runs this file inside the image, so that file has to be
  // copied there or the image build dies on ENOENT exactly like the reads below.
  assert.equal(
    imagePathFor(".dockerignore", buildCopies),
    "/repo/.dockerignore",
    "web/Dockerfile must copy the root .dockerignore to /repo/.dockerignore: this guard reads " +
      "it and this guard runs inside the image, so without that COPY the image build fails while " +
      "the host stays green",
  );
});

test("every literal path in the web suite — file OR directory — reaches the web image", () => {
  const offenders: string[] = [...unresolved];
  for (const t of allTargets) {
    const where =
      `${relative(WEB_DIR, t.file)}:${t.line} reads "${t.literal}"` +
      (t.via ? ` (through ${t.via}())` : "");
    // Declared host-only reads: allowed ONLY while the skip they rely on is
    // still in the file (checked here, so deleting the skip turns this red).
    const hostOnly = HOST_ONLY.find((h) => h.file === t.file && h.literal === t.literal);
    if (hostOnly) {
      const mechanismStillThere = hostOnly.mechanism.test(readFileSync(t.file, "utf8"));
      assert.ok(
        mechanismStillThere,
        `${where} is listed in HOST_ONLY because ${hostOnly.why} — but that skip is gone from ` +
          "the file, so this read is no longer safe in the image and must fail here",
      );
      continue;
    }
    if (t.abs === REPO_ROOT) continue; // the repo root is a BASE for derived paths, not a copy target
    if (t.abs.startsWith(`${WEB_DIR}/node_modules/`) && INSTALLS_DEPS) continue; // `RUN npm ci` creates it
    if (t.abs.startsWith(`${WEB_DIR}/node_modules/`) && !INSTALLS_DEPS) {
      offenders.push(
        `${where} → web/node_modules is read by a test but web/Dockerfile no longer runs ` +
          "`npm ci`/`npm install` in the build stage, so nothing installs it in the image",
      );
      continue;
    }
    if (t.absencePin) continue; // `!existsSync(...)` pins an ABSENCE; absent in the image too
    if (t.rel.startsWith("..")) {
      offenders.push(
        `${where} → ${t.abs} is OUTSIDE the repo root (${REPO_ROOT}). Nothing outside the repo root ` +
          "is copied into the image; `npm run build` would die on the unhandled ENOENT while this " +
          "suite stays green on the host",
      );
      continue;
    }
    if (!existsSync(t.abs)) {
      offenders.push(`${where} → ${t.abs} does not exist (the read throws on any filesystem)`);
      continue;
    }
    const isDir = statSync(t.abs).isDirectory();
    const kind = isDir ? "directory" : "file";
    // The build CONTEXT first: a path the .dockerignore removed never reaches the
    // daemon, so no COPY in the Dockerfile can put it in the image.
    const ignored = excludedFromContext(t.rel, contextRules);
    if (ignored !== undefined) {
      offenders.push(
        `${where} → the root .dockerignore excludes ${t.rel} from the build context (pattern ` +
          `"${ignored}"): web/Dockerfile cannot copy what the daemon was never given, and the ` +
          `image build dies on the unhandled ENOENT while this suite stays green on the host`,
      );
      continue;
    }
    // The test resolves this path relative to its own file, so inside the image
    // it looks at /repo/<rel>. That only works if the build stage copies the
    // path there — for a DIRECTORY, if it copies the directory itself.
    const want = posix.join(IMAGE_ROOT, t.rel);
    const got = isDir ? dirCopyFor(t.rel, buildCopies) : imagePathFor(t.rel, buildCopies);
    if (got === undefined) {
      offenders.push(
        `${where} → the build stage of web/Dockerfile never copies ${t.rel}, but the test will ` +
          `look for ${want} inside the image: add \`COPY ${t.rel} ${want}\` (or move the ${kind} ` +
          "under web/ or api/) — this is the MCP-PARITY.md defect exactly",
      );
      continue;
    }
    if (got !== want) {
      offenders.push(
        `${where} → web/Dockerfile copies ${t.rel} to ${got}, but the test will look for ${want}: ` +
          "the image must mirror the checkout under /repo for a web-relative read to resolve",
      );
    }
  }
  assert.deepEqual(
    offenders,
    [],
    "the web image cannot run this suite:\n  " + offenders.join("\n  "),
  );
});
