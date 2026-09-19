// Regression test — the /forms list must not label a non-sales form
// "Sales".
//
// THE BUG (reported by Wan): every row on https://jomform.com/forms carried a
// "Sales" button. His example was an RSVP form, which is not a sales form at
// all, so the term is wrong for that row. The page drew the action
// unconditionally for every form.
//
// THE FIX: the API now surfaces the form's collection mode (restForm.Mode),
// and this module turns it into the display decision. The Sales action is
// rendered only for a form that can actually take payment.
//
// This repo has no JSX/component test runner (no vitest/jest — see
// src/lib/i18n.test.ts, src/lib/adminGrowth.test.ts), so the decision is tested
// on the real function the page calls, and the page's call sites are asserted
// from source so the fix cannot be dead code behind a green suite.
//
// Run with:
//
// npm run test:unit
// (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import {
  FORM_MODE_PAYMENT,
  FORM_MODE_RESPONSE,
  defaultNotificationEvent,
  formMode,
  isSalesForm,
  salesFormHref,
  formEntriesAction,
} from "./formMode.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

const formsPage = read("../app/forms/page.tsx");
const settingsPage = read("../app/form-settings/page.tsx");
const i18nDict = read("../components/i18nDict.ts");
// The Go rule this mirrors — read from source so the two cannot drift apart
// silently (a mirror that disagrees is a second, wrong rule).
const goFormMode = read("../../../api/internal/dbstore/form_mode.go");

test("the reported case: an RSVP (response) form is not a sales form", () => {
  const rsvp = { id: "f1", title: "RSVP", mode: "response" };
  assert.equal(formMode(rsvp), FORM_MODE_RESPONSE);
  assert.equal(isSalesForm(rsvp), false);
  assert.equal(
    salesFormHref(rsvp, "f1"),
    null,
    "a response-only form must not get a Sales link — that is the reported bug",
  );
});

test("a payment form is a sales form and keeps its Sales link", () => {
  const order = { id: "f2", title: "Order", mode: "payment" };
  assert.equal(formMode(order), FORM_MODE_PAYMENT);
  assert.equal(isSalesForm(order), true);
  assert.equal(salesFormHref(order, "f2"), "/sales?form=f2");
});

test("a LEGACY form with no mode at all is a sales form", () => {
  // The load-bearing default. Every pre-#171 form has no `mode` key, and in
  // production they are the forms that actually have sales. Treating "unknown"
  // as response would HIDE the Sales action on real sales forms — the worse
  // failure. This is the case a naive implementation gets wrong.
  for (const legacy of [{ id: "f3", title: "Legacy" }, { id: "f4", title: "X", mode: undefined }]) {
    assert.equal(formMode(legacy), FORM_MODE_PAYMENT);
    assert.equal(isSalesForm(legacy), true);
    assert.equal(salesFormHref(legacy, legacy.id), `/sales?form=${legacy.id}`);
  }
  assert.equal(formMode(null), FORM_MODE_PAYMENT);
  assert.equal(formMode(undefined), FORM_MODE_PAYMENT);
});

test("the mode falls back to config.mode for a pre-mode payload shape", () => {
  // During a rolling deploy an older API can still answer without a top-level
  // `mode`; the nested config has carried it since #171, so the page must not
  // flash a "Sales" button on an RSVP form in that window.
  assert.equal(formMode({ config: { mode: "response" } }), FORM_MODE_RESPONSE);
  assert.equal(isSalesForm({ config: { mode: "response" } }), false);
  assert.equal(formMode({ config: { mode: "payment" } }), FORM_MODE_PAYMENT);
});

test("the top-level mode wins over a stale nested one", () => {
  // The API response is authoritative; a config blob that disagrees must not
  // override it.
  assert.equal(formMode({ mode: "response", config: { mode: "payment" } }), FORM_MODE_RESPONSE);
  assert.equal(formMode({ mode: "payment", config: { mode: "response" } }), FORM_MODE_PAYMENT);
});

test("an unexpected mode value is treated as a sales form, never as response", () => {
  for (const bad of ["RESPONSE", "", "order", 1, {}, null]) {
    assert.equal(
      formMode({ mode: bad as unknown as string }),
      FORM_MODE_PAYMENT,
      `mode=${JSON.stringify(bad)} must not be read as a response form`,
    );
  }
});

test("no Sales link is built for a form with no id", () => {
  assert.equal(salesFormHref({ mode: "payment" }, ""), null);
});

// Issue: a response form had NO way into its entries.
//
// #602 (correctly) stopped labelling an RSVP "Sales" by gating the action on
// isSalesForm — and that removed the row's only link to its data, because #604
// had made /sales?form=<id> the one destination that renders a response form's
// submissions. The fix is the action for BOTH modes with different labels:
// Sales -> nav.sales, response -> nav.submissions, same URL.
test("a response form gets a Submission action to the same entries URL", () => {
  const rsvp = { id: "f1", title: "Event registration", mode: "response" };
  const action = formEntriesAction(rsvp, rsvp.id);
  assert.ok(action, "a response form must have an action — no link means its submissions are unreachable");
  assert.equal(action.href, "/sales?form=f1", "the destination is the same URL as a payment form's");
  assert.equal(action.labelKey, "nav.submissions");
  // The label must NOT be the sales one: that is the #602 defect coming back.
  assert.notEqual(action.labelKey, "nav.sales");
});

test("a payment form keeps the Sales action", () => {
  const order = { id: "f2", title: "Order", mode: "payment" };
  assert.deepEqual(formEntriesAction(order, order.id), {
    href: "/sales?form=f2",
    labelKey: "nav.sales",
  });
});

test("a LEGACY form with no mode still gets the Sales action", () => {
  // Same load-bearing default as formMode: the pre-#171 forms are the ones
  // that actually have sales in production.
  for (const legacy of [{ id: "f3" }, { id: "f4", mode: undefined }]) {
    assert.equal(formEntriesAction(legacy, legacy.id)?.labelKey, "nav.sales");
  }
  assert.equal(formEntriesAction(null, "f5")?.labelKey, "nav.sales");
});

test("no action is built for a form with no id", () => {
  assert.equal(formEntriesAction({ mode: "response" }, ""), null);
  assert.equal(formEntriesAction({ mode: "payment" }, ""), null);
});

test("the action's destinations differ ONLY by label, never by URL", () => {
  // If a future edit sends the two modes to different URLs, the submissions
  // view (#604, the answers layout on the sales page) stops being reachable
  // while every assertion above still passes.
  const asResponse = { id: "zz-1", mode: "response" };
  const asPayment = { id: "zz-1", mode: "payment" };
  const response = formEntriesAction(asResponse, "zz-1");
  const payment = formEntriesAction(asPayment, "zz-1");
  assert.equal(response?.href, payment?.href);
});

test("the /forms page offers the entries action on BOTH row branches, for both modes", () => {
  // The reported symptom: an action that renders for a payment form and not
  // for a response form. The page draws two row branches (published, and
  // draft/private), so a fix that converts only one leaves the bug live on the
  // other.
  //
  // One assertion per site, anchored on the Link tag plus its href expression —
  // a single loose match would be satisfied by whichever site still carries it.
  const sites = [...formsPage.matchAll(/\{entries && \(\s*<Link href=\{entries\.href\}/g)];
  assert.equal(sites.length, 2, `expected the entries action at both row branches on /forms; found ${sites.length}`);
  assert.equal(
    [...formsPage.matchAll(/\{t\(entries\.labelKey\)\}/g)].length,
    2,
    "both entries links must take their label from the mode, not a hardcoded key",
  );
  // No site may be gated on the sales predicate. This is the mutation check:
  // restoring `isSalesForm(f) && ...` drops the response form's action, which
  // is exactly the bug, and it fails here by name.
  assert.doesNotMatch(
    formsPage,
    /isSalesForm/,
    "/forms gates its entries action on isSalesForm again — a response form loses the link to its submissions",
  );
});

test("form-settings offers the entries action for both modes too", () => {
  // Same page-level gap one surface over: Form Settings drew the sales action
  // only, so a response form reached from its own settings page had no route
  // to its submissions either.
  assert.match(
    settingsPage,
    /\{entries && \(\s*<Link href=\{entries\.href\}/,
    "form-settings no longer renders a per-form entries action",
  );
  assert.match(settingsPage, /formEntriesAction\(form, formId\)/);
  assert.doesNotMatch(
    settingsPage,
    /isSalesForm/,
    "form-settings gates its entries action on isSalesForm again — a response form loses the link to its submissions",
  );
  assert.equal(
    [...settingsPage.matchAll(/href=\{entries\.href\}/g)].length,
    1,
    "expected exactly one entries link site on form-settings",
  );
});

test("nav.submissions is present in all five locales and is not the stat-card key", () => {
  // The button label. It must NOT reuse dash.submissions, which is
  // "Submissions:" (trailing colon, dashboard stat card) and would read wrong
  // as a button.
  const row = i18nDict.split("\n").find((l) => l.includes('"nav.submissions":'));
  assert.ok(row, "nav.submissions is missing from the dictionary");
  for (const lang of ["en", "ms", "zh", "ta", "ar"]) {
    assert.match(row as string, new RegExp(`\\b${lang}:\\s*"`), `nav.submissions has no ${lang} value`);
  }
  assert.match(row as string, /en:\s*"Submission"/, "the English label must be the singular \"Submission\"");
  assert.doesNotMatch(row as string, /en:\s*"Submissions/);
});

test("the mode field is declared on both page payload types", () => {
  // Without the field on the type the data never reaches isSalesForm.
  assert.match(formsPage, /mode\?:\s*string;/, "the /forms FormRow type lost its mode field");
  assert.match(settingsPage, /mode\?:\s*string;/, "the FormDetail type lost its mode field");
});

test("the JS rule mirrors the Go rule: only an explicit 'response' is a response form", () => {
  // The Go side is the authority the API serialises from. If either side's
  // default flips, the two disagree and one surface labels forms wrongly.
  assert.match(
    goFormMode,
    /ok && m == FormModeResponse \{\s*\n\s*return FormModeResponse/,
    "the Go rule no longer requires an explicit 'response'",
  );
  assert.match(
    goFormMode,
    /return FormModePayment\s*\n\}/,
    "the Go rule no longer defaults to payment — a legacy form would be mislabelled a response form",
  );
  assert.match(goFormMode, /FormModeResponse = "response"/);
  assert.match(goFormMode, /FormModePayment\s+= "payment"/);
});

test("a new notification rule starts on an event the form can actually fire", () => {
  // The reported defect (issue #597): a response form's only notification rule
  // was created on sale.paid, an event a response form can never produce, so it
  // existed, was enabled, and silently never fired. The editor opened a new rule
  // on sale.paid for every form — the same mistake, one click away.
  assert.equal(defaultNotificationEvent({ mode: "response" }), "response.submitted");
  assert.equal(defaultNotificationEvent({ mode: "payment" }), "sale.paid");
  // The load-bearing default: a legacy form with no mode key is a payment form
  // with real sales, so it must keep sale.paid.
  assert.equal(defaultNotificationEvent({}), "sale.paid");
  assert.equal(defaultNotificationEvent({ config: { blocks: [] } }), "sale.paid");
  assert.equal(defaultNotificationEvent({ config: { mode: "response" } }), "response.submitted");
  assert.equal(defaultNotificationEvent(null), "sale.paid");
  assert.equal(defaultNotificationEvent(undefined), "sale.paid");
});

test("the notification editor actually uses the mode-aware default", () => {
  // A correct helper the page never calls is dead code behind a green suite.
  assert.match(
    settingsPage,
    /setEdEvent\(defaultNotificationEvent\(form\)\)/,
    "the notification editor no longer derives a new rule's event from the form mode",
  );
  // And no hardcoded sale.paid seed may remain as the editor's starting event.
  assert.doesNotMatch(
    settingsPage,
    /useState<string>\("sale\.paid"\)/,
    "the notification editor's event state is seeded with sale.paid again, so a response form's new rule opens on an event it can never fire",
  );
});

test("the JS notification default mirrors the Go EnsureDefault rule", () => {
  // Two readers of the same rule that disagree is its own bug: the API decides
  // what a new form GETS, the editor decides what a new rule OPENS ON.
  const goNotification = read("../../../api/internal/dbstore/form_notification.go");
  assert.match(
    goNotification,
    /if mode == FormModeResponse \{\s*\n\s*return defaultResponseNotificationName, NotifyEventResponseSubmitted/,
    "EnsureDefault's response branch no longer selects the response event",
  );
  assert.match(
    goNotification,
    /return defaultSaleNotificationName, NotifyEventSalePaid/,
    "EnsureDefault's default branch no longer selects sale.paid for a payment form",
  );
});

test("the checkbox accessible names are translated, not hardcoded English", () => {
  // Same class of defect on the same page: two aria-labels were hardcoded
  // English on a fully i18n page, so a screen reader on ms/zh/ta/ar heard
  // English. The acceptance criteria require no hardcoded English left here.
  assert.doesNotMatch(formsPage, /aria-label="Select all forms"/, "the select-all label is hardcoded English again");
  assert.doesNotMatch(formsPage, /aria-label=\{`Select \$\{f\.title\}`\}/, "the per-row label is hardcoded English again");
  assert.match(formsPage, /aria-label=\{t\("forms\.selectAllAria"\)\}/, "the select-all label is no longer translated");
  assert.match(
    formsPage,
    /aria-label=\{t\("forms\.selectOneAria", \{ title: f\.title \}\)\}/,
    "the per-row label is no longer translated",
  );
  // Both keys must exist in all five locales (the i18n completeness guard
  // checks the dictionary shape; this pins that these keys are present at all).
  for (const key of ["forms.selectAllAria", "forms.selectOneAria"]) {
    const row = i18nDict.split("\n").find((l) => l.includes(`"${key}":`));
    assert.ok(row, `${key} is missing from the dictionary`);
    for (const lang of ["en", "ms", "zh", "ta", "ar"]) {
      assert.match(row, new RegExp(`\\b${lang}:\\s*"`), `${key} has no ${lang} value`);
    }
  }
});
