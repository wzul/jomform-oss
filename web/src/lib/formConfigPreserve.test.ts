// A dashboard save must never delete a config key the builder has no control
// for — the custom-HTML form's body being the one that costs a merchant data
// (card t_59bc3055).
//
// THE DEFECT, measured on prod v1.21.273 (d838f7a8) by card t_ec87dc5c, and
// pre-existing rather than a regression of it:
//
//   -- before: select (config ? 'custom_html') from forms where id='2fb67886…' -> t
//   -- dashboard: /forms -> Edit -> /builder?form=2fb67886… , click Publish
//      -> the panel says "Your form is live"
//   -- after:  select (config ? 'custom_html') ...                            -> f
//              jsonb_object_keys(config) -> blocks, chip_mapping, description,
//                                           mode, products
//
// So one click deleted the merchant's HTML body AND its `fields`
// field-registration contract, with a success message and no undo. The stored
// HTML exists nowhere else.
//
// MECHANISM, both halves needed:
//   * web/src/app/builder/page.tsx built `{ mode, description, chip_mapping,
//     blocks, products }` — no custom_html — and made it the PATCH body;
//   * api/internal/dbstore/form.go:328 is `config=COALESCE($4, config)`, which
//     protects a writer that omits the whole `config` key, NOT one that sends a
//     map missing an entry. A present map REPLACES the whole jsonb.
//
// The API's replace semantics are deliberate (every PATCH caller relies on them,
// and MCP `upsert_form` clears a custom body by omitting the section), so the
// fix belongs at the writer that loses data: the builder now lays its own config
// over the one it LOADED, which is the rule templates have used since issue #457
// (`configWithStoredKeys`, lib/templateEdit.ts).
//
// `config.registered_fields` — the submit contract an agent registers with
// register_form_fields, read by formfields.Contract — was destroyed by the same
// builder save on an ordinary block form, so it is pinned here too.
//
// The behavioural proof that the real page sends the section lives in
// web/e2e/builder-save-keeps-custom-html.spec.ts, which drives the built page
// (API stubbed at the browser level) and reads the PATCH body it produced.
//
// Run with the repo's Node test runner:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { configWithStoredKeys } from "./templateEdit.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

const builderPage = read("../app/builder/page.tsx");

// A custom-HTML response form exactly as MCP upsert_form stores it: the body
// under custom_html.html and the registration contract under custom_html.fields
// (api/internal/customform/customform.go, Config + Field json tags).
const CUSTOM_HTML = {
  html: '<p>Selepas embed</p>\n<label for="email">E-mel</label>\n<input id="email" name="email" type="email" required>\n<button type="submit">Hantar</button>',
  fields: [
    { field_id: "email", type: "email", label: "E-mel", required: true },
  ],
};

// What the builder builds from what it renders: mode + description + blocks +
// products + chip_mapping. It has NO custom_html — it renders no editor for one.
const BUILT = {
  mode: "response",
  description: "Nama",
  chip_mapping: {},
  blocks: [{ id: "b1", type: "text", value: "Nama" }],
  products: [],
};

test("a block-side save keeps a custom-HTML form's body and field contract", () => {
  const stored = { mode: "response", custom_html: CUSTOM_HTML };
  const out = configWithStoredKeys(stored, BUILT);

  // The whole section, byte-identical: the body AND the fields array that tells
  // the submit path which values to record.
  assert.deepEqual(out.custom_html, CUSTOM_HTML, "the stored custom_html section was dropped or rewritten");
  assert.equal((out.custom_html as typeof CUSTOM_HTML).html, CUSTOM_HTML.html);
  assert.equal((out.custom_html as typeof CUSTOM_HTML).fields.length, 1);
  // The builder's own keys still win where it owns them — this is a save, not a
  // no-op. (They are inert for a custom form: customform.Enabled() is true, so
  // the public page renders the iframe and never the blocks.)
  assert.deepEqual(out.blocks, BUILT.blocks);
  assert.equal(out.description, BUILT.description);
});

test("a block-side save keeps the registered_fields contract of a block form", () => {
  // Same mechanism, second victim, and it changes what POST /order accepts:
  // formfields.Contract() reads this key before it falls back to the blocks.
  const registered = [{ field_id: "email", type: "email", label: "E-mel", required: true }];
  const out = configWithStoredKeys({ mode: "response", registered_fields: registered }, BUILT);
  assert.deepEqual(out.registered_fields, registered, "the registered_fields contract was dropped");
});

test("an explicit section in the save body still wins, so a real clear clears", () => {
  // The rule is "stored keys the builder does not own survive", NOT "a stored
  // key can never be removed": a writer that carries the section explicitly —
  // the documented surface, MCP upsert_form / REST — is the authority.
  const out = configWithStoredKeys({ custom_html: CUSTOM_HTML }, { ...BUILT, custom_html: { html: "", fields: [] } });
  assert.deepEqual(out.custom_html, { html: "", fields: [] });
});

test("a record with no stored config saves the built config unchanged", () => {
  // The create path: nothing stored yet, so nothing to carry.
  assert.deepEqual(configWithStoredKeys(null, BUILT), BUILT);
  assert.deepEqual(configWithStoredKeys(undefined, BUILT), BUILT);
  // An empty stored map must not resurrect anything either.
  assert.deepEqual(configWithStoredKeys({}, BUILT), BUILT);
});

// ── the page must actually do it (no dead code) ───────────────────────────

test("the builder keeps the config it LOADED for a form, not only for a template", () => {
  // The defect's first half: hydration stored the config only in the
  // template branch, so a form had nothing to carry.
  const at = builderPage.indexOf("storedConfig.current = cfg");
  assert.ok(at > 0, "the builder no longer keeps the loaded config at all");
  const templateBranch = builderPage.indexOf('if (resolved.kind === "template")');
  assert.ok(templateBranch > 0, "the template branch moved — re-anchor this guard");
  assert.ok(
    at < templateBranch,
    "the loaded config is assigned inside the template-only branch again, so a FORM save has nothing to carry",
  );
});

test("the form PATCH body is merged with the stored config", () => {
  assert.match(
    builderPage,
    /configWithStoredKeys\(storedConfig\.current/,
    "the builder builds its PATCH config without the stored one — custom_html and registered_fields would be deleted again",
  );
  // And the pre-fix body, which shipped the bug, is gone: `{ title, config }`
  // with the raw built map (anchored on the closing brace so the fixed
  // `{ title, config: saveConfig }` does not satisfy it).
  assert.doesNotMatch(
    builderPage,
    /\{ title, config \}/,
    "the builder sends the raw built config as the form PATCH body again — every key it does not render is deleted",
  );
});
