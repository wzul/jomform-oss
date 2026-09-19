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
  formMode,
  isSalesForm,
  salesFormHref,
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

test("the /forms page actually gates BOTH Sales actions on the mode", () => {
  // A correct helper that the page never calls is dead code behind a green
  // suite. The page renders the Sales link twice (published rows and
  // draft/private rows), so both sites must be gated — an unguarded second site
  // would leave the reported bug live on drafts.
  const gated = [...formsPage.matchAll(/isSalesForm\(f\)\s*&&[\s\S]{0,240}?sales\?form=\$\{f\.id\}/g)];
  assert.equal(
    gated.length,
    2,
    `expected both Sales link sites on /forms to be gated by isSalesForm; found ${gated.length}`,
  );
  // And no UNGATED Sales link may remain anywhere in the page.
  const allSalesLinks = [...formsPage.matchAll(/href=\{`\/sales\?form=\$\{f\.id\}`\}/g)].length;
  assert.equal(allSalesLinks, 2, `expected exactly the two known Sales link sites, found ${allSalesLinks}`);
});

test("form-settings gates its Sales action on the mode too", () => {
  assert.match(
    settingsPage,
    /isSalesForm\(form\)\s*&&[\s\S]{0,240}?sales\?form=\$\{formId\}/,
    "the Form Settings page still offers an unconditional Sales action",
  );
  assert.equal(
    [...settingsPage.matchAll(/href=\{`\/sales\?form=\$\{formId\}`\}/g)].length,
    1,
    "expected exactly one Sales link site on form-settings",
  );
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
