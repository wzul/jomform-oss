// The "Make this the default workspace" ACTION — what the click DOES.
//
// WHY THIS FILE EXISTS (card t_74c80a03). The action had no behavioural test
// anywhere. The only coverage was a colour-contrast measurement of the switcher
// row while hovered (`e2e/a11y-switcher.spec.ts`), which says nothing about the
// request the click sends or about the state it paints afterwards. The action
// also cannot be exercised on production: it only SETS a default (nothing in the
// picker ever unsets it), and the protected account it was found on holds
// `default_workspace_id = NULL` — so a click there is an irreversible state
// change on the fixture, not a reading. This file is the substitute for that
// click, and it is a browser measurement on purpose: the claim is about the
// request that goes out and about what the merchant then sees.
//
// TWO RENDERS, ONE ACTION. The action exists twice with the same meaning:
//
//   * `/workspace-picker` (`src/app/workspace-picker/page.tsx`): each row that
//     is NOT the default offers the control, and a successful PUT moves the
//     local `defaultId` — so the badge moves and the control disappears from
//     that row (and appears on the row that lost it).
//   * the header switcher (`src/components/WorkspaceSwitcher.tsx`, rendered by
//     `Shell`): the action belongs to the ACTIVE workspace, so it can only ever
//     put the active id on the wire; when the active workspace already IS the
//     default the same slot offers the clear (`onSetDefault("")`) instead.
//
// WHAT THE ARMS PIN
//   1. PICKER/SUCCESS — with two workspaces and `alpha` stored as the active
//      one, clicking the control on `beta` sends EXACTLY ONE
//      `PUT /api/auth/profile/default-workspace` carrying `beta`'s id. The id on
//      the wire is the assertion: `beta` is neither the active workspace nor the
//      first row, so a call site that sent `active.id` (or `workspaces[0].id`,
//      or the row index) fails here.
//   2. PICKER/SUCCESS — the `Default` badge moves to `beta`, and the control is
//      gone from `beta` and offered on `alpha`. Both halves, in the live DOM.
//   3. PICKER/REFUSAL — a 4xx must NOT move the default AND must NOT take the
//      list away (t_57954ce5): the failure sentence (`workspace.setDefaultFail`)
//      renders beside the rows, both rows are still on screen with `alpha` still
//      carrying the badge and `beta` still offering the control, there is no
//      Retry-only panel in sight, and a re-read still answers `alpha`. A failed
//      write that painted the new badge would be a lie about server state; one
//      that replaced the list with the page's load-error panel would claim a
//      read that never failed. Run TWICE — 400 and the 403 `request.forbidden`
//      this route really answers for a workspace the caller does not own
//      (t_25e9224b) — and both arms assert the URL is still /workspace-picker:
//      a refusal from a live session must never be read as a dead one and
//      navigate to /login, which is what the shipped page did for the 403.
//   4. CONTROL vs BADGE — the control is never offered on the workspace already
//      marked default: asserted on the initial render, after a successful move,
//      and in the `default_workspace_id = NULL` shape (no row is marked default,
//      so EVERY row offers it — the control tracks the badge, not a flag that
//      happens to be hidden).
//   5. SWITCHER/ACTIVE — the menu's own row sends exactly one PUT carrying the
//      ACTIVE workspace's id and closes the menu.
//   6. SWITCHER/DEFAULT — when the active workspace is already the default the
//      menu offers the clear action and NOT the set action, and the clear really
//      sends `{"workspace_id": ""}` (the API's "no default" value).
//
// THE API IS STUBBED AT THE BROWSER LEVEL — no Go server, no database, no
// account and no workspace is created — the idiom `e2e/settings-domains-workspace-switch.spec.ts`
// and `e2e/a11y-switcher.spec.ts` use. Only the routes this action touches are
// controlled; the real client components run in a real browser.
//
// PROVEN TO BITE (each sabotage built and run, then reverted):
//   * the picker's request body pointed at the ACTIVE workspace's id instead of
//     the clicked row's — `2 failed / 4 passed`, the wire arms reading
//     `Measured: [{"workspace_id":"ws-alpha"}]` where `ws-beta` was clicked;
//   * the `setDefaultId(workspaceId)` call dropped after a successful PUT —
//     `3 failed / 3 passed`, the badge arms reading `element(s) not found` for
//     the `Default` text on the clicked row (it never moved);
//   * the picker's `setSetDefaultError(r.error)` reverted to `setLoadError(r.error)`
//     (the t_57954ce5 defect, rebuilt and re-run) — arm 3 failing at
//     `expect(locator('main li')).toHaveCount(2)` with `Received: 0`: the refused
//     write had replaced the whole list with the load-error panel;
//   * the write's `goToLoginIfSessionDead` reverted to `goToLogin` (the
//     t_25e9224b defect, rebuilt and re-run on 2026-11-06) — the 403 arm failing
//     with the browser on /login and no sentence, while the 400 arm still passed.
// The final tree reads 7 passed. See the cards t_74c80a03, t_57954ce5 and
// t_25e9224b.
import { test, expect, type Page } from "@playwright/test";

import { DICT } from "../src/components/i18nDict";
import { signIn } from "./auth";

// The dashboard paths are gated on the SERVER (`web/src/middleware.ts`):
// `/workspace-picker` and the shell pages both redirect to /login without a
// `jf_session` cookie, so the session this spec means is declared explicitly.
// See `e2e/auth.ts`.
test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});

// Labels read from the DICTIONARY, never retyped: a renamed key moves the
// selector with it instead of silently matching nothing (the failure mode a
// literal string has).
const PICKER_TITLE = DICT["workspace.pickerTitle"].en;
const SET_DEFAULT = DICT["workspace.setDefault"].en;
const CLEAR_DEFAULT = DICT["workspace.clearDefault"].en;
const DEFAULT_BADGE = DICT["workspace.defaultLabel"].en;
const SET_DEFAULT_FAIL = DICT["workspace.setDefaultFail"].en;

/** Two workspaces. `alpha` is stored as the ACTIVE one in the browser state. */
const ALPHA = { id: "ws-alpha", name: "Alpha Shop", slug: "alpha", role: "admin", url: "https://alpha.jomform.com" };
const BETA = { id: "ws-beta", name: "Beta Shop", slug: "beta", role: "admin", url: "https://beta.jomform.com" };

type DefaultStub = {
  /** Every `PUT /api/auth/profile/default-workspace` body, in order. */
  readonly puts: { workspace_id?: unknown }[];
  /**
   * Answer the NEXT default-workspace PUT with this status and machine `code`
   * (reset to 200 after one). `code` is the API's own failure code — the body
   * shape `{"error": <prose>, "code": <machine code>}` the Go handlers write.
   */
  refuseWith: (status: number, code?: string) => void;
  /** The default the stubs currently claim, so a re-read is consistent with the writes. */
  readonly defaultId: string;
};

/**
 * Stub the API the two surfaces need.
 *
 * `initialDefault` is the value `default_workspace_id` carries at mount, i.e. the
 * row that starts with the badge; `""` is the production shape found on the
 * protected account (`NULL`).
 *
 * The PUT handler MUTATES `defaultId` on success, so a page that re-reads
 * `/api/auth/me` (the picker's Retry does) sees the state the server would
 * really hold — otherwise the refusal arm would "not move" simply because the
 * stub never acknowledged anything.
 */
async function stubAPI(page: Page, initialDefault: string): Promise<DefaultStub> {
  let defaultId = initialDefault;
  let refuseStatus = 0;
  let refuseCode = "";
  const puts: { workspace_id?: unknown }[] = [];

  const json = (body: unknown, status = 200) => ({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });

  // Catch-all FIRST, specific routes AFTER it: Playwright resolves matching
  // routes in reverse registration order, so the later (specific) ones win.
  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
  await page.route(/(.*)\/api\/workspaces$/, (r) => r.fulfill(json([ALPHA, BETA])));
  // `$`-anchored so it cannot shadow `/api/auth/profile/default-workspace`.
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "" })));
  await page.route(/(.*)\/api\/auth\/me$/, (r) =>
    r.fulfill(
      json({
        id: "u1",
        email: "merchant@example.com",
        name: "Merchant",
        lang: "",
        is_super_admin: false,
        // The picker's badge and the switcher's set/clear slot both read this.
        default_workspace_id: defaultId,
      }),
    ),
  );
  await page.route(/(.*)\/api\/rest\/currency\/rates$/, (r) => r.fulfill(json({ rates: [], cached: false })));
  await page.route(/(.*)\/api\/rest\/products(\?.*)?$/, (r) => r.fulfill(json([])));

  // The default-workspace WRITE. Recorded first, then answered, so an arm can
  // assert on the body even when it is refused.
  await page.route(/(.*)\/api\/auth\/profile\/default-workspace$/, async (route) => {
    const body = (route.request().postDataJSON() ?? {}) as { workspace_id?: unknown };
    puts.push(body);
    if (refuseStatus !== 0) {
      const status = refuseStatus;
      const code = refuseCode;
      refuseStatus = 0;
      refuseCode = "";
      // The API's failure envelope carries Malay prose plus a machine code
      // (`api/internal/httpapi/errcode.go`). The prose is deliberately NOT
      // translatable here: an unmapped code must fall back to the page's own
      // sentence, which is what the refusal arm asserts. `code` is what the
      // client's classifier reads (`lib/wsfetch.ts` → `lib/apiErr.ts`), so an
      // arm can send the exact code the route really answers.
      await route.fulfill(json({ error: "gagal menetapkan workspace default", code }, status));
      return;
    }
    if (typeof body.workspace_id === "string") defaultId = body.workspace_id;
    await route.fulfill(json({}));
  });

  // The shell's bundle. `me.default_workspace_id` is what the switcher's slot
  // reads (`Shell` → `defaultWorkspaceId={user?.default_workspace_id}`).
  const bundle = () => ({
    me: {
      id: "u1",
      email: "merchant@example.com",
      name: "Merchant",
      lang: "",
      is_super_admin: false,
      default_workspace_id: defaultId,
    },
    workspaces: [ALPHA, BETA],
    chip_status: null,
    keys: [],
    domains: [],
    webhooks: [],
    social: [],
    einvoice: null,
    products: [],
    summary: { counts: {}, revenue_sen: 0 },
    sales: { items: [], page: 1, limit: 20, total: 0 },
    form_analytics: [],
    product_analytics: [],
  });
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(bundle())));

  return {
    puts,
    refuseWith: (status: number, code = "") => {
      refuseStatus = status;
      refuseCode = code;
    },
    get defaultId() {
      return defaultId;
    },
  };
}

/**
 * A FRESH browser state: the browser keeps localStorage between runs, so a
 * `jf_workspace` or `jf_lang` left behind by an earlier card would scope this
 * file's pages to a workspace — or a language — this file never described. ONE
 * init script (clear, then set) so the order is not a property of the
 * registration order of two scripts.
 */
async function freshState(page: Page, slug: string) {
  await page.addInitScript((s) => {
    try {
      localStorage.clear();
      sessionStorage.clear();
      localStorage.setItem("jf_workspace", s as string);
      localStorage.setItem("jf_lang", "en");
    } catch {
      /* storage unavailable */
    }
  }, slug);
}

/** One picker row, by the workspace name it prints. */
function pickerRow(page: Page, name: string) {
  return page.locator("main li").filter({ hasText: name });
}

/** The `Default` badge inside a picker row (the row's own claim about server state). */
function badge(page: Page, name: string) {
  return pickerRow(page, name).getByText(DEFAULT_BADGE, { exact: true });
}

/** The "Make this the default workspace" control inside a picker row. */
function setControl(page: Page, name: string) {
  return pickerRow(page, name).getByRole("button", { name: SET_DEFAULT, exact: true });
}

/**
 * Open the topbar workspace switcher. The trigger is visible in the
 * server-rendered shell BEFORE React hydrates, so a click landing in that window
 * is a no-op; `aria-expanded` is the app's own truth for "the menu is open", and
 * the click is retried until it says so (the pattern `e2e/a11y-switcher.spec.ts`
 * uses — a blind click there was measured as a flake).
 */
async function openSwitcher(page: Page) {
  const trigger = page.locator('button[aria-haspopup="menu"]').first();
  await trigger.waitFor({ state: "visible", timeout: 30_000 });
  await expect
    .poll(
      async () => {
        if ((await trigger.getAttribute("aria-expanded")) !== "true") await trigger.click();
        return trigger.getAttribute("aria-expanded");
      },
      { timeout: 30_000, message: "the switcher never reported aria-expanded=true after a click" },
    )
    .toBe("true");
  return trigger;
}

/** The menu the switcher opens (its `role="menu"` panel). */
function switcherMenu(page: Page) {
  return page.locator('[role="menu"]');
}

/** The menu row that carries `label` (both actions are `role="menuitem"`). */
function menuItem(page: Page, label: string) {
  return switcherMenu(page).getByRole("menuitem", { name: label, exact: true });
}

// ---------------------------------------------------------------------------
// The picker: the click's effect on the WIRE, the badge and the control.
// ---------------------------------------------------------------------------

test("picker: clicking the control on a non-default row sends THAT row's id, once", async ({ page }) => {
  // `alpha` is the stored ACTIVE workspace and the current default; we click
  // `beta` — so a request carrying the active id (or the first row's) is wrong.
  const api = await stubAPI(page, ALPHA.id);
  await freshState(page, ALPHA.slug);

  await page.goto("/workspace-picker", { waitUntil: "domcontentloaded" });
  await expect(page.getByRole("heading", { name: PICKER_TITLE })).toBeVisible();
  await expect(page.locator("main li")).toHaveCount(2);

  // Precondition, stated: this arm is only meaningful with a live control to
  // click, and with the badge on the OTHER row.
  await expect(badge(page, ALPHA.name)).toBeVisible();
  await expect(badge(page, BETA.name)).toHaveCount(0);
  await expect(setControl(page, BETA.name)).toBeVisible();

  await setControl(page, BETA.name).click();

  // The badge moves only after the request resolves, so this is also the
  // barrier that makes the wire count below the count for the WHOLE click.
  await expect(badge(page, BETA.name)).toBeVisible();
  expect(
    api.puts.map((b) => b.workspace_id),
    "the click must send exactly one PUT, carrying the CLICKED row's id — `beta` (ws-beta), " +
      `not the active workspace (${ALPHA.id}), not the first row, not a slug. Measured: ` +
      JSON.stringify(api.puts),
  ).toEqual([BETA.id]);
});

test("picker: the badge moves to the clicked row and the control follows it", async ({ page }) => {
  const api = await stubAPI(page, ALPHA.id);
  await freshState(page, ALPHA.slug);

  await page.goto("/workspace-picker", { waitUntil: "domcontentloaded" });
  await expect(page.locator("main li")).toHaveCount(2);

  await setControl(page, BETA.name).click();
  await expect.poll(() => api.puts.length, { timeout: 10_000 }).toBe(1);

  // The clicked row now claims the default AND stops offering the control…
  await expect(badge(page, BETA.name)).toBeVisible();
  await expect(setControl(page, BETA.name)).toHaveCount(0);
  // …and the row that lost it now offers it. Both halves are read from the live
  // DOM: a page that painted the badge without dropping the control (or vice
  // versa) fails here.
  await expect(badge(page, ALPHA.name)).toHaveCount(0);
  await expect(setControl(page, ALPHA.name)).toBeVisible();
});

// THE 4xx SHAPES THIS WRITE REALLY ANSWERS, both exercised.
//
//   * 400 — an unreadable body. The shape the original arm used.
//   * 403 — `request.forbidden`, which is what `handleSetDefaultWorkspace`
//     (api/internal/httpapi/profile.go) answers for "not your workspace": a
//     workspace the session is a MEMBER of but does not own (`WorkspaceStore.ByMember`
//     lists those rows) or a row that went stale between the list read and the
//     click.
//
// The 403 arm is card t_25e9224b: the shipped page passed `goToLogin` as the
// write's `onUnauth`, and `lib/wsfetch.ts` classifies every 403 whose code is not
// `workspace.notFound` as `unauth` — so a refusal from a LIVE session navigated
// the browser to /login and the rows, badge and sentence never rendered
// (measured on prod v1.22.126: url /login, `main li` 0, `/api/auth/me` still
// 200). This arm asserts the url, so a `goToLogin` implementation fails it.
const REFUSAL_SHAPES = [
  { status: 400, code: "", why: "an unreadable body" },
  { status: 403, code: "request.forbidden", why: "a workspace that is not the caller's" },
] as const;

for (const { status, code, why } of REFUSAL_SHAPES) {
  test(`picker: a ${status} on the put (${why}) keeps the rows and the badge, shows the sentence, and does not navigate`, async ({
    page,
  }) => {
    const api = await stubAPI(page, ALPHA.id);
    await freshState(page, ALPHA.slug);

    await page.goto("/workspace-picker", { waitUntil: "domcontentloaded" });
    await expect(page.locator("main li")).toHaveCount(2);
    await expect(badge(page, ALPHA.name)).toBeVisible();

    api.refuseWith(status, code);
    await setControl(page, BETA.name).click();

    // The failure is never silent: the page's own sentence (not the server's
    // Malay prose, which the stub really sends) is rendered in the live DOM.
    await expect(page.getByText(SET_DEFAULT_FAIL, { exact: true })).toBeVisible();

    // THE LIST SURVIVES THE REFUSED WRITE (card t_57954ce5). The rows are the
    // surface the merchant is acting on and they were read successfully; the
    // page's LOAD-error panel claims the opposite ("the list could not be read")
    // and has no business replacing them. Measured pre-fix: `main li` went 2 -> 0,
    // every `Default` badge vanished, and the only button left was Retry.
    await expect(page.locator("main li")).toHaveCount(2);
    expect(
      await page.getByRole("button", { name: DICT["common.retry"].en }).count(),
      "a refused WRITE replaced the workspace list with the load-error panel (and its Retry)",
    ).toBe(0);

    // NO NAVIGATION (card t_25e9224b). A refusal of this WRITE is not a dead
    // session, whatever the status or code, and the session here is alive by
    // construction — the page's own reads answered 200 a moment ago. A
    // `goToLogin` on this arm lands the browser on /login with nothing rendered.
    expect(
      page.url(),
      `a ${status} on the set-default write ejected the picker: the refusal belongs beside the rows, not on /login`,
    ).toContain("/workspace-picker");

    // The badge did NOT move: `alpha` still claims the default and `beta` still
    // does not — a success path would have painted it onto `beta`, and a page that
    // cleared it would be lying about server state it never changed.
    await expect(badge(page, ALPHA.name)).toBeVisible();
    await expect(badge(page, BETA.name)).toHaveCount(0);
    // …and the control is still there to try again on the row that was clicked.
    await expect(setControl(page, BETA.name)).toBeVisible();
    expect(api.puts.map((b) => b.workspace_id), "the refused click sent one put, for the clicked row").toEqual([
      BETA.id,
    ]);

    // The server never acknowledged a change, so a RE-READ must still answer
    // `alpha`. This shape has no Retry button (the list is on screen), so the
    // re-read is the browser's own reload, which re-runs the page's load().
    await page.reload({ waitUntil: "domcontentloaded" });
    await expect(page.locator("main li")).toHaveCount(2);
    await expect(badge(page, ALPHA.name)).toBeVisible();
    await expect(badge(page, BETA.name)).toHaveCount(0);
    await expect(setControl(page, BETA.name)).toBeVisible();
    await expect(setControl(page, ALPHA.name)).toHaveCount(0);
    expect(page.url(), "the reload of the picker must stay on the picker").toContain("/workspace-picker");
    expect(api.defaultId, "the failed write must not have changed the stored default in the stub either").toBe(
      ALPHA.id,
    );
  });
}

test("picker: the control is offered exactly where the badge is not", async ({ page }) => {
  // The production shape this action was found on: NO default at all
  // (`default_workspace_id = NULL`). No row is marked default, so every row
  // offers the control — the control tracks the BADGE, not a hidden flag.
  const api = await stubAPI(page, "");
  await freshState(page, ALPHA.slug);

  await page.goto("/workspace-picker", { waitUntil: "domcontentloaded" });
  await expect(page.locator("main li")).toHaveCount(2);

  await expect(page.getByText(DEFAULT_BADGE, { exact: true })).toHaveCount(0);
  await expect(setControl(page, ALPHA.name)).toBeVisible();
  await expect(setControl(page, BETA.name)).toBeVisible();

  // And once one row IS the default, that row stops offering it — asserted on a
  // re-read of the same page, so the rule is pinned in both directions.
  await setControl(page, ALPHA.name).click();
  await expect.poll(() => api.puts.length, { timeout: 10_000 }).toBe(1);
  await expect(badge(page, ALPHA.name)).toBeVisible();
  await expect(setControl(page, ALPHA.name)).toHaveCount(0);
  await expect(setControl(page, BETA.name)).toBeVisible();
});

// ---------------------------------------------------------------------------
// The header switcher: the same action, on the ACTIVE workspace only.
// ---------------------------------------------------------------------------

test("switcher: its default row sends exactly one put carrying the ACTIVE workspace's id", async ({ page }) => {
  // `alpha` is active (jf_workspace) and `beta` is the default, so the menu's
  // slot offers the SET action for the active workspace.
  const api = await stubAPI(page, BETA.id);
  await freshState(page, ALPHA.slug);

  await page.goto("/products", { waitUntil: "domcontentloaded" });
  const trigger = await openSwitcher(page);
  await expect(menuItem(page, SET_DEFAULT)).toBeVisible();

  await menuItem(page, SET_DEFAULT).click();

  // The handler closes the menu, so `aria-expanded` is the barrier that the
  // click was really handled — not just dispatched.
  await expect.poll(() => trigger.getAttribute("aria-expanded"), { timeout: 10_000 }).toBe("false");
  expect(
    api.puts.map((b) => b.workspace_id),
    "the switcher's row is about the ACTIVE workspace (`alpha`, ws-alpha) — never the " +
      `default one it is replacing (${BETA.id}). Measured: ` + JSON.stringify(api.puts),
  ).toEqual([ALPHA.id]);
});

test("switcher: the workspace already marked default is offered the clear, never the set", async ({ page }) => {
  // The active workspace IS the default, so the same slot must offer the clear.
  const api = await stubAPI(page, ALPHA.id);
  await freshState(page, ALPHA.slug);

  await page.goto("/products", { waitUntil: "domcontentloaded" });
  const trigger = await openSwitcher(page);

  // The rule, in the menu: the set action is never offered for a workspace that
  // is already the default (the slot holds the clear instead).
  await expect(menuItem(page, CLEAR_DEFAULT)).toBeVisible();
  await expect(menuItem(page, SET_DEFAULT)).toHaveCount(0);

  // …and the clear really writes the API's "no default" value: an empty
  // `workspace_id`, which is the only value that unsets it in this contract.
  await menuItem(page, CLEAR_DEFAULT).click();
  await expect.poll(() => trigger.getAttribute("aria-expanded"), { timeout: 10_000 }).toBe("false");
  expect(api.puts, "the clear must send the API's no-default value, `workspace_id: \"\"`").toEqual([
    { workspace_id: "" },
  ]);
});
