// THE SALES DRAWER OFFERS ONLY WHAT THE SERVER SAYS IT CAN DO (cards t_b9d7f01e
// and t_52429d6f).
//
// WHY A BROWSER TEST. The API's sales row now carries `available_events`: the
// events the current state accepts, each with `enabled` and, when a guard refuses
// it, the English reason. The drawer used to decide for itself
// (`status !== "confirmed"`, then `pending || paid`), so it offered buttons the
// server refused — and before the lifecycle table existed, a Confirm on a
// cancelled sale silently rewrote it. A unit test cannot see a rendered button,
// so the real page is driven against an API stubbed at the network boundary (no Go
// server, no credential — the pattern e2e/chat-refusal-language.spec.ts uses) and
// the drawer is read out of the DOM.
//
// THREE ARMS, each killing a different regression:
//
//   1. the server's list decides (a paid sale offers Confirm + Refund; a
//      cancelled one offers neither);
//   2. a MISSING available_events offers NOTHING — the page must not fall back to
//      guessing from status;
//   3. a paid sale whose list contains ONLY refund shows Refund and NO Confirm —
//      the same status as arm 1, a different answer, so arm 1 cannot be passing
//      because the page reads `status`.
import { test, expect, type Page } from "@playwright/test";

import { signIn } from "./auth";

const REF = "JF-RENDER-0001";

const BUNDLE = {
  me: { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "en", is_super_admin: false },
  workspaces: [{ id: "ws-id-b", name: "Second", slug: "ws-b", role: "admin" }],
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

type StubEvent = { event: string; label_key?: string; enabled: boolean; reason?: string };

// What the Go table answers for a paid sale: Confirm and Refund are legal, the
// two authorization moves are defined but refused by the guard.
const PAID_EVENTS: StubEvent[] = [
  { event: "capture", label_key: "bld.capture", enabled: false, reason: "not an authorization awaiting capture" },
  { event: "confirm", label_key: "sales.confirmSale", enabled: true },
  { event: "refund", label_key: "sales.refund", enabled: true },
  { event: "release", label_key: "bld.release", enabled: false, reason: "not an authorization awaiting capture" },
];

// sale builds one row. `available` is OPTIONAL on purpose: `undefined` omits the
// field entirely (the older-server / truncated-body shape arm 2 measures).
function sale(status: string, available?: StubEvent[]) {
  const row: Record<string, unknown> = {
    id: "sale-1",
    ref_code: REF,
    status,
    total_sen: 5000,
    subtotal_sen: 5000,
    currency: "MYR",
    payment_method: "chip",
    sale_type: "one_time",
    created_at: "2026-11-09T02:00:00Z",
    captured_at: null,
    email_status: "",
    client: { full_name: "Ali", email: "ali@example.test" },
    items: [{ product_id: "p", name: "Tee", price_sen: 5000, qty: 1 }],
  };
  if (available !== undefined) row.available_events = available;
  return row;
}

async function stubAPI(page: Page, row: Record<string, unknown>) {
  const json = (body: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(BUNDLE.me)));
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "en" })));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(BUNDLE)));
  await page.route(/(.*)\/api\/rest\/forms(\?.*)?$/, (r) => r.fulfill(json([])));
  await page.route(/(.*)\/api\/rest\/sales(\?.*)?$/, (r) =>
    r.fulfill(json({ items: [row], page: 1, limit: 20, total: 1 })),
  );
  await page.route(/(.*)\/api\/rest\/events\/stream(\?.*)?$/, (r) =>
    r.fulfill({ status: 200, contentType: "text/event-stream", body: "" }),
  );
}

async function openDrawer(page: Page, baseURL: string | undefined, row: Record<string, unknown>) {
  await stubAPI(page, row);
  // Fresh browser state: a jf_lang or jf_workspace left by an earlier spec would
  // change what this page renders for a brand-new visitor.
  await page.addInitScript(() => {
    try {
      localStorage.clear();
      sessionStorage.clear();
      localStorage.setItem("jf_workspace", "ws-b");
      localStorage.setItem("jf_lang", "en");
    } catch {
      /* storage unavailable */
    }
  });
  await signIn(page, baseURL!);
  await page.goto(`${baseURL}/sales`, { waitUntil: "domcontentloaded" });
  const tableRow = page.getByRole("row").filter({ hasText: REF }).first();
  await expect(tableRow, "the sale row must render — otherwise this spec proves nothing").toBeVisible({
    timeout: 20_000,
  });
  await tableRow.getByRole("button", { name: "View", exact: true }).first().click();
  const dialog = page.getByRole("dialog", { name: "Sale detail" });
  await expect(dialog, "the drawer must render").toBeVisible({ timeout: 10_000 });
  return dialog;
}

const ACTION_NAMES = ["Confirm", "Refund", "Capture", "Release"] as const;

test("the sales drawer offers Confirm only where the lifecycle table accepts it", async ({
  page,
  baseURL,
}) => {
  // Cancelled: the table has no `confirm` edge, the API answers 400
  // lifecycle.illegalTransition — and its available_events is empty.
  let dialog = await openDrawer(page, baseURL, sale("cancelled", []));
  for (const name of ACTION_NAMES) {
    await expect(
      dialog.getByRole("button", { name, exact: true }),
      `a cancelled sale must not offer ${name} — the API refuses it`,
    ).toHaveCount(0);
  }
  await page.keyboard.press("Escape");

  // Paid: the table accepts confirm and refund, so both buttons are there — the
  // assertions above are not passing because the drawer renders no buttons at all.
  dialog = await openDrawer(page, baseURL, sale("paid", PAID_EVENTS));
  await expect(
    dialog.getByRole("button", { name: "Confirm", exact: true }),
    "a paid sale must still offer Confirm (the table accepts paid → confirmed)",
  ).toBeVisible();
  await expect(dialog.getByRole("button", { name: "Refund", exact: true })).toBeVisible();
});

test("a sale with no available_events offers no action buttons at all", async ({ page, baseURL }) => {
  // The field is ABSENT (not []): the server did not answer. The page must not
  // fall back to the status predicate — a paid sale would have offered Confirm
  // and Refund under the old code.
  const dialog = await openDrawer(page, baseURL, sale("paid"));
  for (const name of ACTION_NAMES) {
    await expect(
      dialog.getByRole("button", { name, exact: true }),
      `without available_events the drawer must not guess ${name} from status`,
    ).toHaveCount(0);
  }
});

test("the drawer follows the server's list, not the status", async ({ page, baseURL }) => {
  // Same status as the paid arm above, a different server answer: only refund is
  // on offer. Confirm must be absent, Refund present — which is only true if the
  // drawer reads available_events.
  const dialog = await openDrawer(
    page,
    baseURL,
    sale("paid", [{ event: "refund", label_key: "sales.refund", enabled: true }]),
  );
  await expect(dialog.getByRole("button", { name: "Refund", exact: true })).toBeVisible();
  await expect(
    dialog.getByRole("button", { name: "Confirm", exact: true }),
    "the server did not offer confirm — status alone must not put it back",
  ).toHaveCount(0);
});
