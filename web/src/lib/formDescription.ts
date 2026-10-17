// Card t_7fb80479 — the ONE rule for a form's description, shared by the
// builder's save path and the two API readers (the REST template gallery and
// MCP list_templates; api/internal/formfields/formfields.go's Description) so
// the dashboard and the API cannot drift.
//
// A form's description is its INTRO LINE: the first `section`/`html` block's
// value. It used to be the first `text` block's value, which was right while
// `text` meant prose — `text` is a single-line text INPUT now (both public
// renderers paint `<input type="text">` and its value is the field's LABEL),
// so reading it described a form with a field label: the reported form, whose
// one block was a name field labelled "Cuba. Nama", would publish "Cuba. Nama"
// as its description.
//
// A block list written under the old content model — the seeded template
// library kept its intro in a `text` block until card t_d46c21e0 migrated
// those rows, and migration 000060 prepended the same shape to three legacy
// templates — still falls back to the first `text` block, so a template's card
// keeps the description it has on either side of that migration.
//
// The caller owns its own length cap: the builder stores at most 300
// characters (the form-settings limit), the API derivations do not truncate.
export type DescribeBlock = { type: string; value?: string };

/** The block types that carry display copy rather than an answer. */
export const PROSE_BLOCK_TYPES: readonly string[] = ["section", "html"];

export function formDescription(blocks: readonly DescribeBlock[]): string {
  let legacy = "";
  for (const b of blocks) {
    const v = (b.value || "").trim();
    if (!v) continue;
    if (PROSE_BLOCK_TYPES.includes(b.type)) return v;
    if (b.type === "text" && !legacy) legacy = v;
  }
  return legacy;
}
