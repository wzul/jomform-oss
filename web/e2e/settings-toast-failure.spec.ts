// Regression guard for t_ad2af554 (rework of #997): a FAILED save on /settings
// must still be on screen 8s later, and must not be painted as a success.
//
// THE DEFECT, REPRODUCED IN A REAL BROWSER BEFORE THIS FIX
//   #997 added `web/src/components/Toast.tsx` and bound it to the page's `msg`
//   state with a 6s auto-dismiss. Two paths on /settings write `msg` for a
//   FAILED PUT — `/api/rest/settings/tracking` and `/api/rest/settings/einvoice`
//   — so a failure (a) rendered with the toast's emerald fill and tick, and
//   (b) was deleted at 6s by `setMsg("")`, which also clears the page's inline
//   banner (it renders the same state). Measured on the built pre-fix tree with
//   a 500 forced on both PUTs:
//
//     tracking  @1s : toast{text:"Failed", bg=emerald-50}   banner:"Failed"
//     tracking  @8s : toast{present:false}                  banner:[]        <- GONE
//     einvoice  @1s : toast{text:"Failed to save"}          banner:"Failed to save"
//     einvoice  @8s : toast{present:false}                  banner:[]        <- GONE
//
//   The identical probe on origin/main (which has no Toast) left both banners
//   up. The card's acceptance: a failure must not vanish on a timer, and must
//   not be painted in success chrome.
//
// WHY A BROWSER SPEC AND NOT ONLY A SOURCE GUARD
//   The vanish is a TIMER plus a call site, and the chrome is a COMPUTED style —
//   neither is visible to a source scan. `src/lib/toastTone.test.ts` stops the
//   mechanisms coming back out of the code; this measures the rendered page, in
//   the `e2e` gate. The SUCCESS control below is not decoration: it proves the
//   6s timer still auto-dismisses a genuine save, so the failure's persistence
//   cannot be a "the timer is simply broken" reading.
//
// The API is stubbed at the browser level (a real HTTP 500 is fulfilled), so the
// gate needs nothing but the build it is already serving.
//
// ONE LIVE REGION, NOT TWO (t_c4bff404). The last test here is a different
// question about the same toast: the pre-fix component hung `aria-live="polite"`
// on the positioner wrapper AND `role="status"` on the toast, and a status role
// ALREADY implies `aria-live="polite"` (WAI-ARIA 1.2 §5.2.9 "Implicit Value for
// Role", https://www.w3.org/TR/wai-aria-1.2/#status). Two nested live regions can
// announce the same sentence twice. Attribute scans can read that shape, but only
// the browser can say which nodes it exposes as live regions in the accessible
// tree a screen reader consumes — so it is measured here, before and after.
import { test, expect, type Page } from "@playwright/test";
import { DICT } from "../src/components/i18nDict";

const BUNDLE = {
  me: { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "en", is_super_admin: true },
  workspaces: [{ id: "ws1", name: "Demo", slug: "demo", role: "admin" }],
  chip_status: null, keys: [], domains: [], webhooks: [], social: [], einvoice: null,
  products: [], summary: { counts: {}, revenue_sen: 0 },
  sales: { items: [], page: 1, limit: 20, total: 0 },
  form_analytics: [], product_analytics: [],
};

/** The two failure tones the toast can paint, as the browser resolves them. */
const EMERALD_50 = "#ecfdf5";
const RED_50 = "#fef2f2";

/**
 * stubAPI fulfils the whole dashboard so /settings reaches its loaded state, and
 * answers the two PUTs the card measured with the status this test asks for.
 */
async function stubAPI(page: Page, putStatus: number) {
  const json = (status: number, body: unknown) => ({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  // Catch-all FIRST: Playwright resolves routes in REVERSE registration order,
  // so every specific route below it wins.
  await page.route(/(.*)\/api\/.*$/, (r) => r.fulfill(json(200, {})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(200, BUNDLE)));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(200, BUNDLE.me)));
  // lang:"" keeps the browser value in effect, which is how this spec pins en.
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json(200, { lang: "" })));
  await page.route(/(.*)\/api\/rest\/currency\/rates$/, (r) => r.fulfill(json(200, { rates: [], cached: false })));
  // The two settings writes the card measured, forced to the requested status.
  for (const path of ["tracking", "einvoice"]) {
    await page.route(new RegExp(`(.*)\\/api\\/rest\\/settings\\/${path}(\\?.*)?$`), (r) => {
      if (r.request().method() === "PUT") return r.fulfill(json(putStatus, { error: "boom" }));
      return r.fulfill(json(200, {}));
    });
  }
}

/**
 * The toast's rendered shape: presence, text, its tone attribute, and the fill
 * resolved to the sRGB bytes the compositor actually paints.
 *
 * `getComputedStyle().backgroundColor` returns whatever colour space Tailwind
 * emitted — `lab(97.8462 -6.94966 1.85487)` for the emerald ramp here, not an
 * rgb() — so the value is round-tripped through a canvas, exactly as
 * `web/e2e/builder-canvas-price-contrast.spec.ts` does (its header records the
 * same trap). Comparing the raw computed string against an rgb() literal would
 * make this guard fail on correct chrome.
 */
async function toastState(page: Page) {
  return page.evaluate(`(() => {
    const t = document.querySelector('[data-testid="save-toast"]');
    if (!t) return { present: false, text: "", bg: "", tone: "" };
    const cv = document.createElement("canvas");
    cv.width = cv.height = 1;
    const ctx = cv.getContext("2d", { willReadFrequently: true });
    ctx.clearRect(0, 0, 1, 1);
    ctx.fillStyle = getComputedStyle(t).backgroundColor;
    ctx.fillRect(0, 0, 1, 1);
    const d = ctx.getImageData(0, 0, 1, 1).data;
    const hex = "#" + [d[0], d[1], d[2]].map((v) => v.toString(16).padStart(2, "0")).join("");
    return {
      present: true,
      text: (t.textContent || "").trim(),
      bg: hex,
      tone: t.getAttribute("data-tone") || "",
    };
  })()`) as Promise<{ present: boolean; text: string; bg: string; tone: string }>;
}

/** The page-level banner pair (the `msg`/`err` banners above the tab content). */
async function banners(page: Page) {
  return page.evaluate(`(() => ({
    green: [...document.querySelectorAll("p")].filter((p) => /bg-emerald-50/.test(p.className)).map((p) => p.textContent.trim()),
    red:   [...document.querySelectorAll("p")].filter((p) => /bg-red-50/.test(p.className) && !p.closest('[data-testid="save-toast"]')).map((p) => p.textContent.trim()),
  }))()`) as Promise<{ green: string[]; red: string[] }>;
}

type LiveRegion = {
  tag: string;
  role: string;
  ariaLive: string | null;
  ariaAtomic: string | null;
  ariaLabel: string | null;
  testid: string;
  text: string;
};

/**
 * Every element that is a LIVE REGION around the toast, read from the real DOM:
 * the toast itself, every ancestor of it up to <body>, and its whole subtree.
 *
 * A live region is an element carrying an explicit `aria-live` attribute, or one
 * whose ROLE has an implicit aria-live value — `status`, `alert`, `log`,
 * `marquee`, `timer` (WAI-ARIA 1.2 §5.2.9). One entry per ELEMENT, because the
 * number of elements is the number of regions that can announce the message.
 */
async function toastLiveRegions(page: Page) {
  return page.evaluate(`(() => {
    const IMPLICIT = ["status", "alert", "log", "marquee", "timer"];
    const isLive = (el) => el.hasAttribute("aria-live") || IMPLICIT.includes(el.getAttribute("role") || "");
    const describe = (el) => ({
      tag: el.tagName.toLowerCase(),
      role: el.getAttribute("role") || "",
      ariaLive: el.getAttribute("aria-live"),
      ariaAtomic: el.getAttribute("aria-atomic"),
      ariaLabel: el.getAttribute("aria-label"),
      testid: el.getAttribute("data-testid") || "",
      text: (el.textContent || "").trim().slice(0, 60),
    });
    const toast = document.querySelector('[data-testid="save-toast"]');
    if (!toast) return { present: false };
    const around = [];
    for (let el = toast; el && el !== document.body; el = el.parentElement) {
      if (isLive(el)) around.push(describe(el));
    }
    for (const el of toast.querySelectorAll("*")) if (isLive(el)) around.push(describe(el));
    return {
      present: true,
      around,
      toast: describe(toast),
      toastButtons: [...toast.querySelectorAll("button")].map((b) => b.getAttribute("aria-label") || (b.textContent || "").trim()),
    };
  })()`) as Promise<{
    present: boolean;
    around?: LiveRegion[];
    toast?: LiveRegion;
    toastButtons?: string[];
  }>;
}

/**
 * What the BROWSER says about the toast, from its own accessible tree — the
 * `live` / `atomic` properties a screen reader consumes, not the attributes this
 * page wrote. The toast's DOM node is resolved to a backend node id so the AX
 * node is the toast itself and not some other live region on the page.
 *
 * `liveChain` is the toast's AX node plus every AX ANCESTOR of it with a `live`
 * property != "off": the pre-fix shape put the wrapper in that chain, which is
 * exactly the second announcement. `liveSubtree` is the same for its descendants.
 * `documentLive` is the whole page, for the record — this page has other live
 * regions of its own (an `alert`), so the count that matters is the chain's.
 */
async function toastAX(page: Page) {
  const cdp = await page.context().newCDPSession(page);
  await cdp.send("Accessibility.enable");
  await cdp.send("DOM.enable");
  const doc = (await cdp.send("DOM.getDocument")) as { root: { nodeId: number } };
  const { nodeId } = (await cdp.send("DOM.querySelector", {
    nodeId: doc.root.nodeId,
    selector: '[data-testid="save-toast"]',
  })) as { nodeId: number };
  const { node } = (await cdp.send("DOM.describeNode", { nodeId })) as {
    node: { backendNodeId: number };
  };
  type AxNode = {
    nodeId: string;
    parentId?: string;
    childIds?: string[];
    backendDOMNodeId?: number;
    role?: { value?: string };
    name?: { value?: string };
    properties?: { name: string; value: { value?: unknown } }[];
  };
  const { nodes } = (await cdp.send("Accessibility.getFullAXTree")) as { nodes: AxNode[] };
  const prop = (n: AxNode, name: string) => n.properties?.find((p) => p.name === name)?.value?.value;
  const isLive = (n: AxNode) => prop(n, "live") !== undefined && prop(n, "live") !== "off";
  const label = (n: AxNode) => ({
    role: n.role?.value ?? "",
    name: (n.name?.value ?? "").slice(0, 60),
    live: prop(n, "live"),
    atomic: prop(n, "atomic"),
  });
  const byId = new Map(nodes.map((n) => [n.nodeId, n]));
  const mine = nodes.find((n) => n.backendDOMNodeId === node.backendNodeId);
  const liveChain: ReturnType<typeof label>[] = [];
  for (let n = mine; n; n = n.parentId ? byId.get(n.parentId) : undefined) {
    if (isLive(n)) liveChain.push(label(n));
  }
  const liveSubtree: ReturnType<typeof label>[] = [];
  const walk = (ids: string[] | undefined) => {
    for (const id of ids ?? []) {
      const n = byId.get(id);
      if (!n) continue;
      if (isLive(n)) liveSubtree.push(label(n));
      walk(n.childIds);
    }
  };
  if (mine) walk(mine.childIds);
  return {
    found: !!mine,
    toast: mine ? label(mine) : null,
    liveChain,
    liveSubtree,
    documentLive: nodes.filter(isLive).map(label),
  };
}

type Site = { tab: string; button: string; failure: string };

/** Drive one tab's failed save and assert the card's acceptance on it. */
async function failedSaveMustPersist(page: Page, site: Site) {
  test.setTimeout(120_000);

  // A FRESH profile state: only the workspace and the language this guard needs.
  // The board has been bitten by a leftover `jf_lang` from another card's run.
  await page.addInitScript(() => {
    try {
      localStorage.clear();
      localStorage.setItem("jf_workspace", "demo");
      localStorage.setItem("jf_lang", "en");
    } catch {}
  });
  await page.context().clearCookies();
  await stubAPI(page, 500);

  await page.goto(`/settings?tab=${site.tab}`, { waitUntil: "domcontentloaded" });
  await expect(page.locator("html"), "the reading is the ENGLISH default").toHaveAttribute("lang", "en");

  const button = page.getByRole("button", { name: site.button, exact: true });
  await expect(button, `${site.tab}: the Save button must be reachable`).toBeVisible({ timeout: 45_000 });
  await button.click();

  // ---- AT 1s: the failure is present, and it is NOT success chrome --------
  await page.waitForTimeout(1200);
  const first = await toastState(page);
  expect(first.present, `${site.tab}: a FAILED save must render its message at all`).toBe(true);
  expect(first.text, `${site.tab}: the failure sentence`).toContain(site.failure);
  expect(
    first.bg,
    `${site.tab}: the failure is painted in SUCCESS chrome (${first.bg}) — the card's acceptance item 2. ` +
      `The success fill is ${EMERALD_50}; a failure must be on ${RED_50}.`,
  ).toBe(RED_50);
  expect(first.tone, `${site.tab}: the toast does not report its failure tone`).toBe("failure");
  // The inline banner is the other half of what the timer used to erase.
  const bannersAt1s = await banners(page);
  expect(
    bannersAt1s.red.some((t) => t.includes(site.failure)),
    `${site.tab}: the inline banner must carry the failure on its red side, not the emerald one (green=${JSON.stringify(bannersAt1s.green)})`,
  ).toBe(true);

  // ---- SAMPLED TO 8s: it must still be there, and still be the same thing --
  // Sampled every 200ms rather than read once, because the defect is a set-then-
  // WIPED message and a single late read cannot tell "never rendered" from
  // "rendered and deleted". A wipe anywhere in the window fails the `all`.
  const samples: boolean[] = [];
  for (let i = 0; i < 35; i += 1) {
    await page.waitForTimeout(200);
    samples.push((await toastState(page)).present);
  }
  const lastMissing = samples.lastIndexOf(false);
  expect(
    samples.every(Boolean),
    `${site.tab}: the failure must survive the whole 8s window. Samples (200ms apart): ` +
      `${samples.map((s) => (s ? "1" : "0")).join("")}. A 0 after a 1 is the 6s auto-dismiss this card fixes ` +
      `(last missing at sample ${lastMissing}, ~${lastMissing * 200}ms); all-zero means it never rendered.`,
  ).toBe(true);

  // Still the failure, still red, at the end of the window — not some later text.
  const last = await toastState(page);
  expect(last.text, `${site.tab}: the surviving message must still be the failure`).toContain(site.failure);
  expect(last.bg, `${site.tab}: the surviving failure must still be on the failure fill`).toBe(RED_50);
  const bannersAt8s = await banners(page);
  expect(
    bannersAt8s.red.some((t) => t.includes(site.failure)),
    `${site.tab}: the inline failure banner must still be up at 8s (green=${JSON.stringify(bannersAt8s.green)}, red=${JSON.stringify(bannersAt8s.red)})`,
  ).toBe(true);
}

test("a failed Tracking save is still on screen 8s later, and is not a success", async ({ page }) => {
  await failedSaveMustPersist(page, {
    tab: "tracking",
    button: DICT["set.saveTracking"].en,
    failure: DICT["prof.failed"].en,
  });
});

test("a failed e-Invoice save is still on screen 8s later, and is not a success", async ({ page }) => {
  await failedSaveMustPersist(page, {
    tab: "einvoice",
    button: DICT["set.saveEinvoice"].en,
    failure: DICT["set.failedSave"].en,
  });
});

test("CONTROL: a SUCCESSFUL save still auto-dismisses, on success chrome", async ({ page }) => {
  // The control that keeps the two tests above honest: if the 6s timer were
  // simply broken (or the toast always red), a failure "surviving" would prove
  // nothing. A genuine save must be emerald AND gone by ~7s.
  test.setTimeout(120_000);
  await page.addInitScript(() => {
    try {
      localStorage.clear();
      localStorage.setItem("jf_workspace", "demo");
      localStorage.setItem("jf_lang", "en");
    } catch {}
  });
  await page.context().clearCookies();
  await stubAPI(page, 200);

  await page.goto("/settings?tab=tracking", { waitUntil: "domcontentloaded" });
  const button = page.getByRole("button", { name: DICT["set.saveTracking"].en, exact: true });
  await expect(button).toBeVisible({ timeout: 45_000 });
  await button.click();

  await page.waitForTimeout(1200);
  const shown = await toastState(page);
  expect(shown.present, "a successful save must render its confirmation").toBe(true);
  expect(shown.text, "the success sentence").toContain(DICT["set.trackingSaved"].en.replace(/\s*✓\s*$/, ""));
  expect(shown.bg, "a success is on the emerald fill").toBe(EMERALD_50);
  expect(shown.tone).toBe("success");

  // Gone after the 6s timer (sampled, so the moment is measured not assumed).
  let goneAt = -1;
  for (let i = 0; i < 50; i += 1) {
    await page.waitForTimeout(200);
    if (!(await toastState(page)).present) {
      goneAt = (i + 1) * 200;
      break;
    }
  }
  expect(
    goneAt,
    "a SUCCESS must still auto-dismiss (measured ~6s): if it never does, the failure tests above are meaningless",
  ).toBeGreaterThan(0);
});

test("the toast is ONE live region — not a live region nested inside another (t_c4bff404)", async ({ page }) => {
  test.setTimeout(120_000);

  // Fresh profile state again: this is a different question about the toast, but
  // it must see the same English default and the same stubbed 500.
  await page.addInitScript(() => {
    try {
      localStorage.clear();
      localStorage.setItem("jf_workspace", "demo");
      localStorage.setItem("jf_lang", "en");
    } catch {}
  });
  await page.context().clearCookies();
  await stubAPI(page, 500);

  await page.goto("/settings?tab=tracking", { waitUntil: "domcontentloaded" });
  await expect(page.locator("html"), "the reading is the ENGLISH default").toHaveAttribute("lang", "en");
  const button = page.getByRole("button", { name: DICT["set.saveTracking"].en, exact: true });
  await expect(button, "the Save button must be reachable").toBeVisible({ timeout: 45_000 });
  await button.click();
  await expect(
    page.locator('[data-testid="save-toast"]'),
    "the failed save must render a toast for this probe to measure",
  ).toBeVisible({ timeout: 15_000 });

  const regions = await toastLiveRegions(page);
  const ax = await toastAX(page);
  console.log("live-region probe (DOM):", JSON.stringify(regions, null, 2));
  console.log("live-region probe (accessible tree):", JSON.stringify(ax, null, 2));

  expect(regions.present, "the toast must be in the DOM").toBe(true);
  // ---- THE CARDED DEFECT: one message, exactly ONE live region --------------
  // `around` is the toast, its ancestors up to <body>, and its subtree. The
  // pre-fix shape put an aria-live="polite" on the positioner wrapper as well as
  // role="status" on the toast, so this counted 2 and a screen reader could
  // announce the message twice.
  const around = regions.around ?? [];
  expect(
    around.length,
    `one message, ONE live region: the toast plus its ancestors and subtree declares ${around.length} — ` +
      `${JSON.stringify(around)}. A nested pair (aria-live on the positioner wrapper + role="status" on the ` +
      `toast) can announce the same sentence twice.`,
  ).toBe(1);
  expect(around[0]?.testid, "the single live region must be the TOAST itself, not its wrapper").toBe("save-toast");
  expect(around[0]?.role, 'the single live region must carry role="status"').toBe("status");

  // ---- Acceptance item 4 of t_bc97048a, re-measured AFTER the change --------
  // role=status + no aria-label + only a Close button. The `aria-live=polite`
  // half of that item is no longer an ATTRIBUTE — it is the implicit value of the
  // status role (WAI-ARIA 1.2 §5.2.9) — so it is asserted where it is real: the
  // browser's own accessible tree, below.
  expect(regions.toast?.role, "acceptance item 4: role=status").toBe("status");
  expect(
    regions.toast?.ariaLabel,
    "acceptance item 4: no aria-label — a label on a live region REPLACES the announced message",
  ).toBeNull();
  expect(regions.toastButtons, "acceptance item 4: the only control in the region is Close").toEqual([
    DICT["set.tlsCertClose"].en,
  ]);

  // ---- The announcement itself must survive the de-duplication -------------
  expect(ax.found, "the toast must have a node in the browser's accessible tree").toBe(true);
  expect(
    ax.toast,
    `the toast's AX node must still be a live region, and still polite: role="status" implies ` +
      `aria-live=polite (WAI-ARIA 1.2 §5.2.9), so removing the wrapper's redundant attribute must not ` +
      `silence it. Measured: ${JSON.stringify(ax.toast)}`,
  ).toEqual(expect.objectContaining({ role: "status", live: "polite", atomic: true }));
  // ...and the browser must expose that ONE live node for this message: the toast
  // itself, with no live AX ancestor above it and no live AX descendant inside it.
  // Pre-fix the wrapper sat in this chain (a second `live=polite` node), which is
  // the double announcement in the browser's own model of the page.
  expect(
    ax.liveChain.length,
    `the toast's AX chain holds ${ax.liveChain.length} live node(s) — ${JSON.stringify(ax.liveChain)}. ` +
      `Exactly one (the toast) is the de-duplicated shape; an aria-live ancestor adds a second ` +
      `announcement of the same message.`,
  ).toBe(1);
  expect(ax.liveChain[0]?.role, "the one live AX node in the chain must be the status toast").toBe("status");
  expect(
    ax.liveSubtree,
    `a live region inside the toast would announce separately too: ${JSON.stringify(ax.liveSubtree)}`,
  ).toEqual([]);
});
