// Regression guard for t_7f01f01f — a SERVER refusal on the passkey button must
// not be reported as a browser that cannot do passkeys.
//
// THE DEFECT, MEASURED ON THE BUILT PAGE BEFORE THIS FIX
//   `passkeyLogin()` (web/src/app/login/page.tsx) read the start call as
//
//     const st = await fetch("/api/auth/passkeys/login/start", { method: "POST" }).then((r) => r.json());
//
//   inside a try whose catch sets `login.passkeyUnsupported` ("Passkey cancelled
//   or not supported by this browser."). The STATUS was never checked, so every
//   refusal that is not a JSON body — the per-IP limiter's 429, which is
//   `http.Error` PLAIN TEXT plus `Retry-After: 30` (api/internal/httpapi/
//   middleware/ratelimit.go), a 5xx, a proxy error page — made `r.json()` throw
//   and landed in that same catch. The visitor was told their BROWSER cannot do
//   passkeys while the browser was fine and the server had refused the call, and
//   the `Retry-After` the throttle had just sent was dropped on the floor.
//
// WHAT EACH ANSWER MUST NOW SAY
//   * 429 -> the throttle sentence from `lib/rateLimitErr.ts` (card t_e50a9680),
//     naming the window the server chose — the same rule every other surface on
//     this page follows;
//   * any other non-2xx, and a 200 whose body carries no challenge, -> the
//     page's own generic failure line. Nothing about the browser is implied;
//   * a 200 that really did hand over a challenge, and a `navigator.credentials.get`
//     that then rejects (a dismissed prompt, no credential on this device), ->
//     `login.passkeyUnsupported`. That is the ONLY case the sentence is true for,
//     and the arm at the bottom of this file is what keeps it alive.
//
// WHY A BROWSER SPEC AND NOT ONLY THE SOURCE GUARD
//   `src/lib/passkeyStartErr.test.ts` pins the SHAPE of the call site (the status
//   is checked before the body is parsed). It cannot show that the sentence
//   reaches the screen: the header has to be readable in the page, `t()` has to
//   substitute, the alert has to render, and the ceremony arm needs a real
//   `navigator.credentials.get` to reject. This drives the shipped page and reads
//   the live DOM.
//
// THE START CALL IS STUBBED AT THE BROWSER LEVEL, status + headers + body exactly
// as the hop delivers them, so the gate needs nothing but the build it already
// serves.
import { test, expect, type Page } from "@playwright/test";

import { DICT, type Lang } from "../src/components/i18nDict";

/** A dictionary row in one locale, with its placeholders substituted like `t()`. */
function tr(key: keyof typeof DICT, lang: Lang, params: Record<string, string | number> = {}): string {
  let s: string = DICT[key][lang];
  for (const [k, v] of Object.entries(params)) s = s.replaceAll(`{${k}}`, String(v));
  return s;
}

/** The page's own error banner — `page.tsx`, `p[role="alert"]`. */
const ALERT = 'p[role="alert"]';

/** The passkey control (rendered only when the browser really has WebAuthn). */
const PASSKEY_BUTTON = '[data-testid="passkey-login"]';

/** The sentence the pre-fix page rendered for EVERY failure of this call. */
const BLAMES_BROWSER = DICT["login.passkeyUnsupported"].en;

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
  await page.route("**/api/auth/profile", (r) => r.fulfill(json(200, {})));
}

/** Open `/login` and click the REAL passkey button, then read its banner. */
async function clickPasskey(page: Page): Promise<string> {
  await page.goto("/login");
  const button = page.locator(PASSKEY_BUTTON);
  await expect(button).toBeVisible();
  await button.click();
  const alert = page.locator(ALERT);
  await expect(alert).toBeVisible();
  return (await alert.textContent()) ?? "";
}

test("the per-IP throttle's plain-text 429 names the window instead of blaming the browser", async ({ page }) => {
  await freshVisitor(page, "en");
  // Exactly what middleware/ratelimit.go writes over the limit: 429, a
  // delta-seconds Retry-After, and a PLAIN-TEXT body with no JSON in it.
  await page.route("**/api/auth/passkeys/login/start", (r) =>
    r.fulfill({
      status: 429,
      headers: { "content-type": "text/plain", "retry-after": "30" },
      body: "Terlalu banyak percubaan. Cuba lagi sebentar. (429)",
    }),
  );

  const text = await clickPasskey(page);

  // THE BITE: on the pre-fix page this reads "Passkey cancelled or not supported
  // by this browser." — the browser was never asked anything.
  expect(text).toBe(tr("apiErr.tooManyRequestsInSeconds", "en", { seconds: 30 }));
  expect(text).not.toContain(BLAMES_BROWSER);
});

test("a 429 with no usable Retry-After still says too many attempts", async ({ page }) => {
  await freshVisitor(page, "en");
  await page.route("**/api/auth/passkeys/login/start", (r) =>
    r.fulfill({
      status: 429,
      headers: { "content-type": "text/plain" },
      body: "Terlalu banyak percubaan. Cuba lagi sebentar. (429)",
    }),
  );

  const text = await clickPasskey(page);

  // The window is optional; the THROTTLE is not. Composing
  // `rateLimitErr(...) || login.genericError` must not turn a headerless 429
  // into "Failed. Please try again."
  expect(text).toBe(tr("apiErr.rateLimited", "en"));
  expect(text).not.toContain(BLAMES_BROWSER);
});

test("a 5xx or a proxy error page is not blamed on the browser", async ({ page }) => {
  await freshVisitor(page, "en");
  // What a reverse proxy writes when the upstream is down: HTML, no JSON, no code.
  await page.route("**/api/auth/passkeys/login/start", (r) =>
    r.fulfill({
      status: 503,
      headers: { "content-type": "text/html" },
      body: "<html><head><title>503 Service Temporarily Unavailable</title></head></html>",
    }),
  );

  const text = await clickPasskey(page);

  // The page's own generic failure line is the honest floor for a server that
  // did not answer: the browser was never asked to do anything.
  expect(text).toBe(tr("login.genericError", "en"));
  expect(text).not.toContain(BLAMES_BROWSER);
});

test("a 200 whose body carries no challenge is a server-side failure, not a browser one", async ({ page }) => {
  await freshVisitor(page, "en");
  // A start that answers 200 with nothing usable — a half-deployed API, a body
  // rewritten by something in between. Pre-fix this reached the SAME catch, by
  // way of `st.options.publicKey`, and blamed the browser.
  await page.route("**/api/auth/passkeys/login/start", (r) =>
    r.fulfill({ status: 200, headers: { "content-type": "application/json" }, body: "{}" }),
  );

  const text = await clickPasskey(page);

  expect(text).toBe(tr("login.genericError", "en"));
  expect(text).not.toContain(BLAMES_BROWSER);
});

test("a genuine ceremony refusal still says cancelled or not supported (the control)", async ({ page }) => {
  await freshVisitor(page, "en");
  // The start succeeds with a real challenge shape (go-webauthn's
  // BeginDiscoverableLogin marshals under `publicKey`), and the ceremony itself
  // rejects the way a dismissed prompt does.
  await page.route("**/api/auth/passkeys/login/start", (r) =>
    r.fulfill({
      status: 200,
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        options: {
          publicKey: {
            challenge: "AAAA",
            rpId: "127.0.0.1",
            timeout: 60000,
            userVerification: "preferred",
            allowCredentials: [],
          },
        },
        key: "probe-key",
      }),
    }),
  );
  // Installed before the page's own scripts run, so the mount-time WebAuthn
  // support check still sees a CredentialsContainer.
  await page.addInitScript(() => {
    const container = navigator.credentials;
    if (!container) return;
    container.get = () =>
      Promise.reject(new DOMException("The operation either timed out or was not allowed.", "NotAllowedError"));
  });

  const text = await clickPasskey(page);

  // The one case the sentence is true for, and the reason it stays in the
  // dictionary: a browser that was asked and declined.
  expect(text).toBe(BLAMES_BROWSER);
  expect(text).not.toBe(tr("login.genericError", "en"));
  expect(text).not.toContain("Too many attempts");
});
