// Guard for card t_7f01f01f — a SERVER refusal on the passkey button must not be
// reported as a browser that cannot do passkeys, and the refusal's window must not
// be dropped.
//
// THE DEFECT (reproduced on the built page before this fix; the browser arms live
// in `web/e2e/login-passkey-start-failure.spec.ts`, which reds 4/5 on the pre-fix
// build — 429, bare 429, 5xx and broken-200 all rendered "Passkey cancelled or not
// supported by this browser." while the ceremony control stayed green)
//
//     const st = await fetch("/api/auth/passkeys/login/start", { method: "POST" }).then((r) => r.json());
//
//   One try/catch, whose catch set `login.passkeyUnsupported`. The status was never
//   checked, so `r.json()` on the per-IP limiter's PLAIN-TEXT 429 threw into that
//   catch — and so did every 5xx and proxy error page. The visitor was told their
//   browser could not do passkeys while the browser was fine, and `Retry-After`,
//   which the throttle had just sent, was discarded with the same throw.
//
// WHAT THIS FILE PINS, AND WHY EACH ASSERTION IS THE THING THAT BREAKS
//   1. The start call's statement does not read the body. A `.then((r) => r.json())`
//      chain is the pre-fix shape and is what makes a non-JSON refusal throw.
//   2. The refusal branch reads `startRes.ok` and routes a 429 through `rateLimitErr`
//      with the response's own `retry-after` (card t_e50a9680's mapping, reused
//      rather than re-litigated), falling back to the page's own failure line.
//   3. Only the CEREMONY may blame the browser: the try whose body holds
//      `navigator.credentials.get` may answer `login.passkeyUnsupported`, and it
//      must sit INSIDE another try whose catch answers `login.genericError` — that
//      nesting is exactly what the fix added, and reverting the page to one
//      try/catch turns this RED.
//   4. `login.passkeyUnsupported` appears on exactly the two browser-side paths
//      (the rejected ceremony and a null credential). A third site means a server
//      refusal got the browser sentence back.
//   5. The two sentences are distinct and fully translated, so the choice is visible
//      on screen and no locale renders the bare key.
//
// A source guard proves the SHAPE, not the behaviour — the browser arms are the
// behavioural half, and this file says so rather than pretending otherwise.
//
// Run with the repo's Node test runner:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import ts from "typescript";

import { DICT, LANGS } from "../components/i18nDict.ts";

const dict = DICT as Record<string, Record<string, unknown>>;
const LOGIN_PAGE = fileURLToPath(new URL("../app/login/page.tsx", import.meta.url));
const SOURCE = readFileSync(LOGIN_PAGE, "utf8");

/** The `passkeyLogin` declaration of the login page. */
function passkeyLogin(): ts.FunctionDeclaration {
  const sf = ts.createSourceFile(LOGIN_PAGE, SOURCE, ts.ScriptTarget.ESNext, true, ts.ScriptKind.TSX);
  const found: ts.FunctionDeclaration[] = [];
  const visit = (node: ts.Node) => {
    if (ts.isFunctionDeclaration(node) && node.name?.text === "passkeyLogin") found.push(node);
    node.forEachChild(visit);
  };
  visit(sf);
  assert.equal(found.length, 1, `expected exactly one function declaration named passkeyLogin, found ${found.length}`);
  return found[0];
}

const fnText = passkeyLogin().getText();

test("the start call does not parse the body of an unchecked response", () => {
  const startAt = fnText.indexOf('"/api/auth/passkeys/login/start"');
  assert.ok(startAt > 0, "passkeyLogin no longer calls /api/auth/passkeys/login/start — this guard must be re-pointed");

  // The statement that issues the call, up to its first semicolon. The pre-fix
  // shape was `fetch(...).then((r) => r.json())`, i.e. the parse lived IN this
  // statement and a plain-text 429 threw out of it.
  const stmtEnd = fnText.indexOf(";", startAt);
  assert.ok(stmtEnd > startAt, "could not find the end of the start-call statement");
  const stmt = fnText.slice(startAt, stmtEnd);
  assert.doesNotMatch(stmt, /\.json\(/, `the start call parses the body before any status check: ${stmt}`);
  assert.doesNotMatch(stmt, /\.then\(/, `the start call still chains a handler onto the fetch: ${stmt}`);
});

test("a refusal is routed through rateLimitErr with the server's own Retry-After", () => {
  // 1. The status is checked at all. Without this every arm below is unreachable.
  assert.match(fnText, /if\s*\(\s*!\s*startRes\.ok\s*\)/, "passkeyLogin does not test the start response's status");
  // 2. The throttle's window comes from the response HEADER (the per-IP limiter
  //    writes `Retry-After: 30`, middleware/ratelimit.go), through the shared
  //    mapping — not a second hand-rolled sentence.
  assert.match(
    fnText,
    /rateLimitErr\(\s*startRes\.status\s*,\s*startRes\.headers\.get\(\s*"retry-after"\s*\)/,
    "the refusal does not hand the response's retry-after to rateLimitErr",
  );
  // 3. Everything that is not a throttle lands on the page's own failure line.
  assert.match(fnText, /t\("login\.genericError"\)/, "the refusal branch has no honest fallback sentence");
});

test("only the ceremony may blame the browser, and it sits inside a server-side catch", () => {
  const sf = ts.createSourceFile(LOGIN_PAGE, SOURCE, ts.ScriptTarget.ESNext, true, ts.ScriptKind.TSX);
  const fn = passkeyLogin();

  // Every try statement in passkeyLogin, with the text of its try and catch blocks.
  const tries: { node: ts.TryStatement; body: string; caught: string; depth: number }[] = [];
  const walk = (node: ts.Node, depth: number) => {
    if (ts.isTryStatement(node)) {
      tries.push({
        node,
        body: node.tryBlock.getText(sf),
        caught: node.catchClause ? node.catchClause.block.getText(sf) : "",
        depth,
      });
    }
    node.forEachChild((c) => walk(c, ts.isTryStatement(node) ? depth + 1 : depth));
  };
  walk(fn, 0);

  const ceremony = tries.filter((t) => t.body.includes("navigator.credentials.get"));
  assert.ok(ceremony.length >= 1, "passkeyLogin no longer runs the credential ceremony inside a try");
  // The INNERMOST try holding the call is the ceremony's own; any other try whose
  // body contains it is an ANCESTOR (the enclosing, server-side one).
  const inner = ceremony.reduce((a, b) => (b.depth > a.depth ? b : a));
  assert.match(
    inner.caught,
    /t\("login\.passkeyUnsupported"\)/,
    "a rejected ceremony stopped answering login.passkeyUnsupported",
  );

  // THE BITE: on the pre-fix page the credentials.get call sat in the page's ONLY
  // try, so there is no enclosing try and this assertion fails.
  const around = ceremony.filter((t) => t !== inner);
  assert.equal(around.length, 1, `expected the ceremony to sit inside one enclosing try, found ${around.length}`);
  assert.match(
    around[0].caught,
    /t\("login\.genericError"\)/,
    "the enclosing (server-side) catch does not answer with the page's own failure line",
  );
  assert.doesNotMatch(
    around[0].caught,
    /login\.passkeyUnsupported/,
    "the enclosing catch still blames the browser, so every server refusal reads as one",
  );
});

test("login.passkeyUnsupported is reached on exactly the two browser-side paths", () => {
  const occurrences = fnText.split('t("login.passkeyUnsupported")').length - 1;
  assert.equal(
    occurrences,
    2,
    `expected the sentence at the rejected ceremony and the null credential only, found ${occurrences} sites — ` +
      "a third site is a server refusal wearing the browser's sentence",
  );
  // Both name the ceremony's own outcome, so both must sit AFTER the get() call.
  const getAt = fnText.indexOf("navigator.credentials.get");
  assert.ok(getAt > 0, "passkeyLogin no longer runs the credential ceremony");
  for (const at of [fnText.indexOf('t("login.passkeyUnsupported")'), fnText.lastIndexOf('t("login.passkeyUnsupported")')]) {
    assert.ok(at > getAt, "a browser-blame sentence is reached before the ceremony even starts");
  }
});

test("the two sentences are distinct and translated in all five locales", () => {
  const unsupported = dict["login.passkeyUnsupported"];
  const generic = dict["login.genericError"];
  assert.ok(unsupported && generic, "a sentence this guard names is missing from DICT");

  for (const lang of LANGS) {
    const a = unsupported[lang];
    const b = generic[lang];
    assert.equal(typeof a, "string", `login.passkeyUnsupported has no ${lang} translation`);
    assert.equal(typeof b, "string", `login.genericError has no ${lang} translation`);
    assert.ok((a as string).trim() !== "" && (b as string).trim() !== "", `an empty ${lang} sentence would render nothing`);
    assert.notEqual(
      a,
      b,
      `lang=${lang}: the server refusal and the browser refusal read the same, so this fix is invisible`,
    );
  }
});
