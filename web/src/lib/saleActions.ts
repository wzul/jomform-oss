// WHICH SALE ACTIONS THE DASHBOARD MAY OFFER — decided by the SERVER, not by the
// page (card t_52429d6f).
//
// The sales page used to derive its buttons from `status`:
//
//     disabled={s.status !== "paid"}
//     {canWrite && (s.status === "paid" || s.status === "confirmed") && …}
//
// That is a client-side copy of a rule the Go lifecycle table owns
// (api/internal/lifecycle). When the two disagreed, the page offered a button the
// API refused — and before the table existed, a Confirm on a cancelled sale
// silently rewrote it. The API now ships `available_events` on every sale row:
// the events the current state accepts, each with `enabled` and, when a guard
// refuses it, the English reason. This module is the ONE place the page turns
// that list into buttons.
//
// FAIL-CLOSED. A row with no `available_events` (an older server, a stubbed
// response, a truncated body) yields NO actions. The page must never fall back to
// guessing from `status`: a missing answer means "I do not know", and rendering a
// guessed button is the defect this module exists to remove. `saleActions` takes
// no status argument at all, so the guess is not expressible here.

/** One entry of a sale row's `available_events`, exactly as the API sends it. */
export type AvailableEvent = {
  event: string;
  /** The dashboard i18n key for the button. Empty/absent = not a person's action. */
  label_key?: string;
  enabled: boolean;
  /** Why a disabled action is refused (English, from the machine's guard). */
  reason?: string;
};

/** A server event the page can actually dispatch, with its label resolved. */
export type SaleAction = {
  event: string;
  labelKey: string;
  enabled: boolean;
  reason?: string;
};

// The events this page has a handler for, in the order the buttons are drawn.
// An event the server reports but this list does not know would render a button
// with nothing behind it, so it is dropped rather than shown.
const HANDLED_EVENTS = ["capture", "release", "confirm", "refund"] as const;

// The row's action cell has always shown the money-in-flight actions only;
// Confirm stays a drawer action. That is a LAYOUT choice, not a legality claim:
// whether each one is enabled still comes from the server's list.
export const SALE_ROW_EVENTS: readonly string[] = ["capture", "release", "refund"];

/**
 * saleActions turns the server's `available_events` into the actions the page may
 * render. Entries that are not a person's action (no label_key — gateway
 * callbacks such as `pay`/`fail`) and events the page cannot dispatch are
 * dropped. `only` narrows the result to one surface's vocabulary.
 */
export function saleActions(
  events: readonly AvailableEvent[] | null | undefined,
  only?: readonly string[],
): SaleAction[] {
  if (!Array.isArray(events)) return [];
  const out: SaleAction[] = [];
  for (const raw of events) {
    if (!raw || typeof raw.event !== "string") continue;
    const labelKey = typeof raw.label_key === "string" ? raw.label_key : "";
    // No label key = not something a person asks for (a repeated gateway
    // callback, a state-preserving no-op). The machine says so explicitly.
    if (labelKey === "") continue;
    if (!(HANDLED_EVENTS as readonly string[]).includes(raw.event)) continue;
    if (only && !only.includes(raw.event)) continue;
    const action: SaleAction = { event: raw.event, labelKey, enabled: raw.enabled === true };
    if (typeof raw.reason === "string" && raw.reason !== "") action.reason = raw.reason;
    out.push(action);
  }
  out.sort((a, b) => {
    const ia = (HANDLED_EVENTS as readonly string[]).indexOf(a.event);
    const ib = (HANDLED_EVENTS as readonly string[]).indexOf(b.event);
    return ia - ib;
  });
  return out;
}

/** enabledSaleActions keeps only the actions the server says will succeed. */
export function enabledSaleActions(actions: readonly SaleAction[]): SaleAction[] {
  return actions.filter((a) => a.enabled);
}
