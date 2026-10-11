// The OTHER direction of the SSR gate (salvaged from card t_363f32e6): a visitor who HOLDS the
// session cookie is served the page, unchanged.
//
// WHY THIS ARM IS NOT OPTIONAL. The gate is a redirect for a request that carries
// no cookie, and a gate that is one line away from locking real users out of
// their own dashboard is a gate that has to be pinned in both directions — the
// same reason `authedGuard.test.ts` pins the redirect shape as well as the path
// list. This file also pins the plumbing that makes every authenticated spec in
// this gate possible: the cookie these specs set carries the SHARED name, so a
// rename on the Go side (or in `lib/sessionCookie.ts`) cannot leave this gate's
// browsers anonymously browsing without saying so.
//
// This file DECLARES the session it means (`e2e/auth.ts`); the sibling
// `e2e/ssr-authed-gate.spec.ts` is the file that stays anonymous.
import { test, expect } from "@playwright/test";

import { SESSION_COOKIE } from "../src/lib/sessionCookie";
import { signIn } from "./auth";

test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});

const SHELL_MARKERS = ["Sign out", "Passkeys", "Identity"] as const;

test("the gate's browsers carry the session cookie the server reads", async ({ context }) => {
  const names = (await context.cookies()).map((c) => c.name);
  expect(
    names,
    `this gate's browser carries no ${SESSION_COOKIE} — every authenticated spec would be answered ` +
      `with a redirect to /login and would fail on a page that never rendered`,
  ).toContain(SESSION_COOKIE);
});

test("a signed-in GET of a dashboard path is served the account shell, and is not redirected", async ({ context }) => {
  // Measured on the built app with a stub cookie: /profile is 21961 bytes with
  // "Sign out"/"Passkeys"/"Identity" once each; /settings is 20171 bytes with
  // "Sign out" only (it has no Passkeys or Identity panel). Those are the same
  // bytes prod served anonymously before this gate existed — i.e. the gate
  // changed WHO is served them, never what they are.
  const expected: Array<{ path: string; markers: readonly string[] }> = [
    { path: "/profile", markers: ["Sign out", "Passkeys", "Identity"] },
    { path: "/settings", markers: ["Sign out"] },
  ];
  for (const { path, markers } of expected) {
    const res = await context.request.get(path, { maxRedirects: 0 });
    expect(
      res.status(),
      `${path} answered ${res.status()} to a request that carries a session — the gate must never ` +
        `redirect a visitor who holds the cookie (presence only, never validity)`,
    ).toBe(200);
    const body = await res.text();
    for (const marker of markers) {
      expect(body, `${path} was served without its own shell markup ("${marker}" absent)`).toContain(marker);
    }
  }
});

test("a signed-in browser is served /profile at first paint (the page the user asked for)", async ({ page }) => {
  // `waitUntil: "commit"` reads the DOCUMENT the server sent, before the page's
  // own client-side session bootstrap can bounce an expired session — which is
  // exactly the layer this card is about. The API is not stubbed here, so the
  // client may legitimately navigate afterwards; the assertion is already made.
  const res = await page.goto("/profile", { waitUntil: "commit" });
  expect(res?.status(), "a session-holder must get the page, not a redirect").toBe(200);
  const html = await res!.text();
  for (const marker of SHELL_MARKERS) {
    expect(html, `the signed-in first paint of /profile lost its shell markup ("${marker}" absent)`).toContain(marker);
  }
});
