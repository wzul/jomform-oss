// Canvas row reordering for the form builder (web/src/app/builder/page.tsx).
//
// THE BUG
// The canvas block card exposes ↑ / ↓ buttons. They were wired as
//
//   up   : move(i, i - 1)
//   down : move(i, i + 1)
//
// with `move(from, to)` delegating to `insertAt(to, null, from)`. `insertAt`
// resolves its index against the array as it stood BEFORE the dragged row was
// removed — that is the semantics the drag-and-drop gap zones need, because
// those call sites pass a GAP index (`canvasDrop(e, i)` on the gap before
// block `i`, `canvasDrop(e, i + 1)` on the block itself). Its target expression
// is `fromIdx < i ? i - 1 : i`.
//
// For the ↓ button `to = from + 1`, so `fromIdx < i` is true and the target
// collapses to `(from + 1) - 1 = from`: the row was spliced out and re-inserted
// at the index it already occupied. Every ↓ click was therefore a byte-for-byte
// no-op at every position, while ↑ (`to = from - 1` → target `from - 1`) worked.
//
// THE FIX
// A row move is a swap of two adjacent rows — the same shape the other reorder
// controls in this repo already use (`moveImage` in app/products/page.tsx,
// `moveVariation` in app/products/ProductEditor.tsx). Expressed as a swap it is
// symmetric, so ↑ and ↓ cannot disagree about the arithmetic, and the bound
// check reads the same at both ends. `insertAt` is left untouched and still
// serves the drag path.
//
// Kept out of the component so it is testable with the repo's Node test runner
// (there is no JSX/component test setup here — see src/lib/templatePicker.ts
// for the same split).

/**
 * Move the row at `from` one position in direction `dir` (-1 up, +1 down).
 *
 * Returns the SAME array reference when the move is impossible (first row
 * moving up, last row moving down, or an out-of-range index), so a click at a
 * boundary state changes nothing. Otherwise returns a new array — the input is
 * never mutated.
 */
export function moveBlock<T>(rows: T[], from: number, dir: -1 | 1): T[] {
  const to = from + dir;
  if (from < 0 || from >= rows.length) return rows;
  if (to < 0 || to >= rows.length) return rows;
  const next = rows.slice();
  [next[from], next[to]] = [next[to], next[from]];
  return next;
}

/** True when a row at `index` has a neighbour in direction `dir`. */
export function canMove<T>(rows: T[], index: number, dir: -1 | 1): boolean {
  const to = index + dir;
  return index >= 0 && index < rows.length && to >= 0 && to < rows.length;
}
