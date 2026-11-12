// The merchant-side product picker, and the warning that says what the buyer
// will be told (card t_983425e5).
//
// THE DEFECT, measured on prod: the builder rendered the product block's
// <select> with `value={b.productId || ""}` and NO empty placeholder <option>.
// When the block had never been pointed at a product, the control's value was
// "" while the browser displayed the FIRST product — so the merchant read
// "derma — RM1.00" as a selection, saved, and the stored block was
// `{"type":"product","value":"","quantity":true}` with no `product_id` at all.
// The buyer then got "This form is not accepting orders right now". Wan, mid
// demo: "aku dh set la produk kat situ?"
//
// The rule this module encodes: A SELECT MAY ONLY DISPLAY A PRODUCT THE BLOCK
// ACTUALLY NAMES. When the stored id is empty — or names a product that is no
// longer in the list (purged, or archived out of the picker) — the control shows
// the placeholder, and `unpointedProductBlocks` names the block so the builder
// can point at it.
//
// It lives in a plain .ts module because this repo has no JSX test runner (the
// same pattern as lib/formMode.ts and lib/publishResult.ts): node --test can
// import it directly, and the builder's call sites are asserted from source so
// the fix cannot become dead code behind a green suite.

/** The minimum a product needs for this module. */
export type PickableProduct = { id: string };

/** The minimum a block needs: its id and type, plus the optional product link. */
export type PickableBlock = { id: string; type: string; productId?: string };

/**
 * productSelectValue returns the value the product <select> must carry.
 *
 * It is the block's stored id ONLY when the list actually offers that id.
 * Otherwise "" — the placeholder option — so the control can never show product
 * A while the block is bound to nothing (or to a product that was deleted).
 */
export function productSelectValue(productId: string | undefined, products: PickableProduct[]): string {
  if (!productId) return "";
  return products.some((p) => p.id === productId) ? productId : "";
}

/**
 * unpointedProductBlocks returns the ids of the product blocks whose picker would
 * show the placeholder: no product id at all, or one the account's live product
 * list no longer contains.
 *
 * These are the blocks that make a payment form serve "This form is not accepting
 * orders right now" — the builder shows the warning and links to the first one.
 * A block pointing at a product the list still offers is NOT returned (the
 * inverse arm: a valid selection must never be flagged).
 */
export function unpointedProductBlocks(blocks: PickableBlock[], products: PickableProduct[]): string[] {
  return blocks
    .filter((b) => b.type === "product" && productSelectValue(b.productId, products) === "")
    .map((b) => b.id);
}

/**
 * paymentFormCannotTakeOrders reports whether the builder should show the
 * buyer-sentence warning for this form: a payment-mode form with at least one
 * unpointed product block.
 *
 * A response form never takes orders by design, so it is never flagged.
 */
export function paymentFormCannotTakeOrders(
  mode: string,
  blocks: PickableBlock[],
  products: PickableProduct[],
): boolean {
  if (mode === "response") return false;
  return unpointedProductBlocks(blocks, products).length > 0;
}
