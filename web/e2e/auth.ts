// Declaring a SIGNED-IN visitor for a spec that visits a dashboard path.
//
// WHY THIS EXISTS. The dashboard pages are gated on the SERVER now
// (`web/src/middleware.ts`): a request that carries no `jf_session` cookie is
// redirected to /login BEFORE the page renders, because an anonymous visitor
// used to receive the account shell as static markup and only lose it after
// hydration. Before that gate, a spec could visit /products or /profile with no
// session at all and still find the page — so several a11y specs did exactly
// that, which means they were relying on the bug (measured 2026-10-11: 41 a11y
// tests failed the first time the gate ran, every one of them because the page
// had become /login).
//
// A spec that visits one of those paths therefore has to state the session it
// means, exactly as `e2e/session-bootstrap-anon.spec.ts` already did before this
// file existed. The value is a stub on purpose: the gate reads PRESENCE only and
// never inspects the value (validity is the API's business — see
// `web/src/lib/sessionCookie.ts`), and these specs stub the API themselves.
//
// A spec that means to test the SIGNED-OUT path must clear it —
// `page.context().clearCookies()` — as the failure-path specs already do
// (settings-toast-failure, feedback-notify-marker, …). Nothing here is implicit:
// the cookie is added only where a spec asks for it — and that is now the ONLY
// way to stay anonymous on a dashboard path, which is the point: "no cookie"
// means /login in the suite exactly as it does in production.
//
// THE ORIGIN IS OWNED HERE, not in playwright.config.ts, so the cookie's origin
// and the app's baseURL cannot drift: a cookie set for the wrong origin is
// silently dropped by the browser, and the symptom would be indistinguishable
// from the gate not working.
import type { Page } from "@playwright/test";

export const E2E_PORT = Number(process.env.A11Y_PORT || 3100);
export const E2E_ORIGIN = `http://127.0.0.1:${E2E_PORT}`;

/**
 * Give this page a session cookie, so the server gate renders the dashboard.
 *
 * `baseURL` is the Playwright fixture where a spec has it; the fallback is the
 * origin the suite is actually driving (E2E_ORIGIN).
 */
export async function signIn(page: Page, baseURL?: string): Promise<void> {
  await page.context().addCookies([{ name: "jf_session", value: "e2e-test", url: baseURL || E2E_ORIGIN }]);
}
