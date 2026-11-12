// Onboarding-gap guard: the dashboard must actually SHOW the merchant that the
// form is live, must not be able to render a dead "View" link, and must NEVER
// print the API's own prose for a refusal.
//
// THE GAP (drop-off, not a crash). Three outside signups built a form, saved it
// as a draft and never published. Nothing failed — the publish handler answers
// 200 — and after a successful publish the builder showed no confirmation, no
// URL and no way to discover the form was on the internet: the "View ↗" link is
// rendered only when a URL already resolved, so a FIRST publish produced nothing
// visible at all.
//
// THE SECOND DEFECT (t_1d96a18f, the same class as issue #6177). The publish
// refusal's sentence was taken from the response —
// `window.confirm(outcome.message || t("bld.noProductsWarn"))` — and the API's
// `error` for it is a MALAY literal it never localizes (the server does not
// receive the caller's language on a JSON call). So a merchant on an English
// dashboard read:
//
//   Borang bayaran ini tiada produk — ia tidak boleh menerima bayaran. …
//
// #6177's source rule bans `body.error || fallback`, which is why it did not see
// this: the prose arrived through `outcome.message`, a field
// lib/publishResult.ts filled from the response. The fix is structural — the
// outcome for that refusal carries NO message — so this file guards the SHAPE,
// not one call site, and asserts the rendered sentence per locale.
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
import { readFileSync, readdirSync, statSync } from "node:fs";
import { fileURLToPath } from "node:url";

import {
  PUBLISH_NEEDS_ACK,
  PUBLISH_NEEDS_ACK_KEY,
  PUBLISH_NO_PRODUCT_SELECTED,
  PUBLISH_NO_PRODUCT_SELECTED_KEY,
  acknowledgmentBody,
  liveLink,
  needsProductsIDs,
  publishConfirmText,
  publishErrorText,
  publishOutcome,
} from "./publishResult.ts";
import { DICT, LANGS } from "../components/i18nDict.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

const builderPage = read("../app/builder/page.tsx");
const formsPage = read("../app/forms/page.tsx");
const settingsPage = read("../app/form-settings/page.tsx");
const i18nDict = read("../components/i18nDict.ts");
// The Go rule this mirrors — read from source so the two cannot drift apart
// silently (a mirror that disagrees is a second, wrong rule).
const goPublishReady = read("../../../api/internal/dbstore/form_publish_ready.go");
const goRestHandlers = read("../../../api/internal/httpapi/rest_handlers.go");

/**
 * The API's own prose for the no-products refusal, read out of the Go source
 * (`writePublishNeedsAck`) rather than restated here — the assertion below is
 * about THAT string never reaching a merchant, so it must be the real one.
 */
function goNoProductsProse(): string {
  const m = goRestHandlers.match(
    /func writePublishNeedsAck[\s\S]*?out\s*:=\s*map\[string\]any\{\s*"error":\s*((?:"[^"]*"\s*\+?\s*)+)/,
  );
  assert.ok(m, "writePublishNeedsAck's prose was not found in rest_handlers.go — the Go rule moved");
  const parts = [...m[1].matchAll(/"([^"]*)"/g)].map((x) => x[1]);
  return parts.join("");
}

/** The dictionary's rendering of a key in one locale (English is the fallback). */
function dict(lang: string, key: string): string {
  const entry = (DICT as Record<string, Record<string, unknown>>)[key];
  assert.ok(entry, `missing dictionary key ${key}`);
  const v = entry[lang];
  return typeof v === "string" && v !== "" ? v : (entry.en as string);
}

/** Every source file under src/ (excluding tests). */
function walk(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    if (name === "node_modules" || name === ".next") continue;
    const p = `${dir}/${name}`;
    if (statSync(p).isDirectory()) walk(p, out);
    else if (name.endsWith(".tsx") || name.endsWith(".ts")) out.push(p);
  }
  return out;
}

const SRC = fileURLToPath(new URL("..", import.meta.url));
const sources = walk(SRC);

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

test("an empty or non-absolute URL can never become an href, and a self-host http origin can", () => {
  // An <a href=""> navigates to the CURRENT page — the "View form reloads
  // /forms" defect. A relative path would land the merchant back inside the
  // dashboard. Neither may reach an anchor.
  for (const bad of ["", "   ", "/forms", "contest", "//acme.jomform.com/x", "javascript:alert(1)", "http://", "http:///path"]) {
    assert.equal(liveLink(bad), null, `${JSON.stringify(bad)} must not be a live link`);
  }
  assert.equal(liveLink("https://acme.jomform.com/contest"), "https://acme.jomform.com/contest");
  // Trailing whitespace is trimmed, not rejected.
  assert.equal(liveLink("  https://acme.jomform.com/x  "), "https://acme.jomform.com/x");

  // SELF-HOST (card t_6c0badbe): the published form lives on the operator's own
  // origin, and APP_BASE_URL is legitimately http on a local trial
  // (docs/SELF-HOST.md: --base-url http://localhost:8080). The API answers
  // exactly this shape, and https-only made the panel say "its public URL is
  // not available yet" while that URL served the form — no Copy link at all.
  for (const own of ["http://localhost:18099/f/browser-pub", "http://127.0.0.1:8080/f/slug", "https://forms.example.com/f/slug"]) {
    assert.equal(liveLink(own), own, `${own} is the install's own working address and must be linkable`);
  }
});

test("the API's no-products refusal is classified as needs_ack, not a generic error", () => {
  // The merchant cannot act on a generic "failed" — the whole point is that the
  // message tells them to add a product or confirm. And the outcome must NOT
  // carry the server's prose (the next test is the reason).
  const out = publishOutcome(
    {
      ok: false,
      status: 409,
      body: { error: goNoProductsProse(), code: PUBLISH_NEEDS_ACK, requires_ack: true },
    },
    "",
  );
  assert.equal(out.kind, "needs_ack");
  // The whole fix in one assertion: there is no prose field to render.
  assert.equal(
    "message" in out,
    false,
    `the needs_ack outcome carries a message again (${JSON.stringify(out)}) — that field is where the API's Malay prose reached the dashboard`,
  );
  // Classification is on the machine code, not the wording: a reworded prose
  // still lands in the actionable branch.
  assert.equal(publishOutcome({ ok: false, status: 409, body: { code: PUBLISH_NEEDS_ACK } }, "").kind, "needs_ack");
});

test("THE DEFECT: the refusal renders in the dashboard's language, never the API's prose", () => {
  // The reported case, end to end: the response carries the real Go prose, and
  // what a merchant gets per locale is asserted — all five, so a missing
  // translation shows as the English sentence rather than as Malay.
  const body = { error: goNoProductsProse(), code: PUBLISH_NEEDS_ACK, requires_ack: true };
  const prose = goNoProductsProse();
  // The fixture must really be the Malay string, or this test proves nothing.
  assert.match(prose, /This payment form has no products/, "the Go prose is not the refusal this test was written for");

  const outcome = publishOutcome({ ok: false, status: 409, body }, "");
  // `Translate` is now apiErr's translator for ANY i18n key (see the module), so
  // the fixture takes a key and reads the dictionary — the same shape every page
  // passes as `t`.
  const tIn = (lang: string) => (k: string) => dict(lang, k);

  const en = publishConfirmText(outcome, tIn("en"));
  assert.match(en, /payment form with no products/i, `lang=en rendered ${JSON.stringify(en)}`);
  assert.ok(!en.includes(prose), "lang=en rendered the server's prose");

  for (const lang of LANGS) {
    const rendered = publishConfirmText(outcome, tIn(lang));
    assert.ok(rendered.trim() !== "", `lang=${lang} rendered nothing`);
    assert.ok(
      !rendered.includes(prose),
      `lang=${lang} rendered the API's wire prose: ${JSON.stringify(rendered)}`,
    );
    // The English fallback is legitimate for an untranslated locale, Malay is
    // never legitimate for any locale.
    assert.equal(rendered, dict(lang, PUBLISH_NEEDS_ACK_KEY), `lang=${lang} is not the dictionary sentence`);
  }
  // Spot-check the two languages the report names, so a dictionary entry that
  // quietly became English is caught rather than merely "not the prose".
  assert.match(publishConfirmText(outcome, tIn("ms")), /Ini borang bayaran tanpa produk/);
  assert.match(publishConfirmText(outcome, tIn("ar")), /نموذج دفع بلا منتجات/);

  // A non-refusal outcome has no confirm sentence at all, so a caller cannot
  // use this as a generic fallback.
  assert.equal(publishConfirmText(publishOutcome({ ok: true, status: 200 }, ""), tIn("en")), "");
  assert.equal(publishConfirmText(publishOutcome({ ok: false, status: 500, body: { error: "boom" } }, ""), tIn("en")), "");
});

test("the no-products refusal does not offer to collect anything, and says nothing is collected (5 locales)", () => {
  // Card t_39906c0a. The sentence used to give a SECOND, legitimate-sounding
  // reason to confirm — "publish it anyway if you only meant to collect
  // details" — for a form that collects nothing at all:
  //
  //   * legacy layout (no blocks): the submit handler is
  //     `if (!items.length) { alert(NoProductAlert); return; }` and there is
  //     nothing to tick, so nothing is ever posted;
  //   * block layout (inputs, no sellable item): the SAME gate in blockTmpl;
  //   * a published payment form with no blocks and no products serves the
  //     submit-less "not accepting orders" page (t_7d4c20c8).
  //
  // So a merchant who acknowledged the warning BECAUSE of that clause got a form
  // that collects nobody's name or email. The copy must not promise a way to
  // collect, and must say what a buyer gets instead.
  //
  // This is a COPY guard and claims nothing more: it pins the defect's own
  // phrases per locale plus the honest statement in English. It cannot judge
  // translation quality — the same limit src/lib/i18n.test.ts states about the
  // dictionary — so a fluent review stays the only check on the wording itself.
  const promiseOffered: Record<string, RegExp> = {
    // "if you only meant to collect details" and its siblings: a reason to
    // publish that the platform does not honour.
    en: /\bcollect\s+(?:details|answers?|responses?|data)\b/i,
    ms: /(?:mengumpul|kumpul)\s+maklumat/i,
    zh: /收集(?:信息|资料|详情)/,
    ta: /விவரங்களை/,
    ar: /جمع البيانات/,
  };
  for (const lang of LANGS) {
    const sentence = dict(lang, PUBLISH_NEEDS_ACK_KEY);
    assert.ok(
      !promiseOffered[lang].test(sentence),
      `lang=${lang} still offers publishing-anyway as a way to collect: ${JSON.stringify(sentence)}`,
    );
  }
  // The other half: it must actually tell the merchant that acknowledging gains
  // them nothing until a product exists (a reword that silently drops this is
  // the same lie in a different shape).
  const en = dict("en", PUBLISH_NEEDS_ACK_KEY);
  assert.match(en, /collects nothing/i, `the English refusal dropped "collects nothing": ${JSON.stringify(en)}`);
  assert.match(en, /cannot submit/i, `the English refusal dropped what the buyer hits: ${JSON.stringify(en)}`);
  // And this is the sentence the pages render (not a copy kept beside it).
  assert.equal(
    publishConfirmText({ kind: "needs_ack", code: PUBLISH_NEEDS_ACK, blockId: "" }, (k) => dict("en", k)),
    en,
  );
  // The agent/curl/MCP surface reads the GO prose, not the dictionary — it must
  // not read as "confirm and the form is usable" either. Read from source, so a
  // revert of the Go sentence reddens here.
  const goProse = goNoProductsProse();
  assert.match(
    goProse,
    /will not collect anything/,
    `the Go refusal does not say the form collects nothing: ${JSON.stringify(goProse)}`,
  );
});

test("NO source file reads a message off a publish outcome", () => {
  // The structural half of the fix. #6177's rule bans `body.error || fallback`
  // and could not see this defect because the prose travelled through
  // `outcome.message` / `publishNotice.message`. Both names are banned here, and
  // the walk is asserted non-empty so it cannot pass vacuously.
  assert.ok(sources.length > 40, `expected to walk the whole dashboard, got ${sources.length} source files`);
  assert.ok(
    sources.some((p) => p.endsWith("app/form-settings/page.tsx")),
    "form-settings/page.tsx (the reported call site) is not in the walked set",
  );

  const banned = [/(?<![\w$])outcome\s*\??\.\s*message\b/, /kind:\s*"needsAck",\s*message/];
  const offenders: string[] = [];
  for (const p of sources) {
    const rel = p.slice(SRC.length);
    if (rel === "lib/publishResult.ts" || rel.endsWith(".test.ts")) continue; // the module + this file quote the shape
    readFileSync(p, "utf8")
      .split("\n")
      .forEach((line, i) => {
        if (/^\s*(\/\/|\*|\/\*)/.test(line)) return; // a comment quoting the bug
        if (banned.some((re) => re.test(line))) offenders.push(`${rel}:${i + 1}: ${line.trim()}`);
      });
  }
  assert.deepEqual(
    offenders,
    [],
    `a publish outcome's message is the API's unlocalized prose — render the refusal from ` +
      `PUBLISH_NEEDS_ACK_KEY (publishConfirmText) instead:\n  ` +
      offenders.join("\n  "),
  );

  // And the fix must be really in place at the reported call site, not merely
  // absent: form-settings renders the refusal through the module.
  assert.match(
    settingsPage,
    /window\.confirm\(publishConfirmText\(outcome,\s*t\)\)/,
    "the reported call site no longer renders the refusal from the dictionary",
  );
});

test("a 409 with a different code is NOT swallowed as the warning", () => {
  // Any other conflict must stay an error: treating it as "publish anyway"
  // would let the UI offer a confirm button that retries a request the server
  // refused for a different reason.
  const out = publishOutcome({ ok: false, status: 409, body: { error: "nope", code: "something_else" } }, "");
  assert.equal(out.kind, "error");
  // The body is carried for the code lookup, but the PROSE in it is never what
  // a caller renders: an unknown code lands on the caller's own fallback.
  assert.equal(out.kind === "error" && (out.body as { error?: string }).error, "nope");
  assert.equal(publishErrorText(out, "Localized fallback.", () => "T"), "Localized fallback.");
  // And the code alone is not enough — the status must be 409 too.
  const wrongStatus = publishOutcome({ ok: false, status: 400, body: { code: PUBLISH_NEEDS_ACK } }, "");
  assert.equal(wrongStatus.kind, "error");
});

test("an error with no body still yields a usable outcome", () => {
  const out = publishOutcome({ ok: false, status: 500 }, "");
  assert.equal(out.kind, "error");
  assert.equal(publishErrorText(out, "Localized fallback.", () => "T"), "Localized fallback.");
});

test("the acknowledgement body carries exactly the one field", () => {
  // A retry must not re-send a stale copy of anything else.
  assert.deepEqual(acknowledgmentBody(), { acknowledge: true });
  assert.deepEqual(Object.keys(acknowledgmentBody()), ["acknowledge"]);
});

test("WEB/GO CONTRACT: PUBLISH_NEEDS_ACK equals the Go constant", () => {
  // A rename on either side would silently turn the actionable warning into an
  // opaque error, which is exactly the failure this module exists to fix.
  const m = goPublishReady.match(/PublishWarningNoProducts\s*=\s*"([a-z_]+)"/);
  assert.ok(m, "dbstore.PublishWarningNoProducts not found — the Go rule moved or was renamed");
  assert.equal(PUBLISH_NEEDS_ACK, m[1]);
  // Card t_983425e5: the second refusal is a separate code, and the same drift
  // would put the merchant back on a generic error.
  const sel = goPublishReady.match(/PublishWarningNoProductSelected\s*=\s*"([a-z_]+)"/);
  assert.ok(sel, "dbstore.PublishWarningNoProductSelected not found — the Go rule moved or was renamed");
  assert.equal(PUBLISH_NO_PRODUCT_SELECTED, sel[1]);
});

test("a product block with no product selected is its own actionable refusal", () => {
  // The prod shape: `{"type":"product","value":""}` with no product_id. The API
  // answers 409 with `code:no_product_selected` and the block id, and the builder
  // must show the sentence that quotes the BUYER ("This form is not accepting
  // orders right now") — not the no-products sentence, which would send the
  // merchant looking for a product they already created.
  const out = publishOutcome(
    {
      ok: false,
      status: 409,
      body: { error: "This payment form has a product block with no product selected", code: PUBLISH_NO_PRODUCT_SELECTED, requires_ack: true, block_id: "q5uhvoy" },
    },
    "",
  );
  assert.equal(out.kind, "needs_ack");
  assert.equal(out.kind === "needs_ack" && out.code, PUBLISH_NO_PRODUCT_SELECTED);
  assert.equal(out.kind === "needs_ack" && out.blockId, "q5uhvoy", "the refusal's block id was dropped, so the link has no target");
  assert.equal("message" in out, false, "the refusal carries prose again");

  const tIn = (lang: string) => (k: string) => dict(lang, k);
  const en = publishConfirmText(out, tIn("en"));
  assert.match(en, /not accepting orders right now/i, `the English refusal does not quote the buyer: ${JSON.stringify(en)}`);
  for (const lang of LANGS) {
    assert.equal(publishConfirmText(out, tIn(lang)), dict(lang, PUBLISH_NO_PRODUCT_SELECTED_KEY), `lang=${lang} is not the dictionary sentence`);
  }
  // And the OLD code still renders the OLD sentence — the two must not collapse.
  const old = publishOutcome({ ok: false, status: 409, body: { code: PUBLISH_NEEDS_ACK } }, "");
  assert.equal(old.kind === "needs_ack" && old.code, PUBLISH_NEEDS_ACK);
  assert.equal(old.kind === "needs_ack" && old.blockId, "");
  assert.equal(publishConfirmText(old, tIn("en")), dict("en", PUBLISH_NEEDS_ACK_KEY));
});

test("publishErrorText yields the localized failure sentence, and nothing for a refusal or a success", () => {
  // Callers use `publishErrorText(o, t("your.fallback"), t)`. A published outcome
  // must return "" so nothing renders it; a `needs_ack` outcome must ALSO return
  // "" — its sentence comes from the dictionary, and a caller that took it from
  // here is how the Malay prose reached the dashboard.
  //
  // The error case is the t_10453dd9 defect: the sentence is the machine code's
  // translation or the caller's OWN fallback, and NEVER the body's prose.
  const live = publishOutcome({ ok: true, status: 200 }, "https://a.jomform.com/x");
  assert.equal(publishErrorText(live, "F", () => "T"), "");
  const warn = publishOutcome({ ok: false, status: 409, body: { error: "no", code: PUBLISH_NEEDS_ACK } }, "");
  assert.equal(publishErrorText(warn, "F", () => "T"), "", "the refusal's text must not be reachable through publishErrorText");

  // An UNCODED failure — the reported case. The API's prose is on the body and
  // the sentence is the caller's fallback.
  const uncoded = publishOutcome({ ok: false, status: 409, body: { error: "penerbitan gagal atas sebab dalaman pada pelayan" } }, "");
  assert.equal(uncoded.kind, "error");
  assert.equal("message" in uncoded, false, `the error outcome carries a message again (${JSON.stringify(uncoded)}) — that field is where the API's prose reached the dashboard`);
  const rendered = publishErrorText(uncoded, "Failed to set the form status.", () => "T");
  assert.equal(rendered, "Failed to set the form status.");
  assert.ok(!rendered.includes("penerbitan gagal"), "the server's Malay prose was rendered");

  // A KNOWN code wins over the fallback, translated.
  const known = publishOutcome({ ok: false, status: 404, body: { error: "workspace not found", code: "workspace.notFound" } }, "");
  const viaDict = (lang: string) => (k: string) => dict(lang, k);
  const en = publishErrorText(known, "Failed to set the form status.", viaDict("en"));
  assert.notEqual(en, "Failed to set the form status.", "a known code did not translate");
  assert.ok(!en.includes("workspace not found"), `lang=en rendered the server's Malay prose: ${JSON.stringify(en)}`);
  assert.match(en, /workspace was not found/i);
});

test("needsProductsIDs reads the bulk route's withheld ids", () => {
  // The bulk 409 names the ids it held back. A malformed body must degrade to
  // "no ids" (the caller then shows the localized warning), never throw.
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
  // Its unnamed fallback is localized too — that branch used to read the prose.
  assert.match(formsPage, /:\s*publishConfirmText\(outcome,\s*t\)/);
});

test("WEB/GO CONTRACT: the two REST routes still gate on the Go rule", () => {
  // If a refactor drops the guard from a handler, the test above would keep
  // passing while the API went back to publishing these forms silently.
  //
  // The gate is the TRANSITION rule (PublishNeedsAck), not the raw predicate:
  // both routes must ask only when the form is not already live, or the
  // dashboard's status control would demand an acknowledgement on every save.
  assert.match(goRestHandlers, /dbstore\.PublishWarning\(f, dbstore\.FormStatusPublished,/, "publishForm no longer consults the rule");
  assert.match(goRestHandlers, /dbstore\.PublishWarning\(f, in\.Status,/, "setFormStatus no longer consults the rule");
  assert.match(goRestHandlers, /dbstore\.PublishWarningNoProducts/, "the 409 body no longer carries the code the dashboard matches on");
  // Card t_983425e5: the refusal names the product block it is about, and the
  // list/row payloads carry the same dead-form signal the badge reads.
  assert.match(goRestHandlers, /dbstore\.PublishWarningNoProductSelected/, "the 409 body cannot report an unpointed product block");
  assert.match(goRestHandlers, /dbstore\.NotAcceptingOrders\(f, live\)/, "the forms list does not ask the shared dead-form rule");
  // The bulk path is a publish too, and must not be the way around the guard.
  assert.match(goRestHandlers, /PublishNeedsAckIDs/, "bulkForms no longer consults the rule");
});

test("the builder renders the live panel with an open and a copy action", () => {
  // The builder is where the merchant pressed Publish, so it is where the
  // confirmation has to appear. Assert the call sites, not just the module.
  assert.match(builderPage, /import\s*\{[^}]*publishOutcome[^}]*\}\s*from\s*["']\.\.\/\.\.\/lib\/publishResult["']/s);
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

test("the builder's warning panel renders the localized refusal from the key", () => {
  // The builder is the other surface that prints this refusal, and it had the
  // same shape (`publishNotice.message || t("bld.noProductsWarn")`). It now
  // renders the dictionary key off a message-less notice — and, since card
  // t_983425e5, picks WHICH key from the refusal's machine code so an unpointed
  // product block does not read as "this form has no products".
  assert.match(
    builderPage,
    /publishNotice\.kind === "needsAck"[\s\S]{0,260}PUBLISH_NO_PRODUCT_SELECTED[\s\S]{0,200}PUBLISH_NO_PRODUCT_SELECTED_KEY[\s\S]{0,200}PUBLISH_NEEDS_ACK_KEY/,
    "the builder's warning text is not rendered from the dictionary key picked by the refusal code",
  );
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
    "bld.noProductSelectedWarn",
    "bld.chooseProduct",
    "bld.goToProductBlock",
    "bld.dismiss",
  ];
  for (const k of keys) {
    const line = i18nDict.split("\n").find((l) => l.includes(`"${k}":`));
    assert.ok(line, `i18nDict.ts has no entry for "${k}"`);
    for (const lang of ["en", "ms", "zh", "ta", "ar"]) {
      assert.match(line as string, new RegExp(`\\b${lang}:\\s*"`), `"${k}" is missing ${lang}`);
    }
  }
  // Every key except the refusal itself is rendered literally by its own name.
  for (const k of keys.filter((k) => k !== "bld.noProductsWarn")) {
    assert.match(builderPage, new RegExp(`t\\("${k.replace(".", "\\.")}"\\)`), `"${k}" is never rendered`);
  }
  // The refusals are rendered through the constants, so their names live in ONE
  // place (lib/publishResult.ts) and a rename cannot split them from the Go
  // codes. Card t_983425e5 added the second refusal.
  assert.match(builderPage, /PUBLISH_NEEDS_ACK_KEY/, `"bld.noProductsWarn" is never rendered`);
  assert.match(builderPage, /PUBLISH_NO_PRODUCT_SELECTED_KEY/, `"bld.noProductSelectedWarn" is never rendered`);
  assert.match(i18nDict, new RegExp(`"${PUBLISH_NEEDS_ACK_KEY}":\\s*\\{`), "PUBLISH_NEEDS_ACK_KEY does not name a dictionary entry");
  assert.match(i18nDict, new RegExp(`"${PUBLISH_NO_PRODUCT_SELECTED_KEY}":\\s*\\{`), "PUBLISH_NO_PRODUCT_SELECTED_KEY does not name a dictionary entry");
});
