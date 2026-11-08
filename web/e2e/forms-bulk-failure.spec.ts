// Regression guard for t_c9e37f49 — a FAILED /forms BULK publish used to have
// its message wiped ~200ms after it was set, so the merchant read nothing at all.
//
// THE DEFECT, reproduced in a real browser before this fix. `bulk()` set the
// failure sentence inside its `try`, then `return`ed — and the `finally` ran
// anyway:
//
//   try {
//     ...
//     setErr(publishErrorText(outcome, t("forms.loadFail"), t));
//     return;                     // <- returns from INSIDE the try
//   } finally {
//     setBusy(false);
//     load();                     // <- runs even on that return
//   }
//
// and `load()` ends with `setErr("")` on a successful reload, so the freshly
// loaded list REPLACED the refusal. Measured (same driver, live prod v1.19.1 and
// the v1.19.0 control, a real 400 on POST /api/rest/forms/bulk, sampling the
// error surface every 100ms for 8s):
//
//   PROD   bulk n=1  100ms:"We couldn't load your forms. Please try again." -> 300ms:""  (gone)
//   PREFIX bulk n=1  100ms:"" -> 200ms:"invalid json" -> 400ms:""                        (gone)
//
// Pre-existing (identical on the v1.19.0 tree) and NOT a regression of PR 844 —
// which is why it is a follow-up card, not a rework. The release did fix the
// TEXT on this site: pre-fix the flash carried the server's prose, post-fix it
// carries the page's own localized sentence. Both halves are pinned below.
//
// WHY A BROWSER SPEC AND NOT A SOURCE GUARD. The wipe is a RACE between two
// effects (`setErr(sentence)` then a reload that clears it), not a wrong string
// at a call site: the failing source reads correctly in isolation, which is
// exactly why #6177's two source rules and the trash-refusal spec never saw it.
// The pre-fix shape is re-driven by web/scripts/biteproof_forms_bulk_failure.sh —
// this spec is the guard, that harness is the proof it bites.
//
// THE REQUIREMENT PINNED HERE (the card's acceptance): after a refused bulk
// publish the merchant-facing sentence is STILL ON SCREEN 3s later, and it is
// the page's own English sentence — never the API's Malay prose.
//
// The API is stubbed at the browser level (a real HTTP 400 is fulfilled, the
// same pattern e2e/forms-trash-refusal.spec.ts and e2e/builder-publish-retry.spec.ts
// use), so the gate needs nothing but the build it is already serving.
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

const DRAFT = {
  id: "form_bulk_fail_guard",
  slug: "bulk-fail",
  title: "A draft form",
  status: "draft",
  url: "",
  mode: "payment",
};

// The API's wire prose for this failure. `decodeJSON` answering a bad body is
// what the card's driver hit on the prefix build ("invalid json"); the sentence
// here is the same shape — Malay, and something the dashboard must never print
// (issue #6177). It rides `error`, the field the dashboard stopped reading.
const SERVER_PROSE = "invalid request body";

// The page's own sentence (`forms.loadFail`), which publishErrorText falls back
// to when the machine code is the generic `request.badRequest`. Read from
// web/src/components/i18nDict.ts, quoted here so a reword on one side alone
// fails this spec instead of drifting quietly:
//   "forms.loadFail": { en: "We couldn't load your forms. Please try again.", … }
const EN_FALLBACK = "We couldn't load your forms. Please try again.";

/**
 * stubAPI stubs the whole dashboard so /forms reaches its loaded list, and
 * answers POST /api/rest/forms/bulk with a real 400.
 *
 * The body carries the generic class code (`request.badRequest`), which
 * lib/apiErr.ts deliberately does NOT map — so the page must answer its OWN
 * sentence. A specific code would prove less: the fallback is the path the
 * measured PROD reading took.
 *
 * `bulkCalls` counts the batch POSTs and `listCalls` the GET /api/rest/forms
 * reloads, both reported so a failure here can be attributed.
 */
async function stubAPI(page: Page) {
  const json = (status: number, body: unknown) => ({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  let bulkCalls = 0;
  let listCalls = 0;

  // Catch-all FIRST; Playwright resolves routes in REVERSE registration order,
  // so every specific route below it wins.
  await page.route(/(.*)\/api\/.*$/, (r) => r.fulfill(json(200, {})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) =>
    r.fulfill(
      json(200, {
        me: { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "en", is_super_admin: false },
        workspaces: [{ id: "ws1", name: "Demo", slug: "demo", role: "admin" }],
        chip_status: null, keys: [], domains: [], social: [], einvoice: null,
        webhooks: [], products: [], summary: { counts: {}, revenue_sen: 0 },
        sales: { items: [], page: 1, limit: 20, total: 0 },
        form_analytics: [], product_analytics: [],
      }),
    ),
  );
  await page.route(/(.*)\/api\/auth\/me$/, (r) =>
    r.fulfill(json(200, { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "en", is_super_admin: false })),
  );
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json(200, { lang: "en" })));
  await page.route(/(.*)\/api\/rest\/currency\/rates$/, (r) => r.fulfill(json(200, { rates: [], cached: false })));
  await page.route(/(.*)\/api\/rest\/products(\?.*)?$/, (r) => r.fulfill(json(200, [])));
  await page.route(/(.*)\/api\/rest\/forms\/(trash|bulk)(\?.*)?$/, (r) => {
    if (r.request().method() === "POST") {
      bulkCalls += 1;
      return r.fulfill(json(400, { error: SERVER_PROSE, code: "request.badRequest" }));
    }
    return r.fulfill(json(200, []));
  });
  await page.route(/(.*)\/api\/rest\/forms(\?.*)?$/, (r) => {
    listCalls += 1;
    return r.fulfill(json(200, [DRAFT]));
  });
  return {
    counts: () => ({ bulkCalls, listCalls }),
  };
}

test("a refused bulk publish leaves its sentence on screen 3s later, in English", async ({ page }) => {
  test.setTimeout(120_000);

  // A FRESH profile state: only the workspace and the language this guard needs.
  // Nothing is inherited from another card's run (the board has been bitten by a
  // leftover `jf_lang` before).
  await page.addInitScript(() => {
    localStorage.clear();
    localStorage.setItem("jf_workspace", "demo");
    localStorage.setItem("jf_lang", "en");
  });

  const stub = await stubAPI(page);
  await page.goto("/forms", { waitUntil: "networkidle" });

  // Select the row so the bulk toolbar renders, then press its Publish. Scoped
  // to the toolbar: the row ALSO carries a "Publish" button
  // (`builder.publish`) for a draft form, and the toolbar is the batch one.
  await expect(page.getByRole("checkbox", { name: `Select ${DRAFT.title}`, exact: true })).toBeVisible({
    timeout: 45_000,
  });
  await page.getByRole("checkbox", { name: `Select ${DRAFT.title}`, exact: true }).check();

  const toolbar = page.locator("div.jf-card").filter({ hasText: "1 selected" });
  await expect(toolbar).toBeVisible({ timeout: 20_000 });
  // How many list loads happened BEFORE the batch press. Two are expected by this
  // point (the mount effect re-runs when `workspace` settles from "" to "demo"),
  // so the count is a BASELINE, not a constant — what the fix claims is that a
  // REFUSED batch adds none, and a reload would show up as a delta.
  const listLoadsBefore = stub.counts().listCalls;
  await toolbar.getByRole("button", { name: "Publish", exact: true }).click();

  // The refusal must be visible at all (PRE-FIX it flashed for ~200ms, which
  // this wait may or may not catch — either way the sampling below is red).
  // The refusal renders in the page's error card. TWO `role=alert` nodes exist on
  // a Next page (the other is `#__next-route-announcer__`), so the locator is
  // scoped to the card that carries the message — an unscoped one is a strict-mode
  // violation, and a `role=alert` match would not say which node it read.
  const alert = page.locator('div[role="alert"].jf-card');
  await expect(alert, "the bulk refusal must render a message at all").toBeVisible({ timeout: 20_000 });
  // A LOCATOR READ MUST NOT BE ABLE TO HANG (t_ab4aa85b, MEASURED — not a
  // tidy-up). On the PRE-FIX shape this card is a FLASH: `load()`'s `setErr("")`
  // replaces it within a few ms of it appearing, and the `toBeVisible` above can
  // WIN that race. A locator action then waits the WHOLE test timeout (120s) for
  // an element that will never come back, so the pre-fix harness reported
  //
  //     Error: locator.innerText: Test timeout of 120000ms exceeded.
  //
  // — a red that names no assertion AND aborts the test before the sampling leg
  // the bite belongs to. This settle makes the wipe deterministic and owns it
  // under the label above (which the bite harness already accepts); the bounded
  // read below then only ever runs against an element that is really there.
  await page.waitForTimeout(1_000);
  await expect(alert, "the bulk refusal must render a message at all").toBeVisible({ timeout: 5_000 });
  const shown = ((await alert.first().textContent({ timeout: 5_000 }).catch(() => null)) ?? "").trim();
  expect(shown, "the page's own localized sentence, never the API's prose").toContain(EN_FALLBACK);
  expect(shown).not.toContain(SERVER_PROSE);
  expect(shown).not.toContain("badan permintaan");
  await expect(page.locator("html"), "the reading is the ENGLISH default").toHaveAttribute("lang", "en");

  // THE CARD'S ACCEPTANCE: still there 3s later. Sampled every 100ms rather than
  // waited on once, because the defect this guards is a set-then-WIPED message
  // and a single late read cannot tell "never rendered" from "rendered and
  // replaced". A wipe anywhere in the window fails the `all` assertion and names
  // the sample index it went missing at.
  const samples: boolean[] = [];
  for (let i = 0; i < 30; i += 1) {
    await page.waitForTimeout(100);
    samples.push((await alert.count()) > 0 && (await alert.isVisible().catch(() => false)));
  }
  const lastMissing = samples.lastIndexOf(false);
  expect(
    samples.every(Boolean),
    `the refusal must survive the whole 3s window. Samples (100ms apart): ${samples.map((s) => (s ? "1" : "0")).join("")}. ` +
      `A 0 after a 1 is the wipe this card fixes (last missing at sample ${lastMissing}); all-zero means it never rendered.`,
  ).toBe(true);

  // Still the page's own sentence at the END of the window, not some later text.
  // BOUNDED (t_155bdc39): the last unbounded locator read on this surface. Every
  // sample above already asserted the node is on screen, but a read that cannot
  // hang at all is the rule this family carries now — an unbounded `innerText()`
  // against a node that disappears mid-read waits the WHOLE test timeout and reds
  // with an Error line that names no assertion.
  const finalText = ((await alert.textContent({ timeout: 5_000 }).catch(() => null)) ?? "").trim();
  expect(finalText).toContain(EN_FALLBACK);
  expect(finalText).not.toContain(SERVER_PROSE);

  // Attribution: exactly the one batch POST was made, and the refusal did NOT add
  // a list reload. The pre-fix shape reloaded here (from the `finally`), and that
  // reload is what cleared `err` — so a growing delta is the wipe's mechanism and
  // is checked directly, not inferred from the string merely being absent.
  const counts = stub.counts();
  expect(counts.bulkCalls, "the failure under test is the batch POST").toBe(1);
  expect(
    counts.listCalls,
    `a refused batch must not reload the list (baseline before the press was ${listLoadsBefore})`,
  ).toBe(listLoadsBefore);

  console.log(
    `t_c9e37f49 evidence: alert="${finalText}" | window(100ms x30)=${samples.map((s) => (s ? "1" : "0")).join("")} | ` +
      `bulkCalls=${counts.bulkCalls} listCalls=${counts.listCalls}(baseline ${listLoadsBefore}) lang=en prose_present=false`,
  );
});
