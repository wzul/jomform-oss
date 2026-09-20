// Onboarding-gap guard: the dashboard must actually SHOW the merchant that the
// form is live, and must not be able to render a dead "View" link.
//
// THE GAP (drop-off, not a crash). Three outside signups built a form, saved it
// as a draft and never published. Nothing failed — the publish handler answers
// 200 — and after a successful publish the builder showed no confirmation, no
// URL and no way to discover the form was on the internet: the "View ↗" link is
// rendered only when a URL already resolved, so a FIRST publish produced nothing
// visible at all.
//
// This repo has no JSX test runner (see src/lib/i18n.test.ts,
// src/lib/formMode.test.ts), so the decision lives in src/lib/publishResult.ts
// and is tested here on the real functions the pages call. The pages' call sites
// are asserted from source below, so the fix cannot become dead code behind a
// green suite.
//
// Run with:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import {
  PUBLISH_NEEDS_ACK,
  acknowledgmentBody,
  liveLink,
  needsProductsIDs,
  outcomeMessage,
  publishOutcome,
} from "./publishResult.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

const builderPage = read("../app/builder/page.tsx");
const formsPage = read("../app/forms/page.tsx");
const settingsPage = read("../app/form-settings/page.tsx");
const i18nDict = read("../components/i18nDict.ts");
// The Go rule this mirrors — read from source so the two cannot drift apart
// silently (a mirror that disagrees is a second, wrong rule).
const goPublishReady = read("../../../api/internal/dbstore/form_publish_ready.go");
const goRestHandlers = read("../../../api/internal/httpapi/rest_handlers.go");

test("a successful publish is reported as live, with the URL", () => {
  const out = publishOutcome({ ok: true, status: 200, body: { status: "published" } }, "https://acme.jomform.com/contest");
  assert.equal(out.kind, "published");
  assert.ok(out.kind === "published" && out.hasUrl);
  assert.equal(out.kind === "published" && out.url, "https://acme.jomform.com/contest");
});

test("a live publish with no resolved URL says so instead of showing a dead link", () => {
  // The reported condition behind t_457c4d87: the API resolves no URL for an
  // account whose workspace_id is NULL. The publish still succeeded, so the
  // panel must NOT claim a link it does not have.
  const out = publishOutcome({ ok: true, status: 200, body: {} }, "");
  assert.equal(out.kind, "published");
  assert.equal(out.kind === "published" && out.hasUrl, false);
  assert.equal(out.kind === "published" && out.url, "");
});

test("an empty or non-https URL can never become an href", () => {
  // An <a href=""> navigates to the CURRENT page — the "View form reloads
  // /forms" defect. A relative path would land the merchant back inside the
  // dashboard. Neither may reach an anchor.
  for (const bad of ["", "   ", "/forms", "contest", "//acme.jomform.com/x", "http://acme.jomform.com/x", "javascript:alert(1)"]) {
    assert.equal(liveLink(bad), null, `${JSON.stringify(bad)} must not be a live link`);
  }
  assert.equal(liveLink("https://acme.jomform.com/contest"), "https://acme.jomform.com/contest");
  // Trailing whitespace is trimmed, not rejected.
  assert.equal(liveLink("  https://acme.jomform.com/x  "), "https://acme.jomform.com/x");
});

test("the API's no-products refusal is classified as needs_ack, not a generic error", () => {
  // The merchant cannot act on a generic "failed" — the whole point is that the
  // message tells them to add a product or confirm.
  const warn = "Borang bayaran ini tiada produk — ia tidak boleh menerima bayaran.";
  const out = publishOutcome(
    { ok: false, status: 409, body: { error: warn, code: PUBLISH_NEEDS_ACK, requires_ack: true } },
    "",
  );
  assert.equal(out.kind, "needs_ack");
  assert.equal(out.kind === "needs_ack" && out.message, warn);
});

test("a 409 with a different code is NOT swallowed as the warning", () => {
  // Any other conflict must stay an error: treating it as "publish anyway"
  // would let the UI offer a confirm button that retries a request the server
  // refused for a different reason.
  const out = publishOutcome({ ok: false, status: 409, body: { error: "nope", code: "something_else" } }, "");
  assert.equal(out.kind, "error");
  assert.equal(out.kind === "error" && out.message, "nope");
  // And the code alone is not enough — the status must be 409 too.
  const wrongStatus = publishOutcome({ ok: false, status: 400, body: { code: PUBLISH_NEEDS_ACK } }, "");
  assert.equal(wrongStatus.kind, "error");
});

test("an error with no body still yields a usable outcome", () => {
  const out = publishOutcome({ ok: false, status: 500 }, "");
  assert.equal(out.kind, "error");
  assert.equal(out.kind === "error" && out.message, "");
});

test("the acknowledgement body carries exactly the one field", () => {
  // A retry must not re-send a stale copy of anything else.
  assert.deepEqual(acknowledgmentBody(), { acknowledge: true });
  assert.deepEqual(Object.keys(acknowledgmentBody()), ["acknowledge"]);
});

test("WEB/GO CONTRACT: PUBLISH_NEEDS_ACK equals the Go constant", () => {
  // A rename on either side would silently turn the actionable warning into an
  // opaque error, which is exactly the failure this card exists to fix.
  const m = goPublishReady.match(/PublishWarningNoProducts\s*=\s*"([a-z_]+)"/);
  assert.ok(m, "dbstore.PublishWarningNoProducts not found — the Go rule moved or was renamed");
  assert.equal(PUBLISH_NEEDS_ACK, m[1]);
});

test("outcomeMessage yields the failure text and nothing for a success", () => {
  // Callers use `outcomeMessage(o) || fallback`. If this returned a message for
  // a published outcome the fallback would never apply; if it returned "" for a
  // failure the user would see a bare fallback instead of the server's reason.
  const live = publishOutcome({ ok: true, status: 200 }, "https://a.jomform.com/x");
  assert.equal(outcomeMessage(live), "");
  const warn = publishOutcome({ ok: false, status: 409, body: { error: "no", code: PUBLISH_NEEDS_ACK } }, "");
  assert.equal(outcomeMessage(warn), "no");
  const err = publishOutcome({ ok: false, status: 500, body: { error: "boom" } }, "");
  assert.equal(outcomeMessage(err), "boom");
});

test("needsProductsIDs reads the bulk route's withheld ids", () => {
  // The bulk 409 names the ids it held back. A malformed body must degrade to
  // "no ids" (the caller then shows the server's prose), never throw.
  assert.deepEqual(needsProductsIDs({ needs_products: ["a", "b"] }), ["a", "b"]);
  assert.deepEqual(needsProductsIDs({ needs_products: ["a", 7, null] }), ["a"]);
  for (const bad of [null, undefined, {}, { needs_products: "a" }, { needs_products: {} }, 42]) {
    assert.deepEqual(needsProductsIDs(bad), [], `${JSON.stringify(bad)} must yield no ids`);
  }
});

test("the Forms page confirms and re-sends the bulk publish acknowledged", () => {
  // A bulk publish is the third way a form goes live; without this the merchant
  // would get a bare failure with no way to proceed.
  assert.match(formsPage, /t\("forms\.bulkNoProductsConfirm",\s*\{\s*n:\s*named\s*\}\)/);
  assert.match(formsPage, /acknowledgmentBody\(\)/);
  assert.match(formsPage, /needsProductsIDs\(d\)/);
});

test("WEB/GO CONTRACT: the two REST routes still gate on the Go rule", () => {
  // If a refactor drops the guard from a handler, the test above would keep
  // passing while the API went back to publishing these forms silently.
  //
  // The gate is the TRANSITION rule (PublishNeedsAck), not the raw predicate:
  // both routes must ask only when the form is not already live, or the
  // dashboard's status control would demand an acknowledgement on every save.
  assert.match(goRestHandlers, /dbstore\.PublishNeedsAck\(f, dbstore\.FormStatusPublished\)/, "publishForm no longer consults the rule");
  assert.match(goRestHandlers, /dbstore\.PublishNeedsAck\(f, in\.Status\)/, "setFormStatus no longer consults the rule");
  assert.match(goRestHandlers, /dbstore\.PublishWarningNoProducts/, "the 409 body no longer carries the code the dashboard matches on");
  // The bulk path is a publish too, and must not be the way around the guard.
  assert.match(goRestHandlers, /PublishNeedsAckIDs/, "bulkForms no longer consults the rule");
});

test("the builder renders the live panel with an open and a copy action", () => {
  // The builder is where the merchant pressed Publish, so it is where the
  // confirmation has to appear. Assert the call sites, not just the module.
  assert.match(builderPage, /import\s*\{[^}]*publishOutcome[^}]*\}\s*from\s*["']\.\.\/\.\.\/lib\/publishResult["']/);
  // EXACTLY two places raise the live panel: the ordinary save/publish path and
  // the "Publish anyway" retry. A count (not a presence test) is what makes this
  // bite — dropping either one leaves the merchant without the confirmation at
  // that moment, which IS the defect this card fixes.
  const liveCalls = builderPage.match(/setPublishNotice\(\{\s*kind:\s*"live"/g) ?? [];
  assert.equal(liveCalls.length, 2, `the live panel is raised in ${liveCalls.length} place(s), want 2 (publish + publish anyway)`);
  // The panel's own controls, each rendered from its translated key.
  assert.match(builderPage, />\s*\{t\("bld\.openForm"\)\}\s*</, "no open action rendered");
  assert.match(builderPage, /copyPublishUrl/, "no copy action");
  assert.match(builderPage, /\{copied \? t\("bld\.urlCopied"\) : t\("bld\.copyUrl"\)\}/, "the copy button never shows its copied/todo label");
  assert.match(builderPage, /\{t\("bld\.liveNoUrl"\)\}/, "no honest fallback when the URL is unresolved");
  // The anchor must be built from liveLink, never from the raw value.
  assert.equal((builderPage.match(/href=\{liveLink\(publishNotice\.url\) as string\}/g) ?? []).length, 2);
  assert.doesNotMatch(builderPage, /href=\{publishNotice\.url\}/, "an unchecked URL reached an href");
});

test("both anchors in the live panel go through liveLink, so neither can be empty", () => {
  // An <a href=""> reloads the current page — the defect t_457c4d87 tracks. THIS
  // panel must be structurally incapable of producing one, including the
  // bare-URL link beside the buttons (it is a second anchor).
  const guarded = builderPage.match(/href=\{liveLink\([^)]*\) as string\}/g) ?? [];
  assert.equal(guarded.length, 2, `only ${guarded.length} guarded anchor(s) in the publish panel, want 2 (Open form + the URL itself)`);
  // The pre-existing "View ↗" anchor takes `pubUrl` raw, which is safe ONLY
  // because it is rendered behind a truthiness guard — pin that guard, because
  // dropping it is exactly how an empty href gets in. (The link itself is
  // t_457c4d87's territory; this only stops the panel's shape regressing.)
  assert.match(
    builderPage,
    /\{controls\.view && pubUrl && \(\s*<a\s+href=\{pubUrl\}/,
    "the View anchor lost its `&& pubUrl &&` guard, so it can render href=\"\"",
  );
  // And no OTHER anchor takes a bare URL variable without such a guard.
  const rawHrefs = builderPage.match(/href=\{(?:url|form\.url|publishNotice\.url)\}/g) ?? [];
  assert.deepEqual(rawHrefs, [], "an unguarded URL variable reached an href");
});

test("the 'anyone with this link' hint is only shown when there IS a link", () => {
  // Caught by the render proof, not by reasoning: with no resolvable URL the
  // panel said "its public URL is not available yet" AND "anyone who opens this
  // link can use your form" in the same box. The hint must be gated on the URL.
  assert.match(
    builderPage,
    /\{publishNotice\.kind === "live" && Boolean\(publishNotice\.url\) && \(\s*<p[^>]*>\{t\("bld\.liveHint"\)\}<\/p>/,
    "the live hint is not gated on having a URL, so it contradicts the no-URL message",
  );
  assert.equal(
    (builderPage.match(/t\("bld\.liveHint"\)/g) ?? []).length,
    1,
    "the hint is rendered from more than one place — one of them is ungated",
  );
});

test("the builder offers the warn-then-confirm path, and re-sends acknowledged", () => {
  assert.match(builderPage, /t\("bld\.publishAnyway"\)/);
  assert.match(builderPage, /publishAnyway/, "no confirm handler");
  // The status write must be acknowledged too, or the retry is refused a second
  // time by the OTHER route (the status route is what the dashboard reaches).
  assert.match(builderPage, /status:\s*"published",\s*acknowledge:\s*true/);
  assert.match(builderPage, /acknowledgmentBody\(\)/);
});

test("the status route's refusal is not silently discarded by the other two pages", () => {
  // Both pages used to `return` on any non-ok status, so a merchant whose form
  // was refused saw only "failed" with no way forward.
  assert.match(settingsPage, /publishOutcome\(\{ ok: false, status: sr\.status, body: d \}/);
  assert.match(settingsPage, /acknowledgmentBody\(\)/);
  assert.match(formsPage, /publishOutcome\(\{ ok: false, status: r\.status, body: d \}/);
  assert.match(formsPage, /acknowledgmentBody\(\)/);
});

test("every string the live panel and the warning use exists in all five locales", () => {
  // The dictionary itself is guarded by i18n.test.ts; this pins the KEYS the
  // panel references, because a key that exists in neither place renders as the
  // raw key text to the user.
  const keys = [
    "bld.liveTitle",
    "bld.liveHint",
    "bld.liveNoUrl",
    "bld.openForm",
    "bld.copyUrl",
    "bld.urlCopied",
    "bld.publishAnyway",
    "bld.noProductsWarn",
    "bld.dismiss",
  ];
  for (const k of keys) {
    const line = i18nDict.split("\n").find((l) => l.includes(`"${k}":`));
    assert.ok(line, `i18nDict.ts has no entry for "${k}"`);
    for (const lang of ["en", "ms", "zh", "ta", "ar"]) {
      assert.match(line as string, new RegExp(`\\b${lang}:\\s*"`), `"${k}" is missing ${lang}`);
    }
    assert.match(builderPage, new RegExp(`t\\("${k.replace(".", "\\.")}"\\)`), `"${k}" is never rendered`);
  }
});
