// Whether the transcript should follow the answer (card t_148d7100).
//
// THE DEFECT. The page called `scrollIntoView` on EVERY change of `msgs.length`
// and `sending`, so a user who had scrolled UP to re-read an earlier answer was
// yanked back to the bottom the moment the next message arrived — and while a
// streamed answer grew, it would have been every few hundred milliseconds.
// Measured on the staging build: the log had no "jump to latest" affordance at
// all, because nothing was ever allowed to stay scrolled up.
//
// THE DECISION IS THIS FUNCTION, not the page. `isAtBottom` is a pure predicate on
// three numbers the browser reports, so the rule is executed by
// src/lib/chatScroll.test.ts instead of being inferred from a rendering test.
//
// WHY A TOLERANCE. Fractional device pixel ratios and the browser's own rounding
// leave a scrolled-to-the-end box a pixel or two short of `scrollHeight`. A strict
// equality would decide the user had scrolled away from an answer they are in
// fact reading at the bottom, and the auto-scroll would stop working for them.

/** How far from the end still counts as "at the bottom". */
export const STICK_THRESHOLD_PX = 48;

export type ScrollBox = {
  scrollTop: number;
  scrollHeight: number;
  clientHeight: number;
};

/**
 * Should the view follow new content?
 *
 * A box whose content is shorter than the box itself (`scrollHeight <= clientHeight`)
 * is always "at the bottom": there is nothing to scroll away from, and treating a
 * short transcript as scrolled-up would hide the first answer behind a
 * "Jump to latest" button that does nothing.
 */
export function isAtBottom({ scrollTop, scrollHeight, clientHeight }: ScrollBox): boolean {
  if (scrollHeight <= clientHeight) return true;
  return scrollHeight - scrollTop - clientHeight <= STICK_THRESHOLD_PX;
}
