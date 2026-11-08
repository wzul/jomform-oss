// Regression guard for the /media TRASH refusals (card t_66270a9c) — the FOURTH
// surface of the defect t_ab4aa85b fixed on /forms' trash view and in the Settings
// Danger zone, after the /products bin (t_602c48f5).
//
// THE DEFECT CLASS. A refusal wrote its sentence into the page's LOAD-error state
// (`error`) and then fired the relist that clears that state on success:
//
//     restore   setError(t("media.restoreObjectGone"));  void loadTrash();
//     purge     setError(t("media.purgeFail"));          void loadTrash();
//     loadTrash                                          -> setError("")
//
// The sentence was therefore SET and ERASED in the same tick and was readable at
// NO point. MEASURED on this page's pre-fix bytes, built app, real browser
// (chromium via Playwright), English, fresh storage, with a stubbed 409
// `{code: "upload.objectGone"}` on the restore route: `role="alert"` sampled every
// 100 ms thirty times was all-zero, while the trash listing went from ONE call to
// TWO — so the string was right and the same-tick relist was the mechanism.
//
// THE FIX this file pins: the refusal lives in a state NO loader writes
// (`mediaErr`), is cleared at the START of the next user action instead of by the
// refetch that reports it, and renders beside the trash rows it is about — with no
// Retry, because a refusal changes nothing about the listing and reloading it is
// not the remedy (the `error` line above the tabs is the LOAD-failure channel and
// carries that Retry).
//
// WHAT THIS GUARD PROVES, on the real built page with a stubbed API:
//   1. the refusal renders at all, in the merchant's own language;
//   2. it SURVIVES the refetch the refusal branch fires — sampled every 100 ms for
//      3 s, so a set-then-wiped message cannot pass as a "rendered late" one;
//   3. the refetch really happened (the listing call count moves), so the sentence
//      is proven to survive a reload that COMPLETED rather than one that never ran;
//   4. the wire's Malay prose appears NOWHERE on the page (issue #6177);
//   5. the trash list the merchant was reading is still on screen;
//   6. a request that REJECTS mid-flight leaves a readable sentence instead of the
//      unhandled rejection the restore handler used to escape as;
//   7. the refusal is cleared by the user's NEXT action (the Active/Trash switch),
//      so this is not "a message that never goes away".
//
// ON THE CODES, HONESTLY. The card's premise (t_66270a9c) was that the restore
// refusal carries the machine code `upload.objectGone`. It does NOT: that string
// exists nowhere in `api/internal/httpapi/errcode.go` — it was invented by the
// throwaway probe that measured this defect. What the shipped handler really
// answers for the bytes-gone refusal is
//
//     409 {"error":"fail media ini sudah tiada dalam storan — ia tidak boleh
//          dipulihkan","code":"request.conflict"}
//
// i.e. the HTTP-CLASS code, which `web/src/lib/apiErr.ts` deliberately does not
// translate — so the page's own 409 sentence is the correct answer and that leg
// pins the prose-never-renders half. The CODE-ROUTING leg therefore stubs a code
// this route REALLY can answer today: `requireWriteRole` refuses with 403
// `role.manageRequired`. That makes the routing rule (`code` on the wire is
// translated instead of thrown away) a claim about a reachable server answer, not
// about a code the API does not declare.
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
  id: "m_trash_guard",
  key: "u/a1/dokumen-guard.pdf",
  url: "https://cdn.example/dokumen-guard.pdf",
  mime: "application/pdf",
  size: 2048,
  deleted_at: "2026-09-25T10:00:00Z",
  // `object_present: true` so the Restore control is ENABLED — the refusal has to
  // be reachable for this guard to say anything about it. (A row whose bytes are
  // already known gone disables the button and prints the hint instead.)
  object_present: true,
};

// Verbatim wire prose from api/internal/httpapi/media.go, deliberately NOT used by
// the page: Malay is what the dashboard must never print.
const RESTORE_GONE_PROSE =
  "this media file is no longer in storage — it cannot be restored";
const PURGE_NOT_FOUND_PROSE = "media not found";
const ROLE_PROSE = "you need the manage or admin role";

const EN_OBJECT_GONE = "The file itself is no longer in storage, so this item cannot be restored.";
const EN_RESTORE_FALLBACK = "We couldn't restore this file. Please try again.";
const EN_PURGE_FALLBACK = "We couldn't delete this file. Please try again.";
const EN_MANAGE_REQUIRED = "You need the manage or admin role in this workspace for that.";

type Counts = { counts: () => { trashCalls: number } };

/**
 * stubAPI stubs the whole dashboard so /media reaches its loaded trash view without
 * a Go server.
 *
 * The trash listing answers its row EVERY time it is called — including the refetch
 * a refusal fires. Nothing here hangs: the sentence under test has to survive a
 * reload that actually completes, so a stub that withheld the second listing would
 * hide the very wipe this file pins.
 */
async function stubAPI(page: Page, trashed: typeof TRASHED = TRASHED): Promise<Counts> {
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
  await page.route(/(.*)\/api\/rest\/forms(\?.*)?$/, (r) => r.fulfill(json(200, [])));
  await page.route(/(.*)\/api\/rest\/media\/trash(\?.*)?$/, (r) => {
    trashCalls += 1;
    return r.fulfill(json(200, [trashed]));
  });
  // The live library is empty on purpose: this guard is about the BIN.
  await page.route(/(.*)\/api\/rest\/media(\?.*)?$/, (r) =>
    r.fulfill(json(200, { items: [], total: 0, usage: { used: 2048, limit: 1024 * 1024 * 1024 } })),
  );
  // The RESTORE refusal as the shipped handler really answers it when the bytes are
  // gone: 409 with its own prose and the HTTP-CLASS code request.conflict, which the
  // dictionary maps nowhere on purpose.
  await page.route(/(.*)\/api\/rest\/media\/[^/?]+\/restore(\?.*)?$/, (r) =>
    r.fulfill(json(409, { error: RESTORE_GONE_PROSE, code: "request.conflict" })),
  );
  // The PURGE refusal as the shipped handler really answers it for a row that is not
  // in the bin any more: 404 with its own prose and the class code request.notFound.
  await page.route(/(.*)\/api\/rest\/media\/[^/?]+\/purge(\?.*)?$/, (r) =>
    r.fulfill(json(404, { error: PURGE_NOT_FOUND_PROSE, code: "request.notFound" })),
  );
  return { counts: () => ({ trashCalls }) };
}

/**
 * Opens /media in a FRESH browser state and switches to the Trash view (the live
 * library is the default — a new visitor must never land in the bin). The trashed
 * row's file name is the loaded signal.
 */
async function openTrash(page: Page, trashed: typeof TRASHED = TRASHED): Promise<Counts> {
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
  const stub = await stubAPI(page, trashed);
  await page.goto("/media", { waitUntil: "networkidle" });

  await page.getByRole("tab", { name: "Trash", exact: true }).click();
  // The row's file name is derived from the object key by nameFromKey().
  await expect(page.locator("li", { hasText: "dokumen-guard.pdf" }).first()).toBeVisible({ timeout: 45_000 });
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
 * THE CARD'S ACCEPTANCE (t_66270a9c): still on screen 3 s later. Sampled every
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
 * WHY THIS IS LOAD-BEARING (measured while building the /products guard this file
 * is modelled on): on the PRE-FIX bytes the sentence is set and wiped in the same
 * tick, but it is briefly in the DOM — and `toBeVisible` polls, so it can WIN that
 * race. The test then died in the unbounded `innerText()` that followed at
 * `Test timeout of 120000ms exceeded` — a red that names no defect. Waiting for the
 * refetch to land first makes the flash insufficient, so a pre-fix tree fails at the
 * assertion that names it (`… must render a message at all`).
 */
async function settlePastTheReload(page: Page) {
  await page.waitForTimeout(1_500);
}

/** A BOUNDED read: an unbounded `innerText()` on a node that vanished eats the
 *  whole test timeout and turns a real red into an unattributable one. */
async function readLine(line: Locator): Promise<string> {
  return (await line.first().innerText({ timeout: 5_000 })).trim();
}

test("a purge refused for a row no longer in the bin shows the ENGLISH reason, keeps the list, and is still readable 3s later", async ({ page }) => {
  test.setTimeout(120_000);
  const stub = await openTrash(page);

  const row = page.locator("li", { hasText: "dokumen-guard.pdf" }).first();
  // Scoped to the ROW: once the dialog opens its own confirm button carries the
  // same label.
  await row.getByRole("button", { name: "Delete for good", exact: true }).click();

  const dialog = page.getByRole("dialog");
  await expect(dialog).toBeVisible({ timeout: 20_000 });
  const listLoadsBefore = stub.counts().trashCalls;
  await dialog.getByRole("button", { name: "Delete for good", exact: true }).click();

  // The refusal lands in the trash view's own line, beside the rows it is about.
  const line = refusalLine(page, /couldn't delete this file/);
  await settlePastTheReload(page);
  await expect(line, "the purge refusal must render a message at all").toBeVisible({ timeout: 20_000 });
  const shown = await readLine(line);
  expect(shown).toContain(EN_PURGE_FALLBACK);
  expect(shown).not.toContain(PURGE_NOT_FOUND_PROSE);
  expect(shown).not.toContain("media not found");

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
  expect(finalText).toContain(EN_PURGE_FALLBACK);
  expect(finalText).not.toContain(PURGE_NOT_FOUND_PROSE);

  // ATTRIBUTION: the refusal DID refetch the trash listing, so the sentence
  // survived a reload that completed rather than one that never happened.
  const counts = stub.counts();
  expect(
    counts.trashCalls,
    `a refused purge must still refetch the trash listing (baseline before the press was ${listLoadsBefore})`,
  ).toBeGreaterThan(listLoadsBefore);

  console.log(
    `t_66270a9c evidence (purge): alert="${finalText}" | window(100ms x30) present | ` +
      `trashCalls=${counts.trashCalls}(baseline ${listLoadsBefore}) lang=en prose_present=false retry=false`,
  );
});

test("a restore refused because the BYTES ARE GONE renders the page's specific 409 sentence and survives the refetch", async ({ page }) => {
  test.setTimeout(120_000);
  const stub = await openTrash(page);

  const row = page.locator("li", { hasText: "dokumen-guard.pdf" }).first();
  const listLoadsBefore = stub.counts().trashCalls;
  await row.getByRole("button", { name: "Restore", exact: true }).click();

  const line = refusalLine(page, /no longer in storage/);
  await settlePastTheReload(page);
  await expect(line, "the restore refusal must render a message at all").toBeVisible({ timeout: 20_000 });
  const shown = await readLine(line);
  // The 409 keeps its OWN sentence rather than the generic "please try again": the
  // remedy is different (retrying can never work), and that distinction is the
  // reason this branch exists at all.
  expect(shown).toContain(EN_OBJECT_GONE);
  expect(shown).not.toContain(EN_RESTORE_FALLBACK);
  // The wire prose is never rendered, whatever it says (issue #6177).
  expect(shown).not.toContain(RESTORE_GONE_PROSE);
  expect(shown).not.toContain("tiada dalam storan");

  await expect(row, "the row being refused stays on screen with the reason").toBeVisible();

  await expectSurvivesWindow(line, "the restore refusal");
  const finalText = await readLine(line);
  expect(finalText).toContain(EN_OBJECT_GONE);

  const counts = stub.counts();
  expect(
    counts.trashCalls,
    `a refused restore must still refetch the trash listing (baseline before the press was ${listLoadsBefore})`,
  ).toBeGreaterThan(listLoadsBefore);

  console.log(
    `t_66270a9c evidence (restore 409): alert="${finalText}" | window(100ms x30) present | ` +
      `trashCalls=${counts.trashCalls}(baseline ${listLoadsBefore}) lang=en prose_present=false`,
  );
});

// THE CODE-ROUTING HALF of the restore (the card's second ask). The pre-fix handler
// printed a bare page sentence and threw the machine `code` away. This leg pins the
// routing on a refusal this route REALLY answers: `requireWriteRole` → 403
// `role.manageRequired`. See the header for why `upload.objectGone` is NOT stubbed
// anywhere in this file.
test("a restore refusal carrying a machine code renders the CODE's sentence, not the wire prose", async ({ page }) => {
  test.setTimeout(120_000);
  await openTrash(page);

  // Registered LAST, so it wins over the stub's own restore route.
  await page.route(/(.*)\/api\/rest\/media\/[^/?]+\/restore(\?.*)?$/, (r) =>
    r.fulfill({
      status: 403,
      contentType: "application/json",
      body: JSON.stringify({ error: ROLE_PROSE, code: "role.manageRequired" }),
    }),
  );

  const row = page.locator("li", { hasText: "dokumen-guard.pdf" }).first();
  await row.getByRole("button", { name: "Restore", exact: true }).click();

  const line = refusalLine(page, /manage or admin role/);
  await settlePastTheReload(page);
  await expect(
    line,
    "a coded restore refusal must render the dictionary sentence for its code",
  ).toBeVisible({ timeout: 20_000 });
  const shown = await readLine(line);
  expect(shown).toContain(EN_MANAGE_REQUIRED);
  expect(shown).not.toContain(ROLE_PROSE);
  expect(shown).not.toContain("manage or admin role is required");

  await expectSurvivesWindow(line, "the coded restore refusal");
  console.log(
    `t_66270a9c evidence (restore coded): alert="${await readLine(line)}" | ` +
      `window(100ms x30) present | lang=en prose_present=false`,
  );
});

// THE TRANSPORT HALF of the same surface (found while measuring this card:
// `purge()` had a `catch` and `restore()` never did, so a rejected fetch escaped
// through the row's `void restore(m.id)` handler as an unhandled rejection and the
// merchant saw NOTHING at all). A fetch that REJECTS is an ACTION failure like any
// other — it must leave a readable sentence.
for (const action of ["restore", "purge"] as const) {
  test(`a ${action} whose request is aborted mid-flight still leaves a readable sentence`, async ({ page }) => {
    test.setTimeout(120_000);
    await openTrash(page);

    // Registered LAST, so it wins over the stub's own refusal route: the request is
    // aborted mid-flight and the page's `catch` is what has to answer.
    await page.route(new RegExp(`(.*)\\/api\\/rest\\/media\\/[^/?]+\\/${action}(\\?.*)?$`), (r) => r.abort());

    const row = page.locator("li", { hasText: "dokumen-guard.pdf" }).first();
    if (action === "purge") {
      await row.getByRole("button", { name: "Delete for good", exact: true }).click();
      const dialog = page.getByRole("dialog");
      await expect(dialog).toBeVisible({ timeout: 20_000 });
      await dialog.getByRole("button", { name: "Delete for good", exact: true }).click();
      // The dialog must not stay up in front of the sentence it caused.
      await expect(dialog).toHaveCount(0);
    } else {
      await row.getByRole("button", { name: "Restore", exact: true }).click();
    }

    const line = refusalLine(page, action === "purge" ? /couldn't delete this file/ : /couldn't restore this file/);
    await settlePastTheReload(page);
    await expect(line, `a rejected ${action} must not fail silently`).toBeVisible({ timeout: 20_000 });
    const shown = await readLine(line);
    expect(shown).toContain(action === "purge" ? EN_PURGE_FALLBACK : EN_RESTORE_FALLBACK);
    await expect(row, "the row stays on screen with the sentence").toBeVisible();

    await expectSurvivesWindow(line, `the ${action} transport failure`);
    console.log(`t_66270a9c evidence (${action} transport): alert="${await readLine(line)}"`);
  });
}

// THE OTHER DIRECTION: a refusal that could never be cleared would be as wrong as
// one that is wiped instantly — the merchant would be told their last action failed
// every time they came back to the bin. The switch to the live library is the
// user's NEXT action, so the sentence about the bin goes with it.
test("the refusal is cleared by the user's next action (the Active/Trash switch)", async ({ page }) => {
  test.setTimeout(120_000);
  await openTrash(page);

  const row = page.locator("li", { hasText: "dokumen-guard.pdf" }).first();
  await row.getByRole("button", { name: "Restore", exact: true }).click();

  const line = refusalLine(page, /no longer in storage/);
  await settlePastTheReload(page);
  await expect(line, "the refusal must render before this leg can say anything about clearing it").toBeVisible({
    timeout: 20_000,
  });

  // Away from the bin: the trash panel unmounts and the sentence is cleared at the
  // START of that action.
  await page.getByRole("tab", { name: "Active", exact: true }).click();
  await expect(page.getByRole("tabpanel")).toHaveAttribute("aria-labelledby", /media-tab-active/);

  // ...and back. The trash is refetched, the row is there, and nothing claims the
  // previous action failed.
  await page.getByRole("tab", { name: /^Trash/ }).click();
  await expect(row).toBeVisible({ timeout: 20_000 });
  await expect(
    page.getByRole("alert").filter({ hasText: /no longer in storage|couldn't restore this file/ }),
    "a refusal must not outlive the action it reported",
  ).toHaveCount(0);

  console.log("t_66270a9c evidence (cleared by next action): switch to Active -> back to Trash, refusal gone");
});
