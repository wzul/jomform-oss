// Regression guard for card t_106955f6 — the Form Settings General Save
// silently deleted every theme key it does not own.
//
// THE BUG (measured on prod, one click on the General tab's visible Save):
//
//   before  {"layout":"centered","primary":"#b45309","spacing":"comfortable",
//            "product_image_size":"large","product_image_layout":"image-top"}
//   after   {"primary":"#b45309","product_image_size":"large",
//            "product_image_layout":"image-top"}
//
// The API's PATCH replaces the whole `theme` jsonb when the key is present, and
// the page sent a map built from its own four keys only. `layout` and `spacing`
// feed the block renderer's CSS conditionals, so a routine Save changed the
// buyer-facing page: an ORDER-mode form that served `justify-content:center`
// and `.card{padding:48px}` started serving neither.
//
// WHAT THIS PINS, one test each:
//   1. A stored key this tab has no control for SURVIVES the payload — the
//      defect, asserted on the exact keys the prod row lost.
//   2. The keys this tab owns WIN over the stored values (the edit still lands).
//   3. `custom_js` is written when set and DELETED when the field is cleared —
//      so "clear the custom JS" keeps working on a payload that is otherwise a
//      full round trip (the one owned key that can be removed).
//   4. A form with no stored theme still produces exactly the owned keys (no
//      `custom_js:""`, no `undefined` keys that JSON.stringify would drop).
//   5. The payload is what the SERVER will store: a JSON round trip of the
//      payload keeps every key, so a value that cannot be serialised is caught
//      here rather than as a vanished key on the buyer's page.
//   6. Unknown FURTHER keys are carried too — the next theme key nobody has
//      written yet must not need this file edited to survive a save.
//   7. The stored object is not mutated in place (page state must not be
//      rewritten by serialising a request).
//
// Run with the repo's Node test runner:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";

import { generalSaveTheme } from "./formSettingsTheme.ts";

// The prod row's theme, verbatim (form 08a43fd7…, before the QA lane restored
// it): the two keys the General Save dropped, plus the block renderer's other
// inputs so a fix that kept only layout/spacing would still be caught.
const STORED = {
  layout: "centered",
  spacing: "comfortable",
  background: "#eeeeee",
  text: "#111111",
  card_background: "#ffffff",
  card_border_radius: "12px",
  font_family: "Georgia, serif",
  custom_css: ".card { box-shadow: none; }",
  primary: "#5e6ad2",
  product_image_size: "small",
  product_image_layout: "thumbnail-left",
};

const OWN = {
  primary: "#b45309",
  productImageSize: "large",
  productImageLayout: "image-top",
  customJS: "",
};

test("a stored key the General tab does not own survives the save", () => {
  const theme = generalSaveTheme(STORED, OWN);

  // The card's own measurement: these two went missing on prod.
  assert.equal(theme.layout, "centered");
  assert.equal(theme.spacing, "comfortable");

  // ...and every other key the tab has no control for. Each is read by the
  // renderer, so a dropped one is a visible change the merchant never asked for.
  assert.equal(theme.background, "#eeeeee");
  assert.equal(theme.text, "#111111");
  assert.equal(theme.card_background, "#ffffff");
  assert.equal(theme.card_border_radius, "12px");
  assert.equal(theme.font_family, "Georgia, serif");
  assert.equal(theme.custom_css, ".card { box-shadow: none; }");
});

test("the keys the General tab owns are written over the stored values", () => {
  const theme = generalSaveTheme(STORED, OWN);

  assert.equal(theme.primary, "#b45309");
  assert.equal(theme.product_image_size, "large");
  assert.equal(theme.product_image_layout, "image-top");
  // The stored theme had no custom_js and the field is empty: absent, not "".
  assert.equal("custom_js" in theme, false);
});

test("a cleared custom-JS field removes the key rather than writing an empty one", () => {
  const withJS = { ...STORED, custom_js: "window.alert(1)" };

  const kept = generalSaveTheme(withJS, { ...OWN, customJS: "window.alert(2)" });
  assert.equal(kept.custom_js, "window.alert(2)");

  const cleared = generalSaveTheme(withJS, { ...OWN, customJS: "" });
  assert.equal("custom_js" in cleared, false, "an empty field must CLEAR custom_js");
  // Clearing one owned key must not disturb the unowned ones.
  assert.equal(cleared.layout, "centered");
  assert.equal(cleared.spacing, "comfortable");

  // Whitespace is a clear too — a stray space is not custom JS.
  const spaces = generalSaveTheme(withJS, { ...OWN, customJS: "   \n " });
  assert.equal("custom_js" in spaces, false);
});

test("a form with no stored theme produces exactly the owned keys", () => {
  for (const stored of [undefined, null, {}]) {
    const theme = generalSaveTheme(stored, OWN);
    assert.deepEqual(theme, {
      primary: "#b45309",
      product_image_size: "large",
      product_image_layout: "image-top",
    });
  }
});

test("the payload survives the JSON round trip the browser sends", () => {
  const theme = generalSaveTheme(STORED, { ...OWN, customJS: "console.log(1)" });
  const sent = JSON.parse(JSON.stringify({ slug: "demo", title: "Demo", theme }));

  assert.deepEqual(sent.theme, theme);
  assert.equal(sent.theme.layout, "centered");
  assert.equal(sent.theme.spacing, "comfortable");
  assert.equal(sent.theme.custom_js, "console.log(1)");
});

test("a theme key nobody has written yet is carried through too", () => {
  const theme = generalSaveTheme({ ...STORED, future_key: "kept" }, OWN);
  assert.equal(theme.future_key, "kept");
});

test("the stored theme object is not mutated in place", () => {
  const stored = { ...STORED };
  const before = JSON.stringify(stored);

  const theme = generalSaveTheme(stored, { ...OWN, customJS: "x()" });

  assert.equal(JSON.stringify(stored), before, "page state must not be rewritten");
  assert.notEqual(theme, stored);
});
