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

import { signIn } from "./auth";

// The dashboard paths are gated on the SERVER (web/src/middleware.ts): a request
// without the `jf_session` cookie is redirected to /login BEFORE the page
// renders. This spec visits one, so it declares the session it means — before
// the gate existed an anonymous visit rendered the account shell, so this spec
// could (and did) rely on the bug. See e2e/auth.ts.
test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});

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

// expectLinksBeside waits until the locator holds AT LEAST `want` elements, then
// returns them.
//
// WHY NOT A BARE `.count()`. The reported failure (cards t_8a25af49 / t_e37ce316)
// is `expect(count).toBeGreaterThanOrEqual(2)` receiving 0 **while the same run's
// Playwright page snapshot contains both links** — the DOM had them and a
// one-shot count ran before they were in it. That is a false RED in a gate every
// PR depends on, and it is load-sensitive: the spec passes standalone and reds
// inside a full suite on a loaded host.
//
// A bare count is a read of one instant; Playwright's web-first assertions
// retry, so the wait is the fix rather than a longer sleep. `.first()` is NOT
// used here: as cards t_8a25af49 and t_864adaee both note, a `.first()` locator
// always resolves to exactly one element, so a count assertion on it can never
// fail and would read as stronger than it is.
async function expectLinksBeside(
  page: Page, selector: string, want: number, why: string,
): Promise<ReturnType<Page["locator"]>> {
  const links = page.locator(selector);
  await expect(links, why).toHaveCount(want, { timeout: 15_000 });
  return links;
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

    // The subject and body labels must each carry the "?" doc link. The WAIT is
    // the fix for the false red: the count is retried, not read once.
    const links = await expectLinksBeside(
      page, 'a[href="/docs/customize#language"]', 2,
      "expected a doc link beside BOTH the subject and the body field",
    );
    const count = await links.count();

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

    const links = await expectLinksBeside(
      page, 'a[href="/docs/customize#language"]', 2,
      "expected the doc link on both per-form email fields",
    );
    const count = await links.count();

    const title = await links.first().getAttribute("title");
    expect(title).toContain("judged separately");
    expect(await links.first().innerText()).toBe("?");
  });

  test("the notification-RULE editor states the same rule on its Subject and Body", async ({ page }) => {
    // The rule editor is behind TWO clicks (the tab, then "+ Add notification"),
    // because both are component state — no URL reaches it (card t_864adaee). The
    // per-field language rule applies to exactly the values this panel stores
    // (api/internal/payments/notifications.go decides the rule's subject and body
    // half-by-half), so a merchant editing a rule needs the same pointer the
    // receipt fields above already carry.
    //
    // The assertions use the DELTA the panel adds, not absolute counts: the page
    // already carries the receipt fields' links, and an absolute count would
    // silently re-point at those if the panel ever moved. Measured before/after
    // on this tree: language links 2 -> 4, placeholder links 1 -> 3.
    await setLang(page, "en");
    await stubAPI(page);
    await page.goto(`/form-settings?form=${FORM_ID}`);
    await page.waitForLoadState("networkidle");
    await page.getByRole("button", { name: "Notifications", exact: true }).click();

    const RULE = 'a[href="/docs/customize#language"]';

    // `before` must be the SETTLED count of the receipt fields' own links, not a
    // one-shot read of the locator (card t_373a0b5c).
    //
    // Those two links arrive with the email-settings fetch, a tick after the
    // Notifications tab's render commits. A bare `count()` here can therefore land
    // on 0 while the DOM is about to carry 2, and the delta below then reads
    // `after - before` = 4 - 0 and asserts 4 — the exact false RED the `e2e` gate
    // produced on a loaded host (measured 4-of-8 under load; the read at the old
    // :203 saw 0 while the very next 25ms sample already saw 2). Waiting for the
    // settled base removes the window; it is a wait on real elements, not a longer
    // sleep and not a retried delta.
    const base = await expectLinksBeside(
      page, RULE, 2,
      "the receipt fields' own two language links must settle before the panel is opened",
    );
    const before = await base.count();

    // Open the rule editor. The label is a dictionary string, so match the EN
    // dictionary's own words rather than a hand-typed one.
    await page.getByRole("button", { name: /Add notification|Tambah notifikasi/ }).first().click();

    // The panel is open only if ITS Subject label rendered — and that label is the
    // assertion that the link sits next to the field, not merely on the page.
    const panelSubject = page.locator("label", { hasText: /^Subject \?/ });
    await expect(panelSubject).toHaveCount(1, { timeout: 15_000 });
    await expect(panelSubject.locator(RULE)).toHaveCount(1);

    // …and the panel's OWN Body label carries one too. The panel's half is asserted
    // WHERE IT LIVES (one link inside each panel label) rather than inferred from a
    // page-wide delta alone: a link that landed outside the panel, or a genuinely
    // missing one, still fails here. The sentence in this label is unique to the
    // rule editor's placeholders field, so it cannot re-point at the receipt
    // Body label above.
    const panelBody = page.locator("label", { hasText: "A response form has no sale" });
    await expect(panelBody).toHaveCount(1, { timeout: 15_000 });
    await expect(panelBody.locator(RULE)).toHaveCount(1);

    // BOTH fields in the panel got one, so the delta is exactly two. Both operands
    // are settled: `before` was awaited above, and `after` is read only after the
    // panel's own two labels each proved they carry a link.
    const after = await base.count();
    expect(after - before, `the rule editor added ${after - before} language-rule link(s), want 2 ` +
      `(one on Subject, one on Body)`).toBe(2);

    // …and they carry the localised explanation, so a missing dictionary entry
    // cannot pass as a rendered link. Read from the panel's two labels (settled),
    // not from the page-wide locator's last two in DOM order.
    const titles = [
      await panelSubject.locator(RULE).getAttribute("title"),
      await panelBody.locator(RULE).getAttribute("title"),
    ];
    for (const t of titles) {
      expect(t ?? "", `a rule-editor language link has title ${JSON.stringify(t)}`).toContain("judged separately");
    }

    // The placeholder link is a DIFFERENT affordance and survives beside it.
    const PH = 'a[href="/docs/placeholders"]';
    expect(await page.locator(PH).count()).toBeGreaterThanOrEqual(2);
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

    // Count the FULL locator, not a `.first()` one: a `.first()` locator always
    // resolves to exactly one element, so `toHaveCount(1)` on it could never fail
    // and the assertion read as stronger than it was (card t_864adaee). The real
    // count on /settings?tab=emel is TWO — both the receipt Subject and Body carry
    // the link — so the honest assertion is 2, not 1.
    const links = await expectLinksBeside(
      page, 'a[href="/docs/customize#language"]', 2,
      "the Emel tab must carry the rule link beside both receipt fields",
    );
    const link = links.first();
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
