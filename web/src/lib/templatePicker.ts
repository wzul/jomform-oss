// Assembly rule for the New-form template picker (issues #174, #457, #583).
//
// The picker shows TWO independent sources:
//
//   1. the system gallery — GET /api/templates (published template forms), and
//   2. the merchant's own  — GET /api/rest/templates (issue #457: any
//      is_template row owned by the caller's account).
//
// Those two queries can return the SAME row: a template that is both published
// and owned by the caller's account satisfies both. That is not hypothetical —
// the three pre-#174 templates (tpl-fnb, tpl-booking, tpl-tuition) were
// inserted by hand into a merchant account, so the caller got each of them
// twice: once as a gallery card and once as an "own template" card.
//
// This module is the single place that turns the two responses into the cards
// the picker renders, so a row present in both sources is carded exactly once.
// Dedupe is on `id` (the row identity), never on title or slug: two different
// templates may legitimately share a title, and the same row always carries the
// same id. The gallery wins a tie because only the gallery list carries
// category/description, so the surviving card keeps those fields.
//
// Kept out of components/TemplatePicker.tsx so it is testable with the repo's
// Node test runner (there is no JSX/component test setup here).

export type PickerTemplate = {
  id: string;
  slug: string;
  title: string;
  description?: string;
  category?: string;
  mode?: string;
  theme?: { primary?: string };
};

export type PickerLists = {
  /** The merchant's own templates (issue #457). */
  own: PickerTemplate[];
  /** The system gallery. Its entry wins when a row appears in both. */
  gallery: PickerTemplate[];
};

/** Gallery entries first, then own-only entries — one entry per row id. */
export function mergePickerTemplates({ own, gallery }: PickerLists): PickerTemplate[] {
  const seen = new Set<string>();
  const out: PickerTemplate[] = [];
  for (const source of [gallery, own]) {
    for (const tpl of source) {
      // A row with no usable id can be neither deduped nor keyed by React, so
      // it is dropped rather than risking two cards for one row.
      if (!tpl || typeof tpl.id !== "string" || tpl.id === "") continue;
      if (seen.has(tpl.id)) continue;
      seen.add(tpl.id);
      out.push(tpl);
    }
  }
  return out;
}

/**
 * Splits the assembled list into the two sections the picker renders: entries
 * shown under "your templates" are own-only, and everything the gallery serves
 * is rendered from the gallery card (which is the richer one). Every assembled
 * entry lands in exactly one section, so the two sections can never both draw
 * the same row id.
 */
export function splitPickerTemplates(
  merged: PickerTemplate[],
  gallery: PickerTemplate[],
): { ownCards: PickerTemplate[]; galleryCards: PickerTemplate[] } {
  const galleryIDs = new Set(gallery.filter((t) => t && typeof t.id === "string").map((t) => t.id));
  const ownCards: PickerTemplate[] = [];
  const galleryCards: PickerTemplate[] = [];
  for (const tpl of merged) {
    if (galleryIDs.has(tpl.id)) galleryCards.push(tpl);
    else ownCards.push(tpl);
  }
  return { ownCards, galleryCards };
}
