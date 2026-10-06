"use client";

// The save button that confirms on ITSELF.
//
// WHY IT EXISTS
//   The Storefront tab's "Save footer" button was fixed (PR #997) to hold a
//   "Saved ✓" state for two seconds, because the page's confirmation only
//   rendered in a banner at the TOP of a very long page — several screens away
//   from the button the merchant actually pressed. That fix landed on ONE button,
//   hand-rolled inline. Every OTHER save button on the same page kept the old
//   behaviour, so the SAME product answered the same press two different ways:
//
//     "Save footer"        -> the button turns "Saved ✓" (green), and a toast
//     "Save landing page"  -> only a toast; the button never changes
//
//   Measured on staging 2026-10-06: pressing "Save landing page" showed the
//   toast "Landing page saved ✓" while the button's own label and colour stayed
//   exactly as before. A merchant who watches the button — the thing they just
//   clicked — sees nothing happen.
//
//   This is that behaviour, extracted so the next button gets it by construction
//   instead of by remembering to copy the inline version.
//
// WHY NOT JUST ADD THE STATE WHERE IT IS MISSING
//   Because the two implementations then drift, which is how this was found: the
//   footer's version and the landing page's version were written months apart and
//   only one of them was ever converted. The state machine (idle -> busy -> saved
//   -> idle) and its timer have three failure modes that each need the same
//   treatment, and copying them by hand is how one gets missed:
//
//     1. the timer must be cleared on unmount AND before re-arming, or saving
//        twice lets the FIRST save's stale timer flip the label back early;
//     2. the saved state must not outlive its window, or the button lies;
//     3. busy and saved must not be reachable at once.
//
// THE LABEL CONTRACT
//   `label` is the idle text and `savedLabel` the confirmation. They are passed
//   in rather than derived, because the saved label is usually a SHORTER form of
//   the idle one — a longer string widens the button mid-press, which reads as a
//   layout jump exactly when the user is looking at it. The existing footer pair
//   (`set.sfSave` / `set.sfSavedShort`) is the example; do not pass the long
//   confirmation here.

import { useCallback, useEffect, useRef, useState } from "react";

export function SaveButton({
  onSave,
  label,
  savedLabel,
  className = "jf-btn-primary",
  savedClassName = "jf-btn-primary jf-btn-saved",
  savedMs = 2000,
  disabled = false,
  ...rest
}: {
  /**
   * The save action. Return `false` to say it FAILED — the button then stays
   * idle instead of confirming, and the caller's own error message is the only
   * claim on screen. Return nothing (or `true`) to confirm.
   */
  onSave: () => void | boolean | Promise<void | boolean>;
  /** Idle label. */
  label: string;
  /** The confirmation label — usually a SHORTER form of `label`. */
  savedLabel: string;
  className?: string;
  savedClassName?: string;
  /** How long the confirmation holds. Measured reference: 2000ms. */
  savedMs?: number;
  disabled?: boolean;
} & Omit<React.ButtonHTMLAttributes<HTMLButtonElement>, "onClick" | "disabled" | "className" | "children">) {
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState(false);
  // Held in a ref so the cleanup below can clear whatever timer is live. A stale
  // timer flipping a NEWER label back to idle is the bug this prevents.
  const timer = useRef<number | null>(null);

  useEffect(() => {
    // Unmount safety: a save that completes after the user navigates away must
    // not set state on a gone component.
    return () => {
      if (timer.current !== null) window.clearTimeout(timer.current);
    };
  }, []);

  const click = useCallback(async () => {
    if (busy) return;
    // Clear before arming: a second save inside the window must not inherit the
    // first save's pending revert.
    if (timer.current !== null) {
      window.clearTimeout(timer.current);
      timer.current = null;
    }
    setSaved(false);
    setBusy(true);
    let confirmed = true;
    try {
      // `false` is the caller's explicit "this did not save". Any other return
      // (including undefined) is a confirmation.
      if ((await onSave()) === false) confirmed = false;
    } catch {
      // A FAILURE MUST NOT CONFIRM. The caller renders its own error (the page's
      // `err` banner / a failure-tone FeedbackNote); showing "Saved ✓" here would
      // claim the write succeeded.
      confirmed = false;
    } finally {
      setBusy(false);
    }
    if (!confirmed) return;
    setSaved(true);
    timer.current = window.setTimeout(() => {
      setSaved(false);
      timer.current = null;
    }, savedMs);
  }, [busy, onSave, savedMs]);

  return (
    <button
      {...rest}
      type={rest.type ?? "button"}
      onClick={click}
      disabled={disabled || busy}
      aria-busy={busy}
      className={saved ? savedClassName : className}
    >
      {saved ? savedLabel : label}
    </button>
  );
}
