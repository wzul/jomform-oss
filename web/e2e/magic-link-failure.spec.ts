import { test, expect, type Page } from "@playwright/test";
import { DICT } from "../src/components/i18nDict";

// BITE PROOF for card t_9ed9bc2c, kept as a real gate test rather than a one-off run.
//
// The page used to answer every non-2xx with ONE generic sentence and never read the
// response body, so `magicLink.tokenMissing` — its mapping and its dictionary entry —
// could not be rendered by any surface at all. The card measured that in a browser with
// the endpoint stubbed; this asserts the two readings DIFFER, which is the property the
// code decides.

const EN_INVALID = DICT["magic.invalid"].en;
const EN_MISSING_TOKEN = DICT["apiErr.magicLinkTokenMissing"].en;

async function stubVerify(page: Page, body: unknown) {
  // CATCH-ALL FIRST, the specific route AFTER it: Playwright resolves matching routes in
  // REVERSE registration order, so the LAST registered wins. Registered the other way
  // round, the catch-all SHADOWS the verify stub and the page never sees the code under
  // test (measured: the assertion read the generic sentence with the specific stub in
  // place).
  //
  // The catch-all answers **ANONYMOUS (401)**, not 200: with a 200 the app believes there
  // is a session and the page bounces to the landing page, so the assertion would read the
  // marketing copy instead.
  await page.route(/(.*)\/api\/.*$/, (route) =>
    route.fulfill({
      status: 401,
      contentType: "application/json",
      body: JSON.stringify({ code: "unauth", error: "no session" }),
    }),
  );
  await page.route(/(.*)\/api\/auth\/magic-link\/verify$/, (r) =>
    r.fulfill({
      status: 400,
      contentType: "application/json",
      body: JSON.stringify(body),
    }),
  );
}

test.describe("magic-link failure rendering", () => {
  test("a known code renders its OWN sentence, and an unknown one the generic fallback", async ({
    page,
  }) => {
    expect(EN_MISSING_TOKEN).not.toBe(EN_INVALID);

    // KNOWN code: the body must decide.
    await stubVerify(page, {
      code: "magicLink.tokenMissing",
      error: "MALAY-PROSE-SENTINEL",
    });
    await page.goto("/magic-link?token=abc");
    const alert = page.locator("p").first();
    await expect(alert).toHaveText(EN_MISSING_TOKEN, { timeout: 15_000 });
    // …and the server's raw prose must never reach the screen.
    await expect(page.locator("body")).not.toContainText("MALAY-PROSE-SENTINEL");

    // UNKNOWN code: the surface's own sentence, unchanged.
    await page.unrouteAll();
    await stubVerify(page, { code: "some.unknownCode", error: "whatever" });
    await page.goto("/magic-link?token=abc2");
    await expect(page.locator("p").first()).toHaveText(EN_INVALID, { timeout: 15_000 });
  });
});
