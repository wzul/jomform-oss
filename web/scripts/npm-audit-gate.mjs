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
//   But the repo now depends on `eslint-config-next`, which pulls
//   `braces@3.0.3` — and braces has an advisory with NO FIXED RELEASE published
//   (`npm view braces@3.0.4` is a 404; dist-tags.latest is still 3.0.3). The only
//   thing npm offers is a DOWNGRADE of eslint-config-next to 14.2.35, which would
//   remove the Next 16 flat config the `lint` gate exists to run. So the choice is
//   not "fix it or leave it red" — it is "name it precisely and time-box it" or
//   "do not add the linter at all". This file is the first option, and it is built
//   so it cannot become the second kind of thing (a blanket ignore).
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
//
// USAGE
//   node scripts/npm-audit-gate.mjs            # from web/
//   exit 0 = no unexcepted high/critical advisory; 1 = there is one (or the
//   exception file itself is malformed/expired).

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

const { report, rc } = runAudit();
const advisories = advisoriesOf(report);
const exceptions = loadExceptions();

const unexcepted = [...advisories.entries()].filter(([url]) => !exceptions.has(url));
const excused = [...advisories.entries()].filter(([url]) => exceptions.has(url));
const stale = [...exceptions.keys()].filter((url) => !advisories.has(url));

const counts = report.metadata?.vulnerabilities ?? {};
console.log(
  `npm-audit-gate: ${counts.high ?? 0} high, ${counts.critical ?? 0} critical ` +
    `(npm audit rc=${rc}); ${advisories.size} advisory URL(s), ${excused.length} excepted, ${stale.length} stale`,
);

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
  console.error("  npm's own report:");
  console.error(
    execFileSync("npm", ["audit", "--audit-level=high"], { cwd: WEB_DIR, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }) ||
      "",
  );
  process.exit(1);
}

console.log("npm-audit-gate: no unexcepted high/critical advisory — PASS");
process.exit(0);
