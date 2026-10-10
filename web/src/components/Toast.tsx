"use client";

// The save-confirmation toast.
//
// WHY IT EXISTS
//   Every save on /settings already sets a message (`setMsg(t("set.sfSaved"))`
//   and six siblings), but it rendered in a banner at the TOP of the page. The
//   Storefront tab's Save button is several screens down, so a merchant who
//   clicked it saw no confirmation at all — the feedback existed and was
//   invisible. This renders the SAME `msg` state as a toast near the button, so
//   it is not a second success path: whatever sets the message shows the toast.
//
// IT CARRIES THE TONE, BECAUSE `msg` CARRIES SUCCESSES **AND** FAILURES (rework
// t_ad2af554, QA t_29a112bf)
//   The first cut bound this toast to the `msg` string alone and every message
//   got the same treatment: an emerald fill, an emerald tick, and a 6s timer that
//   cleared it. But two paths on the page write `msg` for a FAILED PUT —
//   `/api/rest/settings/tracking` and `/api/rest/settings/einvoice` — so a
//   failure was painted as a success and then VANISHED, and because the page's
//   inline banner renders the same `msg`, the failure indication was gone from
//   the whole page. Measured on the built pre-fix tree (Chromium, 500 forced on
//   both PUTs): tracking `@1s` toast "Failed" on `bg-emerald-50`, `@8s` toast
//   absent and the banner empty; the identical probe on `origin/main` (no Toast)
//   left both banners up. The card's acceptance says it plainly: a failure must
//   not vanish on a timer, and must not be painted in success chrome.
//
//   So the tone is an INPUT. `ok` is true for a save confirmation and false for a
//   failure, and it decides three things: the fill/icon/ink, and whether the 6s
//   timer is armed AT ALL. A failure is cleared only by the reader — the close
//   button — never by a clock.
//
// NO `aria-label` ON THE STATUS REGION, ON PURPOSE. A label on a live region
// REPLACES the announced content, so a screen reader would say a generic word
// instead of the actual message ("Footer saved ✓"). The region's content IS the
// announcement; the only other accessible name needed is the close button's.
//
// ONE LIVE REGION, NOT TWO (follow-up t_c4bff404). The positioner wrapper used to
// carry `aria-live="polite"` as well as the inner `role="status"`. A status role
// ALREADY implies `aria-live="polite"` (and `aria-atomic="true"` — WAI-ARIA 1.2
// §5.2.9 "Implicit Value for Role"), so that pair was two nested live regions
// wrapped around one message and a screen reader could announce the same sentence
// twice — the redundancy the label argument above rejects, one level down. The
// live region is the status element; the wrapper is only a positioner and must
// stay inert. Measured on the built page in Chromium (fresh EN storage state, a
// 500 forced on the tracking PUT, `Accessibility.getFullAXTree`): the pre-fix
// tree exposed the toast as TWO live nodes — the wrapper (`live=polite`,
// `atomic=false`) and the status element (`live=polite`, `atomic=true`) — and the
// same pair came out of the DOM scan; after the change the toast's accessible
// chain holds exactly one, the status element, with `role=status` / `live=polite`
// / `atomic=true` unchanged through the implicit mapping. So nothing stopped
// being announced; one of the two announcements went away.
//
// The 6s timer is re-armed per message and cleared on unmount, so saving twice
// cannot leave a stale timer dismissing a newer toast.

import { useEffect } from "react";

export function Toast({
  message,
  ok,
  onDismiss,
  closeTitle,
}: {
  message: string;
  /** true = a save confirmation, false = a failure. Never inferred from the text. */
  ok: boolean;
  onDismiss: () => void;
  closeTitle: string;
}) {
  useEffect(() => {
    // A FAILURE IS NOT ON A TIMER. `setMsg("")` clears the page banner as well as
    // this toast, so arming the timer for a failure would erase the only
    // indication that the save failed. Only a success auto-dismisses.
    if (!message || !ok) return;
    const id = window.setTimeout(onDismiss, 6000);
    return () => window.clearTimeout(id);
  }, [message, ok, onDismiss]);

  if (!message) return null;

  return (
    <div
      className="pointer-events-none fixed inset-x-0 bottom-0 z-50 flex justify-center px-4 pb-4 sm:justify-end sm:pr-6"
    >
      <div
        role="status"
        data-testid="save-toast"
        data-tone={ok ? "success" : "failure"}
        className={`pointer-events-auto flex w-full max-w-sm items-start gap-3 rounded-xl border px-4 py-3 shadow-lg ${
          ok ? "border-emerald-200 bg-emerald-50" : "border-red-200 bg-red-50"
        }`}
      >
        <span
          aria-hidden="true"
          className={`mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center rounded-full ${
            ok ? "bg-emerald-600" : "bg-red-600"
          }`}
        >
          {ok ? (
            <svg viewBox="0 0 20 20" fill="none" className="h-3.5 w-3.5">
              <path
                d="M4.5 10.5l3.5 3.5 7.5-8"
                stroke="#fff"
                strokeWidth="2.2"
                strokeLinecap="round"
                strokeLinejoin="round"
              />
            </svg>
          ) : (
            /* A failure carries a bang, never the success tick: the tick is the
               mark the emerald chrome makes, and reusing it here would say the
               save succeeded on a screen where it did not. */
            <svg viewBox="0 0 20 20" fill="none" className="h-3.5 w-3.5">
              <path d="M10 4.5v6.5" stroke="#fff" strokeWidth="2.2" strokeLinecap="round" />
              <circle cx="10" cy="14.7" r="1.25" fill="#fff" />
            </svg>
          )}
        </span>
        {/* text-emerald-800 / text-red-800, not the -600 steps: on these -50
            tints the 600s measure 3.47:1 (emerald, card t_fc1b67f1) and 4.36:1
            (red) — both under the 4.5:1 AA floor. The 800s clear it with margin
            (7.225:1 and 7.638:1, measured from the installed theme). */}
        <p className={`flex-1 text-sm font-medium ${ok ? "text-emerald-800" : "text-red-800"}`}>
          {message}
        </p>
        <button
          type="button"
          onClick={onDismiss}
          title={closeTitle}
          aria-label={closeTitle}
          className={`-mr-1 -mt-0.5 shrink-0 rounded-md p-1 transition-colors ${
            ok ? "text-emerald-700 hover:bg-emerald-100" : "text-red-700 hover:bg-red-100"
          }`}
        >
          <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4" aria-hidden="true">
            <path
              d="M5 5l10 10M15 5L5 15"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
            />
          </svg>
        </button>
      </div>
    </div>
  );
}
