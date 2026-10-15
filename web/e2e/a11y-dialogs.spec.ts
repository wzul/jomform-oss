// Accessibility guard for the two TYPED-SLUG confirmation dialogs.
//
// WHY THIS FILE EXISTS (card t_d334fb02). PR #682 added two modal dialogs that
// destroy data: the workspace-delete confirm (Settings → Workspace) and the
// form-purge confirm (/forms → Trash). Both are built correctly — `role="dialog"`
// + `aria-modal="true"` + `aria-labelledby` resolving to a real heading, focus in
// the confirm input on open, the confirm button disabled until the exact slug is
// typed. But the axe gate in `web/e2e/a11y.spec.ts` scans only the
// UNAUTHENTICATED public routes it lists there (`/login`, `/landing`, …), and
// both dialogs live behind a session, so NEITHER of them was ever scanned. The
// coverage was inferred from a checklist row, not measured, and the gap was
// silent: an edit that dropped the `aria-labelledby` off either dialog would
// have shipped with a green a11y gate.
//
// This file deliberately does not edit `a11y.spec.ts` or its PAGES list — the
// dialogs are not routes, and adding a second spec beside it keeps this guard
// independent of changes to that list.
//
// HOW THE DIALOG IS REACHED WITHOUT A SESSION COOKIE. `playwright.config.ts`
// says the authenticated surfaces "require a live session cookie from the Go API,
// which the scan does not provision" — true, and it is why the pages were left
// out. It is not true of the DIALOG: an authenticated surface renders from the
// browser's `dashboard-bundle` fetch, so stubbing that fetch at the browser level
// (the pattern `e2e/affiliate-row.spec.ts` already uses — "no Go server, no
// session, no DB") puts the real component in a real browser with no backend at
// all. No credential is fabricated, so this stays a gate: the `a11y` project
// selects its fixtures by FILENAME PREFIX, so this file (`a11y-dialogs.spec.ts`)
// runs in `npm run a11y` alongside the public-page scan in `a11y.spec.ts`.
//
// WHAT IS PINNED, AND WHAT MAKES IT BITE. Each dialog is (1) asserted to be
// reachable at all — a selector that stops matching FAILS rather than skipping,
// because a guard that silently passes when its subject vanished is the exact
// hole this card is about — and (2) scanned in isolation with the repo's own
// axe-core, failing on critical/serious. The confirm button's disabled state is
// asserted at SIX points, not one: EMPTY (on open); a WRONG slug of the same
// length (still disabled); a PREFIX of the real slug (still disabled); the exact
// slug in INVERTED CASE (still disabled); the exact slug PADDED with whitespace
// (enabled — the shipped `trim()`); and the EXACT slug (enabled). Until card
// t_b2463a72 only the EMPTY state was measured while the message and the docs
// claimed "the exact slug", so a confirm armed by any non-empty text —
// `typed.trim().length === 0` in place of the slug comparison on either dialog —
// shipped green; both halves of the boundary are what make that regression fail
// now. The probes between the wrong-slug one and the exact one are card
// t_2b129c85: a wrong slug of the same length cannot see a comparison that is
// `startsWith` rather than equality, nor one that case-folds `toLowerCase()` on
// both sides — both armed the confirm on a slug the user never typed and passed
// every assertion this file had — so the SHAPE of the comparison is typed at it.
//
// The other two terms of "the confirm is armed" are measured as well, because
// neither was: the `busy` term (the double-submit guard, held open below) and,
// on the workspace dialog, the server's `can_delete` — the second one by the
// source guard `src/lib/workspaceDeleteConfirmGate.test.ts`, because NO runtime
// state can reach it (the dialog's trigger is itself disabled when the server
// withholds `can_delete`, and react-dom refuses the handler of a disabled
// control even after its DOM attribute is stripped — `shouldPreventMouseEvent`,
// props-based: facebook/react#8308). This file still pins the half that IS
// reachable: with `can_delete: false` the dialog is not offered at all.
//
// The isolation is deliberate:
// `AxeBuilder().include()` scopes axe to the dialog subtree, and axe THROWS
// ("No elements found for include in page Context") when that subtree is not
// there, so the scan cannot pass vacuously. The structural assertions are not
// redundant with axe: axe reports a dialog with no accessible name as
// `aria-dialog-name`, but a dialog that lost `role="dialog"` altogether is simply
// not a dialog to axe — it would be scanned as an ordinary div and pass. The
// role/name/aria-modal assertions are what catch that, and the escape/focus
// assertions cover the keyboard contract axe cannot measure at all.
//
// Run with:
//
//   npm run a11y            # the whole gate (this file + the public-page scan)
//   npx playwright test e2e/a11y-dialogs.spec.ts     # this file alone
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
import AxeBuilder from "@axe-core/playwright";
import type { AxeResults, Result } from "axe-core";
import { DICT } from "../src/components/i18nDict";

// The gate's own rule: critical/serious fail, everything else is reported for
// triage. Kept identical to e2e/a11y.spec.ts so the two cannot drift.
const IMPACTS = ["critical", "serious"] as const;

/** The critical/serious subset of an axe result. */
function blockersOf(results: AxeResults): Result[] {
  return results.violations.filter((v) =>
    IMPACTS.includes((v.impact ?? "") as (typeof IMPACTS)[number]),
  );
}

// The workspace-delete dialog's copy, from the app's own dictionary — so an
// assertion below compares against the string the app is actually shipping in
// English, never a copy typed into this file.
const EN = {
  wsConfirmTitle: DICT["ws.confirmTitle"].en,
  wsConfirmFieldLabel: DICT["ws.confirmFieldLabel"].en,
  wsDeleteButton: DICT["ws.deleteButton"].en,
  purgeConfirmTitle: DICT["forms.purgeConfirmTitle"].en,
  purgeConfirmFieldLabel: DICT["forms.purgeConfirmFieldLabel"].en,
  purgeButton: DICT["forms.purge"].en,
  trashTab: DICT["forms.trashTabDeleted"].en,
};

// The workspace the stubs describe. `can_delete: true` with no blockers is the
// happy path the delete button is enabled on, and it is what the contract test
// drives. The `can_delete: false` answer — the server's refusal — is stubbed in
// exactly one place, the test at the bottom of this file (t_2b129c85).
const WORKSPACE_ID = "ws1";
const FORM_SLUG = "tee-2026";
const WS_SLUG = "demo";
// Wrong slugs of the SAME length as the real ones — the confirm must not arm on
// "something was typed", and a length-only comparison must be caught too. The
// lengths are tied to the real slugs in `assertDialogContract`.
const FORM_WRONG_SLUG = "xxxxxxxx";
const WS_WRONG_SLUG = "xxxx";

const BUNDLE = {
  me: { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "", is_super_admin: true },
  workspaces: [{ id: WORKSPACE_ID, name: "Demo", slug: WS_SLUG, role: "admin" }],
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
};

const DELETION_CHECK = {
  id: WORKSPACE_ID,
  name: "Demo",
  slug: WS_SLUG,
  is_owner: true,
  role: "admin",
  last_workspace: false,
  empty: true,
  blockers: [],
  can_delete: true,
};

const TRASHED_FORM = {
  id: "f1",
  slug: FORM_SLUG,
  title: "Tee order",
  deleted_at: "2026-09-20T00:00:00Z",
  sales_count: 0,
  can_purge: true,
};

/** Every /api/* call the two surfaces make, stubbed at the browser level. */
async function stubAPI(
  page: Page,
  /**
   * Override for the workspace deletion check. Default: the happy path above.
   * The only caller that passes one is the `can_delete: false` test at the
   * bottom of this file — the answer the server gives when the workspace must
   * not be deleted.
   */
  opts: { deletionCheck?: Record<string, unknown> } = {},
) {
  const json = (body: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  const deletionCheck = opts.deletionCheck ?? DELETION_CHECK;
  // Catch-all FIRST, specific routes after it: Playwright resolves matching
  // routes in reverse registration order, so the later (specific) ones win.
  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(BUNDLE)));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(BUNDLE.me)));
  // lang:"" (never set) keeps the browser's value in effect, which is how the
  // init script below pins the locale to en; the promotion path then POSTs and
  // the stub accepts it.
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "" })));
  await page.route(/(.*)\/api\/workspaces\/[^/]+\/deletion-check$/, (r) =>
    r.fulfill(json(deletionCheck)),
  );
  await page.route(/(.*)\/api\/workspaces\/[^/]+\/settings$/, (r) => r.fulfill(json({})));
  await page.route(/(.*)\/api\/workspaces\/[^/]+\/email-settings$/, (r) => r.fulfill(json({})));
  await page.route(/(.*)\/api\/workspaces\/[^/]+\/members(\?.*)?$/, (r) => r.fulfill(json([])));
  await page.route(/(.*)\/api\/workspaces\/[^/]+\/invites(\?.*)?$/, (r) => r.fulfill(json([])));
  await page.route(/(.*)\/api\/workspaces\/[^/]+\/affiliates(\?.*)?$/, (r) => r.fulfill(json([])));
  await page.route(/(.*)\/api\/rest\/forms\/trash(\?.*)?$/, (r) => r.fulfill(json([TRASHED_FORM])));
  await page.route(/(.*)\/api\/rest\/forms(\?.*)?$/, (r) => r.fulfill(json([])));
  await page.route(/(.*)\/api\/rest\/currency\/rates$/, (r) =>
    r.fulfill(json({ rates: [], cached: false })),
  );
}

/**
 * A FRESH browser state, English, with one workspace selected. Storage is
 * cleared first (the browser keeps localStorage between runs, and a `jf_lang`
 * left behind by an earlier card would render these dialogs in a locale this
 * file never asserts) and the language is then pinned explicitly.
 */
async function freshState(page: Page) {
  await page.addInitScript(() => {
    try {
      localStorage.clear();
      localStorage.setItem("jf_workspace", "demo");
      localStorage.setItem("jf_lang", "en");
    } catch {
      /* storage unavailable */
    }
  });
}

/** Scans the live dialog subtree and fails on any critical/serious violation. */
async function scanDialog(page: Page, name: string) {
  const results = await new AxeBuilder({ page }).include('[role="dialog"]').analyze();
  const blockers = blockersOf(results);
  if (blockers.length > 0) {
    console.log(
      `\n[${name}] ${blockers.length} critical/serious violation(s):\n` +
        blockers
          .map(
            (v) =>
              `  - ${v.id} (${v.impact}): ${v.help}\n` +
              v.nodes
                .slice(0, 3)
                .map((n) => `      ${n.target.join(" ")} — ${n.failureSummary?.split("\n")[0] ?? ""}`)
                .join("\n"),
          )
          .join("\n"),
    );
  }
  expect(blockers.map((v) => v.id), `${name} must have no critical/serious axe violations`).toEqual([]);
}

/**
 * The contract both dialogs share. Asserted, not assumed — every one of these is
 * something a later edit could drop while leaving the other tests green.
 */
async function assertDialogContract(
  page: Page,
  opts: {
    title: string;
    fieldLabel: string;
    inputId: string;
    confirmName: string;
    /** The SAME button's label while a submit is in flight (`common.saving`). */
    savingName: string;
    /** The real slug the server will be asked to confirm. */
    slug: string;
    /** A different slug of the SAME length — see the boundary assertion below. */
    wrongSlug: string;
    /** The one request this dialog's confirm sends, held open in (4). */
    submitPath: RegExp;
  },
) {
  // The harness' own invariant: if `wrongSlug` were a different length, a gate
  // that compared only `typed.length === slug.length` would arm the confirm and
  // this guard would not see it. Asserting it here keeps a later edit to the
  // constants from quietly weakening the test.
  expect(
    opts.wrongSlug.length,
    "wrongSlug must be the same length as the real slug for the boundary assertion to bite",
  ).toBe(opts.slug.length);
  // The same shape of invariant for the probes below: each is only a probe if it
  // differs from the real slug in the way it claims to. A slug of one character
  // has no strict prefix, and a slug that is already upper-case (or already
  // padded) makes its probe a copy of the exact-slug assertion.
  expect(
    opts.slug.length,
    "the slug must be longer than one character for the prefix probe to bite",
  ).toBeGreaterThan(1);
  expect(
    opts.slug,
    "the slug must have a lower-case letter for the case-fold probe to bite",
  ).not.toBe(opts.slug.toUpperCase());
  expect(
    opts.slug.trim(),
    "the slug must carry no surrounding whitespace for the trim probe to bite",
  ).toBe(opts.slug);
  const dialog = page.locator('[role="dialog"]');
  await expect(dialog, "exactly one dialog is open").toHaveCount(1);
  await expect(dialog).toBeVisible();
  await expect(dialog, "a modal must be announced as such").toHaveAttribute("aria-modal", "true");

  // The accessible name: the labelled heading must exist and hold the shipped
  // English copy, so `aria-labelledby` cannot point at a moved/renamed node and
  // pass on a stale id lookup.
  const labelledBy = await dialog.getAttribute("aria-labelledby");
  expect(labelledBy, "the dialog must name itself with aria-labelledby").toBeTruthy();
  await expect(page.locator(`#${labelledBy}`)).toHaveText(opts.title);
  await expect(dialog).toHaveAccessibleName(opts.title);

  // Focus lands in the confirm field on open (it is `autoFocus`), and the field
  // is reachable by its label — an unassociated label is an unlabelled input.
  const input = page.locator(`#${opts.inputId}`);
  await expect(input).toBeFocused();
  await expect(page.getByLabel(opts.fieldLabel)).toHaveAttribute("id", opts.inputId);

  // The confirm action exists, is named, and is NOT armed on open. Scoped to
  // the dialog: the purge confirm and the row's own purge button carry the SAME
  // English label ("Delete for good"), so an unscoped lookup is ambiguous — and
  // scoping it also asserts the button really is inside the dialog.
  const confirm = dialog.getByRole("button", { name: opts.confirmName });
  await expect(confirm).toBeVisible();

  // (1) EMPTY — nothing typed. Kept: it is the state the dialog opens in, and a
  // confirm that was armed unconditionally fails here.
  await expect(confirm, "confirm must be disabled until the exact slug is typed").toBeDisabled();

  // (2) A WRONG SLUG OF THE SAME LENGTH. This is the boundary that actually
  // guards the write, and (1) never reaches it: the two implementations compare
  // `typed.trim()` against the real slug (`WorkspaceTab.tsx` canDelete,
  // `forms/page.tsx` purge), so a regression that arms the button for any
  // non-empty text — the shape measured shipping green on both dialogs —
  // passes (1) and destroys the wrong workspace / permanently purges the wrong
  // form's CASCADE children. Same length on purpose: a length-only comparison
  // must be caught too.
  await input.fill(opts.wrongSlug);
  await expect(
    confirm,
    `confirm must stay disabled for a wrong slug (typed "${opts.wrongSlug}", the real slug is "${opts.slug}")`,
  ).toBeDisabled();

  // (2b) A PREFIX OF THE REAL SLUG. A comparison of the shape
  // `slug.startsWith(typed.trim())` arms the confirm on the first character the
  // user types — one keystroke would destroy a workspace / purge a form the user
  // never named — and (2) cannot see it: the wrong slug above is not a prefix of
  // the real one. Strictly shorter than the real slug, pinned above.
  const prefix = opts.slug.slice(0, 1);
  await input.fill(prefix);
  await expect(
    confirm,
    `confirm must stay disabled for a PREFIX of the real slug (typed "${prefix}", the real slug is "${opts.slug}")`,
  ).toBeDisabled();

  // (2c) THE EXACT SLUG, INVERTED CASE. A comparison that case-folds both sides
  // (`typed.trim().toLowerCase() === slug.toLowerCase()`) arms the confirm on
  // "TEE-2026" — the right name in the wrong case, which is not what the server
  // accepts — and neither (2) nor (2b) sees it.
  const inverted = opts.slug.toUpperCase();
  await input.fill(inverted);
  await expect(
    confirm,
    `confirm must stay disabled for the right slug in the wrong case (typed "${inverted}", the real slug is "${opts.slug}")`,
  ).toBeDisabled();

  // (2d) THE EXACT SLUG PADDED WITH WHITESPACE ARMS IT. Pinned deliberately: the
  // app trims what it compares, so a pasted slug with a trailing space (or a
  // newline) is accepted. It is a decision, not an accident — a later change to
  // compare the raw value has to change this assertion, and say so.
  await input.fill(` ${opts.slug} `);
  await expect(
    confirm,
    `confirm must be enabled for the exact slug padded with whitespace (typed " ${opts.slug} ", the real slug is "${opts.slug}")`,
  ).toBeEnabled();

  // (3) THE EXACT SLUG arms it. The other side of the boundary: without this a
  // permanently-disabled confirm would satisfy (1) and (2) and the dialog would
  // be a dead end no test noticed.
  await input.fill(opts.slug);
  await expect(confirm, "confirm must be enabled once the exact slug is typed").toBeEnabled();

  // (4) IN FLIGHT — the double-submit guard. The confirm's `disabled` also
  // carries the app's own busy flag, and nothing measured it: dropping it
  // (`disabled={purgeTyped.trim() !== confirmPurge.slug}`, or `canDelete =
  // slugMatches` on the workspace dialog) passed every other assertion here, so
  // a double click would send the delete twice. The submit is HELD OPEN — the
  // route never answers — and the same button is then read through its OTHER
  // label, because the app swaps the copy while busy.
  const heldOpen: number[] = [];
  await page.route(opts.submitPath, () => {
    // Answering nothing: the request never lands, so the app stays busy for the
    // assertion below. (Playwright holds a request whose handler answers
    // neither fulfil nor abort; the page is torn down with it still in flight.)
    heldOpen.push(1);
  });
  await confirm.click();
  const inFlight = dialog.getByRole("button", { name: opts.savingName });
  await expect(
    inFlight,
    "the confirm must be off while one submit is in flight (a double click must not send it twice)",
  ).toBeDisabled();
  expect(
    heldOpen.length,
    "the submit must really have been held open, or the assertion above measured nothing",
  ).toBeGreaterThan(0);

  // Escape closes the dialog — the only keyboard way out of these modals. It is
  // asserted with the submit STILL in flight (the request is deliberately left
  // holding: releasing it as a failure is not equivalent on the two dialogs —
  // /forms replaces its whole body with the error card when `err` is set, so the
  // purge page would be measured as "Escape closed a dialog that no longer
  // exists"), and the modal must still be closable while busy.
  await page.keyboard.press("Escape");
  await expect(dialog, "Escape must close the dialog").toHaveCount(0);
}

test.describe("a11y: typed-slug confirmation dialogs", () => {
  test("workspace-delete dialog (Settings → Workspace)", async ({ page, baseURL }) => {
    await stubAPI(page);
    await freshState(page);
    await page.goto(`${baseURL}/settings?tab=workspace`, { waitUntil: "domcontentloaded" });

    // Reachability is asserted: if the danger-zone button stops being rendered
    // or stops opening the dialog, this FAILS. It must never quietly pass.
    const open = page.getByRole("button", { name: EN.wsDeleteButton });
    await expect(open, "the danger-zone delete button must be offered").toBeVisible({ timeout: 15_000 });
    await open.click();

    await assertDialogContract(page, {
      title: EN.wsConfirmTitle,
      fieldLabel: EN.wsConfirmFieldLabel,
      inputId: "ws-confirm-slug",
      confirmName: DICT["ws.confirmButton"].en,
      savingName: DICT["common.saving"].en,
      slug: WS_SLUG,
      wrongSlug: WS_WRONG_SLUG,
      // The dialog's DELETE — the workspace's own URL, built from the id the
      // stub describes, so it cannot drift from it.
      submitPath: new RegExp(`/api/workspaces/${WORKSPACE_ID}$`),
    });
  });

  test("form-purge dialog (/forms → Trash)", async ({ page, baseURL }) => {
    await stubAPI(page);
    await freshState(page);
    await page.goto(`${baseURL}/forms`, { waitUntil: "domcontentloaded" });

    await page.getByRole("tab", { name: EN.trashTab }).click();
    const open = page.getByRole("button", { name: EN.purgeButton });
    await expect(open, "the purge button must be offered for a purgeable form").toBeVisible({
      timeout: 15_000,
    });
    await open.click();

    await assertDialogContract(page, {
      title: EN.purgeConfirmTitle,
      fieldLabel: EN.purgeConfirmFieldLabel,
      inputId: "purge-confirm-slug",
      confirmName: EN.purgeButton,
      savingName: DICT["common.saving"].en,
      slug: FORM_SLUG,
      wrongSlug: FORM_WRONG_SLUG,
      submitPath: new RegExp(`/api/rest/forms/${TRASHED_FORM.id}/purge`),
    });
  });

  // The workspace dialog's confirm carries a SECOND gate the purge dialog has no
  // equivalent of: the server's `can_delete`, i.e. "may this workspace go at
  // all" (owner, not the last one). It is ANDed into `canDelete`
  // (`WorkspaceTab.tsx`), and it was measured nowhere: no test in `web/` ever
  // stubbed `can_delete: false`, so dropping the conjunct left everything green
  // (t_2b129c85, hole 3).
  //
  // WHAT THIS TEST PINS IS THE REACHABLE HALF. With `can_delete: false` the
  // danger-zone trigger is off, so the dialog is not offered — and that is as far
  // as a browser can be driven: the trigger is the ONLY way in, and react-dom
  // refuses the handler of a disabled control even after its `disabled`
  // attribute is stripped (`shouldPreventMouseEvent` reads the FIBER props, not
  // the DOM — facebook/react#8308; measured here: attribute removed,
  // `el.disabled === false`, and both a dispatched click and a real one did
  // nothing). The other way to reach the state, a re-read of the deletion check
  // while the dialog is open, was measured too: a scope change remounts the tree
  // (a two-tab `storage` probe lost the dialog within 250ms and re-ran every
  // page fetch). So the conjunct is unreachable AT RUNTIME, which is why it is
  // pinned at the source by `src/lib/workspaceDeleteConfirmGate.test.ts` — the
  // term still has to be there, and that guard fails when it is dropped.
  test("workspace-delete dialog: the server's can_delete is the outer gate", async ({
    page,
    baseURL,
  }) => {
    await stubAPI(page, { deletionCheck: { ...DELETION_CHECK, can_delete: false } });
    await freshState(page);
    await page.goto(`${baseURL}/settings?tab=workspace`, { waitUntil: "domcontentloaded" });

    const trigger = page.getByRole("button", { name: EN.wsDeleteButton });
    await expect(trigger, "the danger-zone delete button must be offered").toBeVisible({
      timeout: 15_000,
    });
    await expect(
      trigger,
      "the danger-zone delete button must be off when the server withholds can_delete",
    ).toBeDisabled();
    // ...and off means off: nothing was opened behind it.
    await expect(page.locator('[role="dialog"]'), "no dialog may be reachable").toHaveCount(0);
  });

  // The scan is a separate test from the contract so a failure names WHICH half
  // broke: structure/behaviour, or the axe rules (contrast, ARIA, roles).
  test("workspace-delete dialog has no critical/serious axe violations", async ({ page, baseURL }) => {
    await stubAPI(page);
    await freshState(page);
    await page.goto(`${baseURL}/settings?tab=workspace`, { waitUntil: "domcontentloaded" });
    await page.getByRole("button", { name: EN.wsDeleteButton }).click();
    await expect(page.locator('[role="dialog"]')).toBeVisible({ timeout: 15_000 });
    await scanDialog(page, "workspace-delete dialog");
  });

  test("form-purge dialog has no critical/serious axe violations", async ({ page, baseURL }) => {
    await stubAPI(page);
    await freshState(page);
    await page.goto(`${baseURL}/forms`, { waitUntil: "domcontentloaded" });
    await page.getByRole("tab", { name: EN.trashTab }).click();
    await page.getByRole("button", { name: EN.purgeButton }).click();
    await expect(page.locator('[role="dialog"]')).toBeVisible({ timeout: 15_000 });
    await scanDialog(page, "form-purge dialog");
  });
});
