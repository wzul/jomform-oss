// The ARIA tabs pattern's shared half — the five app tab strips.
//
// THE GAP (card t_40253ae6, filed from the post-deploy verify of v1.21.32 /
// PR #1105). Every tab strip in the app was a PARTIAL ARIA tab pattern: each one
// carried `role="tablist"` + `role="tab"` + `aria-selected` and NOTHING in
// `src/` carried `aria-controls` or `role="tabpanel"` (grep: 0 hits across the
// five strips at src/app/profile/page.tsx, src/app/settings/page.tsx,
// src/app/products/page.tsx, src/app/media/page.tsx, src/app/forms/page.tsx).
// So no tab was associated with the panel it controls — ARIA 1.2 has a `tab`
// expose `aria-controls` pointing at its `tabpanel`, and the panel carry
// `aria-labelledby` back at the tab — and a screen-reader user got no announced
// relationship between the selected tab and the panel below it. Axe does NOT
// flag the omission (measured on the live signed-in page: 0 violations of any
// impact), which is why it survived a release and is a follow-up rather than a
// blocker.
//
// WHY ONE MODULE AND NOT FIVE COPIES. The pair of ids has to MATCH — the tab's
// `aria-controls` and the panel's `id` are one fact written twice, and so are the
// panel's `aria-labelledby` and the tab's `id`. Five hand-written string pairs
// is five chances to drift, and a drifted pair is silent: the idref resolves to
// nothing, axe's `aria-valid-attr-value` exempts a non-selected tab (it skips
// `aria-controls` while `aria-selected` is "false"), and the only symptom is a
// screen reader with no relationship. `tabDomId`/`panelDomId` below are the one
// place that pair is composed, so the strips cannot disagree about the shape of
// a panel id.
//
// WHAT ELSE APG ASKS FOR, AND WHAT IS DELIBERATELY NOT HERE.
//   * `aria-controls` + `role="tabpanel"` + `aria-labelledby` — the association.
//     Every panel id exists in the document WHILE ITS TAB IS SELECTED, because
//     each page keeps its existing conditional render (`{tab === "x" && …}`): the
//     non-selected tabs point at a panel that is not in the DOM, which is the
//     APG's own "Tabs With Automatic Activation" behaviour and is exactly the
//     case axe exempts. The arm in `e2e/a11y-profile-tabs.spec.ts` asserts the
//     part that matters: the SELECTED tab's `aria-controls` resolves to the
//     painted panel.
//   * Roving tabindex: ONE tab stop for the whole strip (the selected tab), the
//     rest out of the page tab sequence — `tabStop` below. Without it, Tab walks
//     every tab one at a time, which is the behaviour the arrow keys exist to
//     replace.
//   * Arrow keys: Left/Right/Home/End move the selection per the APG tabs
//     pattern (automatic activation), with wraparound. APG's note 5 in the
//     Tabs pattern is why Down/Up are NOT handled: on a horizontal tablist they
//     must keep their normal browser scrolling function.
//     RTL (ar, one of this app's five locales) is deliberately reversed. The
//     APG defines Left/Right as "previous/next tab" without saying which way
//     that is in a right-to-left page, and its keyboard-interface guidance says
//     focus movement should match the READING ORDER of the page's language. In
//     `dir="rtl"` the strip lays its first tab out RIGHTMOST, so "next tab" is
//     to the left: Right Arrow must move to the tab the reader sees on the
//     right, i.e. the PREVIOUS one. The direction is read off the RENDERED strip
//     (`getComputedStyle(...).direction`), not off a locale string, so a strip
//     inside an RTL subtree behaves correctly even where the document is ltr.
//   * NO `aria-label` on the tablist. APG asks for one (it has no visible
//     label), but naming five strips in five locales is a dictionary change
//     with its own review, and axe reports nothing for it — it is not part of
//     this card. Recorded so the next reader does not think it was overlooked.
//
// WHY THE SELECTION IS DRIVEN BY A CLICK AND NOT BY A `select` CALLBACK. Every
// strip already owns what activating a tab means (profile/settings persist the
// tab, products/forms/media also clear the refusal banner a view switch
// invalidates). Re-implementing that in a keyboard path would be a second copy
// of the page's own rule; `tablistKeyDown` moves focus and then clicks the tab,
// which is the same code path the mouse takes. It is a real `click()` on a real
// button, so React's onClick runs and the page's state machine decides.

/** `aria-controls` / `aria-labelledby` target: the panel a tab controls. */
export function panelDomId(prefix: string, id: string): string {
  return `${prefix}-panel-${id}`;
}

/** The tab's own id: what its panel's `aria-labelledby` points back at. */
export function tabDomId(prefix: string, id: string): string {
  return `${prefix}-tab-${id}`;
}

/**
 * The roving tabindex: the SELECTED tab is the strip's one tab stop, every other
 * tab is out of the page tab sequence (`-1`, still focusable by script and by
 * the arrow keys). A strip with no selection (`active === null`, the read_only
 * settings state that renders no strip at all) puts nothing in the tab order —
 * `-1` rather than 0, because there is no tab a Tab press should land on.
 */
export function tabStop(id: string, active: string | null): 0 | -1 {
  return active !== null && id === active ? 0 : -1;
}

/**
 * The tab an arrow/home/end key moves to, as an index into the strip's rendered
 * tabs, or `null` when the key is not one this widget owns (so the caller leaves
 * the event alone — a Tab press, a Shift+Tab, a character key).
 *
 * `current` is the index of the SELECTED tab, or -1 when nothing is selected.
 * Wraparound at both ends, per APG ("If focus is on the last tab element, moves
 * focus to the first tab"). Home/End are DOM order in both directions: APG calls
 * them "the first tab" / "the last tab", and the first tab is index 0 however
 * the strip is laid out.
 */
export function nextTabStop(
  count: number,
  current: number,
  key: string,
  rtl = false,
): number | null {
  if (count <= 0) return null;
  const step = rtl ? -1 : 1;
  switch (key) {
    case "ArrowRight":
    case "ArrowLeft": {
      const dir = key === "ArrowRight" ? step : -step;
      if (current < 0) return dir > 0 ? 0 : count - 1;
      return (current + dir + count) % count;
    }
    case "Home":
      return 0;
    case "End":
      return count - 1;
    default:
      return null;
  }
}

/**
 * The shape of the keydown React hands the strip, structurally rather than as a
 * `React.KeyboardEvent`: this module is a `.ts` file (not a `.tsx`) and is
 * imported by five client components and by the unit test, which exercises
 * `nextTabStop` directly without a DOM.
 */
export type TablistKeyEvent = {
  key: string;
  preventDefault: () => void;
  currentTarget: EventTarget | null;
};

/**
 * The strip's own `onKeyDown`: moves the selection and the focus across the tabs
 * with Left/Right/Home/End. Returns whether the key was handled, so the caller
 * can see (and a test can assert) that an unhandled key was left alone.
 *
 * It reads the strip's RENDERED tabs rather than a list passed in, so a page
 * whose strip is filtered by role (settings hides the admin-only tabs from a
 * member) gets the navigation over exactly the tabs it renders. The selection is
 * applied by clicking the tab — see the header for why.
 */
export function tablistKeyDown(event: TablistKeyEvent): boolean {
  const strip = event.currentTarget as (EventTarget & HTMLElement) | null;
  if (!strip || typeof strip.querySelectorAll !== "function") return false;

  const tabs = [...strip.querySelectorAll<HTMLElement>('[role="tab"]')];
  if (tabs.length === 0) return false;

  const current = tabs.findIndex((t) => t.getAttribute("aria-selected") === "true");
  const rtl =
    typeof getComputedStyle === "function" && getComputedStyle(strip).direction === "rtl";
  const next = nextTabStop(tabs.length, current, event.key, rtl);
  if (next === null) return false;

  event.preventDefault();
  // Focus first (the settings strip reveals the focused tab into its scroller),
  // then activate through the page's own click handler.
  tabs[next].focus();
  tabs[next].click();
  return true;
}
