// Card t_983425e5 — the product picker lied, and the builder never said what the
// buyer would see.
//
// THE DEFECT, measured on prod: the builder's product <select> was bound to
// `value={b.productId || ""}` with NO empty placeholder option. A block that had
// never been pointed at a product therefore DISPLAYED the first product
// ("derma — RM1.00") while the control's value was still "" — the merchant read a
// selection that did not exist, pressed Publish, and the stored block was
// `{"type":"product","value":"","quantity":true}` with no `product_id` at all.
// The buyer page then said "This form is not accepting orders right now".
//
// What this file pins, on the REAL functions the builder calls:
//
//   - the select's value is "" (the placeholder) whenever the block names no
//     product, or names one the list no longer offers — so the control can never
//     display a product the block is not bound to;
//   - the INVERSE: a block that DOES name an offered product keeps that
//     selection (the fix must not blank a valid one);
//   - the builder actually uses those answers (source assertions, so the fix
//     cannot become dead code behind a green suite) — the placeholder option is
//     rendered FIRST, before the product list, and the warning banner reads the
//     same predicate;
//   - the /forms row shows the "not accepting orders" pill instead of a clean
//     LIVE pill when the API says the form is dead.
//
// Run with `npm run test:unit` (node --test src/lib/*.test.ts).

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { paymentFormCannotTakeOrders, productSelectValue, unpointedProductBlocks } from "./productPick.ts";

const here = fileURLToPath(new URL(".", import.meta.url));
const builderPage = readFileSync(here + "../app/builder/page.tsx", "utf8");
const formsPage = readFileSync(here + "../app/forms/page.tsx", "utf8");

// The workspace catalogue the picker offers: names and prices are irrelevant to
// this rule, only ids are.
const PRODUCTS = [{ id: "fdf8af0c" }, { id: "76109c58" }];

test("an unset product id selects the placeholder, never the first product", () => {
  // The measured defect: with no id the browser used to display PRODUCTS[0]
  // because the list had no empty option. The value the fix returns is "", and
  // the page renders an explicitly empty option FIRST — so the control shows
  // "Choose a product…".
  assert.equal(productSelectValue(undefined, PRODUCTS), "");
  assert.equal(productSelectValue("", PRODUCTS), "");
  assert.deepEqual(
    unpointedProductBlocks([{ id: "q5uhvoy", type: "product" }], PRODUCTS),
    ["q5uhvoy"],
    "a product block with no product id is not reported — the buyer warning would never fire",
  );
  // The source must render that placeholder before the products, or the browser
  // falls back to the first product again.
  const select = builderPage.slice(
    builderPage.indexOf('id={fid("productId")}'),
    builderPage.indexOf('{products.map((p) => ('),
  );
  assert.ok(select.length > 0, "the product <select> is gone — this guard cannot see the control any more");
  assert.match(
    select,
    /<option value="">\{t\("bld\.chooseProduct"\)\}<\/option>/,
    'the product <select> has no empty placeholder option — an unset productId would display the first product again',
  );
  assert.match(
    select,
    /value=\{productSelectValue\(b\.productId, products\)\}/,
    "the <select> is not bound to productSelectValue — a stale or unset id can display a product the block does not name",
  );
});

test("a block that names an offered product keeps it (the inverse arm)", () => {
  assert.equal(productSelectValue("76109c58", PRODUCTS), "76109c58");
  assert.deepEqual(
    unpointedProductBlocks([{ id: "b_prod", type: "product", productId: "76109c58" }], PRODUCTS),
    [],
    "a valid selection was reported unpointed — the fix would blank a working product block",
  );
  assert.equal(
    paymentFormCannotTakeOrders("payment", [{ id: "b_prod", type: "product", productId: "76109c58" }], PRODUCTS),
    false,
    "the builder would show the not-accepting-orders warning on a healthy form",
  );
});

test("a product the list no longer offers shows the placeholder too (purged sibling)", () => {
  // A block pointing at a purged (or archived-out-of-the-picker) product: the
  // stored id is not offered, so the control must show the placeholder rather
  // than fall back to the first product.
  assert.equal(productSelectValue("purged-0000", PRODUCTS), "");
  assert.deepEqual(
    unpointedProductBlocks([{ id: "b_prod", type: "product", productId: "purged-0000" }], PRODUCTS),
    ["b_prod"],
  );
});

test("only product blocks count, and response forms are never flagged", () => {
  const blocks = [
    { id: "b1", type: "text" },
    { id: "b2", type: "email" },
    { id: "b3", type: "option", productId: "b3" },
  ];
  assert.deepEqual(unpointedProductBlocks(blocks, PRODUCTS), []);
  assert.equal(paymentFormCannotTakeOrders("payment", blocks, PRODUCTS), false);
  // A response form never takes orders by design, so an unpointed product block
  // left over from a mode switch is not the warning's business.
  assert.equal(
    paymentFormCannotTakeOrders("response", [{ id: "b_prod", type: "product" }], PRODUCTS),
    false,
  );
});

test("the builder renders the warning with the buyer's sentence and a link to the block", () => {
  assert.match(
    builderPage,
    /\{mode !== "response" && unpointedBlockIDs\.length > 0 && \(/,
    "the persistent not-accepting-orders banner is not rendered from the shared predicate",
  );
  assert.match(builderPage, /t\("bld\.noProductSelectedWarn"\)/, "the banner does not quote the buyer sentence");
  // The link goes to the product block itself: the card tag carries
  // `jf-block-<id>` and the banner's href targets the same id.
  assert.match(builderPage, /id=\{`jf-block-\$\{b\.id\}`\}/, "block cards carry no anchor id, so the warning cannot link to one");
  assert.match(builderPage, /href=\{`#jf-block-\$\{unpointedBlockIDs\[0\]\}`\}/, "the banner does not link to the product block");
});

test("the /forms row shows a not-accepting-orders pill instead of a clean LIVE pill", () => {
  assert.match(
    formsPage,
    /not_accepting_orders\?: boolean;/,
    "the /forms row type does not read the API's not_accepting_orders field",
  );
  assert.match(
    formsPage,
    /f\.not_accepting_orders \? \(/,
    "the LIVE pill does not check the dead-form signal — a published-but-dead form still looks healthy",
  );
  assert.match(formsPage, /t\("forms\.notAcceptingOrders"\)/, "the dead-form pill renders no sentence");
  assert.match(formsPage, /t\("forms\.notAcceptingHint"\)/, "the dead-form pill has no accessible explanation");
});
