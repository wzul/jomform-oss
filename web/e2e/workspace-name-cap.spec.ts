// The workspace NAME cap, exercised in a real browser against the BUILT app.
//
// WHY THIS EXISTS. The defect this guards is about what a person experiences, not
// what the code compiles to: the server counted the name cap in BYTES while the
// field counted UTF-16 code units, so the field let a merchant type 120 accented
// characters (`valueLen` 120, `maxLength` 120, Save enabled, 240 bytes) and the
// API then refused the save. Nothing in `npm run test:unit` can see that — there
// is no DOM there — and the Go tests prove the server's side of the contract, not
// that the field is wired to it. This file is the executed half: it types through
// REAL input events into the REAL field and reads the result out of the live DOM.
//
// WHAT IT PINS
//   1. The name field's `maxLength` is the shared cap (120), and typing beyond it
//      is genuinely stopped by the browser — the value the merchant can produce
//      and the value the server accepts are the same number.
//   2. The cap is counted in CHARACTERS, so a 120-character multi-byte name
//      (CJK, accented) FITS and the Save button is offered. This is the exact
//      reading the defect failed: the byte-based server refused what the field
//      allowed. The count read back is `[...value].length`, i.e. code points.
//   3. The switcher's CREATE form carries the same cap — it shipped with none, so
//      a merchant could type an unbounded name and only find out from a failed
//      request.
//   4. Submitting an over-cap name is not reported as an invalid ADDRESS: the
//      form surfaces the server's own message. (The field's cap normally prevents
//      reaching that state by typing, which is why the stub is what answers — the
//      assertion is that the message shown is the server's, not a guess.)
//
// THE API IS STUBBED AT THE BROWSER LEVEL (no Go server, no session, no DB), the
// same idiom as form-settings-status-chips.spec.ts, so this runs in the axe gate
// with nothing but the build that gate already serves.
//
// WHAT IT DOES NOT COVER (stated honestly)
//   It does not send a real request to the Go API — workspace_name_cap_rest_test.go
//   and workspace_name_cap_mcp_test.go do that against real Postgres. This file
//   covers the browser half those cannot reach.
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

// The cap, restated here rather than imported, so a change to the shared constant
// that this spec did not follow shows up as a FAILURE instead of silently moving
// the bar. workspaceIdentity.test.ts ties it back to the Go constant.
const NAME_CAP = 120;

// A workspace whose STORED name is over the old byte cap — the row that used to
// make a slug-only rename impossible. 45 CJK characters is 135 bytes.
const LEGACY_LONG_NAME = "店".repeat(45);

// setWorkspaceScope pins the ACTIVE workspace the way the Shell does. The
// Settings page gates the Workspace tab on `role`, and the role is resolved from
// the bundle by matching the bundle's workspaces against the slug in
// localStorage (`jf_workspace` — see session.tsx). With that key unset the match
// fails, the role is "" and the tab is not rendered at all, so without this the
// spec would assert on a tab that never mounted.
//
// It also CLEARS the stored settings tab and language, so the page renders from
// the defaults a first-time visitor gets rather than whatever an earlier run left
// behind.
async function setWorkspaceScope(page: Page, slug: string) {
  await page.addInitScript((s) => {
    try {
      window.localStorage.clear();
      window.localStorage.setItem("jf_workspace", s as string);
    } catch {
      /* storage unavailable — the tab would not render, and the assertions fail loudly */
    }
  }, slug);
}

async function stubAPI(page: Page) {
  const json = (body: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });

  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));

  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (route) =>
    route.fulfill(
      json({
        me: { id: "u1", email: "merchant@example.com", name: "Merchant" },
        workspaces: [{ id: "ws1", name: LEGACY_LONG_NAME, slug: "demo", role: "admin" }],
      }),
    ),
  );
  // lang:"" (never set) keeps the browser value in effect; the field is what we
  // are measuring, so the locale is pinned to the default explicitly.
  await page.route(/(.*)\/api\/auth\/profile$/, (route) => route.fulfill(json({ lang: "" })));
  // The Settings → Workspace identity card and its Danger zone both read the
  // deletion check; this is the payload that seeds the rename inputs.
  await page.route(/(.*)\/api\/workspaces\/[^/]+\/deletion-check(\?.*)?$/, (route) =>
    route.fulfill(
      json({
        id: "ws1",
        name: LEGACY_LONG_NAME,
        slug: "demo",
        is_owner: true,
        role: "admin",
        last_workspace: false,
        empty: true,
        blockers: [],
        can_delete: true,
      }),
    ),
  );
  await page.route(/(.*)\/api\/rest\/forms(\?.*)?$/, (route) => route.fulfill(json([])));
}

// typeInto drives real input events, so the field's own maxLength is what limits
// the value — setting `.value` directly would bypass exactly the behaviour we are
// asserting.
async function typeInto(page: Page, selector: string, text: string) {
  await page.locator(selector).fill("");
  await page.locator(selector).pressSequentially(text, { delay: 1 });
}

// readNameField reports what the browser ACTUALLY holds, in the units that matter:
// code units (what maxLength counts) and code points (what the server counts).
async function readNameField(page: Page, selector: string) {
  return page.evaluate((sel) => {
    const el = document.querySelector(sel) as HTMLInputElement | null;
    if (!el) throw new Error(`no name field at ${sel} — the control moved`);
    return {
      maxLength: el.maxLength,
      valueLen: el.value.length,
      codePoints: [...el.value].length,
      bytes: new TextEncoder().encode(el.value).length,
    };
  }, selector);
}

test("the rename field caps the name at the shared character limit", async ({ page }) => {
  test.setTimeout(180_000);
  await setWorkspaceScope(page, "demo");
  await stubAPI(page);
  // ?tab=workspace selects the Workspace tab (the identity card lives there, and
  // it is not the default tab).
  await page.goto("/settings?tab=workspace");

  const RENAME = "#ws-rename-name";
  await expect(page.locator(RENAME)).toHaveValue(LEGACY_LONG_NAME);

  const cap = await readNameField(page, RENAME);
  expect(cap.maxLength, "the field must carry the shared cap, not the browser default").toBe(NAME_CAP);

  // THE READING THE DEFECT FAILED. 120 multi-byte characters must FIT the field
  // and be accepted as a name — the server judged these by bytes and refused
  // them, so the field showed 120/120 with Save enabled and the save still failed.
  await typeInto(page, RENAME, "é".repeat(NAME_CAP));
  const full = await readNameField(page, RENAME);
  expect(full.codePoints, "120 accented characters must fit the field").toBe(NAME_CAP);
  expect(full.bytes, "…and they are 240 BYTES — the unit the server used to count").toBe(240);
  expect(full.codePoints, "a character count, not a byte count").toBeLessThanOrEqual(cap.maxLength);

  // Typing PAST the cap is stopped by the browser, so the merchant cannot build a
  // name the server will refuse.
  await typeInto(page, RENAME, "a".repeat(NAME_CAP + 40));
  const over = await readNameField(page, RENAME);
  expect(over.codePoints, "the field must stop at the cap").toBe(NAME_CAP);

  // A 120-character CJK name fits too, and Save is offered for it.
  await typeInto(page, RENAME, "店".repeat(NAME_CAP));
  const cjk = await readNameField(page, RENAME);
  expect(cjk.codePoints, "120 CJK characters must fit — 360 bytes, one character each").toBe(NAME_CAP);
  expect(cjk.bytes).toBe(360);
  const save = page.getByRole("button", { name: /save|simpan/i }).first();
  await expect(save, "a name at the cap is saveable — the server accepts it").toBeEnabled();
});

test("the create form carries the same cap, so it cannot accept what the server refuses", async ({
  page,
}) => {
  test.setTimeout(180_000);
  await setWorkspaceScope(page, "demo");
  await stubAPI(page);
  await page.goto("/settings?tab=workspace");

  // The switcher's create form is inside the topbar dropdown.
  await page.getByLabel(/workspace/i).first().click();
  await page.getByRole("menuitem", { name: /new workspace/i }).click();

  const CREATE_NAME = 'input[aria-label="Workspace name"]';
  await expect(page.locator(CREATE_NAME)).toBeVisible();
  const cap = await readNameField(page, CREATE_NAME);
  expect(
    cap.maxLength,
    "the create form shipped with NO cap at all — it must carry the shared one",
  ).toBe(NAME_CAP);

  await typeInto(page, CREATE_NAME, "店".repeat(NAME_CAP + 30));
  const typed = await readNameField(page, CREATE_NAME);
  expect(typed.codePoints, "the create field must stop at the cap too").toBe(NAME_CAP);
});

test("an over-long name is reported as the NAME's problem, never as a bad address", async ({ page }) => {
  test.setTimeout(180_000);
  await setWorkspaceScope(page, "demo");
  // WHAT THIS DRIVES, AND WHY IT IS NOT "print the server's Malay prose".
  //
  // The switcher's create form goes through the SHARED helper
  // (lib/workspace.createWorkspace), which — as of #718/#6177 on main — decides
  // the message from the server's machine `code`, never from its prose (there is
  // a guard for that: lib/workspaceCreateErr.test.ts asserts the helper's source
  // contains no `.error` read, because that is how an English dashboard shipped a
  // Malay sentence).
  //
  // The server answers the NAME cap with `code: "workspace.nameTooLong"`, which
  // the dictionary renders per locale. So the property the card cares about — an
  // over-cap name must NOT be reported as an invalid ADDRESS — is asserted here
  // through the code path the app really uses.
  //
  // The name field's cap normally prevents reaching this by typing, which is why
  // the response is stubbed: the assertion is about how a refusal is REPORTED.
  await stubAPI(page);
  await page.route(/(.*)\/api\/workspaces$/, (route) => {
    if (route.request().method() !== "POST") return route.fallback();
    return route.fulfill({
      status: 400,
      contentType: "application/json",
      body: JSON.stringify({
        error: "nama workspace maksimum 120 aksara",
        code: "workspace.nameTooLong",
      }),
    });
  });

  await page.goto("/settings");
  await page.getByLabel(/workspace/i).first().click();
  await page.getByRole("menuitem", { name: /new workspace/i }).click();

  await page.locator('input[aria-label="Workspace name"]').fill("Kedai Bunga");
  await page.locator('input[aria-label="Slug (a-z, 0-9, dash)"]').fill("kedai-bunga-cap");
  await page.getByRole("button", { name: /create|cipta/i }).click();

  // The NAME's sentence, in the page's own language. A guessed "slug must be
  // 3-40 chars" would send the merchant to fix a field that is not the problem.
  await expect(
    page.getByText(/120 characters or fewer/i),
    "an over-long name must be reported as the name's problem, never as a bad address",
  ).toBeVisible();
  // …and the ADDRESS sentence must NOT be what they read.
  await expect(
    page.getByText(/slug must be 3-40 chars/i),
    "an over-long name was reported as an invalid slug",
  ).toHaveCount(0);
});
