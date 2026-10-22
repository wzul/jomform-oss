// The sidebar nav links' ACCESSIBLE NAME must not carry the decorative glyph
// (card t_950256ab).
//
// THE DEFECT. `web/src/components/Shell.tsx` renders every nav row as
//
//     <Link href={n.href}><span className="w-4 text-center text-xs">{n.icon}</span>{n.label}</Link>
//
// The glyph is decoration — the label right beside it is the name — but the span
// carried no `aria-hidden`, so the accessible name was computed from BOTH nodes.
// MEASURED on this tree before the fix, a production build served by `next start`,
// a FRESH browser state (storage cleared, no `jf_lang`, `html lang="en"`), the API
// stubbed at the browser level, `/` read out of Chromium's own accessibility tree:
//
//     CDP AX node, aside nav a[href="/forms"]   name "✎ Forms"
//     CDP AX node, aside nav a[href="/spam"]    name "⚠ Spam"
//     CDP AX node, aside nav a[href="/templates"] name "🗂 Templates"
//
// …on all 14 rows (`glyphHidden: false` on every one), and the same in the mobile
// drawer, which renders the SAME `NavList`. A screen reader therefore announced
// "pen Forms", "warning Spam" — the icon, again, for every destination.
//
// WHY THIS IS THE REPO'S OWN RULE. `web/e2e/a11y-products.spec.ts` already asserts
// the identical shape for the sortable-table direction glyph: "the ▲/▼ glyph must
// be aria-hidden so the button keeps its plain label". The nav rows are the same
// thing at a different scale, and the fix is the same one: `aria-hidden="true"` on
// the decorative span. The glyph is still PAINTED (it is part of the sidebar's
// design) — only the accessible name stops carrying it.
//
// WHY THIS FILE MEASURES THE AX TREE AND NOT THE ATTRIBUTE. A guard that reads
// `span[aria-hidden]` can pass while AT still hears the glyph (a wrapper that
// re-exposes the text, an `aria-labelledby` on the anchor, a browser that resolves
// the name differently). So every assertion here is on the NAME, read two ways:
//
//   1. `locator.ariaSnapshot()` — the card's own measurement, and
//   2. Chromium's real AX tree over CDP (`Accessibility.getPartialAXTree` on each
//      anchor's DOM node), which is the string Blink hands the platform AT.
//
// They must agree, and both must equal the row's own label EXACTLY. Nothing here
// asserts an attribute — the attribute is only used by the control arm, to put the
// pre-fix DOM back.
//
// WHAT ELSE IT PINS. The fix must not achieve a clean name by DELETING the glyph or
// by emptying the link: each row's own text is still `<glyph><label>`, and the name
// is still the label (never ""). Both surfaces are covered — the desktop <aside>
// and the mobile drawer — and they are the one `NavList`, so one change covers
// both, but a regression could hide either surface behind its own class.
//
// THE CONTROL ARM (at the bottom). A green reading above proves nothing on its own:
// a measurement of the AX tree that cannot see the glyph in the name would pass
// over the pre-fix DOM. So the last test removes `aria-hidden` from the REAL spans,
// requires BOTH measurements to read glyph-carrying names (i.e. to go RED), then
// restores it and requires them to read the labels again — proving the reading
// tracks the fix and not the page load.
//
// Run with:
//   npm run a11y                                                  # the whole gate
//   npx playwright test e2e/a11y-nav-icon-name.spec.ts             # this file alone

import { test, expect, type Page } from "@playwright/test";

import { DICT } from "../src/components/i18nDict";
import { signIn } from "./auth";
import { SETTLE_TIMEOUT_MS, settledLine, waitForSettled } from "./settle";

/** The nav rows Shell.tsx renders, in DOM order, by the dictionary key each label
 *  comes from. `nav.admin` is the super-admin row (the stub below IS a super-admin,
 *  so it renders); the order is the `navGroups` order plus that row, which is
 *  appended to the last group. A row added or moved must re-measure this list —
 *  the test fails with the names it actually read. */
const NAV_LABEL_KEYS = [
  "nav.dashboard",
  "nav.forms",
  "nav.templates",
  "prod.title",
  "nav.sales",
  "nav.spam",
  "sub.title",
  "del.title",
  "act.title",
  "nav.settings",
  "media.title",
  "nav.chat",
  "nav.feedback",
  "nav.admin",
] as const;

/** The EN labels — the page is driven at the DEFAULT locale, so these are what a
 *  new visitor's AT must hear. */
const LABELS: string[] = NAV_LABEL_KEYS.map(
  (k) => DICT[k as keyof typeof DICT].en as string,
);

/** What the DOM paints in each row today (Shell.tsx `navGroups`). Read from the
 *  source at the time this file was added: the fix hides these from AT, it does NOT
 *  remove them from the design. */
const GLYPHS = ["◧", "✎", "🗂", "💰", "⚠", "⟳", "✉", "🕘", "⚙", "🖼", "✨", "💬", "🛡"];

const DESKTOP_SCOPE = "aside nav";
const DESKTOP_LINKS = "aside nav a[href]";
const DRAWER_SCOPE = '[role="dialog"] nav';
const DRAWER_LINKS = '[role="dialog"] nav a[href]';

/** Every `/api/*` read the dashboard makes, stubbed at the browser level — the same
 *  stub the sibling authed a11y specs use, so no Go server, session or database is
 *  involved and the page under test is only the client components. */
async function stubAPI(page: Page) {
  const json = (body: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  // Catch-all FIRST, specific routes AFTER it: Playwright resolves matching routes
  // in reverse registration order, so the later (specific) ones win.
  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) =>
    r.fulfill(
      json({
        me: {
          id: "u1",
          email: "merchant@example.com",
          name: "Merchant",
          lang: "en",
          is_super_admin: true,
        },
        workspaces: [
          { id: "a1", name: "Demo", slug: "demo", role: "admin" },
          { id: "a2", name: "Other", slug: "other", role: "admin" },
        ],
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
      }),
    ),
  );
  await page.route(/(.*)\/api\/auth\/me$/, (r) =>
    r.fulfill(
      json({ id: "u1", email: "merchant@example.com", name: "Merchant", is_super_admin: true }),
    ),
  );
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "en" })));
}

/** A FRESH browser state at the app's DEFAULT locale: storage cleared and NO
 *  `jf_lang` written, so the page renders what a new visitor gets (a `jf_lang` left
 *  behind by an earlier run has already produced a false reading on this board). */
async function freshState(page: Page) {
  await page.addInitScript(() => {
    try {
      localStorage.clear();
      sessionStorage.clear();
      localStorage.setItem("jf_workspace", "demo");
    } catch {
      /* storage unavailable */
    }
  });
}

/** The `link` names out of a Playwright ARIA snapshot. YAML lines look like
 *  `- link "✎ Forms":` (a name may be empty: `- link:`), and the parser is
 *  deliberately line-shaped so a nested anchor cannot be silently skipped. */
function snapshotLinkNames(snapshot: string): string[] {
  const names: string[] = [];
  for (const line of snapshot.split("\n")) {
    const m = /^\s*- link(?: "((?:[^"\\]|\\.)*)")?:/.exec(line);
    if (m) names.push((m[1] ?? "").replace(/\\"/g, '"'));
  }
  return names;
}

type AxLink = { role: string | null; name: string; ignored: boolean };

/**
 * Chromium's OWN accessible name for each element `selector` matches, one CDP
 * `Accessibility.getPartialAXTree` per DOM node. This is the value the platform AT
 * receives — the measurement `ariaSnapshot()` is cross-checked against, so neither
 * can be the only reading.
 */
async function cdpAxLinks(page: Page, selector: string): Promise<AxLink[]> {
  const cdp = await page.context().newCDPSession(page);
  try {
    await cdp.send("Accessibility.enable");
    const { root } = await cdp.send("DOM.getDocument", { depth: 1 });
    const { nodeIds } = await cdp.send("DOM.querySelectorAll", {
      nodeId: root.nodeId,
      selector,
    });
    const out: AxLink[] = [];
    for (const nodeId of nodeIds) {
      const { nodes } = await cdp.send("Accessibility.getPartialAXTree", {
        nodeId,
        fetchRelatives: false,
      });
      const ax = (nodes[0] ?? {}) as {
        role?: { value?: string };
        name?: { value?: string };
        ignored?: boolean;
      };
      out.push({
        role: ax.role?.value ?? null,
        name: ax.name?.value ?? "",
        ignored: ax.ignored === true,
      });
    }
    return out;
  } finally {
    await cdp.detach();
  }
}

/** `<glyph><label>` for each row, read from the DOM's own text — the half of the
 *  contract that says the glyph is still PAINTED. */
async function rowTexts(page: Page, selector: string): Promise<string[]> {
  return page.evaluate(
    (sel) => Array.from(document.querySelectorAll(sel)).map((a) => (a.textContent || "").trim()),
    selector,
  );
}

/** Both readings of the same surface, in DOM order. */
async function readNames(page: Page, scope: string, links: string) {
  const snapped = await page.locator(scope).ariaSnapshot();
  const snapshot = snapshotLinkNames(snapped);
  const cdp = await cdpAxLinks(page, links);
  return { snapshot, cdp, snapped };
}

/** The nav is the thing under test: fewer rows than the list means the Shell did
 *  not render, and a scan of nothing would be GREEN. */
function expectShellRendered(settled: { waitedMs: number; hydrated: boolean; bodyText: number }, navLinks: number, where: string) {
  expect(
    navLinks,
    `${where}: the shell rendered ${navLinks} nav link(s), not the ${LABELS.length} the sidebar has — the ` +
      `page under test did not render [waited ${settled.waitedMs}ms, ceiling ${SETTLE_TIMEOUT_MS}ms, ` +
      `hydrated=${settled.hydrated}, bodyText=${settled.bodyText}]`,
  ).toBeGreaterThanOrEqual(LABELS.length);
}

/** Open `/` signed in, stubbed, at a fresh default-locale state. */
async function openDashboard(page: Page, baseURL: string | undefined) {
  await freshState(page);
  await page.context().clearCookies();
  await signIn(page, baseURL);
  await stubAPI(page);
  await page.goto("/", { waitUntil: "domcontentloaded" });
  const settled = await waitForSettled(page, { wantsShell: true, navFloor: LABELS.length });
  console.log(settledLine("/", settled));
  return settled;
}

test.describe("a11y: the sidebar nav links' ACCESSIBLE NAMES are the labels, not the glyphs", () => {
  test("desktop <aside> — every nav link's accessible name is its label", async ({ page, baseURL }) => {
    const settled = await openDashboard(page, baseURL);

    // (1) NO REDIRECT: a dashboard path without a session is bounced to /login by
    // web/src/middleware.ts, and login has no sidebar — a scan of it would be a
    // clean reading over nothing.
    expect(new URL(page.url()).pathname, "/ redirected — the page under test did not render").toBe("/");

    const rows = await rowTexts(page, DESKTOP_LINKS);
    expectShellRendered(settled, rows.length, "/");

    // (2) FRESH STATE, asserted: the init script clears storage on navigation, so a
    // non-default locale can only be one the APP persisted. `en` is the default a
    // new visitor gets, and the labels compared below are the EN ones.
    expect(
      [null, "", "en"],
      "this run must start at the DEFAULT locale (storage cleared, no leftover jf_lang)",
    ).toContain(await page.evaluate(() => window.localStorage.getItem("jf_lang")));
    await expect(page.locator("html")).toHaveAttribute("lang", "en");

    // (3) THE GLYPH IS STILL PAINTED. This is what makes the fix "hide from AT", not
    // "delete the icons": each row's own text is still `<glyph><label>`.
    const painted = rows
      .map((text, i) => ({ text, label: LABELS[i] }))
      .filter((r) => GLYPHS.some((g) => r.text.startsWith(g)) && r.text.endsWith(r.label));
    expect(
      painted.map((r) => r.text),
      `the sidebar rows no longer paint "<glyph><label>": ${JSON.stringify(rows)}`,
    ).toHaveLength(LABELS.length);
    for (const r of rows) console.log(`[nav-paint] ${JSON.stringify(r)}`);

    // (4) THE MEASUREMENT — both readings, logged before the assertion so a red run
    // carries the names it read.
    const { snapshot, cdp, snapped } = await readNames(page, DESKTOP_SCOPE, DESKTOP_LINKS);
    console.log(`[aria-snapshot aside nav]\n${snapped}`);
    for (const l of cdp) console.log(`[cdp-ax] role=${l.role} ignored=${l.ignored} name=${JSON.stringify(l.name)}`);

    expect(
      cdp.map((l) => l.role),
      "every nav row must still be exposed as a `link` — a row that stopped being one is not a fix",
    ).toEqual(LABELS.map(() => "link"));
    expect(
      cdp.filter((l) => l.ignored).map((l) => l.name),
      "a nav row is IGNORED in the accessibility tree — AT cannot reach that destination at all",
    ).toEqual([]);

    // The names Chromium hands AT must be the labels: no glyph, and not empty.
    expect(
      cdp.map((l) => l.name.trim()),
      "the desktop sidebar's accessible names are not exactly the row labels — an accessible name that still " +
        "carries the decorative glyph is announced by a screen reader as part of the link (\"pen Forms\")",
    ).toEqual(LABELS);

    // …and Playwright's own ARIA snapshot of the same subtree must agree, link for
    // link: two independent readings, so a bug in one cannot carry the other.
    expect(
      snapshot.map((n) => n.trim()),
      "the ARIA snapshot's link names disagree with Chromium's AX tree",
    ).toEqual(cdp.map((l) => l.name.trim()));

    // No name may carry a glyph character, whatever else changed.
    expect(
      cdp.filter((l) => GLYPHS.some((g) => l.name.includes(g))).map((l) => l.name),
      "a nav link's accessible name carries a decorative glyph",
    ).toEqual([]);
  });

  test("mobile drawer — every nav link's accessible name is its label", async ({ page, baseURL }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    const settled = await openDashboard(page, baseURL);
    expect(new URL(page.url()).pathname, "/ redirected at 390px — the page under test did not render").toBe("/");

    // The drawer is a real dialog opened by the real control, not a class the spec
    // flips: a fix that covered only the desktop <aside> would be caught here.
    const opener = page.getByRole("button", { name: "Open menu" });
    await expect(opener, "the mobile topbar did not render its menu button").toBeVisible();
    await opener.click();
    const drawerNav = page.locator(DRAWER_SCOPE);
    await expect(drawerNav, "the mobile drawer did not open").toBeVisible();

    const rows = await rowTexts(page, DRAWER_LINKS);
    expectShellRendered(settled, rows.length, "/ @390px");

    const painted = rows
      .map((text, i) => ({ text, label: LABELS[i] }))
      .filter((r) => GLYPHS.some((g) => r.text.startsWith(g)) && r.text.endsWith(r.label));
    expect(
      painted.map((r) => r.text),
      `the drawer rows no longer paint "<glyph><label>": ${JSON.stringify(rows)}`,
    ).toHaveLength(LABELS.length);
    for (const r of rows) console.log(`[drawer-paint] ${JSON.stringify(r)}`);

    // FRESH STATE at the drawer too: the labels this compares against are the EN
    // ones, so a persisted locale would compare the wrong strings.
    await expect(page.locator("html")).toHaveAttribute("lang", "en");

    const { snapshot, cdp, snapped } = await readNames(page, DRAWER_SCOPE, DRAWER_LINKS);
    console.log(`[aria-snapshot drawer nav]\n${snapped}`);
    for (const l of cdp) console.log(`[cdp-ax drawer] role=${l.role} ignored=${l.ignored} name=${JSON.stringify(l.name)}`);

    expect(
      cdp.map((l) => l.role),
      "every drawer nav row must still be exposed as a `link`",
    ).toEqual(LABELS.map(() => "link"));
    expect(
      cdp.map((l) => l.name.trim()),
      "the mobile drawer's accessible names are not exactly the row labels — this is the SAME NavList as the " +
        "desktop sidebar, so a fix that covered only one surface is not a fix",
    ).toEqual(LABELS);
    expect(
      snapshot.map((n) => n.trim()),
      "the ARIA snapshot's drawer link names disagree with Chromium's AX tree",
    ).toEqual(cdp.map((l) => l.name.trim()));
    expect(
      cdp.filter((l) => GLYPHS.some((g) => l.name.includes(g))).map((l) => l.name),
      "a drawer nav link's accessible name carries a decorative glyph",
    ).toEqual([]);
  });
});

// ── the control: the arm bites ────────────────────────────────────────────────
// Without this, a green reading above proves only that this file ran. The
// measurement must be able to SEE the defect, on the shipped element, from the
// same two readings — so the test puts the pre-fix DOM back (removes the
// `aria-hidden` the fix added, nothing else) and requires BOTH to go RED.
test.describe("a11y: the nav-name measurement bites", () => {
  test("un-hiding the decorative span puts the glyph back into the name, and hiding it again clears it", async ({
    page,
    baseURL,
  }) => {
    const settled = await openDashboard(page, baseURL);
    expectShellRendered(settled, (await rowTexts(page, DESKTOP_LINKS)).length, "/ (control)");

    // ARM 0 — the SHIPPED state. The names are the labels.
    const shipped = await readNames(page, DESKTOP_SCOPE, DESKTOP_LINKS);
    expect(
      shipped.cdp.map((l) => l.name.trim()),
      "the shipped sidebar does not read the plain labels — the control arm cannot reproduce the defect it is " +
        "written for",
    ).toEqual(LABELS);

    // ARM 1 — THE PRE-FIX DOM. Each row's decorative span is found by its own
    // single-character text (never by the attribute under test) and the attribute
    // the fix added is removed. Nothing else is touched.
    const spansBefore = await page.evaluate(
      (sel) =>
        Array.from(document.querySelectorAll(`${sel} > span`))
          .filter((s) => [...((s.textContent || "").trim())].length === 1)
          .map((s) => s.getAttribute("aria-hidden")),
      DESKTOP_LINKS,
    );
    console.log(`[control] glyph spans=${spansBefore.length} aria-hidden=${JSON.stringify(spansBefore)}`);
    expect(
      spansBefore.length,
      "no single-character span inside the nav rows — the control cannot put the pre-fix DOM back",
    ).toBeGreaterThanOrEqual(LABELS.length);

    const stripped = await page.evaluate((sel) => {
      let n = 0;
      for (const s of Array.from(document.querySelectorAll(`${sel} > span`))) {
        if ([...((s.textContent || "").trim())].length !== 1) continue;
        if (s.getAttribute("aria-hidden") !== null) {
          s.removeAttribute("aria-hidden");
          n += 1;
        }
      }
      return n;
    }, DESKTOP_LINKS);
    console.log(`[control] removed aria-hidden from ${stripped} glyph span(s)`);
    expect(stripped, "the fix is not in place: no glyph span carried `aria-hidden` to remove").toBeGreaterThanOrEqual(
      LABELS.length,
    );

    const preFix = await readNames(page, DESKTOP_SCOPE, DESKTOP_LINKS);
    for (const l of preFix.cdp) console.log(`[control pre-fix cdp-ax] name=${JSON.stringify(l.name)}`);
    console.log(`[control pre-fix snapshot]\n${preFix.snapped}`);

    // THE ARM BITES: with the span exposed, Chromium's AX name carries the glyph.
    expect(
      preFix.cdp.map((l) => l.name.trim()),
      "with the decorative span exposed, the accessible names did NOT change — this measurement cannot see the " +
        "defect this file exists for, so a green run above means nothing",
    ).not.toEqual(LABELS);
    expect(
      preFix.cdp.filter((l) => GLYPHS.some((g) => l.name.includes(g))).length,
      `pre-fix, NOT ONE accessible name carried a glyph: ${JSON.stringify(preFix.cdp.map((l) => l.name))}`,
    ).toBe(LABELS.length);
    // …and the second reading sees it too, so neither reading is what carries the
    // arm on its own.
    expect(
      preFix.snapshot.map((n) => n.trim()),
      "the ARIA snapshot did not see the glyph in the pre-fix DOM",
    ).toEqual(preFix.cdp.map((l) => l.name.trim()));

    // ARM 2 — restore, and the same two readings come back clean. This is what
    // proves the reading tracks the DOM state and not the page load.
    await page.evaluate((sel) => {
      for (const s of Array.from(document.querySelectorAll(`${sel} > span`))) {
        if ([...((s.textContent || "").trim())].length !== 1) continue;
        s.setAttribute("aria-hidden", "true");
      }
    }, DESKTOP_LINKS);
    const restored = await readNames(page, DESKTOP_SCOPE, DESKTOP_LINKS);
    expect(
      restored.cdp.map((l) => l.name.trim()),
      "restoring `aria-hidden` on the decorative span did not clear the glyph from the accessible name again",
    ).toEqual(LABELS);
  });
});
