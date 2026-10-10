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
// all. No credential is fabricated, so this stays a gate: `npm run a11y` runs the
// whole `e2e` directory, so these tests run wherever the public scan runs.
//
// WHAT IS PINNED, AND WHAT MAKES IT BITE. Each dialog is (1) asserted to be
// reachable at all — a selector that stops matching FAILS rather than skipping,
// because a guard that silently passes when its subject vanished is the exact
// hole this card is about — and (2) scanned in isolation with the repo's own
// axe-core, failing on critical/serious. The confirm button's disabled state is
// asserted at three points, not one: EMPTY (on open), a WRONG slug of the same
// length (still disabled), and the EXACT slug (enabled). Until card t_b2463a72
// only the EMPTY state was measured while the message and the docs claimed "the
// exact slug", so a confirm armed by any non-empty text — `typed.trim().length
// === 0` in place of the slug comparison on either dialog — shipped green; both
// halves of the boundary are what make that regression fail now.
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
// happy path the delete button is enabled on — the card's own subject is the
// dialog, so the disabled-state variants are the feature tests' business, not
// this file's.
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
async function stubAPI(page: Page) {
  const json = (body: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
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
    r.fulfill(json(DELETION_CHECK)),
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
    /** The real slug the server will be asked to confirm. */
    slug: string;
    /** A different slug of the SAME length — see the boundary assertion below. */
    wrongSlug: string;
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

  // (3) THE EXACT SLUG arms it. The other side of the boundary: without this a
  // permanently-disabled confirm would satisfy (1) and (2) and the dialog would
  // be a dead end no test noticed.
  await input.fill(opts.slug);
  await expect(confirm, "confirm must be enabled once the exact slug is typed").toBeEnabled();

  // Escape closes the dialog — the only keyboard way out of these modals.
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
      slug: WS_SLUG,
      wrongSlug: WS_WRONG_SLUG,
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
      slug: FORM_SLUG,
      wrongSlug: FORM_WRONG_SLUG,
    });
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
