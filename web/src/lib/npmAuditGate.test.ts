// Guard: `web/scripts/npm-audit-gate.mjs` (the `npmaudit` gate's command) must
// FAIL CLOSED on an audit report it cannot judge, and must still pass the one
// real exception it carries (card t_e6041c4f).
//
// THE DEFECT (measured by QA on PR 1028's head `b2d3faf9`, then re-reproduced here)
//   The gate had exactly ONE failure condition: an advisory URL found in the
//   report's `via` OBJECTS that is not named in `npm-audit-exceptions.json`. So a
//   report that parses but whose `via` entries carry no advisory object — only the
//   package-NAME propagation chain — produced ZERO advisories and the gate printed
//
//     npm-audit-gate: no unexcepted high/critical advisory — PASS
//
//   while `npm audit` had exited 1 with 5 high. `npm audit`'s exit code and
//   `metadata.vulnerabilities.high|critical` were read for the PRINTED SUMMARY
//   only — never for the verdict.
//
//   That is a fail-open in the gate that sits MID-LIST in scripts/preflight.sh,
//   which is the worst place for one: a silent pass here hides every gate after
//   it (the exact reason PR 1025 existed for `sharp`).
//
// WHAT THIS FILE PINS, AND WHY IT DRIVES THE REAL SCRIPT
//   Each case runs the SHIPPED `web/scripts/npm-audit-gate.mjs` under a stubbed
//   `npm` that emits a chosen report and exit code. The bytes that run are read
//   from the live file and written into a throwaway sandbox (so the gate resolves
//   its OWN `npm-audit-exceptions.json` fixture, and the repo's real one is never
//   mutated) — and the sandbox script is asserted byte-identical to the live one,
//   which is what binds this test to the shipped behaviour rather than to a copy
//   that could drift.
//
//   The stub ignores its cwd, so the sandbox needs no package tree and the test
//   needs no network and no installed `web/node_modules`. That is deliberate:
//   `npm run test:unit` runs inside the `web` gate (`npm run build` runs it
//   first), and a unit test that reached the npm registry would add a network
//   dependency to a gate that has none.
//
//   Covered: the QA fail-open shape (empty `vulnerabilities`, `metadata.high=5`,
//   rc=1) must be RED; advisory objects stripped to names must be RED; a
//   `metadata` count that disagrees with the detail must be RED; npm exiting 0
//   while the report carries a high entry must be RED; the exception still works
//   (GREEN) and a renamed URL still fails (RED) with npm's report rather than a
//   Node stack; a missing/expired `expires` is still refused; a moderate-only
//   report still PASSes (moderate is not this gate's business).
//
// Run with:
//   npm run test:unit

import { test } from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { chmodSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url)); // <repo>/web/src/lib
const WEB_DIR = resolve(HERE, "..", ".."); //             <repo>/web
const GATE = join(WEB_DIR, "scripts", "npm-audit-gate.mjs");

const ADVISORY_URL = "https://github.com/advisories/GHSA-vfj7-8cjw-p6xm";
const OTHER_URL = "https://github.com/advisories/GHSA-moved-away-0000";

/** A violations entry whose `via` holds the advisory OBJECT. */
function withObject(severity = "high", url = ADVISORY_URL): Record<string, unknown> {
  return {
    name: "braces",
    severity,
    via: [{ source: 1, name: "braces", title: "t", url, severity, range: "<=3.0.3" }],
  };
}

/** A violations entry whose `via` holds only the propagation-chain NAMES. */
function namesOnly(severity = "high", via: string[] = ["micromatch"]): Record<string, unknown> {
  return { name: "braces", severity, via };
}

/** The full report, assembled from a `vulnerabilities` map and a metadata map. */
function report(
  vulnerabilities: Record<string, unknown>,
  metadata: Record<string, unknown>,
): string {
  return JSON.stringify({ auditReportVersion: 2, vulnerabilities, metadata: { vulnerabilities: metadata } });
}

/** One fixture exception entry — deliberately loosely typed: one case omits `expires`. */
type Exception = Record<string, unknown>;

const FIXTURE_EXCEPTION: Exception[] = [
  {
    url: ADVISORY_URL,
    packages: ["braces@3.0.3"],
    severity: "high",
    reason: ["fixture"],
    expires: "2099-01-01",
  },
];

/** The gate's exit code and output. */
type GateRun = { rc: number | null; out: string; err: string };

/**
 * Run the SHIPPED gate against a stub npm.
 *
 * @param reportJson   the JSON the stub prints for `npm audit`
 * @param rc           the exit code the stub uses
 * @param exceptions   the fixture `exceptions` array (default: the working one)
 */
function runGate(reportJson: string, rc: number, exceptions: Exception[] = FIXTURE_EXCEPTION): GateRun {
  const sandbox = mkdtempSync(join(tmpdir(), "npm-audit-gate-"));
  try {
    // The gate reads its exception list as `<scriptDir>/../npm-audit-exceptions.json`,
    // so a sandbox script + a sandbox fixture is a self-contained tree.
    mkdirSync(join(sandbox, "scripts"));
    mkdirSync(join(sandbox, "bin"));
    // THE LIVE BYTES. Read the shipped gate rather than a fixture of it: a change
    // to the script under test is exactly what this test must exercise.
    const live = readFileSync(GATE, "utf8");
    writeFileSync(join(sandbox, "scripts", "npm-audit-gate.mjs"), live);
    writeFileSync(
      join(sandbox, "npm-audit-exceptions.json"),
      JSON.stringify({ exceptions }, null, 2),
    );

    const payloadPath = join(sandbox, "payload.json");
    writeFileSync(payloadPath, reportJson);
    const stub = join(sandbox, "bin", "npm");
    // POSIX `sh`, not bash: the `web` gate runs this suite inside the shipping
    // image (`web/Dockerfile` builds FROM node:24-alpine, which ships busybox sh
    // and NO bash), so a bash stub makes the whole gate red there with
    // `env: can't execute 'bash': No such file or directory` — measured on the
    // real `webimage` docker build. Nothing below needs a bash feature.
    writeFileSync(
      stub,
      [
        "#!/bin/sh",
        'printf "%s\\n" "$NPM_STUB_JSON"',
        'exit "${NPM_STUB_RC:-1}"',
        "",
      ].join("\n"),
    );
    chmodSync(stub, 0o755);

    const run = spawnSync(
      process.execPath,
      ["--disable-warning=MODULE_TYPELESS_PACKAGE_JSON", join(sandbox, "scripts", "npm-audit-gate.mjs")],
      {
        cwd: sandbox,
        encoding: "utf8",
        env: {
          ...process.env,
          // The stub is a fixed payload, so the JSON path is passed by value.
          NPM_STUB_JSON: readFileSync(payloadPath, "utf8").trim(),
          NPM_STUB_RC: String(rc),
          PATH: `${join(sandbox, "bin")}:${process.env.PATH ?? ""}`,
        },
      },
    );
    return { rc: run.status, out: `${run.stdout ?? ""}`, err: `${run.stderr ?? ""}` };
  } finally {
    rmSync(sandbox, { recursive: true, force: true });
  }
}

/** The verdict lines the gate prints. A PASS must never accompany a red. */
function assertVerdict(res: GateRun, wantRc: number, needle: string, label: string): void {
  assert.notEqual(res.rc, null, `${label}: the gate did not run to an exit (signal?): ${res.err}`);
  assert.equal(
    res.rc,
    wantRc,
    `${label}: expected exit ${wantRc}, got ${res.rc}\n--- stdout ---\n${res.out}\n--- stderr ---\n${res.err}`,
  );
  const all = `${res.out}${res.err}`;
  assert.ok(
    all.includes(needle),
    `${label}: output must contain ${JSON.stringify(needle)}\n--- output ---\n${all}`,
  );
  if (wantRc !== 0) {
    assert.ok(
      !res.out.includes("— PASS"),
      `${label}: a failing gate must not print the PASS line\n--- stdout ---\n${res.out}`,
    );
  }
}

test("the fail-open shape must be RED, not PASS (the QA repro: no advisory objects, 5 high, rc=1)", () => {
  // `vulnerabilities` empty while the summary says 5 high and npm exited 1 —
  // byte for byte the report QA stubbed to make the pre-fix gate exit 0.
  const res = runGate(
    report({}, { info: 0, low: 0, moderate: 0, high: 5, critical: 0, total: 5 }),
    1,
  );
  assertVerdict(res, 1, "COULD NOT BE JUDGED", "fail-open repro");
  assert.ok(
    res.err.includes("names NO judgeable advisory URL"),
    `the refusal must say the report names nothing an exception could cover\n${res.err}`,
  );
});

test("a report whose via entries are NAMES only (no advisory object) must be RED", () => {
  // The "registry proxy stripped the advisory objects" shape — the file's own
  // comment calls the strings "the propagation chain".
  const res = runGate(
    report(
      { braces: namesOnly(), micromatch: namesOnly("high", []) },
      { info: 0, low: 0, moderate: 0, high: 2, critical: 0, total: 2 },
    ),
    1,
  );
  assertVerdict(res, 1, "COULD NOT BE JUDGED", "stripped advisory objects");
  assert.ok(
    res.err.includes("trace to no advisory URL"),
    `the refusal must name the untraceable packages\n${res.err}`,
  );
});

test("a metadata high/critical count that disagrees with the detail must be RED", () => {
  // The detail IS judgeable here; the summary lies about it. Either side alone
  // can drift, so a disagreement is not a verdict this gate may hand out.
  const res = runGate(
    report({ braces: withObject("high") }, { info: 0, low: 0, moderate: 0, high: 0, critical: 0, total: 0 }),
    1,
  );
  assertVerdict(res, 1, "COULD NOT BE JUDGED", "metadata/ detail disagreement");
  assert.ok(
    res.err.includes("the summary and the detail disagree"),
    `the refusal must state which two numbers disagree\n${res.err}`,
  );
});

test("npm exiting 0 while the report carries a high entry must be RED", () => {
  const res = runGate(
    report({ braces: withObject("high") }, { info: 0, low: 0, moderate: 0, high: 1, critical: 0, total: 1 }),
    0,
  );
  assertVerdict(res, 1, "COULD NOT BE JUDGED", "rc 0 with a high entry");
  assert.ok(
    res.err.includes("exit code and its report disagree"),
    `the refusal must name the exit-code disagreement\n${res.err}`,
  );
});

test("the working exception still passes, and an unexcepted advisory still fails", () => {
  const clean = report({ braces: withObject("high") }, { info: 0, low: 0, moderate: 0, high: 1, critical: 0, total: 1 });
  const green = runGate(clean, 1);
  assertVerdict(green, 0, "no unexcepted high/critical advisory — PASS", "excepted advisory");
  assert.ok(
    green.out.includes(`EXCEPTED ${ADVISORY_URL}`),
    `the excepted advisory must be named in the output\n${green.out}`,
  );

  // The SAME report against an exception for a different URL: RED, and the
  // failure prints npm's report rather than a Node stack (card part 4).
  const red = runGate(clean, 1, [{ ...FIXTURE_EXCEPTION[0], url: OTHER_URL }]);
  assertVerdict(red, 1, "HIGH/CRITICAL ADVISORY NOT IN THE EXCEPTIONS", "unexcepted advisory");
  assert.ok(
    red.err.includes(ADVISORY_URL),
    `the failing gate must print the advisory URL itself\n${red.err}`,
  );
  assert.ok(
    !/at .*node:internal|Error: Command failed/.test(red.err),
    `the RED path must print npm's report, not a raw Node stack\n${red.err}`,
  );
});

test("an exception with no expiry, or an expired one, is still REFUSED", () => {
  const clean = report({ braces: withObject("high") }, { info: 0, low: 0, moderate: 0, high: 1, critical: 0, total: 1 });

  const noExpiry = runGate(clean, 1, [{ url: ADVISORY_URL, packages: ["braces@3.0.3"] }]);
  assertVerdict(noExpiry, 1, "no valid `expires`", "missing expiry");

  const expired = runGate(clean, 1, [{ ...FIXTURE_EXCEPTION[0], expires: "2020-01-01" }]);
  assertVerdict(expired, 1, "EXPIRED", "expired exception");
});

test("a high/critical package tracing to no advisory URL is RED even when ANOTHER advisory is judgeable", () => {
  // Discrimination for the untraceable rule: a report where one advisory IS
  // judgeable (so the "nothing judgeable" refusal cannot fire) and one package
  // reaches no URL. Without this rule the gate would PASS, because the judgeable
  // URL is covered by an exception — the untraceable package would be invisible.
  const res = runGate(
    report(
      { braces: withObject("high"), ghost: namesOnly("high", []) },
      { info: 0, low: 0, moderate: 0, high: 2, critical: 0, total: 2 },
    ),
    1,
  );
  assertVerdict(res, 1, "COULD NOT BE JUDGED", "untraceable beside a judgeable advisory");
  assert.ok(
    res.err.includes("ghost"),
    `the refusal must name the untraceable package\n${res.err}`,
  );
});

test("a TRANSITIVE chain (a package reached only by NAME) still resolves, so the exception covers it", () => {
  // This is the real tree's shape: `eslint-config-next` names `@next/eslint-plugin-next`,
  // which names `fast-glob`, which names `micromatch`, which names `braces` — and only
  // `braces` carries the advisory OBJECT. The exception is keyed by that URL, so the
  // VERDICT must see every package in the chain as reaching it; a one-level read would
  // call the outer packages untraceable and refuse a tree the exception is meant to
  // cover. Without following the chain this is a false RED on the real tree.
  const res = runGate(
    report(
      {
        "eslint-config-next": { name: "eslint-config-next", severity: "high", via: ["@next/eslint-plugin-next"] },
        "@next/eslint-plugin-next": { name: "@next/eslint-plugin-next", severity: "high", via: ["fast-glob"] },
        "fast-glob": { name: "fast-glob", severity: "high", via: ["micromatch"] },
        micromatch: { name: "micromatch", severity: "high", via: ["braces"] },
        braces: withObject("high"),
      },
      { info: 0, low: 0, moderate: 0, high: 5, critical: 0, total: 5 },
    ),
    1,
  );
  assertVerdict(res, 0, "no unexcepted high/critical advisory — PASS", "transitive chain");
  assert.ok(
    res.out.includes("eslint-config-next") && res.out.includes("braces"),
    `the excepted advisory must be reported as reached through the WHOLE chain\n${res.out}`,
  );
});

test("a genuinely clean report (rc=0, nothing found) still PASSes", () => {
  // The control against over-firing: with no advisories at all the reconciliation
  // must stay silent, so a clean tree is not refused into a false red.
  const res = runGate(
    report({}, { info: 0, low: 0, moderate: 0, high: 0, critical: 0, total: 0 }),
    0,
  );
  assertVerdict(res, 0, "no unexcepted high/critical advisory — PASS", "clean report");
});

test("a moderate-only report PASSes — moderate is not this gate's business", () => {
  const res = runGate(
    report({ braces: withObject("moderate", OTHER_URL) }, { info: 0, low: 0, moderate: 1, high: 0, critical: 0, total: 1 }),
    0,
  );
  assertVerdict(res, 0, "no unexcepted high/critical advisory — PASS", "moderate-only");
});
