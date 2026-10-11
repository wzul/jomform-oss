// Card t_788c06ce: the SSR gate's `?next=` must carry the origin the VISITOR
// reached the app on — not the server's own internal origin.
//
// THE DEFECT (measured 2026-10-11 on the built standalone artifact, tree
// `4b04a39`): `web/src/middleware.ts` built the redirect from `req.nextUrl.origin`,
// i.e. the origin the Next SERVER thinks it is on, and `loginRedirectUrl` put that
// value into `?next=`. With `Host` and `X-Forwarded-Host` both spelled
// `jomform.com` and `X-Forwarded-Proto: https`, the response was
//
//     location: /login?next=https%3A%2F%2Flocalhost%3A3210%2Fprofile
//
// `web/src/app/login/page.tsx`'s `readNext()` requires `next` to be an ABSOLUTE,
// same-origin http(s) URL whose origin is the BROWSER's own origin (or
// NEXT_PUBLIC_OAUTH_BASE), and returns "" for anything else — silently. So the
// visitor was sent to /login and then dropped on the dashboard root after signing
// in, instead of being returned to the page they asked for. On prod the web
// container is dialled as `http://jomform-web-1:3000` (`deploy/vps/traefik-jomform.yml`,
// service `jf-web`), so the value handed to the login page was the container's
// internal origin.
//
// WHY THESE ARMS ARE HERE AND NOT IN A UNIT TEST. `src/lib/authedGuard.test.ts`
// pins the DECISION by reading source, and it feeds `loginRedirectUrl` a
// hand-written origin — which is exactly why it could not see this: the defect is
// not in how the URL is built, it is in WHICH origin reaches it. That is a
// property of a real response from the built app, so the arms below make real
// requests and read the actual value out of the Location header and the browser's
// own URL bar.
//
// WHY THE ORIGIN IS NOT THE HOST THE SPEC CONNECTS TO. The suite drives
// `http://127.0.0.1:<port>` (e2e/auth.ts) while the server's own `nextUrl.origin`
// is `http://localhost:<port>` — one spelling apart, and the same defect shape as
// prod's `jomform-web-1:3000` vs `jomform.com`. A browser origin is compared
// EXACTLY (`readNext` uses a Set of string origins), so the two spellings are not
// interchangeable and the difference is visible here.
import { test, expect } from "@playwright/test";

// A guarded path, from the list in src/lib/authedGuard.ts. /profile is the route
// the card measured on prod.
const GUARDED = "/profile";

const REDIRECT_CODES = [301, 302, 303, 307, 308] as const;

/** The `next` value of a redirect response, or null when there is none. */
function nextOf(location: string | undefined, base: string): string | null {
  if (!location) return null;
  return new URL(location, base).searchParams.get("next");
}

test("an anonymous dashboard request hands /login a ?next= on the VISITOR's own origin", async ({
  request,
  baseURL,
}) => {
  const visitor = new URL(baseURL!);
  const res = await request.get(GUARDED, { maxRedirects: 0 });

  expect(
    REDIRECT_CODES,
    `${GUARDED} answered HTTP ${res.status()} to a cookie-less request — this spec is about the ` +
      `redirect's own ?next= value, so it must be a redirect`,
  ).toContain(res.status() as (typeof REDIRECT_CODES)[number]);

  const location = res.headers()["location"];
  const next = nextOf(location, baseURL!);
  expect(next, `${GUARDED} was redirected without a ?next= (location=${location})`).toBeTruthy();

  const parsed = new URL(next!);
  expect(
    parsed.origin,
    `the SSR gate handed /login next=${next}, whose origin is the SERVER's own spelling ` +
      `(${parsed.origin}) and not the visitor's (${visitor.origin}). app/login/page.tsx's readNext() ` +
      `drops a value whose origin is not the browser's own, so the visitor is NOT returned to ` +
      `${GUARDED} after signing in (card t_788c06ce)`,
  ).toBe(visitor.origin);
  // ...and the value still names the page they asked for.
  expect(parsed.pathname + parsed.search).toBe(GUARDED);
  console.log(`OBSERVED next-origin response location=${location}`);
});

test("the same value survives the browser's own navigation", async ({ page }) => {
  // The response arm above reads the header; this one drives the thing the login
  // page will actually read — `window.location.search` — so the property is
  // asserted where `readNext()` reads it, in the visitor's own URL bar.
  await page.goto(GUARDED, { waitUntil: "domcontentloaded" });
  await expect(page).toHaveURL(/\/login/);

  const observed = await page.evaluate(() => ({
    next: new URLSearchParams(window.location.search).get("next"),
    origin: window.location.origin,
  }));

  expect(observed.next, `the browser landed on /login with no ?next= (url=${page.url()})`).toBeTruthy();
  expect(
    new URL(observed.next!).origin,
    `readNext() compares origins EXACTLY: next=${observed.next} vs the browser's ` +
      `${observed.origin} — the visitor asked for ${GUARDED} and would be dropped on the root instead`,
  ).toBe(observed.origin);
  console.log(`OBSERVED next-origin browser url=${page.url()}`);
});

test("CONTROL: the Location stays RELATIVE, so a forged host can never move the redirect", async ({
  request,
  baseURL,
}) => {
  // The other half of the hazard, and the reason a wrong or forged resolution is
  // a degradation rather than a hole. Only the PATH reaches `Location` (the
  // redirect target is built on the server's own origin), so no value of Host /
  // X-Forwarded-Host can ask the browser to leave the app — a wrong origin can
  // only end up inside `?next=`, where the login page drops it. This arm forges
  // the header outright and pins both halves.
  //
  // Measured on the fixed build (standalone, anonymous GET /profile):
  //   -H 'X-Forwarded-Host: evil.example' -H 'X-Forwarded-Proto: https'
  //     -> location: /login?next=https%3A%2F%2Fevil.example%2Fprofile
  const res = await request.get(GUARDED, {
    maxRedirects: 0,
    headers: { "x-forwarded-host": "evil.example", "x-forwarded-proto": "https" },
  });

  const location = res.headers()["location"] ?? "";
  expect(
    location.startsWith("/login"),
    `a forged X-Forwarded-Host produced location=${location} — anything but a relative /login path ` +
      `would ask the browser to navigate somewhere the app does not control (open redirect)`,
  ).toBe(true);
  // ...and the forgery really did reach the app, i.e. this control is not
  // vacuous: the forged host is visible ONLY in the value the login page
  // validates and discards. If this fails because the header never arrived, the
  // arm above it is measuring nothing either, and that has to be visible.
  const next = nextOf(location, baseURL!);
  expect(
    next && new URL(next).origin,
    `the forged X-Forwarded-Host never reached the app (location=${location}), so this control is ` +
      `measuring nothing — the value the login page would discard must be the forged one here`,
  ).toBe("https://evil.example");
  console.log(`OBSERVED next-origin forged-host location=${location}`);
});
