// The chat page's availability probe under a MISSING workspace scope: the
// refusal must read as "choose a workspace", not "the assistant is not
// available".
//
// THE DEFECT (card t_997bb646, filed by the QA lane while verifying PR 698).
// The page's FIRST chat call is GET /v1/rest/chat, and on a fresh session it
// fires BEFORE the Shell has selected a workspace — so it goes out with no
// ?workspace= at all. On a session that can reach more than one workspace the
// server answers it 400 (rest_scope.go's `scopeAmbiguous`: there is no rule by
// which "no scope" means one of them). The page consumed that 400 through
// `runLoad` and fell straight into `setAvailable(false)`, which renders
// chat.unavailable ("The assistant is not available right now.") and DISABLES
// the composer — a false statement about a condition the user fixes by choosing
// a workspace, and the same 400 the message path already reports correctly
// (chat.scopeRequired). That asymmetry is what this file measures.
//
// WHY THIS IS A BROWSER TEST AND NOT ONLY A SOURCE GUARD. `chatPage.test.ts`
// pins the branch, and that guard bites on a revert — but this page's state is
// set by a request whose URL depends on what the Shell has written to
// localStorage at the moment the effect fires, and only a real build can show
// the two orders (probe first, Shell re-selection second). This file drives the
// production build and reads the rendered DOM, which is how the defect was
// found.
//
// HOW THE EMPTY STATE IS REACHED. The probe's own request is the stub's only
// lever: `stubProbe` answers 400 with the server's scope refusal body while
// every other route is served. No session, no backend, no credential.
//
// WHAT MAKES IT BITE. The assertions read the shipped English copy out of the
// app's own dictionary rather than typing it in, and they assert BOTH
// directions: the scope copy IS rendered, AND chat.unavailable is NOT (the
// pre-fix shape), AND the composer is still offered. A revert to the pre-fix
// branch fails the second and third assertions.
//
// Run with:
//
//   npm run a11y                       # the whole gate (this file included)
//   npx playwright test e2e/chat-probe-400.spec.ts    # this file alone
import { test, expect, type Page } from "@playwright/test";
import { DICT } from "../src/components/i18nDict";

const EN = {
  scopeRequired: DICT["chat.scopeRequired"].en,
  unavailable: DICT["chat.unavailable"].en,
  subtitle: DICT["chat.subtitle"].en,
  title: DICT["chat.title"].en,
  placeholder: DICT["chat.placeholder"].en,
};

/** The two-workspace session: the probe must not guess between them. */
const BUNDLE = {
  me: { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "", is_super_admin: false },
  workspaces: [
    { id: "ws-id-a", name: "First", slug: "ws-a", role: "admin" },
    { id: "ws-id-b", name: "Second", slug: "ws-b", role: "admin" },
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
};

/**
 * Serve everything except the availability probe, which answers `probeStatus`.
 * The probe is identified by its METHOD as well as its path (the message POST
 * shares the path), and its request URL is recorded so the test can assert the
 * shape it believes it reproduced.
 */
async function stubProbe(page: Page, probeStatus: number): Promise<string[]> {
  const json = (body: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  const probes: string[] = [];
  // Catch-all FIRST, specific routes after it: Playwright resolves matching
  // routes in reverse registration order, so the later (specific) ones win.
  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(BUNDLE)));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(BUNDLE.me)));
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "" })));
  await page.route(/(.*)\/api\/rest\/chat(\?.*)?$/, (r) => {
    if (r.request().method() !== "GET") {
      return r.fulfill(json({ reply: "ok" }));
    }
    probes.push(r.request().url());
    return r.fulfill({
      status: probeStatus,
      contentType: "application/json",
      body: JSON.stringify({
        error: "skop workspace diperlukan — pilih workspace dahulu",
        code: "request.badRequest",
      }),
    });
  });
  return probes;
}

/** A FRESH browser state: no localStorage at all, English. */
async function freshState(page: Page) {
  await page.addInitScript(() => {
    try {
      localStorage.clear();
      sessionStorage.clear();
      localStorage.setItem("jf_lang", "en");
    } catch {
      /* storage unavailable */
    }
  });
}

test.describe("chat availability probe: a scope refusal is not 'unavailable'", () => {
  test("a 400 on the probe renders the scope copy and keeps the composer", async ({ page, baseURL }) => {
    const probes = await stubProbe(page, 400);
    await freshState(page);

    await page.goto(`${baseURL}/chat`, { waitUntil: "domcontentloaded" });

    // The page must have rendered at all — otherwise the assertions below would
    // pass on a blank document. The heading alone is NOT enough to say the page
    // has settled: `/chat` is statically prerendered, so it is visible before
    // hydration has run the availability effect at all (measured: reading the
    // probe log right after this line found it EMPTY under load, which is a race
    // in the test, not a defect in the page).
    await expect(page.getByRole("heading", { name: EN.title })).toBeVisible({ timeout: 15_000 });

    // THE FIX, asserted first because it is what hydration produces: the
    // actionable copy, in the alert slot.
    await expect(
      page.locator('p[role="alert"]'),
      "the scope refusal from the availability probe must be rendered as the page's own message",
    ).toHaveText(EN.scopeRequired, { timeout: 15_000 });

    // NOW the probe has definitely fired. The shape is asserted, not assumed.
    //
    // RE-ANCHORED FOR t_da4b2031. This used to assert the probe was sent exactly
    // once and UNSCOPED, on the reasoning that "a fresh state has no jf_workspace,
    // so the Shell has not won yet". Both halves moved with the fix:
    //
    //   * `workspaceQuery()` no longer reads localStorage directly, so the
    //     workspace the bundle CONFIRMS is what the request carries;
    //   * the probe is keyed on that scope, so when it is published the page
    //     probes AGAIN — the self-heal that turns "refused on a stale slug" into a
    //     working page rather than a message the user cannot clear. The stub
    //     answers every probe with the refusal, so both carry the scope copy here.
    //
    // What this file owns is unchanged: the PAGE must render the scope copy, not
    // "the assistant is not available", and keep the composer. The shape asserted
    // is the property that matters — no probe may carry a slug the session does
    // not have, and the final one carries the workspace the session confirmed.
    expect(probes.length, "the availability probe must have been sent at least once").toBeGreaterThan(0);
    for (const url of probes) {
      expect(
        url,
        `no probe may carry a workspace the session does not have. Sent: ${JSON.stringify(probes)}`,
      ).not.toContain("workspace=ws-gone");
    }
    expect(
      probes.at(-1),
      `the last probe must carry the confirmed workspace (the page re-probes once the session has ` +
        `answered), never no scope at all. Sent: ${JSON.stringify(probes)}`,
    ).toContain("workspace=ws-a");

    // Quoted evidence: the live DOM, not what the test expects to find.
    const alertText = await page.locator('p[role="alert"]').innerText();
    const logText = await page.locator('[role="log"]').innerText();
    const composerDisabled = await page.locator("#chat-input").isDisabled();
    console.log(
      `PROBE-400 OBSERVED: alert=${JSON.stringify(alertText)} composerDisabled=${composerDisabled} ` +
        `probeUrl=${JSON.stringify(probes[0])} logHasUnavailable=${logText.includes(EN.unavailable)}`,
    );

    // And NOT the false statement. This is the assertion the defect fails:
    // pre-fix the log region carried exactly this sentence.
    await expect(
      page.getByText(EN.unavailable, { exact: true }),
      `the probe's 400 must not be reported as "the assistant is not available" ` +
        `("${EN.unavailable}") — the assistant IS available, this request just carried no scope`,
    ).toHaveCount(0);

    // The box stays offered: the refusal is fixable by selecting a workspace, so
    // disabling the composer would strand the user on the page they can fix.
    await expect(
      page.locator("#chat-input"),
      "the composer must stay enabled on a scope refusal — choosing a workspace is the fix, and a " +
        "disabled box gives the user nothing to do",
    ).toBeEnabled();
  });

  test("a genuine 5xx on the probe still reports the assistant unavailable", async ({ page, baseURL }) => {
    // The other direction, so the fix cannot be satisfied by deleting the
    // unavailable path: a real failure keeps its own copy and still closes the box.
    await stubProbe(page, 500);
    await freshState(page);
    await page.goto(`${baseURL}/chat`, { waitUntil: "domcontentloaded" });
    await expect(page.getByRole("heading", { name: EN.title })).toBeVisible({ timeout: 15_000 });

    await expect(
      page.getByText(EN.unavailable, { exact: true }),
      "a 5xx on the availability probe must still render chat.unavailable — deleting that path " +
        "would satisfy the scope assertions for the wrong reason",
    ).toBeVisible();
    await expect(
      page.getByText(EN.scopeRequired, { exact: true }),
      "a 5xx is not a scope refusal — the scope copy must not appear",
    ).toHaveCount(0);
    await expect(page.locator("#chat-input"), "a dead assistant must not offer the box").toBeDisabled();
  });
});
