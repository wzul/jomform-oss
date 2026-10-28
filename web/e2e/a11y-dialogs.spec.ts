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
// THE THIRD SURFACE — the /products Trash, which asks for the product's NAME
// (card t_eedd6522). It is the same destructive shape as the form bin and one
// rung wider, because `prod.purgeConfirmBody` says the product AND all of its
// variations go (CASCADE). It was the last of the three typed confirms with no
// browser coverage at all: this file drove exactly two inputs
// (`#ws-confirm-slug`, `#purge-confirm-slug`), and the a11y gate was green for
// the products dialog whatever its comparison did. A source guard cannot stand
// in for it here, because every conjunct of that dialog's `disabled` is
// REACHABLE at runtime (unlike the workspace dialog's server-side `can_delete`,
// which is why that one is pinned in `src/lib/workspaceDeleteConfirmGate.test.ts`
// instead). So the same harness covers it at the same six points, plus the
// in-flight state — which is why the harness speaks of a `value`/`wrongValue`
// and carries `what`: the word a failure message must name ("slug", "product
// name") differs per dialog, the boundary does not.
//
// Extending the suite to this dialog also FIXED it: it named itself with
// `aria-label` and its field had no `id`/`for` pair, so the two contract
// assertions the other dialogs already satisfy (a labelled heading, and a field
// reachable by its label) failed on it — the scan asked for exactly what the
// card predicted. That is PR #1251's own finding repeated on the sibling: a
// dialog can look right and still never be measured.
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

/**
 * `<METHOD> <path>` for a request — what a failure message has to name, without
 * the origin (which differs per worker: the suite's port is claimed per run).
 */
function callOf(req: { method(): string; url(): string }): string {
  return `${req.method()} ${new URL(req.url()).pathname}`;
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
  // /products — the same bin, one row type over, and its confirm asks for the
  // product's NAME (a product has no slug).
  prodTrashTab: DICT["prod.trashTabDeleted"].en,
  prodPurgeButton: DICT["prod.purge"].en,
  prodPurgeConfirmTitle: DICT["prod.purgeConfirmTitle"].en,
  prodPurgeConfirmFieldLabel: DICT["prod.purgeConfirmFieldLabel"].en,
};

// The workspace the stubs describe. `can_delete: true` with no blockers is the
// happy path the delete button is enabled on, and it is what the contract test
// drives. The `can_delete: false` answer — the server's refusal — is stubbed in
// exactly one place, the test at the bottom of this file (t_2b129c85).
const WORKSPACE_ID = "ws1";
const FORM_SLUG = "tee-2026";
const WS_SLUG = "demo";
const PRODUCT_ID = "p1";
// The typed-NAME surface: a product's name, not a slug. It carries a space and a
// lower-case letter, and no surrounding whitespace — the three properties the
// harness asserts before it trusts the prefix / case / trim probes.
const PRODUCT_NAME = "Kopi Ais";
// Wrong slugs/names of the SAME length as the real ones — the confirm must not
// arm on "something was typed", and a length-only comparison must be caught too.
// The lengths are tied to the real values in `assertDialogContract`.
const FORM_WRONG_SLUG = "xxxxxxxx";
const WS_WRONG_SLUG = "xxxx";
const PRODUCT_WRONG_NAME = "Kopi Aiz";

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

// A purgeable row in the /products bin. `can_purge: true` is what makes the
// dialog OFFERED at all — the page withholds the row's button otherwise, so the
// dialog would be unreachable and the reachability assertion below would fail
// (deliberately: a silent skip is the hole this file exists to close).
const TRASHED_PRODUCT = {
  id: PRODUCT_ID,
  name: PRODUCT_NAME,
  price_sen: 650,
  active: true,
  deleted_at: "2026-09-25T10:00:00Z",
  sales_count: 0,
  subscription_count: 0,
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
  // /products: the CATALOGUE is empty on purpose (this guard is about the bin),
  // and the bin holds the one purgeable row the dialog is reached through.
  await page.route(/(.*)\/api\/rest\/products\/trash(\?.*)?$/, (r) =>
    r.fulfill(json([TRASHED_PRODUCT])),
  );
  await page.route(/(.*)\/api\/rest\/products(\?.*)?$/, (r) => r.fulfill(json([])));
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
 * The contract all three dialogs share. Asserted, not assumed — every one of
 * these is something a later edit could drop while leaving the other tests
 * green.
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
    /**
     * The word this dialog asks the caller to type back — "slug" on the forms bin
     * and the danger zone, "product name" on /products. Used in the failure
     * messages ONLY: the boundary is the same for all three, and a red that names
     * the right thing is what makes it readable.
     */
    what: string;
    /** The real value the server will be asked to confirm. */
    value: string;
    /** A different value of the SAME length — see the boundary assertion below. */
    wrongValue: string;
    /**
     * The one request this dialog's confirm sends, held open in (4). The METHOD
     * is pinned with it: all three confirms DELETE, and a regression that turned
     * one into a POST would otherwise still satisfy the in-flight assertion.
     */
    submitMethod: "DELETE" | "POST";
    submitPath: RegExp;
  },
) {
  // The harness' own invariant: if `wrongValue` were a different length, a gate
  // that compared only `typed.length === value.length` would arm the confirm and
  // this guard would not see it. Asserting it here keeps a later edit to the
  // constants from quietly weakening the test.
  expect(
    opts.wrongValue.length,
    `wrongValue must be the same length as the real ${opts.what} for the boundary assertion to bite`,
  ).toBe(opts.value.length);
  // The same shape of invariant for the probes below: each is only a probe if it
  // differs from the real value in the way it claims to. A value of one
  // character has no strict prefix, and a value that is already upper-case (or
  // already padded) makes its probe a copy of the exact-value assertion.
  expect(
    opts.value.length,
    `the ${opts.what} must be longer than one character for the prefix probe to bite`,
  ).toBeGreaterThan(1);
  expect(
    opts.value,
    `the ${opts.what} must have a lower-case letter for the case-fold probe to bite`,
  ).not.toBe(opts.value.toUpperCase());
  expect(
    opts.value.trim(),
    `the ${opts.what} must carry no surrounding whitespace for the trim probe to bite`,
  ).toBe(opts.value);
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
  // The lookup is SCOPED to the dialog: /products' catalogue search box carries
  // the same accessible name ("Product name"), and an unscoped `getByLabel` was
  // a strict-mode violation there (a red that named the harness, not the app).
  // Scoping makes the assertion stricter, not weaker — the label has to be in
  // the dialog subtree, which is where a confirm field's label belongs.
  const input = page.locator(`#${opts.inputId}`);
  await expect(input).toBeFocused();
  await expect(dialog.getByLabel(opts.fieldLabel)).toHaveAttribute("id", opts.inputId);

  // The confirm action exists, is named, and is NOT armed on open. Scoped to
  // the dialog: the purge confirm and the row's own purge button carry the SAME
  // English label ("Delete for good"), so an unscoped lookup is ambiguous — and
  // scoping it also asserts the button really is inside the dialog.
  const confirm = dialog.getByRole("button", { name: opts.confirmName });
  await expect(confirm).toBeVisible();

  // (1) EMPTY — nothing typed. Kept: it is the state the dialog opens in, and a
  // confirm that was armed unconditionally fails here.
  await expect(confirm, "confirm must be disabled until the exact value is typed").toBeDisabled();

  // (2) A WRONG VALUE OF THE SAME LENGTH. This is the boundary that actually
  // guards the write, and (1) never reaches it: every implementation compares
  // `typed.trim()` against the real value (`WorkspaceTab.tsx` canDelete,
  // `forms/page.tsx` purge, `products/page.tsx` purgeProduct), so a regression
  // that arms the button for any non-empty text — the shape measured shipping
  // green on both slugs — passes (1) and destroys the wrong workspace /
  // permanently purges the wrong form's CASCADE children / takes a product's
  // variations with it. Same length on purpose: a length-only comparison must be
  // caught too.
  await input.fill(opts.wrongValue);
  await expect(
    confirm,
    `confirm must stay disabled for a wrong ${opts.what} (typed "${opts.wrongValue}", the real ${opts.what} is "${opts.value}")`,
  ).toBeDisabled();

  // (2b) A PREFIX OF THE REAL VALUE. A comparison of the shape
  // `typed.trim().startsWith(...)` — or one where the REAL value is the prefix
  // side — arms the confirm on the first character the user types: one keystroke
  // would destroy a workspace / purge a form / delete a product the user never
  // named. (2) cannot see it: the wrong value above is not a prefix of the real
  // one. Strictly shorter than the real value, pinned above.
  const prefix = opts.value.slice(0, 1);
  await input.fill(prefix);
  await expect(
    confirm,
    `confirm must stay disabled for a PREFIX of the real ${opts.what} (typed "${prefix}", the real ${opts.what} is "${opts.value}")`,
  ).toBeDisabled();

  // (2c) THE EXACT VALUE, INVERTED CASE. A comparison that case-folds both sides
  // (`typed.trim().toLowerCase() === value.toLowerCase()`) arms the confirm on
  // "TEE-2026" / "KOPI AIS" — the right name in the wrong case, which is not
  // what the server accepts — and neither (2) nor (2b) sees it.
  const inverted = opts.value.toUpperCase();
  await input.fill(inverted);
  await expect(
    confirm,
    `confirm must stay disabled for the right ${opts.what} in the wrong case (typed "${inverted}", the real ${opts.what} is "${opts.value}")`,
  ).toBeDisabled();

  // (2d) THE EXACT VALUE PADDED WITH WHITESPACE ARMS IT. Pinned deliberately:
  // the app trims what it compares (`lib/typedConfirm.ts` on the products page,
  // a one-sided `typed.trim()` on the other two), so a pasted value with a
  // trailing space (or a newline) is accepted. It is a decision, not an
  // accident — a later change to compare the raw value has to change this
  // assertion, and say so.
  await input.fill(` ${opts.value} `);
  await expect(
    confirm,
    `confirm must be enabled for the exact ${opts.what} padded with whitespace (typed " ${opts.value} ", the real ${opts.what} is "${opts.value}")`,
  ).toBeEnabled();

  // (3) THE EXACT VALUE arms it. The other side of the boundary: without this a
  // permanently-disabled confirm would satisfy (1) and (2) and the dialog would
  // be a dead end no test noticed.
  await input.fill(opts.value);
  await expect(confirm, "confirm must be enabled once the exact value is typed").toBeEnabled();

  // (4) IN FLIGHT — the double-submit guard. The confirm's `disabled` also
  // carries the app's own busy flag, and nothing measured it: dropping it
  // (`disabled={purgeTyped.trim() !== confirmPurge.slug}`, or `canDelete =
  // slugMatches` on the workspace dialog) passed every other assertion here, so
  // a double click would send the delete twice. The submit is HELD OPEN — the
  // route never answers — and the same button is then read through its OTHER
  // label, because the app swaps the copy while busy.
  //
  // THE REQUEST IS AWAITED, NOT SAMPLED (card t_c99a03fd). This block used to
  // click, assert the busy label, and read `heldOpen` on the very next line. That
  // read had no synchronisation behind it: the push happens in a handler that only
  // runs once the request has travelled renderer → browser process → this one,
  // while the assertion above it is satisfied by the app's own re-render alone. The
  // ordering is a LEAD, not a guarantee: measured on this dialog's own click path
  // (two concurrent 6-worker runs, 240 executions of a probe that drives the same
  // dialog and the same click) the handler landed 46–408ms BEFORE the label became
  // observable, so 46ms is the smallest margin ever seen and nothing below may
  // depend on it. The merge gate of PR #1411 already spent that margin:
  // `heldOpen.length === 0` on bytes with no defect behind it (151 passed / 1
  // failed; the same arm then passed 3/3 and the whole a11y gate passed on re-run),
  // i.e. ~25 minutes of merge-lane rework manufactured by this assertion. It is
  // reproducible on demand now: a confirm that arms `busy` and only THEN issues its
  // request reds the old line at 400ms and passes this one.
  //
  // So the submit is WAITED FOR. `page.waitForRequest` resolves when the request is
  // ISSUED — before any route handler answers it — which is real synchronisation,
  // and it pins the METHOD and URL the app actually sends, so "the request went
  // somewhere else" is a sentence naming where instead of a silent 0. The
  // handler's own record is then kept as the SECOND proof: that THIS route held
  // the request rather than one of the stub routes `stubAPI` registered before it
  // (Playwright resolves a page's routes in reverse registration order — "the most
  // recently registered route takes precedence" — so the last one wins, which is
  // asserted here rather than assumed). The `> 0` check stays: it is what proves
  // the in-flight assertion measured something.
  const heldOpen: string[] = [];
  await page.route(opts.submitPath, (route) => {
    // Answering nothing: the request never lands, so the app stays busy for the
    // assertion below. (Playwright holds a request whose handler answers
    // neither fulfil nor abort; the page is torn down with it still in flight.)
    heldOpen.push(callOf(route.request()));
  });
  // Every request this page issues, kept for the failure message below.
  const issued: string[] = [];
  page.on("request", (req) => {
    if (req.url().includes("/api/")) issued.push(callOf(req));
  });
  const submitted = page
    .waitForRequest(
      (req) => req.method() === opts.submitMethod && opts.submitPath.test(new URL(req.url()).pathname),
      { timeout: 15_000 },
    )
    .catch(() => null);
  await confirm.click();
  const submit = await submitted;
  if (submit === null) {
    throw new Error(
      `the confirm must send ${opts.submitMethod} ${opts.submitPath}, and it sent nothing matching: ` +
        `this page issued ${issued.join(", ") || "(no /api request at all)"}. ` +
        "Without that request the in-flight assertion below would measure nothing.",
    );
  }
  // POLLED, not read: even with the request awaited, the handler's push is one
  // event-loop turn behind it, and a bare read here would be the same
  // unsynchronised sample this block is being fixed for.
  await expect
    .poll(() => heldOpen.length, {
      timeout: 5_000,
      message:
        `the submit (${callOf(submit)}) must have been HELD OPEN by this route, not answered ` +
        "by one of the stub routes registered before it",
    })
    .toBeGreaterThan(0);
  const inFlight = dialog.getByRole("button", { name: opts.savingName });
  await expect(
    inFlight,
    "the confirm must be off while one submit is in flight (a double click must not send it twice)",
  ).toBeDisabled();

  // Escape closes the dialog — the only keyboard way out of these modals. It is
  // asserted with the submit STILL in flight (the request is deliberately left
  // holding: releasing it as a failure is not equivalent on the two dialogs —
  // /forms replaces its whole body with the error card when `err` is set, so the
  // purge page would be measured as "Escape closed a dialog that no longer
  // exists"), and the modal must still be closable while busy.
  await page.keyboard.press("Escape");
  await expect(dialog, "Escape must close the dialog").toHaveCount(0);
}

test.describe("a11y: typed-confirmation dialogs (slug / product name)", () => {
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
      what: "slug",
      value: WS_SLUG,
      wrongValue: WS_WRONG_SLUG,
      // The dialog's DELETE — the workspace's own URL, built from the id the
      // stub describes, so it cannot drift from it.
      submitMethod: "DELETE",
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
      what: "slug",
      value: FORM_SLUG,
      wrongValue: FORM_WRONG_SLUG,
      submitMethod: "DELETE",
      submitPath: new RegExp(`/api/rest/forms/${TRASHED_FORM.id}/purge`),
    });
  });

  // THE THIRD SURFACE (card t_eedd6522): /products' bin, whose confirm asks for
  // the product's NAME. Same ladder, same six points, same in-flight state — the
  // dialog's `disabled` is `purgeBusy || !matchesTypedValue(purgeTyped,
  // confirmPurge.name)`, and until this test existed nothing in `web/` drove it:
  // a prefix compare, a case-folding compare, or dropping `purgeBusy` all shipped
  // green (`src/lib/typedConfirm.test.ts` pins the SHAPE of the call, not the
  // behaviour behind it, and could not: the button's enablement is runtime
  // state). A product is the widest blast radius of the three — its variations
  // go with it through CASCADE.
  test("product-purge dialog (/products → Trash)", async ({ page, baseURL }) => {
    await stubAPI(page);
    await freshState(page);
    await page.goto(`${baseURL}/products`, { waitUntil: "domcontentloaded" });

    // The catalogue is the default view — a new visitor must never land in the
    // bin — so the Trash tab is clicked deliberately.
    await page.getByRole("tab", { name: EN.prodTrashTab, exact: true }).click();
    const open = page.getByRole("button", { name: EN.prodPurgeButton });
    await expect(open, "the purge button must be offered for a purgeable product").toBeVisible({
      timeout: 15_000,
    });
    await open.click();

    await assertDialogContract(page, {
      title: EN.prodPurgeConfirmTitle,
      fieldLabel: EN.prodPurgeConfirmFieldLabel,
      inputId: "prod-purge-confirm-name",
      confirmName: EN.prodPurgeButton,
      savingName: DICT["common.saving"].en,
      what: "product name",
      value: PRODUCT_NAME,
      wrongValue: PRODUCT_WRONG_NAME,
      submitMethod: "DELETE",
      submitPath: new RegExp(`/api/rest/products/${TRASHED_PRODUCT.id}/purge`),
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

  // /products' bin, scanned the same way. It named itself with `aria-label` and
  // its field carried no `id`/`for` pair until this test existed (card
  // t_eedd6522), which is exactly the shape `aria-dialog-name` / `label` are for:
  // the dialog was never scanned, so nothing said so.
  test("product-purge dialog has no critical/serious axe violations", async ({ page, baseURL }) => {
    await stubAPI(page);
    await freshState(page);
    await page.goto(`${baseURL}/products`, { waitUntil: "domcontentloaded" });
    await page.getByRole("tab", { name: EN.prodTrashTab, exact: true }).click();
    await page.getByRole("button", { name: EN.prodPurgeButton }).click();
    await expect(page.locator('[role="dialog"]')).toBeVisible({ timeout: 15_000 });
    await scanDialog(page, "product-purge dialog");
  });
});
