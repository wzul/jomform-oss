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
import {
  paymentFormCannotTakeOrders,
  productBlockState,
  productBlockStates,
  productSelectValue,
  retiredProductBlocks,
  unpointedProductBlocks,
} from "./productPick.ts";

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
  // A block pointing at a PURGED product: the stored id is not offered, so the
  // control must show the placeholder rather than fall back to the first product.
  // (Card t_d5112f02: an ARCHIVED product is not this arm — see the archived test
  // below, which needs the row set to tell the two apart.)
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

// ─── Card t_d5112f02: an ARCHIVED product is NOT a refusal ───────────────────
//
// THE DEFECT: the picker is active-only, so a block bound to an ARCHIVED product
// showed "Choose a product…" AND the persistent "buyers see: This form is not
// accepting orders right now" banner — while the API called the form healthy.
// The product row still exists, the buyer page resolves the block from the form's
// saved snapshot, and `dbstore.PublishWarning` reports only a PURGED product (it
// takes the account's ROW set, not the on-sale set). Two answers to one question,
// in the opposite direction from the bug #1717 fixed.
//
// The direction of the fix (the card allowed either): keep the notice but tell the
// truth — a "product archived" notice that does NOT quote the buyer's sentence,
// which is only shown for a block the buyer page will really refuse.

// The row set the two reads produce: one active product, one archived, one in the
// bin. `dbstore.ProductIDsForAccount` answers the same three.
const LIVE_ROWS = ["fdf8af0c", "archived-1111", "trashed-2222"];

test("a block bound to an ARCHIVED product is a notice, never the buyer's refusal", () => {
  const block = { id: "b_prod", type: "product", productId: "archived-1111" };
  // The picker still cannot display it (the list is active-only) — that is not
  // what changes.
  assert.equal(productSelectValue("archived-1111", PRODUCTS), "");
  // …but the block is NOT one that makes the buyer page refuse: its row still
  // exists, so the snapshot fallback serves it.
  assert.deepEqual(unpointedProductBlocks([block], PRODUCTS, LIVE_ROWS), [], "an archived product was reported as a buyer refusal — the false alarm this card removes");
  assert.deepEqual(retiredProductBlocks([block], PRODUCTS, LIVE_ROWS), ["b_prod"], "the archived block got no notice at all");
  assert.equal(
    paymentFormCannotTakeOrders("payment", [block], PRODUCTS, LIVE_ROWS),
    false,
    "the builder would quote \"buyers see: This form is not accepting orders right now\" for a form the API calls healthy",
  );
  // The same classification as the module's own vocabulary, so the page's two
  // arrays cannot both claim one block.
  assert.equal(productBlockState(block, PRODUCTS, LIVE_ROWS), "retired");
  assert.deepEqual(productBlockStates([block], PRODUCTS, LIVE_ROWS), { unset: [], purged: [], retired: ["b_prod"] });
});

test("a product in the bin is the same notice as an archived one", () => {
  // The API's row set includes the bin (ProductIDsForAccount: active + archived +
  // trashed), and the buyer page falls back to the snapshot for those too. If the
  // builder called this one "purged" the two surfaces would disagree again, just
  // more rarely.
  const block = { id: "b_prod", type: "product", productId: "trashed-2222" };
  assert.deepEqual(unpointedProductBlocks([block], PRODUCTS, LIVE_ROWS), []);
  assert.deepEqual(retiredProductBlocks([block], PRODUCTS, LIVE_ROWS), ["b_prod"]);
});

test("the PURGED arm still quotes the buyer's sentence (the fix must not soften it)", () => {
  // The inverse arm. A purged product has NO row, so nothing can resolve it: the
  // page serves the submit-less refusal, and the builder must keep saying so.
  const block = { id: "b_prod", type: "product", productId: "purged-0000" };
  assert.deepEqual(unpointedProductBlocks([block], PRODUCTS, LIVE_ROWS), ["b_prod"]);
  assert.deepEqual(retiredProductBlocks([block], PRODUCTS, LIVE_ROWS), []);
  assert.equal(paymentFormCannotTakeOrders("payment", [block], PRODUCTS, LIVE_ROWS), true);
  assert.equal(productBlockState(block, PRODUCTS, LIVE_ROWS), "purged");
});

test("an unreadable row set falls back to the pre-fix reading, never to false comfort", () => {
  // `undefined` = the two reads failed. The builder then cannot tell archived from
  // purged, and must pick the SAFE side: report the block (a false alarm) rather
  // than tell the merchant buyers are fine when they may not be.
  const block = { id: "b_prod", type: "product", productId: "archived-1111" };
  assert.deepEqual(unpointedProductBlocks([block], PRODUCTS, undefined), ["b_prod"]);
  assert.deepEqual(retiredProductBlocks([block], PRODUCTS, undefined), []);
  assert.equal(paymentFormCannotTakeOrders("payment", [block], PRODUCTS, undefined), true);
});

test("the builder reads the ROW set the API's publish guard reads, and renders the notice", () => {
  // The row set has to come from the two reads that together give
  // dbstore.ProductIDsForAccount's answer: `?include_archived=1` (active +
  // archived) and the bin's own list. The Go side has
  // TestBuilderRowSetMatchesThePublishGuard in
  // api/internal/httpapi/builder_product_rows_t_d5112f02_test.go, which asserts the
  // UNION of these two responses equals ProductIDsForAccount using real rows — so a
  // change to either the URLs or the store query fails one of the two tests.
  assert.match(
    builderPage,
    /readLiveProductIDs\(\): Promise<string\[\] \| undefined>/,
    "the builder has no reader for the account's product ROW set",
  );
  assert.match(
    builderPage,
    /read\("\/api\/rest\/products\?include_archived=1"\)/,
    "the row-set read does not ask for archived rows — every archived product would look purged",
  );
  assert.match(
    builderPage,
    /read\("\/api\/rest\/products\/trash"\)/,
    "the row-set read does not ask for the bin — a trashed product would look purged",
  );
  // The scope must go through workspaceUrl: a hand-built
  // `?include_archived=1${workspaceQuery()}` produces two `?` and silently scopes
  // the read to the caller's FIRST workspace (lib/workspace.ts documents the case).
  assert.match(builderPage, /workspaceUrl\(url\)/, "the row-set read does not scope the URL through workspaceUrl");
  // Both answers come from ONE pass, so the two banners are disjoint.
  assert.match(
    builderPage,
    /const productStates = productBlockStates\(blocks, products, liveProductIDs\);/,
    "the builder does not classify the blocks with the row set",
  );
  assert.match(
    builderPage,
    /const unpointedBlockIDs = \[\.\.\.productStates\.unset, \.\.\.productStates\.purged\];/,
    "the refusal banner is not the unset+purged arm",
  );
  assert.match(
    builderPage,
    /const retiredBlockIDs = productStates\.retired;/,
    "the archived notice is not the retired arm",
  );
  // …and the archived notice is rendered, as a NOTICE (role=status, not the
  // refusal's role=alert) and never with the buyer's sentence.
  const notice = builderPage.slice(
    builderPage.indexOf('data-testid="builder-product-archived"'),
    builderPage.indexOf('data-testid="builder-product-archived"') + 900,
  );
  assert.ok(notice.length > 0, "the archived-product notice is gone");
  assert.match(notice, /t\("bld\.productArchivedWarn"\)/, "the archived notice renders no sentence");
  assert.match(notice, /href=\{`#jf-block-\$\{retiredBlockIDs\[0\]\}`\}/, "the archived notice does not link to the block");
  assert.doesNotMatch(notice, /bld\.noProductSelectedWarn/, "the archived notice quotes the buyer's refusal — that is the false alarm this card removes");
  assert.match(
    builderPage,
    /\{mode !== "response" && retiredBlockIDs\.length > 0 && \(/,
    "the archived notice is not gated on the retired arm",
  );
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
