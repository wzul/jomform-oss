// The composer's auto-growing height (card t_148d7100).
//
// THE DEFECT. The box was `rows={2}` + `resize-none`: a fixed two-line window
// onto a message the page accepts up to 2,000 characters of. A pasted question
// was readable two lines at a time, and the handle that used to let a user drag it
// bigger was removed by `resize-none` — so the control was small AND not
// adjustable.
//
// THE RULE, and why it is arithmetic rather than CSS: the height has to follow
// the CONTENT (`scrollHeight`) but stop at a ceiling, after which the box scrolls.
// A CSS `max-height` on its own cannot grow with the content (textarea does not
// size to content), and a JS measurement that is not capped turns a long paste
// into a composer taller than the transcript. So: measured height, clamped to
// `maxRows` lines plus the box's own padding/border, never below one line.
//
// It is a pure function of three numbers the browser reports, so
// src/lib/chatComposerGrow.test.ts executes the rule without a DOM.

/** How many lines the composer may grow to before it starts scrolling. */
export const CHAT_COMPOSER_MAX_ROWS = 8;

/**
 * The height, in pixels, the composer should be given.
 *
 * scrollHeight is the browser's own measurement of the content INCLUDING the
 * element's vertical padding, so `chromePx` (padding + border) is carried through
 * the ceiling rather than added to the measurement.
 */
export function composerHeight(
  scrollHeight: number,
  lineHeight: number,
  chromePx: number,
  maxRows: number = CHAT_COMPOSER_MAX_ROWS,
): number {
  const oneLine = lineHeight + chromePx;
  const ceiling = lineHeight * maxRows + chromePx;
  if (!Number.isFinite(scrollHeight) || scrollHeight <= 0) return oneLine;
  return Math.min(Math.max(scrollHeight, oneLine), ceiling);
}
