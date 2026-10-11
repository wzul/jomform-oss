// Card t_5e39dc9f — the ONE `?next=` rule, now shared by the login page (which
// forwards it with the magic-link request) and the magic-link page (which
// honours it after the emailed link signs the visitor in).
//
// WHY THESE ARMS EXIST. The rule used to be unreachable for the passwordless
// flow: the login page never sent `next`, so the server had nothing to put in
// the email, and the magic-link page hard-coded `resolvePostLogin("")`. Both
// halves are wired now, and this file pins the DECISION both halves depend on:
// which values are honoured, and — just as important — which fall back.
//
// A dropped value must never become an open redirect. `evil.example` (and a
// `www.` alias the browser could not be on) is the shape that would make it one.
//
// Run with the repo's Node test runner: npm run test:unit
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { NEXT_MAX_LEN, safeNextTarget } from "./nextTarget.ts";

const SELF = "https://jomform.com";
const OAUTH = "https://mcp.jomform.com";

test("a same-origin absolute target is honoured, path and query intact", () => {
  assert.equal(
    safeNextTarget("https://jomform.com/profile", SELF, OAUTH),
    "https://jomform.com/profile",
  );
  assert.equal(
    safeNextTarget("https://jomform.com/products/123/edit?tab=images", SELF, OAUTH),
    "https://jomform.com/products/123/edit?tab=images",
  );
});

test("the OAuth authorize origin is honoured (issue #140)", () => {
  const authorize = "https://mcp.jomform.com/oauth/authorize?client_id=abc&state=xyz";
  assert.equal(safeNextTarget(authorize, SELF, OAUTH), authorize);
});

test("the app's other spelling of its own host is honoured — both directions", () => {
  // Prod measured 2026-10-11: www.jomform.com/profile hands /login
  // next=https://www.jomform.com/profile, while the emailed link is always built
  // on the API base URL (the apex). Without this alias that visitor is not
  // returned to the page they asked for.
  assert.equal(
    safeNextTarget("https://www.jomform.com/profile", SELF, OAUTH),
    "https://www.jomform.com/profile",
  );
  assert.equal(
    safeNextTarget("https://jomform.com/profile", "https://www.jomform.com", OAUTH),
    "https://jomform.com/profile",
  );
});

test("a FOREIGN origin is dropped — the alias is the browser's, not the value's", () => {
  // The www alias is derived from the browser's own origin, so a www spelling of
  // somebody else's host is exactly as foreign as the bare one.
  for (const raw of [
    "https://evil.example/profile",
    "https://www.evil.example/profile",
    "https://evil.jomform.com.evil.example/profile",
    "https://notjomform.com/profile",
  ]) {
    assert.equal(safeNextTarget(raw, SELF, OAUTH), "", `${raw} must not be honoured`);
  }
});

test("relative, protocol-relative, schemeless and non-http(s) values are dropped", () => {
  for (const raw of [
    "/profile",
    "//evil.example/profile",
    "evil.example/profile",
    "javascript:alert(1)",
    "data:text/html,<script>1</script>",
    "ftp://jomform.com/x",
  ]) {
    assert.equal(safeNextTarget(raw, SELF, OAUTH), "", `${raw} must not be honoured`);
  }
});

test("absent and empty values are dropped (the caller falls back)", () => {
  assert.equal(safeNextTarget("", SELF, OAUTH), "");
  assert.equal(safeNextTarget(null, SELF, OAUTH), "");
  assert.equal(safeNextTarget(undefined, SELF, OAUTH), "");
});

test("an oversized value is dropped, and the cap is the one the SSR gate uses", () => {
  const target = "https://jomform.com/settings?";
  const atCap = target + "x".repeat(NEXT_MAX_LEN - target.length);
  assert.equal(atCap.length, NEXT_MAX_LEN);
  assert.equal(safeNextTarget(atCap, SELF, OAUTH), atCap, "a value AT the cap is still honoured");

  const overCap = atCap + "x";
  assert.ok(overCap.length > NEXT_MAX_LEN);
  assert.equal(safeNextTarget(overCap, SELF, OAUTH), "");
});

test("no browser (SSR) means no value can be validated against an origin", () => {
  // browserNextTarget() returns "" without a window; the pure rule with an empty
  // self origin must not accidentally honour anything but the OAuth base.
  assert.equal(safeNextTarget("https://jomform.com/profile", "", OAUTH), "");
});

// ── the CAP lives in three languages, and drift between them is silent ───────
//
// The same 700 bounds a `?next=` value at every hop: the SSR gate that builds it
// (lib/authedGuard.ts), this rule that validates it in the browser, and the Go
// handler that decides whether to embed it in an email
// (api/internal/httpapi/auth_recovery.go). A value refused at one hop and
// accepted at the next is a return leg that silently does nothing — the exact
// shape of the defect this card fixed — so the three are compared rather than
// trusted. The idiom is this repo's cross-language source guard
// (chatHistoryCap.test.ts, homeServerContent.test.ts): read the declaration, do
// not restate the number.
const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

/** `export const NEXT_MAX_LEN = <n>;` in a web module, as a number. */
function webCap(src: string): number {
  const m = src.match(/export const NEXT_MAX_LEN = (\d+);/);
  assert.ok(m, "NEXT_MAX_LEN is not declared as an integer literal — retarget this guard at whatever replaced it rather than comparing against nothing");
  return Number(m[1]);
}

/** `const nextMaxLen = <n>` in the Go handler, as a number. */
function goCap(src: string): number {
  const m = src.match(/^\s*const\s+nextMaxLen\s*=\s*(\d+)\s*$/m);
  assert.ok(m, "nextMaxLen is not declared as an integer literal in api/internal/httpapi/auth_recovery.go — retarget this guard rather than comparing against nothing");
  return Number(m[1]);
}

/** Everything wrong with one (browser rule, SSR gate, Go handler) triple. */
function capProblems(targetSrc: string, guardSrc: string, goSrc: string): string[] {
  const out: string[] = [];
  const here = webCap(targetSrc);
  for (const [where, n] of [
    ["the SSR gate's own cap (web/src/lib/authedGuard.ts)", webCap(guardSrc)],
    ["the server's cap (nextMaxLen in api/internal/httpapi/auth_recovery.go)", goCap(goSrc)],
  ] as const) {
    if (n !== here) {
      out.push(`${where} is ${n} but the browser rule is ${here}: a value one hop accepts would be refused at the other`);
    }
  }
  return out;
}

test("the cap on a ?next= value is one number at every hop", () => {
  const targetSrc = read("./nextTarget.ts");
  const guardSrc = read("./authedGuard.ts");
  const goSrc = read("../../../api/internal/httpapi/auth_recovery.go");
  // Not vacuous: all three sources are the files this guard means to read.
  assert.ok(targetSrc.includes("safeNextTarget"), "nextTarget.ts is not the shared rule");
  assert.ok(guardSrc.includes("loginRedirectPath"), "authedGuard.ts is not the SSR gate");
  assert.ok(goSrc.includes("safeMagicLinkNext"), "auth_recovery.go is not the magic-link handler");
  assert.equal(webCap(targetSrc), NEXT_MAX_LEN);

  const problems = capProblems(targetSrc, guardSrc, goSrc);
  assert.deepEqual(problems, [], `the ?next= cap is not one number:\n  - ${problems.join("\n  - ")}`);
});

test("the cap guard bites: a drifted cap at either end is RED", () => {
  const targetSrc = read("./nextTarget.ts");
  const guardSrc = read("./authedGuard.ts");
  const goSrc = read("../../../api/internal/httpapi/auth_recovery.go");

  const mutations = [
    { what: "the SSR gate's cap drifts", guard: guardSrc.replace(/NEXT_MAX_LEN = \d+;/, "NEXT_MAX_LEN = 900;") },
    { what: "the server's cap drifts", go: goSrc.replace(/const nextMaxLen = \d+/, "const nextMaxLen = 900") },
  ];
  for (const m of mutations) {
    const g = m.guard ?? guardSrc;
    const go = m.go ?? goSrc;
    assert.notEqual(`${g}\u0000${go}`, `${guardSrc}\u0000${goSrc}`, `the mutation "${m.what}" changed nothing — this proof would be vacuous`);
    const problems = capProblems(targetSrc, g, go);
    assert.ok(
      problems.some((s) => /is 900 but the browser rule is \d+/.test(s)),
      `"${m.what}" is NOT caught: ${problems.length ? problems.join("; ") : "(nothing)"}`,
    );
  }
});
