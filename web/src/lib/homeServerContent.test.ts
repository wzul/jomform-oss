// Guard for the crawler-visible HTML of the home page (`/`).
//
// THE BUG: `/` served a shell with no content. `app/page.tsx` rendered
// `<Dashboard />`, which is a client component ("use client") that resolves the
// session from a browser fetch, so on the server it rendered only its loading
// shell and the response was, in full:
//
//     <main class="flex min-h-screen items-center justify-center text-slate-400">…</main>
//
// No `<h1>`, no hero copy, no feature list. Measured on production and on a
// local production build (`curl` byte-for-byte 10264, `grep -c '<h1'` = 0).
// The landing markup lived in `app/landing/page.tsx`, which the dashboard only
// reached from the browser (`if (anon) return <Landing />`), so a crawler that
// does not execute the client bundle — search, link previews, AI readers — saw
// an empty page. The `<h1>` in `/landing`'s output was never reachable from `/`.
//
// THE FIX the guard protects: the home page decides from the session cookie on
// the SERVER and renders the landing markup for a visitor without one, so the
// crawler's response carries the `<h1>` and the brand. A visitor with a cookie
// still gets `<Dashboard />`, unchanged.
//
// This repo has no JSX/DOM test runner (no vitest/jest — see src/lib/i18n.test.ts,
// src/lib/adminResponsive.test.ts), so this guard is a source scan. That is the
// right granularity for the defect: the bug was WHICH COMPONENT THE SERVER
// RENDERED, which is a property of these files. The measured proof that the
// crawler's bytes changed is a `curl` against a local production build, which no
// test runner in this repo can do — see the PR description.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
//
// WHAT IS PINNED, AND WHY EACH ONE MATTERS
//   1. The home page is still a server component and still renders the landing
//      markup on the no-cookie branch. This is the fix itself; a revert (back to
//      a bare `<Dashboard />`) fails here rather than silently restoring the
//      empty page.
//   2. The branch is decided from the session cookie, read server-side via
//      `cookies()`, and the cookie NAME is pinned to the Go constant the API
//      writes (api/internal/httpapi/auth.go). A rename on either side would
//      serve the marketing page to signed-in users, silently — the guard reads
//      both sources and compares them.
//   3. The cookie-bearing branch still renders <Dashboard />, so the
//      authenticated path (dashboard-bundle, workspace-scoped fetch,
//      ?workspace= handling) is untouched.
//   4. The landing component really does carry the content: exactly one <h1>
//      inside the JSX it returns, the brand lockup, and the hero strings from
//      the dictionary. A guard that only checked the branch would pass on a
//      landing page whose h1 had been deleted.
//   5. Nothing may opt this route out of server rendering: no `use client` on
//      app/page.tsx, no `next/dynamic` with `ssr: false`, no `force-static`. Any
//      of those returns the crawler to the bug while every other assertion here
//      still passes.
//
// WHAT IT DOES NOT COVER (stated honestly)
//   * It cannot execute React, so it does not itself observe the served bytes.
//     The `curl` evidence is in the PR.
//   * It says nothing about whether a search engine RANKS the page, or what
//     Google prints as the site name. See siteName.test.ts for that half.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { SESSION_COOKIE } from "./sessionCookie.ts";
import { DICT, LANGS } from "../components/i18nDict.ts";
import { SITE_NAME } from "./siteName.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

const home = read("../app/page.tsx");
const landing = read("../app/landing/page.tsx");
// The Go side of the session cookie. Read cross-language on purpose (the same
// idiom as adminGrowth.test.ts / salesRows.test.ts): the web app now decides
// what to render from this cookie, so the two constants are one value living in
// two languages and a literal copy here would let them drift apart.
const goAuth = read("../../../api/internal/httpapi/auth.go");

/** The JSX a component returns — from its top-level `return (` to the last `</main>`. */
function returnedJsx(src: string): string {
  const start = src.indexOf("  return (");
  assert.ok(start > 0, "could not locate the component's top-level `return (` — re-anchor this guard");
  const end = src.lastIndexOf("</main>");
  assert.ok(end > start, "could not locate `</main>` after the return — re-anchor this guard");
  return src.slice(start, end);
}

test("the guard is looking at real files (it cannot pass vacuously)", () => {
  // If any of these move, fail here instead of asserting against nothing — the
  // failure mode the repo has been bitten by before.
  assert.ok(home.includes("export const metadata"), "app/page.tsx is not the home page source");
  assert.ok(
    home.includes("export default async function Page"),
    "app/page.tsx no longer exports an async Page — the guard's anchors moved",
  );
  assert.ok(landing.includes("export default function Landing"), "app/landing/page.tsx is not the landing source");
  assert.ok(goAuth.includes("const sessionCookie"), "could not find the API's session cookie constant");
  assert.ok(SESSION_COOKIE.length > 0, "the shared session cookie name is empty");
  assert.ok(
    returnedJsx(landing).length > 1000,
    "the landing's returned JSX slice is implausibly small — the guard's slice is measuring the wrong region",
  );
});

test("the home page is a server component that renders the landing markup without a session", () => {
  // A `use client` directive would move the decision into the browser bundle,
  // which is exactly where the crawler cannot see it.
  assert.doesNotMatch(home, /^\s*"use client";/m, "app/page.tsx must stay a server component");

  // The fix, anchored on the branch rather than on the word "Landing": a bare
  // `<Dashboard />` return is the reverted bug.
  assert.match(
    home,
    /hasSession\s*\?\s*<Dashboard\s*\/>\s*:\s*<Landing\s*\/>/,
    "app/page.tsx must render <Landing /> for a visitor without a session and <Dashboard /> with one — " +
      "a bare `<Dashboard />` is the original bug: the server renders only the client loading shell, " +
      "so `/` ships no <h1> to a crawler",
  );
});

test("the branch is decided server-side from the session cookie", () => {
  // `cookies()` is the server-only Next API. Reading the session any other way
  // (localStorage, a fetch, `document.cookie`) happens after the HTML is
  // written and cannot put markup in the crawler's response.
  assert.match(home, /\bcookies\(\)/, "app/page.tsx must read the session cookie with Next's server-side `cookies()`");
  assert.match(
    home,
    /cookieStore\.has\(SESSION_COOKIE\)/,
    "the home page must test PRESENCE of the shared session cookie name, not a hard-coded string",
  );
  assert.doesNotMatch(home, /"jf_session"/, "the cookie name must come from SESSION_COOKIE, not a literal");
  assert.ok(home.includes('from "../lib/sessionCookie"'), "app/page.tsx no longer imports the shared cookie name");
});

test("the shared session cookie name is the one the API writes", () => {
  // Silent failure this prevents: the API renames the cookie and the web app
  // keeps looking for the old one, so every signed-in visitor is served the
  // marketing landing page instead of their dashboard — no error anywhere.
  const m = goAuth.match(/const sessionCookie\s*=\s*"([^"]+)"/);
  assert.ok(m, "could not read the API's session cookie value from api/internal/httpapi/auth.go");
  assert.equal(
    SESSION_COOKIE,
    m[1],
    `the web app reads cookie "${SESSION_COOKIE}" but the API writes "${m[1]}" — a signed-in ` +
      `visitor would be treated as anonymous and served the landing page`,
  );
});

test("the cookie-bearing branch keeps the existing dashboard behaviour", () => {
  // The authenticated path must not change: <Dashboard /> still resolves the
  // real session from the dashboard-bundle fetch and still falls back to the
  // landing page itself on a 401 / wedge timeout.
  const dashboard = read("../app/dashboard-client.tsx");
  assert.match(
    dashboard,
    /if \(anon\) return <Landing \/>;/,
    "dashboard-client.tsx must keep its own anon fallback — an expired or revoked session (cookie present, " +
      "bundle 401) relies on it now that the server cannot tell the two apart",
  );
  assert.ok(
    home.includes('from "./dashboard-client"'),
    "app/page.tsx no longer renders the dashboard component",
  );
});

test("the landing markup the crawler receives really carries the h1, the brand and the copy", () => {
  const jsx = returnedJsx(landing);

  // Exactly one h1 in the file, and it is inside the returned JSX — so it is
  // not a second copy somewhere else that never renders.
  const h1s = landing.match(/<h1\b/g) ?? [];
  assert.equal(h1s.length, 1, `expected exactly one <h1> in the landing markup, found ${h1s.length}`);
  assert.match(jsx, /<h1\b/, "the landing's returned JSX has no <h1> — this is the empty-crawler-HTML bug");

  // The h1 must be composed from the hero dictionary keys, not a hard-coded
  // string, and those keys must exist in all five locales.
  const h1 = jsx.slice(jsx.indexOf("<h1"));
  const h1Tag = h1.slice(0, h1.indexOf("</h1>") + 5);
  assert.match(h1Tag, /t\("land\.hero1"\)/, "the landing <h1> no longer renders land.hero1");
  assert.match(h1Tag, /t\("land\.hero2"\)/, "the landing <h1> no longer renders land.hero2");
  for (const key of ["land.hero1", "land.hero2"] as const) {
    for (const lang of LANGS) {
      const v = DICT[key]?.[lang];
      assert.ok(
        typeof v === "string" && v.trim() !== "",
        `${key}.${lang} is missing — the h1 would render empty in that locale`,
      );
    }
  }

  // The brand lockup, which is what a crawler reads as the page's name.
  assert.match(
    jsx,
    /Jom<span[^>]*>Form<\/span>/,
    `the landing nav lockup no longer renders the brand ${SITE_NAME}`,
  );
  // Body copy, so the page is not just a heading.
  assert.match(jsx, /t\("land\.sub"\)/, "the landing hero copy (land.sub) is gone");
  assert.match(jsx, /t\("land\.badge"\)/, "the landing badge copy (land.badge) is gone");

  // The h1 is not gated behind a browser-only check: `typeof window` is the
  // idiom that silently renders nothing on the server.
  assert.doesNotMatch(
    jsx,
    /typeof window/,
    "the landing's returned JSX is gated on `typeof window`, so it renders nothing on the server",
  );
});

test("nothing opts the home route out of server rendering", () => {
  // Each of these returns `/` to the reported bug while every assertion above
  // still passes: the markup would exist in the repo and never reach a crawler.
  assert.doesNotMatch(
    home,
    /from\s+["']next\/dynamic["']|ssr:\s*false/,
    "the home page must not mount the landing through next/dynamic with ssr:false — that ships an empty server response again",
  );
  assert.doesNotMatch(
    home,
    /dynamic\s*=\s*["']force-static["']/,
    "app/page.tsx must not be force-static: the session cookie is read per request",
  );
  const layout = read("../app/layout.tsx");
  assert.doesNotMatch(
    layout,
    /ssr:\s*false/,
    "app/layout.tsx must not disable SSR for the tree containing the home page",
  );
});
