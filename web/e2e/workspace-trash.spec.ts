// Regression guard: the workspace Trash view must render a binned workspace and
// its Restore button from the wire shape the server actually writes.
//
// THE DEFECT
// GET /v1/workspaces/trash answers a BARE ARRAY whose id field is `id`:
//
//     [{ "id": "...", "name": "...", "slug": "...", "deleted_at": "...",
//        "sales_count": 0, "can_purge": true }]
//
// (pinned on the Go side by TestRESTTrashLifecycle, which decodes the body into
// `var bin []struct{ ID string \`json:"id"\` … }`). `WorkspaceTab` read an
// `{"items":[...],"total":N}` envelope and keyed rows off `workspace_id`:
//
//     fetchJson<{ items: TrashedWorkspace[]; total: number }>("/api/workspaces/trash")
//     setTrash(Array.isArray(r.data?.items) ? r.data.items : []);
//
// `r.data.items` is `undefined` on a bare array, so `Array.isArray(...)` was
// always false and the list was always empty. The page then rendered "The Trash
// is empty." for a user who had just binned a workspace — and because the Restore
// button only exists inside a row, the ONLY way back out of the bin from the
// dashboard was unreachable. Every other part of the feature was green: the
// store, the REST route, the MCP tools and the i18n all had tests. Nothing
// rendered the page against the real wire shape, so nothing caught it.
//
// WHAT THIS GUARD PROVES, on the real built page with a stubbed API:
//   1. a row in the server's own wire shape renders in the Trash section
//      (its name, slug and "Moved to Trash" line are on screen);
//   2. a row reporting `can_purge:false` says WHY (its sales count), so the
//      refusal is visible before any permanent-delete affordance is offered;
//   3. the row's Restore button exists and POSTs to that row's id — the call
//      that brings the workspace back;
//   4. an EMPTY bin still renders its own sentence, so the assertion above
//      cannot be satisfied by rendering the row unconditionally.
//
// The API is stubbed at the browser level (no Go server, no session, no DB), so
// the gate needs nothing but the build it is already serving.
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

// The SERVER's shape, copied from the REST test's decode struct (not from the
// page's old type): `id`, not `workspace_id`; a bare array, not an envelope.
const BINNED = [
  {
    id: "ws_binned_1",
    name: "Binned shop",
    slug: "binned-shop",
    deleted_at: "2026-09-01T02:03:04Z",
    sales_count: 0,
    can_purge: true,
  },
  {
    id: "ws_binned_2",
    name: "Sold shop",
    slug: "sold-shop",
    deleted_at: "2026-09-02T02:03:04Z",
    sales_count: 4,
    can_purge: false,
  },
];

const ME = { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "en", is_super_admin: true };

const BUNDLE = {
  me: ME,
  // TWO workspaces, so the one under test is not the last-workspace case
  // (that refusal is the tab's other rule and has its own copy).
  workspaces: [
    { id: "ws1", name: "Demo", slug: "demo", role: "admin" },
    { id: "ws2", name: "Spare", slug: "spare", role: "admin" },
  ],
  chip_status: null, keys: [], domains: [], webhooks: [], social: [], einvoice: null,
  products: [], summary: { counts: {}, revenue_sen: 0 },
  sales: { items: [], page: 1, limit: 20, total: 0 },
  form_analytics: [], product_analytics: [],
};

const CHECK = {
  id: "ws1", name: "Demo", slug: "demo", is_owner: true, role: "admin",
  last_workspace: false, empty: true, blockers: [], can_delete: true,
};

/**
 * stubAPI stubs the dashboard so /settings reaches the Workspace tab without a
 * Go server. `trash` is the exact body the trash route answers with.
 */
async function stubAPI(page: Page, trash: unknown) {
  const json = (status: number, body: unknown) => ({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  // Catch-all FIRST, specific routes after it: Playwright matches a later
  // registration first, so the specific ones below win.
  await page.route(/(.*)\/api\/.*$/, (r) => r.fulfill(json(200, {})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(200, BUNDLE)));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(200, ME)));
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json(200, { lang: "en" })));
  await page.route(/(.*)\/api\/rest\/currency\/rates$/, (r) => r.fulfill(json(200, { rates: [], cached: false })));
  await page.route(/(.*)\/api\/workspaces\/[^/]+\/deletion-check(\?.*)?$/, (r) => r.fulfill(json(200, CHECK)));
  await page.route(/(.*)\/api\/workspaces\/trash(\?.*)?$/, (r) => r.fulfill(json(200, trash)));
}

/** Opens /settings and CLICKS the Workspace tab, with a FRESH browser state. */
async function openWorkspaceTab(page: Page, trash: unknown) {
  await page.addInitScript(() => {
    // Cleared first: jf_lang / jf_workspace left by an earlier spec would make
    // this render a state no new visitor has.
    try {
      localStorage.clear();
      sessionStorage.clear();
      localStorage.setItem("jf_workspace", "demo");
      localStorage.setItem("jf_lang", "en");
      // Preselect the tab too. `role` (which gates the admin-only tabs) resolves
      // from the session bundle asynchronously, so the first paint can render a
      // strip WITHOUT the Workspace tab at all — and a helper that then has to
      // click its way in is racing the bundle rather than testing the trash.
      // Preselecting it is the same "fresh but already-signed-in" state a user
      // who was on this tab has.
      localStorage.setItem("jf_settings_tab", "workspace");
    } catch {
      /* storage unavailable */
    }
  });
  await stubAPI(page, trash);
  await page.goto("/settings", { waitUntil: "domcontentloaded" });

  // Wait for the session bundle to resolve an ADMIN role BEFORE clicking.
  //
  // `settings/page.tsx` gates the Workspace section on `isAdmin` (`tab ===
  // "workspace" && isAdmin`), and `role` arrives with the session bundle. The
  // first paint therefore has a 10-tab strip that does not include the two
  // admin-only tabs, and clicking Workspace in that state renders NOTHING —
  // which a version of this helper turned into a 60s click timeout that read
  // like a broken page. The admin-only CHIP tab is the page's OWN visible marker
  // that the role has landed, so waiting for it is derived from the page rather
  // than pinned to a tab count that a new tab would invalidate.
  await expect(page.getByRole("tab", { name: DICT["set.tab.chip"].en })).toBeVisible({ timeout: 45_000 });

  await page.getByRole("tab", { name: DICT["set.tab.workspace"].en }).click();
  await expect(page.getByRole("heading", { name: DICT["ws.dangerTitle"].en })).toBeVisible({ timeout: 45_000 });
}

test("a binned workspace renders in the Trash view with a working Restore", async ({ page }) => {
  test.setTimeout(120_000);
  await openWorkspaceTab(page, BINNED);

  // RULE 1 — both rows are on screen, by their own name and slug. A page reading
  // an envelope renders neither, and fails here.
  const first = page.locator("li", { hasText: "binned-shop" }).first();
  const second = page.locator("li", { hasText: "sold-shop" }).first();
  await expect(first).toBeVisible({ timeout: 20_000 });
  await expect(second).toBeVisible({ timeout: 20_000 });
  await expect(first).toContainText("Binned shop");
  await expect(first).toContainText(DICT["ws.trashDeletedAt"].en);

  // The empty-bin sentence must NOT be on screen while rows are listed — that
  // is exactly what the old code showed instead of the rows.
  await expect(page.getByText(DICT["ws.trashEmpty"].en)).toHaveCount(0);

  // RULE 2 — a row that cannot be purged says why, with its own count.
  await expect(second).toContainText(DICT["ws.trashHeldBySales"].en.replace("{count}", "4"));

  // RULE 3 — Restore exists per row and addresses THAT row's id (the wire's
  // `id`, which is what the route expects). The request is intercepted, so the
  // assertion is on the URL the page built, not on a stub's side effect.
  const restores = page.getByRole("button", { name: DICT["ws.trashRestore"].en, exact: true });
  await expect(restores).toHaveCount(2);

  const requested: string[] = [];
  await page.route(/(.*)\/api\/workspaces\/[^/]+\/restore(\?.*)?$/, (r) => {
    requested.push(new URL(r.request().url()).pathname);
    return r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ status: "restored" }) });
  });
  // The FIRST row's button. `doRestore` reloads the page on success, so this
  // test asserts the CALL rather than waiting for a settled page afterwards.
  await first.getByRole("button", { name: DICT["ws.trashRestore"].en, exact: true }).click();
  await expect.poll(() => requested.length, { timeout: 20_000 }).toBeGreaterThan(0);
  expect(requested[0]).toBe("/api/workspaces/ws_binned_1/restore");
});

test("an empty bin still renders its own sentence (the guard above is not vacuous)", async ({ page }) => {
  test.setTimeout(120_000);
  await openWorkspaceTab(page, []);

  await expect(page.getByRole("heading", { name: DICT["ws.trashTitle"].en })).toBeVisible();
  await expect(page.getByText(DICT["ws.trashEmpty"].en)).toBeVisible({ timeout: 20_000 });
  await expect(page.getByRole("button", { name: DICT["ws.trashRestore"].en, exact: true })).toHaveCount(0);
});

// ─────────────────────────────────────────────────────────────────────────────
// t_ab4aa85b — the Danger zone's DELETE refusal, the same defect class as the
// /forms trash refusals (web/e2e/forms-trash-refusal.spec.ts).
//
// THE DEFECT. `doDelete()` wrote the refusal into `err` and then called
// `await load()`, and `load()` ends with `setErr("")` on a successful reload —
// so the sentence was set and erased in the same tick. Measured on prod
// (v1.19.0 @ 87040114, reproduced by QA before this fix, and reachable in the
// ordinary way: the workspace is renamed out of band while the dialog is open,
// so the slug the user typed is no longer the slug the server has — the server
// answers 400 and the workspace is NOT trashed): `alerts: []` immediately after,
// `alerts: []` at ~3s, `alerts: []` at the end. The merchant watched the click
// do nothing.
//
// WHAT THIS GUARD PROVES, on the real built page with a stubbed API:
//   1. the refusal renders the page's OWN localized sentence (the 400 carries
//      only the class code `request.badRequest`, which the dashboard
//      deliberately does not translate — see web/src/lib/apiErr.ts), and
//   2. the server's Malay prose appears nowhere on the page, and
//   3. the sentence is still on screen 3s later, sampled every 100ms, and
//   4. it is NOT hidden behind the dialog (the refusal closes the dialog) and
//      the Danger zone is still readable, and
//   5. the deletion-check really was refetched after the refusal — the reload
//      that used to wipe the sentence — so (3) is a claim about a completed
//      reload and not about one that never happened.
test("a refused workspace delete leaves its reason readable, not wiped by its own reload", async ({ page }) => {
  test.setTimeout(120_000);
  await openWorkspaceTab(page, []);

  // The server's own refusal body: 400 + the wire prose + the class code. The
  // prose is what the dashboard must never print; the code is unknown to this
  // build, so the page's own sentence is the answer.
  const REFUSAL_PROSE = "taip slug workspace (demo) untuk sahkan pemadaman";
  await page.route(/(.*)\/api\/workspaces\/[^/]+(\?.*)?$/, (r) => {
    if (r.request().method() !== "DELETE") return r.fallback();
    return r.fulfill({
      status: 400,
      contentType: "application/json",
      body: JSON.stringify({ error: REFUSAL_PROSE, code: "request.badRequest" }),
    });
  });

  // Counted here, and registered after the stub's own route, so it wins: the
  // reload `doDelete` fires on a refusal is what the message has to survive.
  let checks = 0;
  await page.route(/(.*)\/api\/workspaces\/[^/]+\/deletion-check(\?.*)?$/, (r) => {
    checks += 1;
    return r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(CHECK) });
  });

  // The Danger zone's own button (the dialog's confirm carries a different
  // label, so the two never collide).
  await page.getByRole("button", { name: DICT["ws.deleteButton"].en, exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog).toBeVisible({ timeout: 20_000 });
  await dialog.locator("#ws-confirm-slug").fill(CHECK.slug);
  const checksBefore = checks;
  await dialog.getByRole("button", { name: DICT["ws.confirmButton"].en, exact: true }).click();

  // The refusal renders in the Danger zone; the dialog is closed with it, so the
  // sentence is not behind an overlay.
  const line = page.getByRole("alert").filter({ hasText: DICT["ws.deleteFail"].en.slice(0, 20) });
  await expect(line, "the delete refusal must render a message at all").toBeVisible({ timeout: 20_000 });
  await expect(dialog, "the refusal closes the dialog rather than hiding the reason behind it").toHaveCount(0);

  // A LOCATOR READ MUST NOT BE ABLE TO HANG (t_155bdc39, MEASURED — not a
  // tidy-up). On the PRE-FIX shape this refusal is a FLASH: `err` is set and
  // then `await load()`'s `setErr("")` removes it within a few ms, so the
  // `toBeVisible` above can WIN that race. The read that followed was an
  // UNBOUNDED `innerText()` and then waited the WHOLE test timeout (120s) for a
  // node that never comes back, so the test died with
  //
  //     Error: locator.innerText: Test timeout of 120000ms exceeded.
  //
  // — an Error line that names NO assertion AND aborts the test before the
  // sampling leg below, which is the leg the bite belongs to. The bite harness
  // reads the reporter's Error block for a named red
  // (`web/scripts/biteproof_forms_trash_refusal.sh`, `run_case`), so it printed
  // `DETECTED, but NOT at a named assertion` and `SOME CASES FAILED` — a false
  // failure on a tree whose fix is correct. The settle makes the wipe
  // deterministic and the repeated assertion owns it under the SAME label the
  // case already accepts; the bounded read below then only ever runs against a
  // node that is really there. This is the shape
  // `web/e2e/forms-bulk-failure.spec.ts` already carries (re-anchored in this
  // same PR for this same mechanism).
  await page.waitForTimeout(1_000);
  await expect(line, "the delete refusal must render a message at all").toBeVisible({ timeout: 5_000 });
  const shown = ((await line.first().textContent({ timeout: 5_000 }).catch(() => null)) ?? "").trim();
  expect(shown).toContain(DICT["ws.deleteFail"].en);
  expect(shown).not.toContain(REFUSAL_PROSE);
  await expect(page.locator("html"), "the reading is the ENGLISH default").toHaveAttribute("lang", "en");
  await expect(
    page.getByRole("heading", { name: DICT["ws.dangerTitle"].en }),
    "the Danger zone stays on screen with the reason",
  ).toBeVisible();

  // THE CARD'S ACCEPTANCE (t_ab4aa85b): still there 3s later, sampled every
  // 100ms — the defect is a set-then-WIPED message, and a single late read
  // cannot tell "never rendered" from "rendered and replaced".
  const samples: boolean[] = [];
  for (let i = 0; i < 30; i += 1) {
    await page.waitForTimeout(100);
    samples.push((await line.count()) > 0 && (await line.first().isVisible().catch(() => false)));
  }
  expect(
    samples.every(Boolean),
    `the delete refusal must survive the whole 3s window. Samples (100ms apart): ` +
      `${samples.map((s) => (s ? "1" : "0")).join("")}. A 0 after a 1 is the wipe this card fixes ` +
      `(last missing at sample ${samples.lastIndexOf(false)}); all-zero means it never rendered.`,
  ).toBe(true);
  // Bounded for the same reason as the read above: every sample in the window
  // has already asserted this node is on screen, so the read cannot be the leg
  // that hangs (t_155bdc39).
  const finalText = ((await line.first().textContent({ timeout: 5_000 }).catch(() => null)) ?? "").trim();
  expect(finalText).toContain(DICT["ws.deleteFail"].en);
  expect(finalText).not.toContain(REFUSAL_PROSE);

  // ATTRIBUTION: the refusal DID refetch the deletion check — the reload that
  // used to erase the sentence — so the survival above is about a completed
  // reload. (If a future change stops refetching on a refusal, this is the leg
  // to drop; the window assertion is the requirement.)
  expect(checks, `a refused delete must still refetch the deletion check (before the press: ${checksBefore})`)
    .toBeGreaterThan(checksBefore);

  console.log(
    `t_ab4aa85b evidence (workspace delete): alert="${finalText}" | window(100ms x30) present | ` +
      `deletionChecks=${checks}(before ${checksBefore}) lang=en prose_present=false`,
  );
});

// ─────────────────────────────────────────────────────────────────────────────
// t_fe8a5d78 — the OTHER half of the same refusal: the dialog must be USABLE
// again, not stuck on "Working…".
//
// THE DEFECT. `doDelete()` opens with `setBusy(true)` and the `!res.ok` branch
// set the refusal, closed the dialog and returned — without clearing `busy`.
// Only the `catch` cleared it, i.e. a TRANSPORT failure, never a refusal. The
// refusal was readable, so the guard above passed, but every control in the
// dialog is gated on `busy`:
//
//   input#ws-confirm-slug   disabled={busy}
//   Cancel                  disabled={busy}
//   confirm button          disabled={!canDelete}, canDelete = … && !busy,
//                           and its label is `common.saving` ("Working…") while busy
//   the overlay             onClick={() => !busy && setConfirmOpen(false)}
//
// The Danger zone's own button is NOT gated on `busy`, so the merchant could
// reopen a DEAD dialog: no retry, no Cancel, no overlay dismiss — Escape was the
// only exit and the action needed a full page reload. Reachable exactly in the
// stale-slug race this refusal exists for (the workspace is renamed out of band
// while the dialog is open, so the typed slug no longer matches).
//
// WHAT THIS GUARD PROVES, on the real built page with a stubbed API:
//   1. after a refused DELETE the refusal is readable (bounded reads only — see
//      below), and
//   2. reopening it leaves the dialog USABLE: the slug input, Cancel and — once
//      the typed slug matches again — the confirm button are enabled, and the
//      dialog no longer reads "Working…".
//
// EVERY locator read on this path is BOUNDED — settle 1s, re-assert the SAME
// label with a 5s budget, then `textContent({ timeout: 5_000 })`. That is the
// shape `web/e2e/forms-trash-refusal.spec.ts` carries as `settleAndReadRefusal`,
// and the shape the refusal guard above was re-anchored to (t_155bdc39): a
// pre-fix red must land on the NAMED assertion below, never die at a 120s
// locator timeout that names nothing.
test("a refused workspace delete leaves the dialog usable again, not stuck on Working…", async ({ page }) => {
  test.setTimeout(120_000);
  await openWorkspaceTab(page, []);

  // The same server refusal the guard above stubs: 400 + the wire prose + the
  // class code. `fallback()` keeps this route from shadowing the deletion-check
  // and trash stubs for every non-DELETE request.
  const REFUSAL_PROSE = "taip slug workspace (demo) untuk sahkan pemadaman";
  await page.route(/(.*)\/api\/workspaces\/[^/]+(\?.*)?$/, (r) => {
    if (r.request().method() !== "DELETE") return r.fallback();
    return r.fulfill({
      status: 400,
      contentType: "application/json",
      body: JSON.stringify({ error: REFUSAL_PROSE, code: "request.badRequest" }),
    });
  });

  const danger = page.getByRole("button", { name: DICT["ws.deleteButton"].en, exact: true });
  await danger.click();
  const dialog = page.getByRole("dialog");
  await expect(dialog).toBeVisible({ timeout: 20_000 });
  const slugInput = dialog.locator("#ws-confirm-slug");
  await slugInput.fill(CHECK.slug);
  await dialog.getByRole("button", { name: DICT["ws.confirmButton"].en, exact: true }).click();

  // 1. THE REFUSAL IS READABLE — bounded, in the shape above.
  const line = page.getByRole("alert").filter({ hasText: DICT["ws.deleteFail"].en.slice(0, 20) });
  await expect(line, "the delete refusal must render a message at all").toBeVisible({ timeout: 20_000 });
  await page.waitForTimeout(1_000);
  await expect(line, "the delete refusal must render a message at all").toBeVisible({ timeout: 5_000 });
  const shown = ((await line.first().textContent({ timeout: 5_000 }).catch(() => null)) ?? "").trim();
  expect(shown).toContain(DICT["ws.deleteFail"].en);
  expect(shown).not.toContain(REFUSAL_PROSE);
  await expect(dialog, "the refusal closes the dialog rather than hiding the reason behind it").toHaveCount(0);

  // 2. REOPEN from the Danger zone's OWN button — the control that is NOT gated
  // on `busy`, which is how the stuck state is reachable at all.
  await danger.click();
  await expect(dialog, "the dialog must reopen after a refusal").toBeVisible({ timeout: 20_000 });

  // THE CARD'S ACCEPTANCE (t_fe8a5d78). The input is asserted FIRST: on the
  // pre-fix shape `busy` is still true, so it is `disabled` and this reds HERE,
  // by name, before any `fill()` could merely time out.
  await expect(
    slugInput,
    "the slug input must be enabled again — busy was not cleared on the refusal path",
  ).toBeEnabled({ timeout: 5_000 });
  await expect(
    dialog.getByRole("button", { name: DICT["common.cancel"].en, exact: true }),
    "Cancel must be enabled again — busy was not cleared on the refusal path",
  ).toBeEnabled({ timeout: 5_000 });

  // The confirm button is gated on `canDelete`, which needs the typed slug to
  // match again, so type it: a dialog that "opened" but cannot be submitted is
  // not usable.
  await slugInput.fill(CHECK.slug);
  const confirm = dialog.getByRole("button", { name: DICT["ws.confirmButton"].en, exact: true });
  await expect(
    confirm,
    "the confirm button must offer the action again once the typed slug matches",
  ).toBeEnabled({ timeout: 5_000 });
  await expect(
    dialog.getByRole("button", { name: DICT["common.saving"].en, exact: true }),
    "the dialog must not still read Working… (busy stuck true)",
  ).toHaveCount(0);

  // The refusal itself survives the reopen: `deleteErr` is cleared by the user's
  // NEXT delete press, not by looking at the dialog again.
  const keptText = ((await line.first().textContent({ timeout: 5_000 }).catch(() => null)) ?? "").trim();
  expect(
    keptText,
    "the refusal must stay readable after the dialog is reopened — it is cleared by the next action, not by this one",
  ).toContain(DICT["ws.deleteFail"].en);

  console.log(
    `t_fe8a5d78 evidence (dialog usable after refusal): alert="${keptText}" | ` +
      `input=enabled cancel=enabled confirm=enabled label="${DICT["ws.confirmButton"].en}" not working…`,
  );
});

// ─────────────────────────────────────────────────────────────────────────────
// t_8030247d — the refusal must survive the LOAD its own action fires, when that
// load FAILS. The second surface of the defect card t_d9dda884 fixed on /forms'
// Trash view; filed separately because it is a different surface.
//
// THE DEFECT. `doDelete()` writes the refusal into `deleteErr` and then
// `await load()`s — on purpose, so a stale `check.slug` (the workspace was renamed
// out of band while the dialog was open) cannot leave the merchant typing a slug
// the server no longer has. `load()`'s FAILURE branch sets `check` to null, and the
// `!check` early return renders the load-failure card as the WHOLE PAGE. So a
// refusal followed by a failing refetch (a transient 5xx / offline) replaced the
// reason with "We couldn't check this workspace. Please try again." and the
// merchant never read WHY the workspace was not deleted — the sentence the action
// exists to deliver. The trash-RESTORE refusal lives on this same tab and is lost
// through the same branch: any later check reload that fails takes the Trash
// section, and `trashErr` with it, off the page. ONE SITE, TWO REFUSALS.
//
// WHAT THESE GUARDS PROVE, on the real built page with a stubbed API:
//   A. the delete refusal is on screen after the refetch it fires has FAILED, the
//      load failure is ALSO reported beside it (one does not replace the other),
//      and the Retry affordance the failure card exists for is still offered;
//   B. the same for the trash-RESTORE refusal, in the same failure state, with the
//      delete refusal beside it — the attribution for "one site, two refusals".
//
// Both are deterministic (no race): the failing refetch is the only thing that can
// set `check` to null after a successful render, which is why a single settled read
// is the evidence here and not a sampled window.
const DELETE_REFUSAL_PROSE = "taip slug workspace (demo) untuk sahkan pemadaman";

/**
 * Stubs the two failures this card needs: the action is REFUSED by the server, and
 * the deletion-check refetch the refusal fires then FAILS.
 *
 * Registered AFTER `openWorkspaceTab` has rendered the tab, so the first (successful)
 * read is behind us and every later read answers 500 — the shape a transient 5xx or
 * an offline browser produces. Playwright matches the LAST registered route first, so
 * these win over the ones `stubAPI` installed.
 */
async function refuseThenFailTheRefetch(page: Page, extraRefusal?: { method: string; path: RegExp }) {
  const json = (status: number, body: unknown) => ({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });

  // `/api/workspaces/{id}` — the DELETE refusal. `fallback()` keeps this from
  // shadowing the trash listing (`/api/workspaces/trash`, which this pattern also
  // matches) and every other verb.
  const refused: string[] = [];
  await page.route(/(.*)\/api\/workspaces\/[^/]+(\?.*)?$/, (r) => {
    if (r.request().method() !== "DELETE") return r.fallback();
    refused.push(new URL(r.request().url()).pathname);
    return r.fulfill(json(400, { error: DELETE_REFUSAL_PROSE, code: "request.badRequest" }));
  });

  // A second refused ACTION on the same tab (the trash Restore), when a leg needs
  // one: it must be refused BEFORE the delete, because the delete's failing refetch
  // is what takes the page down.
  if (extraRefusal) {
    await page.route(extraRefusal.path, (r) => {
      if (r.request().method() !== extraRefusal.method) return r.fallback();
      refused.push(new URL(r.request().url()).pathname);
      return r.fulfill(json(400, { error: "tidak dibenarkan", code: "request.badRequest" }));
    });
  }

  // THE FAILURE UNDER TEST: every deletion-check read from here on answers 500.
  let failedChecks = 0;
  await page.route(/(.*)\/api\/workspaces\/[^/]+\/deletion-check(\?.*)?$/, (r) => {
    failedChecks += 1;
    return r.fulfill(json(500, { error: "ralat dalaman", code: "internal" }));
  });

  return { refused, failures: () => failedChecks };
}

/** Opens the confirm dialog and submits it with the matching slug. */
async function submitDelete(page: Page) {
  await page.getByRole("button", { name: DICT["ws.deleteButton"].en, exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog).toBeVisible({ timeout: 20_000 });
  await dialog.locator("#ws-confirm-slug").fill(CHECK.slug);
  await dialog.getByRole("button", { name: DICT["ws.confirmButton"].en, exact: true }).click();
}

test("a refused workspace delete keeps its reason when the refetch it fires FAILS", async ({ page }) => {
  test.setTimeout(120_000);
  await openWorkspaceTab(page, []);
  const { failures } = await refuseThenFailTheRefetch(page);
  await submitDelete(page);

  // THE CARD'S ACCEPTANCE (t_8030247d). The refusal must be readable even though
  // the refetch it fired has failed and taken the `check` state with it.
  const refusal = page.getByRole("alert").filter({ hasText: DICT["ws.deleteFail"].en });
  await expect(refusal, "the refusal must stay readable when its own refetch FAILS").toBeVisible({
    timeout: 20_000,
  });
  // A settled re-read: the pre-fix shape renders NOTHING here, so this is a claim
  // about a page that stayed, not about a flash that was caught.
  await page.waitForTimeout(1_000);
  await expect(refusal, "the refusal must stay readable when its own refetch FAILS").toBeVisible({
    timeout: 5_000,
  });
  const shown = ((await refusal.first().textContent({ timeout: 5_000 }).catch(() => null)) ?? "").trim();
  expect(shown).toContain(DICT["ws.deleteFail"].en);
  expect(shown).not.toContain(DELETE_REFUSAL_PROSE);

  // The failure the branch renders must NOT replace it — both are on screen.
  await expect(
    page.getByRole("alert").filter({ hasText: DICT["ws.loadFail"].en }),
    "the load failure must still be reported beside the refusal",
  ).toBeVisible({ timeout: 5_000 });
  await expect(
    page.getByRole("button", { name: DICT["common.retry"].en, exact: true }),
    "the Retry affordance must still be offered",
  ).toBeVisible({ timeout: 5_000 });
  await expect(page.locator("html"), "the reading is the ENGLISH default").toHaveAttribute("lang", "en");

  // ATTRIBUTION: the refusal really did refetch, and that refetch really FAILED —
  // so the survival above is about a completed FAILING reload.
  expect(
    failures(),
    "a refused delete must still refetch the deletion check, and this stub must have failed it",
  ).toBeGreaterThan(0);

  console.log(
    `t_8030247d evidence (delete refusal + failed refetch): alert="${shown}" | ` +
      `load failure readable | retry offered | failed deletionChecks=${failures()} lang=en`,
  );
});

test("a refused trash restore keeps its reason readable beside the load failure", async ({ page }) => {
  test.setTimeout(120_000);
  await openWorkspaceTab(page, BINNED);
  const { refused } = await refuseThenFailTheRefetch(page, {
    method: "POST",
    path: /(.*)\/api\/workspaces\/[^/]+\/restore(\?.*)?$/,
  });

  // 1. The RESTORE is refused, so its sentence is in `trashErr` — on the Trash
  //    section, where it is readable at this point in both the fixed and the
  //    pre-fix code.
  const restoreBtn = page
    .locator("li", { hasText: "binned-shop" })
    .first()
    .getByRole("button", { name: DICT["ws.trashRestore"].en, exact: true });
  await restoreBtn.click();
  await expect.poll(() => refused.length, { timeout: 20_000 }).toBeGreaterThan(0);

  // 2. A refused DELETE then fails its own refetch, which nulls `check` and takes
  //    the whole page — Trash included — down to the load-failure card.
  await submitDelete(page);
  await expect(
    page.getByRole("alert").filter({ hasText: DICT["ws.loadFail"].en }),
    "the load failure must be reported",
  ).toBeVisible({ timeout: 20_000 });

  // THE CARD'S ACCEPTANCE (t_8030247d): the restore refusal the merchant was just
  // shown is still readable in that failure state, beside the delete refusal.
  const restoreRefusal = page.getByRole("alert").filter({ hasText: DICT["ws.trashRestoreFail"].en });
  await expect(
    restoreRefusal,
    "the refused restore must stay readable beside the load failure",
  ).toBeVisible({ timeout: 5_000 });
  await expect(
    page.getByRole("alert").filter({ hasText: DICT["ws.deleteFail"].en }),
    "the delete refusal must stay readable too — one branch, two refusals",
  ).toBeVisible({ timeout: 5_000 });
  await page.waitForTimeout(1_000);
  const kept = ((await restoreRefusal.first().textContent({ timeout: 5_000 }).catch(() => null)) ?? "").trim();
  expect(kept).toContain(DICT["ws.trashRestoreFail"].en);
  await expect(page.locator("html"), "the reading is the ENGLISH default").toHaveAttribute("lang", "en");

  console.log(
    `t_8030247d evidence (restore refusal + failed refetch): alert="${kept}" | ` +
      `delete refusal readable | load failure readable | lang=en`,
  );
});
