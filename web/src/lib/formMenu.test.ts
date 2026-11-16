// Regression guard for card t_4e92c847 — the Form Settings → "Restaurant menu"
// tab read and wrote only the LEGACY single best-seller field.
//
// THE BUG (found on prod while verifying v1.50.0; pre-existing):
//
//   forms.config.menu of 08a43fd7… ("western-menu", published, workspace `wan`)
//     {"enabled":true,"categories":[/*3*/],
//      "best_seller_ids":["76109c58…","ba4d4656…","71aa6068…"]}
//     — no `best_seller` key.
//
//   The tab rendered "Best-seller product (optional)" + "No best-seller
//   selected" + "Choose best-seller", because the loader's type was
//   `{enabled?, categories?, best_seller?, order_form_slug?}` and it called
//   setMenuBestSeller(md.best_seller || ""). `best_seller_ids` was never read.
//
//   Its Save PUT `{enabled, categories, best_seller: menuBestSeller,
//   order_form_slug}`. The handler builds MenuPage{BestSeller: in.BestSeller,
//   BestSellerIDs: in.BestSellerIDs} and dbstore.WithMenu does
//   `out[MenuConfigKey] = menu` — the menu object is REPLACED, not merged — so
//   one click on Save stored `best_seller:""` and no ids at all.
//
// WHAT THIS PINS, one test each:
//   1. The loader maps `best_seller_ids` into the tab's picker state, in the
//      stored order — the prod row above, verbatim.
//   2. A menu that only carries the legacy `best_seller` still hydrates (one
//      selectable product), so the fix does not strand old configs.
//   3. Both shapes present: the LIST wins — the same precedence the renderer
//      applies (MenuPage.BestSellerProductIDs), not a concatenation.
//   4. The save body carries `best_seller_ids` and NEVER sends `best_seller`:
//      sending `""` is the erase.
//   5. Unchanged round trip: load(wire) → save body still carries the three
//      prod ids, and the JSON that goes on the wire does not contain
//      `"best_seller"` at all (a key named best_seller_ids is fine).
//   6. "Clear" is an EMPTY list, not a missing key — the PUT replaces the menu,
//      so an omitted key would read back as no best sellers too, and an empty
//      array is the explicit intent.
//   7. Empty strings and duplicates are dropped (the renderer drops them too),
//      so a dirty stored list cannot reach the picker or the body twice.
//   8. A malformed/absent payload yields an empty draft instead of throwing —
//      the tab must still render its empty state.
//
// Run with the repo's Node test runner:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { menuDraftFromWire, menuSaveBody } from "./formMenu.ts";

/** The page that owns the tab. These two helpers are only correct if the page
 *  actually calls them with the tab's state — a mis-wired call site would pass
 *  every test above and still erase the merchant's menu, so the wiring is
 *  pinned here too (the same source-shape guard the repo uses for
 *  lib/formSettingsTheme.ts's General Save). */
const PAGE_SRC = readFileSync(fileURLToPath(new URL("../app/form-settings/page.tsx", import.meta.url)), "utf8");

/** Page source with comments removed, so a commented-out example cannot make a
 *  negative assertion pass (or a positive one match prose). */
const pageCode = (): string => PAGE_SRC.replace(/\/\*[\s\S]*?\*\//g, "").replace(/^\s*\/\/.*$/gm, "");

// The prod menu, verbatim (form 08a43fd7…, workspace `wan`): three best sellers
// stored as the #485 list and NO legacy `best_seller` key.
const PROD_MENU = {
  enabled: true,
  categories: [
    { name: "Makanan", product_ids: ["76109c58-0000-0000-0000-000000000001"] },
    { name: "Minuman", product_ids: ["ba4d4656-0000-0000-0000-000000000002"] },
    { name: "Pencuci Mulut", product_ids: ["71aa6068-0000-0000-0000-000000000003"] },
  ],
  best_seller_ids: [
    "76109c58-0000-0000-0000-000000000001",
    "ba4d4656-0000-0000-0000-000000000002",
    "71aa6068-0000-0000-0000-000000000003",
  ],
};

test("the loader hydrates a menu stored as best_seller_ids (the prod row)", () => {
  const draft = menuDraftFromWire(PROD_MENU);
  assert.equal(draft.enabled, true);
  assert.deepEqual(
    draft.bestSellerIDs,
    PROD_MENU.best_seller_ids,
    'a menu configured with best_seller_ids read as "No best-seller selected" — the tab only looked at best_seller',
  );
  assert.equal(draft.categories.length, 3);
  assert.deepEqual(draft.categories[1], {
    name: "Minuman",
    product_ids: ["ba4d4656-0000-0000-0000-000000000002"],
  });
});

test("the loader falls back to the legacy single best_seller", () => {
  const draft = menuDraftFromWire({ enabled: true, best_seller: "legacy-id" });
  assert.deepEqual(draft.bestSellerIDs, ["legacy-id"], "a legacy best_seller must still hydrate the picker");
});

test("the list wins over the legacy id when both are present", () => {
  const draft = menuDraftFromWire({
    enabled: true,
    best_seller: "legacy-id",
    best_seller_ids: ["new-a", "new-b"],
  });
  assert.deepEqual(
    draft.bestSellerIDs,
    ["new-a", "new-b"],
    "BestSellerIDs wins in the renderer, so the tab must read the same precedence (not legacy+list)",
  );
});

test("an empty stored list still falls back to the legacy id", () => {
  // `omitempty` keeps an empty list off the wire, but a hand-written or older
  // payload can carry `[]`; len==0 means "no list" to the renderer.
  const draft = menuDraftFromWire({ enabled: true, best_seller: "legacy-id", best_seller_ids: [] });
  assert.deepEqual(draft.bestSellerIDs, ["legacy-id"]);
});

test("the save body carries the ids as best_seller_ids and never sends best_seller", () => {
  const body = menuSaveBody(menuDraftFromWire(PROD_MENU));
  assert.deepEqual(body.best_seller_ids, PROD_MENU.best_seller_ids);
  assert.equal(
    Object.prototype.hasOwnProperty.call(body, "best_seller"),
    false,
    'the body must not carry best_seller at all — best_seller:"" is what erased an MCP-configured selection',
  );
  // The body is what JSON.stringify sends; the key must be absent from the
  // bytes, not merely undefined on the object.
  assert.equal(JSON.stringify(body).includes('"best_seller":'), false);
});

test("an unchanged selection round-trips through load → save", () => {
  const body = menuSaveBody(menuDraftFromWire(PROD_MENU));
  const wire = JSON.parse(JSON.stringify(body)) as { best_seller_ids: string[] };
  assert.deepEqual(
    wire.best_seller_ids,
    PROD_MENU.best_seller_ids,
    "a Save with an untouched best-seller selection must keep all three ids in config.menu.best_seller_ids",
  );
});

test("clearing the best sellers sends an explicit empty list, not a legacy empty string", () => {
  const draft = menuDraftFromWire(PROD_MENU);
  draft.bestSellerIDs = [];
  const body = menuSaveBody(draft);
  assert.deepEqual(body.best_seller_ids, []);
  assert.equal(Object.prototype.hasOwnProperty.call(body, "best_seller"), false);
  assert.equal(JSON.stringify(body).includes('"best_seller":'), false);
});

test("empty and duplicate ids are dropped, order preserved", () => {
  const draft = menuDraftFromWire({
    enabled: true,
    best_seller_ids: ["b", "", "a", "b", 42, null],
  });
  assert.deepEqual(draft.bestSellerIDs, ["b", "a"]);
  // A dirty draft state cannot reach the wire twice either.
  assert.deepEqual(menuSaveBody({ ...draft, bestSellerIDs: ["b", "", "b", "a"] }).best_seller_ids, ["b", "a"]);
});

test("a malformed or absent payload yields an empty draft instead of throwing", () => {
  for (const raw of [null, undefined, 42, "menu", {}]) {
    const draft = menuDraftFromWire(raw);
    assert.deepEqual(draft.bestSellerIDs, []);
    assert.equal(draft.enabled, false);
    assert.deepEqual(draft.categories, []);
    assert.equal(draft.orderFormSlug, "");
  }
});

test("the TAB is wired to these helpers with its own best-seller list", () => {
  const code = pageCode();
  // The loader: the menu GET goes through the shared mapper, and the picker
  // state is the mapped LIST (not the legacy single id, not an empty array).
  assert.match(
    code,
    /const draft = menuDraftFromWire\(await mr\.json\(\)\)/,
    "the menu loader no longer maps the GET through lib/formMenu",
  );
  assert.match(
    code,
    /setMenuBestSellers\(draft\.bestSellerIDs\)/,
    "the loader no longer hydrates the picker with the stored best-seller LIST",
  );
  // The save: the tab's live state is what the body is built from.
  assert.match(
    code,
    /bestSellerIDs: menuBestSellers/,
    "the save body is no longer built from the tab's best-seller list — a Save would write an empty list",
  );
  assert.match(code, /menuSaveBody\(\{/, "saveMenu no longer uses the shared body builder");
  // And no code path writes the legacy `best_seller` key: that is the key whose
  // "" value erased an MCP-configured selection.
  assert.equal(
    /best_seller\s*:/.test(code),
    false,
    "the tab still writes a legacy `best_seller` key — its empty value is what erased the stored ids",
  );
  assert.equal(
    /setMenuBestSeller\s*\(/.test(code),
    false,
    "the singular best-seller setter is back; the tab must hold the list",
  );
});
