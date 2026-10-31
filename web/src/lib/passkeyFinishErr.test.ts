// Guard for card t_3c513380 — a THROTTLED or failed passkey FINISH on /login must
// be reported by its STATUS, not as "Invalid passkey."
//
// THE DEFECT (reproduced on the built page before this fix; the browser arms live
// in `web/e2e/login-passkey-finish-failure.spec.ts`, which reds on the pre-fix
// build with "Invalid passkey." for a plain-text 429 that carried
// `Retry-After: 30`)
//
//     const fin = await fetch("/api/auth/passkeys/login/finish", { … });
//     if (fin.ok) { window.location.href = await resolvePostLogin(next); return; }
//     setError(apiErr(await fin.json().catch(() => ({})), t("login.passkeyInvalid"), t));
//
//   `fin.status` was never read. The finish call sits behind the SAME per-IP
//   limiter as the start call (`api/internal/httpapi/rest.go`,
//   `POST /v1/auth/passkeys/login/finish`), and `middleware/ratelimit.go` writes a
//   429 as `http.Error` PLAIN TEXT plus `Retry-After`. So `fin.json()` threw,
//   `.catch(() => ({}))` swallowed it, and `apiErr({}, …)` rendered
//   `login.passkeyInvalid`: the visitor was told their passkey was WRONG when the
//   server had only refused the call, and the throttle's window went with it. A
//   5xx or a proxy error page had the same path (any non-JSON body throws).
//
// WHAT THIS FILE PINS, AND WHY EACH ASSERTION IS THE THING THAT BREAKS
//   1. The finish call's statement does not parse the body. A `.json()`/`.then()`
//      inside it is the pre-fix shape and is what makes a non-JSON refusal throw.
//   2. `fin.status` is captured BEFORE the first body parse after the call, and
//      every sentence below is chosen from that captured status.
//   3. A 429 goes through `rateLimitErr` with the response's own `retry-after`
//      (card t_e50a9680's mapping, reused rather than re-litigated), falling back
//      to the page's own failure line.
//   4. A 400/401 keeps `login.passkeyInvalid` — the two statuses with which
//      `passkeyLoginFinish` really rejects an ASSERTION — and the branch that
//      reaches it is a CLOSED set: no `>=`, no `>= 500`, so a 5xx cannot inherit
//      the "your passkey is invalid" sentence again.
//   5. Every other non-2xx lands on `login.genericError`, and `passkeyInvalid`
//      appears exactly once in the finish region — a second site is a server
//      refusal wearing the passkey's sentence.
//   6. The three sentences are distinct and fully translated, so the per-status
//      choice is visible on screen and no locale renders a bare key.
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

/**
 * `passkeyLogin` from the FINISH call on, so nothing the start call says can
 * satisfy an assertion about the finish call (the two call sites answer the same
 * limiter and it would be easy to pin the wrong one).
 */
const FINISH_AT = fnText.indexOf('"/api/auth/passkeys/login/finish"');
assert.ok(FINISH_AT > 0, "passkeyLogin no longer calls /api/auth/passkeys/login/finish — this guard must be re-pointed");
const finishRegion = fnText.slice(FINISH_AT);

test("the finish call does not parse the body of an unchecked response", () => {
  // The statement that issues the call, up to its first semicolon: the pre-fix
  // shape put the parse in the NEXT statement but behind no status check at all,
  // and any `.json()` chained onto this fetch would re-create the throw.
  const stmtEnd = finishRegion.indexOf(";");
  assert.ok(stmtEnd > 0, "could not find the end of the finish-call statement");
  const stmt = finishRegion.slice(0, stmtEnd);
  assert.doesNotMatch(stmt, /\.json\(/, `the finish call parses the body before any status check: ${stmt}`);
  assert.doesNotMatch(stmt, /\.then\(/, `the finish call chains a handler onto the fetch: ${stmt}`);
});

test("the status is read BEFORE the body and decides every sentence", () => {
  // THE BITE: the pre-fix page has no `fin.status` read at all, and the first
  // body parse sat in the `setError(apiErr(await fin.json()…))` statement.
  //
  // Read from the AST, not from the text: both call sites carry long comments
  // that quote `fin.json()`, and an index search would be measuring prose.
  const fn = passkeyLogin();
  const sf = fn.getSourceFile();
  let statusDecl: ts.VariableDeclaration | undefined;
  const jsonCalls: ts.CallExpression[] = [];
  const walk = (node: ts.Node) => {
    if (
      ts.isVariableDeclaration(node) &&
      ts.isIdentifier(node.name) &&
      node.name.text === "finStatus" &&
      node.initializer
    ) {
      statusDecl = node;
    }
    // `fin.json(...)` — the body parse of the finish response.
    if (
      ts.isCallExpression(node) &&
      ts.isPropertyAccessExpression(node.expression) &&
      node.expression.name.text === "json" &&
      ts.isIdentifier(node.expression.expression) &&
      node.expression.expression.text === "fin"
    ) {
      jsonCalls.push(node);
    }
    node.forEachChild(walk);
  };
  walk(fn);

  assert.ok(statusDecl, "passkeyLogin does not capture the finish response's status");
  assert.ok(
    ts.isPropertyAccessExpression(statusDecl.initializer!) &&
      ts.isIdentifier(statusDecl.initializer.expression) &&
      statusDecl.initializer.expression.text === "fin" &&
      statusDecl.initializer.name.text === "status",
    "finStatus is not `fin.status`",
  );
  assert.ok(jsonCalls.length >= 1, "passkeyLogin no longer parses the finish response's body");
  const firstParse = Math.min(...jsonCalls.map((c) => c.getStart(sf)));
  assert.ok(
    statusDecl.getStart(sf) < firstParse,
    "the finish body is parsed before the status is read, so a non-JSON refusal still decides the sentence",
  );

  // Nothing below may branch on the response object again: the captured status is
  // the only thing the sentences are chosen from.
  const finReads: string[] = [];
  const walkReads = (node: ts.Node) => {
    if (
      ts.isPropertyAccessExpression(node) &&
      ts.isIdentifier(node.expression) &&
      node.expression.text === "fin" &&
      node.name.text === "status"
    ) {
      finReads.push(node.getText(sf));
    }
    node.forEachChild(walkReads);
  };
  walkReads(fn);
  assert.deepEqual(finReads, ["fin.status"], "a sentence is chosen from fin.status instead of the captured status");
});

test("a 429 goes through rateLimitErr with the server's own Retry-After", () => {
  assert.match(finishRegion, /if\s*\(\s*finStatus\s*===\s*429\s*\)/, "the finish refusal has no throttle branch");
  // The window comes from the response HEADER (the per-IP limiter writes
  // `Retry-After: 30`) through the shared mapping — not a second hand-rolled
  // sentence, and not a dropped header.
  assert.match(
    finishRegion,
    /rateLimitErr\(\s*finStatus\s*,\s*fin\.headers\.get\(\s*"retry-after"\s*\)/,
    "the finished refusal does not hand the response's retry-after to rateLimitErr",
  );
  // A headerless 429 must still say "too many attempts", not fall to a generic
  // line: `rateLimitErr` answers that itself, and the `||` fallback is for the
  // impossible case.
  const branchAt = finishRegion.indexOf("if (finStatus === 429)");
  const branchEnd = finishRegion.indexOf("} else if", branchAt);
  assert.ok(branchEnd > branchAt, "could not find the end of the throttle branch");
  const branch = finishRegion.slice(branchAt, branchEnd);
  assert.match(branch, /rateLimitErr\(/, "the throttle branch does not use the shared rate-limit sentence");
  assert.match(branch, /t\("login\.genericError"\)/, "the throttle branch has no fallback sentence");
});

test("a REAL assertion rejection keeps login.passkeyInvalid, and only a 400 or 401 may", () => {
  // The closed set: exactly the two statuses `passkeyLoginFinish` answers with
  // after it read the assertion (passkey.go — 400 for an unusable key/session/
  // response, 401 for a credential the library refused).
  assert.match(
    finishRegion,
    /finStatus\s*===\s*400\s*\|\|\s*finStatus\s*===\s*401/,
    "the finish refusal no longer separates a rejected assertion from a refused call",
  );
  // A range test is how a 5xx would inherit the passkey sentence again — the
  // whole point of this card.
  assert.doesNotMatch(finishRegion, /finStatus\s*>=\s*4/, "the passkey sentence is reached by a status RANGE, which a 5xx could enter");

  const at = finishRegion.indexOf("} else if (finStatus === 400");
  const end = finishRegion.indexOf("} else {", at);
  assert.ok(at > 0 && end > at, "could not find the rejected-assertion branch");
  const branch = finishRegion.slice(at, end);
  assert.match(
    branch,
    /apiErr\(\s*refusal\s*,\s*t\("login\.passkeyInvalid"\)\s*,\s*t\s*\)/,
    "a rejected assertion no longer answers login.passkeyInvalid",
  );
});

test("every other non-2xx answers the page's own failure line, and the passkey sentence appears once", () => {
  const occurrences = finishRegion.split('t("login.passkeyInvalid")').length - 1;
  assert.equal(
    occurrences,
    1,
    `expected the passkey sentence at the rejected-assertion branch only, found ${occurrences} sites in the finish region — ` +
      "a second site is a server refusal wearing the passkey's sentence",
  );

  // The tail after the rejected-assertion branch is the catch-all: a 429, 400 and
  // 401 have each been handled above, so a 5xx / 404 / proxy page lands here.
  const elseAt = finishRegion.indexOf("} else {");
  assert.ok(elseAt > 0, "the finish refusal has no catch-all branch");
  const tail = finishRegion.slice(elseAt, finishRegion.indexOf("} catch", elseAt));
  assert.match(tail, /t\("login\.genericError"\)/, "the catch-all branch does not answer the page's own failure line");
  assert.doesNotMatch(tail, /login\.passkeyInvalid/, "the catch-all branch still calls the passkey invalid");
});

test("the three sentences are distinct and translated in all five locales", () => {
  const keys = ["login.passkeyInvalid", "login.genericError", "apiErr.rateLimited"] as const;
  for (const key of keys) {
    assert.ok(dict[key], `${key} is missing from DICT`);
  }

  for (const lang of LANGS) {
    const sentences = keys.map((k) => dict[k][lang]);
    for (const [i, s] of sentences.entries()) {
      assert.equal(typeof s, "string", `${keys[i]} has no ${lang} translation`);
      assert.ok((s as string).trim() !== "", `an empty ${lang} sentence for ${keys[i]} would render nothing`);
    }
    // A passkey rejection and a refused call must READ differently, or this fix
    // is invisible on screen in that locale.
    assert.notEqual(
      sentences[0],
      sentences[1],
      `lang=${lang}: "your passkey is invalid" and "the call failed" read the same, so this fix is invisible`,
    );
  }
});
