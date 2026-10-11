// Card t_5e39dc9f — the DEFAULT (passwordless) sign-in must return the visitor
// to the page they were bounced from.
//
// THE DEFECT, measured end-to-end on prod 2026-10-11 in a fresh anonymous
// browser: /profile → 307 /login?next=https%3A%2F%2Fjomform.com%2Fprofile → the
// magic request body carried no `next` → the emailed link was
// https://jomform.com/magic-link?token=… with no `next=` → clicking it landed on
// /workspace-picker. Three sites, none in PR #1140's diff: the login page never
// forwarded the value, the link builder never carried it, and the magic-link page
// hard-coded resolvePostLogin("").
//
// WHY THESE ARMS ARE IN THE BROWSER. A unit test can pin the rule
// (src/lib/nextTarget.test.ts) and the server can be asserted on the email body
// (internal/httpapi/magic_link_next_test.go), but the half that was dead is the
// WIRING: a value the login page validated at mount and then never sent, and a
// navigation the magic-link page never made. Both are observable only by driving
// the real pages — the request the form actually posts, and the URL the browser
// actually ends up on.
//
// The arms below stub the API at the browser level (the suite runs the Next app
// alone — see playwright.config.ts), and intercept the dashboard targets so a
// navigation is read as a real document request rather than a URL the SSR gate
// may have rewritten.
import { test, expect, type Page } from "@playwright/test";
import { DICT } from "../src/components/i18nDict";
import { E2E_ORIGIN } from "./auth";

const GUARDED = `${E2E_ORIGIN}/profile`;
const FOREIGN = "https://evil.example/profile";

/** Register the catch-all anon stub FIRST: Playwright resolves routes in
 *  REVERSE registration order, so the LAST registered wins and every stub below
 *  must come after this one (the same lesson as magic-link-failure.spec.ts). */
async function stubAnon(page: Page) {
  await page.route(/(.*)\/api\/.*$/, (route) =>
    route.fulfill({
      status: 401,
      contentType: "application/json",
      body: JSON.stringify({ code: "unauth", error: "no session" }),
    }),
  );
}

/** A marker page for a URL, so "the browser went there" is a real document
 *  request we can read back. */
async function stubTarget(page: Page, url: string, marker: string) {
  await page.route(url, (route) =>
    route.fulfill({ status: 200, contentType: "text/html", body: `<h1>${marker}</h1>` }),
  );
}

/** Stub the verify endpoint (the magic-link page POSTs the token on mount). */
async function stubVerifyOK(page: Page) {
  await page.route(/(.*)\/api\/auth\/magic-link\/verify$/, (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: "{}" }),
  );
}

/** Collect the body of every magic-link request the page makes. */
async function captureMagicRequests(page: Page): Promise<Record<string, unknown>[]> {
  const seen: Record<string, unknown>[] = [];
  await page.route(/(.*)\/api\/auth\/magic-link\/request$/, async (route) => {
    seen.push(route.request().postDataJSON() as Record<string, unknown>);
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ status: "ok" }),
    });
  });
  return seen;
}

/** Fill the (default, email-first) magic form and submit it. */
async function requestMagicLink(page: Page) {
  await page.locator("input[type=email]").fill("visitor@example.test");
  await page.locator("input[type=email]").press("Enter");
  await expect(page.locator("body")).toContainText(DICT["login.passwordlessSent"].en, {
    timeout: 15_000,
  });
}

test("the login page forwards the validated ?next= with the magic-link request", async ({ page }) => {
  await stubAnon(page);
  const seen = await captureMagicRequests(page);

  // (a) the value the SSR gate hands /login must travel with the request …
  await page.goto(`/login?next=${encodeURIComponent(GUARDED)}`);
  await requestMagicLink(page);
  expect(seen.length, "the magic-link request never reached the stub").toBe(1);
  expect(
    seen[0]?.next,
    `the magic-link request carried no next (body=${JSON.stringify(seen[0])}) — the emailed link ` +
      `can therefore never return the visitor to ${GUARDED} (card t_5e39dc9f)`,
  ).toBe(GUARDED);
  expect(seen[0]?.email).toBe("visitor@example.test");
  console.log(`OBSERVED magic request body=${JSON.stringify(seen[0])}`);

  // (b) CONTROL: a FOREIGN next is discarded by the page, so it cannot be sent
  // to be embedded in an email in the first place.
  seen.length = 0;
  await page.goto(`/login?next=${encodeURIComponent(FOREIGN)}`);
  await requestMagicLink(page);
  expect(seen.length).toBe(1);
  expect(
    seen[0]?.next,
    `the login page forwarded a foreign next (body=${JSON.stringify(seen[0])}) — its own ` +
      `validation must drop https://evil.example before the request is sent`,
  ).toBeFalsy();
});

test("the value the login page sends is the value the magic-link page returns to", async ({ page }) => {
  await stubAnon(page);
  const seen = await captureMagicRequests(page);
  await stubVerifyOK(page);
  await stubTarget(page, `${E2E_ORIGIN}/profile`, "RETURNED-TO-PROFILE");

  await page.goto(`/login?next=${encodeURIComponent(GUARDED)}`);
  await requestMagicLink(page);
  const next = String(seen[0]?.next || "");
  expect(next, `the login page sent no next (body=${JSON.stringify(seen[0])})`).toBe(GUARDED);

  // The mail client now opens the link the server built from that request: the
  // token plus the value the page forwarded.
  await page.goto(`/magic-link?token=e2e-fixture&next=${encodeURIComponent(next)}`);
  await expect(page.locator("h1")).toHaveText("RETURNED-TO-PROFILE", { timeout: 15_000 });
  expect(new URL(page.url()).pathname).toBe("/profile");
  console.log(`OBSERVED magic-link landing url=${page.url()}`);
});

test("CONTROL: a foreign next falls back to today's behaviour and never leaves the app", async ({
  page,
}) => {
  await stubAnon(page);
  await stubVerifyOK(page);
  // resolvePostLogin("") with no workspaces → the picker, exactly as before.
  await page.route(/(.*)\/api\/workspaces$/, (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: "[]" }),
  );
  await stubTarget(page, `${E2E_ORIGIN}/workspace-picker`, "FELL-BACK-TO-PICKER");
  // A leak detector: if the page navigated off-origin, this route would answer
  // and the assertion below would read the wrong marker.
  await stubTarget(page, FOREIGN, "LEFT-THE-APP");

  await page.goto(`/magic-link?token=e2e-fixture&next=${encodeURIComponent(FOREIGN)}`);
  await expect(page.locator("h1")).toHaveText("FELL-BACK-TO-PICKER", { timeout: 15_000 });
  expect(new URL(page.url()).origin).toBe(E2E_ORIGIN);
});

test("CONTROL: the pre-fix link (token only) still resolves as it did before", async ({ page }) => {
  await stubAnon(page);
  await stubVerifyOK(page);
  await page.route(/(.*)\/api\/workspaces$/, (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: "[]" }),
  );
  await stubTarget(page, `${E2E_ORIGIN}/workspace-picker`, "FELL-BACK-TO-PICKER");

  await page.goto("/magic-link?token=e2e-fixture");
  await expect(page.locator("h1")).toHaveText("FELL-BACK-TO-PICKER", { timeout: 15_000 });
});
