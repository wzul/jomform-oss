// The runner-evidence rule shared by the web/scripts bite harnesses
// (web/scripts/lib/node_test_verdict.sh, card t_b014807f).
//
// THE DEFECT THIS PINS. The harnesses read a `node --test` guard's verdict like this:
//
//   out=$(node --test --disable-warning=… "$1" 2>&1) || true
//   if grep -qE '^ℹ fail [1-9]' <<<"$out"; then printf 'RED'; else printf 'GREEN'; fi
//
// GREEN was the ELSE branch of "the runner reported a failure", so a capture carrying NO
// test-runner output at all — a node that rejects the flag, a node that cannot strip
// types, a broken install — was read as GREEN, and the harness printed a verdict it never
// measured. Measured on this host on one worktree with ONE base and only PATH differing:
// node v24.19.0 gave `BITEPROOF REFUSED` where /usr/bin/node v18.19.1 (a 64-byte
// "bad option" capture) gave `BITEPROOF FAIL (8 failing check(s), 1 passing)` — every leg
// that had to see RED read GREEN.
//
// WHAT THIS FILE PINS (all of it executable, none of it a grep over the harnesses' prose):
//
//   A. the reader answers GREEN / RED only off POSITIVE runner evidence, and REFUSED for
//      every capture that carries none (empty, the runner's own error line, TAP-only,
//      cannot-load, all-skipped, a passing summary with a non-zero exit status);
//   B. the PRE-FIX reader reads those same captures as GREEN — the control that shows the
//      fixtures discriminate, so a "fix" that changed nothing could not pass this file;
//   C. the capability gate refuses a `node` that answers `--version` and then cannot run
//      the guard, NAMING that node (v18.19.1 on this host does exactly that);
//   D. the gate end-to-end: a real harness, run with such a node first on PATH, exits 2
//      with a refusal that names it and never prints a PASS/FAIL verdict;
//   E. completeness: every script in web/scripts that runs `node --test` sources the shared
//      rule and calls the gate, so the family cannot drift back one script at a time.
//
// The shared rule is BASH and the harnesses are bash. `web/Dockerfile` runs this suite
// inside `node:24-alpine`, which ships no bash, so the four legs that spawn one state that
// absence and skip (the `webimage` gate builds exactly that image); the completeness leg is
// pure filesystem and still runs there.
//
// The file name deliberately does not contain "bite": the inventory guard
// (api/internal/biteharness) treats every *bite* basename as an on-demand instrument that
// must have a BITE-HARNESSES.md row, and this is a GATE test.

import { test } from "node:test";
import assert from "node:assert/strict";
import { execFileSync, spawn, spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url)); // web/src/lib
const webRoot = join(here, "..", "..");               // web/
const scriptsDir = join(webRoot, "scripts");
const RULE = join(scriptsDir, "lib", "node_test_verdict.sh");

// Real captures. The GREEN and RED fixtures are byte-for-byte shapes this host's
// node v24.19.0 printed for a passing and a failing guard; the others are the ways a
// runner can fail to produce a result at all.
const FIXTURES: Record<string, { rc: string; body: string; want: string }> = {
  empty: { rc: "0", body: "", want: "REFUSED" },
  "runner error only": {
    rc: "9",
    body: "node: bad option: --disable-warning=MODULE_TYPELESS_PACKAGE_JSON\n",
    want: "REFUSED",
  },
  "tap reporter only": {
    rc: "0",
    body: "TAP version 13\nok 1 - the probe\n1..1\n# tests 1\n# pass 1\n# fail 0\n",
    want: "REFUSED",
  },
  "cannot load (all fail, none pass)": {
    rc: "1",
    body: "✖ src/lib/does-not-load.test.ts (12.1ms)\nℹ tests 1\nℹ pass 0\nℹ fail 1\n",
    want: "REFUSED",
  },
  "all tests skipped": {
    rc: "0",
    body: "﹣ a test that never ran (0.1ms) # SKIP\nℹ tests 1\nℹ pass 0\nℹ fail 0\nℹ skipped 1\n",
    want: "REFUSED",
  },
  "passing summary, non-zero exit": {
    rc: "1",
    body: "✔ a test (0.2ms)\nℹ tests 1\nℹ pass 1\nℹ fail 0\n",
    want: "REFUSED",
  },
  "real green run": {
    rc: "0",
    body: "✔ a test (0.2ms)\nℹ tests 1\nℹ pass 1\nℹ fail 0\nℹ cancelled 0\nℹ skipped 0\n",
    want: "GREEN",
  },
  "real red run": {
    rc: "1",
    body: "✔ a test (0.2ms)\n✖ the assertion this case drives (0.3ms)\nℹ tests 2\nℹ pass 1\nℹ fail 1\n",
    want: "RED",
  },
};

const tmp = mkdtempSync(join(tmpdir(), "node-runner-evidence-"));
process.on("exit", () => rmSync(tmp, { recursive: true, force: true }));

let seq = 0;
function capture(body: string): string {
  const path = join(tmp, `capture-${seq++}.txt`);
  writeFileSync(path, body);
  return path;
}

// bashSkipReason — the shared rule and the harnesses are BASH. `web/Dockerfile` runs this
// suite inside `node:24-alpine`, which ships no bash, so every leg that spawns one must
// state that absence and skip rather than fail the image build on an ENOENT that says
// nothing about the code. Probed per test body, never at module load: a module-scope spawn
// is what once killed the image build before its skip could run (harnessLock.test.ts:603).
function bashSkipReason(): string | false {
  const r = spawnSync("bash", ["-c", "command -v bash"], { encoding: "utf8" });
  return r.status === 0
    ? false
    : `this tree has no bash on PATH (${r.error?.message ?? `exit ${r.status}`}), which is the ` +
        `web image build (node:24-alpine ships no bash). The shared rule is a bash library, so ` +
        `it cannot be exercised here; the completeness leg below still runs.`;
}

// readVerdict runs the SHARED rule in a fresh bash over one fixture and returns the
// verdict it prints on stdout.
function readVerdict(cap: string, rc: string): string {
  const script = `. "$1"; shift; nt_verdict_file "$1" "$2"`;
  return execFileSync("bash", ["-c", script, "bash", RULE, cap, rc], { encoding: "utf8" }).trim();
}

// preFixVerdict is the SHIPPED reader, kept here only as a control: it must answer GREEN
// on every capture above that carries no runner output, or these fixtures do not
// discriminate and the legs below prove nothing.
function preFixVerdict(cap: string): string {
  const script = `if grep -qE '^ℹ fail [1-9]' "$1"; then printf RED; else printf GREEN; fi`;
  return execFileSync("bash", ["-c", script, "bash", cap], { encoding: "utf8" }).trim();
}

test("the reader answers only off positive runner evidence, never off a missing failure line", (t) => {
  const why = bashSkipReason();
  if (why) return t.skip(why);
  for (const [label, f] of Object.entries(FIXTURES)) {
    assert.equal(
      readVerdict(capture(f.body), f.rc),
      f.want,
      `${label}: the shared reader must answer ${f.want}`,
    );
  }
});

test("the pre-fix reader reads every no-runner-output capture as GREEN (the control)", (t) => {
  const why = bashSkipReason();
  if (why) return t.skip(why);
  for (const [label, f] of Object.entries(FIXTURES)) {
    // The pre-fix reader keyed on the presence of `ℹ fail [1-9]`; a capture that carries
    // that line was RED then too (it is the cannot-load shape, where the old rule refused
    // nothing but at least did not print GREEN). Everything else it read as GREEN — that
    // is the defect, and it is the set this control proves the fixtures still reproduce.
    if (/^ℹ fail [1-9]/m.test(f.body)) continue;
    assert.equal(
      preFixVerdict(capture(f.body)),
      "GREEN",
      `${label}: the pre-fix reader is expected to answer GREEN here — if it does not, ` +
        `this fixture no longer reproduces the defect and the legs above prove nothing`,
    );
  }
});

// A `node` that answers `--version` and then cannot run the file: exactly the shape the
// host's /usr/bin/node v18.19.1 has, made portable so this leg does not depend on which
// node the gate itself runs under.
function nodeStub(dir: string): string {
  const stub = join(dir, "node");
  writeFileSync(
    stub,
    `#!/usr/bin/env bash
case "$*" in
  *--version*) echo "v18.19.1"; exit 0 ;;
esac
echo "node: bad option: --disable-warning=MODULE_TYPELESS_PACKAGE_JSON" >&2
exit 9
`,
  );
  chmodSync(stub, 0o755);
  return stub;
}

// THE LOCK DIR THIS FILE HANDS A SPAWNED HARNESS IS ITS OWN (card `t_1121a3c0`).
//
// Every harness of the family takes ONE host-wide, per-worktree `flock` and holds it for
// its WHOLE run (`web/scripts/biteproof_*.sh`, key `sha256(git rev-parse
// --absolute-git-dir)[:16]` under `/tmp/jomform-biteproof-docs-bite`; the rule and its
// eleven legs live in `harnessLock.test.ts`). Several of them run THIS suite while they
// hold it — `biteproof_settings_tabs.sh` and `biteproof_forms_bulk_failure.sh` through
// `npm run build`, `biteproof_next_managed_files.sh` through `npm run test:unit` — so a
// harness spawned from the end-to-end leg below inherited `/tmp` and asked for the lock
// its own caller was holding. Measured on this host 2026-10-29 on an installed tree
// (`web/node_modules` from `npm ci`, node v24.4.0): the leg sat in `flock -w 120` three
// times, once per retry, and then read the wait-out (exit 75) as a red — 6m20s for ONE
// case of the caller, and every harness that runs the suite went red for a reason that
// has nothing to do with the node this file puts on PATH.
//
// This leg MUTATES NOTHING (the capability gate refuses before the first case), so there
// is nothing for the family lock to serialise: it gets a private base, exactly as
// `harnessLock.test.ts:640-652` gives its own runs, and the assertion below pins that a
// held family lock cannot turn this leg into a wait-out.
const harnessLocks = join(tmp, "harness-locks");

function runWithStubPath(args: string[], env: Record<string, string> = {}) {
  const bin = join(tmp, "stub-bin");
  mkdirSync(bin, { recursive: true });
  mkdirSync(harnessLocks, { recursive: true });
  nodeStub(bin);
  const res = spawnSync("bash", args, {
    cwd: webRoot,
    encoding: "utf8",
    env: {
      ...process.env,
      PATH: `${bin}:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin`,
      BITEPROOF_LOCK_DIR: harnessLocks,
      ...env,
    },
  });
  return { status: res.status, stdout: res.stdout ?? "", stderr: res.stderr ?? "" };
}

// The family lock file for THIS worktree, re-derived from the rule the harnesses use
// (`biteproof_next_managed_files.sh:59-72` and `harnessLock.test.ts:474`) rather than
// hardcoded: a wrong key would make "the lock is held" true of a file nobody contends for
// and the premise below would pass vacuously.
function familyLockFile(): string | undefined {
  const r = spawnSync("git", ["rev-parse", "--absolute-git-dir"], { cwd: webRoot, encoding: "utf8" });
  if (r.status !== 0) return undefined;
  const key = createHash("sha256").update(r.stdout.trim()).digest("hex").slice(0, 16);
  return join(process.env.BITEPROOF_LOCK_DIR || "/tmp", "jomform-biteproof-docs-bite", `${key}.lock`);
}

function lockIsFree(lock: string): boolean {
  return spawnSync("flock", ["-n", lock, "true"], { encoding: "utf8" }).status === 0;
}

// Hold <lock> from a SEPARATE process — the shape a peer harness has. `flock <lock> -c cat`
// blocks on this process's stdin, so it is held exactly until `release()` ends it (the same
// helper and the same reasoning as `harnessLock.test.ts:515`; it is duplicated here so this
// file does not depend on a test that drives eleven harnesses).
function holdLock(lock: string) {
  const holder = spawn("flock", [lock, "-c", "cat"], { stdio: ["pipe", "ignore", "ignore"] });
  const ended = new Promise<void>((resolve) => holder.on("close", () => resolve()));
  return {
    release: async () => {
      holder.stdin?.end();
      await ended;
    },
  };
}

async function waitFor(cond: () => boolean, timeoutMs: number, what: string) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (cond()) return;
    await new Promise((r) => setTimeout(r, 20));
  }
  throw new Error(`timed out after ${timeoutMs}ms waiting for ${what}`);
}

test("the capability gate refuses a node that answers --version but cannot run the guard, and names it", (t) => {
  const why = bashSkipReason();
  if (why) return t.skip(why);
  const { status, stdout, stderr } = runWithStubPath([
    "-c",
    `. "$1"; nt_require_node`,
    "bash",
    RULE,
  ]);
  assert.equal(status, 2, `the gate must exit 2, not answer; got ${status}\n${stdout}${stderr}`);
  assert.match(stderr, /BITEPROOF REFUSED/, "the gate must refuse loudly");
  assert.match(stderr, /v\d+\.\d+\.\d+/, "the gate must name the node version it found");
  assert.match(stderr, /bad option/, "the gate must show what the runner actually printed");
});

test("end-to-end: a harness run with such a node REFUSES (exit 2, naming it) instead of printing a verdict", async (t) => {
  const why = bashSkipReason();
  if (why) return t.skip(why);
  // word_budget is the harness the card was filed on. The gate sits before every
  // mutation, so this leg touches nothing on disk.
  const harness = join(scriptsDir, "biteproof_docs_index_word_budget.sh");
  assert.ok(existsSync(harness), `${harness} must exist`);

  // THE PREMISE IS THE SHAPE THAT BROKE THIS LEG (card `t_1121a3c0`): a harness holds the
  // family lock. Take it ourselves unless one already does — which is the case whenever
  // this suite is itself running inside `npm run test:unit` under a harness
  // (`biteproof_next_managed_files.sh` did exactly that) — so both the plain suite run and
  // the nested one measure the same property instead of one of them skipping it.
  const lock = familyLockFile();
  if (!lock) return t.skip("cannot resolve this worktree's git dir, so there is no family lock to hold");
  // The property needs flock twice: to hold the lock and to ask whether it is held. A host
  // without it cannot run the family at all (every harness refuses), which
  // `harnessLock.test.ts` asserts on its own; skipping here keeps this leg from failing for
  // a reason that is not about the node it puts on PATH.
  if (spawnSync("bash", ["-c", "command -v flock"], { encoding: "utf8" }).status !== 0) {
    return t.skip("no flock on PATH, so the family lock this leg holds cannot be taken");
  }
  mkdirSync(dirname(lock), { recursive: true });
  const holding = lockIsFree(lock) ? holdLock(lock) : undefined;
  try {
    await waitFor(() => !lockIsFree(lock), 5000, "the family lock to be held by a peer");
    // `BITEPROOF_LOCK_WAIT=0`: if this spawn were serialised by that held lock it would
    // answer 75 (wait-out) at once and print the waiting line, which is what the pre-fix
    // bytes did after 3 x 120s. Zero turns a 6-minute stall into a named read.
    const res = runWithStubPath([harness], { BITEPROOF_LOCK_WAIT: "0" });
    assert.doesNotMatch(
      `${res.stdout}${res.stderr}`,
      /holds this worktree's lock/,
      `the harness this leg spawns must not be serialised by the family lock (${lock}); it ` +
        `mutates nothing, so a wait here proves the spawn inherited the caller's lock dir. Got exit ${res.status}.`,
    );
    assert.notEqual(res.status, 75, `the spawn waited out the family lock instead of reaching the gate:\n${res.stdout}${res.stderr}`);
    assert.equal(res.status, 2, `want exit 2 (refused), got ${res.status}\n${res.stdout}${res.stderr}`);
    assert.match(res.stderr, /BITEPROOF REFUSED/, "the refusal must be loud");
    assert.match(res.stderr, /v18\.19\.1/, "the refusal must name the node it found");
    assert.doesNotMatch(res.stdout, /BITEPROOF (PASS|FAIL)/, "no verdict may be printed");
  } finally {
    await holding?.release();
  }
});

test("every harness that runs a node guard sources the shared rule and calls the gate", () => {
  const scripts = readdirSync(scriptsDir).filter((f) => f.endsWith(".sh"));
  // A harness drives the runner either directly (`node --test`) or through the shared
  // capture helper (`nt_capture`) — the six readers this card ported now use the latter.
  const runners = scripts.filter((f) => {
    const text = readFileSync(join(scriptsDir, f), "utf8");
    return text.includes("node --test") || text.includes("nt_capture");
  });
  assert.ok(runners.length >= 16, `expected the whole family, found ${runners.length}: ${runners.join(", ")}`);
  for (const f of runners) {
    const text = readFileSync(join(scriptsDir, f), "utf8");
    assert.match(text, /node_test_verdict\.sh/, `${f} must source the shared runner rule`);
    assert.match(text, /^nt_require_node/m, `${f} must call the capability gate`);
  }
});
