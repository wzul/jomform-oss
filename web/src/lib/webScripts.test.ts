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
//      RESOLVES to `next`, the subcommand must be one the INSTALLED cli registers.
//      That set is read from the running `next --help` (measured: exit 0, ~0.2s, 11
//      commands), never written here, so a future Next that drops a command turns
//      this red naming the script — and a Next that re-adds one is not refused.
//      Only a WORD that is already a known subcommand, or a plausible name, is
//      judged: `next` alone (help) and `next <directory>` are out of scope,
//      because a bare directory argument and a subcommand are the same shape.
//
//      The command is resolved THROUGH the wrappers that only LAUNCH it — `npx`
//      (and `npm exec`/`npm x`), `pnpx`, `pnpm exec`/`pnpm dlx`, `bunx`/`bun x`,
//      `yarn exec`/`yarn dlx`, `env`, `node`, and an explicit
//      `node_modules/.bin/next` path — so a step that reaches the SAME cli one
//      wrapper out is judged the same (cards t_266bec68, t_1ecbb32c). Measured
//      BEFORE that fix, on the head this guard shipped at, by mutation with
//      `web/package.json` restored byte-identically each time (md5 8cfe24f0
//      before and after): `"lint": "npx next lint"` and
//      `"lint": "./node_modules/.bin/next lint"` were both GREEN (MISSED) while
//      the bare `"lint": "next lint"` was RED — and both MISSED shapes still exit
//      1 with `Invalid project directory provided, no such directory: <web>/lint`,
//      so they are the same live failure, not a theoretical one. This matters
//      more now than it did before the fix: the script is gone, so the most
//      natural re-introduction (`npx next lint`, which is what most Next 15-era
//      docs and muscle memory produce) is exactly the shape that was unwatched.
//
//      `node_modules/.bin/next` is itself a SYMLINK, so the resolution does not
//      stop at the symlink spelling: it is judged at the file it points at too
//      (`node_modules/next/dist/bin/next`, next's own declared `bin`), because
//      naming the target runs the SAME cli and the SAME dead command. Measured
//      on this tree (card t_1ecbb32c), mutating `"lint"` next to `"start"` with
//      the manifest restored byte-identically each time (md5 144542c1804ea50f5a8ad5fc59eb393d
//      before and after): `"lint": "node node_modules/next/dist/bin/next lint"`,
//      `"lint": "./node_modules/next/dist/bin/next lint"` and
//      `"lint": "./node_modules/.bin/../next/dist/bin/next lint"` were all GREEN
//      (MISSED), while `"lint": "./node_modules/.bin/next lint"` was already RED.
//      The MISSED shapes reach the same live failure: `node
//      node_modules/next/dist/bin/next lint` exits 1 with `Invalid project
//      directory provided, no such directory: <web>/lint`, one resolution step
//      deeper than the symlink the card before this one fixed.
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
//   * A step it cannot read as command words is NOT judged, and that is the safe
//     direction (not judged rather than guessed wrong) but it is still a gap: a
//     quoted command (`sh -c "next lint"` reads as command `sh`, not `next`), a
//     pipeline or a command substitution, and a launcher flag that takes a
//     SEPARATE value (`env -u NAME next lint` leaves `NAME` in the command slot).
//     `npm run <script>` steps are test 2's subject, not this one's.
//   * `node <file>` is judged when the FILE is this cli (`node
//     node_modules/next/dist/bin/next lint`). It is NOT a general "any `node`
//     step" rule: a step whose `node` target is anything else — a helper script
//     (`node scripts/web-deps-check.mjs`), or a path that merely ends in `next`
//     without the `.../next/dist/bin/next` shape — is left unjudged, the safe
//     direction. A path spelled with a SYMLINK the guard cannot see (a `.bin`
//     entry of another name, say) is therefore not resolved — only the two
//     spellings next actually ships (`.bin/next` and its `dist/bin/next` target)
//     are.
//   * The walk is a FILESYSTEM walk, not `git ls-files`, so an `npm run` in an
//     UNTRACKED file of this tree is still read — a reference a reader here can
//     act on is a reference whether or not it is committed. The probe tests at
//     the bottom of this file pin that in both directions.
//   * It reads THIS tree only. A nested checkout — any subdirectory carrying its
//     own `.git` entry — and the primary checkout's lane root `.worktrees/` are
//     skipped, because their `npm run` references belong to another branch and
//     are not this tree's to judge. Measured on the primary checkout of this
//     repo: 1,459 lane checkouts under `.worktrees/` holding ~1.8M files, which
//     made this test take over 6m41s there, and made it fail with `ENOENT:
//     scandir '<repo>/.worktrees/t_…'` when a lane removed its worktree
//     mid-walk — a RED that said nothing about the tree under test.
//   * A directory that vanishes between being listed and being read is SKIPPED,
//     not failed: lanes remove worktrees and builds delete output while this
//     runs. The `refs.size >= 4` floor in the sweep keeps that from turning into
//     a green scan of nothing.
//   * The walk skips node_modules/.git/.next/out/coverage, the test-output
//     directories, build-output `bin/` and `.worktrees/`, and does not follow
//     symlinks, so a reference in a generated or ignored directory is not
//     scanned.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, mkdirSync, readFileSync, readdirSync, rmSync, rmdirSync, writeFileSync } from "node:fs";
import type { Dirent } from "node:fs";
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
 * Command words (and the two-word forms) that only LAUNCH the command that
 * follows them, so a `next` behind one is the SAME binary reached another way.
 *
 * Each is a real way to run the binary installed in `node_modules/.bin`, which is
 * what `npm run` puts on PATH: `npx` (`npm exec`'s shorthand) and `npm exec`/
 * `npm x`, `pnpx` and `pnpm exec`/`pnpm dlx`, `bunx`/`bun x`, `yarn exec`/`yarn
 * dlx`, `env`, which execs the command it is given, and `node`, which runs the
 * file it is given — and `node_modules/.bin/next` IS a JS file
 * (`node_modules/next/dist/bin/next`, `#!/usr/bin/env node`), so `node
 * node_modules/…/next` is one more spelling of the same cli (card t_1ecbb32c).
 *
 * Longest forms first, so `npm exec` is matched before a bare `npm` prefix ever
 * could be — a bare `npm` is deliberately NOT a launcher: `npm run <script>` is
 * test 2's subject, and skipping `npm` would read `npm run lint` as a `next` step.
 */
const LAUNCH_PREFIXES: readonly string[][] = [
  ["npm", "exec"],
  ["npm", "x"],
  ["pnpm", "exec"],
  ["pnpm", "dlx"],
  ["yarn", "exec"],
  ["yarn", "dlx"],
  ["bun", "x"],
  ["npx"],
  ["pnpx"],
  ["bunx"],
  ["env"],
  ["node"],
];

/**
 * Is this token the `next` cli — by bare name, by a `node_modules/.bin/next`
 * path (`./node_modules/.bin/next`, an absolute one, another tree's), or by the
 * file that path RESOLVES to, `node_modules/next/dist/bin/next`?
 *
 * The dist spelling is the same cli: `node_modules/.bin/next` is a symlink to
 * it, and `node_modules/next/package.json` declares `"bin": {"next":
 * "./dist/bin/next"}`, so naming either reaches one binary (card t_1ecbb32c).
 * The tail `next/dist/bin/next` is matched on a `/` boundary (or at the start),
 * which is what makes the three measured MISSED spellings judge alike —
 * `node_modules/next/dist/bin/next`, `./…`, and `node_modules/.bin/../next/dist/
 * bin/next` (the `..` spelling, which the shell would normalize to the first).
 *
 * A WHOLE-token match, never a substring: `next-intl`, `next-auth` and `eslint`
 * are different programs and must not be judged as this cli, so the wrapper rule
 * cannot widen into "anything with `next` in it". The same care is why the
 * dist arm is anchored on the full `next/dist/bin/next` tail: a `next-intl` path
 * ends in `/bin/next-intl`, not `/bin/next`.
 */
function isNextWord(token: string): boolean {
  return (
    token === "next" ||
    /(?:^|\/)node_modules\/\.bin\/next$/.test(token) ||
    /(?:^|\/)next\/dist\/bin\/next$/.test(token)
  );
}

/**
 * The command a step runs, with leading `VAR=value` assignments and any launcher
 * prefix removed, plus its arguments.
 *
 * Quote-free on purpose: every script in this manifest is simple, and a step this
 * cannot read (a quoted command, a pipeline, a command substitution) yields a
 * command word that will not match `next`, so it is simply not judged — the safe
 * direction. A launcher's own `-`-flags are skipped with it (`npx --yes next
 * lint`); a flag that takes a SEPARATE value (`env -u NAME …`) leaves its value in
 * the command slot, which does not match `next` and is therefore not judged
 * rather than guessed at — the same safe direction.
 */
function commandOf(step: string): { cmd: string; args: string[] } {
  const toks = step
    .split(/\s+/)
    .filter((t) => t.length > 0 && !/^[A-Za-z_][A-Za-z0-9_]*=/.test(t));
  let i = 0;
  for (;;) {
    const prefix = LAUNCH_PREFIXES.find((p) => p.every((w, k) => toks[i + k] === w));
    if (!prefix) break;
    i += prefix.length;
    while (i < toks.length && toks[i].startsWith("-")) i += 1;
  }
  return { cmd: toks[i] ?? "", args: toks.slice(i + 1) };
}

/**
 * Every `scripts[name]` step that delegates to a `next` subcommand the installed
 * cli does not register.
 *
 * Split out of test 1 so the wrapper cases below drive the SAME walk over fixture
 * scripts instead of re-implementing it — a second resolver written for the tests
 * could agree with itself while disagreeing with the guard.
 */
function nextSubcommandOffenders(
  source: Record<string, string>,
  commands: Set<string>,
): string[] {
  const offenders: string[] = [];
  for (const [name, script] of Object.entries(source)) {
    for (const step of stepsOf(script)) {
      const { cmd, args } = commandOf(step);
      // `next` is the only command whose SUBCOMMAND is a name this tree can lose.
      // A bare `next` (help) and a directory argument are out of scope.
      if (!isNextWord(cmd)) continue;
      const sub = args[0];
      if (!sub || !/^[a-z][a-z0-9-]*$/.test(sub)) continue;
      if (commands.has(sub)) continue;
      offenders.push(
        `  scripts["${name}"] -> \`${step}\`: no \`${sub}\` subcommand in this \`next\` ` +
          `(have: ${[...commands].sort().join(", ")})`,
      );
    }
  }
  return offenders;
}

test("no web script invokes a `next` subcommand this tree's cli does not have", () => {
  const offenders = nextSubcommandOffenders(scripts, nextCommands());
  assert.deepEqual(
    offenders,
    [],
    "a web script delegates to a `next` subcommand the installed cli does not register, so the " +
      "script fails instead of doing its job (this is the `\"lint\": \"next lint\"` defect of Next 16, " +
      "which exits 1 with 'Invalid project directory provided, no such directory: <web>/lint'):\n" +
      offenders.join("\n"),
  );
});

test("a `next <sub>` is judged through the wrappers that only launch it", () => {
  const commands = nextCommands();
  // The premise of every case below is asserted rather than assumed: `lint` is not
  // a subcommand this cli registers, so reaching it — however the step is spelled
  // — is the dead command the guard exists to stop. A Next that re-adds `lint`
  // would make these cases prove nothing, and must say so instead of passing.
  assert.equal(
    commands.has("lint"),
    false,
    "this cli now registers a `lint` subcommand, so the cases below no longer describe a dead " +
      `command — pick another subcommand for them. Have: ${[...commands].sort().join(", ")}`,
  );

  const judge = (step: string) => nextSubcommandOffenders({ probe: step }, commands).length > 0;

  // [a step as a script writes it, must it be JUDGED, why]
  const cases: Array<[string, boolean, string]> = [
    // The two shapes measured MISSED on the head this guard shipped at, by
    // mutation with the manifest restored byte-identically (card t_266bec68):
    // the same defect one wrapper out.
    ["npx next lint", true, "`npx` is transparent — the same cli runs"],
    ["./node_modules/.bin/next lint", true, "an explicit .bin path is the same cli"],
    // ... and the file that path is a SYMLINK TO. Measured MISSED on the head
    // this card fixed, by the same mutation method (card t_1ecbb32c): naming
    // `next/dist/bin/next` runs one identical binary and one identical dead
    // command, so the resolution must not stop at the symlink spelling.
    ["node node_modules/next/dist/bin/next lint", true, "`node` runs the CLI file; the resolved dist path is the same cli"],
    ["./node_modules/next/dist/bin/next lint", true, "the dist path without the `node` launcher (it is an executable script)"],
    ["./node_modules/.bin/../next/dist/bin/next lint", true, "the `.bin` symlink spelled with a `..` that resolves to the same dist file"],
    // The rest of the launcher surface: each is a real way a step reaches the
    // binary `npm run` puts on PATH.
    ["npx --yes next lint", true, "a launcher's own flag is skipped with the launcher"],
    ["npm exec next lint", true, "`npm exec` is what `npx` abbreviates"],
    ["env next lint", true, "`env` execs the command it is given"],
    ["node_modules/.bin/next lint", true, "the .bin path without the `./`"],
    ["cd . && next lint", true, "stepsOf splits `&&`, so the `next lint` step is judged alone"],
    ["cd web && npx next lint", true, "a `cd` prefix AND a launcher"],
    // Controls: the shapes the guard already caught must STAY caught, or the
    // resolution above would be a rewrite that lost the original teeth.
    ["next lint", true, "the original shape (control)"],
    ["next  lint", true, "double space (control)"],
    // The other direction, and the reason this is a resolution and not "red
    // anything near a launcher": a REGISTERED subcommand stays quiet.
    ["npx next build", false, "`build` IS a subcommand of this cli"],
    ["./node_modules/.bin/next start", false, "`start` IS a subcommand of this cli"],
    ["node node_modules/next/dist/bin/next build", false, "`build` IS a subcommand of this cli, reached by its resolved path"],
    // Not judged, deliberately — the safe direction, and the reason the wrapper
    // rule is a whole-token match.
    ["npx some-other-linter lint", false, "another tool's `lint`, not this cli"],
    ["npx next-lint", false, "`next-lint` is a different binary, not this cli"],
    ["node node_modules/next-intl/dist/bin/next-intl lint", false, "`next-intl` has its OWN dist path — the dist arm must not match it"],
    ["node scripts/web-deps-check.mjs", false, "`node <script>` is not this cli"],
    ["sh -c \"next lint\"", false, "a quoted command — the command word reads `sh`, not `next`"],
  ];

  const wrong = cases
    .filter(([step, want]) => judge(step) !== want)
    .map(
      ([step, want, why]) =>
        `  \`${step}\` -> ${judge(step) ? "judged" : "not judged"}, wanted ` +
        `${want ? "judged" : "not judged"} (${why})`,
    );
  assert.deepEqual(
    wrong,
    [],
    "the wrapper resolution does not judge the way this file's header says it does. A `next ... " +
      "lint` step is the Next-16 dead command whether or not a launcher sits in front of it; the " +
      "MISSED shapes at the top of the case list are the coverage gaps cards t_266bec68 (the " +
      "`npx` / `.bin` wrappers) and t_1ecbb32c (the `.bin` SYMLINK's own resolved target, " +
      "`node_modules/next/dist/bin/next`) measured:\n" +
      wrong.join("\n"),
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
  // The PRIMARY checkout's lane root (`git worktree add .worktrees/<task>`):
  // every child is another lane's whole checkout, on another branch, and its
  // `npm run` references are not this tree's. Skipped by NAME as well as by the
  // nested-checkout rule below, because a lane that is being removed can leave a
  // child without its `.git` file, and a half-removed sibling must not be read.
  ".worktrees",
]);

/**
 * A directory carrying its own `.git` entry is a DIFFERENT checkout — a lane's
 * linked worktree (`.git` is a FILE there, naming the primary's gitdir), a clone
 * dropped inside this tree, a vendored repo. Its files are another branch's
 * files: judging its `npm run` references against THIS tree's manifest is how
 * the guard goes red for a reason that has nothing to do with the tree under
 * test. So the walk stops at its boundary. */
function isNestedCheckout(dir: string): boolean {
  return existsSync(join(dir, ".git"));
}

function walkFiles(dir: string, out: string[] = []): string[] {
  let entries: Dirent[];
  try {
    entries = readdirSync(dir, { withFileTypes: true });
  } catch (err) {
    // A directory can be gone between its parent being listed and this read:
    // lanes remove worktrees and builds delete output while the suite runs. That
    // race is not a finding about the tree under test, so the directory is
    // skipped. Anything else (a permission, an I/O error) still throws — the
    // `refs.size` floor below is what stops a swallow from becoming "scanned
    // nothing, passed".
    const code = (err as NodeJS.ErrnoException).code;
    if (code === "ENOENT" || code === "ENOTDIR") return out;
    throw err;
  }
  for (const entry of entries) {
    if (entry.isDirectory()) {
      if (SKIP_DIRS.has(entry.name)) continue;
      const child = join(dir, entry.name);
      if (isNestedCheckout(child)) continue;
      walkFiles(child, out);
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

/**
 * The `npm run <name>` references in this tree: script name -> the files of
 * `root` that mention it.
 *
 * Split out of the guard so its own probes can drive the SAME sweep: a second
 * walker written for the probes could agree with itself while disagreeing with
 * the guard, and the probes below are what keep the skip honest in both
 * directions (it must still see this tree's untracked files, and must not read
 * another checkout's).
 */
function referencedScripts(root: string = REPO_ROOT): Map<string, Set<string>> {
  const refs = new Map<string, Set<string>>();
  for (const file of walkFiles(root)) {
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
      refs.get(name)!.add(relative(root, file));
    }
  }
  return refs;
}

/** Every referenced name `web/package.json` does not define, one line each. */
function scriptsMissingFromManifest(refs: Map<string, Set<string>>): string[] {
  const offenders: string[] = [];
  for (const [name, files] of [...refs].sort()) {
    if (scripts[name] !== undefined) continue;
    offenders.push(
      `  \`npm run ${name}\` is referenced by ${[...files].sort().join(", ")} — but web/package.json ` +
        `defines no \`${name}\` script (defined: ${Object.keys(scripts).sort().join(", ")})`,
    );
  }
  return offenders;
}

test("every `npm run <name>` in the repo names a script web/package.json defines", () => {
  const refs = referencedScripts();
  // The sweep must have found the references that exist, or it is scanning
  // nothing and would pass over a missing script.
  assert.ok(
    refs.size >= 4,
    `derived only ${refs.size} referenced npm script name(s) — the sweep is broken, and an empty ` +
      `set would make this test pass vacuously.`,
  );
  const offenders = scriptsMissingFromManifest(refs);
  assert.deepEqual(
    offenders,
    [],
    "the repo tells a reader to run an npm script that web/package.json does not define — the " +
      `reader gets \`npm error Missing script: "<name>"\`:\n` +
      offenders.join("\n"),
  );
});

/** A probe path that cannot collide with a real one, even across lanes. */
function probePath(tag: string): string {
  return `zz-webscripts-${tag}-probe-${process.pid}`;
}

// The two directions the walk's boundary has to hold, on the REAL root. Without
// them a fix for the host cost above could be "skip everything the walk did not
// need", and the guard would still be green while reading nothing.
test("the sweep still reads an `npm run` in an UNTRACKED file of this tree", () => {
  const file = join(REPO_ROOT, `${probePath("untracked")}.md`);
  const name = "zz-probe-untracked-script";
  writeFileSync(file, `Deploy with \`npm run ${name}\`.\n`, "utf8");
  try {
    const refs = referencedScripts();
    assert.deepEqual(
      [...(refs.get(name) ?? [])],
      [relative(REPO_ROOT, file)],
      "the sweep did not read an untracked file of this tree, so it is no longer a walk of the " +
        "tree — a doc added here before it is committed would be unwatched",
    );
    // ... and the reference must be JUDGED, not just collected: a name the
    // manifest does not define is exactly what the guard exists to report.
    assert.equal(
      scriptsMissingFromManifest(refs).some((o) => o.includes(name)),
      true,
      `a reference to an undefined script (${name}) was read but not reported, so the sweep ` +
        "collects it and the guard never judges it",
    );
  } finally {
    rmSync(file, { force: true });
  }
});

test("the sweep does not read a NESTED checkout's files", () => {
  const dir = join(REPO_ROOT, probePath("nested"));
  // The name is interpolated rather than written literally: this file is inside
  // the tree the sweep reads, and its INLINE `npm run <name>` references are
  // deliberately NOT stripped (see stripCommentLines), so a literal here would be
  // collected as a real reference and red the guard against its own probe.
  const name = "zz-probe-nested-script";
  mkdirSync(dir, { recursive: true });
  writeFileSync(join(dir, ".git"), "gitdir: /nonexistent/another-checkout/.git\n", "utf8");
  writeFileSync(join(dir, "notes.md"), `Run \`npm run ${name}\`.\n`, "utf8");
  try {
    assert.equal(
      referencedScripts().has(name),
      false,
      "the sweep walked into a nested checkout (a directory with its own `.git`), so another " +
        "branch's `npm run` references are being judged against this tree's manifest",
    );
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("a directory that vanished under the walk is skipped, not failed", () => {
  // Never created: to the walk this is exactly what a lane removing its worktree
  // between the parent's listing and the read looks like, and what made this test
  // fail with `ENOENT: scandir '<repo>/.worktrees/t_…'` — a RED that named a
  // sibling lane and nothing about the tree under test. Anything else (EACCES, an
  // I/O error) must still throw; this is a race, not a licence to ignore failures.
  const gone = join(REPO_ROOT, probePath("vanished"));
  assert.deepEqual(
    [...referencedScripts(gone)],
    [],
    "reading a directory that is not there threw instead of being skipped, so a lane removing a " +
      "worktree mid-walk still reds this guard",
  );
});

test("the sweep does not read the primary checkout's lane worktrees", () => {
  const laneRoot = join(REPO_ROOT, ".worktrees");
  const laneRootPreexisting = existsSync(laneRoot);
  const dir = join(laneRoot, probePath("lane"));
  const name = "zz-probe-lane-script"; // interpolated, not literal — see the probe above
  mkdirSync(dir, { recursive: true });
  writeFileSync(join(dir, "notes.md"), `Run \`npm run ${name}\`.\n`, "utf8");
  try {
    assert.equal(
      referencedScripts().has(name),
      false,
      "the sweep read a sibling lane's checkout under .worktrees/ — that is the shape that made " +
        "this test walk ~1.8M files in the primary checkout",
    );
  } finally {
    rmSync(dir, { recursive: true, force: true });
    if (!laneRootPreexisting) {
      try {
        rmdirSync(laneRoot); // only if the probe created it, and only when empty
      } catch {
        // a lane appeared in it meanwhile — leave it
      }
    }
  }
});
