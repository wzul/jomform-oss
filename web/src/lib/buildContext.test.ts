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
// WHAT IT DOES NOT COVER (stated honestly)
//   Only literals are checkable statically: `new URL("<lit>", import.meta.url | HERE | APP)`
//   and `read("<lit>")` (resolved through the file's own `read` helper base).
//   Paths built from variables — a directory walk, an imported constant — are
//   invisible to this guard; they are the host-only risk that remains.
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
 * The `COPY` instructions of the stage that runs `npm run build` — the stage
 * whose filesystem the unit suite sees. Context copies only: a `--from=<stage>`
 * copy comes from another stage's build output, not from the repo, so it can
 * never satisfy a test that reads a source file.
 */
function buildStageCopies(src: string): CopyInstr[] {
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

  const copies: CopyInstr[] = [];
  let workdir = "/";
  for (const instr of instrs.slice(stageStart, runAt + 1)) {
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
 * Drop comment LINES — prose that quotes a path is documentation, not a read.
 * Deliberately line-based: a `/* … *​/` stripper would corrupt any file that
 * carries a comment-matching regex of its own (operatorNotice.test.ts does, and
 * it ate the definitions this guard resolves reads against).
 */
function stripCommentLines(src: string): string {
  return src
    .split(/\r?\n/)
    .filter((l) => {
      const t = l.trim();
      return !(t.startsWith("//") || t.startsWith("/*") || t.startsWith("*"));
    })
    .join("\n");
}

const URL_LITERAL = /new URL\(\s*"([^"]*)"\s*,\s*(import\.meta\.url|HERE|APP)\s*\)/g;
const READ_LITERAL = /\bread\(\s*"([^"]*)"\s*\)/g;
const READ_HELPER = /const read = \([^)]*\)\s*=>[\s\S]{0,200}?new URL\(\s*rel\s*,\s*(import\.meta\.url|HERE|APP)\s*\)/;

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

type Target = { file: string; literal: string; abs: string; rel: string };
type Scan = { targets: Target[]; unresolved: string[] };

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
  const targets: Target[] = [];
  const unresolved: string[] = [];
  const push = (literal: string, abs: string) => {
    targets.push({ file, literal, abs, rel: relative(REPO_ROOT, abs) });
  };
  for (const m of text.matchAll(URL_LITERAL)) {
    const base = baseDir(text, m[2], file);
    if (base) push(m[1], resolve(base, m[1]));
    else unresolved.push(`new URL("${m[1]}", ${m[2]}) — the guard cannot resolve the base ${m[2]}`);
  }
  for (const m of text.matchAll(READ_LITERAL)) {
    if (readBase) push(m[1], resolve(readBase, m[1]));
    else
      unresolved.push(
        `read("${m[1]}") — the file's \`read\` helper is not ` +
          "`(rel) => readFileSync(fileURLToPath(new URL(rel, <base>)), …)`, so this guard cannot " +
          "tell which directory the path is relative to",
      );
  }
  return { targets, unresolved };
}

const files = testFiles();
const scans = files.map((f) => ({ file: f, ...scan(f, readFileSync(f, "utf8")) }));
const allTargets = scans.flatMap((s) => s.targets);
const unresolved = scans.flatMap((s) => s.unresolved);
const escaping = allTargets.filter((t) => !t.abs.startsWith(`${WEB_DIR}/`) && t.abs !== WEB_DIR);

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
  const covered = buildCopies.filter((c) => c.sources.some((s) => s.path === "api" || s.path === "web"));
  assert.ok(
    covered.length >= 2,
    "web/Dockerfile no longer copies web/ and api/ in the build stage — the whole build context " +
      "rationale (and this guard) is stale",
  );
});

test("every literal read in the web suite is copied into the web image by web/Dockerfile", () => {
  const offenders: string[] = [...unresolved];
  for (const t of allTargets) {
    const where = `${relative(WEB_DIR, t.file)} reads "${t.literal}"`;
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
    // The test resolves this path relative to its own file, so inside the image
    // it looks at /repo/<rel>. That only works if the build stage copies the
    // file there.
    const want = posix.join(IMAGE_ROOT, t.rel);
    const got = imagePathFor(t.rel, buildCopies);
    if (got === undefined) {
      offenders.push(
        `${where} → the build stage of web/Dockerfile never copies ${t.rel}, but the test will ` +
          `look for ${want} inside the image: add \`COPY ${t.rel} ${want}\` (or move the file ` +
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
