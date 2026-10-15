// Regression guard for the /products TRASH refusals (card t_602c48f5) — the
// THIRD surface of the defect t_ab4aa85b fixed on /forms' trash view and in the
// Settings Danger zone.
//
// THE DEFECT CLASS. A refusal wrote its sentence into the page's LOAD-error state
// (`error`) and then fired the relist that clears that state on success:
//
//     restoreProduct   setError(t("prod.restoreFail"));        void loadTrash();
//     purgeProduct     setError(apiErr(d, "prod.purgeFail"));  void loadTrash();
//     loadTrash                                                            -> setError("")
//
// The sentence was therefore SET and ERASED in the same tick and was readable at
// NO point. MEASURED on this page's pre-fix bytes, built app, real browser
// (chromium via Playwright), English, fresh storage, with a stubbed refusal on
// the restore route: `p[role="alert"]` sampled every 100 ms thirty times came back
// all-zero, while the trash listing went from ONE call to TWO — so the string was
// right and the same-tick relist was the mechanism, exactly the shape measured on
// /forms.
//
// THE FIX this file pins: the refusal lives in a state NO loader writes
// (`trashErr`), is cleared at the START of the next user action instead of by the
// refetch that reports it, and renders beside the trash rows it is about — with no
// Retry, because a refusal changes nothing about the listing and reloading it is
// not the remedy (the `error` line above the tabs is the LOAD-failure channel and
// carries that Retry).
//
// WHAT THIS GUARD PROVES, on the real built page with a stubbed API:
//   1. the refusal renders at all, in the merchant's own language;
//   2. it SURVIVES the refetch the refusal branch fires — sampled every 100 ms for
//      3 s, so a set-then-wiped message cannot pass as a "rendered late" one;
//   3b the refetch really happened (the listing call count moves), so the sentence
//      is proven to survive a reload that COMPLETED rather than one that never ran;
//   4. the wire's Malay prose appears NOWHERE on the page (issue #6177);
//   5. the trash list the merchant was reading is still on screen;
//   6. a request that REJECTS mid-flight leaves a readable sentence instead of the
//      unhandled rejection the restore handler used to escape as.
//
// ON THE TWO RESTORE LEGS, HONESTLY. `purgeProduct` already routed its refusal
// through the machine `code` (issue #6177); `restoreProduct` did not, and the card
// asked for one call site to be brought in line. The only refusal the SHIPPED
// restore handler can answer is the row not being in the bin — a 404 whose class
// code (`request.notFound`) the dictionary deliberately does NOT translate, so on
// that leg the page's own sentence is the correct answer and the leg pins the
// prose-never-renders half. The SECOND restore leg stubs a coded refusal
// (`product.hasSubscriptions`) that today's restore handler cannot produce, to pin
// the ROUTING rule itself: a `code` on the wire is translated instead of thrown
// away, so the sentence cannot silently regress if the API ever grows a coded
// restore refusal. That leg is a claim about the CLIENT, not about the server.
//
// The API is stubbed at the browser level (no Go server, no session, no DB), so
// the gate needs nothing but the build it is already serving.
import { test, expect, type Page, type Locator } from "@playwright/test";

import { signIn } from "./auth";

// The dashboard paths are gated on the SERVER (web/src/middleware.ts): a request
// without the `jf_session` cookie is redirected to /login BEFORE the page renders.
// This spec visits one, so it declares the session it means — see e2e/auth.ts.
test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});

const TRASHED = {
  id: "p_sold_guard",
  name: "Kopi Ais",
  price_sen: 650,
  active: true,
  deleted_at: "2026-09-25T10:00:00Z",
  // `can_purge: true` so the permanent-delete control is OFFERED; the SERVER still
  // refuses (that is the case under test — the UI's pre-computed hint is not the
  // guard).
  can_purge: true,
  sales_count: 3,
  subscription_count: 0,
};

// Verbatim wire prose from api/internal/httpapi/rest_handlers.go, deliberately NOT
// used by the page: Malay is what the dashboard must never print. The restore one
// is the prose the 404 refusal actually carries ("tidak dijumpai").
const PURGE_PROSE =
  "produk ini mempunyai jualan — sejarah jualan tidak boleh dimusnahkan dengan memadam produk. Ia kekal dalam tong sampah.";
const RESTORE_PROSE = "tidak dijumpai";
const RESTORE_SUB_PROSE =
  "produk ini masih ada langganan — batalkan langganan itu dahulu. Ia kekal dalam tong sampah.";

const EN_HAS_SALES =
  "This product has sales, so it can't be deleted for good — sales history must not be destroyed with the product. It stays in the trash.";
const EN_HAS_SUBS =
  "This product still has subscriptions, so it can't be deleted for good — cancel them first. It stays in the trash.";
const EN_RESTORE_FALLBACK = "We couldn't restore this product. Please try again.";
const EN_PURGE_FALLBACK = "We couldn't delete this product. Please try again.";

type Counts = { counts: () => { trashCalls: number } };

/**
 * stubAPI stubs the whole dashboard so /products reaches its loaded trash view
 * without a Go server.
 *
 * The trash listing answers its row EVERY time it is called — including the
 * refetch a refusal fires. Nothing here hangs: the sentence under test has to
 * survive a reload that actually completes, so a stub that withheld the second
 * listing would hide the very wipe this file pins.
 */
async function stubAPI(page: Page): Promise<Counts> {
  const json = (status: number, body: unknown) => ({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  const ME = { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "en", is_super_admin: false };
  let trashCalls = 0;

  // Catch-all FIRST; Playwright resolves routes in REVERSE registration order, so
  // every specific route below it wins.
  await page.route(/(.*)\/api\/.*$/, (r) => r.fulfill(json(200, {})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) =>
    r.fulfill(
      json(200, {
        me: ME,
        workspaces: [{ id: "a1", name: "Demo", slug: "demo", role: "admin" }],
        chip_status: null, keys: [], domains: [], social: [], einvoice: null,
        webhooks: [], products: [], summary: { counts: {}, revenue_sen: 0 },
        sales: { items: [], page: 1, limit: 20, total: 0 },
        form_analytics: [], product_analytics: [],
      }),
    ),
  );
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(200, ME)));
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json(200, { lang: "en" })));
  await page.route(/(.*)\/api\/rest\/currency\/rates$/, (r) => r.fulfill(json(200, { rates: [], cached: false })));
  // The catalogue is empty on purpose: this guard is about the BIN.
  await page.route(/(.*)\/api\/rest\/products(\?.*)?$/, (r) => r.fulfill(json(200, [])));
  await page.route(/(.*)\/api\/rest\/products\/trash(\?.*)?$/, (r) => {
    trashCalls += 1;
    return r.fulfill(json(200, [TRASHED]));
  });
  // The RESTORE refusal as the shipped handler really answers it: the row is not
  // in the bin any more → respondErr → 404 "tidak dijumpai" → class code
  // request.notFound, which the dictionary maps nowhere on purpose.
  await page.route(/(.*)\/api\/rest\/products\/[^/?]+\/restore(\?.*)?$/, (r) =>
    r.fulfill(json(404, { error: RESTORE_PROSE, code: "request.notFound" })),
  );
  // The PURGE refusal, exactly the shape the handler writes for a sold product:
  // its own prose, plus the machine code the dashboard translates.
  await page.route(/(.*)\/api\/rest\/products\/[^/?]+\/purge(\?.*)?$/, (r) =>
    r.fulfill(json(409, { error: PURGE_PROSE, code: "product.hasSales", sales_count: 3 })),
  );
  return { counts: () => ({ trashCalls }) };
}

/**
 * Opens /products in a FRESH browser state and switches to the Trash view (the
 * catalogue is the default — a new visitor must never land in the bin). The
 * trashed row's name is the loaded signal.
 */
async function openTrash(page: Page): Promise<Counts> {
  await page.addInitScript(() => {
    // Nothing is inherited from another spec: a leftover jf_lang / jf_workspace
    // would make this render a state no new visitor has (the board has been bitten
    // by a leftover jf_lang before).
    try {
      localStorage.clear();
      sessionStorage.clear();
      localStorage.setItem("jf_workspace", "demo");
      localStorage.setItem("jf_lang", "en");
    } catch {
      /* storage unavailable */
    }
  });
  const stub = await stubAPI(page);
  await page.goto("/products", { waitUntil: "networkidle" });

  await page.getByRole("tab", { name: "Trash", exact: true }).click();
  await expect(page.locator("li", { hasText: TRASHED.name }).first()).toBeVisible({ timeout: 45_000 });
  await expect(page.locator("html"), "the reading is the ENGLISH default").toHaveAttribute("lang", "en");
  return stub;
}

/**
 * The refusal line: a `role="alert"` node carrying the sentence under test. A Next
 * page ALSO renders a `role="alert"` node for its route announcer, so the locator
 * is filtered by the text that identifies the message — an unscoped one would be a
 * strict-mode violation and would not say which node it read.
 */
const refusalLine = (page: Page, needle: RegExp) => page.getByRole("alert").filter({ hasText: needle });

/**
 * THE CARD'S ACCEPTANCE (t_602c48f5): still on screen 3 s later. Sampled every
 * 100 ms rather than waited on once — the defect is a set-then-WIPED message, so a
 * single late read cannot tell "never rendered" from "rendered and replaced". A
 * wipe anywhere in the window fails the `all` assertion and names the sample index
 * it went missing at.
 */
async function expectSurvivesWindow(line: Locator, label: string) {
  const samples: boolean[] = [];
  for (let i = 0; i < 30; i += 1) {
    await line.page().waitForTimeout(100);
    samples.push((await line.count()) > 0 && (await line.first().isVisible().catch(() => false)));
  }
  const lastMissing = samples.lastIndexOf(false);
  expect(
    samples.every(Boolean),
    `${label} must survive the whole 3s window. Samples (100ms apart): ` +
      `${samples.map((s) => (s ? "1" : "0")).join("")}. A 0 after a 1 is the wipe this card fixes ` +
      `(last missing at sample ${lastMissing}); all-zero means it never rendered.`,
  ).toBe(true);
}

/**
 * Settle past the reload the refusal branch fires, BEFORE any visibility read.
 *
 * WHY THIS IS LOAD-BEARING (measured while building this guard): on the PRE-FIX
 * bytes the sentence is set and wiped in the same tick, but it is briefly in the
 * DOM — and `toBeVisible` polls, so it can WIN that race. The test then died in
 * the unbounded `innerText()` that followed (the node was already gone) at
 * `Test timeout of 120000ms exceeded` — a red that names no defect. Waiting for the
 * refetch to land first makes the flash insufficient, so a pre-fix tree fails at
 * the assertion that names it (`… must render a message at all`).
 */
async function settlePastTheReload(page: Page) {
  await page.waitForTimeout(1_500);
}

/** A BOUNDED read: an unbounded `innerText()` on a node that vanished eats the
 *  whole test timeout and turns a real red into an unattributable one. */
async function readLine(line: Locator): Promise<string> {
  return (await line.first().innerText({ timeout: 5_000 })).trim();
}

test("a purge refused for sales shows the ENGLISH reason, keeps the list, and is still readable 3s later", async ({ page }) => {
  test.setTimeout(120_000);
  const stub = await openTrash(page);

  const row = page.locator("li", { hasText: TRASHED.name }).first();
  // Scoped to the ROW: once the dialog opens its own confirm button carries the
  // same label.
  await row.getByRole("button", { name: "Delete for good", exact: true }).click();

  const dialog = page.getByRole("dialog");
  await expect(dialog).toBeVisible({ timeout: 20_000 });
  await dialog.getByLabel("Product name").fill(TRASHED.name);
  const listLoadsBefore = stub.counts().trashCalls;
  await dialog.getByRole("button", { name: "Delete for good", exact: true }).click();

  // The refusal lands in the trash view's own line, beside the rows it is about.
  const line = refusalLine(page, /can't be deleted for good/);
  await settlePastTheReload(page);
  await expect(line, "the purge refusal must render a message at all").toBeVisible({ timeout: 20_000 });
  const shown = await readLine(line);
  expect(shown).toContain(EN_HAS_SALES);
  expect(shown).not.toContain(PURGE_PROSE);
  expect(shown).not.toContain("produk ini mempunyai jualan");

  // It is NOT the page's load-failure line: a refusal changes nothing about the
  // listing, so it must not arrive with a Retry that reloads it.
  await expect(
    line.getByRole("button", { name: "Retry" }),
    "a refused action must not be rendered as a LOAD failure (that line reloads the listing)",
  ).toHaveCount(0);

  // The merchant keeps their place: the dialog is dismissed and the row the refusal
  // is about is still there.
  await expect(dialog).toHaveCount(0);
  await expect(row, "the refusal must not take the trash list away").toBeVisible();

  // ...and it is still readable across the whole window.
  await expectSurvivesWindow(line, "the purge refusal");
  const finalText = await readLine(line);
  expect(finalText).toContain(EN_HAS_SALES);
  expect(finalText).not.toContain(PURGE_PROSE);

  // ATTRIBUTION: the refusal DID refetch the trash listing, so the sentence
  // survived a reload that completed rather than one that never happened.
  const counts = stub.counts();
  expect(
    counts.trashCalls,
    `a refused purge must still refetch the trash listing (baseline before the press was ${listLoadsBefore})`,
  ).toBeGreaterThan(listLoadsBefore);

  console.log(
    `t_602c48f5 evidence (purge): alert="${finalText}" | window(100ms x30) present | ` +
      `trashCalls=${counts.trashCalls}(baseline ${listLoadsBefore}) lang=en prose_present=false retry=false`,
  );
});

test("a restore refused with the 404 the handler really answers renders the page's own sentence and survives the refetch", async ({ page }) => {
  test.setTimeout(120_000);
  const stub = await openTrash(page);

  const row = page.locator("li", { hasText: TRASHED.name }).first();
  const listLoadsBefore = stub.counts().trashCalls;
  await row.getByRole("button", { name: "Restore", exact: true }).click();

  const line = refusalLine(page, /couldn't restore this product/);
  await settlePastTheReload(page);
  await expect(line, "the restore refusal must render a message at all").toBeVisible({ timeout: 20_000 });
  const shown = await readLine(line);
  expect(shown).toContain(EN_RESTORE_FALLBACK);
  // The wire prose is never rendered, whatever it says (issue #6177).
  expect(shown).not.toContain(RESTORE_PROSE);
  expect(shown).not.toContain("tidak dijumpai");

  await expect(row, "the row being refused stays on screen with the reason").toBeVisible();

  await expectSurvivesWindow(line, "the restore refusal");
  const finalText = await readLine(line);
  expect(finalText).toContain(EN_RESTORE_FALLBACK);

  const counts = stub.counts();
  expect(
    counts.trashCalls,
    `a refused restore must still refetch the trash listing (baseline before the press was ${listLoadsBefore})`,
  ).toBeGreaterThan(listLoadsBefore);

  console.log(
    `t_602c48f5 evidence (restore 404): alert="${finalText}" | window(100ms x30) present | ` +
      `trashCalls=${counts.trashCalls}(baseline ${listLoadsBefore}) lang=en prose_present=false`,
  );
});

// THE CODE-ROUTING HALF of the restore (the card's second ask). `purgeProduct`
// already translated a refusal from its machine `code`; `restoreProduct` printed a
// bare page sentence and threw the reason away. This leg pins that routing on the
// restore path — see the header for why the shape is not one today's restore
// handler can produce.
test("a restore whose refusal carries a machine code renders the CODE's sentence, not the wire prose", async ({ page }) => {
  test.setTimeout(120_000);
  await openTrash(page);

  // Registered LAST, so it wins over the stub's own restore route.
  await page.route(/(.*)\/api\/rest\/products\/[^/?]+\/restore(\?.*)?$/, (r) =>
    r.fulfill({
      status: 409,
      contentType: "application/json",
      body: JSON.stringify({ error: RESTORE_SUB_PROSE, code: "product.hasSubscriptions" }),
    }),
  );

  const row = page.locator("li", { hasText: TRASHED.name }).first();
  await row.getByRole("button", { name: "Restore", exact: true }).click();

  const line = refusalLine(page, /still has subscriptions/);
  await settlePastTheReload(page);
  await expect(
    line,
    "a coded restore refusal must render the dictionary sentence for its code",
  ).toBeVisible({ timeout: 20_000 });
  const shown = await readLine(line);
  expect(shown).toContain(EN_HAS_SUBS);
  expect(shown).not.toContain(RESTORE_SUB_PROSE);
  expect(shown).not.toContain("produk ini masih ada langganan");

  await expectSurvivesWindow(line, "the coded restore refusal");
  console.log(
    `t_602c48f5 evidence (restore coded): alert="${await readLine(line)}" | ` +
      `window(100ms x30) present | lang=en prose_present=false`,
  );
});

// THE TRANSPORT HALF of the same surface (found while fixing t_602c48f5:
// `purgeProduct` had a `catch` and `restoreProduct` never did, so a rejected fetch
// escaped through the row's `void restoreProduct(p.id)` handler as an unhandled
// rejection and the merchant saw NOTHING at all). A fetch that REJECTS is an ACTION
// failure like any other — it must leave a readable sentence.
for (const action of ["restore", "purge"] as const) {
  test(`a ${action} whose request is aborted mid-flight still leaves a readable sentence`, async ({ page }) => {
    test.setTimeout(120_000);
    await openTrash(page);

    // Registered LAST, so it wins over the stub's own refusal route: the request is
    // aborted mid-flight and the page's `catch` is what has to answer.
    await page.route(new RegExp(`(.*)\\/api\\/rest\\/products\\/[^/?]+\\/${action}(\\?.*)?$`), (r) => r.abort());

    const row = page.locator("li", { hasText: TRASHED.name }).first();
    if (action === "purge") {
      await row.getByRole("button", { name: "Delete for good", exact: true }).click();
      const dialog = page.getByRole("dialog");
      await expect(dialog).toBeVisible({ timeout: 20_000 });
      await dialog.getByLabel("Product name").fill(TRASHED.name);
      await dialog.getByRole("button", { name: "Delete for good", exact: true }).click();
    } else {
      await row.getByRole("button", { name: "Restore", exact: true }).click();
    }

    const line = refusalLine(page, action === "purge" ? /couldn't delete this product/ : /couldn't restore this product/);
    await settlePastTheReload(page);
    await expect(line, `a rejected ${action} must not fail silently`).toBeVisible({ timeout: 20_000 });
    const shown = await readLine(line);
    expect(shown).toContain(action === "purge" ? EN_PURGE_FALLBACK : EN_RESTORE_FALLBACK);
    await expect(row, "the row stays on screen with the sentence").toBeVisible();

    await expectSurvivesWindow(line, `the ${action} transport failure`);
    console.log(`t_602c48f5 evidence (${action} transport): alert="${await readLine(line)}"`);
  });
}
