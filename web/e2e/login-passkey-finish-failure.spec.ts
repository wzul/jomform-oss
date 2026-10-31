// Regression guard for t_3c513380 — a THROTTLED or failed passkey FINISH on
// /login must not be reported as "Invalid passkey."
//
// THE DEFECT, MEASURED ON THE BUILT PAGE BEFORE THIS FIX
//   `passkeyLogin()` (web/src/app/login/page.tsx) read the finish call as
//
//     const fin = await fetch("/api/auth/passkeys/login/finish", { … });
//     if (fin.ok) { window.location.href = await resolvePostLogin(next); return; }
//     setError(apiErr(await fin.json().catch(() => ({})), t("login.passkeyInvalid"), t));
//
//   `fin.status` was never read. That endpoint sits behind the SAME per-IP
//   limiter as the start call (`api/internal/httpapi/rest.go`,
//   `POST /v1/auth/passkeys/login/finish`), and `middleware/ratelimit.go` writes a
//   429 as `http.Error` PLAIN TEXT plus `Retry-After`. So `fin.json()` threw on
//   the plain-text body, `.catch(() => ({}))` swallowed it and `apiErr({}, …)`
//   rendered `login.passkeyInvalid`: the visitor was told their passkey was WRONG
//   when the server had only refused the call, and the throttle's window was
//   dropped with the same throw. A 5xx or a proxy error page took the identical
//   path (any non-JSON body throws).
//
// WHAT EACH ANSWER MUST NOW SAY
//   * 429 -> the throttle sentence from `lib/rateLimitErr.ts` (card t_e50a9680),
//     naming the window the server sent in `Retry-After`; a 429 without a usable
//     header still says "too many attempts";
//   * 400 / 401 -> `login.passkeyInvalid`: the only two statuses with which
//     `passkeyLoginFinish` (api/internal/httpapi/passkey.go) really rejects the
//     ASSERTION, so a wrong passkey is what happened;
//   * anything else non-2xx (403 from the WebAuthn origin check, 404, a 5xx, a
//     proxy error page) -> the page's own generic failure line. The server never
//     judged the passkey, so nothing may call it invalid.
//
// WHY A BROWSER SPEC AND NOT ONLY THE SOURCE GUARD
//   `src/lib/passkeyFinishErr.test.ts` pins the SHAPE of the call site. It cannot
//   show that the sentence reaches the screen: the header has to be readable in
//   the page, `t()` has to substitute, the alert has to render, and the FINISH
//   call has to be reached at all. This drives the shipped page and reads the
//   live DOM.
//
// REACHING THE FINISH CALL: the start call AND the WebAuthn ceremony are stubbed
// at the browser boundary — a start answer with a real challenge shape, and a
// `navigator.credentials.get` that resolves with an assertion-shaped credential —
// so no authenticator and no API are needed. Every arm asserts the finish route
// was actually hit, so an arm cannot pass by never getting there.
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

/** What the pre-fix page rendered for EVERY refusal of the finish call. */
const BLAMES_PASSKEY = DICT["login.passkeyInvalid"].en;

/**
 * A start answer with a real challenge shape (go-webauthn's BeginDiscoverableLogin
 * marshals under `publicKey`) plus the session key the finish body must carry.
 */
function startOK() {
  return {
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
  };
}

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
  await page.route("**/api/auth/passkeys/login/start", (r) => r.fulfill(json(200, startOK())));

  // The ceremony itself. Headless Chromium has no authenticator, and
  // `navigator.credentials` may not exist at all on a plain-http origin, so the
  // container is supplied when absent — installed before the page's own scripts
  // run, which is also what the mount-time WebAuthn support check reads.
  await page.addInitScript(() => {
    const assertion = {
      rawId: new Uint8Array([1, 2, 3, 4]).buffer,
      type: "public-key",
      response: {
        authenticatorData: new Uint8Array([5, 6, 7]).buffer,
        clientDataJSON: new Uint8Array([8, 9, 10]).buffer,
        signature: new Uint8Array([11, 12, 13]).buffer,
        userHandle: null,
      },
    };
    const get = () => Promise.resolve(assertion);
    const nav = navigator as unknown as { credentials?: { get: unknown } };
    try {
      if (nav.credentials) nav.credentials.get = get;
      else Object.defineProperty(navigator, "credentials", { value: { get }, configurable: true });
    } catch {
      /* a read-only container is not reachable from here either */
    }
  });
}

/**
 * Open `/login`, click the REAL passkey button, and read the banner the FINISH
 * call produced. `finishHit` is asserted, so an arm that never reached the call
 * (a start that failed, a ceremony that answered a browser sentence) cannot pass
 * as a statement about the finish refusal.
 */
async function clickThroughToFinish(
  page: Page,
  finish: { status: number; headers: Record<string, string>; body: string },
): Promise<string> {
  let finishHit = false;
  await page.route("**/api/auth/passkeys/login/finish", (r) => {
    finishHit = true;
    return r.fulfill(finish);
  });

  await page.goto("/login");
  const button = page.locator(PASSKEY_BUTTON);
  await expect(button).toBeVisible();
  await button.click();
  const alert = page.locator(ALERT);
  await expect(alert).toBeVisible();
  const text = (await alert.textContent()) ?? "";
  expect(finishHit, "the arm never reached POST /api/auth/passkeys/login/finish").toBe(true);
  return text;
}

test("the per-IP throttle's plain-text 429 on the FINISH call names the window", async ({ page }) => {
  await freshVisitor(page, "en");
  // Exactly what middleware/ratelimit.go writes over the limit: 429, a
  // delta-seconds Retry-After, and a PLAIN-TEXT body with no JSON in it.
  const text = await clickThroughToFinish(page, {
    status: 429,
    headers: { "content-type": "text/plain", "retry-after": "30" },
    body: "Terlalu banyak percubaan. Cuba lagi sebentar. (429)",
  });

  // THE BITE: on the pre-fix page this reads "Invalid passkey." — the server never
  // judged the passkey, it refused the call, and the 30-second window is on wire.
  expect(text).toBe(tr("apiErr.tooManyRequestsInSeconds", "en", { seconds: 30 }));
  expect(text).not.toBe(BLAMES_PASSKEY);
  expect(text).not.toContain("Invalid passkey");
});

test("a FINISH 429 with no usable Retry-After still says too many attempts", async ({ page }) => {
  await freshVisitor(page, "en");
  const text = await clickThroughToFinish(page, {
    status: 429,
    headers: { "content-type": "text/plain" },
    body: "Terlalu banyak percubaan. Cuba lagi sebentar. (429)",
  });

  // The window is optional; the THROTTLE is not. Composing
  // `rateLimitErr(...) || login.genericError` must not turn a headerless 429 into
  // a generic line, and it must never fall back to the passkey sentence.
  expect(text).toBe(tr("apiErr.rateLimited", "en"));
  expect(text).not.toBe(BLAMES_PASSKEY);
});

test("a 5xx or a proxy error page on the FINISH call is not blamed on the passkey", async ({ page }) => {
  await freshVisitor(page, "en");
  // What a reverse proxy writes when the upstream is down: HTML, no JSON, no code.
  const text = await clickThroughToFinish(page, {
    status: 503,
    headers: { "content-type": "text/html" },
    body: "<html><head><title>503 Service Temporarily Unavailable</title></head></html>",
  });

  // The server never judged the assertion, so the honest sentence is the page's
  // own failure line — the pre-fix page said the passkey was invalid.
  expect(text).toBe(tr("login.genericError", "en"));
  expect(text).not.toBe(BLAMES_PASSKEY);
});

test("a 403 from the WebAuthn origin check is a server fault, not an invalid passkey", async ({ page }) => {
  await freshVisitor(page, "en");
  // writeWebAuthnUnavailable: `errPasskeyOriginOutsideApp` → 403 with prose. A
  // misconfigured RP origin rejects EVERY credential, so the passkey sentence
  // would be a lie the visitor cannot act on.
  const text = await clickThroughToFinish(page, {
    status: 403,
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ error: "passkey origin outside app", code: "request.forbidden" }),
  });

  expect(text).toBe(tr("login.genericError", "en"));
  expect(text).not.toBe(BLAMES_PASSKEY);
});

test("a REAL assertion rejection still says the passkey was invalid (the control)", async ({ page }) => {
  await freshVisitor(page, "en");
  // What passkeyLoginFinish answers when the library refuses the assertion: 401,
  // the class code `auth.required`, and the server's Malay prose — which must
  // never reach the screen.
  const text = await clickThroughToFinish(page, {
    status: 401,
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ error: "passkey tidak sah", code: "auth.required" }),
  });

  // The one case the sentence is true for, and the reason it stays in the
  // dictionary. Proving it survives keeps this fix from being "always generic".
  expect(text).toBe(BLAMES_PASSKEY);
  expect(text).not.toBe(tr("login.genericError", "en"));
  expect(text).not.toContain("tidak sah");
});
