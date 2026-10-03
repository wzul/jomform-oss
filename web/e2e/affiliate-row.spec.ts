// Regression guard: the "input row" class in the dashboard must keep its fields usable.
//
// THE DEFECT (reported from a phone screenshot of jomform.com/settings → Affiliates):
// the create row was `flex items-end gap-2` — a NON-wrapping flex row holding two
// `flex-1` inputs, a fixed `w-28` field and a content-sized button. Measured on the
// real built page at 320-430px, the two `flex-1` boxes collapsed to a 30.73px /
// 26px border box. `.jf-input` (web/src/app/globals.css) sets `padding: 8px 12px`
// and is written OUTSIDE any `@layer`, so 24px of that box is padding and it beats
// every layered Tailwind utility — the input was left with 5px / 0px of CONTENT
// width while the "ALI" / "Ali" placeholder is a 21.51px / 17.36px glyph run. The
// fields therefore rendered as empty boxes, exactly as the screenshot showed.
// Measured button : input width was 2.9x locally (3.2x on the screenshot), and the
// affiliate row's own scrollWidth exceeded its clientWidth by up to 48px (Tamil at
// 320px), so the row was also overflowing its card.
//
// THE CLASS, NOT THE ONE ROW
// The same shape exists on other pages, so this guard measures every row in the
// class — settings/affiliates (settings/page.tsx:2302), the chat composer
// (chat/page.tsx:284), the settings/members invite row (MembersTab.tsx:157), the
// admin suppression add row (admin/page.tsx:791), the sales filter bar
// (sales/page.tsx:761), and the three settings rows added later: the add-domain
// row (settings/page.tsx:1175), the add-webhook row (settings/page.tsx:1575) and
// the webhooks inline edit drawer (settings/page.tsx:1706) — because `flex-wrap`
// alone is NOT proof of correctness: the members row already had `flex-wrap` and
// still overflowed its container by 2px (`min-w-56` is `flex: 1 1 0%`'s
// min-width, so it could not shrink) until this card fixed it. Each rule is
// therefore a property of the RENDERED box.
//
// WHY THE GUARD IS A BROWSER MEASUREMENT, NOT A CLASS-NAME ASSERTION
// `npm run test:unit` (node --test over src/lib/*.test.ts) has no DOM, and a guard
// that asserts "the row is not `flex items-end gap-2`" is toothless: any other
// arrangement that re-creates the squeeze would keep it green. This file runs in the
// axe gate (`npm run a11y`, part of scripts/preflight.sh) because that is the one
// gate in this repo that renders the real built page in a real browser, and it
// measures the ACTUAL rendered box of the ACTUAL control against the ACTUAL glyph run
// of its placeholder or label — per locale, at every width the card names.
//
// WHAT MAKES IT BITE (verified, not assumed)
// Every rule below is violated directly by the collapse:
//   RULE 1  a control's content width must fit its own glyph run (canvas-measured in
//           the element's own computed font) and clear a field-sized floor
//   RULE 2  nothing overflows internally, or past its row's container
//   RULE 3  the page never scrolls sideways
//   RULE 4  in a row mixing fields with one action button, the button must not
//           dominate the row (the reported 3.2x button : field ratio)
// Against the pre-fix tree RULE 1 fails in en/ms/ta/zh/ar at 320/360/390/430px (the
// two `flex-1` inputs measured 5px, 0px, 10px, 0px, 1px of content against a 21.51px
// placeholder run). The bite was NOT assumed: this exact file was run against the
// pre-fix build and went 20 failed / 5 passed (the desktop cases) — see the PR.
//
// The API is stubbed at the browser level (no Go server, no session, no DB), so the
// gate needs nothing but the build it is already serving.
import { test, expect, type Page } from "@playwright/test";
import { DICT } from "../src/components/i18nDict";

// Phone widths from the card, plus the desktop width where the `sm:` layout must
// still be the original one-line row.
const WIDTHS = [320, 360, 390, 430, 1024] as const;

// A text input below this content width cannot show a 3-character code plus a
// caret with any margin. The longest placeholder in the row is "Ali" at 17.36px
// and "ALI" at 21.51px, so the second term below is the load-bearing one; this
// floor exists so a future edit cannot shrink the field to "technically fits the
// placeholder". It applies to the affiliate row's own three fields.
const MIN_CONTENT_PX = 48;

const LANGS = ["en", "ms", "ta", "zh", "ar"] as const;

// The class rule, enforced across the rows the card names. `flex-wrap` alone is
// NOT proof of correctness, so each constraint is a property of the RENDERED box:
//
//   RULE 1  every form control on a page's rows keeps a usable content width
//           (a collapsed `flex-1` input below this is the reported defect)
//   RULE 2  no control or label overflows its own box or its row horizontally
//   RULE 3  the page never scrolls sideways
//   RULE 4  in a row that mixes fields with a single action BUTTON, the button
//           must not dominate the row (the reported 3.2x button : input ratio)
type Control = {
  tag: string;
  text: string;
  boxW: number;
  clientW: number;
  scrollW: number;
  contentW: number;
  boxH: number;
  textRun: number;
  // A single-line field holding a VALUE (not just a placeholder). An `input`
  // with a value wider than its box scrolls its own text — that is how every
  // filled text field behaves and is not a clipping defect, so the per-control
  // overflow clause below does not apply to one (`scrollWidth` is not a
  // rendered-clipping measure for a value; the placeholder is not in it at all).
  hasValue: boolean;
};
type RowMeasure = { rowClientW: number; rowScrollW: number; controls: Control[] };

// A FIELD (input/select/textarea) must render its own placeholder or value, and
// must never be reduced to a sliver: the reported collapse left 5px / 0px of
// content where the placeholder is a 21.51px glyph run. The run fit is enforced on
// SINGLE-LINE controls only (`input`, `select`) — those cannot wrap, so text that
// does not fit is invisible. A `textarea` and a button label both wrap by design
// (the chat composer's Arabic placeholder measured 145.24px into 139px at 320px,
// and the affiliate button's label wraps in ms/ta with nothing clipped), so they
// answer to the overflow rules instead, which is what detects real clipping.
const FIELD_FLOOR_PX = 16;
const SHORT_TEXT_MAX_CHARS = 24;
// Slack for sub-pixel glyph metrics and the caret when comparing a run to a box.
const RUN_SLACK_PX = 2;
// Sub-pixel rounding slack.
const SLACK_PX = 1;
// A single action button must not be 3x the width of a field beside it.
const MAX_BUTTON_FIELD_RATIO = 2.5;
// Desktop bound: the affiliate row lives under a `max-w-xl` column (36rem = 576px).
const DESKTOP_MAX_ROW_PX = 576;

// The rows the class guard covers, with the URL and the on-page locator that
// finds each one. They live on five different pages, so this walks them in one
// browser session per locale.
//
// ROWS 6-8 WERE ADDED AFTER THIS FILE'S FIRST CARD: the `min-w-56`-on-a-`flex-1`
// shape this file was written for survived in three more settings rows that the
// first card never measured (`settings/page.tsx:1175`, `:1575`, `:1706`). One of
// them — the webhooks edit drawer — is behind `editId !== null`, so it needs an
// explicit open step (`openLabel`/`openSelector` below); the other two are the
// add-domain and add-webhook rows. Their row-overflow violation is the same
// `row scrollWidth 224 > clientWidth 222` RULE 2 already fires on, with no new
// rule: the drawer row is worse (224 > 188, because the drawer spends 16px of
// padding on each side).
// A row on a page, as the guard needs to reach and measure it.
type RowPage = {
  name: string;
  file: string;
  path: string;
  waitFor: string;
  rowExpr: string;
  isButtonRow: boolean;
  // Rows behind a click (the webhooks edit drawer) name the control that opens
  // them — a dictionary KEY, because the label is localized — and the selector
  // that must then exist. Both are absent for the rows that render directly.
  openLabel?: keyof typeof DICT;
  openSelector?: string;
  // A page whose section is component STATE rather than a query parameter (the
  // form-settings tabs) names the tab to click first — a dictionary KEY, because
  // the label is localized. Without this the row would never render and the
  // measurement would silently read an empty element.
  openTab?: keyof typeof DICT;
  // Dictionary keys for placeholders on this row that are a SENTENCE of help
  // text rather than a token: those may truncate (see RULE 1) and are held to
  // the "reads as empty" floor instead of a full glyph-run render.
  hintPlaceholderKeys?: Array<keyof typeof DICT>;
};

const ROW_PAGES: RowPage[] = [
  { name: "settings affiliates create row", file: "settings/page.tsx:2302",
    path: "/settings?tab=affiliates",
    waitFor: 'input[placeholder="ALI"]',
    rowExpr: `document.querySelector('input[placeholder="ALI"]').parentElement.parentElement`,
    isButtonRow: true },
  { name: "chat composer", file: "chat/page.tsx:284", path: "/chat",
    waitFor: "#chat-input",
    rowExpr: `document.getElementById('chat-input').closest('form')`,
    isButtonRow: false },
  { name: "settings members invite row", file: "MembersTab.tsx:157", path: "/settings?tab=members",
    waitFor: 'input[placeholder="teammate@example.com"]',
    rowExpr: `document.querySelector('input[placeholder="teammate@example.com"]').closest('div.flex')`,
    isButtonRow: true },
  { name: "admin suppression manual add", file: "admin/page.tsx:791", path: "/admin",
    waitFor: 'input[placeholder="name@example.com"]',
    rowExpr: `document.querySelector('input[placeholder="name@example.com"]').closest('div.flex')`,
    isButtonRow: false },
  { name: "sales filter bar", file: "sales/page.tsx:761", path: "/sales",
    waitFor: "div.jf-card select.jf-input",
    rowExpr: `document.querySelector('div.jf-card select.jf-input').closest('div.jf-card')`,
    isButtonRow: false },
  { name: "settings domains add row", file: "settings/page.tsx:1188", path: "/settings?tab=domains",
    waitFor: 'input[placeholder="shop.yourdomain.com"]',
    rowExpr: `document.querySelector('input[placeholder="shop.yourdomain.com"]').closest('form')`,
    isButtonRow: true },
  { name: "settings webhooks add row", file: "settings/page.tsx:1604", path: "/settings?tab=webhooks",
    waitFor: 'form.mt-3 input[placeholder="https://yourapp.com/jomform-hook"]',
    rowExpr: `document.querySelector('form.mt-3 input[placeholder="https://yourapp.com/jomform-hook"]').closest('div.flex')`,
    isButtonRow: true },
  { name: "form-settings webhooks add row", file: "form-settings/page.tsx:2558",
    path: "/form-settings?form=f1", openTab: "fs.webhooks",
    waitFor: 'input[placeholder="https://yourapp.com/jomform-hook"]',
    rowExpr: `document.querySelector('input[placeholder="https://yourapp.com/jomform-hook"]').closest('div.flex')`,
    isButtonRow: true },
  { name: "settings webhooks edit drawer row", file: "settings/page.tsx:1741", path: "/settings?tab=webhooks",
    // The drawer only exists while `editId !== null`, so this row needs an
    // explicit open step that the other rows do not: `waitFor` proves the tab
    // rendered, `openLabel` is the row's (localized) Edit button, and
    // `openSelector` is the drawer's own form, which is what gets measured.
    waitFor: 'form.mt-3 input[placeholder="https://yourapp.com/jomform-hook"]',
    openLabel: "set.webhookEdit",
    openSelector: 'form.bg-slate-50 input[placeholder="https://yourapp.com/jomform-hook"]',
    // `set.secretKeep` is a sentence of help text, not a token: 46 characters in
    // `en` (a 307.56px run) and 12 in `zh`. It truncates in every locale by
    // design, so it answers to the "reads as empty" floor, not a full render.
    hintPlaceholderKeys: ["set.secretKeep"],
    rowExpr: `document.querySelector('form.bg-slate-50 input[placeholder="https://yourapp.com/jomform-hook"]').closest('div.flex')`,
    isButtonRow: false },
] as const;

// The dashboard-bundle + endpoint shapes each page needs to reach its loaded
// state. No Go server, no session cookie, no database: every /api/* call is
// stubbed at the browser level and the gate needs nothing but the build.
const BUNDLE = {
  me: { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "", is_super_admin: true },
  workspaces: [{ id: "ws1", name: "Demo", slug: "demo", role: "admin" }],
  chip_status: null, keys: [], domains: [], social: [], einvoice: null,
  // One webhook row, so the webhooks tab renders the edit drawer's opener. The
  // drawer itself is behind `editId !== null` and is opened by the row's Edit
  // button (see ROW_PAGES), so the bundle has to carry a row to open.
  webhooks: [{ id: "wh1", url: "https://yourapp.com/jomform-hook", events: ["sale.paid"] }],
  products: [], summary: { counts: {}, revenue_sen: 0 },
  sales: { items: [], page: 1, limit: 20, total: 0 },
  form_analytics: [], product_analytics: [],
};

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
  // lang:"" (never set) keeps the browser value in effect, which is how this
  // spec pins the locale; the promotion path then POSTs and the stub accepts it.
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "" })));
  await page.route(/(.*)\/api\/rest\/chat(\?.*)?$/, (r) =>
    r.fulfill(json({ available: true, limits: { messages_per_hour: 60, max_message_chars: 2000 } })),
  );
  await page.route(/(.*)\/api\/workspaces\/[^/]+\/affiliates(\?.*)?$/, (r) => r.fulfill(json([])));
  await page.route(/(.*)\/api\/workspaces\/[^/]+\/members(\?.*)?$/, (r) => r.fulfill(json([])));
  await page.route(/(.*)\/api\/workspaces\/[^/]+\/invites(\?.*)?$/, (r) => r.fulfill(json([])));
  await page.route(/(.*)\/api\/workspaces\/[^/]+\/settings$/, (r) => r.fulfill(json({})));
  await page.route(/(.*)\/api\/workspaces\/[^/]+\/email-settings$/, (r) => r.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/currency\/rates$/, (r) =>
    r.fulfill(json({ rates: [], cached: false })),
  );
  await page.route(/(.*)\/api\/rest\/forms(\?.*)?$/, (r) => r.fulfill(json([])));
  // The form-settings page loads ONE form by id, then its per-form sub-resources.
  // Without this the row behind the Notifications tab never renders and the
  // waitFor above reads nothing (card t_ceb9d7a3).
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+$/, (r) =>
    r.fulfill(json({ id: "f1", account_id: "acc1", slug: "demo", title: "Demo",
      notifications: [], webhooks: [], chip_feeds: [], confirmations: [] })),
  );
  for (const sub of ["notifications", "webhooks", "chip-feeds", "confirmations", "email-settings",
                     "settings", "theme", "fields"]) {
    await page.route(new RegExp(`(.*)/api/rest/forms/[^/]+/${sub}(\\?.*)?$`),
      (r) => r.fulfill(json([])));
  }
  await page.route(/(.*)\/api\/rest\/sales(\?.*)?$/, (r) =>
    r.fulfill(json({ items: [], page: 1, limit: 20, total: 0 })),
  );
  await page.route(/(.*)\/api\/admin\/overview$/, (r) =>
    r.fulfill(
      json({
        totals: { workspaces: 1, forms: 0, products: 0, sales: 0, users: 1, media_bytes: 0 },
        growth_24h: { workspaces: 0, forms: 0, products: 0, sales: 0, users: 0 },
        growth_7d: { workspaces: 0, forms: 0, products: 0, sales: 0, users: 0 },
        email: { delivered: 0, bounce: 0, complaint: 0, open: 0, click: 0 },
        spam: { submissions: 0, sales: 0 },
        recent_signups: [], active_workspaces: [], storage: [], quota_bytes: 0,
      }),
    ),
  );
  await page.route(/(.*)\/api\/admin\/dead-letters(\?.*)?$/, (r) => r.fulfill(json({ items: [] })));
  await page.route(/(.*)\/api\/admin\/suppressions(\?.*)?$/, (r) =>
    r.fulfill(json({ items: [], total: 0 })),
  );
  await page.route(/(.*)\/api\/admin\/feedback(\?.*)?$/, (r) => r.fulfill(json({ items: [] })));
}

// measureRow evaluates the row's own locator and reads the live boxes. Every
// number comes from the element's rendered box + computed style.
async function measureRow(page: Page, rowExpr: string): Promise<RowMeasure> {
  return page.evaluate(`(() => {
    const row = ${rowExpr};
    if (!row) throw new Error("row not found");
    const r2 = (n) => Math.round(n * 100) / 100;
    const ctx = document.createElement("canvas").getContext("2d");
    const controls = [...row.querySelectorAll("input, select, textarea, button")].map((el) => {
      const cs = getComputedStyle(el);
      const b = el.getBoundingClientRect();
      const padL = parseFloat(cs.paddingLeft) || 0;
      const padR = parseFloat(cs.paddingRight) || 0;
      const text = (el.tagName === "SELECT"
        ? (el.selectedOptions && el.selectedOptions[0] && el.selectedOptions[0].textContent) || ""
        : el.getAttribute("placeholder") || el.textContent || "").trim();
      // The glyph run of the text this control actually has to render, measured
      // in the control's OWN computed font (canvas, so it is the real render
      // width rather than the box the text happens to be clipped into).
      ctx.font = cs.font;
      return {
        tag: el.tagName.toLowerCase(), text,
        boxW: r2(b.width), clientW: el.clientWidth, scrollW: el.scrollWidth,
        contentW: r2(el.clientWidth - padL - padR), boxH: r2(b.height),
        textRun: r2(ctx.measureText(text).width),
        // A filled field vs an empty one. A textarea's value is not read here
        // and a select's scrollWidth is about its options, not a value, so only
        // an input counts.
        hasValue: el.tagName === "INPUT" && (el.value || "").length > 0,
      };
    });
    return {
      rowClientW: row.clientWidth, rowScrollW: row.scrollWidth,
      rowH: r2(row.getBoundingClientRect().height), controls,
    };
  })()`) as Promise<RowMeasure>;
}

for (const lang of LANGS) {
  const codeLabel = DICT["set.affiliateCode"][lang];
  const nameLabel = DICT["set.affiliateName"][lang];
  const commissionLabel = DICT["set.affiliateCommission"][lang];
  const addLabel = DICT["set.affiliateAdd"][lang];

  for (const width of WIDTHS) {
    test(`affiliate row is usable: ${lang} @ ${width}px`, async ({ page, baseURL }) => {
      await stubAPI(page);
      await page.addInitScript(
        (l) => {
          // A FRESH browser state per run: storage is cleared first so an earlier
          // spec (or an earlier card) cannot leave jf_lang / jf_workspace behind and
          // make this render for a state no new visitor has.
          try {
            localStorage.clear();
            localStorage.setItem("jf_workspace", "demo");
            localStorage.setItem("jf_lang", l);
            localStorage.setItem("jf_settings_tab", "affiliates");
          } catch {
            /* storage unavailable */
          }
        },
        lang,
      );
      await page.setViewportSize({ width, height: 900 });

      // (a) The affiliate row first, with its own locale assertions: the rendered
      //     labels must be THIS locale's strings, not a raw key or English —
      //     otherwise the measurement below is of some other locale's layout.
      //
      // WAIT BOUNDED BY THIS TEST (card t_4b5411db). `waitForSelector`'s timeout is
      // measured against the PAGE's own clock, and under suite load the page can be
      // starved by the host until that budget is spent: the card's red run shows
      // `TimeoutError: page.waitForSelector: Timeout 15000ms exceeded` at
      // `affiliate-row.spec.ts:350` with load 24.8–29.4 on 6 CPUs, while the SAME
      // case passes alone in ~19s (rc=0). A fixed page-side budget cannot tell "the
      // row never rendered" from "the box was too busy to render it in 15s", and it
      // reds a gate every PR depends on. `expect(...).toBeVisible()` is the
      // first-class form of the same wait, so the budget is the test's own and the
      // failure names the selector it was waiting for.
      await page.goto(`${baseURL}/settings?tab=affiliates`, { waitUntil: "domcontentloaded" });
      await expect(
        page.locator('input[placeholder="ALI"]'),
        "the affiliate create row must render — measured with the test's own timeout, not " +
          "waitForSelector's page-side budget",
      ).toBeVisible({ timeout: 30_000 });
      await expect(page.getByText(codeLabel, { exact: true })).toBeVisible();
      await expect(page.getByText(nameLabel, { exact: true })).toBeVisible();
      await expect(page.getByText(commissionLabel, { exact: true })).toBeVisible();
      await expect(page.getByText(addLabel, { exact: true })).toBeVisible();

      // The affiliate create row is the one row with the reported 3x button and the
      // reported collapsed fields, so it carries the strict per-field floor and the
      // desktop assertions the other rows do not need.
      const aff = await measureRow(page, ROW_PAGES[0].rowExpr);
      const affInputs = aff.controls.filter((c) => c.tag === "input");
      expect(affInputs.map((i) => i.text)).toEqual(["ALI", "Ali", "10"]);
      for (const i of affInputs) {
        expect(
          i.contentW,
          `${lang}@${width} affiliate row: input ${JSON.stringify(i.text)} has only ` +
            `${i.contentW}px of content width — the placeholder cannot render`,
        ).toBeGreaterThanOrEqual(MIN_CONTENT_PX);
      }

      // (b) Every row in the class, measured with the class rule below.
      for (const rowPage of ROW_PAGES) {
        if (rowPage !== ROW_PAGES[0]) {
          await page.goto(`${baseURL}${rowPage.path}`, { waitUntil: "domcontentloaded" });
          // A section reachable only by clicking a tab (component state, not a
          // query parameter) has to be OPENED, and the click is asserted by the
          // `waitFor` below: without it the row would never exist and the
          // measurement would read an empty element instead of failing.
          if (rowPage.openTab) {
            await page.getByRole("button", { name: DICT[rowPage.openTab][lang], exact: true }).first().click();
          }
          // Same reason as the affiliate wait above (t_4b5411db): the budget must
          // be this test's, not `waitForSelector`'s page-side clock.
          // `.first()` matches what `waitForSelector` did (it took the first
          // match): `expect(locator).toBeVisible()` is STRICT, so a selector that
          // legitimately matches more than one row — `div.jf-card select.jf-input`
          // matches 2 — would fail as a strict-mode violation rather than a render
          // failure, blaming the page for a locator choice. The assertion is still
          // meaningful on `.first()` because it decides VISIBILITY, not a count.
          await expect(
            page.locator(rowPage.waitFor).first(),
            `${rowPage.name}: the row must render`,
          ).toBeVisible({ timeout: 30_000 });
        }
        // A row behind a click must be OPENED before it can be measured, and the
        // open itself is asserted: if the drawer's own form does not appear, the
        // row below would measure something else (or throw) and the failure would
        // be misattributed. The label is the locale's, from the dictionary.
        if (rowPage.openLabel && rowPage.openSelector) {
          await page.getByRole("button", { name: DICT[rowPage.openLabel][lang], exact: true }).first()
            .click();
          await expect(
            page.locator(rowPage.openSelector),
            `${rowPage.name}: the opened form must appear`,
          ).toBeVisible({ timeout: 30_000 });
        }
        const m = rowPage === ROW_PAGES[0] ? aff : await measureRow(page, rowPage.rowExpr);
        const where = `${rowPage.name} (${rowPage.file})`;

        // RULE 1 — every SINGLE-LINE field can render the text it carries.
        //
        // `input`/`select` cannot wrap, so a placeholder or value wider than the
        // content box is simply not visible — that is the reported defect (a 21.51px
        // "ALI" run in 5px / 0px of content). A `textarea` and a button label wrap
        // by design, so a single-line comparison is not a valid criterion for them;
        // RULE 2 below is what catches a label that is genuinely clipped.
        //
        // The full-run term applies to text that is a TOKEN (a code, a URL, a
        // domain) or a short label — something the user cannot reconstruct from a
        // partial render. A row may name `hintPlaceholderKeys` for a placeholder
        // that is a SENTENCE of help text (e.g. "Signing secret (leave blank to
        // keep current)", measured at 307.56px — wider than any 320px field, and
        // truncated in every locale by design); a character-count test alone is
        // locale-fragile, because the same sentence is 46 characters in `en` and
        // only 12 in `zh`, so it would enforce the full render for one translation
        // and exempt the other. Those fields keep the "reads as empty" floor.
        for (const c of m.controls) {
          if (c.tag !== "input" && c.tag !== "select") continue;
          const isHint = (rowPage.hintPlaceholderKeys ?? []).some(
            (k) => c.text === DICT[k][lang],
          );
          const short = !isHint && c.text.length > 0 && c.text.length <= SHORT_TEXT_MAX_CHARS;
          const floor = short ? Math.max(c.textRun + RUN_SLACK_PX, FIELD_FLOOR_PX) : FIELD_FLOOR_PX;
          expect(
            c.contentW,
            `${lang}@${width} ${where}: ${c.tag} ${JSON.stringify(c.text)} has only ` +
              `${c.contentW}px of content width (box ${c.clientW}px) but must render a ` +
              `${c.textRun}px glyph run — the field reads as empty`,
          ).toBeGreaterThanOrEqual(floor);
        }

        // RULE 2 — nothing overflows itself or its row.
        //
        // A field that holds a VALUE is excluded from the per-control clause: an
        // `input` whose value is wider than its box scrolls that text, which is how
        // every filled text field behaves (measured: the webhooks edit drawer's URL
        // field reports scrollWidth 269 against clientWidth 186 purely from its
        // pre-filled value — clear the value and scrollWidth drops to 186). That is
        // not the clipping this rule is for; an EMPTY field's scrollWidth is what
        // catches a real overflow, and the row clause below is unaffected.
        for (const c of m.controls) {
          if (c.hasValue && c.tag === "input") continue;
          expect(
            c.scrollW,
            `${lang}@${width} ${where}: ${c.tag} ${JSON.stringify(c.text)} overflows ` +
              `internally (scrollWidth ${c.scrollW} > clientWidth ${c.clientW})`,
          ).toBeLessThanOrEqual(c.clientW + SLACK_PX);
        }
        // A button's label must actually be rendered: its glyph run has to fit
        // within the sum of its own content lines (2 lines allowed, matching the
        // affiliate button's legitimate two-line wrap in ms/ta).
        for (const c of m.controls) {
          if (c.tag !== "button" || c.text.length === 0) continue;
          expect(
            c.textRun,
            `${lang}@${width} ${where}: button label ${JSON.stringify(c.text)} is ` +
              `${c.textRun}px against ${c.contentW}px of content width`,
          ).toBeLessThanOrEqual(c.contentW * 2 + RUN_SLACK_PX);
        }
        expect(
          m.rowScrollW,
          `${lang}@${width} ${where}: the row overflows its container ` +
            `(scrollWidth ${m.rowScrollW} > clientWidth ${m.rowClientW})`,
        ).toBeLessThanOrEqual(m.rowClientW + SLACK_PX);

        // RULE 3 — the page never scrolls sideways.
        const docOverflow = await page.evaluate(
          "document.documentElement.scrollWidth - document.documentElement.clientWidth",
        );
        expect(docOverflow, `${lang}@${width} ${where}: the page scrolls sideways`).toBeLessThanOrEqual(
          SLACK_PX,
        );

        // RULE 4 — a single action button must not dominate a mixed field row.
        if (rowPage.isButtonRow) {
          const fields = m.controls.filter((c) => c.tag !== "button");
          const btn = m.controls.find((c) => c.tag === "button");
          if (btn && fields.length) {
            const widest = Math.max(...fields.map((f) => f.boxW));
            expect(
              btn.boxW / widest,
              `${lang}@${width} ${where}: the action button is ` +
                `${(btn.boxW / widest).toFixed(2)}x the width of a field beside it ` +
                `(button ${btn.boxW}px vs field ${widest}px)`,
            ).toBeLessThanOrEqual(MAX_BUTTON_FIELD_RATIO);
          }
        }
      }

      if (width === 1024) {
        // Desktop is unchanged in spirit: still ONE line of fields under the help
        // text, still bounded by the max-w-xl column.
        const heights = new Set(aff.controls.filter((c) => c.tag === "input").map((c) => c.boxH));
        expect(heights.size, `desktop: the inputs are no longer one equal-height row`).toBe(1);
        expect(aff.rowScrollW, `desktop: the affiliate row is no longer bounded`).toBeLessThanOrEqual(
          DESKTOP_MAX_ROW_PX + SLACK_PX,
        );
      }
    });
  }
}
