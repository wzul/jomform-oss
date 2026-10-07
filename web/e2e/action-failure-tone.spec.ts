// Regression guard for t_c88c854d — on /sales, /spam and /form-settings a FAILED
// action used to render in the SAME banner as a success, so neither tone was
// readable.
//
// THE DEFECT (found by reading the sources while verifying t_2574be79, and
// reproduced in a real browser before this fix)
//   Each of these three pages held ONE message state (`msg`) that carried both
//   outcomes and painted it with ONE class list — `text-xs
//   text-[var(--jf-primary)]`, i.e. indigo 12px text, no fill, no icon:
//
//     web/src/app/sales/page.tsx:849          {msg && <p className="mt-3 text-xs text-[var(--jf-primary)]">{msg}</p>}
//     web/src/app/spam/page.tsx:175           same chrome
//     web/src/app/form-settings/page.tsx:1257 same chrome
//
//   The writers carry BOTH a confirmation and a failure sentence:
//     * /sales — confirm / capture / release / refund / bulk-capture each write
//       `... ✓` on success and `apiErr(...)` on refusal;
//     * /spam — `spam.unspammed` vs `spam.unspamFail`;
//     * /form-settings — `fs.saved` / `fs.menuSaved` / `fs.chipSaved` … vs
//       `prof.failed` / `fs.*Fail` / `publishErrorText(...)`.
//
//   Measured on the pre-fix build (Chromium, production build, API stubbed at
//   the browser level, a real 500 forced on the action's endpoint, fresh
//   localStorage + no cookies, lang=en), the failure reading was identical to
//   the success reading except for the sentence:
//
//     /sales          refund 500  -> {tone:null,   bg:#ffffff, ink:rgb(94,106,210), role:null}
//     /sales          refund 200  -> {tone:null,   bg:#ffffff, ink:rgb(94,106,210), role:null}
//     /spam           unspam 500  -> {tone:null,   bg:#ffffff, ink:rgb(94,106,210), role:null}
//     /form-settings  save   500  -> {tone:null,   bg:#ffffff, ink:rgb(94,106,210), role:null}
//
//   A merchant scanning for a failure saw nothing that read as one.
//
// THE RULE THIS PINS (the one PR #997 established on /settings, card t_ad2af554)
//   The TONE travels with the text: a page keeps a second state (`msgOk`) and
//   every non-empty writer sets both in one call, so a failure is never inferred
//   from the sentence and never inherits a previous save's tone. A failure is
//   painted `bg-red-50` + `text-red-800` and is a live `role="alert"`; a success
//   keeps the app's emerald confirmation chrome.
//
// WHY A BROWSER SPEC *AND* A SOURCE GUARD
//   `src/lib/actionFailureTone.test.ts` stops the mechanism coming back out of
//   the code (a writer that loses its tone, a banner that paints one class list,
//   a `<p>` that is not a live region for a failure). Neither half is sufficient
//   alone: that scan cannot read a computed style or an ARIA role, and this file
//   cannot see a page nobody drives. The SUCCESS arms below are not decoration:
//   they prove the probe can tell the two apart, so a red failure reading cannot
//   be a "the banner is always red" artefact.
//
// The API is stubbed at the browser level (a real HTTP status is fulfilled), so
// the gate needs nothing but the build it is already serving.
import { test, expect, type Page } from "@playwright/test";
import { DICT } from "../src/components/i18nDict";

/** The two tones as the browser resolves them (Tailwind v4 red-50 / emerald-50). */
const RED_50 = "#fef2f2";
const EMERALD_50 = "#ecfdf5";
/** Tailwind v4's `text-red-700` — the failure ink, 5.87:1 on red-50 (AA). */
const RED_700 = "#c10007";
/** The pre-fix ink: `--jf-primary`, which is what BOTH outcomes used to be. */
const JF_PRIMARY = "#5e6ad2";

const ME = {
  id: "u1",
  email: "merchant@example.com",
  name: "Merchant",
  lang: "en",
  is_super_admin: false,
};

/** The dashboard bundle every authenticated page reads on mount. */
const BUNDLE = {
  me: ME,
  workspaces: [{ id: "ws1", name: "Demo", slug: "demo", role: "admin" }],
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

/**
 * A FRESH browser state: no cookie, and only the workspace + language this guard
 * needs. The board has been bitten by a leftover `jf_lang` from an earlier card's
 * run, and `demo`/`en` is what a new visitor gets.
 */
async function freshState(page: Page) {
  await page.addInitScript(() => {
    try {
      localStorage.clear();
      localStorage.setItem("jf_workspace", "demo");
      localStorage.setItem("jf_lang", "en");
    } catch {
      /* storage unavailable */
    }
  });
  await page.context().clearCookies();
}

/**
 * The stub every page below shares. `status` is what the action's own endpoint
 * answers: 500 reproduces the pre-fix defect, 200 is the success control. The
 * catch-all is registered FIRST because Playwright resolves routes in REVERSE
 * registration order, so every specific route below it wins.
 */
async function stubCommon(page: Page, json: (status: number, body: unknown) => unknown) {
  await page.route(/(.*)\/api\/.*$/, (r) => r.fulfill(json(200, {}) as never));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(200, BUNDLE) as never));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(200, ME) as never));
  // lang "" keeps the browser value in effect, which is how this spec pins en.
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json(200, { lang: "" }) as never));
  await page.route(/(.*)\/api\/rest\/currency\/rates$/, (r) => r.fulfill(json(200, { rates: [], cached: false }) as never));
  await page.route(/(.*)\/api\/rest\/forms(\?.*)?$/, (r) => r.fulfill(json(200, []) as never));
}

const jsonOf = () => (status: number, body: unknown) => ({
  status,
  contentType: "application/json",
  body: JSON.stringify(body),
});

/**
 * The banner's rendered shape, read out of the live DOM: presence, the text, the
 * tone attribute, the ARIA role, and the fill/ink resolved to the sRGB bytes the
 * compositor actually paints.
 *
 * `getComputedStyle().backgroundColor` returns whatever colour space Tailwind
 * emitted — a `lab(...)` for the Tailwind v4 ramps — so the value is
 * round-tripped through a canvas, exactly as
 * `web/e2e/settings-toast-failure.spec.ts` and
 * `web/e2e/builder-canvas-price-contrast.spec.ts` do. A transparent background
 * (the PRE-FIX banner: no fill at all) composites to white, which is how the old
 * shape shows up here as `#ffffff` rather than a false pass.
 */
type Reading = { present: boolean; text: string; bg: string; ink: string; tone: string; role: string };

async function reading(page: Page): Promise<Reading> {
  return page.evaluate(`(() => {
    const el = document.querySelector('[data-testid="action-msg"]');
    if (!el) return { present: false, text: "", bg: "", ink: "", tone: "", role: "" };
    const cv = document.createElement("canvas");
    cv.width = cv.height = 1;
    const ctx = cv.getContext("2d", { willReadFrequently: true });
    const hex = (css) => {
      ctx.clearRect(0, 0, 1, 1);
      ctx.fillStyle = css;
      ctx.fillRect(0, 0, 1, 1);
      const d = ctx.getImageData(0, 0, 1, 1).data;
      return "#" + [d[0], d[1], d[2]].map((v) => v.toString(16).padStart(2, "0")).join("");
    };
    const cs = getComputedStyle(el);
    return {
      present: true,
      text: (el.textContent || "").trim(),
      bg: hex(cs.backgroundColor),
      ink: hex(cs.color),
      tone: el.getAttribute("data-tone") || "",
      role: el.getAttribute("role") || "",
    };
  })()`) as Promise<Reading>;
}

/** Wait until the banner exists at all, then read it. */
async function settledReading(page: Page): Promise<Reading> {
  await page.waitForSelector('[data-testid="action-msg"]', { timeout: 30_000 });
  const r = await reading(page);
  expect(r.text, "the banner rendered empty — there is nothing to judge").not.toBe("");
  return r;
}

/** Every assertion the card's acceptance makes, in one place per reading. */
function expectFailure(r: Reading, where: string, mustContain: string) {
  expect(r.text, `${where}: the failure sentence`).toContain(mustContain);
  expect(
    r.bg,
    `${where}: a FAILED action is painted ${r.bg} — the PRE-FIX reading (no fill at all composites to ` +
      `#ffffff, the same as the success banner) or a success fill is still in place. A failure must be red.`,
  ).toBe(RED_50);
  expect(
    r.ink,
    `${where}: the failure ink is ${r.ink}. It must be text-red-700 (${RED_700}, 5.87:1 on red-50 — the ` +
      `same ink /settings paints its failure banner with) — the pre-fix indigo (--jf-primary ` +
      `${JF_PRIMARY}) is the very reading this card is about`,
  ).toBe(RED_700);
  expect(r.tone, `${where}: the banner does not report its failure tone`).toBe("failure");
  // A failure must also be ANNOUNCED, not only seen: the same silence for a
  // screen reader is the other half of "the merchant cannot tell them apart".
  expect(r.role, `${where}: a failure banner must be a live alert region`).toBe("alert");
}

function expectSuccess(r: Reading, where: string, mustContain: string) {
  expect(r.text, `${where}: the success sentence`).toContain(mustContain);
  expect(
    r.bg,
    `${where}: a SUCCESS (${r.bg}) must NOT wear the failure fill — the control that keeps the failure ` +
      `readings above honest`,
  ).toBe(EMERALD_50);
  expect(r.tone, `${where}: the banner does not report its success tone`).toBe("success");
  expect(r.role, `${where}: a success is not a live alert region`).toBe("");
  expect(
    r.bg,
    `${where}: the failure and success banners are the same colour (${r.bg}) — the carded defect`,
  ).not.toBe(RED_50);
}

/** Answer every native prompt/confirm with `value` (the refund/capture flows use them). */
function answerDialogs(page: Page, value: string) {
  page.on("dialog", (d) => {
    if (d.type() === "prompt") void d.accept(value);
    else void d.accept();
  });
}

// ─────────────────────────────────────────────────────────────────────────────
// /sales — a FAILED refund vs a successful one
// ─────────────────────────────────────────────────────────────────────────────
const SALE = {
  ref_code: "SALE-TONE-1",
  status: "paid",
  total_sen: 12500,
  payment_method: "fpx",
  buyer: "Buyer",
  created_at: "2026-01-02T03:04:05Z",
  form_id: "form1",
  items: [],
};

async function stubSales(page: Page, refundStatus: number) {
  const json = jsonOf();
  await stubCommon(page, json);
  await page.route(/(.*)\/api\/workspaces\/[^/]+\/settings(\?.*)?$/, (r) =>
    r.fulfill(json(200, { sound_enabled: false }) as never),
  );
  await page.route(/(.*)\/api\/rest\/sales(\?.*)?$/, (r) =>
    r.fulfill(json(200, { items: [SALE], page: 1, limit: 15, total: 1 }) as never),
  );
  await page.route(/(.*)\/api\/rest\/sales\/[^/]+\/refund(\?.*)?$/, (r) =>
    r.fulfill(json(refundStatus, refundStatus === 200 ? { ok: true } : { error: "gagal" }) as never),
  );
}

/**
 * Drive `/sales` → the row's Refund button → the page's own message state.
 * `promptValue` "" means "refund the full balance" (the page sends no body), and
 * the following `window.confirm` is accepted, so exactly one POST is issued.
 */
async function driveSalesRefund(page: Page, refundStatus: number) {
  await freshState(page);
  await stubSales(page, refundStatus);
  answerDialogs(page, "");
  await page.goto("/sales", { waitUntil: "domcontentloaded" });
  await expect(page.locator("html"), "the reading is the ENGLISH default").toHaveAttribute("lang", "en");
  const row = page.locator("tr", { hasText: SALE.ref_code });
  await expect(row, "the sale row must render before its action can be driven").toBeVisible({ timeout: 45_000 });
  await row.getByRole("button", { name: DICT["sales.refund"].en, exact: true }).click();
}

test("a FAILED refund on /sales is red-filled and a live alert, not the success banner", async ({ page }) => {
  test.setTimeout(120_000);
  await driveSalesRefund(page, 500);
  const r = await settledReading(page);
  console.log(`/sales refund 500 -> ${JSON.stringify(r)}`);
  // The page's OWN sentence: the 500 body carries no code apiErr can map, so it
  // falls back to the refund path's localized refusal, "Refund gagal <ref>."
  expectFailure(r, "/sales refund 500", "Refund gagal");
});

test("CONTROL: a SUCCESSFUL refund on /sales does NOT wear the failure chrome", async ({ page }) => {
  test.setTimeout(120_000);
  await driveSalesRefund(page, 200);
  const r = await settledReading(page);
  console.log(`/sales refund 200 -> ${JSON.stringify(r)}`);
  expectSuccess(r, "/sales refund 200", "✓");
});

// ─────────────────────────────────────────────────────────────────────────────
// /sales — a FAILED workspace-settings save from the HEADER sound switch
// (card t_bfe1cda5, follow-up to t_c88c854d)
// ─────────────────────────────────────────────────────────────────────────────
//
// THE DEFECT THIS ARM PINS
//   The sound switch lives in the page HEADER, but its save failure was written
//   to `err` — the page's LOAD error, whose only render sites sit inside the
//   empty-table branch (`!checking && rows.length === 0`). With a row on the
//   table (the normal state) the switch flipped back and NO failure was rendered
//   anywhere. Measured on the pre-fix build:
//
//     /sales sound PUT 500 -> aria-checked before=false after=false
//     /sales sound PUT 500 -> elements carrying the failure sentence: {"hit":[],"switches":1}
//
//   The row is therefore part of the fixture, not decoration: with an EMPTY
//   table the old `err` banner WOULD render and this arm would pass on the
//   broken code. One row is what separates the two builds.

/** The switch's settings route, with the PUT status under test. */
async function stubSalesSound(page: Page, putStatus: number) {
  const json = jsonOf();
  await stubCommon(page, json);
  await page.route(/(.*)\/api\/rest\/sales(\?.*)?$/, (r) =>
    r.fulfill(json(200, { items: [SALE], page: 1, limit: 15, total: 1 }) as never),
  );
  await page.route(/(.*)\/api\/workspaces\/[^/]+\/settings(\?.*)?$/, (r) => {
    if (r.request().method() === "PUT") {
      return r.fulfill(json(putStatus, putStatus === 200 ? { ok: true } : {}) as never);
    }
    return r.fulfill(json(200, { sound_enabled: false }) as never);
  });
}

/** Drive `/sales` → the header sound switch, with ONE sale row on the table. */
async function driveSalesSound(page: Page, putStatus: number) {
  await freshState(page);
  await stubSalesSound(page, putStatus);
  await page.goto("/sales", { waitUntil: "domcontentloaded" });
  await expect(page.locator("html"), "the reading is the ENGLISH default").toHaveAttribute("lang", "en");
  // The row must be there: this is the state in which the old `err` banner could
  // NOT render, i.e. the state the card was filed on.
  await expect(
    page.locator("tr", { hasText: SALE.ref_code }),
    "the sale row must render — with an EMPTY table the old banner would have shown and this arm would pass on the broken build",
  ).toBeVisible({ timeout: 45_000 });
  const sw = page.getByRole("switch");
  await expect(sw, "the sound switch must be enabled once the setting has loaded").toBeEnabled({
    timeout: 45_000,
  });
  await expect(sw, "the switch starts OFF").toHaveAttribute("aria-checked", "false");
  await sw.click();
}

test("a FAILED sound-setting save on /sales reaches a visible failure banner (t_bfe1cda5)", async ({ page }) => {
  test.setTimeout(120_000);
  await driveSalesSound(page, 500);
  // The handler took the failure path: the optimistic flip was REVERTED.
  await expect(
    page.getByRole("switch"),
    "the switch did not flip back — the failed save was not handled",
  ).toHaveAttribute("aria-checked", "false", { timeout: 30_000 });
  const r = await settledReading(page);
  console.log(`/sales sound PUT 500 -> switch aria-checked=${await page.getByRole("switch").getAttribute("aria-checked")} banner=${JSON.stringify(r)}`);
  expectFailure(r, "/sales sound PUT 500", DICT["sales.soundSaveFail"].en);
});

test("CONTROL: a SUCCESSFUL sound-setting save on /sales renders no failure banner (t_bfe1cda5)", async ({ page }) => {
  test.setTimeout(120_000);
  await driveSalesSound(page, 200);
  // The flip STAYS on, and the page must NOT claim a failure.
  await expect(
    page.getByRole("switch"),
    "a successful save must leave the switch on",
  ).toHaveAttribute("aria-checked", "true", { timeout: 30_000 });
  await expect(
    page.locator('[data-testid="action-msg"][data-tone="failure"]'),
    "a successful sound save rendered a FAILURE banner — the control that keeps the reading above honest",
  ).toHaveCount(0);
});

// ─────────────────────────────────────────────────────────────────────────────
// /sales — a MIXED bulk-capture vs an all-captured one (card t_0863b3ca)
// ─────────────────────────────────────────────────────────────────────────────
//
// THE DECISION THIS ARM PINS (PR #1012's own judgement call)
//   Bulk capture is PARTIAL by design: the endpoint answers with how many
//   authorizations it captured and how many it did not, and the page reports the
//   mixed outcome with the FAILURE tone — some authorizations are still
//   uncaptured and the merchant has to read each status. Only an all-captured run
//   is a success. The card that filed this arm (t_0863b3ca, follow-up to the QA
//   card t_98be596e) measured that this decision had no guard: neutralising it by
//   VALUE —
//
//     web/src/app/sales/page.tsx:647
//     -      if (d.failed) showMsgFail(summary);
//     +      if (false) showMsgFail(summary);
//
//   — put a mixed run back on the SUCCESS banner while the shipped source guard
//   printed `6/6 pass` and this spec had no arm that would have driven it. The
//   `showMsgFail` CALL was still there for the scan to find; only the condition
//   deciding it had been hollowed out, which is why the source guard now reads
//   the CONDITION and why this arm measures the rendered result of both outcomes.
//
//   The CONTROL is not decoration here either — it is what makes the failure arm
//   non-vacuous: on the mutation the CONTROL still passes (an all-captured run was
//   always a success), so a red failure arm cannot be an artefact of "the banner
//   is always red".

/** A capture-PENDING sale: `skip_capture` with no `captured_at` (issue #12). */
const CAPTURE_PENDING_SALE = {
  ...SALE,
  ref_code: "SALE-TONE-CAP",
  status: "authorized",
  skip_capture: true,
};

/** The bulk-capture outcome under test, as the endpoint reports it. */
type CaptureOutcome = { captured: number; requested: number; failed: number };

async function stubSalesBulkCapture(page: Page, outcome: CaptureOutcome) {
  const json = jsonOf();
  await stubCommon(page, json);
  await page.route(/(.*)\/api\/workspaces\/[^/]+\/settings(\?.*)?$/, (r) =>
    r.fulfill(json(200, { sound_enabled: false }) as never),
  );
  await page.route(/(.*)\/api\/rest\/sales(\?.*)?$/, (r) =>
    r.fulfill(json(200, { items: [CAPTURE_PENDING_SALE], page: 1, limit: 15, total: 1 }) as never),
  );
  await page.route(/(.*)\/api\/rest\/sales\/bulk-capture(\?.*)?$/, (r) => r.fulfill(json(200, outcome) as never));
}

/** Drive `/sales` → the header's "Capture all pending" button → the banner. */
async function driveSalesBulkCapture(page: Page, outcome: CaptureOutcome) {
  await freshState(page);
  await stubSalesBulkCapture(page, outcome);
  answerDialogs(page, "");
  await page.goto("/sales", { waitUntil: "domcontentloaded" });
  await expect(page.locator("html"), "the reading is the ENGLISH default").toHaveAttribute("lang", "en");
  await expect(
    page.locator("tr", { hasText: CAPTURE_PENDING_SALE.ref_code }),
    "the capture-pending row must render before its action can be driven",
  ).toBeVisible({ timeout: 45_000 });
  // The button only renders when the page finds a capture-pending row, so its
  // presence is also the fixture's own proof that `isCapturePending` matched.
  await page.getByRole("button", { name: `${DICT["sales.captureAll"].en} (1)` }).click();
}

test("a MIXED bulk-capture on /sales is red-filled and a live alert (t_0863b3ca)", async ({ page }) => {
  test.setTimeout(120_000);
  await driveSalesBulkCapture(page, { captured: 0, requested: 1, failed: 1 });
  const r = await settledReading(page);
  console.log(`/sales bulk-capture failed=1 -> ${JSON.stringify(r)}`);
  expectFailure(r, "/sales bulk-capture failed=1", "Captured 0/1");
  expect(r.text, "/sales bulk-capture failed=1: the mixed run must say to check each status").toContain(
    "1 failed",
  );
});

test("CONTROL: an ALL-CAPTURED bulk-capture on /sales is a success (t_0863b3ca)", async ({ page }) => {
  test.setTimeout(120_000);
  await driveSalesBulkCapture(page, { captured: 1, requested: 1, failed: 0 });
  const r = await settledReading(page);
  console.log(`/sales bulk-capture failed=0 -> ${JSON.stringify(r)}`);
  expectSuccess(r, "/sales bulk-capture failed=0", "Captured 1/1");
  expect(r.text, "/sales bulk-capture failed=0: an all-captured run is the success arm").toContain("0 failed");
});

// ─────────────────────────────────────────────────────────────────────────────
// /spam — a FAILED un-spam vs a successful one
// ─────────────────────────────────────────────────────────────────────────────
const SPAM_SUB = {
  id: "sub_tone_1",
  form_id: "form1",
  data: { full_name: "Buyer" },
  spam_reason: "looks like spam",
  created_at: "2026-01-02T03:04:05Z",
};

async function stubSpam(page: Page, unspamStatus: number) {
  const json = jsonOf();
  await stubCommon(page, json);
  await page.route(/(.*)\/api\/rest\/spam\/submissions(\?.*)?$/, (r) =>
    r.fulfill(json(200, { items: [SPAM_SUB] }) as never),
  );
  await page.route(/(.*)\/api\/rest\/spam\/sales(\?.*)?$/, (r) => r.fulfill(json(200, { items: [] }) as never));
  await page.route(/(.*)\/api\/rest\/spam\/submissions\/[^/]+\/unspam(\?.*)?$/, (r) =>
    r.fulfill(json(unspamStatus, unspamStatus === 200 ? { ok: true } : { error: "gagal" }) as never),
  );
}

async function driveSpamUnspam(page: Page, unspamStatus: number) {
  await freshState(page);
  await stubSpam(page, unspamStatus);
  await page.goto("/spam", { waitUntil: "domcontentloaded" });
  await expect(page.locator("html"), "the reading is the ENGLISH default").toHaveAttribute("lang", "en");
  const row = page.locator("tr", { hasText: SPAM_SUB.id.slice(0, 8) });
  await expect(row, "the spam row must render before its action can be driven").toBeVisible({ timeout: 45_000 });
  await row.getByRole("button", { name: DICT["spam.unspam"].en, exact: true }).click();
}

test("a FAILED un-spam on /spam is red-filled and a live alert, not the success banner", async ({ page }) => {
  test.setTimeout(120_000);
  await driveSpamUnspam(page, 500);
  const r = await settledReading(page);
  console.log(`/spam unspam 500 -> ${JSON.stringify(r)}`);
  expectFailure(r, "/spam unspam 500", DICT["spam.unspamFail"].en);
});

test("CONTROL: a SUCCESSFUL un-spam on /spam does NOT wear the failure chrome", async ({ page }) => {
  test.setTimeout(120_000);
  await driveSpamUnspam(page, 200);
  const r = await settledReading(page);
  console.log(`/spam unspam 200 -> ${JSON.stringify(r)}`);
  expectSuccess(r, "/spam unspam 200", DICT["spam.unspammed"].en.replace(/\s*✓\s*$/, ""));
});

// ─────────────────────────────────────────────────────────────────────────────
// /form-settings — a FAILED General save vs a successful one
// ─────────────────────────────────────────────────────────────────────────────
const FORM_ID = "form_tone_guard";

async function stubFormSettings(page: Page, patchStatus: number) {
  const json = jsonOf();
  await stubCommon(page, json);
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+\/chip-feeds(\?.*)?$/, (r) => r.fulfill(json(200, { feeds: null }) as never));
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+\/confirmations(\?.*)?$/, (r) => r.fulfill(json(200, { confirmations: [] }) as never));
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+\/notifications(\?.*)?$/, (r) => r.fulfill(json(200, []) as never));
  await page.route(/(.*)\/api\/rest\/webhooks(\?.*)?$/, (r) => r.fulfill(json(200, []) as never));
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+\/email-settings(\?.*)?$/, (r) => r.fulfill(json(200, {}) as never));
  // The status POST that a SUCCESSFUL general save makes next; pinned before the
  // generic forms route so it wins.
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+\/status(\?.*)?$/, (r) => r.fulfill(json(200, { ok: true }) as never));
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+$/, (r) => {
    if (r.request().method() === "PATCH") {
      return r.fulfill(json(patchStatus, patchStatus === 200 ? { ok: true } : {}) as never);
    }
    return r.fulfill(
      json(200, {
        id: FORM_ID,
        slug: "demo-form",
        title: "Demo form",
        status: "published",
        url: "https://jomform.example/f/demo-form",
        mode: "payment",
        config: { blocks: [] },
        theme: {},
      }) as never,
    );
  });
}

async function driveFormSettingsSave(page: Page, patchStatus: number) {
  await freshState(page);
  await stubFormSettings(page, patchStatus);
  await page.goto(`/form-settings?form=${FORM_ID}`, { waitUntil: "domcontentloaded" });
  await expect(page.locator("html"), "the reading is the ENGLISH default").toHaveAttribute("lang", "en");
  await expect(page.locator('input[value="demo-form"]'), "the form must load before its save is driven").toBeVisible({
    timeout: 45_000,
  });
  // The General tab's Save — scoped to the first section so a later tab's
  // identical "Save" label cannot be clicked.
  const section = page.locator("section").filter({ has: page.locator("h2") }).first();
  await section.getByRole("button", { name: DICT["fs.save"].en, exact: true }).first().click();
}

test("a FAILED save on /form-settings is red-filled and a live alert, not the success banner", async ({ page }) => {
  test.setTimeout(120_000);
  await driveFormSettingsSave(page, 500);
  const r = await settledReading(page);
  console.log(`/form-settings save 500 -> ${JSON.stringify(r)}`);
  expectFailure(r, "/form-settings save 500", DICT["prof.failed"].en);
});

test("CONTROL: a SUCCESSFUL save on /form-settings does NOT wear the failure chrome", async ({ page }) => {
  test.setTimeout(120_000);
  await driveFormSettingsSave(page, 200);
  const r = await settledReading(page);
  console.log(`/form-settings save 200 -> ${JSON.stringify(r)}`);
  expectSuccess(r, "/form-settings save 200", DICT["fs.saved"].en.replace(/\s*✓\s*$/, ""));
});
