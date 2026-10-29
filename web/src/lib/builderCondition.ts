/**
 * Whether a block's stored conditional rule points at a field that is NOT
 * available as a condition source on this form (card t_a81a9165).
 *
 * THE DEFECT THIS ANSWERS. The block-settings panel renders the rule picker as
 * a `<select>` whose `value` is the stored `condition.field_id` and whose
 * options are derived from the LIVE `blocks` list. When a stored id matches no
 * option, the browser reports the select's value as `""` — indistinguishable
 * from the first option, "— Always show —". So a form whose rule was orphaned
 * (a builder that minted a new id per load, before lib/blockHydrate; a text
 * block retyped to an intro/section; a hand-written config) was painted as
 * ALWAYS SHOWN while `b.condition` was still non-null and the save wrote the
 * broken rule straight back. Meanwhile the SERVER still treats the block as
 * conditional: `formfields.Condition.Met` reads `answers[field_id]`, gets `""`,
 * evaluates the rule FALSE, and `VisibleAnswers` deletes that block's answer —
 * the field is permanently hidden on the public page and its answers are
 * silently discarded.
 *
 * WHY THE REPAIR IS "SHOW IT", NOT "DROP IT". Dropping the rule on load would
 * quietly re-enable a field the merchant may have deliberately hidden, which
 * changes what buyers see on a LIVE form without anyone asking. So the rule is
 * kept byte-for-byte and the panel names the unknown id as unavailable, which
 * gives the merchant the only two honest exits: retarget the rule at a real
 * field, or choose "Always show" to remove it.
 *
 * WHAT COUNTS AS UNAVAILABLE. Anything the picker cannot offer as a source:
 *
 *   * an id no block carries (the pre-hydration orphan — the card's case);
 *   * an id whose block is not an answer field — e.g. a `text` field retyped to
 *     an intro `section` by the builder's own "Treat as intro line" action, or
 *     a product/section block. The picker only lists field types, and the
 *     server's answers map only carries field types, so the rule can never
 *     match on either side;
 *   * an id equal to the block's OWN id — a self-reference the picker refuses
 *     to offer (`x.id !== b.id`), and one the server evaluates against the very
 *     answer the rule guards.
 *
 * An empty / absent id is NOT unavailable: that is a block with no rule at all,
 * which is the overwhelmingly common case and must stay silent.
 */
export type ConditionSourceBlock = { id: string; type: string };

export function conditionTargetUnavailable(
  blocks: readonly ConditionSourceBlock[],
  selfId: string,
  fieldId: string | null | undefined,
  fieldTypes: readonly string[],
): boolean {
  if (typeof fieldId !== "string" || fieldId.trim() === "") return false;
  return !blocks.some(
    (x) => x.id === fieldId && x.id !== selfId && fieldTypes.includes(x.type),
  );
}
