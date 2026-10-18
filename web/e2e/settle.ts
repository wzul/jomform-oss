// A BOUNDED wait for a page to settle — never a bet on a fixed sleep (t_14837ea0).
//
// WHY THIS EXISTS. `a11y-singlechar-text.spec.ts` and `a11y-authed-pages.spec.ts`
// both used `await page.waitForTimeout(1500)` as the step that was supposed to
// prove the app had rendered before they measured it. 1500 ms is a BET that the
// stubbed `dashboard-bundle` fetch plus hydration finishes in time. On a loaded
// agent host it does not always: the Shell renders AFTER the bundle resolves, the
// bet loses, and the gate reds on a page that is correct — measured on tree
// `559f1832` (gate run 1, 138 passed / 1 failed: `/ — every single-character
// element … Error: / rendered no sidebar nav — the shell did not render.
// Expected: >= 10  Received: 0`; the same tree re-ran 139/139 green). A failed
// gate can never be reused, and the gate pool's waiter has been measured at
// 50-86 minutes, so one lost bet costs a lane that much.
//
// THE SHAPE. Poll the DOM until the signals the caller is ABOUT TO ASSERT are
// actually there, under a generous ceiling, and return what was observed. It
// never throws and never skips: a page that genuinely never renders reaches the
// ceiling, the caller's own reachability assertion still fails, and that failure
// can now say how long it waited and what it saw. That is the property that must
// survive this change — the arm must stay able to go RED.
//
// WHY THE REACT MARKER AND NOT A SLEEP. React leaves `__reactFiber$…` /
// `__reactProps$…` on every node it has taken over, which is the deterministic
// "this document is hydrated" signal — the same one `a11y-authed-pages.spec.ts`
// already polls per route. Without it a scan can measure server markup the client
// is about to replace, and an axe reading of the wrong document is worse than a
// flaky one: it can be GREEN.
import type { Page } from "@playwright/test";

/**
 * The ceiling for a settle wait. A CEILING, not a target: the poll returns on the
 * first frame that satisfies the conditions, so a healthy render costs ~50 ms and
 * only a genuinely stuck page pays the full 15 s. It is 10× the 1500 ms bet it
 * replaces, which is what makes a loaded host harmless without hiding a hang.
 */
export const SETTLE_TIMEOUT_MS = 15_000;

export type Settled = {
  /** React took over the page-owned node (`#main-content`, else `<main>`). */
  hydrated: boolean;
  navLinks: number;
  hasMain: boolean;
  bodyText: number;
  /** How long the poll ran before it returned — logged by callers on failure. */
  waitedMs: number;
  /** False when the conditions were met; true when the ceiling was reached. */
  timedOut: boolean;
};

export type SettleOptions = {
  /**
   * True for a route whose chrome is the dashboard Shell (`<nav>` with the
   * sidebar links). False for public pages and `/builder`, which paint their own
   * body — the same split the single-character scan asserts on.
   */
  wantsShell: boolean;
  /** Sidebar links required before the Shell counts as rendered. `nav a[href]`. */
  navFloor?: number;
  /**
   * Treat a `<main>` landmark as "the page's own chrome rendered" even when
   * `wantsShell` is set. For a caller that cannot know in advance WHICH shell a
   * redirect lands on: `/` renders `#main-content` with the sidebar, while
   * `/workspace-picker` renders its own `<main>` and no `<nav>` at all.
   */
  mainCountsAsChrome?: boolean;
  /** Visible text required, so a loading shell is not a settled page. */
  bodyFloor?: number;
  timeoutMs?: number;
};

/**
 * Poll until the page is really rendered, or the ceiling is reached. Never throws:
 * the returned record is what the caller asserts on, so the failure lands on the
 * caller's own message ("the shell did not render") rather than on a timeout.
 *
 * `bodyText` here is the TRIMMED innerText length — a coarse "there is a page
 * here" guard, deliberately without the whitespace collapsing the scan itself
 * does, so the injected probe stays free of another layer of string escaping. The
 * arm's own measurement keeps its exact expression.
 */
export async function waitForSettled(page: Page, opts: SettleOptions): Promise<Settled> {
  const navFloor = opts.navFloor ?? 10;
  const bodyFloor = opts.bodyFloor ?? 200;
  const timeoutMs = opts.timeoutMs ?? SETTLE_TIMEOUT_MS;
  const mainCounts = opts.mainCountsAsChrome === true;

  const probe = `(async () => {
    const wantsShell = ${JSON.stringify(opts.wantsShell)};
    const mainCounts = ${JSON.stringify(mainCounts)};
    const navFloor = ${navFloor};
    const bodyFloor = ${bodyFloor};
    const deadline = Date.now() + ${timeoutMs};
    const started = Date.now();
    // React marks the nodes it owns; read on a PAGE-OWNED node so the wait cannot
    // be satisfied by the Shell hydrating while the route's own body is still a
    // loading state.
    const taken = (el) =>
      !!el && Object.keys(el).some((k) => k.startsWith("__reactFiber$") || k.startsWith("__reactProps$"));
    let last = { hydrated: false, navLinks: 0, hasMain: false, bodyText: 0 };
    while (Date.now() < deadline) {
      const main = document.querySelector("#main-content") || document.querySelector("main");
      const read = {
        hydrated: taken(main),
        navLinks: document.querySelectorAll("nav a[href]").length,
        hasMain: !!document.querySelector("main"),
        bodyText: (document.body.innerText || "").trim().length,
      };
      last = read;
      const rendered = wantsShell ? read.navLinks >= navFloor || (mainCounts && read.hasMain) : read.hasMain;
      if (read.hydrated && rendered && read.bodyText >= bodyFloor) {
        return { ...read, waitedMs: Date.now() - started, timedOut: false };
      }
      await new Promise((r) => setTimeout(r, 50));
    }
    return { ...last, waitedMs: Date.now() - started, timedOut: true };
  })()`;

  return (await page.evaluate(probe)) as Settled;
}

/** One line for the run log: what the wait saw, so a red names the state it was in. */
export function settledLine(what: string, s: Settled): string {
  return (
    `[settle] ${what} waited=${s.waitedMs}ms timedOut=${s.timedOut} hydrated=${s.hydrated} ` +
    `navLinks=${s.navLinks} hasMain=${s.hasMain} bodyText=${s.bodyText}`
  );
}
