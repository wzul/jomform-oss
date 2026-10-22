/**
 * The ids a builder LOAD assigns to a stored form's blocks (card t_cbeafc7c).
 *
 * A block id is not a UI detail on this product: it is the field name the
 * public page emits (`name="<id>"` — render_blocks.go, both templates), the key
 * the page submits (`{fields: {<id>: value}}`) and the `field_id` a stored
 * `config.registered_fields` contract names. `formfields.Contract`
 * (api/internal/formfields) PREFERS that contract over the blocks, and the
 * response submit validates against it — so renaming a block on load does not
 * merely tidy the editor, it silently disowns the contract.
 *
 * MEASURED on prod v1.21.279 by card t_bf407d24: a form stored
 * `blocks: [{id:"b1"},{id:"b2",required:true,value:"Nama"}]` plus
 * `registered_fields: [{field_id:"b2", ...}]`. One click on Publish sent a
 * config whose blocks were `265kivw`/`db8x0qh` (the old `hydrateBlock` did
 * `id: uid()` unconditionally, so EVERY load renamed EVERY block) while the
 * contract still required `b2`. The public page rendered `name="db8x0qh"`, the
 * page's own submit sent `{265kivw:"", db8x0qh:"QA submit probe"}`, and the
 * route answered 400 `medan wajib tidak diisi: Nama` — for every visitor, on a
 * form that accepted submits before the save.
 *
 * The same id space carries three more references that a rename breaks:
 * a block's `condition.field_id` (conditional logic), a form notification's
 * `{field_id}` recipient, and the keys of submissions already stored.
 *
 * So a stored id is KEPT. Two repairs a stored config can still need:
 *
 *   * a block with no id (or a blank / non-string one) gets a fresh id — the
 *     builder cannot render or submit a control without one;
 *   * a DUPLICATE id is broken apart: two controls with one name collapse into
 *     a single submitted value, and the React keys of the canvas collide. The
 *     FIRST occurrence keeps the stored id (it is the one a stored contract can
 *     still name); later ones get an id that collides with nothing.
 *
 * The function is pure and takes the id generator as an argument, so the
 * decision is unit-testable without the page or Math.random.
 */
export function hydrateBlockIds(storedIds: unknown[], fresh: () => string): string[] {
  const taken = new Set<string>();

  // A generated id must not collide with a KEPT one either (a stored config can
  // legitimately already hold a 7-char id in uid()'s own alphabet). uid() is
  // random, so the loop exits immediately in practice; the deterministic tail
  // is what stops a hostile/broken `fresh` from spinning or duplicating.
  const mint = (): string => {
    for (let i = 0; i < 64; i++) {
      const id = fresh();
      if (id && !taken.has(id)) return id;
    }
    const base = (fresh() || "b").trim() || "b";
    for (let n = 2; ; n++) {
      const id = `${base}-${n}`;
      if (!taken.has(id)) return id;
    }
  };

  return storedIds.map((raw) => {
    const kept = typeof raw === "string" && raw.trim() !== "" && !taken.has(raw) ? raw : null;
    const id = kept ?? mint();
    taken.add(id);
    return id;
  });
}
