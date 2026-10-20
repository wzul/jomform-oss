// A block type the palette no longer OFFERS, but that a saved form still
// carries (card t_3abab22c).
//
// WHY THIS EXISTS. When a block is withdrawn from the builder palette, the
// withdrawal is about what a merchant can ADD — not about forms saved before
// it. Those forms still load, and the canvas still has to draw their blocks
// honestly. Two surfaces read the block type directly and both used to trust it:
//
//   * `typeLabel` built `t("blk.<type>")` and `t()` falls back to the key
//     itself, so a block whose dictionary entry went with the withdrawal
//     rendered its raw key on the card's badge (measured live as `BLK.COUPON`
//     once the card's uppercase styling was applied).
//   * `BlockPreview`'s `default` arm draws an empty, bordered, input-shaped box
//     — correct for a real field, and a lie for a block the public renderers
//     serve as a label plus a note instead of a control.
//
// So: a label even when the dictionary has no entry for the key, and a
// predicate for "the palette does not offer this type any more" so the canvas
// can draw the note rather than the control sketch.
//
// What this does NOT do: it never re-adds the surface. The palette, the
// dictionary entry and the hint stay withdrawn; these helpers only make the
// STORED block readable.

/**
 * The human label for a block type.
 *
 * `translated` is what the dictionary returned for `key`. When the dictionary
 * still has the key, that translation is the answer. When it does not, `t()`
 * hands the key back verbatim — which is never a label a merchant should read —
 * so the last segment of the key is turned into words instead:
 *
 *   "blk.coupon"      -> "Coupon"
 *   "blk.image-choice" -> "Image choice"
 *
 * The key is DATA here (a stored block's type), never a literal in the
 * withdrawal guard's scanned source: the point is that no surface names the
 * withdrawn block, while a form that still stores it gets a real label.
 */
export function blockTypeLabel(key: string, translated: string): string {
  if (translated !== key) return translated;
  const tail = key
    .slice(key.lastIndexOf(".") + 1)
    .replace(/[-_]+/g, " ")
    .trim();
  if (!tail) return key;
  return tail.charAt(0).toUpperCase() + tail.slice(1);
}

/**
 * True when `type` is NOT one of the types the palette still offers.
 *
 * `offered` is the palette's own type set (built from PALETTE_GROUPS at the
 * call site), so there is one list of what the builder offers and no second one
 * to drift from it. A type absent from it can only come from a saved form.
 */
export function isWithdrawnBlockType(type: string, offered: ReadonlySet<string>): boolean {
  return !offered.has(type);
}
