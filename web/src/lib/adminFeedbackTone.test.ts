// /admin feedback: the BANNER'S TONE must travel with the delivery OUTCOME
// (card t_01385c19, found while verifying t_2574be79 / PR #997).
//
// THE DEFECT THIS PINS (source-derived; the rendered half is e2e)
//   `/admin` → Feedback has TWO message states — `fbMsg` (plain emerald text,
//   the success line) and `fbActionErr` (a red `role="alert"`):
//
//     web/src/app/admin/page.tsx  {fbMsg && <p className="mt-3 text-sm text-emerald-700">{fbMsg}</p>}
//                                 {fbActionErr && (<p role="alert" className="mt-3 break-words text-sm text-red-600">…}
//
//   `replyResult`/`retryResult` (src/lib/feedbackNotify.ts) return THREE kinds —
//   `ok`, `error`, `unconfirmed` — and only `ok` is a delivery. `unconfirmed`
//   carries a sentence that says so ("The delivery status … was not confirmed —
//   check the thread before assuming the filer was told"; "Nothing was sent — no
//   recipient is configured …"). Both writers branched on `error` FIRST and sent
//   EVERYTHING ELSE to `fbMsg`, so the unconfirmed half — a statement that the
//   mail did NOT go out — was painted in the same emerald chrome the console uses
//   for "Notification sent again."; and the retry's own caveat
//   (`afb.notifyRetryStillMarked`: "Re-sent — but the thread still records a
//   failure, so it was not delivered to everyone.") was on that line too.
//
//   That is the rule PR #997 established on /settings (card t_ad2af554): THE TONE
//   TRAVELS WITH THE TEXT. A page whose one message state carries outcomes of
//   different severity must branch the chrome on a state that says WHICH outcome
//   it was, never on a two-way `else` that swallows the middle case.
//
// WHY A SOURCE SCAN *AND* AN E2E ARM
//   The browser arm (web/e2e/feedback-notify-marker.spec.ts) measures the
//   rendered result on today's bytes — the sentence's computed ink and its ARIA
//   role, against the success CONTROL. What it cannot do is see the writer nobody
//   drove: the `unconfirmed` reply and the `skipped` retry are only reachable with
//   a server that answers a specific field, and a future edit could put a fourth
//   caveat on the success line without any existing arm driving it. This scan is
//   the repo-level half: it reads every `setFbMsg`/`setFbActionErr` call site and
//   requires the success line to be gated on the ONE kind that is a delivery.
//
// WHERE THE RULE IS DERIVED FROM — and why not a grep
//   "Which branch does this `setFbMsg(...)` run in?" is a control-flow fact, so
//   the answer comes from the TypeScript parser (a regex over the source cannot
//   tell a condition from a string that looks like one). Each call is annotated
//   with the chain of `if` conditions it sits under and which SIDE of each it is
//   on, and the rule is stated against that: a non-empty write to the SUCCESS
//   line must be in the THEN side of `result.kind === "ok"`. The pre-fix
//   two-branch `else` fails it by construction — its write's innermost condition
//   is `result.kind === "error"` (reply) or the marker guard (retry), and neither
//   is `ok`.
//
// Run with:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import ts from "typescript";

import { replyResult, retryResult } from "./feedbackNotify.ts";

const SRC = fileURLToPath(new URL("../", import.meta.url)); // web/src/
// Named `srcAt`, not `read`: `src/lib/buildContext.test.ts` resolves every
// `read("<lit>")` in this suite through a helper shape it must be able to see; a
// template-literal path is invisible to that guard, which is correct — the file
// is under web/src and the web image copies that tree whole.
const srcAt = (rel: string) => readFileSync(`${SRC}${rel}`, "utf8");

const PAGE = srcAt("app/admin/page.tsx");

/** The success line's sentence text is a delivery claim; the failure line is a
 *  red alert. Both are pinned below from the REAL render sites. */
const SUCCESS_LINE = "text-emerald-700";
const FAILURE_LINE = "text-red-600";

/**
 * The message keys an OUTCOME can carry, DERIVED from the mapping module rather
 * than re-typed: every outcome the Go side can publish is run through
 * `replyResult`/`retryResult` and its key collected. Split by kind, because the
 * rule is about which side a key's meaning belongs on.
 *
 * `afb.notifyRetryStillMarked` is the page's OWN caveat (a delivery that did not
 * clear the thread's marker) and is not produced by either function, so it is
 * added explicitly — it is the second writer this card is about.
 */
const OUTCOME_KEYS: Record<"ok" | "error" | "unconfirmed", string[]> = (() => {
  const out: Record<string, Set<string>> = { ok: new Set(), error: new Set(), unconfirmed: new Set() };
  const outcomes = ["queued", "sent", "failed", "none", "skipped", "", "unknown", "Queued", "FAILED "];
  for (const o of outcomes) {
    for (const r of [replyResult(o), retryResult(o)]) out[r.kind].add(r.messageKey);
  }
  const plain: Record<"ok" | "error" | "unconfirmed", string[]> = { ok: [], error: [], unconfirmed: [] };
  for (const k of ["ok", "error", "unconfirmed"] as const) plain[k] = [...out[k]].sort();
  plain.unconfirmed.push("afb.notifyRetryStillMarked");
  return plain;
})();

/** The non-ok outcome keys: a write of any of these is a failure STATEMENT. */
const NON_OK_KEYS = [...OUTCOME_KEYS.error, ...OUTCOME_KEYS.unconfirmed];

type Call = {
  name: "setFbMsg" | "setFbActionErr";
  arg: string;
  /** The `if` conditions this call sits under, outermost first. */
  conds: string[];
  /** For each entry in `conds`, whether the call is on the THEN side. */
  thenSide: boolean[];
};

/**
 * Every `setFbMsg(...)` / `setFbActionErr(...)` call in a source string, each
 * annotated with the enclosing `if` conditions and sides. An `else if` is its own
 * `IfStatement` (carrying its own condition), so an `else if (C) { … }` chain is
 * read as nested conditions — which is what makes "which branch runs" answerable.
 */
function setterCalls(src: string): Call[] {
  const sf = ts.createSourceFile("guard.tsx", src, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  const out: Call[] = [];
  const text = (n: ts.Node) => n.getText(sf).replace(/\s+/g, " ").trim();

  const visit = (n: ts.Node, conds: string[], sides: boolean[]): void => {
    if (ts.isIfStatement(n)) {
      const cond = text(n.expression);
      visit(n.thenStatement, [...conds, cond], [...sides, true]);
      if (n.elseStatement) {
        // An `else` — INCLUDING `else if`. The else chain of
        // `if (A) … else if (B) … else …` is `IfStatement(A, else=IfStatement(B,
        // else=…))`, so descending with A recorded as FALSE and letting the
        // nested IfStatement push B itself gives the final branch the chain
        // `[A else, B else]`. Recording only B (the `else if` treated as a fresh
        // statement) loses the fact that A was false, which is the fact that says
        // `kind !== "ok"` on the success write's path.
        visit(n.elseStatement, [...conds, cond], [...sides, false]);
      }
      return; // handled explicitly; a generic walk would double-count the branches
    }
    if (ts.isCallExpression(n) && ts.isIdentifier(n.expression)) {
      const name = n.expression.text;
      if (name === "setFbMsg" || name === "setFbActionErr") {
        out.push({ name, arg: n.arguments.map(text).join(", "), conds, thenSide: sides });
      }
    }
    ts.forEachChild(n, (c) => visit(c, conds, sides));
  };
  visit(sf, [], []);
  return out;
}

/** A non-empty write: `setFbMsg("")` clears the line and is not an outcome. */
const isWrite = (c: Call) => c.arg !== '""';

/**
 * A write of an OUTCOME MESSAGE — the field the API publishes, or one of the keys
 * that field can map to. A page-local confirmation (`afb.statusSet`, the status
 * POST's own success) is NOT one of these and is deliberately out of scope: this
 * card is about the delivery outcome, and widening the rule to every write would
 * flag a genuine success.
 */
function isOutcomeWrite(c: Call): boolean {
  if (c.arg.includes("result.messageKey")) return true;
  return NON_OK_KEYS.some((k) => c.arg.includes(`"${k}"`)) || OUTCOME_KEYS.ok.some((k) => c.arg.includes(`"${k}"`));
}

/** The innermost condition and side at a call — what actually decides the write. */
function innermost(c: Call): { cond: string; then: boolean } {
  const i = c.conds.length - 1;
  return { cond: i >= 0 ? c.conds[i] : "", then: i >= 0 ? c.thenSide[i] : false };
}

/**
 * Does reaching this call IMPLY the outcome was a delivery (`kind === "ok"`)?
 *
 * Walked from the INNERMOST condition outward, because the innermost condition is
 * what actually decides the write. Only a test that NAMES the delivery decides it:
 *
 *   * `kind === "ok"` on the THEN side  → yes;
 *   * `kind !== "ok"` on the ELSE side  → yes (the other spelling of the same gate);
 *   * `kind === "error"` / `kind === "unconfirmed"` on the THEN side → no (this
 *     path is one of the not-ok kinds);
 *   * anything else — the marker check, or an ELSE side of an `error`/`unconfirmed`
 *     test — is walked past.
 *
 * DELIBERATELY NOT ACCEPTED: `if (kind === "error") … else if (kind === "unconfirmed")
 * … else { success }`. It is equivalent TODAY and would be safe today, which is
 * exactly the problem: the moment a fourth kind is added to `replyResult`/
 * `retryResult`, its branch falls through that `else` onto the success line — the
 * same "the middle case is swallowed" defect this card is about. The gate has to be
 * written against the ONE kind that is a delivery, not against the kinds that are
 * not, so a new kind defaults to the FAILURE chrome.
 */
function impliesDelivery(c: Call): boolean {
  for (let i = c.conds.length - 1; i >= 0; i -= 1) {
    const k = c.conds[i].replace(/\s+/g, " ").replace(/'ok'/g, '"ok"');
    const then = c.thenSide[i];
    if (k === 'result.kind === "ok"') return then;
    if (k === 'result.kind !== "ok"') return !then;
    if ((k === 'result.kind === "error"' || k === 'result.kind === "unconfirmed"') && then) return false;
  }
  return false;
}

/**
 * The offenders in one source: every OUTCOME write to the SUCCESS line that is not
 * reachable only on a delivery, and every outcome write to the FAILURE line that
 * IS (which would make the red alert the confirmation — the same defect mirrored).
 */
function offenders(src: string): string[] {
  const bad: string[] = [];
  for (const c of setterCalls(src).filter((x) => isWrite(x) && isOutcomeWrite(x))) {
    const { cond, then } = innermost(c);
    const where = c.conds.length ? `\`if (${cond})\` ${then ? "then" : "else"}` : "no condition";
    if (c.name === "setFbMsg" && !impliesDelivery(c)) {
      bad.push(
        `${c.name}(${c.arg}) writes the SUCCESS line (${SUCCESS_LINE}) under ${where} — a path that ` +
          `does NOT imply the outcome was a delivery. Only \`kind === "ok"\` may use the success line`,
      );
    }
    if (c.name === "setFbActionErr" && c.arg.includes("result.messageKey") && impliesDelivery(c)) {
      bad.push(
        `${c.name}(${c.arg}) puts an outcome message on the FAILURE line under ${where}, on a delivery ` +
          "path — the red alert would then be the confirmation",
      );
    }
  }
  return bad;
}

/** The opening tag of the element carrying `attr`, so a sibling cannot satisfy it. */
function openingTagWith(src: string, attr: string): string | null {
  const at = src.indexOf(attr);
  if (at < 0) return null;
  const open = src.lastIndexOf("<", at);
  const close = src.indexOf(">", at);
  if (open < 0 || close < 0) return null;
  return src.slice(open, close + 1);
}

test("the scan is looking at the real page (it cannot pass vacuously)", () => {
  const calls = setterCalls(PAGE);
  // Both setters, and enough call sites that a shrinking population reads as a
  // broken scan rather than a clean page.
  for (const name of ["setFbMsg", "setFbActionErr"] as const) {
    assert.ok(
      calls.filter((c) => c.name === name).length >= 2,
      `the scan found <2 \`${name}\` calls in app/admin/page.tsx — re-anchor this guard`,
    );
  }
  // The two render sites this card is about must still exist, or the rules below
  // are about an element that is gone.
  const okLine = openingTagWith(PAGE, "{fbMsg}");
  assert.ok(okLine, "app/admin/page.tsx no longer renders `{fbMsg}` — re-anchor this guard");
  assert.match(okLine!, new RegExp(SUCCESS_LINE), "the fbMsg element is no longer the emerald success line");
  // The tone must be OBSERVABLE, or neither this file nor the e2e arm can judge
  // the rendered result — the `data-tone` the other tone guards established.
  assert.match(okLine!, /data-tone="success"/, "the fbMsg element does not expose data-tone=\"success\"");
  assert.match(okLine!, /data-testid="afb-msg"/, "the fbMsg element carries no data-testid=\"afb-msg\" hook");
  const failLine = openingTagWith(PAGE, "{fbActionErr}");
  assert.ok(failLine, "app/admin/page.tsx no longer renders `{fbActionErr}` — re-anchor this guard");
  assert.match(failLine!, /role="alert"/, "the fbActionErr element is not a live alert region");
  assert.match(failLine!, new RegExp(FAILURE_LINE), "the fbActionErr element is no longer the red failure line");
  assert.match(failLine!, /data-tone="failure"/, "the fbActionErr element does not expose data-tone=\"failure\"");
  assert.match(failLine!, /data-testid="afb-msg"/, "the fbActionErr element carries no data-testid=\"afb-msg\" hook");
  // The one condition the rule turns on must be readable, i.e. still written in
  // the page. A guard that matched nothing would otherwise read as a clean run.
  assert.ok(
    calls.some((c) => c.conds.some((k) => k.includes("result.kind"))),
    "no `result.kind …` condition in app/admin/page.tsx — the success line can no longer be gated on " +
      "a delivery, which is this card's whole fix",
  );
});

test("every non-empty write to the success line is gated on a DELIVERY (kind \"ok\")", () => {
  assert.deepEqual(
    offenders(PAGE),
    [],
    "The /admin Feedback banner paints its success chrome from `fbMsg` and its failure chrome from " +
      "`fbActionErr`, and `replyResult`/`retryResult` return a THIRD kind — `unconfirmed` — whose own " +
      "sentence says the mail did NOT go out (\"The delivery status of the notification was not " +
      "confirmed\", \"Nothing was sent — no recipient is configured\"). Branching on `error` and sending " +
      "everything else to the success line puts that statement in the emerald chrome the console uses " +
      "for \"Notification sent again.\" (card t_01385c19). Gate the success line on `result.kind === " +
      "\"ok\"` and send every other kind to the red alert.\n  " +
      offenders(PAGE).join("\n  "),
  );
});

test("the retry's marker-still-set caveat is a failure statement, not a confirmation", () => {
  // The caveat itself: "Re-sent — but the thread still records a failure, so it
  // was not delivered to everyone. Check the banner below." The retry wrote it
  // with `setFbMsg`, i.e. onto the success line, directly above a banner saying
  // the filer was NOT told. It is still rendered (the message must not be
  // deleted), but it must reach the FAILURE line.
  const calls = setterCalls(PAGE).filter(isWrite);
  const hit = calls.filter((c) => c.arg.includes("afb.notifyRetryStillMarked"));
  assert.equal(hit.length, 1, "afb.notifyRetryStillMarked is no longer written exactly once — re-anchor this guard");
  assert.equal(
    hit[0].name,
    "setFbActionErr",
    "afb.notifyRetryStillMarked (\"… the thread still records a failure …\") is written to the SUCCESS " +
      "line again — a delivery that did NOT clear the marker would wear the same emerald chrome as " +
      "\"Notification sent again.\" (card t_01385c19)",
  );
  // …and it is reached only when the reload really still carries the marker.
  assert.ok(
    calls.some((c) => c.arg.includes("hasNotifyFailure(reloaded)") || c.conds.some((k) => k.includes("hasNotifyFailure(reloaded)"))),
    "the marker branch no longer reads the RELOADED thread — a retry whose notification did not clear " +
      "the failure could claim it was delivered",
  );
});

test("the guard would fail on the code it replaced (in-file bite proof)", () => {
  // The exact pre-fix branches, planted hermetically: each must be REJECTED by the
  // same reader the assertions above use. Without this, a scan that matched
  // nothing would read as a clean file.
  const preFixReply = [
    "if (result.kind === \"error\") {",
    "  setFbActionErr(t(result.messageKey as Key));",
    "} else {",
    "  setFbMsg(t(result.messageKey as Key));",
    "}",
  ].join("\n");
  assert.ok(
    offenders(preFixReply).some((o) => o.includes("setFbMsg")),
    "the pre-fix reply branch (`else` → setFbMsg) was read as correct — the guard cannot bite on the " +
      "very shape this card was filed for",
  );

  const preFixRetry = [
    "if (result.kind === \"error\") {",
    "  setFbActionErr(t(result.messageKey as Key));",
    "} else if (reloaded && hasNotifyFailure(reloaded)) {",
    "  setFbMsg(t(\"afb.notifyRetryStillMarked\"));",
    "} else {",
    "  setFbMsg(t(result.messageKey as Key));",
    "}",
  ].join("\n");
  const retryOffenders = offenders(preFixRetry);
  assert.ok(
    retryOffenders.some((o) => o.includes("setFbMsg")),
    "the pre-fix retry branch was read as correct — the `unconfirmed` half would be back on the success line",
  );
  assert.ok(
    retryOffenders.some((o) => o.includes("afb.notifyRetryStillMarked")),
    "the pre-fix marker caveat on the success line was not flagged",
  );

  // …and BOTH fixed shapes are accepted, so the guard does not pin one spelling:
  // the page's actual order (`if (kind !== "ok") … else if (marker) … else ok`)
  // and the equivalent `if (ok) … else …` written the other way round.
  const fixedRetry = [
    "if (result.kind !== \"ok\") {",
    "  setFbActionErr(t(result.messageKey as Key));",
    "} else if (reloaded && hasNotifyFailure(reloaded)) {",
    "  setFbActionErr(t(\"afb.notifyRetryStillMarked\"));",
    "} else {",
    "  setFbMsg(t(result.messageKey as Key));",
    "}",
  ].join("\n");
  assert.deepEqual(offenders(fixedRetry), [], "the FIXED retry branch is flagged by its own guard");

  const fixedRetryAlt = [
    "if (result.kind === \"error\") {",
    "  setFbActionErr(t(result.messageKey as Key));",
    "} else if (result.kind === \"unconfirmed\") {",
    "  setFbActionErr(t(result.messageKey as Key));",
    "} else if (reloaded && hasNotifyFailure(reloaded)) {",
    "  setFbActionErr(t(\"afb.notifyRetryStillMarked\"));",
    "} else {",
    "  setFbMsg(t(result.messageKey as Key));",
    "}",
  ].join("\n");
  // This shape is equivalent TODAY and is REFUSED on purpose: a fourth kind added
  // to replyResult/retryResult would fall through that final `else` onto the
  // success line — the "the middle case is swallowed" defect itself. The gate must
  // name the ONE kind that is a delivery, so the default for anything new is the
  // failure chrome.
  assert.ok(
    offenders(fixedRetryAlt).some((o) => o.includes("setFbMsg")),
    "`if (kind === \"error\") … else if (kind === \"unconfirmed\") … else success` was accepted — a " +
      "future kind would silently wear the success chrome",
  );

  // The reply's fixed shape, both ways.
  const fixedReply = [
    "if (result.kind === \"ok\") {",
    "  setFbMsg(t(result.messageKey as Key));",
    "} else {",
    "  setFbActionErr(t(result.messageKey as Key));",
    "}",
  ].join("\n");
  assert.deepEqual(offenders(fixedReply), [], "the FIXED reply branch is flagged by its own guard");

  const fixedReplyAlt = [
    "if (result.kind !== \"ok\") {",
    "  setFbActionErr(t(result.messageKey as Key));",
    "} else {",
    "  setFbMsg(t(result.messageKey as Key));",
    "}",
  ].join("\n");
  assert.deepEqual(offenders(fixedReplyAlt), [], "the `!== \"ok\"` reply spelling is flagged");

  // …and the mirrored defect: an outcome message on the FAILURE line on the
  // delivery side (the red alert would be the confirmation).
  const mirrored = [
    "if (result.kind === \"ok\") {",
    "  setFbActionErr(t(result.messageKey as Key));",
    "} else {",
    "  setFbMsg(t(result.messageKey as Key));",
    "}",
  ].join("\n");
  assert.equal(offenders(mirrored).length, 2, `the mirrored defect was not read as two offenders: ${offenders(mirrored)}`);
});
