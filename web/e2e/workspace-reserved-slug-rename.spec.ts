// The workspace ADDRESS refusal, driven in a real browser against the BUILT app.
//
// WHY THIS EXISTS (card t_b7f81f5e, the rework of PR 691). The unit suite
// (src/lib/workspaceSlug.test.ts) reads the source and the dictionary, and the Go
// tests drive the REAL route with real Postgres — but neither renders the page.
// A perfectly wired apiErr mapping can still be dead behind a mis-wired call
// site, and the defect this card fixes was itself invisible to the Go suite for
// the opposite reason (the handler answered 500, so no assertion on the message
// could fire). This file types into the REAL address field, clicks the REAL Save
// button and reads the sentence out of the live DOM.
//
// WHAT IT PINS (the merchant's half of the contract)
//   * A rename the server refuses with `code: "reserved_subdomain"` is reported
//     in the merchant's own language, as the RESERVED sentence — not the card's
//     generic "couldn't rename" line, and never the server's Malay prose (#6177's
//     rule: the code decides the sentence, not the prose).
//   * The address sentence is NOT reported as a malformed slug: being told
//     "use a-z, 0-9, dash" would send the merchant to retype a name JomForm owns.
//   * An UNKNOWN code still falls back to the card's own line — the mapping must
//     not have become a wildcard that claims to know a refusal it does not.
//
// THE API IS STUBBED AT THE BROWSER LEVEL (no Go server, no session, no DB), the
// same idiom as workspace-name-cap.spec.ts, so this runs in the `e2e` gate
// (`npm run e2e`) with nothing but the build that gate already serves.
//
// WHAT IT DOES NOT COVER (stated honestly): it does not send a real request to
// the Go API. workspace_rename_reserved_slug_test.go drives the real route and
// asserts the status, the code and the absence of a written row/alias; this file
// covers the browser half that test cannot reach.
import { test, expect, type Page } from "@playwright/test";

import { signIn } from "./auth";

// The dashboard paths are gated on the SERVER (web/src/middleware.ts): a request
// without the `jf_session` cookie is redirected to /login BEFORE the page
// renders. This spec visits one, so it declares the session it means — before
// the gate existed an anonymous visit rendered the account shell, so this spec
// could (and did) rely on the bug. See e2e/auth.ts.
test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});
import { DICT } from "../src/components/i18nDict";

// The code the Go handler sends for the reserved-LIST refusal, restated here
// rather than imported, so a change to it that this spec did not follow shows up
// as a FAILURE instead of silently moving the bar. lib/workspaceSlug.ts ties the
// constant back to urlguard.ReservedSubdomainCode.
const RESERVED_CODE = "reserved_subdomain";
const RESERVED_KEY = "workspace.slugReserved";

// setWorkspaceScope pins the ACTIVE workspace the way the Shell does. The
// Settings page resolves `role` by matching the bundle's workspaces against the
// slug in localStorage (`jf_workspace`); with that key unset the match fails, the
// role is "" and the identity card does not render at all.
//
// It also CLEARS the stored language, so the page renders in the default locale a
// first-time visitor gets rather than a `jf_lang` an earlier card left behind.
//
// The clear/set happens ONCE PER CONTEXT (guarded by sessionStorage), not on
// every navigation: doRename reloads the page on success, and an unconditional
// init script would wipe the renamed slug that reload exists to pick up — the
// success test would then assert against a page it had itself just reset.
async function setWorkspaceScope(page: Page, slug: string) {
  await page.addInitScript((s) => {
    try {
      if (window.sessionStorage.getItem("e2e_ws_seeded") === "1") return;
      window.localStorage.clear();
      window.localStorage.setItem("jf_workspace", s as string);
      window.sessionStorage.setItem("e2e_ws_seeded", "1");
    } catch {
      /* storage unavailable — the card would not render, and the assertions fail loudly */
    }
  }, slug);
}

async function stubAPI(
  page: Page,
  patch: { status: number; body: unknown },
  opts?: { renamedSlug?: string },
) {
  const json = (body: unknown, status = 200) => ({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });

  // The address the Shell should resolve as the ACTIVE workspace. It follows a
  // successful rename: doRename reloads the page, and without this the reloaded
  // bundle would still advertise the OLD slug, the role match would fail and the
  // identity card would not render — so a "no refusal was shown" assertion after
  // the reload would pass by looking at nothing.
  let activeSlug = "demo";

  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));

  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (route) =>
    route.fulfill(
      json({
        me: { id: "u1", email: "merchant@example.com", name: "Merchant" },
        workspaces: [{ id: "ws1", name: "Demo", slug: activeSlug, role: "admin" }],
      }),
    ),
  );
  // lang:"" (never set) keeps the browser/default locale in effect.
  await page.route(/(.*)\/api\/auth\/profile$/, (route) => route.fulfill(json({ lang: "" })));
  await page.route(/(.*)\/api\/workspaces\/[^/]+\/deletion-check(\?.*)?$/, (route) =>
    route.fulfill(
      json({
        id: "ws1",
        name: "Demo",
        slug: activeSlug,
        is_owner: true,
        role: "admin",
        last_workspace: false,
        empty: true,
        blockers: [],
        can_delete: true,
      }),
    ),
  );
  // The rename card PATCHes /api/workspaces/{id} (NOT /api/rest/...). Pinned LAST
  // so it wins — Playwright resolves matching routes in reverse registration
  // order — and it is the one route the tests vary.
  await page.route(/(.*)\/api\/workspaces\/[^/]+$/, (route) => {
    if (route.request().method() === "PATCH") {
      if (patch.status < 300 && opts?.renamedSlug) activeSlug = opts.renamedSlug;
      return route.fulfill(json(patch.body, patch.status));
    }
    return route.fulfill(json({}));
  });
  await page.route(/(.*)\/api\/rest\/forms(\?.*)?$/, (route) => route.fulfill(json([])));
}

// The rename card's save button, scoped to the identity section so the Danger
// zone's own button cannot be clicked.
async function saveRename(page: Page) {
  const section = page.locator("section").filter({ has: page.locator("#ws-rename-slug") }).first();
  await section.getByRole("button", { name: DICT["ws.saveRename"].en }).first().click();
}

// The refusal is the card's `<p role="alert">`. Read it by ROLE rather than by
// class or by picking the last <p>: the card ends with a static "you can rename
// it below" note, so a positional reader returns that note instead of the message
// and the assertion would fail on a page that is behaving correctly. (The success
// text is a `<span role="status">`, but it is only visible for the instant before
// doRename's reload — see the success test at the bottom.)
async function refusalText(page: Page): Promise<string> {
  const alert = page.locator('section:has(#ws-rename-slug) [role="alert"]');
  return (await alert.count()) ? ((await alert.first().textContent()) || "").trim() : "";
}

test("a rename onto a reserved label is reported as the RESERVED sentence", async ({ page }) => {
  test.setTimeout(180_000);
  await setWorkspaceScope(page, "demo");
  // The server's wire body for the refusal: Malay prose + the machine code. The
  // prose must NEVER reach the page.
  await stubAPI(page, {
    status: 400,
    body: {
      error: '"billing" is reserved for JomForm: it is an address JomForm owns or must protect.',
      code: RESERVED_CODE,
    },
  });

  await page.goto("/settings?tab=workspace");
  await expect(page.locator("#ws-rename-slug")).toHaveValue("demo");

  // A rename the merchant actually performs: type a reserved label and save.
  await page.locator("#ws-rename-slug").fill("billing");
  await saveRename(page);

  await expect(page.locator('section:has(#ws-rename-slug) [role="alert"]')).toContainText(
    DICT[RESERVED_KEY].en,
    { timeout: 30_000 },
  );

  const msg = await refusalText(page);
  // Report the OBSERVED values so the evidence is in the run log, not inferred.
  console.log(`OBSERVED reserved message=${JSON.stringify(msg)}`);
  console.log(`OBSERVED title=${JSON.stringify(await page.title())}`);
  console.log(`OBSERVED lang=${await page.evaluate(() => document.documentElement.lang)}`);

  expect(msg).toContain(DICT[RESERVED_KEY].en);
  // The server's prose must not leak to an English reader (#6177).
  expect(msg).not.toContain("reserved for JomForm: it is an address");
  // …and the refusal must not be reported as the card's generic failure line.
  expect(msg).not.toBe(DICT["ws.renameFail"].en);
});

test("a reserved refusal is NOT reported as a malformed address", async ({ page }) => {
  test.setTimeout(180_000);
  await setWorkspaceScope(page, "demo");
  await stubAPI(page, {
    status: 400,
    body: { error: '"billing" is reserved for JomForm: …', code: RESERVED_CODE },
  });

  await page.goto("/settings?tab=workspace");
  await expect(page.locator("#ws-rename-slug")).toHaveValue("demo");

  await page.locator("#ws-rename-slug").fill("billing");
  await saveRename(page);

  await expect(page.locator('section:has(#ws-rename-slug) [role="alert"]')).toContainText(
    DICT[RESERVED_KEY].en,
    { timeout: 30_000 },
  );

  // The ADDRESS-IS-MALFORMED sentence must not be what the merchant reads: it
  // would send them to retype a name JomForm owns instead of picking another.
  const msg = await refusalText(page);
  console.log(`OBSERVED not-malformed message=${JSON.stringify(msg)}`);
  expect(msg).toContain(DICT[RESERVED_KEY].en);
  await expect(
    page.getByText(/slug must be 3-40 chars/i),
    "a reserved label was reported as an invalid slug",
  ).toHaveCount(0);
  await expect(
    page.getByText(/a-z, 0-9, dash/i),
    "a reserved label was reported as a character-shape problem",
  ).toHaveCount(0);
});

test("a code this build does not know still falls back to the card's own line", async ({ page }) => {
  test.setTimeout(180_000);
  await setWorkspaceScope(page, "demo");
  // The mapping must not have become a wildcard: an unknown refusal is reported
  // by the card's own sentence, not as a refusal the app pretends to understand.
  await stubAPI(page, {
    status: 400,
    body: { error: "some future refusal", code: "workspace.somethingBrandNew" },
  });

  await page.goto("/settings?tab=workspace");
  await expect(page.locator("#ws-rename-slug")).toHaveValue("demo");

  await page.locator("#ws-rename-slug").fill("kedai-bunga");
  await saveRename(page);

  await expect(page.locator('section:has(#ws-rename-slug) [role="alert"]')).toContainText(
    DICT["ws.renameFail"].en,
    { timeout: 30_000 },
  );

  const msg = await refusalText(page);
  console.log(`OBSERVED unknown-code message=${JSON.stringify(msg)}`);
  expect(msg).toContain(DICT["ws.renameFail"].en);
  expect(msg).not.toContain(DICT[RESERVED_KEY].en);
});

// The success path RELOADS the page (WorkspaceTab.doRename: `window.location.reload()`)
// so the session bundle re-derives the workspace list from scratch — which means
// the card's transient `<span role="status">` is deliberately not observable.
// Asserting on it would be asserting on something the app does not have.
//
// The property this test can honestly check is the one that matters for the
// refusal change: a 200 must still be treated as a success — no refusal
// sentence, and the browser's stored address follows the server's new slug (the
// reload keeps the user on their own workspace instead of a stale one).
test("a 200 is still treated as a success — the refusal path did not swallow it", async ({
  page,
}) => {
  test.setTimeout(180_000);
  await setWorkspaceScope(page, "demo");
  await stubAPI(
    page,
    { status: 200, body: { id: "ws1", slug: "kedai-bunga", name: "Demo" } },
    { renamedSlug: "kedai-bunga" },
  );

  await page.goto("/settings?tab=workspace");
  await expect(page.locator("#ws-rename-slug")).toHaveValue("demo");

  await page.locator("#ws-rename-slug").fill("kedai-bunga");
  await saveRename(page);

  // The stored address is written from the server's answer BEFORE the reload.
  await page.waitForFunction(
    () => window.localStorage.getItem("jf_workspace") === "kedai-bunga",
    null,
    { timeout: 30_000 },
  );
  const stored = await page.evaluate(() => window.localStorage.getItem("jf_workspace"));
  console.log(`OBSERVED stored workspace after a successful rename=${JSON.stringify(stored)}`);
  expect(stored).toBe("kedai-bunga");

  // Land on the reloaded page with the card actually rendered, so the negative
  // assertions below are about a live card and not about an empty page.
  await expect(page.locator("#ws-rename-slug")).toHaveValue("kedai-bunga", { timeout: 30_000 });
  console.log(
    `OBSERVED reloaded address field=${JSON.stringify(
      await page.locator("#ws-rename-slug").inputValue(),
    )}`,
  );

  // No refusal sentence was rendered for a success.
  await expect(page.locator('section:has(#ws-rename-slug) [role="alert"]')).toHaveCount(0);
  await expect(
    page.getByText(DICT[RESERVED_KEY].en),
    "a successful rename was reported as a reserved-name refusal",
  ).toHaveCount(0);
  await expect(
    page.getByText(DICT["ws.renameFail"].en),
    "a successful rename was reported as a failure",
  ).toHaveCount(0);
});
