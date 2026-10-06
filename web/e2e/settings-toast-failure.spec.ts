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
