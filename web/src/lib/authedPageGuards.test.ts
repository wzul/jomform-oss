// Every page inside the signed-in shell must bounce an anonymous visitor.
//
// WHAT CAN BREAK HERE, and why a source scan is the right guard:
//
//   The session is a CLIENT fetch. `useSession()` starts with an empty state and
//   only learns the visitor is anonymous when the dashboard-bundle request comes
//   back 401 — after the first paint. So the guard cannot live in a layout, and
//   it is not a server redirect: it is a per-page effect that every authed page
//   has to carry.
//
//   That makes it a convention with no enforcement, and conventions drift. When
//   /profile shipped without the bounce (measured 2026-10-11: `GET
//   https://jomform.com/profile` answered 200 to an ANONYMOUS request with the
//   shell — nav, "Sign out", the Profile/Passkeys tab strip — in the HTML),
//   nothing failed: no test, no type, no build. The page simply stayed on screen
//   for a signed-out visitor while all fifteen sibling pages bounced. A page
//   that shows the account UI to a stranger and then 401s every request under it
//   reads as "logged in but broken".
//
//   So this test pins the convention across the whole app directory instead of
//   the one page that broke: every page.tsx that consumes `useSession()` must
//   redirect to /landing when the session resolves anonymous. Adding a new
//   authed page without the bounce now fails HERE, by name, instead of shipping
//   a second /profile.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { join, dirname, relative } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const APP = join(here, "..", "app");

/** Every page.tsx under src/app, as paths relative to src/app. */
function pageFiles(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    if (name === "node_modules" || name.startsWith(".")) continue;
    const p = join(dir, name);
    if (statSync(p).isDirectory()) {
      pageFiles(p, out);
    } else if (name === "page.tsx") {
      out.push(p);
    }
  }
  return out;
}

test("every page that consumes useSession bounces an anonymous visitor to /landing", () => {
  const pages = pageFiles(APP);
  assert.ok(pages.length > 10, `expected to find the app's pages, found ${pages.length}`);

  const authed: string[] = [];
  const missing: string[] = [];

  for (const p of pages) {
    const src = readFileSync(p, "utf8");
    if (!/useSession\s*\(/.test(src)) continue; // not a session-page (e.g. /mcp, /builder)
    authed.push(p);
    // The convention, exactly as the fifteen sibling pages write it.
    if (!/router\.replace\(\s*"\/landing"\s*\)/.test(src)) {
      missing.push(relative(APP, p));
    }
  }

  assert.ok(authed.length > 10, `expected the authed pages, found ${authed.length}`);
  assert.deepEqual(
    missing,
    [],
    `these pages consume useSession but never redirect an anonymous visitor to /landing:\n  ` +
      missing.join("\n  ") +
      `\n\nAdd the sibling guard:\n` +
      `  useEffect(() => {\n    if (anon) router.replace("/landing");\n  }, [anon, router]);`,
  );
});

test("/profile also sends a dead session on its own tab loads to /login", () => {
  const src = readFileSync(join(APP, "profile", "page.tsx"), "utf8");
  assert.match(
    src,
    /const goToLogin = useCallback\(\(\) => \{\s*\n\s*router\.replace\(\s*"\/login"\s*\);/,
    "profile must define goToLogin() the way Shell.signOut() and the /sales, /media & /settings pages do",
  );
  // The passkeys/social/agents/trust fetches run through loadAuthed; an
  // unauthenticated answer there must leave the page rather than paint an empty
  // tab that reads as "you have none".
  assert.match(src, /loadAuthed</, "profile's tab loads must go through loadAuthed");
  assert.match(
    src,
    /if \(classifyLoadError\(e\)\.kind === "unauth"\) goToLogin\(\);/,
    "loadAuthed must send an unauthenticated failure to goToLogin",
  );
  assert.ok(
    !/fetch\("\/api\/auth\/passkeys"\)/.test(src),
    "the raw passkeys fetch must be gone — it swallowed the 401 and painted an empty list",
  );
});
