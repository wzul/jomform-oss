#!/usr/bin/env node
// web/scripts/npm-audit-gate.mjs — `npm audit --audit-level=high`, with a
// NARROW, SELF-EXPIRING exception list (web/npm-audit-exceptions.json).
//
// WHY THIS EXISTS
//   The `npmaudit` gate is `cd web && npm audit --audit-level=high`, and it sits
//   mid-list in scripts/preflight.sh: the runner STOPS at its first failure, so a
//   red here makes every gate after it unreachable and blocks the deploy build
//   (that is exactly what card t_176555e3 / PR 1025 had to fix for `sharp`). Its
//   default is therefore correct and must stay: nothing may be silenced quietly.
//
//   But the repo now depends on `eslint-config-next`, which pulled
//   `braces@3.0.3` — and braces had an advisory with NO FIXED RELEASE published
//   (`npm view braces@3.0.4` is a 404; dist-tags.latest is still 3.0.3). The only
//   thing npm offered was a DOWNGRADE of eslint-config-next to 14.2.35, which
//   would remove the Next 16 flat config the `lint` gate exists to run. So the
//   choice was not "fix it or leave it red" — it was "name it precisely and
//   time-box it" or "do not add the linter at all". This file is the first
//   option, and it is built so it cannot become the second kind of thing (a
//   blanket ignore).
//
//   THAT EXCEPTION IS GONE, AND THE LIST IS EMPTY ON PURPOSE (card t_6cc165c6).
//   npm no longer reports the advisory at all: GitHub WITHDREW GHSA-vfj7-8cjw-p6xm
//   upstream on 2026-10-23 ("Withdrawn Advisory") while braces stayed at 3.0.3, so
//   there is no fixed release to upgrade to and nothing left to except. The gate
//   printed it as STALE until the entry was removed — which is this file's
//   staleness rule doing its job. An empty `exceptions` array is the healthy
//   state; do not read it as "the check is off".
//
// WHAT MAKES IT NOT A BLANKET IGNORE
//   1. An exception is a SPECIFIC advisory URL. Any advisory not named here fails
//      the gate exactly as before, and npm's own report is printed so the reader
//      sees it.
//   2. `expires` is REQUIRED. A missing or unparseable one is refused; a past one
//      FAILS the gate. An exception cannot silently become permanent.
//   3. STALENESS is reported: an entry no longer present in the audit is printed
//      so it is removed. There is no failure for it (a stale entry is harmless to
//      safety, only untidy) — it is printed loudly instead.
//   4. It judges the SAME thing the gate judged: `npm audit --audit-level=high`
//      exit code 0/1 and the high/critical set. Moderate/low are not this gate's
//      business and are not consulted.
//   5. THE VERDICT IS RECONCILED, NOT DERIVED FROM THE PARSED OBJECTS ALONE
//      (card t_e6041c4f). It used to be: the ONLY failure condition was "an
//      advisory URL in `via` objects that is not in the exceptions file". So a
//      report that is parseable but whose `via` entries carry no advisory OBJECT
//      — only the package-NAME propagation chain — yielded zero advisories and the
//      gate printed PASS while `npm audit` exited 1. Measured by QA on PR 1028's
//      head: a stub npm emitting `{"vulnerabilities":{}, "metadata":{…
//      "high":5}}` and exiting 1 made this gate exit 0. That is a FAIL-OPEN, and
//      it is the worst shape for a gate that sits mid-list: a silent pass here
//      hides every gate after it.
//
//      The reconciliation is three refusals, each printed as "cannot be judged"
//      rather than as PASS (see `reconcile` below): the exit code must agree with
//      the parsed high/critical set, `metadata.vulnerabilities.high|critical`
//      must EQUAL the number of high/critical entries in `vulnerabilities`, and
//      every high/critical package must trace to a named advisory URL through
//      its `via` chain. None of these can be satisfied by a report with the
//      advisory objects stripped. Whatever the tree reports, the three counts
//      agree on a real audit (measured while the braces exception was in place:
//      5 high == 5 high entries, all five tracing to that one advisory).
//
// USAGE
//   node scripts/npm-audit-gate.mjs            # from web/
//   exit 0 = no unexcepted advisory; 1 = there is one, or the exception file is
//   malformed/expired, or the report could not be judged.

import { execFileSync } from "node:child_process";
import { readFileSync, existsSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const WEB_DIR = resolve(HERE, "..");
const EXC_PATH = join(WEB_DIR, "npm-audit-exceptions.json");

/** Run the gate's own command and return { rc, report }. */
function runAudit() {
  let out = "";
  let rc = 0;
  try {
    out = execFileSync("npm", ["audit", "--audit-level=high", "--json"], {
      cwd: WEB_DIR,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "pipe"],
    });
  } catch (e) {
    // npm audit exits 1 when it finds something — that is a RESULT, not a crash.
    rc = typeof e.status === "number" ? e.status : 1;
    out = `${e.stdout ?? ""}`;
    if (!out.trim()) {
      console.error("npm-audit-gate: `npm audit` produced no JSON at all — cannot judge it.");
      console.error(String(e.stderr ?? e.message ?? "").slice(0, 2000));
      process.exit(1);
    }
  }
  let report;
  try {
    report = JSON.parse(out);
  } catch {
    console.error("npm-audit-gate: `npm audit --json` output is not JSON — cannot judge it.");
    console.error(out.slice(0, 2000));
    process.exit(1);
  }
  return { rc, report };
}

/** The severity set this gate judges. `--audit-level=high` is the threshold. */
const HIGH_CRITICAL = new Set(["high", "critical"]);

/**
 * The advisory URLs the audit reports, each with the packages it reaches.
 *
 * `npm audit --json` lists VULNERABILITIES (packages) whose `via` array carries
 * either the advisory OBJECT or another package NAME through which it arrives.
 * Only the objects are advisories — the names are the propagation chain, and
 * counting them as advisories would invent URLs that do not exist.
 */
function advisoriesOf(report) {
  const found = new Map(); // url -> Set(package)
  for (const [name, entry] of Object.entries(report.vulnerabilities ?? {})) {
    for (const via of entry.via ?? []) {
      if (typeof via !== "object" || via === null) continue;
      const url = via.url;
      if (!url) continue;
      if (!found.has(url)) found.set(url, new Set());
      found.get(url).add(name);
    }
  }
  return found;
}

/** The `vulnerabilities` entries at high or critical severity, by package name. */
function highCriticalPackages(report) {
  const out = new Map();
  for (const [name, entry] of Object.entries(report.vulnerabilities ?? {})) {
    if (typeof entry !== "object" || entry === null) continue;
    if (HIGH_CRITICAL.has(String(entry.severity ?? "").toLowerCase())) out.set(name, entry);
  }
  return out;
}

/**
 * The advisory URLs a high/critical package is CAUSED BY, walked through the
 * `via` propagation chain (card t_e6041c4f).
 *
 * A meta-vulnerability names the packages it arrives through as STRINGS — that is
 * the only link from, say, `eslint-config-next` to the `braces` advisory object
 * five steps down. So a package's own advisory set is the union over every node
 * reachable from it: itself plus each NAME in `via`, transitively. A package that
 * reaches no URL anywhere in its chain is one this gate cannot name — it is
 * returned as untraceable rather than silently dropped, and `reconcile` refuses
 * the report over it.
 *
 * Cycles are impossible in npm's output but are guarded anyway: a visited set
 * bounds the walk, so a malformed report cannot hang the gate.
 */
function advisoryURLsReaching(name, report) {
  const urls = new Set();
  const seen = new Set();
  const queue = [name];
  while (queue.length > 0) {
    const current = queue.shift();
    if (seen.has(current)) continue;
    seen.add(current);
    const entry = report.vulnerabilities?.[current];
    if (typeof entry !== "object" || entry === null) continue;
    for (const via of entry.via ?? []) {
      if (typeof via === "object" && via !== null) {
        if (via.url) urls.add(via.url);
        continue;
      }
      // A string is the propagation chain: follow it to the package it names.
      if (typeof via === "string" && via) queue.push(via);
    }
  }
  return urls;
}

function loadExceptions() {
  if (!existsSync(EXC_PATH)) return new Map();
  let raw;
  try {
    raw = JSON.parse(readFileSync(EXC_PATH, "utf8"));
  } catch (e) {
    console.error(`npm-audit-gate: ${EXC_PATH} is not readable JSON: ${e.message}`);
    process.exit(1);
  }
  const list = raw.exceptions;
  if (!Array.isArray(list)) {
    console.error(`npm-audit-gate: ${EXC_PATH} has no \`exceptions\` array. An unparseable exception file is a FAILURE, never a pass.`);
    process.exit(1);
  }
  const out = new Map();
  const today = new Date().toISOString().slice(0, 10);
  for (const [i, e] of list.entries()) {
    if (!e || typeof e.url !== "string" || !e.url) {
      console.error(`npm-audit-gate: exceptions[${i}] has no \`url\` — every exception must name the advisory it covers.`);
      process.exit(1);
    }
    // A MISSING EXPIRY IS REFUSED. This is the property that stops an exception
    // list from becoming a permanent ignore.
    if (typeof e.expires !== "string" || !/^\d{4}-\d{2}-\d{2}$/.test(e.expires)) {
      console.error(
        `npm-audit-gate: exceptions[${i}] (${e.url}) has no valid \`expires\` (YYYY-MM-DD). ` +
          `An exception without an expiry would be a permanent ignore — refuse it.`,
      );
      process.exit(1);
    }
    if (e.expires < today) {
      console.error(
        `npm-audit-gate: the exception for ${e.url} EXPIRED on ${e.expires} (today is ${today}). ` +
          `Re-check whether a fixed release exists now; if not, extend it deliberately.`,
      );
      process.exit(1);
    }
    out.set(e.url, e);
  }
  return out;
}

/** A non-negative integer read from the report's metadata, or null. */
function countOf(v) {
  return Number.isInteger(v) && v >= 0 ? v : null;
}

/** Print the report's own bytes as the evidence, without a Node stack dump. */
function printNpmReport() {
  console.error("  npm's own report:");
  let plain = "";
  try {
    plain = execFileSync("npm", ["audit", "--audit-level=high"], {
      cwd: WEB_DIR,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "pipe"],
    });
  } catch (e) {
    // `npm audit` exits non-zero WHEN IT FINDS SOMETHING. That is the report,
    // not a crash: `execFileSync` throws on a non-zero exit, so the unguarded
    // call used to print a raw Node stack trace here instead of npm's output
    // (card t_e6041c4f, part 4). Read the report off the thrown error.
    plain = `${e.stdout ?? ""}`;
  }
  if (plain.trim()) {
    console.error(plain);
  } else {
    console.error("  (npm printed no plain report; the JSON report above is authoritative.)");
  }
}

const { report, rc } = runAudit();
const advisories = advisoriesOf(report);
const exceptions = loadExceptions();

const highCritical = highCriticalPackages(report);
const declared = report.metadata?.vulnerabilities ?? {};
const declaredHigh = countOf(declared.high);
const declaredCritical = countOf(declared.critical);
const declaredHighCritical = declaredHigh === null || declaredCritical === null ? null : declaredHigh + declaredCritical;

// Every high/critical package must trace to a named advisory URL. Anything that
// does not is a report this gate cannot judge.
const judgeable = new Map(); // url -> Set(package), restricted to high/critical reachability
const untraceable = [];
for (const name of highCritical.keys()) {
  const urls = advisoryURLsReaching(name, report);
  if (urls.size === 0) {
    untraceable.push(name);
    continue;
  }
  for (const url of urls) {
    if (!judgeable.has(url)) judgeable.set(url, new Set());
    judgeable.get(url).add(name);
  }
}

const unexcepted = [...judgeable.entries()].filter(([url]) => !exceptions.has(url));
const excused = [...judgeable.entries()].filter(([url]) => exceptions.has(url));
const stale = [...exceptions.keys()].filter((url) => !judgeable.has(url));

console.log(
  `npm-audit-gate: ${declared.high ?? 0} high, ${declared.critical ?? 0} critical ` +
    `(npm audit rc=${rc}); ${advisories.size} advisory URL(s), ${excused.length} excepted, ${stale.length} stale; ` +
    `${highCritical.size} high/critical package(s), ${judgeable.size} judgeable advisory URL(s)`,
);

/**
 * The reconciliation (card t_e6041c4f). Returns an array of reasons the report
 * CANNOT be judged; an empty array means the parsed objects and npm's own exit
 * code agree, and the exception verdict below is meaningful.
 *
 * Each rule closes a different way the report shape can drift out from under a
 * verdict derived from `via` objects alone:
 *
 *   R1  npm exited 0 but the parsed set holds high/critical entries — the two
 *       disagree about what the gate judges. (Should be impossible; if it is not,
 *       the report shape has moved and a verdict is not trustworthy.)
 *   R2  npm exited non-zero (there IS a high/critical advisory) but the parsed
 *       high/critical set is EMPTY — the QA repro: `vulnerabilities:{}` with
 *       `metadata.high = 5`. The old gate printed PASS here.
 *   R3  `metadata.vulnerabilities.high|critical` disagrees with the number of
 *       high/critical entries in `vulnerabilities`. Either count alone can be
 *       stripped or stale; when they disagree there is nothing to reconcile the
 *       verdict against. Refusing a malformed metadata too (a non-integer count).
 *   R4  A high/critical package traces to NO advisory URL through its `via`
 *       chain — the "registry proxy stripped the advisory objects" shape. It is
 *       named, because a package this gate cannot name is one an exception cannot
 *       cover.
 */
function reconcile() {
  const reasons = [];
  if (rc === 0 && highCritical.size > 0) {
    reasons.push(
      `npm audit exited 0 (nothing at or above the high threshold) but the report carries ` +
        `${highCritical.size} high/critical package(s) — npm's exit code and its report disagree.`,
    );
  }
  if (rc !== 0 && judgeable.size === 0) {
    reasons.push(
      `npm audit exited ${rc} (there IS a high/critical advisory) but the report names NO judgeable ` +
        `advisory URL — no exception can cover what cannot be named.`,
    );
  }
  if (declaredHighCritical === null) {
    reasons.push(
      `metadata.vulnerabilities.high|critical is not a pair of counts (got high=${JSON.stringify(declared.high)}, ` +
        `critical=${JSON.stringify(declared.critical)}) — the report's summary is malformed.`,
    );
  } else if (declaredHighCritical !== highCritical.size) {
    reasons.push(
      `metadata.vulnerabilities.high+critical = ${declaredHighCritical} but the report carries ` +
        `${highCritical.size} high/critical package(s) — the summary and the detail disagree.`,
    );
  }
  if (untraceable.length > 0) {
    reasons.push(
      `these high/critical package(s) trace to no advisory URL through their \`via\` chain: ` +
        `${untraceable.sort().join(", ")} — the advisory objects are missing from the report.`,
    );
  }
  return reasons;
}

const notJudgeable = reconcile();

for (const [url, pkgs] of excused) {
  // `pkgs` is the ADVISORIES-side package set; the exception is looked up by URL
  // (`excused` is built from the advisories entries, not from the exception list).
  const e = exceptions.get(url);
  console.log(
    `npm-audit-gate: EXCEPTED ${url} [reached via ${[...pkgs].sort().join(", ")}] ` +
      `— declared packages [${(e.packages ?? []).join(", ")}], expires ${e.expires}`,
  );
}
for (const url of stale) {
  // Not a failure: a stale entry is untidy, not unsafe. It is printed loudly
  // because silently keeping it is how an allowlist rots into noise.
  console.log(`npm-audit-gate: STALE — ${url} is no longer reported; remove it from ${EXC_PATH}.`);
}

if (notJudgeable.length > 0) {
  console.error("");
  console.error("  #################################################################");
  console.error("  #  npmaudit: THE AUDIT REPORT COULD NOT BE JUDGED  #");
  console.error("  #################################################################");
  for (const reason of notJudgeable) console.error(`  * ${reason}`);
  console.error("");
  console.error("  This gate fails CLOSED: a report it cannot verify is never a pass.");
  console.error("  The parsed report:");
  console.error(JSON.stringify(report, null, 2).slice(0, 4000));
  console.error("");
  printNpmReport();
  process.exit(1);
}

if (unexcepted.length > 0) {
  console.error("");
  console.error("  ############################################################");
  console.error("  #  npmaudit: HIGH/CRITICAL ADVISORY NOT IN THE EXCEPTIONS  #");
  console.error("  ############################################################");
  for (const [url, pkgs] of unexcepted) {
    console.error(`  ${url}`);
    console.error(`    reached via: ${[...pkgs].sort().join(", ")}`);
  }
  console.error("");
  console.error("  This gate FAILS on a high/critical advisory that is not named in");
  console.error(`  ${EXC_PATH}. Fix it, or add a time-boxed exception WITH the`);
  console.error("  evidence that no fixed release exists. `npm audit fix --force` is NOT");
  console.error("  an answer here: it is a semver-major DOWNGRADE of the eslint toolchain.");
  console.error("");
  printNpmReport();
  process.exit(1);
}

console.log("npm-audit-gate: no unexcepted high/critical advisory — PASS");
process.exit(0);
