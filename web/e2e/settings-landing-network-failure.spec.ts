// Regression guard for card t_9fad3d6a: a THROWN/network failure on the
// landing-page save must put a FAILURE MESSAGE ON SCREEN.
//
// THE DEFECT, OBSERVED IN A REAL BROWSER (found by jf-qa on PR #1017's head;
// pre-existing on origin/main, NOT a regression)
//   /settings → Storefront, the LANDING-page Save button, with the PUT stubbed at
//   the browser level so the fetch REJECTS (aborted — what a lost connection or a
//   request the browser killed looks like):
//
//     button after: "Save landing page" | jf-btn-primary     <- correctly idle
//     banners after: []                                      <- no banner
//     toast after:   null
//     page text changed? NO — the page says NOTHING new
//     pageerror events: []
//
//   Same run, same server:
//     landing, HTTP 500      -> red banner "Failed to save"  ✓
//     footer, network failure -> red banner "Failed to save"  ✓
//     footer, HTTP 500        -> red banner "Failed to save"  ✓
//
//   So a merchant who loses connectivity presses "Save landing page", the button
//   correctly does not claim success — and the page tells them NOTHING. They
//   cannot tell "saved" from "not saved".
//
// WHY A BROWSER SPEC AND NOT ONLY THE SOURCE GUARD
//   The fix is a `catch` that writes the page's error state, and whether that
//   state RENDERS — on the element the merchant is looking at, with the button
//   still idle — is a rendered property no source scan can measure (this repo has
//   no DOM unit runner; `npm run test:unit` is `node --test src/lib/*.test.ts`).
//   `src/lib/saveButton.test.ts` stops the `try` coming back out of the handler;
//   this measures the result on the real built page. Neither half is sufficient
//   alone. The CONTROL below (an HTTP 500 on the same PUT, in the same place)
//   proves the element this spec asserts on is the one the page already uses for a
//   refused save — so a green here cannot be a second, unrelated banner.
//
// The API is stubbed at the browser level, so the gate needs nothing but the
// build it is already serving.
import { test, expect, type Page } from "@playwright/test";

import { signIn } from "./auth";

// The dashboard paths are gated on the SERVER (web/src/middleware.ts): a request
// without the `jf_session` cookie is redirected to /login BEFORE the page
// renders. This spec visits one, so it declares the session it means — before
// the gate existed an anonymous visit rendered the account shell, so this spec
// could (and did) rely on the bug. See e2e/auth.ts.
test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});
import { DICT } from "../src/components/i18nDict";

const BUNDLE = {
  me: { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "en", is_super_admin: true },
  workspaces: [{ id: "ws1", name: "Demo", slug: "demo", role: "admin" }],
  chip_status: null, keys: [], domains: [], webhooks: [], social: [], einvoice: null,
  products: [], summary: { counts: {}, revenue_sen: 0 },
  sales: { items: [], page: 1, limit: 20, total: 0 },
  form_analytics: [], product_analytics: [],
};

const RED_50 = "#fef2f2";

/**
 * stubAPI fulfils the whole dashboard so /settings reaches its loaded state, and
 * answers the landing workspace PUT the way this test asks for: a real HTTP
 * status, or a REJECTED fetch (`abort`).
 *
 * `abort("failed")` is Playwright's network-failure shape — the request never
 * gets a response, so `fetch()` REJECTS. That is the branch this card is about,
 * and it is not the same as `fulfill(500)`: a 500 resolves the promise with
 * `r.ok === false` and the handler's existing `else` branch already covers it.
 */
async function stubAPI(page: Page, put: { kind: "abort" } | { kind: "status"; status: number }) {
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
  // The landing section only renders its fields (and its Save button) when the
  // workspace's landing page is ENABLED, so the settings GET must say so.
  await page.route(/(.*)\/api\/workspaces\/[^/]+\/settings(\?.*)?$/, (r) => {
    if (r.request().method() === "PUT") {
      if (put.kind === "abort") return r.abort("failed");
      return r.fulfill(json(put.status, { error: "boom" }));
    }
    return r.fulfill(json(200, { landing_page: { enabled: true, hero: "Halo" } }));
  });
}

/**
 * What the merchant can actually see after the press:
 *  * the button's label and class list (idle = `jf-btn-primary`, never
 *    `jf-btn-saved`);
 *  * every element whose trimmed text is the failure sentence, with its class
 *    list and its computed fill resolved to sRGB bytes.
 *
 * `getComputedStyle().backgroundColor` returns whatever colour space Tailwind
 * emitted (`lab(…)` for the Tailwind ramp), not an rgb(), so the value is
 * round-tripped through a canvas exactly as `settings-toast-failure.spec.ts`
 * and `builder-canvas-price-contrast.spec.ts` do. Comparing the raw computed
 * string against an rgb() literal would make this guard fail on correct chrome.
 *
 * The failure is matched on its TEXT, not on a test id: the element the fix
 * writes (`lpUrlErr`) carries no test id, and matching the sentence is what the
 * acceptance is stated in terms of.
 */
async function screenState(page: Page, sentence: string) {
  return page.evaluate(
    `(() => {
      const btn = [...document.querySelectorAll("button")].find(
        (b) => (b.textContent || "").trim() === "Save landing page",
      );
      const cv = document.createElement("canvas");
      cv.width = cv.height = 1;
      const ctx = cv.getContext("2d", { willReadFrequently: true });
      const hex = (el) => {
        ctx.clearRect(0, 0, 1, 1);
        ctx.fillStyle = getComputedStyle(el).backgroundColor;
        ctx.fillRect(0, 0, 1, 1);
        const d = ctx.getImageData(0, 0, 1, 1).data;
        return "#" + [d[0], d[1], d[2]].map((v) => v.toString(16).padStart(2, "0")).join("");
      };
      const want = ${JSON.stringify(sentence)};
      const hits = [...document.querySelectorAll("p,div,span")]
        .filter((e) => (e.textContent || "").trim() === want)
        .map((e) => ({ tag: e.tagName.toLowerCase(), cls: e.className, bg: hex(e) }));
      return {
        button: btn
          ? { text: (btn.textContent || "").trim(), cls: btn.className, disabled: btn.disabled }
          : null,
        hits,
      };
    })()`,
  ) as Promise<{
    button: { text: string; cls: string; disabled: boolean } | null;
    hits: { tag: string; cls: string; bg: string }[];
  }>;
}

/**
 * Drive the landing save and assert the card's acceptance on the outcome this
 * test asks for.
 */
async function landingSave(page: Page, put: { kind: "abort" } | { kind: "status"; status: number }) {
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
  await signIn(page);
  await stubAPI(page, put);

  await page.goto("/settings?tab=storefront", { waitUntil: "domcontentloaded" });
  await expect(page.locator("html"), "the reading is the ENGLISH default").toHaveAttribute("lang", "en");

  // The landing section renders its Save button only once the workspace's
  // landing page is enabled AND the settings GET has resolved.
  const button = page.getByRole("button", { name: DICT["set.lpSave"].en, exact: true });
  await expect(button, "the landing Save button must be reachable").toBeVisible({ timeout: 45_000 });

  await button.click();

  // ---- the failure must be ON SCREEN --------------------------------------
  // Polled rather than read once: the write is a React state update inside a
  // promise continuation, so a single immediate read could land before the
  // commit. Bounded, and still an assertion — with the pre-fix handler the
  // element NEVER appears and this fails at the deadline.
  const sentence = DICT["set.failedSave"].en;
  let found: Awaited<ReturnType<typeof screenState>> | null = null;
  const deadline = Date.now() + 10_000;
  while (Date.now() < deadline) {
    const s = await screenState(page, sentence);
    if (s.hits.length > 0) {
      found = s;
      break;
    }
    await page.waitForTimeout(100);
  }
  expect(
    found,
    "a THROWN/network failure on the landing save must put a failure message on screen — " +
      `measured: NOTHING new anywhere on the page (no element carries ${JSON.stringify(sentence)}). ` +
      "The button correctly stays idle, so this message is the only claim the merchant gets " +
      "(card t_9fad3d6a).",
  ).not.toBeNull();
  expect(found!.hits.length, `exactly one element must carry the failure sentence`).toBe(1);
  expect(
    found!.hits[0].bg,
    `the failure must be on failure chrome (${RED_50}), not a success fill — it read ` +
      `${found!.hits[0].bg} on ${found!.hits[0].tag}.${found!.hits[0].cls}`,
  ).toBe(RED_50);

  // ---- and the button must stay IDLE --------------------------------------
  // The other half of the defect: a failure must not confirm. `jf-btn-saved` is
  // the confirmation state (see components/SaveButton.tsx).
  const after = await screenState(page, sentence);
  expect(after.button, "the Save landing page button must still be on screen").not.toBeNull();
  expect(
    after.button!.text,
    "a FAILED save must not turn the button into its confirmation label",
  ).toBe(DICT["set.lpSave"].en);
  expect(
    after.button!.cls,
    "a FAILED save must not put the button into its saved state — it must stay jf-btn-primary",
  ).toBe("jf-btn-primary");
  expect(after.button!.cls).not.toContain("jf-btn-saved");
  // The SaveButton's busy flag is cleared on every path (its `finally`), so the
  // control must be pressable again — a stuck `disabled` is the same silence.
  expect(after.button!.disabled, "the button must not be left disabled after the failure").toBe(false);
}

test("a THROWN/network failure on the landing save shows the failure and leaves the button idle", async ({ page }) => {
  await landingSave(page, { kind: "abort" });
});

test("CONTROL: a REFUSED landing save (HTTP 500) shows the failure in the SAME place", async ({ page }) => {
  // The control that keeps the test above honest. It proves the element this
  // spec asserts on is the page's EXISTING home for a refused landing save —
  // asserted on both branches of the same handler — so a green above cannot be
  // a second, unrelated banner that happens to appear after the fix.
  //
  // It is also a real regression: the 500 branch is the one jf-qa measured
  // working, and this spec would catch it being removed.
  await landingSave(page, { kind: "status", status: 500 });
});
