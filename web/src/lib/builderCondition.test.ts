// A stored conditional rule whose target is not on the form must be shown as
// BROKEN in the builder panel, not painted as "— Always show —" (card
// t_a81a9165).
//
// THE DEFECT, measured by t_0e2d3d24 on a production build through the real
// builder (form GET stubbed, PATCH captured). The stored config was
//
//   blocks: [{id:"tx1",type:"text",value:"Syarikat"},
//            {id:"ph1",type:"phone",value:"Telefon",
//             condition:{field_id:"gone",op:"equals",value:"x"}}]
//
// and opening the phone block's panel gave a rule picker whose `value` was ""
// with options exactly ["— Always show —", "Syarikat (#1)"] — i.e. the block
// read as ALWAYS SHOWN, while one click on Publish wrote the same broken rule
// back. The `""` is not a choice the merchant made: the `<select>` had no option
// carrying `gone`, so the browser reported no selection and `""` (the Always
// show option) was all the UI and the API could see.
//
// WHY IT MATTERS. formfields.Condition.Met reads `answers["gone"]`, gets "",
// evaluates the rule FALSE, and VisibleAnswers deletes that block's answer — the
// phone field is hidden on the live page forever and its answers discarded,
// while the merchant is told the opposite.
//
// The decision this file pins: the rule is KEPT (dropping it would silently
// re-enable a field the merchant may have hidden on purpose) and the panel names
// the unknown id as unavailable, with the two honest exits — retarget, or
// "Always show".
//
// The behaviour is pinned end to end by
// web/e2e/builder-condition-dangling-target.spec.ts, which drives the built page.
// These cases pin the pure decision and the two call sites in the page, because
// a source-level runner (node --test, no DOM) cannot render the panel at all.
//
// Run with the repo's Node test runner:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { conditionTargetUnavailable } from "./builderCondition.ts";
import { DICT, LANGS } from "../components/i18nDict.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");
const builderPage = read("../app/builder/page.tsx");

// The builder's own field-type list, copied from page.tsx so these cases speak
// the same vocabulary. It is asserted below against the live source, so the copy
// cannot drift silently.
const FIELD_TYPES = [
  "name", "email", "phone", "textarea", "text", "checkbox",
  "number", "select", "radio", "date", "time", "website",
  "address", "consent", "hidden", "list", "repeater", "image-choice", "captcha",
];

// ── the decision ──────────────────────────────────────────────────────────

test("a stored rule naming an id no block carries is unavailable", () => {
  const blocks = [
    { id: "tx1", type: "text" },
    { id: "ph1", type: "phone" },
  ];
  assert.equal(
    conditionTargetUnavailable(blocks, "ph1", "gone", FIELD_TYPES),
    true,
    'the card\'s own fixture (condition.field_id "gone") was read as a live target',
  );
});

test("a stored rule naming another answer field is available", () => {
  const blocks = [
    { id: "tx1", type: "text" },
    { id: "ph1", type: "phone" },
  ];
  assert.equal(conditionTargetUnavailable(blocks, "ph1", "tx1", FIELD_TYPES), false);
});

test("a target that is no longer an ANSWER FIELD is unavailable", () => {
  // Reachable entirely through the builder: "Treat as intro line" retypes a
  // `text` field to a `section` and KEEPS its id (card t_4823943a). The id still
  // resolves to a block, but the picker never lists a section and the server's
  // answers map never carries one — so the rule can never match.
  const blocks = [
    { id: "tx1", type: "section" },
    { id: "ph1", type: "phone" },
  ];
  assert.equal(
    conditionTargetUnavailable(blocks, "ph1", "tx1", FIELD_TYPES),
    true,
    "a rule pointing at a display block was treated as a live target",
  );
});

test("a product block is not a condition source either", () => {
  const blocks = [
    { id: "pr1", type: "product" },
    { id: "ph1", type: "phone" },
  ];
  assert.equal(conditionTargetUnavailable(blocks, "ph1", "pr1", FIELD_TYPES), true);
});

test("a rule naming the block's OWN id is unavailable (the picker refuses self)", () => {
  const blocks = [
    { id: "tx1", type: "text" },
    { id: "ph1", type: "phone" },
  ];
  assert.equal(
    conditionTargetUnavailable(blocks, "ph1", "ph1", FIELD_TYPES),
    true,
    "a self-referencing rule was treated as a live target — the picker never offers it",
  );
});

test("no rule at all is NOT flagged", () => {
  const blocks = [
    { id: "tx1", type: "text" },
    { id: "ph1", type: "phone" },
  ];
  for (const empty of [undefined, null, "", "   "]) {
    assert.equal(
      conditionTargetUnavailable(blocks, "ph1", empty, FIELD_TYPES),
      false,
      `an absent rule (${JSON.stringify(empty)}) was flagged as broken`,
    );
  }
});

// ── the call sites: one decision, used by BOTH surfaces ───────────────────

test("the panel and the canvas card both ask the shared decision", () => {
  const calls = builderPage.match(/conditionTargetUnavailable\(/g) ?? [];
  assert.equal(
    calls.length,
    2,
    "expected exactly two call sites (the rule picker's unavailable option and the canvas card's badge)",
  );
  assert.match(
    builderPage,
    /import \{ conditionTargetUnavailable \} from "\.\.\/\.\.\/lib\/builderCondition";/,
    "page.tsx does not import the shared decision",
  );
});

test("the field-types copy above is the builder's real list", () => {
  const start = builderPage.indexOf("const fieldTypes: BlockType[] = [");
  assert.ok(start >= 0, "page.tsx no longer declares `const fieldTypes: BlockType[]`");
  const end = builderPage.indexOf("];", start);
  const decl = builderPage.slice(start, end);
  for (const t of FIELD_TYPES) {
    assert.match(decl, new RegExp(`"${t}"`), `fieldTypes lost "${t}" — update this test if that is deliberate`);
  }
});

test("the rule picker still carries the stored id as its value", () => {
  // The value is what the browser matches against the options. A rewrite here
  // (`b.condition?.field_id || ""` falling back to the empty string is fine; a
  // rewrite to anything that DROPS a dangling id is not) re-creates the defect:
  // the select would report "" and the block would read as Always show again.
  assert.match(
    builderPage,
    /value=\{b\.condition\?\.field_id \|\| ""\}/,
    "the rule picker no longer renders the stored field_id as its value",
  );
  // The unavailable option is what makes that value resolvable at all.
  assert.match(
    builderPage,
    /condUnavailable && \(\s*<option value=\{b\.condition!\.field_id\}>/,
    "the picker has no option carrying the stored-but-unavailable id",
  );
});

// ── the copy exists in every locale ───────────────────────────────────────

test("the three new keys are translated in all five locales", () => {
  const dict = DICT as unknown as Record<string, Record<string, string>>;
  for (const key of ["bld.condOptionUnavailable", "bld.condWarnUnavailable", "bld.condBrokenBadge"]) {
    const entry = dict[key];
    assert.ok(entry, `${key} is missing from the dictionary`);
    for (const lang of LANGS) {
      assert.ok(
        typeof entry[lang] === "string" && entry[lang].trim() !== "",
        `${key} has no ${lang} value — that locale would silently fall back to English`,
      );
    }
    if (key !== "bld.condBrokenBadge") {
      assert.ok(
        entry.en.includes("{id}"),
        `${key}'s English string does not carry the {id} placeholder, so the merchant cannot see WHICH field is missing`,
      );
    }
  }
});
