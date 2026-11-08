// Regression guard for card t_48319be9: the session bootstrap read must NOT
// decide the visitor is ANONYMOUS when it simply did not get an answer.
//
// THE DEFECT (reproduced deterministically in a real browser before this file —
// see the reproduction note below). `web/src/components/session.tsx`'s
// SessionProvider flipped `anon` when the `dashboard-bundle` read settled as
// `null`, and `fetchBundle` returned `null` for EVERY failure — a non-OK status
// (including a 5xx) and a fetch that rejected. Worse, an explicit 5s WEDGE
// collapsed the still-in-flight state into that same `null`:
//
//   const timer = window.setTimeout(() => {
//     setBundle((prev) => (prev === undefined ? null : prev));
//   }, 5000);
//
// whose comment said it existed "so a hung request can never leave the app stuck
// on the loading screen". So a bundle read that is merely SLOW (a starved main
// thread on a contended host — the gate runs 6 playwright workers + CPU spinners
// on 6 cores) or a bundle read that FAILS (a 5xx, a dropped connection) was
// reported to every page as a confirmed sign-out, and fifteen pages run
// `if (anon) router.replace("/landing")`. The page under test was gone — not a
// missing banner on it.
//
// MEASURED, NOT INFERRED (this is the repro the fix was proven against):
// stubbing `dashboard-bundle` to answer **200** but only after 9s puts `/chat`
// at `http://127.0.0.1:<port>/landing` on the pre-fix bytes — the 5s wedge fires
// first, `anon` flips, the page navigates away, and when the real 200 arrives
// later it cannot navigate back. On the fixed bytes the same 9s read keeps the
// page on `/chat` and it renders. `session.tsx` was changed to reach `null`
// ONLY from an authoritative refusal (a 401, or a 403 with no tenancy code — the
// identical rule `wsfetch.isUnauthStatus` applies on every page) and to expose a
// distinct `sessionError` fault state, which the dashboard shows as a retry.
//
// FOLLOW-UP (card t_f955c2f3). #1041 removed the 5s wedge, and with it the only
// bound the read had: a `dashboard-bundle` that is ACCEPTED and then never
// answers neither resolves nor rejects, so `sessionError` (which is set only
// when the read SETTLES as an error) stayed false for ever and the retry was
// never offered — the wedge had bounded this state, to the wrong outcome, and
// the fix removed the bound without adding a right one. `session.tsx` now arms
// an `AbortController` for the read, so a non-answering request aborts into the
// same `catch {}` as a dropped connection and reports the FAULT. The arms below
// cover the remaining `BundleRead` error producers: the REJECTED fetch
// (`route.abort()`), a 200 that is not JSON, and — the card's own defect — a
// read that never settles at all.
//
// FOLLOW-UP (card t_d1c9a43a). #1041 put the fault + retry surface on ONE route:
// `/` (dashboard-client.tsx). Every OTHER dashboard route read `anon` from
// useSession() and nothing else, so a settled fault left `bundle === undefined`
// and the route sat on its loading branch — a sentence-less "…" with no control
// (measured on the released build: faultSentence=0 retryBtn=0 on /chat, /forms,
// /settings, /products, /sales, against faultSentence=1 retryBtn=1 on `/`). The
// surface now lives in the app chrome (Shell), which shows it INSTEAD of the
// page's content when the bootstrap faulted — so it covers every route that
// renders inside the shell, including ones added later. The coverage arm at the
// bottom of this file drives the same settled 5xx on /chat and /forms.
//
// WHY A BROWSER SPEC AND NOT A SOURCE GUARD. The defect is a RACE and a
// NAVIGATION, not a wrong string at a call site: the failing source
// (`bundle === null → anon → router.replace`) reads correctly in isolation, and
// it is the same source that must keep working for a genuine 401 (the CONTROL
// below). Only a real build shows the final URL.
//
// The API is stubbed at the browser level (the pattern every sibling spec here
// uses), so the gate needs nothing but the build it is already serving.
import { test, expect, type Locator, type Page } from "@playwright/test";

import { signIn } from "./auth";

// The dashboard paths are gated on the SERVER (web/src/middleware.ts): a request
// without the `jf_session` cookie is redirected to /login BEFORE the page
// renders. The /chat cases here are about a visitor whose bundle read
// misbehaves, and the CONTROL case is about a DEAD session — both are states a
// browser reaches WITH the cookie present, so the session is declared as those
// siblings above do. "/" is not gated, so the cases that only touch it are
// unaffected either way.
test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});

import { DICT } from "../src/components/i18nDict";

const EN = {
  // The dashboard's own heading — the page the visitor asked for.
  dashTitle: DICT["dash.title"].en,
  // The /forms page's own heading — the second route the coverage arm recovers.
  formsTitle: DICT["forms.title"].en,
  // The fault sentence + the retry action the fixed build offers.
  sessionUnavailable: DICT["common.sessionUnavailable"].en,
  retry: DICT["common.retry"].en,
  // The landing hero heading, which the anon fallback renders. Its presence is
  // what "the visitor was treated as anonymous" looks like on screen.
  landHero1: DICT["land.hero1"].en,
} as const;

/** The slug the browser holds — the read is scoped to it, so the URL is real. */
const WS = "ws-a";

const BUNDLE = {
  me: { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "", is_super_admin: false },
  workspaces: [{ id: "ws-id-a", name: "Real", slug: WS, role: "admin" }],
  chip_status: null,
  keys: [],
  domains: [],
  webhooks: [],
  social: [],
  einvoice: null,
  products: [],
  summary: { counts: {}, revenue_sen: 0 },
  sales: { items: [], page: 1, limit: 20, total: 0 },
  form_analytics: [],
  product_analytics: [],
};

const json = (status: number, body: unknown) => ({
  status,
  contentType: "application/json",
  body: JSON.stringify(body),
});

/**
 * A fresh visitor state (English, one selected workspace). Storage is cleared
 * first: this browser keeps localStorage between runs on this host, and a
 * `jf_lang` left behind by an earlier card would render the page in a locale
 * this file never asserts.
 */
async function freshState(page: Page, base: string, opts: { sessionCookie?: boolean } = {}) {
  await page.addInitScript(() => {
    try {
      localStorage.clear();
      sessionStorage.clear();
      localStorage.setItem("jf_workspace", "ws-a");
      localStorage.setItem("jf_lang", "en");
    } catch {
      /* storage unavailable */
    }
  });
  // `app/page.tsx` is a SERVER component that decides from the presence of the
  // session cookie: without it, `/` is served as the landing page before any
  // client code runs. `/` renders the dashboard (and its retry surface) only for
  // a visitor whose browser carries the cookie, so a test that drives `/` must
  // set one. The VALUE is irrelevant — the server page tests presence — and the
  // bundle read is stubbed below.
  if (opts.sessionCookie) {
    await page.context().addCookies([{ name: "jf_session", value: "e2e-test", url: base }]);
  }
  // Catch-all FIRST — Playwright resolves matching routes in reverse
  // registration order, so the specific routes below win.
  await page.route(/(.*)\/api\/.*$/, (r) => r.fulfill(json(200, {})));
  await page.route(/(.*)\/api\/rest\/chat(\?.*)?$/, (r) =>
    r.fulfill(json(200, { available: true, limits: { messages_per_hour: 60, max_message_chars: 2000 } })),
  );
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json(200, { lang: "" })));
}

/**
 * The wedge the fix removed fired at 5000ms. A read that answers at 8000ms is
 * therefore past ANY fixed wedge of that order: on the pre-fix build the page
 * has already navigated away by then, and on the fixed build it has not. The
 * delay is the reproduction, not a convenience — it is what makes the test
 * bite deterministically instead of only under host contention.
 */
const SLOW_MS = 8_000;

/**
 * The bound the read now carries (card t_f955c2f3): `BUNDLE_READ_TIMEOUT_MS`
 * in `web/src/components/session.tsx`. Restated here on purpose — the arm below
 * waits it out, so if the constant moves this number is what has to move with
 * it, and the failure is a visible timing miss rather than a silently skipped
 * check. It is asserted to be well above the LEGITIMATE slow arm above (8s):
 * the bound must catch a read that will never answer, not a slow one.
 */
const BOUND_MS = 20_000;

test("a SLOW dashboard-bundle read does not bounce the page to /landing", async ({ page, baseURL }) => {
  test.setTimeout(120_000);
  await freshState(page, baseURL!);
  // The bundle answers 200 — a VALID session, just late. This is the contended
  // host: the client is starved, the server is fine (the spec stubs it 200).
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, async (r) => {
    await new Promise((res) => setTimeout(res, SLOW_MS));
    await r.fulfill(json(200, BUNDLE));
  });

  await page.goto(`${baseURL}/chat`, { waitUntil: "domcontentloaded" });

  // Wait past the old wedge window so a pre-fix build has had its chance to
  // navigate. Then the real assertion is the FINAL state, once the (late) 200
  // lands: the page the visitor asked for, rendered.
  await page.waitForTimeout(SLOW_MS + 4_000);
  expect(
    page.url(),
    "a SLOW (but valid) session read redirected the page — this is the reported bounce to /landing (t_48319be9)",
  ).toContain("/chat");
  await expect(page.locator("#chat-input")).toBeVisible({ timeout: 15_000 });
  console.log(`OBSERVED slow-bundle finalUrl=${page.url()}`);
});

test("a FAILED dashboard-bundle read shows a retry instead of signing the visitor out", async ({ page, baseURL }) => {
  test.setTimeout(120_000);
  await freshState(page, baseURL!, { sessionCookie: true });
  // The bundle answers 500 — the server is broken, the visitor is NOT anonymous.
  // It keeps failing until the RETRY, so a successful background read cannot
  // paper over the fault (the bootstrap issues more than one read as the
  // workspace state settles, and the dashboard would otherwise render itself).
  let recovered = false;
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) =>
    recovered ? r.fulfill(json(200, BUNDLE)) : r.fulfill(json(500, { error: "boom" })),
  );

  await page.goto(`${baseURL}/`, { waitUntil: "domcontentloaded" });

  // A fault is NOT a sign-out: the dashboard says so and offers the retry.
  await expect(page.getByText(EN.sessionUnavailable, { exact: true })).toBeVisible({ timeout: 20_000 });
  // ...and it did NOT serve the landing page (the pre-fix shape).
  expect(
    await page.getByRole("heading", { level: 1, name: EN.landHero1, exact: false }).count(),
    "a FAILED (5xx) bundle read served the landing page — the visitor was treated as anonymous (t_48319be9)",
  ).toBe(0);

  // The retry re-attempts the bootstrap and the dashboard renders for real.
  recovered = true;
  await page.getByRole("button", { name: EN.retry, exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: EN.dashTitle, exact: true })).toBeVisible({
    timeout: 20_000,
  });
  expect(page.url(), "the retry must not have navigated away").not.toContain("/landing");
  console.log(`OBSERVED failed-bundle finalUrl=${page.url()}`);
});

test("a REJECTED dashboard-bundle read shows a retry instead of signing the visitor out", async ({ page, baseURL }) => {
  test.setTimeout(120_000);
  await freshState(page, baseURL!, { sessionCookie: true });
  // The fetch REJECTS (offline / DNS failure / transport abort) — the third
  // `BundleRead` error producer, and the one whose `catch {}` the fix's own
  // comment names. `route.abort()` makes the request fail at the transport, so
  // the read settles as `{kind:"error"}`, which is a FAULT: it must be retryable
  // and must never be read as an anonymous session.
  let recovered = false;
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) =>
    recovered ? r.fulfill(json(200, BUNDLE)) : r.abort("failed"),
  );

  await page.goto(`${baseURL}/`, { waitUntil: "domcontentloaded" });

  await expect(page.getByText(EN.sessionUnavailable, { exact: true })).toBeVisible({ timeout: 20_000 });
  expect(
    await page.getByRole("heading", { level: 1, name: EN.landHero1, exact: false }).count(),
    "a REJECTED (transport failure) bundle read served the landing page — the visitor was treated as anonymous (t_f955c2f3)",
  ).toBe(0);

  recovered = true;
  await page.getByRole("button", { name: EN.retry, exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: EN.dashTitle, exact: true })).toBeVisible({
    timeout: 20_000,
  });
  expect(page.url(), "the retry must not have navigated away").not.toContain("/landing");
  console.log(`OBSERVED rejected-bundle finalUrl=${page.url()}`);
});

test("a 200 dashboard-bundle read that is NOT JSON shows a retry instead of signing the visitor out", async ({ page, baseURL }) => {
  test.setTimeout(120_000);
  await freshState(page, baseURL!, { sessionCookie: true });
  // The read SETTLES 200 but the body is not JSON, so `await r.json()` throws —
  // the fourth `BundleRead` error producer (`catch {}` around the parse). A
  // broken body is a broken READ, not an anonymous session: it must retry.
  let recovered = false;
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) =>
    recovered
      ? r.fulfill(json(200, BUNDLE))
      : r.fulfill({ status: 200, contentType: "text/html", body: "<!doctype html><p>proxy</p>" }),
  );

  await page.goto(`${baseURL}/`, { waitUntil: "domcontentloaded" });

  await expect(page.getByText(EN.sessionUnavailable, { exact: true })).toBeVisible({ timeout: 20_000 });
  expect(
    await page.getByRole("heading", { level: 1, name: EN.landHero1, exact: false }).count(),
    "a NON-JSON 200 bundle read served the landing page — the visitor was treated as anonymous (t_f955c2f3)",
  ).toBe(0);

  recovered = true;
  await page.getByRole("button", { name: EN.retry, exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: EN.dashTitle, exact: true })).toBeVisible({
    timeout: 20_000,
  });
  console.log(`OBSERVED non-json-bundle finalUrl=${page.url()}`);
});

test("a HUNG dashboard-bundle read is BOUNDED — the retry appears instead of hanging for ever", async ({ page, baseURL }) => {
  test.setTimeout(120_000);
  await freshState(page, baseURL!, { sessionCookie: true });
  // The read that neither resolves nor rejects: the route handler never settles,
  // so the request hangs with no HTTP response. Before the bound (`t_f955c2f3`)
  // `fetchBundle` never settled, `bundle` stayed `undefined`, `sessionError`
  // stayed false, and the visitor sat on the loading shell with NO retry for
  // ever. The bound has to settle this as a FAULT.
  let recovered = false;
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, async (r) => {
    if (recovered) {
      await r.fulfill(json(200, BUNDLE));
      return;
    }
    // Never settles: no fulfill, no abort, no continue.
    await new Promise(() => {});
  });

  await page.goto(`${baseURL}/`, { waitUntil: "domcontentloaded" });

  // Before the bound, this text NEVER appears (measured: still absent after 14s
  // and after 60s on the #1041 bytes). The wait is the bound plus slack.
  await expect(page.getByText(EN.sessionUnavailable, { exact: true })).toBeVisible({
    timeout: BOUND_MS + 15_000,
  });
  expect(
    page.url(),
    "a HUNG bundle read left the page — it must stay put and offer the retry",
  ).not.toContain("/landing");

  recovered = true;
  await page.getByRole("button", { name: EN.retry, exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: EN.dashTitle, exact: true })).toBeVisible({
    timeout: 20_000,
  });
  console.log(`OBSERVED hung-bundle finalUrl=${page.url()}`);
});

test("CONTROL: an authoritative 401 STILL bounces to /landing", async ({ page, baseURL }) => {
  test.setTimeout(120_000);
  await freshState(page, baseURL!);
  // The other half of the hazard. The fix narrowed `anon` to an AUTHORITATIVE
  // refusal; a 401 is one, and it must keep its old, wanted behaviour — a fix
  // that swallowed every failure would leave an expired session on a dead
  // dashboard.
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) =>
    r.fulfill(json(401, { error: "not signed in" })),
  );

  await page.goto(`${baseURL}/chat`, { waitUntil: "domcontentloaded" });
  await page.waitForURL(/\/landing/, { timeout: 20_000 });
  expect(page.url()).toContain("/landing");
  console.log(`OBSERVED control=401 finalUrl=${page.url()}`);
});

// ── THE COVERAGE ARM (card t_d1c9a43a) ───────────────────────────────────────
//
// THE DEFECT THIS PINS. The fault + retry surface #1041 added lived on ONE
// route: `/` (dashboard-client.tsx:169). Every other dashboard route read `anon`
// from useSession() and nothing else, so on a settled fault `bundle` stayed
// `undefined`, the route sat on its own loading branch, and the visitor got a
// sentence-less "…" with no control. Measured on the FROZEN release build
// (v1.20.75 = tree 3308cc72) with `dashboard-bundle` → 500, goto(), wait 8s:
//
//   PROBE /chat     settled-5xx faultSentence=0 retryBtn=0 landH1=0
//   PROBE /forms    settled-5xx faultSentence=0 retryBtn=0 landH1=0
//   PROBE /settings settled-5xx faultSentence=0 retryBtn=0 landH1=0
//   PROBE /products settled-5xx faultSentence=0 retryBtn=0 landH1=0
//   PROBE /sales    settled-5xx faultSentence=0 retryBtn=0 landH1=0
//
// `/` under the identical probe read faultSentence=1 retryBtn=1 — the contrast,
// and why the committed guard's 5xx arm (which drives `/`) could not see this.
//
// `/chat` is not incidental: it is the exact route the slow-read claim above is
// asserted on, and the route the deploy lane's own acceptance drove. The fix
// moved the surface into the app chrome (Shell), so this arm drives the SAME
// settled fault on two routes the defect was measured on — one whose loading
// branch is `PageLoading` (/forms) and one whose whole content is the chat box
// (/chat) — and asserts the surface on both, plus that the retry recovers.

/**
 * Drive one route's settled-5xx fault: the sentence must appear, the retry must
 * be offered, the landing page must NOT be served, and pressing the retry must
 * actually recover the page the visitor asked for.
 *
 * The bundle keeps failing until `recovered` flips, so a successful background
 * read cannot paper over the fault (the bootstrap issues more than one read as
 * the workspace state settles).
 */
async function settledFaultOn(page: Page, baseURL: string, route: string, recoveredPage: Locator) {
  let recovered = false;
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) =>
    recovered ? r.fulfill(json(200, BUNDLE)) : r.fulfill(json(500, { error: "boom" })),
  );

  await page.goto(`${baseURL}${route}`, { waitUntil: "domcontentloaded" });

  await expect(
    page.getByText(EN.sessionUnavailable, { exact: true }),
    `a settled 5xx on ${route} showed NO fault sentence — this is the un-Shell'd "…" the visitor could not act ` +
      `on (card t_d1c9a43a): the route stayed on its loading branch and never read sessionError`,
  ).toBeVisible({ timeout: 20_000 });
  await expect(
    page.getByRole("button", { name: EN.retry, exact: true }),
    `a settled 5xx on ${route} offered no way to retry — the visitor's only recovery was a manual reload`,
  ).toBeVisible();
  // A fault is NOT a sign-out, on this route exactly as on `/`.
  expect(
    await page.getByRole("heading", { level: 1, name: EN.landHero1, exact: false }).count(),
    `a settled 5xx on ${route} served the landing page — the visitor was treated as anonymous (t_48319be9)`,
  ).toBe(0);
  expect(page.url(), `a settled 5xx on ${route} navigated away from the route`).toContain(route);

  // The retry re-attempts the bootstrap and the route renders for real.
  recovered = true;
  await page.getByRole("button", { name: EN.retry, exact: true }).click();
  await expect(recoveredPage, `the retry on ${route} did not recover the page`).toBeVisible({
    timeout: 20_000,
  });
  expect(page.url(), "the retry must not have navigated away").not.toContain("/landing");
  expect(
    await page.getByText(EN.sessionUnavailable, { exact: true }).count(),
    `the fault surface is still on screen after a successful retry on ${route}`,
  ).toBe(0);
  console.log(`OBSERVED settled-5xx route=${route} finalUrl=${page.url()}`);
}

test("a settled 5xx on /chat shows the fault + retry (not only on /)", async ({ page, baseURL }) => {
  test.setTimeout(120_000);
  await freshState(page, baseURL!, { sessionCookie: true });
  await settledFaultOn(page, baseURL!, "/chat", page.locator("#chat-input"));
});

test("a settled 5xx on /forms shows the fault + retry (not only on /)", async ({ page, baseURL }) => {
  test.setTimeout(120_000);
  await freshState(page, baseURL!, { sessionCookie: true });
  // The recovered page is identified by its own heading — NOT by a bare
  // `<main>`, because the fault surface renders a <main> too and matching one
  // would let this arm pass on its own loading state.
  await settledFaultOn(
    page,
    baseURL!,
    "/forms",
    page.getByRole("heading", { level: 1, name: EN.formsTitle, exact: true }),
  );
});

test("a HUNG bundle read on /chat is BOUNDED into the fault + retry", async ({ page, baseURL }) => {
  test.setTimeout(120_000);
  await freshState(page, baseURL!, { sessionCookie: true });
  // The OTHER path into the same state, and the one the deploy lane measured on
  // live prod for this card: a read that is ACCEPTED and then never answers. The
  // 20s bound (`t_f955c2f3`) aborts it into `sessionError` — so the bound does
  // NOT by itself make these routes recoverable, because (before this card) they
  // never read `sessionError`. Measured on the released build: `/chat` hung 45s
  // (> the bound) read fault=False retryBtn=0 with the "…" identity.
  let recovered = false;
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, async (r) => {
    if (recovered) {
      await r.fulfill(json(200, BUNDLE));
      return;
    }
    // Never settles: no fulfill, no abort, no continue.
    await new Promise(() => {});
  });

  await page.goto(`${baseURL}/chat`, { waitUntil: "domcontentloaded" });

  // Before this card, this text NEVER appeared on /chat (the bound fired into a
  // state the route did not read). The wait is the bound plus slack.
  await expect(page.getByText(EN.sessionUnavailable, { exact: true })).toBeVisible({
    timeout: BOUND_MS + 15_000,
  });
  expect(
    page.url(),
    "a HUNG bundle read left /chat — it must stay put and offer the retry",
  ).not.toContain("/landing");

  recovered = true;
  await page.getByRole("button", { name: EN.retry, exact: true }).click();
  await expect(page.locator("#chat-input")).toBeVisible({ timeout: 20_000 });
  console.log(`OBSERVED hung-5xx route=/chat finalUrl=${page.url()}`);
});
