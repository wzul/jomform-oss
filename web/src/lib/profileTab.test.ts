// Which Profile tab a link opens — the device-flow email entry point
// (#1703 follow-up).
//
// THE DEFECT THIS GUARD EXISTS FOR. The consent email links to
// `/profile?tab=mcp&user_code=…`. The page honoured `?tab=` only for
// `connected` and otherwise used `localStorage.jf_profile_tab`, defaulting to
// `identity` — and the Approve/Deny card renders only under `tab === "mcp"`.
// So every FIRST-TIME user (nothing stored yet, i.e. every user this feature is
// for) landed on Identity with no code field, and the page then PERSISTED
// `identity`, so the same link did not recover on a second click either.
// Measured on prod v1.37.0 @ 39a541648 with cleared storage: "?tab=mcp&
// user_code=KX9M-X5BS" → active tab "Identity", `#prof-device-code` absent;
// with `jf_profile_tab=mcp` pre-stored → "MCP agents", code prefilled. The
// design-time evidence used the second state, which is why it was invisible.
//
// WHAT IT PINS
//   1. `mcp` requested + empty storage → `mcp`. This is the regression: revert
//      the helper's mcp branch (or narrow it back to `=== "connected"`) and this
//      case goes red while the others stay green.
//   2. `connected` requested → `connected` — the one value that worked before,
//      so the fix cannot trade the old behaviour for the new one.
//   3. A junk request does NOT win: with `security` stored, `?tab=workspace`
//      → `security`, i.e. an unknown id still falls back to the stored tab. A
//      helper that accepted any string would select a tab with no panel and
//      leave the strip with zero `aria-selected`.
//   4. Nothing anywhere → `identity`.
//   5. The query beats a CONFLICTING stored tab (stored `identity`, `?tab=mcp`
//      → `mcp`); a first-time user has nothing stored, but a user who landed on
//      Identity once and then clicked the email must also be recovered.
//   6. A stored-but-not-a-tab value (`workspace`, from an older build) still
//      lands on a rendered tab.
//   7. `?linked=` keeps winning (issue #310's social callback) so this card does
//      not silently move the post-link landing.
//   8. The page WIRES to this helper: src/app/profile/page.tsx imports
//      `resolveProfileTab` and has no second inline copy of the rule. Without
//      this, the pure rule could be perfect and the page could still ignore it —
//      which is exactly the shape of the bug being fixed here.
//
// WHAT IT DOES NOT COVER (stated honestly). This repo has no DOM/JSX unit
// runner, so this file does not render the page: that the tab it picks also
// paints `#prof-device-code` with the code prefilled was read out of the live
// built app in a browser, and is pinned for the a11y gate by the
// `?tab=mcp&user_code=` arm in web/e2e/a11y-profile-tabs.spec.ts.
//
// Run with:
//
//   npm run test:unit

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { PROFILE_TABS, isProfileTab, resolveProfileTab } from "./profileTab.ts";

const HERE = fileURLToPath(new URL(".", import.meta.url));
const PAGE = readFileSync(`${HERE}../app/profile/page.tsx`, "utf8");

test("the email's link: ?tab=mcp with EMPTY storage opens the MCP agents tab", () => {
  // The exact entry point from api/internal/httpapi/oauth_device.go
  // (verification_uri_complete = BaseURL + "/profile?tab=mcp&user_code=…").
  assert.equal(resolveProfileTab("?tab=mcp&user_code=KX9M-X5BS", null), "mcp");
  assert.equal(resolveProfileTab("?tab=mcp", null), "mcp");
  // A first-time user may have a store that is readable but empty.
  assert.equal(resolveProfileTab("?tab=mcp", ""), "mcp");
});

test("?tab=connected still opens Social accounts (the value that worked before)", () => {
  assert.equal(resolveProfileTab("?tab=connected", null), "connected");
});

test("an unknown ?tab= does not win: it falls back to the stored tab", () => {
  assert.equal(resolveProfileTab("?tab=workspace", "security"), "security");
  assert.equal(resolveProfileTab("?tab=MC", "passkeys"), "passkeys");
  // ...and to Identity when nothing is stored, exactly as before this card.
  assert.equal(resolveProfileTab("?tab=nonsense", null), "identity");
});

test("nothing stored and no ?tab= anywhere lands on Identity", () => {
  assert.equal(resolveProfileTab("", null), "identity");
  assert.equal(resolveProfileTab("?user_code=KX9M-X5BS", null), "identity");
});

test("the query beats a conflicting stored tab, so a second click recovers", () => {
  // The bug's second half: the page persists the tab it landed on, so a user who
  // opened the email link once (landing on identity) then clicked it again had
  // `jf_profile_tab=identity` stored. The link must still win.
  assert.equal(resolveProfileTab("?tab=mcp", "identity"), "mcp");
  assert.equal(resolveProfileTab("?tab=mcp", "security"), "mcp");
});

test("a stored value that is not a tab falls back to Identity", () => {
  assert.equal(resolveProfileTab("", "workspace"), "identity");
  assert.equal(resolveProfileTab("", ""), "identity");
});

test("?linked= keeps winning: the social-connect callback still lands on Social", () => {
  // Issue #310. A callback never carries a tab of its own; if one is hand-added
  // the freshly-linked account is still what the user came to see.
  assert.equal(resolveProfileTab("?linked=google", null), "connected");
  assert.equal(resolveProfileTab("?linked=google&tab=mcp", null), "connected");
});

test("every tab the page renders is selectable, and only those are", () => {
  for (const id of PROFILE_TABS) {
    assert.equal(resolveProfileTab(`?tab=${id}`, null), id, `?tab=${id} must select ${id}`);
  }
  for (const junk of ["Identity", "profile", "connected ", "", "mcp "]) {
    assert.equal(isProfileTab(junk), false, `${JSON.stringify(junk)} is not a tab id`);
  }
  assert.deepEqual([...PROFILE_TABS], ["identity", "security", "passkeys", "connected", "mcp"]);

  // The strip's own list (a literal, because e2e/a11y-profile-tabs.spec.ts reads
  // it out of this source) must be exactly the list the arrival rule accepts —
  // otherwise `?tab=` could name a tab the page never paints, and the strip would
  // end up with zero `aria-selected`.
  const decl = PAGE.match(/const TAB_IDS[^=]*=\s*\[([^\]]*)\]/);
  assert.ok(decl, "profile/page.tsx no longer declares TAB_IDS");
  const pageTabs = [...decl[1].matchAll(/"([^"]+)"/g)].map((m) => m[1]);
  assert.deepEqual(pageTabs, [...PROFILE_TABS], "TAB_IDS must equal PROFILE_TABS");
});

test("the page resolves its tab through this helper (no second inline copy)", () => {
  assert.match(
    PAGE,
    /import\s*\{[^}]*resolveProfileTab[^}]*\}\s*from\s*"\.\.\/\.\.\/lib\/profileTab"/,
    "src/app/profile/page.tsx must import resolveProfileTab from lib/profileTab",
  );
  // The old shape — an inline `?tab= === "connected"` special case — must be gone.
  assert.doesNotMatch(
    PAGE,
    /params\.get\(\s*"tab"\s*\)\s*===/,
    "page.tsx still special-cases ?tab= inline instead of using resolveProfileTab",
  );
  // And the helper must receive BOTH inputs: the query and the stored value.
  assert.match(
    PAGE,
    /resolveProfileTab\(\s*window\.location\.search\s*,\s*stored\s*\)/,
    "loadStoredTab must pass the query and the stored tab to resolveProfileTab",
  );
});
