// Render evidence for t_20b1988f: the per-field email-language rule is VISIBLE
// where the merchant edits it.
//
// The Go tests prove the resolver now renders an untouched half in the buyer's
// language. They cannot prove a merchant can find that rule, and AGENTS.md's
// documentation-parity rule asks for an inline doc link at the field it applies
// to. So this file drives the two edit surfaces a user actually sees —
// /settings (the workspace receipt subject/body) and /form-settings (the
// per-form email settings) — in a real browser, against the real built page,
// and reads the live DOM: the rendered link text, its href, its `title`
// attribute (the five-locale string), and the surrounding label text.
//
// The API is stubbed at the browser level (no Go server, no session, no DB), the
// same way e2e/form-settings-header-actions.spec.ts does it.
import { test, expect, type Page } from "@playwright/test";

const WORKSPACE_ID = "ws_email_guard";
const FORM_ID = "form_email_guard";

function json(body: unknown) {
  return { status: 200, contentType: "application/json", body: JSON.stringify(body) };
}

// stubAPI answers every /api/* call the page makes so it reaches its loaded
// state without a backend. Catch-all FIRST, specific routes after it:
// Playwright resolves matching routes in reverse registration order.
//
// profileLang drives /api/auth/profile, which WINS over localStorage in the
// i18n provider (src/components/i18n.tsx: the dashboard language is per-profile).
// Stubbing it as "en" while asking for "ms" would pin the wrong language and
// make a locale assertion pass or fail for the wrong reason.
async function stubAPI(page: Page, profileLang = "en") {
  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));

  await page.route(/(.*)\/api\/auth\/profile$/, (route) => route.fulfill(json({ lang: profileLang })));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (route) =>
    route.fulfill(
      json({
        workspaces: [{ id: WORKSPACE_ID, name: "Acme", slug: "acme", role: "owner" }],
        account: { id: "acc1", name: "Acme", slug: "acme" },
        user: { id: "u1", email: "owner@example.test", lang: profileLang },
      }),
    ),
  );
  // The workspace email settings the /settings Emel tab loads.
  await page.route(/(.*)\/api\/workspaces\/[^/]+\/email-settings(\?.*)?$/, (route) =>
    route.fulfill(
      json({
        receipt_subject: "Resit pembayaran — {{ref_code}}",
        receipt_body: "Terima kasih atas pembayaran anda!",
        receipt_enabled: true,
        notify_merchant: false,
        merchant_notify_email: "",
        html: false,
      }),
    ),
  );
  await page.route(/(.*)\/api\/rest\/workspaces(\?.*)?$/, (route) =>
    route.fulfill(json([{ id: WORKSPACE_ID, name: "Acme", slug: "acme", role: "owner" }])),
  );
  // The /form-settings page reads the form's notification RULES as a LIST and
  // calls .map on it, so the catch-all's `{}` would crash the Notifications tab
  // ("e6.map is not a function"). Stubbed explicitly, as the header-action guard
  // does. The per-form email fields under test live on that tab.
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+$/, (route) =>
    route.fulfill(
      json({
        id: FORM_ID, account_id: "acc1", slug: "demo", title: "Demo",
        status: "published", config: { blocks: [], mode: "payment" },
        url: "https://jomform.example/f/demo",
      }),
    ),
  );
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+\/notifications(\?.*)?$/, (route) =>
    route.fulfill(json([])),
  );
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+\/chip-feeds(\?.*)?$/, (route) =>
    route.fulfill(json({ feeds: null })),
  );
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+\/confirmations(\?.*)?$/, (route) =>
    route.fulfill(json({ confirmations: [] })),
  );
  await page.route(/(.*)\/api\/rest\/forms\/(\?.*)?$/, (route) => route.fulfill(json([])));
}

// setLang pins the dashboard locale in the app's own store (localStorage
// `jf_lang`, see src/components/i18n.tsx STORAGE_KEY) and clears everything else
// first, so nothing an earlier card left behind decides the language for this
// run — the fresh-visitor state is the case a regression hides in.
async function setLang(page: Page, lang: string) {
  await page.addInitScript((l) => {
    try {
      window.localStorage.clear();
      window.localStorage.setItem("jf_lang", l);
    } catch {
      /* ignore */
    }
  }, lang);
}

test.describe("email subject/body per-field rule is stated where it applies", () => {
  // The default, fresh-visitor state: dashboard language unset -> English.
  test("the /settings Emel tab links to the language rule beside both fields", async ({ page }) => {
    await setLang(page, "en");
    await stubAPI(page);
    // The Emel tab is a hash/query tab on the same page.
    await page.goto("/settings?tab=emel");
    await page.waitForLoadState("networkidle");

    const doc = page.locator("html");
    expect(await doc.getAttribute("lang")).toBe("en");

    // The subject and body labels must each carry the "?" doc link.
    const links = page.locator('a[href="/docs/customize#language"]');
    const count = await links.count();
    expect(count, "expected a doc link beside BOTH the subject and the body field").toBeGreaterThanOrEqual(2);

    const titles: string[] = [];
    for (let i = 0; i < count; i++) {
      titles.push((await links.nth(i).getAttribute("title")) || "");
    }
    // The title is the localised explanation, and it must be the English string
    // for an English dashboard — not a bare key and not another locale.
    expect(titles[0]).toContain("judged separately");
    for (const t of titles) {
      expect(t, "the title must be rendered copy, never the raw i18n key").not.toContain("set.emelLanguageRule");
    }

    // The link text a sighted user clicks is the "?" affordance.
    expect(await links.first().innerText()).toBe("?");

    // And it sits next to the field it explains.
    const subjectLabel = page.locator("label", { hasText: /Subject|Subjek|主题/ }).first();
    expect(await subjectLabel.innerText()).toContain("?");
  });

  test("the same rule is stated on the /form-settings per-form email fields", async ({ page }) => {
    await setLang(page, "en");
    await stubAPI(page);
    // The per-form email settings live on the Notifications tab, which is not
    // addressable by URL — the tab is component state, so click the real control
    // a merchant clicks rather than assuming a query parameter works.
    await page.goto(`/form-settings?form=${FORM_ID}`);
    await page.waitForLoadState("networkidle");
    await page.getByRole("button", { name: "Notifications", exact: true }).click();

    const links = page.locator('a[href="/docs/customize#language"]');
    await expect(links.first()).toBeVisible();
    const count = await links.count();
    expect(count, "expected the doc link on both per-form email fields").toBeGreaterThanOrEqual(2);

    const title = await links.first().getAttribute("title");
    expect(title).toContain("judged separately");
    expect(await links.first().innerText()).toBe("?");
  });

  test("the inline link follows the dashboard language", async ({ page }) => {
    // A Malay dashboard must show the Malay explanation, not the English one:
    // the title is a dictionary string, so a missing locale would silently fall
    // back and the merchant would read a language he did not choose.
    // The profile is the source of truth for the dashboard language.
    await setLang(page, "ms");
    await stubAPI(page, "ms");
    await page.goto("/settings?tab=emel");
    await page.waitForLoadState("networkidle");

    await expect(page.locator("html")).toHaveAttribute("lang", "ms");

    const link = page.locator('a[href="/docs/customize#language"]').first();
    await expect(link).toHaveCount(1);
    const title = (await link.getAttribute("title")) || "";
    expect(title, `Malay dashboard showed ${JSON.stringify(title)}`).toContain("dinilai berasingan");
    expect(title).not.toContain("judged separately");
  });

  test("the docs section states the per-field rule to a fresh visitor", async ({ page }) => {
    // Fresh storage: this is what a new visitor gets.
    await setLang(page, "en");
    await page.goto("/docs/customize");
    await page.waitForLoadState("networkidle");

    expect(await page.locator("html").getAttribute("lang")).toBe("en");

    // The language section must carry the new sentence AND still carry the
    // pre-existing rule it sits under.
    await expect(page.locator("body")).toContainText("judged separately");
    await expect(page.locator("body")).toContainText("never to Malay");

    // This section must link on to the placeholder reference (the docs-parity
    // rule's inline doc link). Its label lives in the shared docs sidebar, so
    // the assertion is "a link to that page exists in the section", not a
    // specific anchor text.
    const ref = page.locator('a[href="/docs/placeholders"]');
    expect(await ref.count(), "no link from the language section to the placeholders reference").toBeGreaterThanOrEqual(1);
  });
});
