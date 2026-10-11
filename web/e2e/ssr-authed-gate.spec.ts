// The SERVER-SIDE gate for the dashboard paths, asserted against the built app.
//
// Salvaged from the superseded card t_363f32e6 (its PR #1137 was closed as
// superseded by #1136/#1138) BECAUSE this is the arm nothing else covers:
// `src/lib/authedGuard.test.ts` is a source guard (the repo's `node --test`
// runner cannot load `next/server`), so the property that the SERVER sends no
// shell to a cookie-less request had no end-to-end assertion in main at all.
//
// THE DEFECT THIS PINS. Every dashboard page guards itself AFTER hydration:
// `useSession()` only learns the visitor is anonymous when the dashboard-bundle
// read 401s, and that is after the HTML has been sent. Measured on prod
// 2026-10-11 with a fresh anonymous browser: `GET https://jomform.com/profile`
// answered 200 with 21961 bytes and the account shell — "Sign out", "Passkeys",
// "Identity" — inside that HTML; /settings answered 200 with 20171 bytes. No DATA
// leaked (0 occurrences of `jf_session`, no email, no workspace name, every API
// under the page answered 401), but a crawler, a link preview or a no-JS client
// was served account-UI chrome, and nothing a page does at runtime can act before
// the first paint.
//
// WHY THIS IS AN E2E SPEC AND NOT A UNIT TEST. `src/lib/authedGuard.test.ts`
// pins the DECISION — the path list, the matcher, the redirect shape — by reading
// source, and it must, because the repo's `node --test` runner cannot load
// `next/server`. The defect here is a property of the RESPONSE: which bytes the
// server sends to a request that carries no session. So the arms below make real
// requests against the built app with redirects NOT followed, and read the
// status, the Location and the body; one arm drives the same thing the way a
// visitor would, by asking the browser for /profile and asserting where it lands.
// A source guard cannot see the difference, and a post-hydration bounce cannot
// either — that is the whole point (PR #1133's client guard is correct and
// shipped, and the shell markup still reached first paint).
//
// THE SESSION COOKIE IS OPTED OUT at file scope, deliberately. In this suite a
// spec opts INTO a session (`e2e/auth.ts`), so "no cookie" is already the
// default; the override states that rather than relying on it, and it stays
// correct if a project ever gains a default storage state. An arm that inherited
// a session would be served the page instead of the redirect and would prove
// nothing. The other direction — that a session-holder is NOT locked out — is the
// sibling `ssr-gate-signed-in.spec.ts`, which declares the session on purpose.
import { test, expect } from "@playwright/test";

// The strings the account shell carries and a signed-out response does not. They
// are asserted as ABSENCES, and they are the ones the card measured ("Sign out",
// "Passkeys", "Identity" — one occurrence each in the anonymous /profile HTML).
const SHELL_MARKERS = ["Sign out", "Passkeys", "Identity"] as const;

// The dashboard paths, at the bare path AND one level under it: the gate matches
// a path and everything below it, and `/products/abc/edit` is a real route under
// a guarded page.
const GUARDED = [
  "/profile",
  "/settings",
  "/sales",
  "/chat",
  "/forms",
  "/workspace-picker",
  "/products/abc/edit",
] as const;

// Routes a cookie-less visitor MUST keep reaching, at the same status and with
// the same body. A forgotten entry in the guard list would lock a real user out
// of a flow they cannot complete — a password reset or a magic-link landing is
// not recoverable from inside the app.
const PUBLIC = ["/", "/landing", "/login", "/docs", "/mcp", "/brand"] as const;

const REDIRECT_CODES = [301, 302, 303, 307, 308] as const;

// The public arm checks ONE marker, not the three above, and that is measured
// rather than convenient: `/docs` legitimately contains the word "Passkeys"
// (its FAQ has a "Passkeys (sign in without a password)" entry), so a
// three-marker check on the public routes reds on a page that is perfectly
// correct. "Sign out" is the string only a signed-in shell has — it is the
// control the card measured in the anonymous /profile HTML.
const SHELL_ONLY_MARKER = "Sign out";

test.use({ storageState: { cookies: [], origins: [] } });

test("an anonymous GET of a dashboard path is refused BY THE SERVER, before any JavaScript runs", async ({ request }) => {
  for (const path of GUARDED) {
    const res = await request.get(path, { maxRedirects: 0 });
    const status = res.status();
    expect(
      REDIRECT_CODES,
      `${path} answered HTTP ${status} to an anonymous request — the account shell was SERVED ` +
        `instead of a redirect to /login (this is the first-paint leak of t_363f32e6)`,
    ).toContain(status as (typeof REDIRECT_CODES)[number]);
    expect(
      res.headers()["location"],
      `${path} must send an anonymous visitor to /login, not serve the page`,
    ).toMatch(/\/login(\?|$)/);
    // The other half of the same property, and the reason the assertion is on the
    // BYTES and not only on the status: whatever a refusal is shaped like (today a
    // 307, tomorrow a rewrite), the response must not carry the shell.
    const body = await res.text();
    for (const marker of SHELL_MARKERS) {
      expect(body, `${path} served "${marker}" to a request that carries no session`).not.toContain(marker);
    }
  }
});

test("an anonymous browser asking for /profile lands on /login, not on the account shell", async ({ page }) => {
  const res = await page.goto("/profile", { waitUntil: "domcontentloaded" });

  // The navigation resolved (a redirect-followed 200 on /login), and the visitor
  // is on the login page — the page the server chose, not the one they asked for.
  expect(res?.ok(), "the redirected /profile navigation must resolve on /login").toBe(true);
  await expect(page).toHaveURL(/\/login/);

  // ...and nothing of the account shell is on screen. This is the arm that would
  // have caught the defect as a VISITOR sees it: the pre-fix build painted the
  // shell (nav + "Sign out") before the client-side guard could bounce.
  for (const marker of SHELL_MARKERS) {
    await expect(
      page.getByText(marker, { exact: true }),
      `"${marker}" is on screen for an anonymous visitor of /profile`,
    ).toHaveCount(0);
  }
});

test("the public routes are NOT gated — a cookie-less visitor keeps their status and body", async ({ request }) => {
  for (const path of PUBLIC) {
    const res = await request.get(path, { maxRedirects: 0 });
    expect(
      res.status(),
      `${path} answered HTTP ${res.status()} to a cookie-less visitor — a public route must stay reachable ` +
        `(the guard is a DENY list of dashboard paths, never a catch-all)`,
    ).toBe(200);
    const body = await res.text();
    expect(
      body,
      `${path} served "${SHELL_ONLY_MARKER}" — a public route must not render the account shell`,
    ).not.toContain(SHELL_ONLY_MARKER);
  }
});

test("a public path shape outside the guard list is not collaterally gated", async ({ request }) => {
  // The tenant hosts (*.jomform.com) and the /{slug} form routes are served by
  // the Go API directly (measured 2026-10-11: test-wan.jomform.com/test answers
  // with `x-request-id` and no `x-nextjs-*`, while the dashboard host answers
  // `x-powered-by: Next.js`), so the web app never renders them and this gate can
  // never be the thing that refuses them. What CAN be checked here is the shape:
  // one path segment that is not a dashboard route must not be answered with a
  // /login redirect — the guard must not have widened into "anything unknown".
  const res = await request.get("/some-form-slug", { maxRedirects: 0 });
  expect(
    res.headers()["location"] ?? "",
    "/some-form-slug was redirected — the guard has widened past the dashboard paths",
  ).not.toContain("/login");
  expect(REDIRECT_CODES, `a bare slug answered ${res.status()}`).not.toContain(
    res.status() as (typeof REDIRECT_CODES)[number],
  );
});
