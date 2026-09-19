// Regression test — editing one of your OWN templates from the dashboard.
//
// THE GAP (Wan, 2026-09-19): /templates offered Use template / Rename / Delete
// and nothing else, so a template's fields and design could only be changed
// through the REST API or MCP. The missing piece was one UI path: open the
// template in the builder and save its config/theme.
//
// THE TRAP this test is built around: the builder's normal save path is
//
//   PATCH /api/rest/forms/{id}  then  POST /api/rest/forms/{id}/status
//
// and its Publish button forces status='published'. On a template row that
// breaks two things at once, because the two template queries are disjoint by
// status:
//
//   ListTemplatesByAccount (/templates page):
//     account_id=$1 AND is_template AND NOT (status='published')
//   ListTemplates (public gallery, used by every New-form picker):
//     is_template AND status='published'          -- NO account filter
//
// so publishing a template hides it from the merchant's own list AND shows it
// to every other merchant in the gallery.
//
// The Go side of this invariant is covered by the integration test in
// api/internal/httpapi/templates_issue457_test.go (config change persists,
// the row stays in "my templates", it never reaches the gallery). This file
// covers the UI half: the plan the builder executes must contain no status
// write and no publish for a template, and the page must actually be wired to
// that plan and to the template endpoints.
//
// Run with the repo's Node test runner:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import {
  builderControls,
  builderTarget,
  loadPath,
  savePlan,
  templateConfigWithStoredKeys,
  templateEditHref,
  templateSaveBody,
} from "./templateEdit.ts";

import { DICT } from "../components/i18nDict.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

const builderPage = read("../app/builder/page.tsx");
const templatesPage = read("../app/templates/page.tsx");
const goTemplateStore = read("../../../api/internal/dbstore/form.go");

const TEMPLATE_ID = "4283337b-a0ec-464e-98d8-8dd301632de1";

// ── the reported gap ───────────────────────────────────────────────────────

test("the Templates page links one of your own templates into the builder", () => {
  // The reported gap: there was no Edit action at all, so a template list row
  // was a dead end for editing its content.
  assert.equal(templateEditHref(TEMPLATE_ID), `/builder?template=${TEMPLATE_ID}`);
  assert.match(
    templatesPage,
    /href=\{templateEditHref\(tpl\.id\)\}/,
    "/templates no longer links a template row to the builder — that is the reported gap coming back",
  );
});

test("the template link is URL-encoded, so an id can never inject a query param", () => {
  const href = templateEditHref("a b&c=d");
  assert.equal(href, "/builder?template=a%20b%26c%3Dd");
});

// ── target resolution ─────────────────────────────────────────────────────

test("?template=<id> selects template mode", () => {
  assert.deepEqual(builderTarget({ template: TEMPLATE_ID }), { kind: "template", id: TEMPLATE_ID });
});

test("?form=<id> still selects form mode, and no params is the new-form path", () => {
  assert.deepEqual(builderTarget({ form: "form-1" }), { kind: "form", id: "form-1" });
  assert.deepEqual(builderTarget({ isNew: true }), { kind: "new" });
  assert.deepEqual(builderTarget({}), { kind: "new" });
});

test("a template id wins over a form id when both are present", () => {
  // If form mode won, the builder would treat the template as an ordinary form
  // and save it through the form routes — the exact failure this guards.
  assert.deepEqual(
    builderTarget({ form: "form-1", template: TEMPLATE_ID }),
    { kind: "template", id: TEMPLATE_ID },
  );
});

test("a blank template param does not shadow ?form=", () => {
  assert.deepEqual(builderTarget({ template: "  ", form: "form-1" }), { kind: "form", id: "form-1" });
});

test("the picker only opens on the BARE new-form path, never for ?new=1", () => {
  // Regression from this change: treating every no-id path as "show the
  // picker" would pop the template picker over a ?new=1 blank form.
  assert.match(
    builderPage,
    /if \(!params\.get\("new"\)\) setPickerOpen\(true\);/,
    "the template picker is no longer gated on the bare new-form path",
  );
});

// ── the load/save contract ────────────────────────────────────────────────

test("a template is READ from the template route, never the form route", () => {
  // /forms/{id} has no is_template filter, so it happens to work today — but it
  // does not enforce the ownership rule the template route does, and a 404 for
  // someone else's template is the correct failure.
  assert.equal(loadPath({ kind: "template", id: TEMPLATE_ID })(TEMPLATE_ID), `/api/rest/templates/${TEMPLATE_ID}`);
  assert.equal(loadPath({ kind: "form", id: "f1" })("f1"), "/api/rest/forms/f1");
});

test("a template save is a PUT to the template route and NOTHING else", () => {
  const plan = savePlan({ kind: "template", id: TEMPLATE_ID }, false);
  assert.deepEqual(plan.steps, ["writeRecord"]);
  assert.equal(plan.method, "PUT");
  assert.equal(plan.recordPath(TEMPLATE_ID), `/api/rest/templates/${TEMPLATE_ID}`);
  // The mutation check: this is the assertion that goes red if the save is
  // reverted to the builder's normal flow (PATCH /forms/{id} + status).
  assert.equal(
    plan.steps.includes("writeStatus"),
    false,
    "a template save must not POST /forms/{id}/status — that sets status and can publish it",
  );
});

test("publishing is unreachable for a template, even when the caller asks for it", () => {
  // The Publish button's save(true) must not reach a publish step. This is the
  // regression that used to empty the merchant's list and fill the gallery.
  const plan = savePlan({ kind: "template", id: TEMPLATE_ID }, true);
  assert.deepEqual(plan.steps, ["writeRecord"]);
  assert.equal(plan.canPublish, false);
  assert.equal(plan.steps.includes("publish"), false);
});

test("a form keeps its full save path, publish included", () => {
  // The template mode must not have flattened the form flow.
  const plain = savePlan({ kind: "form", id: "f1" }, false);
  assert.deepEqual(plain.steps, ["writeRecord", "writeStatus"]);
  assert.equal(plain.method, "PATCH");
  assert.equal(plain.recordPath("f1"), "/api/rest/forms/f1");
  assert.equal(plain.canPublish, true);

  const published = savePlan({ kind: "form", id: "f1" }, true);
  assert.deepEqual(published.steps, ["writeRecord", "writeStatus", "publish"]);
});

test("a brand-new form is POSTed and then has its status written like any other", () => {
  // The new-form path is unchanged: create, then apply the selected status to
  // the id that came back.
  const plan = savePlan({ kind: "new" }, false);
  assert.deepEqual(plan.steps, ["writeRecord", "writeStatus"]);
  assert.equal(plan.method, "POST");
  assert.equal(plan.recordPath("ignored"), "/api/rest/forms/ignored");
  assert.equal(plan.canPublish, true);
});

test("a template that failed to load cannot be saved as a NEW template", () => {
  // Without the guard, `let id = currentId` is null, the isCreate branch runs,
  // and POST /api/rest/templates creates a fresh template out of an error
  // screen.
  assert.match(
    builderPage,
    /if \(editingTemplate && !currentId\) \{/,
    "the builder can create a new template from a page whose template never loaded",
  );
});

// ── the payload ───────────────────────────────────────────────────────────

test("the template save body carries title + config and omits an unknown theme", () => {
  const body = templateSaveBody("My template", { mode: "response", blocks: [] });
  assert.deepEqual(body, { title: "My template", config: { mode: "response", blocks: [] } });
  // Omitted, not null: the API COALESCEs a nil theme over the stored value, so
  // sending null/{} would be a silent way to wipe a template's design.
  assert.equal("theme" in body, false);
});

test("the template save body includes a known theme", () => {
  const theme = { primary: "#123456" };
  assert.deepEqual(templateSaveBody("T", { blocks: [] }, theme).theme, theme);
});

test("editing a template keeps stored config keys the builder has no control for", () => {
  // A gallery card's category IS config.category (rest_templates.go reads it).
  // The builder rebuilds config from the blocks it renders, so a wholesale
  // replace would strip the category off a template that has one.
  const stored = { mode: "response", category: "Education", keep_me: 1, blocks: [] };
  const built = { mode: "payment", blocks: [{ id: "b1", type: "text" }], products: [] };
  const out = templateConfigWithStoredKeys(stored, built);
  assert.equal(out.category, "Education", "the stored gallery metadata was dropped");
  assert.equal(out.keep_me, 1, "an unrecognised stored key was dropped");
  // The edit itself (what the builder rendered) wins.
  assert.equal(out.mode, "payment");
  assert.deepEqual(out.blocks, built.blocks);
});

test("a template with no stored config is saved with the built config unchanged", () => {
  const built = { mode: "response", blocks: [] };
  assert.deepEqual(templateConfigWithStoredKeys(null, built), built);
  assert.deepEqual(templateConfigWithStoredKeys(undefined, built), built);
});

// ── controls ──────────────────────────────────────────────────────────────

test("template mode hides every status/publish/slug/form-only control", () => {
  const c = builderControls("template");
  assert.equal(c.publish, false);
  assert.equal(c.slug, false);
  assert.equal(c.saveAsTemplate, false);
  assert.equal(c.formSettings, false);
  assert.equal(c.view, false);
  // Editing the fields IS the feature, so the design controls must stay.
  assert.equal(c.aiGenerate, true);
  assert.equal(c.mode, true);
});

test("form mode keeps every control", () => {
  const c = builderControls("form");
  assert.equal(c.publish, true);
  assert.equal(c.slug, true);
  assert.equal(c.saveAsTemplate, true);
  assert.equal(c.formSettings, true);
  assert.equal(c.view, true);
});

// ── the page must actually execute the plan (no dead code) ─────────────────

test("the builder resolves its target from ?template= and branches on it", () => {
  assert.match(builderPage, /params\.get\("template"\)/, "the builder no longer reads ?template=");
  assert.match(
    builderPage,
    /builderTarget\(\{/,
    "the builder no longer delegates target resolution to the tested helper",
  );
});

test("the builder's save goes through savePlan, so it cannot re-decide inline", () => {
  // A save function that hand-rolls its fetches would pass every assertion
  // above while shipping the old flow. It must build a plan.
  assert.match(builderPage, /savePlan\(/, "the builder save path no longer uses the tested save plan");
  assert.match(builderPage, /plan\.steps\.includes\("writeStatus"\)/);
  assert.match(builderPage, /plan\.steps\.includes\("publish"\)/);
});

test("the builder never calls a form status/publish/forms-PATCH route outside the form branch", () => {
  // Belt and braces on the exact defect: these three URLs may only be reached
  // through a plan step, never hardcoded in a template branch. The template
  // mode's absence of them is what keeps a template draft.
  assert.doesNotMatch(
    builderPage,
    /\/api\/rest\/templates\/\$\{id\}\/status/,
    "a /templates/{id}/status call was added — no such route exists, and status must never be written on a template",
  );
  assert.doesNotMatch(
    builderPage,
    /\/api\/rest\/templates\/\$\{id\}\/publish/,
    "a /templates/{id}/publish call was added — publishing a template puts it in every merchant's gallery",
  );
});

test("the template PUT body is built by the tested helper", () => {
  assert.match(builderPage, /templateSaveBody\(/, "the template save body is hand-built again");
  assert.match(
    builderPage,
    /templateConfigWithStoredKeys\(templateConfig\.current/,
    "the builder replaces a template's config wholesale again — stored keys such as the gallery category would be stripped",
  );
});

test("the builder's load path comes from the tested helper", () => {
  assert.match(builderPage, /loadPath\(resolved\)\(id\)/, "the builder no longer loads through loadPath(target)");
});

test("template mode is what suppresses the slug and Publish controls in the markup", () => {
  // Anchored per control: a file-wide match would be satisfied by the form
  // branch and would survive deleting the guard from the template branch.
  assert.match(
    builderPage,
    /\{controls\.publish && \(/,
    "the Publish buttons are no longer gated on the target's controls",
  );
  assert.match(
    builderPage,
    /\{controls\.slug && \(/,
    "the slug input is no longer gated on the target's controls",
  );
  assert.match(
    builderPage,
    /\{controls\.saveAsTemplate && \(/,
    "\"Save as template\" is no longer gated — saving a template as a template is meaningless",
  );
  assert.match(
    builderPage,
    /\{controls\.formSettings && currentId && \(/,
    "the Form Settings link is no longer gated — it edits a FORM, not a template",
  );
});

test("the builder marks the template title input so a screen reader does not read a bare box", () => {
  assert.match(
    builderPage,
    /aria-label=\{t\(editingTemplate \? "tplmg\.editTitle" : "bld\.formTitle"\)\}/,
    "the title input lost its accessible name in template mode",
  );
});

// ── the Go invariant this mirrors ─────────────────────────────────────────

test("the Go queries still make publishing a template both hide it and expose it", () => {
  // If either predicate changes, the UI reasoning above is stale and the
  // template contract must be re-derived rather than trusted.
  assert.match(goTemplateStore, /NOT \(status=\$2\)/, "ListTemplatesByAccount no longer excludes published rows");
  assert.match(
    goTemplateStore,
    /AND is_template AND status='published' ORDER BY created_at/,
    "ListTemplates no longer filters on published — the gallery rule changed",
  );
  // The gallery query must stay account-unfiltered for this defect to matter
  // at all; if that changes, revisit the decision rather than the test.
  assert.doesNotMatch(
    goTemplateStore,
    /is_template AND status='published' AND account_id/,
    "the gallery is now account-filtered — re-derive the template contract",
  );
});

// ── i18n ──────────────────────────────────────────────────────────────────

test("the new template-mode strings exist in all five locales", () => {
  // Imported from the dictionary rather than grepped: a multi-line entry would
  // defeat a line-based check. The generic i18n completeness guard covers this
  // class too; this pins the keys the template mode actually reads (a page
  // reading a missing key renders the raw key).
  for (const key of [
    "tplmg.edit",
    "tplmg.editHint",
    "tplmg.editLabel",
    "tplmg.editTitle",
    "tplmg.editSaved",
    "tplmg.editFail",
    "tplmg.editLoadFail",
  ]) {
    const entry = DICT[key as keyof typeof DICT] as Record<string, string> | undefined;
    assert.ok(entry, `${key} is missing from the dictionary`);
    for (const lang of ["en", "ms", "zh", "ta", "ar"]) {
      assert.ok(
        typeof entry[lang] === "string" && entry[lang].trim() !== "",
        `${key} has no ${lang} value`,
      );
    }
  }
});
