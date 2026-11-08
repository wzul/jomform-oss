// Regression guard for the two /forms trash refusals that main grew after the
// #6177 base, and that this rebase converted off the server's prose.
//
// THE DEFECT CLASS (issue #6177). The API answers a failure with Malay prose
// (`{"error":"borang ini mempunyai jualan — …"}`) because it never learns the
// caller's language on a JSON call. The dashboard used to render that string
// verbatim:
//
//     setErr(d?.error || t("forms.purgeFail"));
//
// so an ENGLISH dashboard showed a Malay sentence. The fix routes the sentence
// through the machine `code` and the dashboard dictionary. Both of these sites
// are refusals that NAME the obstacle (the form has sales; another form took the
// slug), which is exactly the case where falling back to the page's generic
// "We couldn't delete this form." would hide the reason the merchant needs.
//
// THE SECOND DEFECT, THIS FILE'S REAL SUBJECT SINCE t_ab4aa85b. The refusal was
// written to the page's `err` state and then erased in the SAME TICK by the
// relist the same branch fired — `loadTrash()` ends with `setErr("")` on a
// successful reload — so the sentence was never readable at all. Measured on the
// unfixed build (live DOM, an ordinarily-resolving trash listing, sampled every
// 250ms): `alerts: []` at t~0.25/0.75/1.5/3.0s, and the final body carried
// neither the English sentence nor the Malay one. The merchant watched the click
// do nothing.
//
// That is why this guard USED TO HANG the second trash listing: with a normal
// second response the wipe raced the assertion, so the stub was written to never
// answer it. A stub that hides the defect cannot pin its absence — since the fix
// the second listing RESOLVES ORDINARILY, and the sentence must survive a reload
// that really completes. The `trashCalls` delta below is the attribution leg
// that proves that is what happened.
//
// WHAT THIS GUARD PROVES, on the real built page with a stubbed API:
//   1. the refusal renders the ENGLISH dictionary sentence for its code, and
//   2. the server's Malay prose appears NOWHERE on the page, and
//   3. the sentence is still on screen 3s later, sampled every 100ms — sampled
//      rather than waited on once, because the defect was a set-then-WIPED
//      message and a single late read cannot tell "never rendered" from
//      "rendered and replaced", and
//   4. the trash row the refusal is ABOUT is still on screen beside it, and
//   5. the listing really was refetched after the refusal, so (3) is a claim
//      about a completed reload rather than about a reload that never happened.
//
// The API is stubbed at the browser level (no Go server, no session, no DB), so
// the gate needs nothing but the build it is already serving.
import { test, expect, type Locator, type Page } from "@playwright/test";

import { signIn } from "./auth";

// The dashboard paths are gated on the SERVER (web/src/middleware.ts): a request
// without the `jf_session` cookie is redirected to /login BEFORE the page
// renders. This spec visits one, so it declares the session it means — before
// the gate existed an anonymous visit rendered the account shell, so this spec
// could (and did) rely on the bug. See e2e/auth.ts.
test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});

const TRASHED = {
  id: "form_sold_guard",
  slug: "sold-form",
  title: "A sold form",
  deleted_at: "2026-01-02T03:04:05Z",
  // `can_purge: true` so the purge control is offered; the SERVER still refuses
  // (that is the case under test — the UI's pre-computed hint is not the guard).
  can_purge: true,
  sales_count: 3,
};

// Verbatim wire prose from api/internal/httpapi/rest_handlers.go and
// rest_workspaces are deliberately NOT used here; these are the two bodies the
// page's code must translate. Malay is what the dashboard must never print.
const PURGE_PROSE =
  "this form has sales — sales history cannot be destroyed by deleting the form. It stays in the bin.";
const RESTORE_PROSE =
  "cannot be restored: this form's slug has been taken by another form — change that form's slug first, then try restoring again";

const EN_HAS_SALES =
  "This form has sales, so it can't be deleted for good — sales history must not be destroyed with the form. It stays in the trash.";
const EN_SLUG_TAKEN =
  "This form can't be restored: another form has taken its slug. Change that form's slug first, then restore this one.";
// The page's OWN sentence, which both trash actions fall back to when the
// failure carries no code this build knows — a transport failure aborted
// mid-flight (the `restore`/`purge` catch), or a 400 carrying only the class
// code. Read from web/src/components/i18nDict.ts and quoted here so a reword on
// one side alone fails this spec instead of drifting quietly.
const EN_RESTORE_FALLBACK = "We couldn't restore this form. Please try again.";
const EN_PURGE_FALLBACK = "We couldn't delete this form. Please try again.";

type Counts = { counts: () => { trashCalls: number } };

type StubOpts = {
  // Card t_d9dda884: answer the FIRST trash listing and fail every later one,
  // which is the merchant's "the refusal fired, then the refetch it triggered
  // 500ed / they went offline" case.
  failRelistAfterFirst?: boolean;
};

/**
 * stubAPI stubs the whole dashboard so /forms reaches its loaded trash view
 * without a Go server. The two refusal routes answer the 409 envelopes under
 * test; everything else is a benign empty payload.
 *
 * The trash listing answers a row EVERY time it is called — including the
 * refetch the refusal branch fires. Nothing here hangs: the sentence under test
 * has to survive a reload that actually completes (see the header).
 */
async function stubAPI(page: Page, opts: StubOpts = {}): Promise<Counts> {
  const json = (status: number, body: unknown) => ({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  const ME = { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "en", is_super_admin: false };
  let trashCalls = 0;

  // Catch-all FIRST; Playwright resolves routes in REVERSE registration order,
  // so every specific route below it wins.
  await page.route(/(.*)\/api\/.*$/, (r) => r.fulfill(json(200, {})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) =>
    r.fulfill(
      json(200, {
        me: ME,
        workspaces: [{ id: "ws1", name: "Demo", slug: "demo", role: "admin" }],
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
  await page.route(/(.*)\/api\/rest\/products(\?.*)?$/, (r) => r.fulfill(json(200, [])));
  await page.route(/(.*)\/api\/rest\/forms(\?.*)?$/, (r) => r.fulfill(json(200, [])));
  await page.route(/(.*)\/api\/rest\/forms\/trash(\?.*)?$/, (r) => {
    // EVERY listing answers the row, the first and the refetch alike. The old
    // stub hung the second one to keep the sentence on screen, which is what a
    // guard must NOT do: the wipe it hid is the defect this file now pins.
    trashCalls += 1;
    // t_d9dda884: the relist-FAILS leg. The listing the refusal itself triggers
    // is the one that fails — the page must still say WHY the action was refused.
    if (opts.failRelistAfterFirst && trashCalls > 1) {
      return r.fulfill(json(500, { error: "the trash listing failed", code: "internal.error" }));
    }
    return r.fulfill(json(200, [TRASHED]));
  });
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+\/restore(\?.*)?$/, (r) =>
    r.fulfill(json(409, { error: RESTORE_PROSE, code: "form.slugTaken" })),
  );
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+\/purge(\?.*)?$/, (r) =>
    r.fulfill(json(409, { error: PURGE_PROSE, code: "form.hasSales", sales_count: 3 })),
  );
  return { counts: () => ({ trashCalls }) };
}

/**
 * Opens /forms in a FRESH browser state and switches to the Trash view (the
 * live list is the default). Its tab renders the trashed row's slug, so waiting
 * for that is the loaded signal.
 */
async function openTrash(page: Page, opts: StubOpts = {}): Promise<Counts> {
  await page.addInitScript(() => {
    // Nothing is inherited from another spec: a leftover jf_lang / jf_workspace
    // would make this render a state no new visitor has (the board has been
    // bitten by a leftover jf_lang before).
    try {
      localStorage.clear();
      sessionStorage.clear();
      localStorage.setItem("jf_workspace", "demo");
      localStorage.setItem("jf_lang", "en");
    } catch {
      /* storage unavailable */
    }
  });
  const stub = await stubAPI(page, opts);
  await page.goto("/forms", { waitUntil: "networkidle" });

  await page.getByRole("tab", { name: /Trash/i }).click();
  await expect(page.locator("li", { hasText: TRASHED.slug }).first()).toBeVisible({ timeout: 45_000 });
  return stub;
}

/**
 * The refusal line: a `role="alert"` node carrying the sentence under test. A
 * Next page ALSO renders a `role="alert"` node for its route announcer, so the
 * locator is filtered by the text that identifies the message — an unscoped one
 * would be a strict-mode violation and would not say which node it read.
 */
const refusalLine = (page: Page, needle: RegExp) =>
  page.getByRole("alert").filter({ hasText: needle });

/**
 * THE CARD'S ACCEPTANCE (t_ab4aa85b): still on screen 3s later. Sampled every
 * 100ms rather than waited on once — the defect is a set-then-WIPED message, so
 * a single late read cannot tell "never rendered" from "rendered and replaced".
 * A wipe anywhere in the window fails the `all` assertion and names the sample
 * index it went missing at.
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
 * A LOCATOR READ MUST NOT BE ABLE TO HANG (t_155bdc39, MEASURED — not a
 * tidy-up). Every leg below reaches its read after a `toBeVisible` that can WIN
 * a race against a refusal that is a FLASH: on a pre-fix shape the sentence is
 * set and then erased by the relist the same branch fires, within a few ms. The
 * read that followed was an UNBOUNDED `innerText()`, so when the race was won it
 * waited the WHOLE test timeout for a node that never comes back and the test
 * died with
 *
 *     Error: locator.innerText: Test timeout of 120000ms exceeded.
 *
 * — an Error line that names NO assertion. The bite harness reads the
 * reporter's Error block for a named red
 * (`web/scripts/biteproof_forms_trash_refusal.sh`, `run_case`), so such a death
 * is reported as `DETECTED, but NOT at a named assertion` and the run ends
 * `SOME CASES FAILED` on a tree whose fix is correct.
 *
 * So: settle past the wipe, re-assert the SAME label the case already accepts
 * (which owns the wipe under a named red), then read with a BOUNDED timeout that
 * degrades to the empty string rather than hanging.
 */
async function settleAndReadRefusal(line: Locator, label: string): Promise<string> {
  await line.page().waitForTimeout(1_000);
  await expect(line, label).toBeVisible({ timeout: 5_000 });
  return boundedAlertText(line);
}

/**
 * The same bounded read with no settle — for a node a passing assertion (or the
 * sampling window above) has ALREADY established is on screen. Still bounded,
 * for the same reason: see `settleAndReadRefusal`.
 */
async function boundedAlertText(line: Locator): Promise<string> {
  return ((await line.first().textContent({ timeout: 5_000 }).catch(() => null)) ?? "").trim();
}

test("a purge refused for sales shows the ENGLISH reason, keeps the list, and is still readable 3s later", async ({ page }) => {
  test.setTimeout(120_000);
  const stub = await openTrash(page);

  // Open the purge dialog from the ROW (scoped: the dialog's own confirm button
  // carries the same label once it opens).
  const row = page.locator("li", { hasText: TRASHED.slug }).first();
  await row.getByRole("button", { name: "Delete for good", exact: true }).click();

  const dialog = page.getByRole("dialog");
  await expect(dialog).toBeVisible({ timeout: 20_000 });
  await dialog.locator("#purge-confirm-slug").fill(TRASHED.slug);
  const listLoadsBefore = stub.counts().trashCalls;
  await dialog.getByRole("button", { name: "Delete for good", exact: true }).click();

  // The refusal lands in the trash view's own alert, beside the reason.
  const line = refusalLine(page, /can't be deleted for good/);
  await expect(line, "the purge refusal must render a message at all").toBeVisible({ timeout: 20_000 });
  const shown = await settleAndReadRefusal(line, "the purge refusal must render a message at all");
  expect(shown).toContain(EN_HAS_SALES);
  expect(shown).not.toContain(PURGE_PROSE);
  expect(shown).not.toContain("borang ini mempunyai jualan");
  await expect(page.locator("html"), "the reading is the ENGLISH default").toHaveAttribute("lang", "en");

  // The merchant keeps their place: the row the refusal is about is still there.
  await expect(row, "the refusal must not take the trash list away").toBeVisible();

  // ...and it is still readable across the whole window.
  await expectSurvivesWindow(line, "the purge refusal");
  const finalText = await boundedAlertText(line);
  expect(finalText).toContain(EN_HAS_SALES);
  expect(finalText).not.toContain(PURGE_PROSE);

  // ATTRIBUTION: the refusal DID refetch the trash listing, so the sentence
  // survived a reload that completed rather than one that never happened. (If a
  // future change stops refetching on a refusal, this is the leg to drop — the
  // window assertion above is the requirement.)
  const counts = stub.counts();
  expect(
    counts.trashCalls,
    `a refused purge must still refetch the trash listing (baseline before the press was ${listLoadsBefore})`,
  ).toBeGreaterThan(listLoadsBefore);

  console.log(
    `t_ab4aa85b evidence (purge): alert="${finalText}" | window(100ms x30) present | ` +
      `trashCalls=${counts.trashCalls}(baseline ${listLoadsBefore}) lang=en prose_present=false`,
  );
});

test("a restore refused over a re-claimed slug shows the ENGLISH reason and is still readable 3s later", async ({ page }) => {
  test.setTimeout(120_000);
  await openTrash(page);

  const row = page.locator("li", { hasText: TRASHED.slug }).first();
  await row.getByRole("button", { name: "Restore", exact: true }).click();

  const line = refusalLine(page, /can't be restored/);
  await expect(line, "the restore refusal must render a message at all").toBeVisible({ timeout: 20_000 });
  const shown = await settleAndReadRefusal(line, "the restore refusal must render a message at all");
  expect(shown).toContain(EN_SLUG_TAKEN);
  expect(shown).not.toContain(RESTORE_PROSE);
  expect(shown).not.toContain("tidak boleh dipulihkan");
  await expect(row, "the row being refused stays on screen with the reason").toBeVisible();

  await expectSurvivesWindow(line, "the restore refusal");
  const finalText = await boundedAlertText(line);
  expect(finalText).toContain(EN_SLUG_TAKEN);
  expect(finalText).not.toContain(RESTORE_PROSE);
  console.log(`t_ab4aa85b evidence (restore): alert="${finalText}" | window(100ms x30) present`);
});

// THE TRANSPORT HALF of the same surface (found while fixing t_ab4aa85b:
// `purge()` had a `catch` and `restore()` never did). A fetch that REJECTS is an
// ACTION failure like any other — it must leave a readable sentence, not an
// unhandled rejection that shows the merchant nothing at all.
for (const action of ["restore", "purge"] as const) {
  test(`a ${action} whose request is aborted mid-flight still leaves a readable sentence`, async ({ page }) => {
    test.setTimeout(120_000);
    await openTrash(page);

    // Registered LAST, so it wins over the stub's own refusal route: the request
    // is aborted mid-flight and the page's `catch` is what has to answer.
    await page.route(new RegExp(`(.*)\\/api\\/rest\\/forms\\/[^/]+\\/${action}(\\?.*)?$`), (r) => r.abort());

    const row = page.locator("li", { hasText: TRASHED.slug }).first();
    if (action === "purge") {
      await row.getByRole("button", { name: "Delete for good", exact: true }).click();
      const dialog = page.getByRole("dialog");
      await expect(dialog).toBeVisible({ timeout: 20_000 });
      await dialog.locator("#purge-confirm-slug").fill(TRASHED.slug);
      await dialog.getByRole("button", { name: "Delete for good", exact: true }).click();
    } else {
      await row.getByRole("button", { name: "Restore", exact: true }).click();
    }

    const line = refusalLine(page, action === "purge" ? /couldn't delete this form/ : /couldn't restore this form/);
    await expect(line, `a rejected ${action} must not fail silently`).toBeVisible({ timeout: 20_000 });
    const shown = await settleAndReadRefusal(line, `a rejected ${action} must not fail silently`);
    expect(shown).toContain(action === "purge" ? EN_PURGE_FALLBACK : EN_RESTORE_FALLBACK);
    await expect(row, "the row stays on screen with the sentence").toBeVisible();

    await expectSurvivesWindow(line, `the ${action} transport failure`);
    console.log(`t_ab4aa85b evidence (${action} transport): alert="${await boundedAlertText(line)}"`);
  });
}

// THE FAILED-RELIST LEG (card t_d9dda884) — the path t_ab4aa85b's own fix left
// open. Moving the refusal into a state the loader never writes stops a relist
// that COMPLETES from wiping the sentence; a relist that FAILS was still fatal,
// because `loadTrash()`'s failure branch reported into `err` — this page's
// LOAD-failure chrome, which returns early and replaces the whole page, refusal,
// rows and tab included. Measured on the t_ab4aa85b bytes (probe: first listing
// answers the row, every later one 500s, a Restore refused with 409
// form.slugTaken; 30 samples 100ms apart over 3s): the refusal sentence was
// readable at NO sample, and every reading was "We couldn't load the trash.
// Please try again. Retry" — the merchant lost WHY their action was refused.
//
// Reachability is a DOUBLE failure (a refusal, then a transient 5xx / offline),
// hence a follow-up rather than a release blocker. The rule it violates is still
// the product's: a refusal stays readable until the user's next action — the
// refetch that reports it may not erase it, and "the refetch" is not conditional
// on the refetch succeeding.
test("a restore refusal stays readable when the relist it fires FAILS", async ({ page }) => {
  test.setTimeout(120_000);
  const stub = await openTrash(page, { failRelistAfterFirst: true });

  const row = page.locator("li", { hasText: TRASHED.slug }).first();
  const listingsBefore = stub.counts().trashCalls;
  await row.getByRole("button", { name: "Restore", exact: true }).click();

  const line = refusalLine(page, /can't be restored/);
  await expect(line, "the refusal must stay readable when its own relist fails").toBeVisible({ timeout: 20_000 });
  // Bounded read, never a bare locator action: the rework card t_155bdc39 is
  // about exactly that hazard (a read against a node that is already gone waits
  // the whole test timeout and names no assertion), and a new leg must not
  // reintroduce it.
  const shown = ((await line.first().textContent({ timeout: 5_000 }).catch(() => null)) || "").trim();
  expect(shown, "the refusal sentence must be readable").toContain(EN_SLUG_TAKEN);
  expect(shown).not.toContain(RESTORE_PROSE);

  // The failed listing is REPORTED — beside the refusal, not instead of it.
  await expect(
    page.getByRole("alert").filter({ hasText: /load the trash/i }),
    "the failed relist must be reported beside the refusal",
  ).toBeVisible({ timeout: 20_000 });

  // ...and the trash view is still the page the merchant is on: the tab and the
  // very row the refusal is about are on screen. The pre-fix shape replaced all
  // of this with the load-failure card.
  await expect(page.getByRole("tab", { name: /Trash/i }), "the failed relist must not replace the page").toBeVisible();
  await expect(row, "the rows must not be blanked by a failed relist").toBeVisible();

  await expectSurvivesWindow(line, "the restore refusal across a FAILED relist");

  // ATTRIBUTION: the relist really ran (so the sentence survived a refetch that
  // was attempted), and the alert above shows it really failed.
  const counts = stub.counts();
  expect(
    counts.trashCalls,
    `a refused restore must still fire the relist (baseline before the press was ${listingsBefore})`,
  ).toBeGreaterThan(listingsBefore);
  console.log(
    `t_d9dda884 evidence (failed relist): alert="${shown}" | window(100ms x30) present | ` +
      `trashCalls=${counts.trashCalls}(baseline ${listingsBefore}) listing_failure_shown=true`,
  );
});
