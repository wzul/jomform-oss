// THE PHONE LAYOUT OF THE FIVE DASHBOARD TABLES (card t_c83ecf3e).
//
// THE DEFECT THIS GUARDS. Every dashboard table was a desktop table with its
// columns switched OFF below `sm` — 44 `<th>`/`<td>` pairs carried
// `hidden … sm:table-cell` — and `overflow-x-auto` on the wrapper could not
// scroll to a `display:none` column, so at 390px the data was simply absent. The
// worst case was `/deliveries`, whose hidden column was the EVENT
// (`delivered` / `bounce` / `complaint` / `open` / `click`) — the field the page
// exists to show, and the one its own Status filter and subtitle promise.
//
// WHAT IS ASSERTED, per page, at a REAL 390px viewport, with the API stubbed at
// the browser level (the pattern `e2e/a11y-authed-pages.spec.ts` uses):
//
//   1. THE TABLE IS OFF and the phone cards are ON — the responsive branch really
//      switched, rather than both layouts painting and overlapping.
//   2. EVERY COLUMN THE DESKTOP TABLE CARRIES IS REACHABLE FROM THE PHONE ROW.
//      The expectations are READ OFF THE RENDERED DESKTOP ROW, not hardcoded:
//      each `<td>`'s leaf text is collected, split into the columns the table
//      hides below `sm` (`sm:table-cell`) and the ones it keeps. Every kept
//      column's text must be in the CARD as it renders collapsed; every hidden
//      column's text must be inside the one `<details>` disclosure. A column that
//      goes back to `hidden sm:table-cell` with no phone home therefore reds
//      here BY ITS OWN TEXT — which is why the pre-fix shape cannot come back
//      quietly.
//   3. NO HORIZONTAL SCROLLING, before and after the disclosure is opened: the
//      card is the layout, not a squeeze of the 8-column table.
//   4. `/deliveries`' EVENT IS IN THE COLLAPSED CARD, in its badge area — not
//      behind the disclosure. That is the page's whole purpose and the exact
//      cell the review measured missing.
//
// BITE PROOF (re-run with the phone arm sabotaged): putting `hidden sm:table-cell`
// back on the deliveries card's badge container fails arm 4 with the collapsed
// badge area empty at 390px; deleting one card field fails arm 2 naming that
// column's text. Both were run on this tree before it was handed off.
//
// FRESH BROWSER STATE: storage is cleared and the language pinned to `en` on
// every arm, so a `jf_lang` left behind by an earlier card cannot render a page
// these assertions do not describe — the board has already had one false reading
// of exactly that kind. The session is declared explicitly (`e2e/auth.ts`):
// the dashboard is gated on the SERVER and a spec without the cookie measures
// /login.
import { test, expect, type Page } from "@playwright/test";

import { signIn } from "./auth";

// The dashboard paths are gated on the server (web/src/middleware.ts): declare
// the session this spec means. See e2e/auth.ts.
test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});

// A REAL phone, not a narrow desktop: 390x844 is the viewport the UX review
// measured, and it is below Tailwind's `sm` (640px) where the tables hide their
// columns.
test.use({ viewport: { width: 390, height: 844 } });

const ME = {
  id: "u1",
  email: "merchant@example.com",
  name: "Merchant",
  lang: "en",
  avatar_url: "",
  phone: "",
  timezone: "",
  totp_enabled: false,
  is_super_admin: true,
  default_workspace_id: "a1",
};

const WORKSPACES = [{ id: "a1", name: "Demo", slug: "demo", role: "admin" }];

const BUNDLE = {
  me: ME,
  workspaces: WORKSPACES,
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

const FORM = {
  id: "f1",
  slug: "demo-form",
  title: "Demo form",
  status: "published",
};

const SALE = {
  ref_code: "R1",
  status: "paid",
  total_sen: 1000,
  payment_method: "fpx",
  buyer: "Ali",
  buyer_email: "ali@example.com",
  form_id: "f1",
  email_status: "delivered",
  created_at: "2026-09-01T00:00:00Z",
  items: [{ product_id: "p1", price_sen: 1000, qty: 1, name: "Alpha" }],
};

const PRODUCT = {
  id: "p1",
  name: "Alpha",
  price_sen: 1000,
  type: "one_time",
  active: true,
  stock: 5,
  created_at: "2026-09-01T00:00:00Z",
};

const DELIVERY = {
  id: 1,
  email: "ali@example.com",
  event_type: "delivered",
  sale_ref: "R1",
  form_id: "f1",
  detail: {},
  created_at: "2026-09-01T00:00:00Z",
};

const SPAM_SUBMISSION = {
  id: "sp1234567890",
  form_id: "f1",
  data: { name: "bot" },
  spam_reason: "keyword",
  created_at: "2026-09-01T00:00:00Z",
};

const SPAM_SALE = {
  ref_code: "R9",
  status: "paid",
  total_sen: 500,
  buyer: "bot",
  buyer_email: "bot@example.com",
  form_id: "f1",
  spam_reason: "keyword",
  created_at: "2026-09-01T00:00:00Z",
};

const SUBSCRIPTION = {
  id: "su1",
  customer: "Ali",
  product_id: "p1",
  product_name: "Pro",
  amount_sen: 1000,
  period: "month",
  status: "active",
  next_charge_at: "2026-10-01T00:00:00Z",
  retry_attempts: 2,
};

async function stubAPI(page: Page): Promise<void> {
  const json = (body: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  // Catch-all FIRST, specific routes AFTER it: Playwright resolves matching
  // routes in reverse registration order, so the later (specific) ones win.
  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(BUNDLE)));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(ME)));
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "en" })));
  await page.route(/(.*)\/api\/auth\/profile\/default-workspace$/, (r) => r.fulfill(json({})));
  await page.route(/(.*)\/api\/workspaces(\?.*)?$/, (r) => r.fulfill(json(WORKSPACES)));
  await page.route(/(.*)\/api\/rest\/forms(\?.*)?$/, (r) => r.fulfill(json([FORM])));
  await page.route(/(.*)\/api\/rest\/currency\/rates(\?.*)?$/, (r) =>
    r.fulfill(json({ rates: [], cached: false })),
  );

  // One row per list, so each page paints a real row (a page that only ever
  // renders its empty state proves nothing about a row's columns).
  await page.route(/(.*)\/api\/rest\/sales[^?]*(.*)$/, (r) =>
    r.fulfill(json({ items: [SALE], page: 1, limit: 20, total: 1 })),
  );
  await page.route(/(.*)\/api\/rest\/products\/trash(\?.*)?$/, (r) => r.fulfill(json([])));
  await page.route(/(.*)\/api\/rest\/products(\?.*)?$/, (r) => r.fulfill(json([PRODUCT])));
  await page.route(/(.*)\/api\/rest\/email-deliveries[^?]*(.*)$/, (r) =>
    r.fulfill(json({ items: [DELIVERY], page: 1, limit: 20, total: 1 })),
  );
  await page.route(/(.*)\/api\/rest\/spam\/submissions[^?]*(.*)$/, (r) =>
    r.fulfill(json({ items: [SPAM_SUBMISSION], page: 1, limit: 20, total: 1 })),
  );
  await page.route(/(.*)\/api\/rest\/spam\/sales[^?]*(.*)$/, (r) =>
    r.fulfill(json({ items: [SPAM_SALE], page: 1, limit: 20, total: 1 })),
  );
  await page.route(/(.*)\/api\/rest\/subscriptions[^?]*(.*)$/, (r) =>
    r.fulfill(json({ items: [SUBSCRIPTION], page: 1, limit: 20, total: 1 })),
  );
}

/** A FRESH browser state at the app's default locale (see the file header). */
async function freshState(page: Page): Promise<void> {
  await page.addInitScript(() => {
    try {
      localStorage.clear();
      sessionStorage.clear();
      localStorage.setItem("jf_workspace", "demo");
      localStorage.setItem("jf_lang", "en");
    } catch {
      /* storage unavailable */
    }
  });
}

/** How many CSS pixels wider than the viewport the document is. 0 = no sideways scroll. */
function horizontalOverflowPx(page: Page): Promise<number> {
  return page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
}

type RowColumns = { kept: string[]; hidden: string[] };

/**
 * The FIRST rendered desktop row's text, split by whether the table hides that
 * column below `sm`. Read off the live DOM so the guard follows the table rather
 * than a copy of it: a column added to a table is covered the moment it renders.
 *
 * Leaf text nodes only (not the `<td>`'s concatenated text): a cell like the
 * sales buyer is two blocks ("Ali" / "ali@example.com"), and the card renders them
 * as separate lines too, so a whole-cell string would never match.
 */
function desktopRowColumns(page: Page): Promise<RowColumns> {
  return page.evaluate((): { kept: string[]; hidden: string[] } => {
    const row = document.querySelector("table tbody tr");
    if (!row) return { kept: [], hidden: [] };
    const leafTexts = (node: Element): string[] => {
      const out: string[] = [];
      const walk = (n: Node): void => {
        for (const child of Array.from(n.childNodes)) {
          if (child.nodeType === Node.TEXT_NODE) {
            const s = (child.nodeValue || "").replace(/\s+/g, " ").trim();
            // 1-char glyphs ("—", "✕", "…") carry no column identity; they are
            // rendered in both layouts and would only add noise to the diff.
            if (s.length > 1) out.push(s);
          } else if (child.nodeType === Node.ELEMENT_NODE) {
            const el = child as Element;
            if (el.getAttribute("aria-hidden") === "true") continue;
            walk(el);
          }
        }
      };
      walk(node);
      return out;
    };
    const kept: string[] = [];
    const hidden: string[] = [];
    for (const td of Array.from(row.querySelectorAll("td"))) {
      const hidesBelowSm = td.className.split(/\s+/).includes("sm:table-cell");
      (hidesBelowSm ? hidden : kept).push(...leafTexts(td));
    }
    return { kept, hidden };
  });
}

/** Everything a phone row must be able to show, read out of the rendered table. */
async function expectPhoneRowCarriesEveryColumn(page: Page, what: string): Promise<void> {
  // 1. The responsive branch switched: cards on, table off.
  const records = page.locator("[data-jf-mobile-records]");
  await expect(records, `${what}: the phone row cards must render at 390px`).toBeVisible();
  const card = page.locator("[data-jf-record]").first();
  await expect(card, `${what}: the first phone row card must render`).toBeVisible();
  await expect(
    page.locator("table").first(),
    `${what}: the desktop table must be OFF at 390px (both layouts painting is the overlap this replaces)`,
  ).toBeHidden();

  // 3. No sideways scroll with the card as it renders.
  expect(
    await horizontalOverflowPx(page),
    `${what}: the page scrolls sideways at 390px — the phone layout must fit, not overflow`,
  ).toBeLessThanOrEqual(1);

  // 2. Every column of the rendered row is reachable from the card.
  const { kept, hidden } = await desktopRowColumns(page);
  expect(
    kept.length + hidden.length,
    `${what}: the desktop row carries no text at all — this guard would prove nothing`,
  ).toBeGreaterThan(0);

  const collapsed = await card.evaluate((el) => (el as HTMLElement).innerText);
  for (const text of kept) {
    expect(
      collapsed,
      `${what}: "${text}" is a column the table KEEPS at 390px but it is missing from the phone card`,
    ).toContain(text);
  }

  await card.locator("summary").first().click();
  const opened = await card.evaluate((el) => (el as HTMLElement).innerText);
  for (const text of hidden) {
    expect(
      opened,
      `${what}: "${text}" is a column the table HIDES below sm and it is NOT reachable in the phone card`,
    ).toContain(text);
  }

  // …and opening it did not push the layout sideways either.
  expect(
    await horizontalOverflowPx(page),
    `${what}: opening a row's details scrolls the page sideways at 390px`,
  ).toBeLessThanOrEqual(1);
}

/** Opens a page with the API stubbed and one row rendered, in a fresh state. */
async function openAt390(page: Page, path: string): Promise<void> {
  await stubAPI(page);
  await freshState(page);
  await page.goto(path, { waitUntil: "networkidle" });
  await expect(page.locator("h1").first(), `${path} did not render (no page heading)`).toBeVisible();
}

test.describe("phone layout: the dashboard tables carry every column at 390px", () => {
  test("/deliveries — the EVENT is in the collapsed card, and every other column is reachable", async ({
    page,
  }) => {
    await openAt390(page, "/deliveries");

    const card = page.locator("[data-jf-record]").first();
    await expect(card, "the delivery row must render at 390px").toBeVisible();

    // 4. THE PAGE'S WHOLE PURPOSE, in the COLLAPSED row: the event is a badge you
    // can read without opening anything. Pre-fix the cell carried
    // `hidden sm:table-cell` and the badge was simply not rendered here.
    const badges = card.locator("[data-jf-badges]");
    await expect(badges, "the delivery row must carry a visible badge area at 390px").toBeVisible();
    await expect(
      badges,
      "the delivery EVENT (delivered / bounce / complaint / open / click) must be visible in the collapsed phone row — it is the page's subject",
    ).toContainText(DELIVERY.event_type);

    await expectPhoneRowCarriesEveryColumn(page, "/deliveries");
  });

  test("/sales — items, email status, form and date are reachable", async ({ page }) => {
    await openAt390(page, "/sales");
    await expectPhoneRowCarriesEveryColumn(page, "/sales");
  });

  test("/products — type, stock and date are reachable", async ({ page }) => {
    await openAt390(page, "/products");
    await expectPhoneRowCarriesEveryColumn(page, "/products");
  });

  test("/spam — the submissions tab's date is reachable", async ({ page }) => {
    await openAt390(page, "/spam");
    await expectPhoneRowCarriesEveryColumn(page, "/spam (submissions)");
  });

  test("/spam — the sales tab's reason and date are reachable", async ({ page }) => {
    await openAt390(page, "/spam");
    await page.getByRole("button", { name: /^Sales/ }).click();
    await expect(
      page.locator("[data-jf-record]").first(),
      "the spam sales tab must render a row card at 390px",
    ).toBeVisible();
    await expectPhoneRowCarriesEveryColumn(page, "/spam (sales)");
  });

  test("/subscriptions — product, next charge and retry count are reachable", async ({ page }) => {
    await openAt390(page, "/subscriptions");
    await expectPhoneRowCarriesEveryColumn(page, "/subscriptions");
  });
});
