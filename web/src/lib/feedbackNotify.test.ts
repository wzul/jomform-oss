// Feedback notification: the console must not claim the filer was told when the
// mail never left the building, and a failure must be visible AND actionable.
//
// THE DEFECT. api/internal/httpapi/rest_feedback.go discarded both send errors,
// notifyFeedbackFiler returned nothing, and the reply route answered
// {"status":"sent"} regardless. On the console that read as success: the admin
// saw "Reply sent — the user has been notified", closed the thread, and the filer
// waited forever for mail the queue had rejected. The API now publishes the
// outcome in a `notification` field (queued|sent|failed|none) and marks a failure
// on the thread; this guard pins that the console READS it rather than assuming.
//
// WHAT THIS GUARD PINS (each is a way the fix can silently regress)
//   1. Every outcome the Go side can publish maps onto the right console state —
//      pinned CROSS-LANGUAGE against rest_feedback.go's own constants, so a
//      rename on one side cannot make the console treat a delivery as unknown.
//   2. `failed` is an ERROR on the reply path and on the retry path, never the
//      success message.
//   3. A missing or unrecognised `notification` value is NOT success. This is the
//      original bug in its most likely new shape: an older server (or a proxy
//      stripping the field) must not restore "the filer was notified".
//   4. The failure marker is rendered on the THREAD and in the LIST, and the retry
//      control sits with it — a marker an operator cannot see or cannot act on
//      only half-fixes the card.
//   5. The retry calls the real route (POST /api/admin/feedback/{id}/notify, which
//      maps to /v1/admin/feedback/{id}/notify) — a wrong path would 404 while
//      every pure-function assertion above stayed green.
//   6. Every new console string exists in all five locales (the #514 rule).
//
// WHAT IT DOES NOT COVER (stated honestly): it cannot execute React, so it does
// not observe the rendered banner, the click or the round trip. The executed
// proof of the routes is api/internal/httpapi/feedback_notify_failure_test.go,
// which drives the real mux, and the rendered result is checked in a browser.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { DICT, LANGS } from "../components/i18nDict.ts";
import {
  delivered,
  hasNotifyFailure,
  notifyFailedWhoKey,
  replyResult,
  retryResult,
} from "./feedbackNotify.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

// Paths are resolved relative to THIS file (web/src/lib/), so `../` reaches
// web/src and `../../../` reaches the repo root above web/.
const PAGE = "../app/admin/page.tsx";
const REST = "../../../api/internal/httpapi/rest_feedback.go";
const STORE = "../../../api/internal/dbstore/feedback.go";

// ── 1. the cross-language contract ──────────────────────────────────────────

test("the outcome constants the console reads are the ones the API publishes", () => {
  // Read the Go constants rather than re-typing the strings: a rename there and
  // an unchanged literal here is exactly the silent drift this pins.
  const go = read(REST);
  const pairs: [string, string][] = [
    ["feedbackNotifyQueued", "queued"],
    ["feedbackNotifySent", "sent"],
    ["feedbackNotifyFailed", "failed"],
    ["feedbackNotifyNone", "none"],
  ];
  for (const [constName, want] of pairs) {
    assert.match(
      go,
      new RegExp(`\\b${constName}\\s*=\\s*"${want}"`),
      `${constName} is no longer "${want}" in rest_feedback.go — the console's mapping is now wrong`,
    );
  }

  // And the marker kinds, read from the store where the CHECK constraint lives.
  const store = read(STORE);
  assert.match(store, /FeedbackNotifyKindFiler\s*=\s*"filer"/);
  assert.match(store, /FeedbackNotifyKindAdmin\s*=\s*"admin"/);
});

test("the API publishes the outcome on the reply and on the retry response", () => {
  const go = read(REST);
  // The reply response must carry the field. A response without it is what the
  // old code answered, and the console's "unconfirmed" branch would be the only
  // thing standing between that and a false "the filer was notified".
  assert.match(
    go,
    /"notification":\s*notification/,
    "the admin reply response no longer publishes `notification` — the console falls back to " +
      '"unconfirmed" for every reply, which is a regression from the current honest answer',
  );
  assert.match(
    go,
    /"status":\s*"retried",\s*"notification":\s*outcome/,
    "the retry response no longer publishes `notification`",
  );
  // The failure must be RECORDED, not only logged: a log line is not a surface.
  assert.match(go, /RecordNotifyFailure\(/, "the failure marker is never written");
});

// ── 2/3. the outcome mapping ────────────────────────────────────────────────

test("a delivered outcome is success, and only a delivered outcome is", () => {
  assert.deepEqual(replyResult("queued"), { kind: "ok", messageKey: "afb.replied" });
  assert.deepEqual(replyResult("sent"), { kind: "ok", messageKey: "afb.replied" });
});

test("a failed notification is an error on the reply path, never the success message", () => {
  const r = replyResult("failed");
  assert.equal(r.kind, "error", "the reply console state for a failed notification must be an error");
  assert.notEqual(r.messageKey, "afb.replied");
  assert.match(r.messageKey, /NotifyFailed/);
});

test("an absent or unknown outcome is NOT success (the defect's likely new shape)", () => {
  // An older API, a proxy stripping the field, a typo, or a future value: none of
  // these is evidence the filer was told, so none may render the success copy.
  for (const outcome of ["", "unknown", "Queued", "FAILED ", "delivered"]) {
    const r = replyResult(outcome);
    assert.notEqual(
      r.kind,
      "ok",
      `outcome ${JSON.stringify(outcome)} was read as SUCCESS — the console would tell the admin ` +
        "the filer was notified with no evidence, which is the defect",
    );
    assert.equal(r.kind, "unconfirmed");
    assert.notEqual(r.messageKey, "afb.replied");
  }
});

test("a retry that still failed is reported as a failure, not as done", () => {
  assert.equal(retryResult("failed").kind, "error");
  assert.notEqual(retryResult("failed").messageKey, "afb.notifyRetryDone");
  assert.equal(retryResult("queued").kind, "ok");
  assert.equal(retryResult("none").kind, "ok");
});

test("a retry that sent NOTHING is never reported as sent again", () => {
  // `skipped` is the admin lane's no-op exit (no super-admin recipient is
  // configured): no job was enqueued, so "Notification sent again." would be a
  // claim no delivery backs — the exact false-positive class this module exists
  // to remove, and it would land on the retry path where the operator is already
  // looking at a failure marker.
  const r = retryResult("skipped");
  assert.notEqual(r.messageKey, "afb.notifyRetryDone", "a skipped retry rendered the success copy");
  assert.equal(r.kind, "unconfirmed");
  assert.equal(delivered("skipped"), false, "`skipped` must never count as a delivery");
  // And an unknown/absent value (older or newer server, a stripped field) is not
  // "sent again" either — the same rule the reply path already applies.
  for (const outcome of ["", "unknown", "Skipped", "SKIPPED "]) {
    const got = retryResult(outcome);
    assert.notEqual(
      got.messageKey,
      "afb.notifyRetryDone",
      `retry outcome ${JSON.stringify(outcome)} was presented as a delivery`,
    );
    assert.equal(got.kind, "unconfirmed");
  }
  // `none` stays a success: there was NO failure to retry, so nothing changed and
  // nothing is wrong. It must not be turned into an alarm by the fix above.
  assert.equal(retryResult("none").messageKey, "afb.notifyRetryDone");
});

test("the marker predicate is one rule for the list badge and the thread banner", () => {
  assert.equal(hasNotifyFailure({ notify_failed_at: "2026-09-20T10:00:00Z" }), true);
  assert.equal(hasNotifyFailure({}), false);
  // An EMPTY STRING must not read as a marker: the Go JSON omits the field when
  // there is no failure, but a hand-built payload with "" must not light up the
  // banner either.
  assert.equal(hasNotifyFailure({ notify_failed_at: "" }), false);
});

test("an unknown marker kind names the platform team, never a raw enum value", () => {
  assert.equal(notifyFailedWhoKey("filer"), "afb.notifyFailedFiler");
  assert.equal(notifyFailedWhoKey("admin"), "afb.notifyFailedAdmin");
  // A value the console does not know (a future kind, or a truncated field) must
  // still render words, not an identifier.
  assert.equal(notifyFailedWhoKey(undefined), "afb.notifyFailedAdmin");
  assert.equal(notifyFailedWhoKey("something-else"), "afb.notifyFailedAdmin");
});

// ── 4/5. the wiring on the page ─────────────────────────────────────────────

test("the triage section renders the marker on the thread AND in the list", () => {
  const page = read(PAGE);
  const uses = page.match(/hasNotifyFailure\(/g) ?? [];
  assert.ok(
    uses.length >= 2,
    `hasNotifyFailure is used ${uses.length} time(s) — it must guard BOTH the list badge and the ` +
      "thread banner, or an operator has to open every thread to find the broken one",
  );
  assert.match(page, /afb\.notifyFailedTitle/, "the banner/badge title is not rendered");
  assert.match(page, /afb\.notifyFailedBadge/, "the list badge is not rendered");
  assert.match(page, /role="alert"/, "the marker is not an alert region — a screen reader would miss it");
});

test("the marker carries the retry control, and the retry hits the real route", () => {
  const page = read(PAGE);
  // The action half: seeing the marker must be enough to act on it.
  assert.match(page, /afb\.notifyRetry\b/, "no retry control is rendered next to the marker");
  assert.match(
    page,
    /\/api\/admin\/feedback\/\$\{encodeURIComponent\(fbOpenId\)\}\/notify/,
    "the retry does not call POST /api/admin/feedback/{id}/notify",
  );
  assert.match(page, /method: "POST"/, "the retry is not a POST");
  // And the route exists on the backend, at the path the proxy mapping implies.
  const rest = read("../../../api/internal/httpapi/rest.go");
  assert.match(
    rest,
    /mux\.Handle\("POST \/v1\/admin\/feedback\/\{id\}\/notify"/,
    "the backend has no POST /v1/admin/feedback/{id}/notify — the console would 404",
  );
});

test("the outcome message is set AFTER the reload that resets it", () => {
  const page = read(PAGE);
  // `openFeedback` resets fbMsg/fbActionErr (it makes switching threads safe),
  // so setting the outcome before awaiting the reload leaves the admin with
  // NOTHING on screen — the reply reaches the server and no outcome is shown.
  // The browser proof caught this; the guard pins the ordering.
  //
  // The RETRY path reloads through `loadFeedbackThread` (it needs the thread back
  // to check the marker — see the next test), which resets the same state.
  //
  // The window is wide enough to reach the LAST writer on each path: card
  // t_01385c19 added the `kind === "ok"` branch, so the retry's outcome write now
  // sits after the error branch AND the marker branch (measured offsets from the
  // mapping call: reply 401 → 1199, retry 820 → 1768). A tighter window would
  // report "the outcome message is never set" for a page that sets it.
  for (const [fn, reload] of [
    ["replyResult", "await openFeedback(fbOpenId)"],
    ["retryResult", "await loadFeedbackThread(fbOpenId)"],
  ] as const) {
    const call = page.indexOf(`${fn}(notification)`);
    assert.ok(call > 0, `${fn} is not called`);
    const window = page.slice(call, call + 3200);
    const openAt = window.indexOf(reload);
    const errAt = window.indexOf("setFbActionErr(t(result.messageKey as Key))");
    const markAt = window.indexOf("hasNotifyFailure(reloaded)");
    const msgAt = window.indexOf("setFbMsg(t(result.messageKey as Key))");
    assert.ok(openAt >= 0, `${fn}: no reload (${reload}) after the outcome mapping`);
    assert.ok(errAt >= 0, `${fn}: the outcome message is never set`);
    assert.ok(msgAt >= 0, `${fn}: the success message is never set`);
    assert.ok(
      openAt < errAt && openAt < msgAt,
      `${fn}: the outcome message is set BEFORE the reload, which resets that state — ` +
        "the admin sees no outcome for a reply that was stored",
    );
    if (fn === "retryResult") {
      assert.ok(markAt >= 0, "retryResult: the marker check is gone from the retry path");
      assert.ok(
        openAt < markAt,
        "retryResult: the marker is checked before the reload that produces the thread — " +
          "the value it reads is not the one the retry left behind",
      );
    }
  }
});

test("a retry that left the marker set cannot also claim it was sent", () => {
  const page = read(PAGE);
  // Defect 3. A retry reports what the ATTEMPT did; the marker is what the thread
  // still shows. When they disagree — the retry queued a mail but the failure is
  // still recorded (a differently-kinded marker, a send that did not clear it) —
  // the console printed "Notification sent again." directly ABOVE a banner saying
  // the filer was NOT told, with nothing to say which one was stale. The two must
  // not be able to contradict each other, so the success copy is gated on the
  // thread that came back from the reload.
  const call = page.indexOf("retryResult(notification)");
  assert.ok(call > 0, "the retry does not map the outcome field");
  // Wide enough to reach the marker check AND the writers after it (card
  // t_01385c19 added a branch between them); measured, the last writer sits 2859
  // bytes past the mapping call.
  const window = page.slice(call, call + 3200);

  assert.match(
    window,
    /hasNotifyFailure\(/,
    "the retry's success message is not gated on the RELOADED thread's marker — a retry whose " +
      "notification did not clear the failure would print \"Notification sent again.\" above the " +
      "\"Notification not delivered\" banner",
  );
  assert.match(
    window,
    /afb\.notifyRetryStillMarked/,
    "there is no message for the marker-still-set case, so a retry that did not clear the failure " +
      "falls through to the success copy",
  );
  // Both branches must exist and the success copy must be the LAST resort: the
  // marker check lives INSIDE the branch that precedes it, and the success copy is
  // what that branch falls through to.
  //
  // THE ORDER CHANGED IN CARD t_01385c19, deliberately. It used to be
  // `error` → marker → success, and the marker branch printed
  // `afb.notifyRetryStillMarked` ("Re-sent — but the thread still records a
  // failure…"), which CLAIMS a re-send happened. A retry whose outcome is
  // `unconfirmed` — `skipped`, no recipient configured, nothing enqueued — reached
  // that branch first, so the console asserted "Re-sent" for an attempt that sent
  // nothing (measured in the browser: the `skipped` arm rendered the "Re-sent"
  // wording before this fix). The not-ok outcomes must therefore be decided FIRST,
  // leaving the "Re-sent" caveat to the paths where a re-send really happened.
  const notOkBranchAt = window.indexOf('if (result.kind !== "ok")');
  const markerBranchAt = window.indexOf("else if (reloaded &&");
  const markerAt = window.indexOf("hasNotifyFailure(reloaded)");
  const doneAt = window.indexOf("setFbMsg(t(result.messageKey as Key))");
  assert.ok(notOkBranchAt >= 0, "the not-ok (failure) branch is missing");
  assert.ok(
    markerBranchAt > notOkBranchAt,
    "the marker branch must be checked AFTER the not-ok branch: `afb.notifyRetryStillMarked` says " +
      "\"Re-sent\", which is false for a retry that sent nothing (card t_01385c19)",
  );
  assert.ok(
    markerAt > markerBranchAt,
    "the marker check is not inside the marker branch",
  );
  assert.ok(
    doneAt > markerAt,
    "the success copy must come after the marker branch, so a marker still set cannot reach it",
  );
  // And the retry must actually READ the reload: `await openFeedback(...)`
  // discards the thread, so a fix that only reordered the calls would still be
  // unable to see the marker.
  assert.match(
    window,
    /=\s*await loadFeedbackThread\(/,
    "the retry awaits the reload without keeping its result, so it cannot know whether the " +
      "failure marker is still set",
  );
});

test("the reply path reads the outcome field instead of assuming success", () => {
  const page = read(PAGE);
  assert.match(page, /replyResult\(notification\)/, "the reply handler does not map the outcome field");
  assert.match(page, /retryResult\(notification\)/, "the retry handler does not map the outcome field");
  // The exact regression: the reply used to set the success message unconditionally.
  assert.doesNotMatch(
    page,
    /setFbReply\(""\);\s*\n\s*setFbMsg\(t\("afb\.replied"\)\);/,
    "the reply handler sets the success message unconditionally again — that is the defect",
  );
});

// ── 6. every new string exists in all five locales ──────────────────────────

test("every new console string is translated in all five locales", () => {
  const keys = [
    "afb.notifyFailedTitle",
    "afb.notifyFailedBadge",
    "afb.notifyFailedWho",
    "afb.notifyFailedFiler",
    "afb.notifyFailedAdmin",
    "afb.notifyRetry",
    "afb.notifyRetrying",
    "afb.notifyRetryDone",
    "afb.notifyRetryFail",
    "afb.notifyRetrySkipped",
    "afb.notifyRetryStillMarked",
    "afb.notifyRetryUnconfirmed",
    "afb.repliedNotifyFailed",
    "afb.repliedUnconfirmed",
  ];
  for (const k of keys) {
    const entry = (DICT as unknown as Record<string, Record<string, string>>)[k];
    assert.ok(entry, `${k} is not in the dictionary`);
    for (const lang of LANGS) {
      assert.ok(
        entry[lang] && entry[lang].trim().length > 0,
        `${k} has no ${lang} translation — a missing locale silently shows English (#514)`,
      );
    }
    // The placeholder must survive in every locale, or the banner shows no party.
    if (k === "afb.notifyFailedWho") {
      for (const lang of LANGS) {
        assert.match(entry[lang], /\{who\}/, `${k}.${lang} dropped the {who} placeholder`);
      }
    }
  }
});
