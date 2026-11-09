// Regression guard for t_e50a9680 — a LOCKED-OUT visitor must be told WHEN they
// may try again, not only that the attempt failed.
//
// THE DEFECT, MEASURED ON THE BUILT APP BEFORE THIS FIX
//   With the visitor's address already at the login limiter's threshold, a real
//   password-form submit through the hop answered 429 with
//
//     Retry-After: 900
//     {"code":"request.tooManyRequests","error":"Terlalu banyak percubaan. Sila cuba lagi dalam 15 minit."}
//
//   and the page rendered ONE generic sentence in every locale:
//
//     role=alert "Failed. Please try again."
//
//   `request.tooManyRequests` is a CLASS code (api/internal/httpapi/errcode.go,
//   `classCode(429)`), which apiErr.ts's dictionary deliberately does not map, so
//   the body fell through to `login.genericError`; and nothing in web/src read
//   `Retry-After`, the header the hop had just been fixed to relay
//   (web/src/lib/proxyResponseHeaders.ts). So the one number that answers "when?"
//   was discarded twice over.
//
// WHY A BROWSER SPEC AND NOT ONLY THE UNIT GUARD
//   `src/lib/rateLimitErr.test.ts` proves the mapping in isolation and pins the
//   page's call sites by source text. Neither can prove the number reaches the
//   SCREEN: the header has to survive the hop, `r.headers.get("retry-after")` has
//   to be readable in the page, `t()` has to substitute, and the alert has to
//   render. This drives the shipped page and reads the live DOM.
//
// THE API IS STUBBED AT THE BROWSER LEVEL, exactly as the page's own fetch sees
// it — status, header and body all as the hop delivers them — so the gate needs
// nothing but the build it is already serving, and the assertion is about the
// PAGE, not about a running API.
//
// THE 401 CONTROL IS NOT DECORATION. It proves the wrong-credentials sentence
// still renders, i.e. that the new sentence is chosen for a THROTTLE and not for
// every failure on this form.
import { test, expect, type Page } from "@playwright/test";

import { DICT, type Lang } from "../src/components/i18nDict";

/** A dictionary row in one locale, with its placeholders substituted like `t()`. */
function tr(key: keyof typeof DICT, lang: Lang, params: Record<string, string | number> = {}): string {
  let s: string = DICT[key][lang];
  for (const [k, v] of Object.entries(params)) s = s.replaceAll(`{${k}}`, String(v));
  return s;
}

/** The API's class code for a 429 (api/internal/httpapi/errcode.go). */
const TOO_MANY = "request.tooManyRequests";
/** The prose the API really sends with the login lockout (auth.LoginLockoutMessage). */
const LOCKOUT_PROSE = "Terlalu banyak percubaan. Sila cuba lagi dalam 15 minit.";

/**
 * A FRESH visitor: no cookies, no stored locale, and the page's other network
 * calls answered — this host keeps localStorage between runs, so a `jf_lang`
 * left by an earlier card would render a locale this file never asserts.
 *
 * Routes are registered catch-all FIRST because Playwright resolves them in
 * reverse order: every specific stub added after this one wins.
 */
async function freshVisitor(page: Page, lang: Lang) {
  await page.context().clearCookies();
  await page.addInitScript((l) => {
    try {
      localStorage.clear();
      sessionStorage.clear();
      localStorage.setItem("jf_lang", l);
    } catch {
      /* storage unavailable */
    }
  }, lang);
  const json = (status: number, body: unknown, headers: Record<string, string> = {}) => ({
    status,
    headers: { "content-type": "application/json", ...headers },
    body: JSON.stringify(body),
  });
  await page.route(/(.*)\/api\/.*$/, (r) => r.fulfill(json(200, {})));
  await page.route("**/api/auth/social/providers", (r) => r.fulfill(json(200, [])));
  // `{}` leaves the locale alone: the provider promotes nothing and the
  // `jf_lang` set above stays in effect.
  await page.route("**/api/auth/profile", (r) => r.fulfill(json(200, {})));
}

/** Open the password form (the page's default view is email-first magic link). */
async function openPasswordForm(page: Page, lang: Lang) {
  await page.goto("/login");
  await page.getByRole("button", { name: `${tr("login.loginWithPassword", lang)} ${tr("login.orSignupWithPassword", lang)}` }).click();
  await expect(page.locator('input[name="password"]')).toBeVisible();
}

/** Submit the REAL form (no direct fetch), so the assertion is about the page. */
async function submitPassword(page: Page) {
  await page.fill('input[name="email"]', "merchant@example.com");
  await page.fill('input[name="password"]', "not-the-right-password");
  await page.locator('input[name="password"]').press("Enter");
}

/** The page's own error banner — `page.tsx`, `p[role="alert"]`. */
const ALERT = 'p[role="alert"]';

test("a locked-out submit names the 15-minute window the server chose", async ({ page }) => {
  await freshVisitor(page, "en");
  await page.route("**/api/auth/login", (r) =>
    r.fulfill({
      status: 429,
      headers: { "content-type": "application/json", "retry-after": "900" },
      body: JSON.stringify({ code: TOO_MANY, error: LOCKOUT_PROSE }),
    }),
  );

  await openPasswordForm(page, "en");
  await submitPassword(page);

  const alert = page.locator(ALERT);
  await expect(alert).toBeVisible();
  // THE BITE: with the pre-fix page this reads "Failed. Please try again." and
  // every assertion below fails, in every locale.
  await expect(alert).toHaveText("Too many attempts. Try again in 15 minutes.");
  // It is a dictionary sentence, not the server's Malay prose (which the card
  // forbids rendering on an English page).
  await expect(alert).not.toContainText(LOCKOUT_PROSE);
  await expect(alert).not.toContainText("Failed");
});

test("the window is localized — a Malay visitor reads 15 minit", async ({ page }) => {
  await freshVisitor(page, "ms");
  await page.route("**/api/auth/login", (r) =>
    r.fulfill({
      status: 429,
      headers: { "content-type": "application/json", "retry-after": "900" },
      body: JSON.stringify({ code: TOO_MANY, error: LOCKOUT_PROSE }),
    }),
  );

  await openPasswordForm(page, "ms");
  await submitPassword(page);

  await expect(page.locator(ALERT)).toHaveText(tr("apiErr.tooManyRequestsInMinutes", "ms", { minutes: 15 }));
});

test("the per-IP throttle's 30 seconds is counted in seconds, not rounded into minutes", async ({ page }) => {
  await freshVisitor(page, "en");
  // The coarse per-IP limiter's answer (middleware/ratelimit.go): a PLAIN-TEXT
  // body, no JSON code — which is why the page has to read the status as well.
  await page.route("**/api/auth/login", (r) =>
    r.fulfill({
      status: 429,
      headers: { "content-type": "text/plain", "retry-after": "30" },
      body: "Terlalu banyak percubaan. Cuba lagi sebentar. (429)",
    }),
  );

  await openPasswordForm(page, "en");
  await submitPassword(page);

  await expect(page.locator(ALERT)).toHaveText("Too many attempts. Try again in 30 seconds.");
});

test("a 429 with no usable Retry-After still says too many attempts", async ({ page }) => {
  await freshVisitor(page, "en");
  await page.route("**/api/auth/login", (r) =>
    r.fulfill({
      status: 429,
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ code: TOO_MANY, error: LOCKOUT_PROSE }),
    }),
  );

  await openPasswordForm(page, "en");
  await submitPassword(page);

  await expect(page.locator(ALERT)).toHaveText(tr("apiErr.rateLimited", "en"));
});

test("a 401 keeps the wrong-credentials sentence (the control)", async ({ page }) => {
  await freshVisitor(page, "en");
  // The uniform 401 the API answers a bad password with (failLogin): identical
  // body for a known and an unknown email.
  await page.route("**/api/auth/login", (r) =>
    r.fulfill({
      status: 401,
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ code: "login.invalid", error: "invalid login details" }),
    }),
  );

  await openPasswordForm(page, "en");
  await submitPassword(page);

  const alert = page.locator(ALERT);
  await expect(alert).toHaveText("Email or password is incorrect.");
  await expect(alert).not.toContainText("Too many attempts");
});
