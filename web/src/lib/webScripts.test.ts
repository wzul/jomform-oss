// Guard: a web npm script may not delegate to a `next` subcommand THIS tree does
// not have, and the rest of the repo may not tell a reader to run an npm script
// `web/package.json` does not define.
//
// THE DEFECT (reproduced on this tree at origin/main 2ff5cff6, next 16.3.8)
//   `web/package.json` declared `"lint": "next lint"`. Next.js 16 removed the
//   `next lint` command (https://nextjs.org/docs/app/api-reference/config/eslint:
//   "Starting with Next.js 16, `next lint` is removed"; the v16 upgrade guide:
//   "Use Biome or ESLint directly"), and this repo is on next 16.3.8, whose CLI
//   registers no `lint` subcommand. So the script failed on every run instead of
//   linting — and it is worse than a plain "unknown command", because `next lint`
//   is not rejected as a bad subcommand. Commander reads the trailing word as the
//   `[directory]` argument of a fallback command and tries to build THAT
//   directory:
//
//     $ cd web && npm run lint
//     > jomform-web@0.1.0 lint
//     > next lint
//     Invalid project directory provided, no such directory: <web>/lint
//     EXIT=1
//
//   A person reading that failure is pointed at a directory that has nothing to
//   do with anything, which is why it needed a card to name it. The script was
//   removed. (It was not re-pointed at `eslint .` here because, measured on this
//   tree, that reports 86 errors / 63 warnings across web/src — a repo-wide lint
//   backlog is a separate card, and a gate red on `main` is not a gate.)
//
// WHAT THIS FILE PINS, AND WHY NEITHER LIST IS HARDCODED
//   1. GENERAL — for every step of every `web/package.json` script whose command
//      is `next`, the subcommand must be one the INSTALLED cli registers. That set
//      is read from the running `next --help` (measured: exit 0, ~0.2s, 11
//      commands), never written here, so a future Next that drops a command turns
//      this red naming the script — and a Next that re-adds one is not refused.
//      Only a WORD that is already a known subcommand, or a plausible name, is
//      judged: `next` alone (help) and `next <directory>` are out of scope,
//      because a bare directory argument and a subcommand are the same shape.
//   2. SPECIFIC — every `npm run <name>` the repo hands a reader must name a
//      script web/package.json defines. The referenced set is DERIVED by walking
//      the tree for `npm run` (measured: a11y, build, dev, docs:facts, e2e, start,
//      test:unit — all defined), so a doc that drifts from the manifest is caught
//      without anyone maintaining a list.
//
//   Both halves exist because the defect had both, one level apart: the script
//   was wired to a command that no longer exists (1), and the repo's own
//   references are exactly what keep an operator reaching for it (2).
//
// WHAT IT DOES NOT COVER (stated honestly)
//   * It does not run a linter, and no gate runs one. This is a wiring guard.
//   * `next --help` names the commands the cli REGISTERS. It is not a proof that
//     a command succeeds for this app — that is the `web` gate's job for
//     `next build`, and nothing's for the rest.
//   * The walk skips node_modules/.git/.next/out/coverage and build-output `bin/`,
//     and does not follow symlinks, so a reference in a generated or ignored
//     directory is not scanned.
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
import { spawnSync } from "node:child_process";

const HERE = dirname(fileURLToPath(import.meta.url)); // <repo>/web/src/lib
const WEB_DIR = resolve(HERE, "..", ".."); //            <repo>/web
const REPO_ROOT = resolve(WEB_DIR, ".."); //             <repo>
const NEXT_BIN = join(WEB_DIR, "node_modules", ".bin", "next");

const PKG = JSON.parse(readFileSync(join(WEB_DIR, "package.json"), "utf8")) as {
  scripts?: Record<string, string>;
};
const scripts: Record<string, string> = PKG.scripts ?? {};

/**
 * The `next` subcommands THIS tree's cli registers, read from `next --help`.
 *
 * The CLI prints a `Commands:` section, one command per line, indented two
 * spaces, the command name first (measured at next 16.3.8: build, dev, info,
 * start, telemetry, typegen, upgrade, experimental-analyze, experimental-test,
 * experimental-request-insights, internal). Parsing THAT, rather than globbing
 * `next/dist/cli/next-*.js`, keeps the oracle on the surface a user types.
 *
 * A failure to run `next --help` is a REFUSAL, not a skip: an empty set would
 * make test 1 below pass vacuously, which is the shape that hides a dead script.
 */
function nextCommands(): Set<string> {
  const run = spawnSync(NEXT_BIN, ["--help"], { encoding: "utf8" });
  assert.equal(
    run.status,
    0,
    `\`next --help\` must succeed to derive the subcommand set (it is the oracle for test 1). ` +
      `status=${run.status} stderr=${(run.stderr ?? "").trim()} — if the cli moved, fix this guard rather than skipping it`,
  );
  const out = `${run.stdout ?? ""}${run.stderr ?? ""}`;
  const start = out.split(/\r?\n/).findIndex((l) => /^Commands:/.test(l.trim()));
  const names = new Set<string>();
  if (start !== -1) {
    for (const line of out.split(/\r?\n/).slice(start + 1)) {
      const m = line.match(/^ {2}(\S+)/);
      if (!m) {
        // The commands block ends at the first non-indented, non-empty line.
        if (line.trim() !== "") break;
        continue;
      }
      names.add(m[1]);
    }
  }
  assert.ok(
    names.size >= 5,
    `derived only ${names.size} \`next\` subcommand(s) from \`next --help\` — the parse is broken, ` +
      `and an empty set would make the script check below pass vacuously. Output was:\n${out}`,
  );
  return names;
}

/** The steps of one npm script: `a && b || c; d` -> [a, b, c, d]. */
function stepsOf(script: string): string[] {
  return script
    .split(/&&|\|\||;/)
    .map((s) => s.trim())
    .filter((s) => s.length > 0);
}

/**
 * The command a step runs, with leading `VAR=value` assignments removed, plus its
 * arguments. Quote-free on purpose: every script in this manifest is simple, and
 * a step this cannot read (a quoted command, a pipeline) yields a command word
 * that will not match `next`, so it is simply not judged — the safe direction.
 */
function commandOf(step: string): { cmd: string; args: string[] } {
  const toks = step.split(/\s+/).filter((t) => t.length > 0 && !/^[A-Za-z_][A-Za-z0-9_]*=/.test(t));
  return { cmd: toks[0] ?? "", args: toks.slice(1) };
}

test("no web script invokes a `next` subcommand this tree's cli does not have", () => {
  const commands = nextCommands();
  const offenders: string[] = [];
  for (const [name, script] of Object.entries(scripts)) {
    for (const step of stepsOf(script)) {
      const { cmd, args } = commandOf(step);
      // `next` is the only command whose SUBCOMMAND is a name this tree can lose.
      // A bare `next` (help) and a directory argument are out of scope.
      if (cmd !== "next") continue;
      const sub = args[0];
      if (!sub || !/^[a-z][a-z0-9-]*$/.test(sub)) continue;
      if (commands.has(sub)) continue;
      offenders.push(
        `  scripts["${name}"] -> \`${step}\`: no \`${sub}\` subcommand in this \`next\` ` +
          `(have: ${[...commands].sort().join(", ")})`,
      );
    }
  }
  assert.deepEqual(
    offenders,
    [],
    "a web script delegates to a `next` subcommand the installed cli does not register, so the " +
      "script fails instead of doing its job (this is the `\"lint\": \"next lint\"` defect of Next 16, " +
      "which exits 1 with 'Invalid project directory provided, no such directory: <web>/lint'):\n" +
      offenders.join("\n"),
  );
});

/** Directories that hold no reference anyone reads, and build output that is not
 *  a reference at all. Skipping these keeps the walk to the repo's own files. */
const SKIP_DIRS = new Set([
  "node_modules",
  ".git",
  ".next",
  "out",
  "coverage",
  "test-results",
  "playwright-report",
  "blob-report",
  "bin", // api/bin and friends: build output, never a doc
]);

function walkFiles(dir: string, out: string[] = []): string[] {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (entry.isDirectory()) {
      if (SKIP_DIRS.has(entry.name)) continue;
      walkFiles(join(dir, entry.name), out);
    } else if (entry.isFile()) {
      out.push(join(dir, entry.name));
    }
    // Symlinks are NOT followed: web/node_modules is a symlink in some lanes'
    // worktrees, and following one could loop or leave the tree.
  }
  return out;
}

const NPM_RUN = /\bnpm run ([A-Za-z0-9:_.-]+)/g;

/**
 * Drop comment LINES — prose that quotes a command is documentation, not a
 * reference a reader acts on. Line-based and line-START-only, the same rule
 * `src/lib/buildContext.test.ts` uses: this file's own header quotes `npm run
 * lint` as the defect it pins, and without this the guard would report itself.
 *
 * PROSE FILES ARE NOT STRIPPED AT ALL. Markdown has no comment syntax, and a
 * bullet (`* npm run deploy`) is a reference a reader acts on — stripping it
 * would make this guard blind exactly where the repo talks to an operator
 * (measured: every `*`-leading `npm run` line in this tree is a JSDoc
 * continuation in a `.ts`/`.mjs` file, and no markdown bullet quotes a script,
 * so both arms are correct today and the prose check keeps them correct as docs
 * grow). An INLINE `// npm run x` is deliberately NOT stripped — leaving a
 * reference in widens the check, the safe direction.
 */
const PROSE_RE = /\.(md|markdown|mdx|txt|rst|adoc)$/i;
function stripCommentLines(src: string, file: string): string {
  if (PROSE_RE.test(file)) return src;
  return src
    .split(/\r?\n/)
    .filter((l) => {
      const t = l.trim();
      return !(t.startsWith("//") || t.startsWith("/*") || t.startsWith("*"));
    })
    .join("\n");
}

test("every `npm run <name>` in the repo names a script web/package.json defines", () => {
  const refs = new Map<string, Set<string>>(); // name -> files that mention it
  for (const file of walkFiles(REPO_ROOT)) {
    let text: string;
    try {
      text = readFileSync(file, "utf8");
    } catch {
      continue; // unreadable (a binary, a permission) — not a reference
    }
    if (text.includes("\0")) continue; // binary
    for (const m of stripCommentLines(text, file).matchAll(NPM_RUN)) {
      const name = m[1];
      if (!refs.has(name)) refs.set(name, new Set());
      refs.get(name)!.add(relative(REPO_ROOT, file));
    }
  }
  // The sweep must have found the references that exist, or it is scanning
  // nothing and would pass over a missing script.
  assert.ok(
    refs.size >= 4,
    `derived only ${refs.size} referenced npm script name(s) — the sweep is broken, and an empty ` +
      `set would make this test pass vacuously.`,
  );
  const offenders: string[] = [];
  for (const [name, files] of [...refs].sort()) {
    if (scripts[name] !== undefined) continue;
    offenders.push(
      `  \`npm run ${name}\` is referenced by ${[...files].sort().join(", ")} — but web/package.json ` +
        `defines no \`${name}\` script (defined: ${Object.keys(scripts).sort().join(", ")})`,
    );
  }
  assert.deepEqual(
    offenders,
    [],
    "the repo tells a reader to run an npm script that web/package.json does not define — the " +
      "reader gets `npm error Missing script: \"<name>\"`:\n" +
      offenders.join("\n"),
  );
});
