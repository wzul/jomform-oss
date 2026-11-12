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
// Card t_d5112f02 (the follow-up): the placeholder and the buyer's refusal are
// TWO different facts. An archived product is out of the picker (so the select
// shows the placeholder) but still HAS a row, and the buyer page resolves the
// block through the form's saved snapshot — that form takes orders today, and
// the API's `/forms` flags agree it is healthy. `retiredProductBlocks` is that
// third answer, and it never quotes the buyer's sentence.
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
 * The four ways a product block can answer "can this block sell something?".
 *
 * Card t_d5112f02: "the picker shows the placeholder" and "the buyer is refused"
 * used to be ONE answer, and they are not. The picker is ACTIVE-only, so it
 * cannot display an archived product — but an archived product still has a row,
 * and the buyer page resolves the block through the form's saved snapshot, so
 * that form still takes orders. The API says exactly this
 * (`dbstore.PublishWarning` takes the account's ROW set and reports only a PURGED
 * product), so the builder has to ask the row question too or the two disagree.
 *
 *   - bound   — names an active product the picker offers: nothing to say.
 *   - unset   — names no product at all: the buyer page refuses.
 *   - purged  — names an id with NO row: the buyer page refuses.
 *   - retired — names a row that exists but is off sale / in the bin: the buyer
 *               page still sells it from the snapshot, so this is a notice, not a
 *               refusal, and it must not quote the buyer's sentence.
 *
 * `liveProductIDs` is that row set (active + archived + trashed — the same set
 * `dbstore.ProductIDsForAccount` hands the API). `undefined` means the builder
 * could not read it; an id it cannot see is then treated as PURGED, which is the
 * pre-t_d5112f02 reading: a false alarm at worst, never a false "buyers are fine".
 */
export type ProductBlockState = "bound" | "unset" | "purged" | "retired";

/** The block ids of a form, grouped by the state above (block order preserved). */
export type ProductBlockStates = { unset: string[]; purged: string[]; retired: string[] };

/**
 * productBlockState classifies ONE block. A non-product block is always "bound"
 * (nothing to do); an id the picker offers is "bound" too.
 */
export function productBlockState(
  block: PickableBlock,
  products: PickableProduct[],
  liveProductIDs?: readonly string[],
): ProductBlockState {
  if (block.type !== "product") return "bound";
  if (productSelectValue(block.productId, products) !== "") return "bound";
  if (!block.productId) return "unset";
  if (liveProductIDs && liveProductIDs.includes(block.productId)) return "retired";
  return "purged";
}

/**
 * productBlockStates groups a form's product blocks by state.
 *
 * The two arrays the builder renders come from here, so the buyer-sentence banner
 * and the archived notice cannot both claim the same block.
 */
export function productBlockStates(
  blocks: PickableBlock[],
  products: PickableProduct[],
  liveProductIDs?: readonly string[],
): ProductBlockStates {
  const out: ProductBlockStates = { unset: [], purged: [], retired: [] };
  for (const b of blocks) {
    const state = productBlockState(b, products, liveProductIDs);
    if (state === "unset") out.unset.push(b.id);
    else if (state === "purged") out.purged.push(b.id);
    else if (state === "retired") out.retired.push(b.id);
  }
  return out;
}

/**
 * unpointedProductBlocks returns the ids of the product blocks that make a
 * payment form serve "This form is not accepting orders right now": no product
 * id at all, or one with no product row left (purged).
 *
 * A block pointing at an ARCHIVED product is deliberately NOT returned: its row
 * still exists and the buyer page still sells it from the snapshot (card
 * t_d5112f02 — see retiredProductBlocks for what the builder says instead).
 */
export function unpointedProductBlocks(
  blocks: PickableBlock[],
  products: PickableProduct[],
  liveProductIDs?: readonly string[],
): string[] {
  const s = productBlockStates(blocks, products, liveProductIDs);
  return [...s.unset, ...s.purged];
}

/**
 * retiredProductBlocks returns the ids of the product blocks bound to a product
 * that still HAS a row but is not on sale (archived, or in the bin).
 *
 * These blocks get their own notice and never the buyer's sentence: the form
 * takes orders today, and the API's `/forms` row flags say so
 * (`not_accepting_orders` is false). Saving the builder re-writes the form's
 * product snapshot from the ACTIVE list, which is the only thing that changes
 * here — hence a notice rather than a refusal.
 */
export function retiredProductBlocks(
  blocks: PickableBlock[],
  products: PickableProduct[],
  liveProductIDs?: readonly string[],
): string[] {
  return productBlockStates(blocks, products, liveProductIDs).retired;
}

/**
 * paymentFormCannotTakeOrders reports whether the builder should show the
 * buyer-sentence warning for this form: a payment-mode form with at least one
 * block that makes the buyer page refuse (no product, or a purged one).
 *
 * A response form never takes orders by design, so it is never flagged.
 */
export function paymentFormCannotTakeOrders(
  mode: string,
  blocks: PickableBlock[],
  products: PickableProduct[],
  liveProductIDs?: readonly string[],
): boolean {
  if (mode === "response") return false;
  return unpointedProductBlocks(blocks, products, liveProductIDs).length > 0;
}
