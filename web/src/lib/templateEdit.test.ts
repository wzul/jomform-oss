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
  adoptedTarget,
  builderControls,
  builderHref,
  builderTarget,
  configWithStoredKeys,
  loadPath,
  saveFormAsTemplate,
  savePlan,
  templateEditHref,
  templateSaveBody,
} from "./templateEdit.ts";

import { DICT } from "../components/i18nDict.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

const builderPage = read("../app/builder/page.tsx");
const templatesPage = read("../app/templates/page.tsx");
const goTemplateStore = read("../../../api/internal/dbstore/form.go");

const TEMPLATE_ID = "4283337b-a0ec-464e-98d8-8dd301632de1";

// The body of the `if (isCreate) { … }` branch inside save(). Anchored by index
// rather than by one long regex so a comment (or a reflowed line) inside the
// branch cannot make the pins below silently stop matching anything.
function createBranch(source: string): string {
  const start = source.indexOf("if (isCreate) {");
  assert.ok(start >= 0, "app/builder/page.tsx no longer has an `if (isCreate) {` branch — the create path moved");
  const end = source.indexOf("\n      }", start);
  assert.ok(end > start, "could not delimit the isCreate branch in app/builder/page.tsx");
  return source.slice(start, end);
}

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

// ── the create path adopts its identity (t_b3cc2a7a) ──────────────────────
//
// The reported defect. A merchant creating a form got "Failed to save." on the
// SECOND save, and a refresh after create+publish started ANOTHER empty form.
// One missing step: after the create, the builder never adopted the row it had
// just made — neither in its target (so the plan still said POST /forms, and
// POST /api/rest/forms/{id} is not a route -> 405) nor in its URL (so a refresh
// re-read ?new=1 and began a new form).

test("a create makes the builder adopt the row it just made", () => {
  const adopted = adoptedTarget("93438fc5-c0df-4c40-901b-e064a649f4d0");
  assert.deepEqual(adopted, { kind: "form", id: "93438fc5-c0df-4c40-901b-e064a649f4d0" });
  // THE identity transition, and the exact defect: the plan for a brand-new form
  // is POST, and the plan for the row that POST answered with must be PATCH.
  // Before the fix the target stayed {kind:"new"}, so the second Save of a
  // new-form session re-sent POST to the id-carrying path (405 -> "Failed to
  // save.", with no machine code to name the cause).
  assert.equal(savePlan({ kind: "new" }, false).method, "POST");
  assert.equal(
    savePlan(adopted!, false).method,
    "PATCH",
    "the second save of a new-form session is not a PATCH — it re-POSTs to /forms/{id}, which has no route",
  );
  assert.equal(savePlan(adopted!, false).recordPath("ignored"), "/api/rest/forms/ignored");
  // Acceptance 2: the adopted target keeps the FULL form plan, so a Save after
  // Publish still writes the status and still runs the publish step.
  assert.deepEqual(savePlan(adopted!, true).steps, ["writeRecord", "writeStatus", "publish"]);
  assert.equal(savePlan(adopted!, true).canPublish, true);
});

test("the adopted URL round-trips through the loader a refresh uses", () => {
  // Acceptance 3: after a create the URL must carry the new id. A refresh parses
  // window.location.search with builderTarget — the same loader the mount effect
  // uses — so the round trip is the testable half of "refresh reopens the same
  // form": adopt -> URL -> parse -> the SAME form id (not {kind:"new"}, which is
  // the brand-new empty form, and not a 404).
  const id = "93438fc5-c0df-4c40-901b-e064a649f4d0";
  const href = builderHref(adoptedTarget(id)!);
  assert.equal(href, `/builder?form=${id}`);
  const search = new URLSearchParams(href.slice(href.indexOf("?")));
  const reloaded = builderTarget({ form: search.get("form"), template: search.get("template") });
  assert.deepEqual(reloaded, { kind: "form", id }, "a refresh on the adopted URL does not reopen the form that was just created");
  assert.equal(loadPath(reloaded)(id), `/api/rest/forms/${id}`);
  // The bare ?new=1 the URL had before the fix is the failure being pinned.
  assert.deepEqual(builderTarget({ isNew: true }), { kind: "new" });
});

test("a created row with no id is not adopted", () => {
  // A body without an id cannot name a record: adopting it would make the next
  // save PATCH /forms/undefined and put ?form=undefined in the URL, which a
  // refresh turns into a 404. The builder keeps its old behaviour instead.
  assert.equal(adoptedTarget(""), null);
  assert.equal(adoptedTarget(undefined), null);
  assert.equal(adoptedTarget(null), null);
  assert.equal(adoptedTarget("   "), null);
});

test("the URLs this helper builds are the ones the existing loaders already read", () => {
  // Acceptance 4: ?form= and ?template= are unchanged, and the template link on
  // /templates is now literally the same builder URL helper (one rule, so the
  // two cannot drift).
  assert.equal(builderHref({ kind: "form", id: "f1" }), "/builder?form=f1");
  assert.equal(builderHref({ kind: "template", id: "t1" }), "/builder?template=t1");
  assert.equal(builderHref({ kind: "new" }), "/builder?new=1");
  assert.equal(templateEditHref("a b&c=d"), builderHref({ kind: "template", id: "a b&c=d" }));
  assert.deepEqual(builderTarget({ form: "f1" }), { kind: "form", id: "f1" });
});

test("the builder's create branch adopts the id in BOTH the target and the URL", () => {
  // The hole acceptance 5 names: nothing pinned the create path's identity
  // transition, so deleting it left every other test green. Neutralise it (drop
  // the setTarget/replaceState pair) and this goes red.
  const branch = createBranch(builderPage);
  assert.match(
    branch,
    /adoptedTarget\(d\.id\)/,
    "the create branch no longer adopts the id the API returned",
  );
  assert.match(
    branch,
    /setTarget\(adopted\)/,
    "the create branch does not re-target the builder at the row it just made — the next save re-POSTs to /forms/{id} (405)",
  );
  assert.match(
    branch,
    /window\.history\.replaceState\(null, "", builderHref\(adopted\)\)/,
    "the create branch does not rewrite the URL — a refresh after create+publish starts another new form",
  );
});

test("a created row is never adopted as a template", () => {
  // A template's create path does not exist (the guard test above), and adopting
  // the form route by mistake would send template writes to /forms/{id}.
  const adopted = adoptedTarget(TEMPLATE_ID);
  assert.equal(adopted!.kind, "form");
  assert.equal(savePlan(adopted!, false).recordPath(TEMPLATE_ID), `/api/rest/forms/${TEMPLATE_ID}`);
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
  const out = configWithStoredKeys(stored, built);
  assert.equal(out.category, "Education", "the stored gallery metadata was dropped");
  assert.equal(out.keep_me, 1, "an unrecognised stored key was dropped");
  // The edit itself (what the builder rendered) wins.
  assert.equal(out.mode, "payment");
  assert.deepEqual(out.blocks, built.blocks);
});

test("a template with no stored config is saved with the built config unchanged", () => {
  const built = { mode: "response", blocks: [] };
  assert.deepEqual(configWithStoredKeys(null, built), built);
  assert.deepEqual(configWithStoredKeys(undefined, built), built);
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
    /configWithStoredKeys\(storedConfig\.current/,
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

// ── "Save as template" on an UNSAVED form (t_c33effe4) ─────────────────────
//
// THE DEFECT, MEASURED ON A RUNNING STACK (review stack, fresh browser state,
// /builder?new=1, title typed, ONE click on "Save as template"):
//
//   POST /api/rest/forms/?workspace=…        201   <- the form WAS created
//   POST /api/rest/forms/{id}/status?…       200
//   -- no POST /api/rest/templates at all --
//
// and the DOM carried neither "Saved as a template." nor "Failed to save as
// template." — the button looked like it had done nothing beyond saving the
// draft. A form that already had an id was fine, because the block that ran was
// skipped.
//
// THE MECHANISM: `saveAsTemplate` awaited `save(false)` and then RE-READ
// `currentId`. `currentId` is a value captured by the RENDER's closure, so the
// `setCurrentId` inside save() re-rendered the component and changed nothing
// about the variable the already-running handler held. The re-read saw its own
// stale null and returned — before either `setTplMsg` arm, which is why there was
// no message either.
//
// These tests drive `saveFormAsTemplate` — the helper the button now delegates
// its whole decision to — so the behaviour is exercised, not just the wiring.
// The source pins below keep the page pointing at that helper.

test("an UNSAVED form is snapshotted from the id its own save wrote", async () => {
  // The reported case end to end: no id yet, the save writes one, and the
  // template POST must use THAT id. Pre-fix it returned early and posted
  // nothing at all.
  const seen: string[] = [];
  const outcome = await saveFormAsTemplate({
    currentId: null,
    ensureSaved: async () => {
      seen.push("save");
      return "8f2c4e10-0000-4000-8000-000000000001"; // what POST /api/rest/forms answered
    },
    snapshot: async (id) => {
      seen.push(`snapshot:${id}`);
      return true;
    },
  });
  assert.equal(outcome, "saved");
  assert.deepEqual(
    seen,
    ["save", "snapshot:8f2c4e10-0000-4000-8000-000000000001"],
    "the template was not posted, or was posted for an id the save never produced (t_c33effe4)",
  );
});

test("a form that already has an id is snapshotted WITHOUT a second save", async () => {
  let saved = false;
  let snapped = "";
  const outcome = await saveFormAsTemplate({
    currentId: "existing-form",
    ensureSaved: async () => {
      saved = true;
      return "some-other-form";
    },
    snapshot: async (id) => {
      snapped = id;
      return true;
    },
  });
  assert.equal(outcome, "saved");
  assert.equal(snapped, "existing-form", "the template snapshotted something other than the form on screen");
  assert.equal(saved, false, "the button saved a form that was already saved — a write the merchant did not ask for");
});

test("a save that wrote NOTHING creates no template", async () => {
  // The property the old `if (!currentId) return` guard existed for: a failed
  // save leaves its own error on screen, and an error screen must not become a
  // template. Kept, and now pinned rather than implied.
  for (const written of [null, "", undefined as unknown as string | null]) {
    let posted = false;
    const outcome = await saveFormAsTemplate({
      currentId: null,
      ensureSaved: async () => written,
      snapshot: async () => {
        posted = true;
        return true;
      },
    });
    assert.equal(outcome, "skipped", `a ${JSON.stringify(written)} id produced a template`);
    assert.equal(posted, false, "a template was POSTed for a form the save did not write");
  }
  // An empty `currentId` is not an identity either — same rule as adoptedTarget.
  let posted = false;
  const outcome = await saveFormAsTemplate({
    currentId: "",
    ensureSaved: async () => null,
    snapshot: async () => {
      posted = true;
      return true;
    },
  });
  assert.equal(outcome, "skipped");
  assert.equal(posted, false);
});

test("every finished attempt reports a sentence — the silent path is gone", async () => {
  // The other half of the symptom was NO feedback at all. A rejected snapshot, a
  // snapshot that throws and a SAVE that throws must each answer "failed" (the
  // caller renders "Failed to save as template." for it); only a save that
  // reported no id is allowed to answer "skipped", and that path already has the
  // save's own error on screen.
  const rejected = await saveFormAsTemplate({
    currentId: "f1",
    ensureSaved: async () => null,
    snapshot: async () => false,
  });
  assert.equal(rejected, "failed");

  const throwingSnapshot = await saveFormAsTemplate({
    currentId: "f1",
    ensureSaved: async () => null,
    snapshot: async () => {
      throw new Error("network");
    },
  });
  assert.equal(throwingSnapshot, "failed");

  // Pre-fix this rejected out of the click handler: no message, and an
  // unhandled rejection in the console.
  const throwingSave = await saveFormAsTemplate({
    currentId: null,
    ensureSaved: async () => {
      throw new Error("network");
    },
    snapshot: async () => true,
  });
  assert.equal(throwingSave, "failed");
});

// The body of the `saveAsTemplate` handler, anchored by index (the same reason
// createBranch() is: a comment or a reflowed line inside the body must not make
// the pins below silently stop matching anything).
function saveAsTemplateBody(): string {
  const start = builderPage.indexOf("async function saveAsTemplate()");
  assert.ok(start >= 0, "app/builder/page.tsx no longer has a `saveAsTemplate` handler — the button's wiring moved");
  const end = builderPage.indexOf("\n  }", start);
  assert.ok(end > start, "could not delimit saveAsTemplate in app/builder/page.tsx");
  return builderPage.slice(start, end);
}

test("the button takes the id from the tested helper, never from a re-read of state", () => {
  const body = saveAsTemplateBody();
  assert.match(body, /saveFormAsTemplate\(\{/, '"Save as template" no longer goes through the tested helper');
  assert.match(
    body,
    /ensureSaved: \(\) => save\(false\)/,
    "the handler no longer saves the form before snapshotting it",
  );
  assert.match(body, /form_id: id/, "the template POST no longer snapshots the resolved id");
  // The pre-fix shape, verbatim: await the save, then read `currentId` again.
  // That read is the defect — the value belongs to a render that is already
  // gone by the time the await returns.
  assert.doesNotMatch(
    body,
    /await save\(false\)[\s\S]{0,120}?currentId/,
    "saveAsTemplate reads `currentId` after awaiting its save again: that value is captured by the render's closure, so an unsaved form gets its row created and the template is silently never posted (t_c33effe4)",
  );
});

test("save() reports the id it wrote, and null when any step failed", () => {
  assert.match(
    builderPage,
    /async function save\(publish: boolean\): Promise<string \| null>/,
    'save() no longer reports the id it wrote — that return value is exactly what "Save as template" needs, and without it the handler is back to reading stale state',
  );
  const start = builderPage.indexOf("async function save(publish: boolean)");
  assert.ok(start >= 0, "app/builder/page.tsx no longer has save(publish)");
  const end = builderPage.indexOf("async function copyPublishUrl", start);
  assert.ok(end > start, "could not delimit save() in app/builder/page.tsx");
  const body = builderPage.slice(start, end);
  assert.match(body, /\n    return id;/, "save() no longer returns the id it wrote");
  // `return;` answers undefined, which a caller cannot tell apart from "the save
  // failed" — every failure path must answer the explicit null.
  assert.doesNotMatch(
    body,
    /\n\s+return;/,
    "save() still has a bare `return;` — that reports undefined rather than \"nothing was written\"",
  );
});
