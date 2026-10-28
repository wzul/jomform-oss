// A refused "Use template" must not offer a button that only reloads the list
// (card t_c2bba586).
//
// THE DEFECT, measured on prod v1.21.307 (web) as a signed-in merchant
// (wan@jomform.com, workspace `wan`, fresh browser state). `/templates` kept ONE
// error state for both a failed LIST LOAD and a failed ROW ACTION, and rendered it
// as
//
//     {err && (
//       <p role="alert" className="mt-4 text-sm text-red-700">
//         {err}
//         <button type="button" onClick={() => void load()}>{t("common.retry")}</button>
//       </p>
//     )}
//
// The Retry was added for issue #503 ("a failed load gets an action"). So when the
// API refused a blocked `theme.custom_js` on Use template, the merchant read the
// correct refusal sentence followed by a Retry that re-fetched /api/rest/templates
// + /api/rest/forms — a control whose label promises the one thing it never does.
// Measured: `document.querySelectorAll('p[role="alert"]')` -> ONE node whose text
// ended "…Remove it and save again.Retry", the URL stayed /templates, and the form
// count did not move. The refusal itself was right; only the affordance was wrong.
//
// THE FIX this file pins: two channels with two remedies. A refused ACTION renders
// with NO control (nothing about the listing changed, and no reload can grant a
// refusal); a failed LOAD keeps the Retry issue #503 asked for. The same single
// `err` used to serve createBlank, createFromForm, rename and remove, so arm 2 and
// arm 3 drive that shared branch rather than only `applyTemplate`.
//
// WHAT THIS GUARD PROVES, on the real built page with the API stubbed at the
// browser level (no Go server, no session, no DB):
//   1. the refusal renders — in the merchant's own language, from the machine
//      `code` on the wire — and its alert offers NO control at all;
//   2. no "Retry" button exists anywhere on the page after a refusal (the defect
//      is one button in ONE alert, so a scoped count would be too kind);
//   3. the refusal did not navigate: the URL is still /templates;
//   4. the wire's Malay prose appears nowhere on the page (issue #6177);
//   5. the SAME channel carries a refused DELETE with no Retry (the branch this
//      card's `err -> actionErr` rename touched for four other writers);
//   6. the refusal is cleared by the user's NEXT action, so this is not a message
//      that can never go away;
//   7. CONTROL, the other direction: a FAILED LOAD still renders its Retry, and
//      clicking it really re-issues the listing request and recovers the list —
//      issue #503 must not be dropped by a fix for this card.
//
// Run with:
//
//   npm run e2e                                              # the gate that runs this file
//   npx playwright test e2e/templates-action-refusal.spec.ts --project=feature-e2e
import { test, expect, type Page, type Locator } from "@playwright/test";

import { signIn } from "./auth";
import { readFileSync } from "node:fs";
import { join } from "node:path";

import { DICT } from "../src/components/i18nDict";

// The dashboard paths are gated on the SERVER (web/src/middleware.ts): a request
// without the `jf_session` cookie is redirected to /login BEFORE the page renders.
// This spec visits one, so it declares the session it means — see e2e/auth.ts.
test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});

/**
 * The refusal code read out of the SERVER source rather than typed here: a rename
 * on the API must fail this file, not silently leave it stubbing a body the API
 * never sends. `__dirname`, not `import.meta.url`: package.json has no
 * `"type": "module"`, so Playwright loads this spec as CommonJS.
 */
const errcodeGo = readFileSync(join(__dirname, "../../api/internal/httpapi/errcode.go"), "utf8");
const CUSTOM_JS_CODE = (errcodeGo.match(/\bCodeFormCustomJSRejected\s*=\s*"([^"]+)"/) ?? [])[1] ?? "";

/** The wire prose this handler answers for a blocked value. Malay must never render. */
const CUSTOM_JS_PROSE = "custom_js ditolak: active code execution marker: eval(";

const EN = {
  use: DICT["tplmg.use"].en,
  del: DICT["forms.delete"].en,
  retry: DICT["common.retry"].en,
  loadFail: DICT["tplmg.loadFail"].en,
  deleteFail: DICT["tplmg.deleteFail"].en,
  customJsRejected: DICT["apiErr.formCustomJsRejected"].en,
};

const TPL = {
  id: "tpl-guard-1",
  slug: "borang-guard",
  title: "Guard template",
  updated_at: "2026-10-24T00:00:00Z",
  theme: { primary: "#5e6ad2" },
};

const ME = { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "en", is_super_admin: false };
const BUNDLE = {
  me: ME,
  // `role` decides whether the write controls render at all (issue #163), so a
  // read_only reading would make this whole file vacuous.
  workspaces: [{ id: "a1", name: "Demo", slug: "demo", role: "admin" }],
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

type Counts = {
  templates: () => number;
  use: () => number;
  deletes: () => number;
};

type Answer = { status: number; body: unknown };
/** A fixed answer, or a function read AT REQUEST TIME — the second form is what
 *  lets an arm change the server's behaviour between two requests it drives. */
type AnswerOr = Answer | (() => Answer);

/**
 * stubs the whole dashboard so /templates reaches its loaded view without a Go
 * server. `opts` decides how the two endpoints under test answer.
 */
async function stubAPI(
  page: Page,
  opts: {
    /** answer for GET /api/rest/templates: one per call (the last repeats), or a
     *  function read at request time. */
    list?: AnswerOr[] | (() => Answer);
    /** answer for POST /api/rest/templates/{id}/use. */
    use?: AnswerOr;
    /** answer for DELETE /api/rest/templates/{id}. */
    del?: AnswerOr;
  } = {},
): Promise<Counts> {
  const json = (status: number, body: unknown) => ({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  const resolve = (a: AnswerOr): Answer => (typeof a === "function" ? a() : a);
  const list = opts.list ?? [{ status: 200, body: [TPL] }];
  const use = opts.use ?? { status: 200, body: { id: "form-clone-1" } };
  const del = opts.del ?? { status: 200, body: {} };

  let templates = 0;
  let useCalls = 0;
  let deletes = 0;

  // Catch-all FIRST; Playwright resolves routes in REVERSE registration order, so
  // every specific route below it wins.
  await page.route(/(.*)\/api\/.*$/, (r) => r.fulfill(json(200, {})));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(200, ME)));
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json(200, { lang: "en" })));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(200, BUNDLE)));
  await page.route(/(.*)\/api\/rest\/currency\/rates(\?.*)?$/, (r) =>
    r.fulfill(json(200, { rates: [], cached: false })),
  );
  await page.route(/(.*)\/api\/rest\/forms(\?.*)?$/, (r) => r.fulfill(json(200, [])));
  await page.route(/(.*)\/api\/rest\/templates(\?.*)?$/, (r) => {
    const answer = Array.isArray(list)
      ? resolve(list[Math.min(templates, list.length - 1)])
      : resolve(list);
    templates += 1;
    return r.fulfill(json(answer.status, answer.body));
  });
  await page.route(/(.*)\/api\/rest\/templates\/[^/?]+\/use(\?.*)?$/, (r) => {
    useCalls += 1;
    const answer = resolve(use);
    return r.fulfill(json(answer.status, answer.body));
  });
  await page.route(/(.*)\/api\/rest\/templates\/[^/?]+(\?.*)?$/, (r) => {
    if (r.request().method() !== "DELETE") return r.fulfill(json(200, {}));
    deletes += 1;
    const answer = resolve(del);
    return r.fulfill(json(answer.status, answer.body));
  });

  return {
    templates: () => templates,
    use: () => useCalls,
    deletes: () => deletes,
  };
}

/**
 * Opens /templates in a FRESH browser state — a leftover jf_lang or jf_workspace
 * from another card has produced a false reading on this board before, so the
 * storage is cleared here and the language is pinned to the ENGLISH default.
 * The row's title is the loaded signal.
 */
async function openTemplates(page: Page): Promise<void> {
  await page.addInitScript(() => {
    try {
      localStorage.clear();
      sessionStorage.clear();
      localStorage.setItem("jf_workspace", "demo");
      localStorage.setItem("jf_lang", "en");
    } catch {
      /* storage unavailable */
    }
  });
}

/** The alert node carrying a sentence. A Next page also renders a route announcer
 *  with role="alert", so the locator is filtered by the text under test. */
const alertWith = (page: Page, needle: string | RegExp): Locator =>
  page.getByRole("alert").filter({ hasText: needle });

test.describe("/templates: a refusal is not a failed load", () => {
  test("the guard reads the real server contract (it cannot pass vacuously)", () => {
    expect(
      CUSTOM_JS_CODE,
      "errcode.go no longer declares CodeFormCustomJSRejected — the stub below would answer a code the " +
        "API never sends, so arm 1 would prove nothing about a real refusal",
    ).toBe("form.customJsRejected");
    expect(EN.customJsRejected).toContain("Custom JS");
  });

  test("a refused Use template renders the refusal and offers NO control", async ({ page, baseURL }) => {
    const counts = await stubAPI(page, {
      use: { status: 400, body: { code: CUSTOM_JS_CODE, error: CUSTOM_JS_PROSE } },
    });
    await openTemplates(page);
    await page.goto(`${baseURL}/templates`, { waitUntil: "networkidle" });

    await expect(page.locator("li", { hasText: TPL.title }).first()).toBeVisible({ timeout: 45_000 });
    await expect(page.locator("html"), "the reading is the ENGLISH default").toHaveAttribute("lang", "en");
    const listCallsBefore = counts.templates();

    await page.getByRole("button", { name: EN.use, exact: true }).click();

    const refusal = alertWith(page, EN.customJsRejected);
    await expect(refusal, "the refusal sentence never rendered").toBeVisible({ timeout: 15_000 });
    expect(counts.use(), "no POST /api/rest/templates/{id}/use was sent — nothing was refused").toBe(1);

    // THE CARD: the refusal's own alert must carry no control at all. `count()`
    // rather than `not.toBeVisible()` so a button that exists but is painted off
    // screen fails too — the defect is the AFFORDANCE, not its visibility.
    expect(
      await refusal.locator("button").count(),
      "the refusal alert still carries a button — a refusal is not a load failure and no control can grant it",
    ).toBe(0);
    // …and not anywhere else on the page either.
    expect(
      await page.getByRole("button", { name: EN.retry, exact: true }).count(),
      `a "${EN.retry}" control exists on the page after a refusal (card t_c2bba586)`,
    ).toBe(0);

    // The refusal did not navigate, and it did not relist: nothing about the
    // listing changed, which is exactly why a reload cannot be the remedy.
    expect(page.url(), "a refused use navigated away from /templates").toContain("/templates");
    expect(page.url()).not.toContain("/builder");
    expect(counts.templates(), "the refusal fired a relist — this page does not relist on a refusal").toBe(
      listCallsBefore,
    );

    // The wire's Malay prose must never reach the screen (issue #6177).
    expect(
      await page.getByText(CUSTOM_JS_PROSE, { exact: false }).count(),
      "the API's Malay prose reached the page instead of the dictionary sentence",
    ).toBe(0);

    console.log(
      `OBSERVED use=true url=${page.url()} alertButtons=${await refusal.locator("button").count()} ` +
        `retryButtons=${await page.getByRole("button", { name: EN.retry, exact: true }).count()} ` +
        `listCalls=${counts.templates()}`,
    );
  });

  test("a refused DELETE rides the same channel with no Retry", async ({ page, baseURL }) => {
    // The card's `err` also served createBlank, createFromForm, rename and remove.
    // This arm drives REMOVE so the fix cannot be a special case for applyTemplate.
    const counts = await stubAPI(page, { del: { status: 400, body: { code: "request.badRequest", error: "tidak dibenarkan" } } });
    await openTemplates(page);
    page.on("dialog", (d) => void d.accept());
    await page.goto(`${baseURL}/templates`, { waitUntil: "networkidle" });

    await expect(page.locator("li", { hasText: TPL.title }).first()).toBeVisible({ timeout: 45_000 });
    await page.getByRole("button", { name: EN.del, exact: true }).click();

    const refusal = alertWith(page, EN.deleteFail);
    await expect(refusal, "a refused DELETE rendered nothing").toBeVisible({ timeout: 15_000 });
    expect(counts.deletes(), "no DELETE /api/rest/templates/{id} was sent — nothing was refused").toBe(1);
    expect(
      await refusal.locator("button").count(),
      "a refused DELETE rendered a control — the shared action channel was only half separated",
    ).toBe(0);
    expect(
      await page.getByRole("button", { name: EN.retry, exact: true }).count(),
      `a "${EN.retry}" control exists after a refused DELETE`,
    ).toBe(0);
  });

  test("the refusal is cleared by the user's NEXT action", async ({ page, baseURL }) => {
    // Not "a message that never goes away": every handler opens by clearing its own
    // channel, so a later successful action retires the sentence.
    let rows: unknown[] = [TPL];
    const counts = await stubAPI(page, {
      list: () => ({ status: 200, body: rows }),
      use: { status: 400, body: { code: CUSTOM_JS_CODE, error: CUSTOM_JS_PROSE } },
      // The delete really lands, so the relist that follows it loses the row.
      del: () => {
        rows = [];
        return { status: 200, body: {} };
      },
    });
    await openTemplates(page);
    page.on("dialog", (d) => void d.accept());
    await page.goto(`${baseURL}/templates`, { waitUntil: "networkidle" });

    await expect(page.locator("li", { hasText: TPL.title }).first()).toBeVisible({ timeout: 45_000 });
    await page.getByRole("button", { name: EN.use, exact: true }).click();
    await expect(alertWith(page, EN.customJsRejected)).toBeVisible({ timeout: 15_000 });

    await page.getByRole("button", { name: EN.del, exact: true }).click();
    await expect
      .poll(async () => counts.deletes(), { timeout: 15_000 })
      .toBe(1);
    await expect(
      alertWith(page, EN.customJsRejected),
      "the previous refusal survived the next action — it is not cleared at the start of one",
    ).toHaveCount(0);
    // …and the successful delete really landed (the row is gone after the relist).
    await expect(page.locator("li", { hasText: TPL.title })).toHaveCount(0, { timeout: 15_000 });
  });

  test("CONTROL issue #503: a FAILED LOAD keeps its Retry, and the Retry really reloads", async ({
    page,
    baseURL,
  }) => {
    // The other direction, and the one this card must not break: the load-failure
    // line KEEPS the action issue #503 added, and that action has to do something.
    // A fix that simply removed the button everywhere would pass arms 1–3 and fail
    // here.
    //
    // EVERY list request fails until the arm flips it, rather than "the first one
    // does": this page fires `load()` on mount AND again when the session resolves
    // the workspace (issue #170), so a one-shot 500 would be consumed by the
    // pre-workspace request and the visible load would succeed — measured, that is
    // exactly how the first version of this arm passed vacuously.
    let answer: Answer = { status: 500, body: { error: "boom" } };
    const counts = await stubAPI(page, { list: () => answer });
    await openTemplates(page);
    await page.goto(`${baseURL}/templates`, { waitUntil: "networkidle" });

    const loadFailure = alertWith(page, EN.loadFail);
    await expect(loadFailure, "a failed list load rendered no sentence").toBeVisible({ timeout: 45_000 });
    const retry = loadFailure.getByRole("button", { name: EN.retry, exact: true });
    await expect(retry, `the load-failure line lost the "${EN.retry}" action issue #503 added`).toBeVisible();

    const before = counts.templates();
    answer = { status: 200, body: [TPL] };
    await retry.click();
    // The click must really refetch — not just repaint — and the recovered list is
    // the proof that the request it fired succeeded.
    await expect(page.locator("li", { hasText: TPL.title }).first()).toBeVisible({ timeout: 45_000 });
    expect(counts.templates(), "the Retry did not re-issue GET /api/rest/templates").toBeGreaterThan(before);
    await expect(loadFailure, "the load-failure line survived a successful reload").toHaveCount(0);

    console.log(`OBSERVED loadFail retryClicked=true listCalls=${before}->${counts.templates()} url=${page.url()}`);
  });
});
