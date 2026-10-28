/**
 * A legacy `text` block whose value reads like PROSE (card t_4823943a).
 *
 * WHAT THE BLOCK IS. A `text` block is a single-line answer INPUT: the public
 * page paints `<label class="lbl">{value}</label><input type="text" name="<id>">
 * (render_blocks.go, both renderers), and the builder offers it placeholder /
 * max length / default value / pattern. Its `value` is the field's LABEL.
 *
 * WHAT THE MERCANTILE FORMS STILL CARRY. Under the old content model `text`
 * meant prose, and the seeded template library wrote its intro line into one.
 * The library was repaired (card t_d46c21e0); merchant forms were not. Measured
 * read-only on prod 2026-10-20: ten rows (`NOT is_template AND deleted_at IS
 * NULL`) still hold a non-empty `text` value, and the sentence renders as the
 * LABEL of an empty input:
 *
 *   <label class="lbl">Register for our event — cuma rsvp ja</label>
 *   <input type="text" name="qoubfpj" data-id="qoubfpj">
 *
 * WHY THIS IS A WARNING AND NOT A MIGRATION. `text` is a legitimate label too
 * (`Abc`, `Nama anda`, `Nombor telefon bimbit`), and no reading of the stored
 * bytes can tell a merchant's field label from a merchant's intro sentence —
 * that distinction exists only in the merchant's head. A blanket sweep would
 * rewrite real fields. So the builder DETECTS the shape, says what will happen
 * on the live form, and offers an explicit action; nothing is ever rewritten
 * silently (the ten prod rows are handled per-row, with Wan's authorisation, by
 * card t_935da2bc — not by this module).
 *
 * THE DETECTOR. Three signals, exactly the ones the card names — sentence
 * punctuation, word count, length — and a threshold set so that every shape a
 * LABEL really takes stays quiet:
 *
 *   · six or more words          — a field label is a couple of words.
 *   · four or more words ending in . ! ? … — a sentence, not a caption.
 *   · three or more words carrying a line break — a label is one line.
 *   · three or more words and 40+ characters — a long caption.
 *
 * `Abc` (1 word), `Nama anda` (2), `No. telefon` (2, and the dot does not end
 * the value), `Isi alamat emel anda` (4, no terminator) all stay quiet; the
 * measured prod rows do not. The unit tests pin both directions.
 *
 * This module is deliberately pure and free of React/JSX: `npm run test:unit`
 * runs it under node --test (web/src/lib/textBlockProse.test.ts), and the
 * builder imports the same functions it is tested through.
 */

/** A builder block, narrowed to the two keys this module reads and guarantees. */
export type RetypeableBlock = {
  id?: string;
  type: string;
  value?: string;
  [key: string]: unknown;
};

/** Sentence terminators, ASCII and typographic. */
const TERMINATOR = /[.!?…]["'”’)\]]*$/;

/** Whitespace-separated words. `—` counts as a word, exactly as it reads. */
function wordsOf(value: string): string[] {
  return value.trim().split(/\s+/).filter(Boolean);
}

/**
 * True when the block's value reads like a sentence rather than a field label.
 * Empty / whitespace-only values are never prose (an empty label is a different,
 * already-visible problem — the builder shows the id — not this one).
 */
export function looksLikeProse(value: string | undefined | null): boolean {
  if (typeof value !== "string") return false;
  const trimmed = value.trim();
  if (trimmed === "") return false;
  const words = wordsOf(trimmed);
  if (words.length >= 6) return true;
  if (words.length >= 4 && TERMINATOR.test(trimmed)) return true;
  if (words.length >= 3 && /[\r\n]/.test(trimmed)) return true;
  if (words.length >= 3 && trimmed.length >= 40) return true;
  return false;
}

/**
 * Retype ONE block, keeping every other key — including `id` and `value` — byte
 * for byte. The block is matched by id AND by its current type, so a stale
 * click (or a block the merchant already converted) cannot retype the wrong
 * thing; when neither matches, the array is returned unchanged with the SAME
 * entries (no copy of an untouched block, so an equal-by-JSON round trip is
 * exactly the stored shape).
 *
 * The spread preserves the insertion order of the original keys — `type`
 * already exists, so overriding it does not move it — which is why
 * `JSON.stringify(treatAsFieldLabel(treatAsIntro(blocks, id), id))` is byte-
 * identical to `JSON.stringify(blocks)`. The unit test asserts that instead of
 * asserting a toast.
 */
export function retypeBlock<T extends RetypeableBlock>(
  blocks: readonly T[],
  blockId: string,
  fromType: string,
  toType: string,
): T[] {
  return blocks.map((b) => (b.id === blockId && b.type === fromType ? ({ ...b, type: toType } as T) : b));
}

/**
 * The explicit action the warning offers: `text` (an answer field whose value
 * is its label) becomes `section` (display prose). Nothing else changes — the
 * id stays the field name a stored contract may name, and the value stays the
 * sentence the merchant wrote.
 */
export function treatAsIntro<T extends RetypeableBlock>(blocks: readonly T[], blockId: string): T[] {
  return retypeBlock(blocks, blockId, "text", "section");
}

/**
 * The exact inverse: `section` becomes `text` again. Offered only for the block
 * a merchant converted in this session (the builder keeps that one id), so a
 * `section` the merchant authored as prose is never retyped by this path.
 */
export function treatAsFieldLabel<T extends RetypeableBlock>(blocks: readonly T[], blockId: string): T[] {
  return retypeBlock(blocks, blockId, "section", "text");
}
