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

/**
 * The ceiling for the ANIMATION settle below. A CEILING, not a target: the app's
 * longest entrance is 280ms (`--jf-dur-slow`, the skeleton's single pulse) and the
 * route entrance is 180ms (`--jf-dur-base`), so a healthy page returns on the first
 * poll and only a genuinely stuck or very long animation pays this.
 */
export const ANIMATION_SETTLE_TIMEOUT_MS = 3_000;

export type AnimationsSettled = {
  /** Finite animations still running when the wait started. */
  running: number;
  /** Of those, how many the ceiling had to force to their end state. */
  forced: number;
  /** Animations with no finite end (an infinite pulse); never waited on. */
  infinite: number;
  /** How long the wait ran before it returned. */
  waitedMs: number;
};

/**
 * The body of the animation settle, as ONE source of truth: the scan string in
 * `a11y-singlechar-text.spec.ts` runs this INSIDE its own evaluate, and
 * `settleAnimations()` below runs the same text through its own. Two copies would
 * drift, and a drift here is a measurement bug rather than a style one.
 *
 * It waits (bounded) for every animation WITH A FINITE END to finish, then forces
 * whatever is left to its end state, and never waits on or forces an infinite one.
 */
export function animationSettleScript(timeoutMs: number): string {
  return `(async () => {
    const deadline = Date.now() + ${timeoutMs};
    const started = Date.now();
    const finite = (a) => {
      try {
        const t = a.effect && a.effect.getComputedTiming();
        return !!t && Number.isFinite(t.endTime);
      } catch (e) {
        return false;
      }
    };
    const all = () => document.getAnimations();
    const running = () => all().filter((a) => finite(a) && a.playState !== "finished");
    const runningAtStart = running().length;
    while (Date.now() < deadline && running().length > 0) {
      await new Promise((r) => setTimeout(r, 16));
    }
    const left = running();
    for (const a of left) {
      try {
        a.finish();
      } catch (e) {
        /* an animation with no finite end cannot be finished — already filtered */
      }
    }
    return {
      running: runningAtStart,
      forced: left.length,
      infinite: all().filter((a) => !finite(a)).length,
      waitedMs: Date.now() - started,
    };
  })()`;
}

/**
 * Set the page's RUNNING animations to their settled state before a visual
 * measurement, and report what it did (card t_a4adf2f1).
 *
 * WHY THIS EXISTS. `.jf-enter` paints the dashboard's route container with
 * `animation: jf-rise-in 180ms … both`, i.e. an `opacity` fade from 0. A contrast
 * scan that multiplies the ancestor `opacity` chain into the ink — which is exactly
 * what `a11y-singlechar-text.spec.ts` must do, because a STATIC `opacity-70` was
 * the defect that file exists for — then measures a FRAME of that fade if it
 * happens to sample while the animation is still finishing. Measured on the built
 * app, frame by frame: the `/sales` help glyph is 4.544:1 settled and every frame of
 * the entrance but the last ~30ms is under the 4.5 floor (t=90ms reads 4.415:1 at
 * `op=0.982518` — the shape of the reported `op=0.989814 / 4.42`). A gate whose
 * verdict depends on where in that fade the harness lands is non-deterministic by
 * construction, which is exactly what the card reports.
 *
 * THE RULE. WCAG 1.4.3 is about the text as presented at rest; a transient entrance
 * frame is not a paint. Note the direction of the error: compositing ink toward its
 * own surface can only move a ratio TOWARD 1, so a mid-animation sample can only
 * UNDERSTATE contrast. Folding one in can therefore produce a false red but never a
 * false green — which is why the caller treats "still animating" as a diagnosis and
 * not as a contrast verdict.
 */
export async function settleAnimations(
  page: Page,
  opts: { timeoutMs?: number } = {},
): Promise<AnimationsSettled> {
  return (await page.evaluate(animationSettleScript(opts.timeoutMs ?? ANIMATION_SETTLE_TIMEOUT_MS))) as AnimationsSettled;
}

/** One line for the run log, so a red can say whether the settle had work to do. */
export function animationsLine(what: string, s: AnimationsSettled): string {
  return (
    `[animations] ${what} running=${s.running} forcedAtCeiling=${s.forced} ` +
    `infinite=${s.infinite} waited=${s.waitedMs}ms`
  );
}
